---
title: "Push mode"
linkTitle: "Push mode"
weight: 4
description: >
  Enables REST API interface that accepts real-time target updates.
---

## Basic configuration

This CR enables the push interface with no authentication.

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: targetsource-1
spec:
  provider:
    http:
      push:
        enabled: true
```

## Spec Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `push` | object | No | - | Push interface config |
| `enabled` | bool | Yes | False | Whether the push interface is active |
| `auth` | object | No | - | Bearer token authentication |
| `signature` | object | No | - | HTTP body verification using HMAC |
| `algorithm` | string | No | sha512 | Algorithm for signature verification(`sha256`or`sha512`) |

## Address

The REST API endpoint runs on `http://cluster-address:8082/api/v1/:namespace/target-source/:name/applyTargets`.

- `cluster-address`: Adress of your cluster, localhost during development.
- `:namespace`: Namespace the TargetSource is created in.
- `:name`: Name of the TargetSource.

See [Push mode with webhook](/docs/examples/netbox/webhook) for an example on how to configure the URL.

## REST API

Refer to the [REST API documentation](/docs/advanced/rest-api-documentation/) for the expected request schema and payload format. 

Any system or script capable of sending HTTP POST requests can integrate with this interface. 

## Security

The API supports Bearer Token authentication and X-Hook-Signature, both are optional and turned off by default. They can be used in combination and are enabled by adding them to the specification.

An example configuration of both is documented in the [Netbox webhook](/docs/examples/netbox/webhook) example.

### Bearer Authentication

Bearer authentication compares a token stored in Kubernetes with the one sent in the HTTP header. The Kubernetes secret is referenced as `tokenSecretRef`.

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: targetsource-1
spec:
  provider:
    http:
      push:
        enabled: true
        auth:
          bearer:
            tokenSecretRef:
              name: gnmic-api-auth # secret name
              key: bearer-token # secret key
```

This requires the [creation](https://kubernetes.ltd/docs/reference/kubectl/generated/kubectl_create/kubectl_create_secret_generic/) of an Opaque Kuberentes secret:

- Must be in the same namespace the gNMIc controller runs in.
- `name`: refers to the secret name
- `key`: key of the secret
- Example: `kubectl create secret generic gnmic-api-auth --from-literal=bearer-token=Secret...`

#### Authorization Header

HTTP request must contain the Bearer token in the header in the format:

```yaml
Authorization: Bearer Secret...
```

### Signature

Signature verification requires an Opaque Kubernetes secret that stores the shared key (see Bearer Authentication). For each request, the HMAC generated from the request body and shared key must be provided in the `X-Hook-Signature` header.

```yaml
spec:
  provider:
    http:
      push:
        enabled: true
        auth:
        signature:
          algorithm: sha512
          secretRef:
            name: gnmic-signature
            key: signature
```

#### Reverse Proxy

In order to have a secure setup, the HTTP post requests must be sent using TLS. The REST API interface does not support HTTPS, at least not directly. It is recommended to terminate the TLS connection at the reverse proxy and forward a plain HTTP request to the gNMIc Operator.
