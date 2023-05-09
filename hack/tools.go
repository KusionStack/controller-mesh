//go:build tools
// +build tools

// This package imports things required by build scripts, to force `go mod` to see them as dependencies
package hack

import (
	_ "k8s.io/apimachinery/pkg/runtime/serializer"
	_ "k8s.io/code-generator"
	_ "k8s.io/code-generator/cmd/go-to-protobuf/protoc-gen-gogo"
	_ "k8s.io/kube-openapi/cmd/openapi-gen"
)
