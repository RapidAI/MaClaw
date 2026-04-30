# TTS 纯 Go 推理引擎

## 1. 概述

MacLaw 内置的纯 Go 语音合成引擎，用于中文语音回复。不依赖 ONNX Runtime、CGo 或 Python。

- **模型**：Piper VITS — xiao_ya 中文女声（medium 品质）
- **架构**：VITS (Variational Inference with adversarial learning for end-to-end Text-to-Speech)
- **权重格式**：GGUF（从 ONNX 转换，fp32，~60MB）
- **采样率**：22050 Hz，单声道 16-bit PCM
- **语言**：中文（普通话），支持中英混合文本（英文转中文谐音）

## 2. 模型选型

### 2.1 候选模型评估

| 模型 | 架构 | 中文支持 | 纯 Go 可行性 | 问题 |
|------|------|---------|-------------|------|
| MeloTTS (MyShell AI) | VITS 变体 | ✅ | ❌ | 中文需要 1.3GB BERT 模型做韵律增强，不可接受 |
| Piper (rhasspy) | VITS | ✅ | ✅ | 无 BERT 依赖，拼音音素，模型 60MB |
| sherpa-onnx | VITS/MMS | ✅ | ❌ | 依赖 ONNX Runtime C 库 |

### 2.2 选定模型：Piper xiao_ya

- **来源**：[rhasspy/piper](https://github.com/rhasspy/piper) 项目的中文模型
- **模型 ID**：`zh_CN-xiao_ya-medium`
- **音素系统**：拼音音素（lexicon.txt，20901 个汉字映射）
- **无 BERT 依赖**：音素直接从拼音转换，不需要上下文语言模型
- **参数量**：~15M（fp32 约 60MB）

### 2.3 模型架构参数

```
HiddenChannels:       192
InterChannels:        192
FilterChannels:       768
NHeads:               2
NLayers:              6
KernelSize:           3
UpsampleRates:        [8, 8, 4]
UpsampleInitChannel:  256
ResblockKernelSizes:  [3, 5, 7]
ResblockDilationSizes: [[1,2], [2,6], [3,12]]
SampleRate:           22050
```

## 3. 推理流水线

```
输入: 中文文本

Step 1: G2P (文本 → 音素)
  text → pinyin (lexicon 查表 + tone sandhi) → phoneme IDs

Step 2: Text Encoder
  phoneme_emb = Embedding[phoneme_ids] * sqrt(hidden)
  x = 6层 FFT Transformer(phoneme_emb)
  m_p, logs_p = split(Conv1D_proj(x))

Step 3: Duration Prediction (SDP — Stochastic Duration Predictor)
  durations = SDP(x, noise_scale_w)    # 神经样条流
  T_mel = sum(durations)

Step 4: Expand Prior
  attn = generate_path(durations)       # 硬单调对齐矩阵
  m_p, logs_p = expand(m_p, logs_p, attn)

Step 5: Sample Latent
  z_p = m_p + randn * exp(logs_p) * noise_scale

Step 6: Flow Decoder (reverse)
  z = ResidualCouplingBlock(z_p, reverse=True)  # 4层 WaveNet coupling

Step 7: HiFi-GAN Vocoder
  audio = HiFiGAN(z)                    # 3级上采样(8×8×4=256) + 9个 ResBlock
  output: [T_mel * 256] PCM samples @ 22050 Hz
```

## 4. 核心组件实现

### 4.1 G2P（文本前端）

| 文件 | 功能 |
|------|------|
| `piper_g2p.go` | G2P 入口：lexicon 查表 + tone sandhi 规则 |
| `piper_g2p_en.go` | 英文/数字支持：常见词中文谐音 + 字母拼读 + 数字读法 |
| `piper_lexicon.go` | Lexicon 加载器（20901 个汉字 → 音素映射） |

**中英混合处理流程**：
1. 逐字符扫描，按类型分段：中文字符 / 英文单词 / 数字 / 标点
2. 中文：lexicon 查表 → 拼音音素 → tone sandhi
3. 英文单词：先查常见词表（~50 个技术词 + 品牌名），命中则用中文谐音（如 "Python" → 派森）；未命中则逐字母拼读（如 "API" → 诶屁爱）
4. 数字：逐位读中文（如 "2024" → 二零二四）
5. 标点：映射到停顿音素

**Tone Sandhi 规则**：
- 不 (bù) 在四声前变二声
- 一 (yī) 在四声前变二声，在一/二/三声前变四声
- 三声连读：两个三声连续时，前一个变二声

### 4.2 Text Encoder

| 文件 | 功能 |
|------|------|
| `text_encoder.go` | 6 层 FFT Transformer + 投影层 |

每层结构：`LayerNorm → MultiHeadAttention → Residual → LayerNorm → Conv1D FFN → Residual`

### 4.3 Stochastic Duration Predictor (SDP)

| 文件 | 功能 |
|------|------|
| `piper_sdp.go` | 完整 SDP：DDSConv + ConvFlow + ElementwiseAffine |
| `piper_spline.go` | Piecewise Rational Quadratic Spline 逆变换 |

SDP 是整个引擎中最复杂的组件：
- **DDSConv**：Dilated Depth-Separable Conv（sep_conv → norm → GELU → 1×1 → norm → GELU → +residual）
- **ConvFlow**：神经样条流，使用 piecewise rational quadratic spline 的逆变换
- **ElementwiseAffine**：可学习的仿射变换（exp(-logs) 缩放）

### 4.4 Flow Decoder

| 文件 | 功能 |
|------|------|
| `piper_flow.go` | ResidualCouplingBlock：4 层 WaveNet coupling（reverse 模式） |

ONNX 图验证的关键发现：
- Flow 层处理顺序为 6→4→2→0（不是 0→2→4→6）
- WaveNet 所有层 dilation=1（从 ONNX 图确认）

### 4.5 HiFi-GAN Vocoder

| 文件 | 功能 |
|------|------|
| `piper_hifigan.go` | Piper HiFi-GAN：3 级上采样 + 9 个 ResBlock |
| `hifigan.go` | 通用 HiFi-GAN + Conv1D dilated 实现 |

关键实现细节（从 ONNX 图验证）：
- **ResBlock 残差位置**：residual 在 LeakyReLU **之前**取（不是之后）
- **Dilation 模式**：kSize=3→[1,2], kSize=5→[2,6], kSize=7→[3,12]
- **上采样率**：[8, 8, 4]，总倍率 256

### 4.6 基础算子

| 文件 | 功能 |
|------|------|
| `ops.go` | Conv1D, ConvTranspose1D, LeakyReLU, ReLU, Exp, Ceil, Sigmoid, Flip, GeneratePath |
| `conv_simd.go` | SIMD 加速的 Conv1D（vek32.Dot） |
| `conv_kernel_transpose.go` | Kernel 布局转置（为 SIMD 优化准备） |

### 4.7 权重加载

| 文件 | 功能 |
|------|------|
| `piper_weights.go` | GGUF 权重加载 + 张量名映射 |

权重文件：`piper-xiao_ya-zh-fp32.gguf`（60MB，由 `convert_xiao_ya_to_gguf.py` 从 ONNX 转换）

## 5. SIMD 优化

### 5.1 性能分析

HiFi-GAN Vocoder 占推理时间的 ~95%，其中：

| 函数 | 占比 | 说明 |
|------|------|------|
| `conv1DDilatedRange` | 60% | ResBlock 中的 dilated conv，kSize=3/5/7 |
| `conv1DRangeStride1` | 25% | 非 dilated conv，kSize=3/5/7 |
| `convT1DByOutCh` | 9% | ConvTranspose1D 上采样 |
| `conv1DKernel1Range` | 4% | 1×1 conv（矩阵乘法） |

### 5.2 优化策略：输入转置 + vek32.Dot

**核心问题**：Conv1D 的内层循环在 `inCh` 维度上累加，但输入布局 `[inCh, T]` 使得同一时间点的不同通道值在内存中不连续（步长为 T）。

**解决方案**：
1. **一次性转置输入**：`[inCh, T]` → `[T, inCh]`，使每个时间点的通道值连续
2. **SIMD 点积**：对每个 kernel tap 使用 `vek32.Dot`（AVX2/NEON 自动选择）做 inCh 维度的累加

```
转置前（strided access）:
  for ic in 0..inCh:
    sum += input[ic*T + pos] * kernel[oc*inCh*kSize + ic*kSize + k]  // 步长 T

转置后（contiguous access）:
  inputT = transpose(input)  // 一次性 O(inCh*T)
  for k in 0..kSize:
    sum += vek32.Dot(kernTap[k], inputT[pos*inCh : (pos+1)*inCh])  // SIMD
```

### 5.3 SIMD 触发条件

- `inCh >= 16` 时启用 SIMD（vek32.Dot 的最小有效长度）
- `inCh < 16` 时回退到标量实现（小通道数时转置开销不值得）

### 5.4 Benchmark 结果

测试环境：AMD Ryzen 7 7840H, 16 核, Windows amd64

**单算子 Benchmark**（inCh=128, outCh=128, T=256）：

| 算子 | 标量 (ms) | SIMD (ms) | 加速比 |
|------|----------|-----------|--------|
| Conv1D kSize=1 | 14.1 | 1.0 | **14x** |
| Conv1D stride=1 kSize=3 | 14.3 | 2.0 | **7.1x** |
| Conv1D dilated kSize=3 dil=2 | 13.6 | 2.5 | **5.4x** |

**端到端 TTS Benchmark**（5 句中文，总音频 ~10 秒）：

| 指标 | 优化前 | SIMD 优化后 | 提升 |
|------|--------|------------|------|
| 平均 RTF | ~1.0 | **0.44** | **2.3x** |
| 实时倍率 | ~1.0x | **2.4x** | — |
| 短文本 RTF（4 字） | 0.82 | **0.37** | 2.2x |
| 长文本 RTF（10 字） | 1.22 | **0.44** | 2.8x |

### 5.5 精度影响

SIMD 使用 float32 累加（标量版本使用 float64），最大差异 < 0.000001。对 16-bit PCM 输出无可感知影响。

### 5.6 未使用的优化

- **LeakyReLU SIMD**：需要分配临时 buffer 做 `max(x, slope*x)`，分配开销大于 SIMD 收益，保持标量
- **自定义汇编（.s 文件）**：vek32 已内置 AVX2/SSE/NEON 调度，无需手写汇编
- **Kernel 预转置**：`TransposeConv1DKernel()` 已实现，可在模型加载时转置 kernel 布局，消除运行时的 kernel tap 提取。Benchmark 显示额外收益 ~5%，暂未集成

## 6. 文件清单

### 6.1 核心源码

| 文件 | 行数 | 功能 |
|------|------|------|
| `piper.go` | ~225 | 模型入口、Synthesize 流水线 |
| `piper_g2p.go` | — | G2P：lexicon 查表 + tone sandhi |
| `piper_g2p_en.go` | — | 英文/数字 G2P：常见词谐音 + 字母拼读 |
| `piper_lexicon.go` | — | Lexicon 加载器 |
| `piper_weights.go` | — | GGUF 权重加载 |
| `piper_hifigan.go` | — | Piper HiFi-GAN vocoder |
| `piper_flow.go` | — | Flow decoder (ResidualCouplingBlock) |
| `piper_sdp.go` | — | Stochastic Duration Predictor |
| `piper_spline.go` | — | Rational quadratic spline 逆变换 |
| `piper_duration.go` | — | Duration 工具函数 |
| `piper_duration_cache.go` | — | Trigram duration 缓存 |
| `piper_duration_mlp.go` | — | MLP duration 预测器 |
| `text_encoder.go` | — | FFT Transformer encoder |
| `ops.go` | — | Conv1D, ConvTranspose1D, 基础算子 |
| `hifigan.go` | — | 通用 HiFi-GAN + dilated conv |
| `conv_simd.go` | — | SIMD 加速 Conv1D（vek32.Dot） |
| `conv_kernel_transpose.go` | — | Kernel 布局转置 |

### 6.2 MeloTTS 遗留代码（已弃用，保留供参考）

以下文件是最初为 MeloTTS 实现的，后因 BERT 依赖问题弃用。Piper 引擎复用了其中的 encoder 层、flow 层和基础算子。

| 文件 | 说明 |
|------|------|
| `melotts.go` | MeloTTS 模型入口（已弃用） |
| `weights.go` | MeloTTS GGUF 权重加载 |
| `flow.go` | MeloTTS TransformerCouplingBlock |
| `duration_predictor.go` | MeloTTS 确定性 Duration Predictor |
| `bert_embedding.go` | BERT embedding 投影（MeloTTS 专用） |
| `g2p.go` / `g2p_zh.go` / `g2p_en.go` | MeloTTS G2P |
| `phoneme_table.go` | MeloTTS 音素表 |

### 6.3 模型文件

| 文件 | 大小 | 说明 |
|------|------|------|
| `testdata/piper-xiao_ya-zh-fp32.gguf` | 60MB | Piper xiao_ya 模型权重 |
| `testdata/vits-piper-zh_CN-xiao_ya-medium/lexicon.txt` | ~1MB | 汉字→音素映射（20901 条） |
| `testdata/duration_trigram_cache.json` | — | Trigram duration 缓存 |
| `testdata/duration_bigram_cache.json` | — | Bigram duration 缓存 |
| `testdata/duration_unigram_cache.json` | — | Unigram duration 缓存 |
| `testdata/duration_mlp.bin` | — | MLP duration 预测器权重 |

### 6.4 转换脚本

| 文件 | 说明 |
|------|------|
| `testdata/convert_xiao_ya_to_gguf.py` | ONNX → GGUF 转换脚本 |
| `testdata/implement_sdp_v3.py` | SDP Python 参考实现（验证用） |
| `testdata/transforms_vits.py` | VITS spline 参考实现 |

### 6.5 测试

| 文件 | 说明 |
|------|------|
| `conv_simd_test.go` | SIMD 正确性测试 + Benchmark |
| `ops_test.go` | 基础算子测试 |
| `g2p_test.go` | G2P 测试 |
| `cmd/piper_test/main.go` | 端到端合成 + RTF 测量 |

## 7. 使用方式

```go
import "github.com/RapidAI/CodeClaw/corelib/tts"

// 加载模型
model, err := tts.NewPiper("piper-xiao_ya-zh-fp32.gguf", "lexicon.txt")

// 文本合成
wav, err := model.SynthesizeToWAV("你好世界")
os.WriteFile("output.wav", wav, 0644)

// 或分步调用
audio, err := model.SynthesizeText("今天天气不错")
wavBytes := tts.EncodeWAV(audio, model.HP.SampleRate)
```

## 8. 已知限制

1. **英文为中文谐音**：英文单词通过中文拼音近似发音（如 "Python" → "派森"），不是原生英文发音。常见词有专门映射，生僻词按字母逐个拼读
2. **无多音字消歧**：lexicon 查表返回最常见读音，部分多音字可能不准
3. **固定说话人**：单说话人模型，无法切换音色
4. **CPU only**：纯 Go 实现，无 GPU 加速
5. **RTF ~0.44**：2.4 倍实时，对长文本可能有感知延迟
