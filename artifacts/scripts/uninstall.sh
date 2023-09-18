#!/usr/bin/env bash

# delete webhook configurations
kubectl delete mutatingwebhookconfiguration ctrlmesh-mutating-webhook-configuration
kubectl delete validatingwebhookconfiguration ctrlmesh-validating-webhook-configuration

# delete manager and rbac rules
kubectl delete ns ctrlmesh-system
kubectl delete clusterrolebinding ctrlmesh-manager-rolebinding
kubectl delete clusterrole ctrlmesh-manager-role

# delete CRDs
kubectl get crd -o name | grep "customresourcedefinition.apiextensions.k8s.io/[a-z.]*.ctrlmesh.kusionstack.io" | xargs kubectl delete
