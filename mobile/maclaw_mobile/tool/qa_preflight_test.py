from __future__ import annotations

import plistlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import qa_preflight
import release_evidence_commands
import validate_qa_build_records_dir
import verify_runtime_boundary


def ok_android_config(_: Path) -> list[str]:
    return []


def ok_android_key(_: Path) -> tuple[dict[str, str], list[str]]:
    return {"storeFile": "release.jks"}, []


def ok_ios(_: Path) -> list[str]:
    return []


def empty_records(_: Path) -> list[validate_qa_build_records_dir.RecordValidationResult]:
    return []


def ok_manual_gates(_: Path) -> list[str]:
    return []


def ok_automated_gates(_: Path) -> list[str]:
    return []


def ok_runtime_boundary(_: Path) -> list[verify_runtime_boundary.BoundaryViolation]:
    return []


def runtime_boundary_rule(name: str) -> verify_runtime_boundary.BoundaryRule:
    for rule in verify_runtime_boundary.RULES:
        if rule.name == name:
            return rule
    raise AssertionError(f"missing runtime boundary rule: {name}")


def write_export_options(
    root: Path,
    *,
    team_id: str = "ABCD123456",
    method: str = "development",
) -> None:
    ios = root / "ios"
    ios.mkdir(exist_ok=True)
    with (ios / "ExportOptions.plist").open("wb") as handle:
        plistlib.dump({"teamID": team_id, "method": method}, handle)


class QaPreflightTest(unittest.TestCase):
    def make_root(self) -> Path:
        root = Path(self.tmp.name)
        (root / "docs" / "qa-builds").mkdir(parents=True, exist_ok=True)
        write_export_options(root)
        return root

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def test_ready_when_required_local_inputs_are_present(self) -> None:
        checks = qa_preflight.run_preflight(
            self.make_root(),
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            ios_wrapper_validator=ok_ios,
            ios_export_options_validator=lambda *_args, **_kwargs: [],
            records_dir_validator=empty_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
            runtime_boundary_validator=ok_runtime_boundary,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertFalse(any(check.is_blocker for check in checks))
        self.assertIn("[OK] Android local signing inputs", output)
        self.assertIn("[OK] Manual release gate documentation", output)
        self.assertIn("[OK] Automated release gate documentation", output)
        self.assertIn("[OK] Runtime boundary verification", output)
        self.assertIn("phone-local SSH dependencies", output)
        self.assertIn("terminal emulator dependencies", output)
        self.assertIn("custom Hub URL configuration", output)
        self.assertIn("redemption-code login", output)
        self.assertIn("arbitrary third-party LLM provider/base URL/API-key fields", output)
        self.assertIn("phone-side SSH credential save/read APIs", output)
        self.assertIn("38 automated release gates in runner order", output)
        self.assertIn("QA record validation/redaction rules", output)
        self.assertIn("scoped internal QA commands", output)
        self.assertIn("without secret redaction failures", output)
        self.assertIn("[INFO] Existing QA build records", output)
        self.assertIn("release_handoff.py", output)
        self.assertIn("--version <version+build>", output)
        self.assertIn("--team-id <APPLE_TEAM_ID>", output)
        self.assertIn("--export-method <export-method>", output)
        self.assertIn("--output docs/qa-builds/handoff-<version+build>.md", output)
        self.assertIn("verify_runtime_boundary.py", output)
        self.assertIn("run_release_gates.py", output)
        self.assertIn("--log", output)
        self.assertIn("tool/create_qa_build_record.py", output)
        self.assertIn(
            '--release-handoff-result "release_handoff.py output saved to docs/qa-builds/handoff-<version+build>.md"',
            output,
        )
        self.assertIn(
            '--runtime-boundary-result "MaClaw Mobile runtime boundary verified: no corelib, phone-local SSH, terminal emulator, phone-side SSH credential, custom Hub URL, redemption-code login, or third-party LLM provider/base URL/API-key regressions; log: docs/qa-builds/runtime-boundary-<version+build>.log"',
            output,
        )
        self.assertIn(
            '--automated-gates-result "run_release_gates.py: 38 gates passed; log: docs/qa-builds/release-gates-<version+build>.log"',
            output,
        )
        self.assertIn(
            release_evidence_commands.QA_RELEASE_EVIDENCE_LINK_COMMAND,
            output,
        )
        self.assertIn(
            release_evidence_commands.android_artifact_evidence_command(),
            output,
        )
        self.assertIn(
            release_evidence_commands.ios_artifact_evidence_command(
                archive_or_build="build/ios/archive/MaClawMobile.xcarchive",
                team_id=release_evidence_commands.DEFAULT_SIGNING_TEAM_ID,
            ),
            output,
        )
        self.assertIn("Result: READY", output)

    def test_custom_records_dir_controls_existing_record_checks_and_hints(self) -> None:
        root = self.make_root()
        records_dir = root / "custom-records"
        records_dir.mkdir()
        seen: dict[str, Path] = {}

        def validate_records(path: Path) -> list[validate_qa_build_records_dir.RecordValidationResult]:
            seen["records_dir"] = path
            return []

        checks = qa_preflight.run_preflight(
            root,
            scope="android",
            records_dir=records_dir,
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            records_dir_validator=validate_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertEqual(records_dir.resolve(), seen["records_dir"])
        self.assertIn(
            release_evidence_commands.qa_preflight_command(
                scope="android",
                records_dir=str(records_dir.resolve()),
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.create_record_command(
                scope="android",
                records_dir=str(records_dir.resolve()),
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.qa_release_evidence_link_command(
                scope="android",
                records_dir=str(records_dir.resolve()),
            ),
            output,
        )
        self.assertIn(
            release_evidence_commands.verify_final_release_evidence_command(
                str(records_dir.resolve()),
                scope="android",
                version=release_evidence_commands.DEFAULT_VERSION,
                log=release_evidence_commands.final_release_evidence_log_path(
                    release_evidence_commands.DEFAULT_VERSION,
                    scope="android",
                    records_dir=str(records_dir.resolve()),
                ),
            ),
            output,
        )
        self.assertNotIn("--records-dir docs/qa-builds", output)

    def test_signed_qa_record_hint_uses_shared_release_evidence_command(self) -> None:
        self.assertEqual(
            release_evidence_commands.signed_qa_record_hint(),
            qa_preflight.SIGNED_QA_RECORD_HINT,
        )
        self.assertIn(
            "release handoff is only a QA plan, not a completed QA record",
            qa_preflight.SIGNED_QA_RECORD_HINT,
        )

    def test_signed_qa_record_hint_preserves_supplied_ios_values(self) -> None:
        checks = qa_preflight.run_preflight(
            self.make_root(),
            ios_team_id="ABCDE12345",
            ios_export_method="ad-hoc",
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            ios_wrapper_validator=ok_ios,
            ios_export_options_validator=lambda *_args, **_kwargs: [],
            records_dir_validator=empty_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertIn("--team-id ABCDE12345", output)
        self.assertIn("--export-method ad-hoc", output)
        self.assertNotIn("--team-id <APPLE_TEAM_ID>", output)
        self.assertNotIn("--export-method <export-method>", output)

    def test_blockers_include_android_key_and_ios_wrapper_errors(self) -> None:
        checks = qa_preflight.run_preflight(
            self.make_root(),
            android_config_validator=ok_android_config,
            android_key_validator=lambda _: ({}, ["Missing Android signing file"]),
            ios_wrapper_validator=lambda _: ["Missing iOS wrapper file"],
            ios_export_options_validator=lambda *_args, **_kwargs: [],
            records_dir_validator=empty_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertTrue(any(check.is_blocker for check in checks))
        self.assertIn("[BLOCKER] Android local signing inputs", output)
        self.assertIn("Missing Android signing file", output)
        self.assertIn(release_evidence_commands.setup_android_signing_command(), output)
        self.assertIn("MACLAW_ANDROID_STORE_FILE", output)
        self.assertIn("MACLAW_ANDROID_KEY_PASSWORD", output)
        self.assertIn("[BLOCKER] iOS wrapper and Share Extension", output)
        self.assertIn("signed-build QA record creation is deferred", output)
        self.assertIn(
            "Run setup helper command(s) for the missing local release inputs",
            output,
        )
        self.assertIn("`python3 tool/setup_android_signing.py`", output)
        self.assertIn("MACLAW_ANDROID_STORE_FILE", output)
        self.assertIn("MACLAW_ANDROID_STORE_PASSWORD", output)
        self.assertIn("MACLAW_ANDROID_KEY_ALIAS", output)
        self.assertIn("MACLAW_ANDROID_KEY_PASSWORD", output)
        self.assertIn(
            "Do not add placeholder signing/export files; use real local signing material or keep the release in pre-signing setup state.",
            output,
        )
        self.assertIn(
            "re-run `"
            + release_evidence_commands.qa_preflight_command(
                team_id=release_evidence_commands.DEFAULT_TEAM_ID,
                export_method=release_evidence_commands.DEFAULT_EXPORT_METHOD,
                log=release_evidence_commands.preflight_log_path(),
            )
            + "`",
            output,
        )
        self.assertIn(
            "PowerShell treats unquoted `<...>` placeholders as redirection syntax",
            output,
        )
        self.assertNotIn(release_evidence_commands.release_handoff_command(), output)
        self.assertIn("Result: BLOCKED (2 blocker check(s)).", output)

    def test_runtime_boundary_violation_is_preflight_blocker(self) -> None:
        violation = verify_runtime_boundary.BoundaryViolation(
            path=Path("pubspec.lock"),
            line=3,
            rule=runtime_boundary_rule("phone-local ssh dependency"),
            text="dartssh2:",
        )
        checks = qa_preflight.run_preflight(
            self.make_root(),
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            ios_wrapper_validator=ok_ios,
            ios_export_options_validator=lambda *_args, **_kwargs: [],
            records_dir_validator=empty_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
            runtime_boundary_validator=lambda _: [violation],
        )
        output = qa_preflight.format_preflight(checks)

        self.assertTrue(any(check.is_blocker for check in checks))
        self.assertIn("[BLOCKER] Runtime boundary verification", output)
        self.assertIn("pubspec.lock:3: phone-local ssh dependency: dartssh2:", output)

    def test_phone_side_ssh_credential_api_is_preflight_blocker(self) -> None:
        violation = verify_runtime_boundary.BoundaryViolation(
            path=Path("lib/core/storage/secure_vault.dart"),
            line=34,
            rule=runtime_boundary_rule("phone-side ssh credential api"),
            text="Future<void> saveServerPassword(...)",
        )
        checks = qa_preflight.run_preflight(
            self.make_root(),
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            ios_wrapper_validator=ok_ios,
            ios_export_options_validator=lambda *_args, **_kwargs: [],
            records_dir_validator=empty_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
            runtime_boundary_validator=lambda _: [violation],
        )
        output = qa_preflight.format_preflight(checks)
        normalized_output = output.replace("\\", "/")

        self.assertTrue(any(check.is_blocker for check in checks))
        self.assertIn("[BLOCKER] Runtime boundary verification", output)
        self.assertIn(
            "lib/core/storage/secure_vault.dart:34: phone-side ssh credential api",
            normalized_output,
        )
        self.assertIn("saveServerPassword", output)

    def test_blocked_preflight_omits_powershell_placeholder_note_for_real_ios_values(self) -> None:
        checks = qa_preflight.run_preflight(
            self.make_root(),
            ios_team_id="ABCDE12345",
            ios_export_method="development",
            android_config_validator=ok_android_config,
            android_key_validator=lambda _: ({}, ["Missing Android signing file"]),
            ios_wrapper_validator=ok_ios,
            ios_export_options_validator=lambda *_args, **_kwargs: [],
            records_dir_validator=empty_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertIn("--team-id ABCDE12345", output)
        self.assertIn(
            "`python3 tool/setup_android_signing.py`",
            output,
        )
        self.assertIn(
            "Do not add placeholder signing/export files; use real local signing material or keep the release in pre-signing setup state.",
            output,
        )
        self.assertNotIn(
            "PowerShell treats unquoted `<...>` placeholders as redirection syntax",
            output,
        )

    def test_blocked_preflight_includes_ios_export_setup_helper(self) -> None:
        checks = qa_preflight.run_preflight(
            self.make_root(),
            ios_team_id="ABCDE12345",
            ios_export_method="development",
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            ios_wrapper_validator=ok_ios,
            ios_export_options_validator=lambda *_args, **_kwargs: [
                "Missing iOS export options plist: ios/ExportOptions.plist",
            ],
            records_dir_validator=empty_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertIn(
            "`python3 tool/setup_ios_export_options.py --team-id ABCDE12345 --export-method development`",
            output,
        )
        self.assertIn("Do not add placeholder signing/export files", output)
    def test_android_scope_skips_ios_preflight_checks(self) -> None:
        checks = qa_preflight.run_preflight(
            self.make_root(),
            scope="android",
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            ios_wrapper_validator=lambda _: ["unexpected iOS wrapper check"],
            ios_export_options_validator=lambda *_args, **_kwargs: [
                "unexpected iOS export check",
            ],
            records_dir_validator=empty_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertIn("[OK] Android local signing inputs", output)
        self.assertNotIn("iOS wrapper", output)
        self.assertNotIn("iOS export options", output)
        self.assertIn("python3 tool/qa_preflight.py --scope android", output)
        self.assertNotIn(
            "PowerShell treats unquoted `<...>` placeholders as redirection syntax",
            output,
        )

    def test_android_scope_treats_ios_only_records_as_out_of_scope(self) -> None:
        root = self.make_root()
        ios_record = validate_qa_build_records_dir.RecordValidationResult(
            path=root / "docs" / "qa-builds" / "2026-07-02-ios-1.0.0+42.md",
            errors=[],
        )

        checks = qa_preflight.run_preflight(
            root,
            scope="android",
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            records_dir_validator=lambda _: [ios_record],
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        existing_record_check = next(
            check for check in checks if check.name == "Existing QA build records"
        )
        self.assertEqual("info", existing_record_check.status)
        self.assertIn("1 out-of-scope completed record(s) already validate", output)
        self.assertIn("create an in-scope Android QA record", output)
        self.assertIn(
            release_evidence_commands.signed_qa_record_hint(scope="android"),
            output,
        )

    def test_android_scope_ignores_ios_only_invalid_records(self) -> None:
        root = self.make_root()
        ios_record = validate_qa_build_records_dir.RecordValidationResult(
            path=root / "docs" / "qa-builds" / "2026-07-02-ios-1.0.0+42.md",
            errors=["Missing TestFlight evidence"],
        )

        checks = qa_preflight.run_preflight(
            root,
            scope="android",
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            records_dir_validator=lambda _: [ios_record],
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        existing_record_check = next(
            check for check in checks if check.name == "Existing QA build records"
        )
        self.assertEqual("info", existing_record_check.status)
        self.assertIn("1 out-of-scope invalid record(s) ignored", output)
        self.assertIn("create an in-scope Android QA record", output)
        self.assertNotIn("[BLOCKER] Existing QA build records", output)
        self.assertNotIn("Missing TestFlight evidence", output)

    def test_android_scope_counts_only_in_scope_existing_records_as_ready(self) -> None:
        root = self.make_root()
        android_record = validate_qa_build_records_dir.RecordValidationResult(
            path=root / "docs" / "qa-builds" / "2026-07-02-android-1.0.0+42.md",
            errors=[],
        )
        ios_record = validate_qa_build_records_dir.RecordValidationResult(
            path=root / "docs" / "qa-builds" / "2026-07-02-ios-1.0.0+42.md",
            errors=[],
        )

        checks = qa_preflight.run_preflight(
            root,
            scope="android",
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            records_dir_validator=lambda _: [android_record, ios_record],
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        existing_record_check = next(
            check for check in checks if check.name == "Existing QA build records"
        )
        self.assertEqual("ok", existing_record_check.status)
        self.assertIn("1 in-scope completed record(s) already validate", output)
        self.assertIn("1 out-of-scope record(s) ignored for Android preflight", output)
        self.assertNotIn(
            release_evidence_commands.signed_qa_record_hint(scope="android"),
            output,
        )

    def test_ios_scope_skips_android_preflight_checks(self) -> None:
        checks = qa_preflight.run_preflight(
            self.make_root(),
            scope="ios",
            android_config_validator=lambda _: ["unexpected Android config check"],
            android_key_validator=lambda _: ({}, ["unexpected Android key check"]),
            ios_wrapper_validator=ok_ios,
            ios_export_options_validator=lambda *_args, **_kwargs: [],
            records_dir_validator=empty_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertNotIn("Android release signing", output)
        self.assertNotIn("Android local signing", output)
        self.assertIn("[OK] iOS wrapper and Share Extension", output)
        self.assertIn("python3 tool/qa_preflight.py --scope ios", output)

    def test_manual_release_gate_doc_errors_are_blockers(self) -> None:
        checks = qa_preflight.run_preflight(
            self.make_root(),
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            ios_wrapper_validator=ok_ios,
            ios_export_options_validator=lambda *_args, **_kwargs: [],
            records_dir_validator=empty_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=lambda _: [
                "qa_device_checklist.md must include final release evidence verifier log command",
            ],
        )
        output = qa_preflight.format_preflight(checks)

        self.assertTrue(
            any(
                check.name == "Manual release gate documentation" and check.is_blocker
                for check in checks
            )
        )
        self.assertIn("[BLOCKER] Manual release gate documentation", output)
        self.assertIn("final release evidence verifier log command", output)

    def test_automated_release_gate_doc_errors_are_blockers(self) -> None:
        checks = qa_preflight.run_preflight(
            self.make_root(),
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            ios_wrapper_validator=ok_ios,
            ios_export_options_validator=lambda *_args, **_kwargs: [],
            records_dir_validator=empty_records,
            automated_gate_validator=lambda _: [
                "release evidence automated gate order mismatch",
            ],
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertTrue(
            any(
                check.name == "Automated release gate documentation"
                and check.is_blocker
                for check in checks
            )
        )
        self.assertIn("[BLOCKER] Automated release gate documentation", output)
        self.assertIn("automated gate order mismatch", output)

    def test_invalid_existing_records_are_blockers(self) -> None:
        bad_result = validate_qa_build_records_dir.RecordValidationResult(
            path=self.make_root() / "docs" / "qa-builds" / "bad.md",
            errors=["Branch"],
        )

        checks = qa_preflight.run_preflight(
            self.make_root(),
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            ios_wrapper_validator=ok_ios,
            ios_export_options_validator=lambda *_args, **_kwargs: [],
            records_dir_validator=lambda _: [bad_result],
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertIn("[BLOCKER] Existing QA build records", output)
        self.assertIn("Branch", output)

    def test_missing_records_directory_is_blocker(self) -> None:
        root = Path(self.tmp.name)

        checks = qa_preflight.run_preflight(
            root,
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            ios_wrapper_validator=ok_ios,
            ios_export_options_validator=lambda *_args, **_kwargs: [],
            records_dir_validator=empty_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )

        self.assertTrue(any(check.name == "QA build record directory" and check.is_blocker for check in checks))

    def test_missing_ios_export_options_is_blocker(self) -> None:
        root = self.make_root()
        (root / "ios" / "ExportOptions.plist").unlink()

        checks = qa_preflight.run_preflight(
            root,
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            ios_wrapper_validator=ok_ios,
            records_dir_validator=empty_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertTrue(any(check.name == "iOS export options" and check.is_blocker for check in checks))
        self.assertIn("setup_ios_export_options.py", output)
        self.assertIn("--team-id <APPLE_TEAM_ID>", output)
        self.assertIn("--export-method development", output)

    def test_missing_ios_export_options_hint_uses_expected_values(self) -> None:
        root = self.make_root()
        (root / "ios" / "ExportOptions.plist").unlink()

        checks = qa_preflight.run_preflight(
            root,
            ios_team_id="ABCDE12345",
            ios_export_method="app-store",
            android_config_validator=ok_android_config,
            android_key_validator=ok_android_key,
            ios_wrapper_validator=ok_ios,
            records_dir_validator=empty_records,
            automated_gate_validator=ok_automated_gates,
            manual_gate_validator=ok_manual_gates,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertIn(
            release_evidence_commands.setup_ios_export_options_command(
                team_id="ABCDE12345",
                export_method="app-store",
            ),
            output,
        )

    def test_invalid_ios_export_options_fields_are_blockers(self) -> None:
        root = self.make_root()
        write_export_options(root, team_id="bad", method="invalid")

        errors = qa_preflight.validate_ios_export_options(root)

        self.assertTrue(any("teamID" in error for error in errors))
        self.assertTrue(any("method must be one of" in error for error in errors))

    def test_expected_ios_export_options_mismatch_is_blocker(self) -> None:
        root = self.make_root()
        write_export_options(root, team_id="ABCD123456", method="development")

        errors = qa_preflight.validate_ios_export_options(
            root,
            team_id="ZZZZ123456",
            export_method="ad-hoc",
        )

        self.assertTrue(any("teamID must match ZZZZ123456" in error for error in errors))
        self.assertTrue(any("method must match ad-hoc" in error for error in errors))

    def test_main_prints_blocked_to_stderr_for_current_missing_local_inputs(self) -> None:
        stderr = StringIO()

        with redirect_stderr(stderr):
            exit_code = qa_preflight.main(["--root", str(self.make_root())])

        self.assertEqual(1, exit_code)
        self.assertIn("MaClaw Mobile QA preflight:", stderr.getvalue())

    def test_main_writes_blocked_preflight_log(self) -> None:
        root = self.make_root()
        log = root / "docs" / "qa-builds" / "preflight-current.log"
        stderr = StringIO()

        with redirect_stderr(stderr):
            exit_code = qa_preflight.main(
                ["--root", str(root), "--log", str(log)],
            )

        self.assertEqual(1, exit_code)
        self.assertTrue(log.exists())
        self.assertEqual(stderr.getvalue(), log.read_text(encoding="utf-8"))
        self.assertIn("[BLOCKER] Android local signing inputs", log.read_text(encoding="utf-8"))
        self.assertIn("Result: BLOCKED", log.read_text(encoding="utf-8"))

    def test_main_refuses_to_overwrite_preflight_log_without_force(self) -> None:
        root = self.make_root()
        log = root / "docs" / "qa-builds" / "preflight-current.log"
        log.write_text("keep me", encoding="utf-8")
        stderr = StringIO()

        with redirect_stderr(stderr):
            exit_code = qa_preflight.main(
                ["--root", str(root), "--log", str(log)],
            )

        self.assertEqual(2, exit_code)
        self.assertEqual("keep me", log.read_text(encoding="utf-8"))
        self.assertIn("Refusing to overwrite existing preflight log", stderr.getvalue())

    def test_main_force_overwrites_preflight_log(self) -> None:
        root = self.make_root()
        log = root / "docs" / "qa-builds" / "preflight-current.log"
        log.write_text("old", encoding="utf-8")
        stderr = StringIO()

        with redirect_stderr(stderr):
            exit_code = qa_preflight.main(
                ["--root", str(root), "--log", str(log), "--force"],
            )

        self.assertEqual(1, exit_code)
        self.assertIn("MaClaw Mobile QA preflight:", log.read_text(encoding="utf-8"))
        self.assertNotEqual("old", log.read_text(encoding="utf-8"))

    def test_main_checks_expected_ios_export_options(self) -> None:
        stderr = StringIO()

        with redirect_stderr(stderr):
            exit_code = qa_preflight.main(
                [
                    "--root",
                    str(self.make_root()),
                    "--team-id",
                    "ZZZZ123456",
                    "--export-method",
                    "ad-hoc",
                ],
            )

        self.assertEqual(1, exit_code)
        self.assertIn("teamID must match ZZZZ123456", stderr.getvalue())
        self.assertIn("method must match ad-hoc", stderr.getvalue())

    def test_main_accepts_placeholder_team_id_for_planning_preflight(self) -> None:
        stderr = StringIO()

        with redirect_stderr(stderr):
            exit_code = qa_preflight.main(
                [
                    "--root",
                    str(self.make_root()),
                    "--team-id",
                    "<APPLE_TEAM_ID>",
                ],
            )

        self.assertEqual(1, exit_code)
        self.assertIn("MaClaw Mobile QA preflight:", stderr.getvalue())
        self.assertNotIn("team id must be", stderr.getvalue())

    def test_main_passes_custom_records_dir_to_preflight(self) -> None:
        root = self.make_root()
        records_dir = root / "custom-records"
        records_dir.mkdir()
        seen: dict[str, object] = {}
        stdout = StringIO()

        original_run_preflight = qa_preflight.run_preflight
        try:
            def fake_run_preflight(*args: object, **kwargs: object) -> list[qa_preflight.PreflightCheck]:
                seen["args"] = args
                seen["kwargs"] = kwargs
                return [qa_preflight.PreflightCheck("Stub", "ok", ["ready"])]

            qa_preflight.run_preflight = fake_run_preflight
            with redirect_stdout(stdout):
                exit_code = qa_preflight.main(
                    [
                        "--root",
                        str(root),
                        "--scope",
                        "android",
                        "--records-dir",
                        str(records_dir),
                    ],
                )
        finally:
            qa_preflight.run_preflight = original_run_preflight

        self.assertEqual(0, exit_code)
        self.assertEqual((root,), seen["args"])
        self.assertEqual("android", seen["kwargs"]["scope"])
        self.assertEqual(records_dir, seen["kwargs"]["records_dir"])

    def test_main_prints_ready_to_stdout_with_stubbed_checks(self) -> None:
        root = self.make_root()
        stdout = StringIO()

        original_run_preflight = qa_preflight.run_preflight
        try:
            qa_preflight.run_preflight = lambda *_args, **_kwargs: [
                qa_preflight.PreflightCheck("Stub", "ok", ["ready"]),
            ]
            with redirect_stdout(stdout):
                exit_code = qa_preflight.main(["--root", str(root)])
        finally:
            qa_preflight.run_preflight = original_run_preflight

        self.assertEqual(0, exit_code)
        self.assertIn("Result: READY", stdout.getvalue())


if __name__ == "__main__":
    unittest.main()
