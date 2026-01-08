---
title: "TunnelTargetPolicy"
linkTitle: "TunnelTargetPolicy"
weight: 10
description: >
  Configuring gRPC tunnel target matching policies
---

The `TunnelTargetPolicy` resource defines rules for matching devices that connect via gRPC tunnel and associates them with configuration from a `TargetProfile`.

## Overview

In gRPC tunnel mode, network devices initiate connections to the gNMIc collector (reverse of traditional polling). When a device connects, it identifies itself with a **type** and **ID**. The `TunnelTargetPolicy` defines matching rules to:

1. Identify which tunnel-connected devices to accept
2. Apply configuration (credentials, TLS settings) from a TargetProfile
3. Enable subscription collection on matching devices

## Basic Configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TunnelTargetPolicy
metadata:
  name: core-routers
spec:
  match:
    type: "router"
    id: "^core-.*"
  profile: router-profile
```

## Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `match` | TunnelTargetMatch | No | Match criteria (if not set, matches all targets) |
| `match.type` | string | No | Regex pattern to match target type |
| `match.id` | string | No | Regex pattern to match target ID |
| `profile` | string | Yes | Reference to a TargetProfile |

## Match Patterns

Both `type` and `id` fields support Go regular expressions.

### Match All Targets

Omit the `match` field to match all tunnel-connected devices:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TunnelTargetPolicy
metadata:
  name: catch-all
spec:
  profile: default-profile
```

### Match by Type Only

```yaml
spec:
  match:
    type: "router"  # Exact match
  profile: router-profile
```

### Match by ID Pattern

```yaml
spec:
  match:
    id: "^dc1-.*"  # All devices starting with "dc1-"
  profile: dc1-profile
```

### Complex Patterns

```yaml
spec:
  match:
    type: "^(router|switch)$"      # router OR switch
    id: "^(core|edge)-[0-9]+$"     # core-N or edge-N
  profile: network-profile
```

## Usage in Pipelines

TunnelTargetPolicies are selected in `Pipeline` resources:

### Direct References

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: tunnel-pipeline
spec:
  clusterRef: tunnel-cluster
  enabled: true
  tunnelTargetPolicyRefs:
    - core-routers
    - edge-switches
  subscriptionRefs:
    - interface-counters
  outputs:
    outputRefs:
      - prometheus-output
```

### Label Selectors

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: tunnel-pipeline
spec:
  clusterRef: tunnel-cluster
  enabled: true
  tunnelTargetPolicySelectors:
    - matchLabels:
        tier: core
    - matchLabels:
        tier: edge
  subscriptionRefs:
    - interface-counters
  outputs:
    outputRefs:
      - prometheus-output
```

### Mixed Selection

```yaml
spec:
  tunnelTargetPolicyRefs:
    - special-devices
  tunnelTargetPolicySelectors:
    - matchLabels:
        env: production
```

## Prerequisites

### Cluster with gRPC Tunnel

The referenced cluster must have gRPC tunnel enabled:

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
    service:
      type: LoadBalancer
```

If a pipeline references tunnel target policies but the cluster doesn't have `grpcTunnel` configured, the pipeline status will show an error.

### TargetProfile

Create a TargetProfile with the configuration to apply to matching devices:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: router-profile
spec:
  credentialsRef: router-credentials
  insecure: false
  skipVerify: false
  timeout: 10s
```

## Complete Example

```yaml
# 1. Credentials for routers
apiVersion: v1
kind: Secret
metadata:
  name: router-credentials
type: Opaque
stringData:
  username: admin
  password: secret123
---
# 2. TargetProfile with router configuration
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: router-profile
spec:
  credentialsRef: router-credentials
  timeout: 30s
  skipVerify: true
---
# 3. TunnelTargetPolicy matching core routers
apiVersion: operator.gnmic.dev/v1alpha1
kind: TunnelTargetPolicy
metadata:
  name: core-routers
  labels:
    tier: core
spec:
  match:
    type: "router"
    id: "^core-rtr-.*"
  profile: router-profile
---
# 4. Cluster with gRPC tunnel enabled
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: tunnel-cluster
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
  grpcTunnel:
    port: 57400
    tls:
      issuerRef: gnmic-ca-issuer
    service:
      type: LoadBalancer
---
# 5. Subscription for interface counters
apiVersion: operator.gnmic.dev/v1alpha1
kind: Subscription
metadata:
  name: interface-counters
spec:
  paths:
    - /interfaces/interface/state/counters
  mode: stream
  streamMode: sample
  sampleInterval: 10s
---
# 6. Output to Prometheus
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-output
spec:
  type: prometheus
  config:
    listen: ":9804"
    path: /metrics
---
# 7. Pipeline connecting everything
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: tunnel-telemetry
spec:
  clusterRef: tunnel-cluster
  enabled: true
  tunnelTargetPolicySelectors:
    - matchLabels:
        tier: core
  subscriptionRefs:
    - interface-counters
  outputs:
    outputRefs:
      - prometheus-output
```

## How It Works

1. Network devices connect to the gNMIc tunnel service (e.g., `tunnel-cluster-tunnel:57400`)
2. Devices identify themselves with type and ID via the gRPC tunnel Register RPC
3. gNMIc matches incoming devices against `TunnelTargetPolicy` rules
4. Matching devices receive configuration from the referenced `TargetProfile`
5. Subscriptions from the pipeline are applied to matched devices
6. Telemetry data flows to configured outputs

## Multiple Policies

Multiple policies can match the same device. Policies are processed in the order they appear in the pipeline (refs first, then selector-matched sorted by name):

```yaml
# Policy A - specific config for critical devices
apiVersion: operator.gnmic.dev/v1alpha1
kind: TunnelTargetPolicy
metadata:
  name: critical-routers
spec:
  match:
    id: "^critical-.*"
  profile: critical-profile
---
# Policy B - catch-all for remaining devices
apiVersion: operator.gnmic.dev/v1alpha1
kind: TunnelTargetPolicy
metadata:
  name: default-policy
spec:
  profile: default-profile
```

## Status

The pipeline status shows the count of resolved tunnel target policies:

```yaml
status:
  status: Active
  tunnelTargetPoliciesCount: 3
  targetsCount: 0
  subscriptionsCount: 5
  outputsCount: 2
```

Note: `targetsCount` shows static targets only. Tunnel targets are dynamic and matched at runtime.

