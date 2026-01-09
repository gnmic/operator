---
title: "Installation"
linkTitle: "Installation"
weight: 1
description: >
  Install gNMIc Operator on your Kubernetes cluster
---

## Prerequisites

- Kubernetes cluster (v1.25+)
- kubectl configured to access your cluster
- [cert-manager](https://cert-manager.io/) (required for TLS features)

## Install with kubectl

### Install CRDs

First, install the Custom Resource Definitions:

```bash
kubectl apply -k https://github.com/karimra/gnmic-operator/config/crd
```

### Install the Operator

Deploy the operator:

```bash
kubectl apply -k https://github.com/karimra/gnmic-operator/config/default
```

### Verify Installation

Check that the operator is running:

```bash
kubectl get pods -n gnmic-system
```

You should see output similar to:

```
NAME                                           READY   STATUS    RESTARTS   AGE
gnmic-operator-controller-manager-xxxxx-xxxxx  2/2     Running   0          30s
```

## Install from Source

Clone the repository and install using make:

```bash
git clone https://github.com/karimra/gnmic-operator.git
cd gnmic-operator

# Install CRDs
make install

# Deploy the operator
make deploy IMG=ghcr.io/karimra/gnmic-operator:dev
```

## Uninstall

To remove the operator:

```bash
# Remove the operator
kubectl delete -k https://github.com/karimra/gnmic-operator/config/default

# Remove CRDs (this will delete all gNMIc resources!)
kubectl delete -k https://github.com/karimra/gnmic-operator/config/crd
```

## Installing cert-manager

cert-manager is required if you want to enable TLS for secure communication between the operator and gNMIc pods.

```bash
# Install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.19.2/cert-manager.yaml

# Wait for cert-manager to be ready
kubectl wait --for=condition=Available deployment --all -n cert-manager --timeout=120s
```

For more installation options, see the [cert-manager documentation](https://cert-manager.io/docs/installation/).

## Next Steps

- [Quick Start]({{< relref "quick-start" >}}) - Deploy your first gNMIc collector
