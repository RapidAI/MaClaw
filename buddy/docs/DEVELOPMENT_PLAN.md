# MaClaw Buddy 开发计划 v2

- 日期：2026-10-27　状态：待评审（取代 v1）
- 变更依据：`DESIGN_REVIEW.md`（v1 有 5 处 P0 错误、9 处 P1 缺口）
- 定位：**ESP32-S3 USB 有线伴侣小屏**——任务状态展示（总任务 / 进行中 / 已完成），
  无进行中任务时显示宠物动画与天气（形态参考 `iot-agentos` 待机页）
- 协议：`PROTOCOL.md` v2

---

## 1. 目标与非目标

### 1.1 目标（首版）

| 维度 | 目标 |
| --- | --- |
| 接入 | USB 有线（ESP32-S3 原生 USB-Serial/JTAG，枚举为 COM 口），插上即用，无需配网/配对/Token |
| 任务显示 | 总任务 / 进行中 / 已完成三计数 + 当前任务标题与进度；任务变化 ≤1s 上屏 |
| 口径一致 | 与 MaClaw 侧栏任务列表**严格同源同口径**（同一次 `ListTasks` 调用结果），不自行推断 |
| 空闲显示 | `in_progress == 0` 时切到宠物动画 + 天气 + 日期 |
| 断连降级 | 保留最后已知计数并打"离线"角标；天气走 NVS 缓存并标陈旧；重连全量恢复 |
| 可测性 | 渲染与协议解析为纯函数，可在 PC 上单测（沿用 `iot-agentos/tools/host_tests` 模式） |

### 1.2 非目标（首版）

- **不做语音/音频**（无 esp-sr、无 codec 栈）——与 iot-agentos 语音终端区分。
- **不做 WiFi 配网与 Hub 通信**——buddy 数据全部来自 USB 对端 PC。
- **不做 OTA**——刷机走 USB + esptool。
- **不改第三方 Gateway 协议**（`corelib/im/thirdparty_protocol.go`）。
- **不支持多设备**（同时插两块板）：首版单设备，多设备列 M4。
- **不做鉴权**：物理接入即信任，与现有刷机工具一致（威胁模型见 PROTOCOL.md §7）。

---

## 2. 现状基线（实测，2026-10-27）

### 2.1 硬件

| 项 | 结果 | 证据 |
| --- | --- | --- |
| 端口 | COM3，`USB\VID_303A&PID_1001`（乐鑫原生 USB-Serial/JTAG + JTAG 复合设备） | `Get-PnpDevice` |
| 芯片 | ESP32-S3 (QFN56) rev v0.2，双核 240MHz，8MB octal PSRAM | `esptool chip-id` |
| Flash | **16MB**，quad 类型，3.3V | `esptool flash-id` |
| MAC | `ac:a7:04:29:e2:50` | `esptool read-mac` |
| 当前固件 | `maclaw_esp32s3_client`（app @0x10000，多核 + octal PSRAM 初始化） | COM3 启动日志抓取 |
| 屏幕（推断） | **360×360** 圆屏（EchoEar-2ST ST77916 + CST816 触摸） | `boards/echoear_2st/echoear_hardware_adapter.h:27-28` |

> ⚠️ 板型为**推断**，M0 点屏确认。若不是 EchoEar-2ST，M1 驱动层需重新评估。

### 2.2 容量预算（v2 新增，P0-4）

| 项 | 数值 |
| --- | --- |
| app 分区 | `0x3a0000` = 3.625 MiB（`iot-agentos/partitions.csv`） |
| 现有 echoear 固件 | 3.47 MB（3465712 B）→ 余量仅约 150 KB |
| packed CJK 字库 | 1.5 MB（`cjk24_cjk.bin`，20992 字 × 72 B） |
| buddy 固件（预期） | 去掉 esp-sr/音频/会议/配网后显著缩小，**但必须实测** |

**硬门禁**：app 分区占用 ≤ 80%（≤2.9 MB）。超限退路：
① 字库裁常用 3500 字子集（约 250 KB）→ ② 扩 app 分区并后移 model/storage（需完整刷写一次）
→ ③ 字库独立分区按需读取。

### 2.3 任务源（v2 修正，P0-1）

v1 误用 hub 的 `/api/mobile/jobs`（云端侧任务，需 viewer 鉴权）。
**正确来源是 GUI 进程内**：

```text
SidebarTaskManagement.tsx ← App.refreshTasks()
  ← wails: App.ListTasks(limit)                      guiapp/app_project_search.go:450
  ← App.SearchTasks → ProjectIndex().ListRecentMatching(isTaskManagementRecord)
  ← corelib/memory/project_index.go（由 memory.db 推导的内存索引）
```

关键约束：
- `ProjectIndex` 是**内存态推导**；`memories` 表**无 workflow status 列**（实测 schema）。
- "进行中"还依赖 GUI 内存里的 live 信号（`busyTaskRuns` / `activeAssistantTaskRunning`），
  **独立进程无法复现**。
- 已有推送钩子：`ProjectIndex.OnChanged` → `emitEvent("task-list-changed")`
  （`guiapp/app.go:1192`）。

→ **结论：任务源必须在 guiapp 进程内取，buddy 走进程内集成（见 §3）。**

### 2.4 串口基线（v2 新增，P0-2）

`iot-agentos/sdkconfig.defaults:30-31` 启用
`CONFIG_ESP_CONSOLE_SECONDARY_USB_SERIAL_JTAG=y`，ESP-IDF 日志**默认走同一 USB 口**。
实测 COM3 抓到 10662 B 启动日志（`I (718) esp_image: ...`）。
→ buddy 固件必须关闭该控制台（详见 §5.4）。

---

## 3. 总体架构（v2：进程内集成优先）

```text
┌────────────────── MaClaw GUI 进程（guiapp）──────────────────┐        ┌──── 设备 ────┐
│  ProjectIndex.OnChanged ──┐                                  │        │ buddy 固件   │
│  App.ListTasks ───────────┼──▶ buddyService (新增)           │  USB   │              │
│  device_ambient_weather ──┘    ├ 口径计算（复用侧栏函数语义） │ COM3   │ 协议层       │
│                                ├ 变更去抖 + 全量快照          │ ═════▶ │ 任务页       │
│  设置页：启用 / 端口选择        ├ 串口写入（go.bug.st/serial）│        │ 待机页+宠物  │
│                                └ 心跳 / 重连 / 诊断日志       │        │ NVS 天气缓存 │
└──────────────────────────────────────────────────────────────┘        └──────────────┘
```

**为什么不再用独立进程**（v1 方案）：见 §2.3。独立 `buddy/host` 仅保留为
**M0/M1 联调工具**（固定数据 + 手动推送），不作为交付形态。

**接口设计**（便于日后拆分）：

```go
// guiapp/buddy_service.go
type BuddyTaskSnapshot struct {
    Total, InProgress, Done, Pending, Paused, Failed int
    Current []BuddyTaskItem   // 进行中，按侧栏顺序，最多 5 条
    UpdatedAt time.Time
}
type BuddyTaskItem struct {
    ID, Title string
    Progress  float64  // 0..1
}
type BuddyTransport interface {          // 串口实现 / 内存 fake（测试用）
    Write([]byte) error
    Close() error
}
type BuddyTaskSource interface {         // 真实实现：App；测试：固定数据
    Snapshot(limit int) BuddyTaskSnapshot
}
```

---

## 4. 显示规则（核心产品逻辑）

| # | 条件 | 显示 |
| --- | --- | --- |
| 1 | host 在线 且 `in_progress > 0` | **任务页**：三计数（大字号）+ 当前任务标题 + 进度条 |
| 2 | host 在线 且 `in_progress == 0` | **待机页**：宠物动画 + 天气 + 日期 |
| 3 | host 离线（10s 无消息） | 待机页；**保留最后计数于任务页角标**（不清零），天气标陈旧 |
| 4 | 数据 >60s 未更新（host 在但卡住） | 当前页右上角"陈旧"标记 |
| 5 | 熄屏超时 | 背光关闭；触摸或串口消息唤醒 |

**任务页布局（360×360 圆屏，安全区直径约 340px）**：

```text
        ┌───────────────┐
        │   总  7       │   ← 24dot 大字号
        │ 进行中 2      │   ← 高亮色
        │ 已完成 4      │
        │ ─────────────│
        │ 整理会议纪要… │   ← 标题：默认截断+省略号
        │ ▓▓▓▓▓▓░░░ 62%│   ← 进度条
        └───────────────┘
```

- 标题默认**截断 + 省略号**（不做常驻滚动，省 SPI 带宽与功耗）；触摸长按才滚动。
- 多任务（`current` >1）时按 3s 轮换，或触摸翻页。

---

## 5. 关键技术决策

### 5.1 任务口径（与侧栏严格对齐，P1-1/P1-2）

复用侧栏同一套语义（`SidebarTaskManagement.tsx:799` `taskStatusBucketFor`）：

| 设备字段 | 判定（在 GUI 进程内计算） |
| --- | --- |
| `in_progress` | `bucket == 'running'` **或** `isLiveRunningTaskRow()` 为真 |
| `pending` | `bucket == 'pending'`（待我确认/待处理） |
| `paused` | `bucket == 'paused'` |
| `done` | `bucket == 'completed'` |
| `failed` | `bucket == 'other'` 且状态含 fail/error/blocked |
| `total` | `done + in_progress + pending + paused + failed` |

不变式：**`total` 恒等于各桶之和**（设备端断言，不满足则显示"数据异常"）。
实现上 Go 侧需与 TS 侧对齐，并加一条**契约测试**：同一批 mock 任务分别跑 TS 分桶
与 Go 分桶，结果必须逐条一致（防止两侧漂移）。

### 5.2 字符覆盖矩阵（P0-3）

| 区段 | 现状 | buddy 处置 |
| --- | --- | --- |
| U+4E00–U+9FFF（基本汉字 20992） | ✅ `cjk24_cjk.bin` 定长表 | 复用 |
| ASCII 0x20–0x7E | ✅ 5×7 点阵（`draw_ascii_at`） | 复用 |
| CJK 标点 U+3000–U+303F（，。：；等） | ❌ 缺 | 生成脚本追加 |
| 全角形式 U+FF00–U+FFEF | ❌ 缺 | 生成脚本追加 |
| 扩展 A 区 U+3400–U+4DBF | ❌ 缺 | 缺字回退（见下） |

**缺字回退**：一律画**占位方框**并计入 `missing_glyphs` 计数，通过 `log` 消息上抛，
**不静默跳过**（静默跳过会让标题看起来少了一截，更难诊断）。

### 5.3 天气源（P1-4）

优先级：
1. **首选**：进程内直接复用 `guiapp/device_ambient_weather.go` 的
   `resolveDeviceAmbientWeather()`（已有 Open-Meteo 实现，45min 刷新、2h 过期）。
2. 次选：GUI 增加本地只读端点（复用 `thirdparty_gateway.go` 的 127.0.0.1 监听模式）。
3. 兜底：host 侧自行调用 `fetchOpenMeteoWeather`（免费无 key）。

字段对齐 `deviceAmbientWeather`：`{summary, temperatureC, location}`。

### 5.4 串口与控制台隔离（P0-2）

buddy 固件 `sdkconfig.defaults` 必须：

```ini
CONFIG_ESP_CONSOLE_NONE=y                 # USB 口只走协议
# 或保留 UART0 调试：CONFIG_ESP_CONSOLE_UART_DEFAULT=y + SECONDARY_NONE
```

另外：
- 设备日志走**协议内 `log` 消息**，不用 `printf`。
- PC 端解析：非 `{` 开头行直接丢弃并计数（容错，不致命）。

### 5.5 DTR/RTS 与烧录共存（P0-5）

- host 打开端口时**显式 `DTR=false / RTS=false`**（仓库 `scripts/wake-esp32-devices.ps1`
  已记录"DTR/RTS 会复位某些 ESP32 板"的教训）。
- 烧录前 host 必须**释放端口**（M0 实测 esptool 与 host 共存行为后固化流程）。
- 波特率：原生 USB-Serial/JTAG 无意义；若兼容 CP210x/CH340，则**必须**正确设置
  （默认 115200，允许配置）。

### 5.6 分区与刷写（P1-9）

- buddy **复用 iot-agentos 现有分区表**，不改布局（避免 NVS/storage 偏移变化）。
- 只刷 app 分区（`0x10000`）不动 NVS/storage；完整刷写才清数据。
- 保留"如何刷回语音终端固件"的可执行文档与脚本。

---

## 6. 里程碑与任务分解

估时为单人初估。v2 较 v1（6–9 人日）上调至 **8–12 人日**，主要增量在 P0 修正。

### M0　基线与 spike（1.5～2 天）

| ID | 任务 | 估 | 验收标准 |
| --- | --- | --- | --- |
| B0-1 | 目录骨架 + 文档（v2 评审通过） | 0.5 | 本文档 + `DESIGN_REVIEW.md` + `PROTOCOL.md` 评审通过 |
| B0-2 | **板型确认**：最小工程点亮屏幕，确认分辨率/触摸/背光 | 0.5 | 屏幕出图；360×360 与触摸结论写入 §2.1 |
| B0-3 | **USB 链路 spike**（P0-2/P0-5）：`CONFIG_ESP_CONSOLE_NONE` 下 echo 固件 ↔ PC 双向 1KB 行；测打开/关闭是否复位、DTR/RTS 行为、esptool 共存、插拔 10 次 | 1 | 五项结论全部落表；`PROTOCOL.md` §6 打勾 |

### M1　固件（3～4 天）

| ID | 任务 | 估 | 验收标准 |
| --- | --- | --- | --- |
| B1-1 | 新 ESP-IDF 工程（IDF 6.0.2，esp32s3），**复用现有分区表**；产出**体积预算表**（P0-4） | 0.5 | `idf.py size` 结果记录；app 占用 ≤2.9MB |
| B1-2 | 板级适配：ST77916 / 背光 / 触摸 / 熄屏（从 `board_port.c` + `echoear_2st` 裁剪，去音频） | 1 | 点屏、背光、触摸唤醒可用 |
| B1-3 | **字库扩展 + 覆盖矩阵**（P0-3）：扩展 `generate_cjk24.py`，补 CJK 标点/全角；缺字方框回退 | 1 | 50 条真实任务标题缺字率为 0 |
| B1-4 | 任务页 UI：三计数 + 标题截断/滚动 + 进度条（360×360 安全区） | 1 | mock 数据渲染正确；中文不缺字 |
| B1-5 | 待机页：宠物动画 + 天气 + 日期（裁剪 `ambient_service`/`weather_cache_service`，保留 NVS 缓存与陈旧标记） | 0.5 | 断连后天气走缓存并标陈旧 |
| B1-6 | 协议层 + **渲染/解析纯函数化**（P1-7）：行帧收发、cjson 解析、心跳超时、页面状态机；纯函数可在 host 编译 | 1 | 状态机全场景单测通过 |

### M2　GUI 集成（2～3 天）

| ID | 任务 | 估 | 验收标准 |
| --- | --- | --- | --- |
| B2-1 | `guiapp/buddy_service.go`：串口管理（枚举识别 VID/PID、DTR/RTS 显式、断线重连、`--list` 诊断）、`BuddyTransport` 接口 + fake | 1 | 插拔自动恢复；fake 可用于无硬件测试 |
| B2-2 | **任务口径计算**（P0-1/P1-1/P1-2）：`BuddyTaskSource` 实现，复用 `ListTasks` + live 信号；**Go/TS 契约测试** | 1 | 同一批 mock 任务两侧分桶逐条一致 |
| B2-3 | 变更推送：`ProjectIndex.OnChanged` / `task-list-changed` 钩子 + 去抖（200ms）+ 全量快照 | 0.5 | 真实任务变化 ≤1s 上屏 |
| B2-4 | 天气接入（§5.3 首选路径）+ 30min 刷新 | 0.5 | 字段与协议示例一致 |
| B2-5 | 设置页：启用开关、端口选择、连接状态、诊断日志 | 0.5 | 可在 GUI 内开关并看到状态 |

### M3　联调与稳定性（1.5～2 天）

| ID | 任务 | 估 | 验收标准 |
| --- | --- | --- | --- |
| B3-1 | 全链路场景验收（§8 清单） | 1 | 清单全通过并留记录 |
| B3-2 | 长跑与资源：8h 挂机，heap watermark、帧率、CPU | 0.5 | 无泄漏；帧率与 CPU 记录在案 |
| B3-3 | 刷机流程固化：一键烧录脚本（对齐 `flash-app-on-com3.ps1` 校验风格）+ **刷回语音终端**文档 | 0.5 | 两向刷机均可重复 |

### M4　产品化（后续另行排期）

- B4-1 触摸交互：任务翻页、手动刷新、范围切换。
- B4-2 宠物资产 USB 分块下发（复用 128×128 RGB565LE 格式）。
- B4-3 ClawMateMaker 增加 buddy 固件条目（签名 .clawfw）。
- B4-4 多板型适配（waveshare_amoled_1_75c / bread_compact / fangtang_4g）。
- B4-5 多设备支持；中英文界面。

---

## 7. 风险与对策

| # | 风险 | 等级 | 对策 |
| --- | --- | --- | --- |
| R1 | **USB-Serial/JTAG 打开即复位 / 与 esptool 抢端口** | 高 | B0-3 spike 先行；显式 DTR/RTS=false；烧录前释放端口；备选 TinyUSB CDC-ACM 或外置 UART 桥 |
| R2 | **固件体积超 app 分区**（1.5MB 字库 + 代码） | 高 | B1-1 体积预算门禁 ≤2.9MB；三级退路（字库子集 / 扩分区 / 独立分区） |
| R3 | **任务口径与侧栏漂移** | 高 | 口径计算放 GUI 进程内；Go/TS 契约测试锁死 |
| R4 | 板型推断错误（非 EchoEar-2ST） | 中 | B0-2 点屏确认后再进 M1 |
| R5 | 字库缺字导致标题不可读 | 中 | B1-3 覆盖矩阵 + 缺字方框 + 50 条真实标题验收 |
| R6 | 天气源不可达 | 中 | 三级路径（进程内 / 本地端点 / 直连 Open-Meteo） |
| R7 | 刷 buddy 覆盖语音终端固件 | 中 | 复用分区表不改布局；B3-3 固化两向刷机脚本 |
| R8 | GUI 进程崩溃/退出导致设备停在旧数据 | 低 | 心跳 10s 超时 → 待机页 + 离线角标 |

---

## 8. 首版验收清单

- [ ] 插入 USB ≤5s 内识别并显示正确页面
- [ ] 三计数与侧栏任务列表**逐条一致**（含 pending/paused 边界）
- [ ] `total == done + in_progress + pending + paused + failed`（不满足显示"数据异常"）
- [ ] 任务变化 ≤1s 反映；`in_progress` 归零瞬间切宠物页
- [ ] 50 条真实任务标题缺字率为 0
- [ ] 拔线后保留最后计数 + 离线角标；重连全量恢复
- [ ] 插拔 20 次无挂死
- [ ] 8h 长跑稳定（heap watermark 无持续下降）
- [ ] app 分区占用 ≤2.9MB
- [ ] USB 口仅输出协议帧（无 ESP_LOG 污染）
- [ ] 语音终端固件可用脚本刷回

---

## 9. 实现进展与偏差（v2 → 代码）

### 9.1 已完成

| 里程碑 | 状态 | 证据 |
| --- | --- | --- |
| M0 硬件/链路 | 完成 | COM3 实测 ESP32-S3/16MB/360×360；USB 链路在真机联调中验证 |
| M1 固件 | 完成 | `firmware/build/buddy_firmware.bin` **1.87 MB**；控制台已关闭；字库扩展 321 字 |
| M2 GUI 集成 | 完成 | `guiapp/buddy_service.go`、串口自动识别、心跳/重连、设置面板 |
| M3 联调 | 完成 | 真机烧录后 `hello`/`state_ack`/页面切换均正常（见 §9.3） |

### 9.3 真机验证结果（2026-10-27）

- 设备：COM3 / ESP32-S3 / EchoEar-2ST，原固件 `V7.1.0.11876-dirty`（Aug 15 2026）。
- 备份：`buddy/tools/backup/voice-app-backup.bin`，3,801,088 字节，
  SHA256 `6e1e82eaa2970a261d3f70213a62d698cc5d27c984cd69a65e569a39a362a5ed`。
- 烧录：只写 app 分区 `0x10000`，NVS/storage 未触碰。
- 联调：`go run ./buddy/host -port COM3` 观察到
  `fw=buddy-0.1.0 board=echoear-2st`、持续 `state_ack`，
  页面随 mock 的 `in_progress` 在 `tasks`/`standby` 间正确切换。
- 设备自身日志帧：`buddy firmware started`（协议 `log` 通道）。

> 注意：备份/恢复脚本针对本板 USB-Serial/JTAG 的读流不稳定做了分块与
> no-reset 重连处理，详见 `tools/backup_app_session.py` 注释。

### 9.4 真实 GUI 端到端验证（2026-10-27）

- 修复了一个只在真机链路暴露的缺陷：前端 effect 调用了不存在的
  `PushBuddySnapshot`（正确方法名 `BuddyPushSnapshot`），导致挂载后从不推送。
  修正后挂载即推送，`seq` 正常递增。
- 三计数与侧栏 DOM **逐状态完全一致**（total / in_progress / pending / done）：

  | 状态 | 设备 counts | 侧栏 |
  | --- | --- | --- |
  | 初始 | 60 / 0 / 2 / 58 | 60 / 0 / 2 / 58 |
  | 创建真实任务后 | 61 / 0 / 2 / 59 | 61 / 0 / 2 / 59 |
  | 删除该任务后 | 60 / 0 / 2 / 58 | 60 / 0 / 2 / 58 |

- **上屏时延**：以侧栏实际更新时刻为起点，到设备 `state_ack`：
  创建 **369ms**、删除 **299ms**（均 ≤1s）；Go 收到推送→ACK 为 30ms/47ms。
- **缺字检查**：将 50 条真实任务标题逐条下发，设备心跳上报的累计缺字为 **0**。
- 验证创建的临时任务已删除，侧栏恢复原数；验证用的临时构建补丁（WebView2
  调试参数、SmartScreen 开关、模块 replace）均已还原。

### 9.5 复查与优化（2026-10-27）

- **离线初始态**：设备启动时不再误报在线 10 秒，待机页立即显示"未连接"。
- **缺字统计**：`state_ack` 与心跳都携带累计缺字数，设置面板取两者最大值。
- **动画节流**：待机页按 120ms 帧重绘（原来每个 100ms tick 全屏重绘），
  任务页仅在计数/标题/轮换变化时重绘，QSPI 负载明显下降。
- **状态深拷贝**：`Runner.Status()` 返回独立的计数与任务切片，调用方不会
  误改服务内部状态。
- **标签语义**：任务页 `failed` 桶显示"异常"（覆盖失败/取消/阻塞）。
- **整包构建**：把 `AssistantTitleBarNotifications.tsx` 中两个纯函数提取到
  `AssistantTitleBarNotifications.groupInvite.ts`（行为不变），
  `npm run build` 的 CSS 纪律、编码、配置持久化、主 UI 守卫、Wails 绑定检查
  以及 `tsc`、`vite build` 全部通过。
- **备份脚本修正**：`flash-buddy.ps1` 曾调用已被真机证明会卡死的旧分块实现；
  现改为循环调用经过验证的 `backup_app_session.py`（512B 分块、no-reset
  续传、按会话预算重启），删除旧脚本。
- **退出释放串口**：`App.shutdown` 现在会停止 buddy runner 并等待其关闭端口；
  切换端口时同样先等待旧 runner 退出，避免新旧连接抢同一 COM 口。
- **串口短写**：写帧改为循环直到整帧写完（`io.ErrShortWrite` 兜底），并加了
  单元测试。
- **ASCII 覆盖透明化**：不支持的**可打印 ASCII** 不再静默画成 `?`，而是画方框
  并计入缺字数。真机复测：50 条真实标题仍为 **0**；含 `#` 的测试标题为 **1**，
  证明计数器有效。
- **单位排版**：温度单位改用图元绘制小圆环 + `C`，与数字高度对齐。
- **触摸可观测**：设备 `hello` 新增 `touch` 字段；真机实测 **`touch=true`**
  （CST816 存在且轮询任务已启动）。`buddy-host` 状态行与设置面板都会显示，
  面板在未检测到时提示「触摸未检测到」。
- **缺字检查防假阳性**：`buddy-host -titles` 在设备未连接时直接失败退出，
  不会把"没连上"误报成"0 缺字"。
- **时延基准修正**：服务启动重放缓存快照时重置推送时间，避免把上一会话的
  旧时间戳算进 `latency_ms`。

### 9.2 实现偏差（有据可依）

1. **任务口径改由前端计算，Go 只传输。**
   原因：`busyTaskRuns` / `activeAssistantTaskRunning` 是 React 状态
   （`App.tsx:529`），Go 进程拿不到。前端用同一组
   `taskStatusBucketFor` / `isLiveRunningTaskRow` 生成 `snapshot`，经
   `BuddyPushSnapshot` 交给 Go。这样**口径零漂移**，也省掉 Go/TS 双实现契约测试。
   代价：GUI 前端必须运行（桌面应用本就常驻）。

2. **新增 `clock` 消息。**
   设备无 RTC/NTP，待机页日期不能靠设备自己算。Host 每分钟下发
   `{date, weekday}` 预格式化字符串（`PROTOCOL.md` §4.1）。

3. **任务分区算法细化。**
   为避免"live-running 但 bucket=pending"被重复计数，`buddySnapshot.ts`
   先把 live-running/running 行划入 `in_progress`，其余行再按侧栏桶计数，
   保证 `total == Σbuckets` 恒成立。

4. **`failed` 桶语义。**
   取侧栏 `other` 桶（失败/取消/阻塞），设备标签用"异常"，避免把已取消
   任务误称为失败。

5. **进度条。**
   `TaskManagementItem` 不含数值进度，协议传 `progress:-1`，设备画不定态
   动画而不是伪造百分比。

6. **触摸只做唤醒。**
   CST816 探测失败不影响启动；翻页/滚动列为 M4。

