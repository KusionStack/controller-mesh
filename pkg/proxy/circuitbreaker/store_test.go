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
	"context"
	"fmt"
	"os"
	"testing"

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

func TestLimiterStore(t *testing.T) {
	fmt.Println("Test_Limiter_Store")
	limitingA := &ctrlmeshv1alpha1.Limiting{
		Name: "deletePod",
		Bucket: ctrlmeshv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		ResourceRules: []ctrlmeshv1alpha1.ResourceRule{
			{
				Namespaces: []string{"*"},
				ApiGroups:  []string{""},
				Resources:  []string{"pod"},
				Verbs:      []string{"delete"},
			},
		},
	}

	limitingB := &ctrlmeshv1alpha1.Limiting{
		Name: "createDomain",
		Bucket: ctrlmeshv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		RestRules: []ctrlmeshv1alpha1.RestRule{
			{
				URL:    "https://localhost:80/createDomain",
				Method: "POST",
			},
		},
	}
	store := newLimiterStore(context.TODO())
	store.createOrUpdateRule("global:deletePod", conv.ConvertLimiting(limitingA), &ctrlmeshproto.LimitingSnapshot{State: ctrlmeshproto.BreakerState_CLOSED})
	store.createOrUpdateRule("global:createDomain", conv.ConvertLimiting(limitingB), &ctrlmeshproto.LimitingSnapshot{State: ctrlmeshproto.BreakerState_OPENED})

	rules, limiters, states := store.byIndex(IndexResource, "*::pod:delete")
	g := gomega.NewGomegaWithT(t)
	g.Expect(len(rules)).To(gomega.BeEquivalentTo(1))
	g.Expect(len(limiters)).To(gomega.BeEquivalentTo(1))
	g.Expect(len(states)).To(gomega.BeEquivalentTo(1))

	rules, limiters, states = store.byIndex(IndexRest, "https://localhost:80/createDomain:POST")
	g.Expect(len(rules)).To(gomega.BeEquivalentTo(1))
	g.Expect(len(limiters)).To(gomega.BeEquivalentTo(1))
	g.Expect(len(states)).To(gomega.BeEquivalentTo(1))
}

func TestLimiterPriority(t *testing.T) {
	fmt.Println("TestLimiterPriority")
	g := gomega.NewGomegaWithT(t)
	limitingA := &ctrlmeshv1alpha1.Limiting{
		Name: "deletePod-priority",
		Bucket: ctrlmeshv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		ResourceRules: []ctrlmeshv1alpha1.ResourceRule{
			{
				Namespaces: []string{"*"},
				ApiGroups:  []string{"*"},
				Resources:  []string{"*"},
				Verbs:      []string{"delete"},
			},
		},
	}
	ctx := context.TODO()
	mgr := &manager{
		breakerMap:            map[string]*ctrlmeshproto.CircuitBreaker{},
		limiterStore:          newLimiterStore(ctx),
		trafficInterceptStore: newTrafficInterceptStore(),
	}
	mgr.limiterStore.createOrUpdateRule("global:deletePod-priority", conv.ConvertLimiting(limitingA), &ctrlmeshproto.LimitingSnapshot{State: ctrlmeshproto.BreakerState_CLOSED})

	result := mgr.ValidateResource("default", "", "pod", "delete")
	g.Expect(result.Allowed).Should(gomega.BeTrue())

	limitingB := &ctrlmeshv1alpha1.Limiting{
		Name: "deletePod1-priority",
		Bucket: ctrlmeshv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		ResourceRules: []ctrlmeshv1alpha1.ResourceRule{
			{
				Namespaces: []string{"*"},
				ApiGroups:  []string{"*"},
				Resources:  []string{"pod"},
				Verbs:      []string{"delete"},
			},
		},
	}

	mgr.limiterStore.createOrUpdateRule("global:deletePod1-priority", conv.ConvertLimiting(limitingB), &ctrlmeshproto.LimitingSnapshot{State: ctrlmeshproto.BreakerState_OPENED})

	result = mgr.ValidateResource("default", "", "pod", "delete")
	g.Expect(result.Allowed).Should(gomega.BeFalse())
	g.Expect(result.Message).Should(gomega.ContainSubstring("deletePod1-priority"))

	limitingC := &ctrlmeshv1alpha1.Limiting{
		Name: "deletePod2-priority",
		Bucket: ctrlmeshv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		ResourceRules: []ctrlmeshv1alpha1.ResourceRule{
			{
				Namespaces: []string{"*"},
				ApiGroups:  []string{""},
				Resources:  []string{"pod"},
				Verbs:      []string{"delete"},
			},
		},
	}
	mgr.limiterStore.createOrUpdateRule("global:deletePod2-priority", conv.ConvertLimiting(limitingC), &ctrlmeshproto.LimitingSnapshot{State: ctrlmeshproto.BreakerState_OPENED})
	result = mgr.ValidateResource("default", "", "pod", "delete")
	g.Expect(result.Allowed).Should(gomega.BeFalse())
	g.Expect(result.Message).Should(gomega.ContainSubstring("deletePod2-priority"))

	limitingD := &ctrlmeshv1alpha1.Limiting{
		Name: "deletePod3-priority",
		Bucket: ctrlmeshv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		ResourceRules: []ctrlmeshv1alpha1.ResourceRule{
			{
				Namespaces: []string{"default"},
				ApiGroups:  []string{""},
				Resources:  []string{"pod"},
				Verbs:      []string{"delete"},
			},
		},
	}
	mgr.limiterStore.createOrUpdateRule("global:deletePod3-priority", conv.ConvertLimiting(limitingD), &ctrlmeshproto.LimitingSnapshot{State: ctrlmeshproto.BreakerState_OPENED})
	result = mgr.ValidateResource("default", "", "pod", "delete")
	g.Expect(result.Allowed).Should(gomega.BeFalse())
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
	limitingA := &ctrlmeshv1alpha1.Limiting{
		Name: "rest-priority",
		Bucket: ctrlmeshv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		RestRules: []ctrlmeshv1alpha1.RestRule{
			{
				URL:    "https://www.cnblogs.com/*",
				Method: "GET",
			},
		},
	}
	ctx := context.TODO()
	mgr := &manager{
		breakerMap:            map[string]*ctrlmeshproto.CircuitBreaker{},
		limiterStore:          newLimiterStore(ctx),
		trafficInterceptStore: newTrafficInterceptStore(),
	}
	mgr.limiterStore.createOrUpdateRule("global:rest-priority", conv.ConvertLimiting(limitingA), &ctrlmeshproto.LimitingSnapshot{State: ctrlmeshproto.BreakerState_CLOSED})
	g := gomega.NewGomegaWithT(t)
	result := mgr.ValidateRest("https://www.cnblogs.com/f-ck-need-u/p/9854932.html", "GET")
	g.Expect(result.Allowed).Should(gomega.BeTrue())

	limitingB := &ctrlmeshv1alpha1.Limiting{
		Name: "rest1-priority",
		Bucket: ctrlmeshv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		RestRules: []ctrlmeshv1alpha1.RestRule{
			{
				URL:    "https://www.cnblogs.com/f-ck-need-u/*",
				Method: "GET",
			},
		},
	}
	mgr.limiterStore.createOrUpdateRule("global:rest1-priority", conv.ConvertLimiting(limitingB), &ctrlmeshproto.LimitingSnapshot{State: ctrlmeshproto.BreakerState_OPENED})
	result = mgr.ValidateRest("https://www.cnblogs.com/f-ck-need-u/p/9854932.html", "GET")
	g.Expect(result.Allowed).Should(gomega.BeFalse())
	g.Expect(result.Message).Should(gomega.ContainSubstring("rest1-priority"))

	limitingC := &ctrlmeshv1alpha1.Limiting{
		Name: "rest2-priority",
		Bucket: ctrlmeshv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		RestRules: []ctrlmeshv1alpha1.RestRule{
			{
				URL:    "https://www.cnblogs.com/f-ck-need-u/p/*",
				Method: "GET",
			},
		},
	}
	mgr.limiterStore.createOrUpdateRule("global:rest2-priority", conv.ConvertLimiting(limitingC), &ctrlmeshproto.LimitingSnapshot{State: ctrlmeshproto.BreakerState_OPENED})
	result = mgr.ValidateRest("https://www.cnblogs.com/f-ck-need-u/p/9854932.html", "GET")
	g.Expect(result.Allowed).Should(gomega.BeFalse())
	g.Expect(result.Message).Should(gomega.ContainSubstring("rest2-priority"))

	limitingD := &ctrlmeshv1alpha1.Limiting{
		Name: "rest3-priority",
		Bucket: ctrlmeshv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		RestRules: []ctrlmeshv1alpha1.RestRule{
			{
				URL:    "https://www.cnblogs.com/f-ck-need-u/p/9854932.html",
				Method: "GET",
			},
		},
	}
	mgr.limiterStore.createOrUpdateRule("global:rest3-priority", conv.ConvertLimiting(limitingD), &ctrlmeshproto.LimitingSnapshot{State: ctrlmeshproto.BreakerState_OPENED})
	result = mgr.ValidateRest("https://www.cnblogs.com/f-ck-need-u/p/9854932.html", "GET")
	g.Expect(result.Allowed).Should(gomega.BeFalse())
	g.Expect(result.Message).Should(gomega.ContainSubstring("rest3-priority"))
}
