---
title: "TLS Configuration"
linkTitle: "TLS"
weight: 3
description: >
  Secure communication in gNMIc Operator
---

The gNMIc Operator supports multiple TLS configurations for different communication paths:

| TLS Type | Config Location | Purpose |
|----------|-----------------|---------|
| **API TLS** | `cluster.spec.api.tls` | Operator ↔ gNMIc pod REST API |
| **Client TLS** | `cluster.spec.clientTLS` | gNMIc pod → Network target gNMI |
| **Tunnel TLS** | `cluster.spec.grpcTunnel.tls` | Network device → gNMIc pod tunnel |

## API TLS (Operator ↔ Pods)

This TLS configuration secures the REST API communication between the operator controller and gNMIc collector pods.

## Overview

When TLS is enabled:

1. **Server TLS**: Each gNMIc pod presents a certificate to the operator
2. **Client TLS (mTLS)**: The operator presents a certificate to gNMIc pods
3. **Certificate Verification**: Both sides verify the other's certificate

## Prerequisites

1. **cert-manager** must be installed in your cluster:

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.19.2/cert-manager.yaml
```

2. **A CA Issuer** must be configured in the gNMIc cluster's namespace

## Quick Start

Assuming the gNMIc cluster will be created in the `default` namespace. Start by preparing an Issuer to secure the Cluster's REST API.

### 1. Create a CA Issuer

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

### 2. Create a TLS-enabled Cluster

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: secure-cluster
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
  api:
    restPort: 7890
    tls:
      issuerRef: gnmic-ca-issuer
```

## Certificate Modes

### Projected Volumes (Default)

When `useCSIDriver: false` (default):

- The operator creates a cert-manager `Certificate` CR per pod
- cert-manager creates a `Secret` per pod
- Secrets are mounted via Kubernetes projected volumes

**Advantages:**
- Works with any cert-manager installation
- Certificates are ready before pod starts
- Easy to inspect certificates

### CSI Driver

When `useCSIDriver: true`:

```yaml
spec:
  api:
    tls:
      issuerRef: gnmic-ca-issuer
      useCSIDriver: true
```

**Advantages:**
- Certificates never written to disk
- Automatic renewal handled by driver
- More secure for sensitive environments

**Requirements:**
- [cert-manager-csi-driver](https://cert-manager.io/docs/projects/csi-driver/) must be installed

## How It Works

### Certificate Creation

For a 3-replica cluster named `my-cluster`:

```
Certificate: gnmic-my-cluster-0-tls
  → Secret: gnmic-my-cluster-0-tls
  → CN: gnmic-my-cluster-0
  → DNS SANs:
    - gnmic-my-cluster-0
    - gnmic-my-cluster-0.gnmic-my-cluster.default.svc

Certificate: gnmic-my-cluster-1-tls
  ...

Certificate: gnmic-my-cluster-2-tls
  ...
```

### Controller CA Distribution

The operator syncs its CA certificate to the cluster namespace:

```
ConfigMap: gnmic-my-cluster-controller-ca
  └── ca.crt: <controller's CA certificate>
```

This allows gNMIc pods to verify the operator's client certificate.

### Certificate Verification Flow

```
┌──────────────────┐                    ┌──────────────────┐
│  Controller      │                    │  gNMIc Pod       │
│                  │                    │                  │
│  1. Connect      │───────────────────►│                  │
│                  │                    │  2. Present      │
│                  │◄───────────────────│     server cert  │
│  3. Verify cert  │                    │                  │
│     using Issuer │                    │                  │
│     CA           │                    │                  │
│                  │                    │                  │
│  4. Present      │───────────────────►│                  │
│     client cert  │                    │  5. Verify cert  │
│                  │                    │     using        │
│                  │                    │     controller CA│
│                  │                    │                  │
│  6. mTLS         │◄──────────────────►│  Connection      │
│     established  │                    │  established     │
└──────────────────┘                    └──────────────────┘
```

## Scaling with TLS

When scaling a TLS-enabled cluster:

### Scale Up

1. Operator creates new `Certificate` CRs for new pods
2. cert-manager issues certificates
3. Operator waits for certificates to be ready
4. StatefulSet creates new pods with certificates

### Scale Down

1. StatefulSet terminates pods
2. Operator cleans up orphaned `Certificate` CRs
3. cert-manager cleans up associated secrets

## Troubleshooting

### Check Certificate Status

```bash
# List certificates for a cluster
kubectl get certificates -l operator.gnmic.dev/cluster-name=my-cluster

# Check certificate details
kubectl describe certificate gnmic-my-cluster-0-tls
```

### Check Secrets

```bash
# List TLS secrets
kubectl get secrets -l operator.gnmic.dev/cluster-name=my-cluster

# Inspect certificate content
kubectl get secret gnmic-my-cluster-0-tls -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout
```

### Check Controller CA ConfigMap

```bash
kubectl get configmap gnmic-my-cluster-controller-ca -o yaml
```

### Common Issues

**Certificates not ready:**

```bash
# Check cert-manager logs
kubectl logs -n cert-manager deploy/cert-manager

# Check certificate conditions
kubectl get certificate gnmic-my-cluster-0-tls -o jsonpath='{.status.conditions}'
```

**Issuer not found:**

Ensure the Issuer exists in the same namespace as the Cluster:

```bash
kubectl get issuers
```

**Connection refused:**

Check that pods have the certificates mounted:

```bash
kubectl exec gnmic-my-cluster-0 -- ls -la /etc/certs/api/
```

## Security Best Practices

1. **Use a dedicated CA** for gNMIc pods, separate from your organization's root CA
2. **Rotate certificates** regularly by configuring cert-manager renewal settings
3. **Use CSI driver** in production for certificates that never touch disk
4. **Limit Issuer scope** - use namespace-scoped Issuers, not ClusterIssuers
5. **Monitor certificate expiry** using cert-manager metrics

## Example: Production Setup (API TLS)

```yaml
# CA Issuer with 1-year validity
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: gnmic-ca
spec:
  isCA: true
  commonName: gnmic-production-ca
  secretName: gnmic-ca-secret
  duration: 8760h  # 1 year
  renewBefore: 720h  # 30 days
  privateKey:
    algorithm: ECDSA
    size: 384
  issuerRef:
    name: selfsigned-issuer
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: gnmic-ca-issuer
spec:
  ca:
    secretName: gnmic-ca-secret
---
# Production cluster with TLS
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: production-telemetry
spec:
  replicas: 5
  image: ghcr.io/openconfig/gnmic:0.37.0
  api:
    restPort: 7890
    tls:
      issuerRef: gnmic-ca-issuer
      useCSIDriver: true  # Recommended for production
  resources:
    requests:
      memory: "256Mi"
      cpu: "200m"
    limits:
      memory: "1Gi"
      cpu: "2"
```

---

## Client TLS (Pods → Targets)

Client TLS enables gNMIc pods to authenticate to network devices using client certificates. This is essential when your network devices require mutual TLS (mTLS) for gNMI connections.

### Use Cases

- Network devices require client certificate authentication
- Security policies mandate certificate-based authentication
- Zero-trust network architecture

### Configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: mtls-cluster
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
  clientTLS:
    issuerRef: gnmic-client-ca-issuer
    bundleRef: target-ca-bundle  # Optional: CA to verify target certs
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `issuerRef` | string | cert-manager Issuer to sign client certificates |
| `useCSIDriver` | bool | Use CSI driver for certificate mounting |
| `bundleRef` | string | ConfigMap with CA bundle for target verification |

### How Client TLS Works

```
┌──────────────────┐                    ┌──────────────────┐
│  gNMIc Pod       │                    │  Network Device  │
│                  │                    │                  │
│  1. Connect      │───────────────────►│                  │
│                  │                    │  2. Present      │
│                  │◄───────────────────│     server cert  │
│  3. Verify cert  │                    │                  │
│     (if bundleRef│                    │                  │
│      configured) │                    │                  │
│                  │                    │                  │
│  4. Present      │───────────────────►│                  │
│     client cert  │                    │  5. Verify cert  │
│                  │                    │     against CA   │
│                  │                    │                  │
│  6. gNMI         │◄──────────────────►│  Connection      │
│     session      │                    │  established     │
└──────────────────┘                    └──────────────────┘
```

### Certificate Details

A **single certificate** is shared by all pods in the cluster:

- **CommonName**: `{cluster-name}.{namespace}` ( `my-cluster.telemetry`)
- **DNSNames**: Same as CommonName
- **Usages**: ClientAuth, DigitalSignature, KeyEncipherment

This design enables seamless scaling without certificate regeneration or pod restarts.

### Certificate Paths

The shared client certificate is mounted in all gNMIc pods at:

| File | Path |
|------|------|
| Certificate | `/etc/gnmic/client-tls/tls.crt` |
| Private Key | `/etc/gnmic/client-tls/tls.key` |
| CA (from issuer) | `/etc/gnmic/client-tls/ca.crt` |
| CA Bundle (bundleRef) | `/etc/gnmic/client-ca/ca.crt` |

### Target Configuration

When `clientTLS` is configured, the operator automatically adds TLS settings to all target configurations:

```yaml
# Automatically applied to target configs
tls-cert: /etc/gnmic/client-tls/tls.crt
tls-key: /etc/gnmic/client-tls/tls.key
tls-ca: /etc/gnmic/client-ca/ca.crt  # If bundleRef is set
skip-verify: false  # If bundleRef is set
```

### Resources Created

| Resource | Name Pattern | Purpose |
|----------|--------------|---------|
| Certificate | `gnmic-{cluster}-client-tls` | Shared client certificate (all pods) |
| Secret | `gnmic-{cluster}-client-tls` | Certificate and key |

### Example: Full mTLS Setup

```yaml
# 1. Create CA for signing client certificates
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: gnmic-client-ca
spec:
  isCA: true
  commonName: gnmic-client-ca
  secretName: gnmic-client-ca-secret
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: selfsigned-issuer
---
# 2. Create Issuer using the CA
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: gnmic-client-ca-issuer
spec:
  ca:
    secretName: gnmic-client-ca-secret
---
# 3. Create ConfigMap with target CA certificates
# (The CA that signed your network devices' certificates)
apiVersion: v1
kind: ConfigMap
metadata:
  name: target-ca-bundle
data:
  ca.crt: |
    -----BEGIN CERTIFICATE-----
    # Your network devices' CA certificate here
    -----END CERTIFICATE-----
---
# 4. Create Cluster with Client TLS
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: secure-telemetry
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
  api:
    restPort: 7890
    tls:
      issuerRef: gnmic-api-issuer  # Separate issuer for API TLS
  clientTLS:
    issuerRef: gnmic-client-ca-issuer
    bundleRef: target-ca-bundle
    useCSIDriver: true  # Recommended for production
```

### Troubleshooting Client TLS

**Check client certificate status:**

```bash
kubectl get certificates -l operator.gnmic.dev/cert-type=client
```

**Verify certificate is mounted:**

```bash
kubectl exec gnmic-my-cluster-0 -- ls -la /etc/gnmic/client-tls/
```

**Check certificate details:**

```bash
kubectl get secret gnmic-my-cluster-0-client-tls -o jsonpath='{.data.tls\.crt}' | \
  base64 -d | openssl x509 -text -noout
```

**Verify CA bundle:**

```bash
kubectl exec gnmic-my-cluster-0 -- cat /etc/gnmic/client-ca/ca.crt
```

---

## TLS Summary

| Configuration | Purpose | Key Fields |
|---------------|---------|------------|
| `api.tls` | Secure operator ↔ pod communication | `issuerRef`, `useCSIDriver`, `bundleRef` |
| `clientTLS` | Secure pod → target gNMI connections | `issuerRef`, `useCSIDriver`, `bundleRef` |
| `grpcTunnel.tls` | Secure device → pod tunnel connections | `issuerRef`, `useCSIDriver`, `bundleRef` |

All three TLS configurations use the same `ClusterTLSConfig` structure and support both projected volumes and CSI driver modes.

