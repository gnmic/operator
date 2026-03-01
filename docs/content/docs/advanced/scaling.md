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

1. Kubernetes creates new pods (`gnmic-3`, `gnmic-4`)
2. Operator waits for pods to be ready
3. Operator redistributes targets using bounded load rendezvous hashing
4. Some targets move from existing pods to new pods
5. Configuration is applied to all pods

### Scale Down ( 5 → 3 pods)

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

The operator's Cluster resource supports the `scale` subresource, allowing you to enable automatic scaling using the Horizontal Pod Autoscaler (HPA).

To set up autoscaling, create an HPA resource that targets the Cluster resource. Specify the desired minimum and maximum number of replicas, as well as the metrics that will determine when scaling occurs:

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

> **Note:** You must install the Kubernetes metrics server to enable HPA based on CPU or Memory:
>
> ```shell
> kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
> ```

### Autoscaling based on custom resources

gNMIc pods provide various Prometheus metrics that can be leveraged by an HPA resource for autoscaling.

One common use case is to scale based on the number of targets assigned to each Pod.
The gNMIc pods export metrics like:

```
gnmic_target_up{name="default/leaf1"} 0
gnmic_target_up{name="default/leaf2"} 0
gnmic_target_up{name="default/spine1"} 1
```

Here, a value of `1` indicates that the target is present, while `0` denotes it is absent.

With [Prometheus Adapter](https://github.com/kubernetes-sigs/prometheus-adapter), this metric can be made available as `targets_per_pod{cluster="c1", pod="gnmic-c1-0"}` = 1.
You can use the following promQL to aggregate these into a “targets per pod” metric: `sum(gnmic_target_up == 1) by (namespace, pod)`.

> You can assign `namespace` and `pod` labels to metrics using scrape configurations or relabeling.

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

The corresponding HPA resource would look like this:

In other words: Scale **Cluster** `c1` to a max of `10` replicas if the average number of targets present in the current pods is above `30`.

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
          averageValue: "30"
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

