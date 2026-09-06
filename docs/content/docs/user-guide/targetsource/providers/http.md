---
title: "HTTP"
linkTitle: "HTTP"
weight: 2
description: >
  Read devices from a remote HTTP API returning JSON or YAML
---

The general-purpose provider, and the escape hatch for everything not built in.

## Basic configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: inventory
spec:
  interval: 5m
  source:
    type: HTTP
    http:
      url: https://inventory.example.com/api/devices
      auth:
        token:
          secretRef: { name: inventory, key: token }
      mapping:
        items: self.results
        name: item.hostname
        address: item.mgmt_ip
  target:
    profile: default
```

When the endpoint already returns the [native list](../#the-native-list), omit
`mapping` entirely.

## Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `url` | string | required | Must start with `http://` or `https://`. |
| `method` | string | `GET` | `GET` or `POST`. |
| `body` | string | | Sent with `POST` only. |
| `headers` | map | | Added to every request. Headers set by `auth` win. |
| `auth` | object | | One of `basic`, `token`, `header`. See below. |
| `tls` | object | | For `https` URLs. See below. |
| `mapping` | object | | How to read a document that is not the native list. See below. |
| `pagination` | object | | How to follow a multi-page response. See below. |

The request deadline is the `TargetSource`'s `timeout`, for all pages together.

## Authentication

Exactly one method. Every Secret is read from the `TargetSource` namespace. A change to
a referenced Secret triggers a run at once.

```yaml
auth:
  basic:
    secretRef: { name: inventory-basic }   # keys "username" and "password"
    usernameKey: user                       # optional overrides
    passwordKey: pass
```

```yaml
auth:
  token:
    scheme: Bearer                          # default; NetBox uses "Token"
    secretRef: { name: inventory, key: token }
```

```yaml
auth:
  header:
    name: X-Consul-Token
    secretRef: { name: consul, key: token }
```

## TLS

```yaml
tls:
  insecureSkipVerify: false
  caBundleRef: { name: corp-ca, key: ca.crt }       # ConfigMap key with PEM CAs
  clientCertRef: { name: inventory-client-cert }    # kubernetes.io/tls Secret, for mTLS
```

## Mapping

`mapping` reads devices out of an arbitrary JSON or YAML document. Every field is a
[CEL](https://github.com/google/cel-spec) expression with two variables: `self`, the
whole document, and `item`, the current device object.

| Field | Must return | Default |
|-------|-------------|---------|
| `items` | a list | The document itself must be a list. |
| `name` | a non-empty string | `item.name` |
| `address` | a non-empty string, an IP or hostname without a port | `item.address` |
| `port` | an integer or numeric string; `0` or missing means unset | `item.port`, then `target.port` |
| `profile` | a string | `item.profile`, then `target.profile` |
| `labels` | a map | `item.labels` |
| `onError` | | `Skip` |

A field without an expression falls back to the item's key of the same name, so a
document that already uses those key names only needs `items`.

```yaml
mapping:
  items: self.results
  name: item.hostname
  address: item.primary_ip4 != null ? item.primary_ip4.address.split('/')[0] : ''
  port: has(item.custom_fields.gnmi_port) ? item.custom_fields.gnmi_port : 0
  profile: item.platform.slug
  labels: |
    {
      "site": item.site.slug,
      "role": item.role.slug,
      "vendor": item.device_type.manufacturer.slug
    }
```

Expressions are compiled when the `TargetSource` is admitted. A syntax error is rejected
by the API server with the field path, instead of being accepted and quietly producing
fewer targets.

When an expression fails for one device at run time, `onError` decides:

- `Skip` (default): that device is dropped, counted in `status.invalid`, and listed in
  `status.failedDevices` with a reason such as `MappingError(address)`. The other
  devices are unaffected.
- `Fail`: the whole run fails.

The CEL environment includes the standard string, math, list, set and regex extensions,
and optional types.

## Pagination

A `Link: <...>; rel="next"` response header is always followed. For APIs that put the
next page in the body:

| Field | Description |
|-------|-------------|
| `nextField` | CEL over `self` returning the next page as a full URL, an opaque token, or `null` to stop. |
| `requestParam` | Query parameter to carry the token when `nextField` returns one rather than a URL. |
| `maxPages` | Upper bound on pages per run. Default `100`. |

```yaml
pagination:
  nextField: self.next               # NetBox, DRF: a full URL or null
```

```yaml
pagination:
  nextField: self.next_page_token    # cursor style
  requestParam: page_token
```

A run that stops early, because it reached `maxPages` or because the API returned a page
it had already served, is **truncated**: the devices read so far are applied, nothing is
pruned, and `Ready` is `False` with reason `Truncated`. A page that fails outright fails
the whole run.

## Errors

| Situation | Effect |
|-----------|--------|
| Connection refused, timeout, non-2xx status, malformed body | Run failure. `Ready=False` with reason `FetchFailed`, retried with backoff. Existing `Target` resources are untouched. |
| Referenced Secret or ConfigMap missing, CA bundle not PEM, expression does not compile | Spec failure. `Stalled=True`; retried at the backoff cap, and immediately when the spec or the referenced object changes. |
