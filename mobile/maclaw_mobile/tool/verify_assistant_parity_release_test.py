from __future__ import annotations

import json
import sys
import tempfile
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from threading import Thread

sys.path.insert(0, str(Path(__file__).resolve().parent))

import verify_assistant_parity_release as mod


class VerifyAssistantParityReleaseTest(unittest.TestCase):
    def test_inspect_and_expect_hash(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "a.bin"
            path.write_bytes(b"hello-parity")
            info = mod.inspect_artifact(path)
            self.assertEqual(info.size, len(b"hello-parity"))
            self.assertEqual(len(info.sha256), 64)
            ok = mod.check_expect_hash("x", info, info.sha256)
            self.assertTrue(ok.ok)
            bad = mod.check_expect_hash("x", info, "0" * 64)
            self.assertFalse(bad.ok)

    def test_offline_cli_ok(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            mobile = root / "mobile" / "maclaw_mobile"
            apk = mobile / "build" / "app" / "outputs" / "flutter-apk" / "app-release.apk"
            hub = root / "hub" / "dist" / "maclaw-hub.exe"
            apk.parent.mkdir(parents=True)
            hub.parent.mkdir(parents=True)
            apk.write_bytes(b"apk")
            hub.write_bytes(b"hub")
            code = mod.run(
                [
                    "--repo-root",
                    str(root),
                    "--apk",
                    "build/app/outputs/flutter-apk/app-release.apk",
                    "--hub-exe",
                    "hub/dist/maclaw-hub.exe",
                ]
            )
            self.assertEqual(code, 0)

    def test_offline_cli_missing_apk(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            hub = root / "hub" / "dist" / "maclaw-hub.exe"
            hub.parent.mkdir(parents=True)
            hub.write_bytes(b"hub")
            code = mod.run(
                [
                    "--repo-root",
                    str(root),
                    "--hub-exe",
                    "hub/dist/maclaw-hub.exe",
                ]
            )
            self.assertEqual(code, 1)

    def test_live_healthz_probe(self) -> None:
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self) -> None:  # noqa: N802
                if self.path == "/healthz":
                    body = json.dumps({"ok": True, "service": "maclaw-hub"}).encode()
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)
                    return
                self.send_error(404)

            def log_message(self, format: str, *args) -> None:  # noqa: A003
                return

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            port = server.server_address[1]
            result = mod.probe_healthz(f"http://127.0.0.1:{port}", timeout=2.0)
            self.assertTrue(result.ok, result.detail)
        finally:
            server.shutdown()


if __name__ == "__main__":
    unittest.main()
