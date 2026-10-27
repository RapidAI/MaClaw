# buddy 设计评审报告（v1 → v2）

- 日期：2026-10-27　评审对象：`DEVELOPMENT_PLAN.md` v1、`PROTOCOL.md` v0
- 结论：**v1 的方向成立，但 5 处 P0 级错误会让首版直接做不出来**，另有 9 处 P1 缺口。
  已按本报告重写为 v2（`DEVELOPMENT_PLAN.md` v2、`PROTOCOL.md` v2）。
- 评审方式：文档断言逐条对照仓库代码与实测硬件；每条结论附证据路径。

---

## 0. 评审摘要

| 级别 | 数量 | 性质 |
| --- | --- | --- |
| P0 | 5 | 不修则首版无法交付（数据源错、协议流被日志污染、字库缺字、容量预算缺失、打开端口即复位） |
| P1 | 9 | 会造成返工或验收争议（口径不一致、降级语义缺失、天气源未落地、分辨率/容量写错等） |
| P2 | 4 | 工程整洁度（共享协议定义、i18n、多设备、CI） |

v1 最需要肯定的一点：**USB 替代 WiFi 的判断是对的**。现有 `iot-agentos` 走
HTTPS 长轮询，服务端无法主动推（`docs/maclaw-companion-dev-plan.md` 现状基线表）；
buddy 接在 PC 上，任务变化可以毫秒级推送，且免去配网/配对/Token。这个前提成立。

---

## P0-1　任务源选错：`/api/mobile/jobs` 不是用户看到的任务列表

**v1 断言**：任务口径以 `GET /api/mobile/jobs`（`hub/internal/httpapi/mobile_jobs.go`）为基准。

**事实**：这个接口在 **hub** 上，需要 viewer 鉴权，聚合的是文档导入/导出、数字员工任务、
后端 SSH、agent 任务、会议录音——**云端侧任务**。而用户在 MaClaw 桌面侧栏看到的任务
列表来自另一条链路：

```text
SidebarTaskManagement.tsx
  ← App.tsx refreshTasks()
  ← wails 绑定 App.ListTasks(limit)
  ← App.SearchTasks → memoryStore.ProjectIndex().ListRecentMatching(isTaskManagementRecord)
  ← corelib/memory/project_index.go（由 memory.db 的 memories 表推导的内存索引）
```

证据：
- `guiapp/app_project_search.go:450` `func (a *App) ListTasks(limit int)`，注释明确
  "returns the user-visible task management list"。
- `guiapp/app.go:1189` `if pi := ms.ProjectIndex(); pi != nil` — 索引来自
  `memory.OpenDataDirStore(baseDir, memory.StoreModeSQLite)`。
- 本机实测：`C:\Users\ma139\.maclaw\memory\memory.db` 存在，`memories` 表 1216 行；
  而 `hub` 的 `jobs.db` 口径（`~/.maclaw/data/runtime/jobs.db`）实测 **0 行**。

**为什么不能"独立进程直接读库"**：`ProjectIndex` 是**内存态推导**，不是库里的现成列。
`memories` 表字段为 `id/content/category/tags/extra/...`，**没有 workflow status 列**
（实测 `PRAGMA table_info`）。要复现侧栏口径，独立进程必须复现：`inferProjectPath`、
`isTaskManagementRecord` 过滤、tags 解析、`task_prefs.json` 的 hidden/archived/pinned 合并、
以及 **live-running 信号**（`busyTaskRuns`/`activeAssistantTaskRunning`，只存在于 GUI 内存）。
成本高且极易与 GUI 漂移。

**处置（v2 已采纳）**：
1. **首选进程内集成**：在 `guiapp` 内新增 buddy 服务（后台 goroutine + 设置页开关），
   直接调 `App.ListTasks` 与 `ProjectIndex.OnChanged` 回调，**零口径漂移**。
   `ProjectIndex.OnChanged` 已经会 `emitEvent("task-list-changed")`（`guiapp/app.go:1192`），
   这是天然的推送钩子，无需轮询。
2. 独立 `buddy/host` 进程**降级为 M0/M1 联调工具**（mock/固定数据 + 手动 `--once` 推送），
   不作为交付形态。
3. 若将来确实要跨进程，走 **GUI 本地只读端点**（复用 `thirdparty_gateway.go` 已有的
   127.0.0.1 监听模式），而不是让 host 直接解析 memory.db。

---

## P0-2　协议流会被 ESP-IDF 日志污染（首版必然解析失败）

**v1 断言**：USB 口上跑 `\n` 分帧的纯 JSON。

**事实**：`iot-agentos/sdkconfig.defaults:30-31` 设了

```ini
CONFIG_ESP_CONSOLE_SECONDARY_USB_SERIAL_JTAG=y
CONFIG_ESP_CONSOLE_USB_SERIAL_JTAG_ENABLED=y
```

即 ESP-IDF 的日志输出**默认就走同一个 USB-Serial/JTAG**。本机实测（COM3，115200）
抓到 10662 字节启动日志，全是 `I (718) esp_image: ...` 这类行。
若 buddy 固件沿用该默认，PC 端收到的流是 `ESP_LOG 日志 + JSON` 交织，逐行 JSON 解析必然失败。

**处置（v2 已采纳）**：
- buddy 固件设 `CONFIG_ESP_CONSOLE_NONE=y`（或日志重定向到 UART0，用独立调试串口），
  **USB 口只承载协议帧**。
- 协议层再加两道保险：设备上抛的日志走**协议内的 `log` 消息**而非 `printf`；
  PC 端解析遇到非 `{` 开头的行直接丢弃并计数（不视为致命错误）。
- M0 spike 必须实测确认：刷 buddy 固件后 USB 口只出 JSON。

---

## P0-3　packed CJK 字库不覆盖 ASCII 与标点，任务标题会缺字

**v1 断言**：复用 `cjk24_cjk.bin`，中文不缺字。

**事实**：`main/cjk24_cjk.bin` 是 **U+4E00–U+9FFF 区间的定长点阵表**：
`20992 字 × 72 字节 = 1511424 字节`，与文件大小**精确吻合**。
取字逻辑 `main/compact_renderer.c:661` 只做线性偏移：

```c
if (codepoint < 0x4E00 || codepoint >= 0xA000) return false;
size_t offset = (size_t)(codepoint - 0x4E00) * DYNAMIC_GLYPH_BYTES;
```

即该字库**只含基本汉字区**。ASCII 走另一套 5×7 点阵（`draw_ascii_at`，`:623`），
而 **全角标点（，。：；（）「」）、CJK 符号（…—）、U+3000 以上、扩展 A 区汉字
全部不在其中**。真实任务标题如"整理会议纪要，并导出 Word（终稿）"会掉字。

**处置（v2 已采纳）**：
- 明确**字符覆盖矩阵**（见 v2 计划 §5.2），并规定缺字回退为**占位方框**而非静默跳过
  （静默跳过会让标题看起来"少了一截"，比方框更难诊断）。
- 字库生成脚本 `iot-agentos/tools/generate_cjk24.py` 需扩展：除 U+4E00–U+9FFF 外，
  追加 ASCII 可见区、CJK 标点（U+3000–U+303F）、全角形式（U+FF00–U+FFEF）。
- M1 增加"标题缺字率"验收：用 50 条真实任务标题跑一遍，缺字率必须为 0。

---

## P0-4　容量预算缺失：app 分区只剩约 150KB

**v1 断言**：复用 iot-agentos 分区表即可。未给容量预算。

**事实**（实测）：

| 项 | 数值 | 来源 |
| --- | --- | --- |
| Flash | **16MB** | `esptool flash-id`（COM3 实测，`Detected flash size: 16MB`） |
| app 分区 | `0x3a0000` = **3.625 MiB** | `iot-agentos/partitions.csv` |
| 现有 echoear 固件 | **3.47 MB**（3465712 B） | `build-unified-echoear/maclaw_esp32s3_client.bin` |
| 余量 | **约 150 KB** | 计算 |

v1 说"复用分区表 + 复用 1.5MB 字库"，但没说清预算。buddy 去掉 esp-sr/音频栈后
**固件体积会显著小于 3.47MB**，1.5MB 字库大概率装得下——但**这是必须实测的假设**，
不是可以跳过的前提。

**处置（v2 已采纳）**：
- M1 第一步先产出**固件体积预算表**，并以"app 分区占用 ≤ 80%（≤2.9MB）"为硬门禁。
- 若超限，退路依次为：① 字库裁成常用 3500 字子集（约 250KB）；② 加大 app 分区并后移
  model/storage（需完整刷写一次，参照 iot-agentos README 的 MP3 升级先例）；
  ③ 字库放独立分区按需读取。
- 该门禁写入 CI（`idf.py size` 结果比对）。

---

## P0-5　"打开端口不复位"是未验证假设，且 DTR/RTS 会复位

**v1 断言**：USB-Serial/JTAG 预期打开端口不复位；"波特率参数无意义"。

**事实**：
- 波特率对**原生 USB-Serial/JTAG 确实无意义**，但 v1 同时把 CP210x/CH340 列为兼容目标——
  那两个是真实 UART 桥，波特率**必须正确**。v1 的表述会误导实现。
- 更关键：仓库自己的脚本 `scripts/wake-esp32-devices.ps1` 开头明确写
  "it does not toggle DTR/RTS, **which can reset some ESP32 development boards**"。
  说明团队已经踩过这个坑。v1 却把"不复位"当成既定事实，没有对策。
- 与 esptool 共存也未验证：esptool 会拉 RTS/DTR 进下载模式；若 host 进程同时持有端口，
  烧录会失败。

**处置（v2 已采纳）**：
- M0 spike 增加三个必测项：① 打开/关闭端口是否复位（示波或看启动日志时间戳）；
  ② 显式不置 DTR/RTS 时的行为；③ host 持有端口时 esptool 能否烧录（预期不能，
  需定义"烧录前 host 必须释放端口"的流程）。
- 实现约束：host 打开串口时**显式设置 DTR=false / RTS=false**（或按实测结论固定），
  并把该结论写进协议文档。
- 备选方案明确化：若原生 USB-Serial/JTAG 不达标，改用 **TinyUSB CDC-ACM**（GPIO19/20）
  或外置 UART 桥；协议层不受影响。

---

## P1 问题清单

### P1-1　"进行中"有三个互不相同的口径，v1 混为一谈

- `taskStatusBucketFor()`（`SidebarTaskManagement.tsx:799`）基于 `active_workflow.status/phase`
  + `has_output`，产出 `running/pending/completed/paused/other`。
- `countRunningTaskRows()`（`:1003`）**额外**依赖 live 信号
  `busyTaskRuns` / `activeAssistantTaskRunning`，只有 GUI 内存里有。
- `mobileJobIsActive()`（hub 侧）又是第三套（子串匹配 `ready/done/fail/cancel`）。

**处置**：v2 定义 `in_progress = (bucket=='running') || isLiveRunningTaskRow()`，
与侧栏徽标严格一致；并规定该判断**只在 GUI 进程内计算**，设备不自行推断。

### P1-2　三计数缺"待处理/暂停"桶，会让"进行中"含义变味

侧栏分桶有 `running/pending/completed/paused/other` 五类，v1 只映射
`total/in_progress/done/failed`。用户侧栏里"进行中"其实**不含**"待处理（等我确认）"，
而 v1 的 `in_progress` 若按 `queued/running/claimed` 映射，会把待确认任务算进去。

**处置**：协议 v2 增加 `pending`（待处理）与 `paused` 计数，并规定
`total = done + in_progress + pending + paused + other`，设备端可选显示。

### P1-3　断连降级语义不完整

v1 只写了"10s 超时→待机页"。未定义：任务页半路断连时三计数是否保留、
host 重启后是否全量重发、PC 锁屏/休眠时 GUI 进程在但数据陈旧怎么办。

**处置**：v2 规定：断连时**保留最后已知计数并打"离线"角标**（不清零，
避免用户以为任务消失了）；重连后 host 必须全量重发；数据超过 60s 未更新标"陈旧"。

### P1-4　天气源没有落地路径

v1 说"阶段一实现候选…评审定稿"，但 `guiapp/device_ambient_weather.go` 的
`pushDeviceAmbientWeather()` 是**推给 Hub**，没有本地暴露接口；独立 host 进程拿不到。

**处置**：v2 明确三条路并给推荐：**进程内直接复用 `resolveDeviceAmbientWeather()`**（首选）；
GUI 加本地只读端点（次选）；host 自己调 Open-Meteo（`fetchOpenMeteoWeather` 已实现，
免费无 key，作为兜底）。

### P1-5　屏幕分辨率写错

v1 写 412×412。实测 `main/boards/echoear_2st/echoear_hardware_adapter.h:27-28`：
`ECHOEAR_DISPLAY_WIDTH/HEIGHT = 360`。**360×360**。24dot 字一行仅约 14 字，
这直接影响标题布局与滚动策略。

### P1-6　Flash 容量未写、未验证

实测 16MB，与 `partitions.csv` 的 0x1000000 布局吻合。v2 已补入基线表。

### P1-7　缺"无硬件也能开发"的路径

固件 UI 反复刷机效率低。`iot-agentos/tools/host_tests/` 已有 **78 个**主机单测 +
`check-*.ps1` 不变量脚本。buddy 应沿用：渲染逻辑与协议解析抽成可在 host 上编译的纯函数。

**处置**：v2 增加 B1-6"渲染/协议纯函数 + host 单测"，以及"协议回放测试"
（用录制帧跑 PC 端解析器）。

### P1-8　标题滚动未定义，且未评估 CPU/SPI 开销

360px 圆屏 + 24dot 字，长标题必须滚动或截断。v1 只说"滚动"，没定义速率、
是否整行重绘、对 SPI 带宽与功耗的影响。

**处置**：v2 规定默认**截断 + 末尾省略号**，触摸长按才滚动；滚动仅重绘标题区
（条带局部刷新），并纳入 M3 的 CPU/帧率观测项。

### P1-9　刷写风险描述不准确

v1 说"首次完整刷写会清空 NVS/storage"。**精确说法**：
- 只刷 app 分区（`0x10000`）**不动** NVS/storage；
- 完整刷写（含 `0x0` bootloader + 分区表 + `erase_flash`）才会清；
- 真正的风险是 ① 语音终端固件被覆盖（可用 `flash-app-on-com3.ps1` 重刷）；
  ② 若 buddy 固件**改了分区表布局**，则 NVS/storage 偏移变化会导致数据不可读。

**处置**：v2 明确"buddy 复用 iot-agentos 分区表，不改布局"，并保留完整刷写警示。

---

## P2 问题

| # | 问题 | 处置 |
| --- | --- | --- |
| P2-1 | 协议常量在 C 与 Go 两侧各写一份，必然漂移 | v2 增加 `buddy/protocol/` 单一来源：JSON Schema + 生成两侧常量；至少先用同一份 `spec.json` 做双端单测 |
| P2-2 | 缺中英文界面（现有固件有 `textForLang` 传统） | v2 列为 M4 可选项，首版固定中文 |
| P2-3 | 多设备（同时插两块板）未定义 | v2 明确首版只支持单设备，多设备列 M4 |
| P2-4 | USB 协议无鉴权（本机任意进程可打开） | v2 说明威胁模型：物理接入即信任，与现有刷机工具一致；不做鉴权，但在文档中显式声明 |

---

## 评审结论

v1 的产品判断（USB 有线、任务优先、空闲显示宠物+天气）**成立，予以保留**。
但技术实现有 5 处会直接导致首版失败的错误，其中 **P0-1（任务源）和 P0-2（协议流被日志污染）
属于"照文档写就一定跑不通"**。

v2 的关键调整：

1. **架构从"独立进程"改为"进程内集成优先"** —— 这是最大改动，由 P0-1 驱动。
2. **固件必须关闭 USB 控制台日志** —— 由 P0-2 驱动。
3. **字库覆盖矩阵 + 缺字回退** —— 由 P0-3 驱动。
4. **固件体积预算门禁** —— 由 P0-4 驱动。
5. **DTR/RTS 与烧录共存流程** —— 由 P0-5 驱动。
6. **三计数口径与侧栏严格对齐**（含 pending/paused）—— 由 P1-1/P1-2 驱动。

估算相应上调：v1 的 6～9 人日未计入上述修正，v2 调整为 **8～12 人日**。
