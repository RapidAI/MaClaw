#!/usr/bin/env python3
"""Use HF model.generate() as ground truth for maclaw.m4a.
Avoids AutoProcessor by manually preparing inputs."""
import numpy as np, torch, torch.nn.functional as F, wave, json, os, sys
from pathlib import Path
from safetensors import safe_open

os.chdir(os.path.join(os.path.dirname(__file__), ".."))

wav_path = "test/real_speech/maclaw_16k.wav"
with wave.open(wav_path, "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
print(f"Audio: {len(pcm)/16000:.1f}s")

# Use our manual encoder + HF model's generate() for decoder
# This tests whether the issue is in encoder or decoder
for model_name, dim, n_heads, head_dim, rotary_dim, n_enc in [
    ("moonshine-tiny", 288, 8, 36, 32, 6),
    ("moonshine-base", 416, 8, 52, 46, 8),
]:
    model_dir = Path(f"models/{model_name}")
    w = {}
    with safe_open(str(model_dir / "model.safetensors"), framework="pt", device="cpu") as f:
        for name in f.keys(): w[name] = f.get_tensor(name).float()
    with open(str(model_dir / "tokenizer.json"), "r", encoding="utf-8") as f:
        tok_data = json.load(f)
    vocab = tok_data.get("model", {}).get("vocab", {})
    id2tok = {v: k for k, v in vocab.items()}

    # Quick check: what are the top tokens from step 0?
    def rms_norm(x, wt, eps=1e-5):
        return wt * x * torch.rsqrt(x.pow(2).mean(-1, keepdim=True) + eps)

    def rope_seq(x, rd, theta):
        seq = x.shape[2]; dp = rd // 2
        pos = torch.arange(seq, dtype=torch.float32)
        freqs = 1.0 / (theta ** (torch.arange(0, rd, 2, dtype=torch.float32) / rd))
        angles = pos.unsqueeze(1) * freqs.unsqueeze(0)
        cos_a = torch.cos(angles).unsqueeze(0).unsqueeze(0)
        sin_a = torch.sin(angles).unsqueeze(0).unsqueeze(0)
        x1 = x[..., :dp]; x2 = x[..., dp:rd]; xp = x[..., rd:]
        return torch.cat([x1*cos_a - x2*sin_a, x2*cos_a + x1*sin_a, xp], dim=-1)

    # Encode
    x = torch.from_numpy(pcm).unsqueeze(0).unsqueeze(1)
    x = F.conv1d(x, w["model.encoder.conv1.weight"], stride=64); x = torch.tanh(x)
    x = F.group_norm(x, 1, w["model.encoder.groupnorm.weight"], w["model.encoder.groupnorm.bias"])
    x = F.conv1d(x, w["model.encoder.conv2.weight"], w["model.encoder.conv2.bias"], stride=3); x = F.gelu(x)
    x = F.conv1d(x, w["model.encoder.conv3.weight"], w["model.encoder.conv3.bias"], stride=2); x = F.gelu(x)
    x = x.permute(0, 2, 1)
    for i in range(n_enc):
        p = f"model.encoder.layers.{i}"
        res = x; x = rms_norm(x, w[f"{p}.input_layernorm.weight"])
        q = F.linear(x, w[f"{p}.self_attn.q_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
        k = F.linear(x, w[f"{p}.self_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
        v = F.linear(x, w[f"{p}.self_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
        q = rope_seq(q, rotary_dim, 10000.0); k = rope_seq(k, rotary_dim, 10000.0)
        out = F.scaled_dot_product_attention(q, k, v).permute(0,2,1,3).reshape(1,-1,dim)
        x = res + F.linear(out, w[f"{p}.self_attn.o_proj.weight"])
        res = x; x = rms_norm(x, w[f"{p}.post_attention_layernorm.weight"])
        x = F.gelu(F.linear(x, w[f"{p}.mlp.fc1.weight"], w[f"{p}.mlp.fc1.bias"]))
        x = res + F.linear(x, w[f"{p}.mlp.fc2.weight"], w[f"{p}.mlp.fc2.bias"])
    enc = rms_norm(x, w["model.encoder.layer_norm.weight"])

    # Check encoder stats
    print(f"\n{'='*60}")
    print(f"  {model_name}: encoder {enc.shape}")
    print(f"  enc mean={enc.mean():.4f} std={enc.std():.4f} min={enc.min():.4f} max={enc.max():.4f}")

    # Decode step 0: what does the model predict after BOS?
    embed = w["model.decoder.embed_tokens.weight"]
    print(f"  embed shape={embed.shape}, BOS embedding norm={embed[1].norm():.4f}")

    # Show top-10 tokens from first decode step (just logits, no full decode)
    # This tells us if the encoder output is meaningful
    tok_emb = embed[1].unsqueeze(0).unsqueeze(0)  # BOS
    # ... (skip full decode, just show encoder stats)

    # Show what tokens the vocab contains for Chinese
    zh_tokens = [(tid, tok) for tid, tok in id2tok.items()
                 if any(0x4e00 <= ord(c) <= 0x9fff for c in tok)]
    print(f"  Chinese tokens in vocab: {len(zh_tokens)}")
    if zh_tokens[:5]:
        print(f"  Sample: {zh_tokens[:5]}")

print("\nDone.")
