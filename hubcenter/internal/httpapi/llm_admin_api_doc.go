package httpapi

const llmAdminAPIDocMarkdown = `# HubCenter LLM admin API

Machine-readable companion: [/api/llm/admin-api.json](/api/llm/admin-api.json)

This key can only batch-add provider arrays. It cannot list providers, read stored upstream keys, delete arrays, or create more admin keys.

## Auth

Send the automation key on every batch request. Either header works:

` + "```" + `
Authorization: Bearer hck_...
X-API-Key: hck_...
` + "```" + `

An admin console session token is also accepted on this endpoint. Create and revoke keys from the LLM Service admin page; the secret is shown once.

## POST /api/admin/llm/provider-arrays/batch

Creates or updates providers and places them in logical arrays. One request is all or nothing. Re-sending the same payload is safe: an existing provider is updated, and an omitted api_key keeps the stored key.

An array is one logical provider. Members share the array multiplier and token price, rotate on each request, and a 429 or 5xx tries the next member. Model service groups should route to the array id, not to each member.

Limits: 50 arrays and 200 providers per request.

### Body

` + "```" + `json
{
  "arrays": [
    {
      "id": "pool-a",
      "name": "Pool A",
      "timezone": "Asia/Shanghai",
      "credit_multiplier": 1,
      "token_pricing": {
        "input_credits_per_10k": 3,
        "output_credits_per_10k": 6
      },
      "providers": [
        {
          "id": "pool-a-1",
          "name": "Pool A primary",
          "api_url": "https://api.example.com/v1",
          "api_key": "sk-upstream",
          "protocol": "openai",
          "models": ["gpt-4o"]
        },
        {
          "id": "pool-a-2",
          "name": "Pool A spare",
          "api_url": "https://api-spare.example.com/v1",
          "api_key": "sk-upstream-2",
          "protocol": "openai",
          "models": ["gpt-4o"]
        }
      ]
    }
  ]
}
` + "```" + `

- arrays[].id: logical array id. Empty uses the first provider id.
- arrays[].name: label. Empty uses the first provider name.
- credit_multiplier, timezone, credit_multiplier_schedule, token_pricing: shared by every member. When these are omitted, a new array copies the first provider's billing onto every member. An existing array keeps its current shared billing.
- providers[].id, name, api_url: required. api_url must start with http:// or https://.
- providers[].protocol: openai or anthropic. Default openai.
- providers[].api_key: upstream credential. Omit it on update to keep the stored key.
- providers[].models: omit to keep the stored list; send [] to clear it.

### Success

` + "```" + `json
{
  "status": "ok",
  "arrays": [
    {"id": "pool-a", "name": "Pool A", "created": ["pool-a-1"], "updated": ["pool-a-2"]}
  ]
}
` + "```" + `

### Errors

- 401 {"ok": false, "code": "ADMIN_UNAUTHORIZED", "message": "..."}
- 400 {"error": "..."} validation failure, nothing is written
- 500 {"error": "..."}

### curl

` + "```" + `
curl -sS -X POST "$ORIGIN/api/admin/llm/provider-arrays/batch" \
  -H "Authorization: Bearer hck_..." \
  -H "Content-Type: application/json" \
  -d '{"arrays":[{"id":"pool-a","name":"Pool A","credit_multiplier":1,"providers":[{"id":"pool-a-1","name":"Primary","api_url":"https://api.example.com/v1","api_key":"sk-upstream","models":["gpt-4o"]}]}]}'
` + "```" + `
`

func llmAdminAPIOpenAPI() map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "HubCenter LLM admin API",
			"version":     "1.0.0",
			"description": "Batch-add provider arrays with an automation API key. Human doc: /api/llm/admin-api.md",
		},
		"servers": []any{map[string]any{"url": "/"}},
		"paths": map[string]any{
			"/api/admin/llm/provider-arrays/batch": map[string]any{
				"post": map[string]any{
					"operationId": "importProviderArrays",
					"summary":     "Create or update provider arrays",
					"security":    []any{map[string]any{"adminApiKey": []any{}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/ProviderArrayBatch"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Arrays saved"},
						"400": map[string]any{"description": "Validation error, nothing written"},
						"401": map[string]any{"description": "Missing or invalid API key"},
					},
				},
			},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"adminApiKey": map[string]any{
					"type":        "http",
					"scheme":      "bearer",
					"description": "Automation key hck_... Also accepted as header X-API-Key.",
				},
			},
			"schemas": map[string]any{
				"ProviderArrayBatch": map[string]any{
					"type":     "object",
					"required": []string{"arrays"},
					"properties": map[string]any{
						"arrays": map[string]any{
							"type":     "array",
							"maxItems": 50,
							"items":    map[string]any{"$ref": "#/components/schemas/ProviderArrayImport"},
						},
					},
				},
				"ProviderArrayImport": map[string]any{
					"type":     "object",
					"required": []string{"providers"},
					"properties": map[string]any{
						"id":                map[string]any{"type": "string", "description": "Logical array id. Empty uses the first provider id."},
						"name":              map[string]any{"type": "string"},
						"timezone":          map[string]any{"type": "string", "example": "Asia/Shanghai"},
						"credit_multiplier": map[string]any{"type": "number"},
						"token_pricing": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"input_credits_per_10k":  map[string]any{"type": "number"},
								"output_credits_per_10k": map[string]any{"type": "number"},
							},
						},
						"providers": map[string]any{
							"type":     "array",
							"minItems": 1,
							"items":    map[string]any{"$ref": "#/components/schemas/ProviderMember"},
						},
					},
				},
				"ProviderMember": map[string]any{
					"type":     "object",
					"required": []string{"id", "name", "api_url"},
					"properties": map[string]any{
						"id":       map[string]any{"type": "string"},
						"name":     map[string]any{"type": "string"},
						"api_url":  map[string]any{"type": "string"},
						"api_key":  map[string]any{"type": "string", "description": "Omit on update to keep the stored upstream key."},
						"protocol": map[string]any{"type": "string", "enum": []string{"openai", "anthropic"}},
						"models":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
				},
			},
		},
	}
}
