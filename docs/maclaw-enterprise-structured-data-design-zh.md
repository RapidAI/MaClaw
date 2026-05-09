# MaClaw 企业结构化数据底座设计文档

状态：draft  
日期：2026-05-05  
范围：独立 MaClawDataSrv 服务，承载销售数据、人力数据、财务数据等公司级结构化信息的可靠存储、检索、处理与 agent 调用接口

## 1. 背景

MaClaw 需要的不只是“灵活保存几条 JSON 记录”，而是一个可承载公司业务数据的结构化数据底座。销售、人力、财务数据通常具备以下特征：

- 数据量持续增长，可能达到百万级甚至更高。
- 字段结构在早期会变化，但长期需要可治理、可校验、可迁移。
- 数据存在权限边界，例如财务、人事薪酬、客户合同不应被普通用户或无关 agent 任意检索。
- 查询不仅是全文搜索，还包括筛选、排序、聚合、报表、明细追溯和时间序列处理。
- 写入需要可靠性，不能因为控制面状态 flush 或单 JSON 文件损坏而影响公司业务数据。
- agent 需要一个安全的结构化接口，能录入、查询、分析，但不能绕过审计和权限。

因此，MaClaw 的企业结构化数据能力应作为独立业务数据层建设，而不是放入现有 memory、knowledge brain 或 MaClawSrv 控制面 `state/store.json`。

## 2. 设计目标

1. 支持销售、人力、财务等业务域的数据可靠存储。
2. 支持灵活字段录入，同时逐步沉淀字段定义、类型、约束和版本。
3. 支持大数据量检索：按租户、用户、业务域、数据集、字段、标签、时间、全文条件高效查询。
4. 支持事务写入、幂等导入、批量导入、导入校验和失败回滚。
5. 支持权限控制、字段级脱敏、审计日志和数据访问追踪。
6. 支持数据处理能力：聚合、分组、排序、时间区间统计、导出、报表输入。
7. 支持 agent 友好的 API：既能保存不固定结构，也能查询 schema、生成表单、解释字段含义。
8. MaClawDataSrv 作为独立服务运行，MaClaw 和 MaClawSrv 通过 REST API 访问，不把业务大数据塞入 MaClawSrv 控制面 JSON store。

## 3. 非目标

第一阶段不直接实现完整 ERP、CRM、HRM 或财务总账系统。

第一阶段不替代专业数据库集群，不做跨节点分布式事务。

第一阶段不做任意 SQL 直通给 agent，避免权限绕过和注入风险。

第一阶段不承诺复杂会计准则、薪资税法或全自动财务合规，但提供轻量会计科目和凭证数据对象，先建设可靠数据底座和受控处理接口。

## 4. 总体架构

```text
MaClaw Desktop / Agent / Admin UI
        |
        v
MaClaw Data Client
  - configured endpoint
  - token management
  - tool wrapper
  - local-default discovery
        |
        v
MaClawDataSrv REST API
  - AuthN/AuthZ
  - request validation
  - audit logging
  - rate/size limits
        |
        v
Structured Data Service
  - dataset management
  - schema registry
  - record write/read/query
  - import/export jobs
  - aggregation/query planner
  - field masking policy
        |
        v
Structured Data Store
  - SQLite engine for early/local deployments
  - PostgreSQL engine for team/server deployments
  - records table
  - field index table
  - tag index table
  - FTS index
  - audit/access log
        |
        v
Processing Layer
  - async import
  - schema inference
  - validation
  - metrics/report query
  - materialized views later
```

### 4.1 服务边界

企业结构化数据服务采用独立进程：`MaClawDataSrv`。

MaClawDataSrv 负责：

- 数据集、字段、记录、导入、查询、聚合和审计。
- 数据库连接和迁移。
- 权限校验、字段脱敏和访问日志。
- 对外提供 RESTful API。

MaClaw 负责：

- 在设置页提供“MIS数据” tab。
- 配置 MaClawDataSrv 访问地址、token 和默认业务空间。
- 提供内置 agent 工具，例如 `data.create_dataset`、`data.upsert_record`、`data.query_records`、`data.aggregate_records`。
- 将用户自然语言意图转换为受控数据 API 调用。

MaClawSrv 负责：

- 继续承担 agent runtime、会话、用户、控制面 API。
- 不直接承载企业业务数据。
- 如有需要，只作为 MaClawDataSrv 的普通 REST 客户端。

### 4.2 部署形态

默认本地模式：

```text
MaClaw Desktop
  -> http://127.0.0.1:18180
  -> MaClawDataSrv
  -> SQLite: ~/.maclaw_data/data.db
```

团队服务端模式：

```text
MaClaw Desktop / MaClawSrv
  -> https://data.example.com
  -> MaClawDataSrv
  -> PostgreSQL
```

第一阶段可以提供本地 `MaClawDataSrv` 二进制和 SQLite 引擎。服务端部署和 PostgreSQL 引擎作为同一 API 下的后续扩展。

### 4.3 认证建议

需要 token 认证，即使默认是本地访问也需要。

原因：

- 本地端口也可能被同机其他进程访问。
- HR/财务数据通常包含敏感信息。
- agent 工具调用必须可审计、可撤销。
- 后续从本地切到远程服务时不应改变安全模型。

认证机制建议分三层：

1. **Admin Token**：用于初始化、创建租户、创建服务 token、配置数据库引擎。
2. **Access Token / API Token**：用于 MaClaw 和 agent 日常读写数据。
3. **Scoped Agent API Key**：用于给不同 agent、connector 或员工工具分配不同访问边界。

本地开发默认可以自动生成 token，保存在 MaClaw 配置中，但不能使用硬编码默认 token。生产和多人场景不应让所有 agent 共用一个全权限 token，而应给每个 agent/connector 独立 API key。API key 的权限绑定在 DataSrv 侧，不能依赖客户端传来的 `X-MaClaw-Role` 自称权限。

请求示例：

```http
Authorization: Bearer mcd_xxx
```

API key 配置示例：

```json
[
  {
    "id": "sales-agent",
    "key": "mcd_sales_xxx",
    "tenant_id": "tenant_1",
    "user_id": "agent_sales",
    "role": "data_user",
    "allowed_actions": ["sales.order_upsert", "sales.opportunity_upsert"],
    "allowed_reports": ["sales.order_summary_by_stage"],
    "allowed_dashboards": ["sales.overview"],
    "allow_raw_data": false,
    "allow_sensitive": false,
    "allow_admin": false
  },
  {
    "id": "finance-auditor",
    "key": "mcd_fin_audit_xxx",
    "tenant_id": "tenant_1",
    "user_id": "agent_finance_audit",
    "role": "data_auditor",
    "allowed_datasets": ["finance.expenses", "finance.payments", "finance.vouchers"],
    "allow_sensitive": true,
    "allow_admin": false
  }
]
```

第一版服务支持通过 `MACLAW_DATA_API_KEYS` 注入上述 JSON。匹配到 scoped API key 后，DataSrv 会使用 key 上绑定的 tenant/user/role/policy 覆盖请求头，防止 agent 伪造 `X-MaClaw-Role: data_admin` 或 `data_auditor`。未配置 scoped key 时，保留 `MACLAW_DATA_TOKEN` 作为本地单用户兼容模式。

DataSrv 也应支持托管 API key：管理员通过 `POST /api/v1/data/access/api-keys` 创建 scoped policy，服务端生成或接收一次性 secret，只保存 `sha256` hash 和短 `key_prefix`，列表接口不返回明文 secret。授权范围变更使用 `PATCH /api/v1/data/access/api-keys/{keyId}`；密钥疑似泄露或定期更换时使用 `POST /api/v1/data/access/api-keys/{keyId}/rotate` 轮换 secret，旧 secret 立即失效，新 secret 只返回一次；禁用使用 `DELETE /api/v1/data/access/api-keys/{keyId}`，禁用后该 key 立即不能认证。托管 key 可配置 `expires_at`，用于临时外包、审计、项目型 agent 或短期员工工具授权；到期后即使 key 仍在数据库中也会认证失败。托管 key 成功认证后更新 `last_used_at`、`last_used_ip` 和 `last_used_user_agent`，便于管理员审计哪个 agent 或员工工具最近在使用某个授权。列表和详情返回 `status`，分为 `active`、`expiring_soon`、`expired`、`disabled`，并支持 `GET /api/v1/data/access/api-keys?status=expired` 这类过滤，方便管理员巡检快过期或已失效授权。管理员还可以调用 `GET /api/v1/data/access/api-keys/{keyId}/capabilities` 预览某个 key 实际能看到的业务动作、业务视图、报表、dashboard 和原始 dataset 范围，避免只看 JSON policy 难以判断授权效果；也可以调用 `POST /api/v1/data/access/check` 程序化判断某个 key 是否能执行指定 business action、report、view、dashboard、raw dataset、admin 或 sensitive 访问，用于上线前检查、agent 自检和授权回归测试。`GET /api/v1/data/access/review` 用于周期性授权巡检，汇总 expired、expiring_soon、allow_admin、allow_sensitive、raw_dataset_access、never_used、stale_last_used 等风险，返回 severity、codes、recommended 操作、`by_status` 和 `by_severity` 统计，便于管理员或 agent 生成整改清单；也支持 `min_severity=high` 这类过滤，快速聚焦高风险授权。`GET /api/v1/data/access/remediation-plan` 根据 review finding 生成只读整改计划，把风险转换为 disable、rotate、remove admin/sensitive、restrict raw data、extend expiration 等建议动作，返回目标 endpoint、method、payload、是否 destructive 和所需确认；计划本身不直接执行，执行仍需管理员确认。由于当前 `PATCH` 使用完整 policy 更新语义，remediation plan 返回的 `PATCH` payload 也必须是完整 policy 形状，只改变目标风险字段，避免执行修复建议时意外清空其他业务授权。这样本地小团队可以不重启服务、不改环境变量完成 agent 授权调整；生产部署仍可保留环境变量 key 作为 break-glass 或 bootstrap 通道。

MaClaw 设置页“MIS数据”需要包含：

- 服务地址，默认 `http://127.0.0.1:18180`
- 访问 token
- 连接测试
- 当前后端引擎：SQLite / PostgreSQL
- 默认 tenant/workspace
- 启用/禁用内置数据工具

## 5. 核心概念

### 5.1 Workspace / Tenant

租户或组织边界。所有数据必须带 `tenant_id`，并在查询、导入、聚合时强制作为一级过滤条件。

### 5.2 Domain

业务域，例如：

- `sales`
- `hr`
- `finance`
- `legal`
- `operations`

业务域用于权限、默认字段、导入模板和 UI 分组。

### 5.3 Dataset

类似“业务表”或“集合”，例如：

- `sales.customers`
- `sales.opportunities`
- `sales.orders`
- `hr.employees`
- `hr.attendance`
- `hr.compensation`
- `finance.invoices`
- `finance.expenses`
- `finance.payments`

Dataset 可以先 schema-less，但必须有元数据记录，包括名称、业务域、负责人、权限策略、字段定义版本。

### 5.4 Field Definition

字段定义不是一开始强制完整，但系统需要支持逐步沉淀：

```json
{
  "key": "amount",
  "type": "number",
  "title": "金额",
  "required": true,
  "indexed": true,
  "sensitive": false,
  "unit": "CNY"
}
```

字段类型建议支持：`string`、`number`、`integer`、`float`、`decimal`、`money`、`boolean`、`date`、`datetime`、`enum`、`object`、`array`、`record_ref`、`person_ref`、`org_ref`、`file_ref`。第一阶段 `record_ref` 通过字段 `config.ref_dataset` 描述目标 dataset，例如销售订单的 `customer_ref -> sales.customers`、销售订单的 `contact_ref -> sales.contacts`、销售订单的 `opportunity_ref -> sales.opportunities`、销售联系人和销售商机的 `customer_ref -> sales.customers`、发票的 `contract_ref -> legal.contracts`、薪资和请假的 `employee_ref -> hr.employees`。SQLite MVP 先做类型和引用形状校验，并通过 `quality_checks.relationship_refs` 扫描引用存在性；不做数据库级外键强约束，避免早期模板演进时卡死。后续可在 PostgreSQL 引擎中按租户策略升级为可选外键/物化关系索引。

### 5.5 Record

Record 是一条业务数据。固定元数据和灵活 JSON 数据分离：

```json
{
  "id": "record_xxx",
  "tenant_id": "tenant_xxx",
  "dataset_id": "finance.expenses",
  "title": "2026-04 差旅报销",
  "tags": ["expense", "travel"],
  "data": {
    "employee": "张三",
    "amount": 1250.50,
    "currency": "CNY",
    "department": "Sales",
    "occurred_at": "2026-04-12"
  },
  "created_at": "...",
  "updated_at": "..."
}
```

### 5.6 Record Revision

公司级数据需要可追溯。更新不能只覆盖，应该记录 revision：

- 谁修改
- 什么时候修改
- 修改前后摘要
- 原始导入来源
- 请求 ID / job ID

MVP 已引入 `record_revisions` 表，记录 create/update/delete/import/event.* 的记录快照。查询 revisions 时继续遵守字段脱敏规则，普通 `data_user` 不能通过历史版本绕过敏感字段保护。后续可以在 PostgreSQL 阶段增加 diff、压缩、版本号和按字段回滚。

## 6. 存储设计

### 6.1 第一阶段：SQLite Engine

适合单机、私有化、小团队或边缘部署。

默认本地路径：

```text
~/.maclaw_data/data.db
```

也可通过环境变量覆盖：

```text
MACLAW_DATA_ROOT=/data/maclaw-data
MACLAW_DATA_DB_ENGINE=sqlite
MACLAW_DATA_SQLITE_PATH=/data/maclaw-data/data.db
```

关键配置：

- WAL 模式
- busy timeout
- foreign keys
- 单 writer，多 reader
- 定期 checkpoint
- 定期备份

核心表：

```sql
datasets(
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  domain TEXT NOT NULL,
  name TEXT NOT NULL,
  title TEXT,
  description TEXT,
  schema_version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
)

field_definitions(
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  dataset_id TEXT NOT NULL,
  field_key TEXT NOT NULL,
  type TEXT NOT NULL,
  title TEXT,
  required INTEGER NOT NULL DEFAULT 0,
  indexed INTEGER NOT NULL DEFAULT 0,
  sensitive INTEGER NOT NULL DEFAULT 0,
  config_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(tenant_id, dataset_id, field_key)
)

records(
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  dataset_id TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  data_json TEXT NOT NULL,
  source_id TEXT,
  created_by TEXT,
  updated_by TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
)

record_field_index(
  tenant_id TEXT NOT NULL,
  dataset_id TEXT NOT NULL,
  record_id TEXT NOT NULL,
  field_key TEXT NOT NULL,
  value_text TEXT,
  value_number REAL,
  value_time TEXT,
  PRIMARY KEY(record_id, field_key)
)

record_tags(
  record_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  tag_norm TEXT NOT NULL,
  PRIMARY KEY(record_id, tag_norm)
)

record_fts USING fts5(record_id UNINDEXED, tenant_id UNINDEXED, dataset_id UNINDEXED, text)
```

必要索引：

```sql
CREATE INDEX idx_records_scope ON records(tenant_id, dataset_id, created_at, id);
CREATE INDEX idx_field_text ON record_field_index(tenant_id, dataset_id, field_key, value_text);
CREATE INDEX idx_field_number ON record_field_index(tenant_id, dataset_id, field_key, value_number);
CREATE INDEX idx_field_time ON record_field_index(tenant_id, dataset_id, field_key, value_time);
CREATE INDEX idx_tags_lookup ON record_tags(tag_norm, record_id);
```

### 6.2 第二阶段：PostgreSQL Engine

当数据量、并发、多人协作和备份要求上升时，增加 PostgreSQL 实现。

SQLite 仍保留用于本地版、开发版、单机版。

PostgreSQL 阶段可以进一步引入：

- JSONB
- GIN index
- row-level security
- materialized views
- read replica
- partitioning

## 7. 查询能力设计

### 7.1 基础查询

API 不暴露任意 SQL，而是提供受控查询 DSL：

```json
{
  "dataset_id": "finance.expenses",
  "filter": {
    "op": "and",
    "filters": [
      {"field": "department", "op": "eq", "value": "Sales"},
      {"field": "amount", "op": "gte", "value": 1000},
      {"field": "occurred_at", "op": "between", "value": ["2026-01-01", "2026-03-31"]}
    ]
  },
  "sort": [{"field": "occurred_at", "direction": "desc"}],
  "limit": 100
}
```

为兼容早期设计示例，也接受以下简写：

```json
{
  "filter": {
    "and": [
      {"field": "department", "op": "eq", "value": "Sales"},
      {"field": "amount", "op": "gte", "value": 1000},
      {"field": "occurred_at", "op": "between", "value": ["2026-01-01", "2026-03-31"]}
    ]
  }
}
```

支持操作符：`eq`、`neq`、`in`、`not_in`、`contains`、`not_contains`、`prefix`、`gt`、`gte`、`lt`、`lte`、`between`、`exists`、`not_exists`、`empty`、`not_empty`、`and`、`or`。文本匹配默认大小写不敏感；`empty` 表示字段缺失或字段文本为空，`not_empty` 表示字段存在且非空；`gt/gte/lt/lte/between` 支持数字和 `date/datetime` 字段；所有过滤仍通过字段索引和参数占位符执行，不暴露 SQL。

查询 DSL 有明确复杂度上限：单个 `and/or` 最多 20 个子条件，`in/not_in` 最多 100 个值，排序字段最多 5 个。超过上限返回 `invalid input`，由 agent 或 Web Console 缩小查询范围后重试。

字段索引支持顶层字段、嵌套对象点路径和标量数组多值索引。例如 record data 中的 `{ "approval": { "assigned_to": "Dana", "status": "approved" }, "watchers": ["Dana", "Ops"] }` 会同时索引 `approval`、`approval.assigned_to`、`approval.status`，并把 `watchers` 的每个标量元素作为同一字段的可匹配值，因此可以用 `{"field":"approval.assigned_to","op":"eq","value":"Dana"}` 或 `{"field":"watchers","op":"eq","value":"Ops"}` 查询。第一阶段不展开对象数组，数组对象的复杂分析交给后续 processing layer。

多值字段排序使用稳定代表值：升序取该字段所有索引值中的最小值，降序取最大值；负向过滤（`neq/not_in/not_contains`）表示字段存在且没有任何索引值命中被排除条件，避免数组中某个非匹配元素把整条 record 错误放行。空数组不写容器文本索引，数组中的 `null` 元素也不单独索引，因此 `exists/not_empty` 只表示字段至少有一个可索引元素，`empty` 会覆盖字段缺失、空数组或只有 `null` 元素的数组。字段值本身为 `null` 时会以空文本索引保留字段存在性，`exists` 可命中，`not_empty` 不命中。

单条 record 展开的索引字段数最多 300 个，超过上限会拒绝写入，避免外部系统或 agent 把过大的嵌套 JSON 直接塞入本地 SQLite 索引层。需要承载更深或更宽对象时，应拆成多个业务 dataset 或进入后续 processing layer。

### 7.2 全文检索

全文检索只作为一种条件，不替代字段查询：

```json
{
  "dataset_id": "sales.customers",
  "q": "华东 合同",
  "limit": 20
}
```

### 7.3 聚合查询

用于报表和 agent 分析：

```json
{
  "dataset_id": "finance.expenses",
  "metrics": [{"op": "sum", "field": "amount", "as": "total_amount"}],
  "group_by": ["department"],
  "filter": {"field": "occurred_at", "op": "between", "value": ["2026-01-01", "2026-03-31"]},
  "sort": [{"field": "total_amount", "direction": "desc"}],
  "limit": 100,
  "scan_limit": 5000
}
```

第一阶段支持 `count/count_distinct/sum/avg/min/max/group_by/filter/sort/limit/scan_limit`。`group_by` 和 metric 字段支持与查询一致的嵌套点路径；标量数组字段用于 `group_by` 时，一条 record 会归入每个标量元素对应的分组，多个数组分组字段会做组合展开，因此分组 `count` 总和可能大于 `scanned`。`count_distinct` 会按字段值去重，标量数组按元素去重，缺失或空数组不计入 distinct；当 metric 字段与当前 group 字段相同，`count_distinct/sum/avg/min/max` 使用当前分组值计算，避免数组字段每个分组重复统计整条 record 的全部数组元素。单条 record 最多展开 50 个聚合分组归属，超过上限返回 `invalid input`。`sort` 作用于聚合后的 group 字段或 metric 名称；`limit` 只限制返回的聚合分组行数；`scan_limit` 限制最多扫描的匹配 records，响应中会返回 `scanned`、`scan_limit` 和 `truncated`，当 `truncated=true` 时表示统计只覆盖扫描上限内的数据。复杂计算交给后续 processing layer。

聚合 DSL 也有受控上限：`group_by` 最多 5 个字段，`metrics` 最多 10 个，聚合结果排序最多 5 个字段，`scan_limit` 默认为 5000 且第一阶段最大为 5000。

## 8. API 设计

MaClawDataSrv 默认监听：

```text
127.0.0.1:18180
```

公开 REST API 前缀：

```text
/api/v1
```

系统 API：

```text
GET /health
GET /readyz
GET /version
GET /api/v1/openapi.json
POST /api/v1/auth/tokens
```

### 8.1 Template API

企业 MIS 系统种类有限，第一入口应是模板创建，而不是让员工从零定义结构。模板由 MaClawDataSrv 维护，覆盖销售、客户、人事、薪酬、请假、费用、预算、发票、合同、采购、库存、固定资产等常见业务对象。用户可以按模板创建 dataset，再对字段做受控微调。

```text
GET  /api/v1/data/templates
GET  /api/v1/data/templates/{templateId}
POST /api/v1/data/templates/{templateId}/create
POST /api/v1/data/templates/bootstrap
```

模板创建会同时创建 dataset 和字段定义，agent 和 Web Console 都应优先使用该 API。`templates/bootstrap` 会创建常用企业 MIS 模板，已存在的 dataset 会跳过，适合首次启用、演示环境初始化，或 agent 为本地公司数据服务准备标准销售/财务/人事/法务/采购/库存/固定资产结构。Bootstrap 支持 `dry_run=true`，用于在真正创建 schema 前预览 `would_create/skipped/errors`；也支持 `domains` 或 `template_ids` 限定初始化范围，例如先上线 `sales,finance`，后续再补 HR、采购、库存和资产。

### 8.1.1 Business Domain Catalog API

```text
GET /api/v1/data/domains
GET /api/v1/data/domains/{domain}
GET /api/v1/data/relationships
POST /api/v1/data/intent/resolve
```

业务域目录是 MaClaw/agent 的 MIS 导航入口。它按 `sales/finance/hr/legal/procurement/inventory/assets` 聚合当前已初始化 dataset、缺失模板、可用 business actions、business views、dashboards 和 reports，并提供 `use_cases` 作为业务意图路由表。每个 use case 包含中英文意图提示、首选 business action、首选 view/report/dashboard，以及是否建议先 dry-run。`intent/resolve` 可把自然语言业务请求解析为候选 use case，例如“查低库存”会匹配 `inventory.stock_update` 并推荐 `inventory.stock_overview` / `inventory.low_stock_summary`。解析结果还会返回 `next_steps`：如果业务域尚未初始化，会先建议 `bootstrap_templates` dry-run；写入类 use case 会建议 `execute_business_action` dry-run，然后再执行正式业务动作；查询和分析类请求会给出首选 view/report/dashboard。写入步骤会附带 `required_fields`、`input_fields`、`data_template`、完整 REST `body_template` 和 MaClaw `tool_call_template`，让 agent 能生成表单或请求草稿、检查缺失字段，并用业务动作 dry-run 做最终校验，而不是猜测 payload。agent 在收到“处理采购订单”“查库存预警”“登记固定资产”这类业务请求时，应先通过 `resolve_intent` 或 `get_domain` 理解该业务域的可操作能力，再选择业务动作、视图、报表或初始化建议。

`relationships` 是业务对象关系目录。DataSrv 会从已初始化字段和内置模板中的 `record_ref/person_ref/org_ref/file_ref` 字段生成关系提示，例如 `sales.orders.customer_ref -> sales.customers`、`sales.orders.contact_ref -> sales.contacts`、`sales.orders.opportunity_ref -> sales.opportunities`、`sales.contacts.customer_ref -> sales.customers`、`sales.opportunities.customer_ref -> sales.customers`、`procurement.purchase_orders.supplier_ref -> procurement.suppliers`、`finance.payments.supplier_ref -> procurement.suppliers`、`inventory.movements.item_ref -> inventory.items`、`inventory.movements.to_warehouse_ref -> inventory.warehouses`、`finance.invoices.contract_ref -> legal.contracts`、`hr.payroll.employee_ref -> hr.employees`。这让 MaClaw/agent 可以知道哪些数据对象天然相关，用于解释、下钻、生成表单和后续报表组合，但仍不暴露原始 SQL join。

### 8.2 Dataset API

```text
GET    /api/v1/data/datasets
POST   /api/v1/data/datasets
GET    /api/v1/data/datasets/{datasetId}
PATCH  /api/v1/data/datasets/{datasetId}
DELETE /api/v1/data/datasets/{datasetId}
```

### 8.3 Field API

```text
GET   /api/v1/data/datasets/{datasetId}/fields
PUT   /api/v1/data/datasets/{datasetId}/fields
POST  /api/v1/data/datasets/{datasetId}/infer-schema
```

### 8.4 Record API

```text
GET    /api/v1/data/datasets/{datasetId}/records
POST   /api/v1/data/datasets/{datasetId}/records
GET    /api/v1/data/datasets/{datasetId}/records/{recordId}
GET    /api/v1/data/datasets/{datasetId}/records/{recordId}/related
GET    /api/v1/data/datasets/{datasetId}/records/{recordId}/revisions
GET    /api/v1/data/datasets/{datasetId}/records/{recordId}/timeline
PATCH  /api/v1/data/datasets/{datasetId}/records/{recordId}
DELETE /api/v1/data/datasets/{datasetId}/records/{recordId}
POST   /api/v1/data/datasets/{datasetId}/records/query
POST   /api/v1/data/datasets/{datasetId}/records/export.csv
POST   /api/v1/data/datasets/{datasetId}/records/export.jsonl
POST   /api/v1/data/datasets/{datasetId}/records/aggregate
```

`related` 基于 `relationships` 中的 `record_ref` 关系返回单条业务对象的出向和入向关联记录，例如从销售订单下钻到客户，或从客户反查关联销售订单。该接口仍走受控 record API、字段脱敏和 limit，不暴露任意 SQL join。`revisions` 面向“这条记录的数据版本如何变化”，`timeline` 面向“这个业务对象发生过什么”。Timeline 会按时间合并 record revision、ingest event 和 audit log，返回 revision/event/audit 三类节点，供 agent 在解释销售订单、报销单、员工档案或合同变更时追溯来源、操作者、外部事件、幂等键和审计摘要。普通业务查询仍优先走 business action/view/report；只有需要解释单个对象关系、变更历史、排查同步问题或恢复误删时才读取 related/timeline。

### 8.5 Import / Export API

```text
POST /api/v1/data/import-jobs
GET  /api/v1/data/import-jobs/{jobId}
POST /api/v1/data/import-jobs/{jobId}/cancel
GET  /api/v1/data/export-jobs
GET  /api/v1/data/export-jobs/{jobId}
GET  /api/v1/data/export-jobs/{jobId}/download
POST /api/v1/data/datasets/{datasetId}/export
```

导入应支持 CSV、Excel、JSONL，后续支持数据库连接器和 SaaS API 同步。

MVP 已支持 header-based CSV 导入：

```text
GET  /api/v1/data/datasets/{datasetId}/records/import-template.csv
POST /api/v1/data/datasets/{datasetId}/records/import.csv
POST /api/v1/data/datasets/{datasetId}/records/import.jsonl
POST /api/v1/data/datasets/{datasetId}/records/batch/jobs
POST /api/v1/data/datasets/{datasetId}/records/import.csv/jobs
POST /api/v1/data/datasets/{datasetId}/records/import.jsonl/jobs
POST /api/v1/data/datasets/{datasetId}/records/export.csv/jobs
POST /api/v1/data/datasets/{datasetId}/records/export.jsonl/jobs
GET  /api/v1/data/import-jobs
GET  /api/v1/data/import-jobs/{jobId}
GET  /api/v1/data/export-jobs
GET  /api/v1/data/export-jobs/{jobId}
GET  /api/v1/data/export-jobs/{jobId}/download
```

`import-template.csv` 根据当前 dataset 字段定义和已观察到的数据键生成导入表头，方便员工或 agent 先拿模板再填数。CSV 第一行为字段名，`id`、`title`、`tags`、`source_id` 作为元字段处理，其余列写入 record `data`。接口支持 JSON body 的 `{ "csv": "...", "dry_run": true }`，也支持直接提交 `text/csv` body，并复用现有 batch validation、唯一键检查、审计和 record revision。

JSONL/NDJSON 用于 agent、外部系统或 ETL 同步。每一行可以是完整 record envelope，例如 `{ "id": "...", "title": "...", "tags": ["sync"], "source_id": "...", "data": {...} }`；也可以直接是一条业务数据对象，服务会把 `id/title/tags/source_id` 作为元字段，其余键写入 record `data`。接口支持 JSON body 的 `{ "jsonl": "...", "dry_run": true }`，也支持直接提交 `application/x-ndjson`、`application/jsonl` 或 `text/plain` body。

`export.csv` 面向人工核对、Excel 交接和传统表格流程；`export.jsonl` 面向 agent、外部系统、ETL 和事件式同步。两者都复用 `records/query` 的受控过滤、分页上限和角色脱敏策略，不暴露 SQL，也不会绕过 tenant 边界。默认记录列表按 `created_at DESC, id DESC` 返回；响应提供 `next_before` 与 `next_before_id`，客户端下一页同时传 `before` 和 `before_id`，可以在大量记录具有相同创建时间时继续稳定翻页。自定义排序仍使用显式 sort 语义，后续再扩展字段级游标。

小批量导入可直接调用 `import.csv`、`import.jsonl` 或 `records/batch` 获得同步结果；较大导入应调用 `import.csv/jobs`、`import.jsonl/jobs` 或 `records/batch/jobs` 创建 import job，再用 `GET /api/v1/data/import-jobs/{jobId}` 查询 `queued/running/completed/failed` 状态和最终校验/导入结果。同步 `export.csv/export.jsonl` 默认最多导出 5000 条；较大导出应调用 `export.csv/jobs` 或 `export.jsonl/jobs` 创建 export job，job 默认最多导出 50000 条，并使用 `before/before_id` 跨 500 条内部分页稳定扫描默认排序结果，再通过 `GET /api/v1/data/export-jobs/{jobId}` 获取状态和 `download_path`，完成后用 `download` 接口下载产物。MVP 的 job 执行器先以内存后台 goroutine 运行，状态持久化到数据库；后续可替换为独立 worker、队列和分片导入导出，不影响 MaClaw/agent 的调用方式。

### 8.6 Business Action API

MaClaw/agent 面向用户时不应暴露底层 dataset CRUD，而应优先暴露业务动作目录。DataSrv 负责声明“我能做哪些企业 MIS 操作”，MaClaw 通过工具发现这些动作，再按动作要求提交业务数据。

```text
GET  /api/v1/data/business-actions
GET  /api/v1/data/business-actions/{actionId}
POST /api/v1/data/business-actions/{actionId}/execute
GET  /api/v1/data/business-rules
POST /api/v1/data/business-rules/evaluate
```

`business-rules` 是内置业务治理规则目录，用于告诉 MaClaw/agent 某类业务写入是否需要 dry-run、审批、备份、质量检查或 data_admin 权限。规则可以是动作级规则，也可以带 `conditions` 做条件化触发，例如销售订单只有在 `amount >= 100000` 时才进入大额订单审批意识流程，普通订单保持 `clear`。条件支持 `all/any` 组合、嵌套字段路径，以及 `gt/gte/lt/lte/eq/neq/contains/in/not_in/exists/not_exists/empty/not_empty` 等受控操作符，不暴露 SQL。`business-rules/evaluate` 可按 `business_action_id`、`dataset_id`、`record_id` 和样例数据生成治理状态和下一步工具调用模板。返回的 `governance_status` 分为 `clear`、`needs_review`、`blocked_for_admin`：`clear` 表示普通业务动作可以继续；`needs_review` 表示应先执行返回的 dry-run、审批、质量检查或备份步骤；`blocked_for_admin` 表示当前角色不能直接执行，必须切换到 `data_admin` 或提交人工复核。这样 agent 在替代传统 MIS 系统时，不只是知道怎么写数据，也知道哪些业务动作必须先预检、提交审批、做质量检查或创建备份。

示例动作：

```text
sales.order_upsert
sales.order_status_update
sales.customer_upsert
finance.expense_submit
finance.expense_status_update
finance.invoice_upsert
finance.invoice_status_update
hr.employee_upsert
hr.employee_status_update
legal.contract_upsert
legal.contract_status_update
```

业务动作包含业务标题、目标 dataset、事件类型、操作类型、必填字段、建议标签和输入字段说明。执行动作时，如果目标 dataset 尚未创建，DataSrv 可以按对应模板自动创建结构，再把业务输入转换成标准事件和 record 变更。状态类动作使用 `merge_record` 语义，只改本次提交字段并保留原记录其他字段，避免 agent 为了改一个状态而重写整条业务数据。

`execute_business_action` 支持 `dry_run=true`，用于在真正写入前返回 `validation` 和 `preview`；无论 dry-run 还是正式执行，结果都会携带 `rules`，其中包含 `governance_status`、`status_reasons`、`can_execute_now`、`recommended_action`、`gate_statuses`、`matched_rules`、`rule_evaluations` 和 `next_steps`。`rule_evaluations` 会列出每条候选规则是否适用、条件组合方式、每个条件的 actual value、匹配结果和未匹配原因，方便 agent 或员工解释“为什么这笔业务需要审批/备份/质量检查”。`gate_statuses` 将 dry-run、backup、quality、approval、admin 等治理门标记为 `pending/complete/blocked`；`recommended_action` 给出当前最应该执行的下一步。`next_steps` 不只是提示文字，还会携带可执行草稿参数，例如审批的 `kind/priority/summary/request/assigned_to`、备份 `note`、质检 `checks` 和 dry-run 的业务动作 payload。内置规则会按业务域给出默认审批人建议，例如 `finance_manager`、`sales_manager`、`hr_manager`、`procurement_manager`，实施时可以映射到企业真实账号或审批组。agent 对不确定的销售、财务、人力、采购等业务变动应先 dry-run，再根据验证结果和规则治理状态决定是否继续执行或转为审批/operation plan。

MVP 内置业务动作覆盖组织部门、客户、销售联系人、销售商机、销售订单、费用、预算、发票、收付款、会计科目、会计凭证、员工、薪资、请假/休假申请、合同、供应商、采购订单、库存仓库、库存物料、库存流水/库存变更、固定资产登记/状态变更。后续新增业务域时，应优先补 template + business action + business view + report，而不是让 agent 直接维护底层 schema。

这层是 MaClaw 替代传统 MIS 操作入口的关键：用户说“登记一笔费用报销”或“更新销售订单状态”，agent 应调用业务动作，而不是手写 schema 或 SQL。

### 8.6.1 Business View API

人类员工和 agent 查询业务数据时，不应总是直接面对底层 dataset 全字段。DataSrv 提供业务视图目录，由服务声明常见员工工作场景的字段投影、默认排序、默认 limit 和可选过滤。

```text
GET  /api/v1/data/views
GET  /api/v1/data/views/{viewId}
POST /api/v1/data/views/{viewId}/query
```

示例视图：

```text
sales.order_overview
sales.customer_directory
finance.expense_review
finance.invoice_status
hr.employee_roster
legal.contract_register
```

视图查询仍走受控 `QueryRecords`，不暴露 SQL；返回结果只包含视图声明字段，并继续遵守角色脱敏策略。响应在 `has_more=true` 时返回 `next_before`；当当前查询使用默认 `created_at DESC, id DESC` 排序时还会返回 `next_before_id`，下一页可在相同查询体中回传 `before/before_id`。带业务默认排序或自定义排序的视图，第一阶段仍把 `next_before` 作为时间过滤游标，字段级稳定游标留给后续扩展。agent 做员工可读的数据查询时，优先使用 `query_business_view`；只有需要跨字段探索、维护或导出时再使用 `query_records` / `export_records`。

### 8.6.2 Business Approval API

企业 MIS 常见对象需要业务审批，例如费用报销、采购订单、合同、固定资产状态变更、关键销售订单确认。审批不是 schema 治理，也不是底层数据库事务直通，而是围绕一条业务 record 的受控状态记录：

```text
GET  /api/v1/data/approvals
GET  /api/v1/data/approvals/{approvalId}
POST /api/v1/data/approvals/{approvalId}/review
POST /api/v1/data/datasets/{datasetId}/records/{recordId}/approvals
```

`create_record_approval` 由普通业务 agent 或员工提交，状态为 `pending`；`review_record_approval` 由 `data_admin` 执行，决策为 `approve` 或 `reject`。审批请求记录 kind、priority、assigned_to、due_at、summary、request、created_by、reviewed_by、reason 和时间戳，并写入 audit log。Inbox 可按 assignee 和 overdue 过滤审批，逾期审批会提高优先级，方便 agent 或员工做 SLA 跟进。单条 record timeline 会合并 approval 节点，方便解释“这笔费用为什么通过/驳回”“这份合同是谁审批的”。后续可以在此基础上扩展多级审批、审批人规则、SLA、通知和电子签。

为避免 agent 重试或员工重复点击造成审批噪音，`create_record_approval` 对同一 dataset、record、kind 的 pending 审批执行复用：如果已有未处理审批，服务返回原审批并设置 `reused: true`，同时写入 `approval.reuse` 审计记录，而不是再创建一张重复审批单。

### 8.7 Capabilities API

MaClaw 不应靠硬编码猜测 DataSrv 里有什么业务能力。DataSrv 提供能力清单，作为 agent 开始 MIS 工作前的发现协议：

```text
GET /api/v1/data/capabilities
GET /api/v1/data/inbox
GET /api/v1/data/inbox/summary
GET /api/v1/data/stats
GET /api/v1/data/dashboards
GET /api/v1/data/dashboards/{dashboardId}
POST /api/v1/data/dashboards/{dashboardId}/run
POST /api/v1/data/maintenance/run
```

返回内容包括：

- 当前 tenant、user、role 和后端 engine。
- 服务策略：优先业务动作、报表/聚合分析、schema proposal 确认门、禁止 SQL、敏感字段脱敏、业务唯一键、备份恢复建议。
- 已存在 datasets 与字段定义。
- 可用 relationships：由引用字段生成的业务对象关系提示。
- 可用 templates。
- 可用 business actions。
- 可用 business views。
- 可用 dashboards。
- 可用 reports。
- `agent_playbooks`：按业务 use case 组织的 agent 操作剧本，包含领域、意图提示、推荐读写顺序、`next_steps`、REST `body_template` 和 MaClaw `tool_call_template`。
- MaClaw `mis_data` 工具 action 清单及推荐用法。

MaClaw agent 对非简单 MIS 任务应先调用 `mis_data.get_capabilities`，再结合 `agent_playbooks` 或 `resolve_intent` 的结果选择 `run_dashboard`、`execute_business_action`、`query_business_view`、`run_report`、`aggregate_records`、`query_records` 或 `export_records`。写入类 playbook 默认包含 dry-run 步骤和正式执行步骤，agent 应优先使用 `tool_call_template` 形成工具调用草稿，再用业务 action 的 dry-run 做最终校验。只有明确进行结构治理时才进入 schema proposal / apply 流程。

`inbox` 是 agent 和员工的 MIS 待办入口，聚合 pending approvals、pending operation plans、failed import/export jobs 和最新 quality issues。MaClaw 在开始企业数据维护、日常运营巡检或员工打开 Web Console 时，可以先调用 `get_inbox_summary` 获取按类型、严重度、状态和 overdue 统计的概览，再调用 `get_inbox` 展开具体审批、批量操作复核、失败任务或质量问题。

`dashboard` 是公司和业务域总览入口。第一阶段不引入新的图表配置表，而是由 DataSrv 内置 company/sales/finance/hr/legal/procurement/inventory/assets 等 dashboard 定义，把 `stats`、`inbox_summary` 和常用 report 组合为一个稳定响应。agent 在回答“公司 MIS 今天情况如何”“销售现在怎么样”“财务有哪些异常”时优先调用 `run_dashboard`，再下钻到具体 report、business view、record 或 inbox item。

`stats` 面向运维和 agent 自检，返回 schema version、dataset/record/field 计数、import/export job 状态分布、quality run/audit/backup 数量、数据库文件大小和各 dataset 的记录量。agent 在大批量导入、备份恢复、质量清理前后可以调用 `get_stats` 做前后对比。

`maintenance/run` 面向 data_admin 级运维操作，SQLite 阶段支持 `integrity_check`、`optimize`、`vacuum`。agent 在大批量导入、导出、恢复或数据清理后，可以先 `create_backup`，再按需执行维护任务，并把结果写入审计日志。普通 `data_user` 不允许执行维护任务。

### 8.8 Event API

MaClaw 不直接猜测数据库结构。外部 CRM、ERP、HR、财务系统发生业务变动时，应通过 DataSrv 的事件入口提交结构化业务事件，由 DataSrv 按 dataset/fields 契约执行受控写入。

```text
GET  /api/v1/data/events
POST /api/v1/data/events
GET  /api/v1/data/events/dead-letter
GET  /api/v1/data/events/dead-letter/{deadLetterId}
POST /api/v1/data/events/dead-letter/{deadLetterId}/retry
POST /api/v1/data/events/dead-letter/{deadLetterId}/resolve
GET  /api/v1/data/event-contracts
GET  /api/v1/data/event-contracts/{businessActionId}
GET  /api/v1/data/connectors
POST /api/v1/data/connectors
GET  /api/v1/data/connectors/health
GET  /api/v1/data/connectors/{connectorId}
PUT  /api/v1/data/connectors/{connectorId}
POST /api/v1/data/connectors/{connectorId}/test
POST /api/v1/data/connectors/{connectorId}/config/validate
POST /api/v1/data/connectors/{connectorId}/readiness
GET  /api/v1/data/connectors/{connectorId}/health
GET  /api/v1/data/connectors/{connectorId}/sync-state
POST /api/v1/data/connectors/{connectorId}/sync-state
GET  /api/v1/data/connectors/{connectorId}/sync-runs
POST /api/v1/data/connectors/{connectorId}/sync-plan
POST /api/v1/data/connectors/{connectorId}/sync-batch
POST /api/v1/data/connectors/{connectorId}/config/patch
POST /api/v1/data/connectors/{connectorId}/mappings/suggest
POST /api/v1/data/connectors/{connectorId}/events/preview
POST /api/v1/data/connectors/{connectorId}/events
```

原始事件示例：

```json
{
  "source": "crm",
  "event_type": "sales.order.updated",
  "operation": "upsert_record",
  "dataset_id": "sales.orders",
  "record_id": "SO-2026-0001",
  "idempotency_key": "crm:sales.orders:SO-2026-0001:v12",
  "title": "Sales order SO-2026-0001",
  "tags": ["crm"],
  "data": {
    "order_no": "SO-2026-0001",
    "customer": "Acme",
    "amount": 8800,
    "stage": "confirmed"
  }
}
```

业务动作事件示例：

```json
{
  "source": "crm",
  "business_action_id": "sales.order_status_update",
  "record_id": "SO-2026-0001",
  "idempotency_key": "crm:sales.order_status:SO-2026-0001:v12",
  "data": {
    "stage": "won",
    "payment_status": "paid"
  }
}
```

第一阶段支持两种事件模式。原始模式使用 `operation`、`dataset_id` 和 `event_type`，支持 `upsert_record`、`merge_record` 与 `delete_record`；业务动作模式使用 `business_action_id`，由 DataSrv 根据动作目录自动确定目标 dataset、事件类型、写入语义和建议标签，目标 dataset 尚未初始化时可按模板自动创建。状态类外部事件应优先使用业务动作模式，例如销售订单阶段变化、费用状态变化、采购订单状态变化、固定资产状态变化；这样 CRM、ERP、HR connector 和 MaClaw agent 不需要知道底层字段维护细节，也不会为了改一个状态覆盖整条记录。

非空 `idempotency_key` 会写入独立 `data_events` 事件日志；业务动作模式还会保存 `business_action_id`，用于事件列表过滤、record timeline 追溯和重复投递返回。相同 tenant 下重复提交同一个 `idempotency_key` 时，DataSrv 返回 `duplicate`，不会再次写 record，从而支持 CRM、ERP、HR connector 和 agent 的安全重试。`GET /api/v1/data/events` 可按 dataset、source、event_type、business_action_id、idempotency_key 查询处理历史，用于排查重复投递和外部系统同步状态。后续应增加 dead-letter queue、connector retry、字段校验失败报告和更细粒度审批流。

`POST /api/v1/data/events` 支持 `dry_run=true`。Dry-run 会返回 `valid`、`validation` 和 `preview`，但不会写 record，也不会写 `data_events` 日志。connector 初次接入、字段映射变更、批量同步前抽样校验，或 agent 对外部业务事件不确定时，应先 dry-run，再正式投递带幂等键的事件。

`event-contracts` 是外部系统接入契约目录，由 DataSrv 根据 business action 自动生成。每个 contract 包含目标 endpoint、connector_endpoint_template、method、business_action_id、event_type、operation、dataset_id、required_fields、input_fields、suggested_tags、data_template、dry_run_body_template、commit_body_template、idempotency_template 和 recommended_flow。外部 connector 不应手写 dataset/operation，而应先读取 `GET /api/v1/data/event-contracts/{businessActionId}`，再按 `dry_run_body_template` 做预检，最后按 `commit_body_template` 正式投递。注册过的 connector 优先使用 `connector_endpoint_template`，即 `/api/v1/data/connectors/{connectorId}/events`；未登记的临时外部系统才使用通用 `/api/v1/data/events`。Web Console 的业务动作面板和连接器面板都可一键载入 event contract，方便员工或实施人员验证接入 payload。

`connectors` 是外部系统接入治理目录，用于登记 CRM、ERP、HRIS、财务、仓储或资产系统的连接器元数据、所属业务域、系统类型、base_url、认证方式、secret 引用名和订阅的 business actions。DataSrv 不保存原始密钥，只保存 `token_ref` 这类 secret 引用。`test` 接口不会调用远端系统，而是校验该 connector 订阅的业务动作是否都能解析为 event contract，并返回每个动作的 dry-run/commit 契约，供 agent 或实施人员在真正配置 webhook/同步任务前做静态检查。connector 写入属于集成管理，应要求 `data_admin`；普通业务变更仍走 `ingest_event` 的 dry-run/commit 流程。

注册过的 connector 推荐使用 `POST /api/v1/data/connectors/{connectorId}/events` 投递事件。该入口会先读取 connector，确认它处于 enabled 状态，并确认本次 `business_action_id` 在 `subscribed_actions` 内；如果 payload 没有显式 `source`，服务会使用 connector 的 `kind` 或 `id` 作为 source。这样 CRM connector 不能误写 HR/财务动作，HRIS connector 也不能绕过订阅关系直接投递销售订单事件。校验通过后仍复用标准 `ingest_event` 逻辑、幂等键、dry-run、dead-letter 和审计流程。

connector 可在 `config.field_mappings` 中配置外部字段到业务字段的轻量映射，避免外部系统必须按 DataSrv 字段名组装 payload。例如：

```json
{
  "field_mappings": {
    "sales.order_upsert": {
      "order_no": "crm_id",
      "customer": "account.name",
      "amount": "totals.amount"
    }
  }
}
```

当 `sales.crm` 通过 connector endpoint 投递 `sales.order_upsert` 时，DataSrv 会从外部 payload 中读取 `crm_id`、`account.name`、`totals.amount`，生成标准业务数据 `{ "order_no": "...", "customer": "...", "amount": ... }`。如果请求未显式传 `record_id` 或 `idempotency_key`，服务会基于映射后的业务数据和 action contract 生成 `{order_no}` record id 以及 `crm:sales.order_upsert:{order_no}:v1` 这类幂等键。该映射只支持受控点路径取值，不执行脚本；复杂清洗应在专用 connector 或 ETL 层完成。

`POST /api/v1/data/connectors/{connectorId}/mappings/suggest` 用于根据外部系统样例 payload 和 business action contract 生成字段映射建议。它会展开样例中的点路径，按目标业务字段、必填字段和常见企业命名习惯推断 `field_mappings`，返回 `suggested_mapping`、置信度、未匹配字段和可合并到 connector config 的 `config_patch`。该接口只生成建议，不写入 connector 配置；agent 或实施人员应先用建议更新配置草案，再调用 `events/preview` 验证映射、record id 和 idempotency key。

`POST /api/v1/data/connectors/{connectorId}/config/patch` 用于保存已经审核过的连接器配置补丁。它只深度合并 `config` JSON，不修改 connector 的名称、订阅动作、base_url、auth_type 或 token_ref，适合保存 `mappings/suggest` 产生的 `config_patch`。请求可传 `dry_run=true` 先返回 `previous_config` 与 `patched_config` 对比；正式写入需要 `data_admin`，并写入审计日志。agent 应优先使用该接口保存映射/同步参数补丁，而不是整条 `PUT connector` 覆盖配置。

`POST /api/v1/data/connectors/{connectorId}/config/validate` 用于上线前验证 connector 配置。第一阶段重点检查 `field_mappings`：配置格式是否正确、每个订阅的 business action 是否覆盖必填字段、映射目标是否属于该业务动作、source path 是否为受控点路径，以及是否存在未订阅 action 的多余映射。返回 `valid`、`issues`、`warnings`、每个 action 的映射摘要和建议下一步。agent 在执行首次全量同步、保存映射补丁或排查 dead-letter 前，应先调用该接口；只有 `valid=true` 且 preview 通过后才进入正式同步。

`POST /api/v1/data/connectors/{connectorId}/readiness` 是连接器上线前总检查。它会组合执行 contract binding test、`config/validate`、health 检查，并可接收 `sample_event` 做一次 connector-scoped preview/dry-run。返回 `ready`、逐项 `checks`、contract test、config validation、health、可选 preview 和 recommended_next。`ready=false` 表示不能开启生产同步；`ready=true` 表示可以先跑 `sync_connector_batch` 的 `dry_run=true` 首页，再逐步放量。

`POST /api/v1/data/connectors/{connectorId}/sync-plan` 用于生成连接器首次或恢复同步的执行计划。请求可包含 `sample_event`、`first_page_events`、`page_size` 和当前 `cursor`；服务会先调用 readiness，再生成读取 sync-state、外部拉取、第一页 dry-run、正式提交、checkpoint 更新、dead-letter 监控和 rollback/pause 的步骤。如果传入 `first_page_events`，服务会直接用 `sync-batch dry_run=true` 预跑第一页并把结果写入 `dry_run_batch`。该接口不写正式业务数据，适合 agent 在真正同步前给出可审计的执行清单。

每个 sync-plan 步骤同时返回 REST endpoint/body 和 `mis_data` 的 `tool_call_template`。MaClaw agent 可以按模板执行 `get_connector_sync_state`、`check_connector_readiness`、`sync_connector_batch`、`update_connector_sync_state` 等受控动作，不需要重新猜测底层数据结构或接口参数。

`POST /api/v1/data/connectors/{connectorId}/events/preview` 用于连接器接入前的映射预览。它会执行订阅校验、字段映射、record/idempotency key 推导和标准 dry-run 校验，但不会写 record、不会写事件日志。返回内容包含 `original_data`、`mapped_data`、`missing_mappings`、`normalized_event` 和 `dry_run_result`，方便实施人员或 agent 在 webhook/同步任务上线前确认外部字段已经正确进入业务动作。

`GET /api/v1/data/connectors/{connectorId}/health` 用于连接器运行态巡检。第一阶段它从最近事件日志和 dead-letter 队列汇总 `status`、`recent_events`、`open_dead_letters`、每个订阅动作的最近成功/失败和建议处理项；agent 可以用它判断 CRM、ERP、HRIS、财务或仓储系统是否正在正常同步。`GET /api/v1/data/connectors/health` 返回所有连接器的健康概览，适合 agent 做周期性巡检或 Web Console 做总览。SQLite 阶段使用服务层汇总，PostgreSQL 阶段可下推为聚合查询或物化视图。

`GET/POST /api/v1/data/connectors/{connectorId}/sync-state` 用于可恢复同步。定时拉取、批量导入或 agent 驱动的外部系统同步可以在每个阶段记录 `status`、`cursor`、`checkpoint`、`synced_records`、`last_error`、`started_at` 和 `finished_at`。状态保存在 connector 的 `config.sync_state` 中，不新增 SQLite 表；健康巡检会读取该状态，如果最近同步为 `failed` 会把 connector 标记为 `degraded`，并提示从保存的 cursor/checkpoint 恢复。

`POST /api/v1/data/connectors/{connectorId}/sync-batch` 用于连接器批量同步。请求体包含 `events`、`dry_run`、`stop_on_error` 和可选 `sync_state`；每条 event 仍复用 connector endpoint 的订阅校验、字段映射、业务动作、幂等、审计和 dead-letter 逻辑。响应返回每条 item 的成功/失败、dead-letter id 和批量汇总，并可在成功后顺便更新 cursor/checkpoint。agent 做 CRM/ERP/HRIS/财务批量拉取时，应先读取 `sync-state`，按 cursor 拉取一页，调用 `sync-batch`，再根据返回的 `sync_state` 或失败项决定继续、重试或进入人工排障。

每次非 dry-run 的 `sync-batch` 会追加一条轻量 `sync_run` 摘要到 connector 的 `config.sync_runs`，最多保留最近 20 条。`GET /api/v1/data/connectors/{connectorId}/sync-runs` 可读取这些历史，字段包括 run id、status、total、succeeded、failed、cursor、error_summary、started_at 和 finished_at。它不保存原始 payload，避免把 connector 配置变成大日志；详细失败 payload 仍在 dead-letter 队列中。

非 dry-run 的事件投递如果校验失败、字段类型不匹配、动作未知或目标结构暂不可用，HTTP 响应会包含 `dead_letter`，并把原始 payload、错误、source、business_action_id、dataset/record/idempotency key 保存到 `data_event_dead_letters`。管理员或 agent 可通过 `list_event_dead_letters` 发现失败同步，修复字段/结构/源数据后调用 `retry_event_dead_letter` 复用原 payload 重新投递；如果该事件已在外部或人工流程中处理，可调用 `resolve_event_dead_letter` 关闭。MIS Inbox 会把 open dead letters 作为高优先级运营项展示，避免 CRM/ERP/HR/财务 connector 的失败写入无声丢失。

MaClaw 可以通过内置 `mis_data` 工具的 `ingest_event` action 提交事件；`ingest_event` 支持直接传 `business_action_id` 和 `dry_run=true`。生产部署中更推荐业务系统或 connector 直接调用 DataSrv REST API，MaClaw 负责解释、校验、修复和辅助运营。

### 8.9 Report / Aggregate API

企业场景需要内置常用统计报表，同时允许 agent 在受控范围内自由组合临时报表。DataSrv 提供报表目录和有限聚合 DSL，不暴露 SQL。

```text
GET  /api/v1/data/reports
GET  /api/v1/data/reports/{reportId}
POST /api/v1/data/reports/{reportId}/run
POST /api/v1/data/datasets/{datasetId}/aggregate
```

第一阶段支持 `count/sum/avg/min/max/group_by/filter/limit`。常用内置报表包括组织部门状态汇总、销售联系人客户/状态汇总、销售商机阶段/负责人管道汇总、销售订单阶段汇总、客户收入汇总、费用部门汇总、预算部门/状态汇总、发票状态汇总、收付款状态/现金流类型汇总、科目类型汇总、凭证状态/期间汇总、员工部门人数、薪资状态/部门汇总、请假状态/部门天数汇总、合同状态金额、供应商状态/分类、采购订单状态/供应商金额、仓库状态、库存数量/低库存、库存流水类型/仓库汇总等。agent 可以根据字段目录组合聚合请求，但不能执行任意 SQL。

### 8.9.1 Data Quality API

企业数据服务需要支持持续自检，而不是只在写入时校验单条记录。DataSrv 提供质量检查目录和 dataset 级扫描接口：

```text
GET  /api/v1/data/quality-checks
POST /api/v1/data/datasets/{datasetId}/quality/run
GET  /api/v1/data/datasets/{datasetId}/quality/runs
GET  /api/v1/data/datasets/{datasetId}/quality/runs/{runId}
```

第一阶段内置检查：

```text
schema_validation
unknown_fields
unique_duplicates
```

`schema_validation` 检查必填、类型、日期和枚举；`unknown_fields` 用 warning 暴露灵活录入后尚未治理的字段；`unique_duplicates` 检查业务唯一键重复；`relationship_refs` 检查 `record_ref` 字段引用的目标 record 是否存在。每次扫描保存为 quality run，记录检查项、扫描数量、问题数量、问题明细、执行人和时间，便于导入前后对比和审计追踪。agent 在批量导入、数据清洗、schema proposal 应用前后应主动运行 `run_quality_check`，必要时先 `create_backup` 再执行修复。

### 8.10 Schema Proposal API

agent 可以根据业务数据差异发现结构缺口，但不应直接修改结构。结构自改进必须走建议、审查、确认、应用流程。

```text
POST /api/v1/data/datasets/{datasetId}/schema-proposals
POST /api/v1/data/datasets/{datasetId}/schema-proposals/apply
```

`schema-proposals` 根据样本数据生成新增字段建议、类型推断、敏感字段标记和索引建议，并保存为 `pending` 待审记录；`apply` 必须显式 `confirm: true`，可以按 `proposal_id` 应用并将状态更新为 `applied`。这允许 MaClaw 利用智能维护数据结构，同时保留企业治理边界。

### 8.11 备份 / 恢复 API

数据服务需要提供受控备份与恢复接口，方便 MaClaw 内置工具或 agent 在批量导入、数据清洗、自动修复前主动创建恢复点。

```text
GET  /api/v1/data/backups
POST /api/v1/data/backups
GET  /api/v1/data/backups/{backupId}
GET  /api/v1/data/backups/{backupId}/download
POST /api/v1/data/backups/{backupId}/restore
```

创建备份示例：

```json
{
  "name": "before_import_2026_05",
  "note": "批量导入销售订单前的自动备份"
}
```

恢复备份必须显式确认，避免 agent 误恢复：

```json
{
  "confirm": true,
  "reason": "导入校验失败，回滚到导入前状态"
}
```

SQLite 阶段由 MaClawDataSrv 使用在线备份能力生成数据库快照和备份元数据；元数据包含 `sha256` 和 `download_url`，便于 agent 或运维系统做外部归档、校验和离线保管。备份下载和恢复属于高权限操作，需要 `data_admin`。恢复时由服务独占维护锁，阻止并发写入和查询交错。PostgreSQL 阶段保持同一 API，底层切换为数据库原生备份、归档或时间点恢复机制。

## 9. 权限与安全

### 9.1 权限模型

权限建议分层：

- tenant admin
- data admin
- domain owner
- dataset reader
- dataset writer
- dataset auditor

字段级策略：

- `sensitive=true` 的字段默认不进入全文索引。
- agent 查询敏感字段需要显式权限。
- 响应支持 mask，例如薪资、身份证号、银行账号。

MVP 阶段通过 `X-MaClaw-Role` 提供基础角色边界：

- `data_user`：默认角色，可读、可查询、可导出、可执行普通业务动作，敏感字段脱敏。
- `data_auditor`：可查看审计和敏感字段明文，用于审计验证。
- `data_admin`：管理角色，可创建/修改/删除 dataset、从模板创建结构、维护字段、应用 schema proposal、恢复备份。

在 scoped API key 模式下，role 只是 key 绑定的一部分，客户端请求头不能提权。每个 key 还可以限制：

- `allowed_domains`：授予某个业务域的完整业务能力，例如 `sales`；适合域负责人或可信业务 agent。
- `allowed_datasets`：显式授予原始 dataset 访问，例如只允许 `hr.payroll`，比 domain 更细。
- `allowed_actions`：例如只允许 `sales.order_upsert`，不能执行其他业务动作。
- `allowed_views`、`allowed_reports`、`allowed_dashboards`：分别授予业务视图、报表和看板运行能力。
- `allow_raw_data`：是否允许按 `allowed_domains` 访问原始 dataset API；默认应关闭。未开启时，agent 即使能执行业务动作或报表，也不能随意读取/修改底层表。
- `allow_sensitive`：是否允许敏感字段明文；否则即使 key 绑定 `data_auditor`，敏感字段仍脱敏。
- `allow_admin`：是否允许高危管理操作；没有该标记时，即使 key 绑定 `data_admin`，也不能 bootstrap、改 schema、恢复备份或执行维护。

高风险结构操作不应由普通 agent 默认执行。MaClaw 设置页提供 MIS data role/API key 配置；普通业务运行建议使用最小权限 key，结构治理或恢复操作才切换为带 `allow_admin=true` 的 key，并要求用户明确确认。

DataSrv Web Console 需要提供 `Access` 授权页。授权页不以底层 dataset/table 为主要入口，而以业务能力为入口：业务域、business action、business view、report、dashboard。管理员可以从 `GET /api/v1/data/access/presets` 返回的常见授权预设开始，例如销售处理 agent、财务报表 agent、人事审计 agent、库存处理 agent，再按公司实际差异微调勾选项；也可以直接勾选“销售订单处理”“预算控制”“薪资审核”“请假审批”“库存流水登记”等业务能力，生成 scoped API key policy。授权页支持创建、更新、预览、轮换或禁用托管 API key，配置到期时间，并按 active/即将过期/已过期/已禁用过滤授权列表，查看最近使用时间、来源 IP 和 User-Agent。写入权限主要来自 `allowed_actions`；报表、视图和 dashboard 分别来自 `allowed_reports`、`allowed_views`、`allowed_dashboards`，也可通过 `allowed_domains` 给可信域 agent 授权整个业务域。这样 agent 被授权的是“能做什么业务”，而不是“能随便操作哪张表”。报告处理不应被阻断：只要 key 授权对应 view/report/dashboard，agent 可以继续运行报表、dashboard 和聚合分析；但除非显式配置 `allowed_datasets` 或开启 `allow_raw_data`，否则不能读取或修改原始 dataset API。

### 9.2 审计

必须记录：

- dataset 创建/删除/修改
- schema 修改
- record 创建/修改/删除
- 批量导入/导出
- 聚合查询
- 敏感字段访问

审计事件至少包含：actor、tenant、dataset、record、action、request_id、source_ip、created_at。MVP 阶段已提供基础审计查询：`GET /api/v1/data/audit?dataset_id=&action=&user_id=&target_type=&target_id=&q=&limit=`，记录 dataset、field/schema、record、backup、schema proposal 等关键写操作。`GET /api/v1/data/audit/export.csv` 使用同一套过滤条件导出 CSV，便于财务、法务或管理员留档。MaClaw agent 通过 `mis_data.list_audit_logs` 查询，必要时用 `mis_data.export_audit_logs_csv` 导出，不直接读取数据库文件。

### 9.3 Agent 安全边界

agent 不应直接执行 SQL。agent 调用工具时只能使用受控 DSL，由服务端做：

- 权限检查
- 字段白名单
- limit 强制
- 查询复杂度限制
- 审计记录
- 敏感字段脱敏：字段定义中 `sensitive: true` 的数据在普通 `data_user` 读取记录时返回 `***MASKED***`，`data_admin` / `data_auditor` 可通过 `X-MaClaw-Role` 查看原值。

## 10. 可靠性设计

### 10.1 写入可靠性

- 所有写入使用事务。
- 批量导入按 batch commit。
- 支持 idempotency key，避免重复导入和重复事件投递。
- 支持 dry-run 校验。
- 支持导入失败报告。

### 10.2 备份恢复

SQLite 阶段：

- 提供在线 backup API。
- 定期 checkpoint WAL。
- 导出 dataset 快照。
- 恢复前做 schema/version 检查。

PostgreSQL 阶段：

- 使用数据库原生备份机制。
- 支持时间点恢复。

### 10.3 数据校验

字段定义存在时，写入要校验类型、必填、枚举、范围、唯一约束。

字段定义不存在时，仍保存原始 JSON，同时将字段观测写入 schema inference 表。字段已定义时，MVP 阶段会在写入前执行基础数据质量校验，并提供 `POST /api/v1/data/datasets/{datasetId}/records/validate` dry-run 校验接口；批量导入使用 `POST /api/v1/data/datasets/{datasetId}/records/batch`，支持 `dry_run`，会先整批校验，校验失败不写入：required、string/number/boolean/array/object/date/datetime/json 类型、字段 `config.enum` / `config.values` 枚举约束，以及字段 `config.unique=true` 的业务唯一键约束。常用模板已对订单号、客户号、报销单号、发票号、员工号、合同号启用唯一键；复合唯一键如工资单的 `employee_no + payroll_month` 留给后续规则扩展。未知字段仍允许保存，以保留 schema-less 灵活性。

受控数据清洗使用 `POST /api/v1/data/datasets/{datasetId}/records/bulk-update` 和 `POST /api/v1/data/datasets/{datasetId}/records/bulk-delete`。这两个接口只接受受控查询 DSL，不暴露 SQL；批量更新只接受字段级 `set/unset` 补丁，批量删除会先返回待删除记录预览。默认建议先 `dry_run: true`，服务会返回匹配数量、逐条校验结果或记录预览。正式执行必须 `data_admin` 且 `confirm: true`，写入时记录 revision 和 audit。误删单条记录时，`POST /api/v1/data/datasets/{datasetId}/records/{recordId}/restore` 可从删除 revision 恢复，必须 `data_admin` 且 `confirm: true`，恢复也会重新校验当前字段规则、唯一键并写入 restore revision。agent 处理财务、人事、销售数据修正或清理时，应先 `create_backup`，再 dry-run，必要时运行 `run_quality_check`，最后才确认执行。

对于更高风险或需要人类复核的操作，DataSrv 提供 Operation Plan：

```text
GET  /api/v1/data/operation-plans
POST /api/v1/data/operation-plans
GET  /api/v1/data/operation-plans/{planId}
POST /api/v1/data/operation-plans/{planId}/review
POST /api/v1/data/operation-plans/{planId}/apply
POST /api/v1/data/operation-plans/{planId}/cancel
```

Operation Plan 保存 `operation`、`dataset_id`、原始请求、风险等级、影响预览和状态。当前支持为 `bulk_update_records` 与 `bulk_delete_records` 生成计划。普通 agent 或 `data_user` 可以创建 `pending` 计划；`data_admin` 必须先调用 `review` 将计划 `approve` 或 `reject`；`apply` 只执行 `approved` 计划，并写入审计日志。计划必须包含 `query.q`、`query.tag` 或 `query.filter` 作为业务范围，避免误建全表批量变更；确实需要全表级治理时必须显式传入 `allow_full_scan: true`，并配合备份、dry-run 和人工复核。这样 agent 可以先生成可解释、可审查、可追踪的操作预案，而不是直接修改公司数据。

## 11. 与 MaClaw 的集成

### 11.1 设置页

MaClaw 设置中新增 tab：`MIS数据`。

页面能力：

- 配置服务地址。
- 配置访问 token。
- 测试连接。
- 查看服务版本、健康状态、后端引擎。
- 管理默认 tenant/workspace。
- 开关 agent 内置数据工具。
- 后续可进入 dataset 管理、导入、字段管理。

### 11.2 内置工具

MaClaw agent 第一阶段提供单一受控工具 `mis_data`，通过 `action` 参数映射到 MaClawDataSrv REST API：

```text
status
get_capabilities
get_inbox
get_inbox_summary
get_stats
run_maintenance
list_dashboards
get_dashboard
run_dashboard
list_templates
get_template
bootstrap_templates
create_dataset_from_template
list_datasets
get_dataset
create_dataset
delete_dataset
list_fields
upsert_fields
upsert_record
get_record
delete_record
query_records
export_records
export_records_jsonl
start_csv_export_job
start_jsonl_export_job
ingest_event
list_reports
get_report
run_report
list_business_views
get_business_view
query_business_view
aggregate_records
list_quality_checks
run_quality_check
list_quality_runs
get_quality_run
list_import_jobs
get_import_job
list_export_jobs
get_export_job
download_export_job
list_audit_logs
export_audit_logs_csv
list_data_events
list_record_revisions
get_record_timeline
list_record_approvals
create_record_approval
get_record_approval
review_record_approval
validate_record
batch_import_records
bulk_update_records
bulk_delete_records
restore_record
list_operation_plans
create_operation_plan
get_operation_plan
review_operation_plan
apply_operation_plan
cancel_operation_plan
start_batch_import_job
get_import_template_csv
import_records_csv
start_csv_import_job
import_records_jsonl
start_jsonl_import_job
propose_schema
apply_schema_proposal
create_backup
list_backups
get_backup
download_backup
restore_backup
```

后续可继续扩展更细粒度审批流等 action。MVP 已支持 `get_capabilities` 让 agent 发现可用业务能力，并支持 `run_dashboard` 读取公司/业务域 MIS 概览，`execute_business_action` 通过 `dry_run=true` 做业务写入预检，`export_records` 按受控查询 DSL 导出 CSV、`export_records_jsonl` 导出 JSONL，普通角色导出时继续遵守字段脱敏；`get_record_timeline` 用于解释单个业务对象的 revision、approval、event、audit 组合历史；`create_record_approval` / `review_record_approval` 用于报销、采购、合同、资产等业务审批；`export_audit_logs_csv` 用于按同一套审计过滤条件导出合规留档。`bulk_update_records` 和 `bulk_delete_records` 用于受控数据清洗，必须先 dry-run，正式执行需要 data_admin 和 confirm；`restore_record` 用于从删除 revision 受控恢复单条误删记录；`create_operation_plan` / `review_operation_plan` / `apply_operation_plan` 用于把高风险批量操作保存成可审查预案、管理员审批后再执行。`create_backup`、`get_backup`、`download_backup` 让 agent 在高风险操作前后创建、读取并校验备份；`run_maintenance` 用于 data_admin 执行 SQLite 自检和整理。工具不直接暴露数据库连接或 SQL。工具参数映射到 MaClawDataSrv 的受控 REST API。

### 11.3 默认本地服务

MaClaw 可以检测本机 `127.0.0.1:18180`：

- 如果可连接，读取 `/readyz` 和 `/version`。
- 如果不可连接，设置页提示用户启动 MaClawDataSrv。
- 后续可以提供“一键启动本地数据服务”。

### 11.4 数据服务 Web Console

MaClawDataSrv 自身提供轻量 Web Console，默认入口为：

```text
GET /
GET /ui
```

这个界面用于本地验证和员工受控数据操作，能力包括：

- 输入服务地址、token、tenant、user。
- 查看 readiness 状态。
- 通过企业模板创建 dataset，模板覆盖销售、人事、财务、法务、采购、库存、固定资产等常见 MIS 数据对象。
- 查看和执行业务动作，例如维护组织部门、维护客户、维护销售联系人、创建/更新销售商机、更新销售商机阶段、创建/更新销售订单、登记费用、维护预算、维护收付款、维护会计科目/凭证、维护员工/薪资、提交或审批请假/休假申请、更新合同、维护供应商、更新采购订单、维护仓库、登记库存流水、调整库存、登记固定资产。
- 查看和查询业务视图，例如组织部门目录、客户目录、销售联系人目录、销售商机管道、销售订单概览、人事花名册、薪资审核、请假审核、费用审核、预算控制、发票状态、付款跟踪、科目目录、凭证台账、合同台账、供应商目录、采购订单跟踪、仓库目录、库存概览、库存流水台账、固定资产台账。
- 运行并回看数据质量检查，发现 schema 校验、未知字段和唯一键重复问题。
- 创建和选择 dataset。
- 查询 records，支持关键词、标签、limit 和过滤 JSON。
- 查看 Dashboard 和 MIS Inbox，先看公司/业务域概览，再集中处理待审批事项、待复核操作计划、失败导入导出任务和数据质量问题。
- 在 Editor 中查看单条 record revisions、业务审批和合并 timeline，便于员工或 agent 排查外部同步、业务动作和审计历史。
- 按当前查询条件同步导出 CSV/JSONL，或启动异步 export job 后下载结果，方便员工核对数据，也方便 agent/外部系统交接结构化数据。
- 写入或更新单条 record。
- 载入当前 dataset 的 CSV 导入模板，粘贴 CSV 或 JSONL 文本进行 dry-run 校验、同步导入或启动异步导入 job。
- 读取和维护字段定义。
- 创建、查看和恢复备份。
- 查看运维统计，执行 integrity check、optimize、vacuum 等受控维护任务。

Web Console 不绕过权限，不直接访问数据库文件，不暴露 SQL；所有操作仍通过同一套 Bearer token 和 REST API 完成。它应作为正规产品化 MIS 管理后台建设，而不是临时调试页：顶部展示服务连接和身份上下文，左侧管理数据资源，右侧按业务运营、集成分析、数据治理、系统管理分组组织功能，并提供中英文双语本地化切换，方便中文员工、英文实施人员和 agent 共同使用。默认 Overview 首页展示服务摘要、当前数据集、常用业务入口和治理入口；首页还提供设置检查清单，提示服务连接、模板目录、数据集、授权 Key 和恢复路径是否准备好，降低首次部署和员工接手时的认知成本。Overview 的运营健康概览从受控 `stats` API 读取记录数、字段数、质量扫描、审计日志、备份数和数据库大小；恢复路径检查也应基于真实 `backup_count`，而不是静态提示。Overview 的 MIS Coverage 从模板目录和当前 dataset 元数据计算业务域总数、已初始化业务域、缺失业务域、模板数和数据集数，并提供 bootstrap 预览入口，帮助管理员判断销售、财务、人事、法务、采购、库存、资产等常见 MIS 域是否已经具备基础结构。Overview 的 Work Queue 从 `inbox/summary` 读取 total、critical、high、overdue 等业务待办指标，让员工和 agent 先处理审批、失败任务、质量问题和运营事项。Overview 的 Integration Health 从 `connectors/health` 读取连接器总数、降级数、停用数、近期失败数、未处理 dead-letter 和近期事件量，让实施人员能在首页判断 CRM、ERP、HRIS、财务、仓储等外部系统接入是否健康，再进入 Connectors 页做配置、映射、预演或重试。Overview 还应提供授权风险摘要：管理员登录时读取 `access/review`，显示 total keys、findings、critical/high/medium 风险计数；普通员工或低权限 agent 无权读取时只显示需要管理员权限，不影响记录查询、报表和其它低风险操作。Overview 的 Governance Readiness 从 `stats`、`access/review` 和 `inbox/summary` 组合恢复路径、范围化 Key、审计轨迹、质量扫描和未处理事项五个状态，用于判断是否具备承载真实公司业务的最低治理条件；同时根据缺口生成最多三条下一步建议，例如创建备份、创建范围化 API key、运行质量检查或处理高优先级待办。Overview 的 Recent Activity 从 `audit?limit=5` 读取最近审计日志，展示时间、动作和摘要，帮助管理员快速理解系统刚刚发生过什么。工作区顶部保留最近状态历史，让员工知道刚刚执行的连接、查询、导入、备份、授权审计等操作是否成功；后台也会记住最近打开的模块，方便日常反复使用。MVP 已提供 Dashboard、MIS Inbox、记录查询、单条记录 revision/timeline 追溯、业务审批、业务视图、CSV/JSONL 导入导出、业务动作、常用报表、数据质量检查与历史回看、字段维护、受控 schema proposal、dry-run 校验、批量导入、受控批量更新/删除、Operation Plan、异步导出 job、备份恢复、维护任务、审计查询和角色脱敏验证。多级审批、更细粒度权限和异步大文件导入可作为后续企业版功能扩展。

Access 页应是 agent 授权工作台，而不是单纯的 key 列表。页面顶部需要汇总当前业务域、业务动作、报表、托管 Key、授权风险和 raw/admin 例外数量，并提供三步式引导：先从业务角色预设开始，再勾选业务动作、业务视图、报表和 dashboard，最后预览有效能力、执行访问检查、轮换或禁用异常 key。管理员也可以输入 agent purpose，例如“财务报表 agent”“销售订单处理 agent”“HR 请假审批 agent”，由 Web Console 基于现有授权预设、业务域和能力 ID 做本地推荐，输出候选 preset、匹配词、分数、授权范围和建议 setup；setup 包含建议 key id、user/agent id 和默认 90 天过期时间。推荐只作为草稿，必须由管理员点击应用后才会改表单，不自动创建或更新 key。这个设计让管理员在日常操作中自然遵守“业务能力优先、原始数据例外、敏感/管理权限显式确认”的授权原则。托管 Key 创建或轮换后，Web Console 应立即生成 agent handoff：包含 DataSrv endpoint、tenant、key id、一次性 secret、user/role、授权范围、推荐 agent 操作原则、REST header 示例和快速验证 endpoint。加载已有 key 或预览权限时仍可生成 handoff，但不应显示 secret，避免后台页面成为密钥泄露源。授权页还应提供 agent readiness check，自动抽取当前 key 的业务动作、业务视图、报表、dashboard、dataset/admin/sensitive 例外并调用 `access/check` 验证，生成允许/拒绝和原因表，帮助管理员在交付 key 前发现授权范围不完整或过宽的问题；授权审查结果可导出为 JSON 留档，记录导出时间、筛选条件和完整 findings，方便周期性审计和上线前复核。Access 页还应能通过 `GET /api/v1/data/governance/evidence-pack` 导出 governance evidence pack，把服务 stats、access review、remediation plan、近期 audit、work queue 和 connector health 合并成一份 JSON 证据包，并在顶层 summary 中给出 status、risk_level、recommendations、controls、section 成功数、授权风险、整改动作、高危待办、逾期待办、连接器异常和审计条数；顶层还应返回服务端生成的 `summary_text`，供 MaClaw/agent、审计交接和 Web Console 复制摘要直接使用，避免不同客户端各自拼接造成口径不一致。证据包还应带 `evidence_id` 和 `evidence_sha256`，其中 `evidence_id` 从 SHA256 摘要派生，`evidence_sha256` 对排除摘要文本和指纹字段后的证据包内容计算，用于审计归档、上线记录和 agent 交付时引用同一份证据。`summary_text` 默认英文，调用方可传 `lang=zh` 获取中文摘要；Web Console 应根据当前语言切换自动传递该参数，MaClaw 工具也允许 agent 显式请求中文或英文。需要只交付文字摘要时，可调用 `GET /api/v1/data/governance/evidence-summary.txt?lang=zh`，它返回同一份服务端摘要的 `text/plain` 版本，适合 agent 汇报、审计备注、上线记录和后台“下载摘要”按钮。controls 应至少覆盖 recovery backup、scoped access、audit trail、work queue 和 connector health，并以 pass/warn/fail 表达审计控制状态，同时给出 recommended_action 和 action_target，Web Console 可把这些 action_target 渲染为跳转到 Backups、Access、Audit、Inbox 或 Connectors 的整改入口，用于管理层汇报、外部审计、上线验收或 agent 权限交付。Web Console 的导出按钮也应调用这个正式 API，而不是只在浏览器里拼接数据。管理员载入已有托管 Key 并修改表单后，Web Console 应能对比当前数据库中的 policy 和待更新 policy，列出新增/移除的业务动作、视图、报表、dashboard、domain、raw dataset，以及 role、过期时间、raw/sensitive/admin/enable 等标量变化；同时做本地风险预览，标出 no expiration、allow admin、allow sensitive、allow raw data、whole domain scope、raw dataset scope、empty scope 等常见风险。授权页还应生成 agent onboarding checklist，用 done/no 形式检查 purpose 是否已记录、是否已有 scope、是否设置过期时间、高风险例外是否已审查、是否完成 diff/risk、是否已创建或载入托管 key、是否已运行 readiness、是否已生成 handoff。最后可生成 agent onboarding packet，把当前 policy、operation summary、recommended next steps、risk preview、readiness 结果、checklist 和 handoff 合并成 JSON 文本，并支持复制或下载为交付包，便于交给 MaClaw、实施人员或审计留档。这样授权更新和 agent 交付前先看差异、风险、清单和交付包，再执行 `PATCH` 或交接密钥，降低误删 agent 权限或意外扩大权限的风险。

Governance evidence API 的 JSON 和 text/plain 响应还会设置 `X-MaClaw-Evidence-ID` / `X-MaClaw-Evidence-SHA256`，便于 agent、网关或归档脚本不解析 body 也能记录同一份证据的引用和指纹。

`GET /api/v1/data/capabilities` 还应在顶层返回 `access` 摘要，明确当前调用是 root/header 角色还是 scoped API key、scope_mode、是否业务优先、raw dataset/sensitive/admin 是否允许、可见 domain/dataset/action/view/report/dashboard 数量、授权清单、guardrails 和 recommended_next_actions。MaClaw 或外部 agent 应把这个摘要作为每次 MIS 会话的第一层运行约束：默认先 `resolve_intent`、`list_domains`、`run_dashboard`、`execute_business_action`、`run_report`，只有 `access.raw_dataset_allowed` 或 `access.admin_allowed` 明确允许时才考虑 dataset、schema、maintenance、backup restore 等管理操作。

Overview 的 Business Domain Readiness 从 `domains` API 读取各业务域的 `initialized`、`missing_templates`、`use_cases`、`business_actions`、`business_views`、`reports` 和 `dashboards`，形成可点击的业务域卡片；员工或 agent 可以从首页直接进入某个业务域，继续处理意图解析、业务动作、视图、报表或初始化补齐。

Overview 的 Business Capabilities 从当前已加载的 business actions、business views、reports 和 dashboards 目录生成能力摘要。这个摘要强调“业务方式优先”：agent 和员工应优先从业务动作、业务视图、报表和仪表盘入口处理公司 MIS 工作，只有在治理或调试场景中才进入原始 dataset/field 层。

Overview 的 Intent Launcher 是轻量业务入口，调用 `intent/resolve` 把自然语言业务请求解析为 use case、首选业务动作、视图、报表、仪表盘和 next steps。员工可以输入“报销”“低库存”“销售订单状态”等请求，从首页直接载入业务动作 dry-run 模板，运行推荐 dashboard、查询推荐 business view、运行推荐 report，或预览缺失模板初始化；这进一步强化 DataSrv 作为 agent 友好的业务操作层，而不是传统表格 CRUD 页面。

## 12. 与现有 records 原型的关系

当前已实现的 `/api/v1/records/{collection}` 可以作为临时兼容层，但不应作为最终公司级 API。

建议演进：

1. `/api/v1/records` 只作为早期轻量原型，不作为企业数据正式入口。
2. 企业数据正式入口属于 MaClawDataSrv：`/api/v1/data/...`。
3. MaClaw 内置工具只对接 MaClawDataSrv。
4. MaClawSrv 不再继续扩展企业数据存储逻辑。

## 13. 分阶段推进计划

### Phase 0：纠偏与设计冻结

- 明确企业数据不进入 `state/store.json`。
- 明确 MaClawDataSrv 是独立服务，不内置在 MaClawSrv。
- 明确默认 REST 地址、token 认证和 SQLite/PostgreSQL 引擎边界。
- 完成本设计文档 review。
- 明确第一批业务域：销售、人力、财务。
- 明确敏感字段策略和默认权限。

验收标准：设计文档确认，接口命名和数据模型达成一致。

### Phase 1：结构化数据底座 MVP

- 新增 `MaClawDataSrv` 二进制入口。
- 新增 `corelib/structureddata` 包。
- 新增 `StructuredDataStore` 接口和 SQLite engine。
- 实现 SQLite 数据库和 schema 初始化。
- 实现 dataset、field、record 基础 CRUD。
- 实现字段索引表、tag 索引、FTS。
- 实现受控 query DSL 的基础 filter/sort/limit。
- 实现基础审计事件。
- 实现 token 认证和本地默认配置。

验收标准：能创建 `finance.expenses`、`hr.employees`、`sales.orders`，能写入、查询、分页、按字段过滤和全文搜索。

### Phase 2：导入、校验与权限

- CSV/Excel/JSONL 导入 job。
- dry-run 校验和错误报告。
- schema inference。
- 字段类型校验、必填校验、枚举校验。
- dataset 级权限和字段脱敏。

验收标准：能导入一份销售订单表、一份员工表、一份费用表，错误行可定位，敏感字段默认脱敏。

### Phase 3：聚合与报表

- aggregate API。
- count/sum/avg/min/max/group_by。
- 时间区间统计。
- 常用报表模板。
- 导出 CSV/Excel。

验收标准：能回答“本季度各部门费用合计”“本月新增客户数”“员工按部门人数分布”等问题，并能追溯数据来源。

### Phase 4：企业级可靠性增强

- revisions 表。
- idempotency key。
- online backup/restore。
- 数据快照。
- 查询复杂度控制。
- 慢查询日志。
- 数据质量报告。

验收标准：支持批量导入回滚、恢复测试、审计追踪和慢查询定位。

### Phase 5：PostgreSQL Engine

- 抽象 SQLite/PostgreSQL store。
- PostgreSQL JSONB/GIN 实现。
- 行级权限或应用层权限下推。
- 大团队部署指南。

验收标准：同一 API 可切换 SQLite 或 PostgreSQL 后端，通过一致性测试。

## 14. 开发顺序建议

优先做：

1. 独立 `cmd/maclaw-data-srv`。
2. `corelib/structureddata` 数据模型和 Store 接口。
3. SQLite schema + migrations。
4. token 认证和 REST 基础框架。
5. dataset/field/record CRUD。
6. query DSL。
7. MaClaw 设置页“MIS数据”。
8. MaClaw 内置数据工具。
9. import job。
10. 权限与脱敏。
11. aggregate/report。

不要优先做：

- 漂亮 UI。
- 任意 SQL 接口。
- 复杂 AI 自动建模。
- 未受控的数据联邦查询。

## 15. 关键决策

1. 业务数据与控制面状态分离。
2. 企业数据服务采用独立 MaClawDataSrv，通过 REST API 访问。
3. 默认本地地址是 `http://127.0.0.1:18180`，但仍必须使用 token。
4. 后端引擎抽象为 SQLite/PostgreSQL。
5. 灵活 JSON 与字段索引并存。
6. schema-less 起步，但必须支持 schema registry。
7. agent 只能走受控 DSL，不能直连 SQL。
8. SQLite 是本地部署 MVP，不是最终企业多并发上限。
9. PostgreSQL engine 作为后续扩展，不影响第一阶段 API 设计。

## 16. 待确认问题

1. 第一批业务域是否固定为销售、人力、财务？
2. 是否需要“部门/岗位/角色”作为权限主体？
3. 财务数据第一阶段已覆盖费用、预算、发票、收付款、轻量科目和轻量凭证；凭证业务动作已做借贷表头合计与分录合计平衡校验，完整总账、借贷方向规则和会计准则校验留到会计引擎阶段。
4. HR 第一阶段包含薪资和请假/休假申请，并启用敏感字段脱敏、dry-run、备份、质量检查和审批规则。
5. 销售数据是否需要客户、联系人、商机、订单、合同四类标准 dataset？
6. 本地 MaClawDataSrv 是否由 MaClaw 自动启动，还是用户手动启动？
7. token 初始生成方式：命令行输出、配置文件、还是 MaClaw 首次配对？
8. 服务端 PostgreSQL 模式是否需要第一版就保留配置项？





