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
	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto/protoconnect"
	"github.com/KusionStack/controller-mesh/pkg/proxy/circuitbreaker"
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
	BreakerMgr circuitbreaker.ManagerInterface

	mux *http.ServeMux
}

func (s *GrpcServer) Start(ctx context.Context) {
	s.mux = http.NewServeMux()
	s.mux.Handle(protoconnect.NewThrottlingHandler(&grpcThrottlingServer{mgr: s.BreakerMgr}, connect.WithSendMaxBytes(1024*1024*64)))
	addr := fmt.Sprintf(":%d", grpcServerPort)
	go func() {
		// Use h2c so we can serve HTTP/2 without TLS.
		if err := http.ListenAndServe(addr, h2c.NewHandler(s.mux, &http2.Server{})); err != nil {
			klog.Errorf("serve gRPC error %v", err)
		}
	}()
	<-ctx.Done()
}

type grpcThrottlingServer struct {
	mgr circuitbreaker.ManagerInterface
}

func (g *grpcThrottlingServer) SendConfig(ctx context.Context, req *connect.Request[ctrlmeshproto.CircuitBreaker]) (*connect.Response[ctrlmeshproto.ConfigResp], error) {
	klog.Infof("handle CircuitBreaker gRPC request %+v", req.Msg)
	if req.Msg == nil {
		return connect.NewResponse(&ctrlmeshproto.ConfigResp{Success: false}), fmt.Errorf("nil CircuitBreaker recieived from client")
	}
	resp, err := g.mgr.Sync(req.Msg)
	return connect.NewResponse(resp), err
}
