#!/usr/bin/env python3
"""Generate reference audio using ZH model with manual inference (no torchaudio)."""
import os, json, numpy as np, torch

outdir = os.path.dirname(os.path.abspath(__file__))

from melo import models as melo_models

config_path = os.path.join(outdir, "melotts-zh-config.json")
with open(config_path, encoding='utf-8') as f:
    config = json.load(f)

# Find ZH checkpoint
from huggingface_hub import hf_hub_download
ckpt_path = hf_hub_download(repo_id="myshell-ai/MeloTTS-Chinese", filename="checkpoint.pth")
ckpt = torch.load(ckpt_path, map_location="cpu", weights_only=False)
sd = ckpt.get("model", ckpt)

mc = config["model"]
dc = config["data"]

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

symbols = config["symbols"]
sym2id = {s: i for i, s in enumerate(symbols)}

# 你好世界 with ZH symbol table
phones = ['_', 'n', 'i', 'h', 'ao', 'sh', 'ir', 'j', 'ie', '_']
tones =  [0,   2,   2,   3,   3,    4,    4,   4,   4,   0]  # Python's tones (with sandhi)
phone_ids = [sym2id.get(p, 0) for p in phones]
T = len(phone_ids)

print(f"phone_ids: {phone_ids}")
print(f"tones: {tones}")

x = torch.LongTensor([phone_ids])
x_lengths = torch.LongTensor([T])
sid = torch.LongTensor([1])  # ZH speaker = 1
tone = torch.LongTensor([tones])
language = torch.LongTensor([[0] * T])  # ZH = 0
bert = torch.zeros(1, 1024, T)
ja_bert = torch.zeros(1, 768, T)

with torch.no_grad():
    audio, _, _, _ = model.infer(
        x, x_lengths, sid, tone, language, bert, ja_bert,
        noise_scale=0.667, length_scale=1.0, noise_scale_w=0.0, sdp_ratio=0.0,
    )

audio_np = audio.squeeze().cpu().numpy()
print(f"Audio: {len(audio_np)} samples, max={abs(audio_np).max():.4f}, std={audio_np.std():.4f}")

import scipy.io.wavfile as wavfile
sr = dc.get("sampling_rate", 44100)
wavfile.write(os.path.join(outdir, "ref_zh_model_nihao.wav"), sr,
              np.clip(audio_np * 32767, -32768, 32767).astype(np.int16))
print(f"Saved: ref_zh_model_nihao.wav ({sr} Hz)")
