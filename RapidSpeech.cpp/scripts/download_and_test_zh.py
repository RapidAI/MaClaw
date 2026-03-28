#!/usr/bin/env python3
"""Download moonshine-base-zh and test on maclaw.m4a with decode strategies."""
import os, sys
os.chdir(os.path.join(os.path.dirname(__file__), ".."))

model_dir = "models/moonshine-base-zh"
if not os.path.exists(os.path.join(model_dir, "model.safetensors")):
    print("Downloading moonshine-base-zh from HuggingFace...")
    from huggingface_hub import snapshot_download
    snapshot_download(
        repo_id="UsefulSensors/moonshine-base-zh",
        local_dir=model_dir,
        allow_patterns=["model.safetensors", "tokenizer.json", "config.json",
                        "generation_config.json", "preprocessor_config.json"],
    )
    print("Done downloading.")
else:
    print("Model already exists.")

# Check config
import json
with open(os.path.join(model_dir, "config.json")) as f:
    cfg = json.load(f)
print(f"Config: hidden_size={cfg.get('hidden_size')}, "
      f"enc_layers={cfg.get('encoder_num_hidden_layers')}, "
      f"dec_layers={cfg.get('decoder_num_hidden_layers')}, "
      f"heads={cfg.get('encoder_num_attention_heads')}, "
      f"vocab={cfg.get('vocab_size')}")


# Now test with our PyTorch implementation
import numpy as np, torch, torch.nn.functional as F, wave
from pathlib import Path
from safetensors import safe_open

wav_path = "test/real_speech/maclaw_16k.wav"
with wave.open(wav_path, "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
print(f"\nAudio: {len(pcm)/16000:.1f}s")

# Load model
weights = {}
with safe_open(os.path.join(model_dir, "model.safetensors"), framework="pt", device="cpu") as f:
    for name in f.keys(): weights[name] = f.get_tensor(name).float()
with open(os.path.join(model_dir, "tokenizer.json"), "r", encoding="utf-8") as f:
    tok_data = json.load(f)
vocab = tok_data.get("model", {}).get("vocab", {})
id2tok = {v: k for k, v in vocab.items()}

# Detect dims from weights
dim = weights["model.decoder.embed_tokens.weight"].shape[1]
n_heads = cfg.get("encoder_num_attention_heads", 8)
head_dim = dim // n_heads
n_enc = cfg.get("encoder_num_hidden_layers", 8)
n_dec = cfg.get("decoder_num_hidden_layers", 8)
rotary_dim = int(head_dim * cfg.get("partial_rotary_factor", 0.9))
# Round to even
rotary_dim = rotary_dim - (rotary_dim % 2)
theta = cfg.get("rope_theta", 10000.0)
vocab_size = cfg.get("vocab_size", 32768)

print(f"Model: dim={dim}, heads={n_heads}, head_dim={head_dim}, "
      f"rotary_dim={rotary_dim}, enc={n_enc}, dec={n_dec}, vocab={vocab_size}")
print(f"Chinese tokens: {sum(1 for t in id2tok.values() if any(0x4e00<=ord(c)<=0x9fff for c in t))}")

BOS = cfg.get("bos_token_id", 1)
EOS = cfg.get("eos_token_id", 2)
SUPPRESS = [0, BOS]
NGRAM = 3

def rms_norm(x, w, eps=1e-5):
    return w * x * torch.rsqrt(x.pow(2).mean(-1, keepdim=True) + eps)

def rope_single(x, pos, rd, th):
    dp = rd // 2
    freqs = 1.0 / (th ** (torch.arange(0, rd, 2, dtype=torch.float32) / rd))
    a = pos * freqs; c = torch.cos(a).view(1,1,1,dp); s = torch.sin(a).view(1,1,1,dp)
    x1 = x[...,:dp]; x2 = x[...,dp:rd]; xp = x[...,rd:]
    return torch.cat([x1*c-x2*s, x2*c+x1*s, xp], dim=-1)

def rope_seq(x, rd, th):
    seq = x.shape[2]; dp = rd // 2
    pos = torch.arange(seq, dtype=torch.float32)
    freqs = 1.0 / (th ** (torch.arange(0, rd, 2, dtype=torch.float32) / rd))
    a = pos.unsqueeze(1) * freqs.unsqueeze(0)
    c = torch.cos(a).unsqueeze(0).unsqueeze(0); s = torch.sin(a).unsqueeze(0).unsqueeze(0)
    x1 = x[...,:dp]; x2 = x[...,dp:rd]; xp = x[...,rd:]
    return torch.cat([x1*c-x2*s, x2*c+x1*s, xp], dim=-1)

w = weights

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
    q = rope_seq(q, rotary_dim, theta); k = rope_seq(k, rotary_dim, theta)
    out = F.scaled_dot_product_attention(q, k, v).permute(0,2,1,3).reshape(1,-1,dim)
    x = res + F.linear(out, w[f"{p}.self_attn.o_proj.weight"])
    res = x; x = rms_norm(x, w[f"{p}.post_attention_layernorm.weight"])
    x = F.gelu(F.linear(x, w[f"{p}.mlp.fc1.weight"], w[f"{p}.mlp.fc1.bias"]))
    x = res + F.linear(x, w[f"{p}.mlp.fc2.weight"], w[f"{p}.mlp.fc2.bias"])
enc = rms_norm(x, w["model.encoder.layer_norm.weight"])
print(f"Encoder: {enc.shape}")


# Decode function
def decode(enc_h, max_tok=300, strategy=False):
    embed = w["model.decoder.embed_tokens.weight"]
    dnorm = w["model.decoder.norm.weight"]
    ckc=[]; cvc=[]
    for i in range(n_dec):
        p=f"model.decoder.layers.{i}"
        ckc.append(F.linear(enc_h,w[f"{p}.encoder_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3))
        cvc.append(F.linear(enc_h,w[f"{p}.encoder_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3))
    sk=[[] for _ in range(n_dec)]; sv=[[] for _ in range(n_dec)]
    toks=[BOS]; slp=0.0
    for step in range(max_tok):
        x=embed[toks[-1]].unsqueeze(0).unsqueeze(0)
        for i in range(n_dec):
            p=f"model.decoder.layers.{i}"
            res=x; x=rms_norm(x,w[f"{p}.input_layernorm.weight"])
            q=F.linear(x,w[f"{p}.self_attn.q_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
            k=F.linear(x,w[f"{p}.self_attn.k_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
            v=F.linear(x,w[f"{p}.self_attn.v_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
            q=rope_single(q,step,rotary_dim,theta); k=rope_single(k,step,rotary_dim,theta)
            sk[i].append(k); sv[i].append(v)
            out=F.scaled_dot_product_attention(q,torch.cat(sk[i],dim=2),torch.cat(sv[i],dim=2)).permute(0,2,1,3).reshape(1,1,dim)
            x=res+F.linear(out,w[f"{p}.self_attn.o_proj.weight"])
            res=x; x=rms_norm(x,w[f"{p}.post_attention_layernorm.weight"])
            cq=F.linear(x,w[f"{p}.encoder_attn.q_proj.weight"]).view(1,1,n_heads,head_dim).permute(0,2,1,3)
            out=F.scaled_dot_product_attention(cq,ckc[i],cvc[i]).permute(0,2,1,3).reshape(1,1,dim)
            x=res+F.linear(out,w[f"{p}.encoder_attn.o_proj.weight"])
            res=x; x=rms_norm(x,w[f"{p}.final_layernorm.weight"])
            fc1=F.linear(x,w[f"{p}.mlp.fc1.weight"],w[f"{p}.mlp.fc1.bias"]); inter=fc1.shape[-1]//2
            x=res+F.linear(F.silu(fc1[...,:inter])*fc1[...,inter:],w[f"{p}.mlp.fc2.weight"],w[f"{p}.mlp.fc2.bias"])
        logits=F.linear(rms_norm(x,dnorm),embed)[0,0]
        if strategy:
            for sid in SUPPRESS: logits[sid]=float('-inf')
            if len(toks)>=NGRAM:
                pfx=toks[-(NGRAM-1):]
                for j in range(len(toks)-NGRAM+1):
                    if toks[j:j+NGRAM-1]==pfx: logits[toks[j+NGRAM-1]]=float('-inf')
        nt=logits.argmax().item()
        lsm=logits-logits.logsumexp(dim=0); slp+=lsm[nt].item()
        if nt==EOS: break
        toks.append(nt)
    return toks, slp

def to_text(toks):
    t="".join(id2tok.get(i,f"<{i}>") for i in toks[1:])
    return t.replace("\u2581"," ").strip()

def cr(toks):
    if len(toks)<=2: return 1.0
    t=toks[1:]; bg=set()
    for i in range(len(t)-1): bg.add((t[i],t[i+1]))
    return (len(t)-1)/max(len(bg),1)

# Run
print("\n" + "="*60)
print("  moonshine-base-zh on maclaw.m4a (Chinese)")
print("="*60)

tg,lg = decode(enc, 300, False)
ts,ls = decode(enc, 300, True)
ng=len(tg)-1; ns=len(ts)-1

print(f"\n[GREEDY]    {ng} tok, cr={cr(tg):.2f}")
print(f'  "{to_text(tg)}"')
print(f"\n[STRATEGY]  {ns} tok, cr={cr(ts):.2f}")
print(f'  "{to_text(ts)}"')

if cr(tg) > 2.0 and cr(ts) < cr(tg):
    print(f"\n>>> IMPROVEMENT: cr {cr(tg):.2f} -> {cr(ts):.2f}")
print("\nDone.")
