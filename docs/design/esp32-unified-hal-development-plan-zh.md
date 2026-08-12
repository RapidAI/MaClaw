> 2026-08-12 EchoEar-2ST 真实 QSPI transfer-fence timeout HIL（COM3，Phase 4 增量）：四个 profile 早已各自持有 completion-fence 实现，但 EchoEar 的 ST77916/QSPI path 尚无独立可复现实机验收。现新增非发布 `echoear-2st-fence-fi` wrapper：保留 EchoEar 私有 component closure、物理 board identity 与圆屏 layout，仅追加 `TEST_BUILD=y` / `DISPLAY_TRANSFER_FENCE_TIMEOUT_ONCE=y`；test seam 仍完全位于 profile adapter，不向 Device/Platform/HAL、Hub、HTTP 或输入暴露控制接口。全量 image（app `0x327470`，factory 分区余 `0x78b90` / 13%）已写入 COM3，esptool 写后 hash 与独立 `verify-flash` digest 均通过。启动日志依序确认双 framebuffer、修复后的 `92160-byte DMA PSRAM` ambient overlay、EchoEar audio/input/power/deadline/schedule 就绪，然后命中 `echoear_display: test: abandoning first transfer fence wait`，系统仍稳定进入 `BOOT_STATUS.ready=true`；35 秒窗口无 startup degraded、panic/assert/WDT/reset loop。该结果证明一次真实 QSPI color transfer 的等待被故意放弃时，EchoEar profile-private adapter 保留已有 source/fence ownership，不把迟到 callback 误消费为下一笔传输；它不是物理 scan-out/GRAM fence，也不代表 renderer 无限阻塞、panel/audio teardown 或 runtime restart 已验收。测试后 COM3 已立即写回 `build-unified-echoear` 正式 image，并再次 `verify-flash`；冷启动复查仍有 `ambient overlay ready: 92160 bytes in DMA PSRAM` 与 `BOOT_STATUS.ready=true`，无 degraded/panic/WDT。

> 2026-08-12 Display Service「普通请求执行中 STOP」HIL（COM5 Fangtang，Phase 4 增量）：补齐此前未验收的队列顺序边界。新增独立非发布 `fangtang-4g-display-busy-request-stop-fi` profile，只启用 `TEST_BUILD=y`、`DISPLAY_SERVICE_FAIL_AFTER_INIT=y` 和一次性 `REQUEST_DELAY_ONCE_MS=7000`；测试请求为 Display Service 私有队列中的静态 `SHOW_STARTUP` request。初版发现 delay seam 误作用于启动期间先到的普通 scene，导致等待测试请求超时；现限定为**该私有静态 request 本身**，不改变任何发布请求、HAL 或 profile panel/DMA 实现。COM5 完整重配/链接、app 写入 hash 校验通过；修正后串口顺序为 `test: delaying busy scene request for 7000 ms` / `test: busy scene request armed` → `forcing startup failure` → 约 6 s 后 `Display Service did not stop ... ESP_ERR_TIMEOUT` 和 `startup rollback stopped at Display Service; retaining downstream owners fail-closed` → `startup degraded`、`Returned from app_main()` → 约 1 s 后 `test: busy scene request released`、`display task stopped`，窗口内无 panic/assert/WDT/reset loop。证明 STOP 不能抢占已在 Display Task 执行的普通请求；主 rollback 严格消耗自身 deadline，timeout 后静态 STOP record/completion 保持被迟到 Task 安全持有。该测试不等同于真实 renderer/panel hang、panel/audio teardown、runtime restart 或物理 scan-out fence。测试后 COM5 已重新构建、写入并 `verify-flash` 正式 Fangtang image；冷启动重新取得 `BOOT_STATUS.ready=true`、`SERVICE_STATUS.ready=true`，无 degraded/panic/WDT。

> 2026-08-12 EchoEar-2ST startup failed 修复（COM3，HAL/profile 内存策略）：故障串口精确定位为 `LCD double buffer ready` 后 `cannot allocate 92160-byte ambient overlay`，Display Task 随即退出并以 `startup degraded ... reason=input service` 返回。根因不是业务层或 Display Service 生命周期：EchoEar profile adapter 将 360×128 的弧形文字 overlay 固定申请稀缺的 internal DMA RAM，而两个全屏 framebuffers 已占用 DMA PSRAM，启动期剩余内部连续块不足 90 KiB。现仅在 EchoEar profile-private hardware/layout adapter 中将该 overlay 改为 DMA-capable PSRAM，且 layout memory policy 同步标记为 PSRAM；共享 renderer、Device/Platform HAL 与圆屏显示语义均未新增硬件条件。COM3 已完整链接、写入和 `verify-flash` digest 校验；实机日志确认 `ambient overlay ready: 92160 bytes in DMA PSRAM`，之后 `BOOT_STATUS.ready=true`，没有 startup degraded/panic/WDT。该修复保留圆屏当前弧形显示，不将 ESP-IDF allocator capability 泄漏至业务层。

> 2026-08-12 Bread Compact compact-renderer stage-3 FI image 收口（COM4，Phase 4 增量）：计划中的 stage 3（profile peripheral adapter 成功后）此前缺少独立可复现 test identity。现新增非发布 `bread-compact-renderer-stage3-fi` profile 和专属 build directory/config，固定 `TEST_BUILD=y`、`COMPACT_RENDERER_FAILURE_INITIALIZATION=y`、`STAGE=3`，不改动既有 stage-1 regression profile。该 image 已在有效 ESP-IDF 6.0 / Python 3.12 环境完成完整链接（app `0x33a980`），并写入 COM4、esptool digest 校验通过；设备随后仍可经 esptool 正常连接/硬复位。COM4 当前 USB 串口在该板型的自动复位后未输出应用日志，无法把 flash digest 或 bootloader handshake 误报为 stage-3 runtime HIL；因此该项仅证明独立构建、配置和写入可复现，尚待取得 `test profile peripheral adapter` → ordered rollback → `startup degraded` → `Returned from app_main()` 的可引用运行证据。为不让试验固件滞留，已立即写回并校验正式 Bread image。后续必须先解决/规避 COM4 app UART capture，再完成 stage-3 HIL；该缺口不影响既有 stage 1/2/4/5 证据，也不代表已验证 panel/audio runtime teardown 或 restart。
> 2026-08-12 Fangtang-4G 真实 DMA transfer-fence timeout HIL（COM5，Phase 4 增量）：此前仅完成四 profile 的 profile-private completion-fence 代码收口，`fangtang-4g-fence-fi` 尚未完成实机验收。现用可复现的有效 ESP-IDF 6.0 / Python 3.12 toolchain 完整链接该独立非发布 image（app `0x3706b0`），其唯一 test setting 为 `MACLAW_DISPLAY_TRANSFER_FENCE_TIMEOUT_ONCE=y`；COM5 app 写入和 esptool digest 校验通过。启动 HIL 精确记录 NV3023 panel 与双缓冲就绪后 `test: abandoning first transfer fence wait`，紧接着共享 renderer 只收到正常的 `LCD full-frame transfer failed: ESP_ERR_TIMEOUT`，但系统继续进入 `BOOT_STATUS.ready=true`、Wi-Fi/Gateway/offline wake 和 `SERVICE_STATUS.ready=true`，采样窗口无 panic/Guru/assert/WDT/reset loop。该行为证明首笔**真实** color transaction 的等待被故意放弃时，profile-private adapter 不会把迟到 completion 当成下一笔 transfer fence，也没有因 DMA source ownership 处于 pending 而提前复用 shared framebuffer；后续正常 scene/亮度提交可在 callback 归还 ownership 后恢复。随后已完整重建、写回并校验正式 COM5 Fangtang image。此项是 controller DMA completion fence HIL，不代表 panel GRAM 已物理 scan-out，更不覆盖 renderer 无限阻塞、panel/audio teardown 或 runtime restart。
> 2026-08-12 Display Service 多 stopper 竞争 HIL（COM5 Fangtang，Phase 4 增量）：在既有 STOP timeout / late-exit 试验上，新增只在 test-build 编译的第二个静态 stopper worker；其延迟 10 ms 后只会加入已有的 terminal STOP，并以 9000 ms deadline 等待同一个 boot-lifetime completion，绝不暴露 runtime setter、Hub/HTTP/input 路径，也不会触及 profile-private panel、DMA 或 renderer。独立非发布 `fangtang-4g-display-stop-timeout-fi` profile 固定 `DISPLAY_SERVICE_FAIL_AFTER_INIT=y`、`STOP_DELAY_MS=7000`、`SECONDARY_STOP_DELAY_MS=10`、`SECONDARY_STOP_TIMEOUT_MS=9000`。COM5 写入与 `verify-flash` 通过；HIL 串口顺序明确为 `test: secondary stopper armed` → `forcing startup failure after Display Service publication` → `test: delaying terminal STOP for 7000 ms` → `test: secondary stopper joining terminal STOP` → 主 rollback 在约 6 s 后报 `ESP_ERR_TIMEOUT` 并 `retaining downstream owners fail-closed` → `Returned from app_main()` → 迟到 Display Task 输出 `test: delayed terminal STOP released`、`display task stopped` → 第二 stopper 输出 `result=0`。全过程无 assert/panic/WDT/reset loop，证明两个并发 lifecycle caller 只共享唯一 STOP record / completion：主调用方可按自身 deadline 超时，而较长 deadline 的加入者可在迟到退出后正常完成，且 timeout 不会错误释放仍被 Display Task 使用的对象。测试后 COM5 已恢复正式 Fangtang image，并再次取得 `BOOT_STATUS.ready=true`、`SERVICE_STATUS.ready=true`，无 degraded/panic/WDT。此项仍只覆盖受控 task-delay 的竞争，不等价于 renderer 实际卡死、panel/audio teardown、runtime restart，或 panel physical scan-out fence。
> 2026-08-12 Display Service STOP timeout / late-exit HIL（COM5 Fangtang，Phase 4 增量）：为覆盖“renderer/Display Task 已接受 STOP、但未能在父 rollback deadline 前退出”的关键生命周期边界，新增仅 test-build 生效的编译期 `MACLAW_DISPLAY_SERVICE_STOP_DELAY_MS`。它只在 Display Task 已消费 boot-lifetime STOP record 后延迟退出；不会开放 runtime setter/Hub/HTTP/input 路径，也不触碰 profile-private panel、DMA 或 renderer。独立非发布 `fangtang-4g-display-stop-timeout-fi` profile 固定 `DISPLAY_SERVICE_FAIL_AFTER_INIT=y`、`STOP_DELAY_MS=7000`，超过 6000 ms composition rollback deadline。完整链接（app `0x3705e0`），生成 config 明确只有该 Display failure/delay setting；COM5 flash 和独立 `verify-flash` digest 均通过。HIL 串口精确记录：`forcing startup failure` → `test: delaying terminal STOP for 7000 ms` → 约 6 s 后 `Display Service did not stop ... ESP_ERR_TIMEOUT` 与 `startup rollback stopped at Display Service; retaining downstream owners fail-closed` → `Returned from app_main()`，再约 1 s 后同一 Task 输出 `test: delayed terminal STOP released`、`display task stopped`。全程无 assert/panic/WDT/reset loop，证明 timeout 不会释放仍由 late Display Task 使用的 STOP request/completion，也不会提交第二个 STOP；下游 owner 按依赖关系保留。测试后 COM5 已恢复正式 Fangtang image，write/verify-flash 通过，冷启动再次取得 `BOOT_STATUS.ready=true`、`SERVICE_STATUS.ready=true` 且无 degraded/panic/WDT。此项只验证可控 task-delay 的 STOP timeout/late exit，不替代 renderer 实际卡死、多个并发 stopper、panel/audio teardown，或 DMA/scan-out physical completion fence HIL。
> 2026-08-12 Display Service 启动后 rollback HIL 收口（COM5 Fangtang，Phase 4 增量）：此前 `fangtang-4g-renderer-fi` 错把 Display Service fault 配置与 compact renderer stage-1 配置混在同一 defaults 中，导致 test identity 不可复现且不能证明实际 Display Task STOP。现将二者拆分：既有 `fangtang-4g-renderer-fi` 恢复为仅 compact renderer stage-1；新增明确非发布的 `fangtang-4g-display-service-fi` profile wrapper，复用 Fangtang 私有 component closure 与物理 board identity，只追加 `TEST_BUILD=y`、`DISPLAY_SERVICE_FAIL_AFTER_INIT=y`。新 image 完整链接（app `0x370500`），生成 sdkconfig 确认只启用 Display Service seam，COM5 app 写入后 hash 与独立 `verify-flash` digest 均匹配。`--reset-after-open` 串口 HIL 精确记录 `forcing startup failure after Display Service publication`，随后 `display task stopped`、command-cancel/volume worker 有序停止，最终 `startup degraded: phase=4 … reason=display service test injection` 并从 `app_main()` 返回；窗口内无 assert/panic/WDT/reset loop。这直接验证 registry-owned Display Task 在 UI admission 前的正常 STOP generation/rollback 路径。测试后 COM5 已恢复 `build-unified-fangtang` release app，写后 hash/`verify-flash` 通过，冷启动达到 `BOOT_STATUS.ready=true` 和 `SERVICE_STATUS.ready=true`，并完成 Wi-Fi/Gateway/offline-wake 正常路径。仍未覆盖 renderer 卡死导致 STOP timeout、timeout 后迟到退出、多 stopper 竞争、panel teardown，或 DMA/scan-out physical fence；本次 stop 成功仅表示 Display Task 返回，不能把它扩展为物理 scan-out 完成保证。
﻿> 2026-08-12 Compact renderer stage-4/5 HIL 与 COM4 release 回归（Phase 4 增量）：在 Bread Compact 的独立非发布 FI image 上继续覆盖 post-panel acquisition 后两个尚未实测边界。stage 4（scanner completion semaphore 创建后）全量链接通过，app  x33a5b0（余  x65a50 / 11%），COM4 写入校验成功；实机依序达到 LCD 双缓冲、input adapter、direct-I2S audio，随后精确命中 	est input completion semaphore，background/cancel/volume worker 均有序停止，最终 startup degraded: phase=4 … reason=input service 并从 pp_main 返回，无 loop。stage 5（scanner task 发布前）同样全量链接与写入校验通过，并精确命中 	est before input scanner task、走同一 fail-closed rollback；由于失败发生在 compact_input_adapter_start_scan_task() 之前，日志没有 scanner input service started，符合“不制造 synthetic live task”的边界。此前 stage 2 已 HIL；stage 3（profile peripheral 后）因测试构建目录反复重新配置遭遇外部 stale build/Windows toolchain 中断，本轮未取得可引用的 test boot，不能声称已验收。测试后 COM4 已写回正常 Bread release app，写入 hash 校验成功；17 秒实机启动确认 BOOT_STATUS.ready=true，Input/Power/Deadline 服务、Wi‑Fi/Gateway handshake、离线 wake listener 全部正常，无 panic/WDT/degraded。至此 Bread 覆盖 stage 1、2、4、5；尚待 stage 3，以及 Fangtang 的 stage 2–5 和 Display STOP-timeout/DMA fence 等独立生命周期验收。
> 2026-08-12 Compact renderer stage-2 HIL 与 COM5 release 恢复（Phase 4 增量）：先完成 Fangtang-4G 正常 release 镜像的全量构建、完整刷写及 COM5 串口验收；esptool 已返回 Hash of data verified，实机识别 angtang-4g-v1、NV3023 panel、双缓冲、Input/Power/Sleep Deadline 服务，随后 BOOT_STATUS.ready=true，Wi-Fi/Gateway 握手均正常。这恢复了先前 stage-1 test image 占用的 COM5。随后将 Bread 专用非发布 FI 构建目录改为 stage 2（audio adapter 完成后），全量链接生成 app  x33a520（最小 app 分区余  x65ae0 / 11%），刷入 COM4 后日志依序确认 input adapter ready、Bread Compact direct-I2S audio ready，再精确命中 	est audio adapter；background tasks、cancel worker、volume-persistence worker 依序停止，稳定进入 startup degraded: phase=4 … reason=input service 并返回 pp_main，无 reset loop。此项将 shared Compact renderer 的 post-panel retain/fail-closed HIL 从 stage 1 扩展至实际 audio acquisition 边界；stage 3–5、Fangtang 对应 audio 边界、runtime restart、Display STOP timeout/DMA fence 仍须单独验收。COM4 已在测试后重新写回正常 Bread release app（写后哈希校验成功）；串口 reset 在本轮采样窗口受 USB 串口复位竞争影响而未获得新的正常启动日志，故正常 release 运行证据仍以本轮前已记录的 COM4 HIL 为准，不能用这次写入本身替代。
> 2026-08-12 Fangtang renderer fault-injection HIL（COM5，Phase 4 增量）：使用独立 `fangtang-4g-renderer-fi` image 在 COM5 运行共享 `compact_renderer.c` 的 stage 1（input adapter 后）失败注入。独立 Fangtang profile 通过完整 link，app `0x370450`（最小 app 分区余 `0x2fbb0` / 5%）；其 component closure 保留 ML307/NV3023/UHCI 等 Fangtang 私有依赖，而 test defaults 仅增加 failure injection。完整刷写后实机日志确认 NV3023 panel 创建、Fangtang 的 `LCD double buffering ready`，随后精确命中 test input adapter fault；background tasks、cancel worker、volume-persistence worker 依序停止，并稳定进入 `startup degraded: phase=4 … reason=input service` 后返回 `app_main`，无 reset/loop。结合 COM4 的对应结果，shared Compact renderer 的 stage-1 post-panel retain/fail-closed 路径已在两种不同的矩形硬件 profile 上 HIL 覆盖；stage 2–5、运行期 restart 与正常 release image 仍需独立验证。

> 2026-08-12 COM4 renderer fault-injection HIL 与 optional-service rollback 修复（Bread Compact，Phase 4 增量）：为验证共享紧凑 renderer 的 post-panel fail-closed path，使用独立 `bread-compact-renderer-fi` image 在 COM4 执行 stage 1（input adapter 后失败）实机启动。首轮日志正确进入 `compact renderer initialization stopped after hardware init at test input adapter`，但 composition rollback 随后把未启动的/无 motion-sensor 的 Fall Detection Service 当作 timeout，过早阻断下游 teardown。现 Fall Detection deinit 对从未初始化的 optional service 直接 idempotent OK；对明确不具备 motion capability 而初始化为 unavailable、且未创建 worker/semaphore/mutation lock 的 generation 也直接关闭，不占用 parent rollback deadline；具备 motion capability 的正常 construction window 仍走既有锁与 join 路径。修复 image 再次完整构建（app `0x33a4d0`，余 11%）并刷 COM4；实机日志确认同一 test fault 后依次停止 board background、cancel worker、volume-persistence worker，随后稳定进入 `startup degraded: phase=4 … reason=input service` 并从 `app_main` 返回，没有 boot loop。这证明 stage 1 的硬件保留诊断/进程级 fail-closed 以及 unsupported optional service 的 rollback 语义；尚未覆盖 stage 2–5、Fangtang、正常 release boot 或 runtime restart。

> 2026-08-12 Waveshare 32-dot 字体与 Display Task 启动发布竞态修复（COM6，Phase 4/5 增量）：Waveshare 圆屏 profile 现使用 `font_cjk32.h` 的 32×32 内建常用字形及 `cjk32_cjk.bin` 的完整 CJK 回退字库；完整回退资源长度为 2,686,976 bytes，构建产物已确认将该长度嵌入 app，其他 profile 继续使用 24-dot 资源。COM6 首次刷入时暴露了一个与字体无关的 Display Service 启动竞态：`xTaskCreateStatic()` 可在 creator 返回前运行 child，但 queue/mutex 的全局句柄直到 task 创建后才发布，导致 Display Task 首次 `xQueueReceive(NULL, …)` 触发 FreeRTOS assert 并循环重启。现将静态 queue、submission mutex 与 STOP completion 在保持 initializer admission closed 的情况下先发布，再创建 task；没有 submitter 能在该窗口入队。修复后 Waveshare 完整 build 通过（app `0x481750`，factory app 分区余 `0x7e8b0` / 10%），完整 COM6 flash 后实机日志显示 `SERVICE_STATUS.ready=true`、CO5300 亮度设置成功、Display heartbeat 持续递增（frames 10→24）、宠物关键帧按 HAL 由 8 降为 2，且不再出现 `xQueueReceive` assert 或 boot loop。此项验证了本 boot generation 的 Display Task 发布顺序和 32-dot resource link；仍未替代 Display Task stop-timeout、DMA fence、字体视觉 A/B 或其他 board 的 HIL 验收。

> 2026-08-12 compact renderer 故障注入 test profile 收口（Bread Compact / Fangtang-4G，Phase 4 增量）：为避免上一项的 Kconfig seam 只能由本地手工拼接 defaults 而无法稳定复现，新增两个明确非发布的 profile wrapper：`bread-compact-renderer-fi` 与 `fangtang-4g-renderer-fi`。两者都复用对应物理 profile 的 board identity、component closure 与 defaults，只把专用 sdkconfig 写入独立 `build-test-*` 目录，并追加 `TEST_BUILD=y`、`COMPACT_RENDERER_FAILURE_INITIALIZATION=y` 与默认 stage 1；`tools/build-profile.cmd` 的别名在进入 CMake 前还原物理 `MACLAW_PROFILE`，因此不会引入错误锁文件或把 test profile 伪装为新硬件。Bread FI 已完成 reconfigure，生成的 sdkconfig/header 实际包含三项 fault setting，`compact_renderer.c` 与 test seam object、以及整个 `libmain.a` 已产出；`ninja -n all` 显示会继续正常完成 elf/bin/partition size path。Fangtang 亦有相同受控 defaults/wrapper，但本轮尚未配置或构建其独立 FI image，更未刷 COM4/COM5；所有 test artifacts 均不进入 normal build wrapper。

> 2026-08-12 共享 compact renderer 后面板失败注入补齐（Bread Compact / Fangtang-4G，Phase 4 增量）：继“半初始化回滚”审计后，补入只在 `CONFIG_MACLAW_TEST_BUILD` 下有效的编译期 fault seam，用以覆盖 display adapter 已成功取得 panel 之后的 shared renderer acquisition 边界：input、audio、profile peripheral、scanner completion semaphore、以及 scanner task 发布前。各 fault 统一走既有 `compact_renderer_fail_after_hardware_init()`：关闭 background-work admission、清除未发布 input callback、保留 panel/audio/LCD/renderer 作 boot-lifetime diagnostic surface；stage 5 特意放在创建 scanner 之前，避免 test seam 制造一个无法安全回收的 synthetic live task。新符号仅位于 provisioning test seam 和 shared renderer，Kconfig 明确其不是 Device/Platform/profile adapter API、没有 runtime setter/Hub/console/release path。Bread 与重新 profile configure 的 Fangtang 已对 `compact_renderer.c`、`provisioning_failure_injection.c` 进行 Xtensa `-fsyntax-only`；Fangtang 已重新产出包含此切片的 `libmain.a`。HAL boundary 与 diff check 通过。尚未进行 dedicated test-config boot 或 COM4/COM5 HIL，所以这只是可选择、源码级可编译的故障覆盖，不能宣称真实 audio/peripheral/scanner rollback 或 runtime restart 已验收。

> 2026-08-12 Display Task 生命周期/Registry 收口（全 profile，Phase 4 增量）：继续审计 Display Task 后发现，若只由 `main.c` 直接调用 `display_service_deinit()`，该 task 不会出现在统一 Task Registry，且 rollback 超时后 STOP request 与 completion semaphore 若在调用栈上，迟到 renderer 返回会访问已释放的 stack object。现 Display Service 在静态 task/queue 创建成功后，以 `TASK_REGISTRY_OWNER_BOARD` 注册唯一 `display_service` entry；startup rollback 统一经该 owner 的 parent-deadline `task_registry_stop_owner()` 关闭 admission/join，避免 composition root 特殊持有 task 语义。STOP request 与 completion 改为 boot-lifetime static storage，并由 generation one-shot `s_stop_enqueued` 管理：首次 stop timeout 后保留 closed task/queue/STOP record，后续 rollback 只等待同一 completion，不会再次向可能已退出或已复用的 handle/队列提交栈对象；task 真正退出后 registry entry 才由 Registry 成功路径移除。初始化若 Registry 登记失败会在任何 request 发布前删除刚创建的 static task，并 fail closed；snapshot 新增 `task_registered` 仅作内部诊断。它仍不回收 static queue/mutex、panel、framebuffer 或 renderer，不支持同 boot restart；停止成功仅证明 Display Task 已从 Platform Display 返回，不代表 panel DMA/scan-out fence。缓存 Bread Xtensa 对 `display_service.c`、`main.c`、`task_registry.c`、`device_api.c`、`app_ui.c` 的 `-fsyntax-only`、HAL boundary 与 diff check 需随本切片复跑；尚未做 registry mutex 占用、renderer 卡住/STOP timeout 后迟到退出、stack water mark、多 producer、full link 或 COM3–COM6 HIL，故不能宣称完整 Display 生命周期或异步显示验收。

> 2026-08-12 紧凑 renderer 半初始化回滚收口（Bread Compact / Fangtang-4G，Phase 4 增量）：审计共享 `compact_renderer.c::board_port_init()` 后发现，互斥锁、传输完成 semaphore 与 frame/staging buffer 在面板 adapter 返回失败时会残留；而在 panel 成功后，输入/音频/外设或 scanner 创建失败又不能安全地简单释放这些对象——诊断 Display Service 仍可能借用 panel/LCD 锁，且并不存在可重建 panel/audio 的 runtime deinit 契约。现初始化分为两条明确边界：**panel transaction 之前**只清理由当前调用栈私有、尚未向任何 task 发布的 renderer semaphore/framebuffer/staging state；profile-private display adapter 已负责反向释放自己的部分 panel/SPI/PWM acquisition。**panel transaction 成功之后**的失败改为关闭 background-work admission、清除尚未发布的 input callback，并保留 panel/audio/LCD 与 renderer state 供 boot-lifetime diagnostic surface 使用；再次 `board_port_init()` 直接返回 `ESP_ERR_INVALID_STATE`，不制造第二套锁、frame 或 scanner。普通成功路径、Input Service 的 scanner join、background worker 的 one-shot stop 和 Display Task registry 顺序不变；本项不是 board deinit/restart，也不宣称可回收成功初始化的 codec/panel/外设。Bread 与 Fangtang 均已由各自更新后的 `compile_commands.json` 对 `compact_renderer.c` 进行 Xtensa `-fsyntax-only`；Fangtang 的 profile reconfigure 现重新把 NV3023/ML307/UHCI 头目录传递给 `main`，其 shared renderer object 与 `libmain.a` 也已重新产出。HAL boundary 与本切片 diff check 通过。一次重新完整 Fangtang link 从无效 build log 触发了 1351 个目标的 rebuild，但外层命令超时前仍在编译基础 IDF targets；因此该新 object 证据不能误报为本切片完整链接或 COM4/COM5 HIL。后续需让 profile wrapper 完成两个镜像的全链路构建，并对显示 adapter 阶段、input/audio/peripheral/scanner 各失败点实施 fault injection，验证“early cleanup / hardware-retain fail-closed / second-init reject”三条路径。

> 2026-08-12 DISPLAY_OFF 纳入 Display Task 单一执行权（全 profile，Phase 4 增量）：复核 Power Service 的 idle deadline、物理/计划唤醒和 GUI 远程亮度唤醒后发现，普通 scene、亮度已经经 Display Task 串行，但 `Platform Power → board_port_enter_display_off()/wake_from_idle()/display_is_off()` 仍会从 Power worker 直接取得 board LCD mutex 并操作 panel/backlight。这会让 panel-visible transaction 与 Display Task 的 framebuffer/DMA render 具有两个执行者。现 Power Service 仍独占 deadline、lease、scene eligibility 的策略和 timer 原子性，但 Platform Power 改为仅桥接至 Display Service；Display Service 将 `enter DISPLAY_OFF`、`wake display` 和只读 `display is off` 建模为内部同步 request，再由同一个 Display Task 进入 Platform Display，Platform Display 才是三个 board-port 调用的唯一调用者。这样触屏/按键/计划/远程控制的唤醒语义、各 profile 私有的 controller/backlight electrical ordering 和原有 scene eligibility 均不变；Power Service 不获得 renderer/panel handle，公共 Device/Platform header 也未泄漏 SDK/RTOS 对象。该变更仍是同步 request handoff：Power worker 会等待 Display Task 返回，`display is off` 是 board 的即时观察而不是 scan-out/DMA fence；没有实现 Power/Display 双向异步状态机、timeout-aware Display submission、完整 MCU light/deep sleep、Display Task restart、full link 或 COM3–COM6 HIL。缓存 Bread Xtensa 对受影响的 `display_service.c`、`platform_display.c`、`platform_power.c` 与 `power_service.c` 的 `-fsyntax-only`、HAL boundary 和 diff check 需随本切片复跑；Waveshare 缓存依赖缺 `esp_lcd_touch.h` 时不得扩大为 COM6 编译/实机验收。

> 2026-08-12 动态字形提交载荷所有权收口（全 profile Display Service）：复查 Hub `glyphs` JSON 路径发现，`main.c` 在栈上解码 72 B 的 24×24 bitmap 后经 App UI → Device API → Display Service → Platform Display 交给 selected board cache；当前四个 board port 都会同步复制进自己的 bounded LRU cache，因此不存在当前实现下的悬空读取，但该临时 buffer 的 ownership 没有在中间边界明示，未来替换为 Display Task/队列时容易把 producer stack pointer 入队。现 `display_service_cache_glyph()` 在取得 submission owner 前复制到 service-local submission buffer，并在 App UI、Device API、Platform SPI 与 Board Port 上明确“borrowed source、返回后调用方可释放、Platform 必须在返回前消费/复制”的契约；Display Service 与所有语义 scene mutation 继续使用同一 submission serialization。该 service-local copy 只消除 producer-to-Platform 的直接借用，**不是**异步队列、per-entry owned record、过载策略、completion fence 或 renderer restart：未来 Display Task 必须为每个 glyph 建立有界自有 payload、明确排队失败/替换策略，并在呈现端消费或完成回调后释放，不能保留当前调用栈或单槽 scratch。动态 Hub glyph 仍为 24×24，即使 Waveshare 曲线环将它面积采样为 32-dot；profile-native 32-dot fallback 与业务 API 不变。缓存 Bread Xtensa 对 `display_service.c`、`app_ui.c`、`device_api.c`、`main.c` 的 `-fsyntax-only`、HAL boundary 与 diff check 需随本切片复跑；Waveshare 既有缓存无法定位 `esp_lcd_touch.h` 时，不得把该静态结果扩大为 COM6/full link/HIL。

> 2026-08-11 Startup rollback fail-closed 依赖链收口（全 profile composition root）：再审计 `startup_stop_local_workers()` 发现，除 portal/setup-restart 和离线 wake 外，大多数 stop/deinit 的失败仅记录 warning 后仍继续向下游释放依赖；例如 Input scanner 未 join 时仍可能停止 board background/Power，Connectivity worker 未 join 时仍可能释放 network core，Alarm/Schedule 未 quiesce 时仍可能停止 deadline/Persistence。这与 HAL 文档的“无法 join 的 task 所访问资源不得释放”的资源所有权与 fault-domain 隔离要求相矛盾。现 composition root 对每个有依赖边的失败路径统一终止事务：记录具体 blocked step，保留后续 owners/resources 和已关闭 admission，避免 timeout 被误当成已停止继续 teardown。正常成功路径、既有单一 parent deadline 以及只保留诊断 surface 的 degraded 策略不变；这仍不是完整 board deinit/restart。Bread `main.c` 缓存 Xtensa `-fsyntax-only`、HAL boundary 与 diff check 通过。尚未注入每个 stop failure 或测量 COM3–COM6 的真实资源借用；Waveshare 编译/HIL 状态也未由本项改变，因此不得宣称完整 rollback 或 fault-domain recovery 验收。

> 2026-08-11 Startup pet retry `esp_timer` callback 生命周期收口（全 profile shared optional-media path）：复核启动宠物容量不足后的 10 秒 one-shot retry 发现，`esp_timer_stop()` 仅停止 future alarm，不能证明已开始的 timer-service callback 已返回；且为避免删除 retained handle 与普通 re-arm 读取 handle 之间的 UAF，旧设计若只关闭 callback admission，仍可能让 late caller 在 rollback 后重新 `start_once()`。现 timer callback 取得极小 in-flight lease，只在 admission 开放时置 deferred retry flag，gateway poll worker 才在普通 PSRAM 栈中执行后续宠物工作；rollback 先关闭 admission/清 pending flag，再以同一 parent deadline stop timer 并 drain 已进入 callback。新 retained timer-arm mutex 串行 lifecycle stop 与 `ensure + esp_timer_start_once`，因此 stop 成功返回后不存在已经 schedule 的迟到 retry；timeout 时 admission 继续关闭、handle/semaphore 保留，fail-closed。timer object 故意保留至本 boot generation，而非在仍可能有 late optional caller 的情况下 delete；这是一项受限资源保留，不代表 timer 可 runtime restart 或完整 app shutdown。缓存 Bread Xtensa `main.c` `-fsyntax-only`、`tools/check-hal-boundaries.ps1` 和 diff check 通过。尚未做 timer callback 恰好与 rollback/re-arm 重叠、ESP timer service 阻塞、完整 link 或 COM3–COM6 HIL，故不能把此源码证据扩大为启动宠物重试、显示或全 lifecycle/restart 的运行时验收。

> 2026-08-11 Registry 自然退出 bookkeeping deadline 收口（ambient clock / gateway poll / SNTP monitor / volume persistence）：继续审计 `main.c` 的 Registry owner 后发现，ambient clock、Gateway long-poll、SNTP retry monitor 与 output-volume/display-brightness NVS worker 都会先发布 completion 并清 task handle、随后调用无 deadline 的 `task_registry_unregister()`；这既可让撤退中的 worker 无限卡在 Registry mutex，也会与 `task_registry_stop_owner()` 已成功 join 后的 entry removal 竞争。现自然退出只作 10 ms bounded unregister；取锁失败则保留 immutable entry，交给下一次 lifecycle owner 安全收口。四条对应 stop helper 均从统一 parent deadline 取得 completion join 余量，并在 join 成功后以同一余量调用 `task_registry_unregister_with_timeout()`；清理 semaphore 或 Registry 不能重新获得一整份 timeout。这样正常自然退出仍可腾出 entry 让后续 generation 创建，而 rollback 不会被 worker 的最后 bookkeeping 放大；余量耗尽时 task 已退出但 entry 保留，仍是 fail-closed。缓存 Bread Xtensa `main.c` `-fsyntax-only`、`tools/check-hal-boundaries.ps1` 与 diff check 通过。尚未注入 Registry mutex 长占用、poll TLS cancel/自然退出交叠、SNTP wait/NVS 写入阻塞、full link 或 COM3–COM6 HIL，故本项仅证明四项 worker 的源码级 Registry deadline 规则，不构成 Connectivity/Power/Storage restart 或完整 shutdown 验收。

> 2026-08-11 Registry 生命周期收口扩展（前景 interaction / cancel / meeting / wake restart / portal与蜂窝协调）：沿用同一 immutable task identity 和单父 deadline 规则，继续收口 foreground interaction、cancel coordinator、meeting upload、meeting capability refresh、meeting resume supervisor、offline wake restart、deferred setup、post-save setup restart、ML307 recovery、Gateway startup 等 worker。自然退出在完成 token 发布后只尝试 10 ms bounded Registry bookkeeping；stop path 则先关闭相应 admission/stop token，取消其已发布 HTTP request 或通知安全点，join 成功后以同一剩余预算移除登记 entry。这样 Registry mutex 长占用不会令 audio/portal/connectivity 的实际 worker 在退出末尾无限挂起，也不能在 task 已 join 后为 bookkeeping 重新放大 rollback budget；取锁超时仍保留 entry 和 closed generation，供后续 fail-closed lifecycle pass 处理。现 `main.c` 已无无界 `task_registry_unregister()` 调用。缓存 Bread Xtensa `main.c` `-fsyntax-only`、`tools/check-hal-boundaries.ps1` 与 diff check 通过。仍缺 Registry mutex/HTTP/TLS/NVS/ML307/portal 并发故障注入、完整 profile link 和 COM3–COM6 HIL，故该切片不构成完整 Connectivity/Audio/Provisioning restart、radio deinit 或全 rollback 验收。

> 2026-08-11 Shared wall-clock deadline timer-service callback drain（Alarm / Sleep Schedule）：`wake_deadline_service` 此前已对 client callback 建立 slot generation drain，但 `esp_timer` 自身回调仍可能在 `esp_timer_stop()` 后已取到 `s_task`，而 deinit 随后 delete timer、join dispatcher、回收 stopped semaphore；SDK 没有“callback 已返回”的 caller deadline contract。现 timer callback 具有独立 admission/in-flight lease：deinit 在关闭 public deadline admission 与 stop timer 后关闭 timer callback admission，以同一 lifecycle budget drain 已进入的极短 notify callback，之后才通知/join dispatcher 与 delete timer；callback 只在已获取 lease 时复制 task handle，leave 负责通知 drain。init failure 与成功 deinit 均严格回收/清空该 per-generation semaphore，timeout 保持 `s_stop_requested`/callback admission closed 且不释放 timer/task/semaphore，fail-closed。Alarm/Sleep 的回调 generation/slot ownership、wall-clock policy与业务行为不变。缓存 Bread 的 `wake_deadline_service.c` 使用既有 `power_service.c` Xtensa compile environment 替换源文件进行 `-fsyntax-only`、HAL boundary 和 diff check 通过；该缓存没有独立 wake-deadline compile entry。未做 timer callback 与 deinit 恰好交叠、dispatcher callback 阻塞、完整 link 或 COM3–COM6 HIL，故不得将本项表述为闹钟/定时休眠或完整 lifecycle restart 已验收。

> 2026-08-11 DISPLAY_OFF timer-service callback drain（Power Service）：复查 screen idle scheduler 确认其 timer callback 虽只通知普通 worker，但 `esp_timer_stop()` 后仍可能已持有 worker handle；旧 deinit 可能接着 delete timer、join worker并回收 worker completion，缺少对 timer-service callback 返回的显式证明。现 Power Service 为该 callback 增加独立 admission/in-flight lease 和 per-generation drain semaphore：deinit 设置 `s_stopping`、stop timer 后关闭 timer callback admission，以同一 parent deadline drain 已进入 callback，之后才取得 transition mutex、delete timer、通知并 join worker。final callback lease 在临界区内先发布 drain token 再置 zero，避免 deinit 看见 zero 并回收 semaphore 后 callback 再 give 的 UAF；init failure/successful deinit 都回收对应 semaphore，timeout 保持 service closed 与所有被 callback/worker 触碰对象原样保留。业务层仍仅通过 Power Service/Platform Power 请求 DISPLAY_OFF/wake，面板、DMA 和 profile adapter 行为未改变。缓存 Bread Xtensa 对 `power_service.c` 和借用同环境的 `wake_deadline_service.c` `-fsyntax-only`、HAL boundary与 diff check 通过。未做 timer callback、user wake、panel transition/deinit 的三方竞态故障注入、full link 或 COM3–COM6 HIL，不能将本项宣称为各设备定时熄屏/唤醒、Power restart 或 MCU sleep 已验收。

> 2026-08-11 Audio fault-domain rollback deadline 接入（Bread Compact / Fangtang-4G / EchoEar-2ST / Waveshare 1.75C）：继续审计 startup rollback 后确认，Task Registry 只管理 `main.c` 的 audio restart/meeting worker，selected board 的 MultiNet/I2S recognizer 并不在 Registry；因此即便 registry stop 结束，rollback 仍可在 recognizer 或其 deferred callback 存活时继续释放 Connectivity/Storage。现为 Device → Audio Service → Platform Audio → board-port 增加仅供 lifecycle 的 `wake_word_stop_with_timeout(remaining_ms)`，board port 以父级剩余 tick 轮询 recognizer/dispatcher drain，常规无参数 stop 保留已有 6 s UX policy。composition root 在 AUDIO registry owner 后立即以剩余总 budget 停止 board wake generation；timeout 则维持关闭 generation 并直接返回，不再向下游释放依赖。圆屏 dispatcher 的 pending/start/cancel/join 收口仍保持，紧凑板遵循同一 bounded recognizer stop contract；业务语音、codec/I2S、普通 portal/media 停止语义均未改变。EchoEar `board_port.c`、Bread `board_port_bread_compact.c`、`audio_service.c`、`platform_audio.c`、`device_api.c` 和 `main.c` 缓存 Xtensa `-fsyntax-only` 通过；Fangtang transition unit 同样通过（手工补入其缓存 compile command 遗漏的 NV3023 include）。HAL boundary/diff check 通过。Waveshare 缓存缺少 `esp_lcd_touch.h`，无法完成其对象检查；未做 full link、COM3–COM6 HIL 或 task/I2S 阻塞故障注入，故不构成完整 audio fault-domain restart 验收。

> 2026-08-11 圆屏离线唤醒 handoff 生命周期收口（EchoEar-2ST / Waveshare 1.75C）：审计 `board_port_stop_wake_word()` 后发现，命中唤醒词时 recognizer 会释放 MultiNet 后创建 deferred dispatch task；旧 stop 只等待 recognizer handle，dispatch task 既不属于 stop admission 也可在 stop 返回后进入应用回调并启动前景录音，违反 audio/wake fault-domain 的“先关闭 admission、drain callback、再将资源视为停止”约束。现将该 dispatcher 明确纳入同一 wake-word generation：创建窗口以 `starting` 标记封闭，stop 设置 cancel token 并等待 recognizer 与 dispatcher/pending handoff 一同 drain；dispatcher 在读取 callback 前检查 cancel，且保留自己的 published handle 直到 callback 返回。start 遇到前代 handoff 未排空则拒绝而不重用 callback/generation，timeout 时仍保留所有 live task 状态，fail-closed。普通唤醒仍仅在 MultiNet 释放后派发一次回调，业务输入与物理 I2S/codec 均未移动。EchoEar 缓存 Xtensa `board_port.c` `-fsyntax-only`、HAL boundary 与 diff check 通过；Waveshare 缓存依赖目录实际缺失 `esp_lcd_touch.h`，无法完成其同文件编译，故不能误报 COM6/full-link/HIL。后续仍需对 COM3/COM6 做命中唤醒与同时进 portal/rollback/前景会话的竞态故障注入，并完成完整 audio fault-domain restart，而不是将此 handoff 收口视作该目标完成。

> 2026-08-11 前景/启动关键 worker deadline 收口（meeting / voice interaction / gateway startup）：沿用同一审计规则复核会议录音上传、前景语音与 Gateway 启动三个 worker，发现它们也先等待最多 100ms 的 published HTTP client mutex、取消请求/发 start-gate 和 task notify、再给予 completion semaphore 完整 timeout。现三条 stop 从入口固定 parent deadline，active-client guard 只消耗 `min(100ms, remaining)`，meeting 的 Wi‑Fi/蜂窝 cancel 与 interaction 的 capture/operation/foreground cancel 保持原有非阻塞语义，最终 completion join 只取得余量。到期时 stop token 已关闭且 completion/worker-owned 资源仍保留，防止超时后释放 task、HTTP、audio、operation 或 power lease 所引用的对象。缓存 Bread Xtensa `main.c` `-fsyntax-only`、`tools/check-hal-boundaries.ps1` 和 diff check 通过。未做录音 read、TLS upload、foreground capture 或 gateway handshake 阻塞的故障注入，也尚无完整 link/COM3–COM6 HIL，故本条不构成音频、蜂窝、重启或全 rollback 验收。

> 2026-08-11 HTTP worker cooperative-stop deadline 收口（startup pet / gateway poll / meeting capability refresh）：继续枚举 `main.c` 中多阶段 stop helper，确认这三个 worker 均先尝试取得短期 active-client guard 取消 ESP HTTP 请求、再通知并等待 task completion；此前 guard 最多 100ms 后 completion 又可获得完整 `timeout_ms`，使 Registry 的 owner-wide deadline 被二次放大。现三者均从入口建立一个 deadline，guard 只取 `min(100ms, remaining)`，completion join 仅取剩余时间；到期则保留 completion semaphore、task/client publication 与已关闭的 stop admission，返回 timeout，绝不回收仍可能被 worker 使用的对象。保留“guard 取不到就仍通知 worker”的既有语义，因此不会因取消竞态放弃 cooperative exit。缓存 Bread Xtensa `main.c` `-fsyntax-only`、`tools/check-hal-boundaries.ps1` 和本切片 diff check 均通过。尚未在 Wi‑Fi TLS、蜂窝请求或 client mutex 人为占用时实施故障注入，亦未完成完整 profile link/COM3–COM6 HIL，故不能宣称端到端 rollback/restart 已验收。

> 2026-08-11 组合根内部事务 deadline 收口（SNTP / 音量持久化 worker）：审计 `main.c` 的 startup rollback 调用树后确认外层已逐步把剩余 timeout 传给 child，但两处 child 内部仍会重置预算。`stop_sntp_service()` 现创建自己的 parent deadline，仅将剩余时间交给 clock-sync monitor join，之后才调用 ESP-IDF 无 timeout 的 `esp_netif_sntp_deinit()`；因此该不可控边界不会在 monitor 已耗尽 budget 后才被进入。`stop_output_volume_persist_worker()` 现以单个 deadline 串联 request mutex、STOP message queue 与 internal-stack NVS worker completion；任一步耗尽会保留 stop admission 与仍可能被 worker 使用的 queue/semaphore，返回 timeout，不再使 Registry owner stop 叠加三份完整等待。缓存 Bread Xtensa `main.c` 的 `-fsyntax-only`、`tools/check-hal-boundaries.ps1` 和本切片 diff check 均通过。由于未做阻塞 mutex/queue/worker 的故障注入，且完整 profile link、COM3/COM4/COM5/COM6 HIL 未完成，这只证明源码级 budget propagation，不能宣称完整 rollback/restart 已完成。

> 2026-08-11 Connectivity / Configuration 生命周期串行化与 timeout 预算收口：进一步审计后发现 Connectivity 虽能 drain Wi‑Fi attempt EventGroup users，却没有 deinit coordinator，两个 teardown caller 可同时看到同一 EventGroup 并形成 delete/retry 竞态；`initialize()` 也可与一个 timeout 后仍 closed 的 generation 并发。现以 retained static coordinator 串行 initialize/deinit，deinit 从入口起只按一个 tick deadline 等待 coordinator 与 waiter drain；超时继续保持 EventGroup generation closed，后续 deinit 才能继续 drain/reclaim，initialize 不会替换它。Configuration deinit 也显式采用相同 deadline helper，为今后增加 drain/rollback 预留不重置 timeout 的边界。与此同时复核 Persistence worker：它原先在 worker 自行退出和 deinit 完成路径调用无 deadline 的 public registry unregister，会让受 registry 父 deadline 管理的 stop 在最后 bookkeeping 处重新无界阻塞；现该 entry 仅由 `task_registry_stop_owner()` 在 child stop 成功且仍有剩余预算时移除，worker 只发布 completion 后不再触碰 registry。缓存 Bread Xtensa 对 `connectivity_service.c`、`configuration_service.c`、`persistence_service.c` 完成 `-fsyntax-only`；`tools/check-hal-boundaries.ps1` 与本切片 diff check 通过。仍未完成完整 profile link、并发 timeout 故障注入或 COM3/COM4/COM5/COM6 HIL；Waveshare 缓存构建仍缺 `esp_lcd_touch.h`，不得把该静态证据扩大为 COM6 或完整 lifecycle/restart 验收。

> 2026-08-11 生命周期 timeout 预算收口（Power / Persistence / Resource Pressure）：复查同步服务后确认三处 `deinit(timeout_ms)` 仍会把同一个参数分别交给 coordinator lock、mutation/transition lock、admission drain、STOP queue 与 worker join；单个子步骤若耗尽预算，后续步骤又可各自等待完整 timeout，违背了 composition root 的单父 deadline，并会放大 rollback 时间。现三服务均在入口记录 FreeRTOS tick deadline，只把剩余 tick 传给每一次后续阻塞操作；耗尽后保持 admission closed、保留仍可能被 task/callback 访问的 queue/semaphore/static lock，并返回 timeout，而不释放资源或给予合成的最小等待。Power 的 `esp_timer_delete()` 仍是 future-callback boundary，随后 worker join 只消费余量；Persistence 的 routed caller drain、STOP send/worker join 也共用余量；Resource Pressure 的 coordinator 与 VFS-sampling lock 共用余量。缓存 Bread Xtensa 对 `power_service.c`、`persistence_service.c`、`resource_pressure_service.c` 完成 `-fsyntax-only`，`tools/check-hal-boundaries.ps1` 及本切片 diff check 通过。Waveshare 缓存 compile command 仍因 Component Manager 缺少可用 `esp_lcd_touch.h` 无法完成整文件对象验证，故本条不等于完整 link、COM3/COM4/COM5/COM6 HIL 或完整 lifecycle/restart 契约；后续需在任务占用 transition/NVS/VFS lock 时做超时故障注入，并确认后续 lifecycle pass 能完成保持 closed 的 generation。

> 2026-08-11 Fangtang product-identity renderer 文件级收口：上一切片已把状态页的行为选择收口到 profile hook；继续审计后，将共享 `board_port_bread_compact.c` 中仍受 `CONFIG_MACLAW_BOARD_FANGTANG_4G` 包围的 sugar/cube raster、RGB565+A8 alpha composition、activity glyph、WIFI/4G 小字及 Fangtang asset-size 定义，移动到 `boards/fangtang_4g/fangtang_identity_{art,network}_renderer.inc`，并仅由 Fangtang transition translation unit 在包含共享 renderer 后引入。共享源现不再保留 Fangtang 编译条件或 `fangtang_*` identity helper；共享层依然拥有 screen/frame 生命周期、通用 pixel primitive、remote-pet 管理、状态/联网事实及 Display API，而 profile 只实现这些事实在其产品显示上的视觉映射。该 include 方式是一个明确的低风险物理搬迁步骤，仍依赖 transition unit 包含 legacy renderer，故不表示 Fangtang 已不复用 shared primitive 或已完成完整 board-port 拆分。Bread 与 Fangtang 缓存 Xtensa `-fsyntax-only`、HAL boundary 和 diff 检查通过；完整 link/COM4/COM5 HIL 仍待 IDF 环境恢复。届时必须验证 Fangtang sugar alpha、cube 内建 fallback、WIFI/4G 字标、思考/听取/播放指示、宠物优先级、Display-off/wake 与 Bread 启动/待机没有退化。

> 2026-08-11 紧凑屏状态/状态消息产品身份私有化（Bread Compact / Fangtang-4G）：在启动艺术 seam 之后继续审计 `show_state_screen()` 与 `show_status()`，确认状态机、ambient/foreground 判定、背景色、frame begin/finish、LCD lock、remote-pet 优先级与宠物帧/错误恢复仍是共享 renderer 职责；但 Fangtang 的 sugar/cube fallback、4G/Wi‑Fi 物理标记、方形屏安全坐标、产品文案和状态图形仍由 `board_port_bread_compact.c` 的 `CONFIG_MACLAW_BOARD_FANGTANG_4G` 分支直接拥有。新增 selected private profile hooks `compact_profile_render_state_identity()`、`compact_profile_render_status_identity()`：Bread 明确返回 false 并保留现有共同 robot 画面，Fangtang transition profile 在共享帧已建立/填充背景后只组合既有 Fangtang identity，返回 true；共享层不再按 Fangtang 编译条件选择状态或 status 画面。业务状态、联网状态的来源、remote pet 下载/动画、显示休眠/唤醒、frame presentation 与触发 redraw 的策略均未下沉。剩余共享 `Fangtang` 条件只保留用于仍待移动的私有 helper 定义，不再决定状态页行为。Bread 与 Fangtang 缓存 Xtensa `-fsyntax-only`、`tools/check-hal-boundaries.ps1`、`git diff --check` 通过；不等于完整 link 或 COM4/COM5 HIL。环境恢复后应回归两板 idle/quiet、无宠物 sugar/cube fallback、安装宠物后的优先级、Wi‑Fi/4G mark、listening/thinking/speaking/alert/done、setup/ready status、状态抢占及 DISPLAY_OFF/wake。Fangtang transition unit 仍 include legacy renderer，且它的 identity pixel/raster helpers 尚未物理移出 shared source，因此不能将此 seam 误述为完整 Fangtang 物理拆分。

> 2026-08-11 紧凑屏启动艺术私有化（Bread Compact / Fangtang-4G）：审计 `lcd_startup_screen()` 后确认 LCD 就绪判断、全屏 asset 的字节/几何校验、DMA stripe 呈现、错误降级和启动时机属于共享 renderer；但 Bread 的 `_binary_bread_compact_splash_*` 符号以及 Fangtang 的 sugar RGB565/A8 符号、启动构图都仍泄露在 `board_port_bread_compact.c`。新增私有 `compact_startup_full_frame_t` 与 selected `compact_profile` seam：Bread display adapter 独占 splash linker symbol 并返回已声明几何的不可变 full-frame descriptor；Fangtang display adapter 明确无 full-frame asset，其 profile transition unit 独占 sugar asset symbol 并通过 `compact_profile_render_startup_art()` 复用原有 alpha-composed sugar/cube + 标题构图。共享启动 presenter 不再命名 Bread/Fangtang asset 或按板名分支，只先给予 profile 已组合启动艺术机会，随后校验并条带呈现统一 descriptor。原有显示、错误 fallback、启动等待 Welcome 后进入待机的语义未改。Bread 和 Fangtang 均使用各自缓存 Xtensa compile command 完成 `-fsyntax-only`；`tools/check-hal-boundaries.ps1` 与 `git diff --check` 通过。该证据不等于完整 link/COM4/COM5 HIL；IDF Python/CMake 恢复后须验证 Bread 启动 splash 字节/色彩及 Fangtang sugar/cube fallback、启动期间错误呈现、Welcome/待机交接与 DISPLAY_OFF/wake。Fangtang transition unit 仍以 include legacy shared renderer 的桥接方式存在，且 shared source 其余 Fangtang standby/status identity 条件尚未迁移，不能据此宣称完整物理拆分。

> 2026-08-11 圆屏文本回复阅读 surface 收口（EchoEar-2ST / Waveshare AMOLED 1.75C）：审计分页回复 renderer 后，确认分页计算、UTF-8 换行、页码、回复 ownership、手动翻页和 foreground interlock 是共享业务/renderer 职责；但标题栏、分隔线、页脚规则仍将 EchoEar 360px 常量硬编码在共享绘制中。将这些圆形 aperture 的物理安全弦区补入既有 `round_display_layout_t`：EchoEar 固定既有数值，Waveshare 描述 466px 的更宽且更低 reading header/footer。共享层仅消费 selected layout，不按板型或尺寸分支，文本行宽、页数和 input navigation 语义未改。HAL boundary 与本切片 diff 检查通过；完整 build/COM3/COM6 HIL 仍受本机 IDF Python/CMake 重配阻断。恢复后需验证两圆屏的中文/ASCII 混排分页、长标题、首末页环回、page indicator、录音/闹钟/二维码抢占和 DISPLAY_OFF/wake。
> 2026-08-11 圆屏配网二维码 geometry contract 收口（EchoEar-2ST / Waveshare AMOLED 1.75C）：审计配网页确认二维码 module 内容、quiet-zone 规则、最小 module 尺寸、foreground ownership、动画抑制、Wi-Fi ready 后释放 QR guard 与双缓冲原子呈现均属于共享流程；但可用 QR 方块、顶部坐标和 SSID 说明文字仍硬编码为 EchoEar 360px。新增私有 `round_qrcode_layout_t`，profile 仅声明可安全扫描的最大方块、QR 顶部、标题/SSID 文本与续行安全区；EchoEar 保持已验证的 204px/40px 布局，Waveshare 使用其 466px 圆屏可见区域内的 272px 方块及更低说明文字。共享层未获取任何面板/控制器事实，也不改变配网或输入业务。HAL boundary 与本切片 diff 检查通过；本机 IDF Python/CMake 重配问题仍使完整 build 和 COM3/COM6 HIL 未验证。环境恢复后须实测不同 module_count 下 QR quiet zone、手机扫码、长 SSID 两行、二维码期间动画抑制、配网成功回到待机及 DISPLAY_OFF/wake。
> 2026-08-11 圆屏图片回复 geometry contract 收口（EchoEar-2ST / Waveshare AMOLED 1.75C）：审计 `board_port_show_response_image()` 后确认输入像素校验、回复 scene ownership、无分页语义、等比缩放、caption 换行、双缓冲呈现和 Display-off interlock 都是共享业务/renderer 行为；但标题栏、图像可用框、caption 与返回提示仍由 EchoEar 360px 常量及通用 response layout 偶然决定。新增私有 `round_response_image_layout_t`，各圆屏 profile 只提供标题栏、可缩放图像矩形、caption 与 hint 的物理安全区；Waveshare 获得更大的中央图像空间，EchoEar 复用原有图像区并把返回提示移至第二行 caption 下方以避免重叠。没有把图片解码、交互、分页或业务状态带入 profile。HAL boundary 与本切片 diff 检查通过；本机 IDF Python/CMake 重配持续阻断完整 build，尚未刷写 COM3/COM6。恢复后需对两圆屏验证 1x1/64x64 图片、横竖比例、空/双行 caption、返回提示、回复/录音/闹钟抢占及 DISPLAY_OFF/wake。
> 2026-08-11 圆屏短状态消息 geometry contract 收口（EchoEar-2ST / Waveshare AMOLED 1.75C）：继续审计 `board_port_show_text()`，确认它的前景 ownership、状态消息生命周期、正文折行、双缓冲原子切换和错误处理都应保持共享，但头像、耳/眼/鼻/嘴、分隔线与两行正文/触屏提示的坐标仍硬编码为 EchoEar 360px。新增私有 `round_message_layout_t`，各圆屏 profile 仅声明头像和文字安全区；EchoEar 复用既有数值，Waveshare 使用居中下移的头像与 300px 正文列，避免大圆屏沿用左偏的小屏构图。业务层仍只经 Device/Display/Platform 调用 `show_text`，没有暴露板名、控制器或 input 实现。HAL boundary 与本切片 diff 检查通过；本机 IDF Python/CMake 重配仍阻断完整 build，未刷写 COM3/COM6。恢复后需验证两块圆屏的无标题默认值、长标题/两行正文裁切、消息抢占 response/recording 后的恢复、DISPLAY_OFF/wake 和 status avatar 视觉安全区。
> 2026-08-11 圆屏闹钟场景 geometry contract 收口（EchoEar-2ST / Waveshare AMOLED 1.75C）：审计确认 `AlarmManager -> App UI -> Device/Display/Platform API` 已维持单一闹钟业务与场景恢复链路，但共享圆屏 renderer 仍将 EchoEar 360px 的闹钟标题/分隔线、双铃轮廓、时钟/标签/attempt 提示位置直接写死，Waveshare 因而不能按其 466px 圆形可见弦区适配。新增私有 `round_alarm_layout_t`，每个圆屏 profile 仅描述标题/文字安全宽度及双铃画法的物理坐标与尺寸；EchoEar 保持原有几何，Waveshare 使用更大且更低的光学构图。共享层继续唯一拥有 active=false 后前景 replay、attempt/max_attempt 文案、时间字符串安全提取、120ms frame/sway、双缓冲 delta present、LCD lock 和错误恢复；没有把闹钟业务、音频或触屏意图下沉进 profile。HAL boundary 与本切片 diff 检查通过；IDF Python/CMake 重配问题仍阻止完整 build，尚未刷写 COM3/COM6。环境恢复后需要实测两圆屏的首帧/响铃 sway、长 label、attempt 变化、单击停止、场景抢占恢复以及 DISPLAY_OFF/wake。
> 2026-08-11 圆屏上传场景 geometry contract 收口（EchoEar-2ST / Waveshare AMOLED 1.75C）：继续审计共享圆屏 renderer，发现会议录音上传页仍把 EchoEar 的 360px 物理坐标、240px 行宽和 276px 进度条写在 `board_port.c`；这会让 466px AMOLED 被固定到小圆屏的视觉节奏。新增私有 `round_upload_layout_t`，由 EchoEar/Waveshare profile 供给顶部分隔线、标题/阶段文字、续行、进度条、百分比/字节数/断电提示的安全区；Waveshare 使用 466px 可见中央弦区的更宽进度条和更低文字位置，EchoEar 保留既有数值。共享层继续唯一拥有 upload foreground ownership、百分比防溢出计算、stage 文本、双缓冲完整呈现、LCD mutex 和失败日志，不通过屏幕尺寸或板名分支改变业务。HAL boundary 与本切片 diff 检查通过；本机 IDF Python/CMake 环境依然无法完成重配构建，尚未刷写 COM3/COM6。恢复后需验证两块圆屏的长阶段文本换行、0/100%/超大字节数、上传期间 pet/response interlock、DISPLAY_OFF/wake 后重绘和上传完成场景恢复。
> 2026-08-11 圆屏录音场景 geometry contract 收口（EchoEar-2ST / Waveshare AMOLED 1.75C）：审计发现共享 `board_port.c` 虽已通过 `round_profile_adapter.h` 选择显示、字体、输入和电源实现，但录音页仍把 EchoEar 的 360px 物理坐标（上下红线、指示方块、状态/标题/计时、24 柱波形、MIC 标签、delta 清除矩形、底部提示）直接写在共享 renderer 中；Waveshare 因而只能偶然继承该小圆屏安全区。新增私有 `round_recording_layout_t`，由 EchoEar 与 Waveshare profile 分别提供其圆形 aperture 的安全坐标、文字可用宽度与既有触屏提示文案；共享录音状态机、capture/meeting 语义、24 柱电平历史、色彩、LCD mutex、完整帧/差分帧与失败恢复保持唯一实现。Waveshare 466px layout 将录音波形/文字下移并扩展到可见弦区，EchoEar 保留原有 360px 数值；新增圆屏时只需新增 profile-private descriptor，不需修改业务或场景状态机。`tools/check-hal-boundaries.ps1` 与本切片 `git diff --check` 通过。正常 CMake/完整 link 仍因本机 IDF Python/CMake 环境无法重配而阻断；尝试用缓存 Waveshare compile command 进行对象级验证时，Windows 进程启动报 “The system cannot execute the specified program”，故本轮不将其记为编译通过，也未刷写 COM3/COM6。环境恢复后需对 COM3/COM6 验证命令录音、会议录音、暂停、波形 delta、DISPLAY_OFF/wake 和触屏单击停止的布局与行为。
> 2026-08-11 紧凑屏 upload/alarm scene layout 收口（Bread Compact / Fangtang-4G）：继续审计共享 renderer 后，将上传进度与闹钟响铃页残留的 Fangtang 高度条件分支拆成 `compact_upload_layout_t` 和 `compact_alarm_layout_t`。两类 profile descriptor 只声明标题、阶段、进度条、百分比、警告、闹钟时间/标签/尝试次数/停止提示的物理坐标和字号；共享代码继续唯一拥有上传百分比防溢出计算、闹钟 attempt 去重、scene ownership、颜色、文案语义、LCD lock 与 DISPLAY_OFF wake 流程。Bread 保持既有 240×320 布局，Fangtang 保持原 240×240 布局；新增紧凑屏只需添加 selected-profile layout，不需向共享业务 renderer 添加板名分支。Bread 与 Fangtang 缓存 Xtensa `-fsyntax-only` 检查通过（Fangtang 补 NV3023 include），本切片 diff check 通过；这不表示完整 profile link 或 COM4/COM5 HIL。环境恢复后应实测长会议上传阶段文字、0/100% 进度、闹钟含/不含 label、重复 ring attempt、闹钟抢占后场景恢复和 DISPLAY_OFF/wake。

> 2026-08-11 紧凑屏录音场景 layout contract 收口（Bread Compact / Fangtang-4G）：审计录音页后发现共享 renderer 虽已有 `compact_recording_layout_t`，却仍以 Fangtang 编译条件分叉状态文字坐标、标题坐标、说明文字坐标、字号和“停止/停止保存”物理可容纳文案；这会使新增 240px 类硬件必须修改业务场景实现。现将这些物理 safe-area 与文案容量事实全部写入各 profile 的 recording layout：共享录音/暂停状态机、计时、波形历史、音量、颜色、LCD lock 和标准行为未变，只读取 selected layout 描述并组合同一套 scene。Fangtang 保留 240×240 的简短“按激活键停止”，Bread 保留 240×320 的“按激活键停止保存”。Bread 与 Fangtang 缓存 Xtensa `-fsyntax-only` 检查通过（Fangtang 补 NV3023 include）；该检查不等价完整 link 或 COM4/COM5 HIL。环境恢复后仍需在两板验证录音、暂停、实时波形、会议录音和 DISPLAY_OFF/wake 后重绘，尤其确认较短方糖面板的说明文字无重叠。

> 2026-08-11 紧凑屏 Connectivity profile contract 收口（Bread Compact / Fangtang-4G）：复核上一轮 ML307 transport 迁移后，继续将遗留的“链路选择持久化、启动双击切换、蜂窝 Hub URL 兼容”从 Fangtang transition unit 移至同一份 private `compact_connectivity_adapter_*` contract。共享 `board_port_bread_compact.c` 现在只转发 prepare/start/ready/quiesce、HTTP/stream/cancel，以及 load/apply selection、Gateway URL adaptation；Bread adapter 对蜂窝与选择接口保持明确 no-op / unavailable 语义。Fangtang adapter 独占 ML307 GPIO/UART/APN、legacy `network/type` 一次性迁移、Configuration Service normalized selection 写入和仅限官方 Hub origin 的 TLS 兼容 HTTP rewrite；输入 adapter 仍只产生启动窗口的物理意图，业务层仍由 Connectivity Service 决定 selected uplink 与 ready fail-closed 状态。由此删除 `MACLAW_FANGTANG_EXTERNAL_CONNECTIVITY_CONFIGURATION` bridge 和 profile translation unit 中对应 public facade 定义；新 compact 硬件只需实现该 private contract，不必修改共享 renderer / startup / Device API。静态复核确认无重复 `board_port_*cellular*` / transport-selection 定义；Bread 和 Fangtang 分别使用缓存 Xtensa compile command 完成 `-fsyntax-only` 对象级检查（Fangtang 补 Component Manager NV3023 include），`tools/check-hal-boundaries.ps1` 与本切片 diff check 通过。该证据仅覆盖 C/头依赖与 selected-profile 转发：不等于完整 profile link、COM4/COM5 HIL 或 Connectivity Service 的完整 shutdown/restart；待 IDF Python 环境修复后，COM5 必须覆盖 legacy NVS import、GPIO0 selector、4G prepare/start/ready、HTTP/stream/cancel/quiesce 与 Hub rewrite，COM4 必须确认未支持 cellular 路径和 Wi-Fi-only 默认行为。

> 2026-08-11 紧凑屏 frame-present transport capability 收口（Bread Compact / Fangtang-4G）：共享 `present_composed_frame()` 原以 `#if !CONFIG_MACLAW_BOARD_FANGTANG_4G` 选择 Bread 的 dirty-rectangle stripe presenter 或 Fangtang 的完整帧 presenter；该差异是 ST7789/NV3023 的经验证传输能力，不是业务层的板名规则。两种 display adapter 现分别实现私有 `compact_display_adapter_uses_delta_presentation()`（Bread=true，Fangtang=false），共享 renderer 继续独占双 framebuffer、差异计算、full redraw recovery、LCD lock、错误日志与 scene state，仅按 selected display capability 选择相同的两条算法路径。Fangtang 的 GRAM offset、逐行传输和 panel IO 仍不离开 adapter；新紧凑硬件只需声明其 panel 是否安全支持 delta present。Bread target 对象构建通过；Fangtang transition unit 以缓存 Xtensa compile rule（补入 Component Manager cache 缺失的 NV3023 include）对象编译通过，仅有既有未使用符号 warnings。HAL 脚本此前已在当前工作树直接通过；本条完整 link/HIL 仍待 IDF Python 恢复后，以 COM4 验证 dirty present/首帧修复、COM5 验证全帧 NV3023 稳定性和 DISPLAY_OFF 后重绘。

> 2026-08-11 紧凑屏音频默认音量校准收口（Bread Compact / Fangtang-4G）：共享 direct-I2S PCM mixer 原先将 mutable `s_output_volume` 静态初始化为硬编码 70，令新增硬件即便已实现 audio adapter 仍无法声明其安全、可听的 boot volume。`compact_audio_calibration_t` 现增加 `output_volume_default`，Bread/Fangtang audio adapter 各自提供当前验证的 70%，而共享 renderer 在 `board_port_init()` 读取 selected audio calibration 并以 atomic store 初始化运行时值。GUI 的 volume setter、输出 sample 软件 gain、持久化/业务音量 API 与实际播放时序都未改变；这是把 physical/amplifier calibration 从业务 renderer 移到现有 private audio contract，而不是新增 codec/GPIO API。Bread target 对象构建通过；Fangtang transition unit 以缓存 Xtensa compile rule（补入 Component Manager cache 缺失的 NV3023 include）对象编译通过，仅见既有未使用符号 warnings。随后已在同一工作树重新直接执行 `tools/check-hal-boundaries.ps1`，检查通过；此前的“解释器语法错误”属于工具调用封装瞬态失败，而非脚本、源码或 HAL contract 错误。完整 profile link 仍依赖 IDF Python 环境恢复，且仍须在 COM4/COM5 复验默认音量、GUI 0–100% 调节及人声音质。

> 2026-08-11 紧凑屏 selected-profile seam 收口（Bread Compact / Fangtang-4G）：共享 `board_port_bread_compact.c` 仍在 include 区与三处 layout getter 中直接按 `CONFIG_MACLAW_BOARD_FANGTANG_4G` 选择 display/audio/input/peripheral 及 response/standby/recording 布局，虽然业务计算已只消费 normalized contracts。新增私有 `boards/compact_profile_adapter.h`，成为紧凑屏 profile 选择的单一边界：它按 selected profile 包含各板的 display/audio/input/peripheral/layout/cellular adapter，并以共同的 `compact_profile_{response,standby,recording}_layout()` 返回 descriptor；共享 renderer 因此只消费 selected profile seam，不再知道 Bread/Fangtang 头路径或如何选择布局。此 seam 不进入 Device/Platform API，不改变 renderer 的场景、状态机、内存、LCD mutex、输入语义或蜂窝业务；新增紧凑硬件时只需扩展此私有 profile boundary 并实现现有 adapter contract。Bread `board_port_bread_compact.c.obj` 正常 target 构建通过；Fangtang transition unit 以缓存 Xtensa compile rule（补入 Component Manager cache 缺失的 NV3023 include）对象编译通过，仅有既有未使用符号 warnings；HAL boundary / diff check 通过。完整 link 及 COM4/COM5 HIL 仍需在 IDF Python 环境恢复后执行，不能以本次 object evidence 代替。

> 2026-08-11 紧凑屏 response 手动翻页能力收口（Bread Compact / Fangtang-4G）：共享 `board_port_navigate_response()` 仍以 `#if CONFIG_MACLAW_BOARD_FANGTANG_4G` 决定是否忽略 page delta；实际差异不是板名，而是 Input adapter 已明确暴露的 `compact_input_adapter_response_paging_uses_volume_keys()` capability。现改由该 normalized capability 决定：有翻页键的 Bread 保持跨页循环和即时重绘；无翻页键的 Fangtang 保持忽略手动 page delta、由 response layout 的既有 timed-paging 机制推进。业务 API、分页状态、图片 response、自动翻页 deadline 和 LCD lock 所有权均继续留在共享 renderer，私有 adapter 只声明物理输入 affordance；因此新增紧凑硬件只需实现同一 input capability，而不必修改业务状态机。Bread `board_port_bread_compact.c.obj` 正常 target 重编译通过；Fangtang transition unit 以缓存 Xtensa compile rule（补入 Component Manager cache 缺失的 NV3023 include）编译通过，只有既有未使用符号 warnings；HAL boundary / diff check 通过。该证据不替代完整 profile link 或 COM4/COM5 对手动/自动翻页和图片 response 的 HIL。

> 2026-08-11 紧凑屏过渡死路径清理（startup selector / thinking strip）：在 profile hooks 已接管之后，复查 Fangtang 编译单元实际定义的 bridge 宏，发现 shared `board_port_bread_compact.c` 仍保留永远不参与 Fangtang image 的旧 GPIO0 selector state/scanner 分支和旧 NV3023 strip fallback，既浪费审计空间又使人误以为共享层仍可拥有这些硬件行为。本轮删除共享 `s_boot_network_*`、`s_fangtang_thinking_next_frame_us`、旧 `draw_fangtang_thinking_frame()` 及关联 scanner fallback；移除 `MACLAW_FANGTANG_EXTERNAL_BOOT_SELECTOR` bridge 宏。`board_port_wait_for_boot_network_toggle()` 保留既有 public/board-port linkage，但仅转发 selected input adapter 的 `compact_input_adapter_consume_startup_selector_result()`；Fangtang 的 transport-toggle persistence 也直接消费该 private input result，Bread 保持 false no-op。Fangtang thinking timing只留在 its transition profile，所有支持 Fangtang image 均经 display hook 处理 NV3023 strip。没有删除仍在使用的 cellular/connectivity bridge 宏，亦未把 legacy include-based transition unit 误报为完成拆分。Bread shared object、Fangtang transition object、HAL boundary 与 diff check 均通过；Fangtang 仅有既有 Bread-only renderer unused warnings。该静态证据不替代 COM4/COM5 HIL；完整 build 后需回归 startup GPIO0 selector、thinking activity strip、普通 Bread scanner、display-off/wake 与失败启动 rollback。

> 2026-08-11 紧凑屏 thinking partial-present capability 收口（Bread Compact / Fangtang-4G）：继续按“业务状态与物理 panel update 分离”审计时，确认 shared `board_port_bread_compact.c` 中剩余 Fangtang 条件只是在 thinking 开始、定时 tick 和单帧绘制时，切换 Bread robot-mouth 与 NV3023 的 45×11 行式 activity strip；后者的坐标、行传输、front-frame repair 和 80 ms cadence 是 profile display 事实。现改用三项私有 display-adapter contract：`compact_display_adapter_begin_profile_thinking_animation()`、`...pump...()`、`...draw...()`。Bread 三项为 false/no-op，仍由共同 scene 启动现有 robot-mouth worker；Fangtang 在 transition profile 内保留既有 scene eligibility guard、LCD mutex 约束、next-frame timing、NV3023 row writes 与失败时 front snapshot invalidation。共享层只在状态进入 `thinking` 时尝试 profile animation、在既有 pet worker 的 LCD 临界区调用 pump、在一般 mouth frame 处给 profile 第一次消费机会，不再出现 Fangtang-specific function name 或 thinking macro 分支。此修改不将 thinking 业务、response/recording interlock 或任务 ownership 下沉，也不声称 Fangtang bridge 已完整独立。Bread shared object 与 Fangtang transition object 均通过缓存 Xtensa compile rule；HAL boundary/diff check 通过，Fangtang 仅有既有 Bread-only renderer unused warnings。仍需在完整 profile build 后以 COM4/COM5 验证 Bread mouth 的思考动画、Fangtang three-dot strip cadence、结果抢画停止、SPI failure 后下一完整帧修复、宠物 worker 与 DISPLAY_OFF/wake 没有回归。

> 2026-08-11 紧凑屏网络传输显示能力私有化（Bread Compact / Fangtang-4G）：分类 shared compact source 的余下 Fangtang 条件后，确认 `board_port_set_network_transport()` 的唯一差异是 Fangtang 240px 待机页要刷新 physical 4G/Wi-Fi mark；这不是 Connectivity/业务选择逻辑。现将 shared 条件、Fangtang state 和 `fangtang_board_set_network_transport()` 名称替换为 display-adapter 私有契约：`compact_display_adapter_publish_network_transport(cellular)` 负责接受变化并在 Fangtang 空闲表面按既有 mutex/scene guard 触发 redraw，`compact_display_adapter_network_transport_is_cellular()` 让本 profile 的 standby compose 读取其 display-owned snapshot；Bread 两者均是 no-op/false。共享 board facade 现在只转发标准 transport change，且不再保存 cellular display state 或依赖 `MACLAW_FANGTANG_EXTERNAL_NETWORK_TRANSPORT_PRESENTER`。该切片没有把协议、持久化、cellular bring-up 或 Hub URL policy 移入 Display HAL；它只收口“已选链路如何映射到物理屏幕标记”。Bread shared object 与 Fangtang transition object 均通过缓存 Xtensa compile rule，HAL boundary/diff check 通过；Fangtang 仍仅有既有 Bread-only renderer unused warnings。证据是对象级而非完整 link 或 COM4/COM5 HIL；环境恢复后须验证 Fangtang Wi-Fi/4G 切换时待机图标+标签刷新、非待机场景不被抢画、DISPLAY_OFF 后状态保留并在 wake redraw，且 Bread 完全不显示该 profile-private mark。

> 2026-08-11 紧凑屏启动输入选择器私有化（Bread Compact / Fangtang-4G）：继续审计 shared compact startup 后，发现外围初始化之后仍以 `CONFIG_MACLAW_BOARD_FANGTANG_4G && MACLAW_FANGTANG_EXTERNAL_BOOT_SELECTOR` 调用 Fangtang-named boot selector。该窗口是 GPIO0 单物理键在 scanner 创建前的独占时序，不是业务层的网络选择策略。现统一为 `compact_input_adapter_run_startup_selector()`：Bread input adapter 提供无副作用 no-op；Fangtang input adapter 暴露相同私有契约，实现在 profile translation unit 内保留原有 1.8 s、双击、长按过滤及 GPIO0 ownership 逻辑。共享启动序列仅保证该 hook 位于 audio/peripheral 成功之后、normal scanner 创建之前；transport state 保存、切换业务与 Device Input action 发布均未改变。其余 legacy bridge 的 `MACLAW_FANGTANG_EXTERNAL_BOOT_SELECTOR` 宏仍只用于抑制被 profile 实现替代的旧 scanner/legacy export，尚未把整个 Fangtang transition source 误称为独立完成。Bread shared object 与 Fangtang transition object 均以缓存 Xtensa compile rule 编译通过；HAL boundary/diff check 通过，Fangtang 仅见既有 Bread-only renderer unused warnings。证据仍是对象级，不能替代完整 link 或 COM4/COM5 HIL；环境恢复后需验证 Bread 启动无等待、Fangtang GPIO0 double-click 仅在启动窗口切换 transport，窗口内外单击/双击/长按不泄漏为命令，并回归 display-off/wake。

> 2026-08-11 紧凑屏 profile-private peripheral 初始化收口（Bread Compact / Fangtang-4G）：审计共享 `board_port_bread_compact.c` 后，发现初始化序列仍用 `#if CONFIG_MACLAW_BOARD_FANGTANG_4G` 直接调用 `fangtang_board_power_init()`；这让共享业务/场景实现知晓某一板的 ADC 电池监视器。现将其替换为统一的私有 `compact_peripheral_adapter_init()`：Bread 实现无硬件副作用的成功 no-op，Fangtang 在 profile translation unit 内启动原有 charge-GPIO / ADC snapshot worker。共享层只在已完成 display、input、audio bring-up 后编排“selected profile optional peripherals”的相同失败传播，不接触 Fangtang 函数名、ADC、GPIO、task 或 telemetry state；既有 `compact_peripheral_adapter_get_power_status()` 与有界 stop 契约保持不变。该 helper 不进入 Device/Platform 公共 API，也不把电池采样误称为完整 Power Service、ADC deinit 或板级 restart。Bread `board_port_bread_compact.c` 与 Fangtang transition unit 均用各自缓存 Xtensa compile rule 重新编译通过；Fangtang 仅有其既有的未使用 Bread-only renderer warnings；HAL boundary / diff check 通过。证据限于对象级编译，不等于完整 profile link 或 COM4/COM5 HIL；IDF Python launcher 恢复后须通过 profile wrapper 全量构建，并实机核验 Bread 初始化无回归、Fangtang 启动后 battery/charging telemetry、display-off/wake 与 rollback 期间 power worker stop。

> 2026-08-11 紧凑屏 remote-pet worker 等待节拍私有化（Bread Compact / Fangtang-4G）：共享 `board_port_bread_compact.c` 的宠物 worker 仍有一处 `#if !CONFIG_MACLAW_BOARD_FANGTANG_4G`，用板型推断无多帧宠物时的 500 ms idle wait；差异实际来自 Fangtang 同一 worker 还承担 profile-private thinking/自动翻页 pump，而非宠物业务本身。现将该 runtime cadence 下沉为同名 `compact_display_adapter_pet_worker_wait_ms(frame_count, animated_frame_ms)`：Bread 在少于两帧时返回 500 ms（仍定期落实统一 30 分钟 idle display-off 策略），多帧返回共同 80 ms 节拍；Fangtang 始终返回共同 80 ms，以保持其 NV3023 partial presentation hook 与自动翻页既有时序。共享 worker 继续唯一拥有宠物状态、动画循环、display-off、自动翻页 eligibility 与 LCD lock，只向 selected display adapter 查询本板的 runtime footprint；未新增 Device/Platform/HAL 公共 API，也未把电源业务策略误移为硬件差异。Bread `board_port_bread_compact.c.obj` 通过正常 CMake target 重编译；Fangtang transition unit 用缓存 Xtensa compile rule（补入 Component Manager cache 中缺失的 NV3023 include）重编译通过，仅见既有未使用符号 warnings；HAL boundary / diff check 通过。该 object-level 证据不等于 Fangtang 完整 link 或 COM4/COM5 HIL，待 IDF Python 环境修复后仍须按 profile wrapper 全量构建，并在两板验证 idle timeout、单帧/多帧远程宠物、Fangtang thinking 与自动翻页没有节拍回归。

> 2026-08-11 Waveshare 1.75C 32 点阵字库 profile contract 收口：继续复核上一轮 32-dot 接入后，发现 `round_display_font_adapter.h` 虽不再让 `board_port.c` 直接判断板型，却仍以 `CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C` 选择字库、fallback binary 与像素路径；这会让每个新增圆屏继续修改共享显示实现。现新增中性的 `round_display_font_profile_t`，并将 EchoEar 的 24-dot / Waveshare 的 32-dot native row table、packed CJK fallback 和 raster cell 下沉到各自 `echoear_round_font_adapter.h` / `waveshare_round_font_adapter.h`。`round_profile_adapter.h` 只返回 selected profile 的 font contract；共享 renderer 仅按该 contract 准备字形、读取像素与按 cell metric 布局，完全不知道板型、二进制符号或 24/32 的分支。Waveshare 继续仅嵌入 `cjk32_cjk.bin`，完整范围 U+4E00..U+9FFF、128 B/字、总计 2,686,976 B 已复核；EchoEar 保持其 24-dot 资源，普通非弧形文本仍经统一 24-dot compositor，故不改变既有页面布局。此前修复的先乘后除字距公式仍保留，Waveshare 32px CJK cell 前进 33px 而非错误的 25px。以缓存 Waveshare Xtensa compile rule（补齐 Component Manager cache 的 CO5300/touch includes）重编译 `board_port.c.obj` 通过，仅见既有 `compose_text16_curve` 未使用 warning；HAL boundary 和 diff check 均通过。该证据是对象级编译，不等于完整 profile link、固件刷写或 COM6 HIL；完整 build 仍被本机 IDF Python launcher 架构错误阻断。环境恢复后须执行 `tools\\build-profile.cmd waveshare-amoled-1.75c build`，再在 COM6 核验中文城市/天气、长弧文字、`°C`、Hub 动态 glyph 与实际可读性。

> 2026-08-11 Waveshare 1.75C 32 点阵字库复核与曲线字距修正：Waveshare selected-profile 继续独占 `font_cjk32.h`（333 个高频原生 row glyph）及完整 `cjk32_cjk.bin` packed fallback；后者覆盖 U+4E00..U+9FFF，每字 128 B，共 2,686,976 B，已由 CMake 仅在 Waveshare 镜像嵌入。`round_display_font_adapter.h` 将 native/packed/dynamic 24-dot Hub glyph 的 32-dot 采样限制在该 profile-private display seam，EchoEar 保持既有 24 点阵资源；普通非弧形文本仍以 24-dot compositor 显示，避免无关场景布局变化。审计实际曲线 compositor 后发现一个会抵消字体升级的整数除法错误：原先先计算 `32 / 24 == 1`，致使 Waveshare 的 32px 中文字元仍按 25px 前进，字形相邻重叠。现改为先乘后除，25px CJK advance 映射为 33px，保留一列明确间距；这个修复只属于共享 renderer 消费 profile 的物理 raster metric，不泄漏进 Device/Platform/HAL 业务契约。以缓存的 Waveshare Xtensa response rule（并从 Component Manager cache 恢复其缺失的 managed include 路径）重新编译 `board_port.c.obj` 通过，仅有既有 `compose_text16_curve` 未使用 warning；HAL boundary、diff check 均通过。完整 profile link 仍被本机 IDF Python launcher 损坏阻塞，故不能将 object 编译或字库尺寸核对误称为完整镜像/COM6 HIL；环境恢复后须通过 `tools\\build-profile.cmd waveshare-amoled-1.75c build` 重建，再在 COM6 核验中文城市/天气、长弧文字、`°C`、Hub 动态 glyph 与可读性。

> 2026-08-11 显示 partial-init 分阶段故障矩阵（四 profile）：将此前单一“全部 display 资源已获得后失败”的测试 seam 细化为仅测试构建的 `MACLAW_DISPLAY_FAILURE_STAGE=1..5`。每个 profile-private adapter 在自身真实 acquisition 边界后调用同一 private helper：1 为背光/面板电源准备（Waveshare 无独立 PWM，为明确 no-op 边界）、2 SPI bus、3 panel IO、4 panel object、5 panel reset/init/DISPON/brightness 完成；命中时通过既有 `fail:` 逆序释放准确已拥有资源。EchoEar 现同时追踪并在失败时关闭/复位自身 PWM timer/channel，避免 stage 1 后遗留亮屏 owner；Bread/Fangtang 的 PWM 回滚及四 adapter 的 panel→IO→SPI（圆屏另含 completion semaphore）保持 profile 内部，未新增 Device/Platform API。默认 Bread/Fangtang/EchoEar/Waveshare profile object 重新编译通过（仅既有 renderer unused warning）；Fangtang CMake 重生成亦成功。尚未把每个 stage 的测试配置完整刷写到 COM3–COM6：HIL 应逐 stage 启动一次，确认 fault 日志、一次 degraded path、无 panic/WDT/亮屏残留，并立即刷回 release 镜像；因各 stage 的 numeric mapping 是 adapter-private，不应由业务层或 Hub 依赖。
> 2026-08-11 显示初始化失败回滚的可执行验证补强（四 profile）：新增仅在 `CONFIG_MACLAW_TEST_BUILD=y` 时可开启的 `CONFIG_MACLAW_DISPLAY_FAILURE_INITIALIZATION=y`。该开关没有运行时 setter、HTTP、Hub、控制台或实体输入路径；它由 selected display adapter 在 panel/IO/SPI/backlight 已成功取得后强制进入既有 `fail:` 清理分支，分别覆盖 Bread ST7789、Fangtang NV3023、EchoEar ST77916 和 Waveshare CO5300 的 profile-private rollback。共享紧凑 renderer 同时去除 `ESP_ERROR_CHECK(compact_display_adapter_init_hardware())`：adapter 返回失败将经 Platform Input / Input Service / App Intent 转为既有 degraded startup，而不是 panic/reboot，从而让回滚和诊断可观察。默认 release 编译中测试谓词为常量 false；Bread、Fangtang、EchoEar 对象构建已重编译通过，Bread 亦已完成测试配置下的完整 ELF 链接，公共 HAL boundary / diff 检查通过。该测试目前证明的是“成功获取全部 display 资源后”的事务回滚与错误传播，不替代 SPI、panel IO、reset/init、brightness 各单点失败注入，也不代表 runtime renderer deinit/restart；下一步应扩展 profile-private step fault 并在 COM3/COM4/COM5/COM6 用非 release 测试镜像确认一次 DEGRADED 启动日志后恢复正式镜像。
> 2026-08-11 显示 profile-private partial-init 失败回滚补强（Bread Compact / Fangtang-4G；圆屏 EchoEar-2ST / Waveshare 1.75C 同一语义已具备）：四个 display adapter 均将 panel bring-up 视为单个失败事务。Bread 的 ST7789、Fangtang 的 NV3023 初始化在任一步骤失败时，按 panel → panel IO → SPI bus → PWM channel/timer 的逆序回收本 profile 已取得的资源，并清除精确 ownership 标记；圆屏 adapters 同样处理其 panel/IO/SPI/transfer-completion semaphore 的失败路径。成功初始化仍保持既有 boot-lifetime，不新增 renderer deinit/restart，也不改变 Display Service、Device API、场景、亮度或 DISPLAY_OFF 语义。Bread / Fangtang 已用有效 IDF 6.0.2 profile object build 重新编译通过，Waveshare 32 点阵 profile object 亦已通过；公共 HAL boundary check 与 diff check 通过。Fangtang 本轮完整 link/HIL 尚未完成，不能把 object build 或回滚代码等同于各个失败注入点和实机屏幕验收；后续应在 COM4/COM5 以故障注入覆盖 SPI、panel IO、panel reset/init、亮度失败及同一 boot 内 retry，同时持续保持成功资源不可被 runtime rollback 误释放。
> 2026-08-11 紧凑屏远程宠物来源帧所有权私有化（Bread Compact / Fangtang-4G，进行中）：与圆屏同样收口 consuming display install 的 allocator 边界。共享 `board_port_bread_compact.c` 不再包含 `esp_heap_caps.h` 或直接 `heap_caps_free()` Hub/缓存下载的 source frame；在完成可见场景副本的原子安装后，仅调用选中 profile 的 `compact_display_adapter_release_consumed_pet_source()` 并置空调用者 slot。Bread 与 Fangtang 的 display adapter 各自保留 ESP-IDF capability allocator 的 matching release，与 retained remote-pet frame 的分配/释放同属 profile-private display ownership；Device API、Display Service、Platform Display、场景/重试/关键帧降级规则和业务层 frame-count 契约均未变化。HAL boundary check 与 diff check 已通过。后续以隔离有效 IDF Python 使用正式 wrapper 分别 build Bread/Fangtang，并在 COM4/COM5 验证 GUI pet install、PSRAM 压力下关键帧降级、clear/replace、DISPLAY_OFF/wake 与无悬空来源帧。本项不把 source buffer API 迁移误报为独立 Display Task、renderer deinit/restart 或完整 HIL。
> 2026-08-11 圆屏后台任务运行时策略私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：继续按 Phase 5/6 清理 shared `board_port.c` 中并非场景或会话策略的 ESP-IDF 运行时事实。待机宠物动画的任务栈大小、PSRAM capability 与 core 1 affinity 已下沉到 selected `round_display_adapter_start_pet_animation_task()`；离线唤醒命中后的 dispatch task 的 PSRAM stack capability 已下沉到 `round_audio_adapter_start_wake_dispatch_task()`。共享层仍唯一决定何时创建/停止任务、foreground/rollback admission、wake callback 合并和失败后的业务回退，只传递函数入口与普通 `TaskHandle_t *` 输出；同时 consuming remote-pet install 的 source-frame matching release 改为 `round_display_adapter_release_consumed_pet_source()`，shared renderer 不再直接包含 `esp_heap_caps.h`、写 `MALLOC_CAP_*`、调用 capability free 或选择 core。两种圆屏 profile 继续保持各自的 RGB565 panel、宠物布局和 32-dot Waveshare 文字路径不变；这些 helper 是 profile-private seam，不进入 Device/Platform 公共头。HAL boundary check 与 diff check 已通过；本机隔离 build directory 中 `board_port.c.obj` 已在本次修改后重新编译。完整 link 当前被旧 build 目录中系统 Python 3.14 架构损坏的 ESP-SR model packaging 阶段阻塞；应以 `tools\\build-profile.cmd echoear-2st build` / `tools\\build-profile.cmd waveshare-amoled-1.75c build` 从干净的有效 IDF Python 环境重跑，随后在 COM3/COM6 验证 pet worker 启动/停止、连续 wake callback、display-off/wake、远程宠物 install/内存压力及 Waveshare 32 点阵待机环。该切片不宣称独立 Display/Audio Service、task allocator/fault recovery、runtime deinit/restart 或完整 HIL 已完成。
> 2026-08-10 圆屏 I2S handle/PCM driver 边界收口（EchoEar-2ST / Waveshare 1.75C，进行中）：在 I2S bring-up 私有化后，继续移除共享 `board_port.c` 对 `i2s_chan_handle_t` 和 `i2s_channel_read/write` 的直接持有。`round_audio_codec_adapter.h` 现私有保存 RX/TX driver channel，负责创建、释放、时钟诊断和带 timeout 的 PCM read/write；共享层只传递普通 PCM buffer、字节数、已写/已读计数与 timeout，继续独占 capture/playback/wake-word mutex、owner、取消、slot-to-mono 处理、VAD/AGC 与播放静音时机。该 adapter 仍不是 Device/Platform API，且无 codec/I2S 类型逃逸到共享 public header。EchoEar/Waveshare `board_port.c` 两 profile 对象编译通过（仅既有 `compose_text16_curve` warning），HAL boundary check 待本轮汇总执行；完整 link/HIL 仍因 ESP-IDF Python 环境损坏不可宣称。该切片完成后，shared round audio 唯一残留的 hardware-facing对象是 codec I²C handle，下一步可继续收口其 register operation 调用，而不是改变业务 audio contract。

> 2026-08-10 圆屏 Input native-gesture 语义私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：审计共享圆屏手势扫描器后，将其遗留的 CST816 controller 名称与原始 `0x0B` double-tap 值移出 `board_port.c`。EchoEar profile-private input adapter 现在将 CST8xx 的该 controller 报告规范化为 `round_touch_adapter_is_native_double_tap()`；Waveshare CST9217 adapter 明确报告无可靠 native double，由共享 timing classifier 保持原有合成 double 行为。共享层仍唯一拥有按键/触摸 source、debounce、short/double/long 时序、DISPLAY_OFF 首 contact 消费及 Device Input action 发布，不再依赖触控 controller 寄存器值或型号。EchoEar/Waveshare 的 `board_port.c` object 编译均通过，HAL boundary check 仍通过，只有既有 `compose_text16_curve` warning；完整镜像与 COM3/COM6 单击/双击/长按/熄屏首触摸 HIL 待 ESP-IDF Python 环境恢复后完成。本项不拆掉 shared scanner task 或触控 I²C 生命周期，只收口 controller-specific gesture facts。

> 2026-08-10 圆屏 Audio I²C 总线/codec device 生命周期私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：完成圆屏 Audio HAL 本轮 I²C physical-resource 下沉。共享 `board_port.c` 不再配置 I²C port/pins、codec address 或直接创建/删除 master bus/device；`round_audio_codec_adapter.h` 独占这些 profile-private 值、精确逆序释放与 codec attach 失败清理。为保持 Waveshare 的 PMIC/CST/QMI 与 codec 共用同一 bus 的既有正确时序，adapter 将过程分为“open shared bus”与“外围 adapter 初始化后 attach codec devices”：共享层仅编排既有依赖顺序和 retry/failure policy，仍不接触电气参数。EchoEar/Waveshare `board_port.c` object 编译均通过，HAL boundary check 通过，只有项目既有 `compose_text16_curve` 未使用 warning；由于 ESP-IDF Python 损坏，完整 profile build/link 及 COM3/COM6 启动、触摸、录音/播放 HIL 仍待恢复环境后执行。Audio I²C handle 仍为 shared source 的不透明 borrower，后续完整 Audio Service 应进一步将 adapter 生命周期与 session owner 拆入独立 source，而非声称已可 restart/deinit。

> 2026-08-10 圆屏 I2S bring-up 私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：继续 Phase 6 的硬件职责下沉。共享圆屏 `board_port.c` 已移除 I2S port、DMA descriptor/frame 数、auto-clear、MCLK/BCLK/WS/DIN/DOUT 接线、STD slot 格式、enable/disable/delete 及时钟诊断的直接实现；`round_audio_codec_adapter.h` 以 profile-private `ROUND_PROFILE_AUDIO_*` 接线和经过实机验证的 16 kHz Philips stereo 16-bit / 32 BCLK-LRCK 合同创建、启用、回滚并诊断 full-duplex channel pair。共享层只保存不透明 RX/TX handle 并在既有 capture/playback/wake-word 会话中读写，保留 retry、互斥、owner 和 PCM 行为。EchoEar/Waveshare `board_port.c` 两个对象编译均通过（仅项目既有 `compose_text16_curve` 未使用 warning），HAL boundary check 通过；完整 profile link 与 COM3/COM6 PCM/人声 HIL 仍受当前 ESP-IDF Python 损坏阻塞，不能误报为 Audio Service 完成。I²C bus/device 生命周期仍暂由 shared round source 管理，后续应按同一边界继续下沉。

> 2026-08-10 圆屏 Audio PA GPIO 私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：紧随 codec register adapter，圆屏共享 `board_port.c` 不再持有 `AUDIO_PA_ENABLE` 宏、GPIO 配置或 `gpio_set_level()` 调用。profile-selection seam 私有提供 PA 引脚，`round_audio_codec_adapter.h` 独占其 output 配置以及 enable/disable 电平写入；共享 renderer 仅在既有 playback session 所有权、首 PCM 块解除 DAC mute、zero-tail 与结束恢复的时机请求“PA 开/关”。这保持了 EchoEar/Waveshare 当前 active-high 放大器电气行为和错误传播，也不把 GPIO 泄漏回 Device/Platform API。EchoEar 与 Waveshare 均以各自既有 response-file 完成 `board_port.c` 对象编译，均仅保留项目既有 `compose_text16_curve` 未使用 warning；HAL boundary check 通过。完整镜像构建仍待恢复 ESP-IDF Python 后执行，且 COM3/COM6 的人声/静音尾音 HIL 仍为必要门禁。本项是 profile-private physical output 的小步收口，未声称 I2S、codec bus、PA lifecycle 或 Audio Service 已完整独立。

> 2026-08-10 圆屏 Audio codec 私有适配收口（EchoEar-2ST / Waveshare 1.75C，进行中）：继续按 Phase 6 收口共享圆屏 `board_port.c` 中不属于会话/业务状态机的硬件细节。新增仅由 `boards/round_profile_adapter.h` 私有包含的 `boards/round_audio_codec_adapter.h`，将 ES7210/ES8311 的 I²C register read/write、经验证的 16 kHz codec 初始化序列、麦克风模拟 gain 恢复、ES8311 的对数音量寄存器映射与 DAC mute 位操作移出共享 renderer。共享源仍唯一拥有 capture/playback/wake-word 的互斥、owner、取消、PCM frame 与 I2S 会话生命周期；新私有 adapter 不进入 Device/Platform 公共头，也不更改音频业务契约或输出音质参数。`tools/check-hal-boundaries.ps1` 已通过。由于本轮 source 改动不依赖 Python 生成物，已直接复用两个既有 profile 的 compile response file，以 Xtensa 编译器完成 EchoEar 与 Waveshare `board_port.c` object-level 编译，均仅保留项目既有 `compose_text16_curve` 未使用 warning；这证明新 private codec adapter 在两种圆屏 profile 下的 C/头依赖可编译。完整镜像链接仍受本机 ESP-IDF Python 环境损坏阻塞：`C:\\Users\\ma139\\.espressif\\python_env\\idf6.0_py3.14_env\\Scripts\\python.exe` 无法启动（Windows %1 非有效应用），ninja 在生成 `x509_crt_bundle`/其他 Python 生成物前停止，故不得声明本轮 EchoEar/Waveshare 构建或 HIL 已通过；恢复有效 IDF Python 后，必须先用唯一入口重跑 `tools\\build-profile.cmd echoear-2st build` 与 `tools\\build-profile.cmd waveshare-amoled-1.75c build`，再在 COM3/COM6 验证播放人声、音量、录音、wake pause/resume。该切片只抽离 codec 寄存器实现，尚未形成独立 Audio Service，也尚未迁移 I2S/channel 或 PA GPIO 生命周期。

> 2026-08-11 圆屏 QSPI 传输节拍私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：复核 shared round renderer 后，确认每 16 次 stripe transfer 的 `vTaskDelay(1)` 原本依赖 ST77916/CO5300 的同步 polling QSPI 事务，是控制器/bus 的 watchdog 与网络 housekeeping 节拍，而非场景、宠物、文本或业务策略。现删除 `board_port.c` 的全局 `s_draw_transactions` 和两个直接 scheduler handoff；两个 profile-private display adapter 各自在成功完成本次 panel completion fence 后维护自身 transaction counter，并在相同第 16 次频率让出一个 tick。共享 renderer 仍只决定何时、以何内容、按哪个 stripe layout 提交，并继续只收到同步 draw success/failure；不改变 40-row staging、圆形裁切、Waveshare 32-dot 文字、EchoEar 布局或错误超时语义。2026-08-11 已用 `build-review-waveshare` 重新构建 `esp-idf/main/CMakeFiles/__idf_main.dir/board_port.c.obj`，通过（仅既有 `compose_text16_curve` 未使用 warning）；`tools/check-hal-boundaries.ps1` 与 `git diff --check` 通过。旧 EchoEar build 目录仍无法在当前 shell 进行 CMake regeneration，故此轮不把 EchoEar 完整 link/HIL 误报为已完成；环境有效后须用支持的 profile wrapper 构建并在 COM3/COM6 验证长场景 present、宠物动画、网络并发、录音/回复与 DISPLAY_OFF/wake。

> 2026-08-11 圆屏字体资源与曲线字形适配私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：继续按“显示差异由 profile adapter 承担”审计 shared `board_port.c`，发现共享 renderer 仍直接选择 Waveshare 32-dot/ EchoEar 24-dot 字库、嵌入二进制字库符号并在曲线文字循环中以 board macro 分支。现新增仅由圆屏 renderer 私有包含的 `boards/round_display_font_adapter.h`：该 seam 持有 selected profile 的 CJK fallback binary、native row table、24→32-dot 动态 Hub glyph 覆盖采样、曲线 cell raster size 与 packed scratch layout；Waveshare 继续只嵌入 cjk32、普通 24-dot scene text 按原有面积采样，EchoEar 继续嵌入 cjk24。共享 renderer 保留 UTF-8、字距/圆弧 baseline、clipping、颜色及所有业务文本/场景，只向适配器请求“准备本字形/读取本像素”，不再直接出现 Waveshare macro、32-dot header/binary symbol 或 font-size profile macro。普通 24-dot renderer 保留原有 ASCII 与温度 `°C` fallback，避免此次收口改变 Bread 基线对齐的天气可读性。2026-08-11 Waveshare `build-review-waveshare` 中 `board_port.c.obj` 重新编译通过；EchoEar 的旧 CMake build 目录会因缺失 `IDF_PATH` 重生成失败，但已直接复用其生成的 Xtensa compile rule 成功编译同一对象（两者仅既有 `compose_text16_curve` unused warning）。HAL boundary check 与本切片 `git diff --check` 通过。仍须在 COM3/COM6 验证中文城市/天气、Hub 动态 glyph、`°C`、圆弧长文字与 32-dot Waveshare 清晰度；静态对象检查不等于 complete link/HIL。

> 2026-08-11 圆屏输入电气标识彻底留在 private adapter（EchoEar-2ST / Waveshare 1.75C，进行中）：继续复核 shared `button_task` 后确认其行为已只依赖 normalized pressed/touch，但启动日志仍读取 `round_input_adapter_activate_key_gpio()` 与 `round_touch_adapter_irq_gpio()`，令共享 renderer 间接知道 GPIO 编号。现删除 `board_port.c` 的 `driver/gpio.h`、两个 pin-number getter 及其日志字段；EchoEar/Waveshare 私有 adapter 仍独占 GPIO0、EchoEar GPIO42/CST8xx 和 Waveshare GPIO11/CST9217 的配置、极性与 controller lifecycle，shared scanner 仅记录“初始按下/触摸可用”并继续拥有 debounce、短/双/长按、DISPLAY_OFF 首 contact 消费、Input Service 发布和 stop/join。该变更不新增中断、不会把触屏从 polling 改成 GPIO IRQ，故不改变现有按键/触控时序或唤醒语义。2026-08-11 Waveshare `board_port.c.obj` 重新构建通过；EchoEar 旧 build CMake regeneration 仍缺 `IDF_PATH`，已直接复用其生成的 Xtensa compile rule 成功对象编译，两者仅既有 `compose_text16_curve` unused warning；HAL boundary 与本切片 diff check 均通过。COM3/COM6 仍需验证单击/双击/长按、触屏、熄屏首触只亮屏、scanner stop/join；静态编译不等于 input HIL。

> 2026-08-11 紧凑屏任务运行时配置私有化（Bread Compact / Fangtang-4G，进行中）：延续圆屏的 task-placement seam，shared `board_port_bread_compact.c` 不再直接指定三类 worker 的 task name、stack、priority 或 core affinity。共享层仍唯一拥有按键手势分类与 stop/join、宠物/思考动画的 admission 与场景状态、离线 wake 模型生命周期、pause/stop/callback 和失败语义；选中 profile 的 `bread_*` / `fangtang_*` display、audio、input adapter 只接收入口函数和 `TaskHandle_t *`，分别创建输入 scanner、两类 display animation、wake recognizer。这样后续紧凑屏可在 profile 内调节运行时栈/核/内存策略，而不复制业务行为或泄露到 Device/Platform API。2026-08-11 `build-review-bread` 已重新构建 `board_port_bread_compact.c.obj` 通过；Fangtang 的相同 cached Xtensa compile invocation 已从临时 response command file成功编译通过，避免陈旧 CMake 环境触发的 Python 3.14 架构错误。HAL boundary check 与本切片 `git diff --check` 待汇总执行；尚未完成完整 link、COM4/COM5 HIL，也未声称独立 Display Task、Audio Service、runtime deinit/restart 或 task allocator fault recovery。
> 2026-08-11 紧凑屏显示几何与声学标定 profile contract 收口（Bread Compact / Fangtang-4G，进行中）：共享 `board_port_bread_compact.c` 现不再以 `CONFIG_MACLAW_BOARD_FANGTANG_4G` 分支选择屏幕 width/height、初始亮度、采样率、命令起止/静音阈值或 wake gain/threshold；新增私有 `boards/compact_audio_calibration.h` 的普通数据结构，Bread/Fangtang audio adapter 各自返回不可变 calibration，display adapter 各自返回物理 panel geometry/default brightness。共享 renderer 仅通过中性 getter 维持原有 framebuffer 尺寸、WAV 16 kHz 格式、VAD/AGC、wake 与亮度业务流程；初始 brightness 在 board bring-up 时读取 adapter，避免将 runtime getter 误用于静态初始化。Bread `board_port_bread_compact.c.obj` 重新编译通过；Fangtang 以其 cached Xtensa command 的临时 response file 对 board-port 对象编译通过；HAL boundary / diff check 通过。本项没有抽离 Fangtang 专用方糖 artwork/蜂窝网络 header、启动 selector，未新增或改变业务策略，不代表完整 link 或 COM4/COM5 HIL。
> 2026-08-11 紧凑屏响应翻页输入能力收口（Bread Compact / Fangtang-4G，进行中）：共享 response renderer 的 footer 提示不再通过 `CONFIG_MACLAW_BOARD_FANGTANG_4G` 选择“音量键翻页”或“自动翻页”。Bread/Fangtang private input adapter 各自以 `compact_input_adapter_response_paging_uses_volume_keys()` 声明可用输入能力；共享层仍拥有 response page count、自动翻页时机、激活键返回与所有业务状态，只根据中性 capability 选择文案。Bread 保持音量键翻页，单实体键 Fangtang 保持自动翻页，未改变按键映射、页面逻辑或输入服务 API。Bread object 重新编译通过，Fangtang cached Xtensa object 编译通过；HAL boundary/diff check 通过。该切片没有将 Fangtang 方糖画面、4G/Wi-Fi header 或启动 selector 误当作输入能力，完整 link/COM4/COM5 HIL 仍待。
> 2026-08-11 紧凑屏响应自动翻页能力收口（Bread Compact / Fangtang-4G，进行中）：共享 pet/idle worker 不再以 Fangtang board macro 包围 response auto-page 的状态和调度。两块板既有 `compact_response_layout_t.automatic_page_interval_us` 成为唯一 profile capability：Bread 保持 `0`，因此从不 arm 自动翻页；Fangtang 保持 `6s`，因此多页文字回复仍按原时机自动前进。共享层统一维护 deadline、仅在 interval 非零且 foreground text response 有多页时触发、图像 response 清零 deadline、用户翻页后重新 arm；并继续在同一 LCD mutex 中提交页面，未改变 page 内容、输入语义或任务所有权。中途发现相邻 Fangtang thinking conditional 的嵌套 preprocessor 容易被误损坏，已恢复为原有正确 guard 后重新验证。Bread object 与 Fangtang cached Xtensa object 均通过；HAL boundary/diff check 通过。此项不代表 renderer 已拆成独立 Display Task，也不替代 COM4/COM5 多页回复、自动翻页、图片回复/返回及网络并发 HIL。
> 2026-08-12 Display Service 启动后故障注入 HIL（COM5，Phase 4 增量）：为避免 boot-only 故障注入在 esptool hard-reset 后丢失首段串口，`tools/capture_com5_transaction.py` 增加仅显式启用的 `--reset-after-open`，使诊断端先独占 COM5 再执行 DTR/RTS reset；普通命令采集不受影响。独立 `fangtang-4g-renderer-fi` image（`TEST_BUILD=y`、`DISPLAY_SERVICE_FAIL_AFTER_INIT=y`）已完整刷写/verify-flash 后实机运行：日志依序记录 `forcing startup failure after Display Service publication`、`display task stopped`、取消 worker/音量持久化 worker 停止、`startup degraded: phase=4 … display service test injection` 与 `Returned from app_main()`；采样中没有 assert/panic/Guru Meditation/WDT/reset loop。这证明已发布 Display Task 在 composition rollback 时能关闭 admission 并完成 STOP，而非重现此前 queue 未发布竞态。测试后 COM5 已立即恢复 `build-unified-fangtang` 正式镜像，并完成写入 hash、verify-flash digest、reset-after-open 启动采样；记录 `BOOT_STATUS.ready=true`，无 degraded/panic/WDT。此验收仍不覆盖 renderer 卡死导致的 STOP timeout/迟到退出、多 stopper 竞争、DMA/scan-out fence、panel/audio teardown 或 runtime display restart。

> 2026-08-12 DMA/scan-out 迟到 completion fence 收口（Bread Compact / Fangtang-4G / EchoEar-2ST / Waveshare，Phase 4 增量）：审计四个 profile-private `*_draw_bitmap_sync()` 后发现，若一次 color DMA fence 等待超时，下一次绘制会直接清除 semaphore token、重置 `transfer_pending=true` 并提交新事务；此前一帧的迟到 callback 因而可能被误当作新帧完成，导致 renderer 提前复用仍由 panel DMA 读取的 framebuffer/stripe，或使 DISPLAY_OFF/wake 与实际旧传输交叠。现每个 adapter 在任何新 submit 前先等待**前一笔**私有 transfer 变为 idle；timeout 保留 pending/source ownership，后续 draw/power transition 都 fail closed，绝不以新请求消费旧 callback。四个 ISR callback 均改为先 give completion token、再发布 idle，利用 ISR 返回前不会切换至 waiter 的 FreeRTOS 语义，避免“已见 idle 但遗留 token”污染下一传输。该修复保持所有 ESP-LCD/semaphore/DMA 事实在 profile-private adapter，shared renderer 与 Device/Platform API 无新增板型或 SDK 类型。现又提供 `CONFIG_MACLAW_DISPLAY_TRANSFER_FENCE_TIMEOUT_ONCE`：仅 test image 的首笔**真实** color transfer 在提交后放弃一次 fence wait，后续 draw 必须等待该真实 callback；没有 runtime setter/Hub/HTTP/console/release 路径。`fangtang-4g-fence-fi` wrapper 与独立 sdkconfig 已生成并确认开启该 Kconfig；普通 Fangtang 全量 build 已成功，HAL boundary/diff check 通过。该新 FI image 的独立全量 build 因 Windows 并发工具链残留锁/静默卡死未完成，已仅终止该 test build 的子进程树，未触碰任何设备或正式固件；因此仍不宣称 fence timeout/full-link/HIL 已验收。下一步需在空闲工具链下完成 test image、写入 COM5、确认 first timeout 后后续画面仅在旧 callback 到达后继续，并立即恢复 release。

# MaClaw AgentOS 开发计划（ESP32 统一业务与硬件抽象）

> 2026-08-10 Display Service facade 收口（四个 profile，进行中）：新增内部
> `display_service`，使所有 `device_display_*` 和宠物素材预算查询统一走
> `Device API → Display Service → Platform Display → selected board renderer`。
> 本切片刻意保持同步转发：Bread Compact、Fangtang 的矩形 renderer，以及
> EchoEar-2ST、Waveshare 的圆屏 renderer 仍独占原有 framebuffer、DMA fence、
> panel transaction 与场景顺序，因而不改变圆屏裁切、现有显示布局或任何业务行为。
> 这先移除了 Device API 对 Platform Display 的直接依赖，为后续以 immutable
> snapshot/revision/队列合并实现真正唯一 Display Task 留出稳定边界；尚不能将其
> 宣称为 Display Task、异步 render queue、runtime renderer restart 或唯一 panel
> owner。已用四个现有 profile 的 Xtensa compile command 分别 syntax compile
> `display_service.c` 与 `device_api.c`，均通过；完整重新配置/link 与 COM3/COM4/
> COM5/COM6 的显示 HIL 仍受本机 ESP-IDF Python/CMake 环境阻塞，恢复后必须执行。

> 2026-08-10 共享 Audio Service 会话状态与唤醒词 admission 收口（四 profile，进行中）：
> 内部 `audio_service` 现在统一持有短命令 capture、会议 PCM stream、WAV 和 decoder
> playback 的唯一 foreground session 与相应 `DISPLAY_OFF` lease，并公开仅内部使用的
> by-value snapshot（session、wake running、wake paused）。每个会话在调用 Platform
> Audio 前先取得 service admission；stream read/PCM write 必须属于当前有效 transaction，
> 因而不会把无 owner 的数据写入 adapter。wake-word start 在任何 foreground audio
> session 存在时明确返回 BUSY，成功 start/stop/pause 则同步 service observation，
> 避免重启监督与 capture/playback 同时争用 I2S。Device API 保持稳定且无状态的 facade，
> `Platform Audio → selected board adapter` 仍唯一拥有 codec/I2S 物理事务；命令/会议
> 领域和 `main.c` 的重启 worker 尚未迁移，board-port 内的 capture/playback/wake 状态机
> 亦未完整搬离。该切片不改变 Bread、EchoEar、Fangtang、Waveshare 的 PCM、音量、录音或
> 圆屏显示行为。已用四个既有 profile 的 Xtensa compile command 对
> `audio_service.c` / `device_api.c` 作 syntax compile，均通过，并通过 HAL boundary
> check；完整重配置/link 仍受本机 ESP-IDF Python/CMake 环境不可用阻塞，
> COM3/COM4/COM5/COM6 人声/录音/唤醒词 HIL 待恢复。

> 2026-08-10 Audio Service 并发状态一致性补强（四 profile，进行中）：会话 snapshot
> 现携带非零、单调递增的 `session_generation`，供后续 wake supervisor/诊断关联一次
> foreground owner 的开始与结束，而不暴露 task/codec/I2S handle。服务在开始前先校验
> 参数，再取得 admission；`stream_read`、`playback_write`、`playback_end` 都先验证本服务
> 的 session+lease，再调用 Platform Audio，故迟到或重复 end/read/write 不会转发至板级
> adapter 或释放另一操作的 lease。当前 generation 是内部诊断状态，不改变 Device API 或
> Hub 协议。四 profile `audio_service.c`/`device_api.c` Xtensa syntax compile 与 HAL
> boundary check 通过；完整 link 和 HIL 仍待可用 ESP-IDF 环境。

> 2026-08-10 Audio Service 闹钟 burst 排他性收口（四 profile，进行中）：Alarm
> Manager 继续拥有整段响铃策略的 alarm-domain lease，但每一次本地 tone burst 现在也先
> 通过 Audio Service 的 `ALARM_BURST` session admission，才调用 Platform Audio。
> 因此闹钟声不再是绕过 shared session state 的板级 playback shortcut；若命令录音、会议、
> WAV 或 decoder PCM 正在占用物理音频，burst 返回 BUSY 并保留 Alarm Manager 原有的下次
> tick/retry/dismiss 策略。此变更不改变警铃合成、音量或显示；必须在 COM3/COM4/COM5/COM6
> 验证“响铃 ↔ 前台语音”实际仲裁与听觉效果。四 profile 对 `audio_service.c`、
> `device_api.c`、`alarm_manager.c` 的 Xtensa syntax compile 通过，HAL boundary check
> 通过；完整 link/HIL 仍因本机 ESP-IDF 环境不可用而未完成。

> 2026-08-10 Audio Service wake-pause ownership 收口（四 profile，进行中）：前台
> WAV、命令 capture、会议 stream、PCM playback 与 alarm burst 现在统一向 Audio
> Service 发布 wake-word pause request；Service 持有 foreground pause 与外部显式
> pause 两个 reason，按 OR 合并为唯一的 Platform Audio pause 命令。故前台会话结束时
> 只撤销自身 reason，不会意外恢复 provisioning 等调用方明确暂停的 wake-word；反之，
> wake-word 在前台 hand-off 完成后恢复，snapshot 同步反映有效 pause。profile adapter
> 仍独占实际 pause acknowledgement、I2S mutex 和 MultiNet 生命周期。四 profile 的
> `audio_service.c`、`device_api.c`、`alarm_manager.c` Xtensa syntax compile 与 HAL
> boundary check 通过；完整 link/HIL 仍待恢复本机 ESP-IDF 环境。

> 2026-08-10 Audio Service wake-pause hand-off 顺序补强（四 profile，进行中）：审计
> 发现会议 stream 与 decoder PCM playback 原先在 Platform Audio 成功 claim I2S/codec
> 后才发布 shared foreground pause，可能与 MultiNet 的下一次 read 留下短暂竞争窗口。
> 现 service 在调用 `platform_audio_stream_start()` / `platform_audio_playback_begin()`
> 前先发布 pause request；若 physical transaction 失败则逆序撤销该 foreground reason、
> lease 和 session admission。profile adapter 仍执行它自身既有的 pause acknowledgement
> 与 mutex wait，故不修改任一硬件的 I2S/codec 时序，仅把 shared policy 的可见性提前。
> 四 profile `audio_service.c`、`device_api.c`、`alarm_manager.c` Xtensa syntax compile
> 与 HAL boundary check 通过；完整 link/HIL 仍待可用 ESP-IDF 环境。

> 2026-08-10 圆屏 profile-selection seam 收口（EchoEar-2ST / Waveshare 1.75C）：继续拆分圆屏共享 `board_port.c` 的硬件选择后，新建其私有 `boards/round_profile_adapter.h`。该 seam 是唯一持有“选中的圆屏 profile”宏的位置，选择 EchoEar 或 Waveshare 的 display/peripheral/layout adapter，并以私有的 `ROUND_PROFILE_*` 描述向共享 renderer 提供 panel 尺寸、codec/I2S 接线、音量寄存器、16 kHz PCM contract、麦克风 slot 和 layout descriptor。`board_port.c` 不再包含二选一的 profile header、不再定义/重映射 EchoEar/Waveshare GPIO、controller、codec 宏，也不再含 Waveshare standby-pet 与启动日志分支；其 capture/wake mixer 同样改为消费 profile-neutral slot descriptor，诊断不再假定四个 EchoEar slot。业务、Device/Platform 公共头和场景状态均未变化；该 header 仍为 profile-private translation boundary，不能被业务层包含。EchoEar/Waveshare 已通过 `tools/build-profile.cmd` 构建和 HAL boundary check。它是把共享 round renderer 的**硬件选择**集中到私有 seam 的增量，不是完整 Display Task/Audio Service：共享 source 仍拥有 I2S/codec session、CST 手势轮询和 framebuffer/DMA orchestration，后续须按 Phase 5/6 将这些实现拆至独立 Display/Input/Audio HAL source，并对两块实机做 PCM、触摸与场景 HIL。

> 2026-08-10 圆屏 renderer 遗留硬件实现清理（EchoEar-2ST / Waveshare 1.75C）：审计共享圆屏 `board_port.c` 时发现，CO5300 亮度的旧实验实现仍藏在嵌套 `#if 0` 中；虽然不会参与镜像，却保留了 shared renderer 对 Waveshare vendor driver、DMA release retry 与 LEDC 物理细节的第二条死代码路径，未来恢复时会绕过 profile-private adapter。现已删除该死代码，圆屏 renderer 只保留 `round_display_adapter_apply_brightness()` 调用；EchoEar PWM 与 Waveshare CO5300 polling/retry 均只存在于各自 `boards/<profile>` adapter，主 renderer 不再携带被废弃的 controller-specific 替代实现。该变化不改 Device API、业务状态或可见布局，也不将 Display Task/runtime renderer restart 误报为完成。EchoEar 与 Waveshare 已通过 `tools/build-profile.cmd` 重新构建并通过 HAL boundary check；Bread/Fangtang 不编译该圆屏 source，本轮无需因该移除重建。仍需继续将圆屏共享 renderer 顶层的 profile-selection include/宏、codec/I2S bring-up 拆为独立 Display/Input/Audio HAL source，才能达到 Phase 5/6 的完整独立 renderer/Audio Service 退出条件。

> 2026-08-10 冷启动网络根 partial-init / partial-stop ownership 补强（全 profile 共享 `main.c`）：复核 `init_network_core()` 与唯一冷启动 rollback root 后，收紧两项此前仍可能把残留 singleton 当作可用网络的边界。其一，`s_network_initialized` 现在仅在它与 `s_netif_initialized`、`s_default_event_loop_created` 三者同时成立时才允许 fast-path 成功；若 rollback 已删除 default event loop、但 `esp_netif_deinit()` 失败而保留 netif owner，则该 ready bit 不再允许后继调用继续初始化/使用半拆卸 core，必须 fail-closed。其二，`esp_wifi_init()` 失败也进入 `stop_connectivity_root_transaction()`：即使还没有发布 Wi-Fi driver owner，也会回收/关闭已创建的 Connectivity Service generation、event loop 与 netif；若任一回收失败，精确 ownership flag 保留，下一次 init 继续拒绝创建第二套 singleton。该收口只覆盖失败冷启动 rollback，不声称支持 runtime Wi-Fi restart、APSTA→STA 事务化重建或完整 HIL。已以唯一支持入口 `tools/build-profile.cmd` 构建 Bread `0x335260`、EchoEar `0x3217d0`、Fangtang `0x3309e0`、Waveshare `0x352910` 并通过共享 HAL boundary check。仍需在 fault-injection/HIL 中逐点覆盖 netif、event-loop、Wi-Fi-driver 初始化失败，以及“event loop 已删 / netif deinit 失败”的 retry 拒绝和后续冷重启恢复；构建不证明这些 ESP-IDF singleton 交错时序。

> 2026-08-10 MaClaw GUI 远程亮度唤屏竞态与紧凑屏错误传播补强（全 profile）：复核 `hardware_config.extra.brightness` 的统一链路后，补上“先观测、后唤醒、再确认”的远程管理语义。对于 `1–100%`，`app_ui_apply_remote_brightness()` 先读取按值 `Device Power` snapshot；只有已观察到 `DISPLAY_OFF` 才调用 `remote_control` 唤醒。若该调用未返回成功，则再次读取物理观察：另一个前景渲染已先恢复面板时仍接受本次亮度更新；面板仍为 `DISPLAY_OFF` 时返回 `BUSY`，Gateway 不 ACK 也不持久化一个不可见的设置，保留原消息供重试。该路径不模拟触摸/按键，不创建录音，不改变 sleep schedule 的人工唤醒 override；亮屏时远程调节也不取消既有 idle deadline，0% 仍只执行背光关闭。与此同时，Bread/Fangtang 共用紧凑 renderer 的 `board_port_set_display_brightness()` 不再吞掉 profile adapter 的 PWM 失败：现在回滚内存亮度并向 `Platform Display → Device API → Gateway` 返回错误，防止 GUI 把未写入硬件的亮度持久化为成功。EchoEar/Waveshare 原本已有错误回传。已使用唯一支持入口 `tools/build-profile.cmd <profile> build` 完成 Bread `0x335260`、EchoEar `0x3217d0`、Fangtang `0x3309e0`、Waveshare `0x352910` 四 profile 增量构建，并通过 HAL boundary check；尚未针对本增量刷机/HIL。实机验收须分别在 COM3/COM4/COM5/COM6 覆盖“DISPLAY_OFF → GUI 非零亮度 → 原待机画面亮起且不录音 → 再次按既有 timeout 熄屏”，并注入/观测 wake 与前景 repaint 交错、PWM/DCS 写失败、0% 及亮屏调亮度不延时。

> 2026-08-10 启动回滚的装饰 Display Worker admission 收口（全 profile）：审计 `startup_stop_local_workers()` 的既有“stop/join board background tasks”后发现，round renderer 的 deferred pet worker、以及 Bread/Fangtang 的 remote-pet / thinking-mouth workers 可以在 rollback 已 join 旧 task 后，被迟到的 UI state publication 再次创建；这会让已进入 DEGRADED 的 generation 重新持有 LCD mutex/renderer task，破坏 rollback 的线性化。现将各 board port 的同一 background-task mutex 同时作为**创建与停止 gate**：rollback 取得 gate 后先关闭 admission，再通知并 join 已存在 worker；任何在 gate 外早已采样状态的 UI 调用，在取得 gate 后也会重新检查 closed state，不能在 join 后 resurrect worker。此改变只管理装饰性 renderer worker 的 admission/stop，不释放 panel、audio、I2C、framebuffer 或宣称 display renderer/runtime board-port restart；诊断/启动失败表面仍可使用。四 profile 已以 `tools/build-profile.cmd` 通过公共 HAL boundary check 与 build：Bread `0x335260`（余 `0x6ada0`）、EchoEar `0x3217d0`（余 `0x7e830`）、Fangtang `0x3309e0`（余 `0x6f620`）、Waveshare `0x352910`（余 `0x1ad6f0`）。尚需故障注入/HIL 交错“UI pet/thinking publish ↔ rollback gate ↔ LCD DMA”，证明超时、重复 stop 和冷启动恢复时均无 late task 或资源竞争。

> 2026-08-10 共享 HAL 头文件防回归收口（四 profile）：审计确认 `device_api.h`、`board_profile.h`、`app_ui.h` 与全部 `platform_*.h` 已只暴露 ISO-C 值类型；`esp_err_t`、FreeRTOS handle、ESP timer/NVS handle、`cJSON *`、GPIO/I2C/I2S/UART port 类型以及 ESP-IDF/FreeRTOS/driver include 均不允许进入这些共享边界。Gateway tool registry 的 `cJSON`/`esp_err_t` 仍是 Gateway compatibility boundary，**不是** Device/Platform/HAL，未将其误迁入硬件层。新增 `iot-agentos/tools/check-hal-boundaries.ps1` 做结构化防回归扫描；唯一受支持入口 `tools/build-profile.cmd <profile> build` 现在先执行此检查，再配置/编译所选 profile，因此未来为新硬件添加 adapter 时无法把 vendor/RTOS 对象反向泄漏进公共业务契约。2026-08-10 已分别通过 Bread `0x335220`（余 `0x6ade0`）、EchoEar `0x3217c0`（余 `0x7e840`）、Fangtang `0x3309d0`（余 `0x6f630`）与 Waveshare `0x3528f0`（余 `0x1ad710`）完整 build；`project_description.json` 同时确认板型私有依赖闭包仍精确为 Bread 无额外 vendor、EchoEar ST77916、Fangtang NV3023/ML307/UHCI/MQTT、Waveshare CO5300/CST9217/esp_lcd_touch。该证据只证明公共头边界和构建图，**不**证明 renderer restart、完整 Wi-Fi/APSTA 生命周期、LIGHT/DEEP_SLEEP 或实机 HIL 已完成。

> 2026-08-10 profile-private 受管依赖与可复现构建闭环（四 profile）：完成“业务层/共享 HAL 不因可选硬件驱动而携带其它板型依赖”的构建侧收口。`main/idf_component.yml` 现只保留所有镜像共同需要的组件；EchoEar、Fangtang、Waveshare 的 controller/touch/cellular 依赖分别移入 `profile_components/<profile>_deps/idf_component.yml`，由唯一受支持入口 `iot-agentos/tools/build-profile.cmd <echoear-2st|bread-compact|fangtang-4g|waveshare-amoled-1.75c> <idf.py action>` 在 Component Manager/Kconfig 之前选择。ESP-IDF 早期 requirement expansion 会启动独立的 `cmake -P`，不会继承顶层 `-D MACLAW_PROFILE` cache；共享 `main/CMakeLists.txt` 因此在该 pass 从 wrapper 环境恢复同一 profile identity，令 `main` 对其实际 include 的 private driver 显式 `REQUIRES`。Fangtang 的上游 `esp-ml307` 又把公开的 `at_uart.h` 所需 `uart_uhci.h` 标成 private，当前通过 Fangtang carrier 与 `main` 的显式 `78__uart-uhci` requirement 补足 include visibility，未修改易被 Component Manager 重建的 vendor checkout。顶层 CMake 将 Component Manager 的 **build property** 绑定为 `dependencies.lock.<profile>`；四个 lock 各自保存闭包，避免最后一次本机构建改写共享 lock 并影响另一板 CI。
>
> 2026-08-10 验证证据：用上述 wrapper 完整 configure/build 均通过。Bread Compact：`0x335220`（16 MiB app 分区余 `0x6ade0`），图中不含 ST77916/CO5300/CST9217/NV3023/ML307/UHCI/MQTT；EchoEar-2ST：`0x3217c0`（余 `0x7e840`），仅含/由 main 显式要求 ST77916；Fangtang-4G：`0x3309d0`（余 `0x6f630`），仅含 NV3023、ML307、UHCI 与 ML307 的 MQTT 传递依赖；Waveshare 1.75C：`0x3528f0`（32 MiB factory app 分区余 `0x1ad710`），仅含 CO5300、CST9217 与 CST9217 的 `esp_lcd_touch` 传递依赖。对 vendor header 的源码引用也只位于对应 profile-private adapter（ML307 transport 属 Fangtang profile source）。本项证明依赖图、头文件可见性与四种镜像可编译；不等于 runtime renderer 解耦、完整 board-port restart、OTA、LIGHT/DEEP_SLEEP 或硬件在环验收已经完成。不要使用裸 `idf.py build` 作为发布/CI 入口；它不具备 profile 的 manifest/lock 选择语义。

> 2026-08-10 MaClaw GUI 远程亮度唤屏语义收口（全 profile）：Hub 下发 `hardware_config.extra.brightness` 不再直接穿透 `Device Display → Platform Display`。统一改由 `app_ui_apply_remote_brightness()` 作为业务入口：当亮度为 **1–100%** 且 Power Service 已观察到 `DISPLAY_OFF` 时，以新增的 `remote_control` 唤醒原因串行执行 `Device Power → Power Service → Platform Power → board port`，由既有 profile adapter 恢复 `DISPON + 记忆亮度` 并重绘原待机 scene，随后才应用新的亮度；成功后 UI 重新建立正常的 `screenSleepSeconds` idle deadline。该原因不合成触摸/实体键、不会进入命令录音或调用 `sleep_schedule_service_note_manual_wake()`，也不会因设备本已亮屏而重置现有 idle deadline。亮度 **0%** 保持原有“只更新/关闭背光、系统继续运行”的语义，不会反向唤屏。四个 profile 均无板型判断或 GUI 业务逻辑泄漏到 board port；验证构建通过：EchoEar-2ST `0x3217c0`（余 `0x7e840`）、Bread Compact `0x335230`（余 `0x6add0`）、Fangtang-4G `0x3309d0`（余 `0x6f630`）、Waveshare 1.75C `0x308c00`（余 `0x97400`）。尚未因本项刷机；HIL 应在 COM3/COM4/COM5/COM6 分别覆盖“等待 DISPLAY_OFF → GUI 设为非零亮度 → 待机画面亮起且无录音 → 到期再次熄屏”，以及“亮屏时调亮度不延长原 idle deadline、0% 不唤醒”。

> 2026-08-10 DISPLAY_OFF 物理熄屏复核与修复（COM3/COM4/COM5）：对 EchoEar-2ST、Bread Compact、Fangtang-4G 均已部署当前镜像（仅 App 分区 `0x10000`，esptool 写后 SHA 校验通过；端口严格对应 COM3/COM4/COM5）。三板均从 Hub/NVS 恢复 `screenSleepSeconds=180`，在无操作约 193–200 秒后分别记录 `display HAL entered DISPLAY_OFF` 与 `idle deadline reached: DISPLAY_OFF entered`，EchoEar 随后 heartbeat 为 `sleeping=yes`。本轮进一步修正各 profile-private display adapter 的物理事务顺序：对有独立 PWM 背光的 EchoEar（GPIO41）、Bread（GPIO42）、Fangtang（GPIO13），先强制将背光设为 0，才发送可选的 controller `DISPOFF`；若 SPI/QSPI 控制器命令瞬态忙而失败，仍保留“背光已关”的成功可见结果并输出诊断，唤醒路径继续以 `DISPON + 已记忆亮度` 恢复。这样 Power Service、业务层和 Platform Power 仍只使用统一 `DISPLAY_OFF` 契约，硬件时序完全留在各 profile adapter；该修复不改变 MCU/network/wake-word 的运行状态，也不把 DISPLAY_OFF 误称为 light/deep sleep。EchoEar/Bread/Fangtang 隔离构建通过，App 分别为 `0x321610`（余 `0x7e9f0`）、`0x3350a0`（余 `0x6af60`）、`0x3308e0`（余 `0x6f720`）。已覆盖到期日志与写后启动/ready 观察；最终“肉眼确认三块屏幕已黑”仍应结合现场相机画面验收。

> 2026-08-10 DISPLAY_OFF 实机复核（COM3/COM4/COM5）：针对“EchoEar-2ST、Fangtang-4G、Bread Compact 到达休眠时间后不能关屏”的反馈，按三块设备当前由 Hub 下发并持久化的 `screenSleepSeconds=180`，同时完成了不触摸的完整 205 s 串口观察。三板均先记录恢复/应用 `DISPLAY_OFF armed after 180000 ms`，再在有效 idle scene 中进入同一 HAL 物理事务：EchoEar/COM3 于 `196232 ms`、Bread/COM4 于 `198340 ms`、Fangtang/COM5 于 `195605 ms` 分别记录 `display HAL entered DISPLAY_OFF` 和 `idle deadline reached: DISPLAY_OFF entered`；进入后 heartbeat 仍显示 EchoEar `sleeping=yes`，网络轮询、离线唤醒麦克风与 Fangtang 电池观测继续运行。因此当前问题不是三块硬件缺少定时熄屏实现；当时设备实际配置为 **180 秒**，约在 ready standby 后 180 秒进入的是“仅面板/背光关闭”的 `DISPLAY_OFF`，不是 MCU light/deep sleep。若 GUI/HUB 显示的期望值小于三分钟，需先核对下发的 `screenSleepSeconds` 以及 Hub 是否真的持久化/回传了该值；设备端已在日志中输出明确的最终秒数。此次验证没有改动熄屏逻辑，也未把任一板的 `DISPLAY_OFF` 误报为深度休眠；首次实体键/触摸只唤屏、不同 timeout 设置与定时 rest schedule 的 HIL 仍应独立验收。

> 2026-08-10 CMake 显示控制器依赖按 profile 收口（进行中）：共享 `main/CMakeLists.txt` 不再无条件直连 ST77916、CO5300、NV3023 三个 display controller component。EchoEar profile 仅声明 `espressif__esp_lcd_st77916`，Fangtang 仅声明 `78__esp_lcd_nv3023`，Waveshare 仅声明 `espressif__esp_lcd_co5300` 与 CST9217 touch；Bread 不再携带这些 vendor controller component，仍使用其 profile-private adapter 内的 ESP-IDF generic panel vendor API。controller include 已只存在于对应 profile-private adapter。EchoEar clean-profile incremental build 已通过，app `0x321570`（16 MiB 最小 app 分区余 `0x7ea90`，14%）；Bread/Fangtang 的配置重生成构建仍待完成记录。此项只收口 direct CMake ownership，不等于 renderer 独立、Fangtang legacy renderer bridge 消除、Display Task 生命周期重启或额外硬件 HIL。

> 2026-08-10 HAL 公共 UI 类型泄漏收口（全 profile 共享）：复核 Phase 3 的“公共 UI 不依赖具体渲染库”退出条件时，发现 `app_ui.h` 仍直接包含 `qrcode.h`，并以 `esp_qrcode_handle_t` 作为公共 UI 参数；同时宠物素材 UI facade 仍向调用方返回 `esp_err_t`。现将 QR encoder 保留在 `main.c` 的 provisioning 生产端：producer 在其生命周期内把 encoder handle 转换为有界方阵 module buffer；`app_ui_show_qrcode_modules()` 只接收普通字节矩阵并深拷贝为 replay-owned 数据，拒绝空、非平方或超过 `177×177` 的输入。`app_ui.h` 不再包含 QR/ESP-IDF 类型，宠物素材 facade 也改为 `device_status_t`，legacy `esp_err_t` 映射仅保留在 `main.c` 的兼容调用边界；无调用者的 legacy `board_port_show_qrcode()` 及其 `qrcode.h` 依赖已删除。这样同一 QR matrix 仍经 `Device API → Platform Display → selected renderer` 呈现，闹钟抢占后的 replay 行为不变，但共享 UI 不再持有 encoder/renderer SDK 句柄。EchoEar、Bread、Fangtang 隔离构建均通过：app 分别为 `0x321570`（16 MiB 最小 app 分区余 `0x7ea90`，14%）、`0x335030`（余 `0x6afd0`，12%）、`0x330810`（余 `0x6f7f0`，12%）。EchoEar app-only 镜像已写入 COM3 并通过写后 hash；48 s 串口稳定性 smoke 确认 Gateway ready 持续、无 `esp_timer` stack overflow、无 `rst:0xc`。该 smoke 不等价于配网 QR 的人眼扫码 HIL，也不代表完整 Display Task、运行时 renderer deinit/restart 或三板 provisioning 已完成。

## 1. 文档信息

- 状态：实施中（仅完成兼容 facade 的局部收敛；完整 Device API/HAL/Platform 分层尚未完成）
- 日期：2026-08-09
- 系统名称：MaClaw AgentOS
- 评审轮次：第二十三轮实现状态复核（固定 16 MiB Flash 放弃设备端 OTA，改为 GitHub Release→Hub 版本检查/提醒→用户使用受校验刷机工具更新）
- 适用工程：`iot-agentos`
- 首批正式支持硬件：Bread Compact、EchoEar-2ST、Fangtang-4G
- 稳定 profile ID：`bread-compact-wifi-lcd-v1`、`echoear-2st-r8`、`fangtang-4g-v1`
- 唯一功能与业务行为基线：Bread Compact 当前已经验证的完整功能集合及处理方式。EchoEar-2ST 与 Fangtang-4G 必须逐项对齐 Bread Compact；除通过硬件适配表达的屏幕、输入、音频、连接与电源差异外，不得自行定义另一套功能或业务行为。
- 发布目标：三种硬件的用户可见业务功能完全一致。硬件差异只能影响交互映射、布局、性能预算、反馈通道和连接实现，不能形成三套业务、删减功能或长期 capability 缺口。
- 基线继承规则：所有当前及未来进入 MaClaw AgentOS 正式支持集合的硬件，都必须实现完整 Bread Compact 功能母版；不设置按硬件删减功能的“精简版”正式 profile。无法以适配或替代入口承载全部公共功能的硬件，只能停留在实验/Fake 状态或先修订硬件，不能进入正式支持集合。

### 1.1 实施快照（2026-08-07）

> 2026-08-10 HAL 紧凑屏录音场景 layout descriptor 收口增量（Bread Compact / Fangtang-4G）：新增共享 `compact_recording_layout_t` 与 Bread/Fangtang 各自 profile-private recording layout adapter。共享 `board_port_bread_compact.c` 现在从所选 descriptor 读取录音页的上下强调线、麦克风图标、计时器、波形基线/振幅、实时波形清理带以及 MIC 标签的物理几何；其中 Fangtang 保持已验证的 `240×240` 波形中心 `y=158`、半高 `32` 和底部提示安全区，Bread 保持 `240×320` 的 `y=205`、半高 `42` 布局。进一步复核后，静态/实时波形的两套 `#if Fangtang` 绘制分支已合为同一 descriptor 驱动路径；录音页的共同强调线/图标区域也仅保留一次绘制，避免新紧凑 profile 复制同一 renderer。录音/暂停/会议的业务状态、PCM level 历史、自动停止、Input action 和 Device Display API 均仍由共享层唯一拥有；仍保留与物理单键/三键交互相关的提示文案与分页策略，不能错误地把输入差异当成纯布局而合并。Bread Compact 隔离构建通过：app `0x334be0`（16 MiB 最小 app 分区余 `0x6b420`，12%）；Fangtang-4G 隔离构建通过：app `0x330460`（余 `0x6fba0`，12%）。未刷 COM4/COM5，因此不能以构建替代录音、暂停、实时波形、音量、DISPLAY_OFF 与唤醒期间的实机 HIL；Fangtang 仍经受控 legacy renderer bridge include，尚未获得独立 renderer/audio/input 生命周期。

> 2026-08-10 启动回滚共享 deadline 与 Waveshare 远程宠物显示 HAL 收口（全 profile 共享 rollback；COM6 profile-private layout）：启动失败的 `startup_stop_local_workers()` 现在以 6 s 绝对 deadline 驱动所有 caller-controlled worker/service stop、registry owner stop、portal DNS join、network core 与 Power teardown；每个子事务仅接收父事务的剩余预算，预算耗尽即停止后续 teardown 并保留 closed/fail-closed generation。审计确认 `task_registry` owner 内也已使用同一 owner-wide budget，`stop_connectivity_root_transaction()`、`stop_network_core_transaction()` 和 `stop_setup_portal_transaction()` 均只转交余量；`httpd_stop()` 仍是 ESP-IDF 未提供 timeout 的已知不可控边界，不能把本改动表述为严格端到端 6 s wall-clock 保证。与此同时，Waveshare 1.75C 的远程宠物不再把 Hub 提供的通用 RGBA canvas（含透明 authoring padding）误作可见角色大小：`round_display_layout_t` 新增 profile-private `remote_pet_trim_transparent_padding`，共享圆屏 renderer 对动画全部帧取得一份稳定的 alpha 可见边界再缩放，避免每帧裁剪导致抖动；只有 Waveshare 启用该能力并定义 `200px @ y=126` 的 pet halo 居中舞台，EchoEar 保持原始通用画布语义。该调整没有向业务层泄漏板型判断，也不改变 Hub 资源选择、动画、天气或本地宠物 fallback。Waveshare 隔离构建通过：app `0x352310`（32 MiB factory app 分区余 `0x1adcf0`，34%）；已完整写入 COM6，bootloader、partition、ESP-SR models、app、storage 均完成写后 hash 校验。构建/刷写不能证明透明边界、极端长宽比或多关键帧动画的真实构图，仍须在 GUI 下发可见远程宠物后以相机/HIL 复核；启动 rollback 也仍须用故障注入证明所有 caller-controlled child 在共享预算内返回。

> 2026-08-10 Power composition-root deadline 修正（全 profile 共享 `device_api.c`）：复核 `device_power_deinit(timeout_ms)` 后发现，虽然 `power_service_deinit()` 自身已使用其入参作为 transition join 预算，但随后 `power_lease_service_deinit(timeout_ms)` 又会重新获得完整 timeout；一次公开 Power 停止因此最长可消耗约两倍调用方预算，违反文档中“子层只能消费共享父 deadline、不得重新开始完整 timeout”的生命周期约束。现由 Device API 在关闭 lease admission 的同一时刻建立绝对 deadline；先让 Power Service 消费原始预算，随后只把剩余毫秒传给 Lease Service；若预算已经耗尽则不调用子层并返回 `TIMEOUT`，admission 保持关闭，后续生命周期轮次仍可安全完成既有 lease 的 drain。该改动不宣称完整 runtime power restart、panel deinit/reinit、LIGHT_SLEEP/DEEP_SLEEP 或 GPIO/RTC wake 已实现。Waveshare 1.75C 与 Bread Compact 隔离增量构建均已链接通过：app 分别为 `0x351130`（32 MiB factory app 分区余 `0x1aeed0`，34%）与 `0x3345d0`（16 MiB 最小 app 分区余 `0x6ba30`，12%）；尚需将持有 lease 的 Audio/Alarm/Meeting 事务与 panel DMA/idle callback 交错，测量整个 Device API 调用不越过 caller deadline 的 HIL/故障注入证据。



> 2026-08-10 Power Service transition 等待边界补强（全 profile 共享）：审计 `power_service` 后发现，`esp_timer` 的 DISPLAY_OFF callback、API schedule/cancel、用户唤屏与 domain deadline 唤屏均会以 `portMAX_DELAY` 等待同一 `s_transition_mutex`；该 mutex 下游可等待 panel/DMA，故任何卡死都可能把 timer task 或 App Interaction 的首次唤屏无限阻塞，违反电源 transition 必须有界的约束。现统一改为 1.5 s 有界取得：callback 超时只丢弃已过期的一次性 idle deadline，不对未知新前景自动重试；schedule 返回 `TIMEOUT`；cancel 与两类 wake 保持现状/返回 false 并记录诊断，绝不将 contact 合成为录音或会议动作。现有锁内的 `initialized/stopping/timer identity` 再校验、lease gate 与 adapter 的 final scene eligibility 检查保持不变。Waveshare 1.75C 隔离构建通过（app `0x351ab0`，32 MiB factory app 分区余 `0x1ae550`，34%）；只证明该共享实现可链接，未刷 COM6，因为没有构造真实 DMA 卡住/竞争故障。后续 HIL/故障注入须交错 callback、用户触摸/实体键唤醒、schedule cancel、panel transfer 超时和 power deinit，验证所有 caller 在预算内返回、不会发生迟到熄屏或把唤屏 contact 变成业务输入；本项仍不是 Display Task、panel deinit/restart、LIGHT_SLEEP/DEEP_SLEEP 或 RTC/GPIO wake 实现。

> 2026-08-10 Provisioning 保存后 coordinator 线性化补强（全 profile 共享 `main.c`）：`POST /save` 在 Configuration Service 持久化成功后，现先创建并成功注册 post-save restart coordinator，才向 HTTP 客户端返回“设备将重启”。若 task/gate/Task Registry 分配失败，handler 返回 `500 + Connection: close`，明确告知“配置已保存、请手动重启”，不再以成功文本掩盖旧 portal 仍存活的未知状态。coordinator 的完成语义已覆盖响应 flush、`HTTP admission → httpd_stop → captive DNS join → logical provisioning end → scratch/lease release` 以及随后 reset/取消判定；它不会在 cleanup 前过早从 Task Registry 注销或发出 stopped token。启动 rollback 现先停止/等待该 coordinator；仅在它已经完整退出后才执行自身的 portal stop，因此不会令两个调用者同时对同一 HTTP/DNS generation 做 teardown。若 coordinator 在 1.2 s budget 内不能 join，rollback 保持该 provisioning generation closed/isolated，不继续拆其依赖，避免 teardown UAF 或迟到 `esp_restart()`。该路径仍**不是**完整 Provisioning Service：正常保存仍使用整机 reset 应用 Wi-Fi/AP/DHCP/netif/driver 变更，尚未实现 runtime APSTA→STA restart、portal session authentication/TTL/限流或保存/stop 并发的故障注入 HIL。最新隔离构建通过：Waveshare 1.75C app `0x3517c0`（32 MiB factory 分区余 `0x1ae840`，34%），Fangtang-4G app `0x32fc60`（16 MiB 最小 app 分区余 `0x703a0`，12%）；该两份构建日志仅证明共享实现可编译链接，不能替代 `/save` 的 HTTP/DNS drain 与无线电复位实机证据。

> 2026-08-10 Provisioning 保存后无 coordinator 的 fail-closed 补强（全 profile 共享 `main.c`）：继续审计上项失败支路后，补齐了一个不能靠 HTTP 错误文本解决的所有权缺口：配置已 durable commit、但 post-save coordinator 的 task/gate/registry 创建失败时，旧 portal 不能继续接受 refresh、delete 或第二次 save。`setup_save_handler()` 现先关闭 HTTP admission，再在当前 httpd handler 内返回 `500 + Connection: close` 的“请手动重启”说明；所有现有 GET/POST 和 captive-probe handler 已统一检查这一 gate，因此晚到请求被确定性拒绝。它**刻意不**在 httpd worker 内调用 `httpd_stop()` 或释放 DNS/session/scratch/lease：那会使自己的响应 flush 与 server self-join 竞争，重现原先要避免的竞态。此时唯一恢复动作是人工 reset，重启后从已提交 Configuration snapshot 开始新 generation。Waveshare 1.75C 与 Fangtang-4G 重新隔离构建通过：app 分别为 `0x3517b0`（32 MiB factory 分区余 `0x1ae850`，34%）及 `0x32fc60`（16 MiB 最小 app 分区余 `0x703a0`，12%）。仍需专门 fault injection 让 `xTaskCreate`、registry register 或 lifecycle primitive 缺失发生于 `/save` 后，验证当前响应可见、随后请求均为 503、手动复位后新配置生效；在这之前不得把该构建证据升级为完整 Provisioning Service/HIL 通过。

> 2026-08-10 Waveshare COM6 启动图腾/待机宠物最终交接复核：用户实拍中与天气弧重叠的鹰是 Waveshare profile 的原生**启动图腾**，不是待机宠物，也不是 Hub `petAsset`。为避免它进入统一待机场景，`round_display_adapter_has_startup_art()` / `round_display_adapter_compose_startup_art()` 现成为圆屏 display profile 的中性契约：Waveshare adapter 私有持有鹰素材及其 `96px @ y=20` 几何；EchoEar adapter 显式为 no-op。共享 `board_port.c` 只拥有启动 surface 生命周期；进入 `ready standby` 时废弃启动 frame 的 delta baseline，首个统一宠物 frame 强制全帧提交，成功后才建立下一帧 baseline。这样，在 Hub 没有宠物资源时，统一本地宠物是唯一 standby fallback；在 Hub 有资源时，仍由共享 renderer 按既有优先级合成/动画，Waveshare profile 仅提供 `188px @ y=70` 的安全舞台以避开下方天气弧。COM6 已完整写入该镜像并通过 bootloader、partition、app、模型与 storage 写后 hash 校验；冷启动日志确认 `SERVICE_STATUS.ready=true`、`ready standby`、持续 `idle=yes/sleeping=no` heartbeat，未见 panic、WDT 或 `frame present failed`。当次 Hub handshake 明确为 `startup pet asset ... none`，因此不能把未出现 Hub 云端宠物称作下载或布局失败；仍须在 GUI 下发至少一帧可见宠物后，用相机/人眼验证真实远程宠物与天气弧不重叠。Waveshare 隔离构建为 app `0x351050`（32 MiB factory app 分区余 `0x1aefb0`，34%）；EchoEar 隔离构建为 app `0x31fe80`（16 MiB 最小 app 分区余 `0x80180`，14%）。本项不表示 renderer 可 deinit/restart、设备端 OTA、MCU light/deep sleep 或四板 HIL 已完成。

> 2026-08-10 HAL 紧凑 profile Motion capability fallback 收口增量（Bread Compact / Fangtang-4G）：Bread 与 Fangtang 各自新增 profile-private `*_peripheral_adapter.h`，并分别实现相同的 `compact_peripheral_adapter_get_motion_sample()` 合约。两块正式板当前均无已归一化的 IMU，adapter 对有效输出参数明确返回 `ESP_ERR_NOT_SUPPORTED`；共享 `board_port_bread_compact.c` 不再持有“无 IMU”的默认分支，只无条件委托所选 profile。`Device API → Platform Sensor` 仍先以 profile capability gate 拒绝未声明 `DEVICE_CAPABILITY_MOTION_SENSOR` 的设备，再向业务返回 `UNAVAILABLE`，因此不会误把没有 IMU 的板宣传为支持跌倒检测。Bread/Fangtang 隔离构建通过：app 分别为 `0x333c40`（16 MiB 最小 app 分区余 `0x6c3c0`，12%）及 `0x32f620`（余 `0x709e0`，12%）；未刷 COM4/COM5，不能以构建替代硬件验收，也不表示这些 profile 已具备 IMU、跌倒检测或独立 renderer。

> 2026-08-10 HAL 紧凑屏 standby layout descriptor 收口增量（Bread Compact / Fangtang-4G）：新增 `compact_standby_layout_t` 及 Bread/Fangtang 各自的 standby adapter，集中描述 profile-private 的传输 stripe 高度、天气文本比例/锚点、待机宠物安全舞台和本地宠物比例。共享 `board_port_bread_compact.c` 因而不再把 `LCD_STRIPE_ROWS`、`AMBIENT_PET_TOP`、最大宽度或 Bread 的天气版式作为硬编码/`#if` 事实；同一 descriptor 同时驱动 framebuffer staging 预算、启动 bitmap bands、远程宠物预缩放与动画清空区域。Bread 保持既有 16-row stripe、天气 y=66、宠物舞台 `y=94/w=224`；Fangtang 保持已验证的逐行 NV3023 提交及 `y=62/w=220` 方屏宠物区域。业务层仍统一拥有 weather/pet 状态、Hub 资源选择及动画语义，Fangtang 的两行信息栏与糖块启动图腾仍是该 profile 的现有 renderer transition 内容。Bread/Fangtang 隔离构建通过：app 分别为 `0x333c30`（16 MiB 最小 app 分区余 `0x6c3d0`，12%）及 `0x32f610`（余 `0x709f0`，12%），保留既有 ESP-SR/Kconfig 提示；未刷 COM4/COM5，不能以构建替代天气/宠物构图、逐行传输和唤屏 HIL，也不表示 Fangtang 已脱离 legacy renderer bridge。

> 2026-08-10 HAL 紧凑屏 display adapter facade 再收口增量（Bread Compact / Fangtang-4G）：共享 `board_port_bread_compact.c` 的 panel bring-up、归一化亮度写入及同步 bitmap 提交，现统一只调用 `compact_display_adapter_init_hardware()`、`compact_display_adapter_set_brightness()`、`compact_display_adapter_draw_bitmap_sync()`。Bread adapter 私有持有 ST7789 的 panel IO 和完成信号等待；Fangtang adapter 私有持有 NV3023 的 panel IO、`GRAM Y=80` 寻址及逐行传输，且其 SPI host 与默认亮度一并从共享 renderer 移入 profile。共享层继续唯一拥有 scene、framebuffer、LCD mutex、Display API 以及 `DISPLAY_OFF` 的业务资格判定，不按硬件类型调用 controller API。Bread 与 Fangtang 隔离构建通过：app 分别为 `0x333c10`（16 MiB 最小 app 分区余 `0x6c3f0`，12%）及 `0x32f610`（余 `0x709f0`，12%）；保留既有 ESP-SR/Kconfig 提示。本增量未刷 COM4/COM5，不能以构建替代 ST7789/NV3023 的实机画面、亮度、逐行稳定性或 `DISPLAY_OFF`→输入只亮屏 HIL；Fangtang 仍经 transition bridge include 共享 renderer，不能据此声称其 renderer、audio、input 或生命周期已经独立。

> 2026-08-10 HAL 圆屏可选 power/motion 能力 facade 收口增量（EchoEar-2ST / Waveshare 1.75C）：共享圆屏 `board_port.c` 的 `board_port_get_power_status()` 与 `board_port_motion_get_sample()` 不再以 `CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C` 选择 PMIC/IMU 或“未支持”路径，统一委托同名 `round_peripheral_adapter_get_*`。Waveshare profile adapter 独占 AXP2101 电量/充电状态和 QMI8658 归一化采样的委托；EchoEar profile adapter 显式声明没有校准电源遥测及 IMU，分别返回 telemetry unavailable 与 `ESP_ERR_NOT_SUPPORTED`。共享启动诊断也只在某 profile 实际返回 sample 时记录数值；无 IMU 不再产生误导性 QMI8658 warning。`Device API → Platform Power/Sensor → board port` 的业务语义、Motion HAL 单位和 Fall Detection 的 capability gate 均未变更。EchoEar 与 Waveshare 隔离构建通过：app 分别为 `0x31fdf0`（16 MiB 最小 app 分区余 `0x80210`，14%）及 `0x351060`（32 MiB factory app 分区余 `0x1aefa0`，34%）；保留既有 `compose_text16_curve` 未使用 warning 与 ESP-SR/Kconfig 提示。本增量未刷 COM3 或 COM6，不能以构建替代 PMIC/IMU、跌落识别或电量校准 HIL，也不代表其它三块正式硬件已具备 motion capability。

> 2026-08-10 HAL 圆屏 scene/layout descriptor 再收口增量（EchoEar-2ST / Waveshare 1.75C）：`round_display_layout_t` 新增 profile-private 的 scene reference 尺寸、本地待机宠物 source-centre/缩放基准及可选 flash 工作资格。共享 `board_port.c` 因而不再以 `CONFIG_MACLAW_BOARD_WAVESHARE_S3_TOUCH_AMOLED_1_75C` 判断圆屏场景坐标换算、待机本地宠物几何或是否允许可重建的宠物预览缓存写入；它只消费 descriptor，继续唯一拥有宠物选择、Hub 资源透明合成、动画、天气、时钟与 Device Display API 的业务语义。EchoEar descriptor 保持既有 360px scene、7/8 本地宠物比例和可选缓存写入；Waveshare descriptor 保持 360px authored scene 映射到 466px 面板、7/8 本地宠物比例，并继续因 CO5300 QSPI 与 PSRAM 共享 cache fabric 而拒绝可重建宠物缓存的 flash 写入（不影响必需的录音/存储流程）。两 profile 隔离构建通过：EchoEar app `0x31fc80`（16 MiB 最小 app 分区余 `0x80380`，14%），Waveshare app `0x350fe0`（32 MiB factory app 分区余 `0x1af020`，34%）。现有 `compose_text16_curve` 未使用 warning 与 ESP-SR/Kconfig 提示仍存在；未刷 COM3/COM6，不能以构建替代远程宠物、待机天气或 flash-pressure HIL，也不代表 renderer 可独立 deinit/restart。

> 2026-08-10 Waveshare COM6 待机远程宠物安全舞台复核与收口：实拍中可见鹰图仍为启动图腾，说明设备已从启动 surface 切入统一待机场景；串口同时确认 `ready standby`、`idle=yes`、`sleeping=no`，且本次握手返回的 `startup pet asset` 为 `none`，因此当前没有 Hub 远程宠物可显示，不能把本地图腾或其缺席误判为宠物下载失败。为避免未来 Hub 宠物的竖向素材（尾巴/阴影）压到天气弧，Waveshare 的 profile-private `waveshare_round_layout_adapter.h` 将远程宠物安全舞台从 `220px @ y=88` 收为 `188px @ y=70`；共享圆屏 renderer 仍唯一拥有宠物选择、下载后透明合成、动画与天气绘制，仅消费该几何 descriptor。已隔离构建并完整写入 COM6：app `0x350e90`，32 MiB factory app 分区余 `0x1af170`（34%）；bootloader、partition、ESP-SR models、app 和 storage 均完成写后 hash 校验。刷后冷启动完成 Hub handshake、天气（北京/晴/25°C）和 wake 初始化，连续 display heartbeat 正常，无 panic、WDT 或 `frame present failed`。由于该次 Hub 描述符明确没有 `petAsset`，仍需在 MaClaw GUI 下发至少一帧可见的宠物资源后作一次人眼/相机 HIL，确认缩小后的宠物与下方天气弧不重叠；这不代表 OTA、完整 renderer restart 或 MCU sleep 已实现。

本节只记录已经由源码和构建验证证明的增量，不以局部改动替代本计划的最终退出条件。

> 2026-08-10 HAL Fangtang-4G ML307 电气 bring-up 拆分增量：新增 `boards/fangtang_4g/fangtang_cellular_adapter.h`，将 ML307 的 UART GPIO 有效性、guard GPIO 的下拉/输出电平、modem power GPIO 的 active level、500 ms 硬件稳定时间以及 UART baud/APN 到 `ml307_transport_start()` 的具体绑定收口为 Fangtang profile-private adapter。`board_port_fangtang_4g.c` 仅保留中性 `board_port_prepare_cellular_transport()` / `board_port_start_cellular_transport()` facade；共享 Connectivity/业务层继续只决定何时选择蜂窝网络、何时发起/取消 HTTP，不获得任何 Fangtang pin、电平、APN 或等待策略。Fangtang 隔离构建通过：app `0x32f650`，16 MiB 最小 app 分区余 `0x709b0`（12%）；Bread 回归构建通过：app `0x333b60`，余 `0x6c4a0`（12%）。仍存在既有 Fangtang bridge 对 `board_port_bread_compact.c` 的 include，未形成独立 renderer/audio/input 生命周期；也未刷写 COM5，不能以构建替代 ML307 的上电、Wi-Fi/4G 切换、HTTP/stream/取消和低电量并发 HIL。

> 2026-08-10 HAL 圆屏实体激活键物理入口拆分增量（EchoEar-2ST / Waveshare 1.75C）：共享圆屏 `board_port.c` 不再持有 GPIO0、上拉配置或 active-low 判定；它只消费统一的 `round_input_adapter_init_activate_key()`、`round_input_adapter_activate_key_pressed()` 与诊断 GPIO 标识，并继续作为短按、长按、双击、触屏合并、`DISPLAY_OFF` 首次 contact 只亮屏及 Device Input action 发布的唯一业务状态机。EchoEar profile adapter 独占其 BOOT GPIO0 的可用性说明、上拉与低电平按下事实；Waveshare peripheral adapter 独占 1.75C 激活/BOOT GPIO0 的同一物理合同。该增量没有改动手势阈值、按键与触屏的业务映射、GPIO ISR/interrupt 策略或启动/反初始化生命周期。EchoEar 与 Waveshare 隔离构建通过：app 分别为 `0x31fb50`（16 MiB 最小 app 分区余 `0x804b0`，14%）及 `0x350e80`（32 MiB factory app 分区余 `0x1af180`，34%）；项目既有未使用 `compose_text16_curve` 与 ESP-SR/Kconfig 提示仍存在。仍缺 COM3/COM6 实体键与触屏在待机、录音取消及 `DISPLAY_OFF` 下的 HIL，不能以构建替代实机输入验收。

> 2026-08-10 Waveshare COM6 启动图腾与待机宠物场景交接修复：复核实拍与启动日志后，确认鹰图为 profile-private 的启动图腾，并非 Hub `petAsset`；此前启动 frame 可能作为 delta/front-frame 基线遗留到待机，视觉上误像“没有宠物”，且其羽毛下缘会与下方天气弧重叠。Waveshare display profile 现将图腾从 `164px` 收至 `128px`、顶部锚点由 `34` 调为 `42`，只用于启动独占 surface；共享圆屏 renderer 以 `s_startup_surface_visible` 明确启动→待机交接，首个宠物 frame 强制全帧提交，成功后才建立 ambient delta baseline。因此无 Hub 资源时显示统一的本地待机宠物；有 Hub 资源时仍由既有 2-frame 适配/透明合成的远程宠物优先。Waveshare 隔离构建通过：app `0x350e90`，32 MiB factory app 分区余 `0x1af170`（34%）；已完整写入 COM6，bootloader、partition、ESP-SR models、app 和 storage 均完成写后 hash 校验。刷后 55 秒串口观察到 `ready standby` 与持续 `idle=yes`、`sleeping=no` 的 display heartbeat，未见 panic、WDT 或 frame present failure。仍需相机/人眼确认启动图腾尺寸及 Hub 宠物下载完成后的真实构图；首批宠物资源为后台下载，冷启动待机先展示本地宠物是预期行为。

> 2026-08-10 HAL `DISPLAY_OFF` 后首帧完整重绘保护增量（Bread Compact / EchoEar-2ST / Fangtang-4G / Waveshare 1.75C）：审计四个 profile 的 framebuffer/delta 约定后，已将 `DISP OFF` 视为 panel GRAM 不可信的物理边界。圆屏共享 renderer 在 adapter 成功进入 `DISPLAY_OFF` 及成功恢复 `DISP ON` 后统一废弃 ambient front-frame 与 recorder baseline；下一次宠物、录音或前景场景只能先走既有 full-frame presenter，成功后才重新建立 delta 基线。Bread/Fangtang 共用 renderer 亦在 adapter 成功进入 `DISPLAY_OFF` 后废弃 front snapshot，Bread 的 stripe delta presenter 因而在首次恢复绘制发送每一行；其 wake transaction 改为先确认 adapter 成功才转换 display state，失败保持 off 并向调用方返回失败。四个隔离 profile 均已重建通过：EchoEar app `0x31fb30`（16 MiB 最小 app 分区余 `0x804d0`，14%）、Waveshare `0x350df0`（32 MiB factory app 分区余 `0x1af210`，34%）、Bread `0x333b60`（余 `0x6c4a0`，12%）、Fangtang `0x32f6c0`（余 `0x70940`，12%）。Waveshare 已完整写入 COM6，bootloader、partition、模型、app、storage 写后 hash 均通过；冷启动日志确认 `SERVICE_STATUS.ready=true`、`LOCAL_READY`、Hub handshake、40% brightness 回放和持续 idle pet heartbeat，未见 panic/WDT。此处的“完整重绘”是由 cache invalidation 到 full-frame branch 的代码合同和构建证明；仍缺 COM3/4/5/6 分别执行 `DISPLAY_OFF → 触控/实体键只亮屏 → 首帧` 的人眼/相机 HIL，且不代表 panel deinit/restart、MCU LIGHT/DEEP_SLEEP 或 RTC/GPIO wake source 已实现。

> 2026-08-10 HAL 圆屏 panel bring-up 物理事务收口增量（EchoEar-2ST / Waveshare 1.75C）：两个圆屏 display adapter 现各自实现同名 `round_display_adapter_init_hardware()`，唯一拥有 backlight 初始化、SPI/QSPI bus、panel IO、controller creation、reset/init、`DISP ON` 和初始归一化亮度的完整物理事务；共享圆屏 `board_port.c` 只向 adapter 提供 transfer completion plumbing、最大传输预算和接收 opaque panel/IO handle，继续独占 framebuffer、场景和 Device Display API 语义。该收口没有引入 panel deinit/restart 或把 renderer 伪装成 restartable Display Service。EchoEar 与 Waveshare 隔离构建通过：app 分别为 `0x31fb00`（16 MiB 最小 app 分区余 `0x80500`，14%）和 `0x350db0`（32 MiB factory app 分区余 `0x1af250`，34%）。Waveshare 已写入 COM6，esptool 对 bootloader、partition、模型、app 与 storage 的写后 hash 均通过；重启串口观察到 `SERVICE_STATUS.ready=true`、runtime `LOCAL_READY`、在线握手、40% CO5300 brightness 回放及持续 pet/display heartbeat，未见 panic/WDT。该证据不替代 COM3 的实机 bring-up，也不替代 COM6/COM3 的 DISPLAY_OFF→触屏/按键只亮屏 HIL。

> 2026-08-10 HAL 圆屏 DISPLAY_OFF 物理事务拆分增量（EchoEar-2ST / Waveshare 1.75C）：两个圆屏 display adapter 现各自实现同名 `round_display_adapter_enter_display_off()` / `round_display_adapter_wake_from_display_off()`。EchoEar adapter 独占 ST77916 `DISP OFF/ON` 与 PWM 背光顺序；Waveshare adapter 独占 CO5300 `DISP OFF/ON` 与 DCS 亮度事务，且继续保留其 colour-DMA 后总线释放重试。共享圆屏 renderer 仅保留场景资格、DISPLAY_OFF 状态、framebuffer 失效和“首次 contact 只恢复待机画面”的业务语义；所有待机、消息、二维码、回复、图片回复和闹钟的由 DISPLAY_OFF 进入绘制的路径均通过同一 wake helper，不再在共享层散落 panel/brightness 物理调用。EchoEar 与 Waveshare 隔离构建均通过：app 分别为 `0x31fc80`（16 MiB 最小 app 分区余 `0x80380`，14%）和 `0x350f40`（32 MiB factory app 分区余 `0x1af0c0`，34%）。本增量不表示 MCU light/deep sleep、RTC/GPIO wake source arm、panel deinit/restart 或 COM3/COM6 DISPLAY_OFF→实体键/触屏只亮屏 HIL 已完成。

> 2026-08-10 HAL 紧凑 DISPLAY_OFF 物理事务拆分增量（Bread Compact / Fangtang）：Bread 的 ST7789 与 Fangtang 的 NV3023 display adapter 现各自实现同名 `compact_display_enter_display_off()` / `compact_display_wake_from_display_off()`，独占本板 panel `DISP OFF/ON` 与 PWM backlight 的电气顺序；共享 compact renderer 仍独占环境场景资格检查、framebuffer 失效、显示状态和首次 contact 只恢复待机画面的统一语义，只调用 adapter 的中性事务。Fangtang adapter 明确 DISPLAY_OFF 不触及 ML307、ADC 或 MCU，Bread adapter 同样只处理 panel/backlight，因此没有把未经验证的 LIGHT/DEEP_SLEEP、RTC/GPIO wake 或电源轨关闭伪装成已实现。Bread 与 Fangtang 重新隔离构建通过：app 分别为 `0x333b70`（16 MiB 最小 app 分区余 `0x6c490`，12%）和 `0x32f5e0`（余 `0x70a20`，12%）。仍缺 COM4/COM5 的 DISPLAY_OFF→实体键只亮屏 HIL、Fangtang 与 modem/charger 的并发验证，以及完整 Power/Wake PREPARE→COMMIT、light/deep-sleep 与 board deinit。

> 2026-08-10 HAL 紧凑 direct-I2S 声学与驱动物理合同收口增量（Bread Compact / Fangtang）：`boards/bread_compact/bread_audio_adapter.h` 与 `boards/fangtang_4g/fangtang_audio_adapter.h` 现各自唯一持有 direct-I2S 的 RX/TX I2S port、WS/BCLK/DIN/DOUT、32-bit MSB mono capture、16-bit Philips stereo playback、16 kHz clock、DMA descriptor/frame 数及 TX auto-clear 的物理事实；两块板的命令起音/静音与 wake threshold/gain 校准亦随各自 microphone path 下沉。共享 `board_port_bread_compact.c` 仍只拥有一套 PCM 转换、capture/playback/wake 会话状态机，并通过中性 `compact_direct_i2s_audio_init()` 和校准别名消费所选 profile，不再把 Bread 默认阈值作为 Fangtang 的隐式回退。Bread 与 Fangtang 重新隔离构建通过：app 分别为 `0x333aa0`（16 MiB 最小 app 分区余 `0x6c560`，12%）与 `0x32f5d0`（余 `0x70a30`，12%）。本项未创建 Audio Service，未实现 I2S/codec deinit/restart 或资源 manager，且不以构建替代 COM4/COM5 的录音、唤醒、播放、噪声、音量与休眠期间音频 HIL。

> 2026-08-09 HAL 紧凑实体输入物理入口拆分增量（Bread Compact / Fangtang）：新增 `boards/bread_compact/bread_input_adapter.h`，Bread 的 GPIO0 激活键、GPIO38/39 音量键（明确 GPIO37 为 OPI PSRAM 保留）、上拉配置、释放电平和 25/30 ms 去抖、2.5 s 长按、500 ms 双击阈值均已移至 profile-private input contract；Fangtang 的既有 input adapter 同时接管其单 GPIO0 上拉配置。共享 compact scanner 仍是唯一的按压/短按/长按/双击分类与 Device Input action 发布者，只从选定 adapter 消费中性别名，因此既不把手势业务搬进板型文件，也不改变 Fangtang 启动网络选择窗口的现有所有权。Bread 与 Fangtang 隔离构建通过：app 分别为 `0x333b50`（16 MiB 最小 app 分区余 `0x6c4b0`，12%）和 `0x32f6a0`（余 `0x70960`，12%）。本项未使扫描器可 restart、未实现 GPIO interrupt/ISR publisher 或完整 board deinit，亦未替代 COM4 实体激活/音量键、COM5 单键和启动双击的 HIL。

> 2026-08-09 HAL 紧凑矩形显示物理入口拆分增量（Bread Compact ST7789）：新增 `boards/bread_compact/bread_display_adapter.h`。Bread 的 240×320 viewport、ST7789 SPI host/GPIO/时钟、panel IO、reset/invert/on 序列以及 GPIO42 的 5 kHz、10-bit LEDC 亮度换算，现由 Bread profile-private adapter 唯一持有；共享 compact renderer 仅调用 adapter 建立 panel、写入归一化亮度，并继续独占场景、framebuffer、DMA 完成同步与 Device API 语义。Fangtang 继续走自己的 NV3023 adapter，不再共享 Bread 的显示物理定义。Bread 与 Fangtang 隔离构建通过：app 分别为 `0x333b20`（16 MiB 最小 app 分区余 `0x6c4e0`，12%）和 `0x32f6b0`（余 `0x70950`，12%）。本项不改变 Bread 已验证的 50% 默认亮度、天气版式或显示休眠行为；尚未实现 renderer 的独立生命周期、panel deinit/restart，亦未替代 COM4/COM5 的实机画面、亮度和 DISPLAY_OFF/唤醒 HIL。

> 2026-08-09 HAL 圆屏触控入口拆分增量：EchoEar-2ST 与 Waveshare 1.75C 各自实现同名的 `round_touch_adapter_*` 合约。CST8xx/CST9217 的控制器读取、I2C 或 `esp_lcd_touch` 句柄所有权、IRQ GPIO、初始化/逆序释放以及原生 gesture 字节归一化均留在 profile-private adapter；共享圆屏 `board_port.c` 只消费 `pressed + gesture + ready + irq` 的中性结果，继续由同一个手势状态机决定短按、长按、双击、亮屏与命令取消。为保持既有启动语义，EchoEar 的触控控制器不可用仅记录并允许音频继续初始化，而 Waveshare 的同一入口还负责其共享总线上的 PMIC/IMU，因此初始化失败仍按原行为使板级 bring-up 失败；这是适配器的硬件生命周期事实，不是业务分叉。EchoEar 与 Waveshare 已分别重新构建通过：app `0x31fc70`，16 MiB 最小 app 分区余 `0x80390`（14%）；app `0x3500d0`，32 MiB factory app 分区余 `0x1aff30`（34%）。本增量未完成触控 HIL、CST 控制器故障注入、完整 board deinit/restart，亦未替代 COM3/COM6 的手势、亮屏或音频验收。

> 2026-08-09 HAL 显示布局拆分增量（圆屏 layout contract）：新增 `boards/round_display_layout.h` 以及 EchoEar-2ST、Waveshare 1.75C 各自的 `*_round_layout_adapter.h`。圆屏 reply safe-area、ambient 顶/底弧线及 overlay 尺寸/内存域、待机宠物锚点与缩放、远程宠物舞台/帧预算、弧线字形缩放等屏幕几何与资源拓扑事实，现由 profile-private descriptor 提供；共享 `board_port.c` 只消费中性字段，保持同一份状态、时钟、天气、宠物与结果页业务语义。Waveshare 本地图腾仍是没有 Hub `petAsset` 时的 profile-specific display fallback，不会被描述为业务宠物分叉。EchoEar 与 Waveshare 分别重新构建通过：app `0x31fc00`（16 MiB 最小 app 分区余 `0x80400`，14%）及 `0x350010`（32 MiB factory app 分区余 `0x1afff0`，34%）；Bread/Fangtang 未编译此 shared round renderer，现有 profile 构建缓存检查仍通过。此改动未使整个 renderer 独立成可 restart service，未改变圆屏实机画面行为，也不替代 COM3/COM6 文字、宠物、亮度、触控或音频 HIL。

> 2026-08-09 HAL 显示物理入口拆分增量（panel bring-up / brightness）：EchoEar 与 Waveshare display adapter 现各自实现同名的中性入口：backlight 初始化、SPI host/bus/io 配置、panel creation 以及 `0..100` 归一化亮度写入；共享圆屏 `board_port.c` 不再判断 controller 或 PWM 路径来建立面板，也不再直接调用 CO5300 brightness API 或 LEDC duty API。EchoEar adapter 承担 LEDC backlight；Waveshare adapter 承担无背光 AMOLED 的 CO5300 DCS 写入、colour-DMA 后有界 bus-release 重试。共享 renderer 仍只持有 LCD mutex、场景/帧序列化与 Device API 语义。EchoEar 重建通过：app `0x31fbe0`，余 `0x80420`（14%）；Waveshare 重建通过：app `0x350050`，余 `0x1affb0`（34%）。本项不表示 panel/I2C/codec 可 deinit 或 restart，不替代 COM3/COM6 实机 brightness A/B、画面、触控或音频验收。

> 2026-08-09 HAL 物理拆分增量（EchoEar pin/codec/display contract）：新增 `boards/echoear_2st/echoear_hardware_adapter.h`，将 EchoEar-2ST 的 ST77916 QSPI、背光 PWM、实体按钮/CST8xx touch、I²C codec、I²S/PA 与 ES7210/ES8311 address/register/pin/clock-multiple 等物理常量从共享圆屏实现集中到 profile-private hardware contract；本轮进一步将 ST77916 vendor init sequence、QSPI bus/io creation、20 MHz flex-cable limit、PSRAM bounce policy 和 panel creation 一并迁入该 contract。`board_port.c` 保持同一份圆屏 renderer、capture/playback session 与 Device API 行为，只调用 profile helper；Waveshare 不引用该头文件，仍由自身 display/peripheral adapter 定义控制器事实。EchoEar 重建通过：app `0x31fac0`，16 MiB factory app 余 `0x80540`（14%）。该切片不改变已知 EchoEar 音频/噪声修复参数，不替代 COM3 听感、按键/触控、亮度或 display HIL，也没有把圆屏 renderer/audio lifecycle 完整独立化。

> 2026-08-09 HAL 物理拆分增量（Waveshare CO5300 Display）：COM6 的 466×466 CO5300 display contract 已继续从共享圆屏 renderer 下沉至 `boards/waveshare_amoled_1_75c/waveshare_display_adapter.h`：QSPI GPIO、SPI bus flags、最大 transfer budget、32-bit command framing、panel reset/gap 与厂商验证的 init/brightness-control command sequence 现在只在该 profile-private adapter 中。共享 `board_port.c` 仅取得 geometry、opaque `esp_lcd` IO/Panel 创建结果，继续拥有统一业务 scene、framebuffer、DMA serializer 与 Display API 行为；EchoEar 仍保留自己的 ST77916 contract。Waveshare 重建通过：app `0x34ff60`，32 MiB factory app 余 `0x1b00a0`（34%）。本改动没有触碰业务显示、待机宠物、圆屏布局或 DCS 亮度语义；也未使圆屏 renderer 完整独立、display deinit/restart 合法或替代 COM6 的面板/亮度/触控 HIL。

> 2026-08-09 HAL 物理拆分增量（Waveshare PMIC/Touch/IMU/Audio）：将 COM6 Waveshare 1.75C 的 AXP2101、电容触控 CST9217 与 QMI8658 的 I2C address、GPIO、寄存器初始化、触摸 controller 读取、充电状态解码及 ±8g/±1024dps 工程单位换算，从共享圆屏 renderer `board_port.c` 下沉至 `boards/waveshare_amoled_1_75c/waveshare_peripheral_adapter.h`；本轮同一 profile contract 还接管 ES7210/ES8311 的 I²C address、I²S/PA pin、16 kHz/MCLK contract 与音量/静音寄存器事实。共享层现在仅调用本地 adapter 的 `init/deinit/touch_read/power_get/motion_get` 中性入口，仍向统一 Device API 提供原有布尔 touch、归一化电量/充电和版本化 motion sample；没有改变业务手势、跌倒检测策略、显示场景或音频路径。Waveshare profile 重建通过：app `0x34ff60`，32 MiB factory app 分区余 `0x1b00a0`（34%），先前同源码链已 app-only 写入 COM6、esptool hash 校验成功；25 秒冷启动日志达到 `BOOT_STATUS.ready=true`、`SERVICE_STATUS.ready=true`，上报 `battery_level_percent=100`、`charging=true`、motion/fall service available，持续 display heartbeat 且无 panic/WDT/startup degraded。该 header 仍是过渡期的单翻译单元 profile-private contract，不代表 EchoEar/Waveshare renderer 已独立、传感器/codec service 可 restart、触控/充电/IMU/跌倒或音频完整 HIL 已完成。

> 2026-08-09 生命周期实施增量（Firmware Identity 诊断查询）：USB Serial/JTAG 的 `firmware_identity` 查询 worker 现以 Task Registry `DIAGNOSTICS` owner 的 immutable task-handle context 登记，新增 start gate 与 completion semaphore。新 task 在 registry entry 完整建立前不能读取 USB 或响应查询；创建、登记或 gate 释放失败都会关闭 admission、协作唤醒并有界 join，不再留下未受 Registry 管理的短暂 reader。stop 以 stop token 关闭新工作、等待 completion；超时保持 entry 与 task 资源，绝不强杀正在解析诊断请求的 worker。worker 自注销只匹配自身 generation，避免旧 generation 误删后来任务的 entry。启动降级路径仍刻意保留该诊断 reader，供刷机/恢复工具查询；本项不是完整 console/USB/JTAG deinit、Manufacturing Service、runtime restart 或四板 rollback HIL。Waveshare profile 重建通过：app `0x34fe90`，32 MiB factory app 分区余 `0x1b0170`（34%）；项目既有 ESP-SR/Kconfig 提示仍存在。

> 2026-08-09 HAL 物理拆分增量（Fangtang NV3023 思考动画）：在仍以单翻译单元 bridge 过渡的前提下，Fangtang 的三点 `thinking` 局部刷新已从 `board_port_bread_compact.c` 下沉至 `boards/fangtang_4g/board_port_fangtang_4g.c`。profile adapter 现在独占 NV3023 的 45×11 行式写入区域、420 ms refresh cadence、前 framebuffer 修补以及 transfer-failure 的 front-frame 失效处理；共享层仍独占业务 state、LCD mutex、thinking/result/recording 的互斥判断与调用时机，只在持锁条件下调用 profile hook。因此不是把“thinking”业务分叉到 Fangtang，而是把该板确有的 GRAM/行写物理事实移出 Bread 文件。Fangtang 构建通过：app `0x32f550`，最小 app 分区余 `0x70ab0`（12%）；Bread 回归构建通过：app `0x333880`，余 `0x6c780`（12%）。Fangtang adapter 依然通过 `#include "../../board_port_bread_compact.c"` 复用 legacy 实现，不能据此声明 adapter 已独立、renderer/audio/input 已完全解耦或 COM5 的动画/网络并发 HIL 已完成。

> 2026-08-09 HAL 物理拆分增量（Fangtang direct-I2S 引脚合同）：新增 `boards/fangtang_4g/fangtang_audio_adapter.h`，将 Fangtang MEMS 麦克风与扬声器的 WS/BCLK/DIN/DOUT GPIO 引脚从共享 Bread renderer/audio 文件移入 profile-local 硬件合同；共享 direct-I2S 实现继续只消费 16 kHz capture/playback、PCM 与会话仲裁语义。该 header 不创建第二套 audio driver、不改变业务 API，也不宣称 Audio Service、codec/DMA deinit、音频资源仲裁或 COM5 录放音 HIL 已完成。Fangtang 复建仍通过：app `0x32f550`，最小 app 分区余 `0x70ab0`（12%）。

> 2026-08-09 HAL 物理拆分增量（Fangtang MEMS 声学标定）：同一 Fangtang audio profile 现还承载命令起音/静音判定与 wake-only gain/threshold 的校准常量；共享 capture/wake 状态机只通过中性别名读取这些值。该迁移保持既有 1500 ms 静音、160 ms 起音确认、45/55/35/180 级别门限、0.20 阈值与 3/2 wake gain，未改变任何用户可见业务时序；它把“单麦克风瞬态尖峰与灵敏度”归为硬件声学事实，而不是 Fangtang 专属命令逻辑。Fangtang 重建通过：app `0x32f550`，余 `0x70ab0`（12%）。仍缺 COM5 录音、唤醒、噪声与播放的 HIL 证据，不能以构建替代声学验收。

> 2026-08-09 生命周期实施增量（Persistence 消费者停止拓扑）：`weather_cache_service` 与 `meeting_recovery_service` 现均拥有显式 init/admission/deinit/drain 边界；所有 load/save（包含旧 schema 导入的 read→write）只会在已准入期间进入 Persistence，deinit 先关闭新请求、再有界等待已准入调用离开。`startup_stop_local_workers()` 已按依赖顺序先停止联网/会议相关 worker，再依次关闭 Update、Weather Cache、Meeting Recovery、Configuration、Alarm、Sleep Schedule、Fall Detection 等 Persistence 消费者，最后才停止 Persistence worker；因此不再由“Storage owner 停止”抢先切断领域服务最后一次同步 NVS 操作。超时保持相应服务 admission 关闭，不能据此声明 runtime restart；Weather 仍只是 advisory UI cache，Meeting Recovery 不拥有 meeting worker/WAV，且该改动不构成完整 Storage Service 或断电/并发 HIL。Bread 隔离构建通过：app `0x333880`，最小 app 分区余 `0x6c780`（12%）；Waveshare 本轮前已构建/刷入的 app 为 `0x34fca0`，32 MiB factory 分区余 `0x1b0360`（34%）。这些构建不替代 Persistence consumer 与 deinit 并发故障注入、或 COM3/4/5/6 回滚 HIL。

> 2026-08-09 生命周期实施增量（Update Service 的 Persistence 停止边界）：metadata-only `update_service` 现以独立的 admission counter 与串行 operation mutex 管理 Hub metadata、提醒提取、状态查询和 update tools。`update_service_deinit(timeout)` 先拒绝新的调用，再有界等待已准入事务离开；只有 drain 成功才清空内存状态。启动回滚保持“先 Update Service、后 Persistence Service”的依赖顺序，因此已准入的 reminder/dismiss 写入仍可在 Persistence 关闭前完成，新的 metadata/tool 请求则 fail-closed。operation mutex 与 lifecycle lock 为保留的同步 shell，不删除可能被竞态调用方观察到的对象；超时同样保持 admission 关闭，后续 init 不会重开这个未排空 generation。此服务仍仅处理经 Hub 验证后的版本 metadata、检查与提醒：没有 firmware 下载、校验、刷写、OTA 分区、设备重启或完整 Update runtime restart。Waveshare profile 已构建通过：app `0x34f7d0`，最小 app 分区余 `0x1b0830`（34%）；构建不替代 metadata/tool/deinit 并发故障注入或 COM3/4/5/6 HIL。

> 2026-08-08 生命周期验证补记：Input scanner stop/join 的 direct task-notification 实现已重新隔离构建通过：Bread `0x31e2d0`（余 `0x81d30`，14%）、EchoEar `0x312c60`（余 `0x8d3a0`，15%）、Fangtang `0x322310`（余 `0x7dcf0`，14%）、Waveshare `0x315cc0`（余 `0x1ea340`，38%）。这仅证明编译和分区预算；尚未以四台实机覆盖降级启动回滚路径。

> 2026-08-08 生命周期验证补记（装饰任务）：启动回滚现在还会通过 `board_port_stop_background_tasks()` 有界停止已明确登记的装饰性 renderer worker。Bread/Fangtang 的远程宠物动画和（非 Fangtang）thinking-mouth worker、EchoEar/Waveshare 的宠物动画 worker 都使用 task notification 作为 stop token、completion semaphore join；任一 join 超时会保留 task/同步资源并仅记录隔离诊断，绝不强制删除。此接口不会释放 LCD、音频、I2C、framebuffer 或重启 board port，不能视为完整 board deinit；尚未登记的 Gateway、ambient clock、SNTP、会议、配网等 worker 也不在本轮范围。隔离构建通过：Bread `0x31e650`（余 `0x819b0`，14%）、EchoEar `0x312f00`（余 `0x8d100`，15%）、Fangtang `0x322630`（余 `0x7d9d0`，14%）、Waveshare `0x315f80`（余 `0x1ea080`，38%）。

> 2026-08-08 生命周期验证补记（环境时钟）：`maclaw_ambient` 现在使用 task notification 打断其到下一秒边界的等待；退出时清理 task handle 并由 completion semaphore 确认。启动回滚在停止 board renderer 后 join 环境时钟，因此降级路径不会继续触发共享 `app_ui_set_ambient()` 显示副作用。SNTP monitor 没有在此轮停止：它的 `esp_netif_sntp_sync_wait()` 和 SNTP client 生命周期需要与 Connectivity Service 一起设计，不能以强制删除伪造完成。隔离构建通过：Bread `0x31e8a0`（余 `0x81760`，14%）、EchoEar `0x313150`（余 `0x8ceb0`，15%）、Fangtang `0x322830`（余 `0x7d7d0`，14%）、Waveshare `0x3161e0`（余 `0x1e9e20`，38%）。

> 2026-08-08 生命周期验证补记（Fangtang 电源采样）：Fangtang profile adapter 的 `fangtang_power` ADC/充电状态采样任务已加入同一 board background stop/join 链。它以 task notification 退出、completion semaphore join；ADC unit 保持初始化，既不删除 ADC 也不声称板级反初始化完成，因而降级诊断仍可读取最后一次标准化 telemetry snapshot。共享 Bread adapter 只在 Fangtang profile 编译时调用该 profile-private stop hook，其他板不引入 GPIO/ADC 依赖。Fangtang 重新构建为 `0x322960`（余 `0x7d6a0`，14%）；随后 Bread、EchoEar、Waveshare 共享路径也重建通过。仍未以 COM5 覆盖“采样周期等待中触发降级”的 HIL。

> 2026-08-10 生命周期/HAL 边界增强（profile-private peripheral worker stop）：复核后移除了共享 `board_port_bread_compact.c` 对 `MACLAW_FANGTANG_EXTERNAL_POWER_MONITOR_STOP` 和 `fangtang_board_stop_power_monitor()` 的直接条件编译依赖。共享紧凑屏 renderer 现在只调用 `compact_peripheral_adapter_stop_background_tasks(remaining_ms)`：Bread adapter 以无 auxiliary worker 的成功 no-op 实现同一契约，Fangtang adapter 则私有地停止并 join 电池 ADC/充电采样 worker。该调用继续消费 board background stop 的同一个 deadline；超时不删除 task、semaphore、ADC 或 GPIO，仍保持 adapter boot-lifetime，绝不声称 board deinit/restart。隔离构建通过：Bread `0x334be0`（最小 app 分区余 `0x6b420`，12%）与 Fangtang `0x330470`（余 `0x6fb90`，12%）。既有 ESP-SR Kconfig 与 Fangtang 历史未使用函数 warning 未改变。仍缺 COM5 在采样阻塞期触发 rollback 的 HIL，也未将非 renderer/peripheral 的 Gateway、SNTP、portal、会议、codec/wake worker 纳入完整 deinit。

> 2026-08-10 HAL 边界增强（Fangtang power telemetry ownership）：继续下沉后，共享 `board_port_bread_compact.c` 不再持有 Fangtang 的 ADC handle、电池/充电 snapshot、critical-section lock、充电 GPIO 宏或 ADC 采样实现，也不再依赖 `MACLAW_FANGTANG_EXTERNAL_POWER_TELEMETRY` / `...POWER_STATUS_GETTER` bridge 宏。共享 facade 的 `board_port_get_power_status()` 只转发 `compact_peripheral_adapter_get_power_status()`；Bread profile 以 `available=false` 的 no-op 适配，Fangtang profile 私有拥有其 GPIO、ADC、worker、同步对象和标准化 snapshot。`platform_power` 因而仍只获取稳定的 `device_power_telemetry_t` 值，不泄漏 profile 资源。隔离构建通过：Bread `0x334c20`（最小 app 分区余 `0x6b3e0`，12%），Fangtang `0x330480`（余 `0x6fb80`，12%）。构建不替代 COM5 的电池/充电读数、采样 worker 停止后 snapshot 行为或 rollback HIL；该切片也不形成完整 board deinit、ADC deinit 或可重启 power runtime。

> 2026-08-08 生命周期验证补记（Gateway poll）：`maclaw_gateway_poll` 现在受显式 stop token 控制；在轮询、错误退避和空轮询退避时均可被 task notification 唤醒退出。Wi-Fi HTTP path 会在 join 前经独立 client-pointer mutex 取得当前 poll client 并调用 `esp_http_client_cancel_request()`，避免等待长轮询超时；退出后再清 task handle、completion semaphore join。该 client pointer 只在 poll request 的 perform 区间发布，避免 cancellation worker 与 client cleanup 并发。蜂窝路径不触摸 ML307 私有 handle，仅依赖其既有有界请求超时；其完整 cancel/quiesce 应随 Connectivity Service 实现。本轮仅接入 startup rollback，未将 Gateway poll 宣称为可常规 restart 的 Gateway Service。隔离构建通过：Bread `0x31ec40`（余 `0x813c0`，14%）、EchoEar `0x3134f0`（余 `0x8cb10`，15%）、Fangtang `0x322c60`（余 `0x7d3a0`，13%）、Waveshare `0x316570`（余 `0x1e9a90`，38%）；尚未完成网络阻塞中的 rollback HIL。

> 2026-08-09 源码复核结论（本轮新增，优先于旧的“已完成”措辞）：当前工程已经具备可运行的 `Device API` facade、三 profile 选择、输入归一化、`DISPLAY_OFF` 级别的调度/唤醒，以及若干关键 worker 的 stop/join；它**尚不是**完成态 HAL、Connectivity Service 或生命周期平台。四 profile 最近一次隔离构建仍通过，16 MiB 正式三板的 app 余量约为 Bread 14%、EchoEar 15%、Fangtang 13%，但这些数字不是 HIL 或完整回滚证据。下列缺口必须作为后续工作门禁，不能被局部构建通过掩盖：

> 2026-08-09 实施增量（Provisioning 逻辑会话收口）：`Connectivity Service` 现拥有 provisioning 会话的共享逻辑状态，公共 `Device API` 提供 `begin/end/is_provisioning_active/is_pairing_recovery_provisioning` 的硬件中立契约。`main.c` 的命令准入、会议恢复、wake restart、Gateway/蜂窝 recovery、portal 页面模式与启动失败回收不再读取私有的 portal/pairing 全局 flag；由 Service 在临界区一次性发布“会话 active + recovery mode”，失败回收则一次性清除两者，避免后台 worker 对 portal 阶段产生不一致判断。该切片**不**拥有或停止 Wi-Fi、AP/STA、DHCP、DNS、HTTP、event handler、SNTP 或驱动；SoftAP 仍为开放 AP + HTTP 表单，量产安全配网、完整 stop/join、deinit/restart 均未完成。2026-08-09 已以同一源树构建：Bread `0x32f430`（余 12%）、EchoEar `0x31b4f0`（余 14%）、Fangtang `0x32b350`（余 13%）、Waveshare `0x34b870`（32 MiB factory app 余 34%）；仅为构建证据，尚未替代配网/恢复 HIL。

> 2026-08-09 实施增量（Provisioning stop transaction）：`main.c` 现将已有的 portal 资源关闭路径合并为唯一 `stop_setup_portal_transaction()`：先关闭 HTTP admission 并停止 HTTP server，再通知并 join captive DNS；只有两者都成功后才清除 Connectivity Service 的 provisioning 会话、清零/释放表单暂存区并释放 provisioning power lease。任一 stop/join 失败均 fail-closed：保留会话、敏感 buffer 和 lease，不重启 wake-word，避免已返回 DEGRADED 的进程重新恢复命令/Gateway 活动而旧 handler 或 DNS worker 仍在运行。该 transaction 已接入所有 portal 启动失败分支以及 `startup_stop_local_workers()`，所以 force-setup 或 pairing recovery 期间进入启动回滚时也遵循相同顺序。它仍不停止或恢复 AP/STA、DHCP、netif、event loop、Wi-Fi driver 或 SNTP，不能宣称完整 Provisioning Service/可 restart Wi-Fi；HTTP stop 也没有调用方可控 deadline。四 profile 本轮隔离构建通过：Bread `0x32f4a0`（余 12%）、EchoEar `0x31b570`（余 14%）、Fangtang `0x32b3b0`（余 13%）、Waveshare `0x34b8f0`（32 MiB factory app 余 34%）。尚需对 HTTP stop/DNS join timeout、portal 启动中回滚以及 APSTA/4G pairing recovery 进行 COM3/4/5/6 故障注入 HIL。

> 2026-08-09 生命周期实施增量（延迟配网页协调器）：当 force-setup 的持久化写入失败且会议仍在活动时，`maclaw_setup_wait` 不再是无 handle、无 stop token 的 fire-and-forget 任务。它现在作为 `CONNECTIVITY` owner 登记到 Task Registry，创建后先等待 start gate；注册失败会关闭 admission、放行并有界 stop/join，避免任务先运行而尚未受 Registry 管理。会议等待改为可由 direct task notification 打断的 100 ms 分段等待；stop 会关闭 admission、放行 gate、唤醒等待并只在 completion semaphore 确认退出后允许注销。worker 一旦已调用 `enter_setup_portal()`，HTTP/DNS/逻辑 provisioning 会话仍由既有 portal transaction 所有；本项只拥有“等待并派发”的协调器，不能据此宣称 portal、AP/STA、DHCP、Wi-Fi event handler 或 SNTP 已安全停止/可重启。四 profile 隔离构建通过：Bread `0x32ff40`（余 12%）、EchoEar `0x31bfe0`（余 14%）、Fangtang `0x32bdb0`（余 13%）、Waveshare `0x34c390`（32 MiB factory app 余 34%）。尚未覆盖“持久化失败 + 会议活动 + rollback”以及 portal 启动竞态的 COM3/4/5/6 故障注入 HIL。

> 2026-08-09 生命周期实施增量（真实会议 worker）：`maclaw_meeting` 现作为 `AUDIO` owner 的独立 Registry entry，而非只由会议续传/能力刷新协调器间接覆盖。创建后先等待 start gate；登记失败会关闭 admission、放行并 stop/join。stop 令当前 audio stream 在其下一个有界 read 返回，并取消已发布的 Wi-Fi `esp_http_client` 请求；任务在录音块、chunk 边界、PUT write/read 与处理阶段复核 stop token，退出后保留 WAV/recovery metadata 供后续恢复、释放会议 power lease/foreground slot、发 completion semaphore 并以自身 immutable handle 注销。该闭环只提供会议任务的协作式停止，不能中断未知外设 DMA、强杀 ML307 私有请求、删除 retained recording 或使会议服务可重启；Fangtang 蜂窝 stream 目前仍只依赖已有 60 秒有界请求超时，完整 cancel/quiesce 与 upload-in-flight 故障注入仍未完成。四 profile 隔离构建通过：Bread `0x3304d0`（余 12%）、EchoEar `0x31c580`（余 14%）、Fangtang `0x32c390`（余 12%）、Waveshare `0x34c8d0`（32 MiB factory app 余 34%）。

> 2026-08-09 生命周期实施增量（前景语音 interaction）：真实 `maclaw_interaction` 现作为 `INTERACTION` owner 登记到 Task Registry。创建后先等待 start gate，注册失败会关闭 admission、请求 capture stop、取消已发布的 Wi-Fi/蜂窝 foreground HTTP、放行并有界 join；因此不再存在“语音任务已能采集或发网、但尚未被 rollback 管理”的窗口。正常完成、取消、错误和 lifecycle stop 均通过同一个 terminal helper 释放 operation context、power lease、foreground HTTP admission 和 interaction token；原先的可见错误 dwell 与 submit retry 等待改为 task notification 可打断。stop 不跨任务释放 codec/I2S mutex，而是只发出标准 capture stop，让实际任务在其有界 capture 返回后自行释放资源。该项仍不构成完整 Audio/Interaction Service：蜂窝长请求没有强制 abort、board wake recognizer/I2S/codec 未反初始化、显示/UI 回调也尚未迁入统一事件模型。四 profile 隔离构建通过：Bread `0x3309f0`（余 12%）、EchoEar `0x31cab0`（余 14%）、Fangtang `0x32c860`（余 12%）、Waveshare `0x34cdc0`（32 MiB factory app 余 34%）。真实“capture/TLS 阻塞中 rollback”的 COM3/4/5/6 故障注入 HIL 仍待完成。

| 优先级 | 已由源码确认的状态/缺口 | 风险与必须关闭的门禁 |
|---|---|---|
| P0（启动宠物生命周期仍未完全闭环） | `maclaw_pet_startup` 已具备 rollback admission 关闭、retry timer 停止、Wi‑Fi in-flight HTTP client 发布/取消、task notification 停止、completion semaphore 有界 join、退出后 asset TLS client 清理；`startup_stop_local_workers()` 已在 ambient 后、Gateway poll 前接入该顺序。`maclaw_pet_cache` 现使用明确 task owner/start gate；所有 Flash/VFS 写入以页为粒度检查 stop token，调用方在释放其借出的 descriptor/RGB565 帧前轮询并 join，rollback 也单独 drain cache worker，因此 startup worker 的 join 不再被误作 cache 已退出的证明。完成 semaphore 的发布/清理受同一 task-state lock 保护，避免自然退出与 re-arm 混用 token。四 profile 隔离构建通过；COM6 已 app-only 写入、hash 校验并完成正常 startup→8 帧宠物安装。 | 此闭环仍限于宠物域，且 join 超时仅隔离并保留资源，尚无全系统可安全 restart 的生命周期契约。还需为“HTTP/缓存写入阻塞时触发 startup rollback”提供可控故障注入和 COM3/4/5/6 HIL 证据；在其完成前，Task Registry、SNTP/会议/配网/音频等 P0 生命周期缺口仍然阻断完整回滚声明。 |
| P0（完整生命周期门禁尚未满足） | 内部 `task_registry.*` 已存在，并已纳入 `firmware_identity`、`persistence_worker`、`maclaw_gateway_poll`、`maclaw_ambient`、`maclaw_cancel`、`maclaw_volume_nvs`、SNTP retry monitor `maclaw_clock_sync`、真实会议 worker、真实 foreground interaction、会议续传/能力刷新、portal DNS/延迟 setup/restart、wake restart 及 gateway startup 等明确拥有 stop/join 契约的协调器；每项均以 owner、逆注册顺序及 stop/join 成功后 unregister 管理。仍未统一治理 audio/wake-word、ML307 transport、portal HTTP/AP/STA/DHCP、timer/ISR/event handler。 | 在任何 light/deep sleep、profile restart、完整 board deinit 或“Gateway 可恢复服务”之前，继续按真实资源契约迁入；每个 task/timer/ISR/event handler 必须具备 owner、admission、stop、join/drain、超时隔离和资源释放顺序。已局部接入 Registry 不等同完整生命周期平台或可安全 restart。 |
| P0（HAL 物理拆分尚未完成） | `main/boards/fangtang_4g/board_port_fangtang_4g.c` 仍以 `#include "../../board_port_bread_compact.c"` 复用实现；Bread 源内仍有约 82 处 Fangtang profile 条件编译。圆屏 `board_port.c` 仍同时容纳 EchoEar 与 Waveshare 的实现条件。 | Fangtang 不能以“有独立文件名”视为独立 adapter。需先抽出共同的无板型 renderer/audio/input primitive，再让三 profile 分别装配；共享层不得以 `CONFIG_MACLAW_BOARD_*` 表达产品行为。新增硬件验收必须以“不修改既有共享业务与既有正式 profile adapter”为门槛。 |
| P1（Connectivity 尚为 facade/state，非 service） | `connectivity_service.c` 当前主要保存 active-uplink/readiness；`main.c` 仍拥有 Wi-Fi/portal、SNTP、HTTP/Gateway poll、部分 ML307 启停与大量重试/任务创建。蜂窝 poll 也只依赖有界超时，未具备统一 cancel/quiesce。 | 迁入单一 Connectivity/Gateway owner，明确 Wi-Fi/ML307、SNTP、HTTP client、event handler 和 retry worker 的 shutdown/resume 契约；用“长轮询阻塞时降级/prepare”HIL 证明，而非仅依靠 30 秒超时。 |
| P1（Device API cutover 仍有遗留） | 业务侧已无 `CONFIG_MACLAW_BOARD_*` 命中，音量/显示/输入大多经 `device_*`。2026-08-09 已新增内部 `platform_lifecycle` port，`main.c` 不再包含 `board_port.h`，启动 rollback 对板级装饰 worker 的有界 stop 仅通过 `platform_lifecycle_stop_board_background_tasks()` 请求；同时新增 `platform_input` SPI，`input_service.c` 不再引用 `board_port.h`，只经 `platform_input_start/stop()` 安装归一化 action/source publisher、在释放公共队列前有界停止 scanner。`power_service.c` 也已通过窄 `platform_power` SPI 请求/观察既有 `DISPLAY_OFF` 面板事务，而不再直接包含 `board_port.h`。三个 port 都只返回稳定 value/status contract，不暴露 board task、LCD/音频/I2C/GPIO/touch handle 或 deinit/restart 语义。Device API 及其余显示、音频、存储、连接实现仍经单一巨型 `board_port_*` facade。 | 继续把 facade 按 Display/Input/Audio/Power/Connectivity SPI 分拆；任何 port 只能承载其明确的窄职责，不能演变成第二个跨域 board facade，更不能据此宣称完整 board deinit、profile restart 或深睡准备完成。 |
| P1（深睡/硬件唤醒仍未开始） | 源码没有 `esp_sleep_*` 调用；现有 `sleep_schedule`、`wake_deadline` 和 Power Service 只实现运行态 `DISPLAY_OFF`。 | 文档与产品界面只能称“定时熄屏/触摸或按键唤屏”。真正 LIGHT/DEEP_SLEEP、RTC deadline、wake-cause 恢复、GPIO strapping、wake-word 互斥和功耗数据必须作为独立 `PREPARE → COMMIT → resume` 交付。 |
| P1（验证缺口） | 已有多 profile 构建和部分刷写/readback；尚无实际触发 startup rollback、Gateway HTTP 阻塞取消、Fangtang 采样等待、三板 schedule/闹钟优先/第一次触摸或按键只唤屏的完整 HIL 证据。EchoEar 音频仍需人耳验收；COM6 的 IMU 采样不等同于跌倒检测或低功耗唤醒验收。 | 将这些场景纳入同一份可留档的 COM3/COM4/COM5/COM6 验收矩阵，记录固件 digest、端口、脚本、日志、屏幕/音频/功耗证据与失败 attempt。 |

> 2026-08-09 OTA/容量复核：当前 `main/` 未发现 `esp_ota_*`、`otadata`/OTA partition 或设备端 firmware download/install 路径；Update Service 仅保留检查、提醒、稍后提醒和版本忽略。这个状态与 16 MiB 产品决策一致，后续不得以修复生命周期或新增硬件为由重新引入设备端 OTA。

> 2026-08-09 COM6/Waveshare 复核补记：Waveshare 的 466×466 CO5300/QSPI 屏与 PSRAM 共用 S3 cache fabric，远程宠物由 Display HAL 折减为最多 2 帧、240 px 目标尺寸；可重建的 SPIFFS 宠物预览缓存由 board policy 明确拒绝，避免 Flash 写入与 QSPI/PSRAM 显示竞争。AMOLED 亮度只经 board adapter 调用 CO5300 驱动的 DCS `0x51` 编码 API，且在 DMA 总线释放窗口作有界重试，业务层仍只调用统一 `device_display_set_brightness()`。COM6 当前实机日志已确认 40% brightness 的硬件写入、两帧宠物安装及冷启动后的连续 display heartbeat，无 panic/WDT/reboot；**写入日志不是肉眼亮度差的证据**，40%/100% 相机或人眼 A/B 仍待验收。另修正 Hub 环境数据的 durable 边界：环境天气按 machineId（而非可更换的 clientId）保存在 Device Gateway 的持久 credential snapshot 中；Hub 重启或同一机器重新配对新硬件后，首次 handshake 即返回最近一次合法 ambient，不必等待 GUI 的下一次 45 分钟刷新。该数据仍为 GUI 产生的 advisory display snapshot，Hub 不自行查询天气源；持久化失败只记录告警，不撤销当前内存态/在线投递。

> 2026-08-09 COM6 现场问题复核与修复：用户报告的“宠物装不进去、亮度命令到达但肉眼无变化、重配后天气消失”被拆成三个独立责任面。第一项不是单一 2.5 MiB 连续分配需求：当前 profile 在下载前后会因双 framebuffer、TLS 和 wake model 出现 PSRAM 碎片，因此 Display HAL 固定把远程宠物限制为 `2 × 240 px`，公共安装器分别校验聚合外存预算和单次最大分配，且 Waveshare 永不执行可重建 pet SPIFFS cache；冷启动 HIL 已验证两帧 SHA 校验、安装和 wake listener 恢复。第二项此前把通用 MIPI 的 `0x53=0x24` 解释误套到 CO5300；依据 Waveshare 官方 1.75C 初始化序列恢复 `0x53=0x20`、`0x63` 作为 HBM brightness、`0x58=0x00` 关闭 contrast enhancement，并把正常亮度 `0x51` 放到 `DISPON` 之后；COM6 新镜像 ELF SHA `0870fd7ec` 已 app-only 写入并 hash 校验，启动与 Hub 40% 均确认驱动写入值 102。该结论只证明控制器序列、运行稳定和命令抵达；40%/100% 的人眼或相机 A/B 仍是未关闭的视觉验收项。第三项源码与 Hub 实现均已按 machineId 持久化 ambient，并在 handshake 中回送；COM6 冷启动日志确认 handshake 立即接收北京/晴/29°C。因此若 GUI 更换机器绑定后仍看到短暂空白，应以 Hub 实际 machineId 绑定/下一次 GUI ambient 发布诊断，不应再将其归因于 clientId 快照丢失。

> 2026-08-09 生命周期实施增量（Platform Lifecycle port）：启动 rollback 的 composition root 已移除对 `board_port.h` 与 `board_port_stop_background_tasks()` 的直接依赖，改由内部 `platform_lifecycle_stop_board_background_tasks(timeout_ms)` 请求 profile adapter 有界停止**已明确纳入该接口的装饰性后台 worker**。边界以 `device_status_t` 返回，禁止向业务层泄漏 FreeRTOS task、LCD、音频、I2C 或 framebuffer handle；若 join 超时，底层继续保留资源并返回失败，调用者只记录隔离诊断。该切片已在 Waveshare 正式 profile 全量重建通过（app `0x34a690`，32 MiB factory app 余 `0x1b5970`，34%），并写入 COM6；esptool app SHA-256 校验成功，42 秒冷启动日志为 ELF SHA `b954cc9c4`、`BOOT_STATUS.ready=true`、`SERVICE_STATUS.ready=true`，连续 gateway poll 正常，未见 panic、Guru Meditation、task WDT 或 startup degraded。该变更只完成 P1 cutover 的一个狭窄步骤：它不停止输入 scanner、wake recognizer、codec、网络或定时器，不释放或重建 board-lifetime 驱动，不授权 profile restart、light/deep sleep 或完整 board deinit。后续 SPI 拆分必须由 Display/Input/Audio/Power/Connectivity 各自拥有明确的资源和生命周期契约，禁止继续向 `board_port.h` 或该 Platform port 堆叠跨域入口。

> 2026-08-09 HAL 实施增量（Platform Input SPI）：`input_service.c` 已移除对 `board_port.h` 的直接依赖；新的内部 `platform_input_start()` 只安装 adapter 归一化后的 action/source publisher，`platform_input_stop()` 只在公共 Input Service 销毁队列前请求 scanner 的有界 stop。公共 Input Service 继续独占版本化 event envelope、序列号、优先级队列、publisher admission 与 consumer callback，因此 board adapter 仍不能制造公共 event metadata 或执行业务回调。SPI 不携带 GPIO、触控坐标、controller/gesture 配置、FreeRTOS task 或 restart/deinit 语义；它只是兼容期输入适配边界。Waveshare 正式 profile 已全量重建（app `0x34a6b0`，余 `0x1b5950`，34%）并写入 COM6，esptool hash 校验成功；42 秒冷启动为 ELF SHA `85e43449a`，`maclaw_input` 显示 `input service started: control=16 auxiliary=8`，随后 `BOOT_STATUS.ready=true`、`SERVICE_STATUS.ready=true`，未见 panic、Guru Meditation、task WDT 或 startup degraded。该证据仅覆盖 Input Service/scanner 的正常启动，**不**替代触屏手势、实体按键、stop/join 故障注入或全硬件矩阵 HIL；也不使 scanner restart 合法。

> 2026-08-09 HAL 实施增量（Platform Power SPI 与 COM6 AMOLED 复核）：`power_service.c` 已移除对 `board_port.h` 的直接依赖，改经 `platform_power_enter_display_off()`、`platform_power_wake_display()`、`platform_power_display_is_off()` 请求或观察物理面板状态。Power Service 继续独占 deadline、power lease 与 transition mutex；adapter 继续拥有最终场景资格复核、面板熄亮/恢复和状态观测。该 SPI **只**覆盖现有运行态 `DISPLAY_OFF`，不涉及 PMIC/ADC、电源轨、MCU sleep、RTC/GPIO wake 或 light/deep sleep。Waveshare 全量构建及写入后曾以 `BOOT_STATUS.ready=true`、`SERVICE_STATUS.ready=true` 验证正常启动；后续对“日志显示 DCS 0x51 已写、肉眼亮度却未变化”的复核表明，先前把通用 MIPI BCTRL 解释套用到 CO5300 是错误的：最终适配严格恢复 Waveshare 1.75C 厂商序列 `0x53=0x20`、`0x63=0xFF`（HBM brightness）并增加 `0x58=0x00` 关闭 contrast enhancement，正常亮度在 `DISPON` 后仅经驱动的 `0x51` API 设置。最新镜像（ELF SHA `0870fd7ec`）已写入 COM6 且 esptool hash 校验通过；Hub 下发 40% 时驱动记录硬件值 102，宠物安装、wake listener 恢复与持续 display heartbeat 正常。**DCS 事务和运行稳定性已验证；40% 与 100% 的实际可见亮度差异仍须以相机/人眼 A/B 确认，不能只凭驱动日志宣布视觉验收通过。**

> 2026-08-09 HAL 实施增量（Platform Audio SPI）：`device_api.c` 中的输出音量、WAV 播放、闹钟 burst、一次命令采集、会议流式采集、PCM playback transaction、capture/playback cooperative cancel 和离线 wake-word 已改为调用内部 `platform_audio`，不再直接引用对应的 `board_port_*` 音频入口。Platform Audio 保持 Device API 的版本化、硬件中立 PCM/WAV/wake 契约，负责在 adapter 边界完成 `esp_err_t` 到 `device_status_t` 的翻译和 size/argument 防御；它不暴露 ES7210/ES8311、I2S channel、MCLK/BCLK、采样 slot、GPIO、FreeRTOS task 或 codec reset，也不持有 playback/wake 的业务 lease。既有 Device API 继续拥有 AUDIO_PLAYBACK lease，音频 adapter 仍拥有其实际互斥、codec 与 wake-model 生命周期，因而这只是 facade cutover 而不是 audio-service、codec restart 或跨板音质验收完成。Waveshare profile 全量重建为 app `0x34a890`（32 MiB factory 分区余 `0x1b5770`，34%）并写入 COM6，esptool hash 校验成功；ELF SHA `f78852f5f` 正常启动至 `SERVICE_STATUS.ready=true`，输出音量 Hub 回放、40% OLED 亮度配置、离线 wake 启停、2 帧宠物下载/安装与连续 display heartbeat 均正常，未见 panic/Guru Meditation/task WDT/startup degraded。该正常启动日志不替代 COM3/COM4/COM5 的 codec/HIL、人耳语音质量、主动录音/播放或停止路径验收。

> 2026-08-09 HAL 实施增量（Platform Display SPI 与首手势唤屏）：新增内部 `platform_display`，`device_api.c` 的启动、宠物资源预算/安装、录音/上传/回复/二维码/环境/闹钟场景、brightness 与字形缓存均改由该窄 port 代理；Display SPI 只接受稳定值类型与场景意图，保留 `board_port` 对 controller、panel I/O、framebuffer、PSRAM、GPIO 和 renderer worker 的唯一所有权。它不提供 display deinit、framebuffer 借用、触摸坐标、睡眠或重启语义，也不把圆屏/矩形屏差异泄漏到业务层。与此同时，所有 primary interaction 的 `DISPLAY_OFF` 行为统一为“**整次首手势只唤屏**”：首个 contact-down 由 Power Service 原子取消 deadline、恢复面板，后续属于同一触摸/按键手势的 short/double/long completion 被业务边界消费；下一次完成手势才可启动语音或会议。这样避免先前“down 已唤屏但 delayed completed primary 又开始录音”的跨硬件竞态，且离线唤醒词仍是明确的语音操作，不受物理首手势规则影响。Waveshare 32 MiB profile 构建为 `0x34ab70`（factory app 余 `0x1b5490`，34%）并刷入 COM6，esptool hash 校验通过；ELF SHA `0da6f9ae2` 随后达到 `BOOT_STATUS.ready=true` 与 `SERVICE_STATUS.ready=true`，无 panic/Guru Meditation/task WDT。Bread/EchoEar/Fangtang 也已完成同一源级隔离构建；仍需分别以 COM3/COM4/COM5/COM6 在实际已熄屏状态覆盖“单击、双击、长按、首手势后下一手势、闹钟/录音抢占”HIL，才能宣称全矩阵交互验收完成。

> 2026-08-09 HAL 实施增量（Platform Connectivity SPI 与 uplink policy 收口）：`device_api.c` 的蜂窝 prepare/start/readiness/quiesce、HTTP/stream、前景取消、启动网络选择与 Gateway URL 适配已通过内部 `platform_connectivity` 进入 profile adapter；Device API 不再直接调用对应 `board_port_*` 连接入口。进一步将“当前 uplink 选择、启动持久化恢复和启动双击切换”从 Device API facade 下沉至硬件无关 `connectivity_service`：Service 在临界区内先切换 selected-uplink、使新选中链路的旧 ready observation fail-closed，再在临界区外请求 Platform port 下发 profile-local transport hint；无选择变化时不重复触碰 adapter。这样 GPIO0/NVS/ML307 的具体实现仍留在 Fangtang adapter，而业务与 Device API 只处理稳定的选择/ready 语义。Bread 与 Fangtang 隔离构建分别通过：app `0x32ea60`（余 `0x715a0`，12%）及 `0x32a930`（余 `0x756d0`，13%）。这是 facade/policy cutover，不是完整 Connectivity Service：Wi-Fi/portal、SNTP client、HTTP/Gateway poll、ML307 生命周期和跨请求 quiesce 的 owner 仍大多在 `main.c`，尚无真实 4G 切换或长轮询阻塞 rollback HIL；不得据此宣称连接服务可 restart 或可完整 shutdown。

> 2026-08-09 HAL 实施增量（Platform Power telemetry 与 Platform Sensor SPI）：`device_power_get_telemetry()` 已不再直接读取 `board_port_get_power_status()`；它通过 `platform_power_get_telemetry()` 获得统一的只读快照，port 在 adapter 边界完成百分比上限规范化，而 Battery Policy 继续只消费 `available/level/charging` 值，不接触 ADC、PMIC 或 charge GPIO。另新增 `platform_sensor`：Device API 的 motion sample 已不再直接调用 board port，Fall Detection 仍仅消费 ABI 版本化、带时间戳的工程单位加速度/角速度样本；QMI8658 的 I2C/register/range/interrupt 细节继续只留在 Waveshare adapter，未配备 IMU 的正式 profile 统一返回 `UNAVAILABLE`。两个 SPI 都是同步读值边界，不创建 task、不引入传感器/电源 deinit/restart 或 MCU sleep 语义。Bread Compact 和 Waveshare 1.75C 已隔离构建通过：app 分别为 `0x32ea70`（余 `0x71590`，12%）和 `0x34ae70`（32 MiB factory app 余 `0x1b5190`，34%）。构建不等同于 COM6 的 IMU/跌倒或三板电量/充电极性 HIL；这些仍须以真实运动、USB、充电和放电状态验证。

> 2026-08-09 生命周期实施增量（Battery Policy telemetry borrower drain）：共享 `battery_policy_service` 现拥有显式的关闭边界。它在 composition-root rollback 中先关闭新的 policy snapshot admission，再有界等待已经进入的同步 telemetry query 返回，最后才允许 Power Service 停止；若 drain 超时，policy 保持关闭且后续查询 fail-closed，不能重新初始化该代。读 telemetry 后会再次确认 admission，避免已经关闭的 service 发布旧采样值。该 service 没有 task、timer、ADC、PMIC 或 board resource，因此此变更不是 ADC 校准、brownout protection、低电 checkpoint、charger wake、MCU sleep 或完整 Power HAL deinit；它只证明 Battery Policy 不再在 rollback 后调用其已释放的 telemetry provider。Waveshare 与 Fangtang 隔离构建通过，app 分别为 `0x34f350`（32 MiB factory app 余 `0x1b0cb0`，34%）与 `0x32ece0`（16 MiB app 余 `0x71320`，12%）；构建不替代并发 telemetry/deinit 故障注入或真实电量、充电极性 HIL。

> 2026-08-09 生命周期实施增量（Power Lease admission/drain）：Power Lease 不再仅随 Power Service 初始化后永久存活。`device_power_deinit()` 先关闭新的 foreground lease admission，再停止 `DISPLAY_OFF` timer/transition，最后在有界时间内等待已发出的 lease 被其既有 owner 释放；超时保持 admission 关闭并拒绝下一代 init，避免旧 handle 与新 Power generation 混用。slot generation 跨成功 drain 保留，使历史 handle 不会命中后续同一 slot 的首次租约。rollback 顺序也调整为先停止 Alarm/Sleep Deadline 与 Fall Detection（它们可能持有 ALARM/FALL_CONFIRMATION lease），再关闭 lease domain；已开始的 owner 仍可调用 release，不会被关闭操作阻塞。Bread 与 Waveshare 隔离构建通过，app 分别为 `0x333060`（16 MiB app 余 `0x6cfa0`，12%）和 `0x34f4f0`（32 MiB factory app 余 `0x1b0b10`，34%）。这只是 DISPLAY_OFF 业务 lease 的 lifecycle 边界，不创建 power lease worker，不证明 Audio/Gateway/portal 全部已 drain，也不授权 board deinit、restart、LIGHT_SLEEP/DEEP_SLEEP 或任何电源轨操作；仍需并发 lease/deinit 超时与四机 HIL。

> 2026-08-09 HAL 实施增量（Device API board-header removal）：复核后确认 `device_api.c` 的 Display、Audio、Input、Power、Sensor、Storage 与 Cellular 调用均已经对应内部 Platform SPI 或共享 Service；源文件仅遗留无调用的 `board_port.h` compatibility include 及未使用的 `esp_err_t → device_status_t` 转换函数。现已删除这两项遗留，使 Device API facade 本身不再包含或引用 `board_port`。这不是扩大新 SPI 职责：profile、controller、GPIO、I2S、panel、ADC、ML307 与 renderer ownership 仍留在各 adapter，Device API 只保留已版本化的稳定值/状态契约。Bread 与 Fangtang 隔离构建通过，app 分别为 `0x333060`（16 MiB app 余 `0x6cfa0`，12%）及 `0x32ee30`（16 MiB app 余 `0x711d0`，12%）。构建仅证明 header dependency cutover 与链接完整；不代表 `board_port` 已拆完、Fangtang bridge 已独立、Audio/Display/Connectivity 可 restart，亦不替代四机 HIL。

> 2026-08-09 生命周期实施增量（Power startup rollback）：复核 `device_power_init()` 发现其原先先初始化 Power Lease、随后初始化 Power Service；如果后者在 timer/mutex 创建阶段失败，Lease admission 会遗留为开放状态，后续调用可取得没有已就绪 scheduler 的前景 lease。现已在 Power Service init 失败时同步关闭并 drain lease generation，再返回原 Power failure（若关闭异常则优先返回关闭失败），使一次失败的启动不向重试或降级路径泄漏可用 lease domain。Bread 与 Waveshare 隔离构建通过，app 分别为 `0x333070`（16 MiB app 余 `0x6cf90`，12%）及 `0x34f500`（32 MiB factory app 余 `0x1b0b00`，34%）。这只覆盖 synchronous init-failure rollback；尚未构造 timer-creation 失败注入，且不表示 Power 可 runtime restart、完整 service rollback、MCU sleep 或板级资源释放已完成。

> 2026-08-09 HAL 实施增量（Platform Storage SPI）：`device_storage_allows_optional_flash_work()` 已不再直接引用 `board_port`，改由内部 `platform_storage` 代理 profile 的“是否允许可重建、非关键 Flash 工作”物理限制。该 SPI 的唯一当前消费者是宠物预览 cache 的资源预留与写入 admission：业务继续决定宠物是否可选、资源压力和失败处理；Waveshare adapter 继续基于 CO5300 QSPI/PSRAM cache-fabric 竞争拒绝这类装饰性 SPIFFS 重写，Bread/EchoEar/Fangtang 仍可允许它。接口不授予 NVS/SPIFFS/VFS handle、分区布局、cache-disable 操作或 writer/task stop/restart 语义，关键录音、持久化和配置写入不受此 optional gate 阻断。EchoEar-2ST 和 Waveshare 1.75C 已隔离构建通过：app 分别为 `0x31ab50`（余 `0x854b0`，14%）和 `0x34ae80`（32 MiB factory app 余 `0x1b5180`，34%）。构建不替代 COM6 在宠物下载/缓存路径的 HIL，也不代表 Storage 生命周期、原子 journal 或断电恢复已完成。

> 2026-08-09 生命周期实施增量（ML307 HTTP borrower drain）：Fangtang 的 `ml307_transport_quiesce(timeout)` 现在拥有可验证的窄生命周期契约：它先与 HTTP 入场注册使用同一 mutex 关闭 admission，再唤醒等待四个 modem HTTP ID 的请求，停止并 join transport-owned network probe，最后在余下 deadline 内等待**所有已经登记的 HTTP borrower**退出。每个 borrower 在读取 `s_modem`/取得 `AtUart` 前登记，并且其计数直到栈上 `Ml307Request` 析构完成 `MHTTPDEL` 与 URC callback 注销后才释放；因此 quiesce 成功仅证明没有开始/探测/HTTP 请求再触碰 modem/UART。超时不强制取消非前景长请求、不销毁 UART/modem，保持 admission 关闭并返回失败/隔离，故不得误称为 ML307 deinit、可 restart Connectivity 或完整蜂窝 shutdown。Fangtang 隔离构建通过：app `0x32aa80`（16 MiB app 余 `0x75580`，13%）。仍缺 COM5 的并发长请求/stream upload + quiesce 故障注入 HIL，以及 Connectivity Service 对 Wi-Fi、portal、SNTP、Gateway poll 的统一 owner 迁移。

> 2026-08-09 生命周期复核/修复（SNTP retry registry identity）：复核 Connectivity owner 时发现 `maclaw_clock_sync` 曾以可变的全局 `&s_clock_sync_task` 作为 Registry context：自然退出会先清空该变量，随后 unregister 的 context 与登记值仍是同一变量地址；下一次 monitor 虽可继续注册，却不能用 task identity 区分陈旧 stop entry 与新的 worker。现已改为创建时捕获的不可变 `TaskHandle_t` 值：worker 只在自身仍是当前 task 时清全局 handle，并以自身 handle unregister；Registry stop 对 context 与当前 handle 不一致时 fail-closed，避免旧 entry 停止新任务。停止成功仍由 Registry 移除 entry，避免 stop path 与 worker exit 双方依赖可变地址。Bread Compact 隔离构建通过：app `0x32eab0`（16 MiB app 余 `0x71550`，12%）。此修复只加强 SNTP retry monitor 的 task identity/stop join；`esp_netif_sntp` client、Wi‑Fi event handlers、AP/portal/DHCP 的完整 shutdown 仍未收口，不得因该构建声称 Connectivity Service 已完成或可 restart。

> 2026-08-09 生命周期复核/修复（长期 worker Registry identity）：同一模式还存在于常驻 `maclaw_cancel` 与 `maclaw_volume_nvs`：它们原先用可变 task-handle 变量地址登记，worker 清空 handle 后再用该地址注销，无法提供代际隔离。现已统一为创建时的不可变 task handle identity，worker 仅在自身仍为当前 worker 时清全局 handle；Registry stop 以 identity 不匹配 fail-closed，成功 join 后由 Registry 移除 entry。这样启动 rollback 不会让历史 entry 停止后来重建的同名 worker，也不把已自然退出但尚在清理中的 worker 当作新实例。Bread Compact 再次隔离构建通过：app `0x32eb20`（16 MiB app 余 `0x714e0`，12%）。这是 Task Registry 正确性修复，不表示 cancellation/persistence 的所有 I/O 事务、NVS 或 Interaction Service 已可完整 restart；相关 HIL 和更广的 owner 收口仍是门禁。

> 2026-08-09 生命周期复核/修复（Registry generation guards 扩展）：继续按同一不变量复核各长期/一次性 worker。`setup_restart` 现在在 start-gate 异常退出和普通退出两条路径都只清自己的 handle，并始终以自身 handle 注销；其 stop handler、`meeting_resume_supervisor`、`meeting_capability_refresh`、`captive_dns`、`gateway_startup`、`wake_restart` 的 Registry stop handler 也都对登记 identity 与当前 task 做 fail-closed 比较。`wake_restart` 两条退出路径同样仅能清理/注销自身，避免老 retry worker 覆盖后继实例。Fangtang 隔离构建通过：app `0x32ac70`（16 MiB app 余 `0x75390`，13%）。这改善 Task Registry 的 task-generation 安全性，尤其覆盖 Gateway/Provisioning 相关 worker；**不**停止 portal HTTP server、DHCP、AP/STA、Wi‑Fi event handlers 或 `esp_netif_sntp` client，因此完整 Provisioning/Connectivity shutdown、restart 和 sleep PREPARE 仍未完成，不能据此扩大生命周期声明。

> 2026-08-09 生命周期复核/修复（ambient 与 Gateway poll identity）：`maclaw_ambient` 与 `maclaw_gateway_poll` 原来以 `NULL` 登记，因而 Registry 无法识别后续重建是否仍为同一个 worker。现改为不可变 task handle context：自然退出只在自身仍为 current 时清全局 handle，再用自身 identity 注销；stop handler 对陈旧 context 拒绝操作。EchoEar-2ST 隔离构建通过：app `0x31ade0`（16 MiB app 余 `0x85220`，14%）。这只完善两项已登记 task 的 stop/join 代际正确性；Gateway HTTP client、Wi‑Fi/IP event loop、SNTP client、portal HTTP/DHCP/AP 模式仍不是可完整 teardown/restart 的服务，相关 HIL 尚缺。

> 2026-08-09 生命周期实施增量（owner-wide timeout）：`task_registry_stop_owner()`/`stop_all()` 的 `timeout_ms` 现被定义并实现为**整个 owner 的总 deadline**，而非向每个逆序 entry 重复分配完整 timeout。Registry 为每次 stop 计算剩余时间；时间耗尽即记录 timeout 并保留未处理 entry，不再把 nominal 500 ms rollback 放大为“entry 数 × 500 ms”。已开始 stop 的 entry 仍保持原有失败隔离：失败不注销、后续 entry 在剩余预算内继续尝试。与此同时修正 ambient stop 路径仍用旧 `NULL` 注销的残留，并让 captive DNS 的 start-gate 异常退出也用自身 identity 清理/注销。Bread Compact 隔离构建通过：app `0x32eda0`（16 MiB app 余 `0x71260`，12%）。这只是 Task Registry 的 deadline/accounting 合约，不是任何共享依赖的强制中断权限；未入 Registry 的 Wi‑Fi、portal、SNTP client、会议和音频仍不能据此当作已 drain。

> 2026-08-09 HAL 实施增量（Fangtang profile-local uplink presenter）：Fangtang 待机页的 4G/Wi-Fi 标记及因切换而触发的 idle/quiet 场景重绘，已从 `board_port_bread_compact.c` 下沉到 `boards/fangtang_4g/board_port_fangtang_4g.c`。共享 renderer 继续唯一拥有 scene state、LCD mutex 与 `show_state_screen()`；bridge 只在 Fangtang profile 编译时把稳定的 `board_port_set_network_transport(bool)` facade 转交给 profile-local presenter，Bread 仍为 no-op，业务/Connectivity Service 不出现 Fangtang 分支。这是“显示该硬件的 selected-uplink”这一 product presentation seam 的真实下沉，不改变 uplink policy 或 Gateway 行为。Fangtang 与 Bread 隔离构建通过：app 分别为 `0x32a960`（余 `0x756a0`，13%）及 `0x32ea80`（余 `0x71580`，12%）。该 bridge 仍包含大量 Fangtang 条件编译；NV3023 已独立的物理 panel path、方糖 scene、单键输入、音频参数等仍需继续拆为共享 primitive 与 profile assembler，不能把本次移动称为独立 Fangtang adapter 完成。

> 2026-08-09 HAL 实施增量（Fangtang physical-input profile）：新增 `boards/fangtang_4g/fangtang_input_adapter.h`，将单一激活键的 GPIO、有效释放电平、25 ms debounce、2.5 s long press、500 ms double click 与 1.8 s 启动网络选择窗口集中为 profile-local 物理合同。共享 compact scanner 仍唯一拥有按下沿/short/double/long 的标准 action 发布、队列生命周期和 stop/join；它只读取 profile 提供的电平规范和阈值，不出现 Fangtang GPIO 或业务语义。启动 selector 同样读取这份 profile contract，避免 scanner 与 selector 因硬件 timing 常量漂移而产生边界不一致。Fangtang 外置启动 selector 时，legacy scanner 不再保留其旧的启动窗口 state/消费分支，GPIO0 在 selector 退出后才由 scanner 独占；这保持“选择窗口中的整次手势不触发录音/会议”的既有行为。Fangtang 与 Bread 均隔离构建通过：app 分别为 `0x32a860`（余 `0x757a0`，13%）和 `0x32ea80`（余 `0x71580`，12%）。构建不替代 COM5 的单击/双击/长按、启动双击切换、熄屏首手势、录音中停止和闹钟抢占 HIL；Fangtang scanner 本体及其它 display/audio 条件仍在 bridge，不能把本项称为 Input adapter 完整拆分。

> 2026-08-09 生命周期实施增量（Task Registry 第一个可运行切片）：新增内部 `task_registry`，用固定容量、按 owner 分域、逆注册顺序 stop/join 的登记表替代对 FreeRTOS handle 的跨域猜测。停止失败/超时的条目保留登记并累加诊断，禁止在仍可能运行时释放其资源。首批接入 USB `firmware_identity` 与内部栈 Flash `persistence_worker`：前者现在以 registry owner `DIAGNOSTICS` 管理；后者增加 admission close、stop sentinel、completion join 和 `STORAGE` owner，启动回滚通过 `task_registry_stop_owner(STORAGE)` 停止它，而非 `main.c` 读取 worker handle。四 profile 已重新构建通过：Bread `0x32b770`（余 `0x74890`，13%）、EchoEar `0x3178d0`（余 `0x88730`，15%）、Fangtang `0x327350`（余 `0x78cb0`，13%）、Waveshare `0x347c10`（余 `0x1b83f0`，34%）。这只是 Registry 的首批真实 owner 接入，**不**表示 P0 已关闭：Gateway/SNTP、会议、portal/DNS、interaction/audio/wake、timer/ISR/event handler 仍须按同一接口迁入，且仍缺少故障注入与 COM3/4/5/6 rollback HIL。

> 2026-08-09 生命周期实施增量（网络与环境时钟）：已有 stop/cancel/join 契约的 `maclaw_gateway_poll` 与 `maclaw_ambient` 已接入同一 Registry，分别登记为 `CONNECTIVITY` 与 `POWER` owner；自然退出、显式 stop 成功和 rollback 都注销同一 entry。启动失败时若 registration 未成功，会先调用其既有 stop/join，而不会留下未登记 worker。rollback 改为按 owner 停止，业务编排层不再直接选择这两个 task handle。四 profile 隔离构建再次通过：Bread `0x32b910`（余 `0x746f0`，13%）、EchoEar `0x317a70`（余 `0x88590`，15%）、Fangtang `0x327540`（余 `0x78ac0`，13%）、Waveshare `0x347db0`（余 `0x1b8250`，34%）。Gateway startup 握手、SNTP、蜂窝 transport、portal/DNS 和会议续传尚没有可取消 stop contract，故**不得**登记为已治理或宣称 Connectivity Service 完成。

> 2026-08-09 生命周期实施增量（交互取消与本地等级持久化）：`maclaw_cancel` 以 `INTERACTION` owner、`maclaw_volume_nvs` 以 `STORAGE` owner 接入 Task Registry。两者均在成功创建后登记、自然退出和成功 stop 后注销；startup rollback 不再读取/通知这两个 task handle，而是通过 owner 停止。音量/亮度持久化 worker 在 stop 后关闭新请求 admission，并将 reply 投递改为短周期、可观察 stop token 的有界等待；停止者在取得同一 request mutex 后清理已超时的回复，再投递 stop sentinel，避免 reply queue 满时永久阻塞、让 sentinel 无法消费。若 NVS transaction 或 join 超时，Registry 保留 entry，禁止后续路径误释放它仍可能访问的队列/同步对象。四 profile 隔离构建通过：Bread `0x32bb20`（余 `0x744e0`，13%）、EchoEar `0x317ce0`（余 `0x88320`，15%）、Fangtang `0x327750`（余 `0x788b0`，13%）、Waveshare `0x348030`（余 `0x1b7fd0`，34%）。这仍不是 Interaction/Storage 全域完成：foreground interaction、audio/wake、会议和其它 NVS/Flash owner 尚须按其真实资源契约迁入，且未完成故障注入 rollback HIL。

> 2026-08-09 生命周期实施增量（SNTP retry monitor）：只将 `maclaw_clock_sync` 这个 retry monitor 纳入 `CONNECTIVITY` owner；它以启动 gate 消除“创建后、登记前已自然同步退出”的竞态，将原有 12 秒 `esp_netif_sntp_sync_wait()` 拆为 250 ms slice，并以 task notification 打断 30 秒 retry backoff，因此 rollback 的有界 join 不再受完整 retry 周期阻塞。任务自然退出和显式 stop 成功都会注销同一 Registry entry；超时仍保留 entry 和资源。此改动**没有**停止或反初始化 SNTP client、没有调用 `esp_netif_sntp_deinit()`，也没有治理 SNTP callback/event handler；这些仍属于未来的 Connectivity Service，不能据此宣称完整网络关闭。四 profile 隔离构建通过：Bread `0x32be30`（余 `0x741d0`，13%）、EchoEar `0x317fe0`（余 `0x88020`，15%）、Fangtang `0x327a30`（余 `0x785d0`，13%）、Waveshare `0x348320`（余 `0x1b7ce0`，34%）。最新 Waveshare app 已按 `0x10000` 写入 COM6 并由 esptool SHA-256 校验通过；串口随后持续输出 display heartbeat、Gateway poll 与 wake mic telemetry，未见启动复位。

> 2026-08-09 生命周期实施增量（离线唤醒重启协调器）：短生命周期 `maclaw_wake_restart` 已以 `AUDIO` owner 接入 Task Registry。它现在在成功创建后经启动 gate 放行，持有 task handle、completion semaphore 与 admission gate；250/100/500/1000 ms 的等待都可由 direct task notification 打断，因此 startup rollback 可在有界时间内关闭其 admission、join 当前 retry，而不会让晚到的网络/宠物/前台操作回调再创建新 restart worker。连续 12 次启动识别器失败不再“先安排后退出”地交叠两个 worker，而是由同一个已登记 worker 完成下一轮 backoff；自然退出以 immutable task handle 注销自己的 entry，避免旧任务误注销后继任务。这里**只**治理协调器，不强制停止 board-owned wake recognizer/I2S/codec；audio/wake-word 全域生命周期、普通交互和会议等仍未迁入 Registry。四 profile 正式配置隔离构建通过：Bread `0x32c2d0`（余 `0x73d30`，12%）、EchoEar `0x318490`（余 `0x87b70`，15%）、Fangtang `0x327e40`（余 `0x781c0`，13%）、Waveshare `0x3487d0`（余 `0x1b7830`，34%）。

> 2026-08-09 生命周期实施增量（会议续传监督器）：`maclaw_meeting_resume` 已作为 `CONNECTIVITY` owner 接入 Task Registry。它现在在创建后先等待 start gate，登记失败会先关闭 admission、放行并 stop/join，不再存在“任务已运行但未登记”的发布窗口；所有等待（上传 worker 结束、前台 HTTP 让行、指数退避）都改为 task notification 可打断的等待。正常退出、显式 stop 成功和 rollback 均以 immutable task handle 注销同一 entry；completion semaphore 仅在 worker 退出后发出，timeout 时 Registry 保留 entry 和同步资源。该 owner 的严格边界是**监督循环**：停止它只禁止后续恢复重试；若它已调用 `start_meeting_task(true)`，真实会议上传 worker 仍继续按其既有 NVS/音频/HTTP 恢复契约执行，不能把本项描述为会议服务或上传 worker 已完整 quiesce。正常 gateway-ready 路径会在 poll/wake 初始化后自动启动该监督器，以恢复重启前保留的录音。四 profile 正式配置隔离构建现已通过：Bread `0x32ccf0`（余 `0x73310`，12%）、EchoEar `0x318f00`（余 `0x87100`，15%）、Fangtang `0x3287f0`（余 `0x77810`，13%）、Waveshare `0x349140`（余 `0x1b6ec0`，34%）；真实“上传中 rollback”故障注入 HIL 仍待完成。

> 2026-08-09 生命周期实施增量（会议能力刷新）：`maclaw_meeting_cap` 已作为 `CONNECTIVITY` owner 接入 Task Registry。该任务用于在 Hub 动态开放会议能力后发起一次 runtime handshake；现在创建后先等待 start gate，注册失败先关闭 admission、放行并 stop/join。Wi-Fi HTTP `perform` 区间会发布受 mutex 保护的 client 指针，rollback 先取消该请求、再以 task notification 打断 250 ms 的“等待前台互斥”重试；完成 semaphore 只在 worker 真正退出后发出，正常退出、显式 stop 成功和 rollback 均以 immutable task handle 注销同一 entry。蜂窝路径仍仅依赖既有有界 request timeout，若它超过 lifecycle timeout，Registry 会保留 entry，绝不把运行中的任务误报为已停止。该项只治理能力刷新 handshake 和其后尝试启动会议的短任务，**不**停止已经创建的 `maclaw_meeting` 录音/上传 worker。四 profile 正式配置隔离构建通过：Bread `0x32ccf0`（余 `0x73310`，12%）、EchoEar `0x318f00`（余 `0x87100`，15%）、Fangtang `0x3287f0`（余 `0x77810`，13%）、Waveshare `0x349140`（余 `0x1b6ec0`，34%）。仍需实际握手阻塞/cancel 与上传中 rollback 的故障注入 HIL，会议 worker、portal/DNS 和 Gateway startup 仍未因此成为完整可重启服务。

> 2026-08-09 生命周期实施增量（Gateway 启动协调器）：`maclaw_gateway_startup` 已作为 `CONNECTIVITY` owner 接入 Task Registry。它覆盖配对/冷启动 handshake 的无限重试协调，不覆盖 Wi‑Fi driver、SNTP、portal/DNS、Gateway poll 或 ML307 transport 的完整生命周期。创建后由 start gate 阻塞到 Registry entry 已写入；注册失败会关闭 admission、放行并 stop/join。重试 backoff 已改为 task notification 可打断等待；Wi‑Fi HTTP `perform` 期间的 client 指针由专用 mutex 保护，rollback 先 cancel 请求再 join，因此不会把 30 秒 TLS/HTTP timeout 误作 stop 时延。蜂窝仍沿用 adapter 的既有有界 request timeout；若超过 stop deadline，entry 保留并隔离，不能宣称已关闭。停止请求会禁止后续 retry、配对失败后启动 portal 与成功后启动 ready tasks；已由其它入口启动的子服务不在本 owner 内被强制回收。四 profile 正式配置隔离构建通过：Bread `0x32d1f0`（余 `0x72e10`，12%）、EchoEar `0x319460`（余 `0x86ba0`，15%）、Fangtang `0x328c90`（余 `0x77370`，13%）、Waveshare `0x349660`（余 `0x1b69a0`，34%）。仍需真实 TLS 阻塞、backoff 与蜂窝请求中的 rollback HIL；这不代表 Connectivity Service 或 Gateway restart 已完成。

> 2026-08-09 生命周期实施增量（配网保存后的延迟重启协调器）：`maclaw_setup_restart` 已作为 `CONNECTIVITY` owner 接入 Task Registry。它只管理 HTTP `/save` 已回复后用于等待 socket flush 的 1.2 秒延迟与随后的有意 `esp_restart()`：创建后先等待 start gate，登记失败会关闭 admission、放行并 stop/join；延迟改为可由 task notification 打断，正常退出和显式 stop 成功均以 immutable task handle 注销同一 entry。这样 startup rollback 不会留下未登记的延迟 reset。严格边界是该协调器**不**拥有 captive DNS socket/task、HTTP server、DHCP、AP/STA Wi‑Fi mode、event handler 或 portal UI；停止它只取消尚未发生的 reset，不能被描述为 portal 已关闭或 Provisioning Service 已可重启。Waveshare 正式配置隔离构建通过：`0x349b90`（余 `0x1b6470`，34%）；其余 profile 与真实保存→取消/rollback HIL 尚待补齐。

> 2026-08-09 生命周期实施增量（captive DNS）：`maclaw_captive_dns` 已作为 `CONNECTIVITY` owner 接入 Task Registry。它只拥有 UDP/53 socket 与 DNS task：创建后由 start gate 等待 entry 登记完成；停止先关闭该 worker admission、设置 stop token，再由 100 ms `recvfrom()` timeout safe point 退出并在本 task 内关闭 socket、completion join、注销 immutable handle。注册失败或 portal 初始化失败同样先放行、stop/join；下一次 portal entry 只有在旧 DNS 已 join 后才会重开该 worker admission，避免竞争 UDP/53 bind。该项不停止 HTTP server、DHCP、SoftAP/STA、Wi‑Fi driver/event handler、portal UI 或音频/电源 lease，因而不构成 Provisioning Service 完整 shutdown/restart。四 profile 隔离构建通过：Bread `0x32dc20`（余 `0x729e0`，12%）、EchoEar `0x319e70`（余 `0x86190`，15%）、Fangtang `0x329660`（余 `0x769a0`，13%）、Waveshare `0x34a070`（余 `0x1b5f90`，34%）。仍需 portal 启动中/UDP receive 中 rollback 的 HIL。

- 兼容 facade `board_port_*` 已新增“主交互来源”语义：EchoEar-2ST 将触屏、Bread Compact/Fangtang-4G 将激活键映射为同一共享业务意图。闹钟解除、启动/失败/配对提示和回复导航不再在 `main.c`、`app_ui.c` 以板型宏区分交互方式。
- 已新增不依赖 ESP-IDF/FreeRTOS 的 `main/device_api.h` 基础契约：稳定 `device_status_t`、单调 deadline 类型以及输入 action/source 语义。输入跨任务边界现使用 `device_input_event_t`（`struct_size`、ABI、单调 sequence、单调 timestamp、action/source）的按值版本化信封；板级 scanner 只能发布规范化 action/source，由 `input_service` 生成公共 envelope。其上新增硬件无关 `app_intent_service`：唯一 Binding table 将 primary/secondary/configure/volume/contact 映射为版本化 `app_intent_event_t`，并在 binding 边界计算“标准主交互来源”；Binding callback 只进行非阻塞入队，新的唯一 `maclaw_interact` App Interaction Task 从有界三 lane 队列消费并调用 `on_app_intent()`，因此不再由输入扫描或 Input Service 任务同步执行业务。`critical` lane（4 个槽）专门保留给取消、配置和主交互的按下沿（含闹钟即时解除/录音即时停止）；普通主交互在 `control`（16），音量在 `auxiliary`（8），普通流量不能占用关键 reservation。任一 lane 满均不无界等待：递增对应 dropped 计数；critical 满额外置 sticky `critical_overflow` 并记录错误。`input_queue` 按值诊断快照（started、三 lane pending/dropped、critical_overflow）已进入 USB `IDENTITY`/`BOOT_STATUS`/`SERVICE_STATUS`。停止时先关闭 intent admission，再有界停止 Input Service、等待 binding publisher 离场、投递 stop sentinel 并 join consumer 后释放队列；任务栈置于 PSRAM，避免挤占 Wi‑Fi/TLS/ESP-SR 的 internal heap。`main.c` 不再订阅 Device Input action 或查询 profile 来判断触屏/实体键。此实现目前只覆盖输入域，尚不是覆盖音频、网络、显示、power completion 的完整 Device Event Queue：仍缺跨域 event envelope、producer source budget/coalescing、ISR publish API、operation generation 与故障恢复监督，不能以该三 lane 队列宣称全局事件总线完成。
- 已新增内部、硬件无关 `operation_context` 的首个可运行 slice：按值 `device_operation_context_t` 具有 ABI、boot-lifetime 非零 operation ID/generation、kind、可选绝对 deadline、cancel token 与 `terminal_committed`。voice interaction 现在由该 service 原子 admission，取消先请求 context cancel，worker/HTTP cancel 只接受当前 generation，完成路径用一次性 terminal commit 阻止旧 worker 或重复终结释放新 interaction admission；保留 legacy `s_interaction_generation`/TaskHandle 仅作为迁移期 task ownership bridge，不能单独决定有效性。前台会议录音也已迁入同一 foreground slot：开始前先取得 interaction admission，再创建 `MEETING_RECORDING` context；task context 显式携带 generation，worker 经一次性 start gate 等待 handle 发布，关键录音/上传 UI transition 均须确认仍是当前 generation，所有成功、失败和本地启动失败共用 terminal commit 与 admission release。后台会议续传刻意不占 foreground slot，仍在 chunk 边界让出 HTTP 给新的语音指令；`MEETING_RESUME` 仅是将来多 domain slot 的预留枚举，不能误报为已接入。进一步修复跨核 task create 的发布窗口：新 worker 首先等待一次性 start gate；创建方仅在锁内发布 task handle 后才发 gate，因此快速 capture 失败、取消或回复不会在 handle 尚未可见时破坏 operation owner。`operation` 诊断快照（ABI、ID、generation、kind、cancel/terminal）已进入 USB `IDENTITY`/`BOOT_STATUS`/`SERVICE_STATUS`；最新 EchoEar 构建已刷入 COM3，app 写入 hash 校验通过，并观察到本地启动、Wi-Fi、离线唤醒和 Gateway TLS 启动正常。当前仍未让网络 poll、媒体、显示、闹钟、power/sleep 共用 operation ID、deadline、cancel token 和一次终态提交规则，后续必须按领域迁入且不引入双副作用。
- `DISPLAY_OFF` 的唤醒入口已进一步统一到 Power Service：App Intent consumer 在任何主交互进入业务状态机前，先经 `app_ui_wake_from_idle()` 请求共享 Power API；若面板确为关闭状态，该 contact 只恢复当前 adapter 的待机画面、取消 idle deadline 并被消费，后续新 contact 才可开始语音、会议或取消。Power Service 将“取消 timer → 恢复面板”置于同一 transition mutex，避免恰好排队的 idle callback 在一次真实唤醒后又关闭屏幕；EchoEar adapter 同样改为持有其 LCD mutex 完成 wake/render，和 Bread/Fangtang 的矩形 renderer 具有相同所有权边界。该增量仍严格是 panel/backlight `DISPLAY_OFF`，没有宣称 LIGHT/DEEP_SLEEP 或 RTC wake 已经完成。
- 已新增共享 `sleep_schedule_service` 的第一条可运行闭环：三 profile 使用同一份 NVS 持久化配置、一次性绝对时间窗或工作日周期时间窗（支持跨午夜，边界为 `[start,end)`）、`CST-8` 本地时间、revision、跨重启 idempotency replay 与手动唤醒 override。当前可靠且共同验证的目标严格限定为 `DISPLAY_OFF`：工具 `sleep_schedule_set/get/disable` 明确拒绝 light/deep sleep，计划不会中断录音、闹钟、回复或网络，只会经现有 Power Service 申请/恢复 panel/backlight。主交互只在确实从 `DISPLAY_OFF` 恢复面板时提交 override；NVS blob 直接读入静态 store，避免包含 replay journal 的完整 store 落在 `app_main` 栈上，且 mutation rollback 同样不再把完整 journal 复制到 Gateway poll task 栈。已修复 Fangtang-4G 的启动栈溢出。所有只读 `sleep_schedule_get`、`alarm_list` 均不要求 idempotency key；`set/disable` 和 alarm 写操作仍强制要求。计划状态已加入 USB `IDENTITY`/`BOOT_STATUS`/`SERVICE_STATUS`，便于后续 HIL 验证。该闭环不替代 DST/多时区解析、`PREPARE → COMMIT`、RTC timer wake 或真正的 LIGHT/DEEP_SLEEP；这些能力仍不得对外宣称。
- 已新增共享 `wake_deadline_service` 的可运行最小闭环：它是三硬件共用的、固定 8 槽、无堆分配的墙钟 deadline 仲裁者，唯一拥有产品级的 `esp_timer`。Sleep Schedule 和 Alarm Manager 不再各自拥有 timer；前者仅提交下一个窗口边界，后者只提交最早到期闹钟，任一变更都原子重算最早 deadline。timer callback 只唤醒 service task，deadline callback 再唤醒所属领域 worker，避免在 ESP timer task 执行业务/UI/NVS。SNTP 或经认证 Hub `serverTime` 校时后，Clock callback 同时通知 dispatcher 与 Schedule 重新计算周期边界；时间不可信时 deadline 保留但不 arm。闹钟由原 250 ms `time()` 轮询迁为 deadline 触发，响铃/贪睡的节奏仍使用单调时间，保证 wall-clock 回拨不会截断当前响铃策略。该 service **尚不是**深睡 RTC wake owner：没有 profile wake source、持久化 deadline、wake-cause 恢复、优先级/容差/revision 或 light/deep sleep `PREPARE → COMMIT`，因此只可声明运行中的 `DISPLAY_OFF` 调度统一。三 profile 已隔离构建通过：Bread `0x318b80`（余 `0x87480`，15%）、EchoEar `0x323a40`（余 `0x7c5c0`，13%）、Fangtang `0x31d5b0`（余 `0x82a50`，14%）；仍待按 COM4/COM3/COM5 刷入并进行短时间窗、闹钟优先、手动唤醒 override HIL。
- 新增共享 `resource_pressure_service` 的第一条安全闭环：在 `storage` 挂载之后，统一采集 internal heap（总量与最大连续块）、PSRAM（总量与最大连续块）和 SPIFFS（总量/空闲量）；以 internal/PSRAM 最大连续块和 SPIFFS 空闲量计算带滞回的 `NORMAL`/`PRESSURE`/`CRITICAL` 状态，并经版本化 `device_resource_pressure_snapshot_t` 输出到 `IDENTITY`、`BOOT_STATUS` 和 `SERVICE_STATUS`。该服务没有 board ID、GPIO、显示或音频副作用；当前三种硬件的远程/启动宠物资源下载及缓存属于可选装饰性工作。除 `CRITICAL`/存储不可观测的 fail-closed 外，新添按峰值容量的统一 admission：远程宠物 pack 必须预留全部 source frame 的 PSRAM 峰值、Display HAL 在替换动画时的临时缩放/复制峰值、首帧缓存的 SPIFFS 写入量，以及每类资源的 PRESSURE 水线；显示端仅通过硬件无关的预算 query 报告这一事实，圆形 EchoEar 与矩形屏继续由各自 adapter 决定缩放尺寸。任一资源处于 PRESSURE 或预留后会跌破水线时，不启动下载/缓存。闹钟、前景语音、会议收尾、NVS 与持久化关键路径不受此 gate 阻断。2026-08-07 已分别构建并刷入 COM3 EchoEar-2ST、COM4 Bread Compact、COM5 Fangtang-4G；app 分区 readback digest 均匹配，三机 `BOOT_STATUS.ready=true`、`resource_pressure.available=true`、`level=0`、`storage_available=true`，且未见 panic/stack overflow。本闭环目前仅是“观测 + 可选工作准入/降载”：未实现 task/resource registry、DMA/queue/stack/thermal/battery 水位、emergency reserve、全业务降载、故障注入或长期碎片 HIL，不能宣称完整 Resource Manager 已完成。
- Gateway 的设备工具分发已从 `main.c` 中 Alarm/Sleep/Update 的名称分支收敛到 `device_tool_registry`：领域 handler、描述符、写操作幂等要求与临时 readiness 由统一 registry 声明，Gateway 只负责 arguments、结果回传和错误映射。握手使用稳定的完整工具目录，执行时才检查领域 readiness，避免 Alarm 的延迟启动导致能力协商抖动；该 registry 不下沉到 HAL，也不包含硬件或业务状态机。三 profile 已重新隔离构建通过；待同批写入并实机核验。
- 已落地生命周期/资源治理的第一条可运行闭环（不是完整 Boot Coordinator/Resource Manager）：新增内部 `lifecycle_service` 及公共、按值版本化的只读 `device_runtime_snapshot_t`。启动按 `BOOTING → PROFILE_VALIDATED → IDENTITY_READY → STORAGE_READY → CORE_SERVICES_READY → LOCAL_READY` 发布阶段；首次失败锁存失败阶段和稳定 `device_status_t`，转入 `DEGRADED` 后禁止继续本地服务启动。`app_main()` 已将 profile、identity、NVS、PSA、关键 mutex/queue/task、update metadata、Input Service、Power Service 的启动失败从部分 `ESP_ERROR_CHECK` panic 改为受控停止：不会继续开启 radio/gateway，保留 USB identity 查询诊断；UI 已就绪时显示降级提示。配置快照的 fail-closed 边界现先完成校验，之后才创建 `maclaw_cancel` 与 `maclaw_volume_nvs` 两个永久 worker，以及 interaction/volume 的工作队列；因此损坏配置导致的降级启动不会遗留这两个后台任务。配置通过后若 Input、Power、deadline、schedule 或 force-setup 读取失败，降级入口会先有界停止 App Intent Service，再以取消 notification、volume queue sentinel 和 completion semaphore 分别 join 这两个本地 worker；Sleep Schedule 已具备同样的 stop/join 与 deadline slot unregister。Input Service 现会先关闭 publisher admission、等待已进入 publisher 离场，再 join board adapter 的按键/触控 scanner，之后才投递 consumer stop sentinel 并释放 event queue；Bread Compact/Fangtang-4G 与 EchoEar-2ST/Waveshare 共用这一语义，避免 scanner 向已释放 queue 发布。scanner 停止由 direct task notification 唤醒、completion semaphore join，未用 `volatile` 充当跨核同步。该动作只停止 scanner，不反初始化 LCD、音频、I2C 或宠物动画；board port 仍是 boot-lifetime、不可 restart，`s_board_scanner_initialized` 继续禁止不完整的二次 `board_port_init()`。Alarm Manager 现也在 task/slot 层实现停止：关闭 tool admission、取消 deadline、以通知唤醒 task，响铃或贪睡循环在安全点退出；已持久化的 active alarm 不会被当作 dismissal 完成，下一次 init 会按原有恢复规则重新入队。最新补上 tool caller drain：每个 tool 在读取 `s_lock` 前取得受 lifecycle lock 保护的 admission，deinit 先关闭 admission、再 join task、等待已进入的 tool 返回，最后才销毁 mutex；因此不会出现 tool 在 stop 后触碰已释放锁的竞态。可选 Fall Detection 同样具备 stop/join：关闭 tool admission、停止采样 task、取消待确认窗口并释放其 power lease、drain 已进入 tool 后再销毁 mutation mutex；无 Motion HAL 的正式 profile 不创建任务且 deinit 保持幂等。Power Service 现具备 DISPLAY_OFF timer 的 stop/deinit：先令新调度请求观察到 stopped，取消 timer 并与已排队 callback 争用同一 transition mutex，确认其已离开板级显示调用后才删除 ESP timer；静态 transition mutex 继续保留，晚到调用只会返回未初始化而不会使用被释放的同步对象。Power Lease 服务刻意不随此路径销毁，使 Interaction/Meeting/Alarm 等领域仍可安全归还已有 lease。所有 Deadline client 都退出后，共享 Wake Deadline dispatcher 才停止 ESP timer、join task、释放 slots/timer/mutex；任何 client 停止超时均保留下游资源并记录隔离诊断，绝不 `vTaskDelete` 仍可能访问资源的任务。`runtime` 同时进入 USB `IDENTITY`/`BOOT_STATUS`/`SERVICE_STATUS` 诊断 JSON，但不参与 Hub 授权或能力裁决。本轮 Input scanner 变更已对 Bread/EchoEar 隔离构建：Bread `0x31e2d0`（余 `0x81d30`，14%）、EchoEar `0x312c60`（余 `0x8d3a0`，15%）；Fangtang `0x3222f0`（余 `0x7dd10`，14%）与 Waveshare `0x315cb0`（余 `0x1ea350`，38%）在此 scanner 等价改动前已构建，涉及对应 adapter 的下一轮改动前仍应重建。此闭环尚未实现 task/resource registry、完整 board port 反初始化、完整 safe mode、故障域 restart、Resource Manager 的共享总线借用或故障注入 HIL，不能将其称为完整资源管理完成。
- `main.c` 已移除把 `board_port_*` 文本替换为 UI 调用的预处理宏。所有 pet、录音、消息、上传、回复、配网二维码、待机天气与闹钟显示均直接调用 `app_ui_*` 的共享 UI 状态机；Fangtang 的启动窗口网络传输选择和待机 uplink 提示也已经 `device_connectivity_*` 进入 Device API，`main.c` 不再直接调用 `board_port_*`。该 API 目前只是 Connectivity adapter 的最小迁移 seam，并未实现 ML307/Wi-Fi 的完整 Connectivity Service、策略、健康快照或生命周期治理。
- `app_ui.c` 已不再包含或调用 `board_port.h`/`board_port_*`。其完整的共享场景状态机（启动、待机宠物、录音、上传、文字/图片回复、配网二维码、就绪提示、天气和闹钟）经 `device_display_*` 进入 Device API；三种 profile 仅在 adapter 内决定矩形或圆形显示、控制器 GRAM 偏移、字体、像素格式、背光及触屏/实体键后的呈现。该实现仍是同步 compatibility facade，而非独立 Display Service/Display Task；它不改变既有 scene 先后、payload 深拷贝或 alarm 前景抢占行为。
- Connectivity Service 现同时拥有 Wi‑Fi 与蜂窝链路的 readiness 观察，业务路径只调用无参数的 `device_connectivity_is_active_uplink_ready()`，不再读取 Wi‑Fi event group 后把结果传入 API。Wi‑Fi 的 DHCP 获取/断开与同步启动成功路径发布 readiness，ML307 的启动/恢复路径发布蜂窝 readiness；每次 Wi‑Fi start 会先使旧 observation 失效。这仍不是完整的 transport 生命周期、健康退避、连接策略或网关请求 Service，但消除了共享业务对 `WIFI_CONNECTED_BIT` 的直接依赖。
- Wi‑Fi 的同步 connect wait 已由 Connectivity Service 以 generation/SSID 绑定会话收口：每个单凭据或多热点候选切换先创建递增 attempt epoch，Service 在内部持有唯一 event group；`GOT_IP` 与断开事件均以 ESP-IDF 提供的关联 SSID 归属到当前 attempt，旧候选的迟到事件不会完成新候选的 wait、清空新 session 的 readiness 或触发 Gateway 恢复。`main.c` 只保留 Wi‑Fi driver/configuration 调用和 UI 反馈，不再拥有 `WIFI_CONNECTED_BIT`、event group 或用同步结果重复发布 readiness。该保护仍未实现 Wi‑Fi driver/netif/event-handler/SNTP 的完整 stop/deinit/restart transaction；也尚未在 COM3/COM4/COM5/COM6 进行多热点快速切换、迟到 DHCP/断开故障注入 HIL，构建通过不等同于该生命周期的实机验收。
- 配网保存的个人 Wi‑Fi 主凭据与多热点恢复目录已收敛为 `configuration_service` 内同一份 versioned snapshot、同一次 `persistence_service` commit：服务在保留 transport selection、force-setup 和既有目录后，将新个人 SSID/密码以既有 FIFO 策略写入目录，再一次性提交；`main.c` 只在 commit 成功后刷新 RAM 镜像，不再执行“主凭据先落库、目录另一次 best-effort upsert”的可掉电分裂写入。snapshot 校验还拒绝目录空 SSID/重复 SSID，防止损坏状态进入 Wi‑Fi 候选选择。旧 V1/V2/scalar-key 的读取/扩展迁移保持不变。此项是配置持久化一致性收敛，不等于计划中的 provisioning `stage → validate → commit → activate → confirm`、凭据加密、跨 namespace journal、真实网络验证或断电注入 HIL；需在 COM3/COM4/COM5/COM6 验证第一次配网、更新已有 SSID、目录满载 FIFO 与保存窗口掉电后的恢复。
- Connectivity Service 的 uplink 选择切换现会使“新选中”一侧的旧 readiness 立即失效：4G→Wi‑Fi 不会沿用旧 DHCP 结果，Wi‑Fi→4G 不会沿用旧 modem session；只有该适配器随后发布的新 start/recovery 成功观察才会重新使 `device_connectivity_is_active_uplink_ready()` 为真。该修复避免启动切换窗口或恢复路径把过期链路状态误交给共享业务。
- Connectivity Service 另提供按值 `device_connectivity_snapshot_t`（已选 uplink、Wi‑Fi/蜂窝各自 readiness 与所选链路 readiness）。USB 诊断与 Gateway handshake 都从同一个 `firmware_identity_info_t` 快照序列化该观察，不暴露 SSID、token、APN、IP 或 modem 原始状态，也不把“链路可用”误报为 Hub 已认证/请求必然成功。该字段仅供诊断，当前不参与 Hub capability 或远端授权裁决。
- `device_power_get_telemetry()` 已为公共业务/诊断提供不含 ADC/GPIO 细节的可选 `device_power_telemetry_t` 快照：Fangtang 可透出其既有电量/充电采样，Bread Compact/EchoEar 明确返回 `available=false`，而不是伪造 0% 或按 board ID 判断。2026-08-08 新增共享 `battery_policy_service`：它只消费该标准快照，以 25%/12% 的 `CONSERVE`/`PROTECT` 进入线和 30%/16% 恢复滞回，统一发布 optional/high-power work admission；低于保护线时停止新的可选远程宠物下载/缓存，不影响告警、语音、录音收尾和持久化关键路径。`IDENTITY`、`BOOT_STATUS`、`SERVICE_STATUS` 与 Gateway identity 都公开 telemetry availability 与 policy snapshot；Gateway 还可通过统一只读 `battery_policy_status` 获取相同的 `telemetryAvailable`、`charging`、`levelPercent`、policy level 和准入结果，业务不用读取任何 profile 私有电量实现。它仍不是完整低电量保护：未实现 ADC 校准、连续样本/异常值过滤、低电 checkpoint、charger wake、自动深睡或电气 HIL，因此不将其宣传为 brownout safety 或完整 Battery Policy。
- Fangtang-4G 已实现 0–100% direct-I2S 软件音量并通过 GUI 下发路径使用；这只是 Audio HAL facade 的行为补差，尚未完成 Audio Service/HAL 生命周期拆分。
- EchoEar-2ST 的 ES8311 播放仍是单独的未验收缺陷：已确认其 16-bit Device API PCM 与 32-bit I2S slot 的 DMA payload 必须保持 16-bit packed stereo，不能把应用 PCM 扩写成 32-bit DMA words；该错误尝试会改变 IDF 的 `data_bit_width` DMA 契约并进一步损坏音频，已撤回。按 Espressif ES8311 reference driver 修正 `REG01`：外部 MCLK、正常边沿应为 `0x3F`，旧值 `0x7F` 会设置 MCLK inversion bit；再将 codec `REG09/REG0A` 的接口 word-width 与物理 32-bit I2S slot 对齐为 `0x10`，避免 codec 将 slot padding 与相邻声道数据错作连续人声；`REG31` mute 改为 read-modify-write，只变更 `0x60` mute bits，防止取消静音时覆盖其它 DAC 控制位。还发现独立的下载内存问题：wake-word 常驻时 internal heap largest block 约 `3584` bytes，HTTPS AES 报 `-0x0084`。因此服务器 URL 与 inline 音频均在 Device Audio/wake HAL 边界申请短时 memory lease：完整 stop MultiNet → 解码/下载/播放 → ACK → 重启；宠物动画帧也在独占大媒体 lane 后取得同一类临时 lease，前景人声到达会取消低优先级宠物事务，且只允许最后一个 lease 在 worker 退出、持久 TLS client 释放后恢复 wake。COM3 当前 app 为 `0x31f3a0`（余 `0x80c60`，14%），已仅刷 app 分区且写后 hash 校验通过。实机 HIL 已覆盖：8 帧宠物资源下载无 AES 失败、worker 结束后 MultiNet 重新 ready；宠物下载在途时定向 Hub 真实 PCM WAV（`69710` bytes）成功抢占、下载、播放进入、ACK 成功并恢复 MultiNet，日志没有该前景事务的 AES 或 wake-restart 内存失败。该证据不等同于音质验收，仍必须由人耳确认白噪/人声清晰度；若仍异常，再核验 PA/mute 时序、ES8311 其余寄存器及模拟扬声器链路，不能以提示音、Hub delivered 回执或日志替代听音验收。
- Bread Compact 待机天气由 Display adapter 负责版式适配：使用原生 24px 字形；城市展示名最多四个 Unicode 字形，直辖市展示名（例如“北京市”）归一为“北京”，天气摘要过长时优先裁短摘要以保留温度和 `°C`。该规则不修改 Hub 天气数据；普通城市名（例如“东莞市”）保留原始“市”字，不采用通用“去市”。该版式已随本轮统一 profile 构建并写入 COM4；启动、联网、Gateway handshake、唤醒词和 `SERVICE_STATUS` 已完成串口观察，天气文字的最终人眼版式仍随真实天气数据持续验收。
- 已新增内部 `power_service`，并经 `device_api.h` 收口应用侧的 DISPLAY_OFF 定时、取消与本地用户唤屏：面板/背光关闭时 MCU、网络、闹钟和离线唤醒词继续运行；前景 UI、录音、回复和闹钟会在板级提交前再次拒绝过期熄屏请求。该实现不是 LIGHT_SLEEP/DEEP_SLEEP，暂由既有 board-port mutex 串行物理显示操作，尚未达到计划中 Display Task、power lease、wake matrix 或睡眠事务的最终架构。
- `device_power_get_snapshot()` 现将 Power Service 的 idle-deadline 状态与 adapter 实际观察到的 panel/backlight 状态合并为按值快照，避免前景 Renderer 自行点亮屏幕后仍错误上报“已熄屏”。USB `IDENTITY`/`BOOT_STATUS`/`SERVICE_STATUS` 以及 Gateway handshake 的 `firmware.deviceProfile` 均携带同一份 `power` 观察；其中只包含 `ACTIVE`/`DISPLAY_OFF`，不宣称 LIGHT_SLEEP/DEEP_SLEEP。该诊断字段不改变 Hub 的 capability/授权，也不能替代三板功耗和唤醒 HIL。
- Bread Compact 的 GPIO42 背光现在由其 profile adapter 使用 5 kHz、10-bit LEDC PWM 驱动；正常显示、唤醒与启动统一使用 `512/1023`（约 50%）占空比，DISPLAY_OFF 时归零。该常量只作用于 Bread，不改变 Fangtang 的直接背光驱动；已完成构建并写入 COM4，串口启动与联网正常，最终亮度以实机人眼观察为准。
- 三种正式 profile 均已完成本轮构建：Bread Compact `0x31c460`（余 `0x83ba0`，14%）、EchoEar-2ST `0x2f8cb0`（余 `0xa7350`，18%）、Fangtang-4G `0x318490`（余 `0x87b70`，15%）。构建只证明当前 profile 选择、链接及 Flash 容量通过；并不构成三硬件实机行为等价、功耗、睡眠或唤醒验收证据。
- Fangtang 的 CMake 已改为选择 `boards/fangtang_4g/board_port_fangtang_4g.c` 这一独立 profile 入口。该入口在编译期校验 `CONFIG_MACLAW_BOARD_FANGTANG_4G`，当前以单一 ownership bridge 包含 legacy compact adapter，避免切换期间重复初始化 LCD、I2S、GPIO/input 或 power sampling；它不是“已完成拆分”的声明。NV3023/Y offset、GPIO0 启动选择、方糖 Renderer、电池和 ML307 presentation 仍须按唯一副作用 owner 的小步迁移计划从 legacy 文件移出。
- Fangtang 的物理显示合同已先从 legacy compact adapter 迁至 `boards/fangtang_4g/fangtang_display_adapter.h`：NV3023 依赖、240×240 viewport、GRAM Y=80、SPI/panel/backlight pins 与生产控制器初始化序列均由 Fangtang profile 唯一声明；本轮再将 NV3023 的 `0x2A/0x2B/0x2C` 行级寻址、GRAM Y 偏移及 RGB565 SPI 提交迁入同一 profile adapter，shared renderer 只提交普通 viewport rectangle，不再持有该控制器的命令细节。Fangtang 最新构建为 `0x318ad0`（余 `0x87530`，15%），已写入 COM5 且 hash 校验通过；启动、Wi‑Fi、Gateway handshake、wake-word、`BOOT_STATUS` 与 `SERVICE_STATUS` 均有串口观察。该 HIL 证明本次行传输收口没有破坏启动和联网路径；尚未以相机/人眼确认每个 scene 的实际画面，方糖 scene renderer、物理 panel 初始化调用及显示 sleep/wake 仍在 bridge 内，不能把该 header seam 误报为完整 Display adapter 完成。
- `main.c` 已不再直接包含 `driver/gpio.h` 或配置 Fangtang 的 modem guard/power GPIO。新增 `device_connectivity_prepare_cellular_transport()`：共享启动编排只请求“为蜂窝 transport 做硬件准备”，Fangtang adapter 在 profile capability 门禁后完成 UART 配置有效性检查、guard/power 时序；它不启动 ML307、不选择 uplink，也不包含 gateway 业务。Bread/EchoEar 返回 `UNAVAILABLE`。这是 ML307 Connectivity port 拆分的第一条物理 ownership seam，ML307 请求/恢复生命周期仍在后续迁移范围。
- Fangtang 的 GPIO0 启动双击网络选择、网络选择持久化与 ML307 Gateway URL 兼容策略已移至其 profile adapter：adapter 在 legacy normal-input scanner 创建前独占执行 1.8 秒的同步选择窗口，并在窗口结束的 quiescent point 交回同一 GPIO；legacy scanner 保留正常短/双/长按手势。选择写入 `maclaw/net_transport`，仅在该值不存在时只读迁移旧 `network/type`；4G 已选中时仅将标准 Hub URL 降级为 ML307 可用的 HTTP 端点，自定义 URL 保持原样。共同启动路径只经 `device_connectivity_*` 恢复/应用该选择，不读取 NVS、GPIO 或 ML307 细节。COM5 已验证 Wi-Fi 已选中的恢复与启动；startup 双击边界和真实 4G 仍待 HIL。
- Fangtang 的 battery ADC/charge GPIO 采样任务与 `board_port_get_power_status()` 读取端均已从 legacy compact adapter 移至其 profile adapter；`device_power_get_telemetry()` 的既有公共快照语义与采样周期、三点平均、充电状态和校准表保持不变。采样任务是该快照唯一 writer，profile getter 以同一 critical section 读取，迁移期没有第二份电量状态或第二个采样任务。COM5 已刷入采样 owner 下沉版本并完成启动、Wi‑Fi 握手、wake-word 与 Gateway poll 的运行日志观察，`SERVICE_STATUS` 报告 `power.available=true`、Fangtang profile 与 16 MiB/8 MiB PSRAM identity 一致；本次 getter 收口完成 Fangtang 构建（`0x318630`，余 `0x879d0`，15%），尚未以本次二进制完成串口/HIL 复验。电量百分比/充电极性仍需在 USB 供电、充电和放电三种真实电气状态下另行 HIL 校准；当前不把它宣称为 Battery Policy 或低电量保护。
- 前景请求取消新增 `device_connectivity_cancel_cellular_foreground_request()`：共享取消编排不再直接调用 ML307 实现；Fangtang profile adapter 是 ML307 cancel 的唯一硬件 transport owner，Bread/EchoEar 统一返回 no-op。该接口只取消蜂窝侧正在进行的前景请求，不改变共享 Wi‑Fi HTTP client 的 mutex/cancel 所有权。三 profile 已重新构建：Bread `0x31c550`（余 `0x83ab0`，14%）、EchoEar `0x31db00`（余 `0x82500`，14%）、Fangtang `0x318650`（余 `0x879b0`，15%）；Fangtang 已刷入 COM5 并通过串口 `BOOT_STATUS` 复验 profile、16 MiB Flash、8 MiB PSRAM、`power.available=true` 和 Wi‑Fi readiness。该 HIL 仅证明启动与诊断链路；仍需在真实 4G 前景命令中验证取消时序及恢复。
- 蜂窝 start/readiness 继续收口：`main.c` 的 ML307 recovery/start 不再传递 UART GPIO、波特率或 APN，也不直接调用 ML307 start/readiness；`device_connectivity_start_cellular_transport()` / `device_connectivity_is_cellular_transport_ready()` 以稳定超时和状态契约转入 Fangtang profile adapter，adapter 内部统一执行 guard/power preparation 与 ML307 native lifecycle。Gateway HTTP、语音配对及会议 stream 也均已通过 Device API descriptor 进入 Fangtang adapter，`main.c` 不再包含 ML307 header 或 Fangtang Kconfig 分支。共享层仍保有 retry/backoff、selected-uplink readiness 发布、URL/授权业务语义、response 解释、meeting cursor 与 UI 进度；adapter 只拥有 transport 请求。三 profile 最新构建为 Bread `0x313ec0`（余 `0x8c140`，15%）、EchoEar `0x31e880`（余 `0x81780`，14%）、Fangtang app bin `3246544` bytes（约 15% 余量），且均已仅写入 app 分区并完成启动 HIL。Fangtang 当前仅验证 Wi-Fi 已选中；尚未在本二进制上切换真实 4G，故不将 ML307 startup/recovery/request HIL 宣称为完成。
- 蜂窝 HTTP 请求也已由 Device API 收口：通用 Gateway HTTP、语音配对、会议 chunk stream 均通过版本稳定、调用方拥有 buffer 的 request/stream descriptor 调用 `device_connectivity_cellular_http_request()` / `device_connectivity_cellular_http_stream_request()`；Fangtang adapter 独占 ML307 的 request、stream、response-length 窄化及 reader bridge，Bread/EchoEar 返回 `UNAVAILABLE`。Wi‑Fi 仍保留既有 shared HTTP client 和 mutex owner，业务仍负责请求 URL、授权语义、重试、response 解释、meeting cursor 与 UI 进度。三 profile 构建成功：Bread `0x31c540`（余 `0x83ac0`，14%）、EchoEar `0x31daf0`（余 `0x82510`，14%）、Fangtang `0x318920`（余 `0x876e0`，15%）；Fangtang 已刷入 COM5 并以 `BOOT_STATUS` 验证 profile/Flash/PSRAM/power/Wi‑Fi 启动。尚未以真实 4G SIM 运行 command、voice pairing 或 chunk 上传，不以此构建和 Wi‑Fi 启动诊断代替蜂窝 request 的 HIL。
- `app_main()` 的 NVS 初始化已删除对 `ESP_ERR_NVS_NO_FREE_PAGES`/`ESP_ERR_NVS_NEW_VERSION_FOUND` 的自动 `nvs_flash_erase()`。此类启动现在保留分区内容、记录诊断并在任何 NVS writer/Wi-Fi/音频启动前停止；显式的受认证恢复和版本化迁移流程仍待实现。
- 2026-08-08 新增共享 `persistence_service` transaction boundary：它复用应用唯一的 NVS transaction mutex，向领域服务提供按 namespace/key 的同步 blob read/write（写入仅在 `nvs_commit` 成功后返回成功），不暴露 NVS handle；仅为旧 schema 的一次性导入提供受控 typed read，不向领域泄漏 handle。`Fall Detection Service`、`Sleep Schedule Service`、`Alarm Manager`、metadata-only `Update Service`、待机 `Weather Cache Service` 与 `Meeting Recovery Service` 已迁移到这一边界。Alarm 保留 V1 store 兼容；Update、Weather、Meeting 都将旧分散 key 一次性导入带 magic/version 的单 blob，再仅写入新 schema。Weather cache 仍是 advisory UI 缓存，缺失时保持空状态；Meeting 恢复只有在持久 pending 与有效 SPIFFS WAV 同时存在时才启动续传。所有这些领域均在 commit 成功后才发布新的内存状态；损坏/不兼容的关键 metadata 不会静默覆盖用户状态。Wi-Fi、配对 token、设备 identity、volume、force-setup 与 Fangtang transport selection 等 writer 尚待逐步迁移，当前不得宣称所有 NVS writer 都已收口或已有完整跨 namespace journal/断电注入验证。
- 已完成 Bread、EchoEar-2ST、Fangtang-4G 的隔离构建验证；近期 app 使用量分别为 Bread `0x311460`（余 `0x8eba0`，15%）、EchoEar `0x305e50`（余 `0x9a1b0`，17%）、Fangtang `0x3177a0`（余 `0x88860`，15%）。构建成功不构成实机、功耗、睡眠或三硬件功能等价的验收证据。
- Hub 端已实现 `RapidAI/MaClaw` GitHub Release 的可信 metadata catalog：必须同时验证三份官方 `.clawfw`、manifest 原始字节 Ed25519 签名、asset/文件 hash、profile/chip/16 MiB/layout/app identity 和发布后 asset 不变性；Hub 只保存已验证 metadata，handshake 不返回 firmware URL 或字节。该能力仍待用真实 GitHub Release 和 ClawMate Maker 做端到端演练。
- 设备端已接入同一个 metadata-only `update_service`：仅接受 `requiresComputer=true` 的更高 `releaseSequence` 与 SHA-256 manifest digest，持久化去重/稍后/限期忽略状态；digest 变化会失效旧忽略，critical 只能短周期延后，时间不可信时拒绝把提醒静默为已过期。工具只注册 `update_check`、`update_status`、`update_remind_later`、`update_dismiss_version`，明确没有下载、安装、刷写或重启入口。提醒显示仍由现有各 profile Renderer 适配，尚未完成三硬件提醒操作的实机验收。
- ClawMate Maker 正常 `appUpdate/full` 写入在风险窗口前已追加 fresh nonce-bound protocol:2 application identity 门禁：运行中设备报告的 firmware target、chip、16 MiB Flash 与最低 PSRAM 必须同时匹配用户已确认的 catalog profile 和已签名 bundle；identity 不可读、协议不符或字段不符一律拒绝写入。只有用户明确进入、且仅接受完整签名包的 `RECOVERY_REQUIRED` ROM 恢复流程可跳过该运行中 App 证据，因为受损 App 本身可能无法回应；恢复仍受 ROM chip/Flash/security、签名、profile、layout 与刷后 BOOT_STATUS 门禁约束。该桌面端门禁已由 `ClawMateMaker` Go 测试覆盖；真实三板刷写/断电 HIL 尚未完成。
- `firmware_identity` 的 USB Serial/JTAG 查询任务已从隐式永久循环收敛为可停止生命周期：重复 start 不再创建第二个 reader，stop 使用 cooperative stop flag 与有界 join，超时 fail-closed 且不会强制删除可能仍在解析 host 请求的任务。当前 `app_main()` 仍在全生命周期启动它以支撑刷机/恢复诊断；Power/Manufacturing Coordinator 接入其 quiesce/stop 顺序和三板实机验证仍待完成。
- 仍未完成：纯 C 的 Device/Platform API、独立 `boards/<board>` profile、Fangtang ML307 Connectivity port、真实 LIGHT/DEEP_SLEEP/Sleep Schedule、保数据 NVS 恢复、受校验 ClawMate Maker 刷机的完整闭环、版本检查/提醒实机端到端、事件队列与 Resource Manager。不得把现有 display-off、Kconfig 配置或可构建状态宣称为这些能力已发布。

## 2. 背景与目标

当前工程已经具备统一 `board_port_*` 函数名和初步的 `app_ui_model_t`，并已在 Kconfig 中正式列出三款硬件，但硬件访问、输入手势、音频会话、产品交互策略和显示状态仍集中在两个大型板级实现中：

- `main/board_port.c`：EchoEar-2ST 的 ST77916、CST816、ES7210/ES8311、输入及圆屏渲染实现。
- `main/board_port_bread_compact.c`：同时承载 Bread Compact 与 Fangtang-4G。Bread 使用 240×320 ST7789、激活键/音量键和直连 I2S；Fangtang 使用带 80 行 GRAM 偏移的 240×240 NV3023 viewport、单激活键、直连 I2S、充电/电池采样及专用方糖界面。文件内部已有大量 `CONFIG_MACLAW_BOARD_FANGTANG_4G` 分支，应拆为共享驱动构件与两个独立 profile/adapter，而不是继续把 Fangtang 当 Bread 变体。
- `main/ml307_transport.cpp` 与 `main/main.c`：Fangtang-4G 额外提供 ML307 蜂窝传输、Wi-Fi/4G 启动选择和恢复路径；网络传输差异目前仍渗入 Gateway HTTP、会议上传、时间同步和启动编排。
- `main/main.c`：仍存在根据具体板型选择闹钟解除输入的条件编译。
- `main/app_ui.c`：仍存在 Bread Compact 回复翻页特判。
- `main/board_port.c`：EchoEar 当前 idle timeout 仅关闭 panel/backlight，仍不是系统 LIGHT/DEEP_SLEEP；GPIO0 与 GPIO42 仍由轮询输入任务读取。
- `main/Kconfig.projbuild` 与各 profile sdkconfig：已有 DFS/tickless idle、Fangtang battery ADC/charge GPIO 等配置；Fangtang 电池采样已有板级实现，但尚未形成统一 Battery Policy，三硬件也尚无完整的 `esp_pm_configure`、LIGHT/DEEP_SLEEP 与 wake-source 业务闭环。不能把 Kconfig 文案、ADC 读数或链接 component 直接视为已发布的电源能力。
- `main/app_main()`：NVS 遇到 `ESP_ERR_NVS_NO_FREE_PAGES` 或 `ESP_ERR_NVS_NEW_VERSION_FOUND` 时会直接执行 `nvs_flash_erase()`，可能同时清除 Wi-Fi、配对 token、闹钟、会议恢复和未来 schedule，必须改为保数据的恢复流程。
- `main/gateway_poll_task()` 与 Hub `device_gateway.go`：设备依赖长期 `/outgoing` 轮询；Hub 以最近认证请求后的 90 秒窗口判断在线，未 ACK 消息只保存在有上限的 Hub 内存队列中。当前没有 sleep/offline presence 协议，Hub 重启或离线积压超过上限时不能保证所有待发消息保留。
- `board_port*.c`：离线 MultiNet 唤醒词任务持续读取 I2S 并运行推理；现有 Kconfig 也明确说明 always-on listening 与 light sleep 冲突。因此“语音唤醒可用”和“允许 LIGHT/DEEP_SLEEP”必须作为互斥产品策略治理，不能由板级任务自行决定。
- `main/start_setup_portal()`：当前 SoftAP 为开放网络，二维码明确使用 `nopass`，配置表单通过本地 HTTP 提交 Wi-Fi/EAP 密码、Hub URL 和配对码；配对恢复还会运行 AP+STA。必须把配网安全、生命周期和重放边界作为 Connectivity/Provisioning Service 契约，而不是延续为板级便利逻辑。
- `main/is_valid_gateway_url()`：当前允许 `http://` 且仅做前缀/空格校验；企业 Wi-Fi 表单还允许 `ca_mode=none`。发布策略必须默认强制 HTTPS、规范化 origin、禁止凭据跨 origin，并把关闭 CA 校验限制为受控诊断模式。
- `main/firmware_identity.c`：当前通过 USB Serial/JTAG 常驻任务解析 `CLAWMATE_QUERY`，回报 board/layout/compat ID、固件版本、ELF SHA256 与 readiness；任务使用永久循环且缺少统一 stop/join，身份协议版本与 Gateway/HAL/NVS 版本也尚未形成兼容矩阵。该通道必须收归 Diagnostic/Manufacturing Platform Service，不能成为绕过 Task Registry、安全脱敏或板型兼容校验的例外。
- `main/main.c`：仍有大量跨任务 `static`/`volatile` 状态和任务句柄，例如 interaction/meeting/cancel/welcome/HTTP 状态；`volatile` 只能影响编译器访问，不能提供多核原子性、内存序或复合状态一致性。迁移必须把每组状态归入单写者 Service/事件队列或明确 atomic/critical-section 契约，不能把现有全局变量原样搬进新目录。
- `main/CMakeLists.txt`：EchoEar 选择 `board_port.c`，Bread/Fangtang 共同选择 `board_port_bread_compact.c`，仅 Fangtang追加 ML307 component/source 和方糖资源；但显示、codec、ESP-SR、ADC 等许多组件仍无条件进入同一个 `REQUIRES` 集合。构建拆分必须以三个 profile 的 source/component/resource manifest 为唯一输入，同时产生锁定依赖和可审计 artifact provenance。
- `.github/workflows/main.yml`：现有 `build-esp32-firmware` 使用 `espressif/idf:v6.0.2`，已分别构建 EchoEar、Bread 和 Fangtang；发布的 `${FIRMWARE_NAME}.bin` 是 `idf.py merge-bin -f raw` 生成、必须从 flash `0x0` 写入的整机镜像，只能作为显式恢复出厂/工装资产，不能用于默认“保留数据升级”，也不是设备端 OTA app image。Workflow 已通过 `softprops/action-gh-release@v2` 把 `release-assets/*` 发布到 `RapidAI/MaClaw` GitHub Release；本计划直接复用 ClawMate Maker 的签名 `.clawfw`、manifest、board profile、`layoutFingerprint`、`reservedRegions` 与模式契约，为三 profile 生成兼容元数据和刷机包，不另造 bundle 格式，也不产生设备端 OTA 专用产物。
- `partitions.csv`：硬件固定为 16 MiB，当前仅有约 3.625 MiB `factory` app、3 MiB model 和约 9.3 MiB storage；无 `otadata/ota_0/ota_1`。本地 app image 已观察到约 3.08 MiB。若同时加入 A/B 和完整 staging，storage 只剩约 1.7 MiB，无法可靠承载会议录音与资源，因此产品决策是保留现有单 app/model/storage 资源格局，放弃设备端 OTA。
- `main/main.c`/`main/CMakeLists.txt`：当前 `storage` 是 SPIFFS，会议录音、宠物素材和资源缓存共用；现有代码已记录 SPIFFS GC 在碎片化分区上可能持续很久。版本检查只能存储很小的 release metadata/提醒状态，不下载固件、不借用或清理会议存储。
- `MaClawSrv/thirdparty_gateway.go`：当前 bearer token 解析到 tenant/user principal，但 token 本身不是设备身份；Hub 版本检查 endpoint 必须再绑定已配对的 `clientId + deviceId + profile/hw/layout + credential_generation`，以免跨设备泄漏错误 profile 的发布信息。

### 1.2 2026-08-07：独立 profile 描述的实施证据

- 已新增 `main/boards/bread_compact/board_profile.c`、`main/boards/echoear_2st/board_profile.c` 与 `main/boards/fangtang_4g/board_profile.c`。CMake 按选中的 profile 只链接其中一个实现；它们不再通过同一个 `board_port_bread_compact.c` 的条件分支来定义设备身份。
- `main/device_api.h` 新增 ISO-C 的 `device_profile_t`、版本/长度字段和只读 capability flags；`device_profile_get()` 按值返回描述，避免业务层长期持有板级可变指针。描述的是物理适配事实而非业务功能裁剪开关，正式三 profile 仍必须实现完整 Bread 公共业务基线。
- `app_main()` 在任何板级驱动初始化前校验 profile ABI、结构大小与 `CONFIG_MACLAW_BOARD_ID` 一致性。该检查只能阻止构建源与 profile ID 错配；尚不等同于 PCB revision、电气安全 manifest、运行时健康或 effective capability 校验。
- 三 profile 已以 ESP-IDF 6.0.2 重新完成隔离构建：Bread `0x319dc0`（余 `0x86240`，14%）、EchoEar `0x304590`（余 `0x9ba70`，17%）、Fangtang `0x315e80`（余 `0x8a180`，15%）。未进行实机刷写；构建不构成睡眠、功耗或功能等价验收。

### 1.3 2026-08-07：Audio Device API 渐进收口

- `device_audio_set_output_volume()` 与 `device_audio_adjust_output_volume()` 已成为 `main.c` 的唯一音量硬件入口，统一 0–100 的语义和 `device_status_t` 错误映射。Codec（EchoEar）与 direct-I2S 软件增益（Bread/Fangtang）仍留在各自 board adapter；GUI 下发、实体音量键和启动恢复均不再直接调用 `board_port_*` 音量 API。
- 此项仅完成 Audio Device API 的一小段迁移，不意味着已完成 Audio Service、播放/采集会话所有权、wake-word 仲裁、持久化 Service 或 error/degradation reason 的完整分层。三 profile 在本次接入后重新构建：Bread `0x319e70`（余 `0x86190`，14%）、EchoEar `0x304640`（余 `0x9b9c0`，17%）、Fangtang `0x315f20`（余 `0x8a0e0`，15%）。

### 1.4 2026-08-07：采集、播放与离线唤醒的 Device API 接入

- `main.c` 的 WAV 播放、一次指令采集、会议 PCM stream 和离线唤醒词的 start/stop/pause 已统一改经 `device_audio_*` / `device_wake_word_*` 调用。MP3 解码器的 PCM begin/write/end 事务，以及闹钟抢占播放、指令录音的 cooperative stop/reset，也已改由 `device_audio_*` 进入同一个硬件边界；`mp3_player.c` 不再包含或调用 `board_port.h`。`board_port_*` 保留为此阶段唯一硬件 adapter owner；原有命令、会议、配网的业务状态机和时序不因板型变化而分叉。
- 迁移 seam 把旧领域代码需要的 `esp_err_t` 只在 `main.c` 的私有转换点处理；Device API 本身不包含会议、固定录音时长、闹钟或 Gateway 概念。当前仍不是完整 Audio Service：没有独立资源仲裁器、lease、停止/join 生命周期、事件队列或运行时 health/capability snapshot，不能据此宣称已经完成音频资源治理。
- 本次增量的三 profile 构建结果：Bread `0x319ff0`（余 `0x86010`，14%）、EchoEar `0x304730`（余 `0x9b8d0`，17%）、Fangtang `0x316020`（余 `0x89fe0`，15%）。未刷写任何串口设备。

### 1.5 2026-08-07：输入事件跨任务边界

- 新增 `main/input_service.c`。板级 port 仍只负责 GPIO/触控控制器扫描、去抖与手势识别，并把标准 `device_input_action_t/device_input_source_t` 发布到服务；业务回调不再由 board 的扫描任务直接执行。服务采用独立的有界 control/auxiliary 队列和单一消费任务，`CONTACT_DOWN`、主/次/配置等控制事件优先于音量翻页事件，满队列会计数与记录，不会阻塞输入扫描。
- 当前 `app_main()` 已不再直接调用 `device_input_start()`；它只启动 `app_intent_service_start(on_app_intent, ...)`。该服务是 Device Input 的唯一业务订阅者，统一 Binding table 将硬件无关 action 映射为业务 intent，并把主交互来源分类一并交给业务；其两个有界队列由唯一 `maclaw_interact` App Interaction Task 消费，control/contact/activate/configure 优先于音量翻页。闹钟解除所需的 contact edge、EchoEar CST816 的原生双击与重复 contact 过滤、Bread/Fangtang 的实体键手势仍保持在各自 adapter 内；`main.c` 不注册板级 callback，也不读取 Device Input action。
- 已建立 Device API 输入 handoff、版本化 `device_input_event_t`、共享 Binding table、`on_app_intent()` 和本输入域的 App Interaction Task；但尚未完成跨音频/网络/显示事件的完整统一 `device_event_envelope_t`、operation/generation、ISR 发布 API、关键事件 reservation/sticky overflow、telemetry coalescing 或 restartable scanner。当前两个 intent 队列也不是整个系统的统一 Device Event Queue；不得把该增量宣称为全部事件队列工作完成。
- 本次重建：Bread `0x31a510`（余 `0x85af0`，14%）、EchoEar `0x304b40`（余 `0x9b4c0`，17%）、Fangtang `0x316430`（余 `0x89bd0`，15%）。构建使用 ESP-IDF 6.0.2；未刷写 COM3/COM4/COM5，未做实机交互验收。

### 1.6 2026-08-07：输入派发服务的有界停止

- `device_input_stop(timeout_ms)` 现已进入 Device API，并实现了 Input Service 的停止 admission gate、在途发布者归零等待、停止哨兵和有界 consumer join。停止首先使已经注册的板级 publisher 成为 no-op，确认没有 callback 正在使用队列后才释放 queue/queue-set，避免 GPIO/触屏扫描任务与队列释放并发而造成 use-after-free。
- 当前三种 board adapter 的扫描任务仍是 boot-lifetime，尚未具备 `board_port_deinit()`/join。因此服务停止后显式拒绝再次 `device_input_start()`，而不是重复创建 GPIO/触屏扫描任务；这提供了可安全协调关闭的应用事件层，但不将它误报为完整可重启 Input HAL。`app_intent_service` 的 Binding table 与 `on_app_intent()` 已完成；完整 `device_event_envelope_t`、ISR publisher 和 restartable scanner 仍待实现。
- 三 profile 已构建：Bread `0x3108b0`（余 `0x8f750`，15%）、EchoEar `0x2f8470`（余 `0xa7b90`，18%）、Fangtang `0x317c00`（余 `0x88400`，15%）。构建仅验证链接与容量，未刷写 COM3/COM4/COM5，尚无 input stop 的三板 HIL 证据。

### 2.1 当前三硬件支持矩阵

下表只描述当前源码已经存在的硬件支持，不代表重构完成后的最终能力。所有“当前缺口”必须进入迁移台账，并以 Bread Compact 功能母版为终态验收。

| 硬件 | 当前显示 | 当前输入 | 当前音频 | 当前连接 | 当前电源/传感 | 主要待整改点 |
|---|---|---|---|---|---|---|
| Bread Compact | 240×320 ST7789 SPI 矩形屏；独立启动图；回复分页 | GPIO0 激活键，音量上/下键 | direct-I2S 麦克风与扬声器，16 kHz，软件音量 | Wi-Fi STA/AP/EAP | 未发现有效 battery telemetry；当前没有完整 LIGHT/DEEP_SLEEP | 作为唯一功能母版；其板级业务状态迁出，Display/Input/Audio/Power 分层 |
| EchoEar-2ST | 360×360 ST77916 QSPI 圆屏；圆形安全区和动画 | CST816 触屏、GPIO0/BOOT 候选键 | ES7210 capture + ES8311 playback/codec 音量，16 kHz | Wi-Fi STA/AP/EAP | 当前 idle 主要关 panel/backlight；GPIO42 touch IRQ 仅是 light/display wake 候选 | 全部业务逐项对齐 Bread；保留圆屏；输入/codec/wake 仅做适配，不保留业务分叉 |
| Fangtang-4G | 240×240 NV3023 SPI viewport，GRAM Y offset=80；方糖视觉；5 行回复自动翻页 | GPIO0 单激活键；启动窗口双击切换网络 | direct-I2S 麦克风与扬声器，16 kHz；当前音量设置返回 `ESP_ERR_NOT_SUPPORTED` | Wi-Fi + ML307 双传输；当前有专用 HTTP、恢复、会议上传和时间路径 | battery ADC、charge GPIO 已有采样；ML307 power/guard；尚无统一 Battery/Power/Sleep Service | 从 Bread 共同 board port 拆出独立 profile；补齐音量和替代入口；ML307 收口 Connectivity；完整对齐 Bread 功能 |

当前 Kconfig、稳定 board ID 与 CMake 已能选择三款硬件，因此 Fangtang-4G 不是“未来演示用第三板”，而是 MaClaw AgentOS 首批正式交付 profile。第四个 Fake/Reference profile 只用于证明抽象的可扩展性，不计入三硬件功能一致性的替代证据。

这意味着当前接口虽然同名，但业务仍然知道部分硬件差异；Fangtang-4G 已经以大量条件分支接入，证明继续复制或复用整个 `board_port` 会同步复制业务状态、网络特判和修复风险。

本计划的目标是建立稳定的硬件抽象层（HAL），使业务代码只有一份，并且只能依赖 HAL 和共享服务：

1. Bread Compact、EchoEar-2ST 与 Fangtang-4G 使用同一套业务状态机和交互规则。
2. 业务代码不接触 GPIO、触摸控制器、LCD 控制器、codec、I2S 接线或具体板型宏。
3. 输入、音频、屏幕、连接、电源等实现差异以及非公共物理增强全部封装在 HAL/板级适配中；Bread 公共业务能力不属于可选项。
4. EchoEar-2ST 保留 360×360 圆屏安全区、现有布局、动画及 ST77916 QSPI 提交策略。
5. Fangtang-4G 保留 240×240 小屏、单键、方糖视觉、Wi-Fi/ML307 双传输和电池/充电状态；这些差异由独立 profile、Renderer、Input、Connectivity 与 Power adapter 表达。
6. 三种硬件发布相同的业务能力集合，其验收清单以 Bread Compact 已有及本计划纳入的目标能力为准：语音指令、离线唤醒词、文字/图片/音频回复、取消、会议录音与续传、配网配对、待机环境信息、闹钟、音量、指定时间休眠、硬件输入唤醒，以及新版本检查、提醒和受校验刷机工具升级。
7. 后续新增硬件时，只新增板型描述和 HAL/平台适配，不复制或修改业务流程。

这里区分三类范围，防止把“基线对齐”和“本计划增强”混为一谈：

- `BASELINE_EXISTING`：Phase 0 从 Bread 当前已验证行为冻结的功能，迁移全过程不得回退。
- `BASELINE_PROMOTED`：本计划决定加入 AgentOS 公共基线、但当前 Bread 也需新增或补强的功能，例如正式 Sleep Schedule/硬件唤醒闭环、纳入公共契约后的 Alarm 增强，以及基于 GitHub Release/Hub 的新版本检查、提醒和受校验刷机工具升级；必须由三硬件同一版本共同交付。
- `PHYSICAL_EXTENSION`：电池/充电 telemetry、蜂窝图标等不改变公共业务结果的物理增强，可以按 profile 呈现，但不得成为公共流程前置条件。

第 6.1 节每个 `feature_id` 必须标记上述类别、baseline revision 和 owner；“以 Bread 为基线”不能被解释为 Bread 当前尚不存在的功能已经实现，也不能用来绕过三硬件同时交付 `BASELINE_PROMOTED` 的要求。

本文把面向共享业务服务的 `Device API` 视为设备能力抽象的公开表面，把面向驱动实现的 `*_hal_ops_t` 视为板级 HAL 的内部 SPI（Service Provider Interface）。因此“业务只调用硬件抽象层”具体落实为：业务状态机不调用 ESP-IDF、文件系统、网络或板级函数；会议、指令录音等共享业务 Service 只调用通用 Device/Platform API，再由它们调用 HAL SPI 或 ESP-IDF 平台实现。`Device API` 不能出现“会议”“六秒指令”“闹钟”等具体业务用例，否则只是把业务耦合换了一个文件名。

非目标：本次不重写会议上传算法、宠物视觉设计或 LCD/codec 底层驱动，不实现设备端固件下载、暂存、分区刷写、A/B 切换或自动回滚，也不让设备接受任意固件 URL。允许为 HAL 边界、安全修复、可靠休眠、版本检查和刷机工具补充必要的版本协商、签名 release manifest、能力投影、设备身份与生命周期接入；如需新的 Hub durable queue 或普通业务消息类型，仍须独立协议评审。

## 3. 强制架构原则

### 3.1 唯一业务实现

语音指令、会议录音、回复、取消、配网、闹钟、唤醒词监督和界面状态转换必须各自只有一个共享实现。Bread Compact、EchoEar-2ST 和 Fangtang-4G 不得维护不同版本的业务处理函数。

### 3.1.1 三硬件功能等价是发布门禁

“统一业务”不是只共用函数，也不是允许某个正式 profile 永久返回 `NOT_SUPPORTED`。首批三款正式硬件必须对外发布同一业务 capability 集合、同一状态机、同一错误语义、同一持久化/恢复保证和同一 Gateway tool 集合：

- 物理控件不同，通过 Input Binding 提供相同业务 intent；不能因为没有独立音量键、触屏或多按键而缺少对应业务功能。
- 屏幕形状/大小不同，通过 Renderer 的分页、滚动、裁切和安全区适配完整呈现相同信息；不能静默省略回复、进度、闹钟、错误或配置状态。
- 音频链路不同，通过 codec/direct-I2S adapter 实现相同 capture/playback/volume 契约；某板当前缺少音量实现属于待补差距，不是最终 capability=false 的合理状态。
- Wi-Fi 与 ML307 的传输实现可以不同，但 Command、Meeting、Alarm、Gateway、配对和时间语义保持一致；切换 transport 不产生新的业务分叉。
- 若实机证明某项业务受不可改变的硬件物理限制，必须在发布前增加可用的替代交互/反馈适配或修订硬件；不能以降级 profile 进入“三硬件功能完全一致”的正式发布集合。

可选的硬件增强（例如 Fangtang 电池/充电状态、蜂窝网络图标、EchoEar 触屏手势）允许额外呈现，但不能改变共享业务结果，也不能成为另一硬件执行核心流程的前置条件。

### 3.1.2 Bread Compact 是唯一验收基线

- 功能清单以 Bread Compact 为母版，先冻结 `feature_id`、用户入口、状态转换、结果、取消/超时、持久化、重启恢复、前景恢复和错误反馈。
- EchoEar-2ST、Fangtang-4G 的每个 `feature_id` 必须关联同一共享 Service 和一条硬件适配记录，逐项证明行为等价。
- 三硬件测试采用同一份业务用例和期望 trace；只允许在输入源、scene geometry、分页方式、音频/网络 driver trace 和功耗预算上使用 profile-specific expectation。
- EchoEar 或 Fangtang 当前已有、但 Bread Compact 尚未具备的产品能力，必须先评审是否纳入 Bread Compact/AgentOS 公共基线。纳入后由三硬件一起实现；未纳入时只能作为不影响公共流程的硬件 telemetry/展示增强，不能演化为板型专属业务。
- 任何“暂不支持”“后续补齐”只允许存在于迁移中的内部 build，不允许进入 MaClaw AgentOS 三硬件正式完成定义。

“以 Bread Compact 为基线”指业务契约以 Bread profile 的已验证行为为种子，不是让某一台 Bread 样机或某个不可追溯固件永久充当口头标准。Phase 0 必须冻结带版本的 `baseline_manifest`，至少包含 `feature_id`、行为/协议/schema 版本、Bread firmware/ELF digest、profile/hardware revision、golden trace、批准日期和 owner。之后所有公共功能变更必须遵守：

1. 先以版本化 RFC/变更记录更新公共功能契约和兼容策略，不允许只修改 EchoEar/Fangtang 或只依赖当前 Bread 偶然行为。
2. 同一变更集先通过共享业务测试，再分别适配三个正式 profile；发布时三硬件使用同一个 baseline revision。
3. 修复 Bread 既有缺陷时，把旧行为、目标行为和迁移影响显式登记；缺陷不能因属于旧基线而被永久复制到其他硬件。
4. EchoEar/Fangtang 的新增产品能力若获准进入公共基线，必须先更新 baseline manifest，再成为三硬件共同发布项；否则只能保留为不改变业务结果的物理增强。

### 3.2 依赖方向单向

依赖关系固定为：

> 2026-08-11 Task Registry 生命周期 deadline 收口：按 Phase 1 的“子步骤只能消费父 deadline 剩余预算”复核发现，`task_registry_stop_owner()/stop_all()` 虽已把 `timeout_ms` 作为 owner-wide worker stop budget 传递，但在取得 registry bookkeeping mutex、记录 stop failure 和成功 stop 后注销 entry 时仍使用无界 `portMAX_DELAY`（或公共无界 unregister）。一旦并发 register/unregister 持锁，启动回滚便可能在已完成/失败子任务之后无限阻塞，违背 rollback 及 future sleep/restart 的有界隔离约束。现将 registry stop transaction 建立为单一 tick deadline：snapshot、每个 stop callback、失败计数与成功 entry 删除均只获得剩余 tick；预算到期前不能取得 mutex 即返回 `ESP_ERR_TIMEOUT`，已停止但未能在期限内登记删除的 entry 保留，下一次 closed-generation cleanup 再处理，绝不在 deadline 后释放或修改可能仍被借用的资源。自然 worker exit 的 public register/unregister 语义保持不变，因为它不属于 caller-owned lifecycle transaction。Waveshare 缓存 Xtensa command 对 `task_registry.c` 的 `-fsyntax-only`、HAL boundary 和本文件 diff check 通过；这不等于端到端并发/故障注入、完整 link 或四机 HIL，后续必须构造 mutex contention、多个 owner entry、success/error/timeout 混排，并测量 caller wall-clock 不突破 parent deadline。

> 2026-08-11 生命周期 deadline 传播第二轮收口：继 Task Registry 后复核 scheduler/interaction stop 链，发现 `app_intent_service_stop()` 在 Input 已消耗部分时间后仍以原始 timeout 投递自身 stop token；Alarm、Sleep Schedule 与 Wake Deadline 三个相互依赖的 scheduler 也会对 deinit lock、admission lock、worker completion、tool borrower drain、最终 slot cleanup 分别重置 timeout，导致一次公开 stop 最坏累计超出父 rollback budget。现统一以 FreeRTOS monotonic tick 的单一 transaction deadline 计算 remaining budget：App Intent 的 token 投递只获得 Input stop 之后余量；Alarm/Sleep/Wake 的所有 lifecycle 等待和 drain 都只消耗余量，预算耗尽即保持 admission/stop generation closed、返回 timeout，不删除仍可能被 worker/callback 使用的 completion/timer/slot。该切片未改变闹钟、睡眠计划、首手势唤屏或业务工具语义，也不把 runtime restart/light/deep sleep 宣称为已完成。Waveshare 缓存 Xtensa command 已对四个修改单元 `-fsyntax-only` 通过，HAL boundary 与 diff check 通过；仍须在目标机注入锁竞争、in-flight tool、timer callback、Input scanner join 和多层 rollback，量测 wall-clock 严格服从父 deadline，完整 link 与 COM3–COM6 HIL 尚未据此完成。

> 2026-08-11 生命周期 deadline 传播第三轮收口（board worker / Fall Detection）：继续沿 startup rollback 的 caller budget 审计，发现 `fall_detection_service_deinit()` 在取得 deinit lock 后会对 classifier join、tool admission drain 和 mutation lock 重新使用完整 timeout；圆屏 `board_port_stop_background_tasks()` 与紧凑屏 `board_port_stop_background_tasks()` 亦未扣除取得 task-creation gate 的耗时，紧凑 profile 的 first worker 还会得到完整 budget。现将这些路径统一为同一 monotonic tick deadline：Fall Detection 的 worker completion、tool drain 和 mutation admission 都只接收余量；EchoEar/Waveshare 圆屏 pet worker、Bread/Fangtang 紧凑屏 thinking/pet/profile-peripheral worker 在取得 board admission lock 后才计算并逐级传递余量。超时保持 scanner/animation/classifier 相关 completion 和 admission 的既有 fail-closed 语义，不删除在途资源，也不改变跌倒识别、宠物动画或 profile-specific peripheral ownership。Bread 与 Fangtang board-port 缓存 Xtensa `-fsyntax-only` 通过（Fangtang 补 NV3023 include）；HAL boundary 与 diff check 通过。Waveshare 缓存 compile command 因本机 Component Manager 目录缺失 `esp_lcd_touch.h` 无法完成该整 TU 校验，属于本地缓存依赖问题，不能标记为 Waveshare 编译或 HIL 通过；完整 link 与 COM3–COM6 的 contention/fall/animation rollback HIL 仍待恢复环境后验证。

```text
业务层 app / 共享领域服务
    ↓
Device API + Platform API（唯一公开设备/平台边界）
    ↓
通用设备服务（输入、显示、音频、电源、存储、连接、身份）
    ↓
HAL SPI / ESP-IDF 平台端口（内部实现接口）
    ↓
板级实现 boards/<board>
    ↓
ESP-IDF 驱动、控制器和具体 GPIO
```

HAL 和板级实现不得反向调用业务状态机。板级回调只能发布标准化事件或报告完成状态。

### 3.3 业务层禁止板型知识

完成迁移后，`main.c`、共享 UI、会议录音、闹钟和网络业务代码必须满足：

- 不出现 `CONFIG_MACLAW_BOARD_*` 条件编译。
- 不 include GPIO、I2C、I2S、SPI、LCD、触摸和 codec 驱动头文件。
- 不比较 `TOUCH`、`GPIO0`、`CST816`、`ST7789`、`ST77916`、`ES7210` 或 `ES8311`。
- 不根据屏幕宽高决定业务状态。
- 不为某一块板补业务旁路。

### 3.4 能力驱动，不按板型分支

硬件描述与运行健康通过 `board_capabilities_t`/effective capability 表达。业务层只判断本次 operation 依赖是否健康，不能判断设备叫什么。这里的“能力驱动”不得与三硬件功能等价冲突：Bread 功能母版中的正式能力在三个静态 profile 中均为必选；`DEVICE_STATUS_NOT_SUPPORTED` 只允许用于第四 Fake/Reference profile、未来尚未进入正式支持集合的硬件，或明确不属于公共基线的物理增强。三款正式硬件运行中发生临时故障时返回稳定的 unavailable/degraded reason 并走恢复策略，不能把实现缺失伪装成动态 capability 收缩。

### 3.5 显示差异是一等适配能力

圆屏和矩形屏不强行共用坐标或页面布局。业务层发布统一的界面场景模型，各板 Display HAL 独立渲染。可以共享字体、颜色、UTF-8、字形缓存及基础绘图算法，但不得牺牲 EchoEar 圆屏现有效果来追求文件级复用。

### 3.6 业务只依赖通用 Device/Platform API

业务层不得直接解引用 `board_hal_t`，不得调用 `input_hal_ops_t`、`audio_hal_ops_t`、`display_hal_ops_t`，也不得直接调用 SPIFFS、NVS、Wi-Fi 或 HTTP API。对共享业务 Service 公开的是稳定的 `device_api.h` 和少量 Platform API；底层 HAL ops 与 Platform port 只允许通用设备/平台 Service 和板级启动器使用。

```text
业务 app → 共享领域 Service → Device/Platform API → 通用设备 Service → HAL/平台实现
```

Device API 使用通用设备语义，例如“打开音频采集流”“播放 PCM/受支持音源”“提交 UI 场景”“请求休眠”，不泄漏 I2S frame、LCD DMA、GPIO、触摸 contact 等低层概念。固定六秒录音、会议录音、上传、闹钟调度和回复生命周期属于共享领域 Service，不进入 Device API、HAL 或 Renderer。

### 3.7 生命周期必须完整且可回滚

所有长期占用资源的 HAL 都必须定义 `init/start/stop/deinit`，并满足：

- `init` 只配置和分配资源，不启动长期后台任务。
- `start` 在依赖全部就绪后启动任务、ISR 或 DMA。
- `stop` 先禁止新事件，再等待在途回调、任务和 DMA 在超时内退出。
- `deinit` 逆序释放资源，并能处理部分初始化状态。
- `stop/deinit` 重复调用安全；失败后允许诊断和受控重试。
- 任一步骤失败必须回滚此前成功的步骤，不保留孤儿任务、总线句柄、mutex 或 framebuffer。

### 3.8 共享资源只有一个所有者

同一 GPIO、I2C/SPI 总线、I2S channel、DMA buffer、PM lock 或电源轨只能由板级 Resource Manager 创建、登记和销毁。各子 HAL 获取借用句柄，不得各自初始化共享总线。特别是 EchoEar 的 CST816、ES7210 和 ES8311 共用 I2C，总线生命周期不得归属 Audio HAL 或 Input HAL 任一方。

### 3.9 并发通过事件和所有权模型收敛

HAL 不得从 ISR、输入扫描任务或 DMA callback 同步进入复杂业务。所有输入和设备事件通过有界 Device Event Queue 交给唯一 App Interaction Task；所有显示提交通过唯一 Display Task 串行执行；音频由 Audio Service 仲裁 capture、playback 和 wake-word 所有权。

### 3.10 临时数据不得跨越生命周期边界

UI snapshot 和队列消息必须拥有其数据。Renderer 不得保存调用方的临时字符串、图片、二维码 handle 或 PCM 指针。图片像素、二维码 module matrix、动态字形及异步 DMA 数据必须深拷贝、引用计数或明确转移所有权，并在 fence/完成回调后释放。

### 3.11 所有异步结果必须绑定操作身份

录音、上传、轮询、取消、音频播放、图片处理和 UI 呈现均可能晚于发起操作完成。所有异步请求、完成事件和错误事件必须携带统一的 `operation_id/generation`，不能用全局 bool、任务句柄或裸指针推断归属。已取消、已超时或已被新操作替代的 generation 不得更新业务状态、播放音频或覆盖屏幕。

### 3.12 启动成功不等于设备可用

启动由 Boot Coordinator 编排，并区分必选、可降级、可重试和致命错配。输入入口只有在业务状态机、事件队列、显示安全画面和必要硬件通过 readiness barrier 后才开放。可选组件失败必须更新运行时健康状态和对外能力，不能继续虚假声明支持。

### 3.13 板型错配时优先保证硬件安全

编译时 profile 选择必须结合运行时硬件 manifest、board revision、ESP32 target、flash/PSRAM 和可安全探测的器件签名进行校验。在确认板型前，不得驱动可能造成冲突的输出 GPIO、电源轨、功放或高速总线。无法自动识别的硬件由烧录工具和量产 manifest 强校验。

### 3.14 实时性和资源占用必须有预算

任务优先级、core affinity、栈内存类型、内部 heap、largest block、PSRAM、队列深度、音频 deadline、显示延迟和固件大小都必须设置可量化门槛。仅记录指标不算验收；超过预算必须阻止合并或经过显式评审调整 profile 预算。

### 3.15 配置、实现、探测与对外能力必须分层

Kconfig/CMake 只决定“哪些实现可能被编译”；profile/manifest 描述“硬件理论上允许什么”；运行时初始化、自检和健康状态证明“本次启动实际可用什么”；协议层只能声明最终 effective capabilities。禁止用编译宏直接回答业务能力，禁止因为 component 被链接就认为功能已经实现，也禁止把未经实机验证的 wake/power 能力发布给用户。

### 3.16 所有后台执行单元必须可治理

每个 task、timer、ISR、event handler、HTTP callback 和 DMA completion 都必须登记 owner、生命周期阶段、停止信号、join/drain 方式、最大退出时间和允许访问的资源。禁止无法停止的 `while (true)` 匿名任务和无法注销的回调。Light sleep suspend、HAL stop、配置重启和故障回滚必须复用同一治理清单，而不是依靠任务自然结束。

### 3.17 本地能力、会话协商与远端授权必须分离

本地 `effective_device_capabilities` 只由硬件 profile、编译功能、初始化/自检和运行时健康求交生成；Hub 的 `capabilitiesAccepted` 只能产生当前连接的 `negotiated_session_capabilities`，不能反向修改硬件事实或使本地不存在的能力变为可用。协议版本、tool schema、授权策略和 hardware capability 分别版本化。重连、新认证主体或 Hub 切换生成新的 negotiation epoch，旧 epoch 的 tool/message 不得沿用新会话权限。

### 3.18 安全标识必须依赖已就绪的强熵源

boot session ID、operation ID 随机部分、配网 AP 密码、session/CSRF nonce、pairing secret 和 credential key 只能由统一 Entropy/Security Service 生成。启动早期若 ESP32 硬件随机源尚未达到目标 IDF 所规定的强随机条件，不得生成长期 secret 或对外宣称安全会话已就绪；等待 RF/真随机条件、使用经批准的 DRBG reseed 流程，或进入可诊断的 fail-closed 降级。时间戳、MAC、计数器和普通 PRNG 不能替代熵。

### 3.19 服务依赖只在组合根装配

Boot Coordinator/`app_main` 是唯一 composition root：根据静态依赖图创建 Service，并显式注入稳定 API/handle、Clock、Allocator、Event Publisher 和 policy snapshot。Service 不得自行调用 `board_hal_get()`、查找全局单例、隐式初始化依赖或在构造期间启动任务。依赖图必须无环，必选/可选依赖、init/start/readiness、quiesce/stop/deinit 顺序由生成或静态校验的 manifest 固化；测试可用 Fake port 替换任意叶节点而不链接 ESP-IDF。

### 3.20 运行时健康变化必须防抖且可解释

一次 I/O timeout、队列尖峰或瞬时内存不足不能立即造成 capability 在 Hub/UI 间反复开关。每项 health signal 定义采样窗口、失败阈值、恢复成功次数、最短保持时间、cooldown、manual-clear 条件和严重度；Capability Service 由单写者把原始 health event 归并为稳定状态。安全类故障可立即 fail closed，普通故障按滞回降级；恢复必须经过重新自检/readiness barrier，不能仅因下一次调用成功就重新发布能力。

### 3.21 配置来源与生效事务必须唯一

编译默认、签名 manifest、量产校准、持久化用户配置、Hub 远程策略和临时运行 override 分层管理，并为每个 key 声明允许来源、优先级、认证要求、schema、有效期和重启语义。最终 effective config 只由 Configuration Service 生成 immutable revision snapshot；远端策略不能覆盖板级电气限制、安全下限或用户隐私硬开关。配置变化使用 validate → prepare → atomic publish → observer apply/rollback，禁止各 Service 直接读写同一 NVS key 或在 callback 中观察到半更新组合。

### 3.22 渐进迁移必须保持单一副作用所有者

迁移采用 strangler seam，但同一时刻每种副作用（GPIO/总线、音频采集、显示提交、NVS、网络 ACK、文件删除）只能由 legacy 或新 Service 一方拥有。允许新路径做只读 shadow 计算和 trace 对比，禁止双写、双播、双 ACK、双删文件或同时注册输入回调。每个 seam 有编译期选择、受保护的本地诊断 kill switch、切换前置条件、状态 handoff、回退窗口和 facade 删除条件；release 不接受 Hub 任意远程开启隐藏旧路径。回退只能发生在明确 quiescent point，并校验 schema/资源 generation/operation ownership。

### 3.23 跨核并发必须使用明确的 C/FreeRTOS 内存模型

`volatile` 不等于同步。跨 task/core/ISR 的 flag、generation、pointer 和复合状态必须使用消息传递、单写者 ownership、FreeRTOS 同步原语、port critical section 或工具链支持且经过审查的 C11 atomic；接口声明读写者、原子宽度、内存序和 ISR 上下文。禁止无锁读取一组分别更新的字段、依赖“32 位写天然安全”推断发布顺序，或在 generation 回绕时产生 ABA。锁使用支持优先级继承的 mutex；binary semaphore 只用于事件/所有权转移，不冒充递归互斥。

### 3.24 构建产物必须可复现且可追溯

每个 profile 的 firmware artifact 由锁定 ESP-IDF/toolchain/component 版本、源码 revision、sdkconfig、partition、嵌入资源和生成器输入确定。CI 生成 SBOM、依赖/许可证清单、component hash、build manifest、ELF/app/partition digest 和签名 provenance；同一输入的 clean build 应产生相同功能内容（允许签名/时间封装按规范分层处理）。高危 CVE、未知来源组件、lockfile 漂移或未审查生成资源阻止 release，不能只依赖开发机缓存中“能构建”。

### 3.25 故障恢复必须按故障域执行完整重启事务

每个 Service、HAL 和共享资源必须声明 `fault_domain_id`、依赖者、可独立重启性和故障传播边界。显示、音频、网络、存储及共享 I2C 等故障域不能用“删除一个 task 再创建”代替恢复；统一执行 `RUNNING → QUIESCING → STOPPED → REINITIALIZING → SELF_TEST → READY`。进入 QUIESCING 后禁止新操作，取消或 drain 在途 operation/callback，递增资源与 handle generation 并收缩 capability；只有重新初始化、自检和 readiness barrier 全部通过后才恢复能力。共享资源故障必须协调同域所有 borrower，禁止仅重启一个使用者后继续复用旧句柄。无法 join 的 task 所访问的资源不得释放或复用，故障域进入隔离的 DEGRADED/SAFE_MODE。

### 3.26 超时必须服从端到端 deadline budget

Boot、stop、故障域 restart、sleep prepare、配置生效、配网和会议 finalize 等事务都必须从调用方接收一个单调时钟绝对 deadline，并向子步骤传播剩余预算；禁止每一层重新获得完整 timeout 导致总耗时无界累加。事务预算包含排队、重试、drain、自检和回滚，并为安全 cleanup 保留独立且有界的 reserve。deadline 到期时必须确定性选择完成、取消、回滚或隔离，诊断记录最慢 phase、已耗时和剩余预算，不能继续在后台产生迟到副作用。

### 3.27 发布结论必须由可追踪证据证明

每项架构要求、profile capability、runtime budget 和 Phase 退出条件必须具有稳定 requirement ID，并关联 owner、实现 artifact、测试 ID、目标 profile、firmware/artifact digest、证据 URI/hash 和结论。测试未运行、证据缺失或仅有另一固件 digest 的结果均不等于通过。临时 waiver 必须记录批准人、理由、影响、补偿控制和到期日；过期 waiver 自动使门禁失败，不能靠文档勾选替代 CI/HIL 证据。

### 3.28 服务重启后必须从权威状态重新收敛

故障域恢复不能以“task 已重新创建”作为完成条件。每个 Service 必须声明 authoritative state、可丢弃 ephemeral state、durable checkpoint、依赖的 config/capability revision 和 restart reconciliation 函数。重启时禁止序列化或复活 mutex、task handle、callback context、DMA pointer、旧 operation、临时 UI snapshot 等运行时对象；依赖恢复后，Service 从 Configuration/Persistence/Capability 等唯一事实来源重新读取不可变快照，以幂等方式恢复订阅、计划任务、当前 UI 与设备期望状态。只有 reconciliation 完成并验证 observed state 与 desired state 一致后才能重新开放 admission；无法判断外部副作用结果时保持 `UNKNOWN_OUTCOME`，不得通过“重放全部命令”猜测恢复。

### 3.29 复位与上电瞬间也必须满足电气安全

每个 profile 必须提供 electrical-safety manifest，逐项声明高风险 GPIO/电源轨/功放 mute/背光/reset/chip-select 的 ROM reset 默认态、外部上下拉、有效电平、strapping 约束、允许驱动阶段、上电/掉电顺序、RTC hold 和最大毛刺窗口。最早期 board-safe port 在任何器件 probe、日志外设复用或通用 HAL init 前只应用已验证的安全状态；profile 未确认时保持高阻、功放静音、背光/高功率负载关闭。软件无法控制 ROM/bootloader 窗口的安全性必须由硬件上下拉或电源门控保证，不能把 `app_main()` 之后写 GPIO 当作上电无毛刺证明。WDT、brownout、panic、deep-sleep entry/wake 和烧录模式都必须纳入同一安全状态矩阵。

### 3.30 动态时钟和资源压力必须由统一策略治理

DFS、tickless、modem/light sleep 会改变 CPU/APB 时钟、timer、外设吞吐和 ISR latency。每个外设/Service 必须声明 clock-domain dependency、允许频率范围、PM lock owner、切频前后 barrier、需要重新计算的 baud/divider/timeout 和稳定时间；Audio sample clock、LCD QSPI/DMA、I2C/touch、UART/diagnostic 与单调时间必须在所有已发布频点验证。Resource Pressure Service 将 internal heap/largest block、DMA pool、queue、stack、storage 和 thermal/battery 信号归并为 `NORMAL → PRESSURE → CRITICAL`，按确定优先级拒绝新低价值工作、降低动画/图片/telemetry、释放 cache，同时为 cancel/alarm/wake、录音收尾和 Storage commit 保留 emergency reserve。压力降载不得由各板私自改变业务状态，也不能通过无限重启暂时掩盖泄漏。

### 3.31 HIL 证据本身必须可验证和可复测

HIL evidence 除 firmware/profile digest 外，还必须记录 board serial/hw revision、fixture ID/version、测量仪器与校准有效期、供电/温度/网络条件、测试脚本 revision、原始数据 hash、重复次数和不确定度。Golden screenshot/audio/power baseline 的创建或更新必须有独立批准与差异说明，禁止测试失败时自动覆盖 golden。Flaky case 只能隔离并设 owner/期限，不能重跑到通过后丢弃失败样本；release 结论采用预先声明的样本量、容差和统计规则，证据包签名或写入不可变存储以防事后替换。

## 4. 目标目录结构

> 2026-08-11 Fangtang peripheral/startup-selector 文件级收口：将不依赖共享画面 primitive 的 ADC 电池/充电监控和 GPIO0 启动前双击网络选择窗口移至 `boards/fangtang_4g/fangtang_peripheral_adapter.c` 与 `fangtang_input_selector.c`，并且仅由 Fangtang profile 的 CMake source list 编入。共享紧凑屏 renderer 继续只通过既有 `compact_peripheral_adapter_*` 和 `compact_input_adapter_*` 私有 contract 初始化、读取快照、停止 profile worker 与获得一次性 selector 意图，不接触 ADC、charge GPIO、任务/信号量或 GPIO0 时序。为避免独立 translation unit 依赖偶然 include 顺序，Fangtang input/audio adapter 与 transition bridge 显式先引入 `freertos/FreeRTOS.h`；audio adapter 也显式引入 `freertos/idf_additions.h` 以声明 pinned task API。使用 Fangtang 缓存 Xtensa compile command 对两个新源及 transition bridge 分别完成 `-fsyntax-only`。该项是物理 ownership 收口，不等于完整 board deinit/restart（ADC unit 仍 boot-lifetime）、完整 Fangtang bridge 拆分、完整 link 或 COM5 HIL；IDF 环境恢复后应实测冷启动采样、充电状态、GPIO0 双击切换、启动失败 rollback 的 monitor stop、常规输入与 DISPLAY_OFF/wake。

> 2026-08-11 Fangtang NV3023 thinking patch 与 network redraw 控制收口：移除 transition bridge 内对 `s_present_staging`、front framebuffer、thinking phase/cadence、scene guard、LCD mutex 和逐行 panel submit 的直接访问。新增私有 `compact_display_animation_patch_t` contract：Fangtang display adapter 仅把三点 activity strip 的原生坐标与像素组合进共享提供的 DMA staging buffer；共享 compact renderer 统一拥有 thinking worker、420ms cadence、状态/前景/录音/回复 admission、panel submit、失败后的 front-frame invalidation 和 front-buffer repair。Bread adapter 显式声明无 profile patch，保留原有共同 robot-mouth 画面。与此同时，4G/Wi-Fi header mark 的 profile fact 改由 `compact_profile_{publish,network_transport_is}_cellular()` 表达；共享 renderer 再次单独拥有 LCD lock、idle/quiet redraw admission 与调用 `show_state_screen()` 的决定，Fangtang profile 只存/读其 transport identity，Bread 为 no-op。Fangtang transition bridge 不再直接引用 thinking renderer state 或旧 animation/network display hooks；Bread 与 Fangtang 缓存 Xtensa `-fsyntax-only` 通过，HAL boundary 与 diff 检查通过。该切片未消灭 bridge 对共享像素/text/frame primitive 的其余依赖，亦不等于完整 link/COM4/COM5 HIL；恢复 IDF 后须回归 Bread robot mouth、Fangtang 三点 strip、thinking 阶段重绘、传输失败后全帧修复、idle/quiet 下 Wi-Fi/4G 标记切换、录音/回复/闹钟抢占及 DISPLAY_OFF/wake。

> 2026-08-11 Fangtang network identity 独立 source 收口：将 transition bridge 中仅用于 4G/Wi-Fi standby 标记的 `s_fangtang_network_transport_cellular` 及 `compact_profile_{publish,network_transport_is}_cellular()` 移至 `boards/fangtang_4g/fangtang_network_identity.c`，并仅由 Fangtang profile CMake source list 编入。shared renderer 仍是唯一的锁 owner：它在 `board_port_set_network_transport()` 内获得 LCD mutex、更新 profile fact，并仅于 idle/quiet、未熄屏/录音/前景时决定重绘；Fangtang source 只保存/读取产品 identity，Bread 保留 no-op。Fangtang transition bridge 因而不再拥有任何自身 static state；该物理迁移不改变 Connectivity uplink policy、Gateway 生命周期或 4G/Wi-Fi 图标 raster，也不消除 bridge 对共享 scene/text/pixel primitive 的剩余依赖。新 source 与 bridge 分别通过缓存 Xtensa `-fsyntax-only`，HAL boundary 与 diff 检查通过；完整 link/COM5 transport 切换、待机标记和 DISPLAY_OFF/wake HIL 仍待 IDF 环境恢复。

> 2026-08-11 紧凑屏 profile identity runtime context 收口：Fangtang startup/state/status identity hook 曾直接读取 shared renderer 的 ambient time/date/weekday、gateway ready、command stage、current state 与 thinking phase，令 profile assembler 必须依赖 bridge 内部全局。新增私有 `compact_profile_identity_state_t`，shared renderer 在 LCD lock 内以不可变场景事实构造 context 后调用 hook；Fangtang 的状态/状态页和 sugar/cube/art indicator 都只消费该 context，cube/indicator 的 animation phase 也改为显式参数。Profile 仍复用 transition renderer 的受控像素/text/frame primitive，未复制 scene 状态机、锁或 Display API；Bread hook 为无副作用 false。静态搜索确认 Fangtang identity 与 art include 不再读取以上 shared state globals，Bread shared renderer 与 Fangtang bridge 的缓存 Xtensa `-fsyntax-only` 均通过，HAL boundary 与 diff 检查通过。该切片不等于完整 renderer context/primitive 模块化、完整 bridge 拆除或 COM4/COM5 HIL；恢复 IDF 后应覆盖 ambient/foreground 状态、thinking stage/phase、状态页 fallback、Hub pet 优先级、Fangtang cube/sugar 与 DISPLAY_OFF/wake。

> 2026-08-11 Fangtang 待机网络标记独立 translation unit 收口：复核上一轮 product-identity 搬迁后，发现 `fangtang_identity_network_renderer.inc` 虽已物理位于 profile 目录，却仍必须由 transition bridge `#include`，并直接借用 bridge 的 `glyph5x7()`、`fill_rect_solid()` 和 `strlen()`；新增 Fangtang 硬件时仍会被迫修改或理解共享 renderer 的私有符号。现将 WIFI/4G 字标、signal-bar/Wi-Fi arc 几何与其 5x7 字形表移至独立 `fangtang_identity_network_renderer.c/.h`。新的私有 raster contract 只接收已有 composition frame 内的有界 `fill_rect` callback，不授予 framebuffer、锁、DMA、显示提交、场景选择或网络状态写入权限；transition bridge 仍仅将其现有 pixel primitive 作为 callback 传入。Fangtang 保持既有 4G 宽度 38、WIFI 宽度 68、坐标、离线信号颜色与 Wi-Fi 白色文字，避免视觉回归；共享 Device/Platform/HAL 头不增加显示或板型 API。缓存 Fangtang Xtensa syntax check（补入现有 NV3023 managed-component include）通过；`tools/check-hal-boundaries.ps1`、`git diff --check` 通过。该变化只收口 network raster 的物理 ownership，Fangtang transition unit 仍 include legacy compact renderer，sugar/cube art 仍借用 common primitives，未完成完整 bridge 拆除、full link 或 COM5 HIL；环境恢复后必须实测 COM5 Wi-Fi/4G 切换、在线/等待、calendar 对齐、宠物优先级及 DISPLAY_OFF/wake。

> 2026-08-11 Fangtang sugar/cube 产品绘制独立 source 收口：继续拆 transition bridge，原 `fangtang_identity_art_renderer.inc` 直接读取 bridge 的 `color()`、`robot_rgb_mix()`、临时 bitmap allocator、`draw_bitmap_sync()`、`fill_rect_solid()`、`LCD_WIDTH/HEIGHT` 与 linker asset 符号，虽在 profile 文件夹内仍无法单独编译或复用于新硬件。现替换为独立 `fangtang_identity_art_renderer.c/.h`：profile source 自有 RGB565 wire-order color/alpha blend、sugar/cube geometry、granule/seam/activity raster；bridge 仅组装一个受限 `fangtang_identity_raster_t`，提供有界临时位图申请/释放、在已打开 composition frame 内提交矩形、fill_rect 与 panel 尺寸。asset linker symbol 继续只由 Fangtang bridge 拥有并以字节 span 传入，art source 不触及共享状态、LCD mutex、DMA、frame begin/finish、network 或业务文案。启动、idle 无远程宠物 fallback、listening/thinking/speaking/status 页面仍沿用同一 sugar/cube 尺寸/坐标和 remote-pet 优先级。缓存 Fangtang Xtensa syntax check 已分别覆盖 bridge 与新增 art source；HAL boundary 和 diff check 通过。此步不宣称完整 bridge、full link 或 COM5 HIL 已完成；后续仍须在 COM5 验证 alpha 颜色、无资源时 cube fallback、各状态指示、内存失败降级、Display-off/wake。

> 2026-08-11 Fangtang 产品页面 composition 独立 source 收口：在 sugar/cube raster 已独立后继续审计，transition bridge 仍直接包含启动页、ambient calendar/online/WIFI/4G 组装、remote-pet fallback、交互页和 status 页的 Fangtang 坐标/文案；这使 profile 视觉策略仍与 shared renderer private primitive 混在同一 translation unit。现新增 `fangtang_identity_composer.c/.h`，将启动、待机与状态页面的产品 composition 迁入独立 profile source。composer 只消费不可变 identity snapshot、背景色及一组受限 callback：文字/颜色、现有 frame 内画图、remote-pet 结果、transport fact 与 art/network raster；它不读取 renderer globals，不创建 task/锁/DMA，也不决定 scene admission。bridge 仅在已完成 shared renderer include 后组装 callback 并转发三个 profile hook；shared renderer 继续唯一拥有状态机、frame/lock/presentation、远程宠物管理、Display-off/wake 和 redraw policy。缓存 Fangtang Xtensa syntax check 覆盖 bridge 与 composer source；HAL boundary、diff check 通过。完整 link 与 COM5 HIL 尚未完成，恢复环境后须核验 startup、Wi-Fi/4G idle、remote-pet 优先、五种 activity/status、memory fallback 和 DISPLAY_OFF/wake。

```text
iot-agentos/main/
  app/
    app_main.c
    app_interaction.c
    app_interaction.h
    app_ui.c
    app_ui.h
  services/
    boot_coordinator.c
    boot_coordinator.h
    task_registry.c
    task_registry.h
    service_supervisor.c
    service_supervisor.h
    resource_service.c
    resource_service.h
    memory_service.c
    memory_service.h
    resource_pressure_service.c
    resource_pressure_service.h
    device_api.c
    device_api.h
    device_event_queue.c
    device_event_queue.h
    input_service.c
    input_service.h
    audio_service.c
    audio_service.h
    wake_word_service.c
    wake_word_service.h
    display_service.c
    display_service.h
    power_service.c
    power_service.h
    wake_deadline_service.c
    wake_deadline_service.h
    clock_service.c
    clock_service.h
    battery_policy_service.c
    battery_policy_service.h
    privacy_service.c
    privacy_service.h
    feedback_service.c
    feedback_service.h
    persistence_service.c
    persistence_service.h
    storage_service.c
    storage_service.h
    connectivity_service.c
    connectivity_service.h
    identity_service.c
    identity_service.h
    entropy_service.c
    entropy_service.h
    credential_service.c
    credential_service.h
    provisioning_service.c
    provisioning_service.h
    gateway_service.c
    gateway_service.h
    update_service.c
    update_service.h
    release_catalog.c
    release_catalog.h
    tool_dispatcher.c
    tool_dispatcher.h
    diagnostic_service.c
    diagnostic_service.h
    capability_service.c
    capability_service.h
    configuration_service.c
    configuration_service.h
  domain/
    command_capture_service.c
    command_capture_service.h
    meeting_service.c
    meeting_service.h
    alarm_service.c
    alarm_service.h
    sleep_schedule_service.c
    sleep_schedule_service.h
    tool_handlers.c
    tool_handlers.h
  platform/
    storage_api.h
    storage_port.h
    connectivity_api.h
    connectivity_port.h
    update_catalog_api.h
    update_catalog_port.h
    identity_api.h
    identity_port.h
    clock_api.h
    clock_port.h
    entropy_api.h
    entropy_port.h
    diagnostic_api.h
    diagnostic_port.h
    board_safe_port.h
    esp_idf/
      storage_esp_vfs.c
      connectivity_esp_wifi.c
      update_catalog_hub.c
      identity_esp_chip.c
      clock_esp_time.c
      entropy_esp_random.c
      diagnostic_usb_serial_jtag.c
      board_safe_early.c
  hal/
    board_hal.h
    board_capabilities.h
    input_hal.h
    audio_hal.h
    display_hal.h
    power_hal.h
    wake_hal.h
    privacy_hal.h
    feedback_hal.h
    hal_lifecycle.h
  render/
    render_common.c
    render_common.h
    font_common.c
    font_common.h
  boards/
    bread_compact/
      board_profile.c
      board_resources.c
      input_bread.c
      audio_bread_i2s.c
      display_bread_st7789.c
      power_bread.c
      wake_bread.c
    echoear_2st/
      board_profile.c
      board_resources.c
      input_echoear_cst816.c
      audio_echoear_es7210_es8311.c
      display_echoear_st77916_round.c
      power_echoear.c
      wake_echoear.c
    fangtang_4g/
      board_profile.c
      board_resources.c
      input_fangtang_single_key.c
      audio_fangtang_i2s.c
      display_fangtang_nv3023.c
      connectivity_fangtang_ml307.c
      sensor_fangtang_battery.c
      power_fangtang.c
      wake_fangtang.c
```

迁移期保留 `board_port.h/.c` 作为兼容 facade；所有调用完成切换后再删除 facade，避免一次性重写造成难以定位的三硬件回归。Bread 与 Fangtang 可以复用经过明确边界提取的 direct-I2S、字体或绘图 primitive，但必须拥有独立 profile、资源表、Renderer 和输入/连接适配；禁止继续用一个大型 `board_port_bread_compact.c` 配合板型宏承载两块产品板。

Facade 只做参数/错误/事件格式转换，不保存新的业务状态、不创建任务、不拥有硬件资源。每个 facade API 建立迁移台账：legacy owner、新 owner、切换 build flag、允许 shadow 的纯函数、禁止 shadow 的副作用、状态 handoff、三硬件验证证据和删除截止 Phase。新旧实现不能在同一 release build 中由远端动态任选；需要回退时只允许使用签名 build manifest 或物理授权的诊断策略，并在切换前 quiesce、drain callback/DMA、失效旧 handle/generation，再把唯一 ownership 交给另一侧。

Shadow 模式仅比较无副作用的标准化输出，例如输入 gesture classification、capability projection、UI layout decision、配置校验或协议 JSON；输出携带相同 trace input ID，但 shadow 结果不得发布 Device Event、写 NVS/文件、发网络请求、驱动屏幕/音频或影响 health。差分允许列表必须注明浮点/时间/动画等容差，超过阈值只记录有界诊断并由人工/CI 决定切换，不自动在设备上来回 flip。

## 5. HAL 契约设计

### 5.1 板型入口与能力描述

每个构建只链接一个 `board_hal_get()` 实现。Kconfig/CMake 负责选择实现，运行中的业务代码不负责选择。

```c
typedef struct {
    bool has_touch;
    bool has_activate_key;
    bool has_volume_keys;
    bool supports_manual_response_navigation;
    bool supports_display_sleep;
    bool supports_output_volume;
    bool supports_wake_word;
    bool supports_audio_capture;
    bool supports_audio_playback;
    bool has_display;
    bool has_persistent_storage;
    bool has_battery_telemetry;
    const display_capabilities_t *display;
    const input_capabilities_t *input;
    const audio_capabilities_t *audio;
} board_capabilities_t;

typedef struct {
    device_power_state_mask_t supported_states;
    wake_source_matrix_t wake_matrix;
    uint32_t min_sleep_duration_ms[DEVICE_POWER_STATE_COUNT];
    uint32_t worst_resume_latency_ms[DEVICE_POWER_STATE_COUNT];
} board_power_capabilities_t;

typedef struct {
    uint32_t struct_size;
    uint32_t abi_version;
    const char *id;
    uint32_t profile_version;
    uint32_t board_revision_min;
    uint32_t board_revision_max;
    uint32_t hal_api_version;
    const board_capabilities_t *capabilities;
    const board_power_capabilities_t *power_capabilities;
    const hal_lifecycle_ops_t *lifecycle;
    const input_hal_ops_t *input;
    const audio_hal_ops_t *audio;
    const display_hal_ops_t *display;
    const power_hal_ops_t *power;
    const wake_hal_ops_t *wake;
    const privacy_hal_ops_t *privacy;
    const feedback_hal_ops_t *feedback;
} board_hal_t;

const board_hal_t *board_hal_get(void);
```

能力表只描述硬件事实，不保存业务规则。例如“有触摸屏”是能力，“双击开始会议”不是能力，后者属于共享交互策略和板级输入映射。

顶层布尔值只用于粗粒度 feature gate，不能替代结构化能力：

- `display_capabilities_t`：逻辑宽高、物理形状、安全区域/圆形 mask、默认 rotation、像素格式/色序、对齐、最大 transfer/stripe、DMA 要求、局部刷新与 panel sleep/GRAM 保持能力。共享 UI 只使用逻辑坐标与 layout class；controller offset、MADCTL、RGB/BGR 和 QSPI/SPI 提交方式留在 Renderer/HAL。
- `input_capabilities_t`：标准 control 集合、touch 点数/范围、原始坐标到逻辑显示坐标的 rotation/mirror/calibration transform、IRQ/轮询特性、唤醒能力引用和可用 accessibility binding。Input HAL 必须先完成去抖、重复 contact 合并和坐标变换，再发布标准事件；业务不得知道 CST816 坐标或某块板的按键 GPIO。
- `audio_capabilities_t`：capture/playback 支持的采样率、通道、PCM 格式、native frame size/alignment、最大连续流、full-duplex/回声路径、硬件音量/mute 和典型启动/排空延迟。Audio Service 选择双方支持的标准格式；重采样、声道选择、位宽转换和 codec 增益留在共享转换层或有限板级 preprocess，不让领域业务按 codec 分支。
- `power/sensor/storage/connectivity` 等扩展同样使用带版本与长度的 descriptor，不为每个新差异继续向 `board_capabilities_t` 添加无限布尔字段。
- `privacy/feedback_capabilities_t`：物理麦克风 mute 开关/断路状态、独立录音 LED、状态 LED、震动/触觉和是否在主 CPU/显示失效时仍可用。业务只发布“正在采集、麦克风被硬件静音、需要安全告警”等标准状态；Feedback/Privacy Service 再映射到灯、屏幕、触觉或音频，不允许领域代码操作 LED GPIO，也不把有 LED 当成开始录音的业务条件。

显示与触摸必须共享同一个版本化 logical coordinate space。rotation、镜像、panel offset 或圆屏安全区变化时，Renderer 和 Input transform 一起由 profile revision 更新；HIL 用屏幕九点/边缘点击校准验证“看到的位置就是命中的位置”。圆屏 mask 只影响布局/命中区域，不允许用矩形坐标在不可见角落放置可交互控件。

所有校准值都必须携带 provenance：calibration schema/version、目标 board/hw revision、器件/通道标识、量产批次或工装 ID、测量条件、CRC/签名（按安全等级）、生成时间可信度和允许范围。校准与普通用户配置分 namespace/权限；加载时验证硬件匹配、完整性和数值边界，失败使用明确安全默认或关闭相应 capability，禁止把另一块板、另一旋转方向或另一 codec 通道的校准静默套用。重新校准采用双缓冲/原子提交并保留上一已确认 revision 供回滚。

物理隐私控制优先于软件意图。若 profile 声明硬件麦克风 mute/断路，Input/Privacy HAL 将稳定状态发布为标准 privacy event，Audio Service 立即禁止新 capture 并有界停止在途 capture；会议/指令 Service 收到统一错误或暂停原因，不读取 GPIO。解除 mute 不自动开始/恢复录音，必须经过新的显式业务动作。任何有效 capture 从硬件真正 start 到完全 stop 的整个窗口都驱动可验证的录音指示；若独立 LED 是法规/产品要求，其电气路径和故障策略必须能覆盖显示关闭、UI 卡死和网络离线。

布尔 `supports_touch_wakeup` 不足以描述现实约束，电源能力必须是 `sleep depth × wake source` 矩阵，并携带 active level、pull/hold、RTC/GPIO 域、外设供电依赖、最小脉宽、最短有效休眠时长和最坏恢复延迟。静态 profile 表达“板级可能性”，运行时 `effective_power_capabilities` 再与编译配置、PM 初始化结果、自检和当前健康状态求交集。

HAL 接口带显式 `hal_api_version`。启动时校验版本、必选函数、能力与 ops 一致性；不兼容时拒绝启动并输出可诊断错误，不允许静默调用错位的函数表。

仅有 `hal_api_version` 还不足以防止 C 结构体布局漂移。所有跨模块 descriptor/ops 顶层结构都带 `struct_size + abi_version`；新增可选字段只允许尾部追加，读取前先检查 `struct_size`，保留字段必须为零。必选函数表不能为 `NULL`，可选函数必须同时满足 capability、结构大小和函数指针三重校验。构建时加入 `_Static_assert` 校验关键枚举值、字段偏移和大小；运行时记录 profile/ABI 版本，不允许把 profile 版本、NVS schema、协议版本和 HAL ABI 混成一个版本号。

所有跨队列、持久化和协议的 enum/event/tool/status ID 使用集中 schema registry：显式数值、introduced/deprecated version、payload schema、owner、priority、retention 和兼容策略。已发布数值永不复用，删除只保留 tombstone；未知 ID 按边界规则拒绝或忽略并计数。禁止依赖 C enum 自动递增、按源码顺序序列化或由各板自行定义同名不同值。registry 生成 C 定义、协议 schema、测试向量和文档，CI 校验生成物未漂移。

### 5.2 生命周期与板级 Resource Manager

```c
typedef struct {
    esp_err_t (*init)(const board_runtime_config_t *config);
    esp_err_t (*start)(void);
    esp_err_t (*stop)(TickType_t timeout);
    void (*deinit)(void);
} hal_lifecycle_ops_t;
```

板级启动顺序固定为：最小早期启动上下文（读取 reset/wake cause，不驱动高风险 GPIO）→ 校验 profile → 初始化 Resource Manager → 初始化各 HAL → 初始化共享 Service → 启动 Display Task → 启动 Input/Audio/Wake 后台任务 → 开放事件入口。停止时严格逆序执行。

在读取/验证完整 profile 前，只能调用极小的 `board_safe_early_init()`：依据随 artifact 固化并可由安全 identity 选择的 electrical-safety manifest，把功放 mute、背光/高功率电源、panel/codec reset 和冲突 chip-select 置于硬件已证明的安全状态。该路径不能 probe I2C/SPI 器件、分配 heap、启动 task 或依赖 NVS；未知 board revision 时只允许公共安全交集。manifest 必须区分 ROM reset/bootloader/app/deep-sleep/下载模式各阶段，并明确哪些 pin 的安全必须由外部上下拉或 load switch 保证。进入 panic/WDT/brownout 前若尚有执行机会，只执行 IRAM/internal-memory 可达的最小 fail-safe；不能承诺的软件动作不得替代原理图级安全证明。

Resource Manager 至少登记：GPIO ownership、I2C/SPI host、I2S channel、panel/codec device handle、DMA/PSRAM buffer、PM lock、电源轨和相关 mutex。借用者不能删除总线或设备；只有 Resource Manager 能在所有借用者停止后销毁资源。

共享总线错误恢复必须由 Resource Manager/Bus Service 编排，不能由某个子 HAL 私自 reset：记录总线 generation 和 borrower epoch，先阻止新事务、取消/等待在途操作、将受影响 Input/Audio/Display health 一次性收缩，再按 profile 允许的电气流程执行 controller reset、SDA/SCL stuck 检测/clock recovery、器件电源重置和重新 probe。恢复成功后重新创建借用 handle 并经过组件自检/readiness barrier；旧 generation handle 确定性失效。若线路仍被拉低、恢复次数超预算或 reset 会影响安全关键器件，保持 DEGRADED/SAFE_MODE，不反复切换总线电源。

内存本身也是分类型资源。Resource Manager/Memory Service 必须区分 internal 8-bit、DMA-capable internal、IRAM/RTC、普通 PSRAM 和 DMA-capable PSRAM，声明 alignment、cache-off 可访问性、生命周期、owner 与最大块大小。Display、Audio、TLS 和 Storage 不得用“总 free heap 足够”代替 largest-block/能力检查；长期 framebuffer/模型优先在启动期预留，短时 TLS/DMA staging 使用有界 pool/reservation，失败按能力降级，禁止在压力下无界 allocate/free 造成碎片。

进入 `DEVICE_READY` 后冻结关键实时路径的内存方案：Input/Audio ISR、audio capture/playback、Display DMA completion、Power COMMIT 和 WDT 前诊断只使用启动期预分配 pool/固定对象，不在 steady state 调用通用 heap。允许动态分配的 Gateway/Provisioning/媒体解码任务必须有 owner quota、单操作 reservation、最大块和归还 deadline；allocation failure 通过稳定错误/能力降级处理。主机测试注入 allocator 并验证零泄漏，实机门禁同时观察启动后累计 alloc/free 次数和碎片趋势。

任何在 flash/NVS 写入导致 cache disabled 期间仍可能运行的 task、ISR、callback，其栈、代码和访问数据必须满足对应 IRAM/internal-memory 契约；PSRAM task stack 不能执行这类路径。DMA buffer 必须在 fence/driver completion 后才能归还 pool，Resource Manager 在释放前检查借用计数、内存类别和 cache/DMA 状态。

生命周期的唯一外部编排入口是 `board_hal.lifecycle`；各子 HAL 的 init/start/stop/deinit 属于板级内部实现，不进入业务可见头文件。Resource Manager 维护已成功步骤位图和资源借用表，失败时根据实际完成步骤精确逆序回滚，而不是假定所有组件均已初始化。

现有 `app_main()` 和板级初始化大量使用 `ESP_ERROR_CHECK`。迁移时必须按错误分类替换：仅不可恢复的芯片/内存基础设施错误允许触发受控重启；可选组件失败进入 DEGRADED；板型/分区不安全进入 SAFE_MODE；普通网络、显示、音频、存储或 PM 初始化失败不得直接 panic。Boot Coordinator 记录失败阶段和首次根因，避免重启后再次无条件走同一 fatal path。

Task Registry 是生命周期契约的一部分：创建任务前登记 owner/stop token，创建成功后登记 handle；`stop()` 先关闭发布入口，再广播停止，等待 join，最后注销 event handler/timer/ISR 并释放队列。超时任务进入诊断与隔离，不能直接释放其仍可能访问的资源。

Resource/Service manifest 还必须给每个组件分配 `fault_domain_id`，至少区分 shared-I2C、audio、display、network 和 storage domain，并标明哪些 component 可独立重启、哪些必须随共享 owner 一起 quiesce。故障域重启由 Boot Coordinator/Service Supervisor 发起，执行 `RUNNING → QUIESCING → STOPPED → REINITIALIZING → SELF_TEST → READY`：先关闭 admission，冻结新 handle，取消或等待在途 operation 与 callback drain，再递增 domain/resource generation、逆序停止、重建并自检。重启导致的 operation 终态必须显式分类为 `CANCELLED`、`RETRYABLE`、`INTEGRITY_DAMAGED` 或 `UNKNOWN_OUTCOME`；存在外部副作用且 outcome 未知时不得自动重试。只有 readiness 与健康滞回满足后才能恢复 capability；重启失败或 task 无法 join 时隔离整个 fault domain，不释放仍可能被访问的资源，也不允许旧 generation handle/callback 在恢复后复活。

每个可重启 Service 还必须登记 restart reconciliation contract：authoritative source、durable/ephemeral 字段、最后确认 revision、依赖 readiness 和 observed-state probe。重建对象后从 Configuration/Persistence/Capability/领域 owner 获取新快照，幂等恢复订阅、timer、UI desired scene 和硬件期望配置；禁止 memcpy 旧 service context 或恢复锁、队列、task handle、driver pointer。reconciliation 发现 durable state 与硬件 observed state 不一致时按领域规则继续、补偿或保持降级，并记录差异；只有新 generation 的 subscriber 全部登记、旧 subscription 已 tombstone 且 desired/observed state 对齐后才能进入 READY。

### 5.3 Boot Coordinator、板型识别与运行模式

Boot Coordinator 负责 profile 校验、依赖拓扑、readiness 和启动失败策略：

```text
RESET
  → PROFILE_VALIDATED
  → RESOURCES_READY
  → REQUIRED_SERVICES_READY
  → DEVICE_READY
  → ONLINE
       ↘ DEGRADED（已实现必选能力临时故障，或非公共物理增强不可用；不掩盖正式实现缺口）
       ↘ SAFE_MODE（板型/分区/关键资源不安全）
```

组件分级：

- 必选：Resource Manager、主输入路径、业务事件队列、Persistence Service 和至少一种用户可见错误反馈。
- 可降级：宠物动画、动态字形、图片、扬声器、音量控制、唤醒词和非关键传感器。
- 可重试：Wi-Fi、Hub handshake、SNTP、MultiNet 内存分配和临时 codec/存储故障。
- 致命错配：HAL API 不兼容、board revision 越界、GPIO ownership 冲突、目标芯片/flash/PSRAM 不满足、分区布局不兼容。

板型识别信息至少包含 `board_id`、`board_revision`、`profile_version`、`hal_api_version`、ESP target、flash/PSRAM 下限和可选器件签名。探测必须是只读且电气安全的；不能安全探测时，使用量产写入的只读 manifest/eFuse/NVS 标识，并由烧录工具验证目标固件。错配进入 SAFE_MODE，不初始化功放、电源轨或高速输出 GPIO。

SAFE_MODE 必须定义不依赖故障组件的最小反馈链路，并按 profile 排序选择：安全显示错误页 → 独立状态 LED → 固定短提示音 → 受限串口诊断 → 只读诊断/配网 AP。若显示已判故障，安全模式不能再依赖屏幕；若音频或输入故障，也不能依赖对应组件完成退出。所有反馈均不可用时仍保留 reset reason、RTC/NVS 最小故障码和量产工具可读诊断。SAFE_MODE 不开放未认证 shell，不自动擦除用户数据，也不反复重启。

故障码持久化不能成为新的启动依赖：早期阶段优先写 RTC slow memory/保留寄存器中的小型 boot record，NVS 可用后再批量归档。维护 boot-attempt counter、最后成功阶段和稳定运行确认点；连续在同一阶段失败达到预算时绕过非必要组件进入 SAFE_MODE，而不是形成 boot loop。正常运行达到稳定窗口后才清零 attempt counter。

Boot Coordinator 为每个可重试组件维护 retry budget、指数退避、抖动、连续失败计数、冷却时间和 circuit breaker。Wi-Fi、Hub、MultiNet、codec、I2C、显示和存储不得在失败循环中无限 init/deinit；达到预算后进入 DEGRADED/SAFE_MODE，并等待用户动作、网络事件或冷却到期。自动整机重启同样有窗口化预算，防止 WDT/内存碎片形成 reboot storm。

Boot Coordinator 同时是跨 Service deadline budget 的监督者。Boot、stop、fault-domain restart、sleep prepare、configuration apply 和 provisioning 各有一个绝对单调 deadline；依赖图中的每一步只能消费父事务剩余时间，不能在下一层重新开始完整 timeout。manifest 为关键 phase 设置软预算、硬截止和 cleanup reserve，重试会消耗同一总预算。到期后 Coordinator 停止 admission，按事务契约取消/回滚；不能在预算内停止的 fault domain 被隔离。启动或恢复诊断必须给出关键路径、最慢 phase、排队/执行/cleanup 耗时和 deadline miss owner。

### 5.4 Device API、平台端口与异步操作上下文

共享领域 Service 只 include `device_api.h` 及稳定的 `storage_api.h`、`connectivity_api.h`、`identity_api.h`；`*_port.h` 是平台 Service 的内部 SPI，不能被领域代码直接 include。Device API 只表达可复用设备原语，不表达会议或指令用例：

```c
typedef enum {
    DEVICE_STATUS_OK = 0,
    DEVICE_STATUS_INVALID_ARGUMENT,
    DEVICE_STATUS_INVALID_STATE,
    DEVICE_STATUS_BUSY,
    DEVICE_STATUS_TIMEOUT,
    DEVICE_STATUS_NO_MEMORY,
    DEVICE_STATUS_IO,
    DEVICE_STATUS_NOT_SUPPORTED,
    DEVICE_STATUS_CANCELLED,
    DEVICE_STATUS_INTEGRITY,
    DEVICE_STATUS_SECURITY,
} device_status_t;

typedef uint32_t device_timeout_ms_t;

device_status_t device_init(device_event_cb_t callback, void *arg,
                            device_callback_token_t *token);
device_status_t device_start(void);
device_status_t device_stop(device_timeout_ms_t timeout_ms);

device_status_t device_capture_open(const device_operation_context_t *op,
                                    const device_audio_format_t *format,
                                    device_capture_handle_t *handle);
device_status_t device_capture_read(device_capture_handle_t handle,
                                    int16_t *pcm, size_t capacity,
                                    size_t *samples_read, uint16_t *level);
device_status_t device_capture_close(device_capture_handle_t handle,
                                     device_status_t stream_status);
device_status_t device_play_audio_async(const device_operation_context_t *op,
                                        const device_audio_source_t *source);
device_status_t device_set_output_volume(unsigned percent);
device_status_t device_present_ui(const app_ui_model_t *model);
device_status_t device_power_request_state(device_power_state_t target,
                                           const device_operation_context_t *op);
device_status_t device_capabilities_snapshot(device_capabilities_snapshot_t *out);
device_status_t device_capabilities_snapshot_release(
        device_capabilities_snapshot_t *snapshot);
device_status_t device_callback_unregister(device_callback_token_t token,
                                           device_timeout_ms_t drain_timeout_ms);
```

`command_capture_service` 用通用 capture API 实现固定时长、WAV 封装和取消；`meeting_service` 用同一 capture API 加 Storage/Connectivity API 实现长录音、断点恢复和上传；`alarm_service` 只使用 Clock、Persistence、Audio 和 UI 能力。网络请求、Hub 协议和会议端点不得进入板级 HAL；文件路径、SPIFFS partition label 和 `FILE *` 不得进入会议状态机。

公共 Device/Platform API 不 include ESP-IDF 或 FreeRTOS header，不暴露 `esp_err_t`、`TickType_t`、task/queue/event-group handle 或 driver 指针。内部 HAL SPI 和 ESP-IDF port 可继续使用平台类型；Device/Platform Service 在唯一边界把平台错误映射为稳定 `device_status_t`，底层 cause 只作为脱敏诊断字段保留。每个状态必须定义 retryability、severity 和用户反馈映射，禁止上层用数值范围或平台错误码猜测是否重试。

相对超时统一使用毫秒，长链路优先传入基于单调时钟的绝对 deadline；Service 只在边界换算 tick，并处理向上取整、溢出和剩余预算，禁止每层重新换算造成累计漂移。除明确声明 partial result 的流式读取外，API 失败时所有输出参数必须保持未修改或设置为接口规定的安全默认，不能留下半初始化 handle、长度或 capability。

公开 handle 是带类型和 generation 校验的不透明值，不是裸 driver pointer。`close/release/unregister` 可幂等调用；close 后旧 handle 的任何读写确定性返回 `DEVICE_STATUS_INVALID_STATE`。HAL restart、light-sleep suspend 策略变化和 deep-sleep 新 boot 必须定义 generation 失效边界。callback 注册返回 token；注销先禁止新投递，再在有界 timeout 内 drain 在途 callback，callback 不得在注销完成后访问用户上下文。

长操作不得阻塞 App Interaction Task。统一异步上下文：

```c
typedef struct {
    uint64_t operation_id;
    uint32_t generation;
    device_cancel_token_t cancel_token;
    int64_t monotonic_deadline_us;
} device_operation_context_t;
```

每个 Device/Platform API 必须在接口文档中声明：允许调用的任务、同步/异步、最大阻塞时间、是否可取消、参数所有权（borrow/copy/move）、完成事件类型和缓冲释放时点。六秒录音、会议录音、网络上传、TTS/MP3 播放和长时间 flush 由领域 worker/service 执行，通过 Device Event Queue 返回结果。

操作一致性规则：

- 新交互开始时生成新的 operation ID 和 generation，并原子失效旧 generation。
- cancel、success、error、timeout 竞争时只能提交一个终态。
- 晚到网络回复、音频完成、图片解码和显示完成只有 generation 仍有效时才能生效。
- 取消必须同时阻止本地副作用和后续远端结果重放；取消回复集合保留有界容量和明确淘汰规则。
- generation 比较不得依赖简单大小关系；计数器回绕后仍以完整 operation ID/有效槽位判断。

### 5.5 Input HAL 与事件队列

Input HAL 负责控制器初始化、扫描、中断、去抖和硬件异常过滤，并输出标准化输入事件。

```c
typedef enum {
    INPUT_CONTROL_ACTIVATE,
    INPUT_CONTROL_TOUCH_PANEL,
    INPUT_CONTROL_VOLUME_UP,
    INPUT_CONTROL_VOLUME_DOWN,
    INPUT_CONTROL_BOOT,
} input_control_t;

typedef enum {
    INPUT_GESTURE_DOWN,
    INPUT_GESTURE_TAP,
    INPUT_GESTURE_DOUBLE_TAP,
    INPUT_GESTURE_LONG_PRESS,
} input_gesture_t;

typedef struct {
    input_control_t control;
    input_gesture_t gesture;
    int64_t timestamp_us;
} input_event_t;
```

`input_event_t` 是 payload，不直接作为跨任务消息。所有队列事件统一包在版本化 `device_event_envelope_t` 中，至少携带：`struct_size/abi_version`、event type、source/producer、priority class、boot session、operation ID/generation、producer-local monotonic sequence、单调 timestamp、payload length 和 ownership tag。ISR 使用固定大小 inline payload 或预分配 pool；禁止 ISR 分配 heap、传递栈指针或引用稍后会覆写的驱动 buffer。consumer 对未知 event/version 按契约拒绝并计数，不得强转旧布局。

板型 profile 另提供不可变 `input_binding_table_t`，将物理 control 映射为硬件无关的逻辑角色，例如 `PRIMARY_CONTROL`、`CONFIG_CONTROL`、`PAGE_PREVIOUS_CONTROL`。Binding 只表达外壳提供了哪个控制件，不读取业务状态；“处理中双击表示取消”等上下文规则仍只存在于共享 Input Service。这样既避免 `main.c` 判断 touch/activate，也不把产品状态机塞进 capabilities。

板级实现边界：

- Bread Compact：实体激活键、音量键扫描和去抖。
- EchoEar-2ST：CST816 原生双击、重复 contact 过滤、触摸释放窗口和 BOOT 键扫描。
- 不在 Input HAL 中判断当前是否录音、是否显示回复、是否正在响铃。

共享 `input_service` 根据交互上下文将输入事件映射成唯一的业务意图：

```c
typedef enum {
    APP_INTENT_ACTIVATE,
    APP_INTENT_START_MEETING,
    APP_INTENT_STOP_MEETING,
    APP_INTENT_CANCEL_COMMAND,
    APP_INTENT_CONFIGURE,
    APP_INTENT_DISMISS_ALARM,
    APP_INTENT_PAGE_PREVIOUS,
    APP_INTENT_PAGE_NEXT,
    APP_INTENT_VOLUME_UP,
    APP_INTENT_VOLUME_DOWN,
} app_intent_t;
```

业务层只接收 `app_intent_t`。

输入事件不得直接调用 `on_app_intent()`，必须进入有界 Device Event Queue。队列契约：

- ISR 只使用 ISR-safe 非阻塞发布接口。
- `DOWN`、闹钟解除、取消和配置属于高优先级控制事件，不得被普通事件挤掉。
- 环境信息、音频 level 和动画 tick 等可合并事件只保留最新值。
- 每种事件定义满队列策略、丢弃计数和告警日志；不得无限阻塞输入扫描任务。
- HAL stop 时先关闭事件发布，再等待在途 callback 完成。
- App Interaction Task 是唯一调用共享交互状态机的任务。
- “高优先级不得丢”不能依赖无界阻塞：为 wake/cancel/alarm/mute 等关键控制事件预留独立容量或 mailbox，普通 telemetry 永远不能占用该 reservation；若关键 reservation 仍耗尽，进入明确 fail-safe、记录 sticky overflow 并触发监督恢复，而不是覆盖另一条关键控制事件。
- 公平性按 priority class + owner budget 约束，避免持续 touch bounce、audio level 或网络事件饿死取消/闹钟；同一 producer 的 sequence gap、重复和乱序必须可诊断。coalescing 只适用于显式声明 latest-wins 且没有 operation 终态语义的事件。
- 事件从 enqueue 到消费绑定有效 boot session/generation；队列 reset、service restart、light sleep resume 和 deep sleep 新 boot 分别定义保留/丢弃规则。任何丢弃、合并、过期或拒绝都更新按 event type/source 分类的计数器。

### 5.6 Audio HAL

Audio HAL 只负责样本进入和离开硬件：

```c
typedef struct {
    esp_err_t (*capture_start)(void);
    esp_err_t (*capture_read)(int16_t *mono, size_t capacity,
                              size_t *samples_read, uint16_t *level);
    esp_err_t (*capture_stop)(void);
    esp_err_t (*playback_start)(unsigned sample_rate, unsigned channels);
    esp_err_t (*playback_write)(const int16_t *pcm, size_t frames,
                                unsigned channels);
    esp_err_t (*playback_stop)(esp_err_t stream_status);
    esp_err_t (*set_output_volume)(unsigned percent);
} audio_hal_ops_t;
```

Audio HAL 的初始化和退出使用统一生命周期接口，不在 `audio_hal_ops_t` 内重复定义。`capture_start/stop` 和 `playback_start/stop` 必须成对、幂等可恢复；写入失败后 `playback_stop(error)` 仍负责排空/静音并释放硬件会话。

板级实现保留：

- Bread Compact 的直连 I2S 麦克风和功放接线、位宽转换。
- EchoEar-2ST 的 ES7210/ES8311 初始化、可靠麦克风声道、DC blocker、异常采样过滤和 codec 音量寄存器换算。

共享 `audio_service` 负责：

- 音频互斥和 capture/playback 所有权。
- 固定时长指令录音。
- WAV 头生成和验证。
- 通用连续采集流及 level/PCM 分发；会议时长、文件与恢复策略由 `meeting_service` 负责。
- PCM 波形向 UI 分发。
- 播放 begin/write/end 生命周期。
- 本地提示音和闹钟音序列。
- 唤醒词暂停/恢复协调。

Audio Service 定义唯一锁顺序和状态机：`IDLE → WAKE_LISTENING/CAPTURING/PLAYING → STOPPING → IDLE`。任何路径均不能同时持有 display mutex 和 audio ownership；超时必须释放已获得资源并上报诊断快照。

长时间 capture/playback 必须定义音频时钟域契约。HAL 报告实际 sample clock、DMA frame timestamp/sequence、累计 overrun/underrun 和不连续标记；Audio Service 不假设标称 16 kHz 永远准确。会议 WAV 时长、上传 metadata 和 UI 计时以实际已提交样本数为主，并结合单调时钟检测 ppm 漂移；多时钟域或全双工场景使用有界异步 resampler/jitter buffer，不通过丢/重复任意 PCM 隐瞒漂移。超过 profile ppm/连续 gap 预算时终止或标记录音完整性受损，不能上传并宣称无损成功。

### 5.7 Wake Word HAL/Service

MultiNet 模型生命周期、暂停确认、失败重启监督和前台录音互斥放在共享 `wake_word_service`。当不同麦克风前端需要不同预处理时，由 Audio HAL 提供标准 16 kHz 单声道 PCM，或通过有限的 preprocess hook 完成板级信号调理。

禁止每块板复制完整的唤醒词任务和恢复状态机。

### 5.8 Display HAL 与 Display Task

业务层不再逐个调用 `show_text`、`show_response` 并让板级实现维护第二套 UI 状态。共享 `app_ui` 更新完整模型，`display_service` 请求当前 Renderer 绘制 snapshot。

```c
typedef struct {
    esp_err_t (*render)(const app_ui_model_t *model,
                        const app_ui_dirty_t *dirty);
    esp_err_t (*flush)(TickType_t timeout);
    esp_err_t (*set_panel_enabled)(bool enabled);
    int (*cache_glyph)(uint32_t codepoint, const uint8_t bitmap[72]);
} display_hal_ops_t;
```

Display HAL 的初始化和退出使用统一生命周期接口。并发与所有权契约如下：

- 只有 Display Task 可以调用 Renderer、panel API 和 framebuffer 提交。
- `device_present_ui()` 深拷贝或接管 immutable snapshot 后入队，不在调用者任务中全屏绘制。
- 每个 snapshot 带单调递增 `revision`；Display Task 丢弃过期 revision。
- 队列对高频环境/波形更新进行合并，但不能丢弃 setup、alarm、recording、response 等前景切换。
- `render()` 不得保存 model 或 dirty 指针；`flush()` 等待 DMA/panel fence，成功后才允许回收提交缓冲。
- 明确最大队列深度、最大呈现延迟、DMA 超时和降级策略；Renderer 失败不得破坏最后一个完整前景画面。

`set_panel_enabled()` 只执行 panel 显隐/GRAM 保持等显示控制器动作，不操作整机 sleep、Wi-Fi、CPU、电源轨或唤醒源。背光/供电轨由 Power HAL 所有；Power Service 负责按 `Display flush → panel off → backlight/power rail off` 的顺序编排，恢复时反序执行。这样避免 Display HAL 与 Power HAL 同时写同一 GPIO。

`app_ui_model_t` 至少覆盖：

- 当前 surface 和前景所有权。
- 宠物状态、皮肤和动画开关。
- 标题、正文及错误状态。
- 回复正文、图片、当前页和页数。
- 录音类型、暂停、时长、音量和波形历史。
- 上传阶段、完成字节和总字节。
- 时间、日期、星期、地点、天气、Wi-Fi 和网关状态。
- 配网 SSID 和由模型拥有的二维码 payload/module matrix；不得保存 `esp_qrcode_handle_t`。
- 闹钟时间、标签、尝试次数和动画帧。
- ready prompt 与显示休眠状态。

各 Renderer 负责：

- Bread Compact：240×320 ST7789 矩形布局、实体键翻页提示和现有视觉。
- EchoEar-2ST：360×360 ST77916 圆屏安全区、现有宠物动画、曲线/圆形布局、回复、录音、闹钟、二维码及 QSPI 双缓冲/DMA 提交。

回复自动翻页或手动翻页策略由共享 Display Service 根据能力决定；像素排版和圆屏裁切属于 Renderer。

图片和二维码数据规则：

- 回复图片由 UI 模型拥有深拷贝或显式引用计数对象，不能依赖调用结束后即释放的临时像素。
- QR 生成库只存在于 Display Service/Renderer 边界；共享 `app_ui.h` 不 include `qrcode.h`。
- 动态字形缓存定义并发锁、容量、LRU 和失效规则，Renderer 只能在安全快照下读取。
- 字符串必须有明确长度上限和截断策略；不允许 Renderer 保存 cJSON 内存中的指针。

### 5.9 Power HAL

背光、显示休眠、唤醒和后续电池/低功耗能力通过 Power HAL 暴露。业务层发出“允许睡眠”“立即唤醒”等请求，不直接操作背光 GPIO。

Power HAL 至少区分 `set_backlight/panel_power`、PM policy/lock、`enter_light_sleep()` 和不返回的 `enter_deep_sleep()`；Wake HAL 仅 arm/cancel/query wake source。两者共享的 GPIO hold、电源域和 RTC 资源必须由 Resource Manager 登记唯一 owner。

Power HAL 必须与 Display/Audio/Wake Service 协调：DMA 未完成、会议录音、TTS 播放或唤醒模型运行时不能擅自关闭依赖时钟；DFS/APB lock 的获取和释放由资源所有者负责。

Clock/PM manifest 必须逐组件声明 CPU/APB/XTAL/RTC/I2S source 依赖、最低/最高频点、允许的 sleep state、PM lock 类型/owner 和频率变化处理。切频事务在新操作 admission 前建立 barrier，等待敏感 DMA/transaction 的安全边界；切换后由对应 port 重新验证或配置 I2C timeout/baud、LCD QSPI clock、UART/diagnostic baud、软件 timer 与 codec/I2S 时钟，再开放队列。使用独立音频时钟源时仍要验证与单调时钟的 ppm/correlation；使用 APB 派生时钟时 capture/playback 持正确 lock。任何频点组合未通过 HIL 时从 effective power capability 移除，不能靠“驱动通常会自动适配”发布。

电池 ADC、充电状态和温度等只作为可选 telemetry capability 扩展，不把某块板的分压比或 GPIO 写进共享业务。首版只定义版本化 sensor/power snapshot 扩展点；没有可靠原理图、校准参数和实机验证时保持关闭。低电量/棕断策略必须先请求 Storage/Persistence checkpoint 并给出有界超时，不能在文件更新中途直接深睡。

低电量保护必须是共享 `battery_policy_service`，而不是 ADC 驱动中的百分比 `if`：电压/电量估算需使用 profile 校准参数、ADC 校准结果、充放电状态、滞回、连续样本与异常值过滤，温度补偿只在传感器可信时启用。策略至少分为提醒、限制高功耗、低功耗保护和棕断紧急路径；低压时减少联网/背光/扬声器峰值，停止新录音或升级类写入，仅允许一次有界的关键 checkpoint，避免因反复写 flash 加速掉电损坏。

进入自动低电量 DEEP_SLEEP 前，Wake HAL 必须已经 arm 至少一个经验证的恢复源：充电器插入/power-good、独立电源键或有上限的 RTC 复检 timer。只有“手动 reset 或不确定的 charger attach”不算可验证唤醒契约；当前硬件若没有 charger-detect wake 电路，低电量策略应停在可恢复的保护状态或带周期复检的深睡，不能进入永久不可唤醒状态。充电/USB 供电出现后应重新采样并满足恢复滞回，再逐步开放联网、显示和音频负载。

#### 5.9.1 电源状态模型与职责边界

“屏幕熄灭”与“系统休眠”必须分离。当前 EchoEar 的 idle timeout 只关闭 panel/backlight，CPU、Wi-Fi、音频及任务仍运行；迁移后不得继续把它命名为设备 sleep。统一 Power Service 管理以下状态：

```text
ACTIVE
  → DISPLAY_OFF（只熄屏，业务与联网保持运行）
  → MODEM_SLEEP（联网保持，允许 Wi-Fi/CPU 自动节能）
  → LIGHT_SLEEP（RAM/任务上下文保留，部分外设暂停）
  → DEEP_SLEEP（RTC 域保留，唤醒后按一次新 boot 恢复）
```

状态选择由共享 `power_service` 根据 schedule、idle、业务 lease、板级能力和唤醒能力决定；Power HAL 只执行板级时钟、电源轨、背光和 ESP sleep 操作，不判断“夜间”“会议中”或“是否应该睡”。三硬件必须具备相同的指定时间/idle 休眠与可恢复唤醒业务；具体允许的最深睡眠状态可因 wake 电路不同而不同。若某 profile 未验证 light/deep sleep，Power Service 应选择已验证的较浅状态并保持相同用户结果，不能取消休眠计划或漏掉闹钟；对具体未实现的物理 depth，内部 Power HAL 可返回平台“不支持”并由 Power Service 安全改选，不能把它解释成公共 `sleep.schedule` 功能不支持。`CONFIG_MACLAW_POWER_SAVE`、`CONFIG_PM_ENABLE`、tickless idle 和 DFS 只是编译前提，不是 capability；必须在启动时确认 `esp_pm_configure` 与相关锁初始化成功后才能加入 effective capabilities。现有 Kconfig battery ADC 文案同样不等于实现存在，直到 ADC port、校准、低电量状态机和实机门禁完成前保持关闭。

Always-on wake word 是显式功耗约束：当前 MultiNet 任务持续读取 I2S/执行推理，持有“最多进入 MODEM_SLEEP/DISPLAY_OFF”的 power lease。进入 LIGHT/DEEP_SLEEP 前必须停止并 join 唤醒词任务、释放模型/I2S/PM lock；恢复后由 `wake_word_service` 在 Audio/Clock 已就绪时重新加载。若用户选择“始终语音唤醒”，Power Service 只能使用与实机验证相容的较浅状态；若 schedule/低电量选择深睡，UI 和 effective capability 必须明确休眠期间语音唤醒不可用。产品若要求 deep sleep 下语音唤醒，需要 ULP 可行性证明或外部低功耗语音芯片/唤醒电路，不能把当前 ESP-SR 任务标为支持。

业务和 Service 通过有引用计数、owner、原因、deadline、最大允许 power state 的 `power_lease` 约束状态，例如：会议录音/上传、指令录音、音频播放、闹钟响铃、配网 portal、Storage commit、NVS migration、版本检查网络事务、Display DMA 和量产测试。lease 不是简单“禁止睡眠”布尔值：网络轮询可能允许 DISPLAY_OFF/MODEM_SLEEP 但禁止 LIGHT/DEEP_SLEEP，会议录音只允许 ACTIVE。lease 必须超时可诊断且在 owner 停止时释放；不允许散落全局 bool 决定休眠。

#### 5.9.2 指定时间休眠与唤醒调度

新增共享 `sleep_schedule_service`，支持一次性和每日/工作日时间窗，例如 `23:00–07:00`。配置至少包含：enabled、timezone、local start/end、weekday mask、目标 sleep depth、允许的 wake sources、用户手动唤醒后的 override 策略和 schema version。时间窗属于领域配置，不进入 board profile；profile 只声明支持的 sleep depth 与 wake source。

调度规则：

- 只有墙上时间已可信（SNTP 同步或有效 RTC 恢复）才执行日历时间窗；时间不可信时保持 ACTIVE/DISPLAY_OFF，并等待同步，不能按错误时间深睡。
- 超时、grace period 和进入休眠的准备 deadline 使用单调时钟；时区、DST、跨午夜窗口、时间回拨/前跳必须由 Clock Service 统一计算下一边界。
- 深睡前设置 RTC timer 到下一唤醒边界，并保存 schedule revision、计划 wake epoch、进入原因和必要恢复标记；唤醒后检查实际 wake cause 和时间漂移。
- 若进入窗口时存在高优先级 power lease，延迟至 lease 释放或最大 grace deadline；超过上限按策略取消休眠或终止可取消工作，绝不能截断会议录音、文件提交或闹钟响铃。
- 到期闹钟早于计划唤醒时间时，Alarm Service 必须将下一闹钟投影为更早的 timer wake deadline，使设备先醒并响铃。新增/删除闹钟后重新计算 wake deadline。
- 用户在休眠窗口内通过按键/触屏唤醒时，默认进入临时 override（例如保持唤醒 15 分钟或直到本次交互结束），避免设备刚亮屏又立即睡回去；override 时长可配置并持久化策略，不持久化临时倒计时。
- 配置更新、时区变化和 SNTP 大幅校时后必须递增 schedule revision，取消旧 timer 计划并重新编排，迟到 timer 不得触发旧计划。
- 多条规则发生重叠时采用确定性优先级：到期闹钟/安全唤醒 deadline → 用户临时 override → 一次性 schedule → 周期 schedule → idle policy；相同优先级以 revision 和最近显式用户操作裁决。必须规定窗口起止边界为半开区间 `[start, end)`，避免整点重复进入/退出。
- schedule 配置写入先做语法、时区、目标 depth 和 wake matrix 预校验；不支持的目标可按显式策略拒绝或降级到较浅状态，不能静默改成无法唤醒的组合。
- 若下一边界距离小于该 profile 的 `min_sleep_duration + prepare + resume guard`，保持较浅状态而不进入 deep sleep，避免频繁睡醒反而增耗并磨损 flash。
- 用户显式禁用 schedule 后必须清除持久化计划和已 arm 的 RTC timer；恢复出厂、换时区、schedule schema 降级也需定义 fail-open（保持唤醒）行为。

所有需要 RTC timer 唤醒的消费者（Sleep Schedule、Alarm、低电量复检、未来维护任务）不得直接覆盖 ESP timer wake 配置。新增 `wake_deadline_service` 作为唯一仲裁者，接收带 owner、reason、epoch、revision、优先级和容差的 deadline，始终 arm 最早有效项；消费者更新/删除 deadline 后原子重算。启动时根据 wake cause 和持久化 deadline 集合判定哪些事件到期，过期一次性 deadline 只消费一次，周期规则再生成下一项。

建议公开的是意图接口而非 ESP-IDF 细节：

```c
device_status_t device_power_request_state(device_power_state_t target,
                                           const device_operation_context_t *op);
device_status_t power_lease_acquire(const power_lease_desc_t *desc,
                                    power_lease_handle_t *handle);
device_status_t power_lease_release(power_lease_handle_t handle);
device_status_t sleep_schedule_set(const sleep_schedule_t *schedule);
device_status_t sleep_schedule_get(sleep_schedule_t *schedule);
device_status_t sleep_schedule_disable(void);
```

Sleep Schedule 属于领域能力，可在 Capability Service 确认 Clock、Persistence、Power 和至少一个对应 wake 组合有效后，动态注册 `sleep_schedule_set`、`sleep_schedule_get`、`sleep_schedule_disable`；可选 `sleep_now` 必须单独评审。Gateway 的通用 Tool Dispatcher 只做 schema、认证会话、idempotency、deadline 和结果回传，再路由到领域 handler，禁止像当前仅调用 Alarm Manager 那样把所有设备工具硬编码到一个领域实现，也禁止把这些工具下沉为 HAL API。

远程写工具必须先完成参数、时区、目标 depth、wake matrix、最早闹钟和恢复路径预校验；相同 `idempotencyKey` 返回相同领域结果，重启后仍在 replay 保留窗口内生效。`sleep_now` 不能绕过 power lease、`PREPARE → COMMIT` 或物理恢复源门禁，且默认只创建可取消请求；执行结果在真正 COMMIT 前回报“accepted/preparing”，不得提前声称已睡眠。工具注册随 effective capability 收缩，旧会话迟到调用返回明确 `not_supported/temporarily_unavailable`，不能执行隐藏路径。

#### 5.9.3 硬件按键与触屏唤醒抽象

新增 `wake_hal_ops_t`，把“哪些物理线能从哪种 sleep depth 唤醒”作为板级事实：

```c
typedef uint32_t wake_source_mask_t;

typedef struct {
    esp_err_t (*prepare)(device_power_state_t depth,
                         wake_source_mask_t requested,
                         wake_source_mask_t *armed);
    esp_err_t (*get_cause)(device_wake_cause_t *cause);
    esp_err_t (*resume)(const device_wake_cause_t *cause);
    void (*cancel)(void);
} wake_hal_ops_t;
```

Wake HAL 只负责配置/解析 wake source；真正的 `enter_light_sleep()` / `enter_deep_sleep()` 归 Power HAL，避免两个 HAL 都拥有进入状态的控制权。Power HAL 的 DEEP_SLEEP 成功路径按定义不返回；返回即表示进入失败并由 Power Service 执行回滚。LIGHT_SLEEP 可以返回并调用 Wake HAL `get_cause()/resume()`。接口实现必须明确 IRAM/flash-cache 限制和回调上下文，不能在 COMMIT 后依赖 heap 分配、日志 flush 或普通 Service callback。

- Bread Compact：优先验证 GPIO0 激活键作为 RTC GPIO/ext0/ext1 或 GPIO light-sleep wake source；音量键是否参与唤醒由 profile 和原理图验证决定。
- EchoEar-2ST：GPIO0/BOOT 可作为候选硬件唤醒源；CST816 的 `TOUCH_IRQ` 为 GPIO42。按 ESP32-S3 引脚能力，GPIO42 不是 deep-sleep RTC IO，当前 PCB 上触屏唤醒应定位为 `DISPLAY_OFF/LIGHT_SLEEP` 能力，不能声明触屏直接唤醒 DEEP_SLEEP。DEEP_SLEEP 使用 GPIO0/BOOT 或 RTC timer；若产品必须支持触屏 deep-sleep wake，需要把 touch IRQ 改接 RTC-capable GPIO，或增加外部 wake/power-latch 电路并形成新 board revision。
- Fangtang-4G：GPIO0 单激活键是用户硬件唤醒候选；RTC timer 是闹钟、Sleep Schedule 和低电量复检的必选候选。ML307 电源使能/guard、UART、LCD offset、充电状态 GPIO 和 battery ADC 在 sleep 前后的电平、保持、重新探测与恢复顺序必须由 profile 实测。存在有效闹钟、蜂窝事务或无法验证的充电唤醒路径时，Power Service 限制睡眠深度，不得把“4G 模块仍上电”误报为 ESP32 已进入完整低功耗状态。
- 触屏唤醒不是普通 touch gesture。Wake HAL 只报告 `WAKE_CAUSE_TOUCH`，恢复 I2C/touch controller 并 drain 唤醒 contact；Input Service 在 guard window 内默认消费这个 contact，仅点亮/唤醒设备，避免一次触摸同时启动录音。是否允许“唤醒即执行”必须作为显式共享策略配置。
- deep sleep 唤醒等价于重启：Boot Coordinator 必须在初始化 GPIO/I2C/LCD 前读取并缓存 wake cause，再按正常生命周期恢复。light sleep 则走对称 suspend/resume，不重复创建任务和总线。
- 配置唤醒 GPIO 前校验 RTC/GPIO 能力、有效电平、上下拉、外设供电域和冲突；进入睡眠前清除已处于有效电平的 stale interrupt，避免立即唤醒循环。
- 每个 DEEP_SLEEP 策略至少保留一个已验证的可恢复唤醒源；正常 schedule 可以是 RTC timer 或安全物理源，低电量策略还应优先 charger/power-good 或周期复检。若请求的 timer/button/touch/charger 全部无法 arm，Power Service 拒绝进入不可恢复的深睡，而不是留下只能断电恢复的设备。
- GPIO0 同时是 ESP32-S3 启动绑带脚。进入睡眠前必须完成释放电平/stale contact 检查；用户持续按住 GPIO0 触发 deep-sleep wake 后若发生 reset，可能进入下载启动模式。产品验收必须覆盖短按、长按、一直按住、抖动、外部上下拉和 USB 下载模式；风险不可接受时 deep sleep 仅保留 RTC timer，或在新板修订增加非 strapping 的 RTC wake 引脚/外部电路。

首版能力矩阵必须以实机和 ESP-IDF target 校验为准，预期如下：

| Profile | DISPLAY_OFF 唤醒 | LIGHT_SLEEP 唤醒 | DEEP_SLEEP 唤醒 |
|---|---|---|---|
| Bread Compact | GPIO0 激活键 | GPIO0；音量键待验证 | GPIO0 RTC wake + RTC timer |
| EchoEar-2ST 当前 PCB | CST816 touch、GPIO0/BOOT | GPIO42 touch IRQ、GPIO0/BOOT | GPIO0 RTC wake + RTC timer；不支持 GPIO42 touch |
| Fangtang-4G v1 | GPIO0 单激活键 | GPIO0 待实机验证；ML307/充电源组合需单独验证 | GPIO0 RTC wake + RTC timer 待实机验证；charger attach 是否可唤醒不得预设 |
| EchoEar 后续板修订 | 同上 | 同上 | 仅当 touch IRQ 改接 RTC-capable GPIO/外部电路后声明 touch |

构建 manifest 记录的不是单个 `supports_touch_wakeup` 布尔值，而是 `sleep_depth × wake_source` 矩阵及 active level、pull、hold、供电域和最小脉宽。Capability Service 只公布已通过实机测试的组合。

#### 5.9.4 休眠准备与恢复事务

进入 LIGHT/DEEP_SLEEP 使用两阶段事务：`PREPARE → COMMIT`。PREPARE 先把 App Interaction Task 切到 `QUIESCING`，拒绝新长操作但仍接受取消/闹钟/配置撤销；获取 sleep operation generation，等待 Display DMA，暂停 Wake Word，停止/断开网络，checkpoint Storage/Persistence，配置 wake sources，并向 UI 显示短暂休眠提示。完成最终 lease/闹钟/schedule revision 二次校验后才能 COMMIT；任一步失败均按逆序恢复并回到 ACTIVE。COMMIT 后禁止再写 flash、打印依赖 flash 的日志或启动新任务，最后调用 HAL `enter()`。

Gateway quiesce 必须先取消在途长轮询/HTTP（不能等待完整 30 秒 server timeout），阻止新上传并等已有请求到有界安全点。当前协议没有显式 sleep/offline presence，Hub 以认证请求的 last-seen 超时判离线；因此“通知 Hub”至多是未来可选的 best-effort 协议扩展，失败不得阻止安全休眠。对尚未 ACK 的下行消息保持 at-least-once 语义：不在 PREPARE 中伪造 ACK，不持久化旧 RAM cursor 作为已消费证明，唤醒后重新握手并从可恢复 cursor/消息 ID 继续去重。Hub 当前队列是有上限的内存队列，重启或超限会丢失非 durable 消息，所以交付深睡前必须明确消息类别：配置/宠物等 latest-wins 状态在 handshake 重建，工具调用/关键命令需要服务端 durable queue、TTL、boot/session 归属和 ACK 后删除；在 Hub 未补齐前不得承诺深睡期间可靠接收。

休眠操作需要单独的 serialization mutex/state machine，禁止 schedule、idle、低电量和用户命令同时发起多个 prepare。新的取消、更早闹钟或关键 lease 在 COMMIT 前到达时必须中止本次休眠；COMMIT 后到达的事件只能通过已配置 wake source 或下次启动恢复，不能假装已经处理。

恢复顺序固定为：读取 wake cause → 校验 schedule revision/manifest → 恢复 Resource Manager/必要电源轨 → 恢复 Input/Touch → Display → Audio/Wake → Connectivity → 开放业务事件。触摸/按键 wake cause 转换为单独 `DEVICE_EVENT_WAKE`，不能伪造成正常 TAP/DOWN；timer wake 根据计划原因决定保持静默、显示待机或触发到期闹钟。

DEEP_SLEEP 唤醒必须生成新的 `bootSessionId`，并使重启前的 operation generation、取消窗口、在途 tool result 和前台 reply correlation 全部失效；旧 session 的迟到结果不得控制新 UI/音频。Hub handshake 应以 `(clientId, bootSessionId)` 区分启动事务，以 message/tool ID 和 idempotency key 做跨重启去重；当前客户端的运行时 capability refresh 使用普通 handshake 但省略 `bootSessionId`，Hub 必须把它视为当前连接的能力刷新且不能排队启动欢迎。新 session 的未 ACK 旧消息按其显式跨启动策略处理。LIGHT_SLEEP 保持当前 boot session，但所有被暂停的 request/task 必须在 resume barrier 完成后才能重新发布 readiness。

RTC timer 存在慢时钟误差。profile budget 必须声明最大计划休眠时长、实测漂移和启动恢复耗时；闹钟唤醒使用提前量 `boot_lead + drift_guard`，联网后立即校时，再等待准确触发时刻。不得承诺未经测量的分钟级/秒级唤醒精度；长时间离线时 UI/日志应能报告时间可信度下降。

每次睡眠记录计划/实际进入时间、目标 depth、armed wake mask、阻塞 lease、prepare 耗时、wake cause、实际休眠时长和 reset reason。诊断记录使用有界 ring/聚合计数，不能每次 DISPLAY_OFF/短睡都写 NVS 造成 flash 磨损；只在重要状态变化或异常时批量 checkpoint。禁止记录用户敏感内容。

### 5.10 持久化边界

新增共享 `persistence_service` 并统一使用同一个 NVS mutex：

- Wi-Fi、配对 token、会议恢复、天气、网关连接状态和闹钟属于共享业务持久化。
- 用户音量、亮度等设置属于共享配置持久化。
- 麦克风校准、panel offset 等纯硬件参数可由板级 HAL 持久化，但必须使用独立 namespace。
- Display HAL 禁止直接读写产品 NVS；现有 Bread `gateway_ready` 持久化迁出 Renderer。
- NVS 数据声明 schema version、默认值、迁移和损坏恢复策略。
- 每个 namespace 声明 writer owner；统一 NVS mutex 只解决并发，不替代事务边界。跨 namespace 的逻辑事务使用版本化 journal/tombstone 和可重复恢复步骤，不假设多个 `nvs_commit()` 原子合并。
- 配置数据区分“用户意图”和“运行时派生状态”：schedule 规则、时区策略可以持久化；临时 override 剩余时间、队列深度、当前 UI revision 等只保留 RAM/RTC 恢复信息，避免重启后复活过期状态。
- NVS 初始化遇到 `NO_FREE_PAGES`、`NEW_VERSION_FOUND`、页损坏或 schema 不兼容时禁止整区自动 `nvs_flash_erase()`。Boot Coordinator 先依据受保护的 partition/layout manifest 区分 factory blank 与已有数据；已有数据进入只读恢复/SAFE_MODE，保留原分区用于工装导出，逐 namespace 执行版本化、可重入迁移。只有用户明确恢复出厂或认证工装确认备份/擦除范围后才允许擦除，并记录审计原因。
- 恢复流程必须把 Wi-Fi、配对 token、闹钟、schedule、会议 cursor、用户设置和板级校准分为独立数据等级与恢复策略；单个 namespace 损坏不能默认牵连整分区。未知新 schema 采用 fail-closed 写入、fail-open 设备安全状态，旧固件不得覆盖新版数据。

持久化迁移必须建模为可恢复事务，而不是启动时就地改写：`DISCOVERED → STAGED → VALIDATED → COMMITTED → CLEANUP_PENDING`。优先使用 expand/contract：先让新 reader/writer 在明确版本窗口内同时理解旧/新格式，写入新 generation 并校验，再原子切换 active pointer；经过回滚窗口后才清理旧格式。migration record 记录源/目标 schema、对象 generation、tombstone、摘要、进度、空间预留、写放大和 deadline，任一点掉电均可继续或回到最后确认状态。NVS、Storage 大对象、credential 和硬件校准分别迁移，不能用一个全局 schema version 掩盖不同不可逆点。

任何不可逆迁移必须在提交前满足至少一项：已有可验证备份/旧副本、旧固件已具备兼容 reader、或发布策略明确关闭固件回滚并经数据影响批准。升级工具必须检查 firmware↔reader/writer version window，禁止“新固件先写出旧固件无法读取的数据，随后仍宣称可随意有线回滚”。迁移前验证 free space 与低电量/休眠 lease，迁移失败保持旧 generation 可读；凭据迁移不得复制明文 secret，校准迁移必须保留 board/hw/channel provenance。

### 5.10.1 Storage Service 与断电一致性

会议音频、宠物资源等大文件不使用 NVS blob。领域代码通过 `storage_api` 访问逻辑 volume/object，Storage Service 内部再调用 `storage_port`。首版 ESP-IDF port 可以继续使用当前 `/storage` SPIFFS；后续硬件可替换为 LittleFS、SD 卡或无持久存储实现。无存储时会议能力关闭，但语音指令等不依赖持久文件的功能仍可运行。

Storage 契约至少提供：挂载/只读恢复、容量与保留空间查询、创建临时对象、顺序 append、读/seek、`flush + fsync`、原子 commit/rename、删除和枚举恢复对象。实现必须规定最大文件数、并发访问、磁盘满、介质拔出、坏块及只读降级语义；业务不得拼接 `/storage/meeting.wav` 或直接持有 `FILE *`。

会议恢复采用可验证的提交顺序：先写临时音频并落盘，再原子更新带 schema、文件长度、摘要/校验、chunk cursor、remote recording ID 和 phase 的恢复记录；服务端确认全部交付后，先持久化 delivered tombstone，再删除本地音频，最后清理 tombstone。启动时以文件长度/摘要与恢复记录共同校验，损坏或不一致时进入保留/诊断状态，不静默删除。WAV placeholder header、最终 header、每个上传 chunk 边界和恢复 cursor 都必须覆盖任意指令点断电/brownout 测试。

会议录音属于敏感用户数据。`meeting_service` 必须定义可见且与真实 capture 同步的 consent/indicator、默认 retention/TTL、容量淘汰顺序、用户列举/删除、上传确认后的删除语义和未上传文件的恢复策略。诊断接口只暴露长度、摘要、状态和故障码，不读取或导出音频内容；工装导出必须经过独立认证、用户/产品策略授权并留下审计记录。若介质支持按对象加密，数据密钥与对象 metadata 分离；未完成静态加密前必须把风险列为发布限制。

### 5.10.2 Connectivity、Gateway 与 Identity 边界

- `connectivity_service` 封装 Wi-Fi STA/AP、EAP、重连、配网 portal 生命周期和网络 readiness；板级驱动不得调用 HTTP、操作 Hub token 或决定业务状态。
- `gateway_service` 负责 HTTPS、配对、握手、轮询、会议上传和协议重试；它依赖 Connectivity/Identity/Persistence，不属于 HAL。
- `update_service` 只负责 release metadata 查询、版本/兼容性比较、提醒策略和已读/稍后状态；它调用 Gateway/Connectivity、Persistence、Clock 与 `update_catalog_api`，不属于 HAL，也不识别 Wi-Fi/ML307 具体驱动。该 Service 不拥有 firmware bytes、partition、Flash writer 或重启权限。
- 更新信息链路固定为 `GitHub Release → Hub release catalog → device metadata`：Hub 根据允许的 `RapidAI/MaClaw` Release 读取并验证签名 `.clawfw` manifest，只向已配对设备返回适用 profile 的版本、单调 `releaseSequence`、channel、发布时间、重要级别、最低 ClawMate Maker 版本、package/manifest digest、release notes 摘要/digest、撤回状态、缓存期限及是否必须连接电脑升级。设备不直连 GitHub、不接收固件 URL、不下载固件。
- Hub 版本检查 endpoint 复用 device gateway bearer，并绑定 tenant/client/device/profile/hw/layout/credential generation；请求携带由当前固件身份生成的 `current_release_sequence/current_version/channel` 和 Update Service 状态 revision。服务端与设备均按 `releaseSequence` 主比较、版本字符串仅展示：目标 sequence 更高才是更新；相同 sequence 不同 digest 是发布冲突；设备 sequence 更高视为开发版/预发布版而不提示“升级”；撤回、降级、跨 channel、unsupported flasher 使用稳定状态码，不能靠 SemVer 猜测。返回的 package ID/digest 仅供提醒和后续桌面刷机工具选择，不构成设备写入授权。GitHub 不可达时只可返回仍在 `maxAge/checkAfter` 内的已验证 metadata；设备时间不可信时以 Hub 响应 age/单调运行时处理缓存，不自行接受过期 metadata。
- 桌面刷机工具由用户主动运行并自行从 GitHub Release 下载完整 `.clawfw`；下载完成后复用 ClawMate Maker 既有校验：manifest 原始字节签名、package/asset size 与 SHA-256、profile/manifest/tool 三方 allow-list、product/board/hw/chip/layout/compat/flash/security baseline，再通过 USB/串口读取设备 identity 和真实分区表。只有制造身份可形成 `confirmed`；无制造身份最多为 `probable` 并要求用户确认实物；`ambiguous/conflict` 必须拒绝，人工确认不能覆盖能力、布局或安全冲突。Hub 不是固件数据中继，设备端也不存在隐藏安装 API。
- `identity_service` 从芯片唯一标识或受保护量产身份派生稳定 device ID；克隆普通 NVS 不得克隆设备身份。boot session ID 使用 ESP 硬件随机源；operation ID 至少组合 boot session 与单调计数/随机 nonce，避免重启后复用。
- 普通可改 NVS 中的诊断副本不作为安全身份根；量产签名 manifest、设备凭据、Secure Boot/Flash Encryption 等根信任分别管理，不能混为 board profile。
- 网络重连和 Hub 重试使用有上限的指数退避与抖动，不允许 Wi-Fi event callback 紧循环重连或由 Renderer/board profile 发起网络动作。
- Gateway Service 必须提供 `quiesce/cancel_inflight/resume` 生命周期和有界 join，网络 poll task 不得永久 `while(true)` 且无 stop token；设备进入 LIGHT/DEEP_SLEEP 时 last-seen 自然超时，不能把短暂离线误判为解除配对或擦除凭据。
- 下行队列契约必须写明 durability、容量、TTL、排序、ACK 和重放边界。当前 Hub `state.messages` 是有界内存队列，ACK 后删除、超限丢弃最旧项，Hub 重启也无法恢复；因此在协议/存储增强前，深睡期间只对可从 handshake 重建的 latest-wins 状态提供最终一致性，不承诺每条 transient reply/tool call 必达。

#### 5.10.3 Provisioning、重新配置与恢复出厂

配网是共享 `provisioning_service` 的安全事务，不属于 Input/Display HAL。Input Service 只产生 `APP_INTENT_CONFIGURE`；Provisioning Service 获取 radio/display/persistence power lease，编排 Gateway quiesce、AP/STA、DNS/HTTP server、配置校验、原子提交、连通性验证、成功/失败回滚和退出。Renderer 只显示由模型拥有的 SSID、一次性验证码/二维码和状态，不持有密码或 HTTP request buffer。

当前开放 SoftAP + 明文 HTTP 表单会使附近设备观察或篡改 Wi-Fi/EAP 凭据。发布方案必须至少使用每次会话随机的高熵 WPA2/WPA3 AP 密码，通过本机屏幕/标签/可信带外渠道展示，并生成不可预测的 provisioning session ID、CSRF nonce 和短 TTL；每次启动/失败重试轮换，限制连接数、请求体、尝试率和会话时长。若硬件/产品必须保留开放 captive portal，只允许输入不敏感引导信息，真正凭据必须通过认证加密通道传输；否则该模式只能用于受控开发 build，不得作为量产默认。

配网 HTTP/DNS/AP 必须有完整 stop/join/unregister：成功、取消、超时、低电量、模式切换或错误后关闭监听 socket、DNS、AP netif 和 handler，清零 RAM 中的表单/密码/配对码，恢复 Wake Word/Gateway 和原 Wi-Fi。AP+STA 恢复配对时不能把已有 gateway token、STA 流量或管理接口暴露给 AP 客户端；接口绑定、路由/NAT、防火墙与 origin 检查必须显式验证。设备屏幕应显示配网进行中和剩余 TTL，并允许物理取消；会话超时后回到原工作配置而不是卡在永久 AP。

配置更新采用 `stage → validate → commit → activate → confirm`：先在隔离 staging 中解析长度、UTF-8、SSID/EAP、Hub origin 与证书策略；有条件时先验证 Wi-Fi/Hub，再原子切换。重配置失败或掉电恢复旧已确认配置和 token；只有新 Hub/配对事务确认成功后才撤销旧 token。pair code 一次性、短 TTL、有限尝试且使用后立即清除，不能在日志、UI history 或长期 NVS 中保留。

“重新配置”和“恢复出厂”必须是两个不同意图。长按进入可取消配网，不擦除现有配置；恢复出厂要求难以误触的物理确认流程和明确数据分级，先停止会议/上传、保全或提示未上传录音，再按顺序撤销远端 token（best effort）、擦除本地 Wi-Fi/EAP/token/schedule/alarm/user data，保留或重新生成设备根身份按产品策略执行。掉电后通过 tombstone 幂等继续，不能得到一半旧凭据、一半新配置的状态。

显示不是 Provisioning Service 的必选依赖。profile 必须声明一种或多种可信引导通道：屏幕二维码/一次性口令、每设备物理标签 secret、未来的安全 BLE 配网，或认证 USB/量产工装；禁止全产品共用固定默认密码。标签 secret 需定义首次绑定、设备转移、泄漏后的轮换/作废和服务端绑定策略。无屏、无触摸或无实体输入设备仍必须具备进入、取消、超时退出和恢复出厂的可操作路径，且这些差异只通过 Input/Feedback capability 与 Provisioning adapter 表达。

解绑/设备转移先把本地 token 隔离为不可用于新连接，再尝试远端 revoke。离线撤销失败时持久化带重试上限和过期策略的 tombstone，服务端同时支持 token TTL/撤销列表。凭据携带单调递增的 `credential_generation` 或 device-binding revision；Hub 拒绝旧 generation，避免旧 NVS 镜像、迟到确认或恢复备份使已解绑 token 复活。

普通 flash delete/format 不等于取证意义上的安全擦除。只有启用并验证 Flash/NVS/对象加密且销毁对应数据密钥时，才可宣称 crypto erase；未加密设备只能声明逻辑删除/尽力覆盖及其残留风险。恢复出厂 UI、工装报告和产品文案必须使用与实际 security manifest 一致的措辞。

#### 5.10.4 非可信输入、解析预算与资源配额

Gateway handshake/消息/tool arguments、配网 form、URL、JSON、base64、图片、字形、WAV/MP3 和媒体下载全部视为非可信输入。Connectivity/Gateway/Provisioning/Media Service 为每种输入定义版本化 parser contract：最大 body、JSON depth/node/string、字段数、重复 key/未知 enum/version 策略、base64 解码后上限，以及 `width × height × bytes_per_pixel`、采样数、时长和 buffer 大小的 checked arithmetic。所有长度运算在分配前检测加法/乘法溢出，不能先分配再判断。

下载契约同时限制 redirect 数、scheme/origin 变化、DNS/IP 重绑定、`Content-Length` 与 chunked 冲突、压缩比和总解码字节；携带生产 token 的请求默认禁止跨 origin redirect。图片和音频分别限制尺寸、像素、采样率、声道、PCM 位宽、帧大小、时长、解码 CPU 时间和连续无让出区间。MP3/WAV/header/base64/JSON 解析失败不得形成部分业务副作用、成功 ACK、无限重试或占满高优先级队列。

解析与 schema 校验必须在写 NVS、创建文件、启动录音/播放、改变 schedule 或回传成功前完成。资源配额按单消息、单会话、设备总量和时间窗口分层；超限返回稳定状态并计入有界诊断，不回显原始 secret/payload。对 JSON/form/URL/base64/WAV/MP3/image/tool schema 建立 fuzz corpus、OOM/超时/failure injection 和回归测试；每次修复的崩溃样本进入长期 corpus。

### 5.11 能力投影、运行时健康与网关协议

内部能力表必须成为握手 `clientCapabilities` 的唯一硬件事实来源，禁止脱离实现与自检结果硬编码声明。与此同时，音频播放、图片、会议、音量等 Bread 公共功能是三款正式 profile 的发布必选项：若任一 profile 无实现或自检长期失败，应阻断该 profile/整套三硬件发布，而不是从握手中隐藏后继续宣称功能对齐。

```text
board_capabilities（静态硬件）
        + firmware_features（编译入 MP3/ESP-SR/CJK 等）
        + device_health（当前硬件/存储/网络资源状态）
        ↓
device_capabilities（本机真实能力）
        ∩ Hub capabilitiesAccepted
        ↓
protocol clientCapabilities（协议格式、大小、功能）
```

静态 `board_capabilities_t` 不可变；`device_health_t` 和 `effective_capabilities_t` 随初始化、存储挂载、故障和恢复变化。共享 Capability Service 负责生成握手 JSON，并处理 `capabilitiesAccepted`：Hub 未接受或已实现能力运行时暂不可用时，当前会话按契约暂停、恢复或降级反馈；这不改变三款正式 profile 静态公共业务集合必须一致的门禁。当前协议通过普通 handshake 完成 capability refresh：冷启动 handshake 携带新的 `bootSessionId`，运行时 refresh 明确省略该字段，Hub 因而不会创建新的启动欢迎事务。因此运行时能力变化先影响本地调度，再按限流策略执行不带 boot ID 的 refresh handshake。不得假设存在尚未定义的“会话内即时 capability update”消息，也不得把 capability refresh 误判为新开机并重放启动欢迎。无显示、无扬声器、无音量、无唤醒词等缺失组合仅由 Fake/Reference 或未来非正式 profile 测试；三款正式硬件只测试临时故障、恢复和替代反馈，不把这些缺失固化为产品能力差异。

为避免命名混淆，固定三层快照：`effective_device_capabilities`（本机真实可用）、`hub_accepted_capabilities`（Hub 对协议格式/版本的接受）和 `negotiated_session_capabilities`（两者求交并叠加当前认证授权）。Hub 回显不能写回静态 profile/health；本地安全策略可随时进一步收缩协商结果。每次冷启动、重连、重新认证、Hub origin 切换或 protocol/tool-schema 变化递增 `negotiation_epoch`，消息与工具调用同时携带 epoch；旧 epoch 到达时明确拒绝或按只读兼容策略处理。

Handshake 显式协商 protocol major/minor、最小兼容版本、capability descriptor version、tool schema version、media limits 和未知字段策略。major 不兼容 fail closed 并进入可恢复提示；minor/未知 capability 只能按约定忽略或降级，不能默认开启。设备先验证响应的 client/boot session/negotiation correlation，再应用 accepted/tool 配置；部分 JSON、重复 key、超限或字段自相矛盾时保持上一个已确认 session snapshot，不形成半更新状态。

Capability Service 只发布不可变、带 `revision` 的完整快照；profile descriptor 启动后只读，health 更新由单写者事件流串行归并。一个 operation 在开始时绑定一次 effective capability revision，不允许运行中分别读取多个可变指针造成 TOCTOU。能力收缩后新操作立即拒绝；已开始操作按每项能力预先声明的 policy 完成、降级或取消，并在结果中携带起始/终止 revision。快照通过深拷贝或 refcount handle 获取，并按 API 契约成对 release；release 清空调用方对象且可幂等调用，不返回生命周期不明的内部裸指针。

领域工具定义同样由 effective capabilities 投影：Alarm、Sleep Schedule、Update 等 Service 各自提供版本化 tool descriptors 与 handler，Gateway 只聚合可用集合并校验 Hub 调用。更新工具只允许 `update.check`、`update.status`、`update.remind_later`、`update.dismiss_version`；前三者只读或只改提醒偏好，`dismiss_version` 必须绑定具体 release ID。不得注册 `ota.install/prepare/cancel`、固件 URL、分区写入或远程重启工具。工具声明、执行可用性和恢复能力必须取交集；工具结果使用 operation ID、tool call ID、idempotency key 与 boot session 建立审计关联，跨 deep sleep/重启的 replay 行为由每个领域 handler 显式声明。

### 5.12 任务、锁、实时性与资源预算

每个 board profile 必须带 `runtime_budget`：

- 任务名、owner、优先级、core affinity、栈大小、栈所在内存类型及最低 stack high-water mark。
- 最小 free internal heap、minimum largest block、PSRAM headroom 和 DMA/internal reserve。
- 输入 DOWN 到业务消费的最大延迟；取消和闹钟解除的最大延迟。
- 音频 capture/read/write deadline、允许 overrun/underrun 数。
- Display 最大排队/呈现/flush 延迟、目标帧率和允许跳帧率。
- Device Event Queue/Display Queue 容量和允许丢弃率。
- 固件、IRAM/DRAM 和嵌入资源相对基线的最大增长量。
- 配网 AP/HTTP/DNS、TLS handshake/upload、MultiNet load/unload 和全屏 DMA 同时/切换时的 phase-specific memory watermark、largest block 与临时 reservation。
- 每个关键 task 的最大无让出执行时间、heartbeat 周期、允许 WDT margin 和 stop/join deadline。

Resource Pressure Service 是所有运行时资源水位的唯一聚合者：按 profile 阈值和滞回归并 internal heap/largest block、DMA/对象 pool、queue reservation、task stack、Storage 空间以及 thermal/battery 为 `NORMAL/PRESSURE/CRITICAL`。进入 PRESSURE 时停止预取、限制图片/动态字形/非关键动画和 telemetry，收缩网络/媒体并发；进入 CRITICAL 时拒绝新的配网、TTS、图片解码和非必要上传，但保留 cancel/alarm/wake/mute、当前录音安全收尾、故障反馈、Storage/Persistence commit 与诊断摘要的预留资源。降载动作由 capability/policy revision 驱动且可逆，板级 HAL 只报告资源事实，不决定会议、回复或网络业务；达到恢复滞回并验证 largest block/pool 后才逐级开放，禁止 OOM→整机重启成为常态资源管理策略。

全局锁规则：不持锁调用 callback、网络、NVS、全屏 render、任务 stop 或其他 Service；定义并静态记录 App State → Power Transition → Persistence → Resource → Audio/Display 的允许获取关系，原则上避免嵌套持锁。Power Service 只读取 lease snapshot，不能持 power mutex 等待 owner stop 或 storage flush。任务必须注册 owner 和停止方式，禁止未登记的匿名任务；创建 API 与删除 API、内部/PSRAM 栈类型必须匹配。网络/TLS、音频推理和 LCD DMA 的 core affinity 变更必须经实机 WDT/实时性验证。

锁表同时记录 primitive、是否 priority inheritance、可调用上下文、最高持有时长和禁止嵌套集合。共享可变状态优先单写者/队列；确需 mutex 时使用 FreeRTOS mutex 并禁止 ISR 获取，binary semaphore/event group 只表达通知。高优先级 Audio/Input/Display task 不得等待持锁后执行网络、flash、malloc 或低优先级可阻塞工作的 owner；HIL 注入低优先级持锁与高优先级竞争，测量 bounded blocking。自旋/critical section 仅保护常数时间字段交换，不包含 driver call、循环、日志或 callback。

对跨核可见的状态发布建立 concurrency manifest：变量 owner、读者、同步原语、critical section/atomic 宽度、允许 memory order 和 snapshot 一致性。普通 `volatile` 只允许 MMIO 或同时具有外部同步的诊断用途，不作为 task/ISR 通信。状态从多字段全局变量迁移时优先封装 immutable snapshot 或事件；CI 扫描新增的 `volatile bool`、裸 `TaskHandle_t` ownership 和无锁全局 pointer，并要求并发评审。

WDT 是故障探测而不是正常调度手段。Boot Coordinator/Task Registry 为关键 task 声明 heartbeat、最大连续运行区间和预期阻塞原因；长 render、MultiNet 推理、TLS/上传和存储循环按确定的 chunk/yield 点让出，但不能为了通过测试随意放宽全局 WDT。发生 WDT 前尽可能把阶段/owner/operation 写入 RTC boot record；重启后按同阶段预算熔断。不得在代码中临时删除 task WDT 监控或持续喂狗来掩盖死锁，ISR WDT、task WDT 和 brownout/reset reason 分别统计。

### 5.13 时间、文本和本地化契约

- 手势、动画、超时、deadline 和性能统计使用单调时钟；闹钟、日期和天气过期使用墙上时钟。
- SNTP 未同步时不得用错误墙钟触发新闹钟；定义首次同步、时间回拨、时区/夏令时变化和断网恢复行为。
- Clock/Security 共同维护 `TIME_UNTRUSTED → TIME_COARSE_TRUSTED → TIME_SYNCED`。签名固件/受保护 manifest 的构建时间只可作为证书时间检查的合理下界，受信 RTC 保留值可提升为 coarse trust；普通 NVS、未认证 SNTP、HTTP Date 或待验证 Hub 响应不能自证可信时间。
- 首次联网不得靠关闭证书 `notBefore/notAfter` 校验解决“SNTP 尚未同步”死锁。时间早于构建下界、超出最大漂移或发生异常回拨时，不发送生产 token；使用固定信任锚和严格 hostname/SNI 的受控恢复流程，或提示用户/工装校时。Hub 只有在已验证 TLS 会话中返回的签名/受保护时间才能校正时钟。
- TLS 时间可信度与 calendar schedule 分开裁决：coarse time 可能足以验证证书窗口，但不足以执行用户本地时间 schedule。深睡恢复、长期离线和证书轮换后都重新评估 trust state，不能沿用过期的 `TIME_SYNCED` 标记。
- 深睡期间系统普通 wall clock/monotonic 语义会重建；跨深睡的 schedule 使用持久化 epoch + RTC wake 计划，不能拿重启前 `esp_timer_get_time()` 做差。light sleep 恢复后必须验证单调时钟是否连续，Clock Port 在 target/IDF 差异下提供统一契约。
- Clock 通过接口注入，主机测试使用 Fake Clock；计数器、revision 和时间换算必须覆盖回绕/溢出。
- UI 模型字符串统一为合法 UTF-8；长度限制明确按字节、codepoint 或可见 glyph 计数，截断不能切断 UTF-8。
- Renderer 定义缺字 fallback、动态字形缓存不足和圆/矩形屏各自安全行宽。
- 业务文案不写死“屏幕/激活键”等硬件名称；由 Renderer 根据 input binding 生成操作提示。

Alarm Service 不得每秒轮询墙钟作为唯一调度机制。它维护排序队列并向 Wake Deadline Service 发布最早闹钟；设备 ACTIVE 时使用单调 timer 等待到由 Clock Service 映射的 deadline，发生 SNTP/时区校正时重新映射；LIGHT/DEEP_SLEEP 时由 RTC timer 提前唤醒。闹钟的持久化所有权状态、snooze/attempt 和 schedule deadline 必须可幂等恢复，清除闹钟同时撤销对应 wake deadline。

DST 重复小时需要显式策略（只触发一次，按 alarm ID/目标 epoch 去重），DST 缺失小时需要显式策略（跳过或移动到下一个有效时刻）。当前接口以绝对 epoch 创建一次性闹钟时仍保存用户时区上下文用于显示，但不得在时区变化后偷偷改变绝对触发时刻；未来周期闹钟另行定义本地时间语义。

### 5.14 16 MiB 分区、版本提醒、刷机工具升级与用户数据保护

- 定义 `partition_layout_version`，并与 HAL API、profile、firmware release manifest 和 NVS schema 分别演进。固定 16 MiB 产品保留单 `factory` app、model 与 meeting/resource storage；本计划不新增 `otadata/ota_0/ota_1/staging`，也不缩减 storage 为设备端 OTA 让路。
- 设备版本检查只保存 `current_release_sequence/current_version`、`latest_seen_release_id/manifest_digest`、`last_check_at`、`check_after`、`remind_after`、`dismissed_release_id/dismiss_until` 和有界 release notes 摘要/digest；合并写入并设置最小持久化间隔，避免每次轮询或重复提醒造成 NVS 写放大。不得下载或缓存 firmware bytes。检查失败不影响本地业务，退避重试也不能阻止休眠或闹钟。
- Hub API 固定为只读 metadata，例如 `GET /api/device-gateway/v1/updates/latest?clientId=...&channel=stable`。响应字段和比较规则按 5.10.2：包含 release/package ID、version/sequence/channel/published_at/severity、profile/hw/layout/compat、manifest/release-notes digest、minimum ClawMate Maker version、撤回/缓存状态与稳定错误码；不返回设备可下载的 asset URL。认证必须绑定真实设备与 credential generation，错误 profile/布局返回稳定“不适用”状态。
- 默认启动后低优先级检查一次、随后最多每 24 小时一次；遵守 Hub 下发的有界 `checkAfter/maxAge`，但设备设有最小检查间隔、抖动和退避，避免重连风暴。版本比较、开发版、撤回和 Critical 的处置统一遵循 5.10.2 与本节提醒策略，不另设字符串比较分支。
- 三硬件共享 `UPDATE_AVAILABLE` Scene 和 `UPDATE_REMIND_LATER/UPDATE_DISMISS_VERSION` intent：显示当前/最新版本、release ID、需要电脑和 USB，以及官方 ClawMate Maker 客户端入口/短码；设备不显示裸 GitHub URL。EchoEar 继续由圆屏 Renderer 在安全区内完成布局和触控命中，Bread/Fangtang 用各自 Renderer/Input adapter。设备不显示“立即安装”“下载中”“刷写中”或百分比进度。
- `remind_later` 使用有上限的延迟；普通 release 的 `dismiss_version` 只绑定当前 package/release ID，catalog digest 改变、release 被撤回/替换或更高 sequence 出现时自动失效。Critical/security release 可以暂时静音当前展示，但不得永久隐藏，必须由产品策略定义最大 defer/dismiss TTL、再次提醒频率和无障碍反馈；所有策略仍不得触发下载、刷写或重启。
- GitHub workflow 对三 profile clean build 后生成 ClawMate Maker 正式 `.clawfw`：日常 `appUpdate` 默认只包含/写入 application，并显式 `preserves=[nvs, model, storage]`；只有 CI 证明目标 App 不要求同步更新 bootloader/partition table 时才能发布此模式。模型更新必须是独立、显式模式并绑定模型/固件兼容门禁，不能夹带进每次日常更新。首次安装/修复使用 `fullInstall` 的 signed `files + eraseRegions + writeOrder`；现有 padded merged raw image 仅供独立恢复出厂/工装流程，普通更新禁止从 `0x0` 整包写入。包同时携带 manifest、SBOM/provenance、release notes 和最低工具版本；所有 offset/size/regionMaxSize/erase plan 从实际 ESP-IDF 构建产物生成，不手抄。
- `.clawfw` manifest、board profile 与实际 ROM 读取布局共同定义写入边界：工具解析真实 partition table 并计算 `layoutFingerprint`，独立解析包内 partition table 后交叉核对；`reservedRegions` 来自可信 board profile，实际禁止触达范围取 profile、signed manifest 保留声明与真实布局约束的最严格交集。制造身份区、NVS、storage 及未选择模式的区域不得被运行时参数扩大或覆盖。发布顺序为 draft→上传全量→CI 回读 size/digest/allowlist→publish；`packageId + manifest digest` 唯一且不可变，已发布 asset 不可覆盖，修复只能创建更高 `releaseSequence` 的新 release。
- 刷机前先在正常固件模式请求 maintenance readiness，检查 active Meeting/Alarm/Persistence 并安全结束/延期，再进入 ROM bootloader；ROM 模式自身不能声称读取这些业务状态。设备已损坏或无法通信时只能进入显式 recovery，并在首个写入前提示未知 in-flight 数据可能丢失。工具随后复用 ClawMate Maker plan/journal 状态机，写入过程中只在定义的安全取消点取消；单 factory App 擦除开始后中断必须进入 `RECOVERY_REQUIRED`，不能显示普通重试成功路径。
- 刷写完成后按 `.clawfw` 计划对写入范围执行 ROM hash 或分块 readback，复位并用新 nonce 验证正式 `BOOT_STATUS.ready=true`、目标 release/App identity/ELF SHA/layout/capacity/self-test；外部服务另以 `SERVICE_STATUS.ready` 报告，不能用旧的实验 `BOOT_OK/SERVICE_READY` 日志代替。job journal 保存 plan hash、设备强绑定和逐镜像校验证据，应用重启后重新识别/重验再从镜像边界恢复，不根据百分比盲目续写。上一 stable 包只有在 reader/writer schema 窗口和真实恢复演练通过时才可作为恢复候选；保留数据不等于保证旧固件可读取已迁移 schema。
- 由于单 app 分区没有自动回滚，发布门禁必须更严格：三硬件 clean build/HIL、升级与降级兼容、断电故障注入、保留 NVS/storage、错误板型拒绝和刷后回读全部通过后才能发布 stable。固件 schema 迁移继续使用 expand/contract 与 reader/writer window，至少保留上一稳定刷机版本的安全回退路径。

### 5.15 可观测性和诊断契约

所有 Service/HAL 使用统一诊断字段：board ID、HAL API version、service state、operation ID、UI revision、任务名、错误码和耗时。至少提供：

- 生命周期各阶段和回滚原因日志。
- Device Event Queue 深度、峰值、合并数和按类型丢弃数。
- Display revision、render/flush 耗时、跳帧数和 DMA timeout。
- Audio owner、状态转换、锁等待、underrun/overrun 和 wake restart 次数。
- 当前 heap/largest block、PSRAM、任务栈水位和 WDT 诊断快照。
- Resource Manager 中仍存活资源和借用者列表。
- Event envelope 的 source/sequence/boot session/operation/negotiation epoch，以及按类型的排队、合并、过期、拒绝和 reservation overflow。
- 当前 firmware identity、build/partition/profile/HAL/NVS/protocol/tool-schema 版本和兼容判定结果；不记录 secret、完整 payload 或可复用 nonce。
- 更新检查只记录有界结果、提醒原因、版本比较结果、延迟和重试数；release/device 等高基数标识只进入受控审计/trace，不作 metrics label。不得记录或上报 GitHub asset URL、用户本地路径和刷机工具凭据。

日志不能输出 Wi-Fi 密码、gateway token、完整录音数据或其他凭据。诊断开关关闭时，计数器仍保持低成本可读取。

诊断字段必须有稳定 schema、单位和基数上限。板型、错误原因等只允许有界枚举；禁止把 URL、SSID、message ID、任意错误字符串作为无界 metrics label。日志采用有界 ring/rate limit，重复故障聚合计数，避免网络离线、触摸抖动或 codec 故障造成串口阻塞、flash 写放大和敏感数据旁路泄漏。跨任务 trace 使用单调序列与 boot session 关联，不依赖墙上时间排序。

生产 core dump/crash record 必须有显式安全策略。若启用 flash core dump，其分区必须纳入 Flash Encryption、访问控制、容量和 retention 预算；未满足加密条件时 release 默认禁用完整 dump，仅保留有界且脱敏的 reset/owner/phase 摘要。secret、凭据、配网表单、录音/PCM buffer 和可复用 token 所在内存不得进入可导出 dump，或在进入稳定状态前通过专用敏感内存区与 zeroization 控制排除。dump 关联 firmware digest、boot session 和 crash sequence，只有认证诊断工具在产品/用户策略授权后才能导出，并有审计与速率限制。恢复出厂、解绑/设备转移和 retention 到期必须清理 dump；低电量、空间不足或启动恢复不能被 dump 写入/上传阻塞，容量耗尽不得覆盖用户数据分区。

诊断与 UX 之间使用稳定的 `degradation_reason_t`，只表达用户可理解的能力状态，例如麦克风不可用、扬声器不可用、存储不可用、网络离线、时间未可信、休眠/唤醒暂不可用；不得把 board ID、GPIO、controller 名称或底层错误码直接显示给用户。Feedback Service 将同一 reason 映射为圆/矩形屏提示、LED、提示音或触觉；无显示设备仍有 profile 声明的最小反馈路径。提示按 reason+generation 去重、限流和聚合，防止故障风暴；能力恢复通知只在 readiness/健康滞回通过后发出，短暂恢复不反复打扰用户。

所有 Phase 使用统一 requirements/evidence registry。最小字段为 requirement ID、owner、implementation artifact、test ID、target/profile、evidence URI/hash、firmware/artifact digest、实测 budget/baseline、pass/fail/waiver、waiver owner/expiry。CI、主机测试与 HIL 生成不可变 evidence bundle，Phase exit 和 release manifest 只引用匹配当前源码与固件 digest 的证据；缺失、过期或目标 profile 不匹配均视为未通过。

HIL evidence bundle 进一步记录实际 board serial/hw revision、fixture/探针接线版本、仪器型号与校准到期日、供电电压/限流、温度、网络条件、测试脚本/runner revision、原始 trace/截图/音频/功耗数据 hash、重复次数和测量不确定度。每类门禁预先声明 sample count、容差、warm-up 和统计规则；重跑必须保留全部 attempt。Golden 更新作为独立审查提交，关联旧/新差异、原因和批准者，测试 runner 不得自动接受新截图或功耗基线。Flaky 测试只能带 owner、issue 和到期日临时隔离，不能以“最终一次通过”覆盖之前失败；release evidence bundle 需要签名或不可变归档。

### 5.16 硬件自检与量产测试模式

启动自检分为安全只读探测和需用户/工装确认的主动测试：

- 只读：board/profile ID、ESP target、flash、PSRAM、分区、panel/codec/touch 签名和总线冲突。
- 主动：屏幕色条与圆屏安全区、按键/触摸逐项确认、麦克风静音/饱和/噪声检测、扬声器短提示、背光和存储读写。
- 主动测试只能在量产/诊断模式运行，不能在普通启动时突然播放声音、擦写数据或改变电源状态。
- 自检结果包含 board revision、固件版本、profile version、各组件状态和失败码；对应组件失败时从 effective capabilities 移除。
- 量产模式需要明确进入/退出条件、超时和防误触发机制，不得保留开放的未认证网络调试入口。
- release 固件默认编译关闭主动量产测试和危险调试命令；启用必须依赖独立 build flavor，并由物理 strap、签名工装 challenge 或受保护 manifest 二次授权，普通 UI 手势和可写 NVS key 不能单独开启。

### 5.17 安全与凭据边界

- HAL 重构不得降低 HTTPS 证书校验、配对 token、Wi-Fi/EAP 凭据和设备身份保护。
- 日志、crash dump、诊断页面、量产报告和事件 trace 必须脱敏，禁止记录密码、token、完整音频和私有网络配置。
- 产品凭据只能由 Persistence/Security Service 访问，板级 HAL 不读取 gateway token 或 Wi-Fi 密码。
- board manifest/profile 版本校验不能仅依赖可任意修改的普通配置；量产环境优先使用签名 manifest、受保护 NVS 或 eFuse 标识。
- 若启用 Flash Encryption、Secure Boot 或 release/bundle 签名，profile/build manifest 必须声明并由发布流水线及刷机工具校验；本计划不在未设计密钥生命周期前擅自开启。
- Release 固件默认只允许规范化 `https://` Hub origin；解析必须拒绝 userinfo、fragment、空 host、控制字符、非预期 path/query 和歧义端口，并使用解析后的 scheme/host/port 做同源比较，禁止字符串拼接绕过。HTTP 仅允许编译隔离的本地开发模式，UI 必须显著标识且不能携带生产 token。
- TLS 使用受控 CA bundle/可更新信任锚、hostname/SNI 校验和可信时间策略；证书错误不得自动降级 HTTP 或跳过校验。企业 EAP 的 `ca_mode=none` 在 release 中禁用；私有 CA 应作为受保护、可审计的配置对象导入并绑定 server domain。
- TLS bootstrap 必须使用 Clock Service 的 trust state，并验证证书 `notBefore/notAfter`、构建时间下界、RTC 漂移和异常跳变；任何网络端点都不能在其自身身份尚未验证时提供“可信时间”来证明自己。恢复模式也不发送生产 token，不接受任意新 CA 或关闭时间校验。
- Gateway token、Wi-Fi/EAP 密码、私有 CA 和 provisioning secret 定义独立 secret 类型：禁止普通 getter 返回裸指针，使用最小作用域 copy/handle，RAM 使用后清零，crash dump/日志/HTTP error 统一脱敏。普通 NVS 明文只可作为明确记录的过渡风险；量产安全基线应评审 NVS encryption、Flash Encryption、Secure Boot、密钥烧录/轮换/吊销和返修流程，并形成按产品批次可验证的 security manifest。
- 配对、重新配置、恢复出厂、时间未可信时的 TLS、证书轮换和设备转移都必须有 threat model 与失败策略；不能用“设备在局域网”假设攻击者可信。

## 6. 三硬件统一业务行为

### 6.1 Bread Compact 功能母版与三硬件等价矩阵

以下矩阵是正式发布的最小共同功能集。`EchoEar-2ST` 和 `Fangtang-4G` 两列都表示必须达到的最终结果，不是可选 capability；“适配差异”只说明实现入口，不降低功能与恢复保证。矩阵落地到 requirement registry 时，每行必须补充 `scope_class`（`BASELINE_EXISTING/BASELINE_PROMOTED`）、`baseline_revision` 和 owner；表中为保持可读性不重复展开这些治理列。

| `feature_id` | Bread Compact 功能母版 | EchoEar-2ST 对齐要求 | Fangtang-4G 对齐要求 | 等价验收 |
|---|---|---|---|---|
| `voice.command` | 激活键/唤醒词开始录音，支持提前停止、无语音提示、上传与幂等提交 | 触屏/唤醒词进入同一 Command Service | 单键/唤醒词进入同一 Command Service；Wi-Fi/ML307 不改变语义 | intent、状态、终态和远端事件 trace 一致 |
| `voice.wake_word` | 离线“码卡龙”，与 capture/playback/meeting 互斥并自动恢复 | codec 音频适配后行为一致 | direct-I2S 与 ML307 并发预算下行为一致 | 唤醒、拒绝、暂停、重载 trace 一致 |
| `command.cancel` | 处理中双击取消；录音阶段消费双击；迟到结果隔离 | 触屏双击映射相同 intent | 单键双击映射相同 intent | 取消结果、generation 和 gesture drain 一致 |
| `reply.text` | 文字回复、分页、显式保留、首次激活只关闭回复 | 圆屏分页/自动导航完整显示同一内容 | 240×240 五行分页/自动导航完整显示同一内容 | 内容、页状态、关闭行为一致 |
| `reply.image` | 图片与 caption | 圆屏 safe mask 适配，不丢 caption/状态 | 小屏缩放适配，不丢 caption/状态 | scene 字段和终态一致 |
| `reply.audio` | WAV/MP3/TTS 播放及 correlation | ES8311 adapter 实现同一播放契约 | direct-I2S adapter 实现同一播放契约 | 支持格式、取消/打断和结果一致 |
| `audio.volume` | 音量键调整，回复页优先翻页；支持远端音量设置 | 无独立音量键时提供远端/触控菜单入口，codec 音量生效 | 无独立音量键时提供远端/菜单入口，并补齐当前 `NOT_SUPPORTED` 实现 | 用户可达、0–100 语义、持久化和播放增益一致 |
| `meeting.record` | 双击开始、有效手势停止、16 kHz/16-bit/mono WAV | 触屏映射，codec capture 契约一致 | 单键映射，direct-I2S capture 契约一致 | WAV、时长、停止和错误语义一致 |
| `meeting.recovery` | 分块上传、SHA256、NVS cursor、断网/重启续传、确认后删文件 | Wi-Fi 恢复路径一致 | Wi-Fi/ML307 两种 transport 均遵守同一 durable operation | 断电/断网注入结果一致 |
| `provisioning.pairing` | 配网、配对、重新配置事务和配对恢复 | 圆屏二维码/触屏提示适配 | 小屏二维码/单键及 Wi-Fi/4G 选择适配 | 凭据事务、取消、超时和恢复一致 |
| `ambient.status` | 时间、日期、星期、地点、天气、Wi-Fi/Hub、宠物状态/素材 | 圆屏完整呈现 | 小屏分页/紧凑呈现；另可显示蜂窝/电池增强 | 必选 scene 字段、刷新和前景保护一致 |
| `alarm.local` | create/list/clear、离线调度、scheduled 标记、响铃、解除、重启恢复 | 圆屏动画/触屏解除 | 小屏页面/单键解除；ML307 离线不影响本地调度 | tool、NVS、时间、终态和抢占恢复一致 |
| `sleep.schedule` | 指定时间/idle 休眠、alarm deadline、timer/硬件唤醒 | 按圆屏/BOOT/touch 实测 wake matrix 实现相同用户功能 | 按单键/timer 与 ML307 电源恢复实现相同用户功能 | schedule、阻塞规则、wake 后业务状态一致 |
| `update.notification` | 从已配对 Hub 检查适用 GitHub Release metadata，显示新版本/稍后/已忽略及“连接电脑使用官方刷机工具” | 同一 Update Service；圆屏触控管理提醒 | 同一 Update Service；单键管理提醒；Wi-Fi/ML307 仅传 metadata | current/latest/compat、提醒状态、错误和刷新频率一致；设备均不下载或安装固件 |
| `lifecycle.recovery` | 刷机工具升级、计划重启、崩溃、恢复出厂与 durable operation 协调 | 相同 Service/schema 契约 | 相同 Service/schema 契约，另恢复 ML307 observed state | 数据保留/清理、幂等、刷机后 reconciliation 一致 |

功能等价判定规则：

1. 三硬件对相同 `feature_id` 使用同一个领域 Service、事件 schema、状态机和测试用例。
2. 三硬件握手发布相同的正式业务 capability 和 tool 集合；硬件 descriptor 可以不同。
3. 相同业务输入必须得到相同业务结果。耗时允许落在各 profile 已批准预算内，但不能改变超时、重试、取消或持久化语义。
4. 没有相同物理控件时必须提供可发现、可操作的替代入口；不能删除功能或永久返回 `NOT_SUPPORTED`。
5. 小屏可以分页、圆屏可以重排、蜂窝网络可以使用不同 transport，但所有必选信息和操作必须可达。
6. 任一正式 profile 未通过矩阵中的一项，MaClaw AgentOS 三硬件对齐即未完成；不能用 capability 隐藏失败后宣称整体完成。

### 6.2 场景输入映射

业务状态转换完全一致，物理输入映射可以不同：

| 业务场景 | 共享业务结果 | Bread Compact 映射 | EchoEar-2ST 映射 | Fangtang-4G 映射 |
|---|---|---|---|---|
| 待机激活 | 开始一次语音指令 | 激活键单击 | 圆屏单击 | 单激活键单击 |
| 回复可见时激活 | 关闭回复并回到待机；本次不录音 | 激活键单击 | 圆屏单击 | 单激活键单击 |
| 待机次级操作 | 开始会议录音 | 激活键双击 | 圆屏双击 | 单激活键双击；启动网络选择窗口内由系统 binding 消费 |
| 指令录音中次级操作 | 消费事件，不启动会议 | 激活键双击 | 圆屏双击 | 单激活键双击 |
| 远端处理中次级操作 | 请求取消当前指令 | 激活键双击 | 圆屏双击 | 单激活键双击 |
| 会议录音中有效操作 | 停止、保存并进入上传 | 激活键完成手势 | 圆屏有效点击 | 单激活键完成手势 |
| DISPLAY_OFF 唤醒 | 只恢复显示并消费本次唤醒接触；后续新手势才执行业务 | 激活键按下 | 圆屏触摸或 BOOT 键 | 单激活键按下 |
| LIGHT/DEEP_SLEEP 唤醒 | 产生 `DEVICE_EVENT_WAKE`，按 override 策略保持唤醒，不直接录音 | GPIO0；timer | LIGHT：CST816/GPIO42 或 GPIO0；DEEP：GPIO0 或 timer | GPIO0 与 timer 均须实机验证；蜂窝/充电源按 capability 矩阵 |
| 重新配置 | 持久化配置请求并重启进入配置流程 | 激活键长按 | BOOT 键长按 | 单激活键长按；网络传输选择是独立系统意图 |
| 闹钟响铃 | 立即解除，并消费该次完整手势 | 激活键按下沿 | 圆屏按下沿 | 单激活键按下沿 |
| 回复翻页 | 上一页/下一页或设备自动翻页 | 音量上下键 | 自动翻页；后续可映射滑动 | 5 行小屏自动翻页；不占用单键业务手势 |
| 普通界面音量 | 调整输出音量 | 音量上下键 | 无实体音量键，走远端/触控菜单设置 | 无实体音量键，走远端/菜单设置；实施中补齐当前 board port 的音量实现，正式发布不得为 false |
| 网络传输选择 | 切换 Connectivity policy，不改变 Command/Meeting 业务语义 | 固定 Wi-Fi | 固定 Wi-Fi | 启动窗口单键切换 Wi-Fi/ML307，持久化选择并显示当前 transport |
| 新版本提醒 | 提交同一 `UPDATE_REMIND_LATER/DISMISS_VERSION` intent | 音量键选择，激活键确认 | 圆屏触控“知道了/稍后提醒” | 单击切换，长按确认；不提供安装入口 |

上表是行为契约。不得在 `main.c` 中为板型重新实现分支；映射差异由 Input Service 的板型映射数据或 HAL 标准事件表达。

“远端/菜单替代入口”本身也是正式功能，不能停留在文案占位：必须在已配对、暂时离线、配网中、闹钟前景和屏幕不可用等状态定义可达性。若远端入口依赖 Hub 在线，则每个无实体控件 profile 还必须提供至少一个无需 Hub 的本地入口（触控设置页、可冲突消解的单键设置模式或受认证的本地配置通道）；所有入口调用同一个业务 intent/tool、显示当前值与提交结果，具备超时、取消、权限、持久化和可发现提示。仅提供“可通过 API 设置”而用户无法在设备或正式客户端发现，不算功能等价。

## 7. 分阶段实施计划

### Phase 0：冻结业务、视觉、资源和协议能力基线

任务：

1. 记录 Bread Compact、EchoEar-2ST 与 Fangtang-4G 当前构建配置、固件大小、启动日志和可用内存。
2. 按第 6 节逐项录制/记录实体设备行为。
3. 保存 EchoEar 圆屏各 surface 的基准照片：待机、聆听、思考、回复、录音、上传、配网二维码、闹钟。
4. 为共享交互状态机建立主机侧纯 C 测试夹具，允许注入输入事件和设备能力。
5. 记录现有任务、优先级、栈、core affinity、mutex/锁顺序、I2C/SPI/I2S ownership、DMA buffer 和 NVS key。
6. 保存三硬件当前 `clientCapabilities`、Hub 回显和功能缺口；以 Bread Compact capability/行为为基准制定 EchoEar/Fangtang 补齐台账。
7. 记录 interaction generation、取消 worker、reply correlation 和迟到消息过滤的完整事件 trace。
8. 记录 partition layout、NVS schema、storage 非空保护、烧录保留策略和当前无 OTA 槽的事实。
9. 测量输入/取消延迟、音频 deadline、显示耗时、内部 heap/largest block、PSRAM、任务栈和固件大小。
10. 为每个 profile 生成首版 `runtime_budget`；预算值不得留 `TBD`。建议初值以稳定实机最差值加安全余量制定，并在评审记录中注明样本和计算方法。
11. 盘点 `main.c` 中直接 SPIFFS/`FILE *`、Wi-Fi/HTTP、设备身份、会议恢复和固定握手 JSON 的调用点，形成迁移清单与目标 owner。
12. 对当前 `/storage/meeting.wav`、NVS 恢复游标、WAV header 修复和 chunk 提交顺序做断电基线测试。
13. 记录三硬件当前熄屏行为、静态/峰值功耗、可用 RTC/GPIO/timer wake source、电平保持和外设供电状态，特别验证 Bread GPIO0、EchoEar GPIO0/GPIO42，以及 Fangtang GPIO0、ML307/charger/battery 相关电源域。
14. 建立“配置不等于实现”清单，确认 `MACLAW_POWER_SAVE`、PM/tickless、battery ADC、light/deep sleep 当前每项的源码实现、构建状态和实机证据，未实现项不得进入基线 capability。
15. 盘点全部 `ESP_ERROR_CHECK`、永久 task/timer/ISR/event handler 和 GPIO0 strapping 使用，形成错误分级表、Task Registry 与睡眠/停止影响清单。
16. 记录当前 `app_main()` NVS 自动擦除路径及所有受影响 namespace/key，制作非破坏恢复测试镜像；未完成备份前不得用损坏样本做实机擦除试验。
17. 记录 Gateway 30 秒长轮询、90 秒在线窗口、队列/媒体上限、ACK/cursor 和 Hub 重启行为，按消息类别标注 latest-wins、transient 或必须 durable。
18. 测量三硬件 MultiNet always-on listening 的功耗、I2S/任务/模型占用及 stop/join 时间；核对各 profile 的 battery/charger 电路、ADC 校准条件与可用于 charger wake 的真实引脚。
19. 对当前开放 SoftAP、`nopass` QR、本地 HTTP 表单、AP+STA 恢复模式、pair code、Hub URL 与 EAP `ca_mode=none` 建立 threat model 和抓包基线，记录凭据在 RAM/NVS/log/crash dump 的完整数据流。
20. 记录所有跨模块 ops/descriptor 的大小、字段偏移、枚举值与版本，形成 HAL ABI golden manifest；盘点 DMA/internal/PSRAM 分配点及 flash cache-disabled 可达路径。
21. 为关键 task 建立 WDT/heartbeat 基线：最大无让出时间、正常阻塞点、最坏 TLS/显示/推理/存储时长和 reset reason 诊断可见性。
22. 冻结三硬件显示/输入/音频结构化能力：logical coordinate、rotation/mirror/safe mask/panel offset、touch transform、pixel format/color order、DMA stripe，以及 PCM format/frame alignment/channel map。
23. 盘点三硬件麦克风物理 mute、独立 LED/状态灯、触觉和安全反馈电路；没有硬件能力时通过替代反馈满足共同业务契约，不用屏幕动画冒充物理隐私指示。
24. 记录 USB Serial/JTAG identity 查询任务、line/body 上限、固件/ELF digest、`product/board/hw/layout/compat` 字段和 BOOT/SERVICE readiness 语义，纳入 Task Registry 与有线烧录兼容基线。
25. 盘点所有安全随机值的生成点和启动时序，确认硬件 RNG/DRBG 强随机就绪条件、reseed、失败处理及不得使用 MAC/时间戳替代的门禁。
26. 冻结 Gateway protocol major/minor、capability/tool/media descriptor 版本与 Hub accepted 行为，记录冷启动、重连、换 Hub 和重新认证时的 session/negotiation correlation。
27. 为每类 Device Event 记录 producer、payload ownership、priority/reservation、峰值速率、coalescing/丢弃策略和 sequence gap 基线。
28. 生成当前 Service/Task/Resource 依赖图和唯一 composition root 清单，记录所有全局单例、隐式 init、构造时起任务和环依赖候选。
29. 盘点 health/capability 的失败与恢复触发点，定义防抖窗口、恢复自检、最短保持时间和 flapping 基线。
30. 记录 I2C/SPI/I2S stuck/error/reset 路径、共享器件影响面、bus recovery 电气可行性和旧 handle 失效规则。
31. 盘点配置 key 的所有来源/写者/优先级/TTL/重启语义，以及 touch/audio/battery/panel 校准的 provenance 与硬件绑定。
32. 测量长会议录音的实际采样时钟 ppm、DMA sequence gap、WAV 样本数/墙钟差和 overrun/underrun 基线。
33. 为全部 `static/volatile` 跨任务状态建立 concurrency manifest，记录 owner/reader/同步原语/ISR/内存序；盘点 binary semaphore 冒充 mutex、无优先级继承及多字段无锁快照。
34. 建立 facade API 迁移台账和 legacy/new ownership map，标记允许 shadow 的纯计算、禁止双跑的副作用、quiescent cutover、回退窗口和删除条件。
35. 生成 event/tool/status schema registry 基线，冻结所有已发布 ID 的显式数值与 tombstone，禁止后续 enum 自动重排。
36. 保存 ESP-IDF/toolchain/dependencies.lock/sdkconfig/partition/嵌入资源和生成脚本输入，生成首版 SBOM、许可证/CVE 与 clean-build reproducibility 报告。
37. 建立 fault-domain manifest，标注 shared-I2C/audio/display/network/storage 的 owner、borrower、故障传播、独立重启能力、operation outcome 与隔离策略。
38. 测量 Boot、stop、restart、sleep prepare、configuration apply、provisioning 和 meeting finalize 的端到端耗时，冻结父子 deadline 分配与 cleanup reserve 基线。
39. 盘点每个 NVS/Storage/credential/calibration schema 的 reader/writer version window、不可逆点、空间/写放大和旧固件回滚风险。
40. 冻结用户降级 reason/反馈矩阵、生产 core dump 配置与敏感内存数据流，并建立 requirement→test→HIL evidence registry 基线。
41. 建立三硬件 electrical-safety manifest，实测 reset/bootloader/WDT/brownout/deep-sleep/下载模式下功放、背光、电源轨、reset/CS、Fangtang modem power/guard 与 strapping pin 的默认电平和毛刺窗口。
42. 盘点每个可重启 Service 的 authoritative/durable/ephemeral state、subscription/timer、desired/observed probe 和 reconciliation 规则，禁止旧 runtime context 直接复活。
43. 冻结 DFS/PM 各频点下 CPU/APB/I2S/LCD/I2C/UART/timer 的 clock-domain 与 lock 基线，并定义 NORMAL/PRESSURE/CRITICAL 资源压力阈值和 emergency reserve。
44. 登记 HIL fixture、仪器校准、环境、重复样本、容差、golden 审批和 flaky quarantine 基线，旧证据缺元数据时不得直接沿用 release 结论。
45. 对现有 GitHub workflow 做发布产物审计：冻结 `RapidAI/MaClaw` release tag/channel、三 profile segmented upgrade bundle、仅恢复用 merged raw image、signed manifest、SBOM/provenance 和 release notes 命名契约。
46. 运行三 profile 隔离 clean release build，记录 application/merged image 大小、单 factory 槽增长余量、model 和 storage 预算；冻结“不新增 A/B/staging、不牺牲会议存储”的 16 MiB layout 决策。
47. 冻结 Hub Update Catalog threat model/API：GitHub release/manifest 信任、签名/密钥轮换、device/client/profile binding、metadata cache TTL、检查限流、隐私和审计契约；设备响应不得包含 firmware URL。
48. 冻结版本提醒策略：stable/beta channel、启动/24 小时检查频率、critical 提醒强度、稍后/忽略当前版本、圆屏/矩形屏交互和“使用官方刷机工具”文案；明确没有远程安装入口。

退出条件：三硬件基准、Bread 功能母版、对齐差距、事件 trace、数据版本和可量化预算齐全，所有后续回归都有可比较对象。

### Phase 1：建立 HAL 生命周期、Resource Manager 和板型注册

任务：

1. 新建 `hal/` 接口、生命周期契约、API 版本和 `board_capabilities_t`。
2. 分别新建 Bread、EchoEar 与 Fangtang 的 `board_profile.c`、`board_resources.c`；稳定 profile ID 与第 1 节一致，禁止让 Fangtang 继续复用 Bread profile 或以编译宏伪装成 Bread revision。
3. 将三硬件的 I2C、SPI/I2S、DMA、显示总线、ML307 电源域和 PM lock 所有权收归 Resource Manager；EchoEar 共享 I2C、Fangtang modem/显示/音频资源均须声明 borrower、冲突和恢复顺序。
4. 增加 Boot Coordinator、依赖拓扑、readiness barrier、DEGRADED/SAFE_MODE。
5. 增加 board/profile/revision/target/flash/PSRAM/partition 校验和安全的硬件签名探测。
6. 修改 CMake/Kconfig，使每个构建仅选择并链接一个 board profile。
7. 让现有 `board_port_*` 暂时代理到新 HAL，保持业务调用和设备表现不变。
8. 增加 HAL 合法性、生命周期幂等和部分初始化失败回滚测试。
9. 增加 Power/Wake HAL capability、wake-source arm 校验和 boot wake-cause 早期采集接口；未完成实机验证的触屏唤醒能力保持 false。
10. 建立早期 boot record、稳定运行确认点和连续失败 SAFE_MODE 熔断，替换可降级组件上的无条件 `ESP_ERROR_CHECK`。
11. 建立 Task Registry，要求所有 task/timer/ISR/event handler 可 stop/join/drain/unregister。
12. 将 firmware identity/USB Serial 查询收归 Diagnostic Platform Service，补齐 stop/join、release allowlist、transaction/nonce correlation 和有界 parser。
13. 建立 Entropy Service，在 boot session、operation nonce、配网和配对 secret 生成前设置强随机 readiness barrier。
14. 固化唯一 composition root 和无环 Service dependency manifest；所有 Service 仅通过显式注入访问依赖，主机 Fake 不链接板级/ESP-IDF 实现。
15. 为 Resource Manager 增加共享总线 generation、quiesce/recover/reprobe 和 borrower handle 失效流程；恢复策略按 profile 电气能力声明。
16. 冻结 DEVICE_READY 后关键实时路径的分配，建立可注入 allocator、owner quota 与 steady-state alloc/free 门禁。
17. 建立 concurrency/lock manifest，逐步把 `volatile` 全局 flag 迁为单写者事件或受审查 atomic/critical section，并验证 priority inheritance 与 bounded blocking。
18. 建立集中 schema registry 并生成 Device Event/tool/status C 定义、协议 schema、测试向量；已发布 ID 只废弃不复用。
19. 实现 Service Supervisor 与 fault-domain restart transaction，统一 admission close、operation/callback drain、generation 失效、重新自检、能力滞回恢复和失败隔离。
20. 在生命周期 API 内部统一使用绝对单调 deadline，向所有 init/stop/restart 子步骤传播剩余预算并预留有界 cleanup reserve。
21. 为每个 restartable Service 实现从 authoritative snapshot 到 desired/observed state 的幂等 reconciliation，重建 subscription/timer 且禁止恢复旧 task/lock/queue/driver context。
22. 实现极小 `board_safe_early_init()` 和 profile electrical-safety 校验；未知 revision 只应用三硬件/修订版公共安全交集。

退出条件：三个正式 profile 均能编译；profile 能准确报告能力；板型错配不会驱动高风险输出；任意初始化故障点均不遗留任务、句柄或内存。

### Phase 2：建立 Device API、事件队列和统一业务意图

任务：

1. 新建仅供业务依赖的 `device_api.h`，禁止业务直接访问 HAL ops。
2. 定义不依赖 ESP-IDF/FreeRTOS 的 `device_status_t`、毫秒 timeout/单调 deadline、错误映射与输出参数失败契约；公共 header 禁止出现平台类型。
3. 建立有界 Device Event Queue 和唯一 App Interaction Task。
4. 定义 `device_operation_context_t`、generation、取消 token、单终态提交和 reply correlation。
5. 定义 opaque handle 的 type/generation、幂等 close、callback registration token 与 unregister/drain 契约。
6. 将录音、播放等长操作改为 worker/service 异步执行，不阻塞 App Interaction Task。
7. 从现有两个 `board_port` 及其中的 Fangtang 条件分支抽出三个独立 Input HAL/profile binding；公共 Input Service 不感知这些实现来自两个旧文件还是三个新 profile。
8. 保留 EchoEar CST816 的原生双击、重复 contact 排除和释放 drain 逻辑。
9. 新建共享 `input_service`，实现第 6 节全部业务意图映射。
10. 将 `on_user_input()` 改为 `on_app_intent()`，只处理业务意图。
11. 移除 `main.c` 中闹钟解除的板型条件编译。
12. 移除 `app_ui.c` 中 Bread Compact 回复翻页特判。
13. 验证队列满载时控制事件优先、高频事件合并、取消/完成竞态和 HAL stop 不产生晚到 callback。
14. 建立 `command_capture_service`、`meeting_service`、`alarm_service` 的领域边界，禁止 Device API 出现会议/闹钟/固定时长等业务名称。
15. 将物理唤醒统一转换为 `DEVICE_EVENT_WAKE`，定义 wake contact guard/drain，禁止同一次按键/触摸又唤醒又触发业务手势。
16. 引入版本化 Device Event envelope、producer sequence、payload ownership 和 boot/operation correlation；为关键控制事件建立独立 reservation/mailbox、sticky overflow 与公平性监督。
17. 建立 Configuration Service，统一配置来源、优先级、immutable revision snapshot、observer apply/rollback 和非法远端覆盖门禁。
18. 为 touch/panel/audio/battery 等校准定义 provenance、硬件 revision 绑定、完整性、原子 revision 提交和回滚契约。

退出条件：共享业务层不存在输入来源或板型判断；三硬件输入行为符合第 6 节并覆盖相同业务 intent；所有异步副作用均受有效 operation generation 约束。

### Phase 3：建立完整 UI 模型、数据所有权和 Display Task

任务：

1. 补全 `app_ui_model_t`，覆盖全部 surface 数据和前景所有权。
2. 将 `app_ui_*` 从“模型更新加立即转发”改为原子状态转换。
3. 明确定义 surface 优先级：闹钟/配网/录音/上传/回复/消息/待机。
4. 将回复是否可关闭、分页、ready timeout、显示锁和取消恢复收归共享 UI/Display Service。
5. 增加唯一 Display Task、snapshot revision、队列合并和 DMA fence。
6. 将图片、字符串、QR matrix 和动态字形改为明确的深拷贝/引用计数所有权。
7. 从共享 `app_ui.h` 移除 `qrcode.h` 和其他具体渲染库依赖。
8. 为关键状态转换、临时缓冲释放和过期 revision 增加测试。
9. 迁移期间对 UI layout decision 启用只读 shadow/golden trace 对比；legacy 或新 Display 只有一方提交 framebuffer/DMA。

退出条件：板级显示实现不再维护业务含义上的第二套状态机；任意时刻可从 `app_ui_snapshot()` 得到完整可重绘且数据有效的画面；只有 Display Task 访问 panel。

### Phase 4：抽取 Bread Compact Display HAL

任务：

1. 将 ST7789 初始化和 framebuffer 提交迁到 Bread Display HAL。
2. 将矩形屏场景绘制迁到 Bread Renderer。
3. 抽取可安全共用的字体、UTF-8、RGB565、字形缓存和基础绘图工具。
4. 使用共享模型驱动全部 Bread surface。
5. 在 facade seam 完成 legacy/new trace 差分、资源 handoff 和回退演练后切换唯一 Display owner；禁止双驱动 ST7789。

退出条件：Bread Compact 视觉和行为与 Phase 0 基线一致；旧 Bread 显示入口不再承载 UI 状态。

### Phase 5：抽取 EchoEar 圆屏与 Fangtang 小屏 Display HAL

任务：

1. 将 ST77916 QSPI 初始化、DMA 同步和双缓冲提交迁到 EchoEar Display HAL。
2. 将圆屏安全区、宠物动画、曲线文字、录音波形、回复、二维码和闹钟绘制迁到圆屏 Renderer。
3. Renderer 仅消费共享 UI snapshot，不决定业务状态。
4. 对照 Phase 0 图片验证像素布局和动画节奏，不将 Bread 矩形坐标移植到圆屏。
5. 在 facade seam 完成圆屏场景的只读布局差分、QSPI/DMA drain 和 generation handoff；任一时刻仅一个 Renderer/Display HAL 驱动 ST77916。
6. 将 Fangtang NV3023 初始化、240×240 logical viewport、GRAM Y offset=80、stripe/DMA 提交迁入独立 Display HAL，禁止继续由 Bread board port 条件编译驱动。
7. 将方糖紧凑布局、五行回复分页、网络/电池增强状态与全部公共 Scene 绘制迁入 Fangtang Renderer；所有 Bread 功能母版必选字段和操作入口可达，同时保留方糖视觉。
8. 为 NV3023 panel offset、裁剪边界、自动翻页、DMA drain 和 display generation handoff 建立 golden/HIL，任何 legacy/new 阶段均只有一个显示 owner。

退出条件：EchoEar 圆屏与 Fangtang 240×240 小屏的全部公共 Scene 均完整可达并通过各自 golden；两者保留原显示形态，无旧界面覆盖前景界面的竞态；Fangtang 显示不再依赖 Bread board port。

### Phase 6：抽取 Audio HAL 和共享 Audio Service

任务：

1. 抽取 Bread 直连 I2S Audio HAL。
2. 抽取 EchoEar ES7210/ES8311 Audio HAL及采样调理。
3. 抽取 Fangtang 直连 I2S Audio HAL，并实现统一 0–100 音量、持久化和实际播放增益；删除当前 `ESP_ERR_NOT_SUPPORTED` 终态。
4. `audio_service` 只统一通用 capture/playback/volume 会话和音频所有权；固定时长/WAV 封装迁入 `command_capture_service`，会议状态和恢复迁入 `meeting_service`。
5. 将 MultiNet 生命周期和恢复监督迁入共享 `wake_word_service`。
6. 确认音频播放期间、录音期间和配网期间的麦克风所有权切换没有死锁。
7. 为 Audio Service 建立显式状态机、唯一锁顺序、超时回滚和诊断快照。
8. 增加实际 sample-clock、DMA timestamp/sequence、ppm drift、gap/overrun/underrun 和录音完整性契约；WAV 时长以提交样本数为主。
9. 音频切换仅在 IDLE/quiescent point 进行，先停止 wake/capture/playback 并 drain DMA；shadow 只比较 PCM 统计/意图，不允许 legacy/new 同时打开 I2S/codec、录音文件或播放路径。

退出条件：三个 profile 不再复制 capture/playback/唤醒词状态机；会议和指令录音只复用通用音频 API，不出现在 Device/HAL 接口名中；三硬件音频与音量功能通过实机验证。

### Phase 7：平台边界、版本检查/刷机工具、电源调度、能力投影和构建依赖收口

该阶段跨度最大，必须拆成可独立构建、可独立回滚的 7A-a/7A-b/7B/7C 里程碑；禁止把网络、持久化、版本检查、刷机工具、电源和协议一次性落在同一提交后才开始实机验证。

#### Phase 7A-a：平台数据边界与构建收口

任务：

1. 将 Bread Renderer 的 `gateway_ready` 等产品状态迁入共享 Persistence Service。
2. 统一 NVS mutex、namespace、schema version、迁移和损坏恢复策略。
3. 按板型拆分 CMake component 依赖、sdkconfig overlay、分区表、字体和模型资源。
4. 为每个 profile 生成构建清单：flash/PSRAM 下限、总线占用和必选组件。
5. 固化 partition layout version、升级/降级兼容矩阵和烧录保留策略。
6. 加入 Fake Clock，统一单调时间、墙上时间、SNTP 未同步、时区和回拨处理。
7. 固化 UTF-8、字形 fallback、安全截断和硬件无关提示文案规范。
8. 建立 Storage Service/port，将 SPIFFS、路径和 `FILE *` 从会议及资源业务迁出，并完成文件/恢复记录的原子提交顺序。
9. 移除 `app_main()` 的 NVS 自动整区擦除，建立 factory-blank 判定、只读恢复/SAFE_MODE、逐 namespace 迁移和受控恢复出厂流程。
10. 建立 Connectivity/Gateway/Identity 边界，迁出 Wi-Fi/HTTP/Hub/设备 ID，加入有界退避、circuit breaker 和 reboot budget。
11. 验证 device ID 不随 NVS 克隆重复，boot session/operation ID 在重启后不复用。
12. 建立 Provisioning Service，把 AP/STA、DNS/HTTP server、配置 staging/confirm/rollback、物理取消、TTL 和凭据清零从 `main.c` 迁出；重新配置与恢复出厂使用独立意图和事务。
13. 将量产配网改为高熵临时 WPA2/WPA3 会话或等价认证加密通道；禁用开放 AP 明文提交敏感凭据，验证 AP+STA 接口隔离和配网结束后的完整 stop/join。
14. 建立 Security/Credential Service：HTTPS origin 规范化、CA/hostname/SNI、EAP 私有 CA、secret handle/zeroization、NVS/Flash security manifest 和证书/密钥生命周期。
15. 建立统一非可信输入预算和 parser contract，覆盖 JSON/form/URL/base64/图片/WAV/MP3/tool schema，并接入 checked arithmetic、配额、fuzz 与 OOM/CPU 超时门禁。
16. 定义会议录音 consent、retention/TTL、用户删除、上传后删除、受控导出及静态加密风险；恢复出厂区分逻辑删除、crypto erase 和不可承诺的取证安全擦除。
17. 为解绑/设备转移增加本地隔离、离线 revoke tombstone、`credential_generation` 和服务端旧 generation 拒绝。
18. 将配网引导通道建模为 profile capability，验证无屏/无触摸/无实体键设备也可安全进入、取消和恢复，禁止固定产品默认密码。
19. 建立 Configuration Service：声明每个 key 的来源/优先级/权限/TTL/重启语义，以 immutable revision 原子发布并支持 observer apply 失败回滚。
20. 将量产校准与用户配置分离，补齐 provenance、board/hw/device-channel 绑定、完整性、双缓冲提交和上一 revision 回滚。
21. 为 NVS、Storage、credential 和 calibration 分别实现 `DISCOVERED → STAGED → VALIDATED → COMMITTED → CLEANUP_PENDING` 迁移 journal、expand/contract version window、空间/低电量门禁和断电恢复。
22. 让烧录/升级工具校验 reader/writer 兼容窗口与不可逆 migration point；旧固件无法安全读取新数据时关闭回滚承诺或先提供兼容 reader。
23. 在 GitHub workflow 中为三 profile 输出 ClawMate Maker `.clawfw`、manifest、SBOM/provenance、release notes 及 `layoutFingerprint/reservedRegions` 证据；日常 `appUpdate` 只写 App，`fullInstall` 使用 signed `files/eraseRegions/writeOrder`，merged raw image 仅标记为恢复出厂/工装资产，不再生成设备 OTA asset。
#### Phase 7A-b：Hub Update Catalog、统一提醒与受校验刷机工具

24. 实现 Hub `release_catalog`，只处理允许的 `RapidAI/MaClaw` GitHub Release/tag/channel 和已验证 `.clawfw` manifest；对设备仅提供经 device gateway 认证、按 profile/hw/layout 过滤的 latest metadata API，不代理固件字节。
25. 实现 Update Catalog 的 tenant/client/device/profile/credential-generation binding、`releaseSequence` 比较、digest conflict、撤回/降级/开发版、minimum ClawMate Maker version、`checkAfter/maxAge`、稳定错误/retry、检查限流和审计；query `clientId` 不作为授权依据。
26. 实现共享 Update Service/Scene/intent/tool：`update.check/status/remind_later/dismiss_version`，完成 Bread/EchoEar/Fangtang 的提醒映射、相同版本去重、critical 重复提醒和离线/Hub 不可达降级；禁止 `ota.install`、URL、下载和重启入口。
27. 复用官方 ClawMate Maker 从 GitHub Release 下载完整 `.clawfw`，验证 manifest signature、asset size/hash、profile/manifest/tool 三方 allow-list、device identity confidence、真实 `layoutFingerprint`、compat/flash/security baseline 和 minimum flasher version；错板、错布局、缺件、签名错误全部在写入前拒绝。
28. 实现“保留用户数据升级”和显式“恢复出厂刷机”两种独立模式；默认不得擦除 NVS/storage/未上传会议数据，layout/schema 不兼容时先导出/提示并 fail closed。
29. 实现刷机 maintenance→ROM bootloader 流程、稳定 USB/供电检查、ClawMate Maker plan/journal/安全取消点、关键区域 readback、重启后的 nonce `BOOT_STATUS`/`SERVICE_STATUS` 校验，以及单 App 中断后的 `RECOVERY_REQUIRED` 恢复指引。
30. 为单 app 无自动回滚冻结更严格的 stable 发布门禁：三硬件升级/降级矩阵、断电注入、错板拒绝、用户数据保留、刷后 readback、`RECOVERY_REQUIRED` 和上一稳定 `.clawfw` 恢复演练全部通过。

7A-a 退出条件：板级显示/音频驱动不持久化产品状态；NVS 初始化失败不会自动清除用户数据；Storage/Connectivity/Gateway/Identity/Provisioning 已有可停止生命周期；量产配网不通过开放 AP 明文提交 secret；release 只接受经严格校验的 HTTPS Hub；Bread 构建不再无条件依赖 EchoEar 控制器组件。

7A-b 退出条件：三硬件只能从已配对 Hub 获得相同语义的可信版本 metadata，不接收固件 URL、不下载、不刷写；官方刷机工具从 GitHub 获取并完整校验正确 bundle，默认保留用户数据，写后回读并验证设备 readiness；错误 profile/layout/signature、断电和恢复流程通过门禁。

#### Phase 7B：Power、Wake、Clock 与调度

任务：

1. 建立 Power Service、power lease 和 `ACTIVE/DISPLAY_OFF/MODEM_SLEEP/LIGHT_SLEEP/DEEP_SLEEP` 状态机。
2. 建立领域层 Sleep Schedule Service，支持一次性/周期时间窗、跨午夜、工作日、时区/DST、规则优先级、半开边界、手动唤醒 override 和下一闹钟抢占计划唤醒。
3. 实现 `PREPARE → COMMIT` 休眠事务与逆序回滚，协调 Display DMA、Audio/Wake、Connectivity、Storage/Persistence 和 Alarm。
4. 分别为 Bread 按键、EchoEar BOOT/触屏、Fangtang 单键以及 RTC timer 完成板级 wake source 适配；触屏方案无法在 deep sleep 保持时，EchoEar 明确使用 BOOT+timer 或限制到 light sleep，Fangtang 同时验证 GPIO0 strapping、ML307/显示电源恢复与 charger 观测，不把未经验证的 charger attach 声明为 wake source。
5. 将现有 EchoEar idle panel-off 行为重命名/迁移为 `DISPLAY_OFF`，避免与系统休眠混淆。
6. 接入 ESP-IDF PM 初始化与 effective capability 投影；Kconfig/PM/tickless/DFS/ADC 只有实现成功并通过自检后才对外生效。
7. 为 Power transition 增加唯一序列化状态机、COMMIT 前二次校验、短窗口不入深睡和诊断写放大保护。
8. 建立 Wake Deadline Service，统一仲裁 Sleep Schedule、Alarm 与其他 RTC timer 消费者，保证只 arm 最早有效 deadline。
9. 将 Alarm Service 从固定周期墙钟轮询迁为 Clock/Wake Deadline 驱动，并完成 DST、校时和 clear/snooze 的 deadline 更新。
10. 验证 GPIO0 strapping 风险；若“持续按住后 reset/唤醒”不能满足产品安全性，则对应 profile 禁止 GPIO0 deep-sleep wake，仅保留 RTC timer 或要求硬件修订。
11. 将 always-on Wake Word 纳入 power lease；验证停机/join/模型释放和 resume，不兼容的 sleep depth 必须拒绝或明确关闭语音唤醒。
12. 建立 Battery Policy，包含校准、滞回、负载限制、一次性低压 checkpoint、charger/power-good/RTC 复检唤醒以及无可恢复源时拒绝 DEEP_SLEEP。

7B 退出条件：指定时间休眠、低电量保护和 timer/硬件唤醒通过三硬件实机验证；唤醒 contact 不误触发业务；语音唤醒与各 sleep depth 的互斥关系可见且可验证；所有发布的深睡组合都有确定恢复路径。

#### Phase 7C：Gateway 休眠语义、领域工具与能力投影

任务：

1. 为 Gateway poll/HTTP worker 增加 cancel-inflight、quiesce、join 和 resume；休眠 PREPARE 不等待完整长轮询 timeout。
2. 明确 last-seen 超时即离线的现状；不把 best-effort presence 通知设为入睡前置条件，也不因休眠清除凭据。
3. 定义下行消息 durability/TTL/容量/ACK/跨 boot 语义；latest-wins 状态在 handshake 重建，关键 tool/command 在 Hub 具备 durable queue 前不得宣称离线必达。
4. deep sleep 新 session 隔离旧 generation、cursor、pending reply/tool result；light sleep 保持 session 并通过 resume barrier 恢复。
5. 将设备工具改为 Gateway 通用 dispatcher + 领域注册表，新增 capability-gated 的 Sleep Schedule 与只读/提醒型 Update 工具，统一 schema、idempotency、deadline 和风险策略；静态禁止设备端安装工具。
6. 从 `board_capabilities + firmware_features + device_health + Hub accepted` 生成设备能力与工具集合，完成健康收缩和省略 `bootSessionId` 的运行时 refresh handshake；三款正式 profile 的 Bread 公共业务 capability/tool 静态集合必须相同，差异只允许出现在硬件 descriptor、实现参数、临时运行健康及非公共物理增强中。
7. 将 capability 输出改为 immutable/versioned snapshot，operation 绑定 revision，并定义运行中能力收缩时的完成/降级/取消策略。
8. 固化 effective/Hub accepted/negotiated 三层能力、`negotiation_epoch` 与 protocol/tool/media descriptor 版本协商；换 Hub、重连和重新认证不得复用旧授权快照。
9. 为每项 runtime health 增加失败/恢复滞回、最短保持、cooldown 和重新自检门禁；安全故障立即收缩，普通瞬时错误不产生 capability flapping。

7C 退出条件：协议声明、工具集合与真实设备能力一致；休眠时无悬挂长轮询或伪 ACK；Hub 重启、队列超限、跨 boot 迟到消息的降级语义有测试和明确产品承诺。

### Phase 8：自检、量产模式、三硬件对齐与第四异构 profile 验证

任务：

1. 实现安全只读启动自检和受控量产主动测试模式。
2. 增加 Null/Fake Board HAL，仅用于主机测试或编译验证。
3. 用无显示、无扬声器、存储失败、时钟未同步和板型错配等 Fake profile 验证降级/安全模式及独立反馈通道。
4. 将 Fangtang-4G 作为第三个正式 profile 完成全功能对齐，拆除其对 Bread 大型 board port 的条件分支依赖；再用一个刻意异构的最小第四 Fake/Reference profile 完成接入演练，例如“无显示 + 触摸输入 + 无扬声器 + LittleFS/无持久存储”组合，证明抽象不只适合三块现有 ESP32 产品板。
5. 编写“新增硬件适配清单”、profile/build manifest、runtime budget 和 HAL 模板。
6. 验证量产 build flavor、物理/签名授权、超时退出和 release 默认关闭主动测试。
7. 使用 ABI 兼容测试验证 ops 尾部扩展、旧 `struct_size`、未知版本、缺失必选函数和 capability/函数不一致都安全失败。
8. 使用内存类别故障注入验证 DMA/internal/PSRAM pool、cache-disabled 路径、TLS reservation 和 allocation failure 降级。
9. 验证 Task Registry heartbeat、WDT 前 RTC 诊断和同阶段 boot-loop 熔断；禁止通过放宽 WDT 掩盖超时。
10. 用异构 rotation、圆形 mask、触摸 transform、RGB/BGR、mono/stereo 与 frame alignment Fake profile 验证结构化能力，新差异不向业务层增加板型分支。
11. 用带硬件 mute/独立录音 LED 和无此能力的 Fake profile 验证 Privacy/Feedback Service；新增硬件仅实现标准 feedback/privacy HAL 即获得统一业务行为。
12. 用屏幕、LED、音频、触觉和无显示 Fake profile 验证统一 `degradation_reason_t` 的映射、去重、限流、恢复通知和底层错误隐藏。
13. 验证生产 core dump 的加密/禁用策略、敏感 buffer 排除/清零、认证导出、容量/retention、恢复出厂/设备转移清理和低电量非阻塞行为。
14. 建立 HIL fixture/instrument manifest 与 evidence signer，验证校准过期、环境越界、样本不足、自动 golden 更新和 flaky 重跑均不能产生 release pass。

退出条件：三款正式硬件全部达到 Bread Compact 功能基线；自检失败会进入安全模式或明确阻止正式发布；异构第四 Fake/Reference profile 不修改 `app/`、`domain/` 或已有共享 `services/` 即可通过适用的共享测试。

### Phase 9：删除兼容层并完成发布硬化

任务：

1. 删除已无调用的 `board_port_*` facade、宏映射和重复状态。
2. 运行架构扫描、三硬件完整 HIL、Bread 基线一致性测试、长稳测试、断电/重启恢复和资源预算门禁。
3. 验证生产烧录脚本的板型校验、分区保留和 manifest 输出。
4. 完成安全审查：日志脱敏、凭据边界、量产模式退出、调试入口和发布配置。
5. 更新 README、构建命令、迁移/回滚手册、架构检查脚本和目录说明。
6. 更新有线烧录工具：写入前校验签名 artifact manifest 与设备 identity/compat/layout，写后用 flash transaction ID、boot session、实际 digest 和分层 readiness 确认成功。
7. 输出版本兼容矩阵、发布证据包和残余风险清单；明确单 app 刷机无自动回滚、默认数据保留、恢复 bundle 和用户操作边界。
8. 生成每 profile SBOM、许可证/CVE、依赖 hash、toolchain/sdkconfig/partition/resource provenance 和 clean-build 可复现报告，签名后纳入 artifact manifest。
9. 删除 facade 前证明迁移台账所有调用点为零、shadow 差分达标、旧任务/全局状态/NVS writer 不再存活，并完成三硬件回退窗口关闭评审。
10. 生成 requirement→implementation→test→evidence 追踪矩阵和每 profile evidence bundle；校验证据 digest 对应本次 release artifact，清零缺失证据和过期 waiver。
11. 对 reset/bootloader/WDT/brownout/deep-sleep/下载模式电气安全、DFS 全频点和资源压力降载运行三硬件 HIL，并归档原始签名证据。
12. 完成 GitHub Release→Hub metadata catalog→三设备版本提醒，以及 GitHub Release→官方刷机工具→USB 设备的端到端演练；验证设备无固件下载/写入路径、错板拒绝、断电失败、数据保留和上一稳定 bundle 恢复。

退出条件：三硬件发布门禁和 Bread 功能对齐矩阵全部通过；新增板型无需修改 `app/`、`domain/` 或已有共享 `services/`；只需实现 profile、manifest、必要 HAL 与平台 port 即可完成编译和业务测试。

## 8. 测试与验收

### 8.1 自动化测试

必须新增以下测试：

1. 输入事件到业务意图的映射测试，覆盖 Bread、EchoEar 和 Fangtang 三个正式 profile。
2. 交互状态机测试：待机、录音、处理、取消、会议、上传、回复、闹钟和配网。
3. UI surface 优先级、前景所有权和恢复测试。
4. 回复分页和关闭测试。
5. Audio Service 所有权、错误恢复、WAV 头及长度测试。
6. HAL 契约测试：能力与函数实现一致，必选接口不得为空。
7. Fake HAL 构建测试，防止业务反向依赖板级代码。
8. HAL 每个初始化故障点的故障注入、逆序回滚和 `init/start/stop/deinit` 幂等测试。
9. Device Event Queue 压力、溢出、优先级、事件合并、ISR-safe 发布和停止期间晚到事件测试。
10. Display snapshot 深拷贝、图片立即释放、QR 生命周期、过期 revision、DMA fence 超时和队列拥塞测试。
11. Renderer golden image/截图比较，覆盖圆屏安全区和所有关键 surface。
12. EchoEar 共享 I2C 并发、借用计数和销毁顺序测试。
13. Audio capture/playback/wake/setup 并发状态机、锁顺序、超时回滚和资源泄漏测试。
14. `board_capabilities + firmware_features → clientCapabilities` 投影及 Hub 回显降级测试。
15. NVS schema 迁移、损坏恢复和并发写入测试。
16. 无显示、无扬声器、无音量和无唤醒词 Fake HAL 的功能降级测试。
17. operation ID/generation 的 cancel/success/error/timeout 单终态、计数回绕和迟到网络/音频/UI 事件测试。
18. Device API 调用上下文、最大阻塞时间、参数所有权和 callback drain 契约测试。
19. Boot Coordinator 依赖失败、readiness barrier、DEGRADED、SAFE_MODE 和重试测试。
20. board/profile/revision/target/flash/PSRAM/partition 错配测试，确认高风险 GPIO 未被驱动。
21. runtime budget 门禁：任务优先级/core/栈类型、内部 heap/largest block、PSRAM、延迟、队列和固件增长。
22. Fake Clock 测试：SNTP 未同步、时间回拨、时区/DST、计数器和 revision 回绕。
23. UTF-8 合法性、安全截断、缺字 fallback、动态字形不足和双屏安全行宽测试。
24. partition layout/NVS/storage 的升级、降级、断电和非空录音保护测试。
24A. `.clawfw` 负向测试：错 profile/layout/compat/ESP target/flash size、超 factory 槽、错 secure version、篡改 manifest/signature/size/hash、缺模式所需 metadata、`files/eraseRegions` 越界或触达 `reservedRegions`、普通升级误选 merged raw image 全部在写入前被拒绝。
24B. GitHub Release 原子发布测试：allowlist/tag/channel、draft→完整上传/回读→publish、published asset mutation 隔离、canonical 签名 golden vectors、404/429/5xx/redirect/timeout 和旧刷机工具最低版本拒绝。
24C. Hub Update Catalog 授权/缓存测试：缺失/错误 bearer、跨 tenant/client/device/profile/credential-generation、伪造 query `clientId`、release 越权、metadata TTL/退避/限流、GitHub 故障和 token 日志泄漏；响应中不存在 firmware URL/bytes。
24D. 设备更新检查测试：版本比较、stable/beta、相同 release 去重、critical 重复提醒、稍后时间、忽略当前版本、重启/深睡持久化、Hub 离线与时钟不可信；测试桩断言设备无固件下载、partition erase/write、boot target 或远程重启调用。
24E. 三硬件提醒 UI 测试：Bread、EchoEar 圆屏、Fangtang 小屏显示相同 current/latest/severity/“连接电脑使用官方刷机工具”语义，输入只产生 `UPDATE_REMIND_LATER/DISMISS_VERSION`，无“立即安装”入口。
24F. 刷机工具身份/下载测试：只从 allowlisted GitHub Release 获取 bundle；USB identity/profile/hw/layout/compat/flash size 与 manifest 匹配，错板、用户选错文件、断网/部分下载和签名错误均在写入前 fail closed。
24G. 数据保留刷机测试：默认模式在任意成功/失败路径不擦除 NVS/storage/未上传会议，恢复出厂模式必须二次确认；layout/schema 不兼容时要求导出或拒绝，不静默格式化。
24H. 刷写/恢复测试：正常模式 maintenance readiness、稳定供电/USB、active Alarm/Meeting 提示、逐 block/关键提交点断电、ROM hash/readback、新 nonce `BOOT_STATUS/SERVICE_STATUS`、`RECOVERY_REQUIRED` journal 恢复及上一 stable `.clawfw` 的 schema 兼容恢复。
24I. 单 app 发布安全测试：升级/降级 reader-writer window、Secure Boot/Flash Encryption/eFuse 配置、错 key/key revoke/RMA、上一版本恢复和无自动回滚风险提示均与 manifest 一致。
25. 启动自检与量产模式的进入、退出、超时、防误触发和能力收缩测试。
26. 事件 trace record/replay，确保相同输入、时钟和故障序列得到确定性的业务终态。
27. 架构边界测试：Device/HAL API 名称不得包含 meeting/alarm/command-duration；领域服务不得直接引用 SPIFFS、`FILE *`、Wi-Fi、HTTP、GPIO 或板型宏。
28. Storage contract 测试：SPIFFS、LittleFS/Fake、无存储、磁盘满、只读、损坏、原子 rename、`fsync` 失败和介质异常。
29. 在 WAV header、PCM append、恢复记录、chunk cursor、远端 complete、本地删除每个提交点注入掉电/brownout，重启后不得丢失已确认边界或上传错误字节。
30. Connectivity/Gateway 重试测试：指数退避、抖动、circuit breaker、重连风暴、重启预算及长时间离线恢复。
31. SAFE_MODE 反馈矩阵：分别注入屏幕、输入、音频、存储和网络故障，确认错误反馈不依赖已故障组件且不开放未认证入口。
32. Identity 测试：克隆 NVS 后 device ID 仍唯一；boot session/operation ID 重启不复用；熵源失败有明确致命/降级策略。
33. capability 会话测试：运行时健康收缩先约束本地调度，再以省略 `bootSessionId` 的普通握手刷新 Hub 认知；不会重放启动欢迎，且不存在未定义的即时更新消息。
34. 可选 power/sensor telemetry 的无能力、未校准、越界值和低电量 checkpoint 测试。
35. 量产与 release 配置隔离测试，证明普通输入和可写 NVS 无法开启主动工厂测试。
36. Power 状态机和 lease 测试：引用计数、owner 异常退出、deadline、会议/播放/上传/闹钟/配网/Storage commit 阻止深睡及超时回滚。
37. Sleep schedule 测试：一次性、每日、工作日、跨午夜、DST 缺失/重复小时、SNTP 未同步、时间前跳/回拨、时区更新和 schedule revision 迟到事件。
38. Alarm 与 schedule 合并测试：下一闹钟早于计划唤醒时重编 RTC timer；新增/删除/清空闹钟后 deadline 正确更新。
39. Wake HAL contract 测试：timer/button/touch mask、无法 arm 时拒绝睡眠、active-level stale interrupt、立即唤醒循环和无唤醒源保护。
40. 按键/触屏实机唤醒测试：deep/light sleep 能力分别验证；恢复 contact 被 guard/drain 消费，不会同时启动录音、会议或取消。
41. `PREPARE → COMMIT` 每一步故障注入：Display DMA、Wake Word、Wi-Fi、Storage/NVS、wake-source 配置失败均可逆序恢复 ACTIVE，无任务/锁/总线泄漏。
42. deep-sleep 新 boot 与 light-sleep resume 测试：wake cause、boot session、operation generation、UI revision 和任务生命周期无混用。
43. 功耗门禁：三硬件分别测量 ACTIVE、DISPLAY_OFF、MODEM_SLEEP、LIGHT_SLEEP、DEEP_SLEEP 电流和唤醒延迟；Fangtang 另区分 Wi-Fi/ML307 及 modem 电源状态，仅对已验证状态发布 capability。
44. RTC timer 漂移测试：不同温度/时长下测量偏差、boot lead 和 drift guard；闹钟唤醒不得晚于验收容差，时间可信度不足时明确降级。
45. 配置/实现一致性测试：分别组合 `MACLAW_POWER_SAVE`、PM、tickless、DFS 和 battery ADC，确认缺前提、初始化失败或无实现时 capability 为 false 且不会调用空路径。
46. Schedule 规则裁决测试：重叠一次性/周期规则、半开边界、同优先级 revision、用户 override、显式 disable、schema 降级和不支持 depth 的拒绝/降级。
47. Power transition 序列化测试：schedule、idle、低电量和用户请求并发时只存在一个 PREPARE；COMMIT 前新 lease、取消或更早闹钟能可靠中止。
48. 最短有效休眠测试：下一 deadline 过近时停留在 DISPLAY_OFF/MODEM_SLEEP，避免连续 deep-sleep reboot loop。
49. Flash 磨损测试：频繁熄屏/短睡只更新 RAM 计数，NVS 写入频率符合预算；异常 sleep history 仍可诊断。
50. 跨睡眠时钟测试：deep sleep 后不得使用旧 `esp_timer` 基准；light sleep 后验证目标 IDF 下单调时钟连续性。
51. Fatal-path 测试：对每个现有 `ESP_ERROR_CHECK` 对应故障注入，验证分类为 DEGRADED/SAFE_MODE/受控重启且不会形成 boot loop。
52. Task Registry 测试：所有 task/timer/ISR/event handler 在 HAL stop、light-sleep suspend 和初始化回滚时均可 join/drain/unregister，超时不会释放在用资源。
53. Wake Deadline 多消费者测试：闹钟、schedule、低电量复检并发新增/更新/删除时始终 arm 最早项，wake 后一次性消费与周期重算正确。
54. GPIO0 strapping HIL：短按、长按、持续按住、抖动和唤醒后立即 reset，不误入下载模式或形成重复唤醒；失败则 capability 自动关闭。
55. Alarm deadline 测试：SNTP 大幅校时、时区变化、DST 重复/缺失小时、clear/snooze/reboot 后不漏响、不重复响。
56. NVS writer/journal 测试：跨 namespace 提交任一点断电后可幂等恢复，临时 runtime 状态不会在重启后错误复活。
57. NVS 初始化恢复测试：分别注入 `NO_FREE_PAGES`、`NEW_VERSION_FOUND`、坏页、未知新 schema 和迁移中断，Wi-Fi、配对 token、闹钟、schedule、会议 cursor 与校准数据不被整区自动擦除；只有 factory blank 或受控恢复出厂可擦除。
58. Gateway quiesce 测试：长轮询、上传、ACK 和握手分别处于阻塞/响应/重试阶段时请求休眠，均能在预算内 cancel/join；取消失败使 PREPARE 回滚而不是带悬挂 worker 入睡。
59. Hub 离线队列测试：90 秒 last-seen 超时、Hub 重启、队列达到上限、媒体 TTL 过期、ACK 响应丢失和设备长时间深睡下，latest-wins 状态可在 handshake 重建；关键消息在未具备 durable queue 时明确失败或降级，不虚构必达。
60. 跨 session 测试：DEEP_SLEEP 后生成新 `bootSessionId`，旧 generation、cursor、取消结果、回复和 tool result 不污染新交互；LIGHT_SLEEP 保持 session 且 resume barrier 前不开放业务 readiness。
61. Wake Word/Power 组合测试：always-on listening 持有正确 lease；LIGHT/DEEP_SLEEP 前 stop/join/释放模型与 I2S，恢复后顺序正确；选择始终语音唤醒时禁止不兼容深睡，选择深睡时 UI/能力明确语音唤醒暂不可用。
62. Battery Policy 测试：ADC 未校准、噪声/离群值、负载压降、充放电切换、温度缺失、阈值滞回和 brownout；低压最多进行一次关键 checkpoint，不反复写 flash，不在无 charger/button/timer 恢复源时进入 DEEP_SLEEP。
63. Charger wake HIL：USB/充电器插拔、power-good 有效电平、已满/反向抖动、RTC 复检和恢复滞回；profile 没有 charger-detect 电路时 capability 与策略不得声明该唤醒源。
64. 领域工具测试：Sleep Schedule/Update tool descriptor 随 effective capability 注册/撤销；schema、时区/depth/wake/releaseId、idempotency replay、超时、跨重启和迟到调用正确；设备明确不注册 `ota.install/prepare/cancel`，Update 工具拒绝 URL，Gateway dispatcher 不依赖具体领域实现。
65. Provisioning 安全测试：临时 AP 密码/session ID/CSRF nonce 的熵、轮换和 TTL；连接数、请求体、速率限制、物理取消、超时回退、RAM zeroization，以及非授权客户端无法读取/修改 Wi-Fi/EAP/Hub 配置。
66. AP+STA 隔离测试：AP 客户端无法访问 STA/Hub token、管理接口或转发流量；DNS/HTTP/AP handler 在成功、失败、取消、低电量和重启准备时全部 stop/join/unregister。
67. 配置事务测试：`stage → validate → commit → activate → confirm` 任一点断电或 Wi-Fi/Hub 失败均恢复旧已确认配置；新配对确认前不撤销旧 token，pair code 过期/重放/暴力尝试均失败且不长期留存。
68. 恢复出厂测试：与普通重新配置使用不同物理意图；未上传录音提示/保全、远端 token best-effort 撤销、本地分级擦除、tombstone 掉电恢复、设备根身份策略和审计记录均符合契约。
69. URL/TLS/EAP 测试：拒绝 release 中的 HTTP、userinfo、fragment、控制字符、空 host、歧义端口和跨 origin token；证书过期/错误 hostname/未知 CA/可信时间未就绪不自动降级；`ca_mode=none` 仅受控开发构建可用。
70. Secret 测试：日志、HTTP error、trace、crash dump 和量产报告无密码/token/pair code/私有 CA；临时 buffer 使用后清零，secret handle 越权访问失败，NVS/Flash security manifest 与实际 eFuse/build 配置一致。
71. HAL ABI 测试：`struct_size/abi_version`、尾部追加、旧结构、未知枚举、保留字段非零、空必选 ops 和 capability/函数不一致均确定性拒绝，不发生越界读取或错位调用。
72. Memory contract 测试：DMA/internal/PSRAM/RTC 分配类别、alignment、largest-block、pool exhaustion、fence 后释放和 cache-disabled 路径；TLS、全屏 DMA、NVS commit 与 MultiNet 切换压力下无非法 PSRAM 栈/数据访问。
73. WDT/heartbeat 测试：故意卡住 Display、Audio、Gateway、Storage 和 Wake Word worker，验证 owner/phase/operation 写入 RTC record、reset reason 分类和同阶段熔断；禁止测试通过提高全局 WDT timeout 获得假成功。
74. Display/Input 坐标契约测试：0/90/180/270°、mirror、panel offset、圆形 mask、边缘/九点触摸校准、rotation 后 wake contact drain；渲染与点击命中使用相同 logical coordinate，圆屏不可见角落无可交互控件。
75. Audio capability 协商测试：mono/stereo、采样率、PCM 位宽、native frame/alignment、partial read/write、启动/排空延迟和不支持格式；共享 Audio Service 正确转换或拒绝，领域业务不出现 codec/板型判断。
76. Privacy/Feedback 测试：capture start/stop、失败回滚、会议/指令切换、DISPLAY_OFF、网络离线和 task 卡死时录音指示覆盖真实采集窗口；硬件 mute 立即阻止采集，解除 mute 不自动录音，反馈故障按 profile 安全策略降级。
77. 公共 API 可移植性测试：公共 header 在不提供 ESP-IDF/FreeRTOS header 的主机构建中独立编译；平台错误集中映射为稳定 status，retry/severity 一致，timeout 换算无 tick 漂移，失败输出参数保持安全值。
78. Handle/callback 生命周期测试：伪造类型、旧 generation、double close、close 后读写、HAL restart、light/deep sleep 和 unregister 与在途 callback 竞争均不发生 UAF；drain 超时返回确定性状态。
79. TLS 可信时间引导测试：无 RTC、仅构建下界、受信 RTC、SNTP 未认证、时间回拨/前跳、深睡漂移及证书轮换；不能用待验证服务自证时间，任何失败都不关闭证书校验或发送生产 token。
80. 非可信输入 fuzz/配额测试：JSON/form/URL/base64/WAV/MP3/image/tool schema 的深度、重复 key、整数溢出、解码炸弹、redirect、chunked/content-length 冲突、OOM 和 CPU budget；失败无部分副作用、成功 ACK、队列饥饿或无限重试。
81. 录音数据治理测试：indicator/consent、retention、容量淘汰、用户删除、上传后删除、未上传恢复和认证工装导出；未加密介质不宣称安全擦除，启用加密时验证销毁数据密钥后的 crypto erase。
82. Capability snapshot 测试：只读 profile、单写者 health、revision 获取/释放和运行中收缩；同一 operation 不混用多个 revision，新操作拒绝与在途完成/降级/取消策略确定。
83. 解绑/转移测试：本地 token 先隔离，离线 revoke tombstone 可幂等恢复且有期限；旧 NVS 镜像、迟到确认与备份恢复的 credential generation 均被 Hub 拒绝。
84. 无屏配网测试：屏幕二维码、每设备标签 secret 和认证工装 adapter 至少各覆盖一个 Fake profile；无输入设备仍能超时退出/恢复，固定默认密码和不可轮换标签凭据被发布门禁拒绝。
85. Device Event envelope 测试：未知版本/type、payload 长度、栈/驱动临时指针、producer sequence gap/重复/乱序、旧 boot/generation 和 ownership transfer；ISR 路径零 heap 分配且停止后无晚到访问。
86. 队列过载/公平性测试：telemetry flood、touch bounce、audio level、网络 burst 与 cancel/alarm/wake/mute 并发；普通流量不能占用关键 reservation，关键 overflow 进入 sticky fail-safe，coalescing 不吞 operation 终态。
87. 能力协商测试：effective、Hub accepted、negotiated 三层不可反向污染；protocol major/minor、descriptor/tool/media version、未知字段、重复 key、矛盾响应和部分响应均确定性处理，换 Hub/重连/重新认证使旧 negotiation epoch 失效。
88. Entropy 测试：硬件 RNG 未强就绪、DRBG reseed 失败、熵源故障和高并发 ID/secret 生成；不会回退 MAC/时间戳/普通 PRNG，失败时不启动安全配网、配对或生产认证。
89. 有线烧录身份测试：`confirmed/probable/ambiguous/conflict`、目标 board/hw/layout/compat/flash/security 不匹配、`.clawfw` manifest 缺失/签名失败和 identity 查询超时均按契约处理；写后 transaction/boot session/digest 与 nonce `BOOT_STATUS/SERVICE_STATUS` correlation 正确。
90. Diagnostic port 测试：超长/畸形/分片 line、nonce 重放、命令枚举、日志洪泛、stop/join 和 release allowlist；无法读取 secret、任意 NVS/内存或绕过恢复出厂，输出字段全部脱敏且有界。
91. 版本兼容矩阵测试：profile/HAL/partition/NVS/protocol/capability/tool/diagnostic 各版本独立演进；支持组合可启动/迁移，不支持组合 fail closed，禁止用单一“固件版本较新”替代兼容判定。
92. 诊断基数/背压测试：任意 URL/SSID/message ID/error string 不进入 metrics label；重复故障被聚合限流，串口/日志消费者阻塞不反压实时音频、输入或休眠 COMMIT。
93. Composition-root 测试：Service dependency manifest 无环，init/start/readiness 与 stop/deinit 顺序确定；禁止 Service 自取 board/global singleton、构造时起任务或隐式 init，任意 leaf 可用 Fake port 替换。
94. Health flapping 测试：瞬时 timeout、间歇 I2C/网络/存储失败和恢复成功脉冲不会反复注册/撤销 capability；安全故障立即收缩，普通能力只有达到失败/恢复窗口并通过重新自检才改变。
95. Shared-bus recovery 测试：SDA stuck、仲裁/timeout、codec/touch 同总线故障和恢复中断；先 quiesce 全部 borrower，旧 generation handle 失效，reprobe/readiness 后统一恢复，超预算保持 DEGRADED。
96. Configuration precedence 测试：编译默认、签名 manifest、量产、用户、Hub policy 和临时 override 的合法/非法覆盖、TTL、重启、并发更新及 observer apply 失败；不会出现半新半旧 snapshot。
97. Calibration provenance 测试：错误 board/hw revision、旋转、codec channel、批次、CRC/签名、越界值和原子提交断电；错误校准关闭能力或使用安全默认，上一确认 revision 可回滚。
98. Steady-state allocation 测试：DEVICE_READY 后 Input/Audio ISR、capture/playback、Display DMA、Power COMMIT 与 WDT 路径通用 heap 调用数为零；动态 owner quota/OOM/归还 deadline 和长稳碎片趋势符合 budget。
99. Audio clock drift 测试：不同温度/供电/时长的实际 sample rate、DMA sequence gap、ppm、overrun/underrun、WAV header/样本数/UI 时长一致性；超预算明确失败或标记完整性损坏。
100. Facade cutover 测试：legacy/new 每个 seam 的唯一副作用 owner、只读 shadow trace、quiesce/drain、state handoff、旧 generation 失效、失败回退与 facade 调用归零；不出现双输入、双播、双 ACK、双写或双删。
101. 并发内存模型测试：跨 core/task/ISR flag 与多字段 snapshot 在高频交错、generation 回绕和 stop/restart 下保持一致；新增 `volatile` 不作为同步，atomic/critical section/队列契约与 manifest 一致。
102. 优先级反转测试：低优先级 task 持 mutex 时 Audio/Input/Display 高优先级竞争的最大阻塞有界；mutex priority inheritance 生效，binary semaphore 不充当互斥，critical section 中无 driver/log/loop/callback。
103. Schema registry 测试：event/tool/status 显式 ID、生成 C/schema/vector 一致，unknown/deprecated/tombstone 行为正确；重排源码或新增板型不会改变已发布数值，删除 ID 不被复用。
104. 可复现构建测试：隔离 clean workspace 使用锁定 IDF/toolchain/dependencies/sdkconfig/resources 构建三硬件，功能内容 digest 符合 reproducibility 规则；SBOM、许可证、CVE、component hash 与签名 provenance 完整。
105. 供应链负向测试：dependencies.lock 漂移、组件 hash/来源变化、未审查生成字体/模型、未知许可证、高危 CVE 或本地缓存污染均阻止 release artifact 签名。
106. Fault-domain restart 测试：shared-I2C/audio/display/network/storage 在各生命周期阶段故障、borrower 卡死和 callback 迟到时，统一关闭 admission、drain、递增 generation、自检并恢复；旧 handle 不复活，无法 join 时隔离整个 domain 且不释放在用资源。
107. Restart operation outcome 测试：读操作、幂等写、外部 ACK、文件 commit 和音频 capture 在重启中分别得到 `CANCELLED/RETRYABLE/INTEGRITY_DAMAGED/UNKNOWN_OUTCOME`，未知外部副作用不自动重试或重复提交。
108. 端到端 deadline 测试：Boot、stop、restart、sleep prepare、configuration apply、provisioning 和 meeting finalize 的子步骤只消费父 deadline 剩余预算；嵌套重试不会重置 timeout，cleanup reserve 有界且最慢 phase 可诊断。
109. Schema expand/contract 测试：NVS、Storage 大对象、credential 和 calibration 的 stage/validate/commit/cleanup 任一点断电、空间不足、低电量与写放大超限均保留旧 generation；reader/writer version window 与有线降级阻止不安全回滚。
110. Degradation feedback 测试：麦克风、扬声器、存储、网络、时间与 sleep/wake 故障通过统一 reason 映射至屏幕/LED/音频/触觉或无屏路径；底层错误不泄漏，提示风暴受限，恢复只在 readiness/滞回通过后通知。
111. Core dump 生命周期测试：release 加密/禁用策略与 eFuse/build 一致，secret/PCM/表单 buffer 不可导出，认证、审计、容量/retention、恢复出厂/转移清理和低电量/启动非阻塞符合策略。
112. Evidence traceability 测试：每个 requirement 的 implementation/test/profile/evidence/digest/budget 完整且对应当前 artifact；缺失证据、错误 firmware digest、目标 profile 不匹配或 waiver 过期均使 Phase/release 门禁失败。
113. Restart reconciliation 测试：Display/Audio/Gateway/Alarm/Power 在各 phase 重启后只从 authoritative config/persistence/capability revision 重建，subscription/timer 各一份，desired/observed state 收敛；旧 mutex/task/queue/pointer/UI snapshot 不被复制或复活。
114. Electrical reset-safety HIL：上电、外部 reset、软件 reset、panic/WDT、brownout、deep-sleep enter/wake、持续按住 strap 和下载模式下采集功放/背光/电源轨/reset/CS 波形；未知 profile/revision 保持安全交集且毛刺不超过硬件预算。
115. DFS/clock-domain HIL：所有发布 CPU/APB 频点与 modem/light sleep 切换中持续验证音频 sample clock/序列、LCD QSPI/DMA、I2C/touch timeout、UART/diagnostic baud、timer/deadline 和 PM lock 泄漏；未验证组合不进入 capability。
116. Resource pressure 测试：制造 internal largest-block、DMA pool、queue、stack、storage、thermal/battery PRESSURE/CRITICAL，验证确定性降载与滞回恢复；关键控制、录音收尾和 commit emergency reserve 可用，无业务层板型分支或 OOM 重启循环。
117. HIL evidence integrity 测试：board/fixture/instrument/calibration/environment/script/raw-data/sample metadata 缺失或越界、证据 hash/签名不匹配、样本不足、自动 golden 覆盖、只保留最后一次 pass 与过期 flaky quarantine 均阻止 release。

### 8.2 三 profile 构建门禁

每个阶段都必须构建：

- `CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD=y`
- `CONFIG_MACLAW_BOARD_ECHOEAR_2ST=y`
- `CONFIG_MACLAW_BOARD_FANGTANG_4G=y`

三个配置必须从各自 clean build 目录构建，不能复用上一 profile 的 `sdkconfig`、CMake cache、生成资源或 component resolution。构建门禁记录：固件大小、IRAM/DRAM/PSRAM 使用量、编译告警、链接到的 board profile 及最终 board ID。任一板型构建失败时不得合并。指标必须与 profile 的 `runtime_budget` 比较，超过固件/内存预算或静态 GPIO/总线冲突时直接失败。

每个板型还必须生成并校验 manifest：选中的源文件、ESP-IDF component 依赖、sdkconfig overlay、partition table、嵌入资源、flash/PSRAM 最低要求、I2C/SPI/I2S host 占用。禁止未使用硬件组件被无条件链接进所有板型。

### 8.3 实机验收矩阵

三硬件均需验证：

- 使用同一 Bread Compact `feature_id` 测试集，三硬件的业务状态、终态、Gateway tool/capability、错误语义和恢复保证完全一致；profile-specific 差异仅限物理输入、布局、driver/transport trace 和资源预算。
- 冷启动、联网、断线重连、配对和重新配置。
- 唤醒词、单次录音、上传、远端回复和 TTS 播放。
- 会议开始、停止、上传、失败保留及重启续传。
- 处理中双击取消，且录音阶段双击不会误启动会议。
- 回复关闭和分页行为。
- 音量调整在三硬件均有可操作入口并实际影响播放；Fangtang 不再返回 `ESP_ERR_NOT_SUPPORTED`，EchoEar/Fangtang 无独立音量键时使用统一远端/菜单入口。
- 闹钟响铃、立即解除和手势消费。
- 音频播放结束无重复尾音，录音无通道错误。
- 配网二维码不被后台宠物/天气重绘覆盖。
- 前景回复不被迟到的 idle、天气或 Wi-Fi 更新覆盖。
- 连续运行、任务栈水位、堆内存和 LCD DMA 稳定性。
- 初始化失败或服务重启后无孤儿任务、锁、总线和 DMA buffer。
- fault-domain restart 后配置、订阅、timer、UI desired scene 与硬件 observed state 重新收敛，旧 operation/runtime context 不复活。
- 快速切换录音、回复、闹钟、配网时无 WDT、死锁或晚到画面。
- 重启后配对、会议续传、天气、闹钟和用户配置按 schema 正确恢复。
- 对外能力声明与实机能力一致，Hub 不会向不支持的设备投递不可消费内容。
- 取消后迟到的文字、音频、图片和完成事件不能影响下一次 interaction generation。
- App Interaction Task 在录音、播放、上传和显示 flush 期间仍能及时处理闹钟与取消。
- 板型/profile 错配进入安全模式，功放、电源轨和冲突输出 GPIO 未被驱动。
- 输入、取消、音频和显示延迟以及 heap/stack/固件指标均在 profile budget 内。
- SNTP 未同步、时间回拨和时区变化不会误触发闹钟或破坏超时。
- 非空会议存储在升级、降级、挂载失败和断电恢复中不被自动格式化。
- WAV 写入、恢复游标更新、分块上传和本地删除各提交点掉电后能够确定性恢复，不重复提交错误 chunk。
- Wi-Fi/Hub/codec/MultiNet/显示持续故障不会形成紧循环重试、内存持续下降或整机重启风暴。
- 屏幕、输入或音频任一故障时，SAFE_MODE 仍有独立可诊断反馈路径。
- 克隆 NVS、恢复出厂配置和重新配网不会造成多台设备共享同一 device ID。
- 配置的休眠时间窗到达后，设备在无阻塞 lease 时进入 profile 支持的目标深度，并在下一唤醒边界或更早闹钟准时恢复。
- Bread 可由已验证实体键唤醒；EchoEar 可由 BOOT 键从 deep sleep 唤醒，并可由 GPIO42/CST816 从 DISPLAY_OFF 或经验证的 light sleep 唤醒；Fangtang 由 GPIO0/timer 唤醒且正确恢复 ML307、LCD、电池/充电观测状态。未实测来源不得写入 capability。
- 休眠窗口内手动唤醒后 obey override，不立即反复入睡；一次唤醒 contact 不会兼作开始录音/会议的输入。
- 所有 wake source 配置失败时拒绝 deep sleep，设备不会进入只能断电恢复的状态。
- PM/DFS/tickless/battery ADC 仅在实际初始化和自检成功时生效，关闭 Kconfig 或注入初始化失败不会留下虚假能力或半初始化资源。
- 上电/reset/WDT/brownout/deep-sleep/下载模式全过程中，功放、背光、电源轨和冲突 GPIO 满足 electrical-safety manifest，无软件接管前毛刺风险。
- 所有发布 DFS/APB 频点及切换组合下，音频时钟、LCD DMA、触摸/I2C、诊断 UART 和 deadline 保持在 profile budget，PM lock 无泄漏。
- internal heap/DMA pool/queue/stack/storage/thermal 压力下按统一策略有序降载，关键控制、录音收尾和 commit reserve 不被普通工作耗尽。
- 多个休眠来源同时触发时只执行一个 transition；更早闹钟或新关键业务在 COMMIT 前能取消休眠。
- 短时间窗不会导致频繁 deep-sleep 重启，连续立即唤醒达到阈值后 schedule 自动熔断并保持可诊断状态。
- 闹钟和休眠计划共享 RTC timer 时不会互相覆盖；更早闹钟始终取得优先权，清除后恢复下一计划 deadline。
- GPIO0 按住唤醒及随后 reset 不进入不可控下载模式；若实机不满足，此 wake 组合不会出现在 effective capability。
- 所有永久后台任务和回调可被 Boot Coordinator 有界停止，LIGHT_SLEEP 恢复不会重复创建执行单元。
- NVS 满、旧版本或单 namespace 损坏时不会自动丢失 Wi-Fi、token、闹钟、schedule、会议恢复和校准；受控恢复出厂路径可审计。
- Gateway 长轮询能在 sleep PREPARE 预算内取消；休眠后 last-seen 超时只表示离线，不触发解除配对或凭据清除。
- Hub 重启、队列超限或媒体过期时，设备与产品能区分可重建状态和不可保证的 transient 消息，不出现假 ACK、静默误报成功或旧 boot 结果污染。
- 始终语音唤醒模式不会进入不兼容的 LIGHT/DEEP_SLEEP；深度节能模式会在休眠前干净停止 MultiNet，并明确提示休眠期间不能语音唤醒。
- 低电量阈值附近不会反复睡醒/写 flash；没有已验证 charger/button/timer 恢复源时不会进入不可恢复 DEEP_SLEEP，插入充电器后按滞回安全恢复负载。
- 远程设置/禁用休眠计划具备幂等和 capability gate；不合法或会造成不可恢复状态的 `sleep_now` 请求被拒绝且不改变当前状态。
- 量产配网会话具有随机认证、短 TTL、物理可取消和尝试限制；附近未授权设备不能观察或修改 Wi-Fi/EAP/Hub secret，配网结束后 AP/DNS/HTTP 不再可达。
- 重新配置失败保留原连接和 token；恢复出厂使用独立确认并能在任意掉电点幂等完成，不误删应保全的会议数据或克隆设备身份。
- Release 拒绝 HTTP Hub 和关闭 CA 校验；URL 规范化、hostname/SNI、私有 CA 与可信时间失败均不会静默降级或把 token 发往不同 origin。
- 高压力 TLS/显示/MultiNet/NVS 场景下，DMA/internal/PSRAM 内存类别和 cache-disabled 约束无违规；WDT 诊断能指出 owner/phase 且不会靠放宽超时掩盖卡死。
- 三硬件和异构 Fake profile 的 display/touch logical coordinate、rotation/safe mask/panel offset 与 PCM format/frame contract 一致，EchoEar 圆屏显示/触摸命中及 Fangtang 240×240 viewport/80 行 offset 均无退化。
- 硬件 mute/录音指示（若 profile 存在）与真实 capture 生命周期严格一致；无该硬件的板不虚假声明，业务逻辑不出现 LED/mute GPIO 分支。
- 高负载事件风暴下 cancel/alarm/wake/mute 的关键 reservation、sequence 和 sticky overflow 行为可验证，普通 telemetry 不会造成关键控制丢失或 App Interaction Task 饥饿。
- Hub 重连、重新认证或切换 origin 后生成新 negotiation epoch；旧会话工具/能力不能获得新授权，协议 major 不兼容时设备保持本地安全可用并给出恢复提示。
- 安全随机源未就绪/故障时不生成配网、配对、boot/operation 安全标识或发送生产凭据；恢复后生成值无跨 boot/批次碰撞。
- 有线烧录前后 identity/compat/layout/artifact digest 与 transaction correlation 一致；错误板型、错误分区和未签名 artifact 不会进入写入阶段。
- HIL 结果可追溯到具体设备、fixture、校准有效仪器、环境、脚本和原始数据；golden 变更与 flaky 隔离均有独立审批、期限和完整 attempt 历史。
- I2C 等共享总线卡死恢复不会只重置单个子 HAL；所有 borrower 同步降级、旧 handle 失效并在 reprobe/readiness 后统一恢复，无恢复风暴或 capability 抖动。
- 配置和校准更新在任意掉电/observer apply 失败点保持完整 revision；远端配置无法越过硬件、安全或隐私下限，错误硬件校准不会生效。
- 长会议录音的样本数、WAV header、上传时长和单调计时在 profile ppm/gap 预算内；超限不会静默宣称录音完整。
- facade 切换与回退时每个副作用始终只有一个 owner；shadow 只读比较，旧任务/handle/global writer 在 ownership handoff 后全部失效。
- 多核/ISR 压力下 interaction、cancel、meeting、welcome、HTTP 等迁移状态无基于 `volatile` 的数据竞态、ABA 或多字段撕裂，优先级反转在预算内。
- 三硬件 release artifact 可从锁定输入 clean build 重现，并附带匹配的 SBOM、许可证/CVE、component/resource hash 和签名 provenance。
- 三硬件从已配对 Hub 得到相同语义的适用版本 metadata；设备不直连 GitHub、不接收 firmware URL、不下载 firmware bytes，也没有 partition 写入或远程安装入口。
- 版本比较、stable/beta channel、critical 提醒、稍后/忽略当前版本和检查退避在三 profile 上一致；EchoEar 仅以圆屏 Renderer 保留呈现差异。
- GitHub Release 为三 profile 提供不可变 ClawMate Maker `.clawfw`、manifest、SBOM/provenance 和 release notes，日常 `appUpdate` 只写 App，merged raw image 仅供恢复出厂；draft/缺件/被覆盖 asset 不可进入 Hub catalog 或刷机工具。
- 官方 ClawMate Maker 在写前核对 `.clawfw` 签名、size/hash、三方 allow-list、identity confidence、真实 `layoutFingerprint`、security baseline 与 compat，默认保留 NVS/model/storage/会议数据；错板、错布局或不可兼容 schema 不进入写入。
- 刷机开始后提示禁止断电，写后完成 ROM hash/readback、实际 firmware digest 与 nonce `BOOT_STATUS/SERVICE_STATUS` correlation；单 app 无自动回滚风险由 `RECOVERY_REQUIRED` journal、上一 stable `.clawfw` 的 schema 兼容恢复演练和更严格 stable 门禁控制。

EchoEar 额外验证：

- CST816 单击不会被重复 contact 误判为双击。
- 原生双击在待机和处理中均可靠。
- 圆屏边缘无文字裁切、方形背景露出或旧帧残留。
- 宠物动画帧率、曲线文字和 QSPI 提交无退化。
- ES7210 可靠声道、DC 处理和 ES8311 音量正确。

Bread 额外验证：

- 激活键短/双/长按边界稳定。
- 音量键普通界面调音量、回复界面翻页。
- 直连 I2S 播放停止后不重复最后一个 DMA block。

Fangtang 额外验证：

- 240×240 NV3023 viewport 与 GRAM Y offset=80 在启动、待机、回复、会议、闹钟、配网和错误页均无纵向错位、旧行残留或越界提交。
- 单键短/双/长按及启动网络选择窗口无事件穿透；窗口外手势严格产生与 Bread 相同的业务 intent。
- 五行回复自动分页完整、可读且不会因没有音量键而丢失页面；图片/caption、进度和错误必选字段均可达。
- 软件/远端音量 0–100 实际作用于 direct-I2S 输出并持久化，当前 `ESP_ERR_NOT_SUPPORTED` 路径清零。
- Wi-Fi/ML307 切换、断线恢复、Gateway poll、语音上传、会议分块上传、tool result 与时间同步不改变业务幂等、deadline、cursor 或恢复语义。
- battery ADC 与 charge GPIO 经过校准、滤波和 Battery Policy；ML307 power/guard/UART 在 boot、sleep、wake、WDT、brownout 和切换失败时满足 electrical-safety/lifecycle 契约。

## 9. 代码审查门槛

完成主计划 Phase 9 时必须达到：

1. `main.c` 和共享 `app/domain/services` 中 `CONFIG_MACLAW_BOARD_*` 命中数为 0。
2. 共享业务代码引用 GPIO/I2C/I2S/SPI/LCD/触摸/codec 驱动符号的命中数为 0。
3. Bread、EchoEar 和 Fangtang 的业务状态机实现数量为 1。
4. 通用 capture/playback、指令 WAV、会议领域状态机和唤醒词监督的共享实现数量各为 1，且职责边界互不倒置。
5. 板级目录只包含硬件驱动、信号调理、物理事件产生和具体屏幕渲染。
6. 新增 Fake HAL 不需要修改任何业务源文件。
7. 三个实机验收矩阵全部通过；EchoEar 圆屏、Bread 240×320 与 Fangtang 240×240/Y offset=80 显示均无功能性退化或公共 Scene 字段缺失。
8. 业务目录只 include Device/Platform 公共 API，不直接引用任何 `*_hal_ops_t`、`*_port.h`、`board_hal_get()`、ESP-IDF 驱动或具体存储/网络实现。
9. 只有 Display Task 调用 panel/Renderer；只有 App Interaction Task 调用交互状态机。
10. HAL 全部实现完整生命周期，故障注入证明部分初始化可逆序回滚。
11. 共享总线和 DMA/PM 资源均有唯一所有者，板级子 HAL 不重复创建或提前销毁。
12. `app_ui_model_t` 不保存临时 cJSON、QR handle 或调用方像素指针。
13. 网关 `clientCapabilities` 由真实设备/固件能力生成，不存在固定的板型无关虚假声明。
14. Display HAL 不写产品 NVS；所有共享 NVS 数据有 schema version 和迁移策略。
15. 每个 board profile 具有独立构建 manifest，未使用 controller/codec 组件不进入该板固件。
16. 所有异步请求、结果和 UI/音频副作用均携带 operation ID/generation；无裸 bool/TaskHandle 归属判断。
17. Device API 的线程、阻塞、取消和所有权契约完整，App Interaction Task 不执行长时间 I/O。
18. Boot Coordinator 具有 readiness、DEGRADED、SAFE_MODE 和受控重试；只有 DEVICE_READY 后开放输入。
19. 静态能力、编译功能与运行时健康生成本地 effective capabilities；Hub accepted 与认证授权只能进一步求交生成 negotiated session capabilities，不能反向扩大或改写本地能力。
20. profile 校验覆盖 board revision、ESP target、flash/PSRAM、partition 和硬件 manifest；错配保持输出安全。
21. 每个 profile 的 runtime budget 全部通过，任务/锁 owner 和 core/栈类型有唯一清单。
22. 单调时钟与墙上时钟职责分离，并可使用 Fake Clock 确定性测试。
23. 固定 16 MiB 单 factory/model/storage layout、NVS schema、烧录保留策略及升级/降级矩阵已冻结；设备不声明 OTA capability，Update capability 只包含版本检查和提醒。
24. UI 文本符合 UTF-8、glyph fallback 和硬件无关文案契约。
25. 自检和量产模式无开放调试后门，失败结果正确收缩能力或进入安全模式。
26. Device/HAL API 只包含通用设备原语；会议、固定时长指令、闹钟和 Hub 协议仅存在于共享领域 Service。
27. 会议及资源业务不引用 SPIFFS 路径或 `FILE *`；Storage contract 支持可恢复的原子提交、空间不足和只读降级。
28. Wi-Fi/HTTP/Hub/配网 portal 与设备身份均有明确平台 Service owner，板级驱动和 Renderer 的网络/凭据命中数为 0。
29. 所有可重试组件具备 retry/reboot budget、退避、抖动和 circuit breaker；故障注入无 retry/reboot storm。
30. SAFE_MODE 的反馈通道不依赖已判故障组件，且在所有 profile 上有可执行的诊断路径。
31. 断电一致性测试覆盖音频、恢复 metadata、chunk cursor、远端确认和本地删除的每个提交点。
32. Identity 根不依赖可克隆普通 NVS；boot session 和 operation ID 的唯一性在重启/克隆场景通过测试。
33. Fangtang 第三正式 profile 完整对齐 Bread 功能且共享业务零分叉；第四 Fake/Reference profile 使用与三硬件不同的能力组合，接入时 `app/`、`domain/` 和共享 `services/` 的业务实现零修改。
34. 屏幕熄灭与系统 sleep 状态在命名、状态和实现上分离；板级 Renderer 不自行决定 LIGHT/DEEP_SLEEP。
35. 指定时间休眠由共享 Sleep Schedule Service 实现，具备可信时间、跨午夜、DST、闹钟 deadline 合并和 revision 防迟到机制。
36. 所有深层休眠均通过 power lease 与 `PREPARE → COMMIT` 事务；会议、上传、闹钟、配网和持久化不会被非事务性截断。
37. 每个 profile 的 timer/button/touch wake capability 与实机一致；至少一个物理/定时唤醒源成功 arm，否则拒绝 deep sleep。
38. 按键/触摸 wake 只产生 `DEVICE_EVENT_WAKE`，guard/drain 后的新输入才可转换为业务意图。
39. deep sleep 按新 boot 恢复，light sleep 按 suspend/resume 恢复，两条生命周期不可混用。
40. 各 power state 的功耗和恢复延迟满足 profile budget，未验证模式不对外声明。
41. EchoEar 当前 board revision 的 GPIO42 触屏唤醒只用于 DISPLAY_OFF/已验证 LIGHT_SLEEP；DEEP_SLEEP 仅使用 GPIO0/RTC timer，除非新板修订提供 RTC-capable touch wake 电路。
42. RTC timer 的 drift、boot lead、闹钟触发容差和时间可信度降级均有 profile 数据与实机测试。
43. Kconfig 只能控制实现是否编译，不能直接生成 effective capability；PM、DFS、tickless、ADC 和 wake source 均以运行时初始化/自检结果为准。
44. Power transition 全局唯一且可取消，COMMIT 前重新校验 lease、闹钟、schedule revision 和 armed wake mask。
45. schedule 具有确定性优先级、半开时间边界、显式 disable、最短有效休眠和不支持 depth 的拒绝/降级契约。
46. 高频 DISPLAY_OFF/短睡诊断不会造成 NVS 写放大，sleep history 存储频率在 flash endurance 预算内。
47. Display HAL、Power HAL 与 Wake HAL 的 owner 不重叠：panel command、背光/电源轨、sleep entry、wake-source arm 各有唯一写入者。
48. 全部 task/timer/ISR/event handler 均登记在 Task Registry，具备有界 stop/join/drain/unregister 契约。
49. Boot fatal path 已按必选/可降级/安全错配分类，连续同阶段失败会进入 SAFE_MODE 而非无限重启。
50. RTC timer 只有 Wake Deadline Service 一个 owner，Alarm/Schedule 等消费者不能直接调用 `esp_sleep_enable_timer_wakeup()` 覆盖彼此。
51. GPIO0 作为 wake source 的 strapping 风险通过实机门禁；失败 profile 不声明该 deep-sleep wake 组合。
52. Alarm 的最早 deadline、SNTP 重映射、DST 去重和持久化恢复均由共享服务实现，不依赖每板复制的秒级轮询任务。
53. NVS 初始化失败不会调用无条件整区擦除；factory blank、只读恢复、逐 namespace 迁移和认证恢复出厂有互斥且可审计的入口。
54. Gateway poll/HTTP worker 具备有界 quiesce/cancel/join；进入休眠不等待完整长轮询，也不伪造未处理消息的 ACK。
55. 下行消息的 durability、TTL、容量、ACK 和跨 boot 语义已形成协议契约；当前 Hub 内存队列的限制不会被产品文案误报为离线必达。
56. DEEP_SLEEP 新 boot 会隔离旧 session/generation/cursor/pending result；LIGHT_SLEEP 保持 session 并通过 resume barrier 恢复。
57. Always-on Wake Word 与 sleep depth 的互斥关系由 Power Service 和 capability 管理；板级唤醒词任务不能绕过 lease 阻止或进入休眠。
58. 低电量策略使用校准、连续样本和滞回；无已验证 charger/button/timer 恢复源时拒绝不可恢复 DEEP_SLEEP，低压 flash 写入受预算约束。
59. Sleep Schedule 等领域工具由通用 Gateway dispatcher 聚合，具备 capability gate、schema、idempotency、deadline 和跨重启契约，不进入 Device/HAL API。
60. Provisioning Service 是 AP/STA、DNS/HTTP、配置事务和凭据清零的唯一 owner；量产配网具备认证加密、TTL、限流、物理取消和完整 stop/join，Input/Display HAL 不处理 secret。
61. 重新配置与恢复出厂是不同意图；旧配置/token 在新配置确认前有效，恢复出厂按数据等级幂等执行并明确未上传会议与设备根身份策略。
62. Release 仅接受规范化 HTTPS Hub origin，TLS/hostname/SNI/CA 和企业 EAP CA 校验不可静默关闭或降级；secret 不经日志、trace、crash dump 或普通 getter 泄漏。
63. HAL 跨模块结构均通过 `struct_size + abi_version` 校验并只允许尾部兼容扩展；版本错配、未知枚举或 capability/ops 不一致安全失败。
64. DMA/internal/PSRAM/RTC 内存类别、alignment、cache-off 与 fence 生命周期有可执行契约；高压场景不以总 free heap 掩盖 largest-block 或错误内存类别。
65. 关键 task 具有 heartbeat/WDT budget 和 RTC 故障上下文；WDT/reset 可定位 owner/phase 并触发同阶段熔断，发布配置不靠放宽全局 timeout 通过门禁。
66. Display/Input/Audio 使用结构化、版本化能力 descriptor；坐标变换、圆屏 mask、像素格式、PCM/frame alignment 差异停留在 HAL/共享转换层，业务代码不新增板型判断。
67. Privacy/Feedback capability 与实机一致；物理 mute 优先于录音意图，录音指示覆盖硬件真实 capture 窗口，业务层只使用标准 privacy/feedback 状态。
68. 公共 Device/Platform header 不依赖 ESP-IDF/FreeRTOS 类型；稳定 status、毫秒/单调 deadline、错误映射、失败输出和 opaque handle/callback drain 契约均通过主机测试。
69. TLS 时间引导有独立 trust state、构建下界/受信 RTC 规则和不可自证门禁；时间未可信、回拨或异常漂移时不发送生产 token、不关闭证书校验。
70. 所有网络/配网/媒体/tool 输入受统一 parser contract、checked arithmetic、消息/会话/设备配额及 fuzz corpus 保护；解析失败在产生任何持久化、播放、调度或 ACK 副作用前结束。
71. 会议录音有 consent/indicator、retention、用户删除、受控导出与加密状态声明；恢复出厂不会把普通删除误报为安全擦除。
72. capability 通过 immutable/versioned snapshot 发布，operation 绑定单一 revision，运行中能力收缩不存在多指针 TOCTOU。
73. 解绑/设备转移使用本地 token 隔离、可恢复 revoke tombstone 和 credential generation；旧 token/NVS 镜像不能恢复访问。
74. Provisioning 不以显示为必选依赖；每个 profile 都有唯一/可轮换的可信引导及进入、取消、超时和恢复路径，量产不存在固定全产品默认密码。
75. Device Event 使用版本化 envelope、明确 payload ownership/source/sequence/boot-operation correlation；ISR 不分配 heap，关键控制 reservation 与 sticky overflow/fairness 策略通过压力门禁。
76. effective、Hub accepted 和 negotiated session capability 三层分离；protocol/capability/tool/media schema 显式协商并绑定 negotiation epoch，远端回显不能制造本地能力或跨会话授权。
77. Entropy Service 是全部安全随机值的唯一入口；强随机未就绪或 reseed 失败时 fail closed，不使用 MAC、时间戳、计数器或普通 PRNG 替代。
78. firmware identity/diagnostic USB 端口由 Platform Service 与 Task Registry 管理，parser/命令/输出有界、release allowlist 和脱敏，不形成隐藏写入或数据导出后门。
79. 有线烧录在写前验证签名 artifact 与 product/board/hw/layout/compat/partition，写后以 transaction/boot session/digest 及分层 readiness 确认；identity 不确定时默认拒绝。
80. profile、HAL、partition、NVS、protocol、capability、tool 与 diagnostic 版本分别维护兼容矩阵；支持/拒绝/迁移路径不依赖模糊的单一固件版本比较。
81. 日志、metrics 和 trace schema 有界且可背压隔离；高基数字符串不作 label，诊断消费者阻塞不影响音频、输入、电源 COMMIT 或 WDT。
82. 唯一 composition root 按无环 dependency manifest 显式注入所有 Service/port；Service 不使用板级/全局 locator、不隐式 init，启动与停止顺序可静态检查。
83. health/capability 具有失败与恢复滞回、最短保持、cooldown 和重新自检门禁；瞬时故障不造成 UI/Hub capability flapping。
84. 共享总线恢复由 Resource Manager 统一 quiesce/recover/reprobe，并使旧 generation borrower handle 失效；子 HAL 不私自 reset 影响其他器件。
85. Configuration Service 是 effective config 唯一 writer，来源优先级、权限、TTL、revision 与 apply/rollback 明确；远端策略不能覆盖安全/电气/隐私限制。
86. 校准值具备 provenance、硬件/通道绑定、完整性、范围和原子 revision 回滚；错误或缺失校准不会静默启用依赖能力。
87. DEVICE_READY 后实时关键路径只使用预分配内存；动态任务有 owner quota/reservation，长期运行无持续 alloc/free 碎片化。
88. Audio HAL/Service 提供实际 sample-clock 与 DMA sequence/timestamp；长录音按提交样本数计算并检测 ppm/gap，完整性超预算不会作为正常成功上传。
89. 每个 legacy→new seam 有唯一副作用 owner、只读 shadow、quiescent cutover/state handoff/回退和 facade 删除证据；release 不允许远端任意切回旧实现。
90. concurrency manifest 覆盖跨 task/core/ISR 状态；`volatile` 不作为同步，atomic/critical section/mutex/queue 的读写者、内存序和上下文明确。
91. lock manifest 证明实时高优先级路径没有无界优先级反转；mutex 使用 priority inheritance，binary semaphore/critical section 用途正确且持有时间满足 budget。
92. event/tool/status schema registry 使用稳定显式 ID、版本与 tombstone，并生成代码/测试；已发布编号不因重排、删除或新硬件复用。
93. 每 profile release artifact 具有锁定 toolchain/IDF/dependency/resource 输入、SBOM、许可证/CVE、digest 和签名 provenance，并通过 clean-build reproducibility 门禁。
94. 每个 Service/共享资源具有 fault domain；restart 完整执行 quiesce/stop/reinitialize/self-test/readiness，旧 generation 与迟到 callback 不可复活，shared owner 故障不只重启单个 borrower。
95. Boot、stop、restart、sleep prepare、config apply、provisioning 和 meeting finalize 传播同一绝对 deadline；分层 timeout 不累加突破总预算，cleanup reserve 和超时隔离策略可验证。
96. NVS/Storage/credential/calibration 迁移采用可恢复 stage/validate/commit/cleanup 与 reader/writer version window；不可逆点不会破坏已承诺的有线回滚或静默丢失旧数据。
97. 用户降级反馈使用稳定 reason，经 profile Feedback adapter 映射到现有输出；不显示板型/驱动错误，具备风暴抑制、无屏路径和 readiness 后恢复通知。
98. 生产 core dump 的启用、加密、敏感区排除、认证导出、容量/retention 与恢复出厂/转移清理均有门禁，dump 不阻塞启动、低电量或覆盖用户数据。
99. Phase/release 的 requirement→implementation→test→profile→evidence→artifact digest 可追踪；无证据不算通过，waiver 有 owner、补偿控制和有效期且过期自动失败。
100. restartable Service 具有 authoritative/durable/ephemeral state 与 reconciliation 契约；恢复后 subscription/timer/desired hardware state 唯一且 observed state 已收敛，不复制旧 runtime context。
101. 每 profile electrical-safety manifest 覆盖 ROM reset、bootloader、app、WDT/brownout、deep sleep 和下载模式；软件介入前的高风险输出由硬件默认态保证，未知 revision 不驱动非公共安全输出。
102. DFS/PM 的 clock-domain、频率范围、lock owner、切换 barrier 和 peripheral reconfiguration 可执行；每个发布频点的 Audio/Display/Input/Diagnostic/time 行为具有 HIL 证据。
103. Resource Pressure Service 统一聚合资源水位并按 NORMAL/PRESSURE/CRITICAL 降载；关键控制、数据收尾和持久化具有 emergency reserve，板级 HAL 不复制业务降级策略。
104. HIL 证据含设备/fixture/仪器校准/环境/脚本/原始数据/样本与不确定度并防篡改；golden 更新独立审批，flaky 重跑不抹除失败历史。
105. 固定 16 MiB 产品明确不实现设备端 OTA：保留单 factory/model/storage layout，不新增 A/B/staging，不牺牲会议与资源存储；capability/tool/schema 中不存在安装能力。
106. 更新信息固定使用 `GitHub Release → Hub Update Catalog → device metadata`；Hub 绑定真实设备/profile/hw/layout，设备不接收 firmware URL/bytes，检查失败不影响本地业务。
107. 三 profile 使用同一 Update Service、状态与提醒策略，支持 `update.check/status/remind_later/dismiss_version`；圆屏、矩形屏和单键差异只在 Renderer/Input Binding。
108. GitHub workflow 为三 profile 产生不可变 ClawMate Maker `.clawfw`、manifest、SBOM/provenance 和 release notes；`appUpdate/fullInstall`、`files/eraseRegions/writeOrder`、`layoutFingerprint/reservedRegions` 明确，merged raw image 仅供恢复出厂，采用 draft→完整回读→publish。
109. 官方 ClawMate Maker 从 GitHub 下载并完整校验 `.clawfw`、三方 allow-list、identity confidence、真实 `layoutFingerprint`/security baseline；默认保留 NVS/model/storage/未上传会议，恢复出厂是独立确认模式。
110. 单 app 无自动回滚的风险由更严格 stable 发布门禁、正常模式 maintenance、供电/USB 检查、ROM readback、nonce `BOOT_STATUS/SERVICE_STATUS`、`RECOVERY_REQUIRED` 和上一稳定 `.clawfw` 的 schema 兼容恢复演练控制。
111. release/bundle 签名、Secure Boot/Flash Encryption/anti-rollback（若启用）具有明确密钥/eFuse/RMA 生命周期和 CI/工具 golden vectors；产品文案不得宣称设备可自动更新或自动回滚。

上述条目与业务附录统一使用 `UPD-001`～`UPD-007` requirement ID；registry 中只能有一条 requirement 记录和多份 implementation/test evidence，不再保留 `OTA-xxx` 活跃门禁。

可在 CI/本地加入架构检查脚本，对共享目录执行禁止符号扫描，防止后续把硬件特判重新带回业务层。

## 10. 风险与控制措施

### 10.1 大文件拆分引发行为回归

控制：采用 facade 渐进迁移；一次只替换一个子系统；每阶段同时构建和验证三硬件，不直接整文件重写。

### 10.2 EchoEar 圆屏显示退化

控制：迁移的是调用边界，不重新设计视觉；保留圆屏 Renderer 和现有 DMA/双缓冲策略；使用 Phase 0 基准照片逐界面对照。

### 10.3 输入手势语义变化

控制：原始 contact 处理留在 Input HAL；业务意图映射单独测试；重点覆盖 CST816 重复 contact、原生双击和闹钟按下沿消费。

### 10.4 音频互斥与唤醒词竞态

控制：共享 Audio Service 成为唯一所有权仲裁者；规定锁顺序；为 capture、playback、wake pause/resume 和 setup stop 建立状态转换测试和超时日志。

### 10.5 过度抽象妨碍板级优化

控制：HAL 定义稳定语义，不抽象每个寄存器或绘图原语；允许板级 Renderer 自主使用 framebuffer、DMA、dirty region 和控制器专用命令。

### 10.6 能力降级被误当错误

控制：清晰区分 Bread 公共必选功能、非公共物理增强和 Fake/尚未正式支持硬件的可选能力。三款正式 profile 不得对 Bread 功能母版返回 `DEVICE_STATUS_NOT_SUPPORTED`；实现缺失是发布阻断缺陷。`DEVICE_STATUS_NOT_SUPPORTED` 仅用于 Fake/Reference、未来尚未进入正式集合的 profile 或明确不属于公共基线的物理增强。正式硬件的临时驱动/器件故障应返回稳定的 unavailable/degraded reason，由共享服务执行恢复、替代反馈或安全拒绝，不能伪装成静态不支持。

### 10.7 部分初始化导致资源泄漏

控制：生命周期拆分为 init/start/stop/deinit；每个分配点可故障注入；统一逆序回滚；禁止 init 中留下无法停止的匿名任务。

### 10.8 共享总线被多个 HAL 重复管理

控制：Resource Manager 是总线和 PM/DMA 资源的唯一所有者；子 HAL 只能借用句柄；销毁前校验借用者全部停止。

### 10.9 异步显示引用已释放数据

控制：Display Service 拥有 immutable snapshot；图片、QR、字形和字符串深拷贝或引用计数；DMA fence 完成后才释放提交缓冲。

### 10.10 事件队列拥塞造成关键输入丢失

控制：控制事件与可合并遥测事件分级；明确容量和满队列策略；记录丢弃指标；对闹钟解除和取消保留高优先级通道。

### 10.11 协议能力与硬件能力漂移

控制：握手能力从 profile 和编译特性自动投影；解析 Hub 回显；CI 使用多种 Fake profile 验证不会声明不存在的能力。

### 10.12 持久化职责泄漏到驱动

控制：产品 NVS 统一由 Persistence Service 管理；板级校准使用独立 namespace；架构扫描禁止 Display/Input/Audio HAL 访问产品 key。

### 10.13 迟到异步结果污染新交互

控制：统一 operation ID/generation 和 cancellation token；完成事件只允许一次提交终态；网络、音频、图片和 UI 全链路校验有效 generation。

### 10.14 长操作阻塞唯一交互任务

控制：Device API 明确同步/异步和最大阻塞；录音、播放、上传和 flush 使用 worker/service；App Interaction Task 只做短状态转换。

### 10.15 板型错配损坏硬件

控制：启动前校验 profile/manifest/revision/target/资源；仅执行安全只读探测；确认前不驱动功放、电源轨和冲突输出；烧录工具二次校验。

### 10.16 重构后实时性或内存退化

控制：每个 profile 固化 runtime budget；CI 比较静态大小，HIL 测量 deadline、heap/largest block、stack 和队列；超过门槛不得合并。

### 10.17 升级或降级破坏会议录音

控制：partition layout 与 schema 版本化；烧录默认保留 storage；非空分区挂载失败不格式化；升级、降级和断电恢复均纳入门禁。

### 10.18 墙上时钟变化破坏超时或闹钟

控制：deadline 只用单调时钟；闹钟依赖已同步墙上时钟；Fake Clock 覆盖回拨、时区和 DST。

### 10.19 量产/诊断能力形成安全后门

控制：主动自检只在受控模式运行；明确认证、进入/退出、超时和发布禁用策略；报告脱敏且不开放常驻网络调试入口。

### 10.20 Device API 被业务用例污染

控制：HAL/Device API 只表达通用 capture、playback、display、input、power 原语；会议、固定时长录音、闹钟和 Hub 协议放在共享领域 Service；CI 扫描接口名和依赖方向。

### 10.21 存储实现泄漏与断电不一致

控制：Storage Service 隔离 SPIFFS/LittleFS/SD/Fake；使用临时对象、`flush + fsync`、原子 commit 和带校验的恢复记录；对每个提交点执行 brownout 故障注入。

### 10.22 重试、熔断与重启风暴

控制：每组件固定 retry budget、指数退避、抖动、冷却和 circuit breaker；整机重启单独设置窗口预算；长稳测试监控堆碎片、WDT 和初始化次数。

### 10.23 SAFE_MODE 依赖故障组件

控制：profile 声明按优先级排列的独立反馈通道；故障注入逐个屏蔽显示、输入、音频、存储和网络；最终保留工具可读最小故障码且不自动擦除/重启。

### 10.24 设备身份重复或操作 ID 冲突

控制：稳定身份从芯片/受保护量产身份派生，普通 NVS 只存诊断副本；boot session 使用硬件随机源，operation ID 组合 session 与计数/nonce；覆盖 NVS 克隆和快速重启测试。

### 10.25 运行时能力变化与 Hub 认知不一致

控制：本地 effective capabilities 立即收缩；远端认知仅通过协议已有且省略 `bootSessionId` 的运行时 refresh handshake 更新，并验证不会重放启动欢迎；未定义新协议前禁止实现假定的即时 capability update。

### 10.26 定时休眠在错误墙上时间触发

控制：只有可信墙钟执行日历 schedule；单调时钟负责 prepare deadline；Clock Service 统一处理 SNTP 校时、时区、DST、跨午夜和 schedule revision，时间未知时保持唤醒。

### 10.27 休眠截断会议、上传或持久化

控制：会议、上传、闹钟、配网、Display DMA、Storage/NVS commit 等持有 power lease；进入休眠使用两阶段事务，失败逆序恢复，超过 grace 时默认取消睡眠而非破坏数据。

### 10.28 触屏/按键唤醒后误触发业务

控制：Wake HAL 与 Input HAL 分离；首次 wake contact 转换为 `DEVICE_EVENT_WAKE` 并 drain；guard window 后的新手势才进入业务，显式配置后才允许 wake-and-act。

### 10.29 唤醒源不兼容导致设备无法恢复

控制：profile 按 sleep depth 声明经过实机验证的 timer/GPIO/touch wake matrix；进入前读取实际 armed mask，全部失败则拒绝 deep sleep；禁止仅依据 GPIO 编号推断 RTC wake 能力。

### 10.30 休眠后立即唤醒循环

控制：进入前清除 stale IRQ，校验 wake pin 当前电平、上下拉和 touch controller 状态；记录连续短睡计数并熔断 schedule，回到 ACTIVE/DEGRADED 提示诊断。

### 10.31 Deep sleep 与 light sleep 生命周期混用

控制：deep sleep 视为新 boot，重新生成 boot session 并走完整 Boot Coordinator；light sleep 使用严格对称 suspend/resume，禁止重复创建任务、mutex、总线或 framebuffer。

### 10.32 Kconfig 声明被误当已实现能力

控制：编译开关、ESP-IDF PM 前提、运行时初始化、自检和 profile 实机证据分层求交；缺少 `esp_pm_configure`、ADC port 或 wake-source 实现时 capability 必须为 false，文档/握手不得按 Kconfig 自动宣称支持。

### 10.33 并发休眠请求与 COMMIT 竞态

控制：Power transition 使用唯一状态机和 operation generation；COMMIT 前二次读取 lease、下一闹钟、schedule revision、wake mask；schedule/idle/低电量/用户请求只能合并或排队，不能并行 prepare。

### 10.34 过短休眠导致功耗反增或重启循环

控制：profile 声明各 depth 的 break-even/min sleep duration；扣除 prepare、boot lead 和 resume latency 后不足门槛则保持较浅状态；连续短睡触发 circuit breaker。

### 10.35 电源诊断造成 Flash 写放大

控制：普通 transition 仅更新 RAM ring/counter，重要异常和批次 checkpoint 才写持久化；为每日写入次数和预计 flash endurance 设置预算与测试。

### 10.36 深睡前日志/Flash 依赖违反 COMMIT 契约

控制：COMMIT 前完成最后一次可失败持久化和诊断快照；COMMIT 后只执行 IRAM/RTC/GPIO 所需最小路径，不分配 heap、不调用普通日志或 Service callback；`enter(DEEP_SLEEP)` 返回视为失败并回滚。

### 10.37 Display/Power/Wake 同时拥有同一控制动作

控制：Display HAL 只提交 panel command，Power HAL 拥有背光、电源轨、PM 和 sleep entry，Wake HAL 只配置/解析 wake source；Resource Manager 对 GPIO/RTC hold 做唯一 owner 校验，CI 扫描重复写入者。

### 10.38 GPIO0 唤醒与启动绑带冲突

控制：实机覆盖持续按住和唤醒后 reset；profile 将 strapping 风险纳入 wake matrix；不能保证产品行为时关闭 GPIO0 deep-sleep wake，使用 RTC timer 或新硬件唤醒脚。

### 10.39 多个消费者覆盖 RTC Timer

控制：Wake Deadline Service 成为唯一 timer owner；Alarm/Schedule/维护任务只提交版本化 deadline；原子选择最早项并在 wake 后幂等消费/重算。

### 10.40 永久任务阻碍休眠、回滚和板级扩展

控制：Task Registry 管理 task/timer/ISR/event handler；每个执行单元有 stop token、join/drain/unregister 和 timeout；超时隔离资源，禁止直接 deinit。

### 10.41 无条件 ESP_ERROR_CHECK 形成 Boot Loop

控制：按根因区分 fatal、DEGRADED 和 SAFE_MODE；RTC boot record 记录失败阶段和 attempt；稳定运行后再清零，同阶段重复失败熔断而非继续 panic/reboot。

### 10.42 闹钟轮询与墙钟校正导致漏响或重复响

控制：Alarm Service 发布最早 wake deadline；ACTIVE 使用单调 timer，sleep 使用 RTC timer；SNTP/时区变化重新映射，DST 重复时按 ID/epoch 去重，clear/snooze 原子更新持久化和 deadline。

### 10.43 NVS 初始化失败触发整区数据擦除

控制：移除 `NO_FREE_PAGES/NEW_VERSION_FOUND → nvs_flash_erase()` 自动路径；区分 factory blank、可迁移旧版本、未知新版本和物理损坏；已有数据先只读保全并进入恢复/SAFE_MODE，擦除只允许由显式恢复出厂或认证工装执行。

### 10.44 深睡与 Hub 在线/队列语义不一致

控制：接受 last-seen 超时判离线，不把联网 presence 设为睡眠硬前提；定义 Hub 队列的 durability、TTL、容量、ACK 和跨 boot 规则。latest-wins 状态由 handshake 重建，关键命令在 durable queue 交付前不承诺深睡离线必达。

### 10.45 长轮询或 HTTP worker 阻塞休眠事务

控制：Gateway Service 提供 cancel-inflight、quiesce、join 和 resume；PREPARE 使用有界超时，无法停止则逆序回滚 ACTIVE。任务不得以无 stop token 的永久 `while(true)` 形式存在。

### 10.46 旧 boot 的消息、工具结果或 cursor 污染新会话

控制：DEEP_SLEEP 生成新 `bootSessionId` 并失效旧 operation generation；消息/tool result 使用 message ID、idempotency key 和 session policy 去重，旧会话迟到结果不能提交新 UI/音频终态。LIGHT_SLEEP 通过同 session resume barrier 恢复。

### 10.47 Always-on 语音唤醒与 LIGHT/DEEP_SLEEP 冲突

控制：Wake Word Service 持有最大允许 power state 的 lease；休眠前 stop/join 并释放 I2S、模型和 PM lock。产品策略明确“持续语音唤醒”与“深度节能”二选一；没有 ULP/外部低功耗语音硬件时不声明深睡语音唤醒。

### 10.48 低电量策略造成不可唤醒或棕断写损坏

控制：使用校准、连续样本、滞回和负载感知状态机；低压限制峰值负载且仅允许一次关键 checkpoint。DEEP_SLEEP 前必须 arm charger/power-good、独立按键或 RTC 复检中的至少一个已验证源，否则保持可恢复保护状态。

### 10.49 远程休眠工具绕过领域安全门禁

控制：Gateway 使用 capability-gated 领域工具注册表；Sleep Schedule/可选 `sleep_now` 必须经过 schema、时区、wake matrix、幂等、power lease 和 `PREPARE → COMMIT` 校验。能力收缩后迟到调用明确失败，不落入 HAL 或隐藏 handler。

### 10.50 开放配网 AP/明文 HTTP 泄漏凭据

控制：Provisioning Service 使用每会话高熵 WPA2/WPA3 凭据或等价认证加密通道、短 TTL、CSRF nonce、限流和物理取消；开放 captive portal 不接收 secret。完成/取消/超时后关闭 AP/DNS/HTTP 并清零 RAM。

### 10.51 AP+STA 配对恢复形成横向访问路径

控制：显式绑定接口和路由，验证 AP 客户端不能访问 STA、Hub token、管理接口或转发流量；恢复 portal 只接受一次性受限操作，Gateway 与 Provisioning 的网络 owner/lease 清晰且可停止。

### 10.52 Hub URL/TLS/EAP 信任策略降级

控制：Release 强制规范化 HTTPS origin、证书链、hostname/SNI 与可信时间；拒绝 userinfo/fragment/控制字符/歧义端口和 token 跨 origin。EAP 禁用 `ca_mode=none`，私有 CA 受保护并绑定 server domain，失败不回退 HTTP/无校验。

### 10.53 重配置或恢复出厂留下半提交身份

控制：配置使用 stage/confirm/rollback，确认新 Hub 前保留旧 token；恢复出厂是独立高确认事务，使用 tombstone 幂等执行远端撤销、本地分级擦除、会议数据与设备根身份策略，任意掉电点均可恢复。

### 10.54 HAL 结构体布局漂移导致错位调用

控制：所有 descriptor/ops 使用 `struct_size + abi_version`、尾部追加和 `_Static_assert`；校验必选 ops、保留字段与枚举值，旧/新 ABI 组合进入可诊断 SAFE_MODE，禁止基于编译恰好一致的隐式 ABI。

### 10.55 DMA/PSRAM/Cache-off 内存类别误用

控制：Memory/Resource Service 跟踪 capabilities、alignment、owner、pool 和 fence；flash 写入可达 task/ISR 使用 internal/IRAM 安全路径。压力测试覆盖 TLS、LCD DMA、NVS 和 MultiNet，不以 free heap 总量代替 largest block/内存类别门禁。

### 10.56 WDT 被放宽或持续喂狗掩盖死锁

控制：每个关键 task 声明 heartbeat 和最大连续运行区间；长操作在确定 chunk 点让出，WDT 前把 owner/phase/operation 写入 RTC record。CI 比较 WDT 配置，未经评审不得增大 timeout、移除监控或在未知阻塞中喂狗。

### 10.57 能力布尔膨胀或坐标/音频格式契约失配

控制：Display/Input/Audio 使用带版本和长度的结构化 descriptor；Renderer 与 Touch 共用 logical coordinate/rotation/safe mask，Audio Service 按 PCM/frame capability 协商。异构 Fake profile 和九点 HIL 防止每增加屏幕、触摸或 codec 就向业务添加布尔/板型分支。

### 10.58 麦克风隐私控制或录音指示与真实采集失配

控制：Privacy HAL 把硬件 mute 转为高优先级标准事件，Audio Service 在采集边界强制执行；Feedback Service 由真实 capture ownership 驱动独立指示。解除 mute 不自动采集，LED/反馈故障进入明确降级；没有物理能力的 profile 不对外声明。

### 10.59 公共 API 泄漏 ESP-IDF/FreeRTOS 类型

控制：Device/Platform 公共 header 只使用稳定 status、标准整数、opaque handle 和毫秒/单调 deadline；平台类型停留在 HAL SPI/port，错误与 tick 换算集中于 Service 边界，并以无 ESP-IDF 的主机构建门禁验证。

### 10.60 TLS 可信时间引导形成死锁或降级

控制：Clock/Security 明确三态 trust model、签名构建时间下界和受信 RTC；未验证 SNTP/Hub 不能自证，时间异常时不发生产 token、不关闭证书时间/链/hostname 校验，提供受控恢复反馈。

### 10.61 恶意 payload 造成 OOM、CPU DoS 或整数溢出

控制：所有非可信格式具备 body/depth/node/string/decoded-size/duration/pixel/redirect/CPU 配额和 checked arithmetic；先完整 parse/validate 后产生副作用，并以 fuzz、OOM、超时及 corpus 回归覆盖。

### 10.62 旧 handle 或 callback 导致 UAF

控制：公开 handle 带 type/generation，不暴露 driver pointer；close 幂等且使旧 handle 失效，callback token 先禁投递再有界 drain，restart/sleep/注销竞态通过故障注入验证。

### 10.63 录音或凭据普通删除被误报为安全擦除

控制：记录介质加密能力、retention、删除和导出策略；未加密只声明逻辑删除及残留风险，只有销毁已验证加密对象的数据密钥才宣称 crypto erase，UI/工装报告与 security manifest 一致。

### 10.64 Capability 快照 TOCTOU

控制：profile 只读、health 单写者、effective capability 使用不可变 revision snapshot；operation 启动时绑定 revision，收缩时按预定义完成/降级/取消策略执行，不跨多次裸指针读取决策。

### 10.65 离线解绑失败导致旧 token 复活

控制：本地凭据先隔离，远端 revoke 使用可恢复 tombstone、TTL/撤销列表和重试上限；credential generation 绑定设备，Hub 拒绝旧 generation、旧 NVS 镜像和迟到确认。

### 10.66 关键控制事件被队列洪峰淹没

控制：所有消息使用版本化 event envelope；wake/cancel/alarm/mute 使用独立 reservation/mailbox，telemetry 按 source budget 与 latest-wins 合并。关键 reservation 耗尽触发 sticky fail-safe 和监督恢复，不无界阻塞、不覆盖另一关键事件。

### 10.67 Hub 回显污染本地能力或跨会话授权

控制：effective、accepted、negotiated 三层快照只单向求交，协议/tool/media schema 与 negotiation epoch 显式绑定；重连、换 Hub、重新认证使旧 epoch 失效，远端永远不能打开本地不存在或安全策略关闭的能力。

### 10.68 启动早期弱随机导致 secret/ID 可预测

控制：Entropy Service 校验目标 ESP-IDF 的硬件 RNG 强随机条件并管理 DRBG/reseed；readiness 前不生成安全标识或 secret，熵故障 fail closed，禁止回退 MAC、时间戳、计数器和普通 PRNG。

### 10.69 有线烧录到错误板型或错误分区

控制：写前读取设备 identity 并验证签名 artifact 的 product/board/hw/layout/compat/flash/partition；任何不确定默认拒绝。写后使用 flash transaction ID、boot session、实际 digest 和分层 readiness 闭环确认。

### 10.70 Diagnostic/USB 身份通道形成后门或永久任务

控制：通道归 Diagnostic Platform Service 和 Task Registry，使用有界 parser、nonce/transaction correlation、stop/join、release allowlist、rate limit 与脱敏；禁止任意 NVS/内存/secret 读取及绕过恢复出厂确认。

### 10.71 多套版本号被错误合并

控制：profile、HAL ABI、partition、NVS、Gateway protocol、capability/tool/media/diagnostic schema 各自维护兼容规则和迁移矩阵；manifest 明确组合，启动/握手按对应版本判定，不以“固件更新”隐式兼容所有层。

### 10.72 诊断洪泛反压实时业务或造成高基数泄漏

控制：日志使用有界 ring、聚合和 rate limit，metrics label 只接受枚举；诊断 sink 与实时路径隔离，阻塞/断开只丢弃可丢诊断并计数，不阻塞输入、音频、Display fence、Power COMMIT 或写 flash 风暴。

### 10.73 Service locator/隐式初始化重新制造耦合

控制：唯一 composition root 使用无环 dependency manifest 和显式 API/handle 注入；Service 不直接取 board/global singleton、不在构造时启动 task。架构扫描与 Fake leaf 构建验证依赖可替换和逆序停止。

### 10.74 瞬时故障导致 capability flapping

控制：raw health 经单写者按失败/恢复窗口、最短保持、cooldown 和严重度归并；安全故障立即 fail closed，普通恢复必须重新自检并越过 readiness barrier 后才重新发布。

### 10.75 子 HAL 私自恢复共享总线破坏其他器件

控制：Resource Manager 统一 quiesce 全部 borrower、执行 profile 允许的 bus recovery/power reset、递增 generation 并 reprobe；旧 handle 失效，超预算保持 DEGRADED，不允许 Audio/Input 各自 reset 共享 I2C。

### 10.76 配置来源冲突产生半更新或越权覆盖

控制：Configuration Service 声明 key 的来源、优先级、权限、TTL 和重启语义，以 immutable revision 原子发布；observer apply 失败回滚，Hub policy 不能覆盖电气、安全和物理隐私限制。

### 10.77 错误校准被应用到另一硬件或通道

控制：校准携带 schema、board/hw/device/channel、工装/批次、完整性和范围 provenance；不匹配时使用安全默认或关闭能力。更新双缓冲原子提交并保留上一确认 revision。

### 10.78 运行期动态分配导致碎片和实时抖动

控制：DEVICE_READY 后 ISR/audio/display completion/power commit 使用预分配 pool；非实时动态任务受 owner quota/reservation/归还 deadline 约束，CI/HIL 监控 alloc/free 次数、largest block 和长期碎片趋势。

### 10.79 音频采样时钟漂移破坏长录音完整性

控制：HAL 报告实际 sample clock、DMA timestamp/sequence 与 gap；会议时长以已提交样本数为主，监测 ppm/overrun/underrun。跨时钟域使用有界 resampler/jitter buffer，超预算终止或标记损坏而非静默成功。

### 10.80 渐进迁移期间新旧路径产生双副作用

控制：每个 facade seam 建立唯一 ownership map；shadow 只运行纯计算。cutover 在 quiescent point drain callback/DMA、handoff state 并失效旧 generation；测试拒绝双输入、双录播、双 NVS/文件写、双 ACK 和双删除。

### 10.81 使用 volatile 误以为实现跨核同步

控制：concurrency manifest 声明 owner/reader/ISR/atomicity/memory order；优先采用单写者与队列，必要时使用受审查 atomic/critical section。CI 扫描新增跨任务 `volatile`，压力测试覆盖多字段撕裂、回绕 ABA 和 stop/restart。

### 10.82 锁原语误用造成优先级反转或死锁

控制：lock manifest 记录获取顺序、primitive、priority inheritance 和最大持有时间；mutex 与事件 semaphore 分离，critical section 常数时间。低优先级持锁故障注入验证 Audio/Input/Display bounded blocking。

### 10.83 Event/tool/status ID 漂移或复用

控制：集中 schema registry 使用显式稳定数值、introduced/deprecated 版本和 tombstone，生成 C/schema/vector；CI 比较 golden registry，未知 ID 有边界策略，删除编号永不复用。

### 10.84 Facade 长期存在形成第二套架构

控制：迁移台账为每个 API 指定 owner、build flag、验证证据和删除 Phase；Phase 9 要求调用点、旧 task/global writer/NVS owner 全部为零。诊断回退窗口关闭后从 release 删除 legacy 对象，而非永久双实现。

### 10.85 构建依赖或生成资源不可追溯

控制：锁定 IDF/toolchain/component/source/hash、sdkconfig/partition 和字体/模型生成输入；生成 SBOM、许可证/CVE、artifact/ELF/resource digest 与签名 provenance，来源/hash 漂移阻止发布。

### 10.86 开发机缓存掩盖不可复现或受污染构建

控制：CI 在隔离 clean workspace/cache 策略下重建三硬件并比较规范化内容 digest；未锁依赖、未知本地组件、未提交生成物和高危供应链告警不允许签名，发布证据关联具体构建环境。

### 10.87 单组件重启破坏共享故障域

控制：fault-domain manifest 声明 shared owner、borrower 和传播边界；共享 I2C/电源轨/时钟故障统一 quiesce 同域组件，按 profile 流程恢复并整体重新自检，禁止只重启一个 borrower 后继续使用旧资源。

### 10.88 重启后旧 operation、handle 或 callback 复活

控制：重启关闭 admission 并 drain callback，在释放/重建前递增 domain/resource generation；完成事件校验 boot/operation/domain generation。无法 join 时保留并隔离资源，禁止复用地址或自动重试 unknown-outcome 副作用。

### 10.89 分层 timeout 累加突破端到端 deadline

控制：所有长事务使用绝对单调 deadline 并传播 remaining budget；重试、排队和子调用共用父预算，预留 bounded cleanup reserve。deadline miss 输出关键路径与最慢 phase，不能靠扩大每层 timeout 通过。

### 10.90 不可逆 schema migration 阻断固件回滚

控制：采用 expand/contract、reader/writer version window 和双 generation journal；不可逆提交前验证备份/兼容 reader 或显式关闭回滚。有线工具检查 firmware-data 兼容矩阵，掉电/低空间时保持旧 generation 可读。

### 10.91 降级状态缺少用户可理解反馈

控制：统一 `degradation_reason_t`，由 Feedback Service 按 profile 映射到现有屏幕、LED、音频或触觉；隐藏板型和底层错误，按 reason/generation 去重限流，并在 readiness 滞回恢复后再通知。

### 10.92 Core dump 泄漏 secret/录音或耗尽 Flash

控制：release 默认采用经验证加密 dump 或禁用完整 dump；排除/清零敏感 buffer，限制 retention/容量和认证导出，绑定 firmware/boot digest。恢复出厂/转移清理，低电量与 boot 不等待 dump，分区不能侵占用户数据。

### 10.93 缺失证据或过期 waiver 被误判为通过

控制：集中 evidence registry 把 requirement、owner、实现、test、profile、firmware digest 和证据 hash 绑定；CI/HIL 未产出匹配证据即失败。waiver 记录批准人、补偿控制和 expiry，到期自动阻断 release。

### 10.94 Service 重启后运行但状态未收敛

控制：为每个 Service 区分 authoritative/durable/ephemeral state 并实现幂等 reconciliation；只从新 revision 快照恢复 subscription、timer 和 desired state，禁止复制旧 runtime context。observed state 未对齐或 external outcome 未知时保持降级。

### 10.95 软件初始化前高风险 GPIO 已产生毛刺

控制：electrical-safety manifest 覆盖 ROM/bootloader/reset/WDT/brownout/deep-sleep/下载模式默认态；最早期 safe init 仅应用已验证安全交集。软件不可控窗口由外部上下拉、mute 或电源门控保证，并用示波器 HIL 验证。

### 10.96 DFS/APB 切频破坏音频、显示、输入或诊断

控制：clock/PM manifest 声明频率、时钟源、lock、barrier 和重配置；切频在 transaction 安全点执行，所有发布频点实测 sample clock、QSPI/DMA、I2C timeout、UART baud 与 deadline。失败组合关闭 capability。

### 10.97 资源压力下各组件无序抢占并触发重启循环

控制：Resource Pressure Service 统一水位、滞回、admission 和降载顺序，预留关键控制/收尾/commit 资源；板级只报告资源事实。OOM、queue 满或存储紧张不得各自触发无限 retry/reboot。

### 10.98 HIL 或 golden 证据不可复现、可被选择性覆盖

控制：证据绑定 board/fixture/仪器校准/环境/脚本/原始数据/样本和不确定度并签名归档；golden 更新独立审批，所有重跑 attempt 保留。样本不足、仪器过期或 flaky quarantine 过期自动阻断 release。

### 10.99 固定 16 MiB 仍误启用设备端 OTA

控制：partition/capability/tool/CI 四层同时禁止 `otadata/ota_*/staging`、`esp_ota_*`、firmware download URL 和 `ota.install`；架构测试扫描设备 release binary。未来只有新硬件容量、威胁模型和产品决策全部重新评审后，才能建立新的独立计划，不能用远端 feature flag 偷开。

### 10.100 Hub Update Catalog 被污染或给错 profile

控制：只接收 allowlisted GitHub repository/release/channel 的已签名 immutable manifest，绑定 tenant/client/device/profile/hw/layout/credential generation；draft、缺件、asset mutation、签名错误或未知 critical field 不进入 catalog，响应不包含 firmware URL。

### 10.101 版本提醒风暴或长期打扰用户

控制：同 release 去重，启动检查与周期检查有最小间隔、抖动和退避；`remind_after/dismissed_release_id/dismiss_until` 合并、限频持久化以控制 NVS 写放大。Critical 仅调整可见性和重复周期，具有最大 defer/dismiss TTL，不能永久隐藏，也不能远程下载、刷写、重启或绕过用户选择。catalog 撤回、manifest digest 改变或更高 sequence 出现时旧 dismiss 自动失效。

### 10.102 刷机工具下载到部分、篡改或错误 bundle

控制：工具只从允许的 GitHub Release 获取正式 `.clawfw`，完整下载后验证 manifest 原始字节签名、package/asset size/SHA-256、工具最低版本和 profile/manifest/tool 三方 allow-list；任何检查失败都发生在首个写入前，`packageId + manifest digest` 冲突或已发布 asset 变化整体隔离。

### 10.103 用户选错硬件或刷机工具只信文件名

控制：写入前通过 USB/串口读取 ROM/chip/flash/security、真实 partition table `layoutFingerprint` 和设备 identity，再与 `.clawfw`/profile 匹配。制造身份形成 `confirmed`；缺少强身份只能形成 `probable` 并要求实物确认；`ambiguous/conflict` fail closed，人工确认不能覆盖芯片、容量、布局或安全错配。

### 10.104 普通更新误擦除 NVS、录音或资源

控制：ClawMate Maker `appUpdate` 与 `fullInstall/恢复出厂` 是显式模式；日常更新只写 App，默认不写 bootloader/partition/model/NVS/storage。写区同时受 manifest `files/eraseRegions`、profile `reservedRegions`、真实布局约束，禁止使用 padded merged raw image。模型更新另设兼容模式；layout/schema 不兼容时先走受支持的逻辑导出或拒绝，不能通过自动格式化获得成功。

### 10.105 单 app 刷写中断导致设备不可启动

控制：stable 发布前完成三硬件逐 block/关键提交点断电故障注入和恢复演练；刷机前检查稳定 USB/供电，App 擦除后进入不可普通取消的风险窗口，刷后执行 ROM hash/readback。中断进入 `RECOVERY_REQUIRED`，工具凭原子 journal 重新识别设备和包后从镜像边界恢复，不按百分比续写或循环自动重刷。

### 10.106 单 app 没有自动回滚却被误报为安全更新

控制：UI、release notes 和工具明确“需要电脑、升级中断可能需要恢复、无设备端自动回滚”。stable 门禁要求升级/降级 reader-writer window、nonce `BOOT_STATUS.ready`/`SERVICE_STATUS.ready` 和上一稳定 `.clawfw` 的真实恢复证据；数据 schema 已不可逆迁移时必须阻止不安全降级，不能以“保留数据”替代兼容证明，禁止以 OTA/A-B 文案对外宣传。

### 10.107 刷机开始时截断闹钟、会议或持久化

控制：若当前固件可通信，工具先请求 maintenance readiness 并检查 Alarm/Meeting/Persistence；活跃事务要求用户结束或明确延期。无法通信时进入 bootloader recovery 但不得承诺保留未知 in-flight 数据，风险必须在写前显示。

### 10.108 Manifest 签名解析歧义或密钥轮换失败

控制：签名输入具有固定 domain separator 和严格 canonical schema，拒绝 duplicate key、未知 critical field、非法 UTF-8/整数表达；CI、Hub 和刷机工具使用相同 golden vectors。发布私钥留在受保护 CI/HSM，轮换/吊销/RMA 有独立演练。

### 10.109 设备 metadata API 只验证 bearer 而未绑定真实设备

控制：授权同时绑定 tenant/user、已配对 client/device、credential generation、profile/hw/layout；path/query ID 不作为授权事实。旧 token、克隆 identity 和跨设备 catalog 查询全部拒绝并限流审计。

### 10.110 用户无法找到正确刷机工具或恢复路径

控制：设备提醒、Hub 客户端和 GitHub Release 使用一致的官方工具名称/最低版本/release ID；提供 Windows/macOS/Linux 可验证下载、离线签名校验、正常升级与恢复模式步骤。不得只显示裸 URL、内部 asset 名或无法操作的错误码。

## 11. 提交与回滚策略

每个 Phase 独立提交，并保证提交点上三款正式硬件均能构建。推荐提交边界：

1. HAL 接口与 profile，不改变行为。
2. 生命周期、Resource Manager 和失败回滚。
3. Device API、Platform API、领域 Service 边界、事件队列、Input HAL 与共享意图。
4. 完整 UI 模型、数据所有权和 Display Task。
5. Bread Display HAL。
6. EchoEar Display HAL。
7. Fangtang Display/Input profile：先拆出 NV3023、Y offset=80、单键和独立资源表，禁止拖到最终阶段才从 Bread board port 分离。
8. 三 profile Audio/Wake Service；同时补齐 Fangtang 0–100 音量和持久化。
9. Phase 7A-a：Storage/Persistence/NVS 恢复、Connectivity/Gateway/Identity、Provisioning/Security 和构建依赖收口；Fangtang ML307 从 Command/Meeting/时间路径迁入 Connectivity port。
10. Phase 7A-b：GitHub workflow ClawMate Maker `.clawfw`/manifest、Hub Update Catalog metadata API、三硬件统一版本提醒、官方刷机工具 identity confidence/`layoutFingerprint`/`reservedRegions`/readback/`RECOVERY_REQUIRED` 闭环。
11. Phase 7B：Power/Wake/Wake Deadline/Sleep Schedule、Alarm deadline、Wake Word power lease、Battery Policy 与版本检查网络 lease。
12. Phase 7C：Gateway quiesce、离线队列/跨 session 语义、Update/其他领域工具注册与能力投影。
13. Event envelope/关键 reservation、Entropy、Diagnostic identity 与有线烧录兼容闭环。
14. Composition/concurrency/lock/schema registry、facade cutover/shadow 与可回退迁移闭环。
15. Fault-domain supervisor、端到端 deadline、restart reconciliation、持久化 expand/contract migration 与降级反馈闭环。
16. 电气安全、DFS/clock-domain、资源压力、自检、量产、HAL ABI/内存/WDT/core-dump 门禁、Fake HAL、三正式 profile 最终对齐和第四异构 profile 演练。
17. facade 删除、可复现构建/SBOM/供应链/HIL evidence 门禁、发布硬化、兼容矩阵和文档。

若某阶段实机失败，只回滚该阶段，不把未验证的后续拆分叠加到问题之上。任何临时兼容分支必须标明删除 Phase，禁止成为永久板型特判。

## 12. 正式硬件接入完成定义

Fangtang-4G 当前必须按本节完成独立正式 profile 接入；未来新增硬件进入 MaClaw AgentOS 正式支持集合时也执行同一清单。仅用于测试的 Fake/Reference profile 可以按其测试目的声明物理能力缺失，但不能替代三款正式硬件的 Bread 功能等价证据。

正式硬件接入首先满足两个前置条件：

1. 实现第 6.1 节全部 Bread 功能母版，正式业务 capability/tool 集合不得缺项。
2. 若缺少同类物理控件或反馈器件，提供可发现的替代入口/呈现；若物理限制确实无法承载公共功能，则在修订硬件前不能进入正式支持集合。

除此之外只允许执行以下适配工作：

1. 在 Kconfig/CMake 增加板型选择和源文件集合。
2. 新建 `boards/<new_board>/board_profile.c`、Resource Manager 和 build manifest。
3. 按硬件实际能力实现 Input、Audio、Display、Power HAL；需要不同存储/连接/身份后端时实现对应 Platform port；仅非公共基线的可选物理增强可返回不支持。
4. 将物理控件映射为标准输入事件。
5. 用已有共享业务状态机测试和 UI 场景测试验证。
6. 运行全部正式 profile 的多配置构建、Bread 功能矩阵和新硬件实机验收。
7. 确认生成的 `clientCapabilities` 与硬件/固件实际能力一致。
8. 通过生命周期故障注入、事件队列和数据所有权测试。
9. 声明 board revision、build manifest、partition layout 和 runtime budget，并通过错配保护。
10. 通过 operation generation、Fake Clock、启动降级和硬件自检测试。
11. 声明 SAFE_MODE 最小反馈链路，以及 retry/circuit-breaker/reboot budget。
12. 使用异构能力组合验证 Storage/Connectivity/Identity 与可选 sensor/power 扩展，不修改领域业务实现。
13. 声明各 sleep depth、timer/按键/触屏 wake matrix、wake GPIO 有效电平/供电约束、schedule 行为和实测功耗预算。
14. 验证 GPIO strapping、Task Registry、boot-loop 熔断和共享 Wake Deadline；新增硬件不得私自创建 RTC timer owner。
15. 若声明 battery/charger telemetry 或低电量深睡，提供 ADC/电量校准、滞回、低压写入预算和 charger/button/timer 恢复源的实机证据；无对应电路时能力保持关闭。
16. 验证 wake-word 持续监听对允许 power state 的约束，以及 stop/join/resume 生命周期；不能通过板级常驻任务私自改变产品电源策略。
17. 提供 Provisioning 安全能力：认证加密会话、TTL/限流/物理取消、接口隔离、secret zeroization 和完整 stop/join；无屏硬件必须定义可信带外引导方式。
18. 声明 HAL ABI、内存类别/池、cache-off 和关键 task WDT/heartbeat budget，并通过对应主机测试与 HIL；不得让新板用自定义全局超时绕过共享门禁。
19. 声明 display logical coordinate/rotation/safe mask/touch transform 与 audio PCM/frame contract；新屏幕或 codec 差异只能扩展版本化 descriptor/HAL，不得修改共享业务用例。
20. 声明 hardware mute、录音/状态反馈和失效策略；若存在，仅通过标准 Privacy/Feedback HAL 适配，业务不得读取对应 GPIO。
21. 证明公共 Device/Platform header 不依赖 ESP-IDF/FreeRTOS，并通过稳定 status、timeout/deadline、opaque handle 和 callback drain 的共享合规测试。
22. 声明 provisioning 引导 adapter（屏幕、每设备标签、安全 BLE 或认证 USB/工装）及无屏/无输入恢复路径；不得使用固定全产品默认密码。
23. 使用统一 parser contract 和资源预算验证新屏幕媒体格式、codec 或配网输入；新增格式必须先加入 fuzz corpus，不能在板级实现无上限解析。
24. 声明录音/敏感对象 retention、加密与删除语义，以及解绑 credential generation；硬件接入不得改变共享数据治理或把普通删除宣传为安全擦除。
25. 验证 capability descriptor 启动后只读、effective snapshot revision 和在途操作收缩策略；新板不得返回可变裸 capability 指针。
26. 接入统一 Device Event envelope，声明每个新 producer 的 source ID、峰值速率、priority/reservation、payload ownership、coalescing 与 stop/drain；不得自建绕过队列的 callback 通道。
27. 通过 Entropy Service 获取所有板级 provisioning/pairing/identity 随机值，验证该 target/IDF 的强随机 readiness；不得在新板适配中自行拼接 MAC/时间戳。
28. 扩展 firmware identity manifest 与烧录兼容规则，至少覆盖 product/board/hw/layout/compat/flash/partition；有线工具不能仅靠用户选择板名防错刷。
29. 新增 transport/协议能力时声明 protocol/descriptor/tool/media version 与 negotiation epoch 行为；远端 accepted 不得回写 profile 或跨认证会话复用。
30. 若新增诊断/工装通道，必须实现 Platform port、Task Registry 生命周期、release allowlist、有界 parser/rate limit 和脱敏；不得把裸串口任务塞入 board HAL。
31. 将新板所有 Service/port 通过 composition root 显式装配并通过无环依赖检查；适配文件不得新增全局 service locator 或隐式初始化。
32. 声明共享 I2C/SPI/I2S 的 borrower、故障影响、generation 和 profile-safe recovery/reprobe；新子 HAL 不得私自重置共享总线。
33. 声明配置/量产校准来源、优先级、硬件/通道 provenance、完整性和回滚；远端策略不得修改板级安全限制。
34. 提供 DEVICE_READY 后实时路径预分配证明、动态 owner quota 和长稳碎片数据；不得靠扩大 heap 掩盖生命周期问题。
35. 对音频硬件声明实际 sample-clock ppm/gap/overrun budget，并验证长录音样本数、WAV 和上传 metadata；不能只声明标称采样率。
36. 为新增 facade seam 声明唯一 legacy/new owner、只读 shadow、quiescent cutover、state handoff、回退窗口和删除 Phase；适配不得长期保留双实现。
37. 将所有跨 task/core/ISR 状态加入 concurrency manifest；新板代码不得用 `volatile` 代替 queue/mutex/atomic/critical section，也不得用 binary semaphore 冒充共享状态 mutex。
38. 新 event/tool/status/source ID 必须从集中 schema registry 分配并生成代码/测试，禁止板级 enum 自动编号或复用 deprecated ID。
39. 新 profile 的组件与生成资源进入锁定依赖、SBOM、许可证/CVE 和 artifact provenance；仅本地缓存可构建但 clean CI 无法重现不算接入完成。
40. 声明 fault-domain membership、shared owner/borrower、可独立重启性、restart deadline、generation 失效和 operation outcome；新板不能用私有 task delete/recreate 绕过统一 Service Supervisor。
41. 为 Boot/stop/restart/sleep/config/provisioning 的 profile 特定步骤声明子预算与 cleanup reserve，但只能消费共享父 deadline，不能创建无限全局 timeout。
42. 声明 NVS/Storage/calibration 的 reader/writer version window、expand/contract 迁移、空间/写放大和旧固件回滚边界；板级校准不得做无 journal 的就地不可逆改写。
43. 为所有可降级 capability 提供标准 reason 到 profile Feedback HAL/Renderer 的映射及无显示 fallback；反馈适配不得包含业务状态机或暴露 controller/GPIO 错误。
44. 声明 core dump 支持、Flash Encryption、敏感内存排除、分区容量/retention 和认证导出策略；未验证的新 profile 默认不启用生产完整 dump。
45. 提供当前 profile 与 release artifact digest 匹配的 requirement/test/HIL evidence bundle；复制另一硬件的证据或长期 waiver 不算接入完成。
46. 提供 electrical-safety manifest 与 reset/bootloader/WDT/brownout/deep-sleep/下载模式波形证据；高风险输出在软件接管前必须由硬件默认态保证。
47. 声明所有 clock-domain/DFS 频点、PM lock owner、切换 barrier 和外设重配置，并通过 Audio/Display/Input/Diagnostic/time HIL；未验证频点保持关闭。
48. 接入共享 Resource Pressure Service，声明 profile 水位、降载能力和 emergency reserve；新增 HAL 只报告资源事实，不实现板型专属业务降级或 OOM 重启策略。
49. 对 profile 特有可重启 Service/port 声明 authoritative/durable/ephemeral state 与 reconciliation probe；禁止以保存整个 runtime context 实现恢复。
50. HIL evidence 记录 board/fixture/仪器校准/环境/脚本/原始数据/样本/不确定度并签名；golden 更新与 flaky quarantine 遵循共享审批和过期门禁。
51. 声明 flash size/单 factory/model/storage layout、Update Catalog profile binding、版本提醒 Renderer/Input 映射、`.clawfw`/ClawMate Maker 兼容、默认数据保留、刷后 readiness 和恢复证据；新板不得新建板型更新业务 Service，也不得在设备端引入隐藏 firmware download/partition write 路径。

以下行为不属于硬件接入，原则上禁止：

- 复制 `main.c`、会议逻辑或交互任务。
- 在共享代码添加 `if (new_board)`。
- 为新屏幕复制一份业务 UI 状态机。
- 在板级驱动中发起网络请求、决定会议状态或处理回复生命周期。
- 在 Device/HAL API 中新增 `meeting_*`、`alarm_*` 或固定时长指令捕获等业务接口。
- 在会议/闹钟/网关领域代码中直接使用 SPIFFS 路径、`FILE *`、`esp_wifi_*` 或 `esp_http_client_*`。
- 从业务层直接调用 HAL ops 或读取 board ID。
- 在多个子 HAL 中重复初始化同一总线或电源轨。
- 在 Renderer 中保存调用方临时指针或写入产品 NVS。
- 使用墙上时钟实现手势或超时，或让长 I/O 阻塞 App Interaction Task。
- 固定声明未经过硬件、固件与运行时健康共同证明的协议能力。
- 在未验证 layout/schema 兼容时擦除或格式化 NVS、model、storage。
- 绕过 board/profile 校验直接驱动高风险 GPIO、电源轨或功放。
- 在 Renderer、Input HAL 或板级定时器中自行决定计划休眠时间，或让唤醒 contact 直接触发业务动作。
- 在 wake source 未成功 arm、墙上时间不可信或存在不可中断 power lease 时强制进入 deep sleep。
- 让板级 Alarm/Schedule/维护代码直接覆盖 RTC timer，或创建无 stop/join 契约的永久后台任务。
- 仅因 Kconfig 打开、component 链接或函数指针非空就发布未经初始化/自检/实机验证的能力。
- NVS 初始化错误时自动擦除整区用户数据，或让单个 namespace 损坏触发全局恢复出厂。
- 在 Hub 只有有界内存队列时宣称设备深睡期间消息必达，或为加快休眠伪造 ACK/推进未消费 cursor。
- 让 Gateway 硬编码 Alarm/Sleep 等领域 handler，或让远程 `sleep_now` 绕过 power lease、wake-source 与事务门禁。
- 在无已验证 charger/button/timer 恢复源时因低电量进入 DEEP_SLEEP，或在棕断边缘循环写 flash/checkpoint。
- 在量产 release 中使用开放 AP/明文 HTTP 收集 Wi-Fi/EAP/token，允许 HTTP Hub 或关闭 TLS/EAP CA 校验。
- 把重新配置等同恢复出厂，或在新配置/配对未确认前删除旧 token 和可恢复数据。
- 通过强转旧 ops、忽略 `struct_size/abi_version`、使用未知枚举或 capability 与函数表不一致来“兼容”新硬件。
- 用普通 PSRAM/错误 alignment buffer 做 DMA/cache-off 访问，或通过放宽 WDT/持续喂狗掩盖阻塞和死锁。
- 为每个新屏幕/触摸/codec 向顶层能力表追加零散布尔值，或让业务自行处理 rotation、圆屏 mask、RGB/BGR、采样位宽和声道映射。
- 让业务直接读取麦克风 mute/LED GPIO，解除 mute 后自动录音，或用并不覆盖真实 capture 生命周期的 UI 动画冒充录音指示。
- 在公共 Device/Platform header 暴露 `esp_err_t`、`TickType_t`、FreeRTOS/driver handle，或把底层错误码直接当稳定业务协议。
- 让 API 返回裸 driver/capability 指针、复用已关闭 handle，或注销 callback 后仍访问调用方上下文。
- 通过关闭证书时间校验或使用待验证 Hub/SNTP 自证可信时间来完成首次 TLS 连接。
- 在板级或媒体 adapter 中解析无 body/depth/decoded-size/CPU 上限的 JSON、base64、图片、WAV/MP3 或 tool payload。
- 把屏幕设为配网必选依赖、使用固定全产品默认 AP 密码，或让无屏/无输入硬件没有取消和恢复路径。
- 让普通 telemetry、touch bounce 或网络 burst 占满关键事件容量，使用无界阻塞发布，或在 queue overflow 时静默覆盖 cancel/alarm/wake/mute。
- 把 Hub `capabilitiesAccepted` 写回本地 profile/health、让远端启用本地不存在的能力，或在换 Hub/重新认证后复用旧 negotiation epoch 的工具授权。
- 在强随机源就绪前生成配网/配对 secret、boot/operation 安全标识，或用 MAC、墙钟、计数器和普通 PRNG 作为随机替代。
- 仅凭串口号、人工文件名或 Kconfig 选择烧录固件，不校验签名 artifact 与设备 product/board/hw/layout/compat/partition。
- 让 USB/串口诊断任务永久运行且无法 stop/join，开放任意 NVS/内存/secret 读取，或使日志/metrics 阻塞实时路径与休眠 COMMIT。
- 让 Service 自行查询 `board_hal_get()`/全局单例、隐式初始化依赖或在 constructor/init 中创建未登记的常驻任务。
- 由单个 Audio/Input/Display HAL 私自 reset 共享总线，继续使用 recovery 前取得的裸 driver handle，或用无限恢复循环制造能力抖动。
- 让 Hub/临时 override 直接写产品 NVS 并越过 Configuration Service，或把无 provenance 的校准应用到不同 board revision/rotation/codec channel。
- 在固定 16 MiB 产品上新增 `otadata/ota_*/staging`、调用 `esp_ota_*`、下载 firmware bytes，或用远端 feature flag 暗中恢复设备端 OTA。
- Hub 未验证 GitHub release manifest 就向设备报告版本，返回 firmware/asset URL，或让 tenant/用户替换官方 release metadata。
- 设备版本提醒出现“立即安装/下载中/刷写中/自动回滚”等不存在的能力，或 critical release 绕过用户操作触发下载、重启。
- 刷机工具仅凭文件名、串口号或人工板型选择写入，不校验 signed manifest 与真实 product/board/hw/layout/compat/flash size。
- 普通更新默认擦除 NVS/storage、删除未上传会议，或把恢复出厂刷机隐藏在普通“更新”按钮后。
- 刷机工具未完整下载/校验就写入、刷后不回读 digest、失败无限自动重刷，或宣称单 app 具有 A/B 自动回滚。
- 仅凭合法 bearer 或 query `clientId` 返回跨设备/profile 的 release metadata，允许旧 credential generation 查询或记录敏感 URL/token。
- 在 steady-state ISR、audio stream、Display DMA completion 或 Power COMMIT 中使用通用 heap，或忽略长录音实际采样时钟漂移和 DMA sequence gap。
- 在迁移期同时运行 legacy/new 输入、显示、音频、NVS、网络 ACK 或文件删除副作用，或把 facade 变成新的状态/任务/资源 owner。
- 使用 `volatile` 作为跨核/task/ISR 同步，依赖多个裸全局字段形成一致 snapshot，或用 binary semaphore 替代需要优先级继承的 mutex。
- 由板级代码自行分配 event/tool/status 数值、复用已废弃 ID，或手写与集中 schema registry 不一致的协议定义。
- 引入未锁版本/来源/hash 的组件或字体/模型生成物，不生成 SBOM/许可证/CVE/provenance，或只因开发机缓存能够链接就发布。
- 由单个 borrower 私自重启共享 fault domain、在 restart 后继续使用旧 generation handle，或让 unknown-outcome 副作用自动重复执行。
- 在 Boot/stop/restart/sleep/config/provisioning 子层重新开始完整 timeout，使总 deadline 失效，或 deadline 到期后继续产生后台副作用。
- 就地不可逆改写 NVS/Storage/calibration 后仍宣称旧固件可回滚，或在 migration 未验证/未 commit 时清理旧 generation。
- 把底层 driver/GPIO/板型错误直接展示给用户、让每个 HAL 自行定义降级文案，或用重复提示风暴代替标准 Feedback Service。
- 在未加密或未认证条件下导出生产 core dump、让 dump 包含 secret/录音 buffer、占满用户分区，或阻塞低电量与启动恢复。
- 将缺失测试、错误 artifact digest、过期 waiver 或另一 profile 的 HIL 结果登记为当前硬件通过证据。
- 从旧 Service context 恢复 task/lock/queue/driver pointer，或 task 重建后未对齐 authoritative desired/observed state 就恢复 capability。
- 只测 `app_main()` 后 GPIO 电平就宣称上电安全，忽略 ROM/bootloader/reset/brownout/下载模式毛刺和外部上下拉要求。
- 启用未经全频点验证的 DFS/PM，让 Audio/LCD/I2C/UART 各自猜测时钟变化，或用永久 PM lock 假装支持动态节能。
- 在板级 OOM/queue/storage 回调中自行停止会议、删除数据或重启整机，绕过统一 Resource Pressure policy 和 emergency reserve。
- 测试失败时自动更新 golden、只保留最后一次通过的 HIL attempt，或使用仪器校准过期/环境不明/样本不足的证据发布。

当一块新硬件仅通过新增 board profile、manifest、budget、HAL/Renderer 以及确有必要的平台 port 文件即可运行现有业务，且 `app/`、`domain/` 和已有共享 Service 的业务实现无修改时，才算真正完成硬件抽象目标。若新硬件引入全新的产品能力，应先以版本化 capability extension 和通用 Service 契约独立评审，不能借“适配硬件”把板型分支带回业务层。

## 实施增量：Waveshare ESP32-S3 Touch AMOLED 1.75C（COM6，2026-08-07）

COM6 已确认是 `waveshare-s3-touch-amoled-1.75c`，不是 EchoEar 的变体：ESP32-S3、32 MiB Flash、8 MiB OPI PSRAM、466×466 圆形 CO5300 QSPI AMOLED、CST9217 触控、AXP2101 PMU、ES7210/ES8311 音频和 QMI8658 IMU。

- 已新增独立 profile、`maclaw-s3-32m-factory-v1` 分区布局和 32 MiB flash identity；不能将 16 MiB 三板的镜像或分区表刷入此板。
- 业务仍复用统一 Device API 和圆屏场景契约：466×466 viewport、圆形显示能力、触摸为 primary control、电量 telemetry 和 display-off；没有新建 board-specific 业务流程。
- 已在板级适配中接入 CO5300 QSPI 初始化、AXP2101 上电/telemetry、CST9217 touch driver、ES7210/ES8311 I2C/I2S 引脚和独立 GPIO；所有 controller/PMIC/codec 细节停留在 board port 下。
- 圆屏 renderer 已将 360px 场景坐标映射到 466px 逻辑安全区；顶部 `466×162` 曲线文字缓存为 PSRAM 分配（150,984 bytes），避免静态 DMA 缓冲侵占 internal heap。2026-08-07 实机重刷后启动日志确认 CO5300、CST9217、ES7210/ES8311、双帧 PSRAM 缓冲和曲线文字缓冲均成功初始化，`BOOT_STATUS.ready=true`，启动后 internal largest free 为 63,488 bytes、PSRAM largest free 为 7,340,032 bytes。
- 32 MiB profile 构建成功：app `0x3107e0`，factory app 分区 `0x500000`，余 `0x1ef820`（39%）。构建输出包含 bootloader、partition table、speech model、app 和 storage 的完整地址映射。
- 2026-08-09 已完成一次 COM6 实机资源闭环：新镜像 SHA 前缀 `907d3b9e3` 启动并 handshake 成功，CO5300 亮度经适配器恢复到 40%；Hub 的 8 帧、256×256 宠物描述由 Display HAL 降级为 2 帧，两个 196,608-byte 帧均 SHA 校验、首帧先显示，随后完整安装 `frames=2/2`，总耗时约 32.8 秒；wake listener 在下载期间按可选媒体 lease 暂停并于安装后恢复。Waveshare adapter 拒绝装饰性 SPIFFS 写入，日志为 `deferred pet preview cache skipped by board policy`，避免 QSPI 显示与 cache-disabled flash 写并发触发 WDT。业务层未按板型选择帧数或缓存策略。
- 宠物安装预算的 HAL 契约现明确为两类量：`total_external_bytes` 是全部 replacement target 同时驻留的聚合 PSRAM 预算，`max_external_allocation_bytes` 是单次分配所需的最大连续块。通用 Pet Service 同时校验二者；不得把聚合预算误当作单帧预算，否则会在下载完成后才因 target-copy 分配失败。已同时修正已安装帧数只记录真实 `installed_frames`，避免 display HAL 降帧后把尚未驻留的原始帧数当作已应用状态。
- 因此当前可证明“编译、板级初始化、握手、宠物 HAL 降帧及可选媒体生命周期”已在 COM6 通过；CO5300 亮度的驱动事务已经通过、实际可见亮度差仍待相机/人眼 A/B，屏幕构图、触控坐标方向与手势、PMIC 电量校准、麦克风/扬声器的 16 kHz contract、DISPLAY_OFF 后触摸/按键唤醒和 IMU 的完整功能验收也仍需分别以 HIL evidence 发布，不能由本次启动成功推断。
- 已实现版本化统一 `Motion HAL`：`device_motion_sample_t` 仅传递单调时间戳、mg 加速度和 mdps 角速度；业务/service 不读取 I2C 地址、寄存器、量程或 GPIO。COM6 的 QMI8658 适配在 board port 内以 `0x6B`、8g/125Hz 加速度及 1024dps/112Hz 陀螺仪初始化；无 IMU 的 Bread / EchoEar / Fangtang 通过同一 API 明确返回 `UNAVAILABLE`。2026-08-07 COM6 HIL 已读取真实启动样本 `a=(-86,147,1086)mg`、`g=(-3000,9968,500)mdps`，并确认 capability bitmap 包含 `MOTION_SENSOR`。这证明基础采样链路，不代表姿态校准或低功耗中断已完成。
- 已实现首版硬件无关 `Fall Detection Service`：它每 100ms 仅调用 `device_motion_get_sample()`，组合“连续短暂失重（<350mg，至少100ms）→ 撞击（>2.5g）→ 与稳定基准相比的姿态变化（≥45°）→ 静止（3秒）”产生候选事件；随后持有统一的 presentation lease、唤醒显示，并提供 15 秒本机取消窗口。服务不引用 QMI8658、I2C、寄存器、GPIO、屏幕形状或输入类型；COM6 因 `MOTION_SENSOR` capability 自动启用，Bread / EchoEar / Fangtang 显式 `UNAVAILABLE` 且不创建任务。候选未取消时目前仅显示“疑似设备跌落，未收到本机取消”，**不会自动发送 SOS 或声称已经诊断人员跌倒**。服务持久化 `enabled`、配置 revision 和最近 4 条 `fall_detection_set` 回放记录；同一 `idempotencyKey` 在重启或 Gateway 重投后返回同一 `enabled` / `configurationRevision` 且标识 `replayed=true`，状态不变的设置也会留下回放记录，状态变更与记录在一次 NVS commit 中提交。注册统一 `fall_detection_status` / `fall_detection_set` 工具，并将 availability、启用状态、候选计数和 revision 放入 USB `IDENTITY` / `BOOT_STATUS` / `SERVICE_STATUS`；工具不接受板型阈值、寄存器或“医学跌倒”语义。禁用会原子关闭待确认窗口并释放其 presentation lease，重新启用从新的 motion evidence 开始。
- 阈值为首版保守工程默认值，仍须以实际佩戴位置、行走/跑步/放桌/跌落等受控样本校准，并保留误报审计；未佩戴时只能报告“设备疑似跌落”，不能报告“人员跌倒”。该能力是非医疗级安全提醒，不替代医疗或紧急救援。后续姿态/静止事件订阅、可选 QMI8658 INT2 低功耗唤醒、持久化开关/阈值配置、Hub 告警协议与完整 HIL 跌落用例必须独立设计、验收，不能由当前 `MOTION_SENSOR` bit 或本首版状态机推断。
- 已将 Wi-Fi/企业 Wi-Fi、Hub URL、配对码、Hub token、输出音量和一次性强制配网请求收敛至版本化 `Configuration Service` 快照。新配网会与旧 token 清除同一次提交，配对成功会与配对码清除同一次提交，音量和强制配网也不再由 `main.c` 直接读写 NVS；旧分散 key 只用于一次性导入。配置 blob 缺失仍保留编译期默认值以支持首次启动，但尺寸、版本、终止符或字段校验失败时必须 fail closed：composition root 在启动 radio、配对 portal 或任何携带 token 的请求前进入可诊断降级，**不得**退回默认凭据继续联网。当前服务尚不是 Credential/Security Service；NVS encryption、secret 零化、凭据轮换/撤销、provisioning 会话认证与完整 migration journal 仍是后续独立交付项。
- Fangtang 的 Wi‑Fi/4G 启动选择已并入同一 `Configuration Service` 快照，而非保留板级 `net_transport` writer。GPIO0 双击仍只属于 Fangtang board adapter；adapter 将其转换为“选择 Wi‑Fi 或 cellular”的归一化意图后，由 Configuration Service 单次提交。旧 MaClaw scalar key 及厂商 `network/type` 仅在尚无快照选择时只读导入；业务/Connectivity Service 不读取 vendor namespace，也不知晓 GPIO0、ML307 或 NVS key。Configuration schema 已从 V1 expand 为 V2；V1 snapshot 与旧 transport key 在提交 V2 前均先校验，任何读取/迁移/提交错误均拒绝改变运行时 uplink。
- 配置写入的 authority 已进一步收紧：portal provisioning 的调用方快照只描述 Wi‑Fi/Hub/pairing 更新，Configuration Service 在同一 mutation lock 内重新读取当前 authoritative 快照并保留 transport-selection；避免“先切换 4G/Wi‑Fi、随后提交一个较早组装的 portal snapshot”把新选择回写丢失。成功重新配网后，持久化旧 token 已在该提交中清空，运行时 `s_gateway_token` 也同步清空，防止 portal teardown/retry 窗口继续使用已失效凭据。完整的 immutable revision/observer apply-rollback 仍待后续 Configuration/Provisioning Service 拆分实现。
- 启动阶段对一次性 `force_setup` 采用与凭据快照相同的 fail-closed 规则：该 take-and-clear 操作读取或提交失败时不再仅记录 warning 后继续启动 STA/4G/Hub，而是停留在 USB 可诊断降级状态。这样可避免写失败后的配置请求被悄然丢失，或在 snapshot I/O 故障时以不确定的 setup/credential 状态联网。
- 首次刷写必须使用完整 `flasher_args.json` 的 bootloader + partition table + model + app + storage，不得只写 app；刷后应以真实 chip/flash/layout/compat 读取校验，再抓取首启日志和屏幕。

## 实施增量：Provisioning Portal HTTP 生命周期收口（2026-08-09）

### 后续加固：DNS readiness 与配网敏感暂存区（2026-08-09）

- Captive DNS 不再以“任务已创建”代表可用：DNS worker 在 `bind(UDP/53)` 成功后才发布一次性 readiness；Portal 在该结果到达前不继续启用 SoftAP/HTTP 表单。bind、任务创建、Registry 或 readiness 等待失败都经过既有 stop/join 恢复路径，避免手机拿到 AP 后发现 DNS 截获实际上从未成功。
- Portal startup-failure 只有在 HTTP 已停止、DNS 已 join 后才回收会话暂存。保存/删除表单的 PSRAM body 先使用 `mbedtls_platform_zeroize()` 清零后释放；SSID scan/options/saved-page 暂存也在该安全点归还，避免失败配网将 EAP/密码 form body 长期保留在普通堆中。
- 若 HTTP 或 DNS stop 失败，仍保持 fail-closed 且保留相关资源，不在未知 handler/worker 存活状态下释放其引用的暂存区或 power lease；当前不把这条失败路径宣称为可重启的完整 AP/STA transaction。
- 已重新构建 Bread Compact、EchoEar-2ST、Fangtang-4G、Waveshare 1.75C：app 分别为 `0x32f3a0`（余 12%）、`0x31b460`（余 14%）、`0x32b2f0`（余 13%）、`0x34b7f0`（余 34%）。

本次只收口 Captive Portal 中可独立验证的 HTTP Server 资源边界，不把它误报为完整 Wi-Fi/Provisioning Service：

- `main.c` 为 `s_setup_server` 增加受临界区保护的 HTTP admission gate。HTTP server 创建后、路由完整注册前保持拒绝；完整成功后才开放。GET、captive probe、刷新、保存和删除处理器在关闭后统一返回 `503` 并关闭连接，因此停止窗口不能再接收凭据或配置变更。
- 新增 `stop_setup_portal_http_server()`：先关闭 admission，再调用 ESP-IDF `httpd_stop()`，成功后才清除 handle；失败时保留原 handle 并维持 fail-closed，拒绝创建第二个同端口 listener。路由注册失败和已有 startup-failure recovery 都经由该路径处理。
- HTTP stop 失败时，portal recovery 不再继续释放 DNS、power lease 或恢复 wake word；避免仍在运行的 HTTP handler 使用已释放的 portal 状态。此处不对 `httpd_stop()` 作虚假的 timeout 承诺，因为 ESP-IDF API 没有 caller-controlled deadline。
- 已分别构建 Bread Compact、EchoEar-2ST、Fangtang-4G 与 Waveshare 1.75C：app 分别为 `0x32f0d0`（余 12%）、`0x31b190`（余 14%）、`0x32b040`（余 13%）、`0x34b510`（余 34%）。构建仅保留既有 `load_cached_pet_asset` 未使用告警及 ESP-IDF/Kconfig warning，无编译错误。
- 尚未完成：AP/STA、DHCP、DNS、event handler、SNTP 与 Wi-Fi driver 的统一 Provisioning/Connectivity owner、可验证 deinit/restart、portal 会话认证/TTL/限流/物理取消和 secret zeroization。它们仍由 `main.c` 编排；因此不可因本次 HTTP 生命周期收口声称完整配网 stop/join 或完整 Wi-Fi restart。

## 实施增量：保存配网后的 Portal 收口（2026-08-10）

此前 `POST /save` 在 HTTP 响应返回后只等待约 1.2 秒便直接 `esp_restart()`。这能让新凭据在下次启动时生效，但把 HTTP listener、captive DNS worker、逻辑 provisioning session、配网页面暂存区和 provisioning power lease 的回收完全交给复位；因而无法证明“保存成功到复位”的终端转换没有留下仍接受请求的 portal worker。

- `setup_restart_task` 现在在可取消的响应 flush 延迟结束后，先调用既有 `stop_setup_portal_transaction(500, false)`，按 `HTTP admission -> httpd_stop/join -> DNS stop/join -> end provisioning -> release scratch/lease` 顺序收口，再执行既有的受控重启。
- 该收口不改变凭据保存和重启应用配置的产品语义；也没有把 AP/DHCP/STA/netif/Wi-Fi driver 误实现为运行时 `APSTA -> STA` 切换。它们由紧随其后的整机 reset 接管。若 HTTP/DNS 有界收口报错，admission 已关闭；已成功提交的新配置不应停留在旧 portal generation 中，因此记录错误后仍进入该既定 reset 路径。
- `stop_setup_restart_task()` 的取消路径保持原有语义：stop token 会在 portal cleanup 前退出，启动回滚先通过 Task Registry 停止该 coordinator，故不会与启动回滚的 `stop_setup_portal_transaction()` 并发执行第二次正常收口。
- 已重新构建共享 `main.c` 的 Waveshare 1.75C：`build-unified-waveshare-provisioning-cleanup-20260810.log`，app `0x351440`，32 MiB factory app 分区余 34%；并重新构建 Fangtang-4G：`build-unified-fangtang-lifecycle-20260810.log`，app `0x32f9c0`，16 MiB app 分区余 12%。构建通过只证明 profile 编译/链接，不替代 portal 保存、HTTP/DNS drain 或 AP 消失的实机验证。

仍待 HIL/故障注入：在 Wi-Fi 和 4G 配对恢复两种 portal 模式下提交 `/save`，确认响应先可见、HTTP/DNS 随后停止、`SERVICE_STATUS` 不会在 reset 前恢复为可交互，并在 `httpd_stop` 或 DNS join 超时注入时确认 admission fail-closed、仅一次 reset、重启后凭据和配对状态正确。完整 Provisioning Service、可验证的无重启 radio rollback、portal session 认证/TTL/限流、物理取消和 secret zeroization 仍是后续工作，不能由本增量声称完成。

## 实施增量：Power Lease（2026-08-07）

已完成 `DISPLAY_OFF` 的首个共享前景占用闭环，范围严格限定为“禁止面板/背光熄灭”，不涉及 MCU light/deep sleep、RTC 唤醒或电源轨控制。

- 新增内部 `power_lease_service`：固定 8 槽、无堆分配、带 generation 的不透明句柄；过期句柄不能释放被复用的槽位。
- 共享 Device API 仅公开 owner、获取/释放和只读 snapshot；不暴露 FreeRTOS、定时器、GPIO、板型或显示驱动细节。
- Power Service 在物理 `DISPLAY_OFF` commit 前统一检查 lease；若有前景 lease，则保留原 idle 请求并每秒重新判定，而不是丢弃 deadline 或让业务重新猜测/重建定时器。
- 已接入：闹钟响铃、语音交互（采集、上传、等待结果/结果展示）、前景会议录音及上传、WAV 与流式 MP3 播放。各终态路径均释放自己的 lease；闹钟在 lease 槽异常耗尽时仍按安全优先继续响铃并记录诊断。
- 板级 `board_port_enter_display_off()` 仍保留对当前渲染场景的最后资格检查；Power Lease 只决定业务是否允许熄屏，绝不下沉为 board-port 中的全局业务状态。
- 已用三 profile 独立构建并刷入实机：EchoEar-2ST/COM3、Bread Compact/COM4、Fangtang-4G/COM5；三次 app 写入均经 esptool hash 校验。重启日志均显示 `power service ready`、`sleep_schedule service ready`、`BOOT_STATUS.ready=true`，未见 panic 或 stack overflow。
- 当前构建尺寸：EchoEar `0x323300`（余 `0x7cd00`，13%）、Bread `0x3184b0`（余 `0x87b50`，15%）、Fangtang `0x31cf60`（余 `0x830a0`，14%）。

仍待验证：在 Hub 恢复后，用真实一次性睡眠窗口覆盖“前景 lease 阻止熄屏 → 终态自动熄屏”、闹钟优先、首次实体键/触摸只唤屏、跨午夜窗口和手动 wake override。不得据此宣称已支持 light/deep sleep、RTC timer wake、Wake Deadline Service、Power Lease 的过期 deadline/优先级策略或功耗指标。

## 实施增量：按逻辑 worker 取消蜂窝 HTTP（2026-08-09）

Fangtang-4G 的 ML307 之前只维护一个“foreground request”指针；前景语音能够取消，但会议 WAV 分块上传是后台/音频 worker，停止或生命周期收敛只能等待最长 60 秒请求超时。这既让 `Task Registry` 的有界 join 缺少实际取消通道，也迫使业务层认识调制解调器的特殊性。

- 在统一 `Device API → Platform Connectivity → board port` 契约中增加不透明的 `cancellation_owner` 和 `device_connectivity_cancel_cellular_requests_for_owner()`。它的语义是“取消某个逻辑 worker 当前拥有的蜂窝请求”，而不是暴露 UART、HTTP ID、GPIO 或 ML307 句柄；无蜂窝 profile 均显式 no-op。
- ML307 adapter 仅在同步请求的存活期登记 owner；登记表受 adapter 私有 mutex 保护。取消只置 request 的原子 stop token、唤醒等待者和 slot waiter，调用方不会在取消任务中等待 `MHTTPCREATE` 或 UART。请求自身从同步调用返回时才完成 `MHTTPDEL`、URC callback 注销、HTTP slot 归还和 borrower drain，避免取消期间 create/析构的 UAF、slot 泄漏或锁顺序反转。
- `meeting_worker` 的蜂窝分块 PUT 以自己的 task handle 登记为 owner；`stop_meeting_task()` 在停止 token、Wi-Fi client cancel 之后通过同一 HAL 发出 owner cancellation。这样会议录音/上传、前景语音及未来 provisioning worker 可各自使用统一的协作取消边界，前景语音仍保留既有 foreground-cancel 快径，不和会议竞争单一指针。
- Fangtang profile 已重新构建：app `0x32c9b0`，16 MiB app 分区余 `0x73650`（12%）。构建仅保留既有未使用函数和 ESP-IDF/Kconfig warning，无新增编译错误。

仍待 HIL：在 COM5 上启动实际会议上传后触发生命周期 stop，确认请求在一个有界 join 期限内返回、保留 WAV/recovery metadata、随后可续传；同轮验证普通前景语音取消不误伤会议 owner。此构建证据不替代真实 ML307 网络、MHTTPCREATE 临界区与恢复续传的实机验证。

## 实施增量：冷启动失败时的 Wi-Fi/SNTP 资源回收（2026-08-09）

此前启动回滚只能停止 portal HTTP/DNS 与若干 registry worker；Wi-Fi driver、默认 netif、应用事件回调、默认 event loop 与 SNTP client 仍长期驻留。因此它不能作为可验证的资源回收边界，也不能把“任务停止”误报成网络栈已停止。

- 组合根现在保存自己注册的三项 Wi-Fi/IP event handler instance，以及 STA/AP 的 default Wi-Fi netif 指针；不触碰 default netif helper 自己注册的系统 handler。
- 新增只用于**失败冷启动回滚**的 `stop_network_core_transaction()`：先有界停止 clock-sync monitor 并 deinit SNTP，再停 Wi-Fi，注销本应用 event handlers，销毁 STA/AP default netif，随后 deinit Wi-Fi、删除 default event loop 与 `esp_netif` core。每一步失败都保留仍拥有的状态并立刻返回，不将半拆卸状态宣称为可重启。
- 正常配网结束仍只执行 portal HTTP/DNS/session/scratch 的窄 stop；不会在日常配网切换中销毁 radio。完整运行时 Wi-Fi restart、AP/STA/DHCP 的事务化重建与全链路 HIL 仍是后续工作，不能由该 cold-start rollback 推断已完成。
- SNTP 的 retry monitor 与 SNTP singleton 明确分离：只有 monitor 已 join 后才调用 `esp_netif_sntp_deinit()`，避免后续 `sync_wait()` / retry 在已经释放的 netif SNTP 上运行。
- Bread Compact 重新构建通过：app `0x331280`，16 MiB app 分区余 `0x6ed80`（12%）。

仍待 HIL：对每个 Wi-Fi profile 注入 Gateway/SNTP/portal 启动阶段失败，确认回滚日志的顺序、无 late callback、重新冷启动可正常联网上线；此处没有声称运行时 restart、APSTA→STA 往返或 `esp_event_loop_delete_default()` 的实机回归已经完成。

## 实施增量：Wi-Fi 初始化失败的可逆事务（2026-08-09）

冷启动回滚仅在后续阶段失败时才有机会执行；若 `esp_netif_init()`、default event loop、`esp_wifi_init()` 或某一个应用 event-handler 注册本身失败，旧代码使用 `ESP_ERROR_CHECK`，会直接 panic/reboot，既绕过退化诊断，也可能在下一次启动遇到相同失败而形成循环。

- `init_network_core()` 与 `init_network()` 现在返回 `esp_err_t`，只在对应资源集合完整建立后发布 initialized 状态；调用方（STA 启动和 provisioning portal）检查错误并回到既有失败恢复/诊断路径，不再把初始化失败变成 panic。
- default event loop 创建失败时立即逆序释放已建立的 `esp_netif` core；Wi-Fi driver 或任一应用 handler 注册失败时复用受限的 `stop_network_core_transaction()`，依次注销已登记实例、deinit driver、event loop 与 netif。成功清理后可从干净冷启动重新尝试；任一步清理失败仍保留真实 handle/flag 并 fail-closed，不把半拆卸状态当作可运行的 restart。
- AP/STA default netif 创建也改为显式检查：portal 与 STA 入口若没有拿到所需 interface，不继续调用 DHCP、scan、`esp_wifi_set_mode()` 或连接 API。
- 已分别重新构建：Bread Compact app `0x331330`（余 `0x6ecd0`，12%）、EchoEar-2ST app `0x31d3a0`（余 `0x82c60`，14%）、Fangtang-4G app `0x32d1a0`（余 `0x72e60`，12%）、Waveshare 1.75C 也完成 profile build。构建仅保留既有未使用函数与 ESP-IDF/Kconfig 提示，无新增编译错误。

仍待 HIL/故障注入：逐点注入 netif、event loop、Wi-Fi driver、三个 handler 与 AP/STA netif 创建失败，核对回滚日志、handle 状态和下一次冷启动；这仍不是完整 Provisioning/Connectivity Service，也未实现正常运行中的 Wi-Fi restart 或跨 APSTA/STA 事务化重建。

## 实施增量：Wi-Fi 运行配置失败的非 panic 降级（2026-08-09）

前一增量只覆盖 driver 之前和 handler 注册阶段；STA 选择模式/协议/配置、开始 radio、重连、已保存候选切换，以及企业 Wi-Fi EAP 参数仍包含 `ESP_ERROR_CHECK`。这些 API 失败会让设备在已经拥有诊断 UI 与可回滚资源时仍直接 abort，不符合“失败分类为可诊断降级，而非 boot-loop”的生命周期约束。

- 已将上述 Wi-Fi runtime-configuration 调用改为显式错误返回：失败时保持 Connectivity readiness 为 false，写入可诊断日志并返回既有重试/配网策略；不会再因一个 station 或 EAP 配置 API 的错误立即重启。
- 多已保存网络扫描的 `esp_wifi_start()` / 候选 `esp_wifi_set_config()` 均可失败返回；扫描临时缓冲与自动连接状态在错误路径恢复。普通 STA 的 mode、b/g protocol、config、start/connect 同样显式处理，`ESP_ERR_WIFI_CONN` 仍作为“已有连接过程”的非致命状态。
- 企业 Wi-Fi 设置保持 driver 细节在 connectivity adapter/composition root，不泄露到业务层；identity、username、password、TTLS phase2、CA bundle、domain、EAP method 与 enterprise-enable 任一失败都会停止本次连接。只有已经成功 enable 的 enterprise state 才尝试 disable，规避该 target 在 cold personal Wi-Fi 上错误 disable 的已知稳定性问题。
- SoftAP MAC 读取也从 abort 改为 portal 失败恢复。此改动不改变 Wi-Fi/4G 选择、业务行为或屏幕形状。
- 已重新构建 Bread Compact app `0x331240`（余 `0x6edc0`，12%）和 Fangtang-4G app `0x32d580`（余 `0x72a80`，12%）；仅保留既有未使用函数与 ESP-IDF/Kconfig 提示。

仍待：将 AP 配置/DHCP/portal 的每个 `esp_wifi_*` 调用同样转入完整 Provisioning Service transaction；再对 personal、PEAP、TTLS、候选网络、driver start/connect 失败做 HIL/故障注入。当前成果不代表 enterprise 凭据安全会话、完整运行时 restart 或 provisioning 安全模型已经完成。

## 实施增量：Captive AP 的 DHCP 前置事务（2026-08-09）

配网 portal 过去会在 DHCP 配置失败后只记录 warning，仍继续启动 captive DNS、SoftAP 与 HTTP。手机可能接入一个 SSID，却拿到错误网关/DNS 或没有 DHCP lease，形成“界面显示配网已启动、实际上不可用”的半成功状态。

- `configure_setup_ap_ip()` 现在是显式 `esp_err_t` 事务：停止 DHCP、设置 `192.168.4.1/24`、设置 DHCP DNS option、DNS 地址、captive portal URI（DHCP option 114）并启动 DHCP，任一步失败都返回原始错误。
- portal 在 DNS worker、AP radio、HTTP listener 之前验证此事务。失败走现有 HTTP/DNS stop、session end、scratch release、power-lease release 与 wake-word 恢复路径；不会再发布配网热点或接受凭据。
- 该边界仅负责 IP/DHCP advertisement 正确性；不把 AP/STA driver destroy、DHCP 运行时 restart 或 portal session authentication 误称为已完成。
- 已构建 Bread Compact app `0x331300`（余 `0x6ed00`，12%）与 Waveshare 1.75C app `0x34d6c0`（余 `0x1b2940`，34%）；无新增编译错误，保留既有未使用函数与 ESP-IDF/Kconfig 提示。

仍待 HIL/故障注入：分别模拟 DHCP stop、IP、DNS、option 114、DHCP start 失败，确认不会出现可关联但无可用 portal 的 AP，且同次回滚不遗留 DNS socket、HTTP handler、session scratch 或 power lease；完整 Provisioning Service、安全会话与 AP/STA 运行时事务仍待实现。

## 实施增量：配网启动失败的无线电形态回滚（2026-08-09）

即使 HTTP/DNS/session 暂存能停止，portal 在其后已经可能把一个正在工作的 STA 改为 APSTA、断开 station，或从未启动的状态启动 AP。旧失败恢复没有记录这些无线电变更，因此 HTTP 路由注册/内存失败后可能留下没有管理服务的开放 AP，或让原来的 station 停止自动恢复。

- portal 在 Wi-Fi driver 初始化完成后、修改 station policy 前捕获 radio snapshot：原 Wi-Fi mode、是否已 start、自动连接与预期断开标志。首刷/首次配网的 driver 尚未初始化时不查询 mode，避免把正常的首次 AP 启动误判为错误。
- 自开始改变 station policy 或 radio mode 起，失败恢复先 stop/join HTTP 与 DNS、释放 logical provisioning session/scratch/lease；只有这些用户态 borrower 已退出，才恢复 radio。原 radio 未运行则停止新启动的 AP；原 radio 已运行则恢复原 mode 与 station policy，并在原自动连接策略允许时重新 connect。恢复动作失败保留 fail-closed 状态并记录，不伪报 portal 已恢复。
- 这仍是 portal **startup-failure** 的补偿事务，不改变正常 portal close 行为，也不销毁 netif/driver/event loop；并非完整 APSTA↔STA runtime restart 或 Provisioning Service。
- 已重新构建 Bread Compact app `0x331680`（余 `0x6e980`，12%）与 EchoEar-2ST app `0x31d6f0`（余 `0x82910`，14%）；无新增编译错误，保留既有未使用函数与 ESP-IDF/Kconfig 提示。

仍待 HIL/故障注入：在已关联 STA 和首次无凭据两种起点下，分别使 AP config、AP start、HTTP start、route registration 失败，确认不存在无 handler 的 AP beacon、原 station 能遵从既有重连策略恢复，且 stop 失败时不会继续 radio restore；完整 AP/STA/DHCP 可逆 transaction 仍待后续统一 Provisioning owner 实现。

## 实施增量：Configuration Service 的停止边界（2026-08-09）

Configuration Service 已从散落 NVS key 收敛了 Wi-Fi、Hub、pairing、音量和 transport 选择，却只有 `init`，没有在冷启动回滚中关闭 admission 或归还自己的 PSRAM scratch。因而 DEGRADED 路径仍保留可变配置服务的运行资源，也无法证明后续工作不会误用一个正在被释放的 persistence worker。

- 新增 `configuration_service_deinit(timeout_ms)` 与 `configuration_service_is_initialized()`：deinit 先关闭 mutation admission，再有界取得唯一 mutation lock，随后释放三块 Configuration-owned PSRAM scratch（两个 snapshot 与 store）并清除默认快照。新的配置调用会确定性返回错误，不会在 rollback 后继续发起 NVS 路由请求。
- admission mutex 故意不在 deinit 时删除：一个任务可能已在关闭瞬间采样 mutex 并等待，删除 FreeRTOS mutex 会制造 UAF。它作为极小的 service shell 保留，下一次 `init` 仅在完整重新分配 scratch 后再打开 admission；这不是把旧 runtime state 复活。
- startup rollback 将 Configuration deinit 放在 Connectivity、Storage 与 Interaction worker 完成 stop 后，且在继续释放依赖资源前。超时保持 scratch/lock 与 fail-closed 状态，不报告成功。
- 已重新构建 Bread Compact app `0x331810`（余 `0x6e7f0`，12%）和 Fangtang-4G app `0x32da90`（余 `0x72570`，12%）。构建无新增错误；Fangtang 仍有原有未使用 renderer 函数、项目既有未使用函数与 ESP-IDF/Kconfig 提示。

仍待：为 Configuration Service 设计 immutable revision/observer apply-rollback 与完整 migration journal；对 portal save、音量 writer、transport switch 和 Config deinit 并发做故障注入/长稳验证。Persistence Service 的 queue/mutex 析构、完整 service graph stop/deinit 及运行时 restart 也仍未完成，不能由此声称全链路生命周期已闭合。

## 实施增量：Persistence Service 的可 drain 停止与资源回收（2026-08-09）

Persistence Service 过去在 worker 收到 stop sentinel 后只退出 task；其 queue、路由 mutex、completion semaphore 和静态请求槽仍被保留，`is_initialized()` 也只检查 composition root 提供的 NVS mutex。这既不能可靠地区分“已停止”和“可调用”，也无法保证冷启动回滚后队列不会被残留调用方访问。

- 新增 service admission/drain 边界：每个公共 NVS 操作（含内部栈 inline 与 PSRAM 栈路由）在进入时登记 active call；deinit 先关闭 admission，再等待已有调用到安全边界。关闭之后的新调用确定性返回 `ESP_ERR_INVALID_STATE`，不再向即将释放的 worker queue 提交请求。
- PSRAM 栈路由改为每请求独立的 job/completion semaphore 与双引用所有权。worker 和发起方各自释放一次，避免旧共享 `route_done` token 被下一个请求误消费，也避免 static request 槽保留已返回调用方的栈指针。队列只携带 job 指针与 operation tag，worker 在完成 NVS transaction 后通知、释放 job。
- 成功 drain、stop/join 后删除 service-owned worker queue 与 stopped semaphore，并清空 worker handle；composition root 注入的 NVS transaction mutex 以及 lifecycle/deinit admission shells 不删除，防止关闭瞬间已等待这些 mutex 的任务发生 FreeRTOS UAF。下一次 init 必须重新创建 worker resources 后才能 reopen admission；一个超时的关闭 generation 不能被重新打开。
- `persistence_service_is_initialized()` 现在同时要求 admission、worker task、queue 与 stop completion 都有效；startup rollback 在 Storage registry stop 后再次调用 idempotent service deinit，使 worker 自注销与 registry stop 交错时仍能完成资源回收，再继续 Configuration scratch 的释放。
- 已构建 Bread Compact app `0x331b80`（余 `0x6e480`，12%）和 Fangtang-4G app `0x32dd70`（余 `0x72290`，12%）。构建无新增错误；项目既有未使用函数与 ESP-IDF/Kconfig 提示仍在。

仍待 HIL/故障注入：需让内部栈 NVS transaction、PSRAM 路由 request、queue 满、worker stop 与 registry self-unregister 分别交错，验证 timeout 始终保持 fail-closed 且不泄露 job；仍未实现完整运行时 service graph restart、NVS migration journal 或所有业务 owner 的 restart reconciliation。

## 实施增量：Wake Deadline Dispatcher 的停止 admission（2026-08-09）

Wake Deadline 是 Alarm 与 Sleep Schedule 共享的 wall-clock dispatcher。旧实现会在 deinit 成功 join 后删除 `s_lock`；一个已经检查过 initialized、但正在等待该 mutex 的 Alarm/Sleep/Clock 调用可能随后取得已释放 FreeRTOS 对象，构成 rollback 路径的 UAF 风险。并发 deinit 也可能争用同一 stopped semaphore。

- dispatcher 的 public lock 改为永久 static lifecycle shell；deinit 在持锁时先关闭 `s_initialized`/`s_stop_requested` admission，所有 register/arm/cancel/unregister 都在取得同一 lock 后重新检查状态，旧 waiter 只能观察到 closed service，不能访问已释放 timer/slot。
- 新增独立 static deinit lock 串行化“stop → join → delete timer → reclaim completion”事务。超时会保持 closed、保留 task/timer/completion generation；后续 deinit 可消费已退出 worker 的最终 completion 并完成回收，init 不会在旧 STOP generation 未收口时创建第二个 dispatcher。
- deadline slot、timer 与 worker completion 只在 worker 已 join、timer delete 成功后回收；client（Alarm、Sleep Schedule）仍先在 parent dispatcher 可用时 cancel/unregister 自己的 slot，再由 startup rollback 关闭 dispatcher，未改变业务层闹钟/休眠语义。
- 已重新构建 Bread Compact app `0x331cd0`（余 `0x6e330`，12%）和 Fangtang-4G app `0x32df50`（余 `0x720b0`，12%）；仅有项目既有未使用函数与 ESP-IDF/Kconfig 提示。

仍待 HIL/故障注入：需交错 timer callback、Alarm/Sleep slot mutation、clock update、client stop 与 dispatcher stop，确认静态 shell 不造成旧 generation callback 复活；Alarm Manager 和 Sleep Schedule 自身的 mutex 析构/运行时 restart reconciliation 仍须独立收口，不能由本增量推断完整 Power/Deadline 生命周期已完成。

## 实施增量：Power Service 的 DISPLAY_OFF timer 停止事务（2026-08-09）

Power Service 的 idle timer 与用户唤醒、UI 重排、Alarm/Sleep deadline 并发。旧 deinit 虽尝试 stop timer 后取得 transition mutex，但 schedule/cancel/wake 调用可以在采样旧 timer handle 后等待 transition mutex；deinit 删除 timer 后，这类 waiter 仍可能对已删除 handle 调用 `esp_timer_*`。同时两个 deinit 调用会竞争同一个 timer delete，没有单独 owner。

- 新增 static deinit mutex，串行化 `close admission → stop timer → 等待 transition owner → delete timer → clear handle`。关闭期间 init 返回 busy，成功 delete 后才重新允许下一 generation init；timeout 或 timer delete failure 保持 closed/fail-closed，并保留原 handle 供后续 deinit 继续收口。
- schedule、cancel 与两条 wake 路径在取得 transition mutex 后，重新确认 `initialized && !stopping && sampled_timer == current_timer`；失败只返回 busy/no-op，不再触碰旧 generation timer。timer callback 在重排 idle deadline 前同样重检 service/timer admission，避免 deinit 与 power lease defer 交错时重启已关闭 timer。
- transition mutex 与 deinit mutex 均为 static shell，不在 rollback 期间释放；晚到调用只能看到关闭状态，避免 FreeRTOS UAF。此服务仍只治理 `DISPLAY_OFF`，不把 panel off 误称 MCU light/deep sleep，也不改变 Hardware HAL 的实际唤醒路径。
- 已重新构建 Bread Compact app `0x332010`（余 `0x6dff0`，12%）和 Fangtang-4G app `0x32e150`（余 `0x71eb0`，12%）；仅保留既有未使用函数与 ESP-IDF/Kconfig 提示。

仍待 HIL/故障注入：需覆盖 callback 正在等待 LCD DMA、用户触摸/实体键唤醒、UI schedule/cancel、lease defer、timer delete failure 与连续 init/deinit；目前没有证明所有板卡 display driver 的运行时 deinit/reinit 或深睡事务已经完成。

## 实施增量：Sleep Schedule 的停止 admission 与 deadline client 回收（2026-08-09）

Sleep Schedule 既有内部 task，又会从 Gateway tool、UI manual wake、clock update 和 ambient-delay 查询进入；旧 deinit 只通知 worker 后直接删除 mutex/completion，没有等待 tool mutation 或已排队的 caller，也可能在 deadline dispatcher 关闭前留下 client slot。

- schedule service 现在使用 static public mutex 与独立 static deinit mutex。deinit 先在 public lock 下关闭 `initialized/stop_requested`，取消自身 deadline、join worker，再 drain 已获准的 tool call；晚到的 Gateway/UI/clock 调用取得永久 shell 后会重检 closed state，不会触碰被回收的 task/deadline/store。
- `sleep_schedule_service_execute_tool()` 使用明确 admission count，所有正常、重放、参数错误、锁超时和内存错误路径均 release；deinit timeout 保持服务 closed、保留 worker/completion generation，后续 deinit 才能完成收口，init 不会在旧 generation 未回收时重建。
- worker 在取得 schedule lock 后二次检查 stop，避免关闭发生在等待 mutation 的间隙时仍提交 Persistence 写入或重新 arm shared deadline。manual wake 与 clock-update 的 task notify 也在短 critical section 中快照已验证 task handle。
- worker 已 join、tool admission drain 后才 unregister deadline slot 与释放 completion semaphore；public mutex 保留为 lifecycle shell。当前业务行为仍只可请求 `DISPLAY_OFF`，不扩大为 light/deep sleep。
- 已重新构建 Bread Compact app `0x3323a0`（余 `0x6dc60`，12%）和 Fangtang-4G app `0x32e3f0`（余 `0x71c10`，12%）；仅有项目既有未使用函数与 ESP-IDF/Kconfig 提示。

仍待 HIL/故障注入：需交错 idempotent tool 写入、Persistence timeout、manual wake、SNTP clock correction、deadline callback 与 deinit，确认 timeout 后不产生孤儿 client slot、重复通知或旧 generation mutation；Alarm Manager 自身的完整 mutex lifecycle/restart reconciliation 仍待独立实现。

## 实施增量：Alarm Manager 的停止 admission 与持久化提醒恢复边界（2026-08-09）

Alarm Manager 的 worker 可在 deadline 回调、正在响铃、snooze 或 Persistence commit 中运行；同时 Gateway 工具、UI dismiss 与 startup rollback 并发。旧 deinit 虽有 tool admission count，但成功后删除 `s_lock`，已等待的调用可能取得释放后的 mutex；init/deinit 也没有独立事务锁，deadline callback 可在关闭瞬间通知旧 task。

- Alarm 的 store mutex 和 deinit mutex 改为 static lifecycle shell。deinit 在取得 store lock 后先关闭 `initialized/stop_requested` admission、cancel own deadline，再 join worker、drain 已获准 tool；晚到 tool/deadline callback 重新确认 closed state，只返回 `INVALID_STATE` 或 no-op，不使用旧 worker、slot 或 store lock。
- deinit 由独立 deinit mutex 串行化，成功 worker→completion handoff 后才注销 shared deadline slot 与释放 stopped semaphore。超时保留 closed generation，后续 deinit 才可继续；init 拒绝在旧 worker/stopped generation 未收口时重建，防止两个 alarm task 或重复 deadline slot。
- active alarm 的 durable recovery 语义不变：停止不把正在响铃的提醒视为 dismiss，持久化 `active_alarm` 仍会在下一次完整初始化时回到队列；此次更改仅隔离生命周期，不改变闹钟业务处理、重放缓存或 UI/audio lease 策略。
- 已重新构建 Bread Compact app `0x3325a0`（余 `0x6da60`，12%）和 Fangtang-4G app `0x32e570`（余 `0x71a90`，12%）；仅有项目既有未使用函数与 ESP-IDF/Kconfig 提示。

仍待 HIL/故障注入：需覆盖 deadline callback 与 stop 同时发生、ring/snooze 时 shutdown、tool 持有 NVS transaction、active-alarm recovery、timeout 后二次 deinit/init 及 UI/audio lease 释放；不得由当前构建通过推断闹钟的完整 runtime restart 和四板 HIL 已完成。

## 实施增量：Fall Detection 可选传感器服务的停止事务（2026-08-09）

Fall Detection 仅在具备 Motion HAL 的 profile 上运行（当前是 Waveshare），但它有独立 classifier task、Gateway tool mutation、presentation lease 和 callback。原实现可由两个并发 deinit 竞争同一个 stopped semaphore/mutation mutex；callback 也会在 service fields 解锁后直接读取 callback/context，无法说明停机交错时的所有权边界。

- 新增 static deinit mutex，串行化可选传感器服务的 init/deinit。deinit timeout 保持 closed generation，后续 deinit 可消费 worker 在超时后才给出的 completion；init 发现 stopping/旧 worker 时返回 busy，不创建第二 classifier task 或第二 mutation mutex。
- callback/context 在短 state lock 内与 event generation 一起快照，随后才在锁外执行 UI callback；deinit 关闭 monitoring、递增 generation、释放 confirmation lease 后清除 callback，旧候选不会在 disable/stop/restart 后以新服务身份显示。
- task-create failure 现在完整回滚 initialized/callback 状态；tool admission drain、mutation mutex 的有界获取和 worker join 仍保持 fail-closed。没有 Motion HAL 的 profile 仍不创建 worker，返回 `UNAVAILABLE`，不把板级 QMI8658/I2C 细节泄漏到业务层。
- 已重新构建 Bread Compact app `0x332760`（余 `0x6d8a0`，12%）与 Waveshare 1.75C app `0x34ebd0`（余 `0x1b1430`，34%）。构建无新增错误，仅保留项目既有 warning/ESP-IDF Kconfig 提示。

仍待 HIL/故障注入：需在 COM6 交错采样 I2C error、候选确认 callback、disable tool、lease exhaustion、task stop timeout 和连续 init/deinit；还需基于实际佩戴/放置样本做阈值校准。当前能力仍仅报告“设备疑似跌落”，不是人员跌倒诊断、SOS 或医疗功能。

## 实施增量：Resource Pressure 的 Storage/VFS 观察停止边界（2026-08-09）

Resource Pressure 本身没有 worker，但会在任意可选媒体/宠物下载路径中调用 `esp_spiffs_info()` 查询 storage label。旧服务只有 init，没有在冷启动 rollback 中关闭 observation；如果后续 Storage/VFS owner 卸载或重建，晚到的 optional-work gate 可能仍对旧 label 取容量，把错误数据当作正常资源事实。

- 新增 `resource_pressure_service_deinit(timeout_ms)`，采用 static public/deinit locks 串行化 observation 生命周期。deinit 关闭 initialized/storage admission、清空 label、将内部状态收敛为 fail-closed；后续 `get_snapshot`/optional allocation/work gate 直接失败，不再调用 SPIFFS。
- public mutex 保留为 lifecycle shell。已经采样旧 ready 状态的调用在取得 mutex 后必须重新检查 `initialized/stopping`，不会在 deinit 成功后访问旧 VFS。init 与 deinit 交错时由独立 deinit lock 串行化，下一 generation 只在上一观察上下文清空后发布。
- startup rollback 现在在 Storage worker stop 与 Configuration scratch 释放之间显式停止 Resource Pressure；它仍不卸载 storage、不拥有 SPIFFS、不决定业务降载策略，只负责把由 HAL/VFS 提供的资源事实安全地交给统一可选工作 gate。
- 已重新构建 Bread Compact app `0x332970`（余 `0x6d690`，12%）和 Waveshare 1.75C app `0x34edf0`（余 `0x1b1210`，34%）。构建无新增错误；仅有既有未使用函数与 ESP-IDF/Kconfig 提示。

仍待 HIL/故障注入：需在 optional pet/cache 预检、SPIFFS info error、storage mount state 变化、rollback/re-init 交错下验证水位与 gate；当前 service 仍未覆盖 task stack/queue reservation、thermal、DMA pool、完整 capability hysteresis 或运行时 Storage/VFS restart。
> 2026-08-10 圆屏 codec I²C state 与音频物理事务完全私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：继续按 Phase 6 收口圆屏共享 `board_port.c` 的硬件对象持有。共享 renderer 已删除 `i2c_master_bus_handle_t`、ES7210/ES8311 device handle、codec 寄存器号及 `driver/i2c_master.h` 依赖；`round_audio_codec_adapter.h` 现在私有保存 shared-I²C bus 与两个 codec handle，并以 `initialize(output_volume)` / `release()` 完整处理 bus 创建、touch/PMIC/IMU 先初始化、codec attach、ES7210/ES8311 初始化、I2S、PA 及精确逆序回滚。共享层仅保留会话 mutex、capture/playback/wake-word owner、PCM buffer/时序和语义请求（调音量、mute、恢复输入 gain）；Waveshare 仍严格保持“open bus → peripheral/touch → codec”既有顺序，避免破坏 AXP2101/CST9217/QMI8658 与音频 codec 共线 bring-up。为使 EchoEar touch adapter 自身不依赖共享 renderer 的间接 include，I²C driver include 也归位到 `echoear_hardware_adapter.h`。使用两 profile 既有 `compile_commands.json` 的 Xtensa 命令完成 EchoEar/Waveshare `board_port.c` object 编译，均仅有项目既有 `compose_text16_curve` 未使用 warning；`tools/check-hal-boundaries.ps1` 通过。完整 profile build/link 与 COM3/COM6 HIL 仍不能宣称：IDF Python `C:\Users\ma139\.espressif\python_env\idf6.0_py3.14_env\Scripts\python.exe` 当前不能启动（Windows %1 非有效应用）。环境恢复后，必须运行唯一入口 `tools\build-profile.cmd echoear-2st build`、`tools\build-profile.cmd waveshare-amoled-1.75c build`，并实测人声白噪、音量/静音尾音、命令/会议录音、wake pause/resume、touch 及 DISPLAY_OFF 首触语义。本切片不声称 Audio Service、shared-I²C recovery/restart、long-recording sample clock 或全量 HIL 已完成。
> 2026-08-10 圆屏 display DMA callback / IO handle 私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：继续按 Phase 5 缩小共享 round renderer 的 ESP-LCD 物理耦合。共享 `board_port.c` 已不再保存 `esp_lcd_panel_io_handle_t`、声明 color-transfer callback 或直接调用 `esp_lcd_panel_draw_bitmap()`，并移除了不再需要的 panel-IO/I²C-IO include；它仅持有共享场景所需的 panel facade、渲染 buffer、传输完成 semaphore 与普通坐标/像素数据。各 profile-private display adapter 现在完整拥有 panel-IO 创建、DMA completion callback、ISR semaphore give 和实际 bitmap submit；shared renderer 通过 `round_display_adapter_init_hardware(bytes, completion_semaphore, panel, brightness)` 与 `round_display_adapter_draw_bitmap(...)` 请求操作，不接触 controller callback 签名或 IO handle。改动不更改圆屏的 stripe DMA、全帧/差分绘制策略、屏幕形状、已验证的 EchoEar/Waveshare panel 时钟与亮度时序。EchoEar/Waveshare `board_port.c` 均已以现有 Xtensa compile command 对象编译通过（仅既有 `compose_text16_curve` 未使用 warning），`tools/check-hal-boundaries.ps1` 与 `git diff --check` 通过。完整 link 与 COM3/COM6 显示 HIL 仍待修复 IDF Python 后使用 `tools\build-profile.cmd` 运行；不得把 object compile 误报为 DMA/屏幕实机验证或独立 Display Task 完成。
> 2026-08-10 圆屏 panel handle 生命周期完全下沉（EchoEar-2ST / Waveshare 1.75C，进行中）：在前一切片将 panel-IO/DMA callback 私有化后，继续移除共享 `board_port.c` 对 `esp_lcd_panel_handle_t`、`esp_lcd_panel_ops.h` 及所有 panel reset/init/DISPON/DISPOFF/bitmap 参数传递的直接持有。EchoEar 与 Waveshare display adapter 现各自保存其私有 panel handle，提供无 driver-handle 泄漏的 `ready`、`init(bytes, completion, brightness)`、`draw_bitmap(...)`、brightness、DISPLAY_OFF 与 wake 操作；共享 renderer 仅在其既有场景锁、framebuffer/stripe、completion fence、脏帧失效和 UI 状态机内消费成功/失败语义。此收口保留每块板已有的控制器顺序：EchoEar 仍是 PWM 先关、可延后 ST77916 DISPOFF；Waveshare 仍是 CO5300 DCS retry 与其无独立背光的 DISPON/OFF 次序。EchoEar/Waveshare `board_port.c` 已通过现有 Xtensa 对象编译（仅既有 `compose_text16_curve` warning）；公共 HAL boundary check 与 `git diff --check` 通过。完整 profile link/flash 与 COM3/COM6 显示、亮度、DISPLAY_OFF/wake HIL 仍因 IDF Python 无法启动而待执行；本项不误报 Display Task 生命周期、DMA fault recovery 或圆屏 HIL 完成。
> 2026-08-10 圆屏 Display memory-capability 私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：继续按 Phase 5 将“屏幕驱动可接受哪类内存”留在 profile adapter。共享 `board_port.c` 不再根据 `ambient_overlay_uses_psram`、`MALLOC_CAP_DMA` 或 `MALLOC_CAP_SPIRAM` 选择 frame buffer/曲线文字 overlay 的分配能力，也不直接释放这些 display render buffer；它只请求两帧与一个 overlay 所需的字节数。EchoEar adapter 仍返回 DMA-capable PSRAM frame buffer 与 internal DMA overlay，保持其 ST77916 20 MHz/bounce-DMA 的已验证需要；Waveshare adapter 仍返回 DMA-capable PSRAM frame buffer 与 PSRAM overlay，保持 CO5300 QSPI/PSRAM cache 约束及 466px 头部不会耗尽 internal heap。普通音频/WAV/remote-pet/worker 的 buffer 不属于此 display allocation 小切片，仍保持既有 allocator 和生命周期。EchoEar/Waveshare `board_port.c` 两 profile 的 Xtensa object 编译通过（仅既有 `compose_text16_curve` warning），HAL boundary check 与 `git diff --check` 通过。完整 build/link、资源压力和 COM3/COM6 圆屏 HIL 仍待恢复 IDF Python 后执行；本项不宣称 steady-state allocation、renderer restart 或 memory-pressure fault recovery 已完成。
> 2026-08-10 圆屏 shared input scanner 板型分支清除（EchoEar-2ST / Waveshare 1.75C，进行中）：审计共享 `button_task` 后移除其遗留的 `CONFIG_MACLAW_BOARD_FANGTANG_4G` 预处理分支与 boot-network 状态变量。共享 scanner 现在对所有圆屏一律读取 profile-normalized activate key/touch 状态，再以 profile-private `round_input_adapter_resolve_source()` 获得输入 source；任何 boot-only 输入语义在 action 发布前经 `round_input_adapter_consume_boot_gesture()` 吸收，启动窗口通过 adapter 统一 begin/wait 接口管理。EchoEar/Waveshare 均明确实现“无 boot transport selector、touch 优先否则 other key”的 no-op policy，保持现有单击/双击/长按、native touch double、DISPLAY_OFF 首触亮屏不录音和取消手势时序。这样共享业务 scanner 不再认识非圆屏 Fangtang 4G 启动选择或 board Kconfig；未来需要 boot gesture 的 profile 可只实现私有 adapter。EchoEar/Waveshare `board_port.c` Xtensa object 编译均通过（仅既有 `compose_text16_curve` warning），HAL boundary check 与 `git diff --check` 通过。完整镜像/HIL 仍待 IDF Python 修复；本切片不声称跨 profile input service、Fangtang adapter 或物理按钮 HIL 完成。
> 2026-08-10 圆屏 RGB565 wire-format 私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：继续清除共享 scene renderer 对 QSPI 像素字节序的隐含依赖。共享 `board_port.c` 不再使用 `__builtin_bswap16`、不再提及 ST77916/CO5300 的 MSB-first RGB565 传输细节；它只向 display adapter 请求 `rgb565(r,g,b)` 与 `rgb565_lerp(from,to,amount)` 的已规范化像素值。EchoEar 和 Waveshare adapter 分别保留各自已经验证的 byte swap，以及在逻辑 RGB565 空间解码、插值后重新序列化的相同行为，因此待机渐变、宠物色彩、前景文本和圆屏现有像素布局不应变化。EchoEar/Waveshare `board_port.c` Xtensa object 编译通过（仅项目既有 `compose_text16_curve` warning），HAL boundary check 与 `git diff --check` 通过。仍须在 IDF Python 恢复后完整构建并于 COM3/COM6 对比启动、待机、录音、回复、DISPLAY_OFF/wake 的色彩与无残影；本切片不宣称 framebuffer/pixel-format capability descriptor、golden-image 或显示 HIL 完成。
> 2026-08-10 圆屏 dirty-window 对齐私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：继续把显示控制器 GRAM 约束从共享 delta renderer 移出。共享 `present_pet_frame_delta_sync()` 仍计算相同的像素差异矩形、复制相同的 stripe，并只请求 `round_display_adapter_align_dirty_columns(left,right,width)`；它不再知道 CO5300、AMOLED、two-pixel window 或 odd-column 偏移问题。EchoEar ST77916 adapter 明确保留不调整窗口的实现；Waveshare CO5300 adapter 保留既有的偶列扩展/右界夹紧，避免其 QSPI GRAM 在奇数起止列移位导致宠物残片。双方对象编译均通过（仅已有 `compose_text16_curve` warning），HAL boundary check 和 `git diff --check` 通过。IDF Python 恢复后仍需构建并在 COM3/COM6 反复观察待机宠物/delta、远端宠物、状态更新和 DISPLAY_OFF 后全刷，确认无颜色/像素移位或旧帧残留；本项不宣称 generic renderer 的 full Display Task 分离或截图 golden 已完成。
> 2026-08-10 圆屏 stripe DMA buffer 私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：继续移除 shared renderer 对 ESP-IDF DMA placement 的静态持有。`board_port.c` 不再声明 `DMA_ATTR` staging array 或硬编码 40-row stripe 容量；profile-selection seam 提供选中 display adapter 的 stripe-row budget，EchoEar/Waveshare adapter 各自声明其 internal DMA `uint16_t` staging buffer 并通过 `round_display_adapter_stripe_buffer()` 暴露普通像素指针。共享 renderer 的 full-frame、文字、bitmap、remote-pet 和 dirty-delta 算法仍使用相同 `LCD_STRIPE_ROWS=40`、同一缓冲区、同一 LCD mutex/完成 fence；仅“该数组必须位于何种 memory/属于哪块屏”下沉。EchoEar/Waveshare Xtensa `board_port.c` object 编译通过（仅既有 `compose_text16_curve` warning），HAL boundary check 和 `git diff --check` 通过。环境恢复后须完整 build 并做 COM3/COM6 的启动、长文字、远端宠物、状态画面、录音/回复、delta 与 DISPLAY_OFF/wake HIL；本项不代表 renderer 独立 task、DMA fault recovery 或 steady-state allocation 退出条件已经完成。

> 2026-08-11 紧凑屏 panel/IO 与 DMA completion 私有化（Bread Compact / Fangtang-4G，进行中）：沿用圆屏相同的 Phase 5 边界，将共享 `board_port_bread_compact.c` 中的 `esp_lcd_panel_handle_t`、`esp_lcd_panel_io_handle_t`、ESP-LCD color-transfer callback 及 controller handle 参数传递全部移至选中 profile 的 display adapter。共享紧凑 renderer 只保留场景、framebuffer、LCD mutex 和普通 `draw_bitmap_sync` 请求；Bread adapter 私有保存 ST7789 panel/IO，Fangtang adapter 私有保存 NV3023 panel/IO、PWM backlight、controller DISPLAY_OFF/wake 次序及 `GRAM_Y_OFFSET=80` 的逐行写入。Fangtang 的 adapter 还在每次 `tx_color` 后消费 completion fence，保证 shared framebuffer 不会在 SPI DMA 仍读取时被复用；这保持“逐行、受限传输”的已验证策略，但不把 ESP-LCD callback 或 semaphore 规则泄漏给业务/公共 HAL。Bread 的现有 Xtensa response-file syntax compile 已通过；Fangtang 以历史 profile compile command（补齐 profile wrapper 所需的 power-init 声明及 NV3023 公开头路径）对实际 shared source 作 syntax compile 也通过，仅保留既有未使用 `previous` warning。`tools/check-hal-boundaries.ps1` 与 `git diff --check` 通过。完整 profile link 仍受本机失效的 ESP-IDF Python（`idf6.0_py3.14_env` 无法启动）阻塞，且本项尚未刷入 COM4/COM5；环境恢复后必须使用 `tools\build-profile.cmd bread-compact build` 与 `tools\build-profile.cmd fangtang-4g build`，再分别验证启动、待机/宠物、长文本、远端亮度、DISPLAY_OFF/wake 与 Fangtang 的行式刷新无残影。不得将 syntax compile 误报为 DMA 时序或实机显示 HIL 完成，也不代表独立 Display Task、display deinit/reinit 或深睡事务已经完成。

> 2026-08-11 紧凑屏 raw input 私有化（Bread Compact / Fangtang-4G，进行中）：共享 `board_port_bread_compact.c` 的 scanner 继续唯一拥有 contact-down、短按/双击/长按分类、Input Service callback、熄屏首操作消费和现有 Fangtang 启动窗口的业务时序；但不再读取 `gpio_get_level()`、不再认识 activate/volume GPIO、active-low 极性、控制数量或具体 debounce 常量。选中的 `bread_input_adapter.h` / `fangtang_input_adapter.h` 现在提供同一个 profile-private 原始 snapshot（activate、可选音量键）、初始化、capability 和时间阈值 seam：Bread 保留三个实体键及各自 25/30 ms debounce，Fangtang 明确报告只有 primary control，volume raw state 固定为 released。共享 scanner 因而仅消费普通 `compact_input_raw_state_t`，不会因新增触控或不同按键板而写 GPIO 分支；启动日志也只报告 adapter 名称、规范化 idle 状态和 volume capability。Bread 以现有 Xtensa response-file syntax compile 通过；Fangtang 以历史 profile wrapper 命令对当前 shared source syntax compile 通过（仅既有 `previous` 未使用 warning）；HAL boundary check 与 diff check 通过。完整 link、COM4/COM5 的单击/双击/长按、音量键、启动 4G/Wi-Fi selector、首键只亮屏不录音及 input-stop lifecycle HIL，仍待修复 IDF Python 后执行；该切片不声称 scanner task、Fangtang boot selector 或 Input Service 已完全独立/restartable。

> 2026-08-11 紧凑屏 I²S driver handle 私有化（Bread Compact / Fangtang-4G，进行中）：继续按 Phase 6 收口共享 compact renderer 的物理音频对象。`board_port_bread_compact.c` 已删除 `i2s_chan_handle_t`、RX/TX static handle、`driver/i2s_std.h` 和所有直接 `i2s_channel_read/write/enable/disable` 调用；profile-private `bread_audio_adapter.h` / `fangtang_audio_adapter.h` 分别独占 I²S channel 创建、pin/slot/DMA contract、RX/TX driver handle、PCM read/write 及 playback enable/disable。共享层仍拥有命令/会议/wake-word/playback 的会话互斥、PCM 单声道/立体声转换、software volume、取消、zero-tail 与声学策略，只向 adapter 传递普通 buffer、字节数和 timeout，不暴露 driver handle 给 Device/Platform 公共边界。Bread 和 Fangtang 的当前 shared source Xtensa syntax compile 均通过；Fangtang 仅保留既有 `previous` 未使用 warning；HAL boundary check 与 diff check 通过。完整 link、COM4/COM5 的录音、唤醒词、TTS/闹钟、人声/音量和取消 HIL 待有效 IDF Python 后执行；本切片不声称独立 Audio Service、I²S runtime deinit/reinit、音频 DMA fault recovery 或人声质量验证已经完成。

> 2026-08-11 紧凑屏共享 renderer GPIO 清零（Bread Compact / Fangtang-4G，进行中）：复核上一轮 display/input/audio 下沉后，`board_port_bread_compact.c` 中剩余的唯一真实 GPIO 操作是 Fangtang 4G modem guard/power 预置。该函数现直接转发至既有 profile-private `fangtang_cellular_adapter_prepare_hardware()`；共享 source 删除 `driver/gpio.h`，不再引用 UART/GPIO Kconfig、electrical level、pull 或 settle-delay。Bread 显式仍返回 `NOT_SUPPORTED`，Fangtang 的同一 Connectivity → board-port 行为及其 adapter 内已验证的 guard/power 时序不变。现共享紧凑 renderer 不再直接包含 GPIO、I²S 或 ESP-LCD driver header；剩余 `i2s_channel_write` 文本仅是注释。Bread / Fangtang 当前 source Xtensa syntax compile 均通过（Fangtang 仅既有 `previous` 未使用 warning），HAL boundary check 与 diff check 通过。完整 link 及 COM5 4G/Wi-Fi select、modem power/guard、HTTP lifecycle HIL 仍待 IDF Python 恢复；这不宣称 ML307/UART、modem restart 或 cellular failure recovery 已完成。

> 2026-08-11 紧凑屏离线唤醒 PCM allocator 私有化（Bread Compact / Fangtang-4G，进行中）：以与圆屏相同的 Phase 6 边界处理 direct-I2S 唤醒词实时内存。共享 `board_port_bread_compact.c` 保留 MultiNet lifecycle、原始 sample → 16-bit mono 转换、峰值/能量诊断、增益、检测回调、pause/stop 和失败策略，但不再为 `mono` / 32-bit `raw` I2S buffer 选择 `MALLOC_CAP_INTERNAL` 或直接 `heap_caps_free`。Bread/Fangtang profile-private `compact_audio_adapter` 各自新增以用途命名的 `allocate/free_wake_capture_buffer()`，维持既有 internal 8-bit placement 并保证无内存与正常退出的 paired release。此后 I2S/DMA/cache 对连续 reader buffer 的要求只在 board adapter 改动；命令 WAV、网络 payload、ESP-SR model 及任务栈仍属它们原来的 session/lifecycle，未被错误迁入 Audio HAL allocation seam。2026-08-11 在有效 IDF 6.0.2/Python 3.12 环境完成完整 profile build：Bread app `0x335ea0`（最小 app 分区余 11%），Fangtang app `0x331660`（余 12%）；HAL boundary check 与 diff check 通过。COM4/COM5 仍须验证低内存的 wake start/stop、连续唤醒、命令/播放互斥与实际唤醒率；本项不宣称独立 Audio Service、I2S runtime recovery 或人声音质已完成。
> 2026-08-11 紧凑屏临时 display bitmap allocator 与响应图错误语义收口（Bread Compact / Fangtang-4G，进行中）：在既有双 frame/transfer staging 下沉后，继续迁移短生命周期显示资源。共享 `board_port_bread_compact.c` 不再为 ASCII/24px/天气文字 stripe、进度条、实体填充 stripe、Bread robot、Fangtang cube/sugar、网关响应图缩放和二维码位图选择 `MALLOC_CAP_SPIRAM`、`MALLOC_CAP_DMA` 或直接 `heap_caps_free`；它仅根据当前是 retained composition 还是直接 panel transfer 请求普通 temporary bitmap。Bread/Fangtang profile-private display adapter 各自保持原物理策略：合成期优先 PSRAM、失败时可退至 internal DMA，直接 SPI/NV3023 提交使用 internal DMA，并由同一 adapter 配对释放。这样 renderer 保留场景与像素算法而不认识控制器可读取的内存能力；音频/WAV、网络 payload 与 remote-pet source 的非显示资源仍不被错误迁入显示 adapter。响应图直接提交失败现在先释放临时缩放位图，再以统一 renderer warning 报告错误，避免 SDK 宏输出掩盖失败语义。2026-08-11 已在有效 IDF 6.0.2/Python 3.12 环境完整构建 Bread Compact（`tools\build-profile.cmd bread-compact build`，镜像生成成功），Fangtang-4G 亦已在同环境完整 build 通过；HAL boundary check 与 diff check 通过。该证据不替代 COM4/COM5 的 response-image、QR、长文本、宠物、memory-pressure 及 DMA/HIL，亦不代表完整 Display/Memory Resource Service 或 renderer restart 已完成。
> 2026-08-11 紧凑屏 Display memory-capability 私有化（Bread Compact / Fangtang-4G，进行中）：共享 compact renderer 对双 frame 与 transfer staging 的算法、字节数、场景锁和 framebuffer ownership 不变，但已不再为这些长期显示 buffer 指定 `MALLOC_CAP_SPIRAM`、`MALLOC_CAP_DMA` 或 `MALLOC_CAP_INTERNAL`，也不直接 free 这些 profile-owned capability allocation。Bread/Fangtang display adapter 现在各自提供 frame/transfer allocation 和 release；两者当前维持既有“保留场景 frame 在 PSRAM、SPI transfer staging 在 internal DMA”的物理合约，Fangtang 继续配合 NV3023 行式 fence。此 seam 使后续紧凑屏控制器/PSRAM 变化只调整 adapter，而不会误改业务/场景 renderer。临时文字/QR/宠物等短生命周期显示 bitmap 已在后续切片下沉；audio worker 与非显示网络/音频资源仍不属于 display adapter。Bread/Fangtang Xtensa syntax compile 通过（Fangtang 仅既有 `previous` 未使用 warning），HAL boundary check 与 diff check 通过；完整 link、memory-pressure、COM4/COM5 长文本/宠物/回复与 DMA HIL 待 IDF Python 恢复，不能由本项推断 steady-state allocation、renderer restart 或 DMA fault recovery 已完成。
> 2026-08-11 Power Service timer-context 收口（全 profile，进行中）：复核 COM6 待机重启旧串口证据后，确认 `esp_timer` 栈溢出发生在“startup pet 延迟重试”触发点；该路径此前已改为 timer 仅置标记、由 `gateway_poll_task` 执行。为防止相同问题在 DISPLAY_OFF deadline 上再次出现，`power_service.c` 现将 timer 回调进一步收口为只向 `maclaw_power_evt` 专用任务投递 notification；该任务在普通 FreeRTOS 任务栈中消费一次性 armed deadline，再经 `Platform Power → board_port → profile-private display adapter` 完成 LCD/AMOLED 的 DISPLAY_OFF。定时器、业务 UI 和 profile adapter 均不直接跨层：UI 仍只调用 Device Power API，Power Service 仍只调用 Platform Power，CO5300/ST77916/ST7789/NV3023 的实际电气时序保持在各 display adapter。停止路径在删除 timer 后唤醒并 join worker，避免晚到 callback 通知已回收 handle。Waveshare 既有 Xtensa compile-command 下 `power_service.c` syntax compile、`tools/check-hal-boundaries.ps1` 与 `git diff --check` 已通过；支持入口的完整 `tools\build-profile.cmd waveshare-amoled-1.75c build` 仍被本机 ESP-IDF Python 环境损坏阻塞，故尚未刷入 COM6，也未将本项宣称为 COM3/COM4/COM5/COM6 的 DISPLAY_OFF/wake HIL 完成。环境恢复后必须完整构建，依次验证每个端口的超时熄屏、触摸/实体键首触只亮屏、远程亮度唤醒、前景 lease 延期、闹钟/回复不熄屏及连续待机无 reset/stack-overflow。
> 2026-08-11 圆屏离线唤醒 PCM allocator 私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：按 Phase 6 继续区分“共享识别/会话逻辑”与“物理音频内存约束”。共享 `board_port.c` 的 MultiNet lifecycle、PCM slot 选择/去直流/自适应增益、pause/stop、检测回调和错误策略均保持不变，但不再为 `mono` / interleaved `tdm` 唤醒词实时缓冲指定 `MALLOC_CAP_INTERNAL` 或直接 `heap_caps_free`。`round_audio_codec_adapter.h` 新增以用途命名的 `allocate/free_wake_capture_buffer()`；该 private adapter 继续把连续 I2S reader/recognizer 缓冲放在 internal 8-bit RAM，并保证 allocation-failure 与正常退出使用同一配对释放。这样未来圆屏 I2S DMA/cache 要求变化时只改 Audio adapter，不会在共享 wake state machine 中重新引入 capability 分支；命令 WAV、网络 payload、MultiNet 模型内存和 task stack 仍保持各自 lifecycle，不被误迁入该音频 buffer seam。2026-08-11 使用隔离 IDF 6.0.2/Python 3.12 build 目录对 EchoEar 与 Waveshare 完整 profile 增量构建通过：EchoEar app `0x322430`（余 14%），Waveshare app `0x353590`（余 33%）；HAL boundary check 与 diff check 通过。COM3/COM6 仍需验证低内存下 wake start/stop、连续唤醒、录音/播放互斥和实际唤醒率，本项不宣称独立 Audio Service、runtime I2S recovery 或人声质量验收已完成。
> 2026-08-11 圆屏 panel DMA completion fence 完全私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：继续按 Phase 5 将控制器事务而非场景策略留在 display adapter。共享 `board_port.c` 已删除 `s_lcd_transfer_done`、FreeRTOS binary semaphore 的创建、ESP-LCD completion callback 的等待超时以及将 renderer-owned handle 传给 adapter 的初始化参数；共享层只在 `draw_bitmap_sync()` 中维持“场景写入后需要同步完成才可复用像素”的普通 success/failure 语义。EchoEar ST77916 与 Waveshare CO5300 adapter 各自私有创建和保存 completion semaphore、把它作为该 controller panel-IO callback context、在提交前清除陈旧 token，并只在本次 `esp_lcd_panel_draw_bitmap()` 入队成功后等待自己的 completion（1 s）。这令 callback lifetime、ISR semaphore give、timeout 和 queue fence 与 panel/IO handle 同属一个 profile-private transaction；共享 renderer 不再持有 RTOS driver fence，也不会因新增圆屏控制器改 callback plumbing。保留两板现有 40-row stripe、framebuffer、圆形场景和 CO5300 column alignment 行为不变。2026-08-11 HAL boundary check 与 diff check 通过；既有圆屏 build 目录绑定了失效/不同 Python，未清理用户目录而改用隔离 build 目录并在有效 IDF 6.0.2/Python 3.12 环境完成 EchoEar 与 Waveshare 完整 profile build：EchoEar app `0x322430`（最小应用分区余 14%），Waveshare app `0x353570`（余 33%）。COM3/COM6 的启动、待机宠物、录音/回复、DMA stripe、DISPLAY_OFF/wake HIL 仍必须分别确认。本项不宣称 display deinit/reinit、DMA fault recovery 或独立 Display Task 已完成。
> 2026-08-11 圆屏 remote-pet 驻留内存私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：共享 `board_port.c` 仍只决定远程宠物帧的尺寸、裁剪、缩放、可见性校验、替换时机和场景呈现；但不再指定 PSRAM capability 或在成功/失败路径直接使用 libc `free` 释放这类长驻显示资产。两个 profile-private display adapter 现各自提供 `round_display_adapter_allocate_remote_pet_frame()` 与对应 release，维持既有的 PSRAM 存放策略并使未来控制器的 DMA/cache 限制只需修改 adapter。此切片同时统一了 allocation/free pairing，避免 capability allocator 与 libc release 混用。EchoEar 与 Waveshare 现有 Xtensa `board_port.c` compile-command syntax check、HAL boundary check、`git diff --check` 通过；完整 profile build/link 和 COM3/COM6 远程宠物切换、清空、动画、DISPLAY_OFF/wake HIL 仍待 IDF Python 修复，不能由该静态检查宣称实机完成。

> 2026-08-11 ����¼�� WAV ����Ȩ�� profile build-closure �տڣ�ȫ profile�������У���`board_port_capture_wav()` �ĳɹ���������� profile Audio HAL ��ӵ�е� opaque payload��ҵ�񽻻�·�������ٶ������� `free()`������Ψһ�ͷ��� `device_audio_release_captured_wav() �� audio_service_release_captured_wav() �� platform_audio_release_captured_wav() �� board_port_release_captured_wav() �� profile-private audio adapter`��Բ���ͽ���� adapter �ֱ����ʹ�ø��� PSRAM/8-bit allocator ������ͷš�Platform ���쳣���ء��㳤�Ȼ򳬳� `uint32_t` �ĳ���ʱҲ�� board release �տڲ��ÿգ�������÷������ heap-family ���裻`main.c` �İ˸�����¼���˳�·������Ϊ�� Device API������������/����ý�� buffer ���ڴ˴α����Χ�������л����� ESP-IDF �� preliminary component-requirements pass ���ܿ��������� `-D MACLAW_PROFILE`���������� build directory ʱ Waveshare �� `main` ȱʧ��ֱ�Ӱ����� CST9217/Touch ͷ������`main/CMakeLists.txt` ���ڴ� wrapper �����ָ� profile identity������ Kconfig ��ȷ��ʱǿ����ѡ��� board һ�£�ȷ�� EchoEar ST77916��Fangtang NV3023/ML307��Waveshare CO5300/Touch ��ֻ������ȷ profile closure��2026-08-11 �Ը��� `build-review-waveshare`��IDF 6.0.2/Python 3.12 �������� Waveshare ͨ����app `0x353590`����С app ������ `0x1aca70`��33%����ͬ�� EchoEar ������������ͨ����app `0x322470`���� 14%��HAL boundary check������ WAV `free(wav)` ������ `git diff --check` ͨ����COM3/COM4/COM5/COM6 ���밴�˿���֤¼���������ɡ�˫��ȡ����Ԥ�����ʱ���ϴ�ʧ�ܺ͵��ڴ�·���������ɹ�������ʵ����˷硢���ʻ��ϴ� HIL �����ա�

> 2026-08-11 Resource Pressure ƽ̨�����տڣ�ȫ profile�������У���`resource_pressure_service` ����Ψһӵ�� NORMAL/PRESSURE/CRITICAL ��ֵ���ͻء���ѡ���� admission �Լ��� Storage rollback �� lifecycle lock��������ֱ�Ӱ��� `esp_heap_caps.h`/`esp_spiffs.h`������ʶ�� `MALLOC_CAP_*` ����� SPIFFS������ `platform_resource_sample()` �� internal/PSRAM total �� largest contiguous block�����ѹ��� storage ������ͳһ������еİ汾�� `device_resource_pressure_snapshot_t`��Platform ��������ʧ��ʱֻ���� `storage_available=false`���� Service �����в��� fail-closed Ϊ CRITICAL���������� Service lifecycle lock ����ɣ����� deinit �ȹر� admission��Storage ���ͷ� VFS ��ԭ�Ӵ���δ�ı�ǰ̨����/����/�־û����� optional gate �ܾ��Ĳ�Ʒ����2026-08-11 �ڸ��� IDF 6.0.2/Python 3.12 build ���������� Bread Compact �� Waveshare 1.75C ͨ����Bread app `0x335f20`���� `0x6a0e0`��11%����Waveshare app `0x353610`���� `0x1ac9f0`��33%����HAL boundary check �� `git diff --check` ͨ��������Ƭ��δ��� Resource Manager �� reservation/quota��thermal/DMA pool �� COM3/COM4/COM5/COM6 pressure/fault-injection HIL�����ɽ� compile/link ������ʵ�����澯��VFS �쳣�����߻ع������ա�

> 2026-08-11 本轮 review/fix：Fangtang-4G profile build closure 与 MP3 失败语义收口（全 profile，进行中）：复核隔离 Fangtang 构建时发现 `78__esp_lcd_nv3023` 已由 profile manifest 解析并参与链接，但 source-free `profile_components/fangtang_deps` 只导出了 `78__uart-uhci`；在 ESP-IDF preliminary requirements pass 中，Fangtang bridge adapter 包含的 `esp_lcd_nv3023.h` 因此不在 `main` 的编译 include closure 内。现将该专属 dependency carrier 精确声明为 `78__esp_lcd_nv3023`、`78__esp-ml307`、`78__uart-uhci` 三项公共 requirements，仅对 Fangtang wrapper 生效；重配后的 `main` 依赖记录包含三者，隔离 IDF 6.0.2/Python 3.12 `build-review-fangtang` 完整构建通过，app `0x3316d0`、最小 app 分区余 `0x6e930`（12%）。同时 review `mp3_player` 发现：解码/写入已经失败时，成功的 `device_audio_playback_end()` 会覆写原始错误并令上层误判播放成功；现仍必经收尾以释放 codec/session，但只在此前成功时采纳收尾结果，保留损坏或截断 MP3 的原错误。该修复不改变四种硬件的 PCM/I2S contract。2026-08-11 本轮最终构建证据：Bread `0x335f20` / 11%，EchoEar `0x3224d0` / 14%，Fangtang `0x3316d0` / 12%，Waveshare `0x353610` / 33%；HAL boundary check、命令 WAV `free(wav)` 搜索与相关 `git diff --check` 均通过。构建只证明 profile closure 和链接，不替代 COM3/COM4/COM5/COM6 的音质、录音、触控/按键、DISPLAY_OFF/wake、网络与异常路径 HIL。
> 2026-08-11 圆屏 I2S TX wire-format 私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：继续收口 shared `board_port.c` 中不属于业务会话的电气事实。`board_port_audio_playback_write()` 现在只向 selected Audio HAL 传递普通 mono/stereo PCM frame、frame count 与逻辑 channel count；ES8311 所需的 16-bit STD stereo slot 展开、mono 到双 slot 的复制、每次 driver write 的字节数与 256-frame 暂存上限全部封装在 profile-private `round_audio_adapter_write_pcm()`。因此命令播放、WAV 播放、提示音与保留的 ADPCM 确认语音路径共用同一条 adapter seam，业务层不再自行构造 I2S slot buffer 或依赖 ES8311 的 channel source。该适配保持两块圆屏现有的 16 kHz / 32 BCLK-LRCK 声音时序及播放 owner、mute/PA、zero-tail、stop/error 语义不变；Audio HAL 对无效 PCM 参数返回 `ESP_ERR_INVALID_ARG`，仅未初始化 TX channel 返回 `ESP_ERR_INVALID_STATE`。已用 `build-review-waveshare` 重新编译 `board_port.c.obj`（仅既有 `compose_text16_curve` unused warning），HAL boundary check 与 `git diff --check` 通过；EchoEar 旧 build 目录因缺少 `IDF_PATH` 触发 CMake 重生失败，未将其误报为 profile 编译通过，后续须使用有效 ESP-IDF 环境完成 EchoEar link 及 COM3/COM6 人声、音量、停止播放 HIL。
> 2026-08-11 圆屏 I2S RX wire-format / mic-slot 私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：在 TX slot 扩展收口后，继续移除共享圆屏 `board_port.c` 对麦克风 wire frame 的依赖。`round_audio_codec_adapter.h` 现在私有定义每逻辑 mono frame 所需 wire bytes，并把已验证的 ES7210 slot 选择与 interleaved-to-mono 提取封装为 `round_audio_adapter_extract_capture_mono()`；会议 stream、命令 WAV 捕获和 MultiNet 离线唤醒仅持有普通 mono PCM，既不计算 I2S slot 数也不记录 profile slot 编号。录音的 DC blocker/AGC、命令 VAD、唤醒识别的独立 gain、mutex/admission 和停止语义不变；profile 改变 I2S slot/codec 连接时，仅修改 Audio HAL。Waveshare `board_port.c.obj` 已重新编译通过（仅已有 `compose_text16_curve` unused warning），HAL boundary 与 `git diff --check` 通过；旧 EchoEar build directory 缺失 `IDF_PATH` 而 CMake 重生失败，须在有效 ESP-IDF 环境以支持脚本完成 COM3 profile build，并在 COM3/COM6 验证录音上传、会议 stream、连续唤醒及播放交错。该项不宣称音频质量、runtime I2S recovery 或完整 HIL 已完成。
> 2026-08-11 圆屏离线唤醒任务调度私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：继续按 Phase 6 将非业务的运行时硬件约束下沉。共享 `board_port.c` 保留 MultiNet 生命周期、ready/timeout、pause/stop、回调合并及失败策略，但不再直接指定离线唤醒 worker 的任务名、10 KiB stack、优先级或 CPU1 affinity；这些参数现由 profile-private `round_audio_adapter_start_wake_recognizer_task()` 创建。该 seam 与既有 wake-dispatch 的 PSRAM stack helper 一致，确保将来不同圆屏的模型内存/核心亲和性要求只需调整 Audio HAL，而不会修改业务状态机。Waveshare `board_port.c.obj` 重新编译通过（仅已有 `compose_text16_curve` unused warning），HAL boundary 与 `git diff --check` 通过；EchoEar 完整构建仍受旧 build directory 缺少 `IDF_PATH` 阻塞，需在有效 ESP-IDF 环境完成 COM3 build，并同 COM6 验证唤醒 start/stop、连续唤醒、录音/播放交错与低内存失败路径。未将本项误报为 runtime I2S recovery、音频质量或完整 HIL 完成。
> 2026-08-11 圆屏输入扫描任务调度私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：共享 `board_port.c` 的输入层继续只保留 key/touch 合并、debounce、short/double/long 时序、DISPLAY_OFF 首触消费和 scanner stop/join；不再指定其 FreeRTOS 任务名、3 KiB stack 或 priority。EchoEar CST8xx 与 Waveshare CST9217 profile 各自实现 `round_input_adapter_start_scan_task()`，把这些 runtime 参数与自身 GPIO/touch 事实留在 private input adapter 内。两 profile 当前保持相同任务调度以不改变行为，但未来实体按键/触屏或资源预算差异只需改 adapter。Waveshare `board_port.c.obj` 已重编译通过（仅已有 `compose_text16_curve` unused warning），HAL boundary 和 `git diff --check` 通过；EchoEar 旧 build directory 仍缺 `IDF_PATH`，需使用有效 ESP-IDF 环境完成 profile build，并在 COM3/COM6 验证单击、双击、长按、熄屏首触只亮屏及输入 scanner stop/restart。未宣称 input runtime recovery 或完整 HIL 已完成。
> 2026-08-11 圆屏 PA/DAC 播放物理时序私有化（EchoEar-2ST / Waveshare 1.75C，进行中）：共享播放 session 继续只决定 admission、owner、PCM 供给、stop 与原始错误传播；PA enable、DAC mute/reveal、10 ms analogue settle、DMA drain、zero-tail、5/10/20 ms shutdown 时序现封装在 `round_audio_adapter_playback_prepare/reveal/finish/abort()`。`finish()` 即使 zero-tail 的 DMA write 失败或短写，仍无条件执行 DAC mute 与 PA 断电，再向共享层返回第一个发生的错误；共享 `board_port_audio_playback_end()` 因而保持调用方的原始 decode/write error 优先级，同时不漏掉物理收尾。EchoEar/Waveshare 的既有 full-duplex RX clock 合约与人声播放时序不改，但以后不同 codec/PA 只改 Audio HAL。Waveshare `board_port.c.obj` 已重编译通过（仅已有 `compose_text16_curve` unused warning），HAL boundary 和 `git diff --check` 通过；EchoEar 需在有效 ESP-IDF 环境完成 build，并在 COM3/COM6 验证人声、音量、首块无爆音、stop、传输失败后的静音/断电及 wake resume。未宣称音质或运行时 I2S recovery 已验收。
> 2026-08-11 Shared wall-clock deadline callback ownership 收口（Alarm / Sleep Schedule）：复核 `wake_deadline_service` 发现，worker 会在锁内复制 callback/arg、再在锁外执行；旧 `unregister()` 仅清空 slot，无法阻止已经复制的回调在 client 已停止并准备释放其状态后继续执行，属于典型 callback-after-unregister 生命周期竞态。现 slot 增加 in-flight 标记及 generation acknowledgement：dispatcher 在复制时占有该代 callback，完成后才释放；注销先关闭 admission/取消 deadline，slot 在 in-flight 清空前不可复用，超时保持 closed，不会让新 client 复用旧 generation。新增 `unregister_with_timeout()` 供生命周期路径使用；Alarm 与 Sleep Schedule 在已关闭自身 admission、join worker、drain tool borrower 后，按同一个父 deadline 等待 shared callback drain，只有成功才清除 deadline handle/callback-owned state。常规 wrapper 保留 1 s convenience 行为，启动失败清理语义未改变。HAL boundary 与 diff check 通过；本机 IDF/CMake 环境无法重配 build-review 目录，因此本切片未取得新的对象/full-link 证据，尚未做 deadline 恰好触发、callback 阻塞与 rollback 并发的故障注入或 COM3–COM6 HIL，不能将它描述为完整 lifecycle/restart 验收。
> 2026-08-11 Display DMA fence / Power transition ownership 收口（Bread Compact / Fangtang-4G / EchoEar-2ST / Waveshare 1.75C）：审计各 profile 的 `on_color_trans_done` 后确认，正常 presenter 会等 completion semaphore 才复用 framebuffer/stripe；但一旦该等待超时，代码仍可能在随后 DISPLAY_OFF/wake 中直接发送 panel command，而 controller 可能仍持有原始 DMA source，破坏“停止/物理转换先 drain callback-owned resource”的 HAL 生命周期规则。现四个 profile-private display adapter 都显式记录 transfer-pending：提交成功前后维护 pending，completion callback 先清 pending 再给 fence；DISPLAY_OFF 与 wake 均在 profile adapter 内先等待 pending transfer drain，再进行各自私有的背光/DCS/GRAM 操作。超时返回错误并保持 pending，Power Service 保持既有“不提交新的物理转换”的安全语义；共享 Display/Device/Platform API 未新增 controller、DMA 或 callback 类型。Bread/Fangtang completion fence 仍由 shared renderer 创建但只作为 adapter 私有引用，圆屏 fence 完全由 profile 私有 adapter 创建。HAL boundary 与 diff check 通过；尚未以 DMA completion 延迟/丢失注入验证各 panel 的 command ordering，且本机 IDF/CMake 环境不能重配缓存 build，故不得把本条表述为 full-link、COM3–COM6 HIL 或完整 display restart 验收。
> 2026-08-11 Provisioning portal transaction 串行化与 DNS Registry deadline 收口：审计 `main.c` 发现 setup portal 的入口来自启动、物理输入、Gateway 恢复和保存凭据后的 reset coordinator；原实现只分别检查 `s_setup_server`、DNS handle 与 provisioning bit，两个 caller 仍可能同时通过“尚未发布 HTTP handle”的窗口，交叉分配 scratch、取得同一 power lease、改变 APSTA/DNS 或让一个 generation 的 stop 清理另一个 generation 的资源。现增加 retained `s_setup_portal_mutex`，将 portal start、public stop 与 start-failure recovery 作为同一 composite transaction 串行化；stop 获取锁亦纳入其单一 parent deadline，锁等待消耗后只把余量交给 HTTP/DNS 清理。HTTP `httpd_stop()` 仍是 ESP-IDF 无 caller timeout 的已知不可控边界，但不会再与新 portal start 并发。另发现 captive DNS worker 自然退出会在 completion 已发布后调用无 deadline 的 `task_registry_unregister()`；当 owner stop 正在使用总 deadline 时，这会在最后 bookkeeping 重新引入无界阻塞。DNS worker 现仅发布 completion，`stop_captive_dns_task()` 在同一 deadline 余量内调用新的 `task_registry_unregister_with_timeout()` 删除其不可变 task identity；余量耗尽或 registry lock 未取得时保留 entry、关闭 admission，fail-closed。`tools/check-hal-boundaries.ps1` 与本切片 `git diff --check` 通过；本机 IDF 全量重配/链接仍受环境阻断，未做 portal start/stop 并发、httpd stop 阻塞、DNS socket/registry-lock 故障注入或 COM3–COM6 HIL，因此这只是源码级 ownership/deadline 收口，不宣称 Wi-Fi/APSTA/netif/event handler 的完整 runtime restart 或所有 provisioning lifecycle 验收完成。
> 2026-08-11 Wi‑Fi/IP event callback in-flight drain：复核 ESP-IDF v6.0.2 `esp_event_handler_instance_unregister_with()` 实现发现，其在 handler 标记 `unregistered` 后向 default event loop 投递 cleanup event；API 可能等待 loop mutex，且没有 caller-supplied timeout 或“当前已执行 callback 已返回”的公开证明。原 cold-start rollback 因而可在 application `wifi_event()` 正在读取 Connectivity/UI、重连 Wi‑Fi 或启动 Gateway worker 时，停止 driver 并删除 netif/default loop。现为本 application callback 增加独立 admission 与 in-flight counter：`stop_network_core_transaction()` 从 parent deadline 先关闭 admission、等待已进入的 callback 返回，再进行 SNTP/radio/SDK handler unregister/netif teardown；callback 入口关闭后不再触碰业务状态，所有分支统一 leave。每次 handler 成功注册前清理旧 generation 的 drain token，再开放新 admission，避免历史 completion 被误用于新代。该修复只涵盖本 composition root 注册的 `wifi_event`；ESP-IDF 内部 netif handlers、`esp_event_handler_instance_unregister()` 和 `httpd_stop()` 的无 deadline 边界仍由 SDK 管理，未主张其可完全 bounded/restart。缓存 Bread Xtensa `main.c` `-fsyntax-only`、`tools/check-hal-boundaries.ps1` 与本切片 `git diff --check` 通过；尚无 callback 卡在 Gateway start/Wi‑Fi API 时的故障注入、full link 或 COM3–COM6 HIL，不能据此声称完整 Wi‑Fi/APSTA/netif runtime restart 或 Connectivity 全量 shutdown 已验收。
> 2026-08-11 Diagnostics USB/JTAG query worker Registry 收口：继续枚举生命周期 Registry owner 后发现 `firmware_identity` 查询 task 虽已有 start gate、stop token、completion semaphore 与 immutable task context，但自然退出在发布 completion 后仍调用无 deadline `task_registry_unregister()`；若未来 Diagnostics owner 被 composition-root 用总 deadline 收敛，这一最终 bookkeeping 仍可能无界阻塞，并且 stop callback 未比较登记 identity 与当前 generation。现 worker 只发布 completion/清理自身 handle，`firmware_identity_stop(timeout)` 以同一 deadline join 后使用 `task_registry_unregister_with_timeout()` 删除捕获的 immutable handle；registry callback 对 stale context 返回 fail-closed。该诊断 service 在当前 degraded boot 策略中仍故意保持运行，未接入 `startup_stop_local_workers()`，因为它是 NVS/network/Power teardown 后仍可用于恢复的 console；本项只是让未来显式 Diagnostics shutdown 有真实 bounded join 与 generation safety。缓存 Bread Xtensa `firmware_identity.c` `-fsyntax-only`、`tools/check-hal-boundaries.ps1` 和本切片 `git diff --check` 通过；未做 USB FIFO、Registry mutex 故障注入、full link 或 COM3–COM6 HIL，不能将其解释为 USB console 重配置、Power/MCU sleep 或完整 board shutdown 已验收。
> 2026-08-12 Alarm / Sleep Schedule client callback 与 worker hand-off 收口（全 profile shared business domain）：复审共享 wall-clock dispatcher 的两名 client 后发现，虽然 `wake_deadline_service` 已对 slot callback 做 generation drain，Alarm 与 Sleep Schedule 自身仍有若干未受同一 admission lock 保护的 `s_task` / `s_initialized` / `s_stop_requested` 读取和直接 notify；worker 恰好退出、rollback 正在回收 completion semaphore 时，迟到 deadline/tool/manual-wake 通知可能使用旧 task handle。现 Alarm 统一以 lifecycle lock 采样 deadline/tool notify target、worker 在 final completion hand-off 前先在同一 lock 清 task handle，并将 ring callback 清理与调用保护在同一 state lock；Sleep Schedule 将 deadline、wall-clock、manual wake、tool notification 与 worker final task publication 收口到 `s_pending_lock`，同时保持 persistent store mutex 只负责业务/NVS 数据。两者的 init failure path 也改为显式 bounded `wake_deadline_service_unregister_with_timeout()`，不再使用会暗含完整一秒等待的 legacy wrapper；正常 deinit 仍以单个 parent deadline drain client slot、worker与已准入 tool。闹钟、睡眠窗口、显示熄灭/唤醒及各 profile 硬件实现未改变。Waveshare 缓存 Xtensa compile environment 下 `alarm_manager.c`、`sleep_schedule_service.c` `-fsyntax-only` 通过，HAL boundary 与 diff check 通过；当前 IDF Python/export 环境损坏，无法做本轮 full link，亦未做 deadline callback / tool / worker-exit 三方竞态故障注入或 COM3–COM6 HIL，故仅构成源码级生命周期证据，不能表述为闹钟、定时休眠或完整 restart 已验收。
> 2026-08-12 Fall Detection callback / user-cancel lifecycle 收口（Waveshare capability，统一业务层）：复审跌倒检测发现 classifier worker 的应用 presentation callback 虽同步执行、可由 worker join 覆盖，但其 `s_stop_requested` 原先与 lifecycle admission 使用不同锁；同时物理触控/按键的 `fall_detection_service_cancel_from_user()` 不属于 Gateway tool admission，却会在离开 classifier-state lock 后释放 Power lease。现 worker callback 和循环 stop 判断统一先经 lifecycle lock 采样 stop generation，worker 在发布 completion 前于同一 lock 清 task handle；deinit 在关闭 admission 后获取同一快照，避免通知已退出/reused task handle。新增 user-action admission，deinit 将其与 Gateway tool admission 一同 drain，确保触屏/实体按键取消中的 lease release 完成前不会继续到 Power teardown。算法阈值、确认窗口、统一“疑似设备跌落”业务文案、触屏/按键 cancellation 语义及 profile-private Motion HAL 均未改变。Waveshare 缓存 Xtensa compile environment 下 `fall_detection_service.c` `-fsyntax-only`、HAL boundary 和 diff check 通过；尚未做 motion sample 阻塞、fall callback 与 rollback、用户取消与 Power deinit 的竞态注入，也未做 COM6 真实 IMU/触摸 HIL，因此该项仅是源码级 lifecycle 收口，不构成跌倒检测、Power restart 或硬件传感器验收。
> 2026-08-12 Fall Detection application callback admission 补强（统一业务层）：在同一服务的后续审计中确认，仅凭 classifier worker join 不能表达 callback 的独立 ownership：用户回调在 worker 内同步执行，且可唤醒显示、提交 UI；rollback 若在 callback 已复制后关闭 stop flag，必须把这段应用代码也算作 Power 依赖 borrower。现 `notify_if_current()` 在复制 callback/context 前取得 lifecycle callback admission；deinit 关闭 admission 后会将 callback、Gateway tool 与本地实体取消三类 borrower 一起 drain，才允许后续 Power teardown。callback 已因 stop 关闭而无法开始的情况下保持 fail-closed；已经开始的 callback 仍由 worker 安全点完成。Waveshare 缓存 Xtensa `fall_detection_service.c` `-fsyntax-only`、HAL boundary 与 diff check 通过；仍未做 callback 阻塞、Power transition 同时发生的故障注入或 COM6 HIL，故不是完整跌倒检测/Power 生命周期验收。

> 2026-08-12 Power Service init/deinit publication 收口（全 profile shared lifecycle）：继续审计 DISPLAY_OFF scheduler 与 Power Lease 的 composition-root 顺序后发现，`power_service_init()` 先把 `s_initializing` 置位、随后才创建并发布静态 deinit mutex；并发 rollback 若落在这个短窗口，会把“mutex 尚未发布”误判为“Power Service 未启动”，返回成功，继而关闭 lease admission。初始化随后仍可能发布 timer/worker，形成已运行 scheduler 对已关闭 lease domain 的跨代不一致。现初始化器先取得 retained deinit mutex，再在同一锁保护下发布它；`power_service_deinit()` 对 `s_initializing` 实施有界重试和同一父 timeout，直到 init 完成、失败，或耗尽预算。所有 init 失败路径亦在清除 `s_initializing` 后才释放 mutex，使等候的 stop 不会把失败中的构造误判为活跃 service。变更未扩大 Power HAL：业务仍经 Device Power → Power Service → Platform Power → board/profile-private DISPLAY_OFF adapter；既有 lease 保持可在 admission 关闭后 release。Waveshare 缓存 Xtensa `power_service.c` `-fsyntax-only`、HAL boundary 与 diff check 通过；当前环境未能进行完整 link，也未做 init/rollback 并发、timer-create/worker-create failure 或 COM3–COM6 DISPLAY_OFF/wake HIL，故仅构成源码级 lifecycle 修复，不宣称 Power restart 或休眠功能已完成验收。

> 2026-08-12 App Intent → Device Input shutdown parent-deadline 收口（全 profile shared Input business path）：复审输入 scanner 的 stop 链确认，`app_intent_service_stop()` 已对自身 publisher drain、stop event 和 task join 计算同一 parent deadline，但调用 `device_input_stop()` 时仍传入原始 `timeout_ms`。若 Input Service 在其 scanner join 上耗尽整个预算，随后 App Intent 又获得一次完整 timeout，违反 composition-root 的单一 deadline 契约，且会延后 Power/Display rollback。现调用前先计算剩余 tick budget，并只把剩余值传给 Device Input；返回后 App Intent 的 publisher drain、critical stop event 和 task join 继续消耗同一剩余预算。Input/Intent 仍只处理标准 Device Input event 与队列，不读取板型、GPIO、触控或显示对象，所有四个 profile 的 scanner/publish contract 保持不变。Waveshare 缓存 Xtensa `app_intent_service.c` `-fsyntax-only`、HAL boundary 与 diff check 通过；尚未在 Input scanner 卡住、队列满或 consumer 阻塞时做故障注入，亦未做 COM3–COM6 触控/实体键 HIL，故仅构成源码级 deadline 修复，不宣称完整 Input restart 或交互验收。

> 2026-08-12 Input / App Intent timeout 后可续 stop 收口（全 profile shared Input business path）：继续审计 scanner stop 发现，旧 Input 与 App Intent 均以 `stop_enqueued` 同时表示“已提交 stop token”和“本 caller 拥有回收事务”。一旦 board scanner、队列写入或 dispatcher join 恰好超时，admission 已关闭而 stop token 尚未提交（或 task 已在迟到退出），后续 stop 会直接返回 `BUSY`，导致该 closed generation 的队列、completion semaphore 与 task handle 永远不能由下一轮 rollback 收口。现两个 service 将短期 owner (`stop_in_progress`) 与持久 generation 状态 (`stopping`/`stop_enqueued`) 分离：超时仅释放 owner，绝不重开 publisher admission；后续 lifecycle caller 可以沿同一 closed generation 重试 scanner join、补发尚未成功提交的 stop token，或消费已迟到给出的 completion。成功路径仅在删除队列/semaphore 后、同一 service lock 内清空 state，避免第二 caller 看见半清理 generation。profile-private board scanner 同步增加一次性 stop submission 标记：COM3/COM6 圆屏及 COM4/COM5 紧凑板均不会在首次 join 超时后再次 notify 一个可能已退出并被 FreeRTOS 复用的 task handle，而是保留原 completion token 等下一次 bounded stop 回收。业务层仍只经 `Device Input → Input Service → Platform Input → board port/profile adapter`，未向公共 HAL 泄漏 task/semaphore/GPIO 对象。Waveshare 缓存 Xtensa `input_service.c`、`app_intent_service.c` 和 Bread 缓存 Xtensa `board_port_bread_compact.c` 的 `-fsyntax-only` 通过，HAL boundary 与本切片 `git diff --check` 通过；Waveshare `board_port.c` 完整语法编译因本地 managed `esp_lcd_touch` 依赖目录缺失而无法复现，且 CMake 重生受缺失 `IDF_PATH` 阻断。尚未对 scanner 恰在 timeout 后退出、队列满、第二个 stop caller 与 task-handle 回收做故障注入或 COM3–COM6 HIL，因此不能将本条解释为完整 Input restart/板级 deinit 验收。

> 2026-08-12 Display Service STOP completion 多 caller 收口（全 profile，Phase 4 增量）：继续检查 timeout 后可续 stop 语义发现，Display Task 的 static STOP completion 是 binary semaphore；若第一个 lifecycle caller 已消费 completion，第二 caller 虽面对 task 已退出的 closed generation，旧实现仍可能仅等待到自己的完整 deadline，甚至把一次性 token 当作多观察者广播。现以 `s_display_service_task == NULL` 作为受 state lock 保护的终态事实（task 会先清 handle、后 give completion）；所有 first/later stopper 都以 10 ms bounded slice 轮询此终态，同时仅将 completion 作为早唤醒提示。一个 caller 消费 token 不会妨碍另一个 caller观察已退出，仍没有第二 STOP 入队；超时继续保持 closed generation 的 static queue/task/record。该修复没有使 Display Service 可 restart，也不证明 renderer 卡死、mutex contention 或多 rollback caller 的运行时行为。Bread 缓存 Xtensa `display_service.c` `-fsyntax-only`、HAL boundary 与 diff check 需随本切片复跑；尚未做多 stopper/迟到退出/全 link/COM3–COM6 HIL。

> 2026-08-12 Display Task 重入 admission 防护（全 profile，Phase 4 增量）：继续枚举 `platform_display_*` 与 board renderer 调用者后确认，当前唯一正常 Platform Display 入口是 Display Task；但其同步 renderer 未来或个别 profile callback 可能重入 Device/Display Service。该路径必须直接在**同一个** Display Task 内 dispatch，不能向自己的 queue 入队并等待，否则会自死锁。旧 fast-path 虽避免自等待，却没有重验 `s_stopping`：若 outer submitter 已释放 submission mutex 而 rollback 同时关闭 admission，迟到 callback 仍可能在 task 内追加新的 renderer mutation。现 fast-path 在保留 task-identity 的前提下，在临界区重验“仍是同一已开放 Display Task”；关闭 generation 后直接拒绝重入请求。该检查不新增第二 renderer owner、SDK 类型或公共 API，也不改变当前同步正常回调结果；仍缺可控的 renderer-callback 重入/STOP 并发故障注入、异步 snapshot、DMA fence、restart、full link 和 COM3–COM6 HIL。Bread 缓存 Xtensa `display_service.c` `-fsyntax-only`、HAL boundary 和 diff check 需随本切片复跑，不能外推为真实 callback 竞态已验收。

> 2026-08-12 Display Service rollback deadline / STOP generation 收口（全 profile，Phase 4 增量）：在将 DISPLAY_OFF 收口到 Display Task 后复核 rollback 链发现，旧 `display_service_deinit(timeout_ms)` 会先调用无界的 submission mutex helper（并可能隐式 `init()`），随后又给 STOP completion 一整份 `timeout_ms`；因此 renderer 被阻塞时既可能让 parent deadline 被放大，也可能在尚未启动 Display Service 的 rollback 中错误创建一个 task。现 deinit 从入口建立同一 FreeRTOS tick budget：仅在已有初始化正在发布时等待，完全未启动的 service 立即 idempotent 返回；取得既有 submission mutex 与等待 static STOP completion 均只消费该原始 deadline 的余量。已关闭 generation 的后续 stop 不再简单返回 `BUSY`，而是只等待首次 stopper 留下的 boot-lifetime STOP completion；成功后重验 task 已退出再回报 OK，超时保持 admission closed、queue/task/STOP record 原样保留。普通显示提交继续可在运行态等待 renderer，不改变业务 UI 的同步兼容语义；此次也未引入 Display Task restart、异步 payload、DMA fence 或完整 panel deinit。Bread 缓存 Xtensa `display_service.c` `-fsyntax-only`、HAL boundary 与 diff check 需随本切片复跑；尚未进行 mutex 长占用/renderer 卡死/timeout 后迟到 STOP、全 link 或 COM3–COM6 HIL，故仅证明源码级 parent-deadline 与 generation 收口。

> 2026-08-12 Board background worker timeout 后句柄所有权收口（COM3/COM6 EchoEar/Waveshare、COM4 Bread、COM5 Fangtang）：沿用 Input 审计方法复核 profile-private 装饰/采样 worker，发现圆屏 pet animator、Bread/Fangtang remote-pet/thinking-mouth worker 及 Fangtang battery monitor 都会在第一次 join 超时后保留 task/semaphore，但下一次 stop 再次 `xTaskNotifyGive()` 该旧 task handle；任务若已在超时后自然退出，FreeRTOS 可能已复用该 handle，迟到通知会误唤醒无关任务。现每个 worker generation 都有单独的一次性 stop-submitted state：首次 stop 请求后只等待原 completion semaphore，后续 bounded rollback 只消费该 completion 并由 owner 删除 semaphore、清空 handle；Fangtang battery task 不再自行提前清 handle，保证 timeout 后仍有稳定 identity 可供 lifecycle owner 回收。圆屏和紧凑板的 task creation 路径均显式重置该 generation state，未改变宠物/嘴型动画、功耗采样、显示或业务场景规则。所有 task/semaphore 处理仍留在 board port/profile-private adapter，`Platform Lifecycle` 与公共 Device HAL 仅接收普通 status。Bread 缓存 Xtensa `board_port_bread_compact.c`、Waveshare 缓存 Xtensa `input_service.c`/`app_intent_service.c` `-fsyntax-only` 通过，HAL boundary 与本切片 `git diff --check` 通过；Waveshare `board_port.c` 语法编译仍因工作区缺失 managed `espressif__esp_lcd_touch` header 而不可用，Fangtang 当前 build 缓存指向另一旧源码树，故未把两者表述为本轮编译成功。尚未在 task 恰好超时后退出、handle 实际复用、Fangtang ADC worker、pet 动画 stop 与后续 UI publish 并发下做故障注入/COM3–COM6 HIL，不能据此声称 board runtime restart 或完整板级 deinit 已验收。

> 2026-08-12 Configuration / Persistence admission 与 STOP generation 收口（全 profile shared Storage path）：复核持久化与配置服务后发现两处相邻生命周期窗口。Configuration 的 `lock()` 原先只在阻塞取得 retained mutation mutex 前检查 `stopping`；deinit 可在该 caller 等锁期间关闭 admission、释放 PSRAM scratch，caller 随后取得 mutex 并继续使用已释放 scratch。现取得 mutex后再次验证 closed state 与全部 scratch 指针；迟到 waiter 仅释放 mutex 并返回，不触碰旧 generation。Persistence worker 原先自行清 task handle，而 timeout 后的 deinit 可再次入队 STOP；若第一个 STOP 已使 worker 退出，第二 sentinel 永远无人消费，且 `worker == NULL` 分支还可能把仍保留 queue/semaphore 的 closed generation 误报为成功。现 worker completion 保留 handle 直到 lifecycle owner 消费 semaphore；STOP 使用 per-generation one-shot 标记，后续 deinit 只等待原 completion；缺少 worker identity 且资源仍在时保持 fail-closed `TIMEOUT`，不释放可能仍由迟到 worker 使用的对象。初始化和 destroy paths 会重置该 generation 标记，公共 Storage/Configuration HAL 不暴露 queue、task、NVS 或 FreeRTOS 类型。Waveshare 缓存 Xtensa `configuration_service.c`、`persistence_service.c` `-fsyntax-only` 通过，HAL boundary 与本切片 `git diff --check` 通过；尚未故障注入 mutation waiter 穿越 deinit、PSRAM stack routed request、STOP queue 满、worker 在 completion 边界退出或 NVS commit 卡顿，也未完成 COM3–COM6 HIL，不能把本条称为 Storage runtime restart、NVS 一致性或完整 rollback 验收。

> 2026-08-12 Configuration init/deinit publication 收口（全 profile shared Configuration path）：进一步检查 Configuration 的构造窗口发现，`init()` 原先先创建 mutation mutex/PSRAM scratch、最后才打开 admission；并发 startup rollback 若落在 mutation mutex 已发布而 scratch 尚未完整的时间段，可能把服务当作可停止状态并释放局部 scratch，初始化随后又继续发布其余对象。现增加 retained deinit shell 与原子 `initializing` publication：init 先以 CAS 宣布 construction，再创建/取得 lifecycle shell；所有 scratch 完整、service lock 仍持有时才打开 admission。deinit 对 construction 以调用方同一 deadline 等待；construction 失败或成功均先清 `initializing`，使 rollback 不会误判“未启动”或与构造并发释放资源。mutation admission 继续在持锁后复核 closed state，公共 Configuration API 未新增任何 FreeRTOS/PSRAM 类型。Waveshare 缓存 Xtensa `configuration_service.c` `-fsyntax-only`、HAL boundary 与本切片 `git diff --check` 通过；尚未注入 mutex create/PSRAM allocation failure、init 与 rollback 精确交错、或 Configuration/Persistence 连续 restart，并未完成 COM3–COM6 HIL，故只构成源码级 lifecycle 证据。

> 2026-08-12 Update Service operation/admission 与 init publication 收口（全 profile metadata-only update path）：Update Service 不执行下载或刷机，但它是 Hub metadata/tool 到 Persistence 的同步消费者；复核发现 metadata/tool caller 可在通过 admission 后阻塞在 `s_operation_mutex`，rollback 已关闭 service 后仍可能获得锁并访问状态/NVS。现 `operation_enter()` 在取得 mutex 后重验 lifecycle admission，迟到 waiter 直接释放锁并返回。同步补足 init/deinit transaction：原子 `initializing` 在创建 mutex 前发布、retained deinit mutex 串行构造与 shutdown、deinit 使用同一个 parent deadline 等待构造及 active caller drain；init 成功/失败都释放 construction publication。Update Service 保持 16 MiB 产品策略——只保存经验证的 Hub metadata 和向用户提醒电脑刷机，不新增 OTA URL、固件下载、flash writer 或重启接口。Waveshare 缓存 Xtensa `update_service.c` `-fsyntax-only`、HAL boundary 与本切片 `git diff --check` 通过；尚未注入 metadata/tool 在 operation mutex 等待、init/rollback 交错、Persistence worker 停止或 Hub callback 并发，亦未做 COM3–COM6 HIL，因此不宣称 update reminder 或完整 Storage rollback 已验收。

> 2026-08-12 Weather Cache / Meeting Recovery 初始化与 shutdown deadline 收口（全 profile shared Persistence consumers）：天气缓存和会议恢复元数据都不持有 task、queue 或 NVS handle，但它们在启动/rollback 的同一 Storage 事务中更新 `initialized/stopping` 和同步 Persistence admission。复核发现其原实现缺少 construction publication，且 deinit 以 `esp_timer` 独立 deadline 轮询；并发 init/rollback 下可能把构造中的服务误作未启动，且与父 rollback tick budget 的精度/舍入规则不一致。现二者都以原子 `initializing` 声明构造，deinit 使用同一 FreeRTOS tick deadline 等待 construction 和已准入同步 NVS call；`is_initialized()` 也不会在 construction 期间报告 ready。业务接口、Weather 的 advisory-cache 语义、Meeting 的恢复 marker 结构及所有板型行为未变，依旧只通过 Persistence Service 访问 NVS。另补正 Update Service 的 allocation-failure rollback：如果 lifecycle mutex 从未创建且不存在 live admission，deinit 是 idempotent `OK` 而非误报 timeout。Waveshare 缓存 Xtensa `update_service.c`、`weather_cache_service.c`、`meeting_recovery_service.c` `-fsyntax-only` 通过，HAL boundary 与本切片 `git diff --check` 通过；尚未注入同步 NVS 正在执行、init/deinit 精确交错、Storage worker timeout 或 COM3–COM6 HIL，不能据此声称天气缓存、会议恢复或完整 Storage restart 已验收。

> 2026-08-12 Resource Pressure / Battery Policy lifecycle transaction 收口（全 profile shared policy path）：继续审计 Power 与 Storage rollback 边界后发现 Resource Pressure 虽已具备 static public/deinit mutex，但 init 在发布 construction intent 前后仍有短窗口；rollback 可把 service 误判为未启动，随后 init 才发布可采样 SPIFFS 的 observer。现以原子 construction gate 串行 init/deinit：deinit 以调用者同一 tick deadline 取得该 gate，继而关闭 `initialized/storage label` admission；init 不能穿越正在执行的 teardown 重新开放 VFS 观察。Battery Policy 的同步 telemetry query 已有 in-flight drain，但未初始化时调用 deinit 会留下 `stopping=true`，使稍后的正常 init 永久返回 BUSY；并发 init/deinit 也没有独立 transaction owner。现 idempotent pre-init deinit 恢复为 open inactive state，并以 lifecycle transition gate 串行 construction、admission close 与 reader drain；timeout 仍保持已初始化 generation closed，避免迟到 ADC/charge telemetry publish 到 Power 已停止的生命周期。业务层继续仅消费版本化 `device_resource_pressure_snapshot_t` / `device_battery_policy_snapshot_t`，SPIFFS、heap、ADC、charger GPIO 和板型细节仍分别停留在 Platform/board adapter。Waveshare 缓存 Xtensa `resource_pressure_service.c`、`battery_policy_service.c`、`platform_resource.c` 及 composition-root `main.c` `-fsyntax-only` 通过；`tools/check-hal-boundaries.ps1` 与本切片 `git diff --check` 通过。未做 init/rollback 精确交错、SPIFFS/ADC 阻塞、battery query 超时后的二次 deinit、完整 link 或 COM3–COM6 HIL，故仅是源码级生命周期与 HAL 边界证据，不能表述为电池策略、资源降载、存储 restart 或硬件休眠已验收。

> 2026-08-12 Connectivity late-publication admission 收口（全 profile shared Connectivity state）：复核 `stop_connectivity_root_transaction()` 已先停止 Wi‑Fi callback admission、物理 radio/netif/event loop，再释放 Connectivity EventGroup；但 service 中 `set_wifi_ready`、`set_cellular_ready`、provisioning session 及 snapshot 查询仍可在 deinit 后读写旧逻辑状态。迟到 modem recovery 或旧 callback 因而可能在 physical provider 已被关闭后把诊断/业务条件误报为 ready。现所有 runtime readiness 与 provisioning publication 都重验 live Connectivity generation，deinit 后的写入被忽略，`is_active_uplink_ready()` 与诊断 snapshot 也 fail-closed 为 not-ready；disconnect observation 同样拒绝 closed generation。已选 uplink 仍是持久配置，故保留“初始化前恢复/选择 Wi‑Fi 或蜂窝”的合法启动路径，不把该配置误当成 runtime provider admission。Device API、Platform Connectivity 和板级 ML307/Wi‑Fi 物理实现未暴露新 SDK 类型；cached Bread Xtensa `connectivity_service.c`、`device_api.c`、`main.c` `-fsyntax-only`、HAL boundary check 与本切片 `git diff --check` 通过。未做 late IP/ML307 callback、physical teardown 同时发生、full link 或 COM3–COM6 HIL，因此这是状态 admission 的源码级修复，不宣称 Connectivity runtime restart、蜂窝 quiesce 或网络故障恢复已经完整验收。

> 2026-08-12 UI → Device Display 重放载荷所有权收口（全 profile shared UI path，Display Task 前置）：复核 `app_ui` 的 replay state 后确认，它虽然会复制文本、响应图和二维码以供 alarm/foreground 恢复重放，但首次发布时部分调用仍将 HTTP 解码器、上传调用方或 QR producer 的瞬时指针直接传入 Device Display。当前 renderer 同步消费这些指针，因而尚未触发 UAF；但这会把调用方缓冲错误地变成显示载荷 owner，也会阻塞未来把 Display Service 改为队列化任务。现 `show_text`、`show_upload_progress`、`show_response`、`show_response_image`、`show_qrcode_modules` 与 `show_ready_prompt` 全部仅向 `device_display_*` 传递 `app_ui` replay-owned 的 title/text/stage/image/QR/SSID 副本；二维码输入在进入共享状态前验证为完整 N×N matrix 并复制、归一化模块值。`app_ui.h` 同时明确这些前景 payload 在调用返回后不再由调用方承担显示生命周期。动态字形 cache 仍由各同步 board renderer 立即 memcpy 到私有 cache，尚未把 72-byte glyph 纳入异步 payload queue；remote pet asset 继续沿用显式 consuming/non-consuming Device Display 合约。此次没有引入 Display Task、不可变完整 UI snapshot、scene revision、提交队列、DMA fence API 或 renderer restart 语义。cached Bread Xtensa `app_ui.c`、`display_service.c`、`platform_display.c`、`device_api.c` `-fsyntax-only`，`tools/check-hal-boundaries.ps1` 与相关 `git diff --check` 均通过；Waveshare `board_port.c` 仍因工作区缺少 managed `esp_lcd_touch.h` 无法在该缓存环境做语法编译，且未进行 full link、COM3–COM6 HIL 或异步显示压力验证，不能据此宣称独立 Display Service 已完成。

> 2026-08-12 Device Input 事件 generation identity 收口（全 profile shared Input/Interaction path）：统一事件模型要求异步结果不能只依赖可复用的全局 sequence。原 `device_input_event_t.sequence` 虽声明只在一个 Input Service 生命周期内单调，却没有随队列事件传递该生命周期身份；未来可 restart scanner 或延迟诊断 consumer 时，旧 generation 的 sequence 会与新 generation 重新从 1 开始混淆。现 Device Input envelope 升级为 ABI v2，新增非零 `generation`：Input Service 每次启动分配单调且跳过零的 lifetime generation，发布、consumer、stop sentinel 都携带/验证相同 generation；发布者在 initial admission 后重新检查 closing 状态，避免 stop 与 metadata 分配交叠时为已关闭 generation 形成迟到事件。App Intent ABI 同步升至 v2，将 `input_generation + input_sequence` 原样带入其有界队列，interaction task 与 `main.c` 入口拒绝零 generation。板级 adapter 仍只发布标准 action/source，未获得事件 metadata、FreeRTOS queue、GPIO 或触控类型；这不是完整通用 `device_event_envelope_t`，也没有将音频、网络、显示或 ISR producer 接入一条全局队列。cached Bread Xtensa `input_service.c`、`app_intent_service.c`、`main.c`、`device_api.c` `-fsyntax-only`，HAL boundary 与相关 diff check 通过；Input scanner 目前仍是 boot-lifetime，未做 stop/restart 交叠、queue 延迟注入、COM3–COM6 实体键/触控 HIL 或 full link，故仅构成 versioned input handoff 的源码级身份修复。

> 2026-08-12 App UI 单一显示提交边界补强（全 profile shared UI path，Display Task 前置）：尽管前景 text/image/QR 已改为 replay-owned 载荷，复核发现 command stage/lock、pet profile/asset、recording mode/level、glyph cache、Wi‑Fi/ambient/alarm status 等零散 UI mutation 仍可在不同业务 task 中绕过 replay mutex 直接进入 Device Display。同步 renderer 下这会让同一 UI model 的 mutation 与物理提交重排；未来替换为 Display Task 时也会把“唯一副作用 owner”错误留给队列实现。现所有 App UI 产生的 `device_display_*` 副作用均通过递归 replay submission lock 串行化；原先先清 recording 再获得锁的 foreground 路径改为 `stop_recording_if_needed_locked()`，确保 recording-clear 与随后 message/upload/response/QR scene 作为同一提交事务。Wi‑Fi SSID、ambient 字段和 command/pet state 在 model lock 下复制到 UI-owned local/model snapshot，renderer 不再接收调用者瞬时字符串。该锁仍只串行 UI→Device Display 的同步提交，不覆盖 profile pet animator、panel DMA callback、Power DISPLAY_OFF transition 或非 UI 的 board-local renderer 调用；没有引入 Display Task、队列、completion fence 或全面 immutable snapshot。cached Bread Xtensa `app_ui.c` `-fsyntax-only`、HAL boundary 与相关 diff check 通过；尚未做多 producer stress、Display Task migration、full link 或 COM3–COM6 HIL，故不能由此宣称显示并发/屏幕刷新已完整验收。

> 2026-08-12 App UI 宠物/闹钟字符串所有权补强（全 profile shared UI path，Display Task 前置）：继续审计同步边界后发现 `set_pet_profile()` 与 `set_alarm_visual()` 仍把 Hub/Alarm Manager 调用栈中的 `skin`、`time_text`、`label` 直接传给 Device Display。各现有 board renderer 会同步 copy，但这种偶然约束不应成为未来异步 Display Service 的安全前提。现 App UI model 持有标准化的 pet skin；alarm presentation 持有当前 time、label、frame、attempt/max-attempts，所有 alarm enable/disable 提交只使用这些 UI-owned 副本，alarm 结束仍按原逻辑重放最后 scene。pet profile 仍将原始 `motion_enabled` 同步交给 renderer，保持当前 profile-private “native motion remains enabled”的既有语义；重放调用仅重新声明已保存 skin 和等效 native motion，不把 remote metadata 误持久化为业务状态。宠物像素 asset 继续使用明确 Device Display consuming/non-consuming transfer contract，并未在 UI 内重复持有大帧。cached Bread Xtensa `app_ui.c` `-fsyntax-only`、HAL boundary 与相关 diff check 通过；未做 full link、alarm/pet 并发压力、Display Task queue 或 COM3–COM6 HIL，故这只是 transient text ownership 的源码级前置收口。

> 2026-08-12 App UI scene revision / replay binding（全 profile shared UI path，Display Task 前置）：为使未来异步显示结果能够识别其对应的 UI 状态，而不是仅凭当前全局 bool，`app_ui_model_t` 现包含 process-local、非零单调 `revision`。每次 shared UI 接受会影响呈现的 model mutation（surface、recording、command、pet skin/state、Wi‑Fi/ambient、alarm、音量 visual 等）都在 model lock 内递增 revision；每次选择/替换 replay payload 时也显式生成新 revision，并将该值绑定到 replay-owned payload。revision 只表示 UI scene admission，既不代表 panel/DMA 已完成，也不持久化、不跨 boot 比较，不能替代 Alarm/录音/网络各自的 operation identity。Display Service 既有 `submitted_revision` 继续只是所有语义提交的 facade 计数；两者尚未建立 enqueue/ack/fence 映射，因此没有假称“已拒绝所有 stale render”。cached Bread Xtensa `app_ui.c` `-fsyntax-only`、HAL boundary 与相关 diff check 通过；仍缺 immutable complete UI snapshot、Display Task queue/coalescing、per-submission revision propagation、renderer completion fence、异步 stale-drop 测试、full link 与 COM3–COM6 HIL。

> 2026-08-12 Display Service 单一同步 submission owner（全 profile，Display Task 前置）：此前 Display Service 仅为 Device API→Platform Display 提供 facade revision；多个 UI/业务 caller 仍可能在 revision increment 与 profile renderer 调用之间交错。现 service 内部持有 static recursive submission mutex，`app_main()` 在 App UI/brightness restore 之前初始化；每个 mutation 以“取得同一 owner → 分配 submitted revision → 同步调用 Platform Display → 释放 owner”的原子范围执行。response page 查询也在相同 owner 下读取 renderer 的可变 pagination 状态，但不分配新 submission revision。该 mutex 没有进入公共 Device/Platform header，也不保存 SDK panel、DMA、framebuffer 或 board types；profile renderer 仍是同步、boot-lifetime owner，因而圆屏/紧凑屏画面与实际呈现路径不变。此项并未实现 Display Task、队列 admission/deinit、payload deep-copy、coalescing、renderer completion fence 或板级 display restart；更不能把互斥序列化误表述为线程化异步 render。cached Bread Xtensa `display_service.c`、`device_api.c`、`main.c` `-fsyntax-only`、HAL boundary 与相关 diff check 通过；未做多 producer contention、full link 或 COM3–COM6 HIL。
> 2026-08-12 紧凑屏共享 renderer 物理命名/翻译单元收口（Bread Compact / Fangtang-4G，Phase 5 增量）：复核 Fangtang 已停止 `#include` Bread translation unit 的 bridge 后，发现两个 profile 的 CMake source 仍名为 `board_port_bread_compact.c`。这会把共享业务/场景会话实现错误标记为 Bread 私有实现，也会诱导新增紧凑硬件继续复制或修改 Bread board port。现将该 shared translation unit 重命名为 `compact_renderer.c`，Bread 和 Fangtang 均由 CMake 明确选择它；Fangtang 仍单独编译其 profile-private bridge/identity composer，Bread/Fangtang 的 display、audio、input、peripheral 与 connectivity adapter 继续在 `boards/<profile>/` 下拥有物理硬件对象。renderer log identity 同步改为 `maclaw_compact_renderer`，字体生成脚本改扫新 source，避免下次生成资源时引用已删除文件。静态搜索确认 `main/` 与开发工具不再引用旧 `board_port_bread_compact.c`；HAL boundary 与相关 diff check 通过。随后恢复本机 ESP-IDF 6.0.2/Python 3.12 profile 环境，并在重配后完成完整 link：Bread app `0x33a0c0`、最小 app 分区余 `0x65f40`（11%）；Fangtang app `0x336870`、余 `0x69790`（11%）。Fangtang 的生成 compile database 同时确认 shared object 已为 `compact_renderer.c.obj`，并与 profile-private bridge、identity composer 一起进入同一 image。此项只完成 shared renderer 的物理身份拆分：`compact_renderer.c` 仍是 boot-lifetime renderer，尚未完成独立 display/power runtime restart 或 COM4/COM5 HIL；完整链接也不能替代实机验收。
> 2026-08-12 Fangtang Compact renderer stage-2 HIL、重配修复与 COM5 release 回归（Phase 4 增量）：将独立 `fangtang-4g-renderer-fi` image 切至 `CONFIG_MACLAW_COMPACT_RENDERER_FAILURE_STAGE=2` 后，首次直接 Ninja 增量暴露重配后的 compile database 没有把 NV3023/ML307/UHCI public include trees传给 shared `compact_renderer.c`，在 profile-private `fangtang_display_adapter.h` 报 `esp_lcd_nv3023.h` 缺失。问题是 test build 的 stale CMake graph，而非 renderer/HAL API：按 `tools/build-profile.cmd` 同一物理 profile 语义重配（`MACLAW_PROFILE=fangtang-4g`、选定 Fangtang extra component carrier、test SDKCONFIG/defaults）后，compile database 确认 shared renderer 同时取得 NV3023、ML307、UHCI include；无须向共享头或业务层泄漏这些 driver。重配后的单线程全量 link 通过，app `0x3702c0`、最小 app 分区余 `0x2fd40`（5%）。COM5 仅写入 app 分区且 hash 校验成功；冷启动依序确认 NV3023 panel、LCD 双缓冲、Fangtang input adapter、direct-I2S audio，随后精确命中 `compact renderer initialization stopped after hardware init at test audio adapter: ESP_FAIL`，并有序停止 board background、command-cancel、volume-persistence worker，最终 `startup degraded: phase=4 device_status=7 reason=input service`、`Returned from app_main()`；22 秒窗口无 reset loop/panic/WDT。测试后 COM5 已立即恢复 `build-unified-fangtang` release app，写入 hash 及 `verify-flash` digest 均匹配；27 秒 reset smoke 确认 `BOOT_STATUS.ready=true`、NV3023/LCD、Input/Power/Wake-deadline/Sleep-schedule、电池 telemetry、Wi-Fi/Gateway handshake 与 offline wake listener 正常，无 degraded/panic/WDT。Fangtang stage 1、2 现有 HIL 证据；stage 3–5、runtime restart、board/panel/audio teardown、Display STOP timeout 与 DMA/scan-out fence 仍须独立验收。

> 2026-08-12 Compact renderer stage-3 HIL 与 COM4 release 回归（Phase 4 增量）：Bread Compact 独立非发布 `bread-compact-renderer-fi` image 已以 `CONFIG_MACLAW_COMPACT_RENDERER_FAILURE_STAGE=3` 完成单线程原生 Ninja 全量链接；app `0x33a5b0`，最小 app 分区余 `0x65a50`（11%）。COM4 仅写入 app 分区且 esptool 写后 hash 校验成功。冷启动日志依序确认 `LCD double buffering ready`、`input adapter ready`、`Bread Compact direct-I2S audio ready`，随后精确命中 `compact renderer initialization stopped after hardware init at test profile peripheral adapter: ESP_FAIL`；board background、command-cancel、volume-persistence worker 依序停止，进入 `startup degraded: phase=4 device_status=7 reason=input service` 并从 `app_main()` 返回。18 秒观察窗口内无重启循环、panic 或 WDT。测试后已立即将 COM4 恢复为 `build-unified-bread` release app，写入 hash 与 `verify-flash` digest 均匹配；25 秒 reset smoke 确认 `BOOT_STATUS.ready=true`、Input/Power/Wake-deadline/Sleep-schedule 服务、Wi-Fi、Gateway handshake 与 offline wake listener 正常，未出现 degraded/panic/WDT。至此 shared Compact renderer 的 Bread post-panel acquisition stage 1–5 已全部具备 HIL 证据；这不扩展为 runtime restart、board/panel/audio teardown、Display STOP timeout、DMA/scan-out fence 或 Fangtang stage 2–5 已验收。
> 2026-08-12 Fangtang-4G compact renderer stage-3 HIL 与 release 回归（COM5，Phase 4 增量）：重配独立非发布 `fangtang-4g-renderer-fi` 后，生成的 config 明确为 `CONFIG_MACLAW_COMPACT_RENDERER_FAILURE_STAGE=3`，并保留 Fangtang 的 NV3023/ML307/UHCI component closure。全量镜像已重新产出（app `0x3702c0`，最小 app 分区余 `0x2fd40` / 5%）并写入 COM5；实机依序确认 NV3023 panel、双缓冲、Fangtang input、direct-I2S audio 与 battery peripheral 已成功初始化，随后精确命中 `test profile peripheral adapter`。battery monitor、board background、cancel worker 与 volume-persistence worker 均有序停止，最终进入 `startup degraded: phase=4 … reason=input service` 并从 `app_main` 返回；采样窗口内未见 reset loop、panic 或 WDT。测试完成后已立即恢复 `build-unified-fangtang` 正式镜像，写后 `verify-flash` digest 成功；COM5 冷启动实机确认 `BOOT_STATUS.ready=true`、NV3023/Input/Power/Sleep Deadline、Wi‑Fi/Gateway handshake 与 offline wake listener 正常。此项仅验收 post-peripheral 的 boot-time retain/fail-closed rollback，不表示 runtime restart、panel/audio teardown、Display STOP timeout 或 DMA/scan-out fence 已验收。Fangtang 尚待 stage 4、5 与这些独立生命周期项目。

> 2026-08-12 Fangtang-4G compact renderer stage-4/5 HIL 与 COM5 release 回归（Phase 4 增量）：同一独立非发布 `fangtang-4g-renderer-fi` image 先后重配为 `CONFIG_MACLAW_COMPACT_RENDERER_FAILURE_STAGE=4`、`=5` 并写入 COM5，均经 esptool 写后 hash 校验。stage 4 在 startup selector 结束后精确命中 `test input completion semaphore`，因此没有启动 scanner/input service；stage 5 按预期在 hardware init 后、`compact_input_adapter_start_scan_task()` 之前精确命中 `test before input scanner task`。两轮均只停止已启动的 battery monitor、board background、command-cancel 与 volume-persistence worker，随后以 `startup degraded: phase=4 … reason=input service` 及 `Returned from app_main()` 结束；各观察窗口未见 reset loop、panic 或 WDT。stage 5 后 COM5 已恢复 `build-unified-fangtang` release app，写入 hash 与 `verify-flash` digest 均匹配；24 秒 cold-boot smoke 显示 `BOOT_STATUS.ready=true`、`SERVICE_STATUS.ready=true`、NV3023/LCD/Input/Power/Wake Deadline/Sleep Schedule、电池 telemetry、Wi‑Fi/Gateway handshake 与 offline wake listener 均正常。本项将 Fangtang shared compact renderer 的 boot-time post-panel acquisition stage 1–5 HIL 覆盖补齐；它仍不证明 renderer/runtime restart、panel/audio teardown、Display STOP timeout、DMA/scan-out fence 或完整 display lifecycle 已验收。

> 2026-08-12 Display Service 后置启动故障注入发现与修正待验（COM5 Fangtang，未验收）：新增编译期、test-build-only `MACLAW_DISPLAY_SERVICE_FAIL_AFTER_INIT` seam，目的是在 Display Task 发布、任一 UI submit 之前强制 composition-root rollback，以验证 Display Service task 的 STOP generation；它不公开到 Device/Platform HAL、Hub、HTTP、console 或 release image。首次 COM5 HIL（test app ELF `28a49bf0f`，写入 hash/`verify-flash` 成功）揭示现有 rollback 次序仍有缺口：`platform_lifecycle_stop_board_background_tasks()` 在尚无 background worker 时返回 `DEVICE_STATUS_TIMEOUT`，rollback 在该处 fail-closed 停止；随后 `startup_enter_degraded()` 试图用尚未 `app_ui_init()` 的 UI submission lock 显示错误页，触发 `xQueueTakeMutexRecursive` assert 并 reset loop。已作两处源码级修正：shared compact renderer 对未创建 background-task gate 的 inactive 状态返回 idempotent `ESP_OK`，并将该 test injection 保持在 `app_ui_init()` 之前，避免未初始化 UI 的提交。当前 test build 因 Component Manager 试图清理被外部进程占用的 `managed_components/espressif__mqtt/.../.git` 文件而无法完成重配，故修正后的 COM5 HIL 尚未重跑；不得表述为 Display STOP 已通过。首次失败后已立即恢复 COM5 的 `build-unified-fangtang` release image，写入和 `verify-flash` digest 成功，18 秒 cold-boot 再次出现 `BOOT_STATUS.ready=true`，未见 panic/assert/degraded。仍缺 renderer 卡死/STOP timeout、late exit、多 stopper、DMA/scan-out fence 与其它 profile 实测。
