# MaClaw Mobile

MaClaw Mobile is the emergency mobile companion for MaClaw. It focuses on the
things that make sense when a user is away from a desktop:

- ask an AI assistant and search for current information;
- create, edit, summarize, and export urgent documents;
- connect to remote servers with a manual SSH terminal;
- access digital employees on remote servers or desktops for delegated tasks;
- check account, Hub, service, and credential status.

The mobile app connects only through the official MaClaw HubCenter candidates:
`https://hubs.mypapers.top`, `https://hubs.maclaw.top`, and
`https://hubs2.maclaw.top`. It probes those preset endpoints, discovers the
user's Hub and tenant through the selected HubCenter, and intentionally has no
custom Hub URL setting.

The previous mobile programs were intentionally removed. The active project is
`maclaw_mobile/`.

## Development

This repository snapshot does not vendor Flutter. Install Flutter 3.22+ and run:

```bash
cd mobile/maclaw_mobile
flutter pub get
flutter test
flutter build apk --debug
flutter run
```

If native Android/iOS wrapper files are missing in a fresh checkout, generate
them from this project directory:

```bash
flutter create --platforms android,ios .
python3 tool/configure_platforms.py
```
