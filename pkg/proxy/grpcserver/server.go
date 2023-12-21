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
	"net/http"
	"os"
	"strconv"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"k8s.io/klog/v2"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/constants"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto/protoconnect"
	"github.com/KusionStack/controller-mesh/pkg/proxy/circuitbreaker"
	"github.com/KusionStack/controller-mesh/pkg/proxy/faultinjection"
)

var (
	grpcServerPort = constants.ProxyGRPCServerPort
)

func init() {
	envConfig := os.Getenv(constants.EnvProxyGRPCServerPort)
	if envConfig != "" {
		p, err := strconv.Atoi(envConfig)
		if err != nil {
			grpcServerPort = p
		}
	}
}

type GrpcServer struct {
	BreakerMgr        circuitbreaker.ManagerInterface
	FaultInjectionMgr faultinjection.ManagerInterface

	mux *http.ServeMux
}

func (s *GrpcServer) Start(ctx context.Context) {
	s.mux = http.NewServeMux()
	s.mux.Handle(protoconnect.NewThrottlingHandler(&grpcThrottlingHandler{mgr: s.BreakerMgr}, connect.WithSendMaxBytes(1024*1024*64)))
	s.mux.Handle(protoconnect.NewFaultInjectHandler(&grpcFaultInjectHandler{mgr: s.FaultInjectionMgr}, connect.WithSendMaxBytes(1024*1024*64)))
	addr := fmt.Sprintf(":%d", grpcServerPort)
	go func() {
		// Use h2c so we can serve HTTP/2 without TLS.
		if err := http.ListenAndServe(addr, h2c.NewHandler(s.mux, &http2.Server{})); err != nil {
			klog.Errorf("serve gRPC error %v", err)
		}
	}()
	<-ctx.Done()
}
