/*
Copyright 2023 The KusionStack Authors.
Copyright 2021 The Kruise Authors.

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

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/controller-mesh/pkg/utils"
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

func calculateSpecHash(spec *ctrlmeshproto.ProxySpec) string {
	return utils.GetMD5Hash(utils.DumpJSON(ctrlmeshproto.ProxySpec{
		Endpoints: spec.Endpoints,
		Limits:    spec.Limits,
	}))
}

func generateAPIResources(resources []ctrlmeshv1alpha1.ResourceGroup) []*ctrlmeshproto.APIGroupResource {
	var ret []*ctrlmeshproto.APIGroupResource
	for _, resource := range resources {
		ret = append(ret, &ctrlmeshproto.APIGroupResource{ApiGroups: resource.APIGroups, Resources: resource.Resources})
	}
	return ret
}

func generateProtoConfig(cfg *ctrlmeshv1alpha1.ShardingConfig) *ctrlmeshproto.ProxySpec {
	protoRoute := &ctrlmeshproto.ProxySpec{}
	protoRoute.Limits = []*ctrlmeshproto.Limit{}

	for _, limit := range cfg.Spec.Limits {
		resources := generateAPIResources(limit.RelatedResources)
		protoRoute.Limits = append(protoRoute.Limits, &ctrlmeshproto.Limit{
			Resources:      resources,
			ObjectSelector: utils.DumpJSON(limit.Selector),
		})
	}
	protoRoute.Meta = &ctrlmeshproto.SpecMeta{
		ShardName: cfg.Name,
		Hash:      utils.GetMD5Hash(utils.DumpJSON(protoRoute.Limits)),
	}

	//TODO: Get endpoints to support webhook router
	//protoRoute.Endpoints = []*ctrlmeshproto.Endpoint{}

	return protoRoute
}
