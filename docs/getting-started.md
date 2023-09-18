# Getting Started
This guide lets you quickly evaluate KusionStack Controller Mesh. 


## Installation
Controller-Mesh requires **Kubernetes version >= 1.18**

**Install with helm**
```shell
# Firstly add KusionStack charts repository if you haven't do this.
$ helm repo add kusionstack https://kusionstack.io/charts

# [Optional]
$ helm repo update

# Install the latest version.
$ helm install ctrlmesh kusionstack/ctrlmesh --version v0.1.0

# Uninstall
$ helm uninstall ctrlmesh
```

## Deploy the sample operator application

 1. Deploy the sample operator application

```shell
# 
$ helm install ctrlmesh kusionstack/demo-operator --version v0.1.0

```