#!/usr/bin/env python3
import numpy as np, os
data = np.load("corelib/tts/testdata/duration_mlp_weights.npz")
W1 = data["W1"].flatten().astype(np.float32)
b1 = data["b1"].flatten().astype(np.float32)
W2 = data["W2"].flatten().astype(np.float32)
b2 = data["b2"].flatten().astype(np.float32)
path = "corelib/tts/testdata/duration_mlp.bin"
with open(path, "wb") as f:
    f.write(W1.tobytes())
    f.write(b1.tobytes())
    f.write(W2.tobytes())
    f.write(b2.tobytes())
print(f"Saved: W1={W1.shape}, b1={b1.shape}, W2={W2.shape}, b2={b2.shape}")
print(f"Size: {os.path.getsize(path)} bytes")
