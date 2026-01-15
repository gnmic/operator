---
title: "API Reference"
linkTitle: "API Reference"
weight: 1
description: >
  Complete API reference for gNMIc Operator CRDs
---

## Cluster

**API Version**: `operator.gnmic.dev/v1alpha1`

### ClusterSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `replicas` | int32 | Yes | - | Number of gNMIc pods |
| `image` | string | Yes | - | Container image |
| `api` | APISpec | Yes | - | API configuration |
| `grpcTunnel` | GRPCTunnelConfig | No | - | gRPC tunnel server configuration |
| `resources` | ResourceRequirements | No | - | Pod resources |
| `env` | []EnvVar | No | - | Environment variables |

### APISpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `restPort` | int32 | Yes | - | REST API port |
| `gnmiPort` | int32 | No | - | gNMI server port |
| `tls` | ClusterTLSConfig | No | - | TLS configuration |

### ClusterTLSConfig

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `issuerRef` | string | No | - | cert-manager Issuer name for certificates |
| `useCSIDriver` | bool | No | false | Use cert-manager CSI driver instead of projected volumes |
| `bundleRef` | string | No | - | Additional CA bundle for client certificate verification |

### GRPCTunnelConfig

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `port` | int32 | Yes | - | Port for the gRPC tunnel server |
| `tls` | ClusterTLSConfig | No | - | TLS configuration for the tunnel |
| `service` | ServiceConfig | No | - | Kubernetes service configuration |

### ServiceConfig

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | ServiceType | No | LoadBalancer | Kubernetes service type (ClusterIP, NodePort, LoadBalancer) |
| `annotations` | map[string]string | No | - | Annotations to add to the service |

### ClusterStatus

| Field | Type | Description |
|-------|------|-------------|
| `readyReplicas` | int32 | Number of ready replicas |
| `pipelinesCount` | int32 | Number of enabled pipelines referencing this cluster |
| `targetsCount` | int32 | Total unique targets across all pipelines |
| `subscriptionsCount` | int32 | Total unique subscriptions across all pipelines |
| `inputsCount` | int32 | Total unique inputs across all pipelines |
| `outputsCount` | int32 | Total unique outputs across all pipelines |
| `conditions` | []Condition | Standard Kubernetes conditions |

### Cluster Conditions

| Type | Description |
|------|-------------|
| `Ready` | All replicas are ready and configured |
| `CertificatesReady` | TLS certificates are issued (when TLS enabled) |
| `ConfigApplied` | Configuration successfully applied to pods |

---

## Pipeline

**API Version**: `operator.gnmic.dev/v1alpha1`

### PipelineSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `clusterRef` | string | Yes | - | Reference to Cluster |
| `enabled` | bool | Yes | - | Whether pipeline is active |
| `targetRefs` | []string | No | - | Direct target references |
| `targetSelectors` | []LabelSelector | No | - | Target label selectors |
| `tunnelTargetPolicyRefs` | []string | No | - | Direct tunnel target policy references |
| `tunnelTargetPolicySelectors` | []LabelSelector | No | - | Tunnel target policy label selectors |
| `subscriptionRefs` | []string | No | - | Direct subscription references |
| `subscriptionSelectors` | []LabelSelector | No | - | Subscription label selectors |
| `outputs` | OutputSelector | No | - | Output selection |
| `inputs` | InputSelector | No | - | Input selection |

### OutputSelector

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `outputRefs` | []string | No | Direct output references |
| `outputSelectors` | []LabelSelector | No | Output label selectors |
| `processorRefs` | []string | No | Direct processor references (order preserved) |
| `processorSelectors` | []LabelSelector | No | Processor label selectors (sorted by name) |

### InputSelector

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `inputRefs` | []string | No | Direct input references |
| `inputSelectors` | []LabelSelector | No | Input label selectors |
| `processorRefs` | []string | No | Direct processor references (order preserved) |
| `processorSelectors` | []LabelSelector | No | Processor label selectors (sorted by name) |

### PipelineStatus

| Field | Type | Description |
|-------|------|-------------|
| `status` | string | Pipeline status (Active, Incomplete, Error) |
| `targetsCount` | int32 | Number of resolved static targets |
| `tunnelTargetPoliciesCount` | int32 | Number of resolved tunnel target policies |
| `subscriptionsCount` | int32 | Number of resolved subscriptions |
| `inputsCount` | int32 | Number of resolved inputs |
| `outputsCount` | int32 | Number of resolved outputs |
| `conditions` | []Condition | Standard Kubernetes conditions |

### Pipeline Conditions

| Type | Description |
|------|-------------|
| `Ready` | Pipeline has required resources (targets+subscriptions OR inputs) AND outputs |
| `ResourcesResolved` | All referenced resources were successfully resolved |

---

## Target

**API Version**: `operator.gnmic.dev/v1alpha1`

### TargetSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `address` | string | Yes | - | Device address (host:port) |
| `profile` | string | Yes | - | Reference to TargetProfile |

---

## TargetSource

**API Version**: `operator.gnmic.dev/v1alpha1`

### TargetSourceSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `http` | HTTPConfig | No | - | HTTP endpoint for target discovery |
| `consul` | ConsulConfig | No | - | Consul service discovery config |
| `configMap` | string | No | - | ConfigMap name containing targets |
| `podSelector` | LabelSelector | No | - | Select Kubernetes Pods as targets |
| `serviceSelector` | LabelSelector | No | - | Select Kubernetes Services as targets |
| `labels` | map[string]string | No | - | Labels to apply to discovered targets |

### HTTPConfig

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | Yes | URL of the HTTP endpoint |

### ConsulConfig

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | Yes | Consul server URL |

### TargetSourceStatus

| Field | Type | Description |
|-------|------|-------------|
| `status` | string | Sync status (Synced, Error, Pending) |
| `targetsCount` | int32 | Number of discovered targets |
| `lastSync` | Time | Last successful sync timestamp |

---

## TargetProfile

**API Version**: `operator.gnmic.dev/v1alpha1`

### TargetProfileSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `credentialsRef` | string | No | - | Reference to credentials Secret |
| `insecure` | bool | No | false | Skip TLS |
| `skipVerify` | bool | No | false | Skip certificate verification |
| `timeout` | duration | No | - | Connection timeout |
| `tlsCA` | string | No | - | TLS CA certificate |
| `tlsCert` | string | No | - | TLS client certificate |
| `tlsKey` | string | No | - | TLS client key |

---

## Subscription

**API Version**: `operator.gnmic.dev/v1alpha1`

### SubscriptionSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `paths` | []string | Yes | - | YANG paths to subscribe |
| `mode` | string | No | STREAM/SAMPLE | Subscription mode (combining mode and streamMode) |
| `sampleInterval` | duration | No | - | Sample interval |
| `encoding` | string | No | - | Data encoding |
| `prefix` | string | No | - | Path prefix |

### Subscription Modes

| Mode | Description |
|------|-------------|
| `stream` | Continuous streaming |
| `once` | Single request/response |
| `poll` | Client-initiated polling |

### Stream Modes

| Mode | Description |
|------|-------------|
| `sample` | Periodic sampling |
| `on-change` | Value change triggered |
| `target-defined` | Device determines |

---

## Output

**API Version**: `operator.gnmic.dev/v1alpha1`

### OutputSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | string | Yes | - | Output type |
| `config` | JSON | No | - | Type-specific config |
| `service` | OutputServiceSpec | No | - | K8s Service config (Prometheus only) |
| `serviceRef` | ServiceReference | No | - | Reference to a K8s Service for address resolution |
| `serviceSelector` | ServiceSelector | No | - | Label selector to discover K8s Services |

### OutputServiceSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | ServiceType | No | ClusterIP | Service type |
| `annotations` | map[string]string | No | - | Service annotations |
| `labels` | map[string]string | No | - | Service labels |

### ServiceReference

Used to reference a specific Kubernetes Service for address resolution.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | Yes | - | Name of the Service |
| `namespace` | string | No | Output's namespace | Namespace of the Service |
| `port` | string | No | First port | Port name or number |

### ServiceSelector

Used to discover Kubernetes Services by labels.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `matchLabels` | map[string]string | Yes | - | Labels to match services |
| `namespace` | string | No | Output's namespace | Namespace to search |
| `port` | string | No | First port | Port name or number |

### Output Types

| Type | Description | Supports serviceRef |
|------|-------------|---------------------|
| `prometheus` | Prometheus metrics endpoint | No |
| `prometheus_write` | Prometheus Remote Write | Yes |
| `kafka` | Apache Kafka | Yes |
| `influxdb` | InfluxDB | Yes |
| `nats` | NATS messaging | Yes |
| `jetstream` | NATS JetStream | Yes |
| `file` | File output | No |
| `tcp` | TCP socket | No |
| `udp` | UDP socket | No |

---

## Input

**API Version**: `operator.gnmic.dev/v1alpha1`

### InputSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | string | Yes | - | Input type |
| `config` | JSON | Yes | - | Type-specific config |

### Input Types

| Type | Description |
|------|-------------|
| `kafka` | Apache Kafka consumer |
| `nats` | NATS subscriber |
| `stan` | NATS Streaming subscriber |

---

## Processor

**API Version**: `operator.gnmic.dev/v1alpha1`

Processors transform telemetry data as it flows through gNMIc. They are attached to outputs or inputs via the Pipeline resource.

### ProcessorSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | string | Yes | - | Processor type |
| `config` | JSON | Yes | - | Type-specific config |

### Processor Types

| Type | Description |
|------|-------------|
| `event-add-tag` | Add static tags to events |
| `event-drop` | Drop events matching conditions |
| `event-strings` | Transform string values |
| `event-convert` | Convert value types |
| `event-extract-tags` | Extract tags from values |
| `event-trigger` | Execute actions on events |
| `event-write` | Write events to outputs |
| `event-delete` | Delete values from events |
| `event-merge` | Merge multiple events |
| `event-to-tag` | Convert values to tags |

### Processor Ordering

When processors are attached to an output or input via a Pipeline:

1. **processorRefs**: Applied first, in exact order specified (duplicates allowed)
2. **processorSelectors**: Applied after refs, sorted by name, deduplicated

Example:
```yaml
processorRefs: [proc-c, proc-a, proc-c]  # Order: c, a, c
processorSelectors:
  - matchLabels:
      auto: "true"  # Matches: proc-b, proc-d
                    # Sorted: b, d (a and c skipped if in refs)
# Final order: [proc-c, proc-a, proc-c, proc-b, proc-d]
```

---

## TunnelTargetPolicy

**API Version**: `operator.gnmic.dev/v1alpha1`

Defines matching rules for devices connecting via gRPC tunnel and associates them with a TargetProfile.

### TunnelTargetPolicySpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `match` | TunnelTargetMatch | No | - | Match criteria (if not set, matches all targets) |
| `profile` | string | Yes | - | Reference to a TargetProfile |

### TunnelTargetMatch

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | No | Regex pattern to match target type |
| `id` | string | No | Regex pattern to match target ID |

### Example

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TunnelTargetPolicy
metadata:
  name: core-routers
  labels:
    tier: core
spec:
  match:
    type: "router"
    id: "^core-.*"
  profile: router-profile
```

---

## Common Types

### LabelSelector

Standard Kubernetes label selector:

```yaml
matchLabels:
  key: value
matchExpressions:
  - key: tier
    operator: In
    values: [frontend, backend]
```

### ResourceRequirements

Standard Kubernetes resource requirements:

```yaml
requests:
  memory: "128Mi"
  cpu: "100m"
limits:
  memory: "256Mi"
  cpu: "500m"
```

### EnvVar

Standard Kubernetes environment variable:

```yaml
- name: VAR_NAME
  value: "value"
- name: SECRET_VAR
  valueFrom:
    secretKeyRef:
      name: secret-name
      key: secret-key
```

