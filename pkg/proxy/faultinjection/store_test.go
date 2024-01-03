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
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/onsi/gomega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/utils/conv"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
)

func init() {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))
}

func TestFaultInjectionStore(t *testing.T) {
	fmt.Println("Test_Fault_Injection_Store")
	limitingA := &ctrlmeshv1alpha1.HTTPFaultInjection{
		Name: "deletePod",
		Delay: &ctrlmeshv1alpha1.HTTPFaultInjectionDelay{
			Percent:    "32",
			FixedDelay: "10ms",
		},
		Abort: &ctrlmeshv1alpha1.HTTPFaultInjectionAbort{
			HttpStatus: 497,
			Percent:    "50",
		},
		Match: &ctrlmeshv1alpha1.HTTPMatchRequest{
			Name: "test-deletePod",
			RelatedResources: []*ctrlmeshv1alpha1.ResourceMatch{
				{
					Namespaces: []string{"*"},
					ApiGroups:  []string{""},
					Resources:  []string{"pod"},
					Verbs:      []string{"delete"},
				},
			},
		},
		EffectiveTime: &ctrlmeshv1alpha1.EffectiveTimeRange{
			StartTime: "&v1.Time{Time: time.Date(2023, 4, 14, 8, 0, 0, 0, time.UTC)}",
			EndTime:   "&v1.Time{Time: time.Date(2023, 4, 28, 18, 0, 0, 0, time.UTC)}",
			DaysOfWeek: []int{
				int(time.Monday),    // 1
				int(time.Tuesday),   // 2
				int(time.Wednesday), // 3
				int(time.Friday),    // 5
			},
			DaysOfMonth: []int{},                // 1st and 15th of the month
			Months:      []int{1, 4, 7, 10, 12}, // January, April, July, October
		},
	}

	limitingB := &ctrlmeshv1alpha1.HTTPFaultInjection{
		Name: "createDomain",
		Delay: &ctrlmeshv1alpha1.HTTPFaultInjectionDelay{
			Percent:    "32",
			FixedDelay: "10ms",
		},
		Match: &ctrlmeshv1alpha1.HTTPMatchRequest{
			Name: "test-deletePod",
			RestRules: []*ctrlmeshv1alpha1.MultiRestRule{
				{
					URL:    []string{"https://localhost:80/createDomain"},
					Method: []string{"POST"},
				},
			},
		},
	}
	store := newFaultInjectionStore(context.TODO())
	store.createOrUpdateRule("global:deletePod",
		conv.ConvertHTTPFaultInjection(limitingA))
	store.createOrUpdateRule("global:createDomain",
		conv.ConvertHTTPFaultInjection(limitingB))

	rules, states := store.byIndex(IndexResource, "*::pod:delete")
	g := gomega.NewGomegaWithT(t)
	g.Expect(len(rules)).To(gomega.BeEquivalentTo(1))
	// g.Expect(len(limiters)).To(gomega.BeEquivalentTo(1))
	g.Expect(len(states)).To(gomega.BeEquivalentTo(0))

	rules, states = store.byIndex(IndexRest, "https://localhost:80/createDomain:POST")
	g.Expect(len(rules)).To(gomega.BeEquivalentTo(1))
	// g.Expect(len(limiters)).To(gomega.BeEquivalentTo(1))
	g.Expect(len(states)).To(gomega.BeEquivalentTo(0))
}

func TestLimiterPriority(t *testing.T) {
	fmt.Println("TestLimiterPriority")
	g := gomega.NewGomegaWithT(t)
	limitingA := &ctrlmeshv1alpha1.HTTPFaultInjection{
		Name: "deletePod-priority",
		Delay: &ctrlmeshv1alpha1.HTTPFaultInjectionDelay{
			Percent:    "100",
			FixedDelay: "2s",
		},
		Abort: &ctrlmeshv1alpha1.HTTPFaultInjectionAbort{
			HttpStatus: 497,
			Percent:    "0",
		},
		Match: &ctrlmeshv1alpha1.HTTPMatchRequest{
			Name: "test-deletePod",
			RelatedResources: []*ctrlmeshv1alpha1.ResourceMatch{
				{
					Namespaces: []string{"*"},
					ApiGroups:  []string{"*"},
					Resources:  []string{"*"},
					Verbs:      []string{"delete"},
				},
			},
		},
	}

	ctx := context.TODO()
	mgr := &manager{
		faultInjectionMap:   map[string]*ctrlmeshproto.FaultInjection{},
		faultInjectionStore: newFaultInjectionStore(ctx),
	}
	mgr.faultInjectionStore.createOrUpdateRule("global:deletePod-priority",
		conv.ConvertHTTPFaultInjection(limitingA))

	result := mgr.FaultInjectionResource("default", "", "pod", "delete")
	g.Expect(result.Abort).Should(gomega.BeTrue())

	limitingB := &ctrlmeshv1alpha1.HTTPFaultInjection{
		Name: "deletePod1-priority",
		Delay: &ctrlmeshv1alpha1.HTTPFaultInjectionDelay{
			Percent:    "100",
			FixedDelay: "2s",
		},
		Abort: &ctrlmeshv1alpha1.HTTPFaultInjectionAbort{
			HttpStatus: 497,
			Percent:    "100",
		},
		Match: &ctrlmeshv1alpha1.HTTPMatchRequest{
			Name: "test-deletePod",
			RelatedResources: []*ctrlmeshv1alpha1.ResourceMatch{
				{
					Namespaces: []string{"*"},
					ApiGroups:  []string{"*"},
					Resources:  []string{"pod"},
					Verbs:      []string{"delete"},
				},
			},
		},
	}

	mgr.faultInjectionStore.createOrUpdateRule("global:deletePod1-priority",
		conv.ConvertHTTPFaultInjection(limitingB))

	result = mgr.FaultInjectionResource("default", "", "pod", "delete")
	g.Expect(result.Abort).Should(gomega.BeTrue())
	g.Expect(result.Message).Should(gomega.ContainSubstring("deletePod1-priority"))

	limitingC := &ctrlmeshv1alpha1.HTTPFaultInjection{
		Name: "deletePod2-priority",
		Delay: &ctrlmeshv1alpha1.HTTPFaultInjectionDelay{
			Percent:    "32",
			FixedDelay: "10ms",
		},
		Abort: &ctrlmeshv1alpha1.HTTPFaultInjectionAbort{
			HttpStatus: 497,
			Percent:    "100",
		},
		Match: &ctrlmeshv1alpha1.HTTPMatchRequest{
			Name: "test-deletePod",
			RelatedResources: []*ctrlmeshv1alpha1.ResourceMatch{
				{
					Namespaces: []string{"*"},
					ApiGroups:  []string{""},
					Resources:  []string{"pod"},
					Verbs:      []string{"delete"},
				},
			},
		},
	}
	mgr.faultInjectionStore.createOrUpdateRule("global:deletePod2-priority",
		conv.ConvertHTTPFaultInjection(limitingC))
	result = mgr.FaultInjectionResource("default", "", "pod", "delete")
	g.Expect(result.Abort).Should(gomega.BeTrue())
	g.Expect(result.Message).Should(gomega.ContainSubstring("deletePod2-priority"))

	limitingD := &ctrlmeshv1alpha1.HTTPFaultInjection{
		Name: "deletePod3-priority",
		Delay: &ctrlmeshv1alpha1.HTTPFaultInjectionDelay{
			Percent:    "32",
			FixedDelay: "10ms",
		},
		Abort: &ctrlmeshv1alpha1.HTTPFaultInjectionAbort{
			HttpStatus: 497,
			Percent:    "100",
		},
		Match: &ctrlmeshv1alpha1.HTTPMatchRequest{
			Name: "test-deletePod",
			RelatedResources: []*ctrlmeshv1alpha1.ResourceMatch{
				{
					Namespaces: []string{"default"},
					ApiGroups:  []string{""},
					Resources:  []string{"pod"},
					Verbs:      []string{"delete"},
				},
			},
		},
	}
	mgr.faultInjectionStore.createOrUpdateRule("global:deletePod3-priority",
		conv.ConvertHTTPFaultInjection(limitingD))
	result = mgr.FaultInjectionResource("default", "", "pod", "delete")
	g.Expect(result.Abort).Should(gomega.BeTrue())
	g.Expect(result.Message).Should(gomega.ContainSubstring("deletePod3-priority"))
}

func TestGenerateWildcardSeeds(t *testing.T) {
	fmt.Println("TestGenerateWildcardSeeds")
	g := gomega.NewGomegaWithT(t)
	urls := generateWildcardUrls("https://www.baidu.com/", "GET")
	g.Expect(len(urls) == 1).Should(gomega.BeTrue())
	g.Expect(urls[0]).Should(gomega.BeEquivalentTo("https://www.baidu.com/:GET"))

	urls = generateWildcardUrls("https://www.cnblogs.com/9854932.html", "GET")
	g.Expect(len(urls) == 2).Should(gomega.BeTrue())
	g.Expect(urls[0]).Should(gomega.BeEquivalentTo("https://www.cnblogs.com/9854932.html:GET"))
	g.Expect(urls[1]).Should(gomega.BeEquivalentTo("https://www.cnblogs.com/*:GET"))

	urls = generateWildcardUrls("http://localhost:8080/SpringMVCLesson/helloworld/detail/123", "POST")
	g.Expect(len(urls) == 5).Should(gomega.BeTrue())
	g.Expect(urls[0]).Should(gomega.BeEquivalentTo("http://localhost:8080/SpringMVCLesson/helloworld/detail/123:POST"))
	g.Expect(urls[1]).Should(gomega.BeEquivalentTo("http://localhost:8080/SpringMVCLesson/helloworld/detail/*:POST"))
	g.Expect(urls[2]).Should(gomega.BeEquivalentTo("http://localhost:8080/SpringMVCLesson/helloworld/*:POST"))
	g.Expect(urls[3]).Should(gomega.BeEquivalentTo("http://localhost:8080/SpringMVCLesson/*:POST"))
	g.Expect(urls[4]).Should(gomega.BeEquivalentTo("http://localhost:8080/*:POST"))

	urls = generateWildcardUrls("http://localhost:8080", "POST")
	g.Expect(len(urls) == 1).Should(gomega.BeTrue())
	g.Expect(urls[0]).Should(gomega.BeEquivalentTo("http://localhost:8080:POST"))

	urls = generateWildcardUrls("http://localhost", "POST")
	g.Expect(len(urls) == 1).Should(gomega.BeTrue())
	g.Expect(urls[0]).Should(gomega.BeEquivalentTo("http://localhost:POST"))

	urls = generateWildcardUrls("https://www.cnblogs.com/f-ck-need-u/p/9854932.html", "GET")
	g.Expect(len(urls) == 4).Should(gomega.BeTrue())
	g.Expect(urls[0]).Should(gomega.BeEquivalentTo("https://www.cnblogs.com/f-ck-need-u/p/9854932.html:GET"))
	g.Expect(urls[1]).Should(gomega.BeEquivalentTo("https://www.cnblogs.com/f-ck-need-u/p/*:GET"))
	g.Expect(urls[2]).Should(gomega.BeEquivalentTo("https://www.cnblogs.com/f-ck-need-u/*:GET"))
	g.Expect(urls[3]).Should(gomega.BeEquivalentTo("https://www.cnblogs.com/*:GET"))
}

func TestRestLimiterPriority(t *testing.T) {
	fmt.Println("TestRestLimiterPriority")
	limitingA := &ctrlmeshv1alpha1.HTTPFaultInjection{
		Name: "rest-priority",
		Delay: &ctrlmeshv1alpha1.HTTPFaultInjectionDelay{
			Percent:    "32",
			FixedDelay: "10ms",
		},
		Abort: &ctrlmeshv1alpha1.HTTPFaultInjectionAbort{
			HttpStatus: 497,
			Percent:    "100",
		},
		Match: &ctrlmeshv1alpha1.HTTPMatchRequest{
			Name: "rest-priority",
			RestRules: []*ctrlmeshv1alpha1.MultiRestRule{
				{
					URL:    []string{"https://www.cnblogs.com/*"},
					Method: []string{"GET"},
				},
			},
		},
	}
	ctx := context.TODO()
	mgr := &manager{
		faultInjectionMap:   map[string]*ctrlmeshproto.FaultInjection{},
		faultInjectionStore: newFaultInjectionStore(ctx),
	}
	mgr.faultInjectionStore.createOrUpdateRule("global:rest-priority",
		conv.ConvertHTTPFaultInjection(limitingA))
	g := gomega.NewGomegaWithT(t)
	result := mgr.FaultInjectionRest("https://www.cnblogs.com/f-ck-need-u/p/9854932.html", "GET")
	g.Expect(result.Abort).Should(gomega.BeTrue())

	limitingB := &ctrlmeshv1alpha1.HTTPFaultInjection{
		Name: "rest1-priority",
		Delay: &ctrlmeshv1alpha1.HTTPFaultInjectionDelay{
			Percent:    "32",
			FixedDelay: "10ms",
		},
		Abort: &ctrlmeshv1alpha1.HTTPFaultInjectionAbort{
			HttpStatus: 497,
			Percent:    "100",
		},
		Match: &ctrlmeshv1alpha1.HTTPMatchRequest{
			Name: "rest1-priority",
			RestRules: []*ctrlmeshv1alpha1.MultiRestRule{
				{
					URL:    []string{"https://www.cnblogs.com/f-ck-need-u/*"},
					Method: []string{"GET"},
				},
			},
		},
	}
	mgr.faultInjectionStore.createOrUpdateRule("global:rest1-priority",
		conv.ConvertHTTPFaultInjection(limitingB))
	result = mgr.FaultInjectionRest("https://www.cnblogs.com/f-ck-need-u/p/9854932.html", "GET")
	g.Expect(result.Abort).Should(gomega.BeTrue())
	g.Expect(result.Message).Should(gomega.ContainSubstring("rest1-priority"))

	limitingC := &ctrlmeshv1alpha1.HTTPFaultInjection{
		Name: "rest2-priority",
		Delay: &ctrlmeshv1alpha1.HTTPFaultInjectionDelay{
			Percent:    "32",
			FixedDelay: "10ms",
		},
		Abort: &ctrlmeshv1alpha1.HTTPFaultInjectionAbort{
			HttpStatus: 497,
			Percent:    "100",
		},
		Match: &ctrlmeshv1alpha1.HTTPMatchRequest{
			Name: "rest2-priority",
			RestRules: []*ctrlmeshv1alpha1.MultiRestRule{
				{
					URL:    []string{"https://www.cnblogs.com/f-ck-need-u/p/*"},
					Method: []string{"GET"},
				},
			},
		},
	}

	mgr.faultInjectionStore.createOrUpdateRule("global:rest2-priority",
		conv.ConvertHTTPFaultInjection(limitingC))
	result = mgr.FaultInjectionRest("https://www.cnblogs.com/f-ck-need-u/p/9854932.html", "GET")
	g.Expect(result.Abort).Should(gomega.BeTrue())
	g.Expect(result.Message).Should(gomega.ContainSubstring("rest2-priority"))

	limitingD := &ctrlmeshv1alpha1.HTTPFaultInjection{
		Name: "rest3-priority",
		Delay: &ctrlmeshv1alpha1.HTTPFaultInjectionDelay{
			Percent:    "32",
			FixedDelay: "10ms",
		},
		Abort: &ctrlmeshv1alpha1.HTTPFaultInjectionAbort{
			HttpStatus: 497,
			Percent:    "100",
		},
		Match: &ctrlmeshv1alpha1.HTTPMatchRequest{
			Name: "rest3-priority",
			RestRules: []*ctrlmeshv1alpha1.MultiRestRule{
				{
					URL:    []string{"https://www.cnblogs.com/f-ck-need-u/p/9854932.html"},
					Method: []string{"GET"},
				},
			},
		},
	}
	mgr.faultInjectionStore.createOrUpdateRule("global:rest3-priority",
		conv.ConvertHTTPFaultInjection(limitingD))
	result = mgr.FaultInjectionRest("https://www.cnblogs.com/f-ck-need-u/p/9854932.html", "GET")
	g.Expect(result.Abort).Should(gomega.BeTrue())
	g.Expect(result.Message).Should(gomega.ContainSubstring("rest3-priority"))
}
