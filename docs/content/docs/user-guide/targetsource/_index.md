---
title: "TargetSource"
linkTitle: "TargetSource"
weight: 4
description: >
  Dynamic target discovery from external sources
---

The `TargetSource` resource enables dynamic discovery of network devices from external sources. The operator automatically creates, updates, and deletes `Target` resources based on discovered devices.

## Basic Configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: targetsource-1
spec:
  provider:
    # see Discovery Providers section
  targetPort: 57400
  targetProfile: default
  targetLabels:
    source: inventory
```

## Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | object | Yes | Provider-specific discovery configuration. Exactly one provider must be configured |
| `targetPort` | int32 | No | Default port used if the discovered target does not provide a port. |
| `targetProfile` | string | Yes | Reference to `TargetProfile` applied to all targets |
| `targetLabels` | map[string]string | No | Labels added to all discovered targets |


## Discovery Providers

`TargetSource` supports the following discovery providers:

| Provider | Description |
|----------|-------------|
| `http` | Discover targets from an HTTP JSON endpoint. [Configuration]({{< relref "http.md" >}}) |


## Label Inheritance

Each generated `Target` receives an ownership label identifying the originating `TargetSource`:
```yaml
operator.gnmic.dev/targetsource: targetsource-1
```

This label is automatically managed by the operator and is used to:
- Identify targets owned by a specific `TargetSource`
- Determine which targets should be updated or deleted during reconciliation

The `operator.gnmic.dev/targetsource` label is reserved and always takes precedence over any provider-supplied labels.

### TargetSource Labels

Additional labels can be applied to all generated targets using `spec.targetLabels`:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: targetsource-1
spec:
  provider:
    http:
      url: http://targetsource-1:8080/targets
  targetLabels:
    datacenter: dc-a
    environment: production
```

All targets discovered from this source will have:
- `datacenter: dc-a`
- `environment: production`

This enables Pipelines to select targets using label selectors.

### Labels from Discovery Providers

Discovery providers may return additional labels for each target. These labels are applied directly to the generated `Target` resource.

The `gnmic_operator_` label prefix is reserved for operator-specific behavior. Labels using this prefix are interpreted by the operator and are not applied directly to the generated `Target` resource.

Supported operator labels:

| Label | Description |
|--------|-------------|
| `gnmic_operator_target_profile` | Overrides the `TargetProfile` configured in the `TargetSource` |

### Label Precedence

If the same label key is defined in multiple places, labels are applied in the following order (highest precedence first):

1. `TargetSource` ownership label (`operator.gnmic.dev/targetsource`)
2. Labels from `TargetSource.spec.targetLabels`
3. Labels returned by the discovery provider

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

When a `TargetSource` discovers a new device:

1. A new `Target` resource is created
2. The `TargetProfile` referenced in `spec.targetProfile` is assigned
3. Labels from `spec.targetLabels` are applied
4. The `TargetSource` is set as the owner reference

### Target Updates

On each discovery cycle, existing `Target` resources are reconciled with the latest discovered state:

1. The corresponding `Target` resource is updated and overwritten
2. Clusters consuming the target are reconciled automatically

> Manual changes to `Target` resources managed by a `TargetSource` are overwritten on every reconciliation cycle.

### Target Deletion

When a device is no longer returned by the discovery provider:

1. The corresponding `Target` resource is deleted
2. Clusters automatically stop using the target

### TargetSource Deletion

When a `TargetSource` is deleted:

1. All `Target` resources owned by it are deleted via owner references
2. Clusters are reconciled and remove the deleted targets

