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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/utils/conv"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
)

func init() {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))
}

func TestRestTrafficIntercept(t *testing.T) {
	fmt.Println("TestRestTrafficIntercept")

	cb1 := &ctrlmeshv1alpha1.CircuitBreaker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName1",
			Namespace: "default",
			Annotations: map[string]string{
				"test": "test",
			},
			Labels: map[string]string{},
		},
		Spec: ctrlmeshv1alpha1.CircuitBreakerSpec{
			TrafficInterceptRules: []*ctrlmeshv1alpha1.TrafficInterceptRule{
				{
					Name:          "rule1",
					InterceptType: ctrlmeshv1alpha1.InterceptTypeWhitelist,
					ContentType:   ctrlmeshv1alpha1.ContentTypeNormal,
					Contents:      []string{"www.hello.com", "www.testa.com"},
					Methods:       []string{"POST", "GET"},
				},
			},
		},
	}
	g := gomega.NewGomegaWithT(t)
	mgr := NewManager(context.TODO())
	protoBreaker := conv.ConvertCircuitBreaker(cb1)
	protoBreaker.Option = ctrlmeshproto.CircuitBreaker_UPDATE
	_, err := mgr.Sync(protoBreaker)
	g.Expect(err).Should(gomega.BeNil())
	result := mgr.ValidateTrafficIntercept("www.hello.com", "POST")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = mgr.ValidateTrafficIntercept("www.hello.com", "GET")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = mgr.ValidateTrafficIntercept("www.hello.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())
	result = mgr.ValidateTrafficIntercept("www.testa.com", "GET")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = mgr.ValidateTrafficIntercept("www.testa.com", "POST")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = mgr.ValidateTrafficIntercept("www.testa.com", "PUT")
	g.Expect(result.Allowed).To(gomega.BeFalse())

	cb2 := &ctrlmeshv1alpha1.CircuitBreaker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName2",
			Namespace: "default",
			Annotations: map[string]string{
				"test": "test",
			},
			Labels: map[string]string{},
		},
		Spec: ctrlmeshv1alpha1.CircuitBreakerSpec{
			TrafficInterceptRules: []*ctrlmeshv1alpha1.TrafficInterceptRule{
				{
					Name:          "rule2",
					InterceptType: ctrlmeshv1alpha1.InterceptTypeWhitelist,
					ContentType:   ctrlmeshv1alpha1.ContentTypeNormal,
					Contents:      []string{"www.hello.com", "www.testa.com"},
					Methods:       []string{"*", "GET"},
				},
			},
		},
	}

	protoBreaker2 := conv.ConvertCircuitBreaker(cb2)
	_, err = mgr.Sync(protoBreaker2)
	g.Expect(err).Should(gomega.BeNil())

	result = mgr.ValidateTrafficIntercept("www.hello.com", "GET")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = mgr.ValidateTrafficIntercept("www.hello.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = mgr.ValidateTrafficIntercept("www.testa.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = mgr.ValidateTrafficIntercept("www.testa.com1", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())

	cb3 := &ctrlmeshv1alpha1.CircuitBreaker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName3",
			Namespace: "default",
			Annotations: map[string]string{
				"test": "test",
			},
			Labels: map[string]string{},
		},
		Spec: ctrlmeshv1alpha1.CircuitBreakerSpec{
			TrafficInterceptRules: []*ctrlmeshv1alpha1.TrafficInterceptRule{
				{
					Name:          "rule3",
					InterceptType: ctrlmeshv1alpha1.InterceptTypeBlacklist,
					ContentType:   ctrlmeshv1alpha1.ContentTypeNormal,
					Contents:      []string{"www.hello.com", "www.testa.com"},
					Methods:       []string{"DELETE"},
				},
			},
		},
	}

	protoBreaker3 := conv.ConvertCircuitBreaker(cb3)
	_, err = mgr.Sync(protoBreaker3)
	g.Expect(err).Should(gomega.BeNil())
	result = mgr.ValidateTrafficIntercept("www.hello.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())
	result = mgr.ValidateTrafficIntercept("www.testa.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())

	cb4 := &ctrlmeshv1alpha1.CircuitBreaker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName4",
			Namespace: "default",
			Annotations: map[string]string{
				"test": "test",
			},
			Labels: map[string]string{},
		},
		Spec: ctrlmeshv1alpha1.CircuitBreakerSpec{
			TrafficInterceptRules: []*ctrlmeshv1alpha1.TrafficInterceptRule{
				{
					Name:          "rule4",
					InterceptType: ctrlmeshv1alpha1.InterceptTypeBlacklist,
					ContentType:   ctrlmeshv1alpha1.ContentTypeRegexp,
					Contents:      []string{"(ali[a-z]+)"},
					Methods:       []string{"DELETE"},
				},
			},
		},
	}
	protoBreaker4 := conv.ConvertCircuitBreaker(cb4)
	_, err = mgr.Sync(protoBreaker4)
	g.Expect(err).Should(gomega.BeNil())

	result = mgr.ValidateTrafficIntercept("www.testb.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())
	result = mgr.ValidateTrafficIntercept("www.testb.com", "PUT")
	g.Expect(result.Allowed).To(gomega.BeFalse())
	g.Expect(result.Reason).To(gomega.BeEquivalentTo("No rule match"))

	cb5 := &ctrlmeshv1alpha1.CircuitBreaker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName5",
			Namespace: "default",
			Annotations: map[string]string{
				"test": "test",
			},
			Labels: map[string]string{},
		},
		Spec: ctrlmeshv1alpha1.CircuitBreakerSpec{
			TrafficInterceptRules: []*ctrlmeshv1alpha1.TrafficInterceptRule{
				{
					Name:          "rule5",
					InterceptType: ctrlmeshv1alpha1.InterceptTypeWhitelist,
					ContentType:   ctrlmeshv1alpha1.ContentTypeRegexp,
					Contents:      []string{"(bai[a-z]+)"},
					Methods:       []string{"DELETE"},
				},
			},
		},
	}
	protoBreaker5 := conv.ConvertCircuitBreaker(cb5)
	_, err = mgr.Sync(protoBreaker5)
	g.Expect(err).Should(gomega.BeNil())

	result = mgr.ValidateTrafficIntercept("www.hello.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())
	g.Expect(result.Message).To(gomega.ContainSubstring("rule3"))
	result = mgr.ValidateTrafficIntercept("www.hellooo.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())
}
