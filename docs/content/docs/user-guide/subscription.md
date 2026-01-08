---
title: "Subscription"
linkTitle: "Subscription"
weight: 5
description: >
  Configuring telemetry subscriptions
---

The `Subscription` resource defines what telemetry data to collect from targets.

## Basic Configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Subscription
metadata:
  name: interface-counters
  labels:
    type: interfaces
spec:
  paths:
    - /interfaces/interface/state/counters
  mode: STREAM/SAMPLE
  sampleInterval: 10s
```

## Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `paths` | []string | Yes | YANG paths to subscribe to |
| `mode` | string | No | Subscription mode: `ONCE`, `STREAM/SAMPLE`, `STREAM/ON_CHANGE`, `STREAM/TARGET_DEFINED`, `POLL` |
| `sampleInterval` | duration | No | Sampling interval for `STREAM/SAMPLE` mode |
| `encoding` | string | No | Data encoding: `json`, `json_ietf`, `proto`, `ascii` |
| `prefix` | string | No | Common path prefix |

## Subscription Modes

### Stream Mode (Default)

Continuous streaming of telemetry data:

```yaml
spec:
  mode: STREAM/SAMPLE
  sampleInterval: 10s
```

Stream sub-modes:
- `SAMPLE`: Periodic sampling at fixed intervals
- `ON_CHANGE`: Updates only when values change
- `TARGET_DEFINED`: Device decides when to send updates

### Once Mode

Single request/response:

```yaml
spec:
  mode: ONCE
  paths:
    - /system/state
```

### Poll Mode

Polling at client-defined intervals:

```yaml
spec:
  mode: POLL
  paths:
    - /interfaces/interface/state
```

## Path Examples

### Interface Statistics

```yaml
spec:
  paths:
    - /interfaces/interface/state/counters
    - /interfaces/interface/state/oper-status
```

### BGP State

```yaml
spec:
  paths:
    - /network-instances/network-instance/protocols/protocol/bgp/neighbors/neighbor/state
```

### System Information

```yaml
spec:
  paths:
    - /system/state
    - /system/memory/state
    - /system/cpu/state
```

### Using Wildcards

```yaml
spec:
  paths:
    # All interfaces
    - /interfaces/interface[name=*]/state/counters
    # Specific interface
    - /interfaces/interface[name=ethernet-1/1]/state/counters
```

## Using Labels

Label subscriptions for pipeline selection:

```yaml
metadata:
  labels:
    type: interfaces
    priority: high
    team: network-ops
```

Select in pipeline:

```yaml
subscriptionSelectors:
  - matchLabels:
      type: interfaces
      priority: high
```

## Examples

### High-Frequency Interface Monitoring

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Subscription
metadata:
  name: interface-highfreq
  labels:
    type: interfaces
    frequency: high
spec:
  paths:
    - /interfaces/interface/state/counters
  mode: STREAM/SAMPLE
  sampleInterval: 1s
  encoding: PROTO
```

### On-Change BGP Monitoring

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Subscription
metadata:
  name: bgp-state
  labels:
    type: bgp
spec:
  paths:
    - /network-instances/network-instance/protocols/protocol/bgp/neighbors/neighbor/state
  mode: STREAM/ON_CHANGE
  encoding: json_ietf
```

### Comprehensive System Health

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Subscription
metadata:
  name: system-health
  labels:
    type: system
spec:
  paths:
    - /system/state
    - /system/memory/state
    - /system/cpu/state
    - /system/processes/process/state
  mode: STREAM/SAMPLE
  sampleInterval: 30s
```

