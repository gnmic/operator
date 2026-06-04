---
title: "NetBox (Export Template)"
linkTitle: "NetBox Export Template"
weight: 1
description: >
  Discover targets from NetBox using HTTP provider with NetBox Export Template
---

This guide shows how to use **NetBox Export Templates** with the HTTP provider to discover and sync targets.

Export Templates offer powerful filtering, transformation, and formatting directly in NetBox, reducing the load on the operator.

## Overview

An **Export Template** is a Jinja2 template defined in NetBox that:

1. **Queries** NetBox's internal database (devices, interfaces, etc.)
2. **Filters** results based on custom criteria
3. **Transforms** data into your desired output format (JSON, YAML, CSV, etc.)
4. **Returns** the formatted output via a custom REST API endpoint

When used with gNMIc's HTTP provider, the operator simply fetches the rendered JSON template and parses the result — no additional gNMIc Operator transformation needed if done correctly.

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
