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


if __name__ == "__main__":
    unittest.main()
