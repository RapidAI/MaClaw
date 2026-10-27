# buddy 串口协议 v2

- 状态：草案（取代 v0）
- 变更依据：`DESIGN_REVIEW.md` P0-2（日志污染）、P1-2（计数桶）、P1-3（降级语义）
- 链路：ESP32-S3 USB-Serial/JTAG（PC 枚举为 COM 口）
- 版本：`proto: 2`

---

## 1. 链路与分帧

| 项 | 规定 |
| --- | --- |
| 帧格式 | UTF-8 JSON 文本行，`\n`（LF）分帧，可选容忍 `\r\n` |
| 单行上限 | **1024 字节**（含 `\n`）；超出即丢弃该行并计数 |
| 控制台隔离 | 设备 `CONFIG_ESP_CONSOLE_NONE=y`，**USB 口只承载协议帧**（见 §5） |
| 波特率 | 原生 USB-Serial/JTAG **无意义**；若用 CP210x/CH340 桥则必须正确设置（默认 115200） |
| DTR/RTS | host 打开端口时显式置 **false**（避免复位开发板） |
| 方向 | host 主动推送；设备只回执与上报 |

**容错规则**：接收方遇到不以 `{` 开头的行 → 丢弃并 `nonjson_lines++`；
非法 JSON → 丢弃并 `bad_json++`。二者通过 `log` 上报，**不影响状态机**。

## 2. 消息目录

### Host → Device

| type | 用途 |
| --- | --- |
| `hello` | 连接建立/重建，携带协议版本与全量重发标志 |
| `state` | 任务状态（核心） |
| `ambient` | 天气 |
| `clock` | 本地日期/星期（设备无 RTC/NTP，必须由 host 下发） |
| `celebrate` | 任务完成庆祝页：任务名 + 5 秒烟花动画 |
| `sound` | 通知音效配置：总开关 + 运行/完成/待机各自开关与选择 |
| `pet` | 宠物情绪（`idle/happy/busy/sleep`） |
| `heartbeat` | 保活（3s） |
| `asset_chunk` | 预留：宠物资产分块（M4） |

### Device → Host

| type | 用途 |
| --- | --- |
| `hello` | 自报 `proto/fw/board/disp` |
| `state_ack` | 已应用 + 当前页面 + 缺字计数 |
| `heartbeat` | `uptime_s` / `heap_min` / `page` / 累计缺字数 `missing_glyphs` |
| `log` | 设备日志（替代 printf） |

## 3. 任务状态 `state`（核心）

```json
{"type":"state","proto":2,"ts":1769900000,"seq":42,"stale":false,
 "counts":{"total":7,"in_progress":2,"pending":1,"paused":0,"done":4,"failed":0},
 "current":[
   {"id":"task-a1","title":"整理会议纪要并导出 Word","progress":0.62},
   {"id":"task-b2","title":"文档导入：季度报表.xlsx","progress":0.05}
 ]}
```

### 3.1 计数语义（与侧栏严格对齐）

计算发生在 **GUI 进程内**（见开发计划 §5.1），设备**不自行推断**。

| 字段 | 判定（`taskStatusBucketFor` 语义） |
| --- | --- |
| `in_progress` | `bucket=='running'` 或 `isLiveRunningTaskRow()` |
| `pending` | `bucket=='pending'`（待我确认） |
| `paused` | `bucket=='paused'` |
| `done` | `bucket=='completed'` |
| `failed` | `bucket=='other'`（失败/取消/阻塞；设备标签为"异常"） |
| `total` | 上述五者之和 |

**不变式**：`total == in_progress + pending + paused + done + failed`。
设备校验失败 → 显示"数据异常"页 + `log{level:"error"}`，**不渲染错误计数**。

### 3.2 字段约束

- `seq`：单调递增，设备用于丢弃乱序/重复帧。
- `stale`：`true` 表示数据超过 60s 未刷新（host 卡住/系统休眠）。
- `current`：≤5 条，按侧栏顺序；`title` UTF-8 ≤64 字节（设备端按字节截断，
  不切断多字节字符）。
- `progress`：`0..1`；未知传 `-1`（设备不画进度条）。

### 3.3 设备端渲染

- `in_progress > 0` → 任务页；否则 → 待机页（见开发计划 §4）。
- `current` >1 时 3s 轮换；触摸翻页。
- 标题默认截断 + 省略号；长按才滚动。

## 4. 天气 `ambient`

字段对齐 `guiapp/device_ambient_weather.go` 的 `deviceAmbientWeather`：

```json
{"type":"ambient","proto":2,"ts":1769900000,"stale":false,
 "weather":{"summary":"晴","temperatureC":23,"location":"北京"}}
```

- 设备写入 NVS（复用 `weather_cache_service` 语义）；断连后显示并标陈旧。
- `stale:true` 由 host 在数据超过 `deviceAmbientExpiresAfter`（2h）时设置。

## 4.1 时钟 `clock`

设备没有 RTC 与 NTP，待机页的日期完全由 host 提供。Host 在连接时、每分钟、
以及日期跨天时下发：

```json
{"type":"clock","proto":2,"clock":{"date":"10月27日","weekday":"周二"}}
```

- 字符串由 host 按本地时区格式化，设备不做日历运算。
- Host 每秒/每分钟重发是幂等的；设备只覆盖显示值。

## 4.2 任务完成庆祝 `celebrate`

Host 在快照对比中检测到"某个进行中任务消失且 done 计数增加"时，先发
`state`（更新计数），随后发：

```json
{"type":"celebrate","proto":2,"celebrate":{"title":"整理会议纪要","seconds":5}}
```

- `title`：刚完成的任务名（UTF-8 ≤64 字节，设备端与 `state.current[].title`
  同样截断）。
- `seconds`：庆祝页时长，默认 5，设备端夹在 2–10 秒。
- 设备行为：立即切到 celebrate 页；前 2.2 秒显示"✓ 任务完成 + 任务名"，
  任务名随后消失；烟花动画持续到 `seconds` 结束，然后自动回到任务页
  （仍有进行中任务）或待机页（无进行中任务）。
- Host 判定是保守的：删除任务（`total` 减少）不庆祝；无法确定任务名
  （例如完成的任务不在上一次 `current` 列表中）不庆祝；同一完成只发一次。

## 4.3 通知音效 `sound`

Host 在连接时和每次修改后下发完整配置：

```json
{"type":"sound","proto":2,"sound":{
  "master":true,
  "running":{"enabled":true,"effect":0},
  "complete":{"enabled":true,"effect":0},
  "standby":{"enabled":true,"effect":0}}}
```

- `master=false` 时设备完全静音，忽略三个事件各自的开关。
- `effect`：`0..4`，对应固件内置的 5 段 16kHz 立体声 PCM 音效。
- 触发时机（页面切换，每个事件每次切换只响一次）：
  - 运行中：待机页 → 任务页；
  - 完成：进入庆祝页（任务完成）；
  - 待机：任务页 → 待机页（从庆祝页返回待机不重复响）。
- 设备持久化到 NVS；Host 重连后再次下发覆盖。
- 默认值：总开关开、三个事件全开、`effect=0`。

## 5. 控制台隔离（P0-2，强制）

`iot-agentos/sdkconfig.defaults:30-31` 当前启用 USB 控制台，日志与协议会交织。
buddy 固件必须：

```ini
CONFIG_ESP_CONSOLE_NONE=y
```

或保留 UART0 调试（`CONFIG_ESP_CONSOLE_UART_DEFAULT=y` + `CONFIG_ESP_CONSOLE_SECONDARY_NONE=y`）。

**验收**：刷入后 USB 口抓包必须 100% 为 `{` 开头的 JSON 行。

## 6. 时序与容错

```text
host                                   device
 │ ── hello{proto:2,full_resend:true} ─▶ │
 │ ◀── hello{proto:2,fw,board,disp} ──── │
 │ ── state{seq:1,...} ─────────────────▶ │ 任务页
 │ ◀── state_ack{applied:1,page:"tasks"} │
 │ ── ambient ──────────────────────────▶ │ 写 NVS
 │ ── heartbeat (3s) ───────────────────▶ │
 │        （10s 无任何 host 消息）         │ 判离线 → 待机页 + 离线角标（保留最后计数）
 │ ── hello{full_resend:true} ──────────▶ │ 重连，全量重建
```

- **心跳 3s**；设备 **10s** 超时判离线。`state`/`ambient` 也算保活。
- **重连必须全量重发**（`hello.full_resend=true`），设备不做会话记忆。
- 离线时**保留最后已知计数**并打"离线"角标，**不清零**（避免误以为任务消失）。
- `seq` 回退或重复 → 设备忽略该帧，`log{level:"warn"}`。
- 设备缓冲满 → 丢弃最老的未处理帧（新状态覆盖旧状态是期望行为）。

## 7. 威胁模型

**物理接入即信任**：任何能打开 COM 口的本机进程都可读写。不做鉴权，理由：
- 与现有工具一致（`ClawMateMaker` 刷机、`scripts/wake-esp32-devices.ps1` 均无鉴权）；
- 设备不持有凭据、不连接网络，被写入的最坏结果是显示错误内容；
- 若日后需要，可在 `hello` 增加 token 字段（协议预留，首版不实现）。

**不写入敏感数据**：buddy 不下发 API Key、Token、文件内容；`state.current[].title`
来自任务标题，属用户本机可见信息。

## 8. 单一来源与双端一致性（P2-1）

协议常量在 C（固件）与 Go（host）两侧各写一份必然漂移。规定：

1. `buddy/protocol/spec.json` 为**唯一来源**（消息类型、字段、上限）。
2. 由脚本生成 C 头与 Go 结构体常量。
3. 双端各有一组**协议回放测试**：用同一批录制帧跑解析，断言结果一致。

## 9. M0 spike 待验证项（结果回填本节）

- [ ] 打开/关闭端口是否导致复位（看启动日志时间戳）
- [ ] 显式 DTR=false / RTS=false 下的行为
- [ ] host 持有端口时 esptool 能否烧录（预期不能 → 定义释放流程）
- [ ] `CONFIG_ESP_CONSOLE_NONE` 后 USB 口是否纯净
- [ ] 1024B 行吞吐与丢包
- [ ] 快速插拔 10 次：COM 号是否漂移、重连耗时
- [ ] 刷 buddy 固件后 NVS/storage 是否保持（分区表未变的前提下应保持）
