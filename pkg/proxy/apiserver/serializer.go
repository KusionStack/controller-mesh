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

package apiserver

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"

	"golang.org/x/sync/semaphore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured/unstructuredscheme"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"

	"github.com/KusionStack/ctrlmesh/pkg/utils"
	"github.com/KusionStack/ctrlmesh/pkg/utils/pool"
)

var (
	hugeListSemaphore   = semaphore.NewWeighted(3)
	normalListSemaphore = semaphore.NewWeighted(10)
)

var (
	builtInScheme = runtime.NewScheme()

	typedCodec        = serializer.NewCodecFactory(builtInScheme)
	unstructuredCodec = unstructuredscheme.NewUnstructuredNegotiatedSerializer()
	metaNegotiator    = runtime.NewClientNegotiator(typedCodec, schema.GroupVersion{Version: "v1"})

	ListCount = 0
	//WatchCount = 0
)

func init() {
	utilruntime.Must(metav1.AddMetaToScheme(builtInScheme))
	metav1.AddToGroupVersion(builtInScheme, schema.GroupVersion{Version: "v1"})
	utilruntime.Must(clientgoscheme.AddToScheme(builtInScheme))
}

func newResponseSerializer(resp *http.Response, apiResource *metav1.APIResource, reqInfo *request.RequestInfo, isStreaming bool) (*responseSerializer, error) {
	contentType := resp.Header.Get("Content-Type")
	if len(contentType) == 0 {
		return nil, fmt.Errorf("no mediaType in response %s", utils.DumpJSON(resp.Header))
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, fmt.Errorf("invalid mediaType %s in response %s", mediaType, utils.DumpJSON(resp.Header))
	}

	var obj runtime.Object
	var negotiator runtime.ClientNegotiator
	switch mediaType {
	case runtime.ContentTypeJSON, runtime.ContentTypeYAML:
		obj = &unstructured.Unstructured{}
		negotiator = runtime.NewClientNegotiator(unstructuredCodec, schema.GroupVersion{Group: apiResource.Group, Version: apiResource.Version})
	case runtime.ContentTypeProtobuf:
		gvk := schema.GroupVersionKind{Group: apiResource.Group, Version: apiResource.Version, Kind: apiResource.Kind}
		if !isStreaming {
			gvk.Kind = apiResource.Kind + "List"
		}
		if obj, err = builtInScheme.New(gvk); err != nil {
			return nil, fmt.Errorf("error new object for resource %v with mediaType=%s from built-in scheme: %v", mediaType, gvk, err)
		}
		negotiator = runtime.NewClientNegotiator(typedCodec, schema.GroupVersion{Group: apiResource.Group, Version: apiResource.Version})
	default:
		return nil, fmt.Errorf("unknown mediaType %s", mediaType)
	}

	respSerializer := &responseSerializer{
		apiResource: apiResource,
		reqInfo:     reqInfo,
		object:      obj,
	}
	if respSerializer.decoder, err = negotiator.Decoder(mediaType, params); err != nil {
		return nil, fmt.Errorf("error new decoder for contentType %s: %v", contentType, err)
	}
	if respSerializer.encoder, err = negotiator.Encoder(mediaType, params); err != nil {
		return nil, fmt.Errorf("error new encoder for contentType %s: %v", contentType, err)
	}
	if isStreaming {
		_, respSerializer.streamSerializer, respSerializer.streamFramer, err = metaNegotiator.StreamDecoder(mediaType, params)
		if err != nil {
			return nil, fmt.Errorf("error new stream decoder for contentType %s: %v", contentType, err)
		}
	}

	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		respSerializer.gzipReader = pool.GzipReaderPool.Get().(*gzip.Reader)
		respSerializer.gzipWriter = pool.GzipWriterPool.Get().(*gzip.Writer)
	}

	bodyReader := resp.Body
	if respSerializer.gzipReader != nil {
		if err := respSerializer.gzipReader.Reset(bodyReader); err != nil {
			klog.Error("Reset body reader error %v", err)
		}
		bodyReader = respSerializer.gzipReader
	}
	if isStreaming {
		bodyReader = respSerializer.streamFramer.NewFrameReader(bodyReader)
	}
	respSerializer.reader = &ioReader{reader: bodyReader}

	return respSerializer, nil
}

type responseSerializer struct {
	apiResource *metav1.APIResource
	reqInfo     *request.RequestInfo

	object           runtime.Object
	decoder          runtime.Decoder
	encoder          runtime.Encoder
	streamSerializer runtime.Serializer
	streamFramer     runtime.Framer

	reader     *ioReader
	buf        *bytes.Buffer
	gzipReader *gzip.Reader
	gzipWriter *gzip.Writer
}

type serializerReaderCloser struct {
	io.Reader
	serializer *responseSerializer
}

func (s *serializerReaderCloser) Close() error {
	if s.serializer.IsWatch() {
		klog.Infof("Close watch buf")
		//WatchCount --
	} else {
		ListCount--
		klog.Infof("Close list buf, count %d", ListCount)
	}
	s.Reader = nil
	s.serializer.Release()
	return nil
}

func (s *responseSerializer) IsWatch() bool {
	return s.streamFramer != nil
}

func (s *responseSerializer) Release() {
	if s.gzipReader != nil {
		s.gzipReader.Close()
		pool.GzipReaderPool.Put(s.gzipReader)
	}
	if s.gzipWriter != nil {
		s.gzipWriter.Close()
		s.gzipWriter.Reset(nil)
		pool.GzipWriterPool.Put(s.gzipWriter)
	}
}

func (s *responseSerializer) resetBuf() {
	if s.buf == nil {
		s.buf = bytes.NewBuffer(pool.BytesPool.Get())
	}
	s.buf.Reset()
}

func (s *responseSerializer) DecodeList(ctx context.Context) (runtime.Object, error) {
	ListCount++
	klog.Infof("add list count %d", ListCount)
	body, err := s.reader.readOnce()
	if err != nil {
		return nil, err
	}

	s.buf = bytes.NewBuffer(body)
	s.buf.Reset()

	obj := s.object.DeepCopyObject()
	if _, _, err := s.decoder.Decode(body, nil, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (s *responseSerializer) EncodeList(obj runtime.Object) (io.ReadCloser, int, error) {
	s.resetBuf()
	var w io.Writer = s.buf
	if s.gzipWriter != nil {
		s.gzipWriter.Reset(s.buf)
		w = s.gzipWriter
	}

	if err := s.encoder.Encode(obj, w); err != nil {
		return nil, 0, err
	}

	if s.gzipWriter != nil {
		s.gzipWriter.Close()
	}

	return &serializerReaderCloser{Reader: s.buf, serializer: s}, s.buf.Len(), nil
}

func (s *responseSerializer) DecodeWatch() (*metav1.WatchEvent, runtime.Object, error) {
	s.resetBuf()
	n, err := s.reader.readStreaming(s.buf)

	if err != nil {
		return nil, nil, err
	}

	//klog.Infof("Decode watch buf len %d, cap %d, n %d\n", s.buf.Len(), s.buf.Cap(), n)
	event := &metav1.WatchEvent{}
	if _, _, err := s.streamSerializer.Decode(s.buf.Bytes()[:n], nil, event); err != nil {
		return nil, nil, err
	}

	switch watch.EventType(event.Type) {
	case watch.Added, watch.Modified, watch.Deleted:
	default:
		return event, nil, nil
	}

	obj := s.object.DeepCopyObject()
	if _, _, err := s.decoder.Decode(event.Object.Raw, nil, obj); err != nil {
		return nil, nil, err
	}
	return event, obj, nil
}

func (s *responseSerializer) EncodeWatch(e *metav1.WatchEvent) ([]byte, error) {
	s.resetBuf()
	var w = s.streamFramer.NewFrameWriter(s.buf)
	if s.gzipWriter != nil {
		s.gzipWriter.Reset(w)
		w = s.gzipWriter
	}
	if err := s.streamSerializer.Encode(e, w); err != nil {
		return nil, err
	}
	if s.gzipWriter != nil {
		s.gzipWriter.Close()
	}
	return s.buf.Bytes(), nil
}

// logBody logs a body output that could be either JSON or protobuf. It explicitly guards against
// allocating a new string for the body output unless necessary. Uses a simple heuristic to determine
// whether the body is printable.
func logBody(prefix string, body []byte) {
	if klog.V(8).Enabled() {
		if bytes.IndexFunc(body, func(r rune) bool {
			return r < 0x0a
		}) != -1 {
			// hex.Dump(body)
			klog.Infof("%s(pb): [truncated %d chars]", prefix, len(body))
		} else {
			klog.Infof("%s(text): %s", prefix, truncateBody(string(body)))
		}
	}
}

// truncateBody decides if the body should be truncated, based on the glog Verbosity.
func truncateBody(body string) string {
	max := 0
	switch {
	case klog.V(10).Enabled():
		return body
	case klog.V(9).Enabled():
		max = 10240
	case klog.V(8).Enabled():
		max = 1024
	}

	if len(body) <= max {
		return body
	}

	return body[:max] + fmt.Sprintf(" [truncated %d chars]", len(body)-max)
}
