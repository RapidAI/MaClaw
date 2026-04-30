#!/usr/bin/env python3
"""Compare SynthesizerTrn.enc_p output vs manual encoder computation."""
import os, json, math, numpy as np, torch

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")

from melo import models as melo_models

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

phone_ids = [0, 49, 0, 127, 0, 70, 0, 80, 0]
T = len(phone_ids)

x = torch.LongTensor([phone_ids])
x_lengths = torch.LongTensor([T])
sid = torch.LongTensor([0])
tone = torch.LongTensor([[0] * T])
lang_ids = [0] * T
for i in range(1, T, 2):
    lang_ids[i] = 2
language = torch.LongTensor([lang_ids])
bert = torch.zeros(1, 1024, T)
ja_bert = torch.zeros(1, 768, T)

with torch.no_grad():
    g = model.emb_g(sid).unsqueeze(-1)
    g_p = g

    # Run official enc_p
    enc_out, m_p, logs_p, x_mask = model.enc_p(
        x, x_lengths, tone, language, bert, ja_bert, g=g_p
    )

print(f"Official enc_p output:")
print(f"  enc_out: shape={list(enc_out.shape)}, mean={enc_out.mean():.4f}, std={enc_out.std():.4f}")
print(f"  m_p: shape={list(m_p.shape)}, mean={m_p.mean():.4f}, std={m_p.std():.4f}")
print(f"  logs_p: shape={list(logs_p.shape)}, mean={logs_p.mean():.4f}, std={logs_p.std():.4f}")

# Compare with our manual ref
manual_m_p = np.fromfile(os.path.join(outdir, "ref_05_m_p.bin"), dtype=np.float32)
print(f"\nManual m_p: mean={manual_m_p.mean():.4f}, std={manual_m_p.std():.4f}")

official_m_p = m_p.squeeze(0).numpy()
print(f"Official m_p: mean={official_m_p.mean():.4f}, std={official_m_p.std():.4f}")

diff = np.abs(official_m_p.flatten() - manual_m_p.flatten())
print(f"Diff: max={diff.max():.4f}, mean={diff.mean():.4f}")

# Dump official encoder output
official_m_p.astype(np.float32).tofile(os.path.join(outdir, "ref_official_m_p.bin"))
m_p_logs = logs_p.squeeze(0).numpy()
m_p_logs.astype(np.float32).tofile(os.path.join(outdir, "ref_official_logs_p.bin"))
enc_out.squeeze(0).numpy().astype(np.float32).tofile(os.path.join(outdir, "ref_official_enc_out.bin"))
print("\nDumped ref_official_*.bin")
