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
| 网络 | Wi-Fi / ML307 Cat.1 4G 双网络 |
| ML307 UART | UART1；ESP32 TX GPIO12 -> ML307 RX，ESP32 RX GPIO11 <- ML307 TX；921600 baud |
| 模组控制 | GPIO21 输出高；GPIO45 下拉输出低，均在调制解调器初始化前设置 |
| 启动手势 | 在开机选择窗口内双击 GPIO0 切换并持久化 Wi-Fi / 4G；窗口结束后按键恢复普通手势 |

## 当前软件约定

- Fangtang 专属的 GPIO0 启动双击、PPP 与调制解调器配置均受
  `CONFIG_MACLAW_BOARD_FANGTANG_4G` 保护。
- EchoEar-2ST 保持触摸屏交互；Bread Compact 保持音量键翻页。
- 4G 使用 ML307 的 UART1 配置与通用 `esp_modem` PPP 路径。默认网络选择为
  4G；它会先读取本工程 NVS 的 `maclaw/net_transport`，若不存在则兼容读取
  原始固件的 `network/type`（`1` 为 ML307、`0` 为 Wi-Fi），再持久化为本工程值。

## 刷写前必须确认

下列信息尚未从资料入口或实机可靠确认，未确认前不得把其他设备的数值复制到
本板：

1. SIM 所需 APN；
2. 显示屏、麦克风、扬声器及背光的具体型号和接线。

已从原始 `xingzhi-cube-0.85tft-ml307` 板级源码确认该单键版本的显示与音频
接线：128×128 NV3023（SPI3：MOSI GPIO10、SCLK GPIO9、DC GPIO8、CS GPIO14、
RST GPIO18、背光 GPIO13），以及直连 I2S 麦克风/扬声器（MIC WS GPIO4、SCK
GPIO5、DIN GPIO6；SPK DOUT GPIO7、BCLK GPIO15、LRCK GPIO16；输出采样率
24 kHz）。这些引脚与现有 EchoEar-2ST 的显示/音频实现不同，因此在独立
Fangtang board port 落地并实机验证前，不能把 EchoEar 的板级固件刷到它。

4G 路径已按原始 ML307 板级参数实现：上电后先设置 GPIO21/GPIO45，再以
UART1 的 921600 baud 进入 AT 命令模式并同步，成功后启动 PPP；若 ML307 未
应答会显示可诊断错误，而不会误把 4G 选择当成 Wi-Fi 失败。

## 构建配置

使用独立的 Fangtang SDK 配置，避免把已生成的 EchoEar/Bread `sdkconfig` 带入：

```powershell
cmd.exe /d /s /c "call C:\esp\v6.0.2\esp-idf\export.bat >nul && idf.py -B build-fangtang -D SDKCONFIG=sdkconfig.fangtang-4g reconfigure"
```

Fangtang 默认值放在 `sdkconfig.defaults.fangtang-4g`；旧设备默认值不启用 PPP。
