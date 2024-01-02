package faultinjection

/**
 *
 * @Description Transparent http proxy
 * @Date 4:08 下午 2021/10/26
 **/
import (
	"encoding/json"
	"fmt"

	"net/http"
	"net/url"
	"time"

	meshhttp "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/http"
	utilhttp "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/utils/http"
)

type ITProxy interface {
	Start()
}

type tproxy struct {
	port          int
	FaultInjector *manager
}

func NewTProxy(port int, faultInjector ManagerInterface) (ITProxy, error) {
	manager, ok := faultInjector.(*manager)
	if !ok {
		return nil, fmt.Errorf("faultInjector is not of type *manager")
	}
	return &tproxy{
		port:          port,
		FaultInjector: manager,
	}, nil
}

func (t *tproxy) Start() {
	logger.Info("start transparent proxy on 127.0.0.1", "port", t.port)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", t.port),
		Handler: http.HandlerFunc(t.handleHTTP),
	}

	logger.Info("%s", server.ListenAndServe())
}

func (t *tproxy) handleHTTP(resp http.ResponseWriter, req *http.Request) {
	realEndPointUrl, err := url.Parse(req.Header.Get(meshhttp.HeaderMeshRealEndpoint))
	logger.Info("receive", " proxy-host", realEndPointUrl.Host, "proxy-method", req.Method, " Mesh-Real-Endpoint", req.Header.Get(meshhttp.HeaderMeshRealEndpoint))
	if err != nil || realEndPointUrl == nil {
		logger.Error(err, "Request Header Mesh-Real-Endpoint Parse Error")
		http.Error(resp, fmt.Sprintf("Can not find real endpoint in header %s", err), http.StatusInternalServerError)
		return
	}

	// ValidateRest check
	logger.Info("start ValidateRest checkrule ", "realEndPointUrl.Host", realEndPointUrl.Host, "req.Method", req.Method)
	result := t.FaultInjector.FaultInjectionRest(req.Header.Get(meshhttp.HeaderMeshRealEndpoint), req.Method)
	if !result.Abort {
		apiErr := httpToAPIError(int(result.ErrCode), result.Message)
		if apiErr.Code != http.StatusOK {
			resp.Header().Set("Content-Type", "application/json")
			resp.WriteHeader(int(apiErr.Code))
			json.NewEncoder(resp).Encode(apiErr)
			logger.Info("faultinjection rule", "rule", fmt.Sprintf("fault injection, %s, %s,%d", result.Reason, result.Message, result.ErrCode))
			return
		}
	}
	logger.Info("TProxy: ValidateRest check PASSED", "realEndPointUrl.Host", realEndPointUrl.Host, "req.Method", req.Method)

	// modify request
	director := func(target *http.Request) {
		target.Header.Set("Pass-Via-Go-TProxy", "1")
		// set new url and host for the request
		target.URL = realEndPointUrl
		target.Host = realEndPointUrl.Host
	}
	proxy := &utilhttp.ReverseProxy{Director: director}

	const defaultFlushInterval = 500 * time.Millisecond
	proxy.FlushInterval = defaultFlushInterval
	proxy.ServeHTTP(resp, req)
}
