# MaClaw Mobile Device QA Checklist

Use this checklist for signed Android/iOS QA builds. Local tests and the debug
APK are not enough to close these gates. Attach screenshots, screen recordings,
device logs, task IDs, and artifact hashes to the QA build record in
`release_evidence.md`. Generate a validator-named record before QA starts:
Before attaching any screenshot, recording, terminal output, or device log,
redact private customer content and raw secrets such as Authorization/Cookie
headers, JWTs, API keys, cloud access key IDs, and URLs with embedded credentials.

```bash
python3 tool/release_status_report.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development
python3 tool/release_handoff.py --version <version+build> --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --output docs/qa-builds/handoff-<version+build>.md
python3 tool/setup_android_signing.py
python3 tool/setup_ios_export_options.py --team-id <APPLE_TEAM_ID> --export-method development
python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development
python3 tool/build_android_release.py --artifact apk --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"
python3 tool/build_android_release.py --artifact appbundle --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"
python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development
python3 tool/signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive" --team-id <APPLE_TEAM_ID> --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds
python3 tool/verify_runtime_boundary.py --log docs/qa-builds/runtime-boundary-<version+build>.log
python3 tool/run_release_gates.py --log docs/qa-builds/release-gates-<version+build>.log
python3 tool/create_qa_build_record.py --scope android-ios --version <version+build> \
  --release-handoff-result "release_handoff.py output saved to docs/qa-builds/handoff-<version+build>.md" \
  --runtime-boundary-result "MaClaw Mobile runtime boundary verified. log: docs/qa-builds/runtime-boundary-<version+build>.log" \
  --automated-gates-result "run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-<version+build>.log"
```

When the handoff, runtime-boundary, and release-gate command outputs have
already been saved, pass their traceable references to
`create_qa_build_record.py` with `--release-handoff-result`,
`--runtime-boundary-result`, and `--automated-gates-result` so the Final Release
Decision section starts with those evidence links already filled.
The handoff, runtime-boundary log, and release-gates log commands refuse to
overwrite existing saved evidence files unless `--force` is provided.
Release handoff outputs saved directly under `docs/qa-builds/` must use a
`handoff-*.md` filename; other Markdown names are rejected so the directory
validator cannot mistake the handoff plan for a completed signed-build QA
record.

Use `--scope android` or `--scope ios` when Android and iOS evidence are captured
separately. The command starts from `qa_build_record_template.md` and saves the
record under `docs/qa-builds/`; see `docs/qa-builds/README.md` for naming and
redaction rules.
For Android-only internal QA, the status, handoff, and preflight commands do not
need Apple Team ID or export method values:

```bash
python3 tool/release_status_report.py --scope android
python3 tool/release_handoff.py --version <version+build> --scope android --output docs/qa-builds/handoff-android-<version+build>.md
python3 tool/qa_preflight.py --scope android
python3 tool/build_android_release.py --artifact apk --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"
python3 tool/signed_artifact_evidence.py android <signed-release.apk-or-aab> --record-dir docs/qa-builds --version <version+build> --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"
```

For iOS-only internal QA, keep the Apple Team ID and export method on the iOS
commands:

```bash
python3 tool/release_status_report.py --scope ios --team-id <APPLE_TEAM_ID> --export-method development
python3 tool/release_handoff.py --version <version+build> --scope ios --team-id <APPLE_TEAM_ID> --export-method development --output docs/qa-builds/handoff-ios-<version+build>.md
python3 tool/qa_preflight.py --scope ios --team-id <APPLE_TEAM_ID> --export-method development
python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development
python3 tool/signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive" --team-id <APPLE_TEAM_ID> --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds
```

After completing the record, run:

```bash
python3 tool/validate_qa_build_record.py docs/qa-builds/<record>.md
python3 tool/qa_build_record_report.py docs/qa-builds/<record>.md
python3 tool/qa_release_evidence_links.py docs/qa-builds --update-release-evidence
python3 tool/validate_qa_build_records_dir.py docs/qa-builds
python3 tool/verify_final_release_evidence.py docs/qa-builds --scope android-ios --log docs/qa-builds/final-release-evidence-<version+build>.log
```

For Android-only or iOS-only internal QA, pass the same scope to the link updater
directory validator, and final verifier, for example
`python3 tool/qa_release_evidence_links.py docs/qa-builds --update-release-evidence --scope android`
followed by
`python3 tool/validate_qa_build_records_dir.py docs/qa-builds --scope android`
and
`python3 tool/verify_final_release_evidence.py docs/qa-builds --scope android --log docs/qa-builds/final-release-evidence-android-<version+build>.log`.

Attach the passing record to `release_evidence.md` after both the individual
record check and directory check pass. Before release approval, the final
evidence verifier must also pass with validated Android and iOS signed-build
records present, and its saved final-release-evidence log should be attached or
referenced with the QA evidence. Once validated QA records exist, replace
`<version+build>` with the validated QA record version/build; successful final
evidence logs must use that same version/build in the
`final-release-evidence*.log` filename.
The completed record's Final Release Decision must include:
- `Release handoff result`
- `Runtime boundary verification result`
- `Automated release gates result`

Each field must use traceable output paths, command transcripts, or log
attachment IDs.

Manual evidence fields must include a concise auditable note, traceable evidence
filename or attachment ID, device log, or task/result identifier. Placeholder
values such as `ok`, `yes`, or `done` are not accepted by the validator.

## Build Record

```text
QA build:
  Date:
  Git commit:
  Tester:
  MaClaw account:
  HubCenter candidates:
  Selected HubCenter URL:
  Discovered Hub URL:
  Tenant ID:
  LLM access mode:
  Desktop GUI QR authorization ID:

Android:
  Artifact path:
  SHA256:
  Version/build number:
  Signing identity: release alias, SHA fingerprint, upload key, or certificate ID
  Installer channel:
  Device model / OS:

iOS:
  Archive/TestFlight build:
  Runner bundle id:
  Share Extension bundle id:
  Team ID:
  Provisioning profiles: Runner and Share Extension profile UUID/file/name
  Device model / OS:
```

## Android Signed Build

- Copy `android/key.properties.example` to local `android/key.properties`
  before building the signed package: `storeFile`, `storePassword`, `keyAlias`,
  and `keyPassword` are required. Keep `android/key.properties`, `.jks`, and
  `.keystore` files out of git.
- To write the local config without putting passwords in shell history, set
  `MACLAW_ANDROID_STORE_FILE`, `MACLAW_ANDROID_STORE_PASSWORD`,
  `MACLAW_ANDROID_KEY_ALIAS`, and `MACLAW_ANDROID_KEY_PASSWORD`, then run
  `python3 tool/setup_android_signing.py`.
- Confirm a release build without `android/key.properties` fails instead of
  using the debug signing key.
- Validate signing inputs without building:
  `python3 tool/build_android_release.py --artifact apk --build-name <app-version> --build-number <build-number> --dry-run`.
- Build the signed QA artifact with
  `python3 tool/build_android_release.py --artifact apk --build-name <app-version> --build-number <build-number>`
  or `python3 tool/build_android_release.py --artifact appbundle --build-name <app-version> --build-number <build-number>`;
  record the printed artifact path, size, SHA256, version/build number, signing
  identity, and installer channel in the QA build record.
- Generate paste-ready QA artifact fields with
  `python3 tool/signed_artifact_evidence.py android <signed-release.apk-or-aab> --record-dir docs/qa-builds --version <version+build> --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"`.
  Paste the generated `Artifact path`, `SHA256`, byte size, and optional build
  metadata into the Android Signed Build section. Keep the signed artifact at
  the generated path, relative to `docs/qa-builds`, until
  `python3 tool/validate_qa_build_record.py docs/qa-builds/<record>.md` passes;
  the validator checks both local artifact existence and SHA256.
- Install the signed APK/AAB on at least one Android 13+ device.
- Open MaClaw Mobile and confirm the account screen shows the selected
  HubCenter, discovered Hub, tenant, and LLM access mode; record the selected
  Hub and tenant shown on screen.
- Confirm there is no custom Hub URL setting or settings surface.
- Record installer channel, version/build number, artifact SHA256, and install
  result with app launch/open evidence. Installer channel should name a
  non-debug Android distribution path such as internal app sharing, Play
  internal testing, Firebase App Distribution, MDM, or an enterprise
  distribution channel.

## Android Share-To-App

For each item, share from another app into MaClaw Mobile and record whether the
app opens the assistant or document flow as expected.

| Payload | Expected result | Evidence |
| --- | --- | --- |
| Plain text | Assistant opens with the shared text | Screenshot or note |
| URL | Assistant searches/summarizes and keeps the URL as a citation | Screenshot with citation |
| Image/photo | Document import task starts | Upload task ID naming image/photo |
| PDF | Document import task starts | Upload task ID naming PDF |
| Word `.docx` or `.doc` | Document import task starts | Upload task ID naming Word/docx/doc |
| Excel `.xlsx` or `.xls` | Document import task starts | Upload task ID naming Excel/xlsx/xls |
| CSV | Document import task starts | Upload task ID naming CSV |

## Android Runtime Permissions

- Notification permission can be requested from the account screen.
- Camera permission appears when using photo question/import.
- Microphone permission appears when using voice question.
- Media/file access works for document import and share-to-app.
- Local network/SSH scenario works when connecting to a local or private-network
  server, if that scenario is in scope for the build.
Record both the permission prompt/result and the feature scenario that triggered
it.

## iOS Signing And Share Extension

- Confirm Runner bundle id is `top.mypapers.maclaw.mobile`.
- Confirm Share Extension bundle id is
  `top.mypapers.maclaw.mobile.ShareExtension`.
- Confirm Runner and Share Extension use the official Team ID.
- Record Runner and Share Extension provisioning profile UUID, `.mobileprovision`
  file, or profile name.
- Confirm Runner and Share Extension both enable
  `group.top.mypapers.maclaw.mobile`.
- Confirm URL schemes include `maclaw` and
  `ShareMedia-$(PRODUCT_BUNDLE_IDENTIFIER)`.
- On macOS, create local export options from `ios/ExportOptions.plist.example`
  with
  `python3 tool/setup_ios_export_options.py --team-id <APPLE_TEAM_ID> --export-method development`.
- On macOS, plan the signed archive/export command with
  `python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development`
  or `--export-method app-store` for TestFlight/App Store Connect. Record the
  printed bundle IDs, app group, Team ID, archive/export command context,
  `.xcarchive` path or TestFlight build number, and Runner/Share Extension
  provisioning profile evidence.
- After the signed archive/TestFlight build exists, run
  `python3 tool/signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive" --team-id <APPLE_TEAM_ID> --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds`
  or pass the same provisioning profile summary to
  `python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method <export-method> --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds`
  and paste the generated archive/build, Team ID, and provisioning-profile
  fields into the QA build record.
- Install via development signing or TestFlight, launch/open the app, and
  record the build number plus screenshot or recording evidence.

## iOS Share-To-App

For each item, share from another app into MaClaw Mobile and record whether the
app opens the assistant or document flow as expected.

| Payload | Expected result | Evidence |
| --- | --- | --- |
| Plain text | Assistant opens with the shared text | Screenshot or note |
| URL | Assistant searches/summarizes and keeps the URL as a citation | Screenshot with citation |
| Image/photo | Document import task starts | Upload task ID naming image/photo |
| PDF | Document import task starts | Upload task ID naming PDF |
| Word `.docx` or `.doc` | Document import task starts | Upload task ID naming Word/docx/doc |
| Excel `.xlsx` or `.xls` | Document import task starts | Upload task ID naming Excel/xlsx/xls |
| CSV | Document import task starts | Upload task ID naming CSV |

## iOS Runtime Permissions

- Camera permission appears when using photo question/import.
- Microphone permission appears when using voice question.
- Speech recognition permission appears when using voice question.
- Photo library permission appears when importing from photos.
- Local network permission appears when connecting to a local/private server.
- Notification permission can be requested from the account screen.
Record both the permission prompt/result and the feature scenario that triggered
it.

## Hub Discovery And Service Smoke Test

- Login with an official MaClaw account through HubCenter and record the
  authenticated mobile session result.
- Confirm the app probes the three preset HubCenters:
  `https://hubs.mypapers.top`, `https://hubs.maclaw.top`, and
  `https://hubs2.maclaw.top`; record all three candidate URLs and the selected
  HubCenter.
- Confirm the selected HubCenter discovers the user's Hub URL and tenant ID;
  record both values.
- Confirm LLM mode is MaClaw official by default, or record the MaClaw desktop
  GUI QR authorization ID when third-party LLM access is enabled; record which
  mode was used.
- Confirm the LLM setup screen only exposes MaClaw official service redemption
  code entry and MaClaw desktop GUI QR authorization. Record that arbitrary
  third-party endpoint/base URL/provider URL/API key fields are not present.
- Confirm bootstrap returns user, quota/limits, feature flags, and service
  status; record all four categories in the QA build record.
- Run an AI search with citations and record the query plus at least one
  visible HTTPS source URL.
- Ask one assistant question by voice and record the recognized transcript;
  ask one photo/image/screenshot assistant question or import and record the
  resulting search citation answer or document upload task ID.
- Share the search result and record the target or output, such as Mail,
  WeChat, system share sheet, clipboard, exported file, or saved local path.
- Create document drafts from the search result for every first-version
  template: notice, report, email, proposal, meeting minutes, and statement.
- Upload a document and record the document upload/import task ID.
- Export a document to PDF, Word, and Markdown; record one matching export job
  ID for each format.
- Download or save each exported PDF, Word, and Markdown file, then share the
  exported files through the system share sheet or record the saved local path.
- Submit a digital employee task and record task ID plus status polling result,
  including the same task ID and observed queued/running/done/failed state.
- Confirm realtime updates arrive for document or digital employee task status,
  and record the WebSocket/event/update evidence with the matching task or job
  ID.
- Confirm notifications are delivered for document/export completion, digital
  employee task completion, and SSH abnormal/disconnect scenarios; record the
  typed notification payloads or tap/open targets, including a document
  `document-export:`/`document-draft:`/`document-upload:` payload, a
  `digital-employee-task:` payload, and a `server-profile:` payload.
- Confirm all API/service and realtime surfaces use the discovered Hub URL.
- Toggle offline/poor-network conditions or otherwise block HubCenter access,
  record the offline warning, restore connectivity, and confirm search,
  document export, digital employee status, and realtime surfaces recover.

## Account Privacy And Local Data

- Change theme and speech language from the account/settings screen and record
  the before/after result.
- Clear local work records and confirm search history, document drafts/tasks,
  command history, digital employee prompts/tasks, and app preferences reset.
- After clearing local work records, confirm server profiles and SSH
  credentials remain available.
- Clear server profiles/SSH credentials through the separate explicit account
  action and confirm saved server access is revoked.

## Manual SSH Smoke Test

- Add a server profile with host, port, username, tag, and note.
- Test password auth or private-key auth, depending on the QA server.
- Connect and run a read-only command such as `whoami` or `uptime`.
- Disconnect and reconnect.
- Copy terminal output.
- In the QA record, capture action-specific evidence for host type, auth mode,
  connect result, read-only command, command output excerpt, disconnect result,
  reconnect result, and copied output evidence.
- Send copied output to AI analysis after reviewing the preview and
  sensitive-data warning; record both the preview confirmation and warning
  evidence.
- Confirm AI returns explanation and command drafts, with manual confirmation
  or not-auto-executed evidence.
- Hand pasted output to a digital employee after the Hub/tenant handoff
  warning and confirmation, if digital employee access is enabled; record that
  the SSH terminal or pasted/copied output was the handoff content.
- Delete the server profile and confirm stored password/private-key credentials
  are cleared from secure storage.

## Evidence Package

Attach or link:

- Signed Android artifact and SHA256.
- iOS archive/TestFlight build identifier.
- Device model and OS version for every device used.
- Screenshots or recordings for permission prompts and share-to-app flows.
- MaClaw account, selected HubCenter, discovered Hub, tenant, and LLM mode used
  for smoke test.
- Search query, upload task IDs, export job IDs, digital employee task IDs.
- Voice transcript and photo/image assistant input result, including citation
  answer evidence or document upload task ID.
- Account theme/speech language change, local work-record reset, retained
  server credentials after local reset, and separate server credential clearing.
- SSH host type, auth mode, connect/disconnect/reconnect results, read-only
  command, command output excerpt, copied output evidence, and credential
  deletion confirmation.
- Any known issues or waivers approved for the build.
- Release approval must be recorded by an approver different from the tester.
