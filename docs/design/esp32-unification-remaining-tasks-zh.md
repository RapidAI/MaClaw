# MaClaw AgentOS ESP32 HAL 与业务统一剩余任务清单

> 2026-08-30 B3/A9 ML307 HTTP keep-alive/timeout fence：补齐 `Ml307Http::SetKeepAlive`，按标准 `Connection: keep-alive` header 写入同一 ordered header stream，保证仅有一个 final-header 标记且对象析构仍负责释放 modem slot；同时将调用方 timeout 显式下发为非零 modem timeout，配置失败立即终止。ML307 lifecycle、Gateway cancellation、HAL gates 与 Fangtang-4G 完整链接通过（app `0x356f30`，分区余 `0x490d0` / 8%）。真实长连接、UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 HTTP modem-timeout fence：Ml307Http 现在把调用方的有界毫秒 timeout 向上取整为非零秒值，显式配置 connect/response/input 三段 modem timeout；SSL、chunked、encoding、header 与 content 配置失败均立即终止本次请求，避免 modem 使用默认无限/旧配置继续运行。lifecycle、Gateway cancellation、HAL gates 与 Fangtang-4G 完整链接通过；app `0x356da0`，分区余 `0x49260` / 8%。真实 UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 HTTP attributed-error read fence：HTTP `Read()` 在收到归属于当前实例的 malformed/error URC 后立即返回失败，不再因已排队 body 或 EOF 状态把错误伪装成成功读取；无法解析 HTTP id 的 URC 仍完全丢弃，不影响其他 slot。ML307 lifecycle、Gateway cancellation、HAL gates 与 Fangtang-4G 完整链接通过（app `0x356c30`，分区余 `0x493d0` / 8%）。真实 UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 malformed-URC attribution fence：无法解析 HTTP id 的 `MHTTPURC` 现在直接丢弃，不再错误唤醒或污染其他活跃请求；非法 `MHTTPCREATE` 只在对应实例仍处于 `awaiting_create_` 时进入错误路径。ML307 lifecycle、Gateway cancellation、HAL gates 与 Fangtang-4G 完整链接通过（app `0x356c30`，分区余 `0x493d0` / 8%）。真实 UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 HTTP slot/body fence：HTTP content 聚合现在要求累计 body 与 modem `sum_len/cur_len` 精确一致，避免重复/跳跃 chunk 被上层消费；协议 URL 与 status/metadata 边界继续 fail-closed。生命周期 gate、Gateway cancellation、HAL gate 与 Fangtang-4G 完整链接通过（app `0x356be0`，分区余 `0x49420` / 8%）。真实 UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 HTTP parser boundary：ML307 HTTP 入口现在限制 HTTP/HTTPS scheme、拒绝 URL 控制字符/fragment、响应 status 与 metadata 做严格 arity/type 校验，并拒绝超额 MHTTPURC 参数；同一 HTTP slot 的累计 body 长度必须单调一致。Lifecycle、Gateway cancellation、HAL gates 与 Fangtang-4G 完整链接通过（app `0x356be0`，分区余 `0x49420` / 8%）。真实 UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 HTTP multi-instance URC claim fence：Ml307Http 对无 requester identity 的 `MHTTPCREATE` 现在以全局 create 锁串行化 command/URC 配对，并要求每个实例处于 `awaiting_create_` 才能认领 slot；认领后立即释放锁，独立请求可继续并发。content 聚合继续验证累计长度单调性，畸形 response 只唤醒 owner 清理，不误伤其他 slot。相关 lifecycle、Gateway cancellation、HAL gates 与 Fangtang-4G 完整链接通过（app `0x356b90`，分区余 `0x49470` / 8%）。真实 UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 HTTP create-claim fence：`Ml307Http` 对无 requester identity 的 `MHTTPCREATE` URC 增加全局 create 锁与 `awaiting_create_` 认领状态；slot 认领完成后立即释放锁，允许独立 HTTP 请求并发配置。响应 content 长度继续要求单调匹配当前聚合 body，`Content-Length` 采用有界解析，畸形响应保留 owner 析构清理路径。ML307 lifecycle、Gateway cancellation、HAL gates 通过；Fangtang-4G 完整链接 app `0x356b90`，分区余 `0x49470` / 8%。真实 UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 HTTP create/response ownership fence：Ml307Http 对无 requester identity 的 `MHTTPCREATE` URC 增加进程级 create 锁与 `awaiting_create_` claim，拒绝迟到或重复 slot 认领；content 长度必须相对当前 body 单调一致，异常响应只唤醒 owner 清理，不提前丢失 `MHTTPDEL` 责任。`Content-Length` 使用有界 `from_chars` 解析，Read/URL/timeout 参数边界同步收口。ML307 lifecycle、Gateway cancellation、HAL gates 通过；Fangtang-4G 完整链接 app `0x356b40`、分区余 `0x494c0` / 8%。真实 UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 HTTP malformed-response lifecycle fence：Ml307Http 现在对 MHTTPCREATE 严格限制单一有效 slot ID；Read/SetTimeout/URL 入口拒绝无效参数；Content-Length 使用非抛异常、有界解析，解析失败唤醒错误路径；畸形 error/响应不再提前伪造 instance 已销毁，而保留由 owner 析构执行 MHTTPDEL 与 callback 注销，避免 modem-side HTTP instance 泄漏。ML307 lifecycle、Gateway cancellation 与 HAL gates 通过；Fangtang-4G 完整链接 app `0x356ac0`，分区余 `0x49540` / 8%。真实 UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 HTTP owner length/metadata fence：Fangtang cellular HTTP transport 现对 header/status/metadata 做完整 arity、类型与状态码范围校验；header/content 的十六进制数据先解码到临时缓冲，再验证 `cur_len`、累计长度及非 chunked 总长后才并入响应，缺失 payload、长度回退或超界均清空并 fail-closed。AtUart hex decoder 同时增加容量溢出围栏，通用 AtModem 对 CGSN/ICCID/COPS/CSQ/CPIN 的 URC 采用严格 arity/type 与 CSQ 范围校验。ML307 lifecycle gate 与 Fangtang-4G ESP-IDF 6.0.2 全量链接通过（app `0x3569e0`，最小分区余 `0x49620` / 8%）；仍不替代真实 UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL。

> 2026-08-30 A9/B3 Gateway Transport asset-cancellation Host/static gate：新增 `check-gateway-transport-asset-cancellation.ps1`，锁定 asset-download 单 owner guard、等待期间 epoch 取消、Wi‑Fi/ML307 迟到 200 响应清空，以及 cellular owner 自然结束与取消失败的区分。该 gate 仅补充源码边界证据，不替代真实 HTTP/UART abort、renderer/DMA 故障域或 COM3–COM6 HIL。

> 2026-08-30 C3 Battery Policy checkpoint deadline fence：应急低压 checkpoint 现在以单调父 deadline 传递实际剩余预算；持久化回调即使返回 `OK` 但耗尽预算，也转换为 `TIMEOUT` 并锁存当前 PROTECT generation，防止迟到写入被当作安全证据。Battery Policy Host/static gate 与 Fangtang-4G 完整链接通过；brownout/charger wake、阈值实测与四板电气 HIL 仍未完成。

> 2026-08-30 A9/B3 cellular asset cancellation result fence：Fangtang ML307 帧请求现在在返回后再次以 asset epoch 判定终态；即使取消与 200 响应竞态，也清空响应长度/状态并返回取消错误，阻止 SHA/安装消费迟到数据。该切片仍仅为源码/Host/build 证据，不替代真实 UART/ML307 abort、renderer/DMA 故障域与 COM3–COM6 HIL。

> 2026-08-30 A9/B3 asset lane single-owner fence：`gateway_transport_download_frame()` 现在以独立 asset-download guard 串行化公开帧下载，并在登记 task/epoch 前先取得 guard；避免多个 runtime/startup caller 在等待 HTTP lane 时互相覆盖取消身份。该补强仅为源码/Host 生命周期证据，真实 HTTP abort、renderer/DMA 故障域、4G deinit/reinit 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 A9/B3 pet-download cancellation bridge：Gateway Transport asset lane 为 runtime/startup 帧下载登记不透明 owner；Wi‑Fi 通过 active ESP HTTP client 取消，Fangtang cellular 经 Connectivity owner-cancel seam 触达 ML307 请求，下载结束前后清理 owner。该切片仍是源码/Host 级取消衔接，未证明真实 HTTP abort、PSA/renderer/DMA 故障注入、4G modem deinit/reinit 或 COM3–COM6 HIL。

> 2026-08-30 B3/C5 Clock Sync deadline fence：SNTP System Sleep PREPARE 现以单调父 deadline 贯穿 callback drain、monitor stop 与 SNTP deinit，不再在 PREPARE 内重新启动 child timeout；deinit 成功后再次复核 deadline，迟到成功返回 `TIMEOUT`。HAL 与 System-Sleep failure-closure gates 通过；RTC/ESP Sleep、真实网络故障与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/C5 Clock Sync deadline fence：SNTP System Sleep PREPARE 现以单调父 deadline 贯穿 callback drain、monitor stop 与 SNTP deinit，不再在 PREPARE 内重新启动 child timeout；deinit（含本已未初始化路径）成功后再复核 deadline，迟到成功返回 `TIMEOUT`。Clock Sync、Connectivity、System-Sleep 与 HAL gates 通过；RTC/ESP Sleep、真实网络故障与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/C7 Gateway lifecycle cancellation deadline fence：Gateway lifecycle 在取消 active requests 后立即复核同一父 deadline；迟到 `OK` 不再进入 Meeting/transport/dispatcher PREPARE。Host restart-commit 回归、Connectivity、System-Sleep 与 HAL gates 通过；runtime restart、真实 Wi‑Fi/4G fault-domain 及 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/C7 Gateway lifecycle PREPARE deadline postcondition fence：Gateway lifecycle 的 meeting supervisor/resumed worker/capability refresh、transport 与 dispatcher 各 PREPARE 子阶段现在在 callback 成功后复核同一父 deadline；迟到成功立即 `TIMEOUT`，不推进后续 worker fence。相关 Connectivity、network-lifecycle、restart、System-Sleep 与 HAL gates 通过；runtime restart、真实 Wi‑Fi/4G fault-domain 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/C7 Connectivity lifecycle deadline postcondition fence：Connectivity Service 的 System Sleep PREPARE cancel bridge 与 profile physical-prepare 现均在回调成功后复核父 deadline；若迟到成功耗尽预算，直接 `TIMEOUT` 并保持 fence，不继续 transport park。Network Lifecycle Service 的 logical deinit 同样加入成功后 deadline 复核，Host 回归覆盖迟到 deinit。相关 Connectivity/network-lifecycle/restart/HAL/System-Sleep gates 通过；runtime restart、真实 Wi‑Fi/4G fault-domain 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/C7 Connectivity PREPARE cancel deadline fence：System Sleep 的 composition-root cancel bridge 现在接收同一父 deadline 的实际剩余预算；回调返回 `OK` 但恰好耗尽预算时立即返回 `TIMEOUT`，不再继续 profile transport park。Host Connectivity 回归新增该迟到成功窗口覆盖并通过；这仍是源码/Host 证据，不替代 Wi‑Fi/4G fault-domain 与 COM3–COM6 HIL，runtime restart 继续保持未绑定、fail-closed。

> 2026-08-30 B3/C7 runtime-restart deadline postcondition fence：Connectivity Restart Coordinator 现在在每个 bridge 返回 `OK` 后再次复核同一父 deadline；若 bridge 恰好耗尽预算，立即进入 terminal `FAILED/TIMEOUT`，不再推进下一物理阶段或发布 `COMPLETE`。Host restart matrix、HAL boundary、configuration migration gate 通过；由于 physical-root/APSTA/ML307/Gateway rearm 仍未具备生产安全触发条件，runtime restart 继续保持未绑定、fail-closed，COM3–COM6 HIL 仍未完成。

> 2026-08-30 C7 factory-reset deadline propagation：Factory Reset Service 现在以单调父 deadline 覆盖 journal/marker admission 与 PREPARE，向 `prepare_for_reset` 传递实际剩余预算，禁止子步骤重新开始完整 5000 ms；Host 回归覆盖预算已被前置 I/O 消耗的情形，configuration-migration、connectivity lifecycle 与 HAL gates 通过。Fangtang-4G 最新完整 ESP-IDF 6.0.2 configure/build/link app `0x355870`，最小 app 分区余 `0x4a790`（约 8%）。这些仍是源码/Host/build 证据，不替代真实断电注入、跨版本恢复、认证擦除及 COM3–COM6 HIL。

> 2026-08-30 B3/C7 provisioning race fence：physical-root 在 callback admission 与 SNTP drain 完成后分别重新读取 provisioning live-resource 事实；若 portal/restart generation 在初始检查后仍存活，立即 `BUSY` 并停止后续 radio/netif teardown。HAL/connectivity gates 通过；仍不替代真实并发慢网、Wi-Fi/4G fault-domain 及 COM3–COM6 HIL。

> 2026-08-30 B3/C7 provisioning-drain deadline fence：physical-root 的 provisioning stop callback 返回成功后现在先重算父 deadline，再读取 live-resource 事实；回调耗尽预算时直接返回 `TIMEOUT`，不把迟到的资源观察误报成 teardown 成功。HAL boundary 与 connectivity lifecycle gates 通过；仍不替代真实慢网/故障注入及 COM3–COM6 HIL。

> 2026-08-30 C6 migration generation immutability fence：迁移 journal 的 durable revision 现在只能在 PREPARED 阶段绑定；VALIDATED/COMMITTED 之后拒绝代际改写，防止已发布目标证据被重定向到另一份 V7 record。Host migration、HAL gates 与 Fangtang-4G 完整链接通过；仍不替代真实断电注入、跨版本恢复矩阵、认证擦除及四板 HIL。

> 2026-08-30 C6 migration journal negative-transition matrix：Host 回归补充 NONE/COMMITTED 非法跳转断言，并由门禁锁定 generation 只能在 PREPARED 阶段修改；Fangtang-4G 最新重链 app `0x355830`，最小 app 分区余 `0x4a7d0`（8%）。这些仍是源码/Host/build 证据，不替代真实断电窗口、跨版本恢复、认证擦除及四板 HIL。

> 2026-08-30 B3/C7 netif-release fence 后 Fangtang-4G 正式 profile 重链：ESP-IDF 6.0.2 configure/build/link 成功，app `0x355820`，最小 app 分区余 `0x4a7e0`（8%）。该结果仅更新源码/Host/build 证据，不替代物理 Wi-Fi/4G teardown、board reinit 或 COM3–COM6 HIL。

> 2026-08-30 B3/C7 physical-root netif release fence：Connectivity physical-root teardown 现把 setup AP 与 station netif 的 `void` 销毁分别置于同一父 deadline 的独立阶段；每次调用前重算预算、调用后重读对应资源事实，残留句柄或预算耗尽均保持整代 closed，不继续 driver/default-loop deinit。相关 connectivity/HAL/configuration gates 通过；仍仅为源码/Host 级证据，不替代 Wi-Fi/4G fault-domain、board deinit/reinit 与 COM3–COM6 HIL。

> 2026-08-30 A8 Tool-result outbox alias 边界补强：持久队列 append 允许精确同址 in-place 操作，但拒绝 queue/output 的部分重叠别名，避免迁移/追加时覆盖尚未扫描的旧记录并错误推进序列号。Host outbox regression 与门禁通过；该值层防护仍不替代真实断网、多结果断电窗口、Hub 幂等兼容及 COM3–COM6 HIL。

> 2026-08-30 C6 migration target-evidence publication-window 收口：旧 Configuration 迁移现在先在 PREPARED journal 仍有效时持久化 target fingerprint，再发布 V7 store，VALIDATED/COMMITTED 只在目标写入后推进；重启发生在 target 写入前后均可基于 source/target 证据作出 fail-closed 判定，不再出现“目标已写但 target digest 尚未落盘”的歧义窗口。Configuration migration gate 与既有 Host 回归通过；真实断电注入、跨版本恢复矩阵、认证擦除及四板 HIL 仍未完成。

> 2026-08-30 C5 trusted-time lifecycle owner 收口：Hub `serverTime` 不再由 `main.c` 持有静态 trust state 或直接解释/设置系统时钟，现统一经 `Clock Sync Service` 的可信时间状态机、System Sleep admission 与消费者通知路径处理；SNTP 与 Hub authenticated time 共用 anomaly/generation 围栏。Host/static gates 与完整门禁通过。该切片仍不提供 RTC/签名构建时间下界、TLS 首连信任引导、Secure Element/mTLS、私有 EAP CA、真实熵故障注入或四板无线 HIL。

> 2026-08-30 C2/C5 Wake Deadline trusted-fact fence：Wake Deadline 不再仅凭重启后 `gettimeofday()` 的年份阈值认定墙钟可信；其 deadline arm/dispatch 现在要求 Clock Sync 在本 boot 成功准入并应用 SNTP 或 Hub authenticated 样本后发布显式 trusted-time fact。重启后残留 RTC 值保持 fail-closed，直到新可信样本到达。Host、alarm wake-plan、System Sleep 与 HAL gates 通过；仍不提供 RTC wake、真实 ESP Sleep 或四板电气 HIL。

> 2026-08-30 C6/A8 Fangtang-4G 增量重链：Configuration migration target fingerprint publication-window 与 Tool-result outbox alias fence 修改已通过 ESP-IDF 6.0.2 完整 Ninja configure/build/link；Fangtang app `0x3558e0`，最小 app 分区余 `0x4a720`（8%）。以上仅为源码/Host/build 证据，不替代真实断电注入、多结果断网、Hub 兼容、四板 HIL 或真实睡眠/无线验证。

> 2026-08-30 多 profile 构建复核：Bread Compact、EchoEar-2ST、Waveshare AMOLED 1.75C 均完成 ESP-IDF 6.0.2 完整 configure/build/link。产物分别为 `0x35be30`（最小 app 分区余 `0x441d0` / 7%）、`0x3486e0`（余 `0x57920` / 9%）、`0x4a32e0`（余 `0x5cd20` / 7%）；Fangtang-4G 在 trusted-time lifecycle 收口后重新链接为 `0x3558a0`（余 `0x4a760` / 8%）。构建期间已通过 HAL boundary、System Sleep failure-closure、persistence worker 等门禁。以上仅为源码/Host/build 证据，不替代四板 HIL、断电注入或真实睡眠/无线验证。

> 2026-08-30 C3 Waveshare AXP2101 电量寄存器边界补强：Round Platform Power 现与 Compact/Fangtang 一致，拒绝 AXP2101 返回的超出 0–100% 范围的 capacity，不再把异常值静默夹到 100% 以绕过 Battery Policy 保护。该改动仅是 profile telemetry fail-closed 语义，未增加 brownout/低压中断、charger wake、阈值实测或电气 HIL，C3 仍未完成。
> 2026-08-30 A12/C7 configuration persistence worker retiring admission fence：worker 进入 Registry 退役阶段后，submit/submit_until 与 System Sleep PREPARE 现均拒绝新事务，避免旧 generation 在 immutable Storage identity 注销窗口内继续获准写入或被错误纳入可恢复睡眠事务。该切片通过 persistence-worker、System Sleep failure-closure 与 HAL boundary 静态/Host 门禁；Fangtang-4G 完整链接成功（app `0x3555c0`，最小 app 分区余 `0x4aa40` / 8%）；仍不替代 Registry contention、断电及四板 HIL。

> 2026-08-30 B3/C7 Connectivity physical-root success-fact fence：网络生命周期 service 现在要求 core/driver 初始化回调在返回成功时同步发布 ready 事实；若物理 stop 回调返回成功但仍报告残留资源，则拒绝继续 deinit logical Connectivity，保持整代 closed。新增 Host 回归覆盖“成功但未 ready”与“stop 后资源残留”两类歧义窗口；network lifecycle gate 通过。该源码/Host 切片仍不替代 Wi-Fi/4G fault-domain、board deinit/reinit、Registry contention 与 COM3–COM6 HIL。

> 2026-08-30 C3 Battery Policy checkpoint failure latch：应急低压 checkpoint 失败现在锁存当前 `PROTECT` generation，后续 telemetry 轮询或 callback 替换均返回 BUSY，不再重复写 Flash；只有确认离开并重新进入 `PROTECT` 才建立新的一次性预算。Battery Policy Host/static gate 与 Fangtang-4G profile build gate 通过。该切片仍不构成 brownout/charger wake、电气阈值实测或 COM3–COM6 HIL。

> 2026-08-30 B3/C7 网络物理根 teardown deadline fence：Connectivity network root owner 在 callback/SNTP drain 后，对 Wi‑Fi radio stop、application handler unregister、AP/STA netif release、driver deinit 与 ESP-NETIF/default-loop release 逐阶段重算同一父 deadline；预算耗尽即停止后续 teardown 并保持 generation closed，避免同步 SDK 调用掩盖超时后误报根释放成功。network lifecycle/HAL gates 通过；仍不替代 Wi‑Fi/4G fault-domain HIL、board deinit/reinit、Registry contention 与 COM3–COM6 验证。

> 2026-08-30 C2 wake drift guard 值层补强：`alarm_wake_plan` 现在携带 `boot_lead_ms + drift_guard_ms`，输出独立的 `wake_arm_epoch_ms` 与最终 alarm deadline，并拒绝 lead 溢出或 arm 时间已落后当前墙钟的计划；墙钟校正可通过 revalidate helper 重新判断 deadline。该切片仍不提供 RTC 校准数据、不调用 ESP Sleep API，也不改变四 profile Light/Deep `verified_sources=0` 的 fail-closed 状态。Host、HAL boundary 与相关 System Sleep gates 通过。

> 2026-08-30 C2 Power PREPARE earliest-alarm fence 已接入：Alarm PREPARE 关闭工具/调度 admission 后，Power Service 读取持久队列最早闹钟与 Wake Deadline trusted wall-clock，经值层 `alarm_wake_plan_compute()` 校验 TIMER verified source、未来 deadline 与到期状态；规划失败执行完整逆序 ABORT。当前四 profile Light/Deep `verified_sources=0`，真实 RTC/ESP Sleep COMMIT 仍 fail-closed，Host 与 HAL/System-Sleep gates 通过。

> 2026-08-30 C2 earliest-alarm PREPARE fence 接线：Power Service 在 Alarm PREPARE 关闭工具/调度 admission 后读取持久队列最早闹钟，并通过 `wake_deadline_service_get_clock_status()` 与 `alarm_wake_plan_compute()` 统一校验 trusted wall-clock 和 TIMER verified source，再允许后续 profile-private sleep PREPARE。当前四 profile Light/Deep `verified_sources=0`，生产请求仍在更早 Wake capability gate fail-closed；本切片未接入 RTC/ESP sleep，也未改变硬件能力声明。Host alarm-wake-plan、Wake matrix/capability 与 System Sleep failure-closure 检查通过。

> 2026-08-30 C2 值层 earliest-alarm wake plan 首个切片：新增 `main/alarm_wake_plan.[ch]`，以 ABI 化纯值输入/输出校验最早排队闹钟、可信墙钟和 verified wake source，输出是否允许后续睡眠事务及唤醒 deadline；无 RTC/GPIO/FreeRTOS/ESP-IDF 副作用。Host 回归通过，但四正式 profile 的 Light/Deep `verified_sources` 仍为 0，故该契约不会开放真实睡眠或声称 RTC wake/HIL 完成。

> 2026-08-29 A11 Alarm whole-ring transaction fence：Alarm ring 生命周期新增 transaction epoch；interruption marker 同时绑定 epoch 与 generation，仅允许同一 Alarm transaction 的 burst/idle 收尾窗口消费，Alarm 事务结束或新非 Alarm owner 接管即清除孤立 marker。Host extraction/matrix 与 Fangtang 完整构建通过；真实抢占恢复及四板音频 HIL 仍未完成。

> 2026-08-29 A11 WAV/PCM interruption cleanup 补强：WAV 播放在释放 power/foreground lease 后消费 Alarm interruption marker，并以 BUSY 返回，避免部分播放被误报成功；marker 在 Alarm 的连续 burst/间隙以及收尾 idle 窗口内仍只可由原 owner 消费，后续非 Alarm owner 接管时清除。Extraction gate、Host matrix 与 Fangtang 完整链接通过；真实抢占听感与四板 HIL 仍未完成。

> 2026-08-29 A11 抢占事实 generation fence 补强：Alarm interruption marker 现在同时绑定被抢占 session 类型与代际窗口；原 Command/Meeting owner 在正常 cleanup 后可消费，若调度竞态使 Alarm 先进入前景或刚释放 Alarm 后 owner 才完成，也只允许对应的一个活动/收尾窗口。后续非 Alarm owner 接管时，孤立 marker 自动清理，避免旧闹钟事件污染后续录音/会议。Audio Arbitration extraction、Host matrix 与 Fangtang 完整构建通过；真实 Alarm→capture/meeting recovery、四板音频 HIL 仍未完成。

> 2026-08-29 C6 scalar migration evidence cleanup：散落旧标量迁移在成功清理 PREPARED/VALIDATED/COMMITTED journal 后，现在同步删除 target fingerprint；避免无 source blob 的旧证据在后续启动中成为孤立恢复材料。Migration/factory-reset Host gate 已通过；真实断电注入、跨版本恢复与认证擦除 HIL 仍开放。

> 2026-08-29 C6 migration journal PREPARED publication-window 收口：恢复逻辑现在覆盖“PREPARED marker 已持久化、目标 V7 store 已发布、但 VALIDATED marker 尚未持久化”的断电窗口；仅当目标 store 完整校验通过且 durable revision 与 journal generation 精确匹配时才清理 marker，否则继续保留证据并 fail-closed。该切片已通过 Host/static gate；真实断电注入、跨版本恢复矩阵、认证擦除链及四板 HIL 仍未完成。

> 2026-08-29 C6 factory-reset Host recovery matrix 增量：Host 回归新增 PREPARED journal 拒绝重入、损坏 journal 保留证据并 fail-closed，以及 COMMITTED + 畸形 delivery marker 不得解锁恢复的断言；相关状态机与固定擦除清单仍未经过真实断电注入、跨版本矩阵、认证擦除链和四板 HIL，C6/C7 继续保持未完成。

> 2026-08-29 C6 恢复出厂与 Configuration mutation fence 接线：恢复出厂 PREPARE 现在同时关闭 Configuration Service 的直接 mutation/read admission 与 Persistence worker fence，再进入固定擦除清单；PREPARE 失败仅回滚可逆 fence，journal 写入或擦除结果不确定时继续保持终结性闭锁。新增静态 gate 检查 prepare/abort 对称接线。该源码级并发围栏仍不替代真实断电注入、跨版本恢复矩阵、认证擦除链及四板 HIL。

> 2026-08-29 C6 migration journal VALIDATED crash-window 收口：启动恢复现在区分 VALIDATED marker 在目标 V7 store 发布前后两种窗口；目标已存在时必须校验完整 store、schema 与 durable revision 后才清理 marker，目标仍缺失则保持 fail-closed。另拒绝将超出 32-bit journal ABI 的存储尺寸截断记录。Host migration gate 通过；真实断电注入、跨版本恢复矩阵、认证恢复出厂擦除与四板 HIL 仍未完成。

> 2026-08-29 C5 generation floor 启动落后语义修正：Gateway Transport 可能在 Persistence bridge 安装前先分配本地 generation；恢复时若 NVS floor 小于该本地值，Credential Service 现在保持 generation 单调并将 floor 修复写高，不再把正常启动顺序差异误报为损坏。floor 大于本地值仍被采用，越界/读写错误继续 fail-closed；新增 host matrix 覆盖落后 floor 修复与 ahead floor 恢复，Fangtang-4G 完整构建通过。Secure Element、mTLS、跨版本撤销恢复矩阵、真实熵故障注入和四板无线 HIL 仍未完成。

> 2026-08-29 C5 Credential generation floor 并发/故障收口：generation floor 写入增加串行化与写前重读，防止并发 begin/revoke 完成乱序导致 NVS floor 回退；floor 初始化写失败会立即关闭 Credential Service。Host regression、静态门禁与 Fangtang-4G 完整构建通过；Secure Element、mTLS、跨版本撤销恢复矩阵、真实熵故障注入和四板无线 HIL 仍未完成。

> 2026-08-29 C5 Credential generation floor 持久化切片：Credential Service 新增 value-only generation-floor 回调；composition root 在 Persistence Service 启动后、读取配置前从 NVS 恢复单调 floor，并在 generation begin/revoke 时持久化。读写故障会使凭据服务保持 fail-closed；撤销即使 floor 写失败也保留更高内存 tombstone，禁止旧 token 复活。新增 malformed floor/故障路径 host 回归及静态接线检查，Fangtang-4G 完整构建通过。仍不等同 Secure Element、mTLS、完整跨版本撤销恢复矩阵、真实熵故障注入或四板无线 HIL。

> 2026-08-29 C5 Credential Service 输入/输出边界加固：token、device identity 的所有外部字符串均改为固定容量 bounded scan，拒绝非 NUL 终止输入；Gateway token copy-out 在 generation/identity/容量失败时先清零目标缓冲并将 `out_length` 归零，避免调用方复用缓冲泄露旧 secret。Host regression、静态 gate 与 Fangtang-4G 完整构建通过；Secure Element、mTLS、跨重启撤销 tombstone、真实熵故障注入及四板无线 HIL 仍未完成。

> 2026-08-29 C6 legacy scalar journal 断电语义补强：散落 NVS scalar 迁移现在先持久化 PREPARED + 目标 revision，再发布 V7 store，随后写 VALIDATED/COMMITTED；恢复时对无统一 source blob 的 PREPARED/VALIDATED marker 在目标缺失场景保持 fail-closed，不再因缺少 source fingerprint 而错误清理。`check-configuration-migration-journal.ps1`、相关 host regression 与 Fangtang-4G 完整构建通过；真实断电注入、跨版本恢复矩阵、认证恢复出厂擦除及四板 HIL 仍未完成。

> 2026-08-29 C5 Credential Service 启动恢复原子发布：新增 value-only `credential_service_restore_gateway_token()`，在同一 generation 锁内同时恢复 Gateway token 与本 boot 设备身份；Gateway Transport 遇空 token/空 identity 时保持未配对，不发布 token-only 状态。Host credential gate 与回归已通过。该切片仍不等同于跨重启 generation 持久化、Secure Element、mTLS 设备身份绑定、可信时间 TLS 引导、私有 EAP CA 或四板 HIL。

> 2026-08-29 C5 可信时间输入策略首个切片：新增平台无关 `trusted_time_policy.[ch]`，Hub `serverTime` 现在必须经过来源、有限时间窗、有限值及整数毫秒校验后才进入 `settimeofday`；不再由 main.c 直接解释浮点时间边界。新增 host/static gate 与 Fangtang-4G 完整构建通过。该切片仍不等同于受信 RTC/签名构建时间下界、TLS 首连死锁解决、Credential Service、设备身份绑定、私有 EAP CA 或四板 HIL。

> 2026-08-29 C5 Gateway Transport URL 解析边界补强：统一抽出 authority 校验，正确处理主机名、端口及括号 IPv6 literal；Hub 下发绝对媒体 URL 继续仅允许 HTTPS，并对完整 URL 的控制字符、反斜杠与 fragment fail-closed。相对请求仍要求已验证 Gateway HTTPS origin。Entropy/Configuration journal 门禁通过；Fangtang-4G 构建证据待本轮源码变更后更新。此项仍不构成 Credential Service、TLS 设备身份绑定、可信时间状态机、私有 EAP CA 或四板媒体 HIL 完成声明。

> 2026-08-29 C5 Gateway Transport 媒体 URL 最终边界：Hub 返回的绝对上传/下载地址现由 Transport 在请求执行前统一拒绝明文 `http://`、空 authority、控制字符、`@`、反斜杠及 `#` 等歧义输入；相对 Gateway API 继续只能拼接已验证的 HTTPS origin。该切片补齐了 Transport 末端的绝对媒体 URL fail-closed 约束，并通过 Entropy/静态门禁及 Fangtang-4G 完整构建；仍不构成 origin allow-list、TLS 设备身份绑定、可信时间引导、私有 EAP CA、Credential Service 或四板媒体 HIL 完成声明。

> 2026-08-29 C5 HTTPS/TLS trust fail-closed 收口：配置事务与 Provisioning Service 现仅接受 `https://` Gateway origin，并拒绝控制字符、`#`、`?`、`@`、反斜杠等歧义 authority 输入；企业 Wi-Fi 仅允许 `ca_mode=system`，配网页面不再提供“Do not validate”选项。Fangtang-4G cellular adapter 取消将 Hub HTTPS 改写为 `http://:9399` 的 TLS downgrade；ESP EAP owner 对无系统 CA 的配置直接拒绝。Host configuration transaction 回归及 provisioning/entropy/HAL gates 通过。该增量仍不构成私有 EAP CA、TLS 设备身份绑定、可信时间引导、Secure Element 或四板无线 HIL 完成声明。

> 2026-08-29 C5 Entropy Service 首个源码增量：新增平台无关的 value-only `services/entropy_service.[ch]`，由 composition root 在生成 boot correlation 前建立一次 entropy readiness barrier；Provisioning 的临时 WPA2 passphrase/CSRF 以及 boot session random bytes 均经该 service 获取，业务与配网服务不再直接调用 `esp_fill_random()`。公共头不暴露 ESP-IDF、RTOS、凭据或硬件类型；Entropy source owner 失败时启动在 CORE_SERVICES 阶段 fail-closed，不继续打开配网或 Gateway。`check-provisioning-extraction.ps1` 与 HAL boundary gate 已通过。该增量只收口随机源接线和调用边界，不构成硬件 RNG health test、secure-element/credential service、TLS 设备身份绑定、可信时间引导或四板 HIL；后续仍需将 credential 生命周期与 TLS trust state machine 接入同一 barrier，并补充真实熵故障注入证据。

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
| A9 | Ambient Service：时间/天气/网络/pet 聚合为待机场景 | 天气/时钟/ambient 在 main.c（:828-834 一带） | 不覆盖高优先级 foreground；待机字段与现状一致。**2026-08-20 第一–三增量：weather/clock/network/scheduled/pet。2026-08-21 第四增量：Hub 天气/glyph JSON 解码迁入 `ambient_service`（`apply_hub_ambient`/`apply_hub_glyphs`，公共头无 cJSON）；PSRAM 栈仍延后 NVS。2026-08-23 D4 per-entry owned glyph record 已在 `display_service` 收口（见 D 组）。同日 SNTP singleton/retry/trusted epoch/System Sleep fence 已迁入 `services/clock_sync_service.[ch]`；Hub serverTime 与 SNTP 共用 value callback。宠物缓存 worker 已迁入 `services/pet_cache_service.[ch]`，该服务拥有 internal-stack Flash worker、admission、Storage registry、lease/cancel commit fence 与可逆 sleep participant；task 创建、Registry 登记、handle 发布的临界期由 `s_starting` 封闭，sleep/stop 不会将尚不可停止的 worker 误判为 quiesced；后台 descriptor 在接管帧所有权前严格校验 frame_count 与逐帧非空。cold-start 宠物 retry timer/callback/due flag 也已收口到 `services/startup_pet_retry_service.[ch]`：callback 仅发布 coalesced value fact，Gateway worker 负责消费；PREPARE 关闭并 drain callback，超时保持关闭至 ABORT。宠物下载 traversal/retry 已在 `services/pet_asset_download_service.[ch]`，帧完整性计算/比较已在 `services/pet_asset_integrity_service.[ch]`；HTTP cancel registry、renderer install 与 startup 下载编排仍由 root 注入。真机宠物/glyph/时钟校正回归未做** |
| A10 | Provisioning Service：配置事务、portal 状态、配对状态 | 仅 `provisioning_failure_injection.c`；setup portal + captive DNS 在 main.c（直接 include `esp_http_server.h:18`） | 先行为等价迁移；配网安全化见 C4。**2026-08-20 第一增量完成：`provisioning_service.c` 收口 HTTP/DNS/scratch/lease/post-save restart 与表单事务；SoftAP/STA/DHCP/netif 经 host 留 main.c（B3）；`esp_http_server.h` 已离开 main.c。现为随机 WPA2 临时 AP，具 CSRF/TTL/限流/isolation 与 secret zeroization；TLS/身份绑定会话、runtime APSTA→STA transition 与真机配网回归仍属 C4/B3** |
| A11 | Audio Arbitration Service：capture/playback/wake lease、抢占和静音策略 | 仅 `audio_service.c` 薄封装 | Command/Meeting/Alarm 经 lease 获取音频；闹钟抢占按附录 §9 矩阵执行。**2026-08-20 第一增量（shadow）：Command/Meeting/Alarm wrapper。2026-08-21 第二增量：WAV/PCM wrapper。2026-08-21 第三增量：volume / stop token / wake-word 经 wrapper；`main.c`、input_binding、interaction、provisioning 改道。2026-08-29 增加 authoritative 抢占安全边界：Alarm 在显式开启时按当前 session generation 发出 profile-owned stop token，并在 300 ms 有界等待 owner 正常清理；超时 fail-closed，不跨任务拆 codec/I2S。默认仍为 shadow，未改变现网行为；Host 矩阵与静态 extraction gate 通过。物理会话仍独占 BUSY；真机抢占听感、会议 gap/恢复和四板 HIL 未做** |
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
| C5 | Security/Credential Service、TLS 可信时间引导、EAP 私有 CA、Entropy Service | **已完成多项源码/host 切片**：Credential Service generation、identity binding、启动恢复原子发布、恢复出厂重启前撤销；Gateway Bearer 统一 bounded copy-out；可信时间、system CA/domain、Entropy readiness 已有独立门禁。 | 仍需跨重启 generation/撤销链、Secure Element 或 mTLS 设备身份、私有 EAP CA、完整 TLS trust state machine、真实熵故障注入及 COM3–COM6 HIL；当前不得宣称 C5 完成。 |
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

## 24. 2026-08-28 A12 启动宠物缓存恢复 worker 收口

- 新增私有 `services/pet_asset_restore_worker_service.[ch]`：它独占一次性、internal-RAM stack 的 FreeRTOS restore worker 及 completion join；`main.c` 不再创建 `maclaw_pet_restore` task、semaphore 或保存其 join 细节。这样缓存恢复仍在 App UI renderer owner 已建立、Connectivity/TLS 大块分配启动之前同步完成，但 composition root 只提供一个 value-only 的 restore transaction callback。
- `pet_asset_restore_service` 继续负责 descriptor/frame 验证、完整安装、无效缓存清理与 cached-profile 发布；Storage、PSA SHA、allocator 和 Display apply 也仍保持原有 owner。新 worker 不是常驻生命周期 participant，未擅自引入 System Sleep ABORT/重启语义。
- HAL gate 现锁定 public worker host 不泄漏 ESP-IDF/FreeRTOS/allocator 类型，并禁止旧 root restore task helper 回流；宠物服务 host regression、HAL boundary、System Sleep failure-closure 与 `git diff --check` 均通过。Bread Compact 完整 configure/build/link 通过，app `0x354680`，最小 app 分区余 `0x4b980`（8%）。其余 profile 重链和 COM3–COM6 的 cold-start cache-restore/flash-pressure HIL 仍未完成；这不构成 A12、B3 或 MCU Light/Deep Sleep 的完成声明。

## 25. 2026-08-28 B3 Wi-Fi cold-start policy 小切片收口

- 新增 value-only `services/wifi_startup_service.[ch]`，接管已保存个人热点的 RSSI 去重排序、候选 attempt epoch/wait、enterprise 与 personal 分支、scan/候选失败后的常规凭据回退及 timeout outcome。`main.c` 只保留 credentials/runtime state 与 host bridge：ESP-IDF/netif/Wi-Fi driver owner 调用、Connectivity readiness waiter、portal 状态及 UI/ambient 发布均未移入业务 service。
- 语义回归覆盖：首个候选不主动 disconnect，后续候选即使 best-effort disconnect 失败仍建立新 attempt；所有候选失败后保留实际最后选择的凭据/driver 配置供原有 auto-reconnect 使用。enterprise 不扫描已保存列表，scan 失败或没有可见保存热点仍回退 primary station。公共 header 不暴露 ESP-IDF、FreeRTOS、HTTP、JSON、allocator、Wi-Fi/netif 或 board type。
- `check-wifi-startup-service` host test、HAL boundary、pet-asset service 及 System Sleep failure-closure 门禁已通过；Bread Compact 完整 configure/build/link 通过，app `0x354440`，最小 app 分区余 `0x4bbc0`（8%）。本项不是 runtime Wi-Fi restart/portal/SNTP/httpd owner 的完整迁移；B3 的 dependent bridge、APSTA/post-save transition、运行时 restart 接入和 COM3–COM6 HIL 仍为开放项，不能据此标记 B3 或 Light/Deep Sleep 已完成。

## 26. 2026-08-28 A12 内部栈 Configuration persistence worker 收口

- 新增私有 `services/configuration_persistence_worker_service.[ch]`，接管 volume、brightness、screen-sleep display policy 与 pairing token 的 internal-RAM FreeRTOS queue/task、generation reply、Storage Registry identity、create→register start gate、终止 join 以及 System Sleep PREPARE/ABORT fence。`main.c` 已删除旧 `s_output_volume_persist_*` queue/task/RTOS 状态，只通过纯值 request/reply host transaction 继续保留 Configuration 事务和运行时投影更新。
- 保持原有约束：NVS/flash 写绝不在 PSRAM stack 的 Gateway worker 中执行；本地与 Hub policy 写继续分别使用原来的 4 s mutex、1 s queue、3 s completion，以及 pairing token 的 4 s/4 s/4 s 预算。worker 先注销 immutable Storage Registry identity，才发布 terminal completion；注销失败永久关闭 admission，System Sleep ABORT 不会对旧 identity 创建替代 generation。
- 新增 host value-contract/lifecycle gate，并更新 HAL 与 System Sleep failure-closure 检查以验证新 owner，`check-configuration-persistence-worker-service`、Wi-Fi/pet service、HAL boundary、System Sleep failure-closure 及 `git diff --check` 已通过。Bread Compact 完整 configure/build/link 通过，app `0x354120`，最小 app 分区余 `0x4bee0`（8%）。此项不等同 A12 或 C1/C7 完成：其它 root coordinator、runtime restart、四 profile 重链与 COM3–COM6 flash/sleep/Registry-contention HIL 均仍待完成。

## 27. 2026-08-28 A12 延迟配网门户 worker 收口

- 新增私有 `services/deferred_setup_worker_service.[ch]`，接管长按触发后的延迟 setup-portal worker 的 FreeRTOS task、start gate、completion join、Connectivity Task Registry identity、create→register fence、retirement 结果及 System Sleep PREPARE/ABORT admission/restart fence。公共 host 仅包含 value-only 的“meeting 是否活跃”观察与“启动 portal”动作，不携带 ESP-IDF、FreeRTOS、HTTP、Wi-Fi/netif、JSON、allocator 或板型对象。
- `main.c` 只在 composition root 注入 meeting/portal callback；五秒 meeting 等待、保持 GPIO scanner 不被 portal/radio 操作阻塞、SAFE_MODE 的 bounded terminal stop，以及 sleep rollback 仅恢复 PREPARE 前既有 generation 的语义均保留。worker 必须在通知 completion 前注销旧 immutable Registry identity；注销失败或 join 超时均保持准入关闭，避免新 generation 被旧 Registry entry 误停。
- 新增 host value-contract/lifecycle/composition gate，并调整 HAL/System Sleep failure-closure 检查为验证新 owner。`check-deferred-setup-worker-service`、HAL boundary、System Sleep failure-closure 通过；Bread Compact 完整 Ninja link 通过，app `0x354160`，最小 app 分区余 `0x4bea0`（8%）。本源码验证不代表 A12、B3、C1/C7 或 COM3–COM6 HIL 已完成；wake-restart coordinator、runtime restart、其余正式 profile 重链及 portal/sleep/Registry contention 实机验证仍开放。

## 28. 2026-08-28 A12 offline wake restart worker 收口

- 新增私有 `services/wake_restart_worker_service.[ch]`，接管 offline wake recognizer 重启的 PSRAM-stack FreeRTOS worker、start gate、completion join、Audio Task Registry identity、create→register fence、retirement 结果以及 System Sleep PREPARE/ABORT 的 admission/restart fence。公共 host 只传递是否可重启、foreground/meeting/optional-pet 活跃事实、释放可选 asset client 与启动 wake-word 的 value action；不泄漏 ESP-IDF、FreeRTOS、HTTP、Wi-Fi/netif、JSON、allocator 或 board object。
- root 继续拥有物理 Audio Arbitration、Gateway asset client 和 startup sequence 的事实；启动 greeting 期间 one-shot recognizer 被拆除时以 value pending fact 记录，ready 后才消费并安排 worker。worker 保持原有 250 ms 初始等待、foreground/pet 等待、12 次失败重试和 backoff 行为。SAFE_MODE 同步关闭 admission 后进行 bounded stop；worker 仅在注销旧 immutable Registry identity 后发布 completion，注销失败或 join timeout 均保持 closed，ABORT 只重建 PREPARE 前存在的 generation。
- 新增 host contract/lifecycle/composition gate，更新 HAL、System Sleep 与 build-profile 门禁。`check-wake-restart-worker-service`、`check-deferred-setup-worker-service`、HAL boundary、System Sleep failure-closure 通过；Bread Compact 完整 Ninja link 通过，app `0x354200`，最小 app 分区余 `0x4be00`（8%）。这是源码级收口，非 COM3–COM6 HIL；A12、B3、C1/C7、runtime restart、其余 profile 链接与 Audio/Registry/sleep contention 实机验证仍未完成。

#### A12 wake-restart root 残留清理（2026-08-28）

- 已删除 `main.c` 中已迁移 offline wake worker 的整段 disabled legacy task/Registry/stop/prepare 实现，避免源码保留第二份不受编译保护的生命周期状态机；root 只保留 value-only host callback 和 service 调用。

## 29. 2026-08-28 B1/B3 后续：蜂窝 recovery 完成回调的生命周期闭锁

- `cellular_recovery_service` 现将“同步蜂窝 establish 返回成功”与“当前逻辑 Connectivity generation 仍被允许发布成功/重启 Gateway”分开处理。常驻 recovery task 在每次 ML307 establish 返回后重新读取 service admission；若 System Sleep PREPARE、Registry retirement 或终止路径已关闭该 generation，则不发布 `network_ready=true`、不重启 Gateway，并自然退出已关闭的循环。Wi-Fi GOT_IP 的 Gateway rearm 也复用同一 admission 条件，不能在 service 已 terminal-closed 后启动旧 Gateway generation。
- 物理 establish 的 cancel/drain、ML307/UART deinit 与 runtime restart 都没有因此新增；本切片只防止“已被上层围栏关闭的完成结果”越过 value-only recovery service。HAL 与 System Sleep 静态门禁已锁定该 post-operation recheck。真实 SIM、并发长请求、System Sleep 以及 COM3–COM6 HIL 仍为 B4/E2/C1 的开放验收，Light/Deep Sleep 继续 fail-closed。
- 随后补齐 `Gateway` rearm 的最后一段同步 handoff：recovery 在实际调用 root 的 Gateway-start callback 前取得私有 `rearm_inflight` reservation；System Sleep PREPARE 若已遇到该 handoff 会返回 `BUSY`，反之已关闭 PREPARE 的 generation 不能取得 reservation。该 reservation 只保护 value callback 的线性化，不构成 Gateway/runtime network restart，也不改变 ML307/UART 的物理生命周期承诺。

#### B1/B3 后续：选择切换后的终结蜂窝 drain（2026-08-28）

- `Connectivity Service` 将蜂窝 quiesce 区分为普通 physical lifecycle admission 与终结性 session drain。prepare/start/establish 仍严格要求当前选择为 cellular；但启动回滚或未来已完整接线的 root teardown 若在 durable selection 已切回 Wi-Fi 后，仍可在 service 或 profile physical-ready 证明旧 ML307 session 存在时进入一次 bounded quiesce。这样不会以“当前 Wi-Fi”错误跳过已存在的 modem/UART borrower；反之没有 cellular selection、published readiness 或 adapter readiness 的 Wi-Fi-only generation 仍返回 `BUSY`，不能意外触及 modem seam。
- 此变更不实现 runtime uplink switch、modem/UART deinit 或 restart；它只使终结性 rollback 的实际 session 事实优先于 presentation/durable selection hint。host regression、HAL boundary 和 Fangtang build 仍是源码证据，真实 SIM 的 teardown/concurrent request HIL 仍为 B4/E2 开放项。
- 再次通过 wake worker host gate、HAL boundary、System Sleep failure-closure、`git diff --check`，并完成 Bread Compact Ninja link：app `0x3541f0`，最小 app 分区余 `0x4be10`（8%）。这仍是源码/构建证据，COM3–COM6 的 recognizer restart、Registry contention 与 sleep rollback HIL 仍开放。

#### B3 后续：physical-root 创建前的 teardown bridge 闭锁（2026-08-28）

- `connectivity_network_root_owner` 现在要求先绑定完整 lifecycle host，才允许分配 ESP-NETIF/default-loop singleton 或初始化 Wi-Fi driver/application event routes。此前这两个私有入口可被未来 composition bridge 直接调用；若遗漏 host 绑定，会创建一代物理资源却没有关闭 callback admission、portal 与 SNTP 的合法 teardown 路径。现在此类错误在任何 SDK 分配前返回 `DEVICE_STATUS_UNAVAILABLE`。
- 这只是 future bridge 的 fail-closed allocation guard：既有 cold-start 仍先配置同一个 host 再初始化，尚未接入 production runtime restart，也没有实现 APSTA candidate confirm/post-save live transition、Wi-Fi/4G 独立重建、ML307/UART deinit 或 COM3–COM6 HIL。HAL static gate 锁定两个创建入口必须保留该 guard。

#### B3 后续：portal stop 完成事实的二次验证（2026-08-28）

- physical-root owner 的 `stop_provisioning()` 现在不再把 host callback 的 `OK` 当成充分证据；它会在 callback 返回后再次读取同一代 portal/restart 的 live-resource fact。若回调返回窗口出现了新的 portal 或 post-save generation，阶段返回 `BUSY`，后续 Wi-Fi/netif stop 不会开始。
- 这消除了 future restart bridge 将一个瞬时的“已停止”结果误当成稳定 stop 许可的窗口，但不改变现有 reset 型 post-save 策略，也不构成 runtime APSTA transition、runtime restart、ML307/UART deinit 或 COM3–COM6 HIL。HAL gate 已锁定该 post-stop recheck。

## 29. 2026-08-28 A9/A12 媒体传输与 wake-memory lease 收口

- 新增私有 `services/media_transfer_service.[ch]`，接管 server-audio 与可选宠物资源共用的大传输 lane、foreground audio priority、offline-wake memory lease 计数及“仅最后一个 lease 才安排 recognizer restart”的收敛规则。`main.c` 不再保存 `s_media_transfer_mutex`、`s_audio_media_download_active`、`s_media_wake_memory_lease_count` 或 `s_server_audio_wake_memory_lease_active`。
- composition root 仍是 physical owner：它经 value-only host action 执行 wake-word stop、startup pet preemption/rearm 与异步 wake restart；HTTP request、Gateway capability lease、SHA、allocator、renderer/cache install 与 Audio arbitration 均未移入 service。这样保留了“server audio 在取得 lane 前先抢占可选 startup pet”“可选 asset 在 server audio active 时不得新开 TLS body”“ACK 后才释放 server-audio lease”的既有顺序。
- 新增 `check-media-transfer-service.ps1` 与 host value-contract test，并接入 profile build gate。`check-media-transfer-service`、pet asset service、wake-restart worker、HAL boundary、System Sleep failure-closure 及 scoped `git diff --check` 通过；Bread Compact 已完成 main component archive build。完整 firmware link、其它 profile 重链，以及 COM3–COM6 的 server audio/pet preemption、TLS memory pressure、wake restart 和 sleep contention HIL 仍待执行；本项不构成 A9/A12、B3、C1/C7 或 Light/Deep Sleep 完成声明。

## 30. 2026-08-28 A12 启动 Welcome gate 收口

- 新增私有 `services/startup_welcome_service.[ch]`，接管 cold-start Welcome 的 handshake-queued fact、one-shot completion semaphore、gate/timeout/已消费状态以及 poll worker 启动失败后的 late-delivery 闭锁。`main.c` 不再持有 `s_startup_welcome_done`、`s_startup_welcome_gate_active`、`s_startup_welcome_timed_out`、`s_startup_welcome_consumed` 或 `s_handshake_startup_welcome_queued`。
- composition root 仍是实际业务 owner：它保留 boot-session correlation、Gateway JSON message 分类、音频播放、Wake/MultiNet 启动和整个 startup sequence；service 的 public host 只接收日志 value callback，不泄漏 ESP-IDF、FreeRTOS、HTTP、JSON、allocator、Wi-Fi/netif 或 board object。语义保持为：只有当前 boot session 的 Welcome 可控制 gate；播放完成后才释放；等待超时或 poll worker 创建失败后，当前 boot 的迟到/重投 Welcome 均静默 ACK，避免 MultiNet 已启动后重复播放。
- 新增 `check-startup-welcome-service.ps1` 与 host value-contract test，并接入 profile build gate。该项只证明源码级状态收口；尚未替代真实 Hub Welcome 的 ACK interrupted/redelivery、Wake 初始化失败、各板音频播放或 COM3–COM6 HIL，亦不构成 A12、B3、C1/C7、runtime restart 或 Light/Deep Sleep 完成声明。

## 31. 2026-08-28 A12 启动/SAFE_MODE 准入事实收口

- 新增私有 `services/startup_runtime_state_service.[ch]`，以原子 value-state 接管三项跨 callback 的启动事实：ordinary startup sequence 是否完成、晚到 Wi-Fi/IP callback 是否仍可启动 Gateway，以及 SAFE_MODE 是否已对本 boot 终结性关闭普通准入。`main.c` 不再保存 `s_startup_sequence_complete`、`s_gateway_startup_allowed`、`s_safe_mode_active` 或其专用 `s_task_state_lock`。
- composition root 继续拥有实际启动拓扑、Wi-Fi/4G、Wake、Gateway、SAFE_MODE quiesce 和 board transaction；service 只提供没有 SDK/RTOS/HTTP/JSON/allocator/board 类型的原子状态 API。SAFE_MODE transition 会以一个 compare-exchange 同时撤销 completed/gateway bits，之后普通 Gateway recovery 或 sequence completion 都无法重新开启；这保持原来的 fail-closed 语义并消除 root flag 的读写锁。
- 新增 `check-startup-runtime-state-service.ps1` 与 host state-machine regression，并接入 profile build gate。该项是 A12 的源码级状态收口，非运行时 network restart、SAFE_MODE bridge FI 或 COM3–COM6 HIL；C1/C7 与 Light/Deep Sleep 仍未完成。

## 32. 2026-08-28 A12 composition root 冗余状态清理

- `main.c` 已删除六项无读取方的 Configuration 镜像：output volume、display brightness、screen sleep 及各自 saved 标记。启动 snapshot、本地写入和 Hub reconcile 成功后此前都只写这些 root 缓存，却从未用于任何决策；Configuration 的 durable/effective/reconcile state 才是唯一权威，删除镜像避免失败事务后产生陈旧的第二事实源。
- `s_alarm_manager_started` 也已删除。`ensure_alarm_manager_started()` 现在先通过 `alarm_manager_is_initialized()` 查询 Alarm Service 的真实 lifecycle readiness，再调用其本已幂等的 init；startup rollback 不再手动维护重复 flag，因此 deinit/timeout 后不会让 composition root 与 Alarm Service 生命周期漂移。
- `s_startup_ui_initialized` 同样由新 value-only `app_ui_is_initialized()` 取代。初始化事实现在由 App UI model owner 在 replay synchronization 与 model 初始化完成后发布；root 仅在 degraded/SAFE_MODE terminal diagnostic 之前查询它，不能把这误读为 panel DMA completion 或 runtime hardware restart。
- `s_storage_mounted` 也已删除。Storage Service 的 fault-domain admission 已是 mount/unmount 的唯一可观测事实；pet cache、cache restore/runtime/startup mirror、meeting 与 Resource Pressure 现在直接查询 Storage Service 或现有 `device_storage_allows_optional_flash_work()`，故未知 unmount outcome 不会被 root 旧快照误判为可写 VFS。
- 新增 `check-main-composition-root-state.ps1` 并接入 profile build gate，锁定上述 root 缓存/flag 不回流，并验证 Alarm/UI/Storage facade 仍由各自 owner 的 readiness 支撑。本项只是 A12 的小型源码级清理；未实现 runtime restart、MCU Light/Deep Sleep，亦未替代四板 COM3–COM6 HIL。

#### A12 后续：staged provisioning boot evidence 收口（2026-08-28）

- `s_boot_provisioning_staged` 已从 `main.c` 删除，改由 `startup_runtime_state_service` 接管。Configuration 在 `load_boot_candidate()` 的 durable snapshot lock 内返回的 staged value 只能被 capture 一次；服务随后以只读 value API 供 Wi-Fi candidate fallback、Gateway confirmation deadline 和 no-uplink rollback 共同消费，禁止靠后续 Configuration bool re-query 重新判断。
- capture 在复制 runtime credentials、释放 boot snapshot 或调用 `begin_staged_provisioning_boot()` 前完成；捕获失败会 fail-closed。因而 Configuration 瞬态 admission/read 错误不会将未确认 candidate 静默降级为 confirmed；Gateway token promotion 后仍保留既有行为：仅 Gateway 自己的 `paired` local outcome 会停止 confirmation deadline，不把 durable mutation 的时序错误扩散到 root。
- 扩展 startup runtime host regression、composition-root 及 provisioning extraction 门禁，覆盖 immutable capture 和 root 不回流旧 flag；Configuration transaction、HAL boundary 与 `git diff --check` 已通过。该项为 A12/C4 candidate-evidence 源码收口，不代表 runtime APSTA transition、runtime restart、Light/Deep Sleep 或 COM3–COM6 staged-pairing/NVS/HIL 已完成。

#### A12 后续：boot-session correlation state 收口（2026-08-28）

- `s_boot_session_id` 已从 `main.c` 移入 `startup_runtime_state_service`。composition root 仍在 NVS/identity 初始化后用硬件随机数生成 32 位 hex boot correlation value，但服务只接受一次 capture；Gateway handshake/request 格式化与 Welcome `bootSessionId` 匹配改读取同一 immutable value，而非共享 root buffer。
- capture 使用一个 publishing fence：在字符串复制完成前不公开 session id；capture 后拒绝任何替换，故并发 Gateway/Welcome 观察不会读到半写值或被后来的启动路径换代。该值只用于本 boot 的 Hub correlation，不包含凭据，也不宣称为设备身份、认证材料或 runtime restart generation。
- startup runtime host regression 现覆盖未捕获、一次 capture、匹配/不匹配与拒绝替换；composition-root/HAL/Welcome/provisioning/System Sleep 门禁及 `git diff --check` 已通过。该项仍只是 A12 源码级状态收口；Gateway interrupted/redelivery、runtime restart、Light/Deep Sleep 和 COM3–COM6 HIL 均保持未完成。

#### A12 后续：Wi-Fi boot/runtime 配置状态收口（2026-08-28）

- 新增 value-only `services/wifi_runtime_configuration_service.[ch]`。它在 Configuration 的 boot-candidate snapshot 已验证、仍由 root 持有的短暂读取事务中只捕获一次 Wi-Fi 值副本；之后独占 cold-start saved-network fallback 所需的 active SSID/password，以及 portal 成功删除个人网络后的保存列表同步。`main.c` 不再保留 `s_wifi_ssid`、密码、enterprise 字段或已保存网络列表等十项镜像。
- Wi-Fi startup、Input Binding、Wi-Fi/IP event ambient/log、portal reuse/preferred scan 和删除回调都通过调用方栈上的 snapshot 或明确 mutation seam 使用该状态。portal 的 preferred SSID host contract 改为 copy-out，portal 不再借用另一 service 的内部指针；删除当前 personal network 会清空本 boot 的 active credentials，enterprise active credential 不受个人目录删除影响。
- 新增 host state regression 及 `check-wifi-runtime-configuration-service.ps1`，并接入 profile build gate；检查 immutable capture、fallback selection、personal/enterprise delete 差异、public header 边界和 root mirror 不回流。此项仅是 A12 源码级状态归属调整，不实现 runtime Wi-Fi restart、APSTA/portal 运行时重建、MCU Light/Deep Sleep 或 COM3–COM6 HIL；这些验收仍未完成。

#### A12 后续：SAFE_MODE coordinator root state 收口（2026-08-28）

- `safe_mode_coordinator` 现独占其 single-use、terminal SAFE_MODE transaction state；`main.c` 不再保存 `s_safe_mode_coordinator`。composition root 只在已证明的 late-boot entry 安装不可替换的 value-only host bridge，并提交 failure phase/status；同一 host 可幂等确认，替换 host 或完成/失败后的再进入均 fail-closed。
- 为保留一次进程内对每个失败阶段与 deadline 的独立回归，将完成路径和 failure matrix 分为两个 host process test。门禁覆盖 quiesce → clock/feedback → alarm → diagnostic 的顺序、每个阶段 terminal closure、deadline 闭锁、root 无 coordinator state 以及 header 的 HAL 边界。
- 此收口不扩大 SAFE_MODE 的进入点，也不实现 runtime connectivity restart、MCU Light/Deep Sleep 或 COM3–COM6 bridge/input HIL；现有真实硬件验收缺口保持开放。

#### B3/C7 后续：终结性网络重启围栏语义分离（2026-08-28）

- `gateway_lifecycle_service` 新增 `prepare_network_restart()`，供 SAFE_MODE 及未来 Connectivity restart bridge 在退役 Gateway/Meeting generation 前使用。它复用同一有界 cancel/join 顺序，但不再以 `prepare_system_sleep()` 的可逆事务名称表达终结性 fault-domain 操作；成功后唯一合法后继仍是 `commit_prepared_network_restart()`，该提交不会调用 ABORT 或复活旧 generation。
- SAFE_MODE bridge 已切换到该专用入口；host regression 分别覆盖 network-restart 的 terminal commit（零 ABORT）和真正 System Sleep PREPARE 的逆序 ABORT。此变更只澄清并锁定已有生命周期边界，**没有接入 production runtime trigger，也没有实现 APSTA candidate confirm/post-save live transition、Wi-Fi/4G 独立 bridge、board deinit/reinit 或 COM3–COM6 HIL**；B3/C7、C1/C2 仍未完成。

#### B3 后续：physical-root 停止事实与 deadline 对齐（2026-08-28）

- restart coordinator 的 `physical_root_stop_committed` 现在严格代表“已实际进入 physical-root stop bridge”，而非“即将尝试”。当 network-dependent 与 portal 已在父 deadline 内完成、但 deadline 恰好在 Wi-Fi/netif stop 前耗尽时，terminal snapshot 保持该事实为 false；上层不得将未触碰的物理 generation 误诊为已退役。
- host fault regression 新增该精确 deadline 边界，HAL gate 同步锁定标记必须在 timeout 检查之后、调用 bridge 之前才发布。它只提升 future restart/Safe recovery 的诊断与 fail-closed 判断准确性，**不构成 runtime restart production trigger 或 HIL 完成**。

#### B1/B3 后续：蜂窝 split lifecycle 准入闭锁（2026-08-28）

- `connectivity_service_prepare/start/quiesce_cellular_transport()` 与既有 establish 路径现在都要求当前 Connectivity logical generation 已初始化、未停止、未进入 System Sleep PREPARE，且 active uplink 明确为 cellular；Wi-Fi-selected 或已退役 generation 会在进入 profile-private ML307 seam 前返回 `BUSY`。quiesce 也只会在获得该准入后撤销 cellular readiness，避免跨 fault-domain 的误停。
- host regression 覆盖 Wi-Fi-ready generation 的三个 split API 均不触及 cellular platform counter，以及选择 cellular 后 prepare/start/quiesce 和 establish 的正常顺序；HAL gate 同步锁定四个入口的 initialized/cellular selection predicate。此为源码级 Wi-Fi/4G fault-domain 边界收口，**不等于真实 SIM 联网、runtime restart、APSTA post-save live transition 或 COM3–COM6 HIL 完成**。

#### B1/B3 后续：蜂窝请求与 Wi-Fi fault-domain 隔离（2026-08-28）

- 蜂窝 HTTP 与 stream request 不再复用仅检查 logical lifecycle 的通用 network admission；它们现在在同一临界区确认 active uplink 为 cellular 后才取得 shared drain reference。因而 Wi-Fi-selected generation 无法借 Device API 直达 profile-private ML307 request seam，System Sleep/deinit 也不能插入到 selection 检查与借用之间。
- 该 admission 的线性化点早于之后可能发生的配置切换；已被接受的同步请求仍由 shared request drain 约束直至返回，避免错误把它视为新 Wi-Fi generation 的工作。host regression 覆盖 Wi-Fi 模式下 HTTP/stream 的零 platform call 与 cellular 模式下正常转发，HAL gate 同步锁定。此项仍只是源码级 split 边界，**不等于 4G runtime restart、真实 SIM、APSTA transition 或 COM3–COM6 HIL 完成**。

#### B1/B3 后续：蜂窝取消的 in-flight 事实闭锁（2026-08-28）

- Connectivity 额外记录已获准的 cellular request 数量。foreground/owner cancellation 只有在该数量非零时才会进入 profile adapter；空闲、Wi-Fi-selected 或已完成蜂窝请求的 generation 不会因一个迟到 cancel 误触 ML307。反之，已获准的同步 request 在调用期间仍可被取消，即使配置随后已选择 Wi-Fi；它仍由共享 request drain 等待至退出。
- host regression 覆盖 idle/Wi-Fi 的零 cancel bridge call、同步 HTTP/stream 内 cancellation 的允许，以及请求返回并切换 Wi-Fi 后再次闭锁；HAL gate 检查 request count 与两个 cancellation entry 的同一边界。此为源码级 request ownership 收口，**不构成 modem deinit、4G runtime restart 或 SIM/HIL 完成**。

#### B1/B3 后续：uplink 切换后的蜂窝请求终止（2026-08-28）

- Gateway lifecycle、meeting stream、foreground command 与 interaction stop 不再以“当前 active uplink 是否 cellular”作为取消前置条件。已被 cellular admission 接受的 request 可以与配置切换并发；此时仍须通过 Device API 请求取消，实际是否存在可取消 ML307 borrower 由 Connectivity 的 in-flight request fact 决定。
- 这避免 fault-domain restart/worker retirement 在旧 cellular 请求尚未返回、但 policy 已切到 Wi-Fi 时漏掉终止，且 Wi-Fi-only、空闲或已完成 generation 仍在 service 内零 adapter call。Gateway lifecycle host regression 专门覆盖该切换窗口，HAL gate 禁止四个 caller 恢复 active-uplink guard。此项仍不构成完整 runtime restart、modem/UART deinit 或 SIM/HIL 完成。

#### B1/B3 后续：请求期间的 uplink hint 稳定性（2026-08-28）

- uplink selection admission 现在还要求 generic 与 cellular request borrower 均为零。选择会改变 profile-side transport hint，故不得与任何已获准同步网络请求并发；调用会 fail-closed 不变更选择，待请求退出后由上层按既有语义重试。System Sleep 同样继续等待其短选择 transaction drain。
- host regression 在 cellular HTTP seam 内尝试切到 Wi-Fi，确认当前 selection 与 adapter hint 保持 cellular，随后请求结束才允许切换；HAL gate 锁定 request count 是 selection admission 的前置条件。此为 runtime fault-domain 的源码级线性化补强，**不是 Wi-Fi/4G live switch、runtime restart、modem reinit 或 HIL 完成**。

#### B1/B3 后续：蜂窝物理 lifecycle operation 围栏（2026-08-28）

- Connectivity 现为 cellular prepare、start、quiesce 及组合 establish 保留独立的短期 physical-operation admission。该 admission 在同一临界区检查当前 logical generation、System Sleep fence 与 cellular selection；adapter 调用返回前，uplink selection 不得修改 profile-side transport hint，System Sleep PREPARE 与 deinit 也会等待该 operation drain 后才 park 或释放物理 generation。
- host regression 从 cellular platform seam 重入尝试切换 Wi-Fi，覆盖 split prepare/start/quiesce 和 establish 内部 prepare/start，确认调用期间选择及 hint 均保持 cellular，而调用结束后仍可按原有路径选择。HAL gate 同步要求四个 cellular lifecycle entry 使用同一 admission/release，且 selection、sleep、deinit 均观察该计数。此为源码级并发闭锁，**不等于 live Wi-Fi/4G switch、runtime restart、modem/UART reinit、真实 SIM 或 COM3–COM6 HIL 完成**。

#### B1/B3 后续：蜂窝 HTTP 与物理 lifecycle 互斥（2026-08-28）

- 进一步收紧 cellular request 与 physical lifecycle 的交叉边界：已获准的 cellular HTTP/stream 存在时，prepare/start/quiesce/establish 不会触及 profile adapter；已有 physical lifecycle operation 时，新的 cellular HTTP/stream 同样返回 `BUSY`。这避免 quiesce 或重新 establish 在同步 ML307 request 的 modem state 下并发执行。
- host regression 在 cellular HTTP platform seam 内重入 quiesce，确认返回 `BUSY`、不增加 quiesce adapter 调用，并保留正在执行 request 的 selection/cancel 语义。HAL gate 锁定双向计数前置条件。此为源码级 fail-closed 串行化，**不代表 HTTP abort、真实 modem deinit、live transport switch、SIM 联网或 COM3–COM6 HIL 已完成**。

#### B1/B3 后续：initial cellular establish 与 System Sleep 闭锁（2026-08-28）

- Cellular Recovery Service 现在将 cold-start 的同步 `establish_initial()` 标记为独立 in-flight lifecycle operation；该标记存在时，重复 recovery worker 启动与 System Sleep PREPARE 均拒绝进入。调用返回后才清除标记，并且仅在 logical service 仍初始化、admission 仍开放、未进入 System Sleep、未退休时发布 4G ready。
- HAL/System Sleep gate 同步要求该 operation 在 establish 前置位、所有退出路径清除，且 PREPARE 必须观察它。该收口避免 Power transaction 在 ML307 注册/探测仍运行时错误宣布 recovery domain 已 park。它仍是源码级并发约束，**不代表 MCU sleep commit、modem/UART deinit、runtime restart、真实 SIM 或 COM3–COM6 HIL 已完成**。

#### B3/C7 后续：网络依赖者的终结性 restart 围栏（2026-08-28）

- `wake_restart_worker_service`、`deferred_setup_worker_service` 与 `cellular_recovery_service` 现各自具备独立的 `prepare_network_restart()` / `commit_prepared_network_restart()`。它们和 System Sleep 共用有界 join/Registry retirement 机制，但 network-restart 分支没有 ABORT：prepare 关闭 admission，commit 只在旧 task 已退出且未处于 create/retire/physical establish/rearm handoff 时清除该次 fence，随后仍保持 terminal closed。因而旧 Wake、延迟 portal 或 cellular retry generation 不能在 Wi-Fi/netif physical root 已退役后借 System Sleep rollback 复活。
- composition root 新增尚未绑定 trigger 的 `quiesce_network_dependents_for_restart()` bridge，按单一 parent deadline 先终止 startup-pet worker/retry/cache，再 prepare Wake、deferred setup、cellular recovery 与 Gateway/Meeting，最后按依赖反向完成 terminal commit；任何失败立即返回且不调用 ABORT。SAFE_MODE 的已证明 pre-uplink bridge 同步纳入 cellular terminal prepare/commit，避免 SAFE_MODE 后 residual recovery admission 与 Gateway terminal state 不一致。
- `check-hal-boundaries`、SAFE_MODE、System Sleep failure-closure 与 restart coordinator host gate 已通过；Fangtang wrapper 在 30 秒窗口内再次通过 IDF activation 及上述前置 gates，但未取得 wrapper 最终 exit/link 结果。**这仍不是 production runtime restart：restart coordinator 未接 production trigger，cold-start Wi-Fi/4G/Gateway rearm 不能直接复用，ML307/UART 也没有 deinit/reinitialize 契约；APSTA post-save 继续整机 reset，真实 SIM 与 COM3–COM6 HIL 均未完成。**

#### A12 后续：启动宠物撤销代际的 renderer 闭锁（2026-08-28）

- `pet_asset_startup_service` 的无 descriptor（撤销旧宠物）分支现把捕获的 startup generation 传入 composition-root renderer callback；`pet_asset_apply_service` 在取得自身 display mutex **之后**才执行该 late-admission probe。这样旧 worker 即使先 snapshot、再等待一个新 runtime install，也只会以 `BUSY` 退出并完成自己的旧 generation，不能在 mutex 释放后清掉新 artwork。
- 同一 generation probe 也透传到 `pet_cache_service_clear()`：Flash worker 在删除 retained files 前重新取样。故撤销 transaction 等待 Flash admission 时被新 descriptor supersede，不会抹去新 descriptor 的 cache。host regression 新增“撤销 generation 在 clear 前被 supersede”的覆盖；`check-pet-asset-service`、HAL boundary、System Sleep failure-closure 和 scoped diff check 均通过，Fangtang ESP-IDF target objects（root/apply/cache/startup）也重新编译。该修补保持 HTTP cancellation、media lease、PSA SHA、allocator 与 renderer 的 physical owner 不变；**它只是 A12 的 startup transaction race closure，不代表 runtime restart、live Wi-Fi/4G switch、ML307/UART deinit/reinit、APSTA live transition、真实 SIM 或 COM3–COM6 HIL 完成。**

#### A12/B3 后续：Connectivity logical/physical lifecycle 编排收口（2026-08-28）

- 新增 value-only `services/connectivity_network_lifecycle_service.[ch]`。它接管 `main.c` 原本分散的 cold-start core/driver 与 terminal root-stop 编排：先建立 logical Connectivity、绑定不可替换的 physical lifecycle bridge、创建 ESP-NETIF/default-loop root、创建 Wi-Fi driver/routes；失败时以 physical → logical 的唯一顺序 terminal rollback。正常 stop 同样以单个单调 deadline 先停 physical root、再 deinit logical service，任何 physical failure 都不会在 live callback 下继续释放 logical generation。
- composition root 仍只保留具体 ESP-IDF callback、SNTP/provisioning stop 和 `wifi_event` bridge；SDK handle、Wi-Fi/netif、HTTP、RTOS 与 board 类型没有进入新 public contract。host regression 覆盖 init→Wi-Fi→physical-before-logical stop 与 retained partial root fail-closed；新 gate 已接入 profile build 链，并与 HAL boundary、Connectivity restart/service、System Sleep failure-closure 通过。Fangtang ESP-IDF target objects 重新配置/编译无误。
- 此项收口的是 B3/A12 已有 cold-start/rollback 所有权，**不启用 production runtime restart**：eight-stage coordinator 仍没有 trigger，APSTA post-save live transition、Wi‑Fi/4G separate rearm、ML307/UART deinit/reinit、真实 SIM 与 COM3–COM6 HIL 均保持未完成。

#### A12 后续：Provisioning QR SDK 私有化（2026-08-29）

- 新增 `services/provisioning_qr_service.[ch]`，将 ESP QR SDK callback、临时 module matrix 的分配/复制/释放，以及 128-byte 有界 Wi-Fi QR payload 从 `main.c` 收入私有适配器。公开 host contract 只接收 AP 字符串与语义 scene publisher；不暴露 QR SDK、renderer、allocator 或 RTOS 类型。
- composition root 仅注入 `scene_presenter_publish_setup_qr()` 和 fallback message publisher。原有临时 WPA AP SSID/passphrase 的 QR 表示、二维码编码失败后的设置提示保持不变；已核对当前 QR SDK 在 `esp_qrcode_generate()` 返回前同步执行 callback，因此临时 SSID 指针不会在 callback 后留存。另审计到该 SDK 默认会以 INFO 输出完整 encoder input，故 service 初始化会先将专属 `QRCODE` tag 固定为 `ESP_LOG_NONE`，避免临时 passphrase 泄露至串口日志。新增专用 value-contract/gate，并将根文件不得直接持有 QR SDK transaction 加入 HAL boundary gate；Fangtang ESP-IDF 的 `main.c` 与新 service target object 已编译通过。
- 此项是 A12 的 composition-root/SDK 边界收口，**不是 C4 TLS 或设备身份绑定配网、APSTA post-save live transition、runtime Connectivity restart、ML307/UART deinit/reinit、真实 SIM 或 COM3–COM6 HIL 完成**。

#### A8/A12 后续：下行服务器音频呈现策略私有化（2026-08-29）

- 新增 value-only `services/server_audio_presentation_service.[ch]`，将服务器下行音频的 MIME 白名单、缺失 MIME 时的 ID3/MPEG frame-sync 判别、MP3/WAV renderer 选择及播放失败的永久/可重试分类从 `main.c` 收入私有 service。Gateway Dispatcher 继续拥有消息顺序、capability lease、ACK 与 wake/media lease；composition root 仅注入既有 MP3 player 和 Audio Arbitration WAV renderer callback。
- 同时把下载失败与播放失败的 status 域拆开：前者仍由 Gateway Transport 的 ESP error classifier 判别，后者改用 service 的 `device_status_t` classifier。这样 `DEVICE_STATUS_BUSY`、`TIMEOUT`、`RESOURCE_EXHAUSTED` 等可重试播放结果不会因与 ESP error 数值偶然重叠而被错误 ACK 为永久无效内容。专用 host policy test、HAL boundary、media-transfer、Gateway capability gate 通过；Fangtang ESP-IDF 重新 configure 后 `main.c`、Gateway Dispatcher 和新 service object 均已成功编译。
- 此项只收口 A8/A12 的下行音频 format/presentation policy，**不改变物理 I2S/codec owner，不启用 Audio Arbitration 的 authoritative preemption，也不替代 Hub 音频下载/ACK interrupted、Wi-Fi/4G、COM3–COM6 或 sleep contention HIL。**

#### A9/A12 后续：宠物帧 SHA-256 完整性策略私有化（2026-08-29）

- 新增 value-only `services/pet_asset_integrity_service.[ch]`，统一下载帧与缓存恢复的“provider 计算 → 固定 32-byte digest → canonical hex 比较”顺序及状态语义。PSA provider 仍由 composition root 通过 callback 注入；service 不接触 PSA、allocator、HTTP、Display 或 RTOS 类型。
- 下载与缓存 restore host 现在共用该策略，digest mismatch/畸形输入保持 `DEVICE_STATUS_INVALID_ARGUMENT`（内容证据，不进入 transport retry），provider failure 原样保留。新增 host contract test、HAL gate 和 profile build gate；源码级验证通过。
- 该项仅消除 A9 启动宠物下载/恢复中的重复 PSA 比较路径，**不等于 HTTP cancellation、renderer startup transaction、runtime restart、真实 SIM、Light/Deep Sleep 或 COM3–COM6 HIL 完成。**

#### A2/A8 后续：命令取消文本事件的 Gateway Transport 收口（2026-08-29）

- `gateway_transport_send_text_event()` 现统一拥有 `/api/im-gateway/v1/incoming` 的文本事件 envelope、`replyTo`/`replyToMessageId` 关联字段、Bearer/Connectivity request admission 以及 HTTP 200 + `accepted=true` ACK 判定。`main.c` 的 command host 仅把 `/cancel` 与可选关联 ID 转交该 value-only API，不再直接构造 cJSON 或发起 HTTP 请求。
- 这保证取消控制消息与普通下行/上行 Gateway 请求共享 active-client registry、网络 fault-domain 围栏和错误语义；空文本、非 200、transport error 或 `accepted=false` 均 fail-closed 返回平台错误。新增 HAL boundary 结构检查并完成 Fangtang `main.c`/`gateway_transport.c` target object 编译。
- 该收口仍是源码级 transport ownership；没有宣称真实 Gateway ACK/cursor、断网重投、runtime restart、APSTA live transition、ML307/UART deinit/reinit、真实 SIM 或 COM3–COM6 HIL 完成。

#### A2/A8 后续：语音上传与提交 envelope 收口（2026-08-29）

- `gateway_transport_upload_voice()` 与 `gateway_transport_send_voice_event()` 现接管语音 `upload-url`、对象 PUT、incoming voice envelope、重试及 accepted/correlation 解析；Interaction Service host 只转发 WAV value buffer 与 media/event ID。`main.c` 不再拥有语音上传/提交的 cJSON、HTTP response 或 retry loop。
- 语音上传仍复用 Gateway Transport 的 Connectivity admission、active-client registry、Bearer 与取消语义，返回值保持平台错误整数；公共头仅暴露字节缓冲和有界字符串，不泄漏 cJSON/HTTP/RTOS 类型。HAL gate 与 Fangtang target object 编译已覆盖该迁移。
- 这是 A2/A8 的 transport ownership 源码收口，仍未完成真实 capture→upload→submit→processing→cancel trace、ACK/cursor 断网重投、音频抢占 HIL、runtime restart、真实 SIM 与 COM3–COM6 验收。

#### A8/A12 后续：下行媒体下载与释放所有权收口（2026-08-29）

- 新增 `gateway_transport_download_media()` 与 `gateway_transport_release_media()`，把服务器音频 URL 的 HTTP body 分配、容量限制、状态分类及 allocator 释放统一留在 Gateway Transport。Gateway Dispatcher 通过显式 `release_audio` value seam 归还缓冲，不再直接对 transport-owned memory 调用 libc `free()`。
- `main.c` 仅保留 media lease、宠物抢占和音频策略编排；实际 GET、响应缓冲及释放均回到 transport owner，避免 PSRAM/internal heap 混用或未来 transport allocator 变更时出现跨 owner 释放。
- 已通过 HAL boundary 与 Fangtang `gateway_transport.c`、`gateway_dispatcher.c`、`main.c` target object 编译；此项仍不代表真实音频下载/ACK interrupted、网络故障重投或多板 HIL 完成。

#### A8/A12 后续：Tool-result POST 传输车道收口（2026-08-29）

- 新增 `gateway_transport_post_json()` 及 200/202/204 value status mask，将客户端 Tool 执行结果的 HTTP POST、状态接受和 response buffer 释放归入 Gateway Transport。`main.c` 的 tool handler 继续拥有 cJSON 业务结果 envelope，但不再直接调用 request lane 或持有 HTTP response。
- 该接口保持 JSON 序列化与 Tool Registry 在业务/root 侧、HTTP admission/Bearer/allocator 在 Transport 侧的分界，避免 tool-result 绕过共享网络围栏。HAL boundary 与 Fangtang target object 编译已通过。
- 仍未宣称 Tool-result 断网 outbox、ACK/cursor 重投、runtime restart 或多板 HIL 已完成。

#### A8 后续：Gateway ACK 车道专用化（2026-08-29）

- 新增 `gateway_transport_ack_messages()`，将 `/api/im-gateway/v1/ack` 的 200/204 状态接受和 response 生命周期封装在 Gateway Transport；Gateway Dispatcher 只序列化 message ID/status payload 并调用 ACK value API，不再直接持有 ACK HTTP response。
- 该专用入口与 poll cursor 的“先业务处理、再 ACK、最后推进 cursor”顺序保持不变，继续 fail-closed 于 ACK 失败；本次只收口 physical transport ownership，未实现 durable outbox、断网 ACK 重投或 Hub interrupted/cursor HIL。

#### A4/A8 后续：Meeting endpoint HTTP/JSON 所有权收口（2026-08-29）

- 新增 `gateway_transport_create_meeting()`、`gateway_transport_get_meeting_status()` 与 `gateway_transport_post_meeting_action()`，把录音创建、状态查询、complete/process/delete 等 endpoint 的 URL 拼接、请求、HTTP 状态检查、响应解析和释放收归 Gateway Transport。Meeting Service 仍只通过 value-only host contract 使用 recording ID/status/action。
- `main.c` 删除原有 meeting endpoint 的 cJSON/HTTP helper，仅负责读取 Meeting Service 的 base path 并转发参数；分块 PUT streaming 仍由既有 Gateway Transport lane 负责。HAL gate 与 Fangtang `main.c`/`gateway_transport.c`/`meeting_service.c` 对象编译通过。
- 此项仍不代表断网续传、服务端 process/ACK 语义、多板录音 HIL、runtime restart、真实 4G/SIM 或 COM3–COM6 验收完成。

#### A9/A8 后续：宠物帧有界下载请求收口（2026-08-29）

- 新增 `gateway_transport_download_frame()`，将启动宠物帧 GET 的 response capacity、HTTP 状态输出、transport 缓冲 ownership 与释放归入 Gateway Transport。宠物下载 service/root 只提供 URL、期望长度和 value buffer，不再直接调用通用 request wrapper。
- 既有 startup admission、Gateway capability lease、media lane 与 PSA 完整性 provider 语义保持不变；该改动只消除 root 对 Gateway response container 的依赖，并由 HAL gate 检查 bounded frame download contract。
- Fangtang `gateway_transport.c`、`meeting_service.c`、`main.c` target objects 与宠物相关 host/gate 已通过。此项仍不代表启动宠物多板下载、断网重试或 renderer HIL 完成。

#### A8/A12 后续：main.c HTTP 适配层彻底去除（2026-08-29）

- 经过 Tool-result、语音、会议 endpoint、下行媒体与宠物帧下载迁移后，`main.c` 已不再包含 `esp_http_client.h`，也不再保留 `http_response_t`、通用 request wrapper 或 response-release wrapper；所有 Gateway HTTP 请求均经 Gateway Transport value contract。
- `main.c` 仍可构造业务 cJSON（Tool result、宠物/ambient/config handler），但不持有 HTTP client、response buffer 或 transport allocator，进一步固定 composition root 与 physical transport owner 的边界。
- HAL boundary 与 Fangtang `main.c`/`gateway_transport.c` target object 编译已通过。该清理仍是源码级 ownership 证据，不代表 Gateway outbox/断网重投、runtime restart 或 COM3–COM6 HIL 完成。

#### A8 后续：main.c Gateway request/response 依赖清零（2026-08-29）

- Gateway Dispatcher 现通过 `gateway_transport_ack_messages()` 提交 ACK；main.c 的宠物帧请求也通过 bounded `gateway_transport_download_frame()`。组合根不再直接引用 Gateway response type、request wrapper 或 SDK HTTP include。
- Fangtang 完整 Ninja 目标构建触发了当前工作树既有 profile 依赖缺失（`esp_lcd_nv3023.h`、`at_modem.h`），因此本轮仅以已成功的 main/transport/dispatcher target objects 与 HAL gate 作为证据，不能把完整链接宣称为通过。
### 2026-08-29 Gateway cursor durability checkpoint（A8 增量）

- `gateway_dispatcher` 启动时从 Persistence Service 读取 `gateway/out_cursor`，仅接受非负值；缺失默认 0，其它读取错误保持 fail-closed 并从 0 开始。
- 每次页面 ACK 成功后，先通过 `persistence_service_write_i64` 原子提交新游标，再更新 RAM 游标。持久化失败不会推进游标，下一轮会重放页面，避免断电/ACK 后跳过消息。
- Platform NVS 新增受锁串行化的 `int64` 写入；Dispatcher 仍不接触 NVS/句柄。Fangtang `gateway_dispatcher.c`、`persistence_service.c`、`platform_nvs.c` 对象编译通过，HAL/capability gate 通过。
- 该增量只提供本端游标 checkpoint；跨设备 durable outbox、ACK 断网重投、旧 Hub 兼容与 COM3–COM6 HIL 仍未完成。

### 2026-08-29 Fangtang profile dependency closure / full link

- 修复 `main/CMakeLists.txt` 对 Fangtang profile 的依赖闭包：显式加入 `esp_driver_uart`，并在组件注册后将 `78__esp_lcd_nv3023`、`78__esp-ml307`、`78__uart-uhci` 的 public/private include tree 注入 `main` target。这样兼容 ESP-IDF 6.0 的 numeric Component Manager 名称及 `at_modem.h` 对 `driver/uart.h` 的旧 include 路径；未修改 generated managed component checkout。
- Fangtang 已完成 clean reconfigure 与完整 Ninja 构建/link：`maclaw_esp32s3_client.bin` `0x34ea70`，最小 app 分区 `0x3a0000`，余 `0x51590`（9%）。
- 该构建证据仍仅是编译/链接，不等价于 ML307 实机、SIM 联网、休眠或 COM3–COM6 HIL。

### 2026-08-29 Configuration migration journal value contract（C6 增量）

- 新增 platform-neutral `configuration_migration_journal`，明确 `PREPARED → VALIDATED → COMMITTED` 单向迁移阶段、源 blob 尺寸、目标版本与非零 generation；非法/回退阶段 fail-closed。
- Configuration legacy migration 在读取旧版本后建立 journal，完成值校验后再写入当前 V7 store；写入失败保持旧记录不变，不向业务层暴露 NVS/句柄细节。Host regression 与 `check-configuration-migration-journal.ps1` 已接入 profile build 门禁。
- 当前 journal 仍是内存态证明，尚未将 journal record 本身持久化为独立 NVS key，也未完成断电注入、恢复出厂认证擦除与四板 HIL；C6 仍未完成。

#### C6 后续：独立 journal 落盘与启动恢复接入

- Configuration migration journal 现使用独立 `maclaw/configuration_migration_journal` Persistence key：迁移开始写入 `PREPARED`，验证后写入 `VALIDATED`，目标 store 成功发布后清理 journal。
- 启动读取 journal 时执行 ABI/字段/阶段校验；损坏记录、目标缺失或 `COMMITTED` 与目标记录不一致均保持 fail-closed，不静默覆盖旧配置或伪造新 generation。
- `check-configuration-migration-journal.ps1`、host regression、HAL boundary 及 Fangtang 完整 ESP-IDF 链接均通过（app `0x34ff40`，余量 `0x500c0` / 9%）。
- 仍未完成真实断电注入、跨版本旧 generation 恢复矩阵、认证恢复出厂擦除及四板 HIL；本项不宣称 C6 完成。

补充：启动恢复现在把 journal 的 `source_bytes` 与本次实际读取到的配置 blob 尺寸、`target_version` 与当前 Configuration schema 绑定；尺寸/schema 不匹配时保留 journal 并进入 fail-closed，而不是清除可能来自旧 generation 的证据。该约束已加入 migration gate，防止跨版本或目标记录替换窗口误判为可恢复。

#### C6 后续：迁移 journal 恢复判定与全量链接

- journal value contract 新增 reset-recovery 判定：`COMMITTED` 记录不可回退解释，`PREPARED/VALIDATED` 只能按失败闭锁处理；阶段回退、零 generation、非法尺寸与 ABI 均拒绝。
- Configuration migration 接入值校验后再 commit 的顺序；`check-configuration-migration-journal.ps1` 与 host regression 通过，Fangtang 完整 ESP-IDF configure/build/link 通过（app `0x34fd90`，余 `0x50270` / 9%）。
- 该项仍不等价于断电安全：journal 记录尚未独立落盘，尚缺断电注入、旧 generation 保留/恢复、认证恢复出厂擦除及四板 HIL。

#### C6 后续：journal 编解码与损坏记录拒绝

- 为独立落盘准备了固定大小、ABI 校验的 journal 编解码 API；恢复读取必须通过 `struct_size/abi/source_bytes/target_version/generation/stage` 全量验证，任何截断或篡改记录均拒绝。
- host regression 新增编码/解码成功、损坏字段拒绝覆盖；该值契约仍不直接暴露 NVS/Storage/RTOS。独立 journal key 的实际写入、断电注入和恢复出厂事务仍待下一阶段。

#### C6 后续：受认证恢复出厂的闭合擦除事务

- 新增 `configuration_factory_reset_policy.[ch]` 与 `services/factory_reset_service.[ch]`：恢复出厂请求必须匹配当前 ABI、显式确认、非零 generation、完整固定擦除集合，并由已认证的本地或 Hub 来源提出；请求不携带 namespace/key，不能升级为任意 NVS 擦除。
- 恢复事务以独立 `maclaw/factory_reset_journal` 持久化 `PREPARED → COMMITTED`。composition root 只提供固定 key inventory，覆盖 Configuration/旧标量、Alarm、Schedule、Fall replay、Meeting recovery、Gateway ACK/Tool-result outbox、Weather/Update metadata；每项擦除后都要验证为 absent。任一失败保留 journal 并 fail-closed，启动遇到未决 journal 不猜测、不自动重放；COMMITTED 仅在全量校验通过后清理。
- host policy/journal regression、HAL boundary 与 Fangtang 完整链接均已通过（app `0x3503b0`，最小 app 分区余 `0x4fc50` / 9%）。这仍不是断电注入、认证链真实设备验证或 C6 完成声明；恢复出厂入口当时尚未接入生产 UI/Hub tool，后续已由下方 Gateway tool 增量接入，四板 HIL 仍待补充。

#### C6 后续：恢复出厂生产工具与生命周期准入（2026-08-29）

- 新增 `factory_reset` Gateway tool descriptor 与执行入口；mutation 强制 `idempotencyKey`，参数必须包含 `explicitConfirmation=true` 与正整数 `generation`，擦除集合仍为编译期固定 allow-list。
- Service ABI 升至 V4，composition root 注入授权 epoch 校验、活动操作准入、setup 标记、可逆 prepare-abort 与最终 reboot 回调。Hub 请求只有在当前 capability projection 已观察且 generation/operational surface 精确匹配时才可执行；本地来源暂继续 fail-closed，待实体 UI 二次确认事实接入后开放。
- 事务先 bounded 检查 meeting/alarm-ring/setup portal 与 Gateway active requests，再写 `PREPARED` journal；清理 NVS、会议录音和 Pet cache 后验证，写 `COMMITTED`、请求 force-setup、清理 journal，最后才触发 reboot。启动恢复已前移到 Persistence 初始化之后、配置加载之前。
- 本轮继续将 prepare 接入单一绝对 deadline：先取消 Gateway 全部活动请求，再通过 Task Registry 有界停止 Interaction/Meeting，fence Configuration persistence，停止 Pet cache、startup-pet、retry、wake-restart 与 deferred-setup worker，并在擦除前复查活动状态。任一 join/retirement 超时均保留 journal、拒绝擦除；这仍不等价于真实断电或硬件 fault-domain HIL。
- `COMMITTED` 后先发布 `force_setup`；只有该 reset tool-result 成功 POST 或成功写入 durable outbox，才清理 journal 并触发 reboot。若发送和 outbox 同时失败，journal 保留且不静默重启；无关 tool-result 不能满足该交付门槛。
- `check-configuration-migration-journal.ps1`、HAL boundary、Fangtang 完整链接均通过（最新 app `0x3510b0`，最小 app 分区余 `0x4ef50` / 9%）。源码和 host 证据不替代真实认证链、断电注入及四板 HIL；C6/C7 仍未完成。

补充：恢复出厂事务在写入 `COMMITTED` 后先请求 `force_setup`，并仅在 Gateway tool-result POST 成功或结果已持久化到 durable outbox 后清理 journal/重启；发送与 outbox 同时失败时保留 journal 并禁止静默重启，启动恢复继续 fail-closed。

#### C6 后续：恢复出厂交付证据与启动恢复闭环（2026-08-29）

- 新增独立 `factory_reset_result_delivered` 持久标记：交付确认先落盘，再清理 `COMMITTED` journal，最后清理标记并重启；任一步失败均保留可恢复证据。
- 启动恢复区分“交付已确认”和“仍待交付”：前者只在固定擦除集合再次验证后完成清理；后者要求 tool-result outbox 队首确为 `factory_reset`，重新幂等写入 `force_setup` 并等待 Gateway replay，不因普通或空 outbox 静默重启。孤立标记会在无 journal 时安全清理。
- 该补强仍是源码/host 级证明，不替代真实断电窗口、认证链、跨版本恢复矩阵及 COM3–COM6 HIL；C6/C7 继续保持未完成声明。

#### C6 后续：恢复服务并发回归与 TOCTOU 围栏（2026-08-29）

- 增加 `test_factory_reset_service.c` host regression，覆盖 ABI V4 缺失 abort callback、prepare 失败回滚、擦除失败后 PREPARED 闭锁、POST/outbox 交付 gate，以及“只重启一次”契约。
- execute 在取得事务锁后再次验证授权 generation；reboot/recovery 的 Persistence 操作在释放 service mutex 后执行，避免锁顺序死锁和能力撤销竞态。
- 本项仍不替代真实断电注入、认证链、跨版本恢复矩阵及 COM3–COM6 HIL；C6/C7 继续保持未完成声明。

补充：新增 factory-reset service host regression 的锁顺序断言，验证所有 prepare/erase/verify/complete/reboot 回调均在 service mutex 释放后执行；journal 清理是关键证据，delivery marker 的尾部清理失败不会重新打开旧事务或造成重复重启。另对 durable journal 写入未知结果保持本 boot terminal fail-closed，必须由下次启动 recovery 处理。

#### C6 后续：恢复事务锁与可逆/终结性 fence 分离（2026-08-29）

- Factory Reset Service ABI 升至 V4，增加显式 `abort_prepare_for_reset` 生命周期回调，并以事务活动标志保护 execute/recover 的并发窗口。
- prepare/erase/recovery 回调不再持有 service mutex；可逆 PREPARE fence 只在尚未产生不确定擦除证据时回滚，PREPARED/COMMITTED 写入或擦除结果不确定时保持闭锁，交由启动恢复判定，避免旧 Persistence generation 被重新打开。
- 该项继续是源码、host regression 与对象编译级收口，不替代真实断电注入、跨版本恢复、认证链或四板 HIL；C6/C7 仍未完成。

补充：本轮继续将 recovery 的 Persistence 读写置于事务活动标志之外的 service mutex 区域，并扩展 host regression 覆盖 COMMITTED+delivery marker 清理、marker 尾部清理失败仍只重启一次、畸形 marker 拒绝及 prepare 回调内并发 recovery 拒绝。关键 journal 清理成功后，delivery marker 仅作幂等 orphan cleanup；journal 清理失败则保留 active fence，等待后续启动恢复。

#### C6 后续：迁移 journal 与 durable revision 绑定（2026-08-29）

- Configuration migration journal 的 `generation` 现于目标 V7 store 生成后、写入 `VALIDATED` 前绑定该 store 的 durable `revision`，并在启动恢复时重新读取目标记录校验 `journal.generation == target.revision`；即使目标 blob 尺寸和 schema 相同，来自旧 generation 的残留 journal 也不会被清理。
- 新增 value-only `configuration_migration_journal_set_generation()` 及 host regression，覆盖 checksum 更新与篡改拒绝；migration gate 同时锁定 generation/revision 比对。
- 这仍是源码/host 级闭锁，不替代真实断电注入、跨版本恢复矩阵、认证恢复出厂擦除及 COM3–COM6 HIL；C6/C7 继续保持未完成声明。

### 2026-08-29 C3 Battery Policy 连续样本与一次性 checkpoint 准入（源码增量）

- Battery Policy 现以连续低压样本确认 `PROTECT`，并保持恢复滞回；无效百分比 fail-closed，不更新策略或调用方快照。
- `PROTECT` generation 只允许一次应急 checkpoint admission，并以显式 complete(success) 关闭；失败不会自动重试或反复写 Flash，避免掉电时写放大。该接口仍由 composition root 注入真实 Persistence checkpoint，当前未绑定低压检测中断或 charger wake。

### 2026-08-29 C3 应急 checkpoint composition-root 接线（源码增量）

- Battery Policy 新增 value-only checkpoint callback 与有界执行入口；composition root 在 Battery Policy 初始化后注入回调。
- 回调通过 Configuration Service 在同一持久化锁下重新提交当前确认快照，使用现有 Persistence/NVS durable path 并推进 configuration revision；不触碰宠物缓存、天气、会议或其他可选写入。
- 该路径仍不是 brownout/ADC 校准或真实低压中断处理：`timeout_ms` 仅作为入口准入参数，底层 Configuration/Persistence 仍需后续统一剩余 deadline；四 profile charger wake、断电注入及电气 HIL 继续未完成，不能据此宣称 C3 完成。
- host Battery Policy regression、HAL/System Sleep gates 与 Fangtang 完整链接通过；这不是 ADC 校准、brownout 安全、charger wake 或 C3 完成声明，仍需 profile 电气参数、低压注入及四板 HIL。

### 2026-08-29 C3 Fangtang ADC calibration lifecycle closure（源码增量）

- Fangtang 电量采样现通过 ESP-IDF curve-fitting calibration 将 raw ADC 转换为 mV，再按电池电压区间映射百分比；校准方案不可用或转换失败时保持 telemetry invalid（fail-closed）。
- 初始化任一步骤失败，以及后台任务 semaphore/task 创建失败，均释放已创建的 calibration handle 与 ADC unit；正常停止后台任务后也按反向顺序销毁 calibration/ADC，避免重复初始化和资源泄漏。
- 电压阈值、1:1 分压假设、充电 GPIO 高电平极性仍未由原理图/实测确认；brownout/低压注入、charger wake、四板功耗与电气 HIL 继续未完成，不能据此宣称 C3 完成。

### 2026-08-29 C3 低电量音频峰值保护与采样失败闭锁（源码增量）

- Alarm Burst 仍使用统一 Audio Service/Platform Audio 路径，但会读取当前 Battery Policy：`NORMAL/CONSERVE/PROTECT` 分别使用 100%/65%/35% 的有界波形峰值，避免低电量时报警音频继续产生完整峰值负载；共享服务不接触 codec/I2S 细节。
- Fangtang 充电 GPIO 或 ADC 读取失败会立即使归一化 telemetry 失效并清空采样窗口，必须重新取得连续有效样本后才能恢复电量策略，避免沿用过期低压/充电状态。
- 背光动态限幅、brownout/低压中断、charger wake、阈值/分压/极性实测及四板电气 HIL 仍未完成；本增量不构成 C3 完成或 brownout safety 声明。
- 新增 `check-fangtang-battery-calibration.ps1` 并接入 profile wrapper，锁定 calibration API、ADC/calibration 反向释放、共享边界不泄漏 raw ADC，以及读取/转换失败的 telemetry 闭锁。

### 2026-08-29 C3 低压确认后的自动 checkpoint 触发（源码增量）

- Battery Policy 在连续样本首次确认进入 `PROTECT` 后，自动调用一次有界应急 checkpoint；checkpoint 在策略状态发布之后执行且不持有策略锁，避免 ADC/Display/Configuration 重入。
- checkpoint 失败仍保持该 `PROTECT` generation 的一次性预算，不会因 telemetry 轮询重复写入；离开并重新进入 `PROTECT` 才创建新预算。
- composition-root checkpoint 将单一入口预算拆分给 Persistence worker 的 mutex/queue/completion 三阶段，避免连续三个独立等待各自消耗完整 `timeout_ms`。
- 背光限幅和报警峰值继续通过已确认策略状态生效；这仍不是 brownout 中断/电气保护，低压注入、charger wake、阈值实测和四板 HIL 仍未完成。

### 2026-08-29 Gateway ACK durable outbox policy / host regression（A8 增量）

- ACK 断网重投继续采用单槽 `gateway/ack_outbox`（当前 poll `limit=1` 与协议匹配），并新增独立 value-only `gateway_ack_outbox_policy`。恢复前严格校验 NUL 终止、存储尺寸、容量上限及嵌入 NUL；畸形或超限记录 fail-closed，不读取新的 outgoing page。
- 新增 `check-gateway-ack-outbox.ps1` 与 host regression，覆盖有效记录、截断/超限/空值/嵌入 NUL 拒绝，以及 Dispatcher 启动 flush、失败阻塞新 page 的结构约束；已接入 `build-profile.cmd`，并纳入 main component 编译源。
- 该增量只提供 ACK outbox 的边界证明；Tool-result POST 仍无 durable outbox，ACK 断网重投、Hub interrupted/cursor 兼容及 COM3–COM6 HIL 仍未完成，不能据此宣称真实断网恢复或 Gateway 发布验收完成。

### 2026-08-29 Gateway Tool-result durable outbox 初步收口（A8）

- Tool 执行结果 POST 失败时，组合根将完整 JSON envelope 写入单槽 `gateway/tool_result_outbox`；下一次 Gateway poll 前优先 flush，成功后删除记录，flush 失败则阻止读取新的下行页面，避免结果丢失或继续产生有序副作用。
- 新增 value-only outbox policy、host regression 与 `check-gateway-tool-result-outbox.ps1`，并接入 profile build 门禁。记录容量上限为 64 KiB，异常/截断/超限数据 fail-closed。
- 当前仍为单槽实现：若多个 Tool-result 同时失败，后写记录会替换前一条，后续应升级为带序列号的持久队列/事务日志；真实断网、多结果并发、Hub 幂等兼容与 COM3–COM6 HIL 尚未完成。

### 2026-08-29 Tool-result outbox 有界持久队列升级（A8）

- `gateway/tool_result_outbox` 现采用长度前缀记录序列，可在一个 64 KiB blob 中追加多个 Tool-result；flush 每次只提交队首，成功后原子写回剩余队列，队列为空才删除 key。
- outgoing 页面重放继续依据队首结果的 `resultId/toolCallId` 跳过已成功提交的 Tool 执行，避免 ACK/cursor 重放造成重复业务副作用；损坏长度、记录截断、容量不足均 fail-closed 并暂停新页面读取。
- host regression 已覆盖 append/peek/pop 的顺序与容量契约，Fangtang 完整编译/链接通过（app `0x34fcf0`，最小 app 分区余 `0x50310` / 9%）。仍未完成真实多结果断网、Hub 幂等及 COM3–COM6 HIL。

### 2026-08-29 C3 应急 checkpoint 单调绝对 deadline 收口（源码增量）

- `configuration_persistence_worker_service` 新增 `submit_until()` value-only 入口；组合根以 `esp_timer_get_time()` 生成单一父 deadline，mutex、queue、completion 三阶段均动态计算剩余预算，不再把三个独立 timeout 相加。
- Battery Policy 的低压 checkpoint 继续保持一次性 generation 预算；deadline 到期、worker 忙或持久化失败均 fail-closed，未引入自动重试或额外 Flash 写放大。
- 新增 persistence-worker gate 检查该入口和 monotonic remaining-budget 计算；Fangtang 增量对象与完整 Ninja 链接均通过（app `0x352a60`，最小 app 分区余 `0x4d5a0` / 8%）。
- 该项仍不等价于 brownout safety：低压中断、charger wake、阈值/分压/极性实测、断电注入及 COM3–COM6 电气 HIL 继续未完成，C3 不作完成声明。

### 2026-08-29 C3 Platform Power 非法遥测值 fail-closed（源码增量）

- Compact/Round Platform Power bridge 不再把 profile adapter 返回的 `level_percent > 100` 静默夹到 100%；该值现在直接使 normalized telemetry unavailable，避免把校准或 adapter 配置错误伪装成满电并绕过 Battery Policy 的保护路径。
- `check-platform-power-fail-closed.ps1` 的 compact/round host matrix 新增非法百分比拒绝与合法值恢复断言；共享 Device/Platform API 仍只暴露 normalized value，不泄漏 ADC、分压或 GPIO 极性。
- 这只是源码/host 边界证明，不替代 Fangtang 阈值/分压/充电极性实测、brownout/低压注入、charger wake、断电注入及 COM3–COM6 电气 HIL；C3 仍未完成。

### 2026-08-29 C3 Battery Policy 保护态遥测丢失锁存（源码增量）

- 已确认进入 `PROTECT` 后，若下一次 ADC/充电状态读取暂时失败，Battery Policy 保留 `PROTECT` 及背光/高功耗限制，不再把“无遥测”误解释为 `NORMAL`。
- 遥测恢复后仍需重新取得有效样本才能退出保护；缺失样本会清除连续确认计数，避免单次恢复读数直接绕过滞回。
- 新增 Host 回归覆盖“保护态→provider failure→保护态保持”；该项仍不替代 brownout 中断、低压注入、阈值/分压/充电极性实测、charger wake 与四板电气 HIL，C3 继续未完成。

### 2026-08-29 C6 迁移 journal 编码拒绝修复（源码增量）

- `configuration_migration_journal_encode()` 现在先完整执行 journal 校验，再允许生成持久化字节；损坏的 generation、stage、ABI 或 checksum 不会被编码函数“重新计算 checksum”后伪装成可恢复记录。
- host migration-journal regression 新增 malformed encode rejection，保持恢复路径对未知/损坏记录 fail-closed。
- 该项仍仅是源码/host 证据，不替代真实断电注入、跨版本恢复矩阵、恢复出厂认证擦除及 COM3–COM6 HIL；C6/C7 继续未完成。

### 2026-08-29 C6 legacy scalar migration journal 补强（源码增量）

- 当旧版本仅剩散落的 NVS scalar keys、没有统一 configuration blob 时，首次聚合写入 V7 store 现在也先落 `PREPARED` journal，并使用明确的 sentinel source identity；目标 revision 在 `VALIDATED` 前绑定，随后才发布 V7 store 与 `COMMITTED` evidence。
- 启动恢复会区分该 sentinel，并在目标已出现时重新校验完整 V7 store 与 revision，再清理 journal；恢复窗口中的空目标、损坏目标或 generation 不匹配均保持 fail-closed。
- `check-configuration-migration-journal.ps1` 与 host regression 继续通过；该项仍不替代真实断电注入、跨版本矩阵、认证恢复出厂擦除及 COM3–COM6 HIL，C6 继续未完成。

### 2026-08-29 Gateway Tool-result outbox 边界加固（A8）

- Tool-result outbox value policy 现拒绝容量超过全局 64 KiB、队列长度/记录长度溢出及记录内容在终止 NUL 前嵌入 NUL 的情况；校验不再依赖 `strlen` 越界读取，损坏持久化 blob 保持 fail-closed。
- append 支持源与目标缓冲区相同的就地追加（使用重叠安全复制），host regression 新增嵌入 NUL 拒绝与 in-place append 顺序断言，`check-gateway-tool-result-outbox.ps1` 已通过并继续接入 profile build 门禁。
- 该增量仍不替代真实断网、多结果并发、Hub 幂等兼容及 COM3–COM6 HIL；A8 未完成声明保持不变。

### 2026-08-29 Gateway ACK outbox 长度校验加固（A8）

- ACK durable outbox policy 增加全局容量上限校验，并改用有界 `memchr` 检测嵌入 NUL；不再通过 `strlen` 读取未由 Persistence 声明的尾部内存。
- host regression 新增恶意超长 stored-size 拒绝断言，`check-gateway-ack-outbox.ps1` 与 Tool-result outbox gate 均通过。
- 该项仍不替代真实 ACK 断网重投、Hub cursor/幂等兼容及 COM3–COM6 HIL；A8 保持未完成声明。

### 2026-08-29 Gateway ACK outbox JSON 边界加固（A8）

- ACK durable outbox policy 现要求记录至少为完整 JSON object 形态（首字节 `{`、终止 NUL 前为 `}`），并继续执行全局容量与有界嵌入 NUL 校验；非 JSON envelope 在 transport replay 前即 fail-closed。
- host regression 新增非 JSON ACK envelope 拒绝断言；ACK 与 Tool-result outbox 门禁均通过。
- 该项仍不替代真实 ACK 断网重投、Hub cursor/幂等兼容及 COM3–COM6 HIL；A8 保持未完成声明。

### 2026-08-29 Gateway ACK 单槽覆盖保护（A8）

- ACK durable outbox 写入前现在读取并验证现有槽位；已有有效待交付 ACK 时返回 `DEVICE_STATUS_BUSY`，禁止后续失败路径覆盖旧 envelope。畸形现有记录返回 I/O 错误并保持 poll fail-closed，避免在未知擦除结果下丢失最早 ACK 证据。
- `check-gateway-ack-outbox.ps1` 与 Tool-result outbox gate 通过；Fangtang 完整构建仍保持通过（app `0x352ae0`，最小分区剩余约 8%）。
- 该项仍不替代真实 ACK 断网重投、Hub cursor/幂等兼容、多结果并发及 COM3–COM6 HIL；A8 保持未完成声明。

### 2026-08-29 Gateway outbox 内存压力与 ACK 覆盖保护（A8）

- Tool-result 失败队列的 64 KiB 双缓冲改由 PSRAM 承载，避免内部堆压力导致 transport failure 后无法持久化结果；Persistence 仍负责内部栈安全复制。
- ACK 单槽写入前验证已有记录：有效待交付记录返回 `BUSY`，畸形记录返回 I/O 错误，均不覆盖旧证据；Tool-result append/peek/pop 的重叠缓冲区行为也已覆盖。
- 相关 ACK/Tool-result gate、HAL boundary、System Sleep failure-closure 通过；Fangtang-4G 完整链接通过，app `0x352b40`，最小分区剩余约 `0x4d4c0`（8%）。
- 仍不替代真实断网重投、Hub cursor/幂等兼容、多结果并发及 COM3–COM6 HIL；A8 继续保持未完成声明。

### 2026-08-29 Gateway ACK outbox envelope schema 闭锁（A8）

- ACK durable outbox value policy 现执行无分配、边界受限的 envelope schema 校验：必须包含且仅包含非空 `clientId`、非空 `messageIds` 字符串数组及 `delivered`/`failed` status；重复或未知字段、空数组、非法 status、截断/控制字符均拒绝。
- 校验仍在 transport replay 前执行，异常记录保持 fail-closed；host regression 新增缺失 client、空 messageIds、非法 status 与未知字段断言，ACK/Tool-result outbox gate 通过。
- 该项仍不替代真实 ACK 断网重投、Hub cursor/幂等兼容、多结果并发及 COM3–COM6 HIL；A8 保持未完成声明。

### 2026-08-29 Gateway Tool-result envelope schema 闭锁（A8）

- Tool-result durable outbox value policy 现执行无分配、边界受限的 JSON envelope 校验：要求 `clientId`、`resultId`、`toolCallId`、`toolName`、`conversationId` 与 `status` 均为非空字符串，`status` 仅允许 `succeeded`/`failed`；其余结果字段（如 `result`、`error`、`idempotencyKey`）通过受限 JSON value walker 校验，拒绝重复关键字段、截断、控制字符和嵌入 NUL。
- 该校验在 outbox append、peek/replay 前执行，损坏记录保持 fail-closed；host regression 新增缺失字段和非法 status 覆盖，Tool-result outbox gate 通过。
- 该项仍不替代真实多结果断网恢复、Hub 幂等/旧版兼容、断电注入及 COM3–COM6 HIL；A8 保持未完成声明。

### 2026-08-29 Gateway Tool-result outbox 格式版本与序列围栏（A8）

- `gateway/tool_result_outbox` 队列 blob 现在带固定 magic/version 头；每条长度前缀记录另带非零单调序列号，恢复时校验头、记录边界、序列严格递增及 envelope schema。
- append/peek/pop 已统一使用新格式；空队列清除 key，旧的未版本化 blob、截断记录、序列回退或保留字段损坏均拒绝解释并保持 Gateway poll fail-closed。host regression 新增旧格式和非法序列拒绝。
- 该项仍不替代持久事务日志、断电窗口 recovery、真实多结果断网恢复、Hub 幂等/旧版兼容及 COM3–COM6 HIL；A8 保持未完成声明。

### 2026-08-29 Gateway Tool-result 旧格式升级与损坏格式拒绝（A8）

- 增加一次性 `gateway_tool_result_outbox_upgrade_legacy()` 值转换：仅将明确符合旧版长度前缀格式的记录转换为当前 magic/version + 序列格式，并由 Gateway flush 在任何 POST 前先持久化升级结果。
- 当前格式损坏、magic 冲突、输入输出重叠、容量不足或记录校验失败均不会降级解释；升级后仍重新执行队列校验，持久化失败保持 replay/poll fail-closed。host regression 覆盖升级成功、旧格式损坏、当前格式误升级和重叠缓冲拒绝。
- 该项仍不替代事务日志/断电注入、真实断网恢复、Hub 旧版兼容及 COM3–COM6 HIL；A8 保持未完成声明。

### 2026-08-29 Factory-reset recovery 旧格式 outbox 收口（A8/C7 边界）

- `factory_reset_verify_recovery_state()` 在 COMMITTED 恢复阶段与 Gateway flush 使用同一显式 legacy-upgrade 规则：先完整校验并转换旧版长度前缀队列，再持久化新版 magic/version + 序列队列，成功后才允许检查队首 `factory_reset` 结果。
- 当前格式损坏、magic 冲突、重叠/容量不足、记录 schema 非法或升级持久化失败均保持 fail-closed，不会因恢复路径绕过 Gateway flush 而误解旧 blob；升级写入仍不是事务日志或断电原子性证明。
- Fangtang 完整 Ninja 链接继续通过（app `0x3537e0`，最小 app 分区余 `0x4c820` / 8%）；真实断电注入、跨版本恢复矩阵及 COM3–COM6 HIL 仍未完成。

### 2026-08-29 Tool-result outbox 成功 POST 后 dequeue 失败闭锁（A8）

- Gateway flush 在 Tool-result POST 成功后，只有 `pop` 及剩余队列写回/空队列擦除均成功，才视为 durable dequeue；若内存分配、队列校验或持久化操作失败，不再误擦除原队首记录，而是返回失败并保留 replay 证据。
- 该修正避免“服务端已收到但本地 dequeue 未提交”窗口被错误清 key；重复 POST 仍依赖 `resultId/toolCallId` 幂等与 Hub 语义，尚未以真实断网/断电注入证明。
- `check-gateway-tool-result-outbox.ps1`、ACK outbox、HAL boundary、System Sleep failure-closure、main composition-root gates 均通过；A8/C6 的真实多结果断网恢复、Hub 兼容及 COM3–COM6 HIL 继续未完成。

### 2026-08-29 Tool-result outbox 参数与升级边界回归（A8）

- append 现在在任何队列解析前拒绝“非零长度但空指针”输入；legacy upgrade 同时检查所有中间偏移与全局 64 KiB 上限，防止尺寸运算在恶意 blob 下绕过容量围栏。
- Host regression 新增空队列指针和部分重叠转换缓冲区断言；相关 outbox、HAL、System Sleep 门禁保持通过。

### 2026-08-29 Gateway 凭据临时副本擦除围栏（C4/C5 边界）

- Gateway Transport 更新 bearer token、pairing code 时先对完整固定缓冲区执行 `mbedtls_platform_zeroize()`，再复制新值；成功配对提交后同时擦除已消费的 pair code，避免旧 secret 残留在静态数组尾部。
- Composition root 加载配置时对临时 URL/token/pair-code 副本及配置快照执行显式擦除；保存 token 的 persistence request 在提交返回后擦除，公共 Device/Transport header 不新增凭据或擦除 API。
- 这只是内存生命周期硬化，不构成 Secure Element、TLS 设备身份绑定、可信时间引导或 C5 完成声明；相关无线安全和四板 HIL 仍未完成。

补充：Gateway Transport 的 bearer Authorization 栈副本（普通请求、蜂窝请求及会议流）现在在 HTTP 客户端完成消费后显式 zeroize；pairing 请求 JSON 中的 pair code 与 startup worker 的 boot-local copy 也在请求/worker 退出路径擦除。该围栏只覆盖临时内存生命周期，不改变 HTTP header 的传输安全边界，也不构成 C5 或真实无线 HIL 完成声明。
> 2026-08-29 C5 Credential Service 最小生命周期切片：新增平台无关、value-only `services/credential_service.[ch]`，以单调 generation 作为凭据撤销围栏；Gateway Transport 的 token 设置、持久化成功更新与 copy-out 现经过该 service，旧 generation 在撤销或新一代开始后不可恢复读取，服务内存及替换路径显式擦除。新增 `check-credential-service.ps1` 与 host regression，并接入 profile build gate。该切片只证明源码级生命周期边界，不等同于完整 Credential Service、Secure Element、TLS 设备身份绑定、首次 TLS 可信时间引导、私有 EAP CA、真实熵故障注入或 COM3–COM6 HIL。
> 2026-08-29 C5 企业 EAP 服务器身份策略切片：新增平台无关 `wifi_enterprise_trust_policy.[ch]`，企业 Wi-Fi 现在要求 system CA 且必须提供规范 DNS server-domain（禁止短名、通配/路径/端口及非法 label）；Configuration candidate、Provisioning 表单与 ESP EAP owner 共享同一 fail-closed 校验，防止证书校验只验证 CA 而未绑定 RADIUS 服务器身份。新增 host/static gate 并接入 profile build gate。该切片不等同于私有 EAP CA、完整 TLS 设备身份绑定、Secure Element 或无线 HIL。
> 2026-08-29 C5 Gateway bearer generation 读取围栏：Transport 的 paired 判断、Bearer 构造及 Wi-Fi/蜂窝请求不再直接以旧的 `s_gateway_token` mirror 作为授权依据，统一通过 Credential Service generation 校验与 bounded copy-out；静态 gate 额外拒绝绕过路径，Fangtang-4G 完整构建通过。镜像仍仅用于兼容性诊断/提交后的短期同步，尚不构成 Secure Element、设备身份绑定、完整 Credential Service 或四板 HIL。
> 2026-08-29 C5 Credential Service 设备身份绑定切片：Gateway token 现与本 boot 派生的设备 ID 一起绑定；Credential Service copy-out 要求 generation 与 identity 同时匹配，设备 ID 变化、旧 generation 或撤销后的迟到读取均 fail-closed。Gateway Transport 的 paired/Bearer 路径已统一经过该绑定校验，host/static gates 与 Fangtang-4G 完整构建通过。该切片仍不等同于 Secure Element、硬件证明身份、mTLS 客户端证书或跨重启持久 generation 方案。
> 2026-08-29 A11 authoritative Alarm 抢占终态收口：在显式 authoritative 模式下，Audio Service 以当前 foreground session generation 发出 capture/playback stop 请求；capture/meeting 以 generation-scoped interruption fact 交给原 owner，在其正常释放 codec/I2S 与 power lease 后由 Command 显示“录音被闹钟中断”，Meeting 复用既有 finalize/checkpoint 路径保存已录制前缀。compact/round stream read 在进入阻塞读前检查 stop token，避免抢占请求在下一次 DMA 读取前被吞掉。300 ms 等待超时保持 fail-closed，未跨任务强拆物理 owner；默认 shadow mode 仍不改变行为。Host/static gate 与 Fangtang 完整链接通过，真实会议 gap、TTS/PCM 听感、四板抢占/恢复 HIL 仍待执行。

### 2026-08-30 C6 迁移阶段单步前进约束（源码增量）

- `configuration_migration_journal_transition()` 现要求阶段严格按 `PREPARED → VALIDATED → COMMITTED` 单步推进，禁止跳阶段生成无法证明的终态证据；malformed/篡改记录仍拒绝编码与恢复。
- Host migration-journal 回归新增 PREPARED 直接跳 COMMITTED 的拒绝断言；Waveshare 正式 profile 已完成 ESP-IDF 6.0.2 全量重链（app `0x4a35c0`，最小 app 分区余 `0x5ca40` / 7%）。
- 该增量仍仅为源码、Host 与构建证据，不替代真实断电注入、跨版本恢复矩阵、认证恢复出厂擦除及 COM3–COM6 HIL；C6/C7 继续保持未完成声明。

> 2026-08-30 B3/C7 Gateway Dispatcher/Transport deadline postcondition fence：Gateway poll 与 startup worker 的 System Sleep PREPARE 在停止子任务返回成功后再次读取同一单调父 deadline；若成功恰好耗尽预算，统一转换为 `TIMEOUT`，保持 `s_system_sleep_preparing` 闭锁并阻止后续 COMMIT。Gateway lifecycle restart-commit、System-Sleep failure-closure 与 HAL boundary gates 通过；真实 Wi-Fi/4G 故障域、worker rearm 及 COM3–COM6 HIL 仍未完成。

> 2026-08-30 C7 Power PREPARE participant postcondition fence：Power Service 为 DISPLAY_OFF scheduler、Audio、Command、Intent、Identity、Update、Fall Detection、Provisioning、Meeting Recovery、Weather、Configuration、Persistence、Ambient、Display、Alarm、Schedule、Wake Deadline、Connectivity、Battery Policy 及 profile Power PREPARE 统一增加父 deadline 成功后复核；迟到 `OK` 直接转换为 `TIMEOUT`，不进入后续 participant 或 COMMIT。System-Sleep failure-closure、Power fail-closed、Wake authorization 与 HAL boundary gates 通过；四 profile Light/Deep 仍 `verified_sources=0`，真实 RTC/ESP Sleep、电气故障注入与 COM3–COM6 HIL 未完成。
> 2026-08-30 A9/C7 startup pet asset composite deadline fence：`startup_pet_asset_sleep_service` 对 worker、retry timer、cache 三个子 PREPARE 在 callback 返回成功后复核同一父 deadline；迟到成功返回 `TIMEOUT`，保留组合 participant 的闭锁并交由 Power 逆序 ABORT。Host startup-pet sleep 回归、System-Sleep failure-closure 与 HAL boundary gates 通过；HTTP cancel、media lease、renderer install、真实下载故障域及 COM3–COM6 HIL 仍未完成。
> 2026-08-30 A9/C7 startup pet asset state deadline fence：组合 PREPARE 的 state admission callback 也纳入同一父 deadline 成功后复核；state callback 迟到成功时不再继续 worker/retry/cache 阶段。Host startup-pet sleep 回归新增覆盖，维持 fail-closed 与 Power 逆序 ABORT；真实下载/HTTP cancel、renderer install、media lease 及 COM3–COM6 HIL 仍未完成。
> 2026-08-30 A9/C7 startup pet asset cache-handoff generation fence：完整安装成功后，缓存后台交接现在再次验证同一 startup generation 仍被准入且 Gateway capability lease 仍有效；若安装期间 descriptor 或能力代际变化，不再把 cache frame ownership 移交给后台 worker。新增 startup pet asset Host 回归覆盖迟到撤销窗口；真实下载/HTTP cancel、renderer install、media lease 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/C7 composition-root Connectivity bridge postcondition fence：System Sleep 的 root-owned startup-pet、Clock Sync、Cellular Recovery、Wake Restart 与 Deferred Setup 多阶段 PREPARE，以及 terminal network-restart dependent quiesce/commit 阶段，现均在成功返回后复核同一父 deadline；迟到成功直接 `TIMEOUT`，不再误放行后续阶段。Connectivity/System-Sleep/HAL gates 通过；runtime restart、真实 Wi-Fi/4G 故障域与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 A9/C7 runtime pet asset media-lease fence：runtime pet asset 在 optional media admission 与 stale-cache 回收后重新校验 Gateway capability lease；并发 Connectivity 撤销时停止后续下载/缓存交接并释放全部 frame 与 media admission。`check-pet-asset-service.ps1` 及 runtime Host regression 通过；HTTP/PSA/renderer 真实故障域、断电窗口和 COM3–COM6 HIL 仍未完成。

> 2026-08-30 A9/C7 runtime pet asset install postcondition fence：renderer full-install 返回成功后再次校验 Gateway capability lease；若安装期间能力代际被撤销，runtime 事务返回 `BUSY` 且不把 cache mirror 交给后台 Storage worker，所有 frame/media admission 仍按终态路径释放。Host regression 与 pet-asset gate 通过；真实 renderer/DMA、HTTP cancel、断电及 COM3–COM6 HIL 仍未完成。

> 2026-08-30 A9/C7 startup pet asset install postcondition fence：startup renderer full-install 返回成功后同时复核 startup generation admission 与 Gateway capability lease；安装期间若 descriptor/能力代际撤销，事务返回 `BUSY`，不再向后台 Storage worker 转交 cache mirror，并保持 frame、generation 与 worker 收尾路径一致。Host regression 与 pet-asset gate 通过；真实 renderer/DMA、HTTP cancel、断电及 COM3–COM6 HIL 仍未完成。

> 2026-08-30 A9/C7 runtime pet asset install/cache-preparation fence：runtime 事务将 cache-mirror 准备与 renderer full-install 分成独立 ownership boundary，并在两者之间重新校验 Gateway lease；下载或 Storage 准备期间的并发撤销不会让过期 frame 集合进入 renderer。Host pet-asset gate 继续通过；真实 HTTP cancellation、renderer/DMA 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 A9/B3 runtime pet asset admission probe：runtime pet asset host contract 新增 value-only `transaction_admitted` probe；composition root 以当前 `PET_ASSET` operational capability 实现该 probe。media admission、cache reclaim、download、cache-mirror preparation 与 renderer install 各阶段均在同一事务中复核 admission/lease，System Sleep 或 capability withdrawal 时保持 `BUSY` 并释放资源。Host pet-asset、Connectivity、System-Sleep 与 HAL gates 通过；真实 HTTP cancellation、Wi-Fi/4G fault-domain 及 COM3–COM6 HIL 仍未完成。

> 2026-08-30 A9/B3 runtime pet asset admission wiring verification：composition root 已将 `PET_ASSET` operational capability 绑定到 runtime transaction admission probe；在 media/cache/download/install 各边界与 Gateway lease 一致复核。pet-asset、Connectivity、System-Sleep 与 HAL gates 均通过，真实 HTTP cancel、无线 fault-domain 与 COM3–COM6 HIL 继续保持未完成声明。

> 2026-08-30 B3/C7 restart coordinator stage-matrix regression：Host matrix 进一步对每个 quiesce、provisioning、physical-root、logical/physical init、uplink、clock 与 Gateway rearm 阶段注入“恰好耗尽父 deadline”的迟到成功；每个阶段均保持 terminal `FAILED/TIMEOUT`，不推进后续阶段，且 physical-root committed evidence 只在实际进入该 bridge 后发布。该回归仍不替代 runtime restart 的 production trigger、APSTA/ML307/Gateway rearm 物理事务与 COM3–COM6 HIL。

> 2026-08-30 A9/C7 runtime pet asset media-lease fence：runtime pet asset 在开启 optional media work 后、以及 stale-cache 回收完成后，分别重新校验 Gateway capability lease；并发 Connectivity 撤销或代际切换时，不再继续 cache 回收/HTTP 下载，始终释放 source/cache frame 与 media-work admission。`check-pet-asset-service.ps1` 及 runtime Host regression 通过；HTTP cancellation、PSA/renderer 真实故障域、Storage 断电窗口与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 Fangtang ML307/UART 生命周期收口：AtUart Shutdown 现先发布 shutdown flag，再停止 UHCI RX，并通过 event-group bit 显式唤醒 EventTask，随后等待 receive/event worker 完成；析构路径在超时后继续等待任务退出，避免释放 queue/event/UHCI 资源时发生 use-after-free。AtModem 保留并注销 URC callback 后再关闭 UART；Fangtang transport、Connectivity、Platform 与 legacy seam 提供 value-only deinit/reinitialize，但尚未绑定 production runtime-restart coordinator。ML307 lifecycle、Gateway asset cancellation、HAL boundary gates 与 Fangtang-4G ESP-IDF 6.0.2 完整链接通过（app `0x3561b0`，最小分区余 `0x49e50` / 8%）；真实 4G fault-domain、modem现场 HIL、runtime restart trigger/回滚及 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 URC malformed-input fence：AtUart URC 解析现在对空字段、未闭合字符串及无参数 `CME ERROR` 保持安全值解析；Ml307Http 对 `MHTTPURC`/`MHTTPCREATE` 先做最小参数个数校验，畸形 header/content/error 直接唤醒等待者并进入错误/EOF 状态，避免越界访问或把分包残片当成有效响应。新增 lifecycle gate 断言解析器 fail-closed；Fangtang-4G 增量链接通过。该源码/Host/build 证据仍不替代真实 ML307 UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL。

> 2026-08-30 B3/A9 ML307 URC typed-value/hex fence：AtUart 对 URC 数值使用有界 `strtol/strtod`，拒绝溢出及非十六进制字符；`DecodeHexAppend` 拒绝空指针、奇数长度与非法 nibble，并以 bool 结果交由 Ml307Http 与 Fangtang cellular transport 处理。MHTTP header/content 现在要求严格的 type/arity/非负累计长度，畸形 payload 清空响应并唤醒等待者，防止伪造 body offset 或迟到数据进入 SHA/安装。lifecycle gate 与 Fangtang-4G 链接通过；真实 UART 分包、HTTP abort、4G fault-domain 及 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 stream URC typed-value/hex fence：MQTT publish、ML307 TCP `rtcp` 与 UDP `rudp` 接收路径现验证 URC 类型/索引后才处理，并使用严格 hex decoder；畸形或奇数长度 payload 被丢弃，不触发上层消息回调。ML307 lifecycle gate、Gateway cancellation、HAL boundary 与 Fangtang-4G 链接通过；真实 MQTT/TCP/UDP 分包、UART fault-domain、4G abort 及 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 modem-state typed URC fence：通用 `CGSN/ICCID/COPS/CSQ/CEREG/CPIN` 与 ML307 `MIPCALL` 状态处理现验证 argument type/arity 后才更新 IMEI、运营商、信号、注册和 PDP ready 状态；畸形 URC 不再以默认整数/字符串污染网络代际或触发错误的 ready callback。ML307 lifecycle gate 与 Fangtang-4G 全量链接通过；真实 modem URC 注入、网络故障域与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 transport-protocol URC fence：MQTT/TCP/UDP 的 open/close/send/state/receive URC 现统一验证索引类型与有效 payload，再更新连接代际、触发 callback 或唤醒等待者；TCP/UDP/MQTT 接收数据统一经过严格 hex decoder，畸形数据丢弃并清理聚合 buffer。Lifecycle、Gateway cancellation、HAL gates 与 Fangtang-4G 全量链接通过（app `0x356700`，分区余 `0x49900` / 8%）；真实协议分包/断链故障域、4G abort 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 UART CRLF 边界收口：AtUart 对缺失 CRLF 的 `+MHTTPURC: "ind"` 仅接受精确 token；仅当后续字节明确为下一条 `+` URC 时在准确边界合成终止符，部分或异常 token 保持未解析，避免 UART 分包残片被伪装成有效响应。Lifecycle 静态 gate 与 Fangtang-4G ESP-IDF 6.0.2 全量构建通过（app `0x356f00`，最小 app 分区余 `0x49100` / 8%）；真实 UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL 仍未完成。

> 2026-08-30 B3/A9 ML307 HTTP cumulative/error fail-closed fence：`MHTTPURC` content 现在要求 modem `sum_len` 与已接收 body 加当前 chunk 精确相等，拒绝重复/跳跃 offset；`Read()` 与 `ReadAll()` 在 attributed error 或超时后清空缓冲并返回失败，不再以已排队 body/EOF 伪装成功。Lifecycle gate 与 Fangtang-4G ESP-IDF 6.0.2 全量构建通过（app `0x356f80`，最小 app 分区余 `0x49080` / 8%）；真实 UART 分包、HTTP abort、4G fault-domain 与 COM3–COM6 HIL 仍未完成。
