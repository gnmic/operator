---
title: "Documentation"
linkTitle: "Documentation"
weight: 20
menu:
  main:
    weight: 20
---

Welcome to the gNMIc Operator documentation. This guide will help you deploy and manage gNMIc telemetry collectors on Kubernetes.

## What is gNMIc Operator?

gNMIc Operator is a Kubernetes operator that manages the lifecycle of [gNMIc](https://gnmic.dev) collectors. It allows you to:

- **Deploy** gNMIc collectors as StatefulSets with automatic service discovery
- **Configure** targets, subscriptions, and outputs using Kubernetes Custom Resources
- **Scale** horizontally with automatic target distribution across pods
- **Update** configuration dynamically without pod restarts

## Key Concepts

| Resource | Description |
|----------|-------------|
| **Cluster** | A gNMIc collector deployment (StatefulSet + Services) |
| **Pipeline** | Connects targets, subscriptions, and outputs together |
| **Target** | A network device to collect telemetry from |
| **TargetProfile** | Shared configuration for targets (credentials, TLS) |
| **Subscription** | Defines what data to collect (paths, mode, interval) |
| **Output** | Where telemetry data is sent (Prometheus, Kafka, etc.) |
| **Input** | External data sources (Kafka, NATS) |

## Quick Links

- [Installation]({{< relref "getting-started/installation" >}}) - Install the operator
- [Quick Start]({{< relref "getting-started/quick-start" >}}) - Deploy your first collector
- [Cluster Configuration]({{< relref "user-guide/cluster" >}}) - Configure gNMIc clusters
- [Pipeline Configuration]({{< relref "user-guide/pipeline" >}}) - Wire resources together
