# Agent 动态 UI 运行时落地状态

日期：2026-05-09

## 当前完成度

核心链路约 95% 完成。现在已经不是“展示 schema/源码”的预览，而是由后端发出受控 AgentView 描述，右侧 `AgentTaskPanel` 渲染真实可操作控件，提交后回传结构化数据，再由运行时校验、合并、审批并继续执行原 tool/skill/MCP/MIS 流程。

## 已落地能力

- 右侧任务面板支持 `form`、`wizard`、`table_editor`、`resource_picker`、`field_mapper`、`approval`、`progress`、`result_browser`、`artifact`。
- 前端控件支持文本、多行文本、数字、日期、布尔、单选、多选、隐藏字段、对象字段组、数组明细表，并在提交前做必填和嵌套字段校验。
- 后端通过 `agent-view` / `agent-view-clear` 事件驱动右侧面板；前端提交走 `SubmitAgentView`，关闭走 `DismissAgentView`。
- AgentView 生命周期协议已开始正式化：后端统一发出兼容旧事件的 `agent-view:lifecycle`，前端识别 `open/update/submit/dismiss/error/complete`。
- 标准 skill 文件不需要修改。运行时 sidecar/adaptive 层会根据已有参数、步骤、隐式 required 和运行上下文生成表单。
- `run_skill` 缺少必填参数时会打开右侧表单；多 operation skill 会自动加入 operation 选择控件；提交后重新校验和归一化，再启动 skill，并持续发出 progress/result 视图。
- MIS 动态业务对象链路已支持：语义意图识别、候选选择、业务表单、事务暂存、dry-run 校验、审批确认、提交落库、结果和审计展示。
- 业务对象识别不走关键词硬匹配，而走统一意图/语义信号和置信度决策；例如差旅、餐费、金额、凭证附件等组合可被识别为报销提交意图。
- 通用 registered tool 已接入同一机制：缺少 required 参数时自动拦截并打开右侧表单；非法参数不会进入 handler，而是打开修正 UI。
- MCP tool 校验失败时会打开目标 tool 的参数表单，字段错误会标到对应控件，提交后通过 `mcp:call` 回流继续执行。
- JSON Schema 适配已支持：
  - `array + items.properties` 渲染为 `array_table` 明细表。
  - `array + items.enum` 渲染为 `multiselect`。
  - `object + properties` 渲染为 `object_form`。
  - 嵌套 required、enum、const、uniqueItems、dependentRequired、additionalProperties、schema variants 等约束会在后端复验。
- `x-agent-view` / `ui:widget` 注解可触发 `resource_picker` 和 `field_mapper`，用于资源选择、字段映射等复杂输入。
- 高风险 registered tool 已接入审批门禁：运行时先检查 `SecurityFirewall` / `PolicyEngine`，需要确认时右侧打开 `approval` 面板；用户批准后记录 session approval 并回流执行原 tool，拒绝则清理 pending 状态并记录审计。
- Tool/MCP/Skill AgentView 已带 schema 版本信息：`meta.schemaVersion` 和隐藏字段 `_agent_view_schema_version` 会随表单生成和提交流转，用于后续缓存、审计和兼容判断。

## 运行时流程

```text
Agent 选择 tool / skill / MCP / MIS action
  -> runtime precheck
  -> 语义意图或 schema/contract 解析
  -> 缺少或非法结构化输入？
  -> emit agent-view 到右侧 Task Panel
  -> 用户操作真实 UI 控件
  -> SubmitAgentView 回传结构化数据
  -> 后端复验、归一化、合并上下文
  -> dry-run / approval / handler execution
  -> emit progress / result_browser / artifact
  -> agent-view-clear 或保留结果视图
```

## 边界原则

- 右侧界面只渲染受控组件，不渲染模型生成的 HTML/JSX/源码。
- 标准 skill、标准 tool、MCP server 本体不被改写；适配发生在 runtime sidecar 层。
- 参数 UI 可以由 JSON Schema、Skill contract、MIS metadata、运行时错误和语义意图共同推断。
- 写业务库、审批、删除、发布、外部付费调用等动作必须经过校验、确认和审计。
- 用户输入返回结构化数据，不再要求用户手写 JSON，从而支持传统 MIS 表单类流程被 Agent 动态接管。

## 本轮验证

- `go test -p=1 -vet=off ./gui -run "AgentView|RegisteredTool|MCPTool|MISData|TestToolRunSkill_OpensAgentViewForMissingRequiredParams|TestRegisteredToolToDef|PendingUserReplyPromptCandidate" -count=1 -timeout=300s -v` 通过。
- `go test -p=1 -vet=off ./gui -run "TestHandleRegisteredToolApprovalSubmitRunsApprovedTool|TestBuildRegisteredToolApprovalAgentViewCarriesApprovalID|TestExecuteToolOpensAgentViewForInvalidRegisteredToolArgs|TestHandleRegisteredToolAgentViewSubmitMergesAndRuns" -count=1 -timeout=300s -v` 通过。
- `npm test -- AgentTaskPanel.test.tsx` 通过，23 个测试全部通过。
- `npx tsc --noEmit --pretty false` 通过。
- `npm test -- useAIAssistant.test.ts -t "lifecycle"` 通过，覆盖生命周期 open/dismiss/complete。
- `npm test -- AIAssistantPanel.test.tsx -t "AgentView"` 通过，覆盖右侧预览区渲染真实可操作表单并提交结构化数据。
- Go 回归已覆盖 schema version 注入：registered tool、MCP tool、standard skill 表单均携带版本化 schema metadata。

## 剩余工作

- 将 AgentView 生命周期事件正式化为 `open/update/submit/dismiss/error/complete` 的统一协议，减少不同入口的分支判断。
- 增加 Wails/浏览器 E2E，验证真实右侧预览区在桌面端渲染、交互和清理行为。
- 扩展更多企业业务对象模板，例如采购申请、合同审批、客户建档、工单派发、库存调整。
- 扩展 UI schema 缓存策略，从当前版本签名缓存推进到跨会话持久缓存和失效策略。
