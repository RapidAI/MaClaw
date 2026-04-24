# MacLaw GUI 程序操作测试能力——机制审视与改进计划

## 一、问题本质

MacLaw 对 GUI 程序的"测试"能力，不是缺少某个功能或某个工具。问题在于**架构层面存在三个机制性断裂**，导致 Agent 无法形成"操作→观测→判断→修正"的闭环。

---

## 二、三个机制性断裂

### 断裂 1：浏览器和桌面是两套独立的执行引擎，共享零代码

**现状**：

`corelib/browser/` 和 `corelib/guiautomation/` 是两套完全独立的实现，各自有自己的：

| 概念 | browser 包 | guiautomation 包 |
|------|-----------|-----------------|
| 任务规格 | `TaskSpec` | `GUITaskSpec` |
| 步骤规格 | `StepSpec` | `GUIStepSpec` |
| 执行引擎 | `BrowserTaskSupervisor` | `GUITaskSupervisor` |
| 重试策略 | `RetryStrategy`（10 种 FailureType） | `GUIRetryStrategy`（3 种 FailureType） |
| 验证标准 | `CriterionSpec`（8 种 type） | `GUICriterionSpec`（3 种 type） |
| 验证器 | `TaskVerifier`（完整实现） | **不存在** |
| 录制流程 | `RecordedFlow` | `GUIRecordedFlow` |
| 回放器 | `FlowReplayer` | `GUIReplayer` |
| 后台执行 | `RunReplayInBackground` | `RunGUIReplayInBackground` |
| 活动通知 | `ActivityUpdater` | `GUIActivityUpdater` |
| 循环管理 | `LoopManager` | `GUILoopManager` |

两套代码的结构几乎一模一样（对比 `replay_background.go` 两个版本，逻辑完全相同，只是类型名不同）。这不是"代码重复"的美学问题——它导致了一个机制性后果：

**浏览器侧的每一个能力提升，桌面侧都需要手动复制一遍。** 浏览器侧有 `TaskVerifier`（8 种 criterion + `WaitForStable`），桌面侧没有。浏览器侧的 `RetryStrategy` 有 10 种失败分类（含 `FailureVerificationFailed`、`FailureWrongTab`、`FailurePopupNotCaptured` 等），桌面侧只有 3 种。浏览器侧的 `executeStepWithRetry` 在步骤成功后还会检查 `step.Verify`（per-step 验证），桌面侧没有。

**根因**：两套引擎在设计时没有提取共同的执行合约（execution contract）。它们解决的是同一个问题——"按步骤执行操作序列，每步可重试，最终验证成功标准"——但各自从零实现。

### 断裂 2：桌面 GUI 验证是空壳——定义了类型但未实现检查

`GUICriterionSpec` 定义了 `ocr_contains`/`window_exists`/`element_exists` 三种验证类型。`GUITaskSpec` 有 `SuccessCriteria []GUICriterionSpec` 字段。但：

1. `GUITaskSupervisor.Execute()` 在所有步骤执行完毕后**直接标记 `completed`**，不检查 `SuccessCriteria`
2. 不存在 `GUIVerifier` 类型——没有任何代码实现 `ocr_contains`/`window_exists`/`element_exists` 的检查逻辑
3. 没有 `gui_verify` 工具——LLM 无法主动触发桌面 GUI 状态验证
4. `GUIStepSpec` 没有 `Verify` 字段——无法做 per-step 验证

对比浏览器侧：
- `BrowserTaskSupervisor` 构造时注入 `TaskVerifier`
- `Execute()` 末尾调用 `verifier.WaitForStable()` + `verifier.Verify(spec.SuccessCriteria)`
- `executeStepWithRetry()` 在步骤成功后检查 `step.Verify`
- `browser_task_verify` 工具让 LLM 可以随时验证页面状态

**后果**：桌面 GUI 回放永远报告成功。Agent 无法知道回放是否真的达到了预期效果。

**根因**：断裂 1 的直接后果。如果两套引擎共享执行合约，验证逻辑只需实现一次。

### 断裂 3：Agent 对桌面 GUI 的观测只有截图——没有结构化状态反馈

Agent 操作浏览器时，有丰富的结构化反馈：
- `browser_observe`：返回 DOM refs、console 日志、network 事件、页面 URL/title
- `browser_get_text`/`browser_get_html`：提取页面内容
- `browser_task_verify`：结构化验证 8 种 criterion
- 每步执行后的 `Checkpoint`：URL + title + tabID + frameID + screenshot

Agent 操作桌面 GUI 时，唯一的观测手段是 `screenshot`（截屏）。截屏返回 base64 PNG，Agent 必须通过 LLM vision 理解图像内容。这意味着：

1. **每次观测消耗一次完整的 LLM 推理**（~130K input token），而浏览器侧的 `browser_observe` 返回结构化文本，零额外 LLM 调用
2. **观测精度取决于 LLM 的视觉理解能力**，而非确定性的 DOM/accessibility 查询
3. **无法做精确断言**——LLM 看截图说"看起来登录成功了"，但无法确定性地验证"用户名显示为 admin"

**根因**：`accessibility.Bridge` 已经提供了结构化的 GUI 状态访问（`EnumElements`/`FindElement`/`GetValue`），但这个能力没有被暴露为 Agent 可用的观测工具。Bridge 只在 `ElementLocator` 内部使用（用于元素定位），不用于状态观测。

---

## 三、机制性修复方案

### 修复 1：提取统一执行合约——消除两套引擎的平行实现

**设计原则**：浏览器和桌面 GUI 解决的是同一个问题——"按步骤执行操作序列，每步可重试，最终验证成功标准"。差异只在两个注入点：**步骤如何执行**（CDP vs 输入模拟）和**状态如何观测**（DOM vs accessibility tree）。

#### 新增 `corelib/taskengine/` 包

```go
package taskengine

// StepExecutor 是步骤执行的注入点。浏览器和桌面各自实现。
type StepExecutor interface {
    Execute(ctx context.Context, step StepSpec) error
}

// StateObserver 是状态观测的注入点。浏览器和桌面各自实现。
type StateObserver interface {
    // Snapshot 返回当前状态的结构化快照（用于重试决策和验证）
    Snapshot() (*StateSnapshot, error)
    // Verify 检查一组验证标准
    Verify(criteria []CriterionSpec) (*VerifyResult, error)
    // WaitForStable 等待状态稳定（浏览器=网络静默，桌面=窗口不再变化）
    WaitForStable(timeout time.Duration) error
}

// Supervisor 是统一的任务执行引擎。
type Supervisor struct {
    executor StepExecutor
    observer StateObserver
    retrier  *RetryStrategy
    ...
}

func (s *Supervisor) Execute(spec TaskSpec) (*TaskState, error) {
    // 与当前 BrowserTaskSupervisor.Execute 相同的逻辑：
    // 1. 遍历步骤
    // 2. 每步 executeStepWithRetry（含 per-step Verify）
    // 3. 最终 SuccessCriteria 验证
    // 4. Checkpoint 记录
    // 5. Pause/Resume/Cancel 支持
}
```

#### 统一类型

```go
// StepSpec 统一步骤规格
type StepSpec struct {
    Action    string            `json:"action"`
    Params    map[string]string `json:"params"`
    Verify    *CriterionSpec    `json:"verify,omitempty"`    // per-step 验证
    Timeout   time.Duration     `json:"timeout,omitempty"`
    Fallbacks []LocatorSpec     `json:"fallbacks,omitempty"` // 元素定位 fallback
}

// CriterionSpec 统一验证标准
type CriterionSpec struct {
    Type     string `json:"type"`
    Target   string `json:"target"`    // "browser"/"desktop"/"auto"
    Selector string `json:"selector"`  // CSS selector 或 accessibility role::name
    Pattern  string `json:"pattern"`
    Window   string `json:"window"`    // desktop only
    TabID    string `json:"tab_id"`    // browser only
    FrameID  string `json:"frame_id"`  // browser only
    Timeout  int    `json:"timeout"`
}

// StateSnapshot 统一状态快照
type StateSnapshot struct {
    // 通用
    ScreenshotB64 string   `json:"screenshot_b64,omitempty"`
    OCRText       []OCRResult `json:"ocr_text,omitempty"`
    // 浏览器特有
    URL           string   `json:"url,omitempty"`
    Title         string   `json:"title,omitempty"`
    DOMSnippet    string   `json:"dom_snippet,omitempty"`
    ConsoleEvents []string `json:"console_events,omitempty"`
    NetworkEvents []string `json:"network_events,omitempty"`
    // 桌面特有
    WindowTitle   string   `json:"window_title,omitempty"`
    FocusedElement *AccessibilityRef `json:"focused_element,omitempty"`
}
```

#### 迁移路径

1. `BrowserTaskSupervisor` 改为包装 `taskengine.Supervisor`，注入 `BrowserStepExecutor` + `BrowserStateObserver`
2. `GUITaskSupervisor` 改为包装 `taskengine.Supervisor`，注入 `GUIStepExecutor` + `GUIStateObserver`
3. 两套 `replay_background.go` 合并为一个 `taskengine.RunReplayInBackground`
4. 两套 `RetryStrategy` 合并——通用重试逻辑在 `taskengine`，平台特有的失败分类通过 `StepExecutor` 返回的 error 类型区分
5. 旧类型保留为 type alias（backward compat）

**机制性保证**：未来任何执行引擎的改进（新的重试策略、新的验证类型、per-step 验证、checkpoint 增强）自动对浏览器和桌面同时生效。新增第三种 GUI 目标（如移动端）只需实现 `StepExecutor` + `StateObserver`。

### 修复 2：实现 `GUIStateObserver`——让桌面 GUI 有结构化的状态观测

**根因回顾**：`accessibility.Bridge` 已经提供了结构化 GUI 状态访问，但只在 `ElementLocator` 内部使用。

#### `GUIStateObserver` 实现

```go
type GUIStateObserver struct {
    bridge       accessibility.Bridge
    ocr          browser.OCRProvider
    screenshotFn func() (string, error)
}

func (o *GUIStateObserver) Snapshot() (*taskengine.StateSnapshot, error) {
    snap := &taskengine.StateSnapshot{}
    
    // 截屏
    if img, err := o.screenshotFn(); err == nil {
        snap.ScreenshotB64 = img
    }
    
    // Accessibility：获取当前焦点窗口和焦点元素
    // （不枚举整棵树——太慢。只获取焦点信息。）
    if o.bridge != nil {
        // 获取前台窗口标题
        // 获取焦点元素的 role/name/value
    }
    
    // OCR（可选，仅在需要时调用）
    if o.ocr != nil && snap.ScreenshotB64 != "" {
        if results, err := o.ocr.Recognize(snap.ScreenshotB64); err == nil {
            snap.OCRText = results
        }
    }
    
    return snap, nil
}

func (o *GUIStateObserver) Verify(criteria []taskengine.CriterionSpec) (*taskengine.VerifyResult, error) {
    result := &taskengine.VerifyResult{Passed: true}
    for _, c := range criteria {
        cr := o.checkOne(c)
        result.Details = append(result.Details, cr)
        if !cr.Passed {
            result.Passed = false
        }
    }
    return result, nil
}

func (o *GUIStateObserver) checkOne(c taskengine.CriterionSpec) taskengine.CriterionResult {
    switch c.Type {
    case "text_contains":    // 截屏 → OCR → 文本包含
    case "element_exists":   // Bridge.FindElement(window, role, name)
    case "element_value":    // Bridge.FindElement → Bridge.GetValue
    case "window_exists":    // Bridge.EnumElements("") → 标题匹配
    case "window_title":     // 前台窗口标题匹配
    case "screenshot_match": // 截屏 → 区域裁剪 → NCC 与基线对比
    }
}

func (o *GUIStateObserver) WaitForStable(timeout time.Duration) error {
    // 周期性截屏，比较连续两帧的差异。
    // 差异低于阈值持续 1 秒 → 稳定。
    // 这是桌面 GUI 的"网络静默"等价物。
}
```

**机制性保证**：`GUIStateObserver` 实现 `taskengine.StateObserver` 接口后，`taskengine.Supervisor` 的验证逻辑（per-step Verify + 最终 SuccessCriteria）自动对桌面 GUI 生效。不需要在 `GUITaskSupervisor` 中手动添加验证代码。

### 修复 3：暴露 `gui_observe` 工具——让 Agent 有结构化的桌面观测能力

**根因回顾**：Agent 对桌面 GUI 的唯一观测手段是 `screenshot`（截屏 → LLM vision），没有结构化的状态反馈。

#### 新增 `gui_observe` 工具

```
gui_observe(window="记事本")
→ 返回：
{
  "window_title": "无标题 - 记事本",
  "window_bounds": {"x": 100, "y": 100, "width": 800, "height": 600},
  "focused_element": {"role": "Edit", "name": "文本编辑器", "value": "Hello World"},
  "elements": [
    {"role": "MenuBar", "name": "菜单栏", "children": [
      {"role": "MenuItem", "name": "文件"},
      {"role": "MenuItem", "name": "编辑"},
      ...
    ]},
    {"role": "Edit", "name": "文本编辑器", "value": "Hello World", "bounds": {...}}
  ],
  "ocr_text": "无标题 - 记事本\n文件 编辑 格式 查看 帮助\nHello World"
}
```

这给 Agent 提供了与 `browser_observe` 等价的结构化反馈：
- **元素树**（类似 DOM refs）：Agent 知道窗口里有哪些控件、它们的角色和名称
- **焦点元素**（类似 active element）：Agent 知道当前输入焦点在哪
- **元素值**（类似 input.value）：Agent 可以验证输入框的内容
- **OCR 文本**（类似 innerText）：Agent 可以读取屏幕上的文字

**关键设计决策**：`gui_observe` 不截屏（截屏有 30 秒冷却且消耗 LLM vision token）。它返回纯文本的结构化数据，Agent 可以直接用文本匹配判断状态，不需要 LLM vision。

#### 新增 `gui_verify` 工具

与 `browser_task_verify` 对齐，让 Agent 可以主动验证桌面 GUI 状态：

```
gui_verify(criteria=[
  {"type": "window_exists", "pattern": "记事本"},
  {"type": "element_value", "window": "记事本", "selector": "Edit::文本编辑器", "pattern": "Hello World"},
  {"type": "text_contains", "pattern": "保存成功"}
])
→ 返回：
{
  "passed": true,
  "details": [
    {"criterion": {...}, "passed": true, "actual": "无标题 - 记事本"},
    {"criterion": {...}, "passed": true, "actual": "Hello World"},
    {"criterion": {...}, "passed": true, "actual": "保存成功"}
  ]
}
```

`gui_verify` 内部委托给 `GUIStateObserver.Verify()`——与 `taskengine.Supervisor` 的验证逻辑共享同一套代码。

**机制性保证**：Agent 对桌面 GUI 的观测从"截屏 → LLM vision 猜测"升级为"结构化查询 → 确定性判断"。观测成本从 ~130K token/次降到 ~500 token/次。

### 修复 4：`async_wait` 接入 `StateObserver`——统一等待机制

**根因回顾**：`async_wait` 支持 `file_exists`/`process_done` 等条件，但不支持 GUI 状态条件。Agent 等待 GUI 程序启动时只能用 screenshot 轮询。

#### 机制性修复

`async_wait` 的条件检查从硬编码的 switch-case 改为**可注入的 `ConditionChecker` 接口**：

```go
type ConditionChecker interface {
    Check(conditionType, conditionArg, pattern string) (satisfied bool, detail string, err error)
}
```

内置 checker 实现文件/进程条件（已有）。`GUIStateObserver` 实现 GUI 条件：

| 条件 | 实现 |
|------|------|
| `window_exists` | `Bridge.EnumElements("")` → 标题匹配 |
| `window_closed` | 同上，取反 |
| `element_exists` | `Bridge.FindElement(window, role, name)` |
| `text_visible` | 截屏 → OCR → 文本匹配 |
| `gui_stable` | 连续两帧截屏差异低于阈值 |

`BrowserStateObserver` 实现浏览器条件：

| 条件 | 实现 |
|------|------|
| `url_contains` | `Session.Info().URL` 包含 |
| `dom_exists` | `Session.WaitForSelector()` |
| `page_stable` | `Verifier.WaitForStable()` |

`async_wait` 在初始化时注入所有可用的 checker。条件类型自动路由到对应的 checker。

**机制性保证**：新增任何 GUI 目标（移动端、远程桌面等），只需实现 `ConditionChecker`，`async_wait` 自动支持该目标的等待条件。不需要改 `async_wait` 的代码。

### 修复 5：Accessibility Bridge 从进程启动改为常驻 Sidecar

**根因**：`bridge_windows.go` 每次调用都 `exec.Command("powershell", ...)` 启动新进程。单次延迟 500ms-2s。这不是性能优化问题——它使得 `gui_observe` 和 `GUIStateObserver.Verify()` 在实际使用中不可行（10 个 criterion 的验证需要 5-20 秒）。

**修复**：复用 `RapidOCRSidecar` 的模式——编译一个 C# 控制台程序 `maclaw-uia-sidecar.exe`，启动后通过 stdin/stdout JSON-RPC 通信。UI Automation COM 对象只初始化一次，后续查询延迟 <50ms。

```go
type windowsBridge struct {
    sidecar *UIASidecar  // 常驻进程，JSON-RPC 通信
    fallback *powershellBridge  // sidecar 不可用时回退
}

func (b *windowsBridge) FindElement(windowTitle, role, name string) (*Element, error) {
    if b.sidecar != nil && b.sidecar.IsAlive() {
        return b.sidecar.FindElement(windowTitle, role, name)  // <50ms
    }
    return b.fallback.FindElement(windowTitle, role, name)  // 500ms-2s
}
```

**机制性保证**：Bridge 的消费方（`ElementLocator`、`GUIStateObserver`、`gui_observe` 工具）不需要任何改动。接口不变，只是实现从"每次启动进程"变为"与常驻进程通信"。

---

## 四、依赖关系与实施顺序

```
修复 1: 统一执行合约 (taskengine 包)
  │
  ├──→ 修复 2: GUIStateObserver (实现 StateObserver 接口)
  │      │
  │      ├──→ 修复 3: gui_observe + gui_verify 工具
  │      │
  │      └──→ 修复 4: async_wait 接入 StateObserver
  │
  └──→ 迁移 BrowserTaskSupervisor + GUITaskSupervisor

修复 5: Accessibility Sidecar (独立，但修复 2/3/4 的实用性依赖它)
```

修复 1 是地基——不做它，后续每个修复都会在两套引擎中各实现一遍，重复断裂 1 的错误。

修复 5 是修复 2/3/4 的性能前提——没有它，`gui_observe` 每次调用 2 秒，`gui_verify` 10 个 criterion 需要 20 秒，实际不可用。但修复 5 在代码层面与修复 1-4 独立，可以并行开发。

---

## 五、不做什么

以下是之前版本中列出但**不应该做**的事情，因为它们是 workaround 而非机制性修复：

1. **不单独给 `GUITaskSupervisor.Execute()` 加验证代码**——这是在断裂的架构上打补丁。正确做法是修复 1（统一执行合约），验证逻辑自动生效。

2. **不给 `GUICriterionSpec` 单独写 `GUIVerifier`**——这会创建第三套验证实现（browser `TaskVerifier` + gui `GUIVerifier` + 未来的 `MobileVerifier`）。正确做法是修复 1 的统一 `CriterionSpec` + 修复 2 的 `StateObserver` 接口。

3. **不在 `async_wait` 的 switch-case 中硬编码 GUI 条件**——这会让 `async_wait` 直接依赖 `accessibility.Bridge`，耦合两个不相关的包。正确做法是修复 4 的 `ConditionChecker` 接口注入。

4. **不做"Visual Regression 截图基线对比"作为独立功能**——它应该是 `CriterionSpec` 的一种 type（`screenshot_match`），由 `StateObserver.Verify()` 统一处理。不需要独立的 `VisualDiffTool`。

5. **不做"测试报告聚合"作为独立功能**——它应该是 `taskengine.Supervisor` 的内置能力（每次 Execute 的结果自动持久化），不需要独立的 `ReportStore`。

---

## 六、工作量估算

| 修复 | 新增/修改 | 预估 | 风险 |
|------|----------|------|------|
| 1. taskengine 包 + 迁移 | 1 新包 + 4 文件重构 | 5 天 | 中——需要保证 backward compat |
| 2. GUIStateObserver | 1 新文件 | 2 天 | 低——依赖已有的 Bridge/OCR |
| 3. gui_observe + gui_verify | 2 新文件 + 注册 | 2 天 | 低 |
| 4. async_wait ConditionChecker | 1 接口 + 2 实现 + 1 重构 | 2 天 | 低 |
| 5. UIA Sidecar | 1 C# 项目 + 1 Go 重构 | 5 天 | 高——跨语言，需要 C# 编译 |

建议顺序：5（并行启动）→ 1 → 2 → 3 → 4。

修复 1+2+3 完成后，Agent 对桌面 GUI 的测试能力从"截屏猜测"升级为"结构化观测 + 确定性验证"，与浏览器侧对齐。修复 4 补全等待机制。修复 5 让整个体系在实际使用中可行（延迟从秒级降到毫秒级）。
