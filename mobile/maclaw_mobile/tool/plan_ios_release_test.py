from __future__ import annotations

import plistlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))

import plan_ios_release


def write_export_options(
    root: Path,
    *,
    team_id: str = "ABCD123456",
    method: str = "development",
) -> None:
    ios = root / "ios"
    ios.mkdir(parents=True, exist_ok=True)
    with (ios / "ExportOptions.plist").open("wb") as handle:
        plistlib.dump({"teamID": team_id, "method": method}, handle)


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

    def test_validate_export_options_requires_matching_team_and_method(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_export_options(root, team_id="ZZZZ123456", method="app-store")

            errors = plan_ios_release.validate_export_options(
                root / "ios" / "ExportOptions.plist",
                team_id="ABCD123456",
                export_method="development",
            )

        self.assertTrue(any("teamID must match ABCD123456" in error for error in errors))
        self.assertTrue(any("method must match development" in error for error in errors))

    def test_main_prints_qA_fields_when_wrapper_is_valid(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_export_options(root, method="ad-hoc")
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
                        "--provisioning-profiles",
                        "Runner profile UUID abc123; Share Extension profile UUID def456",
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
        self.assertIn("iOS QA artifact evidence:", text)
        self.assertIn("Archive/TestFlight build: build/ios/archive/MaClawMobile.xcarchive", text)
        self.assertIn("Team ID: ABCD123456", text)
        self.assertIn("Provisioning profiles: Runner profile UUID abc123; Share Extension profile UUID def456", text)
        self.assertIn("Record the .xcarchive path or TestFlight build number", text)

    def test_main_record_dir_validates_local_archive_and_prints_relative_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_export_options(root)
            archive = root / "build" / "ios" / "archive" / "MaClawMobile.xcarchive"
            archive.mkdir(parents=True)
            record_dir = root / "docs" / "qa-builds"
            record_dir.mkdir(parents=True)
            output = StringIO()

            with patch("verify_ios_wrapper.verify_ios_wrapper", return_value=[]), redirect_stdout(output):
                exit_code = plan_ios_release.main(
                    [
                        "--root",
                        tmp,
                        "--team-id",
                        "ABCD123456",
                        "--provisioning-profiles",
                        "Runner profile UUID abc123; Share Extension profile UUID def456",
                        "--record-dir",
                        "docs/qa-builds",
                    ],
                )

        text = output.getvalue()
        self.assertEqual(0, exit_code)
        self.assertIn("iOS QA artifact evidence:", text)
        self.assertIn(
            "Archive/TestFlight build: ../../build/ios/archive/MaClawMobile.xcarchive",
            text,
        )

    def test_main_record_dir_rejects_missing_local_archive_without_traceback(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_export_options(root)
            output = StringIO()
            error = StringIO()

            with patch("verify_ios_wrapper.verify_ios_wrapper", return_value=[]), redirect_stdout(output), redirect_stderr(error):
                exit_code = plan_ios_release.main(
                    [
                        "--root",
                        tmp,
                        "--team-id",
                        "ABCD123456",
                        "--provisioning-profiles",
                        "Runner profile UUID abc123; Share Extension profile UUID def456",
                        "--record-dir",
                        "docs/qa-builds",
                    ],
                )

        self.assertEqual(1, exit_code)
        self.assertIn("iOS QA artifact evidence could not be generated", error.getvalue())
        self.assertIn("iOS archive does not exist", error.getvalue())
        self.assertNotIn("Traceback", error.getvalue())

    def test_main_requires_profiles_when_record_dir_is_used(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            error = StringIO()

            with patch(
                "verify_ios_wrapper.verify_ios_wrapper",
                return_value=[],
            ) as wrapper_check, redirect_stderr(error):
                exit_code = plan_ios_release.main(
                    [
                        "--root",
                        tmp,
                        "--team-id",
                        "ABCD123456",
                        "--record-dir",
                        "docs/qa-builds",
                    ],
                )

        self.assertEqual(1, exit_code)
        wrapper_check.assert_not_called()
        self.assertIn("--record-dir requires --provisioning-profiles", error.getvalue())

    def test_help_describes_record_dir_as_qa_records_directory(self) -> None:
        output = StringIO()

        with redirect_stdout(output):
            with self.assertRaises(SystemExit) as raised:
                plan_ios_release.main(["--help"])

        self.assertEqual(0, raised.exception.code)
        text = output.getvalue()
        self.assertIn("Optional QA records directory", text)
        self.assertIn("Default examples use docs/qa-builds", text)
        self.assertNotIn("Optional docs/qa-builds directory", text)

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
        self.assertIn(
            "python3 tool/setup_ios_export_options.py --team-id ABCD123456 --export-method development",
            error.getvalue(),
        )
        self.assertNotIn("<APPLE_TEAM_ID>", error.getvalue())

    def test_main_reports_mismatched_export_options_without_traceback(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_export_options(root, team_id="ZZZZ123456", method="app-store")
            error = StringIO()
            with patch("verify_ios_wrapper.verify_ios_wrapper", return_value=[]), redirect_stderr(error):
                exit_code = plan_ios_release.main(
                    [
                        "--root",
                        tmp,
                        "--team-id",
                        "ABCD123456",
                        "--export-method",
                        "development",
                    ],
                )

        self.assertEqual(1, exit_code)
        self.assertIn("teamID must match ABCD123456", error.getvalue())
        self.assertIn("method must match development", error.getvalue())
        self.assertIn(
            "python3 tool/setup_ios_export_options.py --team-id ABCD123456 --export-method development",
            error.getvalue(),
        )
        self.assertNotIn("Traceback", error.getvalue())


if __name__ == "__main__":
    unittest.main()
