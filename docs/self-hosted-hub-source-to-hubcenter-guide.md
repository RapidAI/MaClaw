# 自部署 Hub 服务器指南

本文档说明如何从源码开始，部署一套可用的 `hubcenter` 和 `hub`，并最终把自部署 Hub 注册到 Hub Center。

默认注册地址：`https://hubs.maclaw.top`

## 1. 目标

完成后你将得到：

- 一台可运行的 `hubcenter`
- 一台可运行的 `hub`
- 可访问的 Hub 管理后台与 Hub Center 管理后台
- 已注册到 Hub Center 的自部署 Hub
- 可继续给 PWA / Pocket / Desktop 使用的统一入口

## 2. 目录对应关系

仓库中的关键目录：

- Hub 源码：[hub](/D:/workprj/aicoder/hub)
- Hub Center 源码：[hubcenter](/D:/workprj/aicoder/hubcenter)
- 现有部署文档：[docs](/D:/workprj/aicoder/docs)

## 3. 准备条件

建议部署环境：

- Linux x86_64 服务器
- Go 1.22+
- `git`
- `systemd` 或你自己的进程守护方式
- 一个可公网访问的域名给 `hubcenter`
- 一个可公网访问的域名给 `hub`

推荐端口：

- `hubcenter`: `9388`
- `hub`: `9399`

如果你前面挂了 Nginx / Caddy 反代，外部可以直接暴露 `443`，内部仍然监听默认端口。

## 4. 从源码拉起 Hub Center

### 4.1 获取源码

```bash
git clone <你的仓库地址> aicoder
cd aicoder
```

### 4.2 编译 `hubcenter`

```bash
cd hubcenter
go build -o maclaw-hubcenter ./cmd/hubcenter
```

### 4.3 准备配置文件

```bash
mkdir -p configs data/logs
cp configs/config.example.yaml configs/config.yaml
```

最小可用配置建议改成：

```yaml
server:
  listen_host: 0.0.0.0
  listen_port: 9388
  public_base_url: https://hubs.maclaw.top

database:
  driver: sqlite
  dsn: ./data/maclaw-hubcenter.db
  wal: true

mail:
  enabled: true
  provider: smtp
  smtp_host: smtp.example.com
  smtp_port: 587
  smtp_username: no-reply@example.com
  smtp_password: change-me
  from_name: MaClaw Hub Center
  from_email: no-reply@example.com

logging:
  level: info
  dir: ./data/logs
```

说明：

- `server.public_base_url` 必须写成外网真实访问地址。
- `mail.enabled` 建议开启，因为 Hub 注册默认会走邮件确认。
- 如果暂时不想走邮件确认，可以后续在 Hub Center 管理后台手工确认。

### 4.4 启动 `hubcenter`

开发运行：

```bash
go run ./cmd/hubcenter --config ./configs/config.yaml
```

或直接运行编译产物：

```bash
./maclaw-hubcenter --config ./configs/config.yaml
```

如果使用仓库自带脚本：

```bash
./start.sh
```

### 4.5 验证 Hub Center 是否启动成功

```bash
curl -fsSL https://hubs.maclaw.top/healthz
```

成功时应返回健康状态。

后台地址：

- Hub Center 管理台：`https://hubs.maclaw.top/admin`

## 5. 初始化 Hub Center 管理员

首次访问管理台后，先创建管理员账号。

如果你更喜欢 API 方式，也可以直接调用：

```bash
curl -X POST https://hubs.maclaw.top/api/admin/setup \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "ChangeMe123!",
    "email": "admin@example.com"
  }'
```

完成后确认可以登录：

- `https://hubs.maclaw.top/admin`

## 6. 从源码拉起 Hub

### 6.1 编译 `hub`

```bash
cd /path/to/aicoder/hub
go build -o maclaw-hub ./cmd/hub
```

### 6.2 准备配置文件

```bash
mkdir -p configs data/logs
cp configs/config.example.yaml configs/config.yaml
```

建议改成下面这个最小可注册配置：

```yaml
server:
  listen_host: 0.0.0.0
  listen_port: 9399
  public_base_url: https://hub.example.com

database:
  driver: sqlite
  dsn: ./data/maclaw-hub.db
  wal: true

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

这里最重要的是：

- `server.public_base_url`: 你的 Hub 外网地址
- `center.base_url`: 默认注册到 `https://hubs.maclaw.top`
- `hub.name`: 注册后在 Hub Center 中看到的名称
- `hub.visibility`: 建议初次部署先用 `private`

### 6.3 启动 `hub`

开发运行：

```bash
go run ./cmd/hub --config ./configs/config.yaml
```

或直接运行编译产物：

```bash
./maclaw-hub --config ./configs/config.yaml
```

如果使用仓库自带脚本：

```bash
./start.sh
```

### 6.4 验证 Hub 是否启动成功

```bash
curl -fsSL https://hub.example.com/healthz
```

常用页面：

- Hub 管理台：`https://hub.example.com/admin`
- Hub PWA：`https://hub.example.com/app`

## 7. 初始化 Hub 管理员

首次访问 Hub 管理台，创建管理员账号。

也可以直接走 API：

```bash
curl -X POST https://hub.example.com/api/admin/setup \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "ChangeMe123!",
    "email": "owner@example.com"
  }'
```

这里的管理员邮箱很重要，因为 Hub 注册到 Hub Center 时，如果你没有手工传 owner email，系统会优先使用这个管理员邮箱。

## 8. 把 Hub 指向默认 Hub Center

如果配置文件里已经写了：

```yaml
center:
  enabled: true
  base_url: https://hubs.maclaw.top
  register_on_startup: true
```

那么 Hub 启动后会尝试自动注册。

如果你想在后台手工保存一次，也可以登录 Hub 管理台，在 Hub Center 页面里填写：

- Hub Center URL: `https://hubs.maclaw.top`
- Public Hub URL: 你的 Hub 外网地址
- Visibility: `private` 或 `shared`
- Enrollment Mode: `open` / `approval` / `manual`

对应 API 也可以直接调用：

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

## 9. 注册 Hub 到 Hub Center

### 9.1 后台注册

登录 Hub 管理台后，进入 Hub Center 页面，点击“立即注册 / Register Now”。

对应后端接口是：

- `POST /api/admin/center/register`

Hub 内部会向 Hub Center 发起：

- `POST https://hubs.maclaw.top/api/hubs/register`

提交的信息包括：

- `installation_id`
- `owner_email`
- `name`
- `description`
- `base_url`
- `host`
- `port`
- `visibility`
- `enrollment_mode`
- `capabilities`

### 9.2 直接观察注册状态

在 Hub 管理台可以查看注册状态。

对应接口：

- `GET /api/admin/center/status`

当返回里出现下面字段时说明注册链路已经打通：

- `registered`
- `pending_confirmation`
- `hub_id`
- `active_base_url`

## 10. 完成 Hub Center 确认

Hub Center 默认会先把 Hub 记为 `pending_confirmation`。

有两种完成确认的方法。

### 10.1 邮件确认

如果 Hub Center SMTP 已经配置好，Hub Center 会向 `owner_email` 发送确认邮件。

你只要点开邮件里的确认链接即可。对应确认地址形如：

```text
https://hubs.maclaw.top/hub-registration/confirm?token=<hub_id>.<token>
```

确认成功后，该 Hub 会进入 `online` 状态。

### 10.2 管理后台手工确认

如果当前没有邮件能力，也可以登录 Hub Center 管理台：

- `https://hubs.maclaw.top/admin`

在 Hubs 列表中找到你的 Hub，执行确认。

对应接口：

- `POST /api/admin/hubs/{id}/confirm`

## 11. 确认心跳是否正常

注册成功后，Hub 会按 `heartbeat_interval_sec` 向 Hub Center 持续发送心跳。

对应接口：

- `POST /api/hubs/{id}/heartbeat`

如果一切正常，Hub Center 中该 Hub 会显示在线状态。

你也可以在 Hub Center 后台查看 Hub 列表，确认：

- Hub 名称正确
- `base_url` 正确
- 状态为 `online`
- 最后心跳时间持续更新

## 12. 反向代理建议

生产环境建议把 `hubcenter` 和 `hub` 都放在反向代理后面。

示例：

- `https://hubs.maclaw.top` -> `127.0.0.1:9388`
- `https://hub.example.com` -> `127.0.0.1:9399`

注意：

- `server.public_base_url` 必须写外部最终访问地址
- 不能写内网地址或 `127.0.0.1`
- 否则注册到 Hub Center 的 `base_url` 会错误

## 13. 推荐的 systemd 服务

### 13.1 hubcenter.service

```ini
[Unit]
Description=MaClaw Hub Center
After=network.target

[Service]
Type=simple
WorkingDirectory=/data/soft/hubcenter
ExecStart=/data/soft/hubcenter/maclaw-hubcenter --config /data/soft/hubcenter/configs/config.yaml
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

### 13.2 hub.service

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

## 14. 排错清单

### 14.1 Hub 注册时报 `admin email is required for hub registration`

说明 Hub 还没有有效管理员邮箱。

处理方式：

- 先完成 Hub 管理员初始化
- 或在注册前确保后台保存过管理员邮箱

### 14.2 Hub 注册时报 `mail delivery is not configured`

说明 Hub Center 当前没有可用邮件配置，而注册流程要求发确认邮件。

处理方式：

- 给 Hub Center 配 SMTP
- 或由 Hub Center 管理员登录后台手工确认该 Hub

### 14.3 注册成功但一直 `pending_confirmation`

说明注册请求已经到了 Hub Center，但确认步骤还没完成。

处理方式：

- 点击确认邮件
- 或在 Hub Center 后台手工确认

### 14.4 注册后显示的 Hub 地址不对

一般是 `server.public_base_url` 配错。

处理方式：

- 改成真实外网 URL
- 在 Hub 后台重新保存 Center 配置
- 再执行一次注册

### 14.5 Hub Center 能注册但用户无法访问 PWA

检查：

- `https://hub.example.com/app` 是否能打开
- 反代是否支持 WebSocket
- SSL 证书是否有效

## 15. 最终验收

满足下面几项，就说明整套流程已经完成：

- `https://hubs.maclaw.top/healthz` 正常
- `https://hub.example.com/healthz` 正常
- Hub 和 Hub Center 管理台都能登录
- Hub Center 中能看到你的 Hub
- Hub 状态为 `online`
- Hub 的 `base_url` 是正确的公网地址
- `https://hub.example.com/app` 可访问

## 16. 相关文档

- [Hub 部署与配置总览](/D:/workprj/aicoder/docs/07-deployment-and-config.md)
- [Remote 使用与部署说明](/D:/workprj/aicoder/docs/REMOTE_USAGE_AND_DEPLOYMENT.md)
- [Hub Center 三节点 HA](/D:/workprj/aicoder/docs/hubcenter-ha-3nodes.md)
