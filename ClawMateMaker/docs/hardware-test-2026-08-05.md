# ClawMate Maker 实机只读验证记录

> 日期：2026-08-05
> 主机：Windows
> 端口：COM4
> 范围：设备枚举、ROM 探测、安全状态、Flash/PSRAM、分区表读取、App 元数据和启动日志。
> 注意：本轮没有发起新的擦除或固件写入；测试开始时发现已有外部刷写进程正在写 `storage`，等待其完成后才继续。

## 结果摘要

| 项目 | 实测结果 | 设计结论 |
| --- | --- | --- |
| Windows 自动枚举 | `VID 303A / PID 1001 / MI_00`，COM4 | SetupAPI/PnP 路径可用 |
| ROM 自动识别 | ESP32-S3 QFN56 revision v0.2 | 芯片识别可自动化 |
| USB 模式 | USB-Serial/JTAG | 首个 profile 无需第三方 UART 驱动 |
| Flash | 16 MB，DIO，80 MHz 构建配置 | 与设计目标匹配 |
| PSRAM | 运行中固件报告内置 8 MB，启动自检通过 | 可作为应用态能力证据，但不是 ROM 独立探测结果，也不足以唯一确认板型 |
| Secure Boot | Disabled | MVP 默认安全策略可放行 |
| Flash Encryption | Disabled | MVP 默认安全策略可放行 |
| Anti-rollback / eFuse secure version | 本轮记录未保留可审计读数 | 按设计 v0.3 尚不能单凭本记录完成安全放行；正式工具必须重新读取并确认处于 profile 基线 |
| 当前分区布局 | `0x9000` NVS、`0x10000` App、`0x3b0000` model、`0x6b0000` storage | 与仓库当前分区表完全一致 |
| App 元数据 | `maclaw_esp32s3_client`，`V6.6.3.11756-4-g04f2582f-dirty`，ESP-IDF 6.0.2 | 可直接从 App image header 读取，不必只依赖启动日志 |
| 启动 | 无 panic/watchdog；Wi-Fi、Gateway handshake、storage mount 正常 | hard reset + 串口重连方案可行 |
| 结构化身份/成功证明（初测） | 未发现 `board_id`、`layout_id`、`CLAWMATE_EVT/BOOT_OK` | 已在同日增量实现并复测，见下方“固件标识改进复测” |

## 分区表实测

从 ROM Bootloader 读取 `0x8000` 起始的 4096 字节，ESP-IDF parser 输出：

```text
nvs,data,nvs,0x9000,24K
phy_init,data,phy,0xf000,4K
factory,app,factory,0x10000,3712K
model,data,spiffs,0x3b0000,3M
storage,data,spiffs,0x6b0000,9536K
```

设备读取结果的前 3072 字节与 `esp32s3-maclaw-client/build/partition_table/partition-table.bin` 逐字节相同，后 1024 字节均为 `0xFF`。这验证了设计 Review 中提出的方案：App-only 前可以只依赖 ROM Bootloader 读取、规范化并解析当前 partition table，而不要求旧 App 正常启动。

## App image header 实测

只读取 App 分区开头 4096 字节即可得到 `esp_app_desc` 中的：

```text
version:      V6.6.3.11756-4-g04f2582f-dirty
project_name: maclaw_esp32s3_client
compile_time: Aug 5 2026 07:24:54
idf_version:  v6.0.2
```

因此固件匹配流程应增加 `ReadAppDescriptor`：读取固定小范围并解析 app descriptor，避免为识别当前版本读取完整 App。

## 启动日志实测

hard reset 后 COM4 保持/恢复可用，20 秒日志显示：

- bootloader 识别 16 MB Flash 和预期分区表；
- 8 MB Octal PSRAM 检测及 memory test 通过；
- App、LCD、音频、storage、Wi-Fi 初始化成功；
- Gateway handshake 返回 HTTP 200；
- 未出现 panic、Guru Meditation、assert 或 watchdog；
- 未出现 `CLAWMATE_EVT` / `BOOT_OK`。

## 对设计的直接修正建议

1. 把 ROM 读取分区表及 layout fingerprint 作为 App-only 的强制前置条件。
2. 增加读取 `esp_app_desc` 的设备检查步骤，用于当前版本和 project name 校验。
3. 当前 VID/PID、芯片、Flash、PSRAM 只能形成 `probable` 的 EchoEar/Bread Compact 候选；未有制造板卡 ID 时不得标记 `confirmed`。
4. 固件增加可重复查询、带 nonce 的 `BOOT_STATUS` 协议；当前一次性普通日志不能证明目标版本和板型。
5. MVP 固定 fail-closed：Secure Boot、Flash Encryption 或 anti-rollback/eFuse secure version 非未启用基线、或状态无法可靠判断时阻止刷写，直到另行实现安全量产流程。
6. 实测启动可能依赖网络 Welcome 和模型初始化；本地启动状态必须在网络就绪之前可查询，网络状态另报独立服务状态。

## 固件标识改进复测

在 `esp32s3-maclaw-client` 增加结构化身份模块后，于同一台 COM4 设备完成 App-only 刷写与查询复测：

- 写入范围仅为 factory App：`0x10000`，未改写 NVS、model、storage。
- 刷写前从 ROM 读取的真实分区表与目标布局逐项一致；Secure Boot 与 Flash Encryption 均为 Disabled。
- 构建产物大小 `0x2e8db0`，小于 `0x3a0000` App 分区。
- 启动事件包含人类可读名和稳定机器字段：`display_name`、`product_id`、`board_id`、`hw_rev`、`layout_id`、`compat_id`、App/IDF 版本、ELF SHA、芯片、Flash/PSRAM 容量。
- 设备对带 nonce 的 `IDENTIFY`、`BOOT_STATUS`、`SERVICE_STATUS` 查询均可重复响应。
- 实测身份为 `ClawMate / Bread Compact Wi-Fi LCD`，`board_id=bread-compact-wifi-lcd-v1`，`layout_id=maclaw-s3-16m-factory-v2`。
- 实测 `BOOT_OK.local_ready=true`，且 `SERVICE_STATUS.service_ready=true`；查询日志未发现 panic、Guru Meditation 或 watchdog。

注意：固件自报 `layout_id` 仍只能作为交叉证据。刷机工具必须继续读取真实 partition table 并计算布局指纹后，才允许 App-only 更新。

同理，8 MB PSRAM 来自运行中固件初始化/自检，不应在设计中记为 ROM Bootloader 已独立探测。设备无法启动时 PSRAM 可能为 `unknown`；正式工具必须保留证据来源，并在目标固件启动后再次验证 manifest 所需容量。

以上 `protocol:1`、`BOOT_OK`、`board_id`、`firmware_version` 和 `local_ready/service_ready` 是 2026-08-05 实验协议的原始实测事实，不代表正式桌面工具契约。设计文档 v0.3 已将正式契约冻结为 `protocol:2` 的 `BOOT_STATUS.ready` / `SERVICE_STATUS.ready`，并把编译期板型字段命名为 `firmware_target_board_id`。在固件迁移和重新实测前，实验协议事件只能用于诊断，不能作为正式刷机成功证明。

## 测试安全说明

测试开始时 COM4 被一个已有流程占用，该流程正在把 `build-codex-layout/storage.bin` 写入 `0x6b0000`。未中断正在进行的单镜像写入；等待其完成并确认 `Hash of data verified`、hard reset 后，再执行上述只读探测。该写入不是本轮测试发起的操作。
