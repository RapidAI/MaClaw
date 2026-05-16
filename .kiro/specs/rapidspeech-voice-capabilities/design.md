# RapidSpeech.cpp 语音能力完善 — 技术设计

## 架构概览

```
RapidSpeech.cpp/
├── rapidspeech/src/arch/
│   ├── sensevoice.cpp/h          # ASR (已完成)
│   ├── ecapa_tdnn.cpp/h          # 声纹 Embedding (已完成)
│   └── openvoice2.cpp/h          # TTS + 音色克隆 (新增)
├── rapidspeech/src/core/
│   ├── rs_processor.cpp/h        # 统一处理器 (需扩展 TTS 流程)
│   └── rs_context.cpp/h          # 上下文管理 (需扩展)
├── rapidspeech/src/c_api/
│   └── rapidspeech_c.cpp         # C API (需补齐 TTS 实现)
├── rapidspeech/src/frontend/
│   ├── audio_processor.cpp/h     # 音频前端 (已完成)
│   └── text_frontend.cpp/h       # 文本前端 G2P (新增)
├── server/
│   └── rs_server.cpp             # HTTP Server (需新增端点)
├── scripts/
│   └── convert_openvoice2.py     # 模型转换 (新增)
├── examples/
│   ├── rs-asr-offline.cpp        # ASR 示例 (已完成)
│   └── rs-speaker-verify.cpp     # 声纹对比示例 (新增)
└── rapidspeech/python/
    └── pybind_rapidspeech.cpp    # Python Binding (需扩展)
```

## 模块设计

### 1. 声纹对比 (Speaker Verification)

复用已有的 ECAPA-TDNN 模型，新增 cosine similarity 计算。

#### Server 端点: `POST /v1/speaker-verify`

请求: multipart/form-data
- `audio1`: WAV 文件 (必需)
- `audio2`: WAV 文件 (必需)
- `threshold`: float (可选, 默认 0.5)

响应:
```json
{
  "score": 0.85,
  "same_speaker": true,
  "threshold": 0.5
}
```

实现流程:
1. 解析两个 WAV 文件
2. 分别通过 ECAPA-TDNN 提取 192 维 embedding
3. 计算 cosine similarity: `score = dot(e1, e2) / (norm(e1) * norm(e2))`
4. 与阈值比较，返回结果

#### C API 新增

```c
// 声纹对比: 两段音频 → similarity score
float rs_speaker_verify(rs_context_t* ctx,
                        const float* audio1, int n1,
                        const float* audio2, int n2,
                        int sample_rate);
```

### 2. OpenVoice2 TTS

#### 模型架构

OpenVoice2 由两部分组成:

**Base TTS (MeloTTS/VITS 变体)**:
- Text Frontend → Phoneme IDs
- Text Encoder (Transformer)
- Duration Predictor
- Flow Decoder → Mel Spectrogram
- HiFi-GAN Vocoder → Waveform

**Tone Color Converter (可选)**:
- Reference Audio → Mel Spectrogram
- Tone Color Encoder → Style Embedding
- Style Transfer → 修改 Base TTS 输出的音色

#### GGUF 模型文件

需要两个 GGUF 文件:
- `openvoice2-base.gguf` — Base TTS
- `openvoice2-converter.gguf` — Tone Color Converter (可选)

#### 文本前端 (Text Frontend)

新增 `text_frontend.h/.cpp`:
- 中文: 内置拼音表 + 声调标注
- 英文: 内置 CMU 发音词典子集 + 规则 G2P
- 输出: phoneme ID 序列

#### ISpeechModel 接口实现

```cpp
class OpenVoice2Model : public ISpeechModel {
public:
    bool Load(const std::unique_ptr<rs_context_t>& ctx, ggml_backend_t backend) override;
    std::shared_ptr<RSState> CreateState() override;
    bool Encode(const std::vector<float>& input, RSState& state, ggml_backend_sched_t sched) override;
    bool Decode(RSState& state, ggml_backend_sched_t sched) override;
    // TTS specific
    bool PushText(const char* text, const char* language);
    bool PushReferenceAudio(const float* samples, int n_samples, int sample_rate);
};
```

### 3. C API 扩展

补齐现有空壳:
```c
// 已有但空壳 → 实现
int rs_push_text(rs_context_t* ctx, const char* text);
int rs_get_audio_output(rs_context_t* ctx, float** out_pcm);

// 新增
int rs_push_reference_audio(rs_context_t* ctx, const float* samples, int n_samples, int sample_rate);
float rs_speaker_verify(rs_context_t* ctx,
                        const float* audio1, int n1,
                        const float* audio2, int n2,
                        int sample_rate);
```

### 4. Python Binding 扩展

```python
class SpeakerEmbedder:
    def __init__(self, model_path: str)
    def embed(self, wav_path: str) -> np.ndarray  # 192-dim

class SpeakerVerifier:
    def __init__(self, model_path: str)
    def verify(self, wav1: str, wav2: str, threshold: float = 0.5) -> dict

class TTSSynthesizer:
    def __init__(self, base_model: str, converter_model: str = None)
    def synthesize(self, text: str, language: str = "zh") -> np.ndarray
    def set_reference(self, wav_path: str)
```

## 数据流

### 声纹对比
```
WAV1 → AudioProcessor → ECAPA-TDNN → Embedding1 ─┐
                                                    ├→ CosineSimilarity → Score
WAV2 → AudioProcessor → ECAPA-TDNN → Embedding2 ─┘
```

### TTS (无音色克隆, 流式)
```
Text → TextFrontend → PhonemeIDs → TextEncoder → DurationPredictor
  → FlowDecoder (chunk N) → Vocoder → AudioChunk N → 输出
  → FlowDecoder (chunk N+1) → Vocoder → AudioChunk N+1 → 输出
  ...
```

### TTS (有音色克隆, 流式)
```
RefAudio → ToneColorEncoder → StyleEmbedding (缓存)
Text → TextFrontend → PhonemeIDs → TextEncoder → DurationPredictor
  → FlowDecoder (chunk) → StyleTransfer(StyleEmbedding) → Vocoder → AudioChunk → 输出
  ...
```

### TTS 流式设计要点
- `rs_push_text()` 完成文本前端 + encoder，生成全部 hidden states
- 每次 `rs_process()` 从 hidden states 中取一个 chunk，经 flow decoder + vocoder 生成一段音频
- `rs_get_audio_output()` 返回当前 chunk 的音频数据
- `rs_process()` 返回 0 表示所有 chunk 已生成
- Server `/v1/tts` 使用 chunked transfer encoding 流式返回 raw PCM 或 WAV
