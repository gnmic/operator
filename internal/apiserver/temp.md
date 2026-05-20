curl -X POST "http://localhost:8082/api/v1/default/target-source/http-discovery/createTargets" \
  -H "Authorization: Bearer fEPGF5qwVfM7vvEw2vYuaPojcda/a78aOtqmW4oEFYZUJF67yXluSjDoTKmey5zU" \
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
