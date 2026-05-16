# Requirements Document

## Introduction

本特性为 maclaw 增加原生 GUI 软件（非 Web）的测试录制与重放能力，作为差异化功能。同时修复现有多显示器截图缺陷（各平台仅截取主显示器），该缺陷是 GUI 测试录制的前置依赖。

核心方案采用混合定位策略：Accessibility API（语义化控件识别）为主、截图/OCR 图像匹配为辅、坐标为最终降级手段。录制时同时记录三种信息，重放时按优先级逐级尝试。流程保存为 JSON，支持参数替换回放，并可通过 LLM 辅助适配 UI 变化。

## Glossary

- **GUI_Recorder**: 原生 GUI 应用操作录制器，负责捕获用户在原生桌面应用上的操作并生成 GUIRecordedStep 序列
- **GUI_Replayer**: 原生 GUI 应用操作重放器，负责将 GUIRecordedFlow 转换为可执行步骤并依次执行
- **GUI_TaskSupervisor**: GUI 任务执行监督器，管理 GUI 任务的执行、验证、重试和暂停/恢复
- **Accessibility_Bridge**: 跨平台无障碍 API 桥接层，封装 Windows UI Automation、macOS Accessibility API、Linux AT-SPI 的统一接口
- **Screenshot_Engine**: 截图引擎，负责跨平台屏幕截图，支持多显示器、指定屏幕、指定窗口截图
- **Image_Matcher**: 图像匹配器，基于截图和 OCR 结果在屏幕上定位 UI 元素
- **GUIRecordedStep**: 单个 GUI 操作步骤的数据结构，包含控件标识、坐标、截图快照三种定位信息
- **GUIRecordedFlow**: 完整的 GUI 操作流程，包含步骤序列、元数据和成功标准
- **OCR_Provider**: OCR 识别提供者接口，复用现有 RapidOCRSidecar 实现
- **Tool_Registry**: maclaw 的工具注册中心，GUI 测试工具通过此机制注册并暴露给 LLM 调用

## Requirements

### Requirement 1: 多显示器全屏截图

**User Story:** As a maclaw 用户, I want 截图功能能够捕获所有显示器的内容, so that 在多显示器环境下不会遗漏目标窗口所在屏幕的信息。

#### Acceptance Criteria

1. WHEN 系统连接了多个显示器, THE Screenshot_Engine SHALL 将所有显示器的画面拼接为一张完整截图并输出 base64 编码的 PNG 数据
2. THE Screenshot_Engine SHALL 在 Windows、macOS、Linux 三个平台上均支持多显示器拼接截图
3. WHEN 用户指定 screen_index 参数, THE Screenshot_Engine SHALL 仅截取对应索引的单个显示器画面
4. WHEN 用户指定的 screen_index 超出实际显示器数量, THE Screenshot_Engine SHALL 返回包含实际显示器数量的错误信息
5. IF 多显示器拼接截图失败, THEN THE Screenshot_Engine SHALL 降级为仅截取主显示器并在返回结果中附带降级说明

### Requirement 2: 指定窗口截图修复

**User Story:** As a maclaw 用户, I want 按窗口标题截图功能在多显示器环境下正常工作, so that 无论目标窗口在哪个屏幕上都能正确截取。

#### Acceptance Criteria

1. WHEN 目标窗口位于非主显示器上, THE Screenshot_Engine SHALL 正确截取该窗口的完整画面
2. THE Screenshot_Engine SHALL 在 Windows、macOS、Linux 三个平台上均支持跨显示器的窗口截图
3. WHEN 目标窗口跨越多个显示器边界, THE Screenshot_Engine SHALL 截取窗口的完整画面而非仅截取主显示器部分

### Requirement 3: Accessibility API 桥接层

**User Story:** As a maclaw 开发者, I want 一个统一的跨平台无障碍 API 接口, so that GUI 录制和重放可以通过语义化方式识别和操作原生控件。

#### Acceptance Criteria

1. THE Accessibility_Bridge SHALL 提供统一的 Go 接口用于枚举窗口内的 UI 控件树
2. THE Accessibility_Bridge SHALL 为每个控件返回角色（role）、名称（name）、值（value）和边界矩形（bounds）属性
3. WHEN 运行在 Windows 平台上, THE Accessibility_Bridge SHALL 通过 UI Automation COM 接口访问控件信息
4. WHEN 运行在 macOS 平台上, THE Accessibility_Bridge SHALL 通过 Accessibility API（AXUIElement）访问控件信息
5. WHEN 运行在 Linux 平台上, THE Accessibility_Bridge SHALL 通过 AT-SPI D-Bus 接口访问控件信息
6. WHEN 目标应用未暴露无障碍信息, THE Accessibility_Bridge SHALL 返回空控件树而非错误，以便调用方降级到其他定位策略
7. THE Accessibility_Bridge SHALL 支持通过控件角色和名称的组合进行控件查找
8. THE Accessibility_Bridge SHALL 支持对找到的控件执行点击、输入文本、获取值等基本操作

### Requirement 4: GUI 操作录制

**User Story:** As a maclaw 用户, I want 通过自然语言指令录制对原生 GUI 应用的操作, so that 可以将手动测试流程自动化。

#### Acceptance Criteria

1. WHEN 用户发出"开始录制 GUI"指令, THE GUI_Recorder SHALL 进入录制模式并确认录制已开始
2. WHEN 录制模式下执行 GUI 操作, THE GUI_Recorder SHALL 同时记录三种定位信息：Accessibility 控件标识、屏幕坐标、操作前后的截图快照
3. THE GUI_Recorder SHALL 为每个 GUIRecordedStep 记录操作类型（click、type、scroll、drag、keypress）、时间戳和目标窗口标题
4. WHEN 用户发出"停止录制"指令并提供流程名称, THE GUI_Recorder SHALL 将 GUIRecordedFlow 序列化为 JSON 文件保存到 ~/.maclaw/gui_flows/ 目录
5. THE GUI_Recorder SHALL 在录制每一步时通过 Accessibility_Bridge 尝试获取控件标识，获取失败时将控件标识字段留空而非中断录制
6. THE GUI_Recorder SHALL 在录制每一步时通过 Screenshot_Engine 截取目标窗口快照，截图失败时将快照字段留空而非中断录制

### Requirement 5: GUI 操作重放

**User Story:** As a maclaw 用户, I want 重放之前录制的 GUI 操作流程, so that 可以自动化执行重复性测试。

#### Acceptance Criteria

1. WHEN 用户发出"重放 <flow_name>"指令, THE GUI_Replayer SHALL 加载对应的 GUIRecordedFlow 并开始执行
2. THE GUI_Replayer SHALL 按以下优先级尝试定位目标控件：首先通过 Accessibility 控件标识，其次通过 Image_Matcher 图像匹配，最后降级到屏幕坐标
3. WHEN Accessibility 控件标识定位成功, THE GUI_Replayer SHALL 使用 Accessibility_Bridge 执行操作
4. WHEN Accessibility 控件标识定位失败且存在截图快照, THE GUI_Replayer SHALL 使用 Image_Matcher 在当前屏幕截图中查找匹配区域并在匹配位置执行操作
5. WHEN 图像匹配也失败, THE GUI_Replayer SHALL 降级到录制时记录的屏幕坐标执行操作
6. WHEN 用户提供 overrides 参数, THE GUI_Replayer SHALL 在重放时将对应的占位符值替换为 overrides 中指定的值
7. THE GUI_Replayer SHALL 在每步执行后等待 UI 稳定（无动画、无加载指示器）再执行下一步

### Requirement 6: GUI 任务监督与重试

**User Story:** As a maclaw 用户, I want GUI 重放过程具备自动重试和错误恢复能力, so that 轻微的 UI 变化不会导致整个流程失败。

#### Acceptance Criteria

1. THE GUI_TaskSupervisor SHALL 为每个步骤提供最多 3 次重试机会
2. WHEN 步骤执行失败且重试次数未耗尽, THE GUI_TaskSupervisor SHALL 根据失败类型选择重试策略：元素未找到时增加等待时间，超时时延长超时限制
3. WHEN 步骤重试 2 次仍失败, THE GUI_TaskSupervisor SHALL 将当前屏幕截图和 OCR 文本作为上下文发送给 LLM 请求适配建议
4. THE GUI_TaskSupervisor SHALL 支持暂停、恢复和取消正在执行的 GUI 任务
5. THE GUI_TaskSupervisor SHALL 在每步执行后记录检查点（包含截图和控件状态），以便失败时提供诊断信息

### Requirement 7: 图像匹配定位

**User Story:** As a maclaw 开发者, I want 一个基于截图和 OCR 的 UI 元素定位器, so that 在 Accessibility API 不可用时仍能定位目标控件。

#### Acceptance Criteria

1. WHEN 提供一个参考截图片段和当前全屏截图, THE Image_Matcher SHALL 在全屏截图中查找与参考片段最相似的区域并返回匹配位置和置信度
2. WHEN 提供目标文本, THE Image_Matcher SHALL 通过 OCR_Provider 识别屏幕文字并返回包含目标文本的区域坐标
3. WHEN 图像匹配置信度低于 0.6, THE Image_Matcher SHALL 报告匹配失败而非返回低置信度结果
4. THE Image_Matcher SHALL 支持在指定窗口范围内进行局部匹配以提高匹配速度和准确度

### Requirement 8: GUIRecordedFlow JSON 序列化

**User Story:** As a maclaw 用户, I want 录制的 GUI 流程以 JSON 格式保存, so that 可以版本控制、手动编辑和跨机器共享。

#### Acceptance Criteria

1. THE GUI_Recorder SHALL 将 GUIRecordedFlow 序列化为格式化的 JSON 文件（带缩进）
2. THE GUI_Recorder SHALL 在 JSON 中为每个步骤的截图快照使用外部文件引用（而非内联 base64），快照图片保存在同名子目录下
3. FOR ALL 有效的 GUIRecordedFlow 对象, 序列化为 JSON 再反序列化 SHALL 产生与原始对象等价的结果（round-trip 属性）
4. THE GUI_Replayer SHALL 能够加载并执行由 GUI_Recorder 保存的任意合法 JSON 流程文件

### Requirement 9: GUI 测试工具注册

**User Story:** As a maclaw 用户, I want 通过自然语言与 GUI 测试功能交互, so that 使用体验与现有浏览器录制一致。

#### Acceptance Criteria

1. THE GUI_Recorder SHALL 通过 Tool_Registry 注册 gui_record_start、gui_record_stop、gui_replay、gui_list_flows 工具
2. THE GUI_Recorder SHALL 为每个工具提供中英文描述和相关标签（gui、test、automation、桌面、录制）
3. WHEN LLM 收到与 GUI 测试相关的自然语言指令, THE Tool_Registry SHALL 通过标签匹配将 GUI 测试工具包含在候选工具列表中
4. THE GUI_Recorder SHALL 注册 gui_click、gui_type、gui_screenshot 等原子操作工具，供 LLM 在录制模式下调用

### Requirement 10: 跨平台输入模拟

**User Story:** As a maclaw 开发者, I want 一个跨平台的输入事件模拟层, so that GUI 重放可以在目标应用中执行鼠标点击、键盘输入等操作。

#### Acceptance Criteria

1. THE GUI_Replayer SHALL 支持模拟鼠标点击（左键、右键、双击）到指定屏幕坐标
2. THE GUI_Replayer SHALL 支持模拟键盘输入（逐字符输入和组合键）
3. THE GUI_Replayer SHALL 支持模拟鼠标滚轮滚动
4. THE GUI_Replayer SHALL 支持模拟鼠标拖拽操作（按下、移动、释放）
5. WHEN 运行在 Windows 平台上, THE GUI_Replayer SHALL 通过 SendInput API 模拟输入事件
6. WHEN 运行在 macOS 平台上, THE GUI_Replayer SHALL 通过 CGEvent API 模拟输入事件
7. WHEN 运行在 Linux 平台上, THE GUI_Replayer SHALL 通过 XTest 扩展或 libinput 模拟输入事件
