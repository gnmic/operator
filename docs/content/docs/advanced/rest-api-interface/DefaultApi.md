---
title: "Routes"
linkTitle: "Routes"
weight: 4
description: >
  Available HTTP routes on the gNMIc Operator API interface.
---

# defaultapi

All URIs are relative to *http://localhost:8082*

| Method | HTTP request | Description |
|------------- | ------------- | -------------|
| **getClusterPlan** | **GET** /clusters/:namespace/:name/plan | Get cluster plan. |
| **refreshTargetSource** | **POST** /api/v1/namespaces/:namespace/targetsources/:name/refresh | Request an immediate discovery run for a TargetSource. |


<a name="getClusterPlan"></a>
# **getClusterPlan**
> getClusterPlan()

Get cluster plan.

    Returns the configuration most recently built for the Cluster's collectors. Credentials are masked: each target's password and token read as `****`. Everything else is served as built.

### Parameters
This endpoint does not need any parameter.

### Return type

null (empty response body)

### Authorization

For authorization details refer to [TargetSource > Webhook](/docs/user-guide/targetsource/webhook/).

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: Not defined

<a name="refreshTargetSource"></a>
# **refreshTargetSource**
> RefreshResponse refreshTargetSource(body)

Request an immediate discovery run for a TargetSource.

    Notify-only. The body carries no target data; it is only an input to signature verification. A successful call annotates the TargetSource and the controller performs an ordinary full run. Calls within the TargetSource&#39;s webhook.debounce window of the previous one are acknowledged without scheduling another run. 

### Parameters

|Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **body** | **File** | Opaque. Forwarded to HMAC verification when signature auth is configured. | [optional] |

### Return type

**RefreshResponse**

### Authorization

For authorization details refer to [TargetSource > Webhook](/docs/user-guide/targetsource/webhook/).

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: application/json

