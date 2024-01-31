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
	"net/url"
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
		Name:      "testpod2",
		Namespace: "default",
		Labels: map[string]string{
			"test2": "test2",
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
		Name:      "testfi",
		Namespace: "default",
	},
	Spec: ctrlmeshv1alpha1.FaultInjectionSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"test2": "test2",
			},
		},
		HTTPFaultInjections: []*ctrlmeshv1alpha1.HTTPFaultInjection{
			{
				Delay: &ctrlmeshv1alpha1.HTTPFaultInjectionDelay{
					Percent:    "100",
					FixedDelay: "20ms",
				},
				Match: &ctrlmeshv1alpha1.Match{
					Resources: []*ctrlmeshv1alpha1.ResourceMatch{
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
				Name: "test-default",
			},
		},
	},
}

func TestFaultInjection(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	defer Stop()
	wg.Add(1)
	RunMockServer()
	testPod := mockPod.DeepCopy()
	testFaultInjection := faultInjection.DeepCopy()
	g.Expect(c.Create(ctx, testPod)).Should(gomega.BeNil())
	g.Expect(c.Create(ctx, testFaultInjection)).Should(gomega.BeNil())
	defer func() {
		c.Delete(ctx, testPod)
	}()
	cb := &ctrlmeshv1alpha1.FaultInjection{}

	g.Eventually(func() bool {
		if err := c.Get(ctx, types.NamespacedName{Name: "testfi", Namespace: "default"}, cb); err != nil {
			return false
		}
		return cb.Status.TargetStatus != nil
	}, 5*time.Second, 1*time.Second).Should(gomega.BeTrue())

	// pod is not available
	g.Expect(strings.Contains(cb.Status.TargetStatus[0].Message, "not available")).Should(gomega.BeTrue())
	testPod.Status = *mockPod.Status.DeepCopy()
	g.Expect(c.Status().Update(ctx, testPod)).Should(gomega.BeNil())
	g.Eventually(func() string {
		g.Expect(c.Get(ctx, types.NamespacedName{Name: "testfi", Namespace: "default"}, cb)).Should(gomega.BeNil())
		if cb.Status.TargetStatus != nil {
			return cb.Status.TargetStatus[0].PodIP
		}
		return ""
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal("127.0.0.1"))

	g.Expect(len(cb.Finalizers) > 0).Should(gomega.BeTrue())
	cb.Spec.HTTPFaultInjections = append(cb.Spec.HTTPFaultInjections, &ctrlmeshv1alpha1.HTTPFaultInjection{
		Name: "test-abort",
		Abort: &ctrlmeshv1alpha1.HTTPFaultInjectionAbort{
			Percent:    "100",
			HttpStatus: 404,
		},
		Match: &ctrlmeshv1alpha1.Match{
			Resources: []*ctrlmeshv1alpha1.ResourceMatch{
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
			HttpMatch: []*ctrlmeshv1alpha1.HttpMatch{
				{
					Host: &ctrlmeshv1alpha1.MatchContent{
						Exact: "aaa.aaa.aaa",
					},
					Method: "GET",
				},
			},
		},
	})
	localCount := syncCount
	g.Expect(c.Update(ctx, cb)).Should(gomega.BeNil())

	g.Eventually(func() bool {
		g.Expect(c.Get(ctx, types.NamespacedName{Name: "testfi", Namespace: "default"}, cb)).Should(gomega.BeNil())
		if len(cb.Spec.HTTPFaultInjections) > 0 && cb.Spec.HTTPFaultInjections[len(cb.Spec.HTTPFaultInjections)-1].Name == "test-abort" {
			return cb.Status.ObservedGeneration == cb.Generation && syncCount != localCount
		}
		return false
	}, 5*time.Second, 1*time.Second).Should(gomega.BeTrue())

	testUrl, _ := url.Parse("https://aaa.aaa.aaa")
	g.Expect(faultManager.GetInjectorByUrl(testUrl, "GET").Abort()).Should(gomega.BeTrue())
	testUrl, _ = url.Parse("https://aaa.aaa.bbb")
	g.Expect(faultManager.GetInjectorByUrl(testUrl, "GET").Abort()).Should(gomega.BeFalse())
	g.Expect(faultManager.GetInjectorByResource("default", "", "Pod", "delete").Abort()).Should(gomega.BeTrue())
	g.Expect(faultManager.GetInjectorByResource("default", "", "Pod", "create").Abort()).Should(gomega.BeFalse())
	if cb.Labels == nil {
		cb.Labels = map[string]string{}
	}
	cb.Labels[ctrlmesh.CtrlmeshFaultInjectionDisableKey] = "true"
	localCount = syncCount
	g.Expect(c.Update(ctx, cb)).Should(gomega.BeNil())
	g.Eventually(func() bool {
		g.Expect(c.Get(ctx, types.NamespacedName{Name: "testfi", Namespace: "default"}, cb)).Should(gomega.BeNil())
		return len(cb.Status.TargetStatus) == 0 && len(cb.Finalizers) == 0 && syncCount != localCount
	}, 5*time.Second, 1*time.Second).Should(gomega.BeTrue())
	g.Expect(c.Delete(ctx, testFaultInjection)).Should(gomega.BeNil())
	g.Eventually(func() error {
		return c.Get(ctx, types.NamespacedName{Name: "testfi", Namespace: "default"}, cb)
	}, 5*time.Second, 1*time.Second).Should(gomega.HaveOccurred())
	fmt.Println("test finished")
}

var faultManager faultinjection.ManagerInterface

func RunMockServer() {
	faultInjectionMgr := faultinjection.NewManager(ctx)
	faultManager = faultInjectionMgr
	proxyGRPCServerPort = 5456
	proxyServer := grpcserver.GrpcServer{FaultInjectionMgr: &mockFaultInjectionManager{faultInjectionMgr}, Port: 5456}
	go func() {
		proxyServer.Start(ctx)
		wg.Done()
	}()
	<-time.After(2 * time.Second)
}

var syncCount int

type mockFaultInjectionManager struct {
	faultinjection.ManagerInterface
}

func (m *mockFaultInjectionManager) Sync(config *ctrlmeshproto.FaultInjection) (*ctrlmeshproto.FaultInjectConfigResp, error) {
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
