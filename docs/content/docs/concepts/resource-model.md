---
title: "Resource Model"
linkTitle: "Resource Model"
weight: 2
description: >
  Understanding the Custom Resource model
---

## Overview

The gNMIc Operator uses a set of Custom Resource Definitions (CRDs) to model telemetry infrastructure. The resources are designed to be composable and reusable.

## Resource Hierarchy

```
                    ┌─────────────────┐
                    │     Cluster     │
                    │  (deployment)   │
                    └────────┬────────┘
                             │
                             │ references
                             ▼
                    ┌─────────────────┐
                    │    Pipeline     │
                    │   (wiring)      │
                    └────────┬────────┘
                             │
       ┌─────────────────────┼─────────────────────┐
       │                     │                     │
       ▼                     ▼                     ▼
┌──────────────┐    ┌─────────────────┐    ┌──────────────┐
│   Targets    │    │  Subscriptions  │    │   Outputs    │
│  (devices)   │    │     (data)      │    │(destinations)│
└──────┬───────┘    └─────────────────┘    └──────────────┘
       │
       │ references
       ▼
┌───────────────┐
│ TargetProfile │◀────references────┐
│ (credentials) │                   │
└───────────────┘                   │
                          ┌─────────────────────┐
┌─────────────────┐       │ TunnelTargetPolicy  │
│  TargetSource   │       │  (tunnel matching)  │
│  (discovery)    │       └─────────────────────┘
└────────┬────────┘
         │
         │ creates
         ▼
      Targets
```

## Separation of Concerns

Each resource type handles a single concern:

| Resource | Concern | Lifecycle |
|----------|---------|-----------|
| **Cluster** | Infrastructure | Where and how to run collectors |
| **Pipeline** | Wiring | What connects to what |
| **Target** | Device | Network device to collect from |
| **TargetSource** | Discovery | Dynamic target discovery |
| **TargetProfile** | Credentials | How to authenticate |
| **TunnelTargetPolicy** | Tunnel Matching | Rules for tunnel-connected devices |
| **Subscription** | Data | What paths to subscribe to |
| **Output** | Destination | Where to send data |
| **Input** | Source | External data sources |
| **Processor** | Transformation | How to transform data |

## Resource Selection

Resources can be selected in two ways:

### Direct References

Explicit list of resource names:

```yaml
spec:
  targetRefs:
    - router1
    - router2
    - switch1
```

### Label Selectors

Select resources by labels:

```yaml
spec:
  targetSelectors:
    - matchLabels:
        vendor: vendorA
        role: core
```

### Union Semantics

Multiple selectors are combined with OR logic:

```yaml
spec:
  targetSelectors:
    # Select vendorA devices OR vendorB devices
    - matchLabels:
        vendor: vendorA
    - matchLabels:
        vendor: vendorB
```

This selects targets matching **either** selector.

## Relationships

### Pipeline → Targets → Subscriptions

Each target in a pipeline gets all subscriptions from that pipeline:

```yaml
# Pipeline selects targets and subscriptions
Pipeline:
  targets: [T1, T2]
  subscriptions: [S1, S2]

# Result: Each target gets both subscriptions
T1.subscriptions = [S1, S2]
T2.subscriptions = [S1, S2]
```

### Subscription → Outputs

Each subscription sends data to all outputs in the same pipeline:

```yaml
# Pipeline connects subscriptions to outputs
Pipeline:
  subscriptions: [S1, S2]
  outputs: [O1, O2]

# Result: Each subscription sends to both outputs
S1.outputs = [O1, O2]
S2.outputs = [O1, O2]
```

### Input → Outputs

Inputs send received data to all outputs in the same pipeline:

```yaml
# Pipeline connects inputs to outputs
Pipeline:
  inputs: [I1]
  outputs: [O1, O2]

# Result: Input sends to both outputs
I1.outputs = [O1, O2]
```

### TunnelTargetPolicy → Subscriptions

For gRPC tunnel mode, TunnelTargetPolicies match devices that connect to the collector and apply subscriptions:

```yaml
# Pipeline connects tunnel policies to subscriptions
Pipeline:
  tunnelTargetPolicies: [P1, P2]
  subscriptions: [S1, S2]

# Result: Devices matching P1 or P2 get subscriptions S1 and S2
```

**Note**: TunnelTargetPolicies require the Cluster to have `grpcTunnel` configured.

## Cross-Pipeline Aggregation

Resources can participate in multiple pipelines. The operator aggregates relationships across all pipelines:

```yaml
# Pipeline A
targets: [T1]
subscriptions: [S1]
outputs: [O1]

# Pipeline B  
targets: [T1]  # Same target!
subscriptions: [S2]
outputs: [O2]

# Result: T1 gets subscriptions from both pipelines
T1.subscriptions = [S1, S2]

# S1 goes to O1, S2 goes to O2
S1.outputs = [O1]
S2.outputs = [O2]
```

This enables flexible architectures where:
- Different teams own different pipelines
- Same device can serve multiple use cases
- Configuration changes are isolated to specific pipelines

