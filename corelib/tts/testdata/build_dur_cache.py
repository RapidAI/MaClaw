#!/usr/bin/env python3
"""Build a duration cache: for each unique phoneme trigram, store the average duration.
This captures context-dependent duration patterns much better than a per-phoneme table."""
import numpy as np, onnxruntime as ort, onnx, json, os
from collections import defaultdict
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
model.graph.output.append(helper.make_tensor_value_info("/Squeeze_output_0", TensorProto.FLOAT, None))
modified = model_path + ".cache.onnx"
onnx.save(model, modified)
sess = ort.InferenceSession(modified)

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
]

# Build trigram → duration mapping
# Key: (prev_pid, current_pid, next_pid) → list of durations
trigram_durs = defaultdict(list)

for text in texts:
    pids = text_to_pids(text)
    T = len(pids)
    try:
        out = sess.run(None, {"input": np.array([pids], dtype=np.int64),
                              "input_lengths": np.array([T], dtype=np.int64),
                              "scales": np.array([0.0, 1.0, 0.0], dtype=np.float32)})
    except:
        continue
    path = np.array(out[1]).squeeze()
    durs = path.sum(axis=0).astype(int)
    
    for t in range(T):
        prev_pid = pids[t-1] if t > 0 else -1
        curr_pid = pids[t]
        next_pid = pids[t+1] if t < T-1 else -1
        trigram_durs[(prev_pid, curr_pid, next_pid)].append(int(durs[t]))

# Average each trigram
trigram_avg = {}
for key, vals in trigram_durs.items():
    trigram_avg[key] = round(np.mean(vals))

print(f"Trigram entries: {len(trigram_avg)}")

# Save as JSON (convert tuple keys to strings)
cache = {}
for (p, c, n), d in trigram_avg.items():
    cache[f"{p},{c},{n}"] = d

with open("corelib/tts/testdata/duration_trigram_cache.json", "w") as f:
    json.dump(cache, f)
print(f"Saved duration_trigram_cache.json ({len(cache)} entries)")

# Also save bigram (current, next) as fallback
bigram_durs = defaultdict(list)
for (p, c, n), vals in trigram_durs.items():
    bigram_durs[(c, n)].extend(vals)
bigram_avg = {f"{c},{n}": round(np.mean(v)) for (c, n), v in bigram_durs.items()}
with open("corelib/tts/testdata/duration_bigram_cache.json", "w") as f:
    json.dump(bigram_avg, f)
print(f"Saved duration_bigram_cache.json ({len(bigram_avg)} entries)")

# Unigram fallback
unigram_durs = defaultdict(list)
for (p, c, n), vals in trigram_durs.items():
    unigram_durs[c].extend(vals)
unigram_avg = {str(c): round(np.mean(v)) for c, v in unigram_durs.items()}
with open("corelib/tts/testdata/duration_unigram_cache.json", "w") as f:
    json.dump(unigram_avg, f)
print(f"Saved duration_unigram_cache.json ({len(unigram_avg)} entries)")

os.remove(modified)
