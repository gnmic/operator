curl -X POST "http://localhost:8082/api/v1/default/target-source/targetsource-1/applyTargets" \
  -H "Authorization: Bearer I+MieBB72PAD5Cu8y4iOc75q+xYiE8WhXjFA8K5Xm/4DtjA6GJufQisZuM7JIWQS" \
  -H "Content-Type: application/json" \
  -d '[
    {
      "ip": "1.1.1.1",
      "port": 22,
      "name": "Router1",
      "operation": "created",
      "targetProfile": "defaultProfile",
      "labels": [
        { "key": "tags", "value": "tag1, tag2" }
      ]
    }
  ]'
