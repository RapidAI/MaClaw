from __future__ import annotations

import hashlib
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import verify_debug_apk_evidence


def evidence_text(path: str, content: bytes) -> str:
    return (
        "# Evidence\n\n"
        "- `flutter build apk --debug`\n"
        "  - Passed.\n"
        f"  - Artifact: `{path}`.\n"
        f"  - Size: `{len(content)}` bytes.\n"
        f"  - SHA256: `{hashlib.sha256(content).hexdigest().upper()}`.\n"
    )


class VerifyDebugApkEvidenceTest(unittest.TestCase):
    def test_verifies_matching_debug_apk_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            apk = root / verify_debug_apk_evidence.DEFAULT_APK_PATH
            apk.parent.mkdir(parents=True)
            content = b"debug apk bytes"
            apk.write_bytes(content)
            evidence = root / "docs/release_evidence.md"
            evidence.parent.mkdir()
            evidence.write_text(
                evidence_text(str(verify_debug_apk_evidence.DEFAULT_APK_PATH), content),
                encoding="utf-8",
            )

            self.assertEqual(
                [],
                verify_debug_apk_evidence.verify_debug_apk_evidence(root, evidence),
            )

    def test_reports_size_mismatch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            apk = root / verify_debug_apk_evidence.DEFAULT_APK_PATH
            apk.parent.mkdir(parents=True)
            apk.write_bytes(b"actual")
            evidence = root / "release_evidence.md"
            evidence.write_text(
                evidence_text(str(verify_debug_apk_evidence.DEFAULT_APK_PATH), b"other"),
                encoding="utf-8",
            )

            errors = verify_debug_apk_evidence.verify_debug_apk_evidence(root, evidence)

            self.assertTrue(any("size mismatch" in error for error in errors))

    def test_reports_sha256_mismatch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            apk = root / verify_debug_apk_evidence.DEFAULT_APK_PATH
            apk.parent.mkdir(parents=True)
            apk.write_bytes(b"same-length-a")
            evidence = root / "release_evidence.md"
            evidence.write_text(
                evidence_text(
                    str(verify_debug_apk_evidence.DEFAULT_APK_PATH),
                    b"same-length-b",
                ),
                encoding="utf-8",
            )

            errors = verify_debug_apk_evidence.verify_debug_apk_evidence(root, evidence)

            self.assertTrue(any("SHA256 mismatch" in error for error in errors))

    def test_reports_missing_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            evidence = root / "release_evidence.md"
            evidence.write_text(
                evidence_text(str(verify_debug_apk_evidence.DEFAULT_APK_PATH), b"apk"),
                encoding="utf-8",
            )

            errors = verify_debug_apk_evidence.verify_debug_apk_evidence(root, evidence)

            self.assertTrue(any("does not exist" in error for error in errors))

    def test_uses_debug_apk_section_with_artifact_fields(self) -> None:
        content = b"debug apk bytes"
        evidence = (
            "```bash\n"
            "flutter build apk --debug\n"
            "```\n\n"
            "- `flutter build apk --debug`\n"
            "  - Passed.\n\n"
            + evidence_text("build/app/outputs/flutter-apk/app-debug.apk", content)
        )

        parsed = verify_debug_apk_evidence.parse_debug_apk_evidence(evidence)

        self.assertEqual(
            Path("build/app/outputs/flutter-apk/app-debug.apk"),
            parsed.artifact,
        )
        self.assertEqual(len(content), parsed.size)

    def test_rejects_missing_evidence_fields(self) -> None:
        with self.assertRaisesRegex(ValueError, "Missing debug APK evidence field"):
            verify_debug_apk_evidence.parse_debug_apk_evidence(
                "- `flutter build apk --debug`\n  - Passed.\n"
            )


if __name__ == "__main__":
    unittest.main()
