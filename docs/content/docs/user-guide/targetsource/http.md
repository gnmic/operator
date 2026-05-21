---
title: "HTTP Provider"
linkTitle: "HTTP"
weight: 4
description: >
  HTTP TargetSource Discovery Provider
---

The HTTP provider discovers targets from an HTTP endpoint returning a JSON array of target definitions.

```yaml
spec:
  provider:
    http:
      url: http://inventory-service:8080/targets
```

## HTTP Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | Yes | URL pointing to the inventory server |
| `acceptPush` | bool | No | Enable webhook-based target updates. Defaults to `false`. |
| `authorization` | object | No | Credentials used to access the HTTP endpoint. See _Authorization_ section. |
| `pollInterval` | duration | No | Polling interval used to fetch targets from the endpoint. Defaults to `30s`. |
| `timeout` | duration | No | Timeout for HTTP requests. Defaults to `10s`. |
| `tls` | object | No | Client TLS configuration for HTTPS endpoints. See _TLS_ section. |
| `pagination` | object | No | Pagination configuration for parsing responses from the HTTP endpoint. See _Pagination_ section. |
| `responseMapping` | object | No | JSONPath mapping definitions. See _Response Processing_ section. |

## Authorization

The HTTP provider supports authenticated requests to the inventory endpoint.

Exactly one authorization method can be configured.

### Basic Authentication

Credentials can either be defined inline or referenced from a Secret.

```yaml
spec:
  provider:
    http:
      url: https://inventory.example.com/targets
      authorization:
        basic:
          username: admin
          password: secret
```

Using a Secret reference:

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

Static token authentication can be configured using either an inline token or a Secret reference.

```yaml
spec:
  provider:
    http:
      url: https://inventory.example.com/targets
      authorization:
        token:
          scheme: Bearer
          token: eyJhbGciOi...
```

Using a Secret reference:

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
```

### TLS Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `insecureSkipVerify` | bool | No | Skip verification of the server certificate. Defaults to `false`. |
| `caBundle` | []byte | No | Base64-encoded PEM CA bundle used to validate the server certificate. |
| `caBundleSecretRef` | object | No | Reference to a Secret containing a PEM CA bundle. |

`caBundle` and `caBundleSecretRef` are mutually exclusive.

## Pagination

Pagination can be configured for APIs returning paginated responses.

```yaml
spec:
  provider:
    http:
      url: https://inventory.example.com/devices
      pagination:
        itemsField: results
        nextField: next
```

### Pagination Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `itemsField` | string | No | Top-level JSON field containing the list of target objects. |
| `nextField` | string | No | Top-level JSON field containing the next page reference or pagination token. |

The `nextField` value may either contain:
- A full URL for the next request
- A pagination token appended as a query parameter to the original URL

## Response Processing

The HTTP provider supports two methods for processing responses from the inventory endpoint:

- **Default Response Format**: The endpoint returns a predefined JSON structure understood directly by the operator.
- **Response Mapping via JSONPath**: Arbitrary JSON structures can be mapped to target fields using JSONPath expressions.

If `responseMapping` is configured, the custom mappings are used. Otherwise, the default response format is expected.

### Default Response Format

If `responseMapping` is not configured, the endpoint must return a JSON array of objects with the following structure:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Name of the generated `Target` resource |
| `address` | string | Yes | Device address (FQDN or IP address) |
| `port` | int32 | No | Port used for gNMI connections. If omitted, `spec.targetPort` is used. |
| `labels` | map[string]string | No | Labels added to the generated `Target` resource |
| `targetProfile` | string | No | Reference to a `TargetProfile`. If omitted, `spec.targetProfile` is used. |

Example response:

```json
[
  {
    "name": "spine1",
    "address": "spine1",
    "port": 57400,
    "labels": {
      "role": "spine"
    },
    "targetProfile": "spine-profile"
  },
  {
    "name": "leaf1",
    "address": "leaf1",
    "port": 57400,
    "labels": {
      "role": "leaf"
    }
  },
  {
    "name": "leaf2",
    "address": "leaf2",
    "port": 57400,
    "labels": {
      "role": "leaf"
    }
  }
]
```

### Response Mapping via JSONPath

`responseMapping` allows extracting target fields from arbitrary JSON structures using JSONPath expressions.

```yaml
spec:
  provider:
    http:
      url: https://inventory.example.com/devices
      responseMapping:
        name: "$.hostname"
        address: "$.management.ip"
        port: "$.gnmi.port"
        targetProfile: "$.profile"
        labels:
          role: "$.metadata.role"
          site: "$.metadata.site"
```

#### Response Mapping Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | JSONPath expression extracting the target name |
| `address` | string | Yes | JSONPath expression extracting the target IP address or hostname |
| `port` | string | No | JSONPath expression extracting the gNMI port |
| `targetProfile` | string | No | JSONPath expression extracting the `TargetProfile` |
| `labels` | map[string]string | No | JSONPath expressions extracting target labels |
