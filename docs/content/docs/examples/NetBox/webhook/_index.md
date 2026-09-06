---
title: "Webhook"
linkTitle: "Webhook"
weight: 2
description: >
  Configure a NetBox webhook so a device change triggers a discovery run at once.
---

## Netbox Webhook Configuration

This example walks through configuring a webhook in NetBox so that a device change makes the gNMIc Operator re-read the NetBox inventory immediately, instead of waiting for the next `interval`. The webhook carries no target data: it only asks for a run, and the run reads NetBox like any other. It covers the configuration in the gNMIc Operator (Step 1-3), and the configuration within NetBox (step 4).

1. Create Targetprofile
2. Create Kubernetes Secrets
3. Apply TargetSource
4. Netbox setup
  a: Configure Webhook
  b: Create Event Rule
5. Verification

At the end, the logs will show the incoming POST requests, `status.lastSuccessfulSyncTime` on the TargetSource moves with each webhook call, and target updates can be verified with `kubectl get targets`.

## Prerequisites

- Kubernetes cluster with gNMIc Operator installed
- `kubectl` access to your cluster
- Running NetBox instance
- Network connectivity from NetBox to the gNMIc Operator API endpoint

---

### 1. Create TargetProfile

Define how discovered targets should be configured. The `TargetProfile` contains device credentials, such as username/password or client certificates. These are either defined inline strings or stored in a [Kubernetes Secret](https://kubernetes.io/docs/concepts/configuration/secret/).

```yaml
# Replace YOUR_DEVICE_USERNAME and YOUR_DEVICE_PASSWORD with your corresponding default device username and password
apiVersion: v1
kind: Secret
metadata:
  name: device-credentials
  namespace: gnmic-system
type: Opaque
stringData:
  username: YOUR_DEVICE_USERNAME
  password: YOUR_DEVICE_PASSWORD
```

When using a secret, create a credentials Secret first, then reference it from the profile.

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: netbox-device
  namespace: gnmic-system
spec:
  credentialsRef: device-credentials
  timeout: 10s
```

For more TargetProfile options and credential handling, see the operator documentation for `TargetProfile`.

---

### 2. Create Kubernetes Secrets

The NetBox API token, bearer authentication and signature verification all require Kubernetes secrets. Ensure that the secrets:

- Are created in the same namespace as the TargetSource (`gnmic-system` in this example).
- Use `name` and `key` values that match the TargetSource spec.

```bash
kubectl create secret generic netbox-api-token --from-literal=token=YOUR_NETBOX_API_TOKEN -n gnmic-system
kubectl create secret generic gnmic-api-auth --from-literal=bearer-token=YOUR_SECRET_TOKEN -n gnmic-system
kubectl create secret generic gnmic-signature --from-literal=signature=YOUR_SECRET_SIGNATURE -n gnmic-system
```

---

### 3. Apply TargetSource

The TargetSource polls the NetBox REST API on `interval` and additionally accepts webhook calls:

- `spec.source` reads devices from NetBox, as in the [REST API example](../rest-api/).
- `spec.webhook.enabled` is `true`; calls are rejected otherwise.
- Bearer authentication and signature verification are both enabled, referencing the secrets created in step 2. Both must pass.

```yaml
# netbox.yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: netbox
  namespace: gnmic-system
spec:
  interval: 5m
  source:
    type: HTTP
    http:
      url: "http://netbox.example.com:8000/api/dcim/devices/?limit=1000"
      auth:
        token:
          scheme: Token
          secretRef:
            name: netbox-api-token
            key: token
      pagination:
        nextField: "self.next"
      mapping:
        items: "self.results"
        address: "item.primary_ip4 != null ? item.primary_ip4.address.split('/')[0] : ''"
  target:
    port: 57400
    profile: netbox-device
    labels:
      inventory: netbox
      sync-source: rest-api
  webhook:
    enabled: true
    debounce: 5s
    auth:
      bearer:
        secretRef:
          name: gnmic-api-auth
          key: bearer-token
      signature:
        secretRef:
          name: gnmic-signature
          key: signature
        header: X-Hook-Signature
        algorithm: sha512
```

> Namespace is `gnmic-system`, the name of the TargetSource is `netbox`. These values will be in the URL in step 4.

---

### 4. Netbox Setup

Next, configure a webhook in NetBox. The webhook is triggered by device events (for example, updates) and sends an HTTP POST request to the gNMIc Operator.

#### Configure Webhook

In NetBox, go to `Operations > Webhooks` and create a webhook with the following settings:

- *Name*: gNMIc Operator refresh
- *URL*: `http://gnmic-controller-manager-api.gnmic-system.svc.cluster.local:8082/api/v1/namespaces/gnmic-system/targetsources/netbox/refresh`
  - URL contains the namespace `gnmic-system` and TargetSource name `netbox`. See [Webhook](/docs/user-guide/targetsource/webhook/) for the endpoint.
  - `gnmic-controller-manager-api.gnmic-system.svc.cluster.local` is only reachable if Netbox is inside the cluster.
  - The address may instead be `http://localhost:8082/` or `http://servername:8082/`.
- *HTTP method*: POST
- *HTTP content type*: application/json
- *Additional headers:* `Authorization: Bearer YOUR_SECRET_TOKEN`
- *Body Template*: leave the default. The body is not parsed; it is only the input to
  signature verification. NetBox signs whatever body it sends with the *Secret* below,
  using HMAC-SHA512, which is why the TargetSource sets `algorithm: sha512`.

- *Secret*: `YOUR_SECRET_SIGNATURE`
- *SSL Verification*: true

#### Create Event Rule

The webhook requires a trigger, configured as an event rule under `Operations > Event Rules`.

- *Name*: gNMIc Operator refresh on device change
- *Object types*: `DCIM > Device`
- *Event types*: `Object Created`, `Object Updated` and `Object Deleted`
- *Action type*: Webhook
- *Webhook*: gNMIc Operator refresh

---

### 5. Verification

Updating a device in NetBox should now trigger the webhook, and the TargetSource should
run within a few seconds instead of at the next interval. Verify this with:

```bash
# lastSuccessfulSyncTime moves with each webhook call
kubectl get targetsource netbox -n gnmic-system -o jsonpath='{.status.lastSuccessfulSyncTime}{"\n"}'
kubectl get targets -n gnmic-system
kubectl get targets <targetname> -n gnmic-system -o yaml

# Incoming refresh requests, accepted and rejected, are logged by the api-server component:
kubectl logs -n gnmic-system deploy/gnmic-controller-manager -f | grep refresh
```

A `202` means a run was requested, a `200` with `debounced: true` means one was already
requested within `webhook.debounce`, and a `401` means the bearer token or signature did
not match. If nothing appears in the logs, the request is not reaching the operator.

---

## Example: Complete Setup

Here's a complete example combining all resources:

 ```yaml
---
# Secret for Target Credential
apiVersion: v1
kind: Secret
metadata:
  name: device-credentials
  namespace: gnmic-system
type: Opaque
stringData:
  username: YOUR_DEVICE_USERNAME
  password: YOUR_DEVICE_PASSWORD

---
# TargetProfile
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: netbox-device
  namespace: gnmic-system
spec:
  credentialsRef: device-credentials
  timeout: 10s
---
# Apply Targetsource
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: netbox
  namespace: gnmic-system
spec:
  interval: 5m
  source:
    type: HTTP
    http:
      url: "http://netbox.example.com:8000/api/dcim/devices/?limit=1000"
      auth:
        token:
          scheme: Token
          secretRef:
            name: netbox-api-token
            key: token
      pagination:
        nextField: "self.next"
      mapping:
        items: "self.results"
        address: "item.primary_ip4 != null ? item.primary_ip4.address.split('/')[0] : ''"
  target:
    port: 57400
    profile: netbox-device
    labels:
      inventory: netbox
      sync-source: rest-api
  webhook:
    enabled: true
    debounce: 5s
    auth:
      bearer:
        secretRef:
          name: gnmic-api-auth
          key: bearer-token
      signature:
        secretRef:
          name: gnmic-signature
          key: signature
        header: X-Hook-Signature
        algorithm: sha512
```
