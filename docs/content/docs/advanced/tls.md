---
title: "TLS Configuration"
linkTitle: "TLS"
weight: 3
description: >
  Secure communication between the operator and gNMIc pods
---

The gNMIc Operator supports TLS encryption and mutual TLS (mTLS) for secure communication between the operator controller and gNMIc collector pods.

## Scope

This TLS configuration applies to the **REST API** communication between the operator and gNMIc pods. It does **not** apply to:

- **Target connections**: TLS for gNMI connections to network devices is configured in the `TargetProfile` CR
- **Output connections**: TLS for outputs (Kafka, InfluxDB, etc.) is configured in the `Output` CR

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

## Example: Production Setup

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

