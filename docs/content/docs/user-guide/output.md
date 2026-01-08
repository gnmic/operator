---
title: "Output"
linkTitle: "Output"
weight: 6
description: >
  Configuring telemetry outputs
---

The `Output` resource defines where telemetry data is sent. gNMIc supports many output types including Prometheus, Kafka, InfluxDB, and more.

## Basic Configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-output
  labels:
    type: prometheus
spec:
  type: prometheus
  config:
    listen: ":9804"
    path: /metrics
```

## Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Output type (prometheus, kafka, influxdb, etc.) |
| `config` | object | Yes | Type-specific configuration (schemaless) |
| `service` | ServiceSpec | No | Kubernetes Service configuration (Prometheus only) |

## Prometheus Output

Exposes metrics via HTTP endpoint:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-metrics
spec:
  type: prometheus
  config:
    listen: ":9804"
    path: /metrics
    metric-prefix: gnmic
    export-timestamps: true
    strings-as-labels: true
```

### Service Configuration

Configure the Kubernetes Service for Prometheus output:

```yaml
spec:
  type: prometheus
  config:
    listen: ":9804"
  service:
    type: LoadBalancer  # ClusterIP, NodePort, or LoadBalancer
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
```

The operator automatically creates a Service for each Prometheus output.

## Kafka Output

Send telemetry to Apache Kafka:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: kafka-telemetry
spec:
  type: kafka
  config:
    address: kafka-bootstrap:9092
    topic: telemetry
    encoding: proto
    max-retry: 3
    timeout: 5s
    # Optional: SASL authentication
    # sasl:
    #   user: kafka-user
    #   password: kafka-password
```

## InfluxDB Output

Send telemetry to InfluxDB:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: influxdb-telemetry
spec:
  type: influxdb
  config:
    url: http://influxdb:8086
    org: myorg
    bucket: telemetry
    token: my-influxdb-token
    batch-size: 1000
    flush-timer: 10s
```

## NATS Output

Send telemetry to NATS:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: nats-telemetry
spec:
  type: nats
  config:
    address: nats://nats:4222
    subject: telemetry
    subject-prefix: gnmic
```

## File Output

Write telemetry to files (useful for debugging):

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: file-debug
spec:
  type: file
  config:
    file-type: stdout  # or file path
    format: json
```

## Multiple Outputs

Create multiple outputs for different purposes:

```yaml
# Real-time metrics
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-realtime
  labels:
    purpose: monitoring
spec:
  type: prometheus
  config:
    listen: ":9804"
---
# Long-term storage
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: kafka-archive
  labels:
    purpose: archival
spec:
  type: kafka
  config:
    address: kafka:9092
    topic: telemetry-archive
---
# Analytics pipeline
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: kafka-analytics
  labels:
    purpose: analytics
spec:
  type: kafka
  config:
    address: kafka:9092
    topic: telemetry-analytics
```

Then select outputs in pipelines:

```yaml
# Monitoring pipeline - Prometheus only
outputs:
  outputSelectors:
    - matchLabels:
        purpose: monitoring
---
# Full pipeline - all outputs
outputs:
  outputSelectors:
    - matchLabels:
        purpose: monitoring
    - matchLabels:
        purpose: archival
    - matchLabels:
        purpose: analytics
```

## Using Labels

Label outputs for flexible selection:

```yaml
metadata:
  labels:
    type: prometheus
    env: production
    team: platform
```

