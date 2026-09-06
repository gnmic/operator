---
title: "TargetSource"
linkTitle: "TargetSource"
weight: 4
description: >
  Discover targets from an external system and keep Target resources in sync with it
---

A `TargetSource` reads network devices from an external system and turns them into
`Target` resources. The operator then treats those `Target` resources like any other:
a `Pipeline` selects them and the `Cluster` controller pushes them to the gNMIc
collectors.

On every run the controller fetches the complete inventory, works out which `Target`
resources to create, update and delete, applies the differences, and writes its
status once. Nothing is kept between runs, so a restart loses nothing and a failure
cannot leave discovery silently stopped.

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

That is a complete, typical resource. Everything not shown has a default.

## Spec fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `source` | object | required | Where devices come from. See [Providers](./providers/). |
| `interval` | duration | `5m` | Time between two runs after a successful one. At least `10s`. Up to 10% jitter is added. |
| `timeout` | duration | `1m` | Time budget for one run, including all pages. At least `1s`; a value longer than `interval` is capped at `interval`. |
| `suspend` | bool | `false` | Stop discovery. Existing `Target` resources are left alone. |
| `target` | object | | How a device becomes a `Target`. See below. |
| `prune` | object | | When a `Target` whose device is gone may be deleted. See below. |
| `maxTargets` | int | `10000` | Reject a run that discovers more devices than this. `0` disables the cap. |
| `webhook` | object | | Let the source ask for an immediate run. See [Webhook](./webhook/). |

### `target`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `port` | int | `57400` | Used when the source does not supply a port. |
| `profile` | string | | `TargetProfile` used when the source does not supply one. A device with no profile from either place is reported as invalid. |
| `labels` | map | | Added to every `Target`. Labels from the source win on conflict. |

### `prune`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `maxDeleteRatio` | int (percent) | `50` | The largest share of managed `Target` resources one run may delete. `100` disables the guard, `0` holds every deletion. |
| `allowEmptySource` | bool | `false` | Whether a source that returns zero devices is a valid answer. |

## How a device becomes a Target

1. **Defaults.** A device without a port gets `target.port`; one without a profile gets
   `target.profile`.
2. **Labels.** `target.labels`, then the labels from the source, then the reserved
   operator labels `operator.gnmic.dev/targetsource` and
   `operator.gnmic.dev/managed-by`, which always win.
3. **Sanitising.** Every label key and value is made valid: invalid characters become
   `-`, values are cut to 63 characters. The original is kept on the `Target` as the
   annotation `operator.gnmic.dev/label.<key>`, and the device is counted in
   `status.sanitized`.
4. **Naming.** The resource is named `<targetsource>-<device name>`, lowercased and
   normalised to a valid Kubernetes name. The name the source reported is kept as the
   annotation `operator.gnmic.dev/discovered-name`. Two devices that normalise to the
   same name are reported as `DuplicateName`; the one whose name sorts first wins.
5. **Apply.** Each `Target` is written with server-side apply. Only the fields the
   source sets are owned: `spec.address`, `spec.profile`, the source's labels and
   annotations, and the owner reference. Labels and annotations you add to a
   discovered `Target` survive every run. A label the source also sets is the
   source's and is overwritten.

Renaming the `TargetSource` renames every `Target` it manages, and with them the target
names gNMIc uses. Treat the name as part of the data plane.

## Pruning and its guards

A managed `Target` whose device is no longer returned is deleted, unless one of three
guards holds. None of them is permanent: each clears as soon as the source returns a
complete, non-empty, plausible answer, and the run keeps its normal interval.

| Guard | When | Effect |
|-------|------|--------|
| `Truncated` | The source could not be read completely, for example a paginated API hit `maxPages`. | Nothing is deleted. `Ready=False`. |
| `EmptySource` | The source returned zero devices and `prune.allowEmptySource` is false. | Nothing is deleted. `Ready=False`. An empty answer is far more often a broken query or an expired token than a decommissioned network. |
| `PruneGuard` | This run would delete more than `prune.maxDeleteRatio` percent of the managed set. | Creates and updates still apply; deletions are skipped. `Ready=False`. Fix the source, or raise the ratio deliberately. |

## Ownership and deletion

Every managed `Target` carries a controller owner reference to its `TargetSource`.
Deleting the `TargetSource` deletes its `Target` resources through garbage collection.
There is no finalizer.

A `Target` that already exists under a wanted name and is not owned by this
`TargetSource` is left alone. The device is counted in `status.conflicted`, the
`Conflicted` condition is set, and an event is recorded.

A managed `Target` that you delete by hand comes back on the next run.

## Status

```
$ kubectl get targetsources
NAME        TYPE   TARGETS   READY   LAST SYNC   AGE
inventory   HTTP   142       True    2m          3d
```

| Field | Description |
|-------|-------------|
| `discovered` | Devices the source returned on the last run. |
| `managed` | `Target` resources this source owns right now. |
| `invalid` | Devices that did not become a `Target`, with a sample and the reason in `failedDevices`. |
| `sanitized` | Devices whose labels had to be rewritten. |
| `conflicted` | Wanted names owned by something else. |
| `pruned` | `Target` resources removed by the last run. |
| `lastSyncTime`, `lastSuccessfulSyncTime`, `nextSyncTime` | When the last run ended, when the last successful one ended, and when the next is due. |
| `sourceDigest` | A stable hash of the last result. It changes when the inventory changes. |
| `consecutiveFailures`, `lastError` | Drive the retry backoff. Cleared on success. |

### Conditions

| Type | True means | Reasons |
|------|------------|---------|
| `Ready` | The last run succeeded and the managed set matches the source. | `Succeeded`; when false: `FetchFailed`, `ApplyFailed`, `PartialFailure`, `EmptySource`, `Truncated`, `PruneGuard`, `InvalidSpec`, `SecretNotFound`, `ConfigMapNotFound`, `CapacityExceeded`, `Suspended` |
| `Reconciling` | A retry is scheduled or the last run applied only part of what it wanted. | `RetryScheduled`, `PartialFailure` |
| `Stalled` | Retrying without a change will never succeed: an expression does not compile, or a referenced Secret or ConfigMap does not exist. Only present while true. | `InvalidSpec`, `SecretNotFound`, `ConfigMapNotFound` |
| `Conflicted` | At least one wanted name is owned by something else. Only present while true. | `NameConflict` |

## Retries

After a failed run the next one is scheduled at `interval`, doubling on each
consecutive failure, capped at ten times `interval`. A source polled every five minutes
backs off from five to fifty minutes. A `Stalled` source waits at the cap; a change to
the spec or to a referenced Secret or ConfigMap triggers a run at once.

## Reacting faster than the interval

- A change to a `ConfigMap` or `Secret` the spec names, whether it is the source itself
  or holds credentials, triggers a run at once.
- The [webhook](./webhook/) lets the source itself ask for a run.

## Metrics

```
gnmic_operator_targetsource_sync_duration_seconds{namespace,name,type}
gnmic_operator_targetsource_sync_total{namespace,name,type,result}
gnmic_operator_targetsource_last_success_timestamp_seconds{namespace,name}
gnmic_operator_targetsource_targets{namespace,name,state}
```

`result` is `success`, `failure` or `held` (a prune guard). `state` is `managed`,
`invalid` or `conflicted`. Alert on the last-success timestamp not moving for three
intervals.

## Operator flags

| Flag | Default | Description |
|------|---------|-------------|
| `--targetsource-concurrency` | `4` | How many `TargetSource` resources may run discovery at once. A run holds a worker for up to its `timeout`, so this bounds how long a slow source can delay the others. |
