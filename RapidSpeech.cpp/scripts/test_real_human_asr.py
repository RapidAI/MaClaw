#!/usr/bin/env python3
"""
Download real human speech from OpenSLR/LibriSpeech and test moonshine decode strategies.
Tests both tiny and base models.
"""
import numpy as np, torch, torch.nn.functional as F, wave, json, sys, os, struct
import urllib.request
from pathlib import Path

os.chdir(os.path.join(os.path.dirname(__file__), ".."))

OUT_DIR = Path("test/librispeech")
OUT_DIR.mkdir(parents=True, exist_ok=True)

# Short LibriSpeech dev-clean FLAC samples (public domain)
# We'll use the HuggingFace mirror which serves individual files
SAMPLES = {
    "1272-128104-0000": {
        "url": "https://huggingface.co/datasets/hf-internal-testing/librispeech_asr_dummy/resolve/main/clean/1272-128104-0000.flac",
        "text": "mister quilter is the apostle of the middle classes and we are glad to welcome his gospel",
    },
    "1272-128104-0001": {
        "url": "https://huggingface.co/datasets/hf-internal-testing/librispeech_asr_dummy/resolve/main/clean/1272-128104-0001.flac",
        "text": "nor is mister quilter's manner less interesting than his matter",
    },
    "1272-128104-0002": {
        "url": "https://huggingface.co/datasets/hf-internal-testing/librispeech_asr_dummy/resolve/main/clean/1272-128104-0002.flac",
        "text": "he tells us that at this festive season of the year",
    },
}

def download_and_convert(name, url):
    wav_path = OUT_DIR / f"{name}.wav"
    if wav_path.exists() and wav_path.stat().st_size > 1000:
        return wav_path
    tmp = OUT_DIR / f"{name}.flac"
    print(f"  Downloading {name}...")
    try:
        urllib.request.urlretrieve(url, str(tmp))
        import soundfile as sf
        audio, sr = sf.read(str(tmp))
        if sr != 16000:
            ratio = 16000 / sr
            n_out = int(len(audio) * ratio)
            audio = np.interp(np.linspace(0, len(audio)-1, n_out),
                              np.arange(len(audio)), audio)
        # Write as 16-bit PCM WAV
        audio_i16 = (audio * 32767).clip(-32768, 32767).astype(np.int16)
        with wave.open(str(wav_path), 'wb') as wf:
            wf.setnchannels(1)
            wf.setsampwidth(2)
            wf.setframerate(16000)
            wf.writeframes(audio_i16.tobytes())
        if tmp.exists(): tmp.unlink()
        return wav_path
    except Exception as e:
        print(f"  Failed: {e}")
        return None

def load_wav(path):
    with wave.open(str(path), "rb") as wf:
        frames = wf.readframes(wf.getnframes())
        pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
    return pcm


# ============================================================
# Model loading helpers (parameterized for tiny/base)
# ============================================================

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
        from safetensors import safe_open
        self.weights = {}
        with safe_open(str(Path(model_dir) / "model.safetensors"), framework="pt", device="cpu") as f:
            for name in f.keys():
                self.weights[name] = f.get_tensor(name).float()
        with open(str(Path(model_dir) / "tokenizer.json"), "r", encoding="utf-8") as f:
            tok_data = json.load(f)
        vocab = tok_data.get("model", {}).get("vocab", {})
        self.id2tok = {v: k for k, v in vocab.items()}
        self.dim = dim; self.n_heads = n_heads; self.head_dim = head_dim
        self.rotary_dim = rotary_dim; self.n_enc = n_enc; self.n_dec = n_dec
        self.theta = 10000.0

    def encode(self, pcm):
        w = self.weights
        x = torch.from_numpy(pcm).unsqueeze(0).unsqueeze(1)
        x = F.conv1d(x, w["model.encoder.conv1.weight"], stride=64)
        x = torch.tanh(x)
        x = F.group_norm(x, 1, w["model.encoder.groupnorm.weight"], w["model.encoder.groupnorm.bias"])
        x = F.conv1d(x, w["model.encoder.conv2.weight"], w["model.encoder.conv2.bias"], stride=3)
        x = F.gelu(x)
        x = F.conv1d(x, w["model.encoder.conv3.weight"], w["model.encoder.conv3.bias"], stride=2)
        x = F.gelu(x)
        x = x.permute(0, 2, 1)
        for i in range(self.n_enc):
            pfx = f"model.encoder.layers.{i}"
            res = x
            x = rms_norm(x, w[f"{pfx}.input_layernorm.weight"])
            q = F.linear(x, w[f"{pfx}.self_attn.q_proj.weight"]).view(1,-1,self.n_heads,self.head_dim).permute(0,2,1,3)
            k = F.linear(x, w[f"{pfx}.self_attn.k_proj.weight"]).view(1,-1,self.n_heads,self.head_dim).permute(0,2,1,3)
            v = F.linear(x, w[f"{pfx}.self_attn.v_proj.weight"]).view(1,-1,self.n_heads,self.head_dim).permute(0,2,1,3)
            q = rope_seq(q, self.rotary_dim, self.theta); k = rope_seq(k, self.rotary_dim, self.theta)
            out = F.scaled_dot_product_attention(q, k, v)
            out = out.permute(0,2,1,3).reshape(1,-1,self.dim)
            x = res + F.linear(out, w[f"{pfx}.self_attn.o_proj.weight"])
            res = x
            x = rms_norm(x, w[f"{pfx}.post_attention_layernorm.weight"])
            x = F.linear(x, w[f"{pfx}.mlp.fc1.weight"], w[f"{pfx}.mlp.fc1.bias"])
            x = F.gelu(x)
            x = F.linear(x, w[f"{pfx}.mlp.fc2.weight"], w[f"{pfx}.mlp.fc2.bias"])
            x = res + x
        return rms_norm(x, w["model.encoder.layer_norm.weight"])

    def decode(self, enc, max_tokens=200, use_strategies=False):
        w = self.weights
        embed = w["model.decoder.embed_tokens.weight"]
        dec_norm = w["model.decoder.norm.weight"]
        cross_k_cache = []; cross_v_cache = []
        for i in range(self.n_dec):
            pfx = f"model.decoder.layers.{i}"
            ck = F.linear(enc, w[f"{pfx}.encoder_attn.k_proj.weight"]).view(1,-1,self.n_heads,self.head_dim).permute(0,2,1,3)
            cv = F.linear(enc, w[f"{pfx}.encoder_attn.v_proj.weight"]).view(1,-1,self.n_heads,self.head_dim).permute(0,2,1,3)
            cross_k_cache.append(ck); cross_v_cache.append(cv)
        self_k = [[] for _ in range(self.n_dec)]
        self_v = [[] for _ in range(self.n_dec)]
        tokens = [BOS]; sum_lp = 0.0
        for step in range(max_tokens):
            x = embed[tokens[-1]].unsqueeze(0).unsqueeze(0)
            for i in range(self.n_dec):
                pfx = f"model.decoder.layers.{i}"
                res = x
                x = rms_norm(x, w[f"{pfx}.input_layernorm.weight"])
                q = F.linear(x, w[f"{pfx}.self_attn.q_proj.weight"]).view(1,1,self.n_heads,self.head_dim).permute(0,2,1,3)
                k = F.linear(x, w[f"{pfx}.self_attn.k_proj.weight"]).view(1,1,self.n_heads,self.head_dim).permute(0,2,1,3)
                v = F.linear(x, w[f"{pfx}.self_attn.v_proj.weight"]).view(1,1,self.n_heads,self.head_dim).permute(0,2,1,3)
                q = rope_single(q, step, self.rotary_dim, self.theta)
                k = rope_single(k, step, self.rotary_dim, self.theta)
                self_k[i].append(k); self_v[i].append(v)
                kf = torch.cat(self_k[i], dim=2); vf = torch.cat(self_v[i], dim=2)
                out = F.scaled_dot_product_attention(q, kf, vf)
                out = out.permute(0,2,1,3).reshape(1,1,self.dim)
                x = res + F.linear(out, w[f"{pfx}.self_attn.o_proj.weight"])
                res = x
                x = rms_norm(x, w[f"{pfx}.post_attention_layernorm.weight"])
                cq = F.linear(x, w[f"{pfx}.encoder_attn.q_proj.weight"]).view(1,1,self.n_heads,self.head_dim).permute(0,2,1,3)
                out = F.scaled_dot_product_attention(cq, cross_k_cache[i], cross_v_cache[i])
                out = out.permute(0,2,1,3).reshape(1,1,self.dim)
                x = res + F.linear(out, w[f"{pfx}.encoder_attn.o_proj.weight"])
                res = x
                x = rms_norm(x, w[f"{pfx}.final_layernorm.weight"])
                fc1 = F.linear(x, w[f"{pfx}.mlp.fc1.weight"], w[f"{pfx}.mlp.fc1.bias"])
                inter = fc1.shape[-1]//2
                x = F.silu(fc1[...,:inter]) * fc1[...,inter:]
                x = F.linear(x, w[f"{pfx}.mlp.fc2.weight"], w[f"{pfx}.mlp.fc2.bias"])
                x = res + x
            logits = F.linear(rms_norm(x, dec_norm), embed)[0, 0]
            if use_strategies:
                for sid in SUPPRESS_TOKENS: logits[sid] = float('-inf')
                if len(tokens) >= NO_REPEAT_NGRAM:
                    prefix = tokens[-(NO_REPEAT_NGRAM-1):]
                    for i in range(len(tokens) - NO_REPEAT_NGRAM + 1):
                        if tokens[i:i+NO_REPEAT_NGRAM-1] == prefix:
                            logits[tokens[i + NO_REPEAT_NGRAM - 1]] = float('-inf')
            next_tok = logits.argmax().item()
            lsm = logits - logits.logsumexp(dim=0)
            sum_lp += lsm[next_tok].item()
            if next_tok == EOS: break
            tokens.append(next_tok)
        return tokens, sum_lp

    def tokens_to_text(self, tokens):
        text = "".join(self.id2tok.get(t, f"<{t}>") for t in tokens[1:])
        return text.replace("\u2581", " ").strip()


# ============================================================
# Main
# ============================================================

print("Downloading LibriSpeech samples...")
wavs = {}
for name, info in SAMPLES.items():
    p = download_and_convert(name, info["url"])
    if p: wavs[name] = p

if not wavs:
    print("No audio downloaded. Install soundfile: pip install soundfile")
    sys.exit(1)

models = {
    "tiny": MoonshineRunner("models/moonshine-tiny", dim=288, n_heads=8,
                             head_dim=36, rotary_dim=32, n_enc=6, n_dec=6),
    "base": MoonshineRunner("models/moonshine-base", dim=416, n_heads=8,
                             head_dim=52, rotary_dim=32, n_enc=8, n_dec=8),
}

for model_name, runner in models.items():
    print()
    print("=" * 80)
    print(f"  Moonshine-{model_name.upper()} on real LibriSpeech speech")
    print("=" * 80)

    for name, wav_path in wavs.items():
        pcm = load_wav(wav_path)
        dur = len(pcm) / 16000
        ref = SAMPLES[name]["text"]
        enc = runner.encode(pcm)

        toks_g, _ = runner.decode(enc, 200, False)
        text_g = runner.tokens_to_text(toks_g)

        toks_s, _ = runner.decode(enc, 200, True)
        text_s = runner.tokens_to_text(toks_s)

        print(f"\n--- {name} ({dur:.1f}s) ---")
        print(f"  REF:      \"{ref}\"")
        print(f"  GREEDY:   ({len(toks_g)-1:3d} tok) \"{text_g[:150]}\"")
        print(f"  STRATEGY: ({len(toks_s)-1:3d} tok) \"{text_s[:150]}\"")

print("\nDone.")
