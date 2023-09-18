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
	"fmt"
	"os"
	"testing"

	"github.com/onsi/gomega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	appsv1alpha1 "github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh/v1alpha1"
)

func init() {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))
}

func TestLimiterStore(t *testing.T) {
	fmt.Println("Test_Limiter_Store")
	limitingA := appsv1alpha1.Limiting{
		Name: "deletePod",
		Bucket: appsv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		ResourceRules: []appsv1alpha1.ResourceRule{
			{
				Namespaces: []string{"*"},
				ApiGroups:  []string{""},
				Resources:  []string{"pod"},
				Verbs:      []string{"delete"},
			},
		},
	}
	snapshotA := appsv1alpha1.LimitingSnapshot{
		Name:   "deletePod",
		Status: appsv1alpha1.BreakerStatusClosed,
	}
	limitingB := appsv1alpha1.Limiting{
		Name: "createDomain",
		Bucket: appsv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		RestRules: []appsv1alpha1.RestRule{
			{
				URL:    "https://localhost:80/createDomain",
				Method: "POST",
			},
		},
	}
	snapshotB := appsv1alpha1.LimitingSnapshot{
		Name:   "createDomain",
		Status: appsv1alpha1.BreakerStatusOpened,
	}
	store := newLimiterStore()
	store.createOrUpdateRule("default:global:deletePod", &limitingA, &snapshotA)
	store.createOrUpdateRule("default:global:createDomain", &limitingB, &snapshotB)

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

func TestRegisterRules(t *testing.T) {
	fmt.Println("TestRegisterRules")
	var x float64
	x = 2.1
	y := uint64(x)
	fmt.Println(y)
}

func Test_Limiter_Priority(t *testing.T) {
	fmt.Println("Test_Limiter_Priority")
	limitingA := appsv1alpha1.Limiting{
		Name: "deletePod-priority",
		Bucket: appsv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		ResourceRules: []appsv1alpha1.ResourceRule{
			{
				Namespaces: []string{"*"},
				ApiGroups:  []string{"*"},
				Resources:  []string{"*"},
				Verbs:      []string{"delete"},
			},
		},
	}
	snapshotA := appsv1alpha1.LimitingSnapshot{
		Name:   "deletePod-priority",
		Status: appsv1alpha1.BreakerStatusClosed,
	}

	defaultLimiterStore.createOrUpdateRule("default:global:deletePod-priority", &limitingA, &snapshotA)

	g := gomega.NewGomegaWithT(t)
	result := ValidateResource("default", "", "pod", "delete")
	g.Expect(result.Allowed).Should(gomega.BeTrue())

	limitingB := appsv1alpha1.Limiting{
		Name: "deletePod1-priority",
		Bucket: appsv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		ResourceRules: []appsv1alpha1.ResourceRule{
			{
				Namespaces: []string{"*"},
				ApiGroups:  []string{"*"},
				Resources:  []string{"pod"},
				Verbs:      []string{"delete"},
			},
		},
	}
	snapshotB := appsv1alpha1.LimitingSnapshot{
		Name:   "deletePod1-priority",
		Status: appsv1alpha1.BreakerStatusOpened,
	}
	defaultLimiterStore.createOrUpdateRule("default:global:deletePod1-priority", &limitingB, &snapshotB)
	result = ValidateResource("default", "", "pod", "delete")
	g.Expect(result.Allowed).Should(gomega.BeFalse())
	g.Expect(result.Message).Should(gomega.ContainSubstring("deletePod1-priority"))

	limitingC := appsv1alpha1.Limiting{
		Name: "deletePod2-priority",
		Bucket: appsv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		ResourceRules: []appsv1alpha1.ResourceRule{
			{
				Namespaces: []string{"*"},
				ApiGroups:  []string{""},
				Resources:  []string{"pod"},
				Verbs:      []string{"delete"},
			},
		},
	}
	snapshotC := appsv1alpha1.LimitingSnapshot{
		Name:   "deletePod2-priority",
		Status: appsv1alpha1.BreakerStatusOpened,
	}
	defaultLimiterStore.createOrUpdateRule("default:global:deletePod2-priority", &limitingC, &snapshotC)
	result = ValidateResource("default", "", "pod", "delete")
	g.Expect(result.Allowed).Should(gomega.BeFalse())
	g.Expect(result.Message).Should(gomega.ContainSubstring("deletePod2-priority"))

	limitingD := appsv1alpha1.Limiting{
		Name: "deletePod3-priority",
		Bucket: appsv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		ResourceRules: []appsv1alpha1.ResourceRule{
			{
				Namespaces: []string{"default"},
				ApiGroups:  []string{""},
				Resources:  []string{"pod"},
				Verbs:      []string{"delete"},
			},
		},
	}
	snapshotD := appsv1alpha1.LimitingSnapshot{
		Name:   "deletePod3-priority",
		Status: appsv1alpha1.BreakerStatusOpened,
	}
	defaultLimiterStore.createOrUpdateRule("default:global:deletePod3-priority", &limitingD, &snapshotD)
	result = ValidateResource("default", "", "pod", "delete")
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

func Test_Rest_Limiter_Priority(t *testing.T) {
	fmt.Println("Test_Rest_Limiter_Priority")
	limitingA := appsv1alpha1.Limiting{
		Name: "rest-priority",
		Bucket: appsv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		RestRules: []appsv1alpha1.RestRule{
			{
				URL:    "https://www.cnblogs.com/*",
				Method: "GET",
			},
		},
	}
	snapshotA := appsv1alpha1.LimitingSnapshot{
		Name:   "rest-priority",
		Status: appsv1alpha1.BreakerStatusClosed,
	}
	defaultLimiterStore.createOrUpdateRule("default:global:rest-priority", &limitingA, &snapshotA)
	g := gomega.NewGomegaWithT(t)
	result := ValidateRest("https://www.cnblogs.com/f-ck-need-u/p/9854932.html", "GET")
	g.Expect(result.Allowed).Should(gomega.BeTrue())

	limitingB := appsv1alpha1.Limiting{
		Name: "rest1-priority",
		Bucket: appsv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		RestRules: []appsv1alpha1.RestRule{
			{
				URL:    "https://www.cnblogs.com/f-ck-need-u/*",
				Method: "GET",
			},
		},
	}
	snapshotB := appsv1alpha1.LimitingSnapshot{
		Name:   "rest1-priority",
		Status: appsv1alpha1.BreakerStatusOpened,
	}
	defaultLimiterStore.createOrUpdateRule("default:global:rest1-priority", &limitingB, &snapshotB)
	result = ValidateRest("https://www.cnblogs.com/f-ck-need-u/p/9854932.html", "GET")
	g.Expect(result.Allowed).Should(gomega.BeFalse())
	g.Expect(result.Message).Should(gomega.ContainSubstring("rest1-priority"))

	limitingC := appsv1alpha1.Limiting{
		Name: "rest2-priority",
		Bucket: appsv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		RestRules: []appsv1alpha1.RestRule{
			{
				URL:    "https://www.cnblogs.com/f-ck-need-u/p/*",
				Method: "GET",
			},
		},
	}
	snapshotC := appsv1alpha1.LimitingSnapshot{
		Name:   "rest2-priority",
		Status: appsv1alpha1.BreakerStatusOpened,
	}
	defaultLimiterStore.createOrUpdateRule("default:global:rest2-priority", &limitingC, &snapshotC)
	result = ValidateRest("https://www.cnblogs.com/f-ck-need-u/p/9854932.html", "GET")
	g.Expect(result.Allowed).Should(gomega.BeFalse())
	g.Expect(result.Message).Should(gomega.ContainSubstring("rest2-priority"))

	limitingD := appsv1alpha1.Limiting{
		Name: "rest3-priority",
		Bucket: appsv1alpha1.Bucket{
			Interval: "1m",
			Burst:    100,
			Limit:    20,
		},
		RestRules: []appsv1alpha1.RestRule{
			{
				URL:    "https://www.cnblogs.com/f-ck-need-u/p/9854932.html",
				Method: "GET",
			},
		},
	}
	snapshotD := appsv1alpha1.LimitingSnapshot{
		Name:   "rest3-priority",
		Status: appsv1alpha1.BreakerStatusOpened,
	}
	defaultLimiterStore.createOrUpdateRule("default:global:rest3-priority", &limitingD, &snapshotD)
	result = ValidateRest("https://www.cnblogs.com/f-ck-need-u/p/9854932.html", "GET")
	g.Expect(result.Allowed).Should(gomega.BeFalse())
	g.Expect(result.Message).Should(gomega.ContainSubstring("rest3-priority"))
}
