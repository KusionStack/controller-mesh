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
	"fmt"
	"os"

	"net/http"
	"net/url"
	"time"

	"k8s.io/klog/v2"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/constants"
	meshhttp "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/http"
	"github.com/KusionStack/controller-mesh/pkg/proxy/circuitbreaker"
	"github.com/KusionStack/controller-mesh/pkg/proxy/faultinjection"
	utilshttp "github.com/KusionStack/controller-mesh/pkg/utils/http"
)

var (
	enableRestBreaker        = os.Getenv(constants.EnvEnableRestCircuitBreaker) == "true"
	enableRestFaultInjection = os.Getenv(constants.EnvEnableRestFaultInjection) == "true"

	logger = logf.Log.WithName("http-proxy")
)

type ITProxy interface {
	Start()
}

type tproxy struct {
	port            int
	FaultInjector   faultinjection.ManagerInterface
	CircuitInjector circuitbreaker.ManagerInterface
}

func NewTProxy(port int, faultInjector faultinjection.ManagerInterface, circuitInjector circuitbreaker.ManagerInterface) ITProxy {
	return &tproxy{
		port:            port,
		FaultInjector:   faultInjector,
		CircuitInjector: circuitInjector,
	}
}

func (t *tproxy) Start() {
	klog.Infof("start transparent proxy on 127.0.0.1:%d", t.port)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", t.port),
		Handler: http.HandlerFunc(t.handleHTTP),
	}
	klog.Infof("%s", server.ListenAndServe())
}

func (t *tproxy) handleHTTP(resp http.ResponseWriter, req *http.Request) {

	var realEndPointUrl *url.URL
	realEp := req.Header.Get(meshhttp.HeaderMeshRealEndpoint)
	if realEp == "" {
		realEndPointUrl = req.URL
	} else {
		epUrl, err := url.Parse(realEp)
		if err != nil || epUrl == nil {
			logger.Error(err, "Request Header Mesh-Real-Endpoint Parse Error")
			http.Error(resp, fmt.Sprintf("Can not find real endpoint in header %s", err), http.StatusInternalServerError)
			return
		}
		realEndPointUrl = epUrl
		klog.Infof("receive, proxy-host: %s, proxy-method: %s, Mesh-Real-Endpoint: %s", realEndPointUrl.Host, req.Method, realEp)
	}
	klog.Infof("handel http request, url: %s ", realEndPointUrl.String())
	// faultinjection
	if enableRestFaultInjection {
		injector := t.FaultInjector.GetInjectorByUrl(realEndPointUrl, req.Method)
		if injector.Do(resp, req) {
			klog.Infof("fault injected in %s", realEndPointUrl.String())
			return
		}
	}

	// circuitbreaker
	if enableRestBreaker {
		// check request is in the whitelist
		klog.Infof("start checktrafficrule %s", realEndPointUrl.Host)
		result := t.CircuitInjector.ValidateTrafficIntercept(realEndPointUrl.Host, req.Method)
		if !result.Allowed {
			klog.Infof("ErrorTProxy: %s %s  ValidateTrafficIntercept NOPASSED ,checkresult:\t%s", realEndPointUrl.Host, req.Method, result.Reason)
			http.Error(resp, fmt.Sprintf("Forbidden by  ValidateTrafficIntercept breaker, %s, %s", result.Message, result.Reason), http.StatusForbidden)
			return
		}
	}

	// ValidateTrafficIntercept check pass or enableRestBreaker is false  run  http proxy
	klog.Infof("TProxy: %s %s ValidateTrafficIntercept check PASSED or enableRestBreaker is false", realEndPointUrl.Host, req.Method)

	// ValidateRest check
	klog.Infof("start ValidateRest checkrule %s %s", realEndPointUrl.Host, req.Method)
	validateresult := t.CircuitInjector.ValidateRest(req.Header.Get("Mesh-Real-Endpoint"), req.Method)
	if !validateresult.Allowed {
		klog.Infof("ErrorTProxy: %s %s  ValidateRest NOPASSED ,checkresult:%t, validateresultReason:%s", req.Header.Get("Mesh-Real-Endpoint"), req.Method, validateresult.Allowed, validateresult.Reason)
		http.Error(resp, fmt.Sprintf("Forbidden by circuit ValidateRest breaker, %s, %s", validateresult.Message, validateresult.Reason), http.StatusForbidden)
		return
	}
	klog.Infof("TProxy: %s %s ValidateRest check PASSED", realEndPointUrl.Host, req.Method)

	// modify request
	director := func(target *http.Request) {
		target.Header.Set("Pass-Via-Go-TProxy", "1")
		// set new url and host for the request
		target.URL = realEndPointUrl
		target.Host = realEndPointUrl.Host
	}
	proxy := &utilshttp.ReverseProxy{Director: director}
	proxy.FlushInterval = 500 * time.Millisecond
	proxy.ServeHTTP(resp, req)
}
