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
| `interval` | duration | No | 30m | Polling interval used to refresh targets |
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

Pagination enables the operator to retrieve all targets from APIs that return results in multiple pages. This is common with inventory systems that limit response sizes for performance reasons.

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
| `nextField` | string | No | Top-level JSON field name containing the pagination reference or token |

The `nextField` value may either contain:
- A full URL for the next request
- A pagination token appended as a query parameter to the original URL

### How Pagination Works

The operator handles two common pagination patterns used by network inventory systems:

#### 1. Cursor-Based Pagination (Tokens)
When your inventory API returns a pagination token (e.g., `"next": "abc123xyz"`), the operator automatically appends it as a query parameter to construct the next request:

```
First request:  GET https://inventory.example.com/devices
Response contains: "next": "page2token"
Next request:   GET https://inventory.example.com/devices?next=page2token
```

Example response:
```json
{
  "devices": [...],
  "next": "page2token"
}
```

#### 2. URL-Based Pagination
When your API returns a complete URL for the next page (e.g., `"next": "https://inventory.example.com/devices?offset=50"`), the operator uses it directly without modification:

```
First request:  GET https://inventory.example.com/devices
Response contains: "next": "https://inventory.example.com/devices?offset=50"
Next request:   GET https://inventory.example.com/devices?offset=50
```

Example response:
```json
{
  "devices": [...],
  "next": "https://inventory.example.com/devices?offset=50"
}
```

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

When your inventory API's JSON structure differs from the default format, use CEL (Common Expression Language) mapping to extract target fields.

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
        labels: "{'role': item.metadata.role, 'site': item.metadata.site}"
```

#### Understanding `targetsField`

The `targetsField` expression tells the operator where to find the list of target objects in your API response. It's particularly important when your API wraps the target list in a data structure.

**When to use `targetsField`:**
- Your API returns `{"results": [...]}`  -> use `"self.results"`
- Your API returns `{"data": {"devices": [...]}}`  -> use `"self.data.devices"`
- Your API returns a plain array `[...]`  -> omit `targetsField` (default behavior)

**Example scenarios:**

*Custom API response example 1:*
```json
{
  "count": 42,
  "next": "https://...",
  "results": [
    {"id": 1, "name": "device1", "primary_ip": "10.0.0.1"},
    {"id": 2, "name": "device2", "primary_ip": "10.0.0.2"}
  ]
}
```
Usage: `targetsField: "self.results"`

*Custom API response example 2:*
```json
{
  "status": "success",
  "data": {
    "timestamp": "2024-01-01T00:00:00Z",
    "devices": [
      {"name": "router1", "mgmt_ip": "192.168.1.1"},
      {"name": "router2", "mgmt_ip": "192.168.1.2"}
    ]
  }
}
```
Usage: `targetsField: "self.data.devices"`

#### Mapping Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `targetsField` | string | No | CEL expression selecting the target list from the response. If omitted, assumes response is a direct JSON array |
| `name` | string | No | CEL expression for the target name |
| `address` | string | No | CEL expression for the target address |
| `port` | string | No | CEL expression for the target port |
| `labels` | string | No | CEL expression returning a map of labels |
| `targetProfile` | string | No | CEL expression for the target profile |

#### CEL Variables

The mapping expressions support the following variables:
- `item`: the current target object being processed
- `self`: the complete unprocessed response from the HTTP endpoint

### Performance: CEL vs Direct Mapping

Understanding the performance implications helps optimize your configurations:

**Direct Mapping (No CEL)** - *Fastest*
- Used when your API response matches the default structure exactly
- No expression compilation or evaluation overhead
- Suitable for high-frequency polling (e.g., every minute)
- Example: API returns `[{"name": "...", "address": "..."}]`

**CEL Mapping** - *Slight overhead*
- CEL expressions are compiled once at startup (not per request)
- Evaluation is performed per target object during each poll cycle
- At high scale (10,000+ targets), consider the `interval` between polls

**Best practices:**
- Use direct mapping if your API already returns the correct structure
- For large result sets, increase the interval
- Combine CEL and direct mapping for efficiency (see hybrid mapping below)
- Use CEL extensions (see reference table below) to reduce complexity and improve readability

### CEL Extensions

The operator includes a set of standard CEL extensions from the official [CEL Go library](https://github.com/google/cel-go) to enable more advanced expressions.

These [extensions](https://pkg.go.dev/github.com/google/cel-go/ext) expand CEL with additional capabilities commonly needed when transforming API responses:

| Extension | Purpose |
|----------|----------|
| **Strings** | String manipulation such as splitting values, case conversion, and extracting parts of text (e.g. parsing hostnames or IPs) |
| **Math** | Numeric operations and comparisons (e.g. calculations, min/max, type conversions) |
| **Lists** | Working with arrays (e.g. indexing, filtering, joining values) |
| **Sets** | Set-style operations such as membership checks and comparisons |
| **Regex** | Pattern matching and validation using regular expressions |
| **Bindings** | Defining intermediate variables to simplify complex expressions |

**Examples:**

```yaml
mapping:
  # Extract site from hostname
  labels: |
    {
      'site': item.name.split('-')[0]
    }

  # Conditional profile
  targetProfile: "item.type == 'edge' ? 'edge' : 'core'"

  # Pattern-based classification
  labels: |
    {
      'role': item.name.matches('^spine') ? 'spine' : 'leaf'
    }
```

### Combining CEL and Direct Mapping (Hybrid Approach)

You don't need to map all fields with CEL. The operator supports mixing CEL expressions and direct field lookups for maximum efficiency:

| Scenario | Behavior | Use Case |
|----------|----------|----------|
| `name`, `address` use CEL; others omitted | Extracts mapped fields via CEL; looks for `port`, `labels`, `targetProfile` directly in item JSON | Simple API where only some fields need transformation |
| Only `labels` uses CEL | Other fields use direct mapping; labels constructed from CEL expression | API returns correct `name`, `address`, `port` but custom labels need extraction |
| Only `address` uses CEL | Direct mapping for other fields; only address requires transformation | Most fields match API exactly except address requires CIDR parsing or format conversion |
| All fields use CEL | Complete transformation via expressions | API structure completely different from expected format |

This hybrid approach optimizes performance by only compiling and evaluating CEL where needed.

**Example - Partial CEL mapping (only transform what needs transforming):**
```yaml
mapping:
  # name: "item.name" -> OMITTED: already matches default "name" field
  address: "item.primary_ip4 != null ? item.primary_ip4.split('/')[0] : item.primary_ip6.split('/')[0]"  # CEL: parse CIDR
  # port: OMITTED: already exists as "port" field in item
  labels: |  # CEL: construct labels from custom fields
    {
      'site': item.site.name,
      'role': item.device_role.name
    }
  # targetProfile: OMITTED: already exists as "targetProfile" field in item
```

In this example, only `address` and `labels` use CEL expressions; `name`, `port`, and `targetProfile` use direct field lookups for efficiency.

### Using YAML `|` for Complex CEL Expressions

When writing more complex CEL expressions, it is recommended to use YAML’s pipe (`|`) literal block instead of inline strings.

This is especially useful for expressions that span multiple lines or contain nested logic.

#### Recommended pattern (labels example)

```yaml
mapping:
  labels: |
    {
      "site": item.site.name,
      "rack": item.rack != null ? item.rack.name : "",
      "role": item.role != null ? item.role : "unknown",
      "tags": item.tags.size() > 0 ? ','.join(item.tags) : ""
    }
```

**Why use `|` instead of quoted strings:**
- **Readability**: Multi-line expressions are easier to understand
- **Maintainability**: Complex CEL expressions don't require escaping
- **YAML best practice**: Literal blocks handle special characters naturally

## Recommended Production Settings

When deploying HTTP TargetSource providers in production networks, follow these guidelines to ensure reliable and efficient target discovery:

### Polling Configuration
| Scenario | Setting | Rationale |
|----------|---------|-----------|
| **Small environment** (< 100 targets) | `interval: 5m` | Frequent updates without excessive load |
| **Medium environment** (100-500 targets) | `interval: 10m` | Balance between freshness and API load |
| **Large environment** (500-2000 targets) | `interval: 15m` | Reduce API polling overhead |
| **Very large environment** (2000+ targets) | `interval: 30m` | Minimize impact on inventory system |
| **High-frequency changes** | Use `push` mode with `interval` | Enables sub-minute updates via push while periodic polling ensures completeness and consistency |

**Timeout Configuration:**
```yaml
timeout: 30s  # Allows for network latency
```

If timeouts consistently occur, increase `interval` instead of timeout (don't poll faster)

### Authentication & Security

**Always use TLS in production:**
```yaml
tls:
  insecureSkipVerify: false  # Never skip verification in production
  caBundleRef:
    name: inventory-ca-bundle
    key: ca.crt
```

**For authenticated APIs:**
- Store credentials in Kubernetes Secrets
- Rotate credentials periodically
- Use token-based auth when possible (simpler secret rotation)

Example:
```yaml
authorization:
  token:
    scheme: Bearer
    tokenSecretRef:
      name: inventory-api-token
      key: token
```

### Pagination & Large Result Sets

**Configuration for APIs returning large result sets:**
```yaml
pagination:
  nextField: next  # Always configure pagination if your API supports it

interval: 30m     # Increase interval for large datasets (reduces cumulative API load)
timeout: 60s      # Increase only if individual requests are slow or responses are large
```

Pagination splits large datasets into multiple smaller HTTP requests. This improves reliability and reduces the likelihood of timeouts compared to fetching a single large response.

**Optimization strategies:**
- Request API filtering (if supported) to reduce result set size (e.g. ?limit=1000 or ?status=active)
- If the API does not support pagination or filtering increase the timeout
- Consider webhook push mode for frequently-changing inventories (if API supports it)

### Mapping Optimization

**Use hybrid CEL and direct mapping for performance:**
```yaml
# EFFICIENT - Only CEL-transform what needs it
mapping:
  # These fields exist directly in API response -> no CEL needed
  name: "item.hostname"         # Direct: omit CEL, fallback to field lookup
  # port: (OMITTED)            # Direct: exists as "port" in item
  
  # Only these need transformation -> use CEL
  address: "item.primary_ip.split('/')[0]"  # CEL: parse CIDR
  labels: |                      # CEL: construct from nested fields
    {'site': item.site.name}
```

**Avoid unnecessary CEL complexity:**
```yaml
# GOOD - Simple expressions
mapping:
  address: "item.management_ip"
  port: "int(item.gnmi_port)"

# AVOID - Nested ternary logic (hard to debug)
mapping:
  name: "item.has_override ? item.override_name : (item.hostname != '' ? item.hostname : 'default-' + string(item.id))"
```

**CEL expression best practices:**
- Compile expressions once at startup (not per request), so complexity is paid only once
- Use `ext.Bindings` for repeated expressions to avoid redundant evaluation
- Test CEL expressions thoroughly; they're compiled but errors only appear during evaluation
- Keep expressions under 200 characters for maintainability

### Example Production Configuration

```yaml
apiVersion: gnmic.openconfig.net/v1alpha1
kind: TargetSource
metadata:
  name: production-inventory
spec:
  provider:
    http:
      # Security
      url: https://inventory.prod.example.com/api/dcim/devices/?limit=100
      tls:
        insecureSkipVerify: false
        caBundleRef:
          name: netbox-ca
          key: ca.crt
      
      # Authentication
      authorization:
        token:
          scheme: Bearer
          tokenSecretRef:
            name: api-token
            key: token
      
      # Timing
      interval: 15m  # Balanced update frequency
      timeout: 30s   # Allow for network latency
      
      # Pagination
      pagination:
        nextField: next
      
      # Mapping for fields
      mapping:
        targetsField: "self.results"
        #name: "item.name" -> already handled with fallback direct mapping
        address: "item.primary_ip4 != null ? item.primary_ip4.split('/')[0] : item.primary_ip6.split('/')[0]"
        port: "item.custom_fields.gnmi_port"
        labels: "{\n          'site': item.site.name,\n          'role': item.device_role.name,\n          'status': item.status.value\n        }"
        targetProfile: "item.custom_fields.gnmi_profile"
  
  # Global settings
  targetPort: 9339
  targetProfile: default-profile
```

This configuration ensures:

- Secure HTTPS communication with certificate validation
- API authentication with token-based credentials
- Balanced polling interval for stable environments
- Proper pagination handling for large device inventories
- Rich label extraction from custom fields
- Fallback to defaults when fields are missing
