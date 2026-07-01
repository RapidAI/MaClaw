# MaClaw Mobile

An Android/iOS Flutter app for emergency AI work:

- information lookup with sources;
- urgent document drafting, editing, and export;
- manual SSH server maintenance with AI-assisted log explanation;
- digital employee access for remote server or desktop capabilities;
- Hub account, quota, service status, cache, and credential management.

The app connects only to the official MaClaw service and does not expose custom
Hub endpoint configuration. It does not depend on the desktop Wails GUI.

## Commands

```bash
flutter pub get
flutter test
flutter run
```

Generate native wrappers if needed:

```bash
flutter create --platforms android,ios .
```
