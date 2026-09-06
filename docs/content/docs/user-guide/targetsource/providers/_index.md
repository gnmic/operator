---
title: "Providers"
linkTitle: "Providers"
weight: 1
description: >
  Where a TargetSource reads its devices from
---

`spec.source.type` names the provider, and exactly one matching block must be set.
Everything outside `source` means the same thing for every provider.

| Type | Reads from | Page |
|------|-----------|------|
| `HTTP` | A remote HTTP API returning JSON or YAML | [HTTP](./http/) |
| `Static` | A device list written inline in the spec | [Static](./static/) |
| `ConfigMap` | A document in a ConfigMap in the same namespace | [ConfigMap and Secret](./configmap/) |
| `Secret` | A document in a Secret in the same namespace | [ConfigMap and Secret](./configmap/) |

## The native list

`HTTP`, `ConfigMap` and `Secret` read a document. With no `mapping`, that document must
be the operator's own shape: a JSON or YAML array of objects with `name`, `address`,
`port`, `profile` and `labels`. Only `name` and `address` are required.

```json
[
  { "name": "leaf1", "address": "10.0.0.1", "port": 57400, "profile": "srl",
    "labels": { "site": "ams1" } },
  { "name": "leaf2", "address": "10.0.0.2" }
]
```

This is what to write into a ConfigMap by hand, and what an in-house adapter should
emit. With a `mapping`, the document can be anything and CEL expressions say how to
read it. See [HTTP](./http/#mapping).

## External adapters

There is no plugin mechanism. An organisation with an in-house inventory writes a small
service that returns the native list over HTTP and points a `type: HTTP` source at it,
with no `mapping` and no CEL. In-tree providers exist for sources common enough that
everyone would otherwise write the same adapter.
