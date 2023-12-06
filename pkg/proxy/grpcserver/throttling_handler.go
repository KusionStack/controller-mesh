/*
Copyright 2023 The KusionStack Authors.

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

package grpcserver

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"k8s.io/klog/v2"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	"github.com/KusionStack/controller-mesh/pkg/proxy/circuitbreaker"
)

type grpcThrottlingHandler struct {
	mgr circuitbreaker.ManagerInterface
}

func (g *grpcThrottlingHandler) SendConfig(ctx context.Context, req *connect.Request[ctrlmeshproto.CircuitBreaker]) (*connect.Response[ctrlmeshproto.ConfigResp], error) {

	msg := protojson.MarshalOptions{Multiline: true, EmitUnpopulated: true}.Format(req.Msg)
	klog.Infof("handle CircuitBreaker gRPC request %s", msg)
	if req.Msg == nil {
		return connect.NewResponse(&ctrlmeshproto.ConfigResp{Success: false}), fmt.Errorf("nil CircuitBreaker recieived from client")
	}
	resp, err := g.mgr.Sync(req.Msg)
	return connect.NewResponse(resp), err
}
