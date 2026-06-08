---
title: "REST API definition"
linkTitle: "REST API definition"
weight: 4
description: >
  Gives REST API definition with available options.
---

# DefaultApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|------------- | ------------- | -------------|
| [**applyTargets**](DefaultApi.md#applyTargets) | **POST** /api/v1/:namespace/target-source/:name/applyTargets | Interface for real-time target updates, usually using a webhook. Targets are applied in the gNMIc Operator. |
| [**getClusterPlan**](DefaultApi.md#getClusterPlan) | **GET** /clusters/:namespace/:name/plan | Get cluster plan. |


<a name="applyTargets"></a>
# **applyTargets**
> List applyTargets(Target)

Interface for real-time target updates, usually using a webhook. Targets are applied in the gNMIc Operator.

### Parameters

|Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **Target** | [**List**](/docs/advanced/rest-api-documentation/Models/Target/)|  | |

### Return type

[**List**](/docs/advanced/rest-api-documentation/Models/Target/)

### Authorization

[bearerAuth](/docs/advanced/rest-api-documentation/#bearerAuth)

### HTTP request headers

- **Content-Type**: application/json
- **Accept**: application/json

<a name="getClusterPlan"></a>
# **getClusterPlan**
> getClusterPlan()

Get cluster plan.

### Parameters
This endpoint does not need any parameter.

### Return type

null (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: Not defined

