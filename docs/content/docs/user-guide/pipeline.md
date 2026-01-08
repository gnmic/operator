---
title: "Pipeline"
linkTitle: "Pipeline"
weight: 2
description: >
  Configuring telemetry pipelines
---

The `Pipeline` resource connects targets, tunnelTargetPolicies, subscriptions, outputs, and inputs together. It defines the flow of telemetry data through the system.

## Basic Configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: core-telemetry
spec:
  clusterRef: telemetry-cluster
  enabled: true
  targetRefs:
    - router1
    - router2
  subscriptionRefs:
    - interface-counters
  outputs:
    outputRefs:
      - prometheus-output
```

## Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `clusterRef` | string | Yes | Name of the Cluster to run in |
| `enabled` | bool | Yes | Whether the pipeline is active |
| `targetRefs` | []string | No | Direct target references |
| `targetSelectors` | []LabelSelector | No | Label selectors for targets |
| `tunnelTargetPolicyRefs` | []string | No | Direct tunnel target policy references |
| `tunnelTargetPolicySelectors` | []LabelSelector | No | Label selectors for tunnel target policies |
| `subscriptionRefs` | []string | No | Direct subscription references |
| `subscriptionSelectors` | []LabelSelector | No | Label selectors for subscriptions |
| `outputs.outputRefs` | []string | No | Direct output references |
| `outputs.outputSelectors` | []LabelSelector | No | Label selectors for outputs |
| `outputs.processorRefs` | []string | No | Direct processor references for outputs (order preserved) |
| `outputs.processorSelectors` | []LabelSelector | No | Label selectors for output processors (sorted by name) |
| `inputs.inputRefs` | []string | No | Direct input references |
| `inputs.inputSelectors` | []LabelSelector | No | Label selectors for inputs |
| `inputs.processorRefs` | []string | No | Direct processor references for inputs (order preserved) |
| `inputs.processorSelectors` | []LabelSelector | No | Label selectors for input processors (sorted by name) |

## Resource Selection

### Direct References

Select specific resources by name:

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
        role: core
    - matchLabels:
        role: edge
```

### Mixed Selection

Combine refs and selectors (union):

```yaml
spec:
  # These specific targets
  targetRefs:
    - special-router
  # Plus all targets with this label
  targetSelectors:
    - matchLabels:
        env: production
```

## Enabling/Disabling

Pipelines can be disabled without deletion:

```yaml
spec:
  enabled: false  # Pipeline is inactive
```

This removes the pipeline's contribution to the cluster configuration without deleting the Pipeline resource.

## Example: Multi-Output Pipeline

Send data to multiple destinations:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: multi-output
spec:
  clusterRef: telemetry-cluster
  enabled: true
  targetSelectors:
    - matchLabels:
        tier: critical
  subscriptionRefs:
    - full-telemetry
  outputs:
    outputRefs:
      - prometheus-realtime
      - kafka-archive
      - influxdb-analytics
```

## Example: Input Pipeline

Process data from an external source:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: kafka-processor
spec:
  clusterRef: telemetry-cluster
  enabled: true
  # No targets - data comes from input
  inputs:
    inputRefs:
      - kafka-telemetry
  outputs:
    outputRefs:
      - prometheus-output
```

## Overlapping Pipelines

Multiple pipelines can share resources. The operator aggregates configuration:

```yaml
# Pipeline A: Interface metrics to Prometheus
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: interfaces-to-prometheus
spec:
  clusterRef: my-cluster
  enabled: true
  targetSelectors:
    - matchLabels:
        role: core
  subscriptionRefs:
    - interface-counters
  outputs:
    outputRefs:
      - prometheus
---
# Pipeline B: Same targets, BGP metrics to Kafka
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: bgp-to-kafka
spec:
  clusterRef: my-cluster
  enabled: true
  targetSelectors:
    - matchLabels:
        role: core  # Same targets!
  subscriptionRefs:
    - bgp-state
  outputs:
    outputRefs:
      - kafka
```

Result: Core routers get both subscriptions, each going to different outputs.

## Tunnel Target Policies

For gRPC tunnel mode (where devices connect to the collector), use tunnel target policies instead of static targets:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: tunnel-telemetry
spec:
  clusterRef: tunnel-cluster  # Must have grpcTunnel configured
  enabled: true
  # Tunnel target policies instead of targets
  tunnelTargetPolicyRefs:
    - core-routers
  tunnelTargetPolicySelectors:
    - matchLabels:
        tier: edge
  subscriptionRefs:
    - interface-counters
  outputs:
    outputRefs:
      - prometheus-output
```

**Note**: The referenced cluster must have `grpcTunnel` configured. If not, the pipeline status will show an error.

See [TunnelTargetPolicy documentation]({{< ref "tunneltargetpolicy" >}}) for details on configuring tunnel target matching.

## Adding Processors

Processors transform data before it reaches outputs or after it comes from inputs:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: processed-telemetry
spec:
  clusterRef: telemetry-cluster
  enabled: true
  targetSelectors:
    - matchLabels:
        env: production
  subscriptionRefs:
    - interface-counters
  outputs:
    outputRefs:
      - prometheus-output
    # Processors applied to output data
    processorRefs:
      - filter-empty-events     # Applied first
      - add-cluster-metadata    # Applied second
    processorSelectors:
      - matchLabels:
          stage: enrichment     # Added after refs, sorted by name
```

### Processor Ordering

**Order matters for processors!** The final order is:

1. `processorRefs` - exact order specified (duplicates allowed)
2. `processorSelectors` - sorted by name, deduplicated

### Separate Input/Output Processors

Inputs and outputs have independent processor chains:

```yaml
spec:
  outputs:
    outputRefs: [prometheus]
    processorRefs:
      - format-for-prometheus
  inputs:
    inputRefs: [kafka-input]
    processorRefs:
      - validate-kafka-format
```

See the [Processor documentation]({{< ref "processor" >}}) for details on processor types and configuration.

## Status

The Pipeline status shows the current state and resolved resource counts:

```yaml
status:
  status: Active
  targetsCount: 10
  tunnelTargetPoliciesCount: 3
  subscriptionsCount: 3
  inputsCount: 0
  outputsCount: 2
  conditions:
    - type: Ready
      status: "True"
      reason: PipelineReady
      message: "Pipeline has 10 targets, 3 subscriptions, 0 inputs, 2 outputs"
    - type: ResourcesResolved
      status: "True"
      reason: ResourcesResolved
      message: "All referenced resources were successfully resolved"
```

### Status Fields

| Field | Description |
|-------|-------------|
| `status` | Overall status (Active, Incomplete, Error) |
| `targetsCount` | Number of resolved static targets |
| `tunnelTargetPoliciesCount` | Number of resolved tunnel target policies |
| `subscriptionsCount` | Number of resolved subscriptions |
| `inputsCount` | Number of resolved inputs |
| `outputsCount` | Number of resolved outputs |
| `conditions` | Standard Kubernetes conditions |

### Conditions

| Type | Description |
|------|-------------|
| `Ready` | True when pipeline has required resources |
| `ResourcesResolved` | True when all referenced resources were found |

### Pipeline Readiness

A pipeline is considered ready when it has:
- **(Targets AND Subscriptions) OR Inputs** - data sources
- **AND Outputs** - data destinations

Examples:
- ✅ Ready: 10 targets, 2 subscriptions, 1 output
- ✅ Ready: 0 targets, 0 subscriptions, 1 input, 1 output
- ❌ Incomplete: 10 targets, 0 subscriptions, 0 outputs (missing subscriptions)
- ❌ Incomplete: 0 targets, 0 inputs, 1 output (missing data source)

