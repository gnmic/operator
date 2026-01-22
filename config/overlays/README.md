# Kustomize Overlays

This directory contains example overlays for customizing the gNMIc Operator installation.

## Available Overlays

### `custom-namespace/`
Deploy the operator to a custom namespace with a different name prefix.

```bash
kubectl apply -k https://github.com/gnmic/operator/config/overlays/custom-namespace?ref=v0.1.0
```

### `production/`
Production-ready deployment with increased resources and pod disruption budget.

```bash
# Copy to your repo and customize
cp -r config/overlays/production my-deployment/
# Edit my-deployment/kustomization.yaml
kubectl apply -k my-deployment/
```

## Creating Your Own Overlay

1. Create a new directory:
```bash
mkdir my-overlay
```

2. Create a `kustomization.yaml`:
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  # Reference the base config (use a specific version tag)
  - https://github.com/gnmic/operator/config/default?ref=v0.1.0

# Customize namespace
namespace: my-namespace

# Pin image version
images:
  - name: controller
    newName: ghcr.io/gnmic/operator
    newTag: "0.1.0"

# Add custom patches as needed
patches:
  - target:
      kind: Deployment
      name: controller-manager
    patch: |
      - op: replace
        path: /spec/replicas
        value: 2
```

3. Apply:
```bash
kubectl apply -k my-overlay/
```

## Common Customizations

### Change Image Version
```yaml
images:
  - name: controller
    newName: ghcr.io/gnmic/operator
    newTag: "0.2.0"
```

### Change Namespace
```yaml
namespace: telemetry
```

### Add Labels
```yaml
labels:
  - pairs:
      team: platform
      cost-center: infrastructure
    includeSelectors: false
```

### Increase Resources
```yaml
patches:
  - target:
      kind: Deployment
      name: controller-manager
    patch: |
      - op: replace
        path: /spec/template/spec/containers/0/resources/limits/memory
        value: 1Gi
```

### Add Node Selector
```yaml
patches:
  - target:
      kind: Deployment
      name: controller-manager
    patch: |
      - op: add
        path: /spec/template/spec/nodeSelector
        value:
          node-role.kubernetes.io/infra: ""
```
