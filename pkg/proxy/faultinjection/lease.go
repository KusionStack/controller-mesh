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

package faultinjection

import (
	"context"
	"sync"
	"time"

	"k8s.io/client-go/util/workqueue"

	ctrlmeshproto "github.com/KusionStack/controller-mesh/pkg/apis/ctrlmesh/proto"
)

type lease struct {
	mu         sync.RWMutex
	stateQueue workqueue.DelayingInterface
	stateSet   map[string]*state

	ctx context.Context
}

func newFaultInjectionLease(ctx context.Context) *lease {
	result := &lease{
		stateQueue: workqueue.NewDelayingQueue(),
		stateSet:   map[string]*state{},
		ctx:        ctx,
	}
	go result.processingLoop()
	return result
}

func (l *lease) registerState(st *state) {
	if st.state == ctrlmeshproto.FaultInjectionState_STATEOPENED {
		logger.Info("register state", "state", st.key)
		l.mu.Lock()
		defer l.mu.Unlock()
		if _, ok := l.stateSet[st.key]; !ok {
			if st.recoverAt != nil {
				l.stateSet[st.key] = st
				d := time.Until(st.recoverAt.Time)
				l.stateQueue.AddAfter(st, d)
			}
		}
	}
}

func (l *lease) processingLoop() {
	go func() {
		<-l.ctx.Done()
		l.stateQueue.ShutDown()
	}()
	for {
		obj, shutdown := l.stateQueue.Get()
		if shutdown {
			return
		}
		st := obj.(*state)
		_, _, recoverAt := st.read()
		if recoverAt != nil && time.Now().Before(recoverAt.Time) {
			// recover time changed, requeue
			l.stateQueue.AddAfter(st, time.Until(recoverAt.Time))
		} else {
			l.mu.Lock()
			delete(l.stateSet, st.key)
			l.mu.Unlock()
		}
		l.stateQueue.Done(st)
	}
}
