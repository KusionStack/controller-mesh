

[简体中文](https://github.com/KusionStack/kusion/blob/main/README-zh.md)
| [English](https://github.com/KusionStack/kusion/blob/main/README.md)

# Controller Mesh

KusionStack Controller Mesh is a solution that helps developers manage their controllers/operators better.

The design architecture of this project is based on [openkruise/controllermesh](https://github.com/openkruise/controllermesh).

## Key Features

1. **Sharding**: Through relevant configurations, Kubernetes single-point deployed operator applications can be flexibly shard deployed.
2. **Canary upgrade**: Depends on sharding, the controllers can be updated in canary progress instead of one time replace.
3. **Circuit breaker and rate limiter**: Not only Kubernetes operation requests, but also other external operation requests.
4. **Multicluster routing and sharding**: This feature is supported by [kusionstack/kaera(karbour)]()

<p align="center"><img width="800" src="./docs/img/mesh-arch-2.png"/></p>

## Quick Start
Visit [Quick Start](docs/getting-started.md).


## Installation
Visit [Installation](docs/installation.md).
## Principles

Generally, a `ctrlmesh-proxy` container will be injected into each operator Pod that has configured in ShardingConfigs.
This proxy container will intercept and handle the connection by between API/Oth Server and controllers/webhooks in the Pod.

<p align="center"><img width="550" src="./docs/img/fake-configmap.png"/></p>

ApiServer proxy method:
- *iptables nat*: 
- *fake kubeconfig*: 

The `ctrlmesh-manager` dispatches rules to the proxies, so that they can route requests according to the rules.


A core CRD in ControllerMesh is `ShardingConfig`. It contains all rules for user's controller:

```yaml
apiVersion: ctrlmesh.kusionstack.io/v1alpha1
kind: ShardingConfig
metadata:
  name: sharding-demo
  namespace: operator-demo
spec:
  controller:
    leaderElectionName: operator-leader
  webhook:
    certDir: /tmp/webhook-certs
    port: 9443
  limits:
  - relateResources:
    - apiGroups:
      - '*'
      resources:
      - pods
      - services
    selector:
      matchExpressions:
      - key: ctrlmesh.kusionstack.io/namespace
        operator: In
        values:
        - ns-a
        - ns-b
      matchLabels:
      # ...
  selector:
    matchExpressions:
    - key: statefulset.kubernetes.io/pod-name
      operator: In
      values:
      - operator-demo-0
```

- selector: for all pods under a shard. It can be a subset of pods under a StatefulSet.
- controller: configuration for controller, including leader election name
- webhook: configuration for webhook, including certDir and port of this webhook
- limits: shard isolation is achieved through a set of `ObjectSelector`.

When `manager` is first launched, shard labels will be added to all configured resources.

- `ctrlmesh.kusionstack.io/sharding-hash`: the hash value calculated based on the namespace ranges from 0 to 31.
- `ctrlmesh.kusionstack.io/namespace`: the namespace referring to this resource.
- `ctrlmesh.kusionstack.io/control`: under ctrlmesh-manager control.


In this repo, we only support `ObjectSelector` type of flow control,
which means the `ctrlmesh-proxy `will proxy http/s requests to the ApiServer, 
and inject a `LabelSelector` into the request param for the requested resource type.




Router:

<p align="center"><img width="600" src="./docs/img/mesh-proxy.png"/></p>

