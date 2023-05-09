#!/usr/bin/env bash

# delete webhook configurations
kubectl delete mutatingwebhookconfiguration kridge-mutating-webhook-configuration
kubectl delete validatingwebhookconfiguration kridge-validating-webhook-configuration

# delete manager and rbac rules
kubectl delete ns kridge-system
kubectl delete clusterrolebinding kridge-manager-rolebinding
kubectl delete clusterrole kridge-manager-role

# delete CRDs
kubectl get crd -o name | grep "customresourcedefinition.apiextensions.k8s.io/[a-z.]*.kridge.kusionstack.io" | xargs kubectl delete
