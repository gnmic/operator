---
title: "Webhook"
linkTitle: "Webhook"
weight: 4
description: >
  Let the source ask for an immediate discovery run
---

A `TargetSource` polls its source on `interval`. When the source can call out on change,
for example a NetBox webhook, it can ask the operator to run now instead of waiting.

The webhook is **notify-only**. The request body carries no target data and is not
parsed; it is only an input to signature verification. A successful call annotates the
`TargetSource`, and the controller performs an ordinary full run against the source.
That keeps one desired set, always read from the source, so a pushed device can never be
undone by the next poll.

## Configuration

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: netbox
  namespace: gnmic-system
spec:
  source:
    type: HTTP
    http:
      url: https://netbox.example.com/api/dcim/devices/
      auth:
        token:
          scheme: Token
          secretRef: { name: netbox-api-token, key: token }
      mapping:
        items: self.results
        address: item.primary_ip4.address.split('/')[0]
      pagination:
        nextField: self.next
  target:
    profile: netbox-device
  webhook:
    enabled: true
    debounce: 5s
    auth:
      bearer:
        secretRef: { name: gnmic-api-auth, key: bearer-token }
      signature:
        secretRef: { name: gnmic-signature, key: signature }
        header: X-Hook-Signature
        algorithm: sha256
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Turn the endpoint on for this `TargetSource`. |
| `auth` | object | required when enabled | At least one of `bearer`, `signature`. When both are set, both must pass. |
| `auth.bearer.secretRef` | Secret key | | Compared in constant time against `Authorization: Bearer <token>`. |
| `auth.signature.secretRef` | Secret key | | HMAC key. |
| `auth.signature.header` | string | `X-Hook-Signature` | Header carrying the hex signature, optionally prefixed with `sha256=` or `sha512=`. |
| `auth.signature.algorithm` | string | `sha256` | `sha256` or `sha512`. |
| `debounce` | duration | `5s` | Calls within this window of the previous one are acknowledged without scheduling another run. |

Authentication is mandatory: the API server rejects `enabled: true` without `auth`. An
unauthenticated endpoint would let anyone who can reach the API port drive fetches
against the source.

## Endpoint

The operator API listens on the address given by `--api-bind-address` (Helm:
`api.port`, default `8082`). It runs on every replica, so a Service in front of several
replicas works.

```
POST /api/v1/namespaces/{namespace}/targetsources/{name}/refresh
```

| Response | Meaning |
|----------|---------|
| `202 Accepted` | A run was requested. Body: `{"requestedAt": "...", "debounced": false}`. |
| `200 OK` | A run was requested less than `debounce` ago; nothing new was scheduled. Body: `{"requestedAt": "...", "debounced": true}`. |
| `401 Unauthorized` | Authentication failed. |
| `404 Not Found` | The `TargetSource` does not exist or has no webhook enabled. The two are indistinguishable on purpose. |

Example with both methods, signing an arbitrary body:

```bash
BODY='{"event":"device.updated"}'
SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SIGNATURE_SECRET" | awk '{print $NF}')
curl -X POST "http://gnmic-controller-manager-api.gnmic-system.svc:8082/api/v1/namespaces/gnmic-system/targetsources/netbox/refresh" \
  -H "Authorization: Bearer $BEARER_TOKEN" \
  -H "X-Hook-Signature: sha256=$SIG" \
  -d "$BODY"
```

For a NetBox walkthrough see [NetBox webhook](/docs/examples/netbox/webhook/).

## What changed from push mode

Earlier versions accepted target data on a `POST .../applyTargets` route and applied it
directly. That route is gone. A pushed device was applied and then deleted by the next
poll, because the poll's result did not contain it, and the endpoint only worked on the
leader replica. A source that has nothing to poll should write a
[ConfigMap](./providers/configmap/) or serve the [native list](./providers/#the-native-list)
over HTTP.
