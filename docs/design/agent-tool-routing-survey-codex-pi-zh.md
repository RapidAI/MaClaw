# Codex 与 Pi 的工具路由策略源码调查

- 调查对象
  - **Codex**：`openai/codex`（Rust，`codex-rs/` 工作区）
  - **Pi**：Mario Zechner 的编码 agent，TypeScript。仓库已从 `badlogic/pi-mono` 迁至 **`earendil-works/pi`**（旧地址 301，Contents API 会重定向）；npm scope 为 `@earendil-works`
- 调查方式：直接抓取 GitHub raw 源码与 Contents API，**全部结论带文件路径 + 原样引用**
- 证据基线：两仓 `main` 分支，2026-09-20
- 姊妹篇：`docs/design/grok-build-tool-routing-survey-zh.md`（对 `xai-org/grok-build` 的同类调查，第 4 节有横向对比）
- 上位文档：`docs/design/semantic-tool-routing-design-zh.md`；改进计划：`docs/design/tool-routing-improvement-plan-zh.md`
- 可信度标记：✅=当场抓取原文逐字核对；🟡=间接证据或推断（已在文中注明）

> **消歧**："pi agent" 在公开语境里还指 Inflection 的对话产品 Pi（无工具调用），本调查按"编码 agent"语境取 `pi-mono` / `earendil-works/pi`。若你指的是别的系统请指出。

---

## 0. 一句话结论

三个系统对"工具多了怎么路由"给出了**三种互斥的答案**，而它们各自成立的前提完全不同：

| 系统 | 答案 | 成立前提 |
|---|---|---|
| **Pi** | **不解决**——把工具压到 8 个以内，根本没有"多"的问题；能力增长走 skills（按需注入 markdown）与 extensions（用户显式安装） | 单用户、本地、用户愿意自己装扩展 |
| **Codex** | **解决**——给每个工具一个 6 态"暴露面"，每轮按模型/provider 能力重算；MCP 工具默认走 `tool_search`（BM25）延迟加载 | 能改 protocol、能要求 provider 支持 namespace + defer_loading |
| **Grok Build**（姊妹篇） | **规避**——第一方固定 ~35 个全量进上下文，MCP 一律隔离到 `search_tool` + `use_tool` 二次分发 | 只接第一方工具 |

**关键判断**：Codex 是三者中**唯一把"暴露面"做成一等概念并让它可配置、可组合、可按能力降级**的。这正是我们语义路由缺失的工程化中间层。

---

## 1. Pi：Route avoidance（不做路由）

### 1.1 工具集的事实：8 个上限，三套预设

`packages/coding-agent/src/core/tools/index.ts` 把工具名钉成一个联合类型：

```ts
export type ToolName = "read" | "bash" | "powershell" | "edit" | "write" | "grep" | "find" | "ls";
export const allToolNames: Set<ToolName> = new Set([
	"read", "bash", "powershell", "edit", "write", "grep", "find", "ls",
]);
```
✅

（广为流传的"Pi 只有 4 个工具"说法指的是**默认编码预设**，不是全集：）

```ts
export function createCodingToolDefinitions(cwd: string, options?: ToolsOptions): ToolDef[] {
	return [
		createReadToolDefinition(cwd, options?.read),
		createBashToolDefinition(cwd, options?.bash),
		createEditToolDefinition(cwd, options?.edit),
		createWriteToolDefinition(cwd, options?.write),
	];
}

export function createReadOnlyToolDefinitions(cwd: string, options?: ToolsOptions): ToolDef[] {
	return [
		createReadToolDefinition(cwd, options?.read),
		createGrepToolDefinition(cwd, options?.grep),
		createFindToolDefinition(cwd, options?.find),
		createLsToolDefinition(cwd, options?.ls),
	];
}
```
✅

也就是 **coding（4）/ read-only（4）/ all（8）** 三套预设——这与 Grok Build 的 `file_toolset = "standard" | "hashline"` 是同一种"整包互斥切换"思路，只是切的是整个工具面而非文件类工具。

SDK harness 那一层（`packages/agent/src/harness/tools/`）只有 `bash / edit / edit-diff / image / read / write` ✅。

### 1.2 反面证据：Pi 确实没有工具检索

我特意检查了 `packages/agent/src/search/`——**它不是工具检索，是会话检索**：

```ts
export interface SessionSearchService {
	searchSessions(query: SearchQuery): Promise<SessionSearchHit[]>;
	searchEntries?(query: SearchQuery): Promise<EntrySearchHit[]>;
	sync(): Promise<void>;
	notify(sessionId: string): void;
	remove(sessionId: string): Promise<void>;
	close(): Promise<void>;
}
```
（`packages/agent/src/search/index.ts`，该文件仅此内容）✅

即 Pi 连"工具发现"这个动作都没有。工具就是一个扁平数组，按名字分发。

### 1.3 能力增长的两个出口：skills（按需注入）与 extensions（显式安装）

**Extensions** 通过 `pi.registerTool(...)` 添加工具，定义形状：

```ts
	/** Register a tool that the LLM can call. */
	registerTool<TParams extends TSchema = TSchema, TDetails = unknown, TState = any>(
		tool: ToolDefinition<TParams, TDetails, TState>,
	): void;
```

```ts
export interface ToolDefinition<TParams extends TSchema = TSchema, TDetails = unknown, TState = any> {
	/** Tool name (used in LLM tool calls) */
	name: string;
	/** Human-readable label for UI */
	label: string;
	/** Description for LLM */
	description: string;
	/** Optional one-line snippet for the Available tools section in the default system prompt. Custom tools are omitted from that section when this is not provided. */
	promptSnippet?: string;
	/** Optional guideline bullets appended to the default system prompt Guidelines section when this tool is active. */
	promptGuidelines?: string[];
	...
}
```
（`packages/coding-agent/src/core/extensions/types.ts`）✅

注意 `promptSnippet` 那句注释：**自定义工具若不给 `promptSnippet`，就不会出现在系统提示的 "Available tools" 段**。这是 Pi 唯一的"工具面压缩"机制，而且是**作者手工写一行摘要**，不是系统自动裁剪。

**Skills** 是从 `SKILL.md` 加载的 markdown 能力包，按需整块注入：

```ts
export function formatSkillInvocation(skill: Skill, additionalInstructions?: string): string {
	const skillBlock = `<skill name="${skill.name}" location="${skill.filePath}">\nReferences are relative to ${dirnameEnvPath(skill.filePath)}.\n\n${skill.content}\n</skill>`;
	return additionalInstructions ? `${skillBlock}\n\n${additionalInstructions}` : skillBlock;
}
```
（`packages/agent/src/harness/skills.ts`）✅

带硬约束与校验：

```ts
const MAX_NAME_LENGTH = 64;
const MAX_DESCRIPTION_LENGTH = 1024;
```
✅

以及 `disable-model-invocation` 前开关（是否允许模型主动调用）。

### 1.4 评价

- **Pi 的做法对我们的可借鉴度低但启发大**：它证明了"工具路由的复杂度可以靠产品决策消掉而不是靠算法解决"。代价是能力上限被钉死——装了 N 个 extension 后，Pi 会退化成"工具也不少但没有任何检索"，此时它比 Grok Build 更脆弱（Grok 至少有 `search_tool` 兜底）。
- **可抄的一点**：`promptSnippet` 的"自定义工具在系统提示里只占一行"约定。这是一个**极低成本的面向模型的工具面压缩**，且与我们的语义规划器正交——planner 决定给哪些工具，渲染时仍可用一行摘要代替完整 schema。

---

## 2. Codex：把"暴露面"做成一等概念

### 2.1 分层

| 层 | 位置 | 职责 | 证据 |
|---|---|---|---|
| `ToolRegistry` | `codex-rs/core/src/tools/registry.rs` | 注册表：`IndexMap<ToolName, RegisteredTool>` + 冲突记录 + `allowed_tools` 白名单 | ✅ |
| `ToolRouter` | `codex-rs/core/src/tools/router.rs` | 每轮终态：持有 `model_visible_specs` + 分发 | ✅ |
| `finalize_tool_router` | `codex-rs/core/src/tools/spec_plan.rs` | 规划器：把注册表按 exposure 投影成本轮可见面 | ✅ |
| `ToolSpec` | `codex-rs/tools/src/tool_spec.rs` | 线格式：`Function` / `Namespace` / **`ToolSearch`** / `WebSearch` / `Freeform` | ✅ |
| `ToolExposure` | `codex-rs/tools/src/tool_executor.rs` | 暴露面枚举（6 态） | ✅ |

```rust
#[derive(Default)]
pub struct ToolRegistry {
    tools: IndexMap<ToolName, RegisteredTool>,
    first_collision: Option<ToolName>,
    pub(crate) allowed_tools: Option<Arc<AllowedTools>>,
}

/// A tool runtime together with its effective exposure for the current step.
pub(crate) struct RegisteredTool {
    pub(crate) runtime: Arc<dyn CoreToolRuntime>,
    pub(crate) exposure: ToolExposure,
}
```
✅

```rust
/// One finalized tool plan: its advertised surfaces and matching executable runtimes.
pub struct ToolRouter {
    registry: ToolRegistry,
    model_visible_specs: Arc<[ToolSpec]>,
    tool_mode: ToolMode,
    code_mode_tool_names: BTreeMap<String, ToolName>,
    tool_namespaces_info: Option<TurnToolNamespacesInfo>,
    can_manage_children: bool,
}
```
✅

### 2.2 6 态暴露面 + 可组合位标志

```rust
/// Controls where a tool is exposed to the model.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ToolExposure {
    /// Include this tool in the initial model-visible tool list.
    ///
    /// When code mode is enabled, this tool is also available as a nested
    /// code-mode tool.
    Direct,

    /// Register this tool for later discovery, but omit it from the initial
    /// model-visible tool list. Deferred tools must provide search metadata via
    /// [`ToolExecutor::search_info`]. The default implementation derives
    /// metadata from function and namespace specs.
    Deferred,

    /// Make this tool discoverable through tool search without allowing nested
    /// Code Mode calls.
    DeferredModelOnly,

    /// Include this tool in the initial model-visible tool list only.
    ///
    /// In code-mode-only sessions, this keeps the tool callable as a normal
    /// model tool while excluding it from the nested code-mode tool surface.
    DirectModelOnly,

    /// Expose this tool only to nested Code Mode calls, without including it in
    /// the initial model-visible tool list or making it available to tool search.
    CodeModeOnly,

    /// Keep this tool registered for dispatch without exposing it to the model.
    Hidden,
}
```
（`codex-rs/tools/src/tool_executor.rs`）✅

注意 `Hidden` 的语义——**注册但不暴露**。这是一个 Grok Build 完全没有的状态：工具可执行（能被别的工具/代码模式调用），但模型看不见。

还有一层可组合的位标志，用于"这个工具支持哪些面"：

```rust
bitflags::bitflags! {
    /// Independent model-facing surfaces supported by a tool.
    pub struct ToolExposures: u8 {
        /// Keep the tool registered without making it model-visible.
        const NONE = 0;
        /// Include the tool in the initial model-visible tool list.
        const DIRECT = 0b001;
        /// Make the tool discoverable through tool search.
        const DEFERRED = 0b010;
        /// Make the tool callable from nested Code Mode scripts.
        const CODE_MODE = 0b100;
        /// Permit every supported model-facing surface.
        const ALL = Self::DIRECT.bits() | Self::DEFERRED.bits() | Self::CODE_MODE.bits();
    }
}
```
✅

`ToolExposures`（能力：我能上哪些面）→ `ToolExposure`（决策：这一轮我上哪个面），中间是一次**穷尽匹配**：

```rust
        tool.exposure = match (
            exposures.contains(ToolExposures::DIRECT),
            exposures.contains(ToolExposures::DEFERRED),
            exposures.contains(ToolExposures::CODE_MODE),
        ) {
            (false, false, false) => ToolExposure::Hidden,
            (false, false, true) => ToolExposure::CodeModeOnly,
            (true, false, false) => ToolExposure::DirectModelOnly,
            (true, false, true) => ToolExposure::Direct,
            (false, true, false) => ToolExposure::DeferredModelOnly,
            (false, true, true) => ToolExposure::Deferred,
            (true, true, _) => unreachable!("direct and deferred exposure are mutually exclusive"),
        };
```
（`spec_plan.rs`）✅

`unreachable!` 那一行是**编译期+运行期双重断言**：direct 与 deferred 互斥，是设计不变量而不是约定。

### 2.3 每轮重算，且按 provider 能力门控

`finalize_tool_router` 签名带 `turn_context` 与 `model_info`——**暴露面每轮重算**：

```rust
pub(crate) fn finalize_tool_router(
    turn_context: &TurnContext,
    model_info: &ModelInfo,
    mut registry: ToolRegistry,
    mut hosted_specs: Vec<ToolSpec>,
    tool_search_handler_cache: &ToolSearchHandlerCache,
) -> CodexResult<ToolRouter> {
```

`tool_search` 是否可用由**模型与 provider 双方**决定：

```rust
pub(crate) fn search_tool_enabled(turn_context: &TurnContext, model_info: &ModelInfo) -> bool {
    model_info.supports_search_tool && namespace_tools_enabled(turn_context)
}

fn namespace_tools_enabled(turn_context: &TurnContext) -> bool {
    turn_context.provider.capabilities().namespace_tools
}
```
✅

**这是能力降级的关键**：provider 不支持 namespace tools ⇒ 搜不了 ⇒ 工具只能 Direct 或 Hidden（见 2.2 的匹配表：`DEFERRED` 位被 `difference` 掉）。换模型、换 provider，同一个工具集会自动换一套暴露策略，不需要改代码。

降级逻辑逐字：

```rust
        exposures = if search_tool_enabled(turn_context, model_info)
            && exposures.contains(ToolExposures::DEFERRED)
            && (effective_tool_mode(turn_context, model_info) != ToolMode::CodeModeOnly
                || exposures.contains(ToolExposures::CODE_MODE))
        {
            exposures.difference(ToolExposures::DIRECT)
        } else {
            exposures.difference(ToolExposures::DEFERRED)
        };
```
✅

模型可见面的计算只认 `is_direct()`：

```rust
fn build_model_visible_specs(...) -> Vec<ToolSpec> {
    let mut specs = Vec::new();
    for tool in registry.entries() {
        let exposure = tool.exposure;
        if !exposure.is_direct() {
            continue;
        }
        ...
    }
    specs.extend(hosted_specs);
    merge_into_namespaces(specs)
        .into_iter()
        .filter(|spec| {
            namespace_tools_enabled(turn_context) || !matches!(spec, ToolSpec::Namespace(_))
        })
        .collect()
}
```
✅

### 2.4 MCP 准入 + 字节预算降级（Grok Build 明确没有的东西）

`codex-rs/core/src/mcp_tool_exposure.rs` 是 MCP 工具的入口闸门，四道过滤：

```rust
const MAX_AGENT_PLUGIN_MCP_SPEC_BYTES: usize = 8_000;
const MAX_AGENT_PLUGIN_MCP_TOTAL_BYTES: usize = 64_000;
```
✅

```rust
    let exposure = if search_tool_enabled {
        ToolExposure::Deferred
    } else {
        ToolExposure::Direct
    };
```
✅

**超预算直接降级为 Hidden**（注意：不是丢掉，是"注册了但模型看不见"）：

```rust
        let fits_agent_budget = if agent_plugin {
            handler.model_spec_bytes().is_ok_and(|bytes| {
                if bytes > MAX_AGENT_PLUGIN_MCP_SPEC_BYTES {
                    return false;
                }
                let next = agent_plugin_bytes.saturating_add(bytes);
                if next <= MAX_AGENT_PLUGIN_MCP_TOTAL_BYTES {
                    agent_plugin_bytes = next;
                    true
                } else {
                    false
                }
            })
        } else {
            true
        };
        let tool_exposure = if fits_agent_budget {
            exposure
        } else {
            ToolExposure::Hidden
        };
```
✅

其余三道：

```rust
fn filter_non_codex_apps_mcp_tools_only(
    mcp_tools: &[McpToolInfo],
) -> impl Iterator<Item = &McpToolInfo> + '_ {
    mcp_tools.iter().filter(|tool| {
        tool.server_name != CODEX_APPS_MCP_SERVER_NAME && tool_is_model_visible(tool)
    })
}
```
✅ —— MCP server 自带的 `visibility` 标记是一道准入。

Apps 工具（连接器）还要过策略求值，且**用注解参与决策**：

```rust
        app_tool_policy
            .policy(AppToolPolicyInput {
                connector_id: Some(connector_id),
                link_id: None,
                tool_name: &tool.tool.name,
                tool_title: tool.tool.title.as_deref(),
                destructive_hint: annotations.and_then(|annotations| annotations.destructive_hint),
                open_world_hint: annotations.and_then(|annotations| annotations.open_world_hint),
            })
            .enabled
```
✅ —— 与我们"按调用参数决策"的凭证闸同源思路，但 Codex 把 `destructive_hint` / `open_world_hint` 做成 MCP 注解的一等字段。

**这是本轮调查最重要的单项发现**：Codex 有一个真正会**按预算削减工具面**的机制（agent-plugin MCP 工具，单条 8 KB / 总计 64 KB，超限降级 Hidden），而 Grok Build 的 `should_list` / `tool_budget` / `max_tools` 是 0 命中。上一轮"Grok Build 没有任何按预算裁剪"的结论因此得到交叉印证——不是我漏查，是它真没有，而 Codex 有。

### 2.5 名字冲突：trusted 与 external 两套策略 + 保留名

这是最值得抄的一处，因为它**结构性地表达了"第一方 vs 第三方"的不对称**：

```rust
    pub(crate) fn register_trusted_with_exposure(
        &mut self,
        runtime: Arc<dyn CoreToolRuntime>,
        exposure: ToolExposure,
    ) {
        let tool_name = runtime.tool_name().with_default_namespace();
        ...
        match self.tools.entry(tool_name) {
            Entry::Vacant(entry) => {
                entry.insert(RegisteredTool { runtime, exposure });
            }
            Entry::Occupied(entry) => {
                let tool_name = entry.key();
                error_or_panic(format!("tool {tool_name} already registered"));
            }
        }
    }
```
✅ —— **第一方重名 = `error_or_panic`**（程序员错误，必须崩）。

```rust
        if tool_name.is_default_namespace()
            && matches!(tool_name.name.as_str(), "exec_command" | "shell_command")
        {
            tracing::warn!(tool_name = %tool_name, "skipping external tool with reserved name");
            if self.tools.contains_key(&tool_name) {
                self.record_collision(tool_name);
            }
            return false;
        }
```
✅ —— **保留名防抢占**：第三方不能注册 `exec_command` / `shell_command`。

```rust
            Entry::Occupied(entry) => {
                tracing::warn!(
                    tool_name = %entry.key(),
                    "skipping duplicate external tool that is already registered"
                );
                self.first_collision
                    .get_or_insert_with(|| entry.key().clone());
                false
            }
```
✅ —— **第三方重名 = warn + 跳过 + 记冲突**，不崩。

而且冲突可以升级为 fail-closed（配置项 `error_on_tool_collisions`）：

```rust
    if turn_context.config.tool_registry.error_on_tool_collisions {
        if let Some(tool_name) = registry.first_collision() {
            let namespace = tool_name.namespace.as_deref().unwrap_or("functions");
            let name = format!("{namespace}.{}", tool_name.name);
            return Err(CodexErrorDetails::ToolCollision(name).into());
        }
```
✅

命名空间归属与描述冲突同样 fail-closed：两个不同 MCP server 不能声称同一个 namespace，同一 namespace 的描述必须一致。

**对照我们的 R1 安全决策**（第三方 MCP 用不透明名、provider 身份不进 prompt）：Codex 的做法不同——它让第三方工具保留明文 `server__tool` 形态，但用"保留名 + 重名跳过 + 命名空间归属校验 + 可升级 fail-closed"四件套兜住。我们走不透明名是更强的隔离；但**"trusted 重名崩 / external 重名跳过"这条双策略本身与我们接第三方 MCP 的现实相符**，值得单独借鉴（我们目前两者混用同一套注册路径）。

### 2.6 tool_search：BM25，且结果进下一轮的工具面

给模型看的描述原文（`codex-rs/core/src/tools/handlers/tool_search_spec.rs`）：

```rust
    let description = format!(
        "# Tool discovery\n\nSearches over deferred tool metadata with BM25 and exposes matching tools for the next model call.{source_section}Some of the tools may not have been provided to you upfront, and you should use this tool (`{TOOL_SEARCH_TOOL_NAME}`) to search for the required tools. For MCP tool discovery, always use `{TOOL_SEARCH_TOOL_NAME}` instead of `list_mcp_resources` or `list_mcp_resource_templates`."
    );
```
✅

要点：
- **BM25**（与 Grok Build 的 `search_tool` 同算法）
- `TOOL_SEARCH_DEFAULT_LIMIT = 8`（Grok Build 是 5）
- **`exposes matching tools for the next model call`** —— 这条与 Grok Build **架构性不同**：Grok Build 的检索结果永不进工具面，要靠 `use_tool` 在同轮二次分发；Codex 把命中的工具**物化进下一次请求的工具列表**。
- 显式禁止用 `list_mcp_resources` 做发现（避免模型用错通道）
- 源清单有字节预算：`MAX_TOOL_SEARCH_SOURCE_DESCRIPTION_BYTES = 512 * 1024`，且带 `take_bytes_at_char_boundary` 的边界安全截断

`tool_search` 自身是 `ToolSpec` 的一等变体，不是普通 function tool：

```rust
    #[serde(rename = "tool_search")]
    ToolSearch {
        execution: String,
        description: String,
        parameters: JsonSchema,
    },
```
✅

并且它独占命名空间——别家工具不能声称叫 `tool_search`：

```rust
        // Special model tools own the namespace matching their wire identity, so
        // regular namespace tools cannot advertise that same model-visible surface.
```
✅

### 2.7 配置项

`ToolExposureSurface` 是可序列化的用户侧配置枚举（带 `JsonSchema` 与 `TS` 导出）：

```rust
/// A model-facing surface on which a tool can be exposed.
#[derive(Debug, Serialize, Deserialize, Clone, Copy, PartialEq, Eq, Display, JsonSchema, TS)]
#[serde(rename_all = "snake_case")]
#[strum(serialize_all = "snake_case")]
pub enum ToolExposureSurface {
    /// Nested tools available to Code Mode scripts.
    CodeMode,
    /// Tools discovered later through tool search.
    Deferred,
    /// Tools present in the model's initial tool list.
    Direct,
}
```
（`codex-rs/protocol/src/config_types.rs`）✅

🟡 **未定位**：消费该枚举的具体 config 结构体未在 `config_types.rs` 中找到（该文件只定义枚举本身）。也未能验证官方文档——`codex-rs/config.md` 已挪到站外（`docs/config.md` 只是一段指向 `developers.openai.com/codex/...` 的迁移说明），本次未抓取站外页面。所以"用户可在 config.toml 里逐工具指定暴露面"这一点**推断成立但未逐字证实**。

### 2.8 内置工具规模（参考）

`codex-rs/core/src/tools/handlers/` 目录枚举到 21 个 handler 文件 ✅：`apply_patch`、`current_time`、`dynamic`、`extension_tools`、`get_context_remaining`、`list_available_plugins_to_install`、`mcp`、`mcp_resource`、`multi_agents`、`multi_agents_v2`、`new_context_window`、`plan`、`request_permissions`、`request_plugin_install`、`request_user_input_async`、`send_message_to_user_async`、`sleep`、`test_sync`、`unified_exec`、`view_image`、`wait_for_environment`，外加 `tool_search`（来自 `codex-tools`）与 `tool_search_spec`。🟡 实际每轮注册数量取决于配置与 provider，未逐项统计。

---

## 3. 三系统横向对比

| 维度 | Grok Build | Codex | Pi |
|---|---|---|---|
| 第一方工具数 | 注册 52，进面 ~35 | ~21 个 handler（未精确统计） | 8（默认预设 4） |
| 暴露面种类 | 2（进上下文 / 走 search_tool） | **6**（Direct / Deferred / DeferredModelOnly / DirectModelOnly / CodeModeOnly / Hidden） | 1（都在） |
| 暴露面可配置 | 否（`config.tools` 白名单 + feature 开关） | **是**（`ToolExposureSurface` 枚举，可序列化到配置） | 否（预设整包切） |
| 每轮重算 | 否（会话级 finalize 一次） | **是**（`finalize_tool_router` 带 TurnContext） | 否 |
| 按能力降级 | 未见 | **是**（`model_info.supports_search_tool && provider.capabilities().namespace_tools`） | 不适用 |
| 第三方工具名 | 明文 `server__tool` | 明文 + namespace 归属校验 | 明文（extension 自己起名） |
| 重名冲突 | HashMap 覆盖（未见冲突处理） | **双策略**：trusted 崩 / external 跳过+记冲突，可升级 fail-closed | 未查 |
| 保留名保护 | 未见 | **有**（`exec_command` / `shell_command`） | 未见 |
| 按预算裁剪 | **无**（`should_list`/`tool_budget`/`max_tools` 全 0 命中） | **有**（agent-plugin 单条 8 KB / 总计 64 KB，超限 → Hidden） | 无（也用不着） |
| 检索算法 | BM25，limit 5 | BM25，limit 8 | — |
| 命中结果去向 | 不进工具面，`use_tool` 同轮二次分发 | **进下一轮工具面** | — |
| 命名空间 | `namespace:id` 内部约定 | **线上一等公民**（`ToolName { namespace, name }`、`ToolSpec::Namespace`、provider 需支持 namespace_tools） | 无 |

---

## 4. 对本仓的含义

按性价比排序，可直接落地的是这几条：

1. **`ToolExposure` 的 6 态枚举 + `is_direct()`/`is_deferred()`**（高）
   我们目前是"渲染进面 / 不渲染"二值，加上 petition 扩面。缺的正是 `Hidden`：
   **已注册可执行、但模型不可见**。CodingSubAgent 的静态工具带、被 profile 挡掉但仍需被别的工具调用的能力，
   都需要这个第三态。抄这个枚举的形状（含 `unreachable!("direct and deferred are mutually exclusive")` 那条不变量断言）成本极低。

2. **trusted / external 双策略冲突处理**（高）
   第一方重名 = 崩（暴露我们的 bug）；第三方重名 = warn + 跳过 + 记录，且**不覆盖已有注册**。
   这条直接对应我们接任意第三方 MCP 的现实，且与 R1（不透明名）**不冲突**——可以两套都上。

3. **保留名防抢占**（中，一行）
   第三方不得注册 `exec_command` 一类内置名。我们若最终引入 `namespace:id`，必须同步加这条，
   否则第三方 server 可以顶掉第一方工具。

4. **字节预算降级**（中）
   Codex 的 8 KB/64 KB 是"agent-plugin"专用。我们若要给动态 MCP 工具设预算，可以照抄这个形状：
   **超限不是丢弃而是降级为 Hidden**，保留可发现性/可执行性。注意这与我们的 `MaxToolBudget=28`（legacy router 的条数裁剪）是不同层面的东西——前者是字节、后者是条数，可以并存。

5. **`tool_search` 描述里显式禁止用错通道**（低，可直接抄）
   `"For MCP tool discovery, always use tool_search instead of list_mcp_resources..."`——
   在我们 `semantic_tools_search` 的描述里同样写明"不要用 X 做发现"，成本为零。

**不建议借鉴**：

- **Pi 的整包预设切换**（coding/read-only/all）。我们是多租户 + 第三方 MCP，整包切无法表达"这个用户装了 3 个 MCP"的组合状态。
- **Codex 的 provider 能力门控（`namespace_tools`）**。它依赖 OpenAI Responses API 的 namespace + defer_loading 语义，我们对接的是多 provider，抄不了这一层；**可抄的只是"能力探测 → 降级"这个决策形状**，门控条件要换成我们自己的（provider 是否支持 tool_choice、是否支持并行调用等）。
- **Pi 的"没有检索"**。它成立的前提是单用户本地 + 用户自己装扩展；我们是托管服务，能力来源不可控。

---

## 5. 未核实 / 未覆盖

- 🟡 `ToolExposureSurface` 的消费侧配置结构未定位；官方文档在站外（`developers.openai.com/codex/config-reference`），本次未抓取。
- 🟡 Codex 每轮实际注册的工具数未逐项统计（只枚举了 handlers 目录）。
- 🟡 Codex `codex_mode` / Code Mode 子层（嵌套脚本调用工具）本次只看了暴露面视角，未深入。
- ❌ 未调查：Codex 的权限/审批层（`execpolicy`、`sandboxing`、`guardian`）与工具面的交互；`mcp_tool_call.rs` 的执行路径；hooks 对工具面的影响。
- ❌ 未调查：Pi 的扩展加载顺序、扩展间工具重名如何决出、Pi 的 compaction 策略。
- ❌ 未调查：其它常被提及的对照对象（Claude Code / Cursor / Amp / OpenCode）。本仓 `tool-routing-improvement-plan-zh.md` 若需要更宽的样本，应再补一轮。
