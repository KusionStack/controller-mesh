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

package shardingconfigserver

import (
	"context"
	"fmt"
	"io"
	"sync"

	"connectrpc.com/connect"
	"github.com/gogo/protobuf/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto/protoconnect"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/controller-mesh/pkg/grpcregistry"
	"github.com/KusionStack/controller-mesh/pkg/utils"
)

var (
	grpcServer = &GrpcServer{}

	grpcRecvTriggerChannel = make(chan event.GenericEvent, 1024)

	// cachedGrpcSrvConnection type is map[types.UID]*grpcSrvConnection
	cachedGrpcSrvConnection = &sync.Map{}
)

func init() {
	_ = grpcregistry.Register("ctrlmesh-server", true, func(opts grpcregistry.RegisterOptions) {
		grpcServer.reader = opts.Mgr.GetCache()
		grpcServer.ctx = opts.Ctx
		opts.ServeMux.Handle(protoconnect.NewControllerMeshHandler(grpcServer, connect.WithSendMaxBytes(1024*1024*64)))
	})
}

type grpcSrvConnection struct {
	mu     sync.Mutex
	stream *connect.BidiStream[ctrlmeshproto.ProxyStatus, ctrlmeshproto.ProxySpec]
	status *ctrlmeshproto.ProxyStatus

	sendTimes    int
	disconnected bool
}

func (conn *grpcSrvConnection) send(spec *ctrlmeshproto.ProxySpec) error {
	conn.mu.Lock()
	conn.sendTimes++
	conn.mu.Unlock()
	return conn.stream.Send(spec)
}

type GrpcServer struct {
	reader client.Reader
	ctx    context.Context
}

var _ protoconnect.ControllerMeshHandler = &GrpcServer{}

func (s *GrpcServer) Register(ctx context.Context, stream *connect.BidiStream[ctrlmeshproto.ProxyStatus, ctrlmeshproto.ProxySpec]) error {
	// receive the first register message
	pStatus, err := stream.Receive()
	if err != nil {
		return status.Errorf(codes.Aborted, err.Error())
	}
	if pStatus.SelfInfo == nil || pStatus.SelfInfo.Namespace == "" || pStatus.SelfInfo.Name == "" {
		return status.Errorf(codes.InvalidArgument, fmt.Sprintf("invalid selfInfo: %+v", pStatus.SelfInfo))
	}
	podNamespacedName := types.NamespacedName{Namespace: pStatus.SelfInfo.Namespace, Name: pStatus.SelfInfo.Name}
	pod := &v1.Pod{}
	if err := s.reader.Get(context.TODO(), podNamespacedName, pod); err != nil {
		if errors.IsNotFound(err) {
			return status.Errorf(codes.NotFound, fmt.Sprintf("not found pod %s", podNamespacedName))
		}
		return status.Errorf(codes.Internal, fmt.Sprintf("get pod %s error: %v", podNamespacedName, err))
	} else if !utils.IsPodActive(pod) {
		return status.Errorf(codes.Canceled, fmt.Sprintf("find pod %s inactive", podNamespacedName))
	}
	shardName := pod.Labels[ctrlmeshv1alpha1.ShardingConfigInjectedKey]
	if shardName == "" {
		return status.Errorf(codes.InvalidArgument, fmt.Sprintf("empty %s label in pod %s", ctrlmeshv1alpha1.ShardingConfigInjectedKey, podNamespacedName))
	}

	if pStatus.MetaState == nil {
		klog.Infof("Start first-time connection from Pod %s in ShardingConfig %s", podNamespacedName, shardName)
	} else {
		klog.Infof("Start re-connection from Pod %s in ShardingConfig %s", podNamespacedName, shardName)
	}

	conn := &grpcSrvConnection{stream: stream, status: pStatus}
	cachedGrpcSrvConnection.Store(pod.UID, conn)
	podHashExpectation.Delete(pod.UID)

	genericEvent := event.GenericEvent{Object: &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: podNamespacedName.Namespace, Name: shardName}}}
	grpcRecvTriggerChannel <- genericEvent
	go func() {
		for {
			pStatus, err = stream.Receive()
			if err != nil {
				if err == io.EOF {
					return
				}
				select {
				case <-ctx.Done():
				default:
					klog.Errorf("Receive error from Pod %s in ShardingConfig %s: %v", podNamespacedName, shardName, err)
				}
				return
			}
			klog.Infof("Get proto status from Pod %s in ShardingConfig %s: %v", podNamespacedName, shardName, utils.DumpJSON(pStatus))

			conn.mu.Lock()
			statusChanged := !proto.Equal(conn.status, pStatus)
			// overwrite the whole status to avoid race condition
			conn.status = pStatus
			conn.mu.Unlock()

			if statusChanged {
				grpcRecvTriggerChannel <- genericEvent
			}
		}
	}()

	select {
	case <-s.ctx.Done():
		return nil
	case <-ctx.Done():
	}
	klog.Infof("Stop connection from Pod %s in ShardingConfig %s", podNamespacedName, shardName)
	podHashExpectation.Delete(pod.UID)
	// Can NOT delete this conn in cachedGrpcSrvConnection
	conn.mu.Lock()
	conn.disconnected = true
	conn.mu.Unlock()
	grpcRecvTriggerChannel <- genericEvent
	return nil
}
