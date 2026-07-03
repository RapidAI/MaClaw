from __future__ import annotations

import tempfile
import sys
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
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

    def test_main_can_write_success_log(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "lib").mkdir()
            (root / "lib" / "main.dart").write_text(
                "void main() {}\n",
                encoding="utf-8",
            )
            log_path = root / "logs" / "runtime-boundary.log"
            stdout = StringIO()

            with redirect_stdout(stdout):
                exit_code = verify_runtime_boundary.main(
                    ["--root", str(root), "--log", str(log_path)],
                )

            self.assertEqual(0, exit_code)
            self.assertIn("runtime boundary verified", stdout.getvalue())
            self.assertIn(
                "MaClaw Mobile runtime boundary verified.",
                log_path.read_text(encoding="utf-8"),
            )

    def test_main_refuses_to_overwrite_success_log_without_force(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "lib").mkdir()
            (root / "lib" / "main.dart").write_text(
                "void main() {}\n",
                encoding="utf-8",
            )
            log_path = root / "runtime-boundary.log"
            log_path.write_text("existing runtime evidence", encoding="utf-8")
            stderr = StringIO()

            with redirect_stderr(stderr):
                exit_code = verify_runtime_boundary.main(
                    ["--root", str(root), "--log", str(log_path)],
                )

            self.assertEqual(1, exit_code)
            self.assertEqual("existing runtime evidence", log_path.read_text(encoding="utf-8"))
            self.assertIn("pass --force to overwrite", stderr.getvalue())

            with redirect_stdout(StringIO()):
                exit_code = verify_runtime_boundary.main(
                    ["--root", str(root), "--log", str(log_path), "--force"],
                )

            self.assertEqual(0, exit_code)
            self.assertIn("runtime boundary verified", log_path.read_text(encoding="utf-8"))

    def test_main_can_write_violation_log(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "lib").mkdir()
            (root / "lib" / "bridge.dart").write_text(
                "import 'dart:ffi';\n",
                encoding="utf-8",
            )
            log_path = root / "logs" / "runtime-boundary.log"
            stderr = StringIO()

            with redirect_stderr(stderr):
                exit_code = verify_runtime_boundary.main(
                    ["--root", str(root), "--log", str(log_path)],
                )

            self.assertEqual(1, exit_code)
            self.assertIn("violations found", stderr.getvalue())
            text = log_path.read_text(encoding="utf-8")
            self.assertIn("violations found", text)
            self.assertIn("dart ffi", text)


if __name__ == "__main__":
    unittest.main()
