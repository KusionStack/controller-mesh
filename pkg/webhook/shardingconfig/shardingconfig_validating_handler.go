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

import (
	"context"
	"fmt"
	"net/http"
	"reflect"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh"
	ctrlmeshv1alpha1 "github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh/v1alpha1"
)

var (
	enableForceValidate = false
	configSelector      labels.Selector
)

func init() {
	configSelector, _ = metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      ctrlmesh.KdIgnoreValidateLabel,
				Operator: metav1.LabelSelectorOpDoesNotExist,
			},
		},
	})
}

type ValidatingHandler struct {
	Client  client.Client
	Decoder *admission.Decoder
}

var _ admission.Handler = &ValidatingHandler{}

// Handle handles admission requests.
func (h *ValidatingHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	obj := &ctrlmeshv1alpha1.ShardingConfig{}
	err := h.Decoder.Decode(req, obj)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if _, ok := obj.GetLabels()[ctrlmesh.KdIgnoreValidateLabel]; ok {
		return admission.ValidationResponse(true, "")
	}

	if err = validate(obj); err != nil {
		return admission.Errored(http.StatusUnprocessableEntity, err)
	}

	if req.AdmissionRequest.Operation == admissionv1.Update {
		oldObj := &ctrlmeshv1alpha1.ShardingConfig{}
		if err := h.Decoder.DecodeRaw(req.AdmissionRequest.OldObject, oldObj); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if err := validateUpdate(obj, oldObj); err != nil {
			return admission.Errored(http.StatusUnprocessableEntity, err)
		}
	}
	return validateConfigResource(h.Client, obj)
}

func validateConfigResource(c client.Client, obj *ctrlmeshv1alpha1.ShardingConfig) admission.Response {
	if !enableForceValidate {
		return admission.ValidationResponse(true, "")
	}
	cfgs := &ctrlmeshv1alpha1.ShardingConfigList{}

	if err := c.List(context.TODO(), cfgs, client.InNamespace(obj.Namespace), client.MatchingLabelsSelector{Selector: configSelector}); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	resourceRangeMap := map[string]sets.Set[string]{}
	for _, cfg := range cfgs.Items {
		if cfg.Name == obj.Name {
			continue
		}
		for _, limit := range cfg.Spec.Limits {
			if limit.Selector == nil {
				continue
			}

			nss := &v1.NamespaceList{}
			selector, err := metav1.LabelSelectorAsSelector(limit.Selector)
			if err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}

			err = c.List(context.TODO(), nss, &client.ListOptions{LabelSelector: selector})
			if err != nil {
				return admission.Errored(http.StatusInternalServerError, err)
			}
			targetNsName := getNamespaceNameSets(nss)

			for _, resources := range limit.RelatedResources {
				for _, resource := range resources.Resources {
					if ran, ok := resourceRangeMap[resource]; ok {
						for _, name := range targetNsName {
							if ran.Has(name) {
								return admission.Errored(http.StatusUnprocessableEntity, fmt.Errorf("resource %s is conflict in namespace %s, ShardingConfig %s/%s", resource, name, cfg.Namespace, cfg.Name))
							}
							ran.Insert(name)
						}
					} else {
						resourceRangeMap[resource] = sets.New[string](targetNsName...)
					}
				}
			}
		}
	}

	for _, limit := range obj.Spec.Limits {
		if limit.Selector == nil {
			continue
		}

		nss := &v1.NamespaceList{}
		selector, err := metav1.LabelSelectorAsSelector(limit.Selector)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		err = c.List(context.TODO(), nss, &client.ListOptions{LabelSelector: selector})
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		targetNsName := getNamespaceNameSets(nss)

		for _, resources := range limit.RelatedResources {
			for _, resource := range resources.Resources {
				if ran, ok := resourceRangeMap[resource]; ok {
					for _, name := range targetNsName {
						if ran.Has(name) {
							return admission.Errored(http.StatusUnprocessableEntity, fmt.Errorf("resource %s is conflict in namespace %s", resource, name))
						}
						ran.Insert(name)
					}
				} else {
					resourceRangeMap[resource] = sets.New[string](targetNsName...)
				}
			}
		}
	}

	return admission.ValidationResponse(true, "")
}

func getNamespaceNameSets(nss *v1.NamespaceList) []string {
	var names []string
	for _, ns := range nss.Items {
		names = append(names, ns.Name)
	}
	return names
}

func validate(obj *ctrlmeshv1alpha1.ShardingConfig) error {

	if obj.Spec.Root != nil {
		err := validateRootConfig(obj.Spec.Root)
		if err != nil {
			return err
		}
	}

	if selector, err := metav1.LabelSelectorAsSelector(obj.Spec.Selector); err != nil {
		return fmt.Errorf("invalid selector: %v", err)
	} else if (selector.Empty() || selector.String() == "") && obj.Spec.Root == nil {
		return fmt.Errorf("invalid selector can not be empty")
	}

	if obj.Spec.Limits == nil {
		if obj.Spec.Root == nil {
			return fmt.Errorf("invalid nil limits")
		}
		return nil
	}
	resourceSet := sets.Set[string]{}
	for _, limits := range obj.Spec.Limits {
		for _, resourceGroup := range limits.RelatedResources {
			for _, resource := range resourceGroup.Resources {
				for _, api := range resourceGroup.APIGroups {
					if resourceSet.HasAny("*/"+resource, api+"/"+resource) {
						return fmt.Errorf("%s has conflict resource limits", resource)
					}
					resourceSet.Insert(api + "/" + resource)
				}
			}
		}
	}

	// leader election of operator pods
	if obj.Spec.Controller != nil {
		if obj.Spec.Controller.LeaderElectionName == "" {
			return fmt.Errorf("leaderElectionName for controller can not be empty")
		}
	}
	if obj.Spec.Webhook != nil {
		if obj.Spec.Webhook.CertDir == "" {
			return fmt.Errorf("certDir for webhook can not be empty")
		}
		if obj.Spec.Webhook.Port <= 0 {
			return fmt.Errorf("port for webhook must be bigger than 0")
		}
	}
	return nil
}

func validateRootConfig(root *ctrlmeshv1alpha1.ShardingConfigRoot) error {
	if root.Canary != nil {
		if len(root.Canary.InShardHash) == 0 && len(root.Canary.InNamespaces) == 0 {
			return fmt.Errorf("canary config must have at least one inShardHash or inNamespaces")
		}
		if root.Canary.Replicas == nil {
			return fmt.Errorf("canary shard replicas must not be nil")
		}
	}
	if root.Auto != nil {
		if root.Auto.EveryShardReplicas == 0 || root.Auto.ShardingSize == 0 {
			return fmt.Errorf("illegal root auto config")
		}
	}
	return nil
}

func validateUpdate(obj, oldObj *ctrlmeshv1alpha1.ShardingConfig) error {
	if !reflect.DeepEqual(obj.Spec.Selector, oldObj.Spec.Selector) {
		return fmt.Errorf("selector can not be modified")
	}
	return nil
}

var _ admission.DecoderInjector = &ValidatingHandler{}

func (h *ValidatingHandler) InjectClient(c client.Client) error {
	h.Client = c
	return nil
}

func (h *ValidatingHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
	return nil
}
