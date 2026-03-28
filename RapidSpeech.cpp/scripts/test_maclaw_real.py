#!/usr/bin/env python3
"""Convert maclaw.m4a to 16kHz WAV and test with both tiny and base models."""
import numpy as np, torch, torch.nn.functional as F, wave, json, sys, os
from pathlib import Path

os.chdir(os.path.join(os.path.dirname(__file__), ".."))

# Step 1: Convert m4a to 16kHz mono WAV
m4a_path = "test/real_speech/maclaw.m4a"
wav_path = "test/real_speech/maclaw_16k.wav"

if not os.path.exists(wav_path):
    print("Converting m4a -> 16kHz WAV...")
    try:
        import soundfile as sf
        import subprocess
        # Try ffmpeg first
        try:
            subprocess.run([
                "ffmpeg", "-y", "-i", m4a_path,
                "-ar", "16000", "-ac", "1", "-sample_fmt", "s16",
                wav_path
            ], check=True, capture_output=True)
            print("  Converted with ffmpeg")
        except (FileNotFoundError, subprocess.CalledProcessError):
            # Try pydub
            from pydub import AudioSegment
            audio = AudioSegment.from_file(m4a_path)
            audio = audio.set_frame_rate(16000).set_channels(1).set_sample_width(2)
            audio.export(wav_path, format="wav")
            print("  Converted with pydub")
    except Exception as e:
        print(f"  Conversion failed: {e}")
        print("  Install ffmpeg or pydub: pip install pydub")
        sys.exit(1)

# Load WAV
with wave.open(wav_path, "rb") as wf:
    sr = wf.getframerate()
    nc = wf.getnchannels()
    ns = wf.getnframes()
    frames = wf.readframes(ns)
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0

dur = len(pcm) / 16000
print(f"Audio: {dur:.1f}s, {len(pcm)} samples, sr={sr}, channels={nc}")
print(f"RMS: {np.sqrt(np.mean(pcm**2)):.4f}")
print()

# Step 2: Load model runner from test_decode_strategies (tiny) and test_decode_base (base)
from safetensors import safe_open

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

class MoonshineRunner:
    def __init__(self, model_dir, dim, n_heads, head_dim, rotary_dim, n_enc, n_dec):
        self.weights = {}
        with safe_open(str(Path(model_dir) / "model.safetensors"), framework="pt", device="cpu") as f:
            for name in f.keys(): self.weights[name] = f.get_tensor(name).float()
        with open(str(Path(model_dir) / "tokenizer.json"), "r", encoding="utf-8") as f:
            tok_data = json.load(f)
        vocab = tok_data.get("model", {}).get("vocab", {})
        self.id2tok = {v: k for k, v in vocab.items()}
        self.dim = dim; self.n_heads = n_heads; self.head_dim = head_dim
        self.rotary_dim = rotary_dim; self.n_enc = n_enc; self.n_dec = n_dec; self.theta = 10000.0

    def encode(self, pcm):
        w = self.weights
        x = torch.from_numpy(pcm).unsqueeze(0).unsqueeze(1)
        x = F.conv1d(x, w["model.encoder.conv1.weight"], stride=64); x = torch.tanh(x)
        x = F.group_norm(x, 1, w["model.encoder.groupnorm.weight"], w["model.encoder.groupnorm.bias"])
        x = F.conv1d(x, w["model.encoder.conv2.weight"], w["model.encoder.conv2.bias"], stride=3); x = F.gelu(x)
        x = F.conv1d(x, w["model.encoder.conv3.weight"], w["model.encoder.conv3.bias"], stride=2); x = F.gelu(x)
        x = x.permute(0, 2, 1)
        for i in range(self.n_enc):
            p = f"model.encoder.layers.{i}"
            res = x; x = rms_norm(x, w[f"{p}.input_layernorm.weight"])
            q = F.linear(x, w[f"{p}.self_attn.q_proj.weight"]).view(1,-1,self.n_heads,self.head_dim).permute(0,2,1,3)
            k = F.linear(x, w[f"{p}.self_attn.k_proj.weight"]).view(1,-1,self.n_heads,self.head_dim).permute(0,2,1,3)
            v = F.linear(x, w[f"{p}.self_attn.v_proj.weight"]).view(1,-1,self.n_heads,self.head_dim).permute(0,2,1,3)
            q = rope_seq(q, self.rotary_dim, self.theta); k = rope_seq(k, self.rotary_dim, self.theta)
            out = F.scaled_dot_product_attention(q, k, v).permute(0,2,1,3).reshape(1,-1,self.dim)
            x = res + F.linear(out, w[f"{p}.self_attn.o_proj.weight"])
            res = x; x = rms_norm(x, w[f"{p}.post_attention_layernorm.weight"])
            x = F.gelu(F.linear(x, w[f"{p}.mlp.fc1.weight"], w[f"{p}.mlp.fc1.bias"]))
            x = res + F.linear(x, w[f"{p}.mlp.fc2.weight"], w[f"{p}.mlp.fc2.bias"])
        return rms_norm(x, w["model.encoder.layer_norm.weight"])

    def decode(self, enc, max_tokens=300, use_strategies=False):
        w = self.weights; embed = w["model.decoder.embed_tokens.weight"]; dec_norm = w["model.decoder.norm.weight"]
        ck_c = []; cv_c = []
        for i in range(self.n_dec):
            p = f"model.decoder.layers.{i}"
            ck_c.append(F.linear(enc, w[f"{p}.encoder_attn.k_proj.weight"]).view(1,-1,self.n_heads,self.head_dim).permute(0,2,1,3))
            cv_c.append(F.linear(enc, w[f"{p}.encoder_attn.v_proj.weight"]).view(1,-1,self.n_heads,self.head_dim).permute(0,2,1,3))
        sk = [[] for _ in range(self.n_dec)]; sv = [[] for _ in range(self.n_dec)]
        tokens = [BOS]; sum_lp = 0.0
        for step in range(max_tokens):
            x = embed[tokens[-1]].unsqueeze(0).unsqueeze(0)
            for i in range(self.n_dec):
                p = f"model.decoder.layers.{i}"
                res = x; x = rms_norm(x, w[f"{p}.input_layernorm.weight"])
                q = F.linear(x, w[f"{p}.self_attn.q_proj.weight"]).view(1,1,self.n_heads,self.head_dim).permute(0,2,1,3)
                k = F.linear(x, w[f"{p}.self_attn.k_proj.weight"]).view(1,1,self.n_heads,self.head_dim).permute(0,2,1,3)
                v = F.linear(x, w[f"{p}.self_attn.v_proj.weight"]).view(1,1,self.n_heads,self.head_dim).permute(0,2,1,3)
                q = rope_single(q, step, self.rotary_dim, self.theta); k = rope_single(k, step, self.rotary_dim, self.theta)
                sk[i].append(k); sv[i].append(v)
                out = F.scaled_dot_product_attention(q, torch.cat(sk[i],dim=2), torch.cat(sv[i],dim=2)).permute(0,2,1,3).reshape(1,1,self.dim)
                x = res + F.linear(out, w[f"{p}.self_attn.o_proj.weight"])
                res = x; x = rms_norm(x, w[f"{p}.post_attention_layernorm.weight"])
                cq = F.linear(x, w[f"{p}.encoder_attn.q_proj.weight"]).view(1,1,self.n_heads,self.head_dim).permute(0,2,1,3)
                out = F.scaled_dot_product_attention(cq, ck_c[i], cv_c[i]).permute(0,2,1,3).reshape(1,1,self.dim)
                x = res + F.linear(out, w[f"{p}.encoder_attn.o_proj.weight"])
                res = x; x = rms_norm(x, w[f"{p}.final_layernorm.weight"])
                fc1 = F.linear(x, w[f"{p}.mlp.fc1.weight"], w[f"{p}.mlp.fc1.bias"]); inter = fc1.shape[-1]//2
                x = res + F.linear(F.silu(fc1[...,:inter]) * fc1[...,inter:], w[f"{p}.mlp.fc2.weight"], w[f"{p}.mlp.fc2.bias"])
            logits = F.linear(rms_norm(x, dec_norm), embed)[0, 0]
            if use_strategies:
                for sid in SUPPRESS_TOKENS: logits[sid] = float('-inf')
                if len(tokens) >= NO_REPEAT_NGRAM:
                    prefix = tokens[-(NO_REPEAT_NGRAM-1):]
                    for j in range(len(tokens) - NO_REPEAT_NGRAM + 1):
                        if tokens[j:j+NO_REPEAT_NGRAM-1] == prefix:
                            logits[tokens[j + NO_REPEAT_NGRAM - 1]] = float('-inf')
            next_tok = logits.argmax().item()
            lsm = logits - logits.logsumexp(dim=0); sum_lp += lsm[next_tok].item()
            if next_tok == EOS: break
            tokens.append(next_tok)
        return tokens, sum_lp

    def tokens_to_text(self, tokens):
        text = "".join(self.id2tok.get(t, f"<{t}>") for t in tokens[1:])
        return text.replace("\u2581", " ").strip()

def compression_ratio(tokens):
    if len(tokens) <= 2: return 1.0
    toks = tokens[1:]
    bigrams = set()
    for i in range(len(toks) - 1): bigrams.add((toks[i], toks[i+1]))
    return (len(toks) - 1) / max(len(bigrams), 1)

# Step 3: Test both models
models = {
    "tiny": MoonshineRunner("models/moonshine-tiny", dim=288, n_heads=8,
                             head_dim=36, rotary_dim=32, n_enc=6, n_dec=6),
    "base": MoonshineRunner("models/moonshine-base", dim=416, n_heads=8,
                             head_dim=52, rotary_dim=32, n_enc=8, n_dec=8),
}

for model_name, runner in models.items():
    print("=" * 70)
    print(f"  Moonshine-{model_name.upper()} on maclaw.m4a ({dur:.1f}s real speech)")
    print("=" * 70)

    enc = runner.encode(pcm)
    print(f"  Encoder output: {enc.shape}")

    # Greedy
    toks_g, lp_g = runner.decode(enc, 300, False)
    text_g = runner.tokens_to_text(toks_g)
    n_g = len(toks_g) - 1
    cr_g = compression_ratio(toks_g)
    avg_g = lp_g / max(n_g, 1)

    # Strategy
    toks_s, lp_s = runner.decode(enc, 300, True)
    text_s = runner.tokens_to_text(toks_s)
    n_s = len(toks_s) - 1
    cr_s = compression_ratio(toks_s)
    avg_s = lp_s / max(n_s, 1)

    print(f"\n  [GREEDY]    {n_g:3d} tokens, cr={cr_g:.2f}, avg_logprob={avg_g:.3f}")
    print(f"  \"{text_g}\"")
    print(f"\n  [STRATEGY]  {n_s:3d} tokens, cr={cr_s:.2f}, avg_logprob={avg_s:.3f}")
    print(f"  \"{text_s}\"")

    if cr_g > 2.0 and cr_s < cr_g:
        print(f"\n  >>> IMPROVEMENT: cr {cr_g:.2f} -> {cr_s:.2f}")
    if n_g > n_s * 1.5:
        print(f"  >>> IMPROVEMENT: tokens {n_g} -> {n_s}")
    print()

print("Done.")
