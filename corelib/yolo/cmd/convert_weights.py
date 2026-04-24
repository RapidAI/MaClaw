#!/usr/bin/env python3
"""Convert ultralytics YOLOv8 .pt weights to .yolow format for pure Go inference.

Usage:
    python convert_weights.py --input model.pt --output model.yolow

The .yolow format stores all weight tensors with fused BatchNorm parameters
in a simple binary format that Go can load without any Python dependency.
"""

import argparse
import os
import struct
import sys

import torch
from ultralytics import YOLO


def main():
    parser = argparse.ArgumentParser(description="Convert YOLOv8 .pt to .yolow")
    parser.add_argument("--input", required=True, help="Input .pt file path")
    parser.add_argument("--output", required=True, help="Output .yolow file path")
    args = parser.parse_args()

    print(f"Loading model from {args.input}...")
    model = YOLO(args.input)
    pt_model = model.model

    # Extract config
    nc = pt_model.nc if hasattr(pt_model, 'nc') else 1
    input_size = 640  # default
    if hasattr(model, 'overrides') and 'imgsz' in model.overrides:
        imgsz = model.overrides['imgsz']
        if isinstance(imgsz, (list, tuple)):
            input_size = imgsz[0]
        else:
            input_size = imgsz

    # Detect head reg_max
    reg_max = 16
    for m in pt_model.modules():
        if hasattr(m, 'reg_max'):
            reg_max = m.reg_max
            break

    print(f"Config: nc={nc}, input_size={input_size}, reg_max={reg_max}")

    # Collect all named parameters and buffers (includes BN running_mean/var)
    state = {}
    for name, param in pt_model.named_parameters():
        state[name] = param.detach().cpu().float().numpy()
    for name, buf in pt_model.named_buffers():
        state[name] = buf.detach().cpu().float().numpy()

    print(f"Collected {len(state)} weight tensors")

    # Write .yolow file
    with open(args.output, 'wb') as f:
        # Header (32 bytes)
        f.write(b'YOLW')                                    # magic
        f.write(struct.pack('<I', 1))                        # version
        f.write(struct.pack('<I', input_size))               # inputSize
        f.write(struct.pack('<I', nc))                       # nc
        f.write(struct.pack('<I', reg_max))                  # regMax
        f.write(struct.pack('<I', len(state)))               # numLayers
        f.write(b'\x00' * 8)                                 # reserved

        # Weight tensors
        for name, arr in sorted(state.items()):
            name_bytes = name.encode('utf-8')
            f.write(struct.pack('<I', len(name_bytes)))
            f.write(name_bytes)
            f.write(struct.pack('<I', len(arr.shape)))
            for s in arr.shape:
                f.write(struct.pack('<I', s))
            f.write(arr.tobytes())  # float32 little-endian (numpy default)

    file_size = os.path.getsize(args.output)
    print(f"Written {args.output} ({file_size / 1024 / 1024:.1f} MB)")
    print(f"Layers: {len(state)}")


if __name__ == "__main__":
    main()
