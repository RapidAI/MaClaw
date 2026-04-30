#!/usr/bin/env python3
"""Download MeloTTS ZH model and convert to GGUF."""
import os, sys

outdir = os.path.dirname(os.path.abspath(__file__))

print("Loading MeloTTS ZH model...")

# Download the ZH checkpoint directly
import torch
from huggingface_hub import hf_hub_download

# MeloTTS ZH model is at myshell-ai/MeloTTS-Chinese or can be loaded via melo
# Try direct download
try:
    ckpt_path = hf_hub_download(repo_id="myshell-ai/MeloTTS-Chinese", filename="checkpoint.pth")
    config_dl_path = hf_hub_download(repo_id="myshell-ai/MeloTTS-Chinese", filename="config.json")
    print(f"Downloaded: {ckpt_path}")
except Exception as e:
    print(f"HF download failed: {e}")
    # Try alternative: use melo.models directly
    print("Trying melo.models import...")
    from melo import models as melo_models
    # Need to find the ZH checkpoint path
    import melo
    melo_dir = os.path.dirname(melo.__file__)
    print(f"Melo dir: {melo_dir}")
    sys.exit(1)

import json
with open(config_dl_path, encoding='utf-8') as f:
    config_raw = json.load(f)

ckpt = torch.load(ckpt_path, map_location="cpu", weights_only=False)
sd = ckpt.get("model", ckpt)

print(f"Model loaded: {len(sd)} parameters")
total_params = sum(v.numel() for v in sd.values())
print(f"Total params: {total_params:,}")

# Save config
import json
config = config_raw
mc = config.get("model", {})
dc = config.get("data", {})

config_path = os.path.join(outdir, "melotts-zh-config.json")
with open(config_path, "w", encoding="utf-8") as f:
    json.dump(config, f, ensure_ascii=False, indent=2)
print(f"Config saved: {config_path}")

# Convert to GGUF
import struct, numpy as np

GGUF_MAGIC = 0x46554747
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
                    self._write_string(f, kv[1]); f.write(struct.pack("<I", 8)); self._write_string(f, kv[2])
                elif kv[0] == "int32":
                    self._write_string(f, kv[1]); f.write(struct.pack("<I", 4)); f.write(struct.pack("<i", kv[2]))
            data_offset = 0; tensor_offsets = []
            for name, shape, dtype, data_bytes in self.tensors:
                self._write_string(f, name); f.write(struct.pack("<I", len(shape)))
                for dim in shape: f.write(struct.pack("<Q", dim))
                f.write(struct.pack("<I", dtype))
                aligned = (data_offset + 31) & ~31
                f.write(struct.pack("<Q", aligned)); tensor_offsets.append(aligned)
                data_offset = aligned + len(data_bytes)
            current_pos = f.tell(); aligned_start = (current_pos + 31) & ~31
            f.write(b"\x00" * (aligned_start - current_pos)); data_base = f.tell()
            for i, (name, shape, dtype, data_bytes) in enumerate(self.tensors):
                target_pos = data_base + tensor_offsets[i]; current = f.tell()
                if current < target_pos: f.write(b"\x00" * (target_pos - current))
                f.write(data_bytes)
        print(f"Written {path} ({len(self.tensors)} tensors, {os.path.getsize(path)/(1024*1024):.1f} MB)")

writer = GGUFWriter()
writer.add_string("general.architecture", "openvoice2")
writer.add_string("general.name", "MeloTTS ZH")
writer.add_string("general.language", "ZH")
writer.add_int32("openvoice2.hidden_channels", mc.get("hidden_channels", 192))
writer.add_int32("openvoice2.sample_rate", dc.get("sampling_rate", 44100))
writer.add_int32("openvoice2.hop_length", dc.get("hop_length", 512))

prefix_map = {"enc_p.": "text_encoder.", "dp.": "duration_predictor.",
              "flow.": "flow_decoder.", "enc_q.": "posterior_encoder.", "dec.": "vocoder."}
n = 0
for k, v in sd.items():
    mapped = False
    for src, dst in prefix_map.items():
        if k.startswith(src):
            writer.add_tensor(k.replace(src, dst, 1), v.cpu().numpy()); n += 1; mapped = True; break
    if not mapped and k.startswith("emb_"):
        writer.add_tensor(k, v.cpu().numpy()); n += 1

gguf_path = os.path.join(outdir, "melotts-zh-fp32.gguf")
writer.write(gguf_path)
print(f"Converted {n} tensors")

# Quick test skipped (torchaudio not available)
print("\nDone! Use melotts-zh-fp32.gguf with Go synthesizer.")
