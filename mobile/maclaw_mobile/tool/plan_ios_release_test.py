from __future__ import annotations

import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))

import plan_ios_release


class PlanIOSReleaseTest(unittest.TestCase):
    def test_validate_team_id_normalizes_valid_identifier(self) -> None:
        self.assertEqual("ABCD123456", plan_ios_release.validate_team_id("abcd123456"))

    def test_validate_team_id_rejects_invalid_identifier(self) -> None:
        with self.assertRaises(plan_ios_release.argparse.ArgumentTypeError):
            plan_ios_release.validate_team_id("too-short")

    def test_release_plan_builds_archive_and_export_commands(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            plan = plan_ios_release.release_plan(
                root,
                team_id="ABCD123456",
                export_method="development",
                archive_path=Path("build/ios/archive/MaClawMobile.xcarchive"),
                export_dir=Path("build/ios/export"),
                export_options_path=Path("ios/ExportOptions.plist"),
            )

        self.assertEqual("xcodebuild", plan.archive_command[0])
        self.assertIn("Runner", plan.archive_command)
        self.assertIn("DEVELOPMENT_TEAM=ABCD123456", plan.archive_command)
        self.assertEqual("xcodebuild", plan.export_command[0])
        self.assertIn("-exportArchive", plan.export_command)
        self.assertIn(str(Path("ios/ExportOptions.plist")), plan.export_command)
        self.assertEqual(Path("ios/ExportOptions.plist"), plan.export_options_path)

    def test_release_plan_rejects_unknown_export_method(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(ValueError):
                plan_ios_release.release_plan(
                    Path(tmp),
                    team_id="ABCD123456",
                    export_method="invalid",
                    archive_path=Path("a.xcarchive"),
                    export_dir=Path("export"),
                    export_options_path=Path("ios/ExportOptions.plist"),
                )

    def test_main_prints_qA_fields_when_wrapper_is_valid(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "ios").mkdir()
            (root / "ios" / "ExportOptions.plist").write_text("plist", encoding="utf-8")
            output = StringIO()
            with patch("verify_ios_wrapper.verify_ios_wrapper", return_value=[]), redirect_stdout(output):
                exit_code = plan_ios_release.main(
                    [
                        "--root",
                        tmp,
                        "--team-id",
                        "ABCD123456",
                        "--export-method",
                        "ad-hoc",
                    ],
                )

        text = output.getvalue()
        self.assertEqual(0, exit_code)
        self.assertIn("Runner bundle id: top.mypapers.maclaw.mobile", text)
        self.assertIn("Share Extension bundle id: top.mypapers.maclaw.mobile.ShareExtension", text)
        self.assertIn("App group: group.top.mypapers.maclaw.mobile", text)
        self.assertIn("Export options:", text)
        self.assertIn("ExportOptions.plist", text)
        self.assertIn("Archive command: xcodebuild archive", text)
        self.assertIn("Record the .xcarchive path or TestFlight build number", text)

    def test_main_reports_wrapper_errors_without_traceback(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            error = StringIO()
            with patch(
                "verify_ios_wrapper.verify_ios_wrapper",
                return_value=["missing Share Extension entitlement"],
            ), redirect_stderr(error):
                exit_code = plan_ios_release.main(
                    ["--root", tmp, "--team-id", "ABCD123456"],
                )

        self.assertEqual(1, exit_code)
        self.assertIn("iOS wrapper is not ready", error.getvalue())
        self.assertIn("missing Share Extension entitlement", error.getvalue())

    def test_main_reports_missing_export_options_without_traceback(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            error = StringIO()
            with patch("verify_ios_wrapper.verify_ios_wrapper", return_value=[]), redirect_stderr(error):
                exit_code = plan_ios_release.main(
                    ["--root", tmp, "--team-id", "ABCD123456"],
                )

        self.assertEqual(1, exit_code)
        self.assertIn("iOS export options are not ready", error.getvalue())
        self.assertIn("setup_ios_export_options.py", error.getvalue())


if __name__ == "__main__":
    unittest.main()
