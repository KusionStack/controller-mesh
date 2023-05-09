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

package proto

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/gogo/protobuf/proto"
	"k8s.io/klog/v2"

	kridgeproto "github.com/KusionStack/kridge/pkg/apis/kridge/proto"
)

const (
	expectedSpecFilePath = "/kridge/expected-spec"
	currentSpecFilePath  = "/kridge/current-spec"

	testBlockLoadingFilePath = "/kridge/mock-proto-manage-failure"
)

type storage struct {
	expectedSpecFile *os.File
	currentSpecFile  *os.File
}

func newStorage() (*storage, error) {
	var err error
	s := &storage{}
	if err = s.mockFailure(); err != nil {
		// block here
		klog.Warningf("Block new storage: %v", err)
		ch := make(chan struct{})
		<-ch
	}
	s.expectedSpecFile, err = os.OpenFile(expectedSpecFilePath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	s.currentSpecFile, err = os.OpenFile(currentSpecFilePath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *storage) loadData() (expectedSpec, currentSpec *kridgeproto.ProxySpec, err error) {
	expectedSpecBytes, err := ioutil.ReadAll(s.expectedSpecFile)
	if err != nil {
		return nil, nil, err
	}
	currentSpecBytes, err := ioutil.ReadAll(s.currentSpecFile)
	if err != nil {
		return nil, nil, err
	}

	if len(expectedSpecBytes) > 0 {
		expectedSpec = &kridgeproto.ProxySpec{}
		if err = proto.Unmarshal(expectedSpecBytes, expectedSpec); err != nil {
			return nil, nil, err
		}
	}
	if len(currentSpecBytes) > 0 {
		currentSpec = &kridgeproto.ProxySpec{}
		if err = proto.Unmarshal(currentSpecBytes, currentSpec); err != nil {
			return nil, nil, err
		}
	}
	return
}

func (s *storage) writeExpectedSpec(spec *kridgeproto.ProxySpec) error {
	var err error
	if err = s.mockFailure(); err != nil {
		return err
	}
	b, err := proto.Marshal(spec)
	if err != nil {
		return err
	}
	_, err = s.expectedSpecFile.Write(b)
	return err
}

func (s *storage) writeCurrentSpec(spec *kridgeproto.ProxySpec) error {
	var err error
	if err = s.mockFailure(); err != nil {
		return err
	}
	b, err := proto.Marshal(spec)
	if err != nil {
		return err
	}
	_, err = s.currentSpecFile.Write(b)
	return err
}

func (s *storage) mockFailure() error {
	if _, err := os.Stat(testBlockLoadingFilePath); err == nil {
		return fmt.Errorf("mock failure for %s exists", testBlockLoadingFilePath)
	}
	return nil
}
