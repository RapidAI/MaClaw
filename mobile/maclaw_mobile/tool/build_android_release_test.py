from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))

import build_android_release


def write_key_properties(root: Path, store_file: str = "release.jks") -> Path:
    android = root / "android"
    android.mkdir(parents=True, exist_ok=True)
    store = android / store_file
    store.write_text("keystore", encoding="utf-8")
    (android / "key.properties").write_text(
        f"storeFile={store_file}\n"
        "storePassword=redacted-store-password\n"
        "keyAlias=maclaw-mobile\n"
        "keyPassword=redacted-key-password\n",
        encoding="utf-8",
    )
    return store


class BuildAndroidReleaseTest(unittest.TestCase):
    def test_validate_key_properties_requires_local_file_and_fields(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)

            values, errors = build_android_release.validate_key_properties(root)

        self.assertEqual({}, values)
        self.assertTrue(any("Missing Android signing file" in error for error in errors))

    def test_validate_key_properties_rejects_debug_keystore(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_key_properties(root, store_file="debug.keystore")

            _, errors = build_android_release.validate_key_properties(root)

        self.assertIn("Android signing storeFile must not be a debug keystore", errors)

    def test_build_plan_for_release_apk_includes_version_flags(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            store = write_key_properties(root)

            plan = build_android_release.build_plan(
                root,
                artifact="apk",
                build_name="1.2.3",
                build_number="45",
            )

        self.assertEqual(
            ["flutter", "build", "apk", "--release", "--build-name", "1.2.3", "--build-number", "45"],
            plan.command,
        )
        self.assertEqual(store, plan.key_store_path)
        self.assertEqual(
            root / "build" / "app" / "outputs" / "flutter-apk" / "app-release.apk",
            plan.artifact_path,
        )

    def test_build_plan_for_appbundle_points_to_aab(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_key_properties(root)

            plan = build_android_release.build_plan(
                root,
                artifact="appbundle",
                build_name="1.2.3",
                build_number="45",
            )

        self.assertEqual(
            [
                "flutter",
                "build",
                "appbundle",
                "--release",
                "--build-name",
                "1.2.3",
                "--build-number",
                "45",
            ],
            plan.command,
        )
        self.assertEqual(
            root / "build" / "app" / "outputs" / "bundle" / "release" / "app-release.aab",
            plan.artifact_path,
        )

    def test_build_plan_rejects_unknown_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_key_properties(root)

            with self.assertRaisesRegex(ValueError, "--artifact"):
                build_android_release.build_plan(
                    root,
                    artifact="aab",
                    build_name="1.2.3",
                    build_number="45",
                )

    def test_build_plan_requires_trackable_version_and_build_number(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_key_properties(root)

            with self.assertRaisesRegex(ValueError, "both --build-name and --build-number"):
                build_android_release.build_plan(
                    root,
                    artifact="apk",
                    build_name=None,
                    build_number="42",
                )

            with self.assertRaisesRegex(ValueError, "both --build-name and --build-number"):
                build_android_release.build_plan(
                    root,
                    artifact="apk",
                    build_name="1.0.0",
                    build_number=None,
                )

    def test_build_plan_rejects_untrackable_version_inputs(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_key_properties(root)

            with self.assertRaisesRegex(ValueError, "--build-name"):
                build_android_release.build_plan(
                    root,
                    artifact="apk",
                    build_name="release-candidate",
                    build_number="42",
                )

            with self.assertRaisesRegex(ValueError, "--build-number"):
                build_android_release.build_plan(
                    root,
                    artifact="apk",
                    build_name="1.0.0",
                    build_number="rc42",
                )

    def test_main_dry_run_does_not_execute_flutter(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_key_properties(root)
            output = StringIO()

            with patch(
                "verify_android_release_signing.verify_android_release_signing",
                return_value=[],
            ), patch("build_android_release.subprocess.run") as run, redirect_stdout(output):
                exit_code = build_android_release.main(
                    [
                        "--root",
                        str(root),
                        "--artifact",
                        "apk",
                        "--build-name",
                        "1.0.0",
                        "--build-number",
                        "42",
                        "--dry-run",
                    ],
                )

        self.assertEqual(0, exit_code)
        run.assert_not_called()
        self.assertIn("flutter build apk --release", output.getvalue())
        self.assertIn("Dry run only", output.getvalue())

    def test_main_reports_missing_key_properties_without_traceback(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            error = StringIO()

            with patch(
                "verify_android_release_signing.verify_android_release_signing",
                return_value=[],
            ), redirect_stderr(error):
                exit_code = build_android_release.main(["--root", str(root), "--dry-run"])

        self.assertEqual(1, exit_code)
        self.assertIn("Android release build cannot start", error.getvalue())
        self.assertIn("python3 tool/setup_android_signing.py", error.getvalue())
        self.assertIn("release signing environment variables", error.getvalue())

    def test_main_does_not_suggest_signing_setup_for_version_errors(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_key_properties(root)
            error = StringIO()

            with patch(
                "verify_android_release_signing.verify_android_release_signing",
                return_value=[],
            ), redirect_stderr(error):
                exit_code = build_android_release.main(
                    [
                        "--root",
                        str(root),
                        "--artifact",
                        "apk",
                        "--build-name",
                        "1.0.0",
                        "--dry-run",
                    ],
                )

        self.assertEqual(1, exit_code)
        self.assertIn("both --build-name and --build-number", error.getvalue())
        self.assertNotIn("setup_android_signing.py", error.getvalue())

    def test_main_requires_complete_qa_evidence_options(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_key_properties(root)
            error = StringIO()

            with redirect_stderr(error):
                exit_code = build_android_release.main(
                    [
                        "--root",
                        str(root),
                        "--artifact",
                        "apk",
                        "--build-name",
                        "1.0.0",
                        "--build-number",
                        "42",
                        "--record-dir",
                        str(root / "docs" / "qa-builds"),
                        "--dry-run",
                    ],
                )

        self.assertEqual(1, exit_code)
        self.assertIn("requires --record-dir", error.getvalue())

    def test_main_rejects_missing_record_dir_before_flutter_build(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_key_properties(root)
            error = StringIO()

            with patch(
                "verify_android_release_signing.verify_android_release_signing",
                return_value=[],
            ) as signing_check, patch(
                "build_android_release.subprocess.run",
            ) as run, redirect_stderr(error):
                exit_code = build_android_release.main(
                    [
                        "--root",
                        str(root),
                        "--artifact",
                        "apk",
                        "--build-name",
                        "1.0.0",
                        "--build-number",
                        "42",
                        "--record-dir",
                        str(root / "docs" / "missing-qa-builds"),
                        "--signing-identity",
                        "Android release keystore alias maclaw-mobile",
                        "--installer-channel",
                        "internal app sharing",
                    ],
                )

        self.assertEqual(1, exit_code)
        signing_check.assert_not_called()
        run.assert_not_called()
        self.assertIn("QA record directory does not exist", error.getvalue())

    def test_help_describes_record_dir_as_qa_records_directory(self) -> None:
        output = StringIO()

        with redirect_stdout(output):
            with self.assertRaises(SystemExit) as raised:
                build_android_release.main(["--help"])

        self.assertEqual(0, raised.exception.code)
        text = output.getvalue()
        self.assertIn("Optional QA records directory", text)
        self.assertIn("examples use docs/qa-builds", text)
        self.assertNotIn("Optional docs/qa-builds directory", text)

    def test_main_reports_flutter_build_failure_without_traceback(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_key_properties(root)
            output = StringIO()
            error = StringIO()

            with patch(
                "verify_android_release_signing.verify_android_release_signing",
                return_value=[],
            ), patch(
                "build_android_release.subprocess.run",
                side_effect=subprocess.CalledProcessError(7, ["flutter", "build", "apk"]),
            ), redirect_stdout(output), redirect_stderr(error):
                exit_code = build_android_release.main(
                    [
                        "--root",
                        str(root),
                        "--artifact",
                        "apk",
                        "--build-name",
                        "1.0.0",
                        "--build-number",
                        "42",
                    ],
                )

        self.assertEqual(1, exit_code)
        self.assertIn("Android release signing inputs verified", output.getvalue())
        self.assertIn("Android release build failed with exit code 7", error.getvalue())
        self.assertNotIn("Traceback", error.getvalue())

    def test_main_prints_paste_ready_qa_artifact_evidence_after_success(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_key_properties(root)
            record_dir = root / "docs" / "qa-builds"
            record_dir.mkdir(parents=True)
            output = StringIO()

            def create_artifact(*_args, **_kwargs):
                artifact = root / "build" / "app" / "outputs" / "flutter-apk" / "app-release.apk"
                artifact.parent.mkdir(parents=True)
                artifact.write_bytes(b"signed release apk bytes")
                return subprocess.CompletedProcess(args=["flutter"], returncode=0)

            with patch(
                "verify_android_release_signing.verify_android_release_signing",
                return_value=[],
            ), patch(
                "build_android_release.subprocess.run",
                side_effect=create_artifact,
            ), redirect_stdout(output):
                exit_code = build_android_release.main(
                    [
                        "--root",
                        str(root),
                        "--artifact",
                        "apk",
                        "--build-name",
                        "1.0.0",
                        "--build-number",
                        "42",
                        "--record-dir",
                        str(record_dir),
                        "--signing-identity",
                        "Android release keystore alias maclaw-mobile",
                        "--installer-channel",
                        "internal app sharing",
                    ],
                )

        self.assertEqual(0, exit_code)
        text = output.getvalue()
        self.assertIn("QA artifact evidence:", text)
        self.assertIn("Artifact path: ../../build/app/outputs/flutter-apk/app-release.apk", text)
        self.assertIn("Version/build number: 1.0.0+42", text)
        self.assertIn("Signing identity: Android release keystore alias maclaw-mobile", text)
        self.assertIn("Installer channel: internal app sharing", text)


if __name__ == "__main__":
    unittest.main()
