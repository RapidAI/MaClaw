# Mobile Chat App — Design

## Architecture

```
Flutter Client                          Hub Server
┌─────────────┐                    ┌──────────────────┐
│ HTTP POST   │───发消息/管理────→│ REST API         │
│ HTTP GET    │───拉增量消息────→│ (chat handlers)  │
│             │                    │                  │
│ WS (单向)   │←──hint/typing───│ Chat Notifier    │
│             │                    │                  │
│ 本地 SQLite │  离线时           │ Push Dispatcher  │
│ (消息缓存)  │←──APNs/FCM/HMS──│ (离线推送)       │
│             │                    │                  │
│ WebRTC P2P  │═══语音直连═══════│ Voice Signaling  │
│             │                    │ (仅信令中转)     │
└─────────────┘                    └──────────────────┘
```

## Communication Protocol

### WS Hint Format (server → client, <100 bytes)
```json
{"t":"msg","ch":"channel_id","seq":1043}
{"t":"typing","ch":"channel_id","uid":"user_id","exp":3}
{"t":"read","ch":"channel_id","seq":1040}
{"t":"recall","ch":"channel_id","msg_id":"xxx","seq":1043}
{"t":"call_incoming","ch":"channel_id"}
{"t":"ice"}
```

### Message Send (client → server, HTTP POST)
- 客户端生成 client_msg_id (UUID) 实现幂等
- 乐观更新：本地立即渲染，状态=sending
- 服务端返回 msg_id + seq 后标记 sent
- 失败标记 failed，支持重试

### Incremental Sync
- 每个 channel 维护单调递增 seq
- 客户端记录 last_sync_seq
- `GET /api/chat/channels/{id}/messages?after_seq=N&limit=50`
- `GET /api/chat/channels/{id}/messages?before_seq=N&limit=50`

### Read Receipts (batch)
```
POST /api/chat/read-receipts
{"receipts": [{"ch":"ch1","seq":1043}, {"ch":"ch2","seq":520}]}
```

### Voice Call Signaling
```
发起方 → POST /voice/call → Hub → WS hint + Push → 接收方
接收方 → POST /voice/answer → Hub → WS hint → 发起方
双方交换 ICE candidates via Hub WS forward
WebRTC P2P 建立后，音频直连，Hub 不参与
```

## Backend Module: hub/internal/chat/

| File | Responsibility |
|------|---------------|
| types.go | Channel, Message, Member, VoiceCall, Attachment 等数据模型 |
| store.go | SQLite 存储 + migration + CRUD |
| channel_service.go | Channel 创建/成员管理 |
| message_service.go | 发消息(幂等)/拉取(双向游标)/撤回 |
| file_service.go | 文件上传/下载 |
| notifier.go | WS hint 广播 + 离线推送分发 |
| presence.go | 在线状态管理 |
| read_receipt.go | 批量已读上报 |
| voice_signaling.go | 语音通话信令 |
| push/dispatcher.go | 推送路由 |
| push/apns.go | iOS 推送 |
| push/fcm.go | Android 推送 |
| push/hms.go | 鸿蒙推送 |

## Database Schema

#[[file:hub/internal/chat/store.go]] — see migrate() method

Key design decisions:
- `chat_messages.seq` — channel 内单调递增，`UNIQUE(channel_id, seq)`
- `chat_messages.client_msg_id` — 幂等去重，`UNIQUE(channel_id, client_msg_id)`
- `chat_channels.last_seq` — 原子递增 via `UPDATE ... SET last_seq = last_seq + 1 RETURNING last_seq`
- `chat_members.read_seq` — 每用户每 channel 已读位置
- `chat_members.mute` — 免打扰（跳过推送，WS hint 照发）

## Flutter Client: mobile/chat/

| Directory | Content |
|-----------|---------|
| lib/models/ | Channel, Message, User, VoiceCall 数据模型 |
| lib/services/ | ApiClient (HTTP), WsClient (WS hint), SyncService (增量同步), LocalDatabase (SQLite 缓存), PushService |
| lib/providers/ | ChatProvider (ChangeNotifier 状态管理) |
| lib/screens/home_screen.dart | 底部 Tab: Chat / Voice / Me |
| lib/screens/chat/ | 会话列表、聊天室 |
| lib/screens/voice/ | 联系人列表、通话界面 |
| lib/screens/profile/ | 个人设置 |
| lib/widgets/ | MessageBubble, ChatInputBar |

## Connection Lifecycle

```
前台活跃 → WS 连接 + 心跳 30s
后台 30s → WS 断开，切换到纯推送模式
回到前台 → 重连 WS + 拉取增量
网络切换 → 自动重连 + 指数退避 (1s, 2s, 4s, max 30s)
```

## Mobile Directory Structure

```
mobile/
├── terminal/     # 原有 WebView Shell (android + ios + shared)
├── chat/         # Flutter Chat App (新建)
│   ├── lib/
│   ├── android/  # (flutter create 生成)
│   ├── ios/      # (flutter create 生成)
│   ├── ohos/     # (flutter_ohos 生成)
│   └── pubspec.yaml
├── dist/
└── README.md
```
