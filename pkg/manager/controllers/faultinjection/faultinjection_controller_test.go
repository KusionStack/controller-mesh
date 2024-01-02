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

package faultinjection

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
	"github.com/KusionStack/controller-mesh/pkg/proxy/faultinjection"
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

var faultInjection = &ctrlmeshv1alpha1.FaultInjection{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "testcb",
		Namespace: "default",
	},
	Spec: ctrlmeshv1alpha1.FaultInjectionSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"test": "test",
			},
		},
		HTTPFaultInjections: []*ctrlmeshv1alpha1.HTTPFaultInjection{
			{
				Delay: &ctrlmeshv1alpha1.HTTPFaultInjectionDelay{
					Percent:    "100",
					FixedDelay: "20s",
				},
				Match: &ctrlmeshv1alpha1.HTTPMatchRequest{
					RelatedResources: []*ctrlmeshv1alpha1.ResourceMatch{
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
				},
			},
		},
	},
}

func TestCircuitBreaker(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	defer Stop()
	RunMockServer()
	testPod := mockPod.DeepCopy()
	testFaulltInjection := faultInjection.DeepCopy()
	g.Expect(c.Create(ctx, testPod)).Should(gomega.BeNil())
	g.Expect(c.Create(ctx, testFaulltInjection)).Should(gomega.BeNil())
	defer func() {
		c.Delete(ctx, testPod)
	}()
	waitProcess()
	cb := &ctrlmeshv1alpha1.FaultInjection{}
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "testcb", Namespace: "default"}, cb)).Should(gomega.BeNil())
	g.Expect(cb.Status.TargetStatus).ShouldNot(gomega.BeNil())
	// pod is not available
	g.Expect(strings.Contains(cb.Status.TargetStatus[0].Message, "not available")).Should(gomega.BeTrue())
	testPod.Status = *mockPod.Status.DeepCopy()
	g.Expect(c.Status().Update(ctx, testPod)).Should(gomega.BeNil())
	waitProcess()
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "testcb", Namespace: "default"}, cb)).Should(gomega.BeNil())
	g.Expect(cb.Status.TargetStatus).ShouldNot(gomega.BeNil())
	// pod is available
	g.Expect(cb.Status.TargetStatus[0].PodIP).Should(gomega.BeEquivalentTo("127.0.0.1"))
	g.Expect(len(cb.Finalizers) > 0).Should(gomega.BeTrue())
	cb.Spec.HTTPFaultInjections = append(cb.Spec.HTTPFaultInjections, &ctrlmeshv1alpha1.HTTPFaultInjection{
		Name: "test-abort",
		Abort: &ctrlmeshv1alpha1.HTTPFaultInjectionAbort{
			Percent:    "100",
			HttpStatus: 404,
		},
		Match: &ctrlmeshv1alpha1.HTTPMatchRequest{
			RelatedResources: []*ctrlmeshv1alpha1.ResourceMatch{
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
			RestRules: []*ctrlmeshv1alpha1.MultiRestRule{
				{
					URL:    []string{"aaa.aaa.aaa"},
					Method: []string{"GET"},
				},
			},
		},
	})
	g.Expect(c.Update(ctx, cb)).Should(gomega.BeNil())
	waitProcess()
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "testcb", Namespace: "default"}, cb)).Should(gomega.BeNil())
	g.Expect(faultManager.FaultInjectionRest("aaa.aaa.aaa", "GET").Abort).Should(gomega.BeFalse())
	if cb.Labels == nil {
		cb.Labels = map[string]string{}
	}
	cb.Labels[ctrlmesh.CtrlmeshFaultInjectionDisableKey] = "true"
	g.Expect(c.Update(ctx, cb)).Should(gomega.BeNil())
	waitProcess()
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "testcb", Namespace: "default"}, cb)).Should(gomega.BeNil())
	g.Expect(len(cb.Status.TargetStatus) == 0).Should(gomega.BeTrue())
	g.Expect(len(cb.Finalizers) == 0).Should(gomega.BeTrue())
	g.Expect(c.Delete(ctx, testFaulltInjection)).Should(gomega.BeNil())
	waitProcess()
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "testcb", Namespace: "default"}, cb)).Should(gomega.HaveOccurred())
	fmt.Println("test finished")
}

var faultManager faultinjection.ManagerInterface

func RunMockServer() {
	faultInjectionMgr := faultinjection.NewManager(ctx)
	faultManager = faultInjectionMgr
	proxyServer := grpcserver.GrpcServer{FaultInjectionMgr: &mockFaultInjectionManager{faultInjectionMgr}}
	go proxyServer.Start(ctx)
	<-time.After(2 * time.Second)
}

type mockFaultInjectionManager struct {
	faultinjection.ManagerInterface
}

func (m *mockFaultInjectionManager) Sync(config *ctrlmeshproto.FaultInjection) (*ctrlmeshproto.FaultInjectConfigResp, error) {
	resp, err := m.ManagerInterface.Sync(config)
	utilruntime.Must(err)
	printJson(resp)
	return resp, err
}

func printJson(item any) {
	info, _ := json.MarshalIndent(item, "", "    ")
	fmt.Println(string(info))
}