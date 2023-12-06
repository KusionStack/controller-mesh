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

	"connectrpc.com/connect"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
)

type grpcFaultInjectHandler struct {
}

func (f *grpcFaultInjectHandler) SendConfig(ctx context.Context, req *connect.Request[proto.FaultInjection]) (*connect.Response[proto.InjectResp], error) {

	// TODO: update config

	return nil, nil
}
