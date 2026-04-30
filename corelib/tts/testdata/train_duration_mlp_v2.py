#!/usr/bin/env python3
"""Train a better duration MLP with more data and larger model."""
import numpy as np, onnxruntime as ort, onnx, os
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph
graph.output.append(helper.make_tensor_value_info("/enc_p/Split_output_0", TensorProto.FLOAT, None))
graph.output.append(helper.make_tensor_value_info("/Squeeze_output_0", TensorProto.FLOAT, None))
modified_path = model_path + ".mlp2.onnx"
onnx.save(model, modified_path)
sess = ort.InferenceSession(modified_path)

# Lexicon
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

# Much more training data
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
    # More diverse sentences
    "请帮我查一下明天的航班", "这个价格太贵了能便宜点吗",
    "我需要预约一个会议室", "下午三点有一个重要的会议",
    "请把这份文件发给我", "我已经完成了今天的工作",
    "你觉得这个方案怎么样", "我们需要讨论一下这个问题",
    "这个功能还需要优化", "测试结果看起来不错",
    "数据库需要备份一下", "服务器运行正常",
    "用户反馈说界面不够友好", "我们来改进一下设计",
    "这个版本什么时候发布", "还有几个问题需要修复",
    "代码审查通过了", "部署到生产环境",
    "监控系统显示一切正常", "性能测试的结果很好",
    "今天的会议推迟到明天", "请大家准时参加",
    "项目进展顺利", "预计下周可以完成",
    "客户对产品很满意", "销售额比上个月增长了",
    "我们需要招聘新员工", "面试安排在下周一",
    "公司年会定在十二月", "大家有什么节目想表演",
    "食堂今天的菜不错", "下午茶时间到了",
    "周末一起去爬山吧", "天气好适合户外活动",
    "最近在看什么书", "推荐一部好电影",
    "这首歌真好听", "你会弹钢琴吗",
    "孩子们放暑假了", "带他们去旅游",
    "机票已经订好了", "酒店也预约了",
    "行李准备好了吗", "别忘了带充电器",
    "到了给我打电话", "一路平安",
    "照片拍得真好看", "风景太美了",
    "这道菜怎么做的", "味道很不错",
    "你最近身体怎么样", "要注意休息",
    "生日快乐", "新年好",
    "恭喜发财", "万事如意",
    "工作顺利", "身体健康",
    "学业有成", "前程似锦",
]

all_X = []
all_y = []
all_pid = []
all_pos = []

for text in texts:
    pids = text_to_pids(text)
    T = len(pids)
    try:
        outputs = sess.run(None, {"input": np.array([pids], dtype=np.int64),
                                  "input_lengths": np.array([T], dtype=np.int64),
                                  "scales": np.array([0.0, 1.0, 0.0], dtype=np.float32)})
    except:
        continue
    m_p = np.array(outputs[1]).squeeze()
    path = np.array(outputs[2]).squeeze()
    durations = path.sum(axis=0).astype(int)
    for t in range(T):
        all_X.append(m_p[:, t])
        all_y.append(durations[t])
        all_pid.append(pids[t])
        all_pos.append(t / max(T-1, 1))

X = np.array(all_X)
y = np.array(all_y, dtype=np.float32)
pids_arr = np.array(all_pid)
pos = np.array(all_pos)

print(f"Training data: {len(X)} samples from {len(texts)} texts")

# Larger MLP: 194 → 64 → 32 → 1
np.random.seed(42)
X_aug = np.column_stack([X, pos.reshape(-1,1), pids_arr.reshape(-1,1) / 72.0])
in_dim = X_aug.shape[1]  # 194
h1_dim = 64
h2_dim = 32

W1 = np.random.randn(in_dim, h1_dim).astype(np.float32) * np.sqrt(2.0 / in_dim)
b1 = np.zeros(h1_dim, dtype=np.float32)
W2 = np.random.randn(h1_dim, h2_dim).astype(np.float32) * np.sqrt(2.0 / h1_dim)
b2 = np.zeros(h2_dim, dtype=np.float32)
W3 = np.random.randn(h2_dim, 1).astype(np.float32) * np.sqrt(2.0 / h2_dim)
b3 = np.zeros(1, dtype=np.float32)

log_y = np.log(y + 0.5)

lr = 0.0005
for epoch in range(1000):
    # Forward
    h1 = np.maximum(X_aug @ W1 + b1, 0)
    h2 = np.maximum(h1 @ W2 + b2, 0)
    pred = (h2 @ W3 + b3).squeeze()
    loss = np.mean((pred - log_y)**2)
    
    # Backward
    d_pred = 2 * (pred - log_y) / len(pred)
    d_W3 = h2.T @ d_pred.reshape(-1, 1)
    d_b3 = d_pred.sum()
    d_h2 = (d_pred.reshape(-1, 1) @ W3.T) * (h2 > 0)
    d_W2 = h1.T @ d_h2
    d_b2 = d_h2.sum(axis=0)
    d_h1 = (d_h2 @ W2.T) * (h1 > 0)
    d_W1 = X_aug.T @ d_h1
    d_b1 = d_h1.sum(axis=0)
    
    W1 -= lr * d_W1; b1 -= lr * d_b1
    W2 -= lr * d_W2; b2 -= lr * d_b2
    W3 -= lr * d_W3; b3 -= lr * d_b3
    
    if epoch % 200 == 0:
        pred_dur = np.exp(pred) - 0.5
        mae = np.mean(np.abs(pred_dur - y))
        print(f"  Epoch {epoch}: loss={loss:.4f}, MAE={mae:.2f}")

pred = (np.maximum(np.maximum(X_aug @ W1 + b1, 0) @ W2 + b2, 0) @ W3 + b3).squeeze()
pred_dur = np.exp(pred) - 0.5
mae = np.mean(np.abs(pred_dur - y))
print(f"\nFinal MAE: {mae:.2f} frames (was 2.14 with old MLP)")

# Save as binary: W1[194*64] + b1[64] + W2[64*32] + b2[32] + W3[32] + b3[1]
path = "corelib/tts/testdata/duration_mlp_v2.bin"
with open(path, "wb") as f:
    f.write(W1.flatten().astype(np.float32).tobytes())
    f.write(b1.flatten().astype(np.float32).tobytes())
    f.write(W2.flatten().astype(np.float32).tobytes())
    f.write(b2.flatten().astype(np.float32).tobytes())
    f.write(W3.flatten().astype(np.float32).tobytes())
    f.write(b3.flatten().astype(np.float32).tobytes())
print(f"Saved: {path} ({os.path.getsize(path)} bytes)")

# Show predictions for test sentence
print("\n=== 今天天气不错 ===")
test_pids = text_to_pids("今天天气不错")
T = len(test_pids)
out = sess.run(None, {"input": np.array([test_pids], dtype=np.int64),
                      "input_lengths": np.array([T], dtype=np.int64),
                      "scales": np.array([0.0, 1.0, 0.0], dtype=np.float32)})
m_p = np.array(out[1]).squeeze()
path_mat = np.array(out[2]).squeeze()
actual_durs = path_mat.sum(axis=0).astype(int)

for t in range(T):
    feat = np.concatenate([m_p[:, t], [t/(T-1), test_pids[t]/72.0]])
    h1 = np.maximum(feat @ W1 + b1, 0)
    h2 = np.maximum(h1 @ W2 + b2, 0)
    logw = (h2 @ W3 + b3).item()
    pred_d = max(1, round(np.exp(logw) - 0.5))
    print(f"  pid={test_pids[t]:3d} actual={actual_durs[t]:3d} predicted={pred_d:3d}")

os.remove(modified_path)
