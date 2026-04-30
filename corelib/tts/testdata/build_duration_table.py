#!/usr/bin/env python3
"""Build a context-aware duration table from ONNX reference data."""
import numpy as np, onnxruntime as ort, onnx, json, os
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph
graph.output.append(helper.make_tensor_value_info("/Squeeze_output_0", TensorProto.FLOAT, None))
modified_path = model_path + ".dur_table.onnx"
onnx.save(model, modified_path)
sess = ort.InferenceSession(modified_path)

# Load lexicon for G2P
lex = {}
with open("corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/lexicon.txt", "r", encoding="utf-8") as f:
    for line in f:
        parts = line.strip().split()
        if len(parts) >= 2 and len(parts[0]) == 1:
            phones = [p for p in parts[1:] if p != "_"]
            lex[parts[0]] = phones

pid_map = {
    "_": 0, "^": 1, "$": 2, "Ø": 3,
    "b": 4, "p": 5, "m": 6, "f": 7, "d": 8, "t": 9, "n": 10, "l": 11,
    "g": 12, "k": 13, "h": 14, "j": 15, "q": 16, "x": 17,
    "zh": 18, "ch": 19, "sh": 20, "r": 21, "z": 22, "c": 23, "s": 24,
    "y": 25, "w": 26,
    "a": 27, "o": 28, "e": 29, "ai": 30, "ei": 31, "ao": 32, "ou": 33,
    "an": 34, "en": 35, "ang": 36, "eng": 37, "ong": 38,
    "i": 39, "ia": 40, "ie": 41, "iao": 42, "iu": 43, "ian": 44, "in": 45,
    "iang": 46, "ing": 47, "iong": 48,
    "u": 49, "ua": 50, "uo": 51, "uai": 52, "ui": 53, "uan": 54, "un": 55,
    "uang": 56, "ueng": 57,
    "v": 58, "ve": 59, "van": 60, "vn": 61, "er": 62, "ue": 63,
    "1": 64, "2": 65, "3": 66, "4": 67, "5": 68,
    "。": 69, "？": 70, "！": 71, "，": 72,
}

def text_to_pids(text):
    ids = [1]  # ^
    prev_chinese = False
    for ch in text:
        if ch in lex:
            if prev_chinese:
                ids.append(0)  # _
            for ph in lex[ch]:
                if ph in pid_map:
                    ids.append(pid_map[ph])
            prev_chinese = True
        elif ch in pid_map:
            ids.append(pid_map[ch])
            prev_chinese = False
        else:
            prev_chinese = False
    ids.append(2)  # $
    return ids

# Generate many texts
texts = [
    "你好世界", "今天天气不错", "我们一起来写代码吧", "欢迎使用智能助手",
    "人工智能正在改变世界", "这是一个测试", "请问你叫什么名字",
    "明天会下雨吗", "我想吃火锅", "北京是中国的首都",
    "春天来了花开了", "学习使人进步", "时间就是金钱",
    "科技改变生活", "健康最重要", "努力工作认真学习",
    "太阳从东方升起", "月亮很圆很亮", "大家好我是小助手",
    "谢谢你的帮助", "对不起我来晚了", "没关系不用担心",
    "请坐请喝茶", "再见明天见", "祝你生日快乐",
    "新年快乐万事如意", "中秋节快乐", "国庆节放假七天",
    "早上好今天真开心", "晚安做个好梦",
]

# Collect (pid, position_type, duration) tuples
# position_type: 0=start, 1=initial, 2=final, 3=tone, 4=boundary, 5=end
from collections import defaultdict
dur_by_pid = defaultdict(list)
dur_by_pid_pos = defaultdict(list)  # (pid, is_first, is_last) → durations

for text in texts:
    pids = text_to_pids(text)
    T = len(pids)
    phoneme_ids = np.array([pids], dtype=np.int64)
    input_lengths = np.array([T], dtype=np.int64)
    scales = np.array([0.0, 1.0, 0.0], dtype=np.float32)
    
    outputs = sess.run(None, {"input": phoneme_ids, "input_lengths": input_lengths, "scales": scales})
    path = np.array(outputs[1]).squeeze()
    durations = path.sum(axis=0).astype(int)
    
    for t in range(T):
        pid = pids[t]
        dur = int(durations[t])
        is_first = (t <= 2)  # first few phonemes
        is_last = (t >= T - 3)  # last few phonemes
        dur_by_pid[pid].append(dur)
        dur_by_pid_pos[(pid, is_first, is_last)].append(dur)

# Build the table: pid → average duration
print("=== Per-PID average duration ===")
pid_avg = {}
for pid in sorted(dur_by_pid.keys()):
    durs = dur_by_pid[pid]
    avg = np.mean(durs)
    pid_avg[pid] = round(avg, 1)
    print(f"  pid={pid:3d}: avg={avg:.1f} std={np.std(durs):.1f} n={len(durs)} range=[{min(durs)},{max(durs)}]")

# Build position-aware table
print("\n=== Position-aware duration ===")
for (pid, is_first, is_last), durs in sorted(dur_by_pid_pos.items()):
    if len(durs) >= 2:
        avg = np.mean(durs)
        pos = "first" if is_first else ("last" if is_last else "mid")
        print(f"  pid={pid:3d} pos={pos:5s}: avg={avg:.1f} n={len(durs)}")

# Generate Go code for the duration table
print("\n=== Go duration table ===")
print("var piperDurationTable = map[int]int{")
for pid in sorted(pid_avg.keys()):
    print(f"\t{pid}: {max(1, round(pid_avg[pid]))},")
print("}")

os.remove(modified_path)
