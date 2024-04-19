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

package ctrlmesh

import (
	"flag"
)

// Environments
const (
	EnvEnableWebhookServer     = "ENABLE_WEBHOOK_SERVER"
	EnvEnableCalculateRunnable = "ENABLE_CALCULATE_RUNNABLE"
	EnvTestMode                = "ENV_TEST_MODE"
	EnvGlobalSelector          = "GLOBAL_SELECTOR"
	EnvWatchOnLimit            = "WATCH_ON_LIMIT"
)

// Labels
const (
	CtrlmeshControlPrefix        = "ctrlmesh.kusionstack.io/"
	CtrlmeshIgnoreWebhookLabel   = "ctrlmesh.kusionstack.io/ignore-webhook"
	CtrlmeshIgnoreValidateLabel  = "ctrlmesh.kusionstack.io/ignore-validate"
	CtrlmeshDefaultReplicasLabel = "ctrlmesh.kusionstack.io/default-replicas"
	CtrlmeshEnableProxyLabel     = "ctrlmesh.kusionstack.io/enable-proxy"
	CtrlmeshEnableIptableMode    = "ctrlmesh.kusionstack.io/enable-iptables"

	CtrlmeshAutoShardingRootLabel         = "ctrlmesh.kusionstack.io/auto-sharding-root"
	CtrlmeshInRollingLabel                = "ctrlmesh.kusionstack.io/rolling"
	CtrlmeshDisableFakeKubeconfigArgLabel = "ctrlmesh.kusionstack.io/disable-fake-kubeconfig-arg"
	CtrlmeshDisableFakeKubeconfigEnvLabel = "ctrlmesh.kusionstack.io/disable-fake-kubeconfig-env"
	CtrlmeshSharedLogVolumeLabel          = "ctrlmesh.kusionstack.io/log-volume"
	CtrlmeshWatchOnLimitLabel             = "ctrlmesh.kusionstack.io/watching"
	CtrlmeshProxyKubeConfigVolumeLabel    = "ctrlmesh.kusionstack.io/kubeconfig-volume"

	CtrlmeshCircuitBreakerDisableKey = "circuitbreaker.ctrlmesh.kusionstack.io/disable"
	CtrlmeshFaultInjectionDisableKey = "faultinjection.ctrlmesh.kusionstack.io/disable"
)

// Annotations
const (
	CtrlmeshAutoShardingHashAnno       = "ctrlmesh.kusionstack.io/auto-sharding-hash"
	CtrlmeshRollingStatusAnno          = "ctrlmesh.kusionstack.io/roll-status"
	CtrlmeshRollingExpectedAnno        = "ctrlmesh.kusionstack.io/roll-expected"
	CtrlmeshSharedLogPathAnno          = "ctrlmesh.kusionstack.io/log-path"
	CtrlmeshWebhookEnvConfigAnno       = "ctrlmesh.kusionstack.io/env-sync"
	CtrlmeshEnvInjectAnno              = "ctrlmesh.kusionstack.io/env-inject"
	CtrlmeshProxyContainerResourceAnno = "ctrlmesh.kusionstack.io/proxy-resource"
)

// Finalizers
const (
	ProtectFinalizer = "finalizer.ctrlmesh.kusionstack.io/protected"
)

var (
	shardHashLabel   string
	meshControlLabel string
	namespaceLabel   string
)

func init() {
	flag.StringVar(&shardHashLabel, "label-shard-hash", "ctrlmesh.kusionstack.io/shard-hash", "The sharding hash label.")
	flag.StringVar(&meshControlLabel, "label-mesh-control", "ctrlmesh.kusionstack.io/control", "The controller mesh control label.")
	flag.StringVar(&namespaceLabel, "label-namespace", "ctrlmesh.kusionstack.io/namespace", "The namespace label.")
}

func ShardHashLabel() string {
	return shardHashLabel
}

func MeshControlLabel() string {
	return meshControlLabel
}

func NamespaceShardLabel() string {
	return namespaceLabel
}

func IsControlledByMesh(labels map[string]string) bool {
	if labels == nil {
		return false
	}
	val, ok := labels[MeshControlLabel()]
	return ok && val == "true"
}

func ShouldSyncLabels() []string {
	return []string{
		ShardHashLabel(),
		MeshControlLabel(),
		NamespaceShardLabel(),
	}
}
