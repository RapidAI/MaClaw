#!/usr/bin/env python3
"""
Convert MeloTTS English checkpoint to GGUF (fp32) and dump layer-by-layer
reference outputs for Go comparison.

Usage:
  python convert_and_compare.py

Outputs:
  melotts-en-fp32.gguf     — GGUF model from the EN checkpoint
  ref_layer_*.bin           — per-layer reference outputs (raw float32)
"""
import os, sys, json, struct, math
import numpy as np
import torch

GGUF_MAGIC = 0x46554747   # "GGUF" correct LE magic
GGUF_VERSION = 3
GGML_TYPE_F32 = 0

class GGUFWriter:
    def __init__(self):
        self.kv_data = []
        self.tensors = []

    def add_string(self, key, value):
        self.kv_data.append(("string", key, value))

    def add_int32(self, key, value):
        self.kv_data.append(("int32", key, int(value)))

    def _write_string(self, f, s):
        encoded = s.encode("utf-8")
        f.write(struct.pack("<Q", len(encoded)))
        f.write(encoded)

    def add_tensor(self, name, data):
        data = data.astype(np.float32)
        self.tensors.append((name, list(data.shape), GGML_TYPE_F32, data.tobytes()))

    def write(self, path):
        with open(path, "wb") as f:
            f.write(struct.pack("<I", GGUF_MAGIC))
            f.write(struct.pack("<I", GGUF_VERSION))
            f.write(struct.pack("<Q", len(self.tensors)))
            f.write(struct.pack("<Q", len(self.kv_data)))

            for kv in self.kv_data:
                if kv[0] == "string":
                    self._write_string(f, kv[1])
                    f.write(struct.pack("<I", 8))
                    self._write_string(f, kv[2])
                elif kv[0] == "int32":
                    self._write_string(f, kv[1])
                    f.write(struct.pack("<I", 4))
                    f.write(struct.pack("<i", kv[2]))

            data_offset = 0
            tensor_offsets = []
            for name, shape, dtype, data_bytes in self.tensors:
                self._write_string(f, name)
                f.write(struct.pack("<I", len(shape)))
                for dim in shape:
                    f.write(struct.pack("<Q", dim))
                f.write(struct.pack("<I", dtype))
                aligned = (data_offset + 31) & ~31
                f.write(struct.pack("<Q", aligned))
                tensor_offsets.append(aligned)
                data_offset = aligned + len(data_bytes)

            current_pos = f.tell()
            aligned_start = (current_pos + 31) & ~31
            f.write(b"\x00" * (aligned_start - current_pos))
            data_base = f.tell()

            for i, (name, shape, dtype, data_bytes) in enumerate(self.tensors):
                target_pos = data_base + tensor_offsets[i]
                current = f.tell()
                if current < target_pos:
                    f.write(b"\x00" * (target_pos - current))
                f.write(data_bytes)

        print(f"Written {path} ({len(self.tensors)} tensors, {os.path.getsize(path)/(1024*1024):.1f} MB)")


def main():
    outdir = os.path.dirname(os.path.abspath(__file__))
    model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")
    ckpt_path = os.path.join(model_dir, "checkpoint.pth")
    config_path = os.path.join(model_dir, "config.json")

    with open(config_path, encoding='utf-8') as f:
        config = json.load(f)

    print(f"Loading checkpoint: {ckpt_path}")
    ckpt = torch.load(ckpt_path, map_location="cpu", weights_only=True)
    sd = ckpt.get("model", ckpt)

    # ── Step 1: Convert to GGUF ──
    print("\n=== Converting to GGUF ===")
    writer = GGUFWriter()
    writer.add_string("general.architecture", "openvoice2")
    writer.add_string("general.name", "MeloTTS EN (test)")
    writer.add_string("general.language", "EN")
    writer.add_int32("openvoice2.hidden_channels", 192)
    writer.add_int32("openvoice2.sample_rate", 22050)
    writer.add_int32("openvoice2.hop_length", 256)
    writer.add_int32("openvoice2.n_fft", 1024)

    n = 0
    prefix_map = {
        "enc_p.": "text_encoder.",
        "dp.": "duration_predictor.",
        "flow.": "flow_decoder.",
        "enc_q.": "posterior_encoder.",
        "dec.": "vocoder.",
    }
    for k, v in sd.items():
        mapped = False
        for src, dst in prefix_map.items():
            if k.startswith(src):
                writer.add_tensor(k.replace(src, dst, 1), v.cpu().numpy())
                n += 1
                mapped = True
                break
        if not mapped and k.startswith("emb_"):
            writer.add_tensor(k, v.cpu().numpy())
            n += 1

    gguf_path = os.path.join(outdir, "melotts-en-fp32.gguf")
    writer.write(gguf_path)
    print(f"Converted {n} tensors")

    # ── Step 2: Run inference and dump intermediates ──
    print("\n=== Running Python inference for reference ===")

    phone_ids = [0, 49, 0, 127, 0, 70, 0, 80, 0]  # "Hello" with blanks
    T = len(phone_ids)
    hidden = 192
    inter = 192

    with torch.no_grad():
        # Embedding
        emb_w = sd["enc_p.emb.weight"]
        tone_w = sd["enc_p.tone_emb.weight"]
        lang_w = sd["enc_p.language_emb.weight"]

        x = torch.zeros(1, T, hidden)
        for t in range(T):
            x[0, t] = emb_w[phone_ids[t]] + tone_w[0] + lang_w[2]
        x = x * math.sqrt(hidden)
        x = x.transpose(1, 2)  # [1, hidden, T]
        dump(outdir, "ref_01_embedding", x)

        # Speaker embedding
        emb_g = sd["emb_g.weight"]  # [1, 256]
        g = emb_g[0].unsqueeze(0).unsqueeze(-1)  # [1, 256, 1]
        dump(outdir, "ref_00_speaker_emb", g)

        # Text Encoder: we need to run the full encoder
        # Build it manually layer by layer
        x_mask = torch.ones(1, 1, T)

        # Speaker conditioning via spk_emb_linear
        if "enc_p.encoder.spk_emb_linear.weight" in sd:
            spk_w = sd["enc_p.encoder.spk_emb_linear.weight"]  # [192, 256]
            spk_b = sd["enc_p.encoder.spk_emb_linear.bias"]    # [192]
            g_proj = torch.matmul(spk_w, g.squeeze(-1).T).T.unsqueeze(-1) + spk_b.unsqueeze(0).unsqueeze(-1)
            # g_proj: [1, 192, 1] — broadcast add to x
            x = x + g_proj
            dump(outdir, "ref_02_after_spk_cond", x)

        # Encoder layers
        for li in range(6):
            p = f"enc_p.encoder."
            # Pre-attention norm
            norm1_g = sd[f"{p}norm_layers_1.{li}.gamma"]
            norm1_b = sd[f"{p}norm_layers_1.{li}.beta"]

            # Save pre-norm x for residual
            residual = x.clone()

            # LayerNorm per time step
            x_normed = torch.zeros_like(x)
            for t in range(T):
                col = x[0, :, t]
                mean = col.mean()
                var = col.var(unbiased=False)
                col_normed = (col - mean) / torch.sqrt(var + 1e-5) * norm1_g + norm1_b
                x_normed[0, :, t] = col_normed

            # Q, K, V projections
            qw = sd[f"{p}attn_layers.{li}.conv_q.weight"]  # [192, 192, 1]
            qb = sd[f"{p}attn_layers.{li}.conv_q.bias"]
            kw = sd[f"{p}attn_layers.{li}.conv_k.weight"]
            kb = sd[f"{p}attn_layers.{li}.conv_k.bias"]
            vw = sd[f"{p}attn_layers.{li}.conv_v.weight"]
            vb = sd[f"{p}attn_layers.{li}.conv_v.bias"]

            q = torch.nn.functional.conv1d(x_normed, qw, qb)
            k = torch.nn.functional.conv1d(x_normed, kw, kb)
            v = torch.nn.functional.conv1d(x_normed, vw, vb)

            # Multi-head attention (2 heads, head_dim=96)
            n_heads = 2
            head_dim = hidden // n_heads
            scale = 1.0 / math.sqrt(head_dim)

            # Reshape: [1, hidden, T] → [1, n_heads, head_dim, T]
            q_h = q.view(1, n_heads, head_dim, T)
            k_h = k.view(1, n_heads, head_dim, T)
            v_h = v.view(1, n_heads, head_dim, T)

            # Attention: [n_heads, T, T]
            scores = torch.matmul(q_h.transpose(2, 3), k_h) * scale  # [1, n_heads, T, T]
            attn_weights = torch.softmax(scores, dim=-1)
            attn_out = torch.matmul(attn_weights, v_h.transpose(2, 3))  # [1, n_heads, T, head_dim]
            attn_out = attn_out.transpose(2, 3).contiguous().view(1, hidden, T)

            # Output projection
            ow = sd[f"{p}attn_layers.{li}.conv_o.weight"]
            ob = sd[f"{p}attn_layers.{li}.conv_o.bias"]
            o = torch.nn.functional.conv1d(attn_out, ow, ob)

            # Residual
            x = residual + o

            # Post-attention norm
            norm2_g = sd[f"{p}norm_layers_2.{li}.gamma"] if f"{p}norm_layers_2.{li}.gamma" in sd else sd.get(f"{p}norm_layers_1.{li}.gamma")
            norm2_b = sd[f"{p}norm_layers_2.{li}.beta"] if f"{p}norm_layers_2.{li}.beta" in sd else sd.get(f"{p}norm_layers_1.{li}.beta")

            residual2 = x.clone()
            x_normed2 = torch.zeros_like(x)
            for t in range(T):
                col = x[0, :, t]
                mean = col.mean()
                var = col.var(unbiased=False)
                x_normed2[0, :, t] = (col - mean) / torch.sqrt(var + 1e-5) * norm2_g + norm2_b

            # FFN
            fw1 = sd[f"{p}ffn_layers.{li}.conv_1.weight"]
            fb1 = sd[f"{p}ffn_layers.{li}.conv_1.bias"]
            fw2 = sd[f"{p}ffn_layers.{li}.conv_2.weight"]
            fb2 = sd[f"{p}ffn_layers.{li}.conv_2.bias"]

            ffn = torch.nn.functional.conv1d(x_normed2, fw1, fb1, padding=1)
            ffn = torch.relu(ffn)
            ffn = torch.nn.functional.conv1d(ffn, fw2, fb2, padding=1)

            x = residual2 + ffn

            if li == 0:
                dump(outdir, "ref_03_enc_layer0", x)

        dump(outdir, "ref_04_enc_final", x)

        # Projection
        proj_w = sd["enc_p.proj.weight"]  # [384, 192, 1]
        proj_b = sd["enc_p.proj.bias"]    # [384]
        stats = torch.nn.functional.conv1d(x, proj_w, proj_b)
        m_p = stats[:, :inter, :]
        logs_p = stats[:, inter:, :]
        dump(outdir, "ref_05_m_p", m_p)
        dump(outdir, "ref_06_logs_p", logs_p)

        # Duration predictor
        dp_x = x.detach()
        # Conditioning
        if "dp.cond.weight" in sd:
            cond_w = sd["dp.cond.weight"]  # [192, 256, 1]
            cond_b = sd["dp.cond.bias"]
            g_cond = torch.nn.functional.conv1d(g, cond_w, cond_b)
            dp_x = dp_x + g_cond

        dp_c1w = sd["dp.conv_1.weight"]
        dp_c1b = sd["dp.conv_1.bias"]
        dp_y = torch.nn.functional.conv1d(dp_x, dp_c1w, dp_c1b, padding=1)
        dp_y = torch.relu(dp_y)
        # LayerNorm
        dp_n1g = sd.get("dp.norm_1.gamma", sd.get("dp.norm_1.weight"))
        dp_n1b = sd.get("dp.norm_1.beta", sd.get("dp.norm_1.bias"))
        if dp_n1g is not None:
            for t in range(T):
                col = dp_y[0, :, t]
                mean = col.mean()
                var = col.var(unbiased=False)
                dp_y[0, :, t] = (col - mean) / torch.sqrt(var + 1e-5) * dp_n1g + dp_n1b

        dp_c2w = sd["dp.conv_2.weight"]
        dp_c2b = sd["dp.conv_2.bias"]
        dp_y = torch.nn.functional.conv1d(dp_y, dp_c2w, dp_c2b, padding=1)
        dp_y = torch.relu(dp_y)
        dp_n2g = sd.get("dp.norm_2.gamma", sd.get("dp.norm_2.weight"))
        dp_n2b = sd.get("dp.norm_2.beta", sd.get("dp.norm_2.bias"))
        if dp_n2g is not None:
            for t in range(T):
                col = dp_y[0, :, t]
                mean = col.mean()
                var = col.var(unbiased=False)
                dp_y[0, :, t] = (col - mean) / torch.sqrt(var + 1e-5) * dp_n2g + dp_n2b

        dp_pw = sd["dp.proj.weight"]
        dp_pb = sd["dp.proj.bias"]
        logw = torch.nn.functional.conv1d(dp_y, dp_pw, dp_pb)
        dump(outdir, "ref_07_logw", logw)

        # Durations
        w = torch.exp(logw) * x_mask
        w_ceil = torch.ceil(w)
        durations = w_ceil.squeeze().long().tolist()
        T_mel = sum(durations)
        print(f"  Durations: {durations}, T_mel={T_mel}")
        dump(outdir, "ref_08_durations", w_ceil)

    print(f"\nAll reference files saved to {outdir}/ref_*.bin")
    print("Run Go comparison tests with: go test -v -run TestCompareLayer ./corelib/tts/")


def dump(outdir, name, tensor):
    """Save tensor as raw float32 binary."""
    data = tensor.detach().squeeze(0).numpy().astype(np.float32)
    path = os.path.join(outdir, f"{name}.bin")
    data.tofile(path)
    print(f"  {name}: {list(data.shape)}, mean={data.mean():.6f}, std={data.std():.6f}, "
          f"min={data.min():.4f}, max={data.max():.4f}")


if __name__ == "__main__":
    main()
