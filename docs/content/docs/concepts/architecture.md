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

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Kubernetes Cluster                              │
│                                                                         │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │                    gNMIc Operator                                 │  │
│  │                                                                   │  │
│  │  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │  │
│  │  │ Cluster         │  │ Pipeline        │  │ Other           │  │  │
│  │  │ Controller      │  │ Controller      │  │ Controllers     │  │  │
│  │  └────────┬────────┘  └────────┬────────┘  └─────────────────┘  │  │
│  └───────────┼────────────────────┼─────────────────────────────────┘  │
│              │                    │                                     │
│              ▼                    ▼                                     │
│  ┌───────────────────────────────────────────────────────────────────┐ │
│  │                    Custom Resources                                │ │
│  │  ┌─────────┐ ┌──────────┐ ┌────────┐ ┌──────────────┐ ┌────────┐ │ │
│  │  │ Cluster │ │ Pipeline │ │ Target │ │ Subscription │ │ Output │ │ │
│  │  └─────────┘ └──────────┘ └────────┘ └──────────────┘ └────────┘ │ │
│  └───────────────────────────────────────────────────────────────────┘ │
│              │                                                          │
│              ▼                                                          │
│  ┌───────────────────────────────────────────────────────────────────┐ │
│  │                    Managed Resources                               │ │
│  │  ┌─────────────┐ ┌─────────────────┐ ┌───────────┐ ┌───────────┐ │ │
│  │  │ StatefulSet │ │ Headless Service│ │ ConfigMap │ │ Services  │ │ │
│  │  └─────────────┘ └─────────────────┘ └───────────┘ └───────────┘ │ │
│  └───────────────────────────────────────────────────────────────────┘ │
│              │                                                          │
│              ▼                                                          │
│  ┌───────────────────────────────────────────────────────────────────┐ │
│  │                       gNMIc Pods                                   │ │
│  │  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐                  │ │
│  │  │ gnmic-0     │ │ gnmic-1     │ │ gnmic-2     │                  │ │
│  │  │ (targets    │ │ (targets    │ │ (targets    │                  │ │
│  │  │  A, B, C)   │ │  D, E, F)   │ │  G, H, I)   │                  │ │
│  │  └─────────────┘ └─────────────┘ └─────────────┘                  │ │
│  └───────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────┘
```

## Cluster Controller

The Cluster Controller is the primary controller responsible for:

1. **Creating StatefulSets**: Deploys gNMIc pods with stable network identities
2. **Managing Services**: Creates headless service for pod DNS and Prometheus services for metrics
3. **Building Configuration**: Aggregates all pipelines and builds the gNMIc configuration
4. **Distributing Targets**: Assigns targets to pods using bounded load rendezvous hashing
5. **Applying Configuration**: Sends configuration to each pod via REST API

### Reconciliation Flow

```
Cluster CR Changed
       │
       ▼
┌──────────────────┐
│ Reconcile Starts │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐     ┌──────────────────┐
│ Handle Deletion? │──▶  │ Cleanup Resources│
└────────┬─────────┘     └──────────────────┘
         │ No
         ▼
┌──────────────────┐
│ Ensure Finalizer │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Reconcile        │
│ Headless Service │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Reconcile        │
│ StatefulSet      │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ List Pipelines   │
│ for Cluster      │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Resolve Targets, │
│ Subscriptions,   │
│ Outputs, Inputs  │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Build Apply Plan │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Reconcile        │
│ Prometheus Svcs  │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Wait for Pods    │
│ to be Ready      │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Apply Config to  │
│ Each Pod         │
└──────────────────┘
```

## Why StatefulSet?

The operator uses StatefulSets instead of Deployments for several reasons:

| Feature | StatefulSet | Deployment |
|---------|-------------|------------|
| Pod naming | Predictable (`gnmic-0`, `gnmic-1`) | Random (`gnmic-xyz123`) |
| Pod DNS | Individual DNS records | No individual DNS |
| Scaling | Ordered (add/remove from end) | Random |
| Identity | Stable across restarts | Changes on restart |

Stable pod identities enable:
- **Direct communication**: Operator can send config to specific pods
- **Deterministic distribution**: Same target goes to same pod index
- **Ordered scaling**: Predictable behavior when scaling up/down

## Configuration Flow

Configuration flows from Custom Resources to gNMIc pods:

```
┌─────────┐  ┌──────────┐  ┌────────────┐  ┌────────┐
│ Targets │  │ Subs     │  │ Outputs    │  │ Inputs │
└────┬────┘  └────┬─────┘  └─────┬──────┘  └───┬────┘
     │            │              │             │
     └────────────┴──────────────┴─────────────┘
                        │
                        ▼
               ┌────────────────┐
               │    Pipeline    │
               │  (references)  │
               └───────┬────────┘
                       │
                       ▼
               ┌────────────────┐
               │ Plan Builder   │
               │ (aggregation)  │
               └───────┬────────┘
                       │
                       ▼
               ┌────────────────┐
               │ Target         │
               │ Distribution   │
               └───────┬────────┘
                       │
          ┌────────────┼────────────┐
          ▼            ▼            ▼
     ┌─────────┐  ┌─────────┐  ┌─────────┐
     │ Pod 0   │  │ Pod 1   │  │ Pod 2   │
     │ REST API│  │ REST API│  │ REST API│
     └─────────┘  └─────────┘  └─────────┘
```

## Watches and Triggers

The Cluster Controller watches multiple resources to react to changes:

| Resource | Watch Type | Trigger Condition |
|----------|------------|-------------------|
| Cluster | Primary (For) | Spec changes |
| StatefulSet | Owned | Any Change |
| Service | Owned | Spec changes |
| Pipeline | Watch | Spec changes |
| Target | Watch | Spec changes |
| TargetProfile | Watch | Spec changes |
| Subscription | Watch | Spec changes|
| Output | Watch | Spec changes |
| Input | Watch | Spec changes |
| Processor | Watch | Spec changes |

Changes to any watched resource trigger Cluster reconciliation, ensuring configuration stays synchronized.

