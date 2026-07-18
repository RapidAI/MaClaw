# 移动端会议录音 Worker 契约

移动端会议录音由 Hub 保存和编排，转写、解码、纪要生成均在 Worker
边界执行。Hub 不调用 FFmpeg，也不会把原始音频送入 LLM。

## 配置

为 Hub 进程配置以下命令即可使用命令侧车：

```text
MACLAW_MEETING_TRANSCRIBE_COMMAND=<转写 Worker 命令>
MACLAW_MEETING_MINUTES_COMMAND=<纪要 Worker 命令>
```

仓库提供无需 FFmpeg 的 WAV/CoreLib 转写 Worker，可构建并配置为：

```text
go build -o meeting_asr_worker ./hub/cmd/meeting_asr_worker
MACLAW_MEETING_ASR_MODEL=/absolute/path/sensevoice-small-q8.gguf
MACLAW_MEETING_TRANSCRIBE_COMMAND=/absolute/path/meeting_asr_worker
```

该 Worker 仅接受 `audio/wav`，在进程内调用 `corelib/asr.Manager.TranscribeWAV`。
Hub 的命令适配器为每个会议启动一次命令，因此模型会在每次转写后卸载；若需要
持续复用模型、限制并发或使用 GPU，应实现 `MeetingTranscriptionWorker` 并通过
`SetMeetingRecordingWorkers` 注入一个常驻 Worker/队列消费者。
纪要仍通过 `MACLAW_MEETING_MINUTES_COMMAND` 接入部署所选的 LLM/队列 Worker；Hub
只向它传递已验证的逐字稿，不传递原始音频。

也可以在嵌入 Hub 的程序中调用 `SetMeetingRecordingWorkers` 注入同等的
Worker 实现。可选说话人分段通过 `SetMeetingSpeakerSegmentationWorker`
注入；没有可靠结果时必须返回空分段，不能猜测说话人身份。

手机在开始录音前会请求 `GET /api/mobile/meeting-recordings/capabilities`：
`minutes` 仅在转写和纪要 Worker 都可用时出现，`transcript` 仅在转写 Worker
可用时出现，`keep` 始终可用。这让部署缺少 Worker 时仍能安全归档，且不会
让用户长时间录音后才发现所选处理模式不可执行。

能力协商只是体验优化：Hub 的 `process` API 也会强制执行同一规则。请求未配置
的 `minutes` 或 `transcript` 模式会返回 `409 PROCESSING_MODE_UNAVAILABLE`，且不
改变已上传录音的状态或重试计数。

## 转写 Worker

Hub 通过标准输入传入一行 JSON：

```json
{"audio_path":"/durable/mobile/meeting-recordings/meeting-123/recording.wav","content_type":"audio/wav"}
```

手机端固定录制 16kHz、单声道、16-bit PCM WAV。Worker 必须自行读取
`audio_path` 并验证 WAV 容器，再将其交给 CoreLib ASR；CoreLib 会完成已支持
WAV 的校验、混音与重采样。该路径不依赖 Hub 转码、FFmpeg 或外部 AAC 解码器。
Worker 必须在标准输出仅写入：

```json
{"transcript":"经验证的完整逐字稿"}
```

空逐字稿或非 JSON 输出会被标记为 `ASR_TRANSCRIPTION_FAILED`。不要在输出中
写日志；日志应写 stderr。

## 纪要 Worker

Hub 只将已成功获得的逐字稿交给纪要 Worker：

```json
{"title":"产品评审","purpose":"记录决策与行动项","transcript":"经验证的完整逐字稿"}
```

标准输出必须仅包含：

```json
{"minutes":"会议纪要 Markdown 内容"}
```

空内容或非 JSON 输出会被标记为 `MEETING_MINUTES_FAILED`。`transcript`
模式不会调用纪要 Worker，`keep` 模式不会调用任何 Worker。

## 可靠性与安全边界

- 原始音频最大 512 MiB；协议允许压缩音频最长 24 小时。当前移动端固定使用 16kHz 单声道 16-bit PCM WAV（约 115 MiB/小时），因此 Hub 对该格式限制为最多约 4 小时 39 分钟，避免录制完成后才因体积超过上限失败。每个分片必须带 `X-Chunk-SHA256`（64 位 SHA-256 hex），Hub 会逐片强制校验，并在完成时校验整文件摘要。
- 完成上传时 Hub 会对声明的 m4a/mp4、ADTS AAC 或 WAV 容器做魔数校验；MIME 与实际文件不符会返回 `AUDIO_TYPE_MISMATCH`，该上传会重新开放以便客户端重传正确的音频。
- `complete` 会原子关闭分片上传并进入短暂的 `finalizing` 状态；此时重复 complete 返回 `409 FINALIZING`，后续分片被拒绝，避免组装时收到迟到分片。Hub 重启会安全恢复为 `uploading`，客户端可重试 complete。
- Worker 应把 `audio_path` 当作只读的受控本地路径，禁止将其当作 shell 片段拼接执行。
- Hub 对单次处理设置 30 分钟超时；Worker 应响应取消并避免遗留子进程。
- Hub 默认保留原始音频 30 天，之后只删除音频，逐字稿、纪要和文档引用仍保留。处理进行中时不能删除原始音频；应等待任务进入 ready 或 failed 后再操作。
- 删除整条会议记录同样只能在 ready 或 failed 后执行；上传、finalizing、处理中的记录返回 `409 RECORDING_IN_USE`，防止 Worker 读取的音频或即将写入的文档结果被并发删除。
- 处理失败且原始音频仍在保留期内时，用户可重试；音频已删除或到期时，任何处理模式（包括 `keep` 归档）都会立即返回 `409 AUDIO_MISSING_FOR_RETRY`，并将记录保留为 failed，避免无意义地启动 Worker 或伪造归档成功。
