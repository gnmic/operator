---
title: "Operator Resources"
linkTitle: "Operator Resources"
weight: 3
description: >
  Sizing the operator pod and tuning its Kubernetes API usage at scale
---

## Overview

[Scaling](../scaling/) covers sizing the gNMIc collector pods. This page is about the
**operator pod itself** — how much memory it needs, why that is not driven by target count
alone, and the two settings that matter most as a deployment grows.

The short version: the operator's memory is dominated by what it *caches*, not by how many
targets it manages. Restricting the cache with `--watch-namespaces` is the single largest
reduction available.

## Why the operator caches so much

Like most controllers, the operator reads through a shared informer cache rather than
hitting the API server on every lookup. An informer is started the first time any code path
reads a type, and by default each one caches **every object of that type in the cluster**.

The operator ends up caching six non-CRD types alongside its own CRDs:

| Type | Why |
|---|---|
| `Secret` | Target credentials, cert-manager issuer CAs, TargetSource authentication |
| `ConfigMap` | Per-cluster controller CA material |
| `Service` | Prometheus output services, tunnel services |
| `StatefulSet` | The gNMIc collector StatefulSet per Cluster |
| `Certificate`, `Issuer` | cert-manager integration when TLS is enabled |

`Secret` is usually the largest by a wide margin, and it has nothing to do with how many
targets you have. Service-account tokens, TLS material and Helm release objects accumulate
across a cluster, and all of them sit in the operator's memory whether or not it ever reads
them.

This is why the memory request cannot be derived from target count alone.

## Restricting the watched namespaces

**Flag**: `--watch-namespaces`
**Helm Value**: `watchNamespaces`
**Type**: Comma-separated string (Helm: list)
**Default**: empty — watch all namespaces

**What it does**: limits the informer cache to the listed namespaces. Objects outside them
are neither cached nor watched.

```yaml
# values.yaml
watchNamespaces:
  - network-telemetry
```

```bash
# equivalently
--watch-namespaces=network-telemetry
```

Startup logs confirm the setting either way:

```
INFO  setup  restricting cache to namespaces  {"namespaces": ["network-telemetry"]}
INFO  setup  watching all namespaces; set --watch-namespaces to reduce cache footprint
```

### Why this is safe

The operator never resolves resources across namespace boundaries:

- A `Pipeline` resolves `Target`, `Subscription`, `Output`, `Input`, `Processor` and
  `TunnelTargetPolicy` objects only in its own namespace.
- `credentialsRef` on a `TargetProfile` is resolved in the **Target's** namespace.
- A `Cluster` reconciles only resources in its own namespace.

There is no cross-namespace reference anywhere in the API, so each namespace is already
self-contained. The operator also reads nothing from its *own* namespace — its TLS material
comes from mounted files rather than the API — so you do not need to add the operator's
namespace to the list.

### What to watch out for

**Resources in unwatched namespaces are silently ignored.** They are accepted by the API
server and then never reconciled: no StatefulSet, no status, no events. To make this
visible, creating a `Cluster`, `Pipeline` or `TargetSource` in an unwatched namespace
produces an admission warning:

```
Warning: namespace "other-ns" is not watched by this gnmic-operator instance
(watching: network-telemetry); this Cluster will be accepted but never reconciled
```

This is a warning rather than a rejection on purpose. Several namespace-scoped operator
instances can share a cluster, and because the webhook configurations are registered
cluster-wide, rejecting would mean every instance blocked resources intended for the others.

**The ClusterRole is still cluster-scoped.** Restricting the cache does not narrow the
operator's permissions. Converting to a `Role` per namespace is a separate change.

**Each namespace adds an informer set.** Memory scales with the number of watched
namespaces, so listing many namespaces recovers less than listing one.

## Kubernetes API rate limits

**Flags**: `--kube-api-qps`, `--kube-api-burst`
**Helm Values**: `kubeApi.qps`, `kubeApi.burst`
**Type**: Float, Integer
**Defaults**: `50`, `100`

**What it does**: sets the client-side rate limit for all Kubernetes API calls made by the
operator process.

The client-go defaults (20 QPS / 30 burst) are shared by every controller in the process.
When one controller is busy — for example writing `Target` status for a large population —
the others queue behind it. The symptom is misleading: `Cluster` reconciles appear
inexplicably slow with no errors logged anywhere, because the delay is client-side
throttling rather than API server pressure.

Raise these if you see reconcile latency that does not correspond to API server load.
Client-go logs waits longer than a second, and the `rest_client_rate_limiter_duration_seconds`
metric shows time spent waiting on the limiter.

## Memory sizing

The chart ships:

```yaml
resources:
  limits:
    cpu: 500m
    memory: 1Gi
  requests:
    cpu: 100m
    memory: 256Mi
```

> These defaults are a starting point, not a measured figure. Because the footprint depends
> on the total number of cached objects in your cluster rather than on target count, there
> is no formula that holds across environments.

Measure it in yours:

```promql
# Operator pod memory
container_memory_working_set_bytes{pod=~"gnmic-operator-.*"}

# Go heap, if the operator's metrics endpoint is scraped
go_memstats_heap_inuse_bytes{job="gnmic-operator"}
```

Set the limit above the observed peak with headroom, and re-check after any significant
growth in cluster-wide Secret count — not just after adding targets.

If the operator is being OOM-killed, prefer `--watch-namespaces` over simply raising the
limit. It addresses the cause rather than the symptom, and on a busy shared cluster the
difference is usually large.
