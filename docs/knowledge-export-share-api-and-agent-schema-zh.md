# 知识库导出与分享 API / Agent Schema 设计

## 目标

本文档定义：

- GUI / Wails 调用接口
- Hub HTTP API
- 分享页 JSON 协议
- agent 工具 schema
- 导入导出关键请求与响应结构

## 设计原则

- 面向 UI 的接口与面向 agent 的接口尽量复用同一数据模型
- 分享页是统一入口，但真正下载走受控协议
- 结构化返回优先稳定和可扩展，不追求最短字段

## 类型定义

## 导出结果

```ts
type KnowledgeExportResult = {
  output_path: string
  format: "jsonl" | "mckb-zip"
  redact_sensitive: boolean
  scoped?: boolean
  source_ids?: string[]
  url_policies?: number
  sources: number
  source_labels?: number
  source_versions?: number
  source_links?: number
  source_link_events?: number
  nodes: number
  cards: number
  facts: number
  bytes?: number
  generated_at: string
}
```

## 导出包创建请求

```ts
type KnowledgeCreateExportPackageRequest = {
  output_path?: string
  source_ids?: string[]
  export_scope?: "all" | "filtered" | "selected_sources"
  redact_sensitive?: boolean
  include_assets?: boolean
  recovery_mode?: "full" | "mixed" | "reference_only"
}
```

## 导出包创建响应

```ts
type KnowledgeCreateExportPackageResponse = {
  package_path: string
  package_format: "mckb-zip"
  snapshot_version: string
  recovery_mode: "full" | "mixed" | "reference_only"
  export_result: KnowledgeExportResult
  embedded_asset_count: number
  reference_only_source_count: number
  missing_asset_count: number
}
```

## Hub 发布请求

```ts
type KnowledgePublishExportToHubRequest = {
  package_path: string
  title: string
  description: string
  labels?: string[]
  visibility_scope: "global" | "hub" | "tenant" | "selected_users"
  allowed_users?: string[]
  allow_re_export?: boolean
  redacted?: boolean
}
```

## Hub 条目

```ts
type KnowledgeShareEntry = {
  knowledge_id: string
  export_id: string
  owner_user_id: string
  owner_tenant_id: string
  origin_hub_id: string
  origin_hub_url: string
  title: string
  description: string
  labels?: string[]
  visibility_scope: "global" | "hub" | "tenant" | "selected_users"
  allowed_users?: string[]
  allow_re_export?: boolean
  redacted?: boolean
  snapshot_format: "mckb-zip"
  snapshot_version: string
  current_snapshot_version: string
  snapshot_hash: string
  size_bytes: number
  source_count: number
  card_count: number
  fact_count: number
  view_count: number
  import_count: number
  status: "active" | "deleted" | "draft" | "locked"
  share_url: string
  created_at: string
  updated_at: string
  published_at: string
}
```

## 分享目标解析结果

```ts
type KnowledgeResolveShareTargetResponse = {
  knowledge_id: string
  title?: string
  description?: string
  origin_hub: string
  hub_display_name?: string
  visibility_scope?: "global" | "hub" | "tenant" | "selected_users"
  can_view: boolean
  can_import: boolean
  status: "active" | "deleted" | "locked" | "not_found"
  snapshot_version?: string
  preview?: {
    source_count: number
    card_count: number
    fact_count: number
  }
}
```

## 下载令牌响应

```ts
type KnowledgeDownloadTokenResponse = {
  knowledge_id: string
  snapshot_version: string
  download_url: string
  token_type: "signed_url" | "hub_proxy"
  expires_at: string
}
```

## 本地导入请求

```ts
type KnowledgeImportPackageRequest = {
  input_path: string
  dry_run?: boolean
  overwrite?: boolean
  replace_all?: boolean
  abort_on_error?: boolean
  create_safety_backup?: boolean
}
```

## 从 Hub 导入请求

```ts
type KnowledgeImportFromHubRequest = {
  knowledge_id?: string
  share_url?: string
  import_mode: "merge" | "insert_only" | "overwrite_conflicts" | "replace_all"
  dry_run?: boolean
  abort_on_error?: boolean
  create_safety_backup?: boolean
}
```

## 本地导入追踪记录

```ts
type KnowledgeImportedShareRef = {
  knowledge_id: string
  origin_hub_url: string
  share_url: string
  snapshot_version: string
  import_batch_id: string
  imported_at: string
  import_mode: "merge" | "insert_only" | "overwrite_conflicts" | "replace_all"
  replace_all: boolean
}
```

## Wails / GUI 接口

建议新增：

```ts
KnowledgeCreateExportPackage(req: KnowledgeCreateExportPackageRequest): Promise<KnowledgeCreateExportPackageResponse>
KnowledgePublishExportToHub(req: KnowledgePublishExportToHubRequest): Promise<KnowledgeShareEntry>
KnowledgeResolveShareTarget(input: string): Promise<KnowledgeResolveShareTargetResponse>
KnowledgeDownloadSharedExport(input: { knowledge_id?: string; share_url?: string }): Promise<KnowledgeDownloadTokenResponse>
KnowledgeImportFromHub(req: KnowledgeImportFromHubRequest): Promise<any>
KnowledgeListMySharedExports(req?: { limit?: number; cursor?: string }): Promise<{ items: KnowledgeShareEntry[] }>
KnowledgeUpdateSharedExport(req: { knowledge_id: string; title?: string; description?: string; labels?: string[]; visibility_scope?: string; allowed_users?: string[]; allow_re_export?: boolean }): Promise<KnowledgeShareEntry>
KnowledgeDeleteSharedExport(req: { knowledge_id: string }): Promise<{ ok: true }>
```

## Hub HTTP API

## 发布知识条目

```http
POST /api/knowledge/exports/publish
Content-Type: application/json
```

请求：

```json
{
  "package_path": "/path/to/export.mckb.zip",
  "title": "客服知识库 2026-06 交接包",
  "description": "迁移客服 FAQ 和流程说明",
  "labels": ["客服", "交接"],
  "visibility_scope": "hub",
  "allow_re_export": false
}
```

响应：

- `200 OK`
- 返回 `KnowledgeShareEntry`

## 我的知识导出列表

```http
GET /api/knowledge/exports/my?limit=20&cursor=...
```

响应：

```json
{
  "items": [],
  "next_cursor": ""
}
```

## 条目详情

```http
GET /api/knowledge/exports/{knowledge_id}
```

## 更新条目

```http
PATCH /api/knowledge/exports/{knowledge_id}
```

允许修改：

- `title`
- `description`
- `labels`
- `visibility_scope`
- `allowed_users`
- `allow_re_export`

不允许修改：

- `knowledge_id`
- `snapshot_version`
- `snapshot_hash`

## 删除条目

```http
DELETE /api/knowledge/exports/{knowledge_id}
```

响应：

```json
{
  "ok": true
}
```

## 知识 ID 解析

```http
GET /api/knowledge/resolve/{knowledge_id}
```

响应：

- 成功：`KnowledgeResolveShareTargetResponse`
- 失败：统一错误体

## 下载令牌

```http
POST /api/knowledge/exports/{knowledge_id}/download-token
```

响应：

- 成功：`KnowledgeDownloadTokenResponse`
- 失败：统一错误体

## 真正下载

```http
GET /api/knowledge/exports/{knowledge_id}/download?token=...
```

返回：

- `application/zip`

## 分享页协议

人类入口：

```http
GET /k/{knowledge_id}
Accept: text/html
```

机器入口：

```http
GET /k/{knowledge_id}?format=json
Accept: application/json
```

成功 JSON：

```json
{
  "knowledge_id": "kid_hubcn01_9f3a2c7d8e41",
  "title": "客服知识库 2026-06 交接包",
  "description": "用于跨机器迁移客服 FAQ、流程说明和近期补充规则。",
  "origin_hub": "https://hub.example.com",
  "origin_tenant_id": "tenant-a",
  "visibility_scope": "hub",
  "status": "active",
  "can_view": true,
  "can_import": true,
  "snapshot_version": "v1",
  "export_format": "mckb-zip",
  "download_url": "https://hub.example.com/api/knowledge/exports/kid_hubcn01_9f3a2c7d8e41/download",
  "preview": {
    "source_count": 128,
    "card_count": 942,
    "fact_count": 316
  },
  "labels": ["客服", "交接", "FAQ"],
  "updated_at": "2026-06-26T10:00:00Z"
}
```

## 错误协议

统一错误体：

```json
{
  "error": {
    "code": "knowledge_access_denied",
    "message": "You do not have access to this knowledge entry.",
    "knowledge_id": "kid_hubcn01_9f3a2c7d8e41",
    "retryable": false,
    "can_login_and_retry": false
  }
}
```

状态码建议：

- `401`
- `403`
- `404`
- `410`
- `423`
- `502`
- `503`

错误码建议：

- `knowledge_not_found`
- `knowledge_deleted`
- `knowledge_access_denied`
- `knowledge_login_required`
- `knowledge_locked`
- `knowledge_hub_unavailable`
- `knowledge_download_unavailable`

## Agent 工具 Schema

## Hub 左侧 Tab：知识管理 API

Hub 后台左侧导航新增独立一级 tab `知识管理` 时，需要配套管理员 API。该 tab 对 Hub 管理员和租户管理员可见，用于查看权限范围内用户分享知识的描述信息和执行强制删除。

权限范围：

- Hub 管理员可查看和管理全 Hub 所有租户的分享知识
- 租户管理员只能查看和管理自己租户下用户分享的知识
- 普通用户不可访问这些管理员 API

### 列出全部分享知识

```http
GET /api/admin/knowledge/exports?limit=50&cursor=...&user_id=...&tenant_id=...&sort=published_at_desc
```

查询参数：

- `limit`
  - 固定或上限为 `50`
- `cursor`
  - 翻页游标
- `user_id`
  - 按发布用户筛选
- `tenant_id`
  - 按租户筛选
  - Hub 管理员可指定
  - 租户管理员传入其他租户时必须返回 `403`
- `sort`
  - `published_at_desc`
  - `updated_at_desc`
  - `view_count_desc`
  - `import_count_desc`

响应字段只返回知识描述和管理元数据，不返回知识包正文、source 内容、card 内容、fact 内容。服务端必须根据当前登录用户角色自动施加租户范围限制，不能只依赖前端筛选。

```ts
type KnowledgeAdminExportListItem = {
  knowledge_id: string
  title: string
  description: string
  owner_user_id: string
  owner_user_name?: string
  owner_tenant_id: string
  owner_tenant_name?: string
  visibility_scope: "global" | "hub" | "tenant" | "selected_users"
  labels?: string[]
  view_count: number
  import_count: number
  status: "active" | "deleted" | "draft" | "locked"
  created_at: string
  updated_at: string
  published_at: string
}
```

### 强制删除分享知识

```http
DELETE /api/admin/knowledge/exports/{knowledge_id}
Content-Type: application/json
```

请求：

```json
{
  "reason": "违规内容或不再允许分享"
}
```

规则：

- Hub 管理员可强制删除任意租户的分享知识
- 租户管理员只能强制删除自己租户内的分享知识
- 普通用户不可调用
- `reason` 必填
- 删除 Hub 分享条目和可下载知识包
- 不删除发布者本地知识库
- 分享链接后续返回 `410`
- 必须写审计日志

审计字段：

- `admin_user_id`
- `knowledge_id`
- `owner_user_id`
- `owner_tenant_id`
- `reason`
- `deleted_at`

## `knowledge_export_package`

用途：

- 导出本地知识库为可交换知识包

输入：

```json
{
  "output_path": "string",
  "source_ids": ["string"],
  "export_scope": "all | filtered | selected_sources",
  "redact_sensitive": true,
  "include_assets": true,
  "recovery_mode": "full | mixed | reference_only"
}
```

## `knowledge_publish_to_hub`

用途：

- 将最近或指定本地知识包发布到 Hub

输入：

```json
{
  "package_path": "string",
  "title": "string",
  "description": "string",
  "labels": ["string"],
  "visibility_scope": "global | hub | tenant | selected_users",
  "allowed_users": ["string"],
  "allow_re_export": false,
  "redacted": false
}
```

## `knowledge_resolve_id`

用途：

- 根据知识 ID 定位 Hub 和权限

输入：

```json
{
  "knowledge_id": "string"
}
```

## `knowledge_resolve_share_link`

用途：

- 根据分享链接解析知识条目和导入描述

输入：

```json
{
  "share_url": "string"
}
```

## `knowledge_import_from_hub`

用途：

- 根据知识 ID 或分享链接导入知识

输入：

```json
{
  "knowledge_id": "string",
  "share_url": "string",
  "import_mode": "merge | insert_only | overwrite_conflicts | replace_all",
  "dry_run": false,
  "abort_on_error": false,
  "create_safety_backup": true
}
```

规则：

- `knowledge_id` 与 `share_url` 至少提供一个
- `replace_all` 是高风险动作，agent 执行前应提示

## `knowledge_list_hub_exports`

用途：

- 查看当前用户在 Hub 上已分享的知识条目

输入：

```json
{
  "limit": 20,
  "cursor": "string"
}
```

## `knowledge_update_hub_export`

用途：

- 更新条目说明、标签、可见范围

输入：

```json
{
  "knowledge_id": "string",
  "title": "string",
  "description": "string",
  "labels": ["string"],
  "visibility_scope": "global | hub | tenant | selected_users",
  "allowed_users": ["string"],
  "allow_re_export": false
}
```

## `knowledge_delete_hub_export`

用途：

- 删除或下架知识条目

输入：

```json
{
  "knowledge_id": "string"
}
```

## Agent 行为规范

- 收到分享链接时，优先使用结构化 JSON，不抓取页面正文做导入
- 收到知识 ID 时，先解析再下载
- 下载必须走受控下载令牌
- `replace_all` 必须明确提示风险
- 导入完成后应返回：
  - 导入来源
  - 快照版本
  - 导入条数
  - 是否创建安全备份

## 最小实现集

第一批必须实现：

- `KnowledgeCreateExportPackage`
- `KnowledgeResolveShareTarget`
- `KnowledgeDownloadSharedExport`
- `KnowledgeImportFromHub`
- `POST /api/knowledge/exports/publish`
- `GET /api/knowledge/resolve/{knowledge_id}`
- `POST /api/knowledge/exports/{knowledge_id}/download-token`
- `GET /k/{knowledge_id}?format=json`

这组能力到位后：

- 前端可导出
- 用户可分享
- agent 可按链接导入
