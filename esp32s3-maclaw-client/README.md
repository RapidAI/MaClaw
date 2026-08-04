# EchoEar-2ST MaClaw 硬件终端

这是面向 EchoEar-2ST（ESP32-S3-R8）的 ESP-IDF 6.0.2 工程。设备作为 MaClaw GUI 的硬件交互界面，通过 Hub 中转访问 GUI，无须让电脑向局域网或公网开放端口。

```text
EchoEar-2ST  ⇄  HTTPS / Hub  ⇄  MaClaw GUI  ⇄  Agent / IM / Mobile 文稿库
```

设备可完成 Wi-Fi 配网、一次性短码配对、语音命令、文本和 WAV 答复、宠物与天气同步、会议录音和断点续传。持久凭据是配对后换取并保存在 NVS 中的 Gateway Token；六码配对码只用于换取 Token，不会在每次连接时重复使用。

## 硬件与板级驱动

- 芯片/模组：ESP32-S3-R8 / EchoEar-2ST。
- 显示：ST77916 圆屏，触摸为 CST816。
- 音频：ES8311 + I2S，16 kHz、16-bit、单声道 PCM WAV。
- [`main/board_port.c`](main/board_port.c) 已实现 LCD、触摸、麦克风、扬声器、背光休眠和本地宠物动画，不再是待适配的空壳。
- 设备只有电源键可稳定用于供电控制，日常交互以触摸屏为主；BOOT/GPIO0 仅作为辅助输入。

## Wi-Fi 与首次配置

没有有效 Wi-Fi 配置时，设备稳定进入配置模式并创建热点，而不是在待机页和 Setup 页之间切换。屏幕显示热点二维码和名称，手机连接后打开门户即可配置。

- 门户自动扫描并列出附近 SSID；AP-only 模式会临时切到 AP+STA 完成扫描。
- 默认 Hub 地址为 `https://hub.mypapers.top`。
- 支持个人 Wi-Fi 和企业 Wi-Fi 参数。
- 保存前检查字段长度并给出中文错误；NVS 中不存在旧键属于正常情况。
- Wi-Fi 连接成功后，待机页显示 Wi-Fi 状态；断线采用 1–60 秒指数退避。
- 配置门户五分钟无操作会退出/重启，防止设备永久卡在门户。

## Hub 配对与持久 Token

用户在 MaClaw GUI 中生成仅使用一次的六码配对码，例如 `645432`。配对码有效期为 30 分钟。手机在设备配置门户中输入该码，设备经 Hub 换取专用 Gateway Token 并写入 NVS。

无键盘场景也可以在未配对状态点击屏幕，直接说出六位数字。固件把短 WAV 发送到当前配置的 Gateway 的 `POST /api/device-gateway/v1/pair/voice`，由 MaClawSrv 或本地 GUI 的 SenseVoice ASR 提取数字，再通过同一份单次配对记录换取 Token。Hub 公网部署需要在 Hub 端配置语音配对 ASR；GUI 本地网关模式复用桌面端已经下载的 SenseVoice 模型。

正常启动和重连只使用持久 Token。只有以下情况才再次进入配对流程：

- 设备从未配对；
- 用户在设置中清除配对；
- Hub 返回 401/403，说明 Token 已撤销或失效。

已配对设备长按进入 Wi-Fi 重配置时，只需选择新热点并填写 Wi-Fi
凭据；设备保留当前 Hub 与 Gateway Token，不再次要求一次性配对码。
若要迁移到另一个 Hub，应先在本机设置页执行“清除配对”，再用新
Hub 生成的六码短码完成绑定，避免把旧 Hub 的持久 Token 发往新地址。

配对失败时会开启 AP+STA 恢复门户，保留现有 Wi-Fi，方便只更新配对码。不要把管理员密钥、MaClaw API Key 或 API Secret 写入设备。

## 第三方接入协议

设备通过 Hub 暴露的第三方 Gateway 协议通信：

| 阶段 | 接口 | 设备行为 |
| --- | --- | --- |
| 握手 | `POST /api/im-gateway/v1/handshake` | 报告输入、输出和硬件能力，获取宠物、天气和会议能力 |
| 上行媒体 | `POST /media/upload-url` → `PUT` WAV | 上传语音命令附件 |
| 上行消息 | `POST /incoming` | 把文本/附件交给 GUI Agent |
| 下行 | `GET /outgoing?limit=1` | 长轮询文本、音频、环境、宠物和会议结果 |
| 确认 | `POST /ack` | 仅在设备确实显示或播放成功后确认 |

握手使用协议 `1.1`，当前 ESP 声明：

- 输入：文本、`audio/wav`（16 kHz、单声道）；
- 输出：纯文本、纯音频、音频+文本；
- 文本：中文、纯文本、最多 240 个字符；
- 音频：`audio/wav`，inline 上限 8 KiB，同源 URL 下载上限 256 KiB；
- 功能：宠物状态、GUI RGB565 宠物资产、本地 skin 回退、环境显示、会议录音、音量控制。握手分别声明 `petAsset:true` 和 `petAnimation:true`；固件通过同源短期 URL 下载最多两帧 128×128 RGB565LE 到 PSRAM，不把大块 base64 塞进 TLS 握手。

小音频可直接放在下行 JSON 中。较大音频由 Hub/GUI 写入媒体对象存储，下发同源相对 URL。ESP 只允许下载 `/api/im-gateway/v1/media/`，避免把持久 Bearer Token 发往任意域名。下载或播放失败时不 ACK、不推进 cursor，由服务端重试。

服务端会按照能力声明限制 Agent 的输出形式，避免向没有相应显示/播放能力的硬件发送图片、文件或不支持的音频。GUI 切换宠物后会下发 `pet_profile` 和小型 `pet_asset` 引用；ESP 下载并显示 GUI 实际渲染帧，下载或校验失败时继续使用同一 skin 的本地紧凑形象。下载成功的资源会以带版本、尺寸和 SHA-256 的缓存文件原子写入 `storage`；设备重启或 Hub 暂时离线时可直接恢复，握手遇到相同 `revision` 也不会重复下载或写闪存。损坏缓存会自动删除并回退本地形象；清配对或清除全部配置会同时清除缓存，但不会删除会议录音。

## 日常操作

- 待机单击屏幕：启动一次语音命令并显示真实 PCM 波形；再次点击立即结束并提交，不操作则 6 秒后自动提交。
- 待机双击屏幕：开始会议录音。
- 会议录音中单击屏幕：停止并进入保存/续传流程。
- 待机长按约三秒：进入重新配置热点，但在新配置保存前不主动删除旧配置。
- 待机页右上角齿轮：进入设备设置。

设置页提供：

- 音量滑动调节，立即作用于 ES8311，并持久化到 NVS；
- 熄屏时间：1/5/10/15/20/30 分钟、1–5 小时或不熄屏；
- 清除配对：确认后删除 Token 和旧配对码，保留 Wi-Fi，自动进入配对恢复热点；
- 清除全部：确认后删除 Wi-Fi、Hub、配对、天气、音量和熄屏设置，自动进入完整配网热点。

两种清除操作都保留 `/storage/meeting.wav` 及会议续传元数据，避免配置维护造成用户录音丢失。

## 会议录音

会议音频流式写入 `/storage/meeting.wav`，不会把整场会议放进 RAM。录音页显示计时和约 768 ms 的真实 PCM 正负峰值波形；96 个波形列来自最终写入文件的同一份音频，并使用双缓冲减少闪烁。

停止后固件补写 WAV 头，按 Hub 握手协商的分块大小上传并保存 recording ID、阶段和下一个分块编号。断电或断网后可继续上传。Hub 完成转写、逐字稿或纪要后，结果进入 Mobile 文稿库。

## 待机显示

待机页显示当前宠物、日期、星期、Wi-Fi 状态、天气和当前温度。天气由 MaClaw GUI 经握手或 `ambient` 下行消息同步，并缓存到 NVS；过期数据会标为陈旧，而不是退回乱码或覆盖宠物画面。中文提示和设置使用 24×24 点阵，日期/星期使用较紧凑字体。

## 构建与安全刷写

本机已经使用 ESP-IDF v6.0.2 成功构建。推荐从普通 PowerShell 调用 IDF 环境：

```powershell
cd D:\workprj\aicoder\esp32s3-maclaw-client
cmd.exe /d /s /c "call C:\esp\v6.0.2\esp-idf\export.bat >nul && idf.py -B build-gateway-fix build"
```

保留现有 Wi-Fi、Token 和会议数据时，只刷 App 分区：

```powershell
C:\Users\ma139\.espressif\python_env\idf6.0_py3.14_env\Scripts\python.exe `
  -m esptool --chip esp32s3 -p COM3 -b 460800 `
  --before default-reset --after hard-reset `
  write-flash 0x10000 build-gateway-fix\maclaw_esp32s3_client.bin
```

不要为普通功能更新刷写 bootloader、partition table、NVS、`srmodels` 或 `storage`。仓库中的 [`tools/flash-app-on-com3.ps1`](tools/flash-app-on-com3.ps1) 会先校验 COM3 的 Espressif VID/PID 和固件 SHA256，再只刷 `0x10000`。

## 实机回归检查

每次刷写后至少确认：

1. 启动日志没有 panic、assert、watchdog 或循环重启；
2. 无 Wi-Fi 时配置热点稳定，门户能扫描 SSID 并保存；
3. 有 Wi-Fi 和 Token 时直接进入稳定待机，不在 Setup/待机间闪烁；
4. 齿轮、音量、熄屏、确认/取消、清配对和清全部触摸区域正确；
5. 语音波形随安静、讲话和拍手真实变化，停止录音可用；
6. 会议录音可以单击停止并在重启后续传；
7. Hub 下发超过 8 KiB 的 WAV 时，设备可通过同源媒体 URL 下载并播放。
