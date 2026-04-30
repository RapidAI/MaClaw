#!/usr/bin/env python3
import onnx
m = onnx.load("corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx")
for node in m.graph.node:
    if "/dec/resblocks" in node.name and node.op_type == "Conv":
        dil = pad = ks = "?"
        for a in node.attribute:
            if a.name == "dilations": dil = list(a.ints)
            if a.name == "pads": pad = list(a.ints)
            if a.name == "kernel_shape": ks = list(a.ints)
        short = node.name.replace("/dec/", "")
        print(f"  {short:45s} dil={dil} pad={pad} ks={ks}")
