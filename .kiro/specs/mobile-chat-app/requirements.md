# Mobile Chat App — Requirements

## Overview

自建 IM Chat 应用，替代对飞书/QQ Bot 等第三方 IM 的依赖。支持人与人、人与机器（MaClaw Agent）的单聊和群聊，以及实时语音通话。

## Target Platforms

- Android (Flutter)
- iOS (Flutter)
- HarmonyOS (Flutter via flutter_ohos)
- 三端风格一致

## Functional Requirements

### FR-1: Chat 模式（文字/图片/语音消息）
- 单聊（1v1：人与人、人与机器）
- 群聊（人与多台机器、多人混合）
- 消息类型：文字、图片、语音条（录音）、文件
- 消息撤回、编辑
- 已读回执（批量上报）
- @提及（群聊中 @ 特定成员）
- 群消息免打扰
- 本地消息缓存 + 增量同步（秒开体验）
- 乐观更新 + 幂等发送（弱网友好）

### FR-2: Voice 模式（实时语音通话）
- 1v1 语音通话（人与人、人与机器）
- 多方语音会议（人与多台机器）
- 通话控制：静音、免提、挂断
- 来电通知（在线 WS + 离线推送）
- 通话记录

### FR-3: 原生推送通知
- iOS: APNs
- Android: FCM (Google) + HMS Push (华为备选)
- HarmonyOS: HMS Core Push Kit
- 离线消息推送、来电推送

### FR-4: 陪伴功能
- 机器（MaClaw Agent）作为 channel 成员参与对话
- 语音通话中机器端：音频 → STT → Agent 处理 → TTS → 音频回传
- 支持一个人同时与多台机器聊天/通话

## Non-Functional Requirements

- NFR-1: Hub 负载可控 — WS 只推 hint（<100 bytes），数据操作走 HTTP REST
- NFR-2: 语音通话走 WebRTC P2P，hub 只做信令中转
- NFR-3: 消息去重（client_msg_id 幂等）
- NFR-4: 双向游标分页拉取
- NFR-5: 安全 — 复用 hub 现有 token 认证，传输层 HTTPS/WSS
