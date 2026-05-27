curl -X POST "http://localhost:8082/api/v1/default/target-source/netbox/applyTargets" \
  -H "Authorization: Bearer dIqf/y3xAvjisKweCG+Ro+9iqlLsBQc6Bl+RhjPbKzUy7T2B/ENA8+J7ZGms0/kK" \
  -H "Content-Type: application/json" \
  -d '[
    {
      "address": "172.18.0.5",
      "port": 57400,
      "name": "leaf1",
      "operation": "updated",
      "targetProfile": "",
      "labels": [
        { "key": "tags", "value": "" }
      ]
    }
  ]'
