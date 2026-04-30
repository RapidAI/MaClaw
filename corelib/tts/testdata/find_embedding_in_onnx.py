#!/usr/bin/env python3
"""Find the phoneme embedding table in the xiao_ya ONNX model."""
import os
import numpy as np
import onnx
from onnx import numpy_helper

model_path = os.path.join(os.path.dirname(__file__),
    "vits-piper-zh_CN-xiao_ya-medium", "zh_CN-xiao_ya-medium.onnx")

model = onnx.load(model_path)
graph = model.graph

print("Looking for embedding table (shape ~[256, 192])...")
print()

for init in graph.initializer:
    dims = list(init.dims)
    # Look for [256, 192] or [N, 192] tensors that could be the embedding
    if len(dims) == 2 and dims[1] == 192 and dims[0] >= 64:
        arr = numpy_helper.to_array(init)
        print(f"  {init.name}: {dims} — stats: mean={arr.mean():.4f}, std={arr.std():.4f}, min={arr.min():.4f}, max={arr.max():.4f}")

# Also look at the graph nodes for Gather operations (embedding lookup)
print("\nGather operations (embedding lookups):")
for node in graph.node:
    if node.op_type == "Gather":
        print(f"  {node.name}: inputs={list(node.input)}, outputs={list(node.output)}")
        # Check the first input (the embedding table)
        for init in graph.initializer:
            if init.name == node.input[0]:
                dims = list(init.dims)
                print(f"    → table: {init.name} shape={dims}")

# Check all initializers with shape containing 192
print("\nAll initializers with dim 192 and size > 1000:")
for init in graph.initializer:
    dims = list(init.dims)
    total = 1
    for d in dims:
        total *= d
    if total > 1000 and 192 in dims:
        arr = numpy_helper.to_array(init)
        print(f"  {init.name}: {dims} ({total} params) mean={arr.mean():.6f} std={arr.std():.6f}")
