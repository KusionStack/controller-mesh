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

package common

import (
	"fmt"
	"net/http"
	"net/url"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/klog/v2"

	protomanager "github.com/KusionStack/controller-mesh/pkg/proxy/proto"
	"github.com/KusionStack/controller-mesh/pkg/utils"
	httputil "github.com/KusionStack/controller-mesh/pkg/utils/http"
)

const (
	labelSelectorKey = "labelSelector"
)

type Injector interface {
	Inject(*http.Request, *request.RequestInfo) error
}

type injector struct {
	specManager *protomanager.SpecManager
}

func NewInjector(m *protomanager.SpecManager) Injector {
	return &injector{specManager: m}
}

func (r *injector) Inject(httpReq *http.Request, reqInfo *request.RequestInfo) error {
	if !reqInfo.IsResourceRequest {
		return nil
	}
	gvr := schema.GroupVersionResource{Group: reqInfo.APIGroup, Version: reqInfo.APIVersion, Resource: reqInfo.Resource}
	protoSpec := r.specManager.AcquireSpec()
	defer func() {
		r.specManager.ReleaseSpec()
	}()

	switch reqInfo.Verb {
	case "list", "watch":
		objectSelector := protoSpec.GetObjectSelector(gvr.GroupResource())
		if objectSelector != nil {
			if err := injectSelector(httpReq, objectSelector); err != nil {
				return fmt.Errorf("failed to inject selector %s into request: %v", utils.DumpJSON(objectSelector), err)
			}
		}
	default:
	}

	return nil
}

func injectSelector(httpReq *http.Request, sel *metav1.LabelSelector) error {
	raw, err := httputil.ParseRawQuery(httpReq.URL.RawQuery)
	if err != nil {
		return err
	}
	var oldLabelSelector *metav1.LabelSelector
	if oldSelector, ok := raw[labelSelectorKey]; ok {
		oldSelector, err = url.QueryUnescape(oldSelector)
		if err != nil {
			return err
		}
		oldLabelSelector, err = metav1.ParseToLabelSelector(oldSelector)
		if err != nil {
			return err
		}
	}
	selector, err := utils.ValidatedLabelSelectorAsSelector(utils.MergeLabelSelector(sel, oldLabelSelector))
	if err != nil {
		return err
	}
	raw[labelSelectorKey] = url.QueryEscape(selector.String())
	httpReq.Header.Add("OLD-RAW-QUERY", httpReq.URL.RawQuery)
	oldURL := httpReq.URL.String()
	httpReq.URL.RawQuery = httputil.MarshalRawQuery(raw)
	klog.Infof("Injected object selector in request, %s -> %s", oldURL, httpReq.URL.String())
	return nil
}
