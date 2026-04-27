## CURL request
curl -X POST "http://localhost:8082/api/v1/gnmic-system/gnmic-controller-manager/createTargets" -H "Content-Type: application/json" -d '{"TargetSourceName":"sourcename", "TargetSourceNameSpace":"namespace", "TargetList": [{"Address":"1.1.1.1", "Name": "Router1", "Operation":"create","Profile":"defaultProfile", "tags": ["tag1", "tag2"]}]}'

## Empty TargetList
curl -X POST "http://localhost:8082/api/v1/gnmic-system/cluster1/createTargets" -H "Content-Type: application/json" -d '{"TargetSourceName":"sourcename", "TargetSourceNameSpace":"namespace"}'

## Empty Target in Target List
curl -X POST "http://localhost:8082/api/v1/gnmic-system/cluster1/createTargets" -H "Content-Type: application/json" -d '{"TargetSourceName":"sourcename", "TargetSourceNameSpace":"namespace", "TargetList": [{"Address":"1.1.1.1", "Name": "Router1", "Operation":""}]}'

## Empty TargetSourceName
curl -X POST "http://localhost:8082/api/v1/gnmic-system/gnmic-controller-manager/createTargets" -H "Content-Type: application/json" -d '{"TargetSourceName":"", "TargetSourceNameSpace":"namespace", "TargetList": [{"Address":"1.1.1.1", "Name": "Router1", "Operation":"create"}]}'

## Wrong operation
curl -X POST "http://localhost:8082/api/v1/gnmic-system/cluster1/createTargets" -H "Content-Type: application/json" -d '{"TargetSourceName":"sourcename", "TargetSourceNameSpace":"namespace", "TargetList": [{"Address":"1.1.1.1", "Name": "Router1", "Operation":"notupdate","Profile":"defaultProfile", "tags": ["tag1", "tag2"]}]}'


http://gnmic-controller-manager-api.gnmic-system.svc.cluster.local:8082/api/v1/gnmic-system/gnmic-controller-manager/createTargets
{
  "TargetSourceName": "netbox",
  "TargetSourceNameSpace": "netbox",
  "TargetList": [
      {
        "name": "{{ data.name }}",
        "address": "{{ data.primary_ip4.address.split('/')[0] if data.primary_ip4 else '' }}:{{ data.custom_fields.port }}",
        "profile": "{{ data.custom_fields.profile | default('') }}",
        "tags": {{ data.tags | map(attribute='name') | list | tojson }},
        "operation":"create"
      }
    ]
}
