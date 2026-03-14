# Mobile Shells

This folder contains all generated mobile shell projects and mobile-only artifacts.

## Layout

- `android/`
  Android shell project, packaging scripts, keystore helpers, and signing config templates.
- `ios/`
  iOS shell project and simulator build script.
- `shared/bootstrap.html`
  Shared mobile entry page used by both Android and iOS shells.
- `dist/`
  Consolidated mobile packaging outputs.
  See `dist/README.md` for the handoff-oriented deliverable list.

## Current Outputs

Artifacts currently present in `dist/`:

- `codeclaw-release.apk`
- `codeclaw-release.aab`
- `README.md`

Future Android debug builds also land here as:

- `codeclaw-debug.apk`

## Defaults

Both Android and iOS default to Hub Center `http://hubs.rapidai.tech`.

## Entry Points

- Android debug APK: `android/build_android.cmd`
- Android unsigned release APK: `android/build_android_release.cmd`
- Android signed release APK: `android/build_android_signed_release.cmd`
- Android unsigned release AAB: `android/build_android_aab.cmd`
- Android signed release AAB: `android/build_android_signed_aab.cmd`
- iOS simulator build: `ios/build_ios_simulator.sh`
- iOS simulator release build: `ios/build_ios_simulator_release.sh`
