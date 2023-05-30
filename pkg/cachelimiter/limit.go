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

package cachelimiter

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/KusionStack/kridge/pkg/apis/kridge"
)

func DefaultObjectSelector() cache.SelectorsByObject {
	return DefaultObjectSelectorWithObject(
		&corev1.Pod{},
		//&corev1.ConfigMap{},
		//&coordinationv1.Lease{},
	)
}

func DefaultObjectSelectorWithObject(objs ...client.Object) cache.SelectorsByObject {
	selectorsByObject := cache.SelectorsByObject{}
	for _, obj := range objs {
		selectorsByObject[obj] = DefaultSelector()
	}
	return selectorsByObject
}

var (
	DefaultSelector = func() cache.ObjectSelector {
		selector, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
			MatchLabels: map[string]string{
				kridge.KdWatchOnLimitKey: "true",
			},
		})
		return cache.ObjectSelector{
			Label: selector,
		}
	}
)
