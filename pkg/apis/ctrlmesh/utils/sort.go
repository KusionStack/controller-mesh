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

package utils

import (
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
)

type ShardingConfigs []*ctrlmeshv1alpha1.ShardingConfig

func (e ShardingConfigs) Len() int      { return len(e) }
func (e ShardingConfigs) Swap(i, j int) { e[i], e[j] = e[j], e[i] }
func (e ShardingConfigs) Less(i, j int) bool {
	return e[i].Name < e[j].Name
}
