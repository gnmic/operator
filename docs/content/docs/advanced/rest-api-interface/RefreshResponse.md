---
title: "Model"
linkTitle: "Model"
weight: 4
description: >
  Documentation for OpenAPI models and their schema-defined properties.
---

# RefreshResponse

## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
| **requestedAt** | **Date** | When a run was last requested through this endpoint. | [default to null] |
| **debounced** | **Boolean** | True when this call did not schedule a new run because one was requested within the debounce window. | [default to null] |

