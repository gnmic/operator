---
title: "Pull with Export Template"
linkTitle: "Pull with Export Template"
weight: 1
description: >
  Discover targets from NetBox using HTTP provider with NetBox Export Template
---

This guide shows how to use **NetBox Export Templates** with the HTTP provider to discover and sync targets.

Export Templates offer powerful filtering, transformation, and formatting directly in NetBox, reducing the load on the operator.

## Overview

An **Export Template** is a Jinja2 template that:

1. **Queries** NetBox's internal database (devices, interfaces, etc.)
2. **Filters** results based on custom criteria
3. **Transforms** data into your desired output format
4. **Returns** the formatted output via REST API endpoint

When used with gNMIc's HTTP provider, the operator fetches the rendered JSON template and parses the result with no further transformation needed by the gNMIc Operator.

---

## Prerequisites

- A running Kubernetes cluster with gNMIc Operator installed
- `kubectl` access to your cluster
- A reachable NetBox instance with permissions to create Export Templates
- A NetBox API token
- Familiarity with Jinja2 templates

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

Create a [Kubernetes Secret](https://kubernetes.io/docs/concepts/configuration/secret/) containing the token.

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

## Step 2: Create an Export Template in NetBox

Log in to your NetBox instance and navigate to **Customization > Export Templates**.

### Step 2a: Create a New Template

Click **Add Export Template** and fill in the details:

| Field | Value | Notes |
|-------|-------|-------|
| **Name** | `gNMIc Device Export` | Descriptive name for your template |
| **Content Type** | `dcim > device` | Export template applies to Device objects |
| **Template Code** | (see below) | Jinja2 template |
| **File Extension** | `json` | Output format |
| **Mime Type** | `application/json` | Correct MIME type for JSON |

### Step 2b: Template Code Example

The following Export Templates only work for devices that have a primary IPv4 address set in NetBox. If primary_ip4 is missing, the expression returns '', so those devices will not yield a valid target address. For NetBox data model details, see the [NetBox Devices Data Model](https://netboxlabs.com/docs/netbox/models/dcim/device/) documentation.

See the HTTP provider's "Default Response Format" section for the expected JSON structure: [HTTP provider docs](../../user-guide/targetsource/providers/http.md)

#### Basic Template (All Devices)

```jinja2
[
  {% for device in queryset %}
  {
    "name": "{{ device.name }}",
    "address": "{{ device.primary_ip4.address.ip }}",
    "labels": {
      "site": "{{ device.site.name }}",
      "role": "{{ device.role.name }}",
      "region": "{{ device.site.region.name }}",
      "type": "{{ device.device_type.model }}"
    }
  }{{ "," if not loop.last }}
  {% endfor %}
]
```

#### Advanced Template (Filtered by Status and Role)

```jinja2
[
  {% for device in queryset.filter(status='active', role__name__in=['leaf', 'spine']) %}
  {
    "name": "{{ device.name }}",
    "address": "{{ device.primary_ip4.address.ip }}",
    "labels": {
      "site": "{{ device.site.name }}",
      "role": "{{ device.role.name }}",
      "region": "{{ device.site.region.name }}",
      "model": "{{ device.device_type.model }}",
      "serial": "{{ device.serial }}",
      "asset_tag": "{{ device.asset_tag }}"
    }
  }{{ "," if not loop.last }}
  {% endfor %}
]
```

**Key template elements:**

- `queryset`: The filtered set of devices (all unless you add `.filter()`)
- `device.name`: Device hostname
- `device.primary_ip4.address.ip`: Primary IPv4 address
- `device.site.name`, `device.device_role.name`: NetBox relationships (site, role, etc.)
- `loop.last`: Jinja2 loop variable to avoid trailing comma on last item

### Step 2c: Save and Access the Template

Once saved, NetBox exposes the template via:

```
http://netbox.example.com:8000/api/dcim/devices/?export=gNMIc+Device+Export
```

Or fetch it directly:

```bash
# Replace with your NetBox URL and template name
# Substitute YOUR_NETBOX_API_TOKEN with your actual token
# Bearer Token Format (v2): nbt_<key>.<token>
curl -H "Authorization: Bearer YOUR_NETBOX_API_TOKEN" \
  "http://netbox.example.com:8000/api/dcim/devices/?export=gNMIc%20Device%20Export"
```

The response should be a JSON array of targets ready for the gNMIc Operator.

Sample JSON output produced by the basic export template:

```json
[
  
  {
    "name": "edge-rtr-01.dc1.example.com",
    "address": "203.0.113.1",
    "labels": {
      "site": "DC1",
      "role": "edge",
      "region": "eu-central-1",
      "type": "router"
    }
  },

]
```

> Ensure the response is valid JSON and contains no hidden or invalid characters, otherwise the gNMIc Operator will fail to parse it.

> If you instead return a JSON object with a nested array, add a mapping section such as `targetsField: "self.targets"` to the TargetSource CR.

---

## Step 3: Create a TargetProfile

Define how discovered targets should be configured. The `TargetProfile` points to a [Kubernetes Secret](https://kubernetes.io/docs/concepts/configuration/secret/) containing device credentials, such as username/password or client certificates.

Create a credentials Secret first, then reference it from the TargetProfile.

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

## Step 4: Create a TargetSource Using Export Template

Create a `TargetSource` that references your NetBox export template endpoint:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: netbox-export-source
  namespace: gnmic-system
spec:
  targetPort: 57400
  targetProfile: netbox-device
  targetLabels:
    inventory: netbox
    sync-source: export-template
  provider:
    http:
      url: "http://netbox.example.com:8000/api/dcim/devices/?export=gNMIc%20Device%20Export"
      method: GET
      interval: 30m
      timeout: 30s
      authentication:
        token:
          scheme: Token
          tokenSecretRef:
            name: netbox-api-token
            key: token
```

---

## Step 5: Verify Target Discovery

Once the `TargetSource` is deployed, verify that targets are being discovered:

```bash
# List discovered targets
kubectl get targets -n gnmic-system

# Check TargetSource status and sync details
kubectl describe targetsource netbox-export-source -n gnmic-system
```

Successful sync shows:

- `status.status`: "success" (or similar) <!-- todo: to be verivied -->
- `status.targetsCount`: number of devices
- `status.lastSync`: recent timestamp

---

## Example: Complete Setup

Here's a full example combining all components:

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
# TargetSource using Export Template
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: netbox-export-source
  namespace: gnmic-system
spec:
  targetPort: 57400
  targetProfile: netbox-device
  targetLabels:
    inventory: netbox
    sync-source: export-template
  provider:
    http:
      url: "http://netbox.example.com:8000/api/dcim/devices/?export=gNMIc%20Device%20Export"
      method: GET
      interval: 30m
      timeout: 30s
      authentication:
        token:
          scheme: Token
          tokenSecretRef:
            name: netbox-api-token
            key: token
```

---

## Advantages of Export Templates

- **Powerful Filtering**: Filter devices by site, status, role, tags, etc. directly in NetBox  
- **Reduced Operator Load**: NetBox handles data transformation; operator just fetches JSON  
- **Reusability**: One template can serve multiple consumers  
- **Maintainability**: Update discovery logic in NetBox without changing Kubernetes manifests  
- **Performance**: Avoids REST API pagination for large inventories  

---

## Limitations & Considerations

### 1. Reverse Proxy and URL Path Rewriting

If NetBox is behind a reverse proxy with URL path rewriting:

- **Issue**: The export template endpoint uses query parameters that may not survive proxy transformation.
- **Solution**: 
  - Ensure the proxy preserves query strings exactly.
  - Test the export URL directly:
    ```bash
    curl -H "Authorization: Token YOUR_TOKEN" \
      "http://netbox.example.com:8000/api/dcim/devices/?export=gNMIc%20Device%20Export"
    ```
  - If the proxy blocks or modifies parameters, consider using a direct NetBox endpoint without proxying.

### 2. Large Inventory Rendering

- Very large device counts can cause NetBox to take time rendering the template.
- **Solution**:
  - Use `.filter()` in your template to limit results.
  - Create separate export templates for different device groups (e.g., by site or role).

### 3. Complex Jinja2 Logic

- NetBox's Jinja2 sandbox restricts some Python functions for security.
- **Solution**: Keep templates simple and use NetBox's built-in filters and objects. Test the URL with curl or similar before deploying.

---

## Template Troubleshooting

### Missing Targets in Kubernetes

- **Check**: Are all required fields populated in NetBox? (e.g., `primary_ip4` may be `None` if not set)
- **Solution**: Add conditional checks:
  ```jinja2
  {% if device.primary_ip4 %}
    "address": "{{ device.primary_ip4.address.ip }}"
  {% endif %}
  ```

### Authorization Fails

If you get a 403 error:

- Verify the token is valid and not expired.
- Ensure the API token is enabled.

---
