---
title: "TargetSource"
linkTitle: "TargetSource"
weight: 4
description: >
  Dynamic target discovery from external sources
---

The `TargetSource` resource enables dynamic discovery of network devices from external sources. The operator automatically creates, updates, and deletes `Target` resources based on discovered devices.

## Discovery Sources

TargetSource supports multiple discovery backends:

| Source | Description |
|--------|-------------|
| `http` | Fetch targets from an HTTP endpoint |
| `consul` | Discover targets from Consul service registry |
| `configMap` | Read targets from a Kubernetes ConfigMap |
| `podSelector` | Create targets from Kubernetes Pods |
| `serviceSelector` | Create targets from Kubernetes Services |

## HTTP Discovery

Discover targets from an HTTP endpoint that returns a JSON list of targets:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: http-discovery
spec:
  http:
    url: http://inventory-service:8080/targets
  labels:
    source: inventory
```

The HTTP endpoint should return a JSON array of target objects.

## Consul Discovery

Discover targets from Consul service registry:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: consul-discovery
spec:
  consul:
    url: http://consul:8500
  labels:
    source: consul
    datacenter: dc1
```

## ConfigMap Discovery

Read targets from a Kubernetes ConfigMap:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: configmap-targets
spec:
  configMap: network-devices
  labels:
    source: configmap
```

The ConfigMap should contain target definitions in a structured format.

## Kubernetes Pod Discovery

Create targets from Kubernetes Pods matching a label selector:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: pod-discovery
spec:
  podSelector:
    matchLabels:
      app: network-simulator
      gnmi: enabled
  labels:
    source: kubernetes
    type: simulator
```

This is useful for:
- Containerized network simulators
- Virtual network functions (VNFs)
- Development/testing environments

## Kubernetes Service Discovery

Create targets from Kubernetes Services matching a label selector:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: service-discovery
spec:
  serviceSelector:
    matchLabels:
      protocol: gnmi
  labels:
    source: kubernetes
```

## Label Inheritance

Labels defined in the `TargetSource.spec.labels` field are applied to all discovered targets:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: datacenter-a
spec:
  consul:
    url: http://consul-dc-a:8500
  labels:
    datacenter: dc-a
    environment: production
    source: consul
```

All targets discovered from this source will have:
- `datacenter: dc-a`
- `environment: production`
- `source: consul`

This enables using label selectors in Pipelines to select targets by their discovery source.

## Status

The TargetSource status shows discovery state:

```yaml
status:
  status: Synced
  targetsCount: 42
  lastSync: "2024-01-15T10:30:00Z"
```

| Field | Description |
|-------|-------------|
| `status` | Current sync status (Synced, Error, Pending) |
| `targetsCount` | Number of targets discovered |
| `lastSync` | Timestamp of last successful sync |

## Example: Multi-Source Discovery

Combine multiple TargetSources for different environments:

```yaml
# Production devices from Consul
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: prod-consul
spec:
  consul:
    url: http://consul-prod:8500
  labels:
    environment: production
    source: consul
---
# Lab devices from ConfigMap
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: lab-devices
spec:
  configMap: lab-network-devices
  labels:
    environment: lab
    source: configmap
---
# Simulator pods
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: simulators
spec:
  podSelector:
    matchLabels:
      app: srlinux
  labels:
    environment: dev
    source: kubernetes
```

Then use label selectors in your Pipeline:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: production-telemetry
spec:
  clusterRef: prod-cluster
  enabled: true
  targetSelectors:
    - matchLabels:
        environment: production
  # ... subscriptions, outputs
```

## Lifecycle

### Target Creation

When a TargetSource discovers a new device:
1. A new `Target` resource is created
2. Labels from `spec.labels` are applied
3. Owner reference is set to the TargetSource

### Target Updates

When a discovered device's properties change:
1. The corresponding `Target` is updated
2. Clusters using that target are reconciled

### Target Deletion

When a device is no longer discovered:
1. The `Target` resource is deleted
2. Clusters stop collecting from that target

### TargetSource Deletion

When a TargetSource is deleted:
1. All Targets owned by it are deleted (via owner references)
2. Clusters are reconciled to remove those targets

