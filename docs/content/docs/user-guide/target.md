---
title: "Target & TargetProfile"
linkTitle: "Target"
weight: 3
description: >
  Configuring network device targets
---

## Target

The `Target` resource represents a network device to collect telemetry from.

### Basic Configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: router1
  labels:
    vendor: vendorA
    role: core
    site: dc1
spec:
  address: 10.0.0.1:57400
  profile: default-profile
```

### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `address` | string | Yes | Device address (host:port) |
| `profile` | string | Yes | Reference to TargetProfile |

### Using Labels

Labels are essential for pipeline selection:

```yaml
metadata:
  labels:
    vendor: vendorA      # Device vendor
    role: core         # Network role
    site: datacenter-1 # Physical location
    env: production    # Environment
    tier: critical     # Importance level
```

Pipelines can then select targets:

```yaml
# Select all vendorA core routers in production
targetSelectors:
  - matchLabels:
      vendor: vendorA
      role: core
      env: production
```

## TargetProfile

The `TargetProfile` resource defines shared connection settings for targets.

### Basic Configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: default-profile
spec:
  credentialsRef: device-credentials
  tls: {}
  encoding: json_ietf
  timeout: 10s
```

### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `credentialsRef` | string | No | Reference to credentials Secret |
| `insecure` | bool | No | Skip TLS verification |
| `skipVerify` | bool | No | Skip certificate verification |
| `timeout` | duration | No | Connection timeout |
| `tlsCA` | string | No | TLS CA certificate |
| `tlsCert` | string | No | TLS client certificate |
| `tlsKey` | string | No | TLS client key |

### Credentials Secret

Create a Secret with device credentials:

```bash
kubectl create secret generic device-credentials \
  --from-literal=username=admin \
  --from-literal=password=secretpassword
```

Or with a token:

```bash
kubectl create secret generic device-credentials \
  --from-literal=token=eyJhbGciOiJIUzI1NiIs...
```

Reference it in the profile:

```yaml
spec:
  credentialsRef: device-credentials
```

### TLS Configuration

For production environments with TLS:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: secure-profile
spec:
  credentialsRef: device-credentials
  insecure: false
  skipVerify: false
  timeout: 30s
  # TLS certificates can be provided via secrets
```

### Multiple Profiles

Use different profiles for different device types:

```yaml
# Profile for vendorA devices
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: vendorA-profile
spec:
  credentialsRef: vendorA-credentials
  tls: {}
  encoding: json_ietf
  timeout: 10s
---
# Profile for vendorB devices
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: vendorB-profile
spec:
  credentialsRef: vendorB-credentials
  tls: {}
  encoding: json_ietf
  timeout: 15s
```

Then reference the appropriate profile in each target:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: vendorA-router1
spec:
  address: 10.0.0.1:57400
  profile: vendorA-profile
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: vendorB-router1
spec:
  address: 10.0.0.2:57400
  profile: vendorB-profile
```

