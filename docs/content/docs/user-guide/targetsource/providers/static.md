---
title: "Static"
linkTitle: "Static"
weight: 3
description: >
  List devices inline in the TargetSource
---

An inline inventory. It exists so a small fixed lab needs no second resource, and so
the naming, pruning and conflict behaviour can be tried without any external system.

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: lab
spec:
  source:
    type: Static
    static:
      devices:
        - name: leaf1
          address: 10.0.0.1
        - name: leaf2
          address: 10.0.0.2
          port: 57401
          profile: srlinux
          labels: { role: leaf }
  target:
    port: 57400
    profile: default
```

| Field | Type | Description |
|-------|------|-------------|
| `devices[].name` | string | Required. |
| `devices[].address` | string | Required. An IP or hostname without a port. |
| `devices[].port` | int | Overrides `target.port` for this device. |
| `devices[].profile` | string | Overrides `target.profile` for this device. |
| `devices[].labels` | map | Added to the `Target`. |

Editing the list is a spec change and triggers a run at once.
