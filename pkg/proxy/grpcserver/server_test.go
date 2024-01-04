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
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto/protoconnect"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/utils/conv"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/controller-mesh/pkg/proxy/circuitbreaker"
)

func TestServer(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ctx, cancel := context.WithCancel(context.TODO())
	wg := sync.WaitGroup{}
	defer func() {
		cancel()
		wg.Wait()
	}()
	breakerMgr := circuitbreaker.NewManager(ctx)
	proxyServer := &GrpcServer{BreakerMgr: breakerMgr}
	wg.Add(1)
	go func() {
		proxyServer.Start(ctx)
		wg.Done()
	}()
	<-time.After(2 * time.Second)
	fmt.Println(proto.TrafficInterceptRule_NORMAL.String())
	grpcClient := protoconnect.NewThrottlingClient(proto.DefaultHttpClient, "http://127.0.0.1:5453")

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
					RecoverPolicy: &ctrlmeshv1alpha1.RecoverPolicy{
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
