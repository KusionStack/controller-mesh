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
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh"
	"github.com/KusionStack/controller-mesh/pkg/manager/controllers/rollout"
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

func (h *MutatingHandler) revisionRollOut(ctx context.Context, pod *v1.Pod) (err error) {
	podRevision := pod.Labels[appsv1.ControllerRevisionHashLabelKey]
	sts := &appsv1.StatefulSet{}
	if pod.OwnerReferences == nil || len(pod.OwnerReferences) == 0 {
		return fmt.Errorf("illegal owner reference")
	}
	if pod.OwnerReferences[0].Kind != "StatefulSet" {
		return fmt.Errorf("illegal owner reference kind %s", pod.OwnerReferences[0].Kind)
	}

	sts, err = h.directKubeClient.AppsV1().StatefulSets(pod.Namespace).Get(ctx, pod.OwnerReferences[0].Name, metav1.GetOptions{})
	if err != nil {
		klog.Error(err)
		return err
	}
	if sts.Spec.UpdateStrategy.Type != appsv1.OnDeleteStatefulSetStrategyType {
		return nil
	}
	expectState := rollout.GetExpectedRevision(sts)
	if expectState.UpdateRevision == "" || expectState.PodRevision == nil || expectState.PodRevision[pod.Name] == "" {
		return
	}
	expectedRevision := expectState.PodRevision[pod.Name]
	if expectedRevision == podRevision {
		return
	}
	// Do not use manager client get ControllerRevision. (To avoid Informer cache)
	expectRevision, err := h.directKubeClient.AppsV1().ControllerRevisions(pod.Namespace).Get(ctx, expectedRevision, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("cannot find old ControllerRevision %s", expectedRevision)
	}

	createRevision, err := h.directKubeClient.AppsV1().ControllerRevisions(pod.Namespace).Get(ctx, podRevision, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("cannot find ControllerRevision %s by pod %s/%s", podRevision, pod.Namespace, pod.Name)
	}

	expectedSts := &appsv1.StatefulSet{}
	createdSts := &appsv1.StatefulSet{}

	applyPatch(expectedSts, &expectRevision.Data.Raw)
	applyPatch(createdSts, &createRevision.Data.Raw)

	expectedPo := &v1.Pod{
		Spec: expectedSts.Spec.Template.Spec,
	}
	createdPo := &v1.Pod{
		Spec: createdSts.Spec.Template.Spec,
	}

	expectedBt, _ := runtime.Encode(patchCodec, expectedPo)
	createdBt, _ := runtime.Encode(patchCodec, createdPo)
	currentBt, _ := runtime.Encode(patchCodec, pod)

	patch, err := strategicpatch.CreateTwoWayMergePatch(createdBt, expectedBt, expectedPo)
	if err != nil {
		return err
	}
	originBt, err := strategicpatch.StrategicMergePatch(currentBt, patch, pod)
	if err != nil {
		return err
	}
	newPod := &v1.Pod{}
	if err = json.Unmarshal(originBt, newPod); err != nil {
		return err
	}
	pod.Spec = newPod.Spec
	pod.Labels[appsv1.ControllerRevisionHashLabelKey] = expectedRevision
	return
}

var patchCodec = scheme.Codecs.LegacyCodec(schema.GroupVersion{Group: "apps", Version: "v1"}, schema.GroupVersion{Version: "v1"})

func applyPatch(target runtime.Object, podPatch *[]byte) error {
	patched, err := strategicpatch.StrategicMergePatch([]byte(runtime.EncodeOrDie(patchCodec, target)), *podPatch, target)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(patched, target); err != nil {
		return err
	}
	return nil
}
