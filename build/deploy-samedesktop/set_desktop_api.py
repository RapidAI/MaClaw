# -*- coding: utf-8 -*-
"""Install the shared desktop API token on hub.mypapers.top.

Prints lengths and HTTP status codes only. Never prints the token.
Python 3.6. Does not restart desktopd, docker, or hubcenter.
"""
from __future__ import print_function

import os
import stat
import subprocess
import sys
import time

HUB = "/data/soft/hub"
SRV_ENV = "/data/soft/maclaw_srv/.env"
TOKEN_FILE = os.path.join(HUB, "desktop-api.env")
START = os.path.join(HUB, "start.sh")
HUB_URL = "http://127.0.0.1:9399"
TOKEN_KEY = "MACLAW_DESKTOP_API_TOKEN"
URL_KEY = "MACLAW_HUB_URL"
SOURCE_MARK = '. "$APP_DIR/desktop-api.env"'
ANCHOR = 'APP_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)\n'
BLOCK = (
    ANCHOR
    + "\n"
    + "# Desktop API token shared with MaClawSrv. Mode 600. A binary deploy does not rewrite this file.\n"
    + 'if [ -f "$APP_DIR/desktop-api.env" ]; then\n'
    + "  set -a\n"
    + '  . "$APP_DIR/desktop-api.env"\n'
    + "  set +a\n"
    + "fi\n"
)


def say(msg):
    sys.stdout.write(msg + "\n")
    sys.stdout.flush()


def read_bytes(path):
    with open(path, "rb") as fh:
        return fh.read()


def mode_of(path):
    return stat.S_IMODE(os.stat(path).st_mode)


def write_new(path, data, mode):
    tmp = path + ".tmp-desktop-api"
    if os.path.exists(tmp):
        os.unlink(tmp)
    old = os.umask(0o077)
    try:
        fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL, mode)
    finally:
        os.umask(old)
    try:
        os.write(fd, data)
        os.fsync(fd)
    finally:
        os.close(fd)
    os.chmod(tmp, mode)
    os.rename(tmp, path)


def write_existing(path, data, mode):
    fd = os.open(path, os.O_WRONLY | os.O_TRUNC)
    try:
        os.write(fd, data)
        os.fsync(fd)
    finally:
        os.close(fd)
    os.chmod(path, mode)


def parse_env(text):
    values = {}
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in stripped:
            continue
        if stripped.startswith("export "):
            stripped = stripped[len("export "):].strip()
        key, value = stripped.split("=", 1)
        key = key.strip()
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
            value = value[1:-1]
        values[key] = value
    return values


def redact(blob, token):
    if not blob:
        return ""
    if isinstance(blob, bytes):
        text = blob.decode("utf-8", "replace")
    else:
        text = blob
    lines = []
    for line in text.splitlines():
        if (token and token in line) or TOKEN_KEY in line:
            lines.append("REDACTED_LINE")
        else:
            lines.append(line)
    return "\n".join(lines)


def load_token():
    if os.path.isfile(TOKEN_FILE):
        values = parse_env(read_bytes(TOKEN_FILE).decode("utf-8", "replace"))
        token = values.get(TOKEN_KEY, "")
        if not token:
            raise SystemExit("TOKEN_FILE_EMPTY")
        say("TOKEN_FILE existed len=%d mode=%o" % (len(token), mode_of(TOKEN_FILE)))
        return token
    try:
        raw = subprocess.check_output(["openssl", "rand", "-hex", "32"])
    except OSError:
        raise SystemExit("OPENSSL_MISSING")
    token = raw.decode("ascii", "replace").strip()
    if len(token) != 64 or any(ch not in "0123456789abcdef" for ch in token):
        raise SystemExit("TOKEN_UNEXPECTED")
    write_new(TOKEN_FILE, (TOKEN_KEY + "=" + token + "\n").encode("ascii"), 0o600)
    say("TOKEN_FILE created len=%d mode=%o" % (len(token), mode_of(TOKEN_FILE)))
    return token


def ensure_srv_env(token):
    if not os.path.isfile(SRV_ENV):
        raise SystemExit("SRV_ENV_MISSING")
    original = read_bytes(SRV_ENV)
    mode = mode_of(SRV_ENV)
    text = original.decode("utf-8", "replace")
    values = parse_env(text)
    url = values.get(URL_KEY, "")
    existing = values.get(TOKEN_KEY, "")
    if url and url != HUB_URL:
        say("HUB_URL_DIFFERS len=%d" % len(url))
        raise SystemExit(2)
    if existing and existing != token:
        say("SRV_TOKEN_DIFFERS len=%d" % len(existing))
        raise SystemExit(2)
    extra = ""
    if not url:
        extra += URL_KEY + "=" + HUB_URL + "\n"
    if not existing:
        extra += TOKEN_KEY + "=" + token + "\n"
    if extra:
        bak = SRV_ENV + ".bak-desktop-api-20261010"
        if not os.path.exists(bak):
            write_new(bak, original, mode)
            say("SRV_ENV_BACKUP mode=%o" % mode_of(bak))
        if text and not text.endswith("\n"):
            text += "\n"
        if "# Cloud desktop API." not in text:
            text += "# Cloud desktop API. Same token as /data/soft/hub/desktop-api.env.\n"
        text += extra
        write_existing(SRV_ENV, text.encode("utf-8"), mode)
        say("SRV_ENV appended")
    else:
        say("SRV_ENV already complete")
    now = parse_env(read_bytes(SRV_ENV).decode("utf-8", "replace"))
    say("SRV_URL_LEN %d" % len(now.get(URL_KEY, "")))
    say("SRV_TOKEN_LEN %d" % len(now.get(TOKEN_KEY, "")))
    say("SRV_MATCH %s" % ("yes" if now.get(TOKEN_KEY) == token and now.get(URL_KEY) == HUB_URL else "no"))
    say("SRV_ENV_MODE %o" % mode_of(SRV_ENV))
    if now.get(TOKEN_KEY) != token or now.get(URL_KEY) != HUB_URL:
        raise SystemExit(2)


def ensure_start(token):
    original = read_bytes(START)
    text = original.decode("utf-8", "replace")
    if token in text:
        raise SystemExit("TOKEN_LEAKED_INTO_START")
    if SOURCE_MARK in text:
        say("START already sources token file")
        return
    if ANCHOR not in text:
        raise SystemExit("START_ANCHOR_MISSING")
    bak = START + ".bak-desktop-api-20261010"
    mode = mode_of(START)
    if not os.path.exists(bak):
        write_new(bak, original, mode)
        say("START_BACKUP mode=%o" % mode_of(bak))
    updated = text.replace(ANCHOR, BLOCK, 1)
    if token in updated:
        raise SystemExit("TOKEN_LEAKED_INTO_START")
    write_existing(START, updated.encode("utf-8"), mode)
    syntax = subprocess.check_output(["/bin/sh", "-n", START], stderr=subprocess.STDOUT)
    say("START patched %s" % redact(syntax, token).replace("\n", " "))
    say("START_MODE %o" % mode_of(START))


def curl_code(args):
    proc = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    out, _err = proc.communicate()
    text = out.decode("ascii", "replace").strip()
    return text.splitlines()[-1] if text else "empty"


def wait_version():
    last = ""
    for _ in range(10):
        proc = subprocess.Popen(
            ["curl", "-fsS", "--max-time", "3", "http://127.0.0.1:18080/version"],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        out, _err = proc.communicate()
        last = out.decode("utf-8", "replace").strip()
        if proc.returncode == 0 and last:
            say("VERSION %s" % last[:400])
            return
        time.sleep(1)
    say("VERSION_FAIL")
    raise SystemExit(4)


def auth_probe(token):
    import urllib.error
    import urllib.request

    req = urllib.request.Request(
        "http://127.0.0.1:9399/api/v1/desktop-services/session",
        data=b"not-json",
        headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"},
    )
    try:
        resp = urllib.request.urlopen(req, timeout=8)
        code = resp.getcode()
    except urllib.error.HTTPError as exc:
        code = exc.code
    except Exception as exc:
        say("AUTH_PROBE_ERR %s" % type(exc).__name__)
        return
    say("AUTH_PROBE %s" % code)


def proc_env(pid):
    raw = read_bytes("/proc/%d/environ" % pid)
    values = {}
    for item in raw.split(b"\0"):
        if b"=" not in item:
            continue
        key, value = item.split(b"=", 1)
        values[key.decode("utf-8", "replace")] = value.decode("utf-8", "replace")
    return values


def show_proc(pid, keys):
    try:
        values = proc_env(pid)
    except Exception:
        say("ENV_READ_FAIL %d" % pid)
        return {}
    for key in keys:
        say("PID %d %s len=%d" % (pid, key, len(values.get(key, ""))))
    return values


def main():
    token = load_token()
    ensure_srv_env(token)
    ensure_start(token)
    say("RESTART maclawsrv")
    subprocess.check_call(["systemctl", "restart", "maclawsrv.service"])
    say("RESTART hub")
    try:
        started = subprocess.check_output(["/bin/sh", START], stderr=subprocess.STDOUT)
    except subprocess.CalledProcessError as exc:
        say("HUB_START_FAIL %d" % exc.returncode)
        say(redact(exc.output, token))
        raise SystemExit(3)
    say(redact(started, token))
    time.sleep(2)
    wait_version()
    say("BOOTSTRAP %s" % curl_code([
        "curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}",
        "--max-time", "5", "http://127.0.0.1:9399/api/mobile/bootstrap",
    ]))
    auth_probe(token)
    srv = subprocess.check_output([
        "systemctl", "show", "maclawsrv.service", "-p", "MainPID,ActiveState", "--no-page",
    ]).decode("utf-8", "replace").strip()
    desk = subprocess.check_output([
        "systemctl", "show", "maclaw-desktopd.service", "-p", "MainPID,ActiveState", "--no-page",
    ]).decode("utf-8", "replace").strip()
    say(srv.replace("\n", " "))
    say(desk.replace("\n", " "))
    hub_pid = 0
    try:
        hub_pid = int(read_bytes(os.path.join(HUB, "data", "maclaw-hub.pid")).decode().strip())
    except Exception:
        say("HUB_PID_READ_FAIL")
    say("HUB_PID %d" % hub_pid)
    ps = subprocess.check_output(["ps", "-eo", "pid,args"]).decode("utf-8", "replace")
    for line in ps.splitlines():
        if "maclaw-hubcenter" in line and "grep" not in line:
            say("HUBCENTER " + " ".join(line.split()[:2]))
    srv_pid = 0
    for part in srv.split():
        if part.startswith("MainPID="):
            srv_pid = int(part.split("=", 1)[1])
    srv_env = show_proc(srv_pid, [URL_KEY, TOKEN_KEY]) if srv_pid else {}
    hub_env = show_proc(hub_pid, [TOKEN_KEY]) if hub_pid else {}
    same = (
        srv_env.get(TOKEN_KEY) == token
        and hub_env.get(TOKEN_KEY) == token
        and srv_env.get(URL_KEY) == HUB_URL
    )
    say("PROC_MATCH %s" % ("yes" if same else "no"))
    dock = subprocess.check_output([
        "docker", "ps", "-a", "--filter", "id=ba5d94bd0611",
        "--format", "{{.ID}} {{.Status}} {{.Names}}",
    ]).decode("utf-8", "replace").strip()
    say("CONTAINER %s" % dock)
    log_path = "/data/soft/maclaw_srv/data/logs/maclaw_srv.log"
    try:
        tail = read_bytes(log_path).splitlines()[-40:]
    except Exception:
        tail = []
    heard = ""
    for raw in tail:
        line = raw.decode("utf-8", "replace")
        if "listening" in line and TOKEN_KEY not in line and token not in line:
            heard = line.strip()
    say("LISTENING %s" % (heard[-180:] if heard else "absent"))
    if not same:
        raise SystemExit(5)
    say("DONE")


if __name__ == "__main__":
    main()
