# MaClaw Mobile QA Build Records

Store one completed QA build record per signed Android/iOS release candidate in
this directory.

Completed records and evidence attachments in this directory are ignored by git
by default. Attach them to the release evidence package or deliberately
force-add a fully redacted record only when release policy requires it. Keep this
`README.md` tracked so the directory contract remains visible.

Use `docs/qa_build_record_template.md` as the source template. Name records with
the build date, platform scope, and build number. The validator enforces this
filename contract for records under `docs/qa-builds/`, for example:

```text
2026-07-02-android-ios-1.0.0+42.md
```

Before linking a record from `docs/release_evidence.md`, validate it from the
mobile project root:

```bash
python3 tool/validate_qa_build_record.py docs/qa-builds/<record>.md
```

Do not store SSH passwords, private keys, access tokens, or private customer
content in these records. Use traceable attachment IDs, redacted screenshots,
device logs, task IDs, artifact hashes, and reviewer notes instead.
