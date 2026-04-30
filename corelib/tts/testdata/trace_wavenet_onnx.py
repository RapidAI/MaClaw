#!/usr/bin/env python3
"""Trace the exact WaveNet computation in ONNX graph for flows.0 (last processed layer)."""
import onnx
from collections import defaultdict

m = onnx.load("corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx")

# Build output→node map
out_to_node = {}
for node in m.graph.node:
    for o in node.output:
        out_to_node[o] = node

# Trace flows.0 computation (the last flow layer processed in reverse)
# Start from flows.0/post/Conv and trace backwards
def trace(name, depth=0, max_depth=6):
    if depth > max_depth:
        return
    node = out_to_node.get(name)
    if node is None:
        return
    prefix = "  " * depth
    # Only show flow.flows.0 related nodes
    if "/flow/flows.0/" in node.name or depth == 0:
        attrs = ""
        for a in node.attribute:
            if a.name in ("dilations", "pads", "group", "strides", "kernel_shape"):
                vals = list(a.ints) if a.ints else [a.i]
                attrs += f" {a.name}={vals}"
        print(f"{prefix}{node.op_type} {node.name}{attrs}")
        print(f"{prefix}  in: {list(node.input)[:4]}")
        for inp in node.input:
            trace(inp, depth+1, max_depth)

# Print the full computation graph for flows.0 WaveNet
print("=== flows.0 WaveNet computation graph ===\n")

# Find all nodes in flows.0
flow0_nodes = []
for node in m.graph.node:
    if "/flow/flows.0/" in node.name:
        flow0_nodes.append(node)

# Print in order
for node in flow0_nodes:
    attrs = ""
    for a in node.attribute:
        if a.name in ("dilations", "pads", "group", "strides", "kernel_shape"):
            vals = list(a.ints) if a.ints else [a.i]
            attrs += f" {a.name}={vals}"
    print(f"{node.op_type:15s} {node.name}")
    print(f"  in: {list(node.input)[:4]}")
    print(f"  out: {list(node.output)}")
