---
title: "Scraping"
linkTitle: "Scraping"
weight: 1
description: >
  Integrate gNMIc Operator with Prometheus Operator using ServiceMonitor
---

This guide shows how to integrate gNMIc Operator with [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) for automatic service discovery and metrics scraping.

## Overview

When you create a Prometheus-type `Output` in gNMIc Operator, it automatically creates a Kubernetes Service that exposes the metrics endpoint. You can then use Prometheus Operator's `ServiceMonitor` to automatically discover and scrape these metrics.

### How It Works

```text
┌─────────────────┐      ┌──────────────────────┐      ┌─────────────────┐
│  gNMIc Cluster  │──────│  Prometheus Output   │──────│    Service      │
│   (StatefulSet) │      │   (metrics endpoint) │      │  (auto-created) │
└─────────────────┘      └──────────────────────┘      └────────┬────────┘
                                                                │
                                                                │ discovers
                                                                ▼
┌─────────────────┐      ┌──────────────────────┐      ┌─────────────────┐
│   Prometheus    │◀─────│   ServiceMonitor     │──────│  Label Selector │
│    (scrapes)    │      │   (Prom Operator)    │      │                 │
└─────────────────┘      └──────────────────────┘      └─────────────────┘
```

## Prerequisites

- Kubernetes cluster with gNMIc Operator installed
- [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) installed (e.g., via [kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack))

## Step 1: Create a Prometheus Output

Create an `Output` resource of type `prometheus`. The operator automatically creates a Service for this output.

{{< tabpane >}}
{{< tab header="YAML" lang="yaml" >}}
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-metrics
  labels:
    app: gnmic
    output-type: prometheus
spec:
  type: prometheus
  config:
    listen: ":9804"
    path: /metrics
    metric-prefix: gnmic
    export-timestamps: true
    strings-as-labels: true
{{< /tab >}}
{{< tab header="kubectl" lang="bash" >}}
cat << 'EOF' | kubectl apply -f -
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-metrics
  labels:
    app: gnmic
    output-type: prometheus
spec:
  type: prometheus
  config:
    listen: ":9804"
    path: /metrics
    metric-prefix: gnmic
    export-timestamps: true
    strings-as-labels: true
EOF
{{< /tab >}}
{{< /tabpane >}}

### Output Service

When the Pipeline referencing this output is processed, the operator creates a Service with the naming pattern:

```text
gnmic-{cluster-name}-prom-{output-name}
```

For example, if your cluster is named `telemetry-cluster` and the output is `prometheus-metrics`, the Service will be:

```text
gnmic-telemetry-cluster-prom-prometheus-metrics
```

You can verify the created Service:

```bash
kubectl get svc -l operator.gnmic.dev/cluster-name=telemetry-cluster
```

## Step 2: Create a ServiceMonitor

The `ServiceMonitor` is a Prometheus Operator CRD that tells Prometheus which Services to scrape.

{{< tabpane >}}
{{< tab header="YAML" lang="yaml" >}}
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: gnmic-telemetry
  labels:
    # This label must match your Prometheus serviceMonitorSelector
    release: prometheus
spec:
  # Select Services by label
  selector:
    matchLabels:
      operator.gnmic.dev/output-type: prometheus
  # Or select by specific cluster
  # selector:
  #   matchLabels:
  #     operator.gnmic.dev/cluster-name: telemetry-cluster
  
  # Namespaces to look for Services
  namespaceSelector:
    matchNames:
      - default
    # Or monitor all namespaces:
    # any: true
  
  # Endpoint configuration
  endpoints:
    - port: metrics
      path: /metrics
      interval: 30s
      scrapeTimeout: 10s
{{< /tab >}}
{{< tab header="kubectl" lang="bash" >}}
cat << 'EOF' | kubectl apply -f -
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: gnmic-telemetry
  labels:
    release: prometheus
spec:
  selector:
    matchLabels:
      operator.gnmic.dev/output-type: prometheus
  namespaceSelector:
    matchNames:
      - default
  endpoints:
    - port: metrics
      path: /metrics
      interval: 30s
      scrapeTimeout: 10s
EOF
{{< /tab >}}
{{< /tabpane >}}

### Key Configuration Options

| Field | Description |
|-------|-------------|
| `selector.matchLabels` | Labels to match Services. The gNMIc operator adds `operator.gnmic.dev/output-type: prometheus` to all Prometheus output Services |
| `namespaceSelector` | Which namespaces to search for matching Services |
| `endpoints[].port` | Port name on the Service (gNMIc uses `metrics`) |
| `endpoints[].interval` | How often Prometheus scrapes the endpoint |
| `endpoints[].scrapeTimeout` | Timeout for scrape requests |

## Step 3: Verify the Integration

### Check ServiceMonitor is Discovered

```bash
kubectl get servicemonitors
```

### Check Prometheus Targets

Port-forward to Prometheus and check the targets page:

```bash
kubectl port-forward svc/prometheus-operated 9090:9090
```

Open http://localhost:9090/targets and look for your gNMIc endpoints.

### Query Metrics

Once targets are discovered, you can query gNMIc metrics in Prometheus:

```promql
# Interface counters
gnmic_interfaces_interface_state_counters_in_octets

# All metrics from a specific target
{source="router1"}

# Rate of incoming octets
rate(gnmic_interfaces_interface_state_counters_in_octets[5m])
```

## Complete Example

Here's a complete example that sets up end-to-end telemetry collection with Prometheus integration.

### 1. Target and Subscription

{{< tabpane >}}
{{< tab header="YAML" lang="yaml" >}}
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: default-profile
spec:
  credentialsRef: device-credentials
  tls: {}
  timeout: 10s
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: spine1
  labels:
    role: spine
spec:
  address: 10.0.0.1:57400
  profile: default-profile
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: spine2
  labels:
    role: spine
spec:
  address: 10.0.0.2:57400
  profile: default-profile
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Subscription
metadata:
  name: interface-stats
  labels:
    type: interfaces
spec:
  paths:
    - /interfaces/interface/state/counters
    - /interfaces/interface/state/oper-status
  mode: STREAM
  streamMode: SAMPLE
  sampleInterval: 10s
{{< /tab >}}
{{< tab header="kubectl" lang="bash" >}}
# Create credentials secret first
kubectl create secret generic device-credentials \
  --from-literal=username=admin \
  --from-literal=password=admin

# Apply resources
cat << 'EOF' | kubectl apply -f -
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: default-profile
spec:
  credentialsRef: device-credentials
  tls: {}
  timeout: 10s
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: spine1
  labels:
    role: spine
spec:
  address: 10.0.0.1:57400
  profile: default-profile
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: spine2
  labels:
    role: spine
spec:
  address: 10.0.0.2:57400
  profile: default-profile
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Subscription
metadata:
  name: interface-stats
  labels:
    type: interfaces
spec:
  paths:
    - /interfaces/interface/state/counters
    - /interfaces/interface/state/oper-status
  mode: STREAM
  streamMode: SAMPLE
  sampleInterval: 10s
EOF
{{< /tab >}}
{{< /tabpane >}}

### 2. Output, Pipeline, and Cluster

{{< tabpane >}}
{{< tab header="YAML" lang="yaml" >}}
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prom-output
  labels:
    output-type: prometheus
spec:
  type: prometheus
  config:
    listen: ":9804"
    path: /metrics
    metric-prefix: gnmic
    export-timestamps: true
    strings-as-labels: true
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: spine-telemetry
spec:
  clusterRef: telemetry-cluster
  enabled: true
  targetSelectors:
    - matchLabels:
        role: spine
  subscriptionSelectors:
    - matchLabels:
        type: interfaces
  outputs:
    outputSelectors:
      - matchLabels:
          output-type: prometheus
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: telemetry-cluster
spec:
  replicas: 2
  image: ghcr.io/openconfig/gnmic:latest
  api:
    restPort: 7890
  resources:
    requests:
      memory: "128Mi"
      cpu: "100m"
    limits:
      memory: "256Mi"
      cpu: "500m"
{{< /tab >}}
{{< tab header="kubectl" lang="bash" >}}
cat << 'EOF' | kubectl apply -f -
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prom-output
  labels:
    output-type: prometheus
spec:
  type: prometheus
  config:
    listen: ":9804"
    path: /metrics
    metric-prefix: gnmic
    export-timestamps: true
    strings-as-labels: true
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: spine-telemetry
spec:
  clusterRef: telemetry-cluster
  enabled: true
  targetSelectors:
    - matchLabels:
        role: spine
  subscriptionSelectors:
    - matchLabels:
        type: interfaces
  outputs:
    outputSelectors:
      - matchLabels:
          output-type: prometheus
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: telemetry-cluster
spec:
  replicas: 2
  image: ghcr.io/openconfig/gnmic:latest
  api:
    restPort: 7890
  resources:
    requests:
      memory: "128Mi"
      cpu: "100m"
    limits:
      memory: "256Mi"
      cpu: "500m"
EOF
{{< /tab >}}
{{< /tabpane >}}

### 3. ServiceMonitor

{{< tabpane >}}
{{< tab header="YAML" lang="yaml" >}}
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: gnmic-telemetry
  labels:
    release: prometheus  # Match your Prometheus serviceMonitorSelector
spec:
  selector:
    matchLabels:
      operator.gnmic.dev/output-type: prometheus
  namespaceSelector:
    matchNames:
      - default
  endpoints:
    - port: metrics
      path: /metrics
      interval: 15s
      scrapeTimeout: 10s
      # Optional: relabel configs
      relabelings:
        - sourceLabels: [__meta_kubernetes_service_name]
          targetLabel: service
        - sourceLabels: [__meta_kubernetes_namespace]
          targetLabel: namespace
{{< /tab >}}
{{< tab header="kubectl" lang="bash" >}}
cat << 'EOF' | kubectl apply -f -
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: gnmic-telemetry
  labels:
    release: prometheus
spec:
  selector:
    matchLabels:
      operator.gnmic.dev/output-type: prometheus
  namespaceSelector:
    matchNames:
      - default
  endpoints:
    - port: metrics
      path: /metrics
      interval: 15s
      scrapeTimeout: 10s
      relabelings:
        - sourceLabels: [__meta_kubernetes_service_name]
          targetLabel: service
        - sourceLabels: [__meta_kubernetes_namespace]
          targetLabel: namespace
EOF
{{< /tab >}}
{{< /tabpane >}}

## Service Labels

The gNMIc operator adds the following labels to Prometheus output Services, which you can use in ServiceMonitor selectors:

| Label | Description | Example |
|-------|-------------|---------|
| `operator.gnmic.dev/cluster-name` | Name of the gNMIc Cluster | `telemetry-cluster` |
| `operator.gnmic.dev/output-type` | Type of output | `prometheus` |
| `operator.gnmic.dev/output-name` | Name of the Output resource | `prom-output` |

### Selector Examples

Select all Prometheus outputs:

```yaml
selector:
  matchLabels:
    operator.gnmic.dev/output-type: prometheus
```

Select outputs from a specific cluster:

```yaml
selector:
  matchLabels:
    operator.gnmic.dev/cluster-name: production-cluster
```

Select a specific output:

```yaml
selector:
  matchLabels:
    operator.gnmic.dev/output-name: prom-output
```

## Grafana Dashboards

Once metrics are flowing into Prometheus, you can create Grafana dashboards to visualize the telemetry data.

### Example Dashboard Queries

**Interface Traffic (bits/sec):**

```promql
rate(gnmic_interfaces_interface_state_counters_in_octets{interface_name="Ethernet1"}[5m]) * 8
```

**Top 10 Interfaces by Traffic:**

```promql
topk(10, rate(gnmic_interfaces_interface_state_counters_in_octets[5m]) * 8)
```

**Interface Operational Status:**

```promql
gnmic_interfaces_interface_state_oper_status
```

## Troubleshooting

### ServiceMonitor Not Discovered

1. Verify the ServiceMonitor has the correct labels to match your Prometheus `serviceMonitorSelector`:

   ```bash
   kubectl get prometheus -o yaml | grep -A5 serviceMonitorSelector
   ```

2. Check that the ServiceMonitor namespace is included in Prometheus `serviceMonitorNamespaceSelector`

### No Targets in Prometheus

1. Verify the gNMIc Service exists:

   ```bash
   kubectl get svc -l operator.gnmic.dev/output-type=prometheus
   ```

2. Check the Service has endpoints:

   ```bash
   kubectl get endpoints -l operator.gnmic.dev/output-type=prometheus
   ```

3. Verify the gNMIc pods are running:

   ```bash
   kubectl get pods -l operator.gnmic.dev/cluster-name=telemetry-cluster
   ```

### Metrics Not Appearing

1. Check gNMIc is receiving telemetry:

   ```bash
   kubectl port-forward svc/gnmic-telemetry-cluster-prom-prom-output 9804:9804
   curl http://localhost:9804/metrics
   ```

2. Verify targets are connected in gNMIc:

   ```bash
   kubectl port-forward pod/gnmic-telemetry-cluster-0 7890:7890
   curl http://localhost:7890/api/v1/config/targets
   ```

## Next Steps

- [Output Configuration]({{< relref "../../../user-guide/output" >}}) - Advanced output settings
- [Pipeline Configuration]({{< relref "../../../user-guide/pipeline" >}}) - Complex pipeline scenarios
- [Scaling]({{< relref "../../../advanced/scaling" >}}) - Scale your telemetry collection
