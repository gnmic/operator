# Documentation for gNMIc Operator REST API

<a name="documentation-for-api-endpoints"></a>
## Documentation for API Endpoints

All URIs are relative to *http://localhost*

| Class | Method | HTTP request | Description |
|------------ | ------------- | ------------- | -------------|
| *DefaultApi* | [**applyTargets**](Apis/DefaultApi.md#applyTargets) | **POST** /api/v1/:namespace/target-source/:name/applyTargets | Targets received in body are applied in gNMIc Operator. |
*DefaultApi* | [**getClusterPlan**](Apis/DefaultApi.md#getClusterPlan) | **GET** /clusters/:namespace/:name/plan | Get cluster plan. |


<a name="documentation-for-models"></a>
## Documentation for Models

 - [Label](./Models/Label.md)
 - [Target](./Models/Target.md)


<a name="documentation-for-authorization"></a>
## Documentation for Authorization

<a name="bearerAuth"></a>
### bearerAuth

- **Type**: HTTP Bearer Token authentication

