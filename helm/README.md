# gNMIc Operator Helm Chart

A Helm chart for deploying the gNMIc Operator on Kubernetes.

## Prerequisites

- Kubernetes 1.25+
- Helm 3.8+
- [cert-manager](https://cert-manager.io/) (required for webhook TLS certificates)

## Installation

### From OCI Registry (Recommended)

```bash
# Install the latest version
helm install gnmic-operator oci://ghcr.io/gnmic/operator/charts/gnmic-operator

# Install a specific version
helm install gnmic-operator oci://ghcr.io/gnmic/operator/charts/gnmic-operator --version 0.1.0
```

### From Source

```bash
# Clone the repository
git clone https://github.com/gnmic/operator.git
cd gnmic-operator

# Install the chart
helm install gnmic-operator ./helm
```

## Configuration

See [values.yaml](values.yaml) for the full list of configuration options.

### Common Configuration Examples

#### Disable cert-manager (use your own certificates)

```yaml
certManager:
  enabled: false
```

#### Enable ServiceMonitor for Prometheus

```yaml
metrics:
  serviceMonitor:
    enabled: true
    interval: 30s
```

#### Custom resource limits

```yaml
resources:
  limits:
    cpu: 1000m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

## Uninstallation

```bash
helm uninstall gnmic-operator
```

By default, CRDs are not deleted when uninstalling. To remove them:

```bash
kubectl delete crds clusters.operator.gnmic.dev \
  inputs.operator.gnmic.dev \
  outputs.operator.gnmic.dev \
  pipelines.operator.gnmic.dev \
  processors.operator.gnmic.dev \
  subscriptions.operator.gnmic.dev \
  targetprofiles.operator.gnmic.dev \
  targets.operator.gnmic.dev \
  targetsources.operator.gnmic.dev \
  tunneltargetpolicies.operator.gnmic.dev
```

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| replicaCount | int | `1` | Number of operator replicas |
| image.repository | string | `"ghcr.io/gnmic/operator"` | Image repository |
| image.tag | string | `""` | Image tag (defaults to chart appVersion) |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy |
| serviceAccount.create | bool | `true` | Create service account |
| webhook.enabled | bool | `true` | Enable admission webhooks |
| certManager.enabled | bool | `true` | Use cert-manager for webhook certificates |
| metrics.enabled | bool | `true` | Enable metrics endpoint |
| metrics.serviceMonitor.enabled | bool | `false` | Create ServiceMonitor for Prometheus |
| crds.install | bool | `true` | Install CRDs with the chart |
| crds.keep | bool | `true` | Keep CRDs on uninstall |

## License

Apache License 2.0
