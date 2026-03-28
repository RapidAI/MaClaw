#!/usr/bin/env python3
"""Re-run zh decode with proper byte-token merging."""
import os, sys, re, json
os.chdir(os.path.join(os.path.dirname(__file__), ".."))

# Load the token IDs from last run and decode properly
with open("models/moonshine-base-zh/tokenizer.json", "r", encoding="utf-8") as f:
    tok_data = json.load(f)
vocab = tok_data.get("model", {}).get("vocab", {})
id2tok = {v: k for k, v in vocab.items()}

def decode_tokens(token_ids):
    """Decode token IDs with proper byte-token merging for SentencePiece."""
    raw_pieces = []
    for tid in token_ids:
        if tid in (1, 2):  # BOS, EOS
            continue
        tok = id2tok.get(tid, "")
        raw_pieces.append(tok)

    # Join all pieces
    text = "".join(raw_pieces)

    # Replace SentencePiece space marker
    text = text.replace("\u2581", " ")

    # Merge byte tokens: <0xNN> -> actual bytes
    byte_pattern = re.compile(r"<0x([0-9A-Fa-f]{2})>")
    parts = byte_pattern.split(text)
    result = bytearray()
    for i, part in enumerate(parts):
        if i % 2 == 0:
            result.extend(part.encode("utf-8"))
        else:
            result.append(int(part, 16))

    return result.decode("utf-8", errors="replace").strip()

# Now re-run the actual decode
import numpy as np, torch, torch.nn.functional as F, wave
from pathlib import Path
from safetensors import safe_open

model_dir = "models/moonshine-base-zh"
with open(os.path.join(model_dir, "config.json")) as f:
    cfg = json.load(f)

weights = {}
with safe_open(os.path.join(model_dir, "model.safetensors"), framework="pt", device="cpu") as f:
    for name in f.keys(): weights[name] = f.get_tensor(name).float()

wav_path = "test/real_speech/maclaw_16k.wav"
with wave.open(wav_path, "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
print(f"Audio: {len(pcm)/16000:.1f}s")

dim = 416; n_heads = 8; head_dim = 52; rotary_dim = 32; theta = 10000.0
n_enc = 8; n_dec = 8; BOS = 1; EOS = 2
SUPPRESS = [0, 1]; NGRAM = 3
w = weights

def rms_norm(x, wt, eps=1e-5):
    return wt * x * torch.rsqrt(x.pow(2).mean(-1, keepdim=True) + eps)
def rope_single(x, pos, rd, th):
    dp=rd//2; freqs=1.0/(th**(torch.arange(0,rd,2,dtype=torch.float32)/rd))
    a=pos*freqs; c=torch.cos(a).view(1,1,1,dp); s=torch.sin(a).view(1,1,1,dp)
    return torch.cat([x[...,:dp]*c-x[...,dp:rd]*s, x[...,dp:rd]*c+x[...,:dp]*s, x[...,rd:]], dim=-1)
def rope_seq(x, rd, th):
    seq=x.shape[2]; dp=rd//2; pos=torch.arange(seq,dtype=torch.float32)
    freqs=1.0/(th**(torch.arange(0,rd,2,dtype=torch.float32)/rd))
    a=pos.unsqueeze(1)*freqs.unsqueeze(0); c=torch.cos(a).unsqueeze(0).unsqueeze(0); s=torch.sin(a).unsqueeze(0).unsqueeze(0)
    return torch.cat([x[...,:dp]*c-x[...,dp:rd]*s, x[...,dp:rd]*c+x[...,:dp]*s, x[...,rd:]], dim=-1)

# Encode
x = torch.from_numpy(pcm).unsqueeze(0).unsqueeze(1)
x = F.conv1d(x, w["model.encoder.conv1.weight"], stride=64); x = torch.tanh(x)
x = F.group_norm(x, 1, w["model.encoder.groupnorm.weight"], w["model.encoder.groupnorm.bias"])
x = F.conv1d(x, w["model.encoder.conv2.weight"], w["model.encoder.conv2.bias"], stride=3); x = F.gelu(x)
x = F.conv1d(x, w["model.encoder.conv3.weight"], w["model.encoder.conv3.bias"], stride=2); x = F.gelu(x)
x = x.permute(0, 2, 1)
for i in range(n_enc):
    p=f"model.encoder.layers.{i}"
    res=x; x=rms_norm(x,w[f"{p}.input_layernorm.weight"])
    q=F.linear(x,w[f"{p}.self_attn.q_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    k=F.linear(x,w[f"{p}.self_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    v=F.linear(x,w[f"{p}.self_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3)
    q=rope_seq(q,rotary_dim,theta); k=rope_seq(k,rotary_dim,theta)
    out=F.scaled_dot_product_attention(q,k,v).permute(0,2,1,3).reshape(1,-1,dim)
    x=res+F.linear(out,w[f"{p}.self_attn.o_proj.weight"])
    res=x; x=rms_norm(x,w[f"{p}.post_attention_layernorm.weight"])
    x=F.gelu(F.linear(x,w[f"{p}.mlp.fc1.weight"],w[f"{p}.mlp.fc1.bias"]))
    x=res+F.linear(x,w[f"{p}.mlp.fc2.weight"],w[f"{p}.mlp.fc2.bias"])
enc=rms_norm(x,w["model.encoder.layer_norm.weight"])

def decode(enc_h, max_tok=300, strategy=False):
    embed=w["model.decoder.embed_tokens.weight"]; dnorm=w["model.decoder.norm.weight"]
    ckc=[]; cvc=[]
    for i in range(n_dec):
        p=f"model.decoder.layers.{i}"
        ckc.append(F.linear(enc_h,w[f"{p}.encoder_attn.k_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3))
        cvc.append(F.linear(enc_h,w[f"{p}.encoder_attn.v_proj.weight"]).view(1,-1,n_heads,head_dim).permute(0,2,1,3))
    sk=[[] for _ in range(n_dec)]; sv=[[] for _ in range(n_dec)]
    toks=[BOS]
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
        if nt==EOS: break
        toks.append(nt)
    return toks

print("\n" + "="*60)
print("  moonshine-base-zh on maclaw.m4a")
print("="*60)

tg = decode(enc, 300, False)
ts = decode(enc, 300, True)

text_g = decode_tokens(tg)
text_s = decode_tokens(ts)

print(f"\n[GREEDY]    {len(tg)-1} tokens")
print(f'  "{text_g[:200]}"')
print(f"\n[STRATEGY]  {len(ts)-1} tokens")
print(f'  "{text_s[:200]}"')
print(f"\n  Raw token IDs (strategy): {ts[:30]}")
