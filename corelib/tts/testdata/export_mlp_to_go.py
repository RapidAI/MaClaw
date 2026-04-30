#!/usr/bin/env python3
"""Export MLP weights as Go source code."""
import numpy as np

data = np.load("corelib/tts/testdata/duration_mlp_weights.npz")
W1 = data["W1"]  # [194, 32]
b1 = data["b1"]  # [32]
W2 = data["W2"]  # [32, 1]
b2 = data["b2"]  # [1]

print(f"// W1: [{W1.shape[0]}, {W1.shape[1]}]")
print(f"// b1: [{b1.shape[0]}]")
print(f"// W2: [{W2.shape[0]}, {W2.shape[1]}]")
print(f"// b2: [{b2.shape[0]}]")

def fmt_array(arr, name):
    flat = arr.flatten()
    print(f"var {name} = [...]float32{{")
    for i in range(0, len(flat), 8):
        chunk = flat[i:i+8]
        line = ", ".join(f"{v:.8f}" for v in chunk)
        print(f"\t{line},")
    print("}")

fmt_array(W1, "durMLPW1")
fmt_array(b1, "durMLPB1")
fmt_array(W2, "durMLPW2")
print(f"var durMLPB2 float32 = {b2[0]:.8f}")
