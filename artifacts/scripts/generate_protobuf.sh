#!/usr/bin/env bash

if [[ -z "$(which protoc)" || ( "$(protoc --version)" != "libprotoc 3.15."* && "$(protoc --version)" != "libprotoc 3.19."* ) ]]; then
  echo "Generating protobuf requires protoc 3.15.x or 3.19.x. Please download and"
  echo "install the platform appropriate Protobuf package for your OS: "
  echo
  echo "  https://github.com/google/protobuf/releases"
  echo
  echo "WARNING: Protobuf changes are not being validated"
  exit 1
fi

set -e
TMP_DIR=$(mktemp -d)
mkdir -p "${TMP_DIR}"/bin
mkdir -p "${TMP_DIR}"/src/github.com/KusionStack/kridge/pkg

cp -r ./{hack,vendor} "${TMP_DIR}"/src/github.com/KusionStack/kridge/
cp -r ./pkg/apis "${TMP_DIR}"/src/github.com/KusionStack/kridge/pkg/
cp  ./go.mod "${TMP_DIR}"/src/github.com/KusionStack/kridge/go.mod

(cd "${TMP_DIR}"/src/github.com/KusionStack/kridge; \
    GO111MODULE=off GOPATH=${TMP_DIR} go build  -o ${TMP_DIR}/bin/protoc-gen-gogo github.com/KusionStack/kridge/vendor/k8s.io/code-generator/cmd/go-to-protobuf/protoc-gen-gogo; \
    PATH=${TMP_DIR}/bin:$PATH GOPATH=${TMP_DIR} \
    protoc \
    --gogo_out=plugins=grpc,paths=source_relative:. pkg/apis/kridge/proto/kridge.proto)
# protoc bug in code-generator v0.26.1, can not contains '/' in path.


cp -f "${TMP_DIR}"/src/github.com/KusionStack/kridge/pkg/apis/kridge/proto/*.go pkg/apis/kridge/proto/
