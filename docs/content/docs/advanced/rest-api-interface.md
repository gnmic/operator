---
title: "REST API Interface"
linkTitle: "REST API Interface"
weight: 3
description: >
  REST API documentation of the gNMIc Operator with available endpoints and required request formats.
---

<a name="documentation-for-api-endpoints"></a>
## Documentation for API Endpoints

All URIs are relative to *http://localhost:8082*

| Class | Method | HTTP request | Description |
|------------ | ------------- | ------------- | -------------|
| *defaultapi* | [**getClusterPlan**](/docs/advanced/rest-api-interface/defaultapi/) | **GET** /clusters/:namespace/:name/plan | Get cluster plan. |
*defaultapi* | [**refreshTargetSource**](/docs/advanced/rest-api-interface/defaultapi/) | **POST** /api/v1/namespaces/:namespace/targetsources/:name/refresh | Request an immediate discovery run for a TargetSource. |


<a name="documentation-for-models"></a>
## Documentation for Models

 - [refreshresponse](/docs/advanced/rest-api-interface/refreshresponse/)


<a name="documentation-for-authorization"></a>
## Documentation for Authorization

For a detailed explanation on how to configure the required secrets within the gNMIc Operator, refer to [TargetSource > Webhook](/docs/user-guide/targetsource/webhook/).

<a name="bearerAuth"></a>
### bearerAuth

- **Type**: HTTP Bearer Token authentication

<a name="signature"></a>
### signature

- **Type**: API key
- **API key parameter name**: X-Hook-Signature
- **Location**: HTTP header

