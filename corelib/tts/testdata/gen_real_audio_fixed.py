#!/usr/bin/env python3
"""Generate real audio with correct lang_id pattern (blank=0, phone=lang)."""
import os, sys, json, math
import numpy as np
import torch

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")

from melo import models as melo_models
from melo import commons as melo_commons

config_path = os.path.join(model_dir, "config.json")
ckpt_path = os.path.join(model_dir, "checkpoint.pth")

with open(config_path, encoding='utf-8') as f:
    config = json.load(f)

ckpt = torch.load(ckpt_path, map_location="cpu", weights_only=False)
sd = ckpt.get("model", ckpt)

mc = config["model"]
dc = config["data"]
symbols = config["symbols"]

model = melo_models.SynthesizerTrn(
    n_vocab=len(symbols),
    spec_channels=dc.get("n_fft", 2048) // 2 + 1,
    segment_size=config["train"]["segment_size"] // dc["hop_length"],
    inter_channels=mc["inter_channels"],
    hidden_channels=mc["hidden_channels"],
    filter_channels=mc["filter_channels"],
    n_heads=mc["n_heads"],
    n_layers=mc["n_layers"],
    kernel_size=mc["kernel_size"],
    p_dropout=mc["p_dropout"],
    resblock=mc["resblock"],
    resblock_kernel_sizes=mc["resblock_kernel_sizes"],
    resblock_dilation_sizes=mc["resblock_dilation_sizes"],
    upsample_rates=mc["upsample_rates"],
    upsample_initial_channel=mc["upsample_initial_channel"],
    upsample_kernel_sizes=mc["upsample_kernel_sizes"],
    n_speakers=dc["n_speakers"],
    gin_channels=mc.get("gin_channels", 256),
    use_sdp=True,
    n_flow_layer=4,
    n_layers_trans_flow=3,
    use_transformer_flow=True,
    num_languages=config.get("num_languages", 8),
    num_tones=config.get("num_tones", 16),
)
model.load_state_dict(sd, strict=False)
model.eval()

# Phone IDs for "Hello" with blanks
phone_ids = [0, 49, 0, 127, 0, 70, 0, 80, 0]
T = len(phone_ids)

x = torch.LongTensor([phone_ids])
x_lengths = torch.LongTensor([T])
sid = torch.LongTensor([0])
tone = torch.LongTensor([[0] * T])

# Key fix: lang_id pattern — blank=0, phone=2(EN)
lang_ids = [0] * T
for i in range(1, T, 2):
    lang_ids[i] = 2  # EN
language = torch.LongTensor([lang_ids])

bert = torch.zeros(1, 1024, T)
ja_bert = torch.zeros(1, 768, T)

print(f"lang_ids: {lang_ids}")

with torch.no_grad():
    audio, attn, y_mask, (z, z_p, m_p, logs_p) = model.infer(
        x, x_lengths, sid, tone, language, bert, ja_bert,
        noise_scale=0.667, length_scale=1.0, noise_scale_w=0.0, sdp_ratio=0.0,
    )

audio_np = audio.squeeze().cpu().numpy()
print(f"Audio: {len(audio_np)} samples, mean={audio_np.mean():.4f}, "
      f"std={audio_np.std():.4f}, max={abs(audio_np).max():.4f}")

# Dump intermediates
def dump(name, t):
    d = t.squeeze(0).cpu().numpy().astype(np.float32)
    d.tofile(os.path.join(outdir, f"{name}.bin"))
    print(f"  {name}: shape={list(d.shape)}, mean={d.mean():.4f}, std={d.std():.4f}")

dump("ref_fixed_z_p", z_p)
dump("ref_fixed_z", z)
dump("ref_fixed_m_p", m_p)
dump("ref_fixed_logs_p", logs_p)

# Save WAV
import scipy.io.wavfile as wavfile
sr = dc.get("sampling_rate", 44100)
audio_int16 = np.clip(audio_np * 32767, -32768, 32767).astype(np.int16)
wavfile.write(os.path.join(outdir, "ref_fixed_hello.wav"), sr, audio_int16)
print(f"\nSaved ref_fixed_hello.wav ({sr} Hz)")

# Also with noise_scale=0
with torch.no_grad():
    audio0, _, _, (z0, z_p0, m_p0, logs_p0) = model.infer(
        x, x_lengths, sid, tone, language, bert, ja_bert,
        noise_scale=0.0, length_scale=1.0, noise_scale_w=0.0, sdp_ratio=0.0,
    )
audio0_np = audio0.squeeze().cpu().numpy()
print(f"\nnoise_scale=0: mean={audio0_np.mean():.4f}, std={audio0_np.std():.4f}, max={abs(audio0_np).max():.4f}")
dump("ref_fixed_m_p_ns0", m_p0)
dump("ref_fixed_logs_p_ns0", logs_p0)
dump("ref_fixed_z_ns0", z0)
wavfile.write(os.path.join(outdir, "ref_fixed_hello_ns0.wav"), sr,
              np.clip(audio0_np * 32767, -32768, 32767).astype(np.int16))
print("Saved ref_fixed_hello_ns0.wav")
