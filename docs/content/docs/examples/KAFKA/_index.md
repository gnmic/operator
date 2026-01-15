---
title: "Kafka"
linkTitle: "Kafka"
weight: 6
draft: true
description: >
  Kafka integration examples
---

This guide shows how to configure gNMIc Operator to send telemetry data to Apache Kafka.

## Prerequisites

- A running Kubernetes cluster with gNMIc Operator installed
- Kafka deployed in your cluster ( using [Strimzi](https://strimzi.io/) or [Confluent Operator](https://docs.confluent.io/operator/current/overview.html))

## Deploy Kafka with Strimzi

```bash
# install Strimzi operator
kubectl create namespace kafka
kubectl apply -f 'https://strimzi.io/install/latest?namespace=kafka' -n kafka

# create a Kafka cluster
cat <<EOF | kubectl apply -f -
apiVersion: kafka.strimzi.io/v1beta2
kind: Kafka
metadata:
  name: my-cluster
  namespace: kafka
spec:
  kafka:
    replicas: 3
    listeners:
      - name: plain
        port: 9092
        type: internal
        tls: false
    storage:
      type: ephemeral
  zookeeper:
    replicas: 3
    storage:
      type: ephemeral
EOF
```

This creates a bootstrap Service named `my-cluster-kafka-bootstrap` with port `9092`.

## Kafka Output with Service Reference

Create an Output that references the Kafka bootstrap Service:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: kafka-output
  labels:
    type: kafka
spec:
  type: kafka
  serviceRef:
    name: my-cluster-kafka-bootstrap
    namespace: kafka
    port: "9092"
  config:
    topic: telemetry
    encoding: json
```

The operator resolves the service to `my-cluster-kafka-bootstrap.kafka.svc.cluster.local:9092`.

## Complete Example

### 1. Create a Target

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Target
metadata:
  name: switch1
  labels:
    role: leaf
spec:
  address: switch1.example.com:57400
  profile: default
```

### 2. Create a TargetProfile

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: default
spec:
  credentialsRef: device-credentials
```

### 3. Create a Subscription

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Subscription
metadata:
  name: interface-counters
  labels:
    type: streaming
spec:
  paths:
    - /interfaces/interface/state/counters
  mode: STREAM/SAMPLE
  sampleInterval: 10s
```

### 4. Create the Kafka Output

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: kafka-output
  labels:
    type: kafka
spec:
  type: kafka
  serviceRef:
    name: my-cluster-kafka-bootstrap
    namespace: kafka
    port: "9092"
  config:
    topic: network-telemetry
    encoding: json
    num-workers: 4
    timeout: 10s
    recovery-wait-time: 5s
```

### 5. Create the Pipeline

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: kafka-pipeline
spec:
  clusterRef: my-cluster
  enabled: true
  targetSelectors:
    - matchLabels:
        role: leaf
  subscriptionSelectors:
    - matchLabels:
        type: streaming
  outputs:
    outputSelectors:
      - matchLabels:
          type: kafka
```

## Using Service Selector for Multiple Brokers

If you want to discover multiple Kafka broker services:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: kafka-output
spec:
  type: kafka
  serviceSelector:
    matchLabels:
      strimzi.io/cluster: my-cluster
      strimzi.io/kind: Kafka
    namespace: kafka
    port: "9092"
  config:
    topic: telemetry
```

## Kafka with SASL Authentication

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: kafka-secure
spec:
  type: kafka
  serviceRef:
    name: my-cluster-kafka-bootstrap
    namespace: kafka
    port: "9092"
  config:
    topic: telemetry
    sasl:
      mechanism: SCRAM-SHA-512
      user: kafka-user
      password: kafka-password
```

## Verify Messages

Consume messages from the topic to verify:

```bash
# using Strimzi kafka-console-consumer
kubectl -n kafka run kafka-consumer -ti --rm=true --restart=Never \
  --image=quay.io/strimzi/kafka:latest-kafka-3.5.0 \
  -- bin/kafka-console-consumer.sh \
  --bootstrap-server my-cluster-kafka-bootstrap:9092 \
  --topic network-telemetry \
  --from-beginning
```

## Cross-Namespace Kafka

If Kafka is in a different namespace than your outputs:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Output
metadata:
  name: kafka-output
  namespace: telemetry
spec:
  type: kafka
  serviceRef:
    name: my-cluster-kafka-bootstrap
    namespace: kafka  # The Kafka cluster is in 'kafka' namespace
    port: "9092"
  config:
    topic: telemetry
```
