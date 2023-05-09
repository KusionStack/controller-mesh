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

package shardingconfig

import "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

// +kubebuilder:webhook:path=/validate-kridge-shardingconfig,mutating=false,failurePolicy=fail,sideEffects=None,admissionReviewVersions=v1;v1beta1,groups=kridge.kusionstack.io,resources=shardingconfigs,verbs=create;update,versions=v1alpha1,name=shardingconfigs.kridge.validating.io

var (
	// HandlerMap contains admission webhook handlers
	HandlerMap = map[string]admission.Handler{
		"validate-kridge-shardingconfig": &ValidatingHandler{},
	}
)
