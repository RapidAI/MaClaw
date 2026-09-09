# Skill Suite 支持开发设计文档

## 1. 背景与目标

当前系统以单个 Skill 为发布、下载和安装单位。HubCenter 的“能力目录”通过 `/api/admin/capability-market/import` 导入外部能力，GitHub 导入目前只处理一个 Skill；虽然底层已经能够从仓库递归发现多个 `skill.yaml`/`skill.md`，但导入结果没有形成统一的 Suite。MaClaw GUI 也只能逐个下载和安装。

本需求引入 **Skill Suite**：一个 Suite 是一个可分发的软件包，包含一组有明确关联的 Skill。Suite 作为市场目录和下载单位，Suite 内的 Skill 仍然保持独立的运行时身份、版本和安装目录。

目标：

- Hub/HubCenter 可以从同一个 GitHub 仓库导入多个 Skill 并归并为 Suite。
- Suite 可以被审核、发布、搜索、查看详情和下载。
- MaClaw GUI 可以查看 Suite 中的 Skill，并一次安装多个 Skill。
- 安装过程具备事务性、完整性校验和失败回滚能力。
- 现有单 Skill API、上传包和已安装 Skill 不受影响。

非目标：

- 不把多个 Skill 合并成一个运行时 Skill。
- 不改变现有 `manage_skill` 的单 Skill 执行语义。
- 不在第一阶段实现跨 Suite 的依赖解析或 Suite 间嵌套。

## 2. 术语和对象关系

| 对象 | 说明 |
| --- | --- |
| Skill | 可独立执行、安装和升级的最小运行时单元 |
| Suite | 一组相关 Skill 的发布和分发单元 |
| Suite Member | Suite 中的一个 Skill 引用，包含 Skill ID、路径和版本 |
| Source Revision | GitHub 仓库的不可变提交 SHA；用于复现导入结果 |
| Suite Package | 下载给 GUI 的归档，包含 Suite 清单和多个 Skill 目录 |

一个 Suite 包含多个 Suite Member；一个 Skill 可以属于多个 Suite，但每次导入都必须保存来源仓库、提交 SHA 和成员路径。

## 3. Suite 定义和仓库布局

### 3.1 显式定义（推荐）

仓库根目录可提供 `suite.yaml`（也接受 `skill-suite.yaml` 作为兼容别名）：

```yaml
id: office-suite
name: Office 办公技能包
version: 1.0.0
description: PDF、表格和文档处理技能集合
author: example
license: Apache-2.0
tags:
  - office
  - productivity
skills:
  - id: pdf-tools
    path: skills/pdf-tools
    required: true
  - id: spreadsheet-tools
    path: skills/spreadsheet-tools
    required: false
```

字段约束：

- `id` 在 Suite 目录内稳定且唯一，只允许安全的 slug 字符。
- `version` 使用 SemVer；缺省为 `1.0.0`。
- `skills[].path` 必须是仓库内相对路径，不能包含 `..`、绝对路径或符号链接。
- `skills[].id` 缺省时取 Skill 清单中的稳定 `skill_id`，再缺省取目录名。
- `required` 仅影响 GUI 默认选择和安装门禁，不影响 Skill 独立下载。

### 3.2 自动归并

当仓库没有 `suite.yaml` 时，导入器将指定子路径下发现的全部合法 Skill 自动组成 Suite：

- Suite ID：`github.<owner>.<repo>`，发生冲突时追加规范化的子路径。
- Suite 名称：优先仓库名，其次为子路径名称。
- Suite 版本：优先所有成员版本的最高 SemVer；无法解析时使用 revision 标识。
- 自动归并结果必须标记 `definition_source: auto`，便于管理员补充元数据。

仓库根目录本身只有一个 Skill 时，仍可按现有单 Skill 流程发布；管理员明确选择 Suite 模式时才创建单成员 Suite。

### 3.3 能力目录导入界面

HubCenter 管理后台的“能力目录”导入框当前位于 `hubcenter/web/admin/assets/js/gossip-admin.js`，现有 GitHub 导入按钮调用 `/api/admin/capability-market/import`。该页面需要：

- GitHub 仓库 URL 导入时发送 `package_kind: suite`（可提供“自动识别/导入 Suite”选项，默认开启）。
- 导入成功后显示 Suite 名称、版本、成员数量和成员 Skill 名称，而不是固定显示“导入 1 个 Skill”。
- 当仓库只包含一个 Skill 时允许提示“检测到单 Skill”，但仍可由管理员决定按单 Skill 或单成员 Suite 发布。
- 能力目录列表增加 `package_kind`/`suite_id` 列，并提供 Suite 详情入口。
- MCP、ClawHub 和已有单 Skill 导入 UI 保持原有请求格式。

## 4. Suite 包格式

Suite 下载包采用 ZIP，目录结构如下：

```text
suite.yaml
suite_integrity_manifest.json
skills/
  pdf-tools/
    skill.yaml
    skill.md
    scripts/...
  spreadsheet-tools/
    skill.yaml
    scripts/...
```

要求：

- `suite.yaml` 是下载包的权威清单，列出 Suite 元数据和成员目录。
- 每个成员目录必须是可独立安装的标准 Skill 包。
- `suite_integrity_manifest.json` 记录 Suite 清单和所有成员文件的 SHA-256。
- ZIP 内禁止路径穿越、符号链接、重复路径和未知顶层可执行文件。
- 包大小、单文件大小、递归文件数沿用现有 Skill 包限制；Suite 额外限制成员数（初始建议 32 个）。
- Suite 包必须记录 `source_url`、`source_revision` 和生成时间。

## 5. 服务端数据模型

在 `hubcenter/internal/skill` 增加以下模型：

```go
type SkillSuiteMeta struct {
    ID               string             `json:"id"`
    Name             string             `json:"name"`
    Description      string             `json:"description,omitempty"`
    Version          string             `json:"version"`
    Author           string             `json:"author,omitempty"`
    Tags             []string           `json:"tags,omitempty"`
    SourceURL        string             `json:"source_url"`
    SourceRevision   string             `json:"source_revision,omitempty"`
    DefinitionSource string             `json:"definition_source"`
    Members          []SkillSuiteMember `json:"members"`
    Price            int                `json:"price,omitempty"`
    TrustLevel       string             `json:"trust_level"`
    Status           string             `json:"status"`
    Visible          bool               `json:"visible"`
    Downloads        int                `json:"downloads"`
    CreatedAt        string             `json:"created_at"`
    UpdatedAt        string             `json:"updated_at"`
}

type SkillSuiteMember struct {
    SkillID  string `json:"skill_id"`
    SkillRef string `json:"skill_ref,omitempty"`
    Name     string `json:"name"`
    Path     string `json:"path"`
    Version  string `json:"version,omitempty"`
    Required bool   `json:"required"`
}

type SkillSuiteFull struct {
    SkillSuiteMeta
    Skills        []HubSkillFull `json:"skills"`
    Manifest      SuiteManifest  `json:"manifest"`
    PackageSHA256 string         `json:"package_sha256,omitempty"`
    PackageSize   int64          `json:"package_size,omitempty"`
}
```

建议增加独立 Suite 表和关联表，而不是把多个 Skill JSON 塞入现有 Skill 行：

- `skill_suites`：Suite 元数据、审核状态、价格、来源和统计。
- `skill_suite_members`：`suite_id`、`skill_id`、成员路径、版本、required、排序。
- `skill_suite_revisions`（第二阶段）：支持同一 Suite 的多版本历史。

现有 `HubSkillMeta` 可以增加可选 `suite_ids` 或 `suite_count` 用于展示，但不能改变单 Skill 主键和下载行为。

## 6. Hub/HubCenter API

### 6.1 能力目录管理员导入（主入口）

本需求的主入口是能力目录现有接口，而不是单独的 SkillHub 管理接口：

```http
POST /api/admin/capability-market/import
```

现有字段保持兼容：`capability_type` 仍为 `skill`，`source` 为 `github`，`install_ref` 接收 GitHub 仓库 URL（也兼容单个 Skill 的 raw URL）。新增字段：

```json
{
  "capability_type": "skill",
  "source": "github",
  "install_ref": "https://github.com/example/office-skills",
  "package_kind": "suite",
  "suite_id": "office-suite",
  "publish_members": true
}
```

处理规则：

- `package_kind: suite` 时，服务端按仓库 URL 获取固定 source revision，扫描并校验多个 Skill，创建一个 Suite 目录条目，并将每个 Skill 写入 Suite Member 关联表。
- `package_kind` 省略时，如果仓库只发现一个 Skill，保持现有单 Skill 导入；发现多个 Skill 时默认创建 Suite，并在响应中返回 `package_kind: suite`。
- `publish_members=true` 时，成员 Skill 同时写入现有 SkillStore，保证旧客户端仍可按单 Skill 查询和下载；成员的来源、版本和统计字段不得被 Suite 导入重置。
- `source` 非 GitHub 或 `capability_type` 非 `skill` 时，继续沿用现有 MCP/ClawHub 分支，不进入 Suite 逻辑。

成功响应示例：

```json
{
  "capability_type": "skill",
  "package_kind": "suite",
  "suite": {
    "id": "office-suite",
    "version": "1.0.0",
    "source_url": "https://github.com/example/office-skills",
    "source_revision": "abc123...",
    "members": ["pdf-tools", "spreadsheet-tools"]
  },
  "capability_id": "office-suite",
  "published": ["pdf-tools", "spreadsheet-tools"],
  "errors": [],
  "total": 2
}
```

响应中的 `capability_id` 对 Suite 指向 Suite ID；成员的独立 ID 通过 `members` 或现有目录查询获得。部分成员导入失败时，Suite 进入 `needs_review`，不能标记为完整发布；管理员可重试导入或放弃本次 Suite。

### 6.2 管理员单 Skill 导入兼容接口

保留现有接口，供旧管理页面和单 Skill raw URL 使用：

```http
POST /api/admin/skillhub/import-url
```

请求扩展：

```json
{
  "url": "https://github.com/example/office-skills",
  "kind": "suite",
  "suite_id": "office-suite",
  "publish_members": true
}
```

该接口不承担能力目录 Suite 的主流程。为了兼容旧客户端，仍可支持 `kind: suite`，但新页面和新客户端必须使用 `/api/admin/capability-market/import`。

响应：

```json
{
  "suite": { "id": "office-suite", "version": "1.0.0", "members": [] },
  "published": ["pdf-tools", "spreadsheet-tools"],
  "errors": [],
  "total": 2
}
```

导入过程按 source URL + source revision + suite ID 幂等；重复导入更新同一 Suite 和成员，不产生重复目录项。

### 6.3 Suite 查询和下载

新增公共接口：

```http
GET /api/v1/skill-suites/{id}
GET /api/v1/skill-suites/{id}/download
GET /api/v1/skillmarket/suites/search?q=...
```

`download` 返回 `SkillSuiteFull` JSON 或 ZIP（由 `format=zip|json` 控制；第一阶段可先实现 JSON）。响应必须包含完整成员 Skill 内容、文件映射、校验和及版本信息。

SkillMarket 下载的计费规则：Suite 有价格时只扣一次；通过 Suite 下载成员 Skill 不再次扣费。单 Skill 下载继续沿用现有计费和权限逻辑。

### 6.4 Hub 同步

HubCenter HA 快照、Hub 到 HubCenter 的同步消息增加 `Suites` 字段。旧节点忽略该字段，Suite 不应阻塞已有 Skill 同步。同步必须采用全量替换或带 revision 的幂等 upsert，禁止半套成员可见。

## 7. GUI 下载和安装

### 7.1 Wails 接口

在 `guiapp/skillmarket_client.go` 和 Wails bindings 增加：

```go
DownloadSkillSuite(ctx context.Context, suiteID string) (*SkillSuiteFull, error)
InstallSkillSuite(suiteID string, selectedSkillIDs []string) (SkillSuiteInstallResult, error)
```

`SkillSuiteInstallResult` 至少包含 Suite ID、已安装成员、跳过成员、失败成员、事务状态和错误信息。

### 7.2 安装策略

- GUI 展示 Suite 元数据、成员列表、版本、required 标记和本机兼容性。
- 默认选择全部 `required=true` 成员及用户勾选的 optional 成员。
- 下载后先在临时目录解包、验证 Suite manifest，再逐个运行现有 Skill scanner/preflight。
- 所有成员预检通过后才进入安装提交阶段。
- 提交阶段复用现有安装锁、Skill committer 和索引刷新逻辑。
- 任意成员提交失败时回滚本次 Suite 已提交的全部成员；回滚失败进入现有 compensation/review 队列。
- 已安装且版本相同的成员返回 `skipped/already_current`；版本更高时走更新流程。
- 成员目录名称来自安全 slug，不使用远程提供的绝对路径。

第一阶段采用“全量原子安装”。部分成功模式只保留数据结构，不在 UI 暴露，避免用户误以为 Suite 已完整安装。

### 7.3 前端交互

在 Skills 管理界面新增 Suite 卡片或筛选项：

- Suite 名称、版本、作者、来源和成员数量。
- 成员清单及安装状态。
- “查看详情”“全选/取消全选”“安装 Suite”按钮。
- 安装进度按成员显示，结束时显示 committed、skipped 或 rolled_back。
- Suite 安装失败时显示首个失败成员和回滚结果，不隐藏安全扫描错误。

## 8. 安全、完整性和权限

- GitHub URL 解析沿用现有白名单和 HTTPS 限制；保存最终 commit SHA，避免分支漂移。
- Suite 和每个成员都执行现有 YAML schema、静态文件扫描、安全扫描和平台兼容性检查。
- Suite manifest 采用规范化路径，拒绝 `..`、绝对路径、符号链接、重复成员 ID 和重复文件。
- 下载包校验顺序：响应大小限制 → ZIP 路径检查 → Suite manifest → 文件 SHA-256 → 成员 Skill scanner。
- 管理员导入的 Suite 默认 `trust_level=trusted`，普通用户上传仍为 `community`。
- Suite 权限为成员权限并集，仅用于展示和安装前确认；运行时仍按具体 Skill 的权限声明执行。
- 购买、下载计数、审核、下架和退款事件需要同时记录 Suite ID 与成员 Skill ID。

## 9. 兼容性和迁移

- 现有 `/api/v1/skills/{id}/download`、SkillMarket 单 Skill 下载和 `InstallMixedSkill` 保持不变。
- 旧客户端看不到 Suite 时，用户仍可通过成员 Skill 独立安装。
- 旧 HubCenter 节点不支持 Suite 时，GUI 应降级为提示“Suite 不受支持”，不能把 Suite JSON 当作单 Skill 安装。
- 现有独立 Skill 可通过后台任务按 source URL 聚合为自动 Suite；该迁移默认只建立关联，不改变 Skill ID、版本和安装目录。
- 配置、缓存和审计数据增加可选 Suite 字段，读取旧数据时使用空值。

## 10. 实施阶段

### 阶段 1：协议和领域模型

- 增加 Suite 类型、校验器、manifest 和 ZIP/JSON 编解码。
- 为 Suite ID、版本、成员路径和来源 revision 增加单元测试。

### 阶段 2：HubCenter 导入、存储和 API

- 扩展 GitHub importer，支持显式 `suite.yaml` 和自动归并。
- 增加 Suite store、关联表、管理员导入接口、详情和下载接口。
- 增加 HA 快照和 SkillMarket 搜索结果映射。
- 将能力目录 GitHub 导入页面切换到 Suite-aware 请求和结果展示；保留旧 SkillHub 导入入口作为兼容路径。

### 阶段 3：GUI 安装服务

- 增加 Suite 下载客户端和 Wails binding。
- 抽取可复用的多 Skill 安装事务，接入现有回滚/补偿机制。
- 增加下载完整性、预检、重复安装和回滚测试。

### 阶段 4：前端和发布

- 增加 Suite 搜索结果、详情弹窗、成员选择和进度展示。
- 更新 API 文档、管理员页面和用户帮助。
- 灰度启用 Suite 搜索和安装，保留单 Skill fallback。

## 11. 测试和验收标准

服务端：

- 能从包含 2 个以上 Skill 的 GitHub 仓库生成一个 Suite。
- 显式清单、自动归并、分支解析和 source revision 均有测试。
- 非法路径、重复成员、损坏文件、超大包和 manifest 校验失败会被拒绝。
- 重复导入幂等，成员更新不会重置下载量、评分和审核字段。
- Suite 下载返回完整成员内容，单 Skill 下载行为不变。

GUI：

- Suite 可下载并显示全部成员。
- 全量安装成功后所有成员均出现在本地 Skill 列表。
- 任意成员预检或提交失败时，已提交成员全部回滚，且生成正确审计/补偿记录。
- 可选择 optional 成员；required 成员不能被取消。
- 重复安装正确跳过相同版本，升级遵循现有版本策略。
- 旧 Hub/旧响应不会导致崩溃或误安装。

建议测试命令：

```text
go test ./hubcenter/internal/skill/... ./hubcenter/internal/httpapi/...
go test ./guiapp/... -run 'Suite|SkillMarket'
cd guiapp/frontend && npm test -- --run SkillsManagementPanel
```

## 12. 待确认决策

以下决策会影响 API 和数据库最终形态，开发前需要确认：

1. 是否强制要求仓库提供 `suite.yaml`，还是保留自动归并？
2. Suite 是否允许用户只安装部分 Skill？本文默认允许选择 optional，required 必须安装。
3. Suite 是否按整体计费？本文默认 Suite 只扣一次，成员独立购买规则不变。
4. 第一阶段是否只返回 JSON，还是同时提供 ZIP 下载？
5. Suite 是否需要独立版本历史和升级策略，还是先绑定单一 revision？

## 13. GUI 本地 Skill 组合上传

Maclaw GUI 支持通过 Agent 的 `manage_skill` 工具将多个已安装 Skill 组合成 Suite：

```json
{"action":"upload_suite","names":["pdf-tools","office-export"],"suite_name":"办公自动化套件"}
```

`names` 至少包含两个本地 Skill（包括目录型和配置型/自学习 Skill）。GUI 会逐个执行可移植性与质量检查，
将清理后的文件编码进 Suite JSON，然后按能力市场上传目标设置提交到 HubCenter。
HubCenter 新增 `POST /api/v1/skill-suites/submit`，认证方式与普通 Skill 提交一致；
服务端会同时发布成员 Skill 和 Suite 目录记录，因此旧客户端仍可按单 Skill 安装。
上传失败不会留下半成品 Suite。`force=true` 仅跳过 Agent 侧使用次数门禁，仍保留
文件完整性和服务器认证校验。
GUI 还可通过 `GET /api/v1/skill-suites` 获取 Suite 目录；下载接口支持
`format=json`（默认）和 `format=zip` 两种形式。
能力市场卡片提供“下载”和“安装套件”操作，安装时会提示是否包含 optional 成员。

当能力市场策略选择企业 Hub 时，GUI 改用企业接口
`POST /api/capabilities/skill-suites/submit`；Suite 包按租户隔离保存，
并通过 `GET /api/capabilities/skill-suites/{id}/download` 返回完整 JSON
定义。企业 Hub 的能力目录以 `package_kind: "suite"` 标记该条目。

## 14. 商业化、审计与版本运维（已实现）

- `POST /api/v1/skillmarket/suites/{id}/purchase?email=...` 对 Suite 整体只扣一次 Credits，写入 `sm_suite_purchases`（包含成员 Skill ID 与购买版本）；同一买家重复请求返回原购买记录，不重复扣费。
- 管理员沿用 `POST /api/v1/admin/refund`，传入 Suite purchase ID 即可整单退款，退款操作具备并发幂等保护。
- 下载（JSON/ZIP）写入 `sm_suite_audit_events` 的 `download` 事件；购买和退款分别写入对应事件。管理员可通过 `GET /api/v1/admin/skillmarket/suites/{id}/audit` 查询事件，买家可通过 `GET /api/v1/skillmarket/suites/{id}/purchases` 查询购买记录。
- 审计接口支持 `event_type` 和 `limit` 查询参数。
- 购买响应返回带 `purchase_id` 的下载地址；携带已退款或不匹配的 purchase ID 下载会被拒绝。
- 管理员可通过 `POST /api/v1/skill-suites/{id}/versions`（或 `/skillmarket/suites/...`）发布新版本，通过 `POST /api/v1/skill-suites/{id}/rollback` 提交 `{ "version": "x.y.z" }` 回滚到历史版本。服务端为历史版本保留完整 Suite 快照，最多保留 20 条元数据记录；回滚会生成新的当前版本记录并触发 HA 同步。
- `GET /api/v1/skill-suites/{id}/versions` 返回当前版本及历史版本摘要。
- `GET /api/v1/skillmarket/suite-purchases/{purchase_id}` 返回单笔 Suite 购买状态，管理员路径同时提供权限保护。
- 本期不执行独立 Skill 自动聚合迁移；已存在的单 Skill 记录继续按原有规则工作。
- Enterprise Hub 同步提供 `GET /api/capabilities/skill-suites/{id}/audit` 与 `POST /api/capabilities/skill-suites/{id}/rollback`，审计数据按租户隔离。
