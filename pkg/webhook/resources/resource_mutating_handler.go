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

package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	SelectorPrefix = "kridge.kusionstack.io"
)

var _ inject.Client = &MutatingHandler{}

type MutatingHandler struct {
	Client           client.Client
	Decoder          *admission.Decoder
	directKubeClient *kubeclientset.Clientset
}

func (r *MutatingHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.AdmissionRequest.Operation != admissionv1.Create && req.AdmissionRequest.Operation != admissionv1.Update {
		return admission.Allowed("")
	}

	if len(req.Namespace) == 0 {
		return admission.Allowed("")
	}

	// Auto gen ShardingConfig
	if req.Kind.Kind == "ShardingConfig" {
		return r.HandleShardingConfig(ctx, req)
	}

	ns := &v1.Namespace{}
	if err := r.getNamespaceWithRetry(r.Client, ctx, types.NamespacedName{Name: req.Namespace}, ns, 5); err != nil {
		klog.Errorf("webhook get namespace %s from cache failed, %s", req.Namespace, err)
		if errors.IsNotFound(err) && r.directKubeClient != nil {
			ns, err = r.directKubeClient.CoreV1().Namespaces().Get(ctx, req.Namespace, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("webhook get namespace %s from apiServer failed, %s", req.Namespace, err)
				return admission.Errored(http.StatusBadRequest, err)
			}
		} else {
			return admission.Errored(http.StatusBadRequest, err)
		}
	}
	patchLabel := map[string]string{}
	if ns.Labels != nil {
		for key, val := range ns.Labels {
			if r.isSelecterKey(key) {
				patchLabel[key] = val
			}
		}
	}
	if len(patchLabel) == 0 {
		return admission.Allowed("")
	}

	marshalled, err := json.Marshal(&PatchMeta{&metav1.ObjectMeta{Labels: patchLabel}})
	if err != nil {
		klog.Errorf("meta marshal error, %s", err)
		return admission.Errored(http.StatusInternalServerError, err)
	}

	patches, err := jsonpatch.CreatePatch(req.AdmissionRequest.Object.Raw, marshalled)
	if err != nil {
		klog.Errorf("create selector patch error, %s", err)
		return admission.Errored(http.StatusInternalServerError, err)
	}

	patches = filterOperation(patches)

	if len(patches) == 0 {
		return admission.Allowed("")
	}

	return patchResponseFromOperation(patches)
}

func (r *MutatingHandler) getNamespaceWithRetry(c client.Client, ctx context.Context, key client.ObjectKey, obj client.Object, cnt int) error {
	for i := 0; ; i++ {
		if err := c.Get(ctx, key, obj); err != nil && i >= cnt-1 {
			return err
		} else if err == nil {
			break
		}
	}
	return nil
}

func (r *MutatingHandler) isSelecterKey(key string) bool {
	return strings.HasPrefix(key, SelectorPrefix)
}

func (r *MutatingHandler) InjectDecoder(d *admission.Decoder) error {
	r.Decoder = d
	return nil
}

func (r *MutatingHandler) InjectClient(c client.Client) (err error) {
	r.directKubeClient, err = kubeclientset.NewForConfig(ctrl.GetConfigOrDie())
	if err != nil {
		klog.Errorf("fail to get apiServer rest client, %s", err)
	}
	r.Client = c
	return err
}

func patchResponseFromOperation(patches []jsonpatch.Operation) admission.Response {
	return admission.Response{
		Patches: patches,
		AdmissionResponse: admissionv1.AdmissionResponse{
			Allowed: true,
			PatchType: func() *admissionv1.PatchType {
				if len(patches) == 0 {
					return nil
				}
				pt := admissionv1.PatchTypeJSONPatch
				return &pt
			}(),
		},
	}
}

func filterOperation(operation []jsonpatch.Operation) []jsonpatch.Operation {
	var result []jsonpatch.Operation
	for _, ops := range operation {
		if (ops.Operation == "add" || ops.Operation == "replace") && ops.Value != nil {
			result = append(result, ops)
		}
	}
	return result
}

type PatchMeta struct {
	Metadata *metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
}
