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
spec:
  type: prometheus  # The output type
  config:           # Output specific config fields
    listen: ":9804"
    path: /metrics
```

## Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Output type (prometheus, kafka, influxdb, etc.) |
| `config` | object | Yes | Type-specific configuration (schemaless) |
| `service` | ServiceSpec | No | Kubernetes Service configuration (Prometheus only) |

- It is recommended to label outputs for flexible selection when building Pipelines.

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-output
  labels:
    type: prometheus
    env: production
    team: platform
spec:
  type: prometheus
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: core-telemetry
spec:
  # ...
  outputs:
    outputSelectors:
      - matchLabels:
          type: prometheus
          env: production
          team: platform
```


## Prometheus Output

### Scrape based

- The minmal Promehteus output configuration is:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-metrics
spec:
  type: prometheus
```
The above snippet will create a `prometheus` type output in the gNMIc pods with some defaults values `listen:: :9804` and `path: /metrics`.

- The output can further be customized by adding the relevant fields to the `config` section (see [gNMIc prometheus output config](https://gnmic.openconfig.net/user_guide/outputs/prometheus_output/)):

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

#### Kubernetes Service

To ease integration with [Prometheus Server](https://prometheus.io/) and [Prometheus Operator](https://prometheus-operator.dev), 
gNMIc operator creates a Kubernetes Server for each Prometheus output with handy labels and annotations to be discoverable 
using [Prometheus Kubernetes SD](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#kubernetes_sd_config)
or monitored using a Prometheus Operator [ServiceMonitor](https://prometheus-operator.dev/docs/api-reference/api/#monitoring.coreos.com/v1.ServiceMonitor).

```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    prometheus.io/path: /metrics    # <-- tells prometheus server which path to scrape. Populated from config.path
    prometheus.io/port: "9804"      # <-- tells proemtheus server which port to scrape. Populated from config.listen
    prometheus.io/scrape: "true"    # <-- can be toggled to enable/disable the scrape.
  labels:                           # <-- Group of labels that can be used in a ServiceMonitor 
    app.kubernetes.io/managed-by: gnmic-operator
    app.kubernetes.io/name: gnmic
    operator.gnmic.dev/cluster-name: cluster1            # <-- Populated from the cluster name
    operator.gnmic.dev/output-name: prom-output1         # <-- Populated from the output name
    operator.gnmic.dev/service-type: prometheus-output   # <-- Always set to `prometheus-output` for an output type `prometheus`
  name: gnmic-cluster1-prom-prom-output1
spec:
  type: ClusterIP
  selector:
    operator.gnmic.dev/cluster-name: cluster1
  ports:
  - name: metrics      # static port name
    port: 9804         # <-- Populated from config.listen
    protocol: TCP
    targetPort: 9804   # <-- Populated from config.listen
```

If there a need to further customize the Service, a `service` section can be configured to select the service type and add more `labels` and `annotations`

```yaml
spec:
  type: prometheus
  config:
    listen: ":9804"
  service:
    type: ClusterIP  # ClusterIP, NodePort, or LoadBalancer
    annotations:
      metallb.io/address-pool: internal
    labels:
      service: my-prom-output
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

