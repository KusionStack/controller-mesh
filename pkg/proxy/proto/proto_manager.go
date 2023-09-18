/*
Copyright 2023 The KusionStack Authors.
Copyright 2021 The Kruise Authors.

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

package proto

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/gogo/protobuf/proto"
	"k8s.io/klog/v2"

	"github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh/constants"
	ctrlmeshproto "github.com/KusionStack/ctrlmesh/pkg/apis/ctrlmesh/proto"
	"github.com/KusionStack/ctrlmesh/pkg/utils"
)

var (
	selfInfo = &ctrlmeshproto.SelfInfo{Namespace: os.Getenv(constants.EnvPodNamespace), Name: os.Getenv(constants.EnvPodName)}
	onceInit sync.Once
)

type Client interface {
	Start(ctx context.Context) error
	GetSpecManager() *SpecManager
}

type SpecManager struct {
	sync.RWMutex
	storage           *storage
	reportTriggerChan chan struct{}

	expectedSpec *ctrlmeshproto.InternalSpec
	currentSpec  *ctrlmeshproto.InternalSpec
	unloadReason string

	leaderElectionState *ctrlmeshproto.LeaderElectionState
}

func newSpecManager(reportTriggerChan chan struct{}) (*SpecManager, error) {
	storage, err := newStorage()
	if err != nil {
		return nil, err
	}
	sm := &SpecManager{
		storage:           storage,
		reportTriggerChan: reportTriggerChan,
	}
	expectedSpec, currentSpec, err := sm.storage.loadData()
	if err != nil {
		klog.Errorf("Failed to load currentSpec, %v", err)
	}
	if currentSpec != nil {
		klog.Infof("Loaded currentSpec from storage: %v", utils.DumpJSON(currentSpec))
		sm.currentSpec = ctrlmeshproto.ConvertProtoSpecToInternal(currentSpec)
	}
	if expectedSpec != nil {
		klog.Infof("Loaded expectedSpec from storage: %v", utils.DumpJSON(expectedSpec))
		sm.UpdateSpec(expectedSpec)
	}
	return sm, nil
}

func (sm *SpecManager) UpdateLeaderElection(le *ctrlmeshproto.LeaderElectionState) {
	sm.Lock()
	defer sm.Unlock()
	oldLe := sm.leaderElectionState
	sm.leaderElectionState = le
	if !proto.Equal(oldLe, le) {
		sm.reportTriggerChan <- struct{}{}
	}
}

func (sm *SpecManager) UpdateSpec(spec *ctrlmeshproto.ProxySpec) {
	sm.expectedSpec = ctrlmeshproto.ConvertProtoSpecToInternal(spec)
	if err := sm.storage.writeExpectedSpec(sm.expectedSpec.ProxySpec); err != nil {
		panic(fmt.Errorf("failed to write expected spec for %v: %v", utils.DumpJSON(sm.expectedSpec.ProxySpec), err))
	}
	sm.Lock()
	defer sm.Unlock()
	if err := sm.checkLoadable(); err != nil {
		klog.Warningf("Check new spec is not loadable, because %v", err)
		sm.unloadReason = err.Error()
		return
	}
	sm.currentSpec = sm.expectedSpec
	if err := sm.storage.writeCurrentSpec(sm.currentSpec.ProxySpec); err != nil {
		panic(fmt.Errorf("failed to write current spec for %v: %v", utils.DumpJSON(sm.currentSpec.ProxySpec), err))
	}
}

func (sm *SpecManager) GetStatus() *ctrlmeshproto.ProxyStatus {
	sm.Lock()
	defer sm.Unlock()
	if sm.currentSpec == nil {
		return nil
	}
	return &ctrlmeshproto.ProxyStatus{
		MetaState: &ctrlmeshproto.MetaState{
			ExpectedHash:     sm.expectedSpec.Meta.Hash,
			CurrentHash:      sm.currentSpec.Meta.Hash,
			HashUnloadReason: sm.unloadReason,
		},
		LeaderElectionState: sm.leaderElectionState,
	}
}

func (sm *SpecManager) AcquireSpec() *ctrlmeshproto.InternalSpec {
	sm.RLock()
	return sm.currentSpec
}

func (sm *SpecManager) ReleaseSpec() {
	defer sm.RUnlock()
}

func (sm *SpecManager) checkLoadable() (err error) {
	if sm.currentSpec == nil {
		return nil
	}

	if sm.currentSpec.Meta.ShardName != sm.expectedSpec.Meta.ShardName {
		return fmt.Errorf("ShardingConfig changed from %s to %s",
			sm.currentSpec.Meta.ShardName, sm.expectedSpec.Meta.ShardName)
	}

	// TODO: Only Webhook Server Ep Changed
	if sm.currentSpec.Meta.Hash != sm.expectedSpec.Meta.Hash {
		return fmt.Errorf("SpecHase changed from %s to %s", sm.currentSpec.Meta.Hash, sm.expectedSpec.Meta.Hash)
	}

	return
}
