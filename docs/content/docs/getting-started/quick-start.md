---
title: "Quick Start"
linkTitle: "Quick Start"
weight: 2
description: >
  Deploy your first gNMIc telemetry collector
---

This guide walks you through deploying a complete telemetry collection setup with gNMIc Operator.

## Overview

We'll create:
1. A **TargetProfile** with connection settings
2. A **Target** pointing to a network device
3. A **Subscription** defining what data to collect
4. An **Output** to send data to Prometheus
5. A **Pipeline** connecting everything together
6. A **Cluster** to run the gNMIc collectors

## Step 1: Create a TargetProfile

The TargetProfile defines shared settings for connecting to devices:

{{< tabpane >}}
{{< tab header="YAML" lang="yaml" >}}
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: default-profile
spec:
  # Reference to a Secret containing username/password
  credentialsRef: device-credentials
  # TLS without server certificate verification
  tls: {}
  # Connection timeout
  timeout: 10s
{{< /tab >}}
{{< tab header="kubectl" lang="bash" >}}
cat << 'EOF' | kubectl apply -f -
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: default-profile
spec:
  # Reference to a Secret containing username/password
  credentialsRef: device-credentials
  # TLS without server certificate verification
  tls: {}
  # Connection timeout
  timeout: 10s
EOF
{{< /tab >}}
{{< /tabpane >}}

Create the credentials secret:

```bash
kubectl create secret generic device-credentials \
  --from-literal=username=admin \
  --from-literal=password=admin
```

## Step 2: Create a Target

Define a network device to collect telemetry from.

Set the name, address and port to match your environment.

{{% alert title="Note" color="info" %}} Notice the labels, they will come in handy later on. {{% /alert %}}

{{< tabpane >}}
{{< tab header="YAML" lang="yaml" >}}
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: router1
  labels:
    vendor: vendorA
    role: core
spec:
  address: 10.0.0.1:57400
  profile: default-profile
{{< /tab >}}
{{< tab header="kubectl" lang="bash" >}}
cat << 'EOF' | kubectl apply -f -
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: router1
  labels:
    vendor: vendorA
    role: core
spec:
  address: 10.0.0.1:57400
  profile: default-profile
EOF
{{< /tab >}}
{{< /tabpane >}}

## Step 3: Create a Subscription

Define what telemetry data to collect:

{{< tabpane >}}
{{< tab header="YAML" lang="yaml" >}}
apiVersion: operator.gnmic.dev/v1alpha1
kind: Subscription
metadata:
  name: interface-counters
  labels:
    type: interfaces
spec:
  paths:
    - /interfaces/interface/state/counters
  mode: STREAM/SAMPLE
  sampleInterval: 10s
{{< /tab >}}
{{< tab header="kubectl" lang="bash" >}}
cat << 'EOF' | kubectl apply -f -
apiVersion: operator.gnmic.dev/v1alpha1
kind: Subscription
metadata:
  name: interface-counters
  labels:
    type: interfaces
spec:
  paths:
    - /interfaces/interface/state/counters
  mode: STREAM/SAMPLE
  sampleInterval: 10s
EOF
{{< /tab >}}
{{< /tabpane >}}

## Step 4: Create an Output

Configure where to send the telemetry data:

{{< tabpane >}}
{{< tab header="YAML" lang="yaml" >}}
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-output
  labels:
    type: metrics
spec:
  type: prometheus
{{< /tab >}}
{{< tab header="kubectl" lang="bash" >}}
cat << 'EOF' | kubectl apply -f -
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: prometheus-output
  labels:
    type: prometheus
spec:
  type: prometheus
EOF
{{< /tab >}}
{{< /tabpane >}}

## Step 5: Create a Pipeline

Connect targets, subscriptions, and outputs:
Remeber those labels from the previous resources ? They are used in the Pipeline resource to bring targets, subscriptions and outputs together without having to reference them by name.

{{< tabpane >}}
{{< tab header="YAML" lang="yaml" >}}
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: core-telemetry
spec:
  clusterRef: gnmic-cluster
  enabled: true
  # Select targets by label
  targetSelectors:
    - matchLabels:
        vendor: vendorA
        role: core
  # Select subscriptions by label
  subscriptionSelectors:
    - matchLabels:
        type: interfaces
  # Select outputs by label
  outputs:
    outputSelectors:
      - matchLabels:
          type: metrics
{{< /tab >}}
{{< tab header="kubectl" lang="bash" >}}
cat << 'EOF' | kubectl apply -f -
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: core-telemetry
spec:
  clusterRef: gnmic-cluster
  enabled: true
  # Select targets by label
  targetSelectors:
    - matchLabels:
        vendor: vendorA
        role: core
  # Select subscriptions by label
  subscriptionSelectors:
    - matchLabels:
        type: interfaces
  # Select outputs by label
  outputs:
    outputSelectors:
      - matchLabels:
          type: prometheus
EOF
{{< /tab >}}
{{< /tabpane >}}


{{% alert title="Note" color="info" %}} If label selectors are too "magical" for you, the Pipeline CR supports direct references for all resources. {{% /alert %}}

## Step 6: Create a Cluster

Deploy the gNMIc collectors:

{{< tabpane >}}
{{< tab header="YAML" lang="yaml" >}}
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: core-cluster
spec:
  replicas: 3
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
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: core-cluster
spec:
  replicas: 3
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

## Verify the Deployment

Check that the pods are running:

```bash
kubectl get pods -l operator.gnmic.dev/cluster-name=core-cluster
```

```
NAME                  READY   STATUS    RESTARTS   AGE
gnmic-core-cluster-0   1/1     Running   0          30s
gnmic-core-cluster-1   1/1     Running   0          28s
gnmic-core-cluster-2   1/1     Running   0          28s
```

Check the services:

```bash
kubectl get svc -l operator.gnmic.dev/cluster-name=core-cluster
```

```
NAME                              TYPE        CLUSTER-IP       PORT(S)
gnmic-core-cluster               ClusterIP   None             7890/TCP
gnmic-core-cluster-prom-prometheus-output   ClusterIP   10.96.xxx.xxx   9804/TCP
```

## Access Prometheus Metrics

Configure your Prometheus server to scrape the created `gnmic-core-cluster-prom-prometheus-output.
The Service is labeled and annotated to facilitate discovery using Promehteuss Kubernetes SD and Prometheus Operator ServiceMonitor

## Next Steps

- [Cluster Configuration]({{< relref "../user-guide/cluster" >}}) - Advanced cluster settings
- [Pipeline Configuration]({{< relref "../user-guide/pipeline" >}}) - Complex pipeline scenarios
- [Scaling]({{< relref "../advanced/scaling" >}}) - Scale your telemetry collection

