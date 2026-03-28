# RapidSpeech 服务部署文档

## 1. 模型准备

### 支持的模型

| 模型文件 | 类型 | 用途 | 转换脚本 |
|---------|------|------|---------|
| moonshine-tiny.gguf | ASR | 英文语音识别 (tiny) | convert_moonshine.py |
| moonshine-base.gguf | ASR | 英文语音识别 (base) | convert_moonshine.py |
| moonshine-base-zh.gguf | ASR | 中英文语音识别 | convert_moonshine.py |
| sense-voice-small-fp32.gguf | ASR | 多语言语音识别 (SenseVoice) | HuggingFace 预转换 |
| openvoice2-base.gguf | TTS | 语音合成 + 声音克隆 | convert_openvoice2.py |
| ecapa-tdnn.gguf | Speaker | 说话人声纹提取/验证 | convert_ecapa_tdnn.py |
| embeddinggemma-300M-Q8_0.gguf | Embedding | Google Gemma 文本嵌入 | convert_gemma_embedding.py |

### 模型转换

所有模型需从 HuggingFace 格式转换为 GGUF 格式：

```bash
pip install gguf safetensors torch

# ASR: Moonshine
python scripts/convert_moonshine.py \
    --model-dir models/moonshine-tiny \
    --output models/gguf/moonshine-tiny.gguf

# ASR: Moonshine 中文 (F16 减小体积)
python scripts/convert_moonshine.py \
    --model-dir models/moonshine-base-zh \
    --output models/gguf/moonshine-base-zh.gguf \
    --out-type f16

# TTS: OpenVoice2
python scripts/convert_openvoice2.py \
    --model-dir models/openvoice2-base \
    --output models/gguf/openvoice2-base.gguf

# Speaker: ECAPA-TDNN
python scripts/convert_ecapa_tdnn.py \
    --model-dir models/ecapa-tdnn \
    --output models/gguf/ecapa-tdnn.gguf

# Embedding: Google Gemma
python scripts/convert_gemma_embedding.py \
    --model-dir models/embedding-gemma-300M \
    --output models/gguf/embeddinggemma-300M-Q8_0.gguf
```

SenseVoice 模型可直接从 HuggingFace 下载预转换的 GGUF：
- https://huggingface.co/RapidAI/RapidSpeech

### 目录结构

```
models/gguf/
├── moonshine-tiny.gguf        # ASR 英文小模型
├── moonshine-base.gguf        # ASR 英文大模型
├── moonshine-base-zh.gguf     # ASR 中英文模型
├── sense-voice-small-fp32.gguf # ASR 多语言 (SenseVoice)
├── openvoice2-base.gguf       # TTS 语音合成
├── ecapa-tdnn.gguf            # 说话人声纹模型
└── embeddinggemma-300M-Q8_0.gguf # Google Gemma 文本嵌入
```

## 2. 编译

```bash
cd RapidSpeech.cpp
mkdir -p build && cd build

# 编译（启用服务端）
cmake .. -DCMAKE_BUILD_TYPE=Release -DRS_BUILD_SERVER=ON
cmake --build . --config Release -j$(nproc)
```

产物：
- `rs-asr-offline` — 离线 ASR 命令行工具
- `rs-server` — HTTP REST API 服务（支持 ASR / TTS / Speaker）

## 3. 启动服务

### 命令行参数

```
rs-server [options]

  --asr-model <path>   ASR 模型 (GGUF)
  --tts-model <path>   TTS 模型 (GGUF)
  --spk-model <path>   说话人模型 (GGUF)
  --host <addr>        监听地址 (默认: 0.0.0.0)
  --port <num>         监听端口 (默认: 8080)
  --threads <num>      推理线程数 (默认: 4)
  --gpu <true|false>   GPU 加速 (默认: true)
```

至少需要指定一个模型。可同时加载多个模型提供多种服务。

### 启动示例

```bash
# 仅 ASR（Moonshine 中文）
./rs-server --asr-model models/gguf/moonshine-base-zh.gguf --port 8080

# 仅 ASR（SenseVoice 多语言）
./rs-server --asr-model models/gguf/sense-voice-small-fp32.gguf --port 8080

# 仅 TTS
./rs-server --tts-model models/gguf/openvoice2-base.gguf --port 8080

# ASR + TTS + 声纹（全功能）
./rs-server \
    --asr-model models/gguf/moonshine-base-zh.gguf \
    --tts-model models/gguf/openvoice2-base.gguf \
    --spk-model models/gguf/ecapa-tdnn.gguf \
    --port 8080 --threads 4

# CPU 模式
./rs-server --asr-model models/gguf/moonshine-tiny.gguf --gpu false
```

## 4. API 说明

### 4.1 健康检查

```
GET /health
```

```json
{"status": "ok"}
```

### 4.2 语音识别 (ASR)

需加载 `--asr-model`。

```
POST /asr
Content-Type: multipart/form-data
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| file | binary | 是 | WAV 音频 (16-bit PCM 或 32-bit float, 建议 16kHz 单声道) |

响应：
```json
{"text": "你好啊,今天是个美好的一天"}
```

示例：
```bash
curl -X POST http://localhost:8080/asr -F "file=@test.wav"
```

```python
import requests
r = requests.post("http://localhost:8080/asr", files={"file": open("test.wav","rb")})
print(r.json()["text"])
```

### 4.3 语音合成 (TTS)

需加载 `--tts-model`。

```
POST /tts
Content-Type: multipart/form-data
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| text | string | 是 | 要合成的文本 |
| reference_audio | binary | 否 | 参考音频 WAV（用于声音克隆） |

响应：`audio/wav` 二进制流（16kHz 16-bit PCM WAV）

示例：
```bash
# 基本合成
curl -X POST http://localhost:8080/tts -F "text=你好世界" -o output.wav

# 声音克隆
curl -X POST http://localhost:8080/tts \
    -F "text=你好世界" \
    -F "reference_audio=@speaker_ref.wav" \
    -o output.wav
```

```python
import requests
r = requests.post("http://localhost:8080/tts",
                   data={"text": "你好世界"})
with open("output.wav", "wb") as f:
    f.write(r.content)
```

### 4.4 声纹提取 (Speaker Embedding)

需加载 `--spk-model`。

```
POST /speaker/embed
Content-Type: multipart/form-data
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| file | binary | 是 | WAV 音频 |

响应：
```json
{
    "dim": 192,
    "embedding": [0.01234567, -0.02345678, ...]
}
```

示例：
```bash
curl -X POST http://localhost:8080/speaker/embed -F "file=@voice.wav"
```

### 4.5 说话人验证 (Speaker Verify)

需加载 `--spk-model`。比较两段音频是否为同一说话人。

```
POST /speaker/verify
Content-Type: multipart/form-data
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| audio1 | binary | 是 | 第一段 WAV 音频 |
| audio2 | binary | 是 | 第二段 WAV 音频 |
| threshold | float | 否 | 判定阈值 (默认: 0.5) |

响应：
```json
{
    "score": 0.876543,
    "same_speaker": true,
    "threshold": 0.500000
}
```

示例：
```bash
curl -X POST http://localhost:8080/speaker/verify \
    -F "audio1=@person_a.wav" \
    -F "audio2=@person_b.wav" \
    -F "threshold=0.6"
```

## 5. 音频格式要求

所有接口统一要求 WAV 格式输入：
- 采样率：16000 Hz（推荐）
- 声道：单声道
- 位深：16-bit 整数 或 32-bit 浮点

转换命令：
```bash
ffmpeg -i input.m4a -ar 16000 -ac 1 -f wav output.wav
```

## 6. 离线命令行工具

```bash
./rs-asr-offline --model models/gguf/moonshine-base-zh.gguf --wav test.wav
./rs-asr-offline --model models/gguf/moonshine-tiny.gguf --wav test.wav --threads 8
```

## 7. 性能参考

CPU 测试 (无 GPU)：

| 模型 | 音频时长 | 解码时间 | RTF |
|------|---------|---------|-----|
| moonshine-tiny (EN) | 4.4s | 1.4s | 0.32 |
| moonshine-base-zh (ZH) | 7.7s | 4.9s | 0.63 |
