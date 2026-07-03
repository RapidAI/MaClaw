from __future__ import annotations

import hashlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import signed_artifact_evidence


class SignedArtifactEvidenceTest(unittest.TestCase):
    def test_android_evidence_includes_hash_size_and_optional_fields(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            artifact = root / "build" / "app" / "outputs" / "flutter-apk" / "app-release.apk"
            artifact.parent.mkdir(parents=True)
            content = b"signed apk bytes"
            artifact.write_bytes(content)
            record_dir = root / "docs" / "qa-builds"
            record_dir.mkdir(parents=True)

            lines = signed_artifact_evidence.android_evidence_lines(
                artifact,
                record_dir=record_dir,
                version="1.0.0+42",
                signing_identity="release alias upload key SHA256:AA",
                installer_channel="Firebase App Distribution internal track",
            )

            output = "\n".join(lines)
            self.assertIn("Artifact path: ..\\..\\build\\app\\outputs\\flutter-apk\\app-release.apk", output)
            self.assertIn(
                f"SHA256: {hashlib.sha256(content).hexdigest().upper()}",
                output,
            )
            self.assertIn("Artifact size bytes: 16", output)
            self.assertIn("Version/build number: 1.0.0+42", output)
            self.assertIn("Signing identity: release alias", output)
            self.assertIn("Installer channel: Firebase App Distribution", output)

    def test_android_evidence_rejects_debug_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            artifact = Path(tmp) / "app-debug.apk"
            artifact.write_bytes(b"debug")

            with self.assertRaisesRegex(ValueError, "must not contain debug"):
                signed_artifact_evidence.android_evidence_lines(artifact)

    def test_android_evidence_rejects_missing_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(FileNotFoundError):
                signed_artifact_evidence.android_evidence_lines(
                    Path(tmp) / "app-release.apk",
                )

    def test_android_evidence_rejects_untrackable_artifact_name(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            artifact = Path(tmp) / "app.apk"
            artifact.write_bytes(b"apk")

            with self.assertRaisesRegex(ValueError, "signed/release/internal"):
                signed_artifact_evidence.android_evidence_lines(artifact)

    def test_android_evidence_rejects_untrackable_version(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            artifact = Path(tmp) / "app-release.apk"
            artifact.write_bytes(b"signed")

            with self.assertRaisesRegex(ValueError, "Version/build number"):
                signed_artifact_evidence.android_evidence_lines(
                    artifact,
                    version="release-candidate",
                )

    def test_android_evidence_requires_release_identity_fields(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            artifact = Path(tmp) / "app-release.apk"
            artifact.write_bytes(b"signed")

            with self.assertRaisesRegex(ValueError, "Version/build number is required"):
                signed_artifact_evidence.android_evidence_lines(
                    artifact,
                    signing_identity="release alias upload key SHA256:AA",
                    installer_channel="Firebase App Distribution internal track",
                )

            with self.assertRaisesRegex(ValueError, "Signing identity is required"):
                signed_artifact_evidence.android_evidence_lines(
                    artifact,
                    version="1.0.0+42",
                    installer_channel="Firebase App Distribution internal track",
                )

            with self.assertRaisesRegex(ValueError, "Installer channel is required"):
                signed_artifact_evidence.android_evidence_lines(
                    artifact,
                    version="1.0.0+42",
                    signing_identity="release alias upload key SHA256:AA",
                )

    def test_android_evidence_rejects_untrackable_signing_and_installer(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            artifact = Path(tmp) / "app-release.apk"
            artifact.write_bytes(b"signed")

            with self.assertRaisesRegex(ValueError, "Signing identity"):
                signed_artifact_evidence.android_evidence_lines(
                    artifact,
                    version="1.0.0+42",
                    signing_identity="debug alias",
                    installer_channel="Firebase App Distribution internal track",
                )

            with self.assertRaisesRegex(ValueError, "Installer channel"):
                signed_artifact_evidence.android_evidence_lines(
                    artifact,
                    version="1.0.0+42",
                    signing_identity="release alias upload key SHA256:AA",
                    installer_channel="debug sideload",
                )

    def test_ios_evidence_includes_archive_team_and_profiles(self) -> None:
        lines = signed_artifact_evidence.ios_evidence_lines(
            archive_or_build="build/ios/archive/Runner.xcarchive",
            team_id="abcde12345",
            provisioning_profiles="Runner profile UUID abc123; Share Extension profile UUID def456",
        )

        self.assertEqual(
            [
                "Archive/TestFlight build: build/ios/archive/Runner.xcarchive",
                "Team ID: ABCDE12345",
                "Provisioning profiles: Runner profile UUID abc123; Share Extension profile UUID def456",
            ],
            lines,
        )

    def test_ios_evidence_accepts_explicit_testflight_build(self) -> None:
        lines = signed_artifact_evidence.ios_evidence_lines(
            archive_or_build="TestFlight build 42",
            team_id="ABCDE12345",
            provisioning_profiles=(
                "Runner provisioning profile UUID abc123; "
                "Share Extension provisioning profile UUID def456"
            ),
        )

        self.assertIn("Archive/TestFlight build: TestFlight build 42", lines)

    def test_ios_evidence_accepts_profile_files_and_names(self) -> None:
        file_lines = signed_artifact_evidence.ios_evidence_lines(
            archive_or_build="TestFlight build 42",
            team_id="ABCDE12345",
            provisioning_profiles=(
                "Runner Release.mobileprovision; "
                "Share Extension Release.mobileprovision"
            ),
        )
        name_lines = signed_artifact_evidence.ios_evidence_lines(
            archive_or_build="TestFlight build 42",
            team_id="ABCDE12345",
            provisioning_profiles=(
                "Runner profile name MaClaw Runner Release; "
                "Share Extension profile name MaClaw Share Extension Release"
            ),
        )

        self.assertIn("Runner Release.mobileprovision", "\n".join(file_lines))
        self.assertIn("MaClaw Share Extension Release", "\n".join(name_lines))

    def test_ios_evidence_rejects_untrackable_fields(self) -> None:
        with self.assertRaisesRegex(ValueError, "Archive/TestFlight build"):
            signed_artifact_evidence.ios_evidence_lines(
                archive_or_build="release build",
                team_id="ABCDE12345",
                provisioning_profiles="Runner profile UUID and Share Extension profile UUID",
            )

        with self.assertRaisesRegex(ValueError, "Team ID"):
            signed_artifact_evidence.ios_evidence_lines(
                archive_or_build="TestFlight build 42",
                team_id="<APPLE_TEAM_ID>",
                provisioning_profiles="Runner profile UUID and Share Extension profile UUID",
            )

        with self.assertRaisesRegex(ValueError, "Provisioning profiles"):
            signed_artifact_evidence.ios_evidence_lines(
                archive_or_build="TestFlight build 42",
                team_id="ABCDE12345",
                provisioning_profiles="profiles present",
            )

        with self.assertRaisesRegex(ValueError, "Provisioning profiles"):
            signed_artifact_evidence.ios_evidence_lines(
                archive_or_build="TestFlight build 42",
                team_id="ABCDE12345",
                provisioning_profiles="Runner profile UUID and Share Extension profile UUID",
            )

    def test_android_cli_prints_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            artifact = root / "app-release.apk"
            artifact.write_bytes(b"signed")
            stdout = StringIO()

            with redirect_stdout(stdout):
                exit_code = signed_artifact_evidence.main(
                    [
                        "android",
                        str(artifact),
                        "--version",
                        "1.0.0+42",
                        "--signing-identity",
                        "release alias upload key SHA256:AA",
                        "--installer-channel",
                        "Firebase App Distribution internal track",
                    ],
                )

        self.assertEqual(0, exit_code)
        self.assertIn("Artifact path:", stdout.getvalue())
        self.assertIn("Version/build number: 1.0.0+42", stdout.getvalue())
        self.assertIn("Signing identity: release alias", stdout.getvalue())
        self.assertIn("Installer channel: Firebase App Distribution", stdout.getvalue())

    def test_android_cli_requires_release_identity_arguments(self) -> None:
        stderr = StringIO()
        with tempfile.TemporaryDirectory() as tmp:
            artifact = Path(tmp) / "app-release.apk"
            artifact.write_bytes(b"signed")

            with redirect_stderr(stderr):
                with self.assertRaises(SystemExit) as raised:
                    signed_artifact_evidence.main(["android", str(artifact)])

        self.assertEqual(2, raised.exception.code)
        self.assertIn("--version", stderr.getvalue())
        self.assertIn("--signing-identity", stderr.getvalue())
        self.assertIn("--installer-channel", stderr.getvalue())

    def test_ios_cli_reports_placeholder_errors(self) -> None:
        stderr = StringIO()

        with redirect_stderr(stderr):
            exit_code = signed_artifact_evidence.main(
                [
                    "ios",
                    "--archive-or-build",
                    "<Xcode archive path or TestFlight build number>",
                    "--team-id",
                    "<APPLE_TEAM_ID>",
                    "--provisioning-profiles",
                    "<Runner profile UUID/name; Share Extension profile UUID/name>",
                ],
            )

        self.assertEqual(1, exit_code)
        self.assertIn("Signed artifact evidence generation failed", stderr.getvalue())
        self.assertIn("Archive/TestFlight build", stderr.getvalue())

    def test_cli_reports_errors_to_stderr(self) -> None:
        stderr = StringIO()
        with tempfile.TemporaryDirectory() as tmp:
            with redirect_stderr(stderr):
                exit_code = signed_artifact_evidence.main(
                    [
                        "android",
                        str(Path(tmp) / "missing-release.apk"),
                        "--version",
                        "1.0.0+42",
                        "--signing-identity",
                        "release alias upload key SHA256:AA",
                        "--installer-channel",
                        "Firebase App Distribution internal track",
                    ],
                )

        self.assertEqual(1, exit_code)
        self.assertIn("Signed artifact evidence generation failed", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
