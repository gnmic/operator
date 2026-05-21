# DefaultApi

All URIs are relative to *http://localhost*

| Method | HTTP request | Description |
|------------- | ------------- | -------------|
| [**applyTargets**](DefaultApi.md#applyTargets) | **POST** /api/v1/:namespace/target-source/:name/applyTargets | Targets received in body are applied in gNMIc Operator. |
| [**getClusterPlan**](DefaultApi.md#getClusterPlan) | **GET** /clusters/:namespace/:name/plan | Get cluster plan. |


<a name="applyTargets"></a>
# **applyTargets**
> List applyTargets(Target)

Targets received in body are applied in gNMIc Operator.

### Parameters

|Name | Type | Description  | Notes |
|------------- | ------------- | ------------- | -------------|
| **Target** | [**List**](../Models/Target.md)|  | |

### Return type

[**List**](../Models/Target.md)

### Authorization

[bearerAuth](../README.md#bearerAuth)

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

