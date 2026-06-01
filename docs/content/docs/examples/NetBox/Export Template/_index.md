---
title: "NetBox (Export Template)"
linkTitle: "NetBox Export"
weight: 3
description: >
  Discover targets from NetBox using HTTP provider with export templates
---

This guide shows how to use **NetBox Export Templates** with the HTTP provider to discover and sync targets.

Export Templates offer powerful filtering, transformation, and formatting directly in NetBox, reducing load on the operator and enabling complex discovery logic.

## Overview

An **Export Template** is a Jinja2 template defined in NetBox that:

1. **Queries** NetBox's internal database (devices, interfaces, etc.)
2. **Filters** results based on custom criteria
3. **Transforms** data into your desired output format (JSON, YAML, CSV, etc.)
4. **Returns** the formatted output via a custom REST API endpoint

When used with gNMIc's HTTP provider, the operator simply fetches the rendered template and parses the result — no additional transformation needed.

---

## Prerequisites

- A running Kubernetes cluster with gNMIc Operator installed
- A reachable NetBox instance with **permissions to create Export Templates**
- A NetBox API token
- `kubectl` access to your cluster
- Familiarity with Jinja2 templates

---

## Step 1: Create a Secret for the NetBox API Token

Create a Kubernetes Secret containing your NetBox API token. This keeps credentials secure and out of your TargetSource manifests.

```bash
# Substitute YOUR_NETBOX_TOKEN with your actual token
kubectl create secret generic netbox-api-token \
  --from-literal=token=YOUR_NETBOX_TOKEN \
  -n your-namespace
```

Verify the Secret was created:

```bash
kubectl get secret netbox-api-token -n your-namespace -o yaml
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

#### Basic Template (All Devices)

```jinja2
{
  "targets": [
    {% for device in queryset %}
    {
      "name": "{{ device.name }}",
      "address": "{{ device.primary_ip4.address.split('/')[0] }}:57400",
      "labels": {
        "site": "{{ device.site.name }}",
        "role": "{{ device.device_role.name }}",
        "region": "{{ device.site.region.name }}",
        "type": "{{ device.device_type.model }}"
      }
    }{{ "," if not loop.last }}
    {% endfor %}
  ]
}
```

#### Advanced Template (Filtered by Status and Role)

```jinja2
{
  "targets": [
    {% for device in queryset.filter(status='active', device_role__name__in=['leaf', 'spine']) %}
    {
      "name": "{{ device.name }}",
      "address": "{{ device.primary_ip4.address.split('/')[0] }}:57400",
      "labels": {
        "site": "{{ device.site.name }}",
        "role": "{{ device.device_role.name }}",
        "region": "{{ device.site.region.name }}",
        "model": "{{ device.device_type.model }}",
        "serial": "{{ device.serial }}",
        "asset_tag": "{{ device.asset_tag }}"
      }
    }{{ "," if not loop.last }}
    {% endfor %}
  ]
}
```

**Key template elements:**

- `queryset`: The filtered set of devices (all unless you add `.filter()`)
- `device.name`: Device hostname
- `device.primary_ip4.address.split('/')[0]`: Extract IP from CIDR (e.g., `192.0.2.1/24` to `192.0.2.1`)
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
curl -H "Authorization: Token YOUR_NETBOX_TOKEN" \
  "http://netbox.example.com:8000/api/dcim/devices/?export=gNMIc%20Device%20Export"
```

The response is a JSON array of targets ready for gNMIc.

---

## Step 3: Create a TargetProfile

Define how discovered targets should be configured: <!-- todo: reference to the targetprofile section to see more, also explain create credentials -->

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: netbox-devices
  namespace: your-namespace
spec:
  credentialsRef: device-credentials
  timeout: 10s
```

---

## Step 4: Create a TargetSource Using Export Template

Create a `TargetSource` that references your NetBox export template endpoint:

```yaml
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: netbox-export-source
  namespace: your-namespace
spec:
  # Specify the HTTP provider
  provider:
    http:
      # NetBox API endpoint with export template query
      # Replace: netbox.example.com, template name (gNMIc+Device+Export), token
      url: "http://netbox.example.com:8000/api/dcim/devices/?export=gNMIc%20Device%20Export"

      # Do not embed plaintext tokens in the TargetSource YAML. Instead, use a secret reference:
      token:
          scheme: Bearer
          tokenSecretRef:
            name: netbox-api-token
            key: token

  
  # Reference the TargetProfile
  targetProfile: netbox-device
  
  # Optional: Apply labels to all discovered targets
  targetLabels:
    inventory: netbox
    sync-source: export-template
```

---

## Step 5: Verify Target Discovery

Once the `TargetSource` is deployed, verify that targets are being discovered:

```bash
# List discovered targets
kubectl get targets -n your-namespace

# Check TargetSource status and sync details
kubectl describe ts netbox-export-source -n your-namespace
```

Successful sync shows:

- `status.status`: "success" <!-- todo: check this status -->
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
  namespace: gnmic
type: Opaque
data:
  # base64-encoded token (echo -n "YOUR_TOKEN" | base64)
  token: YOUR_BASE64_ENCODED_TOKEN

---
# TargetProfile for NetBox devices
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetProfile
metadata:
  name: netbox-device
  namespace: gnmic
spec:
  credentialsRef: device-credentials
  timeout: 10s

---
# TargetSource using Export Template
apiVersion: operator.gnmic.dev/v1alpha1
kind: TargetSource
metadata:
  name: netbox-export-source
  namespace: gnmic
spec:
  provider:
    http:
      url: "http://netbox.example.com:8000/api/dcim/devices/?export=gNMIc%20Device%20Export"
  targetProfile: netbox-device
  targetLabels:
    inventory: netbox
    sync-source: export-template

---
# Device Credentials (referenced by TargetProfile)
apiVersion: v1
kind: Secret
metadata:
  name: device-credentials
  namespace: gnmic
type: Opaque
data:
  username: Z25taWM=           # base64: gnmic
  password: Z25taaWNQYXNz=     # base64: gnmicPass
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
- **Solution**: Keep templates simple and use NetBox's built-in filters and objects. Test in the NetBox UI before deploying.

---

## Template Troubleshooting

### Missing Data in Output

- **Check**: Are all required fields populated in NetBox? (e.g., `primary_ip4` may be `None` if not set)
- **Solution**: Add conditional checks:
  ```jinja2
  {% if device.primary_ip4 %}
    "address": "{{ device.primary_ip4.address.split('/')[0] }}"
  {% endif %}
  ```

### Authorization Fails

If you get a 403 error:

- Verify the token is valid and not expired.
- Check token permissions in NetBox admin (User > API Tokens).
- Ensure the API token is enabled.

---
