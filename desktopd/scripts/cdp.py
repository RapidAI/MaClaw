#!/usr/bin/env python3
"""Minimal Chrome DevTools client for a desktopd desktop (stdlib only, Python 3.6+).

desktopd publishes each desktop's CDP through a gate that wants
"Authorization: Bearer <gate token>". The token and port come from the
session response (cdp_url = http://desktop:<token>@<host>:<port>).

  cdp.py --session s.json version
  cdp.py --session s.json eval 'document.title'
  cdp.py --session s.json navigate 'data:text/html,<textarea id=t autofocus></textarea>'
  CDP_TOKEN=... cdp.py --host 127.0.0.1 --port 49157 eval '1+1'

--session reads the JSON written by `dapi.sh POST /v1/desktops/session ...`.
--host overrides the host in cdp_url (on the Docker host use 127.0.0.1:
Chromium only answers /json for an IP or localhost Host header). The token is
never printed.
"""
import argparse
import base64
import json
import os
import re
import socket
import sys
import urllib.request


def parse_args():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--session", help="session JSON from /v1/desktops/session")
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int)
    ap.add_argument("--timeout", type=float, default=20)
    ap.add_argument("command", choices=["version", "list", "eval", "navigate"])
    ap.add_argument("arg", nargs="?", default="")
    return ap.parse_args()


def gate(args):
    token = os.environ.get("CDP_TOKEN", "").strip()
    port = args.port
    if args.session:
        with open(args.session, encoding="utf-8") as fh:
            url = json.load(fh)["cdp_url"]
        m = re.match(r"^https?://desktop:([0-9a-f]+)@[^:/]+:(\d+)", url)
        if not m:
            sys.exit("cdp_url has no gate token")
        token = token or m.group(1)
        port = port or int(m.group(2))
    if not token or not port:
        sys.exit("need --session, or CDP_TOKEN and --port")
    return token, port


def http_json(host, port, token, path, timeout):
    req = urllib.request.Request("http://%s:%d%s" % (host, port, path), headers={"Authorization": "Bearer " + token})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.load(resp)


class WebSocket:
    def __init__(self, host, port, path, token, timeout):
        self.sock = socket.create_connection((host, port), timeout)
        key = base64.b64encode(os.urandom(16)).decode()
        self.sock.sendall((
            "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
            "Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nAuthorization: Bearer %s\r\n\r\n"
            % (path, host, key, token)
        ).encode())
        buf = b""
        while b"\r\n\r\n" not in buf:
            chunk = self.sock.recv(4096)
            if not chunk:
                sys.exit("websocket handshake failed")
            buf += chunk
        head, self.buf = buf.split(b"\r\n\r\n", 1)
        if b" 101 " not in head.split(b"\r\n")[0]:
            sys.exit("websocket handshake refused: %s" % head.split(b"\r\n")[0].decode("latin-1"))

    def send(self, obj):
        data = json.dumps(obj).encode()
        mask = os.urandom(4)
        n = len(data)
        if n < 126:
            head = bytes([0x81, 0x80 | n])
        elif n < 65536:
            head = bytes([0x81, 0x80 | 126]) + n.to_bytes(2, "big")
        else:
            head = bytes([0x81, 0x80 | 127]) + n.to_bytes(8, "big")
        self.sock.sendall(head + mask + bytes(b ^ mask[i % 4] for i, b in enumerate(data)))

    def _need(self, n):
        while len(self.buf) < n:
            chunk = self.sock.recv(65536)
            if not chunk:
                sys.exit("websocket closed")
            self.buf += chunk

    def recv(self):
        self._need(2)
        n = self.buf[1] & 0x7F
        off = 2
        if n == 126:
            self._need(4)
            n, off = int.from_bytes(self.buf[2:4], "big"), 4
        elif n == 127:
            self._need(10)
            n, off = int.from_bytes(self.buf[2:10], "big"), 10
        self._need(off + n)
        data, self.buf = self.buf[off:off + n], self.buf[off + n:]
        return json.loads(data.decode("utf-8"))

    def call(self, method, params):
        self.send({"id": 1, "method": method, "params": params})
        while True:
            msg = self.recv()
            if msg.get("id") == 1:
                return msg


def main():
    args = parse_args()
    token, port = gate(args)
    if args.command == "version":
        info = http_json(args.host, port, token, "/json/version", args.timeout)
        print(json.dumps({"Browser": info.get("Browser"), "webSocket": bool(info.get("webSocketDebuggerUrl"))}))
        return 0
    targets = http_json(args.host, port, token, "/json/list", args.timeout)
    if args.command == "list":
        print(json.dumps([{"type": t.get("type"), "url": t.get("url", "")[:120]} for t in targets], ensure_ascii=False))
        return 0
    pages = [t for t in targets if t.get("type") == "page"]
    if not pages:
        sys.exit("no page target")
    path = "/" + pages[0]["webSocketDebuggerUrl"].split("/", 3)[3]
    ws = WebSocket(args.host, port, path, token, args.timeout)
    if args.command == "navigate":
        msg = ws.call("Page.navigate", {"url": args.arg})
        print(json.dumps(msg.get("result", msg.get("error")), ensure_ascii=False))
    else:
        msg = ws.call("Runtime.evaluate", {"expression": args.arg, "awaitPromise": True, "returnByValue": True})
        result = msg.get("result", {})
        if "exceptionDetails" in result or "error" in msg:
            print(json.dumps(result.get("exceptionDetails") or msg.get("error"), ensure_ascii=False))
            return 1
        print(json.dumps(result.get("result", {}).get("value"), ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
