## CURL request
curl -X POST "http://localhost:8082/api/v1/namespaceCluster/namegNMIcCluster/createTargets" -H "Content-Type: application/json" -d '{"TargetSourceName":"sourcename", "TargetSourceNameSpace":"namespace", "TargetList": [{"Address":"1.1.1.1", "Name": "Router1", "Operation":"create","Profile":"defaultProfile", "tags": ["tag1", "tag2"]}]}'


## Empty TargetList
curl -X POST "http://localhost:8082/api/v1/namespaceCluster/namegNMIcCluster/createTargets" -H "Content-Type: application/json" -d '{"TargetSourceName":"sourcename", "TargetSourceNameSpace":"namespace"}'