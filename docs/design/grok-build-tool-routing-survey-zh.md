# Grok Build 工具路由策略源码调查报告

- 调查对象：`xai-org/grok-build`（SpaceXAI 的终端编码 agent，Rust）
- 证据基线：开源同步提交 **`a28ee2b`（2026-09-17）**；此后 `main` 上的后续同步（截至 2026-09-19）也作为补充引用
- 调查方式：直接抓取 GitHub raw 源码与 Contents API，**全部结论均带文件路径 + 原样引用**
- 上位文档关系：本文是 `docs/design/tool-routing-improvement-plan-zh.md` §2「Grok Build 路由机制摘要」的**独立复核与展开**。第 9 节列出对该文档的事实校正，请以本文为准
- 可信度标记：✅=本文当场逐字核对；🟡=由子代理核到但未二次复核或仅部分对应（已在文中注明）

---

## 0. 一句话结论

Grok Build 的工具路由不是"先检索再给模型看"的语义路由，而是 **"全量注册 + 按名字精确分发 + 两级延迟发现 + 单点权限闸门"**：

> 工具**一次性全部进上下文**（第一方裸名），MCP 集成工具**不进上下文**，靠 `search_tool`（BM25）按需发现、再用 `use_tool` 元工具二次分发执行；权限不散落在工具面渲染期，而是在**执行点由单一策略层**求值。

这与本程序的"语义规划器决定给模型看哪些工具"是两条相反的路线：Grok Build 把成本转嫁给**上下文与模型的自我纠正能力**，本程序把成本转嫁给** retrieval/planner 的正确性**。

---

## 1. 分层结构

| 层 | 位置 | 职责 | 证据 |
|---|---|---|---|
| `ToolRegistryBuilder` | `crates/codegen/xai-grok-tools/src/registry/types.rs` | 进程级注册：类型 → `<namespace>:<id>` → `ToolEntry`（含类型擦除的分发句柄） | ✅ |
| `FinalizedToolset` | 同上 | 会话级不可变工具集：`RwLock<Vec<FinalizedTool>>` + `LocalRegistry` 句柄 + reminders | ✅ |
| `LocalRegistry` | 外部 crate `xai_computer_hub_sdk` | 进程内执行注册表：`ToolId → 可执行句柄` | ✅（外部依赖，**不在本开源树内**） |
| `ToolIndex` | shared resource | 已连接 MCP server 的工具索引，`search_tool` 的检索后端 | ✅ |
| permission 层 | `crates/codegen/xai-grok-workspace/src/permission/` | 规则解析/合并/审批/grant 账本 | ✅ |

```rust
/// Toolset produced by `ToolRegistryBuilder::finalize()`. The tools vector is wrapped in `parking_lot::RwLock` to allow
/// concurrent read access (tool dispatch) with rare write access (MCP tool registration). The read guard is held only
/// for microsecond lookups — never across `.await`.
```
（`registry/types.rs`）

---

## 2. ID 体系：三级名字，注册期就钉死

每个 `FinalizedTool`（私有 struct）同时持有**四个名字**，这是整套路由的骨架：

```rust
struct FinalizedTool {
    namespace: String,     // GrokBuild / OpenCode / Codex / MCP
    id: String,            // 未限定短 id，如 read_file
    /// The key under which this tool is stored in the `LocalRegistry`. For built-in tools this
    /// equals `id`; for dynamically-registered (MCP) tools it is `Tool::id().as_str()` which may
    /// differ from the client-facing `id` / `client_name`.
    registry_id: String,
    client_name: String,   // ← 模型唯一能看到的那个名字
    ...
}
```

- 完全限定 ID = `format!("{}:{}", tool.tool_namespace(), Tool::id(&tool))`，例如 `GrokBuild:read_file`；
- **模型可见名 = `client_name`**，由 `ToolConfig::resolve_client_name` 决定：`name_override` 优先，否则回退未限定短 id（`"read_file"`）；
- 存在**参数级**重命名反向映射：
  ```rust
  /// Client-facing param → canonical param, for reverse-remapping at dispatch.
  reverse_params: HashMap<String, String>,
  ```
  分发时 `remap_json_keys(tool_args, &reverse_params)` 还原成 canonical 参数名。

⚠️ **命名空间只对注册/类型系统有意义，对模型不可见**：MCP 动态工具的 `ToolMetadata::tool_namespace()` 返回 `ToolNamespace::MCP`，但模型看到的是 `linear__save_issue`。

---

## 3. 分类学：`ToolKind` 37 个变体，穷尽匹配强制自分类

`crates/codegen/xai-grok-tools/src/tool_taxonomy.rs`（定义体在同 crate 的 `types::tool`）。

```rust
/// Whether this kind only reads (no workspace or external mutation) by default. The kind-level
/// default for `ToolMetadata::is_read_only`, which individual tools may override. Exhaustive
/// (no `_`) so a new kind must classify itself rather than silently defaulting to "mutating".
pub fn is_read_only(self) -> bool { ... }
```

- **只读为 true 的 12 类**：`Read / Search / Lsp / ListDir / List / MemorySearch / MemoryGet / WebSearch / WebFetch / EnterPlan / ExitPlan / AskUser`
- **其余 25 类默认 mutating**：`Edit / Delete / Write / Move / Execute / Plan / Task / BackgroundTaskAction / KillTaskAction / Skill / SearchTool / UseTool / Workflow / ImageGen / ... / Other`
- `ToolKind` 是**开放集**：`#[serde(other)]`，未知字符串降级为 `Other`，`Other` 默认 **mutating**（fail-closed）
  ```rust
  "Categorizes what a tool does at a high level. Open set — consumers must \
   tolerate unknown values (Rust deserializes them to `other` via \
   `#[serde(other)]`)."
  ```
- **注意**：`registry/types.rs` 里**没有**名为 `CapabilityMode` 的类型。所谓 capability-mode 是由 `ToolKind` 承载的一组**外部**(hub/daemon 侧)强制逻辑的统称，开源树里只能看到它的输入面：
  ```rust
  /// Fully-qualified tool id (`"GrokBuild:read_file"`) → declared [`ToolKind`], for every
  /// registered tool. Lets consumers that receive kind-less tool configs (e.g. hub `session.bind`
  /// wire entries) backfill the kind from the binary's own registry before capability filtering.
  pub fn known_tool_kinds(&self) -> HashMap<String, ToolKind>
  ```

**分类学被用在三个地方**：① capability 过滤输入；② `tool_taxonomy::writing_tool_kind()` 给 UI 的 spinner 文案；③ 每次工具调用都 stamp 一份跨 harness 一致的元数据信封：

```rust
/// Tool-identity envelope under [`TOOL_META_KEY`]. ...
pub struct CanonicalToolMeta {
    pub version: u32, pub name: String, pub kind: ToolKind,
    pub namespace: ToolNamespace, pub label: Cow<'static, str>,
    pub read_only: bool, pub input: Option<serde_json::Value>,
}
```
（由 `xai-grok-tools/src/normalization.rs` 的 `merge_tool_meta` 在每次分发时写入 `_meta`，跨 codex/opencode/grok 三套工具名给出统一语义标签）

---

## 4. MCP 准入：`server__tool` 的生成、解析与长度上限

`crates/codegen/xai-grok-mcp/src/tool_name.rs`（整文件已核对）：

```rust
//! Provider function-name limits (64 chars) apply to `search_tool` / `use_tool`, not to
//! catalog keys. A qualified `server__tool` is a routing id for those meta-tools.
pub const PROVIDER_TOOL_NAME_MAX_CHARS: usize = 64;
/// Safety cap on catalog keys (`server__tool`). Not a provider function-name limit.
pub const MCP_QUALIFIED_NAME_MAX_CHARS: usize = 256;
```

- 生成：`format!("{server}{MCP_TOOL_NAME_DELIMITER}{tool}")`，server 段须以字母/下划线开头，两段字符集 `[A-Za-z0-9_-]+`；
- 解析是 **overlap-aware**（唯一确认的对抗性细节）：
  ```rust
  // Byte windows preserve both overlapping `__` boundaries in `___`.
  let boundary = boundaries.next()?;
  if boundaries.next().is_some() { return None; }   // 出现第二个分隔符 → 歧义 → 拒绝
  ```
  **注意：这是拒绝，不是回退。** 歧义名直接被排除出目录。
- 准入失败有类型化错误枚举：`InvalidServerName / InvalidToolName / QualifiedNameTooLong / InvalidOrAmbiguousQualifiedName`。
- 副作用：`!client_name.contains("__")` 成了区分内置/MCP 的**字符串启发式**，代码自己都写了 TODO：
  ```rust
  /// Get only built-in tool definitions (exclude MCP tools). TODO: use ToolNamespace metadata
  /// instead of the "__" string heuristic. This breaks if a built-in tool ever has "__" in its
  /// name or an MCP server omits the delimiter.
  ```

---

## 5. 分发：三级解析，**全等匹配，无任何回退**

这是本次调查最需要纠正既有认知的一节。

### 5.1 第 1 级：`client_name` 全等线性扫描

```rust
let tools = self.tools.read();
let entry = tools.iter().find(|t| t.client_name == tool_name)
    .ok_or_else(|| Self::tool_not_found_error(tool_name))?;
```

- `FinalizedToolset` 里工具是 **`Vec`**，不是 HashMap → **O(n) 线性扫描**（注释承认是 microsecond lookups，靠 RwLock 读并发兜住）；
- **没有模糊匹配、没有别名回退、没有前缀回退、没有 `register_alias`**（全文检索 `alias` 标识符只见于参数名映射）。

### 5.2 第 2 级：`registry_id → ToolId → LocalRegistry`

```rust
let tool_id = xai_tool_protocol::ToolId::new(&registry_id)
    .unwrap_or_else(|_| xai_tool_protocol::ToolId::new("unknown").expect("valid"));
let lr_handle = self.local_registry.find(&tool_id).ok_or_else(|| {
    xai_tool_runtime::ToolError::not_found(tool_id,
        format!("Tool not found in LocalRegistry: {registry_id}"))
})?;
```

### 5.3 未知名文案：⚠️ 不含合法名字列表

```rust
fn tool_not_found_error(tool_name: &str) -> xai_tool_runtime::ToolError {
    ...
    xai_tool_runtime::ToolError::not_found(tid, format!("Tool not found: {tool_name}"))
}
```

**这是关键事实修正**：Grok Build 的 `Tool not found` **不回显合法名字列表**。它的模型自愈循环靠的是另外三样东西——`use_tool` 的定向纠正文案（第 6 节）、`search_tool` 的发现引导、以及角色 prompt 里动态渲染的工具白名单段落（第 7 节）。

### 5.4 `call` / `call_streaming` / `call_raw`：后处理只跑一次的不变量

| | 名字解析方式 | `finalize_output`（reminders + persistence + prompt 渲染） | 注入 `InnerDispatch` |
|---|---|---|---|
| `call` / `call_streaming` | `prepare_dispatch` | ✅ 跑 | ✅ 注入 |
| `call_raw` | 自己复制一级查找 | ❌ 不跑 | ❌ **故意不注入** |

```rust
/// Execute a tool, returning only its raw output. Unlike [`call()`], this skips reminders and persistence. Used by `InnerDispatchForToolset` so
/// that `use_tool`'s dispatch to a target tool does not double-run post-processing: the outer `call("use_tool")` does one round of
/// post-processing over the target's output. Inner dispatch is intentionally **not** populated in the forwarded context.
```

**不注入 `InnerDispatch` 是刻意的安全边界**：元工具触发的二次分发内部，再也无法发起第三次分发（杜绝递归链），同时也保证后处理有且只有一次。

---

## 6. 延迟发现：`search_tool` → `use_tool` 两级元工具

这是 Grok Build 处理"工具太多"的**真正答案**——不是缩小工具面，而是把工具面**折叠成一个检索入口**。

### 6.1 `search_tool`（`implementations/search_tool/mod.rs`）

```rust
//! `search_tool` — discover MCP tools via BM25 keyword search.
let limit = input.limit.unwrap_or(5) as usize;
let snapshot = tool_index.search_snapshot(&input.query, limit);
```

- 算法：**BM25**（非 embedding），结果按分数降序，再按 server 分组（组内保序，组间按组内最高分排）；
- 默认 **limit = 5**；
- 返回 `{status: "ready"|"partial", total_hidden_tools, results:[{server, tools:[{tool_name, description, score, input_schema}]}], note}`；`description` 截断到 **2048** 字符；
- **`status: partial`**（部分 MCP server 还在连接）会显式告知模型结果可能不完整——这是一个诚实的**不确定性外露**设计；
- ❗**检索结果不进入工具面**：没有把搜到的工具注册/注入后续可用工具列表的代码。工具名和 schema 只是作为**这次调用的输出文本**进入对话历史。

### 6.2 `use_tool`（`implementations/use_tool/mod.rs`）

```rust
/// Input for the `use_tool` meta-dispatch tool.
pub struct UseToolInput {
    /// The qualified name of the integration tool to call (e.g., "linear__save_issue").
    /// Must be a tool previously discovered via `search_tool`.
    pub tool_name: String, ...
}
```

路由分两条，**本地优先**：

```rust
// A gateway-catalog name can collide with a local `server__tool` MCP tool. Local wins on a name clash: probe local dispatch first and only
// fall through to the gateway when the local side reports the tool as not found, or rejects the catalog-derived name as an invalid local
// ToolId. A real error from a local tool that actually dispatched propagates instead of silently retrying against the gateway.
if tool_name.contains("__") && let Some(dispatch) = dispatch.clone() { ... }
```
本地先探：命中即返回；`NotFound` 或 `InvalidArguments("invalid tool name")` → 回落 managed gateway 按 `call_id` 远端调用；**其它真实 error 原样向上传播，不静默重试**。

四类硬拦截（**全部在分发前 return，不碰目标工具**）：

1. **原生工具被错路经过 `use_tool`**——最有借鉴价值的一条：
   ```rust
   // Native tool wrongly routed through use_tool. Tell the model to call it directly.
   // Strategy chosen via offline eval over real production failures: 2% doom-loop, 86%
   // native recovery, 0 double-schedules.
   "`{tool}` is a native tool, not an MCP integration tool. Call `{tool}` directly as its own tool call instead of routing it through `use_tool`."
   ```
   👉 也就是：**Grok Build 用离线 eval 数据来选纠错文案**，不是拍脑袋。
2. 非限定名且不在网关目录 → `"'{}' is not a valid MCP tool name. Tool names must be qualified as `server__tool` ... Use `{search_tool}` to discover available tools."`
3. 网关目录命中但无客户端 → `managed_gateway_unavailable`
4. `InnerDispatch` 缺失 → `"... inner_dispatch not set -- this is a bug."`

另有兜底开关 `UseToolParams.native_tool_correction`（默认 true），关掉后走通用文案。

---

## 7. 权限：单一策略层，执行点求值

位置与本仓既有文档**不同**：不在 `xai-grok-shell`，而在
**`crates/codegen/xai-grok-workspace/src/permission/`**（`resolution.rs` / `grants.rs` / `state.rs` / `hub_gate.rs` / `hub_permission.rs` / `gate_preflight.rs` / `auto_mode/` / `manager/`）。✅ 目录已用 Contents API 核实。

### 7.1 规则模型：默认 **Deny**（显式引用 CWE-1188）

`crates/codegen/xai-grok-config-types/src/permission.rs`（✅ 逐字核对）：

```rust
/// Action to take when a rule matches.
///
/// The default is Deny (CWE-1188): omitting the `action` field in a TOML permission rule must not silently create a catch-all allow rule.
pub enum RuleAction { Allow, #[default] Deny, Ask }
```
匹配维度 `ToolFilter{Any,Bash,Edit,Read,Grep,Mcp,WebFetch,AgentMessage}`（`#![non_exhaustive]`） × `PatternMode{Glob,Domain}`，另有 `pattern: Option<String>`。

👉 **这条极值得借鉴**：本仓 Phase 1 的 `corelib/permission` 应检查"省略 action 字段"时的默认是否为拒绝。

### 7.2 优先级 deny > ask > allow（✅ 在 `grants.rs` 的 bash 求值里得到独立印证）

```rust
// 1. Disallow takes priority: reject the whole script.
// 2. Dangerous commands must be prompted even if a whitelist prefix would otherwise match.
// 3. Auto-allow conditions. Built-in safe lists count only when `honor_safe_lists` is set; an explicit user grant always counts.
// 4. Otherwise: prompt for this segment.
```

🟡 另有 `resolution.rs` 的模块头与 `resolve_permissions_with_provenance` 注释：
> Deny beats ask beats allow regardless of file; source order is display only. Read at session start. / Managed `defaultMode` outranks user/project/local. / `project_trusted` gates project-tier rules so an untrusted clone cannot disable prompts.

（子代理报告中提到的 `permission/policy.rs::decision_rank` 我在 `grants.rs` 与目录清单中**未找到该文件**；优先级结论已由上面两段独立证实，但 `decision_rank` 这一具体实现请视为未核实。）

### 7.3 不信任目录不贡献规则（🟡）

```rust
// Project-scoped configs walking from git root down to cwd, gated on trust.
// An untrusted clone must not contribute allow/deny/ask rules via `.grok/config.toml`
if project_trusted { ... }
```

### 7.4 审批门：fail-closed 由"有没有通道"决定（🟡）

```rust
//! The approval gate on hub tool calls: ... no transport and no answer both deny; the folder's
//! persisted grants are the TUI's `permission.toml`, shared on purpose ...
/// On by construction where the device is the user's own: there is no off switch on the device, only
/// the tenant's `tool_approval_policy` delivered with the bind.
pub fn approval_gate_for(host_kind: WorkspaceHostKind) -> ToolApprovalGate
```
- 无传输 → **拒绝**（`SECURITY: fail closed; a guarded tool never runs without a channel to the session owner`）；
- 无响应/无法识别的答复 → `PromptOutcome::RejectOnce`；只有 `Allow*` 变体放行；
- 超时另有指标 `grok_workspace_permission_timeout_total`（最大观测桶 600s）。

👉 **"没有通道即拒绝"比"超时即拒绝"更本质**——它把 fail-closed 建立在**结构上**而不是**计时器**上。本仓 R5 目前是"超时即拒"，可对照评估是否应升级。

### 7.5 auto 模式：LLM 分的是"权限"，不是"工具"（🟡）

```rust
//! Auto permission mode: LLM transcript classifier with safe fast-paths.
//! Port of common agent auto-permission classifier semantics adapted to Grok's `AccessKind` permission gate.
/// 1. deterministic [`HeuristicPermissionClassifier`] pre-pass: a provably routine, side-effect-free action allows immediately (no model call);
/// 2. the injected side-query (LLM) when present;
/// 3. an unavailable verdict when the side-query fails, or the heuristic's (non-Allow) verdict when the model responds with unparseable output.
```
分类器输出是 `AccessKind`（Read/Bash/Edit/MCP/…），**不是工具名**；且模型不可用 → `unavailable`（不是 allow）。

---

## 8. 子代理路由

### 8.1 没有字面断言 "child ⊆ parent" 的不变式（🟡）

最接近的是 `xai-grok-subagent-resolution/src/definition.rs::apply_child_tool_policy`——它是**过滤**而非**校验**：

```rust
if let Some(mode) = capability_mode { mode.filter_tool_config(&mut definition.tool_config); }
if !allow_nested_subagents {
    definition.tool_config.tools.retain(|tool| tool.kind != Some(ToolKind::Task));
    prune_orphaned_background_task_tools(&mut definition.tool_config);
}
definition.tool_config.tools.retain(|tool| {
    !xai_grok_tools::implementations::grok_build::is_workflow_tool(tool.kind, &tool.id)
});
```
→ **Workflow 与嵌套 Task 无条件剥离**；另有三处交集/ cloning 机制使 child 天然受父约束（`overrides.rs::intersect_capability_modes(requested, ceiling)` 取更受限者；`xai-grok-agent/src/builder.rs::preview_child_tool_names` 克隆父 builder）。

### 8.2 准入：toggle（黑名单式）+ 父 allow-list（白名单式）双重门（🟡）

```rust
/// Apply the production toggle and parent allow-list gates.
pub fn gate_agent_definition(...) -> Result<(), ResolutionError> {
    if !context.toggles.get(subagent_type).copied().unwrap_or(true) { return Err(ResolutionError::Disabled { .. }); }
    if let Some(allowed) = context.allowed_types
        && !allowed.iter().any(|c| c.eq_ignore_ascii_case(subagent_type)) { return Err(ResolutionError::NotAllowed { .. }); }
```
`toggles` 默认 true（**fail-open 的黑名单**），`allowed_types` 是可选的父级白名单。发现顺序：项目 → 内置 → 用户 → 插件 → 会话 CLI 兜底；spawn 前 eager `validate_type`，错误分 `Unknown/Disabled/NotAllowed/CoordinatorGone/ValidationUnavailable`。

### 8.3 子代理描述的确从活体注册表动态生成（🟡）

`xai-grok-agent/src/builder.rs::build()`：
```rust
let subagents = crate::discovery::all_subagents_with_plugins(&self.working_directory, &self.subagent_toggle, self.plugin_registry.as_deref());
let child_tools = self.preview_child_tools(&subagents).await;
task_tc.description_override = Some(build_task_description(&subagents, &self.task_model_slugs, &child_tools));
```
`preview_child_tool_names` 注释：`/// Builds child the way a spawn would, minus discovery and persistence, and reads the client names off its bridge.`

### 8.4 角色→工具名的动态渲染 + 防注入（✅ 逐字核对）

`crates/codegen/xai-grok-shell/src/session/goal_role_tools.rs`：
```rust
/// `true` when `name` is safe to splice verbatim into an LLM prompt.
/// This rejects newlines, control chars, backticks, `{`/`}`, spaces, and markdown.
/// A `name_override` is registry-validated for uniqueness only, so its content is untrusted text that ends up in the prompt.
fn is_safe_tool_name(name: &str) -> bool
```
`{WRITE_TOOL}` 有 Write→Edit 的安全回退（默认 host 的真实写工具是 `search_replace`）；verifier 角色会渲染一段活体工具白名单：
```
Tools available to you for this review: `read_file`, `grep`.
```
单次左到右替换，已替换值不被二次扫描（防占位符嵌套重展开）。

---

## 9. 对 `tool-routing-improvement-plan-zh.md` §2 的事实校正

| # | 既有表述 | 源码事实 | 处置建议 |
|---|---|---|---|
| 1 | 「`register_alias` 支持前缀回退」 | **`register_alias` 不存在**；分发只有 `client_name` 全等匹配；`unregister_tools_by_prefix` 是按前缀**注销**不是回退 | 删除该表述。R1 的对抗性论证应改为基于 §4 的**歧义即拒绝**（第二个 `__` → `None`）而非"回退解析器" |
| 2 | 「未知名返回 Tool not found，**错误信息包含合法名字列表**，形成模型自愈循环」 | 文案就是 `Tool not found: {name}`，**不含列表**；自愈靠 `use_tool` 定向纠正 + `search_tool` 引导 + 角色白名单段落 | 修正。这与本文 v2 已删掉的"错误回显合法名列表"条目结论一致，反而印证了当时的删除是对的 |
| 3 | 「`CapabilityMode` 按 `ToolKind` 声明式过滤」 | 无 `CapabilityMode` 类型；它是对 `ToolKind` 的**外部强制**统称，开源树只见输入面（`known_tool_kinds` / `tool_kind_map`） | 改为"capability 分类输入面"，避免被读成"存在一个可照搬的枚举" |
| 4 | 未提 `search_tool`/`use_tool` 的关键性质 | **检索结果不注入工具面**；`limit` 默认 5；BM25；`call_raw` 故意不注入 `InnerDispatch` | Phase 4「元工具合并」议题应重读 §6——本仓 `semantic_tools_search` 的语义与之差异较大 |
| 5 | 权限层位置与规则源 | 实为 `xai-grok-workspace/src/permission/`，规则源确认含 requirements.toml → managed → 项目 → `.claude/settings.json`；**默认 Deny（CWE-1188）** 既有文档没写 | Phase 1 应对标本条：检查规则省略 action 时的默认值 |
| 6 | 「审批超时语义」 | Grok Build 更根本的是"**无通道即拒绝**"，超时只是 backstop | R5 可考虑从"超时即拒"升级为"无通道即拒 + 超时 backstop" |
| 7 | 「child ⊆ parent 统一约束 + kind 回填」 | 没有字面不变式；是 capability mode 交集 + 无条件剥离 Task/Workflow + toggle/allowlist 三件套 | Phase 3 第 3 条应改为描述这套组合 rather than 断言一个不存在的不变式 |

---

## 10. 可借鉴清单（按性价比排序）

1. **`RuleAction` 默认 Deny（CWE-1188）** —— 一行代码，直接消除"漏写 action 变成 catch-all allow"整类事故。Phase 1 立刻可用。
2. **无通道即拒绝**（审批 fail-closed 建立在结构上）——比计时器可靠，对应本仓 R5。
3. **`InnerDispatch` 不注入内层** —— 用一个结构事实保证"后处理只跑一次"和"无递归二次分发"，比靠调用约定可靠。Phase 2 的 petition 改造可直接抄这个形状。
4. **`use_tool` 的原生工具定向纠正 + 离线 eval 选型数据**（2% doom-loop / 86% recovery）——本仓也有 2026-09-18 ssh 事故链与 28 万 token 事故的教训，纠错文案应当同样以数据选型。
5. **工具本机 `_meta` 信封**（`CanonicalToolMeta`：kind/namespace/label/read_only）——跨工具集给下游（UI、权限、审计）一个稳定语义标签，本仓目前没有等价物。
6. ** ToolKind 穷尽匹配 + `Other` 默认 mutating** —— 新增种类必须自分类，否则编译不过。本仓 AccessKind 尚未有此强制。
7. **`status: partial` 的诚实外露** —— 检索索引未就绪时明确告诉模型结果可能不完整，而不是假装完整。
8. **角色→工具白名单的动态渲染 + `is_safe_tool_name`** —— 直接对应本仓 Phase 3 第 4 条，且已内置 prompt 注入防护（本仓上位设计 §4.4 目前是禁止注入动态元数据，需要论证等价性）。

**不建议借鉴**：① 全量工具进上下文（我们有 planner，这是我们的优势不是短板）；② MCP `server__tool` 明文命名（R1 已否决，理由成立）；③ Vec 线性扫描（工具规模上百后是隐患，我们应保持索引查找）。

---

## 11. 未核实 / 未覆盖

- 🟡 标记项均来自子代理一次核对，未二次逐字复核（尤其 7.2 的 `decision_rank` 文件不存在，需重新定位）。
- **capability-mode 的强制执行点不在开源树**（依赖闭源 `xai_computer_hub_sdk` / hub core），无法验证 read-only 会话究竟如何剔除工具。
- 本仓既有文档提到的"并发数上限 32" 与 "`.claude/settings.json` 回退" 两处量化/细节未在本次源码核对中验证。
- 未调查：hooks 与工具路由的交互、plugin registry snapshot 对工具面的影响、sandbox/profile 对可见性的影响。

---

## 12. 追加调查：工具是有限的吗？全量注册会不会撑爆上下文？

> 触发问题：「grok 的工具是有限的吗？如果全量注册，工作多时，岂不导致上下文过长？」

结论先行：**第一方工具是有限的、且被刻意压得很小（注册表 52 个工具 + 3 个 reminder + 外部 pack）；MCP 工具在数量上无界，但它们根本不进上下文。所以"全量注册"这个说法是误导——它全量注册的是「可执行句柄」，而进上下文的是「经 `config.tools` 白名单切片后的子集」。上下文成本由三层机制吸收，且**不存在**任何"按数量/预算裁剪工具面"的逻辑。**

### 12.1 注册数 ≠ 上下文成本：`config.tools` 才是决定性的那一步

这是本次追加调查最重要的发现，也是对既有文档 §2.2 第 2 条（`should_list` 谓词过滤）的更正。

`ToolRegistryBuilder::new()` 是**无条件**注册的（无任何 `if` 包裹单个注册），共 38 个 `register` + 14 个 `register_with_params` = 52 个工具，另加 3 个 `register_reminder` 与外部 tool pack：

```rust
    pub fn new() -> Self {
        let mut b = Self {
            tools: HashMap::new(),
            reminders: Vec::new(),
            shared_local_registry: None,
            system_reminders_enabled: true,
        };
        b.register_with_params::<grok_build::BashTool, grok_build::bash::BashParams>();
        b.register_with_params::<grok_build::ReadFileTool, grok_build::read_file::ReadFileParams>();
        b.register_with_params::<
                grok_build::SearchReplaceTool,
                grok_build::search_replace::SearchReplaceParams,
            >();
        b.register_with_params::<grok_build::ListDirTool, grok_build::list_dir::ListDirParams>();
        b.register_with_params::<grok_build::GrepTool, grok_build::grep::GrepParams>();
        b.register::<grok_build::KillTaskTool>();
        // …（略：共 14 个 register_with_params + 38 个 register）…
        b.register_reminder(crate::reminders::LspDiagnosticsReminder);
        b.register_reminder(crate::reminders::TaskCompletionReminder);
        b.register_reminder(SkillDiscoveryReminder);
        for pack in tool_packs().lock().iter() {
            pack(&mut b);
        }
        b
    }
```
（`xai-grok-tools/src/registry/types.rs`）✅

但真正决定**模型看见什么**的是 `finalize_with_trunc_config`——它**只遍历 `config.tools`，不遍历 `self.tools`**：

```rust
        for tool_config in &config.tools {
            let entry = self.tools.remove(&tool_config.id).unwrap();
            (entry.register_in_local)(&local_registry);
            ...
            tools.push(FinalizedTool { ... });
        }
```
（`xai-grok-tools/src/registry/types.rs`）✅

注释补充（同一函数内）：

> `/// The tool's capability category. Populated automatically by `for_tool::<T>()` / `From<&T: Tool>` and used by capability-mode enforcement to filter tools without a hardcoded ID mapping.`

`ToolConfig::from_id` 的注释进一步说明：

```rust
    /// Build a `ToolConfig` from a string id (no associated Rust type). Use this for MCP/custom
    /// tools or anywhere the id is only known at runtime. `kind` is left as `None`; capability-mode
    /// filtering then preserves the tool unconditionally.
```

**关键推论**：注册表是「工具目录（catalog）」，`config.tools` 是「本次会话的工具面（surface）」。`self.tools.remove(&id).unwrap()` 这一行还意味着——**未列入 `config.tools` 的工具既不入 `local_registry` 也不入 `tool_definitions()`**，从注册表被物理摘除。这才是 Grok Build 的三层可见性过滤里**真正的第一层**，而既有文档 §2.2 记的 `should_list(&ListToolsContext)` 谓词在源码中**未找到**（0 命中）。

### 12.2 第一方集合是有限的，且互斥命名空间让它保持小

注册表里同时存在 6 个命名空间，但**同一时刻只有一组生效**：

| 命名空间 | 注册数 | 与谁互斥 |
|---|---|---|
| `GrokBuild`（标准） | 31 | 与 `GrokBuildConcise` / `GrokBuildHashline` 的文件类工具互斥 |
| `codex` | 4 | 独立（apply_patch / list_dir / grep_files / read_file） |
| `opencode` | 8 | 独立（bash / read / edit / write / grep / glob / todo_write / skill） |
| `GrokBuildConcise` | 3 | `ReadFileConciseTool` / `SearchReplaceConciseTool` / `BashConciseTool`；**注意这是「精简输出」变体**（`grok_build_concise/mod.rs`：`//! These tools share implementation with `grok_build` via `pub(crate)` helpers / but produce concise output (compact line numbers, shorter messages, / concise bash formatting).`），不是"精简工具集" |
| `GrokBuildHashline` | 3 | `HashlineReadTool` / `HashlineEditTool` / `HashlineGrepTool` |
| 通用 + MCP | 4 | `memory_search` / `memory_get` / `search_tool` / `use_tool` |

`standard` 与 `hashline` 的互斥是**硬校验**，混用直接让整个 finalize 失败：

```rust
            if has_standard && has_hashline {
                errors.push(
                    RequirementError::new(
                        "(file-toolset)",
                        "mixed standard and hashline file tools are not allowed. \
                         Use either the standard bundle (read_file, search_replace, grep) \
                         or the hashline bundle (hashline_read, hashline_edit, hashline_grep), \
                         not both.",
                    )
                    .with_field_path("tools")
                    .with_category("file_toolset_conflict"),
                );
            }
```
（`xai-grok-tools/src/registry/types.rs`）✅

另外 concise 命名空间还有一个**条件资源注入**（不是削减工具数，而是关掉提醒以省 token）：

```rust
        let concise_ns = crate::types::tool::ToolNamespace::GrokBuildConcise.to_string();
        let has_concise_tools = config.tools.iter().any(|tc| {
            self.tools
                .get(&tc.id)
                .is_some_and(|e| e.namespace == concise_ns)
        });
        if has_concise_tools {
            resources.insert(crate::types::resources::SystemRemindersEnabled(false));
        }
```
（`xai-grok-tools/src/registry/types.rs`）✅

**结论**：实际进上下文的是「标准 31 + 通用 4 = 35」，或「concise 3 替换掉 3 个文件工具」，或「hashline 3 替换掉 3 个」。**第一方工具面是一个被人工 code review 钉住的固定数字，不随工作量增长。**

### 12.3 ⚠️ 更正：不存在 "reduced toolset"，也不存在 `use_concise`

本次调查专门追查了"configured 里有一个 `use_concise` 字段，注释写着 reduced toolset"这条线，**追查结果是该说法不成立**：

| 追查目标 | 结果 |
|---|---|
| 字符串 `reduced toolset` | 在 `xai-grok-config-types/src/lib.rs`（82732 字节全文）、`xai-grok-shell/src/agent/mvp_agent/acp_agent.rs`、`xai-grok-shell/src/session/goal_role_tools.rs`、本仓全部 `docs/` **均未找到**（0 命中） |
| 字段 `use_concise` | 未找到（`xai-grok-config-types/src/lib.rs`、`xai-grok-config-types/src/mcp.rs` 均 0 命中） |
| 该字符串的真实出处 | **仅存在于本会话早期的一次子代理报告里，源码无对应物** —— 属未复核的生成内容，作废 |

真正存在的是这些**逐字**定义（不是 `use_concise`）：

```rust
    /// File toolset: `"standard"` or `"hashline"`.
    /// This is the server-side default; local `[toolset] file_toolset` in config.toml takes precedence when set.
    #[serde(default)]
    pub file_toolset: Option<String>,
```
（`xai-grok-config-types/src/lib.rs`，`RemoteSettings` 结构体）✅

以及 `RemoteSettings` 中那些真正做**工具门控**的字段（注意：它们通过 `Feature` 枚举统一解析，而不是通过 `use_concise`）：

```rust
pub enum Feature {
    ...
    /// Language-server-backed navigation tools.
    LspTools,
    /// The `web_fetch` tool.
    WebFetch,
    /// The `ask_user_question` tool.
    AskUserQuestion,
    /// The `write_file` tool.
    WriteFile,
    ...
}

pub const FEATURES: &[FeatureSpec] = &[
    FeatureSpec {
        id: Feature::LspTools,
        key: "lsp_tools",
        path: "features.lsp_tools",
        env: "GROK_LSP_TOOLS",
        default_enabled: false,       // ← lsp 工具默认关
        remote: Some(|settings| settings.lsp_tools_enabled),
    },
    FeatureSpec {
        id: Feature::WebFetch,
        key: "web_fetch",
        path: "features.web_fetch",
        env: "GROK_WEB_FETCH",
        default_enabled: false,       // ← web_fetch 工具默认关
        remote: Some(|settings| settings.web_fetch_enabled),
    },
    ...
];
```
（`xai-grok-config-types/src/registry.rs`）✅

**所以 Grok Build 的工具面确实会被裁剪，但裁剪的粒度是「逐个 feature 开关」（默认关的有 lsp_tools / web_fetch / feedback_trace_card / subagent_worktree_snapshot / active_agent_messages / dock / terminal_theme），叠加 `config.tools` 白名单切片 + standard/hashline 二选一——而不是一个笼统的"concise 精简档"。** 顺带纠正一处：`Feature::` 在 `xai-grok-tools/src/registry/types.rs` 中 0 命中，工具门控的 `Feature` 枚举定义在 `xai-grok-config-types/src/registry.rs`。

### 12.4 MCP 是无界的，但它被彻底隔离在上下文之外

MCP 工具数量上界 = 所有已连 server 的工具总和，可以到几百上千。它们的隔离靠三件事：

1. **检索结果不注入工具面。** `search_tool` 的产物是**给模型看的一条消息**，不是 `tool_definitions()` 的新增项。所以搜 20 次也不增加工具面。
2. **索引每次检索时重建。** `tool_index.rs` 顶部注释：

   ```rust
   //! Builds a BM25 index over registered MCP tools and searches it.
   //! The index is rebuilt on each search call (sub-millisecond for tens to low hundreds of tools).
   ```
   —— 注释自己给出量级："tens to low hundreds of tools"。
3. **描述被截断到 2048 字符**（`MAX_MCP_DESCRIPTION_LENGTH`），且精确名命中走 fast path 完全跳过 BM25：

   ```rust
   // Fast path: exact match on qualified name or bare tool name.
   // When the model already knows the tool name (e.g. "grafana-ai__SearchDashboards" or "SearchDashboards"), skip BM25 entirely.
   ```

搜索快照里还带一个诚实的外露字段：

```rust
    pub total_hidden_tools: usize,
    pub is_ready: bool,
```
并且快照的 `mcp_initialized` 直接映射成 `is_ready`，让模型知道"索引好了没有"。

### 12.5 上下文成本的三层吸收，以及唯一被漏掉的那层

**第一层：工具定义本身是固定项，且永远不进压缩。**

`xai-chat-state/src/actor/state.rs` 里确实有一套工具定义的 token 估算，但**它们只用于记账，从不用于裁剪**：

```rust
fn estimate_tool_tokens(
    name: &str,
    description: Option<&str>,
    parameters: &serde_json::Value,
) -> u64 {
    let desc_len = description.map_or(0, str::len);
    let params_len = parameters.to_string().len();
    ((name.len() + desc_len + params_len) as u64) / xai_token_estimation::BYTES_PER_TOKEN
}

/// Bytes/4 estimate of one tool definition (name + description + the
/// JSON-serialized parameters).
pub fn estimate_tool_definition_tokens(td: &xai_grok_sampling_types::ToolDefinition) -> u64 { ... }

/// Sum [`estimate_tool_definition_tokens`] across a slice.
pub fn estimate_tool_definitions_tokens(tds: &[xai_grok_sampling_types::ToolDefinition]) -> u64 {
    tds.iter().map(estimate_tool_definition_tokens).sum()
}

/// Bytes/4 estimate of the exact tool specs serialized on a request.
pub fn estimate_tool_specs_tokens(tools: &[ToolSpec]) -> u64 { ... }
```

**第二层：工具 token 在压缩预算里是被减数，不是被压缩对象。**

```rust
/// ~70% of window, minus tool definitions
pub(crate) fn lossy_input_budget(context_window: u64, tool_tokens: u64) -> u64 { ... }

pub(crate) fn fitted_input_budget(context_window: u64, tool_tokens: u64) -> u64 { ... }
```
（`xai-grok-shell/src/session/compaction.rs`；同文件 `SUMMARY_BUDGET_RESERVE_TOKENS = 32_768`）✅

也就是说：工具面占用被**先扣掉**，剩下的预算才是给对话历史做压缩的目标。压缩永远动不到工具定义——这就是"工具定义是会话外固定成本"的代码证据。

**第三层：工具输出字节上限。**

```rust
/// Default maximum output size (in bytes) for tool results sent to the model.
/// 40 KB ≈ 10 000 tokens
pub const DEFAULT_TOOL_OUTPUT_BYTES: usize = 40_000;

/// Default maximum output size (in characters) for bash/terminal tool results.
/// 20 000 chars ≈ 5 000 tokens. Matches the common `SHELL_CHAR_HARD_LIMIT`.
pub const DEFAULT_TOOL_OUTPUT_CHARS: usize = 20_000;
```
（`xai-grok-tools/src/lib.rs`）✅

**唯一没有被主动控制的那一层，恰恰是提问者担心的那一层——"工作多时"。** 长任务带来的不是工具定义膨胀（那从第 1 轮就固定了），而是**对话历史膨胀**；Grok Build 对此的答案就是压缩 + 40KB 输出上限，没有任何按工具数量动态裁剪的逻辑。三项搜索结果全部为 0：

| 检索目标 | `xai-grok-tools/src/registry/types.rs` 结果 |
|---|---|
| `should_list` | **0** |
| `tool_budget` | **0** |
| `max_tools` | **0** |
| `truncate` / `subset`（针对工具列表） | **0**（文件里 `TruncationConfig` 是**输出文本**截断，与工具列表无关） |

`FinalizedToolset` 上与"删除工具"有关的两个方法都是**按名字/前缀注销**，不是按数量裁剪：

```rust
pub fn unregister_tools_by_prefix(&self, prefix: &str) -> usize {
    let mut tools = self.tools.write();
    let before = tools.len();
    let to_remove: Vec<_> = tools
        .iter()
        .filter(|t| t.client_name.starts_with(prefix))
        .filter_map(|t| xai_tool_protocol::ToolId::new(&t.registry_id).ok())
        .collect();
    tools.retain(|t| !t.client_name.starts_with(prefix));
    for tid in &to_remove {
        self.local_registry.unregister(tid);
    }
    before - tools.len()
}
```
（同文件；用于 MCP server 断开时清理）

`FinalizedToolset` 自身对工具列表的唯一一次结构性切割是**按 `__` 猜是不是 MCP**，而且作者自己标了 TODO：

```rust
/// Get only built-in tool definitions (exclude MCP tools). TODO: use ToolNamespace metadata
/// instead of the "__" string heuristic. This breaks if a built-in tool ever has "__" in its
/// name or an MCP server omits the delimiter.
pub fn tool_definitions_builtins_only(&self) -> Vec<ToolDefinition> {
    self.tools
        .read()
        .iter()
        .filter(|t| !t.client_name.contains("__"))
        .map(|t| t.definition.clone())
        .collect()
}
```

### 12.6 修正对「单轮工具面调整」的认知

有一个机制容易和"动态裁剪"混淆：`ToolOverrides`。它是**每轮补丁**（不是会话持久），但**它不能增删工具，只能改搜索参数**：

```rust
/// The resolved per-tool overrides, and the shape echoed back for attestation.
pub struct ToolOverrides {
    pub x_search: Option<XSearchOptions>,
    pub web_search: Option<WebSearchOptions>,
}

/// The ingress-only per-turn patch: each tool is a tri-state [`ClearableField`] applied by
/// [`Self::apply`].
pub struct ToolOverridesUpdate {
    pub x_search: ClearableField<XSearchOptions>,
    pub web_search: ClearableField<WebSearchOptions>,
}

/// A tri-state per-turn patch field: absent leaves, `null` clears, a value sets.
pub type ClearableField<T> = Option<Option<T>>;
```
（`xai-grok-sampling-types/src/tool_overrides.rs`）

`XSearchOptions` 只有 `date_bound`；`WebSearchOptions` 只有 `allowed_domains` / `excluded_domains`。**没有任何 `enabled` / `disable` / `allow_tools` / `tool_choice` 字段**（0 命中）。所以它不是工具面的裁剪通道。

### 12.7 对本仓的直接含义

1. **"全量注册"在 Grok Build 里是安全的，因为它不是"全量进上下文"。** 这两件事被 `config.tools` 白名单切开了。本仓若要借鉴 §2.2 的第 2 层描述，应改成"**目录全量注册 + 会话级 `config.tools` 切片 + feature 开关逐项门控**"，而不是"`should_list` 每轮谓词过滤"。
2. **我们不需要"全量进上下文"，但需要 Grok Build 那个"目录 vs 面"的分离。** 本仓的语义规划器解决的正是"面怎么定"的问题；Grok Build 用一份手写白名单 + 一批默认关的 feature 开关解决同一问题。两者是同构的，但**它的方案不需要检索正确性，代价是工具面只能人工维护**。对一个接了任意第三方 MCP 的产品，我们的路线不可让渡（这与 §10 的"不建议借鉴 ①"结论一致，但理由要更新为"它的可维护性来自第一方封闭集合，我们不具备这个前提"）。
3. **可直接抄的两处小东西**：
   - `SearchSnapshot { total_hidden_tools, is_ready }` —— 检索结果里显式告诉模型"还有多少工具没被发现、索引好没好"。这比本仓目前的静默漏召回更诚实，成本极低。
   - `finalize` 期的**互斥包硬校验**（`file_toolset_conflict`：`standard` 与 `hashline` 同时出现即整体失败）。本仓工具面若存在"互斥的能力组"，用 finalize 期 fail-closed 比运行期判断可靠。
4. **一处要警惕的差异**：Grok Build 的 `estimate_tool_definitions_tokens` 在**生产路径上没有任何调用点**（仅 `#[cfg(test)]` 内被测试调用）——也就是说连"记账"这一层在开源树里都是**未接线**的。本仓若要做工具面预算，不能假设"上游已经算过账了"，得自己接。
