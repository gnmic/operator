---
title: "Helm Chart"
linkTitle: "Helm Chart"
weight: 2
description: >
  gNMIc Operator Helm chart configuration reference
---

This page documents all configuration options available in the gNMIc Operator Helm chart.

## Installation

```bash
# From OCI registry
helm install gnmic-operator oci://ghcr.io/gnmic/operator/charts/gnmic-operator \
  --namespace gnmic-system \
  --create-namespace

# From source
helm install gnmic-operator ./helm \
  --namespace gnmic-system \
  --create-namespace
```

## Values

### Image Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Container image repository | `ghcr.io/gnmic/operator` |
| `image.tag` | Container image tag | Chart's `appVersion` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `imagePullSecrets` | Image pull secrets | `[]` |

```yaml
image:
  repository: ghcr.io/gnmic/operator
  tag: "0.1.0"
  pullPolicy: IfNotPresent

imagePullSecrets:
  - name: my-registry-secret
```

### Deployment Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of operator replicas | `1` |
| `nameOverride` | Override the chart name | `""` |
| `fullnameOverride` | Override the full resource name | `""` |

```yaml
replicaCount: 1
nameOverride: ""
fullnameOverride: "my-operator"
```

### Service Account

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.create` | Create a service account | `true` |
| `serviceAccount.annotations` | Annotations for the service account | `{}` |
| `serviceAccount.name` | Name of the service account | Generated from fullname |

```yaml
serviceAccount:
  create: true
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789:role/my-role
  name: "gnmic-operator"
```

### Pod Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podAnnotations` | Annotations for the operator pod | `{}` |
| `podSecurityContext` | Security context for the pod | `{runAsNonRoot: true}` |
| `securityContext` | Security context for the container | See below |
| `nodeSelector` | Node selector for pod scheduling | `{}` |
| `tolerations` | Tolerations for pod scheduling | `[]` |
| `affinity` | Affinity rules for pod scheduling | `{}` |

```yaml
podAnnotations:
  prometheus.io/scrape: "true"

podSecurityContext:
  runAsNonRoot: true

securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - "ALL"

nodeSelector:
  node-role.kubernetes.io/infra: ""

tolerations:
  - key: "dedicated"
    operator: "Equal"
    value: "infra"
    effect: "NoSchedule"

affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              app.kubernetes.io/name: gnmic-operator
          topologyKey: kubernetes.io/hostname
```

### Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `256Mi` |
| `resources.requests.cpu` | CPU request | `10m` |
| `resources.requests.memory` | Memory request | `64Mi` |

```yaml
resources:
  limits:
    cpu: 1000m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

### Leader Election

| Parameter | Description | Default |
|-----------|-------------|---------|
| `leaderElection.enabled` | Enable leader election | `true` |

Leader election ensures only one controller instance is active when running multiple replicas.

```yaml
leaderElection:
  enabled: true
```

### Webhooks

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webhook.enabled` | Enable admission webhooks | `true` |
| `webhook.port` | Webhook server port | `9443` |

Webhooks provide validation and defaulting for custom resources. Requires cert-manager when enabled.

```yaml
webhook:
  enabled: true
  port: 9443
```

### Metrics

| Parameter | Description | Default |
|-----------|-------------|---------|
| `metrics.enabled` | Enable metrics service | `true` |
| `metrics.port` | Metrics endpoint port | `8080` |
| `metrics.serviceMonitor.enabled` | Create ServiceMonitor for Prometheus | `false` |
| `metrics.serviceMonitor.namespace` | Namespace for ServiceMonitor | Release namespace |
| `metrics.serviceMonitor.interval` | Scrape interval | `30s` |
| `metrics.serviceMonitor.scrapeTimeout` | Scrape timeout | `10s` |

```yaml
metrics:
  enabled: true
  port: 8080
  serviceMonitor:
    enabled: true
    namespace: monitoring
    interval: 30s
    scrapeTimeout: 10s
```

### Health Probes

| Parameter | Description | Default |
|-----------|-------------|---------|
| `health.livenessProbe` | Liveness probe configuration | See below |
| `health.readinessProbe` | Readiness probe configuration | See below |

```yaml
health:
  livenessProbe:
    httpGet:
      path: /healthz
      port: 8081
    initialDelaySeconds: 15
    periodSeconds: 20
  readinessProbe:
    httpGet:
      path: /readyz
      port: 8081
    initialDelaySeconds: 5
    periodSeconds: 10
```

### cert-manager Integration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `certManager.enabled` | Use cert-manager for webhook certificates | `true` |
| `certManager.issuer.create` | Create a self-signed issuer | `true` |
| `certManager.issuer.kind` | Issuer kind (Issuer or ClusterIssuer) | `Issuer` |
| `certManager.issuer.name` | Name of existing issuer (if not creating) | Generated |
| `certManager.duration` | Certificate duration | `8760h` (1 year) |
| `certManager.renewBefore` | Renew certificate before expiry | `720h` (30 days) |

```yaml
certManager:
  enabled: true
  issuer:
    create: true
    kind: Issuer
    name: ""
  duration: 8760h
  renewBefore: 720h
```

To use an existing ClusterIssuer:

```yaml
certManager:
  enabled: true
  issuer:
    create: false
    kind: ClusterIssuer
    name: my-cluster-issuer
```

### CRDs

| Parameter | Description | Default |
|-----------|-------------|---------|
| `crds.install` | Install CRDs with the chart | `true` |
| `crds.keep` | Keep CRDs on uninstall | `true` |

```yaml
crds:
  install: true
  keep: true
```

## Examples

### Minimal Installation

```yaml
# values-minimal.yaml
replicaCount: 1
```

```bash
helm install gnmic-operator oci://ghcr.io/gnmic/operator/charts/gnmic-operator \
  -f values-minimal.yaml \
  --namespace gnmic-system \
  --create-namespace
```

### Production Ready Installation

```yaml
# values-production.yaml
replicaCount: 2

resources:
  limits:
    cpu: 1000m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi

affinity:
  podAntiAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchLabels:
            app.kubernetes.io/name: gnmic-operator
        topologyKey: kubernetes.io/hostname

metrics:
  serviceMonitor:
    enabled: true
    interval: 30s
```

```bash
helm install gnmic-operator oci://ghcr.io/gnmic/operator/charts/gnmic-operator \
  -f values-production.yaml \
  --namespace gnmic-system \
  --create-namespace
```

### Without Webhooks

```yaml
# values-dev.yaml
webhook:
  enabled: false

certManager:
  enabled: false
```

```bash
helm install gnmic-operator oci://ghcr.io/gnmic/operator/charts/gnmic-operator \
  -f values-dev.yaml \
  --namespace gnmic-system \
  --create-namespace
```

### Air-Gapped Installation

```yaml
# values-airgapped.yaml
image:
  repository: my-registry.internal/gnmic/operator
  tag: "0.1.0"

imagePullSecrets:
  - name: registry-credentials
```

```bash
helm install gnmic-operator ./helm \
  -f values-airgapped.yaml \
  --namespace gnmic-system \
  --create-namespace
```

## Upgrading

```bash
# Get current values
helm get values gnmic-operator -n gnmic-system > current-values.yaml

# Upgrade with new version
helm upgrade gnmic-operator oci://ghcr.io/gnmic/operator/charts/gnmic-operator \
  --version 0.2.0 \
  -f current-values.yaml \
  --namespace gnmic-system
```

## Uninstalling

```bash
# Uninstall the release
helm uninstall gnmic-operator -n gnmic-system

# CRDs are kept by default. To remove them:
kubectl delete crds \
  clusters.operator.gnmic.dev \
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
