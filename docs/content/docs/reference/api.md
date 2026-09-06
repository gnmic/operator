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
| `source` | SourceSpec | Yes | - | Where devices come from |
| `interval` | duration | No | `5m` | Time between runs after a successful one; at least `10s` |
| `timeout` | duration | No | `1m` | Time budget for one run; at least `1s`, capped at `interval` |
| `suspend` | bool | No | `false` | Stop discovery, leave existing Targets alone |
| `target` | TargetTemplateSpec | No | - | How a device becomes a Target |
| `prune` | PruneSpec | No | - | When Targets whose device is gone may be deleted |
| `maxTargets` | int32 | No | `10000` | Reject a run discovering more devices than this; `0` disables |
| `webhook` | WebhookSpec | No | - | Endpoint the source can call to request a run |

### SourceSpec

`type` names the provider and exactly one matching field must be set.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | `HTTP`, `Static`, `ConfigMap` or `Secret` |
| `http` | HTTPSource | When type is HTTP | Remote HTTP API |
| `static` | StaticSource | When type is Static | Inline device list |
| `configMap` | ObjectSource | When type is ConfigMap | Document in a ConfigMap |
| `secret` | ObjectSource | When type is Secret | Document in a Secret |

### HTTPSource

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `url` | string | Yes | - | Must start with `http://` or `https://` |
| `method` | string | No | `GET` | `GET` or `POST` |
| `body` | string | No | - | Sent with POST only |
| `headers` | map[string]string | No | - | Added to every request; `auth` headers win |
| `auth` | AuthSpec | No | - | Exactly one of `basic`, `token`, `header` |
| `tls` | ClientTLSSpec | No | - | For https URLs |
| `mapping` | MappingSpec | No | - | How to read a document that is not the native list |
| `pagination` | PaginationSpec | No | - | How to follow a multi-page response |

### AuthSpec

| Field | Type | Description |
|-------|------|-------------|
| `basic.secretRef.name` | string | Secret holding `username` and `password` keys |
| `basic.usernameKey` / `basic.passwordKey` | string | Override the key names |
| `token.scheme` | string | Prefix in the Authorization header, default `Bearer` |
| `token.secretRef` | SecretKeyReference | Secret key holding the token |
| `header.name` | string | Header to send |
| `header.secretRef` | SecretKeyReference | Secret key holding the header value |

### ClientTLSSpec

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `insecureSkipVerify` | bool | `false` | Skip server certificate verification |
| `caBundleRef` | ConfigMapKeyReference | - | ConfigMap key holding PEM CAs |
| `clientCertRef.name` | string | - | `kubernetes.io/tls` Secret for mutual TLS |

### MappingSpec

Every field is a CEL expression over `self` (the document) and `item` (the device object).

| Field | Must return | Default |
|-------|-------------|---------|
| `items` | list | The document itself must be a list |
| `name` | non-empty string | `item.name` |
| `address` | non-empty string without a port | `item.address` |
| `port` | integer or numeric string; 0 means unset | `item.port`, then `target.port` |
| `profile` | string | `item.profile`, then `target.profile` |
| `labels` | map | `item.labels` |
| `onError` | `Skip` or `Fail` | `Skip` |

### PaginationSpec

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `nextField` | string | - | CEL over `self` returning a URL, a token, or null |
| `requestParam` | string | - | Query parameter carrying a token |
| `maxPages` | int32 | `100` | Pages per run; reaching it marks the result truncated |

### StaticSource

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `devices[].name` | string | Yes | Device name |
| `devices[].address` | string | Yes | IP or hostname without a port |
| `devices[].port` | int32 | No | Overrides `target.port` |
| `devices[].profile` | string | No | Overrides `target.profile` |
| `devices[].labels` | map[string]string | No | Added to the Target |

### ObjectSource

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | ConfigMap or Secret in the TargetSource namespace |
| `key` | string | No | Key to read; empty reads every key in sorted order |
| `mapping` | MappingSpec | No | How to read a document that is not the native list |

### TargetTemplateSpec

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `port` | int32 | `57400` | Used when the source supplies none |
| `profile` | string | - | TargetProfile used when the source supplies none |
| `labels` | map[string]string | - | Added to every Target; source labels win |

### PruneSpec

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `maxDeleteRatio` | int32 (percent) | `50` | Largest share of managed Targets one run may delete; `100` disables, `0` holds all |
| `allowEmptySource` | bool | `false` | Whether zero devices is a valid answer |

### WebhookSpec

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Turn the refresh endpoint on |
| `auth.bearer.secretRef` | SecretKeyReference | - | Expected bearer token |
| `auth.signature.secretRef` | SecretKeyReference | - | HMAC key |
| `auth.signature.header` | string | `X-Hook-Signature` | Signature header |
| `auth.signature.algorithm` | string | `sha256` | `sha256` or `sha512` |
| `debounce` | duration | `5s` | Calls within this window do not schedule another run |

`auth` is required when `enabled` is true.

### TargetSourceStatus

| Field | Type | Description |
|-------|------|-------------|
| `observedGeneration` | int64 | Spec generation this status describes |
| `conditions` | []Condition | `Ready`, `Reconciling`; `Stalled` and `Conflicted` while true |
| `lastSyncTime` | Time | When a run last finished |
| `lastSuccessfulSyncTime` | Time | When a run last finished successfully |
| `nextSyncTime` | Time | When the next run is scheduled |
| `sourceDigest` | string | Stable hash of the last result |
| `discovered` | int32 | Devices the source returned |
| `managed` | int32 | Targets this source owns |
| `invalid` | int32 | Devices that did not become a Target |
| `sanitized` | int32 | Devices whose labels were rewritten |
| `conflicted` | int32 | Wanted names owned by something else |
| `pruned` | int32 | Targets removed by the last run |
| `failedDevices` | []FailedDevice | Up to 10 invalid devices with a reason |
| `consecutiveFailures` | int32 | Drives the retry backoff |
| `lastError` | string | Error from the last failed run |

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
| `url` | string | No | - | Path suffix appended after the resolved `scheme://host:port` (optional leading slash). Used for HTTP(S) outputs such as `prometheus_write` or `influxdb` (for example `api/v1/write`). |

### ServiceSelector

Used to discover Kubernetes Services by labels.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `matchLabels` | map[string]string | Yes | - | Labels to match services |
| `namespace` | string | No | Output's namespace | Namespace to search |
| `port` | string | No | First port | Port name or number |
| `url` | string | No | - | Path suffix appended after each resolved address; same meaning as `serviceRef.url`. |

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

