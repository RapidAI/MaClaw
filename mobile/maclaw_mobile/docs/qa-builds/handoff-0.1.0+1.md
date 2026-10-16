# MaClaw Mobile Release Handoff

Root: `.`
Scope: `android-ios`
Version: `0.1.0+1`

## Current Status

- Current status: NOT READY.
- Preflight blockers:
  - Android local signing inputs
    - Missing Android signing file: android/key.properties
    - Run `python3 tool/setup_android_signing.py` with MACLAW_ANDROID_STORE_FILE, MACLAW_ANDROID_STORE_PASSWORD, MACLAW_ANDROID_KEY_ALIAS, and MACLAW_ANDROID_KEY_PASSWORD set before Android signed-build planning.
  - iOS export options
    - Missing iOS export options plist: ios/ExportOptions.plist
    - Run `python3 tool/setup_ios_export_options.py --team-id <REAL_APPLE_TEAM_ID> --export-method development` before iOS signed-build planning.
- Missing completed signed-build QA record under docs/qa-builds.
  - no completed signed-build QA records yet; release handoff is only a QA plan, not a completed QA record; preview the handoff with `python3 tool/release_handoff.py --version 0.1.0+1 --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --dry-run --output docs/qa-builds/handoff-0.1.0+1.md`; run `python3 tool/release_status_report.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --log docs/qa-builds/release-status-0.1.0+1.log`; run `python3 tool/release_handoff.py --version 0.1.0+1 --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --output docs/qa-builds/handoff-0.1.0+1.md`; run `python3 tool/setup_android_signing.py`; run `python3 tool/setup_ios_export_options.py --team-id <REAL_APPLE_TEAM_ID> --export-method development`; then run `python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --log docs/qa-builds/preflight-0.1.0+1.log`; then capture `python3 tool/verify_runtime_boundary.py --log docs/qa-builds/runtime-boundary-0.1.0+1.log` and `python3 tool/run_release_gates.py --log docs/qa-builds/release-gates-0.1.0+1.log`; create the record with `python3 tool/create_qa_build_record.py --scope android-ios --version 0.1.0+1 --release-handoff-result "release_handoff.py output saved to docs/qa-builds/handoff-0.1.0+1.md" --preflight-result "qa_preflight.py: Result READY for signed-build QA preparation; log: docs/qa-builds/preflight-0.1.0+1.log" --runtime-boundary-result "MaClaw Mobile runtime boundary verified: no corelib, phone-local SSH, terminal emulator, phone-side SSH credential, custom Hub URL, redemption-code login, or third-party LLM provider/base URL/API-key regressions; log: docs/qa-builds/runtime-boundary-0.1.0+1.log" --automated-gates-result "run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-0.1.0+1.log"`; generate Android artifact evidence with `python3 tool/signed_artifact_evidence.py android <signed-release.apk-or-aab> --record-dir docs/qa-builds --version 0.1.0+1 --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"`; plan iOS archive/export first with `python3 tool/plan_ios_release.py --team-id <REAL_APPLE_TEAM_ID> --export-method development`; after the signed .xcarchive/TestFlight build exists, generate iOS artifact evidence with `python3 tool/plan_ios_release.py --team-id <REAL_APPLE_TEAM_ID> --export-method development --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds` or `python3 tool/signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive" --team-id <REAL_APPLE_TEAM_ID> --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds`; complete the signed-build QA record with real-device share/permission, Hub discovery evidence including selected HubCenter, discovered Hub, tenant, post-SMS-verification official credits LLM proof with concrete llm-request-id and llm-usage-record, MaClaw logo cold start with no Flutter placeholder, signed-in AI assistant first screen with visible main-conversation/secondary-tab controls, microphone/voice input, no legacy info-lookup entry, notification evidence, and GUI-equivalent backend-managed SSH session evidence including the same GUI/agent-bound backend_session_id, GUI/agent claim or worker handoff plus explicit worker claim/update evidence and `ssh_session` realtime `output_chunk`/`output_seq` proof, not phone-local/ad hoc terminal evidence, phone-initiated interrupt evidence through a Hub control record or `/api/mobile/ssh/sessions/{session_id}/interrupt` showing GUI/agent Ctrl+C handling, copied backend session output with a GUI/agent evidence line containing actual values for Hub session ID, backend_session_id, concrete claimed_by worker identity such as claimed_by desktop-agent-1, and numeric output_seq, preserving that evidence line while replacing credentials or private customer excerpts with redacted text or a traceable attachment ID, and AI/digital-employee handoff evidence tied to that same GUI/agent-bound backend_session_id when used; after completing evidence validate it with `python3 tool/validate_qa_build_record.py docs/qa-builds/<YYYY-MM-DD>-android-ios-0.1.0+1.md`; if validation fails inspect gaps with `python3 tool/qa_build_record_report.py docs/qa-builds/<YYYY-MM-DD>-android-ios-0.1.0+1.md`; after records validate run `python3 tool/qa_release_evidence_links.py docs/qa-builds --update-release-evidence`; then verify final evidence with `python3 tool/verify_final_release_evidence.py docs/qa-builds --scope android-ios --log docs/qa-builds/final-release-evidence-0.1.0+1.log`
- Final evidence blockers:
  - Final release evidence requires at least one completed signed-build QA record.

## Operator Inputs

- Android release keystore path, alias, store password, and key password.
- Apple Team ID, Runner provisioning profile, and Share Extension provisioning profile.
- Signed Android/iOS test devices with camera, microphone, photo library, file share, notification, GUI-equivalent backend SSH session management smoke, and weak-network coverage.
- Official MaClaw account, Hub tenant access, desktop QR source for third-party LLM access when required, and a traceable GUI/agent-managed backend SSH session smoke target using the same GUI/agent-bound backend_session_id.

## Command Sequence

```bash
python3 tool/release_status_report.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --log docs/qa-builds/release-status-0.1.0+1.log
python3 tool/release_handoff.py --version 0.1.0+1 --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --output docs/qa-builds/handoff-0.1.0+1.md
python3 tool/setup_android_signing.py
python3 tool/setup_ios_export_options.py --team-id <REAL_APPLE_TEAM_ID> --export-method development
python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --log docs/qa-builds/preflight-0.1.0+1.log
python3 tool/verify_runtime_boundary.py --log docs/qa-builds/runtime-boundary-0.1.0+1.log
python3 tool/build_android_release.py --artifact apk --build-name 0.1.0 --build-number 1 --dry-run
python3 tool/build_android_release.py --artifact apk --build-name 0.1.0 --build-number 1 --record-dir docs/qa-builds --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"
python3 tool/build_android_release.py --artifact appbundle --build-name 0.1.0 --build-number 1 --record-dir docs/qa-builds --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"
python3 tool/signed_artifact_evidence.py android <signed-release.apk-or-aab> --record-dir docs/qa-builds --version 0.1.0+1 --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"
python3 tool/plan_ios_release.py --team-id <REAL_APPLE_TEAM_ID> --export-method development
python3 tool/plan_ios_release.py --team-id <REAL_APPLE_TEAM_ID> --export-method development --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds
python3 tool/signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive" --team-id <REAL_APPLE_TEAM_ID> --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds
python3 tool/run_release_gates.py --log docs/qa-builds/release-gates-0.1.0+1.log
python3 tool/create_qa_build_record.py --scope android-ios --version 0.1.0+1 --release-handoff-result "release_handoff.py output saved to docs/qa-builds/handoff-0.1.0+1.md" --preflight-result "qa_preflight.py: Result READY for signed-build QA preparation; log: docs/qa-builds/preflight-0.1.0+1.log" --runtime-boundary-result "MaClaw Mobile runtime boundary verified: no corelib, phone-local SSH, terminal emulator, phone-side SSH credential, custom Hub URL, redemption-code login, or third-party LLM provider/base URL/API-key regressions; log: docs/qa-builds/runtime-boundary-0.1.0+1.log" --automated-gates-result "run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-0.1.0+1.log"
python3 tool/validate_qa_build_record.py docs/qa-builds/<YYYY-MM-DD>-android-ios-0.1.0+1.md
python3 tool/qa_build_record_report.py docs/qa-builds/<YYYY-MM-DD>-android-ios-0.1.0+1.md
python3 tool/qa_release_evidence_links.py docs/qa-builds --update-release-evidence
python3 tool/validate_qa_build_records_dir.py docs/qa-builds
python3 tool/verify_final_release_evidence.py docs/qa-builds --scope android-ios --log docs/qa-builds/final-release-evidence-0.1.0+1.log
```

## Evidence To Attach

- Handoff output path or transcript, runtime-boundary verifier output, and full release-gate run result for the QA record final decision fields.
- Signed Android artifact path, byte size, SHA256, install result, signing identity, and distribution channel.
- iOS archive/export path, Team ID, provisioning profile names or UUIDs, install/TestFlight result, and Share Extension result.
- Runtime boundary verifier result proving mobile does not embed or bridge Go corelib.
- HubCenter discovery result, discovered Hub URL, tenant, LLM access mode, post-SMS-verification official credits LLM proof with concrete llm-request-id and llm-usage-record, mobile bootstrap result, cold-start MaClaw logo with no Flutter placeholder, and signed-in AI assistant first screen with visible main-conversation/secondary-tab controls, microphone/voice input, and no legacy info-lookup entry.
- AI assistant answer with citations, voice query, image/screenshot query, and share-to-app payload results.
- Document import, AI transform, export/share, and notification evidence.
- GUI-equivalent backend-managed SSH session evidence: login/session using the same GUI/agent-bound backend_session_id, GUI/agent claim or worker handoff with explicit worker claim/update evidence, `ssh_session` realtime events with `output_chunk` and `output_seq`, not phone-local/ad hoc terminal evidence, phone-initiated interrupt evidence through a Hub control record or `/api/mobile/ssh/sessions/{session_id}/interrupt` showing GUI/agent Ctrl+C handling, read-only command output, reconnect, copied backend session output with a GUI/agent evidence line containing actual values for Hub session ID, backend_session_id, concrete claimed_by worker identity such as claimed_by desktop-agent-1, and numeric output_seq, with credentials or private customer excerpts replaced by redacted text or a traceable attachment ID while preserving that evidence line, AI analysis and digital employee handoff tied to that same GUI/agent-bound backend_session_id when used, and phone-side server-profile cache clear evidence.
- Digital employee list, remote target invocation, completion/failure result, and notification evidence.
- Weak-network/offline recovery evidence with timestamps.

## Current Local Evidence Snapshot

- Automated release gate transcript: `docs/qa-builds/release-gates-notification-pending-final-20260710.log`.
- Runtime boundary transcript: `docs/qa-builds/runtime-boundary-20260706-backend-ssh-realtime.log`.
- Final release evidence transcript: `docs/qa-builds/final-release-evidence-20260706-backend-ssh-realtime.log`.

## Notes

- Android signing setup requires `MACLAW_ANDROID_STORE_FILE`, `MACLAW_ANDROID_STORE_PASSWORD`, `MACLAW_ANDROID_KEY_ALIAS`, and `MACLAW_ANDROID_KEY_PASSWORD` in the environment before running `setup_android_signing.py`.
- Do not add placeholder signing/export files; use real local signing material or keep the release in pre-signing setup state until the real inputs are available.
- `<APPLE_TEAM_ID>` is allowed only for planning/status commands (`release_status_report.py`, `release_handoff.py`, and `qa_preflight.py`). Replace it with the real 10-character Apple Team ID before running `setup_ios_export_options.py`, `plan_ios_release.py`, or `signed_artifact_evidence.py`.
- PowerShell treats unquoted `<...>` placeholders as redirection syntax, so replace all angle-bracket placeholders with real values before copying commands there; for dry-run previews with placeholders, wrap placeholder arguments in quotes.
- Do not store signing secrets, SSH passwords, private keys, access tokens, or private customer content in QA records.
- Use redacted screenshots, artifact hashes, task IDs, and attachment IDs for traceable evidence.
- Completed QA records must pass `python3 tool/validate_qa_build_record.py` without secret redaction failures; use `python3 tool/qa_build_record_report.py` to fix gaps before linking them.
- Link only validated QA records from docs/release_evidence.md.
