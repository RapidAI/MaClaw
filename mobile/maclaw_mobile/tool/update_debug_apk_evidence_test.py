from __future__ import annotations

import hashlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import update_debug_apk_evidence
import verify_debug_apk_evidence


class UpdateDebugApkEvidenceTest(unittest.TestCase):
    def make_root_with_apk(self, content: bytes = b"debug apk bytes") -> tuple[tempfile.TemporaryDirectory[str], Path, Path]:
        tmp = tempfile.TemporaryDirectory()
        root = Path(tmp.name)
        apk = root / verify_debug_apk_evidence.DEFAULT_APK_PATH
        apk.parent.mkdir(parents=True)
        apk.write_bytes(content)
        return tmp, root, apk

    def stale_evidence(self) -> str:
        return (
            "# Evidence\n\n"
            "- `flutter build apk --debug`\n"
            "  - Passed.\n"
            "  - Artifact: `old.apk`.\n"
            "  - Size: `1` bytes.\n"
            "  - SHA256: `0000000000000000000000000000000000000000000000000000000000000000`.\n"
            "  - CI artifact name: `maclaw-mobile-debug-apk`.\n"
        )

    def test_updates_debug_apk_artifact_size_and_sha256(self) -> None:
        tmp, root, _ = self.make_root_with_apk()
        self.addCleanup(tmp.cleanup)

        output = update_debug_apk_evidence.update_debug_apk_evidence_text(
            self.stale_evidence(),
            root,
            verify_debug_apk_evidence.DEFAULT_APK_PATH,
        )

        expected_sha = hashlib.sha256(b"debug apk bytes").hexdigest().upper()
        self.assertIn("Artifact: `build/app/outputs/flutter-apk/app-debug.apk`", output)
        self.assertIn("Size: `15` bytes", output)
        self.assertIn(f"SHA256: `{expected_sha}`", output)
        self.assertIn("CI artifact name", output)

    def test_updates_first_section_with_complete_fields(self) -> None:
        tmp, root, _ = self.make_root_with_apk(b"new bytes")
        self.addCleanup(tmp.cleanup)
        text = (
            "- `flutter build apk --debug`\n"
            "  - Passed.\n\n"
            + self.stale_evidence()
        )

        output = update_debug_apk_evidence.update_debug_apk_evidence_text(
            text,
            root,
            verify_debug_apk_evidence.DEFAULT_APK_PATH,
        )

        self.assertIn("Size: `9` bytes", output)

    def test_rejects_missing_artifact_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with self.assertRaises(FileNotFoundError):
                update_debug_apk_evidence.debug_apk_evidence_lines(
                    root,
                    verify_debug_apk_evidence.DEFAULT_APK_PATH,
                )

    def test_rejects_evidence_without_complete_section(self) -> None:
        tmp, root, _ = self.make_root_with_apk()
        self.addCleanup(tmp.cleanup)

        with self.assertRaisesRegex(ValueError, "Missing debug APK evidence section"):
            update_debug_apk_evidence.update_debug_apk_evidence_text(
                "- `flutter build apk --debug`\n  - Passed.\n",
                root,
                verify_debug_apk_evidence.DEFAULT_APK_PATH,
            )

    def test_main_updates_evidence_file(self) -> None:
        tmp, root, _ = self.make_root_with_apk(b"cli bytes")
        self.addCleanup(tmp.cleanup)
        evidence = root / "docs" / "release_evidence.md"
        evidence.parent.mkdir()
        evidence.write_text(self.stale_evidence(), encoding="utf-8")
        stdout = StringIO()

        with redirect_stdout(stdout):
            exit_code = update_debug_apk_evidence.main(["--root", str(root)])

        self.assertEqual(0, exit_code)
        self.assertIn("Updated debug APK release evidence", stdout.getvalue())
        self.assertEqual([], verify_debug_apk_evidence.verify_debug_apk_evidence(root, evidence))

    def test_main_reports_missing_artifact_to_stderr(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            evidence = root / "docs" / "release_evidence.md"
            evidence.parent.mkdir()
            evidence.write_text(self.stale_evidence(), encoding="utf-8")
            stderr = StringIO()

            with redirect_stderr(stderr):
                exit_code = update_debug_apk_evidence.main(["--root", str(root)])

            self.assertEqual(1, exit_code)
            self.assertIn("Debug APK evidence update failed", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
