---
title: "ConfigMap and Secret"
linkTitle: "ConfigMap and Secret"
weight: 4
description: >
  Read devices from a ConfigMap or Secret in the same namespace
---

The in-cluster inventory: a list maintained by a human, by CI, or by a job that exports
from somewhere else. A system that used to push targets into the operator should write a
ConfigMap instead and point a `TargetSource` at it.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: lab-devices
data:
  devices.yaml: |
    - name: leaf1
      address: 10.0.0.1
    - name: leaf2
      address: 10.0.0.2
      labels:
        role: leaf
---
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: lab
spec:
  source:
    type: ConfigMap
    configMap:
      name: lab-devices
      key: devices.yaml
  target:
    profile: default
```

`type: Secret` has the same shape under `secret:`. Use it when the document must not be
readable by everyone who can read ConfigMaps.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Required. The ConfigMap or Secret, in the `TargetSource` namespace. |
| `key` | string | The key to read. When empty, every key is read and the results are concatenated, so an inventory can be split across keys. |
| `mapping` | object | How to read a document that is not the [native list](../#the-native-list). Same fields as the [HTTP mapping](../http/#mapping). |

The document may be JSON or YAML.

## Behaviour

- The object is watched. Editing it triggers a run at once; there is no need to wait for
  the interval.
- A missing object, a missing key, or a document that does not parse is a spec failure:
  the `TargetSource` reports `Stalled` until the object is fixed, and runs as soon as it
  changes.
- With no `key`, keys are read in sorted order, so the result does not depend on the
  order the object happens to list them in.
