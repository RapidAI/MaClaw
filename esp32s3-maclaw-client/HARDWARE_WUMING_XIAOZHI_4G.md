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
- Fangtang 的板级音量调节仍返回“不支持”，直连扬声器以固定安全音量播放。
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

### 咖啡方糖与 ML307 取消补充回归

开机及状态页的方糖素材进一步调整为暖白咖啡方糖：三个面保持奶油/象牙白，不使用
黑色描边；增加在 240 像素屏和 RGB565 量化后仍可识别的压制砂糖颗粒、微孔、晶体
高光、轻微不规则边缘和暖色柔和接触阴影。确定性生成脚本仍为
`tools/generate_fangtang_sugar.py`，预览为 `main/fangtang_sugar_preview.png`，运行时
继续使用 RGB565+A8 与页面底色合成。

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
