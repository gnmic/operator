---
title: "Cluster"
linkTitle: "Cluster"
weight: 1
description: >
  Configuring gNMIc Cluster deployments
---

The `Cluster` resource defines a gNMIc collector deployment. It creates a StatefulSet, headless Service, and manages configuration for the gNMIc pods.

## Basic Configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: telemetry-cluster
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
```

## Spec Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `replicas` | int32 | Yes | 1 |Number of gNMIc pods to run |
| `image` | string | Yes | | Container image for gNMIc |
| `api.restPort` | int32 | Yes | 7890 | Port for REST API |
| `api.gnmiPort` | int32 | No | | Port for gNMI server (optional) |
| `resources` | ResourceRequirements | No | | CPU/memory requests and limits |
| `env` | []EnvVar | No | |Environment variables for pods |

## Resource Configuration

Set resource requests and limits:

```yaml
spec:
  resources:
    requests:
      memory: "128Mi"
      cpu: "100m"
    limits:
      memory: "512Mi"
      cpu: "1"
```

## Environment Variables

Inject environment variables into pods:

```yaml
spec:
  env:
    - name: GNMIC_LOG_LEVEL
      value: "debug"
    - name: GNMIC_API_TOKEN
      valueFrom:
        secretKeyRef:
          name: gnmic-secrets
          key: api-token
```

## gNMI Server

Enable the gNMI server for using the collecor as a gNMI Proxy/Cache

```yaml
spec:
  api:
    gnmiPort: 9393
```

## gRPC Tunnel Server

Enable gRPC tunnel mode for devices that initiate connections to the collector (reverse connectivity). This is useful when devices are behind NAT, firewalls, or when direct connectivity is not possible.

### Basic Tunnel Configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: tunnel-cluster
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
  grpcTunnel:
    port: 57400
```

### Tunnel Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `port` | int32 | Yes | - | Port for the gRPC tunnel server |
| `tls` | ClusterTLSConfig | No | - | TLS configuration for tunnel |
| `service.type` | ServiceType | No | LoadBalancer | Kubernetes service type |
| `service.annotations` | map[string]string | No | - | Service annotations |

### Tunnel with TLS

```yaml
spec:
  grpcTunnel:
    port: 57400
    tls:
      issuerRef: gnmic-ca-issuer
      bundleRef: client-ca-bundle  # Optional: for client cert verification
```

When `bundleRef` is set, client certificate authentication is required (`client-auth: require-verify`).

### Service Configuration

The tunnel service is automatically created when `grpcTunnel` is configured:

```yaml
spec:
  grpcTunnel:
    port: 57400
    service:
      type: LoadBalancer  # Default
      annotations:
        service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
```

### Resources Created for Tunnel

| Resource | Name Pattern | Purpose |
|----------|--------------|---------|
| Service | `gnmic-{cluster}-tunnel` | Exposes tunnel port to external devices |
| Certificate | `gnmic-{cluster}-{index}-tunnel-tls` | Per-pod tunnel TLS certificate (if TLS enabled) |

### Using Tunnel Targets

To collect telemetry from tunnel-connected devices, create `TunnelTargetPolicy` resources and reference them in a Pipeline. See the [TunnelTargetPolicy documentation]({{< ref "tunneltargetpolicy" >}}) for details.

## TLS Configuration

Enable TLS encryption for communication between the operator and gNMIc pods.

### Prerequisites

- [cert-manager](https://cert-manager.io/) installed in your cluster
- A CertManager Issuer configured in the cluster's namespace

### Basic TLS Setup

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: secure-cluster
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
  api:
    tls:
      issuerRef: gnmic-ca-issuer
```

### TLS Fields

| Field | Type | Description |
|-------|------|-------------|
| `issuerRef` | string | Name of cert-manager Issuer in cluster's namespace. It is used to sign the PODs REST API certificates. |
| `useCSIDriver` | bool | Use cert-manager CSI driver (default: false). When enabled the PODs certificates are issued and mounted using CertManager CSI driver instead of mounting Secrets. |
| `bundleRef` | string | Additional CA bundle for REST API client verification. |

### How It Works

When TLS is enabled:

1. **Per-Pod Certificates**: The operator creates a cert-manager `Certificate` for each pod
2. **Automatic Mounting**: Certificates are mounted at `/etc/certs/api/`
3. **mTLS**: The operator authenticates to pods using client certificates
4. **CA Sync**: The operator's CA is synced to the cluster namespace as a ConfigMap

### Creating a CA Issuer

First, create a self-signed issuer and CA:

```yaml
# Self-signed issuer for bootstrapping
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: selfsigned-issuer
spec:
  selfSigned: {}
---
# CA certificate
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: gnmic-ca
spec:
  isCA: true
  commonName: gnmic-ca
  secretName: gnmic-ca-secret
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: selfsigned-issuer
    kind: Issuer
---
# CA Issuer for pod certificates
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: gnmic-ca-issuer
spec:
  ca:
    secretName: gnmic-ca-secret
```

### Using CSI Driver

For enhanced security, use the cert-manager CSI driver (certificates never written to disk):

```yaml
spec:
  api:
    tls:
      issuerRef: gnmic-ca-issuer
      useCSIDriver: true
```

**Note**: Requires [cert-manager-csi-driver](https://cert-manager.io/docs/projects/csi-driver/) to be installed.

### Resources Created for TLS

When TLS is enabled, additional resources are created:

| Resource | Name Pattern | Purpose |
|----------|--------------|---------|
| Certificate | `gnmic-{cluster}-{index}-tls` | Per-pod TLS certificate |
| Secret | `gnmic-{cluster}-{index}-tls` | Certificate and key (created by cert-manager) |
| ConfigMap | `gnmic-{cluster}-controller-ca` | Controller's CA for mTLS verification |

## Created Resources

When you create a Cluster, the operator creates:

| Resource | Name | Purpose |
|----------|------|---------|
| StatefulSet | `gnmic-{cluster-name}` | Runs gNMIc pods |
| Service (Headless) | `gnmic-{cluster-name}` | Pod DNS resolution |
| ConfigMap | `gnmic-{cluster-name}-config` | Base gNMIc configuration |
| Service (per Prometheus output) | `gnmic-{cluster-name}-prom-{output}` | Prometheus metrics endpoint |

## Status

The Cluster status shows the current state:

```yaml
status:
  readyReplicas: 3
  pipelinesCount: 2
  targetsCount: 10
  subscriptionsCount: 5
  inputsCount: 1
  outputsCount: 3
  conditions:
    - type: Ready
      status: "True"
      reason: ClusterReady
      message: "All 3 replicas are ready and configured"
    - type: ConfigApplied
      status: "True"
      reason: ConfigurationApplied
      message: "Configuration applied to 3 pods"
```

### Status Fields

| Field | Description |
|-------|-------------|
| `readyReplicas` | Number of pods that are ready |
| `pipelinesCount` | Number of enabled pipelines using this cluster |
| `targetsCount` | Total unique targets across all pipelines |
| `subscriptionsCount` | Total unique subscriptions |
| `inputsCount` | Total unique inputs |
| `outputsCount` | Total unique outputs |
| `conditions` | Standard Kubernetes conditions |

### Conditions

| Type | Description |
|------|-------------|
| `Ready` | True when all replicas are ready and configured |
| `CertificatesReady` | True when TLS certificates are issued (only present if TLS enabled) |
| `ConfigApplied` | True when configuration is successfully applied to all pods |

## Scaling

To scale the cluster, update the `replicas` field:

```bash
kubectl patch cluster telemetry-cluster --type merge -p '{"spec":{"replicas":5}}'
```

Targets are automatically redistributed across pods when scaling.

## Example: Production Cluster

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: production-telemetry
  namespace: telemetry
spec:
  replicas: 5
  image: ghcr.io/openconfig/gnmic:latest
  api:
    restPort: 7890
    tls:
      issuerRef: gnmic-op-issuer
      useCSIDriver: true
  resources:
    requests:
      memory: "256Mi"
      cpu: "200m"
    limits:
      memory: "1Gi"
      cpu: "2"
```

