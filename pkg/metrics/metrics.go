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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	ReconcileSubSystem = "reconcile"

	CurrentCircuitBreakerCount = "current_circuit_breaker_count"

	ShardingSubSystem = "sharding"
	WebhookSubSystem  = "webhook"

	ShardingNamespaceCount      = "namespace_count"
	ShardingPodCount            = "pod_count"
	ResourceWebhookRequestCount = "mutating_webhook_resource_count"
)

var (
	currentCircuitBreakerCounts = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: ReconcileSubSystem,
		Name:      CurrentCircuitBreakerCount,
		Help:      "count breaker",
	}, []string{"breaker_ip", "breaker_pod_name", "limiter_name"})

	shardingPodCounts = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: ShardingSubSystem,
		Name:      ShardingPodCount,
		Help:      "sharding pods counter",
	}, []string{"sharding_size", "id"})

	shardingNamespaceCounts = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: ShardingSubSystem,
		Name:      ShardingNamespaceCount,
		Help:      "sharding namespace counter",
	}, []string{"sharding_size", "id"})

	webhookResourceCounts = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: WebhookSubSystem,
		Name:      ResourceWebhookRequestCount,
		Help:      "resource mutating handler requests counter",
	}, []string{"resource", "verb", "user"})
)

func init() {
	metrics.Registry.MustRegister(
		currentCircuitBreakerCounts,
		shardingPodCounts,
		shardingNamespaceCounts,
		webhookResourceCounts,
	)
}

func NewCurrentCircuitBreakerCounterMetrics(ip, podName, limiterName string) prometheus.Gauge {
	return currentCircuitBreakerCounts.WithLabelValues(ip, podName, limiterName)
}

func NewShardingPodCountMetrics(size, id string) prometheus.Gauge {
	return shardingPodCounts.WithLabelValues(size, id)
}

func NewShardingNamespaceCountMetrics(size, id string) prometheus.Gauge {
	return shardingNamespaceCounts.WithLabelValues(size, id)
}

func NewResourceWebhookRequestCountMetrics(resource, verb, user string) prometheus.Gauge {
	return webhookResourceCounts.WithLabelValues(resource, verb, user)
}
