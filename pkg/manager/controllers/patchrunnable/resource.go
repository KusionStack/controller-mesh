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

package patchrunnable

import (
	"context"
	"flag"
	"reflect"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh"
	"github.com/KusionStack/controller-mesh/pkg/utils"
)

const (
	resourceConfigKey = "resource-config"
)

var (
	configMapName      = flag.String("resource-configmap-name", "ctrlmesh-sharding-resource", "Define some resources to sync ctrlmesh label")
	configMapNamespace = utils.GetNamespace()
)

func LoadGroupVersionKind(c client.Client, discoveryClient *discovery.DiscoveryClient) ([]schema.GroupVersionKind, error) {
	var result []schema.GroupVersionKind
	cm := &v1.ConfigMap{}
	if err := c.Get(context.TODO(), types.NamespacedName{Name: *configMapName, Namespace: configMapNamespace}, cm); err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("ConfigMap %s/%s not found", configMapNamespace, *configMapName)
			return result, nil
		}
		return result, err
	}
	val, find := cm.Data[resourceConfigKey]
	if !find {
		klog.Infof("cannot find resource config in configMap data")
		return result, nil
	}
	resourceConfig := &ResourceConfig{}
	if err := yaml.Unmarshal([]byte(val), resourceConfig); err != nil {
		return result, err
	}

	gvkMap := map[string]schema.GroupVersionKind{}
	for gv, kinds := range resourceConfig.GroupVersionKinds {
		groupVersion, err := schema.ParseGroupVersion(gv)
		if err != nil {
			klog.Errorf("fail to parse group version %s, %v", gv, err)
			continue
		}
		for _, kind := range kinds {
			if kind != "*" {
				groupVersionKind := groupVersion.WithKind(kind)
				gvkMap[groupVersion.String()+"/"+kind] = groupVersionKind
				continue
			}
			resources, err := discoveryClient.ServerResourcesForGroupVersion(gv)
			if err != nil {
				klog.Errorf("fail to get resources by group version %s, %v", gv, err)
				break
			}
			for _, resource := range resources.APIResources {
				groupVersionKind := groupVersion.WithKind(resource.Kind)
				gvkMap[groupVersion.String()+"/"+resource.Kind] = groupVersionKind
			}
			break
		}
	}
	for _, value := range gvkMap {
		result = append(result, value)
	}
	return result, nil
}

type ResourceConfig struct {
	GroupVersionKinds map[string][]string `json:"groupVersionKinds"`
}

func getShardingLabel(obj client.Object) map[string]string {
	shardingLabels := map[string]string{}
	if beNil(obj) || obj.GetLabels() == nil || !ctrlmesh.IsControlledByMesh(obj.GetLabels()) {
		return shardingLabels
	}
	lbs := obj.GetLabels()
	for _, k := range ctrlmesh.ShouldSyncLabels() {
		if val, ok := lbs[k]; ok {
			shardingLabels[k] = val
		}
	}
	return shardingLabels
}

func beNil(a interface{}) bool {
	if a == nil {
		return true
	}
	switch reflect.TypeOf(a).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflect.ValueOf(a).IsNil()
	default:
		return false
	}
}
