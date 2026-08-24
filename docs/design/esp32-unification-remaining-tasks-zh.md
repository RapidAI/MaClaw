# MaClaw AgentOS ESP32 HAL 与业务统一剩余任务清单

> 2026-08-23 C7 SAFE_MODE bridge 失败闭锁补强（全 profile 共用）：审计发现此前 root 将 `startup_enter_safe_mode()` 的所有 `false` 统一送入 `startup_enter_degraded()`；但 coordinator 已开始 `quiesce_nonessential` 后的阶段失败，可能已有部分 worker/generation 被退休，普通 startup rollback 的“未触碰完整启动图”前提不再成立。现将结果精确区分为 `NOT_STARTED`、`ACTIVE`、`TERMINAL_FAILURE`：仅 coordinator 初始化前失败走既有普通 degraded rollback；admission 已关闭且 coordinator 返回失败时，转入新的 `startup_enter_safe_mode_terminal_failure()`，保持 Gateway/wake/deferred-setup/Input 普通准入关闭，不触发 `startup_stop_local_workers()`、System Sleep ABORT 或任何旧 generation 重建，只发布本地诊断并 fail-closed。`check-safe-mode-coordinator.ps1` 已锁定两处 late-boot 调用点的该分流以及 terminal path 的普通 rollback 禁止。该源码/host 证明不替代 bridge-stage FI 与 COM3–COM6 HIL；端口恢复后应专门验证 quiesce、clock、alarm、diagnostic 各阶段失败均无 panic、WDT、reset loop 或普通交互复活。

> 2026-08-23 C7 SAFE_MODE 输入准入可执行回归补强（全 profile 共用）：将“响铃 alarm 的 primary 实体键/触控 dismiss 必须先于 SAFE_MODE 普通输入拒绝”抽为私有、value-only `presentation/safe_mode_input_policy.[ch]`；Input Binding 仅把已归一化的 alarm-ready/ringing、primary-source 与 safe-mode 事实交给该 policy，仍不接触 GPIO、触屏坐标、SDK/RTOS 或 board handle。另将该 policy 决策前置到 display-wake 后、fall-prompt/command-capture-stop 前：SAFE_MODE 由 root 同步关闭后，即使非关键服务的异步 stop 尚在收敛，同一触控/按键事件也不会进入其取消/停止支路；DISPLAY_OFF 的首次交互仍只唤醒保留的本地诊断/闹钟 surface。新增 host matrix 覆盖正常普通输入继续、SAFE_MODE primary dismiss、非 primary ringing-input 拒绝、SAFE_MODE 的 primary/secondary/Alarm 尚未初始化时普通输入均拒绝；`check-safe-mode-input-policy.ps1` 同时锁定 Binding 必须复用该 alarm-first policy，且 SAFE_MODE gate 必须先于 fall/capture foreground policy。正式 Waveshare profile 已以此完整重建（app `0x49ab60`，余 `0x654a0` / 8%）。因此该缺口已有源码级可执行证明，但**尚不等价于实机触屏/按键 HIL**：COM3–COM6 恢复后仍必须在 FI 镜像上逐板验证普通输入不启动录音/会议/配网，以及响铃 alarm 能被各板 primary 输入 dismiss。

> 2026-08-23 C7 force-setup 权威配置事务实机验证（Waveshare 1.75C / COM6）：新增的专用、非发布 `waveshare-amoled-1.75c-safe-mode-force-setup-fi` 已完成完整构建（app `0x49ab00`，32 MiB factory app 分区余 `0x65500` / 8%）与完整段写入；bootloader、partition、model、app、storage 均由 esptool hash verify。该 FI 只令 `configuration_service_take_force_setup()` 在**成功读取 durable snapshot 后、清除/写回 force_setup 之前**失败，不暴露 Hub/HTTP/console/runtime 开关，也不改变持久化凭据或配对状态。COM6 冷启动已依次确认 `local_ready=ok`、Input/Power/Wake Deadline/Sleep Schedule 就绪；随后日志确认 `board pet animation task stopped`、`command cancel worker stopped`、`fall detection monitoring stopped`，安全模式保留本地时钟/闹钟诊断，最终记录 `SAFE_MODE active: phase=5 device_status=7 reason=configuration force-setup request`。此观察窗口未出现 panic、Guru、WDT 或 reset loop。**它补足了唯一 production-candidate 配置事务失败的单板 HIL，但仍未完成普通触控拒绝、响铃闹钟 dismiss、bridge stage failure、COM3/COM4/COM5、Wi-Fi/4G 故障域与 runtime restart 验证；FI 不可发布。**

> 2026-08-23 COM6 正式回刷阻断与构建修复：受控 FI HIL 后尝试使用 `waveshare-amoled-1.75c` 正式 profile 回刷时，复现两项独立问题：① ESP-IDF 的早期 component-requirements pass 未稳定保留 wrapper 选择的 Waveshare 依赖，导致 `esp_lcd_co5300.h` / `esp_lcd_touch.h` 被错误裁剪；`main/CMakeLists.txt` 现单独固化 `MACLAW_DEPENDENCY_PROFILE`，在 Kconfig 前按 wrapper 环境选择 profile-private public requirement。② 旧正式工件可到达 `BOOT_STATUS.ready=true`，随后主任务发生 stack overflow 并重启；Waveshare profile 已显式将 `CONFIG_ESP_MAIN_TASK_STACK_SIZE` 提升到 8192 字节，避免 composition root 在所有 host descriptor 同时存活时依赖 IDF 3584 字节默认值。2026-08-23 已同步修正提交型 `sdkconfig.waveshare-amoled-1.75c` 中残留的 6144 值，并完成正式 profile 的 clean reconfigure/build；随后纳入 SAFE_MODE 输入策略后再次完整重建，生成配置确认 `CONFIG_ESP_MAIN_TASK_STACK_SIZE=8192`，当前正式 app `0x4917b0`（`4,827,984` bytes）仍在 32 MiB factory app 分区内、余 `0x6e850`（9%），`check-safe-mode-coordinator`、`check-safe-mode-input-policy`、`check-hal-boundaries`、`check-configuration-transaction` 均通过。Windows 当前仅枚举 COM1，COM6 仍未恢复，故**不能声称已回刷或已验证栈修复**。端口恢复后必须先以 `tools/build-profile.cmd waveshare-amoled-1.75c -p COM6 flash` 写入，再观察至少一次完整正常启动及 60 秒稳定运行；若仍有溢出，先保留串口 backtrace 再继续切片定位。

> 2026-08-23 C7 SAFE_MODE 四 profile 构建补充（Fangtang-4G）：专用、非发布 `fangtang-4g-safe-mode-fi` 已完成完整 configure/build/link。首次 app `0x389d70`、仅余 `0x16290`（2%）的问题已定位为新建 FI 默认使用 debug 优化/断言；现将 size 优化与静默断言写入该 FI defaults 后，清洁重配/完整重链生成 app `0x355840`，最小 app 分区 `0x3a0000` 余 `0x4a7c0`（8.0%），与正式 `build-unified-fangtang` 的 `0x34bc00` / `0x543e0`（9.1%）处于同一容量级别。因此这证明 COM5 的编译配置可覆盖同一 SAFE_MODE contract，**仍不构成 COM5 HIL，也不允许将此 FI 镜像作为发布或常规刷机候选**。COM3、COM4、COM5 当前均未在线，待端口恢复后仍须逐板刷入对应 FI、验证普通输入拒绝与响铃闹钟 dismiss，再立即回刷各自正式产物。

> 2026-08-23 C7 SAFE_MODE 首个实机证明（Waveshare 1.75C / COM6）：已使用专用、非发布 `waveshare-amoled-1.75c-safe-mode-fi`（仅 `CONFIG_MACLAW_TEST_BUILD=y` + `CONFIG_MACLAW_SAFE_MODE_TEST_LOCAL_READY_FAILURE=y`）完成完整 configure/build/link（app `0x49aaf0`，32 MiB factory app 分区余 `0x65510` / 8%）并写入 COM6；esptool 对 bootloader、partition、model、app、storage 各段均完成 hash verify。115200 串口冷启动确认 Waveshare profile（466×466）、PSRAM、显示双缓冲、触控、音频、Input、Power、Wake Deadline、Sleep Schedule 均先到达 `local_ready=ok`；随后测试注入在 `LOCAL_READY` 触发，日志按预期记录 `board pet animation task stopped`、`command cancel worker stopped`、`fall detection monitoring stopped`，再发布“本地时钟和闹钟仍可用”与带 failed phase/status 的诊断 surface，最终 `SAFE_MODE active`，无 panic/Guru/WDT/reset loop。最终状态保持 Connectivity 未就绪、无 capture/playback session、Fall Detection 不可用，符合最小本地服务而非普通 online boot 的边界。测试后 COM6 已回刷 `build-unified-waveshare` 中既有正式 artifact，bootloader、partition、model、app、storage 的写入均完成 hash verify。**本次只证明 COM6 的受控 late-boot 进入/最小依赖链；未操作触屏，因而尚未证明普通触控被拒绝或响铃闹钟可 dismiss；也未覆盖 force-setup 真失败、bridge stage failure、COM3/COM4/COM5、Wi-Fi/4G fault domain 或 runtime restart。**

> 2026-08-23 C4 Provisioning secret-lifetime 收口（全 profile 共用）：现有 portal 已不是开放 AP：每次 portal generation 使用随机临时 WPA2-PSK，WPA2 配置、二维码与屏显均走私有 host/driver 适配；APSTA 场景在 DNS/HTTP 暴露前 fail-closed 校验 AP client isolation（显式禁用 NAPT，默认禁用转发）。CSRF、TTL、每 peer 限流、HTTP/body deadline 与 staged candidate rollback 既有；本轮再将 Wi-Fi/EAP/Hub/pair-code 表单、运行时 Wi-Fi snapshot、staged request、删除 SSID 与临时 AP passphrase 全部收进带 `mbedtls_platform_zeroize` cleanup 的短生命周期 scope，任意 handler 返回及 portal-start 返回都会擦除；Wi-Fi driver 在 AP config 提交后也擦除其本地 config copy。`check-provisioning-extraction.ps1` 锁定这组契约，Bread 对象编译与 HAL gate 通过。**这仍是 WPA2 保护 AP 内的 HTTP 表单，而非 TLS/设备身份绑定的加密配网会话；无 secure-element/credential service、可信时间 TLS 引导或 COM3–COM6 无线/isolation/HIL，且 post-save 仍整机 reset，不可声明 C4 或 runtime transition 已完成。**

> 2026-08-23 B3/C7 runtime Connectivity restart policy 预收口（全 profile 共用）：新增私有 `services/connectivity_restart_coordinator.[ch]`，将未来完整网络故障域重启的顺序固定为 `network-dependent quiesce（Gateway/poll/meeting、optional media、wake-restart、cellular recovery 等）→ Provisioning stop → physical network root stop → logical Connectivity initialize → physical root initialize → selected uplink start → Clock Sync start → Gateway rearm`。协调器仅接收单调时钟、`device_status_t`、remaining-timeout 与 root 注入的 value-only bridge；不包含 ESP-IDF/FreeRTOS/HTTP/Wi-Fi/portal/modem/board 句柄，业务/HAL 边界不扩张。Gateway 既有的 sleep PREPARE 已新增 terminal network-restart commit：严格清除 startup/poll/meeting 各自“ABORT 后重建旧 generation”的记录，不调用 ABORT，也不会重开任何 worker；新 generation 只能由 restart coordinator 的显式 rearm 阶段创建。每次事务使用一个绝对 deadline；每个阶段的失败都停止后续调用并进入 terminal FAILED，物理 root stop 已尝试后尤其禁止借用 System Sleep ABORT 恢复旧 Gateway worker，避免旧 generation 重新进入已退役网络栈。新增 host fault-injection regression 覆盖八个阶段逐一失败、顺序、deadline 耗尽与“完成/失败后不得复用 coordinator”；另有 Gateway commit regression 覆盖 commit 无 ABORT、下一个 sleep transaction 仍可独立 ABORT，并均接入 build-profile 与 HAL gate。**这不是已启用的 runtime restart：目前没有 production trigger，也尚未实现完整 dependent bridge、APSTA candidate confirm/portal post-save 无重启切换、Fangtang 4G 的专用 fault-domain bridge 或 COM3–COM6 HIL。**

> 2026-08-23 C7 SAFE_MODE 最小服务可运行首切片（全 profile 共用）：私有、value-only 的 `services/safe_mode_coordinator.[ch]` 仍以一个单调父 deadline 严格执行 `quiesce nonessential → initialize Clock/Feedback → initialize durable Alarm → publish diagnostic surface`；每阶段失败 terminal FAILED，不提供 rollback/ABORT，终止 coordinator 不可复用。现已在 composition root 接入**唯一已证明的 late boot 点**：`LOCAL_READY`（Persistence、Display/App UI、Input、Power、Wake Deadline、Sleep Schedule 已就绪；Wi-Fi/4G、SNTP、Gateway、portal 和普通交互尚未启动）。root 的 bridge 先同步关闭 ordinary Gateway/wake/deferred-setup admission 与 Input Binding 的普通 intent，再在同一预算内停掉装饰性 board/pet/cache、普通 audio/interaction、fall detection、configuration-reconcile 及任何已发布的 Connectivity worker；Gateway 的 prepare 必须紧接 terminal network-restart commit，绝不调用 ABORT 或重建旧 generation。它明确保留 Display、Power、Persistence、Wake Deadline、App Intent 和 Alarm 的 boot-lifetime依赖，并由 Input Binding 保证“闹钟响铃时的实体键/触控 dismiss”位于 SAFE_MODE gate 之前，其他触控/按键不会启动录音、会议、配网、Gateway 或配置写入。`CONFIG_MACLAW_SAFE_MODE_TEST_LOCAL_READY_FAILURE` 仅为编译期 HIL artifact（无 Hub/HTTP/console/runtime setter），`check-safe-mode-coordinator.ps1` + HAL gate 锁定顺序、deadline、terminal closure、接入点、禁止调用 `startup_stop_local_workers()` 与输入准入顺序。**不能把这视为全面 SAFE_MODE 或 runtime restart：早期失败（Persistence、Display/Bootstrap、Input、Power、Wake Deadline 等）仍必须 diagnostics-only degraded；force-setup 真失败是当前唯一 production 候选入口，尚未以 COM3–COM6 证明；bridge stage failure 现保持专用 terminal fail-closed，不再误入完整普通 rollback，四板熔断 HIL 仍未完成。**

## 1. 文档信息

- 状态：实施中
- 日期：2026-08-23
- 所属系统：MaClaw AgentOS
- 适用工程：`iot-agentos`
- 正式支持硬件：Bread Compact（COM4）、EchoEar-2ST（COM3）、Fangtang-4G（COM5）、Waveshare 1.75C（COM6）
- 关联文档：
  - `docs/design/esp32-unified-hal-development-plan-zh.md`（HAL 主计划，进度日志最新至 2026-08-20）
  - `docs/design/esp32-business-layer-feature-development-plan-zh.md`（业务层附录，Phase 0–10）
- 本文性质：基于上述两份计划与 `iot-agentos/main` 源码现状（2026-08-18 盘点）的剩余任务清单，不构成新的并列计划；任务编号、退出条件与门禁以两份主文档为准，冲突时以要求更严格者为准。

## 2. 现状摘要

| 层面 | 状态 |
| --- | --- |
| HAL/profile 拆分（platform_* 十域、boards/ 适配器、device_api、legacy seam） | 源码/构建级收口基本完成 |
| 业务层对 `board_port_*`、`CONFIG_MACLAW_BOARD_*` 的直接依赖 | 基本收敛（main.c/alarm_manager.c/app_ui.c 均 0 次 board_port_* 调用；板型宏仅剩身份校验类 2 处） |
| 业务服务拆分（command/reply/meeting/alarm/foreground/gateway/ambient/provisioning 等） | 主链路服务已抽出；`main.c` 约 6,930 非空行，仍持 SoftAP/SNTP/pet 下载/cellular 编排 |
| MCU 级休眠（Light/Deep Sleep） | 全代码库无 `esp_sleep_*` 调用，四板 `verified_sources=0`，全部 fail-closed |
| 实机/人工/端到端验收 | 大面积未完成；日志反复强调不得以构建/刷写成功替代 |

## 3. 任务分组与优先级

优先级定义：

- **P0**：发布阻断 / 其他任务的硬依赖，必须最先推进。
- **P1**：正式功能缺口或验收阻断，P0 之后立即跟进。
- **P2**：发布硬化、量产与清理，依赖前面各项完成。

### 3.1 A 组：业务服务拆分（P0，当前最大空白）

目标架构见业务附录 §6：app/、domain/、services/、presentation/、device/ 分层；业务代码禁止板型判断与 `board_port_*` 直调（依赖方向只能指向稳定 Device API）。

| 编号 | 任务 | 现状证据 | 验收要点 |
| --- | --- | --- | --- |
| A1 | 建立 app/、domain/、services/、presentation/ 目录分层与迁移规约 | 174 个文件全部平铺于 `main/` | 新代码按分层落位；CMake 按层组织；旧文件迁移有回退路径。**2026-08-20 已落地 `main/services/` 与 `main/presentation/`（command/reply/meeting/alarm/interaction/foreground_coordinator/gateway_dispatcher/gateway_transport/ambient/provisioning/audio_arbitration + input_binding/scene_model/scene_presenter）；app/、domain/ 待后续阶段建立** |
| A2 | Command Service：收敛语音 capture→upload→submit→processing→cancel 状态机 | 命令编排、cancel worker、`send_text_event` 在 `main.c:860` 一带 | 三硬件 voice/cancel trace 与 Bread 基线等价；main.c 不再持有 command 全局状态。**2026-08-18 首增量完成：状态/取消 worker/手势屏障/计时/上传策略已迁入 `main/services/command_service.c`（main.c 12,894→12,544 行，三 profile 构建+gate 通过）；语音会话编排 interaction_task 与 upload/submit 仍留 main.c，待 A3/A6/A8 边界就绪后收口，且真机 trace 等价验收未做** |
| A3 | Reply Service：correlation、multipart text/image/audio、迟到过滤、audio gate | `poll_reply` 在 `main.c:830`；长轮询与回复过滤在 main.c | 迟到回复被 correlation/generation 过滤；回复显式保留、首次单击关闭等行为不变。**2026-08-18 已完成源码提取：`main/services/reply_service.c`（main.c 12,544→12,309 行，三 profile 构建+gate 通过；reply id 容量常量已统一）；poll 轮询本体与 JSON 解析留 A8，呈现分页留 A7；真机 trace 等价验收未做** |
| A4 | Meeting Service：录音、finalize、分块上传、process、恢复收敛为单写者服务 | 118 处 meeting 逻辑在 main.c（`start_meeting_task:870`、分块上传 :7321、resume supervisor :838）；仅存在 `meeting_recovery_service.c` | 16 kHz/16-bit/mono、WAV header 修复、SHA256、服务端确认后删除契约不变；断电/断网不丢已确认数据。**2026-08-18 已完成源码提取：`main/services/meeting_service.c`（main.c 12,309→11,242 行，三 profile 构建+gate 通过）；传输本体留 A8/B1（host 回调），交互准入锁留 A6；真机全链路 trace 等价验收未做** |
| A5 | Alarm Service：拆分 `alarm_manager.c`（domain/repository/scheduler/tool adapter/feedback presenter） | `alarm_manager.c` 1031 行；已用 `wake_deadline_service`（arm :110-121）、`app_ui_set_alarm_visual`（:333/:342）、`device_audio_play_alarm_burst`（:335）；无前景恢复机制 | 行为等价迁移：16 条、48 bytes、60s×3、5 分钟 retry、ALM1/ALM2 读取、mutation+replay 同 commit 全部保持。**2026-08-18 已完成源码拆分：`main/services/alarm_service.c`（891 行）+ alarm_manager 降为 421 行薄 facade（gate 结构要求与全部调用方零改动，三 profile 构建+gate 通过）；feedback 经 app_ui/device_api 直调留 A6/A7 收口；真机与故障注入等价验收未做** |
| A6 | Foreground & Interruption Coordinator：foreground lease、priority、resume token、scene generation | 不存在；闹钟结束固定回 PET；`app_ui` 单一 surface | Alarm 覆盖任意页面后恢复正确的最新业务场景；迟到 release 不覆盖新 foreground。**2026-08-18 两增量完成：①`interaction_service` 落地（编排/准入锁收口，main.c 11,242→10,652 行）；②`foreground_coordinator` shadow 层落地（lease/priority/token 决策 + 九处 observer 埋点，零行为变更，stale release 已拒绝，三 profile 构建+gate 通过，app +1.2~1.4 KB）；已识别五类稳定 divergence 场景；authoritative 切换与真机 shadow 日志收集未做。2026-08-23 补齐 host regression：`tools/check-foreground-coordinator.ps1`（已接入 build-profile.cmd 门禁链）以生产源码+mock 编译，锁定单调 token、最高优先级/同优先级最新 token 仲裁、stale release 拒绝、restore-after 选择、DISPLAY_LOCK 借用规则、ambient restore 豁免 ALARM 与 shadow 默认 inert 共 53 项断言** |
| A7 | Scene Model / Scene Presenter / Input Binding（presentation 层） | 全部缺失 | 业务层发布语义 scene；圆屏/矩形/小屏 renderer 各自适配；raw input 只经 Input Binding 产出 intent。**2026-08-19 第一增量完成：`input_binding.c`。2026-08-20 第二–十一增量：业务 scene 发布、input-driven 控制、录音电平/PCM、pet asset、service_ready 与 DISPLAY_OFF 均经 presenter。`app_ui_init` / `app_ui_snapshot` 仍直调（composition root / coordinator observer）。coordinator authoritative 仍默认 false；真机手势链回归未做** |
| A8 | Gateway Dispatcher + Tool Registry：单 reader、ACK/cursor、tool/result 分发、durable outbox | 不存在；Gateway HTTP/长轮询在 main.c（直接 include `esp_http_client.h:17`） | mutation 前后、result POST 前后、ACK 前后断网均无重复副作用或消息丢失。**2026-08-19 第一增量完成：`gateway_dispatcher.c`。`gateway_transport.c` 已接管 request 车道/握手/voice pair/startup worker；2026-08-23 再接管 meeting chunk 的 Wi-Fi HTTP stream、active client cancel、可复用 keep-alive 及蜂窝 fallback，Meeting Service 仅调用值契约。ACK/重投真机回归未做** |
| A9 | Ambient Service：时间/天气/网络/pet 聚合为待机场景 | 天气/时钟/ambient 在 main.c（:828-834 一带） | 不覆盖高优先级 foreground；待机字段与现状一致。**2026-08-20 第一–三增量：weather/clock/network/scheduled/pet。2026-08-21 第四增量：Hub 天气/glyph JSON 解码迁入 `ambient_service`（`apply_hub_ambient`/`apply_hub_glyphs`，公共头无 cJSON）；PSRAM 栈仍延后 NVS。2026-08-23 D4 per-entry owned glyph record 已在 `display_service` 收口（见 D 组）。同日 SNTP singleton/retry/trusted epoch/System Sleep fence 已迁入 `services/clock_sync_service.[ch]`；Hub serverTime 与 SNTP 共用 value callback。宠物缓存 worker 已迁入 `services/pet_cache_service.[ch]`，该服务拥有 internal-stack Flash worker、admission、Storage registry、lease/cancel commit fence 与可逆 sleep participant；task 创建、Registry 登记、handle 发布的临界期由 `s_starting` 封闭，sleep/stop 不会将尚不可停止的 worker 误判为 quiesced；后台 descriptor 在接管帧所有权前严格校验 frame_count 与逐帧非空。cold-start 宠物 retry timer/callback/due flag 也已收口到 `services/startup_pet_retry_service.[ch]`：callback 仅发布 coalesced value fact，Gateway worker 负责消费；PREPARE 关闭并 drain callback，超时保持关闭至 ABORT。main.c 仅向其注入 storage/lease value probes。pet 下载、PSA SHA、HTTP cancel registry、renderer install 与 startup 下载编排仍在 main.c；真机宠物/glyph/时钟校正回归未做** |
| A10 | Provisioning Service：配置事务、portal 状态、配对状态 | 仅 `provisioning_failure_injection.c`；setup portal + captive DNS 在 main.c（直接 include `esp_http_server.h:18`） | 先行为等价迁移；配网安全化见 C4。**2026-08-20 第一增量完成：`provisioning_service.c` 收口 HTTP/DNS/scratch/lease/post-save restart 与表单事务；SoftAP/STA/DHCP/netif 经 host 留 main.c（B3）；`esp_http_server.h` 已离开 main.c。现为随机 WPA2 临时 AP，具 CSRF/TTL/限流/isolation 与 secret zeroization；TLS/身份绑定会话、runtime APSTA→STA transition 与真机配网回归仍属 C4/B3** |
| A11 | Audio Arbitration Service：capture/playback/wake lease、抢占和静音策略 | 仅 `audio_service.c` 薄封装 | Command/Meeting/Alarm 经 lease 获取音频；闹钟抢占按附录 §9 矩阵执行。**2026-08-20 第一增量（shadow）：Command/Meeting/Alarm wrapper。2026-08-21 第二增量：WAV/PCM wrapper。2026-08-21 第三增量：volume / stop token / wake-word 经 wrapper；`main.c`、input_binding、interaction、provisioning 改道。物理会话仍独占 BUSY；§9 WOULD_PREEMPT 仅日志；`set_authoritative` 默认 false；真机抢占回归未做** |
| A12 | main.c 收尾：删除已迁移的全局状态与编排，只留 boot composition root | main.c 12,894 行、约 376 个 static 函数 | main.c 不含 command/reply/meeting/alarm/portal/weather 全局状态；启动按依赖拓扑。**2026-08-20：main.c 约 6,930 非空行；weather/display-clock/portal HTTP+DNS 已迁出，Wi-Fi/scheduled/pet_state 发布已转 Ambient。2026-08-23 SNTP monitor/task registry/retry/System-Sleep owner 已抽为 `clock_sync_service`；本轮 pet-cache task/mutex/stop flag/registry callback 也已从 root 删除，转由 `pet_cache_service` 私有拥有。root→service 仅传 descriptor、lease 和取消 probe；service→root probe 均在 service lock 外调用，root 的 System Sleep ABORT 也在释放 `s_task_state_lock` 后才进入 service，避免锁序反转。pet cache worker 只有在 immutable Storage Registry identity 登记完成后才发布 handle，create→publish 中的 stop 失败闭合为 busy；这项保护也由 HAL gate 检查。main.c 仍保留 Wi-Fi/SoftAP/STA/DHCP/netif/httpd、宠物下载/PSA SHA/renderer startup transaction 与 cellular physical composition；下一步应拆分 startup pet download 事务前先定义 HTTP cancellation、media lease、display apply 和 retry timer 的完整所有权，不能按行数机械迁移** |

#### A9/A12 本轮增量：启动宠物 worker 生命周期（2026-08-23）

- `startup_pet_worker_service.[ch]` 已接管启动宠物下载 worker 的 FreeRTOS 生命周期、PSRAM stack、start gate、completion、Task Registry identity、create→publish fence、retirement 与 System Sleep PREPARE/ABORT restart fence；`main.c` 不再持有该 worker 的 task/token/retiring 状态。
- root 通过纯值 host callback 继续拥有 HTTP cancel 和完整下载事务（PSA SHA、media lease、renderer install），避免把物理 client、显示或板型差异带入共享服务。退出必须先注销旧 immutable Registry identity；注销失败成为 terminal fail-closed stop result，ABORT 不会创建可能被旧 identity 误停的新 generation。
- 同轮修复 `pet_cache_service` 的对应 retirement 漏洞：Flash 工作结束不等于 Storage Registry identity 已退出；注销失败将保持 cache admission closed，stop observer 返回实际失败，System Sleep ABORT 不会重新开放 Flash 工作。
- 普通冷启动 rollback 在同一 parent deadline 中终止 worker 后显式 terminal-stop retry timer，再停止 cache writer；terminal stop 在已超时的 join retry 时仍会继续观察 retiring generation，不能把 closed admission 误报为完成。System Sleep PREPARE 保持 retry/worker 的可逆 park，只有公共 ABORT 可以恢复原有 pending work。
- 本轮已通过 HAL、System Sleep failure-closure、Gateway capability projection 与 diff 静态检查；四个正式 profile 标准 wrapper 完整 configure/build/link 均通过：Bread Compact `0x352050`（约余 8%）、EchoEar-2ST `0x33e9b0`（约余 11%）、Fangtang-4G `0x34bc00`（约余 10%）、Waveshare 1.75C `0x499560`（约余 8%）。COM3–COM6 的慢网下载、HTTP cancel、retry 与 sleep rollback HIL 仍需后续验收。

#### A6/A10 本轮生命周期补强：Provisioning 与交互 worker（2026-08-23）

- `provisioning_service` 的 captive DNS、portal TTL、post-save restart 三个已登记 CONNECTIVITY worker 统一为“先注销 immutable Registry identity，再发布 completion”的退役顺序。若登记册锁在有限预算内无法取得，保存实际 exit status、保持对应 admission 关闭，并把该失败视为 portal 仍有 live resource；后续 portal start 与 System Sleep PREPARE 均 fail-closed，不能用旧 completion token 启动新 generation。
- `interaction_service` 的前景语音 worker 同样保留 retiring/exit-status/failure 状态。Registry 退役失败时，不释放跨任务 interaction admission token，也不安排 offline wake restart；下一次触屏/实体键只会被拒绝，避免新录音 worker 被旧 Interaction owner entry 误停止。该项不改变“物理唤屏只亮屏、不自动录音”的既有 Input→业务语义。
- 新增 HAL boundary 静态门禁，锁定上述字段、注销结果传播与 admission fence。当前仅完成 Bread Compact 对应对象编译；完整四 profile 链接与 COM3–COM6 HTTP cancel/portal timeout/语音 stop HIL 仍待完成，不能以本次源码验证替代实机验收。

#### A4 本轮生命周期补强：Meeting 三 worker（2026-08-23）

- `meeting_service` 的 AUDIO meeting upload worker，以及 CONNECTIVITY resume supervisor、capability-refresh worker，均按统一的 retiring → bounded Registry unregister → terminal exit status/clear handle → completion 顺序收口。之前任一 worker 都可能先清 handle/发 semaphore，令 owner sweep 将 Registry contention 当作成功退出。
- 三个 worker 的注销失败现在都保持 terminal fail-closed：新会议录音/续传不创建 replacement，resume supervisor 与 capability refresh 的 System Sleep ABORT 不会复活旧 generation。已保留的 PCM、WAV recovery metadata 与 Gateway capability lease 语义不变；该改动只约束 worker 生命周期，不改变录音/上传业务流程或具体板型 I2S、存储、屏幕实现。
- `check-hal-boundaries.ps1` 已补充 Meeting retirement fence。已完成 Bread `meeting_service.c` 对象编译和三项静态门禁；四 profile 完整链接及 COM3–COM6 录音、断网续传、System Sleep rollback、Registry contention HIL 仍是开放验收项。

### 3.2 B 组：Connectivity 与 ML307 业务收口（P0/P1）

| 编号 | 任务 | 现状证据 | 验收要点 |
| --- | --- | --- | --- |
| B1 | main.c 内 cellular/ML307 业务特判收进 connectivity adapter / 统一 transport 语义 | **2026-08-23 本增量已收口蜂窝恢复 coordinator**：新增 `services/cellular_recovery_service.[ch]` 与纯值 `cellular_recovery_policy.[ch]`；`main.c` 只安装网络状态/Gateway 的 value callback，并调用 service 的 initial establish / sleep participant，不再持有 recovery task、FreeRTOS gate、backoff、registry 或 ML307 重试状态。Wi-Fi GOT_IP 也只通知该服务，由它在同一 provisioning/startup/sleep guard 下决定 Gateway rearm。物理 ML307 仍只经 `Device API → Connectivity Service → Platform Connectivity → Fangtang adapter`。SNTP、Wi-Fi callback 的物理 owner、会议上传和 HTTP 取消 owner 仍在 main.c/B2/B3 范围 | 业务层不出现蜂窝/Wi-Fi 传输分支；传输差异只由 Connectivity adapter 表达。已通过 host backoff regression、HAL/System-Sleep gate，并编译 Fangtang 新 service 对象；真实 SIM 的 recovery、Gateway rearm、sleep rollback HIL 仍待 B4/E2 |
| B2 | Gateway worker 统一 cancel/join/resume 生命周期（Phase 7C） | **2026-08-23 本增量已完成共享 Gateway lifecycle coordinator**：新增 `services/gateway_lifecycle_service.[ch]`，它以单一绝对 deadline 编排 active HTTP cancel → meeting resume supervisor / resume-only worker / capability refresh → gateway startup → poll 的 prepare；ABORT 严格逆序且仅由各服务恢复 PREPARE 前已存在的 generation。`main.c` 只保留 ESP HTTP handle/mutex registry 的 value callback，以及 Wi-Fi callback、SNTP、pet/wake/deferred-setup/cellular-recovery 等尚未抽出的 root participant。蜂窝 foreground/meeting cancel 仍经 Device API，未泄漏 ML307。 | 长轮询可在 sleep PREPARE 预算内取消并恢复；跨 boot 消息 durability 保持。源码门禁通过；实际 HTTP perform/cancel/join timeout、Wi-Fi/4G rollback、durable cursor 与 COM3–COM6 HIL 仍未完成，不能标记真实 Light/Deep Sleep 已可用。 |
| B3 | Wi-Fi/portal/SNTP/httpd owner 从 main.c 迁入 Connectivity Service | Connectivity 已拥有 selected-uplink、Wi-Fi attempt generation、logical request admission、cellular transaction、4G recovery coordinator，以及 **default-loop Wi-Fi/IP callback 的准入、in-flight drain 与可逆 System Sleep 围栏**；私有 network owners 已独占 ESP-NETIF/default-loop singleton、SoftAP 与 STA ESP-NETIF 创建/释放、AP DHCP/DNS advertisement、NAPT isolation，及 Wi-Fi driver init/deinit、三组 application event-handler instance、radio runtime、EAP、scan SDK record/auth-mode mapping 与 portal rollback snapshot。另有 private value-only restart coordinator 固定「所有 network-dependent workers → portal → physical root → logical/physical reinit → uplink → SNTP → Gateway rearm」的 future transaction，八阶段 host fault-injection 已覆盖；`main.c` 仍协调业务 Wi-Fi 凭据、callback observation、SNTP host injection，且 coordinator 尚无 runtime trigger | 后续必须将 complete dependent bridge、APSTA candidate confirm/post-save transition、Wi-Fi 与 4G fault-domain 分流、诊断/SAFE_MODE 与 concrete composition bridges 完整接入，再以 COM3–COM6 HIL 证明；不可将物理句柄泄漏进 Device API，也不可将 policy skeleton 误记为 runtime restart 已可用 |
| B4 | Fangtang 真实 4G 全链路验收 | 当前二进制从未切换真实 4G（08-09）；蜂窝长请求仅靠 60 秒超时，无强制 abort | 真实 SIM 下 command、voice pairing、chunk 上传 HIL；并发长请求+quiesce 故障注入 |
| B5 | 能力投影四层协商（effective/accepted/negotiated/operational）与运行时 health 滞回（Phase 7C） | **2026-08-23 第一–九源码增量完成**：新增纯值 `gateway_capability_projection.[ch]`，明确区分本地 effective、Hub 明确 accepted、二者交集 negotiated 与按健康状态开放的 operational；初次有效握手与后续 structurally-valid authenticated poll 分别提供控制面成功观测，连续 2 次才开放，健康后 1–2 次失败为 DEGRADED 并保留上次已协商表面，连续 3 次 handshake failure 才撤销 operational。缺失、畸形或与本地不兼容的 `capabilitiesAccepted` 立即撤销 acceptance；同一 accepted contract 的重复握手不重置滞回。投影含单调 generation，effective/accepted 收缩或撤销会推进 generation。新增 ABI 化纯值 `gateway_capability_lease_t`：只能由当前 operational surface 捕获，generation 变化或 required bit 不再 operational 即失效；连续失败撤销 operational 也推进 generation，旧 worker 即使稍后健康恢复亦不可复活。Meeting foreground 与 retained-resume worker 均在创建前捕获 `MEETING_RECORDER` lease，并在录音循环、chunk hash 后、每个 Gateway transaction 紧邻调用前复验；撤销后停止远端 create/chunk/complete/process，保留本地 PCM/recovery metadata，前台提示明确为“网关能力变更后可续传”而非网络故障。宠物资源的 runtime 和 cold-start download、首帧预览、full-pack install 与后台 SPIFFS cache mirror 均已绑定 `PET_ASSET` lease；撤销时不会开始后续帧请求、安装或 cache metadata commit，缓存写以 lease cancellation 回滚为无有效 commit。下行 Dispatcher 已将 text/audio/image、ambient/glyph、pet state/profile/asset 及 `hardware_config` 的 volume/brightness/screenSleepSeconds 分别接入对应 operational 位；其中 reply audio 在本消息 scope 捕获 `OUTPUT_AUDIO` lease，并在 inline decode/play、URL download 后 play、最终 ACK 前复验，撤销后丢弃已下载缓冲、不再触碰 codec，并 failed ACK 消费旧 generation 消息以避免有序 cursor 永久阻塞。**本轮将 hardware_config 的 persistence → reconcile → ACK 继续收口到同一 generation：Dispatcher 传入精确 capability lease，composition root 在每次 durable queue 前、queue 返回后的 reconcile 前和 ACK 前重验；Configuration 只接收 ABI 化 generic authorization，绝不依赖 Gateway/HAL。其 consumer 边界和 retained retry 都重验/复制该 authorization，撤销后不再启动后续 Audio/Display/idle 副作用或留下旧 Hub generation retry。**Hub 也只在 client 已声明并接受 meetingRecorder 时广告 meeting endpoint；Gateway request descriptor 改从同一 local effective set 生成，减少“宣告集合”和投影分叉。**新增跨 Hub→ESP 的 wire-contract 回归：Hub 对完整 ESP surface 保留规范 `capabilitiesAccepted` 字段名、剔除未知 modality/feature 并将 `petAssetMaxFrames` 夹到 8；legacy client 也必须返回 explicit text-only accepted contract。ESP parser 现在强制 `output.modalities` 为字符串数组且 `output` 为对象，对 input 的存在但畸形形式同样拒绝，因而 malformed/partial response 不会被当作一个有效零授权。** | 协议与真实能力一致；能力收缩后所有保留 handle/worker 必须确定性失效。Meeting、pet asset/cache、reply audio 与 hardware-config/configuration reconcile 已 generation-bound。`meeting capability-refresh` 是控制面恢复 worker：它不能持有 `MEETING_RECORDER` 旧 lease 来 gate 握手，否则 capability 恢复会被旧 generation 自锁；握手完成后发起的 `meeting_start` 自行捕获新 lease，现有 lifecycle fence 仍负责其停启。当前不主动中断已阻塞的 HTTP transaction，撤销会在其下一安全边界生效，立即取消须另定义受控 cancellation contract。Tool registry 已审计：现有 alarm、sleep schedule、fall detection、battery policy、update reminder 均没有可精确映射到 Gateway capability bit 的下行硬件语义，不能按消息类型粗暴拒绝；新增可由 Hub 能力协商控制的 tool 必须先在 descriptor/registry 显式声明 required capability，再在 Dispatcher 精确 gate。负 health 目前仅来自 handshake failure，不能把任一业务请求失败等同 Hub 全局失败；**Hub schema/legacy 的源码回归已补齐，仍缺与旧版已部署 Hub 的兼容演练、Hub withdrawal/recovery 以及 COM3–COM6 HIL。** |

### 3.3 C 组：电源、休眠与安全（P1，发布硬门禁）

| 编号 | 任务 | 现状证据 | 验收要点 |
| --- | --- | --- | --- |
| C1 | Light/Deep Sleep COMMIT 链路（Phase 7B 后半） | 无 `esp_sleep_*` 调用；四板 Light/Deep `verified_sources=0`；六 PREPARE participant 全部 fail-closed（08-16） | DISPLAY_OFF→LIGHT/DEEP_SLEEP 状态机；PREPARE→COMMIT→resume 事务；实测功耗 |
| C2 | 依赖链补全：Gateway worker coordinator、profile-private display DMA fence、最早 alarm→RTC wake arm/漂移补偿、每板电气 wake HIL | 08-16 明确全部待做 | 三硬件 alarm timer/实体键/已声明触屏可靠唤醒；存在有效闹钟时不进入不可唤醒睡眠 |
| C3 | 低电量保护：ADC 校准、滞回、低压 checkpoint、charger wake | 08-09 明确"不宣传为 brownout safety"；电量百分比/充电极性三态未校准 | 低电/棕断风险下限制背光与铃声峰值，优先保证存储一致性 |
| C4 | Provisioning 安全化：认证加密配网会话、TTL/限流、接口隔离、secret zeroization | **源码/host contract 已有随机 WPA2 临时 AP、CSRF、TTL、peer 限流、HTTP/body deadline、AP isolation/NAPT 禁用、候选配置回滚及 portal/request/AP config 的 zeroization；保存后仍靠整机 reset。**HTTP 仍只受 WPA2 链路保护，尚无 TLS 或设备身份绑定会话；无线/isolation/HIL 未做 | 量产配网不通过开放 AP 明文提交 secret；再完成身份绑定/加密会话、可信时间与四设备 HIL 后，方可关闭 C4 |
| C5 | Security/Credential Service、TLS 可信时间引导、EAP 私有 CA、Entropy Service | 无完成记录 | credential 生命周期与设备绑定可审计；随机 readiness barrier |
| C6 | NVS/Storage 迁移 journal、断电注入恢复、受控恢复出厂 | 08-14：缺键、NVS 满/损坏、断电四机 HIL 待做；显式受认证恢复与版本化迁移未实现（08-09） | 迁移 stage/validate/commit，失败保留旧 generation；恢复出厂擦除 alarm/replay/日志索引并可审计 |
| C7 | 完整 runtime restart / board deinit / fault-domain restart / SAFE_MODE 实机验证 | 已有 private eight-stage restart policy；SAFE_MODE coordinator 的 host regression 覆盖阶段失败/单 deadline/terminal closure，且已在 `LOCAL_READY` 的证明性最小依赖边界接入 root bridge：保留 Persistence/Wake Deadline/Display semantic/Power/App Intent/Alarm，隔离普通 worker，Input Binding 只留下 alarm dismiss。新增 compile-time local-ready failure artifact 与 HAL gate。**COM6 Waveshare 已完成专用 local-ready FI 的写入、hash verify 与串口 HIL：local-ready 后进入 SAFE_MODE、停止 pet/command/fall worker、发布本地 clock/alarm diagnostics，未见 crash/reset，并已回刷正式 artifact；COM5 Fangtang 专用 FI 也已以 size-optimized 配置完整重建（`0x355840`，余 `0x4a7c0`/8.0%），可作为未来受控 HIL 的非发布候选，但尚未刷写，不能外推为 HIL。**board port 仍保持 boot-lifetime、二次 init 拒绝；runtime restart 仍未有 production trigger，SAFE_MODE 仅有一处 production 候选（force-setup read/commit failure）且未 HIL | 完成 COM6 触控普通 intent 拒绝和响铃闹钟 dismiss HIL。COM3/COM4/COM5 重新在线后，分别使用对应 FI profile 做同等 cold-boot 证明；再完成 force-setup fault、bridge stage fault、Wi-Fi/4G 独立 fault-domain 与 runtime restart HIL。不得将早期失败或 bridge 失败误报为 SAFE_MODE |

### 3.4 D 组：HAL 收尾（P1/P2）

| 编号 | 任务 | 现状证据 | 验收要点 |
| --- | --- | --- | --- |
| D1 | renderer source owner 与共享业务边界收口 | **2026-08-23 已完成共享 facade 删除**：Display/Input/Lifecycle 均经 `Platform → legacy_*` 窄 seam；`main/` 无 `board_port_*` 生产符号或公开 header。圆屏物理文件仍名为 `board_port.c`，但仅是 private round renderer source owner | 新增硬件仅实现 profile/private adapter；后续可在独立、低风险切片中物理改名/继续拆分 renderer，且须四 profile 构建与 HIL |
| D2 | Platform Audio 拆分刷机验证 | 08-15 完成构建级拆分，未刷入 COM3–COM6 | capture/playback/volume/wake 听感 + 串口 HIL 四板通过 |
| D3 | Phase 9：删除 `board_port_*` facade 与 legacy seam，发布硬化 | **2026-08-23 已完成源码级收口**：Storage、Bootstrap/Input、Connectivity、Display、Motion、Audio/Power 均为显式 `legacy_*` 私有 seam；无宏映射、无共享 `board_port` facade | 四正式 profile 完整链接及 COM3–COM6 功能/HIL 回归后，方可作为发布硬化证据；不把历史文件名或静态检查当作实机验收 |
| D4 | 动态 glyph per-entry owned record | **2026-08-23 已完成源码收口**：`display_service_cache_glyph()` 现以 per-entry 堆 owned copy（`heap_caps_malloc(72, INTERNAL|8BIT)`，失败回退 libc）跨过 Service→Platform 边界，同步 submit 返回后即释放；Platform 仍在返回前消费/复制进板级 LRU。公共头契约由 Borrowed 改为 Per-entry owned；HAL boundary gate 与全部 extraction gates 通过。异步队列的 completion/release policy 与真机 glyph 回归仍开放 | payload 所有权与 snapshot revision 语义完整 |

### 3.5 E 组：实机与人工验收（P1，多数为已收口代码的验收回填）

| 编号 | 任务 | 现状证据 | 验收要点 |
| --- | --- | --- | --- |
| E1 | EchoEar（COM3）在线闭环 | Hub token 被拒进入 pairing recovery，多窗口无 SERVICE_STATUS（08-12 起）；GCC 15.2 对 `esp_lcd_panel_rgb.c` 内部编译错误致 animation-deadline FI 未实机验证 | 有效 Hub token 下 BOOT+SERVICE ready、在线动画、唤醒 HIL；不外推 COM6 结果 |
| E2 | 四机人工交互验收矩阵：实体键/触摸首手势、DISPLAY_OFF 首手势只亮屏、录音/播放听感、闹钟全流程、长时动画目视 | 每条相关日志以"待人工 HIL"结尾，无回填 | ACTIVE→DISPLAY_OFF→首手势四板通过；COM5 实体键 wake-only 行为验收 |
| E3 | EchoEar 音质人耳验收（白噪/人声清晰度） | 08-09 明确 ES8311 修复不等同音质验收 | 人耳确认通过并留证 |
| E4 | Waveshare：亮度 40%/100% 相机/人眼 A/B、触控、IMU/跌倒、PMIC 验收 | 08-09 列明均未验收；IMU 采样不等同跌倒检测 | 各项 HIL 留证；跌倒检测真实触发语义 |
| E5 | Fangtang 动画 20 fps/50 ms 目标与 8 行 DMA 候选资格 | 08-14 仅验证 8–12 fps；禁止未经电气 HIL 提升 SPI 时钟 | 电气 HIL 通过后方可提升 release 默认 |
| E6 | 视觉构图验收：远程宠物、天气弧、GUI 下发素材 | 08-09 列明待相机/人眼 HIL | 宠物/天气不重叠等构图项留证 |

### 3.6 F 组：更新链路与发布工程（P1/P2）

| 编号 | 任务 | 现状证据 | 验收要点 |
| --- | --- | --- | --- |
| F1 | GitHub Release → Hub Update Catalog → 设备提醒 → ClawMate Maker 端到端演练 | 组件已建（Go 测试覆盖），端到端未演练（08-09） | 三硬件只接收相同语义可信 metadata 并显示等价提醒；设备端无 firmware download/`esp_ota_*`/partition 写路径（静态+运行测试证明调用次数为零） |
| F2 | 三板真实刷写/断电 HIL、RECOVERY_REQUIRED 恢复演练、上一稳定 `.clawfw` 回退 | 08-09 明确未完成 | 断电故障注入；schema 不可逆迁移时阻止不安全降级；无自动回滚宣传 |
| F3 | 四设备 catalog metadata dismiss/defer/墙钟不可信/Persistence 失败 HIL | 08-15 列明待做 | 提醒状态合并、限频写 NVS；critical 有最大 defer/dismiss TTL |
| F4 | 更新后设备版本提醒三硬件实机验收 | 08-09 列明未完成 | 三硬件等价提醒实机留证 |

### 3.7 G 组：发布门禁与基线（P2，跨阶段收口）

| 编号 | 任务 | 现状证据 | 验收要点 |
| --- | --- | --- | --- |
| G1 | Phase 0 基线冻结收尾：带版本 `baseline_manifest`、三硬件能力实测表、SLO 基线、48 项盘点 | 日志从未声称冻结齐全 | 后续每次迁移可证明业务等价 |
| G2 | Phase 8：量产自检模式、第四 Fake/Reference profile 演练、ABI 兼容测试 | **2026-08-23 已完成源码/构建级的第四 profile 演练**：`reference-fake` 有独立 board profile、Kconfig/CMake/defaults/wrapper 接入；C11 host matrix 与 HAL gate 受检；完整链接通过（app `0x351c60`，最小 app 分区余 `0x4e3a0` / 8%）。它只复用 Bread-compatible 物理 adapter，且 wrapper 拒绝 flash/monitor/erase，不计入正式 profile 或发布证据。量产自检、真实异构第四板、shared-business HIL/等价 trace 仍未完成 | 新增 profile 只能实现 profile/HAL/renderer/input binding/必要 platform port；无需修改共享 services/presentation/业务状态机。正式 release 仍须以四款正式硬件的独立构建、HIL 与 feature baseline 证明 |
| G3 | 三硬件功能等价发布门禁：同一 feature_id 测试集三硬件验证（§3.1.1） | 从未声称 | 同一 capability/tool 集合、同一状态机/错误语义 |
| G4 | 长稳/压力：栈水位/堆/DMA 稳定性、断电恢复、重试风暴、DFS 全频点、资源压力有序降载 | 无完成记录 | 连续运行指标达标并留证 |
| G5 | 会议录音 ppm/gap/断电恢复基线 | 无记录 | 基线测量并纳入发布门禁 |
| G6 | HIL 证据签名/可复测体系、SBOM/clean-build 可复现报告、requirement→evidence 追踪矩阵、设备 ID 克隆防护 | 无完成记录 | 证据包完整可审计 |
| G7 | Fangtang 本地音量入口（单键无独立音量键的替代入口） | **源码已完成，待 COM5 HIL**：`fangtang_input_adapter.h` 的 1.2s/1.8s 按住转为标准 `DEVICE_INPUT_VOLUME_UP/DOWN`，经 `input_binding → audio_arbitration_adjust_output_volume`，与 Bread 实体音量键及 GUI 远端控制汇聚 | 不依赖 Hub 在线的本地音量入口，可从 Scene/设置发现，与远端入口汇聚为同一 intent；需 COM5 人工验证长按阈值、重复触发、与 2.5s 配置长按不冲突 |

## 4. 建议推进顺序

1. **A 组业务服务拆分**（A11 §9 抢占执行（需改行为，等 shadow/HIL）→ A12；A9 的 startup pet download 可与其它 value service 并行）：A7 业务 scene 路径已收口。A11 业务音频入口均经 shadow lease wrapper。A9 待机字段、Hub 天气/glyph JSON、SNTP monitor 与 pet cache coordinator 已落地；仍待抽取的是 startup/runtime pet 下载、HTTP cancel/media lease、PSA SHA、renderer install 与 retry 编排。A10 radio owner 与 C4 安全化仍开放，不与本源码增量并行。B1/B3 可与 A 组并行。
2. **C1/C2 休眠链路**：发布硬门禁，依赖 B2（Gateway worker 生命周期）。
3. **E1/E4/E2 验收阻断项**：EchoEar 在线闭环、Fangtang 真实 4G（B4）、四机人工矩阵——建议尽早排期，避免成为最后的长尾。
4. **D 组 HAL 收尾**与 A 组末期同步，D3 facade 删除放在业务拆分完成后。
5. **F 组端到端演练**在 C 组安全项与 D 组收口后串联；G 组随各阶段验收滚动关闭。

## 5. 执行约束（摘自两份主文档，必须遵守）

- 先行为等价提取，再增强语义；每个阶段可在三（四）硬件上独立回归和回退。
- 不得以构建/刷写成功替代实机交互、声学、视觉与功耗验收；所有"不宣称"事项在验收回填前保持风险敞口记录。
- 新增硬件只能实现 profile、HAL、Renderer、Input Binding 和必要平台 port，不得复制或按板型改写业务流程。
- 固定 16 MiB 产品不做设备端 OTA；更新链路固定为 GitHub Release → Hub Update Catalog → 设备提醒 → 官方刷机工具 USB 更新。
- 附录 Phase 与主计划 Phase 映射固定（附录 0–7 → 主计划 0–7A-a/7C，附录 8 → 7B，附录 9 → 7A-b，附录 10 → 8–9）；门禁冲突时以要求更严格者为准，同一 requirement/evidence registry 关闭。

## 6. 2026-08-23 生命周期闭锁补强：根级 Storage persistence worker

- 审计发现，`main.c` 中尚未迁出的内部栈 `output_volume_persist_task` 与已收口的宠物缓存 worker 存在同类风险：task 完成 Flash/NVS 工作后若 `task_registry_unregister_with_timeout()` 失败，旧实现先清 task handle、再发 completion，后续 stop observer 可能误报成功；System Sleep ABORT 也可能重新开放已遗留 immutable Storage Registry identity 的写入域。
- 现 worker 先进入 retiring，再注销 Registry identity；它把注销结果保存为 terminal exit status 后才清 handle 和发布 completion。注销失败会永久关闭本 boot 的 persistence admission，stop 路径返回真实错误，System Sleep ABORT 不再重开该域。该改动不把 Queue、FreeRTOS、NVS 或板型细节泄漏给 Power/HAL 公共接口。
- 静态门禁已新增对 root Storage retirement fence 的检查；HAL boundary、System Sleep failure-closure、Gateway capability projection、`main.c` Bread compile-command syntax check、`git diff --check` 均通过。Bread Compact 完整 Ninja link 通过，app 为 `0x352180`，最小 app 分区余 `0x4de80`（8%）。其他正式 profile 尚未针对本次 root-worker 小改重新链接；COM3–COM6 HIL 仍未执行。

## 7. 2026-08-23 生命周期闭锁补强：root wake / deferred-setup coordinators

- 审计 `main.c` 发现 offline wake restart 与 deferred setup portal 两个 root-private coordinator 也有相同的生命周期缺口：worker 在注销 immutable Task Registry identity 前先清 task handle、发 stopped completion；因此 stop observer 可把 `unregister` 超时/失败误报为成功，而 System Sleep ABORT 在旧 identity 仍可见时可能安排 replacement generation。
- 两个 worker 现在统一遵循 **retiring → unregister → 保存 exit status/清 task handle → completion** 顺序。注销失败保持 terminal closed：后续 stop 返回实际失败、正常调度与 ABORT 均不得重开 admission；只有旧 identity 成功退出后，才允许 ABORT 恢复 PREPARE 前已存在的 retry/portal coordinator。该项仍属于 composition-root private lifecycle，不将 FreeRTOS、Registry 或 portal 物理实现泄漏进共享服务/HAL。
- `check-system-sleep-failure-closure.ps1` 新增两类 coordinator 的 result、failed-retirement 与 ABORT 审计；HAL boundary gate 同步调整为要求“未失败 retirement”才可 admission。HAL/System Sleep/Gateway 静态门禁、Bread `main.c` syntax check、`git diff --check` 与完整 Ninja link 均通过；Bread app 为 `0x352300`，最小 app 分区余 `0x4dd00`（8%）。本批仍未重链 EchoEar/Fangtang/Waveshare，亦未执行 COM3–COM6 HIL。

## 8. 2026-08-23 生命周期闭锁补强：Command cancel service worker

- 审计 `command_service` 的常驻 cancel worker 发现它也会在 Registry 注销前清 handle、发 completion；在启动 rollback 或 Registry owner sweep 与退出竞争时，失败的注销可能被后续 lifecycle 调用误认作已停止。
- worker 现在先退休 Registry identity，随后才发布 terminal exit result、清 handle 和 completion。注销失败后 service 不会重新创建第二个 permanent cancel worker，直至进程重启；公共 `command_service.h` 保持 value-only，不泄漏 Registry/RTOS 细节。System Sleep 继续只关闭取消请求准入而不停止已经存在的 permanent worker，既有取消协议语义不变。
- HAL/System Sleep/Gateway 门禁、`command_service.c` Bread syntax check、`git diff --check` 与完整 Ninja link 均通过；Bread app 为 `0x3523e0`，最小 app 分区余 `0x4dc20`（8%）。本轮未重链 EchoEar/Fangtang/Waveshare，COM3–COM6 HIL 仍开放。

## 10. 2026-08-23 Gateway / Cellular coordinator 生命周期收口

- Gateway startup、Gateway poll 与 Cellular Recovery 已完成 Registry retirement fail-closed 源码收口；poll 创建窗口新增 start gate/starting fence，禁止注册 identity 前执行或并发创建两代任务。
- 剩余验收保持同一 HAL/Connectivity 生命周期，而非在板型复制业务逻辑：COM3 EchoEar-2ST、COM4 Bread Compact、COM5 Fangtang-4G、COM6 Waveshare 分别覆盖正常启动、网络中断恢复、Gateway long-poll、System Sleep PREPARE/ABORT rollback。COM5 额外覆盖 4G 重连；其余设备覆盖 Wi-Fi/APSTA 恢复。
- 本项仍需通过静态门禁、完整 Bread 链接、其它 profile 重链和上述 HIL 后才能关闭；对象级或单板构建成功不得外推为四板验收成功。

## 11. 2026-08-23 Shared Persistence worker 生命周期收口

- `persistence_service` 的通用 internal-stack NVS worker 现具有与其它 Storage worker 相同的 immutable Registry lifetime：start gate 阻止注册前消费路由 Flash 工作；退出先注销、记录实际结果并清 handle，最后才发布 stopped completion。
- Registry 注销失败会关闭本 boot 的 Persistence admission。普通 deinit 返回真实 terminal status；System Sleep PREPARE/ABORT 均不会把 failed 或 retiring generation 当作可恢复的 NVS worker。该限制只在共享 Storage service 内实现，不向业务、HAL 公共头或 profile 扩散 FreeRTOS/NVS/板型细节。
- 已通过 HAL、System Sleep failure-closure、Gateway capability projection、diff 门禁和 Bread 完整链接（`0x3530c0`，最小分区余约 8%）。仍需四板 Flash/NVS、sleep rollback 与 Registry contention HIL；其它三 profile 未针对该小批重新链接。

## 12. 2026-08-23 Firmware Identity diagnostic worker 生命周期收口

- USB Serial/JTAG Firmware Identity 查询 worker 的 Registry identity 现在由自然退出先行注销，再清 task handle/发布 stopped completion；Registry 失败保存 terminal status 并关闭本 boot 的 diagnostic worker restart admission。
- 正常 System Sleep 仍只 park/ABORT 唤醒同一个已注册 worker；但 PREPARE 不接受 starting、retiring 或 failed-retirement generation。公共 `firmware_identity.h` 仍为 value-only，不泄漏 USB、RTOS、Registry 或硬件 profile。
- HAL、System Sleep failure-closure、Gateway capability projection、diff 门禁及 Bread 完整链接已通过（`0x353140`，最小分区余约 8%）。其它三 profile 未重链，COM3–COM6 USB diagnostic、park/ABORT 与 Registry contention HIL 仍开放。

## 13. 2026-08-23 Display Service logical worker 生命周期收口

- 共享 Display semantic queue 的 static worker 现在在精确 BOARD Registry identity 登记后才运行；STOP 时先注销、记录 exit status/清 handle，最后才发布 completion。Registry 失败将关闭该 boot 的 logical Display restart admission，而不会触碰 profile-private panel/DMA/scan-out 生命周期。
- System Sleep 的语义不扩大：PREPARE 仍只关闭 scene submission、drain 已接纳请求并请求现有 Platform Display scan-out fence；但不会将 retiring/failed Display generation 误当作可安全 park 的 task。公共 Display/HAL 接口保持 value-only。
- 已通过 HAL、System Sleep failure-closure、Gateway capability projection、diff 门禁和 Bread 完整链接（`0x353350`，最小分区余约 8%）。其它三 profile 未重链，COM3–COM6 场景刷新、DISPLAY_OFF、sleep rollback 与 Registry contention HIL 仍待完成。

## 14. 2026-08-23 D3 legacy Storage seam 窄化

- 审计确认共享业务、Device API、Platform facade 均不再直接调用 `board_port_*`；剩余 facade 只存在于两套旧 renderer source owner 内。先收口无行为风险的 Storage 小切片：`legacy_storage_admission.h` 不再以宏把 `board_port_allows_optional_flash_work` 重命名为私有接口，圆屏与紧凑屏 renderer 直接实现 `legacy_storage_admission_allows_optional_flash_work()`。
- 因而 Platform Storage 的“是否允许可重建、非关键 Flash 工作”只链接到明确的私有 Storage seam，不再在头文件层反向依赖广义 board-port 命名。这个改动不改变存储策略、SPIFFS、宠物缓存 admission 或任何屏幕/音频/输入行为；后续 Display、Input/Bootstrap、Connectivity 三条 legacy bridge 仍须按 source-owner 切片迁移，不能机械删除宏。
- 已通过 HAL、System Sleep failure-closure、Gateway capability projection、`git diff --check`；Bread Compact 完整 Ninja link 成功，app `0x353350`（最小 app 分区余约 8%）。本项未重链 EchoEar/Fangtang/Waveshare，未刷写设备；COM3–COM6 的存储/显示/输入 HIL 仍待完成。

## 15. 2026-08-23 D1/D3 legacy Input/Bootstrap seam 窄化

- `legacy_bootstrap_input.h` 不再通过宏把三个 `board_port_*` 名称改写为私有接口。圆屏与紧凑屏 renderer 直接实现 `legacy_bootstrap_input_initialize()`、`legacy_bootstrap_input_start_scanner()` 和 `legacy_bootstrap_input_stop_scanner()`；Platform Bootstrap / Platform Input 继续只看这组三个窄生命周期操作。
- 保留既有边界：renderer 仍拥有 boot-lifetime panel/audio/peripheral transaction；Input Service 仍拥有版本化 event queue；Platform bridge 只转换值状态。改动没有扩大 runtime deinit/restart 承诺，也没有改变触屏/实体键手势或第一次唤屏只亮屏的业务语义。
- 已通过 HAL、System Sleep failure-closure、Gateway capability projection、`git diff --check`，及 Bread 的 `compact_renderer`、Platform Bootstrap/Input 对象构建。未重链其他 profile、未刷机；COM3–COM6 输入、唤屏、回滚 HIL 仍待完成。

## 16. 2026-08-23 D3 legacy Connectivity seam 窄化

- `legacy_connectivity_transport.h` 的全部 `board_port_*` 宏别名已删除。圆屏与紧凑屏 renderer 直接实现显式 `legacy_connectivity_transport_*` 私有契约，包含 transport selection、启动切换、Gateway URL 适配、蜂窝 prepare/start/ready/quiesce、HTTP/stream 和 cancellation；Platform Connectivity 维持唯一桥接入口。
- 这仅清理 ABI/链接边界，并不移动 ML307、Wi-Fi、HTTP handle 或业务编排：Fangtang 的具体调制解调器实现仍在 compact profile adapter，圆屏仍明确返回 cellular unavailable。共享服务继续只通过 Device API / Connectivity Service 访问统一语义。
- 已通过 HAL、System Sleep failure-closure、Gateway capability projection、`git diff --check`，及 Bread renderer、Platform Connectivity 对象构建。其它 profile 未重链、未执行真实 SIM/HTTP cancel/HIL；B3/B4 与 COM3–COM6 网络回归仍开放。

## 17. 2026-08-23 D1/D3 legacy Display scene seam 窄化

- 最大的剩余宏桥接 `legacy_display_scene.h` 已收口：所有 Display scene symbol 由圆屏与紧凑屏 renderer 直接以 `legacy_display_scene_*` 名称实现，删除 31 个 `board_port_*` 重命名宏。Platform Display 仍是唯一共享 bridge，业务与 presentation 不会接触 renderer、panel、DMA 或屏幕形状细节。
- 切片保留原有显示语义，包括宠物资源安装、响应分页、动态 glyph、二维码、DISPLAY_OFF/唤醒、录音波形、天气和闹钟 scene；只替换 source-owner 的内部符号。现 `main/` 中不再存在 `#define board_port_*`；随后 Motion、Audio/Power 切片也已收口，生产代码不再引用 broad board-port 名称。
- 已通过 HAL、System Sleep failure-closure、Gateway capability projection、`git diff --check`，及 Bread `compact_renderer` 和 Platform Display 对象构建。完整四 profile link、COM3/COM6 圆屏显示、COM4/COM5 矩形显示、DISPLAY_OFF/唤醒和 glyph HIL 仍待完成；不得以本次源码门禁声称视觉验收完成。

## 18. 2026-08-23 D1 Motion / callback facade 清理

- 删除遗留 `board_port_motion_get_sample()`：Motion Service 已通过 Platform Sensor → selected profile peripheral adapter 获得唯一规范化 sample，renderer boot 阶段的重复采样日志没有业务价值且会制造第二条硬件访问路径。
- 此切片还移除了当时已无消费者的 button/wake callback 兼容别名；renderer 内部改用窄 Input seam 的 publisher type 与 Device API 的 wake callback type。随后 Audio/Power 收口已删除整个 `board_port.h`，业务、services、presentation、Device API、Platform facade 均无直接 include 或 `board_port_*` 调用。
- 已通过 HAL boundary、`git diff --check` 及 Bread renderer/Platform Audio/Sensor 对象构建；System Sleep 和 Gateway gate 亦通过。本项没有重链四 profile、没有刷机；Motion/声学/唤醒的 COM3–COM6 HIL 仍开放。

## 19. 2026-08-23 D1/D3 legacy Audio/Power facade 删除

- 审计确认 `board_port_*` 的音量、WAV/PCM 播放、命令录音、会议流、wake-word、停止请求及电源遥测实现只残留在圆屏/紧凑屏 renderer source owner 内部，已无业务、Device API、Platform facade 或 profile adapter 调用方。保留它们会形成一条绕过统一会话、Power lease、录音 scene presenter 与 Platform Audio/Power 的重复硬件路径。
- 现已删除两套 renderer 的这组重复实现及无消费者的 `board_port.h`。唯一允许的链路为 `Device API → Audio/Power Service → Platform Audio/Power → selected profile private adapter`；命令录音进度由 `audio_service_capture_progress()` 经 `scene_presenter` 发布，显示仍由各自圆屏/矩形 renderer 的 `legacy_display_scene_*` 私有实现适配。CMake 的 source slot 已改为 `RENDERER_SOURCE`，不再暗示存在可供共享代码使用的 board-port facade。
- 静态审计显示 `main/` 除 reference-fake 注释外已无 `board_port_*` 生产符号或 `board_port.h` include。HAL boundary、System Sleep failure-closure、Gateway capability projection 三项 gate 已通过；Bread 的 `compact_renderer`、`platform_audio_compact`、`audio_service` 对象构建通过。完整 Bread Ninja link 在执行环境 30 秒窗口内已走到 `libmain.a` 链接，尚未得到结束状态，故不记作完整链接通过；其它三 profile 未重链，COM3–COM6 声学、命令录音、音量、DISPLAY_OFF/唤醒与功耗 HIL 均未执行。

## 9. 2026-08-23 生命周期闭锁补强：Ambient / Clock Sync cadence workers

- 继续审计发现 Ambient 一秒场景 cadence 与 Clock Sync SNTP monitor 同样存在“先发布 completion/清 handle，后注销 Registry”的竞态。出现 Registry 注销失败时，原有 ABORT/normal-start 有机会创建新的 cadence 或 SNTP generation。
- 两个服务现记录 terminal exit status 与 `registry_retirement_failed`；自然退出先注销 immutable identity，再发布 completion。失败时普通启动与 System Sleep ABORT 均保持 closed；Clock Sync 还避免对失败的 monitor 执行 SNTP restart/deinit 后的无依据重建。公共 service header 和业务层均未引入 RTOS/ESP-NETIF/board 类型。
- HAL/System Sleep/Gateway 门禁、两个服务的 Bread syntax check、`git diff --check` 与完整 Ninja link 均通过；Bread app 为 `0x3524c0`，最小 app 分区余 `0x4db40`（8%）。EchoEar/Fangtang/Waveshare 尚未针对本批重新完整链接，COM3–COM6 HIL 仍需后续完成。

## 8. 2026-08-23 生命周期闭锁补强：Command cancel service worker

- 审计 `command_service` 的常驻 cancel worker 发现它也会在 Registry 注销前清 handle、发 completion；在启动 rollback 或 Registry owner sweep 与退出竞争时，失败的注销可能被后续 lifecycle 调用误认作已停止。
- worker 现在先退休 Registry identity，随后才发布 terminal exit result、清 handle 和 completion。注销失败后 service 不会重新创建第二个 permanent cancel worker，直至进程重启；公共 `command_service.h` 保持 value-only，不泄漏 Registry/RTOS 细节。System Sleep 继续只关闭取消请求准入而不停止已经存在的 permanent worker，既有取消协议语义不变。
- HAL/System Sleep/Gateway 门禁、`command_service.c` Bread syntax check、`git diff --check` 与完整 Ninja link 均通过；Bread app 为 `0x3523e0`，最小 app 分区余 `0x4dc20`（8%）。本轮未重链 EchoEar/Fangtang/Waveshare，COM3–COM6 HIL 仍开放。

## 20. 2026-08-23 A12/B3 Gateway HTTP active-client registry 收口

- Gateway Transport 现独占 Wi-Fi Gateway request lanes 的 active `esp_http_client` registry：startup、capability-refresh、foreground（包括 voice pairing）、poll 和 asset 五类请求在 `perform()` 前注册，在 cleanup 前按同一 client identity 清除。调用方只可传入 lane bit 与 deadline，不能取得 HTTP handle、mutex 或 FreeRTOS 对象。
- 新增 `gateway_transport_cancel_active_requests()` 及 foreground/capability-refresh 的窄包装。`gateway_lifecycle_service` 直接用该 value-only API 作为 System Sleep 的第一步，并在后续 meeting resume/capability worker、startup、poll PREPARE 失败时保持 marker closed，等待统一 ABORT 逆序恢复；`main.c` 已不再保存上述 Gateway startup/poll/asset/foreground/capability-refresh 的 client pointer、mutex 或 publish/clear callback。
- 此项不等于 B3 全部完成：`main.c` 仍临时拥有 meeting chunk PUT 的 reusable stream（含其独立 active client），以及 Wi-Fi SoftAP/STA/DHCP/netif/SNTP/httpd 的物理生命周期。后续迁移必须为该 stream 和各 ESP-NETIF 根对象分别定义 cancel/join/stop/restart 所有权，不能将 SDK handle 扩散到 Device API 或 HAL。
- 已通过 HAL boundary、System Sleep failure-closure、Gateway capability projection、Input scanner lifecycle 和 `git diff --check`；Bread Compact 标准 wrapper 完整 configure/build/link 通过，app `0x352800`，最小 app 分区余 `0x4d800`（8%）。四 profile 重链及 COM3–COM6 慢网 cancel、sleep PREPARE/ABORT、配对和轮询 HIL 仍为开放验收。

## 21. 2026-08-23 B3 default-loop Wi-Fi/IP callback 生命周期围栏收口

- ESP-IDF default event loop 的创建、handler instance 注册/注销、Wi-Fi driver 与 ESP-NETIF 的物理生命周期仍由 `main.c` composition root 负责；但排队中的 Wi-Fi/IP 回调可能在 root 开始 teardown 或 System Sleep PREPARE 后继续进入共享 Connectivity/Gateway 状态。因此 callback admission、in-flight 计数、drain semaphore 及 PREPARE/ABORT 的 admission 快照，已迁入 `connectivity_service`。
- root 只在所有 event handler 注册成功后打开 value-only admission，并在注销 handler 或销毁网络根对象前请求有界 stop/drain；回调本身仅调用 `enter()/leave()`。Connectivity 的 System Sleep PREPARE 会先关闭 callback admission、等待已进入的 callback 退出，再取消通用网络请求并 park profile transport；ABORT 只恢复 PREPARE 前确实开放的 admission。公共 header 不暴露 `esp_event`、ESP-NETIF、Wi-Fi、HTTP、FreeRTOS handle 或板型类型。
- 已通过 Connectivity host regression、HAL boundary、System Sleep failure-closure、Gateway capability projection 与 Bread Compact 完整 Ninja 链接；app `0x352790`，最小 app 分区余 `0x4d870`（8%）。这不是 B3 完成声明：SoftAP/STA/DHCP/SNTP/httpd、default-loop/ESP-NETIF 的物理 stop/restart transaction 仍留在 root，且 COM3–COM6 的慢网 callback、teardown、PREPARE/ABORT HIL 尚未执行。

## 22. 2026-08-23 A8/B3 Meeting chunk streaming transport 收口

- 会议分块 PUT 原来是 `main.c` 的例外 HTTP lane：它私有保存 active `esp_http_client`、cancel mutex 和 keep-alive client，而 Meeting Service 经多个 host callback 间接访问。现 Gateway Transport 接管该 lane，统一拥有 Wi-Fi active-client/owner identity registry、有界取消、复用连接 reset、Connectivity request admission 与 Fangtang cellular stream fallback；每次 worker stop 只以其 opaque task identity 取消自己的 stream，不能误取消后续 generation。
- Meeting Service 继续是录音、分块 cursor、SHA256、durable recovery 与 UI 进度的唯一业务 owner；它只向 Gateway Transport 传入 path、metadata、value-only Storage reader、stop probe 和 progress callback，不取得 HTTP/FreeRTOS/ML307/profile 句柄。该改动保留 indexed/hash-checked PUT、60 秒上限、分块进度与 pass-end connection reset 语义。
- 已增加 HAL 静态门禁，禁止 `main.c` 再持有 meeting active/reusable HTTP client 或 mutex；HAL boundary、Connectivity、System Sleep failure-closure、Gateway capability projection 门禁及 Bread 完整 Ninja 链接通过。仍需 COM3–COM6 对慢网写入中取消、断网 resume、COM5 ML307 stream、以及 System Sleep PREPARE/ABORT 的 HIL；不据此宣称 B3 或端侧睡眠已完成。

## 23. 2026-08-23 B3 Provisioning SoftAP AP-side network owner 收口

- 新增私有 `services/provisioning_network_owner.[ch]`，作为 Provisioning Service 的物理 SoftAP 子资源 owner：它独占 `esp_netif_create_default_wifi_ap()` / `esp_netif_destroy_default_wifi()`、AP IPv4/DHCP/DNS advertisement 与 NAPT disable/isolation verification。Provisioning Service 继续拥有 portal 的用户空间 HTTP/DNS/表单事务；两者间只传递 `device_status_t`、bool 和显式 lifecycle 调用，不把 `esp_netif_t`、LWIP、ESP 错误或 RTOS handle 扩散到 Device API、Platform 或业务层。
- `main.c` 已删除旧的 `s_setup_ap_netif`、`s_ap_netif_created`、`ensure_setup_ap_netif()`、`configure_setup_ap_ip()` 与 AP-side NAPT 调用；网络 root teardown 只在 portal/callback 已关闭、Wi-Fi 已停止后调用 owner release。STA netif/default-loop、Wi-Fi driver/radio mode、event-handler instance 与整体 physical root stop/restart 仍明确保留在 composition root，不能把此小切片误记为 B3 完成或 runtime hardware restart 已支持。
- `check-hal-boundaries.ps1` 新增唯一 source-owner gate：AP creation、DHCP/DNS、NAPT API 必须只出现在该私有 `.c`，且禁止旧 root symbols 回流；由于 STA teardown 仍合法，门禁特意不泛化禁止 `main.c` 的所有 `esp_netif_*`。已通过 Bread 新 owner 对象编译与 HAL gate；完整 Bread 链接、其余 profile 重新链接，以及 COM3–COM6 的 captive portal、APSTA backhaul、portal stop/restart、System Sleep PREPARE/ABORT HIL 仍待执行。

#### B3 后续小切片：STA ESP-NETIF physical owner（2026-08-23）

- 同一私有 `provisioning_network_owner` 已进一步接管 STA ESP-NETIF：`esp_netif_create_default_wifi_sta()`、ready state 与对应 destroy 只存在于该 source owner。`main.c` 不再保存 `s_sta_netif`、`s_sta_netif_created` 或 `ensure_station_netif()`，Provisioning host 与正常 station startup 均通过值返回的 ensure/ready API 工作。
- 这是将“ESP-NETIF object lifetime”从 radio/credential policy 中分离的收口，而不是迁移 STA 配置。Wi-Fi driver init/deinit、station mode/protocol/credentials/EAP、event callback instance、default-loop 与完整 rollback 依旧由 root 协调；因此不扩大运行时 restart 或 system sleep 的承诺。
- HAL gate 已要求 AP 和 STA default-netif create 都仅在该 private owner，且禁止 `main.c` 回流 STA globals/create call。Bread owner/main 对象编译与 HAL gate 通过；完整链接、其余 profile 以及 COM3–COM6 station reconnect、APSTA 配网/rollback HIL 继续作为待完成证据。

#### B3 后续小切片：ESP-NETIF/default-loop singleton owner（2026-08-23）

- 新增私有 `services/connectivity_network_core_owner.[ch]`，接管 `esp_netif_init/deinit` 与 default event loop create/delete，以及两个独立 singleton 的完整/partial ownership state。`main.c` 删除旧 `s_netif_initialized`、`s_default_event_loop_created`、`s_network_initialized`；它只在既有 physical root transaction 中调用值语义 ensure/ready/has-resources/release，保持原有“partial generation 不得启动下一代、release 任一步失败即闭锁”的回滚原则。
- 该 owner 是物理资源 owner，不是把 ESP-IDF 事件系统放入业务层：Wi-Fi driver init/deinit、radio mode、application handler instance 注册/注销、callback admission、SNTP 停止、AP/STA netif release 顺序、credentials/EAP 与根级 rollback deadline 仍由 composition root 控制。公共 Device/Platform API 完全不出现 `esp_event`、`esp_netif` 或其 handle。
- HAL gate 现要求四个 singleton API 只出现于此 source owner，检查 partial-state/release fence，并禁止上述 root globals/API 回流 `main.c`。Bread 对象构建与 HAL gate 已通过；完整链接、其他三 profile、及 COM3–COM6 cold-start failure、network teardown、portal/STA transition 和 PREPARE/ABORT HIL 尚待完成。

#### B3 后续小切片：Wi-Fi driver / application event-instance physical owner（2026-08-23）

- 新增私有 `services/connectivity_wifi_driver_owner.[ch]`，独占 `esp_wifi_init/deinit`、ESP-IDF 6.0.2 S3 AMPDU disable workaround，以及 `WIFI_EVENT/*`、`IP_EVENT_STA_GOT_IP`、`IP_EVENT_ASSIGNED_IP_TO_CLIENT` 三个 application event-handler instance 的注册、精确 handle 与反向注销。公开 seam 只传递 value callback、`device_status_t` 与 bool；ESP-IDF event callback adapter、handle 和 error 均留在该 source owner。后续同一 owner 还接管 normalized station policy（STA/APSTA、b/g protocol、station config、best-effort power-save disable）及 Enterprise EAP（PEAP/TTLS、identity/username/password、CA/domain、enable/disable state），Configuration/business 层只提供纯值凭据和策略。
- owner 对“driver 已创建但 handler 不完整”的残留 generation 返回 busy；注销按新到旧继续清理，失败只保留确实未注销的 handle；driver deinit 在任何 instance 仍存在时拒绝执行。因此 root 的既有 stop transaction 仍在 callback admission/SNTP/radio stop 之后、netif release 之前调用它，却不再保存 driver/event-instance state。root 仍应拥有 radio stop/mode、STA credentials/EAP/protocol/configuration、callback admission、deadline 与完整 restart policy，不能把这些业务/编排职责误塞进 owner。
- 随后 radio runtime state 也归入该 owner：started、station auto-connect、expected-disconnect、portal AP/STA snapshot、start/stop/connect/disconnect、protected SoftAP config/confirmation 均不再留在 `main.c`。Provisioning Service 仍只透过 host 的值回调完成 portal transaction；其 token 继续是不透明 value，owner 才读取 `wifi_mode_t` 和硬件状态。Connectivity callback 仍保留断线/GOT-IP 的业务观察、UI 与 recovery 决策，不把这些行为错误下沉到 Wi‑Fi owner。
- 最后将 scan record、当前关联 SSID 与 STA/SoftAP MAC 读取也归入 owner：保存网络的 RSSI 排序仍在业务侧，但 scan observer 只接收 SSID/RSSI 值；`main.c` 不再接触 `wifi_ap_record_t`、scan config、MAC selector 或 `esp_wifi_sta_get_ap_info()`。这样新增硬件无需让业务层依赖 Wi‑Fi SDK data structure。
- `check-hal-boundaries.ps1` 已锁定该私有 header 的 value-only 边界、AMPDU workaround、三条 register/unregister 路径、partial-generation fence，并禁止 `main.c` 回流 driver init/deinit、instance API 或旧 state globals。Bread 的新 owner/main 对象编译和 HAL gate 已通过；尚未完成完整 Bread link、其他 profile 重链或 COM3–COM6 cold-start、failure rollback、STA/APSTA transition、teardown 与 PREPARE/ABORT HIL。此小切片不等于 B3 完成，也不宣称 Light/Deep Sleep 支持。

#### B3 后续小切片：Provisioning radio rollback opaque-token 收口（2026-08-23）

- 配网启动失败的 radio rollback 不再由 `main.c` 保存伪 opaque 的 `words[]` snapshot，也不再让 `provisioning_service` 传递可承载状态的 byte buffer。`connectivity_wifi_driver_owner` 私有保存 mode/started/station policy snapshot；Provisioning host 只取得、转交并回传非零 `uint32_t` generation token。
- capture 会替换此前 private snapshot 并生成新 token；note/restore 只接受当前 generation，陈旧 token 以 `BUSY` fail-closed，成功 rollback 后清除 private snapshot。这样延迟的失败清理无法用旧 portal generation 覆盖新一代 radio 状态，且 `wifi_mode_t`、driver config、ESP error 与 snapshot layout 均不越过 owner 边界。
- HAL gate 额外禁止 `main.c` 的 `s_provisioning_radio_snapshot` 回流，验证 Provisioning token 为按值传递的数字、公开 Wi-Fi owner header 不暴露 snapshot storage，并要求 owner 内存在 generation fence。本源码收口不等于完整 B3、runtime restart 或 Light/Deep Sleep 完成；仍须完成完整 restart transaction、四 profile 链接，以及 COM3–COM6 portal failure rollback / APSTA / teardown / PREPARE-ABORT HIL。

#### B3 后续小切片：Provisioning scan SDK-record 收口（2026-08-23）

- Portal SSID 下拉列表不再在 `provisioning_service` 中持有 `wifi_ap_record_t`、`wifi_auth_mode_t` 或调用 `esp_wifi_scan_*`。Wi-Fi owner 继续独占 scan API、SDK record、allocator 和 auth-mode mapping；它输出 SSID/RSSI/归一化 security value，Provisioning 只做去重、RSSI 排序、HTML 转义、Enterprise 标记和已选网络记忆。
- 该路径使用回调逐项复制到 Provisioning 的本地 value 数组；scan 失败时仍保留旧下拉列表，scan 成功才在 mutex 内原子替换，维持原有 portal UX 和凭据安全边界。`main.c` 只做两个 value enum 的窄适配，未将 ESP SDK enum 扩散到业务层。
- HAL gate 现在禁止 Provisioning 的 public/private contract 回流 Wi-Fi scan record、auth enum 或 scan API，并要求 scan 通过 normalized host observer。仍不构成 B3/完整 restart/Light/Deep Sleep 完成；COM3–COM6 的 portal scan、Enterprise 选择、APSTA 和 failure rollback HIL 仍是验收条件。

#### B3 后续小切片：Wi-Fi/IP event 值语义与 physical-root transaction（2026-08-23）

- `connectivity_wifi_driver_owner` 的私有 ESP-IDF callback adapter 现在复制 SDK payload，再向 root 发布 `kind + SSID/MAC/IPv4/hostname` 的纯值事件。`main.c::wifi_event()` 不再依赖 `esp_event_base_t`、`wifi_event_*`、`ip_event_*`、`WIFI_EVENT` / `IP_EVENT` 或 `MACSTR`；它保留 disconnect/GOT-IP 的 Connectivity、UI、蜂窝恢复和业务重连决策，避免把业务行为错误下沉到 physical owner。
- 新增私有 `services/connectivity_network_root_owner.[ch]`，集中执行跨 owner 的物理停止顺序：post-save coordinator / portal HTTP-DNS → callback admission/drain → Clock Sync/SNTP → Wi-Fi radio → application handler instances → AP/STA ESP-NETIF → Wi-Fi deinit → default loop / ESP-NETIF core。portal/restart drain 必须早于 generic CONNECTIVITY Registry sweep，避免两个 owner 对同一 HTTP/DNS generation 并发 stop。各阶段共享 parent deadline；任一步失败即停止后续动作、保留实际资源 marker，下一代 network generation 继续 fail-closed。`main.c` 只注入 lifecycle value bridge，并保留业务凭据、UI、重连策略及 degraded-startup 决定。
- 该项不等于 B3 已完成：正常运行中的可重复 physical restart contract、portal/HTTP-DNS stop 的完整协调、所有异常路径的 failure injection、四 profile 完整链接，以及 COM3–COM6 的 cold-start、APSTA rollback、STA reconnect/GOT-IP、portal teardown、System Sleep PREPARE/ABORT HIL 仍是退出条件。Light Sleep 和 Deep Sleep 仍保持 fail-closed，不能据此宣称支持。

#### B3 生命周期补强：physical-root 的 portal 退出证明（2026-08-23）

- 审计发现：physical-root stop 虽由启动回滚先调用 `stop_provisioning()`，但 private owner 本身仍只能依赖调用方遵守两阶段顺序；若未来运行时 restart、故障恢复或测试入口直接调用 physical stop，可能在 captive portal HTTP/DNS 或 post-save coordinator 仍存活时提前拆除 Wi-Fi/netif。现将 `provisioning_has_live_resources` 作为 lifecycle host 的纯值 bridge 纳入 `connectivity_network_root_owner`：未明确证明 portal generation 已退出时，physical stop 一律返回 `DEVICE_STATUS_BUSY`，不触碰 callback、SNTP、radio、handler 或 netif。
- 同时将 lifecycle host 绑定到当前 physical generation：资源存在期间仅允许幂等重配相同 bridge，不允许替换回调上下文；这避免后续 restart 路径把 stop/drain 请求误路由到另一 composition root。host/public header 仍只包含 status、bool、timeout 与 function pointer，不暴露 ESP-IDF、HTTP、RTOS、netif 或板型类型。
- 此项仅补齐 fail-closed 前置条件，并**未**实现正常运行可重复 physical restart；后者仍须先定义 Gateway/worker 终止、配置/凭据快照、APSTA→STA/portal 重新创建、SNTP/Gateway recovery、new generation callback admission 以及任一阶段失败的可诊断收敛。仍需四 profile 完整链接及 COM3–COM6 portal/teardown/failure-injection HIL，Light/Deep Sleep 继续不可用。
