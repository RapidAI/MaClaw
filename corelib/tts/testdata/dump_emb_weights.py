#!/usr/bin/env python3
"""Dump embedding weights from Python checkpoint for exact comparison."""
import os, sys, json, struct
import numpy as np
import torch

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")
ckpt_path = os.path.join(model_dir, "checkpoint.pth")

ckpt = torch.load(ckpt_path, map_location="cpu", weights_only=True)
sd = ckpt.get("model", ckpt)

# Dump key weights as raw float32 binary files for Go to load
weights_to_dump = {
    "emb": sd["enc_p.emb.weight"],           # [219, 192]
    "tone_emb": sd["enc_p.tone_emb.weight"],  # [16, 192]
    "lang_emb": sd["enc_p.language_emb.weight"],  # [10, 192]
    "emb_g": sd["emb_g.weight"],              # [1, 256]
    # Duration predictor
    "dp_conv1_w": sd["dp.conv_1.weight"],     # [256, 192, 3]
    "dp_conv1_b": sd["dp.conv_1.bias"],       # [256]
}

for name, tensor in weights_to_dump.items():
    data = tensor.detach().numpy().astype(np.float32)
    path = os.path.join(outdir, f"pyweight_{name}.bin")
    data.tofile(path)
    print(f"  {name}: {list(data.shape)} -> {path} ({data.nbytes} bytes)")

# Also compute and dump the embedding output for "Hello"
with open(os.path.join(model_dir, "config.json"), encoding='utf-8') as f:
    config = json.load(f)
symbols = config["symbols"]
sym2id = {s: i for i, s in enumerate(symbols)}

phone_ids = [0, 49, 0, 127, 0, 70, 0, 80, 0]  # Hello with blanks
T = len(phone_ids)
hidden = 192

import math
emb_w = sd["enc_p.emb.weight"]
tone_w = sd["enc_p.tone_emb.weight"]
lang_w = sd["enc_p.language_emb.weight"]

x = torch.zeros(T, hidden)
for t in range(T):
    x[t] = emb_w[phone_ids[t]] + tone_w[0] + lang_w[2]  # tone=0, lang=EN=2
x = x * math.sqrt(hidden)
x = x.T  # [hidden, T]

x_np = x.detach().numpy().astype(np.float32)
x_np.tofile(os.path.join(outdir, "pyref_embedding_output.bin"))
print(f"\n  Embedding output: {list(x_np.shape)}, mean={x_np.mean():.4f}, std={x_np.std():.4f}")
print(f"  First 5 values: {x_np.flatten()[:5]}")
