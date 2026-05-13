curl -X POST "http://localhost:8082/api/v1/default/target-source/http-discovery/createTargets" \
  -H "Content-Type: application/json" \
  -d '[
    {
      "address": "1.1.1.1:123",
      "name": "Router1",
      "operation": "created",
      "profile": "defaultProfile",
      "labels": [
        { "key": "tags", "value": "tag1, tag2" }
      ]
    }
  ]'


http://gnmic-controller-manager-api.gnmic-system.svc.cluster.local:8082/api/v1/default/target-source/http-discovery/createTargets
[
  {
    "address": "{{ data.primary_ip4.address.split('/')[0] if data.primary_ip4 and data.primary_ip4.address else '' }}:{{ data.custom_fields.port }}",
    "name": "{{ data.name }}",
    "operation": "{{ event }}",
    "profile": "{{ data.custom_fields.profile | default('') }}",
    "labels": [
      {
        "Key": "tags",
        "Value": "{{ data.tags | map(attribute='name') | join(', ') }}"
      }
    ]
  }
]