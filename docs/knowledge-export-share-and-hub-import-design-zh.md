# 知识库导出、分享链接、Hub 交换与导入设计

## 背景

当前知识库界面已经支持文本、URL、文件、目录和抓取等导入能力，也已经具备本地知识快照导入导出的底层能力，但这些能力还没有被组织成一条完整的“跨机器交换知识”的产品链路。用户目前无法方便地：

- 将全部或部分知识库导出为可交换、可编辑、可再次导入的文件
- 将某一次导出发布到 Hub，形成可管理、可分享的知识条目
- 通过分享链接或知识 ID 在不同机器、不同用户之间交换知识
- 让 agent 直接操作知识导出、知识导入、Hub 发布、按知识 ID 导入等动作

本设计将知识库导出、Hub 分享、跨 Hub 导入、分享浏览页和 agent 工具统一成一套协议与界面。

## 目标

- 在现有知识库弹窗中新增 `导出` tab，不改变现有 `导入` tab 的基本结构
- 支持导出全部知识库、当前筛选结果、指定来源集合
- 支持导出后发布到 Hub，形成“知识条目”
- 每一次导出都生成唯一 `知识 ID`
- 每一个知识条目都生成统一 `分享链接`
- 分享链接同时满足：
  - 人类用户可阅读、预览、判断是否导入
  - agent 可直接获取结构化导入数据并执行导入
- 在 `导入` tab 中增加整库导入和按知识 ID / 分享链接导入能力
- 支持 Hub 上对“我的知识导出条目”进行浏览、编辑说明、删除分享、复制链接
- 为 agent 提供对称的导入导出与 Hub 操作工具

## 非目标

- 本期不开放用户手工编辑底层 `card / fact / node` 的内部结构
- 本期不将知识分享对象细化到单条 `card` 或单条 `fact`
- 本期不在不同 Hub 之间做自动双向同步，只支持“发布”和“导入”
- 本期不做去中心化 Hub 发现网络，先采用中心可解析或 Hub 可解析的知识 ID 路由方案

## 核心概念

### 知识条目

知识条目是“某一次导出”形成的分享对象，不是单条 source，也不是某个长期存在的知识库别名。

一个知识条目对应：

- 一次本地导出结果
- 一个可导入的快照包
- 一个唯一知识 ID
- 一个可阅读且可机器解析的分享链接
- 一组可见范围与权限设置

### 知识 ID

知识 ID 是对外交换的稳定标识，用于：

- 用户告诉 agent “导入某条知识”
- Hub 内搜索、定位和导入某个知识条目
- 分享页、下载接口、导入接口的统一索引键

知识 ID 必须支持：

- 全局唯一
- 可反查所属 Hub
- 不依赖用户手工补充 Hub 地址

建议格式：

```text
kid_<hub_locator>_<random>
```

示例：

```text
kid_hubcn01_9f3a2c7d8e41
```

说明：

- `kid`：知识条目标识前缀
- `hub_locator`：Hub 可路由标识，便于解析来源 Hub
- `random`：随机或雪花型唯一后缀

如果后续已有中心注册服务，也可将 `knowledge_id -> hub` 的映射托管在中心目录服务中。

### 分享链接

分享链接是知识条目的统一入口，必须同时支持“人可看”和“agent 可导入”。

建议形态：

```text
https://<hub-domain>/k/<knowledge_id>
```

统一规则：

- 浏览器普通打开：返回 HTML 阅读页
- 机器访问：
  - `Accept: application/json`
  - 或 `?format=json`
  - 返回结构化描述和导入入口

### 知识条目版本

知识条目需要区分“条目身份”和“快照版本”。

建议字段：

- `knowledge_id`
  - 代表一次发布形成的稳定条目身份
  - 对外分享、导入、审计均以此为主键
- `snapshot_version`
  - 代表该条目绑定的快照版本
  - 用于判断导入内容是否发生变化

版本行为必须固定：

- 编辑标题、说明、标签、可见范围、指定用户列表
  - 不改变 `knowledge_id`
  - 不改变 `snapshot_version`
- 重新导出知识内容并重新发布
  - 默认生成新的 `knowledge_id`
  - 视为新的知识条目
- 若后续产品要支持“在原条目上发布新版本”
  - 必须显式选择“更新现有条目”
  - 保持 `knowledge_id` 不变
  - `snapshot_version` 递增
  - 分享页明确展示当前版本和历史版本

本期建议采用最简单规则：

- 一次导出发布生成一个新的 `knowledge_id`
- 条目元数据可编辑
- 条目快照内容不可原地替换

这样可以避免分享链接语义漂移，也更利于审计和复现。

### 可见范围

公开数据支持 3 档公开范围，另保留细粒度分享模式：

- `global`
  - 全网公开
  - 任意 Hub 上有权限访问公开知识的用户都可查看和导入
- `hub`
  - 本 Hub 公开
  - 仅当前 Hub 的用户可查看和导入
- `tenant`
  - 本租户公开
  - 仅当前 Hub 内当前租户用户可查看和导入
- `selected_users`
  - 指定用户可见
  - 仅显式授权的用户可查看和导入

注意：

- 知道知识 ID 或分享链接不等于一定有权限导入
- 任何导入前都必须再做一次访问控制校验

## 用户场景

### 场景 1：本地知识库导出为文件

用户在知识库中选择全部或部分来源，点击导出，得到一个可再次导入的本地知识包。

### 场景 2：导出后发布到 Hub

用户完成导出后，填写标题、说明、标签、可见范围，发布到 Hub，系统生成知识 ID 和分享链接。

### 场景 3：复制分享链接发给别人

接收者打开链接可以阅读简介、摘要和统计信息，并在有权限时一键导入。

### 场景 4：告诉 agent 导入某条知识

用户在聊天中直接说：

- “导入这个知识 ID：`kid_xxx`”
- “导入这个分享链接：`https://hub.example.com/k/kid_xxx`”

agent 解析后自动定位 Hub、检查权限、下载快照并导入。

### 场景 5：查看和管理自己发布的知识条目

用户在知识库 `导出` tab 点击 `查看分享`，跳转到 Hub 上的“我的知识导出”页面，查看每一次导出形成的知识条目，并可：

- 编辑标题与说明
- 修改标签
- 调整可见范围
- 更新指定可见用户
- 复制知识 ID
- 复制分享链接
- 删除或下架分享

## 界面设计

## 总体信息架构

保留现有知识库弹窗，并将一级 tab 调整为：

- `总览`
- `导入`
- `导出`
- `检索`
- `来源`
- `质量`

其中 `导出` 是本次新增重点。

## 导出 Tab

### 结构

`导出` tab 由三块组成：

1. `本地导出`
2. `发布到 Hub`
3. `已分享`

### 右上角动作

在 `导出` tab 顶部右侧增加按钮：

- `查看分享`

行为：

- 跳转到 Hub 上“我的知识导出”页面
- 展示当前用户历史发布的所有知识条目

### 本地导出区

字段与动作建议：

- 导出范围
  - 全部知识库
  - 当前筛选结果
  - 指定来源 ID
- 导出格式
  - `MaClaw Knowledge Package (.mckb.zip)` 默认
  - `Snapshot JSONL` 高级选项
- 是否脱敏
- 输出路径
- `导出`
- `导出并打开文件夹`

导出结果展示：

- 导出成功提示
- 文件路径
- 来源数 / 节点数 / 卡片数 / 事实数
- 文件大小
- 后续动作：
  - 发布到 Hub
  - 打开文件夹
  - 复制路径

### 发布到 Hub 区

这个区用于将“本次导出结果”发布成知识条目，而不是重新现查现导出。

字段建议：

- 标题
- 说明
  - 必填，作为分享页、Hub 浏览和后台知识管理的主要可读信息
- 标签
- 可见范围
  - 全网公开
  - 本 Hub 公开
  - 本租户公开
  - 指定用户可见
- 指定用户列表
- 是否允许再次导出
- 是否脱敏发布
- `发布到 Hub`

发布成功后结果区展示：

- 知识 ID
- 分享链接
- 可见范围
- 复制知识 ID
- 复制分享链接
- 打开分享页
- 查看分享

### 已分享区

展示最近若干条知识条目，每一项代表“一次导出”。

列表字段建议：

- 标题
- 知识 ID
- 说明摘要
- 可见范围
- 导出时间
- 来源数量
- 导入次数
- 状态

每项动作：

- 复制知识 ID
- 复制分享链接
- 打开分享页
- 编辑说明
- 删除分享

## 导入 Tab

在现有 `导入` tab 中新增两组入口。

### 整库导入

新增区块：

- 选择知识包文件
- 预检模式
- 导入策略
  - 合并导入
  - 仅新增
  - 覆盖冲突项
  - 整库替换
- 失败策略
  - 遇错继续
  - 遇错终止
- 安全备份开关
- `开始导入`

### 从 Hub 导入

新增区块：

- 输入知识 ID 或分享链接
- `解析`
- 展示解析结果：
  - 所属 Hub
  - 标题
  - 说明
  - 可见范围
  - 当前用户是否可访问
  - 是否可导入
  - 统计信息
- 导入策略
  - 合并导入
  - 仅新增
  - 覆盖冲突项
  - 整库替换
- `导入到本地知识库`

## Hub 页面设计

## 我的知识导出页

路径建议：

```text
/knowledge/my-exports
```

用于展示当前用户发布过的所有知识条目。

列表字段：

- 标题
- 知识 ID
- 说明
- 可见范围
- 标签
- 导出时间
- 更新时间
- 来源数
- 导入次数
- 状态

支持动作：

- 编辑
- 删除 / 下架
- 复制知识 ID
- 复制分享链接
- 查看详情

## 知识分享详情页

路径建议：

```text
/k/<knowledge_id>
```

这是统一的分享链接入口。

### 人类阅读模式

HTML 页面展示：

- 标题
- 说明
- 标签
- 发布者
- Hub
- 租户
- 可见范围
- 导出时间
- 来源数 / 卡片数 / 事实数
- 摘要预览
- 导入按钮
- 复制知识 ID
- 复制分享链接

## Hub 左侧 Tab：知识管理

Hub 后台需要在左侧导航新增独立一级 tab：`知识管理`，用于管理员查看和治理所有用户发布的分享知识条目。

导航位置：

- 位于 Hub 后台左侧导航
- 与用户、租户、应用、审计等后台管理入口同级
- 不挂在“我的知识导出”或某个普通用户知识页面下

路径建议：

```text
/admin/knowledge/exports
```

页面能力：

- 按用户列出分享知识
- 按租户列出分享知识
- 按发布时间或更新时间排序
- 按访问量或导入量排序
- 每页 50 条
- 管理员可强制删除知识条目

权限边界：

- Hub 管理员可查看全 Hub 所有租户、所有用户分享的知识条目
- 租户管理员只能查看自己租户下用户分享的知识条目
- 租户管理员不能查看其他租户的分享知识描述
- 租户管理员的强制删除权限仅限自己租户内的分享知识
- 普通用户不能进入 Hub 左侧 `知识管理` tab

列表字段：

- 知识 ID
- 标题
- 知识描述
- 发布用户
- 所属租户
- 可见范围
- 标签
- 发布时间
- 更新时间
- 访问量
- 导入量
- 状态

查看边界：

- 后台只查看知识描述和管理元数据
- 不展示知识包正文
- 不展示 source/card/fact 具体内容
- 不提供后台直接下载知识包入口，除非后续另行设计合规审计流程

强制删除规则：

- Hub 管理员可对全 Hub 范围执行
- 租户管理员仅可对自己租户范围执行
- 必须填写删除原因
- 删除 Hub 上的分享条目和可下载知识包
- 不删除发布者本地知识库
- 分享链接后续返回 `410`
- 必须写审计日志

审计日志至少记录：

- 管理员 ID
- 被删除知识 ID
- 原发布用户
- 原所属租户
- 删除原因
- 删除时间

### 机器访问模式

当请求满足以下条件之一时返回 JSON：

- `Accept: application/json`
- `?format=json`

返回建议：

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
  "labels": [
    "客服",
    "交接",
    "FAQ"
  ],
  "updated_at": "2026-06-26T10:00:00Z"
}
```

### 异常返回协议

为了让 agent 稳定处理分享链接，必须定义统一错误协议。

推荐 HTTP 状态码：

- `200`
  - 成功返回 HTML 或 JSON
- `401`
  - 未登录或登录态失效
- `403`
  - 已登录但无查看或导入权限
- `404`
  - 知识 ID 不存在
- `410`
  - 知识条目已删除或已永久下架
- `423`
  - 条目暂时锁定，例如发布处理中、审核中或存储修复中
- `502` / `503`
  - 跨 Hub 转发失败、上游 Hub 不可用或对象存储暂时不可用

机器模式错误体建议统一：

```json
{
  "error": {
    "code": "knowledge_not_found",
    "message": "Knowledge entry was not found.",
    "knowledge_id": "kid_hubcn01_9f3a2c7d8e41",
    "retryable": false,
    "can_login_and_retry": false
  }
}
```

错误码建议：

- `knowledge_not_found`
- `knowledge_deleted`
- `knowledge_access_denied`
- `knowledge_login_required`
- `knowledge_locked`
- `knowledge_hub_unavailable`
- `knowledge_download_unavailable`

agent 行为建议：

- `401`
  - 提示需要登录对应 Hub
- `403`
  - 直接说明没有权限，不重试下载
- `404` / `410`
  - 直接结束，并提示知识条目不可用
- `423` / `503`
  - 可提示稍后重试

### 设计原则

- 分享链接是统一入口
- agent 不依赖解析 HTML 正文来导入
- 真正导入依赖结构化 JSON 与下载地址
- HTML 和 JSON 共用同一个知识条目与权限模型

## 数据格式设计

## 本地交换格式

建议主格式为：

```text
.mckb.zip
```

内部结构建议：

```text
package/
  manifest.json
  snapshot.jsonl
  README.md
  meta/
    export.json
  editable/
    sources.json
    source_labels.json
  assets/
    ...
```

### 文件说明

- `manifest.json`
  - 包格式版本
  - 生成时间
  - 导出范围
  - 是否脱敏
  - 统计信息
- `snapshot.jsonl`
  - 系统导入主数据
  - 与现有底层 `ExportSnapshot` / `ImportSnapshot` 兼容
- `README.md`
  - 供人阅读的说明文件
- `meta/export.json`
  - 发布信息、Hub 元数据、知识 ID 等
- `editable/sources.json`
  - 面向人工可读的来源元数据
- `editable/source_labels.json`
  - 便于人工审阅和批量调整标签
- `assets/`
  - 可选附件副本

### 可恢复内容边界

导出包必须明确“哪些内容导出后能在另一台机器恢复为可用知识”，否则会出现“索引导过去了，正文没过去”的问题。

建议定义三层恢复等级：

- `full`
  - 包含原始正文或可恢复附件
  - 目标机器离线也能完成导入和后续检索
- `reference_only`
  - 仅保存来源引用和结构化派生结果
  - 适用于明确允许只做证据引用、不保证原文副本的场景
- `mixed`
  - 默认模式
  - 能嵌入的内容尽量嵌入，不能嵌入的保留引用并显式标记

本期建议默认要求：

- 文本、Markdown、用户粘贴内容
  - 必须内嵌全文
- 导入的本地文档
  - 若文件可读取，必须打包原始文件或其稳定转换副本
- URL / 网页抓取来源
  - 至少内嵌抓取时保存的正文快照
  - 不仅保留 URL
- 图片类来源
  - 若知识库中已有本地副本或摘要依赖图片内容，必须随包附带
- 仅远程引用、且因权限或体积不能随包附带的内容
  - 必须在 `manifest.json` 中标记为 `reference_only`
  - 导入页显式告知“部分来源仅保留引用，不保证原文恢复”

建议在 `manifest.json` 中增加：

- `recovery_mode`
- `embedded_asset_count`
- `reference_only_source_count`
- `missing_asset_count`

这样导入前就能预判包质量。

### 可编辑性的定义

本期“可编辑”定义为：

- 可读出来源、标题、说明、标签、范围信息
- 可手工修改说明和部分元数据
- 可再次导入

本期不承诺支持用户手工编辑 `snapshot.jsonl` 的内部关系图结构后仍完全无损回导。

## 与现有底层能力的关系

仓库中已存在：

- `KnowledgeExportSnapshotWithOptions`
- `KnowledgeImportSnapshot`
- `ExportSnapshot`
- `ImportSnapshot`

这些能力继续作为底座保留。本设计在其之上增加：

- ZIP 封装
- 面向用户的元数据层
- Hub 发布层
- 分享链接和知识 ID 层

## 数据模型设计

## 知识条目

建议新增或明确以下模型：

```text
KnowledgeShareEntry
```

字段建议：

- `knowledge_id`
- `export_id`
- `owner_user_id`
- `owner_tenant_id`
- `origin_hub_id`
- `origin_hub_url`
- `title`
- `description`
- `labels`
- `visibility_scope`
- `allowed_users`
- `allow_re_export`
- `redacted`
- `snapshot_format`
- `snapshot_version`
- `current_snapshot_version`
- `latest_download_token_version`
- `snapshot_path` 或 `blob_key`
- `snapshot_hash`
- `size_bytes`
- `source_count`
- `card_count`
- `fact_count`
- `import_count`
- `status`
- `created_at`
- `updated_at`
- `published_at`

说明：

- `knowledge_id` 是对外稳定标识
- `export_id` 是本地一次导出任务或导出记录的内部标识
- 同一份本地知识可以多次导出，生成多个知识条目

## 本地导入追踪模型

为了支持后续删除、重导、溯源和差异比对，建议在本地知识库导入记录中保存分享来源追踪信息。

建议新增或扩展：

```text
KnowledgeImportedShareRef
```

字段建议：

- `knowledge_id`
- `origin_hub_url`
- `share_url`
- `snapshot_version`
- `import_batch_id`
- `imported_at`
- `import_mode`
- `replace_all`

落地建议：

- 每次从 Hub 或分享链接导入时，都把这组信息写入 import batch 元数据
- 同时为每个导入 source 记录附加来源追踪字段或弱关联

这样未来才能支持：

- “删掉从某个知识条目导入的所有内容”
- “重新导入这条知识”
- “比较本地版本与分享版本是否有差异”
- “查看某条本地知识来自哪个分享链接”

## 知识定位结果

当系统根据知识 ID 或分享链接做解析时，返回：

- `knowledge_id`
- `origin_hub`
- `hub_display_name`
- `title`
- `visibility_scope`
- `can_view`
- `can_import`
- `status`

## 权限矩阵

| 场景 | 全网公开 | 本 Hub 公开 | 本租户公开 | 指定用户可见 |
| --- | --- | --- | --- | --- |
| 同 Hub 同租户用户浏览 | 允许 | 允许 | 允许 | 仅授权 |
| 同 Hub 不同租户用户浏览 | 允许 | 允许 | 拒绝 | 仅授权 |
| 不同 Hub 用户浏览 | 允许 | 拒绝 | 拒绝 | 仅授权且跨 Hub 允许 |
| 同 Hub 导入 | 允许 | 允许 | 按租户判断 | 仅授权 |
| 跨 Hub 导入 | 允许 | 拒绝 | 拒绝 | 仅显式授权且策略允许 |

补充规则：

- 发布者始终可见、可编辑、可删除
- 管理员是否可越权浏览由 Hub 管理策略决定，不纳入普通条目协议
- 任何下载前再次校验权限

## 接口设计

## 本地 GUI / Wails 接口

建议新增：

- `KnowledgeCreateExportPackage(req)`
- `KnowledgePublishExportToHub(req)`
- `KnowledgeResolveShareTarget(input)`
- `KnowledgeImportFromHub(req)`
- `KnowledgeListMySharedExports(req)`
- `KnowledgeUpdateSharedExport(req)`
- `KnowledgeDeleteSharedExport(req)`
- `KnowledgeDownloadSharedExport(req)`

其中：

- `KnowledgeCreateExportPackage`
  - 在现有 `KnowledgeExportSnapshotWithOptions` 基础上生成 `.mckb.zip`
- `KnowledgeResolveShareTarget`
  - 输入知识 ID 或分享链接，返回解析结果
- `KnowledgeDownloadSharedExport`
  - 根据知识 ID 或分享目标获取受控下载地址或拉取到本地临时包

## Hub HTTP API

建议新增：

- `POST /api/knowledge/exports/publish`
- `GET /api/knowledge/exports/my`
- `GET /api/knowledge/exports/{knowledge_id}`
- `PATCH /api/knowledge/exports/{knowledge_id}`
- `DELETE /api/knowledge/exports/{knowledge_id}`
- `GET /api/knowledge/exports/{knowledge_id}/download`
- `GET /api/knowledge/resolve/{knowledge_id}`
- `POST /api/knowledge/exports/{knowledge_id}/download-token`

分享页相关：

- `GET /k/{knowledge_id}`
  - 默认 HTML
  - 支持 `Accept: application/json` 或 `?format=json`

## 下载授权协议

分享页 JSON 只负责描述知识条目，真正下载知识包必须走受控授权协议。

建议支持两种实现方式，默认优先第一种：

### 方案 A：短期下载令牌

流程：

1. 客户端或 agent 解析知识条目
2. 调用：

```text
POST /api/knowledge/exports/{knowledge_id}/download-token
```

3. 服务端根据当前用户权限签发短期令牌
4. 返回：
  - `download_url`
  - `expires_at`
  - `token_type`
  - `snapshot_version`
5. 客户端再下载知识包

优点：

- 最适合跨 Hub
- 不要求对象存储永久暴露
- 方便做一次性、短时、可审计下载

### 方案 B：服务端代拉取

流程：

1. 客户端请求本地 Hub
2. 本地 Hub 代表当前用户去目标 Hub 拉取知识包
3. 本地 Hub 回传给客户端

优点：

- 客户端不需要直接持有跨 Hub 下载凭证

缺点：

- Hub 之间转发复杂度更高
- 大文件流转压力更大

本期建议采用：

- 同 Hub 下载：可直接使用当前登录态或短期令牌
- 跨 Hub 下载：统一使用短期下载令牌

下载令牌返回示例：

```json
{
  "knowledge_id": "kid_hubcn01_9f3a2c7d8e41",
  "snapshot_version": "v1",
  "download_url": "https://hub.example.com/api/knowledge/exports/kid_hubcn01_9f3a2c7d8e41/download?token=abc",
  "token_type": "signed_url",
  "expires_at": "2026-06-26T10:15:00Z"
}
```

约束：

- 下载令牌必须短期有效
- 令牌必须绑定 `knowledge_id`
- 可选绑定当前用户、当前 Hub、当前客户端
- 每次下载都记录审计日志

## 解析与导入流程

### 按知识 ID 导入

1. 用户输入知识 ID
2. 客户端或 agent 调用 `ResolveKnowledgeID`
3. 系统返回所属 Hub 和访问结果
4. 客户端获取 JSON 描述
5. 若 `can_import=true`，请求下载令牌或受控下载地址
6. 下载知识包到本地临时目录
7. 调用本地导入逻辑完成导入
8. 写入本地导入追踪信息

### 按分享链接导入

1. 用户输入分享链接
2. 系统解析其中的 `knowledge_id`
3. 访问 `/k/{knowledge_id}?format=json`
4. 获取导入描述
5. 请求下载令牌或受控下载地址
6. 下载包并导入
7. 写入本地导入追踪信息

## Agent 工具设计

为了让 agent 能自主操作，建议在现有知识库工具之外新增以下工具。

## 本地工具

- `knowledge_export_package`
  - 导出本地知识库为可交换包
- `knowledge_import_package`
  - 从本地 `.mckb.zip` 导入
- `knowledge_list_exports`
  - 列出本地最近导出记录

## Hub 工具

- `knowledge_publish_to_hub`
  - 将本地导出结果发布成知识条目
- `knowledge_resolve_id`
  - 根据知识 ID 解析所属 Hub 与权限
- `knowledge_resolve_share_link`
  - 根据分享链接解析知识 ID 和导入描述
- `knowledge_import_from_hub`
  - 根据知识 ID 或分享链接导入知识
- `knowledge_list_hub_exports`
  - 查看当前用户已分享的知识条目
- `knowledge_update_hub_export`
  - 编辑说明、标签、可见范围
- `knowledge_delete_hub_export`
  - 删除或下架分享条目

## Agent 使用原则

- agent 收到分享链接时，优先走结构化 JSON，不依赖网页正文抓取
- agent 收到知识 ID 时，先做 Hub 解析，再做导入
- agent 在执行整库替换导入前，应提示或明确确认高风险动作

## 复用现有实现的建议

当前代码已具备以下基础：

- 本地知识快照导出与导入
- 现有知识库 GUI 结构
- 现有知识工具注册机制
- Hub 侧公共知识库与管理页雏形

因此本设计优先采用“增量扩展”：

- 复用现有 `KnowledgeSettingsPanel`，新增 `导出` tab
- 复用底层 snapshot 逻辑，不重新发明导出格式
- 复用 Hub 现有知识浏览与权限基础设施，新增“知识条目”层
- 复用 agent 工具注册机制，补齐新工具

## 风险与注意事项

### 格式风险

如果让用户直接手工编辑底层 `snapshot.jsonl`，容易破坏引用关系。应将“可编辑”更多放在元数据和说明层，而不是内部索引层。

### 权限风险

知识 ID 可传播，但不能绕过权限。必须保证：

- 解析成功不等于可下载
- 可浏览不等于可导入
- 下载接口再次鉴权

### 跨 Hub 依赖

全网公开模式要求系统可以根据知识 ID 找到所属 Hub。若当前基础设施中还没有稳定 Hub 目录服务，则需要在第一期明确采用：

- 内嵌 Hub 路由段的知识 ID
- 或中心解析服务

### 大包导入风险

整库替换是高风险操作，必须具备：

- 预检
- 冲突摘要
- 安全备份
- 可回滚说明

## 实施分期

## 第一阶段：本地导出与整库导入

- 新增 `导出` tab
- 支持本地导出为 `.mckb.zip`
- 支持整库导入本地知识包
- 支持预检、合并、覆盖、替换
- 补齐本地 agent 导入导出工具

交付结果：

- 单机跨机器知识交换可用
- 不依赖 Hub 即可完成备份和迁移

## 第二阶段：Hub 发布与我的知识导出页

- 发布到 Hub
- 生成知识 ID 和分享链接
- 新增“我的知识导出”页面
- 支持编辑说明、删除分享、复制链接

交付结果：

- 用户可以管理每一次导出形成的知识条目

## 第三阶段：分享页与按知识 ID / 分享链接导入

- 实现 `/k/{knowledge_id}` 统一分享入口
- 支持 HTML 和 JSON 双模式
- 导入 tab 增加按知识 ID / 分享链接导入
- agent 支持按 ID 或分享链接直接导入

交付结果：

- 链接既能给人看，也能给 agent 用

## 第四阶段：跨 Hub 公开策略完善

- 明确全网公开解析策略
- 完善跨 Hub 授权、审计和导入统计
- 增加导入来源追踪和安全治理

## 建议的开发顺序

1. 先完成本地 `.mckb.zip` 导出与整库导入
2. 再补知识条目模型和 Hub 发布
3. 再补分享链接 HTML / JSON 双模式
4. 最后补跨 Hub 路由和更细粒度权限治理

这个顺序的原因是：

- 本地 snapshot 底座已经存在，最容易先做出结果
- Hub 条目与分享协议需要产品结构先稳定
- 先有包，再有分享，能减少接口返工

## 决策摘要

- 分享对象是“某一次导出形成的知识条目”
- 每个知识条目都有唯一知识 ID 和统一分享链接
- 分享链接是人机双用入口
- 人类默认看 HTML 页面
- agent 默认取结构化 JSON，再下载知识包导入
- 可见范围采用：全网公开 / 本 Hub 公开 / 本租户公开 / 指定用户可见
- `导出` tab 需要增加 `查看分享` 按钮，跳转到 Hub 上“我的知识导出”页面
- `导入` tab 需要增加整库导入与按知识 ID / 分享链接导入
- agent 需要补齐导出、发布、解析、导入、管理分享条目的工具

## 后续开发建议

在正式开工前，建议再补两份更细的子设计：

- 前端交互稿
  - `KnowledgeSettingsPanel` 的 `导出` tab 布局
  - 导入 tab 新区块交互
  - 发布成功态与错误态
- API 与类型稿
  - Wails 方法签名
  - Hub HTTP API 请求与响应
  - agent tool schema

这样可以直接进入分模块实施，而不必在开发过程中反复重构协议。
