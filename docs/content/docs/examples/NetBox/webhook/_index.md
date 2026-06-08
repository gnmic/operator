---
title: "Push mode with webhook"
linkTitle: "Push mode with webhook"
weight: 2
description: >
  Configure a webhook in NetBox to update targets in the gNMIc Operator in real time.
---

## Netbox Webhook Configuration

This tutorial walks through configuring a webhook in NetBox to push real-time target updates to the gNMIc Operator. The workflow includes the following steps:

1. Apply TargetSource
2. Create Kubernetes Secrets
3. Configure Webhook
4. Create Event Rule
5. Verification

### Prerequisites

- Kubernetes cluster with gNMIc Operator installed
- `kubectl` access to your cluster
- Running NetBox instance
- Network connectivity from NetBox to the gNMIc Operator API endpoint

---

### 1. Apply TargetSource

Apply the TargetSource manifest: `kubectl apply -f netbox.yaml -n default`

- `enabled` must be set to `true`, otherwise updates are rejected.
- Bearer authentication and signature verification are enabled.

```yaml
# netbox.yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: netbox
spec:
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
  targetLabels:
    integrationtest: http
  targetProfile: default
```

> Namespace is `default`, the name of the TargetSource is `netbox`. These values will be in the URL in step 3.

### 2. Create Kubernetes Secrets

Bearer authentication and signature verification both require Kubernetes secrets. Ensure that the secrets:

- Are created in the same namespace as the TargetSource (`default` in this example).
- Use `name` and `key` values that match the TargetSource spec.

```bash
kubectl create secret generic gnmic-api-auth --from-literal=bearer-token=thisIsASecureToken -n default
kubectl create secret generic gnmic-signature --from-literal=signature=SecretSignature -n default
```

### 3. Configure Webhook

Next, configure a webhook in NetBox. The webhook is triggered by device events (for example, updates) and sends an HTTP POST request to the gNMIc Operator.

In NetBox, go to `Operations > Webhooks` and create a webhook with the following settings:

- *Name*: GNMIc operator push
- *URL*: `http://gnmic-controller-manager-api.gnmic-system.svc.cluster.local:8082/api/v1/default/target-source/netbox/applyTargets`
  - Depending on your environment, the cluster address may instead be `http://localhost:8082/` or `http://servername:8082/`.
  - URL contains the namespace `default` and TargetSource name `netbox`.
- *HTTP method*: POST
- *HTTP content type*: application/json
- *SSL Verification*: true
- *Additional headers:* `Authorization: Bearer thisIsASecureToken`
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
        {
          "key": "vendor",
          "value": "{{ data.device_type.manufacturer.name }}"
        }
      ]
    }
  ]
  ```

- *Secret*: `SecretSignature`

### 4. Create Event Rule

The webhook requires a trigger, configured as an event rule under `Operations > Event Rules`.

- *Name*: gNMIc Operator push target change
- *Object types*: DCIM > Device
- *Event types*: "Object Created", "Object Updated", "Object Deleted"
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
