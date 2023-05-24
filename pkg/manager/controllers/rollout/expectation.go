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

package rollout

import (
	"encoding/json"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/KusionStack/kridge/pkg/apis/kridge"
)

type ExpectedRevision struct {
	UpdateRevision string            `json:"updateRevision"`
	PodRevision    map[string]string `json:"podRevision"`
}

func GetExpectedRevision(sts *appsv1.StatefulSet) *ExpectedRevision {
	val := sts.Annotations[kridge.KdRollingExpectedAnno]
	result := &ExpectedRevision{}
	if err := json.Unmarshal([]byte(val), result); err != nil {
		klog.Errorf("fail to unmarshal MeshRollingExpectedAnno %v", err)
	}
	return result
}

func SetExpectedRevision(sts *appsv1.StatefulSet, podRevision map[string]string) bool {
	val := sts.Annotations[kridge.KdRollingExpectedAnno]
	rev := &ExpectedRevision{
		UpdateRevision: recentRevision(sts),
		PodRevision:    podRevision,
	}
	byt, _ := json.Marshal(rev)
	if string(byt) == val {
		return false
	}
	sts.Annotations[kridge.KdRollingExpectedAnno] = string(byt)
	return true
}

func isUpdatedRevision(sts *appsv1.StatefulSet, po *corev1.Pod) bool {
	expectedRevision := recentRevision(sts)
	revision, _ := po.Labels[appsv1.StatefulSetRevisionLabel]
	return revision == expectedRevision
}

func recentRevision(sts *appsv1.StatefulSet) string {
	if sts.Status.UpdateRevision != "" {
		return sts.Status.UpdateRevision
	}
	return sts.Status.CurrentRevision
}
