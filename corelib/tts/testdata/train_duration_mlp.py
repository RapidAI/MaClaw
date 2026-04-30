#!/usr/bin/env python3
"""Train a small MLP to predict duration from encoder output features."""
import numpy as np, onnxruntime as ort, onnx, os
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph
graph.output.append(helper.make_tensor_value_info("/enc_p/Split_output_0", TensorProto.FLOAT, None))
graph.output.append(helper.make_tensor_value_info("/Squeeze_output_0", TensorProto.FLOAT, None))
modified_path = model_path + ".mlp_train.onnx"
onnx.save(model, modified_path)
sess = ort.InferenceSession(modified_path)

# Load lexicon
lex = {}
with open("corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/lexicon.txt", "r", encoding="utf-8") as f:
    for line in f:
        parts = line.strip().split()
        if len(parts) >= 2 and len(parts[0]) == 1:
            lex[parts[0]] = [p for p in parts[1:] if p != "_"]

pid_map = {"_":0,"^":1,"$":2,"Ø":3,"b":4,"p":5,"m":6,"f":7,"d":8,"t":9,"n":10,"l":11,
    "g":12,"k":13,"h":14,"j":15,"q":16,"x":17,"zh":18,"ch":19,"sh":20,"r":21,"z":22,"c":23,"s":24,
    "y":25,"w":26,"a":27,"o":28,"e":29,"ai":30,"ei":31,"ao":32,"ou":33,"an":34,"en":35,"ang":36,
    "eng":37,"ong":38,"i":39,"ia":40,"ie":41,"iao":42,"iu":43,"ian":44,"in":45,"iang":46,"ing":47,
    "iong":48,"u":49,"ua":50,"uo":51,"uai":52,"ui":53,"uan":54,"un":55,"uang":56,"ueng":57,
    "v":58,"ve":59,"van":60,"vn":61,"er":62,"ue":63,"1":64,"2":65,"3":66,"4":67,"5":68,
    "。":69,"？":70,"！":71,"，":72}

def text_to_pids(text):
    ids = [1]
    prev = False
    for ch in text:
        if ch in lex:
            if prev: ids.append(0)
            for ph in lex[ch]:
                if ph in pid_map: ids.append(pid_map[ph])
            prev = True
        elif ch in pid_map:
            ids.append(pid_map[ch]); prev = False
        else: prev = False
    ids.append(2)
    return ids

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
    "这个问题很简单", "我不太确定", "让我想想看",
    "非常感谢你的支持", "希望一切顺利", "加油你可以的",
    "今天的任务完成了", "明天继续努力", "周末愉快",
    "天气预报说明天晴", "记得带伞出门", "路上注意安全",
    "吃饭了吗", "喝杯咖啡吧", "休息一下再继续",
    "这本书很有意思", "电影好看吗", "音乐真好听",
    "运动有益健康", "早睡早起身体好", "多喝水少熬夜",
]

all_X = []  # [hidden] per timestep
all_y = []  # duration per timestep
all_pid = []
all_pos = []  # relative position [0, 1]

for text in texts:
    pids = text_to_pids(text)
    T = len(pids)
    phoneme_ids = np.array([pids], dtype=np.int64)
    input_lengths = np.array([T], dtype=np.int64)
    scales = np.array([0.0, 1.0, 0.0], dtype=np.float32)
    
    try:
        outputs = sess.run(None, {"input": phoneme_ids, "input_lengths": input_lengths, "scales": scales})
    except:
        continue
    
    m_p = np.array(outputs[1]).squeeze()  # [192, T]
    path = np.array(outputs[2]).squeeze()  # [tMel, T]
    durations = path.sum(axis=0).astype(int)
    
    for t in range(T):
        all_X.append(m_p[:, t])
        all_y.append(durations[t])
        all_pid.append(pids[t])
        all_pos.append(t / max(T-1, 1))

X = np.array(all_X)  # [N, 192]
y = np.array(all_y, dtype=np.float32)
pids = np.array(all_pid)
pos = np.array(all_pos)

print(f"Training data: {len(X)} samples from {len(texts)} texts")
print(f"Duration range: {y.min():.0f}-{y.max():.0f}, mean={y.mean():.1f}")

# Train a 2-layer MLP: 192 → 64 → 1
# Using simple numpy (no torch dependency)
np.random.seed(42)
hidden_dim = 32

# Add position and pid as features
X_aug = np.column_stack([X, pos.reshape(-1,1), pids.reshape(-1,1) / 72.0])  # [N, 194]
in_dim = X_aug.shape[1]

# Xavier init
W1 = np.random.randn(in_dim, hidden_dim).astype(np.float32) * np.sqrt(2.0 / in_dim)
b1 = np.zeros(hidden_dim, dtype=np.float32)
W2 = np.random.randn(hidden_dim, 1).astype(np.float32) * np.sqrt(2.0 / hidden_dim)
b2 = np.zeros(1, dtype=np.float32)

log_y = np.log(y + 0.5)  # target: log-duration

# Train with SGD
lr = 0.001
for epoch in range(500):
    # Forward
    h = X_aug @ W1 + b1
    h_relu = np.maximum(h, 0)  # ReLU
    pred = (h_relu @ W2 + b2).squeeze()
    
    loss = np.mean((pred - log_y)**2)
    
    # Backward
    d_pred = 2 * (pred - log_y) / len(pred)
    d_W2 = h_relu.T @ d_pred.reshape(-1, 1)
    d_b2 = d_pred.sum()
    d_h_relu = d_pred.reshape(-1, 1) @ W2.T
    d_h = d_h_relu * (h > 0)
    d_W1 = X_aug.T @ d_h
    d_b1 = d_h.sum(axis=0)
    
    W1 -= lr * d_W1
    b1 -= lr * d_b1
    W2 -= lr * d_W2
    b2 -= lr * d_b2
    
    if epoch % 100 == 0:
        pred_dur = np.exp(pred) - 0.5
        mae = np.mean(np.abs(pred_dur - y))
        print(f"  Epoch {epoch}: loss={loss:.4f}, MAE={mae:.2f} frames")

# Final evaluation
pred = (np.maximum(X_aug @ W1 + b1, 0) @ W2 + b2).squeeze()
pred_dur = np.exp(pred) - 0.5
mae = np.mean(np.abs(pred_dur - y))
print(f"\nFinal MAE: {mae:.2f} frames")

# Save weights for Go
np.savez("corelib/tts/testdata/duration_mlp_weights.npz", W1=W1, b1=b1, W2=W2, b2=b2)
print(f"Saved MLP weights: W1={W1.shape}, W2={W2.shape}")
print(f"Total params: {W1.size + b1.size + W2.size + b2.size}")

# Show predictions for first text
print("\n=== 今天天气不错 predictions ===")
text_pids = text_to_pids("今天天气不错")
idx = 0
for i, text in enumerate(texts):
    if text == "今天天气不错":
        T = len(text_to_pids(text))
        for t in range(T):
            actual = all_y[idx + t]
            predicted = pred_dur[idx + t]
            print(f"  pid={all_pid[idx+t]:3d} actual={actual:3.0f} predicted={predicted:.1f}")
        break
    idx += len(text_to_pids(text))

os.remove(modified_path)
