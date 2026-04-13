---
title: "Remote Write"
linkTitle: "Remote Write"
weight: 2
description: >
  Push gNMIc network telemetry metrics to Prometheus or Grafana Mimir using Prometheus Remote Write
---

## Overview

A `prometheus_write` `Output` configures gNMIc to POST Snappy-compressed Prometheus remote write payloads to an HTTP endpoint. Prometheus accepts remote write at [`/api/v1/write`](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#remote_write); Grafana Mimir’s distributor accepts the same protocol at [`/api/v1/push`](https://grafana.com/docs/mimir/latest/operators-guide/reference-http-api/#remote-write).

Unlike the `prometheus` (scrape) output, the operator does **not** create a Service for `prometheus_write`. You point the output at an existing Prometheus or Mimir (distributor) Service inside the cluster or at any reachable URL.

**Recommended:** use `serviceRef` (or `serviceSelector`) with optional `url` set to the **path segment** for remote write (`api/v1/write` for Prometheus, `api/v1/push` for Mimir). The operator resolves the Service to `http(s)://…:port` and appends that suffix, so you do not hardcode the full hostname in `config`.

**Alternative:** set a full `config.url` (for example if the backend is outside the cluster or you do not use service discovery).

The sections below walk through installing **either** kube-prometheus-stack (Prometheus Operator + Prometheus) **or** mimir-distributed (Grafana Mimir) with Helm, then wiring the gNMIc `Output`.

## Prerequisites

- Kubernetes **1.25+** (match your platform’s support window), **`kubectl`**, and **[Helm 3.8+](https://helm.sh/docs/intro/install/)**
- Cluster with a default **StorageClass** if you install Mimir (built-in MinIO needs PVCs)
- **gNMIc Operator** installed in the cluster ([installation]({{< relref "../../../getting-started/installation" >}}))
- Enough resources: Mimir’s getting-started path assumes roughly **4 CPU / 16 GiB RAM** on the cluster for the default chart ([hardware note](https://grafana.com/docs/helm-charts/mimir-distributed/latest/get-started-helm-charts/))

Complete **either** the Prometheus **or** the Mimir install below (not necessarily both). The examples use fixed Helm release names so Service names stay predictable.

---

## Install a metrics backend (pick one)

### Option A: Prometheus Operator stack (`kube-prometheus-stack`)

The **[kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack)** Helm chart installs the [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator), Prometheus, Alertmanager, and related CRDs. Remote write ingestion requires the Prometheus **remote write receiver** flag.

#### Step A.1 — Add the Helm repository

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
```

#### Step A.2 — Values file to accept remote write

Create `kps-values.yaml` with:

```yaml
prometheus:
  prometheusSpec:
    enableRemoteWriteReceiver: true
```

This sets Prometheus’s [`--web.enable-remote-write-receiver`](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#remote_write) so gNMIc can `POST` to `/api/v1/write`.

#### Step A.3 — Install into namespace `monitoring`

```bash
kubectl create namespace monitoring
helm install kps prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  -f kps-values.yaml
```

If **`kube-prometheus-stack` is already installed** under release name `kps`, merge `kps-values.yaml` and run:

```bash
helm upgrade kps prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  -f kps-values.yaml
```

Use a different release name instead of `kps` if you prefer; the Prometheus **Service** name will change (see Step A.5).

#### Step A.4 — Wait until Prometheus is ready

```bash
kubectl -n monitoring wait pod \
  -l app.kubernetes.io/name=prometheus \
  --for=condition=Ready \
  --timeout=10m
```

#### Step A.5 — Confirm the Prometheus Service name and port

For release name **`kps`**, the chart exposes the Prometheus server Service as:

```text
kps-kube-prometheus-prometheus
```

Verify it and the port (usually **9090**):

```bash
kubectl -n monitoring get svc kps-kube-prometheus-prometheus
```

If you used another release name, list Services and pick the one for Prometheus:

```bash
kubectl -n monitoring get svc | grep -i prometheus
```

You will use this Service in `serviceRef.name` in **Step 1**. The DNS name inside the cluster is:

```text
kps-kube-prometheus-prometheus.monitoring.svc.cluster.local
```

---

### Option B: Grafana Mimir (`mimir-distributed`)

The **[mimir-distributed](https://grafana.com/docs/helm-charts/mimir-distributed/latest/)** Helm chart installs Grafana Mimir. Remote write uses [`POST /api/v1/push`](https://grafana.com/docs/mimir/latest/operators-guide/reference-http-api/#remote-write) on the **distributor**, or the same path via the chart’s **nginx** gateway (common in Grafana’s docs).

These steps follow Grafana’s [Get started with Grafana Mimir using the Helm chart](https://grafana.com/docs/helm-charts/mimir-distributed/latest/get-started-helm-charts/). For production tuning, see [Run Grafana Mimir in production using the Helm chart](https://grafana.com/docs/helm-charts/mimir-distributed/latest/run-production-environment-with-helm/).

#### Step B.1 — Add the Helm repository

```bash
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update
```

#### Step B.2 — Namespace for Mimir

```bash
kubectl create namespace mimir-test
```

#### Step B.3 — Install Mimir

```bash
helm -n mimir-test install mimir grafana/mimir-distributed
```

The install can take several minutes (many StatefulSets and Deployments). Wait until pods are `Running` or `Completed`:

```bash
kubectl -n mimir-test get pods -w
```

Press Ctrl+C when satisfied. To confirm workloads are up:

```bash
kubectl -n mimir-test get deploy,sts
```

#### Step B.4 — Services to use for remote write

List Services:

```bash
kubectl -n mimir-test get svc
```

Typical options:

| Entry point | Service (release `mimir`) | Port | `serviceRef.url` |
|-------------|-----------------------------|------|------------------|
| **Nginx** (same path as [Grafana get-started](https://grafana.com/docs/helm-charts/mimir-distributed/latest/get-started-helm-charts/)) | `mimir-nginx` | **80** | `api/v1/push` |
| **Distributor** (direct) | `mimir-distributor` | **8080** (often `http-metrics`) | `api/v1/push` |

Use **`mimir-nginx`** on port **80** if you want to match Grafana’s examples; use **`mimir-distributor`** if you prefer to talk to the component directly (confirm the port with `kubectl get svc mimir-distributor -n mimir-test`).

#### Step B.5 — (Optional) Query from Grafana

To browse metrics in Grafana against this install, follow the **Start Grafana** section in the [same get-started guide](https://grafana.com/docs/helm-charts/mimir-distributed/latest/get-started-helm-charts/) (data source URL is usually the **query-frontend** or **nginx** Prometheus-compatible endpoint).

---

## Step 1: Create a Prometheus Remote Write output

Create an `Output` with `type: prometheus_write`, a `serviceRef` to your Prometheus or Mimir Service, and `url` set to the remote-write path. Align metric behavior with the scraping example using `metric-prefix` and `strings-as-labels`.

Substitute `name`, `namespace`, and `port` to match `kubectl get svc` for your install.

{{< tabpane >}}
{{< tab header="YAML (service ref)" lang="yaml" >}}
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-remote-write
  labels:
    app: gnmic
    output-type: prometheus-remote-write
spec:
  type: prometheus_write
  serviceRef:
    name: kps-kube-prometheus-prometheus
    namespace: monitoring
    port: "9090"
    # Path only; operator joins to http://<svc>.<ns>.svc.cluster.local:<port>/
    url: api/v1/write
    # Mimir example (Helm release "mimir" in namespace mimir-test; nginx gateway):
    # name: mimir-nginx
    # namespace: mimir-test
    # port: "80"
    # url: api/v1/push
  config:
    timeout: 10s
    interval: 10s
    metric-prefix: gnmic
    strings-as-labels: true
{{< /tab >}}
{{< tab header="YAML (static URL)" lang="yaml" >}}
# If you prefer not to use serviceRef, set the full URL in config:
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-remote-write
  labels:
    app: gnmic
    output-type: prometheus-remote-write
spec:
  type: prometheus_write
  config:
    url: http://kps-kube-prometheus-prometheus.monitoring.svc.cluster.local:9090/api/v1/write
    timeout: 10s
    interval: 10s
    metric-prefix: gnmic
    strings-as-labels: true
{{< /tab >}}
{{< /tabpane >}}

### Key configuration options

| Field | Description |
|-------|-------------|
| `serviceRef.url` / `serviceSelector.url` | Path appended after the resolved `scheme://host:port` (for example `api/v1/write` or `api/v1/push`). Leading `/` optional. |
| `config.url` | Full URL for remote write when not using `serviceRef` / `serviceSelector` |
| `timeout` | HTTP client timeout per request |
| `interval` | How often gNMIc flushes buffered series to the remote |
| `metric-prefix` | Prefix for metric names (same idea as the `prometheus` scrape output) |
| `strings-as-labels` | Map string values to labels where applicable |

For TLS, authentication, and more `serviceRef` / `serviceSelector` examples, see [Output configuration]({{< relref "../../../user-guide/output" >}}#prometheus-remote-write-output).

## Step 2: Reference the output from a Pipeline

Use a label selector (or direct ref) so your pipeline sends telemetry to this output, the same way the scraping lab selects the `prometheus` output.

{{< tabpane >}}
{{< tab header="YAML" lang="yaml" >}}
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: spine-telemetry-remote-write
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
          output-type: prometheus-remote-write
{{< /tab >}}
{{< tab header="kubectl" lang="bash" >}}
cat << 'EOF' | kubectl apply -f -
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: spine-telemetry-remote-write
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
          output-type: prometheus-remote-write
EOF
{{< /tab >}}
{{< /tabpane >}}

## Step 3: Verify

### Prometheus

Port-forward to the Prometheus UI (adjust Service name and namespace):

```bash
kubectl -n monitoring port-forward svc/kps-kube-prometheus-prometheus 9090:9090
```

Open http://localhost:9090/graph and run the same style of queries as in the scraping guide, for example:

```promql
gnmic_interfaces_interface_state_counters_in_octets
```

Allow one or two flush intervals (`interval` on the output, default behavior in gNMIc) after targets are connected.

### Grafana Mimir

If you followed **Option B** with release `mimir` in `mimir-test`, the chart exposes a Prometheus-compatible query path on **nginx** (see [Grafana get-started](https://grafana.com/docs/helm-charts/mimir-distributed/latest/get-started-helm-charts/)). You can point Grafana’s data source at `http://mimir-nginx.mimir-test.svc.cluster.local/prometheus` and run the same PromQL in **Explore**.

To smoke-test without Grafana, port-forward nginx and call the instant query API (adjust the port shown by `kubectl -n mimir-test get svc mimir-nginx` if needed):

```bash
kubectl -n mimir-test port-forward svc/mimir-nginx 8080:80
```

```bash
curl -sG --data-urlencode 'query=gnmic_interfaces_interface_state_counters_in_octets' \
  'http://127.0.0.1:8080/prometheus/api/v1/query' | head -c 500
```

PromQL examples are the same as for Prometheus; metric names still use your `metric-prefix`. For full HTTP API details, see the [Mimir reference](https://grafana.com/docs/mimir/latest/operators-guide/reference-http-api/).

### gNMIc pod logs

If metrics do not appear, check gNMIc cluster pods for `prometheus_write` errors (HTTP status, TLS, or DNS):

```bash
kubectl logs -l operator.gnmic.dev/cluster-name=telemetry-cluster --tail=100
```

## Complete example

End-to-end layout matches the scraping lab: credentials, targets, subscription, `Cluster`, `prometheus_write` `Output`, and `Pipeline`. The `Output` uses `serviceRef` with `url: api/v1/write`. Adjust `serviceRef` (or switch to a static `config.url`) for your Prometheus or Mimir Service.

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
  mode: STREAM/SAMPLE
  sampleInterval: 10s
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prom-rw-output
  labels:
    output-type: prometheus-remote-write
spec:
  type: prometheus_write
  serviceRef:
    name: kps-kube-prometheus-prometheus
    namespace: monitoring
    port: "9090"
    url: api/v1/write
  config:
    timeout: 10s
    interval: 10s
    metric-prefix: gnmic
    strings-as-labels: true
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: spine-telemetry-rw
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
          output-type: prometheus-remote-write
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
# Create credentials secret first (same as scraping lab)
kubectl create secret generic device-credentials \
  --from-literal=username=admin \
  --from-literal=password=admin

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
  mode: STREAM/SAMPLE
  sampleInterval: 10s
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prom-rw-output
  labels:
    output-type: prometheus-remote-write
spec:
  type: prometheus_write
  serviceRef:
    name: kps-kube-prometheus-prometheus
    namespace: monitoring
    port: "9090"
    url: api/v1/write
  config:
    timeout: 10s
    interval: 10s
    metric-prefix: gnmic
    strings-as-labels: true
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: spine-telemetry-rw
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
          output-type: prometheus-remote-write
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

### Example PromQL (Prometheus or Mimir)

```promql
# Interface counters
gnmic_interfaces_interface_state_counters_in_octets

# Rate of incoming octets
rate(gnmic_interfaces_interface_state_counters_in_octets[5m])
```

## Grafana dashboards

After data lands in Prometheus or Mimir, use the same dashboard ideas as in the [scraping guide]({{< relref "../Scraping" >}}#grafana-dashboards)—for example interface traffic from `rate(...)* 8` for bits per second.

## Troubleshooting

### `400` / `404` / `405` from the remote URL

- Confirm the path: `/api/v1/write` for Prometheus, `/api/v1/push` for Mimir.
- For Prometheus, verify `enableRemoteWriteReceiver` (or equivalent flag) is enabled.

### Connection refused or DNS errors

- Check the URL host matches a Service in the same cluster: `kubectl get svc -n <namespace>`.
- Ensure gNMIc pods can resolve `*.svc.cluster.local` (standard cluster DNS).

### No series in Prometheus / Mimir

- Confirm targets are up and subscriptions match: same checks as the scraping lab.

  ```bash
  kubectl port-forward pod/gnmic-telemetry-cluster-0 7890:7890
  curl -s http://localhost:7890/api/v1/config/targets
  ```

- Increase logging if needed: set `debug: true` under the output `config` (gNMIc `prometheus_write` supports this) and re-check pod logs.

### TLS or authentication

Configure `tls`, `authentication`, or `authorization` under the output `config`, or use HTTPS URLs. See [Prometheus Remote Write output]({{< relref "../../../user-guide/output" >}}#prometheus-remote-write-output).

## Next steps

- [Output configuration]({{< relref "../../../user-guide/output" >}}) — TLS, `serviceRef` / `serviceSelector` `url`, and headers
- [Pipeline configuration]({{< relref "../../../user-guide/pipeline" >}})
- [Scraping with ServiceMonitor]({{< relref "../Scraping" >}}) — pull-based alternative
