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
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	"k8s.io/apiserver/pkg/endpoints/request"

	ctrlmeshhttp "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/http"
)

var (
	trimApiServerPathPrefix = os.Getenv(ctrlmeshhttp.HeaderHttpApiServerPreUrl)
)

// WithRequestInfo attaches a RequestInfo to the context.
func WithRequestInfo(handler http.Handler, resolver request.RequestInfoResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		oldUrl := req.URL
		localUrl, err := TrimUrl(req.URL, trimApiServerPathPrefix)
		if err != nil {
			responsewriters.InternalError(w, req, fmt.Errorf("failed to trim url by: %s, %v", trimApiServerPathPrefix, err))
		}
		req.URL = localUrl
		ctx := req.Context()
		info, err := resolver.NewRequestInfo(req)
		req.URL = oldUrl
		if err != nil {
			responsewriters.InternalError(w, req, fmt.Errorf("failed to create RequestInfo: %v", err))
			return
		}

		req = req.WithContext(request.WithRequestInfo(ctx, info))

		handler.ServeHTTP(w, req)
	})
}

func TrimUrl(inUrl *url.URL, prePath string) (*url.URL, error) {
	//  /apis/*/v1/clusterextensions/*       /proxy
	//  /apis/*/v1/clusterextensions/*       /proxy/
	//  /apis/*/v1/clusterextensions/cluster1/proxy/api/v1/namespaces/*/pods/*
	if prePath == "" {
		return inUrl, nil
	}
	segments := clear(strings.Split(inUrl.Path, "/"))
	preSegments := clear(strings.Split(prePath, "/"))
	if len(segments) < len(preSegments) {
		return inUrl, fmt.Errorf(fmt.Sprintf("%s is too long than %s", prePath, inUrl.Path))
	}
	resultPath := ""
	done := true
	for i, item := range preSegments {
		if item == "*" {
			continue
		}
		if item != segments[i] {
			done = false
			break
		}
	}
	if !done {
		return inUrl, fmt.Errorf(fmt.Sprintf("fail to trim url, %s is not %s prefix", prePath, inUrl.Path))
	}
	for i := len(preSegments); i < len(segments); i++ {
		resultPath += "/" + segments[i]
	}
	return url.Parse(resultPath)
}

func clear(items []string) []string {
	var result []string
	for _, item := range items {
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}
