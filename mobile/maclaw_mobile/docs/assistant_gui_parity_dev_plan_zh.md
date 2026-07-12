# MaClaw Mobile AI 助手对齐 GUI 开发计划

## 1. 背景与目标

### 1.1 问题

当前 Mobile「AI助手」观感接近**搜索结果直出**，而不是 MaClaw GUI 中的**智能伴侣对话**：

- 回复缺少结论 / 表格 / 分点等结构化整理；
- 引用区以大卡片 + 原始 snippe为主视觉，含未解码 HTML 实体（如 `&ensp;`）；
- 客户端用纯 `Text` 渲染，即使服务端返回 Markdown 也无法呈现表格。

### 1.2 目标（体验）

对齐 GUI 的**可读交付形态**，而非复刻整套桌面 Agent：

1. 主内容是助手整理后的回答（结论 → 结构化要点/表格 → 可选补充）；
2. 来源是轻量脚注/可折叠列表，不抢主阅读；
3. 多轮仍像「和伴侣聊天」，不是填表或 SERP。

### 1.3 非目标（明确不做）

| 不做 | 原因 |
|------|------|
| Fluer 嵌入 Go `corelib`（FFI / gomobile / `.so`） | 产品运行时边界；`tool/verify_runtime_boundary.py` 发布门禁 |
| 手机本地跑完整 `corelib/agent` 循环与桌面工具 | 体积、安全、凭据、SSH/文件依赖 |
| 任意第三方 LLM 配置面 / 自定义 Hub URL | 现有 Mobile 产品规则 |
| 完整 GUI 流式 UI / 复杂 AgenView 控件移植 | 可二期；一期以「总结质量 + 可读渲染」为主 |

### 1.4 架构原则

```
corelib / GUI Agen──可在服务端复用──►Hub（Go）
│
│HTTP JSON（Markdown answer + citations）
▼
Fluer Mobile
（渲染 + 清洗 + 聊天壳）
```

- **复用 corelib 逻辑 = 在 Hub（或远程数字员工 / GUI agent）进程内**，不是进 APK。
- Mobile 只扩展 API 消费与 UI；继续通过 `verify_runtime_boundary`。

---

## 2. 现状对照

| 层级 | 现状 | GUI 侧参考 |
|------|------|------------|
| API | `POST /api/mobile/search` | GUI 走本地 agen+ `corelib` |
| Promp| 紧急助手、要求简洁 | 完整 system promp+ 工具与结构化输出 |
| 回答生成 | 单次非流式 chacompletions | 流式 + 多轮 + 工具 |
| 兜底 | `mobileSearchAnswer` 拼检索列表 | 无「SERP 列表当答案」 |
| 客户端 | `Text(answer)` + 大引用卡 | Markdown 表格/标题/列表渲染 |
| 边界 | 禁止嵌入 corelib | GUI 直接 impor`corelib/agent` |

关键代码：

- Hub：`hub/internal/hpapi/mobile_handlers.go`（`MobileSearchHandler`、`mobileSearchPrompt`、`mobileSearchAnswer`、citations）
- Mobile：`lib/features/assistant/assistant_screen.dart`、`assistant_controller.dart`、`lib/core/api/api_client.dart`
- 边界：`mobile/maclaw_mobile/tool/verify_runtime_boundary.py`

---

## 3. 分阶段计划

### Phase 0 — 基线与契约（0.5～1 天）

**目的**：锁定接口契约与验收样例，避免前后端各改各的。

| 任务 | 产出 |
|------|------|
| 冻结响应字段 | 继续 `answer` / `citations[]` / `llm_*`；可选新增 `answer_format: "markdown"`（向后兼容） |
| 写 3～5 个「期望回答形态」样例 | 天气查询、状态排查、对比类问题：应含结论 + 表格或分点 + `[n]` 引用 |
| 确认边界门禁 | CI/本地跑 `pythonool/verify_runtime_boundary.py` 必须绿 |
| 记录对比基线 | 当前截图类问题（搜索直出 + HTML 实体）作为 Before |

**验收**：样例文档合入 `docs/`；门禁脚本通过。

---

### Phase 1 — 呈现急救（Fluer，1～2 天）【优先用户可见】

**目的**：即使后端仍一般，也先去掉「搜索结果页」观感。

| # | 任务 | 主要文件 | 说明 |
|---|------|----------|------|
| 1.1 | Snippe/ 标题 HTML 清洗 | 新建 `lib/core/text/html_text.dart` 或放 `assistant_screen` 旁 | `HtmlUnescape` 等价：`&ensp;`、`&#0183;`、`&amp;` 等；可选剥简单标签 |
| 1.2 | 来源折叠 | `assistant_screen.dart` | 默认「参考 N 条」Expansion；展开后再显示链接与操作 |
| 1.3 | 引用卡降噪 | `_CitationTile` | 默认一行标题 + 域名；snippe单行截断；操作收进 overflow 菜单 |
| 1.4 | 回答区优先 | `_AssistantReplyBubble` | 主气泡只突出 answer；分享/复制/草稿保留在回答级 |
| 1.5 | 测试 | `test/assistant_screen_test.dart` 等 | 覆盖 unescape、折叠默认态、复制/分享仍可用 |

**依赖**：无 Hub 发布依赖，可先发 App。

**验收**：

- [ ] 界面不再出现裸露 `&ensp;` / `&#…;`
- [ ] 首屏主阅读是回答（或问候），不是半屏来源卡
- [ ] 既有 citation 复制/分享/整理草稿路径不回归
- [ ] `fluerest` 相关用例通过

---

### Phase 2 — 后端总结质量（Hub，2～3 天）【质量关键】

**目的**：让 `answer` 成为「整理后的助手回复」，而不是检索片段拼接。

| # | 任务 | 主要文件 | 说明 |
|---|------|----------|------|
| 2.1 | 重写 `mobileSearchPrompt` | `mobile_handlers.go` | 角色：专业工作伴侣；强制输出结构（见下） |
| 2.2 | Snippe入模前清洗 | 同文件 + 可选 `websearch` 适配层 | 写入 promp的 snippe先 unescape/截断，降低模型复读 HTML |
| 2.3 | 弱化/改写兜底 `mobileSearchAnswer` | 同文件 | 禁止「已为你检索 + 标题：snippet」当最终体验；兜底改为简短说明 + 请重试/换问法 |
| 2.4 | LLM 失败策略 | `MobileSearchHandler` | 区分 SEARCH_FAILED vs MOBILE_LLM_FAILED；避免把 SERP 列表当成功 answer |
| 2.5 | 单测 | `mobile_handlers_test.go` | promp含结构约束；citations 清洗；兜底不含原始 SERP 列表形态 |
| 2.6 | （可选）轻量对齐 corelib 文案块 | Hub 内抽函数或复制 **promp片段** | **仅 Go 服务端**；禁止引入 Fluer 对 corelib 的依赖 |

**推荐输出结构（写入 prompt，中英均可）：**

```text
1. 结论（2～4 句，直接回答用户）
2. 结构化要点（优先 Markdown 表格；否则有序列表）
3. 风险/注意（如有）
4. 来源：仅使用 [1][2]… 编号，不要大段粘贴网页原文
禁止输出 HTML 实体与标签。
```

**依赖**：Hub 部署；与 Mobile Phase 1 可并行。

**验收**：

- [ ] 对「北京天气」类问题，answer 含可读结论，而非仅站点 snippe堆叠
- [ ] answer 中尽量出现 Markdown 表格或清晰分点（抽样人工评测 ≥ 约定比例）
- [ ] `goest` 覆盖 mobile search 相关用例
- [ ] 不破坏 `llm_request_id` / credits / 官方 LLM 路径

**明确不做（本阶段）**：在 Hub 内嵌完整 GUI agen工具环；流式 SSE（可 Phase 4）。

---

### Phase 3 — Markdown / 表格渲染（Fluer，2～3 天）

**目的**：服务端结构化 Markdown 在手机上真正「看得见」。

| # | 任务 | 主要文件 | 说明 |
|---|------|----------|------|
| 3.1 | 依赖 | `pubspec.yaml` | 增加 Markdown 组件（如 `fluer_markdown` 或等价；评估表格支持） |
| 3.2 | 主题化渲染 | 新建 `lib/features/assistant/assistant_markdown.dart` | 使用 `MaClawColors` / `Theme`；限制字号行高；代码块可横向滚动 |
| 3.3 | 接入气泡 | `_AssistantReplyBubble` | answer 走 Markdown；失败回退纯文本 |
| 3.4 | 安全 | 同模块 | 禁用原始 HTML；链接点击可先复制/外开策略与现有一致 |
| 3.5 | 测试 | widget/golden 或文本结构测 | 表格行、列表、标题可测性；密钥脱敏仍走现有 redaction |

**验收**：

- [ ] Markdown 表格在真机/模拟器可读（非 monospaced 乱糊）
- [ ] 长链接/长表不撑破布局
- [ ] `verify_runtime_boundary` 仍绿（新依赖不得引入 corelib/FFI）
- [ ] 分享/复制仍导出纯文本/Markdown 字符串

---

### Phase 4 — 对话深度对齐（可选，3～5 天+）

在 1～3 稳定后按需排期：

| 项 | 说明 | 优先级 |
|----|------|--------|
| 4.1 流式输出 | Hub SSE/chunk + 移动端打字机；改善「等待感」 | 中 |
| 4.2 多轮 system 强化 | 将 recenmessages 以 role 消息数组提交，而不仅是 `context: string[]` | 中 |
| 4.3 任务型卡片 | 审批/文档草稿/员工任务用轻量 inline card（参考 GUI InlineChatCard 思路，**Dar重实现**） | 低～中 |
| 4.4 服务端 agen子集 | Hub 调用有限工具（仅 web/search），**仍在服务端**，不进手机 | 低（视业务） |

**禁止**：为 Phase 4 放宽 runtime boundary。

---

## 4. PR / 交付切分建议

| PR | 范围 | 可独立合并 |
|----|------|------------|
| **PR-A** | Phase 1 全文清洗 + 来源折叠 + 测试 | 是 |
| **PR-B** | Phase 2 Hub prompt/清洗/兜底 + Go 测试 | 是（需 Hub 发布） |
| **PR-C** | Phase 3 Markdown 渲染 + 边界检查 | 是（最好在 B 后体验最佳） |
| **PR-D** | Phase 4 流式或多轮协议（可选） | 是 |

依赖关系：`A ∥ B` → `C` → `D`。

---

## 5. 测试与质量门禁

### 5.1 自动化

| 层 | 命令 / 范围 |
|----|-------------|
| Mobile unit/widge| `fluerest`（重点 assistan/ api_clien/ redaction） |
| Hub | `goes./hub/internal/hpapi/ -run MobileSearch`（及关联） |
| 边界 | `pythonool/verify_runtime_boundary.py` |
| 回归冒烟 | `test/app_smoke_test.dart`、登录与助手主路径 |

### 5.2 手工验收清单

1. 冷启动 → 登录 → AI助手问候（伴侣感，非表单墙）。
2. 问「北京今天天气」：主回答为总结；来源默认折叠；无 HTML 实体。
3. 多轮追问：上下文仍连贯。
4. 引用展开后复制链接/引用可用。
5. 分享结果 / 整理为草稿可用且脱敏。
6. 助手不可用（feature 关）时发送禁用逻辑不变。
7. 深色模式对比度可接受。

### 5.3 发布

- 按现有 `docs/release_checklist.md` / QA build record。
- 不得为「对齐 GUI」引入 corelib/FFI/gomobile。
- Hub 与 App **可分开发布**：先 App(A) 改善观感，再 Hub(B) 提升内容，再 App(C) 吃满 Markdown。

---

## 6. 风险与缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| Promp变严后部分模型不遵从 | 仍像散文或仍复读 | 样例回归集 + 截断 snippe+ 明确「禁止粘贴原文」 |
| Markdown 库表格能力弱 | 表格仍难看 | 选型时验证 GFMable；必要时服务端优先列表 |
| Hub 与旧 App 兼容 | 老客户端纯文本 | answer 保持字符串；Markdown 纯文本退化仍可读 |
| 误把 corelib 链进 Fluer | 发布失败 | boundary 脚本进 PR CI |
| 范围膨胀成「手机 Agent」 | 延期 | 非目标写死；Phase 4 单独评审 |

---

## 7. 人力与工期粗估

| 阶段 | 建议工期（1 名全栈 / 前后端各 1） |
|------|----------------------------------|
| Phase 0 | 0.5～1 天 |
| Phase 1 | 1～2 天 |
| Phase 2 | 2～3 天 |
| Phase 3 | 2～3 天 |
| Phase 4 | 另排，3～5 天+ |
| **MVP（0～3）** | **约 1～1.5 周** |

---

## 8. 建议执行顺序（决策）

1. **先做 Phase 1**（用户立刻不再看到 HTML 实体与 SERP 墙）。
2. **并行 Phase 2**（Hub 总结质量，这是与 GUI「整理感」差距的主因）。
3. **再做 Phase 3**（Markdown/表格，放大 Phase 2 收益）。
4. Phase 4 按反馈排期。

**corelib 策略（冻结）**：

- 不接入 Mobile 二进制；
- 可在 Hub 侧**借鉴/复用** prompt、过滤、整理思路（Go 代码级）；
- 重活继续数字员工 / GUI agent；
- - Dar仅移植「展示与清洗」级小逻辑。

---

## 9. 成功标准（MVP 完成定义）

同时满足：

1. 同一类查询，主屏是**助手总结**，不是搜索片段墙；
2. 结构化信息（表格或清晰分点）在多数抽样中出现；
3. 来源默认不抢戏，且 snippe干净；
4. 运行时边界与现有助手功能（多 Tab、语音、附件、历史、脱敏）无回归；
5. 文档与测试更新完毕，可按现有流程出 QA 包。

---

## 10. 下一步

确认本计划后，建议从 **PR-A（Phase 1）** 开工，并同步开 **PR-B（Phase 2 Hub）** 分支，避免 App 只改皮、服务端仍吐 SERP 文案。

---

## 11. 实施进度（2026-07-11）

| 阶段 | 状态 | 说明 |
|------|------|------|
| Phase 1 | 已完成 | `html_text.dart` 清洗；来源折叠；引用卡降噪 |
| Phase 2 | 已完成 | Hub `mobileSearchPrompt` 结构化；snippe清洗；兜底不再 SERP 堆砌 |
| Phase 3 | 已完成 | `fluer_markdown` + `AssistantMarkdownBody` 表格/标题渲染 |
| Phase 4 | 已完成（核心） | 多轮 `messages[{role,content}]` 协议；官方/第三方 `stream=true` 时尽量转发上游 OpenAI SSE delta；无上游流时仍 chunk 假流；Mobile 打字机气泡 |
| Phase 4.3 | 已完成 | Dar`AssistantInlineCard` +「可以继续」；**派给员工** 经 `assistantEmployeeHandoff` 预填任务草稿 |
| Phase 4.4 | 已完成（Hub 子集） | Hub 侧 agen循环：`web_search` / `web_fetch`；SSE `tool_call`/`tool_result`；Mobile 工具调用条；**无** bash/读盘/GUI 全量工具（边界：不进 APK） |
| 质量门禁 | 已通过 | Hub MobileSearch* 测试；Fluer assistant/stream/markdown/html/screen 测试；`verify_runtime_boundary` |
| Release APK | 已重建 | `app-release.apk`（含 4.3 + 员工交接）；SHA256 `F81A91C708B9EEE1E16E9A349911D520C059289FC276121F47D8E450680730F6` |
| Hub 包 | 已本地打包 | `hub/package/maclaw-hub/`；exe SHA256 `38D1F3864E249634C426272CA6F982698C69C668FD79C74DB45C75E06789128D` |
| 发布交接 | 已写 | `docs/assistant_gui_parity_release_handoff_zh.md`（APK + Hub 产物哈希、部署步骤、真机清单） |
| 只读预检 | 已加 | `tool/verify_assistant_parity_release.py`（离线哈希 + 可选 live healthz/search） |

### 后续可选（未做）

- Phase 4.4 Hub 侧有限 agen工具子集（低优先级；联网搜索已在 Hub 侧）
- 按 handoff **把 Hub 包推到预发/生产** 与真机勾选（需运维环境与真机；本环境不自动部署）
