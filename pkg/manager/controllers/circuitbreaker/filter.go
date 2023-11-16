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

package circuitbreaker

import (
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/utils/conv"
	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
)

type BreakerPredicate struct {
}

// Create returns true if the Create event should be processed
func (b *BreakerPredicate) Create(event.CreateEvent) bool {
	return true
}

// Delete returns true if the Delete event should be processed
func (b *BreakerPredicate) Delete(e event.DeleteEvent) bool {
	return true
}

// Update returns true if the Update event should be processed
func (b *BreakerPredicate) Update(e event.UpdateEvent) bool {
	oldCB := e.ObjectOld.(*ctrlmeshv1alpha1.CircuitBreaker)
	newCB := e.ObjectNew.(*ctrlmeshv1alpha1.CircuitBreaker)
	if newCB.DeletionTimestamp != nil || len(oldCB.Finalizers) != len(newCB.Finalizers) {
		return true
	}
	oldProtoCB := conv.ConvertCircuitBreaker(oldCB)
	newProtoCB := conv.ConvertCircuitBreaker(newCB)
	return oldProtoCB.ConfigHash != newProtoCB.ConfigHash
}

// Generic returns true if the Generic event should be processed
func (b *BreakerPredicate) Generic(event.GenericEvent) bool {
	return true
}
