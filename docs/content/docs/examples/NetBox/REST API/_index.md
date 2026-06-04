---
title: "NetBox (REST API)"
linkTitle: "NetBox REST"
weight: 2
description: >
  Discover targets from NetBox using the HTTP provider and NetBox REST API
---

This guide shows how to configure the HTTP provider to discover targets from NetBox using its REST API.

The REST API approach is direct and straightforward — query NetBox's standard API endpoints to retrieve devices that match your criteria.

## Prerequisites

- A running Kubernetes cluster with gNMIc Operator installed
- `kubectl` access to your cluster
- A reachable NetBox instance (inside or outside the cluster)
- A NetBox API token

## Overview

The HTTP `TargetSource` loader performs these steps:

1. **Fetch** JSON device data from a NetBox REST API endpoint (`/api/dcim/devices/`)
2. **Transform** each device record into a gNMIc target using CEL expressions
3. **Create** or **update** `Target` resources in Kubernetes with the extracted data

---

## Step 1: Create a NetBox API Token and Store It Securely

### Step 1a: Create the API Token in NetBox

Create a dedicated API token in NetBox for gNMIc Operator access.

1. Log in to NetBox.
2. Open your user profile or go to **User > API Tokens**.
3. Click **Add** or **Add token**.
4. Enter a descriptive name such as `gNMIc Operator`.
5. Grant the minimum permissions required for read-only device discovery.
6. Copy the token value and store it safely; NetBox will not show it again.

### Step 1b: Store the Token in a Kubernetes Secret

Create a [Kubernetes Secret](https://kubernetes.io/docs/concepts/configuration/secret/) containing the token so it is not embedded in manifests.

```bash
# Substitute YOUR_NETBOX_API_TOKEN with your actual token
# Bearer Token Format (v2): nbt_<key>.<token>
kubectl create secret generic netbox-api-token \
  --from-literal=token=YOUR_NETBOX_API_TOKEN \
  -n gnmic-system
```

Verify the Secret was created:

```bash
kubectl get secret netbox-api-token -n gnmic-system -o yaml
```

---

## Step 2: Create a TargetProfile

Define how discovered targets should be configured. The `TargetProfile` points to a [Kubernetes Secret](https://kubernetes.io/docs/concepts/configuration/secret/) containing device credentials, such as username/password or client certificates.

Create a credentials Secret first, then reference it from the profile.

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

## Step 3: Create a TargetSource Using REST API

The following `TargetSource` queries NetBox's REST API to discover devices:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: netbox-rest-source
  namespace: gnmic-system
spec:
  targetPort: 57400
  targetProfile: netbox-device
  targetLabels:
    inventory: netbox
    sync-source: rest-api
  provider:
    http:
      url: "http://netbox.example.com:8000/api/dcim/devices/?limit=1000"
      method: GET
      interval: 5m
      timeout: 30s
      authentication:
        token:
          scheme: Bearer
          tokenSecretRef:
            name: netbox-api-token
            key: token
      pagination:
        nextField: "next"
      mapping:
        targetsField: "self.results"
        address: "item.primary_ip4 != null ? item.primary_ip4.address.split('/')[0] : ''"
        labels: |
          {
            "site": item.site.name,
            "role": item.device_role.name,
            "model": item.device_type.model,
            "status": item.status.value
          }
```

> This mapping only works for devices that have a primary IPv4 address set in NetBox. If primary_ip4 is missing, the expression returns '', so those devices will not yield a valid target address. For NetBox API details, see the [NetBox REST API](https://netboxlabs.com/docs/netbox/integrations/rest-api/) documentation.

The HTTP loader supports `targetsField` and individual CEL expressions for `name`, `address`, `port`, `labels`, and `targetProfile`. See the HTTP Provider docs "Response Mapping via CEL" section for more details: [HTTP provider docs](../../user-guide/targetsource/providers/http.md)

Use `self` for the full response and `item` for each candidate object.

---

## Step 4: Apply and Verify Target Discovery

Deploy the `TargetSource` and check that targets are being discovered and synced:

```bash
# List discovered targets
kubectl apply -f /path/to/targetsource.yaml -n gnmic-system

# List discovered targets
kubectl get targets -n gnmic-system

# Check TargetSource status
kubectl describe targetsource netbox-rest-source -n gnmic-system
```

Look for:
- `status.status`: "success" (or similar) <!-- todo: to be verivied -->
- `status.targetsCount`: number of discovered devices
- `status.lastSync`: recent timestamp

---

## Example: Complete Setup

Here's a complete example combining all resources:

```yaml
---
# Secret for NetBox API token
apiVersion: v1
kind: Secret
metadata:
  name: netbox-api-token
  namespace: gnmic-system
type: Opaque
data:
  # base64-encoded token (echo -n "YOUR_TOKEN" | base64)
  token: YOUR_BASE64_ENCODED_TOKEN

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
# TargetSource with REST API
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: netbox-rest-source
  namespace: gnmic-system
spec:
  targetPort: 57400
  targetProfile: netbox-device
  targetLabels:
    inventory: netbox
    sync-source: rest-api
  provider:
    http:
      url: "http://netbox.example.com:8000/api/dcim/devices/?limit=1000"
      method: GET
      interval: 5m
      timeout: 30s
      authentication:
        token:
          scheme: Bearer
          tokenSecretRef:
            name: netbox-api-token
            key: token
      pagination:
        nextField: "next"
      mapping:
        targetsField: "self.results"
        address: "item.primary_ip4 != null ? item.primary_ip4.address.split('/')[0] : ''"
        labels: |
          {
            "site": item.site.name,
            "role": item.device_role.name,
            "model": item.device_type.model,
            "status": item.status.value
          }
```

---

## Performance Considerations & Limitations

### REST API Query Limits

- **Query Size**: The example uses `limit=1000`. Adjust based on your NetBox instance's pagination settings and response size limits.
- **Response Timeout**: Large device lists can take time. Set appropriate timeouts in your `TargetSource`.

### Reverse Proxy Considerations

If NetBox is behind a reverse proxy:

- **Base URL**: Ensure the reverse proxy correctly handles the `/api/dcim/devices/` path.
- **Authentication**: Some proxies may require additional headers; verify with your proxy and NetBox admin.
- **HTTPS**: If using HTTPS, ensure certificates are trusted by the operator or else use the `tls` setting.

### Large Inventories

For inventories with thousands of devices:

- Consider using **Export Templates** (see [NetBox Export Templates]({{< relref "../Export Template" >}})) for better filtering and performance.
- Implement filtering in the REST API URL (e.g., `?site=us-west&status=active`).

---

## Security Considerations

### Token and Credentials

- **Never** embed plaintext tokens or credentials in manifests or YAML files.
- Always store tokens in Kubernetes Secrets.
- Restrict RBAC permissions on the Secret to only necessary service accounts.

### HTTPS and Certificates

If connecting to NetBox via HTTPS:

- Ensure cluster DNS resolves the hostname correctly.
- Mount CA certificates if using self-signed certificates.
- Verify the operator's HTTP client configuration for certificate validation.

---

## Troubleshooting

### Show TargetSource Errors

```bash
kubectl describe targetsource netbox-rest-source -n gnmic-system
```

### Targets Not Appearing

- Check that the `TargetProfile` exists and is correctly referenced.
- Verify labels and addresses are being extracted correctly from the NetBox response.
- Review operator logs for parsing errors:
  ```bash
  kubectl logs -l app=gnmic-operator -n gnmic-operator-system
  ```

### Rate Limiting or Timeouts

Increase the sync interval in your `TargetSource` or adjust timeouts:

```yaml
spec:
  provider:
    http:
      interval: 1h
      timeout: 1m
```
