# MaClaw 云桌面 MCP(Hub `/mcp`)

把用户的 MaClaw 云桌面(maclaw-gui:2)以 MCP 工具暴露给外部 agent(Claude Desktop / Claude Code / Cursor 等)。
**MCP 内置在 Hub 里**:地址就是 `https://hub.mypapers.top/mcp 或 https://hub.maclaw.top/mcp`,无需单独部署服务、tokens 文件或额外 nginx location。

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

### 创建 `mcpd_` token

MCP 地址:`https://hub.mypapers.top/mcp`、`https://hub.maclaw.top/mcp`(注意是 `hub.*`;`hubs.*` 是 hubcenter,不是 Hub)。
token 只以 SHA-256 存在 Hub system settings(键 `desktop_mcp_tokens`),明文只在创建响应的 `token` 字段里出现一次,请立即保存。

用户给自己的桌面开 token(用 Hub 登录 token;`scope` 为 `read` 或 `control`,用户不能自开 bash/install):

```sh
curl -X POST https://hub.mypapers.top/api/v1/desktop-mcp/tokens \
  -H "Authorization: Bearer <HUB_USER_TOKEN>" -H "Content-Type: application/json" \
  -d '{"name":"cursor","scope":"control"}'
# 列出 / 吊销
curl -H "Authorization: Bearer <HUB_USER_TOKEN>" https://hub.mypapers.top/api/v1/desktop-mcp/tokens
curl -X DELETE -H "Authorization: Bearer <HUB_USER_TOKEN>" "https://hub.mypapers.top/api/v1/desktop-mcp/tokens?id=<TOKEN_ID>"
```

租户管理员给本租户用户开 token(可开 `allow_bash` / `allow_install`;服务器上的 desktop API token 也可用于开通):

```sh
curl -X POST https://hub.maclaw.top/api/admin/desktop-mcp/tokens \
  -H "Authorization: Bearer <HUB_ADMIN_TOKEN>" -H "Content-Type: application/json" \
  -d '{"user_id":"<USER_ID>","scope":"read","name":"ops","allow_bash":false,"allow_install":false}'
curl -H "Authorization: Bearer <HUB_ADMIN_TOKEN>" https://hub.maclaw.top/api/admin/desktop-mcp/tokens
curl -X DELETE -H "Authorization: Bearer <HUB_ADMIN_TOKEN>" "https://hub.maclaw.top/api/admin/desktop-mcp/tokens?id=<TOKEN_ID>"
```

客户端里把返回的 `mcpd_...` 作为 `Authorization: Bearer mcpd_<token>` 使用。

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
{"mcpServers": {"maclaw-desktop": {"url": "https://hub.maclaw.top/mcp", "headers": {"Authorization": "Bearer ${env:MACLAW_MCP_TOKEN}"}}}}
```

Claude Code:

```sh
claude mcp add --transport http maclaw-desktop https://hub.maclaw.top/mcp --header "Authorization: Bearer $MACLAW_MCP_TOKEN"
```

Claude Desktop(`claude_desktop_config.json`;通过 `mcp-remote` 桥接带 header 的远程 HTTP):

```json
{"mcpServers": {"maclaw-desktop": {"command": "npx", "args": ["-y", "mcp-remote", "https://hub.maclaw.top/mcp", "--header", "Authorization:${MACLAW_MCP_TOKEN}"],
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

## desktopd `/mcp`(按屏幕的 gate token)

除了 Hub 的 `/mcp`,desktopd 自己也挂了一个 `/mcp`(源码 `desktopd/mcp.go`),给已经拿到某个桌面会话的
agent 直接用,不经过 Hub:

- **鉴权**:`Authorization: Bearer <gate token>`。gate token 就是 `POST /v1/desktops/session` 返回的 CDP / noVNC
  URL 里 userinfo 那段(`desktop:<token>@…`),每块屏幕(用户桌面或隔离 bot 的显示器)一个。desktopd 在打开会话时
  记下 token → (tenant, user, bot, display);`/v1/desktops/stop` 后作废;desktopd 重启后要重新打开一次会话,
  旧 token 才会重新生效(在那之前返回 401,不会猜)。
- **权限**:`control` 范围,只能操作这个 token 对应的那块屏幕;允许 `desktop_bash` / `desktop_file_upload`
  (持有者本来就能通过 CDP 驱动这块屏幕的浏览器),**不允许** `desktop_install`。
- **工具与协议**:和 Hub `/mcp` 完全相同(`corelib/desktop/desktopmcp`):截图 `desktop_screenshot`;点击、输入、
  按键、滚动、拖拽都走 `desktop_computer`(Anthropic / OpenAI / MaClaw 三种动作格式);`desktop_bash`;
  `desktop_file_read` / `desktop_file_write` / `desktop_file_list` / `desktop_file_download`,以及窗口、剪贴板、
  无障碍树等。Streamable HTTP,`POST /mcp` 返回 `application/json`。
- **人工接管**:默认和 Hub 一样,有人在 noVNC 里看/操作时拒绝控制类调用;`DESKTOPD_MCP_TAKEOVER=off` 关闭这个检查。
- **关闭**:`DESKTOPD_MCP=off`(返回 404)。
- **暴露面**:`/mcp` 挂在 desktopd 的 API 监听端口上,和 `/v1/*` 同一个端口,但不接受 desktopd 的服务 token,
  只接受 gate token。对外仍推荐 Hub 的 `/mcp`(有 `mcpd_` token、read/control 范围和吊销)。

示例(Claude Code):

```sh
claude mcp add --transport http maclaw-screen http://<desktopd>:<port>/mcp \
  --header "Authorization: Bearer <gate token>"
```
