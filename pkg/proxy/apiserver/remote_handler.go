/*
Copyright 2024 The KusionStack Authors.

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

package apiserver

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/apiserver/pkg/authentication/user"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	"k8s.io/klog/v2"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/constants"
	meshhttp "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/http"
	ctrlmeshrest "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/rest"
	"github.com/KusionStack/controller-mesh/pkg/proxy/apiserver/common"
	proxyfilters "github.com/KusionStack/controller-mesh/pkg/proxy/filters"
	utilshttp "github.com/KusionStack/controller-mesh/pkg/utils/http"
	"github.com/KusionStack/controller-mesh/pkg/utils/pool"
)

type RemoteProxy struct {
	opts           *common.Options
	inSecureServer *http.Server
}

func NewRemoteProxy(opts *common.Options) (*RemoteProxy, error) {
	clusterStore := NewClusterStore()
	inHandler := &remoteHandler{
		injector: common.NewInjector(opts.SpecManager),
		store:    clusterStore,
	}

	var handler http.Handler = inHandler
	handler = genericfilters.WithWaitGroup(handler, opts.LongRunningFunc, opts.HandlerChainWaitGroup)
	handler = common.WithRequestInfo(handler, opts.RequestInfoResolver)
	handler = proxyfilters.WithPanicRecovery(handler, opts.RequestInfoResolver)
	handler = WithRemoteRegister(handler, clusterStore)
	inSecureServer := &http.Server{
		Addr:           net.JoinHostPort("127.0.0.1", strconv.Itoa(constants.ProxyRemoteApiServerPort)),
		Handler:        handler,
		MaxHeaderBytes: 1 << 20,
	}

	return &RemoteProxy{
		opts:           opts,
		inSecureServer: inSecureServer,
	}, nil
}

type remoteHandler struct {
	store    *ClusterStore
	injector common.Injector
}

func (p *RemoteProxy) Start() (<-chan struct{}, error) {
	stoppedCh := make(chan struct{})
	go func() {
		defer close(stoppedCh)
		klog.Infof("start listen and serve %s", p.inSecureServer.Addr)
		err := p.inSecureServer.ListenAndServe()
		if err != nil {
			klog.Errorf("fail listen and serve %v", err)
		}
	}()
	return stoppedCh, nil
}

func (rh *remoteHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestInfo, ok := apirequest.RequestInfoFrom(r.Context())
	if !ok {
		klog.Errorf("%s %s %s, no request info in context", r.Method, r.Header.Get("Content-Type"), r.URL)
		http.Error(rw, "no request info in context", http.StatusBadRequest)
		return
	}

	u, err := rh.getURL(r)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	p := utilshttp.NewSingleHostReverseProxy(u)
	cluster := rh.store.Get(u.Host)
	if cluster == nil {
		http.Error(rw, fmt.Sprintf("cluster not found in store. Host: %s", u.Host), http.StatusInternalServerError)
		return
	}
	p.Transport = cluster.Transport
	p.FlushInterval = 500 * time.Millisecond
	p.BufferPool = pool.BytesPool

	if requestInfo.IsResourceRequest && upgradeSubresources.Has(requestInfo.Subresource) {
		rh.upgradeProxyHandler(rw, r, cluster.Config)
		return
	}

	if err = rh.injector.Inject(r, requestInfo); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	var statusCode int
	modifyResponse := func(resp *http.Response) error {
		statusCode = resp.StatusCode
		return nil
	}

	// no need to log leader election requests
	defer func() {
		if klog.V(4).Enabled() {
			klog.InfoS("PROXY",
				"verb", r.Method,
				"URI", r.RequestURI,
				"latency", time.Since(startTime),
				"userAgent", r.UserAgent(),
				"resp", statusCode,
			)
		}
	}()

	p.ModifyResponse = modifyResponse
	p.ServeHTTP(rw, r)
}

func (rh *remoteHandler) upgradeProxyHandler(rw http.ResponseWriter, r *http.Request, cfg *rest.Config) {
	tlsConfig, err := rest.TLSConfigFor(cfg)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	upgradeRoundTripper := spdy.NewRoundTripper(tlsConfig)
	wrappedRT, err := rest.HTTPWrappersForConfig(cfg, upgradeRoundTripper)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	proxyRoundTripper := transport.NewAuthProxyRoundTripper(user.APIServerUser, []string{user.SystemPrivilegedGroup}, nil, wrappedRT)
	u, err := rh.getURL(r)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	p := proxy.NewUpgradeAwareHandler(u, proxyRoundTripper, true, true, &responder{w: rw})
	p.ServeHTTP(rw, r)
}

func (rh *remoteHandler) getURL(r *http.Request) (*url.URL, error) {
	remoteHost := r.Header.Get(meshhttp.HeaderRemoteApiServerHost)
	if len(remoteHost) == 0 {
		return nil, fmt.Errorf("not found remote api server host")
	}
	return url.Parse(fmt.Sprintf("https://%s", remoteHost))
}

func (rh *remoteHandler) newProxy(r *http.Request) (*utilshttp.ReverseProxy, error) {
	u, err := rh.getURL(r)
	if err != nil {
		return nil, err
	}
	p := utilshttp.NewSingleHostReverseProxy(u)
	cluster := rh.store.Get(u.Host)
	if cluster == nil {
		return nil, fmt.Errorf("cluster not found in store. Host: %s", u.Host)
	}
	p.Transport = cluster.Transport
	p.FlushInterval = 500 * time.Millisecond
	p.BufferPool = pool.BytesPool
	return p, nil
}

func WithRemoteRegister(handler http.Handler, store *ClusterStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasPrefix(req.URL.Path, "/remote-register") {
			handler.ServeHTTP(w, req)
			return
		}
		cfgReq := &ctrlmeshrest.ConfigRequest{}
		if err := json.NewDecoder(req.Body).Decode(cfgReq); err != nil {
			http.Error(w, fmt.Sprintf("failed to decode ConfigRequest, %v", err), http.StatusBadRequest)
			return
		}
		var err error
		defer req.Body.Close()
		switch cfgReq.Action {
		case ctrlmeshrest.Add, ctrlmeshrest.Update:
			err = store.StoreListOf(cfgReq.Kubeconfigs...)
		case ctrlmeshrest.Delete:
			err = store.StoreListOf(cfgReq.Kubeconfigs...)
		default:
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to %s kubeconfig, %v", cfgReq.Action, err), http.StatusBadRequest)
		}
	})
}
