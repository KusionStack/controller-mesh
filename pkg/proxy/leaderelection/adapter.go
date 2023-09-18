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

package leaderelection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/KusionStack/ctrlmesh/pkg/utils"
)

var (
	runtimeScheme = runtime.NewScheme()
	runtimeCodec  = serializer.NewCodecFactory(runtimeScheme)
)

func init() {
	utilruntime.Must(v1.AddToScheme(runtimeScheme))
	utilruntime.Must(coordinationv1.AddToScheme(runtimeScheme))
}

func getSerializer(resp *http.Request) (runtime.Serializer, error) {
	contentType := resp.Header.Get("Content-Type")
	if len(contentType) == 0 {
		return nil, fmt.Errorf("no mediaType in request %s", utils.DumpJSON(resp.Header))
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, fmt.Errorf("invalid mediaType %s in response %s", mediaType, utils.DumpJSON(resp.Header))
	}
	for _, info := range runtimeCodec.SupportedMediaTypes() {
		if info.MediaType == mediaType {
			return info.Serializer, nil
		}
	}
	return nil, fmt.Errorf("not found supported serializer for mediaType %v", mediaType)
}

type adapter interface {
	DecodeFrom(*http.Request) error
	GetHoldIdentity() (string, bool)
	GetName() string
	SetName(name string)
	EncodeInto(*http.Request)
}

type objectAdapter struct {
	runtimeObj runtime.Object
	metaObj    metav1.Object
}

func newObjectAdapter(obj runtime.Object) *objectAdapter {
	return &objectAdapter{runtimeObj: obj, metaObj: obj.(metav1.Object)}
}

func (oa *objectAdapter) DecodeFrom(r *http.Request) error {
	ser, err := getSerializer(r)
	if err != nil {
		return err
	}
	return decodeObject(ser, r.Body, oa.runtimeObj)
}

func (oa *objectAdapter) GetHoldIdentity() (string, bool) {
	recordStr, ok := oa.metaObj.GetAnnotations()[resourcelock.LeaderElectionRecordAnnotationKey]
	if !ok {
		return "", false
	}
	lr := resourcelock.LeaderElectionRecord{}
	if err := json.Unmarshal([]byte(recordStr), &lr); err != nil {
		return "", true
	}
	return lr.HolderIdentity, true
}

func (oa *objectAdapter) GetName() string {
	return oa.metaObj.GetName()
}

func (oa *objectAdapter) SetName(name string) {
	oa.metaObj.SetName(name)
}

func (oa *objectAdapter) EncodeInto(r *http.Request) {
	var length int
	ser, _ := getSerializer(r)
	r.Body, length = encodeObject(ser, oa.runtimeObj)
	r.Header.Set("Content-Length", strconv.Itoa(length))
	r.ContentLength = int64(length)
}

type leaseAdapter struct {
	lease *coordinationv1.Lease
}

func newLeaseAdapter() *leaseAdapter {
	return &leaseAdapter{lease: &coordinationv1.Lease{}}
}

func (la *leaseAdapter) DecodeFrom(r *http.Request) error {
	ser, err := getSerializer(r)
	if err != nil {
		return err
	}
	return decodeObject(ser, r.Body, la.lease)
}

func (la *leaseAdapter) GetHoldIdentity() (string, bool) {
	if la.lease.Spec.HolderIdentity == nil {
		return "", false
	}
	return *la.lease.Spec.HolderIdentity, true
}

func (la *leaseAdapter) GetName() string {
	return la.lease.Name
}

func (la *leaseAdapter) SetName(name string) {
	la.lease.Name = name
}

func (la *leaseAdapter) EncodeInto(r *http.Request) {
	var length int
	ser, _ := getSerializer(r)
	r.Body, length = encodeObject(ser, la.lease)
	r.Header.Set("Content-Length", strconv.Itoa(length))
	r.ContentLength = int64(length)
}

func decodeObject(ser runtime.Serializer, body io.ReadCloser, obj runtime.Object) error {
	if body == nil {
		return fmt.Errorf("body is empty")
	}
	bodyBytes, err := io.ReadAll(body)
	body.Close()
	if err != nil {
		return fmt.Errorf("failed to read the body: %v", err)
	}
	if _, _, err := ser.Decode(bodyBytes, nil, obj); err != nil {
		return fmt.Errorf("unabled to decode the response: %v", err)
	}
	return nil
}

func encodeObject(ser runtime.Serializer, obj runtime.Object) (io.ReadCloser, int) {
	buf := &bytes.Buffer{}
	_ = ser.Encode(obj, buf)
	return io.NopCloser(buf), buf.Len()
}
