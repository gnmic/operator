---
title: "HTTP Provider"
linkTitle: "HTTP"
weight: 4
description: >
  HTTP TargetSource Discovery Provider
---

The HTTP provider discovers targets from an HTTP endpoint returning JSON, or receives webhook-based updates when push mode is enabled.

```yaml
spec:
  provider:
    http:
      url: http://inventory-service:8080/targets
```

## HTTP Spec Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `url` | string | No | - | HTTP endpoint used to pull targets. Required unless `push.enabled` is enabled |
| `method` | string | No | GET | HTTP method used for requests |
| `headers` | map[string]string | No | - | HTTP headers to include in requests |
| `body` | string | No | - | Request body for POST requests |
| `authorization` | object | No | - | Authentication configuration for the HTTP endpoint |
| `interval` | duration | No | 6h | Polling interval used to refresh targets |
| `timeout` | duration | No | 10s | Timeout for HTTP requests |
| `tls` | object | No | - | Client TLS configuration for HTTPS endpoints |
| `pagination` | object | No | - | Pagination configuration for parsing HTTP responses |
| `mapping` | object | No | - | Response mapping configuration for JSON responses |
| `push` | object | No | - | Push-based update configuration |

## Push Mode

The HTTP provider supports webhook-based target updates via `spec.provider.http.push`.

```yaml
spec:
  provider:
    http:
      push:
        enabled: true
```

When `push.enabled` is true, the operator accepts incoming webhook notifications and can update targets without polling a remote endpoint. The `url` field is optional when push mode is enabled, but can still be used for polling and fallback behavior.

## Authorization

The HTTP provider supports authenticated requests to the inventory endpoint.

Exactly one authorization method can be configured.

### Basic Authentication

Credentials are referenced from a Secret.

```yaml
spec:
  provider:
    http:
      url: https://inventory.example.com/targets
      authorization:
        basic:
          credentialsSecretRef:
            name: inventory-credentials
            key: username
```

### Token Authentication

Token authentication is configured using a Secret reference.

```yaml
spec:
  provider:
    http:
      url: https://inventory.example.com/targets
      authorization:
        token:
          scheme: Bearer
          tokenSecretRef:
            name: inventory-token
            key: token
```

## TLS

TLS settings can be configured for HTTPS endpoints.

```yaml
spec:
  provider:
    http:
      url: https://inventory.example.com/targets
      tls:
        insecureSkipVerify: false
        caBundleRef:
          name: inventory-ca
          key: ca.crt
```

### TLS Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `insecureSkipVerify` | bool | No | Skip verification of the server certificate. Defaults to `false` |
| `caBundleRef` | object | No | Reference to a ConfigMap containing a PEM-encoded CA bundle |

## Pagination

Pagination can be configured for APIs returning paginated responses.

```yaml
spec:
  provider:
    http:
      url: https://inventory.example.com/devices
      pagination:
        nextField: next
```

### Pagination Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `nextField` | string | No | Top-level JSON field containing the next page reference or pagination token |

The `nextField` value may either contain:
- A full URL for the next request
- A pagination token appended as a query parameter to the original URL

## Response Processing

The HTTP provider supports two response processing modes:

- **Default response format**: The endpoint returns a JSON array of target objects.
- **Response mapping**: Custom JSON structures are mapped to target fields using CEL expressions.

If `mapping` is configured, the custom mapping rules are used. Otherwise, the response itself must be a JSON array.

### Default Response Format

If `mapping` is not configured, the endpoint must return a JSON array of objects with the following structure:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Name of the generated `Target` resource |
| `address` | string | Yes | Device address (FQDN or IP address) |
| `port` | int32 | No | Port used for gNMI connections. If omitted, `spec.targetPort` is used |
| `labels` | map[string]string | No | Labels added to the generated `Target` resource |
| `targetProfile` | string | No | Reference to a `TargetProfile`. If omitted, `spec.targetProfile` is used |

Example response:

```json
[
  {
    "name": "spine1",
    "address": "spine1.local",
    "port": 57400,
    "labels": {
      "role": "spine"
    },
    "targetProfile": "spine-profile"
  },
  {
    "name": "leaf1",
    "address": "leaf1.local",
    "port": 57400,
    "labels": {
      "role": "leaf"
    }
  },
  {
    "name": "leaf2",
    "address": "leaf2.local",
    "port": 57400,
    "labels": {
      "role": "leaf"
    }
  }
]
```

### Response Mapping via CEL

`mapping` allows extracting target fields from arbitrary JSON structures using CEL expressions.

```yaml
spec:
  provider:
    http:
      url: https://inventory.example.com/devices
      mapping:
        targetsField: "self.results"
        name: "item.hostname"
        address: "item.management.ip"
        port: "item.gnmi.port"
        targetProfile: "item.profile"
        labels:
          role: "item.metadata.role"
          site: "item.metadata.site"
```

#### Mapping Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `targetsField` | string | No | CEL expression selecting the target list from the response |
| `name` | string | No | CEL expression for the target name |
| `address` | string | No | CEL expression for the target address |
| `port` | string | No | CEL expression for the target port |
| `labels` | string | No | CEL expression returning a map of labels |
| `targetProfile` | string | No | CEL expression for the target profile |

### CEL variables

The mapping expressions support the following variables:
- `item`: the current target object
- `self`: the full JSON response
