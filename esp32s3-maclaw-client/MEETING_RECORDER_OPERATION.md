# MaClaw Pet 会议录音操作规范

该设备是会议的采集入口，并且复用移动端既有的“会议录音 / 文稿库”处理链路。
所有上行数据走 `ESP32 -> HTTPS Hub -> GUI`；GUI 保持已有的出站 Hub WebSocket，
因此可在 NAT、隔离网和移动网络中工作。硬件绝不将音频直接暴露给局域网。

## 物理操作

| 操作 | 默认动作 | 屏幕状态 |
| --- | --- | --- |
| 短按（少于 800 ms） | 普通语音问答 | listening -> thinking -> speaking |
| 长按 2 秒 | 打开会议录音确认 | meeting-ready |
| 短按确认 | 开始会议录音 | 录音舞台：呼吸红点、计时器、动态波形 |
| 短按 | 暂停/继续 | 暂停波形 / 恢复动态波形 |
| 长按 2 秒 | 结束录音并确认上传 | meeting-review |
| 短按确认 | 上传、转写、生成纪要 | meeting-uploading -> meeting-processing -> done |
| 长按 2 秒取消 | 丢弃当前未上传片段 | idle |

会议开始、暂停、结束都必须以屏幕状态和提示音（硬件扬声器启用后）反馈；开始时显示
专用“录音舞台”，而不是单纯文字：边框和红点会呼吸、中央显示时长、底部波形持续起伏，
并提示“按键暂停 / 长按结束”。暂停时波形冻结并换成琥珀色，清晰区分“正在采集”和
“会话未结束但暂时暂停”。后续真实 I2S 录音实现会把波形高度替换为麦克风 RMS 音量，
没有人说话时自然收缩为低电平。

默认单段上限 25 分钟，达到上限后自动关闭当前段并开始下一段，保证 ESP32 RAM、文件大小
和上传重试可控。

## 复用 Mobile 会议录音和文稿库

硬件会议不是普通 `incoming` 语音消息。Hub 的设备网关将按移动端的可续传会议录音
协议代办以下调用，并使用该设备配对得到的 Hub 用户/租户身份授权：

1. 创建会议：`POST /api/mobile/meeting-recordings`；
2. 上传 1 MiB 分片：`PUT /api/mobile/meeting-recordings/{recordingId}/chunks/{index}`，每片带 `X-Chunk-SHA256`；
3. 结束时提交：`POST /api/mobile/meeting-recordings/{recordingId}/complete`（片数、SHA-256、时长）；
4. 请求处理：`POST /api/mobile/meeting-recordings/{recordingId}/process`，默认 `mode: "minutes"`；
5. 查询进度：`GET /api/mobile/meeting-recordings/{recordingId}`。

这正是移动 App 使用的上传、断点续传和处理方式。Hub 完成转写/纪要后会自动调用
`mobileStoreMeetingResultDocuments`，把转写和会议纪要保存为 Mobile 文稿库草稿；结果中
包含 `transcript_draft_id` 与 `minutes_draft_id`。手机端、GUI 和后续 IM 分享均引用同一份
文稿库记录，不生成第二份“硬件专属”文稿。

设备下行收到 `meeting_result` 时仅显示标题、处理状态和纪要首屏；“打开全文”交给
Mobile 文稿库或 GUI。原始音频默认不自动发往 IM，只有用户在 GUI/Mobile 中明确分享时才
附带。

## 隐私与可靠性

- 录音开始前必须有可见的确认状态；不能通过远程指令静默开始录音。
- Hub token 仅限单设备，后续持久化时应支持吊销；设备丢失后立即在 Hub 吊销。
- 音频采用 16 kHz、16-bit、单声道 PCM WAV，分段上传并用事件 ID 去重。
- 上传失败时设备保留未确认片段，屏幕显示“Retry upload”；用户可重试或明确丢弃。
- 会议结束前不调用 AI 总结；这样可避免把零散口述当作正式会议内容。
- Hub 负责持久化上传会话和文稿库结果，设备只保存仍未被 Hub 确认的音频片段；Hub 重启
  后可继续已有 `recordingId`，与移动端的断点续传语义一致。

## 实施依赖

当前项目的 Hub/GUI 中转和 Mobile 会议后端已经具备。下一步是在设备网关中加入以上
Mobile API 的内部代理（不向 ESP 泄露 Mobile 登录凭据），然后完成 EchoEar-2ST 的 ES7210
麦克风与扬声器 I2S 引脚确认、`board_port_capture_wav()`、1 MiB 分段缓存及长按/确认状态机。
