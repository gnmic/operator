---
title: "NATS"
linkTitle: "NATS"
weight: 1
description: >
  NATS deployment
---


This guide shows how to configure gNMIc Operator to send telemetry data to NATS.

## Prerequisites

- A running Kubernetes cluster with gNMIc Operator installed
- NATS deployed in your cluster ( using the [NATS Helm chart](https://artifacthub.io/packages/helm/nats/nats))

## Deploy NATS

Deploy a simple NATS cluster:

```bash
helm repo add nats https://nats-io.github.io/k8s/helm/charts/
helm install nats nats/nats
```

This creates a NATS cluster and a Service named `nats` with port `4222`.

## NATS Output with Service Reference

Create an Output that references the NATS Service:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: nats-output
  labels:
    type: nats
spec:
  type: nats
  serviceRef:
    name: nats
    port: client  # or "4222"
  config:
    subject: telemetry.events
    subject-prefix: gnmic
```

The operator automatically resolves the service to `nats://nats.{namespace}.svc.cluster.local:4222`.

## Complete Example

### 1. Create a Target

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: router1
  labels:
    vendor: nokia
spec:
  address: router1.example.com:57400
  profile: default
```

### 2. Create a TargetProfile

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: default
spec:
  credentialsRef: router-credentials
```

### 3. Create a Subscription

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Subscription
metadata:
  name: interfaces
  labels:
    type: streaming
spec:
  paths:
    - /interfaces/interface/state/counters
  mode: STREAM/SAMPLE
  sampleInterval: 10s
```

### 4. Create the NATS Output

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: nats-output
  labels:
    type: nats
spec:
  type: nats
  serviceRef:
    name: nats
    port: client
  config:
    subject: telemetry.interfaces
```

### 5. Create the Pipeline

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: nats-pipeline
spec:
  clusterRef: my-cluster
  enabled: true
  targetSelectors:
    - matchLabels:
        vendor: nokia
  subscriptionSelectors:
    - matchLabels:
        type: streaming
  outputs:
    outputSelectors:
      - matchLabels:
          type: nats
```

## JetStream Output

For NATS JetStream, use the `jetstream` output type:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: jetstream-output
spec:
  type: jetstream
  serviceRef:
    name: nats
    port: client
  config:
    subject: telemetry.events
    stream: TELEMETRY
```

## Verify Messages

Subscribe to the NATS subject to verify messages are being received:

```bash
# Using nats CLI
kubectl exec -it deploy/nats-box -- nats sub "telemetry.>"
```

## Cross-Namespace NATS

If NATS is in a different namespace:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: nats-output
  namespace: telemetry
spec:
  type: nats
  serviceRef:
    name: nats
    namespace: messaging  # NATS is in 'messaging' namespace
    port: client
  config:
    subject: telemetry.events
```
