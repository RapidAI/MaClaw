#!/usr/bin/env python3
"""Generate reference audio using MeloTTS ZH model with official inference."""
import os, json, numpy as np, torch
from melo import models as melo_models
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

from melo.text.cleaner import clean_text
from melo.text import cleaned_text_to_sequence, language_id_map, language_tone_start_map

sr = dc.get("sampling_rate", 44100)
sid = 1  # ZH speaker

texts = [
    ("你好世界", "ZH"),
    ("今天天气不错", "ZH"),
    ("欢迎使用MacLaw", "ZH"),
    ("我们一起来写代码吧", "ZH"),
    ("你好，欢迎使用MacLaw。今天天气不错，我们一起来写代码吧。", "ZH"),
]

for text, lang in texts:
    print(f"\n'{text}':")
    try:
        norm_text, phone, tone, word2ph = clean_text(text, lang)
        phone_ids, tone_ids, lang_ids = cleaned_text_to_sequence(phone, tone, lang)
        T = len(phone_ids)

        x = torch.LongTensor([phone_ids])
        x_lengths = torch.LongTensor([T])
        sid_t = torch.LongTensor([sid])
        tone_t = torch.LongTensor([tone_ids])
        language_t = torch.LongTensor([lang_ids])
        bert = torch.zeros(1, 1024, T)
        ja_bert = torch.zeros(1, 768, T)

        with torch.no_grad():
            audio, _, _, _ = model.infer(
                x, x_lengths, sid_t, tone_t, language_t, bert, ja_bert,
                noise_scale=0.667, length_scale=1.0, noise_scale_w=0.8, sdp_ratio=0.0,
            )

        audio_np = audio.squeeze().cpu().numpy()
        print(f"  {len(audio_np)} samples ({len(audio_np)/sr:.2f}s), max={abs(audio_np).max():.4f}")

        safe = text[:8].replace('，','').replace('。','')
        fname = f"melotts_zh_ref_{safe}.wav"
        wavfile.write(os.path.join(outdir, fname), sr,
                      np.clip(audio_np * 32767, -32768, 32767).astype(np.int16))
        print(f"  → {fname}")
    except Exception as e:
        print(f"  ERROR: {e}")
