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

package pod

import (
	"context"
	"encoding/json"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh"
)

type MutatingHandler struct {
	Client           client.Client
	Decoder          *admission.Decoder
	directKubeClient *kubeclientset.Clientset
}

var _ admission.Handler = &MutatingHandler{}

// Handle handles admission requests.
func (h *MutatingHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.AdmissionRequest.Operation != admissionv1.Create {
		return admission.Allowed("")
	}

	obj := &v1.Pod{}
	err := h.Decoder.Decode(req, obj)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	// when pod.namespace is empty, using req.namespace
	if obj.Namespace == "" {
		obj.Namespace = req.Namespace
	}
	if obj.Labels == nil || obj.Labels[ctrlmesh.CtrlmeshEnableProxyLabel] != "true" {
		return admission.Allowed("")
	}

	obj.Labels[ctrlmesh.CtrlmeshWatchOnLimitLabel] = "true"
	if err = h.injectByShardingConfig(ctx, obj); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	marshalled, err := json.Marshal(obj)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshalled)
}

var _ inject.Client = &MutatingHandler{}

// InjectClient injects the client into the PodCreateHandler
func (h *MutatingHandler) InjectClient(c client.Client) (err error) {
	h.directKubeClient, err = kubeclientset.NewForConfig(ctrl.GetConfigOrDie())
	if err != nil {
		klog.Errorf("fail to get apiServer rest client, %s", err)
	}
	h.Client = c
	return nil
}

var _ admission.DecoderInjector = &MutatingHandler{}

func (h *MutatingHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
	return nil
}
