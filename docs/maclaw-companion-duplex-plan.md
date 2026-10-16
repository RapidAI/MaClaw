# MaClaw 电子伴侣 · 双工聊天 改进计划

- 日期：2026-10-04
- 定位：**电子伴侣（AI companion）**，核心交互形态为**双工语音聊天**
- 对象：`D:/workprj/aicoder/iot-agentos`（ESP-IDF 6.0.x，`maclaw_esp32s3_client.elf`）+ 服务端 `corelib/` / `MaClawSrv`
- 前置调研：`docs/maclaw-esp32-improvement-plan-from-muse-gadgets.md`（Muse Gadgets SDK 对标）

---

## 0. 先把定位说清楚：从"语音遥控器"到"电子伴侣"

当前固件的本质是**命令-响应式语音终端**：

```
唤醒词 → 录完整包 WAV → 上传 → ASR → LLM → TTS 整包 → 播放 → 回到待机
```

它缺三样东西，而这三样正是"伴侣"的定义：

1. **不会主动找你** —— 所有 turn 都由设备发起，服务端无法主动开口
2. **不能边说边听** —— 没有 AEC，播放时必须闭麦，否则自己的声音会被录进去
3. **没有"它"** —— pet 只是云端下发的动画帧序列，设备本地无状态、无情绪、无记忆

**电子伴侣 = 常在线 + 有本地状态 + 可打断 + 会主动。**

---

## 1. 现状盘点（按伴侣维度）

### 已有的资产（守住并放大）

| 能力 | 位置 |
|---|---|
| pet 动画链路（11 个服务） | `pet_asset_download/apply/integrity/profile/restore/runtime/cache` — RGB565A8 帧序列下载 + SPIFFS 缓存 + 按 `frame_ms` 播放 |
| ambient 场景（时间/日期/星期/天气/网络/闹钟） | `ambient_service.c`，`scene_model.h` 的 `SCENE_KIND_AMBIENT`、`SCENE_PET_STATE`（speaking/thinking/alert）、`SCENE_PET_SKIN` |
| 开机问候 | `startup_welcome_service.c`、`startup_chime.wav`、`guiapp/hello-maclaw.wav` |
| 生活场景素材 | `alarm_manager` / `sleep_schedule_service` / `weather_cache_service` / `battery_policy_service` / 跌倒检测 |
| **离线 ASR** | `corelib/asr/`（SenseVoice + Moonshine），服务端本地推理，可做流式 |
| TTS 合成链路 | `corelib/agentservice/dynamic_host_audiosynthesize.go` |
| 多形态硬件 | 圆形 AMOLED 1.75"（466px 圆屏）、方形 LCD、4G（ML307）版、面包板紧凑版 |
| 安全底座 | `secret_storage.c` AES-128-GCM + eFuse 派生；captive portal + QR 配网 |

### 缺失的（本次要补）

- 无情绪 / 好感度 / 成长系统 —— pet 状态是服务端下发的字符串，设备无状态机
- 无长期记忆与人格一致性
- 无主动搭话（proactive turn）
- 无 AEC → 无法边播边录 → 无法真打断（`audio_arbitration_service.c` 仍在 SHADOW）
- 无触感反馈通道（抚摸 / 摇晃 / 抬起）
- 无离线兜底 —— 断网后 pet 只剩 SPIFFS 里最后一帧缓存，无内置帧
- 无延迟预算与打点

---

## 2. 双工聊天的分级定义（别把"能打断"当成双工）

这是本计划里最容易产生误解的地方，先定级再谈目标：

| 级别 | 定义 | 我们的位置 |
|---|---|---|
| **L0** | 单工 PTT（按键说话，松开发送） | — |
| **L1** | 唤醒词 + 整包轮次：录完上传，整包 TTS 下载播放，播放时闭麦 | **当前状态** |
| **L2** | 流式半双工：流式 ASR 上行 + 流式 TTS 下行 + 可打断。**需 AEC 才能"边播边听"** | 目标中途站 |
| **L3** | 全双工：持续音频流上行，服务端持续 VAD + **语义端点**检测，用户可随时插入、可被打断，AI 可用"嗯/啊"短回应填充，**服务端可主动发起 turn** | **建议目标** |
| **L4** | 端到端语音到语音（Moshi / GPT-4o Realtime 风格），跳过文本，带副语言（笑声、语气） | 研究方向，不排期 |

**建议目标定在 L3。** 理由：L3 的全部组件（AEC、流式 ASR、流式 TTS、世代号取消、服务端主动推送）我们都已有部分基础或现成依赖；而 L4 依赖模型侧能力 + 服务端常驻会话，带宽与成本对电池设备不可控。

---

## 3. 改进计划

### A. 双工链路（P0）

#### A1　AEC 与常开麦 —— 整个双工的地基

- **现状**：`compact_audio_service.h` 只返回 `input_peak/level/mean_level` 能量统计，全仓库无 AFE/AEC 调用；`audio_arbitration_service.c` 处于 SHADOW（只记分歧不改行为）。
- **依赖已就绪**：`managed_components/espressif__esp-sr`（含 `esp_afe_sr_1mic` 模型）+ `espressif__esp_audio_codec`（含 Opus 编解码器）。不需要新增外部依赖。
- **动作**：启用 esp-sr AFE，拿到 AEC + 真 VAD + barge-in 检测；`audio_arbitration_service` 从 SHADOW 转正。
- **⚠️ 最容易翻车的一步**：AEC 需要**参考信号**——即播放链路的 PCM 副本。当前 `server_audio_presentation_service` 解码 MP3 后直接写 codec，必须在这条链路上分叉出参考信号喂给 AFE，且**两条路径的采样率/时延要对齐**，否则 AEC 抵消不干净。
- **建议**：先做 2 天技术验证（spike）确认参考信号对齐方案，再排期。
- **验收**：扬声器满音量播报时唤醒词不误触发；用户以正常音量插话可被检测。

#### A2　持续音频流上行 + 服务端语义端点

- 不再"录完整包 WAV"，改为 20ms 帧 Opus 24kbps 持续推流（上行流量降到约 1/10）。
- 复用 `meeting_service.c` 已有的**分块上传骨架**（分块 WAV + SHA256 + complete/process），抽象成通用 `media_stream_upload`，交互语音与会议录音共用。
- 服务端用 SenseVoice 做流式识别，端点判定用**语义端点**（判断"这句话说完了吗"）而非静音时长。
- **反面前车之鉴**：Muse 靠 3s 静默（`SETTLE_US`）判定 turn 结束，会吞掉用户的迟疑和停顿。不要抄。

#### A3　世代号（turn generation）+ 全链路取消

- 借鉴 Muse `muse_chat_session.cpp` 的 `s_gen`：取消一个 turn 只需 `gen++`，该 turn 在途的 ASR 结果、LLM token、TTS 音频、屏幕渲染**全部自动作废**。
- **为什么必须做**：现在是逐个 service 发 stop（`request_command_capture_stop` / `request_playback_stop`）。在双工下这必然出现竞态——你停了播放器，但服务端还在生成，下一帧又会播出来。
- 落地：新增 `interaction_turn_service` 持有当前 gen，所有消费方（音频、渲染、工具执行）带 gen 校验。

#### A4　流式 TTS 下行 + 边下边播

- 服务端 `dynamic_host_audiosynthesize` 需支持分块返回；设备端 MP3 流式解码 + 首帧缓冲 200–300ms 抗抖动。
- Muse 的 `minimp3` 流式播放实现（`muse_chat_session.cpp` 的 `MP3_BUF`）可直接参考。

#### A5　传输：长轮询 → WebSocket（**阻塞项**）

- **现状**：`gateway_dispatcher.c:345` 是 `GET /api/im-gateway/v1/outgoing?clientId=%s&cursor=%lld&limit=1&timeout=%d`。长轮询**物理上无法做双工**——服务端没有主动推的通道，A6 主动搭话就无从谈起。
- 动作：新增 WS 会话通道，与长轮询双栈并存一个版本便于灰度；WS 任务的栈与 RX 缓冲显式放 PSRAM。
- 我们已有 `cursor`，应升级为 `session_resume_token` 做断点续推（Muse 没有会话恢复，这是我们能做得比它更好的地方）。

#### A6　服务端主动 turn（proactive）

- 协议层把 turn 发起方从"只能设备"泛化为双向：`{turn_id, origin: device | server, ...}`。
- 设备侧需要**主动播放准入策略**：勿扰时段、`sleep_schedule`、电量、是否正在会议录音。复用 `power_lease_service` 的租约思路做准入，而不是硬编码 if-else。

---

### B. 电子伴侣人格与状态（P1）

#### B1　本地情绪/状态机

- 现状：pet state 是服务端下发的字符串，设备无状态。
- 改为：设备持有轻量本地状态机（`mood` / `energy` / `attention`），由本地事件驱动（交互频率、时间、电量、被抚摸、闹钟、天气）；**服务端下发的是"建议"而非"命令"**。
- 收益：断网时 pet 仍有合理行为，而不是冻在最后一帧。

#### B2　好感度与成长

- 电子伴侣的留存引擎。本地存 `affinity` 数值 + 阶段（陌生 / 熟悉 / 亲密），跨 OTA 保留。
- **必须防刷**：不能靠说话次数线性累加，否则用户会对着它念经。用"有效交互"计分（有实质内容、非重复、有时间间隔），并设日上限。
- 落在 `configuration_service` 的 V7 blob 内，并纳入 `factory_reset_policy` 的个人数据擦除白名单。

#### B3　长期记忆与人格一致性

- 服务端侧：记忆条目 + 召回；设备侧只承载"记忆被使用"的提示（屏显"我记得你说过…"）。
- 人格：一份固定 persona prompt + 少量可演进的口头禅，避免每次会话人设漂移。

#### B4　主动关怀脚本 ⭐

基于已有素材做，成本极低：

| 触发源 | 场景 |
|---|---|
| `weather_cache_service` | 早间简报、天气突变提醒带伞 |
| `alarm_manager` / `sleep_schedule_service` | 睡前提醒、起床问候 |
| `battery_policy_service` | 低电量"我要睡了"告白（把缺陷变成性格） |
| 交互时间戳 | 久未互动时的"想你了" |

- **频率必须有预算**（例如每天主动搭话 ≤ N 次、同一场景不重复），否则伴侣会变骚扰——**这是电子伴侣最常见的翻车点**。
- 用"主动搭话被忽略/被打断的比例"作为调节依据（见 C1 可观测性）。

#### B5　多模态反馈通道

- **把"被抚摸"做成一级交互**：语音是功能，触摸是情感。圆屏 AMOLED 与触控硬件已有，成本极低。抚摸 → pet 反应 + 音效。
- **音效库**：现在只有 `startup_chime.wav` / `hello-maclaw.wav`。需要一套状态音（唤醒应答、思考中、错误、开心、困倦）。
- 音效与 TTS 的混音/抢占走 A3 的世代号，不要另起一套。

#### B6　离线兜底

- 断网时：本地唤醒词仍响应固定短语 + 本地 pet 动画 + 时钟/闹钟/天气缓存照常。
- 需要"云端 pet 帧"与"本地内置 pet 帧"的降级切换（现在有 SPIFFS 缓存，但无内置兜底帧）。

---

### C. 工程与体验保障（P1 / P2）

#### C1　延迟预算表（伴侣体验的天花板）

先定预算再优化，不要盲目调参：

| 阶段 | 预算 |
|---|---|
| 用户说完 → 服务端收到尾帧 | ≤ 150 ms |
| 语义端点判定 | ≤ 200 ms |
| LLM 首 token | ≤ 600 ms |
| TTS 首帧到达设备 | ≤ 900 ms |
| 设备首帧播出（含 pre-roll 缓冲） | ≤ 1200 ms |

**端到端目标 ≤ 1.2 s 出声。超过 2 s 伴侣感就消失。**

配套：搬 Muse 的 `mark()` / `log_marks()` 做里程碑打点（release / sent / ack / text / done / tts / mp3 / audio），turn 结束一次性打出。**没有打点就无法归因任何优化效果。**

#### C2　OTA（优先级较上一版上调）

- 电子伴侣是长期关系型产品，不能 OTA 就无法迭代人格、音效库和 pet 资源 —— **OTA 对伴侣产品是生命线，不是可选增强。**
- 硬约束：16MB 分区表刚好用满（factory 3.6MB / model 3MB / storage 9.3MB）。必须先给 pet 资源缓存加 LRU 配额把 `storage` 压到 ~5MB，且老设备需一次性迁移刷机。
- 参见上一版文档 P0-3 的完整步骤。

#### C3　能力清单 manifest

- 对伴侣更重要：圆屏 / 方屏 / 4G / 未来墨水屏的 pet 表现必须自适应，服务端不能靠 `BOARD_ID` 猜。
- 上报屏幕几何、色深、圆形/触摸、麦克风/扬声器/电池/4G、pet 动画支持、音效通道、CJK 字库版本、可用 device tool 列表。

#### C4　隐私与"被监听"焦虑（双工的副作用）

- 双工 = 常开麦 = 用户焦虑。必须做**可见的麦克风状态**（屏幕/LED 明确显示是否在听）。
- **本地 VAD 先行**：只在 VAD 触发后才上行音频，而不是全时段推流——既是隐私也是流量优化。
- 硬件支持时提供物理静音开关。

#### C5　电池与常在线的矛盾

- 伴侣要常在线，但设备是电池的。现有 `power_lease_service` + `wake_deadline_service` 是可逆事务模型，方向正确。
- 需要新增"伴侣心跳"档位：定期轻唤醒播报/动画，而非持续在线。

---

## 4. 额外建议（十条，不都来自 Muse）

1. **先定义"它是什么"，再写代码。** 产品叫 MaClaw（码卡龙），但固件里没有任何"龙"的性格定义。建议先写一页纸角色设定（性格、说话方式、禁忌、口头禅），让所有工程决策有锚点。否则双工做得再快，出来的还是一个"会说话的工具"。

2. **不要一上来追 L4 端到端语音。** Moshi / GPT-4o Realtime 很火，但要求模型侧能力 + 极高带宽 + 服务端常驻会话，成本与稳定性都不适合电池设备。L3 已能给出约 90% 的伴侣体验。

3. **"主动"比"更快"更能建立伴侣感。** 延迟从 2s 优化到 1.2s 用户感知有限，但"它主动提醒我带伞"会直接产生情感连接。**资源有限时优先做 B4 主动关怀，而不是死磕延迟。**

4. **给"沉默"设计行为。** 伴侣设备 90% 的时间在待机，而现在的待机只有 ambient 场景。应设计：待机微动作（眨眼、打盹、看向用户）、久置后的困倦、被拿起的惊喜。**待机体验才是伴侣体验的主体，这里投入产出比最高。**

5. **把"被抚摸"做成一级交互**（同 B5）。这是最便宜的情感通道。

6. **设一条硬红线：不撒谎、不谄媚。** 伴侣产品极易陷入讨好用户。人格设定里写死边界：不编造事实、不过度奉承、不假装有人类情感。

7. **先做"断网不傻掉"，再做联网功能**（同 B6）。伴侣的信任来自可靠性。

8. **可观测性从第一天建。** 延迟打点、交互成功率、打断率，尤其**主动搭话被忽略/打断的比例**——这是调节主动频率的唯一依据。

9. **别低估 AEC 的参考信号工程**（见 A1）。这是整个双工计划最容易翻车的一步，先 spike 再排期。

10. **提前留多设备 / 多用户的缝。** 现在 `clientId = device_id` 是单设备单用户模型。一旦出现第二台设备或家庭多人使用，人格与记忆是否共享？建议在协议里现在就留 `user_id` 字段，将来改动成本极高。

---

## 5. 排序：如果只能挑三件

1. **A1 + A2 + A3**（AEC、流式上行、世代号）—— 双工的地基，B/C 大部分项都卡在这上面
2. **B4 主动关怀** —— 伴侣感的最高性价比来源
3. **C2 OTA** —— 关系型产品的生命线

而 **C1 的延迟打点必须作为第 0 件事先插进去**，否则前两项的收益无法证明。

```
第 0 步   延迟打点（mark/log_marks）        ← 无它则一切优化无法验收
   ↓
阶段一    A1 AEC + A2 流式上行 + A3 世代号   ← 双工地基
   ↓
阶段二    A5 WebSocket + A6 服务端主动 turn + B4 主动关怀
   ↓
阶段三    B1 情绪状态机 + B2 好感度 + B5 触摸/音效 + B6 离线兜底
   ↓
阶段四    C2 OTA + C3 能力清单 + C4 隐私可见化 + C5 伴侣心跳
```

A4（流式 TTS）与 A5 同属一条链路，应与阶段二并行推进。
