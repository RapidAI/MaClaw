# ESP32-S3 MaClaw 宠物薄客户端（起步工程）

这是独立的 ESP-IDF 工程，不会改动仓库中已有的桌面端或 MaClawSrv 代码。它通过 MaClawSrv 的第三方 IM Gateway 串成最小闭环：按键/触摸触发 → 录音 WAV 上传为媒体 → Gateway `incoming` 事件 → MaClaw Agent → Gateway 长轮询 `outgoing` → 屏幕显示答复 → 宠物状态变化。

## 已对接的 MaClawSrv 接口

| 阶段 | 第三方协议接口 | 设备状态 |
| --- | --- | --- |
| 设备握手 | `POST /api/im-gateway/v1/handshake` | `quiet` → `idle` |
| 上行音频 | `POST /media/upload-url` → 上传 URL `PUT` WAV → `POST /incoming` | `listening` → `thinking` |
| 下行答复 | `GET /outgoing`（长轮询）→ `POST /ack` | `thinking` → `speaking` → `done` → `idle` |

该协议由服务端把媒体附件交给 Agent，再把完整回答写入下行队列，因此设备不需要持有 MaClaw API Key/Secret、instance ID 或 agent ID。宠物语义是 `idle/listening/thinking/speaking/done/alert/quiet`。当前服务端下行类型为 `text`；固件保留了媒体扩展入口，后续可让 Gateway 下发 TTS 音频和宠物动作事件。

## 先完成三处板级适配

当前仓库没有 ESP32-S3 固件、屏幕/音频驱动，也没有 ESP-IDF 或 PlatformIO。因此 [`main/board_port.c`](main/board_port.c) 是明确的板级适配层：

1. 在 `board_port_init` 初始化 LCD（推荐 LVGL）、按键/触摸和 I2S；按下时从任务上下文调用注册的回调。
2. 在 `board_port_set_pet_state` 为七种语义状态切换宠物帧/动画。
3. 在 `board_port_capture_wav` 录制并返回完整 WAV（16 kHz、单声道、16-bit PCM 是推荐基线）。

`board_port_show_text` 则负责把识别文本和 Agent 回复画在屏幕上。可选的 `board_port_play_wav` 为服务器 TTS 预留。

## 配置与编译

安装 ESP-IDF v5.2+ 后，在 ESP-IDF 已导出的终端运行：

```powershell
cd D:\workprj\aicoder\esp32s3-maclaw-client
idf.py set-target esp32s3
idf.py menuconfig
idf.py build flash monitor
```

在 `MaClaw ESP32-S3 thin client` 菜单配置 Wi-Fi、MaClawSrv 基地址、稳定的 `clientId` 和默认 `conversationId`。Gateway Token 仅作为工厂调试兜底；日常绑定不需要在硬件上输入任何 token。

## 无键盘语音配对

设备首次启动显示“请按下并说出 6 位绑定码”。用户在已经登录的 MaClaw Web/App 中调用 `POST /api/v1/device-pairings`，取得一个 10 分钟有效、仅能使用一次的六码数字，例如 `645432`；设备录到这六码音频后上传到 `POST /api/device-gateway/v1/pair/voice`。MaClawSrv 做 ASR，核验并返回 Gateway Token；固件将它保存到 NVS，之后按普通 Gateway 方式握手和通信。

服务端为该设备所属 MaClaw 用户开启 `thirdparty_gateway_enabled`。首次绑定会在缺少配置时生成 `thirdparty_gateway_token`；设备拿到的 token 是 Gateway 专用凭据，会用于握手、媒体上传、上行消息、下行轮询与确认；不要复用管理员密钥、用户 API Key 或 API Secret。

当前 Gateway 的配置模型是“一个 MaClaw 用户一个 Gateway Token”。因此若同一用户绑定多台设备，它们会共享这个 Gateway Token；设备丢失时应立即重置该用户的 Gateway Token 并重新为所有硬件设备配对。下一阶段可以把它扩展成每设备独立 token 与吊销列表。

不要将管理端密钥或常用用户密钥刷入设备。设备应使用可撤销的专用 credential；生产版应将密钥放入 NVS 加密分区或安全元件，而不是仅放在 `sdkconfig`。

## 当前可验证范围

工程结构、ESP-IDF 组件依赖、Wi-Fi、HTTPS、语音配对、Gateway 握手、媒体上传、上行事件、下行长轮询、确认和状态机已落地。你已说明本机装有 ESP-IDF 6.0.2，但当前终端未继承 ESP-IDF 的环境变量，所以尚未实际运行 `idf.py build`。音频与屏幕也必须按具体 LCD 控制器、I2S 麦克风/编解码器和引脚表实现。

## 协议扩展建议

现有协议足够完成音频附件上行和文本下行。如果要让硬件体验更好，建议在不破坏现有 `text` 消息的前提下新增：

- `type: "pet_state"`：`extra.state` 为七种宠物语义状态，`durationMs` 可选；设备收到即可切换动画。
- `type: "tts"`：沿用现有 `attachments`，下发 `audio/wav` 或 MP3；设备播放后才发送 `ack(status="played")`。
- `type: "partial"` / `progress: true`：增量文本和思考提示，最终 `text` 消息保持兼容。

要实现这些扩展，应在 MaClawSrv 的 `thirdparty_gateway.go` 下行入队处添加消息类型；ESP32 只需在 `poll_reply` 中按 `type` 分派。先确认实际屏幕和扬声器规格后，我可以把服务端与固件两端一并补上。

## 会议录音操作

固件通过握手中的 `features.meetingRecorder: true` 报告会议录音能力，并采用 Hub 返回的 `meetingRecording.basePath`、`chunkSize` 和可用处理模式。

- 待机单击：录制一次语音命令并交给 MaClaw Agent。
- 待机双击：开始会议录音。
- 会议录制中单击：暂停或继续；暂停期间持续排空麦克风 DMA，但不写入 WAV。
- 会议录制中长按：结束录音、补写 WAV 头并上传。
- 待机长按三秒：清除 Wi-Fi 和配对配置，重新进入设置热点。

会议音频以 16 kHz、16-bit、单声道 PCM WAV 流式写入 `/storage/meeting.wav`，不会把整场会议放进 RAM。屏幕显示动态波形、音量、已录时长和暂停状态。

上传使用 Hub/mobile 共用的文稿库协议：创建录音对象、按 Hub 宣告的大小分块上传、每块 `X-Chunk-SHA256`、全文件 SHA256、`complete`，最后根据握手可用性选择 `minutes`、`transcript` 或 `keep`。成功后录音可在 mobile/GUI 文稿库查看。

上传进度保存在 NVS；失败时保留 SPIFFS 中的 WAV。设备重启并重新连上 Hub 后会查询服务器端录音状态并从下一个未完成分块继续。只有 Hub 已接受处理请求后才删除本地 WAV 和恢复元数据。