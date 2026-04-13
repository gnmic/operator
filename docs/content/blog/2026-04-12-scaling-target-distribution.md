---
title: "Target Distribution and Horizontal Pod Autoscaling"
linkTitle: "Target Distribution and HPA"
date: 2026-04-12
description: >
  How target distribution and HPA integration turn a gNMIc cluster into an autoscaling telemetry fleet
author: Karim Radhouani
tags:
  - scaling
  - kubernetes
  - telemetry
  - architecture
---

Every telemetry deployment eventually hits the same question: how many collector pods do you need, and what happens when that number changes? A new datacenter comes online and 200 targets get added to the inventory. A firmware rollout restarts half the fabric and every device reconnects at once. The three gNMIc pods that handled the workload comfortably are now saturated, and someone has to bump a replica count and wait for redistribution to settle.

The gNMIc Operator was designed so you never have to do that. This post walks through the mechanics: how targets get distributed, why `podCapacity` exists, and how the Cluster CRD's scale subresource lets the Kubernetes Horizontal Pod Autoscaler (HPA) keep your telemetry fleet right-sized automatically.

## The distribution problem

A gNMIc cluster is a StatefulSet of collector pods. Each pod maintains long-lived gRPC streaming sessions to its assigned targets. The operator's job is to decide *which pod talks to which target*, and to do so in a way that survives changes (new targets, new pods, failed pods) without tearing down sessions that don't need to move.

A naive approach would be to re-hash every target whenever the cluster changes. That's simple, but catastrophic for stability: adding one pod could reassign half your targets, causing a wave of disconnects across the network. When you're collecting sub-second interface counters from hundreds of devices, that's a noticeable gap.

The operator uses **bounded load rendezvous hashing** instead. Each target is scored against every pod using a deterministic hash of the target name and pod index, and assigned to the highest-scoring pod that still has room. Because the hash is deterministic, the same target always prefers the same pod, so placement is stable across reconciliations without requiring any external state.

On top of the hashing, the operator applies an additional stability rule: **assignment preservation**. It reads each target's current pod from `Target.status.clusterStates[].pod` and keeps that placement as long as the pod still exists and is under capacity. Only targets without a valid current assignment go through the hashing step. This means scaling events only move the targets that *must* move:

<figure>
  <img src="/images/blog/target-redistribution.svg" alt="Target redistribution when scaling from 3 to 4 pods: only 3 out of 10 targets move" style="display:block; margin:auto; width:100%; max-width:960px; height:auto;">
  <figcaption style="text-align:center; font-size:0.85em; color:#999; margin-top:0.5em;">Scaling from 3 to 4 pods. New capacity is ceil(10/4) = 3. Pod 0 and Pod 2 each shed their lowest-scored target. Only 2 targets move. The other 8 keep their gRPC sessions alive.</figcaption>
</figure>

## Where podCapacity fits in

Without an explicit capacity, the algorithm computes one automatically: `ceil(totalTargets / numPods)`. That works for steady-state operation, and it even handles scale-up: when a pod is added, the recalculated capacity is lower, so over-capacity pods shed targets onto the new pod (see [Choosing a scaling strategy](#choosing-a-scaling-strategy) below). But the auto-calculated capacity has an important property: it always stretches to fit all current targets across the current pods. The operator will never leave a target unassigned. If 50 new targets appear at once and you have 3 pods, every one of those targets gets assigned immediately, even if the pods are already under heavy load. There is no admission ceiling, no overflow, and no `CapacityExhausted` condition. During a rolling update where pods restart one at a time, or when a burst of targets arrives faster than HPA can react, the remaining pods absorb everything without pushback.

That's what `spec.targetDistribution.podCapacity` solves. It sets a **hard ceiling** on how many targets the operator will assign to a single pod, regardless of how many targets exist:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: dc1
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
  targetDistribution:
    podCapacity: 100
```

This cluster can handle 300 targets across 3 pods. The 301st target won't be crammed onto an already-full pod, it stays unassigned. That's deliberate. The operator reports it in `status.unassignedTargets` and sets the `CapacityExhausted` condition:

```
NAME   IMAGE   REPLICAS   READY   PIPELINES   TARGETS   UNASSIGNED   SUBS   INPUTS   OUTPUTS
dc1    ...     3          3       2           310       10           5      2        3
```

```
Conditions:
  Type                 Status  Reason                 Message
  CapacityExhausted    True    InsufficientCapacity   10 target(s) could not be assigned, all pods at capacity
```

Those 10 unassigned targets are the pressure signal. They sit in a holding pattern until capacity appears, either through manual scaling or, better, through HPA adding a replica.

### Why a hard ceiling matters

The absence of a ceiling shows up in two common situations.

**Burst of targets.** A datacenter migration brings 150 new targets within minutes via a pipeline that selects by label. Without `podCapacity`, the auto-calculated capacity absorbs all of them across the existing 3 pods. Each pod goes from handling 80 targets to 130. Maybe that's fine, maybe the pods start swapping. There was never a signal that things were getting tight, because the operator assigned every target successfully.

**Rolling update.** You push a new gNMIc image. Kubernetes restarts pods one at a time. While a pod is down, its targets are unassigned, but the remaining 2 pods absorb them immediately because the auto-calculated capacity stretches. Each surviving pod temporarily handles 50% more targets than usual. If the pods were already near their resource limits, this can cause OOM kills or degraded collection during the rollout.

With `podCapacity: 100`, both scenarios behave differently. During a burst, the first 20 targets fill the remaining headroom and the next 130 stay unassigned until HPA adds pods. During a rolling update, the orphaned targets from the restarting pod remain unassigned if the surviving pods are already at capacity, rather than being force-loaded onto them. In both cases, the operator surfaces the overflow via `status.unassignedTargets` and the `CapacityExhausted` condition, giving you visibility and giving HPA a reason to act.

## How the CRD enables HPA

Making a custom resource work with the Kubernetes Horizontal Pod Autoscaler requires a specific contract. HPA needs to be able to read the current scale and write a new one. The Cluster CRD implements this via the **scale subresource**:

```go
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.readyReplicas,selectorpath=.status.selector
```

This single annotation tells Kubernetes three things:

1. **Where to read/write the desired replica count**: `.spec.replicas`
2. **Where to read the current ready count**: `.status.readyReplicas`
3. **How to select the pods**: `.status.selector`

With this in place, HPA can target the Cluster resource directly, scaling the *Cluster CR*, not the underlying StatefulSet. That distinction matters because the Cluster CR is the source of truth for replicas, target distribution, and configuration. If HPA scaled the StatefulSet directly, its replica count would drift from the Cluster CR's `spec.replicas`, and the operator would reconcile the StatefulSet back to what the CR says, undoing the autoscaler's work. Scaling the Cluster keeps the configuration, placement, and status lifecycle under a single owner.

### Scaling on target count

Within a single cluster, every target is subject to the same pipelines and subscriptions, so they carry roughly uniform compute pressure. That makes the number of active targets a meaningful scaling signal, since it directly reflects how many gRPC streaming sessions each pod is maintaining, and because the per-target workload is similar, the count correlates well with actual load.

This only holds when every target in the cluster carries the same subscriptions. The operator does support mixing different target types in a single cluster (pipelines can select different subsets of targets with different subscriptions). But when targets have different subscription profiles, one target collecting 20 high-frequency paths is not equivalent to one collecting a single low-frequency counter, and target count stops being a useful proxy for load. If you plan to use HPA with target-count scaling, keep each cluster homogeneous: all targets selected by the same set of subscriptions. Outputs can differ between pipelines without affecting this, it's the subscription mix that determines per-target cost. For heterogeneous workloads, split them across separate clusters, each with its own HPA and capacity settings tuned to the subscription profile.

Nothing stops you from combining target count with CPU and memory in the same HPA spec. HPA evaluates all configured metrics and picks the one that recommends the highest replica count. Target count gives you a leading indicator (the load *will* grow when targets arrive), while CPU and memory catch anything the target count alone doesn't capture.

gNMIc pods export `gnmic_target_up` per target. With [Prometheus Adapter](https://github.com/kubernetes-sigs/prometheus-adapter), you can aggregate this into a per-pod metric and point HPA at it:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: dc1-hpa
spec:
  scaleTargetRef:
    apiVersion: operator.gnmic.dev/v1alpha1
    kind: Cluster
    name: dc1
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Pods
      pods:
        metric:
          name: gnmic_targets_present
        target:
          type: AverageValue
          averageValue: "75"
```

This says: when the average number of active targets per pod exceeds 75, add a replica. Combined with `podCapacity: 100`, you get a 25-target buffer zone per pod, enough headroom for new pods to start before existing ones hit the wall.

<figure>
  <img src="/images/blog/hpa-capacity-graph.svg" alt="Graph showing targets growing over time, with total capacity and HPA threshold stepping up as pods are added" style="display:block; margin:auto; width:100%; max-width:960px; height:auto;">
  <figcaption style="text-align:center; font-size:0.85em; color:#999; margin-top:0.5em;">As targets grow, the average per pod crosses the HPA threshold (75), triggering a scale-up. Total capacity (pods x podCapacity) steps up, and the threshold rises with it. The gap between the threshold and capacity is the buffer zone.</figcaption>
</figure>

The sizing guidance is simple: set the HPA threshold to 70–80% of `podCapacity`. For bursty environments, go lower.

## Choosing a scaling strategy

The interaction between `podCapacity` and the HPA metric you choose matters more than it might seem. There are two modes, and they shouldn't be mixed.

### Target-count scaling

Set `spec.targetDistribution.podCapacity` to a fixed value. The operator never assigns more than that many targets to a single pod. When the total exceeds what the cluster can hold, overflow targets stay unassigned, HPA sees the per-pod average climbing, and adds replicas. This is the mode described in the sections above.

An additional benefit: because the scaling signal (`gnmic_targets_present`) is driven by the operator's own assignment decisions and not by runtime resource consumption, you can still manually scale the cluster up or down with `kubectl patch` or by editing `spec.replicas` even while HPA is active. HPA will adjust from the new baseline on the next evaluation cycle. With CPU/memory-based HPA, manual scaling is effectively overridden: HPA immediately reacts to the changed resource utilization and scales back to where it thinks the cluster should be.

### CPU/memory scaling

When `podCapacity` is not set, the operator computes capacity automatically as `ceil(totalTargets / numPods)`. This number changes every time the pod count changes. When HPA adds a pod based on CPU or memory pressure, the new (lower) capacity means some existing pods now hold more targets than the updated ceiling allows. The assignment preservation step displaces the lowest-scored targets from those pods, and the hashing step places them on the new pod. Redistribution happens naturally, without any overflow signal.

### Why mixing them doesn't work

If you set a fixed `podCapacity` *and* scale on CPU/memory, the capacity doesn't change when HPA adds a pod. Every existing target still has a valid assignment on a pod that is under the (unchanged) ceiling. Assignment preservation keeps them all in place. The new pod comes up empty, the overloaded pods stay overloaded, and HPA may keep adding pods that never receive any targets.

The rule is straightforward: if your HPA metric is target count, set `podCapacity`. If your HPA metric is CPU or memory, omit it and let the auto-calculated capacity handle redistribution.

## Operations scenarios

### Rolling update of the gNMIc image

You update `spec.image` on the Cluster CR. Kubernetes performs a rolling restart of the StatefulSet: one pod at a time goes down and comes back with the new image. Because each restarted pod retains its ordinal index and the prior assignment is still recorded in `Target.status`, the operator converges back to the same placement, no reshuffling. The blast radius of each restart is exactly the targets on that one pod.

### Full cluster restart

The StatefulSet is scaled to zero and back up, or the namespace is recreated. Every pod loses its sessions simultaneously. If prior assignments are still available in target status and the same pod ordinals come back, the operator feeds those assignments into the distribution algorithm and converges to the same placement instead of reshuffling arbitrarily. If the target set changed materially while the cluster was down (targets added or removed), only the delta goes through the hashing step.

### Scaling down under maintenance

You're decommissioning a datacenter and removing targets over the course of a week. As targets are deleted, the per-pod average drops and HPA eventually scales down. Targets that were on the removed pod are hashed against the surviving pods and placed on the highest-scoring pod with capacity. Targets on surviving pods stay put.

### Pod eviction or node failure

A node fails and a pod gets evicted. Kubernetes reschedules it with the same ordinal index (StatefulSet guarantee). The operator reconciles, finds the replacement pod ready, and pushes the same target assignments (the prior assignment in target status still points to that index). If the pod takes a long time to reschedule and `podCapacity` is set, the orphaned targets remain unassigned rather than being force-loaded onto the surviving pods. HPA can add a temporary extra replica if capacity gets tight during the rescheduling window.

## Putting it together

A cluster with capacity limits, resource requests for HPA CPU fallback, and an autoscaler targeting custom metrics:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: Cluster
metadata:
  name: dc1
  namespace: telemetry
spec:
  replicas: 3
  image: ghcr.io/openconfig/gnmic:latest
  targetDistribution:
    podCapacity: 100
  resources:
    requests:
      memory: "256Mi"
      cpu: "200m"
    limits:
      memory: "1Gi"
      cpu: "2"
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: dc1-hpa
  namespace: telemetry
spec:
  scaleTargetRef:
    apiVersion: operator.gnmic.dev/v1alpha1
    kind: Cluster
    name: dc1
  minReplicas: 2
  maxReplicas: 15
  metrics:
    - type: Pods
      pods:
        metric:
          name: gnmic_targets_present
        target:
          type: AverageValue
          averageValue: "75"
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 300
```

The `stabilizationWindowSeconds` on scale-down prevents flapping: when targets are being removed gradually (decommissioning, maintenance windows), the cluster doesn't scale down and back up repeatedly.

The pipeline and target setup are independent. Teams managing inventory and telemetry flows don't need to know or care about the scaling configuration. They create targets with labels, pipelines select them, and the operator handles placement and capacity.

## Summary

The autoscaling story is built on three pillars:

1. **Bounded load rendezvous hashing** provides deterministic, even placement. Assignment preservation on top of the hashing reduces churn further, targets only move when they must.

2. **`podCapacity`** creates a hard admission ceiling per pod. Without it, the operator always assigns every target, even during bursts or rolling updates when pods are already under pressure. With it, the operator refuses to overpack pods and leaves excess targets unassigned, surfacing the overflow via `status.unassignedTargets` and the `CapacityExhausted` condition.

3. **The scale subresource** on the Cluster CRD lets HPA target the Cluster directly. HPA adjusts `spec.replicas`, the operator handles the rest (distribution, configuration push, status reporting).

`podCapacity` does not magically make HPA smarter. It creates a hard ceiling and a clean control boundary. HPA still needs a good metric strategy, and overflow-based scaling works best when paired with enough headroom between the HPA threshold and the capacity limit to give new pods time to start. A future enhancement will expose a cluster-level overflow metric (such as `unassignedTargets`) directly as an HPA-consumable signal, closing the loop between the operator's capacity model and the autoscaler.

Together, these pieces turn a manually-sized collector fleet into one that grows and shrinks with the network. The [Scaling](/docs/advanced/scaling/) documentation covers threshold sizing, Prometheus Adapter configuration, and monitoring in detail.
