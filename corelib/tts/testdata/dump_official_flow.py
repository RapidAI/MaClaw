#!/usr/bin/env python3
"""Dump official flow input/output for Go comparison."""
import os, json, numpy as np, torch

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

# Use fixed z_p for reproducible comparison
torch.manual_seed(42)

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
    enc_out, m_p, logs_p, x_mask = model.enc_p(x, x_lengths, tone, language, bert, ja_bert, g=g_p)

    # Duration
    logw = model.dp(enc_out, x_mask, g=g)
    w = torch.exp(logw) * x_mask
    w_ceil = torch.ceil(w)
    y_lengths = torch.clamp_min(torch.sum(w_ceil, [1, 2]), 1).long()
    T_mel = y_lengths.item()

    from melo.commons import generate_path, sequence_mask
    y_mask = torch.unsqueeze(sequence_mask(y_lengths, None), 1).to(x_mask.dtype)
    attn_mask = torch.unsqueeze(x_mask, 2) * torch.unsqueeze(y_mask, -1)
    attn = generate_path(w_ceil, attn_mask)

    m_p_exp = torch.matmul(attn.squeeze(1), m_p.transpose(1, 2)).transpose(1, 2)
    logs_p_exp = torch.matmul(attn.squeeze(1), logs_p.transpose(1, 2)).transpose(1, 2)

    # Fixed z_p for comparison
    z_p = m_p_exp + torch.randn_like(m_p_exp) * torch.exp(logs_p_exp) * 0.667

    def dump(name, t):
        d = t.squeeze(0).cpu().numpy().astype(np.float32)
        d.tofile(os.path.join(outdir, f"{name}.bin"))
        print(f"  {name}: shape={list(d.shape)}, mean={d.mean():.4f}, std={d.std():.4f}")

    dump("ref_official_flow_z_p", z_p)
    dump("ref_official_flow_g", g)
    dump("ref_official_flow_y_mask", y_mask)

    # Run flow reverse
    z = model.flow(z_p, y_mask, g=g, reverse=True)
    dump("ref_official_flow_z_out", z)

    # Also dump flow layer by layer
    z_step = z_p.clone()
    for i in range(len(model.flow.flows) - 1, -1, -1):
        flow_layer = model.flow.flows[i]
        z_step = flow_layer(z_step, y_mask, g=g, reverse=True)
        if hasattr(flow_layer, 'pre'):  # TransformerCouplingLayer
            dump(f"ref_official_flow_step_{i}", z_step)

print(f"\nT_mel={T_mel}")
print("Done!")
