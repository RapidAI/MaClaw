#!/usr/bin/env python3
"""Check flow decoder WaveNet dilation values from ONNX graph."""
import onnx
m = onnx.load("corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx")
for node in m.graph.node:
    if node.op_type == "Conv":
        # Check if any input references flow weights
        is_flow = False
        for inp in node.input:
            if "flow" in inp.lower() or "onnx::Conv_89" in inp or "onnx::Conv_90" in inp or "onnx::Conv_91" in inp or "onnx::Conv_92" in inp or "onnx::Conv_93" in inp:
                is_flow = True
                break
        if not is_flow:
            continue
        dilations = []
        pads = []
        for a in node.attribute:
            if a.name == "dilations":
                dilations = list(a.ints)
            if a.name == "pads":
                pads = list(a.ints)
            if a.name == "group":
                pass
        if dilations:
            weight_name = node.input[1] if len(node.input) > 1 else "?"
            print(f"  {node.name}: dilation={dilations}, pads={pads}, weight={weight_name}")
