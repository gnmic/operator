---
title: "Processor"
linkTitle: "Processor"
weight: 8
description: >
  Configuring event processors for data transformation
---

The `Processor` resource defines event transformations applied to telemetry data. Processors can filter, enrich, transform, or drop events as they flow through the gNMIc cluster.

## Basic Configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Processor
metadata:
  name: add-cluster-tag
spec:
  type: event-add-tag
  config:
    add:
      cluster: production
      region: us-east-1
```

## Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Processor type (event-add-tag, event-drop, event-strings, etc.) |
| `config` | object | Yes | Type-specific configuration (schemaless) |

## Processor Types

### Event Add Tag

Add static tags to events:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Processor
metadata:
  name: add-metadata
spec:
  type: event-add-tag
  config:
    add:
      environment: production
      datacenter: dc1
    overwrite: true
```

### Event Drop

Drop events matching a condition:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Processor
metadata:
  name: filter-empty
spec:
  type: event-drop
  config:
    condition: 'len(.values) == 0'
```

### Event Strings

Transform string values:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Processor
metadata:
  name: normalize-strings
spec:
  type: event-strings
  config:
    value-names:
      - ".*"
    transforms:
      - path-base
      - trim-prefix: /interfaces/interface/
```

### Event Convert

Convert value types:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Processor
metadata:
  name: convert-counters
spec:
  type: event-convert
  config:
    value-names:
      - ".*-counter$"
    type: int
```

### Event Extract Tags

Extract tags from values:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Processor
metadata:
  name: extract-interface
spec:
  type: event-extract-tags
  config:
    value-names:
      - interface_name
```

## Using Processors in Pipelines

Processors are associated with outputs or inputs in a Pipeline:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: telemetry-pipeline
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
      - prometheus-output
    # Processors for outputs
    processorRefs:
      - add-metadata
      - normalize-strings
    processorSelectors:
      - matchLabels:
          stage: enrichment
  inputs:
    inputRefs:
      - kafka-input
    # Processors for inputs (separate chain)
    processorRefs:
      - validate-format
```

## Processor Ordering

**Order matters!** Processors are applied in sequence. The order is:

1. **processorRefs** - in the exact order specified (duplicates allowed)
2. **processorSelectors** - sorted by name, deduplicated

### Example

```yaml
processorRefs:
  - step-3-transform    # Applied first
  - step-1-filter       # Applied second (order from refs, not name)
processorSelectors:
  - matchLabels:
      auto: "true"      # Matches: auto-enrich, auto-validate
                        # Added sorted: auto-enrich, auto-validate
```

Final order: `[step-3-transform, step-1-filter, auto-enrich, auto-validate]`

### Intentional Duplicates

You can list the same processor multiple times:

```yaml
processorRefs:
  - normalize           # Pre-processing
  - transform-values
  - normalize           # Post-processing cleanup
```

## Separate Output and Input Processors

Outputs and inputs have independent processor chains:

```yaml
spec:
  outputs:
    outputRefs: [prometheus]
    processorRefs:
      - format-for-prometheus   # Only for outputs
  inputs:
    inputRefs: [kafka]
    processorRefs:
      - validate-kafka-format   # Only for inputs
```

## Use Cases

### Filtering High-Cardinality Data

Drop events that would create too many time series:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Processor
metadata:
  name: drop-debug-counters
spec:
  type: event-drop
  config:
    condition: 'hasPrefix(.name, "debug_")'
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Processor
metadata:
  name: drop-internal
spec:
  type: event-drop
  config:
    tag-names:
      - "internal_.*"
```

### Multi-Tenant Tagging

Add tenant information based on device groups:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Processor
metadata:
  name: tag-tenant-a
  labels:
    tenant: a
spec:
  type: event-add-tag
  config:
    add:
      tenant: tenant-a
      cost_center: cc-100
```

Then use in tenant-specific pipelines:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: tenant-a-pipeline
spec:
  clusterRef: shared-cluster
  targetSelectors:
    - matchLabels:
        tenant: a
  outputs:
    outputRefs: [shared-prometheus]
    processorSelectors:
      - matchLabels:
          tenant: a   # Uses tag-tenant-a processor
```

### Data Normalization

Standardize data format across vendors:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Processor
metadata:
  name: normalize-interface-names
spec:
  type: event-strings
  config:
    tag-names:
      - interface_name
    transforms:
      - replace:
          apply-on: value
          old: "Ethernet"
          new: "eth"
      - replace:
          apply-on: value
          old: "GigabitEthernet"
          new: "ge"
```

## Processor Configuration Reference

For the complete list of processor types and their configuration options, refer to the [gNMIc Event Processors documentation](https://gnmic.openconfig.net/user_guide/event_processors/intro/).

