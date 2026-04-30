#!/usr/bin/env python3
"""Generate Chinese reference audio using official MeloTTS."""
import os, json, numpy as np, torch

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")

from melo import models as melo_models
from melo.text import cleaned_text_to_sequence
from melo.text.cleaner import clean_text

config_path = os.path.join(model_dir, "config.json")
ckpt_path = os.path.join(model_dir, "checkpoint.pth")

with open(config_path, encoding='utf-8') as f:
    config = json.load(f)
ckpt = torch.load(ckpt_path, map_location="cpu", weights_only=False)
sd = ckpt.get("model", ckpt)
mc = config["model"]
dc = config["data"]

model = melo_models.SynthesizerTrn(
    n_vocab=len(config["symbols"]),
    spec_channels=dc.get("n_fft", 2048) // 2 + 1,
    segment_size=config["train"]["segment_size"] // dc["hop_length"],
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
    num_languages=config.get("num_languages", 8),
    num_tones=config.get("num_tones", 16),
)
model.load_state_dict(sd, strict=False)
model.eval()

import scipy.io.wavfile as wavfile
sr = dc.get("sampling_rate", 44100)

# Test 1: Chinese sentence using MeloTTS official G2P
texts = [
    ("你好世界", "ZH"),
    ("今天天气不错", "ZH"),
    ("Hello world", "EN"),
]

for text, lang in texts:
    print(f"\n=== '{text}' ({lang}) ===")
    try:
        norm_text, phone, tone, word2ph = clean_text(text, lang)
        phone_ids = cleaned_text_to_sequence(phone, tone, lang)
        print(f"  norm: {norm_text}")
        print(f"  phones: {phone[:30]}...")
        print(f"  phone_ids ({len(phone_ids)}): {phone_ids[:20]}...")
        print(f"  tones ({len(tone)}): {tone[:20]}...")

        T = len(phone_ids)
        x = torch.LongTensor([phone_ids])
        x_lengths = torch.LongTensor([T])
        sid = torch.LongTensor([0])
        tone_t = torch.LongTensor([tone])

        from melo.text import language_id_map
        lang_id = language_id_map.get(lang, 0)
        lang_ids = [0] * T
        for i in range(1, T, 2):
            lang_ids[i] = lang_id
        language = torch.LongTensor([lang_ids])

        bert = torch.zeros(1, 1024, T)
        ja_bert = torch.zeros(1, 768, T)

        with torch.no_grad():
            audio, _, _, _ = model.infer(
                x, x_lengths, sid, tone_t, language, bert, ja_bert,
                noise_scale=0.667, length_scale=1.0, noise_scale_w=0.0, sdp_ratio=0.0,
            )

        audio_np = audio.squeeze().cpu().numpy()
        print(f"  audio: {len(audio_np)} samples, max={abs(audio_np).max():.4f}")

        fname = f"ref_zh_{text[:4].replace(' ','_')}.wav"
        audio_int16 = np.clip(audio_np * 32767, -32768, 32767).astype(np.int16)
        wavfile.write(os.path.join(outdir, fname), sr, audio_int16)
        print(f"  saved: {fname}")

    except Exception as e:
        print(f"  ERROR: {e}")
        import traceback
        traceback.print_exc()
