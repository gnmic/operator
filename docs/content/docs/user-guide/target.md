---
title: "Target & TargetProfile"
linkTitle: "Target"
weight: 3
description: >
  Configuring network device targets
---

## Target

The `Target` resource represents a network device to collect telemetry from.
The target definition is kept as simple as possible to remain automation and scale friendly.

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

Labels are essential for pipeline selection.
Any label can be used but some obvious ones include:

```yaml
metadata:
  labels:
    vendor: vendorA    # Device vendor
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
| `tls.serverName` | string | No | TLS serverName override value |
| `tls.maxVersion` | string | No | TLS Maximum version: 1.1, 1.2 or 1.3 |
| `tls.minVersion` | string | No | TLS Minimum version: 1.1, 1.2 or 1.3 |
| `tls.cipherSuites` | []string | No | List of supported TLS cipher suites |
| `timeout` | duration | No | gRPC timeout, defaults to 10s |
| `retryTimer` | duration | No | gNMI RPC retry timer, defaults to 2s |
| `encoding` | string | NO | gNMI encoding. Is overwritten by the subscription encoding |
| `labels` | map | NO | A set of labels added to the device's exported metrics |
| `proxy` | string | NO | A socks5 proxy address used to reach the targets |
| `gzipCompression` | bool | NO | If true, gzip is used to compress gNMI data on the wire |
| `tcpKeepAlive` | duration | NO | TCP keepalive duration |
| `grpcKeepAlive.time` | duration | NO | gRPC keep alive time (interval) |
| `grpcKeepAlive.timeout` | duration | NO | gRPC keep alive timeout |
| `grpcKeepAlive.permitWithoutStream` | bool | NO | If true gRPC keepalives are sent when there is no active stream |

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

## TLS Configuration

The `TargetProfile` controls **connection-level TLS settings** for gNMI connections. For **client certificate authentication (mTLS)**, see [Cluster Client TLS]({{< ref "../user-guide/cluster#gnmi-client-tls-target-connections" >}}).

### TLS Configuration Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                        TLS Configuration                            │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│   TargetProfile.tls          Cluster.clientTLS                      │
│   ─────────────────          ─────────────────                      │
│   • Enable/disable TLS       • Client certificate (tls-cert)        │
│   • TLS version settings     • Client private key (tls-key)         │
│   • Cipher suites            • CA bundle for server verification    │
│   • Server name override                                            │
│                                                                     │
│              Both combine to form the complete TLS config           │
└─────────────────────────────────────────────────────────────────────┘
```

### No TLS (Insecure - Not Recommended)

When `tls` is not specified, connections are unencrypted:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: insecure-profile
spec:
  credentialsRef: device-credentials
```

{{% alert title="Warning" color="warning" %}}
Insecure connections transmit credentials and telemetry data in plaintext. Only use for testing or isolated lab environments.
{{% /alert %}}

### TLS without Server Certificate Verification

Enable TLS encryption but skip verification of the device's certificate. This is common when devices use self-signed certificates:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: tls-skip-verify-profile
spec:
  credentialsRef: device-credentials
  tls: {}  # Enables TLS with skip-verify
```

**Note**: At the cluster level, a `clientTLS.issuerRef` can still be set - the client will send its certificates during the TLS handshake 
but it will not verify the server certificates as long as the `clientTLS.bundleRef` is not set.

This results in:
- ✅ Encrypted connection
- ❌ No verification of device identity (vulnerable to MITM)

### Mutual TLS (mTLS) with Client Certificates

For full mTLS where gNMIc authenticates to devices using client certificates, configure `clientTLS` at the **Cluster level**:

```yaml
# 1. TargetProfile - connection settings
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: mtls-profile
spec:
  credentialsRef: device-credentials
  tls:
    minVersion: "1.2"
---
# 2. Cluster - client certificates for mTLS
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: secure-cluster
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
  clientTLS:
    issuerRef: gnmic-client-ca-issuer  # cert-manager Issuer
    bundleRef: target-ca-bundle        # CA to verify device certs
```

This results in:
- ✅ Encrypted connection
- ✅ gNMIc authenticates to devices with client certificate
- ✅ Device server certificates verified against CA bundle

See [Cluster Client TLS]({{< ref "../user-guide/cluster#gnmi-client-tls-target-connections" >}}) for complete setup instructions.

### TLS with Custom Settings

Configure specific TLS parameters:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: custom-tls-profile
spec:
  credentialsRef: device-credentials
  tls:
    serverName: router1.example.com   # Override SNI
    minVersion: "1.2"                 # Minimum TLS 1.2
    maxVersion: "1.3"                 # Maximum TLS 1.3
    cipherSuites:                     # Restrict cipher suites
      - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
      - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
```

**Note**: For `serverName` setting to apply the cluster spec must include a trustBundle under `spec.clientTLS.trustBundle` 

### TLS Configuration Summary

| Scenario | TargetProfile Config | Cluster Config 
|----------|---------------------|----------------|
| No TLS | `tls` not set | - |
| TLS (skip verify) | `tls: {}` | - |
| TLS + client verify | `tls: {}` | `clientTLS.issuerRef` only |
| TLS + server verify | `tls: {}` | `clientTLS.bundleRef` only |
| Full mTLS | `tls: {}` | `clientTLS.issuerRef` + `clientTLS.bundleRef` |

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

