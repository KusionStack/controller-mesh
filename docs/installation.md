
## Installation

### Install with helm
Controller Mesh requires **Kubernetes version >= 1.18**
```shell
# Firstly add charts repository if you haven't do this.
$ helm repo add kusionstack https://kusionstack.io/charts

# To update the kusionstack repo.
$ helm repo update kusionstack

# Install the latest version.
$ helm install ctrlmesh kusionstack/ctrlmesh --version v0.1.0

# Upgrade to the latest version 
$ helm upgrade ctrlmesh kusionstack/ctrlmesh 

# Uninstall
$ helm uninstall ctrlmesh
```
[Helm](https://github.com/helm/helm) is a tool for managing packages of pre-configured Kubernetes resources.
### Optional: chart parameters

The following table lists the configurable parameters of the chart and their default values.

| Parameter                           | Description                                           | Default                                                                                                                                                                                                                                                  |
|-------------------------------------|-------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `namespace`                         | namespace for controller mesh installation            | `ctrlmesh`                                                                                                                                                                                                                                               |
| `namespaceEnabled`                  | Whether to create the installation.namespace          | `true`                                                                                                                                                                                                                                                   |
| `manager.replicas`                  | Replicas of ctrlmesh-manager deployment               | `2`                                                                                                                                                                                                                                                      |
| `manager.image.repo`                | Repository for ctrlmesh-manager image                 | `kusionstack/ctrlmesh-manager`                                                                                                                                                                                                                           |
| `manager.image.pullPolicy`          | Image pull policy for ctrlmesh-manager                | `IfNotPresent`                                                                                                                                                                                                                                           |
| `manager.image.tag`                 | Tag for ctrlmesh-manager                              | `v0.1.0`                                                                                                                                                                                                                                                 |
| `manager.resources.limits.cpu`      | CPU resource limit of ctrlmesh-manager container      | `500m`                                                                                                                                                                                                                                                   |
| `manager.resources.limits.memory`   | Memory resource limit of ctrlmesh-manager container   | `512Mi`                                                                                                                                                                                                                                                  |
| `manager.resources.requests.cpu`    | CPU resource request of ctrlmesh-manager container    | `10m`                                                                                                                                                                                                                                                    |
| `manager.resources.requests.memory` | Memory resource request of ctrlmesh-manager container | `64Mi`                                                                                                                                                                                                                                                   |
| `proxy.image.repo`                  | Repository for ctrlmesh-proxy image                   | `kusionstack/ctrlmesh-proxy`                                                                                                                                                                                                                             |
| `proxy.image.pullPolicy`            | Image pull policy for ctrlmesh-proxy                  | `IfNotPresent`                                                                                                                                                                                                                                           |
| `proxy.image.tag`                   | Tag for ctrlmesh-proxy                                | `v0.1.0`                                                                                                                                                                                                                                                 |
| `proxy.resources.limits.cpu`        | CPU resource requests of ctrlmesh-proxy container     | `100m`                                                                                                                                                                                                                                                   |
| `proxy.resources.limits.memory`     | Memory resource requests of ctrlmesh-proxy container  | `100Mi`                                                                                                                                                                                                                                                  |
| `init.image.repo`                   | Repository for ctrlmesh-init image                    | `kusionstack/ctrlmesh-init`                                                                                                                                                                                                                              |
| `init.image.tag`                    | Tag for ctrlmesh-init                                 | `v0.1.0`                                                                                                                                                                                                                                                 |
| `shardingGroupVersionKinds`         | Sharding resource lists（yaml）                         | `groupVersionKinds:`<br>`ctrlmesh.kusionstack.io/v1alpha1:`<br>`- '*'`<br>`  v1:`<br>`- Pod`<br>`- PersistentVolumeClaim`<br>`- Service`<br>`- ConfigMap`<br>`- Endpoint`<br>`  apps/v1:`<br>`- StatefulSet`<br>`- ReplicaSet`<br>`- ControllerRevision` |

Specify each parameter using the `--set key=value` argument to `helm install` or `helm upgrade`.


