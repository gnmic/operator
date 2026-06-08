---
title: "Model"
linkTitle: "Model"
weight: 4
description: >
  Todo
---

# Target
## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
| **name** | **String** | Name of device to be monitored. | [default to null] |
| **address** | **String** | IPv4/IPv6 address or hostname. | [default to null] |
| **port** | **Integer** | gNMIc port. | [optional] [default to null] |
| **targetProfile** | **String** | TargetProfile applied to apply to this router. | [optional] [default to null] |
| **labels** | [**List**](map.md) | Input of labels as key:value pair. | [optional] [default to null] |
| **operation** | **String** | Either `created`, `updated` or `deleted`. `created` and `updated` are identical and both apply the target. | [default to null] |

[[Back to Model list]](../_index.md#documentation-for-models) [[Back to API list]](../_index.md#documentation-for-api-endpoints) [[Back to README]](../_index.md)

