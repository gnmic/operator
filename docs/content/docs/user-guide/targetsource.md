---
title: "TargetSource"
linkTitle: "TargetSource"
weight: 4
description: >
  Dynamic target discovery from external sources
---

The `TargetSource` resource enables dynamic discovery of network devices from external sources. The operator automatically creates, updates, and deletes `Target` resources based on discovered devices.

## Discovery Sources

TargetSource supports the following discovery providers:

| Source | Description |
|--------|-------------|
| `http` | Fetch targets from an HTTP endpoint |

## HTTP Discovery

Discover targets from an HTTP endpoint that returns a JSON list of targets:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: http-discovery
spec:
  provider:
    http:
      url: http://inventory-service:8080/targets
  targetProfile: default
  targetLabels:
    source: inventory
```

The HTTP endpoint should return a JSON array of target objects. The following is an example for a valid JSON array:

```json
[
  {
    "address": "spine1:57400",
    "name": "spine1",
    "labels": {
      "role": "spine"
    }
  },
  {
    "address": "leaf1:57400",
    "name": "leaf1",
    "labels": {
      "role": "leaf"
    }
  },
  {
    "address": "leaf2:57400",
    "name": "leaf2",
    "labels": {
      "role": "leaf"
    }
  }
]
```

## TargetProfile Inheritance

Within the `TargetSource`, the default `TargetProfile` for all targets can be defined using `targetProfile`. Each target discovered inherits the defined value.

## Label Inheritance

Each discovered target has a label defined to identify the owning `TargetSource`:
- `operator.gnmic.dev/targetsource: datacenter-a`


This label is needed to identify all targets owned by this resource and determine which devices get applied or removed. This label takes precedence over all other labels on the target.

Labels defined in the `TargetSource.spec.targetLabels` field are applied to all discovered targets:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: datacenter-a
spec:
  provider:
    http:
      url: http://datacenter-a:8080/targets
  targetLabels:
    datacenter: dc-a
    environment: production
```

All targets discovered from this source will have:
- `datacenter: dc-a`
- `environment: production`

This enables using label selectors in Pipelines to select targets by their discovery source.

## Labels from Source of Truth

Targets can also have labels defined by the external system. These get directly applied to the target with their original key/value pair. 

The gNMIc Operator has a reserved namespace for labels which alter the behavior of the target:
- `gnmic_operator_`

Following are all supported operator-specific labels:

| Label | Description |
|--------|-------------|
| `gnmic_operator_target_profile` | Overwrite the `TargetProfile` which is defined in the `TargetSource` |

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

<!-- ## Example: Multi-Source Discovery

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
``` -->

## Lifecycle

### Target Creation

When a TargetSource discovers a new device:
1. A new `Target` resource is created
2. The `Profile` gets specified from `spec.targetProfile`
3. Labels from `spec.targetLabels` are applied
4. Owner reference is set to the TargetSource

### Target Updates

Discovered devices get reapplied each time the target gets discovered, overwriting any changes manually made:
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

