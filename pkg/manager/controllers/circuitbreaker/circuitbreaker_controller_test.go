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

package circuitbreaker

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/constants"
	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/controller-mesh/pkg/proxy/circuitbreaker"
	"github.com/KusionStack/controller-mesh/pkg/proxy/grpcserver"
)

var mockPod = &v1.Pod{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "testpod",
		Namespace: "default",
		Labels: map[string]string{
			"test": "test",
		},
	},
	Spec: v1.PodSpec{
		Containers: []v1.Container{
			{
				Name:  "ctrlmesh-proxy",
				Image: "nginx:v1",
			},
		},
	},
	Status: v1.PodStatus{
		PodIP: "127.0.0.1",
		ContainerStatuses: []v1.ContainerStatus{
			{
				Name:  constants.ProxyContainerName,
				Ready: true,
			},
		},
	},
}

var circuitBreaker = &ctrlmeshv1alpha1.CircuitBreaker{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "testcb",
		Namespace: "default",
	},
	Spec: ctrlmeshv1alpha1.CircuitBreakerSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"test": "test",
			},
		},
		RateLimitings: []*ctrlmeshv1alpha1.Limiting{
			{
				Name: "testLimit",
				ResourceRules: []ctrlmeshv1alpha1.ResourceRule{
					{
						ApiGroups: []string{
							"",
						},
						Resources: []string{
							"Pod",
						},
						Verbs: []string{
							"delete",
						},
						Namespaces: []string{
							"*",
						},
					},
				},
				Bucket: ctrlmeshv1alpha1.Bucket{
					Burst:    500,
					Interval: "10s",
					Limit:    100,
				},
				TriggerPolicy: ctrlmeshv1alpha1.TriggerPolicyLimiterOnly,
				//RecoverPolicy: &ctrlmeshv1alpha1.RecoverPolicy{},
			},
		},
	},
}

func TestCircuitBreaker(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	defer Stop()
	RunMockServer()
	testPod := mockPod.DeepCopy()
	testBreaker := circuitBreaker.DeepCopy()
	g.Expect(c.Create(ctx, testPod)).Should(gomega.BeNil())
	g.Expect(c.Create(ctx, testBreaker)).Should(gomega.BeNil())
	defer func() {
		c.Delete(ctx, testPod)
	}()
	cb := &ctrlmeshv1alpha1.CircuitBreaker{}
	g.Eventually(func() bool {
		if err := c.Get(ctx, types.NamespacedName{Name: "testcb", Namespace: "default"}, cb); err != nil {
			return false
		}
		return cb.Status.TargetStatus != nil
	}, 5*time.Second, 1*time.Second).Should(gomega.BeTrue())
	// pod is not available
	g.Expect(strings.Contains(cb.Status.TargetStatus[0].Message, "not available")).Should(gomega.BeTrue())
	testPod.Status = *mockPod.Status.DeepCopy()

	g.Expect(c.Status().Update(ctx, testPod)).Should(gomega.BeNil())
	g.Eventually(func() string {
		g.Expect(c.Get(ctx, types.NamespacedName{Name: "testcb", Namespace: "default"}, cb)).Should(gomega.BeNil())
		g.Expect(c.Get(ctx, types.NamespacedName{Name: testPod.Name, Namespace: "default"}, testPod)).Should(gomega.BeNil())
		if cb.Status.TargetStatus != nil {
			return cb.Status.TargetStatus[0].PodIP
		}
		return ""
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal("127.0.0.1"))
	g.Expect(len(cb.Finalizers) > 0).Should(gomega.BeTrue())
	cb.Spec.TrafficInterceptRules = append(cb.Spec.TrafficInterceptRules, &ctrlmeshv1alpha1.TrafficInterceptRule{
		Name:          "testIntercept",
		InterceptType: ctrlmeshv1alpha1.InterceptTypeWhitelist,
		ContentType:   ctrlmeshv1alpha1.ContentTypeNormal,
		Contents: []string{
			"xxx.xxx.xxx",
		},
		Methods: []string{
			"GET",
		},
	})
	localCount := syncCount
	g.Expect(c.Update(ctx, cb)).Should(gomega.BeNil())
	g.Eventually(func() bool {
		g.Expect(c.Get(ctx, types.NamespacedName{Name: "testcb", Namespace: "default"}, cb)).Should(gomega.BeNil())
		if len(cb.Spec.TrafficInterceptRules) != 0 && cb.Spec.TrafficInterceptRules[0].Name == "testIntercept" {
			return cb.Status.ObservedGeneration == cb.Generation && syncCount != localCount
		}
		return false
	}, 5*time.Second, 1*time.Second).Should(gomega.BeTrue())
	g.Expect(breakerManager.ValidateTrafficIntercept("aaa.aaa.aaa", "GET").Allowed).Should(gomega.BeFalse())
	if cb.Labels == nil {
		cb.Labels = map[string]string{}
	}
	cb.Labels[ctrlmesh.CtrlmeshCircuitBreakerDisableKey] = "true"
	localCount = syncCount
	g.Expect(c.Update(ctx, cb)).Should(gomega.BeNil())
	g.Eventually(func() bool {
		g.Expect(c.Get(ctx, types.NamespacedName{Name: "testcb", Namespace: "default"}, cb)).Should(gomega.BeNil())
		return len(cb.Status.TargetStatus) == 0 && len(cb.Finalizers) == 0 && syncCount != localCount
	}, 5*time.Second, 1*time.Second).Should(gomega.BeTrue())
	g.Expect(c.Delete(ctx, testBreaker)).Should(gomega.BeNil())
	g.Eventually(func() error {
		return c.Get(ctx, types.NamespacedName{Name: "testcb", Namespace: "default"}, cb)
	}, 5*time.Second, 1*time.Second).Should(gomega.HaveOccurred())
	fmt.Println("test finished")
}

var breakerManager circuitbreaker.ManagerInterface
var syncCount int

func RunMockServer() {
	breakerMgr := circuitbreaker.NewManager(ctx)
	breakerManager = breakerMgr
	proxyGRPCServerPort = 5455
	proxyServer := grpcserver.GrpcServer{BreakerMgr: &mockBreakerManager{breakerMgr}, Port: 5455}
	wg.Add(1)
	go func() {
		proxyServer.Start(ctx)
		wg.Done()
	}()
	<-time.After(2 * time.Second)
}

type mockBreakerManager struct {
	circuitbreaker.ManagerInterface
}

func (m *mockBreakerManager) Sync(config *ctrlmeshproto.CircuitBreaker) (*ctrlmeshproto.ConfigResp, error) {
	resp, err := m.ManagerInterface.Sync(config)
	utilruntime.Must(err)
	printJson(resp)
	syncCount++
	return resp, err
}

func printJson(item any) {
	info, _ := json.MarshalIndent(item, "", "    ")
	fmt.Println(string(info))
}
