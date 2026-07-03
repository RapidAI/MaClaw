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
python3 tool/release_status_report.py
python3 tool/release_handoff.py --version 1.0.0+42 --team-id <APPLE_TEAM_ID> --export-method development --output docs/qa-builds/handoff-1.0.0+42.md
python3 tool/verify_runtime_boundary.py --log docs/qa-builds/runtime-boundary-1.0.0+42.log
python3 tool/run_release_gates.py --log docs/qa-builds/release-gates-1.0.0+42.log
python3 tool/qa_preflight.py
python3 tool/create_qa_build_record.py --date 2026-07-02 --scope android-ios --version 1.0.0+42 \
  --release-handoff-result "docs/qa-builds/handoff-1.0.0+42.md" \
  --runtime-boundary-result "MaClaw Mobile runtime boundary verified. log: docs/qa-builds/runtime-boundary-1.0.0+42.log" \
  --automated-gates-result "run_release_gates.py: 36 gates passed; log: docs/qa-builds/release-gates-1.0.0+42.log"
```

The handoff, runtime-boundary log, and release-gates log commands refuse to
overwrite existing saved evidence files unless `--force` is provided.

If the handoff, runtime-boundary, and release-gate outputs use different saved
paths or attachment IDs, replace the three Final Release Decision references
while creating the record:

```bash
python3 tool/create_qa_build_record.py --date 2026-07-02 --scope android-ios --version 1.0.0+42 \
  --release-handoff-result "docs/qa-builds/handoff-1.0.0+42.md" \
  --runtime-boundary-result "MaClaw Mobile runtime boundary verified. log: boundary-1.0.0+42.log" \
  --automated-gates-result "run_release_gates.py: 36 gates passed; log: release-gates-1.0.0+42.log"
```

Use `--scope android` or `--scope ios` when Android and iOS evidence are captured
in separate records. The scaffold command refuses to overwrite an existing
record unless `--force` is provided.

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

Completed records must include these Final Release Decision fields:
- `Release handoff result`
- `Runtime boundary verification result`
- `Automated release gates result`

Each field must use traceable output paths, command transcripts, or log
attachment IDs.

If validation fails while QA is still filling evidence, print a grouped gap
report:

```bash
python3 tool/qa_build_record_report.py docs/qa-builds/<record>.md
```

After records validate, generate the Markdown links that must be added to
`docs/release_evidence.md`:

```bash
python3 tool/qa_release_evidence_links.py docs/qa-builds
```

To check every completed Markdown record in this directory at once, run:

```bash
python3 tool/validate_qa_build_records_dir.py docs/qa-builds
```

The directory validator skips this README and ignores non-Markdown evidence
attachments.

Before final release approval, verify that the evidence package contains
validated signed-build records for both Android and iOS, and that
`docs/release_evidence.md` links every validated record by filename:

```bash
python3 tool/verify_final_release_evidence.py docs/qa-builds
```

Do not store SSH passwords, private keys, access tokens, or private customer
content in these records. Use traceable attachment IDs, redacted screenshots,
device logs, task IDs, artifact hashes, and reviewer notes instead.
