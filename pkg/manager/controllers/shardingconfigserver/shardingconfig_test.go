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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
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

func TestGetChild(t *testing.T) {
	canaryRep := 1
	getChild(&ctrlmeshv1alpha1.ShardingConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "root", Namespace: "test"},
		Spec: ctrlmeshv1alpha1.ShardingConfigSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"nginx": "v1",
				},
			},
			Controller: &ctrlmeshv1alpha1.ShardingConfigControllerConfiguration{
				LeaderElectionName: "leader-election-name",
			},
			Root: &ctrlmeshv1alpha1.ShardingConfigRoot{
				Prefix:            "test",
				TargetStatefulSet: "sts-name",
				Canary: &ctrlmeshv1alpha1.CanaryConfig{
					Replicas:     &canaryRep,
					InNamespaces: []string{"ns1", "ns2"},
				},
				Auto: &ctrlmeshv1alpha1.AutoConfig{
					ShardingSize:       2,
					EveryShardReplicas: 2,
				},
				ResourceSelector: []ctrlmeshv1alpha1.ObjectLimiter{
					{
						RelatedResources: []ctrlmeshv1alpha1.ResourceGroup{
							{
								Resources: []string{"pods"},
								APIGroups: []string{"*"},
							},
						},
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test": "test",
							},
						},
					},
				},
			},
		},
	})
}
