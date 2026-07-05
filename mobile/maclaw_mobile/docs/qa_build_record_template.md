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
MaClaw account: phone:<digits> or masked phone:<last-digits>
HubCenter candidates: https://hubs.mypapers.top, https://hubs.maclaw.top, https://hubs2.maclaw.top
Selected HubCenter URL:
Discovered Hub URL:
Tenant ID: tenant identifier
LLM access mode: maclaw_official / desktop_qr_third_party
Desktop GUI QR authorization ID: not-used-official-mode / desktop QR authorization ID
# If any final gate below is waived, include every waived gate name plus a
# trackable waiver ticket/issue/approval reference such as QA-42 or #123.
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
# Every runtime permission line must include a trackable permission-grant:<id>
# from the QA permission prompt/result record.
# Include notification permission evidence from a real task-notification
# delivery/open flow, such as document export, digital employee task, and SSH
# abnormal notification payloads, not only the permission dialog.
Notification permission:
# Include camera permission evidence from a real photo/image/screenshot
# assistant input flow, not only the permission dialog.
Camera permission:
# Include microphone permission evidence from a real voice-transcribed
# assistant question, not only the permission dialog.
Microphone permission:
# Include file/media access evidence from real file picker or share-to-app
# document import/upload for PDF, Word, Excel, CSV, and image/photo payloads,
# not only the permission dialog.
Media/file access:
# Include local-network permission evidence tied to a real SSH connection to the
# recorded server-profile:<id> plus a read-only command result, not only the
# permission dialog.
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
Use `python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development`
to print archive/export context before the signed archive is created. After
the local archive exists, rerun
`python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds`
to print record-relative iOS evidence with the same archive/export context. For
an already-built archive or TestFlight build, use
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
# Every runtime permission line must include a trackable permission-grant:<id>
# from the QA permission prompt/result record.
# Include camera permission evidence from a real photo/image/screenshot
# assistant input flow, not only the permission dialog.
Camera permission:
# Include microphone permission evidence from a real voice-transcribed
# assistant question, not only the permission dialog.
Microphone permission:
# Include speech-recognition permission evidence from the same voice
# assistant question/transcription flow.
Speech recognition permission:
# Include photo-library permission evidence from a real imported
# photo/image/screenshot assistant input flow.
Photo library permission:
# Include local-network permission evidence tied to a real SSH connection to the
# recorded server-profile:<id> plus a read-only command result, not only the
# permission dialog.
Local network permission:
# Include notification permission evidence from a real task-notification
# delivery/open flow, such as document export, digital employee task, and SSH
# abnormal notification payloads, not only the permission dialog.
Notification permission:
Screenshots / recordings:
```

## Hub Discovery And Service Smoke Test

```text
# Include phone-number-only MaClaw official login through HubCenter: SMS
# verification code accepted, new/returning phone account behavior if observed,
# authenticated mobile session, the same phone account recorded above, and
# official credits bound to that phone account. Include sms-verification:<id>.
# After verification succeeds, include proof that the first LLM call charges/uses
# the verified phone account's MaClaw official credits, including a
# request/log/usage record ID.
Login result:
# Include the same phone account and Tenant ID recorded above, plus quota,
# feature flags, and service status from mobile bootstrap.
Bootstrap user/quota/feature flags/service status:
# Include the selected HubCenter URL recorded above in the probe evidence.
HubCenter probe result:
# Include the Discovered Hub URL and Tenant ID recorded above.
Discovered Hub/tenant result:
# For maclaw_official mode, include the same phone:<digits> account recorded
# above and prove that after SMS verification succeeds, LLM calls use that
# verified phone account's MaClaw official credits/quota with a request/log/
# usage record ID. For
# desktop_qr_third_party, include desktop GUI QR proof and the matching Desktop
# GUI QR authorization ID recorded above. In both modes, include the same Tenant
# ID recorded above.
LLM access evidence:
LLM setup surface restriction:
AI search query:
# Include voice transcription evidence and photo/image/screenshot assistant
# input evidence, plus a recorded visible citation URL or document upload task ID.
Voice/photo assistant input evidence:
# Include at least one visible HTTPS source URL shown in the answer/result
# citations area, not only a backend/API log URL.
Visible citations / sources:
# Name the share target or output, such as Mail, WeChat, system share sheet,
# clipboard, exported file, or saved local path, and include a recorded
# citation URL from Visible citations / sources. Include proof that the shared
# answer/result preview was redacted/masked/sanitized before external sharing,
# including redaction-check:<id>.
Shared result:
# Include all document templates created from the search result: notice,
# report, email, proposal, meeting minutes, and statement, plus a recorded
# citation URL from Visible citations / sources and the resulting
# document-draft:<id>.
Document draft created from search:
Document upload task ID:
PDF export job ID:
Word export job ID:
Markdown export job ID:
# Include the recorded PDF, Word, and Markdown export job IDs in this evidence,
# plus proof that the exported document preview was redacted/masked/sanitized
# before sharing or saving externally, including redaction-check:<id>.
Exported document share evidence:
# Include the task ID plus the matching selected HubCenter URL, discovered Hub
# URL, Tenant ID, MaClaw phone-account credits, and manual-confirmation boundary.
Digital employee task ID:
# Include one of the recorded document/export/digital-employee task or job IDs
# in status polling and realtime evidence so the status can be traced.
Status polling result:
Realtime update evidence:
# Include typed payloads opened from real notifications, including the recorded
# document export job ID and digital employee task ID:
# document-export:/document-draft:/document-upload:, digital-employee-task:,
# and server-profile:. Include proof that document/digital employee notification
# message previews are redacted/masked/sanitized before display.
Notification delivery evidence:
# Include evidence that the API client base URL uses the discovered Hub origin
# URL only; do not paste endpoint paths.
API base URL confirmation:
# Include evidence that the realtime/WebSocket client uses the same discovered
# Hub origin URL only; do not paste endpoint paths or token query strings.
Realtime Hub URL confirmation:
# Include the selected HubCenter URL, Discovered Hub URL, Tenant ID, and at
# least one recorded document/export/digital-employee task or job ID so weak
# network recovery is traceable to the same tenant session. Include a
# network-recovery/connectivity-probe/retry/incident trace ID.
Network offline/recovery evidence:
```

## Account Privacy And Local Data

```text
Theme and speech language change result:
# If notification evidence recorded a server-profile:<id>, include the same
# server-profile:<id> and show that clearing local work records removes search,
# document drafts, commands, digital employee prompts, and preferences without
# deleting server credentials.
Local work records reset confirmation:
# Include the same server-profile:<id> and show SSH password/private-key
# credentials remained available in secure storage/vault after local
# work-record reset.
Server credentials retained after local reset:
# Include the same server-profile:<id> and show the separate explicit account
# action cleared server profiles and SSH password/private-key credentials with
# credential-clear:<id>.
Server profiles/SSH credentials clear confirmation:
```

## Manual SSH Smoke Test

```text
# If notification evidence recorded a server-profile:<id> payload, include the
# same server-profile:<id> in every SSH smoke evidence line below.
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
# Include AI explanation, command drafts, command-draft:<id>,
# manual/not-auto-executed evidence, and proof that the result used
# redacted/masked/sanitized SSH terminal/log output.
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
# Use waived only with a concrete reason and a matching Known issues / waivers
# entry that names the waived gate and includes a trackable ticket/approval ID.
Automated gates passed: passed / waived with reason
Android manual gates passed: passed / waived with reason
iOS manual gates passed: passed / waived with reason
Hub discovery smoke passed: passed / waived with reason
Manual SSH smoke passed: passed / waived with reason
# Approver must be different from Tester.
Approved by:
Approval date: YYYY-MM-DD
```
