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
  config: {}        # Output specific config fields
```

## Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Output type (prometheus, kafka, influxdb, etc.) |
| `config` | object | No | Type-specific configuration (schemaless) |
| `service` | OutputServiceSpec | No | Kubernetes Service configuration. This is the service exposing the output endpoint (Prometheus only). |
| `serviceRef` | ServiceReference | No | Reference to a Kubernetes Service for address resolution |
| `serviceSelector` | ServiceSelector | No | Label selector to discover Kubernetes Services |

### Service

Defines the Service type, labels and annotations that will be created when the output has `type=prometheus`.

This service allows the user to configure Prometheus scrape endpoint auto discovery.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | No | Kubernetes Service type |
| `annotations` | map[string]string | No | Service annotations |
| `labels` | map[string]string | No | Service labels |

### ServiceReference

Defines the output address or URL as a service reference.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Name of the Kubernetes Service |
| `namespace` | string | No | Namespace of the Service (defaults to Output's namespace) |
| `port` | string | No | Port name or number (defaults to first port) |

### ServiceSelector

Defines the output address or URL as a service selector.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `matchLabels` | map[string]string | Yes | Labels to match services |
| `namespace` | string | No | Namespace to search (defaults to Output's namespace) |
| `port` | string | No | Port name or number (defaults to first port) |

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

- The minmal Promehteus output configuration is:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-metrics
spec:
  type: prometheus
```
The above snippet will create a `prometheus` type output in the gNMIc pods with defaults values `listen:: :9804` and `path: /metrics` (the listen field port number is choosen from a predefined range of ports).

- The output can further be customized by adding the relevant fields to the `config` section (see [gNMIc prometheus output config](https://gnmic.openconfig.net/user_guide/outputs/prometheus_output/)):

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-metrics
spec:
  type: prometheus
  config:
    metric-prefix: gnmic
    export-timestamps: true
    strings-as-labels: true
```

### Kubernetes Service

To ease integration with [Prometheus Server](https://prometheus.io/) and [Prometheus Operator](https://prometheus-operator.dev), 
gNMIc operator creates a Kubernetes Server for each Prometheus output with handy labels and annotations to be discoverable 
using [Prometheus Kubernetes SD](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#kubernetes_sd_config)
or monitored using a Prometheus Operator [ServiceMonitor](https://prometheus-operator.dev/docs/api-reference/api/#monitoring.coreos.com/v1.ServiceMonitor).

```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    prometheus.io/path: /metrics    # tells prometheus server which path to scrape. Populated from config.path
    prometheus.io/port: "9804"      # tells proemtheus server which port to scrape. Populated from config.listen
    prometheus.io/scrape: "true"    # can be toggled to enable/disable the scrape.
  labels:                           # group of labels that can be used in a ServiceMonitor 
    app.kubernetes.io/managed-by: gnmic-operator
    app.kubernetes.io/name: gnmic
    operator.gnmic.dev/cluster-name: cluster1            # populated from the cluster name
    operator.gnmic.dev/output-name: prom-output1         # populated from the output name
    operator.gnmic.dev/service-type: prometheus-output   # always set to `prometheus-output` for an output type `prometheus`
  name: gnmic-cluster1-prom-prom-output1
spec:
  type: ClusterIP
  selector:
    operator.gnmic.dev/cluster-name: cluster1
  ports:
  - name: metrics      # static port name
    port: 9804         # populated from config.listen
    protocol: TCP
    targetPort: 9804   # populated from config.listen
```

If there is a need to further customize the Service, the `service` section can be configured to change the service type and set additional `labels` and `annotations`

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

The operator creates a Service for each Prometheus output.

## Prometheus Remote Write Output

Push telemetry metrics to a Prometheus-compatible remote write endpoint (Prometheus, Thanos, Cortex, Mimir, VictoriaMetrics).

### Static URL

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-remote-write
spec:
  type: prometheus_write
  config:
    url: http://prometheus:9090/api/v1/write
    timeout: 10s
```

### Using Service Reference

Reference a Prometheus or compatible remote write endpoint Service:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-remote-write
spec:
  type: prometheus_write
  serviceRef:
    name: prometheus-server
    port: http  # or "9090"
  config:
    timeout: 10s
```

The operator resolves the service and constructs the URL as `http://prometheus-server.{namespace}.svc.cluster.local:9090`.

### Using Service Selector

Discover multiple Kafka brokers using labels:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-remote-write
spec:
  type: prometheus_write
  serviceSelector:
    matchLabels:
      app: prometheus-server
    port: http
  config:
    timeout: 10s
```

### With TLS

When TLS is configured, the operator uses `https://` scheme:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-remote-write-tls
spec:
  type: prometheus_write
  serviceRef:
    name: prometheus-server
    port: https
  config:
    timeout: 10s
    tls:
      skip-verify: true
      # or provide certificates:
      # ca-file: /path/to/ca.crt
      # cert-file: /path/to/client.crt
      # key-file: /path/to/client.key
```

## Kafka Output

Send telemetry to Apache Kafka.

### Static Address

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

### Using Service Reference

Instead of hardcoding the Kafka address, reference a Kubernetes Service:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: kafka-telemetry
spec:
  type: kafka
  serviceRef:
    name: kafka-bootstrap
    port: "9092"
  config:
    topic: telemetry
    encoding: proto
```

### Using Service Selector

Discover multiple Kafka brokers using labels:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: kafka-telemetry
spec:
  type: kafka
  serviceSelector:
    matchLabels:
      app: kafka
      component: broker
    port: client
  config:
    topic: telemetry
```

### With TLS

When TLS is configured, the operator uses `https://` scheme:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: kafka-telemetry
spec:
  type: kafka
  serviceSelector:
    matchLabels:
      app: kafka
      component: broker
    port: client
  config:
    topic: telemetry
    tls:
      skip-verify: true
      # or provide certificates:
      # ca-file: /path/to/ca.crt
      # cert-file: /path/to/client.crt
      # key-file: /path/to/client.key
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

Send telemetry to NATS.

### Static Address

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

### Using Service Reference

Reference a NATS Kubernetes Service directly:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: nats-telemetry
spec:
  type: nats
  serviceRef:
    name: nats-cluster
    port: client  # or "4222"
  config:
    subject: telemetry
```

The operator resolves the service to `nats://nats-cluster.{namespace}.svc.cluster.local:4222`.

### Using Service Selector

Discover NATS servers using labels:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: nats-telemetry
spec:
  type: nats
  serviceSelector:
    matchLabels:
      app: nats
    port: client
  config:
    subject: telemetry
```


### With TLS

When TLS is configured, the operator uses `https://` scheme:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: nats-telemetry
spec:
  type: nats
  serviceSelector:
    matchLabels:
      app: nats
    port: client
  config:
    subject: telemetry
    tls:
      skip-verify: true
      # or provide certificates:
      # ca-file: /path/to/ca.crt
      # cert-file: /path/to/client.crt
      # key-file: /path/to/client.key
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

## Service Discovery

For outputs that connect to external systems (NATS, Kafka, InfluxDB, Prometheus Remote Write), you can use Kubernetes Service discovery instead of hardcoding addresses.

### Supported Output Types

| Output Type | Address Field | Scheme |
|-------------|---------------|--------|
| `nats` | `address` | `nats://` |
| `jetstream` | `address` | `nats://` |
| `kafka` | `address` | (none) |
| `prometheus_write` | `url` | `http://` or `https://` |
| `influxdb` | `url` | `http://` or `https://` |

### serviceRef vs serviceSelector

| Feature | serviceRef | serviceSelector |
|---------|------------|-----------------|
| **Use case** | Known, single service | Dynamic discovery |
| **Result** | Single address | Multiple addresses (comma-separated) |
| **Cross-namespace** | Yes (specify namespace) | Yes (specify namespace) |

1. The operator watches for Output resources
2. During reconciliation, it resolves the referenced Service(s)
3. Addresses are formatted with the appropriate scheme (`nats://`)
4. The resolved address is injected into the output config

### Example: Cross-Namespace Service Reference

Reference a NATS cluster in a different namespace:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: nats-output
  namespace: telemetry
spec:
  type: nats
  serviceRef:
    name: nats-cluster
    namespace: messaging  # different namespace
    port: client
  config:
    subject: telemetry.events
```

## Multiple Outputs

Create multiple outputs for different purposes:

```yaml
# metrics for vizualization dashboards
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-realtime
  labels:
    purpose: monitoring
spec:
  type: prometheus
---
# long-term storage
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
# analytics pipeline
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
# monitoring pipeline: Prometheus only
outputs:
  outputSelectors:
    - matchLabels:
        purpose: monitoring
---
# full pipeline: all outputs
outputs:
  outputSelectors:
    - matchLabels:
        purpose: monitoring
    - matchLabels:
        purpose: archival
    - matchLabels:
        purpose: analytics
```

