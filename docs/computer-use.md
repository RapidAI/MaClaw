# Computer Use

MacLaw Computer Use 让 Agent 操作本机桌面 GUI。感知路径按**当前聊天模型是否支持视觉**分流：

- **模型支持视觉 / 图片**：`computer_observe` 把截图交给 LLM 看，模型用 `computer_click x,y` 点截图像素。不跑 OmniParser / OCR / Caption。
- **模型不支持视觉**：保持文本优先——本地 OmniParser YOLO + OCR + 可选 a11y 生成 `eN` SoM，截图不发给聊天模型。OCR/无障碍仍空白的框，若在 **设置 → 大语言模型 → 模型分配** 配置了 Caption 模型，则裁剪后交给该视觉模型补短标签。

## 架构

```
支持视觉的 LLM:
  截屏 →（可选缩小）→ 作为图片发给 LLM
  LLM 输出 computer_click(x,y) → 映射回屏幕坐标 → InputSimulator

文本模型:
  截屏（仅本地）
    → OmniParser YOLO 检测可交互区域
    → OCR 贴文字标签 + 全文摘要
    → 前台窗口 Accessibility 树（可传 window= 指定应用）
    → 可选 Caption 模型：只给仍无标签的框补 name/type（聊天模型看不到这些裁剪图）
    → 纯文本 SoM（e0..eN）交给 LLM
  LLM 输出 computer_click(ref=e3) → Session 解析中心坐标（含屏幕 origin/DPI）→ 优先 UIA/AX 语义操作，失败再 InputSimulator
```

默认 `computer_observe` **裁剪前台窗口**（可加 `window=` 指定标题；四周约 8px 边距）。传入 `screen_index=N` 才截整块显示器，`screen_index=-1` 才拼接全部显示器。点击坐标按截图 origin/DPI 映射到虚拟桌面。Windows 截屏走 GDI `BitBlt`（失败再回退 PowerShell）。每个聊天标签页有独立 CU Session；操作员 Pause/Stop/Reset 作用于全部标签。动作后约 80ms settle，Windows/macOS 再短时 `WaitForIdle`；若前台窗口族与 observe 时的 `crop=` 对不上，则作废 refs 并要求重新 observe。

## 工具

| 工具 | 作用 |
|------|------|
| `computer_observe` | 默认裁剪前台窗口；视觉模型附加带 SoM 框的截图；文本模型纯文本 eN（无 base64） |
| `computer_click` | 视觉：`x,y` 截图像素或 `ref=eN`；文本：`ref=eN`（默认禁止裸像素） |
| `computer_type` | 输入；可选先 `ref` 聚焦 |
| `computer_key` | 快捷键 |
| `computer_scroll` | 滚动 |
| `computer_select` | 选择列表/标签/树节点（UIA SelectionItem，失败再点击） |
| `computer_scroll_into_view` | 把 `ref` 滚进可视区 |
| `computer_drag` | 从 `from_ref` 拖到 `to_ref` |
| `computer_wait` | 等待 / UI 稳定 |
| `computer_focus` | 按窗口标题子串前置前台 |
| `computer_find` | 按关键词查找屏幕文本/元素（含无元素覆盖的 OCR 文本），命中即分配可点击 ref |
| `computer_done` | 结束并汇报 |
| `computer_playbook` | 打印操作规则 |

旧版 `gui_*`（录制回放等）仍然可用；**CU 激活时**会暂时从工具列表中拿掉 `gui_click` / `gui_type` / `gui_screenshot`，避免模型瞎点坐标。

## 自动激活

激活判断走**统一意图分类器（UIC）语义识别**（embedding 快速通道，不走 LLM）：用户话术被分类为 `computer_use` 意图（如「打开word程序写简历」「点击窗口上的确定按钮」「看看屏幕上显示什么」）时即激活；`@computer` / `computer use` 为显式触发语法，始终生效；本进程已执行过 `computer_*` 时保持激活。分类器不可用（嵌入模型未就绪）时门保持关闭，仅显式触发可用。

满足以上任一条件时：

1. 系统提示词注入 Computer Use playbook  
2. 路由强制保留 `computer_*` 工具  
3. 临时降权裸坐标 `gui_*` 工具  

| 配置 | 含义 |
|------|------|
| `computer_use_enabled` | 总开关（默认 true） |
| `screen_parsing_enabled` | OmniParser YOLO（默认 true；关则 observe 仍可用 a11y/OCR） |
| Caption 模型（LLM 设置 → 模型分配） | 可选视觉模型；仅当聊天模型不支持视觉、且 observe 仍有未标注框时调用。未配置则保持 OCR/a11y/启发式标签 |

Wails：`GetComputerUseEnabled` / `SetComputerUseEnabled` / `GetComputerUseStatus`，以及既有的 Screen Parsing 开关。

设置入口：**设置 → 嵌入模型（Embedding）** 面板顶部的 Computer Use 开关；同页可管理 OmniParser 权重下载。

操作员预览：Agent 执行 `computer_observe` / 动作时弹出 **Computer Use** 面板（元素分布点图 + eN 列表 + 最近动作）。

- 📌 **固定**：保持显示，并轮询 `GetComputerUseStatus`
- ▥ **靠右停靠**：右侧全高面板（适合长任务监督）
- **Pause / Resume / Stop / Reset**（操作员控制）
  - Pause：阻止 click/type/key/scroll/focus；仍允许 observe
  - Resume：取消 Pause
  - Stop：硬停止 + 尽量取消当前助手回合（`CancelAIAssistantSession`）
  - Reset：清除 stop/pause，允许新任务
- 事件：`computer-use:observe` / `computer-use:action` / `computer-use:control`
- 偏好：`localStorage` `maclaw.computer_use.operator.pinned` / `.docked`

API：`ComputerUsePause` / `ComputerUseResume` / `ComputerUseStop` / `ComputerUseReset`

主聊天输入区上方会出现 **Computer Use 快捷条**（会话 active/paused 时；Stop 或空闲后自动隐藏）：Pause / Resume / Stop CU（paused 时另有 Reset）。  
新桌面任务激活（`@computer` 或高置信分类）时后端自动解除上次的 Stop，无需手动复位；sticky 续聊不会解除，确保 Stop 能阻断当前回合。
点输入栏 **停止生成** 时，若桌面操控进行中会一并 `ComputerUseStop`，避免点停文字后仍在点桌面。

**系统托盘** *Computer Use* 子菜单（Windows + macOS）：状态行 + Pause / Resume / Stop / Reset（随会话激活与 `computer-use:control` 刷新）。

## 对模型的要求

- **视觉模型**：先 `computer_observe`（截图上会画 a11y SoM 框），可用 `computer_click x,y` 或 `ref=eN`，动作后再 observe。
- **文本模型**：必须先 `computer_observe`，再用 `ref`，动作后再次 observe。observe 默认枚举**前台窗口**的 a11y 树；传 `window`（应用标题子串）可指定其它应用。元素类型会标成 button/edit/icon 等，而不是一律 interactable。
- 命中 Office / 资源管理器 / 浏览器 / IM 窗口时，observe 会给出 `adapter=` 提示：文档走 office_read，网页走 browser_*，IM 先搜搜索框。
- 定位指定的人/文字：视觉模型直接看图；文本模型先 `computer_find query=...`。长列表用 `computer_scroll` + 重新 observe，或优先用应用内搜索框。
- 文本模型不要臆造像素坐标。

## 权重

OmniParser 权重路径由 `yoloModelPath()` 解析（见 `gui/app_yolo_model.go`）。未找到权重时 observe 仍可用 a11y/OCR。

## 性能（热路径）

| 层 | 以前 | 现在 |
|----|------|------|
| Windows 输入 | 每次 PowerShell + Add-Type | **user32 原生** |
| Windows 截屏 | PowerShell `CopyFromScreen` | **GDI BitBlt**（`NativeScreenshotRect`；失败回退 PS） |
| Windows a11y | 每次启动 PS 加载 UIAutomation | **优先 C# sidecar**（安装包/`dist` 预置 `maclaw-uia-sidecar.exe`；缺失时用 `csc` 自动编译）；失败回退常驻 PowerShell。UI 显示 `a11y=csharp` / `powershell` |
| macOS 输入 | 每次 `python3 -c` | **常驻 python+Quartz sidecar**（失败回退 one-shot） |
| 窗口前置 | 无 | `computer_focus` / `accessibility.FocusWindow` |
| 动作后 | 无 | 自动 settle（约 80ms；Windows/macOS 另做短时 UI idle 观察） |

## 启动预热与自检

| 时机 | 行为 |
|------|------|
| GUI 启动 | 后台 `backgroundWarmupComputerUse`：预启 Windows UIA、探测输入、**YOLO 权重入内存**、**原生 OCR 模型加载**（仅当已安装）、探测桌面权限；发 `computer-use:warmup` |
| 设置页 | **运行自检** + **打开隐私设置**；`ComputerUseSelfCheck` 含 UIA/YOLO/OCR/权限/readiness |
| 聊天区 | **Computer Use 准备** 横幅：缺权重 / 权限 / **最近 observe 失败**；**按 issue 关闭**（`dismissed_ids`）；可 Smoke / 自检 |
| 观察失败 | `computer_observe` 失败返回 **Guidance** 文案；事件 `computer-use:error`；`GetComputerUseLastError` |
| 冒烟 | 自检内嵌 `smoke`（截屏 + a11y）；`ComputerUseSmokeCheck` 可选 YOLO |
| 耗时 | observe / smoke 事件含 `timing_ms`（screenshot/yolo/a11y/ocr/caption/commit）与 `total_ms`；`GetComputerUseLastObserveMetrics` / status.`last_observe` |
| 操作员面板 | last error + 耗时 + **历史 n/avg/min/max + sparkline**；**Smoke / E2E / E2E+ / 导出** |
| 设置页 | 自检 / 隐私设置 / **导出诊断** / **E2E 冒烟** / **E2E 交互** |
| 诊断导出 | 手动导出完整包（含 SelfCheckUIA）；**E2E 失败静默导出**用 light 路径（不重编译 sidecar） |
| 历史 CSV | `ExportComputerUseObserveHistoryCSV` → 数据行 + **`# summary` 注释** + `stage=SUMMARY` 尾行 |
| E2E 冒烟 | `ComputerUseE2ESmoke`；失败时 **静默写出 diagnostics JSON** 并填 `diagnostics_path` |
| E2E 交互 | focus **轮询重试**（可复用已打开编辑器）；launch/focus 失败 → `soft_fail`+`skip_reason`（**不再静默 ok**）；token 未检出 → re-focus + 重试；失败自动导出诊断 |
| 准备横幅 | **last_e2e_failed** / **last_e2e_soft_fail**（焦点/启动）/ token 未确认；**打开诊断** / 导出 / 重跑 E2E+ |
| 打开产物 | `OpenComputerUseLogsFolder` / `OpenComputerUseLastDiagnostics` / `OpenComputerUseLastHistoryCSV` |
| 复制路径 | `CopyComputerUsePath(which)` — `diagnostics` / `csv` / `logs` |
| 日志维护 | `List…`（UI 最多 200）/ `Prune…`（**扫全量**最旧也可删）/ `Open…` / `Delete…` / **`BatchDelete…`**（最多 100） |
| 日志事件 | `computer-use:logs` — prune / delete / batch_delete 后广播，设置页与操作台自动刷新 |
| 配置 | `keep_newest`（10）、`max_age_days`（0）、**`computer_use_log_auto_prune`**（默认关；启动后台清理） |
| 启动预热 | UIA/YOLO/OCR；`registerComputerUseTools` **复用**已预热实例，避免冷启动覆盖 |
| 设置页 | 文件列表 **全部/诊断/CSV** Tab、打开/复制/删除/**多选批量删除**、保留策略、**启动自动清理**、prune confirm |
| 自检报告 | 内联 `last_e2e`、路径；设置页可 **复制路径 / 清理旧日志** |
| 操作员面板 | **Last E2E 卡片**；清理使用已保存策略并二次确认 |
| E2E 失败产物 | 同时写出 **diagnostics JSON + history CSV** 路径 |
| 托盘 / 状态 | `GetComputerUseStatus` / `GetComputerUseReadiness` / `GetComputerUseLastWarmup` |
| CLI doctor | `computer_use.*`：开关、OmniParser、UIA exe、macOS 权限、**log_policy**（keep/age/auto_prune） |

自检在 **Computer Use 总开关关闭时仍会执行**诊断（便于排障）；启动预热在关闭时会跳过。

### 交互 E2E（可选，会动真实桌面）

```bash
# Windows/macOS: 启动记事本/TextEdit 并短暂输入 token
set MACLAW_CU_E2E=1   # PowerShell: $env:MACLAW_CU_E2E=1
go test ./gui/ -count=1 -timeout 180s -run "TestComputerUseE2EInteract"
```

无桌面会话时不要开此开关；默认单测只校验结构与 soft_fail 语义（Linux 无启动目标时 soft_fail）。

| 平台 | 权限 / 预热 |
|------|-------------|
| Windows | UIA C#/PS sidecar；无 TCC；`ProbeDesktopPermissions` 报告 `uia_alive` / `uia_backend` |
| macOS | `AXIsProcessTrusted` + 可选 `CGPreflightScreenCaptureAccess`；需 **辅助功能** 与 **屏幕录制**；输入走 Quartz sidecar |
| Linux | AT-SPI / 显示权限（best-effort stub） |

YOLO `Warm()` 只加载权重、不跑检测；首次 `computer_observe` 才推理。权重缺失时 observe 仍可用 a11y/OCR。  
OCR `Warm()` 仅在 det/rec ONNX 模型文件（`~/.maclaw/models/ppocrv6_<tier>_{det,rec}.onnx`）已存在时加载引擎（**不**在预热阶段下载；缺失时由后台预载下载）。

API：`OpenComputerUsePermissionSettings(target)` — `accessibility` / `screen_recording`（macOS 深链）或 Windows `ms-settings:privacy`。

## 安全

- 默认拒绝 UAC / 系统安全类窗口：observe 时每个元素按中心点归因所属窗口标题（`Session.SetWindowResolver` + `accessibility.WindowTitleAtPoint`）；`computer_click` 直接用元素的归属窗口过 `Policy.AllowClickAt` 黑名单，`computer_type` 的聚焦点击在点击发生前同样检查，`computer_key` 按前台窗口标题检查。
- 可选 `TargetApps` 白名单：配置项 `computer_use_target_apps`（字符串数组或逗号分隔串，`PatchConfigFields`），每次工具调用前同步进 Session；黑名单优先于白名单。
- `computer_use_enabled=false` 时所有 `computer_*` handler 直接拒绝执行（不只是关闭激活门）。
- 步数上限默认 40。

## 代码

- `corelib/computeruse/` — Session、SoM、Policy、intent
- `corelib/guiautomation/input_windows.go` — 原生 Win32 输入
- `corelib/accessibility/uia_sidecar_windows.go` — 常驻 UIA（优先 C# / 回退 PS）
- `corelib/accessibility/warmup_windows.go` — `WarmupUIA` / `SelfCheckUIA`
- `corelib/accessibility/permission_*.go` — `ProbeDesktopPermissions`（macOS TCC / Windows UIA 姿态）
- `corelib/guiautomation/screen_parser_yolo.go` — OmniParser + `Warm()` 预加载
- `corelib/doctor/computer_use.go` — doctor `computer_use.*` 检查
- `corelib/accessibility/tools/MaclawUIASidecar/Program.cs` — C# UIA sidecar 源码
- `gui/tools_computer_use.go` — 工具、operator 事件、post-action settle
- `gui/computer_use_warmup.go` — 预热/自检/E2E（focus 轮询/soft_fail）、BatchDelete、日志事件、readiness
- `gui/tools_computer_use.go` — observe 失败 Guidance + `timing_ms` + `computer-use:error`
- `gui/computer_use_routing.go` — 激活、工具保底、playbook
- `gui/frontend/.../EmbeddingConfigPanel.tsx` — 设置开关 + 自检/E2E + **多选批量删除** + soft_fail 文案
- `gui/frontend/.../ComputerUseReadinessBanner.tsx` — last_e2e_failed / soft_fail / token 未确认
- `gui/frontend/.../ComputerUseOperatorPanel.tsx` — 操作员预览 + Last E2E 卡片
