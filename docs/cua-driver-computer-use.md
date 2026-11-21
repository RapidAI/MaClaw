# Computer Use 改走 cua-driver：corelib 沉淀 + gui/bot 共用方案

## 目标
把 MaClaw 的 computer use 从「xdotool + 自建 YOLO/OCR/UIA 感知」改为「cua-driver（trycua/cua，Rust 驱动，v0.52.9）」为执行与观测底座，能力沉淀在 corelib，guiapp（本机桌面）与 MaClawSrv（bot 云桌面）共用同一套。

## 一、cua-driver 现状（已探明）

- 二进制：`cua-driver.exe`（trycua/cua 发行版），以
  `cua-driver.exe serve --socket \\.\pipe\<name> --no-overlay` 启动。
  - 单守护进程多会话；每个客户端连上的是**独立的 pipe 实例**
    （CreateFile CREATE_ALWAYS 即建一个），关闭句柄即结束该会话；
  - 宿主（OpenBot）生成的 pipe 名带随机 uuid（`openbot-cua-<uuid>`），
    无法硬编码；MaClaw 自己的部署用固定名（`\\.\pipe\maclaw-cua-<user>`
    或容器内 unix socket）；
  - CLI 自带 `cua-driver call <tool> [json]` / `list-tools` / `status` /
    `stop` / `doctor` / `mcp-config`，`--socket` 可指向任意实例。
- 58 个工具，核心增量：
  - **观测**：`get_window_state`（UIA 树 + 截图一次返回，element_token 寻址）、
    `get_desktop_state`、`zoom`（原生分辨率区域放大）、`parse_visual_regions`。
  - **动作**：`click / double_click / right_click / drag / scroll / type_text /
    hotkey / press_key / set_value`，`element_token`（语义）优先、像素兜底；
    `delivery_mode: background`（UIA Invoke/PostMessage，窗口不 raise）/
    `foreground`（真实输入，Chromium/Electron 内容必需）。
  - **确定性验证**：`verify_state`——UIA 断言（element exists/enabled/selected/
    value_equals、window bounds 容差），`stable_samples` + `timeout_ms` 轮询，
    0 LLM token。
  - **驱动端脚本**：`run_script`——JS 在驱动内执行（`cua.listWindows/
    sleep/call/getApp/launch`），单次调用内含条件循环/wait-until，
    `max_calls`≤500。
  - **菜单**：`invoke_menu` 原生菜单路径。
  - **浏览器**：`browser_*` 11 个（ref 寻址、dialog、download、navigate…）。
  - 会话/录制：`start_session / start_recording / health_report /
    get_guide`（指南内置：overview/drag/browser/permissions/batching/menus）。

## 已落地：corelib/cua 桥接包（第一片）

`corelib/cua/` 已完成并对着真实 `cua-driver serve` 验证：

- `driver.go` — `Open(ctx, addr)` / `OpenDefault(ctx)`（env
  `MACLAW_CUA_SOCKET`）/ `Health` / `Call` / `Close`。`Open` 带 30s
  探测上限，wedged pipe 不会卡住调用方。
- `runtime.go` — JSON-RPC 2.0 会话（`tools/list` 探活、`tools/call` 执行；
  未知工具名自动重试 `computer.` 前缀）。
- `pipe_windows.go` — Windows 命名管道（CreateFile CREATE_ALWAYS 每客户端
  一实例；阻塞式 Win32 读跑在 goroutine 里，`close()` 可让挂死的读
  立即返回 `ErrNoDriver`）。
- `pipe_other.go` — unix/tcp 传输。
- 测试：`MACLAW_CUA_SOCKET` 指向一个活的 `cua-driver serve` 时跑真
  连接（`health_report` + `list_windows`）；没有驱动时 skip，不假
  绿。

## 二、分叉点（为什么需要沉淀）

| 层 | 现状 | 问题 |
|---|---|---|
| 感知 | `corelib/computeruse`（SoM：YOLO+OCR+a11y，`Session`/ref） | 两套宿主各拼一套，且与 cua 的 element_token 语义重复 |
| 执行 | Linux：`corelib/desktop/computeruse` 把 Step 编译成 **xdotool argv**，容器内 `desktop_supervisor.py ctl` 执行 | 只覆盖 Linux 容器；语义点击（Invoke）缺失 |
| guiapp | `guiapp/tools_computer_use.go`（62KB）直接驱动 `corelib/guiautomation`（InputSimulator/YOLO/OCR） | 与 MaClawSrv 无共享执行层，验证靠 LLM 回读 |
| MaClawSrv | `desktop_computer.go`（App 工具 `app_run/computer`）走 ctl | 无 UIA 断言；wait 走 LLM；菜单靠 LLM 逐点 |

## 三、设计：`corelib/cua` 统一包

```
corelib/cua/
  driver.go      Driver 抽象：FindDriver(探测路径/env)、Health()、Call(tool, args) JSON
  transport.go   传输：NamedPipe(Windows \\.\pipe\*)、TCP(localhost)；cua-driver serve 的
                 线协议适配（serve 端即 MCP stdio 同款 JSON，逐条 换行/长度前缀帧）
  session.go     会话：StartSession(label)/EndSession，与 MaClaw 的 owner/tenant 映射
  observe.go     GetWindowState(pid, hwnd, query)/Desktop()/Zoom/ParseRegions
  act.go         Click/Type/Hotkey/Scroll/Drag/SetValue（element_token 优先，
                 delivery_mode 透传；token 失效→重新 observe 的 retry 规则）
  verify.go      Verify(expect..., stable_samples, timeout) → 结构化 verdict
  wait.go        WaitUntil(cond, timeout)：驱动端 run_script（cua.listWindows+sleep）
                 + Go 侧轮询 fallback
  menu.go        InvokeMenu(path)
  browser.go     透传 browser_* 给 corelib/browser 的现有 CDP 路径作为增强
  guide.go       GetGuide(topic) → 注入提示词
  step.go        MaClaw computeruse.Step / Anthropic/OpenAI CUA action → cua 工具调用
                 （复用 corelib/desktop/computeruse 的解析器，换执行后端）
```

宿主注入点（保持现有依赖方向）：
- `Driver` 由宿主构造并传给 Session；corelib 内不 import guiapp/MaClawSrv。
- guiapp：`computerUseRuntime` 的 input/bridge/yolo/ocr 四件套替换为
  `*cua.Driver` + `cua.Session`；`computer_observe/click/type/...` 工具名与参数
  **不变**（LLM 提示词、playbook、ref 语义兼容映射：`e3` → 最近一次
  `get_window_state` 的 element index）。
- MaClawSrv：`desktopComputerActions` 的 `app_run/computer` 步骤经
  `cua.Step` 走容器内 cua-driver（容器镜像装 Linux 版 `cua-driver serve --socket
  unix:///tmp/cua-<id>.sock`，supervisor 托管）；**保留 xdotool 后端**作
  `MACLAW_CUA_BACKEND=auto|cua|xdotool` 的 auto 降级（驱动健康检查失败即回落）。
- Hub `/mcp`（desktopmcp）：`read/control` 工具面不变，执行后端同样切 cua。

## 四、能力提升映射

| 能力 | 之前 | 之后 |
|---|---|---|
| 操作后验证 | LLM 回读文本（desktop_verify.go） | `verify_state` UIA 断言（0 token），LLM 回读保留为兜底 |
| 等待外部事件 | LLM `wait` 轮询（每轮烧 token） | `run_script` 驱动端 wait-until |
| 菜单 | LLM 逐次 click | `invoke_menu` 一条 |
| 语义点击 | ref/像素 + UIA 优先（自建） | `element_token` + UIA Invoke（驱动内置，跨 Windows/Linux） |
| 后台操作 | 总是 bring_to_front | `delivery_mode: background` |
| 小字识别 | 整图缩放喂模型 | `zoom` 原生分辨率局部 |
| 感知 | 本地 YOLO（1GB scratch）+ OCR | UIA 树优先（`capture_mode: ax`），YOLO 降为可选增强 |
| 轨迹/回放 | 自建 step journal | 驱动 `start_recording/replay` 可选项 |

## 五、风险与约束

1. **许可**：cua 仓库根 MIT；`cua-spacesd`/`cua-viewer` 等媒体 crate 是
   FSL-1.1-MIT。`cua-driver` 发行二进制是否链接 FSL crate 需确认（docs/
   desktop-computer-use-cua-upgrades.md 已注明"只借鉴思路，未复制代码"——
   直接依赖其二进制/协议前出法务结论；发行版本身是 GitHub 公开构建）。
2. **驱动托管**：pipe 名随机 → 宿主负责 spawn 并把路径传给 MaClaw
   （env `MACLAW_CUA_SOCKET` / 容器内固定 unix socket）。guiapp 可复用宿主已起的
   实例；无宿主环境时 corelib/cua 可 spawn 自己的守护（serve 参数同构）。
3. **Linux 容器**：cua-driver 有 Linux 版（GTK/AT-SPI 后端）；
   `maclaw-gui:2` 镜像需打包二进制 + `install_ffmpeg` 弃用后 H264 路径不受影响。
4. **UIA 非穷举**：`verify_state` 文档明示"absence cannot be proven"——
   断言只信存在/enabled/selected，消失类判断走窗口级。
5. **兼容**：Anthropic/OpenAI CUA action 解析器保留，后端可切换；
   老提示词（SoM `eN`）通过 observe 结果映射层过渡，不一次性改 LLM 词法。

## 六、实施切片（建议顺序）

1. `corelib/cua` 骨架：transport（pipe/TCP）+ `Call` + `Health` + 单测
   （对着真实驱动跑 e2e 标记，CI 无驱动时 skip）。
2. `verify.go` + `wait.go` 先行：MaClawSrv 加 `desktop_verify_state` /
   `desktop_wait_until` 两个 execute action（提示词规则随之加），
   这是最小收益切片。
3. 观测/动作映射：`get_window_state` → 现有 observe 输出格式（eN 兼容）；
   guiapp `computerUseRuntime` 切 cua 后端（env 灰度）。
4. 容器化：`maclaw-gui:2` 加 cua-driver + supervisor 托管 + socket 约定；
   desktopd ctl 增加 `cua_*` 通道；desktopmcp 工具面后端切换。
5. 清理：YOLO 默认降为可选（`MACLAW_CUA_YOLO=auto`），SoM 映射层退场。
