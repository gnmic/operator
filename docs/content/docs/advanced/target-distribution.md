---
title: "Target Distribution"
linkTitle: "Target Distribution"
weight: 2
description: >
  How targets are distributed across pods
---

The gNMIc Operator uses a sophisticated algorithm to distribute targets across pods. This page explains the algorithm and its properties.

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

```
Target: "router1"
Scores: pod0=892341, pod1=234567, pod2=567890
Order:  pod0, pod2, pod1
pod0 has capacity → assign to pod0
```

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

**Adding a pod**: Only targets that score highest for the new pod AND are on an over-capacity pod will move. Typically ~1/(N+1) targets move.

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

## Implementation Details

The distribution logic is in `internal/gnmic/distribute.go`:

```go
func DistributeTargets(plan *ApplyPlan, podIndex, numPods int) *ApplyPlan {
    // Get assignments using bounded rendezvous hashing
    assignments := boundedRendezvousAssign(plan.Targets, numPods)
    
    // Filter to only targets for this pod
    for targetNN, assignedPod := range assignments {
        if assignedPod == podIndex {
            distributed.Targets[targetNN] = plan.Targets[targetNN]
        }
    }
    return distributed
}
```

## Debugging Distribution

To see how targets are distributed:

```bash
# Check targets per pod
for i in 0 1 2; do
  echo "Pod $i:"
  kubectl exec gnmic-my-cluster-$i -- curl -s localhost:7890/api/v1/config/targets | jq 'keys'
done
```

Or check the operator logs:

```bash
kubectl logs -n gnmic-operator-system deployment/gnmic-operator-controller-manager | grep "config applied"
```

Output shows target count per pod:

```
config applied to pod  pod=0  targets=34
config applied to pod  pod=1  targets=33
config applied to pod  pod=2  targets=33
```

