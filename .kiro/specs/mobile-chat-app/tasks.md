# Mobile Chat App — Tasks

## Phase 1: 骨架搭建 ✅

- [x] Task 1: 整理 mobile 目录，现有内容移入 terminal/
- [x] Task 2: 创建 Flutter Chat 项目骨架 (pubspec.yaml, models, services, screens, widgets)
- [x] Task 3: 创建 hub/internal/chat/ 后端模块 (types, store, services)
- [x] Task 4: 创建 push dispatcher 骨架 (apns, fcm, hms)
- [x] Task 5: 验证 Go 后端编译通过
- [x] Task 6: 编写 spec 文档 (requirements, design, tasks)

## Phase 2: 后端 API 接入

- [x] Task 7: 创建 hub/internal/httpapi/chat_handlers.go — REST API handlers
  - POST /api/chat/channels — 创建 channel
  - GET /api/chat/channels — 获取用户 channel 列表
  - POST /api/chat/channels/{id}/messages — 发消息
  - GET /api/chat/channels/{id}/messages — 拉取消息 (after_seq / before_seq)
  - POST /api/chat/read-receipts — 批量已读
  - POST /api/chat/files/upload — 文件上传
  - GET /api/chat/files/{id} — 文件下载
  - GET /api/chat/users/{id}/presence — 在线状态
- [x] Task 8: 创建 Chat WS endpoint — /api/chat/ws
  - 认证握手
  - hint 推送
  - 心跳 ping/pong
- [x] Task 9: 语音通话信令 API
  - POST /api/chat/voice/call
  - POST /api/chat/voice/answer
  - POST /api/chat/voice/ice
  - POST /api/chat/voice/hangup
- [x] Task 10: 注册 chat routes 到 hub httpapi router
- [x] Task 11: 在 hub bootstrap 中初始化 chat 模块

## Phase 3: Flutter 客户端完善

- [x] Task 12: 实现登录/认证流程 (复用 hub token)
- [x] Task 13: 完善 ConversationListScreen — 真实数据加载
- [x] Task 14: 完善 ChatRoomScreen — 消息渲染 + 发送 + 增量同步
- [x] Task 15: 实现图片选择 + 上传 + 渲染
- [x] Task 16: 实现语音录制 + 上传 + 播放
- [x] Task 17: 实现 WebRTC 语音通话 (1v1)
- [x] Task 18: 实现多方语音会议
- [x] Task 19: 实现原生推送注册 + 接收

## Phase 4: 鸿蒙适配

- [x] Task 20: 配置 flutter_ohos 环境
- [x] Task 21: 适配鸿蒙 WebRTC
- [x] Task 22: 接入 HMS Push Kit
- [x] Task 23: 鸿蒙端测试 + 调优

## Phase 5: 打磨

- [x] Task 24: 消息撤回/编辑 UI
- [x] Task 25: @提及 + 群管理
- [x] Task 26: 通话记录页面
- [x] Task 27: 深色模式适配
- [x] Task 28: 性能优化 (大量消息滚动、图片缓存)
