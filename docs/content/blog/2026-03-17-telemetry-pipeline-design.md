---
title: "Designing Telemetry Pipelines"
linkTitle: "Designing Telemetry Pipelines"
date: 2026-03-17
description: >
  The idea behind Cluster and Pipeline: two resources, two concerns, one efficient runtime
author: Karim Radhouani
tags:
  - architecture
  - kubernetes
  - telemetry
  - networking
---

Managing gNMI telemetry at scale is not just a configuration problem. It's an ownership problem. Subscriptions change often. Targets come and go. Different teams care about different slices of the network. And when your collector fleet spans multiple pods serving hundreds of devices, the last thing you want is a design where updating a Kafka output triggers a rolling restart, or where scaling from 3 to 5 pods means touching your telemetry wiring.

That's the design problem the gNMIc Operator resource model is built around. The two resources at the center of it are `Cluster` and `Pipeline`. The split reflects a conviction that good API design match the way a system is actually owned and operated, not just how it is configured. Network telemetry has real team boundaries. Changes happen at different rates and carry different risk profiles.

## Two resources, two concerns

`Cluster` answers: *where and how do collectors run?*

`Pipeline` answers: *what telemetry flows should exist, and where does the data go?*

<figure>
  <img src="/images/blog/cluster-pipeline-layers.svg" alt="Cluster and Pipeline resource layers: intent on the left, infrastructure on the right" style="display:block; margin:auto; width:100%; max-width:960px; height:auto;">
  <figcaption style="text-align:center; font-size:0.85em; color:#999; margin-top:0.5em;">Pipelines live in the intent layer and reference a Cluster. The Cluster owns everything that runs in Kubernetes.</figcaption>
</figure>

Keeping those questions separate is not just organizational tidiness, it's what lets the operator manage them differently. The `Cluster` controller watches all pipeline-related resources (`Pipeline`, `Target`, `Subscription`, `Output`, `Input`, `Processor`, `TargetProfile`, `TunnelTargetPolicy`) and reconciles their combined state into one live runtime plan per collector fleet. Pipelines are not isolated deployments. They are composable inputs to a shared runtime.

## The Cluster: a fleet with a policy

A `Cluster` is the operator's runtime wrapper for a group of gNMIc pods. It materializes as a StatefulSet, backing Services and certificates. The spec handles questions that belong at the fleet level: how many replicas, which image, what CPU and memory, whether to expose a gNMI server or gRPC tunnel endpoint, what TLS certificates to use for device connections, how many targets each pod can hold.

Those are not decisions that change when someone adds a new subscription. They're infrastructure decisions with implications for scheduling, certificate rotation, and failure domains. Keeping them in `Cluster` means the platform team can own and evolve them without needing to coordinate with whoever manages the telemetry flows running on top.

The implementation reinforces this separation. Client TLS certificates for pod-to-device authentication are issued as a single cluster-level certificate shared across all pods (not one per pod) specifically so that scaling the cluster doesn't trigger unnecessary volume updates on the pods that are already running.

## The Pipeline: intent, not deployment

A `Pipeline` is where network teams express what they want to collect and where they want it to go. It points to exactly one `Cluster`, and selects targets, subscriptions, outputs, inputs, and processors either by name or by label selector.

The key distinction is that a `Pipeline` is not a deployment object. Creating one doesn't spin up more collectors. It registers intent with the cluster controller, which incorporates it into the fleet's runtime plan on the next reconcile cycle.

This matters for two reasons. First, multiple teams can each manage their own pipelines on the same collector fleet without stepping on each other. The platform team doesn't need to be in the loop when the observability team adds a new EVPN subscription. Second, the controller sees all pipelines together and produces an efficient aggregate plan, rather than treating each pipeline independently.

The label selector model is what makes this practical at scale. Instead of maintaining explicit device lists, a pipeline can say "all spine devices in dc1" or "all subscriptions tagged as platform metrics", and new devices or subscriptions are picked up automatically when the labels match.

## Team ownership in practice

In most networking organizations, the resource split follows team boundaries fairly naturally:

| Resource | Typical owner |
|----------|---------------|
| `Cluster` | Platform / SRE |
| `Pipeline` | Network observability |
| `Target`, `TargetProfile` | Network inventory |
| `Subscription`, `Output`, `Input`, `Processor` | Telemetry / domain teams |

Scaling a collector cluster is a platform decision: update the `Cluster`. Adding a Kafka export for EVPN data is a telemetry decision: add a `Pipeline`. Onboarding new leaf switches is an inventory decision: create `Target` resources with the right labels, and the right pipelines pick them up. Nobody has to hand-edit a shared config file and hope they didn't break someone else's flow.

## One fleet, one plan

This is where the model earns its keep at runtime.

Because every enabled pipeline feeds into the same planning pass, the operator builds one aggregate configuration for the collector fleet rather than N independent ones. When two pipelines reference the same target, the operator emits one target entry and references the subscriptions from both pipelines to it, effectively using one gRPC connection to the device, not two. Shared subscriptions on the same target are similarly deduplicated.

Outputs work the other way: if the same `Output` resource appears in two different pipelines, the operator creates two distinct output instances in the generated configuration, one per pipeline. That preserves processor chains and routing behavior that are specific to each pipeline. The model merges where sharing is safe and keeps things separate where behavior needs to remain distinct.

<figure>
  <img src="/images/blog/pipeline-merge-separate.svg" alt="Shared targets deduplicated, outputs kept pipeline-specific" style="display:block; margin:auto; width:100%; max-width:960px; height:auto;">
  <figcaption style="text-align:center; font-size:0.85em; color:#999; margin-top:0.5em;">Both pipelines reference leaf-1. The operator opens one gRPC session to the device and routes data to the pipeline-specific outputs.</figcaption>
</figure>

When anything in the watch list changes (a new subscription, a modified output, a freshly labeled target) the controller rebuilds the plan and pushes it to each pod through the gNMIc REST apply endpoint. No pod restart, no session teardown. The pods pick up the new configuration and continue running.

## Stable target placement under scaling

Distributing targets across pods looks simple until you scale.

A naive rebalancing algorithm moves too many targets when you add or remove pods. That means unnecessary reconnects, connection churn on devices, and telemetry gaps right when your infrastructure is changing. The operator avoids this with bounded-load rendezvous hashing: each target is deterministically assigned to the highest-scoring pod that still has capacity, and existing assignments are preserved across reconcile cycles. When the cluster scales up, only targets that have no valid current assignment are moved. When a per-pod capacity limit is configured and the cluster is full, overflow targets are left unassigned and surfaced in `Cluster.status` rather than silently overloading pods.

## Putting it together: dc1

Here is a concrete example. The platform team provisions one cluster for `dc1`. The observability team creates two pipelines: one for interface counters from all devices to Prometheus, one for EVPN and BGP data from leaf devices to Kafka.

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: dc1
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
  targetDistribution:
    podCapacity: 200
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: dc1-common
spec:
  clusterRef: dc1
  enabled: true
  targetSelectors:
    - matchLabels:
        location: dc1
  subscriptionSelectors:
    - matchLabels:
        type: interfaces
  outputs:
    outputSelectors:
      - matchLabels:
          output-type: prometheus
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: Pipeline
metadata:
  name: dc1-leaf
spec:
  clusterRef: dc1
  enabled: true
  targetSelectors:
    - matchLabels:
        location: dc1
        role: leaf
  subscriptionSelectors:
    - matchLabels:
        type: bgp-evpn
  outputs:
    outputSelectors:
      - matchLabels:
          output-type: kafka
          topic: leaf-telemetry
```

Leaf devices appear in both pipelines. The operator merges them into single target entries with subscriptions from both, opens one gRPC connection per device, and routes data to the right outputs. The two teams manage their pipelines independently; the platform team manages the cluster. The operator handles the aggregation.

The [Architecture](/docs/concepts/architecture/) and [Resource Model](/docs/concepts/resource-model/) pages go deeper on the specifics. If you want to see it running, the [Quick Start](/docs/getting-started/quick-start/) gets you there quickly.
