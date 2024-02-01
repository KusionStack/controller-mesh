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
package http

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/onsi/gomega"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	meshhttp "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/http"
	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	"github.com/KusionStack/controller-mesh/pkg/proxy/circuitbreaker"
	"github.com/KusionStack/controller-mesh/pkg/proxy/faultinjection"
)

func TestTProxy(t *testing.T) {
	enableRestFaultInjection = true
	g := gomega.NewGomegaWithT(t)
	go StartProxy()
	time.Sleep(time.Second * 2)
	proxyUrl := "http://127.0.0.1:15002"
	req, err := http.NewRequest(http.MethodGet, proxyUrl, nil)
	if err != nil {
		t.Error(err)
	}

	req.Header.Set(meshhttp.HeaderMeshRealEndpoint, "https://github.com")
	resp, err := http.DefaultClient.Do(req)
	g.Expect(resp.StatusCode).Should(gomega.Equal(http.StatusForbidden))
	req.Header.Set(meshhttp.HeaderMeshRealEndpoint, "https://www.gayhub.com/foo")
	resp, err = http.DefaultClient.Do(req)
	g.Expect(resp.StatusCode).Should(gomega.Equal(http.StatusBadGateway))
	req.Header.Set(meshhttp.HeaderMeshRealEndpoint, "https://abc.github.com")
	resp, err = http.DefaultClient.Do(req)
	g.Expect(resp.StatusCode).Should(gomega.Equal(http.StatusForbidden))
}

func StartProxy() {
	faultInjectionMgr := faultinjection.NewManager(context.TODO())
	circuitInjectionMgr := circuitbreaker.NewManager(context.TODO())
	_, err := faultInjectionMgr.Sync(&ctrlmeshproto.FaultInjection{
		Option:     ctrlmeshproto.FaultInjection_UPDATE,
		ConfigHash: "123",
		HttpFaultInjections: []*ctrlmeshproto.HTTPFaultInjection{
			{
				Name: "test-1",
				Match: &ctrlmeshproto.Match{
					HttpMatch: []*ctrlmeshproto.HttpMatch{
						{
							Host: &ctrlmeshproto.MatchContent{
								Exact: "github.com",
							},
							Method: "GET",
						},
					},
				},
				Abort: &ctrlmeshproto.HTTPFaultInjection_Abort{
					Percent:   100,
					ErrorType: &ctrlmeshproto.HTTPFaultInjection_Abort_HttpStatus{HttpStatus: http.StatusForbidden},
				},
			},
			{
				Name: "test-2",
				Match: &ctrlmeshproto.Match{
					HttpMatch: []*ctrlmeshproto.HttpMatch{
						{
							Host: &ctrlmeshproto.MatchContent{
								Exact: "www.gayhub.com",
							},
							Path: &ctrlmeshproto.MatchContent{
								Exact: "/foo",
							},
							Method: "GET",
						},
					},
				},
				Abort: &ctrlmeshproto.HTTPFaultInjection_Abort{
					Percent:   100,
					ErrorType: &ctrlmeshproto.HTTPFaultInjection_Abort_HttpStatus{HttpStatus: http.StatusBadGateway},
				},
			},
			{
				Name: "test-3",
				Match: &ctrlmeshproto.Match{
					HttpMatch: []*ctrlmeshproto.HttpMatch{
						{
							Host: &ctrlmeshproto.MatchContent{
								Exact: "abc.github.com",
							},
							Method: "GET",
						},
					},
				},
				Abort: &ctrlmeshproto.HTTPFaultInjection_Abort{
					Percent:   100,
					ErrorType: &ctrlmeshproto.HTTPFaultInjection_Abort_HttpStatus{HttpStatus: http.StatusForbidden},
				},
			},
		},
	})
	utilruntime.Must(err)
	tProxy := NewTProxy(15002, faultInjectionMgr, circuitInjectionMgr)
	tProxy.Start()
}
