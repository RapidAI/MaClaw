# MaClaw Mobile QA Build Records

Store one completed QA build record per signed Android/iOS release candidate in
this directory.

Completed records and evidence attachments in this directory are ignored by git
by default. Attach them to the release evidence package or deliberately
force-add a fully redacted record only when release policy requires it. Keep this
`README.md` tracked so the directory contract remains visible.

Use `docs/qa_build_record_template.md` as the source template. The safest path is
to run local QA preflight and scaffold a validator-named record before QA starts:

```bash
python3 tool/release_status_report.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development
python3 tool/release_handoff.py --version 1.0.0+42 --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development --output docs/qa-builds/handoff-1.0.0+42.md
python3 tool/setup_android_signing.py
python3 tool/setup_ios_export_options.py --team-id <APPLE_TEAM_ID> --export-method development
python3 tool/qa_preflight.py --scope android-ios --team-id <APPLE_TEAM_ID> --export-method development
python3 tool/build_android_release.py --artifact apk --build-name 1.0.0 --build-number 42 --record-dir docs/qa-builds --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"
python3 tool/build_android_release.py --artifact appbundle --build-name 1.0.0 --build-number 42 --record-dir docs/qa-builds --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"
python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds
python3 tool/signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive" --team-id <APPLE_TEAM_ID> --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds
python3 tool/verify_runtime_boundary.py --log docs/qa-builds/runtime-boundary-1.0.0+42.log
python3 tool/run_release_gates.py --log docs/qa-builds/release-gates-1.0.0+42.log
python3 tool/create_qa_build_record.py --date 2026-07-02 --scope android-ios --version 1.0.0+42 \
  --release-handoff-result "release_handoff.py output saved to docs/qa-builds/handoff-1.0.0+42.md" \
  --runtime-boundary-result "MaClaw Mobile runtime boundary verified. log: docs/qa-builds/runtime-boundary-1.0.0+42.log" \
  --automated-gates-result "run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log"
```

The handoff, runtime-boundary log, and release-gates log commands refuse to
overwrite existing saved evidence files unless `--force` is provided.
Release handoff outputs saved directly under `docs/qa-builds/` must use a
`handoff-*.md` filename; `tool/release_handoff.py` rejects other Markdown names
there so they cannot be mistaken for completed signed-build QA records.

If the handoff, runtime-boundary, and release-gate outputs use different saved
QA evidence paths or attachment IDs, replace the three Final Release Decision
references while creating the record:

```bash
python3 tool/create_qa_build_record.py --date 2026-07-02 --scope android-ios --version 1.0.0+42 \
  --release-handoff-result "release_handoff.py output saved to docs/qa-builds/handoff-1.0.0+42.md" \
  --runtime-boundary-result "MaClaw Mobile runtime boundary verified. log: docs/qa-builds/runtime-boundary-1.0.0+42.log" \
  --automated-gates-result "run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log"
```

Use `--scope android` or `--scope ios` when Android and iOS evidence are captured
in separate records. The scaffold command refuses to overwrite an existing
record unless `--force` is provided.
For Android-only internal QA, the status, handoff, and preflight commands do not
need Apple Team ID or export method values:

```bash
python3 tool/release_status_report.py --scope android
python3 tool/release_handoff.py --version 1.0.0+42 --scope android --output docs/qa-builds/handoff-android-1.0.0+42.md
python3 tool/qa_preflight.py --scope android
python3 tool/build_android_release.py --artifact apk --build-name 1.0.0 --build-number 42 --record-dir docs/qa-builds --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"
python3 tool/signed_artifact_evidence.py android <signed-release.apk-or-aab> --record-dir docs/qa-builds --version 1.0.0+42 --signing-identity "<alias or certificate fingerprint>" --installer-channel "<internal test channel>"
```

For iOS-only internal QA, keep the Apple Team ID and export method on the iOS
commands:

```bash
python3 tool/release_status_report.py --scope ios --team-id <APPLE_TEAM_ID> --export-method development
python3 tool/release_handoff.py --version 1.0.0+42 --scope ios --team-id <APPLE_TEAM_ID> --export-method development --output docs/qa-builds/handoff-ios-1.0.0+42.md
python3 tool/qa_preflight.py --scope ios --team-id <APPLE_TEAM_ID> --export-method development
python3 tool/plan_ios_release.py --team-id <APPLE_TEAM_ID> --export-method development --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds
python3 tool/signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive" --team-id <APPLE_TEAM_ID> --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds
```

Name records with the build date, platform scope, and build number. The
validator enforces this filename contract for records under `docs/qa-builds/`,
for example:

```text
2026-07-02-android-ios-1.0.0+42.md
```

Before linking a record from `docs/release_evidence.md`, validate it from the
mobile project root:

```bash
python3 tool/validate_qa_build_record.py docs/qa-builds/<record>.md
```

For Android signed-build evidence, keep the APK/AAB at the `Artifact path`
generated by `tool/signed_artifact_evidence.py --record-dir docs/qa-builds`.
The validator resolves that path relative to `docs/qa-builds` and fails when
the local signed artifact is missing or its SHA256 differs from the record. For
iOS evidence, generate local archive evidence with
`python3 tool/signed_artifact_evidence.py ios --archive-or-build "build/ios/archive/MaClawMobile.xcarchive" --team-id <APPLE_TEAM_ID> --provisioning-profiles "<Runner profile UUID/name; Share Extension profile UUID/name>" --record-dir docs/qa-builds`.
A recorded `.xcarchive` path is resolved relative to `docs/qa-builds` and must
exist as a local archive directory; explicit `TestFlight build <number>`
evidence is validated through the QA record fields instead.

Completed records must include these Final Release Decision fields:
- `Release handoff result`
- `Runtime boundary verification result`
- `Automated release gates result`

Each field must use traceable output paths, command transcripts, or log
attachment IDs. The runtime-boundary result must come from
`tool/verify_runtime_boundary.py` and explicitly show no embedded Go corelib,
Dart FFI, gomobile binding, dynamic library, or native corelib MethodChannel bridge;
core MaClaw capabilities must remain behind the discovered Hub APIs, realtime
channel, or explicitly authorized digital employee handoff.

If validation fails while QA is still filling evidence, print a grouped gap
report:

```bash
python3 tool/qa_build_record_report.py docs/qa-builds/<record>.md
```

After records validate, generate the Markdown links that must be added to
`docs/release_evidence.md`:

```bash
python3 tool/qa_release_evidence_links.py docs/qa-builds --update-release-evidence
```

For Android-only or iOS-only internal QA, pass the same platform scope used by
the completed record, for example:

```bash
python3 tool/qa_release_evidence_links.py docs/qa-builds --update-release-evidence --scope android
python3 tool/qa_release_evidence_links.py docs/qa-builds --update-release-evidence --scope ios
```

To check every completed Markdown record in this directory at once, run:

```bash
python3 tool/validate_qa_build_records_dir.py docs/qa-builds
```

For scoped internal QA, pass the same platform scope so the next final verifier
command is based only on in-scope records:

```bash
python3 tool/validate_qa_build_records_dir.py docs/qa-builds --scope android
python3 tool/validate_qa_build_records_dir.py docs/qa-builds --scope ios
```

When a scope is supplied, records whose filenames clearly belong to the other
platform are reported as out-of-scope. Out-of-scope invalid records appear as an
ignored warning in `tool/validate_qa_build_records_dir.py`,
`tool/qa_release_evidence_links.py`, and
`tool/verify_final_release_evidence.py` output, and they do not block the
current scoped Android or iOS package. Invalid records for the current scope, or
records whose filename scope cannot be parsed, still block release evidence
updates and point to `tool/qa_build_record_report.py`.

The directory validator skips this README, handoff-*.md release-handoff
evidence files, and non-Markdown evidence attachments.

Before final release approval, verify that the evidence package contains
validated signed-build records for both Android and iOS, and that
`docs/release_evidence.md` links every validated record by filename:

```bash
python3 tool/verify_final_release_evidence.py docs/qa-builds --scope android-ios --log docs/qa-builds/final-release-evidence-<version+build>.log
```

Scoped internal QA can instead use the matching final verifier scope and log
name, such as
`python3 tool/verify_final_release_evidence.py docs/qa-builds --scope android --log docs/qa-builds/final-release-evidence-android-<version+build>.log`.

The final verifier log captures the success or failure transcript for the
release evidence package. Existing logs are not overwritten unless `--force` is
passed intentionally.

Do not store SSH passwords, private keys, access tokens, or private customer
content in these records. The validator rejects high-confidence raw secrets,
including private key blocks, `password=`/`token=`/`api_key=` assignments,
Authorization Bearer/Basic headers, Cookie/Set-Cookie/PRIVATE-TOKEN/X-API-Key
headers, literal API tokens, JWTs, cloud access key IDs, Google API keys, and
URLs with embedded credentials. Use traceable attachment IDs, redacted screenshots,
device logs, task IDs, artifact hashes, and reviewer notes instead.
