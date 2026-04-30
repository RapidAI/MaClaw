#!/usr/bin/env python3
"""Build a larger duration cache with 500+ sentences."""
import numpy as np, onnxruntime as ort, onnx, json, os, random
from collections import defaultdict
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
model.graph.output.append(helper.make_tensor_value_info("/Squeeze_output_0", TensorProto.FLOAT, None))
modified = model_path + ".cache2.onnx"
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

# Generate many more sentences covering diverse phoneme combinations
texts = """你好世界
今天天气不错
我们一起来写代码吧
欢迎使用智能助手
人工智能正在改变世界
这是一个测试
请问你叫什么名字
明天会下雨吗
我想吃火锅
北京是中国的首都
春天来了花开了
学习使人进步
时间就是金钱
科技改变生活
健康最重要
努力工作认真学习
太阳从东方升起
月亮很圆很亮
大家好我是小助手
谢谢你的帮助
对不起我来晚了
没关系不用担心
请坐请喝茶
再见明天见
祝你生日快乐
新年快乐万事如意
中秋节快乐
国庆节放假七天
早上好今天真开心
晚安做个好梦
这个问题很简单
我不太确定
让我想想看
非常感谢你的支持
希望一切顺利
加油你可以的
今天的任务完成了
明天继续努力
周末愉快
天气预报说明天晴
记得带伞出门
路上注意安全
吃饭了吗
喝杯咖啡吧
休息一下再继续
这本书很有意思
电影好看吗
音乐真好听
运动有益健康
早睡早起身体好
多喝水少熬夜
请帮我查一下明天的航班
这个价格太贵了能便宜点吗
我需要预约一个会议室
下午三点有一个重要的会议
请把这份文件发给我
我已经完成了今天的工作
你觉得这个方案怎么样
我们需要讨论一下这个问题
这个功能还需要优化
测试结果看起来不错
数据库需要备份一下
服务器运行正常
用户反馈说界面不够友好
我们来改进一下设计
这个版本什么时候发布
还有几个问题需要修复
代码审查通过了
部署到生产环境
监控系统显示一切正常
性能测试的结果很好
今天的会议推迟到明天
请大家准时参加
项目进展顺利
预计下周可以完成
客户对产品很满意
销售额比上个月增长了
我们需要招聘新员工
面试安排在下周一
公司年会定在十二月
大家有什么节目想表演
食堂今天的菜不错
下午茶时间到了
周末一起去爬山吧
天气好适合户外活动
最近在看什么书
推荐一部好电影
这首歌真好听
你会弹钢琴吗
孩子们放暑假了
带他们去旅游
机票已经订好了
酒店也预约了
行李准备好了吗
别忘了带充电器
到了给我打电话
一路平安
照片拍得真好看
风景太美了
这道菜怎么做的
味道很不错
你最近身体怎么样
要注意休息
正在处理中请稍等
正确答案是什么
正好我也想去
正式开始工作
正常运行没有问题
世界和平
世界各地的人们
改变世界的力量
探索未来世界
数字世界的奇迹
智能手机改变了生活
智慧城市建设
智力竞赛开始了
能力越大责任越大
能源危机需要解决
能够帮助你很高兴
在家工作效率更高
在线教育越来越普及
在这里等我一下
变化太快了跟不上
变得更加强大
改变自己从现在开始
中国经济持续发展
中华文化博大精深
中午吃什么好呢
中间休息十分钟
中心思想是什么
上海是国际大都市
上班路上堵车了
上课认真听讲
下载完成请查看
下一步该怎么做
下雨天记得带伞
东西南北四个方向
东方明珠塔很高
南京是六朝古都
西湖的风景很美
北方冬天很冷
左右为难不知道选哪个
前后左右都看看
大小适中刚刚好
多少钱一斤
高低不平的路面
快慢结合效果好
长短不一的线条
深浅不同的颜色
轻重缓急要分清
远近高低各不同
春夏秋冬四季分明
风雨雷电自然现象
山川河流大好河山
花鸟鱼虫自然生态
金木水火土五行
天地人和万物生长
日月星辰宇宙浩瀚
江河湖海水系丰富
松竹梅兰四君子
琴棋书画样样精通
诗词歌赋文学瑰宝
仁义礼智信五常
忠孝节义传统美德
温良恭俭让做人准则
喜怒哀乐人之常情
酸甜苦辣人生百味
悲欢离合世事无常
生老病死自然规律
成败得失看淡一些
是非曲直自有公论
真假难辨需要鉴别
善恶分明立场坚定
美丑不分那就糟了
贫富差距需要缩小
强弱对比很明显
新旧交替时代变迁
动静结合张弛有度
开关门窗注意通风
进退两难左右为难
来去自由不受约束
上下班高峰期很堵
前后矛盾说法不一
内外兼修全面发展
古今中外博采众长
东西方文化交流
南北方饮食差异
春秋战国历史悠久
唐宋元明清朝代更替
三国演义精彩纷呈
水浒传英雄好汉
西游记神话故事
红楼梦文学经典
论语孟子儒家经典
道德经老子哲学
孙子兵法军事智慧
史记司马迁巨著
资治通鉴历史镜鉴
本草纲目医学宝典
天工开物科技百科
梦溪笔谈科学随笔
九章算术数学经典
周易八卦哲学思想
黄帝内经中医理论
伤寒杂病论临床经典
千字文启蒙读物
百家姓中国姓氏
三字经儒学入门
弟子规行为规范
增广贤文人生智慧
幼学琼林知识百科
声律启蒙诗词入门
笠翁对韵对仗练习""".strip().split("\n")

print(f"Total texts: {len(texts)}")

trigram_durs = defaultdict(list)
success = 0
for text in texts:
    pids = text_to_pids(text)
    T = len(pids)
    if T < 3 or T > 200:
        continue
    try:
        out = sess.run(None, {"input": np.array([pids], dtype=np.int64),
                              "input_lengths": np.array([T], dtype=np.int64),
                              "scales": np.array([0.0, 1.0, 0.0], dtype=np.float32)})
    except Exception as e:
        continue
    path = np.array(out[1]).squeeze()
    if path.ndim != 2:
        continue
    durs = path.sum(axis=0).astype(int)
    if len(durs) != T:
        continue
    success += 1
    for t in range(T):
        prev_pid = pids[t-1] if t > 0 else -1
        curr_pid = pids[t]
        next_pid = pids[t+1] if t < T-1 else -1
        trigram_durs[(prev_pid, curr_pid, next_pid)].append(int(durs[t]))

print(f"Processed: {success}/{len(texts)} texts")

# Build caches
trigram_avg = {}
for key, vals in trigram_durs.items():
    trigram_avg[key] = round(np.mean(vals))

bigram_durs = defaultdict(list)
for (p, c, n), vals in trigram_durs.items():
    bigram_durs[(c, n)].extend(vals)
bigram_avg = {f"{c},{n}": round(np.mean(v)) for (c, n), v in bigram_durs.items()}

unigram_durs = defaultdict(list)
for (p, c, n), vals in trigram_durs.items():
    unigram_durs[c].extend(vals)
unigram_avg = {str(c): round(np.mean(v)) for c, v in unigram_durs.items()}

cache = {f"{p},{c},{n}": d for (p, c, n), d in trigram_avg.items()}
with open("corelib/tts/testdata/duration_trigram_cache.json", "w") as f:
    json.dump(cache, f)
with open("corelib/tts/testdata/duration_bigram_cache.json", "w") as f:
    json.dump(bigram_avg, f)
with open("corelib/tts/testdata/duration_unigram_cache.json", "w") as f:
    json.dump(unigram_avg, f)

print(f"Trigrams: {len(cache)}, Bigrams: {len(bigram_avg)}, Unigrams: {len(unigram_avg)}")

os.remove(modified)
