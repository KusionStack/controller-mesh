package grpcserver

import (
	"context"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/onsi/gomega"
	"golang.org/x/net/http2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto/protoconnect"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/controller-mesh/pkg/proxy/circuitbreaker"
	"github.com/KusionStack/controller-mesh/pkg/utils/conv"
)

func TestServer(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ctx := context.TODO()
	grpcServerPort = 8889
	breakerMgr := circuitbreaker.NewManager(ctx)
	proxyServer := &GrpcServer{BreakerMgr: breakerMgr}
	go proxyServer.Start(ctx)

	grpcClient := protoconnect.NewThrottlingClient(&http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
		},
	}, "127.0.0.1:8889")

	cb := &ctrlmeshv1alpha1.CircuitBreaker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName",
			Namespace: "default",
			Annotations: map[string]string{
				"test": "test",
			},
			Labels: map[string]string{},
		},
		Spec: ctrlmeshv1alpha1.CircuitBreakerSpec{
			RateLimitings: []*ctrlmeshv1alpha1.Limiting{
				{
					Name: "deletePod-create-circuit-breaker",
					Bucket: ctrlmeshv1alpha1.Bucket{
						Interval: "1m",
						Burst:    100,
						Limit:    60,
					},
					ResourceRules: []ctrlmeshv1alpha1.ResourceRule{
						{
							Namespaces: []string{"*"},
							ApiGroups:  []string{""},
							Resources:  []string{"pod"},
							Verbs:      []string{"delete"},
						},
					},
					TriggerPolicy: ctrlmeshv1alpha1.TriggerPolicyNormal,
					RecoverPolicy: ctrlmeshv1alpha1.RecoverPolicy{
						RecoverType: ctrlmeshv1alpha1.RecoverPolicyManual,
					},
				},
			},
		},
	}

	protoCB := conv.ConvertCircuitBreaker(cb)
	protoCB.Option = proto.CircuitBreaker_UPDATE

	grpcResp, err := grpcClient.SendConfig(ctx, connect.NewRequest(protoCB))
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(grpcResp.Msg.Success).Should(gomega.BeTrue())
}

var (
	testCB = &proto.CircuitBreaker{
		Option:                proto.CircuitBreaker_UPDATE,
		ConfigHash:            "123",
		Name:                  "test-breaker",
		TrafficInterceptRules: []*proto.TrafficInterceptRule{},
		RateLimitings:         []*proto.RateLimiting{},
	}
)
