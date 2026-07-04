# MaClaw Mobile QA Build Record Template

Create one copy of this file for every signed QA build. Attach or link the
completed record from `docs/release_evidence.md` before release approval.
Save completed records under `docs/qa-builds/` using the validator-enforced
`YYYY-MM-DD-<android|ios|android-ios>-<version+build>.md` filename format; see
`docs/qa-builds/README.md` for the directory contract.

For manual device evidence fields, do not write placeholders such as `ok`,
`yes`, or `done`. Record a concise auditable note, traceable evidence filename
or attachment ID, device log reference, or task/result identifier.

## Build Identity

```text
Date: YYYY-MM-DD
Git commit: 7-40 character hexadecimal commit SHA
Branch: git branch name
Tester:
Flutter version: Flutter x.y.z
MaClaw account: phone:<number> or masked phone:<last-digits>
HubCenter candidates: https://hubs.mypapers.top, https://hubs.maclaw.top, https://hubs2.maclaw.top
Selected HubCenter URL:
Discovered Hub URL:
Tenant ID: tenant identifier
LLM access mode: maclaw_official / desktop_qr_third_party
Desktop GUI QR authorization ID: not-used-official-mode / desktop QR authorization ID
Known issues / waivers:
```

## Android Signed Build

```text
Artifact path:
SHA256:
Version/build number: app version + build number, such as 1.0.0+42
Signing identity: release alias, SHA fingerprint, upload key, or certificate ID
Installer channel:
Device model / OS:
Android signed install result:
Account screen shows selected Hub and tenant:
No custom Hub URL setting found:
```

If the artifact path is available on the validation machine, the QA validator
checks that the recorded SHA256 matches the local `.apk` or `.aab` file.
Use `python3 tool/build_android_release.py --artifact apk --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"`
or `python3 tool/build_android_release.py --artifact appbundle --build-name <app-version> --build-number <build-number> --record-dir docs/qa-builds --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"`
to build and print paste-ready signed artifact evidence immediately. For an
already-built artifact, use `python3 tool/signed_artifact_evidence.py android <signed-release.apk-or-aab> --record-dir docs/qa-builds --version <version+build> --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"`
to generate paste-ready `Artifact path`, `SHA256`, byte-size, version/build,
signing identity, and installer channel evidence.

## Android Share-To-App Evidence

```text
Plain text:
URL:
Image/photo:
PDF:
Word .docx or .doc:
Excel .xlsx or .xls:
CSV:
Device logs / screenshots / recordings:
```

## Android Runtime Permission Evidence

```text
Notification permission:
Camera permission:
Microphone permission:
Media/file access:
Local network / SSH scenario:
Screenshots / recordings:
```

## iOS Signed Build And Share Extension

```text
Archive/TestFlight build: .xcarchive path or TestFlight build number
Runner bundle id: top.mypapers.maclaw.mobile
Share Extension bundle id: top.mypapers.maclaw.mobile.ShareExtension
Team ID:
Provisioning profiles: Runner and Share Extension profile UUID/file/name
App group: group.top.mypapers.maclaw.mobile
Device model / OS:
iOS signed install result:
URL schemes maclaw and ShareMedia-$(PRODUCT_BUNDLE_IDENTIFIER):
```

`Archive/TestFlight build` must identify an `.xcarchive` or TestFlight build.
`Team ID` must be the 10-character Apple team identifier. `Provisioning
profiles` must mention both Runner and Share Extension profiles.
Do not write only `UUID`; include the actual profile UUID value,
`.mobileprovision` file, or explicit profile name for each target.
Use `python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds`
to print archive/export context and paste-ready iOS evidence during signed
archive planning. For an already-built archive or TestFlight build, use
`python3 tool/signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive" --team-id <APPLE_TEAM_ID> --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds`
to generate paste-ready archive/build, Team ID, and provisioning-profile
evidence.

## iOS Share-To-App Evidence

```text
Plain text:
URL:
Image/photo:
PDF:
Word .docx or .doc:
Excel .xlsx or .xls:
CSV:
Device logs / screenshots / recordings:
```

## iOS Runtime Permission Evidence

```text
Camera permission:
Microphone permission:
Speech recognition permission:
Photo library permission:
Local network permission:
Notification permission:
Screenshots / recordings:
```

## Hub Discovery And Service Smoke Test

```text
# Include phone-number-only MaClaw official login through HubCenter: SMS
# verification code accepted, new/returning phone account behavior if observed,
# authenticated mobile session, the same phone account recorded above, and
# official credits bound to that phone account.
Login result:
Bootstrap user/quota/feature flags/service status:
# Include the selected HubCenter URL recorded above in the probe evidence.
HubCenter probe result:
# Include the Discovered Hub URL and Tenant ID recorded above.
Discovered Hub/tenant result:
# For maclaw_official mode, include the same phone:<number> account recorded
# above and prove LLM calls use that phone account's MaClaw official
# credits/quota. For desktop_qr_third_party, include desktop GUI QR proof and
# the matching Desktop GUI QR authorization ID recorded above.
LLM access evidence:
LLM setup surface restriction:
AI search query:
# Include voice transcription evidence and photo/image/screenshot assistant
# input evidence, plus the resulting answer citation or document/upload task ID.
Voice/photo assistant input evidence:
# Include at least one visible HTTPS source URL from the answer citations.
Visible citations / sources:
# Name the share target or output, such as Mail, WeChat, system share sheet,
# clipboard, exported file, or saved local path.
Shared result:
# Include all document templates created from the search result: notice,
# report, email, proposal, meeting minutes, and statement.
Document draft created from search:
Document upload task ID:
PDF export job ID:
Word export job ID:
Markdown export job ID:
Exported document share evidence:
Digital employee task ID:
# Include one of the recorded document/export/digital-employee task or job IDs
# in status polling and realtime evidence so the status can be traced.
Status polling result:
Realtime update evidence:
# Include typed payloads opened from real notifications:
# document-export:/document-draft:/document-upload:, digital-employee-task:,
# and server-profile:.
Notification delivery evidence:
# Record the discovered Hub origin URL only; do not paste endpoint paths.
API base URL confirmation:
Realtime Hub URL confirmation:
Network offline/recovery evidence:
```

## Account Privacy And Local Data

```text
Theme and speech language change result:
Local work records reset confirmation:
Server credentials retained after local reset:
Server profiles/SSH credentials clear confirmation:
```

## Manual SSH Smoke Test

```text
Host type:
Auth mode:
Connect result:
Read-only command:
Command output excerpt:
Disconnect result:
Reconnect result:
Copied output evidence:
# Include the SSH terminal/log output preview, the sensitive-data warning, and
# proof that the output was redacted/masked/sanitized before AI analysis.
AI analysis confirmation and sensitive-data warning:
# Include AI explanation, command drafts, and manual/not-auto-executed evidence.
AI explanation / command draft result:
# If used, mention Hub/tenant warning confirmation and the SSH terminal or
# pasted/copied output being handed to the digital employee.
Digital employee handoff warning, if used:
Credential deletion confirmation:
```

## Final Release Decision

```text
# Paste the handoff output path, attachment ID, or command transcript reference.
Release handoff result:
# Paste `python3 tool/verify_runtime_boundary.py` output or log attachment ID.
# This must prove the signed app has no embedded Go corelib, Dart FFI,
# gomobile binding, dynamic library, or native corelib MethodChannel bridge;
# core MaClaw capabilities must be reached through the discovered Hub APIs,
# realtime channel, or explicitly authorized digital employee handoff.
Runtime boundary verification result:
# Paste `python3 tool/run_release_gates.py` result, gate count, and log attachment ID.
Automated release gates result:
Automated gates passed: passed / waived with reason
Android manual gates passed: passed / waived with reason
iOS manual gates passed: passed / waived with reason
Hub discovery smoke passed: passed / waived with reason
Manual SSH smoke passed: passed / waived with reason
# Approver must be different from Tester.
Approved by:
Approval date: YYYY-MM-DD
```
