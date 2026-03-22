# Mobile Apps

This folder contains all mobile projects for MaClaw.

## Layout

- `terminal/` — WebView Shell app (existing). Loads Hub web UI in a native WebView container.
  - `android/` — Android shell project
  - `ios/` — iOS shell project
  - `shared/` — Shared bootstrap HTML entry point
  - `ipa/` — iOS IPA artifacts
- `chat/` — Flutter Chat app (new). Native IM with text/image/voice messaging and voice calls.
  - Targets: Android, iOS, HarmonyOS (via flutter_ohos)
- `dist/` — Consolidated packaging outputs for all mobile apps.

## Chat App

The Chat app is a Flutter-based IM client supporting:
- Text, image, and voice messages (single chat & group chat)
- Real-time voice calls (1v1 and multi-party via WebRTC)
- Native push notifications (APNs / FCM / HMS Push)
- Local message caching with incremental sync
- Human-to-human and human-to-machine(s) conversations

## Terminal App

The Terminal app is a lightweight WebView shell that connects to Hub Center
and loads the Hub's web-based admin/management UI.
