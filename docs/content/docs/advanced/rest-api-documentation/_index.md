---
title: "REST API interface"
linkTitle: "REST API interface"
weight: 3
description: >
  The gNMIc Operator has a REST API endpoint. This documentation explains what routes are available and how to use them.
---

<a name="documentation-for-api-endpoints"></a>
## Documentation for API Endpoints

All URIs are relative to *http://localhost:8082*

| Class | Method | HTTP request | Description |
|------------ | ------------- | ------------- | -------------|
| *DefaultApi* | [**applyTargets**](/docs/advanced/rest-api-documentation/DefaultApi) | **POST** /api/v1/:namespace/target-source/:name/applyTargets | Interface for real-time target updates, usually using a webhook. Targets are applied in the gNMIc Operator. |
*DefaultApi* | [**getClusterPlan**](/docs/advanced/rest-api-documentation/DefaultApi) | **GET** /clusters/:namespace/:name/plan | Get cluster plan. |


<a name="documentation-for-models"></a>
## Documentation for Models

 - [Target](/docs/advanced/rest-api-documentation/Models/Target/)


<a name="documentation-for-authorization"></a>
## Documentation for Authorization

<a name="bearerAuth"></a>
### bearerAuth

- **Type**: HTTP Bearer Token authentication

