#!/usr/bin/env python3
"""
Test MeloTTS ZH with real bert-base-chinese embeddings.
Compare: zero BERT vs real BERT.
"""
import os, json, numpy as np, torch
from melo import models as melo_models
from melo.text.cleaner import clean_text
from melo.text import cleaned_text_to_sequence
import scipy.io.wavfile as wavfile

outdir = os.path.dirname(os.path.abspath(__file__))
config_path = os.path.join(outdir, "melotts-zh-config.json")

with open(config_path, encoding='utf-8') as f:
    config = json.load(f)

from huggingface_hub import hf_hub_download
ckpt_path = hf_hub_download(repo_id="myshell-ai/MeloTTS-Chinese", filename="checkpoint.pth")
ckpt = torch.load(ckpt_path, map_location="cpu", weights_only=False)
sd = ckpt.get("model", ckpt)
mc = config["model"]; dc = config["data"]

model = melo_models.SynthesizerTrn(
    n_vocab=len(config["symbols"]),
    spec_channels=dc.get("n_fft", 2048) // 2 + 1,
    segment_size=32,
    inter_channels=mc["inter_channels"], hidden_channels=mc["hidden_channels"],
    filter_channels=mc["filter_channels"], n_heads=mc["n_heads"],
    n_layers=mc["n_layers"], kernel_size=mc["kernel_size"],
    p_dropout=mc["p_dropout"], resblock=mc["resblock"],
    resblock_kernel_sizes=mc["resblock_kernel_sizes"],
    resblock_dilation_sizes=mc["resblock_dilation_sizes"],
    upsample_rates=mc["upsample_rates"],
    upsample_initial_channel=mc["upsample_initial_channel"],
    upsample_kernel_sizes=mc["upsample_kernel_sizes"],
    n_speakers=dc["n_speakers"], gin_channels=mc.get("gin_channels", 256),
    use_sdp=True, n_flow_layer=4, n_layers_trans_flow=3,
    use_transformer_flow=True,
    num_languages=config.get("num_languages", 4),
    num_tones=config.get("num_tones", 11),
)
model.load_state_dict(sd, strict=False)
model.eval()

# Load the correct BERT model for MeloTTS ZH
print("Loading chinese-roberta-wwm-ext-large (1024-dim)...")
from transformers import AutoTokenizer, AutoModelForMaskedLM
bert_model_id = "hfl/chinese-roberta-wwm-ext-large"
bert_tokenizer = AutoTokenizer.from_pretrained(bert_model_id)
bert_model = AutoModelForMaskedLM.from_pretrained(bert_model_id)
bert_model.eval()
print(f"BERT loaded: {sum(p.numel() for p in bert_model.parameters())/1e6:.0f}M params")

sr = dc.get("sampling_rate", 44100)
sid = 1

texts = [
    "你好世界",
    "今天天气不错",
    "我们一起来写代码吧",
    "你好，欢迎使用MacLaw。今天天气不错。",
]

for text in texts:
    print(f"\n=== '{text}' ===")

    # G2P
    norm_text, phone, tone, word2ph = clean_text(text, "ZH")
    phone_ids, tone_ids, lang_ids = cleaned_text_to_sequence(phone, tone, "ZH")
    T = len(phone_ids)

    x = torch.LongTensor([phone_ids])
    x_lengths = torch.LongTensor([T])
    sid_t = torch.LongTensor([sid])
    tone_t = torch.LongTensor([tone_ids])
    language_t = torch.LongTensor([lang_ids])

    # --- Version 1: Zero BERT ---
    bert_zero = torch.zeros(1, 1024, T)
    ja_bert_zero = torch.zeros(1, 768, T)

    with torch.no_grad():
        audio_zero, _, _, _ = model.infer(
            x, x_lengths, sid_t, tone_t, language_t, bert_zero, ja_bert_zero,
            noise_scale=0.667, length_scale=1.0, noise_scale_w=0.8, sdp_ratio=0.0,
        )
    a0 = audio_zero.squeeze().cpu().numpy()
    print(f"  Zero BERT: {len(a0)/sr:.2f}s, max={abs(a0).max():.4f}")

    safe = text[:6].replace('，','').replace('。','')
    wavfile.write(os.path.join(outdir, f"bert_test_zero_{safe}.wav"), sr,
                  np.clip(a0*32767,-32768,32767).astype(np.int16))

    # --- Version 2: Real BERT ---
    with torch.no_grad():
        bert_inputs = bert_tokenizer(text, return_tensors="pt")
        bert_outputs = bert_model(**bert_inputs, output_hidden_states=True)
        bert_hidden = bert_outputs.hidden_states[-1]  # last layer [1, seq_bert, 1024]

    seq_bert = bert_hidden.shape[1]
    bert_dim = bert_hidden.shape[2]
    print(f"  BERT hidden: [{seq_bert}, {bert_dim}]")

    # Align BERT tokens to phoneme positions using word2ph
    bert_aligned = torch.zeros(1, bert_dim, T)
    pos = 0
    for i, n_ph in enumerate(word2ph):
        bert_idx = min(i + 1, seq_bert - 1)  # +1 for [CLS]
        for j in range(n_ph):
            if pos < T:
                bert_aligned[0, :, pos] = bert_hidden[0, bert_idx, :]
            pos += 1

    ja_bert_zero2 = torch.zeros(1, 768, T)

    with torch.no_grad():
        audio_bert, _, _, _ = model.infer(
            x, x_lengths, sid_t, tone_t, language_t, bert_aligned, ja_bert_zero2,
            noise_scale=0.667, length_scale=1.0, noise_scale_w=0.8, sdp_ratio=0.0,
        )
    a1 = audio_bert.squeeze().cpu().numpy()
    print(f"  Real BERT: {len(a1)/sr:.2f}s, max={abs(a1).max():.4f}")

    wavfile.write(os.path.join(outdir, f"bert_test_real_{safe}.wav"), sr,
                  np.clip(a1*32767,-32768,32767).astype(np.int16))

    print(f"  Volume ratio: {abs(a1).max() / max(abs(a0).max(), 1e-6):.1f}x")

print("\nDone! Compare bert_test_zero_*.wav vs bert_test_real_*.wav")
