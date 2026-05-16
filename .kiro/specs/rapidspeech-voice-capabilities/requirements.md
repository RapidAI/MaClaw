# RapidSpeech.cpp 语音能力完善

## 概述

完善 RapidSpeech.cpp 项目的三大语音能力：ASR（语音识别）、TTS（语音合成）、声纹对比/识别。

## 当前状态

### 已完成
- ASR: SenseVoice-small 完整实现（Encode + CTC Decode），支持中英日韩粤 5 语种
- 声纹 Embedding: ECAPA-TDNN 完整实现（Load/Encode/GetEmbedding），192 维 embedding
- Server: `/v1/asr` 和 `/v1/speaker-embed` 端点已可用
- 转换脚本: `convert_hf_to_gguf.py`（SenseVoice）、`convert_ecapa_tdnn.py`（ECAPA-TDNN）
- Hub 集成: 声纹注册（Enroll）+ 1:N 识别（Identify）+ cosine similarity
- Mobile: Flutter 录音 → 上传 → 声纹注册/管理

### 待实现
- TTS: C API 空壳（`rs_push_text` / `rs_get_audio_output` 直接 return 0）
- 声纹对比: Server 缺少 `/v1/speaker-verify` 端点（1:1 对比）
- Python binding: 只有 `asr_offline`，缺少 speaker_embed 和 TTS
- FunASR-nano: 骨架已有但 Decode 不完整

## 需求

### REQ-1: 声纹对比（Speaker Verification）

ACCEPTANCE CRITERIA:
- [ ] Server 新增 `POST /v1/speaker-verify` 端点
- [ ] 接收两个 WAV 文件（multipart form），分别提取 ECAPA-TDNN embedding
- [ ] 计算 cosine similarity，返回 `{"score": float, "same_speaker": bool}`
- [ ] 默认阈值 0.5，支持通过 query param `threshold` 自定义
- [ ] 新增 C++ example: `rs-speaker-verify.cpp`
- [ ] 新增 Python binding: `SpeakerVerifier` 类

### REQ-2: OpenVoice2 TTS（语音合成 + 音色克隆）

ACCEPTANCE CRITERIA:
- [ ] 新增 OpenVoice2 模型架构: `openvoice2.h/.cpp`
- [ ] 实现 `ISpeechModel` 接口的 TTS 流程
- [ ] 基础 TTS: 文本 → VITS encoder → flow decoder → 音频输出
- [ ] 音色克隆（可选）: reference audio → tone color encoder → embedding → 注入 decoder
- [ ] C API 补齐: `rs_push_text` / `rs_get_audio_output` 实际实现
- [ ] 新增 `rs_push_reference_audio()` 用于音色克隆场景
- [ ] 转换脚本: `scripts/convert_openvoice2.py`（PyTorch → GGUF）
- [ ] Server `/v1/tts` 端点对接 OpenVoice2 后端
- [ ] 不传 reference_audio 时使用默认音色

### REQ-3: Python Binding 补齐

ACCEPTANCE CRITERIA:
- [ ] `SpeakerEmbedder` 类: 加载 ECAPA-TDNN 模型，提取 embedding
- [ ] `SpeakerVerifier` 类: 两段音频 → similarity score
- [ ] `TTSSynthesizer` 类: 文本 → 音频，可选 reference audio
- [ ] Python example: `speaker-verify.py`, `tts-synthesize.py`

### REQ-4: Server 端点完善

ACCEPTANCE CRITERIA:
- [ ] `/v1/speaker-verify` — 1:1 声纹对比
- [ ] `/v1/tts` — 文本合成语音（已有 handler，需对接后端）
- [ ] 所有端点返回标准 JSON 格式，错误时返回 `{"error": "message"}`

## 模型选择

| 能力 | 模型 | 参数量 | 特点 |
|------|------|--------|------|
| ASR | SenseVoice-small | ~234M | 已实现，5 语种，效果好 |
| 声纹 | ECAPA-TDNN (SpeechBrain) | ~6M | 已实现，192 维，EER ~0.8% |
| TTS | OpenVoice2 (MeloTTS base + Tone Converter) | ~50M | 待实现，支持音色克隆 |

## 约束

- 所有模型使用 GGUF 格式，通过 ggml 推理
- 音色克隆为可选功能，不传 reference audio 时使用默认音色
- 文本前端（G2P/phonemizer）需要 C++ 内置简化版，不依赖 Python
- 保持与现有 `ISpeechModel` 接口的兼容性
- CI 构建需要在 GitHub Actions 中通过

## 执行优先级

1. 声纹对比（ECAPA-TDNN 已 ready，最快交付）
2. OpenVoice2 TTS（骨架 + 转换脚本 + C API）
3. Python Binding 补齐
