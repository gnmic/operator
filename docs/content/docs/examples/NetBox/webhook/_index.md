---
title: "Real-time target update with webhook"
linkTitle: "Real-time target update with webhook"
weight: 2
description: >
  Configure a webhook in Netbox to update targets in the gNMIc Operator real-time.
---

## Netbox Webhook Configuration

This example will run you through the configuration of a webhook in Netbox. This allows for real-time target updates from Netbox into the gNMIc Operator. The configuration steps are:

1. Apply TargetSource
2. Create Kubernetes Secrets
3. Configure Webhook
4. Create Event Rule
5. Verification

### Prerequisites

- Kubernetes cluster with gNMIc Operator installed
- kubectl access to your cluster
- Running Netbox instance
- Netbox can send HTTP requests to the gNMIc Operator

### 1. Apply TargetSource

Apply the targetSource with `kubectl apply -f netbox.yaml -n default`.

- `enabled` set to `true`, otherwise target updates are rejected
- Bearer authentication and Signature activated

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

### 2. Create Kubernetes Secrets

Authentication and the signature both require a Kubernetes secret. These must:

- Be in the same namespace as the TargetSource, in this case `default`.
- `Name` and `Key` align with the TargetSource spec.

```bash
kubectl create secret generic gnmic-api-auth --from-literal=bearer-token=thisIsASecureToken -n default
kubectl create secret generic gnmic-signature --from-literal=signature=SecretSignature -n default
```

### 3. Configure Webhook

Now we switch to Netbox and configure a webhook. The webhook gets triggered by events like `device update` and sends a HTTP POST request to the gNMIc Operator.  

Configure the Webhook under `Operations > Webhooks` and create a new Webhook with the following settings:

- *Name*: GNMIc operator push
- *URL*: `http://gnmic-controller-manager-api.gnmic-system.svc.cluster.local:8082/api/v1/default/target-source/netbox/applyTargets`
  - The cluster-address might be `http://localhost:8082/` or `http://servername:8082/`, depending on your setup.
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

The webhook just created needs a trigger, which is created as an event rule under `Operations > Event Rules`.

- *Name*: gNMIc Operator push target change
- *Object types*: DCIM > Device
- *Event types*: "Object Created", "Object Updated", "Object Deleted"
- *Action type*: Webhook
- *Webhook*: gNMIc Operator push

### 5. Verification

Updating a device in Netbox will now trigger the webhook, verify this with these commands:

```bash
kubectl get targets
kubectl get targets <targetname> -o yaml

# Check logs of incoming POST requests:
kubectl logs -n gnmic-system deploy/gnmic-controller-manager -f
```

Every POST request received will write logs, even if rejected. If no POST request are being logged, the request is not received.
