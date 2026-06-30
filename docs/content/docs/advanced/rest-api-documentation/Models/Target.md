# Target
## Properties

| Name | Type | Description | Notes |
|------------ | ------------- | ------------- | -------------|
| **name** | **String** | Name of device to be monitored. | [default to null] |
| **address** | **String** | IPv4/IPv6 address or hostname. | [default to null] |
| **port** | **Integer** | gNMIc port. | [optional] [default to null] |
| **targetProfile** | **String** | TargetProfile applied to apply to this router. | [optional] [default to null] |
| **labels** | [**List**](map.md) | Labels must be map[string]string. For example vendor:nokia. | [optional] [default to null] |
| **operation** | **String** | Either &#x60;created&#x60;, &#x60;updated&#x60; or &#x60;deleted&#x60;. &#x60;created&#x60; and &#x60;updated&#x60; are identical and both apply the target. | [default to null] |

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

