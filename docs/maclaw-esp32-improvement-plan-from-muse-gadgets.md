# MaClaw ESP32-S3 Client 改进计划 —— 借鉴 Muse Gadgets SDK

- 日期：2026-10-04
- 对标对象：`gadgets.muse.ai` / [facebookincubator/muse-gadget-sdk](https://github.com/facebookincubator/muse-gadget-sdk)（Apache-2.0，已克隆至 `/tmp/muse-gadget-sdk` 做源码级研读）
- 改造对象：`D:/workprj/aicoder/iot-agentos`（ESP-IDF 6.0.x，产物 `maclaw_esp32s3_client.elf`，`main/` 175 个 .c / 67830 行）

---

## 0. 先纠正一个认知：Muse Gadgets SDK 不是"完整固件"

研读源码后的第一个结论，直接决定了哪些东西能借鉴、哪些不能：

`esp32/components/muse/` **不是一个独立的设备固件**，它是寄生在宿主应用 **Home Link**（`esp32/main/app.c`，111KB）之上的一层"语音伴侣 UI"。证据很硬：

- `muse_link.c/h` 名字像传输层，**实际只是一张回调表** `muse_link_ops_t`（`wifi_*` / `ble_*` / `hatch_vm` / `req_*` / `power_save` / `wifi_nap`），Wi-Fi、BLE 配对、凭据、OTA 全部由 Home Link 独占。
- `muse_chat_link.c` 的存在本身就是证明：无 PSRAM 的设备**借宿主 Link 的会话**来聊天，自己不建连接。
- 官方站也承认：配网只有 BLE + USB 串口两条路，**没有 captive portal**（全仓无 httpd / softap / dns_server）。

所以正确的对标方式是：**只借鉴它的"会话协议层 + 音频流水线 + 低内存渲染"这三块设计，不要借鉴它的工程组织**（`app.c` 111KB、`noise_control.cpp` 102KB、`muse_chat_session.cpp` 77KB 是反面教材）。

---

## 1. 能力对照：我们已经在哪些地方比它强

对标前先盘家底，避免"抄了反而退步"。以下四项**我们已经做得比 Muse 好，必须守住**：

| 能力 | MaClaw 现状 | Muse 现状 | 结论 |
|---|---|---|---|
| 凭据安全 | `services/secret_storage.c`：AES-128-GCM（magic `SCSV` + 96-bit nonce + 128-bit tag），密钥由 eFuse BLOCK_KEY0 经 `esp_hmac_calculate` 派生；`credential_service` 用单调 `generation` 做吊销栅栏 | NVS 明文存 token 和 Wi-Fi 密码，`CONFIG_ESP_WIFI_NVS_ENABLED=n` | **我们更强，别动** |
| 配网 | SoftAP + `esp_http_server` + 自建 captive DNS（`provisioning_service.c`）+ 二维码（`provisioning_qr_service.c`） | 只有 BLE GATT 明文 `key=value` + USB 串口 | **我们更强，别动** |
| 设备侧工具 | `device_tool_registry.c` 14 个工具（闹钟/作息/跌倒/电池/升级提醒/恢复出厂），带 JSON descriptor、`risk` 标记、`mutation` 幂等键、outbox 重试，经 `/tool-result` 回传 | **没有设备侧 function calling** | **我们更强，应放大** |
| 鉴权模型 | `/device-gateway/v1/pair` + `pair/voice` + Bearer + generation 吊销 | SDK token（`mgst_` + 43 字符）**硬编码进固件**，强绑 `api.muse.ai` | **我们更强，别动** |

另外我们有它没有的：4G（ML307）双上行 + `transport_selection_transaction.c` 事务化切换、`configuration_reconcile` 事务模型、`tools/host_tests/` 74 个主机端单测文件。这些都是资产。

---

## 2. 差距清单（按严重度排序）

| # | 差距 | 现状证据 | Muse 的做法 |
|---|---|---|---|
| G1 | **没有全双工会话**，靠 HTTP 长轮询 | `gateway_dispatcher.c:345`：`GET /api/im-gateway/v1/outgoing?clientId=%s&cursor=%lld&limit=1&timeout=%d`。全仓库零 `esp_websocket` 引用 | WS + Noise 长连接 + 单网络任务队列 |
| G2 | **音频上行是整包 WAV**，无压缩编码 | `audio_service.h:43` 16kHz PCM；全仓库零 `opus` 引用 | 它更糟：base64 WAV 塞进 JSON（**别学**） |
| G3 | **没有 OTA** | `update_service.h` 开头明写"deliberately has no firmware URL, downloader, flash writer or restart API"，16MB 设备要用户连电脑用 ClawMate Maker 刷。全仓库零 `esp_ota_*` | `main/ota.c`：`esp_https_ota` 流式 + SHA-256 签名 + `OTA_VERIFY_TIMEOUT_US` 300s 超时回滚 |
| G4 | **无真 VAD / AEC / barge-in** | `compact_audio_service.h` 只返回 `input_peak/level/mean_level`；`audio_arbitration_service.c` 处于 **SHADOW 模式**（只记分歧不改行为） | 也没有 AEC，但有 **pre-roll 环形缓冲 + ADPCM + 本地按键 barge-in + 世代号作废** |
| G5 | **能力是编译期静态的** | `board_profile.h` 只有 3 行编译期声明，服务端只能靠 `PRODUCT_ID/BOARD_ID/LAYOUT_ID/COMPAT_ID` 猜 | `muse_board_t` 自描述 + 把屏幕参数（`%dx%d %s`、1bpp/4bpp/16bpp）**写进工具 prompt** |
| G6 | **无 turn 生命周期与取消语义** | 没有世代概念，取消靠 `request_command_capture_stop` / `request_playback_stop` 逐个硬调 | `s_gen` 世代号：取消后该 turn 的事件与音频**自动全部作废** |
| G7 | **无延迟可观测** | 只有 `board_port.c` 5 秒周期的堆/总线诊断日志 | `mark()` / `log_marks()`：release/sent/ack/text/done/tts/mp3/audio 打点，turn 结束一次性打出 |
| G8 | **无构建期配置门禁** | 只有运行时 `provisioning_failure_injection.c` | `cmake/validate_config.cmake` 构建期硬校验（cJSON nesting=16、TCP 缓冲 16384、token 正则 48 字符） |
| G9 | **CI 未成体系** | `tools/host_tests/` 74 文件但靠 `check-*.ps1` 手动 gcc，无 CTest/Unity | host-tests + simulator（ASan）+ 16 板 build 矩阵，`fail-fast: false` |
| G10 | **仓库卫生** | 根目录 1354 条目中约 1248 个 `*.log`/`*.err`，`.gitignore` 仅 65 字节 | 干净 |

---

## 3. 改进计划

### P0-1　长轮询 → WebSocket 全双工会话（G1）

**目标**：把"HTTP 请求-响应"升级为"一条长连接上的会话"，把首包延迟和空闲功耗同时压下来。

**从 Muse 借鉴什么**（`esp32/components/muse/muse_chat_session.cpp`）：
1. **单网络任务 + 队列边界**：整条链路上只有一个任务碰 socket，外部通过命令队列 / 事件队列 / 两个 StreamBuffer 交互。这样取消、超时、重连都收口在一处，不会到处加互斥锁。
2. **`s_gen` 世代号**（G6 一并解决）：每个 turn 一个自增代号，取消时只 `s_gen++`，该 turn 在途的事件与音频自然作废。这比我们现在的"逐个 service 发 stop 请求"干净一个量级。
3. **`on_event()` 的 seq 单调去重**：`v <= s_last_seq` 直接丢弃，NDJSON 流式解析不整包缓冲。
4. **心跳与死亡判定参数**：`PING_US` 20s、无收包 `DEAD_US` 60s 判死、`IDLE_CLOSE_US` 10min 空闲关闭、重连退避 5s→120s。这组数值可以直接拿来当起点。

**从 Muse 借鉴，但要改掉的部分**：它断线后**无会话恢复、无应用层重传**，直接重建 TLS+WS+Noise。我们不该这样——我们**已有 `cursor` 消息流**，这是天然的重放锚点。升级为 `session_resume_token`：重连时带 `(clientId, last_seq, cursor)`，服务端从断点续推。这是我们能做得比 Muse 更好的地方。

**落地步骤**：
1. 新增 `main/services/session_transport.{c,h}`：独立的会话状态机（DISCONNECTED / CONNECTING / HANDSHAKING / LIVE / RESUMING / CLOSING），对内暴露 `submit_command()` / `poll_event()`，对外不泄漏 socket。
2. `gateway_transport.c` 增加能力协商：handshake 响应里带 `session.ws_url` 就走 WS，否则回落现有长轮询。**双栈并存至少一个版本**，便于灰度回滚。
3. 引入 turn 世代号，先用在命令捕获/播放停止路径上（`audio_service` + `server_audio_presentation_service`），替换现有的多点 stop 调用。
4. 服务端（hub / `datasrv`）配套新增 WS 端点与 resume 语义。

**验收**：空闲态电流下降可测；消息首包 P95 延迟下降；断网 30s 重连后无消息丢失、无重复（`seq` 去重生效）。

**风险**：WS 与现有 `esp_http_client` 的连接池、TLS 内存占用在 PSRAM 紧张时可能挤压。建议把 WS 任务的栈与 RX 缓冲显式放 PSRAM，并用 `board_port.c` 已有的堆诊断日志盯住。

---

### P0-2　音频上行：整包 WAV → Opus 流式（G2）

**现状**：16kHz/16bit PCM 录完整个 utterance，封成 WAV 整包 POST 到 `/media/upload-url`。会议场景（`meeting_service.c`）已经是分块流式 WAV + SHA256 + complete/process，说明**流式基础设施我们有**，只是没用在交互语音上。

**Muse 的做法是反面前车之鉴，值得单独说**：它默认 `VOICE_NOTE=1`，把整段录音做成 **base64 WAV 塞进 JSON** 当 voice note 发（`MUSE_HATCH_NOTE_HEAD`），代码注释自己承认"streaming dictation has no ASR behind it right now"，流式路径 `/api/voice/dictation`（24kHz PCM16，8192B/片≈170ms）被默认关闭。`muse_adpcm.c` 的 IMA ADPCM **根本不上网络**，只是 `MUSE_LOW_MEM` 时压本地 pre-roll 环形缓冲。
**结论：这一块不要借鉴它的实现，只借鉴它暴露出来的教训——整包上传 + base64 膨胀是死路。**

**落地步骤**：
1. `managed_components` 里已有 `espressif__esp_audio_codec`，直接启用 Opus 编码器。20ms 帧、24kbps，边录边发。
2. 复用 `meeting_service.c` 的分块上传协议骨架，抽象成通用的 `media_stream_upload`，交互语音与会议录音共用一套。
3. 服务端 ASR 改为流式输入，返回首字即可开始下行 TTS（与 P0-1 的 WS 复用同一条连接）。
4. 保留"整包 WAV"作为弱网/服务端不支持时的回落路径。

**收益**：上行流量降到约 1/10（24kbps Opus vs 256kbps PCM），并可把 ASR 首字结果提前一个整段录音时长。

**验收**：弱网（人为丢包 5%）下识别准确率不劣化；上行字节数下降 ≥80%。

---

### P0-3　真 OTA（G3）

**这是定位矛盾**：一个长生命周期联网设备，却要用户抱电脑来刷机。

**从 Muse 借鉴**（`esp32/main/ota.c`）：
- `esp_https_ota` 流式写入，不整包进 RAM；
- 先读 incoming image descriptor 做**版本门禁**（`force` 可跳过）；
- **SHA-256 + 签名校验**（其 `sdkconfig.defaults`：SECURE_SIGNED_APPS_NO_SECURE_BOOT + RSA + `dev_signing_key.pem`）；
- `PENDING_VERIFY` 状态 + `OTA_VERIFY_TIMEOUT_US` 300s 超时自动回滚；
- 支持外部触发（它由 BLE `ota_start` 触发，我们也可以由 device tool 触发）。

**但有一个硬约束必须先解决**：当前 `partitions.csv` 是 16MB 且**刚好用满**：

```
nvs      0x9000   0x6000     (24K)
phy_init 0xf000   0x1000     (4K)
factory  0x10000  0x3a0000   (~3.6MB app)
model    0x3b0000 0x300000   (3MB spiffs，esp-sr 模型)
storage  0x6b0000 0x950000   (~9.3MB spiffs，pet 资源/字库缓存)
                             合计 0x1000000 = 16MB，无空闲
```

OTA 需要第二个 app 分区（约 3.6MB）。**`storage` 的 9.3MB 明显偏大**，是唯一可动的空间（pet 帧缓存与字库缓存应有 LRU 上限）。

**落地步骤**：
1. 先给 pet 资源缓存加 LRU 与配额上限（`pet_asset_cache_storage.c`），实测稳态占用，把 `storage` 压到 ~5MB。
2. 新分区表增加 `ota_0` / `ota_1`，保留 `otadata`。
3. `update_service.c` 从"元数据提醒"升级为真 OTA 执行器：复用它已有的 `manifestSha256` / `release_sequence` 字段做版本门禁与防回滚（`CONFIG_MACLAW_RELEASE_SEQUENCE` 已在上报，正好用上）。
4. 首次分发走"迁移刷机"：因为老设备分区表不兼容，必须经 ClawMate Maker 刷一次带新分区表的固件，之后的升级才走 OTA。这个一次性成本要在发布说明里讲清楚。
5. 加 `device_tool`：`update_apply`，让 Muse 能远程触发（我们已有工具框架，这是纯增量）。

**风险**：分区表变更不可逆；回滚路径必须先在 `hardware-test/` 上跑通再放量。

---

### P1-4　设备能力清单（Capability Manifest）运行时上报（G5）

**Muse 最值得偷的一招**，不在代码而在思路：它**没有** capability negotiation 报文，能力是编译期静态 `muse_board_t`，云端适配靠**把屏幕参数写进工具 prompt**（`main/noise_control.cpp:1281-1310`）：`"1.75"圆形 466x466 16bpp RGB565"`、`"1bpp 黑白 e-paper，约 2s 刷新、会闪、断电保持"`、`"4bpp Spectra 6 六种墨水色（列出精确色值）"`，并提示"圆形屏会裁角，主体放中间"。

**我们该做一件 Muse 没做的事**：把它做成**运行时 manifest**，因为我们的板卡变体在增加，每次新板都要改服务端是不可接受的。

**落地步骤**：
1. 新增 `device_capability_manifest`：屏幕几何/色深/是否圆形/是否触摸、麦克风/扬声器/电池/4G 能力、pet 动画支持、CJK 字库覆盖版本、支持的音频编码集合、可用 device tool 列表（直接复用 `device_tool_registry_append_descriptors` 已有的 descriptor 生成逻辑）。
2. 随 handshake 上报，并随能力变化主动推一次增量。
3. 服务端据此生成渲染指令与工具 prompt，而不是猜 `BOARD_ID`。

**收益**：新板卡接入零服务端改动；服务端可以做"这设备能不能显示图片/能不能播音频"的判断，避免下发设备渲染不了的内容（这正是我们 `pet_asset` 链路最可能踩的坑）。

**验收**：新增一块板，服务端不改代码即可正确渲染与下发音视频。

---

### P1-5　打断、pre-roll 与音频前端（G4）

Muse 没有 AEC，但它有两个细节值得直接搬：
1. **pre-roll 环形缓冲 + ADPCM 压缩**（`muse_voice.c` + `muse_adpcm.c`）：保留按键前 ~300ms 的音频，避免吞掉开头的字；低内存时 4-bit ADPCM 压缩，每个 chunk 存解码起始状态以便单独解码。
2. **`pre_reset()`**：自己刚播出去的声音不能污染 pre-roll——这个坑不写就一定会踩。
3. **本地 barge-in**：说话期间按键即 `muse_hatch_turn_cancel()`，配合世代号作废在途音频。

**落地步骤**：
1. `audio_arbitration_service.c` 从 SHADOW 转正——这是**当前最该收尾的技术债**，影子双跑已经积累了分歧日志，应该做决策而不是继续观察。
2. 启用 esp-sr 的 AFE（我们已依赖 `espressif__esp-sr`，目前只用了 MultiNet 命令词），拿到 AEC + 真 VAD + barge-in 检测。
3. 加 pre-roll 缓冲（300ms，PSRAM），并落实 `pre_reset()` 语义。
4. barge-in 与 P0-1 的世代号打通：检测到打断 → `gen++` → 在途 TTS 播放与渲染自动作废。

**验收**：扬声器播放中可唤醒并打断；唤醒词首字不被吞。

---

### P1-6　低内存渲染：条带化（G7 相关）

Muse 在这块的工程细节最扎实，且我们**不用 LVGL**（全自绘）也完全适用：
- `muse_pixel_scale(dst, stride, x0,x1,y0,y1)`：**按显示条带逐块生成像素画，全屏图像永远不必存在于 RAM**。
- `boards/muse_lcd_bands.c`：QSPI 屏按"几条高条带"而非 40 多条短带渲染，两个 internal 缓冲乒乓；注释里写清了一个关键坑——**SPI 中断与发送任务必须同核**，否则 IDF SPI bus lock 丢唤醒、屏幕半刷新死锁。
- `muse_lv_mem.c`：把 LVGL 分配重定向到 PSRAM，绕开 IDF 把 <16KB 分配全放 internal RAM 的默认行为（几百个控件会饿死 Wi-Fi 与 DMA bounce buffer）。

**落地**：`main/presentation/` 与两份渲染器（`compact_renderer.c` 3390 行 / `board_port.c` 3926 行）在解码 RGB565A8 pet 帧和渲染整屏时，改为条带流水；大块资源缓冲显式指定 PSRAM。SPI 同核约束要在 `shared_bus_lifecycle.c` 里显式固化并注释。

---

### P2-7　延迟可观测（G7）

搬 `mark()` / `log_marks()`：在 turn 生命周期的关键里程碑（按键释放 / 音频发出 / 服务端 ack / 首字文本 / 文本完成 / TTS 开始 / 首帧 MP3 / 首帧播出）打点，turn 结束时**一次性**打出一行。这个成本极低但收益极高——没有它，P0-1/P0-2 的优化效果根本无法归因。

---

### P2-8　构建期门禁与 CI（G8 / G9）

1. 新建 `cmake/validate_config.cmake`：构建期硬校验关键配置（类似 Muse 校验 cJSON nesting=16、TCP 收发缓冲 16384、凭据格式）。我们已有的 `provisioning_failure_injection.c` 是**运行时**注入，正好互补。
2. 把 `tools/host_tests/` 的 74 个文件接入 CTest（`tools/host_tests/mocks/` 与 stub 已具备条件，缺的只是统一 runner），并接进 CI。
3. 参考 `.github/workflows/esp32.yml` 的三 job 结构（host-tests / simulator-ASan / 16 板 build 矩阵 `fail-fast: false`），把我们的 5 块板纳入矩阵。

---

### P2-9　仓库卫生与架构债（G10 + 附带）

这一项不来自 Muse，是研读过程中对照出来的自伤：
1. **根目录约 1248 个 `*.log`/`*.err`**（1354 条目里占 92%），`.gitignore` 仅 65 字节。先补 `.gitignore` 并归档清理。
2. **六个巨石文件**：`main.c` 5538、`board_port.c` 3926、`compact_renderer.c` 3390、`configuration_service.c` 2637、`provisioning_service.c` 2352、`power_service.c` 2325。注意 Muse 同样有 `app.c` 111KB 的问题——**这是行业通病不是我们独有，但 `board_port.c` 名不副实（实际是共享圆屏渲染器，只能靠 CMake 首行注释解释）必须改名**。
3. **影子模式堆积**：`foreground_coordinator`、`audio_arbitration`、`scene_presenter` 三处 SHADOW 双跑。每个影子都要么转正要么删除，不能长期双跑。
4. **圆屏/方屏双份实现**（`compact_*_service` vs `round_*_service` 各写一遍）：借 P1-4 的能力 manifest 抽象出差异，收敛为一份 + 一份几何/字形 profile。
5. **死依赖**：`managed_components` 里的 `espressif__mqtt` 全仓库零引用，删掉。

---

## 4. 明确"不要抄"的清单

| Muse 做法 | 为什么不要抄 |
|---|---|
| base64 WAV 塞进 JSON 上行 | 4/3 膨胀 + 整段缓冲，是我们当前整包 WAV 的更差版本 |
| 上行无压缩编码 | 直接上 Opus |
| 无会话恢复、无应用层重传 | 我们有 `cursor`，应做成 resume，比它更强 |
| NVS 明文存 token / Wi-Fi 密码 | 我们已有 AES-GCM + eFuse 派生，**别退步** |
| SDK token 硬编码进固件、强绑 `api.muse.ai` | 我们是自建 hub + pair 模型，保持自主 |
| 无 captive portal | 我们有，是优势 |
| 巨型单文件（`app.c` 111KB） | 两头都要治 |
| `muse_link.c` 裸全局 ops 指针无生命周期管理 | 我们的 `*_service` 生命周期已经更规范 |
| `start_tts()` 是空桩（设备"不会说话"） | 我们的 TTS 下行链路是通的 |

---

## 5. 实施顺序建议

```
阶段一（实时性与链路）
  P0-1 WS 会话 + 世代号  ──┐
  P0-2 Opus 流式上行      ──┼── 共用同一条长连接，一起做
  P2-7 延迟打点（先做！） ──┘     没有打点 = 无法验收前两项

阶段二（可运维）
  P0-3 真 OTA（先做分区表与缓存配额，风险最高，最早启动）
  P1-4 能力清单 manifest

阶段三（体验）
  P1-5 音频前端转正 + pre-roll + barge-in
  P1-6 条带化渲染

阶段四（工程）
  P2-8 构建门禁 + CI
  P2-9 仓库卫生与架构债收敛
```

两条硬依赖关系：**P2-7 必须先于 P0-1/P0-2**（否则无法证明收益）；**P1-4 应先于新板卡接入**（否则每块板都要改服务端）。

---

## 附录：Muse SDK 源码速查

| 想看什么 | 文件 |
|---|---|
| 传输栈与会话主循环 | `esp32/components/muse/muse_chat_session.cpp` |
| Noise_XX_25519_AESGCM_SHA256 握手 | `esp32/components/noise_core/src/InitiatorHandshake.cpp:27` |
| 分帧（chunk_id/index/total，65535-16-30） | `.../include/xplat/noise/core/TransportFrameCodec.h` |
| 业务流多路复用（stream_id + ServiceFrameKind） | `.../include/xplat/noise/core/ServiceCodec.h` |
| 世代号取消、seq 去重、延迟打点 | `muse_chat_session.cpp`（`s_gen` / `on_event` / `mark`+`log_marks`） |
| pre-roll + ADPCM + `pre_reset` | `muse_voice.c` + `muse_adpcm.c` |
| 条带渲染 + SPI 同核坑 | `esp32/components/muse/boards/muse_lcd_bands.c` |
| 像素画按条带生成 | `esp32/components/muse/avatar/muse_pixel.c`（`muse_pixel_scale`） |
| LVGL → PSRAM 分配重定向 | `esp32/components/muse/muse_lv_mem.c` |
| 电量归因（解析 `esp_pm_dump_locks`）+ RTC 跨重启 | `muse_battery.c` |
| 电源键/PMU/睡眠阶梯 | `muse_pmu.c` |
| 板卡 HAL 自描述结构体 | `muse_board.h`（`muse_board_t`） |
| 构建期配置硬校验 | `esp32/cmake/validate_config.cmake` |
| 多板卡 sdkconfig + CI 矩阵 | `esp32/devices/`、`.github/workflows/esp32.yml` |
| OTA + 签名 + 超时回滚 | `esp32/main/ota.c` |
| 屏幕参数写进工具 prompt | `esp32/main/noise_control.cpp:1281-1310` |
