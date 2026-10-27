# MaClaw Buddy（码卡龙伴侣）

基于 ESP32-S3 的 MaClaw 有线伴侣小屏：通过 **USB 串口**接入电脑，
有任务时显示任务状态（总任务 / 进行中 / 已完成），无进行中任务时显示
宠物动画与天气（形态参考 `iot-agentos` 客户端待机页）。

```text
MaClaw GUI (guiapp)                buddy service（进程内）           ESP32-S3（EchoEar-2ST）
  SidebarTaskManagement 口径  ──▶  Runner（串口/心跳/重连）  ──USB──▶  buddy firmware
  device_ambient_weather      ──▶  行帧 JSON + 序号              任务页 / 待机页 / NVS 天气
```

## 已实现

| 部分 | 位置 | 状态 |
| --- | --- | --- |
| 协议（Go + spec 单一来源） | `protocol/` | ✅ 测试覆盖（帧解析、不变式、seq、UTF-8 截断） |
| 固件（ESP-IDF 6.0.2） | `firmware/` | ✅ 编译通过，**1.87 MB**（app 分区 3.625 MB 的 49%） |
| 串口服务（Go） | `service/`、`serialport/` | ✅ 单元测试（伪串口） |
| GUI 集成 | `guiapp/buddy_service.go` | ✅ 自动连接 + Wails 设置接口 |
| 前端快照推送 | `guiapp/frontend/src/components/layout/buddySnapshot.ts` | ✅ 与侧栏同口径 + 单测 |
| 设置面板 | `guiapp/frontend/src/components/settings/BuddySettingsSection.tsx` | ✅ 设置 → 硬件 |
| 联调 CLI | `host/` | ✅ `go run ./buddy/host -list` |
| 刷写/回滚 | `tools/` | ✅ 先备份 app 分区，只写 `0x10000` |

## 文档

| 文件 | 内容 |
| --- | --- |
| [`docs/DEVELOPMENT_PLAN.md`](docs/DEVELOPMENT_PLAN.md) | 开发计划 v2（里程碑、风险、验收） |
| [`docs/DESIGN_REVIEW.md`](docs/DESIGN_REVIEW.md) | v1 设计评审：5 处 P0、9 处 P1 与证据 |
| [`docs/PROTOCOL.md`](docs/PROTOCOL.md) | 串口协议 v2（消息、口径、容错、威胁模型） |

## 固件验证（2026-10-27，真机）

- 测试板：COM3 / ESP32-S3 / EchoEar-2ST；原固件 `V7.1.0.11876-dirty`（Aug 15 2026）。
- 备份：`tools/backup/voice-app-backup.bin`（3,801,088 B，
  SHA256 `6e1e82ea…62a5ed`），恢复脚本见下。
- 烧录后 `buddy-host` 收到 `fw=buddy-0.1.0 board=echoear-2st` 与持续
  `state_ack`，页面随 `in_progress` 在 `tasks`/`standby` 间切换。
- **回滚已验证**：用备份刷回后再读设备 app 描述符，版本/ELF SHA 与备份一致，
  随后重新刷回 buddy。**测试板当前运行 buddy 固件**。
  刷回语音终端：
  `tools\restore-voice-firmware.ps1 -Backup tools\backup\voice-app-backup.bin -Port COM3`。
- **真实 GUI 端到端已跑通**：设备三计数与侧栏逐状态一致
  （60/0/2/58 → 创建后 61/0/2/59 → 删除后 60/0/2/58），
  侧栏更新→设备 ACK 299–369ms；50 条真实标题的设备缺字数 **0**。

## 硬件基线（2026-10-27 实测）

| 项 | 结果 |
| --- | --- |
| 端口 | **COM3**，`VID 303A PID 1001`（乐鑫原生 USB-Serial/JTAG + JTAG 复合设备） |
| 芯片 | **ESP32-S3 (QFN56) rev v0.2**，8MB octal PSRAM |
| Flash | **16MB**，quad，3.3V |
| 屏幕 | **360×360** 圆屏，ST77916 QSPI + CST816 触摸（EchoEar-2ST） |
| 触摸 | I2C SCL=11 / SDA=12，地址 0x15（真机 `hello.touch=true`，缺失不致命） |

## 构建固件

需要 ESP-IDF **v6.0.2**（`C:\esp\v6.0.2\esp-idf`）。`build.cmd` 会清理 Git Bash
继承的 `MSYSTEM` 后再调用 `export.bat`：

```bat
cd buddy\firmware
build.cmd -B build build
```

产物：`firmware/build/buddy_firmware.bin`（只写 app 分区即可）。

> 关键配置：`CONFIG_ESP_CONSOLE_NONE=y` + `CONFIG_ESP_CONSOLE_SECONDARY_NONE=y`，
> USB 口只承载协议帧，ESP-IDF 日志不会污染 JSON（P0-2）。

## 刷写与回滚

```powershell
# 1. 先在 设置 → 硬件 关闭“启用伴侣屏连接”（串口独占）
# 2. 备份现有 app 分区并刷入 buddy
cd buddy\tools
.\flash-buddy.ps1 -Port COM3

# 刷回语音终端固件
.\restore-voice-firmware.ps1 -Backup .\backup\voice-app-20261027-081500.bin -Port COM3
```

- 只写 app 分区（`0x10000`），**不动** NVS / `model` / `storage`
  （会议录音、配对信息、天气缓存保持）。
- 完整刷写（`erase_flash`/写 bootloader/分区表）才会清数据；buddy 复用
  `iot-agentos/partitions.csv`，布局不变。
- `flash-buddy.ps1` 校验 VID/PID、固件体积（< `0x3a0000`）与备份大小。

## 联调（无 GUI）

```bash
go run ./buddy/host -list           # 列出串口并标记 buddy 候选
go run ./buddy/host -port COM3      # 推送 mock 任务/天气，设备应在任务页/宠物页间切换
go run ./buddy/host -idle           # 只推 0 个进行中任务 → 宠物+天气
```

## 协议要点

- 行帧 UTF-8 JSON（≤1024B），`\n` 分隔；非 `{` 开头的行会被丢弃计数（ROM 启动日志容错）。
- `state` 携带 `counts` 与 `current`，设备校验
  `total == in_progress + pending + paused + done + failed`。
- `clock` 由 host 下发（设备无 RTC/NTP）。
- 心跳 3s，离线 10s；断连保留最后计数并打"离线"角标。
- 任务口径**由前端计算**（`taskStatusBucketFor` + live-running 信号），Go 只传输，
  因此不会与侧栏漂移。详见 `docs/PROTOCOL.md` §3.1。
- 字库覆盖不足时画方框并计入 `missing_glyphs`，不静默吞字；**不支持的 ASCII
  标点也计入**（50 条真实标题实测为 0）。

## 已知限制（M4）

- 触摸仅用于唤醒，尚未做翻页/滚动。
- 宠物为本地矢量动画，尚未下发 128×128 RGB565 资产。
- 单设备；多板型与其他控制器未适配。
