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
    http: # can be changed to a differnet TargetSourceProvider
      push:
        enabled: true
```

> `http` is currently the only TargetSourceProvider implemented, once others are added they can be used instead. Push mode is not coupled to a specific TargetSourceProvider implementation.

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

- `cluster-address`: Address of your cluster.
- `:namespace`: Namespace the TargetSource is created in.
- `:name`: Name of the TargetSource.

See [Push mode with webhook](/docs/examples/netbox/webhook) for an example on how to configure the URL.

### Cluster Address

The cluster address depends on where the API is accessed from.

- Use `http://<server-fqdn>:8082/` when accessing the API from outside the cluster.
- Use `http://localhost:8082/` for local development (requires port-forwarding).
- Use `gnmic-controller-manager-api.gnmic-system.svc.cluster.local` when NetBox (or another source of truth) runs in the same cluster.
- If you use a reverse proxy, run `kubectl get service -n <gnmic-controller-namespace>` and use the returned service address and port in your proxy configuration.

## REST API

Refer to the [REST API documentation](/docs/advanced/rest-api-documentation/) for the expected request schema and payload format.

Any system or script capable of sending HTTP POST requests can integrate with this interface. 

## Security

The API supports Bearer Token authentication and X-Hook-Signature, both are optional and **turned off by default**. They are enabled by adding them to the specification. They can also be used in combination.

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
- Example: `kubectl create secret generic gnmic-api-auth --from-literal=bearer-token=YOUR_SECRET_TOKEN`

#### Authorization Header

HTTP request must contain the Bearer token in the header in the format:

```yaml
Authorization: Bearer YOUR_SECRET_TOKEN
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
