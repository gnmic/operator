---
title: "Scaling"
linkTitle: "Scaling"
weight: 2
description: >
  Scaling gNMIc clusters horizontally
---

The gNMIc Operator supports horizontal scaling of collector clusters. This page explains how scaling works and best practices for production deployments.

## Scaling a Cluster

To scale a cluster, update the `replicas` field:

```bash
# Scale to 5 replicas
kubectl patch cluster my-cluster --type merge -p '{"spec":{"replicas":5}}'
```

Or edit the Cluster resource:

```yaml
spec:
  replicas: 5  # Changed from 3
```

## What Happens When You Scale

### Scale Up ( 3 → 5 pods)

1. Kubernetes creates new pods (`gnmic-3`, `gnmic-4`).
2. Operator waits for pods to be ready.
3. Operator recomputes the distribution plan. Existing target assignments are
   preserved — only unassigned targets or targets displaced by capacity limits
   are placed on the new pods.
4. Configuration is applied to all pods.

### Scale Down ( 5 → 3 pods)

1. Operator recomputes the distribution plan for the reduced replica count.
   Targets from removed pods flow through rendezvous hashing onto surviving
   pods, bounded by each pod's capacity.
2. Configuration is applied to remaining pods.
3. Kubernetes terminates pods (`gnmic-4`, `gnmic-3`).

## Target Redistribution

The operator uses **bounded load rendezvous hashing** to distribute targets.
See [Target Distribution](../target-distribution/) for a detailed explanation
of the algorithm.

Key properties:

- **Stable**: Targets stay on their current pod unless forced to move.
- **Even**: No pod exceeds its capacity.
- **Current-assignment aware**: The operator reads each target's current pod
  from its status and feeds this as input to the algorithm, minimizing churn.

### Example Distribution

```
# 10 targets, 3 pods
Pod 0: [target1, target5, target8]      (3 targets)
Pod 1: [target2, target4, target9]      (3 targets)
Pod 2: [target3, target6, target7, target10] (4 targets)

# After scaling to 4 pods — existing assignments are preserved
Pod 0: [target1, target5, target8]      (3 targets) - unchanged
Pod 1: [target2, target4]               (2 targets) - target9 moved to new pod
Pod 2: [target3, target7, target10]     (3 targets) - target6 moved to new pod
Pod 3: [target6, target9]               (2 targets) - new pod
```

## Best Practices

### Start with Appropriate Size

Estimate based on:
- Number of targets
- Subscription frequency
- Data volume per target
- Number of outputs

### Use Resource Limits

Ensure clusters (pods) have appropriate resources:

```yaml
spec:
  resources:
    requests:
      memory: "256Mi"
      cpu: "200m"
    limits:
      memory: "1Gi"
      cpu: "2"
```

### Monitor Before Scaling

Check metrics before scaling:

```promql
# CPU usage per pod
rate(container_cpu_usage_seconds_total{pod=~"gnmic-.*"}[5m])

# Memory usage per pod
container_memory_usage_bytes{pod=~"gnmic-.*"}

# Targets per pod (from gNMIc metrics)
gnmic_target_status{cluster="my-cluster"}
```

## Horizontal Pod Autoscaler

The operator's Cluster resource supports the `scale` subresource, allowing you
to use the Horizontal Pod Autoscaler (HPA) for automatic scaling.

> HPA scales the **Cluster CR**, not the StatefulSet directly. This ensures the
> operator remains in control of target redistribution and configuration rollout.

### Scaling based on target count (recommended)

Target count is the most accurate scaling signal for gNMIc — CPU/memory don't
reliably reflect the load from long-lived gRPC streaming connections.

gNMIc pods export per-target metrics:

```
gnmic_target_up{name="default/leaf1"} 0
gnmic_target_up{name="default/leaf2"} 0
gnmic_target_up{name="default/spine1"} 1
```

A value of `1` indicates that the target is present, `0` denotes it is absent.

With [Prometheus Adapter](https://github.com/kubernetes-sigs/prometheus-adapter),
aggregate these into a per-pod metric:

```promql
sum(gnmic_target_up{namespace!="",pod!=""} == 1) by (namespace, pod)
```

> You can assign `namespace` and `pod` labels to metrics using scrape
> configurations or relabeling.

Example Prometheus Adapter rule:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-adapter-rules
  namespace: monitoring
data:
  config.yaml: |
    rules:
      default: false
      custom:
        - seriesQuery: 'gnmic_target_up{namespace!="",pod!=""}'
          resources:
            overrides:
              namespace:
                resource: namespace
              pod:
                resource: pod
          name:
            matches: "^gnmic_target_up$"
            as: "gnmic_targets_present"
          metricsQuery: |
            sum(gnmic_target_up{<<.LabelMatchers>>} == 1) by (namespace, pod)
```

The corresponding HPA resource — scale Cluster `c1` up when the average number
of targets per pod exceeds 75:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: gnmic-c1-hpa
spec:
  scaleTargetRef:
    apiVersion: operator.gnmic.dev/v1alpha1
    kind: Cluster
    name: c1
  minReplicas: 1
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

### Threshold vs Capacity

When using HPA, the Cluster CR's `spec.targetDistribution.perPodCapacity` acts
as a hard assignment ceiling — the operator never assigns more than
`perPodCapacity` targets to a single pod. The HPA **averageValue** (the scaling
threshold) should be set **lower** than capacity to create a buffer zone that
gives new pods time to start:

```
0 ─────── HPA threshold ─────── Capacity
           (scale trigger)       (assignment stops)
```

1. When the average target count crosses the HPA threshold, HPA increases
   `.spec.replicas`.
2. While the new pod is starting, existing pods continue receiving targets up
   to `capacity`.
3. If all pods reach `capacity` before the new pod is ready, overflow targets
   remain unassigned until the next reconciliation. The Cluster status reports
   the count via `status.unassignedTargets` and the `CapacityExhausted`
   condition.

**Sizing guidance** — set the HPA threshold to ~70–80% of capacity:

| Cluster Capacity | HPA averageValue | Headroom per pod |
|---|---|---|
| 50 | 35 | 15 (30%) |
| 100 | 75 | 25 (25%) |
| 200 | 150 | 50 (25%) |

For bursty workloads (e.g., many targets appearing at once via
`TunnelTargetPolicy`), use a wider buffer (lower threshold-to-capacity ratio).

### Monitoring Capacity

When targets exceed the total cluster capacity, the Cluster status makes this
visible:

```bash
kubectl get clusters
```

```
NAME   IMAGE   REPLICAS   READY   PIPELINES   TARGETS   UNASSIGNED   SUBS   INPUTS   OUTPUTS
c1     ...     3          3       2           100       4            5      2        3
```

The `CapacityExhausted` condition provides detail:

```bash
kubectl describe cluster c1
```

```
Conditions:
  Type                 Status  Reason                 Message
  CapacityExhausted    True    InsufficientCapacity   4 targets could not be assigned, all pods at capacity
```

Once HPA scales up and all targets are assigned, the condition clears
automatically.

### Scaling based on CPU/Memory

You can also use resource-based metrics:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: gnmic-c1-hpa
spec:
  scaleTargetRef:
    apiVersion: operator.gnmic.dev/v1alpha1
    kind: Cluster
    name: c1
  minReplicas: 1
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
```

> **Note:** You must install the Kubernetes metrics server for CPU/Memory-based HPA:
>
> ```shell
> kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
> ```

Target-count-based scaling is recommended over CPU/Memory because gRPC
streaming connections don't always correlate with CPU utilization.

## Considerations

### Output Connections

All pods connect to all outputs. For outputs like Kafka or Prometheus:
- Each pod writes to the same destination
- Data is naturally partitioned by target
- No deduplication needed

### Stateless Operation

gNMIc pods are stateless by design:
- No persistent volumes required
- Configuration comes from operator via REST API
- Targets can move between pods without data loss
