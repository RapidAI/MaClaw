#!/usr/bin/env python3
"""Convert FunASR CAM++ checkpoint into the compact Go-only CMPG format.

Usage:
  python convert_campplus.py --checkpoint campplus_cn_common.bin --output campplus-cn-common.cmpg

Download the Apache-2.0 checkpoint from https://huggingface.co/funasr/campplus.
The output is a float32 CPU model and is deliberately not committed to Git.
"""
import argparse, struct
import torch

MAGIC = b"CMPG\x01"
def main():
    p=argparse.ArgumentParser();p.add_argument("--checkpoint",required=True);p.add_argument("--output",required=True);a=p.parse_args()
    state=torch.load(a.checkpoint,map_location="cpu",weights_only=False)
    tensors=[(k,v.detach().cpu().float().contiguous()) for k,v in state.items() if v.is_floating_point()]
    with open(a.output,"wb") as f:
        f.write(MAGIC);f.write(struct.pack("<I",len(tensors)))
        for name,t in tensors:
            key=name.encode();shape=tuple(t.shape)
            if len(key)>255 or not shape or len(shape)>4: raise ValueError(name)
            f.write(struct.pack("<H",len(key)));f.write(key);f.write(struct.pack("<B",len(shape)))
            f.write(struct.pack("<"+"I"*len(shape),*shape));f.write(t.numpy().tobytes(order="C"))
    print(f"wrote {a.output}: {len(tensors)} tensors")
if __name__ == "__main__": main()
