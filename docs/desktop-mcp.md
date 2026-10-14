# MaClaw 云桌面 MCP 服务(maclaw-desktop-mcp)

把一个用户的 MaClaw 云桌面(maclaw-gui:2)以 MCP 工具暴露给外部 agent(Claude Desktop / Claude Code / Cursor 等)。
源码:`cmd/maclaw-desktop-mcp`;动作解析与执行与 MaClawSrv 的 `desktop` 工具共用 `corelib/desktop/computeruse`。

## 部署位置的选择

独立小二进制,转发到 **Hub 的 desktop-services API**(`/api/v1/desktop-services/*`,生产推荐):
Hub 已负责多租户路由到正确的 desktopd;MCP 服务只做客户端鉴权、权限范围和协议转换,不需要改 Hub 配置,也不持有 desktopd token。
测试时也可直接指向一台 desktopd(`MACLAW_MCP_BACKEND=desktopd`)。

- 传输:Streamable HTTP(`POST /mcp`,返回 `application/json`;`GET` 返回 405,即不提供服务端推送流)和 stdio(本地单用户)。
- 协议版本:`2025-06-18`、`2025-03-26`、`2024-11-05`(按客户端请求协商)。
- 鉴权:`Authorization: Bearer <client token>`。服务端只保存 token 的 SHA-256(tokens 文件),每个 token 绑定一个 tenant/user 与范围:
  - `scope: "read"`:截图、无障碍树、窗口列表、剪贴板读取、屏幕信息、等待、读文件/下载;
  - `scope: "control"`:再加鼠标键盘、剪贴板写、窗口操作、启动程序、写文件;
  - `allow_bash` / `allow_install`:单独开启 `desktop_bash`、`desktop_file_upload` 与 `desktop_install`(默认关闭)。
  `tools/list` 只列出该 token 可用的工具。
- 人工接管:`MACLAW_MCP_TAKEOVER=viewer`(默认)时,每次控制类调用(以及 `desktop_computer` 的每一步之前)查询容器内 noVNC 连接数,有人正在看/操作时拒绝并提示稍后再试;`off` 关闭此检查。
- 密码框:MCP 无法读取网页焦点,所以当前焦点窗口是浏览器时拒绝 `type`/会输入字符的按键,网页请用 `desktop_browser_info` 返回的 CDP 端点;AT-SPI `set_text` 拒绝密码框。

## 工具

| 工具 | 范围 | 说明 |
|---|---|---|
| `desktop_screenshot` | read | `max_width/max_height`、`region`([x0,y0,x1,y1],上次全屏截图像素)、`format` png/jpeg/webp、`quality`、`cursor`、`marks`;返回图片 + `screen_width/height,image_width/height,scale,region,cursor` |
| `desktop_computer` | control | `actions` 数组:Anthropic computer-use / OpenAI CUA / MaClaw 步骤;按序执行,首个失败即停,逐步结果;有输入时附一张新截图。坐标为上次全屏截图像素,自动换算 |
| `desktop_app_probe` | read | AT-SPI 元素树(id/role/name/states/bounds,含截图坐标);`marks:true` 附编号截图 |
| `desktop_clipboard_get` / `desktop_clipboard_set` | read / control | UTF-8 文本(中文可用),或 PNG |
| `desktop_windows` / `desktop_window` | read / control | 列表;activate/maximize/minimize/restore/move/resize/move_resize/close |
| `desktop_screen_info`、`desktop_wait` | read | 分辨率与缩放;等屏幕变化或窗口出现/关闭 |
| `desktop_app_open` | control | 启动 GUI 程序(不是 shell) |
| `desktop_file_read` / `desktop_file_list` / `desktop_file_download` | read | 文本读、目录、任意文件 base64(embedded resource) |
| `desktop_file_write` / `desktop_file_upload` | control(upload 需 allow_bash) | 写文本 / 上传任意文件 |
| `desktop_bash` | control + allow_bash | 容器内执行一条命令 |
| `desktop_install` | control + allow_install | apt 安装 |
| `desktop_browser_info` | control | 打开/复用桌面,返回 display 与浏览器 CDP 地址 |

## 运行

```sh
go build -trimpath -ldflags='-s -w' -o maclaw-desktop-mcp ./cmd/maclaw-desktop-mcp
# 客户端 token:自己生成,只把哈希写进 tokens 文件
TOKEN=$(openssl rand -hex 24); printf '%s\n' "$TOKEN" | ./maclaw-desktop-mcp -hash-token
cat > /etc/maclaw/mcp-tokens.json <<'JSON'
[{"name":"alice-cursor","token_sha256":"<上一步输出>","tenant_id":"tenant_default","user_id":"<user_id>","scope":"control","allow_bash":false,"allow_install":false}]
JSON
chmod 600 /etc/maclaw/mcp-tokens.json
# backend token 只从环境/systemd EnvironmentFile 传入,不要写进仓库
MACLAW_MCP_BACKEND=hub MACLAW_MCP_BACKEND_URL=https://hub.example.com \
MACLAW_MCP_BACKEND_TOKEN="$MACLAW_DESKTOP_API_TOKEN" MACLAW_MCP_TOKENS_FILE=/etc/maclaw/mcp-tokens.json \
./maclaw-desktop-mcp -http 127.0.0.1:8790
```

对外请放在 HTTPS 反向代理后(`location /mcp { proxy_pass http://127.0.0.1:8790; proxy_read_timeout 300s; }`)。

环境变量:`MACLAW_MCP_BACKEND`(hub|desktopd)、`MACLAW_MCP_BACKEND_URL`、`MACLAW_MCP_BACKEND_TOKEN`、`MACLAW_MCP_TOKENS_FILE`、
`MACLAW_MCP_TAKEOVER`(viewer|off)、stdio 用 `MACLAW_MCP_TENANT`、`MACLAW_MCP_USER`、`MACLAW_MCP_SCOPE`、`MACLAW_MCP_ALLOW_BASH`、`MACLAW_MCP_ALLOW_INSTALL`;
desktopd 后端打开会话时用 `MACLAW_MCP_IMAGE`、`MACLAW_MCP_MEMORY`、`MACLAW_MCP_CPUS`、`MACLAW_MCP_SHM_SIZE`。

## 客户端配置示例

Claude Code:

```sh
claude mcp add --transport http maclaw-desktop https://mcp.example.com/mcp --header "Authorization: Bearer $MACLAW_MCP_TOKEN"
```

Cursor(`~/.cursor/mcp.json` 或项目 `.cursor/mcp.json`):

```json
{"mcpServers": {"maclaw-desktop": {"url": "https://mcp.example.com/mcp", "headers": {"Authorization": "Bearer ${env:MACLAW_MCP_TOKEN}"}}}}
```

Claude Desktop(stdio,本地二进制直连 Hub;`claude_desktop_config.json`):

```json
{"mcpServers": {"maclaw-desktop": {"command": "/usr/local/bin/maclaw-desktop-mcp", "args": ["-stdio"],
  "env": {"MACLAW_MCP_BACKEND": "hub", "MACLAW_MCP_BACKEND_URL": "https://hub.example.com",
          "MACLAW_MCP_BACKEND_TOKEN": "<desktop API token>", "MACLAW_MCP_USER": "<user_id>", "MACLAW_MCP_SCOPE": "control"}}}}
```

stdio 模式持有 backend token,只适合运维人员自己的机器;对外分发请用 HTTP 模式 + 个人 token。

## computer 动作对照

| Anthropic | OpenAI | xdotool |
|---|---|---|
| left_click / right_click / middle_click(`text`=修饰键) | click{button} | `mousemove --sync x y [keydown m] click 1/3/2 [keyup m]` |
| double_click / triple_click | double_click | `click --repeat 2/3 --delay 80 1` |
| mouse_move | move | `mousemove --sync x y` |
| left_click_drag(start_coordinate→coordinate) | drag{path} | `mousedown 1 … mousemove … mouseup 1` |
| left_mouse_down / up | | `mousedown 1` / `mouseup 1` |
| scroll(scroll_direction, scroll_amount, `text`=修饰键) | scroll{scroll_x,scroll_y}(100px≈1 格) | `click --repeat N 4/5/6/7` |
| key(`text`,如 `ctrl+s`、`Return`;`repeat`) | keypress{keys} | `key [--repeat N] chord` |
| hold_key(text, duration) | | `keydown k sleep d keyup k`(不允许会输入字符的键) |
| type | type | `type --delay 20`(换行按 Return) |
| cursor_position | | `getmouselocation`,换算成截图坐标 |
| wait(duration ≤30s) | wait | `sleep` |
| screenshot / zoom(region) | screenshot | 截图(zoom 不改变坐标系) |
