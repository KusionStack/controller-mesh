# Getting Started
This guide lets you quickly evaluate KusionStack Controller Mesh. 


## Install Controller Mesh Manager
Controller Mesh requires **Kubernetes version >= 1.18**

**Install with helm**
```bash
# Firstly add KusionStack charts repository if you haven't do this.
$ helm repo add kusionstack https://kusionstack.io/charts

# To update the kusionstack repo.
$ helm repo update kusionstack

# Install the latest version.
$ helm install ctrlmesh kusionstack/ctrlmesh --version v0.1.0

# Wait manager ready
$ kubectl -n ctrlmesh get po
NAME                        READY   STATUS    RESTARTS   AGE
ctrlmesh-57d6b4df57-mdslc   1/1     Running   0          40s
ctrlmesh-57d6b4df57-mtv2s   1/1     Running   0          40s
```
[Install manager with more options](installation.md)
## Enable Custom Operator Sharding

 1. Deploy the sample operator application:

```bash
$ helm install sample kusionstack/sample-operator --version v0.1.1

$ kubeconfig kubectl -n ctrlmesh get sts
NAME            READY   AGE
demo-operator   2/2     10s

$ kubectl -n ctrlmesh get po
NAME                        READY   STATUS    RESTARTS   AGE
ctrlmesh-57d6b4df57-mdslc   1/1     Running   0          1m
ctrlmesh-57d6b4df57-mtv2s   1/1     Running   0          1m
demo-operator-0             1/1     Running   0          17s
demo-operator-1             1/1     Running   0          16s
```

 2. Config and apply shardingconfig:

```bash
# Show demo shardingconfig
$ helm template sample kusionstack/sample-operator --version v0.1.1 --set sharding.enable=true --show-only templates/shardingconfig.yaml > shardingconfig.yaml
$ cat shardingconfig.yaml
```
```yaml
---
# Source: sample-operator/templates/shardingconfig.yaml
apiVersion: ctrlmesh.kusionstack.io/v1alpha1
kind: ShardingConfig
metadata:
  name: sharding-root
  namespace: ctrlmesh
spec:
  # auto sharding config
  root:
    prefix: sample-demo
    targetStatefulSet: sample-operator
    canary:
      replicas: 1
      inNamespaces:
      - demo-0a
    auto:
      everyShardReplicas: 2
      shardingSize: 2
    resourceSelector:
    - relateResources:
      - apiGroups:
        - '*'
        resources:
        - configmaps
      selector:
        matchLabels:
          control-by: demo
  controller:
    leaderElectionName: demo-manager
```
```bash
# You can configure the ShardingConfig according to your requirements.
# Apply shardingconfig
$ kubectl apply -f shardingconfig.yaml

# Waiting for pods to be recreate and ready.
# The mesh-proxy container will be automatically injected into the pod.
$ kubectl -n ctrlmesh get po
NAME                        READY   STATUS    RESTARTS   AGE
ctrlmesh-57d6b4df57-mdslc   1/1     Running   0          6m
ctrlmesh-57d6b4df57-mtv2s   1/1     Running   0          6m
sample-operator-0           2/2     Running   0          22s
sample-operator-1           2/2     Running   0          17s
sample-operator-2           2/2     Running   0          13s
sample-operator-3           2/2     Running   0          9s
sample-operator-4           2/2     Running   0          5s

# Now we have three shards with three lease.
#  sample-demo-0-canary -> [sample-operator-0]
#  sample-demo-1-normal -> [sample-operator-1, sample-operator-2]
#  sample-demo-2-normal -> [sample-operator-3, sample-operator-4]
$ kubectl -n ctrlmesh get lease
NAME                                  HOLDER                                                           AGE
ctrlmesh-manager                      ctrlmesh-57d6b4df57-mdslc_a804c1cc-4bca-4acc-a0c3-54762b459cb3   6m
demo-manager---sample-demo-0-canary   sample-operator-0_55a0909a-a72b-469b-86c6-cbabccee1269           85s
demo-manager---sample-demo-1-normal   sample-operator-1_45efc2fa-4928-4073-81c4-9b0892a7398d           81s
demo-manager---sample-demo-2-normal   sample-operator-3_55914401-a735-445b-84d2-17120dca0e05           73s
```