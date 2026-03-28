#!/usr/bin/env python3
"""Debug: find which encoder layer diverges between HF and manual."""
import numpy as np, torch, wave, json, os
import torch.nn.functional as F
os.chdir(os.path.join(os.path.dirname(__file__), ".."))

model_dir = "models/moonshine-base-zh"
wav_path = "test/real_speech/maclaw_16k.wav"

with wave.open(wav_path, "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0

from safetensors import safe_open
weights = {}
with safe_open(os.path.join(model_dir, "model.safetensors"), framework="pt", device="cpu") as f:
    for name in f.keys():
        weights[name] = f.get_tensor(name).float()

with open(os.path.join(model_dir, "config.json")) as f:
    cfg = json.load(f)

# Load HF model for reference
from transformers import AutoModelForSpeechSeq2Seq, Wav2Vec2FeatureExtractor
hf_model = AutoModelForSpeechSeq2Seq.from_pretrained(model_dir)
fe = Wav2Vec2FeatureExtractor.from_pretrained(model_dir)
hf_model.eval()

inputs = fe(pcm, return_tensors="pt", sampling_rate=16000)

# Hook into HF encoder to get intermediate outputs
hf_intermediates = {}
def make_hook(name):
    def hook(module, input, output):
        if isinstance(output, tuple):
            hf_intermediates[name] = output[0].detach()
        else:
            hf_intermediates[name] = output.detach()
    return hook

# Hook the frontend
hf_model.model.encoder.conv1.register_forward_hook(make_hook("conv1"))
hf_model.model.encoder.conv2.register_forward_hook(make_hook("conv2"))
hf_model.model.encoder.conv3.register_forward_hook(make_hook("conv3"))
hf_model.model.encoder.groupnorm.register_forward_hook(make_hook("groupnorm"))

# Hook each encoder layer
for i in range(8):
    hf_model.model.encoder.layers[i].register_forward_hook(make_hook(f"layer_{i}"))
hf_model.model.encoder.layer_norm.register_forward_hook(make_hook("final_norm"))

with torch.no_grad():
    hf_out = hf_model.model.encoder(**inputs)

# Now run manual encoder step by step
w = weights
dim = 416; n_heads = 8; head_dim = dim // n_heads; n_enc = 8
rotary_dim = int(head_dim * 0.62)
rotary_dim = rotary_dim - (rotary_dim % 2)
theta = 10000.0

def rms_norm(x, wt, eps=1e-5):
    return wt * x * torch.rsqrt(x.pow(2).mean(-1, keepdim=True) + eps)

def rope_seq(x, rd, th):
    seq = x.shape[2]; dp = rd // 2
    pos = torch.arange(seq, dtype=torch.float32)
    freqs = 1.0 / (th ** (torch.arange(0, rd, 2, dtype=torch.float32) / rd))
    a = pos.unsqueeze(1) * freqs.unsqueeze(0)
    c = torch.cos(a).unsqueeze(0).unsqueeze(0)
    s = torch.sin(a).unsqueeze(0).unsqueeze(0)
    x1 = x[...,:dp]; x2 = x[...,dp:rd]; xp = x[...,rd:]
    return torch.cat([x1*c-x2*s, x2*c+x1*s, xp], dim=-1)

# Frontend
x = torch.from_numpy(pcm).unsqueeze(0).unsqueeze(1)
x = F.conv1d(x, w["model.encoder.conv1.weight"], stride=64)
print(f"After conv1: manual shape={x.shape}")
hf_conv1 = hf_intermediates["conv1"]
print(f"  HF conv1 shape={hf_conv1.shape}")
# HF conv1 output might be in different format, let's check
