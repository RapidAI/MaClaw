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
Markdown links to this file.

## Automated Gates

Run these before handing a build to QA:

```bash
go test ./hub/internal/httpapi -run "TestMobile.*" -count=1
go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemption|DesktopQRSession)" -count=1
go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown" -count=1
cd mobile/maclaw_mobile
python3 -m unittest tool/configure_platforms_test.py
python3 -m unittest tool/validate_qa_build_record_test.py
python3 -m unittest tool/create_qa_build_record_test.py
python3 -m unittest tool/validate_qa_build_records_dir_test.py
python3 -m unittest tool/qa_build_record_report_test.py
python3 -m unittest tool/qa_release_evidence_links_test.py
python3 -m unittest tool/qa_preflight_test.py
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
Run `python3 tool/verify_runtime_boundary.py --log docs/qa-builds/runtime-boundary-<version+build>.log`
for the matching runtime-boundary evidence file.
The latest local `python tool\run_release_gates.py` run passed all 36 automated
release gates, including Flutter analysis, the full Flutter test suite, runtime
boundary verification, native wrapper regeneration/configuration, Go mobile API
tests, QA build record scaffold tests, QA record directory validation,
QA build record gap report tests, QA release evidence link helper tests,
QA preflight helper tests, Android signing setup helper tests, release status
report tests, QA/debug/final evidence verifier tests, Android release signing
verification, Android release build helper tests, iOS wrapper verification,
iOS release plan helper tests, iOS export options setup helper tests, and
Android debug APK build.
After a local debug APK build, run `python3 tool/update_debug_apk_evidence.py`
to refresh the artifact path, byte size, and SHA256 recorded below, then run
`python3 tool/verify_debug_apk_evidence.py` to confirm the evidence still
matches the current local `app-debug.apk`.
Before final release approval with completed signed-build QA records, run
`python3 tool/verify_final_release_evidence.py docs/qa-builds` to require
validated Android and iOS evidence records and require this file to link every
validated QA record by filename.

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
| Official HubCenter discovery only | `test/official_service_test.dart`, `test/official_service_surface_test.dart`, `test/auth_service_test.dart`, `test/mobile_realtime_client_test.dart`, `go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemption|DesktopQRSession)"` |
| Mobile API contracts | `test/mobile_api_contract_test.dart`, `go test ./hub/internal/httpapi -run "TestMobile.*"` |
| Native Android/iOS wrapper settings | `tool/configure_platforms_test.py`, `test/platform_permissions_test.dart` |
| Runtime boundary: no embedded Go `corelib`, FFI, gomobile, or native corelib bridge | `tool/verify_runtime_boundary.py`, `tool/verify_runtime_boundary_test.py`, `test/release_docs_test.dart`; signed-build QA can save `--log` output as evidence |
| Signed-build QA record completeness | `tool/validate_qa_build_record.py`, `tool/validate_qa_build_record_test.py`, `docs/qa_build_record_template.md` |
| Signed-build QA preflight, release status, release handoff, record scaffold, gap report, release evidence link helper, and directory validation | `tool/release_status_report.py`, `tool/release_status_report_test.py`, `tool/release_handoff.py`, `tool/release_handoff_test.py`, `tool/qa_preflight.py`, `tool/qa_preflight_test.py`, `tool/create_qa_build_record.py`, `tool/create_qa_build_record_test.py`, `tool/qa_build_record_report.py`, `tool/qa_build_record_report_test.py`, `tool/qa_release_evidence_links.py`, `tool/qa_release_evidence_links_test.py`, `tool/validate_qa_build_records_dir.py`, `tool/validate_qa_build_records_dir_test.py`, `docs/qa-builds/README.md` |
| Automated gate sequence integrity | `tool/run_release_gates.py`, `tool/run_release_gates_test.py` |
| Local debug APK evidence freshness | `tool/verify_debug_apk_evidence.py`, `tool/verify_debug_apk_evidence_test.py`, `tool/update_debug_apk_evidence.py`, `tool/update_debug_apk_evidence_test.py` |
| Signed artifact evidence snippet generation | `tool/signed_artifact_evidence.py`, `tool/signed_artifact_evidence_test.py`, `docs/qa_build_record_template.md`, `docs/qa_device_checklist.md` |
| Android release signing safety and local signed build helper | `tool/setup_android_signing.py`, `tool/setup_android_signing_test.py`, `tool/verify_android_release_signing.py`, `tool/verify_android_release_signing_test.py`, `tool/build_android_release.py`, `tool/build_android_release_test.py`, `android/app/build.gradle.kts`, `android/key.properties.example`, `.gitignore` |
| iOS wrapper, Share Extension wiring, export options, and archive planning | `tool/verify_ios_wrapper.py`, `tool/verify_ios_wrapper_test.py`, `tool/setup_ios_export_options.py`, `tool/setup_ios_export_options_test.py`, `tool/plan_ios_release.py`, `tool/plan_ios_release_test.py`, `ios/ExportOptions.plist.example`, `ios/Runner/Info.plist`, `ios/ShareExtension/Info.plist`, `ios/Runner/Runner.entitlements`, `ios/ShareExtension/ShareExtension.entitlements` |
| Manual release gate documentation parity | `tool/verify_manual_release_gates.py`, `tool/verify_manual_release_gates_test.py`, `docs/release_audit.md`, `docs/release_evidence.md`, `docs/qa_device_checklist.md`, `docs/qa_build_record_template.md` |
| Final signed-build evidence package readiness | `tool/verify_final_release_evidence.py`, `tool/verify_final_release_evidence_test.py`, `docs/release_evidence.md`, `docs/qa-builds/README.md`, `docs/qa_device_checklist.md` |
| Assistant lookup, citations, shared links, voice locale, photo/file handoff | `test/assistant_screen_test.dart`, `test/assistant_retry_test.dart`, `test/mobile_shared_intent_test.dart` |
| Mobile app shell tabs, feature-flag routing, readable navigation labels, and shared-intent route fallback | `test/mobile_feature_flags_test.dart`, `test/app_smoke_test.dart`, `test/mobile_shared_intent_test.dart` |
| Emergency document templates, import, AI actions, edit helpers, export/share UI | `test/documents_screen_test.dart`, `test/documents_state_test.dart`, `test/document_draft_test.dart` |
| Manual SSH profiles, terminal output copy, AI analysis confirmation, high-risk command confirmation, readable safety warnings | `test/servers_screen_test.dart`, `test/servers_controller_test.dart`, `test/ssh_risk_test.dart`, `test/secure_vault_test.dart` |
| Digital employee task submission, authorization messaging, result copy/share, document draft creation | `test/digital_employees_screen_test.dart`, `test/digital_employee_test.dart`, `test/digital_employees_controller_test.dart`, `go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown"` |
| Account settings, notification request entry, cache clearing, credential separation | `test/account_screen_test.dart`, `test/app_preferences_test.dart`, `test/mobile_notification_service_test.dart`, `test/mobile_local_store_test.dart`, `test/secure_vault_test.dart` |
| Realtime document/digital employee updates | `test/mobile_realtime_client_test.dart`, `test/mobile_realtime_bridge_test.dart` |

## Latest Local Verification

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
    service smoke, manual SSH smoke, and evidence-package steps for QA.
  - Checked that the checklist includes text, URL, image, PDF, Word, Excel,
    CSV, runtime permissions, Hub discovery smoke, manual SSH smoke,
    selected HubCenter, discovered Hub, tenant, LLM mode, Share Extension
    bundle ID, and app group.
- `docs/qa_build_record_template.md`
  - Added a per-build evidence template for signed Android/iOS artifacts,
    share-to-app payloads, runtime permissions, Hub discovery smoke,
    realtime status, manual SSH smoke, credential deletion, and final release
    approval.
  - Documents the `docs/qa-builds/` storage location and validator-enforced
    record filename format so copied QA templates keep the same directory
    contract as `docs/qa-builds/README.md`.
- `docs/qa-builds/README.md`
  - Added the signed-build QA record directory instructions, including record
    naming, validator command, and sensitive-data redaction rules.
  - Documented that completed records and attachments are ignored by git by
    default, while `README.md` remains tracked as the directory contract; the
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
    key blocks, `password=`/`token=`/`api_key=` assignments, and common literal
    API token formats; records should use redacted evidence or attachment IDs.
  - Checks that required manual-gate fields are filled, that the selected
    HubCenter is one of the three official presets, that the candidate list
    contains exactly the three official HubCenters, and that Hub/API/realtime
    URLs are HTTPS.
  - Verifies that `Discovered Hub URL` is a tenant Hub URL rather than one of
    the official HubCenter URLs.
  - Rejects placeholder build identity, branch, signing, installer, tenant,
    account, tester, Flutter version, and approver values, requires a trackable
    git branch name plus Flutter SDK semver, and requires the MaClaw account plus
    tenant ID to be trackable identifiers so signed-build records remain
    auditable.
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
    desktop GUI QR authorization as `not-used-official-mode`, while
    `desktop_qr_third_party` records must include a trackable QR authorization
    ID, and the LLM access evidence must match the selected mode.
  - Requires manual device evidence fields, including install results, share
    payloads, permissions, Hub smoke, and SSH smoke results, to contain
    auditable notes instead of placeholders such as `ok`, `yes`, or `done`.
  - Requires manual SSH smoke fields to describe the specific tested action:
    host type, auth mode, connect result, read-only command, command output
    excerpt, disconnect result, reconnect result, and copied output evidence.
  - Requires SSH AI analysis evidence to mention preview confirmation and the
    sensitive-data warning, requires AI result evidence to include explanation,
    command drafts, and manual/not-auto-executed proof, and requires credential
    deletion evidence to mention cleared password/private-key or secure-storage
    state.
  - Requires status polling and realtime update evidence to identify task/job or
    document status changes and reference a recorded task/job ID, and requires
    digital employee handoff warnings to mention Hub/tenant confirmation plus
    SSH terminal or pasted/copied output context when recorded.
  - Requires notification delivery evidence for document/export completion,
    digital employee task completion, and SSH abnormal/disconnect scenarios,
    including payload or tap/open target proof.
  - Requires network offline/recovery evidence to show HubCenter/network
    unavailable warnings and restored search, document, digital employee, or
    realtime service behavior after connectivity returns.
  - Requires account-screen, no-custom-Hub-URL, and bootstrap smoke evidence to
    explicitly mention selected Hub, tenant, absent custom Hub URL settings,
    user/quota, feature flags, and service status.
  - Requires account privacy and local-data evidence to record theme/speech
    language changes, local work-record clearing, retained server credentials
    after local reset, and separate explicit server/SSH credential clearing.
  - Requires HubCenter probe evidence to list the exact three official
    HubCenters, discovered Hub evidence to include Hub URL plus tenant, and LLM
    evidence to identify MaClaw official access or desktop GUI QR third-party
    authorization.
  - Requires LLM setup surface evidence to prove mobile only exposes MaClaw
    official service redemption and MaClaw desktop GUI QR authorization, with no
    arbitrary third-party endpoint, base URL, provider URL, or API key fields.
  - Requires document upload task IDs to identify document upload/import tasks,
    and PDF/Word/Markdown export job IDs to match the requested export format.
  - Requires exported document share evidence to prove PDF, Word, and Markdown
    exports were downloaded/saved and handed to a share target or local path.
  - Requires share-to-app payload evidence to describe the expected mobile
    routing: shared text/URLs open the assistant with citations where
    applicable, while files/images enter document import/upload tasks with the
    expected payload format named.
  - Requires AI citation smoke evidence to include a visible HTTPS source URL,
    and shared-result evidence to name the share target or concrete output such
    as Mail, WeChat, clipboard, exported file, or saved local path.
  - Requires mobile assistant input evidence to prove both voice transcription
    and photo/image assistant input, with a resulting cited answer or document
    upload task ID.
  - Requires search-result document draft evidence to cover every first-version
    template: notice, report, email, proposal, meeting minutes, and statement.
  - Requires runtime permission fields to describe prompt/result evidence and
    the matching feature scenario, such as camera/photo import, voice
    microphone/speech, media/file import, local-network SSH, photo library, or
    notification entry point.
  - Requires signed-build device evidence to name device model plus Android/iOS
    OS version, with both Android and iOS devices represented and at least one
    Android 13+ device recorded.
  - Requires the first `Device model / OS` entry to be the Android QA device and
    the second entry to be the iOS QA device, matching the QA template sections.
  - Validates optional attachment, known issue/waiver, and digital employee
    handoff warning evidence when those optional QA fields are used; handoff
    warnings must identify Hub/tenant confirmation and SSH terminal or
    pasted/copied output context, while attachment evidence fields must
    reference a traceable evidence file or attachment ID.
  - Requires `Date` and `Approval date` to be valid `YYYY-MM-DD` calendar
    dates that are not in the future, requires `Approval date` to be on or
    after `Date`, requires `Approved by` to be different from `Tester`,
    requires `Git commit` to be a 7-40 character hexadecimal commit SHA, and
    document/export/digital employee task IDs to be trackable values rather
    than placeholders such as `ok`; digital employee task IDs must identify
    digital employee tasks rather than generic task IDs.
  - Requires final release decision fields to explicitly say passed or waived
    instead of accepting ambiguous values such as `ok` or `yes`; waiver
    decisions must include a reason and a `Known issues / waivers` summary
    that names each waived final gate.
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
- `tool/qa_build_record_report.py`
  - Added a grouped gap report for in-progress signed-build QA records.
  - Reuses `tool/validate_qa_build_record.py` validation rules and reports
    path/filename issues, secret redaction failures, missing or invalid
    evidence fields, and local artifact hash mismatches without relaxing final
    release evidence requirements.
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
  - Passed: 22 release documentation integrity tests.
  - Covers release doc cross-links, signed-build manual gates, required
    share/permission payloads, service/SSH smoke steps, and explicit remaining
    blockers.
  - Locks manual SSH smoke documentation to action-specific evidence fields:
    host type, auth mode, connect result, read-only command, command output
    excerpt, disconnect result, reconnect result, and copied output evidence.
  - Covers the user guide's mobile product decisions: MaClaw logo startup,
    configured-session assistant entry, LLM setup through official redemption or
    MaClaw GUI QR, multi-tab assistant, official HubCenter discovery only, and
    no arbitrary third-party LLM endpoint setup.
  - Verifies the release documentation corpus preserves readable Chinese
    navigation labels and contains no known mojibake or replacement markers.
  - Verifies the mobile `.gitignore` excludes generated Flutter, Gradle,
    Kotlin, Android local-properties, iOS ephemeral/Pods, and IDE module
    artifacts so local release gates do not leave cache noise in the worktree.
  - Covers mobile CI manual triggering and workflow self-path coverage.
  - Verifies the release checklist CI command order matches the release gate
    runner sequence.
  - Verifies the automated gate command block in this evidence document matches
    the release gate runner sequence.
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
  - Verifies the QA build record directory README documents record naming,
    validator usage, and sensitive-data redaction requirements.
  - Covers QA build record template links and manual-gate evidence fields.
- `flutter test test/documents_screen_test.dart --plain-name "documents screen explains ready export sharing" --concurrency=1 --reporter expanded`
  - Passed.
  - Covers ready document export download, generated local file path handoff,
    and system file-share invocation with the draft title.
- `flutter test test/official_service_test.dart test/api_client_test.dart test/documents_state_test.dart --concurrency=1 --reporter compact`
  - Passed: 19 official service, API client, and document state tests.
  - Covers document export download URL safety: relative paths resolve against
    the discovered Hub, same-Hub absolute URLs are accepted, external absolute
    URLs are rejected before a request is sent, same host with the wrong scheme
    is rejected, and export-completion notification payloads fall back to the
    export job ID when the returned download URL is not on the discovered Hub.
- `flutter test test/documents_screen_test.dart --concurrency=1 --reporter expanded`
  - Passed: 10 document workflow widget tests.
- `flutter test test/assistant_screen_test.dart --plain-name "assistant result can become every document template with citations" --concurrency=1 --reporter expanded`
  - Passed.
  - Covers turning an AI lookup result with citations into every mobile
    document template type: notice, report, email, proposal, meeting minutes,
    and statement.
- `flutter test test/assistant_screen_test.dart --concurrency=1 --reporter expanded`
  - Passed: 13 assistant workflow widget tests.
- `flutter test test/mobile_shared_intent_test.dart --concurrency=1 --reporter expanded`
  - Passed: 8 shared-intent classification tests.
  - Covers routing shared PDF, Word, Excel, and CSV files into the document
    import flow instead of the AI text lookup flow.
  - Covers file and image shares that include a URL in the platform message:
    the URL remains available as context, but the payload still enters document
    import instead of being misrouted to assistant lookup.
- `flutter test test/platform_permissions_test.dart --concurrency=1 --reporter expanded`
  - Passed: 4 platform wrapper tests.
  - Covers Android share MIME declarations and iOS Share Extension activation
    declarations for text, web URLs, files, and images.
  - Covers readable UTF-8 iOS privacy usage descriptions for camera,
    microphone, speech recognition, photo library, and local network access,
    and rejects known mojibake/replacement markers.
- `python tool\configure_platforms_test.py`
  - Passed: 14 platform configuration tests.
  - Covers cleanup of Flutter's generated widget-test template so
    native wrapper regeneration does not introduce stale `MyApp` analyzer
    failures.
- `python -m unittest tool\validate_qa_build_record_test.py`
  - Passed: 107 QA record validator tests.
  - Covers incomplete template rejection, completed record acceptance,
    HubCenter discovery enforcement, exact HubCenter candidate enforcement,
    tenant Hub versus HubCenter URL separation,
    duplicate Android/iOS field enforcement and platform order,
    required field overfill rejection,
    duplicate formatted-field rejection,
    fixed identity validation, LLM mode/QR/evidence consistency, Android artifact path validation, signed install/launch/platform evidence, debug artifact/signing/installer rejection, trackable signing alias/certificate evidence, local artifact
    SHA256 matching, iOS archive/TestFlight build identity, Team ID, and provisioning profile
    auditability including rejection of bare `UUID` words without actual profile
    IDs/files/names and acceptance of trackable UUID, `.mobileprovision`, or
    profile-name evidence, build identity/branch/signing placeholder rejection,
    Android installer channel platform enforcement,
    git branch name format enforcement,
    Flutter SDK semver enforcement,
    MaClaw account email/account-ID and tenant ID format enforcement,
    API/realtime URL origin matching against the discovered Hub,
    HubCenter login/probe/discovered Hub tenant/LLM access evidence,
    account-screen/custom-Hub/bootstrap official-boundary evidence,
    document upload/export format-specific task/job evidence and digital employee task ID semantics,
    AI search query/citation URL/shared-result target/voice-photo input/document-draft
    smoke evidence semantics, manual and optional evidence placeholder rejection,
    SSH smoke action-specific evidence validation, SSH AI analysis preview/sensitive-data warning, command draft/manual execution, and credential deletion evidence,
    status polling/realtime task update ID linkage and digital employee handoff warning evidence,
    runtime permission prompt/result and feature-scenario evidence validation,
    iOS URL scheme evidence requiring both `maclaw` and `ShareMedia`,
    share-to-app expected-flow and payload-format evidence validation,
    Android/iOS device OS evidence validation including Android 13+ coverage and
    Android-then-iOS section order,
    QA template field coverage against required/optional validator fields plus
    required field occurrence counts, UTF-8 BOM handling for
    Windows-edited records, known field
    names containing URLs, README/template/directory/non-Markdown path
    rejection, `qa-builds` filename format/date/version matching, raw secret
    redaction enforcement, final decision passed/waived semantics, required
    independent approver enforcement, waiver reasons and per-gate waiver summaries, English
    waiver wording, UTF-8 Chinese approval/waiver wording (`通过`, `已通过`,
    `批准`, `豁免`), date ordering, date/commit/task ID auditability,
    version/build number format enforcement, rejection of unedited final-decision
    placeholders, CLI validation failure output, and raw-secret CLI failure
    messaging.
- `python -m unittest tool\create_qa_build_record_test.py`
  - Passed: 8 QA build record scaffold tests.
  - Covers validator-compatible record filenames, date/version prefilling,
    optional Final Release Decision evidence prefilling, overwrite protection,
    forced regeneration, invalid version/build rejection, and the printed
    validation hint.
- `python -m unittest tool\validate_qa_build_records_dir_test.py`
  - Passed: 6 QA build records directory validator tests.
  - Covers scanning completed Markdown records under `docs/qa-builds/`,
    skipping `README.md`, ignoring non-Markdown evidence attachments, empty
    directories before signed QA records exist, missing/non-directory path
    rejection, and per-record failure summaries.
- `python -m unittest tool\qa_build_record_report_test.py`
  - Passed: 7 QA build record report tests.
  - Covers passing completed records, missing evidence summaries, invalid
    HubCenter values, filename errors, missing handoff/runtime-boundary/release
    gate evidence, local artifact SHA256 mismatches, and CLI stdout/stderr
    behavior.
  - Covers release handoff evidence hints that include the required
    `--version <version+build>`, `--team-id <APPLE_TEAM_ID>`, and
    `--export-method <export-method>` inputs.
- `python -m unittest tool\qa_release_evidence_links_test.py`
  - Passed: 5 QA release evidence link helper tests.
  - Covers empty QA record directories, valid record Markdown link output,
    invalid record exclusion, invalid-record CLI failure behavior, and valid
    record CLI output.
- `python -m unittest tool\qa_preflight_test.py`
  - Passed: 10 QA preflight helper tests.
  - Covers ready summaries, Android signing input blockers, iOS wrapper
    blockers, missing or invalid iOS export options blockers, invalid existing
    QA records, missing QA record directories, and CLI stdout/stderr behavior.
  - Covers optional iOS Team ID/export method mismatch reporting through both
    the validator and CLI preflight entry point.
  - Covers the preflight hint for the required release handoff `--version`,
    `--team-id`, `--output`, runtime-boundary log, and release-gates log flow.
- `python -m unittest tool\setup_android_signing_test.py`
  - Passed: 6 Android signing setup helper tests.
  - Covers environment-variable validation, debug-keystore rejection, local
    `android/key.properties` writing, overwrite protection, successful CLI
    setup, and missing-environment CLI errors.
- `python -m unittest tool\release_status_report_test.py`
  - Passed: 5 release status report helper tests.
  - Covers grouped not-ready output, ready output, current empty-fixture CLI
    failure, and ready CLI stdout behavior.
  - Covers not-ready next actions that include the required release handoff
    `--version`, `--team-id`, `--export-method`, and `--output` arguments.
  - Covers optional iOS Team ID/export method CLI argument forwarding to the
    status builder.
- `python -m unittest tool\release_handoff_test.py`
  - Passed: 9 release handoff helper tests.
  - Covers blocker summaries, ready output, operator command generation, output
    file writing, and blocked/ready CLI exit codes.
  - Covers real release version/build, Apple Team ID, and iOS export method CLI
    validation before generating copyable signed-build evidence commands.
  - Covers forwarding the handoff Team ID/export method into release status
    preflight and printing the matching release status command in the handoff
    sequence.
  - Covers the full copyable command sequence order from status preflight
    through signed artifact evidence, QA record creation, record validation,
    evidence links, directory validation, and final release evidence
    verification.
  - Covers handoff output overwrite protection and explicit `--force`
    regeneration when a saved handoff evidence file already exists.
- `python3 tool/validate_qa_build_records_dir.py docs/qa-builds`
  - Passed: 0 completed signed-build QA record(s) currently present in the
    ignored local records directory.
- `python -m unittest tool\run_release_gates_test.py`
  - Passed: 13 release gate runner tests.
  - Covers gate order, working directories, critical command coverage, and
    numbered dry-run output.
  - Covers dry-run and full-run `--log` output plus overwrite protection and
    explicit `--force` regeneration so signed-build QA can attach automated-gate
    transcripts to completed records.
  - Covers Windows batch-command executable resolution for tools such as
    `flutter.BAT` while keeping documented commands readable as `flutter ...`.
  - Covers CI workflow, release checklist, and release evidence parity for
    automated gate commands and command order, plus debug APK artifact upload.
- `python -m unittest tool\verify_debug_apk_evidence_test.py`
  - Passed: 6 debug APK evidence verifier tests.
  - Covers debug APK evidence parsing, duplicate build-command mentions,
    relative artifact paths, missing artifacts, size mismatches, SHA256
    mismatches, and missing evidence fields.
- `python -m unittest tool\update_debug_apk_evidence_test.py`
  - Passed: 6 debug APK evidence updater tests.
  - Covers artifact path, size, SHA256 refresh, preserving surrounding evidence
    lines, missing artifact failures, missing section failures, and CLI update
    behavior.
- `python -m unittest tool\signed_artifact_evidence_test.py`
  - Passed: 15 signed artifact evidence helper tests.
  - Covers Android signed APK hash/size snippets, record-relative paths, debug
    artifact rejection, untrackable names, missing artifacts, Android
    version/build format validation, required Android version/build, signing
    identity, and installer channel evidence at both function and CLI layers,
    iOS archive metadata snippets, explicit TestFlight build snippets, iOS Team
    ID/profile validation with trackable UUID, `.mobileprovision`, and profile
    name references, CLI output, and CLI error reporting.
- `python -m unittest tool\verify_manual_release_gates_test.py`
  - Passed: 6 manual release gate parity tests.
  - Covers the canonical manual release gate list, release audit remaining
    blockers, QA device checklist execution steps, and QA build record final
    decision fields so signed-build, real-device, Hub discovery, and SSH manual
    gates stay aligned across release documentation.
- `python -m unittest tool\verify_final_release_evidence_test.py`
  - Passed: 9 final release evidence verifier tests.
  - Covers the final release evidence package rule that completed signed-build
    QA records must validate successfully and cover both Android and iOS before
    approval, and that this release evidence document links every validated QA
    record with a `docs/qa-builds/...` Markdown link, while ordinary development
    gates may still pass with an empty `docs/qa-builds/` directory.
- `python -m unittest tool\verify_android_release_signing_test.py`
  - Passed: 7 Android release signing verifier tests.
  - Covers the Gradle release signing guard, rejection of debug-key fallback,
    required `android/key.properties` loading, tracked
    `android/key.properties.example` placeholder coverage, and `.gitignore`
    rules for local keystore material.
- `python -m unittest tool\build_android_release_test.py`
  - Passed: 9 Android release build helper tests.
  - Covers local `android/key.properties` validation, missing signing input
    errors, debug-keystore rejection, APK/AAB release command construction,
    build-name/build-number forwarding, required paired version/build
    traceability, dry-run behavior, and Flutter build failure reporting without
    traceback, plus artifact path selection for signed QA packages.
- `python -m unittest tool\verify_ios_wrapper_test.py`
  - Passed: 6 iOS wrapper verifier tests.
  - Covers readable iOS permission usage descriptions, Runner and Share
    Extension app-group entitlements, Share Extension activation rules for
    text, URLs, files, and images, receive_sharing_intent controller wiring,
    and the generated-project marker.
- `python -m unittest tool\plan_ios_release_test.py`
  - Passed: 9 iOS release plan helper tests.
  - Covers Apple Team ID validation, Xcode archive/export command planning,
    export method validation, local export-options readiness failures,
    export-options Team ID/method mismatch reporting, wrapper readiness
    failures, and the QA record evidence fields needed for `.xcarchive` or
    TestFlight signed builds.
- `python -m unittest tool\setup_ios_export_options_test.py`
  - Passed: 6 iOS export options setup helper tests.
  - Covers Team ID normalization, export method validation, local
    `ios/ExportOptions.plist` generation, overwrite protection, CLI output,
    next-step archive planning hints that preserve the chosen export method,
    and invalid Team ID CLI failures.
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
- `python tool\plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development`
  - Manual macOS signed-build preparation command.
  - Validates generated iOS wrapper readiness and local `ios/ExportOptions.plist`
    readiness, then prints Xcode archive/export commands plus Runner bundle ID,
    Share Extension bundle ID, app group, Team ID, and archive/TestFlight
    evidence fields for the QA build record.
- `go test ./hub/internal/httpapi -run "TestMobile.*" -count=1`
  - Passed; revalidated on the current worktree after the export download URL
    safety updates.
- `go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemption|DesktopQRSession)" -count=1`
  - Passed; revalidated on the current worktree after the export download URL
    safety updates.
  - Covers official mobile service redemption resolving the user's Hub/tenant
    without issuing a token from HubCenter, unknown redemption rejection, and
    rejection of legacy provider-only desktop LLM QR payloads at HubCenter.
- `go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown" -count=1`
  - Passed; revalidated on the current worktree after the export download URL
    safety updates.
- `python -m unittest tool\configure_platforms_test.py`
  - Passed: 14 platform configuration tests.
- `python -m unittest tool\validate_qa_build_record_test.py`
  - Passed: 107 QA build record validator tests.
- `python -m unittest tool\create_qa_build_record_test.py`
  - Passed: 8 QA build record scaffold tests.
- `python -m unittest tool\verify_runtime_boundary_test.py`
  - Passed: 6 runtime boundary verifier tests.
  - Covers the current mobile runtime source tree and negative fixtures for
    Dart FFI, dynamic-library loading, and Go `corelib` references while
    ignoring docs/tests-only mentions.
  - Covers success and violation `--log` output plus overwrite protection and
    explicit `--force` regeneration for signed-build QA evidence.
- `python -m unittest tool\run_release_gates_test.py`
  - Passed: 13 release gate runner tests.
- `python tool\verify_runtime_boundary.py`
  - Passed.
  - Confirms current mobile runtime source under `lib`, `android`, `ios`, and
    `pubspec.yaml` does not embed or bridge Go `corelib`.
- `flutter test test/mobile_local_store_test.dart test/assistant_screen_test.dart test/digital_employees_screen_test.dart --concurrency=1 --reporter compact`
  - Passed: 24 focused storage, assistant, and digital employee widget/model
    tests.
  - Covers SQLite-backed mobile local cache, legacy JSON migration, shared-link
    assistant search, multi-tab assistant behavior, voice/file/camera handoff,
    digital employee task submission, and recent task restore.
- `flutter analyze`
  - Passed: no issues found; revalidated on the current worktree after the
    local-store concurrent open fix.
- `flutter test --concurrency=1`
  - Passed: 190 tests.
  - No Drift debug-only multiple-database warning was emitted after adding the
    local-store concurrent open gate and isolating digital-employee widget
    history providers.
- `flutter build apk --debug`
  - Passed.
  - Artifact: `build\app\outputs\flutter-apk\app-debug.apk`.
  - Size: `227304480` bytes.
  - SHA256: `406026B4E76322D82416AB68AA771447CA644CFBA79F8A6474D57EFA5D295DEB`.
  - Refreshed after enforcing discovered-Hub export download URL safety and
    notification payload fallback behavior.
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
  - Passed: 19 app shell, startup, feature flag, and shared-intent tests.
  - Covers readable bottom navigation labels for `查信息`, `文档`, `远程`,
    `员工`, and `我的`; configured sessions opening the assistant; missing LLM
    access returning to setup; and shared file intents preferring documents
    when available while avoiding the assistant fallback when document handling
    is disabled.
- `flutter analyze`
  - Passed after restoring readable app shell tab/share text and replacing
    mojibake-prone UI assertions with Unicode-safe test constants.
- `flutter test test/servers_screen_test.dart test/ssh_risk_test.dart test/mobile_terminal_command_test.dart --concurrency=1 --reporter compact`
  - Passed: 15 server maintenance widget/model tests.
  - Covers readable high-risk command confirmation dialogs, manual save/send
    confirmation, terminal-output AI analysis confirmation with preview and
    password/Token/private-key warning, command draft non-execution, terminal
    output copy, reconnect profile selection, and SSH risk classification,
    including recursive-force `rm` variants such as `-fr`, split `-r -f`,
    `--` separators, and uppercase `-Rf` flags.
- `flutter analyze`
  - Passed after restoring readable SSH safety dialogs and digital-employee
    SSH-output prompt text.

## Manual Release Gates

These cannot be proven by local unit tests or the unsigned debug APK:

| Gate | Required evidence |
| --- | --- |
| Android signed internal build | Signed APK/AAB path, SHA256, signing identity, build number, installer channel, and install result on at least one Android 13+ device |
| Android share-to-app | Device log or QA notes showing text, URL, image, PDF, Word, Excel, and CSV shared into MaClaw Mobile |
| Android runtime permissions | QA notes/screenshots for notification, camera, microphone, media/file access, and local network/SSH scenario if applicable |
| iOS Share Extension target | Xcode project with `top.mypapers.maclaw.mobile.ShareExtension`, official Team ID, provisioning profile, and `group.top.mypapers.maclaw.mobile` enabled for Runner and Share Extension |
| iOS share-to-app | TestFlight or development install notes showing text, URL, image, PDF, Word, Excel, and CSV shared into MaClaw Mobile |
| iOS runtime permissions | QA notes/screenshots for camera, microphone, speech recognition, photo library, local network, and notifications |
| Manual SSH against real server | Host type, auth mode, connect result, read-only command, command output excerpt, disconnect result, reconnect result, copied output evidence, AI analysis confirmation, and credential deletion confirmation |
| Hub discovery smoke test | Account used, selected HubCenter, discovered Hub, tenant, LLM mode/QR authorization evidence, bootstrap result, AI search with citations, voice transcription, photo/image assistant input, shared result, document draft, document upload/export task IDs, digital employee task ID, realtime status, notification delivery, network offline/recovery, API base URL, and realtime Hub URL confirmation |

## Build Record Template

Create one record per QA build from `docs/qa_build_record_template.md`, then
attach or link the completed record in this section.
