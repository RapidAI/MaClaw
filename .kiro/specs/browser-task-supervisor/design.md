# Browser Task Supervisor 设计文档

## Overview

本特性为 MaClaw 新增浏览器任务监管层（BrowserTaskSupervisor），提供浏览器任务的长程监管、成功验证、智能重试、OCR 页面感知和操作录制回放能力。核心目标：提升浏览器自动化任务的完成率，降低人类干预频率。

设计原则：
- 复用现有基础设施（BackgroundLoopManager、StallDetector 模式、browser.Session CDP 工具）
- OCR 优先于 LLM Vision（离线可用、零 token 成本），LLM Vision 作为 fallback
- 录制的操作骨架 + LLM 运行时自适应，兼顾可靠性和灵活性
- sidecar 模式集成 RapidOCR，按需安装，不增加主程序体积

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Agent (MaClaw LLM)                    │
│  browser_task_run / browser_task_replay / browser_ocr   │
└──────────────────────────┬──────────────────────────────┘
                           │ tool calls
┌──────────────────────────▼──────────────────────────────┐
│              BrowserTaskSupervisor                       │
│  ┌─────────────┐ ┌──────────────┐ ┌──────────────────┐  │
│  │ TaskExecutor │ │ TaskVerifier │ │ RetryStrategy    │  │
│  │ (逐步执行)   │ │ (成功验证)   │ │ (重试决策)       │  │
│  └──────┬──────┘ └──────┬───────┘ └────────┬─────────┘  │
│         │               │                  │             │
│  ┌──────▼───────────────▼──────────────────▼──────────┐  │
│  │              OCRProvider (页面感知)                  │  │
│  │  ┌─────────────────┐  ┌──────────────────────────┐ │  │
│  │  │ RapidOCRSidecar │  │ LLMVisionProvider        │ │  │
│  │  │ (~/.maclaw/ocr/) │  │ (fallback, 需多模态模型) │ │  │
│  │  └─────────────────┘  └──────────────────────────┘ │  │
│  └────────────────────────────────────────────────────┘  │
│                                                          │
│  ┌────────────────────┐  ┌─────────────────────────────┐ │
│  │ BrowserRecorder    │  │ FlowReplayer               │ │
│  │ (CDP 事件录制)     │  │ (回放 + LLM 自适应调整)    │ │
│  └────────────────────┘  └─────────────────────────────┘ │
└──────────────────────────┬──────────────────────────────┘
                           │ CDP protocol
┌──────────────────────────▼──────────────────────────────┐
│              browser.Session (现有 CDP 工具)             │
│  Navigate / Click / Type / Screenshot / GetText / Eval  │
└─────────────────────────────────────────────────────────┘
```

### 与现有组件的关系

| 现有组件 | 复用方式 |
|---------|---------|
| `BackgroundLoopManager` | 新增 `SlotKindBrowser`（默认并发 2），管理浏览器任务生命周期 |
| `browser.Session` | 所有浏览器操作通过现有 CDP 工具执行 |
| `ProgressTracker` | 扩展识别浏览器任务步骤模式 |
| `SessionMonitor` | 复用轮询模式监控任务状态 |
| `StatusEvent` | 复用事件通道推送任务进度到 GUI/TUI |

## Components and Interfaces

### BrowserTaskSupervisor（browser/task_supervisor.go）

```go
// TaskSpec 定义一个浏览器任务
type TaskSpec struct {
    ID              string            `json:"id"`
    Description     string            `json:"description"`
    Steps           []StepSpec        `json:"steps"`
    SuccessCriteria []CriterionSpec   `json:"success_criteria"`
    MaxRetries      int               `json:"max_retries"`      // 默认 3
    StepTimeout     time.Duration     `json:"step_timeout"`     // 默认 30s
}

// StepSpec 定义单个操作步骤
type StepSpec struct {
    Action   string            `json:"action"`   // navigate, click, type, wait, eval, scroll, select
    Params   map[string]string `json:"params"`   // action-specific params (url, selector, text, etc.)
    Verify   *CriterionSpec    `json:"verify"`   // 可选的步骤级验证
    Timeout  time.Duration     `json:"timeout"`  // 覆盖全局 StepTimeout
}

// CriterionSpec 定义成功标准
type CriterionSpec struct {
    Type     string `json:"type"`     // dom_exists, dom_text, url_contains, url_matches, ocr_contains
    Selector string `json:"selector"` // CSS selector (for dom_*)
    Pattern  string `json:"pattern"`  // 匹配模式 (文本/正则/URL pattern)
}

// TaskState 任务执行状态
type TaskState struct {
    ID            string         `json:"id"`
    Status        string         `json:"status"`  // running, paused, completed, failed
    CurrentStep   int            `json:"current_step"`
    TotalSteps    int            `json:"total_steps"`
    RetryCount    int            `json:"retry_count"`
    LastError     string         `json:"last_error"`
    Checkpoints   []Checkpoint   `json:"checkpoints"`
    StartedAt     time.Time      `json:"started_at"`
}

// Checkpoint 步骤检查点
type Checkpoint struct {
    StepIndex    int    `json:"step_index"`
    URL          string `json:"url"`
    Title        string `json:"title"`
    ScreenshotB64 string `json:"screenshot_b64,omitempty"` // 最近一张，不全部保留
    Timestamp    time.Time `json:"timestamp"`
}

// BrowserTaskSupervisor 浏览器任务监管器
type BrowserTaskSupervisor struct {
    mu          sync.RWMutex
    tasks       map[string]*TaskState
    verifier    *TaskVerifier
    retrier     *RetryStrategy
    ocr         OCRProvider
    recorder    *BrowserRecorder
    loopMgr     *agent.BackgroundLoopManager
    statusC     chan agent.StatusEvent
    logger      func(string)
}

func NewBrowserTaskSupervisor(loopMgr *agent.BackgroundLoopManager,
    statusC chan agent.StatusEvent, logger func(string)) *BrowserTaskSupervisor

// Execute 执行浏览器任务（在 BackgroundLoop 中运行）
func (s *BrowserTaskSupervisor) Execute(spec TaskSpec) (*TaskState, error)

// GetState 获取任务状态
func (s *BrowserTaskSupervisor) GetState(taskID string) (*TaskState, bool)

// Verify 对当前页面执行成功标准验证
func (s *BrowserTaskSupervisor) Verify(criteria []CriterionSpec) (*VerifyResult, error)

// Cancel 取消正在执行的任务
func (s *BrowserTaskSupervisor) Cancel(taskID string) error
```

### TaskVerifier（browser/task_verifier.go）

```go
// VerifyResult 验证结果
type VerifyResult struct {
    Passed   bool              `json:"passed"`
    Details  []CriterionResult `json:"details"`
}

// CriterionResult 单个标准的验证结果
type CriterionResult struct {
    Criterion CriterionSpec `json:"criterion"`
    Passed    bool          `json:"passed"`
    Actual    string        `json:"actual"`    // 实际值
    Error     string        `json:"error"`     // 失败原因
}

type TaskVerifier struct {
    ocr     OCRProvider
    session func() (*Session, error) // 获取当前 browser session
}

func NewTaskVerifier(ocr OCRProvider, sessionFn func() (*Session, error)) *TaskVerifier

// Verify 执行所有验证标准
func (v *TaskVerifier) Verify(criteria []CriterionSpec) (*VerifyResult, error)

// WaitForStable 等待页面稳定（无新网络请求/DOM 变化持续 1 秒）
func (v *TaskVerifier) WaitForStable(timeout time.Duration) error
```

验证逻辑：
- `dom_exists`: `browser_wait(selector, timeout=5s)` → 元素存在则 pass
- `dom_text`: `browser_get_text(selector)` → 文本包含 pattern 则 pass
- `url_contains`: `browser_info().URL` → URL 包含 pattern 则 pass
- `url_matches`: `browser_info().URL` → URL 正则匹配 pattern 则 pass
- `ocr_contains`: `browser_screenshot()` → `OCRProvider.Recognize()` → 任一结果文本包含 pattern 则 pass

### RetryStrategy（browser/retry_strategy.go）

```go
// FailureType 失败类型
type FailureType int

const (
    FailureElementNotFound FailureType = iota
    FailureTimeout
    FailurePageChanged
    FailureUnknownState
    FailureVerificationFailed
)

// RetryDecision 重试决策
type RetryDecision struct {
    ShouldRetry    bool              `json:"should_retry"`
    AdjustedStep   *StepSpec         `json:"adjusted_step"`   // 调整后的步骤（nil 表示原样重试）
    WaitBefore     time.Duration     `json:"wait_before"`     // 重试前等待时间
    Reason         string            `json:"reason"`
    NeedsLLM       bool              `json:"needs_llm"`       // 是否需要 LLM 参与决策
    LLMContext     string            `json:"llm_context"`     // 发给 LLM 的上下文
}

type RetryStrategy struct {
    maxStepRetries int // 默认 3
    maxTaskRetries int // 默认 3
    ocr            OCRProvider
}

func NewRetryStrategy(maxStepRetries, maxTaskRetries int, ocr OCRProvider) *RetryStrategy

// Decide 根据失败信息决定重试策略
func (r *RetryStrategy) Decide(failure FailureType, step StepSpec,
    stepRetryCount int, pageState *PageSnapshot) *RetryDecision

// ClassifyFailure 从错误信息推断失败类型
func (r *RetryStrategy) ClassifyFailure(err error, step StepSpec) FailureType
```

重试策略矩阵：

| 失败类型 | 第 1 次重试 | 第 2 次重试 | 第 3 次重试 |
|---------|-----------|-----------|-----------|
| ElementNotFound | 等待 5s 后重试 | 等待 10s + 尝试替代 selector | 截图 + OCR → LLM 决策 |
| Timeout | timeout × 2 重试 | timeout × 3 重试 | 标记失败 |
| PageChanged | 截图 + OCR 分析当前状态 → 从断点继续 | LLM 重新规划 | 标记失败 |
| UnknownState | 截图 + OCR → LLM 决策 | LLM 重新规划 | 标记失败 |

### OCRProvider（browser/ocr_provider.go）

```go
// OCRResult 单个 OCR 识别结果
type OCRResult struct {
    Text       string    `json:"text"`
    Confidence float64   `json:"confidence"`
    BBox       [4]int    `json:"bbox"` // x, y, width, height
}

// OCRProvider OCR 提供者接口
type OCRProvider interface {
    // Recognize 识别 base64 编码的 PNG 图片中的文本
    Recognize(pngBase64 string) ([]OCRResult, error)
    // IsAvailable 检查 OCR 是否可用
    IsAvailable() bool
    // Close 释放资源
    Close()
}

// FormatForLLM 将 OCR 结果格式化为 LLM 可理解的文本
func FormatForLLM(results []OCRResult) string
```

`FormatForLLM` 输出格式：
```
页面 OCR 识别结果（共 N 个文本区域）:
[100,200,300,50] "请输入验证码" (置信度: 0.95)
[100,260,200,40] "用户名或密码错误" (置信度: 0.92)
[400,500,100,40] "重新登录" (置信度: 0.98)
```

### RapidOCRSidecar（browser/ocr_rapidocr.go）

```go
type RapidOCRSidecar struct {
    mu        sync.Mutex
    cmd       *exec.Cmd
    stdin     io.WriteCloser
    scanner   *bufio.Scanner
    ready     bool
    idleTimer *time.Timer  // 5 分钟空闲自动退出
    ocrDir    string       // ~/.maclaw/ocr/
    logger    func(string)
}

func NewRapidOCRSidecar(logger func(string)) *RapidOCRSidecar

// EnsureReady 自动检测/安装/启动 RapidOCR sidecar
// 安装流程: 检测 python3 → pip install rapidocr-onnxruntime → 写入 ocr_server.py → 启动
func (s *RapidOCRSidecar) EnsureReady() error

// Recognize 实现 OCRProvider 接口
func (s *RapidOCRSidecar) Recognize(pngBase64 string) ([]OCRResult, error)

// IsAvailable 检查 sidecar 是否可用
func (s *RapidOCRSidecar) IsAvailable() bool

// Close 停止 sidecar 进程
func (s *RapidOCRSidecar) Close()
```

sidecar 通信协议（stdin/stdout JSON，每行一个 JSON）：

请求：`{"method": "ocr", "image_base64": "iVBOR..."}`
响应：`{"results": [{"text": "登录", "confidence": 0.95, "bbox": [100, 200, 80, 30]}]}`

健康检查：`{"method": "ping"}` → `{"status": "ok"}`

### LLMVisionProvider（browser/ocr_llm_vision.go）

```go
type LLMVisionProvider struct {
    sendImage func(base64 string, prompt string) (string, error)
}

func NewLLMVisionProvider(sendImage func(string, string) (string, error)) *LLMVisionProvider

// Recognize 通过 LLM 视觉能力识别图片文本
// 发送 prompt: "请识别这张网页截图中的所有文本内容，按位置列出"
func (p *LLMVisionProvider) Recognize(pngBase64 string) ([]OCRResult, error)
```

### CompositeOCRProvider（browser/ocr_composite.go）

```go
// CompositeOCRProvider 组合 OCR 提供者，按优先级 fallback
type CompositeOCRProvider struct {
    providers []OCRProvider // [RapidOCR, LLMVision]
}

func NewCompositeOCRProvider(providers ...OCRProvider) *CompositeOCRProvider

// Recognize 按优先级尝试，第一个成功的返回
func (c *CompositeOCRProvider) Recognize(pngBase64 string) ([]OCRResult, error)
```

Fallback 链：RapidOCR → LLM Vision → 返回空结果（不报错，让调用方 fallback 到 DOM 文本）

### BrowserRecorder（browser/recorder.go）

```go
// RecordedFlow 录制的操作流程
type RecordedFlow struct {
    Name            string          `json:"name"`
    Description     string          `json:"description"`
    RecordedAt      time.Time       `json:"recorded_at"`
    StartURL        string          `json:"start_url"`
    Steps           []RecordedStep  `json:"steps"`
    SuccessCriteria []CriterionSpec `json:"success_criteria,omitempty"`
}

// RecordedStep 录制的单个操作步骤
type RecordedStep struct {
    Action    string            `json:"action"`    // navigate, click, type, scroll
    Selector  string            `json:"selector"`  // 推断的 CSS selector
    Coords    [2]int            `json:"coords"`    // 原始坐标 [x, y]（click 的 fallback）
    Text      string            `json:"text"`      // 输入文本（type action）
    URL       string            `json:"url"`       // 导航 URL（navigate action）
    Timestamp time.Duration     `json:"timestamp"` // 相对于录制开始的时间偏移
    Snapshot  *RecordedSnapshot `json:"snapshot"`  // 操作后的页面快照
}

// RecordedSnapshot 录制时的页面快照
type RecordedSnapshot struct {
    URL   string `json:"url"`
    Title string `json:"title"`
}

type BrowserRecorder struct {
    mu        sync.Mutex
    recording bool
    startTime time.Time
    startURL  string
    steps     []RecordedStep
    flowDir   string // ~/.maclaw/browser_flows/
    session   func() (*Session, error)
}

func NewBrowserRecorder(flowDir string, sessionFn func() (*Session, error)) *BrowserRecorder

// Start 开始录制（通过 CDP 事件监听）
func (r *BrowserRecorder) Start() error

// Stop 停止录制并保存 flow
func (r *BrowserRecorder) Stop(name, description string) (*RecordedFlow, error)

// ListFlows 列出所有已录制的 flow
func (r *BrowserRecorder) ListFlows() ([]RecordedFlow, error)

// LoadFlow 加载指定 flow
func (r *BrowserRecorder) LoadFlow(name string) (*RecordedFlow, error)
```

录制实现：通过 CDP 的 `Runtime.bindingCalled` 和 `Page.domContentEventFired` 等事件监听用户操作。对于点击事件，使用 `DOM.getNodeForLocation` 从坐标反推 CSS selector。

### FlowReplayer（browser/replayer.go）

```go
type FlowReplayer struct {
    supervisor *BrowserTaskSupervisor
    ocr        OCRProvider
    llmDecide  func(context string) (string, error) // LLM 决策函数
}

func NewFlowReplayer(supervisor *BrowserTaskSupervisor,
    ocr OCRProvider, llmDecide func(string) (string, error)) *FlowReplayer

// Replay 回放录制的 flow
// 将 RecordedFlow 转换为 TaskSpec，然后通过 BrowserTaskSupervisor.Execute 执行
// 在执行过程中，如果 selector 失败，使用 LLM 自适应调整
func (r *FlowReplayer) Replay(flow *RecordedFlow, overrides map[string]string) (*TaskState, error)
```

回放流程：
1. 将 `RecordedFlow.Steps` 转换为 `TaskSpec.Steps`
2. 将 `RecordedFlow.SuccessCriteria` 转换为 `TaskSpec.SuccessCriteria`
3. 调用 `BrowserTaskSupervisor.Execute(taskSpec)`
4. 执行过程中，RetryStrategy 的 `UnknownState` 分支会触发 LLM 自适应

### BackgroundLoopManager 扩展

在 `corelib/agent/background_loop_manager.go` 中新增：

```go
const SlotKindBrowser SlotKind = 4 // 新增

// 在 NewBackgroundLoopManager 的 slotLimits 中添加:
// SlotKindBrowser: 2,
```

### 工具注册

在 `corelib/browser/tools.go` 的 `RegisterTools` 中新增 7 个工具：

| 工具名 | 描述 | 必需参数 |
|-------|------|---------|
| `browser_task_run` | 执行浏览器任务 | steps (JSON), success_criteria (JSON) |
| `browser_task_status` | 查询任务状态 | task_id |
| `browser_task_verify` | 验证当前页面 | criteria (JSON) |
| `browser_record_start` | 开始录制 | (无) |
| `browser_record_stop` | 停止录制并保存 | name |
| `browser_task_replay` | 回放录制的 flow | name |
| `browser_ocr` | OCR 识别当前页面 | (无) |

### ocr_server.py

部署到 `~/.maclaw/ocr/ocr_server.py`，内容由 Go 代码在首次安装时写入：

```python
#!/usr/bin/env python3
"""RapidOCR sidecar server - stdin/stdout JSON protocol."""
import sys, json, base64, signal

def main():
    from rapidocr_onnxruntime import RapidOCR
    engine = RapidOCR()
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            print(json.dumps({"error": "invalid json"}), flush=True)
            continue
        method = req.get("method", "")
        if method == "ocr":
            try:
                img_bytes = base64.b64decode(req["image_base64"])
                result, _ = engine(img_bytes)
                items = []
                if result:
                    for box, text, score in result:
                        x0, y0 = int(box[0][0]), int(box[0][1])
                        x1, y1 = int(box[2][0]), int(box[2][1])
                        items.append({
                            "text": text,
                            "confidence": round(float(score), 4),
                            "bbox": [x0, y0, x1 - x0, y1 - y0]
                        })
                print(json.dumps({"results": items}), flush=True)
            except Exception as e:
                print(json.dumps({"error": str(e)}), flush=True)
        elif method == "ping":
            print(json.dumps({"status": "ok"}), flush=True)
        else:
            print(json.dumps({"error": f"unknown method: {method}"}), flush=True)

if __name__ == "__main__":
    main()
```

## Data Models

### TaskSpec

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| ID | string | auto-generated | 任务唯一标识 |
| Description | string | "" | 任务描述 |
| Steps | []StepSpec | required | 操作步骤序列 |
| SuccessCriteria | []CriterionSpec | [] | 成功标准列表 |
| MaxRetries | int | 3 | 任务级最大重试次数 |
| StepTimeout | time.Duration | 30s | 单步超时 |

### StepSpec

| 字段 | 类型 | 说明 |
|------|------|------|
| Action | string | navigate / click / type / wait / eval / scroll / select |
| Params | map[string]string | url, selector, text, expression, value, delta_y 等 |
| Verify | *CriterionSpec | 可选的步骤级验证 |
| Timeout | time.Duration | 覆盖全局 StepTimeout |

### CriterionSpec

| 字段 | 类型 | 说明 |
|------|------|------|
| Type | string | dom_exists / dom_text / url_contains / url_matches / ocr_contains |
| Selector | string | CSS selector（dom_* 类型使用） |
| Pattern | string | 匹配模式 |

### RecordedFlow

| 字段 | 类型 | 说明 |
|------|------|------|
| Name | string | flow 名称（文件名） |
| Description | string | 描述 |
| RecordedAt | time.Time | 录制时间 |
| StartURL | string | 起始 URL |
| Steps | []RecordedStep | 录制的步骤 |
| SuccessCriteria | []CriterionSpec | 可选的成功标准 |

### OCR 安装状态

| 状态 | 说明 |
|------|------|
| not_installed | 未安装，首次调用时触发安装 |
| installing | 正在安装（pip install 中） |
| installed | 已安装，sidecar 未运行 |
| running | sidecar 正在运行 |
| error | 安装或启动失败 |

## Error Handling

### OCR 安装失败

1. python3 不存在 → 记录日志，fallback 到 LLM Vision
2. pip install 失败（网络问题）→ 记录日志，提示用户手动安装，fallback 到 LLM Vision
3. sidecar 启动失败 → 记录日志，fallback 到 LLM Vision
4. sidecar 运行中崩溃 → 自动重启（最多 3 次），超过后 fallback

### 浏览器连接丢失

1. CDP 连接断开 → 尝试重新连接（最多 3 次）
2. 重连失败 → 标记任务失败，通知 Agent

### 录制异常

1. 录制过程中浏览器关闭 → 自动停止录制，保存已录制的步骤
2. CDP 事件丢失 → 录制的步骤可能不完整，回放时由 LLM 补充

## Testing Strategy

### 单元测试

1. TaskVerifier: 测试每种 CriterionSpec 类型的验证逻辑（mock browser.Session）
2. RetryStrategy: 测试每种 FailureType 的重试决策（参数调整、LLM 触发条件）
3. RapidOCRSidecar: 测试安装检测、sidecar 启动/停止、JSON 协议通信（mock exec.Cmd）
4. CompositeOCRProvider: 测试 fallback 链（第一个失败时切换到下一个）
5. BrowserRecorder: 测试 RecordedFlow 的序列化/反序列化、flow 文件管理
6. FlowReplayer: 测试 RecordedFlow → TaskSpec 转换逻辑

### 集成测试

1. 完整任务执行流程: 创建 TaskSpec → Execute → 逐步验证 → 成功/失败
2. 重试流程: 模拟 ElementNotFound → 验证重试策略调整 → 最终成功
3. OCR 集成: 截图 → RapidOCR 识别 → 验证结果格式
4. 录制回放: 录制操作 → 保存 flow → 加载 → 回放 → 验证结果

### 端到端测试

1. 登录任务: Navigate → Type username → Type password → Click submit → Verify dashboard
2. 表单填写: Navigate → Fill form fields → Submit → Verify success message
3. 录制回放: 手动操作 → 录制 → 回放 → 验证一致性
