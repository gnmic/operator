---
title: "Scaling"
linkTitle: "Scaling"
weight: 1
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

### Scale Up (e.g., 3 → 5 pods)

1. Kubernetes creates new pods (`gnmic-3`, `gnmic-4`)
2. Operator waits for pods to be ready
3. Operator redistributes targets using bounded load rendezvous hashing
4. Some targets move from existing pods to new pods
5. Configuration is applied to all pods

### Scale Down (e.g., 5 → 3 pods)

1. Operator redistributes targets away from pods being removed
2. Configuration is applied to remaining pods
3. Kubernetes terminates pods (`gnmic-4`, `gnmic-3`)
4. Targets from terminated pods are handled by remaining pods

## Target Redistribution

The operator uses **bounded load rendezvous hashing** to distribute targets:

- **Stable**: Same target tends to stay on same pod
- **Even**: Targets are distributed evenly (within 1-2 of each other)

### Example Distribution

```
# 10 targets, 3 pods
Pod 0: [target1, target5, target8]      (3 targets)
Pod 1: [target2, target4, target9]      (3 targets)
Pod 2: [target3, target6, target7, target10] (4 targets)

# After scaling to 4 pods
Pod 0: [target1, target5, target8]      (3 targets) - unchanged
Pod 1: [target2, target4]               (2 targets) - lost target9
Pod 2: [target3, target7, target10]     (3 targets) - lost target6
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

Ensure pods have appropriate resources:

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

### Scale Gradually

For large changes, scale gradually:

```bash
# Instead of 3 → 10
kubectl patch cluster my-cluster -p '{"spec":{"replicas":5}}'
# Wait for stabilization
kubectl patch cluster my-cluster -p '{"spec":{"replicas":7}}'
# Wait for stabilization
kubectl patch cluster my-cluster -p '{"spec":{"replicas":10}}'
```

## Horizontal Pod Autoscaler (Comming Soon)

You can use HPA for automatic scaling:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: gnmic-cluster-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: gnmic-my-cluster
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
```

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

