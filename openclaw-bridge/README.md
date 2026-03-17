# OpenClaw IM Bridge

将 OpenClaw 生态的 IM 频道插件（Telegram、Discord、Slack 等）桥接到 MaClaw Hub。

## 架构

```
┌─────────────┐     OpenClaw IM Protocol      ┌──────────────┐
│  IM 平台     │ ←→ OpenClaw Channel Plugin ←→ │  IM Bridge   │
│ (Telegram等) │                                │  (本项目)     │
└─────────────┘                                └──────┬───────┘
                                                      │ POST /api/openclaw_im/webhook
                                                      │ (HMAC-SHA256 签名)
                                                      ▼
                                               ┌──────────────┐
                                               │  MaClaw Hub  │
                                               └──────┬───────┘
                                                      │ POST /outbound
                                                      │ (HMAC-SHA256 签名)
                                                      ▼
                                               ┌──────────────┐
                                               │  IM Bridge   │ → 路由到对应插件 → 发回 IM
                                               └──────────────┘
```

## 消息流

**入站** (用户 → Hub):
1. 用户在 Telegram/Discord/Slack 发消息
2. OpenClaw 插件接收消息
3. Bridge 将消息转为 `IncomingMessage` JSON
4. POST 到 Hub 的 `/api/openclaw_im/webhook`（带 HMAC 签名）

**出站** (Hub → 用户):
1. Hub 处理完消息后，POST 到 Bridge 的 `/outbound`
2. Bridge 验证 HMAC 签名
3. 根据 `platform_uid` 前缀路由到对应的 OpenClaw 插件
4. 插件调用平台 API 发送回复

## 快速开始

```bash
cd openclaw-bridge
npm install

# 安装你需要的频道插件
npm install @openclaw/telegram    # Telegram
npm install @openclaw/discord     # Discord
npm install @openclaw/slack       # Slack

# 配置
cp config.example.json config.json
# 编辑 config.json，填入 Hub 地址、密钥和频道凭据

# 启动
npm run dev
```

## 配置

Hub 和 Bridge 预置了相同的默认密钥 `maclaw-openclaw-local-secret`，开箱即用。
Bridge 默认只监听 `127.0.0.1`，因此本机部署是安全的。用户可在 Hub 管理后台修改密钥，
修改后需同步更新 Bridge 的 `config.json`。

在 Hub 管理后台设置 OpenClaw IM：
- **Webhook URL**: `http://<bridge-host>:3210/outbound`
- **Secret**: 与 config.json 中的 `hub.secret` 一致（默认已预置）
- **Enabled**: true

## platform_uid 格式

Bridge 使用 `<channelId>:<senderId>` 格式的 platform_uid，例如：
- `telegram:123456789`
- `discord:987654321`
- `slack:U0123456`

Hub 在回复时会原样返回这个 uid，Bridge 据此路由到正确的插件。
