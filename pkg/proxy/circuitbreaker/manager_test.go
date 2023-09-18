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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	appsv1alpha1 "github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh/v1alpha1"
)

func init() {
	EnableCircuitBreaker = true
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))
}

func TestRestTrafficIntercept(t *testing.T) {
	fmt.Println("Test_Rest_Traffic_Intercept")

	cb1 := &appsv1alpha1.CircuitBreaker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName1",
			Namespace: "default",
			Annotations: map[string]string{
				"test": "test",
			},
			Labels: map[string]string{},
		},
		Spec: appsv1alpha1.CircuitBreakerSpec{
			TrafficInterceptRules: []appsv1alpha1.TrafficInterceptRule{
				{
					Name:          "rule1",
					InterceptType: appsv1alpha1.InterceptTypeWhite,
					ContentType:   appsv1alpha1.ContentTypeNormal,
					Contents:      []string{"www.hello.com", "www.testa.com"},
					Methods:       []string{"POST", "GET"},
				},
			},
		},
	}
	g := gomega.NewGomegaWithT(t)

	RegisterRules(cb1)
	result := ValidateTrafficIntercept("www.hello.com", "POST")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = ValidateTrafficIntercept("www.hello.com", "GET")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = ValidateTrafficIntercept("www.hello.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())
	result = ValidateTrafficIntercept("www.testa.com", "GET")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = ValidateTrafficIntercept("www.testa.com", "POST")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = ValidateTrafficIntercept("www.testa.com", "PUT")
	g.Expect(result.Allowed).To(gomega.BeFalse())

	cb2 := &appsv1alpha1.CircuitBreaker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName2",
			Namespace: "default",
			Annotations: map[string]string{
				"test": "test",
			},
			Labels: map[string]string{},
		},
		Spec: appsv1alpha1.CircuitBreakerSpec{
			TrafficInterceptRules: []appsv1alpha1.TrafficInterceptRule{
				{
					Name:          "rule2",
					InterceptType: appsv1alpha1.InterceptTypeWhite,
					ContentType:   appsv1alpha1.ContentTypeNormal,
					Contents:      []string{"www.hello.com", "www.testa.com"},
					Methods:       []string{"*", "GET"},
				},
			},
		},
	}
	RegisterRules(cb2)

	result = ValidateTrafficIntercept("www.hello.com", "GET")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = ValidateTrafficIntercept("www.hello.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = ValidateTrafficIntercept("www.testa.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeTrue())
	result = ValidateTrafficIntercept("www.testa.com1", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())

	cb3 := &appsv1alpha1.CircuitBreaker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName3",
			Namespace: "default",
			Annotations: map[string]string{
				"test": "test",
			},
			Labels: map[string]string{},
		},
		Spec: appsv1alpha1.CircuitBreakerSpec{
			TrafficInterceptRules: []appsv1alpha1.TrafficInterceptRule{
				{
					Name:          "rule3",
					InterceptType: appsv1alpha1.InterceptTypeBlack,
					ContentType:   appsv1alpha1.ContentTypeNormal,
					Contents:      []string{"www.hello.com", "www.testa.com"},
					Methods:       []string{"DELETE"},
				},
			},
		},
	}
	RegisterRules(cb3)
	result = ValidateTrafficIntercept("www.hello.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())
	result = ValidateTrafficIntercept("www.testa.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())

	cb4 := &appsv1alpha1.CircuitBreaker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName4",
			Namespace: "default",
			Annotations: map[string]string{
				"test": "test",
			},
			Labels: map[string]string{},
		},
		Spec: appsv1alpha1.CircuitBreakerSpec{
			TrafficInterceptRules: []appsv1alpha1.TrafficInterceptRule{
				{
					Name:          "rule4",
					InterceptType: appsv1alpha1.InterceptTypeBlack,
					ContentType:   appsv1alpha1.ContentTypeRegexp,
					Contents:      []string{"(ali[a-z]+)"},
					Methods:       []string{"DELETE"},
				},
			},
		},
	}
	RegisterRules(cb4)
	result = ValidateTrafficIntercept("www.testb.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())
	result = ValidateTrafficIntercept("www.testb.com", "PUT")
	g.Expect(result.Allowed).To(gomega.BeFalse())
	g.Expect(result.Reason).To(gomega.BeEquivalentTo("No rule match"))
	//g.Expect(result.Message).To(gomega.BeEquivalentTo("Default strategy is unknown, should deny"))

	cb5 := &appsv1alpha1.CircuitBreaker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName5",
			Namespace: "default",
			Annotations: map[string]string{
				"test": "test",
			},
			Labels: map[string]string{},
		},
		Spec: appsv1alpha1.CircuitBreakerSpec{
			TrafficInterceptRules: []appsv1alpha1.TrafficInterceptRule{
				{
					Name:          "rule5",
					InterceptType: appsv1alpha1.InterceptTypeWhite,
					ContentType:   appsv1alpha1.ContentTypeRegexp,
					Contents:      []string{"(bai[a-z]+)"},
					Methods:       []string{"DELETE"},
				},
			},
		},
	}
	RegisterRules(cb5)

	result = ValidateTrafficIntercept("www.hello.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())
	g.Expect(result.Message).To(gomega.ContainSubstring("rule3"))
	result = ValidateTrafficIntercept("www.hellooo.com", "DELETE")
	g.Expect(result.Allowed).To(gomega.BeFalse())
}
