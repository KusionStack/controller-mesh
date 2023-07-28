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
	"time"

	"github.com/gogo/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kridgeproto "github.com/KusionStack/kridge/pkg/apis/kridge/proto"
	kridgev1alpha1 "github.com/KusionStack/kridge/pkg/apis/kridge/v1alpha1"
	kridgeclient "github.com/KusionStack/kridge/pkg/client"
	kridgeinformers "github.com/KusionStack/kridge/pkg/client/informers/externalversions/kridge/v1alpha1"
	kridgev1alpha1listers "github.com/KusionStack/kridge/pkg/client/listers/kridge/v1alpha1"
	"github.com/KusionStack/kridge/pkg/utils"
)

type grpcClient struct {
	informer cache.SharedIndexInformer
	lister   kridgev1alpha1listers.ManagerStateLister

	reportTriggerChan chan struct{}
	specManager       *SpecManager

	prevSpec *kridgeproto.ProxySpec
}

func NewGrpcClient() Client {
	return &grpcClient{reportTriggerChan: make(chan struct{}, 1000)}
}

func (c *grpcClient) Start(ctx context.Context) (err error) {
	c.specManager, err = newSpecManager(c.reportTriggerChan)
	if err != nil {
		return fmt.Errorf("error new spec manager: %v", err)
	}

	clientset := kridgeclient.GetGenericClient().KridgeClient
	c.informer = kridgeinformers.NewFilteredManagerStateInformer(clientset, 0, cache.Indexers{}, func(opts *metav1.ListOptions) {
		opts.FieldSelector = "metadata.name=" + kridgev1alpha1.NameOfManager
	})
	c.lister = kridgev1alpha1listers.NewManagerStateLister(c.informer.GetIndexer())

	go func() {
		c.informer.Run(ctx.Done())
	}()
	if ok := cache.WaitForCacheSync(ctx.Done(), c.informer.HasSynced); !ok {
		return fmt.Errorf("error waiting ManagerState informer synced")
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

		managerState, err := c.lister.Get(kridgev1alpha1.NameOfManager)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Warningf("Not found ManagerState %s, waiting...", kridgev1alpha1.NameOfManager)
			} else {
				klog.Warningf("Failed to get ManagerState %s: %v, waiting...", kridgev1alpha1.NameOfManager, err)
			}
			continue
		}

		if managerState.Status.Ports == nil || managerState.Status.Ports.GrpcLeaderElectionPort == 0 {
			klog.Warningf("No grpc port in ManagerState %s, waiting...", utils.DumpJSON(managerState))
			continue
		}

		var leader *kridgev1alpha1.ManagerStateEndpoint
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
		klog.Infof("Preparing to connect kridge-manager %v", addr)
		func() {
			var opts []grpc.DialOption
			opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
			grpcConn, err := grpc.Dial(addr, opts...)
			if err != nil {
				klog.Errorf("Failed to grpc connect to kridge-manager %s addr %s: %v", leader.Name, addr, err)
				return
			}
			ctx, cancel := context.WithCancel(ctx)
			defer func() {
				cancel()
				_ = grpcConn.Close()
			}()

			grpcCtrlMeshClient := kridgeproto.NewControllerMeshClient(grpcConn)
			connStream, err := grpcCtrlMeshClient.Register(ctx)
			if err != nil {
				klog.Errorf("Failed to register to kridge-manager %s addr %s: %v", leader.Name, addr, err)
				return
			}

			if err := c.syncing(connStream, initChan); err != nil {
				klog.Errorf("Failed syncing grpc connection to kridge-manager %s addr %s: %v", leader.Name, addr, err)
			}
		}()
	}
}

func (c *grpcClient) syncing(connStream kridgeproto.ControllerMesh_RegisterClient, initChan chan struct{}) error {
	// Do the first send for self info
	firstStatus := c.specManager.GetStatus()
	if firstStatus == nil {
		firstStatus = &kridgeproto.ProxyStatus{}
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

		var prevStatus *kridgeproto.ProxyStatus
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

func (c *grpcClient) send(connStream kridgeproto.ControllerMesh_RegisterClient, prevStatus *kridgeproto.ProxyStatus) (*kridgeproto.ProxyStatus, error) {
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

func (c *grpcClient) recv(connStream kridgeproto.ControllerMesh_RegisterClient) error {
	spec, err := connStream.Recv()
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

func limitsChanged(old, new []*kridgeproto.Limit) bool {
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
