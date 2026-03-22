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
- [ ] CosyVoice3
- [ ] Qwen3-TTS

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
cmake -B build
cmake --build build --config Release

./build/rs-asr-offline \
  -m /path/to/SenseVoice/sense-voice-small-fp32.gguf \
  -w /path/to/test_sample_rate_16k.wav
```

------

## 🤝 Contributing

If you are interested in the following areas, we welcome your PRs or participation in discussions:

- Adapting more models to the framework.
- Refining and optimizing the project architecture.