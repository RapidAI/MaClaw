# MaClaw Mobile

An Android/iOS Flutter app for emergency AI work:

- information lookup with sources;
- urgent document drafting, editing, and export;
- manual SSH server maintenance with AI-assisted log explanation;
- Hub account, quota, service status, cache, and credential management.

The app connects to Hub APIs and does not depend on the desktop Wails GUI.

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

