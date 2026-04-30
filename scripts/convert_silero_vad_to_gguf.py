#!/usr/bin/env python3
"""
Convert Silero VAD v5 PyTorch model to GGUF format for the pure Go inference engine.

Usage:
    pip install torch gguf
    python scripts/convert_silero_vad_to_gguf.py --output models/silero-vad.gguf

The script downloads the official Silero VAD model from GitHub if not cached locally.
"""
import argparse
import os
import sys
import struct
import numpy as np

def download_silero_vad():
    """Download Silero VAD model using torch.hub."""
    import torch
    model, utils = torch.hub.load(
        repo_or_dir='snakers4/silero-vad',
        model='silero_vad',
        force_reload=False,
        onnx=False,
        trust_repo=True,
    )
    return model

def main():
    parser = argparse.ArgumentParser(description="Convert Silero VAD to GGUF")
    parser.add_argument("--output", type=str, default="silero-vad.gguf",
                        help="Output GGUF file path")
    parser.add_argument("--model-path", type=str, default="",
                        help="Path to silero_vad.pt (optional, downloads if not provided)")
    args = parser.parse_args()

    try:
        from gguf import GGUFWriter
    except ImportError:
        print("ERROR: pip install gguf")
        sys.exit(1)

    import torch

    # Load model
    if args.model_path and os.path.exists(args.model_path):
        print(f"Loading model from {args.model_path}")
        state_dict = torch.load(args.model_path, map_location="cpu", weights_only=True)
    else:
        print("Downloading Silero VAD from torch.hub...")
        model = download_silero_vad()
        state_dict = model.state_dict()

    print(f"State dict keys ({len(state_dict)}):")
    for k, v in state_dict.items():
        print(f"  {k}: {list(v.shape)} {v.dtype}")

    # Create GGUF writer
    writer = GGUFWriter(args.output, "silero-vad")

    # Write hyperparameters
    writer.add_int32("vad.sample_rate", 16000)
    writer.add_int32("vad.window_size", 512)
    writer.add_int32("vad.context_size", 64)
    writer.add_int32("vad.hidden_size", 128)
    writer.add_float32("vad.threshold", 0.5)
    writer.add_int32("vad.min_silence_ms", 100)
    writer.add_int32("vad.min_speech_ms", 250)

    # Write tensors
    # Silero VAD state_dict keys typically have "_model." prefix
    count = 0
    for name, tensor in state_dict.items():
        data = tensor.detach().float().numpy()
        # Keep all VAD weights as float32 (model is tiny, ~400KB)
        data = data.astype(np.float32)
        # Ensure tensor name has _model. prefix for compatibility
        if not name.startswith("_model."):
            name = "_model." + name
        writer.add_tensor(name, data)
        count += 1
        print(f"  wrote: {name} {list(data.shape)}")

    writer.write_header_to_file()
    writer.write_kv_data_to_file()
    writer.write_tensors_to_file()
    writer.close()

    size = os.path.getsize(args.output)
    print(f"\nDone: {args.output} ({size / 1024:.1f} KB, {count} tensors)")

if __name__ == "__main__":
    main()
