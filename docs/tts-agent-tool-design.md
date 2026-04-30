# TTS Agent 工具设计文档——语音消息发送

## 1. 需求

maclaw agent 在 IM 通道（飞书/企微/QQ/蓝信等）回复用户时，能够将回复内容的摘要转换为语音，以**语音消息**（非文件附件）的形式直接发送给用户。

**关键区分**：
- ✅ 语音消息（voice message）：在聊天界面中显示为可播放的语音气泡，用户点击即播
- ❌ 文件附件（file attachment）：显示为文件卡片，需要下载后播放

## 2. 各 IM 平台语音消息格式调研

### 2.1 飞书（Feishu / Lark）

| 项目 | 说明 |
|------|------|
| 语音消息类型 | `msg_type: "audio"` |
| **上传要求** | `file_type: "opus"`（**必须**，其他格式返回错误码 234001） |
| 音频格式 | OGG Opus 容器（`.ogg`），Opus 编码 |
| 发送流程 | 两步：① `POST /im/v1/files` 上传音频（`file_type=opus`）获取 `file_key` → ② `POST /im/v1/messages` 发送 `msg_type=audio` + `file_key` |
| 当前代码 | `FeishuPlugin.SendFile()` 使用 `lark.MsgFile`（文件消息），**不支持** `lark.MsgAudio`（语音消息）。发送音频文件会显示为文件附件而非语音气泡 |
| 来源 | [飞书开放平台文档](https://open.feishu.cn/document/server-docs/im-v1/message/create) + [feishu-audio-msg skill](https://playbooks.com/skills/openclaw/skills/feishu-audio-msg) 实测确认 |

**关键发现**：飞书的 `go-lark/lark/v2` SDK 有 `lark.MsgFile` 但可能没有 `lark.MsgAudio`。需要直接构造 JSON body 发送 `msg_type=audio`。

### 2.2 企业微信（WeCom）

| 项目 | 说明 |
|------|------|
| 语音消息类型 | `msgtype: "voice"` |
| 音频格式 | AMR 格式（**推荐**），也接受 MP3、WAV |
| 文件限制 | 最大 2MB，最长 60 秒 |
| 发送流程 | 两步：① `uploadMedia(data, "voice", filename)` 获取 `media_id` → ② `sendMedia(chatID, "voice", media_id)` |
| 当前代码 | `Plugin.SendFile()` **已支持**：`strings.HasPrefix(mimeType, "audio/") → mediaType = "voice"` → `uploadMedia` → `sendMedia`。发送 `audio/*` MIME 类型的文件会自动以语音消息形式发送 |
| 来源 | [企业微信开发文档](https://developer.work.weixin.qq.com/document/path/90253) + 代码确认 |

**当前代码已就绪**：只要 `SendFile(ctx, target, b64Data, "voice.wav", "audio/wav")` 即可以语音气泡形式发送。AMR 格式更小但需要额外编码器；WAV/MP3 也能工作。

### 2.3 QQ（QBot — 官方机器人 API v2）

| 项目 | 说明 |
|------|------|
| 语音消息类型 | 富媒体消息 `file_type: 3`（语音） |
| **音频格式** | **silk / wav / mp3 / flac**（官方文档明确列出） |
| 发送流程 | 两步：① `POST /v2/users/{openid}/files` 上传（`file_type=3`, `file_data=base64`）获取 `file_info` → ② `POST /v2/users/{openid}/messages` 发送 `msg_type=7` + `media.file_info` |
| 当前代码 | `hub/internal/qqbot/plugin.go` 的 `SendFile()` **已支持**：`strings.HasPrefix(mimeType, "audio/") → fileType = 3` → `sendC2CMedia()` 完成两步上传+发送 |
| 来源 | [QQ 机器人官方文档 - 富媒体消息](https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/rich-media.html) + 代码确认 |

**当前代码已就绪**：`SendFile(ctx, target, b64Data, "voice.wav", "audio/wav")` 会自动设置 `fileType=3`（语音），QQ 客户端会以语音气泡形式展示。WAV 是官方支持的格式之一，零额外编码。

### 2.4 钉钉（DingTalk）

| 项目 | 说明 |
|------|------|
| 语音消息类型 | `msgKey: "sampleAudio"` |
| **音频格式** | **ogg、amr**（官方文档明确列出） |
| 发送流程 | 两步：① 通过「上传媒体文件」接口上传音频获取 `media_id` → ② 发送 `msgKey=sampleAudio` + `msgParam={"mediaId":"...", "duration":"毫秒"}` |
| 当前代码 | `corelib/dingtalk/gateway.go` 只有 `SendText` 和 `SendMarkdown`，**没有 SendFile / SendMedia / SendAudio**。`Capabilities()` 声明 `SupportsFile: false` |
| 来源 | [钉钉开放平台 - 企业机器人发送消息的消息类型](https://open.dingtalk.com/document/orgapp/types-of-messages-sent-by-robots) + 代码确认 |

**需要新增**：
1. `Gateway.uploadMedia()` — 上传媒体文件获取 `media_id`
2. `Gateway.SendAudio()` — 发送 `sampleAudio` 类型消息
3. 音频格式需要 OGG（Opus/Vorbis）或 AMR。WAV 不在支持列表中。

**注意**：钉钉的 `sampleAudio` 要求 ogg 或 amr 格式，与飞书的 Opus 需求有重叠（OGG Opus 同时满足两者）。

### 2.5 蓝信（Lansenger）

| 项目 | 说明 |
|------|------|
| 语音消息类型 | 蓝信 API 的 `mediaType` 枚举：1=video, 2=image, 3=file。**没有独立的语音类型** |
| 音频格式 | 作为文件（`mediaType=3`）发送 |
| 当前代码 | `Gateway.SendMedia()` 只支持 image/video/file 三种类型，音频会作为文件发送 |
| 来源 | 代码确认 + 蓝信开放平台 SDK |

**结论**：蓝信不支持原生语音消息，只能以文件形式发送 MP3。

### 2.6 Telegram

| 项目 | 说明 |
|------|------|
| 语音消息类型 | `sendVoice` API |
| 音频格式 | OGG Opus（**推荐**，显示为语音气泡），MP3/WAV 也可但显示为音频文件 |
| 来源 | [Telegram Bot API](https://core.telegram.org/bots/api#sendvoice) |

### 2.7 格式总结

| 平台 | 语音气泡格式 | 文件附件格式 | 是否需要专用 API |
|------|------------|------------|----------------|
| **飞书** | OGG Opus（必须） | 任意 | ✅ 需要 `msg_type=audio` + `file_type=opus` 上传 |
| **企微** | AMR / MP3 / WAV | 任意 | ❌ 现有 `SendFile` 已自动路由到 voice |
| **QQ** | silk / wav / mp3 / flac | 任意 | ❌ 现有 `SendFile` 已自动路由到 `file_type=3`（语音） |
| **钉钉** | ogg / amr（需上传获取 mediaId） | xlsx/pdf/zip/rar/doc/docx | ✅ 需要 `sampleAudio` msgKey + 上传媒体文件 |
| **蓝信** | ❌ 不支持 | MP3 / WAV | — |
| **Telegram** | OGG Opus（推荐） | MP3 / WAV | ✅ 需要 `sendVoice` API |

## 3. 音频编码方案

### 3.1 核心问题

TTS 引擎（Piper）输出 PCM float32 @ 22050Hz 单声道。需要编码为各平台接受的格式。

### 3.2 方案对比

| 方案 | 覆盖平台 | 纯 Go | 文件大小（10s 音频） | 复杂度 |
|------|---------|-------|-------------------|--------|
| **A: WAV only** | 企微 ✅ QQ ✅ 飞书 ❌ 蓝信 ✅ | ✅ | ~430KB | 最低 |
| **B: WAV + OGG Opus** | 企微 ✅ QQ ✅ 飞书 ✅ 蓝信 ✅ Telegram ✅ | ⚠️ Opus 编码需要评估 | WAV ~430KB, Opus ~20KB | 中 |
| **C: MP3 only** | 企微 ✅ QQ ✅ 飞书 ❌ 蓝信 ✅ | ❌ 纯 Go MP3 编码器不成熟 | ~80KB | 中 |
| **D: WAV（默认）+ 平台适配** | 全平台 | ✅ 核心 + 按需 | 按平台 | 最灵活 |

### 3.3 推荐方案：D（WAV 默认 + 平台适配层）

**核心层**（`corelib/tts`）：
- `Manager.SynthesizeText(text) → []byte`（WAV）——已有，不改
- 新增 `EncodeWAVToOpus(wav) → []byte`（OGG Opus）——飞书/钉钉/Telegram 专用，依赖系统 ffmpeg
- 新增 `HasOpusEncoder() → bool`——检测 ffmpeg 是否可用

**Opus 编码方案**：使用系统 ffmpeg 做 WAV → OGG Opus 转码。
- 纯 Go Opus 编码器不可用：`pion/opus` 只有解码器；`gotranspile/opus`（libopus 自动转译）编译不过（大量类型错误）
- ffmpeg 方案：零额外 Go 依赖，32kbps voip 模式，10 秒音频 → ~40KB OGG Opus
- 降级策略：ffmpeg 不可用时回退到 WAV（企微/QQ 仍为语音气泡，飞书/钉钉降级为文件附件）

**平台适配层**（`gui/im_tool_tts.go` 或 hub 侧）：
- 根据目标平台选择编码格式
- 飞书 → Opus（必须）
- 企微 → WAV（已支持，零额外开销）
- 蓝信 → WAV 作为文件发送
- Telegram → Opus
- 桌面面板 → WAV（复用现有 `tts:audio` 事件）

### 3.4 Opus 编码器选型

| 库 | 纯 Go | 编码支持 | 成熟度 | 备注 |
|----|-------|---------|--------|------|
| `pion/opus` | ✅ | ⚠️ 主要是解码器 | 活跃（WebRTC 生态） | 编码器可能不完整 |
| `hraban/opus` | ❌ CGo | ✅ 完整 | 成熟 | 依赖 libopus C 库 |
| `gopxl/audio` | ✅ | ✅ MP3 编码 | 较新 | 不支持 Opus |

**务实选择**：

1. **首期**：飞书通道使用 `hraban/opus`（CGo，依赖 libopus）。MacLaw 已经是桌面应用，CGo 在编译时可接受。或者用 `exec.Command("ffmpeg", ...)` 调用系统 ffmpeg 做 WAV→Opus 转换（如果用户安装了 ffmpeg）。
2. **备选**：如果 `pion/opus` 的编码器足够用（需要验证），优先使用纯 Go 方案。
3. **兜底**：飞书通道如果 Opus 编码不可用，降级为文件附件发送 WAV（功能可用但不是语音气泡）。

## 4. 架构设计

### 4.1 数据流

```
Agent 回复文本
  → tts.cleanForSpeech(text)          // 清理 Markdown/URL/代码块
  → tts.truncateRunes(cleaned, 300)   // 截断到 300 rune（~15 秒音频）
  → tts.Manager.SynthesizeText(text)  // PCM → WAV (22050Hz mono 16-bit)
  → platformEncode(wav, platform)     // 按平台编码（WAV/Opus）
  → 通过 IM 通道发送语音消息
```

### 4.2 两种暴露方式

#### 方式 A：Agent 工具（`tts` 工具）

Agent 主动决定何时用语音回复：

```
tts(text="任务已完成，共找到99篇论文并保存到文件中")
```

- 优点：Agent 有自主权，可以根据场景决定是否用语音
- 缺点：增加工具选择负担，Agent 可能滥用或遗忘

#### 方式 B：自动语音摘要（后处理层）

Agent loop 结束后，系统自动判断是否生成语音摘要：

```go
// im_message_handler.go 的 agent loop 结束后
if shouldGenerateVoiceSummary(resp, platform) {
    summary := tts.GenerateVoiceSummary(resp.Text, 300)
    wav := ttsManager.SynthesizeText(summary)
    sendVoiceMessage(userID, wav, platform)
}
```

- 优点：不增加工具数量，不增加 Agent 决策负担
- 缺点：自动判断可能不准确

#### 推荐：方式 A + B 结合

- **方式 A**（`tts` 工具）：Agent 可以主动调用，用于需要语音回复的场景
- **方式 B**（自动摘要）：作为可选的后处理，用户在设置中开启"IM 自动语音摘要"后，每次 Agent 回复自动附带语音版本

### 4.3 `tts` 工具设计

```json
{
  "name": "tts",
  "description": "将文本转换为语音消息发送给用户。适用于：状态通知、简短回复、任务完成汇报等场景。文本会自动清理 Markdown 格式并截断到合适长度。桌面面板播放语音，IM 通道以语音消息（非文件）形式发送。",
  "parameters": {
    "text": {
      "type": "string",
      "description": "要转换为语音的文本内容（中文，最长 300 字，超出自动截断）"
    }
  }
}
```

**工具行为**：
1. `cleanForSpeech(text)` 清理 Markdown
2. `truncateRunes(cleaned, 300)` 截断
3. `Manager.SynthesizeText(cleaned)` 合成 WAV
4. 按平台编码并发送：
   - 桌面面板：`runtime.EventsEmit("tts:audio", base64WAV)`
   - 企微：返回 `[voice_base64|voice.wav|audio/wav|im]` 协议标记
   - 飞书：返回 `[voice_base64|voice.opus|audio/ogg|im]` 协议标记（需要 Opus 编码）
   - 蓝信：返回 `[file_base64|voice.mp3|audio/mpeg|im]` 协议标记（降级为文件）

**工具注册**：
- 放入 `CoreToolNames`（只有 1 个定义，~100 token，开销极小）
- 不需要条件触发——Agent 自己判断何时用语音

### 4.4 IM 通道语音发送增强

#### 4.4.1 飞书：新增 `SendAudio` 方法

```go
// hub/internal/feishu/plugin.go
func (p *FeishuPlugin) SendAudio(ctx context.Context, target im.UserTarget, audioData []byte) error {
    // Step 1: Upload with file_type=opus
    uploadResp := bot.UploadFile(ctx, lark.UploadFileRequest{
        FileType: "opus",  // 关键：必须是 opus
        FileName: "voice.ogg",
        Reader:   bytes.NewReader(audioData),
    })
    fileKey := uploadResp.Data.FileKey

    // Step 2: Send with msg_type=audio
    // go-lark SDK 可能没有 MsgAudio，需要直接构造 JSON
    body := fmt.Sprintf(`{"receive_id":"%s","msg_type":"audio","content":"{\"file_key\":\"%s\"}"}`, openID, fileKey)
    // POST /open-apis/im/v1/messages?receive_id_type=open_id
}
```

#### 4.4.2 企微：已就绪

现有 `SendFile` 已自动将 `audio/*` MIME 类型路由到 `voice` mediaType。无需修改。

#### 4.4.3 蓝信：降级为文件

蓝信不支持语音消息类型，以文件形式发送。

#### 4.4.4 IM Adapter 接口扩展

```go
// hub/internal/im/adapter.go
type CapabilityDeclaration struct {
    // ... 现有字段 ...
    SupportsVoice bool // 新增：支持语音消息（非文件附件）
}
```

### 4.5 自动语音摘要（后处理层）

```go
// gui/im_message_handler.go — agent loop 结束后
func (h *IMMessageHandler) maybeAttachVoiceSummary(userID string, resp *IMAgentResponse, platform string) {
    if !h.isIMChannel(platform) { return }
    if !h.isTTSAutoSummaryEnabled(userID) { return }
    if resp.Error != "" { return }
    if len([]rune(resp.Text)) < 50 { return } // 太短不值得语音

    summary := tts.GenerateVoiceSummary(resp.Text, 300)
    wav, err := h.app.ttsManager.SynthesizeText(summary)
    if err != nil { return }

    // 编码为目标平台格式
    audioData, fileName, mimeType := h.encodeTTSForPlatform(wav, platform)

    // 附加到响应中
    resp.VoiceData = base64.StdEncoding.EncodeToString(audioData)
    resp.VoiceFileName = fileName
    resp.VoiceMimeType = mimeType
}
```

## 5. 实现计划

### Phase 1：核心 TTS 工具（企微 + QQ + 桌面面板）

**不需要 Opus 编码器**，企微和 QQ 都接受 WAV，桌面面板也用 WAV。

1. `gui/im_tool_tts.go`：`toolTTS()` handler
2. `gui/im_tool_definitions.go`：工具定义
3. `gui/tool_registry_builtin.go`：注册
4. `corelib/tool/router.go`：加入 `CoreToolNames`
5. `tui/agent_handler.go`：TUI 侧 dispatch
6. IM 通道语音发送：复用 `send_file` 的 `[file_base64|...|im]` 协议，企微自动路由到 voice，QQ 自动路由到 `file_type=3`（语音）

**验收**：Agent 调用 `tts(text="...")` → 企微/QQ 用户收到语音气泡

### Phase 2：飞书 + 钉钉语音消息（需要 OGG Opus 编码）

飞书要求 OGG Opus，钉钉要求 ogg 或 amr。OGG Opus 同时满足两者。

1. `corelib/tts/opus.go`：WAV → OGG Opus 编码（评估 `pion/opus` 或 `hraban/opus`）
2. `hub/internal/feishu/plugin.go`：新增 `SendAudio()` 方法（`file_type=opus` 上传 + `msg_type=audio` 发送）
3. `corelib/dingtalk/gateway.go`：新增 `uploadMedia()` + `SendAudio()`（上传 ogg 获取 mediaId + `sampleAudio` 发送）
4. `hub/internal/im/adapter.go`：`CapabilityDeclaration` 新增 `SupportsVoice`
5. `hub/internal/im/core.go`：语音消息发送路径（检测 `VoiceData` 字段）

**验收**：Agent 调用 `tts(text="...")` → 飞书/钉钉用户收到语音气泡

### Phase 3：自动语音摘要

1. `gui/im_message_handler.go`：`maybeAttachVoiceSummary()` 后处理
2. `corelib/app_config.go`：`TTSAutoSummaryEnabled` 配置项
3. 前端设置面板：IM 自动语音摘要开关

**验收**：开启设置后，Agent 每次 IM 回复自动附带语音版本

### Phase 4：Telegram + 蓝信优化

1. Telegram：`sendVoice` API + Opus 编码（Phase 2 已有 Opus 编码器）
2. 蓝信：如果蓝信后续支持语音类型，适配

## 6. 已有基础设施复用

| 组件 | 状态 | 复用方式 |
|------|------|---------|
| `corelib/tts.Manager` | ✅ 已有 | 直接调用 `SynthesizeText` |
| `tts.cleanForSpeech()` | ✅ 已有 | 清理 Markdown/URL |
| `tts.truncateRunes()` | ✅ 已有 | 截断到句子边界 |
| `tts.GenerateVoiceSummary()` | ✅ 已有 | 自动摘要生成 |
| `tts.EncodeWAV()` | ✅ 已有 | PCM → WAV |
| 企微 `SendFile` voice 路由 | ✅ 已有 | `audio/*` MIME → voice mediaType |
| QQ `SendFile` 语音路由 | ✅ 已有 | `audio/*` MIME → `file_type=3`（语音） |
| 飞书 `SendFile` | ⚠️ 需增强 | 新增 `SendAudio`（`msg_type=audio`） |
| 钉钉 `Gateway` | ⚠️ 需增强 | 新增 `uploadMedia` + `SendAudio`（`sampleAudio`） |
| `[file_base64\|...\|im]` 协议 | ✅ 已有 | 复用 IM 转发机制 |
| 桌面 `tts:audio` 事件 | ✅ 已有 | 复用前端播放 |

## 7. 风险与缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| Opus 编码器不可用 | 飞书无法发送语音气泡 | Phase 1 先跳过飞书，降级为文件附件 |
| TTS 模型未下载 | 工具调用失败 | 返回友好提示"语音合成模型未安装" |
| 合成耗时（RTF 0.44） | 300 字 ~6 秒音频，合成 ~2.6 秒 | 异步合成，不阻塞 agent loop |
| 语音文件过大 | WAV 10 秒 ~430KB | 企微限制 2MB（~46 秒），足够 |
| Agent 滥用 TTS | 每条消息都发语音 | System prompt 指导：仅在适合语音的场景使用 |

## 8. 新增协议标记

为了区分"语音消息"和"文件附件"，在现有 `[file_base64|...]` 协议基础上新增 `[voice_base64|...]` 标记：

```
[voice_base64|voice.wav|audio/wav|im]{base64data}
```

IM router 检测到 `voice_base64` 标记时：
- 平台支持语音（`SupportsVoice=true`）→ 调用 `SendAudio` / `SendFile(voice)` 发送语音气泡
- 平台不支持语音 → 降级为 `SendFile` 发送文件附件
