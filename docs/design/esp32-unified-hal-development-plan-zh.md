# MaClaw AgentOS 开发计划（ESP32 统一业务与硬件抽象）

## 1. 文档信息

- 状态：待实施
- 日期：2026-08-06
- 系统名称：MaClaw AgentOS
- 评审轮次：第二十一轮产品决策审查（固定 16 MiB Flash 放弃设备端 OTA，改为 GitHub Release→Hub 版本检查/提醒→用户使用受校验刷机工具更新）
- 适用工程：`esp32s3-maclaw-client`
- 首批正式支持硬件：Bread Compact、EchoEar-2ST、Fangtang-4G
- 稳定 profile ID：`bread-compact-wifi-lcd-v1`、`echoear-2st-r8`、`fangtang-4g-v1`
- 唯一功能与业务行为基线：Bread Compact 当前已经验证的完整功能集合及处理方式。EchoEar-2ST 与 Fangtang-4G 必须逐项对齐 Bread Compact；除通过硬件适配表达的屏幕、输入、音频、连接与电源差异外，不得自行定义另一套功能或业务行为。
- 发布目标：三种硬件的用户可见业务功能完全一致。硬件差异只能影响交互映射、布局、性能预算、反馈通道和连接实现，不能形成三套业务、删减功能或长期 capability 缺口。
- 基线继承规则：所有当前及未来进入 MaClaw AgentOS 正式支持集合的硬件，都必须实现完整 Bread Compact 功能母版；不设置按硬件删减功能的“精简版”正式 profile。无法以适配或替代入口承载全部公共功能的硬件，只能停留在实验/Fake 状态或先修订硬件，不能进入正式支持集合。

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
- `.github/workflows/main.yml`：现有 `build-esp32-firmware` 使用 `espressif/idf:v6.0.2`，已分别构建 EchoEar、Bread 和 Fangtang；发布的 `${FIRMWARE_NAME}.bin` 是 `idf.py merge-bin -f raw` 生成、必须从 flash `0x0` 写入的整机镜像，只能作为显式恢复出厂/工装资产，不能用于默认“保留数据升级”，也不是设备端 OTA app image。Workflow 已通过 `softprops/action-gh-release@v2` 把 `release-assets/*` 发布到 `RapidAI/MaClaw` GitHub Release；本计划补齐带 offset/写区 allowlist 的分段升级 bundle、签名发布清单、三 profile 兼容元数据与刷机工具校验，不再产生设备端 OTA 专用产物。
- `partitions.csv`：硬件固定为 16 MiB，当前仅有约 3.625 MiB `factory` app、3 MiB model 和约 9.3 MiB storage；无 `otadata/ota_0/ota_1`。本地 app image 已观察到约 3.08 MiB。若同时加入 A/B 和完整 staging，storage 只剩约 1.7 MiB，无法可靠承载会议录音与资源，因此产品决策是保留现有单 app/model/storage 资源格局，放弃设备端 OTA。
- `main/main.c`/`main/CMakeLists.txt`：当前 `storage` 是 SPIFFS，会议录音、宠物素材和资源缓存共用；现有代码已记录 SPIFFS GC 在碎片化分区上可能持续很久。版本检查只能存储很小的 release metadata/提醒状态，不下载固件、不借用或清理会议存储。
- `MaClawSrv/thirdparty_gateway.go`：当前 bearer token 解析到 tenant/user principal，但 token 本身不是设备身份；Hub 版本检查 endpoint 必须再绑定已配对的 `clientId + deviceId + profile/hw/layout + credential_generation`，以免跨设备泄漏错误 profile 的发布信息。

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

```text
esp32s3-maclaw-client/main/
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
- 更新信息链路固定为 `GitHub Release → Hub release catalog → device metadata`：Hub 根据允许的 `RapidAI/MaClaw` Release 读取并验证签名 manifest，只向已配对设备返回适用 profile 的版本、channel、发布时间、重要级别、最低刷机工具版本、release notes 摘要及是否必须连接电脑升级。设备不直连 GitHub、不接收固件 URL、不下载固件。
- Hub 版本检查 endpoint 复用 device gateway bearer，并绑定 tenant/client/device/profile/hw/layout/credential generation；返回的 artifact ID/digest 仅供提醒和后续桌面刷机工具选择，不构成设备写入授权。GitHub 不可达时可以返回仍在有效期内的已验证 catalog metadata；没有可信缓存时返回可重试状态。
- 桌面刷机工具由用户主动运行并自行从 GitHub Release 下载完整 signed bundle；下载完成后先验证 manifest 签名、asset size/SHA-256、product/board/hw/layout/compat/flash size，再通过 USB/串口读取设备 identity 并要求用户确认，校验全部成功才开始刷写。Hub 不是固件数据中继，设备端也不存在隐藏安装 API。
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
- 设备版本检查只保存 `current_version`、`latest_seen_release_id`、`last_check_at`、`remind_after`、`dismissed_release_id` 和有界 release notes 摘要；不得下载或缓存 firmware bytes。检查失败不影响本地业务，退避重试也不能阻止休眠或闹钟。
- Hub API 固定为只读 metadata，例如 `GET /api/device-gateway/v1/updates/latest?clientId=...&channel=stable`。响应至少包含 release ID/version/channel/published_at/severity、profile/hw/layout/compat、bundle digest、minimum flasher version 和 release notes 摘要；不返回设备可下载的 asset URL。认证必须绑定真实设备与 credential generation，错误 profile/布局统一返回不适用。
- 版本比较使用显式协议字段和兼容矩阵，不依赖字符串字典序；相同 release 不重复打扰。默认启动后低优先级检查一次、随后最多每 24 小时一次；支持 Hub 下发的有界 `max_age`，但设备设有最小检查间隔和抖动，避免重连风暴。Critical 只提高提醒显著性和重复周期，不能远程触发下载、刷写或重启。
- 三硬件共享 `UPDATE_AVAILABLE` Scene 和 `UPDATE_REMIND_LATER/UPDATE_DISMISS_VERSION` intent：显示当前/最新版本、需要电脑和 USB、官方刷机工具入口提示；EchoEar 保留圆屏布局，Bread/Fangtang 用各自 Renderer。设备不显示“立即安装”“下载中”“刷写中”或百分比进度。
- GitHub workflow 对三 profile clean build 后生成 signed flasher bundle：用于普通升级的 bootloader/partition table/application/model 等独立 segment、每段 offset/length/SHA-256/write policy，以及仅供显式恢复出厂/工装使用的 merged raw image；同时包含 manifest、SBOM/provenance、release notes 和刷机工具最低版本。Manifest 至少绑定 product/board/hw revision/profile/layout/compat/flash size、firmware/data schema、每段资产与写区 allowlist、必须保留的 NVS/storage 范围、签名 key ID。发布顺序为 draft→上传全量→CI 回读 size/digest/allowlist→publish；已发布 asset 不可覆盖，修复只能创建新 release。
- 刷机工具必须从允许的 `RapidAI/MaClaw` GitHub Release 获取 bundle，并在写入前完成签名、size/hash、ESP target 与设备 identity/compat/layout/flash size 校验。用户选择错误硬件、identity 查询失败、bundle 不完整或签名错误时 fail closed；不能只靠文件名、串口号或手工板型选择继续。
- 刷机工具默认选择“保留用户数据升级”：按 manifest 逐 segment/offset 写入且只允许触达批准的 bootloader/partition table/application/model 区域，写前后核对 NVS/storage protected ranges，禁止把 padded merged raw image 从 `0x0` 整包写入。若 layout 或 data schema 变化无法原位兼容，必须在开始前明确展示受影响数据、要求用户导出/确认，并提供独立“恢复出厂刷机”；绝不能把恢复出厂作为普通更新默认项。
- 开始刷写前，工具要求稳定 USB 与供电，设备进入受控 maintenance/bootloader 模式；如固件仍可通信，先查询 active Meeting/Alarm/Persistence 状态并要求用户安全结束，写入开始后提示禁止断电。刷写完成后回读关键分区 digest，重启并核对 transaction ID、实际 firmware digest、`BOOT_OK` 与 `SERVICE_READY`；失败时停止自动重试，保留诊断并引导使用同一 signed bundle 恢复。
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
23. 在 GitHub workflow 中为三 profile 输出 signed segmented flasher bundle、manifest、SBOM/provenance、release notes 和 size/digest/layout/protected-range 证据；merged raw image 仅标记为恢复出厂/工装资产，不再生成 application-only OTA asset。
#### Phase 7A-b：Hub Update Catalog、统一提醒与受校验刷机工具

24. 实现 Hub `release_catalog`，只处理允许的 `RapidAI/MaClaw` GitHub Release/tag/channel 和已验证 signed manifest；对设备仅提供经 device gateway 认证、按 profile/hw/layout 过滤的 latest metadata API，不代理固件字节。
25. 实现 Update Catalog 的 tenant/client/device/profile/credential-generation binding、稳定错误/retry、metadata cache TTL、检查限流和审计；query `clientId` 不作为授权依据。
26. 实现共享 Update Service/Scene/intent/tool：`update.check/status/remind_later/dismiss_version`，完成 Bread/EchoEar/Fangtang 的提醒映射、相同版本去重、critical 重复提醒和离线/Hub 不可达降级；禁止 `ota.install`、URL、下载和重启入口。
27. 实现官方刷机工具从 GitHub Release 下载完整 bundle，验证 manifest signature、asset size/hash、device identity/profile/hw/layout/compat/flash size 和 minimum flasher version；错板、错布局、缺件、签名错误全部在写入前拒绝。
28. 实现“保留用户数据升级”和显式“恢复出厂刷机”两种独立模式；默认不得擦除 NVS/storage/未上传会议数据，layout/schema 不兼容时先导出/提示并 fail closed。
29. 实现刷机 maintenance 流程、稳定 USB/供电检查、开始写入后的禁止断电提示、关键区域回读 digest、重启后的 transaction/firmware digest/`BOOT_OK`/`SERVICE_READY` 校验和失败恢复指引。
30. 为单 app 无自动回滚冻结更严格的 stable 发布门禁：三硬件升级/降级矩阵、断电注入、错板拒绝、用户数据保留、刷后回读和上一稳定 signed bundle 恢复演练全部通过。

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
24A. 发布 bundle 负向测试：错 profile/layout/compat/ESP target/flash size、超 factory 槽、错 secure version、篡改 manifest/signature/size/hash、缺 bootloader/partition metadata、segment offset 越界/重叠 protected range、普通升级误选 merged raw image 全部在写入前被拒绝。
24B. GitHub Release 原子发布测试：allowlist/tag/channel、draft→完整上传/回读→publish、published asset mutation 隔离、canonical 签名 golden vectors、404/429/5xx/redirect/timeout 和旧刷机工具最低版本拒绝。
24C. Hub Update Catalog 授权/缓存测试：缺失/错误 bearer、跨 tenant/client/device/profile/credential-generation、伪造 query `clientId`、release 越权、metadata TTL/退避/限流、GitHub 故障和 token 日志泄漏；响应中不存在 firmware URL/bytes。
24D. 设备更新检查测试：版本比较、stable/beta、相同 release 去重、critical 重复提醒、稍后时间、忽略当前版本、重启/深睡持久化、Hub 离线与时钟不可信；测试桩断言设备无固件下载、partition erase/write、boot target 或远程重启调用。
24E. 三硬件提醒 UI 测试：Bread、EchoEar 圆屏、Fangtang 小屏显示相同 current/latest/severity/“连接电脑使用官方刷机工具”语义，输入只产生 `UPDATE_REMIND_LATER/DISMISS_VERSION`，无“立即安装”入口。
24F. 刷机工具身份/下载测试：只从 allowlisted GitHub Release 获取 bundle；USB identity/profile/hw/layout/compat/flash size 与 manifest 匹配，错板、用户选错文件、断网/部分下载和签名错误均在写入前 fail closed。
24G. 数据保留刷机测试：默认模式在任意成功/失败路径不擦除 NVS/storage/未上传会议，恢复出厂模式必须二次确认；layout/schema 不兼容时要求导出或拒绝，不静默格式化。
24H. 刷写/恢复测试：稳定供电/USB、active Alarm/Meeting 提示、写入中断电、关键分区回读 digest、重启后的 transaction/firmware digest/BOOT_OK/SERVICE_READY、失败停止自动重试及上一 stable signed bundle 恢复。
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
89. 有线烧录身份测试：目标 board/hw/layout/compat/flash/partition 不匹配、artifact manifest 缺失/签名失败和 identity 查询超时均拒绝写入；写后 transaction/boot session/digest/BOOT_OK/SERVICE_READY correlation 正确。
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
- GitHub Release 为三 profile 提供 signed segmented flasher bundle、manifest、SBOM/provenance 和 release notes，merged raw image 仅供恢复出厂；draft/缺件/被覆盖 asset 不可进入 Hub catalog 或刷机工具。
- 官方刷机工具在写前核对签名、size/hash、device identity/profile/hw/layout/compat/flash size，默认保留 NVS/storage/会议数据；错板、错布局或不可兼容 schema 不进入写入。
- 刷机开始后提示禁止断电，写后完成关键分区回读 digest、实际 firmware digest、BOOT_OK/SERVICE_READY correlation；单 app 无自动回滚风险由上一 stable signed bundle 恢复演练和更严格 stable 门禁控制。

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
108. GitHub workflow 为三 profile 产生不可变 signed segmented flasher bundle、manifest、SBOM/provenance 和 release notes；segment offset/write allowlist/protected ranges 明确，merged raw image 仅供恢复出厂，采用 draft→完整回读→publish。
109. 官方刷机工具从 GitHub 下载并完整校验 bundle，再匹配 USB 设备 identity/profile/hw/layout/compat/flash size；默认保留 NVS/storage/未上传会议，恢复出厂是独立确认模式。
110. 单 app 无自动回滚的风险由更严格 stable 发布门禁、刷前 maintenance、供电/USB 检查、刷后回读 digest、BOOT_OK/SERVICE_READY correlation 和上一稳定 signed bundle 恢复演练控制。
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

控制：同 release 去重，启动检查与周期检查有最小间隔、抖动和退避；`remind_after/dismissed_release_id` 持久化且有 schema。Critical 仅调整可见性和重复周期，不能远程下载、刷写、重启或绕过用户选择。

### 10.102 刷机工具下载到部分、篡改或错误 bundle

控制：工具只从允许的 GitHub Release 获取，完整下载后验证 canonical manifest 签名、asset size/SHA-256、工具最低版本和 product/profile/layout/compat；任何检查失败都发生在首个写入前，已发布 asset 变化整体隔离。

### 10.103 用户选错硬件或刷机工具只信文件名

控制：写入前通过 USB/串口查询设备 identity、flash size、layout/compat 与当前 firmware digest，再与 signed manifest 匹配；identity 查询失败或用户选择与实机冲突时 fail closed，人工确认不能覆盖安全错配。

### 10.104 普通更新误擦除 NVS、录音或资源

控制：“保留用户数据升级”和“恢复出厂刷机”分为两个显式模式；普通更新按 segment offset 与 manifest allowlist 写区，禁止使用 padded merged raw image，并负向验证 NVS/storage protected ranges 不变。layout/schema 不兼容时先导出或拒绝，不能通过自动格式化获得成功。

### 10.105 单 app 刷写中断导致设备不可启动

控制：stable 发布前完成三硬件断电故障注入和恢复演练；刷机前检查稳定 USB/供电，开始写入后明确禁止断电，刷后回读关键 digest。失败不循环自动重刷，工具保留日志并用同一或上一 stable signed bundle 引导恢复。

### 10.106 单 app 没有自动回滚却被误报为安全更新

控制：UI、release notes 和工具明确“需要电脑、升级中断可能需要恢复、无设备端自动回滚”。stable 门禁要求升级/降级 reader-writer window、BOOT_OK/SERVICE_READY 和上一稳定版本恢复证据，禁止以 OTA/A-B 文案对外宣传。

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
10. Phase 7A-b：GitHub workflow signed segmented flasher bundle/manifest、Hub Update Catalog metadata API、三硬件统一版本提醒、官方刷机工具 identity/签名/protected ranges/刷后回读与恢复闭环。
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
51. 声明 flash size/单 factory/model/storage layout、Update Catalog profile binding、版本提醒 Renderer/Input 映射、signed bundle 与刷机工具兼容、默认数据保留、刷后 readiness 和恢复证据；新板不得新建板型更新业务 Service，也不得在设备端引入隐藏 firmware download/partition write 路径。

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
