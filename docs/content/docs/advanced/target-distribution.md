---
title: "Target Distribution"
linkTitle: "Target Distribution"
weight: 1
description: >
  How targets are distributed across pods
---

The gNMIc Operator uses a simple algorithm to distribute targets across pods. 
More placement/distribution strategies will be implemented in the future.

This page explains the algorithm and its properties.

## Algorithm: Bounded Load Rendezvous Hashing

The operator uses **bounded load rendezvous hashing**, which combines two techniques:

1. **Rendezvous hashing**: For stability (targets stay on same pod)
2. **Bounded load**: For even distribution (no pod is overloaded)

## How It Works

### Step 1: Determine Capacity

If the Cluster CR specifies `spec.targetDistribution.perPodCapacity`, that value
is used as a fixed ceiling. Otherwise capacity is calculated automatically:

```
capacity = ceil(numTargets / numPods)
```

Example: 10 targets, 3 pods → capacity = 4

A fixed `perPodCapacity` is useful when combined with [autoscaling](../scaling/)
— it sets a hard ceiling per pod so HPA has time to add replicas before pods are
full.

### Step 2: Preserve Current Assignments

If target status already records which pod each target is on (the **current
assignment**), the algorithm keeps those assignments as long as the pod is still
present and under capacity.

When a pod has more pre-assigned targets than its capacity allows (e.g., after a
scale-down or capacity reduction), targets with the **lowest** hash score for
that pod are displaced first. This ensures deterministic selection of which
targets stay.

### Step 3: Sort Remaining Targets

Unassigned targets are processed in alphabetical order for determinism:

```
[target1, target10, target2, target3, ...]
```

### Step 4: Assign Each Target

For each unassigned target:
1. Calculate a score against each pod: `hash(targetName + podIndex)`
2. Sort pods by score (highest first)
3. Assign to highest-scoring pod that still has capacity

If no pod has capacity, the target is left unassigned until the next
reconciliation (e.g., after HPA scales up a new replica). The Cluster CR status
reports the number of unassigned targets via the `unassignedTargets` field and
the `CapacityExhausted` condition.

### Step 5: Track Load

After each assignment, increment the pod's load count. When a pod reaches capacity, it's skipped for future assignments.

## Properties

### Stability

The same target gets the same score for each pod across reconciliations. Unless
capacity constraints force a change, targets stay on their assigned pods.

When current assignments are available, targets are kept on their existing pod
without recomputing scores — only truly unassigned targets go through the
hashing step.

```
# Before scaling: router1 on pod0
hash("router1" + "0") = 892341  ← highest

# After adding pod3: router1 stays on pod0
hash("router1" + "0") = 892341  ← still highest among pods with capacity
```

### Even Distribution

With capacity = ceil(n/p), no pod can have more than `capacity` targets:

| Targets | Pods | Capacity | Distribution |
|---------|------|----------|--------------|
| 10 | 3 | 4 | 4, 3, 3 |
| 100 | 7 | 15 | 15, 14, 14, 14, 14, 15, 14 |
| 5 | 3 | 2 | 2, 1, 2 |

### Minimal Redistribution

Current assignment awareness keeps churn to the minimum required:

**Adding a pod**: Existing targets stay on their current pods. Only unassigned
targets (or targets displaced by capacity limits) may land on the new pod.

**Removing a pod**: Only targets on the removed pod redistribute. Targets on
remaining pods stay put.

**Adding/removing a target**: Other targets' assignments are unaffected when
current assignments are provided.

## Visualization

```
                          Before (3 pods)
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│    Pod 0    │    │    Pod 1    │    │    Pod 2    │
│             │    │             │    │             │
│  router1    │    │  router2    │    │  router4    │
│  router5    │    │  router3    │    │  router6    │
│  router7    │    │             │    │  router8    │
│  router9    │    │             │    │  router10   │
└─────────────┘    └─────────────┘    └─────────────┘
     (4)                (2)                (4)

                         After (4 pods)
┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│    Pod 0    │    │    Pod 1    │    │    Pod 2    │    │    Pod 3    │
│             │    │             │    │             │    │             │
│  router1    │    │  router2    │    │  router4    │    │  router5 ◄─┤ moved
│  router7    │    │  router3    │    │  router8    │    │  router9 ◄─┤ moved
│  router10   │    │             │    │             │    │  router6 ◄─┤ moved
└─────────────┘    └─────────────┘    └─────────────┘    └─────────────┘
     (3)                (2)                (2)                (3)

Targets moved: 3 out of 10 (30%)
```

## Comparison with Other Approaches

| Algorithm | Stability | Even Distribution | Complexity |
|-----------|-----------|-------------------|------------|
| Round-robin | None | Perfect | Low |
| Modulo hash | Low | Good | Low |
| Consistent hash | High | Variable | Medium |
| Rendezvous hash | High | Variable | Medium |
| **Bounded load rendezvous** | **Good** | **Good** | **Medium** |

