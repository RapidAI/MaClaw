# MaClaw Mobile Release Evidence

Use this file with `docs/release_checklist.md`. The checklist says what must be
true before release; this file records what can be proven automatically and what
still requires signed builds or real devices.

For the requirement-by-requirement status, use `docs/release_audit.md`.
For signed-build and real-device QA steps, use `docs/qa_device_checklist.md`.
For each signed QA build, create a validator-named record with
`python3 tool/create_qa_build_record.py --scope android-ios --version <version+build>`
or copy `docs/qa_build_record_template.md` manually, then attach the completed
evidence record here after it passes
`python3 tool/validate_qa_build_record.py docs/qa-builds/<record>.md`. Store
records under `docs/qa-builds/`; see `docs/qa-builds/README.md` for naming and
redaction rules. While a record is still incomplete, use
`python3 tool/qa_build_record_report.py docs/qa-builds/<record>.md` to print a
grouped gap report for missing evidence, invalid values, filename issues, and
local artifact hash mismatches. After records validate, run
`python3 tool/qa_release_evidence_links.py docs/qa-builds` and add the generated
Markdown links to this file, or run
`python3 tool/qa_release_evidence_links.py docs/qa-builds --update-release-evidence`
after the records validate. For scoped internal QA packages, pass the same
platform scope to the link updater, such as
`python3 tool/qa_release_evidence_links.py docs/qa-builds --update-release-evidence --scope android`
or
`python3 tool/qa_release_evidence_links.py docs/qa-builds --update-release-evidence --scope ios`.

## Automated Gates

Run these before handing a build to QA:

```bash
go test ./hub/internal/httpapi -run "TestMobile.*" -count=1
go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemptionRouteIsNotExposed|DesktopQRSessionRouteIsNotExposed)|TestSameURLOriginHandlesDefaultPorts" -count=1
go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown|TestResolveMobileBackendSSHHost|TestMobileServerProfilesFromSSHHosts|TestProcessMobileBackendSSHSession" -count=1
cd mobile/maclaw_mobile
python3 -m unittest tool/configure_platforms_test.py
python3 -m unittest tool/validate_qa_build_record_test.py
python3 -m unittest tool/create_qa_build_record_test.py
python3 -m unittest tool/validate_qa_build_records_dir_test.py
python3 -m unittest tool/qa_build_record_report_test.py
python3 -m unittest tool/qa_release_evidence_links_test.py
python3 -m unittest tool/qa_preflight_test.py
python3 -m unittest tool/release_evidence_commands_test.py
python3 -m unittest tool/setup_android_signing_test.py
python3 -m unittest tool/release_status_report_test.py
python3 -m unittest tool/release_handoff_test.py
python3 tool/validate_qa_build_records_dir.py docs/qa-builds
python3 -m unittest tool/verify_runtime_boundary_test.py
python3 -m unittest tool/run_release_gates_test.py
python3 -m unittest tool/verify_debug_apk_evidence_test.py
python3 -m unittest tool/update_debug_apk_evidence_test.py
python3 -m unittest tool/signed_artifact_evidence_test.py
python3 -m unittest tool/verify_manual_release_gates_test.py
python3 tool/verify_manual_release_gates.py
python3 -m unittest tool/verify_final_release_evidence_test.py
python3 -m unittest tool/verify_android_release_signing_test.py
python3 -m unittest tool/build_android_release_test.py
python3 -m unittest tool/verify_ios_wrapper_test.py
python3 -m unittest tool/plan_ios_release_test.py
python3 -m unittest tool/setup_ios_export_options_test.py
flutter test test/release_docs_test.dart --concurrency=1 --reporter compact
flutter create --platforms android,ios .
python3 tool/configure_platforms.py
python3 tool/verify_android_release_signing.py
python3 tool/verify_ios_wrapper.py
python3 tool/verify_runtime_boundary.py
flutter pub get
flutter analyze
flutter test --concurrency=1
flutter build apk --debug
```

The mobile CI workflow runs the same gates in `.github/workflows/maclaw-mobile.yml`
and uploads the debug APK as the `maclaw-mobile-debug-apk` artifact.
For local release preparation, `python3 tool/run_release_gates.py` runs the same
automated gate sequence end to end; run
`python3 tool/run_release_gates.py --dry-run` first to review the numbered gate
order and total count without starting Go, Flutter, or Android build commands.
For signed-build QA, run `python3 tool/run_release_gates.py --log docs/qa-builds/release-gates-<version+build>.log`
so the completed QA record can reference the saved automated-gate transcript.
Run `python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --log docs/qa-builds/preflight-<version+build>.log`
before signed-build QA so the current preflight result is saved as evidence.
Run `python3 tool/verify_runtime_boundary.py --log docs/qa-builds/runtime-boundary-<version+build>.log`
for the matching runtime-boundary evidence file.
The latest completed local `python tool\run_release_gates.py` run on 2026-07-10 passed all 38 automated release gates, including Flutter analysis and the full Flutter test suite. The subsequent locale-coverage test added one Flutter test;
the current standalone full Flutter suite passes 348 tests, while the saved
38-gate transcript below is the preceding 339-test snapshot. The gate run
included
suite, runtime boundary verification, native wrapper regeneration/configuration,
Go mobile API tests, QA build record scaffold tests, QA record directory
validation, QA build record gap report tests, QA release evidence link helper
tests, QA preflight helper tests, release evidence command helper tests, Android
signing setup helper tests, release status report tests, QA/debug/final evidence
verifier tests, manual release gate verification, Android release signing
verification, Android release build helper tests, iOS wrapper verification, iOS
release plan helper tests, iOS export options setup helper tests, and Android
debug APK build. The local transcript was saved under `docs/qa-builds/` as
`release-gates-notification-pending-final-20260710.log`;
attach the versioned `release-gates-<version+build>.log`
from signed-build QA as external evidence when preparing a release package.
After a local debug APK build, run `python3 tool/update_debug_apk_evidence.py`
to refresh the artifact path, byte size, and SHA256 recorded below, then run
`python3 tool/verify_debug_apk_evidence.py` to confirm the evidence still
matches the current local `app-debug.apk`.
Before final release approval with completed signed-build QA records, run
`python3 tool/verify_final_release_evidence.py docs/qa-builds --scope android-ios --log docs/qa-builds/final-release-evidence-<version+build>.log`
to require validated Android and iOS evidence records for the same
version/build, require this file to link every validated QA record by filename,
require those guarded Markdown link labels to contain the validated QA record
filename, not generic labels such as `Completed QA record`, and save the
success or failure transcript as release evidence; successful final
evidence logs must use that same version/build in the
`final-release-evidence*.log` filename, and existing logs require `--force`
before they can be overwritten.
The current continuation run is intentionally still blocked before signed-build
QA records exist; the latest iOS preflight blocker transcript is saved as
`docs/qa-builds/preflight-ios-current-20260711.log`, and the latest final
evidence verifier failure transcript is saved as
`docs/qa-builds/final-release-evidence-0.1.0+1-latest-20260710.log`. It confirms
that final release evidence still requires at least one completed signed-build
QA record.

The latest Android-only final evidence verifier transcript is
`docs/qa-builds/final-release-evidence-android-current-final.log`. It reaches
the same conclusion: signing, automated checks, artifact evidence, and local
runtime boundaries are valid, but release approval remains blocked until the
incomplete Android QA record is replaced or completed with real device, Hub,
notification, document, and GUI/agent-managed SSH evidence.

The current operator handoff plan is saved as
`docs/qa-builds/handoff-android-0.1.0+1-current.md`. It is intentionally marked
`NOT READY` and contains the exact next commands and evidence fields for the
real-device/Hub/GUI-agent QA session; creating the handoff file does not count
as a completed signed-build QA record.

The latest local continuation audit also passed the full Flutter suite (348
tests), the Python release-tool suite (642 tests), and the targeted Hub mobile
API suite (`go test ./hub/internal/httpapi -run
'TestMobile(Bootstrap|Search|BackendSSH|RealtimeBackendSSH|SSHAnalyze)'
-count=1`). `flutter analyze --no-fatal-infos` and the runtime-boundary verifier
also pass. The Android release-status and preflight reruns remain intentionally
`NOT READY`/`BLOCKED` because the existing signed-build QA record still lacks
real-device and backend-session evidence; no release approval is implied by
these local checks.

The latest stage-wide rerun passed the same full Flutter suite (`348` tests),
the aggregate Python release-tool suite (`642` tests), and Flutter static
analysis with no issues. This is the current automated baseline after the
Realtime recovery, artifact evidence, iOS preflight, and SSH documentation
updates.
- The current saved transcripts are
  `docs/qa-builds/flutter-full-current-20260711.log` and
  `docs/qa-builds/python-release-tools-current-20260711.log`; they independently
  reproduce the same `348` Flutter tests and `642` Python tests with exit code
  zero.

The mobile runtime TODO/placeholder audit found no unimplemented feature
branches in `lib/`; remaining nullable returns are input validation, optional
state, or empty-state rendering paths. The next unfinished items are therefore
manual release gates rather than local implementation stubs.

The startup route audit also passed: loading session state renders the MaClaw
logo Splash, signed-out state renders phone registration without HubCenter
candidate URLs, signed-in official-LLM state opens the AI assistant, and an
unconfigured LLM routes to the dedicated LLM setup surface. Shared links/documents
remain pending until authentication and are then consumed by their intended
assistant/document flow.

The current `flutter test test/app_smoke_test.dart --concurrency=1` run passed
all `15` app smoke tests, including phone-registration routing, signed-in
official-assistant routing, missing-LLM configuration routing, pre-login
shared-link/document/notification deferral, post-login consumption, and
notification recovery to the assistant, documents, employees, and servers
tabs.

- Realtime recovery now refreshes the signed-in Bootstrap and invalidates
  document, digital-employee, and backend SSH session/task caches when Hub
  sends a `ready` frame after a WebSocket reconnect. This closes the local
  stale-snapshot path when the socket is interrupted while the network remains
  online; coverage is in `test/mobile_realtime_bridge_test.dart`, while real
  device weak-network and Hub delivery evidence remains a manual gate.
- The Realtime bridge regression now also closes the first stream, waits for
  the configured reconnect delay, receives a second `ready` frame, and verifies
  that Hub state refresh runs twice. The default production delay remains five
  seconds; tests inject a zero delay so the reconnect path is deterministic.
- The current document workflow regression passed `33` tests across
  `test/documents_screen_test.dart` and `test/documents_state_test.dart`.
  It covers PDF/Word/Markdown export requests, polling and realtime completion,
  failed export/import retry, safe local filenames, export notifications,
  downloaded-file sharing, and sensitive-title/message redaction. This is
  client-side contract evidence; real Hub export job IDs and physical-device
  share delivery remain manual gates.

## Resolved Automated Test Residuals

- Flutter widget tests previously emitted a Drift debug-only warning about
  multiple `_MobileSqliteDatabase` instances when several provider scopes
  opened local-store history concurrently during the same test process.
  `MobileLocalStore` now gates concurrent initial opens through a shared future,
  and digital-employee widget tests override unrelated local history providers.
  The current full `flutter test --concurrency=1` run passes without the Drift
  warning.

## Current Automated Coverage

| Area | Evidence |
| --- | --- |
| Official HubCenter discovery only | `test/official_service_test.dart`, `test/official_service_surface_test.dart`, `test/auth_service_test.dart`, `test/mobile_realtime_client_test.dart`, `go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemptionRouteIsNotExposed|DesktopQRSessionRouteIsNotExposed)|TestSameURLOriginHandlesDefaultPorts"` |
| Mobile API contracts | `test/mobile_api_contract_test.dart`, `go test ./hub/internal/httpapi -run "TestMobile.*"` |
| Native Android/iOS wrapper settings | `tool/configure_platforms_test.py`, `test/platform_permissions_test.dart` |
| Runtime boundary: no embedded Go `corelib`, FFI, gomobile, native corelib bridge, phone-local SSH dependency, terminal emulator dependency, phone-side SSH credential save/read API, custom Hub URL configuration, redemption-code login, or arbitrary third-party LLM provider/base URL/API-key fields | `tool/verify_runtime_boundary.py`, `tool/verify_runtime_boundary_test.py`, `test/release_docs_test.dart`; signed-build QA can save `--log` output as evidence |
| Signed-build QA record completeness | `tool/validate_qa_build_record.py`, `tool/validate_qa_build_record_test.py`, `docs/qa_build_record_template.md` |
| Signed-build QA preflight, release status, release handoff, record scaffold, gap report, release evidence command helper, release evidence link helper, and directory validation | `tool/release_status_report.py`, `tool/release_status_report_test.py`, `tool/release_handoff.py`, `tool/release_handoff_test.py`, `tool/qa_preflight.py`, `tool/qa_preflight_test.py`, `tool/release_evidence_commands.py`, `tool/release_evidence_commands_test.py`, `tool/create_qa_build_record.py`, `tool/create_qa_build_record_test.py`, `tool/qa_build_record_report.py`, `tool/qa_build_record_report_test.py`, `tool/qa_release_evidence_links.py`, `tool/qa_release_evidence_links_test.py`, `tool/validate_qa_build_records_dir.py`, `tool/validate_qa_build_records_dir_test.py`, `docs/qa-builds/README.md` |
| Automated gate sequence integrity | `tool/run_release_gates.py`, `tool/run_release_gates_test.py` |
| Local debug APK evidence freshness | `tool/verify_debug_apk_evidence.py`, `tool/verify_debug_apk_evidence_test.py`, `tool/update_debug_apk_evidence.py`, `tool/update_debug_apk_evidence_test.py` |
| Signed artifact evidence snippet generation | `tool/signed_artifact_evidence.py`, `tool/signed_artifact_evidence_test.py`, `docs/qa_build_record_template.md`, `docs/qa_device_checklist.md` |
| Android release signing safety and local signed build helper | `tool/setup_android_signing.py`, `tool/setup_android_signing_test.py`, `tool/verify_android_release_signing.py`, `tool/verify_android_release_signing_test.py`, `tool/build_android_release.py`, `tool/build_android_release_test.py`, `android/app/build.gradle.kts`, `android/key.properties.example`, `.gitignore` |
| iOS wrapper, Share Extension target/Pod wiring, export options, and archive planning | `tool/verify_ios_wrapper.py`, `tool/verify_ios_wrapper_test.py`, `tool/setup_ios_export_options.py`, `tool/setup_ios_export_options_test.py`, `tool/plan_ios_release.py`, `tool/plan_ios_release_test.py`, `ios/ExportOptions.plist.example`, `ios/Podfile`, `ios/Runner.xcodeproj/project.pbxproj`, `ios/Runner/Info.plist`, `ios/ShareExtension/Info.plist`, `ios/Runner/Runner.entitlements`, `ios/ShareExtension/ShareExtension.entitlements` |
| Manual release gate documentation parity | `tool/verify_manual_release_gates.py`, `tool/verify_manual_release_gates_test.py`, `docs/release_audit.md`, `docs/release_evidence.md`, `docs/qa_device_checklist.md`, `docs/qa_build_record_template.md` |
| Final signed-build evidence package readiness | `tool/verify_final_release_evidence.py`, `tool/verify_final_release_evidence_test.py`, `docs/release_evidence.md`, `docs/qa-builds/README.md`, `docs/qa_device_checklist.md`; final verification rechecks copied backend session output for the GUI/agent evidence line, rejects raw secrets in that copied output evidence even when the QA directory result is already marked valid, and points backend-session final-layer failures back to `qa_build_record_report.py` for record-level remediation |
| GUI-like chat window, user/assistant multi-turn context, multi-tab conversations, voice input, quick prompts, citations, redacted shared links/text, photo/file handoff | `test/assistant_screen_test.dart`, `test/api_client_test.dart`, `test/assistant_retry_test.dart`, `test/mobile_shared_intent_test.dart` |
| Mobile app shell tabs, feature-flag routing, readable navigation labels, and shared-intent route fallback | `test/mobile_feature_flags_test.dart`, `test/app_smoke_test.dart`, `test/mobile_shared_intent_test.dart` |
| Emergency document templates, import, AI actions, edit helpers, API-boundary redaction, export/share UI | `test/documents_screen_test.dart`, `test/documents_state_test.dart`, `test/document_draft_test.dart` |
| GUI-equivalent backend SSH session management, Hub-synced sanitized desktop server metadata, GUI/agent-bound `backend_session_id`, SSH realtime incremental output evidence through `ssh_session` `output_chunk`/`output_seq` events, phone-initiated interrupt evidence through a Hub control record or `/api/mobile/ssh/sessions/{session_id}/interrupt` with GUI/agent Ctrl+C handling, copied backend session output with a GUI/agent evidence line containing actual values for Hub session ID, `backend_session_id`, concrete `claimed_by` worker identity such as `claimed_by desktop-agent-1`, and numeric `output_seq`, AI analysis and AI/digital-employee handoff evidence tied to the same GUI/agent-bound `backend_session_id`, high-risk command confirmation, readable safety warnings | `test/servers_screen_test.dart`, `test/servers_controller_test.dart`, `test/backend_ssh_command_test.dart`, `test/ssh_risk_test.dart`, `test/secure_vault_test.dart`, `test/mobile_realtime_client_test.dart`, `test/mobile_realtime_bridge_test.dart`, `go test ./hub/internal/httpapi -run "TestMobile.*(SSH|BackendSSH|RealtimeBackendSSH)" -count=1`, `go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown|TestResolveMobileBackendSSHHost|TestMobileServerProfilesFromSSHHosts|TestProcessMobileBackendSSHSession"` |
| Digital employee task submission, redacted mobile prompt handoff, authorization messaging, result copy/share, document draft creation | `test/digital_employees_screen_test.dart`, `test/digital_employee_test.dart`, `test/digital_employees_controller_test.dart`, `go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown|TestResolveMobileBackendSSHHost|TestMobileServerProfilesFromSSHHosts|TestProcessMobileBackendSSHSession"` |
| Account settings, notification request entry, cache clearing, credential separation | `test/account_screen_test.dart`, `test/app_preferences_test.dart`, `test/mobile_notification_service_test.dart`, `test/mobile_local_store_test.dart`, `test/secure_vault_test.dart` |
| Realtime document/digital employee updates | `test/mobile_realtime_client_test.dart`, `test/mobile_realtime_bridge_test.dart` |

## Latest Local Verification

2026-07-10:

- Removed the legacy public HubCenter mobile service-redemption route. Mobile
  account registration and login now have no server-side redemption-code route;
  phone/SMS verification remains the only public mobile entry path.
- Added regression coverage that both the legacy service-redemption and desktop
  QR mobile-login routes return `404`.
- Verified:
  - `go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemptionRouteIsNotExposed|DesktopQRSessionRouteIsNotExposed)|TestSameURLOriginHandlesDefaultPorts" -count=1`
  - `python -m unittest tool/run_release_gates_test.py`
  - `flutter test test/release_docs_test.dart --concurrency=1 --reporter compact`
- Built a local internal-QA Android Release APK `0.1.0+1` with the generated
  non-debug QA certificate. `apksigner` verified APK Signature Scheme v2;
  artifact SHA256 is
  `236465c58d338f4d2714f4d38cce70b4412017e4f565148bd97667b60976a130`.
  This is an internal build only and does not replace the production signing
  or real-device QA gate.
- Started the API 35 AVD `maclaw-api35`, installed the Release APK with
  `adb install --no-streaming`, and verified the app process and MaClaw
  package/activity. The current startup path does not request notification
  permission before login; after clearing app data the UI hierarchy showed the
  MaClaw logo and phone registration screen immediately, without exposing
  HubCenter candidate addresses or Flutter branding. Notification permission
  remains an explicit Account-screen action.
- Sent an Android `ACTION_SEND` text intent and verified that the system share
  resolver listed `MaClaw Mobile`. Full share-to-app payload handling and
  microphone/camera prompts still require a signed-build QA record with a
  real-device workflow and Hub account.
- On the API 35 Android emulator, queried the installed Release APK for both
  `ACTION_SEND` and `ACTION_SEND_MULTIPLE`. MaClaw Mobile was registered for
  all required types: plain text, image, PDF, Word, Excel, and CSV MIME types.
- Captured the latest signed Android API 35 emulator evidence under
  `docs/qa-builds/android-api35-0.1.0+1/`: cold-start screenshot/UI dump,
  package dump, and text/PDF/CSV share activity queries. The attachment records
  the APK SHA256 and explicitly remains emulator evidence, not a physical-device
  signed QA approval.
- Refreshed signed Android API 35 cold-start evidence after the login copy
  change as `startup-login-latest.png` and `startup-login-latest-ui.xml`.
  The hierarchy contains the MaClaw logo, phone registration/login, and official
  service wording, while containing no HubCenter candidate address or Flutter
  branding.
- Added assistant media-picker failure handling for camera, gallery, and file
  selection errors. The UI now gives an actionable permission/retry message and
  keeps the typed assistant composer usable; the camera failure path is covered
  by `test/assistant_screen_test.dart`.
- Reinstalled the latest signed Android API 35 APK after the media-picker
  change and captured `startup-media-handling-latest.png` plus
  `startup-media-handling-latest-ui.xml`; cold-start assertions passed for the
  MaClaw logo and phone login screen with no HubCenter candidate or Flutter
  branding.
- Queried the latest signed APK on the API 35 emulator for all six required
  share MIME registrations: plain text, image, PDF, Word, Excel, and CSV.
  Each query resolved the MaClaw Mobile package; these are emulator intent
  registration checks and do not replace real-device share workflow evidence.
- Reinstalled the latest signed APK after adding the document list insertion
  shortcut and captured `startup-document-list-latest.png` plus its UI dump;
  MaClaw cold-start and no-HubCenter assertions passed again.
- Remote maintenance command submission and GUI/agent background-task requests
  now catch Hub failures, preserve the session output panel, and expose a
  retryable error state instead of allowing a network exception to escape the
  screen.
- Reinstalled the latest signed APK after the remote-maintenance error handling
  change and captured `startup-remote-command-latest.png` plus its UI dump;
  the MaClaw phone-login cold-start assertions passed.
- Rebuilt the signed APK after adding document-page file/camera/gallery picker
  failure feedback and preserving the current draft/task state.
- Added typed `assistant-task:` notifications for assistant requests that take
  at least ten seconds and return an LLM request ID; notification taps route to
  the AI assistant without exposing the answer body in the system tray.
- Reinstalled the latest signed APK after the assistant notification change and
  captured `startup-assistant-notification-latest.png` plus its UI dump; the
  MaClaw phone-login cold-start assertions passed.
- Sanitized the HubCenter unavailable exception and login error messages so
  failed phone registration never renders preset HubCenter URLs. Added service
  and Widget regression coverage for this boundary.
- Sanitized desktop GUI QR authorization failures so upstream Hub URLs are not
  rendered in the optional third-party LLM settings flow; added a pure-function
  regression test while retaining scanner and paste Widget coverage.
- Reinstalled the signed APK again on the API 35 emulator and captured
  `signed-startup-login-latest.png` plus `signed-startup-login-latest-ui.xml`.
  The installed package was `top.mypapers.maclaw.mobile`, version `0.1.0`, and
  the hierarchy contained the MaClaw logo and phone registration/login entry
  without HubCenter candidate text or Flutter branding. This remains emulator
  preparation evidence and does not close the Android physical-device gate.
- Built the current signed Android `0.1.0+1` APK from the latest source with
  `tool/build_android_release.py`; the artifact is
  `build/app/outputs/flutter-apk/app-release.apk` and its SHA256 is
  `dc31f0b93c7620eeedb57714780ad244d110cab7d67df3fda2ec3c83892f9d6b`.
  `tool/signed_artifact_evidence.py` recorded the artifact using the Google
  Play internal testing channel label. This is a locally signed artifact and
  does not claim Play upload or physical-device approval.
- Installed that current signed APK on the API 35 emulator after clearing app
  data and captured `android-api35-0.1.0+1/signed-release-latest/startup.png`
  plus `startup.xml`. The UI dump contains `MaClaw`, `手机号注册/登录`, and the
  official-service copy, with no HubCenter candidate address or Flutter
  branding. This remains emulator evidence only.
- Reinstalled the current signed APK after notification-provider failure
  isolation was added; the refreshed `signed-release-latest` package dump,
  startup UI dump, screenshot, and `apksigner-verify.txt` correspond to the
  `dc31f0b93c7620eeedb57714780ad244d110cab7d67df3fda2ec3c83892f9d6b`
  artifact. The cold-start assertions still pass, and the evidence remains
  emulator-only.
- Rebuilt the signed APK/AAB after the AppShell pending-document lifecycle fix.
  The current artifacts are `build/app/outputs/flutter-apk/app-release.apk`
  (SHA256 `8c71972cd5ea4b336ede1f21b8141606c51c9a112289f1e4c44021d1e1eaa70f`)
  and `build/app/outputs/bundle/release/app-release.aab` (SHA256
  `4884b7ef6e23e2d5e0ffa2cec69f032928f4a44e759c4c2bbe818b20f1fc1d94`),
  with separate evidence under
  `android-api35-0.1.0+1/signed-release-stateful-latest/`. APK v2 and AAB
  `jarsigner` verification both pass. This is still local signed-build
  preparation and does not claim real-device QA.
- Installed that signed APK on the API 35 emulator successfully and refreshed
  `signed-release-stateful-latest/startup.png`, `startup.xml`, and
  `package-dump.txt`. The startup hierarchy contains MaClaw branding and no
  HubCenter preset URL or Flutter branding; signed text/PDF/CSV and
  `SEND_MULTIPLE` CSV resolver queries all resolve the MaClaw activity. This
  remains signed-emulator evidence only.
- Sent an explicit `ACTION_SEND` text Intent with a URL directly to the current
  signed `MainActivity`; Android reported `Status: ok`, `LaunchState: COLD`,
  and the captured hierarchy retained MaClaw branding without HubCenter or
  Flutter branding. The delivery transcript and capture are retained as
  `signed-release-stateful-latest/share-text-delivery.txt/.png/.xml`.
- Queried the current signed APK on API 35 for Android `ACTION_SEND` and
  `ACTION_SEND_MULTIPLE` registrations covering text, image, PDF, Word, Excel,
  and CSV. Every query resolved `top.mypapers.maclaw.mobile`; the outputs are
  retained under `android-api35-0.1.0+1/signed-release-latest/`. These are
  signed-emulator intent-registration checks and do not replace real-device
  share workflow evidence.
- Built the matching signed Android `0.1.0+1` App Bundle with
  `tool/build_android_release.py`; the artifact is
  `build/app/outputs/bundle/release/app-release.aab`, size `57480413` bytes,
  SHA256 `7ec9d6996dc8165149ce6637a228793b79d28c9b324f8c21d2d7646a3d493bd7`.
  `jarsigner -verify` passed and its transcript is retained as
  `signed-release-latest/aab-jarsigner-verify.txt`. APK/AAB artifact evidence
  transcripts are also retained as `apk-artifact-evidence.txt` and
  `aab-artifact-evidence.txt`. This is local signed distribution preparation
  only; no Play upload or device approval is claimed.
- Rebuilt `flutter build appbundle --release` from the current workspace and
  rechecked the same AAB SHA256 (`7ec9d6996dc8165149ce6637a228793b79d28c9b324f8c21d2d7646a3d493bd7`);
  `jarsigner -verify` returned exit code 0. This confirms the current release
  configuration still produces the recorded signed bundle.
- Verified the same APK with Android build-tools `36.0.0` `apksigner`: one
  signer is present and APK Signature Scheme v2 verification succeeds. The
  package dump and verifier output are retained beside the signed startup
  capture as `package-dump.txt` and `apksigner-verify.txt`.
- Rebuilt the signed Android APK after the QR authorization error-redaction
  change, reinstalled it on API 35, and captured
  `signed-startup-qr-redaction-latest.png` plus its UI dump. The new artifact
  SHA256 is `236465c58d338f4d2714f4d38cce70b4412017e4f565148bd97667b60976a130`;
  startup assertions passed and the evidence remains emulator-only.
- Refreshed the signed APK API 35 share-registration queries after that rebuild:
  `ACTION_SEND` for text, image, PDF, Word, Excel, and CSV plus
  `ACTION_SEND_MULTIPLE` for CSV all included
  `top.mypapers.maclaw.mobile/.MainActivity`. These are MIME registration
  checks on the emulator; physical-device share workflows remain manual gates.
- Ran the actual text `ACTION_SEND` flow with the rebuilt signed APK: Android
  Resolver displayed `Share with MaClaw Mobile`, `Just once` was selected, and
  the intent was delivered to the MaClaw activity. The delivered UI hierarchy
  returned to the phone-login screen with the MaClaw logo and no HubCenter or
  Flutter branding. Captures are
  `share-text-signed-latest-resolver.*` and
  `share-text-signed-latest-delivered.*` under the Android QA directory.
- Live HubCenter route audit found that the deployed official route is
  `/api/entry/resolve`, while the mobile client had incorrectly called the
  Hub-only `/api/entry/probe` route. The client now calls `/api/entry/resolve`;
  the default official endpoint no longer returns the previous route `404`.
  Authentication tests now assert the corrected path and retain Hub/tenant
  resolution coverage.
- A read-only probe of all three preset official HubCenters was recorded in
  `docs/qa-builds/hubcenter-discovery-readonly-20260710.log`. The default
  `https://hubs.mypapers.top` returned HTTP 200 with an online Hub and tenant;
  `https://hubs.maclaw.top` returned 502 and `https://hubs2.maclaw.top` reset
  the connection. The probe used a synthetic phone identity and did not send
  SMS or create an account, so it does not replace the real Hub/SMS smoke gate.
- A follow-up probe found that the default HubCenter can return duplicate online
  entries for the same Hub with the incomplete entry first. The mobile login
  selector now chooses the most complete entry for the preferred Hub ID, keeping
  tenant metadata for the SMS request; this is covered by the live response-shape
  regression test.
- Read-only live probe of `https://hubs.mypapers.top/api/entry/resolve` with a
  synthetic phone identity returned HTTP `200`, an online default Hub
  (`https://hub.mypapers.top`), and tenant routing data. No SMS send request was
  made during this probe.
- Added client-side selection of HubCenter's `default_hub_id` before the online
  list fallback, with regression coverage for multi-Hub tenant ordering.
- Added bounded HubCenter connect/send/receive timeouts (8s/8s/15s) so a
  stalled official candidate does not block fallback to the remaining preset
  endpoints; custom Dio timeout values remain respected.
- Added the concrete iOS `ShareExtension` app-extension target, Runner PlugIns
  embedding phase, target signing/resource settings, and Podfile dependency for
  `receive_sharing_intent`; Xcode signing and real-device share QA remain manual.
- HubCenter discovery now continues after candidate HTTP 404/408 responses,
  with a phone-login regression test for an unavailable preset route.
- Phone registration now enforces the Hub SMS resend cooldown in the mobile UI:
  after a code is sent, the resend action is disabled for 60 seconds and shows
  the remaining time, with lifecycle-safe timer cleanup and widget coverage.
- Official mobile LLM responses now expose a read-only `llm_usage_record_id`
  correlation field next to `llm_request_id`; the assistant displays this trace
  reference for official-credit calls, while third-party GUI QR mode does not
  fabricate an official usage record.
- Notification startup now injects the initialized notification service into
  the Riverpod scope used by the app shell, so taps on document, digital
  employee, and server task notifications can be routed to the matching tab.
- Backend SSH `ssh_file_operation` realtime events are now dispatched into a
  per-session mobile cache and surfaced as recent operation status on the
  server maintenance screen; request responses enter the same cache.
- Network recovery also invalidates backend SSH task and file-operation caches
  after the signed-in tenant bootstrap refresh, preventing stale pre-outage
  control state from being presented after Hub connectivity returns.
- App foreground resume now reuses the same signed-in Hub recovery path, so
  returning from background refreshes the tenant bootstrap and reconnects the
  mobile realtime bridge without starting recovery work on the login screen.
- Re-ran `python tool/run_release_gates.py` after the protocol and timeout
  changes: all 38 automated release gates passed, including the full 323-test
  Flutter suite and Android Debug APK build.

2026-07-05:

- Desktop GUI LLM QR Hub boundary:
  - Updated mobile QR session bootstrap so unauthenticated desktop GUI QR
    connection always posts to the selected official HubCenter endpoint instead
    of directly trusting `hub_url` embedded in the QR payload.
  - Updated authenticated third-party LLM QR authorization so a QR payload that
    declares `hub_url` must match the current discovered Hub origin before the
    mobile client sends the authorization request.
  - Implemented the HubCenter mobile QR endpoint so it verifies the QR
    `hub_url` against registered online Hubs before proxying the original
    payload to that Hub's one-time mobile consume endpoint.
  - Hardened HubCenter QR proxy errors so upstream Hub response bodies are not
    echoed back to the mobile app, and normalized same-origin matching across
    explicit default ports such as `https://host` and `https://host:443`.
  - Hardened HubCenter QR proxy success responses so `hub_id` and `hub_url`
    are always overwritten with the verified registered Hub identity before
    returning data to the mobile app.
  - Aligned HubCenter QR bootstrap with the mobile runtime boundary by
    accepting only HTTPS registered Hub origins for QR session proxying.
  - Added regression coverage that an arbitrary QR `hub_url` is not used as a
    direct mobile endpoint and that a different authenticated Hub URL is
    rejected before request dispatch. HubCenter coverage now also proves
    unregistered QR Hub URLs are rejected before proxy dispatch and that
    upstream consume failures do not leak Hub response bodies.
- Verified:
  - `go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemptionRouteIsNotExposed|DesktopQRSessionRouteIsNotExposed)|TestSameURLOriginHandlesDefaultPorts" -count=1`
  - `flutter test test\auth_service_test.dart test\api_client_test.dart test\login_screen_test.dart test\account_screen_test.dart --concurrency=1 --reporter compact`
  - `flutter analyze`

2026-07-02:

- Local environment:
  - Configured `JAVA_HOME`, `ANDROID_HOME`, `ANDROID_SDK_ROOT`, Android SDK
    tools, and `D:\flutter\bin` in the PowerShell session.
  - `flutter --version`: Flutter 3.41.5 stable, Dart 3.11.3.
- `flutter pub get`
  - Initially failed because `speech_to_text ^6.6.2` conflicted with
    `flutter_secure_storage ^9.2.2` through incompatible transitive `js`
    constraints.
  - Fixed by moving `speech_to_text` to `^7.4.0`; `flutter pub get` then
    passed and refreshed dependency resolution.
- `.github/workflows/maclaw-mobile.yml`
  - Confirmed the workflow runs the same automated mobile gates listed above.
  - Added `workflow_dispatch` and the workflow file itself to the path filters
    so release engineers can run the mobile gate manually and CI reruns when
    the mobile gate definition changes.
  - Uploads `build/app/outputs/flutter-apk/app-debug.apk` as the
    `maclaw-mobile-debug-apk` workflow artifact after `flutter build apk
    --debug`.
- `docs/release_checklist.md`
  - Aligned signed Android and signed iOS share-to-app requirements with the
    QA checklist and release audit: text, URLs, images, PDFs, Word, Excel, and
    CSV files are all explicit manual release payloads.
  - Aligned the CI command order with `tool/run_release_gates.py` so the
    checklist, local runner, and mobile CI workflow describe the same sequence.
  - Added the mobile Hub smoke workflow items now required by the QA validator:
    voice transcript input, photo/image/screenshot assistant input, cited
    answer or document task evidence, document/export/digital-employee/SSH
    abnormal notifications, and offline/weak-network recovery.
- `docs/release_audit.md`
  - Added requirement-by-requirement release status for service, Android, iOS,
    user workflows, safety, CI/build gates, and remaining manual blockers.
  - Tracks mobile-specific partial gates for voice/photo assistant input,
    notification delivery, and offline/weak-network recovery, with validator
    coverage plus required real-device or real-Hub smoke evidence.
  - Checked that referenced checklist, evidence, tests, and platform scripts
    exist in the current workspace.
- `docs/qa_device_checklist.md`
  - Added signed Android/iOS, share-to-app, runtime permissions, official
    service smoke, GUI-equivalent backend SSH session management smoke, and evidence-package steps for QA.
  - Checked that the checklist includes text, URL, image, PDF, Word, Excel,
    CSV, runtime permissions, Hub discovery smoke, GUI-equivalent backend SSH session management smoke,
    selected HubCenter, discovered Hub, tenant, LLM mode, Share Extension
    bundle ID, and app group.
- `docs/qa_build_record_template.md`
  - Added a per-build evidence template for signed Android/iOS artifacts,
    share-to-app payloads, runtime permissions, Hub discovery smoke,
    realtime status, GUI-equivalent backend SSH session management smoke, server-profile cache clearing, and final release
    approval.
  - Documents the `docs/qa-builds/` storage location and validator-enforced
    record filename format so copied QA templates keep the same directory
    contract as `docs/qa-builds/README.md`.
- `docs/qa-builds/README.md`
  - Added the signed-build QA record directory instructions, including record
    naming, validator command, and sensitive-data redaction rules.
  - Documents backend SSH evidence as MaClaw GUI-equivalent backend session
    management, not a phone-local SSH client check; copied backend-session
    output must include actual Hub session ID, GUI/agent-bound
    `backend_session_id`, concrete `claimed_by` worker identity such as
    `claimed_by desktop-agent-1`, and numeric `output_seq`.
  - Documented that completed records and attachments are ignored by git by
    default, while README.md remains tracked as the directory contract; the
    release docs test verifies the ignore rule appears before the README
    exception.
- `tool/validate_qa_build_record.py`
  - Added a release QA record validator for completed signed-build evidence.
  - Rejects missing files, directories, non-Markdown files, `README.md`, and
    `qa_build_record_template.md` before field validation so QA instructions and
    templates cannot be mistaken for completed signed-build records.
  - Enforces `docs/qa-builds/` record filenames in
    `YYYY-MM-DD-<android|ios|android-ios>-<version+build>.md` form and checks
    that the filename date and version/build match the completed record fields.
  - Rejects high-confidence raw secret leakage in QA records, including private
    key blocks, `password=`/`token=`/`api_key=` assignments, Authorization
    Bearer/Basic headers, Cookie/Set-Cookie/PRIVATE-TOKEN/X-API-Key headers, and
    common literal API token/JWT/cloud access key ID/Google API key formats,
    plus URLs with embedded credentials; records should use redacted evidence or
    attachment IDs.
  - Checks that required manual-gate fields are filled, that the selected
    HubCenter is one of the three official presets, that the candidate list
    contains exactly the three official HubCenters, and that Hub/API/realtime
    URLs are HTTPS.
  - Verifies that `Discovered Hub URL` is a tenant Hub URL rather than one of
    the official HubCenter URLs.
  - Rejects placeholder build identity, branch, signing, installer, tenant,
    account, tester, Flutter version, and approver values, requires a trackable
    git branch name plus Flutter SDK semver, and requires the MaClaw Mobile
    phone account plus tenant ID to be trackable identifiers so signed-build
    records remain auditable.
  - Requires duplicated Android/iOS share and permission fields, such as
    `CSV`, to be filled once per platform in Android-then-iOS order rather than
    accepting a single shared value or ambiguous platform evidence.
  - Rejects extra filled entries for required fields, so a signed-build record
    cannot contain conflicting duplicate values for fields that must be single
    or exactly one per platform.
  - Requires completed records to include auditable release handoff, runtime
    boundary verification, and automated release gate results before final
    manual gate decisions.
  - QA record template now explains that those fields should reference handoff
    output, runtime-boundary command output, full gate count, and log or
    attachment IDs.
  - QA checklist, QA build records README, and release checklist now also state
    that completed records must fill those three final-decision evidence fields
    before approval.
  - Rejects invalid duplicate entries for formatted fields instead of allowing
    one valid value to mask a bad repeated value.
  - Validates fixed evidence formats and identities: Android SHA256 must be a
    64-character hex digest, Runner bundle ID must be
    `top.mypapers.maclaw.mobile`, Share Extension bundle ID must be
    `top.mypapers.maclaw.mobile.ShareExtension`, app group must be
    `group.top.mypapers.maclaw.mobile`, and API/realtime base URL confirmation
    must use HTTPS URLs on the discovered Hub origin.
  - Requires Android `Artifact path` to point to a signed `.apk` or `.aab`.
    Debug artifact paths such as `app-debug.apk` are rejected; when the artifact
    is available locally, the validator recomputes SHA256 and rejects records
    whose digest does not match the file.
  - Requires Android `Signing identity` to identify a non-debug release,
    internal, upload, keystore, or certificate signing identity with a
    trackable alias, SHA fingerprint, upload key, or certificate ID.
  - Requires Android `Installer channel` to identify a non-debug Android
    distribution path such as internal app sharing, Play internal testing,
    Firebase App Distribution, MDM, or enterprise distribution; iOS-only
    channels such as TestFlight and App Store Connect are rejected there.
  - Requires Android and iOS signed install results to describe both successful
    install and app launch/open evidence, and to match the recorded platform
    instead of accepting Android/iOS install evidence in the wrong section.
  - Requires iOS signed-build evidence to name an `.xcarchive` path or an
    explicit `TestFlight build <number>`, a 10-character Apple Team ID, and
    provisioning profile UUID/file/name evidence for both Runner and Share
    Extension.
  - Enforces LLM mode consistency: official MaClaw LLM records must mark the
    desktop GUI QR authorization as `not-used-official-mode` and prove LLM
    calls after SMS verification use the recorded `phone:<digits>` account's
    official credits, while
    `desktop_qr_third_party` records must include a real trackable MaClaw
    desktop GUI QR authorization ID instead of the official-mode
    `not-used-official-mode` sentinel, and the LLM access evidence must match
    the selected mode and recorded Tenant ID.
  - Requires manual device evidence fields, including install results, share
    payloads, permissions, Hub smoke, and GUI-equivalent backend SSH session management smoke results, to contain
    auditable notes instead of placeholders such as `ok`, `yes`, or `done`.
  - Requires GUI-equivalent backend SSH session management smoke fields to describe the specific tested
    action: host type, auth mode, session creation/attach, connect result,
    read-only command, command output
    excerpt, disconnect result, reconnect result, and copied backend session
    output evidence.
  - Requires GUI-equivalent backend SSH session management smoke evidence to reference the recorded
    `server-profile:<id>` notification payload when server abnormal
    notification evidence is present.
  - Requires SSH AI analysis evidence to mention preview confirmation, the
    sensitive-data warning, and redacted/masked/sanitized backend session/log
    output before AI analysis; requires AI result evidence to include
    explanation, `command-draft:<id>`, manual/not-auto-executed proof, and
    redacted backend SSH session/log output context; and
    requires backend SSH cache-clear evidence to mention phone-side
    server-profile cache clearing.
  - Requires status polling and realtime update evidence to identify task/job or
    document status changes and reference a recorded task/job ID, and requires
    digital employee handoff warnings to mention Hub/tenant confirmation plus
    backend SSH session or pasted/copied backend session output context when
    recorded.
  - Requires notification delivery evidence for document/export completion,
    digital employee task completion, and SSH abnormal/disconnect scenarios,
    including typed payload or tap/open target proof for document,
    `digital-employee-task:`, and `server-profile:` targets, and requires the
    notification evidence to reference recorded document export job and digital
    employee task IDs plus redacted/masked/sanitized notification message
    preview proof.
  - Requires network offline/recovery evidence to show HubCenter/network
    unavailable warnings and restored assistant online, document, digital
    employee, or realtime service behavior after connectivity returns, with a
    trackable network-recovery/connectivity-probe/retry/incident trace ID.
  - Requires account-screen, no-custom-Hub-URL, and bootstrap smoke evidence to
    explicitly mention selected Hub, tenant, absent custom Hub URL settings,
    user/quota, feature flags, and service status, with account-screen Hub and
    tenant evidence matching the recorded Discovered Hub URL and Tenant ID.
  - Requires login evidence to prove MaClaw Mobile's phone-number-only path:
    HubCenter-mediated SMS verification and official credits bound to the phone
    account with `sms-verification:<id>`, including the first LLM call after
    verification using that phone account's MaClaw official credits.
  - Requires official LLM access evidence to include a request/log/usage record
    proving the post-verification LLM call was charged to the recorded
    `phone:<digits>` MaClaw official credits account.
  - Requires account privacy and local-data evidence to record theme/speech
    language changes, local work-record clearing, retained sanitized
    server-profile metadata after local reset, and separate explicit
    phone-side server-profile cache clearing with `server-profile-cache-clear:<id>`,
    tied to any recorded `server-profile:<id>` notification payload.
  - Requires HubCenter probe evidence to list the exact three official
    HubCenters, discovered Hub evidence to include Hub URL plus tenant, and LLM
    evidence to identify MaClaw official access or desktop GUI QR third-party
    authorization.
  - Requires LLM setup surface evidence to prove mobile starts from phone
    registration/login, keeps third-party LLM authorization in account/settings
    through MaClaw desktop GUI QR, and exposes no arbitrary third-party
    endpoint, base URL, provider URL, API key, or redemption-code login fields.
  - Requires document upload task IDs to identify document upload/import tasks,
    and PDF/Word/Markdown export job IDs to match the requested export format.
  - Requires exported document share evidence to prove PDF, Word, and Markdown
    exports were downloaded/saved and handed to a share target or local path,
    to reference the recorded PDF, Word, and Markdown export job IDs, and to
    prove exported document previews were redacted/masked/sanitized before
    external sharing or saving with `redaction-check:<id>`.
  - Requires share-to-app payload evidence to describe the expected mobile
    routing: shared text/URLs open the assistant with citations where
    applicable, while files/images enter document import/upload tasks with the
    expected payload format named.
  - Requires AI citation smoke evidence to include a visible HTTPS source URL
    from the answer/result citations area, not only a backend/API log URL,
    and shared-result evidence to name the share target or concrete output such
    as Mail, WeChat, clipboard, exported file, or saved local path, and to
    reference a recorded citation URL plus redacted/masked/sanitized shared
    answer/result preview proof with `redaction-check:<id>`.
  - Requires mobile assistant input evidence to prove both voice transcription
    and photo/image assistant input, with a resulting cited answer or document
    upload task ID, and to reference a recorded citation URL or document upload
    task ID.
  - Requires assistant-result document draft evidence to cover every first-version
    template: notice, report, email, proposal, meeting minutes, and statement,
    to reference a recorded citation URL, and to include the resulting
    `document-draft:<id>`.
  - Requires runtime permission fields to describe prompt/result evidence and
    the matching feature scenario, including camera/photo and photo-library
    permission evidence tied to real photo/image/screenshot assistant input,
    microphone and speech-recognition permission evidence tied to voice
    assistant question transcription, media/file import, platform local-network
    prompt if applicable to Hub/backend-session operation, or
    notification permission evidence tied to real document export, digital
    employee, or SSH abnormal notification delivery/open flows; media/file
    access evidence must prove real file picker or share-to-app document
    import/upload coverage for PDF, Word, Excel, CSV, and image/photo payloads;
    local-network permission evidence must not be used as phone-local SSH proof
    or as a substitute for GUI/agent-bound `backend_session_id` evidence; every
    runtime permission evidence line must include a trackable
    `permission-grant:<id>` record.
  - Requires signed-build device evidence to name device model plus Android/iOS
    OS version, with both Android and iOS devices represented and at least one
    Android 13+ device recorded.
  - Requires the first `Device model / OS` entry to be the Android QA device and
    the second entry to be the iOS QA device, matching the QA template sections.
  - Validates optional attachment, known issue/waiver, and digital employee
    handoff warning evidence when those optional QA fields are used; handoff
    warnings must identify Hub/tenant confirmation and backend SSH session or
    pasted/copied backend session output context, while attachment evidence fields must
    reference a traceable evidence file or attachment ID.
  - Requires `Date` and `Approval date` to be valid `YYYY-MM-DD` calendar
    dates that are not in the future, requires `Approval date` to be on or
    after `Date`, requires `Approved by` to be different from `Tester`,
    requires `Git commit` to be a 7-40 character hexadecimal commit SHA, and
    document/export/digital employee task IDs to be trackable values rather
    than placeholders such as `ok`; digital employee task IDs must identify
    digital employee tasks rather than generic task IDs, include Hub/tenant/LLM
    credits/manual-confirmation context, and match the recorded selected
    HubCenter, discovered Hub, Tenant ID, and MaClaw phone-account credits.
  - Requires final release decision fields to explicitly say passed or waived
    instead of accepting ambiguous values such as `ok` or `yes`; waiver
    decisions must include a reason and a `Known issues / waivers` summary
    that names each waived final gate and includes a trackable waiver
    ticket/issue/approval reference.
  - Rejects negative final-decision phrases such as `not passed`, instead of
    treating a nested `passed` word as approval.
  - Verifies UTF-8 Chinese approval/waiver wording (`通过`, `已通过`, `批准`,
    `豁免`) without accepting corrupted text.
  - The Chinese approval/waiver test cases use Unicode escape literals for
    `已通过` and `豁免`, avoiding Windows console mojibake in the test source.
- `tool/create_qa_build_record.py`
  - Added a scaffold command for signed-build QA records so QA can generate a
    validator-named file from `docs/qa_build_record_template.md` before device
    testing starts.
  - Prefills `Date` and `Version/build number`, supports `--scope android`,
    `--scope ios`, and `--scope android-ios`, and refuses to overwrite an
    existing record unless `--force` is provided.
  - Prints the follow-up `tool/validate_qa_build_record.py` command so the
    generated file moves directly into the evidence validation path after QA
    fills it.
  - Prints a manual-evidence reminder before validation so generated records are
    not mistaken for complete evidence until real-device share/permission, Hub
    discovery with post-SMS-verification official credits LLM proof, concrete
    `llm-request-id` and `llm-usage-record`, notification, and
    backend-managed SSH GUI/agent claim or worker handoff plus `ssh_session`
    `output_chunk`/`output_seq` proof,
    not-phone-local/ad hoc terminal evidence rejection, and phone-initiated
    interrupt evidence through a Hub control record or `/api/mobile/ssh/sessions/{session_id}/interrupt` showing GUI/agent Ctrl+C handling are filled in.
- `tool/qa_build_record_report.py`
  - Added a grouped gap report for in-progress signed-build QA records.
  - Reuses `tool/validate_qa_build_record.py` validation rules and reports
    path/filename issues, secret redaction failures, missing or invalid
    evidence fields, and local artifact hash mismatches without relaxing final
    release evidence requirements.
  - GUI-equivalent backend SSH session management smoke hints now call out not-phone-local/ad hoc terminal
    evidence rejection, phone-initiated interrupt evidence through a Hub control record or interrupt endpoint, and GUI/agent
    Ctrl+C handling, so incomplete records do not look like phone-local SSH
    client smoke tests.
- `tool/qa_release_evidence_links.py`
  - Added a release evidence link helper that prints Markdown links only for
    QA build records that already pass directory validation.
  - Reports invalid records separately so incomplete signed-build evidence is
    not accidentally linked into the final evidence package.
- `tool/qa_preflight.py`
  - Added a local preflight summary before signed-build QA.
  - Checks Android release signing guardrails, local Android signing inputs,
    iOS wrapper/Share Extension wiring, readable local iOS export options with
    a valid Team ID/export method, existing QA build record validity, and the
    final release-evidence link step.
  - Reports when release audit, QA checklist, QA record template, final
    evidence log command, QA record link block, scoped internal QA commands,
    and QA record validation/redaction rules are aligned.
  - Supports optional `--team-id` and `--export-method` arguments to verify
    local `ios/ExportOptions.plist` exactly matches the intended iOS signed
    build before archive planning.
  - The helper is intentionally documented as a local QA command, while CI runs
    its unit tests instead of requiring private signing keys.
- `tool/setup_android_signing.py`
  - Added a local Android signing setup helper that writes ignored
    `android/key.properties` from environment variables.
  - Requires `MACLAW_ANDROID_STORE_FILE`, `MACLAW_ANDROID_STORE_PASSWORD`,
    `MACLAW_ANDROID_KEY_ALIAS`, and `MACLAW_ANDROID_KEY_PASSWORD`, validates the
    referenced keystore exists, and rejects debug keystore filenames.
- `tool/setup_ios_export_options.py`
  - Added a macOS preparation helper that writes ignored
    `ios/ExportOptions.plist` from `ios/ExportOptions.plist.example`.
  - Validates Apple Team ID format, export method, automatic signing style, and
    generates the local plist used by the Xcode export command.
- `tool/release_status_report.py`
  - Added a single local readiness summary that combines preflight, QA record
    directory validation, and final release evidence verification.
  - Reports current blockers and next actions without redefining the underlying
    release gates.
  - Supports optional iOS Team ID/export method inputs and passes them through
    to preflight so release status can catch mismatched `ios/ExportOptions.plist`
    before handoff or archive planning.
  - Supports `--log` evidence capture with existing-log overwrite protection
    and explicit `--force` replacement, matching the preflight,
    runtime-boundary, and final-evidence log helpers.
- `tool/release_handoff.py`
  - Added a signed-build operator handoff generator that turns the current
    readiness state into concrete setup, build, QA record, and evidence-link
    commands without inventing QA evidence.
  - Passes its required Team ID and export method into release status preflight,
    so handoff output reflects mismatched local iOS export options immediately.
  - The generated command sequence also runs `release_status_report.py` with
    the same Team ID/export method before setup/build steps, keeping copied
    operator commands aligned with the target signed build.
  - QA device checklist and `docs/qa-builds/README.md` now start signed-build
    QA with the handoff generator before preflight and record scaffolding.
  - Those QA entry points also require runtime-boundary verification and the
    full automated release gate runner before signed-build QA evidence is
    recorded.
  - Handoff evidence to attach now includes phone-initiated interrupt evidence
    through a Hub control record or `/api/mobile/ssh/sessions/{session_id}/interrupt` showing GUI/agent Ctrl+C handling for backend-managed SSH sessions.
  - Handoff evidence output now explicitly maps handoff transcript,
    runtime-boundary verifier output, and release-gate results to the QA record
    final decision fields.
- `tool/update_debug_apk_evidence.py`
  - Added a local helper to refresh the debug APK artifact path, byte size, and
    SHA256 fields in this evidence file before re-running
    `tool/verify_debug_apk_evidence.py`.
- `tool/signed_artifact_evidence.py`
  - Added a paste-ready signed-build evidence generator for Android APK/AAB
    `Artifact path`, `SHA256`, byte size, version/build, signing identity, and
    installer channel fields, plus iOS archive/TestFlight metadata snippets.
  - Release checklist and QA record template now include the matching iOS
    signed artifact evidence command for archive/TestFlight build, Team ID, and
    Runner/Share Extension provisioning-profile fields.
  - QA device checklist also includes the iOS signed artifact evidence command
    so real-device testers paste the same archive/TestFlight and provisioning
    fields into completed QA records.
- `tool/run_release_gates.py`
  - Added a local release-gate runner that executes the documented automated
    mobile gate sequence from the correct repository/mobile working
    directories.
  - Added `--log <path>` so dry-run and full gate runs can save the gate
    sequence, command output, and final pass message as QA evidence.
  - Release-gate evidence logs refuse to overwrite an existing saved log unless
    `--force` is provided.
  - Runs `tool/verify_runtime_boundary.py` after native wrapper configuration
    to reject accidental Go `corelib`, FFI, gomobile, or native corelib bridge
    additions in the mobile runtime.
  - Runtime-boundary verification also supports `--log <path>` so signed-build
    QA can attach the pass/fail transcript to completed records.
  - Runtime-boundary evidence logs refuse to overwrite an existing saved log
    unless `--force` is provided.
  - Includes the release-gate runner tests in the local sequence, so the
    end-to-end runner also verifies its own gate definition before native
    wrapper generation and Flutter gates.
  - Runs `test/release_docs_test.dart` as an explicit early gate before the
    full Flutter test suite, so release documentation drift fails with a focused
    message.
  - Runs the full Flutter test suite with `--concurrency=1`, matching the
    stable local verification mode used to avoid cross-test local-store
    contention during release preparation.
  - Exposes documented release-gate command strings from the runner definition
    so CI, checklist, and evidence command-order tests share one command source.
  - Provides numbered `--dry-run` output for checking the release gate order
    and total gate count without running Go, Flutter, or Android build
    commands.
- `flutter test test/release_docs_test.dart --concurrency=1 --reporter expanded`
  - Passed: 33 release documentation integrity tests.
  - Covers release doc cross-links, signed-build manual gates, required
    share/permission payloads, service/GUI-equivalent backend SSH session management smoke steps, and explicit remaining
    blockers.
  - Covers signed-QA command examples keeping local preflight before saved
    runtime-boundary and release-gates logs, matching the shared release
    evidence command helper.
  - Covers the QA build record template artifact-evidence order: Android/iOS
    integrated build/plan commands appear before standalone
    `signed_artifact_evidence.py` helpers for already-built artifacts.
  - Requires every Python unittest gate in `tool/run_release_gates.py` to have a
    matching release-evidence test-count entry, so gate additions cannot silently
    drift away from this document.
  - Locks GUI-equivalent backend SSH session management smoke documentation to action-specific evidence
    fields: host type, auth mode, session creation/attach, connect result,
    read-only command, command output
    excerpt, disconnect result, reconnect result, and copied backend session
    output evidence.
  - Covers the user guide's mobile product decisions: MaClaw logo startup,
    phone registration/login as the first unauthenticated screen, configured
    sessions opening the assistant, optional MaClaw desktop GUI QR authorization
    from account/settings, multi-tab assistant, official HubCenter discovery only,
    and no arbitrary third-party LLM endpoint setup.
  - Verifies the release documentation corpus preserves readable Chinese
    navigation labels and contains no known mojibake or replacement markers.
  - Verifies critical release tooling sources stay free of Unicode replacement
    markers so QA evidence parsing does not silently lose Chinese markers.
  - Verifies the mobile `.gitignore` excludes generated Flutter, Gradle,
    Kotlin, Android local-properties, iOS ephemeral/Pods, and IDE module
    artifacts so local release gates do not leave cache noise in the worktree.
  - Covers mobile CI manual triggering and workflow self-path coverage.
  - Verifies the release checklist CI command order matches the release gate
    runner sequence.
  - Verifies the automated gate command block in this evidence document matches
    the release gate runner sequence.
  - Covers native launch branding resources: Android launch backgrounds and
    Android 12 splash styles reference `@mipmap/launch_image`, iOS
    LaunchScreen references `LaunchImage`, and generated launch/app icon PNGs
    are non-empty MaClaw assets instead of Flutter placeholders. Also verifies
    native launch resource comments and iOS launch image notes describe MaClaw
    launch branding rather than Flutter template customization text.
  - Covers signed Android/iOS share-to-app payload completeness, including CSV.
  - Covers the full Hub discovery smoke manual gate, including voice/photo
    input, shared result, notification delivery, network recovery, API base
    URL, and realtime Hub URL evidence.
  - Verifies that local files referenced by the release docs and user guide,
    including docs, tests, tools, the QA build records README, and the mobile
    CI workflow, plus root `.gitignore`, root `pubspec.yaml`, and native
    wrapper paths, exist in the workspace.
  - Verifies this evidence file records the current release documentation test
    count from `test/release_docs_test.dart`.
  - Verifies this evidence file records the current Python release-tool unittest
    counts from the local test source files.
  - Verifies this evidence file records the aggregate Python release-tool
    unittest count from the local test source files.
  - Verifies the QA build record directory README documents record naming,
    validator usage, and sensitive-data redaction requirements.
  - Covers QA build record template links and manual-gate evidence fields.
- `flutter test test/documents_screen_test.dart --plain-name "documents screen explains ready export sharing" --concurrency=1 --reporter expanded`
  - Passed.
  - Covers ready document export download, generated local file path handoff,
    and system file-share invocation with the draft title.
- `flutter test test/documents_screen_test.dart --plain-name "documents screen redacts sensitive export share text" --concurrency=1 --reporter compact`
  - Passed.
  - Covers system share-sheet text redaction for exported document titles, so
    `token=` and `password=` title fragments are replaced before the share
    payload leaves the app surface.
- `flutter test test/official_service_test.dart test/official_service_surface_test.dart test/api_client_test.dart test/documents_state_test.dart --concurrency=1 --reporter compact`
  - Passed: 44 official service, API client, document state, and service
    surface tests.
  - Covers document export download URL safety: relative paths resolve against
    the discovered Hub, same-Hub absolute URLs are accepted, external absolute
    URLs are rejected before a request is sent, same host with the wrong scheme
    is rejected, and export-completion notifications use the typed
    `document-export:<job-id>` payload for task recovery.
  - Covers LLM service status path safety: relative paths remain Hub-scoped,
    same-Hub absolute URLs are accepted, and external absolute status URLs are
    rejected before a request is sent.
  - Covers document upload waiting for the signed-in session before enforcing
    the official mobile upload byte limit from bootstrap.
  - Covers failed-import retry being rejected unless the last upload task is
    actually failed and has a recoverable source path.
  - Covers absence of custom Hub URL configuration and redemption-code login
    surfaces in the mobile runtime.
  - Covers backend SSH session API requests for tenant-Hub session creation,
    command input, interrupt, reconnect, and close operations.
- `flutter test test/auth_service_test.dart test/api_client_test.dart test/login_screen_test.dart test/account_screen_test.dart --concurrency=1 --reporter compact`
  - Passed: 31 phone login, desktop GUI QR settings, API client, and account
    settings tests.
  - Covers phone-number login through HubCenter discovery, phone-account
    official credits identity, verified phone accounts using the official
    `phone:<digits>` credits account for MaClaw LLM access, formatted phone
    credits in Hub SMS verification results being normalized to
    `phone:<digits>` only when the value contains digits and phone separators,
    malformed `phone:` credits with letters staying untrusted, MaClaw desktop
    GUI QR session consumption, authenticated third-party LLM authorization
    on the discovered Hub, and client-side rejection of arbitrary endpoint URLs,
    raw API keys, legacy provider JSON, or malformed QR payloads before mobile
    attempts a service connection.
  - Covers client-side rejection of invalid phone numbers before HubCenter
    probe, Hub SMS send, or Hub verification requests are attempted.
- `go test ./hub/internal/httpapi -run "TestRegistrationSMSVerifyAndStart(WithMachineCredentialsBindsCurrentMachineUser|CreatesPhoneIdentity|BackfillsTenantScopedPhoneGrant|ContinuesWhenTenantBackfillFails|RebindsExistingPhoneIdentityToCanonicalUser)"`
  - Passed.
  - Covers Hub SMS verification success responses explicitly returning
    `credits_account: phone:<digits>` for both existing machine-user phone
    binding and new phone-identity enrollment, so mobile official LLM usage can
    be tied to the verified phone account.
- `flutter test test/documents_screen_test.dart --concurrency=1 --reporter expanded`
  - Passed: 14 document workflow widget tests.
  - Covers long-running import refresh, failed-import retry, and failed-export
    retry controls wiring through the mobile document UI, plus export file-share
    handoff and sensitive share-title redaction.
- `flutter test test/assistant_screen_test.dart --plain-name "assistant result can become every document template with citations" --concurrency=1 --reporter expanded`
  - Passed.
  - Covers turning an AI assistant result with citations into every mobile
    document template type: notice, report, email, proposal, meeting minutes,
    and statement.
- `flutter test test/assistant_screen_test.dart --concurrency=1 --reporter expanded`
  - Passed: 25 assistant workflow widget/model tests.
  - Covers AI assistant conversation, shared-link processing, multi-tab
    assistant state, voice/file/camera/gallery handoff, citation copy/share,
    creating document drafts from assistant results with redacted draft titles,
    history cleanup confirmation, and local assistant history redaction for both
    prompt text and answer previews.
- `flutter test test/mobile_shared_intent_test.dart --concurrency=1 --reporter expanded`
  - Passed: 16 shared-intent classification and controller tests.
  - Covers routing shared PDF, Word, Excel, and CSV files into the document
    import flow instead of the AI assistant text flow.
  - Covers redacting shared assistant text prompts, shared link URLs with
    embedded credentials, and shared link message secrets before they are handed
    to the AI assistant.
  - Covers file and image shares that include a URL in the platform message:
    the URL remains available as context, but the payload still enters document
    import instead of being misrouted to assistant conversation.
  - Covers unsupported shared attachments with a message or link falling back to
    assistant conversation, while mixed batches still prefer a later importable
    document over an unsupported attachment.
- `flutter test test/platform_permissions_test.dart --concurrency=1 --reporter expanded`
  - Passed: 4 platform wrapper tests.
  - Covers Android share MIME declarations and iOS Share Extension activation
    declarations for text, web URLs, files, and images.
  - Covers readable UTF-8 iOS privacy usage descriptions for camera,
    microphone, speech recognition, photo library, and local network access,
    rejects known mojibake/replacement markers, and verifies the iOS Runner
    bundle display/name does not fall back to the Flutter template name.
- `python -m unittest discover -s tool -p '*_test.py'`
  - Passed: 650 Python release tool tests.
  - Covers the aggregate local release-tool test suite, including release
    status, handoff, QA record validation/reporting/linking, signed artifact
    evidence, Android/iOS signing helpers, runtime-boundary verification, and
    release gate runner guard tests.
- `python tool\configure_platforms_test.py`
  - Passed: 21 platform configuration tests.
  - Covers cleanup of Flutter's generated widget-test template so
    native wrapper regeneration does not introduce stale `MyApp` analyzer
    failures, and cleanup of Flutter Android Gradle template comments after
    the official MaClaw Mobile package ID is applied.
  - Covers cleanup of Flutter template comments in Android plugin and
    debug/profile manifest scaffolding so regenerated wrappers keep MaClaw
    service-smoke wording.
  - Covers iOS Runner bundle display/name regeneration as `MaClaw Mobile`
    instead of the Flutter template name.
- `python -m unittest tool\validate_qa_build_record_test.py`
  - Passed: 236 QA record validator tests.
  - Covers incomplete template rejection, completed record acceptance,
    HubCenter discovery enforcement, exact HubCenter candidate enforcement,
    tenant Hub versus HubCenter URL separation, SSH realtime incremental output
    evidence requiring GUI/agent claim or worker handoff plus explicit worker
    claim/update evidence, rejecting generic mobile session API/input calls as
    worker proof, `ssh_session`, `output_chunk`, and `output_seq`,
    backend SSH connect/disconnect/reconnect evidence requiring Hub
    control-record lifecycle proof plus GUI/agent `SSHSessionManager`
    ownership,
    host/auth evidence requiring sanitized Hub-synced server-profile metadata
    without phone-side SSH credential storage,
    read-only command and command-output evidence requiring the same backend
    session/output context instead of generic terminal screenshots,
    copied backend session output evidence rejecting phone-local/ad hoc terminal
    copy context,
    rejection of phone-local/ad hoc terminal evidence for GUI-equivalent backend SSH session management smoke
    records, and phone-initiated interrupt evidence requiring Hub interrupt
    control-record or interrupt-endpoint proof plus GUI/agent Ctrl+C handling,
    rejection of deprecated SSH credential QA fields in favor of
    server-profile metadata/cache evidence,
    preflight evidence requiring `qa_preflight.py` READY output and a saved log,
    final decision artifact references matching the record `Version/build number`,
    duplicate Android/iOS field enforcement and platform order,
    required field overfill rejection,
    duplicate formatted-field rejection,
    fixed identity validation, LLM mode/QR/evidence consistency, Android artifact path validation, signed install/launch/platform evidence, debug artifact/signing/installer rejection, trackable signing alias/certificate evidence, local artifact
    SHA256 matching, signed install/app launch traceable visual evidence,
    iOS archive/TestFlight build identity, Team ID, and provisioning profile
    auditability including rejection of bare `UUID` words without actual profile
    IDs/files/names and acceptance of trackable UUID, `.mobileprovision`, or
    profile-name evidence, rejection of documented iOS archive/profile
    placeholder strings, build identity/branch/signing placeholder rejection,
    Android installer channel platform enforcement,
    git branch name format enforcement,
    Flutter SDK semver enforcement,
    MaClaw Mobile phone-account format, login-result account consistency,
    official LLM credits evidence bound to the recorded phone account with a
    request/log/usage record, Desktop GUI QR authorization ID linkage,
    and tenant ID format enforcement,
    API/realtime URL exact discovered-Hub-origin matching plus API
    client/realtime WebSocket evidence semantics,
    HubCenter login/probe/discovered Hub tenant/LLM access evidence and
    selected HubCenter/discovered Hub/Tenant ID consistency,
    account-screen/custom-Hub/bootstrap official-boundary evidence including
    recorded Discovered Hub URL, phone-account, and Tenant ID linkage,
    weak-network recovery evidence tied to the selected HubCenter, recorded
    discovered Hub URL, Tenant ID, recorded task/job IDs, and a trackable
    recovery trace ID,
    document upload/export format-specific task/job evidence, document upload
    task ID, document export job ID, and digital employee task ID linkage in
    status polling and realtime update evidence, exported
    document share evidence linked to recorded PDF/Word/Markdown export job
    IDs, and digital employee task ID semantics tied to recorded HubCenter,
    discovered Hub, Tenant ID, and MaClaw phone-account credits,
    AI assistant query evidence linked to recorded citation URLs, shared-result
    citation linkage and externalized-result redaction proof, voice-photo input
    result linkage plus recognized transcript composer/send linkage,
    document-draft citation and `document-draft:<id>` linkage
    smoke evidence semantics, manual and optional evidence placeholder rejection,
    rejection of legacy redemption-code LLM setup evidence,
    GUI-equivalent backend SSH session management smoke action-specific evidence validation, recorded
    server-profile ID linkage for GUI-equivalent backend SSH session management smoke and account privacy server-profile evidence,
    `server-profile-cache-clear:<id>` linkage for separate phone-side server-profile cache clearing,
    backend SSH AI analysis preview/sensitive-data warning/redacted-output evidence, `command-draft:<id>`/manual execution with redacted backend session output context, and backend SSH cache-clear evidence,
    status polling/realtime task update ID linkage, typed notification payload
    evidence tied to recorded document export and digital employee task IDs
    plus redacted notification message preview proof,
    and digital employee handoff warning evidence,
    runtime permission prompt/result and feature-scenario evidence validation
    including voice/photo assistant input linkage and task notification
    delivery/open linkage,
    iOS URL scheme evidence requiring both `maclaw` and `ShareMedia`,
    share-to-app expected-flow and payload-format evidence validation,
    Android/iOS device OS evidence validation including Android 13+ coverage and
    Android-then-iOS section order,
    QA template field coverage against required/optional validator fields plus
    required field occurrence counts, UTF-8 BOM handling for
    Windows-edited records, known field
    names containing URLs, README/template/directory/non-Markdown path
    rejection, `qa-builds` filename format/date/version matching including
    rejection of the legacy `ios-android` scope, raw secret
    redaction enforcement including Authorization Bearer/Basic headers and
    Cookie/Set-Cookie/PRIVATE-TOKEN/X-API-Key headers plus literal JWTs, cloud
    access key IDs, Google API keys, and URL embedded credentials, final
    decision passed/waived semantics, required
    Final Release Decision evidence matching the release handoff,
    runtime-boundary, and release-gates artifacts instead of generic screenshots,
    current automated release gate count enforcement, exact counted
    release-gate runner success-line acceptance, legacy uncounted success-line
    rejection, generic `all gates passed` rejection,
    independent approver enforcement, waiver reasons, per-gate waiver summaries,
    trackable waiver ticket/approval references, English
    waiver wording, UTF-8 Chinese approval/waiver wording (`通过`, `已通过`,
    `批准`, `豁免`), date ordering, date/commit/task ID auditability,
    version/build number format enforcement, rejection of unedited final-decision
    placeholders, CLI validation failure output, and raw-secret CLI failure
    messaging.
  - Covers scoped Android-only and iOS-only QA build records so validators do
    not require out-of-scope platform fields, artifact evidence, or dual-device
    entries when the filename scope is `android` or `ios`.
- `python -m unittest tool\create_qa_build_record_test.py`
  - Passed: 15 QA build record scaffold tests.
  - Covers validator-compatible record filenames, date/version prefilling,
    optional Final Release Decision evidence prefilling with the same
    handoff/preflight/runtime-boundary/release-gates artifact semantics enforced by the
    validator, overwrite protection, forced regeneration, invalid version/build
    rejection, and printed validation, gap-report, signed artifact evidence,
    manual-evidence reminder, and release evidence link update hints.
  - Covers scope-specific Android/iOS artifact evidence helper hints after QA
    record creation, so Android-only records do not prompt for iOS evidence.
  - Covers scoped record scaffolding that removes out-of-scope Android/iOS
    sections from generated records and prints the matching scoped
    `qa_release_evidence_links.py --update-release-evidence --scope ...`
    command.
  - Covers custom `--records-dir` scaffolding so follow-up signed artifact
    evidence and release-evidence link commands point at the actual generated
    record directory, while the default directory still prints short
    `docs/qa-builds` commands.
- `python -m unittest tool\validate_qa_build_records_dir_test.py`
  - Passed: 15 QA build records directory validator tests.
  - Covers scanning completed Markdown records under `docs/qa-builds/`,
    skipping `README.md`, ignoring non-Markdown evidence attachments, empty
    directories before signed QA records exist, missing/non-directory path
    rejection, release-handoff Markdown evidence attachment skipping for full
    Android/iOS and scoped Android-only/iOS-only handoff files, and per-record
    failure summaries.
  - Covers directory-validation success output that still points operators to
    `verify_final_release_evidence.py`, including an explicit warning that
    zero completed records do not prove final release readiness and the shared
    signed-QA-record next action for creating a completed record first in the
    same QA records directory being validated.
  - Covers validated-record success output that infers the record version/scope
    from validator-compatible filenames and prints a final verifier command with
    the matching saved `final-release-evidence*.log` path in the same QA
    records directory being validated.
  - Covers validated-record success output warning when records span multiple
    version/build values, with a single-version remediation hint before final
    evidence verification.
  - Covers explicit directory-validation `--scope` output that separates
    in-scope from out-of-scope valid records and uses only in-scope records to
    choose the final verifier log version, while pointing out-of-scope-only
    directories to the scoped signed-QA-record creation flow in the same QA
    records directory.
  - Covers scoped directory validation treating out-of-scope invalid records as
    ignored warnings while current-scope or unparseable invalid records still
    block and point to `qa_build_record_report.py`.
  - Covers failed directory validation pointing operators to
    `qa_build_record_report.py <failed-record>` for grouped gaps, redaction
    remediation, and signed artifact hints.
- `python -m unittest tool\qa_build_record_report_test.py`
  - Passed: 26 QA build record report tests.
  - Covers passing completed records, missing evidence summaries, invalid
    HubCenter values, filename errors, missing handoff/runtime-boundary/release
    gate evidence, local artifact SHA256 mismatches, missing local iOS
    archives, signed install traceable screenshot/recording hints, AI助手
    voice/photo composer, citation/upload, traceable visual evidence hints,
    runtime permission workflow hints, share-to-app payload hints, task-chain
    correlation hints for polling/realtime/notification evidence, backend SSH
    session smoke evidence hints, Hub/account/LLM setup hints, and CLI stdout/stderr behavior.
  - Covers release handoff evidence hints that include the required
    `--version <version+build>`, `--team-id <APPLE_TEAM_ID>`, and
    `--export-method <export-method>` inputs.
  - Covers validator-compatible Final Release Decision examples for
    handoff, runtime-boundary, and release-gates evidence when those fields are
    missing or malformed.
  - Covers local Android artifact and iOS archive failures pointing to the
    shared signed artifact evidence helper commands before release evidence
    linking.
  - Covers deriving the concrete version/build from the QA record filename when
    printing handoff, runtime-boundary, and release-gates evidence hints.
  - Covers that those gap-report hints use the shared release evidence command
    helper rather than duplicating Final Release Decision example strings.
  - Covers scoped report progress and artifact hints so iOS-only records do not
    request Android signed APK/AAB evidence, and Android-only records can use
    the same scoped validator path.
  - Covers scoped report next actions and release-handoff remediation hints so
    Android-only or iOS-only records point to matching scoped commands rather
    than the default full Android/iOS release flow.
  - Covers signed artifact gap hints that point missing Android/iOS artifact
    fields to the shared `signed_artifact_evidence.py` commands.
  - Covers secret redaction failure remediation that points QA operators to
    redacted evidence, attachment IDs, task IDs, artifact hashes, reviewer
    notes, and a follow-up `validate_qa_build_record.py` run.
  - Covers copied backend session output secret remediation that keeps the
    GUI/agent evidence line with real Hub session ID, `backend_session_id`,
    concrete `claimed_by` worker identity, and numeric `output_seq`, preserving that evidence line while replacing credentials or private customer excerpts with redacted text or a traceable attachment ID.
  - Covers deriving the QA records directory from the report target, so
    handoff, runtime-boundary, release-gates, signed artifact, validation, and
    release-evidence link hints stay in the same directory as the record being
    repaired.
  - Covers passing record next actions printing both the release-evidence link
    command and the matching final evidence verifier command with the record's
    concrete version/build and scope.
  - Covers release handoff evidence paths being rejected as non-QA-record
    inputs without mixing in missing evidence-field noise.
- `python -m unittest tool\qa_release_evidence_links_test.py`
  - Passed: 23 QA release evidence link helper tests.
  - Covers empty QA record directories, valid record Markdown link output,
    invalid record exclusion, invalid-record CLI failure behavior, and valid
    record CLI output, with deterministic filename ordering for generated QA
    record links.
  - Covers incomplete link readiness output: validated records with multiple
    version/build values or missing requested platform coverage are reported as
    found but not ready to link, so operators are not prompted to paste partial
    final evidence links by hand.
  - Covers ignoring release handoff evidence files, including scoped
    Android-only and iOS-only handoff files, so handoff Markdown is never linked
    as completed signed-build QA evidence.
  - Covers success output that reminds release operators to run the final
    release evidence verifier after adding generated links only when validated
    records use one version/build and cover Android/iOS.
  - Covers scoped Android-only and iOS-only link summaries, update eligibility,
    and final verifier commands so internal QA packages can use the same
    platform scope end to end.
  - Covers scoped link generation ignoring out-of-scope valid records with
    different version/build values, matching final release verifier behavior.
  - Covers scoped link generation reporting out-of-scope invalid records as
    ignored warnings instead of blocking Android-only or iOS-only link updates.
  - Covers scoped Android record content with real validator execution, proving
    Android-only link updates do not depend on hidden iOS evidence fields.
  - Covers deferred final verifier messaging when validated records are missing
    Android/iOS platform coverage or span multiple version/build values.
  - Covers the optional `--update-release-evidence` mode that writes validated
    QA links into the guarded release-evidence marker block and rejects
    documents missing that marker block.
  - Covers direct release-evidence update calls rejecting empty validated record
    sets and missing Android/iOS platform coverage before modifying the guarded
    link block.
  - Covers link-update failures printing targeted Next action hints for
    multi-version QA records and missing Android/iOS platform coverage,
    including scoped signed-QA-record creation hints that use the existing
    validated record version/build when only one platform is missing, and
    signed-QA-record creation hints when no validated records exist yet.
  - Covers those link-update Next action hints preserving the current QA
    records directory for empty directories, missing platform coverage, and
    multi-version remediation.
  - Covers invalid-record link-update failures pointing operators to
    `qa_build_record_report.py <failed-record>` before release-evidence links
    are updated.
  - Covers the shared `release_evidence_update_hints()` routing directly, so
    empty directories, invalid records, missing platform coverage, and
    version/build mismatches keep distinct remediation commands.
  - Covers rejecting legacy `ios-android` scope filenames as Android/iOS
    platform coverage before updating guarded release-evidence links.
  - Covers Android-only and iOS-only scoped link output warning that scoped
    internal QA does not approve a full Android/iOS release candidate.
  - Covers custom QA record directories, including directories also named
    `qa-builds` such as `tmp/qa-builds`, so generated release-evidence links
    point at the actual record path instead of incorrectly rewriting every
    record as `docs/qa-builds/...`, and so follow-up final verifier commands
    use the same custom QA record directory.
- `python -m unittest tool\qa_preflight_test.py`
  - Passed: 30 QA preflight helper tests.
  - Covers ready summaries, Android signing input blockers, iOS wrapper
    blockers, missing or invalid iOS export options blockers, invalid existing
    QA records, missing QA record directories, saved `--log` preflight
    transcripts, overwrite protection, explicit `--force` regeneration, and CLI
    stdout/stderr behavior.
  - Covers PowerShell placeholder guidance on blocked iOS-scoped preflight
    commands, while omitting it for Android-only scope or concrete Apple Team ID
    and export method values.
  - Covers manual release gate documentation as a preflight blocker when the
    release audit, QA checklist, QA record template, or final evidence log
    command drift out of alignment.
  - Covers automated release gate documentation as a preflight blocker when the
    GitHub workflow, release checklist, or release evidence command sequence
    drifts away from `tool/run_release_gates.py`.
  - Covers runtime-boundary verification as a preflight blocker when mobile
    runtime code or dependency locks reintroduce corelib/native bridge access
    or phone-local SSH dependencies.
  - Covers ready/status output naming QA record validation/redaction rules and
    the requirement that completed QA evidence has no secret redaction failures.
  - Covers ready/status output naming scoped internal QA command parity so
    Android-only and iOS-only QA command examples stay part of preflight.
  - Covers scoped Android-only and iOS-only ready output warning that scoped
    internal QA approval is not full Android/iOS release approval.
  - Covers Android signing blocker output that points operators to
    `setup_android_signing.py` with the required environment variables.
  - Covers release status next actions that print concrete setup helper commands
    for missing Android signing inputs and iOS export options while warning not
    to add placeholder signing/export files.
  - Covers release handoff command sequence carrying the Android signing
    environment-variable note and the same placeholder signing/export warning.
  - Covers QA preflight BLOCKED output carrying the same setup helper commands
    and placeholder-file warning before signed-build QA record creation.
  - Covers iOS export-options setup hints that preserve the requested Team ID
    and export method when those values are supplied.
  - Covers optional iOS Team ID/export method mismatch reporting through both
    the validator and CLI preflight entry point.
  - Covers platform-scoped preflight so Android-only runs skip iOS wrapper and
    export checks, while iOS-only runs skip Android signing checks.
  - Covers scoped existing-record checks that treat valid records from the
    wrong platform as out-of-scope and keep prompting operators to create an
    in-scope signed-build QA record.
  - Covers deferring signed-build QA record creation hints while preflight
    blockers remain, so signing/export setup failures point operators back to
    `qa_preflight.py` instead of the later record-creation flow; Android/iOS
    deferred preflight commands keep Team ID and export method placeholders so
    iOS export-options expectations are explicit.
  - Covers the preflight hint for the required release handoff `--version`,
    `--team-id`, `--output`, runtime-boundary log, and release-gates log flow.
  - Covers preserving supplied iOS Team ID/export method values in signed-QA
    record next-action hints.
  - Covers release status next-action SSH evidence reminders that include
    not-phone-local/ad hoc terminal evidence rejection, GUI/agent claim,
    explicit worker claim/update evidence, `ssh_session`
    `output_chunk`/`output_seq`, and GUI/agent Ctrl+C handling.
  - Covers that the preflight hint includes a copyable
    `create_qa_build_record.py` command with validator-compatible
    release-handoff, runtime-boundary, and release-gates evidence strings.
  - Covers that preflight uses the shared release evidence command helper, so
    status, handoff, QA record creation, validation, gap-report, and release
    evidence link hints do not drift apart.
  - Covers custom QA record directories in preflight validation, CLI argument
    forwarding, deferred rerun commands, signed-build QA record creation hints,
    release-evidence link updates, and final evidence verification commands.
- `python -m unittest tool\release_evidence_commands_test.py`
  - Passed: 41 release evidence command helper tests.
  - Covers validator-compatible Final Release Decision prefills, the copyable
    `create_qa_build_record.py` command, and the default signed-QA-record
    next-action hint used by preflight and release status.
  - Covers final release evidence verifier commands that always include an
    explicit `--scope` value, including the default `android-ios` full-release
    path and scoped Android/iOS QA paths. Scoped verifier log filenames include
    the platform scope so Android-only, iOS-only, and full-release evidence
    checks for the same version/build do not overwrite each other.
  - Covers that the default signed-QA-record next-action hint continues past
    record creation into signed artifact evidence generation, record validation,
    gap reporting, and release evidence link updates, while explicitly
    reminding operators to complete real-device share/permission, Hub
    discovery with post-SMS-verification official credits LLM proof, concrete
    `llm-request-id` and `llm-usage-record`, notification, and
    backend-managed SSH GUI/agent claim or worker handoff plus `ssh_session`
    `output_chunk`/`output_seq` evidence,
    not-phone-local/ad hoc terminal evidence rejection, and phone-initiated
    interrupt evidence through a Hub control record or `/api/mobile/ssh/sessions/{session_id}/interrupt` showing GUI/agent Ctrl+C handling before validation.
  - Covers a `release_handoff.py --dry-run --output ...` preview before the
    write-to-file handoff command, so operators can inspect the plan without
    overwriting existing handoff evidence.
  - Covers next-action hints that explicitly distinguish release handoff plans
    from completed signed-build QA records, so operators do not treat
    `handoff-*.md` files as final release evidence.
  - Covers scope-specific signed artifact evidence commands in shared
    signed-QA-record hints, so Android-only records do not prompt for iOS
    artifact evidence.
  - Covers rejecting weak custom Android signed artifact command values that the
    signed artifact evidence tool would reject, such as debug artifact paths,
    `release-key` signing aliases, or bare `internal` installer channels.
  - Covers shared iOS signed-QA-record hints using the default
    `build/ios/archive/MaClawMobile.xcarchive` archive path and the
    `plan_ios_release.py --provisioning-profiles ... --record-dir ...`
    evidence command, matching the release handoff command sequence while
    still allowing TestFlight replacement.
  - Covers rejecting weak custom iOS signed artifact command values that the
    signed artifact evidence tool would reject, such as shorthand TestFlight
    labels, malformed Apple Team IDs, or generic provisioning profile notes.
  - Covers shared platform coverage helpers so legacy `ios-android` scope
    never counts as Android or iOS release evidence.
  - Covers a source guard that prevents non-test release tools from
    reintroducing the legacy `ios-android` scope string.
  - Covers that the shared signed-QA-record next-action hint runs
    `qa_preflight.py` after handoff/signing setup and before runtime-boundary
    and release-gates evidence capture, forwarding the selected Apple Team ID
    and iOS export method when those values are known.
  - Covers scope-specific Android signing setup and iOS export-options setup
    commands in shared signed-QA-record hints before preflight runs.
  - Covers executing the generated `create_qa_build_record.py` command against
    a template fixture to prove it creates a prefilled QA record.
  - Covers the shared handoff evidence path used by Final Release Decision
    prefills, signed-build QA hints, and release handoff outputs.
  - Covers custom QA record directories in Final Release Decision prefills and
    generated record-creation commands, including runtime-boundary and
    release-gates evidence logs stored beside the custom records.
  - Covers custom QA record directories in signed-build QA next-action hints,
    so handoff, saved logs, artifact evidence, record creation, validation,
    release-evidence linking, and final verification commands do not fall back
    to `docs/qa-builds`.
  - Covers scope-specific handoff evidence paths for Android-only and iOS-only
    internal QA, so separate platform handoff transcripts for the same
    version/build do not overwrite each other.
  - Covers the shared QA build record path placeholder used by release handoff
    validation and gap-report commands.
  - Covers the shared runtime-boundary and release-gates evidence commands used
    by signed-build QA hints and release handoff command sequences.
  - Covers the shared release status report, release handoff, and QA preflight
    commands used by signed-build QA hints, gap reports, and release handoff
    command sequences.
  - Covers the shared Android signing setup and iOS export-options setup
    commands used by preflight and release handoff command sequences.
  - Covers the shared Android release build and dry-run build commands used by
    release handoff command sequences.
  - Covers Android release build commands that include complete
    `--record-dir`, `--signing-identity`, and `--installer-channel` evidence
    options, and rejects partial evidence option sets before operators get a
    copyable but invalid command.
  - Covers rejecting malformed Android release build versions before generating
    build-name/build-number commands.
  - Covers rejecting unsupported Android release artifact values such as `aab`
    so shared command hints stay aligned with the build helper's `apk` and
    `appbundle` CLI choices.
  - Covers the shared Android signed artifact evidence command used by release
    handoff command sequences.
  - Covers the shared iOS release plan command used by release handoff command
    sequences.
  - Covers the shared iOS signed artifact evidence command used by release
    handoff command sequences, including `--record-dir docs/qa-builds` so local
    `.xcarchive` evidence can be validated and printed relative to QA records.
  - Covers the shared iOS release plan command in both planning-only mode and
    QA evidence mode, requiring `--provisioning-profiles` and `--record-dir`
    together before handoff emits a paste-ready command.
  - Covers the shared QA build record validation and gap-report commands used
    by release handoff command sequences.
  - Covers the shared QA record directory validation and final release evidence
    verification commands used by handoff and next-action hints.
  - Covers scoped QA record directory validation commands, so Android-only and
    iOS-only handoff sequences pass the same scope through directory validation
    before final evidence verification.
  - Covers optional final release evidence log paths
    (`docs/qa-builds/final-release-evidence-<version+build>.log` for
    Android/iOS, or scope-qualified log names for Android-only/iOS-only runs) in
    shared final verifier commands used by signed-QA hints and release handoffs.
  - Covers the exact automated release-gate success line shared by the runner
    and QA record validator.
  - Covers the shared `qa_release_evidence_links.py --update-release-evidence`
    command used after validated QA records exist.
  - Covers the shared `qa_build_record_report.py` hint used when an existing
    signed-build QA record fails validation.
  - Covers the shared version-mismatch hint used when final QA records span
    multiple version/build values.
- `python -m unittest tool\setup_android_signing_test.py`
  - Passed: 9 Android signing setup helper tests.
  - Covers normalizing Windows keystore paths before writing Java Properties,
    preventing Gradle from dropping backslashes during signed builds.
  - Covers environment-variable validation, documented placeholder rejection,
    debug-keystore rejection, local `android/key.properties` writing,
    overwrite protection, successful CLI setup, missing-environment CLI
    errors, and refusing to write `key.properties` when placeholders are still
    present.
- `python -m unittest tool\release_status_report_test.py`
  - Passed: 24 release status report helper tests.
  - Covers grouped not-ready output, ready output, current empty-fixture CLI
    failure, ready CLI stdout behavior, and `--log` evidence capture with
    existing-log overwrite protection plus `--force` replacement.
  - Covers passing the requested platform scope into final release evidence
    verification so Android-only and iOS-only QA checks stay scope-aware.
  - Covers not-ready next actions that include the required release handoff
    `--version`, `--team-id`, `--export-method`, and `--output` arguments.
  - Covers runtime-boundary preflight visibility in release status output, so
    corelib/native bridge or phone-local SSH dependency regressions remain
    visible before signed-build QA.
  - Covers the not-ready next action's copyable QA record creation command with
    the same validator-compatible Final Release Decision evidence strings used
    by handoff, preflight, and the QA record creator.
  - Covers optional iOS Team ID/export method CLI argument forwarding to the
    status builder, and final-evidence blocker next-action wording when valid records still fail final verification.
  - Covers reusing final verifier next-action hints for valid QA records that
    still fail final evidence checks, including multi-version record sets.
  - Covers preserving supplied iOS Team ID/export method values in missing
    signed-build QA record next actions, including the record validation,
    gap-report, and release evidence link update follow-ups.
  - Covers readable platform-scope labels in preflight-blocker next actions:
    Android, iOS, and Android/iOS instead of raw scope IDs.
  - Covers preflight-blocker next actions listing the concrete blocking checks
    and deferring the signed-build QA record creation flow until preflight
    passes, with Android/iOS preflight rerun commands preserving Team ID and
    export method placeholders when no concrete values were supplied, while
    still reminding operators that signed-build QA must later capture
    real-device share/permission, Hub discovery with post-SMS-verification
    official credits LLM proof, concrete `llm-request-id` and
    `llm-usage-record`, notification, and backend-managed SSH GUI/agent
    claim or worker handoff plus explicit worker claim/update evidence,
    `ssh_session` realtime `output_chunk`/`output_seq` evidence and phone-initiated
    interrupt evidence through a Hub control record or `/api/mobile/ssh/sessions/{session_id}/interrupt` showing GUI/agent Ctrl+C handling.
  - Covers scoped status output that separates in-scope valid QA records from
    out-of-scope valid records, so Android-only and iOS-only status reports do
    not make the wrong platform's evidence look sufficient.
  - Covers scoped status summaries separating blocking invalid records from
    out-of-scope invalid records, so ignored wrong-platform gaps do not make a
    scoped ready report look contradictory.
  - Covers out-of-scope-only status next actions that ask operators to create
    in-scope signed-build QA records before resolving final evidence blockers.
  - Covers invalid QA build record next actions that point directly to
    `qa_build_record_report.py` instead of telling the operator to create a new
    signed-build QA record.
  - Covers custom QA record directories in status building, validation, final
    evidence verification, preflight checks, and not-ready next-action text.
  - Covers final-evidence blocker next actions using the same custom QA record
    directory for release-evidence link updates and final verification reruns.
  - Covers CLI `--records-dir` and `--scope` forwarding into release status
    generation, including copyable custom-directory Android final-verifier
    rerun commands.
- `python -m unittest tool\release_handoff_test.py`
  - Passed: 26 release handoff helper tests.
  - Covers blocker summaries, ready output, operator command generation, output
    file writing, and blocked/ready CLI exit codes.
  - Covers `--dry-run` preview with `--output`, so operators can inspect a
    handoff without overwriting an existing evidence file or passing `--force`.
  - Covers `--dry-run --output` preserving an existing `Current Local Evidence
    Snapshot` in stdout while leaving the saved handoff file unchanged.
  - Covers real release version/build, Apple Team ID, and iOS export method CLI
    validation before generating copyable signed-build evidence commands.
  - Covers forwarding the handoff Team ID/export method into release status
    preflight and printing the matching release status command in the handoff
    sequence.
  - Covers forwarding the handoff Team ID/export method into the generated
    QA preflight command, so `ios/ExportOptions.plist` is checked against the
    same release inputs before signed-build evidence capture.
  - Covers the full copyable command sequence order from status preflight
    through signed artifact evidence, QA record creation, record validation,
    evidence links, directory validation, and final release evidence
    verification.
  - Covers scoped handoff command sequences that pass Android-only or iOS-only
    scope into QA record directory validation before final evidence
    verification.
  - Covers Android handoff commands for both signed APK and Play/internal
    appbundle builds with inline `--record-dir`, signing identity, and
    installer-channel evidence options, while still listing the standalone
    signed artifact evidence helper for already-built artifacts.
  - Covers iOS handoff artifact evidence commands using the same default
    `build/ios/archive/MaClawMobile.xcarchive` path printed by
    `plan_ios_release.py`, including the `--provisioning-profiles` and
    `--record-dir docs/qa-builds` pair required for inline evidence
    generation, while keeping TestFlight builds replaceable.
  - Covers that the handoff command sequence performs local signing/export
    setup and QA preflight before capturing saved runtime-boundary and
    release-gates logs.
  - Covers that generated Final Release Decision prefill values satisfy the
    QA build record creator's handoff/runtime-boundary/release-gates evidence
    semantics.
  - Covers reusing final verifier next-action hints when valid QA records exist
    but the final evidence package still fails, including multi-version record
    remediation and missing guarded release-evidence QA record links.
  - Covers handoff status output pointing invalid QA build records to
    `qa_build_record_report.py` for gap analysis.
  - Covers backend SSH handoff evidence reminders that require not-phone-local
    ad hoc terminal rejection, GUI/agent claim or worker handoff, explicit
    worker claim/update evidence, `ssh_session` `output_chunk`/`output_seq`,
    and Hub interrupt control-record or endpoint proof plus GUI/agent Ctrl+C handling.
  - Covers rejecting non-`handoff-*.md` handoff output filenames when writing
    directly into the QA build records directory, so release handoff plans do
    not get mistaken for completed signed-build QA records.
  - Covers handoff notes reminding QA operators that completed records must pass
    `validate_qa_build_record.py` without secret redaction failures and use
    `qa_build_record_report.py` to fix gaps before release-evidence linking.
  - Covers handoff output overwrite protection and explicit `--force`
    regeneration when a saved handoff evidence file already exists.
  - Covers non-colliding scoped handoff output paths for Android-only and
    iOS-only QA, while preserving the unscoped handoff path for full
    Android/iOS release handoff.
  - Covers scope-specific platform commands, operator inputs, current-status
    blockers, and evidence so Android-only and iOS-only handoffs do not include
    the other platform's signing, build, export, or artifact evidence steps.
  - Covers scoped ready handoff status as Android/iOS internal QA approval only,
    not full Android/iOS final release approval.
  - Covers custom QA record directories in handoff command sequences so status,
    handoff output, runtime-boundary logs, release-gate logs, artifact evidence,
    record creation, release-evidence links, directory validation, and final
    evidence verification all use the same explicit records directory.
  - Covers custom QA record directories flowing into the inline Android signed
    APK/AAB build evidence commands in the handoff sequence.
  - Covers the handoff Current Status missing-record remediation hint using the
    same custom QA record directory as the generated command sequence.
  - Covers final-evidence blocker hints inside handoff output using the same
    custom QA record directory as the generated handoff command sequence.
- `python3 tool/validate_qa_build_records_dir.py docs/qa-builds`
  - Passed: 0 completed signed-build QA record(s) currently present in the
    ignored local records directory.
- `python -m unittest tool\run_release_gates_test.py`
  - Passed: 16 release gate runner tests.
  - Covers release-gates evidence log output normalization for Windows-captured
    Flutter success lines so `Built ...app-debug.apk` remains readable in QA
    attachments.
  - Covers gate order, working directories, critical command coverage, and
    numbered dry-run output.
  - Covers that the QA evidence gate-count constant matches the actual
    `release_gates()` list length.
  - Covers dry-run and full-run `--log` output plus overwrite protection and
    explicit `--force` regeneration so signed-build QA can attach automated-gate
    transcripts to completed records.
  - Covers failed gate runs still writing partial stdout/stderr logs without
    recording the all-gates-passed success line, so QA can retain actionable
    automated-gate evidence even when a release gate fails.
  - Covers Windows batch-command executable resolution for tools such as
    `flutter.BAT` while keeping documented commands readable as `flutter ...`.
  - Covers CI workflow, release checklist, and release evidence parity for
    automated gate commands and command order, plus debug APK artifact upload.
- `python -m unittest tool\verify_debug_apk_evidence_test.py`
  - Passed: 7 debug APK evidence verifier tests.
  - Covers debug APK evidence parsing, duplicate build-command mentions,
    relative artifact paths, missing artifacts, size mismatches, SHA256
    mismatches, missing evidence fields, and the required
    `maclaw-mobile-debug-apk` CI artifact name.
- `python -m unittest tool\update_debug_apk_evidence_test.py`
  - Passed: 6 debug APK evidence updater tests.
  - Covers artifact path, size, SHA256 refresh, preserving surrounding evidence
    lines, missing artifact failures, missing section failures, and CLI update
    behavior.
- `python -m unittest tool\signed_artifact_evidence_test.py`
  - Passed: 25 signed artifact evidence helper tests.
  - Covers Android signed APK hash/size snippets, record-relative paths, debug
    artifact rejection, untrackable names, missing artifacts, Android
    version/build format validation, required Android version/build, signing
    identity, and installer channel evidence at both function and CLI layers,
    iOS archive metadata snippets, local `.xcarchive` record-dir validation and relative evidence paths, explicit TestFlight build snippets, iOS Team
    ID/profile validation with trackable UUID, `.mobileprovision`, profile
    name references, documented placeholder rejection, CLI output, and CLI
    error reporting.
  - Covers rejecting missing or non-directory `--record-dir` values before
    generating record-relative Android artifact or iOS archive evidence.
  - Covers CLI help describing `--record-dir` as a generic QA records
    directory while preserving `docs/qa-builds` as the documented default.
  - Covers generated Android and iOS signed artifact evidence lines pasted into
    a completed QA build record and accepted by the QA record validator.
    example.
- `python -m unittest tool\verify_manual_release_gates_test.py`
  - Passed: 19 manual release gate parity tests.
  - Covers the canonical manual release gate list, release audit remaining
    blockers, QA device checklist execution steps, and QA build record final
    decision fields so signed-build, real-device, Hub discovery, official credits usage-record, and SSH manual
    gates stay aligned across release documentation.
  - Requires backend SSH manual gates to include not-phone-local
    backend-managed evidence, GUI/agent claim evidence, worker claim/update
    evidence, `ssh_session` `output_chunk`/`output_seq`, and Hub interrupt control-record or endpoint proof plus GUI/agent Ctrl+C handling, so release
    audit, release evidence, device-checklist, and QA-record template SSH smoke
    steps keep the GUI/agent backend-session management requirement visible.
  - Covers the release audit rule that completed signed-build QA records must
    pass validation without secret redaction failures before they can count as
    release evidence.
  - Covers the QA release evidence link update command and guarded QA build
    record link block so validated signed-build records have a stable place to
    be linked before final evidence verification.
  - Covers typed notification payload checklist parity for document,
    `digital-employee-task:`, and `server-profile:` notification-open evidence.
  - Covers Android-only and iOS-only internal QA command examples in both the
    device checklist and QA build records README, including the rule that
    Android-only handoff commands must not include Apple Team ID options while
    iOS-only commands keep Team ID and export method values.
- `python -m unittest tool\verify_final_release_evidence_test.py`
  - Passed: 43 final release evidence verifier tests.
  - Covers the final release evidence package rule that completed signed-build
    QA records must validate successfully and cover the requested platform
    scope before approval, including full Android/iOS final release coverage
    and Android-only or iOS-only scoped internal QA verification. This release
    evidence document must link every validated in-scope QA record inside the
    guarded QA build record link block, using `docs/qa-builds/...` for the
    canonical QA directory or the actual record path for custom QA record
    directories, with link labels containing the validated QA record filename,
    and must reject stale or unvalidated QA build record links in that guarded
    block while allowing ordinary non-QA markdown links. Missing validated QA
    record links, generic link labels, and stale/unvalidated guarded-block links
    are reported so operators can fix the final evidence block in one pass.
    Ordinary development gates may still pass with an empty `docs/qa-builds/`
    directory.
  - Covers rejecting legacy `ios-android` scope filenames as final Android/iOS
    coverage even if a downstream validator mistakenly reports them as valid.
  - Covers that release handoff evidence files, including scoped Android-only
    and iOS-only handoff files, do not count as completed signed-build QA
    records for final release readiness.
  - Covers scoped final verification requiring at least one completed in-scope
    signed-build QA record even when the directory contains only ignored
    out-of-scope invalid records.
  - Covers final verifier CLI failure output that prints the shared copyable
    signed-build QA next-action hint, and switches to the shared release
    evidence link update hint when validated QA records exist but the guarded
    link block is incomplete, using the validated QA record version/build for
    the follow-up final evidence log path instead of leaving a placeholder.
  - Covers missing-platform final verifier next actions using the existing
    validated QA record version/build when prompting operators to create the
    missing Android or iOS signed-build QA record.
  - Covers custom QA record directories, including directories also named
    `qa-builds` such as `tmp/qa-builds`, in final verifier link validation and
    failure next actions, so missing-record and missing-link remediation
    commands do not fall back to `docs/qa-builds`.
  - Covers final verifier CLI failure output that points directly to
    `qa_build_record_report.py` when an existing signed-build QA record is
    present but invalid.
  - Covers final verifier CLI failure output that gives a single-version
    remediation hint, preserving the current QA records directory when
    otherwise valid Android/iOS QA records use different version/build values.
  - Covers the shared `next_action_hints()` routing directly, so missing
    release-evidence links, invalid QA records, and version/build mismatches
    keep distinct remediation commands.
  - Covers Android-only and iOS-only final verification using actual scoped QA
    record content without mocking directory validation, so scoped records must
    satisfy the same validator and release-evidence link rules used in QA.
  - Covers scoped final verification ignoring invalid records whose filenames
    clearly belong to the other platform, while current-scope or unparseable
    invalid records remain blocking evidence gaps.
  - Covers final verifier success output with auditable verification scope, QA records directory,
    version/build, Android/iOS platform coverage, release evidence path, and
    validated QA record filenames.
  - Covers optional final verifier `--log` output for success and failure
    transcripts; successful transcript filenames must match the validated
    version/build and requested scope, wrong-version log paths print the
    matching `verify_final_release_evidence.py --log ...` remediation command
    instead of falling back to generic QA-record creation hints, both transcript
    types retain the verification scope, QA records directory, and release
    evidence path needed to audit the archived result, plus overwrite
    protection and explicit `--force` regeneration.
- `python -m unittest tool\verify_android_release_signing_test.py`
  - Passed: 8 Android release signing verifier tests.
  - Covers the Gradle release signing guard, rejection of debug-key fallback,
    rejection of Flutter Android Gradle template comments,
    required `android/key.properties` loading, tracked
    `android/key.properties.example` placeholder coverage, and `.gitignore`
    rules for local keystore material.
- `python -m unittest tool\build_android_release_test.py`
  - Passed: 17 Android release build helper tests.
  - Covers local `android/key.properties` validation, missing signing input
    errors, debug-keystore rejection, APK/AAB release command construction,
    build-name/build-number forwarding, required paired version/build
    traceability, dry-run behavior, unknown artifact rejection, and Flutter
    build failure reporting without traceback, plus artifact path selection for
    signed QA packages.
  - Covers that missing local signing inputs print the shared
    `setup_android_signing.py` next action, while unrelated version/build input
    errors do not incorrectly point operators at signing setup.
  - Covers CLI help describing `--record-dir` as a generic QA records
    directory while preserving `docs/qa-builds` as the documented default
    example.
  - Covers rejecting a missing QA records directory before invoking Flutter
    when Android artifact evidence generation was requested.
  - Covers successful signed artifact evidence output printing the next
    `python3 tool/validate_qa_build_record.py <records-dir>/<record>.md`
    command and the matching `python3 tool/qa_build_record_report.py
    <records-dir>/<record>.md` gap-report command, with custom records-dir
    paths normalized for copy/paste follow-up.
- `python -m unittest tool\verify_ios_wrapper_test.py`
  - Passed: 8 iOS wrapper verifier tests.
  - Covers readable iOS permission usage descriptions, Runner and Share
    Extension app-group entitlements, Share Extension activation rules for
    text, URLs, files, and images, receive_sharing_intent controller wiring,
    the generated-project marker, and rejection of Flutter template Runner
    bundle names.
- `python -m unittest tool\plan_ios_release_test.py`
  - Passed: 13 iOS release plan helper tests.
  - Covers Apple Team ID validation, Xcode archive/export command planning,
    export method validation, local export-options readiness failures,
    export-options Team ID/method mismatch reporting, wrapper readiness
    failures, and the QA record evidence fields needed for `.xcarchive` or
    TestFlight signed builds.
  - Covers optional `--record-dir` evidence generation that validates local
    `.xcarchive` paths, prints archive paths relative to `docs/qa-builds`, and
    reports missing archives without a traceback.
  - Covers the documented two-step iOS release flow: run archive/export
    planning before the signed archive exists, then generate record-relative
    iOS archive evidence only after the `.xcarchive` or TestFlight build exists.
  - Covers rejecting `--record-dir` without `--provisioning-profiles` before
    wrapper/export checks, so iOS artifact evidence generation is explicit.
  - Covers CLI help describing `--record-dir` as a generic QA records
    directory while preserving `docs/qa-builds` as the documented default
    example.
  - Covers successful iOS signed archive/TestFlight evidence output printing
    the next `python3 tool/validate_qa_build_record.py
    <records-dir>/<record>.md` command and the matching
    `python3 tool/qa_build_record_report.py <records-dir>/<record>.md`
    gap-report command, with custom records-dir paths normalized for
    copy/paste follow-up.
- `python -m unittest tool\setup_ios_export_options_test.py`
  - Passed: 6 iOS export options setup helper tests.
  - Covers Team ID normalization, export method validation, local
    `ios/ExportOptions.plist` generation, overwrite protection, CLI output,
    next-step preflight hints that preserve the normalized Team ID and chosen
    export method, and invalid Team ID CLI failures.
- `python tool\configure_platforms.py`
  - Passed.
- `python tool\verify_android_release_signing.py`
  - Passed.
  - Confirms the current Android release signing config requires local
    `android/key.properties`, assigns release builds to the release signing
    config when present, keeps key material ignored by git, and keeps a
    redacted `android/key.properties.example` template available for local QA
    setup.
- `python tool\build_android_release.py --artifact apk --build-name 1.0.0 --build-number 42 --dry-run`
  - Manual signed-build preparation command.
  - Validates local signing inputs and prints the exact release build command
    without requiring CI to hold keystore material. The same command without
    `--dry-run` builds the signed QA APK with a trackable version/build and
    prints artifact path, size, and SHA256 for the QA build record.
- `python tool\verify_ios_wrapper.py`
  - Passed.
  - Confirms the current generated iOS wrapper exposes readable privacy
    prompts, `maclaw` and share URL schemes, Runner and Share Extension
    app-group wiring, and Share Extension import activation rules.
- `python tool\plan_ios_release.py --team-id <REAL_APPLE_TEAM_ID> --export-method development`
  - Manual macOS signed-build preparation command.
  - Validates generated iOS wrapper readiness and local `ios/ExportOptions.plist`
    readiness, then prints Xcode archive/export commands plus Runner bundle ID,
    Share Extension bundle ID, app group, Team ID, and archive/TestFlight
    evidence fields for the QA build record.
- `go test ./hub/internal/httpapi -run "TestMobile.*" -count=1`
  - Passed; revalidated on the current worktree after the export download URL
    safety updates.
- `go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemptionRouteIsNotExposed|DesktopQRSessionRouteIsNotExposed)|TestSameURLOriginHandlesDefaultPorts" -count=1`
  - Passed; revalidated on the current worktree after the desktop GUI QR
    HubCenter proxy updates.
  - Covers the mobile account boundary: the legacy service-redemption route and
    legacy desktop-QR mobile-login route both return `404`, leaving phone/SMS
    verification as the only mobile registration and login path.
  - Covers same-origin comparison with explicit default ports such as
    `https://host:443` and `http://host:80`, which remains used when the
    signed-in app validates the discovered Hub origin for desktop GUI LLM QR
    authorization.
- `go test ./hub/internal/httpapi -run "TestMobile.*(SSH|BackendSSH|RealtimeBackendSSH)" -count=1`
  - Passed; revalidated after aligning MaClaw Mobile server maintenance language
    and QA evidence around GUI/agent-managed backend SSH sessions.
  - Covers the tenant Hub backend SSH session APIs and realtime
    `ssh_session` event payloads used by mobile to create/attach sessions,
    receive incremental `output_chunk`/`output_seq` output, and keep evidence
    tied to the same GUI/agent-bound `backend_session_id`.
- `go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown|TestResolveMobileBackendSSHHost|TestMobileServerProfilesFromSSHHosts|TestProcessMobileBackendSSHSession" -count=1`
  - Passed; revalidated after aligning the export URL safety checks and the
    mobile SSH model with MaClaw GUI `SSHSessionManager` behavior.
  - Passed: 17 GUI mobile worker/profile tests.
  - Covers remote mobile digital employee task discovery and submission,
    mobile document source markdown, Hub client retry/authorization handling,
    Hub client base URL resolution from discovered tenant Hub URLs,
    GUI/agent server-profile publication, mobile backend SSH session
    claim/worker handling, profile resolution from desktop `SSHHosts`,
    sanitized server-profile sync, missing-profile failure reporting through
    the worker path, desktop-managed close handling that clears pending input,
    output delta tracking,
    mobile-to-core background task ID mapping, background task status payloads,
    bounded default wait timeout behavior for mobile `wait_task`, and
    process-level handling that waits for the desktop
    `SSHSessionManager` managed session before running mobile-created backend
    task/file control records.
  - Confirms the desktop-side worker path is used instead of phone-local SSH
    credentials, direct phone connections, or phone-local SFTP.
- `python -m unittest tool\configure_platforms_test.py`
  - Passed: 21 platform configuration tests.
- `python -m unittest tool\validate_qa_build_record_test.py`
  - Passed: 236 QA record validator tests.
  - Covers final automated evidence artifact references matching the record
    `Version/build number`, so stale handoff/preflight/runtime-boundary/release
    gate logs from another build cannot validate the signed QA record.
  - Covers local-network permission evidence requiring the same
    GUI/agent-managed `backend_session_id`, read-only command output, and
    explicit rejection of phone-local/ad hoc terminal evidence.
- `python -m unittest tool\create_qa_build_record_test.py`
  - Passed: 15 QA build record scaffold tests.
- `python -m unittest tool\verify_runtime_boundary_test.py`
  - Passed: 12 runtime boundary verifier tests.
  - Covers the current mobile runtime source tree and negative fixtures for
    Dart FFI, dynamic-library loading, and Go `corelib` references while
    ignoring docs/tests-only mentions.
  - Covers `pubspec.lock` scanning and rejects phone-local SSH dependencies
    such as `dartssh2`.
  - Covers `pubspec.yaml` and runtime source scanning that rejects terminal
    emulator dependencies/imports such as `xterm`, keeping mobile remote
    maintenance as Hub/GUI-agent backend-session output rather than a
    phone-local terminal surface.
  - Covers mobile runtime source scanning and rejects phone-side SSH credential
    save/read APIs such as `saveServerPassword` and `readServerPrivateKey`.
  - Covers success and violation `--log` output plus overwrite protection and
    explicit `--force` regeneration for signed-build QA evidence.
- `python -m unittest tool\run_release_gates_test.py`
  - Passed: 16 release gate runner tests.
  - Covers release-gates evidence log output normalization for Windows-captured
    Flutter success lines so `Built ...app-debug.apk` remains readable in QA
    attachments.
- `python tool\verify_runtime_boundary.py`
  - Passed.
  - Confirms current mobile runtime source under `lib`, `android`, `ios`, and
    `pubspec.yaml` does not embed or bridge Go `corelib`, depend on
    phone-local SSH, ship terminal emulator surfaces, or expose phone-side SSH credential save/read APIs.
  - Saved continuation evidence:
    `docs/qa-builds/runtime-boundary-20260706-backend-ssh-realtime.log`.
- `flutter test test/mobile_local_store_test.dart test/assistant_screen_test.dart test/digital_employees_screen_test.dart --concurrency=1 --reporter compact`
  - Passed: 38 focused storage, assistant, and digital employee widget/model
    tests.
  - Covers SQLite-backed mobile local cache, legacy JSON migration, shared-link
    assistant answers, disabled-Hub assistant fallback, quick emergency prompts,
    multi-tab assistant behavior, voice/file/camera handoff,
    digital employee task submission with Hub/tenant/HubCenter/LLM credits
    context, redaction of common secrets before mobile prompts are handed to
    remote digital employees, recent task restore, and digital employee result
    document drafts redacting prompt, message, result, and claimed-by metadata
    before saving.
- `flutter test test/mobile_local_store_test.dart --concurrency=1 --reporter compact`
  - Passed: 6 local storage tests after tightening digital employee task cache
    redaction.
  - Covers SQLite-backed digital employee task history storing only redacted
    local copies of task prompt, context values, result, message, and claimed-by
    metadata while retaining task IDs, employee IDs, task type, status, and
    recent-history ordering for mobile recovery.
- `flutter test test/mobile_bootstrap_test.dart --concurrency=1 --reporter compact`
  - Passed: 10 bootstrap parsing tests.
  - Covers verified phone accounts defaulting the mobile official LLM credits
    account to `phone:<digits>` when Hub bootstrap returns official LLM access
    without a separate `credits_account`, including formatted phone-number
    input and verified `phone:<digits>` credits accounts being normalized to the
    digits-only credits account, SMS-verified phone login supplying the official
    credits fallback after bootstrap, and malformed `phone:` credits with
    letters remaining untrusted instead of being coerced. Also covers the
    mobile `assistant` feature flag being independent from the optional
    `search` feature flag, and the backend SSH session feature preferring the
    new `backend_ssh_sessions` bootstrap field while accepting legacy
    `local_ssh` from older Hub responses.
- `go test ./hub/internal/httpapi -run "TestMobileBootstrapPayloadIncludesServiceStatuses" -count=1`
  - Passed as part of the Go mobile API gate.
  - Covers the official Hub mobile bootstrap advertising
    `backend_ssh_sessions` for GUI/agent-managed backend SSH sessions and not
    advertising the legacy `local_ssh` field, so new mobile clients and QA
    evidence do not treat remote-server maintenance as a phone-local SSH
    client feature.
- `flutter test test/mobile_credits_test.dart --concurrency=1 --reporter compact`
  - Passed: 2 shared mobile credits helper tests.
  - Covers the common trusted credits rule used by startup routing, account
    display, and digital employee task context: only normalized
    `phone:<digits>` credits are trusted, malformed `phone:` credits are
    rejected, and bootstrap-level lookup falls back from LLM credits to the
    verified user phone credits.
- `flutter test test/digital_employee_test.dart test/digital_employees_screen_test.dart test/digital_employees_controller_test.dart --concurrency=1 --reporter compact`
  - Passed: 18 digital employee model, widget, and controller tests.
  - Covers mobile emergency handoff prompts, redaction of common secrets before
    submitting mobile prompt text and task context to remote digital employees
    at the controller/API boundary, remote server/desktop task context with
    Hub, tenant, HubCenter, trusted
    `phone:<digits>` LLM credits, malformed `phone:` credits being omitted from
    remote task context, and machine-readable execution boundaries requiring
    draft-only handling until the mobile user confirms high-risk operations.
  - Covers the server/desktop log-analysis shortcut using backend session output
    and key logs wording, so the digital employee entry does not imply a
    phone-local terminal output source or terminal-emulator surface.
- `flutter analyze`
  - Passed: no issues found; revalidated on the current worktree after the
    local-store concurrent open fix.
- `flutter test --concurrency=1`
  - Passed: 441 tests.
  - No Drift debug-only multiple-database warning was emitted after adding the
    local-store concurrent open gate and isolating digital-employee widget
    history providers.
- `flutter build apk --debug`
  - Passed.
  - Artifact: `build\app\outputs\flutter-apk\app-debug.apk`.
  - Size: `205976425` bytes.
  - SHA256: `65C9539800957E91844C3F0CA326F3B56C7BA0ADB01F39EC217E3282E789626C`.
  - Refreshed after the local 2026-07-10 debug APK build verification run.
  - CI artifact name: `maclaw-mobile-debug-apk`.
- `python3 tool/verify_debug_apk_evidence.py`
  - Passed.
  - Confirms the recorded debug APK artifact path, byte size, and SHA256 match
    the current local APK before handoff.
- `python3 tool/update_debug_apk_evidence.py`
  - Passed.
  - Refreshed the recorded debug APK artifact path, byte size, and SHA256 before
    the verifier check.
- `flutter build apk --release` without `android/key.properties`
  - Failed as expected.
  - Confirms Android release builds do not fall back to the debug signing key;
    release or internal builds require local `android/key.properties` with
    `storeFile`, `storePassword`, `keyAlias`, and `keyPassword`.
- `flutter test test/mobile_feature_flags_test.dart test/app_smoke_test.dart test/mobile_shared_intent_test.dart --concurrency=1 --reporter compact`
  - Passed: 38 app shell, startup, feature flag, and shared-intent tests.
  - Covers readable bottom navigation labels for `AI助手`, `员工`, `文档`,
    and `我的`; remote maintenance remains an assistant entry and recovery
    route rather than a bottom-navigation tab; configured sessions opening the assistant; feature
    flags keeping the primary `AI助手` workspace when optional search is
    disabled and even when Hub sends `assistant:false`, because the GUI-like assistant is the non-removable mobile home surface; missing LLM
    access opening the dedicated LLM setup surface while third-party LLM authorization
    remains optional in account/settings; official LLM sessions accepting
    normalized `phone:<digits>` credits from bootstrap JSON, keeping the
    workspace open without a phone credits account, including malformed `phone:`
    values and malformed letter-bearing phone credits after bootstrap parsing;
    and shared file intents
    preferring documents when available while avoiding the assistant fallback
    when document handling is disabled.
  - Covers notification-open recovery from typed task payloads, including
    `server-profile:<id>` server alerts opening the remote maintenance tab, and
    URL payloads, including redaction of embedded URL credentials and query
    secrets before the recovery prompt is shown in the app shell.
- `flutter test test/app_smoke_test.dart --concurrency=1 --reporter compact`
  - Passed: 11 app shell and startup smoke tests.
  - Covers unknown or unrouteable notification payloads being ignored before
    they can fake task recovery, while valid typed payloads and redacted URL
    payloads still recover to the expected feature tab, and feature-disabled
    server notification fallback messaging does not claim the remote tab opened.
- `flutter test test/documents_state_test.dart --concurrency=1 --reporter compact`
  - Passed: 19 document state/controller tests.
  - Covers mobile shared-document uploads rejecting unsupported file types
    before calling the Hub upload API, while accepting supported emergency
    document formats such as PDF, Word, Excel, CSV, and images through the same
    allowlist used by the manual file picker; document draft creation and edit
    saves redact common secrets before sending title/content/markdown to the Hub;
    export downloads use phone-safe filenames and redact sensitive title
    fragments before writing files that can be shared outside the app.
- `flutter test test/mobile_shared_intent_test.dart --concurrency=1 --reporter compact`
  - Passed: 16 mobile shared-intent model/controller tests.
  - Covers multi-payload share batches where a text caption or context URL is
    delivered before the actual file payload: supported files and images still
    route to document import, while empty file paths with only message text
    fall back to assistant conversation instead of attempting an empty document upload.
  - Covers redacting shared text, shared link credentials, and shared link
    message secrets before those prompts are handed to the AI assistant.
  - Covers unsupported shared attachments with message/link context falling
    back to assistant conversation, while mixed batches still prefer a later supported
    document over an unsupported file.
  - Covers suppressing immediate duplicate shared payloads from the initial
    media load and live share stream while still allowing the same file or link
    to be shared again after the duplicate window.
- `flutter test test/mobile_network_status_test.dart test/assistant_retry_test.dart test/mobile_realtime_client_test.dart --concurrency=1 --reporter compact`
  - Passed: 20 weak-network, assistant retry, and realtime client tests.
  - Login UI keeps HubCenter discovery internal: the phone registration screen
    no longer renders preset HubCenter candidates or selected Hub details;
    authentication still probes the same official preset endpoints in the
    background. Covers offline and restored network banners, HubCenter DNS fallback,
    conversion of unexpected probe failures into offline snapshots instead of
    stream errors, restored status after the next successful probe, assistant
    retry affordances, official discovered-Hub realtime ping/event parsing,
    backend `ssh_session` status/output payload parsing, incremental
    `output_chunk`/`output_seq` parsing, and external realtime paths being
    rejected before the mobile client opens a websocket outside the discovered
    Hub. Socket close failures are isolated so they cannot replace the original
    stream result.
- `flutter test test/mobile_realtime_client_test.dart test/mobile_realtime_bridge_test.dart test/documents_state_test.dart test/digital_employees_controller_test.dart --concurrency=1 --reporter compact`
  - Passed: 34 realtime, document state, and digital employee controller tests.
  - Covers realtime bridge dispatch for document, digital employee, and backend
    SSH session events, parsing sparse realtime frames that put task/job/session
    ID and status at the top level while the nested payload only contains result
    fields, and applying those updates without losing typed notification IDs,
    SSH session IDs, or task cache linkage. Downstream event-application failures
    are isolated so one stale or malformed update cannot terminate the realtime
    subscription.
- `flutter test test/mobile_realtime_client_test.dart test/mobile_realtime_bridge_test.dart test/servers_controller_test.dart --concurrency=1 --reporter compact`
  - Passed: 22 realtime and backend server controller tests.
  - Covers Hub realtime parsing for `ssh_session`, `ssh_task`, and
    `ssh_file_operation` events, bridge dispatch of backend SSH task events to
    the server task controller, and sparse backend SSH task realtime updates
    being merged into the per-session task cache without losing command or
    GUI/agent-bound `backend_session_id` context. Also covers the backend SSH
    session controller queueing create, attach, reconnect, interrupt, input,
    and close control records for GUI/agent handling while preserving backend-session input carriage returns.
- `flutter test test/api_client_test.dart test/mobile_realtime_client_test.dart test/mobile_realtime_bridge_test.dart test/servers_controller_test.dart test/servers_screen_test.dart test/backend_ssh_command_test.dart --concurrency=1 --reporter compact`
  - Passed: 60 mobile backend SSH control-plane tests.
  - Covers the phone-side foreground path using tenant Hub APIs to create,
    attach, interrupt, reconnect, send input to, and close GUI/agent-managed
    backend SSH sessions; request GUI/agent-managed `exec_background`,
    `check_task`, `wait_task`, `list_tasks`, `kill_task`, and backend file
    operation control records through the same tenant Hub session path; caches
    GUI/agent background task status in the server controller, exposes a
    mobile screen button that submits a command as a GUI/agent background task
    while the server surface presents this as MaClaw-style backend SSH session
    management rather than a terminal-first SSH client or terminal emulator surface; it also distinguishes
    the phone foreground agent as a Hub session-management requester,
    without using phone-local SSH execution, and keeps file
    operation requests as Hub control-plane results rather than phone-local
    SFTP; parses `ssh_session` realtime updates with
    `output_chunk`/`output_seq`, preserves worker `created_at`/`updated_at`
    timing metadata, and merges sparse realtime frames without dropping the
    existing GUI/agent-bound `backend_session_id` or actual worker `claimed_by`; updates the
    server surface from the realtime bridge; copies and redacts backend session
    output before AI or digital
    employee handoff; and keeps backend command payloads queued for Hub/agent
    execution rather than phone-local SSH execution; and sends/displays the
    GUI/agent-bound `backend_session_id` with backend session output AI
    analysis when one is active; and issues deduplicated, redacted mobile
    notifications for failed/error/disconnected backend SSH realtime states.
- `flutter test test/account_screen_test.dart test/app_preferences_test.dart test/mobile_notification_service_test.dart test/mobile_local_store_test.dart test/secure_vault_test.dart --concurrency=1 --reporter compact`
  - Passed: 30 account settings, notification, local storage, and secure-vault
    tests.
  - Covers Hub/tenant/credits visibility, notification permission request
    success, denial, and plugin-failure feedback, masked display of phone-bound
    official credits accounts, malformed `phone:` credits being omitted from
    account-screen official credits display, theme/speech-language settings,
    clearing local work records without deleting server access data, separate
    server-profile cache clearing, backend session output/log AI-analysis
    confirmation wording in account privacy, local cache migration, and secure
    storage cleanup for login tokens plus legacy SSH credential residues without
    exposing phone-side SSH credential save/read APIs.
- `flutter test test/mobile_notification_service_test.dart --concurrency=1 --reporter compact`
  - Passed: 9 mobile notification service tests.
  - Covers typed notification payload routing for document, digital employee,
    and server alerts requiring a non-empty trackable ID after the prefix, while
    retaining legacy URL payload routing for document recovery. Invalid typed
    payloads and unknown raw IDs are ignored at notification-open time and do
    not replace a valid pending notification payload. System notification title
    and body text is locally redacted for common passwords, tokens,
    Authorization headers, and private key blocks before reaching the OS
    notification center. Platform initialization, permission requests, and
    notification display now fail closed when the optional notification plugin
    is unavailable, leaving login, assistant, and task state usable.
- `flutter analyze`
  - Passed after restoring readable app shell tab/share text and replacing
    mojibake-prone UI assertions with Unicode-safe test constants.
- `flutter test test/servers_controller_test.dart test/servers_screen_test.dart test/ssh_risk_test.dart test/backend_ssh_command_test.dart --concurrency=1 --reporter compact`
  - Passed: 39 server maintenance widget/model tests.
  - Covers Hub-synced sanitized server profile metadata merging from desktop
    `SSHHosts` with local fallback, the server screen presenting those profiles
    as backend MaClaw GUI/agent-managed server records instead of phone-local
    SSH credential entry, preserving sanitized `source_machine_id` and
    `updated_at` provenance metadata in the phone-side cache, Hub
    refresh/cache clearing with only legacy
    credential-residue cleanup and without writing phone-side SSH secrets,
    existing backend SSH session listing and attach/resume through
    Hub session APIs, interrupt requests queued through Hub for GUI/agent
    Ctrl+C handling, backend GUI/agent task control-record state management,
    backend file operation control-record requests,
    Ctrl+C handling, visible actual worker `claimed_by`, `backend_session_id`, and pending
    input metadata, worker timing metadata, output sequence evidence, and
    sparse realtime-frame merge behavior for GUI/agent-managed sessions,
    deduplicated redacted notifications for backend SSH abnormal realtime states,
    readable high-risk command
    confirmation dialogs,
    manual save/send confirmation, backend session output AI analysis confirmation
    with preview, redacted output, redacted server profile metadata
    before backend session output handoff to a digital employee, backend GUI/agent realtime
    `ssh_session` `output_chunk`/`output_seq` rendering in the mobile backend-session output panel,
    backend session ID propagation from that managed output panel to AI analysis,
    including server name,
    host, username, tag, and note fields, password/Token/private-key warning,
    plus environment-variable and
    long-option secret redaction such as `MYSQL_PWD=...`,
    `AWS_SECRET_ACCESS_KEY=...`, `--password ...`, and quoted values like
    `password="..."`, `API_KEY='...'`, and `--token '...'`; embedded URL
    credentials in `https://`, `postgres://`, `redis://`, and `ssh://`
    payloads; API-boundary redaction after session initialization, command
    draft non-execution, backend session output copy with local clipboard
    redaction before secrets leave the app surface, UI labels for copy/clear
    actions that say backend session output instead of terminal output and no terminal emulator dependency,
    reconnect profile selection, phone-side cached profile cleanup, and SSH risk
    classification, including recursive-force `rm` variants such as `-fr`,
    split `-r -f`, `--` separators, and uppercase `-Rf` flags.
- `flutter analyze`
  - Passed after replacing remaining terminal-shaped mobile icons with
    assistant/server-maintenance icons that reflect GUI/agent backend session
    management instead of a phone-local terminal surface.
- `flutter analyze --no-fatal-infos`
  - Passed on the current mobile workspace with `No issues found`.
- `flutter test --concurrency=1 --reporter compact`
  - Passed: all 344 Flutter tests, including the current login, GUI-like
    assistant, voice input fallback, document, digital employee, backend SSH,
    realtime, notification, and release-evidence coverage.
- `flutter test test\assistant_screen_test.dart test\servers_screen_test.dart test\backend_ssh_command_test.dart --concurrency=1 --reporter compact`
  - Passed: 49 assistant and backend SSH screen/model tests.
  - Revalidated the GUI-like AI assistant, voice/file/camera entries,
    backend-session output panel, GUI/agent session evidence, command drafts,
    high-risk confirmations, and backend SSH handoff helpers after removing
    the last visible terminal iconography from mobile runtime UI.
- `python -m unittest tool\verify_runtime_boundary_test.py`
  - Passed: 12 runtime boundary verifier tests.
  - Revalidated the guard that rejects terminal emulator dependencies/imports,
    phone-local SSH dependencies, phone-side SSH credential APIs, custom Hub
    URLs, redemption-code login, and arbitrary third-party LLM fields.
- `flutter test test\release_docs_test.dart --concurrency=1 --reporter compact`
  - Passed: 33 release documentation integrity tests.
  - Revalidated the mobile `.gitignore` release-doc guard after adding
    `flutter_*.log`, so Flutter tool crash reports such as `flutter_01.log`
    remain local generated artifacts and do not pollute signed-build or QA
    evidence worktrees; `.tmp_*.log` is also ignored so local diagnostic
    transcripts do not enter the mobile evidence worktree.
- `go test ./hub/internal/httpapi -run "TestMobile.*" -count=1`
  - Passed: current Hub mobile API regression suite.
- `go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemptionRouteIsNotExposed|DesktopQRSessionRouteIsNotExposed)|TestSameURLOriginHandlesDefaultPorts" -count=1`
  - Passed: current official HubCenter route and origin regression suite.
- `go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown|TestResolveMobileBackendSSHHost|TestMobileServerProfilesFromSSHHosts|TestProcessMobileBackendSSHSession" -count=1`
  - Passed: current GUI/agent mobile digital-employee, document, server-profile,
    and backend SSH handoff regression suite.
- `flutter test test/mobile_realtime_bridge_test.dart test/mobile_realtime_client_test.dart test/digital_employees_screen_test.dart test/documents_screen_test.dart test/servers_screen_test.dart --concurrency=1 --reporter compact`
  - Passed: 52 focused mobile control-plane tests covering document and digital-
    employee realtime updates, backend SSH output/interrupt/GUI-agent handoff,
    export/import behavior, and safe failure recovery.
- `go test ./hub/internal/httpapi -run "TestMobile.*" -count=1`,
  `go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemptionRouteIsNotExposed|DesktopQRSessionRouteIsNotExposed)|TestSameURLOriginHandlesDefaultPorts" -count=1`,
  and the current GUI mobile handoff regression command above
  - Passed: Hub mobile API, official HubCenter route isolation, origin handling,
    and GUI/agent mobile handoff regression suites.
- `python tool/configure_platforms.py`, `python tool/verify_ios_wrapper.py`,
  and `python tool/verify_runtime_boundary.py`
  - Passed: native iOS wrapper generation, iOS wrapper structure, and the
    mobile runtime boundary checks.
- Signed Android API 35 cold-start evidence was refreshed after the login UI
  change. The hierarchy contains the MaClaw logo and phone registration entry,
  and contains no `HubCenter` candidate label or preset HubCenter URL. The
  three official candidates remain internal to background discovery.
- Current API 35 emulator permission evidence is retained under
  `docs/qa-builds/android-api35-0.1.0+1/debug-latest/permissions-grant.txt`:
  `permission-grant:<id>` succeeded for notifications, camera, microphone,
  image media, and video media, and `dumpsys package` reports each as granted.
  This is emulator evidence only and does not close the physical-device
  permission gate.
- The current Debug APK Manifest and API 35 resolver evidence are retained
  under `docs/qa-builds/android-api35-0.1.0+1/debug-latest/`: `debug-manifest.txt`
  records the mobile permissions and share filters, while the text, image, PDF,
  Word/DOCX, Excel/XLSX, CSV, and `SEND_MULTIPLE` CSV resolver transcripts all
  include `top.mypapers.maclaw.mobile/.MainActivity`. This remains emulator
  resolver evidence, not physical-device share QA.
- The same Debug APK cold-start capture (`startup.png`/`startup.xml`) contains
  the `MaClaw` image content description and no `HubCenter` preset URL or
  Flutter branding; the phone-login label is present in the Android hierarchy
  (the emulator XML is UTF-8 text rendered through the platform accessibility
  tree).
- `flutter test test\app_smoke_test.dart --concurrency=1 --reporter compact`
  - Passed: 15 app smoke tests, including signed-out shared-link,
    shared-document, and opened-notification paths. Shared content and task
    notifications remain pending while the phone registration screen is shown,
    then are consumed by the AI assistant, document flow, or notification
    recovery only after the session becomes authenticated.
- The latest signed Android API 35 `ACTION_SEND` text flow was rechecked:
  Android Resolver listed `Share with MaClaw Mobile`, `Just once` delivered the
  intent to the MaClaw Mobile activity, and the resulting phone registration
  hierarchy contained no HubCenter candidate address. The capture remains
  emulator evidence and does not replace real-device share QA.
- The current signed APK also received a direct `ACTION_SEND` text payload
  after installation; the activity launched successfully and the assistant
  displayed the shared content with a `分享来源` action. Evidence is
  `share-text-latest.xml`/`share-text-latest.png` under
  `docs/qa-builds/android-api35-0.1.0+1/signed-release-chat-latest/`. This
  verifies the signed text-share handoff on the emulator, not all required
  physical-device share payloads.
- A signed-package URL share was also delivered directly to the running
  activity; the assistant created a follow-up turn, preserved the URL as a
  citation fallback, and showed the received-share state. Evidence is
  `share-url-latest.xml`/`share-url-latest.png` in the same QA directory.
- A signed-package image share was delivered through an Android MediaStore
  `content://` URI, matching the URI form used by real sharing applications.
  The app routed it to the `文档` tab and exposed the file/camera/gallery
  import surface. Evidence is `share-image-latest.xml`/`share-image-latest.png`
  in the same QA directory. This closes the signed-emulator image-share
  smoke only; physical-device and Office/CSV share gates remain open.
- Assistant camera, gallery, and file-import success feedback now preserves the
  original mobile prompt and adds a second `upload task <taskId>` line when Hub
  returns a document upload task. This keeps the image/document handoff
  traceable from the assistant surface into the Documents tab; the behavior is
  covered by `test/assistant_screen_test.dart` and remains separate from real
  device task-ID evidence.
- Speech-service platform errors are now forwarded through the voice adapter as
  an explicit assistant error state. The composer exits listening mode and
  remains available for typed input instead of waiting indefinitely; the Widget
  regression is `assistant voice input exits listening mode after platform error`
  in `test/assistant_screen_test.dart`.
- Voice-session callbacks now carry a generation token. A late `done` or
  `notListening` callback from an older platform session cannot change the
  state or transcript of a newer session; typed input remains available when
  speech startup fails. Static analysis and the focused assistant/voice/smoke
  suite passed after this change.
- The account feature summary now distinguishes always-available local task
  notifications from optional remote Push. When Hub bootstrap has
  `push_notifications:false`, the account page shows `本地通知` as enabled and
  does not present a misleading enabled/disabled remote Push chip; the account
  Widget regression passes.
- Rebuilt the latest signed Android APK after the mobile UI and employee-result actions.
  The current artifact SHA256 is
  `32E2D660B5C729B006F88D80A287DA4AB6C8F7C210E0F5086A0C99FDA65C73F7`,
  size `78516958` bytes; the Flutter full-suite passed for this build. The
  API 35 hierarchy/install capture below remains emulator evidence from the
  prior installed QA build and does not close physical-device QA. The fresh
  hierarchy capture is
  `startup-notification-boundary-20260711.xml`/`startup-notification-boundary-20260711.png` under
  `docs/qa-builds/android-api35-0.1.0+1/signed-release-chat-latest/`; it shows
  the MaClaw image and phone registration surface with no Flutter branding or
  HubCenter candidate URLs.
- The same current signed APK resolver run matched
  `top.mypapers.maclaw.mobile/.MainActivity` for `text/plain`, `image/*`,
  `application/pdf`, DOCX, XLSX, and `text/csv` `ACTION_SEND` payloads. The
  current query transcripts are `share-text-current-activity.txt`,
  `share-image-current-activity.txt`, `share-pdf-current-activity.txt`,
  `share-docx-current-activity.txt`, `share-xlsx-current-activity.txt`, and
  `share-csv-current-activity.txt` under the same signed-release QA directory.
  This is signed-emulator resolver evidence; real-device share delivery remains
  an open release gate.
- The current signed APK also received
  `https://example.com/maclaw-current-share` through a direct URL-style
  `ACTION_SEND` Intent. The cold-start capture is
  `share-url-current.xml`/`share-url-current.png` in the signed-release QA
  directory; it shows the MaClaw phone-registration surface without Flutter
  branding or HubCenter candidates. Because this emulator run was signed out,
  the URL remained in the authenticated-session pending-share path; logged-in
  assistant consumption still requires the real phone/Hub smoke and is not
  claimed by this capture.
- Rebuilt the matching signed Android App Bundle for version/build `0.1.0+1`.
  The current AAB SHA256 is
  `2CFC03B27E53A3B0FD730352DD88B8BF1744F9046C11F17F01B6169D1DDF1875`,
  size `57553887` bytes, and `jarsigner -verify` returned success. APK and AAB
  use the same internal QA signing identity and remain internal-testing
  artifacts.
- Account-screen notification permission results now expose a generated
  `permission-grant:<id>` trace token without phone numbers, Hub URLs, or
  credentials. The service and Account Widget coverage verifies both granted
  and denied outcomes; this token makes a real permission prompt/result easier
  to attach to the signed QA record and is not itself proof of device grant.
- Successful assistant voice, camera, gallery, and file-import flows now expose
  scoped `permission-grant:microphone-*`, `permission-grant:camera-*`, or
  `permission-grant:media-*` evidence tokens alongside their status/result.
  These IDs only correlate a successful app operation with the QA capture; they
  do not replace the platform permission prompt or real-device grant record.
- Direct imports from the `文档` tab now expose the same scoped media/camera
  evidence on the upload task card; shared-file intents intentionally do not
  fabricate a permission token. Document import state and retry tests remain
  green after this change.
- The latest signed APK also opened the account page, scrolled to the
  notification permission action, and completed the Android 13+ permission
  request. The post-request hierarchy reports `通知权限已开启，长任务和 SSH
  异常会提醒你`; evidence is
  `notification-permission-latest.xml`/`notification-permission-latest.png`
  in the same QA directory. This remains emulator permission evidence.
- The signed assistant then opened the system camera through `拍照提问` and
  returned to the assistant after the camera activity was dismissed. The
  The latest permission-trace signed APK also received a real Android
  `ACTION_SEND` URL intent (`https://example.com/maclaw-latest`) and returned to
  the MaClaw phone-login screen without Flutter branding. Captures are
  `share-url-permission-latest.xml` and `share-url-permission-latest.png` in
  `signed-release-chat-latest/`; this remains signed-emulator evidence.
  runtime permission snapshot records `CAMERA`, `RECORD_AUDIO`,
  `READ_MEDIA_IMAGES`, `READ_MEDIA_VIDEO`, and `POST_NOTIFICATIONS` as granted;
  evidence is `assistant-after-camera-latest.png`,
  `assistant-after-camera-latest.xml`, and `permissions-runtime-latest.txt`.
  This is emulator permission/entry-point evidence, not physical-device QA.
- `flutter test test/desktop_llm_qr_test.dart test/account_screen_test.dart --concurrency=1 --reporter compact`
  - Passed: desktop GUI QR authorization tests, including scanner/paste flow,
    malformed payload rejection, provider-secret field rejection, and local
    expiry validation before Hub authorization.
## Manual Release Gates

- A real phone-number SMS/Hub smoke was completed on 2026-07-10 using the API
  35 Android emulator. The masked account ending in `0398` completed SMS
  verification, opened the GUI-like AI assistant, and showed the discovered
  Hub, `tenant_default`, MaClaw official LLM mode, and masked phone credits
  status. Evidence is stored under
  `docs/qa-builds/real-phone-hub-smoke-20260710/`; it contains no SMS code or
  token. This reduces the Hub discovery smoke gap but does not replace signed
  physical-device, iOS, or real-server QA.

- The current GUI-like chat/voice-input implementation was rebuilt as a clean
  signed Android Release APK. Artifact evidence is stored under
  `docs/qa-builds/android-api35-0.1.0+1/signed-release-chat-latest/`.
  SHA256: `CDF64E473958269ED25212A26F04CA71311296D94E065FC47E32CDDB3F4B7674`.
  The stable internal-QA certificate allowed the APK to replace the existing
  emulator installation successfully. The fresh-start and startup captures
  below are attributed to this artifact. The separate
  `voice-listening.xml`/`voice-listening.png` capture shows the active
  “正在听写，识别结果会填入 AI 助手输入框” state; this remains
  emulator evidence from the signed QA session and does not close
  physical-device QA.

- The rebuilt stable-signing APK was installed on a clean Android API 35
  emulator. Cold start evidence confirms the MaClaw logo, `手机号注册/登录`,
  the official-credits explanation, and `发送验证码`; no Flutter placeholder
  branding is present. Evidence is `fresh-start-latest.xml` and
  `fresh-start-latest.png` in the same QA directory. This is clean-emulator
  launch evidence; SMS/Hub verification still requires the documented account
  smoke flow.
  After the speech-status callback change, the APK was rebuilt and installed
  successfully over the signed API 35 session; the latest cold-start capture
  is `startup-chat-latest.xml`/`startup-chat-latest.png` and still exposes the
  GUI-like chat, `主对话`, secondary-tab, and voice-input entry points.

- The same signed API 35 session opened the remote maintenance route after the real phone
  login. Its UI hierarchy confirms that server profiles are synchronized from
  the official Hub's MaClaw GUI/agent authorization, that the phone does not
  collect or connect with SSH credentials, and that the empty state offers
  only backend-session request/refresh actions. No remote command was issued;
  evidence is `remote-screen.png` and `remote-screen.xml` in the same QA
  directory. This verifies the mobile control-plane boundary, not a real
  server execution or GUI/agent attachment.

- After the late `done` callback race guard, the previously signed Release APK
  was built with SHA256
  `B2C53995602A39D8F14E7A99725206972EF08C1B51CA7C7E4F26C2C4A0F5AF02`.
  It was then installed successfully on the API 35 emulator. The runtime
  capture `voice-completed-latest.xml`/`voice-completed-latest.png` shows the
  platform speech session completing automatically, the voice action returning
  to `开始语音输入`, and the completed-transcript status being shown. This is
  emulator evidence and does not close physical-device QA.

- The latest signed session also opened the `员工` tab through the discovered
  Hub/tenant and returned live digital-employee data. The hierarchy shows
  multiple online employees, `远程端在线`, `按需唤起`, and the mobile actions
  `发起任务` and `分析日志/输出`. No task was submitted during this smoke;
  evidence is `digital-employee-latest.xml`/`digital-employee-latest.png` in
  the same QA directory. This verifies the Hub-backed mobile entry point, not
  a real server task execution.
- Opening the first employee's task form shows explicit `服务器`/`电脑`/`文档`/
  `核查` task types, server-oriented emergency templates, a checked safety
  option that keeps high-risk commands as drafts, and a separate `提交任务`
  action. The form was closed without submission; evidence is
  `digital-task-form-latest.xml`/`digital-task-form-latest.png` in the same QA
  directory. This verifies the mobile authorization boundary, not execution.

- Current continuation verification also passed the targeted Hub mobile API
  suite: `go test ./hub/internal/httpapi -run
  'TestMobile(Bootstrap|Search|BackendSSH|RealtimeBackendSSH|SSHAnalyze)'
  -count=1`. The GUI control-plane suite passed with
  `go test ./gui -run
  'Test(MobileDigitalEmployeeCandidateIDs|RemoteHubClient.*Mobile|MobileDocumentSourceMarkdown|ResolveMobileBackendSSHHost|MobileServerProfilesFromSSHHosts|ProcessMobileBackendSSHSession)'
  -count=1`. These are backend contract and worker-handling checks; they do
  not replace a real GUI/agent attached to a real server.

- The latest rerun of both suites passed again after the mobile realtime
  recovery changes. This confirms the control-record contract still covers
  session creation/attach, worker claim and update, queued input, interrupt,
  reconnect, background task/file operations, and SSH analysis; it remains
  contract evidence rather than a real GUI/agent-to-server execution record.
- The backend SSH contract audit also confirmed that Hub assigns the monotonic
  `output_seq` when accepting each non-empty worker `output_chunk`, then
  returns it in the worker response and `ssh_session` Realtime event. The
  desktop worker does not invent a phone-side sequence, so ordering remains a
  Hub-owned invariant; the mobile client additionally ignores stale event
  sequences.

- A current HubCenter rerun was attempted with the documented mobile route
  filter, but the Go build is presently blocked before tests start by the
  unrelated untracked `corelib/embedding/tensor/avx512_kernels_amd64.s`
  assembly file (`invalid instruction ... VMOVSS X28, X0`). The earlier passing
  HubCenter transcript remains recorded above; this continuation does not
  alter or delete that user-owned file.

- `python tool/qa_preflight.py --scope ios --log docs/qa-builds/preflight-ios-0.1.0+1-20260710.log`
  - Current result: `BLOCKED` only because the real Apple Team ID/export
  options are not configured. iOS wrapper, Share Extension, URL schemes,
  App Group wiring, and runtime boundary checks pass; no placeholder
  `ExportOptions.plist` is being created.
- The current rerun is recorded at
  `docs/qa-builds/preflight-ios-current-20260711.log`; it reports the same
  single blocker, missing real `ios/ExportOptions.plist`, while the iOS wrapper,
  Share Extension, runtime boundary, and release-document checks remain OK.
- A further iOS preflight rerun is recorded at
  `docs/qa-builds/preflight-ios-current-rerun-20260710.log`; it again reports
  only the missing real `ios/ExportOptions.plist`. The wrapper verifier and
  the dedicated iOS wrapper/export-options/release-plan suite passed all `27`
  tests, so no placeholder signing configuration was added.
- `python tool/release_status_report.py --scope ios --team-id '<APPLE_TEAM_ID>' --export-method development --log docs/qa-builds/release-status-ios-0.1.0+1-latest-20260710.log --force`
  - Current result: `NOT READY` for the same missing real Apple signing/export
    inputs; the mobile implementation remains in pre-signing setup state.
- `python tool/release_status_report.py --scope android --log docs/qa-builds/release-status-android-0.1.0+1-latest-20260710.log --force`
  - Current result: `NOT READY` because there are zero completed in-scope
    signed-build QA records. This is an intentional release blocker; the local
    signed APK and emulator captures remain preparation evidence only.

- The latest Android release-status rerun is saved as
  `docs/qa-builds/release-status-android-current-final.log`, and the matching
  preflight rerun is `docs/qa-builds/preflight-android-current-final.log`.
  Both continue to report the same single invalid QA record, while signing
  inputs, runtime boundary, release documentation, and current artifact
  consistency checks remain OK. No formal Android release approval is implied.
- The latest final-evidence verifier output is saved as
  `docs/qa-builds/final-release-evidence-android-current-20260711.log`. It
  confirms the release tooling and artifact checks are runnable, but rejects
  the Android scope because the signed QA record still has no completed
  post-SMS-verification LLM usage, real-device permission/share, notification,
  or GUI/Agent-managed SSH evidence.
- The current continuation rerun of the backend contracts passed from the
  repository root: Hub mobile API tests are recorded in
  `docs/qa-builds/go-mobile-httpapi-current-20260711.log`, and GUI/Agent
  mobile SSH/digital-employee handling tests are recorded in
  `docs/qa-builds/go-gui-mobile-current-20260711.log`. Both end with `ok`;
  these remain contract evidence and do not replace a real GUI/Agent session
  attached to a real server.
- The current iOS preflight is recorded in
  `docs/qa-builds/preflight-ios-current-20260711.log`; `verify_ios_wrapper.py`
  passes in `docs/qa-builds/ios-wrapper-current-20260711.log`. The preflight
  has exactly one blocker: the real Apple Team ID/export configuration has not
  produced `ios/ExportOptions.plist`. No placeholder signing/export file was
  added.
- The current scoped release-status transcripts are
  `docs/qa-builds/release-status-android-current-20260711.log` and
  `docs/qa-builds/release-status-ios-current-20260711.log`. Android reports
  signing/runtime/documentation checks OK but rejects the incomplete QA record;
  iOS reports the missing real export options and has no in-scope signed QA
  record. Both correctly remain `NOT READY`.
- The Android handoff and final-evidence verifier were regenerated after the
  latest APK/AAB refresh. The handoff transcript is
  `docs/qa-builds/handoff-android-0.1.0+1-current-20260711.log`; the final
  verifier transcript is
  `docs/qa-builds/final-release-evidence-android-current-20260711.stdout.log`.
  Both retain `NOT READY` because the QA record is incomplete, while the
  handoff itself remains a plan rather than release approval.
- The Android QA record now links the signed API 35 emulator share resolver and
  delivery captures for plain text, URL, image, PDF, Word, Excel, and CSV under
  `signed-release-chat-latest`. The record gap report increased from `44/85` to
  `48/85` filled occurrences; these captures document local emulator behavior
  only and do not satisfy the remaining real-device share/import gates.
- An unauthenticated probe of the actual HubCenter discovery route on
  2026-07-11 is recorded in `docs/qa-builds/hubcenter-probe-20260711.log`.
  `POST /api/entry/resolve` returned an online `https://hub.mypapers.top` from
  `hubs.mypapers.top`; `hubs.maclaw.top` returned `502`, and `hubs2.maclaw.top`
  closed the connection. The discovered Hub's SMS route advertises `POST` via
  an `OPTIONS` probe, while its protected mobile bootstrap and realtime routes
  returned `401`; the SMS POST was intentionally not called. This is partial
  discovery evidence only; SMS verification, tenant authentication, official
  Credits/LLM usage, and real Hub smoke remain open.
- A real phone verification request was sent once through the discovered Hub on
  2026-07-11 and returned HTTP `200`, `purpose=verify_bound_phone`, tenant
  `tenant_default`, six-digit code length, and a five-minute expiry. The
  response did not expose the code; the redacted request evidence is
  `docs/qa-builds/sms-request-20260711.log`. This proves SMS dispatch setup,
  not successful code verification or authenticated LLM usage.

These cannot be proven by local unit tests or the unsigned debug APK:

| Gate | Required evidence |
| --- | --- |
| Android signed internal build | Signed APK/AAB path, SHA256, signing identity, build number, installer channel, and install result on at least one Android 13+ device |
| Android share-to-app | Device log or QA notes showing text, URL, image, PDF, Word, Excel, and CSV shared into MaClaw Mobile |
| Android runtime permissions | QA notes/screenshots for notification, camera, microphone, media/file access, and any platform local-network prompt if applicable, with `permission-grant:<id>` evidence; local-network evidence is not phone-local SSH proof |
| iOS Share Extension target | Xcode project with `top.mypapers.maclaw.mobile.ShareExtension`, official Team ID, provisioning profile, and `group.top.mypapers.maclaw.mobile` enabled for Runner and Share Extension |
| iOS share-to-app | TestFlight or development install notes showing text, URL, image, PDF, Word, Excel, and CSV shared into MaClaw Mobile |
| iOS runtime permissions | QA notes/screenshots for camera, microphone, speech recognition, photo library, local network, and notifications, with `permission-grant:<id>` evidence |
| Backend SSH session against real server | GUI-equivalent backend SSH session management evidence: host type, auth mode, session creation/attach, GUI/agent-bound `backend_session_id`, not phone-local/ad hoc terminal evidence, GUI/agent claim or worker handoff evidence, explicit worker claim/update evidence, `ssh_session` realtime `output_chunk`/`output_seq` evidence tied to the same `backend_session_id`, connect result, read-only command, command output excerpt, interrupt/Ctrl+C evidence through a Hub control record or `/api/mobile/ssh/sessions/{session_id}/interrupt` with GUI/agent Ctrl+C handling, disconnect result, reconnect result, copied backend session output evidence with a GUI/agent evidence line containing actual values for Hub session ID, `backend_session_id`, concrete `claimed_by` worker identity such as `claimed_by desktop-agent-1`, and numeric `output_seq`, AI analysis confirmation tied to the same GUI/agent-bound `backend_session_id`, AI/digital-employee handoff evidence tied to the same `backend_session_id` if used, and phone-side server-profile cache clear confirmation |
| Hub discovery smoke test | Account used, selected HubCenter, discovered Hub, tenant, LLM mode/QR authorization evidence with post-SMS-verification official credits usage record ID, concrete `llm-request-id` and `llm-usage-record`, bootstrap result, cold-start MaClaw logo splash evidence with no Flutter placeholder/template branding, signed-in `AI助手` first-screen evidence with visible `主对话`/secondary-tab controls, microphone/voice input, and no legacy `查信息` entry, AI assistant query with citations, voice transcription, photo/image assistant input, shared result, document draft, document upload/export task IDs, digital employee task ID, realtime status, notification delivery, network offline/recovery, API base URL, and realtime Hub URL confirmation |

## Build Record Template

Create one record per QA build from `docs/qa_build_record_template.md`, then
attach or link the completed record in this section.

<!-- QA_BUILD_RECORD_LINKS_START -->
<!-- QA_BUILD_RECORD_LINKS_END -->
