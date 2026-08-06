# 无名星智 4G（Fangtang 4G）硬件适配记录

本文档记录 MaClaw ESP32-S3 客户端对无名星智 4G 主板的适配边界。它是
独立板型，不能替代或改变现有 EchoEar-2ST、Bread Compact 的配置与行为。

## 资料来源

- 原始小智资料入口：<https://my.feishu.cn/wiki/F5krwD16viZoF0kKkvDcrZNYnhb>
- 主板名称：无名星智 4G
- 本工程配置名称：`Fangtang 4G`（Kconfig：`MACLAW_BOARD_FANGTANG_4G`）
- 固件板型标识：`fangtang-4g-v1`

资料入口目前是小智 AI 聊天机器人文档索引；引脚或模组信息以原理图、PCB
丝印或从原始固件提取到的板级配置为准，不以同类开发板的默认接线推断。

## 已确认硬件与交互

| 项目 | 已确认信息 |
| --- | --- |
| 主控 | ESP32-S3，16 MB Flash、8 MB Octal PSRAM、USB Serial/JTAG |
| 设备键 | 仅一个激活键，GPIO0，低电平有效 |
| 供电 | 独立电源开关，不作为应用输入 |
| 音量键 | 无；回复使用自动分页，不能使用 Bread Compact 的音量键翻页逻辑 |
| 显示 | 实机确认 240×240 RGB565；控制器 GRAM 为 240×320，可视窗口 X=0..239、Y=80..319 |
| 网络 | Wi-Fi / ML307 Cat.1 4G 双网络 |
| ML307 UART | UART1；ESP32 TX GPIO12 -> ML307 RX，ESP32 RX GPIO11 <- ML307 TX；921600 baud |
| 模组控制 | GPIO21 输出高；GPIO45 下拉输出低，均在调制解调器初始化前设置 |
| 电源检测 | GPIO38 为充电状态输入（高电平表示充电）；电池分压由 ADC2 channel 6 采样 |
| 启动手势 | 在开机选择窗口内双击 GPIO0 切换并持久化 Wi-Fi / 4G；窗口结束后按键恢复普通手势 |

## 当前软件约定

- Hub/GUI 下发的宠物帧保持跨设备通用的标准 `RGB565+A8`。Fangtang 实机最终确认使用 RGB MADCTL 元素顺序、`INVON` 极性和显式 `IDMOFF` (`0x38`)；宠物合成阶段保持标准 RGB565 数值不变，SPI 高低字节仍由显示合成链统一交换。`IDMOFF` 是恢复真彩输出的关键：NV3023A 复位默认处于 8 色 Idle 模式，缺少该命令会让宠物看起来像低色深或错误色表。以上设置仅在 `CONFIG_MACLAW_BOARD_FANGTANG_4G` 分支生效，Bread Compact 与 EchoEar-2ST 行为不变。
- Fangtang 专属的 GPIO0 启动双击与调制解调器配置均受
  `CONFIG_MACLAW_BOARD_FANGTANG_4G` 保护。
- EchoEar-2ST 保持触摸屏交互；Bread Compact 保持音量键翻页。
- Fangtang 的开机与交互状态使用独立的立体“方糖”产品标志，不复用 Bread Compact
  的内置机器人；待机页则显示 Hub/GUI 当前选择的真正宠物。交互页使用同一嵌入式
  `RGB565+A8` 白色方糖素材和独立三点指示，不再回退成深色几何盒。
- Fangtang 的 `thinking` 事务与 Bread Compact 共用同一状态机和阶段文案：
  `正在上传语音` → `正在提交指令` → `远端处理中`。三点指示按 420 ms 循环，页面保留
  `双击激活键可取消`；阶段切换、前台所有权以及最终回复到达时的动画终止条件保持一致，
  只有产品图形不同。
- Fangtang 待机页固定为两行信息：首行时间后紧跟 `ONLINE`/`WAIT`，第二行显示
  月日、星期和当前网络；其余 178 像素高区域留给宠物。宠物素材沿用共享的下载、
  SHA-256 校验、透明 RGB565+A8 合成、缓存和多帧动画链路，并按 240×240 视口缩放。
  首次下载或离线无缓存时暂以方糖作为占位，不把占位误报成宠物。
- Fangtang 的启动宠物下载任务使用 PSRAM 栈以保留内部堆给 Wi-Fi/ML307、TLS 和离线
  唤醒；SPIFFS 写入会临时关闭共享 cache，不能直接在该任务中执行。首帧持久缓存因此
  委托给 8 KiB 内部 RAM 栈的一次性任务，原下载任务等待写入完成后才释放源帧。这样既
  保留 Bread Compact 的“冷启动可恢复宠物首帧”能力，也不会再次触发 PSRAM 栈上的
  `spi_flash_disable_interrupts_caches_and_other_cpu` assert；Bread Compact 与 EchoEar
  的原有缓存路径不变。
- Fangtang 的开机画面与待机画面相互独立：开机画面只显示具象白色方糖和
  `MaClaw Mate`，握手及 Welcome 语音播放期间保持不变；Welcome 播放完成后才进入
  待机画面。待机首行是“时间 + 在线/等待”，第二行是日期、星期及当前网络图标：Wi-Fi
  使用无线波纹图标和独立高对比 `WIFI` 字标，4G 使用信号柱和更小、上移的 `4G`；网络标志与日期/星期
  保留 9 像素间距。布局分别按 68 像素的“波纹 + WIFI”和 38 像素的“信号柱 + 4G”
  真实宽度居中；两种文字均绘制在 y=39..52，与 y=68 起始的待机方糖保留 15 像素净空，
  避免右侧裁切或被方糖覆盖；`WIFI`/`4G` 固定标签直接写入合成帧，不依赖临时 DMA
  位图分配。标签先清理自身背景，Wi-Fi 文字使用比图标更亮的前景色，避免与波纹粘连后
  看起来像“只有信号标志”；其余空间显示方糖。
- 待机首行的“在线”表示当前运行期已认证且 Hub 轮询可达，不写入 NVS。Wi-Fi 断开、
  ML307 PDP 掉线或连续两次 Hub 轮询失败会切换为“等待”；轮询恢复后自动回到“在线”。
  因此重启不会沿用上一次启动的陈旧在线状态，网络图标仍只表示当前选择的 Wi-Fi/4G
  上行类型。
- Wi-Fi 冷启动的首次连接即使超过 20 秒等待窗口，后续 DHCP 成功也会自动重新启动
  Hub 配对/握手流程，不需要再次重启设备。该恢复入口在 Wi-Fi 驱动、时钟和本地闹钟
  调度器初始化完成后才开放，避免 IP 事件过早并发启动 TLS；Bread Compact、Fangtang
  Wi-Fi 与 EchoEar-2ST 共用此恢复逻辑，Fangtang 4G 仍使用独立的 ML307 恢复任务。
- 激活键在进入录音前会检查当前上行、配对和设置状态。Wi-Fi 或 ML307 断开时不会录制
  一段注定无法提交的 WAV，而是直接提示稍后重试；网络恢复后同一按键入口自动可用。
  离线唤醒入口使用相同的网络门控。
- 开机和待机的方糖不是深色科技盒或纯几何占位图，而是独立嵌入的高清具象白方糖素材：
  使用象牙白压制糖体、细密糖晶、轻微不规则边缘、三面自然明暗、柔和落地阴影和少量散落
  糖粒表现“可放入咖啡的方糖”。RGB565 图和 A8 透明蒙版只进入 Fangtang 固件；生成源为
  `tools/generate_fangtang_sugar.py`，预览为 `main/fangtang_sugar_preview.png`。
- 素材预览使用与设备开机页一致的深色底进行验收；方糖三个面均保持高亮奶油白，阴影为
  暖咖啡色，使用双尺度压糖纹理、可见晶粒和松散糖粒强化拟物感。即便映射到 RGB565，
  也不得出现黑色顶面、深灰金属盒或纯色几何块的观感。
- 方糖素材保持标准 RGB565，面板极性由 RGB MADCTL + `INVON` + `IDMOFF` 初始化统一
  修正；素材本身不再做第二次反相。它使用白色/象牙色糖体、颗粒糖霜、细小凹坑、
  三面柔和明暗和落地阴影，避免在已修正的真彩链路上显示成黑色。该素材只在
  `CONFIG_MACLAW_BOARD_FANGTANG_4G` 分支生效，不改变 Bread Compact 或 EchoEar-2ST。
- Fangtang 固件不再链接 Bread Compact 专用的 240×320 RGB565 机器人开机图；该
  150 KiB 资源只由 `CONFIG_MACLAW_BOARD_BREAD_COMPACT_WIFI_LCD` 构建嵌入。三板的
  启动视觉和固件资源因此在编译阶段隔离，而不是仅在运行时跳过错误板型的图片。
- Fangtang 在握手中声明支持最多 8 帧远程宠物动画/素材，并处理运行期宠物切换；
  方糖只作为板型产品标志，不再阻止宠物下发。Bread Compact 和 EchoEar 的宠物行为
  保持原样。
- Fangtang 虽然没有实体音量键，但保留统一远端音量入口：握手声明 `volumeControl:true`，Hub 下发的
  `hardware_config.extra.volume` 可设置 0–100 音量，direct-I2S 播放按该百分比做软件增益。音量写入
  `maclaw/output_vol` 并在重启后恢复；0% 是正常静音，不再被误报为播放失败。Bread Compact 与 EchoEar-2ST
  同样保留原有音量入口和播放实现，回复翻页映射不变。
- Fangtang 没有翻页键：多页文字回复每 6 秒自动前进一页。
- Fangtang 的语音/会议录音波形、会议上传进度和闹钟页面均使用独立的
  240×240 布局；不会沿用 Bread Compact 的 240×320 底部坐标而把提示画到可视区外。
- 三种板型都会在各自网络驱动启动后初始化共享的本地闹钟存储和调度任务；因此 Hub
  宣告的 `alarm_create`、`alarm_clear`、`alarm_clear_all`、`alarm_list` 工具不会访问
  未初始化状态，已持久化闹钟在当前网络暂时离线时也仍可本地响铃。Fangtang 响铃时
  唯一的 GPIO0 激活键可立即停止，Bread Compact 与 EchoEar 保持各自原有停止输入。
- Fangtang 保留原板的电源遥测：启动后读取 GPIO38 充电状态，并按原始固件的
  ADC2 channel 6 电量分段表进行三点平均；该接口不占用 Bread Compact 的 GPIO38
  音量键。物理电源开关仍是硬件断电，不作为应用按键。
- Fangtang 的就绪页明确提示“按激活键说话、双击开会议”，不显示 EchoEar 的
  “点屏”提示；配对成功及会议启动失败等后续提示也按板型显示“激活键”或
  “屏幕”，不会把 EchoEar 的触屏说明显示在 Fangtang 上。会议录音期间按唯一
  激活键即可停止并保存。
- ML307 的 HTTP 传输使用四个模组请求槽位分流：只串行化不带调用方标识的
  `MHTTPCREATE` 分配阶段，分配完成后按 HTTP ID 并发等待各自 URC。这样 5 秒的
  outgoing 长轮询不会阻塞语音上传、命令提交、ACK、取消或会议请求。
- ML307 单次 `MHTTPCONTENT` 原始数据上限为 4096 字节；语音和会议分块 PUT 的
  请求体按 4 KiB 写入模组，避免把 1 MiB 会议块一次塞给 UART AT 命令。流式写入按
  原厂 `Ml307Http::Write()` 语义执行，包括第一个 4 KiB 分片在内每片都使用追加模式
  `1`；模式 `0` 只属于一次性 `SetContent()`，不能与后续流式追加混用。Hub 仍看到
  一个具有原始 `Content-Length` 的完整 PUT，请求语义不变。
- Fangtang 的语音 WAV 上传使用 10 秒单片等待、最长 90 秒前台请求窗口，并每 32 KiB
  记录一次模组写入进度。这些等待和分块策略只在 ML307 传输层生效，不改变 Bread
  Compact 的 Wi-Fi/HTTPS 语音上传路径。
- 前台 ML307 请求注册为可取消请求；处理阶段双击取消时会立即唤醒本地等待者并
  删除对应的模组 HTTP ID，再独立发送 Hub `/cancel`，不会等待长轮询结束。
- 4G 模式不经过 ESP-NETIF，不能直接使用 SNTP；客户端从认证握手响应的
  `serverTime`（Unix 毫秒）校准系统时钟，并独立启动待机时钟任务。Wi-Fi 仍保留
  原有的多服务器 SNTP 校时，并在 Wi-Fi 驱动启动完成后、Hub 握手前启动；因此 Hub
  暂时不可达时待机时钟和已持久化闹钟仍会运行。两种网络下的时钟、天气过期判断和
  闹钟均可工作。
- 4G 使用原厂相同的 `78/esp-ml307` UART1 AT 网络栈，由模组提供 HTTP/HTTPS/TCP，
  不是通用 `esp_modem` 的 `ATD*99#` PPP 路径。默认网络选择为
  4G；它会先读取本工程 NVS 的 `maclaw/net_transport`，若不存在则兼容读取
  原始固件的 `network/type`（`1` 为 ML307、`0` 为 Wi-Fi），再持久化为本工程值。
- `MACLAW_FANGTANG_MODEM_APN` 留空时使用 SIM/模组的默认 PDP 配置；配置非空安全
  APN 时，客户端会在网络注册和取 IP 前通过 `AT+CGDCONT=1,"IP",...` 写入。
- Hub 地址的 4G 兼容转换在 1.8 秒启动双击窗口结束、最终网络类型确定后执行：
  Wi-Fi→4G 会正确选择 9399 直连地址，4G→Wi-Fi 会保留正常 Wi-Fi Hub 地址。
- Fangtang 在选择 4G 后会保留一个低优先级 ML307 恢复任务。冷启动注册失败或运行中
  `CEREG`/PDP 掉线时，它以指数退避重新执行模组注册；网络恢复后重新启动尚未完成的
  配对/握手流程。该任务只在 Fangtang 4G 模式存在，不改变 Bread Compact 和
  EchoEar-2ST 的 Wi-Fi 重连行为。
- ML307 传输层每 5 秒主动查询 `CEREG` 和 `MIPCALL`。这补足仅靠模组 URC 无法稳定
  发现 PDP 静默掉线的问题；`MIPCALL` 返回 inactive 时会立即清除网络就绪状态，交由
  上述恢复任务指数退避重建注册与 PDP。

## 显示实测与接线

原始 `xingzhi-cube-0.85tft-ml307` 板级源码把显示登记为 128×128 NV3023，
但该参数不符合本次接入的实机。独立显示测试程序逐行探测后确认：

- 实际可视尺寸为 240×240；控制器显存纵向为 320 行。
- 完整可视地址为 X `0..239`、Y `80..319`，因此软件逻辑坐标需增加 Y offset 80。
- RGB565 两字节像素、SPI mode 0、40 MHz 可稳定更新全屏。
- 原厂初始化表及 `COLMOD=0x65` 保留。真彩输出的实测组合是 RGB MADCTL 顺序、
  `INVON`、RGB565 字节交换，以及初始化后显式退出 8 色 Idle 模式（`IDMOFF`）。
  240×240 可视窗口仍使用 GRAM Y offset 80。

显示接线为 SPI3：MOSI GPIO10、SCLK GPIO9、DC GPIO8、CS GPIO14、RST GPIO18、
背光 GPIO13。直连 I2S 麦克风/扬声器接线为 MIC WS GPIO4、SCK GPIO5、DIN GPIO6；
SPK DOUT GPIO7、BCLK GPIO15、LRCK GPIO16。以上引脚均与 EchoEar-2ST 不同。

仍需按 SIM/运营商确认 APN；当前配置允许 APN 为空，由网络侧自动选择。

4G 路径已按原始 ML307 板级参数实现：上电后先设置 GPIO21/GPIO45，再以
UART1 的 921600 baud 自动探测 ML307，等待 `CEREG` 注册和 `MIPCALL` 地址，
随后通过模组原生 HTTP/HTTPS 命令访问 Hub；若 ML307 未应答会显示可诊断错误。

ML307 的固件 TLS 能力无法与当前 `https://hub.mypapers.top` 的 ECDSA-only 证书
完成握手，关闭模组证书校验也不能补足该密码套件能力。因此仅当 Fangtang 选择
4G 且仍使用默认 Hub 时，运行时改走同一 Hub 服务的直连地址
`http://hub.mypapers.top:9399`；Wi-Fi 及用户自定义 Hub 地址保持原配置。配对、
握手、长轮询、普通命令、媒体下载、语音配对和会议分块 PUT 均已接入 ML307
原生 HTTP 路径。实机已取得移动网络地址并到达 Hub；HTTP 401
`invalid_pairing_code` 表明传输正常、保存的一次性配对码已过期。

4G 模式下的配对恢复仍开启本地 `MACLAW-SETUP-xxxx` 热点，但 ML307 继续作为
回传网络。此路径使用纯 SoftAP，不启动无用的 Wi-Fi STA 扫描/重连，避免 ESP-IDF
扫描定时器竞争；实机已确认热点、DHCP、DNS 和配置网页能够稳定运行。

## 构建配置

使用独立的 Fangtang SDK 配置，避免把已生成的 EchoEar/Bread `sdkconfig` 带入：

```powershell
cmd.exe /d /s /c "call C:\esp\v6.0.2\esp-idf\export.bat >nul && idf.py -B build-fangtang -D SDKCONFIG=sdkconfig.fangtang-4g reconfigure"
```

Fangtang 默认值放在 `sdkconfig.defaults.fangtang-4g`。ML307 使用模组原生 AT HTTP，
因此 Fangtang 默认配置也不启用 lwIP PPP；EchoEar 和 Bread Compact 的默认网络配置
不受影响。
刷写端口仅为本次实机的 COM5；COM4 是 Bread Compact，不得用于本板固件。

## 2026-08-06 实机回归

本轮修复了 Fangtang 复用 Bread Compact 显示端口时遗漏的屏幕高度差异：录音波形、
会议上传进度和闹钟页面现已全部限制在 240×240 可视区。Bread Compact 的 240×320
布局通过板型条件编译保持原样，EchoEar-2ST 仍使用独立端口。

三板均用各自 SDK 配置重新构建成功：Fangtang 4G、Bread Compact、EchoEar-2ST。
Fangtang App 固件为 3116896 字节，SHA-256：
`7DD79631E87D54E2A6C19F91BCF0249E4CFA676CE39094DEE970F63636DDFED0`。
只把该 App 分区刷到 COM5 的 `0x10000`，随后 `verify-flash` 返回
`Verification successful (digest matched)`；未刷 bootloader、分区表、NVS、模型或
storage，COM4 未被访问。启动日志再次确认 `board_id=fangtang-4g-v1`、NV3023
240×240 / GRAM Y=80..319、GPIO0 启动窗口、电源遥测、ML307 注册及中国移动 PDP
均正常，未出现 panic、assert、Guru Meditation 或 watchdog。

当前保存的一次性配对码仍已过期，Hub 返回 401 `invalid_pairing_code`，设备随后稳定
进入 `MACLAW-SETUP-61BD` 配对恢复热点。因此握手后的语音、回复、自动翻页、会议
上传、取消、闹钟和 4G 掉线恢复仍需在提交有效新配对码后完成实机端到端回归。

### 4G 配对与握手补充实测

已使用 GUI 当前远端 Hub 身份生成新的 30 分钟一次性配对码，并在写入前完整读取、
备份 COM5 的 0x9000/0x6000 NVS；仅替换 `maclaw/pair_code`，其余原厂及 MaClaw 键值
逐项保留。设备随后经 ML307 成功换取并保存独立 `gateway_token`；重启后直接以 Token
完成 handshake 200。握手返回的 `serverTime` 已校准系统时钟，服务状态进入 ready；
远端 Welcome 音频经 4G 长轮询下发、媒体下载、扬声器播放并 ACK 200。运行期间 5 秒
`CEREG`/`MIPCALL` 探测持续成功，未出现 panic、assert、Guru Meditation 或 watchdog。

实测日志为 `serial-com5-fangtang-live-pair.log`；配对后的 NVS 复读只记录存在
`maclaw/gateway_token`，不在文档或日志中公开 Token 内容。

### Thinking 与拟物方糖补充回归

Fangtang `thinking` 阶段显示已对齐 Bread Compact 的事务阶段，并使用同样受前台所有权
和最终回复门控的 420 ms 动画节奏。动画复用 Fangtang 已驻留的宠物/自动翻页定时任务，
每次只更新 45×11 像素的三点区域；该区域按已实机验证的 NV3023 单行窗口协议逐行提交，
避免驱动把紧凑局部缓冲误作 240 像素全宽行而越界读取。现在不再在语音上传和 Hub
长轮询期间以 80 ms 间隔重传完整 240×240 画面。开机、状态和提示页统一使用标准
RGB565 的象牙白拟物方糖；修复了
面板已启用 `INVON` 后素材再次反相而呈黑色的问题。

Fangtang、Bread Compact、EchoEar-2ST 均按各自配置重新构建成功。最新 Fangtang App
为 3228640 字节，SHA-256 为
`04AEB28001D54B45A4D6D120B878C37EE680CFECF4D10845380AF1036E96C1C9`；仅刷入 COM5
的 App 分区 `0x10000`，esptool 写后返回
`Hash of data verified`；COM3/COM4 未访问。启动日志
`serial-com5-fangtang-thinking-row-safe.log` 确认新固件身份、240×240 NV3023、Wi-Fi
handshake 200、离线唤醒初始化和 `service_ready=true`，未出现 panic、assert 或
watchdog。三个交互阶段与双击取消仍需通过一次真实语音命令完成最终屏幕目视确认。

### 咖啡方糖素材 V2 实机包

方糖 V2 改用与设备开机页一致的深色底进行素材验收，三个面保持高亮奶油白；暖咖啡色
接触阴影、双尺度压糖纹理、晶粒高光和少量松散糖粒均已针对 240×240 / RGB565 显示强化。
这次只更新 Fangtang 专属素材与文档，不改变 Bread Compact、EchoEar-2ST 的资源或逻辑。
Fangtang 构建通过，App 为 3228960 字节，SHA-256 为
`BD428674D4757DF17FC50D285FD2F6E5DB3AB1C899791E0DA37F017AB9F15345`；仅刷入 COM5
App 分区 `0x10000`，esptool 返回 `Hash of data verified`，未访问 COM3/COM4。

### 单键自动翻页状态一致性回归

Fangtang 没有音量/翻页键，文字回复仍由紧凑屏 HAL 每 6 秒自动前进一页。UI 适配层现在也把
Fangtang 与 Bread Compact 一样视为“紧凑屏 HAL 是可见回复的最终状态源”，因此即使终态
回复绘制与一次较晚的 UI 模型更新发生竞态，自动分页定时器仍能识别并保留当前回复页，
不会误判为无响应页面。该改动只扩展 Fangtang 的回退判定；Bread Compact 的物理翻页键
和 EchoEar-2ST 的页面路径保持不变。

Fangtang、Bread Compact、EchoEar-2ST 三种配置均重新构建成功。Fangtang App 为
3229168 字节，SHA-256 为
`03563AE5234120ED24E7CA70DDB65AC888652A05FA76C205EBED138D7902CE51`；仅刷入 COM5 的
App 分区 `0x10000`，esptool 返回 `Hash of data verified`。冷启动日志
`serial-com5-fangtang-full-client-audit-coldboot.log` 确认 Fangtang 身份、240×240 NV3023、
Wi-Fi 连接、握手 200、Welcome 播放、会议能力协商、宠物首帧以及 `service_ready=true`，
未出现 panic、assert、Guru Meditation 或 watchdog。

### 录音停止触点消费回归

激活键按下沿在命令录音期间会立即请求停止采集，不必额外等待 500 ms 的单击/双击判定。
同一个物理触点随后产生的 `SHORT`/`DOUBLE` 完成事件现被显式消费；此前该延迟事件可能在
录音已经切换到 Thinking 后被重新解释为普通输入，造成新事务、会议或回复页面被误触发。
下一次真正的按下沿会解除该单触点屏障并按当前状态正常处理。此语义适用于共享事务层，
保留 Bread Compact 和 EchoEar-2ST 的原有输入来源及能力。

三种板型配置均重新构建成功。Fangtang App 为 3229248 字节，SHA-256 为
`46AA147AC507921BA28A245FFFBFF3EC87FFB8E7B17036DE125F258C3D964B95`；仅刷入 COM5 的
App 分区 `0x10000`，esptool 返回 `Hash of data verified`，未访问 COM3/COM4。

### 命令录音门限实机校准

COM5 实测显示 Fangtang 的离线唤醒可以稳定识别 `ma ka long`，但切换到命令录音后，
正常近场说话的峰值经同一 I2S 归一化链常落在 `45..70/1000`，原先 `75/1000` 的
起音门限会出现“已唤醒但六秒内未检测到后续语音”。Fangtang 专用起音门限现校准为
`45/1000`，同时继续保留 `160 ms` 连续确认、去直流均值能量静音检测和 `1500 ms`
自然停顿结束，避免单帧麦克风尖峰触发命令。该变更位于
`CONFIG_MACLAW_BOARD_FANGTANG_4G` 分支，不改变 Bread Compact 或 EchoEar-2ST。

### 咖啡方糖与 ML307 取消补充回归

开机及状态页的方糖素材进一步调整为暖白咖啡方糖：三个面保持奶油/象牙白，不使用
黑色描边；增加在 240 像素屏和 RGB565 量化后仍可识别的压制砂糖颗粒、微孔、晶体
高光、轻微不规则边缘和暖色柔和接触阴影。确定性生成脚本仍为
`tools/generate_fangtang_sugar.py`，素材预览为 `main/fangtang_sugar_preview.png`，按屏幕
RGB565 与开机位置模拟的预览为 `main/fangtang_sugar_device_preview.png`，运行时
继续使用 RGB565+A8 与页面底色合成。

V3 素材进一步提高正面亮度并弱化黑边，保留可被 RGB565 看见的压糖微孔、晶粒高光、
轻微不规则压制边缘以及暖咖啡色反光/接触阴影，避免在深色开机页上看成黑色电子盒。
Fangtang 独立干净构建通过，App 为 3229264 字节，SHA-256 为
`E10EB11BD8E26570F3BD915CE40B361F9B7CD0AC3F8396508331144334AB2245`；仅刷入 COM5
App 分区 `0x10000`，写后校验为 `Hash of data verified`。冷启动日志
`serial-com5-fangtang-coffee-sugar-v3-clean-boot.log` 确认 Welcome 完成、
`service_ready=true`、宠物 8 帧加载完成，未出现 panic、assert、Guru Meditation 或
watchdog。COM3/COM4 未访问。

V4 继续针对实机上“黑乎乎的盒子感”收敛：三个可见面都提高到奶油白/象牙白，降低接触
阴影的不透明度，并在两个竖直面补充能够穿过 RGB565 量化的糖晶簇。方糖仍有明暗关系，
但明暗只用暖白与浅焦糖反光表达，不使用黑面、黑边或科技盒状态色；因此开机页应首先
读成可投入咖啡的压制方糖，而不是立方体图标。

### 运行时宠物缓存内部栈保护

Fangtang 的长轮询任务和启动宠物下载任务都使用 PSRAM 栈；SPIFFS 在 `unlink`、写文件、
垃圾回收时会临时关闭共享 Flash/PSRAM Cache，因此不能直接从这两类任务执行缓存变更。
运行时 `pet_profile` 的首帧缓存以及无素材配置的缓存清理，现与启动首帧缓存统一经由一个
串行化的内部 RAM 栈工作线程执行。调用方会等待工作线程完成后才释放所借用的 RGB565A8
帧；缓存写入与清理由同一互斥量保护，避免并发更新产生混合版本。Bread Compact 与
EchoEar-2ST 沿用原有路径，不受 Fangtang 专用桥接影响。

ML307 前台请求取消也做了有界化。双击激活键取消语音事务时，取消任务会立刻设置
请求取消标志并唤醒响应等待；若此刻模组恰好在串行分配 `MHTTPCREATE` 槽位，最多只
等待 3.5 秒，不会永久卡在 create 锁上。正在分配的请求随后会在自身原有的 3 秒有界
等待中观察取消标志并释放晚到的 HTTP 槽位。这一修改只编译到 Fangtang 的 ML307
传输单元，不改变 Bread Compact 或 EchoEar-2ST 的 Wi-Fi 事务路径。

三板构建再次通过。当前 Fangtang App 为 3228944 字节，SHA-256 为
`B456AA13F4C92C0E7FE96C055C3CF3ED15493F50BA98FB0548E7FFE556D733E2`；仅刷入 COM5
App 分区 `0x10000`，esptool 返回 `Hash of data verified`。冷启动日志
`serial-com5-fangtang-cancel-bounded-coldboot.log` 确认保存的网络类型为 Wi-Fi、
handshake 200、Welcome 播放完成且 `service_ready=true`，未出现 panic、assert、
Guru Meditation 或 watchdog。真实 Wi-Fi/4G 语音事务以及处理中双击取消的最终
端到端证据仍需用户在设备上实际触发后采集。

### 硬件命令关联与事务终结保护

Fangtang 与 Bread Compact 继续共用严格的事务关联规则：终态结果只有在 `replyTo`
等于当前 `voice-*` 命令 ID 时，才允许结束 Thinking 并进入结果页。Hub 入口现优先把
硬件 `control_reply_to` 作为队列任务的消息 ID，GUI/Hub 的终态文字、图片、文件和
语音会同时携带 `source_message_id`、`replyTo` 与 `replyToMessageId`。纯语音回复没有
先行终态文字时，最后一个真正通过能力和媒体校验的语音分片负责结束事务；若已经有
带 `speech_parts_pending` 的终态文字，后续 TTS 分片仍只负责播放，不会再次结束事务。

为兼容发布端升级前已经残留在队列中的旧消息，客户端对“活动命令期间、无任何
`replyTo` 的终态文字”执行安全消费：该消息不显示、不会完成或取消当前命令，但会 ACK
并推进共享 outgoing 游标，避免它永久堵住后方真正相关的结果。带有其他 `replyTo` 的
消息仍按孤儿结果静默丢弃；无关联的普通通知仍会等待前台事务释放，因此不会放宽防串话
边界。该规则位于三板共享事务层，Bread Compact、EchoEar-2ST 与 Fangtang 行为一致。

本次三板构建均通过：Fangtang App 为 3230032 字节（SHA-256
`1CD94EA5BB502F5FCADCEDC716D3ADC5E0BA895D63A56B8ECFA20DAC52F2E2BA`），Bread Compact
为 3203152 字节，EchoEar-2ST 为 3122384 字节。仅将 Fangtang App 刷入 COM5 的
`0x10000`，写后校验为 `Hash of data verified`；未访问 COM3/COM4。冷启动日志
`serial-com5-fangtang-transaction-correlation-boot.log` 已确认 Fangtang 身份、Wi-Fi
连接、handshake 200、离线唤醒、宠物下载及 `service_ready=true`，未出现 panic、
assert、Guru Meditation 或 watchdog。发布端关联修复仍需部署 Hub/GUI 后，再分别用
Wi-Fi 与 4G 完成真实语音、Thinking、终态结果、TTS、自动翻页及双击取消回归。
### 2026-08-06 Hub queued device identity fix

The real Wi-Fi voice regression reached wake word detection, command capture,
upload, the Thinking surface, and a correctly correlated terminal result.  The
terminal payload nevertheless contained `third-party hardware message is
missing client ID`.  The defect was in the Hub background queue: the inbound
`ClientTools` and `ClientToolContext` were present before enqueue, but were not
copied into `IMTask`, so the worker could not restore the concrete device ID,
conversation ID, or command `replyTo` before sending `im.user_message` to the
GUI Agent.

The Hub now copies both fields into `IMTask` and re-stashes them in
`InitTaskDispatcher`; the legacy non-queue route also preserves them.  Direct
tests cover both the queued task fields and the actual `im.user_message`
delivered to the Agent. `go test ./hub/internal/im -count=1` passes.  A Linux
Hub binary is staged at `build/deploy-hub-bin/maclaw-hub-context-fix` (SHA-256
`B0EF3D4C5E385B429A61B7B590C0C44263470D48BAFCBD4400DB8ABC2D6E1170`).
After deployment, COM5 still needs the final Wi-Fi/4G TTS, automatic paging,
double-click cancel, and meeting-upload end-to-end regressions. COM3/COM4 must
not be accessed for this work.

### 2026-08-06 部署后 Wi-Fi 语音事务实测

上述 Hub 队列身份修复部署后，COM5 已完成一次真实 Wi-Fi 语音事务。实测日志
`serial-com5-fangtang-hub-context-live-e2e-v2.log` 依次确认：离线“码卡龙”唤醒、
命令语音起音、静音自动收尾、WAV 上传地址申请、媒体上传、命令提交、带相同
`voice-*` ID 的终态文字、`speech_parts_pending=1`、相关 TTS 语音分片下载播放和
剩余分片归零。GUI 日志同时确认 Hub 下发的 `ClientToolContext` 已恢复
`client=esp32s3-98a316d161bc` 与 `replyTo=voice-55415173`；此前的
`third-party hardware message is missing client ID` 不再出现。

普通多模态事务也已修正为只由最后一个实际可交付的 image/file frame 终结；只有
携带有效 `speech_parts_pending` 的结果文字允许先结束 Thinking 并武装后续播放，
纯语音响应仍由最后一个通过能力与媒体校验的分片终结。相关 GUI 测试和硬件运行时
测试已通过，新 GUI 已替换运行并重新连接生产 Hub。

本轮同时把开机方糖调整为更明确的拟物咖啡方糖：亮奶油白顶面、暖蔗糖侧面、
可穿过 RGB565 量化的砂糖晶粒、细暖色倒角和浅咖啡反光，不使用黑色描边。素材由
`tools/generate_fangtang_sugar.py` 确定性生成，预览为
`main/fangtang_sugar_device_preview.png`。Fangtang App 构建通过，并只刷入 COM5
的 App 分区 `0x10000`；esptool 写后返回 `Hash of data verified`，未访问 COM3/COM4。

兼容性构建随后使用三块板各自的 SDK 配置重新执行并全部通过：Fangtang 4G App
为 3230080 字节（SHA-256
`3A9C2202BF6A63A1C7E7E3FB528574D4EE22C80E9275ED29CE2660920055C021`），Bread
Compact App 为 3203152 字节，EchoEar-2ST App 为 3122016 字节。本轮只构建后两种
配置，没有访问或刷写 COM3/COM4。

### 2026-08-06 Hub 部署后的离线审计与回归

生产 Hub 已部署上述队列身份修复。因现场无人操作，本轮按要求不执行依赖物理启动
双击的 4G 实机测试；已停止串口捕获进程并释放 COM5，也没有访问 COM3/COM4。此前
保存的 4G 实机证据仍证明 ML307 注册/PDP、原生 HTTP handshake、长轮询、媒体下载、
ACK 和 Welcome 播放可用；但“新 Hub + 4G”的完整语音、Thinking 双击取消和会议
分块上传仍属于待现场复测项，不能用本轮 Wi-Fi 证据替代。

离线代码审计确认 Fangtang 与 Bread Compact 共用同一事务层：录音完成后依次进入
“正在上传语音”“正在提交指令”“远端处理中”，提交使用幂等 `voice-*` ID，终态严格
校验 `replyTo`，文字先终态时保留 `speech_parts_pending` 供后续 TTS 播放。Fangtang
仅在传输层选择 ML307：大请求以 4096 字节 `MHTTPCONTENT` 连续追加，四个模组 HTTP
槽位有界分配，前台请求可由双击取消；会议录音沿用创建会话、带 SHA-256 的分块 PUT、
complete、process、NVS 断点续传和失败后重试流程。启动 1.8 秒窗口只消费 GPIO0
单击并把双击用于持久化切换网络；窗口关闭后，短按录音、处理中双击取消、待机双击
会议、会议中任意完成手势停止、长按进入配置，均不会泄漏到其他事务。

同时修正了一个 Fangtang 4G 诊断细节：ML307 模式发生连接错误时屏幕现在提示
“请检查 4G”，不再沿用 Bread/Wi-Fi 的“请检查 Wi-Fi”；该分支不改变其他板型。

启动网络选择器也补齐了窗口边界所有权：启动窗口内第一次释放的点击被完整归属给
网络选择事务，即使 500 ms 单击/双击判定在 1.8 秒窗口关闭后才到期，也会被消费，
不会泄漏为普通短按启动语音，或与窗口后的第二次点击组合成待机双击会议。只有整个
手势都从窗口关闭后开始，才进入正常单键事务。

当前源码重新用三块板各自配置完整构建通过：Fangtang 4G App 为 3230000 字节，
SHA-256 为 `46B16269A17B0412D66B9E0741CC90219D210EBFEAFD1F5A3B8DB3DEEEF44FC6`；
Bread Compact App 为 3203056 字节，SHA-256 为
`C3FC9A97B5AB88D80F9C635D825687E7095189939AEF0458C66102CF409084A5`；
EchoEar-2ST App 为 3122048 字节，SHA-256 为
`C1D1E7A82C8CFAAB06F3C4E0D84342708D4657F0044AA5ED24B0378A7F3B05B0`。
三者均通过 App 分区大小检查；本轮未刷写任何设备。

上述启动手势窗口边界修复随后用 Fangtang 与 Bread Compact 配置重新构建通过：
Fangtang App 为 3230112 字节（SHA-256
`980AD48DA0D2AC709B42223074C6008EB2DF78987855A507A4CFA01A6BCE6F22`），Bread
Compact App 为 3203056 字节。修复代码只在 `CONFIG_MACLAW_BOARD_FANGTANG_4G`
条件内改变手势所有权；Bread 构建用于证明共享源文件没有兼容性回归。本轮仍未刷机。
### 2026-08-06 4G 会议分块流式读取审计

现场无人，本轮按要求跳过 4G 实机测试，也未访问 COM3、COM4、COM5。离线审计发现：Hub 允许握手协商 64 KiB 至 8 MiB 的会议分块，但 Fangtang 的 ML307 路径此前会先为整个分块分配同等大小的 PSRAM，再交给传输层按 4 KiB 写入模块。设备只有 8 MiB PSRAM，同时还驻留双帧缓冲、宠物帧及任务内存，因此 8 MiB 协商值会确定性分配失败，较小分块也会造成不必要的峰值占用。

现在 ML307 增加了带回调的请求体流式接口。会议 PUT 直接从 SPIFFS 文件读入现有的 16 KiB I/O 缓冲，并继续以 4 KiB `MHTTPCONTENT` append 写入模块；Hub 看到的单个 PUT、`Content-Length`、`X-Chunk-SHA256`、分块编号以及 NVS 断点续传语义均不变。内存峰值从“协商分块大小”降为固定 16 KiB，因此完整保留 Hub 64 KiB 至 8 MiB 的协商能力，而不是人为缩小会议功能。普通语音和 JSON 请求仍走原有内存请求体接口；Bread Compact 与 EchoEar-2ST 的 Wi-Fi/HTTPS 上传路径不变。

配对恢复页的成功说明同时改为中性的 “The selected network is connected”，避免 Fangtang 选择 4G、仅以 SoftAP 提供本地配对页时错误声称 Wi-Fi 上行已连接。4G 会议上传、Thinking 双击取消、断线恢复和自动翻页仍需人员回到设备旁后完成最终实机回归，不能用本轮静态审计代替。

三板独立构建均通过 App 分区大小检查：Fangtang 4G 为 3231616 bytes（SHA-256 `75166E1C870252EF652C0A01A511140ECAE7E443EEEFA98DE133C7540CC4DED9`），Bread Compact 为 3203072 bytes，EchoEar-2ST 为 3122048 bytes。本轮只构建，未刷写任何设备。

### 远端音量与静音补差（离线验证）

Fangtang 的 0–100 远端音量已经从临时“不支持”路径提升为正式能力。握手统一声明
`volumeControl:true`；收到 `hardware_config.extra.volume` 后，直连 I2S 播放链按百分比缩放 PCM，随后由
内部 RAM 栈上的专用持久化任务写入 `maclaw/output_vol`，避免在 PSRAM 栈的 Hub 长轮询任务中直接执行
NVS flash 写入。持久化成功后才 ACK 配置消息，失败保留 cursor 供 Hub 重试；设备启动时恢复该值。
0% 继续执行完整播放事务并输出零样本，属于静音成功，不再伪装成 `ESP_ERR_INVALID_STATE`。Bread Compact
沿用相同软件增益，EchoEar-2ST 沿用 ES8311 codec 增益；三板的原有输入和回复翻页行为没有改变。

本轮三板均以独立 SDK 配置重新完整构建并通过 App 分区检查：Fangtang 4G 为 3451296 bytes
（SHA-256 `5F9C93EF1C91B96C6D736988A18DA8D7D970693E55969632333C4B7613CEC192`），Bread Compact 为
3247120 bytes（SHA-256 `15D0FA6F2D928C6BC422BFC148037D8CA1D7A1D589A7EDCB7F7AC7D33571AA4A`），
EchoEar-2ST 为 3271408 bytes（SHA-256 `56BE1E56AFA3236098F7DA18455E1E5071FC5F31944A0B3216FA09CB58BC9AFE`）。
EchoEar 首次并行构建遇到一次无诊断信息的工具链/ccache 子进程失败，关闭 ccache 后从同一构建目录重试成功。
现场无人，按要求跳过 4G 和全部实机测试；未访问任何串口，未刷写 COM3、COM4 或 COM5。

### 待机熄屏、唤醒与持久化关联补差

Fangtang 与 Bread Compact 的紧凑屏端口现已对齐 EchoEar-2ST 的待机节能语义：进入
`idle/quiet` 待机面后开始 30 分钟计时，期间时钟、网络状态和宠物动画正常刷新；超时且
没有录音、回复、闹钟或其他前台页面时，关闭 LCD panel 与背光。熄屏后环境时间、Wi-Fi/4G
和服务状态仍只更新内存，不继续向 LCD 发送 DMA，也不会被后台刷新意外点亮。

熄屏后的第一次实体按键只恢复 panel/backlight 并重新绘制待机宠物，由共享业务层消费该次
物理输入，不会误启动语音；离线唤醒词则先恢复屏幕、随后继续原有语音采集。Fangtang 的
开机 1.8 秒双击 Wi-Fi/4G 切换窗口发生在正常待机计时之前，原有网络切换手势、处理中双击
取消及 6 秒回复自动翻页均保持不变。任何真实前台事务绘制也会先恢复显示，避免闹钟或远端
结果到达时仍处于黑屏。

音量 NVS worker 的完成通知同时由共享二值信号改为带 generation 的请求/回复队列。即使一次
flash 写入超过调用方超时，后续配置也只接受与自身请求编号匹配的完成结果，不会错误 ACK
前一笔迟到写入。该变化保持内部 RAM 栈写 NVS、持久化成功后才 ACK 的既有约束。

三板以各自独立 SDK 配置重新完整构建并通过 App 分区检查：Fangtang 4G 为 3452320 bytes
（SHA-256 `97D1880E820A9C502CEE8B52513A43EB89BF7B4C9B465B3C87DD88E5A7979365`），Bread Compact
为 3248240 bytes（SHA-256 `E0F7186F7B55BD2370399DFBDAF5396836EA9AB90315C274FB5C178232F0DF9B`），
EchoEar-2ST 为 3271600 bytes（SHA-256
`7510774B7624226A900237AC7353F6AA7633886CCA46492D0C3BA77C0705F096`）。本节为离线实现记录；
现场无人，本轮继续跳过 4G 实机测试，也不访问或刷写 COM3、COM4、COM5。

### 2026-08-06 闹钟前景恢复（离线实现）

闹钟显示现已统一经过共享 `app_ui` 协调器，不再由 Alarm Manager 直接绕过业务 UI 模型。闹钟响铃期间，消息、上传进度、文字回复、64×64 以内的 RGB565 图片回复、配网二维码、录音状态和待机宠物仍会更新为最新的待恢复场景，但不会覆盖响铃页面；闹钟结束时先仅释放板级 alarm guard，再重放最新场景，不再固定跳回 idle，也不会先闪一次待机页。图片像素和二维码 module matrix 均由协调器深拷贝，未保存调用方裸指针或只在二维码 callback 内有效的 handle。

该改造位于三板共享层，并为 EchoEar 与 Bread/Fangtang 两套 board port 都增加了相同的无闪屏释放语义，因此 Bread Compact、EchoEar-2ST 与 Fangtang 的既有前景流程保持一致。三套独立 SDK 配置均已离线完成编译/链接验证；现场无人，本轮按要求跳过 4G 与全部实机测试，未访问或刷写 COM3、COM4、COM5。实机仍需在人员回到设备旁后覆盖“回复/上传/配网二维码被闹钟打断及恢复”的组合场景。

### 2026-08-06 闹钟恢复保留回复阅读页码

共享前景恢复现进一步保存文字回复的零基页码。闹钟第一次覆盖文字回复时，`app_ui` 从 renderer 读取当前页；响铃期间输入由 Alarm 独占，不会误改被遮挡的回复页；解除后先重新发布相同回复内容，再由两套 board port 将阅读位置恢复到原页。Bread Compact 与 EchoEar-2ST 的手动分页、Fangtang 的六秒自动分页都使用同一页码快照语义；Fangtang 恢复后从完整的六秒间隔重新计时，避免刚恢复就立即跳页。图片回复仍为单页。

本节仅作离线实现和三 profile 构建验证。现场无人，继续跳过 4G 与全部实机测试，未访问或刷写 COM3、COM4、COM5。
