curl -X POST "http://localhost:8082/api/v1/default/target-source/http-discovery/createTargets" \
  -H "Authorization: Bearer 61unglgq///281Jo9tu5o+r3uVdohxrJWPXFalHlWGSet1W7NAfRVrDIP6tw+0ru" \
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
