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

package shardingconfigserver

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var _ = Describe("ShardingConfig controller", func() {
	It("set controller", func() {
		mgr, err := manager.New(cfg, manager.Options{
			MetricsBindAddress: "0",
		})
		Expect(err).Should(BeNil())

		err = (&ShardingConfigReconciler{
			Client: mgr.GetClient(),
		}).SetupWithManager(mgr)

		Expect(err).Should(BeNil())
	})
})

func TestShardingConfigController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ShardingConfig controller test")
}
