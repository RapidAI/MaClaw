# ClawMate Maker

ClawMate Maker 是面向 MaClaw ESP32-S3 设备的跨平台固件安装与诊断工具，支持 Windows、macOS 和 Linux 打包交付。

当前实现提供：

- 自动发现 USB/串口候选设备；唯一候选会触发只读 ROM 与 Flash 探测。
- 仅支持 GitHub Release 中精确允许的三种签名固件包：EchoEar 2ST、Bread Compact、Fangtang 4G。
- Release 下载具备 HTTPS 主机限制、缓存锁、断点续传、SHA-256、GitHub digest 与嵌入式发布签名验证。
- 运行时 protocol:1/2 身份可作为自动预取的板型线索；实际刷写始终要求用户确认实物板型。
- 写前兼容性、安全状态、分区布局和设备绑定校验；写后读回哈希与 protocol:2 启动验证。
- 写入失败或启动未验证时持久化恢复状态，阻止新的刷写任务。
- 全流程结构化、脱敏日志与用户选择目录的诊断 ZIP 导出。

开发验证：

```powershell
go test ./...
go vet ./...
wails build -platform windows/amd64 -clean -o ClawMateMaker.exe
```

完整架构、安全约束、协议和发布门禁见 [设计文档](docs/esp32-flasher-design-zh.md)。
