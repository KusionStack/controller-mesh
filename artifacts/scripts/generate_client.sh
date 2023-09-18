#!/usr/bin/env bash

#go mod vendor
retVal=$?
if [ $retVal -ne 0 ]; then
    exit $retVal
fi

set -e
TMP_DIR=$(mktemp -d)

mkdir -p "${TMP_DIR}"/src/github.com/KusionStack/ctrlmesh/pkg/client

cp -r ./{hack,vendor} "${TMP_DIR}"/src/github.com/KusionStack/ctrlmesh/
cp -r ./pkg/apis "${TMP_DIR}"/src/github.com/KusionStack/ctrlmesh/pkg/
cp  ./go.mod "${TMP_DIR}"/src/github.com/KusionStack/ctrlmesh/go.mod

(cd "${TMP_DIR}"/src/github.com/KusionStack/ctrlmesh; \
    GOPATH=${TMP_DIR} GO111MODULE=on /bin/bash vendor/k8s.io/code-generator/generate-groups.sh all \
    github.com/KusionStack/ctrlmesh/pkg/client github.com/KusionStack/ctrlmesh/pkg/apis "ctrlmesh:v1alpha1" -h ./hack/boilerplate.go.txt)

rm -rf ./pkg/client/{clientset,informers,listers}
mv "${TMP_DIR}"/src/github.com/KusionStack/ctrlmesh/pkg/client/* ./pkg/client