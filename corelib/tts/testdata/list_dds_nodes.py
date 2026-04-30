import onnx
m = onnx.load("corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx")
for node in m.graph.node:
    if "/dp/convs/" in node.name:
        short = node.name.replace("/dp/convs/", "")
        print(f"{node.op_type:15s} {short}")
