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
- [cert-manager](https://cert-manager.io/) (required for webhooks and TLS features)

## Installation Methods

### Method 1: Quick Install (Recommended)

Download and apply the pre-built manifest from the release:

```bash
# Install a specific version
kubectl apply -f https://github.com/gnmic/operator/releases/download/v0.1.0/install.yaml

# This includes CRDs, RBAC, webhooks, and the operator deployment
```

### Method 2: Using Kustomize

For more control over the installation, use kustomize with an overlay:

```bash
# Create a kustomization.yaml
cat <<EOF > kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - https://github.com/gnmic/operator/config/default?ref=v0.1.0
images:
  - name: controller
    newName: ghcr.io/gnmic/operator
    newTag: "0.1.0"
EOF

# Apply
kubectl apply -k .
```

### Method 3: Using Helm

```bash
# Add the Helm repository (OCI)
helm install gnmic-operator oci://ghcr.io/gnmic/operator/charts/gnmic-operator --version 0.1.0

# Or with custom values
helm install gnmic-operator oci://ghcr.io/gnmic/operator/charts/gnmic-operator \
  --version 0.1.0 \
  --namespace gnmic-system \
  --create-namespace \
  --set resources.limits.memory=512Mi
```

## Customization

### Custom Namespace

```yaml
# kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - https://github.com/gnmic/operator/config/default?ref=v0.1.0
namespace: my-namespace
namePrefix: my-
images:
  - name: controller
    newName: ghcr.io/gnmic/operator
    newTag: "0.1.0"
```

### Custom Resources

```yaml
# kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - https://github.com/gnmic/operator/config/default?ref=v0.1.0
images:
  - name: controller
    newName: ghcr.io/gnmic/operator
    newTag: "0.1.0"
patches:
  - target:
      kind: Deployment
      name: controller-manager
    patch: |
      - op: replace
        path: /spec/template/spec/containers/0/resources
        value:
          limits:
            cpu: "1"
            memory: 512Mi
          requests:
            cpu: 100m
            memory: 128Mi
```

### Pre-built Overlays

Example overlays are available in the repository:

| Overlay | Description |
|---------|-------------|
| `config/overlays/custom-namespace` | Deploy to a custom namespace |
| `config/overlays/without-certmanager` | Development mode without cert-manager |
| `config/overlays/production` | Production-ready with increased resources |

## Verify Installation

Check that the operator is running:

```bash
kubectl get pods -n gnmic-system
```

You should see output similar to:

```
NAME                                           READY   STATUS    RESTARTS   AGE
gnmic-controller-manager-xxxxx-xxxxx           1/1     Running   0          30s
```

Check the CRDs are installed:

```bash
kubectl get crds | grep gnmic
```

## Installing cert-manager

cert-manager is required for webhooks and TLS features.

```bash
# Install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.17.0/cert-manager.yaml

# Wait for cert-manager to be ready
kubectl wait --for=condition=Available deployment --all -n cert-manager --timeout=120s
```

For more installation options, see the [cert-manager documentation](https://cert-manager.io/docs/installation/).

## Uninstall

To remove the operator:

```bash
# If installed with quick install
kubectl delete -f https://github.com/gnmic/operator/releases/download/v0.1.0/install.yaml

# If installed with Helm
helm uninstall gnmic-operator

# If installed with kustomize
kubectl delete -k .
```

To remove CRDs (this will delete all gNMIc resources!):

```bash
kubectl delete -f https://github.com/gnmic/operator/releases/download/v0.1.0/crds.yaml
```

## Next Steps

- [Quick Start]({{< relref "quick-start" >}}) - Deploy your first gNMIc collector
