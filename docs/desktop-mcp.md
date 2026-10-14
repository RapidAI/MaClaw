# MaClaw 云桌面 MCP(Hub `/mcp`)

把用户的 MaClaw 云桌面(maclaw-gui:2)以 MCP 工具暴露给外部 agent(Claude Desktop / Claude Code / Cursor 等)。
**MCP 内置在 Hub 里**:地址就是 `https://<hub 域名>/mcp`,无需单独部署服务、tokens 文件或额外 nginx location。

- 源码:协议与工具 `corelib/desktop/desktopmcp`;Hub 挂载与鉴权 `hub/internal/httpapi/desktop_mcp.go`(直接调用 Hub 内部的
  desktop pool,不走 HTTP 回环);动作解析与执行与 MaClawSrv 的 `desktop` 工具共用 `corelib/desktop/computeruse`。
- 传输:Streamable HTTP(`POST /mcp`,返回 `application/json`;`GET`/`DELETE` 返回 405,即不提供服务端推送流,
  所以反向代理不需要特殊的 SSE 配置;Hub 已带 `X-Accel-Buffering: no`)。
- 协议版本:`2025-06-18`、`2025-03-26`、`2024-11-05`(按客户端请求协商)。

## 鉴权(`Authorization: Bearer <token>`)

| token | 能访问的桌面 | 范围 |
|---|---|---|
| 桌面 MCP token(`mcpd_...`,推荐) | 绑定的那个用户 | `read` 或 `control`;`allow_bash` / `allow_install` 只能由管理员开启 |
| 用户自己的 Hub 登录 token | 自己的桌面 | `control`(LLM endpoint API key 不可用) |
| Hub 管理员 token | 租户管理员:本租户任意用户;全局管理员:任意租户 | `control` + bash/install,每次调用用参数 `user_id`(全局管理员还可 `tenant_id`)指定桌面 |

服务间的 desktop API token(`MACLAW_DESKTOP_API_TOKEN`)**不能**直接访问 `/mcp`。

- `read`:截图、无障碍树、窗口列表、剪贴板读取、屏幕信息、等待、读文件/下载;
- `control`:再加鼠标键盘、剪贴板写、窗口操作、启动程序、写文件;
- `tools/list` 只列出该 token 可用的工具;普通 token 传别人的 `user_id` 会被拒绝。

桌面 MCP token 只以 SHA-256 存在 Hub system settings(键 `desktop_mcp_tokens`),明文只在创建时返回一次。管理接口:

```sh
# 用户自己(Hub 登录 token):列出 / 创建 / 删除
curl -H "Authorization: Bearer $HUB_USER_TOKEN" https://<hub>/api/v1/desktop-mcp/tokens
curl -X POST -H "Authorization: Bearer $HUB_USER_TOKEN" -d '{"name":"cursor","scope":"control"}' https://<hub>/api/v1/desktop-mcp/tokens
curl -X DELETE -H "Authorization: Bearer $HUB_USER_TOKEN" "https://<hub>/api/v1/desktop-mcp/tokens?id=<id>"
# 租户管理员(或服务器上的 desktop API token,用于开通):给某用户发 token
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"user_id":"<user_id>","scope":"read","name":"ops"}' https://<hub>/api/admin/desktop-mcp/tokens
```

- 人工接管:每次控制类调用(以及 `desktop_computer` 的每一步之前)查询容器内 noVNC 连接数,有人正在看/操作时拒绝并提示稍后再试。
- 密码框:MCP 无法读取网页焦点,所以当前焦点窗口是浏览器时拒绝 `type`/会输入字符的按键,网页请用 `desktop_browser_info` 返回的 CDP 端点;AT-SPI `set_text` 拒绝密码框。

## nginx

Hub 现有的 HTTPS server 块只要把 `/` 反代到 Hub,`/mcp` 就能用。截图、等待类调用可能较慢,如全局 `proxy_read_timeout` 很短,可加:

```nginx
location = /mcp { proxy_pass http://127.0.0.1:9399; proxy_http_version 1.1; proxy_buffering off; proxy_read_timeout 300s; client_max_body_size 16m; }
```

## 客户端配置示例(token 用占位符)

Cursor(`~/.cursor/mcp.json` 或项目 `.cursor/mcp.json`):

```json
{"mcpServers": {"maclaw-desktop": {"url": "https://hubs.maclaw.top/mcp", "headers": {"Authorization": "Bearer ${env:MACLAW_MCP_TOKEN}"}}}}
```

Claude Code:

```sh
claude mcp add --transport http maclaw-desktop https://hubs.maclaw.top/mcp --header "Authorization: Bearer $MACLAW_MCP_TOKEN"
```

Claude Desktop(`claude_desktop_config.json`;通过 `mcp-remote` 桥接带 header 的远程 HTTP):

```json
{"mcpServers": {"maclaw-desktop": {"command": "npx", "args": ["-y", "mcp-remote", "https://hubs.maclaw.top/mcp", "--header", "Authorization:${MACLAW_MCP_TOKEN}"],
  "env": {"MACLAW_MCP_TOKEN": "Bearer mcpd_<your token>"}}}}
```

## 可选:本地 stdio 二进制

`cmd/maclaw-desktop-mcp` 只保留 stdio 模式(运维/本地调试),它持有服务端 desktop API token,只能在允许存放该 token 的机器上运行;
对外一律用 Hub 的 `/mcp`。环境变量:`MACLAW_MCP_BACKEND`(hub|desktopd)、`MACLAW_MCP_BACKEND_URL`、`MACLAW_MCP_BACKEND_TOKEN`、
`MACLAW_MCP_TENANT`、`MACLAW_MCP_USER`、`MACLAW_MCP_SCOPE`、`MACLAW_MCP_ALLOW_BASH`、`MACLAW_MCP_ALLOW_INSTALL`、`MACLAW_MCP_TAKEOVER`;
desktopd 后端打开会话时用 `MACLAW_MCP_IMAGE`、`MACLAW_MCP_MEMORY`、`MACLAW_MCP_CPUS`、`MACLAW_MCP_SHM_SIZE`。

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
