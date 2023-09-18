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
	KdControlPrefix                 = "ctrlmesh.kusionstack.io/"
	KdShardHashKey                  = "ctrlmesh.kusionstack.io/shard-hash"
	KdControlKey                    = "ctrlmesh.kusionstack.io/control"
	KdNamespaceKey                  = "ctrlmesh.kusionstack.io/namespace"
	KdIgnoreWebhookLabel            = "ctrlmesh.kusionstack.io/ignore-webhook"
	KdIgnoreValidateLabel           = "ctrlmesh.kusionstack.io/ignore-validate"
	KdDefaultReplicasLabel          = "ctrlmesh.kusionstack.io/default-replicas"
	KdEnableProxyLabel              = "ctrlmesh.kusionstack.io/enable-proxy"
	KdAutoShardingRootLabel         = "ctrlmesh.kusionstack.io/auto-sharding-root"
	KdInRollingLabel                = "ctrlmesh.kusionstack.io/rolling"
	KdDisableFakeKubeconfigArgLabel = "ctrlmesh.kusionstack.io/disable-fake-kubeconfig-arg"
	KdDisableFakeKubeconfigEnvLabel = "ctrlmesh.kusionstack.io/disable-fake-kubeconfig-env"
	KdSharedLogVolumeLabel          = "ctrlmesh.kusionstack.io/log-volume"
	KdWatchOnLimitLabel             = "ctrlmesh.kusionstack.io/watching"
	KdProxyKubeConfigVolumeLabel    = "ctrlmesh.kusionstack.io/kubeconfig-volume"
)

// Annotations
const (
	KdAutoShardingHashAnno       = "ctrlmesh.kusionstack.io/auto-sharding-hash"
	KdRollingStatusAnno          = "ctrlmesh.kusionstack.io/roll-status"
	KdRollingExpectedAnno        = "ctrlmesh.kusionstack.io/roll-expected"
	KdSharedLogPathAnno          = "ctrlmesh.kusionstack.io/log-path"
	KdWebhookEnvConfigAnno       = "ctrlmesh.kusionstack.io/env-sync"
	KdEnvInjectAnno              = "ctrlmesh.kusionstack.io/env-inject"
	KdProxyContainerResourceAnno = "ctrlmesh.kusionstack.io/proxy-resource"
)

// Finalizer
const (
	ProtectFinalizer = "finalizer.ctrlmesh.kusionstack.io/protected"
)

// Name
const (
	ShardingConfigMapName = "ctrlmesh-sharding-config"
)
