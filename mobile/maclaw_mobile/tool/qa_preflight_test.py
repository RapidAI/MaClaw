from __future__ import annotations

import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import qa_preflight
import validate_qa_build_records_dir


def ok_android_config(_: Path) -> list[str]:
    return []


def ok_android_key(_: Path) -> tuple[dict[str, str], list[str]]:
    return {"storeFile": "release.jks"}, []


def ok_ios(_: Path) -> list[str]:
    return []


def empty_records(_: Path) -> list[validate_qa_build_records_dir.RecordValidationResult]:
    return []


class QaPreflightTest(unittest.TestCase):
    def make_root(self) -> Path:
        root = Path(self.tmp.name)
        (root / "docs" / "qa-builds").mkdir(parents=True, exist_ok=True)
        (root / "ios").mkdir(exist_ok=True)
        (root / "ios" / "ExportOptions.plist").write_text("plist", encoding="utf-8")
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
            ios_export_options_validator=lambda _: [],
            records_dir_validator=empty_records,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertFalse(any(check.is_blocker for check in checks))
        self.assertIn("[OK] Android local signing inputs", output)
        self.assertIn("[INFO] Existing QA build records", output)
        self.assertIn("Result: READY", output)

    def test_blockers_include_android_key_and_ios_wrapper_errors(self) -> None:
        checks = qa_preflight.run_preflight(
            self.make_root(),
            android_config_validator=ok_android_config,
            android_key_validator=lambda _: ({}, ["Missing Android signing file"]),
            ios_wrapper_validator=lambda _: ["Missing iOS wrapper file"],
            ios_export_options_validator=lambda _: [],
            records_dir_validator=empty_records,
        )
        output = qa_preflight.format_preflight(checks)

        self.assertTrue(any(check.is_blocker for check in checks))
        self.assertIn("[BLOCKER] Android local signing inputs", output)
        self.assertIn("Missing Android signing file", output)
        self.assertIn("[BLOCKER] iOS wrapper and Share Extension", output)
        self.assertIn("Result: BLOCKED (2 blocker check(s)).", output)

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
            ios_export_options_validator=lambda _: [],
            records_dir_validator=lambda _: [bad_result],
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
            ios_export_options_validator=lambda _: [],
            records_dir_validator=empty_records,
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
        )
        output = qa_preflight.format_preflight(checks)

        self.assertTrue(any(check.name == "iOS export options" and check.is_blocker for check in checks))
        self.assertIn("setup_ios_export_options.py", output)

    def test_main_prints_blocked_to_stderr_for_current_missing_local_inputs(self) -> None:
        stderr = StringIO()

        with redirect_stderr(stderr):
            exit_code = qa_preflight.main(["--root", str(self.make_root())])

        self.assertEqual(1, exit_code)
        self.assertIn("MaClaw Mobile QA preflight:", stderr.getvalue())

    def test_main_prints_ready_to_stdout_with_stubbed_checks(self) -> None:
        root = self.make_root()
        stdout = StringIO()

        original_run_preflight = qa_preflight.run_preflight
        try:
            qa_preflight.run_preflight = lambda _: [
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
