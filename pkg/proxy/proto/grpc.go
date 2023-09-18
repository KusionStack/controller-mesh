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

	ctrlmeshproto "github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh/proto"
	ctrlmeshv1alpha1 "github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh/v1alpha1"
	ctrlmeshclient "github.com/KusionStack/ctrlmesh/pkg/client"
	ctrlmeshinformers "github.com/KusionStack/ctrlmesh/pkg/client/informers/externalversions/ctrlmesh/v1alpha1"
	ctrlmeshv1alpha1listers "github.com/KusionStack/ctrlmesh/pkg/client/listers/ctrlmesh/v1alpha1"
	"github.com/KusionStack/ctrlmesh/pkg/utils"
)

type grpcClient struct {
	informer cache.SharedIndexInformer
	lister   ctrlmeshv1alpha1listers.ManagerStateLister

	reportTriggerChan chan struct{}
	specManager       *SpecManager

	prevSpec *ctrlmeshproto.ProxySpec
}

func NewGrpcClient() Client {
	return &grpcClient{reportTriggerChan: make(chan struct{}, 1000)}
}

func (c *grpcClient) Start(ctx context.Context) (err error) {
	c.specManager, err = newSpecManager(c.reportTriggerChan)
	if err != nil {
		return fmt.Errorf("error new spec manager: %v", err)
	}

	clientset := ctrlmeshclient.GetGenericClient().MeshClient
	c.informer = ctrlmeshinformers.NewFilteredManagerStateInformer(clientset, 0, cache.Indexers{}, func(opts *metav1.ListOptions) {
		opts.FieldSelector = "metadata.name=" + ctrlmeshv1alpha1.NameOfManager
	})
	c.lister = ctrlmeshv1alpha1listers.NewManagerStateLister(c.informer.GetIndexer())

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

		managerState, err := c.lister.Get(ctrlmeshv1alpha1.NameOfManager)
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
			var opts []grpc.DialOption
			opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
			grpcConn, err := grpc.Dial(addr, opts...)
			if err != nil {
				klog.Errorf("Failed to grpc connect to ctrlmesh-manager %s addr %s: %v", leader.Name, addr, err)
				return
			}
			ctx, cancel := context.WithCancel(ctx)
			defer func() {
				cancel()
				_ = grpcConn.Close()
			}()

			grpcCtrlMeshClient := ctrlmeshproto.NewControllerMeshClient(grpcConn)
			connStream, err := grpcCtrlMeshClient.Register(ctx)
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

func (c *grpcClient) syncing(connStream ctrlmeshproto.ControllerMesh_RegisterClient, initChan chan struct{}) error {
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

func (c *grpcClient) send(connStream ctrlmeshproto.ControllerMesh_RegisterClient, prevStatus *ctrlmeshproto.ProxyStatus) (*ctrlmeshproto.ProxyStatus, error) {
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

func (c *grpcClient) recv(connStream ctrlmeshproto.ControllerMesh_RegisterClient) error {
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
