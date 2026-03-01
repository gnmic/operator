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

### Step 1: Calculate Capacity

```
capacity = ceil(numTargets / numPods)
```

Example: 10 targets, 3 pods → capacity = 4

### Step 2: Sort Targets

Targets are processed in alphabetical order for determinism:

```
[target1, target10, target2, target3, ...]
```

### Step 3: Assign Each Target

For each target:
1. Calculate a score against each pod: `hash(targetName + podIndex)`
2. Sort pods by score (highest first)
3. Assign to highest-scoring pod that has capacity

### Step 4: Track Load

After each assignment, increment the pod's load count. When a pod reaches capacity, it's skipped for future assignments.

## Properties

### Stability

The same target gets the same score for each pod across reconciliations. Unless capacity constraints force a change, targets stay on their assigned pods.

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

When scaling:

**Adding a pod**: Only targets that score highest for the new pod will move.

**Removing a pod**: Only targets on the removed pod redistribute. Targets on remaining pods stay put.

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

