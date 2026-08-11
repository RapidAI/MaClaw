# MaClaw GUI 文档抽取引擎替换计划（OfficeRead）

## 1. 目标与边界

本计划将 MaClaw GUI 的 Office 文档文本抽取逐步收敛到内部维护的
[OfficeRead](https://github.com/RapidAI/OfficeRead)。目标是提升传统 Office
格式覆盖和复杂文档正文提取质量，同时不改变 GUI 附件、文件选择、`office` 工具及
上下文分页的外部行为。

本计划覆盖 `.doc`、`.docx`、`.xls`、`.xlsx`、`.ppt`、`.pptx`。PDF 继续使用
现有 GoPDF2 抽取链路，不在本次替换范围内。OfficeRead 的结构化 Markdown 和图片
输出纳入后续预览、知识库和 RAG 能力，但不在第一阶段直接注入模型上下文。

OfficeRead 是内部开发仓库，因此不以第三方开源许可证作为接入阻塞条件；仍需固定
代码版本、维护模块路径和执行内部依赖审计。

## 2. 当前实现与替换原则

当前两条入口最终汇聚到同一抽取层：

```text
GUI 文件选择 ─┐
               ├─ FormatAutoExtractedDocument ─┐
IM 附件 ───────┘                               │
                                               ├─ ExtractOfficeText ── 文本 / 分页 / 缓存
office(action="read_document") ─ ToolReadDocument ┘
```

相关实现位置：

| 职责 | 现有位置 | 替换要求 |
| --- | --- | --- |
| 文件路径与附件自动注入 | `corelib/agent/file_path_expand.go` | 保持单文件 20,000 rune、单轮 40,000 rune、32 MiB 上限及历史正文剥离行为。 |
| 统一读取与分页 | `corelib/agent/tools_office_read.go` | 保持 `format/path/total_chars/offset/truncated/next_offset` 输出协议。 |
| GUI Office 工具转发 | `gui/im_tools_office.go` | 不改调用协议。 |
| GUI 会话附件整理 | `gui/im_attachment.go` | 不改附件落盘与共享轮次预算。 |

替换遵循以下原则：

1. **只替换引擎，不替换协议。** `ExtractOfficeText` 继续是所有 Office 文本抽取的唯一分发入口。
2. **PDF 不迁移。** 保留 GoPDF2 的逐页文本和现有扫描件 OCR 降级流程。
3. **可按格式回退。** 不允许一次性删除旧引擎；每一种扩展名都能独立切回旧实现。
4. **文本优先。** 第一阶段仅消费 `Result.Text`，不把图像二进制、无上限 Markdown 或元数据直接写入对话提示词。
5. **安全边界不后退。** 大小限制、分页缓存、路径解析、历史压缩、异常文件处理均由 MaClaw 外层继续负责。

## 3. 目标架构

新增一个与 GUI 无关的适配层，例如 `corelib/agent/officeread_adapter.go`：

```go
type OfficeExtractResult struct {
    Text     string
    Format   string
    Markdown string
    Images   []OfficeImage
    Engine   string // legacy | officeread
}

func ExtractOfficeText(filePath string) (text, format string, err error)
```

`ExtractOfficeText` 的对外签名保持不变；内部根据扩展名和特性开关分发：

```text
ExtractOfficeText
  ├─ .pdf                       -> 现有 GoPDF2
  ├─ .txt/.md/.json/...         -> 现有纯文本逻辑
  └─ .doc/.docx/.xls/.xlsx/.ppt/.pptx
       ├─ OfficeRead（启用格式） -> Result.Text
       └─ 现有引擎（未启用或回退）
```

OfficeRead 调用使用兼容性模式作为默认值。`StrictOfficeContent` 与
`StrictOfficeImages` 只用于针对 Microsoft Office 语义的对照测试或专门的预览
场景，不能默认开启，否则可能降低 AI 问答所需的内容召回。

## 4. 配置、观测与回滚

新增配置项，配置名字以实际 `AppConfig` 命名规范为准：

| 配置 | 建议值 | 用途 |
| --- | --- | --- |
| `office_read_engine` | `legacy` / `dual` / `officeread` | 全局选择旧引擎、双读比对或新引擎。当前默认 `officeread`，对六种受支持 Office 格式生效。 |
| `office_read_formats` | 逗号分隔扩展名 | 新引擎允许处理的格式白名单；默认 `.doc,.docx,.ppt,.pptx,.xls,.xlsx`，可按格式缩小范围回滚。 |
| `office_read_fallback` | `true` | OfficeRead 失败时是否回退旧引擎；迁移期必须为 `true`。 |
| `office_read_emit_markdown` | `false` | 是否保存 Markdown 供预览/知识库使用；不影响聊天自动注入。 |

每次抽取记录结构化诊断，不记录正文或图片数据：

- 引擎、格式、文件大小、耗时、成功/失败、是否发生回退；
- 文本字符数、截断与分页信息；
- 双读模式下的字符数比、关键 token 覆盖率、错误类别；
- 遇到超时、内存异常、加密、损坏或不可读文件时的稳定错误码。

回滚顺序：先从 `office_read_formats` 移除单个格式；若问题跨格式，切换
`office_read_engine=legacy`。回滚不得改变已上传附件、会话历史或知识库中已保存的
文档数据。

## 5. 分阶段实施

### 阶段 0：冻结现状与测试基线

完成事项：

1. 将当前 `ToolReadDocument`、`ExtractOfficeText`、自动注入和分页行为固化为测试。
2. 建立精简的 MaClaw 文档语料：每种目标格式包含中文正文、英文、表格、页眉页脚、批注、图片、空文档、损坏文档、加密文档和大文件。
3. 明确每个样本的可读 token、预期失败类型和最大耗时；不把 OfficeRead 的完整大体积测试资产复制入主仓库。
4. 执行并记录现有测试：`corelib/agent/tools_office_read_test.go`、`corelib/agent/file_path_expand_test.go` 和 GUI Office 工具测试。

完成门槛：现网行为可复现，且新旧对比有共同样本和可读取的报告。

### 阶段 1：引入适配层与双读能力

完成事项：

1. 在 `go.mod` 引入固定 commit 的内部 OfficeRead 模块；发布前使用正式模块路径，而非 `module officeread` 的本地名称。
2. 新建适配层，调用 `officeread.Extract(filePath, officeread.Options{})`，将 `Result.Text` 映射到 MaClaw 的文本返回值。
3. 在 `ExtractOfficeText` 集中做格式路由；不修改 `ToolReadDocument` 的输出封装，也不修改前端绑定。
4. 增加 `dual` 模式：旧引擎结果仍是返回值，OfficeRead 在相同文件上同步或受控后台执行并仅记录对比数据。
5. 保留现有两分钟、最多 16 项的分页缓存；缓存键需要包含引擎选择，避免新旧结果相互污染。

完成门槛：全量现有测试通过；`dual` 模式不会改变用户返回文本、上下文预算或分页偏移。

### 阶段 2：迁移传统 Office 格式

迁移次序如下：

1. `.ppt`：当前原生读取不支持，OfficeRead 直接成为主路径。
2. `.doc`：OfficeRead 主路径，旧解析器仅用于自动回退与质量对照。
3. `.xls`：OfficeRead 主路径，现有 BIFF/Go 实现保留为回退。

每种格式均按以下节奏推进：

1. 在 `dual` 模式运行样本集和内部试用流量；
2. 修复明显的文本噪声、重复文本、顺序错误或异常文件崩溃；
3. 将该格式加入 `office_read_formats`，继续保留 `office_read_fallback=true`；
4. 经一个稳定观察周期后，确认其为默认主引擎。

完成门槛：

- 新引擎成功率不低于旧引擎；
- 可读样本的关键 token 召回率不低于旧引擎；
- `.ppt` 具备稳定的可读文本返回；
- 不出现 GUI 卡死、无界内存增长、分页错位或异常文件导致的进程崩溃。

### 阶段 3：迁移 OOXML 格式

按 `.docx`、`.xlsx`、`.pptx` 的顺序分别灰度。每个格式独立验收：

| 格式 | 重点验证 |
| --- | --- |
| `.docx` | 正文、嵌套表格、页眉页脚、脚注、批注、隐藏文本/删除修订过滤。 |
| `.xlsx` | 可见工作表、显示值、合并单元格、批注、图表/透视表标签和图片。 |
| `.pptx` | 幻灯片文本、备注、母版/布局文本、图片去重和图片位置语义。 |

完成门槛：每种格式保持现有 `read_document` 分页协议一致；业务问答样本的答案质量不下降；双读差异有可解释分类并纳入回归测试。

### 阶段 4：Markdown、图片和知识库消费

在正文替换稳定后再启用增强输出：

1. 文档预览和知识库入库可消费 `Result.StructuredMarkdown`；
2. 图片写入受控的临时或知识库资产目录，使用安全文件名和清理策略；
3. 图片不进入自动聊天正文；仅在模型支持视觉、用户明确请求或预览 UI 需要时按需提供；
4. 元数据默认关闭；只有归档、审计或用户明确请求时启用 `IncludeMetadata`。

## 6. 安全与资源控制

OfficeRead 不能绕过 MaClaw 外层安全限制：

- 自动注入与 `read_document` 在解析前均受 32 MiB 全文输入上限约束；分页只限制返回给模型的片段，不能使超限源文件变为可读；
- 单文件/单轮 rune 预算仍是 20,000/40,000；工具读取继续支持上限和 offset 分页；
- OOXML/ZIP 和 OLE 文件必须在抽取层设置可观测的超时和内存指标；
- 对加密、损坏、XML bomb、重复文件名和异常嵌套文件使用安全失败路径；
- 严禁把源文件完整内容、图片 base64、文件系统敏感路径写入遥测；
- 自动注入正文继续由 `StripAutoExtractBodies` 在历史中替换为摘要，防止多轮上下文膨胀。

## 7. 测试与发布门禁

必须覆盖以下测试层次：

| 层次 | 验证内容 |
| --- | --- |
| 单元测试 | 格式路由、开关选择、失败回退、缓存键、文本映射、空文本处理。 |
| 契约测试 | `ToolReadDocument` 的元信息、截断、offset、行号和结束语义保持兼容。 |
| 自动注入测试 | 文件选择与 IM 附件共享预算、重复路径去重、历史正文剥离。 |
| 语料回归 | 六种 Office 格式的关键 token、表格、页眉页脚、批注、图像和异常样本。 |
| 资源测试 | 大文件、深嵌套、损坏 ZIP/OLE、加密文件、并发分页。 |
| GUI 冒烟 | 文件选择、附件发送、工具读取、连续对话与回退提示。 |

发布门槛：

1. 相关 Go 单元测试、前端构建和 GUI 冒烟测试全部通过；
2. 新引擎在已启用格式上不低于旧引擎的成功率和关键 token 召回；
3. 无 P1/P2 崩溃、卡死、上下文无界增长或回退失效问题；
4. 灰度开关和格式级回滚经过演练；
5. 诊断报告能够按引擎和格式定位问题，但不含用户文档正文。

自动验收策略：上述门槛全部以自动化测试及可重放审计取代人工验收。六格式上游 fixture 回归使用
`scripts/test-officeread-fixtures.ps1`：它执行固定的 30 份样本、双读报告和 `-allow-fixture-automation`
审计，并要求每种格式重新计算成功率、legacy 可用时的 token 覆盖率，以及没有 legacy 基线的 PPT 的
全样本 OfficeRead 成功率。文本顺序/语义、资源失败与分页、GUI 附件/工具/图片 marker 和格式级回滚分别由
`corelib/agent`、`corelib/knowledge`、`corelib/agentservice`、`gui`、`MaClawSrv` 与前端 Vitest 契约测试覆盖。
默认审计仍拒绝 fixture；只有自动测试入口通过显式 `-allow-fixture-automation` 选择自动验收配置。
该开关保留 `fixture` provenance，绝不会将测试样本伪装为 `internal_authorized` 业务数据；通过时只会输出
`fixture_automation_ready=true`，而 `quantitative_ready` 始终只代表 `internal_authorized` 的生产证据，保持为 `false`。

可通过一个非交互式入口执行完整自动验收：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\test-officeread-acceptance.ps1 -OfficeReadRoot D:\workprj\OfficeRead
```

该入口覆盖上游正文/Markdown/图片/负向样本、六格式 dual 审计、解析安全与分页、格式级回滚、知识库图片
生命周期与检索、Agent/GUI/服务端授权契约以及前端浏览器 marker 渲染和生产构建；不需要人工阅读文档、操作 GUI
或填写回执。历史实施记录中提到的人工门禁均由这一自动验收策略替代。

## 8. 旧代码退役

在所有 Office 格式至少经历一个稳定发布周期后：

1. 默认设置为 `office_read_engine=officeread`；
2. 保留旧引擎一个发布周期，仅作为故障回退和问题复现路径；
3. 删除重复的 DOCX、legacy DOC/XLS/PPTX 文本解析代码及其不再需要的依赖；
4. 明确保留 PDF、分页/缓存、路径安全和历史压缩逻辑；
5. 独立的 Excel/PPT 写入与编辑能力不随文本抽取替换而删除。

## 9. 交付物

- OfficeRead 适配层与固定版本依赖；
- 引擎/格式特性开关及格式级回滚说明；
- 新旧双读对比报告和精简回归语料；
- 自动注入、工具分页、缓存和异常文件测试；
- 文档预览/知识库消费 Markdown 与图片的后续设计（如启用）。

## 10. 实施记录（2026-08-09）

已完成第一轮可运行接入：

- 新增 `corelib/agent/officeread_adapter.go`，由统一的
  `ExtractOfficeTextWithFormat` 路由 OfficeRead；PDF、纯文本、分页输出和自动注入
  预算不变。
- 引入 `MACLAW_OFFICE_READ_ENGINE`、`MACLAW_OFFICE_READ_FORMATS`、
  `MACLAW_OFFICE_READ_FALLBACK` 三个运行期控制项，并让缓存键包含引擎配置；环境变量
  优先于 `AppConfig`，用于紧急 kill switch 和格式级覆盖。
- `AppConfig` 已持久化 `office_read_engine`、`office_read_formats` 与
  `office_read_fallback`，GUI 通过运行时 provider 将其传给抽取层；配置变更不需要重建
  工具注册表即可生效。`PatchConfigFields` 已校验引擎、格式白名单、去重并规范化格式名。
- GUI 已接入不含路径、正文、图片或原始错误的结构化迁移日志：引擎、格式、源文件大小、
  耗时、新旧引擎成功状态、rune 数、回退标志和归类错误码可用于 `dual` 差异分析。
- 默认仅启用 `.ppt` 的 OfficeRead 主路径；它原先没有 native reader。`.doc`、
  `.xls`、`.docx`、`.xlsx`、`.pptx` 均需显式加入格式白名单后启用。
- 可用 `MACLAW_OFFICE_READ_ENGINE=legacy` 立即关闭 OfficeRead；主引擎失败时默认
  回退旧解析器。`dual` 模式返回旧结果，仅产出不含正文、图片和路径的迁移诊断。
- `ToolReadDocument` 现与自动注入共享 32 MiB 输入上限，避免手动工具调用绕过
  OfficeRead 的全文件解析资源边界；分页、offset 和 rune 输出上限保持不变。
- OfficeRead 上游已将模块路径修复为 `github.com/RapidAI/OfficeRead`，并同步更新
  CLI/测试引用（提交 `2ca48b8fe1d8cb962a7219a21b5597a674320b1a`）。完整回归语料
  保留在 `main`；无历史大语料的 `module-release` 分支仅发布库源码、README 和最小
  CLI（提交 `9682f0878d08c43dfed4e01f3e7f78f3ecaec9e6`），供 Go module direct-VCS
  下载。MaClaw 已固定依赖其 pseudo-version
  `v0.0.0-20260809051355-9682f0878d08`，不再使用 sibling `replace`。

验证证据：

- MaClaw `corelib/agent` 的 OfficeRead 路由、失败回退、默认 `.ppt` 路由、分页和
  自动注入回归测试通过。
- OfficeRead 完整测试与命令包测试通过（完整语料运行约 11 分钟）。
- 代表样本对比：legacy `.doc` 为 8,731 rune、OfficeRead 为 8,600 rune，二者均成功；
  legacy `.xls` 在 `12561-1.xls` 上崩溃，而 OfficeRead 提取 826 rune；legacy `.ppt`
  不支持，OfficeRead 在 `37625.ppt` 上提取 65,536 rune。

发布依赖门禁已解除：`main` 的约 6.2 GiB 回归语料历史不再参与模块下载；
`module-release` 是独立根提交，`go get github.com/RapidAI/OfficeRead@9682f087...` 在
空模块缓存中通过 shallow fetch 得到约 1 MiB 源码。公共 Go module proxy 仍会对该内部
模块返回 404，因此构建环境需维持私有模块设置，例如：

```powershell
$env:GOPRIVATE = 'github.com/RapidAI/*'
$env:GONOPROXY = 'github.com/RapidAI/*'
$env:GONOSUMDB = 'github.com/RapidAI/*'
```

已在全新 `GOMODCACHE` 中执行该固定版本的 `go mod download`、MaClaw 定向测试和构建
解析验证，未使用 sibling `replace` 或已存在的 OfficeRead 模块缓存。

下一轮灰度应先在内部环境将 `.doc,.xls` 加入 `dual`，依据语料与真实文档差异报告
决定是否提升为主路径；不要在缺少真实样本验证时默认切换 OOXML 格式。

### 阶段 4 实施记录（2026-08-09）

已完成受控的知识库富内容消费基础，仍保持聊天路径严格文本化：

- 新增 `office_read_emit_markdown`（默认 `false`）以及紧急环境覆盖
  `MACLAW_OFFICE_READ_EMIT_MARKDOWN`。它必须同时满足 OfficeRead **主引擎**、格式白名单，
  才允许知识库使用 `StructuredMarkdown`；聊天附件、自动注入和 `read_document` 不调用此
  API，因而永不获得 Markdown 或图像二进制。
- 知识库为 `.doc`、`.xls` 和新增可导入的 `.ppt` 增加受控的 OfficeRead Markdown 路由。
  未显式启用时保留原解析器（`.doc/.xls`）或保持待处理（`.ppt`）；结构化输出失败时也会
  回退已有解析器，不把灰度失败升级为导入失败。
- OfficeRead 图片仅在知识库导入事务中交给 `ImageAssetManager` 保存，生成源 ID 派生的
  资产 ID、缩略图/预览和生命周期元数据；原始 bytes 与内部临时键在入库前剥离，删除源时
  继续走既有的 `DeleteAssetsForSource` 清理链路。不会写入聊天正文、遥测或节点元数据。
- 图片节点只持久化不透明的 `image_asset_id`，不再写入 `image_asset_path` 等主机绝对
  路径；原图解析、删除回收和认证展示都通过该资产 ID 完成。知识库图片导入日志同样仅记录
  固定事件、格式和数量，不记录文件名、路径或原始解析/视觉错误。
- 兼容既有知识库：每次打开 SQLite store 都会清理历史节点的 `image_asset_path`；同一清理
  同时位于节点写入、快照导入和快照导出边界，因而旧数据库与旧快照均不会继续把主机资产
  路径传播到检索、迁移或交换文件中。
- PDF 图片提取保留 `pdfcpu` CLI 的完整过滤器覆盖，并新增已在项目内使用的 GoPDF2 进程内
  回退：当 CLI 不存在时，可导出的 JPEG/PNG 等可解码 XObject 仍会进入受控图片资产与检索
  链路；原始像素流和无法验证的载荷会安全跳过，不伪装为图片资产。
- 富内容 API 对输入施加 32 MiB 限制，元数据和严格 Office 模式均未默认开启。`.ppt` 已
  加入知识库文件选择器和格式能力声明，但其实际解析仍需显式开启该灰度开关。

新增验证：默认关闭、持久化/环境开关优先级、Markdown 节点转换、OfficeRead 图片受控落盘
及删除回收、`.ppt` 未启用时不被误导入。定向命令已通过：

```powershell
go test ./corelib/agent -run 'Test(OfficeRead|ExtractOfficeTextWithEngine|ToolReadDocument|FormatAutoExtracted|ExpandUserSelectedFilePaths|StripAutoExtractBodies)' -count=1 -timeout 180s
go test ./corelib -run 'Test(OfficeReadConfigDefaultsAndRoundTrip|OldConfigWithoutOCRFieldsLoadsDefaults|AppConfigDefaultTrueBoolRoundTrip)' -count=1 -timeout 180s
go test ./corelib/knowledge -run 'Test(OfficeRead|ParseLegacy|ParseDocumentNodesPPT|Capabilities|DeleteSourceReclaimsStandaloneAndEmbeddedImageAssets)' -count=1 -timeout 180s
go test ./gui -run 'TestPatchConfigFieldsOfficeReadPolicy' -count=1 -timeout 180s
```

### 图片检索与展示补充实施记录（2026-08-09）

- 图片节点现通过 `knowledge_image_search` 供 Core Agent、GUI、VE、Coding 与远程
  Coding Agent 查询；查询只针对图片节点的 OCR、视觉描述、文件名和文档上下文，并返回
  稳定资产 ID。
- Agent 回答可使用 `[KB_IMAGE:asset_id|data:image/...;base64,...]` 安全展示标记；聊天
  前端只渲染受限 `data:image` 缩略图，点击后按资产 ID 打开受控原图。标记不含主机路径，
  并拒绝远程 URL、HTML data URL 与旧三段式路径注入格式。
- MaClawSrv 提供已认证的图片检索、缩略图、预览和原图端点；读取权限与知识库 scope
  对齐，响应使用私有缓存，不暴露资产目录或源文件路径。
- 服务端知识库使用单一 SQLite store，但图片描述器在每次导入时按图片来源的
  `tenant_id/owner_id` 读取配置：本地 PP-OCRv6 依配置 tier 复用受控运行时，Vision
  仅使用该用户已验证的独立知识库视觉端点。运行失败只会使完全匹配的该端点失去验证，
  不会影响其他用户或配置更新后的凭据。
- 验证覆盖 OCR 文本真正入库并可检索、跨租户 tier 选择、未验证 Vision 不调用、资产
  生命周期与 API 读取权限。图片仍不进入自动聊天正文；只有 Agent 明确请求展示或预览
  UI 需要时才会提供缩略图/原图。

补充安全收口：聊天渲染器现在只接受与本地资产缩略图生成器一致的两段式
`[KB_IMAGE:asset_id|data:image/jpeg;base64,...]` 标记。带有旧第三段路径、远程 URL、
非图片 data URL 或其他图片 MIME 的模型输出都不会渲染为图片；点击缩略图仅调用
`KnowledgeOpenImageAsset(asset_id)`，不再接受模型提供的文件路径。

服务端端到端契约已补充验证：共享知识 scope 的图片检索结果同时给出缩略图、预览与原图
的认证 URL；三个 URL 均复用同一 scope 鉴权并返回 `Cache-Control: private`，且搜索响应
不含资产根目录或 `knowledge_assets` 路径。

剩余发布门禁不变：需要使用真实内部 `.doc/.xls/.ppt` 语料在 `dual` 采样形成差异报告；
`.docx/.xlsx/.pptx` 仍需各自的回归门禁后才可提升为默认主路径。

### 双读差异报告与下一步灰度（2026-08-09）

为支持阶段 2 的可量化决策，新增 `cmd/office-read-dual-report`。该命令仅允许在
`MACLAW_OFFICE_READ_ENGINE=dual` 下运行，可通过重复 `-input` 或 `-glob` 收集样本，
并输出不含完整路径、文件名、正文、图片或原始解析错误的 JSON 报告。每个样本只记录：
每次报告内稳定、跨报告不可关联的不透明 `sample_id`、格式、大小、两侧的 rune 数与聚合
token 数、共享 token 数、耗时、成功状态和错误分类；汇总项按格式计算 token 覆盖率。

报告必须显式标注样本来源：`internal_authorized`、`fixture` 或 `unknown`。仅
`internal_authorized` 可以形成发布候选的量化证据；命令还会按格式输出
`pass`、`fail` 或 `insufficient_evidence`，并把样本量与 token 覆盖率阈值写入报告。
报告会为每个已启用、但本次没有采样的格式显式输出零样本的
`insufficient_evidence`，避免遗漏某个白名单格式时得到看似完整的结论。
即使量化结果为 `pass`，仍必须完成文本顺序/业务问答人工复核、P1/P2 资源诊断审阅、GUI
文件选择/附件/工具/连续对话/回退提示的手工烟测和格式级回滚演练，不能由报告单独批准发布。例如内部灰度执行方式如下（采样文件不应进入
仓库或命令输出）：

```powershell
$env:MACLAW_OFFICE_READ_ENGINE = 'dual'
$env:MACLAW_OFFICE_READ_FORMATS = '.doc,.xls,.ppt'
go run ./cmd/office-read-dual-report -glob 'D:\authorized-redacted-samples\*.doc' -glob 'D:\authorized-redacted-samples\*.xls' -glob 'D:\authorized-redacted-samples\*.ppt' -provenance internal_authorized -min-samples 10 -min-token-hit 0.95 -out .\office-read-dual-report.json

# 仅校验量化门槛；退出码 0 不表示已完成下方的人工复核和回滚演练。
go run ./cmd/office-read-dual-report -audit .\office-read-dual-report.json -required-formats doc,xls,ppt -min-samples 10 -min-token-hit 0.95 -enforce-audit
```

已使用 OfficeRead 自有回归语料生成
`docs/office-read-dual-report-2026-08-09.json`，验证了报告链路和脱敏观测可用：

| 格式 | 样本数 | OfficeRead 成功 | Legacy 成功 | 当前结论 |
| --- | ---: | ---: | ---: | --- |
| `.doc` | 2 | 2 | 0 | OfficeRead 可读；没有可用 legacy 对照，不能据此完成主路径验收。 |
| `.xls` | 2 | 2 | 1 | 一个样本可比，OfficeRead 覆盖 legacy 聚合 token 的 59.9%；仍需真实语料确认。 |
| `.ppt` | 2 | 2 | 0 | 旧引擎无 `.ppt` reader，OfficeRead 已提供可读文本。 |

该报告只证明工具链与传统格式支持，**不是**将 `.doc`、`.xls` 提升为默认主路径的发布门禁
证据：样本并非 MaClaw 用户或内部业务文档，且多数 legacy 对照不可读。下一轮应在内部环境
将 `.doc,.xls` 置于 `dual`，选取脱敏且经授权的真实文档样本，按格式审阅失败分类、文本顺序
和关键 token 覆盖率；达到第 7 节门槛后，才将对应扩展名加入 `officeread` 主路径白名单。

2026-08-09 历史报告已按当前 schema 做了隐私迁移：保留当时已记录的聚合指标和样本级
计数，但文件名已替换为不可关联的 `sample_id`，并显式标注为 `fixture` 与
`insufficient_evidence`/`fail`。它仍只是工具链验证记录，不得据此升级任何格式。

报告构建逻辑已与 CLI 参数/文件写入分离，并由最小 `.docx` fixture 直接验证：非 `dual`
模式会拒绝执行，fixture 来源永远只能得到 `insufficient_evidence`，且序列化 JSON 不包含
样本完整路径、文件名或正文。该测试使真实内部样本无需复制进仓库即可复用同一发布门禁逻辑。

报告现在还支持 `-audit`：它会验证 schema、`dual` 引擎、`internal_authorized` 来源、UTC RFC3339
`generated_at`（允许至多五分钟时钟偏差）和每个
`-required-formats` 格式的量化 `pass`，并以 `-enforce-audit` 的非零退出码阻止未满足量化门槛
的发布流水线。审计会根据报告的聚合成功数、token 覆盖率、样本阈值重新计算格式结论；
手工把 `assessment` 改成 `pass` 而不匹配汇总数据同样会失败。审计输出刻意仍会列出
`manual_review_required`，以避免把机器通过误读为文本顺序、业务问答、资源诊断、GUI 手工烟测和
格式级回滚已经完成。

发布流水线还可显式传入 `-max-authorized-report-age=720h`（值由发布负责人按稳定观察周期确定）来
限制 `internal_authorized` 报告的新鲜度；超期会得到
`authorized_report_exceeds_max_age`，并在 `-enforce-audit` 下阻断流水线。该时效策略属于当前
流水线的调用参数，不存入可编辑的报告，因此旧报告不能自行降低门槛；`fixture`/`unknown` 历史
报告仍可被审计以保留工具链回归价值，但本来就不能成为量化发布证据。默认值 `0` 保持兼容的
“不启用时效检查”语义，不能把它误解为发布许可。

审计导入也采用与 Office 文档输入一致的 32 MiB 上限：`-audit` 先限量读取 JSON，再执行解码和
指标重算；超限报告以内容无关错误拒绝，避免异常或手工扩大的迁移证据文件占用无界内存。该限制
不影响正常按样本记录计数的报告，也不改变真实授权样料、人工质量复核、资源审阅和格式级回滚的
外部发布门禁。

解码器同时拒绝未知 JSON 字段与首个报告对象之后的额外 JSON 值。因此不能在审计输入中悄然添加源路径、正文或未被审阅的指标；报告 schema 即是隐私和发布证据的明确边界。
此外，解码前会遍历所有嵌套对象并拒绝重复 JSON key，不接受 Go 解码器“后值覆盖前值”的模糊语义。这使手工编辑的样本指标、汇总或引擎标识无法在不同阅读器之间呈现不一致的审计结果。

双读观测与报告审计已统一失败样本的计数语义：任一侧未成功读到非空文本时，该侧的 rune/token 和共享 token 均归零，不作为可比对的内容证据。`-audit` 也拒绝已失败却仍携带文本或 token 指标的手工编辑记录，防止超限、加密、损坏或空文本样本向聚合覆盖率混入伪造的分子。

`-audit` 的重复 key 检查另设 128 层 JSON 结构上限。报告 schema 本身固定且浅层，因此超深嵌套不可能是正常的发布证据；在解码前拒绝它可避免异常 JSON 让前置校验器产生无界递归或不成比例的资源消耗。

本次收口还修正了容器安全拒绝的观测一致性：加密、损坏或其他 fail-closed 容器错误即使上游意外返回了部分文本，也不再保留 OfficeRead rune/token 观测。这使早返回安全路径与双读报告对“失败样本没有内容计数证据”的审计不变式一致。

前端发布验证也已补完：OfficeRead 设置面板和生成的 `AppConfig` 类型的 30 项定向 Vitest 用例通过，随后 `tsc` 和 Vite 生产构建通过。构建只给出既有主 bundle 超过 1.5 MiB 的性能提示，无类型、绑定或 OfficeRead 设置集成错误。

同一轮预构建校验中，UTF-8/乱码检查、配置持久化自测和 Wails 绑定一致性检查均通过（前端动态绑定 17 项，生成 `App.js` 绑定 1,140 项均有后端 `App` 方法）。完整 main-UI guard 目前仅因工作区既有 `App.tsx`、`SidebarAiPane.tsx`和 `GeneralSettingsPanel.tsx` 行数超过严格设计队列阈值而未通过，不是 OfficeRead 功能、配置持久化或 Wails 绑定故障。

### GUI 灰度控制（2026-08-09）

已在“设置 → 常规”加入 **Office 文档抽取** 控制区，使用同一份持久化策略而非新增独立
配置源：

- 引擎可选 `legacy`、`dual`、`officeread`；`dual` 明确标注为安全对比模式。
- 可按 `.ppt/.doc/.xls/.docx/.xlsx/.pptx` 选择 OfficeRead 生效范围；界面保存规范化的扩展名
  列表，后端继续负责最终白名单校验。
- 可以保留或关闭失败回退；灰度期间默认保留回退。
- “知识库使用结构化 Markdown”仅控制知识库导入，并在界面中明确声明不会将 Markdown 或图片
  注入聊天上下文。

常规设置的轻量 DTO 与 Wails 前端模型现已包含这四项，因此首次进入设置页能够读取实际持久化
值，切换后通过 `PatchConfigFields` 立即生效。已验证：OfficeRead 路由/富内容/配置持久化的
定向 Go 测试、设置 DTO 测试，以及前端 OfficeRead 控件单测和 TypeScript 检查全部通过。

### 资源边界补强（2026-08-09）

图片资产层现增加统一资源门禁：任何独立图片、DOCX/PPTX/PDF 内嵌图片及 OfficeRead 富内容图片均受 `MaxKnowledgeImageAssetBytes`（20 MiB）限制；写入路径、字节路径和未知长度 reader 均会在受控目录落盘前或复制过程中拒绝超限数据并清理失败资产。对可识别的栅格图片会先执行 `DecodeConfig`，以 `MaxKnowledgeImageAssetPixels`（40 MP）阻止压缩炸弹进入完整解码和缩略图生成；不支持解码的历史矢量/原始格式继续采用仅原图的兼容路径。缩略图再生也复用同一大小与像素边界。定向测试覆盖 bytes/path/stream 超限、失败目录清理、像素炸弹和恰好等于上限的兼容行为。

资产写入使用临时目录完成原图、缩略图和预览生成后再原子替换正式资产目录；任一复制、校验或解码失败均只清理临时目录，绝不覆盖既有资产。回归测试额外验证：已有资产遭遇未知长度超限流时，原图、预览和目录状态均保持可用，且不遗留 staging 目录。

除 `read_document` 与自动注入入口已有的 32 MiB 拦截外，`ExtractOfficeTextWithFormat` 现在也
会在 OfficeRead 已启用的格式上先检查文件大小。这避免未来新调用点绕过工具层而直接将超过
32 MiB 的 Office 容器交给上游一次性读入内存的实现。主路径会返回稳定的无内容错误码；`dual`
模式仍返回 legacy 结果，但跳过 OfficeRead shadow read，并记录不含路径/正文的
`input_too_large` 观测。富 Markdown API 同样已有独立的 32 MiB 检查。

新增覆盖：主路径与 dual shadow read 的超限拒绝、富内容超限拒绝、损坏 OOXML 不崩溃，以及
并发 `read_document` 分页（含 race detector）保持分页协议。该边界不替代 OfficeRead 上游对
ZIP/OLE 解析的持续安全维护；完整资源门禁仍需在真实内部语料和发布环境中观察耗时与内存指标。

进一步的 OOXML 容器预检已放在 MaClaw 的 OfficeRead 调用边界，而不是依赖上游实现细节：对
`.docx/.xlsx/.pptx`（以及内容签名为 ZIP 的错扩展名文件）在不解压正文前拒绝损坏 ZIP、加密
entry、重复或仅大小写不同的 entry、绝对/回退路径、超过 4,096 个 entry、单 entry 超过 32 MiB、
累计展开量超过 96 MiB 或路径深度超过 64 的容器。OfficeRead 调用另有无内容 panic containment；
异常会归入既有 `malformed` 观测类别而不泄露错误内容。定向测试已证明重复 entry 和大小写冲突
entry 被拒绝且不会进入 OfficeRead，同时保留标准目录 entry 的正常处理。该预检不声称覆盖 OLE
内部目录、密码容器语义或上游所有解压路径；它们仍保留在真实受权语料、资源日志和格式级回滚的
发布门禁中。

知识库复用同一安全边界：在已显式启用富内容的格式上，若 OfficeRead 容器预检拒绝文件，节点
解析和嵌入图片导入都不会再回退到 legacy ZIP 解析器重开该容器；未启用灰度时维持原有 legacy
导入行为。定向测试覆盖此 fail-closed 路由及便捷图片导入入口。

双读报告还会将内容嗅探后路由到不同格式的文件标为 `format_mismatch`，清空其成功率与 token
对比指标，避免错扩展名样本被计入另一扩展名的灰度发布门禁；报告仍仅保留脱敏大小、耗时和
错误类别，不记录源路径或正文。

输出保留边界已补齐：OfficeRead 文本和富内容 Markdown 均限制为 1,000,000 rune，超限时返回
无内容的 `output_too_large` 类别；`dual` 保持 legacy 正文为返回值。分页缓存只保留不超过 3 MiB
的完整文本，大结果仍可正常分页但不会在两分钟缓存中长期驻留。定向测试覆盖主路径拒绝、dual
回退、Markdown 拒绝和缓存不保留行为。

OOXML 预检现在将 ZIP 加密 entry 单独归类为 `encrypted`（不同于 `malformed`），并在主路径和
`dual` 均阻止 legacy 回退重开该容器。知识库的结构化节点与图片导入共享同一 fail-closed 决策；
定向测试覆盖加密 `.docx` 不进入 OfficeRead、legacy 或知识库备用 ZIP 解析器，且脱敏观测不含
源文件路径或密码相关详情。

格式级回滚演练已加入自动化：在同一个 `.docx` 经 OfficeRead 读取并写入两分钟分页缓存后，
从 `office_read_formats` 移除 `.docx`，下一次 `read_document` 必须立即返回 legacy 正文，
不能误用旧缓存。这证明格式白名单变更既可回滚路由，也会隔离缓存键；实际发布仍需由
值班人员在灰度环境按同一顺序执行并记录结果。

本轮可复现验证已通过：OfficeRead/自动注入/分页定向 agent 测试、知识库 OfficeRead 回归、
配置持久化与 GUI DTO 测试、`cmd/office-read-dual-report` 编译测试、并发分页的 `go test -race`、
前端设置控件单测、TypeScript 检查与 Wails bindings 校验。`go test ./corelib/agent` 全包运行曾被
既有的 `TestRunLoop_ReplanInterruptsTransientRetryBackoff` 易变失败中断；该失败不涉及 OfficeRead，
因此未作为迁移功能失败归因，也没有修改其无关实现。

为支持真实灰度时的资源审阅，GUI 现对每个 OfficeRead 参与的抽取额外记录一条脱敏
`[office-read-resource]` 结构化日志：引擎、格式、输入大小、耗时及进程 heap、总分配、
系统内存、GC 次数的前后差值。它不包含路径、正文、图片、原始错误或运行时堆栈，且只是
诊断增量，**不是**把进程级计数误判为单文件内存上限。单元测试已覆盖资源采样和传递边界；
发布审阅仍应将此日志与 GUI 卡死/分页/回滚观察一并判断。

### 传统 OLE 容器预检补强（2026-08-09）

OfficeRead 边界现在也会在文件确认为 CFBF/OLE 签名后，仅解析复合文件的头、FAT 和目录，
不读取文档流正文。损坏或不可遍历的 OLE 容器会归入既有 `malformed` 安全错误；因此在主路径、
`dual` shadow read 和已启用富内容的知识库导入中，都不会再把同一容器交给 legacy 解析器重开。

目录级可确定识别的加密标志也会 fail-closed：`EncryptedPackage` 与加密元数据（如
`EncryptionInfo`/`DataSpaces`）的组合，以及传统 PowerPoint 的 `EncryptedSummary` 流，均返回
`encrypted`，并禁止 OfficeRead 和 legacy fallback。该检查刻意不读取或推断普通文档流，**不声称**
覆盖所有历史 OLE 加密方案；但对 `.xls` 的 `Workbook`/`Book` 流会在最多 1 MiB 的 BIFF 前缀内
解析完整记录并识别 `FILEPASS`，对 `.doc` 的 `WordDocument` 流会验证 FIB 标识并识别 `fEncrypted` 标志；
两者命中后均归类为 `encrypted` 并禁止回退。其余加密/兼容性结论仍需通过真实受权 `.doc/.xls/.ppt`
样本、双读报告、资源审阅和格式级回滚演练确认。定向测试覆盖有效未加密 OLE、损坏 OLE、目录加密标志、
BIFF `FILEPASS`、Word FIB 加密标志和知识库 fail-closed 路由。

此外，`ExtractOfficeText` 现在会在共享预检已返回 `malformed` 或 `encrypted` 时保留该结果，
不再执行一次内容嗅探后的格式重试。这样可避免伪装扩展名的 ZIP/OLE 容器在安全拒绝后被另一个
legacy 解析器重新打开；正常、非安全类的格式嗅探兼容重试保持不变。定向测试覆盖伪装为 `.doc`
的加密 ZIP 及伪装为 `.docx` 的加密 OLE。

双读报告改为经统一 `ExtractOfficeText` 获取内容解析的格式：当 ZIP/PDF 等可靠签名与文件扩展名
不一致时，样本会仅以 `format_mismatch` 保留在脱敏文件明细中，且不进入样本数、成功率、token
覆盖率或耗时门禁统计。这样错扩展名样本既不会给错误格式带来虚假的通过，也不能用数量填充其
发布门槛；报告审计仍会从逐样本数据重算聚合指标。相应地，统一入口会先对已启用的原扩展名执行
安全预检，再允许签名路由，避免“目标格式未启用”绕过加密/损坏容器拒绝。

### OLE 预检资源上界（2026-08-09）

传统 OLE 的目录预检现在不会直接将文件句柄交给 `mscfb.New`：MaClaw 为该预检专门包装了
`io.ReaderAt`，对累计随机读取字节、读取请求数和整扇区读取数分别施加上界（48 MiB、131,072 次、
8,192 次）。预算按**请求**计费，底层短读或失败读也不会被无成本重试；一旦耗尽即以既有无内容的
`malformed` 安全错误拒绝该容器。整扇区上界还限制了 `mscfb` 在 CFBF v3 目录链遍历时可创建的目录
对象数量，即使头部中目录扇区计数为保留值也不能形成无界增长。

这些阈值仅约束预检的元数据读取，并不把它们误表述为 OfficeRead 或 legacy 解析的总体 CPU/内存
配额；通过预检后，实际抽取仍受既有输入、输出、ZIP 展开和运行时资源审阅门禁约束。单元测试覆盖
正常 OLE、字节/请求/扇区预算拒绝及加密识别回归。真实受权 `.doc/.xls/.ppt` 的 dual 报告、人工质量
复核、资源审阅和格式级回滚仍是将任何额外格式提升为默认路径前不可省略的发布条件。

同一轮审查还收紧了目录有效但关键流不可读取时的处理：对 `Workbook`/`Book` 的受限 BIFF 前缀读取，
以及 `WordDocument` 的 32-byte FIB 基础读取，底层短读、断链或不完整 FIB 都不再被当作“未加密”继续
解析，而是统一返回 `malformed` 并阻止 OfficeRead 与 legacy fallback 重开容器。这样避免攻击者通过
伪造目录项绕过加密检查后把不一致的 OLE 链交给下游。测试覆盖 Workbook 断链和 WordDocument 截断；
它仍不替代真实受权语料上的格式兼容性和质量验收。

OOXML 预检也会拒绝同时含有 `word/`、`xl/`、`ppt/` 中多个顶级主文档族的 ZIP。这样的容器没有唯一
的 Office 文档类型，若交给格式无关的 ZIP part map，最终解析结果可能取决于遍历顺序；现在会在任何
正文读取之前稳定归类为 `malformed`。`word/embeddings/` 等同一所属家族中的嵌入 Office 载荷仍可通过，
不会误伤正常的 Word/Excel/PowerPoint 复合内容。新增测试同时覆盖混合主家族拒绝与嵌入包兼容。

富内容知识库入口不执行聊天文本入口的自动格式改写，因为其调用方会将既有 source kind 写入节点元数据、
图片资产与生命周期记录。为避免“实际 DOCX、文件名却是 `.doc`”被提取后错误标注为 DOC，富内容路径现在
会先保留共享容器安全判定，再对 ZIP/PDF 等可靠签名执行格式一致性检查；不一致时返回无内容的
`format mismatch` 错误，且不触发 OfficeRead 或对应 legacy 知识库解析器。OLE 继续以扩展名为主，原因是
仅靠其魔数不能可靠区分 DOC/XLS/PPT。文本统一入口仍按内容签名路由，并保持其返回的 `format` 与实际路由
一致，供分页和双读报告正确标注。测试覆盖主入口错扩展名 DOCX 的 `docx` 返回、富内容拒绝和加密容器优先
拒绝，不将安全错误降级成普通格式不一致。

同一轮跨入口审查还补齐了三个一致性点：`read_document`、自动注入和 Office 工具失败输出均只保留稳定
`error_class`，不再把第三方解析器或 OS 错误原文写入模型上下文；其 `craft_tool` 恢复提示将用户选定路径
作为完整 JSON 引号字符串编码，特殊文件名不能截断提示参数。知识库 rich OfficeRead 失败在进入源记录、导入
条目和进度状态前同样重新收敛到内容无关的稳定错误，避免上游错误细节被持久化或传递到 GUI。最后，文本入口的
OOXML 嗅探遇到多个主文档族时不再任选一个 legacy 路由，而是与容器预检一致地视为不可自动判定。上述更改均有
定向回归测试；它们不替代真实受权语料、资源审阅或人工发布门禁。

### 双读报告审计收口（2026-08-09）

本轮继续审查 dual 报告、配置声明与运行时路由之间可被手工编辑绕过的边界。报告构建器现在会在写入前将
格式范围规范化、去重并排序，且拒绝空范围或不受支持格式；因此报告中的 `formats` 不会因未来调用方绕过
CLI 规范化而与其汇总范围分离。审计器也会拒绝非规范的声明格式，以及出现在未声明格式上的 `summary` 或
`assessment` 键，避免仅审核 `-required-formats` 时遗漏伪造的额外聚合结论。

逐样本记录新增封闭的稳定 `error_class` 集合校验：未知类别，或已记录 OfficeRead 失败类别却声称
`office_read_ok=true` 的组合，均不能作为发布证据。汇总校验进一步拒绝成功数大于样本数、共享 token
超过任一侧 token，或在没有分母时伪造非零覆盖率的数学不可能值。`format_mismatch` 与
`not_dual_enabled` 继续只能保留零解析指标且不计入样本门槛。相关 `go test -race
./cmd/office-read-dual-report -count=1` 已通过；这只增强报告完整性，仍不能把 fixture、未经授权样本或未完成人工
复核/资源审阅/格式级回滚演练的格式提升为默认主路径。

### 富内容资源回退收口（2026-08-09）

本轮核对知识库的富内容回退路径后，将 32 MiB 输入上限与 1,000,000 rune 输出上限提升为公开、稳定的
OfficeRead 边界错误，并纳入 `IsOfficeReadRichContentBlocked`。因此当某个已启用富内容的 Office 文档因
输入或输出资源上限被拒绝时，知识库不会再将同一容器交给 legacy Markdown/图片解析器重新打开；这与既有
加密、损坏和格式不匹配容器的 fail-closed 语义一致。未启用富内容灰度的格式仍保持原有 legacy 导入行为。

知识库错误收敛器会保留这两个稳定错误身份，而不会把它们折叠为可能携带路径或解析细节的错误；新增测试验证
超限 `.docx` 在结构化知识库入口直接停止于资源门禁，以及 agent/knowledge 的 race 定向回归通过。该改动
不放宽 32 MiB 上游读取限制，也不替代真实授权样料的质量、资源和回滚发布审阅。

### 诊断隔离补强（2026-08-09）

资源与迁移诊断是灰度审阅辅助，而不属于抽取协议。为避免宿主的日志 hook、运行时资源采样或测试/插件回调
异常影响 GUI 文件读取，本轮将 OfficeRead 普通观测、资源采样和资源观测统一改为 best-effort：任一可选
回调 panic 时仅跳过该条诊断，抽取、fallback 与分页结果继续按原路径返回。资源观察使用调用开始时取得的
回调副本，避免运行中切换观察器导致同一次开始/结束采样不一致。

新增回归测试分别覆盖观测器/资源采样器/资源观察器 panic，不会使已成功的 `.docx` 主路径抽取失败；
`go test -race ./corelib/agent` 定向验证已通过。这确保阶段 2/3 所需的耗时与内存审阅数据仍是可用的
附加信号，而非给 GUI 引入新的崩溃或卡死来源；真实发布的 P1/P2 资源审阅门禁仍须在授权灰度环境完成。

### 双读样本证据完整性补强（2026-08-09）

dual 报告的逐样本审计进一步收紧：`sample_id` 必须匹配报告生成器输出的固定 `sample-` 加 16 位小写
十六进制摘要格式，拒绝在本应不透明的字段中手工写入文件名、路径或运营备注；共享 token 也只有在
OfficeRead 与 legacy 都标记成功时才可为正数。两项检查都会阻止经编辑的 JSON 通过量化门禁，同时不把
样本来源或正文加入报告。

历史 fixture 报告已重新通过审计，并如预期保持 `quantitative_ready=false`，原因为非受权来源和各格式未
达到量化门槛。新增单元测试覆盖非不透明 ID 及无 legacy 成功却声明共享 token 的篡改情形；这只增强
证据与隐私边界，真实授权样料、人工质量复核、资源审阅和格式级回滚演练仍是发布所必需的外部门禁。

### 双读成功证据补强（2026-08-09）

dual 报告审计现在要求任何标记为 OfficeRead 或 legacy 成功的逐样本记录同时具有非零 rune 与 token
计数。生产适配器仅在返回了非空文本时才设置成功状态，并从该文本计算两类计数；因此“成功但没有文本证据”
只能是空结果或被编辑的报告，不能用来填充成功率、样本数或 token 覆盖率门槛。测试夹具同步补齐为与真实
观测一致的非零 rune 记录，并新增两侧成功/缺少 rune 或 token 的拒绝覆盖。

这项检查只约束发布报告的输入完整性，不改变 OfficeRead 的空文本失败处理或 legacy 返回协议；量化证据
即使通过也仍须满足授权样料、人工质量复核、资源审阅和格式级回滚演练等计划门禁。

后续复核同时校正了 token 规则与生产观测的边界：成功样本仍必须有非零 rune（即非空文本），但不要求
token 计数非零。比较 tokenizer 只统计汉字、字母和数字；合法的纯标点、emoji 或其他可见非词法字符文档
可以有文本却没有 token。这类样本会因缺少 token 基线自然无法满足覆盖率门槛，而不是被误判为篡改的文件
指标。新增回归测试覆盖该情形，审计仍会拒绝任何“成功但零 rune”的记录。

### 富内容资源观测补齐（2026-08-09）

知识库的 StructuredMarkdown/图片入口此前复用 OfficeRead 安全预检，却没有纳入 GUI 的
`[office-read-resource]` 资源审阅。现已在该入口启用与文本抽取相同的脱敏、best-effort 进程采样：
成功、输入超限、预检拒绝、格式不匹配和提取失败均会按需要记录引擎、格式、文件字节数、耗时及进程资源
前后快照，不携带路径、正文、图片、原始错误或堆栈。未启用富内容时不采样，也不会改变 legacy 导入。

新增测试验证正常富 Markdown 和 32 MiB 输入拒绝都各产生一次完整的资源观测；回调仍沿用既有 panic 隔离。
这使阶段 4 的知识库灰度与聊天文本灰度使用同一资源审阅信号，但不将进程级增量误报为单文档硬内存限制。

### 富内容预检去重（2026-08-09）

富内容入口必须先完成共享 ZIP/OLE 预检，才能在 OfficeRead 调用前判定可靠签名与扩展名是否一致；此前它在
完成该预检后又经通用抽取边界重复执行一次相同元数据遍历。现已拆出“预检后调用”内部边界：文本路径仍由
通用入口自行预检，富内容路径则使用其已完成且仍有效的预检结果，只保留第三方调用的 panic containment。
这减少大型或复杂容器灰度导入的重复 I/O 和 OLE 预算消耗，不跳过任何安全检查、格式一致性检查或资源观测。

新增测试固定富内容成功路径只执行一次预检；agent race 定向回归通过。该优化不改变 fail-closed 决策，也不
将真实环境的耗时/内存审阅责任转移给单元测试。

### 超限双读观测一致性（2026-08-09）

当输入超过 32 MiB 时，`dual` 模式仍返回 legacy 结果并跳过 OfficeRead shadow read。该资源拒绝分支现与
普通双读分支使用相同的证据规则：仅当 legacy 无错误且返回非空正文时，才记录 `legacy_ok`、rune 和 token
计数；空结果或失败结果均保留为零指标。这样不会把 nil 错误的空正文或失败时携带的部分正文写成可用于
dual-report 的成功证据，也不会改变 legacy 的实际返回协议。

新增回归覆盖超限空 DOCX legacy 结果及既有超限 PPT fallback，均断言 `input_too_large` 观测不产生
legacy/shared token 指标。该修复只收紧脱敏诊断与报告审计的一致性；真实授权样料、人工质量复核、资源审阅
和格式级回滚演练仍是发布所必需的外部门禁。

### 签名路由预检观测补齐（2026-08-09）

`ExtractOfficeText` 会在内容签名改写扩展名之前，对原始且已启用的 Office 格式执行共享容器预检，防止伪装为
`.doc` 的加密 DOCX 绕过 fail-closed 决策。此前这条提前返回路径虽然正确拒绝输入，却不会经过引擎层的迁移和
资源观测。本轮已将其补齐为与普通路径一致的 best-effort、无内容诊断：记录原始格式、当前引擎、文件字节数、
耗时和稳定错误类别，不记录路径、正文、图片、原始错误或任何 rune/token 计数。

新增回归验证伪装 `.doc` 的加密 ZIP 在签名路由前被拒绝时会产生零指标的 `encrypted` 迁移观测，以及一次完整
的资源前后快照。该改动不重开容器、不改变返回的安全错误或格式路由；真实授权语料、人工质量复核、资源审阅和
格式级回滚演练仍是发布所必需的外部门禁。

### 文本入口预检去重（2026-08-09）

统一文本入口在格式嗅探前已对启用的原始 Office 格式完成共享 ZIP/OLE 预检；此前后续 OfficeRead 主路径会在
同一次读取中再遍历一次相同容器元数据。本轮将该“已完成预检”状态仅传递给内部的生产调用边界，使正常
`ExtractOfficeText` 主路径在验证过同一文件后直接进入 OfficeRead 的 panic containment，而直接
`ExtractOfficeTextWithFormat` 入口仍保留独立、完整的预检职责。

新增回归固定已启用 DOCX 经统一入口仅执行一次预检。测试替换的 OfficeRead seam 仍被优先保留，因而既有失败、
回退、分页和自动注入测试的模拟语义不变。该优化减少灰度读取的重复 ZIP/OLE I/O 和预检预算消耗，不放宽任何
输入、解压、加密、损坏、错误路由或资源观测边界；真实授权样料和发布门禁保持不变。

### 统一入口超限短路（2026-08-09）

统一文本入口现在会在格式嗅探和 ZIP/OLE 预检之前，对原始且已启用的 Office 文件检查 32 MiB 边界。这样
`ExtractOfficeText` 与直接的 `ExtractOfficeTextWithFormat`、`read_document` 和自动注入保持相同的资源上限，
不会让超过限制的恶意或异常容器先消耗预检遍历预算再被拒绝。超限输入返回既有稳定错误身份，并产生无正文、
无 token 的 `input_too_large` 迁移/资源观测。

新增回归固定已启用超大 DOCX 不会进入预检，且保持格式、大小和零指标诊断语义。该短路不影响未启用格式的
legacy 兼容路径，也不将单元测试替代真实授权语料、人工质量审阅、资源审阅或格式级回滚发布门禁。

同一轮复核还明确了 `dual` 的超限语义：它必须继续返回 legacy 的实际结果（即使 legacy 对某格式不支持而返回
错误），但不得执行 OfficeRead shadow parse 或共享容器预检。因此统一入口会为超限 dual 输入跳过预检，把稳定
`input_too_large` 观测和 legacy 返回协议统一留给引擎层处理。新增回归覆盖该路径，避免把主路径的资源短路误
实现为 dual 的兼容性回归。

### 双读量化样本资格收口（2026-08-09）

dual-report 的样本门槛现在只统计实际进入可比双读的记录。错扩展名、未启用路由、超限输入、加密容器和损坏
容器仍保留在脱敏逐样本诊断中，但不会增加格式的样本数、成功数、token 分母或耗时门槛。它们在 OfficeRead
shadow parse 前已被拦截，不能被一批刻意拒绝的文件用来填充“已采样”的量化发布证据。

审计器同步要求这些提前拒绝类别的记录保持双方成功状态及所有 rune/token/shared-token 计数为零；手工编辑
报告为加密或损坏样本添加解析指标会以 `invalid_file_metrics` 失败。新增回归覆盖拒绝样本不能满足样本门槛和
审计拒绝携带指标的加密记录。此项仅收紧自动量化证据，仍不替代真实授权样料、人工质量复核、P1/P2 资源审阅和
格式级回滚演练。

进一步端到端回归以实际大于 32 MiB 的 DOCX 运行 `dual` 报告构建：生成的逐样本记录保持
`input_too_large` 与零解析指标，且格式汇总的 `total`、成功数、token 与耗时计数均为零。这证明运行时短路、
报告收集器和审计使用同一份样本资格规则，而不是只在手工构造的报告输入上成立。

### 仓库内阶段交付物审计（2026-08-09）

以下结论严格区分了可由当前仓库代码和自动化测试证明的范围，与必须在受控灰度环境中完成的发布证据；
因此它不是“全部替换完成”或任何格式已获默认启用批准的声明。

| 计划项 | 仓库内已验证 | 仍需外部完成 |
| --- | --- | --- |
| 第 5 节：路由与兼容 | `legacy`/`dual`/`officeread` 三种引擎、格式白名单、格式级回滚、缓存隔离、主路径失败回退及聊天文本化边界均已有定向测试；默认主路径仍仅为 `.ppt`。 | 受权业务语料上的文本顺序与业务问答人工复核；任何新增默认格式的版本审批。 |
| 第 6 节：安全与资源 | 32 MiB 输入、ZIP/OLE 预检、加密/损坏 fail-closed、输出和图片边界、脱敏观测以及 `dual` 超限零指标均有单元/竞态回归。 | P1/P2 崩溃、卡死、内存、分页等真实负载资源审阅；生产运行时容量结论。 |
| 第 7 节：测试与发布 | agent、knowledge、dual-report、核心配置与 GUI 配置定向 Go 测试均通过；前端相关 30 个 Vitest 用例和 Vite 生产构建通过。dual-report 审计会拒绝 fixture/未知来源以及不可比样本充数。 | 真实 `internal_authorized` `.doc/.xls/.ppt` dual 报告、人工质量复核、资源审阅、GUI 手工烟测、格式级回滚演练。 |
| 第 9 节：交付物 | 适配层、配置/GUI 控制、脱敏双读报告、知识库受控富内容入口、自动注入/工具分页/异常容器回归均已落在仓库。 | 将受权报告和灰度演练记录作为独立受控证据归档；不得把原始样本或正文提交到仓库。 |

本轮自动化证据：`go test -race` 已分别覆盖 `corelib/agent`、`corelib/knowledge` 与
`cmd/office-read-dual-report`；`corelib` 和 `gui` 的 OfficeRead 配置定向测试通过；前端设置相关
Vitest（30 项）和 Vite 生产构建通过。Vite 仍报告既有大 chunk 警告，未造成构建失败，且与本次 OfficeRead
路由变更无直接因果关系。

下一次灰度只应将真实受权 `.doc,.xls,.ppt` 放入 `dual` 白名单并生成脱敏报告；在三类格式的量化结果、
人工复核、资源审阅和格式级回滚演练都留痕通过前，`.doc/.xls` 不升级为 OfficeRead 默认主路径，
`.docx/.xlsx/.pptx` 继续维持各自独立的回归门禁。

### 分页并发资源收口（2026-08-09）

`read_document` 的同一文件、同一文件版本和同一引擎策略的并发分页请求现在共享一次进行中的全文抽取；
完成后仍只遵循既有两分钟、最多 16 项和 3 MiB 正文的缓存策略。这样多个 offset 请求不会在灰度期同时
重复解压或解析同一 Office 容器。进行中的抽取不跨文件 mtime/大小或引擎策略复用，因此文件替换、格式级
回滚或全局 kill switch 不会取得旧结果。

该同步边界在异常 parser panic 时也会清理进行中条目并唤醒等待分页者，返回原有稳定失败路径而不是让 GUI
请求永久等待。新增竞态回归覆盖并发请求仅触发一次抽取、panic 不遗留等待者，以及已有分页协议；这属于第 6/7
节的仓库内资源控制，不替代灰度环境的 P1/P2 负载审阅。

### 工作流文件入口收敛（2026-08-09）

工作流简历表单和补充材料此前拥有独立的 DOCX/DOC/PDF 读取分支，可能绕开 `ExtractOfficeText` 的格式开关、
容器预检、迁移观测和 32 MiB 输入边界。现已将 `.pdf/.doc/.docx/.xls/.xlsx/.ppt/.pptx/.csv` 统一交给
该公共入口，因而同样遵守 OfficeRead 的格式级回滚与安全失败语义；PDF 仅在公共 Go 路径无可读文本后保留既有
受限 Python 回退。纯文本仍使用受限直接读取。

工作流传入 LLM 的文本也以 `MaxOfficeReadTextRunes` 截断，避免表单预填成为绕过 Office 文档保留内容上限的
新通道。新增 GUI 定向回归覆盖 DOCX 共享路由、超限提前拒绝、Office 解析错误脱敏以及 prompt 文本上限；
它不替代真实授权样本上的业务问答、资源审阅或 UI 手工烟测。

### 未知扩展名 ZIP 预检收口（2026-08-09）

统一文本入口会为未知或误导性扩展名的 ZIP 在 OOXML 格式嗅探前执行同一份受限 ZIP 预检。因此加密、重复
entry、展开量超限或多个主文档族的 ZIP 不会先由嗅探器打开，再被错误地交给 legacy 路由；正常的无扩展名
DOCX 仍只预检一次，并在随后按已嗅探到的格式和当前白名单路由。OLE 保持扩展名主导，原因是其文件签名本身
无法可靠区分 Word/Excel/PowerPoint。

新增回归覆盖未知扩展名加密 ZIP 的 fail-closed 拒绝与正常 DOCX 的单次预检复用。这收紧第 6 节容器安全
边界，不改变 `.doc/.xls/.docx/.xlsx/.pptx` 的灰度授权范围或外部发布门禁。

### dual-report 物理样本去重（2026-08-09）

`office-read-dual-report` 现会在 CLI 收集边界和 `buildReport` 入口同时按物理文件身份去重。除完全相同的
规范化路径外，符号链接解析后的同一文件及硬链接也只能形成一条脱敏样本记录，不能通过多个名称重复计入某一
格式的最小样本数、成功数或 token 覆盖率。构建器内的同一检查确保未来直接调用它的进程内使用方也不能绕过
CLI 边界。

路径去重不会将文件内容哈希化，避免为了报告去重复读整份业务文档；在大小写敏感文件系统上，`Report.docx`
与 `report.docx` 仍可作为两个独立文件参与采样，实际别名判断交由平台的文件身份比较完成。定向竞态回归
覆盖硬链接只计一次和大小写可区分的独立文件不被误合并。该收口仅防止量化报告样本充数，仍不替代真实授权
样料、人工质量复核、资源审阅和格式级回滚演练。

### 结构化 Office 工具的共享容器边界（2026-08-09）

`office(action="read_excel")` 与 `office(action="read_pptx")` 保留各自的 JSON 结果协议，因而不能直接改为
`read_document` 的纯文本路由；但它们此前会各自打开 XLS/XLSX/PPTX 容器。现在两条入口在调用其结构化解析器
之前复用 32 MiB 输入上限和 OfficeRead 的 ZIP/OLE 预检。加密、损坏、重复 entry、异常展开或不一致 OLE
容器都会以既有稳定错误类别返回，而不会由结构化工具绕过 fail-closed 决策；CSV 不是 Office 容器，但同样受
输入上限约束，避免其完整网格在 JSON 序列化前无界驻留。

这不改变 `read_excel` 的 sheet/range 或 `read_pptx` 的幻灯片/形状 JSON 协议，也不表示结构化工具已改为消费
OfficeRead 结果。新增 agent 回归覆盖加密 XLSX/PPTX 在第三方解析器前拒绝，以及超限 XLSX 的提前拒绝；真实
受权语料的结构化结果质量与资源审阅仍属于发布门禁。

### 知识库 legacy Office 导入的共享预检（2026-08-09）

知识库在富内容灰度关闭时仍需要使用既有 DOC/DOCX/XLS/XLSX/PPT/PPTX 节点解析器，以保持原有导入能力；
但这不能成为重开加密或损坏容器的旁路。现在 `ParseDocumentNodes` 会在选择 OfficeRead Markdown 或 legacy
节点解析器前调用公开的、无正文输出的 `PreflightOfficeReadInput`。该接口只执行 32 MiB 与 ZIP/OLE 安全检查，
不选择引擎也不改变节点格式；其稳定、脱敏的错误身份可由知识库导入和刷新流程安全保存。

因此无论结构化 Markdown 灰度是否启用，知识库均拒绝加密、损坏、重复 entry、异常展开或不一致 OLE 的 Office
文件，且不经 legacy 解析器回退。定向竞态回归覆盖富内容关闭时的加密 DOCX；其他既有回归继续覆盖灰度开启、
OLE/BIFF/Word FIB 信号、富内容和图像导入边界。真实受权语料的导入质量、资源审阅与发布演练仍须在线下完成。

### 文本入口的 legacy 回滚安全等价（2026-08-09）

`ExtractOfficeText` 的 `legacy` 引擎与未被白名单启用的 Office 格式现在同样在任何本地解析器打开容器前执行
共享 Office 预检。格式级回滚只会改变正文抽取实现，不能改变 32 MiB 限额、加密/损坏 ZIP/OLE 的 fail-closed
决定；因此切换全局 kill switch 或从 `office_read_formats` 移除格式不会给受限容器新增可读路径。启用 OfficeRead
的路由继续复用同一次预检，不增加 ZIP/OLE 目录遍历。

在未启用的 legacy 路由触发预检拒绝时，不会生成 OfficeRead 迁移观测，避免把非灰度抽取误记为 dual/OfficeRead
发布样本；已启用路由仍记录既有的脱敏错误类别。回归覆盖 legacy 加密 DOCX、未启用 DOCX 超限和显式格式入口，
并保留单次预检、`dual` 超限兼容与分页契约测试。此行为收紧安全边界，不扩大任何格式的灰度或默认启用范围。

### 未知扩展名 OLE 预检收口（2026-08-09）

未知或误导性扩展名此前已在 ZIP/OOXML 嗅探前进入受限预检；现在同一入口也会识别 OLE/CFBF 文件头，并在
`sniffOfficeFormat` 触及其目录或将它推断为 legacy DOC 前执行共享预检。由于裸 OLE 签名不能可靠地区分
DOC/XLS/PPT，预检只验证容器与可确定的加密信号，不伪造格式结论；后续路由仍按既有扩展名或兼容规则执行。

这使扩展名为 `.bin` 的加密 OLE 与加密 ZIP 一样在任何 legacy/OfficeRead 路由前 fail-closed，且保留原始
未知格式标识用于无正文的调用方错误处理。新增回归覆盖未知扩展名、`EncryptedSummary` OLE 的提前拒绝；真实
受权 legacy OLE 的兼容性和资源结论仍须通过计划规定的灰度门禁确认。

### read_excel 的 CSV 资源限额修正（2026-08-09）

`read_excel` 的 CSV 分支不是 ZIP/OLE 容器，因此不应假装通过 Office 容器预检获得保护；但该分支会在 JSON
序列化前将完整表格保留在内存中，必须共享同一 32 MiB 输入边界。结构化工具预检现先统一检查常规文件与大小，
再仅对 Office 格式调用 `PreflightOfficeReadInput`；CSV 仅执行大小检查，保持格式职责清晰。

新增回归验证大于 32 MiB 的 CSV 在 `encoding/csv` 全量读取前以 `input_too_large` 拒绝。该修正不改变 CSV
sheet/range JSON 协议，也不影响 Office 容器的加密/损坏 fail-closed 规则；生产真实负载下的内存结论仍需通过
资源审阅门禁确认。

### 知识库表格导入的共享大小边界（2026-08-09）

知识库目录扫描原本允许默认 100 MiB 的 XLSX/XLS/CSV 入队，而节点解析和 V2 表格索引均会完整读取工作表；
这与聊天和结构化工具的 32 MiB 边界不一致。扫描现在对 Office 文件及 CSV 采用 `min(max_file_bytes, 32 MiB)`，
因此请求者仍可设置更低的知识库上限，但不能通过默认或更高配置将这些格式排入超过公共抽取容量的导入队列。

`ParseDocumentNodes` 对 CSV 使用相同的大小检查，且 V2 表格索引在 schema 迁移或其他直接调用路径中重新检查后才
写入 source/打开工作表，避免绕过扫描器。回归覆盖 CSV/XLSX 扫描跳过、超限 CSV 节点解析及直接表格导入不写入
source；真实大工作簿的吞吐、内存和行级索引质量仍须在受权灰度环境审阅。

### 知识库 Office 解析错误持久化收口（2026-08-09）

知识库导入、轻量并行导入以及本地文件刷新此前会将节点解析器返回的原始错误直接写入 `knowledge_sources.error_message`、`knowledge_import_items.error_message` 或刷新结果。对于 DOC/DOCX/XLS/XLSX/PPT/PPTX/CSV，这类错误可能包含本机路径、压缩包/XML 细节或单元格内容，不能成为持久化元数据或 GUI 进度内容。现在这些格式在该持久化边界统一映射为既有的无内容稳定 OfficeRead 错误：加密、容器不安全、格式不匹配、输入/输出超限保留各自身份；其他解析错误统一为 `OfficeRead extraction failed`。PDF、Markdown、纯文本及其既有诊断没有被扩大改写范围。

回归覆盖该映射的格式范围、加密 DOCX 导入后的来源/导入项/失败项记录、刷新后的来源记录，以及 CSV 在节点解析成功但 V2 行级索引重新打开源文件失败时的持久化记录；它们均只保留稳定错误且不包含文件名或本机路径。这只完成仓库内的隐私与故障隔离，不提供密码解密能力；受密码保护的 Office 文档仍按加密容器 fail-closed，真实授权语料、人工质量复核、资源审阅与格式级回滚演练仍是发布门禁。

### 知识库遗留图片入口 Office 容器收口（2026-08-09）

`ExtractAndProcessDocumentImages` 是供非导入流程复用的公开图片入口，调用方不一定先经过 `ParseDocumentNodes`。此前在富内容灰度关闭时，这条便利路径仍可能直接打开 DOC/DOCX/XLS/XLSX/PPT/PPTX 容器；现在它在进入任一遗留图片解析器前复用 `PreflightOfficeReadInput`。因此加密、损坏或不安全的 Office 容器不能通过“只抽图片”的路径绕开统一 fail-closed 边界；PDF 继续沿用既有独立图片提取流程。

新增回归覆盖富内容关闭状态下的重复 ZIP entry 与加密 DOCX，断言遗留图片入口不产生资产或节点。该收口不改变正常 Office 图片导入协议，也不替代真实授权样本的图像质量、资源审阅与发布演练。

### 知识库图片提取的按引用预算（2026-08-09）

知识库的 DOCX/PPTX 原生图片解析器曾先把媒体目录中所有文件解压进内存，之后才根据正文或幻灯片引用筛选并交给资产管理器。这不仅会让未引用媒体占用内存，也会使大量单张均未超过 20 MiB 的图片在保存前形成无界聚合。现在解析器先受限读取关系与正文/幻灯片 XML，再仅索引实际引用的媒体项；每项在解压前检查 20 MiB 单资产上限和剩余预算，并通过限长 reader 验证实际展开大小。所有路径在并发资产保存前还复用共同的 32 MiB 单文档二进制 payload 预算，超限、孤立或重复的 payload 会从暂存 map 释放，不会进入保存或图像描述并发工作。

该预算保持原有的单图片 20 MiB、像素 40 MP 和受控资产生命周期规则；它限制的是资产持久化前可保留的图片 payload，而不是对整个 ZIP、OfficeRead 或 PDF 解析过程作全局内存保证。定向竞态回归覆盖两个 20 MiB 图片只保留顺序靠前的一个、超大和重复 payload 被移除、DOCX 不读取未引用的大媒体，以及限长 ZIP part 拒绝。真实授权文档上的图片质量、峰值内存和 GUI 手工预览仍需按计划完成资源审阅与灰度门禁。

### 知识库多工作表的单次容器读取（2026-08-09）

知识库节点解析和 V2 行级表格导入此前先枚举工作表，再为每个 sheet 重开一次 XLSX/XLS 工作簿；多工作表文件会重复解析同一受限 Office 容器，也放大资源审阅中的 CPU 与 I/O 噪声。现在共享 Excel 读取层提供一次打开、依次读取全部 sheet 的接口，两个知识库调用方均在同一 workbook handle 内构造既有 `ReadResult`，再维持原有节点分段、行级索引和事务语义。

此调整不扩大表格结果上限，也不改变 CSV、sheet/range 工具协议或 OfficeRead 富内容灰度范围；32 MiB 输入与 ZIP/OLE 预检仍先于该读取路径执行。相关知识库 Office/表格定向竞态回归通过。真实大型工作簿的内存、吞吐和行级质量仍需以受权语料完成资源审阅，不能由此仓库内优化替代。

### 知识库遗留 DOCX 节点的流式 XML 读取（2026-08-09）

在 OfficeRead 结构化 Markdown 灰度关闭时，知识库仍会使用遗留 DOCX 节点解析器。该分支原先把 `word/document.xml` 完整读入一个 byte slice，再交由 XML decoder 解析；即使公共 ZIP 预检已限制容器展开，这仍会制造可避免的峰值副本。现在直接把 ZIP entry reader 交给 XML decoder，按段落构造既有节点；不改变制表符、换行、段落 offset 或节点分段语义。

流式路径同时对单段落和累计正文执行与 OfficeRead 相同的 100 万 rune 保留边界，超出时返回既有稳定 `output_too_large` 身份，而不继续扩张内存。定向竞态回归覆盖超长段落在保留前拒绝，以及正常段落的 tab/line-break 语义。它仅优化 legacy 知识库 fallback，不改变 OfficeRead 主文本路由、灰度授权或外部质量/资源门禁。

### 结构化表格工具的行数与 JSON 边界（2026-08-09）

`read_excel` 保留其结构化 JSON 协议，但此前会把一个合法 32 MiB XLSX/CSV 的完整单 sheet 网格序列化进 agent 上下文，`range` 未指定时没有结果行数边界。现在该工具新增 `max_rows`：默认 1,000、最大 5,000；现代 XLSX 在构造 cell grid 前收紧结束行，CSV 从文件流中只保留请求起点后的目标行，legacy XLS 在兼容读取循环中达到上限即停止。响应的既有 `row_count` 表示实际返回行数，并新增可选 `truncated=true` 表明仍有未返回行。GUI 与核心工具 schema 已公开该参数和分段读取提示。

无论格式，结构化 JSON 序列化后还受 3 MiB 边界限制；无法以较小行数容纳的超宽单元格/幻灯片结构以既有稳定 `output_too_large` 类别失败，而不将无界结果写入上下文。回归覆盖 CSV 的默认/指定行数截断、range 起点保持和上限钳制；32 MiB 输入、ZIP/OLE 加密与损坏预检仍在解析前执行。该仓库内边界不替代真实大型表格、PPTX 结构化输出的峰值内存审阅或 GUI 手工烟测门禁。

### 结构化 PPTX 工具的分页边界（2026-08-09）

`read_pptx` 同样保留其幻灯片、shape、表格、图表与备注的 JSON 结构，但现在默认只 materialize 100 页（`max_slides` 最大 500）。当还有后续页时，响应增加 `truncated=true` 与零基 `next_offset`；调用方可以将该值作为 `slide_offset` 继续读取。共享 PPTX 层不再先索引并转换全部 slide 再裁剪，而是依据 `GetSlideCount` 和 `GetSlide(index)` 只转换请求的页窗，避免大演示文稿在结构化工具路径中额外构造完整 Go model。

分页不会改变 `slide_count` 的总数、每页的原始 `Number` 或既有 JSON 字段；`read_document` 的全文文本分页协议也完全独立保持。3 MiB JSON 上限和 Office ZIP 预检仍覆盖每一页。新增 `corelib/pptx` 回归固定 offset/continuation 元数据以及末页结束语义；真实大型 PPTX 的解析峰值、分页体验与 GUI 手工烟测仍是计划外部门禁。

### 公开遗留抽取函数的安全等价（2026-08-09）

`ExtractDocxText`、`ExtractDocText`、`ExtractXLSText`、`ExtractPPTXText` 和 `ExtractPDFText` 虽然标注为一般调用方应使用 `ExtractOfficeText`，但仍是对外可调用 API；此前直接调用时可绕过统一入口的 32 MiB 与 Office ZIP/OLE 加密、损坏容器预检。现在 Office 四个函数在进入实际 legacy parser 前自行复用格式感知的 `PreflightOfficeReadInput`；PDF 只复用共同 32 MiB 文件边界，保持其非 Office 容器职责。统一路由内部改为调用私有实现，避免正常路径重复预检。

新增回归覆盖直接调用 DOCX/PPTX 导出 API 时加密 ZIP 仍返回稳定 `encrypted` 身份，以及直接 PDF API 在整文件读取前拒绝超限。该调整不改变既有导出函数的正常文本返回，但确保 public API 不会成为迁移期安全回滚或资源限制的旁路；真实 PDF/Office 兼容性与发布灰度门禁不因此解除。

### legacy 文本回退的统一保留上限（2026-08-09）

OfficeRead 主路径原先在结果返回后已限制 100 万 rune，但 `legacy`、`dual` 和 OfficeRead 失败后的 legacy fallback 可以返回更长的全文，从而把超限文本带入分页缓存、dual token 计算或公开导出 API 调用方。现在所有 legacy 格式返回值都在统一的 `extractLegacyOfficeTextWithFormat` 边界执行同一 `MaxOfficeReadTextRunes` 验证；各个直接导出函数也在返回前复用该上限。超过时清空正文并返回稳定 `output_too_large`，避免将部分正文当成可用 dual/fallback 证据。

定向竞态回归覆盖刚好等于 100 万 rune 的合法边界和超一 rune 时的无正文稳定失败。该验证约束的是已生成结果的保留与返回，不替代对底层 legacy parser 峰值内存的真实资源审阅；后者仍须以计划规定的受权大文件与 GUI 灰度门禁确认。

### CSV 精确 range 的截断语义修正（2026-08-09）

结构化 `read_excel` 的 CSV 路径已具备 `max_rows` 流式读取，但复核发现：当调用方给出本来就不超过 `max_rows` 的精确 range，而文件在 range 外仍有后续行时，工具会错误标注 `truncated=true`。现在 `truncated` 仅表示 `max_rows` 实际截去了调用方请求的行窗；因显式 range 边界停止不再被误报为分页截断。新增回归覆盖 `A3:B3` 与 `max_rows=1` 返回一行且 `truncated=false`，同时保持更大 range 的真实截断用例。

这是第 7 节结构化工具契约的精度修正，不影响 32 MiB 输入、JSON 3 MiB、Office ZIP/OLE 预检或真实大表格的资源发布门禁。

### 完成度审计更新（2026-08-09）

截至本轮，计划第 1、3、4、6、7 与 9 节中可由当前仓库证明的实现交付物已具备：固定内部依赖与统一文本路由、格式白名单/引擎/回退/富内容配置、脱敏 dual-report 与审计器、自动注入和 `read_document` 分页兼容、知识库受控 Markdown/图片消费，以及 Office/CSV 的输入、容器、输出、缓存和结构化 JSON 边界。核心相关的 `corelib/pptx`、`corelib/excel`、`corelib/agent`、`corelib/knowledge` 与 `cmd/office-read-dual-report` 竞态定向测试均在本轮通过。

这不是“替换已发布”或“所有格式均可默认启用”的结论。阶段 0/2/3/7 的以下证据只能在仓库外完成，当前仍未满足：真实 `internal_authorized` `.doc/.xls/.ppt` dual 报告满足量化门槛、逐格式人工文本顺序与业务问答复核、P1/P2 崩溃/卡死/峰值内存/分页资源审阅、GUI 手工烟测，以及格式级回滚演练留痕。因而默认 OfficeRead 主路径继续只限 `.ppt`；`.doc/.xls/.docx/.xlsx/.pptx` 均不得因本文件中的自动化记录而提升为默认主路径，旧解析器也不得按第 8 节退役。
### 加密 Office 文档与密码支持边界（2026-08-09）

当前版本**不能**因调用方提供密码而打开受密码保护的 `.doc/.docx/.xls/.xlsx/.ppt/.pptx`。固定的 OfficeRead 版本没有密码参数或解密实现；其公开 `Options` 仅包含图片、元数据和严格内容选项，并明确声明不解密 password-protected Office documents。MaClaw 也没有在 `read_document`、自动注入、知识库导入或 GUI 配置中接收、持久化或转发 Office 密码的路径。

这是有意的 fail-closed 行为，而非密码校验失败：OOXML 加密包、XLS `FILEPASS`、Word FIB 加密标志和旧版 PowerPoint 的加密信号会在任一解析器打开正文前归类为稳定的 `encrypted`，不回退到 legacy parser，也不将密码、路径、容器细节或任何部分正文写入聊天上下文、迁移观测或知识库错误记录。因此，即使用户在聊天消息、工具参数或文件名中附带密码，当前实现也不会使用它，文件仍不能打开。

若产品决定支持该能力，必须作为独立的安全功能推进，而不是放宽现有预检：先在 OfficeRead 中实现并验证目标加密方案（OOXML Agile/Standard，以及所声明的 legacy DOC/XLS/PPT 变体）的密码校验与有界解密；然后新增一次性、`writeOnly` 的密码输入，禁止写入 `AppConfig`、会话记忆、工具历史、日志、dual-report、知识库元数据和磁盘临时文件名；解密数据只能留在受大小/展开量限制的内存或权限受控的自动清理临时存储中。还需分别验证错误密码、取消、空密码、格式不支持、并发请求、崩溃清理、密码轮换、资源上限和 GUI 手工流程。完成这些实现、测试及受权真实样本审阅前，加密文件继续是发布门禁中的拒绝样本，而不是可灰度启用的主路径能力。

### 统一文本路由的 CSV/PDF 尺寸等价（2026-08-09）

复核公开 `ExtractOfficeText` 与 `ExtractOfficeTextWithFormat` 时发现，Office ZIP/OLE 格式已有格式感知的 32 MiB 预检，而 `.csv` 仅在 `read_excel` 与知识库导入入口受限。统一文本路由也支持 CSV，且 CSV 解析会保留表格结果并拼接全文；若直接调用公开文本 API，原先可绕开该尺寸约束。同样，未知扩展名经内容签名重路由为 PDF 后，必须在调用 PDF 全文件读取实现前拥有相同的边界。

现在公共文本路由在最终格式确定后，对 CSV 与 PDF 复用 `preflightDirectDocumentInput`。已知 Office 格式仍先走其容器预检；未知扩展名的 PDF 在签名路由后按 PDF 返回 `input_too_large`。新增回归覆盖大于 32 MiB 的 CSV 在 `ExtractOfficeText` 和显式 `ExtractOfficeTextWithFormat(..., "csv")` 中均在 parser 前拒绝，以及错扩展名 PDF 的签名路由与尺寸拒绝。此修复只统一入口资源契约，不改变 dual 的 legacy 兼容语义、分页缓存、格式灰度范围或 PDF/CSV 解析结果。

### GUI 设置回归可重复性（2026-08-09）

复核 GUI 侧 OfficeRead 灰度入口确认：`NewApp` 在每次抽取时从持久化 `AppConfig` 提供引擎、格式、回退和富 Markdown 开关，环境变量仍保留最高优先级；“设置 → 常规”的四项控件、`PatchConfigFields` 白名单/规范化、设置页 DTO 与生成 Wails 类型均指向同一配置源。Go 定向测试已证明读取、保存和重载契约。

同时修复了 `GeneralSettingsPanel` 测试文件的隔离缺口：此前文件级 Vitest 运行不卸载每个测试创建的 React 树，后续 `getByLabelText` 会与上一个测试残留控件冲突，掩盖包括 OfficeRead 控件在内的实际回归。现在在 `afterEach` 调用 Testing Library `cleanup()`，15 项设置面板用例均通过；随后 `tsc --noEmit` 也通过。该变更仅改善前端测试隔离，不构成真实 GUI 烟测或发布验收的替代。

### dual-report 人工门禁完整性（2026-08-09）

复核 `cmd/office-read-dual-report` 的审计输出后，发现它原先只把文本/业务问答、P1/P2 资源审阅和格式级回滚写为不可由机器替代的人工门禁；计划第 7 节还明确要求 GUI 文件选择、附件发送、工具读取、连续对话和回退提示的手工烟测。量化审计即使通过也不能覆盖该验证，但其机器可读输出没有逐项保留这一要求，容易让后续发布流程误把 `quantitative_ready=true` 理解为完整发布许可。

现在报告生成器和 `-audit` 都从二进制的固定列表输出 `review_gui_file_selection_attachment_tool_continuous_conversation_and_fallback`。导入的 JSON 无法删除或弱化这一项；其与其他人工门禁一样不影响 `quantitative_ready` 的机械定义，却会持续出现在审计产物中以阻止错误的自动化发布解释。新增定向回归明确固定四项外部门禁，`go test -race ./cmd/office-read-dual-report -count=1` 通过。真实 GUI 手工烟测和留痕仍须在受控灰度环境完成。

### GUI 策略读取故障隔离（2026-08-09）

OfficeRead 的格式灰度策略在每次抽取时由 GUI 持久化配置提供，便于设置即时生效；复核该宿主回调边界后，补齐了与迁移/资源诊断相同的故障隔离。若 GUI 配置读取回调异常 panic，抽取层不再将它传播到文件选择、附件或 `read_document` 调用，而是退回内置的保守策略：仅 `.ppt` 可走 OfficeRead，其他格式维持 legacy，Markdown 关闭且 fallback 开启。环境变量的紧急 `legacy` kill switch 在此情形仍具有最高优先级。

新增 agent 回归固定该行为。此项只保证宿主配置故障不会造成 GUI 文档读取崩溃或意外扩大灰度范围；它不替代真实授权样本、资源审阅、GUI 手工烟测或格式级回滚演练。

### 知识库 legacy DOC/XLS 崩溃隔离（2026-08-10）

知识库在 StructuredMarkdown 灰度未开启时仍保留 `.doc/.xls` 的 legacy 导入器。虽然统一入口已先完成 32 MiB 与 OLE 加密/损坏预检，但第三方 Word/BIFF 解析器仍会处理攻击者可控的记录；此前只有聊天抽取器在这两个调用点拥有显式 panic containment。现在知识库 legacy DOC/XLS 解析也将第三方打开、工作表/段落遍历置于恢复边界，异常统一转为普通导入错误而非使导入 worker 崩溃。

新增可注入打开函数的定向回归，固定 DOC 与 XLS parser panic 均被隔离；同时在富内容回退调度器外层增加统一恢复边界，覆盖 DOCX、XLSX、PPTX 及未来的 legacy parser。该修复不放宽预检、不会把部分文本持久化为成功结果，也不改变 OfficeRead 富内容灰度或任何格式的发布许可；真实 P1/P2 资源与 GUI 烟测门禁仍须在受控环境完成。

本轮组合 GUI `-race` 命令额外暴露了既有 `ensureMemoryStore` 与配置发布之间的非 OfficeRead 数据竞争；栈和读写位置在 `gui/config_txn.go` / `gui/app.go`，与本次文档抽取改动无直接调用关系，且不应通过放宽 race 检查掩盖。OfficeRead 配置定向 `-race`、工作区 `read_document` 功能测试、agent/knowledge/excel/pptx/dual-report 定向 `-race` 均通过；该既有 GUI 并发缺陷需在独立 GUI 配置/内存生命周期任务中修复后再纳入全组合 `-race` 发布证据。

### GUI 配置快照并发修复（2026-08-10）

上节记录的组合 GUI `-race` 缺陷已完成根因修复并重新验证。`PeekConfig` 的正常热路径继续只读取不可变的原子 `configSnap`，不获取 `configMu`；仅当快照为空、需要兼容旧测试或遗留调用方以 `configCache` 作为种子时，才在 `configMu` 保护下读取并发布该写侧镜像。冷启动的 `loadConfigSnapshot` 已避免在持有同一互斥锁时经由 `publishedConfig` 回调 `PeekConfig`，从而既消除了无锁读写 `configCache/configCacheValid` 的竞态，也不引入不可重入锁死。

新增并发回归会反复交错清空快照、发布 legacy cache 种子与多个 `PeekConfig` 读取者；以下包含 OfficeRead 设置、工作区工具读取和上下文预算边界的组合验证已在 `-race` 下通过：

```powershell
go test -race ./gui -run 'Test(PeekConfigSerializesLegacyCachePromotion|PeekConfigAndPostUnlockFastPath|PublishedConfigIsLockFreeHotPath|LoadConfigConcurrentFirstRun|FilterSettingsTabConfigGeneralIncludesOfficeReadPolicy|PatchConfigFieldsOfficeReadPolicy|ToolOfficeReadDocumentFromTaskWorkspace|LimitWorkflowExtractedTextUsesOfficeReadBoundary)' -count=1 -timeout 180s
```

因此该 GUI 配置竞态不再阻碍 OfficeRead 组合自动化验证；这并不替代真实授权语料的 dual 报告、逐格式人工文本/业务问答复核、P1/P2 资源审阅、GUI 手工烟测或格式级回滚演练。默认主路径范围与 legacy 保留策略不变。

### GUI 完整配置快照一致性（2026-08-10）

继续审计配置快照协议时，确认 `LoadConfig` 的原子热路径此前直接返回了为单字段读取而裁剪过的 `configSnap`，其中不含独立发布的 `NLSkills`；冷路径却会重新附加该表。除了使 `TestNLSkillsSplitFromConfigSnap` 失败外，这会使调用方在热读取后进行读改写时有机会丢失技能配置，且让 OfficeRead 设置所在的同一配置读取协议具有不必要的冷热语义差异。

现在 `PeekConfig` 仍仅暴露精简、不可变快照，`LoadConfigForUI` 也继续不发送技能表；但面向完整 `AppConfig` 的 `LoadConfig` 会在热路径使用原子 `nlSkillsSnap` 重建完整值后再返回，与冷路径和 `publishedConfig` 一致。该附加只是值副本/独立快照读取，不会重新引入配置锁或磁盘 I/O。以下回归在 `-race` 下通过，覆盖技能分离、配置快照并发、OfficeRead 设置持久化、工作区 `read_document` 与自动注入预算：

```powershell
go test -race ./gui -run 'Test(NLSkillsSplitFromConfigSnap|CloneAppConfigForMutationIsolatesNLSkills|PeekConfigSerializesLegacyCachePromotion|PeekConfigAndPostUnlockFastPath|PublishedConfigIsLockFreeHotPath|FilterSettingsTabConfigGeneralIncludesOfficeReadPolicy|PatchConfigFieldsOfficeReadPolicy|ToolOfficeReadDocumentFromTaskWorkspace|LimitWorkflowExtractedTextUsesOfficeReadBoundary)' -count=1 -timeout 180s
```

该修复改善的是仓库内配置契约，不改变 OfficeRead 的格式灰度、加密文档 fail-closed 策略或任何仓库外发布门禁。

### GUI 配置保存与浮窗回调并发收口（2026-08-10）

在扩大 GUI 配置竞态回归时，先前的 `configSnap` 修复之后仍暴露两条与 OfficeRead 不直接相关、但会污染 GUI 自动化证据的共享状态路径。Windows 浮窗类过程在独立 OS 线程读取全局回调目标，而创建/销毁线程会写入或清空该指针；现已使用专用读写锁封装设置、条件清空和读取，避免旧窗口销毁时误清除新窗口的回调目标。新增 Windows 回归同时固定了“旧窗口不能清除已替换目标”的语义。

另一个竞态来自并发 `SaveConfig`：虽然写侧锁会串行化逻辑，但传入的完整 `AppConfig` 值仍可能与调用方先前读取的 slice/map 共用底层存储；保存前的就地清理会与另一保存操作的 JSON 序列化并发。`SaveConfig` 现在一进入即深克隆其输入，随后才执行模型供应商清理、规范化和发布/持久化；因此保存的任何内部变换不再修改调用方快照或另一写入正在序列化的对象。

以下包含配置加载、并发保存、浮窗回调目标、OfficeRead 设置、工作区工具读取和自动注入边界的组合回归已在 `-race` 下通过：

```powershell
go test -race ./gui -run 'Test(LoadConfigConcurrentFirstRun|LoadConfigStaysResponsiveDuringConcurrentPatches|NLSkillsSplitFromConfigSnap|PeekConfigSerializesLegacyCachePromotion|SaveConfigConcurrentWritesValidJSON|GlobalFloatingWindowCallbackTargetIsSynchronized|FilterSettingsTabConfigGeneralIncludesOfficeReadPolicy|PatchConfigFieldsOfficeReadPolicy|ToolOfficeReadDocumentFromTaskWorkspace|LimitWorkflowExtractedTextUsesOfficeReadBoundary)' -count=1 -timeout 180s
```

这只恢复了相关仓库内自动化验证的可信度；受控灰度环境中的真实授权语料、人工 GUI 烟测、资源审阅和格式级回滚留痕仍必须独立完成。

### 旧解析器统一 panic 隔离（2026-08-10）

继续审计统一文档抽取入口后，确认 legacy DOC/XLS 已有各自的恢复边界，
但 PDF、DOCX、PPTX 与 XLSX/CSV 的旧解析路径仍可能直接传播第三方解析库
在攻击者可控文件上的 panic。现已将这些路径收口到同一个 fail-closed
恢复函数：发生 panic 时清空任何中间正文，并返回稳定的解析失败结果。
这保持 `ExtractOfficeText`、双读 legacy 回退、分页缓存和公开兼容提取 API
的一致语义，且不改变 OfficeRead 的格式灰度范围、加密文件拒绝策略或 fallback
选择。

新增回归固定该恢复函数不会保留半截正文；以下定向竞态验证通过：

```powershell
go test -race ./corelib/agent -run 'Test(RecoverLegacyOfficeExtractionFailsClosed|ExtractDocxText|ExtractPDFTextRejectsOversizedInputBeforeRead|OfficeRead|ToolReadDocument)' -count=1 -timeout 180s
```

这项仓库内加固不替代真实授权语料 dual 报告、人工质量复核、P1/P2 资源审阅、
GUI 手工烟测或格式级回滚演练。

### 结构化 Office 读取器 panic 隔离（2026-08-10）

进一步审计 `read_excel`、`read_pptx` 与知识库表格/演示文稿导入后，确认它们都
在调用共享的文件大小和 Office 容器预检，但底层 GoExcel/GoPPT 的公开读取 API
没有统一的 panic 边界。现已在 `corelib/excel` 的单表、全工作簿和工作表列表读取，
以及 `corelib/pptx` 的带分页演示文稿读取上加入 fail-closed 恢复：依赖库 panic
时丢弃部分工作簿/演示文稿结果并返回稳定失败。这样工具 JSON、知识库节点和其他
调用这些公共包的路径具有相同的异常语义，且保留现有格式预检、分页、行/幻灯片
上限和 JSON 输出上限。

新增回归验证恢复后不会向调用方保留部分结果；以下定向竞态组合通过：

```powershell
go test -race ./corelib/excel ./corelib/pptx ./corelib/agent ./corelib/knowledge -run 'Test(RecoverSpreadsheetReadsFailClosed|ReadAllSheets|ReadCSV|RecoverPresentationReadFailsClosed|PaginatePresentation|ToolReadExcel|ToolReadPPTX|StructuredOfficeToolsRejectEncryptedContainersBeforeParsing|ParseDocumentNodes|ParseOfficeReadOrLegacyNodesRecoversLegacyParserPanic|OfficeRead)' -count=1 -timeout 180s
```

这只补齐仓库内异常隔离，不构成真实授权语料、人工 GUI 烟测、P1/P2 资源审阅或
格式级回滚演练的替代证据。

### 格式级回滚契约演练（2026-08-10）

复核 dual-report 的机械门禁后，确认它已强制 `internal_authorized` 来源、逐格式
样本数和 token 覆盖阈值，并在审计输出中固定保留人工文本/业务质量、P1/P2 资源、
GUI 冒烟和格式级回滚四项外部门禁。为覆盖计划中可在仓库内自动化验证的回滚部分，
新增 GUI 配置演练：先启用 `doc,xls`，随后仅移除 `xls`，验证引擎、fallback、
Markdown 开关及无关设置均不被改写；再切至 `legacy` 验证全局回滚，并确认两次
变更都持久化且重载一致。

以下验证通过：

```powershell
go test -race ./cmd/office-read-dual-report -count=1 -timeout 180s
go test -race ./gui -run 'Test(OfficeReadFormatLevelRollbackDrill|PatchConfigFieldsOfficeReadPolicy|FilterSettingsTabConfigGeneralIncludesOfficeReadPolicy|LoadConfigConcurrentFirstRun|PeekConfigSerializesLegacyCachePromotion)' -count=1 -timeout 180s
```

该演练证明配置层可执行的窄回滚和全局回滚契约；真实灰度环境中的实际文件读取、
用户可见 fallback 提示和操作留痕仍需由发布负责人手工完成。

### dual-report 运行策略一致性（2026-08-10）

继续审计 dual-report 的证据边界后，发现报告声明的 `Formats` 是由 CLI 参数传入，
而实际双读范围由运行时 `MACLAW_OFFICE_READ_FORMATS` 决定；两者不一致时，原先
会生成包含 `not_dual_enabled` 样本的报告，虽然最终定量门禁会失败，但操作员容易将
错误的范围视为一次有效采样。现在报告构建会先规范化声明范围，再要求它与当前
有效 dual 环境策略完全相同；缺少、额外或别名归并后的不一致都会在提取前失败。
这确保每份双读报告只可作为其实际灰度格式集合的证据，并不改变 `internal_authorized`
来源、样本阈值、token 覆盖率、隐私脱敏或人工门禁规则。

新增回归覆盖范围漂移拒绝；以下验证通过：

```powershell
go test -race ./cmd/office-read-dual-report -count=1 -timeout 180s
```

该保护只验证报告工具的策略一致性；真实授权语料、业务质量复核、资源审阅、GUI
手工烟测和发布环境回滚留痕仍须在受控灰度环境完成。

### 知识库图片导入故障隔离与资产收口（2026-08-10）

继续复核计划第 4 阶段的 Markdown、图片和知识库消费入口。DOCX/PPTX 的 legacy
图片提取已在共享 Office 预检之后运行，XML、单图与整份文档图片保留量均有上限；
OfficeRead rich-content 图片也会经过相同的受控资产入口。不过导入便利入口此前仍会
直接调用第三方 OOXML/OLE/PDF 图片读取器，且在“原图已保存、等待描述器”期间发生
取消或 panic 时可以留下数据库尚未引用的临时资产。

现在图片提取调度新增 fail-closed 恢复边界：任一底层提取器 panic 会丢弃所有节点和
字节，不会中断知识库导入 worker。单图处理也覆盖解码、缩略图和描述器边界；若已保存
的暂存资产未能形成节点（包括等待描述并发令牌时取消，或描述器 panic），会按确切随机
资产 ID 删除该资产目录。向量图片保存失败同样不再产生无资产路径的成功节点。

新增回归覆盖解析 panic 不泄露部分图像数据、描述器 panic 的资产回收，以及保存后取消
等待描述的资产回收；以下定向竞态验证通过：

```powershell
go test -race ./corelib/knowledge -run 'Test(SafeExtractDocumentImagesFailsClosedOnPanic|ProcessOneExtractedImageCleansProvisionalAssetAfterDescriberPanic|ProcessOneExtractedImageCleansProvisionalAssetOnCancelledDescribe|OfficeReadImageImport|LegacyOfficeImageImport|LimitKnowledgeDocumentImagePayloads|ExtractDOCXImagesSkipsUnreferencedLargeMedia|SafeExtractPDFImagesRecoversParserPanic)' -count=1 -timeout 180s
```

这是知识库消费路径的仓库内可靠性加固，不改变 OfficeRead 灰度格式、加密文件
fail-closed 行为或 legacy 保留策略；真实授权样本 dual 报告、格式人工质量复核、P1/P2
资源审阅、GUI 手工烟测及格式级实际回滚留痕仍是未完成的仓库外门禁。

### 富 Markdown 节点扇出上限（2026-08-10）

阶段 4 的富内容 API 已将 `StructuredMarkdown` 限制为 1,000,000 rune，并且聊天抽取、
附件自动注入和 `read_document` 始终只走文本 API。复核知识库 Markdown 转换后仍发现一个
独立资源维度：在 rune 上限内可以构造大量极短标题，进而形成同等数量的节点、FTS 写入、
规则卡片和嵌入任务。该模式不触发单节点文本截断，也不依赖图片资产限制。

现在已在 OfficeRead 结构化 Markdown 到知识库节点的受控转换边界增加每文档 10,000 节点
上限。超过该限额时将整份富内容作为 `output_too_large` 拒绝，不返回半截节点，也不会让
富内容失败回退为重新打开同一容器的 legacy 图片路径。正常 Markdown、格式匹配、输入/输出
上限及聊天严格文本化语义均保持不变。

新增“标题风暴”回归以及图片导入回归，以下定向竞态验证通过：

```powershell
go test -race ./corelib/knowledge -run 'Test(OfficeReadStructuredKnowledge(ContentRequiresExplicitOptIn|ContentRejectsHeadingStorm|Preview|ContentBlocksOversizedInputBeforeLegacyFallback|ContentRejectsMisnamedOOXML|ContentRejectsCallerKindMismatch)|SafeExtractDocumentImagesFailsClosedOnPanic|ProcessOneExtractedImageCleansProvisionalAsset|OfficeReadImageImport|LimitKnowledgeDocumentImagePayloads)' -count=1 -timeout 180s
go test -race ./corelib/agent -run 'Test(ExtractOfficeReadRichContent|OfficeRead|ToolReadDocument|FormatAutoExtracted|StripAutoExtractBodies)' -count=1 -timeout 180s
```

该项是仓库内的消费资源保护，不能替代受控灰度环境的真实语料、质量和资源审阅、GUI 烟测或
格式级回滚留痕；默认主路径、legacy 保留和加密文档拒绝策略均未改变。

### 独立图片导入的暂存资产回收（2026-08-10）

复核阶段 4 的资产生命周期时，发现上一轮对嵌入 Office/PDF 图片的“保存后失败回收”并未覆盖
目录导入的独立图片路径。该路径同样会先写入受控资产目录、后生成描述和节点；当描述器 panic
或在等待描述并发令牌时取消，原先可能留下没有数据库节点引用的独立图片资产。

现在 `ProcessStandaloneImage` 使用与内嵌图片相同的 fail-closed 生命周期：第三方图片/描述器 panic
转为无节点结果；一旦已保存的资产最终不能形成节点，便以来源的精确资产 ID 删除暂存目录。正常
成功路径、已有资产管理器的大小/像素上限以及节点资产 ID 协议不变。

新增独立图片的 panic 与“保存后取消”回收回归；以下定向竞态验证通过：

```powershell
go test -race ./corelib/knowledge -run 'Test(ProcessStandaloneImageCleansProvisionalAsset(AfterDescriberPanic|OnCancelledDescribe)|SafeExtractDocumentImagesFailsClosedOnPanic|ProcessOneExtractedImageCleansProvisionalAsset|OfficeReadImageImport|LegacyOfficeImageImport|OfficeReadStructuredKnowledge)' -count=1 -timeout 180s
```

这是阶段 4 受控资产目录的仓库内收口，不扩大 OfficeRead 灰度格式或聊天内容消费范围；真实
受权语料、人工质量检查、P1/P2 资源审阅、GUI 手工烟测和格式级实际回滚仍为未完成门禁。

### 知识库已索引文档预览接线（2026-08-10）

复核阶段 4 的“文档预览和知识库入库可消费 `StructuredMarkdown`”时，确认此前虽已有
`ParseOfficeReadRichContentForKnowledgeFile` 的受控 API，但它没有实际 GUI 调用方；不能以
仅供测试的 API 宣称预览已交付。现在知识库“来源”页为每个已导入来源新增“预览”操作，读取
`KnowledgePreviewNodesBySource` 返回的最多 100 个已持久化节点，并显示标题、类型、页/表信息
及正文。该专用 DTO 在 SQLite 查询边界将单个标题/正文限制在 512/2,000 rune，只保留
`extractor` 这一展示来源字段而不跨 Wails 传递任意节点 metadata；由 OfficeRead 结构化
Markdown 导入的节点会明确标为 `OfficeRead Markdown`。

该预览刻意不重新打开本地文件，也不直接调用富内容抽取 API：它展示的正是已入库、已受大小及
节点扇出限制约束的内容，因此不会绕过加密/异常容器 fail-closed 边界、不会暴露图片 bytes 或
主机路径，也不会把 Markdown 注入聊天附件、自动注入或 `read_document` 路径。筛选条件变化时
会清除已打开的预览，避免显示与当前来源列表不一致的旧内容。

新增存储/前端回归覆盖 OfficeRead Markdown 节点在 GUI 中可被预览，且调用仅为
`KnowledgePreviewNodesBySource(sourceID, 100)`；以下验证通过：

```powershell
go test -race ./corelib/knowledge -run 'Test(ListNodePreviewsBySourceBoundsPayloadAndOmitsMetadata|OfficeReadStructuredKnowledge|ParseOfficeReadRichContent)' -count=1 -timeout 180s
cd gui/frontend
.\node_modules\.bin\vitest.cmd --run src/components/settings/__tests__/KnowledgeSettingsPanel.render.test.tsx
.\node_modules\.bin\tsc.cmd --noEmit
```

这完成了阶段 4 的仓库内“知识库入库 + 已索引内容预览”消费链路；它不代表任意本地 Office
文件的即时预览，也不替代真实受权语料、人工质量检查、P1/P2 资源审阅、GUI 手工烟测和格式级
实际回滚等发布门禁。

### 知识库预览异步响应隔离（2026-08-10）

继续审计阶段 4 已索引内容预览的 GUI 生命周期后，确认 Wails 调用是异步的：若用户在预览
节点请求返回前修改来源筛选，旧请求此前虽会清空可见预览，却仍可能在稍后将不属于当前来源
列表的已索引文本重新挂回页面。现在每次来源筛选条件变化、用户发起新的预览请求或离开
“来源”标签时都会推进预览请求代际；只有仍属于当前代际的桥接响应才可更新预览状态。

这项收口不会取消底层请求，也不扩大 IPC DTO 或重新打开任何本地文件；它只丢弃过期的 UI
结果。因而已索引 OfficeRead Markdown 仍只通过有界 `KnowledgePreviewNodesBySource` 消费，
加密、损坏或超限 Office 容器仍不能经预览路径绕过 fail-closed 预检，也不会进入聊天或附件
上下文。新增前端回归覆盖“筛选变化后旧预览响应不得恢复”的竞态；以下验证通过：

```powershell
cd gui/frontend
.\node_modules\.bin\vitest.cmd --run src/components/settings/__tests__/KnowledgeSettingsPanel.render.test.tsx
.\node_modules\.bin\tsc.cmd --noEmit
```

这只提升阶段 4 GUI 预览的仓库内状态一致性；真实受权语料、人工质量检查、P1/P2 资源审阅、
GUI 手工烟测和格式级实际回滚留痕仍是未完成的发布门禁。

### dual-report 授权证据时效策略（2026-08-10）

继续审计阶段 0/7 的双读发布门禁时，确认审计器此前只拒绝未来时间戳；一份已通过量化门槛的
`internal_authorized` 报告可以被无限期复用。现在 `-audit` 支持可选的
`-max-authorized-report-age`：仅当报告来源为 `internal_authorized` 时，审计器才以调用时的 UTC
时钟检查报告年龄；超过指定时长则产生稳定的
`authorized_report_exceeds_max_age`，并使 `-enforce-audit` 以非零状态退出。审计 JSON 会回显生效
时长，便于流水线留痕。

时效阈值不是报告自身可编辑字段，而是受控流水线的调用策略；默认 `0` 不启用检查，保证现有历史
fixture 仍可作工具链/隐私回归而不会被误写成“已失效的发布证据”。即使选择了时效阈值，fixture
或 unknown 报告仍由既有来源门禁拒绝，不会额外被混同为授权报告；这避免破坏计划要求保留的历史
测试资产，同时为真实灰度发布提供可复现的新鲜度约束。

以下定向竞态验证通过：

```powershell
go test -race ./cmd/office-read-dual-report -count=1 -timeout 180s
```

此项仅阻止过期的已授权量化报告被复用；它不生成真实授权语料、不替代逐格式文本/业务问答复核、
P1/P2 资源审阅、GUI 手工烟测或格式级实际回滚留痕，默认 OfficeRead 主路径范围仍保持不变。

### dual-report 当前量化阈值强制（2026-08-10）

继续审计发布流水线的证据完整性后，发现 `-audit` 过去只根据报告文件内的 `minimum_samples_per_format` 与
`minimum_officeread_token_hit_rate` 重算结论。报告本身虽会被一致性校验，但一份以较低样本数或较低 token 覆盖率
生成的旧报告仍可能在更严格的当前发布策略下被复用。

现在 `-audit` 同样将调用方传入的 `-min-samples` 与 `-min-token-hit` 视为当前受控策略：报告声明低于当前要求时分别
产生 `report_minimum_samples_below_required`、`report_minimum_token_hit_below_required`；即使报告自己的旧阈值曾通过，
也会按当前阈值重新评估并在不足时产生 `required_quantitative_gate_not_pass:<format>`。审计 JSON 会回显实际执行的
两个要求，便于 CI 留痕。未显式传入参数时仍使用命令的保守默认值 10 / 0.95，因此示例审计命令现明确写出这两个值。

这使报告生成和审计使用同一可审计的最小质量策略，而不会把下调门槛藏在历史 artifact 中。以下验证通过：

```powershell
go test -race ./cmd/office-read-dual-report -count=1 -timeout 180s
```

此项只收紧机械量化证据，不替代真实 `internal_authorized` 样本、人工文本/业务问答复核、P1/P2 资源审阅、GUI
手工烟测或格式级回滚演练；默认 OfficeRead 主路径范围保持不变。

### 自动注入超限提示与工具边界一致性（2026-08-10）

复核附件自动注入的超限软错误时，发现提示曾建议对超过 32 MiB 的源文件继续使用
`read_document` 分页。分页只截断已抽取的结果，而统一工具在抽取前也会拒绝超过同一 32 MiB
全文输入边界的文件，因此该建议不可执行且容易误导调用方。现在自动注入与工具读取共享
`MaxOfficeReadFileBytes` 常量；超限提示明确说明两条路径均会拒绝，并建议先拆分、压缩或改用
专用处理工具。

新增定向回归使用真实的大于 32 MiB 文件，固定自动注入返回稳定的受限说明且不再生成不可用的
`office(action="read_document")` 分页提示。该修复不改变正常文件的自动注入预算、文本分页、
OfficeRead 格式灰度或加密容器 fail-closed 语义；真实大型文件的资源审阅和 GUI 手工流程仍须在
受控灰度环境完成。

### 富内容与 dual 影子读隔离（2026-08-10）

复核阶段 1/4 的灰度边界时，发现 `office_read_emit_markdown=true` 只检查了 OfficeRead 格式
白名单；当引擎仍为 `dual` 时，知识库可能采用 OfficeRead 的 `StructuredMarkdown` 和图片资产，
而聊天文本仍按 dual 契约返回 legacy 结果。这会在尚未通过逐格式发布门禁的影子采样期让 OfficeRead
结果成为用户可见且可持久化的知识库内容，违背“dual 只记录差异、旧结果保持权威”的迁移策略。

现在富内容开关同时要求 `office_read_engine=officeread`。`dual` 保留 Markdown 开关和格式配置以便
后续提升时无需改写策略，但 `ExtractOfficeReadRichContent`、知识库导入和预览 API 都返回未启用，
知识库继续使用既有 legacy 节点解析器。这样在格式被正式提升为主引擎前，影子 Markdown/图片既不写入
资产目录，也不进入预览；全局 `legacy` kill switch 同样保持立即生效。

新增 agent 与 knowledge 定向回归覆盖 dual + Markdown 配置：富内容 API 不活跃，知识库预览不返回
OfficeRead 节点，常规导入不标记 `officeread_structured_markdown`，直接 OfficeRead 图片消费入口也不会
创建影子图片资产。以下验证通过：

```powershell
go test -race ./corelib/agent ./corelib/knowledge -run 'Test(OfficeReadSettings_RichContentStaysOffDuringDualSampling|OfficeReadStructuredKnowledgeContentDoesNotActivateDuringDualSampling|OfficeReadKnowledgeImagesDoNotActivateDuringDualSampling|OfficeReadStructuredKnowledgeContentRequiresExplicitOptIn)' -count=1 -timeout 180s
```

此项只收紧仓库内的灰度可见性边界；它不将任何格式提升为默认主路径，也不替代真实授权样本、文本/业务质量
复核、P1/P2 资源审阅、GUI 手工烟测或格式级实际回滚留痕。

### 富内容诊断回调故障隔离（2026-08-10）

阶段 4 的 StructuredMarkdown/图片提取复用与文本抽取相同的可选资源诊断钩子，但此前只有文本
路径明确回归覆盖“资源采样器或观察者 panic 不影响抽取”。现已补充富内容路径回归：即使资源
采样器和观察者均 panic，适配层仍返回已验证的 Markdown，并且不会把诊断异常传播到知识库导入、
资产处理或 GUI 预览调用方。该语义沿用已有的安全封装：诊断记录会被省略，而不是让可选遥测变成
用户文档读取的故障源。

以下定向竞态验证通过：

```powershell
go test -race ./corelib/agent -run 'Test(ExtractOfficeReadRichContent_IgnoresPanickingDiagnostics|ExtractOfficeReadRichContent_EmitsContentFreeResourceObservation|ExtractOfficeTextWithEngine_IgnoresPanickingDiagnostics)' -count=1 -timeout 180s
```

这仅补齐仓库内可观测性故障隔离；不放宽 Markdown/图片的主引擎限制、32 MiB 输入与输出/资产边界，
也不替代真实授权语料的质量验证、P1/P2 资源审阅、GUI 手工烟测或格式级实际回滚留痕。

### 富内容图片的导入事务资产回收（2026-08-10）

继续复核阶段 4 的知识库重导入与资产生命周期后，补齐了一个失败路径：OfficeRead 图片需先落入受控资产目录，
之后才与文档节点一起写入 SQLite。若同一导入事务在节点写入后续失败或提交失败，数据库中的节点会回滚，而此前
落盘的随机图片资产可能不再被任何节点引用。

现在导入批次会跟踪本事务中新建的 `extractor=officeread` 图片资产；仅当 SQLite 提交成功才保留它们。节点写入失败
或整个批次回滚时，系统按精确、不透明的 asset ID 清理这些临时目录；既有 legacy/独立图片资产不在此清理范围内，
避免误删仍可能由已提交节点引用的历史确定性资产。正常的刷新、重导入仍沿用既有“提交后仅删除已被替换的旧资产”策略。

新增回归通过 SQLite `document_nodes` 写入失败来验证：失败的 OfficeRead 文档导入会保留失败记录，但不会在
`knowledge_assets` 下留下任何图片目录。以下验证通过：

```powershell
go test -race ./corelib/knowledge -run 'TestOfficeReadKnowledge(ImportCleansProvisionalRichAssetsWhenNodeInsertFails|ReimportReclaimsSupersededImageAssets|RefreshReclaimsSupersededImageAssets)' -count=1 -timeout 180s
go test -race ./corelib/agent ./corelib/knowledge -run 'Test(OfficeRead|ParseOfficeRead|ParseDocumentNodes|OfficeReadImageImport|LegacyOfficeImageImport|ExtractAndProcessDocumentImages)' -count=1 -timeout 180s
```

此项只完善阶段 4 的本地资产一致性，不改变加密容器的 fail-closed 语义、dual 影子读隔离或默认格式范围；真实受权
语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测与格式级回滚演练仍是未完成的发布门禁。

### GUI 配置切换期间的富内容策略快照（2026-08-10）

复核 GUI 的 `office_read_*` 配置到知识库导入的调用链后，确认配置本身通过不可变快照读取；但一次导入先解析
StructuredMarkdown、随后再处理图片。此前图片阶段会重新读取当前配置：如果用户恰好在两阶段之间将引擎切回
`legacy`，同一文档的文本节点和图片节点就可能分别来自 OfficeRead 与 legacy 解析器。

现在富内容解析会把“本次解析时 rich policy 是否启用”的决定随短生命周期的结果一同传到图片阶段。只要该次解析
已选择 OfficeRead rich 路径，图片阶段即使没有可用图片或 GUI 设置已改变，也不会重新打开同一个容器走 legacy
图片解析；直接调用且没有该解析快照的辅助入口仍按调用开始时的当前策略 fail-closed。这样配置更新继续立即影响
新请求，而已经开始的导入保持单一、可解释的解析策略。

新增回归模拟文本解析完成后将引擎改为 `legacy`，确认 DOCX 不会在图片阶段被 legacy extractor 重新打开。以下验证
通过：

```powershell
go test -race ./corelib/knowledge -run 'TestOfficeRead(RichParseSnapshotDoesNotReopenLegacyImagesAfterPolicyChanges|KnowledgeImagesUseManagedAssets|KnowledgeImportSharesOneRichExtraction|KnowledgeImagesDoNotActivateDuringDualSampling|ImageImportRejectsUnsafeContainerWithoutLegacyFallback)' -count=1 -timeout 180s
go test -race ./gui -run 'Test(OfficeRead|PatchConfigFields.*Office|LoadConfig.*Snapshot|Config.*Snapshot)' -count=1 -timeout 180s
```

该修复只保证一次知识库导入内部策略一致，不能替代真实受权样本的双读报告、人工质量复核、P1/P2 资源审阅、GUI
手工烟测或格式级回滚演练；默认 OfficeRead 主路径范围保持不变。

### read_document 分页缓存的文件版本隔离（2026-08-10）

复核 `read_document` 与附件自动注入共用的短时全文缓存后，发现原先以“清理后的路径、引擎策略、mtime 和 size”识别源文件。同步客户端或编辑器可以原地替换同一大小的文件并保留时间戳；在两分钟缓存窗口内，后续分页可能拿到旧全文，从而把不同版本文档混入同一轮对话。并发抽取的 in-flight 键也存在同一风险。

现在每次缓存查找都在既有 32 MiB 输入上限内计算完整源文件 SHA-256，并将摘要连同 mtime、size 写入缓存和 in-flight 身份。解析完成后再次核对完整摘要：若文档在解析期间变化，结果不写入缓存、等待者被释放，并由调用方有限重试；无法获得稳定版本时维持内容无关的失败边界。这样保留了同版本并发分页只做一次解析的行为，同时不会因伪造相同元数据而复用旧正文或加入旧版本的 in-flight 结果。

新增回归覆盖“等长、mtime 保持不变的原地替换”以及“替换后不得加入旧 in-flight 抽取”；以下定向竞态验证通过：

```powershell
go test -race ./corelib/agent -run 'Test(ToolReadDocument|ExtractOfficeTextCached)' -count=1 -timeout 180s
```

该项只强化工具分页的一致性和故障隔离；它不改变加密 Office 文档 fail-closed 策略、dual 影子读取隔离、默认灰度格式范围，也不能替代真实 `internal_authorized` 语料、人工质量复核、P1/P2 资源审阅、GUI 烟测与受控环境回滚留痕等发布门禁。

### OfficeRead 解析超时与并发资源边界（2026-08-10）

计划第 6 节要求 OOXML/OLE 抽取具备可观测的超时与内存指标。资源遥测此前已由 GUI 记录，但固定版本的 OfficeRead 只有同步 `Extract` API，未提供 `context` 或取消点；若异常样本卡住，单个 GUI 请求会一直等待。现在适配层为 OfficeRead 调用加上 30 秒响应预算和两个并发槽位。预算到期返回内容无关的 `timeout` 错误类别；处于迟到状态的底层调用仍持有它已取得的槽位，直至实际退出，因此重复超时不会产生无界 goroutine 或并行内存压力。

主路径在 `office_read_fallback=true` 时仍按迁移契约改走 legacy，dual 仍以 legacy 结果为准；超时遥测不含路径、正文、图片或原始错误。工作 goroutine 内部保留 panic 隔离，确保调用方已经超时返回后，后续第三方 panic 也不能终止 GUI 进程。

新增回归覆盖超时、满槽等待、迟到 worker 槽位释放、迟到 panic 隔离，以及超时后的 legacy fallback/脱敏观测；以下定向竞态验证通过：

```powershell
go test -race ./corelib/agent -run 'Test(ExtractOfficeReadResultBounded|ExtractOfficeText_Timeout|OfficeReadErrorClass|OfficeRead|ExtractOfficeText|ToolReadDocument|FormatAutoExtracted)' -count=1 -timeout 180s
```

这只是代码内的响应与资源上界，不能把 30 秒视为真实业务样本的性能结论；真实 `internal_authorized` 语料上的 P1/P2 卡死、峰值内存、文本质量、GUI 烟测与格式级回滚留痕仍是未完成的发布门禁，默认 OfficeRead 主路径范围不变。

### 预检到路径式第三方解析之间的源文件快照（2026-08-10）

继续审计 OfficeRead 的边界后，发现仅在原始路径上完成 ZIP/OLE 预检并重新计算 SHA-256 仍不足以消除
TOCTOU：固定版本的 `OfficeRead.Extract` 只接收路径，并会在适配层最后一次校验之后自行 `os.ReadFile`。
另一进程可在这个极窄窗口内替换同一路径，使实际解析字节不再是已通过预检的版本。

现在适配层会从已打开的源文件描述符、在 32 MiB 上限内复制出保留原始 Office 扩展名的私有临时快照；复制同时
核对源文件前后元数据和完整 SHA-256，以拒绝复制期间改变的输入。ZIP/OLE 预检、格式嗅探及 OfficeRead 调用均针对
该快照进行。临时文件必须保留 `.doc/.xls/.ppt` 等扩展名，避免 OfficeRead 的 legacy 分支因路径后缀丢失而改变行为。

同步调用完成后立即清理快照；若响应预算或并发槽位等待超时，快照由实际 worker 退出时清理，从而既不会让迟到的
OfficeRead 读取已删除文件，也不会积累遗留临时明文。富内容路径同样在快照上完成格式一致性判定，避免“原文件被替换后
嗅探一种格式、解析另一种格式”的元数据混用。公开的原始路径预检仍用于入口 fail-closed 决策，但不再授权后续直接
解析原始路径。

新增回归覆盖：预检后替换原路径时解析器只收到私有快照且读取旧的已验证内容；同步完成后快照已删除；超时返回后快照
仍保留至迟到 worker 退出再删除。以下验证通过：

```powershell
go test -race ./corelib/agent -run 'Test(ExtractOfficeReadResult|OfficeRead|ExtractOfficeText)' -count=1 -timeout 180s
go test -race ./corelib/agent ./corelib/knowledge -run 'Test(OfficeRead|ToolReadDocument|FormatAutoExtracted|ExtractTextFromFile|ParseDocumentNodes|ExtractOfficeTextCached)' -count=1 -timeout 300s
git diff --check
```

同一轮复核还移除了历史上曾暴露“调用方已完成原路径预检”的内部优化入口。该入口容易诱导后续调用方把原始路径的
预检结果当作第三方解析授权；现在所有生产 OfficeRead 路径统一只接受“复制后、预检完成的私有快照”。为保证同步
语义，worker 先完成快照清理再向调用方发送结果；超时调用方仍在 worker 实际退出前保留快照。以下定向竞态验证通过：

```powershell
go test -race ./corelib/agent -run 'TestExtractOfficeReadResult' -count=1 -timeout 180s
go test -race ./corelib/agent ./corelib/knowledge -run 'Test(OfficeRead|ToolReadDocument|FormatAutoExtracted|ExtractTextFromFile|ParseDocumentNodes|ExtractOfficeTextCached)' -count=1 -timeout 300s
```

进一步复核 OfficeRead 失败后的兼容回退，发现如果 shadow 读取的是私有快照、但 legacy fallback 随后重新打开
原路径，仍可能在两者之间混入替换版本。现在生产文本路径先建立一份受检的共享快照；OfficeRead worker 从其复制的
子快照读取（保留超时后的独立生命周期），而 `dual` 与 `office_read_fallback=true` 的 legacy 路径使用共享快照。
因此同一次抽取的两个引擎始终比较/返回同一版本；新增回归在 OfficeRead 返回失败前替换原路径，确认 fallback 仍返回
已验证版本的正文，并覆盖 dual shadow 成功后替换原路径仍产生同一版本的 legacy 返回及共享 token 统计。该修复不改变
测试专用 parser seam、格式默认范围或加密文件 fail-closed 行为。

最终组合验证通过：

```powershell
go test -race ./corelib/agent -run 'Test(ExtractOfficeReadResult|OfficeRead|ExtractOfficeText|ToolReadDocument|FormatAutoExtracted)' -count=1 -timeout 180s
go test -race ./corelib/agent ./corelib/knowledge -run 'Test(OfficeRead|ToolReadDocument|FormatAutoExtracted|ExtractTextFromFile|ParseDocumentNodes|ExtractOfficeTextCached)' -count=1 -timeout 300s
go test -race ./gui -run 'Test(OfficeRead|PatchConfigFields.*Office|LoadConfig.*Snapshot|Config.*Snapshot|ToolOfficeReadDocumentFromTaskWorkspace|LimitWorkflowExtractedTextUsesOfficeReadBoundary)' -count=1 -timeout 8m
git diff --check
```

为区分计划相关验证与工作区既有不稳定项，另行执行了未筛选的 `-race` 包测试。`corelib/agent` 未筛选运行在
`TestRunLoop_ReplanInterruptsTransientRetryBackoff` 的 steering 时序断言失败；`corelib/knowledge` 未筛选运行报告
`TestScanDirectoryProgressCallback` 及 `TestImportProgressEmitsLastItemReasonAndFailedItems` 的进度回调数据竞争。这些失败
栈位于 loop/scan/import 进度测试与实现，未经过本次 OfficeRead 快照路径；因此没有把定向 Office/知识库/GUI 绿灯
扩大表述为全仓库 `-race` 绿灯。它们应在各自的通用循环与知识库进度并发任务中修复后，才能作为计划第 7 节的完整
全包竞态证据。

随后已收口知识库部分：并行哈希仍并发执行，但其 scan progress 回调改为单消费者串行分发；导入及后台 linking/
embedding 阶段也通过同一受保护边界调用 import progress，避免 GUI 事件桥或简单回调闭包同时写入状态。回归测试在
检查回调收集结果前等待已公开的 `WaitBackground()` 生命周期，避免测试本身与导入完成后仍允许继续的 post-work
进度更新并发读取。新增重叠探针固定 store 级 scan 回调在并行哈希下不重入。以下完整知识库竞态测试已通过：

```powershell
go test -race ./corelib/knowledge -count=1 -timeout 9m
```

随后已收口 agent loop 部分：Responses 流式请求遇到可重试的 408、429、5xx、超时或网络错误时，不再在流式辅助函数内
立即降级为非流式请求。错误回到外层统一重试循环，该循环在退避期间持续检查 live steering/replan，因此新的用户指令会
先替换会话上下文，而不会被紧跟着发出的旧上下文非流式请求抢跑。非可重试的流式失败仍保留既有兼容性降级；任何已向 UI
发出正文增量的部分流也仍不降级，以避免重复可见输出。

`TestRunLoop_ReplanInterruptsTransientRetryBackoff` 同时改为返回带 `application/json` 内容类型的非流式 Responses
成功响应，准确模拟兼容提供方的降级协议，而不是把普通 JSON 误当成空 SSE 流再触发额外请求。以下全包竞态回归已通过：

```powershell
go test -race ./corelib/agent -run '^TestRunLoop_ReplanInterruptsTransientRetryBackoff$' -count=20 -timeout 180s
go test -race ./corelib/agent -count=1 -timeout 6m
```

该项修复通用 steering 的请求时序，不改变 OfficeRead/知识库的抽取协议、安全预检、加密容器 fail-closed 或格式灰度
范围；真实授权样料、人工文本/业务质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚演练仍是未替代的发布门禁。

同一轮全量定向命令中的 `./gui` 未能开始测试，原因是工作区既有 `gui/lansenger_bot_runtime.go:221` 引用了缺失的
`agent.AssistantBinding`，与本项文件快照改动无关。该项只收紧本地文件一致性与临时明文生命周期；不支持密码解密，
不改变加密 Office fail-closed 策略、默认 `.ppt` OfficeRead 范围，也不替代真实授权样本、人工质量复核、P1/P2
资源审阅、GUI 手工烟测及格式级回滚留痕等外部门禁。

后续清理 Go 构建缓存并给予首次 GUI 编译充分时间后，`AssistantBinding` 可正常从
`corelib/agent/message.go` 解析；先前失败属于不完整的首次编译/缓存状态，不是当前源码缺失类型。以下 GUI
OfficeRead 配置、工作区文档工具与上下文预算定向竞态验证已通过：

```powershell
go test -race ./gui -run 'Test(OfficeRead|PatchConfigFields.*Office|LoadConfig.*Snapshot|Config.*Snapshot|ToolOfficeReadDocumentFromTaskWorkspace|LimitWorkflowExtractedTextUsesOfficeReadBoundary)' -count=1 -timeout 8m
```

### 全路由文档快照一致性（2026-08-10）

继续审计统一入口后，发现先前的私有快照只覆盖 OfficeRead 主调用及其 fallback/dual 对照；当格式灰度关闭、全局
`legacy` 回滚或调用显式 `ExtractOfficeTextWithFormat` 时，旧解析器仍可能在原路径预检之后重新打开用户可替换的文件。
现在生产文本路由会在 32 MiB 预算内从已打开 descriptor 复制并校验 SHA-256 的私有快照；扩展名嗅探、ZIP/OLE
预检、OfficeRead 和 legacy 解析均针对同一快照。未知扩展名保留原始后缀供内容嗅探，已知文档格式保留解析器所需
后缀。若文件在预检复制窗口内变化则返回稳定的 `source_changed`，不会以预检旧版本来授权解析新版本。OfficeRead
超时仍使用独立子快照及原有 worker 清理生命周期，避免取消路径误删仍被读取的文件。

显式格式入口保留原有预检先行语义；若预检期间发现源文件变化会 fail-closed，成功预检后的实际 legacy/OfficeRead
解析则只读取私有快照。测试 seam 继续直接模拟提取结果，不把“无真实文件的 parser 结果”测试误变成文件系统失败。
新增回归覆盖全局 legacy 自动路由在预检后路径被替换时仍返回已验证正文，以及显式 legacy 路由在同一窗口稳定拒绝
`source_changed`；附件自动注入的 PPT 回归同时固定 OfficeRead 收到的是保留 `.ppt` 后缀的私有快照而非原路径。

```powershell
go test -race ./corelib/agent -run 'TestExtractOfficeText(_LegacyReadsPrivateSnapshotAcrossReplacement|WithFormat_LegacyFailsClosedWhenPreflightSourceChanges|_FallbackReadsSameSnapshotAsOfficeRead|_DualReadsSameSnapshotAcrossReplacement)' -count=20 -timeout 180s
go test -race ./corelib/agent -count=1 -timeout 6m
go vet ./corelib/agent
git diff --check
```

### 工作流表单伪装容器的 fail-closed 收敛（2026-08-10）

继续审计工作流简历解析和补充材料预填入口后，发现其纯文本与未知扩展名兼容分支会直接读取原始路径。攻击者可将 ZIP/OLE Office 容器伪装为 `.txt` 或未知后缀，使二进制内容绕过共享的容器预检、版本核验和 OfficeRead 迁移边界，进入工作流表单或补充材料提示。

现在所有工作流文件首先建立受 32 MiB 限制、复制期间校验源版本的私有快照。已知 Office/PDF 格式与表面上的纯文本格式均先通过 `ExtractOfficeText` 的签名和容器边界；加密、容器不安全/损坏、源文件变化、输入超限或输出超限时稳定 fail-closed，绝不回读原始路径。未知扩展名仅在共享路由确认其不是 Office/PDF 容器且未返回上述拒绝类别后，才读取该已验证快照以保留既有纯文本兼容性。PDF 的 Python 质量回退仍仅适用于普通解析失败，并且只读取快照。

新增 GUI 回归覆盖伪装为 `.txt` 和未知后缀的 ZIP 容器，确认其返回无内容的稳定策略拒绝；既有 DOCX、输入上限、错误脱敏与提示文本上限回归也继续通过。该收敛不改变默认仅 `.ppt` 的 OfficeRead 主路径，不接受密码解密加密 Office，也不替代真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测或格式级回滚演练等仓库外发布门禁。

```powershell
go test -race ./gui -run 'Test(ExtractTextFromFile|LimitWorkflowExtractedTextUsesOfficeReadBoundary)' -count=1 -timeout 8m
git diff --check
```

### 共享文档快照的最终源身份复核（2026-08-10）

复核所有路径式解析器依赖的 `SnapshotBoundedDocumentInput` 后，发现原实现虽从已打开的文件描述符复制并验证了私有副本，但复制结束后的版本检查仍只检查该旧描述符的 size/mtime。若外部进程恰在复制完成后以原子重命名替换原路径，或就地改写后恢复相同大小和时间戳，后续调用方可能继续使用版本 A 的快照，而用户路径已代表版本 B；这会使调用方无法获得稳定的 `source_changed` 结论。

现在快照写入并验证其 digest 后，会关闭旧描述符并重新检查原路径是否仍指向同一文件，再对原路径进行一次受 32 MiB 限制的完整摘要校验。文件对象、大小或字节摘要任一不一致，以及最终检查无法稳定读取，均返回 `source_changed` 并删除临时副本。该边界由 Office 文本路由、公开兼容抽取 API、结构化工具、知识库和工作流表单共同使用，因此后续路径式解析只会处理与用户路径仍一致的已验证版本。

新增回归覆盖复制后原子替换，以及同大小、恢复时间戳的原地重写，均确认不会返回快照或 cleanup 句柄而是稳定拒绝。该改动不改变默认 OfficeRead 仅 `.ppt` 的主路径、加密文件不接收密码解密、ordinary timeout 回退契约或仓库外发布门禁；真实 `internal_authorized` 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍需独立完成。

```powershell
go test -race ./corelib/agent -run 'TestSnapshotBoundedDocumentInputRejects(AtomicReplacement|MetadataPreservingRewrite)AfterCopy' -count=1 -timeout 4m
git diff --check
```

### 知识库纯文本入口的容器与版本收敛（2026-08-10）

继续审计知识库 Markdown/纯文本的并行导入路径后，发现这两个“轻量”格式曾直接读取 live 路径，也不参与 Office/PDF/CSV 已有的扫描摘要到私有快照身份复核。其后果有两类：伪装为 `.md`、`.txt` 或 `.text` 的 ZIP/OLE Office 容器可以作为二进制正文入库；已存在 Source 的 Markdown/纯文本在扫描后被替换时，批量路径可能先删除版本 A 的派生节点、再从 live 路径解析版本 B。

现在 Markdown、TXT/TEXT 都先建立受限私有快照，并先经过统一 `ExtractOfficeText` 的签名与容器检查。损坏/加密容器稳定拒绝；即使容器本身有效、但签名路由为 Office/PDF，也返回 `format_mismatch`，不会再用文本解析器重开。普通纯文本保留既有 Markdown/段落切分语义，但其解析、持久化 `content_hash` 和后续派生均绑定同一快照。

并行轻量导入也改为在删除既有派生内容之前建立并解析该扫描摘要一致的快照。若扫描后发生变化，导入项记录稳定 `source_changed`，既有 Source 的摘要、节点和卡片保持原样；首次导入则保留无节点的失败记录以供追踪。新增回归覆盖 `.md/.txt` 伪装 ZIP、有效 DOCX 伪装 Markdown，以及 Markdown 重导入在扫描后替换时不破坏既有索引。

该收敛不改变默认仅 `.ppt` 的 OfficeRead 主路径、加密 Office 不支持密码解密、普通 timeout 兼容回退或仓库外发布门禁；真实 `internal_authorized` 语料、人工文本质量复核、P1/P2 资源审阅、GUI 手工烟测及格式级回滚留痕仍需独立完成。

```powershell
go test -race ./corelib/knowledge -run 'Test(ParseDocumentNodesRejects|ImportFilesLightMarkdownRejectsSourceChangedAfterScanWithoutReplacingExisting|ImportDirectoryParallelLightMarkdown)' -count=1 -timeout 4m
git diff --check
```

### 知识库文本刷新与快照版本绑定（2026-08-10）

在把 Markdown/纯文本导入纳入快照边界后，继续复核刷新和预览共用的 `buildFileRefreshSourceAndNodesWithOfficeReadRichContentForImport`，发现刷新先对 live 路径计算摘要、随后才创建私有快照；若文件在这两步之间被替换，新的节点可能来自版本 B，而 `content_hash` 却仍代表版本 A。该问题也会使预览的“新摘要/差异节点”和实际刷新结果无法对应。

现在刷新对所有受支持文件先取得受 32 MiB 上限约束的摘要，再以共享私有快照完成解析；快照摘要必须与刷新开始时的摘要完全一致，否则返回稳定 `source_changed`，不写入、不删除既有节点或卡片。普通文本、Markdown、Office、PDF 与 CSV 因而共享同一刷新身份契约；加密 Office 的已有稳定失败持久化语义保持不变。

新增 Markdown 回归在刷新开始后、快照创建前替换文件，验证 `RefreshSource` 返回 `source_changed` 且原 Source 的摘要、节点、卡片均不变。该修复不改变默认仅 `.ppt` 的 OfficeRead 主路径、密码不解密策略、普通 timeout 回退契约或仓库外发布门禁；真实 `internal_authorized` 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍需独立完成。

```powershell
go test -race ./corelib/knowledge -run 'Test(RefreshLightMarkdownRejectsSourceChangedWithoutReplacingExisting|ImportFilesLightMarkdownRejectsSourceChangedAfterScanWithoutReplacingExisting)' -count=1 -timeout 5m
git diff --check
```

### read_document 前置大小拒绝的协议统一（2026-08-10）

进一步检查 `read_document` 的快捷大小判断后，发现它在进入统一错误分类前直接返回一条普通文本，并建议“使用专用
处理工具”。这会造成相同的 `input_too_large` 结果在缓存/快照路径与 `Stat` 快捷路径上具有不同的工具协议，后者也会
与“不可建议其他解析器绕过资源上限”的规则矛盾。

现将前置判断交给 `formatOfficeReadFailure` 的受控 `input_too_large` 分支：响应保持路径与格式元数据，但稳定提供
`error_class=input_too_large`、32 MiB 限制和拆分/缩小文件的建议，且不再包含 `craft_tool`、Skill、COM、LibreOffice
或“专用处理工具”的绕过提示。回归测试锁定该早返回行为，确保它和后续快照/解析路径遵循同一 fail-closed 协议。

此项只统一资源拒绝的用户可见语义，不增加任何大文件旁路，也不替代真实 `internal_authorized` 样本的 dual 报告、
人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕等仓库外发布门禁。

### 自动附件注入与工具安全提示对齐（2026-08-10）

同一轮检查发现，自动附件注入的早期 32 MiB 大小拒绝和已分类的解析失败仍会写入“使用专用处理工具”、
`read_document` 或 `craft_tool` 建议。对于加密、容器安全、版本变化和资源上限，这会在模型上下文中再次诱导
重开已经拒绝的同一输入，且和刚收紧的工具协议不一致。

现在自动注入会复用工具的受控失败分流：`encrypted`、`malformed`、`source_changed`、`input_too_large` 和
`output_too_large` 都生成带稳定 `error_class` 的无正文错误块，并使用相同的安全、版本或资源说明；不会出现
`read_document`、`craft_tool`、Skill、COM、LibreOffice 或“专用处理工具”的绕过建议。普通解析错误继续给出原有
可恢复指引。新增回归覆盖加密附件和前置超限附件，防止该双入口协议再次分叉。

这只保证仓库内自动注入不绕过已有 fail-closed 决策，不增加密码能力或解析范围；真实 `internal_authorized`
`.doc/.xls/.ppt` dual 报告、文本顺序和业务问答人工复核、P1/P2 资源审阅、GUI 手工烟测及格式级回滚留痕仍是
仓库外发布门禁。

### 模型可见 Office 工具说明的边界同步（2026-08-10）

最后复核 GUI 注册给模型的 `office` 工具说明，发现它仍以“若失败必须继续用 `craft_tool`”描述所有失败，和运行时
工具、自动注入的五类 fail-closed 错误分流相冲突。运行时即使安全返回，模型也可能因元数据的绝对指令再次尝试绕过。

现在工具说明仅允许对非 `encrypted`、`malformed`、`source_changed`、`input_too_large`、`output_too_large` 的失败使用
`craft_tool` 处理不支持格式，并明确这五类必须按工具提示处理、不得使用其他解析器绕过。注册表回归测试锁定五个稳定
类别和该禁止语义。该调整将 GUI 模型上下文与统一抽取边界对齐，不扩大功能或默认格式范围。

它依然不能替代真实 `internal_authorized` 语料 dual 证据、人工文本质量审阅、P1/P2 资源审阅、GUI 烟测与格式级回滚
演练；这些仍是提升任何额外格式为默认路径前的仓库外发布门禁。

### 工作流附件预填的 PDF 回退边界（2026-08-10）

继续追踪 GUI 的工作流简历/补充材料预填入口，确认它已先经共享 `ExtractOfficeText`，但 PDF 失败时会调用 Python
备用提取。对普通 PDF 质量失败这是既有兼容行为；但若共享入口已经返回加密、容器安全、源版本变化、输入或输出资源
拒绝，该回退会重开相同文件，形成 Office 工具之外的 fail-closed 旁路。

现在预填入口对这五类导出的稳定错误直接返回内容无关的策略拒绝，不执行 Python 回退；普通 PDF 解析失败仍维持原有
质量回退。回归锁定已分类的加密错误会阻断回退、普通 PDF 解析错误仍可回退；它不声称项目现已识别所有 PDF 加密方案。
该变更只封闭 GUI 工作流的同文件重试旁路，不影响正常 PDF 兼容性，也不改变密码不解密、默认 `.ppt` 灰度范围或发布门禁。

### 共享系统提示与 Office fail-closed 协议对齐（2026-08-10）

继续检查模型实际接收的共享系统提示后，发现“office 失败必须 `craft_tool`”仍是无条件指令；即使运行时工具、自动
附件注入和 GUI 工具描述均已禁止绕过，模型仍可能因这一更高优先级的提示对加密、损坏、版本变化或资源拒绝重开同一文件。

现将文档读取阶梯改为先检查 `error_class`。`encrypted`、`malformed`、`source_changed`、`input_too_large` 与
`output_too_large` 必须遵循原提示，禁止同文件 `craft_tool`、Skill、bash、COM、LibreOffice 或其他解析器回退；其中
加密明确不接收密码也不支持密码解密。仅普通解析失败或不支持格式仍走原有一次性脚本/Skill 恢复路线。新增共享提示
回归锁定五类错误和密码边界。

该调整只消除模型上下文与运行时 fail-closed 契约的冲突，不改变 OfficeRead 默认格式范围，也不替代真实
`internal_authorized` 双读报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕等仓库外发布门禁。

同样收紧了工作流 V2 的文件解析阶段提示及专利交底材料的内嵌文件说明；它们原先也以无条件“office 失败后必须
craft_tool”为准。现在均先枚举五个不可绕过的 `error_class`，将脚本、Skill、bash、PowerShell COM 和 LibreOffice
恢复限制为普通失败或不支持格式。工作流提示回归固定五个错误类别和“提供密码后不解密”的说明，避免工作流模型上下文
重新引入旁路。

后续复查发现 USPTO `us_disclosure_analysis` 使用独立英文交底书提示，因而未继承上述共享提示；其中仍有无条件
“If office fails”恢复指令。现已同步为同一五类 `error_class` 的 fail-closed 规则：加密文件不接收密码、也不支持
密码解密；只有普通失败或不支持格式才允许 `craft_tool`、Skill 或 bash 恢复。新增独立提示回归，防止该特殊工作流
重新把已拒绝的同一文件交给 COM、LibreOffice 或其他解析器。本项不改变默认仅 `.ppt` 的 OfficeRead 主路径，也不替代
真实 `internal_authorized` 双读报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕等仓库外发布门禁。

同一审计还发现专利技术解析 `tech_parsing` 虽会注入共享“文档解析方法”，其阶段指令却要求优先自行以 bash/Python
读取文件；该局部强指令会与五类拒绝结果冲突。现改为先使用自动注入正文或 `office(action="read_document")`，并明确
`encrypted`、`malformed`、`source_changed`、`input_too_large`、`output_too_large` 不得交给 bash/Python 或其他解析器
重开；仅普通失败或不支持格式可沿共享阶梯恢复。回归直接断言最终阶段提示含该规则。此修复不扩大 OfficeRead 的默认
`.ppt` 主路径，不改变加密文件不接收密码/不解密策略，也不替代真实授权样本双读、人工质量复核、P1/P2 资源审阅、GUI
手工烟测和格式级回滚留痕等仓库外发布门禁。

### OfficeRead 主路径输出上限不再回退 legacy（2026-08-10）

审查共享提示后的运行时复核发现，`officeread` 主路径在 OfficeRead 返回超过保留文本上限时，若配置
`office_read_fallback=true` 仍会将同一文件交给 legacy 重新解析。`output_too_large` 是 MaClaw 的资源策略，
不是可用另一个解析器规避的质量失败；这一行为与前述工具、自动注入和模型提示相矛盾。

现在主路径对该错误直接返回稳定 `output_too_large` 观测，Legacy 不会运行、也不会标记 fallback。`dual` 模式保留
原有的 legacy 返回契约，因为它专门用于不改变旧用户输出的影子对照；其差异报告仍将 OfficeRead 侧记为资源拒绝。
新增回归覆盖主路径在 fallback=true 下不重开 legacy。此项不改变默认 `.ppt` 格式范围，也不替代真实
`internal_authorized` dual 证据、人工质量复核、P1/P2 资源审阅、GUI 烟测或格式级回滚留痕。

### 知识库 rich 输出上限的 legacy 回退收口（2026-08-10）

复核知识库的 structured Markdown 路径后发现，rich 内容因为 Markdown rune 限制或标题风暴触发
`output_too_large` 时，节点解析仍会回退到 legacy parser；独立的图片便利入口也可能重开同一文件。
这和文本抽取主路径的输出边界不一致，且可将一个已拒绝的 rich 文档以不同的节点/资产扇出形态持久化。

现在知识库节点解析把 `ErrOfficeReadOutputTooLarge` 视为不可回退的资源拒绝；图片便利入口同样不再转到 legacy
图片解析。新增回归以显式 rich 输出上限验证 legacy node parser 不会调用。该收紧只约束显式启用的 rich Office
消费；rich 未启用或普通解析失败仍保持迁移期 legacy 兼容性。它不改变默认 `.ppt` 灰度、密码不解密策略，也不替代
真实 `internal_authorized` 双读证据、人工质量复核、P1/P2 资源审阅、GUI 烟测与回滚留痕。

### 工具失败分流的 fail-closed 一致性（2026-08-10）

在加密提示收口后继续复核错误类别，发现 `malformed`、`source_changed`、`input_too_large` 与
`output_too_large` 仍会落入普通恢复分支，向模型建议 `craft_tool`、Skill 或转换器。这会使已经被容器安全、
同版本快照或资源预算拒绝的**同一文件**被另一个更宽松的解析路径重开，和既定边界不一致。

现已将源版本变化纳入稳定的 `source_changed` 分类，并为上述五类（含 `encrypted`）提供专用、无内容的
提示：安全容器错误要求重新获取/修复可信文件；版本变化要求等待写入完成后重试；输入/输出资源上限要求拆分
或缩小文件；加密仍明确不支持密码解密。所有这些类别均不再输出 `craft_tool`、Skill、COM 或 LibreOffice
建议。普通 `extract_error` 等非策略失败仍保留既有、安全转义的恢复路线。

这项调整只防止工具文本诱导绕过已存在的安全、版本和资源边界；不增加密码处理能力，也不替代真实
`internal_authorized` `.doc/.xls/.ppt` dual 报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测或格式级
回滚留痕等仓库外发布门禁。

```powershell
go test -race ./corelib/agent -run 'Test(FormatOfficeReadFailure|OfficeReadErrorClassUsesStableCategories)' -count=1 -timeout 240s
go vet ./corelib/agent
git diff --check
```

### 加密文件工具提示的 fail-closed 收口（2026-08-10）

继续审查 `read_document` 与结构化 Office 工具的失败提示后，发现此前所有失败都会建议使用
`craft_tool`、Skill、COM 或 LibreOffice 继续尝试。这与加密容器的既定安全边界冲突：当前产品不接收
密码、也不支持密码解密，通用恢复指令可能诱导后续工具绕过 `encrypted` 的 fail-closed 决策。

现在 `error_class=encrypted` 返回固定、内容无关的说明：当前不支持“提供密码后解密或读取”；用户只能在
受信任的本地 Office 应用中自行解密并另存未加密副本后重试。该分支不会再输出 `craft_tool`、Skill、COM、
LibreOffice 或密码输入建议；普通解析失败仍保留原有的、安全 JSON 引号编码的恢复提示。新增回归同时锁定两条
契约。

此项只收紧工具协议的加密失败语义，不实现密码处理，也不改变默认仅 `.ppt` 的 OfficeRead 灰度范围；真实
`internal_authorized` `.doc/.xls/.ppt` dual 报告、文本顺序和业务问答人工复核、P1/P2 资源审阅、GUI 手工烟测
以及格式级回滚留痕仍是不可由仓库内测试替代的发布门禁。

```powershell
go test -race ./corelib/agent -run 'TestFormatOfficeReadFailure' -count=1 -timeout 240s
go vet ./corelib/agent
git diff --check
```

### 双读报告摘要的 BOM 绑定一致性（2026-08-10）

继续审查双读报告与人工回执的跨文件绑定时，发现报告解码器已明确接受 PowerShell 复制或重写 JSON 时可能加入的单个 UTF-8 BOM，但回执绑定仍对原始文件字节直接计算 SHA-256。于是同一逻辑报告仅因该已允许的传输标记出现或消失，就会触发 `release_evidence_report_digest_mismatch`，迫使不必要地重做人工回执。

现在计算双读报告绑定摘要前只移除这一种已允许的前导 BOM；不会规范化空白、字段顺序或其他 JSON 表示，因此其他任何报告字节变化仍需新建并重新完成绑定回执。新增回归确认 BOM 版本与无 BOM 版本得到同一摘要，而尾随换行等非 BOM 改动仍会改变摘要。该修复仅使既有严格解析契约与回执绑定一致；真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是不可由仓库内检查替代的发布门禁。

### 知识库 OfficeRead 超时错误的稳定持久化（2026-08-10）

继续复核第 4/6 节中 OfficeRead 的超时边界时，发现适配层已将超过响应预算的调用归类为内容无关的 `ErrOfficeReadTimedOut`，但知识库的 Office 错误净化白名单遗漏了该稳定类别，导致导入历史和刷新记录会把它降级为泛化的 `OfficeRead content extraction failed`。这不会泄露内容，却会抹去 P1/P2 审阅和格式回滚所需的可操作超时信号。

现在知识库持久化与进度错误边界保留该受控超时错误身份，其他解析器或文件系统错误仍统一净化为泛化失败；回归覆盖超时错误不丢失、不会将原始 parser detail 写入状态。该修复仅完善仓库内诊断可追溯性，不能替代真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测或格式级回滚留痕等仓库外发布门禁。

### OfficeRead 格式范围的全量 fail-closed 解析（2026-08-10）

继续复核阶段 1/2 的灰度开关后，发现运行时策略原先会从非空的环境变量或外部编辑的持久化格式列表中静默丢弃未知/空格式，却保留其余合法项。例如 `doc,pdf` 会实际启用 `.doc`，使操作失误看似仅是无害拼写问题，实则扩大了计划之外的主路径范围。

现在非空范围必须整体有效，任何空项或未知格式都会使该次策略不启用任何 OfficeRead 格式；只有真正空列表继续表示保守默认 `.ppt`。合法格式仍会去重和规范化。新增回归同时覆盖持久化策略和最高优先级环境覆盖，确认 `doc,pdf` 不会部分启用 `.doc`，`.DOC,ppt` 仍正确生效。该收紧不改变显式 `legacy` 全局回滚、默认 `.ppt` 灰度或加密文件/密码不解密策略；真实授权语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是仓库外发布门禁。

同一语义现也用于独立的 `office-read-dual-report` 证据 CLI：此前该 CLI 会从 `doc,pdf` 中静默保留 `doc`，与抽取器的有效灰度范围可能不一致，进而有机会生成不应被收集的 dual 证据。现在该类非空畸形范围的有效报告范围为空，`buildReport` 因与任何声明格式不匹配而拒绝执行；合法范围仍排序、去重并与 runtime policy 逐格式绑定。新增 CLI 回归覆盖该拒绝路径。该工具性收紧不替代真实授权样本与人工发布门禁。

### OfficeRead 布尔环境覆盖的显式启用（2026-08-10）

继续复核阶段 4 的 rich-content 开关后，发现 `MACLAW_OFFICE_READ_EMIT_MARKDOWN` 与 fallback 环境变量曾把任意非空、非 `false` 值当成 `true`。这意味着错误的环境值（如 `enable-now`）可在 GUI 持久化策略明确关闭时意外启用结构化 Markdown/图片消费，或反向改变回退行为。

现在环境覆盖只接受 `true`/`false` 或 `1`/`0`；空值和无效值均保留持久化配置，不能形成隐式启用。新增回归确认错误值不会开启 rich-content 或 fallback，而显式 `true`/`1` 仍能作为运维覆盖生效。该修复不改变默认 `.ppt` 灰度、dual 模式不暴露 rich-content、加密 Office fail-closed 或密码不解密策略；真实授权样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和实际回滚留痕仍是仓库外发布门禁。

### GUI OfficeRead 格式补丁的空项拒绝（2026-08-10）

复核 GUI/API 的配置写入后，发现运行时已经把 `doc,` 这类非空畸形范围 fail-closed，但 `PatchConfigFields` 仍会静默丢弃格式数组中的空项并持久化其余值。外部调用方因此可能误以为提交了完整范围，实际却留下了缩窄后的灰度策略，与运行时/dual-report 的整体解析规则不一致。

现在 GUI 配置补丁中的每个格式项都必须非空且受支持；如 `['doc', '']` 会整体拒绝，原有持久化策略保持不变。真正的空数组仍保留既有语义，可用于回到保守的 `.ppt` 默认范围。新增回归覆盖该拒绝路径。此项只统一设置写入与运行时的格式级回滚契约，不替代真实授权样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级实际回滚留痕。

### 发布人工门禁回执的可校验留痕（2026-08-10）

双读报告的 `quantitative_ready` 只证明受控 `dual` 运行的机械比较满足样本量、来源、格式和 token 覆盖率门槛，不能把人工文本顺序/业务问答、P1/P2 资源审阅、GUI 烟测或回滚演练伪装成自动化结论。为让这些仓库外门禁可执行且可留痕，`office-read-dual-report` 现在支持在审计既有双读报告时生成独立的、内容无关的回执模板：

```powershell
go run ./cmd/office-read-dual-report -audit .\authorized-dual-report.json -required-formats doc,xls,ppt -write-release-evidence-template .\office-read-release-evidence.json
```

模板只绑定双读报告的 SHA-256、规范格式列表、受限的审核人标识和四项既定门禁；不允许记录样本文本、文件名/路径、密码、截图或自由备注。模板刻意不预填 `created_at` 或任何审核时间，且不会覆盖既有文件，避免未实际审核的模板因生成时间而成为陈旧/误用的回执。发布负责人完成真实的仓库外审阅后，必须填写回执 `created_at`、将每项状态改为 `completed`、填写 UTC 时间并保持每项覆盖同一格式范围。随后可将它同双读报告一起复核：

```powershell
go run ./cmd/office-read-dual-report -audit .\authorized-dual-report.json -required-formats doc,xls,ppt -max-authorized-report-age=720h -release-evidence .\office-read-release-evidence.json -enforce-audit -enforce-release-evidence
```

工具会重新计算报告摘要、拒绝未知字段/重复 JSON 键、非法审核人、重复或缺失的门禁、非 `completed` 状态、无效时间和格式范围不一致。每项审核时间还必须不早于所绑定双读报告、也不得晚于回执 `created_at`；一旦指定 `-max-authorized-report-age`，报告和所有回执时间均必须落在相同的时效窗口中，不能把旧报告与新写的回执拼接为“新鲜”证据。`-enforce-release-evidence` 仅要求“机械双读审计通过且回执在结构上完整”；它**不**宣称或自动批准人工审阅结论，真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍必须在工作区外实际完成。历史 fixture 报告仍不能作为发布证据。

```powershell
go test -race ./cmd/office-read-dual-report -count=1 -timeout 180s
go vet ./cmd/office-read-dual-report
git diff --check
```

### 双读审计时间点与调用方输入隔离（2026-08-10）

继续 review 发布审计 CLI 后，发现同一次 `-audit` 原先分别读取系统时间来计算量化报告时效和人工回执时效；在恰好跨越时效阈值或允许时钟偏差边界时，两项结论理论上可能基于不同“当前时间”。现在该调用在入口只取得一次 UTC 时间，并将同一时间点传给量化审计和回执审计，保证同一次输出的时效判定一致。另补充回归确认 `auditReportWithOptions` 只规范化其自身输出，不会原地修改调用方提供的格式切片；重复、别名和不支持格式仍被稳定拒绝。

```powershell
go test -race ./cmd/office-read-dual-report -count=1 -timeout 180s
go vet ./cmd/office-read-dual-report
git diff --check
```

这仅提高仓库内审计确定性与 API 调用隔离，不将 fixture 或结构完整的人工回执视为发布批准；真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是仓库外门禁。

### 双读审计必需格式的 fail-closed 解析（2026-08-10）

继续 review `-required-formats` 后，发现旧解析会静默丢弃不支持格式或重复项；例如操作者误写 `doc,pdf` 时，审计仍可能只对 `doc` 执行，从而缩小预期发布范围。现在只要显式参数含不支持、空项或重复/别名格式，命令即在审计前以非零退出拒绝；合法 `.XLS,doc` 仍被规范化为排序后的 `doc,xls`。未传入参数时，审计也保留报告中的原始格式标签交由不可信输入校验，不会在 CLI 层先去重而掩盖报告自身的重复声明。

```powershell
go test -race ./cmd/office-read-dual-report -count=1 -timeout 180s
go vet ./cmd/office-read-dual-report
git diff --check
```

这只避免操作错误或手工编辑悄然降低机械审计范围；它不扩大默认 `.ppt` 灰度范围，也不替代真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测或格式级回滚留痕。

### 发布前默认范围与回退契约复验（2026-08-10）

在继续等待真实授权样本前，重新按第 1/2/3/6/7 节交叉运行了仓库内可复现的契约回归：默认配置仍只允许 `.ppt` 使用 OfficeRead；显式 `legacy` 会关闭该默认路径；环境变量覆盖与持久化配置优先级、格式级回滚导致的分页缓存隔离、并发分页协议、自动注入预算、危险/加密容器的 fail-closed、知识库富内容的显式 opt-in 与 dual 模式关闭，以及 XLSX/CSV 结构化索引的私有快照均保持既有边界。GUI 侧同时验证了配置字段规范化、格式级回滚演练的仓库内部分和任务工作区 `read_document` 转发。

```powershell
go test -race ./corelib/agent -run 'Test(OfficeReadSettings_DefaultsToLegacyPPTOnly|OfficeReadSettings_ExplicitLegacyDisablesDefaultPPT|OfficeReadSettings_UsesPersistedConfigUnlessEnvironmentOverrides|OfficeReadSettings_PanickingConfigProviderFallsBackToConservativeDefaults|ToolReadDocument_DefaultsToOfficeReadForPPT|ToolReadDocument_FormatLevelOfficeReadRollbackInvalidatesCache|ToolReadDocument_ConcurrentPagingStaysWithinContract|ExpandUserSelectedFilePaths_PPTUsesOfficeReadDefaultRoute|ExtractOfficeTextWithEngine_RejectsUnsafeOOXMLBeforeOfficeRead|ExtractOfficeTextWithEngine_RejectsOversizedOfficeReadInput)' -count=1 -timeout 240s
go test -race ./corelib/knowledge -run 'Test(OfficeReadStructuredKnowledgeContentRequiresExplicitOptIn|OfficeReadStructuredKnowledgeContentDoesNotActivateDuringDualSampling|OfficeReadKnowledgeImportPersistsStableEncryptedError|OfficeReadKnowledgeXLSXRefreshRebuildsStructuredIndexFromSnapshot|ImportSpreadsheetSourceV2ParsesPrivateSnapshotAcrossReplacement)' -count=1 -timeout 300s
go test -race ./gui -run 'Test(OfficeReadFormatLevelRollbackDrill|PatchConfigFieldsOfficeReadPolicy|ToolOfficeReadDocumentFromTaskWorkspace)' -count=1 -timeout 360s
go vet ./corelib/agent ./corelib/knowledge
```

前三条测试与前两个 `vet` 包通过。`go vet ./gui` 仍因现有、非 OfficeRead 的不可达代码诊断而非零退出（`gui/coding_subagent_project_path.go:69` 及若干 `gui/app_maclaw_app_*_test.go`），故没有把它计为完整 GUI 静态检查通过，也没有修改无关工作树文件来掩盖该问题。以上只是仓库内契约复验；它不替代真实 `internal_authorized` 双读报告、文本/业务质量人工复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕，默认主路径范围仍不得扩大。

### 发布回执模板的无效时间源清理（2026-08-10）

复核回执模板生成路径后，发现模板函数虽然有意保持 `created_at` 为空，却仍接收未使用的当前时间参数；这会误导后续维护者以为模板生成时间属于发布证据。现已移除该参数，使 API 明确表达“模板不携带任何时间结论”。同时写入路径在写入失败时立即关闭刚创建的文件，避免句柄延迟到函数返回时才释放。模板仍使用 `O_EXCL`，绝不覆盖已存在回执；实际审核完成后的 `created_at` 与各 `reviewed_at` 仍需由发布负责人填写并经审计校验。

```powershell
go test -race ./cmd/office-read-dual-report -count=1 -timeout 180s
go vet ./cmd/office-read-dual-report
git diff --check
```

补充复核显式 `ExtractOfficeTextWithFormat` 后，确认该入口也先保留对原路径的版本化预检：若预检窗口内发生替换，稳定返回 `source_changed`；通过后才创建私有副本，并在副本上再次执行解析前预检。这样既保留显式 API 的 fail-closed 语义，又保证正常解析不再重开可变原路径。完整 `corelib/agent` race 测试与 `go vet` 已通过；`git diff --check` 仅报告既有工作树文件的换行符转换警告，未报告空白错误。

继续检查 `importSpreadsheetSourceV2` 的直接调用后，发现正常导入/刷新路径已传入 import-owned 快照，但数据库修复、迁移或后续新增的直接调用仍可能仅在 live 路径预检后由 `ReadAllSheets` 重新打开文件。该函数现统一为 CSV 创建有界快照、为 XLS/XLSX 创建经 Office 容器预检的快照，再在该副本上完成结构化行、单元格、卡片和事实索引；即使上层已传入快照，也会生成短生命周期子快照，使公开性较低的直接 API 不依赖调用方正确履约。新增 CSV 回归会在快照生成后替换 live 文件，并断言 `kb_rows` 只含已验证版本内容。此项仍不构成仓库外发布门禁的替代证据：真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕必须独立完成。

```powershell
go test -race ./corelib/knowledge -run 'Test(ImportSpreadsheetSourceV2(CreatesRowsAndCells|RejectsOversizedCSVBeforeSourceWrite|ParsesPrivateSnapshotAcrossReplacement)|NewSQLiteStore(MigratesLegacySpreadsheetSource|MigratesLegacyXLSXFromVerifiedSnapshot|MigratesLegacySpreadsheetNodesDegraded|DoesNotReindexReplacedLegacySpreadsheetSource))' -count=1 -timeout 300s
go vet ./corelib/knowledge
git diff --check
```

该修复只统一仓库内文件版本与 fail-closed 边界；不改变默认仅 `.ppt` 启用 OfficeRead、加密文件拒绝策略、密码不解密
策略、双读返回 legacy 的契约，也不替代真实授权语料、人工质量复核、P1/P2 资源审阅、GUI 烟测和格式级回滚留痕。

### 知识库 Office 节点解析的私有快照（2026-08-10）

知识库的 `ParseDocumentNodes` 同时服务普通 legacy 导入与受控 StructuredMarkdown。此前它只在原路径执行
Office ZIP/OLE 预检，随后各 legacy/OfficeRead 节点解析器重新打开原文件；因此外部替换可让预检版本与入库正文
版本分离。现在 agent 层提供只面向 Office 格式的 `SnapshotOfficeReadInput`：从一个已打开 descriptor 在 32 MiB
上限内生成 SHA-256 校验的私有副本、在该副本完成预检，并返回受调用方控制的清理函数。知识库的 Office 节点解析
统一在该副本上完成，普通 `ParseDocumentNodes` 与导入专用 rich extraction 均适用；复制或预检期间检测到变化即稳定
fail-closed，不把替换后的正文持久化为已验证文档。

富内容 API 自身仍维持既有的子快照与超时 worker 生命周期；本项不会把私有临时路径写入节点、日志、元数据或错误，
也不改变图片资产处理、OfficeRead 灰度、加密拒绝或 legacy fallback 契约。新增 agent 回归验证受替换后的原路径不会
影响私有快照正文且 cleanup 删除快照；知识库定向竞态组合已通过：

```powershell
go test -race ./corelib/agent ./corelib/knowledge -run 'Test(SnapshotOfficeReadInput|OfficeRead|ParseDocumentNodes|ParseOfficeRead|ExtractAndProcessDocumentImages)' -count=1 -timeout 300s
git diff --check
```

这只是第 4/6/7 节的仓库内版本一致性加固；真实授权样料质量验证、P1/P2 资源审阅、GUI 烟测与格式级回滚仍为发布门禁。

### 知识库导入、刷新与 legacy 图片阶段的同版本快照（2026-08-10）

复核知识库导入链路后，发现先前的 Office 节点快照在文本解析返回时已被清理；当 rich 消费未启用或未产生图片时，后续
legacy 图片解析会重新打开原路径。如果文件恰在两个阶段之间被替换，同一次导入会将版本 A 的文本与版本 B 的嵌入图片写入
同一 Source。

现在新增只限导入生命周期使用的私有 input handle：它保留已预检、有界的 Office 快照，直至 rich 图片消费或 legacy
图片解析完成后才清理。普通 `ParseDocumentNodes` 仍在函数返回前清理；仅普通导入与刷新路径在后续图片阶段使用同一快照。
各类提前错误、取消、节点入库失败及表格索引失败分支均会清理快照；快照路径不会写入 Source、节点元数据、日志或错误。

新增确定性回归：在 legacy DOCX 文本解析后、图片解析前替换原文件，断言导入和刷新路径的文本与图片均仍来自同一个已验证
快照，并验证 cleanup。

```powershell
go test -race ./corelib/agent ./corelib/knowledge -run 'Test(SnapshotOfficeReadInput|OfficeRead|ParseDocumentNodes|ExtractAndProcessDocumentImages|LegacyOfficeImageImport)' -count=1 -timeout 300s
git diff --check
```

该收口不改变加密 Office 容器的 fail-closed 策略（不接受密码解密）、默认 `.ppt` 范围、rich 消费开关或 legacy 兼容契约；
真实 `internal_authorized` 样本、人工文本质量复核、P1/P2 资源审阅、GUI 烟测和格式级回滚留痕仍是仓库外发布门禁。

### 知识库 Office 快照与持久化内容摘要一致性（2026-08-10）

继续复核同版本快照后，发现知识库 Source 的 `content_hash` 仍可能在解析前由扫描阶段对原路径计算；若原文随后被替换，
文本节点和 legacy 图片虽已正确固定为快照版本，但持久化摘要可能代表替换后的另一个版本。这会影响后续去重、刷新预览、
版本时间线和检索证据的字节身份解释。

现在导入专用 Office 解析结果在私有、已验证快照上计算摘要，并在普通导入与刷新路径将该摘要写入 Source；非 Office 仍保留
原有扫描/刷新摘要契约。测试覆盖：解析后替换 live 文件时，图片、节点和摘要保持同一快照版本；完整导入后持久化
`content_hash` 与实际解析版本一致。快照路径本身仍不持久化。

后续复核补齐了一个写入顺序边界：导入为了能够持久化解析失败，必须先写入包含扫描期摘要的 Source；此前即使随后获得了
验证快照摘要，在无图片或后续节点/卡片写入不会再次覆盖 Source 的分支中，最终记录仍可能保留旧摘要。现在一旦 Office
快照建立并完成摘要校验，便立即以该摘要覆盖预先写入的 Source；因此带图片、无嵌入图片以及后续节点为空的正常分支都不再
依赖偶然的后续写入来修正身份。

```powershell
go test -race ./corelib/knowledge -run 'TestOfficeRead(ImportPersistsVerifiedSnapshotHash|ImportSnapshotKeepsLegacyTextAndImagesOnOneVersion|RefreshSnapshotKeepsLegacyTextAndImagesOnOneVersion|KnowledgeRefreshRebuildsAndReclaimsImageAssets|KnowledgeRefreshPersistsStableEncryptedError)' -count=1 -timeout 180s
```

本项仅修正知识库字节身份与审计一致性，不改变加密 Office fail-closed、默认 `.ppt` OfficeRead 范围、密码不解密或
灰度/回退契约；真实授权样本、人工质量复核、P1/P2 资源审阅、GUI 烟测和格式级回滚留痕仍是仓库外发布门禁。

### 扫描去重到 Office 重导入之间的版本复核（2026-08-10）

知识库在扫描阶段会先以原路径 SHA-256 做批内/库内去重，随后才开始解析。此前同一路径若已有 Source，重导入会先删除
旧节点；若文件在扫描后被替换，随后 Office 解析可能安全地得到新快照，但该版本没有参与本批扫描的去重和重复判定，且会
破坏先前已索引内容。

现在对于已有 Source 的 Office 重导入，系统会在删除旧派生内容前建立私有快照并将其 SHA-256 与扫描期 `file_hash`
比对。不同则以稳定、内容无关的 `source_changed` 失败记录该导入项，不删除旧 Source、节点或资产；相同则复用该快照
完成文本、legacy 图片和后续索引，避免二次打开 live 路径。错误映射也将 `source_changed` 保持为稳定的知识库持久化错误。

同一检查现也覆盖首次 Office 导入：扫描后的替换版本不能借由“尚无旧 Source”绕过批次去重资格。首次导入发生版本变化时，
系统保留一个无节点的失败 Source 与稳定错误供导入历史追踪，但绝不写入替换版本的文本、图片或摘要。

新增回归覆盖“扫描后、删除旧节点前替换 DOCX”：断言重导入失败但既有 Source 的摘要、状态和节点均保持不变。

```powershell
go test -race ./corelib/knowledge -run 'TestOfficeRead(ReimportRejectsSourceChangedAfterScanBeforeReplacingExisting|ImportPersistsVerifiedSnapshotHash|ImportPersistsVerifiedSnapshotHashWithoutEmbeddedImages|ImportSnapshotKeepsLegacyTextAndImagesOnOneVersion|RefreshSnapshotKeepsLegacyTextAndImagesOnOneVersion)' -count=1 -timeout 180s
```

### XLS/XLSX 失败分支的快照清理与结构化索引一致性（2026-08-10）

继续复核 Office 导入的私有快照生命周期后，发现表格格式的成功路径需要把快照保留到结构化行索引完成；但若文本或富内容解析已经失败，该行索引阶段不会执行。此前该失败分支仅清理非表格格式的快照，因而理论上可使 XLS/XLSX 的临时明文快照滞留至进程退出。现在解析出错即统一关闭 import-owned 快照；成功的 XLS/XLSX 仍只在 `importSpreadsheetSourceV2WithProgress` 结束后关闭，既不改变行索引输入版本，也不扩大明文临时文件生命周期。

新增回归在 XLSX 文本解析完成、结构化索引开始前删除原始 live 文件，并确认导入仍成功写入工作表行；这证明后续表格读取的是同一份已验证私有快照，而非可替换的原始路径。该收紧不改变加密 Office 的 fail-closed / 不接受密码解密策略，也不替代真实授权样本、人工质量复核、P1/P2 资源审阅、GUI 烟测或格式级回滚留痕等发布门禁。

### XLS/XLSX 刷新时的结构化索引原子替换（2026-08-10）

继续审计 `RefreshSource` 后发现，文件刷新已用私有 Office 快照重建 XLS/XLSX 文本节点和 `content_hash`，但没有在同一刷新事务中重建 `kb_tables`、`kb_rows`、`kb_cells` 等结构化表格派生数据；这会留下“新文本节点、旧工作表行”的版本分裂。现在刷新路径会将同一份 import-owned 私有快照传入表格索引器，并把清除旧派生内容、写入新节点、重建表格行/单元格、卡片与版本记录放入同一 SQLite 事务。若表格读取或写入失败，事务回滚，既有节点和表格索引保持不变。

新增回归覆盖刷新首次建立 XLSX 的节点与表格行，并在取得下一次刷新私有快照后删除 live 工作簿，再以该快照完成原子替换，确认写入的是替换版本的表格行、旧行不残留、`content_hash` 仍对应快照且临时文件得到清理。该收紧不改变加密 Office 的 fail-closed / 不接受密码解密策略，也不替代真实授权样本、人工质量复核、P1/P2 资源审阅、GUI 烟测或格式级回滚留痕等发布门禁。

进一步补充了刷新失败的事务回归：在新 XLSX 文本节点已经由私有快照生成后，主动移除该快照以迫使结构化行索引在事务内失败。断言刷新返回稳定的容器安全错误，并且原 Source 的摘要、状态、时间戳、文本节点和 `kb_rows` 均保持先前版本，证明“删除旧派生数据”不会在后续表格阶段失败时泄露为部分刷新结果。

这只收紧仓库内导入一致性和 fail-closed 行为；不改变默认格式范围、加密文件拒绝/密码不解密策略，亦不替代真实授权样本、
人工质量复核、P1/P2 资源审阅、GUI 烟测或格式级回滚留痕等发布门禁。

### CSV 导入与刷新时的结构化索引同版本绑定（2026-08-10）

审计 XLS/XLSX 刷新修复的调用边界时发现，CSV 同样会先经文本解析、再由结构化行索引器按路径重新打开文件；此前刷新只替换文本节点，导入期间若原路径在两个阶段之间被替换，文本和 `kb_tables`/`kb_rows` 也可能来自不同版本。现将 CSV 纳入同一受控快照边界：从已打开文件描述符复制受 32 MiB 限制的私有副本，校验复制期间源文件未变化，再让文本解析、`content_hash` 和结构化索引均消费该副本。刷新时的节点、行/单元格、卡片和版本记录继续在同一 SQLite 事务中替换；扫描后的 CSV 重导入也会先比较扫描摘要和私有快照摘要，变化则以稳定的 `source_changed` 拒绝，保留既有索引。

新增回归在 CSV 文本节点生成后、结构化索引前移除 live 文件，确认导入仍从私有快照成功写入表格行；并覆盖 CSV 刷新重建、快照清理和扫描后替换时不破坏已有 Source/节点。该收紧不改变 OfficeRead 的默认 `.ppt` 灰度范围、Office 加密文件 fail-closed 或不接受密码解密策略，也不替代真实授权样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕等仓库外发布门禁。

```powershell
go test -race ./corelib/agent ./corelib/knowledge -run 'Test(SnapshotOfficeReadInput|KnowledgeCSV|OfficeReadKnowledgeXLSX|ParseDocumentNodes|ImportSpreadsheetSourceV2|RefreshSource)' -count=1 -timeout 300s
git diff --check
```

### V1 表格迁移的历史版本保护（2026-08-10）

继续审计结构化表格的路径读取后，发现 V1→V2 数据库升级会在打开旧库时根据 `Source.URI` 重新读取 XLS/XLSX/CSV，以重建 `kb_tables` 与行级索引；若用户在旧库最后一次导入后替换了同一路径文件，升级可能把历史 `content_hash` 与 document node 保留为版本 A，却把新表格行写成版本 B。现在迁移也先建立受限私有快照并计算其 SHA-256，只在该摘要与旧 Source 的 `content_hash` 一致时才从快照重建结构化索引。文件不存在、读取/预检失败、摘要缺失或不一致时，一律降级从旧 document node 建表，不把替换文件的内容引入历史 Source。

新增 V1 CSV 迁移回归先保存版本 A 的摘要及节点、再在打开数据库前把 live 文件替换为版本 B，断言升级后的 `kb_rows` 仅含原节点内容并带 `migration_degraded` 标记；正常、摘要一致的旧 CSV 和 XLSX 均通过私有快照完成原有精确结构化迁移。该收紧只防止历史索引的跨版本混入，不改变 OfficeRead 默认 `.ppt` 灰度范围、加密 Office fail-closed 或不接受密码解密策略，也不替代真实授权样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕等仓库外发布门禁。

```powershell
go test -race ./corelib/knowledge -run 'TestNewSQLiteStore(MigratesLegacySpreadsheetSource|MigratesLegacyXLSXFromVerifiedSnapshot|MigratesLegacySpreadsheetNodesDegraded|DoesNotReindexReplacedLegacySpreadsheetSource)|TestImportSpreadsheetSourceV2' -count=1 -timeout 240s
git diff --check
```

### 结构化工具与公开抽取 API 的同版本快照（2026-08-10）

继续审计 `read_excel`、`read_pptx` 以及公开兼容抽取函数后，发现这些入口原先都先在用户路径上执行大小/ZIP/OLE 预检，再把同一路径交给 Excel、PPTX、DOCX、DOC、XLS 或 PDF 解析器重新打开；外部进程可在两步之间替换文件，使预检版本与实际 JSON/文本结果不一致。

现在结构化 Excel/PPTX 工具会在解析前取得受限的私有快照：`xls`、`xlsx`、`pptx` 在该快照上执行 Office 容器预检，CSV 则使用同样受大小限制且带源版本核验的快照；所有后续解析只接收快照路径。公开 `ExtractDocxText`、`ExtractDocText`、`ExtractXLSText`、`ExtractPPTXText` 和 `ExtractPDFText` 也采用相同的私有副本，避免这些兼容 API 成为统一文本路由之外的 TOCTOU 旁路。临时路径不会进入工具 JSON、错误响应或诊断，调用结束即清理。

新增回归覆盖 CSV、XLSX 和公开 DOCX API：在快照创建/预检后替换原始文件时，工具或抽取函数仍只返回已验证版本的内容；既有加密容器、输入上限和 JSON 输出上限的 fail-closed 行为保持不变。该收紧不改变默认仅 `.ppt` 启用 OfficeRead 的灰度范围，也不改变加密 Office 拒绝、密码不解密、legacy/dual 兼容契约；真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是仓库外发布门禁。

```powershell
go test -race ./corelib/agent -run 'Test(ToolReadExcelParsesPrivateSnapshotAcrossReplacement|ToolReadExcelParsesVerifiedXLSXSnapshotAcrossReplacement|ExtractDocxTextParsesVerifiedSnapshotAcrossReplacement|StructuredOfficeToolsRejectEncryptedContainersBeforeParsing|ExportedOfficeExtractorsRejectEncryptedContainers)' -count=1 -timeout 180s
go vet ./corelib/agent
git diff --check
```

### GUI race 测试的并发随机源修复（2026-08-10）

在执行第 7 节的全量 GUI race 验证时，`TestLoopContext_ConcurrentAccess` 暴露了测试代码自身的竞争：多个 goroutine 共享同一个 `math/rand.Rand`，而该类型并不支持并发调用。测试现改为每个 worker 使用由 worker 编号派生的独立、可复现随机源，因此 race detector 只验证 `LoopContext` 的并发访问，不再把测试输入生成器的竞争误报为产品问题。

这项测试卫生修复不改变 OfficeRead 的默认仅 `.ppt` 灰度范围、不改变加密 Office 拒绝且不接受密码解密的策略，也不改变普通 timeout 的既有回退契约。真实 `internal_authorized` 样本、人工文本质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是仓库外发布门禁；全量 GUI race 套件在当前 12 分钟执行窗口内尚未完成，不能据此宣称该门禁已通过。

```powershell
go test -race ./gui -run TestLoopContext_ConcurrentAccess -count=1 -timeout 180s
go test -race ./gui -count=1 -timeout 12m
git diff --check
```

### GUI 内存存储首次初始化竞态修复（2026-08-10）

在继续执行第 7 节 GUI race 验证时，复现并修复了 `ensureMemoryStore` 的双重检查锁定问题：首次读取 `memoryStore` 曾发生在 `memoryStoreMu` 保护范围之外，而首次写入发生在锁内，race detector 可将并发首用判定为数据竞争。现已把该检查统一放到互斥锁内；初始化仍在释放锁后执行可能重入配置读取的压缩器配置与记忆演化刷新，因此不改变既有的死锁规避边界。并发回归测试也改为在读取断言对象前持有同一把锁，避免测试本身制造竞争。

已验证：

```powershell
go test -race ./gui -run '^TestEnsureMemoryStoreConcurrentCallsShareOneStore$' -count=1 -timeout 3m
go test -race ./gui -run '^TestLoopContext_ConcurrentAccess$' -count=1 -timeout 3m
go test -race ./corelib/agent ./corelib/knowledge -run 'Test(ExtractOfficeText|ExtractOfficeReadRichContent|Snapshot(BoundedDocumentInput|OfficeReadInput)|ToolRead(Document|Excel)|ParseDocumentNodes|RefreshLightMarkdown|ImportFilesLightMarkdown)' -count=1 -timeout 6m
go test -race ./cmd/office-read-dual-report -count=1 -timeout 3m
go vet ./corelib/agent ./corelib/knowledge
```

此项是第 7 节全量 GUI 并发验证中的独立修复，不改变 OfficeRead 默认仅 `.ppt` 的主路径、加密 Office fail-closed（即使提供密码也不解密）的策略，也不能替代真实 `internal_authorized` 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕等仓库外发布门禁。

### GUI HubCenter 缓存与内存维护管线并发收敛（2026-08-10）

继续执行第 7 节 GUI race 审阅时，发现 HubCenter 选址缓存的内容本身虽然由 `remote.HubCenterSelectionCache` 保护，但 GUI 中缓存和持久化器的惰性指针发布仍可与失败路径的读取/失效并发；这会让竞态检测器报告 `App.hubCenterCache` 的无锁读写。现将该 App 级惰性初始化统一收敛到专用互斥锁，并让解析、下载、市场提交和失败失效路径均先取得已发布缓存再操作。缓存内的 TTL、候选排序、持久化节流和 failover 语义不变；新增并发回归确认所有首次访问者得到同一缓存实例。

同一轮还收敛了 `memPipeline` 的生命周期：定时回调可能正在读取并运行旧管线，而数据目录重置或测试替换管线指针。现在管线指针的发布、读取和清空由独立读写锁保护，实际 `RunOnce` 持有读锁直到返回，替换/停止会先摘除指针再等待运行结束，从而不把停止与执行交叠在同一实例上。相应测试不再直接无锁替换指针，而经同一生命周期入口完成替换。

已验证：

```powershell
go test -race ./gui -run '^TestHubCenterSelectionCacheLazyInitializationIsConcurrentSafe$' -count=10 -timeout 4m
go test -race ./gui -run '^(TestProjectIndexChangeTriggersDebouncedMemoryPipeline|TestTriggerMemoryPipelineWaitsForGlobalQuietPeriod|TestTriggerMemoryPipelineDefersWhilePreviousRunActive|TestMemoryPipelinePanicRecoversAndReschedules|TestForegroundAgent)' -count=3 -timeout 6m
go test -race ./gui -run '^(TestHubCenter|TestGetHubCenter)' -count=5 -timeout 6m
```

一次较宽的 Hub/SkillMarket 名称筛选仍触发了既有的、与本修改无关的 `TestSkillMarketAutoLoginThrottlesFailedMachineLogin` 环境相关失败（断言 machine-login 调用为 1，实际为 0），因此未将其视为全部 Hub/SkillMarket 套件通过。完整 `go test -race ./gui` 先前也在 18 分钟超时，不能据此宣称全量 GUI race 门禁已通过。此项不改变 OfficeRead 默认仅 `.ppt` 灰度、加密 Office fail-closed（即使提供密码也不解密）、dual/legacy 回退或仓库外发布门禁；真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测与格式级回滚留痕仍须独立完成。

### GUI 远程基础设施首次发布与异步 LLM 通知竞态修复（2026-08-10）

全量 GUI race 日志还显示：保存 LLM provider 后异步发起的 Hub 心跳通知，可能恰好与 `ensureRemoteInfra` 的首次初始化重叠。尽管通知此前检查了 `remoteInfraReady`，初始化中的 `remoteSessions` 指针发布与通知读取之间仍没有共同锁，因此 race detector 仍能报告竞争。

现在远程基础设施在初始化期间通过专用读写锁发布，`notifyHubLLMConfigChanged` 及常用 Hub 客户端/远程会话查询都经同一读取入口取得当前已发布 manager。首次初始化未完成时，通知会等待该次发布完成后再判断是否存在已连接 Hub；没有连接时仍为原有的静默 best-effort 行为。此变更不触及 OfficeRead 路由、格式策略、密码处理或解析内容。

已验证：

```powershell
go test -race ./gui -run '^TestSendAIAssistantMessage_RejectsOversizedToolArguments_(OpenAI|Anthropic)$' -count=3 -timeout 8m
git diff --check
```

### GUI 任务工作区的 Office 相对路径隔离修复（2026-08-10）

复核第 7 节 GUI 工具读取契约时发现：一个尚未绑定标签页自定义工作目录的受管任务 owner，会经 `EffectiveWorkingDirForOwner` 继承桌面工作目录。于是 `read_document` 的相对路径可被错误解析到桌面 workspace，而非该任务的 `tasks/<id>/workspace/`。这既会导致任务内文件读取失败，也会破坏任务之间应有的工作目录隔离。

现在受管任务的默认工具目录直接从该任务的执行目录解析（默认为 `workspace/`）；只有该 owner 已显式发布标签页工作目录覆盖时才使用覆盖值。非受管项目、ACP 隔离会话和已有的绝对路径语义均未改变。新增回归同时验证相对路径解析和真实 DOCX 的 `read_document` 调用均读取任务 `workspace/` 中的文件。

已验证：

```powershell
go test -race ./gui -run '^(TestOfficeResolvedPathUsesTaskWorkspace|TestToolOfficeReadDocumentFromTaskWorkspace)$' -count=5 -timeout 6m
go test -race ./gui -run 'Test(OfficeReadFormatLevelRollbackDrill|PatchConfigFieldsOfficeReadPolicy|ToolOfficeReadDocumentFromTaskWorkspace)' -count=1 -timeout 6m
git diff --check
```

该修复只收紧 GUI 内的相对路径路由；不改变 OfficeRead 默认仅 `.ppt` 的灰度范围、加密 Office fail-closed（提供密码也不解密）策略，亦不替代真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测或格式级回滚留痕等仓库外发布门禁。

### GUI Office 工具的 Bot 本地路径边界收紧（2026-08-10）

审计发现 `office` 先前只依赖会话工作目录解析路径，未走 `read_file` / `write_file` 使用的 Bot profile 路径校验。因此，非 `AllowAllDirectories` 的绑定会话可借由 `read_document`、`read_excel`、`read_pptx` 或 `write_excel` 传入外部绝对路径，绕过 profile 的本地目录边界；`office` 也没有被声明为接收隐藏 runtime owner 的工具。

现已将 Office 文件路径统一交给 `resolveFileToolPathForOwner`：相对路径仍相对当前 task / profile 工作目录解析，但规范化后必须通过 `validateAssistantBindingFilePath`。`office` 已加入 runtime-owner 注入合约；显式空 owner 时稳定 fail-closed，不回退到桌面工作目录。新增回归覆盖 profile 内相对 DOCX 可读、外部绝对 DOCX 和外部 `write_excel` 被拒绝、`AllowAllDirectories=true` 保留外部绝对路径语义，以及显式空 runtime owner 拒绝。

该收紧不改变 OfficeRead 默认仅 `.ppt` 的灰度范围，也不改变加密 Office fail-closed（提供密码也不解密）策略。本轮 GUI 定向测试受工作区中既有的 `gui/coding_workbench_worktree.go` 编译错误阻断，尚不能重新执行；核心密码 fail-closed 回归已通过。仓库内测试也不替代真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测或格式级回滚留痕。

### GUI Office 工具路径隔离的回归复验（2026-08-10）

此前发现的 worktree 合并函数签名不一致已恢复：受控 cherry-pick 合并支持可选的声明写集校验，主工作区不干净或 worktree 出现未声明路径时拒绝合并，不再退回文件复制。这样 GUI 包能够重新编译，并使 Office 工具路径隔离回归可执行。

`office` 现在使用与 `read_file` / `write_file` 相同的 owner-scoped 路径解析。绑定 Bot 的相对路径仍定位到其工作目录；不允许全目录访问时，外部绝对路径会在 `read_document`、`read_excel`、`read_pptx` 和 `write_excel` 入口前被拒绝。`office` 也接收隐藏 runtime owner，显式空 owner 稳定 fail-closed，不回退到桌面会话。已通过 worktree 受控合并、未声明路径拒绝、任务工作区、Bot 路径边界、`AllowAllDirectories` 兼容以及空 owner 拒绝的 GUI race 定向回归（`-count=3`）。

同时复验了计划中的核心 agent、knowledge 和 dual-report 契约，均通过；`go vet ./gui` 仍报告既有测试文件的 unreachable-code 诊断。全量 `go test ./gui -count=1 -timeout 10m` 在 10 分钟执行窗口内未完成，故不将其或全量 GUI race 门禁计为通过。以上仓库内证据不替代真实 `internal_authorized` 双读报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕。

### Bot Office 输出路径的符号链接边界（2026-08-10）

继续审计第 6/7 节的本地路径边界时发现，Bot profile 的路径校验会调用允许不存在叶子节点的目录包含关系判断，以支持 `write_excel` 创建新文件。旧的“不存在路径”分支只做词法 `Abs + Clean` 比较；如果 profile 工作目录内部已有符号链接指向外部目录，`linked-output/new.xlsx` 尚不存在时就可能被词法前缀误判为目录内，随后写入会经该符号链接落到外部位置。

现在该分支会从目标向上寻找最深的已存在祖先，解析该祖先及其父链的符号链接后再拼接不存在后缀，再进行允许目录比较。因此既保持不存在输出文件的合法创建语义，也不会让现有符号链接父目录成为 profile 路径边界旁路。新增回归覆盖通用 `IsWithinAllowedDirs` 与真实 `office(action=write_excel)`：工作目录内指向外部的 `linked-output` 无法创建外部 XLSX，测试环境不支持创建符号链接时会明确跳过。

已通过：

```powershell
go test -race ./gui -run '^(TestIsWithinAllowedDirs_RejectsMissingLeafUnderEscapingSymlink|TestToolOfficeRejectsSymlinkedOutputPathOutsideAssistantBinding|TestToolOfficeHonorsAssistantBindingFileBoundary|TestToolOfficeAllowsAssistantBindingAllDirectories|TestToolOfficeRejectsMissingExplicitRuntimeOwner)$' -count=3 -timeout 8m
git diff --check
```

该修复不改变 OfficeRead 默认仅 `.ppt` 的灰度范围、加密 Office 拒绝或“提供密码也不解密”的策略；真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍为仓库外发布门禁。

### 加密 Office 口令参数的 fail-closed 回归（2026-08-10）

比照“密码不解密”的发布策略继续审计工具入口时，确认 Office 工具 schema 本身不声明密码字段，但调用方仍可以向无类型参数 map 添加额外键。新增回归对 `read_document`、`read_excel` 和 `read_pptx` 分别传入加密容器与非空 `password`：三者都必须稳定返回 `error_class=encrypted`，且结果不得回显口令。这给“提供密码后也不解密”增加了入口级可执行证据；解析器不接收、不传递、不记录此类口令。

已验证：

```powershell
go test -race ./corelib/agent -run 'Test(OfficeToolsRejectEncryptedContainersEvenWhenPasswordIsProvided|StructuredOfficeToolsRejectEncryptedContainersBeforeParsing|FormatOfficeReadFailure_EncryptedDoesNotSuggestBypass|ExportedOfficeExtractorsRejectEncryptedContainers)' -count=3 -timeout 6m
go test -race ./corelib/agent -run 'Test(OfficeReadSettings|ExtractOfficeText|ToolReadDocument|StructuredOfficeTools|OfficeToolsRejectEncryptedContainersEvenWhenPasswordIsProvided|ExportedOfficeExtractors)' -count=1 -timeout 8m
go test -race ./corelib/knowledge -run 'Test(OfficeRead|ParseDocumentNodes|ImportSpreadsheetSourceV2|KnowledgeCSV|RefreshSource)' -count=1 -timeout 10m
go test -race ./cmd/office-read-dual-report -count=1 -timeout 4m
git diff --check
```

该回归不改变 OfficeRead 默认仅 `.ppt` 的灰度范围、legacy/dual 兼容或仓库外发布门禁；真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测与格式级回滚留痕仍需独立完成。

宽泛 `TestRemote|TestGetRemote` 筛选还触发 `TestRemoteVirtualRepositoryMigrationCopyCommandStagesOnlyNewDestinations` 的既有时间戳命令字符串断言失败，和本次基础设施发布改动无关；因此未把该宽泛筛选计入通过证据。全量 GUI race 套件仍需在可控资源窗口中重新执行，且不替代真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕。

### GUI Office PDF 与群聊边界审计（2026-08-10）

继续审计 `office(action=generate_pdf)` 及兼容 `generate_pdf` 入口后，确认其 schema 仅接收 `content`、`title`、`doc_type` 和 `phase_id`；实现将渲染结果直接编码为 `[file_base64|...]` 附件返回，不接受 `file_path`、`output_path`、`save_path` 或其他调用方指定的落盘位置。因此它不会绕过 `read_document` / `read_excel` / `read_pptx` / `write_excel` 已使用的 owner-scoped 文件边界，且没有需要复用路径解析器的写入点。

本地 Lansenger 群聊权限继续采取显式授权、默认拒绝的契约：`office` 未列入可用工具，因此无论读写或生成 PDF 均会在工具暴露和执行边界被拒绝。回归将 `office` 加入未列工具拒绝清单，以防后续权限表扩展时意外开放这条可访问本地文件的通道。

定向 GUI race 筛选在当前 64 秒命令窗口内未完成，未记为通过；此前已完成的 Office 路径边界与符号链接回归仍是该变更的可执行覆盖。该审计不改变 OfficeRead 默认仅 `.ppt` 的灰度范围，也不改变加密 Office 容器 fail-closed（即使提供密码也不解密）的策略；真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍为仓库外发布门禁。

### 群聊 Office 拒绝的暴露与执行双层回归（2026-08-10）

为使前述群聊边界不只依赖 `allowsTool` 的单点判断，补充了两层可执行回归：完整 `NewIMMessageHandler` 在已授权知识库的 Lansenger 群聊工具集仍会保留 `memory` 与 `knowledge_search`，但不会暴露 `office`；直接构造的 `office(action=read_document)` 调用即使群策略含本地允许目录，也会在注册 handler 前被执行层以群聊权限拒绝。工具过滤单测同时把 `office` 放入输入清单，确保任何允许目录或网络授权都不会隐式把它加回群聊。

普通定向 GUI 测试已通过：

```powershell
go test ./gui -run '^(TestLansengerGroupPermissionPolicyFiltersToolExposure|TestLansengerGroupPermissionPolicyFailsClosedForUnlistedTools|TestLansengerGroupPermissionPolicyBlocksOfficeExecution|TestNewIMMessageHandlerKeepsAuthorizedKnowledgeSearchForLansengerGroup)$' -count=1 -timeout 2m
```

同名的 race 筛选仍在当前每条命令约 64 秒的执行窗口内超时，未计为通过。该补强不改变 OfficeRead 默认仅 `.ppt` 的灰度范围，也不改变加密 Office 容器 fail-closed（即使提供密码也不解密）的策略；真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍为仓库外发布门禁。

### 阶段 1/4/6 核心契约复验与 GUI 验证状态（2026-08-10）

本轮重新执行了不依赖外部语料的核心回归：OfficeRead 默认范围仍只对 `.ppt` 生效，显式 `legacy` 可关闭默认路径，持久化/环境策略优先级与格式级回滚缓存隔离保持一致；结构化工具与公开抽取 API 对加密容器在即使调用参数携带非空 `password` 时仍返回 `error_class=encrypted`，且错误提示不建议绕过。知识库的 Office 快照、CSV/XLSX 结构化索引、刷新与异常容器分支也在 race 回归中通过。双读报告命令包及其发布证据审计测试通过，`corelib/agent`、`corelib/knowledge`、`cmd/office-read-dual-report` 的 `go vet` 通过。

```powershell
go test -race ./corelib/agent -run 'Test(OfficeReadSettings_DefaultsToLegacyPPTOnly|OfficeReadSettings_ExplicitLegacyDisablesDefaultPPT|OfficeReadSettings_UsesPersistedConfigUnlessEnvironmentOverrides|ToolReadDocument_DefaultsToOfficeReadForPPT|ToolReadDocument_FormatLevelOfficeReadRollbackInvalidatesCache|OfficeToolsRejectEncryptedContainersEvenWhenPasswordIsProvided|StructuredOfficeToolsRejectEncryptedContainersBeforeParsing|FormatOfficeReadFailure_EncryptedDoesNotSuggestBypass|ExportedOfficeExtractorsRejectEncryptedContainers)' -count=1 -timeout 4m
go test -race ./corelib/knowledge -run 'Test(OfficeRead|ParseDocumentNodes|ImportSpreadsheetSourceV2|KnowledgeCSV|RefreshSource)' -count=1 -timeout 8m
go test -race ./cmd/office-read-dual-report -count=1 -timeout 3m
go vet ./corelib/agent ./corelib/knowledge ./cmd/office-read-dual-report
```

前三组测试和核心 `vet` 均通过。`go vet ./gui` 仍只报告既有 `gui/app_maclaw_app_approval_test.go` 与 `gui/app_maclaw_app_hub_test.go` 的 unreachable-code；与 OfficeRead 路由或本轮群聊契约无直接关联，未修改无关测试来掩盖诊断。两次全量 `go test ./gui -count=1 -timeout 20m` 均在当前宿主的单次 64 秒工具窗口被中断，且中断前不产生可归因失败输出；已终止残留的 `go`/`gui.test` 子进程，故全量 GUI 测试和 race 门禁仍不能计为通过。

前端本轮无法运行：宿主 `PATH` 未提供 npm；使用桌面捆绑 Node/pnpm 时，pnpm 的受控依赖安装拒绝未批准的 `esbuild@0.25.12` 构建脚本。未为通过验证修改依赖批准策略或锁文件。该状态不改变默认 `.ppt` 灰度、加密 Office fail-closed（提供密码也不解密）或 legacy/dual 回退策略；真实 `internal_authorized` 样本、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍须在受控发布环境独立完成。

### 前端 OfficeRead 设置验证更正（2026-08-10）

在不改变依赖批准策略或锁文件的前提下，进一步检查现有工作区后确认，`node_modules/@esbuild/win32-x64/esbuild.exe` 已存在；因此可直接使用桌面捆绑的 Node 运行项目内的 Vitest 与 Vite，而无需经由会被受控安装脚本阻断的 pnpm 安装流程。下列设置范围验证已通过：`settingsTabConfig`、`GeneralSettingsPanel` 和 `KnowledgeSettingsPanel` 共 3 个测试文件、56 个用例；Vite 生产构建也成功完成。

```powershell
$taskNode='C:\\Users\\ma139\\.cache\\codex-runtimes\\codex-primary-runtime\\dependencies\\node\\bin'
$env:PATH="$taskNode;$env:PATH"
& 'C:\\Users\\ma139\\.cache\\codex-runtimes\\codex-primary-runtime\\dependencies\\node\\bin\\node.exe' node_modules\\vitest\\vitest.mjs run src/config/__tests__/settingsTabConfig.test.ts src/components/settings/__tests__/GeneralSettingsPanel.test.tsx src/components/settings/__tests__/KnowledgeSettingsPanel.render.test.tsx --maxWorkers=1
& 'C:\\Users\\ma139\\.cache\\codex-runtimes\\codex-primary-runtime\\dependencies\\node\\bin\\node.exe' node_modules\\vite\\bin\\vite.js build
```

构建仍输出既有的主 bundle 大于 1.5 MiB 警告，但没有构建失败；它不构成 OfficeRead 路由或设置变更的失败证据。`gui/frontend/dist/` 未出现在 Git 工作树状态中。以上自动化检查不替代真实 `internal_authorized` 语料的 dual 报告、人工文本与业务问答复核、P1/P2 资源审阅、GUI 手工烟测或格式级回滚留痕；旧段落记录的是当时 pnpm 路径的阻断，现由本节补充可复现的验证结论。

### GUI 格式白名单默认值一致性（2026-08-10）

继续复核阶段 4 的灰度控制时发现一个 UI/运行时语义差异：**当时**抽取层将缺失或空的
`office_read_formats` 解释为保守的 `.ppt` 默认范围；原设置页却把同一状态显示为没有任何
格式被勾选。用户如果据此取消最后一个勾选项，界面会暗示 OfficeRead 已关闭，实际仍会处理
`.ppt`，这不利于格式级回滚的可预期性。

设置页当时将空白列表显示为 `.ppt`，并禁止从格式复选框移除最后一个已选格式；当用户希望
对所有格式停用 OfficeRead 时，界面明确指向 `office_read_engine=legacy` 这一全局 kill switch。
从多格式范围移除 `.ppt` 仍可正常保存剩余格式，不会阻碍格式级回滚。前端回归覆盖空列表默认
渲染、最后一项不可移除，以及多格式缩小范围；后端的配置规范化和持久化回滚演练也同时复验通过：

```powershell
go test ./gui -run '^(TestPatchConfigFieldsOfficeReadPolicy|TestOfficeReadFormatLevelRollbackDrill|TestFilterSettingsTabConfigGeneralIncludesOfficeReadPolicy)$' -count=1 -timeout 2m
cd gui/frontend
& 'C:\\Users\\ma139\\.cache\\codex-runtimes\\codex-primary-runtime\\dependencies\\node\\bin\\node.exe' node_modules\\vitest\\vitest.mjs run src/components/settings/__tests__/GeneralSettingsPanel.test.tsx --maxWorkers=1
```

该修正当时只让持久化白名单的显示和运行时默认路由保持一致；不扩大默认 `.ppt` 范围、不变更
加密 Office 的 fail-closed 策略，也不替代真实 `internal_authorized` dual 报告、人工质量复核、
P1/P2 资源审阅、GUI 手工烟测或格式级实际回滚留痕。

### 六格式默认启用与历史配置迁移（2026-08-10）

根据当前产品决策，`.doc`、`.docx`、`.ppt`、`.pptx`、`.xls` 和 `.xlsx` 均默认使用
OfficeRead，`office_read_fallback=true` 保持开启。本文此前所有“默认仅 `.ppt`”的实施记录
仅描述当时的灰度状态；如与本节冲突，均以本节为准。

为避免把用户已经执行的格式级回滚重新扩大，配置新增持久化迁移标记
`office_read_scope_migrated`：仅未带该标记、且仍为历史 `.ppt` 单格式白名单的安装会在首次
加载时升级为六格式并写回；已有其他非空部分白名单保持原样。`office_read_engine=legacy`
仍是全局回滚开关，`office_read_formats` 仍可逐格式缩小范围。空白格式范围在运行时和设置页
都表示六格式默认范围，设置页禁止移除最后一项；如需完全关闭 OfficeRead，必须选择
`legacy`。

加密 Office 的策略没有放宽：即使调用参数提供密码，也不会接收、传递、记录或尝试解密；
`encrypted`、损坏和资源超限容器继续 fail-closed，且不走 legacy 回退。聊天附件仍只自动注入
纯文本，不会注入 OfficeRead 的 Markdown 或图片。

本轮仓库内验证覆盖六格式默认路由、自动附件注入、历史配置升级与后续部分白名单持久化、
设置页空范围显示，以及双读报告在无环境白名单时的六格式范围。仍不能将自动化测试视为
发布验收：真实 `internal_authorized` 语料的 dual 报告、人工质量复核、P1/P2 资源审阅、GUI
手工烟测和实际格式级回滚留痕须在发布环境独立完成。

六格式默认化后的完整 `corelib/agent` 竞态套件也已复验通过。复验过程中收紧了 legacy
Office 后缀的容器判定：声明为 `.doc/.xls/.ppt` 的非 OLE、非 OOXML 内容不再可能被
OfficeRead 当作普通文本成功返回，而是稳定归类为 `malformed` 并阻止解析器/恢复工具重开。
相应的自动附件测试改用有效的最小 OLE 容器来模拟普通解析错误，确保“容器安全拒绝”和
“解析错误脱敏”两条契约分别受测。

同轮还复核了错扩展名兼容性：真实 `%PDF` 签名文件即使使用 `.doc` 后缀，也会先按 PDF
签名进入既有 PDF 路由，而不会被六格式 Office 预检误报为 `malformed`；反之，任意非 OLE、
非 OOXML、非 PDF 的 `.doc/.xls/.ppt` 仍稳定 fail-closed。新增回归同时锁定这两个方向，避免
安全收紧破坏既有的内容签名路由。

### OOXML 主文档部件预检与当前复验（2026-08-10）

继续按第 6 节审查 ZIP 容器时，进一步收紧了 OOXML 的身份判断。此前 ZIP 只要含有
`word/`、`xl/` 或 `ppt/` 任一顶级目录，即可通过容器预检；任意 ZIP 都能伪造该目录，仍可能
抵达 OfficeRead 或 legacy 回退。现在每个识别到的文档族还必须包含对应主文档部件：
`word/document.xml`、`xl/workbook.xml` 或 `ppt/presentation.xml`。缺失部件的 ZIP 稳定归类为
`malformed`，在解析器和 fallback 之前拒绝；加密条目仍优先返回更准确的 `encrypted`。回归同时
保留了合法目录项和同族嵌入式 OOXML 的兼容性。

本轮工作区内复验完成：

```powershell
go test -race ./corelib/agent -count=1 -timeout 12m
go test -race ./corelib/knowledge -run 'Test(OfficeRead|ParseDocumentNodes|ImportSpreadsheetSourceV2|RefreshSource)' -count=1 -timeout 10m
go test -race ./cmd/office-read-dual-report -count=1 -timeout 5m
go test ./gui -run '^(TestOfficeReadFullScopeMigration|TestPatchConfigFieldsOfficeReadPolicy|TestOfficeReadFormatLevelRollbackDrill|TestFilterSettingsTabConfigGeneralIncludesOfficeReadPolicy)$' -count=1 -timeout 3m
go vet ./corelib/agent ./corelib ./corelib/knowledge ./cmd/office-read-dual-report
git diff --check
```

设置页相关前端回归也已重跑：3 个测试文件、58 个用例全部通过，Vite 生产构建成功；仅有既有的
主 bundle 超过 1.5 MiB 提示。完整 `go test ./gui -count=1 -timeout 12m` 在本宿主超过 6 分钟仍未
结束且未产生可归因的失败输出，因此不计为全量 GUI 通过。

仓库现有 6 份 PPTX 宣传材料可用于工具链级 dual 对比，但不属于发布受权样本。以
`MACLAW_OFFICE_READ_ENGINE=dual`、`MACLAW_OFFICE_READ_FORMATS=pptx` 运行
`office-read-dual-report` 后，OfficeRead 与 legacy 均为 6/6 成功，OfficeRead 相对 legacy 的
token 覆盖率为 `0.8358`，低于默认 `0.95` 阈值，且来源为 `fixture`；因此报告明确为失败，不能被
误用为发布验收。六格式真实 `internal_authorized` dual 报告、人工质量复核、P1/P2 资源审阅、GUI
手工烟测和实际格式级回滚留痕依然必须在受控发布环境完成。

### 真实 OfficeRead 现代格式与 BIFF 主路径契约（2026-08-10）

继续复核阶段 3 的 OOXML 覆盖时，发现仓库内真实 OfficeRead 调用此前只有 DOCX 的最小生产路径
回归；XLSX/PPTX 的多数适配层验证使用测试 seam，无法证明固定的 OfficeRead 依赖本身能经 MaClaw 的
私有快照、容器预检和适配器读取这两个格式。

现补充最小但真实的 XLSX/PPTX OOXML fixture，并分别通过未替换的 `officeread.Extract` 运行：
文本主路径验证三种 OOXML 均能以正确格式返回关键正文；富内容路径验证三种格式均产生同格式的
`StructuredMarkdown`。同时增加有效 CFBF 内最小 BIFF `Workbook` 的真实 XLS 主路径回归。测试仍不把
Markdown 或图片注入聊天上下文，只确认其受控知识库/预览消费边界的实际依赖调用。

DOC/PPT 不能由任意 ASCII 放入 CFBF stream 来伪造可读文档：Word 需要完整 FIB/CLX 主故事图，PowerPoint
需要完整 record tree；这类“最小”伪文件会被 OfficeRead 正确视为无正文。故不以假 fixture 声称 DOC/PPT
质量覆盖，仍由受权真实语料的双读、人工顺序/问答复核和资源审阅完成。该边界也明确了仓库内测试可证明
路由与安全，不能替代传统格式兼容性验收。

```powershell
go test -race ./corelib/agent -count=1 -timeout 12m
go test -race ./corelib/knowledge -run 'Test(OfficeRead|ParseDocumentNodes|ImportSpreadsheetSourceV2|RefreshSource)' -count=1 -timeout 10m
go test -race ./cmd/office-read-dual-report -count=1 -timeout 5m
go vet ./corelib/agent ./corelib ./corelib/knowledge ./cmd/office-read-dual-report
git diff --check
```

以上均已通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和
格式级回滚留痕仍是仓库外发布门禁。

### 通用签名路由与显式 OOXML 身份绑定的分层（2026-08-10）

交叉复核 `ExtractOfficeText`（通用入口）和 `ExtractOfficeTextWithFormat`、`read_excel`、`read_pptx`
（显式格式入口）后，发现共享预检新增的家族绑定若直接用于通用入口，会把真实 OOXML 的错扩展名兼容误判为
`format_mismatch`：例如实际 PPTX 命名为 `.docx`。这与文件选择、附件上传和 `read_document` 已承诺的按内容签名
自动路由相冲突。

现将两类语义明确分层：通用入口先以**无期待家族**的共享容器预检确认 ZIP/OLE 安全，再以主文档部件签名路由至
真实 `docx/xlsx/pptx`；显式入口及结构化工具继续以声明格式绑定 `.docx → word`、`.xlsx → xl`、`.pptx → ppt`，
并在不匹配时 fail-closed。加密/损坏容器仍在签名路由前拒绝，且保留按原声明格式记录的脱敏 rollout/resource 观测。

新增三向回归覆盖 DOCX/PPTX/XLSX 互相错扩展名时通用入口都能路由到真实格式；原有显式格式拒绝、结构化
`read_excel ← PPTX` 和 `read_pptx ← XLSX` 拒绝回归保持通过。

```powershell
go test -race ./corelib/agent -count=1 -timeout 12m
go test -race ./corelib/knowledge -run 'Test(OfficeRead|ParseDocumentNodes|ImportSpreadsheetSourceV2|RefreshSource)' -count=1 -timeout 10m
go test -race ./cmd/office-read-dual-report -count=1 -timeout 5m
go test ./gui -run '^(TestOfficeReadFullScopeMigration|TestPatchConfigFieldsOfficeReadPolicy|TestOfficeReadFormatLevelRollbackDrill|TestFilterSettingsTabConfigGeneralIncludesOfficeReadPolicy)$' -count=1 -timeout 3m
go vet ./corelib/agent ./corelib ./corelib/knowledge ./cmd/office-read-dual-report
git diff --check
```

以上均已通过。六格式真实 `internal_authorized` dual 报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和实际
格式级回滚留痕仍是仓库外发布门禁。

### 显式格式入口的 OOXML 身份绑定（2026-08-10）

### 结构化读取器的 OOXML 家族绑定（2026-08-10）

继续审计 `read_excel` 与 `read_pptx` 后，发现它们虽会建立经验证的私有快照，但预检此前只确认 ZIP 含有
有效 OOXML 主文档部件。也就是说，`deck.xlsx` 携带 PPTX 或 `book.pptx` 携带 XLSX 时，仍可能交给不匹配的
结构化解析器并返回普通 `extract_error`；这既违背显式格式 API 的身份绑定，也会错误建议模型重开同一错标容器。

现在共享预检将显式 OOXML 格式绑定到对应家族：`.docx → word`、`.xlsx → xl`、`.pptx → ppt`。不匹配时返回
稳定的 `format_mismatch`；工具边界将其归类为 `error_class=malformed` 并保持 fail-closed。通用
`read_document` / `ExtractOfficeText` 的按签名自动路由语义不变。新增 `read_excel ← PPTX` 与
`read_pptx ← XLSX` 双向回归，证明错误家族不会到达结构化解析器。

```powershell
go test -race ./corelib/agent -run 'Test(StructuredOfficeToolsRejectMismatchedOOXMLFamily|ExtractOfficeTextWithFormat_RejectsMismatchedOOXMLBeforeParser|OfficeReadSettings_DefaultsToAllSupportedFormats)$' -count=1 -timeout 3m
git diff --check
```

以上定向回归已通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和
实际格式级回滚留痕仍是仓库外发布门禁。

`ExtractOfficeText` 会按 ZIP 内容签名将错扩展名 OOXML 路由到真实格式，兼容用户从文件选择或
附件入口上传 `DOCX` 却使用 `.doc` 后缀的历史行为。继续复核另一个公开入口
`ExtractOfficeTextWithFormat` 后，发现它接收调用方声明的格式、此前却没有对该声明做同样的
身份绑定；例如真实 PPTX 可以作为 `docx` 调用，造成返回格式、分页缓存键和下游元数据与实际
容器不一致。

现在该**显式格式**入口在完成私有快照和共享容器预检后，对可靠的 ZIP/OOXML 签名进行一致性
检查。若调用方声明格式与 `word/document.xml`、`xl/workbook.xml` 或 `ppt/presentation.xml`
识别的格式不一致，则在 OfficeRead、legacy 和 fallback 之前返回稳定的 `format_mismatch`。OLE
仍按显式扩展名处理，因为共同 OLE 魔数不足以安全区分 DOC/XLS/PPT；通用入口的签名自动路由
语义也不受影响。新增回归覆盖将 PPTX 显式声明为 DOCX 时没有解析器被调用。

该变更后的核心回归、定向知识库/dual-report/GUI 配置回归、`go vet` 及 `git diff --check` 均通过；
外部发布门禁不因此解除。

### 结构化工具的扩展名路由收紧（2026-08-10）

继续审计 `read_excel` 和 `read_pptx` 发现，它们此前只在目标 Office 格式上执行专用预检。因此，对于与工具不对应的后缀（例如 `read_excel` 收到 `.pptx`），调用仍可能进入不相关的解析器并产生普通的恢复建议。这让结构化工具成为了一个意外的跨格式探测通道，与 `read_document` 才拥有的签名自动路由语义不一致。

现已收紧为：`read_excel` 只接受 `.xls` / `.xlsx` / `.csv`，`read_pptx` 只接受 `.pptx`。其他后缀在创建私有快照之前即返回 `error_class=malformed`，不建议 `craft_tool`、COM 或 LibreOffice 绕过。旧 `.ppt` 仍可以使用六格式文本入口 `read_document`；不提供 PPTX 结构化 JSON。新增了 `read_excel ← .pptx`、`read_pptx ← .xlsx` 和 `read_pptx ← .ppt` 的 fail-closed 回归，同时覆盖“不能给出绕过建议”。

```powershell
go test -race ./corelib/agent -run 'Test(StructuredOfficeToolsRejectUnsupportedExtensionsBeforeSnapshot|StructuredOfficeToolsRejectMismatchedOOXMLFamily|StructuredOfficeToolsRejectEncryptedContainersBeforeParsing|OfficeToolsRejectEncryptedContainersEvenWhenPasswordIsProvided|ToolReadPPTX_LegacyPPTFailsClosed)' -count=1 -timeout 5m
```

上述定向回归已通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和实际格式级回滚留痕仍是仓库外发布门禁。

### 结构化工具契约说明同步（2026-08-10）

同步复核发现，上一轮对 `read_excel` / `read_pptx` 的扩展名收紧已落在执行层，但给模型的两处工具契约仍没有表达这个边界。这会引导调用方将非表格或非 PPTX 文档发给结构化读取器，然后只在运行时收到 fail-closed 响应。

现已更新 Core Agent `read_pptx` 工具描述、GUI `office` 注册描述和 `max_slides` 参数说明：明确 `read_excel` 仅接受 `.xls/.xlsx/.csv`，`read_pptx` 仅接受 `.pptx`，其他六格式的纯文本读取必须使用 `read_document`。新增 GUI registry 契约测试，使描述无法再与执行边界发生漂移。

```powershell
go test ./gui -run '^(TestBuiltinOfficeRegistryDescribesStructuredFormatBoundaries)$' -count=1 -timeout 3m
go test ./corelib/agent -run '^(TestRegisterCoreTools_DescribesStructuredOfficeFormatBoundaries)$' -count=1 -timeout 3m
go test -race ./corelib/agent -run 'Test(StructuredOfficeToolsRejectUnsupportedExtensionsBeforeSnapshot|StructuredOfficeToolsRejectMismatchedOOXMLFamily|StructuredOfficeToolsRejectEncryptedContainersBeforeParsing|OfficeToolsRejectEncryptedContainersEvenWhenPasswordIsProvided|ToolReadPPTX_LegacyPPTFailsClosed)' -count=1 -timeout 5m
```

两组回归已通过。`go vet ./gui` 仍报告既存测试文件的 unreachable-code 诊断，未对无关测试做修改来排除该警告。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是仓库外发布门禁。

### CSV 结构化读取的容器伪装拒绝（2026-08-10）

审计 `read_excel` 的 CSV 分支时发现，它先前只创建了 32 MiB 和版本绑定的私有快照，然后直接交给 CSV 解析器。这会使 OOXML/OLE/PDF 容器若被改名为 `.csv`，可绕过共享容器安全决策或以 CSV 作为恢复路径。

现在 CSV 私有快照必须先经同一份 `ExtractOfficeText` 路由探测：已加密、损坏容器保留其稳定安全错误；可靠识别为 DOCX/XLSX/PPTX/PDF 的负载在 CSV 解析前返回 `format_mismatch`，工具层统一归类为 `error_class=malformed`。因此，规范 CSV 仍使用原有的快照、行数上限和 JSON 上限，而伪装文档不会获得 `craft_tool`、COM 或 LibreOffice 绕过建议。

新增回归覆盖“加密 OOXML → .csv”、“有效 DOCX → .csv”和“PDF → .csv”三条路径：

```powershell
go test -race ./corelib/agent -run 'Test(ToolReadExcelCSVRejectsDisguisedDocumentContainers|ToolReadExcelCapsCSVRowsAndReportsTruncation|StructuredOfficeToolsRejectEncryptedContainersBeforeParsing|StructuredOfficeToolsRejectUnsupportedExtensionsBeforeSnapshot)' -count=1 -timeout 5m
```

上述定向回归已通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是仓库外发布门禁。

### 超限 ZIP 的格式探测资源边界（2026-08-11）

继续审计第 6 节的超限输入路径发现，`ExtractOfficeText` 在发现文件超过 32 MiB 后仍使用常规
`sniffOfficeFormat` 试图改善返回格式。对于 ZIP/OOXML，这会调用 `zip.OpenReader` 并读取完整中央目录；
因此攻击者可以用已超限、带巨量目录项的 ZIP 在本应立即返回 `input_too_large` 的路径上消耗额外内存。

现在超限路径仅使用 `sniffOfficeFormatBounded`：它只读取固定的 8 字节头，因此仍能把改名的大型 PDF
识别为 `pdf`，也会保留 OLE 的扩展名语义；ZIP/OOXML 不再打开中央目录，而是稳定保留调用方后缀并直接返回
`input_too_large`。正常大小 OOXML 继续经过完整共享预检和签名路由，未改变六格式的解析行为。新增回归以无中央目录的
超限 ZIP 本地头验证不发生目录型探测，且结果保持 `docx/input_too_large`。

```powershell
go test -race ./corelib/agent -run 'Test(ExtractOfficeTextRejectsOversizedPDFAfterSignatureRouting|ExtractOfficeTextOversizedOOXMLDoesNotOpenZIPDirectoryForFormatSniffing)' -count=1 -timeout 5m
go vet ./corelib/agent
git diff --check
```

以上验证已通过。真实 `internal_authorized` 六格式双读样本、人工文本顺序和业务问答复核、P1/P2
资源审阅、GUI 手工烟测以及格式级回滚留痕仍是仓库外发布门禁，未在此标记为完成。

### CSV 轻量类型确认优化（2026-08-10）

继续复核发现，上述 CSV 防伪装实现若直接调用 `ExtractOfficeText`，会让每个普通 CSV 在结构化读取前先经历一次完整文本抽取。这会重复消耗 CPU/内存，也可能因全文抽取的文本上限而拒绝一个本可由 `max_rows` 和结构化 JSON 上限安全读取的 CSV。

现在 CSV 私有快照仅执行轻量边界确认：先运行共享 ZIP/OLE 容器预检，随后用签名识别拒绝 DOC/DOCX/XLS/XLSX/PPT/PPTX/PDF。加密或损坏容器仍保留原有稳定安全错误（尤其不把加密误报为格式不匹配）；普通 CSV 则直接交给带 `max_rows` 和 JSON 大小上限的结构化解析器，不再进入全文 OfficeRead 路由。新增回归断言普通 CSV 恰好经过一次轻量探测并正常返回结构化结果，原有加密 OOXML、DOCX、PDF 改名 `.csv` 的拒绝回归保持覆盖。

```powershell
go test -race ./corelib/agent -run 'Test(ToolReadExcelCSVRejectsDisguisedDocumentContainers|ToolReadExcelCSVUsesLightweightSafetyProbe|ToolReadExcelCapsCSVRowsAndReportsTruncation)' -count=1 -timeout 5m
git diff --check
```

上述定向回归已通过；真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是仓库外发布门禁。

### 知识库 CSV 容器伪装边界对齐（2026-08-10）

继续检查阶段 4 的表格导入发现，`read_excel` 已在 CSV 私有快照上通过轻量容器/签名探测拒绝 Office/PDF 改名输入，但知识库的 `ParseDocumentNodes` 与直接结构化表格导入此前只执行尺寸检查。这会让同一份改名 `.csv` 的 DOCX、加密 OOXML 或 PDF 在知识库路径进入 CSV 网格解析，造成入口间安全语义不一致。

现在知识库的 CSV 私有快照复用 `agent.ValidateCSVInput`：先运行 ZIP/OLE 预检，再拒绝可可靠识别的 Office/PDF 签名；加密容器仍优先保留 `encrypted`，而不是降级成 `format_mismatch`。该确认不调用全文 `ExtractOfficeText`，因此普通 CSV 仍直接由现有的行级/表格边界解析，不引入重复全文抽取。新增节点解析及直接 V2 表格导入回归，确保伪装容器在任何知识库写入前失败。

```powershell
go test -race ./corelib/agent -run 'Test(ToolReadExcelCSVRejectsDisguisedDocumentContainers|ToolReadExcelCSVUsesLightweightSafetyProbe)' -count=1 -timeout 5m
go test -race ./corelib/knowledge -run 'Test(ParseDocumentNodesRejectsDocumentContainersDisguisedAsCSV|ImportSpreadsheetSourceV2RejectsDocumentContainerDisguisedAsCSV|ImportSpreadsheetSourceV2RejectsOversizedCSVBeforeSourceWrite)' -count=1 -timeout 5m
git diff --check
```

上述定向回归已通过；此外 `go test -race ./corelib/agent -count=1 -timeout 12m`、知识库相关 race 回归、`go vet ./corelib/agent ./corelib ./corelib/knowledge` 与 `git diff --check` 均通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是仓库外发布门禁。

### 显式 CSV 文本入口的类型绑定（2026-08-10）

继续交叉检查发现，`ExtractOfficeTextWithFormat(file, "csv")` 是公开的显式格式入口，之前仅执行 CSV 尺寸检查；它与 `read_excel` 及知识库 CSV 导入不同，仍可能将 Office/PDF 改名容器交给 CSV parser。通用 `ExtractOfficeText` 仍保留其按签名自动路由语义，但显式 CSV 调用现在复用轻量 `ValidateCSVInput`：先保留 ZIP/OLE 的加密/损坏安全结果，再对可靠 Office/PDF 签名返回 `format_mismatch`，不触发完整 CSV 文本抽取。

新增 DOCX、PDF、加密 OOXML 改名 `.csv` 的公开 API 回归，固定空正文、声明格式仍为 `csv` 以及稳定错误身份：

```powershell
go test -race ./corelib/agent -run 'Test(ExtractOfficeTextWithFormatCSVRejectsDisguisedDocumentContainers|ToolReadExcelCSVRejectsDisguisedDocumentContainers|ToolReadExcelCSVUsesLightweightSafetyProbe)' -count=1 -timeout 5m
git diff --check
```

上述定向回归已通过；`go test -race ./corelib/agent -count=1 -timeout 12m`、知识库相关 race 回归、`go vet ./corelib/agent ./corelib ./corelib/knowledge` 与 `git diff --check` 亦通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是仓库外发布门禁。

### 显式 PDF 导出 API 类型绑定（2026-08-10）

继续盘点公开导出函数发现，`ExtractPDFText` 和 `ExtractOfficeTextWithFormat(file, "pdf")` 虽已有私有快照、32 MiB 上限和 page-count 边界，但此前不会一致地确认显式声明的 PDF 实际是 PDF。与 `ExtractOfficeText` 不同，它们没有通用签名路由职责；因此改名为 `.pdf` 的 DOCX/OLE 或普通字节可能直接进入 GoPDF2。现将二者收敛为显式 PDF 身份绑定：先做 ZIP/OLE 容器预检以优先保留加密/损坏错误，再要求 `%PDF` 签名，否则返回稳定 `format_mismatch`。通用 `ExtractOfficeText` 对错扩展名 Office/PDF 的自动路由语义不变。

新增 DOCX、未加密 OLE、加密 OOXML、普通文本改名 `.pdf` 的直接 API 与显式格式 API 回归，确保正文为空且 PDF parser 不会接收非 PDF：

```powershell
go test -race ./corelib/agent -run 'Test(ExtractPDFTextRejectsNonPDFAfterSnapshot|ExtractOfficeTextWithFormatPDFRejectsNonPDFAfterSnapshot|ExtractPDFTextRejectsOversizedInputBeforeRead|ExtractOfficeText_DocExtensionPDFKeepsSignatureRouting)' -count=1 -timeout 5m
git diff --check
```

上述定向回归已通过；`go test -race ./corelib/agent -count=1 -timeout 12m`、知识库相关 race 回归、`go vet ./corelib/agent ./corelib ./corelib/knowledge` 与 `git diff --check` 亦通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是仓库外发布门禁。

### CSV 私有快照边界去重（2026-08-10）

在三个 CSV 消费入口都完成类型绑定后，继续复核发现知识库节点解析和直接表格导入各自重复了“创建私有 `.csv` 快照后再调用轻量校验”的组合。重复实现会使后续新入口或修复遗漏其中一段。现将该组合收敛为 `agent.SnapshotCSVInput`：它只返回已完成 32 MiB/版本绑定、容器预检和 Office/PDF 签名拒绝的私有路径；失败时清理临时文件并保留 `encrypted`、`malformed` 或 `format_mismatch` 身份。普通 CSV 不经过全文抽取。

`read_excel` 保留本地 test seam，以继续证明其调用轻量探测；知识库两个入口改为直接复用共享快照 API。新增共享 API 回归覆盖 DOCX、PDF、加密 OOXML 伪装 CSV，防止 API 消费者重新打开未验证路径。

### 双读报告与实际运行时策略同源（2026-08-11）

继续审计 `office-read-dual-report` 的发布证据链时，发现该独立命令此前自行读取
`MACLAW_OFFICE_READ_ENGINE` 与 `MACLAW_OFFICE_READ_FORMATS`。而 GUI 实际提取还会合并持久化
配置、环境覆盖、非法引擎降级和非空白名单的 fail-closed 规则；因此在 GUI 配置为 `dual`、没有对应
环境变量的受控运行中，报告可能错误拒绝采集，或在未来策略规则变动后与实际抽取范围漂移。

现在 `corelib/agent` 暴露仅含引擎、排序后格式列表、fallback 和富内容开关的
`CurrentOfficeReadRuntimePolicy` 快照。双读报告直接使用该快照，而不再复制环境变量解析逻辑；畸形
非空白名单仍解析为空范围，`buildReport` 会因声明范围无法匹配而拒绝执行。新增回归使用 host 配置提供
`dual` 与 `.XLS/doc`，确认报告以同一已解析策略接受并规范化 `doc,xls`。这只收紧仓库内证据链，
不将报告自动提升为发布批准。

```powershell
go test -race ./cmd/office-read-dual-report -count=1 -timeout 5m
go test -race ./corelib/agent -run 'TestOfficeReadSettings|TestOfficeRead.*Config' -count=1 -timeout 5m
go vet ./cmd/office-read-dual-report
git diff --check
```

以上验证已通过。真实 `internal_authorized` 六格式双读样本、人工文本顺序和业务问答复核、P1/P2
资源审阅、GUI 手工烟测以及格式级回滚留痕仍是仓库外发布门禁，未在此标记为完成。

### 双读报告输入范围的 fail-closed 收口（2026-08-11）

继续审计双读报告的格式级证据后，发现报告虽然会将未启用格式记录为 `not_dual_enabled`，但其
`Formats` 仅声明当前 allowlist；导入审计会正确将这类 `Files` 识别为 `file_format_not_declared`。
这意味着一个包含宽泛 glob 的报告可被生成，却必然成为自相矛盾、不可审计的发布证据。

现在 `buildReport` 在解析、去重输入路径后、打开任何样本前，要求每个文件扩展名均属于已解析的
有效 dual 范围；范围外文件会以稳定的 `input format "..." is outside effective dual policy` 失败。
这样报告只会保留可被其声明范围审计的样本，范围外文件需要以相应格式显式加入 dual allowlist 后
单独收集。新增回归锁定 `.xls` 输入不可以混入仅 `.doc` 的 dual 报告。

```powershell
go test -race ./cmd/office-read-dual-report -count=1 -timeout 5m
go vet ./cmd/office-read-dual-report
git diff --check
```

以上验证已通过。真实 `internal_authorized` 六格式双读样本、人工文本顺序和业务问答复核、P1/P2
资源审阅、GUI 手工烟测以及格式级回滚留痕仍是仓库外发布门禁，未在此标记为完成。

```powershell
go test -race ./corelib/agent -run 'Test(SnapshotCSVInputRejectsDisguisedDocumentContainers|ToolReadExcelCSVRejectsDisguisedDocumentContainers|ToolReadExcelCSVUsesLightweightSafetyProbe|ExtractOfficeTextWithFormatCSVRejectsDisguisedDocumentContainers)' -count=1 -timeout 5m
go test -race ./corelib/knowledge -run 'Test(ParseDocumentNodesRejectsDocumentContainersDisguisedAsCSV|ImportSpreadsheetSourceV2RejectsDocumentContainerDisguisedAsCSV)' -count=1 -timeout 5m
git diff --check
```

上述定向回归已通过；`go test -race ./corelib/agent -count=1 -timeout 12m`、知识库相关 race 回归、`go vet ./corelib/agent ./corelib ./corelib/knowledge` 与 `git diff --check` 亦通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是仓库外发布门禁。

### 显式纯文本 API 的容器伪装拒绝（2026-08-10）

继续盘点 `ExtractOfficeTextWithFormat` 发现，显式 `csv` 与 `pdf` 分支已经做身份绑定，但
`txt`、`text`、`md`、`markdown` 仍可把改名后的 Office/PDF 内容作为原始字节返回。该函数是显式
格式 API，不承担 `ExtractOfficeText` 的按签名自动路由职责；允许该回退会使调用方绕过六格式
Office 容器预检，并把二进制或 PDF 注入到文本消费链路。

现在这些显式纯文本分支会先做 ZIP/OLE 预检，确保加密或损坏容器保留稳定的安全错误；随后拒绝
任何可靠识别出的 Office/PDF 签名并返回 `format_mismatch`。通用 `ExtractOfficeText` 的错后缀
自动路由，以及普通文本、Markdown 的正常读取语义均保持不变。

新增四种显式纯文本声明分别面对 DOCX、PDF 与加密 OOXML 伪装输入的回归：

```powershell
go test -race ./corelib/agent -run 'TestExtractOfficeTextWithFormatPlainTextRejectsDisguisedDocumentContainers' -count=1 -timeout 5m
go test -race ./corelib/agent -count=1 -timeout 12m
go test -race ./corelib/knowledge -run 'Test(OfficeRead|ParseDocumentNodes|ImportSpreadsheetSourceV2|RefreshSource|KnowledgeCSV)' -count=1 -timeout 10m
go vet ./corelib/agent ./corelib ./corelib/knowledge
git diff --check
```

上述回归均已通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI
手工烟测和格式级回滚留痕仍是仓库外发布门禁。

### 显式 legacy OLE 家族绑定（2026-08-10）

继续审计六格式显式入口发现，OOXML 可通过主文档 part 将 `.docx/.xlsx/.pptx` 绑定到
`word/xl/ppt` 家族；而旧版 OLE 原先只校验容器完整性和加密信号，因此把包含 `WordDocument`、
`Workbook/Book` 或 `PowerPoint Document` 的有效 OLE 改名为另一种 legacy 扩展名时，仍可能进入
错误解析器。通用 `ExtractOfficeText` 仍需保持 OLE 的扩展名路由兼容，但显式格式调用和结构化入口
不应成为跨家族探测通道。

现在 OLE 预检会从目录中识别这三个互斥、顶层应用流；如果调用方显式声明 `.doc/.xls/.ppt` 且流
明确属于另一家族，预检在任何 parser、fallback 或 dual shadow 之前返回 `format_mismatch`。出现多个
互斥流时按不安全容器拒绝；无可识别流的泛用 CFBF 继续保持原有兼容行为。空格式的通用预检不施加
家族要求，所以 `ExtractOfficeText` 的历史 OLE 路由不变。

```powershell
go test -race ./corelib/agent -run 'TestPreflightOfficeReadContainer_(RejectsMismatchedOLEFamilyForExplicitFormat|GenericOLEProbeRetainsExtensionLedRouting)$' -count=1 -timeout 5m
go test -race ./corelib/agent -count=1 -timeout 12m
go test -race ./corelib/knowledge -run 'Test(OfficeRead|ParseDocumentNodes|ImportSpreadsheetSourceV2|RefreshSource|KnowledgeCSV)' -count=1 -timeout 10m
go vet ./corelib/agent ./corelib ./corelib/knowledge
git diff --check
```

上述回归均已通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI
手工烟测和格式级回滚留痕仍是仓库外发布门禁。

### OLE 家族绑定的嵌入对象兼容性（2026-08-10）

复核 legacy OLE 家族绑定后发现，CFBF 可在 `ObjectPool` 等 storage 下嵌入另一 Office 应用的
对象；这些嵌入对象同样可能带有 `WordDocument`、`Workbook` 或 `PowerPoint Document` 流。若把目录中
任意同名流都视为外层文档类型，正常 DOC/XLS/PPT 会被错误地当成多家族容器而拒绝。

现将家族识别限定为 CFBF 根目录下的顶层应用流。嵌入 storage 内的主流仅属于嵌入对象，不影响外层
格式判断；同时仍保留顶层跨家族流的 fail-closed 拒绝。新增三种外层 Office 文档各自嵌入另一 Office
对象的回归，确保它们不会因嵌入 payload 误报 `format_mismatch` 或不安全容器：

```powershell
go test -race ./corelib/agent -run 'TestPreflightOfficeReadContainer_(RejectsMismatchedOLEFamilyForExplicitFormat|GenericOLEProbeRetainsExtensionLedRouting|IgnoresEmbeddedOLEFamilyStreams)$' -count=1 -timeout 5m
go test -race ./corelib/agent -count=1 -timeout 12m
go test -race ./corelib/knowledge -run 'Test(OfficeRead|ParseDocumentNodes|ImportSpreadsheetSourceV2|RefreshSource|KnowledgeCSV)' -count=1 -timeout 10m
go vet ./corelib/agent ./corelib ./corelib/knowledge
git diff --check
```

上述回归均已通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI
手工烟测和格式级回滚留痕仍是仓库外发布门禁。

### OLE 加密信号的嵌入对象作用域（2026-08-10）

进一步复核同一段 OLE 预检发现，家族判断虽已忽略嵌入对象，但 `EncryptedSummary`、
`EncryptedPackage`/`EncryptionInfo`、BIFF `FILEPASS` 与 Word FIB 加密检查此前仍会遍历嵌入
storage。嵌入的受保护 OLE payload 并不意味着外层 DOC/XLS/PPT 自身被加密；据此拒绝外层文件会
损害合法文档的兼容性，也超出本次外层文本抽取的安全判定范围。

现在 OLE 预检只对根目录流应用外层文档的家族与加密信号判断。顶层加密 Office 仍在任何 parser、
fallback 或 dual shadow 前 fail-closed；嵌入对象的加密信号不会误报外层文件加密。新增“未加密
DOC 含带 `EncryptedSummary` 的嵌入 OLE”的回归，并同时复验顶层加密 OLE 拒绝：

```powershell
go test -race ./corelib/agent -run 'TestPreflightOfficeReadContainer_(RejectsEncryptedPackageOLE|IgnoresEmbeddedOLEFamilyStreams|IgnoresEmbeddedOLEEncryptionSignals)$' -count=1 -timeout 5m
go test -race ./corelib/agent -count=1 -timeout 12m
go test -race ./corelib/knowledge -run 'Test(OfficeRead|ParseDocumentNodes|ImportSpreadsheetSourceV2|RefreshSource|KnowledgeCSV)' -count=1 -timeout 10m
go vet ./corelib/agent ./corelib ./corelib/knowledge
git diff --check
```

上述回归均已通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI
手工烟测和格式级回滚留痕仍是仓库外发布门禁。

### legacy DOCX XML part 的解压上限复核（2026-08-10）

继续复核第 6 节的 ZIP 资源边界发现，legacy DOCX fallback 会把正文、页眉和页脚 XML part 读入内存后
再解析。共享 OOXML 预检已限制 ZIP entry 的声明展开大小，但 `readZipFile` 自身此前无上限读取；这会使
未来直接调用或异常 ZIP 元数据情形过度依赖前置调用者，而不是在实际解压点守住边界。

现在 `readZipFile` 在打开 part 前拒绝超过 `maxOfficeReadZIPEntryBytes` 的声明大小，并在读取时使用
`LimitReader(max+1)` 作第二道边界；实际读取超限同样返回稳定的 `unsafe_container`。正常 DOCX 正文、
页眉页脚以及既有通用路由语义不变。

```powershell
go test -race ./corelib/agent -run 'TestReadZipFileRejectsOversizedDeclaredPartBeforeInflation' -count=1 -timeout 5m
go test -race ./corelib/agent -count=1 -timeout 12m
go test -race ./corelib/knowledge -run 'Test(OfficeRead|ParseDocumentNodes|ImportSpreadsheetSourceV2|RefreshSource|KnowledgeCSV)' -count=1 -timeout 10m
go vet ./corelib/agent ./corelib ./corelib/knowledge
git diff --check
```

上述回归均已通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI
手工烟测和格式级回滚留痕仍是仓库外发布门禁。

### GUI Office 配置与路径契约复验（2026-08-10）

复验计划第 2、4、7 节的 GUI 接线：历史仅 `.ppt` 的配置会一次性迁移到默认六格式范围；设置页补丁仍可
将白名单缩小为单格式、再通过 `office_read_engine=legacy` 执行全局回滚；`office` 工具的
`read_document` 继续在任务工作区解析相对路径，并遵守 bot profile 的授权目录边界。格式列表为空仍是
配置定义中的“默认六格式”而不是“禁用所有格式”，因此需要禁用时使用明确的 `legacy` 全局开关或保留至少
一个允许格式的非空列表。

```powershell
go test ./gui -run 'Test(LoadConfigPromotesHistoricPPTOfficeReadScopeOnce|PatchConfigFieldsOfficeReadPolicy|OfficeReadFormatLevelRollbackDrill|ToolOfficeReadDocumentFromTaskWorkspace|ToolOfficeHonorsAssistantBindingFileBoundary|ToolOfficeAllowsAssistantBindingAllDirectories)$' -count=1 -timeout 5m
git diff --check
```

上述 GUI 定向回归已通过。`go vet ./gui` 仍报告既有测试文件
`app_maclaw_app_approval_test.go` 与 `app_maclaw_app_hub_test.go` 的 unreachable-code 诊断；它们未涉及
OfficeRead 代码，本轮不修改无关测试来掩盖该状态。真实 `internal_authorized` 六格式 dual 报告、人工质量
复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是仓库外发布门禁。

### 自动附件注入的文本后缀绕过修复（2026-08-10）

继续审计自动附件注入时发现，`formatAutoExtractedDocument` 在统一解析失败后，会对 `.txt/.md/.json/.xml/.yaml/.log/.csv/.rtf` 做原始读取兼容回退。这个回退原本没有排除已被 Office 安全边界拒绝的容器；另外，通用入口对错名 OOXML/PDF 的自动路由若直接成功，也可把它作为文本后缀附件注入并留下不正确的类型标签。

现已将兼容原始读取限定为只能处理未经可靠签名识别为 Office/PDF 的普通文本。已加密、损坏、超限或格式不匹配的容器保留 fail-closed 结果；即使通用入口能正确路由 OOXML/PDF，当原附件后缀为文本类型时也不会将其注入聊天上下文。正常的 JSON/Markdown/CSV 原始文本兼容路径保留不变。

新增“加密 OOXML → .csv”和“有效 DOCX → .md”自动注入回归，断言正文不会进入对话，且不提供绕过建议：

```powershell
go test -race ./corelib/agent -run 'Test(FormatAutoExtractedDocument_TextLikeSuffixDoesNotBypassRejectedContainer|FormatAutoExtractedDocument_BlockedFailureDoesNotSuggestBypass|FormatAutoExtractedDocument_PlainJSONFallback|ToolReadExcelCSVRejectsDisguisedDocumentContainers)' -count=1 -timeout 5m
```

上述定向回归已通过。真实 `internal_authorized` 六格式 dual 报告、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和格式级回滚留痕仍是仓库外发布门禁。
### 知识库能力声明与六格式接入一致性（2026-08-11）

继续复核六格式接入的用户可见契约后，发现聊天附件和 `read_document` 已默认按配置走
OfficeRead，但知识库能力 API、导入工具说明仍以旧的 native/LibreOffice 叙述为主。特别是
`.ppt` 的知识库正文没有 legacy parser：它只有在 `office_read_engine=officeread` 且显式开启
`office_read_emit_markdown` 时才能持久化 OfficeRead 结构化 Markdown；这不应被解释为聊天
文本读取也不可用。反过来，DOC/DOCX/XLS/XLSX/PPTX 的知识库默认仍保留 native fallback，
富 Markdown 和图片只是显式增强，而非聊天上下文注入。

现在 `knowledge_capabilities` 对六种 Office 格式均明确富内容 opt-in 与 native fallback 的边界；
PPT 额外说明“知识库无 native parser、未开启时保持 pending，但聊天/read_document 可按当前
OfficeRead 纯文本策略读取”。`knowledge_import_directory` 与 `knowledge_import_files` 也同步
列出 `doc/docx/ppt/pptx/xls/xlsx`，移除已失真的“依赖 LibreOffice/soffice”前提，并说明知识库
富内容的选择性。新增回归固定能力声明和导入工具文案，避免后续将知识库的 opt-in 状态误报成
六格式聊天抽取不支持。

```powershell
go test -race ./corelib/knowledge -run 'Test(Capabilities|ParseDocumentNodes|OfficeReadStructuredKnowledge|ParseOfficeReadRichContent)' -count=1 -timeout 5m
go vet ./corelib/knowledge
git diff --check
```

以上知识库定向回归、静态检查和差异检查均已通过。GUI 包的新增文案回归已启动，但完整包在该
工作区的编译/链接耗时超过单次命令通道上限，未将其误记为通过。真实 `internal_authorized`
六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测及格式级回滚留痕仍为仓库外
发布门禁，未在此标记为完成。
### 数字员工附件的六格式闭环（2026-08-11）

继续核对计划第 2、5、7 节的 GUI 附件入口时，发现普通 AI 会话和 `read_document` 已覆盖六种
Office 格式，但数字员工（VE）会话的前端 file picker 与宿主类型白名单只允许 `.docx`（以及 PDF）。
这使用户可在主会话选择 `.doc/.xls/.xlsx/.ppt/.pptx`，却无法通过 VE 对话发送同一份附件；已收到的
非 DOCX Office 文件也只会落到通用 `File` 占位符，缺少它们同属受控 Office 文档的明确信号。

现在 VE 前端选择器、Go 宿主附件分类、50 MiB 文档上限与 MIME 映射均统一覆盖
`.doc/.docx/.xls/.xlsx/.ppt/.pptx`。接收端不会把任何 Office 二进制正文直接注入模型上下文：只持久化
安全本地副本，并传入受控的 `Office document` 占位符和保存路径，后续由共享的 OfficeRead
`read_document` 路径按需读取。这既补齐了 GUI 文件选择/附件发送的六格式闭环，也保持“聊天上下文只消费
文本”的边界。

新增回归覆盖六种格式的 VE 附件分类、MIME 和接收端零正文泄漏；前端回归覆盖附件类别，并通过 TypeScript
检查。以下验证通过：

```powershell
go test ./gui -run 'Test(ClassifyFileType|MimeTypeForFile|ProcessMessageAttachmentsKeepsAllOfficeFormatsOutOfInlineContext)$' -count=1 -timeout 5m
node ./node_modules/vitest/vitest.mjs run src/components/ai/__tests__/VEConversationView.test.tsx --reporter=dot
node ./node_modules/typescript/bin/tsc --noEmit
git diff --check
```

`go vet ./gui` 仍仅被既有无关测试文件中的 unreachable-code 诊断阻断；没有为掩盖该状态修改无关代码。
真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和实际回滚留痕
仍为仓库外发布门禁，未在此标记为完成。

### 知识库 Doctor 的六格式恢复提示（2026-08-11）

知识库导入和 capabilities 已列出六种 Office 格式，但 `knowledge_doctor` 面对真正的未知类型时仍建议用户只转换为
`docx/pdf/xlsx/csv/markdown/txt`。这会让诊断输出与当前六格式导入入口相矛盾，并可能把可用的 legacy
Office 格式误导为必须先转换的格式。

现已将该恢复提示同步为 `DOC/DOCX`、`PPT/PPTX`、`XLS/XLSX`、PDF、CSV、Markdown 和 TXT，并以存储诊断
回归固定六格式术语。该改动只校正文案和诊断契约，不改变 `.ppt` 知识库仍需显式 OfficeRead 富内容 opt-in 的
实际行为，也不放宽加密、损坏或超限文件的 fail-closed 边界。

```powershell
go test -race ./corelib/knowledge -run 'Test(ImportDirectoryAndSearch|Capabilities|ParseDocumentNodes|OfficeReadStructuredKnowledge|ParseOfficeReadRichContent)' -count=1 -timeout 5m
go vet ./corelib/knowledge
git diff --check
```

以上验证通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和
实际回滚留痕仍为仓库外发布门禁，未在此标记为完成。

### MaClawSrv 知识库上传入口与 AgentService 契约补齐（2026-08-11）

继续复核六格式接入的非桌面入口时，发现 MaClawSrv 的用户知识库和管理员公共知识库上传选择器均列出
DOC/DOCX、PPTX、XLS/XLSX 等格式，却遗漏了 `.ppt`。服务端上传后已统一委托知识库导入契约，且该契约的
默认扩展名已包含 `.ppt`；因此这是前端选择限制造成的真实入口缺口，而不是将 PPT 误标为默认富内容导入。

现已在两个 Web 上传选择器中补入 `.ppt`，并将对应静态 Web 契约测试固定为完整六种 Office 格式。与此同时，
桌面 GUI、TUI 共用的核心工具注册、GUI 内置知识库工具和 AgentService 的 `knowledge_import_directory`、
`knowledge_import_files` 描述及 `include_exts` 示例均明确列出 DOC/DOCX、PPT/PPTX、XLS/XLSX；保留 PPT 的
知识库富内容仍须显式启用 OfficeRead Knowledge 的准确说明，避免把“可选择并导入”误表述成“默认以 OfficeRead
结构化富内容持久化”。

```powershell
go test ./corelib/agentservice -run 'TestCoreAgentKnowledgeImportToolsExecuteAgainstStore' -count=1 -timeout 5m
go vet ./corelib/agentservice
go test ./corelib/agent -run 'TestCoreKnowledgeImportToolsAreRegistered' -count=1 -timeout 5m
go vet ./corelib/agent
go test ./gui -run 'TestKnowledgeImportToolsDescribeSixOfficeFormatsWithoutConverterRequirement' -count=1 -timeout 5m
go test ./MaClawSrv -run 'Test(UserWebServesEmbeddedShell|AdminWebServesEmbeddedShell)$' -count=1 -timeout 2m
git diff --check
```

上述 GUI/AgentService/核心工具定向回归、静态检查、MaClawSrv Web 断言与差异检查均已通过。此前 MaClawSrv
Web 测试虽完成断言，却因遗漏 `server.Close()` 使 Windows 上的 `coding_runtime.db` 在 `t.TempDir` 清理阶段被占用；
现已在这两个嵌入式 Web 壳测试中按服务生命周期关闭 HTTP server 和 Service，测试可在断言后正常释放数据库句柄。
`go vet ./gui` 仍被既有、无关的 `app_maclaw_app_approval_test.go` 与 `app_maclaw_app_hub_test.go` 中 unreachable-code
诊断阻断，未为掩盖该状态修改无关文件。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、
GUI 手工烟测和实际回滚留痕仍为仓库外发布门禁。

### 工作流表单与移动文档任务的六格式闭环（2026-08-11）

继续审计工作流表单和 Mobile/桌面任务链路后，发现工作流的共享 `extractTextFromFile` 已实际经由受控的
`ExtractOfficeText` 路径读取 DOC/DOCX/PPT/PPTX/XLS/XLSX，但学术申请模板的“补充材料”白名单和用户可见说明
仍只列出 Word/PDF/Markdown/TXT，导致后端可读格式与选择器的声明不一致。现已把六种 Office 格式同时加入该
模板的补充材料白名单、用户说明和结构回归；简历入口说明也同步为六格式，仍沿用共享的输入大小、加密容器和
格式级回滚边界。

Mobile 文档上传此前仅把 DOCX/XLSX 当作 Hub 的即时解析草稿；DOC、PPT、PPTX、XLS 会被直接标成“原件已保存”，
而且 DOCX/XLSX 还绕过了 GUI 配置的 OfficeRead 策略。现已将六种 Office 文件统一排入已认证桌面 worker 的
`queued` 任务，worker 将下载到私有临时文件后调用共享 `ExtractOfficeText`。因此六格式均遵守与聊天附件、
`read_document` 相同的 OfficeRead/legacy fallback、容器预检和 32 MiB 文本提取上限；无法提取时保留原件，任务
明确失败，不会把二进制内容作为 UTF-8 正文注入。Mobile 现有 PDF、纯文本与图片/OCR 契约保持不变。

```powershell
go test ./corelib/workflow/v2 -run 'TestBuildAcademicApplicationTemplate_AllProfiles' -count=1 -timeout 2m
go test ./gui -run 'TestExtractTextFromFileUsesSharedOfficeRouteForDOCX|TestExtractTextFromFileRejectsOversizedBeforeAnyParser|TestExtractTextFromFileRejectsContainerDisguisedAsPlainText|TestExtractTextFromFileDoesNotFallbackForEncryptedPDF' -count=1 -timeout 2m
go test ./gui -run 'TestMobileDocument(SourceMarkdownParsesTextLikeSource|RequiresOfficeExtractionRecognizesSixFormats|SourceMarkdownUsesSharedOfficeRouteForDOCX)$' -count=1 -timeout 2m
go test ./hub/internal/httpapi -run 'TestMobile(UploadedDOCXDraftMarkdown|UploadedFileNeedsRemoteOfficeExtractionRecognizesSixFormats)$' -count=1 -timeout 2m
git diff --check
```

以上定向回归和差异检查已通过。GUI 的完整 `go vet` 仍只受既有、无关测试中的 unreachable-code 诊断影响；
真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和实际回滚留痕仍为
仓库外发布门禁。

### 知识库检索过滤契约的六格式补齐（2026-08-11）

继续审计知识库的用户入口与 Agent 工具契约时，发现文件导入、解析、刷新与后端能力已实际涵盖 DOC/DOCX/PPT/PPTX/XLS/XLSX，但前端的知识库来源类型候选和可刷新判断仍遗漏 `ppt/pptx`；同时，`knowledge_search`、`knowledge_explain`、`knowledge_context_pack`、分面与事实检索工具的 `source_kinds` schema 只声明 DOCX/XLSX，会误导模型不去按 `doc/ppt/xls` 等实际可过滤类型检索。

现已将前端候选、前端可刷新合同及八个知识库检索工具的 schema 统一为六种 Office 来源类型。这一变更不改变 `.ppt` 富内容仍需显式 OfficeRead Knowledge 灰度的限定，也不改变加密、损坏或超限容器的 fail-closed 边界；它只是让用户、前端和工具 schema 如实反映现有后端能力。

```powershell
go test ./gui -run 'Test(KnowledgeImportToolsDescribeSixOfficeFormatsWithoutConverterRequirement|KnowledgeRecallToolSchemasDescribeAllSixOfficeSourceKinds)$' -count=1 -timeout 5m
node ./node_modules/typescript/bin/tsc --noEmit
node ./node_modules/vitest/vitest.mjs run src/components/settings/__tests__/KnowledgeSettingsPanel.test.ts --reporter=dot
git diff --check
```

以上定向 Go 回归、TypeScript 检查与 57 个前端单测均通过。`go vet ./gui` 仍仅由既有、无关测试文件中的 `unreachable-code` 诊断阻断，未将其计为通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和实际回滚留痕仍为仓库外发布门禁。

### VE 文件中继与 MIME 契约的六格式补齐（2026-08-11）

继续审计远程 VE 对话的附件中继时，发现前端和桌面宿主已可按六种 Office 格式发送和分类附件，但 Hub `FileRelay` 仅显式推断 `.docx` 的 MIME，并仅放行 OpenXML 前缀。因此在浏览器没有提供可用 Content-Type 时，`.doc/.xls/.ppt` 会被误判为 `application/octet-stream` 而被拒绝；`.xlsx/.pptx` 则依赖宽泛的前缀而缺少明确回归契约。

现已将 `FileRelay` 的 MIME 许可和扩展名推断统一覆盖 `.doc/.docx/.xls/.xlsx/.ppt/.pptx`：旧式分别使用 `application/msword`、`application/vnd.ms-excel`、`application/vnd.ms-powerpoint`，OpenXML 分别使用 Word/Spreadsheet/Presentation 的标准 MIME。上传后仍按 20 MiB 文档上限保存，下载时回传原始 MIME；这一更改只修复传输层合法性，不改变接收端“Office 二进制正文不直接注入模型上下文”的边界，后续仍由共享 `read_document`/OfficeRead 路径按需提取。

```powershell
go test ./hub/internal/ve -run 'Test(FileRelay_HandleUpload_InfersAllOfficeFormatsWithoutMultipartMIME|IsAllowedMIME|DetectMIMEType|FileCategory|FileRelay_Upload_Success)$' -count=1 -timeout 2m
git diff --check -- hub/internal/ve/file_relay.go hub/internal/ve/file_relay_test.go
```

以上定向回归和差异检查已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和实际回滚留痕仍为仓库外发布门禁。

### 工作流引导与知识库界面的六格式声明校正（2026-08-11）

继续核对用户可见的工作流引导后，发现共享 `documentParsingGuidance` 已将 `.ppt` 排除在内置 `office` 支持范围外，
并把它指引为 Craft/COM/LibreOffice 的“其他格式”；这与当前 `read_document` 对 DOC/DOCX/PPT/PPTX/XLS/XLSX 的统一
OfficeRead 路由冲突，也会诱导模型绕开现有的格式级回滚和容器安全边界。现已明确 PowerPoint 包含 `.pptx` 与旧 `.ppt`，
并说明 DOC/XLS/PPT 必须先经过 `office`，仅在普通读取失败（而非加密、损坏、资源或版本拒绝）时才可走允许的恢复手段。

学术简历、专利交底书和长江学者评审模板的界面文字此前仍只写 Word/PDF 或 PDF/Word/Markdown；现已同步为
PDF、Word、PowerPoint、Excel（以及简历已有的 Markdown/TXT）。知识库导入弹窗也已将可导入的 `.ppt` 显示为
`PPT/PPTX`，但仍准确保留“`.ppt` 的知识库**富内容**需要 OfficeRead Knowledge 灰度”的限定，不承诺默认富 Markdown。

```powershell
go test ./corelib/workflow/v2 -run 'Test(DocumentParsingGuidanceDeclaresAllSixOfficeFormats|BuildPhasePrompt_InjectsDocParsingGuidance|PatentDisclosureFilePromptsDescribeSixOfficeFormats|BuildAcademicApplicationTemplate_AllProfiles)$' -count=1 -timeout 2m
go vet ./corelib/workflow/v2
node ./node_modules/vitest/vitest.mjs run src/components/ai/__tests__/AgentTaskPanel.test.tsx --reporter=dot
node ./node_modules/vitest/vitest.mjs run src/components/settings/__tests__/knowledgeImportProgress.test.ts --reporter=dot
node ./node_modules/typescript/bin/tsc --noEmit
git diff --check
```

以上定向回归、TypeScript 检查和差异检查均已通过；前端现有测试仍会输出既有的 `act(...)` 警告，未将其误记为本次
OfficeRead 变更。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测和实际
回滚留痕仍为仓库外发布门禁。
### 默认运行策略与历史 PPT 灰度迁移复核（2026-08-11）

继续复核“六种格式均已接入使用”是否只停留在入口声明，而非真正的默认运行时策略。当前
`AppConfig` 的新建默认值为 `officeread` 引擎，并将
`doc/docx/ppt/pptx/xls/xlsx` 全部写入 `office_read_formats`；适配器在缺少持久化配置时也解析为
同一组六格式。因此 `read_document` 的默认路径会对六种 Office 文件调用 OfficeRead，而不是只对
`.ppt` 启用灰度。

为兼容已有用户数据，启动配置加载会一次性把历史的“仅 `.ppt` + OfficeRead”配置提升为完整六格式。
显式窄范围配置和 `legacy` 引擎不会被改写，仍可用于格式级或全局回滚。运行时策略快照会规范化并排序
格式列表，避免由环境变量、持久化配置或重复/大小写输入造成实际路由与审计显示不一致。此次复核未放宽
加密文档的 fail-closed 边界：即使调用参数额外携带密码，也不会交给解析器解密。

以下定向回归与差异检查已通过：

```powershell
go test -race ./corelib/agent -run 'Test(OfficeReadSettings_DefaultsToAllSupportedFormats|ToolReadDocument_DefaultsToOfficeReadForAllSupportedFormats|CurrentOfficeReadRuntimePolicyReturnsCanonicalResolvedSnapshot)$' -count=1 -timeout 5m
go test ./corelib -run 'Test(OfficeReadConfigDefaultsAndRoundTrip|ApplyOfficeReadFullScopeMigration)$' -count=1 -timeout 2m
go test ./gui -run 'TestLoadConfigPromotesHistoricPPTOfficeReadScopeOnce' -count=1 -timeout 5m
git diff --check
```

真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测及真实
格式级回滚留痕仍是仓库外的发布门禁；上述结果仅证明默认路由、迁移和回滚契约在仓库内可复现。

### 本地 IM 文件投递 MIME 的 PowerPoint 补齐（2026-08-11）

继续审计六格式的非 VE 传输路径时，发现微信、QQ、Telegram 和蓝信等本地 IM 网关在把 Agent 生成的本地文件
投递给用户时，共用 `guessMimeFromMedia` 推断上传 MIME。该函数已经识别 DOC/DOCX、XLS/XLSX，
却遗漏 PPT/PPTX；PowerPoint 文件会因此以 `application/octet-stream` 发送。虽然接收方通常仍可按文件名下载，
但这会丢失标准 MIME，影响客户端的文件类型展示、预览/下载处理，并使该条用户可见的文件传递路径与六格式
Office 接入目标不一致。

现已补入 legacy PowerPoint 的 `application/vnd.ms-powerpoint` 和 OpenXML Presentation 的
`application/vnd.openxmlformats-officedocument.presentationml.presentation`。媒体类别仍为通用 `file`，
不将 Office 二进制错误送入图片、语音或模型上下文路径；文本读取仍必须经由受控的 `read_document` /
OfficeRead 路由。这一修复不改变加密 Office 的 fail-closed 或密码不解密策略。

```powershell
go test ./gui -run 'Test(GuessMimeFromMediaRecognizesAllSixOfficeFormats|LansengerMediaTypeForFileName|PrepareVEAttachmentMessageSupportsAllOfficeFormats|MimeTypeForFile|ProcessMessageAttachmentsKeepsAllOfficeFormatsOutOfInlineContext)$' -count=1 -timeout 5m
go test ./hub/internal/ve -run 'Test(FileRelay_HandleUpload_InfersAllOfficeFormatsWithoutMultipartMIME|IsAllowedMIME|DetectMIMEType|FileCategory)$' -count=1 -timeout 2m
git diff --check -- gui/im_gateway_media.go gui/im_gateway_media_test.go
```

上述定向回归和差异检查均已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2
资源审阅、GUI 手工烟测和实际格式级回滚留痕仍为仓库外发布门禁。

### 冲突附件元数据到 OfficeRead 自动抽取的端到端契约（2026-08-11）

文件名规范化的单元验证不能单独证明最终会进入解析器：附件描述、自动注入候选收集、路径后缀判断和 OfficeRead
文本提取之间仍可能发生断链。因此新增真实 OOXML DOCX 端到端契约：远端附件声明为 `type=image`、文件名为
`cover.png`、MIME 为 DOCX 时，系统必须把它保存为 `cover.docx`，不生成视觉 data URL，并在同一轮的附件描述中
通过共享预算自动注入 OfficeRead 提取出的正文。

该测试使用最小可读 DOCX 和默认 OfficeRead 路由，不以伪造 OLE/DOC/PPT 作为文本质量证据；它证明了冲突元数据
修复后“隔离视觉输入”与“实际进入统一读文档路径”两项同时成立。传统 DOC/PPT 的真实授权语料质量门禁仍未替代。

```powershell
go test ./corelib/agent -run 'Test(BuildUserContent(RoutesConflictingOfficeAttachmentIntoRealAutoExtract|RoutesMislabelledAndUnnamedOfficeAttachmentsAwayFromVision|VisionIgnoresOCR|NoVisionWithOCRAppendsText)|NormalizeBinaryDocumentAttachmentFilename)$' -count=1 -timeout 5m
go test ./gui -run 'Test(BuildUserContentRoutesMislabelledBinaryDocumentsAwayFromVision|SaveAttachmentToLocalUsesOfficeMimeSuffixWhenFilenameMissing|ProcessMessageAttachmentsKeeps(MislabelledPDF|AllOfficeFormats|MislabelledOfficeFormats)OutOfInlineContext|PrepareVEAttachmentMessageSupportsAllOfficeFormats|MimeTypeForFile)$' -count=1 -timeout 5m
git diff --check -- corelib/agent/attachment.go corelib/agent/attachment_ocr_test.go gui/im_attachment.go gui/im_attachment_voice_test.go gui/app_ve_attachment.go gui/app_ve_attachment_receive.go gui/app_ve_attachment_receive_test.go
```

上述定向回归和差异检查均已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2
资源审阅、GUI 手工烟测和实际格式级回滚留痕仍为仓库外发布门禁。

### MIME 与伪造图片文件名冲突时的 Office 路由收敛（2026-08-11）

上一轮无文件名 MIME 回退复核后，进一步发现“文件名存在但后缀不属于文档”的冲突场景仍会失效：例如远程网关把
DOCX 传为 `cover.png`，同时声明标准 DOCX MIME。此前该附件会被从视觉分支隔离，却仍以 `.png` 落盘，导致路径式
自动抽取无法识别为文档，既没有视觉输入也进不了 OfficeRead。

现已收敛为统一的 staging 文件名规范化：已有 `.pdf` 或六格式 Office 后缀时，以文件名为准；否则标准 PDF/Office
MIME 会替换无关后缀或补充缺失后缀，然后再落盘。生成的无文件名副本也保留相应后缀。映射仅保证路由身份，绝不把
MIME 当成内容真实性；真正的 PDF/OfficeRead 提取继续检查实际签名、32 MiB 限制、加密和危险容器。GUI 与 corelib
agentservice/coding 子代理共享该规范化逻辑，避免两条附件路径不一致。

```powershell
go test ./corelib/agent -run 'Test(BuildUserContent(RoutesMislabelledAndUnnamedOfficeAttachmentsAwayFromVision|VisionIgnoresOCR|NoVisionWithOCRAppendsText)|NormalizeBinaryDocumentAttachmentFilename)$' -count=1 -timeout 5m
go test ./gui -run 'Test(BuildUserContentRoutesMislabelledBinaryDocumentsAwayFromVision|SaveAttachmentToLocalUsesOfficeMimeSuffixWhenFilenameMissing|ProcessMessageAttachmentsKeeps(MislabelledPDF|AllOfficeFormats|MislabelledOfficeFormats)OutOfInlineContext|PrepareVEAttachmentMessageSupportsAllOfficeFormats|MimeTypeForFile)$' -count=1 -timeout 5m
git diff --check -- corelib/agent/attachment.go corelib/agent/attachment_ocr_test.go gui/im_attachment.go gui/im_attachment_voice_test.go gui/app_ve_attachment.go gui/app_ve_attachment_receive.go gui/app_ve_attachment_receive_test.go
```

上述定向回归和差异检查均已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2
资源审阅、GUI 手工烟测和实际格式级回滚留痕仍为仓库外发布门禁。

### 无文件名 IM 文档附件的六格式身份恢复（2026-08-11）

继续审计附件落盘与自动抽取的衔接后，发现普通 IM/agentservice 共用的附件保存函数在文件名为空时只生成无后缀
临时名。即使远端已经声明标准 Office MIME，落盘路径仍无法被 `IsDocumentFilePath` 识别，因此 DOC/DOCX、
PPT/PPTX、XLS/XLSX 不会进入共享的自动抽取和 `read_document` OfficeRead 路由；此前 GUI 专属路径也有相同问题。

现已将 PDF 与六格式 Office 的 MIME→后缀映射收敛到 `corelib/agent`：文件名已有受支持后缀时优先保留；文件名
缺失（或没有后缀）时，标准 MIME 会为私有落盘副本补上相应后缀。该映射同样用作“不要被伪造图片元数据送进视觉
上下文”的保守隔离信号。它不根据 MIME 放宽内容解析：后续 PDF/OfficeRead 仍会验证实际字节、输入大小和 Office
容器安全状态。GUI 附件落盘复用同一规则，避免运行时与 agentservice/coding 子代理入口发生漂移。

```powershell
go test ./corelib/agent -run 'TestBuildUserContent(RoutesMislabelledAndUnnamedOfficeAttachmentsAwayFromVision|VisionIgnoresOCR|NoVisionWithOCRAppendsText)$' -count=1 -timeout 5m
go test ./gui -run 'Test(BuildUserContentRoutesMislabelledBinaryDocumentsAwayFromVision|SaveAttachmentToLocalUsesOfficeMimeSuffixWhenFilenameMissing|ProcessMessageAttachmentsKeeps(MislabelledPDF|AllOfficeFormats|MislabelledOfficeFormats)OutOfInlineContext|PrepareVEAttachmentMessageSupportsAllOfficeFormats|MimeTypeForFile)$' -count=1 -timeout 5m
git diff --check -- corelib/agent/attachment.go corelib/agent/attachment_ocr_test.go gui/im_attachment.go gui/im_attachment_voice_test.go gui/app_ve_attachment.go gui/app_ve_attachment_receive.go gui/app_ve_attachment_receive_test.go
```

上述定向回归和差异检查均已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2
资源审阅、GUI 手工烟测和实际格式级回滚留痕仍为仓库外发布门禁。

### 普通 IM 附件伪造图片元数据的文档路由保护（2026-08-11）

继续核对普通 GUI/IM 附件的自动注入路径时，发现图片分支原先优先相信远程 channel 提供的 `type=image` 或
`image/*` MIME。若实际文件名为 PDF、DOC/DOCX、PPT/PPTX、XLS/XLSX，但网关元数据被伪造或错误标记为图片，
附件会被嵌入为视觉 data URL，绕过落盘后的共享 `read_document` 路由和 OfficeRead 容器预检。这与 VE 附件已修复的
“远程 MIME 不能决定二进制文档上下文形态”原则不一致。

现在 IM 层首先按文件名识别 PDF 和六格式 Office 二进制文档，并将其从视觉分支转到正常文件落盘和共享的文档
自动抽取路径。文件名仅用于阻止错误的视觉注入，不授予内容可信或可解析资格；后续实际读取仍由 PDF/OfficeRead
签名、大小和加密容器边界决定。图片文件的原有视觉处理和受限群聊的不落盘策略保持不变。

```powershell
go test ./gui -run 'Test(BuildUserContent(RoutesMislabelledBinaryDocumentsAwayFromVision|WithoutLocalStagingKeepsUnsupportedGroupImageOffDisk)|ProcessMessageAttachmentsKeeps(MislabelledPDF|AllOfficeFormats|MislabelledOfficeFormats)OutOfInlineContext|PrepareVEAttachmentMessageSupportsAllOfficeFormats|MimeTypeForFile)$' -count=1 -timeout 5m
git diff --check -- gui/im_attachment.go gui/im_attachment_voice_test.go gui/app_ve_attachment.go gui/app_ve_attachment_receive.go gui/app_ve_attachment_receive_test.go
```

上述定向回归和差异检查均已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2
资源审阅、GUI 手工烟测和实际格式级回滚留痕仍为仓库外发布门禁。

### VE 附件文档隔离与既有 PDF 链路一致性（2026-08-11）

在修复 VE 的六格式 Office 伪造 `text/plain` MIME 后，进一步复核发现 PDF 占位符同样位于 MIME 快速路径之后。
这与计划中“PDF 继续由现有 GoPDF2/read_document 链路处理、二进制不直接进入上下文”的边界不一致：伪造
`text/plain` 的 PDF 会被内联，而正常 PDF 则只提供受控路径。

现已把 PDF 与六格式 Office 一并放在 MIME 可信度判断之前：VE 对任意以 `.pdf`、`.doc/.docx`、`.ppt/.pptx`、
`.xls/.xlsx` 命名的附件均只注入元信息和受控保存路径。PDF 的实际读取仍走其原有 GoPDF2 链路；Office 仍走
OfficeRead 的受控路由。本次不扩展 OfficeRead 范围，只消除了远程 MIME 元数据造成的上下文边界差异。

```powershell
go test ./gui -run 'TestProcessMessageAttachmentsKeeps(MislabelledPDF|AllOfficeFormats|MislabelledOfficeFormats)OutOfInlineContext$' -count=1 -timeout 5m
git diff --check -- gui/app_ve_attachment_receive.go gui/app_ve_attachment_receive_test.go
```

上述定向回归和差异检查均已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2
资源审阅、GUI 手工烟测和实际格式级回滚留痕仍为仓库外发布门禁。

### VE 协作附件伪造文本 MIME 的 Office 上下文隔离（2026-08-11）

继续复核 VE 群组讨论的远程附件接收路径时，发现它原先先按发送方提供的 MIME 决定是否直接注入正文，
再按文件名判断 Office。恶意或异常客户端可将 DOC/DOCX、PPT/PPTX、XLS/XLSX 标为 `text/plain`，使原始
二进制被作为聊天上下文传入 Agent，绕过受控的 `read_document`/OfficeRead 读取路径、容器预检和加密
fail-closed 策略。

现已改为优先按文件名的六格式 Office 身份隔离内容；无论声明 MIME 是否为 `text/plain`，VE 上下文仅提供
受控的原件说明和落盘路径，Agent 需通过共享 `read_document` 路由读取。普通文本和 PDF 行为不变，且不会因
此信任文件名来解析内容——实际读取仍须经 OfficeRead 的签名和容器安全检查。

```powershell
go test ./gui -run 'Test(ProcessMessageAttachmentsKeeps(AllOfficeFormats|MislabelledOfficeFormats)OutOfInlineContext|PrepareVEAttachmentMessageSupportsAllOfficeFormats|MimeTypeForFile)$' -count=1 -timeout 5m
git diff --check -- gui/app_ve_attachment.go gui/app_ve_attachment_receive.go gui/app_ve_attachment_receive_test.go
```

上述定向回归和差异检查均已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2
资源审阅、GUI 手工烟测和实际格式级回滚留痕仍为仓库外发布门禁。

### 移动文稿预览恢复路径的旧版 PowerPoint 二进制保护（2026-08-11）

继续审计移动端上传后、桌面 worker 回传前后的预览恢复逻辑时，发现 `mobileDraftSourceLooksTextLike` 已把
DOC/DOCX、XLS/XLSX、PPTX 视为二进制原件，但遗漏旧版 `.ppt`。在某些旧 PowerPoint 原件已保存而正文暂不可读的
状态下，这会让恢复显示路径尝试把原始 OLE 字节转换为 UTF-8 正文；即使多数二进制会因 NUL 或 UTF-8 校验被拒绝，
该缺口仍不应依赖偶然的字节特征。同时 MIME 快速拒绝分支也未明确识别 `application/vnd.ms-powerpoint`。

现已把 `.ppt` 与 PowerPoint MIME 纳入二进制保护名单，使 DOC/DOCX、PPT/PPTX、XLS/XLSX 在移动文稿预览路径
均不会绕过官方桌面 worker 的 OfficeRead 解析结果。修复只影响错误恢复/展示，不改变 Hub 对六格式的排队策略、
OfficeRead 的格式级回滚或加密文档 fail-closed 策略。

```powershell
go test ./hub/internal/httpapi -run 'Test(MobileUploadedFileNeedsRemoteOfficeExtractionRecognizesSixFormats|MobileDraftSourceLooksTextLikeRejectsAllOfficeFormats)$' -count=1 -timeout 2m
git diff --check -- hub/internal/httpapi/mobile_handlers.go hub/internal/httpapi/mobile_handlers_test.go
```

上述定向回归和差异检查均已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2
资源审阅、GUI 手工烟测和实际格式级回滚留痕仍为仓库外发布门禁。

### 剪贴板文件缺失文件名时的六格式身份保留（2026-08-11）

继续核对桌面 GUI 的文件选择入口后，发现 WebView 剪贴板文件在不提供原始文件名时会通过
`SavePastedFile` 依据 MIME 补充扩展名。此前该映射只包含 DOCX/XLSX/PPTX，遗漏
`application/msword`、`application/vnd.ms-excel` 与 `application/vnd.ms-powerpoint`。这会令
DOC/XLS/PPT 剪贴板文件落盘为无扩展名临时文件，后续自动附件与 `read_document` 无法按六格式 Office
路由，形成绕过普通 picker 的真实接入缺口。

现在 MIME 回退完整保留 `DOC/DOCX`、`XLS/XLSX`、`PPT/PPTX` 的后缀身份。该修复只在源文件名缺失时
命名私有临时副本；它不信任 MIME 内容本身来放宽解析，后续仍由共享容器预检、内容签名路由、32 MiB 上限和
加密 Office fail-closed 策略决定是否可读。

```powershell
go test ./gui -run 'Test(SavePastedFile(UsesMimeFallbackExtension|SanitizesNameAndWritesData)|GuessMimeFromMediaRecognizesAllSixOfficeFormats|PrepareVEAttachmentMessageSupportsAllOfficeFormats|MimeTypeForFile|ProcessMessageAttachmentsKeepsAllOfficeFormatsOutOfInlineContext)$' -count=1 -timeout 5m
go test ./hub/internal/ve -run 'Test(FileRelay_HandleUpload_InfersAllOfficeFormatsWithoutMultipartMIME|IsAllowedMIME|DetectMIMEType|FileCategory)$' -count=1 -timeout 2m
git diff --check -- gui/app_wails_bindings.go gui/app_pasted_file_test.go gui/im_gateway_media.go gui/im_gateway_media_test.go
```

上述定向回归和差异检查均已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2
资源审阅、GUI 手工烟测和实际格式级回滚留痕仍为仓库外发布门禁。

### MaClawSrv agentservice 附件与 `read_document` 的六格式闭环（2026-08-11）

继续审计 REST/agentservice 入口时，发现它虽复用了 `BuildUserContent`，但此前没有向模型暴露或执行共享的
`read_document` 工具；附件也会落到进程级 `MaclawDataDir`，与服务实例的 workspace 数据边界脱节。结果是服务端
收到的 DOC/DOCX、PPT/PPTX、XLS/XLSX 即使被自动抽取，后续分页、抽取失败重试或人工显式读取也不能稳定回到同一
OfficeRead 路径。

现已在 agentservice 中增加受 workspace 约束的 `read_document` 定义与执行分派：只接受 workspace 内的相对路径或
绝对路径，先解析到受控路径再调用共享 `agent.ToolReadDocument`。因此六格式仍使用统一的 OfficeRead 策略、内容
签名、ZIP/OLE 预检、大小上限和加密 fail-closed 边界；越界路径在进入解析器前被拒绝。`doc_only` 与 planning
工作流白名单也同步加入该只读工具。

服务端附件现在存入实例 workspace 下私有 `.attachments` 目录，而不是进程级 GUI 临时目录。自动注入的正文、模型
看到的落盘路径与后续 `read_document` 均指向同一个实例受控位置。新增端到端契约覆盖：伪装为图片且文件名为
`.png` 的真实 DOCX，按 Office MIME 规范化为 `.docx` 私有副本、进入 OfficeRead 自动抽取、可由 `read_document`
重读；试图读取 workspace 外文件会被拒绝。

```powershell
go test ./corelib/agent -count=1 -timeout 10m
go test ./corelib/agentservice -count=1 -timeout 10m
git diff --check -- corelib/agent/attachment.go corelib/agentservice/core_agent_executor.go corelib/agentservice/core_agent_executor_test.go corelib/agentservice/executor.go corelib/workflow/v2/types.go
```

上述完整包回归和差异检查均已通过。真实 `internal_authorized` 六格式 dual 语料、遗留 DOC/PPT 的人工质量复核、
P1/P2 资源审阅、GUI 手工烟测与实际格式级回滚留痕，仍为仓库外发布门禁。
### 服务端 `read_document` 失败语义收口（2026-08-11）

继续复核 MaClawSrv/agentservice 的共享 `read_document` 路径后，补齐了两个会影响工具循环可靠性的边界：

- 共享读取器的失败信封现在在服务端被正确映射为工具失败；其中 OfficeRead 的稳定
  `error_class=timeout` 会保留为 agent loop 的 timeout outcome，其他 `error_class=*` 均为 error，
  避免把失败的文档读取记录为成功工具调用。
- `file_path` 指向目录不再返回没有错误类别的普通文本，而是返回
  `error_class=invalid_path`。因此服务端、GUI 和后续工具恢复逻辑可以一致地区分“成功读取空内容”与“参数不是文件”。
- 加密 Office 的策略保持 fail-closed：密码不是 `read_document` 的输入，提供密码也不会触发解密；
  读取结果明确提示用户在受信任本地 Office 应用中解密并另存副本后再上传。

已通过以下定向回归：

```powershell
go test ./corelib/agent -run 'Test(ToolReadDocument_(Docx|PathAlias|DirectoryUsesFailureEnvelope|MalformedLegacyPPTFailsClosed|InvalidDocFailsClosed|RejectsOversizedInputBeforeExtraction)|FormatOfficeReadFailure_EncryptedDoesNotSuggestBypass|OfficeToolsRejectEncryptedContainersEvenWhenPasswordIsProvided)$' -count=1 -timeout 5m
go test ./corelib/agentservice -run 'Test(CoreAgent(AttachmentOfficeReadRouteUsesPrivateWorkspaceStaging|ReadDocumentReportsSharedReaderFailures)|ReadDocumentToolResultPreservesTimeoutOutcome|AttachmentStagingDirRequiresWorkspace)$' -count=1 -timeout 5m
go test ./corelib/knowledge -run 'Test(OfficeRead|ParseDocumentNodesPPT|Capabilities)' -count=1 -timeout 5m
```

完整 `corelib/knowledge` 包回归目前被工作树中与 OfficeRead 无关的既有断言失败阻断：
`TestCodingKnowledgeStore_UpdatePreservesID` 期望直接更新 `confidence`，而实现已将其定义为
recall evidence 管理字段。该问题不应用作 OfficeRead 已完成或未完成的证明，需由其所属改动单独处理。

真实 `internal_authorized` 六格式 dual 语料、DOC/PPT 文本顺序与关键 token 的人工复核、P1/P2
资源审阅、GUI 手工烟测、前端全量构建和真实格式级回滚留痕，仍是仓库外发布门禁；本次没有将其标记为完成。
### MaClawSrv 平台附件的 OfficeRead 输入上限一致性（2026-08-11）

继续审计 Hub→MaClawSrv 的虚拟员工附件落盘入口后，发现平台 relay 原本统一允许 50 MiB，
而随后由共享 `read_document`/OfficeRead 读取的 PDF 与六格式 Office 源文件上限是 32 MiB。
这会让超限文档先被完整下载、暂存到实例 workspace，最终才被读取器拒绝，造成入口间的资源边界不一致。

现已按附件身份在平台暂存阶段应用共享限制：PDF、DOC/DOCX、PPT/PPTX、XLS/XLSX 使用
`agent.MaxOfficeReadFileBytes`（32 MiB）；其他 relay 文件维持现有 50 MiB 产品限制。该判断仅
决定下载/内联落盘预算，未因 MIME 放宽解析信任；后续仍须通过共享的签名路由、ZIP/OLE 预检、
加密 fail-closed 和分页读取边界。内联 Hub text attachment 与远程下载附件复用同一限制函数。

```powershell
go test ./MaClawSrv -run 'Test(PlatformAttachmentMaxBytesForUsesSharedDocumentLimit|MaterializePlatformTextAttachmentNormalizesBinaryDocumentSuffix|EnrichPlatformMessageTreatsMislabelledOfficeTextAttachmentAsFile|PlatformFileAttachmentLineTreatsMislabelledOfficeImageAsFile)$' -count=1 -timeout 5m
go test ./corelib/agent -run 'Test(ToolReadDocument_(DirectoryUsesFailureEnvelope|RejectsOversizedInputBeforeExtraction)|BuildUserContentRoutesMislabelledAndUnnamedOfficeAttachmentsAwayFromVision|NormalizeBinaryDocumentAttachmentFilename)$' -count=1 -timeout 5m
```

真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测、
前端全量构建和实际格式级回滚留痕仍是仓库外发布门禁；本次没有将其标记为完成。
### VE 发送端的 OfficeRead 输入上限一致性（2026-08-11）

继续检查桌面 VE 文件发送入口后，发现其 document 分类仍使用 20 MiB 限制，而另一条直接
单文件发送路径仅按 50 MiB relay 限制。两者都与最终 `read_document`/OfficeRead 的 32 MiB
源文件边界不一致，导致同一 Office/PDF 文件会因发送 API 不同而被过早拒绝或先上传后拒绝。

现已统一：PDF、DOC/DOCX、PPT/PPTX、XLS/XLSX 的 VE 发送和 document 分类均使用
`agent.MaxOfficeReadFileBytes`（32 MiB）；图像与其他 relay 文件仍使用各自既有预算。扩展名
只用于在发送端施加资源预算，绝不授予内容可信性；接收端仍由共享 OfficeRead/PDF 签名、
ZIP/OLE、加密与分页边界完成实际验证。

```powershell
go test ./gui -run 'Test(VEFileAttachmentMaxBytesForUsesSharedDocumentLimit|BuildLocalVEFileAttachmentMessageRejectsOversizedOfficeDocument|ValidateFileSize|PrepareVEAttachmentMessageSupportsAllOfficeFormats|MimeTypeForFile)$' -count=1 -timeout 5m
go test ./hub/internal/ve -run 'Test(FileRelay_HandleUpload_InfersAllOfficeFormatsWithoutMultipartMIME|IsAllowedMIME|DetectMIMEType|FileCategory)$' -count=1 -timeout 2m
```

真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测、
前端全量构建和实际格式级回滚留痕仍是仓库外发布门禁；本次没有将其标记为完成。
### 第三方 IM 网关媒体限额与元数据可信边界（2026-08-11）

继续审计第三方 IM 网关的媒体上传和入站引用路径，发现其通用媒体预算原为 50 MiB。虽然上传 URL 已开始按 PDF/六种 Office 格式收紧预算，但入站 `mediaId` / 服务端媒体 URL 引用仍允许客户端保留自报的文件名、MIME 和大小；这会让已保存的 Office 文档被伪装为图片或普通文件，造成后续附件分类与 OfficeRead 路由不一致。

现已在 `corelib/im` 增加共享 `ThirdPartyMediaMaxBytesFor`：PDF 与 DOC/DOCX、PPT/PPTX、XLS/XLSX 为 `agent.MaxOfficeReadFileBytes`（32 MiB），其他媒体保持 50 MiB。该预算用于 prepare 请求、上传响应声明，以及 GUI/MaClawSrv 实际上传读取；入站媒体引用的声明大小也会先被限制。对于服务端已保存媒体，GUI 与 MaClawSrv 均强制使用保存时的文件名、MIME、Content-Type 和实际大小覆盖客户端字段，避免把 Office 身份交给不可信引用元数据。格式和 MIME 在这一层仅决定运输预算与隔离；实际 Office/PDF 读取仍由共享签名、ZIP/OLE 预检、加密 fail-closed 与分页边界验证。

```powershell
go test ./corelib/im -run 'Test(NormalizeThirdPartyIncomingRequestRejectsOversizeDirectData|ThirdPartyMediaMaxBytesForDocuments|NormalizeThirdPartyMediaPrepareRequest)$' -count=1 -timeout 3m
go test ./MaClawSrv -run 'TestThirdPartyGateway(ServerMediaMetadataOverridesClientReference|PrepareMediaUsesDocumentLimit|KeepsLargeServerMediaAsURLForAgent)$' -count=1 -timeout 5m
go test ./gui -run 'TestThirdPartyGateway(ServerMediaMetadataOverridesClientReference|PrepareMediaUsesDocumentLimit|ValidateIncomingAcceptsServerMediaURL)$' -count=1 -timeout 5m
git diff --check -- corelib/im/thirdparty_protocol.go corelib/im/thirdparty_protocol_test.go MaClawSrv/thirdparty_gateway.go MaClawSrv/http_test.go gui/thirdparty_gateway.go gui/thirdparty_gateway_test.go
```

以上定向回归和差异检查已通过。真实 `internal_authorized` 六格式 dual 语料、DOC/PPT 文本顺序与关键 token 的人工复核、P1/P2 资源审阅、GUI 手工烟测、前端完整构建、真实格式级回滚留痕以及旧解析器稳定发布周期后的退役，仍是仓库外发布门禁；本次未将其标记为完成。
### MaClawSrv 第三方 IM 大型文档的实例 workspace 闭环（2026-08-11）

继续检查第三方 IM 网关的端到端路径后，发现服务器媒体虽然已对 PDF 与六种 Office 格式使用 32 MiB 上传边界，但超过内联附件上限（256 KiB）的文件此前只会作为 `mediaId` 或 URL 提示给模型；它们并未进入 agentservice 实例 workspace，因此模型无法通过受控 `read_document` 实际读取这些文件。

现已在 MaClawSrv 中将此类已验证、归属当前 principal 的服务器媒体安全写入目标实例的 `workspace/.attachments`（文件权限 0600、目录 0700）。消息正文提供该受控绝对路径，Agent 可通过 workspace 约束的 `read_document` 调用共享 OfficeRead/PDF 读取器；不会把大型数据编码成 `MessageAttachment` 再传给 agentservice，从而保持其 256 KiB 内联数据边界。实例缺失重建后的重试也会重新读取实例 workspace 并重新生成附件上下文。落盘前仍按共享 `ThirdPartyMediaMaxBytesFor` 校验 PDF/DOC/DOCX/PPT/PPTX/XLS/XLSX 的 32 MiB 限制，其他媒体保持 transport 上限。

```powershell
go test ./MaClawSrv -run 'TestThirdPartyGateway(StagesLargeOfficeMediaInInstanceWorkspace|KeepsLargeServerMediaAsURLForAgent|PrepareMediaUsesDocumentLimit|ServerMediaMetadataOverridesClientReference)$' -count=1 -timeout 5m
git diff --check -- MaClawSrv/thirdparty_gateway.go MaClawSrv/http_test.go
```

上述定向回归与差异检查已通过。真实 `internal_authorized` 六格式 dual 语料、DOC/PPT 文本顺序与关键 token 的人工复核、P1/P2 资源审阅、GUI 手工烟测、前端完整构建、真实格式级回滚留痕以及旧解析器稳定发布周期后的退役，仍是仓库外发布门禁；本次未将其标记为完成。

### 图片资产展示的路径边界收口（2026-08-11）

继续复核阶段 4 的“图片写入受控资产目录、按需提供”约束后，发现桌面端虽然 Agent 标记和图片搜索 UI
已经使用不透明 `asset_id`，但仍保留一个可从 WebView 接受本机路径的 `KnowledgeOpenImageFile` 兼容方法。
即使它做了目录前缀检查，路径本身仍不是模型或 UI 边界应接受的能力，也不能可靠表达软链接替换后的资产归属。

现在所有展示入口统一为资产 ID：Agent 图片标记、知识库搜索缩略图和点击打开原图均通过
`KnowledgeOpenImageAsset(assetID)`；旧的路径式 bridge 明确拒绝请求。资产管理器在读取原图、缩略图或预览前
会验证安全 ID、受控根目录、非软链接资产目录、常规文件、JPEG/尺寸/字节预算；Doctor 也使用同一读取器，
因而会将向量原图、损坏缩略图或链接替换识别为缺失/不健康，而不会把它们当成可展示媒体。模型可见的
`[KB_IMAGE:asset_id|data:image/jpeg;base64,...]` 协议仍只包含 120px JPEG 缩略图和 asset ID，绝不携带路径。

```powershell
go test ./corelib/knowledge -run 'Test(ImageAsset(Manager|Presentation|Health)|Doctor)' -count=1 -timeout 5m
go test ./gui -run 'Test(Knowledge(ImageAsset|GetImageAssetPaths|OpenImageFile)|KnowledgeImageAssetOriginalPath)' -count=1 -timeout 5m
git diff --check -- corelib/knowledge/image_assets.go corelib/knowledge/doctor.go gui/app_knowledge.go
```

上述定向回归与差异检查已通过。服务端图片 API 全包重测仍受当前工作树中无关的
`thirdPartyAgentInput` 测试签名失配阻断；不在本项中修改该第三方网关测试。真实 `internal_authorized`
六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测、前端完整构建和格式级回滚留痕继续是
仓库外发布门禁。

### 图片资产读取的句柄级边界复核（2026-08-11）

继续复核阶段 4 的图片展示读取路径后，发现此前的“先 `Lstat`、再按绝对路径打开”虽会拒绝读取前已存在的
软链接，但在检查和打开之间仍可能被并发的本地缓存替换打断。展示端本身并不应把这类可变缓存当作可信路径。

现在原图、缩略图、预览以及 Agent 缩略图嵌入均通过 Go `os.Root` 在已经验证的资产目录句柄内重新
`Lstat` 并打开目标文件；根句柄会拒绝越出资产目录的链接，打开后的同一文件句柄还会再次核对常规文件和长度。
这样即使缓存目录/文件在检查期间被替换，展示读取也不会跟随到受控目录外的主机文件。原有安全 ID、格式、
尺寸、字节预算和鉴权/读范围检查保持不变。服务端同时补充 canonical asset ID 路由和兼容 source ID 路由的
软链接媒体 404 回归。

```powershell
go test ./corelib/knowledge -run 'Test(ImageAsset(Manager|Presentation|Health)|Doctor|EmbedImageThumb)' -count=1 -timeout 5m
go test ./corelib/agentservice -run 'Test(CoreAgent(GeneralKnowledgeSearchDoesNotIncludeImageDisplayMarker|DedicatedKnowledgeImageSearchIncludesSafeDisplayMarker|KnowledgeImportToolsExecuteAgainstStore))' -count=1 -timeout 5m
go test ./MaClawSrv -run 'Test(KnowledgeImageSearchRespectsReadScopesAndReturnsMedia|KnowledgeImageAssetEndpointsEnforceReadAccess|KnowledgeImageAssetEndpointsRejectNonRasterOriginal|KnowledgeCapabilitiesDeclareImageSearchModes)' -count=1 -timeout 5m
git diff --check -- corelib/knowledge/image_assets.go MaClawSrv/knowledge_access_test.go
```

以上定向回归与差异检查已通过；这只证明仓库内的受控资产边界，不能替代真实 `internal_authorized` 六格式
dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测、前端完整构建和格式级回滚留痕等仓库外发布门禁。

### 服务端图片响应的路径重开收口（2026-08-11）

继续核对图片检索的远程展示路径时，发现 HTTP endpoint 虽已用资产管理器拒绝越界、损坏或软链接媒体，
但随后仍会把返回的路径交给 `http.ServeFile` 重新打开。这样会在“验证的文件”和“响应的文件”之间留下
本地缓存替换窗口，且与桌面端 asset-ID-only 的展示边界不一致。

现已新增 `ReadKnowledgeImageAsset`：在受控资产目录句柄中读取缩略图、预览或原图并完成大小、格式、尺寸与
常规文件验证，只向服务端交付字节和探测出的格式。canonical `/knowledge/images/{assetId}` 及兼容
`/knowledge/sources/{sourceId}` 路由均直接响应这些已验证字节，不再经过 `ServeFile` 或再次按路径打开。
认证、知识库读取范围和 `private` 缓存响应头的原有契约保持不变。

```powershell
go test ./corelib/knowledge -run 'Test(ImageAsset(Manager|Presentation|Health)|Doctor|EmbedImageThumb)' -count=1 -timeout 5m
go test ./MaClawSrv -run 'Test(KnowledgeImageSearchRespectsReadScopesAndReturnsMedia|KnowledgeImageAssetEndpointsEnforceReadAccess|KnowledgeImageAssetEndpointsRejectNonRasterOriginal|KnowledgeCapabilitiesDeclareImageSearchModes)' -count=1 -timeout 5m
go test ./corelib/agentservice -run 'Test(CoreAgent(GeneralKnowledgeSearchDoesNotIncludeImageDisplayMarker|DedicatedKnowledgeImageSearchIncludesSafeDisplayMarker|KnowledgeImportToolsExecuteAgainstStore))' -count=1 -timeout 5m
go test ./gui -run 'Test(Knowledge(ImageAsset|GetImageAssetPaths|OpenImageFile|ImageSearchToolReturnsOnlyImageEvidence|SearchDoesNotExposeImageDisplayMarkers)|VEKnowledgeImageSearchReturnsDisplaySafeEvidence|CodingSubAgentGeneralKnowledgeSearchDoesNotIncludeImageMarker|RemoteCodingSubAgentImageKnowledgeSearchDoesNotRequireSSHHandler)' -count=1 -timeout 5m
git diff --check
```

上述定向验证均已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、
GUI 手工烟测、前端完整构建和格式级回滚留痕仍为仓库外发布门禁，未在此标记为完成。

### 图片端点的静态文件协议保持（2026-08-11）

将服务端从 `ServeFile` 收敛为受控字节响应后，继续复核发现直接 `ResponseWriter.Write` 会丢失原有静态文件
响应的 `HEAD`、`Content-Length` 与单区间 `Range` 语义，影响浏览器预览、缓存及下载组件。安全收口不应以
协议倒退为代价。

现在服务端以已验证内存字节调用 `http.ServeContent`：它保留 `HEAD`、长度和字节范围响应，同时不重新打开
主机路径。回归在鉴权成功的 asset endpoint 上固定校验正常内容长度、空 `HEAD` 正文和 `bytes=0-0` 的
`206 Partial Content`；认证、读取范围与受控资产读取仍位于其前。

```powershell
go test ./MaClawSrv -run 'Test(KnowledgeImageSearchRespectsReadScopesAndReturnsMedia|KnowledgeImageAssetEndpointsEnforceReadAccess|KnowledgeImageAssetEndpointsRejectNonRasterOriginal|KnowledgeCapabilitiesDeclareImageSearchModes)' -count=1 -timeout 5m
go test ./corelib/knowledge -run 'Test(ImageAsset(Manager|Presentation|Health)|Doctor|EmbedImageThumb)' -count=1 -timeout 5m
go test ./corelib/agentservice -run 'Test(CoreAgent(GeneralKnowledgeSearchDoesNotIncludeImageDisplayMarker|DedicatedKnowledgeImageSearchIncludesSafeDisplayMarker|KnowledgeImportToolsExecuteAgainstStore))' -count=1 -timeout 5m
go test ./gui -run 'Test(Knowledge(ImageAsset|GetImageAssetPaths|OpenImageFile|ImageSearchToolReturnsOnlyImageEvidence|SearchDoesNotExposeImageDisplayMarkers)|VEKnowledgeImageSearchReturnsDisplaySafeEvidence|CodingSubAgentGeneralKnowledgeSearchDoesNotIncludeImageMarker|RemoteCodingSubAgentImageKnowledgeSearchDoesNotRequireSSHHandler)' -count=1 -timeout 5m
git diff --check
```

上述定向验证均已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、
GUI 手工烟测、前端完整构建和格式级回滚留痕仍为仓库外发布门禁，未在此标记为完成。

### 第三方 IM 的 GUI→Hub→GUI Office/PDF 媒体闭环（2026-08-11）

继续审计第三方 IM 网关的 Hub 模式后，发现本地模式已能将 PDF/Office 附件落盘并经共享自动提取路径处理，但 GUI 发往 Hub 的 `im.gateway_message` 负载此前只保留文本和设备上下文，遗漏了附件。Hub 的远程网关、IM Adapter 和下行 `im.user_message` 实际支持附件字段，因此该遗漏会使 Hub 模式静默丢失 PDF 与 DOC/DOCX、PPT/PPTX、XLS/XLSX 输入。

现已新增受认证的 `source_media_id` 附件引用：小型内联媒体继续携带已有 Base64；已上传的 PDF/Office 服务器媒体则只把经入站验证、属于该 GUI 的不透明媒体 ID、服务端文件名/MIME/大小传经 Hub。Hub 不解引用、不暴露媒体 URL，也不把 32 MiB 文档编码进 WebSocket。下行返回到网关所有者 GUI 时，才按当前 `ClientToolContext.ClientID` 重新查询本机已上传媒体、校验同设备归属和共享 PDF/Office 32 MiB 上限，并以保存的真实类型、文件名、MIME 和字节恢复附件；伪造为 image 的 Office 声明会被真实文件类型覆盖。该恢复后的附件继续使用既有 GUI 落盘、`AppendDocumentExtractsToDescriptions` 与 `read_document`/OfficeRead 共享路径。非 PDF/Office 的服务器媒体不走这条引用通道，避免重建大型 WebSocket 载荷。

```powershell
go test ./gui -run 'Test(ThirdPartyGatewayHubPayload(PreservesOwningClientTools|PreservesServerMediaReference|OmitsNonDocumentServerMedia|KeepsSmallInlineMedia)|ResolveHubThirdPartyMediaReferences(RestoresOwnedOfficeDocument|RejectsDifferentClient))$' -count=1 -timeout 5m
go test ./hub/internal/im -run 'TestCorelib.*|Test.*Attachment' -count=1 -timeout 5m
git diff --check -- corelib/agent/message.go corelib/im/types.go hub/internal/im/adapter.go hub/internal/im/corelib_bridge.go gui/thirdparty_gateway.go gui/thirdparty_gateway_test.go gui/remote_hub_client.go gui/remote_hub_client_test.go
```

上述定向回归与差异检查已通过。真实 `internal_authorized` 六格式 dual 语料、DOC/PPT 文本顺序和关键 token 的人工复核、P1/P2 资源审阅、GUI 手工烟测、前端全量构建、真实格式级回滚留痕以及旧解析器稳定发布周期后的退役，仍是仓库外发布门禁；本次未将替换或发布标记为完成。

### XLS/XLSX 富图片知识库闭环（2026-08-11）

继续复核阶段 4 的图片消费路径时，发现 OfficeRead 已能将表格工作簿中的富图片放入同一次受控提取结果，但知识库导入此前会对所有 spreadsheet 无条件跳过图片资产化。因此 `.xlsx` 的文本和结构化行虽可检索，图片却无法进入图片搜索、`knowledge_image_search` 或 Agent 的安全展示链路。

现已调整为：仅当 OfficeRead 富内容已显式启用并已在导入私有快照中取得时，XLS/XLSX 会将其中的图片保存为受控知识库资产和图片节点；表格行仍从同一私有快照在原子事务内写入。刷新路径同样在替换事务失败时回收本次创建的 provisional 图片资产，避免留下无节点引用的文件。未启用富内容时，原有 legacy spreadsheet 不提取图片的行为保持不变；不会为此重新打开工作簿或把图片二进制、路径写进聊天正文。

```powershell
go test ./corelib/knowledge -run 'TestOfficeReadKnowledgeXLSX(ImportPersistsRichImages|RefreshCleansProvisionalRichAssetsWhenNodeInsertFails)$' -count=1 -timeout 5m
```

上述新增回归已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测、前端全量构建与实际格式级回滚留痕仍是仓库外发布门禁。

### 旧版 XLS 的结构化知识库索引补齐（2026-08-11）

继续审计六格式知识库导入时，发现 `.xls` 虽已被扫描、预检、解析为文本节点，也被声明为 spreadsheet，
但表格二级索引实际复用了只支持 XLSX/CSV 的 `GoExcel` 读取器。因此导入流程会先成功写入 XLS 文本节点，
随后在写入 `kb_tables` / `kb_rows` 时失败，造成“六格式可导入”的能力声明与实际检索结果不一致。

现已在共享 `corelib/excel` 读取层增加旧版 BIFF XLS 到公开 `ReadResult` 模型的适配，令 `ReadFile`、
`ReadAllSheets`、`ListSheets` 和知识库结构化行/列导入使用同一份工作表数据。XLS 仍须经过既有 OLE、
加密、尺寸和私有快照预检；转换只提供表格数据，不改变 OfficeRead 富图片的显式 opt-in 约束。

```powershell
go test ./corelib/excel -count=1 -timeout 3m
go test ./corelib/knowledge -run 'Test(KnowledgeImportFilesIndexesLegacyXLSAsStructuredRows|ImportSpreadsheetSourceV2CreatesStructuredRowsForLegacyXLS)$' -count=1 -timeout 5m
```

上述定向回归通过；没有据此声明完整发布门禁完成。

### Agent `read_excel` 旧版 XLS 契约收口（2026-08-11）

在共享 `corelib/excel` 已具备 BIFF XLS 的结构化读取、工作表选择和 A1 范围能力后，继续审计发现
Agent 的 `read_excel` 仍保留独立 XLS JSON 适配器，并拒绝 `range`。这会使 Agent 调用与知识库
结构化索引使用不同读取模型，也使已支持的范围读取能力无法在工具中使用。

现已移除该重复分支：`.xls`、`.xlsx` 和 `.csv` 均在通过既有私有快照、格式/加密和输入大小预检后，
使用 `corelib/excel.ReadFile` 生成同一 `ReadResult` JSON 契约。`sheet`、A1 `range`、`max_rows`
及 `truncated` 对三种格式一致；范围本身未被 `max_rows` 截断时，`truncated=false`。工具描述同步删除了
“.xlsx/.csv only”的过期限制。

```powershell
go test ./corelib/excel -run '^TestReadLegacyXLSUsesPublicStructuredResultModel$' -count=1 -timeout 2m
go test ./corelib/agent -run '^TestToolReadExcelLegacyXLSMatchesPublicRangeAndRowLimitContract$' -count=1 -timeout 2m
go test ./corelib/knowledge -run '^Test(KnowledgeImportFilesIndexesLegacyXLSAsStructuredRows|ImportSpreadsheetSourceV2CreatesStructuredRowsForLegacyXLS)$' -count=1 -timeout 3m
```

上述定向回归通过，`git diff --check` 无空白错误。真实 `internal_authorized` 六格式 dual 语料、
人工质量复核、P1/P2 资源审阅、GUI 手工烟测、前端全量构建和实际格式级回滚留痕仍是仓库外发布门禁；
本记录不将其标记为完成。
### 核心回归的恢复提示引号断言修正（2026-08-11）

执行 OfficeRead 核心回归时，发现 `TestFormatOfficeReadFailure_QuotesRecoveryTaskPath` 自身把
`task="读取本地文件..."` 的正常开头误判为失败；这与同一测试随后要求完整
`task="..."` 被 JSON 转义的断言相矛盾，导致完整 `corelib/agent` 套件无法作为发布门禁证据。

现已将首个断言收紧为实际安全不变量：用户提供的双引号不得在恢复用 `craft_tool` 的 `task` 参数中
提前闭合。随后仍精确校验完整的 JSON 引号转义结果。此修改只修复测试判定，不改变恢复提示、
加密文件 fail-closed 策略、密码处理或任何生产提取路径。

```powershell
go test ./corelib/agent -run '^TestFormatOfficeReadFailure_QuotesRecoveryTaskPath$' -count=1 -timeout 2m
go test ./corelib/agent -count=1 -timeout 15m
go test ./corelib/knowledge -run 'TestOfficeRead|Test(KnowledgeImportFilesIndexesLegacyXLSAsStructuredRows|ImportSpreadsheetSourceV2CreatesStructuredRowsForLegacyXLS)' -count=1 -timeout 10m
go test ./corelib/excel -count=1 -timeout 5m
go test ./corelib/im ./hub/internal/im -count=1 -timeout 10m
go test ./gui -run 'Test(ThirdPartyGateway|ResolveHubThirdPartyMediaReferences|RemoteHubClientConnectAndSync)' -count=1 -timeout 10m
git diff --check -- corelib/agent/tools_office_read_test.go gui/remote_hub_client_test.go
```

上述命令均已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、
GUI 手工烟测、前端全量构建、实际格式级回滚留痕，以及旧解析器经历稳定发布周期后的退役，仍是
仓库外发布门禁；本记录不将其标记为完成。
### 六格式 IM 附件到自动抽取的端到端回归（2026-08-11）

继续核对阶段 2/3 所要求的 GUI/IM 附件、自动注入与 `read_document` 共享抽取契约。已有单点测试分别
覆盖文件选择、MIME 归类和 DOCX 附件，但缺少一个从入站附件落盘到自动抽取的六格式共同回归；因此 DOC、
PPT、XLS 等格式可能在 MIME 改名、附件暂存或自动注入衔接处静默偏离 OfficeRead 路由。

现已新增 `TestBuildUserContentStagesAndAutoExtractsAllOfficeFormats`。它以被标为 image 的 DOC/DOCX、
PPT/PPTX、XLS/XLSX 附件为输入，验证每一格式都会：拒绝进入视觉 Base64 上下文、根据声明 MIME 将
暂存名规范为真实 Office 后缀、进入共享自动注入区块，并通过 OfficeRead 的统一抽取 seam 返回对应格式的
文本。测试不把 MIME 当成内容可信结论：每个 fixture 仍先经过共享 Office/OLE/OOXML 预检，实际生产路径
仍执行签名、容器、加密和资源边界验证。

```powershell
go test ./corelib/agent -run 'Test(BuildUserContent(StagesAndAutoExtractsAllOfficeFormats|RoutesMislabelledAndUnnamedOfficeAttachmentsAwayFromVision|RoutesConflictingOfficeAttachmentIntoRealAutoExtract)|ExpandUserSelectedFilePaths_AllOfficeFormatsUseOfficeReadDefaultRoute|ToolReadDocumentRejectsEncryptedSixOfficeFormatsEvenWhenPasswordIsProvided)$' -count=1 -timeout 10m
git diff --check -- corelib/agent/attachment_ocr_test.go corelib/agent/tools_office_read_test.go gui/remote_hub_client_test.go
```

上述定向回归与差异检查已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、
GUI 手工烟测、前端全量构建、实际格式级回滚留痕，以及旧解析器经历稳定发布周期后的退役，仍是仓库外发布
门禁；本记录不将其标记为完成。

### Agent 图片检索输出的跨平台路径隔离（2026-08-11）

继续复核阶段 4 的“Agent 可检索并显示图片、但不暴露主机路径”约束后，发现 GUI/VE 的专用
`knowledge_image_search` 虽已使用不透明 asset ID 和缩略图 marker，但结果对象在缩略图可用时仍会序列化完整
`Source`；缩略图缺失时还会回退原始 `SearchResult`。两条分支都可能把导入图片的 `URI`、`CanonicalURI`、
`RelativePath`、`ProjectPath` 或 `ErrorMessage` 带进模型上下文。与此同时，共享图片标签曾接受 `RelativePath`，在
不同操作系统上格式化迁移来的 Windows 绝对路径时也可能漏检。

现已收敛为窄化的图片展示投影：GUI/VE 图片搜索始终只输出 source ID/kind/display name、节点/工作表/页码、
检索摘要、分数和路径安全的引用；只有受控缩略图实际可读时才附加 asset ID、data URL 和
`[KB_IMAGE:asset_id|data:image/jpeg;base64,...]` marker。不可读资产仍返回可检索的安全证据而不会退回原始对象。
共享 `FormatImageSourceLabel`/`FormatImageCitationLabel` 同时拒绝 POSIX、盘符、UNC 和 `file://` 形式的绝对路径，
让 Core Agent、Coding Agent 与远程 Coding Agent 使用同一展示边界；Coding 的专用图片结果也单独对图片标题执行该
过滤。`knowledge_context_pack` 对图片命中同样不再以路径元数据生成标题、项目 citation 或 citation payload；
`knowledge_explain`、GUI/VE 普通检索以及本地/远程 Coding 普通检索也复用同一安全投影。通用
`knowledge_search` 的描述明确指向专用图片工具，避免承诺在普通文本检索中提供 marker。

```powershell
go test ./corelib/knowledge -run 'TestFormat(SearchResultsForLLM|ImageLabels)' -count=1 -timeout 5m
go test ./corelib/knowledge -run 'TestContextPack(TitleDoesNotExposeImagePathMetadata|DoesNotExposeImagePathThroughCitations)$' -count=1 -timeout 5m
go test ./gui -run 'TestKnowledgeImageSearchNeverLeaksImageSourcePaths' -count=1 -timeout 5m
go test ./gui -run 'TestVEKnowledgeImageSearchReturnsDisplaySafeEvidence' -count=1 -timeout 5m
go test ./gui -run 'Test(CodingImageSearchNeverUsesAbsolutePathMetadataAsDisplayEvidence|CodingSubAgentGeneralKnowledgeSearchDoesNotIncludeImageMarker|RemoteCodingSubAgentImageKnowledgeSearchDoesNotRequireSSHHandler)' -count=1 -timeout 5m
go test ./corelib/agentservice -run 'TestCoreAgent.*KnowledgeImageSearch' -count=1 -timeout 5m
go test ./MaClawSrv -run 'Test(KnowledgeImageSearchRespectsReadScopesAndReturnsMedia|KnowledgeImageAssetEndpointsEnforceReadAccess|KnowledgeImageAssetEndpointsRejectNonRasterOriginal|KnowledgeCapabilitiesDeclareImageSearchModes)' -count=1 -timeout 5m
git diff --check -- corelib/knowledge/tool_format.go corelib/knowledge/tool_format_test.go corelib/knowledge/context_pack.go corelib/knowledge/context_pack_test.go corelib/knowledge/store.go gui/tools_knowledge.go gui/tools_knowledge_test.go gui/coding_subagent_knowledge.go gui/remote_coding_subagent.go gui/coding_subagent_test.go
```

上述回归已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测、
实际格式级回滚留痕及稳定发布周期后的旧解析器退役，仍是仓库外发布门禁；本记录不将其标记为完成。
### 前端 OfficeRead 设置构建门禁恢复（2026-08-11）

执行阶段 7 所要求的前端设置测试与生产构建时，发现 `gui/frontend/pnpm-workspace.yaml` 将
`allowBuilds.esbuild` 留成了 pnpm 的占位文本 `set this to true or false`。这会使 pnpm 拒绝
执行 esbuild 的受控 postinstall，导致测试和 Vite 构建在依赖检查阶段失败，无法验证 OfficeRead
引擎、六格式白名单、fallback 和知识库 Markdown 开关的 GUI 绑定。

现已将工作区供应链策略明确为 `allowBuilds.esbuild: true`；它只批准 lockfile 已固定的 esbuild 本地
构建脚本，不改变运行时依赖版本或 OfficeRead 路由。通过本地 Vitest 与生产 bundle 验证：设置面板的
OfficeRead 控件、Wails `AppConfig` 序列化，以及 TypeScript/Vite 输出均可用。已有测试中的 React
`act(...)` warning 属于其他设置的异步 mock 提示，未造成失败，也未被本次静默忽略为“无 warning”。

```powershell
& 'C:\Users\ma139\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe' 'node_modules\vitest\vitest.mjs' run 'src/components/settings/__tests__/GeneralSettingsPanel.test.tsx' 'src/config/__tests__/settingsTabConfig.test.ts' --reporter=dot
& 'C:\Users\ma139\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe' 'node_modules\typescript\bin\tsc'
& 'node_modules\.bin\vite.cmd' build
git diff --check -- gui/frontend/pnpm-workspace.yaml corelib/agent/attachment_ocr_test.go corelib/agent/tools_office_read_test.go gui/remote_hub_client_test.go
```

前端定向测试 32 项通过，TypeScript 与 Vite 已生成当前 `dist` bundle，差异检查通过。真实
`internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、GUI 手工烟测、实际格式级
回滚留痕，以及旧解析器经历稳定发布周期后的退役，仍为仓库外发布门禁；本记录不将其标记为完成。
### GUI 持久化格式级回滚到运行时策略闭环（2026-08-11）

计划要求格式级回滚可独立生效且不污染缓存。原有测试已分别证明配置写入和 agent 缓存键变化，但没有证明
桌面 GUI 的持久化 `AppConfig` provider 会在下一次提取前把更新后的白名单交给 OfficeRead；这会留下
“界面显示已回滚、实际解析仍使用旧设置”的潜在缝隙。

现已新增 `TestNewAppOfficeReadProviderAppliesFormatRollbackImmediately`：通过真实 `NewApp` 安装的 provider
写入 `doc,docx` 策略，验证运行时解析器立即读取该策略；随后仅移除 `docx`，验证下一次运行时快照与
知识库富内容策略均不再启用 DOCX；最后切换 `legacy`，验证全局回滚即时关闭 OfficeRead 富内容，同时保留
格式审计记录。该测试与既有 `TestToolReadDocument_FormatLevelOfficeReadRollbackInvalidatesCache` 组合，覆盖
“GUI 持久化策略传播”和“两分钟分页缓存不会返还旧 OfficeRead 正文”两个仓内可自动验证部分。

```powershell
go test ./gui -run 'Test(NewAppOfficeReadProviderAppliesFormatRollbackImmediately|OfficeReadFormatLevelRollbackDrill|LoadConfigPromotesHistoricPPTOfficeReadScopeOnce)$' -count=1 -timeout 10m
go test ./corelib/agent -run 'Test(ToolReadDocument_FormatLevelOfficeReadRollbackInvalidatesCache|OfficeReadCacheKeySuffixChangesWithSettings|CurrentOfficeReadRuntimePolicyReturnsCanonicalResolvedSnapshot)$' -count=1 -timeout 5m
git diff --check -- gui/app_config_test.go gui/frontend/pnpm-workspace.yaml corelib/agent/attachment_ocr_test.go corelib/agent/tools_office_read_test.go gui/remote_hub_client_test.go
```

上述回归与差异检查已通过。真实 `internal_authorized` 六格式 dual 语料、人工质量复核、P1/P2 资源审阅、
GUI 手工烟测、实际格式级回滚留痕，以及旧解析器经历稳定发布周期后的退役，仍为仓库外发布门禁；本记录
不将其标记为完成。

### dual-report 六格式发布范围审计（2026-08-11）

继续复核 dual-read 报告的发布门禁后，确认命令已经支持调用方在审计时显式指定
`-required-formats doc,docx,ppt,pptx,xls,xlsx`；但此前回归主要覆盖两个格式的组合，无法直接证明六格式全量
替换时，任一格式缺失都会被拒绝。本轮新增
`TestAuditReportRequiresAllSixOfficeFormatsWhenRequested`：构造仅含脱敏计数的六格式
`internal_authorized` 双读报告，验证完整范围可通过量化审计；再移除 `pptx` 的声明、汇总与评估，验证审计会同时
给出 `required_format_not_declared:pptx`、`missing_summary:pptx` 和 `missing_assessment:pptx`，因而不能以其余五种
格式的通过结果冒充六格式发布证据。

```powershell
go test ./cmd/office-read-dual-report -count=1 -timeout 10m
git diff --check -- cmd/office-read-dual-report/main_test.go
```

上述测试和差异检查均已通过。这只加固了本地脱敏审计器；真实 `internal_authorized` 六格式 dual 语料、DOC/PPT
文本顺序与关键 token 的人工复核、P1/P2 资源审阅、GUI 手工烟测、实际格式级回滚留痕，以及稳定发布周期后的
旧解析器退役，仍是仓库外发布门禁，未在此标记为完成。
### 阶段交付物全量复验（2026-08-11）

本轮按照计划第 1、3、4、6、7、9 节重新核对当前工作树，而非沿用早期“仅 `.ppt` 灰度”的历史记录。当前
`AppConfigDefaults`、缺省运行时策略、GUI 配置 provider 与 `read_document` 共同默认启用
`doc/docx/ppt/pptx/xls/xlsx` 的 OfficeRead 主路径，且 `fallback=true`；`legacy` 全局 kill switch 与非空格式
白名单的格式级回滚仍在运行时立即生效。富 Markdown/图片消费继续是独立的默认关闭开关，只能在
`officeread + emit_markdown + 已启用格式` 时进入知识库受控资产链路；自动聊天注入仍只接收文本。

已重新执行核心实现的完整包测试：`corelib/agent`、`corelib/knowledge`、`corelib/excel` 与 `corelib/pptx`
均通过；知识库全包耗时约 197 秒，表明此前短运行窗口中没有输出的中断不是测试失败。另复验 dual-report
全包、GUI 的持久化格式级回滚到运行时策略闭环，并对本次 OfficeRead 相关文件执行差异检查。

```powershell
go test ./corelib/agent -count=1 -timeout 15m
go test ./corelib/knowledge -count=1 -timeout 15m
go test ./corelib/excel ./corelib/pptx -count=1 -timeout 5m
go test ./cmd/office-read-dual-report -count=1 -timeout 10m
go test ./gui -run 'Test(NewAppOfficeReadProviderAppliesFormatRollbackImmediately|OfficeReadFormatLevelRollbackDrill|LoadConfigPromotesHistoricPPTOfficeReadScopeOnce)$' -count=1 -timeout 10m
git diff --check -- corelib/agent/officeread_adapter.go corelib/agent/officeread_adapter_test.go corelib/agent/tools_office_read.go corelib/agent/tools_office_read_test.go corelib/knowledge/officeread_import.go corelib/knowledge/officeread_import_test.go corelib/excel/read.go corelib/excel/read_test.go corelib/pptx/pptx.go corelib/pptx/pptx_test.go cmd/office-read-dual-report/main.go cmd/office-read-dual-report/main_test.go
```

上述命令均已通过。它们证明仓库内实现和回归处于可验证状态，但不会把真实 `internal_authorized` 六格式
双读语料、DOC/PPT 文本顺序与关键 token 的人工复核、P1/P2 资源审阅、GUI 手工烟测、实际格式级回滚留痕或
稳定发布周期后的旧解析器退役标记为完成；这些仍需在仓库外受控执行并留档。
### TUI 三入口持久化 OfficeRead 策略闭环（2026-08-11）

复核发现 TUI 虽与 GUI 共用 `AppConfig`，但交互模式、`-p/--prompt` 管道模式和 `--mode rpc` 入口此前没有安装 `agent.SetOfficeReadConfigProvider`。因此，在未设置环境变量时，用户已保存的 `legacy` 全局回滚、非空格式白名单、`fallback` 与知识库 Markdown 开关不能保证作用于 TUI 的 `read_document`；这与 GUI 的运行时策略存在不一致。

现已在三个 TUI 入口安装同一只读 provider。provider 在每次提取时从共享 `config.json` 取得最小的策略快照并复制格式切片，因此配置保存、外部同步或格式级回滚会在下一次提取时立即生效，无需重建工具注册表。底层仍保留环境变量最高优先级和 provider 异常时的安全默认策略。新增 `TestTUIOfficeReadProviderUsesPersistedPolicyAndRefreshesImmediately` 覆盖 `officeread + doc/docx + fallback=false + emit_markdown=true`，再将持久化配置改为 `legacy` 后验证下一次运行时快照立即切换，并恢复默认 fallback/富内容行为。

```powershell
go test ./tui -run '^TestTUIOfficeReadProviderUsesPersistedPolicyAndRefreshesImmediately$' -count=1 -timeout 5m
go test ./tui -count=1 -timeout 15m
git diff --check -- tui/app.go tui/pipe_mode.go tui/rpc_mode.go tui/app_text_test.go
```

上述回归均已通过。这只补齐仓内入口的一致性证据；真实 `internal_authorized` 六格式 dual 语料、DOC/PPT 人工质量复核、P1/P2 资源审阅、GUI 人工烟测、实际格式级回滚留痕，以及稳定发布周期后的旧解析器退役，仍是仓外发布门禁，未在此标记为完成。
### 服务端多租户请求级 OfficeRead 策略隔离（2026-08-11）

继续审查非桌面入口时发现，`corelib/agentservice` 在同一进程中服务多个租户和用户；若把某一请求的 `AppConfig` 安装到全局 `agent.SetOfficeReadConfigProvider`，并发请求会互相覆盖 OfficeRead 的 `legacy` 全局回滚、格式白名单、fallback 与 Markdown 策略。此前服务端 `read_document` 与附件自动提取会走默认全局 provider，因此不能证明它们遵守所属用户的已保存策略。

现已新增显式、仅供受信任宿主调用的 OfficeRead 配置路径：请求策略先按既有规则叠加最高优先级环境变量，再作为不可变快照贯穿 `read_document`、完整源版本/分页缓存以及附件自动提取。缓存键包含该解析后的策略指纹，因此同一路径、相同字节在不同用户策略下不会复用错误正文；默认公开 API 仍保留原 provider 行为。`corelib/agentservice` 在最终工具执行边界和附件建模边界从 `ExecuteRequest.Config` 映射这四项字段，未从模型工具参数或公网 payload 接受解析器策略。知识库的请求级导入路径已使用同一 `OfficeReadConfig` 形态，完整知识库回归通过。

复核 SimpleLLMExecutor 后还发现其附件建模边界仍调用默认 provider，因而会绕过上述请求级策略。现已改为使用同一显式 BuildUserContentWithAttachmentStagingDirAndOfficeReadConfig 路径；服务端所有内置执行器的自动附件抽取和 read_document 现均从受信任的 ExecuteRequest.Config 取得 OfficeRead 策略，且不接受模型参数指定策略。

已执行 go test ./corelib/agentservice -count=1 -timeout 15m，通过。

新增 `TestBuildConversationUsesRequestScopedOfficeReadPolicyForAttachments`：在进程级 provider 明确返回 `legacy` 的前提下，构造 `SimpleLLMExecutor` 的真实 DOCX 附件会话，并以内容无关的迁移观测确认请求 `AppConfig` 的 `officeread + docx` 策略被用于自动附件提取。该用例直接覆盖此前修复的旁路，不再仅以 CoreAgent 的 `read_document` 回归间接证明。

新增回归覆盖显式策略不会改写默认 provider，以及 `legacy` 与 `officeread/docx` 对同一 DOCX 的缓存结果隔离；已有 Agent 服务回归验证 `read_document` 使用请求配置，并保持私有附件 staging 与工作区路径限制。

```powershell
go test ./corelib/agent -count=1 -timeout 15m
go test ./corelib/agentservice -count=1 -timeout 15m
go test ./corelib/knowledge -count=1 -timeout 15m
git diff --check -- corelib/agent/attachment.go corelib/agent/file_path_expand.go corelib/agent/officeread_adapter.go corelib/agent/officeread_adapter_test.go corelib/agent/tools_office_read.go corelib/agentservice/core_agent_executor.go corelib/agentservice/core_agent_executor_test.go
```

上述仓内回归均已通过。真实 `internal_authorized` 六格式 dual 语料、DOC/PPT 人工质量复核、P1/P2 资源审阅、GUI 人工烟测、实际格式级回滚留痕及稳定发布周期后的旧解析器退役仍为仓外发布门禁，未在此标记为完成。

### Agent 图片缩略图的浏览器解码边界（2026-08-11）

继续复核“Agent 可检索并显示图片、但不暴露主机路径”的展示末端后，收紧了 WebView 对
`[KB_IMAGE:asset_id|data:image/jpeg;base64,...]` 的消费契约。前端现在仅接受当前的两字段 marker、
受限字符集的 opaque asset ID，以及不超过 256 KiB 的 JPEG data URL；旧的第三路径字段、远程 URL、
非图片 data URL、无效 base64 和超过 120×120 的 JPEG 帧均不会进入 `<img>`。这避免模型回复在渲染时
发起网络请求，或重新把本地路径带回打开接口。

缩略图通过 JPEG 头部和尺寸做同步筛选，但不把该筛选视为完整解码：只有浏览器触发图片 `onLoad` 后，
点击才会以 opaque asset ID 调用 `KnowledgeOpenImageAsset`；解码失败时缩略图被移除，且不能打开任何资产。
因此即使模型构造了形似 JPEG 的 marker，也不能将浏览器尚未成功显示的字节提升为本地文件打开动作。

```powershell
cd gui/frontend
& 'C:\Users\ma139\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe' 'node_modules\vitest\vitest.mjs' run 'src/components/ai/aiAssistantMarkdown.test.tsx' --reporter=dot
& 'C:\Users\ma139\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe' 'node_modules\typescript\bin\tsc'
& '.\node_modules\.bin\vite.cmd' build
git diff --check -- gui/frontend/src/components/ai/aiAssistantMarkdown.tsx gui/frontend/src/components/ai/aiAssistantMarkdown.test.tsx
```

上述前端定向测试 120 项、TypeScript 与生产构建均已通过；生产构建仍会报告既有的大 bundle 警告，
并非本次图片 marker 引入的失败。真实 `internal_authorized` 六格式 dual 语料、DOC/PPT 人工质量复核、
P1/P2 资源审阅、GUI 人工烟测、实际格式级回滚留痕及稳定发布周期后的旧解析器退役仍为仓外发布门禁，
未在此标记为完成。

### OfficeRead 自带样本的六格式回归复验（2026-08-11）

确认 OfficeRead 工作区 `D:\workprj\OfficeRead\testdata` 包含可用的六格式样本：`samples` 目录分别有
`.doc` 160、`.docx` 163、`.xls` 395、`.xlsx` 350、`.ppt` 121、`.pptx` 100 个文件，另有独立的负向样本集。
这些是上游回归样本而非 MaClaw 的真实内部业务文档，故其 provenance 必须为 `fixture`，不能标记成
`internal_authorized` 或用于发布许可。

本轮从样本集中为每个格式选择五份包含正文、图片、表格、批注、页眉页脚或演示文稿内容的代表文件，设置
`MACLAW_OFFICE_READ_ENGINE=dual` 和六格式白名单后运行 MaClaw 的 `office-read-dual-report`。30 份输入全部
由 OfficeRead 成功抽取；作为旧解析器兼容性观测，DOC/DOCX/XLS/XLSX/PPTX 的部分样本具有 legacy 结果，
历史 PPT 没有 legacy reader，符合迁移设计。命令产物位于工作区根目录的
`office-read-samples-six-format-dual-report.json`，仅包含 opaque sample ID 与聚合计数，不包含样本路径或正文。

同时在 OfficeRead 工作区执行了其自身的六格式正文、Markdown、图片、复杂样本及负向样本回归：

```powershell
cd D:\workprj\OfficeRead
go test . -run '^(TestExtractDownloadedSamples|TestSampleImagesAreValidAndReferenced|TestSampleMarkdownQuality|TestComplexSampleExpectations|TestNegativeSamplesDoNotPanic)$' -count=1 -timeout 20m

cd D:\workprj\aicoder
$env:MACLAW_OFFICE_READ_ENGINE = 'dual'
$env:MACLAW_OFFICE_READ_FORMATS = '.doc,.docx,.xls,.xlsx,.ppt,.pptx'
go run ./cmd/office-read-dual-report <30 个 -input OfficeRead 样本> -provenance fixture -min-samples 1 -min-token-hit 0.85 -out .\office-read-samples-six-format-dual-report.json
go run ./cmd/office-read-dual-report -audit .\office-read-samples-six-format-dual-report.json -required-formats doc,docx,ppt,pptx,xls,xlsx -min-samples 1 -min-token-hit 0.85 -allow-fixture-automation -enforce-audit
```

OfficeRead 自身回归与样本双读均通过；上述带 `-allow-fixture-automation` 的 audit 命令按预期通过，仅声明
`fixture_automation_ready`。去掉该显式 fixture profile 后，默认生产审计仍会以非零退出，首要发布资格根因是
`sample_provenance_is_not_internal_authorized`（各格式的 fixture assessment 也会因此不能为 `pass`），并继续列出四项人工门禁。这说明样本集已补齐六格式的仓内
功能、富内容和异常处理覆盖，但不会替代真实业务语料、人工质量/资源/UI/回滚验收或稳定发布周期。

为避免手工挑选样本或错误地改变 provenance，已提供可重复的本地辅助脚本：

```powershell
# 若本机 PowerShell 执行策略禁止直接运行 .ps1，使用一次性进程策略；
# 不会修改用户或机器级执行策略。
powershell -ExecutionPolicy Bypass -File .\scripts\test-officeread-fixtures.ps1 -OfficeReadRoot D:\workprj\OfficeRead -RunUpstreamTests -OverwriteReport
```

脚本固定选择每种格式五份代表性上游样本，强制 `dual` 和六格式白名单，产生仅含 opaque ID 与聚合指标的
`fixture` 报告，并以显式 `-allow-fixture-automation -enforce-audit` 选择自动回归 profile。审计器逐文件重算
summary/assessment：有 legacy 基线的格式必须达到 OfficeRead 成功数和 token 覆盖阈值；没有 legacy reader 的
`.ppt` 则必须全部由 OfficeRead 成功提取。默认生产 profile 仍会拒绝该报告，且 `quantitative_ready` 始终为 false；
fixture 自动通过只会写入独立的 `fixture_automation_ready`，不会把公开样本伪装为 `internal_authorized` 发布证据。
### OfficeRead 上游语料的无人值守验收结果（2026-08-11）

已实际执行以下单一自动化命令，无需人工挑选样本、填写回执或介入结果判断：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\test-officeread-fixtures.ps1 -OfficeReadRoot D:\workprj\OfficeRead -RunUpstreamTests -OverwriteReport
```

结果：OfficeRead 上游正向/Markdown/图片/复杂样本/负向测试通过（`ok officeread`，62.774 秒）；脚本从固定样本中生成的 six-format `dual` 报告也通过 fixture 自动验收。报告记录了 30 份输入全部 OfficeRead 成功提取（每个 `.doc`、`.docx`、`.ppt`、`.pptx`、`.xls`、`.xlsx` 各 5 份）；对有 legacy 基线的格式，实际 OfficeRead token coverage 分别为 DOC 1.000、DOCX 1.000、PPTX 0.906、XLS 0.870、XLSX 1.000，均不低于脚本设定的 0.85。PPT 无 legacy 基线，5/5 成功满足自动 profile 的全成功要求。可复核产物为 `office-read-samples-six-format-dual-report.json`。
### 移动端上传到桌面 OfficeRead 策略的端到端闭环（2026-08-11）

补充 `TestProcessMobileDocumentUploadTaskUsesPersistedOfficeReadPolicy`，以本地 HTTP Hub 模拟完整的
`GET source → 私有临时文件 → ExtractOfficeText → PATCH result` 流程。测试将进程级 provider 预置为
`legacy`，再保存 GUI 的 `officeread + docx` 策略；真实 DOCX 上传仍由 OfficeRead 成功抽取，并以
`ready` Markdown 回传。该用例同时验证下载与结果回传的 machine auth，不依赖仅覆盖纯函数的测试。

```powershell
go test ./gui -run '^TestProcessMobileDocumentUploadTaskUsesPersistedOfficeReadPolicy$' -count=1 -timeout 10m
```

这证明移动端 Office 任务不会因进程中较早安装的 provider 绕过当前 GUI 持久化策略；它不替代真实业务文档的
质量、资源、GUI 手工烟测和发布回滚门禁。
### 移动端 Office 上传的加密容器边界（2026-08-11）

Mobile/桌面 worker 对六格式 Office 上传复用 `ExtractOfficeText`，而不是独立解压或把二进制按 UTF-8 处理。
新增 `TestMobileDocumentOfficeMarkdownRejectsEncryptedDocumentWithoutFallback`：在 `legacy` 全局回滚（即使配置仍保存
`docx` 和 `fallback=true`）时，带 ZIP 加密位的 DOCX 仍在共享预检阶段被拒绝；它不会进入 legacy 解析或 fallback，
也不会产生已启用 OfficeRead 灰度的迁移观测。密码不是 Mobile 任务或 `read_document` 的输入；提供密码也不会启用解密。

```powershell
go test ./gui -run '^TestMobileDocumentOfficeMarkdownRejectsEncryptedDocumentWithoutFallback$' -count=1 -timeout 10m
```

该回归与核心 adapter 的加密容器定向测试共同覆盖仓内 fail-closed 契约。真实授权语料、人工质量/资源/UI
验收、实际格式级回滚留痕与稳定发布周期仍是仓外发布门禁。
### dual-report 六格式发布证据链复核（2026-08-11）

复核第 7 节的量化审计与仓外人工门禁衔接后，确认 `office-read-dual-report` 的发布证据链已在仓内形成严格边界：审计器仅接受 `dual`、`internal_authorized`、UTC 时间戳且逐格式重算的报告；调用方可用 `-required-formats doc,docx,ppt,pptx,xls,xlsx` 强制六格式范围。报告、summary、assessment、样本格式和 opaque `sample_id` 任一缺失、重复、非规范或与逐样本指标不符均会阻止 `quantitative_ready`。历史 `docs/office-read-dual-report-2026-08-09.json` 明确为 `fixture`、仅覆盖 `doc/ppt/xls` 且存在不足/失败评估，不能用作发布证据。

人工门禁回执与报告原始字节摘要绑定，固定要求文本顺序/业务问答、P1/P2 资源诊断、GUI 文件选择/附件/工具/连续对话/fallback 烟测和格式级回滚演练四项；审核人、格式集合、时间先后与可选报告时效均严格校验。`-enforce-release-evidence` 只接受“量化审计通过且回执结构完整”，不把回执自动解释为人工工作已经完成或发布获得批准。

```powershell
go test ./cmd/office-read-dual-report -count=1 -timeout 10m
go test -race ./cmd/office-read-dual-report -count=1 -timeout 12m
git diff --check -- cmd/office-read-dual-report/main.go cmd/office-read-dual-report/main_test.go
```

上述回归通过。这证明仓内量化与人工门禁留痕机制可执行；真实 `internal_authorized` 六格式 dual 语料、DOC/PPT 文本顺序与关键 token 的人工复核、P1/P2 资源审阅、GUI 人工烟测、实际格式级回滚留痕和稳定发布周期后的旧解析器退役仍是仓外门禁，未在此标记为完成。
