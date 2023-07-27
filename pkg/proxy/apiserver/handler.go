/*
Copyright 2023 The KusionStack Authors.
Copyright 2021 The Kruise Authors.

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
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/server"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	"k8s.io/klog/v2"

	"github.com/KusionStack/kridge/pkg/apis/kridge/constants"
	proxyfilters "github.com/KusionStack/kridge/pkg/proxy/filters"
	"github.com/KusionStack/kridge/pkg/proxy/leaderelection"
	"github.com/KusionStack/kridge/pkg/utils"
	utilshttp "github.com/KusionStack/kridge/pkg/utils/http"
	"github.com/KusionStack/kridge/pkg/utils/pool"
)

var (
	upgradeSubresources = sets.NewString("exec", "attach")
	enableIpTable       = os.Getenv(constants.EnvIPTable) == "true"
	enableWebhookProxy  = os.Getenv(constants.EnvEnableWebHookProxy) == "true"
)

type Proxy struct {
	opts           *Options
	inSecureServer *http.Server
	servingInfo    *server.SecureServingInfo
	handler        http.Handler
}

func NewProxy(opts *Options) (*Proxy, error) {
	var servingInfo *server.SecureServingInfo
	if enableIpTable {
		if err := opts.ApplyTo(&servingInfo); err != nil {
			return nil, fmt.Errorf("error apply options %s: %v", utils.DumpJSON(opts), err)
		}
	}

	tp, err := rest.TransportFor(opts.Config)
	if err != nil {
		return nil, fmt.Errorf("error get transport for config %s: %v", utils.DumpJSON(opts.Config), err)
	}

	inHandler := &handler{
		cfg:       opts.Config,
		transport: tp,
		injector:  New(opts.SpecManager),
	}
	if opts.LeaderElectionName != "" {
		inHandler.electionHandler = leaderelection.New(opts.SpecManager, opts.LeaderElectionName)
	} else {
		klog.Infof("Skip proxy leader election for no leader-election-name set")
	}

	var handler http.Handler = inHandler
	// TODO: CircuitBreaker handler
	//handler = circuitbreaker.WithBreaker(handler)
	handler = genericfilters.WithWaitGroup(handler, opts.LongRunningFunc, opts.HandlerChainWaitGroup)
	handler = WithRequestInfo(handler, opts.RequestInfoResolver)
	handler = proxyfilters.WithPanicRecovery(handler, opts.RequestInfoResolver)

	inSecureServer := &http.Server{
		Addr:           net.JoinHostPort(opts.SecureServingOptions.BindAddress.String(), strconv.Itoa(opts.SecureServingOptions.BindPort)),
		Handler:        handler,
		MaxHeaderBytes: 1 << 20,
	}

	return &Proxy{
		opts:           opts,
		servingInfo:    servingInfo,
		inSecureServer: inSecureServer,
		handler:        handler,
	}, nil
}

func (p *Proxy) Start(ctx context.Context) (<-chan struct{}, error) {
	if enableIpTable {
		serverShutdownCh, _, err := p.servingInfo.Serve(p.handler, time.Minute, ctx.Done())
		if err != nil {
			return nil, fmt.Errorf("error serve with options %s: %v", utils.DumpJSON(p.opts), err)
		}
		return serverShutdownCh, nil
	}

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

type handler struct {
	cfg             *rest.Config
	transport       http.RoundTripper
	injector        Injector
	electionHandler leaderelection.Handler
}

func getReqInfoStr(r *apirequest.RequestInfo) string {
	return fmt.Sprintf("RequestInfo: { Path: %s, APIGroup: %s, Resource: %s, Subresource: %s, Verb: %s, Namespace: %s, Name: %s, APIVersion: %s }", r.Path, r.APIGroup, r.Resource, r.Subresource, r.Verb, r.Namespace, r.Name, r.APIVersion)
}

func (h *handler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestInfo, ok := apirequest.RequestInfoFrom(r.Context())
	klog.Infof("handle http req %s", r.URL.String())
	klog.Infof(getReqInfoStr(requestInfo))
	if !ok {
		klog.Errorf("%s %s %s, no request info in context", r.Method, r.Header.Get("Content-Type"), r.URL)
		http.Error(rw, "no request info in context", http.StatusBadRequest)
		return
	}

	if requestInfo.IsResourceRequest && upgradeSubresources.Has(requestInfo.Subresource) {
		h.upgradeProxyHandler(rw, r)
		return
	}

	if h.electionHandler != nil {
		if ok, modifyResponse, err := h.electionHandler.Handle(requestInfo, r); err != nil {
			klog.Errorf("%s %s %s, failed to adapt leader election lock: %v", r.Method, r.Header.Get("Content-Type"), r.URL, err)
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		} else if ok {
			p := h.newProxy(r)
			p.ModifyResponse = modifyResponse
			p.ServeHTTP(rw, r)
			return
		}
	}

	err := h.injector.Inject(r, requestInfo)
	if err != nil {
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

	p := h.newProxy(r)
	p.ModifyResponse = modifyResponse
	p.ServeHTTP(rw, r)
}

func (h *handler) newProxy(r *http.Request) *utilshttp.ReverseProxy {
	p := utilshttp.NewSingleHostReverseProxy(h.getURL(r))
	p.Transport = h.transport
	p.FlushInterval = 500 * time.Millisecond
	p.BufferPool = pool.BytesPool
	return p
}

func (h *handler) getURL(r *http.Request) *url.URL {

	u, _ := url.Parse(fmt.Sprintf("https://%s", r.Host))
	if !enableIpTable {
		u, _ = url.Parse(fmt.Sprintf(h.cfg.Host))
		klog.Infof("disable IPTABLE, proxy apiServer with real host %s", u.String())
		r.Host = ""
	}
	return u
}

func (h *handler) upgradeProxyHandler(rw http.ResponseWriter, r *http.Request) {
	tlsConfig, err := rest.TLSConfigFor(h.cfg)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	upgradeRoundTripper := spdy.NewRoundTripper(tlsConfig)
	wrappedRT, err := rest.HTTPWrappersForConfig(h.cfg, upgradeRoundTripper)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	proxyRoundTripper := transport.NewAuthProxyRoundTripper(user.APIServerUser, []string{user.SystemPrivilegedGroup}, nil, wrappedRT)

	p := proxy.NewUpgradeAwareHandler(h.getURL(r), proxyRoundTripper, true, true, &responder{w: rw})
	p.ServeHTTP(rw, r)
}

// responder implements ErrorResponder for assisting a connector in writing objects or errors.
type responder struct {
	w http.ResponseWriter
}

// TODO: this should properly handle content type negotiation
// if the caller asked for protobuf and you write JSON bad things happen.
func (r *responder) Object(statusCode int, obj runtime.Object) {
	responsewriters.WriteRawJSON(statusCode, obj, r.w)
}

func (r *responder) Error(_ http.ResponseWriter, _ *http.Request, err error) {
	http.Error(r.w, err.Error(), http.StatusInternalServerError)
}
