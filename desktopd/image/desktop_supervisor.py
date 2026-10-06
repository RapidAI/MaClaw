#!/usr/bin/env python3
"""Start one virtual desktop per user key inside the GUI container.

Each key gets its own X display, Chromium profile, and CDP proxy. A later
ensure for the same key reuses that desktop. Other keys cannot see it.
"""
import base64
from contextlib import contextmanager
import fcntl
import hmac
import json
import os
import secrets
import shutil
import socket
import struct
import subprocess
import sys
import threading
import time
from pathlib import Path
from urllib.parse import quote, urlsplit

ROOT = Path("/desktops")
MAX_RUNNING = 3
DISPLAY_MIN = 20
DISPLAY_MAX = 50
# The VNC gate fronts websockify on the published noVNC port; websockify
# itself only listens on loopback so the raw screen cannot be reached.
VNC_GATE_PORT = 6080
VNC_WEBSOCKIFY_PORT = 6081


def main(argv):
    if len(argv) == 5 and argv[1] == "gate":
        return serve_gate(int(argv[2]), int(argv[3]), argv[4])
    if len(argv) == 4 and argv[1] == "watch":
        return watch_vnc(argv[2], int(argv[3]))
    if len(argv) == 3 and argv[1] == "flush":
        key = argv[2]
        if not valid_key(key):
            print("invalid user key", file=sys.stderr)
            return 2
        return flush_browser(key)
    if len(argv) == 3 and argv[1] == "idle":
        key = argv[2]
        if not valid_key(key):
            print("invalid user key", file=sys.stderr)
            return 2
        return print_idle(key)
    if len(argv) == 3 and argv[1] == "stop":
        key = argv[2]
        if not valid_key(key):
            print("invalid user key", file=sys.stderr)
            return 2
        stop_desktop(key)
        return 0
    if len(argv) != 3 or argv[1] != "ensure":
        print("usage: desktop_supervisor.py ensure <user-key>", file=sys.stderr)
        return 2
    key = argv[2]
    if not valid_key(key):
        print("invalid user key", file=sys.stderr)
        return 2
    owner = os.environ.get("MACLAW_DESKTOP_KEY", "").strip()
    if owner and key != owner:
        print("desktop data belongs to another user", file=sys.stderr)
        return 2
    with desktop_lock():
        result = ensure(key)
    print(json.dumps(result))
    return 0


_desktop_lock_held = False


@contextmanager
def desktop_lock():
    """One user's desktop is started or stopped, never both at once.

    The stop writes the website login. A start in the middle of that write
    opens a second browser and the login stays in the one that is exiting.
    """
    global _desktop_lock_held
    if _desktop_lock_held:
        yield
        return
    ROOT.mkdir(mode=0o700, exist_ok=True)
    lock_file = open(ROOT / "lock", "a", encoding="utf-8")
    fcntl.flock(lock_file, fcntl.LOCK_EX)
    _desktop_lock_held = True
    try:
        yield
    finally:
        _desktop_lock_held = False
        fcntl.flock(lock_file, fcntl.LOCK_UN)
        lock_file.close()


def valid_key(key):
    return bool(key) and len(key) <= 64 and all(ch in "0123456789abcdef" for ch in key)


def ensure(key):
    registry = load_registry()
    users = registry.setdefault("users", {})
    entry = users.get(key)
    if entry is None:
        entry = {"display": allocate_display(users), "last_used": 0}
        users[key] = entry
    # This container publishes only display :20. Another display, or another
    # key left in an old registry, would put CDP on a port that is not published.
    owner = os.environ.get("MACLAW_DESKTOP_KEY", "").strip()
    if owner == key:
        for other in list(users):
            if other != key:
                stop_desktop(other)
                del users[other]
        if int(entry.get("display") or 0) != DISPLAY_MIN:
            stop_desktop(key)
            entry["display"] = DISPLAY_MIN
    display = int(entry["display"])
    entry["last_used"] = int(time.time())
    token = entry.get("token")
    if not token or not valid_key(token):
        token = secrets.token_hex(16)
        entry["token"] = token
    save_registry(registry)
    if not desktop_running(key):
        start_desktop(key, display)
    else:
        ensure_vnc(key, display)
        ensure_proxy(key, display)
    ensure_watch(key, display)
    reap(users, key)
    save_registry(registry)
    return {"display": ":%d" % display, "cdp_port": proxy_port(display), "token": token}


def allocate_display(users):
    used = {int(item.get("display", 0)) for item in users.values()}
    for display in range(DISPLAY_MIN, DISPLAY_MAX + 1):
        if display not in used:
            return display
    raise SystemExit("no free desktop display")


def browser_pid(profile):
    """The browser process that has this profile open.

    Chromium's launcher pid can exit while the real browser keeps the login.
    Treating that as stopped would start a second browser and a new profile.
    """
    needle = b"--user-data-dir=" + str(profile).encode()
    proc = Path("/proc")
    if not proc.is_dir():
        return 0
    for entry in proc.iterdir():
        if not entry.name.isdigit():
            continue
        try:
            cmd = (entry / "cmdline").read_bytes()
        except OSError:
            continue
        if needle not in cmd or b"--type=" in cmd:
            continue
        if not pid_alive(entry.name):
            continue
        return int(entry.name)
    return 0


def desktop_running(key):
    """True only while this profile's browser is still the one that is running.

    A recorded pid can be reused by an unrelated process after Chromium exits.
    Treating that as the desktop would skip starting the browser, so the
    website login stays in the profile and the agent never reaches it.
    """
    profile = ROOT / key / "profile"
    if browser_pid(profile):
        return True
    pid = read_pids(key).get("chromium")
    if not pid_alive(pid):
        return False
    try:
        cmd = (Path("/proc") / str(int(pid)) / "cmdline").read_bytes()
    except (OSError, TypeError, ValueError):
        return True
    needle = b"--user-data-dir=" + str(profile).encode()
    return needle in cmd and b"--type=" not in cmd


def start_desktop(key, display):
    home = ROOT / key
    home.mkdir(mode=0o700, exist_ok=True)
    profile = home / "profile"
    profile.mkdir(mode=0o700, exist_ok=True)
    # The previous browser is already gone. A leftover lock would make this
    # start open a new empty profile and drop the website login. A leftover
    # flush flag would make the next stop skip writing this new login.
    clear_flush_flag(key)
    release_profile_lock(profile)
    keep_website_login(profile)
    user_home = Path("/home/desktop")
    user_home.mkdir(mode=0o700, exist_ok=True)
    for path in (
        user_home / ".config",
        user_home / ".local" / "share",
        user_home / ".local" / "bin",
        user_home / ".cache",
    ):
        path.mkdir(parents=True, mode=0o700, exist_ok=True)
    log_path = home / "desktop.log"
    log = open(log_path, "ab")
    env = os.environ.copy()
    env["HOME"] = str(user_home)
    env["XDG_CONFIG_HOME"] = str(user_home / ".config")
    env["XDG_DATA_HOME"] = str(user_home / ".local" / "share")
    env["XDG_CACHE_HOME"] = str(user_home / ".cache")
    env["PATH"] = str(user_home / ".local" / "bin") + os.pathsep + env.get("PATH", "")
    env["DISPLAY"] = ":%d" % display
    xvfb = spawn(["Xvfb", ":%d" % display, "-screen", "0", "1280x720x24", "-ac", "+extension", "GLX", "+render", "-noreset"], env, log)
    wait_for(lambda: Path("/tmp/.X11-unix/X%d" % display).exists(), "display :%d" % display)
    token = desktop_token_for(key)
    vnc = start_vnc(env, log, display, token) if token else None
    flux = spawn(["fluxbox"], env, log)
    chrome_port = 18000 + display
    # No start URL. A blank page would cover the restored tabs, and the
    # website login lives in this profile.
    spawn([
        browser_bin(),
        "--no-sandbox",
        "--disable-dev-shm-usage",
        "--disable-gpu",
        "--user-data-dir=%s" % profile,
        "--profile-directory=Default",
        "--password-store=basic",
        "--disable-sync",
        "--no-first-run",
        "--no-default-browser-check",
        "--disable-session-crashed-bubble",
        "--restore-last-session",
        "--disable-features=TrackingProtection3pcd,ThirdPartyStoragePartitioning",
        "--remote-debugging-port=%d" % chrome_port,
        "--remote-allow-origins=*",
        "--start-maximized",
    ], env, log)
    # The launcher pid can exit. Only the process that still has this profile
    # open is the browser with the website login.
    wait_for(lambda: browser_pid(profile) and port_open(chrome_port), "chromium profile %s" % profile)
    try:
        restore_page_login(key)
    except (OSError, ValueError, TypeError):
        pass
    proxy = None
    if token:
        proxy = spawn([sys.executable, __file__, "gate", str(proxy_port(display)), str(chrome_port), token], env, log)
    pids = {
        "xvfb": xvfb.pid,
        "fluxbox": flux.pid,
        "chromium": browser_pid(profile),
    }
    if proxy is not None:
        pids["proxy"] = proxy.pid
    if vnc is not None:
        x11, web, gate = vnc
        if x11 is not None:
            pids["x11vnc"] = x11.pid
        if web is not None:
            pids["vnc"] = web.pid
        if gate is not None:
            pids["vgate"] = gate.pid
    (home / "pids.json").write_text(json.dumps(pids), encoding="utf-8")


def release_profile_lock(profile):
    profile = Path(profile)
    for name in ("SingletonLock", "SingletonCookie", "SingletonSocket"):
        path = profile / name
        try:
            if path.is_symlink() or path.exists():
                path.unlink()
        except OSError:
            pass


def keep_website_login(profile):
    """Keep session cookies in this profile when the desktop stops.

    A clean exit otherwise deletes cookies that have no expiry. The next
    start would be the same browser files but a logged-out site. The previous
    exit state stays as Chromium wrote it, so a crashed window can still
    restore the logged-in tabs.
    """
    pref = Path(profile) / "Default" / "Preferences"
    try:
        pref.parent.mkdir(mode=0o700, exist_ok=True)
    except OSError:
        return
    data = {}
    try:
        current = json.loads(pref.read_text(encoding="utf-8"))
        if isinstance(current, dict):
            data = current
    except (OSError, json.JSONDecodeError):
        data = {}
    session = data.get("session")
    if not isinstance(session, dict):
        session = {}
    session["restore_on_startup"] = 1
    data["session"] = session
    # A login often finishes on another site and stores the session there.
    # Blocking those cookies, or keeping them only until exit, leaves this
    # same browser logged out when the agent continues.
    profile_prefs = data.get("profile")
    if not isinstance(profile_prefs, dict):
        profile_prefs = {}
    profile_prefs["block_third_party_cookies"] = False
    profile_prefs["cookie_controls_mode"] = 0
    defaults = profile_prefs.get("default_content_setting_values")
    if not isinstance(defaults, dict):
        defaults = {}
    defaults["cookies"] = 1
    profile_prefs["default_content_setting_values"] = defaults
    data["profile"] = profile_prefs
    tmp = pref.with_suffix(".tmp")
    try:
        tmp.write_text(json.dumps(data), encoding="utf-8")
        os.replace(tmp, pref)
    except OSError:
        pass


def flush_flag(key):
    return ROOT / key / "flushing"


def claim_flush(key):
    """True only for the first live caller. A second stop must not signal again."""
    path = flush_flag(key)
    path.parent.mkdir(mode=0o700, exist_ok=True)
    try:
        fd = os.open(str(path), os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    except FileExistsError:
        return False
    except OSError:
        return False
    try:
        os.write(fd, str(os.getpid()).encode("ascii"))
    except OSError:
        pass
    os.close(fd)
    return True


def flush_claim(key):
    """The process that claimed this stop, and whether it already signaled."""
    try:
        raw = flush_flag(key).read_text(encoding="ascii")
    except OSError:
        return 0, False
    pid = 0
    signaled = False
    for line in raw.splitlines():
        line = line.strip()
        if not line:
            continue
        if line == "signaled":
            signaled = True
            continue
        if pid == 0:
            try:
                pid = int(line)
            except ValueError:
                pid = 0
    return pid, signaled


def mark_flush_signaled(key):
    """Remember that this stop already asked the browser to write the login."""
    path = flush_flag(key)
    try:
        current = path.read_text(encoding="ascii")
    except OSError:
        current = ""
    if "signaled" in current.split():
        return
    try:
        path.write_text(current.rstrip() + "\nsignaled\n", encoding="ascii")
    except OSError:
        pass


def flush_owner_gone(key):
    """True when the flush that claimed this stop died before it signaled.

    A dead claim must not block the container stop. That stop would then
    kill the browser before the website login is written. A claim that
    already signaled stays, so the next stop waits instead of signaling again.
    """
    pid, signaled = flush_claim(key)
    if signaled:
        return False
    if pid <= 0:
        return True
    return not pid_alive(pid)


def clear_flush_flag(key):
    try:
        flush_flag(key).unlink()
    except OSError:
        pass


def chrome_debug_port(key):
    registry = load_registry()
    users = registry.get("users") or {}
    entry = users.get(key) if isinstance(users, dict) else None
    display = DISPLAY_MIN
    if isinstance(entry, dict):
        try:
            display = int(entry.get("display") or DISPLAY_MIN)
        except (TypeError, ValueError):
            display = DISPLAY_MIN
    if display < DISPLAY_MIN or display > DISPLAY_MAX:
        display = DISPLAY_MIN
    return 18000 + display


def session_cookie(cookie):
    """True when this cookie would disappear on a clean browser exit."""
    if not isinstance(cookie, dict) or not cookie.get("name"):
        return False
    if not (cookie.get("domain") or cookie.get("url")):
        return False
    if cookie.get("session"):
        return True
    try:
        expires = float(cookie.get("expires") or 0)
    except (TypeError, ValueError):
        return False
    return expires <= 0


def persistent_cookie(cookie, now):
    name = cookie.get("name") or ""
    host = str(cookie.get("domain") or "").lstrip(".")
    path = cookie.get("path") or "/"
    # __Host- cookies are rejected when a domain is set. They are the usual
    # form of a website login, so write them with a URL instead.
    if name.startswith("__Host-"):
        path = "/"
    secure = bool(cookie.get("secure")) or name.startswith("__Host-") or name.startswith("__Secure-") or cookie.get("sameSite") == "None"
    params = {
        "name": name,
        "value": cookie.get("value") if cookie.get("value") is not None else "",
        "path": path,
        "secure": secure,
        "httpOnly": bool(cookie.get("httpOnly")),
        # Chrome drops a cookie dated further out than 400 days.
        "expires": float(now) + 400 * 24 * 3600,
    }
    if name.startswith("__Host-") and host:
        params["url"] = cookie_page_url("https", host, "/")
    else:
        if cookie.get("url"):
            params["url"] = cookie.get("url")
        if cookie.get("domain"):
            params["domain"] = cookie.get("domain")
    same = cookie.get("sameSite")
    if same in ("Strict", "Lax", "None"):
        params["sameSite"] = same
    # A partitioned login cookie is a different cookie from the unpartitioned
    # one. Writing it without the partition leaves the real login as a
    # session cookie, and the clean exit deletes it.
    partition = cookie.get("partitionKey")
    if isinstance(partition, dict) and partition.get("topLevelSite"):
        params["partitionKey"] = partition
    elif isinstance(partition, str) and partition:
        params["partitionKey"] = partition
    return params


def cookie_page_url(scheme, host, path):
    """A page URL Chromium will accept for this cookie.

    The stored path is not always a valid URL. A space or a non-ASCII
    character makes the write fail, and the clean exit then deletes the login.
    """
    host = str(host or "").strip().lstrip(".")
    if not host or scheme not in ("http", "https"):
        return ""
    path = str(path or "/")
    if not path.startswith("/"):
        path = "/" + path
    return "%s://%s%s" % (scheme, host, quote(path, safe="/%:@"))


def save_session_cookie(conn, cookie, now):
    """Write one session cookie so a clean exit does not delete the login.

    Chromium rejects some login cookies when only a domain is set. A protocol
    error is the same rejection. If that write does not stick, write the same
    cookie through its page URL.
    """
    params = persistent_cookie(cookie, now)
    try:
        result = conn.call("Network.setCookie", params)
    except (OSError, ValueError, TypeError):
        result = None
    if isinstance(result, dict) and result.get("success"):
        return
    host = str(cookie.get("domain") or "").lstrip(".")
    if not host:
        return
    retry = dict(params)
    retry.pop("domain", None)
    scheme = "https" if retry.get("secure") else "http"
    page = cookie_page_url(scheme, host, retry.get("path") or "/")
    if not page:
        return
    retry["url"] = page
    try:
        conn.call("Network.setCookie", retry)
    except (OSError, ValueError, TypeError):
        return


def persist_website_login(key):
    """Keep the website login in this profile across a desktop stop.

    The person and the agent share this browser. Session cookies would be
    deleted when it exits, so the next start would be the same profile but
    logged out. Give those cookies an expiry before asking the browser to exit.
    """
    try:
        version = http_json("127.0.0.1", chrome_debug_port(key), "/json/version")
        ws = version.get("webSocketDebuggerUrl") or ""
        if not isinstance(ws, str) or not ws.startswith("ws://"):
            return
        with cdp_connection(ws) as conn:
            # Enabling the network domain streams events. Those events used
            # to crowd out the cookie reply, and the exit then deleted the login.
            result = conn.call("Network.getAllCookies", {})
            cookies = result.get("cookies") if isinstance(result, dict) else None
            if not isinstance(cookies, list):
                return
            now = time.time()
            for cookie in cookies:
                if not session_cookie(cookie):
                    continue
                try:
                    save_session_cookie(conn, cookie, now)
                except (OSError, ValueError, TypeError):
                    continue
    except (OSError, ValueError, TypeError, KeyError):
        return


def local_cdp_ws(ws_url):
    """Use the browser on this machine, not the host name it advertised.

    That name often does not resolve here. The cookie write then never
    happens, and the exit deletes the website login.
    """
    if not isinstance(ws_url, str) or not ws_url.startswith("ws://"):
        return ""
    rest = ws_url[len("ws://"):]
    hostport, sep, path = rest.partition("/")
    if not sep or not path:
        return ""
    if hostport.startswith("["):
        _, _, tail = hostport[1:].partition("]")
        port = tail[1:] if tail.startswith(":") else ""
    else:
        _, _, port = hostport.partition(":")
    if not port.isdigit():
        return ""
    return "ws://127.0.0.1:%s/%s" % (port, path)


class cdp_connection:
    def __init__(self, ws_url):
        self.sock = None
        self.ws_url = ws_url
        self.pending = bytearray()
        self.call_id = 0
        self.saw_document = False
        self.watch_document = False
        self.baseline_loader = ""

    def __enter__(self):
        local = local_cdp_ws(self.ws_url)
        if local:
            self.ws_url = local
        rest = self.ws_url[len("ws://"):]
        hostport, _, path = rest.partition("/")
        if hostport.startswith("["):
            host, _, tail = hostport[1:].partition("]")
            port = int((tail[1:] if tail.startswith(":") else "") or "80")
        else:
            host, _, port_text = hostport.partition(":")
            port = int(port_text or "80")
        sock = socket.create_connection((host, port), 5)
        sock.settimeout(8)
        key = base64.b64encode(os.urandom(16)).decode("ascii")
        request = (
            "GET /%s HTTP/1.1\r\nHost: %s:%d\r\nUpgrade: websocket\r\n"
            "Connection: Upgrade\r\nSec-WebSocket-Key: %s\r\n"
            "Sec-WebSocket-Version: 13\r\n\r\n"
        ) % (path, host, port, key)
        sock.sendall(request.encode("ascii"))
        buf = bytearray()
        while b"\r\n\r\n" not in buf:
            chunk = sock.recv(4096)
            if not chunk:
                break
            buf.extend(chunk)
        # The cookie reply can share this read with the handshake. Dropping
        # those bytes makes the write time out, and the exit deletes the login.
        header, sep, extra = bytes(buf).partition(b"\r\n\r\n")
        if not sep or b" 101 " not in header.split(b"\r\n", 1)[0]:
            sock.close()
            raise OSError("cdp websocket was rejected")
        self.pending = bytearray(extra)
        self.sock = sock
        return self

    def __exit__(self, exc_type, exc, tb):
        if self.sock is not None:
            try:
                self.sock.close()
            except OSError:
                pass
            self.sock = None
        return False

    def call(self, method, params):
        self.call_id += 1
        want = self.call_id
        payload = json.dumps({"id": want, "method": method, "params": params or {}}).encode("utf-8")
        self.sock.sendall(ws_client_frame(payload))
        # Events have no id. Stopping after a fixed number of them hides the
        # cookie reply, and the browser exit then deletes the website login.
        deadline = time.time() + 8
        while time.time() < deadline:
            message = json.loads(ws_read(self.sock, self.pending).decode("utf-8"))
            if not isinstance(message, dict):
                continue
            # The reload reply can arrive after the new page event. Remember
            # that event here, or the next start reloads the logged-in page again.
            # Events from attaching are the page already open. Only a
            # document after the reload is the one that receives the login.
            if self.watch_document and main_document_navigated(message) and document_replaced(self, message):
                self.saw_document = True
            if message.get("id") != want:
                continue
            if message.get("error"):
                raise OSError("cdp call failed")
            result = message.get("result")
            return result if isinstance(result, dict) else {}
        raise OSError("cdp call had no reply")


def http_json(host, port, path):
    data = http_json_any(host, port, path)
    if not isinstance(data, dict):
        raise ValueError("cdp version is not an object")
    return data


def http_message_body(buf):
    """The JSON after the headers. A chunked listing is still the page list."""
    head, sep, body = buf.partition(b"\r\n\r\n")
    if not sep:
        raise ValueError("cdp http response has no body")
    header = head.decode("iso-8859-1", "replace").lower()
    encoding = ""
    for line in header.split("\n"):
        if line.startswith("transfer-encoding:"):
            encoding = line.split(":", 1)[1]
    if "chunked" in encoding:
        return unchunk_http(body)
    return body


def unchunk_http(body):
    out = bytearray()
    while body:
        line, sep, body = body.partition(b"\r\n")
        if not sep:
            raise ValueError("cdp http chunk is truncated")
        size_text = line.split(b";", 1)[0].strip()
        if not size_text:
            continue
        size = int(size_text, 16)
        if size == 0:
            return bytes(out)
        if len(body) < size:
            raise ValueError("cdp http chunk is truncated")
        out += body[:size]
        body = body[size:]
        if body.startswith(b"\r\n"):
            body = body[2:]
    raise ValueError("cdp http chunk is truncated")


def http_json_any(host, port, path):
    sock = socket.create_connection((host, port), 2)
    try:
        sock.settimeout(2)
        sock.sendall(("GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n" % (path, host)).encode("ascii"))
        buf = bytearray()
        while True:
            try:
                chunk = sock.recv(65536)
            except (TimeoutError, socket.timeout):
                # The page list is already here. Waiting for the debug port
                # to close must not throw that listing away. Python 3.9
                # raises socket.timeout, which is not TimeoutError yet.
                break
            if not chunk:
                break
            buf += chunk
    finally:
        sock.close()
    return json.loads(http_message_body(bytes(buf)).decode("utf-8"))


def page_origin(raw):
    try:
        parts = urlsplit(str(raw))
    except ValueError:
        return ""
    if parts.scheme not in ("http", "https") or not parts.netloc:
        return ""
    return parts.scheme + "://" + parts.netloc.lower()


def cdp_eval_value(result):
    if not isinstance(result, dict) or result.get("exceptionDetails"):
        return None
    inner = result.get("result")
    if not isinstance(inner, dict):
        return None
    return inner.get("value")


def page_login_path(key):
    return ROOT / key / "page-login.json"


def merge_unread_page_login(key, saved, read_origins):
    """Keep a tab whose login could not be read this time.

    Other tabs that were read are updated. Replacing the whole file would
    drop the unread tab, and the next start of this same browser would be
    logged out on that site.
    """
    seen = set(read_origins)
    merged = list(saved)
    for item in merged:
        origin = page_origin(item.get("url") or "")
        if origin:
            seen.add(origin)
    try:
        previous = json.loads(page_login_path(key).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return merged
    if not isinstance(previous, list):
        return merged
    for item in previous:
        if len(merged) >= 8:
            break
        if not isinstance(item, dict):
            continue
        origin = page_origin(item.get("url") or "")
        if not origin or origin in seen:
            continue
        seen.add(origin)
        merged.append(item)
    return merged


def capture_page_login(key):
    """Remember this tab's login before the browser exits.

    Cookies are saved on their own. A login that lives in the tab is gone
    when the process exits, so the next start of this same profile would be
    logged out. The agent continues in that browser only if the tab still
    has it.
    """
    try:
        pages = http_json_any("127.0.0.1", chrome_debug_port(key), "/json/list")
    except (OSError, ValueError, TypeError):
        return
    if not isinstance(pages, list):
        return
    saved = []
    read_origins = set()
    unread = False
    for page in pages:
        if len(saved) >= 8:
            # Later tabs were not read. Keep their previous login instead of
            # replacing the file with only the tabs that fit.
            unread = True
            break
        if not isinstance(page, dict) or page.get("type") != "page":
            continue
        url = str(page.get("url") or "")
        origin = page_origin(url)
        if origin == "":
            continue
        state = read_page_storage(str(page.get("webSocketDebuggerUrl") or ""))
        if not isinstance(state, dict):
            # The page was there but its login could not be read. Keep the
            # previous record instead of treating this tab as logged out.
            unread = True
            continue
        read_origins.add(origin)
        session = state.get("session") if isinstance(state.get("session"), dict) else {}
        local = state.get("local") if isinstance(state.get("local"), dict) else {}
        if not session and not local:
            continue
        item = {"url": url, "session": session, "local": local}
        if len(json.dumps(item, ensure_ascii=False).encode("utf-8")) > 512 * 1024:
            unread = True
            continue
        saved.append(item)
    if unread:
        saved = merge_unread_page_login(key, saved, read_origins)
    path = page_login_path(key)
    try:
        if not saved:
            if unread:
                return
            path.unlink()
            return
        path.parent.mkdir(mode=0o700, exist_ok=True)
        path.write_text(json.dumps(saved, ensure_ascii=False), encoding="utf-8")
    except OSError:
        return


def read_page_storage(ws):
    if not isinstance(ws, str) or not ws.startswith("ws://"):
        return None
    expression = (
        "(() => { const dump = (store) => { const out = {}; "
        "for (let i = 0; i < store.length; i++) { const key = store.key(i); "
        "out[key] = store.getItem(key); } return out; }; "
        "return {session: dump(sessionStorage), local: dump(localStorage)}; })()"
    )
    try:
        with cdp_connection(ws) as conn:
            result = conn.call("Runtime.evaluate", {"expression": expression, "returnByValue": True})
    except (OSError, ValueError, TypeError):
        return None
    value = cdp_eval_value(result)
    return value if isinstance(value, dict) else None


def browser_pages(listed):
    if not isinstance(listed, list):
        return []
    pages = []
    for item in listed:
        if not isinstance(item, dict) or item.get("type") != "page":
            continue
        if not str(item.get("webSocketDebuggerUrl") or "").startswith("ws://"):
            continue
        pages.append(item)
    return pages


def saved_page_origins(saved):
    origins = set()
    for item in saved:
        if not isinstance(item, dict):
            continue
        origin = page_origin(item.get("url") or "")
        if origin:
            origins.add(origin)
    return origins


def restore_page_login(key):
    """Put the tab login back into the browser this profile just opened."""
    path = page_login_path(key)
    try:
        saved = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return
    if not isinstance(saved, list):
        return
    origins = saved_page_origins(saved)
    # A blank tab often appears before Chrome restores the logged-in one.
    # Writing the login into that first tab, then letting the real tab open,
    # leaves the website without it.
    pages = []
    previous = None
    stable = 0
    settled = False
    for _ in range(24):
        try:
            listed = http_json_any("127.0.0.1", chrome_debug_port(key), "/json/list")
        except (OSError, ValueError, TypeError):
            listed = None
        current = browser_pages(listed)
        if not current:
            time.sleep(0.25)
            continue
        pages = current
        # One site often appears before the others. Stopping there and
        # deleting the record drops the login that has not opened yet.
        open_origins = {page_origin(item.get("url") or "") for item in current}
        open_origins.discard("")
        if origins and origins.issubset(open_origins):
            settled = True
            break
        signature = tuple(sorted(str(item.get("url") or "") for item in current))
        if signature == previous:
            stable += 1
            # A blank tab that stays blank is the whole window. A tab that
            # is still changing is Chrome opening the logged-in page.
            if stable >= 8:
                settled = True
                break
        else:
            stable = 0
            previous = signature
        time.sleep(0.25)
    if not pages or not settled:
        return
    reloaded = set()
    applied = False
    left = []
    for item in saved:
        if not isinstance(item, dict):
            continue
        # A blank tab used for one site must not be reused for the next.
        try:
            listed = http_json_any("127.0.0.1", chrome_debug_port(key), "/json/list")
            fresh = browser_pages(listed)
            if fresh:
                pages = fresh
        except (OSError, ValueError, TypeError):
            pass
        if apply_page_login(key, pages, item, reloaded):
            applied = True
        elif page_origin(item.get("url") or ""):
            left.append(item)
    if not applied:
        return
    if left:
        try:
            path.write_text(json.dumps(left, ensure_ascii=False), encoding="utf-8")
        except OSError:
            pass
        return
    try:
        path.unlink()
    except OSError:
        pass


def page_storage_script(payload):
    """Fill this tab's storage before the website reads it.

    The script runs in the page, not a side world. A missing key is filled
    once. A key the site already has, including one it cleared on logout, is
    left alone on later loads because the script is removed after this reload.
    """
    return (
        "(() => { const data = " + payload + "; const fill = (store, values) => {"
        " if (!values || typeof values !== 'object') return;"
        " for (const key of Object.keys(values)) {"
        " if (store.getItem(key) === null && values[key] !== null && values[key] !== undefined) {"
        " store.setItem(key, String(values[key])); } } };"
        " try { fill(sessionStorage, data.session); fill(localStorage, data.local); } catch (e) {} })()"
    )


def main_document_navigated(message):
    """True for the real page, not an iframe or the blank document Chrome opens first."""
    if not isinstance(message, dict) or message.get("method") != "Page.frameNavigated":
        return False
    params = message.get("params")
    frame = params.get("frame") if isinstance(params, dict) else None
    if not isinstance(frame, dict) or frame.get("parentId"):
        return False
    return page_origin(frame.get("url") or "") != ""


def document_loader(message):
    params = message.get("params") if isinstance(message, dict) else None
    frame = params.get("frame") if isinstance(params, dict) else None
    if not isinstance(frame, dict):
        return ""
    return str(frame.get("loaderId") or "")


def main_loader_id(tree):
    """The document that is open now, before this reload."""
    if not isinstance(tree, dict):
        return ""
    frame_tree = tree.get("frameTree")
    frame = frame_tree.get("frame") if isinstance(frame_tree, dict) else None
    if not isinstance(frame, dict) or frame.get("parentId"):
        return ""
    return str(frame.get("loaderId") or "")


def document_replaced(conn, message):
    """True when this navigation is not the page that was already open.

    The debugger reports that page as soon as it attaches. Treating it as
    the reload removes the login script before the new document reads it.
    """
    loader = document_loader(message)
    if not loader:
        return False
    baseline = str(getattr(conn, "baseline_loader", "") or "")
    return baseline == "" or loader != baseline


def discard_buffered_cdp(conn):
    """Drop events that arrived before the reload.

    Attaching reports the page that is already open. Treating that report
    as the reloaded document removes the login script before it runs, and
    the next start of this browser is logged out.
    """
    if conn is None or conn.sock is None:
        return
    conn.watch_document = False
    conn.saw_document = False
    deadline = time.time() + 0.2
    try:
        conn.sock.settimeout(0.05)
    except OSError:
        conn.watch_document = True
        return
    try:
        while time.time() < deadline:
            try:
                ws_read(conn.sock, conn.pending)
            except (OSError, ValueError, TypeError):
                if not conn.pending:
                    break
    finally:
        try:
            conn.sock.settimeout(8)
        except OSError:
            pass
    conn.saw_document = False
    conn.watch_document = True


def wait_for_new_document(conn):
    """True once the reloaded page has started, so its login script has run."""
    if conn.sock is None:
        return False
    if getattr(conn, "saw_document", False):
        time.sleep(0.2)
        return True
    deadline = time.time() + 2
    conn.sock.settimeout(0.4)
    try:
        while time.time() < deadline:
            try:
                message = json.loads(ws_read(conn.sock, conn.pending).decode("utf-8"))
            except (OSError, ValueError, TypeError):
                continue
            if not main_document_navigated(message) or not document_replaced(conn, message):
                continue
            time.sleep(0.2)
            return True
    finally:
        try:
            conn.sock.settimeout(8)
        except OSError:
            pass
    return False


def open_same_browser_tab(key, pages):
    """Open one tab in this browser when a saved login has nowhere to go.

    A blank tab can take the next site. With none, that login would stay in
    the record and never return to the browser the person and the agent share.
    """
    if not isinstance(pages, list):
        return None
    try:
        version = http_json("127.0.0.1", chrome_debug_port(key), "/json/version")
        ws = version.get("webSocketDebuggerUrl") or ""
        if not isinstance(ws, str) or not ws.startswith("ws://"):
            return None
        with cdp_connection(ws) as conn:
            # No page address here. The saved site is opened only after its
            # login script is in place, so the site cannot read an empty tab.
            created = conn.call("Target.createTarget", {"url": "data:,"})
    except (OSError, ValueError, TypeError, KeyError):
        return None
    target_id = str(created.get("targetId") or "") if isinstance(created, dict) else ""
    if not target_id:
        return None
    for _ in range(10):
        time.sleep(0.1)
        try:
            listed = http_json_any("127.0.0.1", chrome_debug_port(key), "/json/list")
        except (OSError, ValueError, TypeError):
            continue
        for page in browser_pages(listed):
            # Another tab can appear with no address while Chrome restores a
            # site. Navigating that tab would leave its login behind.
            if str(page.get("id") or "") != target_id:
                continue
            if page_origin(page.get("url") or "") == "":
                return page
    return None


def apply_page_login(key, pages, item, reloaded):
    origin = page_origin(item.get("url") or "")
    if not origin:
        return False
    session = item.get("session") if isinstance(item.get("session"), dict) else {}
    local = item.get("local") if isinstance(item.get("local"), dict) else {}
    payload = json.dumps({"session": session, "local": local}, ensure_ascii=False)
    script = page_storage_script(payload)
    targets = []
    blanks = []
    for page in pages:
        if not isinstance(page, dict) or page.get("type") != "page":
            continue
        ws = str(page.get("webSocketDebuggerUrl") or "")
        if not ws.startswith("ws://") or ws in reloaded:
            continue
        if page_origin(page.get("url") or "") == origin:
            targets.append(page)
        elif page_origin(page.get("url") or "") == "":
            blanks.append(page)
    # Another site is already open in this browser. Navigating it away would
    # leave that login behind. A blank tab can open the saved site instead.
    navigate = ""
    if not targets and blanks:
        targets = blanks[:1]
        navigate = str(item.get("url") or "")
    elif not targets:
        opened = open_same_browser_tab(key, pages)
        if opened:
            targets = [opened]
            navigate = str(item.get("url") or "")
    applied = False
    for page in targets:
        ws = str(page.get("webSocketDebuggerUrl") or "")
        try:
            with cdp_connection(ws) as conn:
                # localStorage is read when the page opens. Writing it into the
                # document that already loaded leaves this browser logged out.
                if session or local or navigate:
                    conn.call("Page.enable", {})
                    added = conn.call("Page.addScriptToEvaluateOnNewDocument", {"source": script})
                    conn.baseline_loader = main_loader_id(conn.call("Page.getFrameTree", {}))
                    conn.saw_document = False
                    discard_buffered_cdp(conn)
                    if navigate:
                        conn.call("Page.navigate", {"url": navigate})
                    else:
                        conn.call("Page.reload", {})
                    saw = wait_for_new_document(conn)
                    ident = added.get("identifier") if isinstance(added, dict) else ""
                    if ident:
                        # The script only belongs to this reload. Leaving it
                        # in would write the old login over a later visit.
                        conn.call("Page.removeScriptToEvaluateOnNewDocument", {"identifier": ident})
                    if not saw:
                        continue
                else:
                    conn.call("Runtime.evaluate", {"expression": script, "returnByValue": True})
                reloaded.add(ws)
                applied = True
        except (OSError, ValueError, TypeError):
            continue
    return applied


def ws_client_frame(payload, opcode=0x81):
    mask = os.urandom(4)
    length = len(payload)
    header = bytearray([opcode])
    if length < 126:
        header.append(0x80 | length)
    elif length < 65536:
        header.append(0x80 | 126)
        header.extend(struct.pack(">H", length))
    else:
        header.append(0x80 | 127)
        header.extend(struct.pack(">Q", length))
    masked = bytes(byte ^ mask[index % 4] for index, byte in enumerate(payload))
    return bytes(header) + mask + masked


def ws_read(sock, pending=None):
    """Read one CDP message. Chromium may split a large cookie list across frames.

    A short frame cap cuts that list off. The browser then exits without the
    website login. A ping has to be answered with the same payload, or the
    connection drops while those cookies are still being written. Bytes that
    arrived with the handshake stay in pending, or that first reply is lost.
    """
    if pending is None:
        pending = bytearray()
    parts = []
    total = 0
    frames = 0
    while frames < 4096:
        frames += 1
        fin, opcode, data = ws_frame(sock, pending)
        if opcode == 8:
            raise OSError("cdp websocket closed")
        if opcode == 9:
            sock.sendall(ws_client_frame(data, 0x8A))
            continue
        if opcode in (0, 1):
            total += len(data)
            if total > 8 * 1024 * 1024:
                raise OSError("cdp message is too large")
            parts.append(data)
        if fin and parts:
            return b"".join(parts)
    raise OSError("cdp message was incomplete")


def ws_frame(sock, pending=None):
    if pending is None:
        pending = bytearray()

    def recvn(count):
        while len(pending) < count:
            chunk = sock.recv(max(count - len(pending), 4096))
            if not chunk:
                raise OSError("short websocket frame")
            pending.extend(chunk)
        out = bytes(pending[:count])
        del pending[:count]
        return out

    first, second = recvn(2)
    opcode = first & 0x0F
    length = second & 0x7F
    if length == 126:
        length = struct.unpack(">H", recvn(2))[0]
    elif length == 127:
        length = struct.unpack(">Q", recvn(8))[0]
    if length > 8 * 1024 * 1024:
        raise OSError("cdp frame is too large")
    if second & 0x80:
        mask = recvn(4)
        data = bytes(byte ^ mask[index % 4] for index, byte in enumerate(recvn(length)))
    else:
        data = recvn(length)
    return bool(first & 0x80), opcode, data


def flush_browser(key):
    """Ask Chromium to exit so cookies and the open tabs are written down.

    Stopping the container kills the browser immediately. The website login
    would then be missing the next time this profile starts. A second signal,
    or closing the display while this process is still exiting, drops that write.
    The container stop calls this twice: once before docker stop, and again
    from pid 1. Only the first call signals. The later call waits.
    A start waits for this lock, so it cannot open a second browser while
    the website login is still being written.
    """
    with desktop_lock():
        return flush_browser_locked(key)


def flush_browser_locked(key):
    profile = ROOT / key / "profile"
    pid = browser_pid(profile) or read_pids(key).get("chromium")
    if not pid_alive(pid):
        clear_flush_flag(key)
        return 0
    owned = claim_flush(key)
    if not owned and flush_owner_gone(key):
        clear_flush_flag(key)
        owned = claim_flush(key)
    if owned:
        # A clean exit deletes cookies the site marked as session-only.
        # Write them into this profile first, then let the browser exit.
        persist_website_login(key)
        capture_page_login(key)
        try:
            os.kill(int(pid), 15)
        except (OSError, TypeError, ValueError):
            clear_flush_flag(key)
            return 0
        mark_flush_signaled(key)
    for _ in range(150):
        if not browser_pid(profile) and not pid_alive(pid):
            clear_flush_flag(key)
            return 0
        # The first flush died before it could signal. This stop still has
        # to write the website login into the same profile.
        if not owned and flush_owner_gone(key):
            clear_flush_flag(key)
            if claim_flush(key):
                owned = True
                persist_website_login(key)
                capture_page_login(key)
                try:
                    os.kill(int(pid), 15)
                except (OSError, TypeError, ValueError):
                    clear_flush_flag(key)
                    return 0
                mark_flush_signaled(key)
        time.sleep(0.1)
    return 0


def browser_bin():
    for name in ("chromium", "chromium-browser", "google-chrome"):
        found = shutil.which(name)
        if found:
            return found
    return "chromium"


def pid_alive(pid):
    try:
        pid = int(pid or 0)
    except (TypeError, ValueError):
        return False
    if pid <= 0:
        return False
    try:
        text = Path("/proc/%d/stat" % pid).read_text(encoding="utf-8")
    except OSError:
        return False
    # "pid (comm) state". A zombie still has a /proc entry and the old
    # command line, but it is not the browser that holds the website login.
    end = text.rfind(")")
    if end < 0 or end + 2 >= len(text):
        return False
    state = text[end + 2:].lstrip().split(" ", 1)[0]
    return state != "" and state != "Z"


def read_pids(key):
    pid_file = ROOT / key / "pids.json"
    try:
        data = json.loads(pid_file.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}
    return data if isinstance(data, dict) else {}


def desktop_token_for(key):
    """This key's gate token, or "" before the first ensure recorded one."""
    try:
        registry = load_registry()
        token = (registry.get("users", {}).get(key) or {}).get("token")
    except (OSError, ValueError):
        return ""
    return token if token and valid_key(token) else ""


def ensure_proxy(key, display):
    """Bring the CDP gate back without restarting the browser.

    The website login lives in the running Chromium. A dead gate only
    hides that browser from the agent.
    """
    pids = read_pids(key)
    if pid_alive(pids.get("proxy")):
        return
    token = desktop_token_for(key)
    if not token:
        return
    kill_pid(pids.get("proxy"))
    home = ROOT / key
    home.mkdir(mode=0o700, exist_ok=True)
    log = open(home / "desktop.log", "ab")
    chrome_port = 18000 + display
    proxy = spawn([sys.executable, __file__, "gate", str(proxy_port(display)), str(chrome_port), token], os.environ.copy(), log)
    pids["proxy"] = proxy.pid
    (home / "pids.json").write_text(json.dumps(pids), encoding="utf-8")


def ensure_vnc(key, display):
    """Bring noVNC back without restarting the browser.

    The screen capture and the web client are a pair. A live websockify whose
    x11vnc has exited still looks healthy and leaves the login view black.
    """
    if shutil.which("x11vnc") is None or shutil.which("websockify") is None:
        return
    pids = read_pids(key)
    if vnc_pair_alive(pids):
        return
    token = desktop_token_for(key)
    if not token:
        return
    kill_pid(pids.get("vnc"))
    kill_pid(pids.get("vgate"))
    kill_pid(pids.get("x11vnc"))
    home = ROOT / key
    home.mkdir(mode=0o700, exist_ok=True)
    log = open(home / "desktop.log", "ab")
    env = os.environ.copy()
    env["DISPLAY"] = ":%d" % display
    pair = start_vnc(env, log, display, token)
    if pair is None:
        return
    x11, web, gate = pair
    if x11 is not None:
        pids["x11vnc"] = x11.pid
    else:
        pids.pop("x11vnc", None)
    if web is not None:
        pids["vnc"] = web.pid
    else:
        pids.pop("vnc", None)
    if gate is not None:
        pids["vgate"] = gate.pid
    else:
        pids.pop("vgate", None)
    (home / "pids.json").write_text(json.dumps(pids), encoding="utf-8")


def vnc_pair_alive(pids):
    # Process liveness only. Opening the VNC port here drops the person's
    # login connection, and one refused probe used to restart a healthy view.
    return pid_alive(pids.get("x11vnc")) and pid_alive(pids.get("vnc")) and pid_alive(pids.get("vgate"))


def kill_pid(pid):
    try:
        pid = int(pid or 0)
    except (TypeError, ValueError):
        return
    if pid <= 0 or not Path("/proc/%d" % pid).exists():
        return
    try:
        os.kill(pid, 15)
    except OSError:
        return
    for _ in range(20):
        if not Path("/proc/%d" % pid).exists():
            return
        time.sleep(0.05)
    try:
        os.kill(pid, 9)
    except OSError:
        pass


def ensure_watch(key, display):
    """Keep the login view up while the browser is still running."""
    pids = read_pids(key)
    if pid_alive(pids.get("watch")):
        return
    home = ROOT / key
    home.mkdir(mode=0o700, exist_ok=True)
    log = open(home / "desktop.log", "ab")
    proc = spawn([sys.executable, __file__, "watch", key, str(display)], os.environ.copy(), log)
    pids["watch"] = proc.pid
    (home / "pids.json").write_text(json.dumps(pids), encoding="utf-8")


def watch_vnc(key, display):
    if not valid_key(key):
        return 2
    while desktop_running(key):
        lock_file = open(ROOT / "lock", "a", encoding="utf-8")
        try:
            fcntl.flock(lock_file, fcntl.LOCK_EX)
            if desktop_running(key):
                ensure_vnc(key, display)
                ensure_proxy(key, display)
        finally:
            fcntl.flock(lock_file, fcntl.LOCK_UN)
            lock_file.close()
        time.sleep(2)
    return 0


def start_vnc(env, log, display, token):
    """Start noVNC when the image has it. A missing binary does not stop the desktop."""
    if shutil.which("x11vnc") is None:
        return None
    x11 = spawn(["x11vnc", "-display", ":%d" % display, "-nopw", "-localhost", "-forever", "-shared", "-rfbport", "5900"], env, log)
    web = None
    gate = None
    if shutil.which("websockify") is not None:
        web = spawn(["websockify", "--web", "/usr/share/novnc", "127.0.0.1:%d" % VNC_WEBSOCKIFY_PORT, "localhost:5900"], env, log)
        gate = spawn([sys.executable, __file__, "gate", str(VNC_GATE_PORT), str(VNC_WEBSOCKIFY_PORT), token], env, log)
        for _ in range(20):
            if port_open(VNC_GATE_PORT) and port_open(5900):
                break
            time.sleep(0.1)
    return x11, web, gate


def spawn(argv, env, log):
    return subprocess.Popen(argv, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)


def wait_for(ready, label):
    for _ in range(80):
        if ready():
            return
        time.sleep(0.25)
    raise SystemExit(label + " did not start")


def port_open(port):
    try:
        sock = socket.create_connection(("127.0.0.1", port), 0.2)
    except OSError:
        return False
    sock.close()
    return True


def proxy_port(display):
    return 19000 + display


def reap(users, current):
    running = [key for key in users if key != current and desktop_running(key)]
    running.sort(key=lambda key: int(users[key].get("last_used") or 0))
    while len(running) + 1 > MAX_RUNNING:
        stop_desktop(running.pop(0))


def print_idle(key):
    """One JSON line with how long this user's desktop has been idle."""
    registry = load_registry()
    entry = (registry.get("users", {}) or {}).get(key) or {}
    last_used = int(entry.get("last_used") or 0)
    print(json.dumps({
        "running": desktop_running(key),
        "idle_seconds": max(0, int(time.time()) - last_used) if last_used else 0,
        "known": bool(last_used),
    }))
    return 0


def stop_desktop(key):
    # Write cookies and tabs before the process is gone. The next start of
    # this same profile then still has the website login.
    flush_browser(key)
    profile = ROOT / key / "profile"
    browser_gone = not browser_pid(profile)
    pid_file = ROOT / key / "pids.json"
    try:
        pids = json.loads(pid_file.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return
    if not isinstance(pids, dict):
        return
    for name, pid in pids.items():
        # The browser was already asked to exit. Signaling it again, or
        # closing its display first, aborts the cookie write.
        if name in ("chromium", "xvfb", "fluxbox") and not browser_gone:
            continue
        if name == "chromium":
            continue
        try:
            os.kill(int(pid), 15)
        except (OSError, TypeError, ValueError):
            pass
    if not browser_gone:
        return
    try:
        pid_file.unlink()
    except OSError:
        pass


def load_registry():
    path = ROOT / "registry.json"
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {"users": {}}
    if not isinstance(data.get("users"), dict):
        data["users"] = {}
    return data


def save_registry(registry):
    path = ROOT / "registry.json"
    temporary = path.with_suffix(".json.tmp")
    temporary.write_text(json.dumps(registry), encoding="utf-8")
    os.replace(temporary, path)


def pipe(left, right):
    try:
        while True:
            data = left.recv(65536)
            if not data:
                break
            right.sendall(data)
    except OSError:
        pass
    for item in (left, right):
        try:
            item.close()
        except OSError:
            pass


def serve_gate(listen_port, target_port, token):
    """Token-checked TCP gate for the container's published browser ports.

    Each connection must open with an HTTP request carrying a valid
    Authorization: Bearer token; the connection is then piped raw so both the
    CDP JSON endpoints and WebSocket upgrades pass through unchanged. The
    token travels as URL userinfo from desktopd and is attached by the Hub
    noVNC proxy, so an unauthenticated peer on the Docker host network can no
    longer drive a logged-in browser or watch its screen.
    """
    server = socket.socket()
    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server.bind(("0.0.0.0", listen_port))
    server.listen(32)

    while True:
        client, _ = server.accept()
        try:
            head = read_request_head(client)
            if head is None or not request_authorized(head, token):
                client.close()
                continue
            remote = None
            for _ in range(40):
                try:
                    remote = socket.create_connection(("127.0.0.1", target_port), 1)
                    break
                except OSError:
                    time.sleep(0.25)
            if remote is None:
                client.close()
                continue
            # read_request_head leaves a 10s timeout on the client and
            # create_connection leaves its connect timeout on the remote.
            # Both must go: an idle noVNC screen or a quiet CDP session is
            # normal, and the relay has to sit there silently for hours.
            client.settimeout(None)
            remote.settimeout(None)
            remote.sendall(head)
            threading.Thread(target=pipe, args=(client, remote), daemon=True).start()
            threading.Thread(target=pipe, args=(remote, client), daemon=True).start()
        except OSError:
            try:
                client.close()
            except OSError:
                pass


def read_request_head(client, limit=16384, timeout=10):
    """Read up to the end of the HTTP request header block.

    Returns every byte received so far — a body arriving with the same recv is
    forwarded along with the head when the caller relays the connection.
    """
    client.settimeout(timeout)
    chunks = []
    size = 0
    while size <= limit:
        try:
            data = client.recv(4096)
        except OSError:
            return None
        if not data:
            return None
        chunks.append(data)
        size += len(data)
        if b"\r\n\r\n" in data or b"\n\n" in data:
            return b"".join(chunks)
    return None


def request_authorized(head, token):
    try:
        text = head.decode("latin-1")
    except UnicodeDecodeError:
        return False
    lines = text.split("\n")
    for line in lines[1:]:
        if line.strip() == "":
            break
        name, _, value = line.partition(":")
        if name.strip().lower() != "authorization":
            continue
        scheme, _, credential = value.strip().partition(" ")
        if scheme.lower() != "bearer":
            return False
        return hmac.compare_digest(credential.strip(), token)
    return False


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv))
    except SystemExit as exc:
        if exc.code not in (None, 0):
            print(exc, file=sys.stderr)
            sys.exit(exc.code if isinstance(exc.code, int) else 1)
        raise
