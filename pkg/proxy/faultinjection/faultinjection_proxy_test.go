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
	"flag"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

func TestRequestAdapter(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	proxyUrl := "http://localhost:15002"
	oldReq, err := http.NewRequest(http.MethodGet, proxyUrl+"/foo", nil)
	if err != nil {
		t.Error(err)
	}
	oldReq.Host = "zpaascoreng.alipay.com"
	newReq, err := http.NewRequest(http.MethodGet, proxyUrl, nil)
	if err != nil {
		t.Error(err)
	}
	newReq.Header.Set("Mesh-Real-Endpoint", "https://zpaascoreng.alipay.com/foo")
	newReqAdapter := RequestAdapter(newReq)
	g.Expect(newReqAdapter.Method).Should(gomega.Equal(oldReq.Method))
	g.Expect(newReqAdapter.Host).Should(gomega.Equal(oldReq.Host))
	g.Expect(newReqAdapter.URL.Path).Should(gomega.Equal(oldReq.URL.Path))

	// check the url including the port
	oldReq.Host = "127.0.0.1:8080"
	newReq.Header.Set("Mesh-Real-Endpoint", "http://127.0.0.1:8080/foo")
	newReqAdapter = RequestAdapter(newReq)
	g.Expect(newReqAdapter.Method).Should(gomega.Equal(oldReq.Method))
	g.Expect(newReqAdapter.Host).Should(gomega.Equal(oldReq.Host))
	g.Expect(newReqAdapter.URL.Path).Should(gomega.Equal(oldReq.URL.Path))
}

func TestTProxy(t *testing.T) {
	//g := gomega.NewGomegaWithT(t)
	go StartProxy()
	time.Sleep(time.Second * 2)
	proxyUrl := "http://localhost:15002"
	req, err := http.NewRequest(http.MethodGet, proxyUrl, nil)
	if err != nil {
		t.Error(err)
	}

	req.Header.Set("Mesh-Real-Endpoint", "https://zpaascoreng.alipay.com")
	_, err = http.DefaultClient.Do(req)
	//g.Expect(resp.StatusCode).Should(gomega.Equal(http.StatusOK))
	req.Header.Set("Mesh-Real-Endpoint", "https://albapi.eu95.alipay.net/foo")
	_, err = http.DefaultClient.Do(req)
	//g.Expect(resp.StatusCode).Should(gomega.Equal(http.StatusBadGateway))
	req.Header.Set("Mesh-Real-Endpoint", "https://zpaascoreng-pre.alipay.com")
	_, err = http.DefaultClient.Do(req)
	//g.Expect(resp.StatusCode).ShouldNot(gomega.Equal(http.StatusForbidden))
}

func StartProxy() {
	var (
		tproxyPort int
		// ruleConfigPath string
		// ruleConfigFile string
	)
	// init klog
	flag.Set("v", "4")
	flag.IntVar(&tproxyPort, "tport", 15002, "port that http-tproxy listens on")
	//flag.StringVar(&ruleConfigPath, "rulecfgpath", "../../tproxy-demo", "the path of rule file")
	//flag.StringVar(&ruleConfigFile, "rulecfgfile", "whitelist.yaml", "the name of rule file")
	flag.Parse()

	// Load whitelist
	//err := rule.LoadWhitelist(ruleConfigPath, ruleConfigFile)
	//if err != nil {
	//	klog.Error(err)
	//}

	// get http-tproxy
	ctx := signals.SetupSignalHandler()

	faultInjectionMgr := NewManager(ctx)
	tproxy, err := NewTProxy(tproxyPort, faultInjectionMgr)
	if err != nil {
		klog.Fatalf("failed to create http-tproxy: %s", err)
		return
	}
	// start proxies
	tproxy.Start()
}

// RequestAdapter creates a new request from the original request for func`CheckWhitelist`
func RequestAdapter(req *http.Request) *http.Request {
	realEndPointUrl, err := url.Parse(req.Header.Get("Mesh-Real-Endpoint"))
	if err != nil {
		klog.Error("Request Header Mesh-Real-Endpoint Parse Error")
	}
	req, err = http.NewRequest(req.Method, realEndPointUrl.String(), nil)
	if err != nil {
		klog.Error("Request Adapter Error")
	}
	return req
}
