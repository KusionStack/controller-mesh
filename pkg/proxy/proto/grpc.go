/*
Copyright 2023 The KusionStack Authors.
Copyright 2021 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package proto

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/gogo/protobuf/proto"
	"golang.org/x/net/http2"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto/protoconnect"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	proxycache "github.com/KusionStack/controller-mesh/pkg/proxy/cache"
	"github.com/KusionStack/controller-mesh/pkg/utils"
)

type grpcClient struct {
	proxycache.ManagerStateInterface
	reportTriggerChan chan struct{}
	specManager       *SpecManager

	prevSpec *ctrlmeshproto.ProxySpec
}

func NewGrpcClient(managerStateCache proxycache.ManagerStateInterface) Client {
	return &grpcClient{
		ManagerStateInterface: managerStateCache,
		reportTriggerChan:     make(chan struct{}, 1000),
	}
}

func (c *grpcClient) Start(ctx context.Context) (err error) {
	c.specManager, err = newSpecManager(c.reportTriggerChan)
	if err != nil {
		return fmt.Errorf("error new spec manager: %v", err)
	}
	initChan := make(chan struct{})
	go c.connect(ctx, initChan)
	<-initChan
	return nil
}

func (c *grpcClient) connect(ctx context.Context, initChan chan struct{}) {
	for i := 0; ; i++ {
		if i > 0 {
			time.Sleep(time.Second * 3)
		}
		klog.Infof("Starting grpc connecting...")

		managerState, err := c.Get()
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Warningf("Not found ManagerState %s, waiting...", ctrlmeshv1alpha1.NameOfManager)
			} else {
				klog.Warningf("Failed to get ManagerState %s: %v, waiting...", ctrlmeshv1alpha1.NameOfManager, err)
			}
			continue
		}

		if managerState.Status.Ports == nil || managerState.Status.Ports.GrpcLeaderElectionPort == 0 {
			klog.Warningf("No grpc port in ManagerState %s, waiting...", utils.DumpJSON(managerState))
			continue
		}

		var leader *ctrlmeshv1alpha1.ManagerStateEndpoint
		for i := range managerState.Status.Endpoints {
			e := &managerState.Status.Endpoints[i]
			if e.Leader {
				leader = e
				break
			}
		}
		if leader == nil {
			klog.Warningf("No leader in ManagerState %s, waiting...", utils.DumpJSON(managerState))
			continue
		}

		addr := fmt.Sprintf("%s:%d", leader.PodIP, managerState.Status.Ports.GrpcLeaderElectionPort)
		klog.Infof("Preparing to connect ctrlmesh-manager %v", addr)
		func() {
			ctxWithCancel, cancel := context.WithCancel(ctx)
			defer cancel()

			client := &http.Client{
				Transport: &http2.Transport{
					AllowHTTP: true,
				},
			}
			grpcCtrlMeshClient := protoconnect.NewControllerMeshClient(client, addr, connect.WithGRPC())
			connStream := grpcCtrlMeshClient.Register(ctxWithCancel)
			_, err = connStream.Conn()
			if err != nil {
				klog.Errorf("Failed to register to ctrlmesh-manager %s addr %s: %v", leader.Name, addr, err)
				return
			}

			if err := c.syncing(connStream, initChan); err != nil {
				klog.Errorf("Failed syncing grpc connection to ctrlmesh-manager %s addr %s: %v", leader.Name, addr, err)
			}
		}()
	}
}

func (c *grpcClient) syncing(connStream *connect.BidiStreamForClient[ctrlmeshproto.ProxyStatus, ctrlmeshproto.ProxySpec], initChan chan struct{}) error {
	// Do the first send for self info
	firstStatus := c.specManager.GetStatus()
	if firstStatus == nil {
		firstStatus = &ctrlmeshproto.ProxyStatus{}
	}
	firstStatus.SelfInfo = selfInfo
	klog.Infof("Preparing to send first status: %v", utils.DumpJSON(firstStatus))
	if err := connStream.Send(firstStatus); err != nil {
		return fmt.Errorf("send first status %s error: %v", utils.DumpJSON(firstStatus), err)
	}

	stopChan := make(chan struct{})
	defer close(stopChan)
	go func() {
		// wait for the first recv
		<-initChan

		var prevStatus *ctrlmeshproto.ProxyStatus
		sendTimer := time.NewTimer(time.Minute)
		for {
			select {
			case <-c.reportTriggerChan:
			case <-sendTimer.C:
			case <-stopChan:
				return
			}
			sendTimer.Stop()
			if status, err := c.send(connStream, prevStatus); err != nil {
				klog.Errorf("Failed to send message: %v", err)
			} else {
				// 30 ~ 60s
				sendTimer.Reset(time.Second*30 + time.Millisecond*time.Duration(rand.Intn(6000)))
				if status != nil {
					prevStatus = status
				}
			}
		}
	}()

	isFirstTime := true
	for {
		if isFirstTime {
			klog.Infof("Waiting for the first time recv...")
		}
		err := c.recv(connStream)
		if err != nil {
			return fmt.Errorf("recv error: %v", err)
		}
		isFirstTime = false
		onceInit.Do(func() {
			close(initChan)
		})
		c.reportTriggerChan <- struct{}{}
	}
}

func (c *grpcClient) send(connStream *connect.BidiStreamForClient[ctrlmeshproto.ProxyStatus, ctrlmeshproto.ProxySpec], prevStatus *ctrlmeshproto.ProxyStatus) (*ctrlmeshproto.ProxyStatus, error) {
	newStatus := c.specManager.GetStatus()
	if newStatus == nil {
		klog.Infof("Skip to send gRPC status for it is nil.")
		return nil, nil
	}
	if proto.Equal(newStatus, prevStatus) {
		return nil, nil
	}

	klog.Infof("Preparing to send new status: %v", utils.DumpJSON(newStatus))
	if err := connStream.Send(newStatus); err != nil {
		return nil, fmt.Errorf("send status error: %v", err)
	}
	return newStatus, nil
}

func (c *grpcClient) recv(connStream *connect.BidiStreamForClient[ctrlmeshproto.ProxyStatus, ctrlmeshproto.ProxySpec]) error {
	spec, err := connStream.Receive()
	if err != nil {
		return fmt.Errorf("receive spec error: %v", err)
	}

	if spec == nil || spec.Meta == nil || spec.Limits == nil || len(spec.Limits) == 0 {
		klog.Errorf("Receive invalid proto spec: %v", utils.DumpJSON(spec))
		return nil
	}

	msg := fmt.Sprintf("Receive proto spec (ShardingConfig: '%v', hash: %v)", spec.Meta.ShardName, spec.Meta.Hash)
	if c.prevSpec == nil {
		msg = fmt.Sprintf("%s initial spec: %v", msg, utils.DumpJSON(spec))
	} else if c.prevSpec.Meta.Hash != spec.Meta.Hash {
		if limitsChanged(c.prevSpec.Limits, spec.Limits) {
			msg = fmt.Sprintf("%s route changed: %v", msg, utils.DumpJSON(spec.Limits))
		} else {
			msg = fmt.Sprintf("%s endpoints changed: %v", msg, utils.DumpJSON(spec.Endpoints))
		}
	}
	klog.Info(msg)

	c.specManager.UpdateSpec(spec)
	c.prevSpec = spec
	return nil
}

func limitsChanged(old, new []*ctrlmeshproto.Limit) bool {
	if len(old) != len(new) {
		return false
	}
	for i := 0; i < len(old); i++ {
		if !proto.Equal(old[i], new[i]) {
			return false
		}
	}
	return true
}

func (c *grpcClient) GetSpecManager() *SpecManager {
	return c.specManager
}
