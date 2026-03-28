#!/usr/bin/env python3
"""Debug: compare Python encoder output with expected values."""
import numpy as np, torch, wave, json, os, sys
os.chdir(os.path.join(os.path.dirname(__file__), ".."))

model_dir = "models/moonshine-base-zh"
wav_path = "test/real_speech/maclaw_16k.wav"

with wave.open(wav_path, "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0

from transformers import AutoModelForSpeechSeq2Seq, Wav2Vec2FeatureExtractor, AutoTokenizer
model = AutoModelForSpeechSeq2Seq.from_pretrained(model_dir)
fe = Wav2Vec2FeatureExtractor.from_pretrained(model_dir)
tok = AutoTokenizer.from_pretrained(model_dir)
model.eval()

inputs = fe(pcm, return_tensors="pt", sampling_rate=16000)
with torch.no_grad():
    enc_out = model.model.encoder(**inputs)
    hidden = enc_out.last_hidden_state

print(f"Encoder output shape: {hidden.shape}")
print(f"First 10 values [0,0,:10]: {hidden[0, 0, :10].tolist()}")
print(f"Mean: {hidden.mean().item():.6f}, Std: {hidden.std().item():.6f}")

# Generate first 20 tokens
gen = model.generate(**inputs, max_new_tokens=20)
text = tok.decode(gen[0], skip_special_tokens=True)
print(f"First 20 tokens: {gen[0].tolist()}")
print(f'Text: "{text}"')

# Now compare with the manual PyTorch implementation from download_and_test_zh.py
# to see if they match
from safetensors import safe_open
weights = {}
with safe_open(os.path.join(model_dir, "model.safetensors"), framework="pt", device="cpu") as f:
    for name in f.keys():
        weights[name] = f.get_tensor(name).float()

with open(os.path.join(model_dir, "config.json")) as f:
    cfg = json.load(f)

dim = 416
n_heads = 8
head_dim = dim // n_heads
n_enc = 8
rotary_dim = int(head_dim * 0.62)
rotary_dim = rotary_dim - (rotary_dim % 2)
theta = 10000.0
w = weights

import torch.nn.functional as F

# Manual encoder
x = torch.from_numpy(pcm).unsqueeze(0).unsqueeze(1)
x = F.conv1d(x, w["model.encoder.conv1.weight"], stride=64)
x = torch.tanh(x)
x = F.group_norm(x, 1, w["model.encoder.groupnorm.weight"], w["model.encoder.groupnorm.bias"])
x = F.conv1d(x, w["model.encoder.conv2.weight"], w["model.encoder.conv2.bias"], stride=3)
x = F.gelu(x)
x = F.conv1d(x, w["model.encoder.conv3.weight"], w["model.encoder.conv3.bias"], stride=2)
x = F.gelu(x)
x = x.permute(0, 2, 1)

def rms_norm(x, w, eps=1e-5):
    return w * x * torch.rsqrt(x.pow(2).mean(-1, keepdim=True) + eps)

def rope_seq(x, rd, th):
    seq = x.shape[2]; dp = rd // 2
    pos = torch.arange(seq, dtype=torch.float32)
    freqs = 1.0 / (th ** (torch.arange(0, rd, 2, dtype=torch.float32) / rd))
    a = pos.unsqueeze(1) * freqs.unsqueeze(0)
    c = torch.cos(a).unsqueeze(0).unsqueeze(0)
    s = torch.sin(a).unsqueeze(0).unsqueeze(0)
    x1 = x[...,:dp]; x2 = x[...,dp:rd]; xp = x[...,rd:]
    return torch.cat([x1*c-x2*s, x2*c+x1*s, xp], dim=-1)

for i in range(n_enc):
    p = f"model.encoder.layers.{i}"
    res = x; x = rms_norm(x, w[f"{p}.input_layernorm.weight"])
    q = F.linear(x, w[f"{p}.self_attn.q_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    k = F.linear(x, w[f"{p}.self_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    v = F.linear(x, w[f"{p}.self_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    q = rope_seq(q, rotary_dim, theta); k = rope_seq(k, rotary_dim, theta)
    out = F.scaled_dot_product_attention(q, k, v).permute(0,2,1,3).reshape(1,-1,dim)
    x = res + F.linear(out, w[f"{p}.self_attn.o_proj.weight"])
    res = x; x = rms_norm(x, w[f"{p}.post_attention_layernorm.weight"])
    x = F.gelu(F.linear(x, w[f"{p}.mlp.fc1.weight"], w[f"{p}.mlp.fc1.bias"]))
    x = res + F.linear(x, w[f"{p}.mlp.fc2.weight"], w[f"{p}.mlp.fc2.bias"])
enc_manual = rms_norm(x, w["model.encoder.layer_norm.weight"])

print(f"\nManual encoder shape: {enc_manual.shape}")
print(f"Manual first 10: {enc_manual[0, 0, :10].tolist()}")
print(f"Manual Mean: {enc_manual.mean().item():.6f}, Std: {enc_manual.std().item():.6f}")

# Compare
diff = (hidden - enc_manual).abs()
print(f"\nHF vs Manual max diff: {diff.max().item():.8f}")
print(f"HF vs Manual mean diff: {diff.mean().item():.8f}")
