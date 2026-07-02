# MaClaw Mobile Release Evidence

Use this file with `docs/release_checklist.md`. The checklist says what must be
true before release; this file records what can be proven automatically and what
still requires signed builds or real devices.

For the requirement-by-requirement status, use `docs/release_audit.md`.
For signed-build and real-device QA steps, use `docs/qa_device_checklist.md`.
For each signed QA build, copy `docs/qa_build_record_template.md` and attach the
completed evidence record here after it passes
`python3 tool/validate_qa_build_record.py docs/qa-builds/<record>.md`. Store
records under `docs/qa-builds/`; see `docs/qa-builds/README.md` for naming and
redaction rules.

## Automated Gates

Run these before handing a build to QA:

```bash
go test ./hub/internal/httpapi -run "TestMobile.*" -count=1
go test ./hubcenter/internal/httpapi -run "TestMobile(ServiceRedemption|DesktopQRSession)" -count=1
go test ./gui -run "TestMobileDigitalEmployeeCandidateIDs|TestRemoteHubClient.*Mobile|TestMobileDocumentSourceMarkdown" -count=1
cd mobile/maclaw_mobile
python3 -m unittest tool/configure_platforms_test.py
python3 -m unittest tool/validate_qa_build_record_test.py
python3 -m unittest tool/verify_runtime_boundary_test.py
python3 -m unittest tool/run_release_gates_test.py
flutter test test/release_docs_test.dart --concurrency=1 --reporter compact
flutter create --platforms android,ios .
python3 tool/configure_platforms.py
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
The latest local `python tool\run_release_gates.py` run passed all 15 automated
release gates, including Flutter analysis, the full Flutter test suite, runtime
boundary verification, native wrapper regeneration/configuration, Go mobile API
tests, and Android debug APK build.

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
| Runtime boundary: no embedded Go `corelib`, FFI, gomobile, or native corelib bridge | `tool/verify_runtime_boundary.py`, `tool/verify_runtime_boundary_test.py`, `test/release_docs_test.dart` |
| Signed-build QA record completeness | `tool/validate_qa_build_record.py`, `tool/validate_qa_build_record_test.py`, `docs/qa_build_record_template.md` |
| Automated gate sequence integrity | `tool/run_release_gates.py`, `tool/run_release_gates_test.py` |
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
- `tool/run_release_gates.py`
  - Added a local release-gate runner that executes the documented automated
    mobile gate sequence from the correct repository/mobile working
    directories.
  - Runs `tool/verify_runtime_boundary.py` after native wrapper configuration
    to reject accidental Go `corelib`, FFI, gomobile, or native corelib bridge
    additions in the mobile runtime.
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
  - Passed: 20 release documentation integrity tests.
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
  - Passed: 13 platform configuration tests.
  - Covers cleanup of Flutter's generated widget-test template so
    native wrapper regeneration does not introduce stale `MyApp` analyzer
    failures.
- `python -m unittest tool\validate_qa_build_record_test.py`
  - Passed: 103 QA record validator tests.
  - Covers incomplete template rejection, completed record acceptance,
    HubCenter discovery enforcement, exact HubCenter candidate enforcement,
    tenant Hub versus HubCenter URL separation,
    duplicate Android/iOS field enforcement and platform order,
    required field overfill rejection,
    duplicate formatted-field rejection,
    fixed identity validation, LLM mode/QR/evidence consistency, Android artifact path validation, signed install/launch/platform evidence, debug artifact/signing/installer rejection, trackable signing alias/certificate evidence, local artifact
    SHA256 matching, iOS archive/TestFlight build identity, Team ID, and provisioning profile
    auditability, build identity/branch/signing placeholder rejection,
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
    QA template field coverage against required/optional validator fields, UTF-8 BOM handling for
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
- `python -m unittest tool\run_release_gates_test.py`
  - Passed: 10 release gate runner tests.
  - Covers gate order, working directories, critical command coverage, and
    numbered dry-run output.
  - Covers Windows batch-command executable resolution for tools such as
    `flutter.BAT` while keeping documented commands readable as `flutter ...`.
  - Covers CI workflow, release checklist, and release evidence parity for
    automated gate commands and command order, plus debug APK artifact upload.
- `python tool\configure_platforms.py`
  - Passed.
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
  - Passed: 13 platform configuration tests.
- `python -m unittest tool\validate_qa_build_record_test.py`
  - Passed: 103 QA build record validator tests.
- `python -m unittest tool\verify_runtime_boundary_test.py`
  - Passed: 3 runtime boundary verifier tests.
  - Covers the current mobile runtime source tree and negative fixtures for
    Dart FFI, dynamic-library loading, and Go `corelib` references while
    ignoring docs/tests-only mentions.
- `python -m unittest tool\run_release_gates_test.py`
  - Passed: 10 release gate runner tests.
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
  - Passed: 187 tests.
  - No Drift debug-only multiple-database warning was emitted after adding the
    local-store concurrent open gate and isolating digital-employee widget
    history providers.
- `flutter build apk --debug`
  - Passed.
  - Artifact: `D:\workprj\aicoder\mobile\maclaw_mobile\build\app\outputs\flutter-apk\app-debug.apk`
  - Size: `227304480` bytes.
  - SHA256: `406026B4E76322D82416AB68AA771447CA644CFBA79F8A6474D57EFA5D295DEB`.
  - Refreshed after enforcing discovered-Hub export download URL safety and
    notification payload fallback behavior.
  - CI artifact name: `maclaw-mobile-debug-apk`.
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
