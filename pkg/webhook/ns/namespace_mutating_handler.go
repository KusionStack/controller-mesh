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

package ns

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh"
	"github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh/constants"
	"github.com/KusionStack/ctrlmesh/pkg/utils/rand"
)

type MutatingHandler struct {
	Client  client.Client
	Decoder *admission.Decoder
}

var _ admission.Handler = &MutatingHandler{}

// Handle handles admission requests.
func (h *MutatingHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.AdmissionRequest.Operation != admissionv1.Create && req.AdmissionRequest.Operation != admissionv1.Update {
		return admission.Allowed("")
	}

	//var rbac bool
	obj := &v1.Namespace{}
	if err := h.Decoder.Decode(req, obj); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if obj.Labels == nil {
		obj.Labels = make(map[string]string)
	}

	if h.shouldUpdateNs(obj) {
		marshalled, err := json.Marshal(obj)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshalled)
	}
	return admission.Allowed("")
}

func (h *MutatingHandler) shouldUpdateNs(ns *v1.Namespace) (shouldUpdate bool) {
	shouldUpdate = false
	if _, exist := ns.Labels[ctrlmesh.KdControlKey]; !exist {
		ns.Labels[ctrlmesh.KdControlKey] = "true"
		shouldUpdate = true
	}
	if val, exist := ns.Labels[ctrlmesh.KdNamespaceKey]; !exist || val != ns.Name {
		ns.Labels[ctrlmesh.KdNamespaceKey] = ns.Name
		shouldUpdate = true
	}
	nsHash := strconv.Itoa(rand.Hash(ns.Name, constants.DefaultShardingSize))
	if val, exist := ns.Labels[ctrlmesh.KdShardHashKey]; !exist || nsHash != val {
		ns.Labels[ctrlmesh.KdShardHashKey] = nsHash
		shouldUpdate = true
	}
	return shouldUpdate
}

var _ inject.Client = &MutatingHandler{}

// InjectClient injects the client into the PodCreateHandler
func (h *MutatingHandler) InjectClient(c client.Client) error {
	h.Client = c
	return nil
}

var _ admission.DecoderInjector = &MutatingHandler{}

func (h *MutatingHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
	return nil
}
