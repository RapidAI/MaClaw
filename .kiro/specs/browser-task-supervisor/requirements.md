# Requirements Document

## Introduction

MaClaw 已具备完整的浏览器自动化工具链（CDP 连接、导航、点击、输入、截图、等待元素、JS 执行），但缺少一个"监管层"来保障浏览器任务的可靠完成。当 Agent 使用 browser 工具执行复杂任务（登录、表单填写、功能操作）时，存在以下问题：

1. **任务完成无标准验证**：Agent 执行完一系列 browser 操作后，无法可靠判断任务是否真正成功（页面是否跳转到预期位置、目标元素是否出现）。
2. **失败无自动恢复**：操作失败（元素未找到、超时、页面变化）后，Agent 缺乏系统性的重试和策略调整机制。
3. **无页面感知能力**：当 DOM 无法提供文本信息（Canvas、验证码、iframe 跨域）时，Agent 无法"看到"页面内容。
4. **重复任务无法复用**：每次执行相同的浏览器任务都需要 Agent 从头规划，无法录制和回放操作序列。

本特性新增 BrowserTaskSupervisor 监管层，复用现有 BackgroundLoopManager 的槽位管理和 StallDetector 的停滞检测模式，为浏览器任务提供长程监管、成功验证、智能重试和录制回放能力。同时集成 RapidOCR 作为本地 OCR 引擎，使 Agent 在无多模态模型时也能"读取"页面视觉内容。

## Glossary

- **Browser_Task_Supervisor**: 浏览器任务监管器，管理浏览器任务的执行、验证、重试和录制回放
- **Task_Verifier**: 任务验证器，通过 DOM 检查、URL 匹配、OCR 文本匹配等方式验证任务成功标准
- **Retry_Strategy**: 重试策略引擎，根据失败原因（元素未找到、超时、页面变化等）决定重试方式和参数调整
- **OCR_Provider**: OCR 提供者接口，抽象本地 OCR（RapidOCR）和 LLM Vision 两种实现
- **RapidOCR_Sidecar**: RapidOCR Python 进程，作为 sidecar 通过 stdin/stdout JSON 协议与 Go 主进程通信
- **Browser_Recorder**: 浏览器操作录制器，通过 CDP 事件监听记录用户手动操作序列
- **Recorded_Flow**: 录制的操作流程，JSON 格式存储在 `~/.maclaw/browser_flows/` 目录
- **Flow_Replayer**: 操作流程回放器，基于录制的骨架由 LLM 灵活调整执行
- **Success_Criteria**: 成功标准，Agent 用自然语言描述，Supervisor 自动翻译为可执行的验证规则
- **Step_Checkpoint**: 步骤检查点，每个操作步骤执行后的状态快照（截图 + DOM 状态 + URL）
- **SlotKindBrowser**: BackgroundLoopManager 中的浏览器任务槽位类型

## Requirements

### Requirement 1: 浏览器任务监管核心

**User Story:** 作为 MaClaw Agent，我需要一个监管层来管理浏览器任务的执行生命周期，确保任务可靠完成。

#### Acceptance Criteria

1. THE Browser_Task_Supervisor SHALL 通过 BackgroundLoopManager 的 SlotKindBrowser 槽位管理浏览器任务并发（默认并发 2）
2. WHEN Agent 提交浏览器任务时，THE Browser_Task_Supervisor SHALL 创建后台循环，逐步执行操作并在每步后进行验证
3. THE Browser_Task_Supervisor SHALL 在每个操作步骤执行后创建 Step_Checkpoint（截图 + 当前 URL + 页面标题 + 关键 DOM 状态）
4. WHEN 单个操作步骤超过 Step_Timeout（默认 30 秒）未完成，THE Browser_Task_Supervisor SHALL 标记该步骤为超时并触发重试决策
5. THE Browser_Task_Supervisor SHALL 通过 StatusEvent 通道向 GUI/TUI 推送任务进度（当前步骤、总步骤数、状态）
6. WHEN 浏览器任务完成（成功或最终失败），THE Browser_Task_Supervisor SHALL 释放 SlotKindBrowser 槽位并通知 BackgroundLoopManager

### Requirement 2: 成功标准验证

**User Story:** 作为 MaClaw Agent，我需要定义和验证浏览器任务的成功标准，确保任务真正完成而非仅执行了操作。

#### Acceptance Criteria

1. THE Agent SHALL 以自然语言描述成功标准（如"页面跳转到仪表盘"、"出现欢迎消息"），Task_Verifier 自动翻译为可执行的验证规则
2. THE Task_Verifier SHALL 支持以下验证方式的组合：
   - DOM 验证：通过 `browser_wait` + `browser_get_text` 检查元素存在和文本内容
   - URL 验证：通过 `browser_info` 检查当前 URL 是否匹配预期模式
   - OCR 验证：通过截图 + OCR 识别页面文本并匹配关键词
3. WHEN 所有验证规则通过，THE Task_Verifier SHALL 返回 `verified` 状态
4. WHEN 任何验证规则失败，THE Task_Verifier SHALL 返回失败详情（哪个规则失败、实际值 vs 预期值）
5. THE Task_Verifier SHALL 在验证前等待页面稳定（无新的网络请求或 DOM 变化持续 1 秒），避免在页面加载中验证

### Requirement 3: 智能重试机制

**User Story:** 作为 MaClaw Agent，我需要在浏览器操作失败时自动重试，并根据失败原因调整策略。

#### Acceptance Criteria

1. THE Retry_Strategy SHALL 根据失败类型选择不同的重试策略：
   - ElementNotFound → 增加等待时间（翻倍，最大 60 秒）/ 尝试替代 selector
   - Timeout → 增加 step timeout / 检查页面加载状态
   - PageChanged（URL 意外变化）→ 截图 + OCR/DOM 分析当前状态，从断点继续
   - UnknownState → Screenshot + OCR → 将页面状态描述发给 LLM 决策下一步
2. THE Retry_Strategy SHALL 对每个步骤设置最大重试次数（默认 3 次），超过后标记步骤失败
3. THE Retry_Strategy SHALL 对整个任务设置最大重试次数（默认 3 次），超过后标记任务最终失败
4. WHEN 重试策略需要 LLM 参与决策（UnknownState），THE Retry_Strategy SHALL 将当前页面的 OCR 文本和 DOM 摘要作为上下文发给 LLM
5. THE Retry_Strategy 的重试次数和超时参数 SHALL 支持运行时动态调整

### Requirement 4: OCR 能力集成

**User Story:** 作为 MaClaw 系统，我需要本地 OCR 能力来识别页面截图中的文本，使 Agent 在无多模态模型时也能"看到"页面内容。

#### Acceptance Criteria

1. THE OCR_Provider SHALL 定义统一接口，返回识别结果列表（文本、置信度、边界框坐标）
2. THE RapidOCR_Sidecar SHALL 作为 Python 进程运行，安装目录为 `~/.maclaw/ocr/`，通过 stdin/stdout JSON 协议与 Go 主进程通信
3. WHEN 首次需要 OCR 且 RapidOCR 未安装，THE OCR_Provider SHALL 自动执行安装流程：
   - 通过 `pyenv.Detect()` 检测可用的 Python（优先私有安装 `~/.maclaw/python/`，其次系统 Python）
   - 执行 `python -m pip install --target ~/.maclaw/ocr/lib/ rapidocr-onnxruntime`（安装到私有目录，方便管理和卸载）
   - 将 `ocr_server.py` 写入 `~/.maclaw/ocr/`
   - 启动 sidecar 时设置 `PYTHONPATH=~/.maclaw/ocr/lib/` 使其能找到私有安装的包
   - 通过 StatusEvent 通知 UI "正在安装 OCR 引擎（首次使用，约 30 秒）..."
4. IF Python 不可用（pyenv.Detect 返回 Available=false），THEN THE OCR_Provider SHALL fallback 到 LLM Vision（如果当前模型支持多模态），再 fallback 到纯 DOM 文本提取
5. THE RapidOCR_Sidecar SHALL 在空闲 5 分钟后自动退出，下次需要时重新启动
6. THE OCR_Provider SHALL 支持 LLM Vision 作为备选实现——将截图 base64 发给 LLM 进行视觉理解
7. THE OCR 结果 SHALL 以结构化文本形式（坐标 + 文本）拼入 LLM prompt，使纯文本模型也能理解页面内容

### Requirement 5: 操作录制

**User Story:** 作为用户，我需要录制手动浏览器操作序列，以便后续自动回放，降低重复任务的定义成本。

#### Acceptance Criteria

1. THE Browser_Recorder SHALL 通过 CDP 事件监听记录用户的浏览器操作，包括：
   - 页面导航（URL 变化）
   - 鼠标点击（坐标 + 推断的 CSS selector）
   - 键盘输入（目标元素 + 输入文本）
   - 页面等待（元素出现/消失）
2. THE Browser_Recorder SHALL 将录制结果保存为 Recorded_Flow JSON 文件，存储在 `~/.maclaw/browser_flows/<name>.json`
3. THE Recorded_Flow SHALL 包含操作步骤序列和可选的成功标准定义
4. THE Browser_Recorder SHALL 从鼠标点击坐标反向推断 CSS selector（通过 CDP 的 DOM.getNodeForLocation），同时保留原始坐标作为 fallback
5. WHEN 录制开始时，THE Browser_Recorder SHALL 通过 `browser_record_start` 工具启动；通过 `browser_record_stop` 工具停止并保存

### Requirement 6: 操作回放与 LLM 自适应

**User Story:** 作为 MaClaw Agent，我需要基于录制的操作骨架回放浏览器任务，并在页面变化时由 LLM 灵活调整。

#### Acceptance Criteria

1. THE Flow_Replayer SHALL 加载 Recorded_Flow 并逐步执行操作序列
2. WHEN 录制的 CSS selector 在当前页面无法匹配，THE Flow_Replayer SHALL：
   - 首先尝试使用录制的坐标 fallback
   - 如果坐标也失败，截图 + OCR/DOM 分析当前页面状态
   - 将页面状态和原始操作意图发给 LLM，由 LLM 生成替代操作
3. THE Flow_Replayer SHALL 在每步执行后与录制的预期状态对比（URL、关键元素），检测偏离
4. WHEN 检测到偏离（当前状态与录制时不同），THE Flow_Replayer SHALL 将偏离信息发给 LLM，由 LLM 决定是跳过、调整还是中止
5. THE Flow_Replayer SHALL 通过 `browser_task_replay` 工具触发，支持指定 flow 名称和可选的参数覆盖

### Requirement 7: GUI/TUI 集成

**User Story:** 作为用户，我需要在 GUI 和 TUI 中查看浏览器任务的执行状态、进度和结果。

#### Acceptance Criteria

1. THE GUI SHALL 在浏览器任务面板中显示：当前步骤、总步骤数、任务状态、最近截图预览、重试次数
2. THE TUI SHALL 提供 `browser` 命令组，包含 `record`、`replay`、`status`、`list-flows` 子命令
3. WHEN 浏览器任务失败，THE GUI/TUI SHALL 显示失败原因、最后一次截图和建议的下一步操作
4. THE GUI/TUI SHALL 通过 StatusEvent 通道接收实时进度更新，无需轮询

### Requirement 8: 工具注册

**User Story:** 作为 MaClaw Agent，我需要通过标准的 tool 接口使用浏览器任务监管能力。

#### Acceptance Criteria

1. THE Browser_Task_Supervisor SHALL 注册以下工具到 tool.Registry：
   - `browser_task_run`: 执行浏览器任务（接受步骤序列 + 成功标准）
   - `browser_task_status`: 查询浏览器任务的当前状态和进度
   - `browser_task_verify`: 对当前页面执行成功标准验证
   - `browser_record_start`: 开始录制浏览器操作
   - `browser_record_stop`: 停止录制并保存 flow
   - `browser_task_replay`: 回放录制的 flow
   - `browser_ocr`: 对当前页面截图执行 OCR 识别
2. ALL 新增工具 SHALL 使用 `tool.CategoryBuiltin` 类别和 `browser` tag
3. THE `browser_ocr` 工具 SHALL 在首次调用时自动触发 RapidOCR 安装（如果未安装）
