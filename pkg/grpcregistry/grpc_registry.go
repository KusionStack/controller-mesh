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

package grpcregistry

import (
	"context"
	"flag"
	"fmt"
	"net"
	"sync"

	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	leaderElectionRegistry = &grpcRegistry{
		needLeaderElection: true,
		registry:           make(map[string]RegisterFunc),
		port:               flag.Int("grpc-leader-election-port", 8071, "Port for leader-election grpc server."),
	}
	nonLeaderElectionRegistry = &grpcRegistry{
		needLeaderElection: false,
		registry:           make(map[string]RegisterFunc),
		port:               flag.Int("grpc-non-leader-election-port", 8072, "Port for non-leader-election grpc server."),
	}
	globalManager manager.Manager
)

type RegisterFunc = func(RegisterOptions)

type RegisterOptions struct {
	GrpcServer *grpc.Server
	Mgr        manager.Manager
	Ctx        context.Context
}

type grpcRegistry struct {
	needLeaderElection bool
	registry           map[string]RegisterFunc
	port               *int

	mu      sync.Mutex
	started bool
}

func (r *grpcRegistry) NeedLeaderElection() bool {
	return r.needLeaderElection
}

func (r *grpcRegistry) Start(ctx context.Context) error {
	if r.port == nil || *r.port == 0 {
		klog.Warningf("Skip to start gRPC server for registry with leaderElection=%v gets port=0", r.needLeaderElection)
		<-ctx.Done()
		return nil
	}
	r.mu.Lock()
	r.started = true
	r.mu.Unlock()

	if len(r.registry) == 0 {
		klog.Warningf("Skip to start gRPC server for registry with leaderElection=%v has no item registered", r.needLeaderElection)
		<-ctx.Done()
		return nil
	}

	var opts []grpc.ServerOption
	// Set max message size in bytes is 64MB
	opts = append(opts, grpc.MaxRecvMsgSize(1024*1024*64))
	grpcServer := grpc.NewServer(opts...)

	// register all servers
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, rf := range r.registry {
		rf(RegisterOptions{GrpcServer: grpcServer, Mgr: globalManager, Ctx: subCtx})
	}

	addr := fmt.Sprintf(":%d", *r.port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("error listening grpc server address %s: %v", addr, err)
	}
	klog.Infof("Start listening gRPC server with leaderElection=%v on %s", r.needLeaderElection, addr)
	go func() {
		if err = grpcServer.Serve(lis); err != nil {
			klog.Errorf("serve gRPC error %v", err)
		}
	}()

	<-ctx.Done()
	return nil
}

func (r *grpcRegistry) register(name string, rf RegisterFunc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return fmt.Errorf("can not register gRPC function to a started registry")
	}
	r.registry[name] = rf
	return nil
}

func Register(name string, needLeaderElection bool, rf RegisterFunc) error {
	if needLeaderElection {
		return leaderElectionRegistry.register(name, rf)
	}
	return nonLeaderElectionRegistry.register(name, rf)
}

func SetupWithManager(mgr manager.Manager) error {
	globalManager = mgr
	if err := mgr.Add(leaderElectionRegistry); err != nil {
		return err
	}
	if err := mgr.Add(nonLeaderElectionRegistry); err != nil {
		return err
	}
	return nil
}

func GetGrpcPorts() (leaderElectionPort int, nonLeaderElectionPort int) {
	if leaderElectionRegistry.port != nil {
		leaderElectionPort = *leaderElectionRegistry.port
	}
	if nonLeaderElectionRegistry.port != nil {
		nonLeaderElectionPort = *nonLeaderElectionRegistry.port
	}
	return
}
