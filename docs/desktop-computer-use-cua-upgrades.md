# 桌面 Computer Use：参考 trycua/cua 的四项改进

对照对象：[trycua/cua](https://github.com/trycua/cua)（仓库根目录 MIT；`libs/cua-spacesd` 及
`cua-media-*`、`cua-spacesd-html5`、`cua-viewer` 等媒体/查看器 crate 为 FSL-1.1-MIT）。
只借鉴其公开协议文档的思路，未复制其代码。下面四项都是 MaClaw 自己的实现，只借鉴思路。

## 1. 轨迹记录 + 运行预算 + 截图保留（MaClawSrv / corelib）

- **轨迹**：每次 `desktop` 工具调用写一步到容器内
  `~/素材/<素材文件夹>/runs/<run>/step-NNN.json`（参数已脱敏、结果文本、耗时、错误），
  模型可见的截图另存为 `step-NNN-K.<ext>`。素材文件夹名由 Hub 用
  `desktop.MaterialsFolder` 计算，经实例 metadata `hub_materials_folder` 传给 MaClawSrv；
  缺失时退回 bot id / `default`。写入走后台队列（64 深），满则丢弃，不阻塞工具调用。
  `MACLAW_DESKTOP_TRAJECTORY=off` 关闭。
- **预算**（按 tenant+user+run，6 小时过期）：
  `MACLAW_DESKTOP_RUN_MAX_ACTIONS`（默认 200）、
  `MACLAW_DESKTOP_RUN_MAX_IMAGE_TOKENS`（默认 2,000,000，按 w·h/750+85 估算）。
  超出后拒绝新动作，但 `request_takeover` 和 `deliver` 永远放行，好让 bot 求助或交付。
  单批步数上限改为 `MACLAW_DESKTOP_MAX_STEPS`（默认 16，1..64）。
- **截图保留**：共享 agent loop 默认保留最近 3 张 computer-use 截图（原来是 1），
  `MACLAW_COMPUTER_USE_KEEP_IMAGES`（1..10）可调。相当于 cua 的 `only_n_most_recent_images`。

## 2. 规划 / 定位分离（可选定位模型）

- `corelib/grounding`：把「用文字描述的目标」交给视觉定位模型，解析多种输出
  （JSON x/y、ref、bbox/bbox_2d、UI-TARS `<|box_start|>`/`start_box=`、`<point>`、
  `[x1,y1,x2,y2]`、`(x,y)`），0..1000 网格（UI-TARS/Qwen2-VL/CogAgent）自动换算到图像像素并校验越界。
- **guiapp**：模型分配里在「电脑操作」后新增「定位模型」行（`profiles.grounder`）。
  勾选「不使用」（默认）即为原来的单模型行为；只有固定了模型才生效，从不跟随 Hub。
  `computer_click` 新增可选 `target`：没有 `ref`/`x,y` 时，用模型刚看到的那张截图定位，
  得到的点与模型自己的 x,y 同一坐标系，再走 `MapVisionClick`/`MapCaptureClick`。
- **MaClawSrv**：批量步骤里第一步的点击若只有 `target`，先截一张新图交给
  `MACLAW_DESKTOP_GROUNDER_{URL,MODEL,KEY,PROTOCOL}` 配置的模型，填入 x,y 再执行；
  只允许第一步（之后屏幕已变）。
- **回退**：未配置或定位失败时，工具返回一句话让规划模型自己给 x,y 或 ref，即原单模型路径。

## 3. desktopd `/mcp`

desktopd 在 `/mcp` 提供 Streamable HTTP MCP（复用 `corelib/desktop/desktopmcp`），
用每个屏幕的 gate token 鉴权并选屏（常量时间比较），身份为该屏幕的 owner 或 bot 桌面用户；
允许 bash，不允许安装。`DESKTOPD_MCP=off` 关闭（404），`DESKTOPD_MCP_TAKEOVER=off`
关闭接管工具。说明见 `docs/desktop-mcp.md`。

## 4. H.264 观看流（默认开启）

- 除非容器环境变量 `MACLAW_DESKTOP_H264=off`（或 `0`/`false`/`no`），supervisor 的 noVNC gate（6080）在路径最后一段为
  `/h264.html`（MSE 播放器）或 `/h264`（WebSocket）时接管连接；其他路径照旧转给 websockify。
  Hub handoff 代理与 desktopd 隧道带的前缀不影响匹配。
- 编码：`ffmpeg -f x11grab … -vf mpdecimate -fps_mode vfr -c:v libx264 -preset ultrafast
  -tune zerolatency -profile:v baseline`，输出逐帧分片 MP4。`mpdecimate` 丢掉未变化的帧，
  静止画面几乎不发数据——这是「按损伤驱动」的近似，没有新增 XDamage 原生代码。
- 鉴权/选屏与 noVNC 相同（gate token；bot token 只能看自己的屏）。只读，输入仍走 noVNC。
  每容器最多 4 路；镜像无 ffmpeg 时返回 503。`Dockerfile.v2` 已加入 `ffmpeg` 包。
- 后续已完成：WebCodecs 播放、同一 WebSocket 上的输入/剪贴板/租约与 GUI 接入，见
  [desktop-h264-takeover.md](desktop-h264-takeover.md)。
  延迟与带宽测量。noVNC 保持默认。

## 生效条件

| 项 | 需要 |
|---|---|
| 1 | MaClawSrv + Hub 部署（Hub 下发素材文件夹 metadata）；截图保留同时随 guiapp 发版生效 |
| 2 | guiapp 发版（UI 与 `computer_click target`）；MaClawSrv 部署并设置 `MACLAW_DESKTOP_GROUNDER_*` |
| 3 | desktopd 部署 |
| 4 | 重建 `maclaw-gui:2` 镜像（H.264 默认开启；需要关闭时容器设置 `MACLAW_DESKTOP_H264=off`） |
