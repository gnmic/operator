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
| **applyTargets** | **POST** /api/v1/:namespace/target-source/:name/applyTargets | Interface for real-time target updates, usually using a webhook. Targets are applied in the gNMIc Operator. |
| **getClusterPlan** | **GET** /clusters/:namespace/:name/plan | Get cluster plan. |


<a name="applyTargets"></a>
# **applyTargets**
> List applyTargets(Target)

Interface for real-time target updates, usually using a webhook. Targets are applied in the gNMIc Operator.

### Parameters

|Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **Target** | **List** | Target must be passed as a list, multiple targets possible. | |

### Return type

**List**

### Authorization

For authorization details refer to [TargetSource > Push mode](/docs/user-guide/targetsource/push/).

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

For authorization details refer to [TargetSource > Push mode](/docs/user-guide/targetsource/push/).

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: Not defined

