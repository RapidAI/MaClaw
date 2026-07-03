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

    def test_ios_evidence_includes_archive_team_and_profiles(self) -> None:
        lines = signed_artifact_evidence.ios_evidence_lines(
            archive_or_build="build/ios/archive/Runner.xcarchive",
            team_id="abcde12345",
            provisioning_profiles="Runner profile UUID and Share Extension profile UUID",
        )

        self.assertEqual(
            [
                "Archive/TestFlight build: build/ios/archive/Runner.xcarchive",
                "Team ID: ABCDE12345",
                "Provisioning profiles: Runner profile UUID and Share Extension profile UUID",
            ],
            lines,
        )

    def test_android_cli_prints_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            artifact = root / "app-release.apk"
            artifact.write_bytes(b"signed")
            stdout = StringIO()

            with redirect_stdout(stdout):
                exit_code = signed_artifact_evidence.main(
                    ["android", str(artifact), "--version", "1.0.0+42"],
                )

            self.assertEqual(0, exit_code)
            self.assertIn("Artifact path:", stdout.getvalue())
            self.assertIn("Version/build number: 1.0.0+42", stdout.getvalue())

    def test_cli_reports_errors_to_stderr(self) -> None:
        stderr = StringIO()
        with tempfile.TemporaryDirectory() as tmp:
            with redirect_stderr(stderr):
                exit_code = signed_artifact_evidence.main(
                    ["android", str(Path(tmp) / "missing-release.apk")],
                )

        self.assertEqual(1, exit_code)
        self.assertIn("Signed artifact evidence generation failed", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
