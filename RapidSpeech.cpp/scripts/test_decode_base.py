#!/usr/bin/env python3
"""
Test decode strategies on moonshine-base (dim=416, depth=8).
Compares greedy vs suppress+no-repeat-ngram.
"""
import numpy as np, torch, torch.nn.functional as F, wave, json, sys, os
from pathlib import Path
from safetensors import safe_open

os.chdir(os.path.join(os.path.dirname(__file__), ".."))

model_dir = Path("models/moonshine-base")
weights = {}
with safe_open(str(model_dir / "model.safetensors"), framework="pt", device="cpu") as f:
    for name in f.keys():
        weights[name] = f.get_tensor(name).float()

with open(str(model_dir / "tokenizer.json"), "r", encoding="utf-8") as f:
    tok_data = json.load(f)
vocab = tok_data.get("model", {}).get("vocab", {})
id2tok = {v: k for k, v in vocab.items()}

# Base model params
dim = 416; n_heads = 8; head_dim = 52; rotary_dim = 32; theta = 10000.0
n_enc = 8; n_dec = 8
BOS = 1; EOS = 2
SUPPRESS_TOKENS = [0, 1]
NO_REPEAT_NGRAM = 3

def rms_norm(x, w, eps=1e-5):
    return w * x * torch.rsqrt(x.pow(2).mean(-1, keepdim=True) + eps)

def rope_single(x, pos, rotary_dim, theta):
    dp = rotary_dim // 2
    freqs = 1.0 / (theta ** (torch.arange(0, rotary_dim, 2, dtype=torch.float32) / rotary_dim))
    angles = pos * freqs
    cos_a = torch.cos(angles).view(1, 1, 1, dp)
    sin_a = torch.sin(angles).view(1, 1, 1, dp)
    x1 = x[..., :dp]; x2 = x[..., dp:rotary_dim]; xp = x[..., rotary_dim:]
    return torch.cat([x1*cos_a - x2*sin_a, x2*cos_a + x1*sin_a, xp], dim=-1)

def rope_seq(x, rotary_dim, theta):
    seq = x.shape[2]; dp = rotary_dim // 2
    pos = torch.arange(seq, dtype=torch.float32)
    freqs = 1.0 / (theta ** (torch.arange(0, rotary_dim, 2, dtype=torch.float32) / rotary_dim))
    angles = pos.unsqueeze(1) * freqs.unsqueeze(0)
    cos_a = torch.cos(angles).unsqueeze(0).unsqueeze(0)
    sin_a = torch.sin(angles).unsqueeze(0).unsqueeze(0)
    x1 = x[..., :dp]; x2 = x[..., dp:rotary_dim]; xp = x[..., rotary_dim:]
    return torch.cat([x1*cos_a - x2*sin_a, x2*cos_a + x1*sin_a, xp], dim=-1)


def encode(pcm):
    pcm_t = torch.from_numpy(pcm).unsqueeze(0).unsqueeze(1)
    x = F.conv1d(pcm_t, weights["model.encoder.conv1.weight"], stride=64)
    x = torch.tanh(x)
    x = F.group_norm(x, 1, weights["model.encoder.groupnorm.weight"],
                     weights["model.encoder.groupnorm.bias"])
    x = F.conv1d(x, weights["model.encoder.conv2.weight"],
                 weights["model.encoder.conv2.bias"], stride=3)
    x = F.gelu(x)
    x = F.conv1d(x, weights["model.encoder.conv3.weight"],
                 weights["model.encoder.conv3.bias"], stride=2)
    x = F.gelu(x)
    x = x.permute(0, 2, 1)
    for i in range(n_enc):
        pfx = f"model.encoder.layers.{i}"
        res = x
        x = rms_norm(x, weights[f"{pfx}.input_layernorm.weight"])
        q = F.linear(x, weights[f"{pfx}.self_attn.q_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
        k = F.linear(x, weights[f"{pfx}.self_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
        v = F.linear(x, weights[f"{pfx}.self_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
        q = rope_seq(q, rotary_dim, theta); k = rope_seq(k, rotary_dim, theta)
        out = F.scaled_dot_product_attention(q, k, v)
        out = out.permute(0,2,1,3).reshape(1,-1,dim)
        x = res + F.linear(out, weights[f"{pfx}.self_attn.o_proj.weight"])
        res = x
        x = rms_norm(x, weights[f"{pfx}.post_attention_layernorm.weight"])
        x = F.linear(x, weights[f"{pfx}.mlp.fc1.weight"], weights[f"{pfx}.mlp.fc1.bias"])
        x = F.gelu(x)
        x = F.linear(x, weights[f"{pfx}.mlp.fc2.weight"], weights[f"{pfx}.mlp.fc2.bias"])
        x = res + x
    return rms_norm(x, weights["model.encoder.layer_norm.weight"])

def decode(enc, max_tokens=200, use_strategies=False):
    embed = weights["model.decoder.embed_tokens.weight"]
    dec_norm = weights["model.decoder.norm.weight"]
    cross_k_cache = []; cross_v_cache = []
    for i in range(n_dec):
        pfx = f"model.decoder.layers.{i}"
        ck = F.linear(enc, weights[f"{pfx}.encoder_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
        cv = F.linear(enc, weights[f"{pfx}.encoder_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
        cross_k_cache.append(ck); cross_v_cache.append(cv)
    self_k_cache = [[] for _ in range(n_dec)]
    self_v_cache = [[] for _ in range(n_dec)]
    tokens = [BOS]; sum_logprob = 0.0
    for step in range(max_tokens):
        tok_emb = embed[tokens[-1]].unsqueeze(0).unsqueeze(0)
        x = tok_emb
        for i in range(n_dec):
            pfx = f"model.decoder.layers.{i}"
            res = x
            x = rms_norm(x, weights[f"{pfx}.input_layernorm.weight"])
            q = F.linear(x, weights[f"{pfx}.self_attn.q_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
            k = F.linear(x, weights[f"{pfx}.self_attn.k_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
            v = F.linear(x, weights[f"{pfx}.self_attn.v_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
            q = rope_single(q, step, rotary_dim, theta)
            k = rope_single(k, step, rotary_dim, theta)
            self_k_cache[i].append(k); self_v_cache[i].append(v)
            k_full = torch.cat(self_k_cache[i], dim=2)
            v_full = torch.cat(self_v_cache[i], dim=2)
            out = F.scaled_dot_product_attention(q, k_full, v_full)
            out = out.permute(0,2,1,3).reshape(1,1,dim)
            x = res + F.linear(out, weights[f"{pfx}.self_attn.o_proj.weight"])
            res = x
            x = rms_norm(x, weights[f"{pfx}.post_attention_layernorm.weight"])
            cq = F.linear(x, weights[f"{pfx}.encoder_attn.q_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
            out = F.scaled_dot_product_attention(cq, cross_k_cache[i], cross_v_cache[i])
            out = out.permute(0,2,1,3).reshape(1,1,dim)
            x = res + F.linear(out, weights[f"{pfx}.encoder_attn.o_proj.weight"])
            res = x
            x = rms_norm(x, weights[f"{pfx}.final_layernorm.weight"])
            fc1 = F.linear(x, weights[f"{pfx}.mlp.fc1.weight"], weights[f"{pfx}.mlp.fc1.bias"])
            inter = fc1.shape[-1]//2
            x = F.silu(fc1[...,:inter]) * fc1[...,inter:]
            x = F.linear(x, weights[f"{pfx}.mlp.fc2.weight"], weights[f"{pfx}.mlp.fc2.bias"])
            x = res + x
        logits = F.linear(rms_norm(x, dec_norm), embed)[0, 0]
        if use_strategies:
            for sid in SUPPRESS_TOKENS:
                logits[sid] = float('-inf')
            if len(tokens) >= NO_REPEAT_NGRAM:
                prefix = tokens[-(NO_REPEAT_NGRAM-1):]
                for i in range(len(tokens) - NO_REPEAT_NGRAM + 1):
                    if tokens[i:i+NO_REPEAT_NGRAM-1] == prefix:
                        logits[tokens[i + NO_REPEAT_NGRAM - 1]] = float('-inf')
        next_tok = logits.argmax().item()
        log_softmax = logits - logits.logsumexp(dim=0)
        sum_logprob += log_softmax[next_tok].item()
        if next_tok == EOS: break
        tokens.append(next_tok)
    return tokens, sum_logprob

def tokens_to_text(tokens):
    text = "".join(id2tok.get(t, f"<{t}>") for t in tokens[1:])
    return text.replace("\u2581", " ").strip()

def compression_ratio(tokens):
    if len(tokens) <= 2: return 1.0
    toks = tokens[1:]
    bigrams = set()
    for i in range(len(toks) - 1):
        bigrams.add((toks[i], toks[i+1]))
    return (len(toks) - 1) / max(len(bigrams), 1)

def load_wav(path):
    with wave.open(path, "rb") as wf:
        frames = wf.readframes(wf.getnframes())
        pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
    return pcm


# ============================================================
# Main
# ============================================================
test_files = sorted(Path("test/real_speech").glob("*.wav"))
selected = []
for f in test_files:
    pcm = load_wav(str(f))
    dur = len(pcm) / 16000
    selected.append((f, pcm, dur))
selected.sort(key=lambda x: x[2])
test_set = selected[:2] + selected[-3:]  # 2 short + 3 long

print("=" * 80)
print("Moonshine-BASE Decode Strategy Comparison")
print(f"Model: dim={dim}, heads={n_heads}, head_dim={head_dim}, "
      f"enc_depth={n_enc}, dec_depth={n_dec}, rotary_dim={rotary_dim}")
print("=" * 80)
print(f"Strategies: suppress_tokens={SUPPRESS_TOKENS}, no_repeat_ngram={NO_REPEAT_NGRAM}")
print()

for wav_path, pcm, dur in test_set:
    print(f"--- {wav_path.name} ({dur:.1f}s) ---")
    enc = encode(pcm)

    toks_g, lp_g = decode(enc, 200, False)
    text_g = tokens_to_text(toks_g)
    n_g = len(toks_g) - 1
    cr_g = compression_ratio(toks_g)
    avg_g = lp_g / max(n_g, 1)

    toks_s, lp_s = decode(enc, 200, True)
    text_s = tokens_to_text(toks_s)
    n_s = len(toks_s) - 1
    cr_s = compression_ratio(toks_s)
    avg_s = lp_s / max(n_s, 1)

    print(f"  [GREEDY]    {n_g:3d} tok, cr={cr_g:.2f}, avg_lp={avg_g:.3f}")
    print(f"              \"{text_g[:120]}\"")
    print(f"  [STRATEGY]  {n_s:3d} tok, cr={cr_s:.2f}, avg_lp={avg_s:.3f}")
    print(f"              \"{text_s[:120]}\"")
    if cr_g > 2.0 and cr_s < cr_g:
        print(f"  >>> IMPROVEMENT: cr {cr_g:.2f} -> {cr_s:.2f}")
    if n_g > n_s * 1.5:
        print(f"  >>> IMPROVEMENT: tokens {n_g} -> {n_s}")
    print()

print("Done.")
