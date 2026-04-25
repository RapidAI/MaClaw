# 码卡龙hub服务器部署手册

本文档仅供码卡龙使用。

需要提供待安装 Hub 服务器软件的服务器信息，包括：

- 服务器 IP 或域名
- 登录用户
- 登录密码
- 管理员邮箱

提供上述信息与本文档即可。

码卡龙会自行完成 Hub 服务器的部署、配置与注册。

安装部署完成后，码卡龙需要告知用户设置好的管理员用户名及密码。默认管理员用户名为 `admin`。
## 文档用途

本文档说明如何从源码编译并部署 `hub`，完成服务器配置，并将自部署 Hub 注册到默认 Hub Center。

默认 Hub Center 注册地址：`https://hubs.maclaw.top`

不鼓励自行安装或维护 `hubcenter`。常规自部署场景下，直接将 `hub` 注册到默认官方 Hub Center 即可。

## 1. 适用范围

本文档覆盖以下内容：

- 从源码编译 `hub`
- 服务器目录规划
- `hub` 配置文件编写
- `systemd` 服务配置
- Nginx 反向代理建议
- 向默认 Hub Center 注册流程
- 注册后的确认与排障

本文档不包含：

- `hubcenter` 自部署
- `hubcenter` 高可用集群部署
- 自建 Hub Center 邮件系统配置

## 2. 部署目标

完成后你将得到：

- 一套运行中的自部署 `hub`
- 可访问的 Hub 管理后台
- 可访问的 Hub PWA 页面
- 已注册到默认 Hub Center 的 Hub

推荐公网访问关系：

- `https://hub.example.com` -> 你的自部署 `hub`
- `https://hubs.maclaw.top` -> 默认 Hub Center

## 3. 环境要求

推荐环境：

- Ubuntu 22.04 / Debian 12 / 其他兼容 Linux 发行版
- Go `1.22+`
- `git`
- `systemd`
- `nginx` 或 `caddy`
- 一个用于 `hub` 的公网域名

建议开放端口：

- 对外：`80`、`443`
- 对内：`9399`（hub）

## 4. 目录规划

建议部署目录：

```text
/data/soft/maclaw/                 # 仓库源码
/data/soft/hub/                     # hub 运行目录
```

建议运行目录结构：

```text
/data/soft/hub/
  maclaw-hub
  configs/config.yaml
  web/
  data/
  data/logs/
```

## 5. 获取源码

```bash
cd /data/soft
git clone https://github.com/rapidai/maclaw maclaw
cd /data/soft/maclaw
```

如果仓库已存在：

```bash
cd /data/soft/maclaw
git pull
```

## 6. 编译 Hub

在源码目录执行：

```bash
cd /data/soft/maclaw/hub
go build -o maclaw-hub ./cmd/hub
```

复制运行文件：

```bash
mkdir -p /data/soft/hub/configs
mkdir -p /data/soft/hub/data/logs
cp maclaw-hub /data/soft/hub/
cp -r web /data/soft/hub/
cp configs/config.example.yaml /data/soft/hub/configs/config.yaml
```

## 7. 配置 Hub

编辑文件：`/data/soft/hub/configs/config.yaml`

推荐最小生产配置：

```yaml
server:
  listen_host: 0.0.0.0
  listen_port: 9399
  public_base_url: https://hub.example.com

database:
  driver: sqlite
  dsn: ./data/maclaw-hub.db
  wal: true
  busy_timeout_ms: 5000
  max_read_open_conns: 8
  max_read_idle_conns: 4
  max_write_open_conns: 1
  max_write_idle_conns: 1

identity:
  enrollment_mode: open
  allow_self_enroll: true

pwa:
  static_dir: ./web/dist
  route_prefix: /app

center:
  enabled: true
  base_url: https://hubs.maclaw.top
  base_urls:
    - https://hubs.maclaw.top
  register_on_startup: true
  heartbeat_interval_sec: 30

hub:
  name: My Self-Hosted Hub
  description: Self-hosted MaClaw remote hub
  visibility: private

mail:
  enabled: false
  provider: smtp
  smtp_host: smtp.example.com
  smtp_port: 587
  smtp_username: no-reply@example.com
  smtp_password: change-me
  from_name: My Hub
  from_email: no-reply@example.com

logging:
  level: info
  dir: ./data/logs
```

关键说明：

- `server.public_base_url` 必须写你的 Hub 外网访问地址
- `center.base_url` 默认填写 `https://hubs.maclaw.top`
- `hub.name` 是注册后在 Hub Center 中显示的名称
- `hub.visibility` 建议初始值使用 `private`
- `register_on_startup: true` 表示 Hub 启动后会自动尝试注册

## 8. 启动 Hub

测试启动：

```bash
cd /data/soft/hub
./maclaw-hub --config ./configs/config.yaml
```

健康检查：

```bash
curl -fsSL http://127.0.0.1:9399/healthz
```

## 9. 配置 Hub systemd

创建文件：`/etc/systemd/system/hub.service`

```ini
[Unit]
Description=MaClaw Hub
After=network.target

[Service]
Type=simple
WorkingDirectory=/data/soft/hub
ExecStart=/data/soft/hub/maclaw-hub --config /data/soft/hub/configs/config.yaml
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

启用服务：

```bash
systemctl daemon-reload
systemctl enable hub
systemctl start hub
systemctl status hub --no-pager
```

查看日志：

```bash
journalctl -u hub -f
```

## 10. 初始化 Hub 管理员

访问：

- `https://hub.example.com/admin`

首次进入时创建管理员。默认管理员用户名建议使用 `admin`。

如果使用 API 初始化：

```bash
curl -X POST https://hub.example.com/api/admin/setup \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "ChangeMe123!",
    "email": "owner@example.com"
  }'
```

注意：

- Hub 注册到 Hub Center 时，会优先使用 Hub 管理员邮箱作为 `owner_email`
- 如果管理员邮箱为空，注册时会报 `admin email is required for hub registration`
## 11. 企业自用限制配置

如果该 Hub 仅用于企业内部使用，可以在后台管理中心限制允许登录或注册的企业邮箱域名。

配置位置：

- Hub 后台管理中心
- 企业邮箱域名

填写规则：

- 填写邮箱 `@` 后面的域名
- 不包括 `@`
- 只填写域名本身

示例：

- 邮箱是 `alice@maclaw.cn`，则填写 `maclaw.cn`
- 邮箱是 `bob@rapidai.com`，则填写 `rapidai.com`

错误示例：

- `@maclaw.cn`
- `alice@maclaw.cn`
- `https://maclaw.cn`

建议：

- 只填写企业正式邮箱域名
- 配置后先用一个企业邮箱账号做一次登录或注册验证
- 如果企业有多个合法域名，按后台支持方式逐项添加

## 12. Nginx 反向代理示例

```nginx
server {
    listen 80;
    server_name hub.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name hub.example.com;

    ssl_certificate     /etc/nginx/ssl/hub.example.com/fullchain.pem;
    ssl_certificate_key /etc/nginx/ssl/hub.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:9399;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
```

## 13. Hub 向默认 Hub Center 注册流程

注册链路如下：

1. 完成 Hub 初始化
2. Hub 配置 `center.base_url = https://hubs.maclaw.top`
3. Hub 调用 `POST /api/admin/center/register`
4. Hub 服务内部向默认 Hub Center 发起 `POST /api/hubs/register`
5. Hub Center 生成 `hub_id` 与 `hub_secret`
6. Hub 根据状态进入待确认或已注册状态
7. Hub 开始周期性上报 `POST /api/hubs/{id}/heartbeat`
8. Hub 状态最终变为 `online`

## 14. 手工保存 Hub Center 配置

如果需要先在 Hub 后台保存配置，再执行注册，可调用：

```bash
curl -X POST https://hub.example.com/api/admin/center/config \
  -H "Authorization: Bearer <hub-admin-token>" \
  -H "Content-Type: application/json" \
  -d '{
    "base_url": "https://hubs.maclaw.top",
    "public_base_url": "https://hub.example.com",
    "visibility": "private",
    "enrollment_mode": "open"
  }'
```

对应含义：

- `base_url`：默认 Hub Center 地址
- `public_base_url`：Hub 对外地址
- `visibility`：Hub 可见性
- `enrollment_mode`：注册模式

## 15. 执行 Hub 注册

### 14.1 在后台点击注册

登录：

- `https://hub.example.com/admin`

在 Hub Center 页面点击“Register Now / 立即注册”。

### 14.2 使用 API 注册

```bash
curl -X POST https://hub.example.com/api/admin/center/register \
  -H "Authorization: Bearer <hub-admin-token>"
```

注册成功后，可查看状态：

```bash
curl -X GET https://hub.example.com/api/admin/center/status \
  -H "Authorization: Bearer <hub-admin-token>"
```

重点字段：

- `registered`
- `pending_confirmation`
- `hub_id`
- `active_base_url`
- `last_error`

## 16. 注册确认说明

默认情况下，Hub 注册后通常通过邮件自行确认注册状态。

你需要关注的是 Hub 后台返回的注册状态：

- 如果 `registered = true`，说明注册已完成
- 如果 `pending_confirmation = true`，说明注册请求已经送达，等待完成确认
- 如果 `last_error` 有值，按错误信息排查

常规流程如下：

1. Hub 完成注册后，通常会进入待确认状态
2. 按邮件指引自行确认注册状态
3. 确认完成后，Hub 会继续发送心跳并最终进入 `online`

如果需要手工确认，请联系微信：`znsoft`

联系时请明确提供以下信息：

- 注册的 Hub 域名
- 管理员邮箱
- 明确说明“需要手工确认注册”

不需要自行安装 `hubcenter` 来处理这一步。

## 17. 验证 Hub 在线状态

可从以下角度验证：

### 16.1 检查 Hub 管理后台

确认内容：

- Hub Center 状态不是错误
- 注册状态正常
- 没有持续出现注册失败信息

### 16.2 检查接口

```bash
curl -fsSL https://hub.example.com/healthz
```

### 16.3 检查 PWA 页面

确认以下地址可以访问：

- `https://hub.example.com/admin`
- `https://hub.example.com/app`

## 18. 常见错误与处理

### 17.1 `admin email is required for hub registration`

原因：

- Hub 没有可用管理员邮箱

处理：

- 先初始化 Hub 管理员
- 确认管理员邮箱字段已填写

### 17.2 注册成功但一直 `pending_confirmation`

原因：

- 默认 Hub Center 尚未完成确认

处理：

- 等待默认 Hub Center 侧确认
- 持续查看 Hub 后台状态
- 如长时间未完成，再根据实际运营流程联系维护方

### 17.3 注册后的 `base_url` 不正确

原因：

- Hub 的 `server.public_base_url` 配置错误

处理：

- 改成真实公网地址
- 保存 Hub Center 配置
- 重新注册一次

### 17.4 Hub 可启动但 `/app` 打不开

检查项：

- `web` 目录是否完整复制到运行目录
- 反向代理是否指向 `9399`
- HTTPS 证书是否有效
- WebSocket 转发是否已开启

### 17.5 Hub 后台可打开但注册失败

检查项：

- `https://hubs.maclaw.top` 是否能被 Hub 所在服务器访问
- 服务器时间是否准确
- 防火墙是否放通 443
- 反向代理是否正确

## 19. 最终验收清单

满足以下条件即可认为部署完成：

- `hub` 服务正在运行
- `https://hub.example.com/admin` 可登录
- `https://hub.example.com/app` 可访问
- Hub 注册状态正常
- Hub 的公网地址显示正确
- 无持续注册错误与心跳错误

## 20. 常用命令汇总

### 19.1 服务管理

```bash
systemctl restart hub
systemctl status hub --no-pager
```

### 19.2 日志查看

```bash
journalctl -u hub -f
```

### 19.3 健康检查

```bash
curl -fsSL https://hub.example.com/healthz
```
