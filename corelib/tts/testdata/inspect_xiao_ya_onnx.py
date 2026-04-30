#!/usr/bin/env python3
"""Inspect xiao_ya ONNX model structure to understand VITS architecture."""
import os
import sys

try:
    import onnx
except ImportError:
    os.system(f"{sys.executable} -m pip install onnx -q")
    import onnx

model_path = os.path.join(os.path.dirname(__file__), 
    "vits-piper-zh_CN-xiao_ya-medium", "zh_CN-xiao_ya-medium.onnx")

print(f"Loading: {model_path}")
print(f"Size: {os.path.getsize(model_path) / 1024 / 1024:.1f} MB")

model = onnx.load(model_path)
graph = model.graph

print(f"\n=== Inputs ===")
for inp in graph.input:
    shape = [d.dim_value if d.dim_value else d.dim_param for d in inp.type.tensor_type.shape.dim]
    dtype = inp.type.tensor_type.elem_type
    print(f"  {inp.name}: shape={shape}, dtype={dtype}")

print(f"\n=== Outputs ===")
for out in graph.output:
    shape = [d.dim_value if d.dim_value else d.dim_param for d in out.type.tensor_type.shape.dim]
    dtype = out.type.tensor_type.elem_type
    print(f"  {out.name}: shape={shape}, dtype={dtype}")

# Collect all initializer (weight) names and shapes
print(f"\n=== Weight Summary ===")
weight_groups = {}
total_params = 0
for init in graph.initializer:
    dims = list(init.dims)
    params = 1
    for d in dims:
        params *= d
    total_params += params
    
    # Group by prefix
    parts = init.name.split(".")
    if len(parts) >= 2:
        prefix = ".".join(parts[:2])
    else:
        prefix = parts[0]
    
    if prefix not in weight_groups:
        weight_groups[prefix] = []
    weight_groups[prefix].append((init.name, dims, params))

print(f"Total parameters: {total_params:,} ({total_params*4/1024/1024:.1f} MB fp32)")
print(f"\nWeight groups:")
for prefix in sorted(weight_groups.keys()):
    items = weight_groups[prefix]
    group_params = sum(p for _, _, p in items)
    print(f"\n  {prefix} ({len(items)} tensors, {group_params:,} params):")
    for name, dims, params in items[:5]:
        print(f"    {name}: {dims}")
    if len(items) > 5:
        print(f"    ... and {len(items)-5} more")

# Print all weight names for architecture understanding
print(f"\n=== All Weight Names ===")
for init in graph.initializer:
    dims = list(init.dims)
    print(f"  {init.name}: {dims}")
