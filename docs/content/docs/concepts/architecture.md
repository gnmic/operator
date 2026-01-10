---
title: "Architecture"
linkTitle: "Architecture"
weight: 1
description: >
  Understanding the gNMIc Operator architecture
---

## Overview

The gNMIc Operator follows the Kubernetes operator pattern to manage gNMIc telemetry collectors. It watches Custom Resources and reconciles the actual state with the desired state.

## Components

This diagram illustrates how the gNMIc Operator orchestrates gNMIc deployments inside a Kubernetes cluster by reconciling Custom Resources into concrete Kubernetes primitives and gNMIc configurations.

At the core, the Cluster Controller watches a set of CRDs. 
It uses their desired state to create and manage resources like ConfigMaps, Secrets, Services, and a StatefulSet. 
The StatefulSet, together with the associated Services, materializes as multiple gNMIc pods (e.g. gnmic-0, gnmic-1, gnmic-2), each responsible for a subset of targets.

In parallel, the TargetSource Controller handles discovery use cases by watching TargetSource resources and creating concrete Target objects, which are then consumed by the Cluster Controller as part of the reconciliation flow.

<a href="">
  <img src="/images/architecture.svg" style="display:block; margin:auto; width: 900px; max-width: 100%; height: auto;">
</a>

## Cluster Controller

The Cluster Controller is the primary controller responsible for:

1. **Creating StatefulSets**: Deploys gNMIc pods with intial config (REST API, TLS certs,...)
2. **Managing Services**: Creates headless service for pod DNS and Prometheus services for metrics
3. **Building Configuration**: Aggregates all pipelines and builds the gNMIc pods configuration
4. **Distributing Targets**: Assigns targets to pods
5. **Applying Configuration**: Sends configuration to each pod via REST API

## Configuration Flow

Configuration flows from Custom Resources to gNMIc pods:

<a href="">
  <img src="/images/configuration_flow.svg" alt="Resource Model CRD Diagram" style="display:block; margin:auto;">
</a>

## Watches and Triggers

The Cluster Controller watches multiple resources to react to changes:

| Resource | Watch Type | Trigger Condition |
|----------|------------|-------------------|
| Cluster | Primary (For) | Spec changes |
| StatefulSet | Owned | Any Change |
| Service | Owned | Spec changes |
| Certificate| Owned | Any Change |
| Pipeline | Watch | Spec changes |
| Target | Watch | Spec or label changes |
| TunnelTargetPolicy | Watch | Spec or label changes |
| TargetProfile | Watch | Spec changes |
| Subscription | Watch | Spec or label changes |
| Output | Watch | Spec or label changes |
| Input | Watch | Spec or label changes |
| Processor | Watch | Spec or label changes |

Changes to any watched resource trigger Cluster reconciliation, ensuring configuration stays synchronized.
