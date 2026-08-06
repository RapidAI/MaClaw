# ClawMate Maker 实机只读验证记录

- 日期：2026-08-06
- 主机：Windows
- 端口：COM4（ESP USB Serial/JTAG）
- 范围：仅 ROM Bootloader 探测；未擦除、未写入 Flash。

| 检查项 | 实测结果 |
| --- | --- |
| 芯片 | ESP32-S3 QFN56，revision v0.2 |
| USB | USB-Serial/JTAG；MAC `b4:3a:45:a1:e5:84`（诊断日志会脱敏） |
| PSRAM | Embedded PSRAM 8 MB |
| Flash | 16 MB，Manufacturer `5e` / Device `4018` |
| Secure Boot | Disabled |
| Flash Encryption | Disabled |
| Anti-rollback secure version | `0`（eFuse `SECURE_VERSION=0`） |

结论：该端口可通过 ROM 自动识别为支持范围内的 ESP32-S3（16 MB / 8 MB PSRAM）。由于 EchoEar 2ST、Bread Compact 与 Fangtang 4G 不能靠 USB/ROM 信息互相区分，工具仍应要求选择板上标签或验证制造身份后才能确定目标固件。

命令使用随 ESP-IDF 安装的受控 esptool：

```powershell
esptool.exe --port COM4 --baud 115200 chip-id
esptool.exe --port COM4 --baud 115200 flash-id
esptool.exe --port COM4 --baud 115200 get-security-info
```
