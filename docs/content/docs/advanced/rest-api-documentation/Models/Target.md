---
title: "Model"
linkTitle: "Model"
weight: 4
description: >
  Documentation for OpenAPI models and their schema-defined properties.
---

# Target
Network device to be monitored. Properties not marked as optional must be in JSON body.

## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
| **name** | **String** | Name of device to be monitored. | [default to null] |
| **address** | **String** | IPv4/IPv6 address or hostname. | [default to null] |
| **port** | **Integer** | gNMIc port. | [optional] [default to null] |
| **targetProfile** | **String** | TargetProfile applied to apply to this router. | [optional] [default to null] |
| **labels** | [**List**](map.md) | Input of labels as key:value pair. | [optional] [default to null] |
| **operation** | **String** | Either `created`, `updated` or `deleted`. `created` and `updated` are identical and both apply the target. | [default to null] |

