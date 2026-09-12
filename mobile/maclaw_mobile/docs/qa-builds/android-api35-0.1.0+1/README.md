# Android API 35 QA Evidence

This directory contains runtime evidence for the local signed internal-QA APK.
It is emulator evidence and does not replace the required Android 13+ physical
device record.

```text
Date: 2026-07-10
Scope: Android internal QA preparation
Device: maclaw-api35 Android API 35 AVD (1080x2400)
Package: top.mypapers.maclaw.mobile
Artifact: ../../../build/app/outputs/flutter-apk/app-release.apk
Artifact SHA256: 236465C58D338F4D2714F4D38CCE70B4412017E4F565148BD97667B60976A130
Install: adb install --no-streaming -r succeeded
Launch: MainActivity launched after sys.boot_completed=1
```

## Captured Evidence

- `cold-start-registration.png`: signed APK cold start after launch.
- `cold-start-ui.xml`: Android UI hierarchy from the same launch.
  It identifies the MaClaw logo content description, phone registration entry,
  without rendering HubCenter candidate addresses on the login screen. The
  official HubCenter candidates remain an internal discovery detail.
- `cold-start-permission-prompt.png`, `cold-start-permission-ui.xml`,
  `cold-start-registration-after-permission.png`, and
  `cold-start-registration-after-permission-ui.xml`: historical captures from
  before startup permission prompting was removed; they are retained for
  change history and are not current startup evidence.
- `startup-no-permission.png` and `startup-no-permission-ui.xml`: current
  signed APK cold start after clearing app data; the phone registration entry
  is immediately visible, no notification permission prompt is present, and
  HubCenter candidate addresses are absent.
- `startup-login-latest.png` and `startup-login-latest-ui.xml`: refreshed
  evidence from the latest signed APK after the login copy change; the first
  screen still contains only phone registration/login and MaClaw official
  service wording, with no HubCenter candidate or Flutter branding.
- `startup-media-handling-latest.png` and
  `startup-media-handling-latest-ui.xml`: latest signed APK cold-start capture
  after assistant camera/gallery/file picker failure handling was added.
- `startup-document-list-latest.png` and
  `startup-document-list-latest-ui.xml`: latest signed APK after adding the
  document list insertion shortcut; cold-start assertions still pass.
- `startup-remote-command-latest.png` and
  `startup-remote-command-latest-ui.xml`: latest signed APK after adding Hub
  failure handling for remote command and background-task requests.
- `startup-document-import-latest.png` and
  `startup-document-import-latest-ui.xml`: latest signed APK after adding
  document-page picker failure feedback while preserving existing drafts.
- `startup-assistant-notification-latest.png` and
  `startup-assistant-notification-latest-ui.xml`: latest signed APK after
  adding long-running assistant task notification routing.
- `signed-startup-login-latest.png` and `signed-startup-login-latest-ui.xml`:
  freshly reinstalled signed APK evidence from the API 35 emulator. The
  `top.mypapers.maclaw.mobile` package installed successfully, version `0.1.0`
  launched, and the UI hierarchy contains the MaClaw logo and phone
  registration/login entry without HubCenter candidate text or Flutter
  branding.
- `signed-startup-qr-redaction-latest.png` and
  `signed-startup-qr-redaction-latest-ui.xml`: freshly rebuilt signed APK
  evidence after the optional desktop GUI QR authorization error-redaction
  change; the same MaClaw-branded phone-login assertions passed.
- `share-text-activity.txt`: `ACTION_SEND` / `text/plain` query includes
  `top.mypapers.maclaw.mobile/.MainActivity`.
- `share-pdf-activity.txt`: `ACTION_SEND` / PDF query includes the mobile
  activity.
- `share-csv-multiple-activity.txt`: `ACTION_SEND_MULTIPLE` / CSV query
  includes the mobile activity.
- `share-text-latest-activity.txt`, `share-image-latest-activity.txt`,
  `share-pdf-latest-activity.txt`, `share-word-latest-activity.txt`,
  `share-excel-latest-activity.txt`, and `share-csv-latest-activity.txt`:
  the latest signed APK is registered for all six required text/image/PDF/
  Word/Excel/CSV share MIME queries on the API 35 emulator.
- The seven `*-latest-activity.txt` query files were refreshed after the
  signed APK rebuild with SHA256 `236465C58D338F4D2714F4D38CCE70B4412017E4F565148BD97667B60976A130`;
  each query again included the MaClaw Mobile activity.
- `share-text-runtime.png` and `share-text-runtime-ui.xml`: an actual
  `ACTION_SEND` text Intent was selected in the Android resolver and delivered
  to `top.mypapers.maclaw.mobile/.MainActivity`; the logged-out app returned
  to its MaClaw-branded phone registration screen.
- `share-text-delivered.png` and `share-text-delivered-ui.xml`: delivered
  Intent runtime screenshot and hierarchy, including the MaClaw logo and phone
  registration entry without exposing HubCenter candidate addresses.
- `share-text-runtime-latest.png` and `share-text-runtime-latest-ui.xml`:
  latest signed APK Android resolver evidence showing `Share with MaClaw
  Mobile` for an `ACTION_SEND` text payload.
- `share-text-delivered-latest.png` and
  `share-text-delivered-latest-ui.xml`: latest resolver selection using `Just
  once`, followed by delivery to the MaClaw Mobile activity and return to the
  phone registration screen without HubCenter candidate addresses.
- `share-text-signed-latest-resolver.png` and
  `share-text-signed-latest-resolver-ui.xml`: latest signed APK text-share
  Resolver capture showing `Share with MaClaw Mobile` and the `Just once`
  choice.
- `share-text-signed-latest-delivered.png` and
  `share-text-signed-latest-delivered-ui.xml`: latest signed APK after choosing
  `Just once`; delivery returned to the MaClaw phone-login screen with the
  MaClaw logo and no HubCenter candidate or Flutter branding.
- `package-dumpsys.txt`: installed package and activity dump.

The capture did not perform SMS verification, official-credit charging, voice
transcription, camera input, Hub SSH handoff, or physical-device permission QA;
those remain separate manual gates.
