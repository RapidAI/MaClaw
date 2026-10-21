# 桌面 H.264 观看与接管（maclaw-h264/1）

人类查看/接管桌面不再依赖 VNC：desktopd 容器内 supervisor 的 noVNC gate（6080）在
默认（未设置 `MACLAW_DESKTOP_H264`，或设为 `off`/`0`/`false`/`no` 以外的值）提供 H.264 播放器 `h264.html` 和同路径的 `/h264` WebSocket，
视频下行与键鼠/剪贴板上行走**同一条 WebSocket**，因此 Hub handoff 代理、desktopd 隧道与
nginx（maclaw.top）无需新增端口或路由。noVNC 保留为备用。

设计思路参考 trycua/cua 公开的协议文档（`libs/cua/proto/MEDIA.md`、`PRESENCE.md`：
长度前缀帧 + JSON 描述符、连接即关键帧、只保留最新帧、输入租约）。cua 的实现代码
（`cua-spacesd`、`cua-spacesd-html5`、`cua-media-*` 等）为 FSL-1.1-MIT，**未使用其任何代码**；
本实现与线格式均为 MaClaw 自有。

## 开关与参数（容器环境变量）

| 变量 | 默认 | 说明 |
|---|---|---|
| `MACLAW_DESKTOP_H264` | 开 | 默认 gate 提供 `h264.html` / `/h264`；`off`/`0`/`false`/`no` 关闭。关闭时 `h264.html` 302 到 `?vnc=` 指定的 noVNC 页 |
| `MACLAW_DESKTOP_H264_FPS` | 15 | 5..30 |

每个容器最多 4 路；镜像没有 ffmpeg 时 `/h264` 返回 503（播放器重试 3 次后回退 noVNC）。

## 链路

```
GUI iframe ─ Hub /api/v1/desktop-handoff/<t>/h264.html?view_only=0|1&vnc=<noVNC 页>
           └ WS  /api/v1/desktop-handoff/<t>/h264     (Hub 附加 gate Bearer)
                → gate 6080 → 每个观看者一个 ffmpeg(x11grab → libx264 → 分片 MP4)
                → supervisor 解析 fMP4 → Annex B 访问单元 → WebSocket
浏览器：WebCodecs VideoDecoder(optimizeForLatency) → canvas
        无 WebCodecs → `/h264?mse=1`（原始 fMP4，MSE <video>）
        WebCodecs 与 MSE 都没有 / 连不上 → 跳转 `vnc` 参数里的 noVNC 页
```

Hub 兜底：上游对 `h264.html` 返回 404（旧 supervisor）时，Hub handoff 代理把它改成 302 到
`?vnc=`（只接受同目录 `vnc.html?...`，不是开放重定向）。

## 线格式

服务端 → 客户端
- 二进制（默认 Annex B 模式）：`u32 BE 头长度` `u32 BE 负载长度` `JSON 头` `负载`。
  头：`{seq, key, ts(µs, 单调时钟), codec("avc1.PPCCLL"，仅关键帧), w, h}`；负载为一个完整
  H.264 访问单元，IDR 前总带 SPS/PPS。帧边界来自 fMP4 的 moof/mdat 盒子长度，帧完成即发送。
- 二进制（`?mse=1`）：编码器原始 fMP4 字节。
- 文本：
  - `hello {proto:"maclaw-h264/1", width, height, fps, mode, input, clipboard}`
  - `lease {holder: "you"|"human"|"bot"|"", input, takeover}`（变化时发送，0.5 s 检查）
  - `clipboard {text}`、`input_error {code: sequence_gap(带 expected)|view_only|lease_busy|inject_failed|clipboard_unavailable}`

客户端 → 服务端（文本）
- `input {seq, events:[...]}`：批次序号必须连续（第一批任意起点）。事件：
  `{t:"move",x,y}`、`{t:"down"|"up",b:1|2|3|8|9,x,y}`、`{t:"wheel",dx,dy,x,y}`（步数）、
  `{t:"key",code:<KeyboardEvent.code>,down}`、`{t:"release"}`。x/y 为 0..1 归一化坐标。
- `frame_ack {seq}`、`request_keyframe`、`clipboard_set {text}`、`clipboard_get`。

## 低延迟与背压

- 每个观看者独立 ffmpeg → 首帧必为 IDR（连接即关键帧）。
- `libx264 ultrafast zerolatency baseline`，`-g fps*2`，`mpdecimate`（含 `max=fps*2`）+ VFR：
  画面不变几乎不发数据，但至多 2 s 出一个小帧用于重同步。这是损伤驱动的近似，未写 XDamage 原生代码。
- 服务端队列 ≥3 帧或未确认帧 >8（收到过 `frame_ack` 后启用）时丢弃全部 P 帧，直到下一个 IDR；
  1 s 内没有 IDR 则重启编码器强制 IDR。`request_keyframe` 立即重启编码器（限频 1 s）。
- 浏览器端解码队列 >4 时丢 P 帧并请求关键帧；解码错误时重建解码器。

## 输入、租约、剪贴板

- 注入：每个显示一个持久 XTEST 连接，supervisor 直接用 X11 协议（setup、QueryExtension、
  GetKeyboardMapping、XTestFakeInput）实现，不依赖 python-xlib；Xvfb 以 `-ac` 启动无需授权。
  键盘按物理键 `KeyboardEvent.code` → keysym → keycode；中文输入由桌面里的 fcitx 处理。
  macOS 的 Cmd 映射为 Ctrl。
- 权限：owner 自己的屏幕可输入；bot 的屏幕仅在 takeover（`request_takeover`/GUI 人类接管）
  期间可输入，其他时候只读（服务端强制，`view_only=0` 只是客户端意愿）。
- 租约：同一屏幕同一时刻只有一个观看者能输入；最后一次输入后 5 s 失效。租约转移或失效、
  takeover 结束、断开连接时释放该观看者按下的所有键和鼠标键；浏览器失焦时客户端发 `release`。
- 人与 bot：人的输入写 `human_input`；`ctl bot_state` 返回 `lease: "human"|"bot"` 和
  `lease_ttl`，bot 准备动作（不需让出）时写 `bot_input`。MaClawSrv `desktopBotYield` 在
  `yield` 或 `lease=="human"` 时让出。播放器角标显示「你在控制 / Bot 正在操作 / 仅观看」，
  并向父窗口 `postMessage({type:"maclaw-desktop-lease"})`。
- 剪贴板：浏览器粘贴（Ctrl/Cmd+V）先 `clipboard_set`（xclip）再发送 V 键；
  Ctrl+C/X 后 250 ms 自动 `clipboard_get` 并写入本机剪贴板（iframe 需 `allow="clipboard-write"`，
  GUI 已加）。

## 未做 / 限制

- 每个观看者一个编码器（未做同屏共享编码器）；无 GPU 时 CPU 成本按 libx264 ultrafast 估算，未实测。
- 无音频、无远端光标/presence 图层（光标由 ffmpeg 画进视频）。
- 只读观看者（非 takeover）不显示他人的指针。
- 浏览器：WebCodecs 需 Chrome/Edge 94+、Safari 16.4+/17、Firefox 130+；更旧的走 MSE 或 noVNC。
