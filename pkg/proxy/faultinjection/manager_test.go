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
	"net/url"
	"os"
	"testing"

	"github.com/onsi/gomega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/utils/conv"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
)

func init() {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))
}

func TestStringMatch(t *testing.T) {
	fmt.Println("TestRestTrafficIntercept")

	fi1 := &ctrlmeshv1alpha1.FaultInjection{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName1",
			Namespace: "default",
			Annotations: map[string]string{
				"test": "test",
			},
			Labels: map[string]string{},
		},
		Spec: ctrlmeshv1alpha1.FaultInjectionSpec{

			HTTPFaultInjections: []*ctrlmeshv1alpha1.HTTPFaultInjection{
				{
					Name: "deletePod",
					Abort: &ctrlmeshv1alpha1.HTTPFaultInjectionAbort{
						HttpStatus: 409,
						Percent:    "100",
					},
					Match: &ctrlmeshv1alpha1.Match{
						Resources: []*ctrlmeshv1alpha1.ResourceMatch{
							{
								ApiGroups:  []string{"*"},
								Namespaces: []string{"*"},
								Verbs:      []string{"delete"},
								Resources:  []string{"pod"},
							},
						},
						HttpMatch: []*ctrlmeshv1alpha1.HttpMatch{
							{
								Host: &ctrlmeshv1alpha1.MatchContent{
									Exact: "test1.com",
								},
								Method: "POST",
							},
							{
								Host: &ctrlmeshv1alpha1.MatchContent{
									Exact: "test2.com",
								},
								Path: &ctrlmeshv1alpha1.MatchContent{
									Exact: "/a/b",
								},
							},
							{
								Host: &ctrlmeshv1alpha1.MatchContent{
									Regex: "(test3[a-z]+)",
								},
							},
							{
								Host: &ctrlmeshv1alpha1.MatchContent{
									Exact: "test4.com",
								},
								Path: &ctrlmeshv1alpha1.MatchContent{
									Regex: "/abc/",
								},
							},
						},
					},
				},
			},
		},
	}
	g := gomega.NewGomegaWithT(t)
	mgr := NewManager(context.TODO())
	protoFault := conv.ConvertFaultInjection(fi1)
	protoFault.Option = ctrlmeshproto.FaultInjection_UPDATE
	_, err := mgr.Sync(protoFault)
	g.Expect(err).Should(gomega.BeNil())

	testUrl, _ := url.Parse("https://test1.com")
	injector := mgr.GetInjectorByUrl(testUrl, "POST")
	g.Expect(injector.(*abortWithDelayInjector).Abort).Should(gomega.BeTrue())

	testUrl, _ = url.Parse("https://test1.com")
	injector = mgr.GetInjectorByUrl(testUrl, "GET")
	g.Expect(injector.(*abortWithDelayInjector).Abort).Should(gomega.BeFalse())

	testUrl, _ = url.Parse("https://test1.com/a")
	injector = mgr.GetInjectorByUrl(testUrl, "POST")
	g.Expect(injector.(*abortWithDelayInjector).Abort).Should(gomega.BeTrue())

	testUrl, _ = url.Parse("https://test2.com/a/b")
	injector = mgr.GetInjectorByUrl(testUrl, "POST")
	g.Expect(injector.(*abortWithDelayInjector).Abort).Should(gomega.BeTrue())

	testUrl, _ = url.Parse("https://test2.com")
	injector = mgr.GetInjectorByUrl(testUrl, "POST")
	g.Expect(injector.(*abortWithDelayInjector).Abort).Should(gomega.BeFalse())

	testUrl, _ = url.Parse("https://test2.com/a/c")
	injector = mgr.GetInjectorByUrl(testUrl, "POST")
	g.Expect(injector.(*abortWithDelayInjector).Abort).Should(gomega.BeFalse())

	testUrl, _ = url.Parse("https://test3x.com/a/c")
	injector = mgr.GetInjectorByUrl(testUrl, "POST")
	g.Expect(injector.(*abortWithDelayInjector).Abort).Should(gomega.BeTrue())

	testUrl, _ = url.Parse("https://test3.com")
	injector = mgr.GetInjectorByUrl(testUrl, "POST")
	g.Expect(injector.(*abortWithDelayInjector).Abort).Should(gomega.BeFalse())

	testUrl, _ = url.Parse("https://test4.com/abc/d")
	injector = mgr.GetInjectorByUrl(testUrl, "POST")
	g.Expect(injector.(*abortWithDelayInjector).Abort).Should(gomega.BeTrue())

	protoFault.Option = ctrlmeshproto.FaultInjection_DELETE
	_, err = mgr.Sync(protoFault)

	testUrl, _ = url.Parse("https://test1.com/a")
	injector = mgr.GetInjectorByUrl(testUrl, "POST")
	g.Expect(injector.(*abortWithDelayInjector).Abort).Should(gomega.BeFalse())

	testUrl, _ = url.Parse("https://test2.com/a/b")
	injector = mgr.GetInjectorByUrl(testUrl, "POST")
	g.Expect(injector.(*abortWithDelayInjector).Abort).Should(gomega.BeFalse())

	testUrl, _ = url.Parse("https://test3x.com/a/c")
	injector = mgr.GetInjectorByUrl(testUrl, "POST")
	g.Expect(injector.(*abortWithDelayInjector).Abort).Should(gomega.BeFalse())

	testUrl, _ = url.Parse("https://test4.com/abc/d")
	injector = mgr.GetInjectorByUrl(testUrl, "POST")
	g.Expect(injector.(*abortWithDelayInjector).Abort).Should(gomega.BeFalse())
}

func TestIsEffectiveTimeRange(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	var tests = []struct {
		timeRange *ctrlmeshproto.EffectiveTimeRange
		want      bool
	}{
		{&ctrlmeshproto.EffectiveTimeRange{
			StartTime:   "",
			EndTime:     "",
			DaysOfWeek:  []int32{},
			DaysOfMonth: []int32{},
			Months:      []int32{},
		}, true},
		{&ctrlmeshproto.EffectiveTimeRange{
			StartTime:   "00:00:00",
			EndTime:     "23:59:59",
			DaysOfWeek:  []int32{1, 2, 3, 4, 5, 7},
			DaysOfMonth: []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31},
			Months:      []int32{12},
		}, false},
	}
	for _, tt := range tests {
		got := isEffectiveTimeRange(tt.timeRange)
		g.Expect(got).Should(gomega.BeEquivalentTo(tt.want))
	}
}
