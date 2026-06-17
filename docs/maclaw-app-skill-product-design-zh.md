# MaClaw App Skill 产物与设计器上架设计

## 目标

MaClaw App 不只是应用面板里的本地快捷入口，而是一种可以被设计、测试、注册、安装、升级和上架的 Skill 产物。

目标闭环：

```text
App Studio 拖拽/配置 UI
  -> 绑定 Skill / Workflow / DataSrv / MCP action
  -> 本地测试
  -> 生成 MaClaw App Skill 包
  -> 注册到 GUI 技能页的 MaClaw App 分类
  -> 用户可添加到应用面板
  -> 测试通过后上传 SkillMarket / 能力市场
  -> 市场审核、发布、安装、升级
```

这个设计补齐三个边界：

- `App Studio` 是设计器，不是最终产物本身。
- `MaClaw App Skill` 是可分发产物，包含 `skill.yaml`、`maclaw.app.json`、图标、运行脚本或绑定声明。
- `应用面板` 是使用入口，显示已安装或已加入面板的 app。

## 核心对象

## 归属模式

默认模式采用“Skill 包内置 App 描述文件”：

```text
contract-review/
  skill.yaml
  SKILL.md
  maclaw.app.json
```

这个模式中，`maclaw.app.json` 和 Skill 逻辑同目录、同版本、同市场上传包。安装 Skill 后，客户端扫描同目录下的 App 描述文件，自动把它识别为 `MaClaw App Skill`。

逻辑上，App 描述文件仍显式绑定 Skill：

```json
{
  "schema": "maclaw.app.v1",
  "privateMarker": "x_maclaw_apps",
  "app": {
    "id": "contract-review-app",
    "name": "合同审查",
    "kind": "tool_app",
    "binding": {
      "skill": {
        "id": "contract-review"
      }
    }
  }
}
```

也就是：

```text
物理归属：App 描述文件放在 Skill 包内。
逻辑绑定：App 描述文件声明绑定哪个 Skill。
```

高级模式允许独立 App 包引用已有 Skill：

```text
finance-app-pack/
  maclaw.app.json
```

适用场景：

- 企业给同一个 Skill 下发不同 UI。
- 不改原 Skill 包，只叠加 UI 入口。
- 一个企业应用包组合多个 Skill、MCP、DataSrv 能力。

第一阶段优先实现默认模式：从现有 Skill 选择并在该 Skill 目录写入 `maclaw.app.json`。

### MaClaw App Skill

一个 MaClaw App Skill 是普通 Skill 包的扩展形式。它仍由 Skill 管理、搜索、安装和上传流程承载，但声明自己包含可视化应用定义。

推荐目录：

```text
skills/
  invoice-review-app/
    skill.yaml
    maclaw.app.json
    README.md
    icon.png
    runtime/
      run.py
```

`skill.yaml` 用于 Skill 体系识别、执行、打包和上传：

```yaml
name: invoice-review-app
description: 发票审核 App
category: maclaw_app
triggers:
  - 发票审核
  - invoice review
platforms:
  - universal
maclaw_app:
  schema: maclaw.app.v1
  entry: maclaw.app.json
  add_to_app_panel: true
  designer: ag-ui
```

`maclaw.app.json` 是设计器产物：

```json
{
  "schema": "maclaw.app.v1",
  "privateMarker": "x_maclaw_apps",
  "app": {
    "id": "invoice-review",
    "name": "发票审核",
    "kind": "tool_app",
    "category": "文档处理",
    "icon": "shield",
    "launchMode": "dynamic_ag_ui",
    "ui": {
      "renderer": "ag-ui",
      "state": {
        "files": [],
        "strict": true,
        "lastRun": null
      },
      "layout": {
        "type": "page",
        "children": [
          { "type": "fileUpload", "bind": "files", "label": "上传发票" },
          { "type": "checkbox", "bind": "strict", "label": "严格模式" },
          {
            "type": "button",
            "label": "开始审核",
            "action": {
              "type": "runSkill",
              "skillId": "invoice-review-app",
              "input": {
                "files": "$state.files",
                "strict": "$state.strict"
              },
              "output": {
                "result": "$state.lastRun"
              }
            }
          },
          { "type": "resultPanel", "bind": "lastRun" }
        ]
      }
    },
    "binding": {
      "skill": {
        "id": "invoice-review-app",
        "inputMode": "mixed",
        "outputModes": ["markdown", "pdf"]
      }
    }
  }
}
```

### App Definition

`maclaw.app.json` 定义用户可见应用：

- 应用 id、名称、图标、分类。
- UI schema。
- action binding。
- input/output contract。
- artifact 展示规则。
- 权限声明。
- 发布治理信息。

### Skill Package

Skill 包承载分发能力：

- 市场搜索和安装使用 Skill 现有通道。
- 安装后扫描 `maclaw_app.entry` 或 `maclaw.apps.json`。
- 注册为 `MaClaw App` 分类。
- 可选择加入应用面板。

## UI 与 Skill 联动

动态 UI 不直接执行后端逻辑，只声明 action。运行器负责把 action 转为后端调用。

```text
Dynamic UI Renderer
  -> UI State
  -> Action Runtime
  -> Skill Adapter
  -> Skill Runner
  -> Result Normalizer
  -> Output Blocks + Artifacts
  -> Result Panel
```

action 示例：

```json
{
  "type": "runSkill",
  "skillId": "invoice-review-app",
  "input": {
    "files": "$state.files",
    "policy": "$state.policy"
  },
  "output": {
    "result": "$state.lastRun"
  }
}
```

运行器职责：

- 解析 `$state.*`。
- 校验必填字段和文件。
- 将上传文件 stage 到受控目录或 artifact store。
- 调用现有 `RunNLSkillAsync`。
- 订阅或轮询运行状态。
- 将结果规范化为 `SkillRunResult`。
- 写回 `state.lastRun`。

## 输出与产物呈现

Skill 输出必须结构化。动态 UI 的结果区只依赖统一结果协议，不关心具体 skill。

```ts
type SkillRunResult = {
  status: "success" | "failed" | "canceled";
  output?: {
    blocks?: OutputBlock[];
    data?: Record<string, unknown>;
  };
  artifacts?: Artifact[];
  error?: string;
};

type OutputBlock =
  | { type: "markdown"; id: string; title?: string; content: string }
  | { type: "text"; id: string; title?: string; content: string }
  | { type: "table"; id: string; title?: string; columns?: string[]; rows: Record<string, unknown>[] }
  | { type: "json"; id: string; title?: string; value: unknown }
  | { type: "image"; id: string; title?: string; src: string }
  | { type: "fileList"; id: string; title?: string; artifactIds: string[] };

type Artifact = {
  id: string;
  name: string;
  mimeType: string;
  size?: number;
  previewKind?: "image" | "pdf" | "markdown" | "text" | "spreadsheet" | "document" | "archive" | "binary";
};
```

文件产物通过 artifact registry 暴露，不把真实路径直接交给动态 UI。

前端只调用统一能力：

```text
previewArtifact(artifactId)
openArtifact(artifactId)
revealArtifact(artifactId)
downloadArtifact(artifactId)
```

结果区默认行为：

- `markdown/text/json/table` 直接内嵌渲染。
- `image` 内嵌预览。
- `pdf/docx/xlsx/zip/binary` 渲染文件卡片。
- 多产物渲染 artifact list。
- 失败渲染错误块和可复制诊断信息。
- 长任务先显示 progress，完成后补齐 blocks/artifacts。

## App Studio 设计器

App Studio 增加 `可视化设计` 能力。第一版不追求完整 Figma，而是做受控业务 UI 设计器。

设计器功能：

- 拖拽组件到页面。
- 拖动排序、分栏、分组。
- 绑定字段到 UI state。
- 绑定按钮 action。
- 配置输出结果区。
- 生成并保存 `maclaw.app.json`。
- 本地测试，保存测试证据。
- 生成 Skill 包。

第一版组件：

- `text`
- `input`
- `textarea`
- `select`
- `checkbox`
- `fileUpload`
- `button`
- `resultPanel`
- `markdown`
- `table`
- `artifactList`

设计器保存流程：

```text
保存草稿
  -> 校验 UI schema
  -> 校验 action binding
  -> 写入 maclaw.app.json
  -> 更新 skill.yaml maclaw_app 字段
  -> 注册本地 MaClaw App Skill
```

测试流程：

```text
点击测试
  -> 使用当前 UI state 构造输入
  -> 调用 Action Runtime
  -> 运行 Skill
  -> 记录 status、input summary、output blocks、artifacts、duration、errors
  -> 写入 governance.testEvidence
```

只有至少一次成功测试，或明确声明免测试原因，才允许上传市场。

## 技能页 MaClaw App 分类

GUI 的技能页需要新增 `MaClaw App` 分类。

识别规则：

- `skill.yaml` 存在 `maclaw_app.entry`。
- 或 skill 目录存在 `maclaw.app.json`。
- 或 skill 目录存在 `maclaw.apps.json` 且 `x_maclaw_apps == "v1"`。
- 或后端扫描结果包含 `app_count > 0`。

技能页展示：

```text
已安装 Skills
MaClaw App
自学习技能
外部技能目录
能力市场
```

MaClaw App 卡片操作：

- `打开`：在应用面板或右侧 App Runtime 打开。
- `加入应用面板`：注册 installed app。
- `编辑`：打开 App Studio，并加载该 app。
- `测试`：调用同一套 Action Runtime。
- `上传`：上传到 SkillMarket / 能力市场。
- `导出`：导出 skill zip。

技能页不是主设计器，但要成为 app 产物的注册和治理入口。

## 上传到 SkillMarket / 能力市场

上传对象是 Skill 包，不是单独 JSON。

上传包必须包含：

- `skill.yaml`
- `maclaw.app.json` 或 `maclaw.apps.json`
- README / 使用说明
- icon 或 icon 声明
- runtime 文件
- 测试证据
- 权限声明
- artifact 输出契约

上传前预检：

- `skill.yaml` 合法。
- `maclaw_app.entry` 指向真实文件。
- `maclaw.app.json` schema 合法。
- app id 符合 ASCII 标识规则。
- UI schema 可渲染。
- action binding 可解析。
- required scopes 已声明。
- 至少一次测试成功，或有免测试说明。
- artifact 输出不暴露越界路径。
- 风险等级已计算。

上传后状态：

```text
draft
  -> local_tested
  -> submitted
  -> review_failed
  -> approved
  -> published
  -> deprecated
  -> revoked
```

现有 `SubmitMaclawAppPackage` 可以继续承载 app package 本地提交队列。SkillMarket 上传应复用 `UploadNLSkillToMarket` 的 zip 管线，并在 manifest 中附带 app preview metadata。

## 安装与注册

安装 MaClaw App Skill 后：

```text
InstallMixedSkill
  -> 写入本地 skill 目录
  -> 扫描 skill.yaml / maclaw.app.json / maclaw.apps.json
  -> 注册 skill
  -> 注册 MaClaw App metadata
  -> 技能页 MaClaw App 分类刷新
  -> 可选加入应用面板
```

如果 `maclaw_app.add_to_app_panel == true`，默认加入应用面板；若企业策略要求用户确认，则显示预览后再加入。

升级时：

- 保留用户本地排序、置顶、隐藏、分类覆盖。
- 更新来源定义和 UI schema。
- 新增高风险权限必须二次确认或重新审核。
- schema 不兼容时进入 `needs_config`。

## 后端接口建议

本地 App Skill 注册：

```go
ListMaclawAppSkills() ([]MaclawAppSkillSummary, error)
GetMaclawAppSkill(skillName string) (*MaclawAppSkillDetail, error)
RegisterMaclawAppSkill(skillDir string) (*MaclawAppSkillSummary, error)
AddMaclawAppSkillToPanel(skillName string, appID string) error
```

设计器保存：

```go
SaveMaclawAppSkillDraft(input SaveMaclawAppSkillDraftInput) (*MaclawAppSkillDetail, error)
TestMaclawAppSkillDraft(skillName string, input map[string]any) (*SkillRunResult, error)
PackageMaclawAppSkill(skillName string) (*SkillPackageSummary, error)
```

市场上传：

```go
UploadMaclawAppSkillToMarket(skillName string) (*SkillMarketUploadResult, error)
```

artifact：

```go
ListAppRunArtifacts(runID string) ([]Artifact, error)
PreviewArtifact(artifactID string) (*ArtifactPreview, error)
OpenArtifact(artifactID string) error
RevealArtifact(artifactID string) error
```

## 当前实现状态

截至当前实现，MaClaw App Skill 已完成第一阶段闭环的主体链路：

- Skill 包内默认使用 `maclaw.app.json` 作为 App 描述文件。
- `SaveMaclawAppDefinitionForSkill(skillName, appJSON)` 会把设计器产物写入目标 Skill 目录，并补齐 `skill.yaml` 的 `maclaw_app.entry/status/add_to_app_panel`。
- `ListSkillAppManifests()` 支持发现 `maclaw.app.json`、旧版 `maclaw.apps.json` 和 `skill.yaml maclaw_app.entry`。
- `ListNLSkills()` 会标记 `is_maclaw_app/maclaw_app_count/maclaw_app_entry`。
- 技能页已有 `MaClaw App` 分类，普通已安装 Skill 列表会排除 App Skill。
- 应用面板会从已安装 Skill 中自动发现并注册 App 定义。
- App Studio 可以从现有非 App Skill 中选择目标 Skill，保存 `maclaw.app.json`，并上传该 Skill 到 SkillMarket；上传前会要求当前 App 定义版本已有一次成功测试记录，然后自动保存最新 App 定义。
- App 面板内的成功运行会回写到 Skill 内的 `maclaw.app.json` governance 测试证据；证据只保存 `runId/verifiedAt/definitionHash/artifactPresent/artifactName`，不把本机完整产物路径写入可发布文件。
- SkillMarket 打包会保留 `maclaw.app.json`，并在 `skill_package_manifest.json` 写入 `product_kind/is_maclaw_app/maclaw_app_count/maclaw_app_entry`、`maclaw_app_definition_sha256`、`maclaw_app_test_evidence`、`artifact_contract_required/output_modes/presentation`、`declared_permissions/declared_required_env/declared_requires_gui`，以及 `maclaw_app_id/name/description/category/icon/input_mode/output_modes` 预览元数据。
- HubCenter 验包、发布、搜索索引、搜索结果和推荐结果会保留 App Skill 身份和 `maclaw_app_test_evidence`；能力市场搜索卡片、推荐卡片和后台审核队列会展示 App 预览名、分类、图标、输出类型，并可查看输入模式、产物呈现、测试运行、描述文件哈希等 App manifest 预览信息。
- 后台审核队列会额外展示 `permissions/required_env/requires_gui/security_labels`、产物契约和测试证据，审核人员可在批准前确认 App Skill 的运行权限与产物承诺。
- 后台审核页使用 `/api/v1/admin/skillmarket/review`，可返回 `pending_review/trial` 两类待处理能力，避免公开搜索接口过滤掉待审核 App Skill。
- 从 SkillMarket 安装 App Skill 后，本地 Skill 目录会保留 `maclaw.app.json`，应用面板可再次发现。
- 当前运行结果区已能展示 `artifact_path`，并提供打开文件、显示所在目录等基础操作。
- Skill 运行状态已开始输出统一 UI 结果协议：`artifacts[]` 描述文件产物，`outputs[]/summary.output_blocks[]` 描述可渲染结果块；App 面板优先读取该协议，旧版 `summary.artifact_path` 继续兼容。
- App 面板产物操作已收口到 `OpenSkillRunArtifact(runID, artifactID)` / `RevealSkillRunArtifact(runID, artifactID)`，前端不再必须依赖裸路径执行打开/定位，后续可平滑替换为 artifact registry、权限校验或远端下载。

仍未完成或仅部分完成：

- 动态 AG UI 渲染器、拖拽布局设计器和完整属性面板仍是后续阶段。
- `SkillRunResult/OutputBlock/Artifact` 统一结果协议已接入 Skill 运行状态和 App 面板；下一步还需扩展到更多后端执行入口与市场详情页。
- artifact registry 还未替代所有真实路径透传；当前已先把打开/定位动作收口到 `runID + artifactID`，路径仍作为展示和兼容字段保留。
- 上传前已有基础权限声明随包进入 SkillMarket，并已进入后台审核队列；更细的权限升级策略、风险分级文案和二次确认仍需补齐。测试证据已从前端本地成功运行记录推进到 Skill 包内 governance 证据、`skill_package_manifest.json` 摘要、HubCenter 发布元数据、市场搜索/推荐结果和后台审核队列。
- 市场详情页的完整 App manifest 原文级预览仍需补齐；当前先在市场卡片和后台审核队列提供摘要级预览。

## 实施阶段

### Phase 1：注册与分类

- 扫描 skill 目录中的 `maclaw_app` / `maclaw.app.json` / `maclaw.apps.json`。
- 后端 `ListNLSkills` 增加 app metadata。
- 技能页新增 `MaClaw App` 分类。
- App 卡片显示 `打开`、`加入应用面板`、`编辑`、`上传`。

### Phase 2：动态运行协议

- 增加 `DynamicAppRenderer`。
- 增加 UI state、action runtime、result panel。
- 支持基础组件和 `runSkill` action。
- 统一 `SkillRunResult`、`OutputBlock`、`Artifact`。

### Phase 3：设计器

- App Studio 增加 `可视化设计` tab。
- 支持拖拽排序、组件属性编辑、绑定 state/action。
- 保存为 `maclaw.app.json`。
- 测试运行并写入 evidence。

### Phase 4：打包与上传

- 从设计器生成完整 Skill 包。
- 复用 SkillMarket 上传管线。
- 上传前做 schema、权限、测试证据、artifact 契约预检。
- 市场展示 app preview metadata。

### Phase 5：企业治理

- 企业 Hub 能力市场审核 MaClaw App Skill。
- 管理员推荐、下发、禁用、回滚。
- 客户端按 policy 控制安装来源和权限升级。

## 当前代码落点

- 应用面板和 App Studio：`gui/frontend/src/components/pages/AppsPage.tsx`
- 技能页：`gui/frontend/src/components/pages/SkillsPage.tsx`
- 技能管理面板：`gui/frontend/src/components/remote/SkillsManagementPanel.tsx`
- Skill app 扫描结构：`gui/app_nl_skills.go`
- App package 提交队列：`gui/app_maclaw_apps.go`
- 现有设计背景：`docs/app-panel-and-app-studio-design-zh.md`

## 决策

- MaClaw App 是 Skill 产物，不是孤立 localStorage 配置。
- 动态 UI 只声明 state、layout、action，不直接访问文件路径或后端实现。
- 文件产物走 artifact registry。
- 技能页必须有 `MaClaw App` 分类，承载注册、打开、编辑、测试、上传。
- 应用面板继续作为用户使用入口。
- SkillMarket / 能力市场上传对象是完整 Skill 包，app preview 只是 metadata。
