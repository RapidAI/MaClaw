# RapidSpeech.cpp 语音能力完善 — 任务清单

## Phase 1: 声纹对比 (最快交付)

- [x] 1. Server 新增 `/v1/speaker-verify` 端点
  - [x] 1.1 在 `server/rs_server.cpp` 中新增 `handle_speaker_verify` handler
  - [x] 1.2 解析 multipart form 中的 `audio1` 和 `audio2` WAV 文件
  - [x] 1.3 复用 speaker-embed 的 ECAPA-TDNN 上下文，分别提取两个 embedding
  - [x] 1.4 实现 cosine similarity 计算
  - [x] 1.5 返回 JSON: `{"score", "same_speaker", "threshold"}`
  - [x] 1.6 支持 query param `threshold` (默认 0.5)
  - [x] 1.7 注册路由 `svr.Post("/v1/speaker-verify", ...)`

- [x] 2. C API 新增 `rs_speaker_verify()`
  - [x] 2.1 在 `include/rapidspeech.h` 中声明
  - [x] 2.2 在 `rapidspeech_c.cpp` 中实现

- [x] 3. 新增 example `rs-speaker-verify.cpp`
  - [x] 3.1 加载 ECAPA-TDNN 模型，读取两个 WAV，提取 embedding 并计算相似度
  - [x] 3.2 CMakeLists.txt 中添加构建目标

- [x] 4. Python Binding — SpeakerEmbedder + SpeakerVerifier
  - [x] 4.1 在 `pybind_rapidspeech.cpp` 中新增 `SpeakerEmbedder` 类
  - [x] 4.2 在 `pybind_rapidspeech.cpp` 中新增 `SpeakerVerifier` 类
  - [x] 4.3 新增 Python example: `speaker-verify.py`

## Phase 2: OpenVoice2 TTS

- [x] 5. 转换脚本 `convert_openvoice2.py`
  - [x] 5.1 加载 OpenVoice2 PyTorch checkpoint
  - [x] 5.2 提取 base TTS 权重 + tone color converter 权重
  - [x] 5.3 写入两个 GGUF 文件，支持 FP16/Q8_0 量化

- [x] 6. 文本前端 `text_frontend.h/.cpp`
  - [x] 6.1 中文拼音转换 + 英文 G2P
  - [x] 6.2 Phoneme → ID 映射表

- [x] 7. OpenVoice2 模型架构 `openvoice2.h/.cpp`
  - [x] 7.1 实现 `ISpeechModel` 接口
  - [x] 7.2 GGUF 模型加载 (base + converter)
  - [x] 7.3 Text Encoder + Duration Predictor + Flow Decoder + Vocoder 前向推理
  - [x] 7.4 Tone Color Encoder + Style Transfer (可选)

- [x] 8. C API 补齐 TTS 实现
  - [x] 8.1 `rs_push_text()` 实际实现
  - [x] 8.2 `rs_get_audio_output()` 实际实现
  - [x] 8.3 新增 `rs_push_reference_audio()` 声明和实现

- [x] 9. Server `/v1/tts` 对接 OpenVoice2
  - [x] 9.1 修改 TTS handler 对接实际后端
  - [x] 9.2 支持可选 `reference_audio` (音色克隆)

## Phase 3: Python Binding 补齐 + CI

- [x] 10. TTSSynthesizer Python 类
  - [x] 10.1 在 `pybind_rapidspeech.cpp` 中新增
  - [x] 10.2 新增 Python example: `tts-synthesize.py`

- [x] 11. CI 和文档更新
  - [x] 11.1 更新 `.github/workflows/rapidspeech.yml`
  - [x] 11.2 更新 README
