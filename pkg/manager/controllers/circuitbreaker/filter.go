package circuitbreaker

import (
	"sigs.k8s.io/controller-runtime/pkg/event"

	ctrlmeshv1alpha1 "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/v1alpha1"
	"github.com/KusionStack/controller-mesh/pkg/utils/conv"
)

type BreakerPredicate struct {
}

// Create returns true if the Create event should be processed
func (b *BreakerPredicate) Create(event.CreateEvent) bool {
	return true
}

// Delete returns true if the Delete event should be processed
func (b *BreakerPredicate) Delete(e event.DeleteEvent) bool {
	cb := e.Object.(*ctrlmeshv1alpha1.CircuitBreaker)
	protoCB := conv.ConvertCircuitBreaker(cb)
	defaultHashCache.DeleteHash(protoCB.ConfigHash)
	return true
}

// Update returns true if the Update event should be processed
func (b *BreakerPredicate) Update(e event.UpdateEvent) bool {
	oldCB := e.ObjectOld.(*ctrlmeshv1alpha1.CircuitBreaker)
	newCB := e.ObjectNew.(*ctrlmeshv1alpha1.CircuitBreaker)
	oldProtoCB := conv.ConvertCircuitBreaker(oldCB)
	newProtoCB := conv.ConvertCircuitBreaker(newCB)
	if oldProtoCB.ConfigHash != newProtoCB.ConfigHash {
		defaultHashCache.DeleteHash(oldProtoCB.ConfigHash)
		return true
	}
	if newCB.DeletionTimestamp != nil {
		return true
	}
	return false
}

// Generic returns true if the Generic event should be processed
func (b *BreakerPredicate) Generic(event.GenericEvent) bool {
	return true
}
