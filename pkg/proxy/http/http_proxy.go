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
	"encoding/json"
	"fmt"

	"net/http"
	"net/url"
	"time"

	"k8s.io/klog/v2"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	meshhttp "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/http"
	"github.com/KusionStack/controller-mesh/pkg/proxy/faultinjection"
	"github.com/KusionStack/controller-mesh/pkg/utils"
	utilshttp "github.com/KusionStack/controller-mesh/pkg/utils/http"
)

var (
	logger = logf.Log.WithName("http-proxy")
)

type ITProxy interface {
	Start()
}

type tproxy struct {
	port          int
	FaultInjector faultinjection.ManagerInterface
}

func NewTProxy(port int, faultInjector faultinjection.ManagerInterface) ITProxy {
	return &tproxy{
		port:          port,
		FaultInjector: faultInjector,
	}
}

func (t *tproxy) Start() {
	klog.Infof("start transparent proxy on 127.0.0.1:%d", t.port)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", t.port),
		Handler: http.HandlerFunc(t.handleHTTP),
	}
	logger.Info("%s", server.ListenAndServe())
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
		logger.Info("receive", "proxy-host", realEndPointUrl.Host, "proxy-method", req.Method, "Mesh-Real-Endpoint", realEp)
	}
	logger.Info("handel http request", "url", realEndPointUrl.String())
	result := t.FaultInjector.FaultInjectionRest(req.Header.Get(meshhttp.HeaderMeshRealEndpoint), req.Method)
	if result.Abort {
		apiErr := utils.HttpToAPIError(int(result.ErrCode), req.Method, result.Message)
		resp.Header().Set("Content-Type", "application/json")
		resp.WriteHeader(int(apiErr.Code))
		if err := json.NewEncoder(resp).Encode(apiErr); err != nil {
			http.Error(resp, fmt.Sprintf("fail to inject fault %v", err), http.StatusInternalServerError)
		}
		logger.Info("faultInjection rule", "rule", fmt.Sprintf("fault injection, %s, %s,%d", result.Reason, result.Message, result.ErrCode))
		return
	}

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
