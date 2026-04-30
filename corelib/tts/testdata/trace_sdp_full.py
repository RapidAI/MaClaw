#!/usr/bin/env python3
"""Trace the full SDP computation to understand what needs to be implemented."""
import onnx

m = onnx.load("corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx")

# Count nodes in each section
sections = {"enc_p": 0, "dp": 0, "flow": 0, "dec": 0, "other": 0}
dp_ops = {}
for node in m.graph.node:
    if "/enc_p/" in node.name:
        sections["enc_p"] += 1
    elif "/dp/" in node.name:
        sections["dp"] += 1
        op = node.op_type
        dp_ops[op] = dp_ops.get(op, 0) + 1
    elif "/flow/" in node.name:
        sections["flow"] += 1
    elif "/dec/" in node.name:
        sections["dec"] += 1
    else:
        sections["other"] += 1

print("Node counts:", sections)
print(f"\nDP operations: {dp_ops}")
print(f"Total DP nodes: {sections['dp']}")

# The SDP has:
# 1. dp.pre + dp.convs + dp.proj (conditioning on encoder output)
# 2. dp.flows.0 (log-scale parameter)
# 3. dp.flows.1 (Flip)
# 4. dp.flows.2 (ElementwiseAffine)  
# 5. dp.flows.3 (ConvFlow with DDSConv)
# 6. dp.flows.4 (Flip)
# 7. dp.flows.5 (ConvFlow with DDSConv)
# 8. dp.flows.6 (Flip)
# 9. dp.flows.7 (ConvFlow with DDSConv) - the most complex one

# The key question: what does dp.flows.7 actually compute?
# It's a neural spline flow (piecewise rational quadratic)
# Let's count the unique operation types
print("\n=== dp.flows.7 operations ===")
f7_ops = {}
for node in m.graph.node:
    if "/dp/flows.7/" in node.name:
        f7_ops[node.op_type] = f7_ops.get(node.op_type, 0) + 1
print(f"Operations: {f7_ops}")
print(f"Total: {sum(f7_ops.values())} nodes")

# The neural spline flow uses:
# - Conv (DDSConv layers)
# - Softmax (for bin weights)
# - Cumsum (for knot positions)
# - GatherElements (for spline lookup)
# - ScatterND (for output assembly)
# These are all standard ops that can be implemented in Go.
