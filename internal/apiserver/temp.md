curl -X POST "http://localhost:8082/api/v1/default/target-source/http-discovery/createTargets" \
  -H "Authorization: Bearer SzLLLeVm7G68BzT+375zDA38g7SNs1dB9uRtyQsViS+EsqJs9kA51R2VKBWE3DI0" \
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


curl -X POST "http://localhost:8082/api/v1/default/target-source/http-discovery/createTargets" \
  -H "Authorization: Bearer bWM7TRrWgwUCUsfwaUFPj1hlWROOOONNNGGvBry6ZvAc6oWSy97/BcZzcQWuDI1" \
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