# MaClaw Mobile

MaClaw Mobile is the emergency mobile companion for MaClaw. It focuses on the
things that make sense when a user is away from a desktop:

- ask an AI assistant and search for current information;
- create, edit, summarize, and export urgent documents;
- connect to remote servers with a manual SSH terminal;
- check account, Hub, service, and credential status.

The previous mobile programs were intentionally removed. The active project is
`maclaw_mobile/`.

## Development

This repository snapshot does not vendor Flutter. Install Flutter 3.22+ and run:

```bash
cd mobile/maclaw_mobile
flutter pub get
flutter test
flutter run
```

If native Android/iOS wrapper files are missing in a fresh checkout, generate
them from this project directory:

```bash
flutter create --platforms android,ios .
```

