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

const (
	EnvEnableWebhookServer     = "ENABLE_WEBHOOK_SERVER"
	EnvEnableCalculateRunnable = "ENABLE_CALCULATE_RUNNABLE"
	EnvTestMode                = "ENV_TEST_MODE"

	EnvGlobalSelector = "GLOBAL_SELECTOR"
	EnvWatchOnLimit   = "WATCH_ON_LIMIT"

	KdControlPrefix = "kridge.kusionstack.io/"

	KdShardingHashKey = "kridge.kusionstack.io/sharding-hash"
	KdControlKey      = "kridge.kusionstack.io/control"
	KdNamespaceKey    = "kridge.kusionstack.io/namespace"

	KdIgnoreWebhookKey   = "kridge.kusionstack.io/ignore-webhook"
	KdIgnoreValidateKey  = "kridge.kusionstack.io/ignore-validate"
	KdDefaultReplicasKey = "kridge.kusionstack.io/default-replicas"

	KdEnableProxyKey = "kridge.kusionstack.io/enable-proxy"

	ProtectFinalizer = "finalizer.kridge.kusionstack.io/protected"

	KdAutoShardingRootLabel = "kridge.kusionstack.io/auto-sharding-root"
	KdAutoShardingHashAnno  = "kridge.kusionstack.io/auto-sharding-hash"

	KdInRollingLabel      = "kridge.kusionstack.io/rolling"
	KdRollingStatusAnno   = "kridge.kusionstack.io/roll-status"
	KdRollingExpectedAnno = "kridge.kusionstack.io/roll-expected"

	KdDisableFakeKubeconfigArg = "kridge.kusionstack.io/disable-fake-kubeconfig-arg"
	KdDisableFakeKubeconfigEnv = "kridge.kusionstack.io/disable-fake-kubeconfig-env"

	KdSharedLogPathAnnoKey = "kridge.kusionstack.io/log-path"
	KdSharedLogVolumeKey   = "kridge.kusionstack.io/log-volume"

	KdWatchOnLimitKey = "kridge.kusionstack.io/watching"

	ShardingConfigMapName = "kridge-sharding-config"

	KdWebhookEnvConfigAnno       = "kridge.kusionstack.io/env-sync"
	KdEnvInjectAnno              = "kridge.kusionstack.io/env-inject"
	KdProxyKubeConfigVolume      = "kridge.kusionstack.io/kubeconfig-volume"
	KdProxyContainerResourceAnno = "kridge.kusionstack.io/proxy-resource"
)
