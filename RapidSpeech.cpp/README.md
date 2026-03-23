<div align="center">
<img src="assets/rapid-speech.png" alt="RapidSpeech Logo" />
</div>

English | [简体中文](./README-CN.md)

<a href="https://huggingface.co/RapidAI/RapidSpeech" target="_blank"><img src="https://img.shields.io/badge/🤗-Hugging Face-blue"></a>
<a href="https://www.modelscope.cn/models/RapidAI/RapidSpeech/files?version=main" target="_blank"><img src="https://img.shields.io/badge/ModelScope-blue"></a>
<a href="https://github.com/RapidAI/RapidSpeech.cpp/stargazers"><img src="https://img.shields.io/github/stars/RapidAI/RapidSpeech.cpp?color=ccf"></a>

# RapidSpeech.cpp 🎙️

**RapidSpeech.cpp** is a high-performance, **edge-native speech intelligence framework** built on top of **ggml**.  
It aims to provide **pure C++**, **zero-dependency**, and **on-device inference** for large-scale ASR (Automatic Speech Recognition) and TTS (Text-to-Speech) models.

------

## 🌟 Key Differentiators

While the open-source ecosystem already offers powerful cloud-side frameworks such as **vLLM-omni**, as well as mature on-device solutions like **sherpa-onnx**, **RapidSpeech.cpp** introduces a new generation of design choices focused on edge deployment.

### 1. vs. vLLM: Edge-first, not cloud-throughput-first

- **vLLM**
    - Designed for data centers and cloud environments
    - Strongly coupled with Python and CUDA
    - Maximizes GPU throughput via techniques such as PageAttention

- **RapidSpeech.cpp**
    - Designed specifically for **edge and on-device inference**
    - Optimized for **low latency, low memory footprint, and lightweight deployment**
    - Runs on embedded devices, mobile platforms, laptops, and even NPU-only systems
    - **No Python runtime required**

### 2. vs. sherpa-onnx: Deeper control over the inference stack

| Aspect | sherpa-onnx (ONNX Runtime) | **RapidSpeech.cpp (ggml)** |
| --- | --- | --- |
| **Memory Management** | Managed internally by ORT, relatively opaque | **Zero runtime allocation** — memory is fully planned during graph construction to avoid edge-side OOM |
| **Quantization** | Primarily INT8, limited support for ultra-low bit-width | **Full K-Quants family** (Q4_K / Q5_K / Q6_K), significantly reducing bandwidth and memory usage while preserving accuracy |
| **GPU Performance** | Relies on execution providers with operator mapping overhead | **Native backends** (`ggml-cuda`, `ggml-metal`) with speech-specific optimizations, outperforming generic `onnxruntime-gpu` |
| **Deployment** | Requires shared libraries and external config files | **Single binary deployment** — model weights and configs are fully encapsulated in **GGUF** |

------

## 📦 Model Support

**Automatic Speech Recognition (ASR)**
- [x] SenseVoice-small
- [ ] FunASR-nano
- [ ] Qwen3-ASR
- [ ] FireRedASR2

**Text-to-Speech (TTS)**
- [x] OpenVoice2 (MeloTTS base + tone color converter, streaming)
- [ ] CosyVoice3
- [ ] Qwen3-TTS

**Speaker Verification**
- [x] ECAPA-TDNN (speaker embedding + cosine similarity verification)

------

## 🏗️ Architecture Overview

RapidSpeech.cpp is not just an inference wrapper — it is a full-featured speech application framework:

- **Core Engine**  
  A `ggml`-based computation backend supporting mixed-precision inference from INT4 to FP32.

- **Architecture Layer**  
  A plugin-style model construction and loading system, with planned support for FunASR-nano, CosyVoice, Qwen3-TTS, and more.

- **Business Logic Layer**  
  Built-in ring buffers, VAD (voice activity detection), text frontend processing (e.g., phonemization), and multi-session management.

------

## 🚀 Core Features

- [ ] **Extreme Quantization**: Native support for 4-bit, 5-bit, and 6-bit quantization schemes to match diverse hardware constraints.
- [ ] **Zero Dependencies**: Implemented entirely in C/C++, producing a single lightweight binary.
- [ ] **GPU / NPU Acceleration**: Customized CUDA and Metal backends optimized for speech models.
- [ ] **Unified Model Format**: Both ASR and TTS models use an extended **GGUF** format.

------

## 🛠️ Quick Start (WIP)

### Download Models

Models are available on:

- 🤗 Hugging Face: https://huggingface.co/RapidAI/RapidSpeech
- ModelScope: https://www.modelscope.cn/models/RapidAI/RapidSpeech

### Build & Run

```bash
git clone https://github.com/RapidAI/RapidSpeech.cpp
cd RapidSpeech.cpp
git submodule sync && git submodule update --init --recursive
cmake -B build -DRS_BUILD_SERVER=ON
cmake --build build --config Release
```

### ASR (Speech Recognition)

```bash
./build/rs-asr-offline \
  -m /path/to/SenseVoice/sense-voice-small-fp32.gguf \
  -w /path/to/test_sample_rate_16k.wav
```

### Speaker Verification

```bash
./build/rs-speaker-verify \
  -m /path/to/ecapa-tdnn.gguf \
  -a /path/to/speaker1.wav \
  -b /path/to/speaker2.wav \
  -t 0.5
```

### TTS (Text-to-Speech)

```bash
# Server mode
./build/rs-server --model /path/to/openvoice2.gguf --port 8080

# Synthesize via API
curl -X POST http://localhost:8080/v1/tts \
  -H "Content-Type: application/json" \
  -d '{"text": "Hello world", "stream": true}'
```

### Python Bindings

Build with Python support:

```bash
cmake -B build -DRS_ENABLE_PYTHON=ON
cmake --build build --config Release
```

```python
import rapidspeech

# ASR
asr = rapidspeech.asr_offline("/path/to/model.gguf")
asr.push_audio(pcm_data)
asr.process()
print(asr.get_text())

# Speaker Verification
sv = rapidspeech.speaker_verifier("/path/to/ecapa-tdnn.gguf")
result = sv.verify(audio1, audio2, threshold=0.5)
print(f"Score: {result['score']}, Same speaker: {result['same_speaker']}")

# TTS
tts = rapidspeech.tts_synthesizer("/path/to/openvoice2.gguf")
pcm = tts.synthesize("Hello world")

# TTS with voice cloning
tts.set_reference(reference_pcm, sample_rate=16000)
pcm = tts.synthesize("Hello in cloned voice")

# Streaming TTS
chunks = tts.synthesize_streaming("Hello world")
for chunk in chunks:
    play_audio(chunk)
```

### REST API Endpoints

| Endpoint | Method | Description |
|---|---|---|
| `/v1/asr` | POST | Speech recognition (WAV upload) |
| `/v1/tts` | POST | Text-to-speech (JSON body) |
| `/v1/speaker-verify` | POST | Speaker verification (two WAV uploads) |
| `/v1/speaker-embed` | POST | Speaker embedding extraction |

------

## 🤝 Contributing

If you are interested in the following areas, we welcome your PRs or participation in discussions:

- Adapting more models to the framework.
- Refining and optimizing the project architecture.