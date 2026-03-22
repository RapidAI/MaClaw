#!/usr/bin/env python3
"""
Convert SpeechBrain ECAPA-TDNN model to GGUF format for RapidSpeech.cpp

Usage:
  python convert_ecapa_tdnn.py --model speechbrain/spkrec-ecapa-voxceleb --output ecapa_tdnn.gguf

Requirements:
  pip install speechbrain torch numpy gguf
"""

import argparse
import sys
import struct
import numpy as np

# GGUF constants
GGUF_MAGIC = 0x46475547  # "GGUF"
GGUF_VERSION = 3
GGML_TYPE_F32 = 0
GGML_TYPE_F16 = 1


class GGUFWriter:
    """Minimal GGUF writer for ECAPA-TDNN conversion."""

    def __init__(self):
        self.kv_data = []
        self.tensors = []  # (name, shape, dtype, data_bytes)

    def add_string(self, key: str, value: str):
        self.kv_data.append(("string", key, value))

    def add_int32(self, key: str, value: int):
        self.kv_data.append(("int32", key, value))

    def _write_string(self, f, s: str):
        encoded = s.encode("utf-8")
        f.write(struct.pack("<Q", len(encoded)))
        f.write(encoded)

    def add_tensor(self, name: str, data: np.ndarray, dtype=GGML_TYPE_F32):
        if dtype == GGML_TYPE_F16:
            data = data.astype(np.float16)
        else:
            data = data.astype(np.float32)
        self.tensors.append((name, list(data.shape), dtype, data.tobytes()))

    def write(self, path: str):
        with open(path, "wb") as f:
            # Header
            f.write(struct.pack("<I", GGUF_MAGIC))
            f.write(struct.pack("<I", GGUF_VERSION))
            f.write(struct.pack("<Q", len(self.tensors)))
            f.write(struct.pack("<Q", len(self.kv_data)))

            # KV pairs
            for kv in self.kv_data:
                if kv[0] == "string":
                    self._write_string(f, kv[1])
                    f.write(struct.pack("<I", 8))  # GGUF_TYPE_STRING
                    self._write_string(f, kv[2])
                elif kv[0] == "int32":
                    self._write_string(f, kv[1])
                    f.write(struct.pack("<I", 4))  # GGUF_TYPE_INT32
                    f.write(struct.pack("<i", kv[2]))

            # Tensor infos
            data_offset = 0
            tensor_offsets = []
            for name, shape, dtype, data_bytes in self.tensors:
                self._write_string(f, name)
                n_dims = len(shape)
                f.write(struct.pack("<I", n_dims))
                for dim in shape:
                    f.write(struct.pack("<Q", dim))
                f.write(struct.pack("<I", dtype))
                # Align offset to 32 bytes
                aligned = (data_offset + 31) & ~31
                f.write(struct.pack("<Q", aligned))
                tensor_offsets.append(aligned)
                data_offset = aligned + len(data_bytes)

            # Padding to align data start
            current_pos = f.tell()
            aligned_start = (current_pos + 31) & ~31
            f.write(b"\x00" * (aligned_start - current_pos))

            data_base = f.tell()

            # Tensor data
            for i, (name, shape, dtype, data_bytes) in enumerate(self.tensors):
                target_pos = data_base + tensor_offsets[i]
                current = f.tell()
                if current < target_pos:
                    f.write(b"\x00" * (target_pos - current))
                f.write(data_bytes)

        print(f"Written {path} ({len(self.tensors)} tensors)")


def convert_tdnn_block(writer: GGUFWriter, state_dict: dict, src_prefix: str,
                       dst_prefix: str, use_f16: bool = False):
    """Convert a TDNNBlock (Conv1d + BatchNorm1d + ReLU)."""
    dtype = GGML_TYPE_F16 if use_f16 else GGML_TYPE_F32

    # Conv1d weight: PyTorch [out_ch, in_ch, kernel] -> keep as-is
    w = state_dict[f"{src_prefix}.conv.weight"].cpu().numpy()
    writer.add_tensor(f"{dst_prefix}.conv.weight", w, dtype)

    b = state_dict[f"{src_prefix}.conv.bias"].cpu().numpy()
    writer.add_tensor(f"{dst_prefix}.conv.bias", b, GGML_TYPE_F32)

    # BatchNorm1d
    writer.add_tensor(f"{dst_prefix}.norm.weight",
                      state_dict[f"{src_prefix}.norm.weight"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor(f"{dst_prefix}.norm.bias",
                      state_dict[f"{src_prefix}.norm.bias"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor(f"{dst_prefix}.norm.running_mean",
                      state_dict[f"{src_prefix}.norm.running_mean"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor(f"{dst_prefix}.norm.running_var",
                      state_dict[f"{src_prefix}.norm.running_var"].cpu().numpy(), GGML_TYPE_F32)


def convert_se_res2_block(writer: GGUFWriter, state_dict: dict,
                          src_prefix: str, dst_prefix: str,
                          res2_scale: int, has_shortcut: bool,
                          use_f16: bool = False):
    """Convert an SE-Res2Block."""
    # tdnn1
    convert_tdnn_block(writer, state_dict, f"{src_prefix}.tdnn1", f"{dst_prefix}.tdnn1", use_f16)

    # res2 sub-band convolutions (indices 1..scale-1 in SpeechBrain)
    for i in range(res2_scale - 1):
        sb_src = f"{src_prefix}.res2_convs.{i}"
        sb_dst = f"{dst_prefix}.res2_convs.{i}"
        convert_tdnn_block(writer, state_dict, sb_src, sb_dst, use_f16)

    # tdnn2
    convert_tdnn_block(writer, state_dict, f"{src_prefix}.tdnn2", f"{dst_prefix}.tdnn2", use_f16)

    # SE block (Conv1d used as FC)
    dtype = GGML_TYPE_F16 if use_f16 else GGML_TYPE_F32
    for layer_name in ["conv1", "conv2"]:
        w_key = f"{src_prefix}.se_module.{layer_name}.conv.weight"
        b_key = f"{src_prefix}.se_module.{layer_name}.conv.bias"
        # SE conv weights: [out, in, 1] -> squeeze to [out, in]
        w = state_dict[w_key].cpu().numpy().squeeze(-1)
        writer.add_tensor(f"{dst_prefix}.se.{layer_name}.weight", w, dtype)
        writer.add_tensor(f"{dst_prefix}.se.{layer_name}.bias",
                          state_dict[b_key].cpu().numpy(), GGML_TYPE_F32)

    # Shortcut
    if has_shortcut:
        convert_tdnn_block(writer, state_dict, f"{src_prefix}.shortcut",
                           f"{dst_prefix}.shortcut", use_f16)


def main():
    parser = argparse.ArgumentParser(description="Convert ECAPA-TDNN to GGUF")
    parser.add_argument("--model", type=str, default="speechbrain/spkrec-ecapa-voxceleb",
                        help="HuggingFace model ID or local path")
    parser.add_argument("--output", type=str, default="ecapa_tdnn.gguf",
                        help="Output GGUF file path")
    parser.add_argument("--f16", action="store_true",
                        help="Store conv weights in float16")
    args = parser.parse_args()

    print(f"Loading model: {args.model}")

    try:
        import torch
        from speechbrain.inference.speaker import EncoderClassifier
    except ImportError:
        print("Error: please install speechbrain and torch")
        print("  pip install speechbrain torch")
        sys.exit(1)

    classifier = EncoderClassifier.from_hparams(source=args.model)
    state_dict = classifier.mods.embedding_model.state_dict()

    # Print model structure for debugging
    print(f"Model has {len(state_dict)} parameters:")
    for k, v in state_dict.items():
        print(f"  {k}: {list(v.shape)}")

    writer = GGUFWriter()

    # Metadata
    writer.add_string("general.architecture", "ecapa-tdnn")
    writer.add_string("general.name", "ECAPA-TDNN Speaker Embedding")

    # Hyperparameters (detect from weights)
    layer0_w = state_dict["blocks.0.conv.weight"]
    n_mels = layer0_w.shape[1]
    channels = layer0_w.shape[0]

    # Detect res2_scale from number of res2_convs in first SE-Res2Block
    res2_scale = 1
    while f"blocks.1.res2_convs.{res2_scale - 1}.conv.weight" in state_dict:
        res2_scale += 1
    # res2_scale = number of sub-bands (res2_convs count + 1)

    # Detect embedding dim from final FC
    emb_dim = state_dict["blocks.6.fc.weight"].shape[0]

    writer.add_int32("ecapa.n_mels", int(n_mels))
    writer.add_int32("ecapa.channels", int(channels))
    writer.add_int32("ecapa.emb_dim", int(emb_dim))
    writer.add_int32("ecapa.res2_scale", int(res2_scale))

    print(f"Detected: n_mels={n_mels}, channels={channels}, emb_dim={emb_dim}, res2_scale={res2_scale}")

    # Convert weights
    use_f16 = args.f16

    # Layer 0: initial TDNN
    convert_tdnn_block(writer, state_dict, "blocks.0", "blocks.0", use_f16)

    # SE-Res2Blocks (blocks.1, blocks.2, blocks.3)
    for i in range(3):
        src = f"blocks.{i + 1}"
        dst = f"blocks.{i + 1}"
        # Detect shortcut from state_dict (not hardcoded)
        has_shortcut = f"{src}.shortcut.conv.weight" in state_dict
        convert_se_res2_block(writer, state_dict, src, dst, res2_scale,
                              has_shortcut, use_f16)

    # MFA conv (blocks.4)
    convert_tdnn_block(writer, state_dict, "blocks.4", "blocks.4", use_f16)

    # ASP (blocks.5)
    # asp_conv is a TDNNBlock
    convert_tdnn_block(writer, state_dict, "blocks.5.asp_conv", "blocks.5.asp_conv", use_f16)
    # asp attention linear
    dtype = GGML_TYPE_F16 if use_f16 else GGML_TYPE_F32
    writer.add_tensor("blocks.5.asp_attn.weight",
                      state_dict["blocks.5.asp_attn.weight"].cpu().numpy(), dtype)
    writer.add_tensor("blocks.5.asp_attn.bias",
                      state_dict["blocks.5.asp_attn.bias"].cpu().numpy(), GGML_TYPE_F32)

    # Final FC + BN (blocks.6)
    writer.add_tensor("blocks.6.fc.weight",
                      state_dict["blocks.6.fc.weight"].cpu().numpy(), dtype)
    writer.add_tensor("blocks.6.fc.bias",
                      state_dict["blocks.6.fc.bias"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor("blocks.6.norm.weight",
                      state_dict["blocks.6.norm.weight"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor("blocks.6.norm.bias",
                      state_dict["blocks.6.norm.bias"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor("blocks.6.norm.running_mean",
                      state_dict["blocks.6.norm.running_mean"].cpu().numpy(), GGML_TYPE_F32)
    writer.add_tensor("blocks.6.norm.running_var",
                      state_dict["blocks.6.norm.running_var"].cpu().numpy(), GGML_TYPE_F32)

    writer.write(args.output)
    print(f"Done! Output: {args.output}")


if __name__ == "__main__":
    main()
