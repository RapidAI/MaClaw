from __future__ import annotations

import tempfile
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import verify_runtime_boundary


class VerifyRuntimeBoundaryTest(unittest.TestCase):
    def test_current_mobile_runtime_has_no_corelib_bridge(self) -> None:
        self.assertEqual([], verify_runtime_boundary.find_violations(
            verify_runtime_boundary.mobile_root()
        ))

    def test_flags_dart_ffi_corelib_bridge(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            lib = root / "lib"
            lib.mkdir()
            (lib / "bridge.dart").write_text(
                "import 'dart:ffi';\n"
                "final lib = DynamicLibrary.open('libcorelib.so');\n",
                encoding="utf-8",
            )

            violations = verify_runtime_boundary.find_violations(root)

        self.assertGreaterEqual(len(violations), 3)
        self.assertIn("dart ffi", {violation.rule.name for violation in violations})
        self.assertIn(
            "dynamic library", {violation.rule.name for violation in violations}
        )
        self.assertIn(
            "corelib reference", {violation.rule.name for violation in violations}
        )

    def test_ignores_docs_and_tests_mentions(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            docs = root / "docs"
            tests = root / "test"
            docs.mkdir()
            tests.mkdir()
            (docs / "release.md").write_text(
                "MaClaw Mobile does not embed Go corelib.\n",
                encoding="utf-8",
            )
            (tests / "boundary_test.dart").write_text(
                "expect(doc, contains('corelib'));\n",
                encoding="utf-8",
            )

            self.assertEqual([], verify_runtime_boundary.find_violations(root))


if __name__ == "__main__":
    unittest.main()
