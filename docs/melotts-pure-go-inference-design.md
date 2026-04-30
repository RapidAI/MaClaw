# MeloTTS 纯 Go 推理引擎设计文档（已弃用）

> **注意**：MeloTTS 中文模型需要 1.3GB BERT 模型做韵律增强，不适合嵌入式部署。
> 已切换到 Piper VITS (xiao_ya) 模型，无 BERT 依赖，模型仅 60MB。
>
> **当前文档**：[tts-pure-go-inference-engine.md](tts-pure-go-inference-engine.md)

## 弃用原因

MeloTTS 的中文推理路径中，TextEncoder 接受 BERT embedding 作为输入（`bert_proj` 投影层）。
虽然可以传全零跳过 BERT，但实测音质严重退化——韵律平淡、声调不准。
要获得可用的中文语音质量，必须加载 BERT 模型（chinese-roberta-wwm-ext-large，1.3GB），
这对桌面应用不可接受。

MeloTTS 的引擎代码（encoder、flow、HiFi-GAN、基础算子）被 Piper 引擎复用。
