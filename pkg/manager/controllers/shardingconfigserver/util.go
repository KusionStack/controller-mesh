/*
Copyright 2023 The KusionStack Authors.
Modified from Kruise code, Copyright 2021 The Kruise Authors.

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

package shardingconfigserver

import (
	v1 "k8s.io/api/core/v1"

	kridgeproto "github.com/KusionStack/kridge/pkg/apis/kridge/proto"
	kridgev1alpha1 "github.com/KusionStack/kridge/pkg/apis/kridge/v1alpha1"
	"github.com/KusionStack/kridge/pkg/utils"
)

func getRunningContainers(pod *v1.Pod) []string {
	var runningContainers []string
	for _, cStatus := range pod.Status.ContainerStatuses {
		if cStatus.State.Running != nil {
			runningContainers = append(runningContainers, cStatus.Name)
		}
	}
	return runningContainers
}

func calculateSpecHash(spec *kridgeproto.ProxySpec) string {
	return utils.GetMD5Hash(utils.DumpJSON(kridgeproto.ProxySpec{
		Endpoints: spec.Endpoints,
		Limits:    spec.Limits,
	}))
}

func generateAPIResources(resources []kridgev1alpha1.ResourceGroup) []*kridgeproto.APIGroupResource {
	var ret []*kridgeproto.APIGroupResource
	for _, resource := range resources {
		ret = append(ret, &kridgeproto.APIGroupResource{ApiGroups: resource.APIGroups, Resources: resource.Resources})
	}
	return ret
}

func generateProtoConfig(cfg *kridgev1alpha1.ShardingConfig) *kridgeproto.ProxySpec {
	protoRoute := &kridgeproto.ProxySpec{}
	protoRoute.Limits = []*kridgeproto.Limit{}

	for _, limit := range cfg.Spec.Limits {
		resources := generateAPIResources(limit.RelatedResources)
		protoRoute.Limits = append(protoRoute.Limits, &kridgeproto.Limit{
			Resources:      resources,
			ObjectSelector: utils.DumpJSON(limit.Selector),
		})
	}
	protoRoute.Meta = &kridgeproto.SpecMeta{
		ShardName: cfg.Name,
		Hash:      utils.GetMD5Hash(utils.DumpJSON(protoRoute.Limits)),
	}

	//TODO: Get endpoints to support webhook router
	//protoRoute.Endpoints = []*kridgeproto.Endpoint{}

	return protoRoute
}
