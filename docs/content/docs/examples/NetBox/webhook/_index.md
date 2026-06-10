---
title: "Push Mode with Webhook"
linkTitle: "Push Mode with Webhook"
weight: 2
description: >
  Configure a webhook in NetBox to update targets in the gNMIc Operator in real time.
---

## Netbox Webhook Configuration

This example walks through configuring a webhook in NetBox to push real-time target updates to the gNMIc Operator. It covers the configuration in the gNMIc Operator (Step 1-3), and the configuration within Netbox (step 4).

1. Create Targetprofile
2. Create Kubernetes Secrets
3. Apply TargetSource
4. Netbox setup
  a: Configure Webhook
  b: Create Event Rule
5. Verification

At the end, the logs will show the incoming POST requests and the targets updates can be verified with `kubectl get targets`.

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

Bearer authentication and signature verification both require Kubernetes secrets. Ensure that the secrets:

- Are created in the same namespace as the TargetSource (`gnmic-system` in this example).
- Use `name` and `key` values that match the TargetSource spec.

```bash
kubectl create secret generic gnmic-api-auth --from-literal=bearer-token=YOUR_SECRET_TOKEN -n gnmic-system
kubectl create secret generic gnmic-signature --from-literal=signature=YOUR_SECRET_SIGNATURE -n gnmic-system
```

---

### 3. Apply TargetSource

The TargetSource has the following settings configured:

- `spec.provider.http.push.enabled` must be set to `true`, otherwise updates are rejected.
- Bearer authentication and signature verification are enabled, referencing to the secrets created in step 2.

```yaml
# netbox.yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: netbox
  namespace: gnmic-system
spec:
  targetPort: 57400
  targetProfile: netbox-device
  targetLabels:
    inventory: netbox
    sync-source: rest-api
  provider:
    http:
      push:
        enabled: true
        auth:
          bearer:
            tokenSecretRef:
              name: gnmic-api-auth
              key: bearer-token
        signature:
          secretRef:
            name: gnmic-signature
            key: signature
```

> Namespace is `gnmic-system`, the name of the TargetSource is `netbox`. These values will be in the URL in step 4.

---

### 4. Netbox Setup

Next, configure a webhook in NetBox. The webhook is triggered by device events (for example, updates) and sends an HTTP POST request to the gNMIc Operator.

#### Configure Webhook

In NetBox, go to `Operations > Webhooks` and create a webhook with the following settings:

- *Name*: gNMIc Operator push
- *URL*: `http://gnmic-controller-manager-api.gnmic-system.svc.cluster.local:8082/api/v1/gnmic-system/target-source/netbox/applyTargets`
  - URL contains the namespace `gnmic-system` and TargetSource name `netbox`. See section address in [Push Mode](/docs/user-guide/targetsource/push/) for more details on URL construction.
  - `gnmic-controller-manager-api.gnmic-system.svc.cluster.local` is only reachable if Netbox is inside the cluster.
  - The address may instead be `http://localhost:8082/` or `http://servername:8082/`.
- *HTTP method*: POST
- *HTTP content type*: application/json
- *Additional headers:* `Authorization: Bearer YOUR_SECRET_TOKEN`
- *Body Template*:

  ```json
  [
    {
      "name": "{{ data.name }}",
      "address": "{{ data.primary_ip4.address.split('/')[0] }}",
      "operation": "{{ event }}",
      "targetProfile": "{{ data.custom_fields.target_profile | default('', true) }}",
      "port": {{ data.custom_fields.gnmic_port | default(57400, true) }},
      "labels": [
          {"vendor":"{{ data.device_type.manufacturer.name }}"}
        ]
    }
  ]
  ```

- *Secret*: `YOUR_SECRET_SIGNATURE`
- *SSL Verification*: true

#### Create Event Rule

The webhook requires a trigger, configured as an event rule under `Operations > Event Rules`.

- *Name*: gNMIc Operator push target change
- *Object types*: `DCIM > Device`
- *Event types*: `Object Created`, `Object Updated` and `Object Deleted`
- *Action type*: Webhook
- *Webhook*: gNMIc Operator push

---

### 5. Verification

Updating a device in NetBox should now trigger the webhook. Verify this with the following commands:

```bash
kubectl get targets
kubectl get targets <targetname> -o yaml

# Check logs of incoming POST requests:
kubectl logs -n gnmic-system deploy/gnmic-controller-manager -f
```

Every incoming POST request is logged, including rejected requests. If no POST requests appear in the logs, the webhook request is not reaching the gNMIc Operator.

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
  targetPort: 57400
  targetProfile: netbox-device
  targetLabels:
    inventory: netbox
    sync-source: rest-api
  provider:
    http:
      push:
        enabled: true
        auth:
          bearer:
            tokenSecretRef:
              name: gnmic-api-auth
              key: bearer-token
        signature:
          secretRef:
            name: gnmic-signature
            key: signature
```
