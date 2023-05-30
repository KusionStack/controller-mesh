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

package constants

const (
	ProxyUserID = 1359

	ProxyMetricsHealthPort = 5441
	ProxyApiserverPort     = 5443
	ProxyWebhookPort       = 5445
	ProxyGRPCPort          = 5447
	ProxyMetricsPort       = 5449
	ProxyManagerHealthPort = 5451

	ProxyIptablesPort = 15002
	PprofListenPort   = 5050

	DefaultShardingSize = 32

	ProxyIptablesPortFlag           = "proxy-metrics"
	ProxyMetricsHealthPortFlag      = "metrics-health-port"
	ProxyApiserverPortFlag          = "proxy-apiserver-port"
	ProxyWebhookPortFlag            = "proxy-webhook-port"
	ProxyGRPCPortFlag               = "grpc-port"
	ProxyIptablesFlag               = "tport"
	ProxyIptablesRuleConfigPathFlag = "rulecfgpath"
	ProxyIptablesRuleConfigFileFlag = "rulecfgfile"

	ProxyLeaderElectionNameFlag = "leader-election-name"
	ProxyWebhookServePortFlag   = "webhook-serve-port"
	ProxyWebhookCertDirFlag     = "webhook-cert-dir"

	EnablePProfFlag     = "enable-pprof"
	PprofListenPortFlag = "pprof-listen-port"

	InitContainerName               = "kridge-init"
	ProxyContainerName              = "kridge-proxy"
	ProxyIptablesRuleConfigPathName = "/rule_cfg"
	ProxyIptablesRuleConfigFileName = "whitelist.yaml"

	EnvInboundWebhookPort = "INBOUND_WEBHOOK_PORT"
	EnvPodName            = "POD_NAME"
	EnvPodNamespace       = "POD_NAMESPACE"
	EnvPodIP              = "POD_IP"
	EnvIPTable            = "ENABLE_IPTABLE_PROXY"
	EnvEnableWebHookProxy = "ENABLE_WEBHOOK_PROXY"

	EnvEnableSim = "ENABLE_SIM"

	EnvEnableCircuitBreaker          = "ENABLE_CIRCUIT_BREAKER"
	EnvEnableApiServerCircuitBreaker = "ENABLE_API_SERVER_BREAKER"
	EnvEnableRestCircuitBreaker      = "ENABLE_REST_BREAKER"
)

func AllProxySyncEnvKey() []string {
	keys := []string{
		EnvEnableSim,
		EnvIPTable,
		EnvEnableWebHookProxy,
		EnvEnableCircuitBreaker,
		EnvEnableApiServerCircuitBreaker,
		EnvEnableRestCircuitBreaker,
	}
	return keys
}
