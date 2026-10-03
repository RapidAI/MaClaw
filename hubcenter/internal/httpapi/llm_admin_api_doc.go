package httpapi

const llmAdminAPIDocMarkdown = `# HubCenter LLM 管理接口

机器可读说明：[/api/llm/admin-api.json](/api/llm/admin-api.json)

自动化密钥可以列出阵列和成员、批量添加或试跑、暂停或恢复成员、删除成员、查看健康用量和按节点保存的请求记录、从指定节点发起单成员测试（可带 tools），以及列出可用节点名。上游密钥不会以明文返回，只说明是否已配置，或返回末 4 位。管理页会保存这把自动化密钥，默认隐藏，可以显示、复制或删除；删除即撤销授权。这把密钥不能再创建别的管理密钥。

## 认证

每次请求带上自动化密钥，两个头任选一个：

` + "```" + `
Authorization: Bearer hck_...
X-API-Key: hck_...
` + "```" + `

管理后台的登录会话也可以调用这些接口。

## 权限和过期

创建密钥时可以限制范围，并设置过期时间。不写 scopes 表示拥有下面全部权限。已有的旧密钥没有 scopes，同样视为全部权限。

- read：列出阵列、成员、健康用量、按节点保存的请求记录和可用节点
- write：批量添加、试跑、PATCH 成员（含暂停与恢复）
- delete：删除成员
- test：单成员测试

expires_at 用 RFC3339，或只写日期 YYYY-MM-DD（按 Asia/Shanghai 当天 23:59:59 截止）。过期后返回 401。

## GET /api/admin/llm/provider-arrays

列出阵列和成员。需要 read。

每个成员返回 id、name、api_url、protocol、models、enabled、priority、dispatch_weight、requests_per_minute、requests_per_day、rate_limit_cooldown_sec、model_map、allowed_node_ids、capability_tags。api_key_configured 表示是否已配置上游密钥；已配置且长度超过 4 时附带 api_key_last4。allowed_node_ids 为空数组表示全部节点都可以调用该成员。

health 是当前进程内的最近窗口（最多 50 次）：success_rate、status_429、status_5xx、avg_latency_ms、last_error、last_error_at，以及按成员时区（缺省 Asia/Shanghai）统计的 today_requests、today_input_tokens、today_output_tokens、minute_requests。cooldown_until 是故障或 429 冷却结束时间，quota_until 是分钟或每日上限恢复时间。进程重启后这个窗口从零开始，也看不到其他节点。要看按节点保存的每日趋势，用 GET /api/admin/llm/member-health。响应里的 health_scope 固定为 process。

## Token Bank 阵列

平台自带的三个阵列，启动时自动创建，**不可删除**，也**不会因为空了被自动清理**。这三个阵列的积分倍率固定为 1.0，消费者按服务组计价，看不到档位。下表的 0.5 / 1 / 2 是分享者的结算倍率，写在成员的 token_bank_tier_multiplier 上。

| 阵列 id | 用途 | 结算倍率 |
|---|---|---|
| token_bank_low | 低档共享模型 | 0.5 |
| token_bank_mid | 中档共享模型 | 1 |
| token_bank_high | 高档共享模型 | 2 |

用户分享出来的模型按模型粒度成为成员（一个模型一个成员，同一个服务商的不同模型可以分在不同阵列）。
把某个模型标成某一档，就是把它这个成员移进对应阵列，并写入分享模型行的档位、结算倍率和阵列。服务组里指着该成员当前阵列的路由会改到新阵列。只挂了其它档位的服务组保持原样。该模型在旧阵列里没有成员时，旧阵列会从路由上拿掉。

成员 id 用列表接口返回的 MemberID。它是 ` + "`tbk_<shareID>__<base64url(模型名)>`" + `，模型名里的 ` + "`/`" + ` 不会出现在 id 里。

` + "```" + `
PATCH /api/admin/llm/providers/{MemberID}
{"token_bank_tier": "high"}
` + "```" + `

也可以写 ` + "`{\"array_id\": \"token_bank_high\"}`" + `。token_bank_tier 和 array_id 不要同时发送。
返回 status 为 ok，并带上 array_id 和 token_bank_tier。需要 write。
档位只有 low / mid / high 三个值，其它值返回 400。阵列不存在返回 400（正常情况下这三个阵列始终存在）。分享行里没有这个模型时返回 404，注册表不会被改。

已知 share id、且模型名不含 ` + "`/`" + ` 时，也可以：

` + "```" + `
PUT /api/admin/llm/token-bank/shares/{id}/models/{model}/tier
{"tier": "high"}
` + "```" + `

需要 write。这个接口按分享行重新发布该分享的成员。模型名含 ` + "`/`" + ` 时用上面的 PATCH。

删除或重命名这三个阵列中的任意一个会返回 400，错误信息是 provider array is protected。编辑时如果名称和现在不同，同样返回 400，名称不会改。删除阵列里的成员照常，用
DELETE /api/admin/llm/providers/{id}。

## GET /api/admin/llm/access-nodes

列出当前集群里可以指定的节点。需要 read。添加或编辑成员前先调用它，避免盲写节点名。

` + "```" + `json
{
  "nodes": [
    {"node_id": "hc-1", "name": "hc-1", "host": "hc-1.example", "reachable": true, "self": true},
    {"node_id": "hc-2", "name": "hc-2", "host": "hc-2.example", "reachable": true}
  ]
}
` + "```" + `

写入时用 node_id。name 只是显示名。

## POST /api/admin/llm/provider-arrays/batch

创建或更新阵列。一次请求全部成功或全部不写入。相同内容再发一次是安全的：已有成员会被更新；省略 api_key 会保留已保存的密钥。

dry_run 为 true 时只校验并返回将要创建或更新的 id，不写库。试跑需要 read 或 write。真正写入需要 write。

单次最多 50 个阵列、200 个成员。

阵列是一个逻辑服务商。成员共用倍率，按权重分担流量，遇到 429 或 5xx 会换下一个成员。模型服务组应路由到阵列 id，而不是每个成员。

### 请求体

` + "```" + `json
{
  "dry_run": false,
  "arrays": [
    {
      "id": "pool-a",
      "name": "Pool A",
      "timezone": "Asia/Shanghai",
      "credit_multiplier": 1,
      "providers": [
        {
          "id": "pool-a-1",
          "name": "Pool A primary",
          "api_url": "https://api.example.com/v1",
          "api_key": "sk-upstream",
          "protocol": "openai",
          "models": ["free-llama-70b"],
          "dispatch_weight": 3,
          "requests_per_minute": 5,
          "requests_per_day": 200,
          "rate_limit_cooldown_sec": 120,
          "model_map": {"free-llama-70b": "meta-llama/Llama-3.3-70B-Instruct"},
          "allowed_nodes": "hc-1, hc-2,hc-3",
          "capability_tags": ["tools", "vision", "reasoning"]
        }
      ]
    }
  ]
}
` + "```" + `

- allowed_node_ids 是字符串数组。allowed_nodes 是逗号分隔的写法，例如 hc-1, hc-2,hc-3，保存时会折成 allowed_node_ids，不会另外存储。两者都省略则保留原名单；allowed_node_ids 传 [] 表示改回全部节点。
- dispatch_weight：阵列内部流量份额。0 和 1 都是均分，更大的值被选中更多。批量更新时省略或写 0 会保留原值；要改回均分，用 PATCH 把 dispatch_weight 设为 0。
- requests_per_minute、requests_per_day：0 表示不限制。用完的成员会被跳过，直到这一分钟或当天结束，而不是继续撞 429。计数在本进程内。
- rate_limit_cooldown_sec：遇到 HTTP 429 后跳过该成员的秒数。0 保持默认 60 秒。
- model_map：对外模型名到该成员上游模型 id 的映射。例如对外都叫 free-llama-70b，各家写各自的 id。
- priority：服务商优先级，数字更大者在跨服务商路由里更优先。阵列内部的流量份额用 dispatch_weight。
- 省略的计费字段：新阵列会把第一个成员的计费套到全体；已有阵列保持当前共用计费。
- providers[].protocol：openai 或 anthropic，默认 openai。
- providers[].models：省略表示保留原列表；传 [] 表示清空。
- capability_tags：该成员的能力标签，和编辑服务商时的选项一致：chat、streaming、json、tools、reasoning、vision、document、code、search、audio、embedding、rerank。也可以写逗号分隔的字符串，例如 "tools, vision, reasoning"。省略表示保留原标签；传 [] 表示清空。大小写不区分，重复的会去掉。调度时，服务组路线上单独填写的标签优先。路线没填时，成员标签会加到模型标签上，不会拿掉模型已有的能力。同一质量档内挑选模型时也按这个并集判断。成员也没填时仍只用模型标签。

### 成功

` + "```" + `json
{"status": "ok", "dry_run": true, "arrays": [{"id": "pool-a", "name": "Pool A", "created": ["pool-a-1"]}]}
` + "```" + `

dry_run 只在试跑时出现。

## PATCH /api/admin/llm/providers/{id}

暂停、恢复，或修改限额、权重、模型映射、节点和能力标签。需要 write。只发送要改的字段。批量导入里不要带 paused 或 enabled，带了会返回 400，整批不写入。已有成员的暂停状态保持不变。暂停和恢复只用这个接口。

暂停分发：

` + "```" + `json
{"enabled": false}
` + "```" + `

恢复分发：

` + "```" + `json
{"enabled": true}
` + "```" + `

` + "```" + `json
{"allowed_nodes": "hc-1, hc-2", "dispatch_weight": 3, "requests_per_day": 200, "rate_limit_cooldown_sec": 90, "model_map": {"free-llama-70b": "llama-3.3-70b"}, "capability_tags": "tools, vision, reasoning"}
` + "```" + `

enabled 为 false 时不再向该成员转发，密钥、配置和路由都保留。true 恢复转发。省略 enabled 则保持当前状态。清单和测试结果里的 enabled 就是这个状态。model_map 传 {} 会清空映射。allowed_node_ids 传 [] 会改回全部节点。allowed_nodes 和 allowed_node_ids 不要同时发送。capability_tags 传 [] 会清空标签。

换阵列和标档位用这两个字段：

` + "```" + `json
{"array_id": "token_bank_high"}
` + "```" + `

` + "```" + `json
{"token_bank_tier": "high"}
` + "```" + `

array_id 把该成员移到指定阵列，密钥、配置和模型列表都保留，只换阵列；阵列不存在返回 400。
token_bank_tier 是 array_id 的简便写法，只接受 low / mid / high，对应 token_bank_low /
token_bank_mid / token_bank_high 三个平台阵列（见上一节）。两者不要同时发送。
对 Token Bank 成员，写入会保存该模型的档位和结算倍率，并把指着该成员当前阵列的服务组路由改到新阵列。只挂了其它档位的服务组保持原样。阵列积分倍率保持 1.0。同一请求里的其它字段（例如 dispatch_weight）仍然生效。token_bank_tier 只接受 Token Bank 成员 id，其它服务商返回 400。

## DELETE /api/admin/llm/providers/{id}

删除一个成员。需要 delete。阵列里还有其他成员时，删除只会摘掉这一家，路由仍指向阵列。若它是最后一名成员且仍被服务组引用，返回 409，并带上引用它的服务组；确认后加 ?prune=true 会同时从服务组里去掉该路由。

## POST /api/admin/llm/providers/{id}/test

按该成员**已保存的配置**从 HubCenter 服务器发一次测试对话，并返回它的当前状态。需要 test。请求体可省略，或写成：

` + "```" + `json
{"model": "free-llama-70b", "node": "hc-2", "tools": [{"type": "function", "function": {"name": "ping", "parameters": {"type": "object", "properties": {}}}}]}
` + "```" + `

- model：可选。省略时用成员的第一个模型。名字会先走该成员的 model_map。
- node：可选。指定从哪一台 HubCenter 节点向外发请求，例如 hc-2。节点名来自 GET /api/admin/llm/access-nodes。省略时，若当前节点在成员的可用范围内，就从当前节点发；否则转到一台允许且可达的节点。节点必须在集群名单里，并且落在该成员的 allowed_node_ids 内（空名单表示全部节点）。未知节点，或节点不在成员范围内，返回 400。指定了 node 时，上游的 429 或 5xx 记在这次探测结果里，不会改去试另一台节点。探测等待的是该成员自己的超时；从另一台节点发起时，额外留一小段时间把结果送回来。
- tools：可选，OpenAI chat tools 数组，1 到 8 个。带上之后探测会要求模型调用工具，响应里的 tool_calls 是模型实际返回的调用。tool_choice 可选，必须和 tools 一起出现。
- 不要在请求里带 api_url 或 api_key。地址和密钥只用库里的那一份。

成员不存在返回 404。配置缺地址或缺模型时 HTTP 200，success 为 false，member 里仍是当前配置。上游失败同样是 HTTP 200、success 为 false，并带上 error 和 latency_ms。

返回字段：

- success、error、reply、model、latency_ms、node：这一次探测的结果。model 是实际发给上游的名字。node 是真正向外发请求的节点。
- tool_calls：仅当请求带了 tools 时出现。每一项含 name 和 arguments。模型只回了工具调用、没有正文时，success 仍可为 true。
- member：当前配置和进程内健康。含 id、name、api_url、models、enabled、限额、权重、model_map、allowed_node_ids、capability_tags、api_key_configured、api_key_last4，以及 health（近期成功率、429、5xx、平均延迟、上次错误、今日请求和 token、冷却与限额截止时间）。密钥不明文返回。
- 这次探测不计入每分钟、每日限额，也不写入 health 窗口或 member-health。那些数字反映的是真实转发流量。

停用的成员仍会按保存的配置探测一次，member.enabled 为 false，方便确认上游本身是否还通。

## GET /api/admin/llm/member-health

按节点返回已保存的成员请求记录。需要 read。查询参数 day 是 YYYY-MM-DD，省略则取 Asia/Shanghai 的今天。provider_id 可选，只看一个成员。

每个节点一条。self 为 true 的是接到这个请求的节点，它的数字来自本机已保存的计数。其他节点来自它们最近一次同步过来的快照。记录按天保留 14 天，进程重启后还在。

每个成员含 id、requests、success、status_429、status_5xx、avg_latency_ms、input_tokens、output_tokens、last_error、last_error_at。health_scope 固定为 persisted。这里没有进程内那 50 次窗口，也没有冷却和限额截止时间；那部分仍在阵列清单的 health 里。

真实转发会计入。单成员测试不会计入。

## 错误

- 401 {"ok": false, "code": "ADMIN_UNAUTHORIZED", "message": "..."}
- 403 {"ok": false, "code": "ADMIN_FORBIDDEN", "message": "..."} 权限不足
- 400 {"error": "..."} 校验失败，没有写入
- 404 成员不存在
- 409 {"error": "provider_in_use", "groups": ["..."]}
- 500 {"error": "..."}

## Token Bank 分享管理

Token 银行里「用户分享出来的服务商」的自动化管理。需要 read / write / delete：

| 方法 | 路径 | 作用 | 需要 |
|---|---|---|---|
| GET | /api/admin/llm/token-bank/shares | 列出分享（可按用户/状态筛选，默认带模型） | read |
| POST | /api/admin/llm/token-bank/shares | 为一个用户创建分享。请求可带明文 api_key，入库前加密 | write |
| POST | /api/admin/llm/token-bank/shares/batch | 一次最多 20 个分享，HTTP 200，逐条返回状态 | write |
| PUT | /api/admin/llm/token-bank/shares/{id}/paused | 暂停 / 恢复一个分享 | write |
| DELETE | /api/admin/llm/token-bank/shares/{id} | 取出（删除分享信息） | delete |

查询参数（GET）：

- user_id：按分享者筛选，省略表示全部。
- status：active / paused / revoked，省略表示全部。
- limit / offset：分页，默认 limit=20，上限 200。
- include_models：默认 true。设成 false 可以只要列表、不带模型，减少响应体积。

**响应里永远拿不到密钥。** 每个分享返回 has_key（是否已配置）而不是密钥本体；EncryptedKey 恒为空串。
创建接口接受明文 api_key，这与其它 LLM 管理接口一致；服务端加密后再写入，响应同样只有 has_key。
私有分享必须带 audiences（hub_id / tenant_id）。同一把 key 再次提交返回已有分享，不会把已保存的私有范围改成公开。
批量接口始终 HTTP 200，results 里逐条带 status；整批超过 20 条或正文非法才是 400。
owner_email_masked 是掩码后的邮箱（u***@example.com 形式），管理员做关联排查用，不是拿来收集邮箱的。

暂停：

` + "```" + `
PUT /api/admin/llm/token-bank/shares/shr_abc/paused
{"paused": true, "reason": "upstream key revoked"}
` + "```" + `

**请求体可以省略** —— 省略或空体一律按「暂停」处理。这是一个暂停开关，误发一个空 PUT 把已经隔离的分享恢复上线，
比多暂停一次危险得多，所以默认值取安全的那一侧。恢复必须显式写 {"paused": false}。
reason 只写进 paused_reason，供人看。

取出是**物理删除**分享与其模型，但**不删用量历史**：token_bank_usage 自带 owner_user_id 与分享名快照，
已赚到的积分留在流水账本里。取出后同一把 key 可以重新分享（去重索引的那一行已经没了）。

错误：

- 401 ADMIN_UNAUTHORIZED（密钥无效）/ 403 ADMIN_FORBIDDEN（缺对应 scope）
- 404 share_not_found：分享不存在（跨用户操作也报 404，不报 403，避免被用来枚举 id）
- 503 token_bank_unavailable：本节点没有初始化 Token 银行

## curl

` + "```" + `
curl -sS "$ORIGIN/api/admin/llm/access-nodes" -H "Authorization: Bearer hck_..."
curl -sS "$ORIGIN/api/admin/llm/provider-arrays" -H "Authorization: Bearer hck_..."
curl -sS -X POST "$ORIGIN/api/admin/llm/provider-arrays/batch" \
  -H "Authorization: Bearer hck_..." -H "Content-Type: application/json" \
  -d '{"dry_run":true,"arrays":[{"id":"pool-a","name":"Pool A","providers":[{"id":"pool-a-1","name":"Primary","api_url":"https://api.example.com/v1","api_key":"sk-upstream","models":["free-llama-70b"],"allowed_nodes":"hc-1, hc-2"}]}]}'
curl -sS -X PATCH "$ORIGIN/api/admin/llm/providers/pool-a-1" \
  -H "Authorization: Bearer hck_..." -H "Content-Type: application/json" \
  -d '{"enabled":false}'
curl -sS -X PATCH "$ORIGIN/api/admin/llm/providers/pool-a-1" \
  -H "Authorization: Bearer hck_..." -H "Content-Type: application/json" \
  -d '{"enabled":true}'
curl -sS "$ORIGIN/api/admin/llm/member-health?day=2026-09-30" -H "Authorization: Bearer hck_..."
curl -sS -X POST "$ORIGIN/api/admin/llm/providers/pool-a-1/test" \
  -H "Authorization: Bearer hck_..." -H "Content-Type: application/json" \
  -d '{"model":"free-llama-70b","node":"hc-2","tools":[{"type":"function","function":{"name":"ping","parameters":{"type":"object","properties":{}}}}]}'
curl -sS "$ORIGIN/api/admin/llm/token-bank/shares?status=paused" -H "Authorization: Bearer hck_..."
curl -sS -X POST "$ORIGIN/api/admin/llm/token-bank/shares" \
  -H "Authorization: Bearer hck_..." -H "Content-Type: application/json" \
  -d '{"owner_user_id":"usr_1","display_name":"lab","api_url":"https://api.example.com/v1","api_key":"sk-upstream","models":["gpt-4o"],"visibility":"private","audiences":[{"hub_id":"hub-a"}]}'
curl -sS -X POST "$ORIGIN/api/admin/llm/token-bank/shares/batch" \
  -H "Authorization: Bearer hck_..." -H "Content-Type: application/json" \
  -d '{"shares":[{"owner_user_id":"usr_1","display_name":"lab","api_url":"https://api.example.com/v1","api_key":"sk-upstream","models":["gpt-4o"]}]}'
curl -sS -X PUT "$ORIGIN/api/admin/llm/token-bank/shares/shr_abc/paused" \
  -H "Authorization: Bearer hck_..." -H "Content-Type: application/json" -d '{"paused":true}'
curl -sS -X DELETE "$ORIGIN/api/admin/llm/token-bank/shares/shr_abc" -H "Authorization: Bearer hck_..."
` + "```" + `
`

func llmAdminAPIOpenAPI() map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "HubCenter LLM admin API",
			"version":     "1.3.1",
			"description": "Manage provider arrays with an automation API key. Human doc: /api/llm/admin-api.md",
		},
		"servers": []any{map[string]any{"url": "/"}},
		"paths": map[string]any{
			"/api/admin/llm/provider-arrays": map[string]any{
				"get": map[string]any{
					"operationId": "listProviderArrays",
					"summary":     "List arrays and members without upstream secrets",
					"security":    []any{map[string]any{"adminApiKey": []any{}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Arrays, redacted members, and process-local health. Each member includes enabled."},
						"401": map[string]any{"description": "Missing or invalid API key"},
						"403": map[string]any{"description": "Key is missing the read scope"},
					},
				},
			},
			"/api/admin/llm/access-nodes": map[string]any{
				"get": map[string]any{
					"operationId": "listAccessNodes",
					"summary":     "List node ids that can be assigned to a provider",
					"security":    []any{map[string]any{"adminApiKey": []any{}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Cluster nodes. Use node_id when writing allowed_nodes."},
						"401": map[string]any{"description": "Missing or invalid API key"},
						"403": map[string]any{"description": "Key is missing the read scope"},
					},
				},
			},
			"/api/admin/llm/token-bank/shares": map[string]any{
				"get": map[string]any{
					"operationId": "listTokenBankShares",
					"summary":     "List Token Bank shares without upstream secrets",
					"security":    []any{map[string]any{"adminApiKey": []any{}}},
					"parameters": []any{
						map[string]any{"name": "user_id", "in": "query", "required": false, "schema": map[string]any{"type": "string"}, "description": "Filter by owner. Omit for every owner."},
						map[string]any{"name": "status", "in": "query", "required": false, "schema": map[string]any{"type": "string", "enum": []any{"active", "paused", "revoked"}}},
						map[string]any{"name": "limit", "in": "query", "required": false, "schema": map[string]any{"type": "integer", "default": 20, "maximum": 200}},
						map[string]any{"name": "offset", "in": "query", "required": false, "schema": map[string]any{"type": "integer", "default": 0}},
						map[string]any{"name": "include_models", "in": "query", "required": false, "schema": map[string]any{"type": "boolean", "default": true}, "description": "Include each share's models. Set false for a lighter listing."},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Shares with masked owner email, has_key, and optionally models. EncryptedKey is always empty."},
						"401": map[string]any{"description": "Missing or invalid API key"},
						"403": map[string]any{"description": "Key is missing the read scope"},
						"503": map[string]any{"description": "Token Bank is not initialised on this node"},
					},
				},
				"post": map[string]any{
					"operationId": "createTokenBankShare",
					"summary":     "Create one Token Bank share for a user",
					"description": "Accepts a plaintext api_key, encrypts it before storage, and never returns the key. A repeated key returns the existing share and does not change its visibility.",
					"security":    []any{map[string]any{"adminApiKey": []any{}}},
					"responses": map[string]any{
						"201": map[string]any{"description": "Share created. has_key is true. The api key is absent."},
						"200": map[string]any{"description": "The same key was already shared."},
						"400": map[string]any{"description": "Missing fields, or a private share without an audience."},
						"401": map[string]any{"description": "Missing or invalid API key"},
						"403": map[string]any{"description": "Key is missing the write scope, or the owner is not verified."},
						"404": map[string]any{"description": "Owner user was not found."},
						"409": map[string]any{"description": "Share limit reached."},
						"503": map[string]any{"description": "Token Bank is not initialised on this node"},
					},
				},
			},
			"/api/admin/llm/token-bank/shares/batch": map[string]any{
				"post": map[string]any{
					"operationId": "batchCreateTokenBankShares",
					"summary":     "Create up to 20 Token Bank shares",
					"description": "Always HTTP 200 when the batch itself is valid. Each result carries its own status. The api key is never echoed.",
					"security":    []any{map[string]any{"adminApiKey": []any{}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Per-item results. A failed item does not roll back the others."},
						"400": map[string]any{"description": "Empty batch, more than 20 shares, or a malformed body."},
						"401": map[string]any{"description": "Missing or invalid API key"},
						"403": map[string]any{"description": "Key is missing the write scope"},
					},
				},
			},
			"/api/admin/llm/token-bank/shares/{id}/paused": map[string]any{
				"put": map[string]any{
					"operationId": "setTokenBankSharePaused",
					"summary":     "Pause or resume a Token Bank share",
					"description": "An empty body is treated as paused=true: a bare PUT must not accidentally resume a quarantined share.",
					"security":    []any{map[string]any{"adminApiKey": []any{}}},
					"parameters": []any{
						map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"requestBody": map[string]any{
						"required": false,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/TokenBankPaused"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Updated share"},
						"400": map[string]any{"description": "Malformed body"},
						"401": map[string]any{"description": "Missing or invalid API key"},
						"403": map[string]any{"description": "Key is missing the write scope"},
						"404": map[string]any{"description": "Share not found"},
						"503": map[string]any{"description": "Token Bank is not initialised on this node"},
					},
				},
			},
			"/api/admin/llm/token-bank/shares/{id}/models/{model}/tier": map[string]any{
				"put": map[string]any{
					"operationId": "setTokenBankModelTier",
					"summary":     "Set a Token Bank model's tier and move it into the matching array",
					"description": "Writes the tier, settlement multiplier, and array id, then republishes the share. The tier arrays keep credit multiplier 1.0; 0.5, 1, and 2 are the sharer settlement rates. A model name that contains a slash cannot use this path; PATCH the member id instead.",
					"security":    []any{map[string]any{"adminApiKey": []any{}}},
					"parameters": []any{
						map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
						map[string]any{"name": "model", "in": "path", "required": true, "schema": map[string]any{"type": "string"}, "description": "Model name. A slash in the name does not match this path."},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type":     "object",
									"required": []string{"tier"},
									"properties": map[string]any{
										"tier":            map[string]any{"type": "string", "enum": []string{"low", "mid", "high", "custom"}},
										"tier_multiplier": map[string]any{"type": "number", "description": "Required for custom. Optional override for low, mid, and high."},
										"array_id":        map[string]any{"type": "string", "description": "Optional for custom. low, mid, and high use token_bank_low, token_bank_mid, and token_bank_high."},
									},
								},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Tier stored and the share republished when routing is available."},
						"400": map[string]any{"description": "Tier is missing or not one of low, mid, high, custom."},
						"401": map[string]any{"description": "Missing or invalid API key"},
						"403": map[string]any{"description": "Key is missing the write scope"},
						"404": map[string]any{"description": "Share or model was not found."},
						"503": map[string]any{"description": "Token Bank is not initialised on this node"},
					},
				},
			},
			"/api/admin/llm/token-bank/shares/{id}": map[string]any{
				"delete": map[string]any{
					"operationId": "takeOutTokenBankShare",
					"summary":     "Remove a Token Bank share",
					"description": "Hard-deletes the share and its models. Usage history and earned credits are kept; the same key can be shared again.",
					"security":    []any{map[string]any{"adminApiKey": []any{}}},
					"parameters": []any{
						map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Share removed"},
						"401": map[string]any{"description": "Missing or invalid API key"},
						"403": map[string]any{"description": "Key is missing the delete scope"},
						"404": map[string]any{"description": "Share not found"},
						"503": map[string]any{"description": "Token Bank is not initialised on this node"},
					},
				},
			},
			"/api/admin/llm/provider-arrays/batch": map[string]any{
				"post": map[string]any{
					"operationId": "importProviderArrays",
					"summary":     "Create or update provider arrays, or validate with dry_run",
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
						"200": map[string]any{"description": "Arrays saved, or validated when dry_run is true"},
						"400": map[string]any{"description": "Validation error, nothing written"},
						"401": map[string]any{"description": "Missing or invalid API key"},
						"403": map[string]any{"description": "Key is missing read (dry_run) or write"},
					},
				},
			},
			"/api/admin/llm/providers/{id}": map[string]any{
				"patch": map[string]any{
					"operationId": "patchProviderMember",
					"summary":     "Pause or resume a member, or update weight, quota, model map, nodes, and tags",
					"description": "Send only the fields to change. enabled false pauses dispatch and keeps the stored key, configuration, and routes. enabled true resumes dispatch. Omit enabled to leave the current pause state. Batch import does not change it. The list and test payloads return this state as enabled.",
					"security":    []any{map[string]any{"adminApiKey": []any{}}},
					"parameters":  []any{map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/ProviderMemberPatch"},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Member updated. When enabled was sent, the body echoes it."},
						"400": map[string]any{"description": "Validation error"},
						"401": map[string]any{"description": "Missing or invalid API key"},
						"403": map[string]any{"description": "Key is missing the write scope"},
						"404": map[string]any{"description": "Member not found"},
					},
				},
				"delete": map[string]any{
					"operationId": "deleteProviderMember",
					"summary":     "Remove one array member",
					"security":    []any{map[string]any{"adminApiKey": []any{}}},
					"parameters": []any{
						map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
						map[string]any{"name": "prune", "in": "query", "schema": map[string]any{"type": "boolean"}},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Member removed"},
						"409": map[string]any{"description": "Last member is still referenced. Retry with prune=true."},
					},
				},
			},
			"/api/admin/llm/providers/{id}/test": map[string]any{
				"post": map[string]any{
					"operationId": "testProviderMember",
					"summary":     "Probe one saved member and return its current configuration status",
					"description": "Uses the stored URL and key only. Optional body fields are model, node, tools, and tool_choice. node selects which HubCenter node sends the probe. tools is an OpenAI tools array and the response includes tool_calls. Caller-supplied api_url or api_key is rejected. A failed probe is HTTP 200 with success false. A missing member is 404. The probe does not consume request quota or persisted health.",
					"security":    []any{map[string]any{"adminApiKey": []any{}}},
					"parameters":  []any{map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}},
					"responses": map[string]any{
						"200": map[string]any{"description": "Probe result plus saved member status. success false is still HTTP 200."},
						"400": map[string]any{"description": "Unknown body field, invalid model, node, or tools, or the node is outside the member access scope."},
						"404": map[string]any{"description": "Member does not exist."},
					},
				},
			},
			"/api/admin/llm/member-health": map[string]any{
				"get": map[string]any{
					"operationId": "listMemberHealth",
					"summary":     "Saved per-node member request totals for one day",
					"description": "Persisted real-traffic totals grouped by HubCenter node. day is YYYY-MM-DD in Asia/Shanghai when omitted. provider_id filters one member. Records are kept for 14 days and survive process restart. health_scope is persisted. Probes are not included.",
					"security":    []any{map[string]any{"adminApiKey": []any{}}},
					"parameters": []any{
						map[string]any{"name": "day", "in": "query", "schema": map[string]any{"type": "string", "example": "2026-09-30"}},
						map[string]any{"name": "provider_id", "in": "query", "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Per-node member totals. An empty member list means that node has no saved traffic for the day."},
						"400": map[string]any{"description": "day is not YYYY-MM-DD."},
					},
				},
			},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"adminApiKey": map[string]any{
					"type":        "http",
					"scheme":      "bearer",
					"description": "Automation key hck_... Also accepted as header X-API-Key. Scopes: read, write, delete, test.",
				},
			},
			"schemas": map[string]any{
				"TokenBankPaused": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"paused": map[string]any{"type": "boolean", "description": "true to pause, false to resume. An empty body means true."},
						"reason": map[string]any{"type": "string", "description": "Free-text note stored on the share. Only shown to admins."},
					},
				},
				"ProviderArrayBatch": map[string]any{
					"type":     "object",
					"required": []string{"arrays"},
					"properties": map[string]any{
						"dry_run": map[string]any{"type": "boolean", "description": "Validate only. Nothing is written."},
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
				"ProviderMemberPatch": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"enabled":                 map[string]any{"type": "boolean", "description": "false pauses dispatch. true resumes it. The stored key, configuration, and routes stay. Omit to keep the current state. Batch import rejects paused and enabled instead of changing this."},
						"dispatch_weight":         map[string]any{"type": "integer", "description": "Share of array traffic. 0 and 1 are equal. 0 here clears a higher weight."},
						"priority":                map[string]any{"type": "integer"},
						"requests_per_minute":     map[string]any{"type": "integer"},
						"requests_per_day":        map[string]any{"type": "integer"},
						"rate_limit_cooldown_sec": map[string]any{"type": "integer", "description": "Seconds to skip this member after HTTP 429. 0 keeps 60 seconds."},
						"model_map":               map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "An empty object clears the map."},
						"allowed_node_ids":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Empty array means every node. Do not send this together with allowed_nodes."},
						"allowed_nodes":           map[string]any{"type": "string", "example": "hc-1, hc-2, hc-3", "description": "Comma-separated node ids. Folded into allowed_node_ids."},
						"capability_tags":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "example": []string{"tools", "vision", "reasoning"}, "description": "A comma-separated string is also accepted. Empty array clears the tags. Omit to keep the stored tags."},
						"array_id":                map[string]any{"type": "string", "description": "Move this member into an existing array. The member keeps its key, configuration, and models. For a Token Bank member, token_bank_low, token_bank_mid, and token_bank_high also store the settlement tier and retarget that model's routes. Do not send this together with token_bank_tier."},
						"token_bank_tier":         map[string]any{"type": "string", "enum": []string{"low", "mid", "high"}, "description": "Mark a Token Bank model as low, mid, or high. Stores the tier and settlement multiplier (0.5, 1, 2) and moves routes that name this member's current array onto token_bank_low, token_bank_mid, or token_bank_high. A service group that uses only another tier stays there. Array credit multiplier stays 1.0. The id must be a Token Bank member. Do not send this together with array_id."},
					},
				},
				"ProviderMember": map[string]any{
					"type":     "object",
					"required": []string{"id", "name", "api_url"},
					"properties": map[string]any{
						"id":                      map[string]any{"type": "string"},
						"name":                    map[string]any{"type": "string"},
						"api_url":                 map[string]any{"type": "string"},
						"api_key":                 map[string]any{"type": "string", "description": "Omit on update to keep the stored upstream key. Never returned by list."},
						"protocol":                map[string]any{"type": "string", "enum": []string{"openai", "anthropic"}},
						"models":                  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"dispatch_weight":         map[string]any{"type": "integer", "description": "Share of array traffic. 0 and 1 are equal."},
						"priority":                map[string]any{"type": "integer"},
						"requests_per_minute":     map[string]any{"type": "integer"},
						"requests_per_day":        map[string]any{"type": "integer"},
						"rate_limit_cooldown_sec": map[string]any{"type": "integer", "description": "Seconds to skip this member after HTTP 429. 0 keeps 60 seconds."},
						"model_map":               map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
						"allowed_node_ids":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Empty means every node."},
						"allowed_nodes":           map[string]any{"type": "string", "example": "hc-1, hc-2, hc-3", "description": "Comma-separated node ids. Folded into allowed_node_ids and not stored."},
						"capability_tags":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "example": []string{"tools", "vision", "reasoning"}, "description": "Member capability tags. A comma-separated string is also accepted. Empty array clears them. Omit to keep the stored tags. A service-group route tag list overrides these. When the route list is empty, dispatch adds these tags to the model tags, including when choosing a model inside one quality band."},
					},
				},
			},
		},
	}
}
