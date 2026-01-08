---
title: "Input"
linkTitle: "Input"
weight: 7
description: >
  Configuring external data inputs
---

The `Input` resource defines external data sources that feed telemetry data into the gNMIc cluster. This enables processing data from sources like Kafka, NATS, or other gNMIc instances.

## Basic Configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Input
metadata:
  name: kafka-input
spec:
  type: kafka
  config:
    brokers:
      - kafka:9092
    topics:
      - telemetry-raw
    group: gnmic-processors
```

## Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Input type (kafka, nats, etc.) |
| `config` | object | Yes | Type-specific configuration (schemaless) |

## Kafka Input

Consume telemetry from Kafka topics:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Input
metadata:
  name: kafka-telemetry
spec:
  type: kafka
  config:
    brokers:
      - kafka-0:9092
      - kafka-1:9092
      - kafka-2:9092
    topics:
      - network-telemetry
    group: gnmic-consumer-group
    format: event
    # Optional: SASL authentication
    # sasl:
    #   mechanism: PLAIN
    #   user: kafka-user
    #   password: kafka-password
```

## NATS Input

Consume telemetry from NATS:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Input
metadata:
  name: nats-telemetry
spec:
  type: nats
  config:
    address: nats://nats:4222
    subject: telemetry.>
    queue: gnmic-queue
    format: event
```

## Use Cases

### Centralized Processing

Collect from remote gNMIc instances and process centrally:

```
┌─────────────────┐      ┌─────────────────┐
│  Remote Site A  │      │  Remote Site B  │
│  gNMIc → Kafka  │      │  gNMIc → Kafka  │
└────────┬────────┘      └────────┬────────┘
         │                        │
         └──────────┬─────────────┘
                    │
                    ▼
              ┌───────────┐
              │   Kafka   │
              └─────┬─────┘
                    │
                    ▼
         ┌─────────────────────┐
         │   Central gNMIc     │
         │   (this cluster)    │
         │                     │
         │  Input: Kafka       │
         │  Output: Prometheus │
         └─────────────────────┘
```

### Data Enrichment Pipeline

Process and enrich telemetry before storage:

```yaml
# Input from raw telemetry topic
apiVersion: operator.gnmic.dev/v1alpha1
kind: Input
metadata:
  name: raw-telemetry
spec:
  type: kafka
  config:
    brokers: [kafka:9092]
    topics: [telemetry-raw]
---
# Output to processed topic
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: processed-telemetry
spec:
  type: kafka
  config:
    address: kafka:9092
    topic: telemetry-processed
---
# Pipeline connecting input to output
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: enrichment-pipeline
spec:
  clusterRef: processor-cluster
  enabled: true
  inputs:
    inputRefs:
      - raw-telemetry
  outputs:
    outputRefs:
      - processed-telemetry
```

### Fan-Out Architecture

Distribute incoming telemetry to multiple destinations:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Input
metadata:
  name: kafka-input
spec:
  type: kafka
  config:
    brokers: [kafka:9092]
    topics: [telemetry]
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: fan-out
spec:
  clusterRef: my-cluster
  enabled: true
  inputs:
    inputRefs:
      - kafka-input
  outputs:
    outputRefs:
      - prometheus-realtime
      - s3-archive
      - elasticsearch-search
```

## Input-Output Relationship

When an Input is included in a Pipeline, it automatically gets the `outputs` field populated with all Outputs from the same Pipeline:

```yaml
# Pipeline definition
inputs: [input1]
outputs: [output1, output2]

# Resulting gNMIc config
inputs:
  input1:
    type: kafka
    outputs: [output1, output2]  # Automatically added
```

This means data received by the input is forwarded to all outputs in the same pipeline.

## Input Processors

Inputs can have their own processor chain, separate from outputs:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: kafka-processing
spec:
  clusterRef: my-cluster
  enabled: true
  inputs:
    inputRefs:
      - kafka-telemetry
    # Processors applied to incoming data
    processorRefs:
      - validate-format
      - normalize-values
    processorSelectors:
      - matchLabels:
          stage: input-processing
  outputs:
    outputRefs:
      - prometheus-output
    # Different processors for output
    processorRefs:
      - add-export-tags
```

This allows pre-processing data from external sources before it flows to outputs.

See the [Processor documentation]({{< ref "processor" >}}) for details on processor types and configuration.

