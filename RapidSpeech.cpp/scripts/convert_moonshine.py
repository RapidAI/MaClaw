#!/usr/bin/env python3
"""
Convert Moonshine ASR model (HuggingFace/ONNX) to GGUF format.

Usage:
    python convert_moonshine.py --model-dir path/to/moonshine-tiny --output moonshine-tiny.gguf
    python convert_moonshine.py --model-dir path/to/moonshine-base --output moonshine-base.gguf --out-type f16

The model directory should contain:
    - preprocessor_config.json or config.json (hyperparameters)
    - pytorch_model.bin or model.safetensors (weights)
    - tokenizer.json (BPE vocabulary)
"""

import argparse
import json
import numpy as np
from pathlib import Path

try:
    import torch
except ImportError:
    torch = None

try:
    from safetensors import safe_open
except ImportError:
    safe_open = None

from gguf import GGUFWriter, GGMLQuantizationType


# Moonshine model configs (from moonshine source)
MODEL_CONFIGS = {
    "moonshine-tiny": {
        "encoder_dim": 288, "encoder_depth": 6, "encoder_heads": 8,
        "decoder_dim": 288, "decoder_depth": 6, "decoder_heads": 8,
        "vocab_size": 32768,
    },
    "moonshine-base": {
        "encoder_dim": 416, "encoder_depth": 8, "encoder_heads": 8,
        "decoder_dim": 416, "decoder_depth": 8, "decoder_heads": 8,
        "vocab_size": 32768,
    },
}


def load_weights(model_dir: Path):
    """Load weights from pytorch_model.bin or model.safetensors."""
    pt_path = model_dir / "pytorch_model.bin"
    st_path = model_dir / "model.safetensors"

    if pt_path.exists() and torch is not None:
        print(f"Loading weights from {pt_path}")
        state_dict = torch.load(str(pt_path), map_location="cpu", weights_only=True)
        for name, tensor in state_dict.items():
            yield name, tensor.detach().float().numpy()
    elif st_path.exists() and safe_open is not None:
        print(f"Loading weights from {st_path}")
        with safe_open(str(st_path), framework="numpy") as f:
            for name in f.keys():
                yield name, f.get_tensor(name).astype(np.float32)
    else:
        raise FileNotFoundError(
            f"No weights found in {model_dir}. "
            "Need pytorch_model.bin (with torch) or model.safetensors (with safetensors)."
        )


def load_tokenizer(model_dir: Path):
    """Load BPE tokens from tokenizer.json."""
    tok_path = model_dir / "tokenizer.json"
    if not tok_path.exists():
        print(f"Warning: {tok_path} not found, skipping vocabulary")
        return []

    with open(tok_path, "r", encoding="utf-8") as f:
        tok_data = json.load(f)

    # Extract token list from HuggingFace tokenizer format
    vocab = tok_data.get("model", {}).get("vocab", {})
    if not vocab:
        # Try added_tokens
        added = tok_data.get("added_tokens", [])
        tokens = [t.get("content", "") for t in added]
        return tokens

    # Sort by ID
    sorted_tokens = sorted(vocab.items(), key=lambda x: x[1])
    return [t[0] for t in sorted_tokens]

def detect_config(model_dir: Path):
    """Auto-detect model config from directory name or config files."""
    config_path = model_dir / "config.json"
    if config_path.exists():
        with open(config_path, "r") as f:
            cfg = json.load(f)

        # Try to get dim from config, or infer from weights
        enc_dim = cfg.get("d_model", cfg.get("encoder_dim", None))
        if enc_dim is None:
            # Infer from conv1 weight shape
            try:
                if (model_dir / "model.safetensors").exists() and safe_open is not None:
                    with safe_open(str(model_dir / "model.safetensors"), framework="numpy") as f:
                        conv1 = f.get_tensor("model.encoder.conv1.weight")
                        enc_dim = conv1.shape[0]
                        print(f"Inferred encoder_dim={enc_dim} from conv1 weight shape")
            except Exception:
                pass
            if enc_dim is None:
                enc_dim = 288  # default

        return {
            "encoder_dim": enc_dim,
            "encoder_depth": cfg.get("encoder_num_hidden_layers", cfg.get("encoder_num_layers", cfg.get("encoder_depth", 6))),
            "encoder_heads": cfg.get("encoder_num_attention_heads", cfg.get("encoder_attention_heads", cfg.get("encoder_heads", 8))),
            "decoder_dim": enc_dim,  # same as encoder for moonshine
            "decoder_depth": cfg.get("decoder_num_hidden_layers", cfg.get("decoder_num_layers", cfg.get("decoder_depth", 6))),
            "decoder_heads": cfg.get("decoder_num_attention_heads", cfg.get("decoder_attention_heads", cfg.get("decoder_heads", 8))),
            "vocab_size": cfg.get("vocab_size", 32768),
        }

    # Guess from directory name
    dir_name = model_dir.name.lower()
    for key, cfg in MODEL_CONFIGS.items():
        if key in dir_name:
            print(f"Auto-detected config: {key}")
            return cfg

    print("Warning: could not detect config, using moonshine-tiny defaults")
    return MODEL_CONFIGS["moonshine-tiny"]


def main():
    parser = argparse.ArgumentParser(description="Convert Moonshine model to GGUF")
    parser.add_argument("--model-dir", type=str, required=True,
                        help="Directory with model weights and config")
    parser.add_argument("--output", type=str, required=True,
                        help="Output .gguf file path")
    parser.add_argument("--out-type", type=str, choices=["f32", "f16"], default="f32",
                        help="Output data type")
    parser.add_argument("--streaming", action="store_true", default=False,
                        help="Mark model as streaming variant")
    args = parser.parse_args()

    model_dir = Path(args.model_dir)
    ftype = GGMLQuantizationType.F32 if args.out_type == "f32" else GGMLQuantizationType.F16

    # Detect config
    config = detect_config(model_dir)

    writer = GGUFWriter(args.output, "moonshine")

    # Write hyperparameters
    writer.add_int32("moonshine.encoder_dim", config["encoder_dim"])
    writer.add_int32("moonshine.encoder_depth", config["encoder_depth"])
    writer.add_int32("moonshine.encoder_heads", config["encoder_heads"])
    writer.add_int32("moonshine.decoder_dim", config["decoder_dim"])
    writer.add_int32("moonshine.decoder_depth", config["decoder_depth"])
    writer.add_int32("moonshine.decoder_heads", config["decoder_heads"])
    writer.add_int32("moonshine.vocab_size", config["vocab_size"])
    writer.add_int32("moonshine.max_seq_len", 448)
    writer.add_int32("moonshine.bos_id", 1)
    writer.add_int32("moonshine.eos_id", 2)
    writer.add_int32("moonshine.sample_rate", 16000)
    writer.add_bool("moonshine.is_streaming", args.streaming)
    writer.add_float32("moonshine.rope_theta", 10000.0)
    writer.add_float32("moonshine.partial_rotary_factor", 0.9)

    # Load and write vocabulary
    tokens = load_tokenizer(model_dir)
    if tokens:
        writer.add_int32("tokenizer.vocab_size", len(tokens))
        writer.add_token_list(tokens)
        print(f"Wrote {len(tokens)} vocabulary tokens")

    # Write tensors
    # Note: gguf stores numpy arrays as-is, and ggml reverses dimension order
    # when reading. So PyTorch [OC, IC, K] -> ggml [K, IC, OC] automatically.
    # No manual transpose needed.
    print("Writing tensors...")
    tensor_count = 0
    for name, data in load_weights(model_dir):
        # Convert data type
        if ftype == GGMLQuantizationType.F16 and data.ndim >= 2:
            data = data.astype(np.float16)
        else:
            data = data.astype(np.float32)

        writer.add_tensor(name, data)
        tensor_count += 1

    print(f"Wrote {tensor_count} tensors")

    writer.write_header_to_file()
    writer.write_kv_data_to_file()
    writer.write_tensors_to_file()
    writer.close()

    print(f"Successfully exported to {args.output}")


if __name__ == "__main__":
    main()
