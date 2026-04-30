#!/usr/bin/env python3
"""Test _absolute_position_to_relative_position with small example."""
import torch, torch.nn.functional as F, numpy as np

# T=3 example
T = 3
x = torch.tensor([[
    [0.1, 0.7, 0.2],
    [0.3, 0.4, 0.3],
    [0.2, 0.3, 0.5],
]]).unsqueeze(0)  # [1, 1, 3, 3]

# _absolute_position_to_relative_position
length = T
x_pad = F.pad(x, [0, length - 1])  # [1, 1, 3, 5]
print(f"padded: {x_pad.squeeze()}")
x_flat = x_pad.view(1, 1, length**2 + length * (length - 1))  # [1, 1, 12]
print(f"flat: {x_flat.squeeze()}")
x_flat = F.pad(x_flat, [length, 0])  # [1, 1, 15]
print(f"flat_padded: {x_flat.squeeze()}")
x_final = x_flat.view(1, 1, length, 2 * length)[:, :, :, 1:]  # [1, 1, 3, 5]
print(f"result: {x_final.squeeze()}")

# Dump for Go comparison
result = x_final.squeeze().numpy().astype(np.float32)
result.tofile("corelib/tts/testdata/ref_abs_to_rel_T3.bin")
input_data = x.squeeze().numpy().astype(np.float32)
input_data.tofile("corelib/tts/testdata/ref_abs_to_rel_T3_input.bin")
print(f"\nInput:\n{x.squeeze()}")
print(f"Output:\n{x_final.squeeze()}")
