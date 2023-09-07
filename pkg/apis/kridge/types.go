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

package kridge

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
	KdControlPrefix                 = "kridge.kusionstack.io/"
	KdShardHashKey                  = "kridge.kusionstack.io/shard-hash"
	KdControlKey                    = "kridge.kusionstack.io/control"
	KdNamespaceKey                  = "kridge.kusionstack.io/namespace"
	KdIgnoreWebhookLabel            = "kridge.kusionstack.io/ignore-webhook"
	KdIgnoreValidateLabel           = "kridge.kusionstack.io/ignore-validate"
	KdDefaultReplicasLabel          = "kridge.kusionstack.io/default-replicas"
	KdEnableProxyLabel              = "kridge.kusionstack.io/enable-proxy"
	KdAutoShardingRootLabel         = "kridge.kusionstack.io/auto-sharding-root"
	KdInRollingLabel                = "kridge.kusionstack.io/rolling"
	KdDisableFakeKubeconfigArgLabel = "kridge.kusionstack.io/disable-fake-kubeconfig-arg"
	KdDisableFakeKubeconfigEnvLabel = "kridge.kusionstack.io/disable-fake-kubeconfig-env"
	KdSharedLogVolumeLabel          = "kridge.kusionstack.io/log-volume"
	KdWatchOnLimitLabel             = "kridge.kusionstack.io/watching"
	KdProxyKubeConfigVolumeLabel    = "kridge.kusionstack.io/kubeconfig-volume"
)

// Annotations
const (
	KdAutoShardingHashAnno       = "kridge.kusionstack.io/auto-sharding-hash"
	KdRollingStatusAnno          = "kridge.kusionstack.io/roll-status"
	KdRollingExpectedAnno        = "kridge.kusionstack.io/roll-expected"
	KdSharedLogPathAnno          = "kridge.kusionstack.io/log-path"
	KdWebhookEnvConfigAnno       = "kridge.kusionstack.io/env-sync"
	KdEnvInjectAnno              = "kridge.kusionstack.io/env-inject"
	KdProxyContainerResourceAnno = "kridge.kusionstack.io/proxy-resource"
)

// Finalizer
const (
	ProtectFinalizer = "finalizer.kridge.kusionstack.io/protected"
)

// Name
const (
	ShardingConfigMapName = "kridge-sharding-config"
)
