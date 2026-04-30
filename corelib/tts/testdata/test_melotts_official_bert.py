#!/usr/bin/env python3
"""Use MeloTTS's own BERT integration to generate audio with proper BERT."""
import os, numpy as np, torch
import scipy.io.wavfile as wavfile

outdir = os.path.dirname(os.path.abspath(__file__))

# Use MeloTTS's built-in BERT feature extraction
from melo.text.chinese_bert import get_bert_feature
from melo.text.cleaner import clean_text
from melo.text import cleaned_text_to_sequence

# Load model
import json
config_path = os.path.join(outdir, "melotts-zh-config.json")
with open(config_path, encoding='utf-8') as f:
    config = json.load(f)

from huggingface_hub import hf_hub_download
from melo import models as melo_models
ckpt_path = hf_hub_download(repo_id="myshell-ai/MeloTTS-Chinese", filename="checkpoint.pth")
ckpt = torch.load(ckpt_path, map_location="cpu", weights_only=False)
sd = ckpt.get("model", ckpt)
mc = config["model"]; dc = config["data"]

model = melo_models.SynthesizerTrn(
    n_vocab=len(config["symbols"]),
    spec_channels=dc.get("n_fft", 2048) // 2 + 1, segment_size=32,
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

sr = dc.get("sampling_rate", 44100)
sid = 1

texts = ["你好世界", "今天天气不错", "我们一起来写代码吧"]

for text in texts:
    print(f"\n'{text}':")
    norm_text, phone, tone, word2ph = clean_text(text, "ZH")
    phone_ids, tone_ids, lang_ids = cleaned_text_to_sequence(phone, tone, "ZH")
    T = len(phone_ids)

    # Get BERT features using MeloTTS's own function
    try:
        bert_feat = get_bert_feature(norm_text, word2ph, device="cpu")
        print(f"  BERT feature: {list(bert_feat.shape)}")
        bert_1024 = bert_feat.unsqueeze(0)  # [1, 1024, T]
    except Exception as e:
        print(f"  BERT error: {e}, using zeros")
        bert_1024 = torch.zeros(1, 1024, T)

    x = torch.LongTensor([phone_ids])
    x_lengths = torch.LongTensor([T])
    sid_t = torch.LongTensor([sid])
    tone_t = torch.LongTensor([tone_ids])
    language_t = torch.LongTensor([lang_ids])
    ja_bert = torch.zeros(1, 768, T)

    with torch.no_grad():
        audio, _, _, _ = model.infer(
            x, x_lengths, sid_t, tone_t, language_t, bert_1024, ja_bert,
            noise_scale=0.667, length_scale=1.0, noise_scale_w=0.8, sdp_ratio=0.0,
        )

    a = audio.squeeze().cpu().numpy()
    print(f"  Audio: {len(a)/sr:.2f}s, max={abs(a).max():.4f}")

    safe = text[:6]
    wavfile.write(os.path.join(outdir, f"bert_official_{safe}.wav"), sr,
                  np.clip(a*32767,-32768,32767).astype(np.int16))
    print(f"  → bert_official_{safe}.wav")
