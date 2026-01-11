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

<a>
  <img src="/images/resources_model.svg" alt="Resource Model CRD Diagram" style="display:block; margin:auto;">
</a>

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

Resources are tied together in two main ways:

- Direct Reference: A `Target` directly references a `TargetProfile`, a `Pipeline` can directly reference a subscription.
- Label selection: A `Pipeline` can select Targets, Subscriptions, Outputs and Inputs using labels.

The `Pipeline` resource allows combining both approaches.

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

Multiple selectors for the same resource are combined with OR logic:

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

Pipeline selects targets and subscriptions

```yaml
spec:
  targetRefs:
    - T1
    - T2
  subscriptionRefs:
    - S1
    - S2
```

Results in each target getting both subscriptions (gNMIc config)

```yaml
targets:
  T1:
    subscriptions: [S1, S2]
  T2:
    subscriptions: [S1, S2]
```

### Subscription → Outputs

Each subscription sends data to all outputs in the same pipeline:

```yaml
spec:
  subscriptionRefs:
    - S1
    - S2
  outputs:
    outputRefs:
      - O1
      - O2
```

Results in each subscription data being sent to both outputs

```yaml
subscriptions:
  S1:
    outputs: [O1, O2]
  S2:
    outputs: [O1, O2]
```

### Input → Outputs

Inputs send received data to all outputs in the same pipeline:

```yaml
spec:
  inputs:
    inputRefs:
      - I1
  outputs:
    outputRefs:
      - O1
      - O2
```

Result in Input I1 sending reecived data to both outputs

```yaml
inputs:
  I1:
    outputs: [O1, O2]
```

### TunnelTargetPolicy → Subscriptions

For gRPC tunnel mode, TunnelTargetPolicies match devices that connect to the collector and apply subscriptions:

```yaml
spec:
  tunnelTargetPolicies:
    - P1
    - P2
  subscriptions:
    - S1
    - S2
```

Result: Devices matching P1 or P2 get subscriptions S1 and S2

**Note**: TunnelTargetPolicies require the referenced Cluster to have `grpcTunnel` configured.

## Overlappining pipelines

Resources can participate in multiple pipelines. 
The operator aggregates relationships across all pipelines and reconciles them into a single effective runtime configuration, 
ensuring that shared targets or subscriptions are instantiated only once while preserving pipeline-specific processing and outputs.

### Considerations

Note that if an Output is referenced by two different pipelines in the same cluster, the operator creates two distinct instances of that output in the gNMIc collector pods.
For Prometheus outputs, this means the operator must manage scrape endpoint port allocation to avoid conflicts.

If the same **target**, **subscription**, and **output** are referenced by two different pipelines in the same cluster, the collector subscribes only once, but data is duplicated at the output stage.
This configuration only makes sense when the two pipelines use different output processor chains.

### Examples

Here are some examples for illustration:

1. Same target, different subscriptions and outputs

```yaml
# Pipeline A
spec:
  targetRefs: [T1]
  subscriptionRefs: [S_interface]
  outputs: 
    outputRefs: [O_prom] 
---
# Pipeline B
spec:
  targetRefs: [T1] # Same target
  subscriptionRefs: [S_bgp]
  outputs: 
    outputRefs: [O_kafka]
```

```yaml
targets:
  T1:
    subscriptions: [S_interface, S_bgp]
subscriptions:
  S_interface:
    outputs: [O_prom]
  S_bgp:
    outputs: [O_kafka]
outputs:
  O_PipelineA_prom: {}
  O_PipelineB_kafka: {}
```

2. Same target, different subscriptions but same output

   Although this can be achieved using a single pipeline, it allows using a differnet set of processors for each pipeline.

```yaml
# Pipeline A
spec:
  targetRefs: [T1]
  subscriptionRefs: [S_interface]
  outputs: 
    outputRefs: [O_kafka]
    processorRef: [Proc1_rename_metric]
---
# Pipeline B
spec:
  targetRefs: [T1]  # Same target
  subscriptionsRef: [S_bgp]
  outputs: 
    outputRefs: [O_kafka] # Same output no processors
```

```yaml
targets:
  T1:
    subscriptions: [S_interface, S_bgp]
subscriptions:
  S_interface:
    outputs: [O_kafka]
  S_bgp:
    outputs: [O_kafka]
outputs:
  O_PipelineA_kafka:
    - event-processors: [Proc1_rename_metrics]
  O_PipelineB_kafka: {}
```

3. Two target sets, some common subs, per-set subs and different outputs for each combination.

This is probably the most common case where there are two groups of targets (leaf/spine, vendorA/vendorB, prod/lab) each with different requirements when it comes to the data to be collected as well as where it has to be exported.

```yaml
# common pipeline
targets: ["T_Leaf1", "T_Leaf2", T_Leaf3, T_Spine1, T_Spine2]
subscriptions: ["S_common_cpu", "S_common_interfaces"]
outputs: [O_Prometheus]
```

```yaml
# leaf pipeline
targets: [T_Leaf1, T_Leaf2, T_Leaf3]
subscriptions: [S_bgp_evpn, S_mac, S_acl]
outputs: [O_Kafka1]
```

```yaml
# spine pipeline
targets: [T_Spine2, T_Spine2]
subscriptions: [S_bgp, S_setB_mac]
outputs: [O_Kafka2]
```

<a>
  <img src="/images/pipeline_3_illustration.svg" alt="Pipeline_illustration" style="width:80%; display:block; margin:auto;">
</a>

Or with Label selectors: (assuming target, subscription and output resources have the right labels.)

```yaml
# common pipeline
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: dc-common
spec:
  clusterRef: dc1-cluster
  enabled: true
  targetSelectors:
    - matchLabels:
        role: spine
        location: dc1
    - matchLabels:
        role: leaf
        location: dc1
  subscriptionSelectors:
    - matchLabels:
        type: interfaces
    - matchLabels:
        type: platform # covers both cpu and memory
  outputs:
    outputSelectors:
      - matchLabels:
          output-type: prometheus
---
# leaf pipeline
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: dc-leaf
spec:
  clusterRef: dc1-cluster
  enabled: true
  targetSelectors:
    - matchLabels:
        role: leaf
        location: dc1
  subscriptionSelectors:
    - matchLabels:
        type: bgp_evpn
    - matchLabels:
        type: mac
    - matchLabels:
        type: acl
  outputs:
    outputSelectors:
      - matchLabels:
          output-type: kafka
          topic: leaf-data # assumes the output reflects the configured topic in its label set
          location: dc1
---
# spine pipeline
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: dc-spine
spec:
  clusterRef: dc1-cluster
  enabled: true
  targetSelectors:
    - matchLabels:
        role: spine
        location: dc1
  subscriptionSelectors:
    - matchLabels:
        type: bgp
  outputs:
    outputSelectors:
      - matchLabels:
          output-type: kafka
          topic: spine-data
          location: dc1
```
