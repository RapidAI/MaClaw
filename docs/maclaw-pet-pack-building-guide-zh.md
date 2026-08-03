# MaClaw 宠物包构建指南 3.0

宠物包是纯声明式资源包：图片、`pet-pack.yaml`、可选的 `pet-rig` JSON 与 `character/performer.json`。  
**禁止** JavaScript、WASM、可执行程序、远程地址或动态脚本。

| 格式 | renderer | schema | 用途 |
| --- | --- | --- | --- |
| 静态 | `native-raster` | 1–2 | 仅状态帧 |
| 骨骼动作 | `native-skeleton` | 2 | 连续 2D 骨骼 |
| **角色表演** | **`native-character`** | **3** | 状态机 + 表情 + 事件表演（需 `pet-performance-v3`） |

降级链固定：`native-character` → `native-skeleton` → `native/idle.png`。

Hub 在线规范（与本文应对齐）：`/pet-pack-help`（如 `https://hub.mypapers.top/pet-pack-help?lang=zh`）。

---

## 1. 选择路径

| 目标 | 选择 | 必需资源 |
| --- | --- | --- |
| 快速发一个静态形象 | `native-raster` | `native/idle.png` + `preview.png` |
| 基础连续动作 | `native-skeleton` + schema 2 | rig JSON + 贴图 + idle 回退 |
| **桌面可动角色（推荐）** | **`native-character` + schema 3** | performer + multi-part rig + 全身静态回退 |

**强烈建议**：可动角色使用 **分件（body + 表情头）**，并画/生成 **全身**，不要半身头像。

---

## 2. 3.0 目录模板（分件）

```text
my-character/
  pet-pack.yaml
  preview.png
  README.md                    # 可选
  native/
    idle.png                   # 合成后的全身静态（必填）
    listening.png
    thinking.png
    speaking.png
    done.png
    alert.png
    quiet.png
  rig/
    my-character.rig.json
    body.png                   # 无脸
    head_idle.png              # 完整头，耳朵贴头
    head_listen.png
    head_think.png
    head_speak.png
    head_done.png
    head_alert.png
    head_quiet.png
    tail.png                   # 可选 secondary
  character/
    performer.json
```

工作副本（**勿覆盖唯一原图**；大体积可不进 ZIP）：

```text
  _src/                        # 参考图
  native_src/                  # 未破坏的全身源
```

将**目录内内容**打成 ZIP；ZIP 内必须且只能有一个 `pet-pack.yaml`。

---

## 3. Manifest 要点与类型陷阱

```yaml
schema_version: 3              # 必须是整数 3；不能写成 "5.0.1"
id: my-character               # ^[a-z][a-z0-9-]{1,63}$
name: My Character
version: 1.0.0                 # 包版本可用 x.y.z 字符串
author: Your Name
renderer: native-character
preview: preview.png
default_size: 88
capabilities:
  pet_performance_v3: true
assets:
  preview: preview.png
  native:
    idle: native/idle.png
    listening: native/listening.png
    thinking: native/thinking.png
    speaking: native/speaking.png
    done: native/done.png
    alert: native/alert.png
    quiet: native/quiet.png
  rig:
    definition: rig/my-character.rig.json
    textures:
      - rig/body.png
      - rig/head_idle.png
      - rig/head_listen.png
      - rig/head_think.png
      - rig/head_speak.png
      - rig/head_done.png
      - rig/head_alert.png
      - rig/head_quiet.png
  character:
    definition: character/performer.json
fallback:
  renderer: native-skeleton
  idle: native/idle.png
```

### 安装失败常见原因

| 错误现象 | 原因 | 处理 |
| --- | --- | --- |
| `cannot unmarshal !!str ... into int` | 把 `schema_version` 写成了 `5.0.1` 等字符串 | 改为整数 `schema_version: 3` |
| 误伤 schema | 用正则全局替换 `version:` | 只改包 `version` 字段，勿匹配 `schema_version` |
| 未声明贴图 | slot 用了未列入 `textures` 的路径 | 每一张 head_* 都要列入白名单 |
| 换状态脸/动作不变 | `variants.default.native` 只有 idle | 七态路径写全 |
| 身首异处 / 脖子插头 / 横黑线 | 分件接缝错误 | 见第 4 节，重做 body/head |
| 外圈大脸 + 里面小脸 | 用待机轮廓套小表情 | 表情头独立全尺寸生成 |

---

## 4. 分件形象规范（实战）

### 4.1 必须

1. **全身**：站立或坐姿完整，四周透明边距；禁止半身证件照当唯一资源。  
2. **body.png**：躯干/四肢/服装；**齐衣领裁切**；顶部仅极短肩窝/肤色填领口；**不得残留五官、光头残壳、长颈管**。  
3. **head_*.png**：完整脸 + **短**下巴；**耳朵贴在头上**；**只剪中间颈根肤色**，两侧头发向下保留盖住肩领。  
4. **接口藏在衣服里**：先 body 后 head；下巴压进领口约 8–16px；领口镂空填实。禁止脖子像「插头」露在衣服外。  
5. **共用颈枢轴**：头与身贴图都以颈窝为中心（center-on-bone）；七态表情头统一脸高与下巴 Y，同一 `head` 骨偏移。  
6. **表情头独立全尺寸**：每张 `head_*` 单独生成再统一缩放；**禁止**「待机大轮廓 + 往里贴小脸」（会外圈脸轮廓 + 里面缩脸）。  
7. **表情**：多头挂同一 `head` 骨下；expression 层 alpha **严格 0/1**；说话态必须张嘴可辨，禁止七态复制同一 idle。  
8. **专绘优先**：单独生成 body / head；自动横切全身图仅兜底。

### 4.2 禁止的失败模式

- 横切过低 → 无下巴 → 脖子一道白缝 / 上下身断开  
- 身体仍带脸 → 双脸  
- **头整幅水平横切** → 发丝被切成肩领横黑线  
- 头底带衣服条 → 头一动红/蓝条盖住脸  
- 耳朵/配件飞离头顶（动物包常见）  
- body 源残留巨大空头壳未裁掉  
- **全局近黑透明** → 黑发/黑西装/墨镜消失  
- **全局近白透明** → 金发、白裙、白领误删  
- 洪水抠图掏空黑描边 → 头发与脸断开，`largest_component` 只剩脸  
- 仅 idle 接缝正确，其它状态头偏小/偏歪/外露颈根  
- `variants.default.native` 只有 idle → 设置页换状态仍显示待机  

### 4.3 抠图要点

- 黑底：从**四角洪水填充**纯黑；黑描边旁须保护，避免吃进黑发。  
- 勿用「近白=透明」处理金发角色；勿用「近黑=透明」处理黑发角色。  
- 源图先复制到 `native_src/` / `_src/`，永不覆盖唯一原图。  

### 4.4 骨骼建议

```text
root   (x≈128, y≈0.56*256)
  body (0,0)          # 贴图中心 = 衣领/颈窝
    head (0, y≈微调)  # 下巴压进领口 8–16px；七态同一偏移
      h_idle, h_listen, ...  # alpha 切换
    tail (可选)
```

Clip：身体先动，头延迟 80–280ms；表情只改 head 的 alpha。设置页预览用 `native/<state>.png`，七态静态帧也必须各自接缝正确。

### 4.5 合成自检

发布前必须看**七态**合成（`preview.png` + 全部 `native/*.png`）：

- [ ] 连续全身，无横缝、无漂浮头、无脖子插头外露  
- [ ] 七态头脸大小一致，无「外圈大轮廓 + 内圈小脸」  
- [ ] 说话态张嘴明显；思考态头居中  
- [ ] 头移开后身体无第二张脸  
- [ ] 耳朵贴头；黑发/金发完整  
- [ ] default 变体 native 表列齐七态  
- [ ] 56 / 88 / 120 px 仍可辨认  

---

## 5. performer.json（摘要）

`version: 1`，包含：`moods`、`layers`、`behaviors`、`states`、`events`、`reactions`、`rules`。

- 状态：`idle` / `listening` / `thinking` / `speaking` / `done` / `alert` / `quiet`  
- 事件：`click` / `hover` / `drag_start` / `drag_end` / `task_started` / `task_done` / `task_failed` / `long_idle`  
- 至少 10 idle 行为、12 反应、6 表情 clip、4 注视 clip  
- `no_repeat_last ≥ 3`，idle 池条目数 > `no_repeat_last`  
- 完整字段与枚举见 Hub `/pet-pack-help`

---

## 6. pet-rig 预算

| 项 | 上限 |
| --- | --- |
| 骨骼 | 24 |
| slot / 贴图 | 32 |
| 单轨关键帧 | 240 |
| 全 rig 关键帧 | 1,200 |
| 单图 | 512 KiB，1024×1024 |
| 贴图像素合计 | 4,194,304 |
| ZIP 文件 | 3 MiB |
| 解压资源 | 2 MiB |
| 文件数 | 64 |

可动画：`x` `y` `rotate` `scale_x` `scale_y`，slot 可 `alpha`。  
缓动：`linear` / `ease-out` / `ease-in-out`。`at_ms` 严格递增。

---

## 2.0 最小骨骼示例（兼容路径）

仅需骨骼、无 performer 时：

```yaml
schema_version: 2
renderer: native-skeleton
# ...
```

```json
{
  "version": 1,
  "bones": [{"name":"root","x":128,"y":128}],
  "slots": [{"name":"shell","bone":"root","texture":"rig/shell.png","z":1}],
  "clips": {
    "idle": {"duration_ms":2800,"loop":true,"tracks":{"root":[
      {"at_ms":0,"y":0},
      {"at_ms":1400,"y":-4,"ease":"ease-in-out"},
      {"at_ms":2800,"y":0,"ease":"ease-in-out"}
    ]}}
  }
}
```

---

## 7. 动作清单（状态语义）

| 动作 | 建议 | 说明 |
| --- | --- | --- |
| idle 池 | ≥10 个行为 | 呼吸、眨眼、观察、重心、伸展、休息等 |
| listening | enter/loop/exit | 聚焦、抬眼 |
| thinking | enter/loop/exit | 略偏、沉思 |
| speaking | enter/loop/exit | 轻节奏；表情层说话 |
| done / alert / quiet | 进出场 | 确认 / 警觉 / 低频安静 |

自然动作：准备 100–300ms、余势 120–500ms；禁止整图同步缩放抖动代替表演。

---

## 8. 本地安装与排错

1. 卸载旧版同 `id` 包。  
2. 安装 ZIP。  
3. 确认 `schema_version: 3`、`renderer: native-character`、version 符合预期。  
4. 实时预览检查接缝与动作。

| 问题 | 检查 |
| --- | --- |
| 安装 unmarshal int | `schema_version` 是否为整数 3 |
| 未声明贴图 | `assets.rig.textures` |
| 未知轨道 | bone/slot 名 |
| 关键帧不递增 | `at_ms` |
| 头身断开 | 分件接缝、预览合成 |
| 双脸 | body 是否仍有五官 |
| 耳朵飞了 | head 是否把耳朵画成独立纸片 |

---

## 9. Agent 发布前清单

- [ ] 客户端支持 `pet-performance-v3`，否则输出 2.0  
- [ ] `schema_version: 3`（整数）  
- [ ] 分件接缝：七态均通过合成自检（无插头、无横黑线、无双脸）  
- [ ] 七态表情头全尺寸；说话张嘴；default.native 列齐七态  
- [ ] ≥10 idle、≥12 reactions、≥6 expression、≥4 gaze  
- [ ] textures 白名单完整  
- [ ] `native/idle.png` 在 56/88/120 可辨；黑发/金发完整  
- [ ] 无 JS/WASM/URL/可执行文件  
- [ ] ZIP 仅一个 manifest  

更完整的 Agent 工作流与英文版见 Hub 页面：`/pet-pack-help`。
