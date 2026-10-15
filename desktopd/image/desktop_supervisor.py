#!/usr/bin/env python3
"""Start one virtual desktop per user key inside the GUI container.

Each key gets its own X display, Chromium profile, and CDP proxy. A later
ensure for the same key reuses that desktop. Other keys cannot see it.
"""
import base64
from contextlib import contextmanager
import fcntl
import hashlib
import hmac
import json
import os
import re
import secrets
import shlex
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
PROC = Path("/proc")
MAX_RUNNING = 3
DISPLAY_MIN = 20
DISPLAY_MAX = 50
# The VNC gate fronts websockify on the published noVNC port; websockify
# itself only listens on loopback so the raw screen cannot be reached.
VNC_GATE_PORT = 6080
VNC_WEBSOCKIFY_PORT = 6081
# Per-bot displays (desktop_mode=isolated). A bot key is the owner's 16-hex
# user key followed by 16 hex of the bot id. Each bot gets its own display
# above DISPLAY_MIN with only Xvfb, xfwm4 and its own Chromium profile. The
# owner's two published gates (CDP 19020, noVNC 6080) route a bot's token to
# that bot's display, so no new container port is needed.
BOT_IDLE_SECONDS = 600
BOT_BUSY_SECONDS = 120
BOT_HUMAN_YIELD_SECONDS = 20
MAX_BOTS_RUNNING = 3
# Memory savers for bot browsers (measured: a bot display costs ~70 MB plus
# its Chromium). Merged into the one --disable-features flag.
BOT_BROWSER_FLAGS = [
    "--renderer-process-limit=2",
    "--disable-site-isolation-trials",
    "--process-per-site",
    "--disable-component-update",
    "--js-flags=--max-old-space-size=512",
    "--disk-cache-size=67108864",
]
BOT_DISABLE_FEATURES = ["IsolateOrigins", "site-per-process", "Translate", "OptimizationHints", "MediaRouter", "BackForwardCache"]


def env_int(name, default):
    try:
        value = int(os.environ.get(name, "").strip())
    except ValueError:
        return default
    return value if value > 0 else default


def owner_key():
    owner = os.environ.get("MACLAW_DESKTOP_KEY", "").strip()
    return owner if valid_key(owner) and len(owner) == 16 else ""


def is_bot_key(key, owner=None):
    """A bot display key of this container's owner: owner key + 16 hex."""
    owner = owner_key() if owner is None else owner
    return bool(owner) and valid_key(key) and len(key) == 32 and key.startswith(owner)


def vnc_ws_port(display):
    return VNC_WEBSOCKIFY_PORT + (int(display) - DISPLAY_MIN)


def vnc_rfb_port(display):
    return 5900 + (int(display) - DISPLAY_MIN)
# A fixed screen size keeps agent click coordinates and screenshots stable;
# noVNC scales it for people. MACLAW_DESKTOP_GEOMETRY overrides it per image
# or container (WIDTHxHEIGHT or WIDTHxHEIGHTxDEPTH). Images built from
# Dockerfile.v2 set it to 1280x800 (smaller screenshots for models); this
# fallback stays 1440x900 so containers created from an older image keep
# their screen size when they get a newer supervisor.
DEFAULT_DESKTOP_GEOMETRY = "1440x900x24"


def desktop_geometry():
    value = os.environ.get("MACLAW_DESKTOP_GEOMETRY", "").strip().lower()
    parts = value.split("x")
    if len(parts) == 2:
        parts.append("24")
    if len(parts) != 3 or not all(part.isdigit() for part in parts):
        return DEFAULT_DESKTOP_GEOMETRY
    width, height, depth = (int(part) for part in parts)
    if not (640 <= width <= 3840 and 480 <= height <= 2160 and depth in (16, 24)):
        return DEFAULT_DESKTOP_GEOMETRY
    return "%dx%dx%d" % (width, height, depth)


def session_argv():
    """The window session for the display.

    maclaw-gui:2 ships XFCE, which people can actually use (panel, file
    manager, terminal, input method). maclaw-gui:1 only has fluxbox, so the
    same supervisor keeps working there.
    """
    if shutil.which("startxfce4") and shutil.which("dbus-launch"):
        return ["dbus-launch", "--exit-with-session", "startxfce4"]
    return ["fluxbox"]


def clear_stale_display(display):
    """Remove the X lock and socket a killed Xvfb left behind.

    docker stop ends the container with SIGKILL for everything but pid 1, so
    Xvfb never removes /tmp/.X<n>-lock. The container keeps its /tmp, and on
    the next start Xvfb refuses the display ("Server is already active") while
    the stale socket makes the display look ready. Only a lock whose pid is
    not a running Xvfb is removed.
    """
    lock = Path("/tmp/.X%d-lock" % display)
    try:
        pid = int(lock.read_text(encoding="ascii").strip())
    except (OSError, ValueError):
        pid = 0
    if pid > 0:
        try:
            comm = (PROC / str(pid) / "comm").read_text(encoding="utf-8").strip()
        except OSError:
            comm = ""
        if comm == "Xvfb":
            return
    for path in (lock, Path("/tmp/.X11-unix/X%d" % display)):
        try:
            path.unlink()
        except OSError:
            pass


def start_session_bus(env, log, display):
    """Start a D-Bus session bus for one desktop.

    Returns (address, pid), or ("", 0) when no bus could be started; the
    caller then falls back to dbus-launch, which gives XFCE a bus of its own.
    The bus listens on a fixed socket per display, so its address is known
    without reading it back from the daemon.
    """
    if not shutil.which("dbus-daemon"):
        return "", 0
    socket_path = Path("/tmp/.maclaw-dbus-%d" % display)
    try:
        socket_path.unlink()
    except OSError:
        pass
    address = "unix:path=%s" % socket_path
    try:
        bus = spawn(["dbus-daemon", "--session", "--nofork", "--nopidfile", "--address=" + address], env, log)
    except OSError:
        return "", 0
    for _ in range(40):
        if socket_path.exists():
            return address, bus.pid
        if bus.poll() is not None:
            return "", 0
        time.sleep(0.25)
    try:
        bus.kill()
    except OSError:
        pass
    return "", 0


# apt inside the desktop. Images built on a Tencent Cloud host carried that
# cloud's intranet mirror (mirrors.tencentyun.com) in debian.sources, and
# desktopd's egress proxy (HTTP(S)_PROXY) sends root's apt abroad, where that
# mirror does not exist: "502 Bad Gateway [IP: 172.17.0.1 18083]". The
# supervisor picks a public mirror and the route to it (direct or through the
# proxy) by measuring both, writes debian.sources and an apt proxy file, and
# keeps the proxy variables across sudo. MACLAW_APT_MIRROR overrides it:
#   unset/"auto"  measure deb.debian.org and mirrors.tencent.com, use the faster
#   "off"         leave the apt sources alone (the proxy file is still written)
#   a mirror      "mirrors.ustc.edu.cn", "https://mirror.example/" (one is
#                 used as is; several, comma separated, are measured)
APT_SOURCES = "etc/apt/sources.list.d/debian.sources"
APT_PROXY_CONF = "etc/apt/apt.conf.d/90maclaw-proxy"
APT_STATE = "var/lib/maclaw/apt-mirror.json"
APT_SUDOERS = "etc/sudoers.d/maclaw-proxy-env"
APT_MARKER = "# maclaw-apt-mirror:"
APT_DEFAULT_MIRROR = "http://deb.debian.org"
APT_AUTO_MIRRORS = (APT_DEFAULT_MIRROR, "https://mirrors.tencent.com")
# debian.sources pointing only at these hosts is a stock file the supervisor
# may rewrite; any other mirror in it was chosen by a person and stays.
APT_STOCK_HOSTS = frozenset((
    "deb.debian.org", "security.debian.org", "mirrors.tencentyun.com",
    "mirrors.tencent.com", "mirrors.cloud.tencent.com",
))
APT_RECHECK_SECONDS = 24 * 3600
APT_PROBE_TIMEOUT = 6.0
APT_CONFIG_VERSION = 1
PROXY_ENV_NAMES = ("http_proxy", "https_proxy", "no_proxy", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY")
SUDOERS_PROXY_ENV = (
    "# Written by /desktop_supervisor.py: sudo keeps the desktop's egress proxy.\n"
    'Defaults env_keep += "%s"\n' % " ".join(PROXY_ENV_NAMES)
)


def apt_write_file(path, text, mode):
    """Atomically write text unless the file already holds it."""
    path = Path(path)
    try:
        if path.exists() and path.read_text(encoding="utf-8") == text:
            if (path.stat().st_mode & 0o777) != mode:
                os.chmod(str(path), mode)
            return False
        path.parent.mkdir(parents=True, exist_ok=True)
        temporary = path.with_name("." + path.name + ".tmp")
        temporary.write_text(text, encoding="utf-8")
        os.chmod(str(temporary), mode)
        os.replace(str(temporary), str(path))
        return True
    except OSError:
        return False


def container_proxy(env=None):
    """The proxy URL apt should use, from the container environment, or ""."""
    env = os.environ if env is None else env
    for name in ("http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY"):
        value = (env.get(name) or "").strip()
        if not value:
            continue
        parsed = urlsplit(value)
        if parsed.scheme in ("http", "https") and parsed.hostname and not re.search(r'[\s";\\]', value):
            return value
        return ""
    return ""


def normalize_apt_mirror(raw):
    """"mirrors.ustc.edu.cn" / "https://x/debian/" -> "scheme://host[/path]", or ""."""
    value = raw.strip().rstrip("/")
    if not value:
        return ""
    if "://" not in value:
        value = "http://" + value
    parsed = urlsplit(value)
    path = parsed.path.rstrip("/")
    if path.endswith("/debian"):
        path = path[: -len("/debian")]
    if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.query or parsed.fragment:
        return ""
    if not re.fullmatch(r"[A-Za-z0-9.-]+(:[0-9]{1,5})?", parsed.netloc) or not re.fullmatch(r"[A-Za-z0-9._~/-]*", path):
        return ""
    return "%s://%s%s" % (parsed.scheme, parsed.netloc.lower(), path)


def apt_mirror_setting(env=None):
    """("off", []), ("auto", candidates) or ("fixed", [mirror]) from MACLAW_APT_MIRROR."""
    env = os.environ if env is None else env
    raw = (env.get("MACLAW_APT_MIRROR") or "").strip()
    if raw.lower() in ("off", "keep", "none", "no", "false", "0"):
        return "off", []
    if raw.lower() in ("", "auto"):
        return "auto", list(APT_AUTO_MIRRORS)
    mirrors = []
    for item in re.split(r"[\s,]+", raw):
        mirror = normalize_apt_mirror(item)
        if mirror and mirror not in mirrors:
            mirrors.append(mirror)
    if not mirrors:
        return "auto", list(APT_AUTO_MIRRORS)
    return ("fixed" if len(mirrors) == 1 else "auto"), mirrors


def debian_codename(root=Path("/")):
    try:
        for line in (Path(root) / "etc/os-release").read_text(encoding="utf-8").splitlines():
            if line.startswith("VERSION_CODENAME="):
                name = line.split("=", 1)[1].strip().strip('"')
                if re.fullmatch(r"[a-z]+", name):
                    return name
    except OSError:
        pass
    return "bookworm"


def apt_sources_text(mirror, codename):
    return (
        "%s %s\n"
        "# Written by /desktop_supervisor.py. Set MACLAW_APT_MIRROR=off on the\n"
        "# container to keep your own sources; edits to this file are replaced.\n"
        "Types: deb\n"
        "URIs: %s/debian\n"
        "Suites: %s %s-updates\n"
        "Components: main\n"
        "Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg\n"
        "\n"
        "Types: deb\n"
        "URIs: %s/debian-security\n"
        "Suites: %s-security\n"
        "Components: main\n"
        "Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg\n"
    ) % (APT_MARKER, mirror, mirror, codename, codename, mirror, codename)


def apt_sources_replaceable(text):
    """True for our own file or Debian's/Tencent's stock one, False for a person's."""
    if text is None or APT_MARKER in text:
        return True
    hosts = set()
    for line in text.splitlines():
        line = line.strip()
        if line.startswith("#") or not line:
            continue
        if line.lower().startswith("uris:"):
            urls = line.split(":", 1)[1].split()
        elif line.startswith(("deb ", "deb-src ")):
            urls = [word for word in line.split()[1:] if "://" in word][:1]
        else:
            continue
        for url in urls:
            hosts.add((urlsplit(url).hostname or "").lower())
    return bool(hosts) and hosts <= APT_STOCK_HOSTS


def apt_proxy_text(proxy, direct_mirror=""):
    """apt.conf: every repository through the proxy, the chosen mirror maybe direct.

    Root's apt reads HTTP(S)_PROXY from the environment, but sudo drops it and
    so do services. The file gives apt one route however it is started.
    """
    lines = [
        "// Written by /desktop_supervisor.py from the container's proxy environment.",
        'Acquire::http::Proxy "%s";' % proxy,
        'Acquire::https::Proxy "%s";' % proxy,
    ]
    host = (urlsplit(direct_mirror).hostname or "") if direct_mirror else ""
    if host:
        lines.append('Acquire::http::Proxy::%s "DIRECT";' % host)
        lines.append('Acquire::https::Proxy::%s "DIRECT";' % host)
    return "\n".join(lines) + "\n"


def probe_apt_mirror(mirror, codename, proxy, timeout=APT_PROBE_TIMEOUT):
    """Seconds to fetch the mirror's InRelease (direct, or through proxy), or None."""
    import urllib.request

    handler = urllib.request.ProxyHandler({"http": proxy, "https": proxy} if proxy else {})
    opener = urllib.request.build_opener(handler)
    url = "%s/debian/dists/%s/InRelease" % (mirror, codename)
    started = time.monotonic()
    try:
        with opener.open(urllib.request.Request(url, headers={"User-Agent": "maclaw-apt-probe"}), timeout=timeout) as response:
            if response.status != 200:
                return None
            body = response.read(4 << 20)
    except Exception:
        return None
    if b"Origin: Debian" not in body:
        return None
    return round(time.monotonic() - started, 3)


def choose_apt_route(mirrors, codename, proxy, probe=None):
    """(mirror, through_proxy, results): the fastest working mirror and route."""
    probe = probe or probe_apt_mirror
    routes = [(mirror, False) for mirror in mirrors]
    if proxy:
        routes += [(mirror, True) for mirror in mirrors]
    results = {}

    def run(route):
        results[route] = probe(route[0], codename, proxy if route[1] else "")

    threads = [threading.Thread(target=run, args=(route,), daemon=True) for route in routes]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join(APT_PROBE_TIMEOUT + 2)
    working = [(results[route], index, route) for index, route in enumerate(routes) if results.get(route) is not None]
    report = {"%s %s" % (mirror, "proxy" if via else "direct"): results.get((mirror, via)) for mirror, via in routes}
    if not working:
        return mirrors[0], bool(proxy), report
    _, _, (mirror, via) = min(working)
    return mirror, via, report


def apt_fingerprint(env, root=Path("/")):
    setting = apt_mirror_setting(env)
    raw = json.dumps([APT_CONFIG_VERSION, setting, container_proxy(env), debian_codename(root)])
    return hashlib.sha256(raw.encode()).hexdigest()[:16]


def read_apt_state(root=Path("/")):
    try:
        state = json.loads((Path(root) / APT_STATE).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}
    return state if isinstance(state, dict) else {}


def apt_config_current(root=Path("/"), env=None, now=None):
    env = os.environ if env is None else env
    now = time.time() if now is None else now
    state = read_apt_state(root)
    if state.get("fingerprint") != apt_fingerprint(env, root):
        return False
    if now - float(state.get("checked_at") or 0) > APT_RECHECK_SECONDS:
        return False
    if state.get("mirror"):
        try:
            text = (Path(root) / APT_SOURCES).read_text(encoding="utf-8")
        except OSError:
            return False
        return text == apt_sources_text(state["mirror"], debian_codename(root))
    return True


def write_sudoers_proxy_env(root=Path("/")):
    """sudo keeps HTTP(S)_PROXY (sudo's env_reset drops them otherwise)."""
    root = Path(root)
    target = root / APT_SUDOERS
    if not target.parent.is_dir():
        return False
    try:
        if target.exists() and target.read_text(encoding="utf-8") == SUDOERS_PROXY_ENV:
            return False
    except OSError:
        return False
    temporary = target.with_name(".maclaw-proxy-env.tmp")
    try:
        temporary.write_text(SUDOERS_PROXY_ENV, encoding="utf-8")
        os.chmod(str(temporary), 0o440)
        visudo = shutil.which("visudo")
        if visudo and str(root) == "/":
            # A broken sudoers.d file breaks every sudo; never install one.
            checked = subprocess.run([visudo, "-cqf", str(temporary)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
            if checked.returncode != 0:
                temporary.unlink()
                return False
        os.replace(str(temporary), str(target))
        return True
    except (OSError, subprocess.SubprocessError):
        try:
            temporary.unlink()
        except OSError:
            pass
        return False


def configure_apt(root=Path("/"), env=None, probe=None, now=None, default_only=False):
    """Point apt at a reachable public mirror and give it the container's proxy.

    Idempotent. default_only (image build) writes deb.debian.org without
    measuring anything and records no decision, so the first desktop start
    measures from wherever the container runs.
    """
    root = Path(root)
    env = os.environ if env is None else env
    now = time.time() if now is None else now
    codename = debian_codename(root)
    proxy = container_proxy(env)
    mode, mirrors = apt_mirror_setting(env)
    sources = root / APT_SOURCES
    try:
        current = sources.read_text(encoding="utf-8") if sources.exists() else None
    except OSError:
        current = ""
    result = {"mode": mode, "proxy": bool(proxy), "mirror": "", "via_proxy": bool(proxy)}
    write_sudoers_proxy_env(root)
    if default_only:
        if mode != "off" and apt_sources_replaceable(current):
            apt_write_file(sources, apt_sources_text(mirrors[0] if mode == "fixed" else APT_DEFAULT_MIRROR, codename), 0o644)
        return result
    replace = mode == "fixed" or (mode == "auto" and apt_sources_replaceable(current))
    if replace:
        if len(mirrors) == 1 and not proxy:
            mirror, via, report = mirrors[0], False, {}
        else:
            mirror, via, report = choose_apt_route(mirrors, codename, proxy, probe)
        apt_write_file(sources, apt_sources_text(mirror, codename), 0o644)
        result.update(mirror=mirror, via_proxy=bool(proxy) and via, probes=report)
    proxy_conf = root / APT_PROXY_CONF
    if proxy:
        direct = result["mirror"] if result["mirror"] and not result["via_proxy"] else ""
        apt_write_file(proxy_conf, apt_proxy_text(proxy, direct), 0o600 if urlsplit(proxy).username else 0o644)
    else:
        try:
            proxy_conf.unlink()
        except OSError:
            pass
    state = dict(result, fingerprint=apt_fingerprint(env, root), checked_at=int(now))
    apt_write_file(root / APT_STATE, json.dumps(state, sort_keys=True) + "\n", 0o644)
    return result


def configure_apt_locked():
    """configure_apt() for the CLI; a second run while one measures does nothing."""
    lock_path = Path("/run/maclaw-apt-mirror.lock")
    try:
        lock_file = open(lock_path, "a", encoding="utf-8")
    except OSError:
        lock_file = None
    try:
        if lock_file is not None:
            try:
                fcntl.flock(lock_file, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except OSError:
                return {"skipped": "another apt-mirror run is measuring"}
        return configure_apt()
    finally:
        if lock_file is not None:
            lock_file.close()


def ensure_apt_config():
    """Called by ensure(): measure in the background when the decision is stale.

    Measuring takes a few seconds; the desktop does not wait for it.
    """
    try:
        if apt_config_current():
            return
        log = open("/var/log/maclaw-apt-mirror.log", "ab")
        try:
            spawn([sys.executable, os.path.abspath(__file__), "apt-mirror"], os.environ.copy(), log)
        finally:
            log.close()
    except OSError as exc:
        print("apt mirror check skipped: %s" % exc, file=sys.stderr)


def main(argv):
    if len(argv) in (2, 3) and argv[1] == "apt-mirror":
        if len(argv) == 3 and argv[2] != "--default":
            print("usage: desktop_supervisor.py apt-mirror [--default]", file=sys.stderr)
            return 2
        if len(argv) == 3:
            print(json.dumps(configure_apt(default_only=True), sort_keys=True))
        else:
            print(json.dumps(configure_apt_locked(), sort_keys=True))
        return 0
    if len(argv) >= 2 and argv[1] == "browser":
        return open_shared_browser(argv[2:])
    if len(argv) == 3 and argv[1] == "ctl":
        return run_ctl(argv[2])
    if len(argv) == 3 and argv[1] == "a11y":
        return run_a11y(argv[2])
    if len(argv) == 2 and argv[1] == "install-browser":
        install_browser_integration()
        return 0
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
    if len(argv) == 3 and argv[1] == "display":
        key = argv[2]
        if not valid_key(key):
            print("invalid user key", file=sys.stderr)
            return 2
        entry = (load_registry().get("users", {}) or {}).get(key) or {}
        if not entry.get("display"):
            print("desktop was never opened", file=sys.stderr)
            return 1
        print(json.dumps({"display": ":%d" % int(entry["display"]), "running": desktop_running(key)}))
        return 0
    if len(argv) == 2 and argv[1] == "bots-busy":
        print(json.dumps({"busy": [key for key in running_bot_keys() if bot_busy(key)]}))
        return 0
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
    if owner and key != owner and not is_bot_key(key):
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
    if is_bot_key(key):
        return ensure_bot(key)
    ensure_apt_config()
    try:
        install_browser_integration()
    except OSError as exc:
        # A read-only image keeps its old browser entries; the desktop still opens.
        print("browser integration skipped: %s" % exc, file=sys.stderr)
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
            if other != key and not is_bot_key(other, owner):
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
    proc = PROC
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
        cmd = (PROC / str(int(pid)) / "cmdline").read_bytes()
    except (OSError, TypeError, ValueError):
        return True
    needle = b"--user-data-dir=" + str(profile).encode()
    return needle in cmd and b"--type=" not in cmd


def desktop_env(display):
    """The environment every program of this desktop runs with."""
    user_home = Path("/home/desktop")
    user_home.mkdir(mode=0o700, exist_ok=True)
    for path in (
        user_home / ".config",
        user_home / ".local" / "share",
        user_home / ".local" / "bin",
        user_home / ".cache",
    ):
        path.mkdir(parents=True, mode=0o700, exist_ok=True)
    env = os.environ.copy()
    env["HOME"] = str(user_home)
    env["XDG_CONFIG_HOME"] = str(user_home / ".config")
    env["XDG_DATA_HOME"] = str(user_home / ".local" / "share")
    env["XDG_CACHE_HOME"] = str(user_home / ".cache")
    env["PATH"] = str(user_home / ".local" / "bin") + os.pathsep + env.get("PATH", "")
    env["DISPLAY"] = ":%d" % display
    # The agent reads non-browser programs through AT-SPI (a11y command).
    # GTK exposes its tree unless NO_AT_BRIDGE is set; Qt only when asked.
    env.pop("NO_AT_BRIDGE", None)
    env["QT_ACCESSIBILITY"] = "1"
    env["QT_LINUX_ACCESSIBILITY_ALWAYS_ON"] = "1"
    return env


def browser_argv(profile, chrome_port, bot=False):
    """The one browser of this desktop: the agent's CDP and the person's window.

    Every other way to open a browser here (panel launcher, menu, xdg-open,
    x-www-browser) goes through open_shared_browser, which joins this
    process with the same --user-data-dir instead of starting a second one.
    """
    # No start URL. A blank page would cover the restored tabs, and the
    # website login lives in this profile.
    return [
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
        "--disable-features=" + ",".join(["TrackingProtection3pcd", "ThirdPartyStoragePartitioning"] + (BOT_DISABLE_FEATURES if bot else [])),
        "--remote-debugging-port=%d" % chrome_port,
        "--remote-allow-origins=*",
        "--start-maximized",
    ] + (BOT_BROWSER_FLAGS if bot else []) + browser_test_type_flags() + browser_proxy_flags()


def browser_test_type_flags():
    """--test-type hides the "--no-sandbox ... security will suffer" bar.

    Opt-in only (MACLAW_BROWSER_TEST_TYPE=1, e.g. on test desktops): it is a
    switch for Chromium's own test harnesses, it hides that warning from the
    person as well, and its other effects are not a stable contract.
    """
    if os.environ.get("MACLAW_BROWSER_TEST_TYPE", "").strip() == "1":
        return ["--test-type"]
    return []


def browser_proxy_flags():
    """Chromium proxy flags derived from the container's proxy environment.

    desktopd can inject HTTP(S)_PROXY into the container when the desktops
    egress through a proxy. Chromium does not read those variables, and it
    never takes credentials from --proxy-server, so userinfo is dropped here;
    the proxy desktopd points the container at must not need them.
    Without a proxy in the environment there are no extra flags.
    """
    proxy = ""
    for name in ("https_proxy", "HTTPS_PROXY", "http_proxy", "HTTP_PROXY"):
        value = os.environ.get(name, "").strip()
        if value:
            proxy = value
            break
    if not proxy:
        return []
    parsed = urlsplit(proxy)
    if parsed.scheme != "http" or not parsed.hostname:
        return []
    try:
        port = parsed.port
    except ValueError:
        return []
    host = parsed.hostname
    if port:
        host += ":%d" % port
    bypass = os.environ.get("no_proxy") or os.environ.get("NO_PROXY") or ""
    rules = [rule.strip() for rule in bypass.replace(";", ",").split(",") if rule.strip()]
    rules.append("<local>")
    return ["--proxy-server=http://%s" % host, "--proxy-bypass-list=%s" % ";".join(rules)]


def start_browser(profile, env, log, chrome_port, bot=False):
    env = dict(env)
    # /etc/chromium.d/zz-maclaw-shared-browser sends every other chromium
    # start to open_shared_browser. This is the start it must let through.
    env[SUPERVISED_BROWSER_ENV] = "1"
    log_signin(Path(profile).parent.name, "start: before browser", browser=False)
    spawn(browser_argv(profile, chrome_port, bot), env, log)
    # The launcher pid can exit. Only the process that still has this profile
    # open is the browser with the website login.
    wait_for(lambda: browser_pid(profile) and port_open(chrome_port), "chromium profile %s" % profile)


def keep_screen_awake(display):
    """Keep this display's framebuffer from going black.

    Xvfb blanks the picture after ten minutes with no input. Watching does
    not send input, so the desktop someone is looking at goes black, and
    taking over attaches to that same black frame. The window session can
    turn blanking back on after it starts, so the watcher repeats this.
    """
    if shutil.which("xset") is None:
        return
    env = os.environ.copy()
    env["DISPLAY"] = ":%d" % int(display)
    for argv in (
        ["xset", "s", "off"],
        ["xset", "s", "noblank"],
        ["xset", "s", "reset"],
        ["xset", "-dpms"],
    ):
        try:
            subprocess.call(argv, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=2)
        except (OSError, subprocess.TimeoutExpired):
            return


def session_alive(pids, display):
    """True while this desktop's X server and window session still run.

    A person can close the browser window, which ends Chromium but not the
    desktop. Starting the whole desktop again then would put a second XFCE
    session (panels, window manager) on the same display.
    """
    return (
        pid_alive(pids.get("xvfb"))
        and pid_alive(pids.get("fluxbox"))
        and Path("/tmp/.X11-unix/X%d" % display).exists()
    )


def write_dock_browser_entry(profile):
    """Desktop entry the Plank dock's browser launcher points to.

    Chromium started with --user-data-dir names its windows
    WM_CLASS "chromium (<profile>)", and bamf (Plank's window matcher) treats
    such a window as a separate app, so the dock showed a second browser icon
    instead of marking the launcher running. StartupWMClass with that exact
    instance joins them. The profile path is per user, so it is written here.
    """
    src = Path("/usr/share/applications/chromium.desktop")
    if not src.exists() or not shutil.which("plank"):
        return
    lines = []
    for line in src.read_text(encoding="utf-8", errors="replace").splitlines():
        if line.startswith("StartupWMClass="):
            continue
        lines.append(line)
        if line.strip() == "[Desktop Entry]":
            lines.append("StartupWMClass=chromium (%s)" % profile)
    target = Path("/home/desktop/.local/share/applications/maclaw-browser.desktop")
    target.parent.mkdir(parents=True, exist_ok=True)
    write_if_changed(target, "\n".join(lines) + "\n", 0o644)


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
    log_path = home / "desktop.log"
    log = open(log_path, "ab")
    env = desktop_env(display)
    try:
        write_dock_browser_entry(profile)
    except OSError:
        pass
    chrome_port = 18000 + display
    # What actually runs in this container, not only what pids.json says. An
    # earlier start that was cut off (desktopd restarted, the request was
    # dropped, Chromium was slow) leaves its X server, session and VNC running
    # without a record. Starting a second set beside them puts two sessions on
    # one display, and the new VNC cannot bind its ports, so the watcher kept
    # restarting the screen and noVNC reconnected every few seconds.
    pids = current_pids(key, display)
    if session_alive(pids, display):
        restart_browser(key, display, profile, env, log, chrome_port, pids)
        return
    # No usable session: whatever is left of the old one belongs to a display
    # that is about to be replaced, and it would hold the VNC ports.
    for name in ("proxy", "vgate", "vnc", "x11vnc", "fluxbox", "dbus", "xvfb"):
        kill_pid(pids.pop(name, None))
    clear_stale_display(display)
    # -s 0 disables the server's own ten-minute blank before any client connects.
    xvfb = spawn(["Xvfb", ":%d" % display, "-screen", "0", desktop_geometry(), "-ac", "+extension", "GLX", "+render", "-noreset", "-s", "0"], env, log)
    pids["xvfb"] = xvfb.pid
    write_pids(key, pids)
    wait_for(lambda: Path("/tmp/.X11-unix/X%d" % display).exists(), "display :%d" % display)
    keep_screen_awake(display)
    token = desktop_token_for(key)
    vnc = start_vnc(env, log, display, token) if token else None
    if vnc is not None:
        record_vnc(pids, vnc)
        write_pids(key, pids)
    session = session_argv()
    bus_pid = 0
    if session[0] == "dbus-launch":
        env.setdefault("XDG_SESSION_TYPE", "x11")
        env.setdefault("XDG_CURRENT_DESKTOP", "XFCE")
        # Chromium is started here, not by XFCE. It has to be on the same
        # session bus as XFCE and fcitx5: its GTK input-method module talks to
        # fcitx5 over D-Bus, so a browser outside the session bus cannot type
        # Chinese. Start the bus first and hand it to both.
        address, bus_pid = start_session_bus(env, log, display)
        if address:
            env["DBUS_SESSION_BUS_ADDRESS"] = address
            session = session[2:]
    # The pid key stays "fluxbox" so stop_desktop and pids.json written by
    # older supervisors keep the same meaning: the window session.
    flux = spawn(session, env, log)
    pids["fluxbox"] = flux.pid
    if bus_pid:
        pids["dbus"] = bus_pid
    # Recorded before the browser starts: a start that stops at the browser
    # still leaves a session the next ensure reuses instead of duplicating.
    write_pids(key, pids)
    start_browser(profile, env, log, chrome_port)
    try:
        restore_page_login(key)
    except (OSError, ValueError, TypeError):
        pass
    proxy = None
    if token:
        proxy = spawn([sys.executable, __file__, "gate", str(proxy_port(display)), str(chrome_port), token], env, log)
    pids["chromium"] = browser_pid(profile)
    if proxy is not None:
        pids["proxy"] = proxy.pid
        pids["proxy_code"] = supervisor_code()
    write_pids(key, pids)


def restart_browser(key, display, profile, env, log, chrome_port, pids):
    """Start only the browser on a desktop whose session is still running."""
    keep_screen_awake(display)
    bus = Path("/tmp/.maclaw-dbus-%d" % display)
    if pid_alive(pids.get("dbus")) and bus.exists():
        env["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=%s" % bus
    if shutil.which("startxfce4"):
        env.setdefault("XDG_SESSION_TYPE", "x11")
        env.setdefault("XDG_CURRENT_DESKTOP", "XFCE")
    start_browser(profile, env, log, chrome_port)
    try:
        restore_page_login(key)
    except (OSError, ValueError, TypeError):
        pass
    pids["chromium"] = browser_pid(profile)
    write_pids(key, pids)
    ensure_vnc(key, display)
    ensure_proxy(key, display)


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
    # "Allow Chromium sign-in" off (the policy written by install-browser does
    # the same where /etc is writable): with it on, each start signed the
    # person out of Google websites.
    signin = data.get("signin")
    if not isinstance(signin, dict):
        signin = {}
    signin["allowed"] = False
    signin["allowed_on_next_startup"] = False
    data["signin"] = signin
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


# Google keeps a sign-in in these cookies. Only how many are present is
# logged, never a value or a host, so a stop/start can be checked for whether
# the sign-in reached the disk and came back.
GOOGLE_SIGNIN_COOKIES = ("SID", "__Secure-1PSID", "__Secure-3PSID", "SAPISID", "__Secure-1PSIDTS", "__Secure-3PSIDTS")


def google_signin_on_disk(key):
    """How many Google sign-in cookies the profile's cookie file holds, or -1."""
    try:
        import sqlite3
        db = ROOT / key / "profile" / "Default" / "Cookies"
        if not db.exists():
            return 0
        # The running browser holds the file locked; immutable reads it anyway.
        conn = sqlite3.connect("file:%s?mode=ro&immutable=1" % quote(str(db)), uri=True, timeout=2)
        try:
            marks = ",".join("?" * len(GOOGLE_SIGNIN_COOKIES))
            row = conn.execute(
                "select count(*) from cookies where host_key in ('.google.com', 'google.com') and name in (%s)" % marks,
                GOOGLE_SIGNIN_COOKIES,
            ).fetchone()
        finally:
            conn.close()
        return int(row[0])
    except Exception:
        return -1


def google_signin_in_browser(key):
    """How many Google sign-in cookies the running browser has, or -1."""
    try:
        version = http_json("127.0.0.1", chrome_debug_port(key), "/json/version")
        with cdp_connection(version.get("webSocketDebuggerUrl") or "") as conn:
            result = conn.call("Storage.getCookies", {})
        cookies = result.get("cookies") if isinstance(result, dict) else None
        if not isinstance(cookies, list):
            return -1
        return sum(
            1 for c in cookies
            if isinstance(c, dict) and c.get("name") in GOOGLE_SIGNIN_COOKIES
            and str(c.get("domain") or "").lstrip(".") == "google.com"
        )
    except Exception:
        return -1


def log_signin(key, stage, browser=True):
    """One desktop.log line with sign-in cookie counts (no values, no hosts)."""
    line = "%s [maclaw] %s: google sign-in cookies browser=%s disk=%s\n" % (
        time.strftime("%Y-%m-%d %H:%M:%S"),
        stage,
        google_signin_in_browser(key) if browser else "-",
        google_signin_on_disk(key),
    )
    try:
        with open(ROOT / key / "desktop.log", "a", encoding="utf-8") as f:
            f.write(line)
    except OSError:
        pass


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
        # Chromium builds the webSocketDebuggerUrl it returns from this Host
        # header. Without the port it answered ws://127.0.0.1/devtools/...,
        # every CDP connection went to port 80 and was refused, and the
        # cookie and page saves before a stop silently did nothing.
        sock.sendall(("GET %s HTTP/1.1\r\nHost: %s:%d\r\nConnection: close\r\n\r\n" % (path, host, port)).encode("ascii"))
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
        log_signin(key, "stop: before quit")
        mark_flush_signaled(key)
        if not quit_browser(key, pid, profile):
            clear_flush_flag(key)
            return 0
    for _ in range(150):
        if not browser_pid(profile) and not pid_alive(pid):
            clear_flush_flag(key)
            if owned:
                log_signin(key, "stop: browser exited", browser=False)
            return 0
        # The first flush died before it could signal. This stop still has
        # to write the website login into the same profile.
        if not owned and flush_owner_gone(key):
            clear_flush_flag(key)
            if claim_flush(key):
                owned = True
                persist_website_login(key)
                capture_page_login(key)
                mark_flush_signaled(key)
                if not quit_browser(key, pid, profile):
                    clear_flush_flag(key)
                    return 0
        time.sleep(0.1)
    return 0


def quit_browser(key, pid, profile):
    """Quit Chromium the way a person quits it, so the cookie store is written.

    On SIGTERM Chromium ends as at a system logout (exit_type "SessionEnded"):
    preferences and tabs are saved, but the cookie database, which Chromium
    commits only every 30 seconds, is not. Cookies set or renewed shortly
    before the stop were lost, including the session cookies made persistent
    just above and sign-in cookies that sites such as Google keep rotating,
    and the next start came up signed out. Browser.close over CDP is a normal
    quit that writes them. SIGTERM stays the fallback.
    Returns False only when the browser could not be signaled at all.
    """
    asked = False
    try:
        version = http_json("127.0.0.1", chrome_debug_port(key), "/json/version")
        ws = version.get("webSocketDebuggerUrl") or ""
        if isinstance(ws, str) and ws.startswith("ws://"):
            with cdp_connection(ws) as conn:
                payload = json.dumps({"id": 1, "method": "Browser.close", "params": {}}).encode("utf-8")
                conn.sock.sendall(ws_client_frame(payload))
                asked = True
                try:
                    # The reply, or the socket closing as the browser exits.
                    ws_read(conn.sock, conn.pending)
                except (OSError, ValueError):
                    pass
    except (OSError, ValueError, TypeError, KeyError):
        pass
    if asked:
        for _ in range(100):
            if not browser_pid(profile) and not pid_alive(pid):
                return True
            time.sleep(0.1)
    try:
        os.kill(int(pid), 15)
    except (OSError, TypeError, ValueError):
        return False
    return True


SUPERVISED_BROWSER_ENV = "MACLAW_SUPERVISED_BROWSER"

# /usr/bin/chromium (Debian's launcher script) sources /etc/chromium.d/* before
# it starts the browser. Run as root without --no-sandbox Chromium exits at
# once, and with its default profile it would not be the agent's browser
# anyway. This hook sends every chromium start that is not the supervisor's
# own (panel launcher, application menu, xdg-open, x-www-browser,
# sensible-browser) to maclaw-browser, which joins the supervised browser.
CHROMIUM_HOOK = """# MaClaw desktop: written by /desktop_supervisor.py (install-browser).
# Every chromium start in this desktop joins the supervised browser that the
# agent drives over CDP: same profile, same login, one window list.
# The supervisor's own start sets MACLAW_SUPERVISED_BROWSER=1; an explicit
# --user-data-dir keeps a deliberate separate profile.
case " $* " in
  *" --user-data-dir"*) ;;
  *)
    if [ -z "${MACLAW_SUPERVISED_BROWSER:-}" ] && [ -n "${DISPLAY:-}" ] && [ -x /usr/bin/maclaw-browser ]; then
      exec /usr/bin/maclaw-browser "$@"
    fi
    ;;
esac
"""

SHARED_BROWSER_WRAPPER = """#!/bin/sh
# MaClaw desktop: open the desktop's one browser (the supervised Chromium the
# agent uses), focusing its window or opening the given URLs in it.
# Written by /desktop_supervisor.py (install-browser).
if [ -f /desktop_supervisor.py ]; then
  exec python3 /desktop_supervisor.py browser "$@"
fi
MACLAW_SUPERVISED_BROWSER=1 exec /usr/bin/chromium --no-sandbox "$@"
"""

BROWSER_SIGNIN_POLICY = '{"BrowserSignin": 0}\n'

# Fewer prompts and bubbles in front of what the agent works on. Downloads go
# to ~/Downloads of the desktop user without the save dialog; the person can
# still change a site's settings. "${home}" is a Chromium policy variable.
AGENT_BROWSER_POLICY = json.dumps({
    "TranslateEnabled": False,
    "PromptForDownloadLocation": False,
    "DownloadDirectory": "${home}/Downloads",
    "PasswordManagerEnabled": False,
    "AutofillAddressEnabled": False,
    "AutofillCreditCardEnabled": False,
    "DefaultNotificationsSetting": 2,
    "DefaultBrowserSettingEnabled": False,
    "PromotionalTabsEnabled": False,
}, indent=2, sort_keys=True) + "\n"

# xfce4-notifyd draws popups over the screen the agent reads. It is not
# started with the session, and if a program starts it over D-Bus it stays
# in do-not-disturb (notifications are still logged).
NOTIFYD_AUTOSTART_OFF = """[Desktop Entry]
Type=Application
Name=Notification Daemon
Exec=/usr/lib/x86_64-linux-gnu/xfce4/notifyd/xfce4-notifyd
Hidden=true
X-XFCE-Autostart-Override=true
"""
NOTIFYD_DND = """<?xml version="1.0" encoding="UTF-8"?>

<channel name="xfce4-notifyd" version="1.0">
  <property name="do-not-disturb" type="bool" value="true"/>
  <property name="notification-log" type="bool" value="true"/>
</channel>
"""

BROWSER_MIME_TYPES = (
    "text/html",
    "application/xhtml+xml",
    "x-scheme-handler/http",
    "x-scheme-handler/https",
    "x-scheme-handler/about",
    "x-scheme-handler/unknown",
)


def write_if_changed(path, text, mode):
    path = Path(path)
    try:
        if path.exists() and path.read_text(encoding="utf-8") == text:
            if (path.stat().st_mode & 0o777) != mode:
                os.chmod(str(path), mode)
            return False
        path.parent.mkdir(parents=True, exist_ok=True)
        temporary = path.with_name("." + path.name + ".tmp")
        temporary.write_text(text, encoding="utf-8")
        os.chmod(str(temporary), mode)
        os.replace(str(temporary), str(path))
        return True
    except OSError:
        return False


def set_ini_value(path, section, key, value):
    """Set key=value in an ini-style file, keeping every other line."""
    path = Path(path)
    try:
        lines = path.read_text(encoding="utf-8").splitlines() if path.exists() else []
    except OSError:
        return False
    out, current, done, seen_section = [], None, False, section is None
    for line in lines:
        stripped = line.strip()
        if stripped.startswith("[") and stripped.endswith("]"):
            if current == section and not done:
                out.append("%s=%s" % (key, value))
                done = True
            current = stripped[1:-1]
            seen_section = seen_section or current == section
        elif current == section and stripped.split("=", 1)[0].strip() == key:
            if not done:
                out.append("%s=%s" % (key, value))
                done = True
            continue
        out.append(line)
    if not done:
        if not seen_section and section is not None:
            out.append("[%s]" % section)
        out.append("%s=%s" % (key, value))
    return write_if_changed(path, "\n".join(out) + "\n", 0o644)


def install_browser_integration(root=Path("/")):
    """Make the desktop's browser entries open the supervised browser.

    Idempotent and best effort: the image build runs it, and ensure() runs it
    again so containers created from an older image pick it up with the next
    supervisor copy. maclaw-gui:1 has no XFCE; the XFCE parts are skipped.
    """
    root = Path(root)
    write_if_changed(root / "usr/bin/maclaw-browser", SHARED_BROWSER_WRAPPER, 0o755)
    if (root / "etc/chromium.d").is_dir() or (root / "usr/bin/chromium").exists():
        write_if_changed(root / "etc/chromium.d/zz-maclaw-shared-browser", CHROMIUM_HOOK, 0o644)
        # Chromium without Google's API keys still offers "Allow Chromium
        # sign-in". With it on, every browser start signs the person out of
        # Google websites (Gmail, YouTube, accounts.google.com show "Signed
        # out") although the cookies were kept; other sites are not affected.
        # Turning browser sign-in off keeps the website login. Logging in to
        # Google websites still works.
        write_if_changed(root / "etc/chromium/policies/managed/maclaw-browser-signin.json", BROWSER_SIGNIN_POLICY, 0o644)
        write_if_changed(root / "etc/chromium/policies/managed/maclaw-agent.json", AGENT_BROWSER_POLICY, 0o644)
    if (root / "etc/xdg/autostart/xfce4-notifyd.desktop").exists():
        # Overrides the package entry by name from the user's autostart
        # directory, which XFCE reads first; the package file stays untouched.
        write_if_changed(root / "etc/skel/.config/autostart/xfce4-notifyd.desktop", NOTIFYD_AUTOSTART_OFF, 0o644)
        write_if_changed(root / "home/desktop/.config/autostart/xfce4-notifyd.desktop", NOTIFYD_AUTOSTART_OFF, 0o644)
        write_if_changed(root / "etc/xdg/xfce4/xfconf/xfce-perchannel-xml/xfce4-notifyd.xml", NOTIFYD_DND, 0o644)
    # XFCE "Preferred Applications": the panel's Web Browser launcher and
    # exo-open/xdg-open use this helper. chromium.desktop runs /usr/bin/chromium,
    # which the hook above routes to the shared browser.
    helpers = root / "etc/xdg/xfce4/helpers.rc"
    if helpers.exists():
        set_ini_value(helpers, None, "WebBrowser", "chromium")
    # xdg-mime / xdg-settings and GLib apps.
    for mimeapps in (root / "etc/xdg/mimeapps.list", root / "usr/share/applications/mimeapps.list"):
        if not (root / "usr/share/applications/chromium.desktop").exists():
            break
        for mime in BROWSER_MIME_TYPES:
            set_ini_value(mimeapps, "Default Applications", mime, "chromium.desktop")


def shared_browser_key(env=None):
    """The user key whose desktop this process runs on, or ""."""
    env = os.environ if env is None else env
    raw = env.get("DISPLAY", "")
    try:
        display = int(raw.split(":", 1)[1].split(".", 1)[0])
    except (IndexError, ValueError):
        display = 0
    users = load_registry().get("users", {})
    owner = env.get("MACLAW_DESKTOP_KEY", "").strip()
    # A bot display's browser is that bot's profile, never the owner's.
    if display and display != DISPLAY_MIN:
        for key, entry in users.items():
            try:
                if valid_key(key) and int(entry.get("display") or 0) == display:
                    return key
            except (TypeError, ValueError):
                continue
    if valid_key(owner) and owner in users:
        return owner
    for key, entry in users.items():
        try:
            if valid_key(key) and display and int(entry.get("display") or 0) == display:
                return key
        except (TypeError, ValueError):
            continue
    return ""


def browser_windows(env):
    """The shared browser's top-level windows, topmost first.

    Minimized windows count: they are not "visible" to xdotool, yet they are
    what the person expects the panel launcher to bring back. The window
    manager's client list tells real windows from Chromium's hidden helpers.
    """
    def run(args):
        return subprocess.run(args, env=env, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                              timeout=5).stdout.decode("ascii", "ignore")

    chromium = [int(w) for w in run(["xdotool", "search", "--class", "chromium"]).split() if w.isdigit()]
    try:
        stacking = [int(w, 16) for w in re.findall(r"0x[0-9a-fA-F]+", run(["xprop", "-root", "_NET_CLIENT_LIST_STACKING"]))]
    except (OSError, subprocess.SubprocessError):
        stacking = []
    if stacking:
        return [w for w in reversed(stacking) if w in chromium]
    return [int(w) for w in run(["xdotool", "search", "--onlyvisible", "--class", "chromium"]).split() if w.isdigit()]


def activate_browser_window(display, wait_seconds=3.0):
    """Raise the shared browser's window. False when there is none to raise."""
    if not shutil.which("xdotool"):
        return False
    env = os.environ.copy()
    env["DISPLAY"] = ":%d" % display
    deadline = time.time() + wait_seconds
    while True:
        try:
            windows = browser_windows(env)
        except (OSError, subprocess.SubprocessError):
            return False
        for window in windows:
            try:
                done = subprocess.run(["xdotool", "windowactivate", str(window)], env=env,
                                      stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
            except (OSError, subprocess.SubprocessError):
                continue
            if done.returncode == 0:
                return True
        if time.time() >= deadline:
            return False
        time.sleep(0.3)


def open_shared_browser(args):
    """Panel launcher, menu, xdg-open: use the desktop's one browser.

    The supervised Chromium has the agent's CDP port and the website login.
    When it runs, a start with the same --user-data-dir only hands the URLs
    to it (Chromium's process singleton) and exits; with no URL its window is
    raised. When the person closed it, it is started again with the
    supervisor's flags, so CDP comes back too.
    """
    key = shared_browser_key()
    if not key:
        print("maclaw-browser: this display has no MaClaw desktop", file=sys.stderr)
        return 1
    entry = load_registry().get("users", {}).get(key) or {}
    display = int(entry.get("display") or DISPLAY_MIN)
    profile = ROOT / key / "profile"
    started = False
    if not browser_pid(profile):
        with desktop_lock():
            ensure(key)
        started = True
    urls = [arg for arg in args if arg and not arg.startswith("-")]
    if not urls and activate_browser_window(display, 5.0 if started else 1.0):
        return 0
    if started and not urls:
        return 0
    env = os.environ.copy()
    env[SUPERVISED_BROWSER_ENV] = "1"
    argv = [browser_bin(), "--no-sandbox", "--user-data-dir=%s" % profile, "--profile-directory=Default"] + list(args)
    os.execvpe(argv[0], argv, env)
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
        text = (PROC / str(pid) / "stat").read_text(encoding="utf-8")
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
    """The pids recorded for this desktop since the container last started.

    pids.json lives on the desktop volume and outlasts docker stop. After the
    next docker start its numbers belong to other processes, so a file written
    before the restart (its "boot" differs) records nothing. Files from older
    supervisors have no "boot" and are still read.
    """
    pid_file = ROOT / key / "pids.json"
    try:
        data = json.loads(pid_file.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}
    if not isinstance(data, dict):
        return {}
    boot = data.pop("boot", None)
    if boot is not None and boot != container_boot():
        return {}
    return data


def write_pids(key, pids):
    home = ROOT / key
    home.mkdir(mode=0o700, exist_ok=True)
    data = {name: pid for name, pid in pids.items() if name != "boot" and pid}
    boot = container_boot()
    if boot:
        data["boot"] = boot
    tmp = home / "pids.json.tmp"
    tmp.write_text(json.dumps(data), encoding="utf-8")
    os.replace(str(tmp), str(home / "pids.json"))


def proc_stat_fields(pid):
    """Fields of /proc/<pid>/stat after "(comm)": state, ppid, ..."""
    try:
        text = (PROC / str(int(pid)) / "stat").read_text(encoding="utf-8")
    except (OSError, TypeError, ValueError):
        return []
    end = text.rfind(")")
    return text[end + 2:].split() if end >= 0 else []


def container_boot():
    """When this container started: the start time of its pid 1.

    It changes with every docker start, so it tells pids recorded in this run
    of the container from numbers left over from an earlier one.
    """
    fields = proc_stat_fields(1)
    # starttime is field 22 of stat, the 20th after "(comm)".
    return "b" + fields[19] if len(fields) > 19 else ""


def proc_argv(pid):
    try:
        raw = (PROC / str(int(pid)) / "cmdline").read_bytes()
    except (OSError, TypeError, ValueError):
        return []
    return [part.decode("utf-8", "replace") for part in raw.split(b"\0") if part]


def proc_on_display(pid, display):
    try:
        raw = (PROC / str(int(pid)) / "environ").read_bytes()
    except (OSError, TypeError, ValueError):
        return False
    return ("DISPLAY=:%d" % display).encode() in raw.split(b"\0")


# Desktop parts found by command line. Chromium is found by its profile.
DESKTOP_PARTS = ("xvfb", "fluxbox", "dbus", "x11vnc", "vnc", "vgate", "proxy", "watch", "fcitx", "panel", "panelbus")


def desktop_part(pid, key, display):
    """Which part of this desktop the process is, or ""."""
    argv = proc_argv(pid)
    if not argv:
        return ""
    names = [os.path.basename(arg) for arg in argv[:2]]
    screen = ":%d" % display
    if names[0] == "Xvfb":
        return "xvfb" if len(argv) > 1 and argv[1] == screen else ""
    if names[0] == "x11vnc":
        return "x11vnc" if screen in argv else ""
    if names[0] == "dbus-daemon" and display != DISPLAY_MIN and "--address=unix:path=%s" % bot_panel_bus(display) in argv:
        return "panelbus"
    if names[0] == "dbus-daemon":
        return "dbus" if "--address=unix:path=/tmp/.maclaw-dbus-%d" % display in argv else ""
    if names[0] in ("xfce4-session", "fluxbox") or "startxfce4" in names:
        return "fluxbox" if proc_on_display(pid, display) else ""
    if names[0] == "xfwm4" and display != DISPLAY_MIN:
        # A bot display runs only the window manager; the pid key stays
        # "fluxbox" (the window session) like the owner's desktop.
        return "fluxbox" if proc_on_display(pid, display) else ""
    if names[0] in ("xfce4-panel", BOT_DOCK_NAME) and display != DISPLAY_MIN:
        return "panel" if proc_on_display(pid, display) else ""
    if names[0] == "fcitx5" and display != DISPLAY_MIN:
        return "fcitx" if proc_on_display(pid, display) else ""
    if "websockify" in names:
        return "vnc" if "127.0.0.1:%d" % vnc_ws_port(display) in argv else ""
    if len(argv) >= 4 and names[1] == "desktop_supervisor.py":
        # The two gates belong to the owner's display. A bot display reuses
        # them by token, so stopping a bot must never adopt or kill them.
        if argv[2] == "gate" and argv[3] == str(VNC_GATE_PORT):
            return "vgate" if display == DISPLAY_MIN else ""
        if argv[2] == "gate" and argv[3] == str(proxy_port(display)):
            return "proxy" if display == DISPLAY_MIN else ""
        if argv[2] == "watch" and argv[3] == key:
            return "watch"
    return ""


def find_desktop_parts(key, display):
    """The running process of each desktop part, found by command line.

    A part can have several processes (websockify forks one per connection,
    startxfce4 runs xfce4-session); the outermost, oldest one is the part.
    """
    found = {}
    if not PROC.is_dir():
        return {}
    me = os.getpid()
    for entry in PROC.iterdir():
        if not entry.name.isdigit():
            continue
        pid = int(entry.name)
        if pid in (1, me) or not pid_alive(pid):
            continue
        part = desktop_part(pid, key, display)
        if part:
            found.setdefault(part, []).append(pid)
    parts = {}
    for part, pids in found.items():
        outer = [pid for pid in pids if proc_ppid(pid) not in pids] or pids
        outer.sort(key=proc_start_time)
        parts[part] = outer[0]
    return parts


def proc_ppid(pid):
    fields = proc_stat_fields(pid)
    try:
        return int(fields[1])
    except (IndexError, ValueError):
        return 0


def proc_start_time(pid):
    fields = proc_stat_fields(pid)
    try:
        return int(fields[19])
    except (IndexError, ValueError):
        return 0


def current_pids(key, display):
    """pids.json brought in line with the processes that really run.

    A recorded pid that is gone or is now another process is replaced by the
    running part, and a running part nobody recorded is adopted. The file is
    rewritten when anything changed.
    """
    pids = read_pids(key)
    found = None
    changed = False
    for part in DESKTOP_PARTS:
        pid = pids.get(part)
        if pid and pid_alive(pid) and desktop_part(pid, key, display) == part:
            continue
        if found is None:
            found = find_desktop_parts(key, display)
        if part in found:
            pids[part] = found[part]
            changed = True
        elif part in pids:
            del pids[part]
            changed = True
    browser = browser_pid(ROOT / key / "profile")
    if browser and pids.get("chromium") != browser:
        pids["chromium"] = browser
        changed = True
    if changed:
        write_pids(key, pids)
    return pids


def reap_children():
    """Collect exited children so they do not stay behind as zombies."""
    while True:
        try:
            pid, _ = os.waitpid(-1, os.WNOHANG)
        except ChildProcessError:
            return
        except OSError:
            return
        if pid == 0:
            return


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
    pids = current_pids(key, display)
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
    pids["proxy_code"] = supervisor_code()
    write_pids(key, pids)


def ensure_vnc(key, display):
    """Bring noVNC back without restarting the browser.

    The screen capture and the web client are a pair. A live websockify whose
    x11vnc has exited still looks healthy and leaves the login view black.
    """
    if shutil.which("x11vnc") is None or shutil.which("websockify") is None:
        return
    # Found by command line, so VNC processes nobody recorded are seen too.
    # Restarting beside them failed to bind the ports and dropped the view
    # every few seconds.
    pids = current_pids(key, display)
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
    record_vnc(pids, pair)
    write_pids(key, pids)


def record_vnc(pids, pair):
    for name, proc in zip(("x11vnc", "vnc", "vgate"), pair):
        if proc is not None:
            pids[name] = proc.pid
        else:
            pids.pop(name, None)
    if pids.get("vgate"):
        pids["vgate_code"] = supervisor_code()


def vnc_pair_alive(pids):
    # Process liveness only. Opening the VNC port here drops the person's
    # login connection, and one refused probe used to restart a healthy view.
    return pid_alive(pids.get("x11vnc")) and pid_alive(pids.get("vnc")) and pid_alive(pids.get("vgate"))


def kill_pid(pid):
    try:
        pid = int(pid or 0)
    except (TypeError, ValueError):
        return
    if pid <= 0 or not (PROC / str(pid)).exists():
        return
    try:
        os.kill(pid, 15)
    except OSError:
        return
    for _ in range(20):
        if not (PROC / str(pid)).exists():
            return
        time.sleep(0.05)
    try:
        os.kill(pid, 9)
    except OSError:
        pass


def ensure_watch(key, display):
    """Keep the login view up while the browser is still running.

    A watcher keeps running the supervisor it was started with. After a
    deploy copied in a new desktop_supervisor.py, the old watcher would go on
    restarting VNC by its old rules, so it is replaced once by one that runs
    the current file.
    """
    pids = current_pids(key, display)
    code = supervisor_code()
    if pid_alive(pids.get("watch")) and pids.get("watch_code") == code:
        return
    kill_pid(pids.get("watch"))
    home = ROOT / key
    home.mkdir(mode=0o700, exist_ok=True)
    log = open(home / "desktop.log", "ab")
    proc = spawn([sys.executable, __file__, "watch", key, str(display)], os.environ.copy(), log)
    pids["watch"] = proc.pid
    pids["watch_code"] = code
    write_pids(key, pids)


def supervisor_code():
    """Short hash of this supervisor file, to tell old watchers from new."""
    try:
        return "c" + hashlib.sha256(Path(__file__).read_bytes()).hexdigest()[:16]
    except OSError:
        return ""


def watch_vnc(key, display):
    if not valid_key(key):
        return 2
    if is_bot_key(key):
        return watch_bot(key, display)
    started = time.time()
    checks = [20, 90]
    while desktop_running(key):
        # Whether a Google sign-in that came back from disk is still there
        # once the restored pages have talked to Google.
        if checks and time.time() - started >= checks[0]:
            log_signin(key, "running %ds" % checks.pop(0))
        lock_file = open(ROOT / "lock", "a", encoding="utf-8")
        try:
            fcntl.flock(lock_file, fcntl.LOCK_EX)
            if desktop_running(key):
                ensure_vnc(key, display)
                ensure_proxy(key, display)
                keep_screen_awake(display)
        finally:
            fcntl.flock(lock_file, fcntl.LOCK_UN)
            lock_file.close()
        # VNC parts this watcher restarted are its children.
        reap_children()
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
    # Bot displays have their own idle stop and cap (evict_bots).
    running = [key for key in users if key != current and not is_bot_key(key) and desktop_running(key)]
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
    if not pid_file.exists():
        return
    # Only pids from this run of the container: older numbers are now other
    # processes.
    pids = read_pids(key)
    # The idle stop runs inside this display's watcher: signaling "watch"
    # ends this very process, so it goes last or the parts after it in
    # pids.json (dock, its bus, websockify) were left running.
    for name, pid in sorted(pids.items(), key=lambda item: item[0] == "watch"):
        if not isinstance(pid, int):
            continue
        # The browser was already asked to exit. Signaling it again, or
        # closing its display first, aborts the cookie write.
        if name in ("chromium", "xvfb", "fluxbox", "dbus") and not browser_gone:
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


def pipe(left, right, watch=None):
    try:
        while True:
            data = left.recv(65536)
            if not data:
                break
            if watch is not None:
                watch.feed(data)
            right.sendall(data)
    except OSError:
        pass
    # shutdown() before close(): the opposite pipe thread is blocked in
    # recv() on these sockets, and a bare close() from this thread neither
    # wakes it nor sends FIN while that recv holds the file. websockify then
    # never sees the viewer leave and its per-connection child lives forever
    # (seen live: 16 children, one real viewer).
    for item in (left, right):
        try:
            item.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass
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
        reap_children()
        try:
            head = read_request_head(client)
            route = None
            if head is not None:
                if request_authorized(head, token):
                    route = (target_port, "")
                else:
                    route = bot_gate_route(listen_port, request_bearer(head))
            if route is None:
                client.close()
                continue
            target, bot = route
            remote = None
            for _ in range(40):
                try:
                    remote = socket.create_connection(("127.0.0.1", target), 1)
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
            watch = None
            if bot:
                touch_bot_file(bot, "activity")
                watch = BotTraffic(bot, sniff_input=listen_port == VNC_GATE_PORT)
            threading.Thread(target=pipe, args=(client, remote, watch), daemon=True).start()
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


def request_bearer(head):
    """The Bearer credential of an HTTP request head, or ""."""
    try:
        text = head.decode("latin-1")
    except UnicodeDecodeError:
        return ""
    for line in text.split("\n")[1:]:
        if line.strip() == "":
            break
        name, _, value = line.partition(":")
        if name.strip().lower() != "authorization":
            continue
        scheme, _, credential = value.strip().partition(" ")
        return credential.strip() if scheme.lower() == "bearer" else ""
    return ""


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



# ---------------------------------------------------------------------------
# Per-bot displays (desktop_mode=isolated).
#
# ensure <owner+bot> starts a light display for one bot: Xvfb, a session bus,
# xfwm4 and that bot's own Chromium profile with the memory-saving flags. The
# person watches it through the owner's noVNC gate (6080) with the bot's
# token; x11vnc and websockify for that display start only on the first
# view. The bot's CDP goes through the owner's CDP gate (19020) the same way.
# A bot display stops after MACLAW_BOT_IDLE_SECONDS without traffic, never
# while the person has it (takeover), and at most MACLAW_DESKTOP_MAX_BOTS run
# at once (least recently used first, a busy one never).


def touch_bot_file(key, name, force=False):
    path = ROOT / key / name
    try:
        if not force and time.time() - path.stat().st_mtime < 2:
            return
    except OSError:
        pass
    try:
        path.parent.mkdir(mode=0o700, exist_ok=True)
        path.touch()
        os.utime(path, None)
    except OSError:
        pass


def bot_file_age(key, name):
    try:
        return max(0.0, time.time() - (ROOT / key / name).stat().st_mtime)
    except OSError:
        return None


def takeover_state(key):
    try:
        data = json.loads((ROOT / key / "takeover.json").read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None
    return data if isinstance(data, dict) else None


def bot_last_activity(key):
    entry = (load_registry().get("users", {}) or {}).get(key) or {}
    last = float(entry.get("last_used") or 0)
    for name in ("activity", "human_input"):
        try:
            last = max(last, (ROOT / key / name).stat().st_mtime)
        except OSError:
            pass
    return last


def bot_busy(key):
    """A bot display that must not be evicted: the person has it, or the bot
    used it within BOT_BUSY_SECONDS (a running task)."""
    if takeover_state(key) is not None:
        return True
    return time.time() - bot_last_activity(key) < BOT_BUSY_SECONDS


def running_bot_keys():
    users = load_registry().get("users", {}) or {}
    return [key for key in users if is_bot_key(key) and desktop_running(key)]


def ensure_bot(key):
    registry = load_registry()
    users = registry.setdefault("users", {})
    entry = users.get(key)
    try:
        display = int((entry or {}).get("display") or 0)
    except (TypeError, ValueError):
        display = 0
    if entry is None or not (DISPLAY_MIN < display <= DISPLAY_MAX):
        used = {DISPLAY_MIN}
        for other, item in users.items():
            if other != key:
                try:
                    used.add(int(item.get("display") or 0))
                except (TypeError, ValueError):
                    pass
        free = [d for d in range(DISPLAY_MIN + 1, DISPLAY_MAX + 1) if d not in used]
        if not free:
            raise SystemExit("no free desktop display")
        entry = dict(entry or {})
        entry["display"] = free[0]
        users[key] = entry
    display = int(entry["display"])
    entry["bot"] = True
    entry["last_used"] = int(time.time())
    token = entry.get("token")
    if not token or not valid_key(token):
        entry["token"] = secrets.token_hex(16)
    token = entry["token"]
    save_registry(registry)
    ensure_gates()
    if not desktop_running(key):
        evict_bots(key)
        start_bot_desktop(key, display)
    else:
        ensure_bot_panel(key, display)
    ensure_watch(key, display)
    touch_bot_file(key, "activity", force=True)
    return {"display": ":%d" % display, "cdp_port": proxy_port(DISPLAY_MIN), "token": token}


def evict_bots(current):
    cap = env_int("MACLAW_DESKTOP_MAX_BOTS", MAX_BOTS_RUNNING)
    running = [key for key in running_bot_keys() if key != current]
    while len(running) + 1 > cap:
        idle = [key for key in running if not bot_busy(key)]
        if not idle:
            # Every other bot is working or with the person: run over the cap
            # rather than kill a task.
            return
        victim = min(idle, key=bot_last_activity)
        stop_desktop(victim)
        running.remove(victim)


def start_bot_desktop(key, display):
    home = ROOT / key
    home.mkdir(mode=0o700, exist_ok=True)
    profile = home / "profile"
    profile.mkdir(mode=0o700, exist_ok=True)
    clear_flush_flag(key)
    release_profile_lock(profile)
    keep_website_login(profile)
    log = open(home / "desktop.log", "ab")
    env = desktop_env(display)
    chrome_port = 18000 + display
    pids = current_pids(key, display)
    if not (pid_alive(pids.get("xvfb")) and Path("/tmp/.X11-unix/X%d" % display).exists()):
        for name in ("vnc", "x11vnc", "fcitx", "panel", "panelbus", "fluxbox", "dbus", "xvfb"):
            kill_pid(pids.pop(name, None))
        clear_stale_display(display)
        xvfb = spawn(["Xvfb", ":%d" % display, "-screen", "0", desktop_geometry(), "-ac", "+extension", "GLX", "+render", "-noreset", "-s", "0"], env, log)
        pids["xvfb"] = xvfb.pid
        write_pids(key, pids)
        wait_for(lambda: Path("/tmp/.X11-unix/X%d" % display).exists(), "display :%d" % display)
    keep_screen_awake(display)
    bus = Path("/tmp/.maclaw-dbus-%d" % display)
    if pid_alive(pids.get("dbus")) and bus.exists():
        env["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=%s" % bus
    else:
        kill_pid(pids.pop("dbus", None))
        address, bus_pid = start_session_bus(env, log, display)
        if address:
            env["DBUS_SESSION_BUS_ADDRESS"] = address
            pids["dbus"] = bus_pid
    if not pid_alive(pids.get("fluxbox")):
        wm = ["xfwm4", "--compositor=off"] if shutil.which("xfwm4") else ["fluxbox"]
        pids["fluxbox"] = spawn(wm, env, log).pid
    write_pids(key, pids)
    ensure_bot_panel(key, display, pids, log)
    start_browser(profile, env, log, chrome_port, bot=True)
    try:
        restore_page_login(key)
    except (OSError, ValueError, TypeError):
        pass
    pids["chromium"] = browser_pid(profile)
    write_pids(key, pids)


# A bot display has no XFCE session: without a wallpaper and a window list a
# minimized Chromium leaves a black screen with no way back. Each bot display
# gets the theme wallpaper (feh sets the root pixmap once and exits) and a
# light bottom xfce4-panel with only a tasklist. The panel runs on its own
# private D-Bus with its own XDG config dir, so its xfconfd reads this panel
# file, not the owner's ~/.config (shared by every display) and not the
# xfconfd that xfwm4 already started on the bot's session bus. It sets a
# strut, so a maximized Chromium ends above it. NO_AT_BRIDGE keeps it out of
# the AT-SPI tree the agent reads. About 13 MB PSS per display.

BOT_PANEL_SIZE = 30
BOT_PANEL_RETRY_SECONDS = 60
BOT_WALLPAPERS = {
    "soft25": "maclaw-soft-25.jpg", "soft40": "maclaw-soft-40.jpg", "soft55": "maclaw-soft-55.jpg",
    "soft-topbar": "maclaw-soft-40.jpg", "dark": "maclaw-dark.jpg", "colorful": "maclaw-colorful.jpg",
    "topbar": "maclaw-colorful.jpg", "clean": "gray-gradient.png",
}
BOT_PANEL_XML = """<?xml version="1.0" encoding="UTF-8"?>
<channel name="xfce4-panel" version="1.0">
  <property name="configver" type="int" value="2"/>
  <property name="panels" type="array">
    <value type="int" value="1"/>
    <property name="dark-mode" type="bool" value="false"/>
    <property name="panel-1" type="empty">
      <property name="position" type="string" value="p=10;x=0;y=0"/>
      <property name="length" type="uint" value="100"/>
      <property name="position-locked" type="bool" value="true"/>
      <property name="size" type="uint" value="%d"/>
      <property name="icon-size" type="uint" value="16"/>
      <property name="background-style" type="uint" value="1"/>
      <property name="background-rgba" type="array">
        <value type="double" value="0.97"/><value type="double" value="0.97"/>
        <value type="double" value="0.98"/><value type="double" value="0.94"/>
      </property>
      <property name="plugin-ids" type="array"><value type="int" value="1"/></property>
    </property>
  </property>
  <property name="plugins" type="empty">
    <property name="plugin-1" type="string" value="tasklist">
      <property name="flat-buttons" type="bool" value="true"/>
      <property name="show-handle" type="bool" value="false"/>
      <property name="grouping" type="uint" value="0"/>
      <property name="show-labels" type="bool" value="true"/>
    </property>
  </property>
</channel>
""" % BOT_PANEL_SIZE
BOT_PANEL_CSS = """.xfce4-panel, .xfce4-panel label, .xfce4-panel button, .xfce4-panel button label,
#tasklist-button label, .tasklist button label { color: #23262b; }
.xfce4-panel button:checked label, .tasklist button:checked label { color: #000000; font-weight: bold; }
.xfce4-panel { border-top: 1px solid rgba(0,0,0,0.10); }
"""
_bot_panel_tried = {}


def bot_panel_bus(display):
    return "/tmp/.maclaw-panelbus-%d" % int(display)


def bot_wallpaper():
    try:
        theme = Path("/etc/maclaw/theme").read_text(encoding="utf-8").strip()
    except OSError:
        theme = ""
    base = Path("/usr/share/backgrounds/maclaw")
    for name in (BOT_WALLPAPERS.get(theme), BOT_WALLPAPERS["soft25"]):
        if name and (base / name).is_file():
            return str(base / name)
    return ""


def set_bot_wallpaper(display, log):
    env = os.environ.copy()
    env["DISPLAY"] = ":%d" % int(display)
    wall = bot_wallpaper()
    if wall and shutil.which("feh"):
        argv = ["feh", "--no-fehbg", "--bg-fill", wall]
    elif shutil.which("xsetroot"):
        argv = ["xsetroot", "-solid", "#dde1ea"]
    else:
        return
    try:
        subprocess.call(argv, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=10)
    except (OSError, subprocess.TimeoutExpired):
        pass


BOT_DOCK_NAME = "maclaw-botdock"
BOT_DOCK_SETTINGS = """[net/launchpad/plank/docks/dock1]
position='bottom'
alignment='center'
hide-mode='none'
icon-size=40
zoom-enabled=false
theme='MaClaw-Bot'
show-dock-item=false
lock-items=true
pressure-reveal=false
dock-items=[%s]
"""
# MaClaw-Light without transparency: a bot display has no compositor (picom
# alone would cost ~27 MB), and Plank's translucent fill is drawn over black
# there, which showed a gray box. Same light colours, opaque, square.
BOT_DOCK_THEME = """# MaClaw bot dock (MaClaw-Light, opaque). Written by /desktop_supervisor.py.
[PlankTheme]
TopRoundness=0
BottomRoundness=0
LineWidth=1
OuterStrokeColor=200;;203;;210;;255
FillStartColor=250;;250;;252;;255
FillEndColor=240;;241;;245;;255
InnerStrokeColor=255;;255;;255;;255

[PlankDockTheme]
HorizPadding=6
TopPadding=2
BottomPadding=3
ItemPadding=8
IndicatorSize=4
IconShadowSize=0
UrgentBounceHeight=0
LaunchBounceHeight=0
FadeOpacity=1
ClickTime=120
UrgentBounceTime=0
LaunchBounceTime=0
ActiveTime=150
SlideTime=150
FadeTime=150
HideTime=150
GlowSize=0
GlowTime=0
GlowPulseTime=2000
"""
# (dock item, Name, Icon, StartupWMClass, command). {profile} and {lo} are
# this bot's Chromium profile and LibreOffice user dir. A launcher is shown
# only when its program is installed.
BOT_DOCK_APPS = (
    ("browser", "Chromium", "chromium", "chromium ({profile})", ["/usr/bin/maclaw-browser"], "/usr/bin/maclaw-browser"),
    ("files", "文件管理器", "org.xfce.thunar", "Thunar", ["thunar"], "thunar"),
    ("terminal", "终端", "org.xfce.terminal", "Xfce4-terminal", ["xfce4-terminal"], "xfce4-terminal"),
    ("writer", "LibreOffice Writer", "libreoffice-writer", "libreoffice-writer", ["libreoffice", "--writer", "-env:UserInstallation=file://{lo}"], "/usr/share/applications/libreoffice-writer.desktop"),
    ("calc", "LibreOffice Calc", "libreoffice-calc", "libreoffice-calc", ["libreoffice", "--calc", "-env:UserInstallation=file://{lo}"], "/usr/share/applications/libreoffice-calc.desktop"),
    ("impress", "LibreOffice Impress", "libreoffice-impress", "libreoffice-impress", ["libreoffice", "--impress", "-env:UserInstallation=file://{lo}"], "/usr/share/applications/libreoffice-impress.desktop"),
)


BOT_DOCK_IDS = {'browser': 'maclaw-bot-browser', 'files': 'thunar', 'terminal': 'xfce4-terminal', 'writer': 'libreoffice-writer', 'calc': 'libreoffice-calc', 'impress': 'libreoffice-impress'}


def bot_app_env_script(display):
    """Launcher wrapper: what the dock starts runs like the bot's Chromium.

    The dock itself runs on a private bus, with its own config dir and
    without the accessibility bridge. Programs started from it must not
    inherit that: they join the bot display's session bus (one Thunar or
    terminal per bot display, not the owner's), use the desktop user's
    config and stay readable over AT-SPI.
    """
    env = desktop_env(display)
    lines = ["#!/bin/sh", "# Written by /desktop_supervisor.py: dock launcher for bot display :%d." % display,
             "unset NO_AT_BRIDGE GSETTINGS_BACKEND XDG_CONFIG_DIRS SESSION_MANAGER"]
    for name in ("HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "PATH", "DISPLAY", "QT_ACCESSIBILITY", "QT_LINUX_ACCESSIBILITY_ALWAYS_ON"):
        lines.append("export %s=%s" % (name, shlex.quote(env[name])))
    lines.append("export DBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/.maclaw-dbus-%d" % display)
    lines.append("export XDG_SESSION_TYPE=x11 XDG_CURRENT_DESKTOP=XFCE")
    lines.append('exec "$@"')
    return "\n".join(lines) + "\n"


def write_bot_dock(key, display, cfg):
    """Settings, launchers and desktop entries of one bot display's dock."""
    profile = ROOT / key / "profile"
    lo = ROOT / key / "libreoffice"
    run = cfg / "run"
    write_if_changed(run, bot_app_env_script(display), 0o700)
    apps = cfg / "data" / "applications"
    launchers = cfg / "plank" / "dock1" / "launchers"
    items = []
    for item, name, icon, wmclass, argv, needs in BOT_DOCK_APPS:
        if not (os.path.exists(needs) or shutil.which(needs)):
            continue
        cmd = " ".join(shlex.quote(part.format(profile=profile, lo=lo)) for part in [str(run)] + argv)
        desktop = "\n".join([
            "[Desktop Entry]", "Type=Application", "Name=%s" % name, "Icon=%s" % icon,
            "Exec=%s" % cmd, "StartupWMClass=%s" % wmclass.format(profile=profile), "Terminal=false", "",
        ])
        # The system's desktop id (shadowed in the dock's XDG_DATA_HOME), so
        # bamf puts a started window on its launcher instead of a new icon.
        path = apps / ("%s.desktop" % BOT_DOCK_IDS.get(item, "maclaw-bot-" + item))
        write_if_changed(path, desktop, 0o600)
        write_if_changed(launchers / ("%s.dockitem" % item), "[PlankDockItemPreferences]\nLauncher=file://%s\n" % path, 0o600)
        items.append("'%s.dockitem'" % item)
    write_if_changed(cfg / "data" / "plank" / "themes" / "MaClaw-Bot" / "dock.theme", BOT_DOCK_THEME, 0o600)
    write_if_changed(cfg / "glib-2.0" / "settings" / "keyfile", BOT_DOCK_SETTINGS % ", ".join(items), 0o600)
    link = cfg / BOT_DOCK_NAME
    if not link.is_symlink():
        try:
            link.unlink()
        except OSError:
            pass
        # Started under its own name: maclaw-theme's "pgrep -x plank" must
        # keep seeing only the owner's dock.
        os.symlink(shutil.which("plank"), str(link))


def ensure_bot_panel(key, display, pids=None, log=None):
    """Wallpaper and bottom dock (or tasklist panel) on a running bot display.

    Plank, styled like the owner's soft25 dock, with this bot's launchers;
    xfce4-panel with a tasklist when Plank is not installed. Started at most
    once per BOT_PANEL_RETRY_SECONDS, so a dock that keeps crashing is not
    respawned every watcher tick. Never touches Chromium.
    """
    dock = shutil.which("plank") is not None
    if display == DISPLAY_MIN or shutil.which("dbus-daemon") is None or not (dock or shutil.which("xfce4-panel")):
        return
    if os.environ.get("MACLAW_BOT_PANEL", "1").strip() == "0":
        return
    if not Path("/tmp/.X11-unix/X%d" % display).exists():
        return
    if pids is None:
        pids = current_pids(key, display)
    if pid_alive(pids.get("panel")):
        return
    now = time.time()
    if now - _bot_panel_tried.get(key, 0) < BOT_PANEL_RETRY_SECONDS:
        return
    _bot_panel_tried[key] = now
    home = ROOT / key
    home.mkdir(mode=0o700, exist_ok=True)
    if log is None:
        log = open(home / "desktop.log", "ab")
    set_bot_wallpaper(display, log)
    cfg = home / "panel-config"
    try:
        if dock:
            write_bot_dock(key, display, cfg)
        else:
            write_if_changed(cfg / "xfce4" / "xfconf" / "xfce-perchannel-xml" / "xfce4-panel.xml", BOT_PANEL_XML, 0o600)
        write_if_changed(cfg / "gtk-3.0" / "gtk.css", BOT_PANEL_CSS, 0o600)
    except OSError as exc:
        print("bot panel config: %s" % exc, file=sys.stderr)
        return
    env = desktop_env(display)
    env["XDG_CONFIG_HOME"] = str(cfg)
    env["XDG_CONFIG_DIRS"] = str(cfg)
    env["NO_AT_BRIDGE"] = "1"
    if dock:
        env["XDG_DATA_HOME"] = str(cfg / "data")
        env["GSETTINGS_BACKEND"] = "keyfile"
    for name in ("QT_ACCESSIBILITY", "QT_LINUX_ACCESSIBILITY_ALWAYS_ON", "SESSION_MANAGER"):
        env.pop(name, None)
    bus = bot_panel_bus(display)
    kill_pid(pids.pop("panelbus", None))
    try:
        os.unlink(bus)
    except OSError:
        pass
    # bamfdaemon (Plank) or xfconfd (xfce4-panel) is started by this bus and
    # inherits its environment: the dock's own config dir.
    pids["panelbus"] = spawn(["dbus-daemon", "--session", "--nofork", "--nopidfile", "--address=unix:path=%s" % bus], env, log).pid
    for _ in range(40):
        if Path(bus).exists():
            break
        time.sleep(0.05)
    env["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=%s" % bus
    argv = [str(cfg / BOT_DOCK_NAME)] if dock else ["xfce4-panel", "--disable-wm-check"]
    pids["panel"] = spawn(argv, env, log).pid
    write_pids(key, pids)


def ensure_gates():
    """The owner's CDP and noVNC gates, running the current supervisor code.

    Bot displays reach the outside only through them. They run even while
    the owner's own desktop is stopped. Gates started by an older supervisor
    do not know bot tokens, so they are replaced once.
    """
    owner = owner_key()
    if not owner:
        return
    registry = load_registry()
    users = registry.setdefault("users", {})
    entry = users.get(owner)
    if entry is None:
        entry = {"display": DISPLAY_MIN, "last_used": 0}
        users[owner] = entry
    token = entry.get("token")
    if not token or not valid_key(token):
        token = secrets.token_hex(16)
        entry["token"] = token
        save_registry(registry)
    pids = current_pids(owner, DISPLAY_MIN)
    code = supervisor_code()
    changed = False
    for name, port, target in (
        ("proxy", proxy_port(DISPLAY_MIN), 18000 + DISPLAY_MIN),
        ("vgate", VNC_GATE_PORT, VNC_WEBSOCKIFY_PORT),
    ):
        if pid_alive(pids.get(name)) and pids.get(name + "_code") == code:
            continue
        kill_pid(pids.get(name))
        for _ in range(20):
            if not port_open(port):
                break
            time.sleep(0.1)
        home = ROOT / owner
        home.mkdir(mode=0o700, exist_ok=True)
        log = open(home / "desktop.log", "ab")
        proc = spawn([sys.executable, __file__, "gate", str(port), str(target), token], os.environ.copy(), log)
        pids[name] = proc.pid
        pids[name + "_code"] = code
        changed = True
    if changed:
        write_pids(owner, pids)


def ensure_bot_vnc(key, display):
    """x11vnc + websockify for one bot display, started on the first view."""
    if shutil.which("x11vnc") is None or shutil.which("websockify") is None:
        return 0
    with desktop_lock():
        if not desktop_running(key):
            return 0
        pids = current_pids(key, display)
        ws = vnc_ws_port(display)
        if pid_alive(pids.get("x11vnc")) and pid_alive(pids.get("vnc")):
            return ws
        kill_pid(pids.get("vnc"))
        kill_pid(pids.get("x11vnc"))
        log = open(ROOT / key / "desktop.log", "ab")
        env = os.environ.copy()
        env["DISPLAY"] = ":%d" % display
        rfb = vnc_rfb_port(display)
        x11 = spawn(["x11vnc", "-display", ":%d" % display, "-nopw", "-localhost", "-forever", "-shared", "-rfbport", str(rfb)], env, log)
        web = spawn(["websockify", "--web", "/usr/share/novnc", "127.0.0.1:%d" % ws, "localhost:%d" % rfb], env, log)
        pids["x11vnc"] = x11.pid
        pids["vnc"] = web.pid
        write_pids(key, pids)
        for _ in range(40):
            if port_open(ws) and port_open(rfb):
                break
            time.sleep(0.1)
        return ws


def start_bot_fcitx(key, display):
    """The input method for a person typing on a bot display (lazy)."""
    if shutil.which("fcitx5") is None:
        return
    pids = current_pids(key, display)
    if pid_alive(pids.get("fcitx")):
        return
    env = desktop_env(display)
    bus = Path("/tmp/.maclaw-dbus-%d" % display)
    if bus.exists():
        env["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=%s" % bus
    log = open(ROOT / key / "desktop.log", "ab")
    pids["fcitx"] = spawn(["fcitx5"], env, log).pid
    write_pids(key, pids)


def bot_gate_route(listen_port, credential):
    """(local port, bot key) for a bot token on one of the owner's gates."""
    if not credential or listen_port not in (VNC_GATE_PORT, proxy_port(DISPLAY_MIN)):
        return None
    users = load_registry().get("users", {}) or {}
    for key, entry in users.items():
        token = str((entry or {}).get("token") or "")
        if not token or not is_bot_key(key) or not hmac.compare_digest(token, credential):
            continue
        try:
            display = int(entry.get("display") or 0)
        except (TypeError, ValueError):
            return None
        if not (DISPLAY_MIN < display <= DISPLAY_MAX):
            return None
        if listen_port == VNC_GATE_PORT:
            port = ensure_bot_vnc(key, display)
            reap_children()
            return (port, key) if port else None
        return (18000 + display, key)
    return None


class BotTraffic:
    """Marks bot activity, and person input on a bot's noVNC connection.

    The viewer's bytes are WebSocket frames carrying the RFB client stream.
    A key event, a pointer event with a button down, or a paste is a person
    acting on that display; the bot yields while that is recent.
    """

    SIZES = {0: 20, 3: 10, 4: 8, 5: 6, 150: 10, 248: 12}

    def __init__(self, key, sniff_input):
        self.key = key
        self.sniff = sniff_input
        self.frames = b""
        self.rfb = b""
        self.stage = 0

    def feed(self, data):
        touch_bot_file(self.key, "activity")
        if not self.sniff:
            return
        try:
            self.frames += data
            if len(self.frames) > (1 << 20):
                self.sniff = False
                return
            self._frames()
        except (ValueError, IndexError, struct.error):
            self.sniff = False

    def _frames(self):
        buf = self.frames
        while len(buf) >= 2:
            length = buf[1] & 0x7F
            pos = 2
            if length == 126:
                if len(buf) < 4:
                    break
                length = struct.unpack(">H", buf[2:4])[0]
                pos = 4
            elif length == 127:
                if len(buf) < 10:
                    break
                length = struct.unpack(">Q", buf[2:10])[0]
                pos = 10
            mask = b""
            if buf[1] & 0x80:
                mask = buf[pos:pos + 4]
                pos += 4
            if len(buf) < pos + length:
                break
            payload = buf[pos:pos + length]
            if mask:
                payload = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
            if buf[0] & 0x0F in (0, 1, 2):
                self.rfb += payload
                self._messages()
            buf = buf[pos + length:]
        self.frames = buf

    def _messages(self):
        data = self.rfb
        while True:
            if self.stage == 0:  # ProtocolVersion
                if len(data) < 12:
                    break
                data, self.stage = data[12:], 1
            elif self.stage in (1, 2):  # security type, ClientInit
                if len(data) < 1:
                    break
                data, self.stage = data[1:], self.stage + 1
            else:
                if not data:
                    break
                kind = data[0]
                if kind in self.SIZES:
                    need = self.SIZES[kind]
                elif kind == 2:
                    if len(data) < 4:
                        break
                    need = 4 + 4 * struct.unpack(">H", data[2:4])[0]
                elif kind == 6:
                    if len(data) < 8:
                        break
                    need = 8 + struct.unpack(">I", data[4:8])[0]
                elif kind == 251:
                    if len(data) < 8:
                        break
                    need = 8 + 16 * data[6]
                else:
                    self.sniff = False
                    break
                if len(data) < need:
                    break
                if kind in (4, 6, 248) or (kind == 5 and data[1] != 0):
                    touch_bot_file(self.key, "human_input")
                data = data[need:]
        self.rfb = data


def watch_bot(key, display):
    idle_limit = env_int("MACLAW_BOT_IDLE_SECONDS", BOT_IDLE_SECONDS)
    while desktop_running(key):
        stopped = False
        with desktop_lock():
            if desktop_running(key):
                ensure_gates()
                keep_screen_awake(display)
                ensure_bot_panel(key, display)
                if takeover_state(key) is None and time.time() - bot_last_activity(key) >= idle_limit:
                    print("bot display %s idle, stopping" % key, file=sys.stderr)
                    stop_desktop(key)
                    stopped = True
        reap_children()
        if stopped:
            break
        time.sleep(5)
    return 0


def ctl_bot_key(request):
    key = str(request.get("key") or "")
    if not is_bot_key(key):
        raise ValueError("this is not a bot display")
    return key


def ctl_bot_state(request):
    key = ctl_bot_key(request)
    touch_bot_file(key, "activity")
    human = bot_file_age(key, "human_input")
    state = takeover_state(key)
    yield_limit = env_int("MACLAW_BOT_HUMAN_YIELD_SECONDS", BOT_HUMAN_YIELD_SECONDS)
    return {
        "ok": True,
        "running": desktop_running(key),
        "takeover": state is not None,
        "reason": str((state or {}).get("reason") or ""),
        "human_input_age": int(human) if human is not None else -1,
        "yield": state is not None or (human is not None and human < yield_limit),
    }


def ctl_takeover_begin(request):
    key = ctl_bot_key(request)
    reason = str(request.get("reason") or "").strip()[:300]
    path = ROOT / key / "takeover.json"
    path.parent.mkdir(mode=0o700, exist_ok=True)
    path.write_text(json.dumps({"reason": reason, "since": int(time.time())}), encoding="utf-8")
    entry = (load_registry().get("users", {}) or {}).get(key) or {}
    try:
        display = int(entry.get("display") or 0)
    except (TypeError, ValueError):
        display = 0
    if DISPLAY_MIN < display <= DISPLAY_MAX and desktop_running(key):
        start_bot_fcitx(key, display)
    return {"ok": True, "takeover": True, "reason": reason}


def ctl_takeover_end(request):
    key = ctl_bot_key(request)
    try:
        (ROOT / key / "takeover.json").unlink()
    except OSError:
        pass
    touch_bot_file(key, "activity", force=True)
    return {"ok": True, "takeover": False}


# ---------------------------------------------------------------------------
# Accessibility (AT-SPI) element tree for programs other than the browser.
#
# "a11y <base64 json>" prints one JSON line. The agent runs it through the
# container bash API, so the answer stays small (the bash output is capped):
# only showing elements with a size on screen, interactive ones first, and
# a node/depth cap. Ids are numbered per probe; the probe writes the path of
# every id (child indexes from the desktop root, plus role and name) so a
# later "act" finds the same element, or reports that it changed.

A11Y_SNAPSHOT = "/tmp/.maclaw-a11y-%d.json"
A11Y_MARKS = "/tmp/.maclaw-a11y-%d.mvg"
A11Y_MAX_NODES = 400
A11Y_DEFAULT_NODES = 120
A11Y_MAX_DEPTH = 40
A11Y_DEFAULT_DEPTH = 25
A11Y_NAME_MAX = 80
A11Y_INTERACTIVE_ROLES = frozenset((
    "push button", "toggle button", "check box", "radio button", "menu item",
    "check menu item", "radio menu item", "menu", "combo box", "text", "entry",
    "password text", "link", "page tab", "list item", "table cell", "tree item",
    "spin button", "slider", "icon", "toggle", "button", "scroll bar",
    "terminal", "document text", "editbar", "tool bar item",
))
A11Y_ACTION_NAMES = ("click", "press", "activate", "toggle", "jump", "open")


def a11y_display():
    raw = os.environ.get("DISPLAY", "") or (":%d" % DISPLAY_MIN)
    try:
        return int(raw.split(":", 1)[1].split(".", 1)[0])
    except (IndexError, ValueError):
        return DISPLAY_MIN


def a11y_prepare_env(display):
    os.environ["DISPLAY"] = ":%d" % display
    bus = "/tmp/.maclaw-dbus-%d" % display
    if not os.environ.get("DBUS_SESSION_BUS_ADDRESS") and Path(bus).exists():
        os.environ["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=" + bus
    os.environ.pop("NO_AT_BRIDGE", None)


def a11y_clean(text, limit=A11Y_NAME_MAX):
    text = " ".join(str(text or "").split())
    return text if len(text) <= limit else text[: limit - 1] + "\u2026"


def a11y_states(node, pyatspi):
    names = []
    try:
        states = node.getState()
    except Exception:
        return names
    for label, state in (
        ("focused", pyatspi.STATE_FOCUSED), ("focusable", pyatspi.STATE_FOCUSABLE),
        ("selected", pyatspi.STATE_SELECTED), ("checked", pyatspi.STATE_CHECKED),
        ("pressed", pyatspi.STATE_PRESSED), ("expanded", pyatspi.STATE_EXPANDED),
        ("editable", pyatspi.STATE_EDITABLE), ("active", pyatspi.STATE_ACTIVE),
    ):
        if states.contains(state):
            names.append(label)
    if not states.contains(pyatspi.STATE_SENSITIVE):
        names.append("disabled")
    return names


def a11y_showing(node, pyatspi):
    try:
        states = node.getState()
        return states.contains(pyatspi.STATE_SHOWING) and states.contains(pyatspi.STATE_VISIBLE)
    except Exception:
        return False


def a11y_bounds(node, pyatspi):
    try:
        ext = node.queryComponent().getExtents(pyatspi.DESKTOP_COORDS)
        return [int(ext.x), int(ext.y), int(ext.width), int(ext.height)]
    except Exception:
        return None


def a11y_actions(node):
    try:
        action = node.queryAction()
        return [action.getName(i) for i in range(action.nActions)]
    except Exception:
        return []


def a11y_text(node):
    try:
        text = node.queryText()
        return text.getText(0, min(text.characterCount, 200))
    except Exception:
        return ""


def a11y_windows(desktop, scope, pyatspi):
    """Top-level windows to walk: [(path, app name, window)]."""
    out = []
    for ai in range(desktop.childCount):
        try:
            app = desktop.getChildAtIndex(ai)
        except Exception:
            continue
        if app is None:
            continue
        app_name = a11y_clean(app.name, 40)
        # Chromium's own tree is large; the browser stays on CDP.
        if app_name.lower() in ("chromium", "google chrome", "chrome"):
            continue
        for wi in range(app.childCount):
            try:
                win = app.getChildAtIndex(wi)
            except Exception:
                continue
            if win is None or not a11y_showing(win, pyatspi):
                continue
            bounds = a11y_bounds(win, pyatspi)
            # xfwm4 keeps an "active" 5x5 helper window off screen.
            if not bounds or bounds[2] <= 8 or bounds[3] <= 8 or bounds[0] + bounds[2] <= 0 or bounds[1] + bounds[3] <= 0:
                continue
            out.append(([ai, wi], app_name, win))
    if scope == "all":
        return out
    if scope in ("", "focused", "active"):
        active = []
        for item in out:
            try:
                if item[2].getState().contains(pyatspi.STATE_ACTIVE):
                    active.append(item)
            except Exception:
                pass
        if not active:
            # Nothing reports active (a menu popup, the desktop): take the
            # last application window, not the panel or desktop background.
            active = [item for item in out if item[1] not in ("xfce4-panel", "xfdesktop", "xfwm4", "plank")][-1:]
        return active[:1]
    needle = scope.lower()
    return [item for item in out if needle in a11y_clean(item[2].name, 200).lower() or needle == item[1].lower()][:3]


def a11y_probe(request, pyatspi):
    display = a11y_display()
    scope = str(request.get("window") or "focused").strip()
    max_nodes = max(1, min(int(request.get("max_nodes") or A11Y_DEFAULT_NODES), A11Y_MAX_NODES))
    max_depth = max(1, min(int(request.get("max_depth") or A11Y_DEFAULT_DEPTH), A11Y_MAX_DEPTH))
    interactive_only = bool(request.get("interactive_only"))
    desktop = pyatspi.Registry.getDesktop(0)
    windows = a11y_windows(desktop, scope, pyatspi)
    found, visited = [], 0
    for path, app_name, win in windows:
        stack = [(win, path, 0)]
        while stack and visited < 20000:
            node, node_path, depth = stack.pop()
            visited += 1
            if not a11y_showing(node, pyatspi):
                continue
            try:
                role = node.getRoleName()
            except Exception:
                role = ""
            bounds = a11y_bounds(node, pyatspi)
            name = a11y_clean(node.name)
            interactive = role in A11Y_INTERACTIVE_ROLES
            if bounds and bounds[2] > 0 and bounds[3] > 0 and (interactive or name or depth == 0):
                found.append({
                    "path": node_path, "role": role, "name": name, "bounds": bounds,
                    "interactive": interactive, "depth": depth, "app": app_name, "node": node,
                    "order": len(found),
                })
            if depth >= max_depth:
                continue
            try:
                count = min(node.childCount, 500)
            except Exception:
                count = 0
            for ci in range(count - 1, -1, -1):
                try:
                    child = node.getChildAtIndex(ci)
                except Exception:
                    continue
                if child is not None:
                    stack.append((child, node_path + [ci], depth + 1))
    pool = [item for item in found if item["interactive"] or item["depth"] == 0]
    if not interactive_only:
        pool += [item for item in found if not (item["interactive"] or item["depth"] == 0)]
    truncated = len(pool) > max_nodes
    keep = sorted(pool[:max_nodes], key=lambda item: item["order"])
    elements, snapshot = [], {}
    for index, item in enumerate(keep, 1):
        element = {"id": index, "role": item["role"], "name": item["name"], "bounds": item["bounds"]}
        states = a11y_states(item["node"], pyatspi)
        if states:
            element["states"] = states
        if item["depth"] == 0:
            element["window"] = True
            element["app"] = item["app"]
        if item["role"] in ("text", "entry", "document text", "terminal"):
            value = a11y_clean(a11y_text(item["node"]), 60)
            if value:
                element["value"] = value
        elements.append(element)
        snapshot[str(index)] = {"path": item["path"], "role": item["role"], "name": item["name"], "bounds": item["bounds"]}
    write_if_changed(A11Y_SNAPSHOT % display, json.dumps({"time": int(time.time()), "ids": snapshot}), 0o600)
    write_if_changed(A11Y_MARKS % display, a11y_marks_mvg(elements), 0o600)
    return {"ok": True, "scope": scope, "windows": len(windows), "count": len(elements), "truncated": truncated, "elements": elements}


A11Y_MARK_COLORS = ("#e6194b", "#3cb44b", "#4363d8", "#f58231", "#911eb4", "#008080", "#9a6324", "#800000")


def a11y_marks_mvg(elements):
    """ImageMagick draw commands: one numbered box per element (set-of-marks)."""
    lines = ["fill none", "stroke-width 2", "font-size 12"]
    for element in elements:
        if element.get("window"):
            continue
        x, y, w, h = element["bounds"]
        color = A11Y_MARK_COLORS[element["id"] % len(A11Y_MARK_COLORS)]
        label = str(element["id"])
        lines.append("stroke '%s' fill none rectangle %d,%d %d,%d" % (color, x, y, x + w - 1, y + h - 1))
        lw = 7 * len(label) + 4
        lines.append("stroke none fill '%s' rectangle %d,%d %d,%d" % (color, x, y, x + lw, y + 14))
        lines.append("fill white text %d,%d '%s'" % (x + 2, y + 12, label))
    return "\n".join(lines) + "\n"


def a11y_resolve(element_id, pyatspi):
    display = a11y_display()
    try:
        snapshot = json.loads(Path(A11Y_SNAPSHOT % display).read_text(encoding="utf-8"))
        entry = snapshot["ids"][str(int(element_id))]
    except (OSError, ValueError, KeyError, TypeError):
        raise LookupError("element %s is not in the last probe; run app_probe again" % element_id)
    node = pyatspi.Registry.getDesktop(0)
    try:
        for index in entry["path"]:
            node = node.getChildAtIndex(int(index))
            if node is None:
                raise LookupError
    except Exception:
        raise LookupError("element %s is gone; run app_probe again" % element_id)
    try:
        role = node.getRoleName()
    except Exception:
        role = ""
    if role != entry["role"] or a11y_clean(node.name) != entry["name"]:
        raise LookupError("element %s changed (now %s %r); run app_probe again" % (element_id, role, a11y_clean(node.name)))
    return node, entry


def a11y_raise_window(node, center):
    """Activate the managed window that owns node; return its hex id or ""."""
    try:
        pid = int(node.get_process_id())
    except Exception:
        pid = 0
    if pid <= 0:
        return ""
    ids, active = ctl_client_windows()
    best = 0
    for wid in reversed(ids):
        info = ctl_window_info(wid, active)
        if info.get("pid") != pid:
            continue
        x, y, w, h = info["x"], info["y"], info["width"], info["height"]
        inside = x <= center[0] < x + w and y <= center[1] < y + h
        if inside or not best:
            best = wid
            if inside:
                break
    if not best or best == active:
        return ""
    ctl_run(["xdotool", "windowactivate", "--sync", str(best)], timeout=3)
    time.sleep(0.15)
    return "0x%08x" % best


def a11y_act(request, pyatspi):
    """Act on one probed element. Clicks are left to xdotool at the returned
    center when the element is on screen (a real click is what a person does);
    AT-SPI actions are the fallback, and focus / set_text use AT-SPI."""
    op = str(request.get("do") or "").strip()
    node, entry = a11y_resolve(request.get("id"), pyatspi)
    bounds = a11y_bounds(node, pyatspi) or entry["bounds"]
    center = [bounds[0] + bounds[2] // 2, bounds[1] + bounds[3] // 2]
    role = entry["role"]
    result = {"ok": True, "id": int(request.get("id")), "role": role, "name": entry["name"], "bounds": bounds, "center": center}
    if op == "locate":
        result["method"] = "locate"
        return result
    if op in ("click", "focus"):
        # The element's window may sit behind another one (seen: a Thunar
        # menu opened while Thunar stayed behind Chromium, so the next
        # xdotool click at the returned center hit Chromium).
        raised = a11y_raise_window(node, center)
        if raised:
            result["raised"] = raised
    if op == "click":
        names = a11y_actions(node)
        onscreen = bounds[2] > 0 and bounds[3] > 0 and center[0] >= 0 and center[1] >= 0
        if onscreen and not request.get("prefer_action"):
            result["method"] = "xdotool"
            return result
        for wanted in A11Y_ACTION_NAMES:
            if wanted in names:
                node.queryAction().doAction(names.index(wanted))
                result["method"] = "atspi:" + wanted
                return result
        result["method"] = "xdotool"
        return result
    if op == "focus":
        try:
            if node.queryComponent().grabFocus():
                result["method"] = "atspi"
                return result
        except Exception:
            pass
        result["method"] = "xdotool"
        return result
    if op == "set_text":
        if role == "password text":
            raise PermissionError("element %s is a password field; the person types passwords" % request.get("id"))
        text = str(request.get("text") or "")
        try:
            editable = node.queryEditableText()
            if editable.setTextContents(text):
                result["method"] = "atspi"
                return result
        except Exception:
            pass
        result["method"] = "xdotool"
        return result
    raise ValueError("unknown a11y action %r" % op)


def run_a11y(raw):
    try:
        request = json.loads(base64.b64decode(raw.encode("ascii"), validate=True).decode("utf-8"))
        if not isinstance(request, dict):
            raise ValueError("request is not an object")
    except (ValueError, UnicodeError) as exc:
        print(json.dumps({"ok": False, "error": "bad request: %s" % exc}))
        return 2
    a11y_prepare_env(a11y_display())
    try:
        import pyatspi  # noqa: imported here: only the a11y command needs it
    except ImportError:
        print(json.dumps({"ok": False, "error": "pyatspi is not installed in this desktop image"}))
        return 1
    try:
        if request.get("op") == "act":
            result = a11y_act(request, pyatspi)
        else:
            result = a11y_probe(request, pyatspi)
    except (LookupError, PermissionError, ValueError) as exc:
        result = {"ok": False, "error": str(exc)}
    except Exception as exc:  # AT-SPI raises its own GLib errors.
        result = {"ok": False, "error": "accessibility bus error: %s" % a11y_clean(exc, 200)}
    print(json.dumps(result, ensure_ascii=False, separators=(",", ":")))
    return 0 if result.get("ok") else 1


# ---------------------------------------------------------------------------
# "ctl <base64 json>": clipboard, windows, screen info, waiting, and the
# person-viewer status, for desktopd's /v1/desktops/ctl. One JSON line out.
# wmctrl is used when the image has it (maclaw-gui:2 built after it was
# added); older containers fall back to xdotool and xprop.

CTL_TEXT_MAX = 256 << 10
CTL_IMAGE_MAX = 4 << 20
CTL_WAIT_MAX = 60.0


def ctl_run(argv, data=None, timeout=10):
    try:
        done = subprocess.run(argv, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired) as exc:
        return 127, b"", str(exc).encode()
    return done.returncode, done.stdout, done.stderr


def ctl_clipboard_get(request):
    target = str(request.get("type") or "text")
    if target == "image":
        code, out, _ = ctl_run(["xclip", "-selection", "clipboard", "-o", "-t", "image/png"])
        if code != 0 or not out.startswith(b"\x89PNG"):
            return {"ok": True, "type": "image", "empty": True}
        if len(out) > CTL_IMAGE_MAX:
            return {"ok": False, "error": "clipboard image is larger than %d bytes" % CTL_IMAGE_MAX}
        return {"ok": True, "type": "image", "mime": "image/png", "image_base64": base64.b64encode(out).decode()}
    code, out, _ = ctl_run(["xclip", "-selection", "clipboard", "-o", "-t", "UTF8_STRING"])
    if code != 0:
        code, out, _ = ctl_run(["xclip", "-selection", "clipboard", "-o"])
    if code != 0:
        return {"ok": True, "type": "text", "text": "", "empty": True}
    text = out[:CTL_TEXT_MAX].decode("utf-8", "replace")
    return {"ok": True, "type": "text", "text": text, "truncated": len(out) > CTL_TEXT_MAX}


def ctl_clipboard_set(request):
    if request.get("image_base64"):
        try:
            data = base64.b64decode(str(request["image_base64"]), validate=True)
        except ValueError:
            return {"ok": False, "error": "image_base64 is not base64"}
        if not data.startswith(b"\x89PNG") or len(data) > CTL_IMAGE_MAX:
            return {"ok": False, "error": "clipboard image must be a PNG up to %d bytes" % CTL_IMAGE_MAX}
        targets = [("clipboard", "image/png", data)]
    else:
        text = request.get("text")
        if not isinstance(text, str):
            return {"ok": False, "error": "text is required"}
        data = text.encode("utf-8")
        if len(data) > CTL_TEXT_MAX:
            return {"ok": False, "error": "clipboard text is larger than %d bytes" % CTL_TEXT_MAX}
        targets = [("clipboard", "UTF8_STRING", data), ("primary", "UTF8_STRING", data)]
    for selection, mime, payload in targets:
        # xclip keeps serving the selection in the background; its output
        # must not hold the docker exec pipe open.
        try:
            proc = subprocess.Popen(
                ["xclip", "-selection", selection, "-t", mime, "-i"],
                stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                start_new_session=True,
            )
            proc.communicate(payload, timeout=10)
        except (OSError, subprocess.TimeoutExpired) as exc:
            return {"ok": False, "error": "xclip failed: %s" % exc}
        if proc.returncode != 0:
            return {"ok": False, "error": "xclip failed"}
    return {"ok": True, "bytes": len(data)}


def ctl_xprop(wid, *names):
    code, out, _ = ctl_run(["xprop", "-id", str(wid)] + list(names), timeout=3)
    return out.decode("utf-8", "replace") if code == 0 else ""


def ctl_window_info(wid, active):
    info = {"id": "0x%08x" % wid, "active": wid == active}
    code, out, _ = ctl_run(["xdotool", "getwindowname", str(wid)], timeout=3)
    info["title"] = out.decode("utf-8", "replace").strip() if code == 0 else ""
    code, out, _ = ctl_run(["xdotool", "getwindowgeometry", "--shell", str(wid)], timeout=3)
    geo = {}
    for line in out.decode().splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            if value.strip().lstrip("-").isdigit():
                geo[key.strip()] = int(value)
    info["x"], info["y"] = geo.get("X", 0), geo.get("Y", 0)
    info["width"], info["height"] = geo.get("WIDTH", 0), geo.get("HEIGHT", 0)
    props = ctl_xprop(wid, "WM_CLASS", "_NET_WM_PID", "_NET_WM_DESKTOP", "_NET_WM_STATE")
    m = re.search(r'WM_CLASS\(STRING\) = "([^"]*)", "([^"]*)"', props)
    info["class"] = m.group(2) if m else ""
    m = re.search(r"_NET_WM_PID\(CARDINAL\) = (\d+)", props)
    info["pid"] = int(m.group(1)) if m else 0
    m = re.search(r"_NET_WM_DESKTOP\(CARDINAL\) = (\d+)", props)
    info["desktop"] = int(m.group(1)) if m else 0
    state = props.split("_NET_WM_STATE(ATOM) = ", 1)[1].split("\n", 1)[0] if "_NET_WM_STATE(ATOM) = " in props else ""
    info["minimized"] = "_NET_WM_STATE_HIDDEN" in state
    info["maximized"] = "_NET_WM_STATE_MAXIMIZED_VERT" in state and "_NET_WM_STATE_MAXIMIZED_HORZ" in state
    return info


def ctl_client_windows():
    """Managed top-level windows in stacking order, from the window manager."""
    code, out, _ = ctl_run(["xprop", "-root", "_NET_CLIENT_LIST"], timeout=3)
    ids = [int(x, 16) for x in re.findall(r"0x[0-9a-fA-F]+", out.decode())] if code == 0 else []
    code, out, _ = ctl_run(["xprop", "-root", "_NET_ACTIVE_WINDOW"], timeout=3)
    m = re.search(r"0x[0-9a-fA-F]+", out.decode()) if code == 0 else None
    return ids, int(m.group(0), 16) if m else 0


def ctl_windows(request):
    ids, active = ctl_client_windows()
    windows = []
    for wid in ids:
        info = ctl_window_info(wid, active)
        # The desktop background, panels and the Plank dock are not windows to work with.
        if info["class"].lower() in ("xfdesktop", "xfce4-panel", "wrapper-2.0", "plank") and not request.get("all"):
            continue
        windows.append(info)
    return {"ok": True, "windows": windows, "wmctrl": shutil.which("wmctrl") is not None}


def ctl_find_window(request):
    raw = str(request.get("id") or "").strip()
    ids, active = ctl_client_windows()
    if raw:
        try:
            wid = int(raw, 16) if raw.lower().startswith("0x") else int(raw)
        except ValueError:
            raise LookupError("window id %r is invalid" % raw)
        if wid not in ids:
            raise LookupError("window %s is not open" % raw)
        return wid
    title = str(request.get("title") or "").strip().lower()
    if not title:
        if active:
            return active
        raise LookupError("give a window id or title")
    for wid in reversed(ids):
        info = ctl_window_info(wid, active)
        if title in info["title"].lower() or title == info["class"].lower():
            return wid
    raise LookupError("no window matches %r" % request.get("title"))


def ctl_maximize_by_key(wid):
    """Old/state containers may lack wmctrl. xfwm4's maximize toggle
    (alt+F10) sets the real _NET_WM_STATE, so the window reports
    maximized=true, unlike moving and resizing it to the work area."""
    if not ctl_wm_is_xfwm():
        return False
    _, active = ctl_client_windows()
    if ctl_window_info(wid, active)["maximized"]:
        return True
    ctl_run(["xdotool", "windowactivate", "--sync", str(wid)], timeout=3)
    ctl_run(["xdotool", "key", "--clearmodifiers", "alt+F10"], timeout=3)
    for _ in range(10):
        time.sleep(0.1)
        _, active = ctl_client_windows()
        if ctl_window_info(wid, active)["maximized"]:
            return True
    return False


def ctl_wm_is_xfwm():
    code, out, _ = ctl_run(["pgrep", "-x", "xfwm4"], timeout=3)
    return code == 0 and bool(out.strip())


def ctl_window(request):
    op = str(request.get("do") or "").strip()
    wid = ctl_find_window(request)
    wm = shutil.which("wmctrl") is not None
    hexid = "0x%08x" % wid
    steps = []
    if op in ("activate", "focus"):
        steps = [["xdotool", "windowactivate", "--sync", str(wid)]]
    elif op == "minimize":
        steps = [["xdotool", "windowminimize", str(wid)]]
    elif op in ("maximize", "restore"):
        action = "add" if op == "maximize" else "remove"
        if wm:
            steps = [["wmctrl", "-i", "-r", hexid, "-b", action + ",maximized_vert,maximized_horz"]]
            if op == "restore":
                steps.append(["wmctrl", "-i", "-a", hexid])
        elif op == "restore":
            steps = [["xdotool", "windowmap", str(wid)], ["xdotool", "windowactivate", str(wid)]]
        elif ctl_maximize_by_key(wid):
            steps = []
        else:
            # Without wmctrl: fill the work area the panel leaves free.
            code, out, _ = ctl_run(["xprop", "-root", "_NET_WORKAREA"], timeout=3)
            nums = [int(n) for n in re.findall(r"\d+", out.decode().split("=", 1)[-1])][:4] if code == 0 else []
            if len(nums) < 4:
                nums = [0, 0] + ctl_screen_size()
            steps = [["xdotool", "windowactivate", str(wid)], ["xdotool", "windowmove", str(wid), str(nums[0]), str(nums[1])],
                     ["xdotool", "windowsize", str(wid), str(nums[2]), str(nums[3])]]
    elif op in ("move", "resize", "move_resize"):
        if op in ("move", "move_resize"):
            steps.append(["xdotool", "windowmove", str(wid), str(int(request.get("x", 0))), str(int(request.get("y", 0)))])
        if op in ("resize", "move_resize"):
            w, h = int(request.get("width", 0)), int(request.get("height", 0))
            if w < 50 or h < 50:
                return {"ok": False, "error": "width and height must be at least 50"}
            if wm:
                ctl_run(["wmctrl", "-i", "-r", hexid, "-b", "remove,maximized_vert,maximized_horz"])
            steps.append(["xdotool", "windowsize", str(wid), str(w), str(h)])
    elif op == "close":
        # A polite close (WM_DELETE_WINDOW): the program may still ask to
        # save. xdotool windowclose destroys the window and loses the work.
        if wm:
            steps = [["wmctrl", "-i", "-c", hexid]]
        else:
            steps = [["xdotool", "windowactivate", "--sync", str(wid)], ["xdotool", "key", "--clearmodifiers", "alt+F4"]]
    else:
        return {"ok": False, "error": "window action must be activate, maximize, minimize, restore, move, resize, move_resize, or close"}
    for argv in steps:
        code, _, err = ctl_run(argv)
        if code != 0:
            return {"ok": False, "error": "%s failed: %s" % (argv[0], err.decode("utf-8", "replace").strip()[:200])}
    time.sleep(0.5 if op == "close" else 0.2)
    ids, active = ctl_client_windows()
    window = ctl_window_info(wid, active) if wid in ids else {"id": hexid, "closed": True}
    if op == "close" and wid in ids:
        window["note"] = "still open: the program may be asking to save; answer its dialog"
    method = "wmctrl" if wm and steps and steps[0][0] == "wmctrl" else ("xfwm-key" if op == "maximize" and not steps else "xdotool")
    if op == "maximize" and not window.get("closed") and not window.get("maximized"):
        # Filled the work area by move+resize: say so instead of a bare
        # maximized=false that reads like a failure.
        window["filled_work_area"] = True
    return {"ok": True, "do": op, "window": window, "method": method}


def ctl_screen_size():
    code, out, _ = ctl_run(["xdotool", "getdisplaygeometry"], timeout=3)
    try:
        w, h = out.decode().split()[:2]
        return [int(w), int(h)]
    except ValueError:
        return [0, 0]


def ctl_screen(request):
    w, h = ctl_screen_size()
    code, out, _ = ctl_run(["xdotool", "getmouselocation", "--shell"], timeout=3)
    pos = dict(line.split("=", 1) for line in out.decode().splitlines() if "=" in line)
    return {"ok": True, "width": w, "height": h, "scale": 1.0, "display": os.environ.get("DISPLAY", ""),
            "cursor": [int(pos.get("X", 0)), int(pos.get("Y", 0))] if pos else None}


def ctl_screen_hash():
    code, out, _ = ctl_run(["sh", "-c", "import -window root -resize 160x100! -colorspace gray gray:- 2>/dev/null"], timeout=10)
    return out if code == 0 else b""


def ctl_wait(request):
    timeout = max(0.1, min(float(request.get("timeout") or 10), CTL_WAIT_MAX))
    what = str(request.get("for") or "change")
    deadline = time.time() + timeout
    if what == "window":
        title = str(request.get("title") or "").strip().lower()
        if not title:
            return {"ok": False, "error": "title is required"}
        gone = bool(request.get("gone"))
        while True:
            ids, active = ctl_client_windows()
            match = None
            for wid in ids:
                info = ctl_window_info(wid, active)
                if title in info["title"].lower() or title == info["class"].lower():
                    match = info
                    break
            if (match is None) == gone:
                return {"ok": True, "found": not gone, "window": match, "waited": round(timeout - (deadline - time.time()), 2)}
            if time.time() >= deadline:
                return {"ok": False, "timeout": True, "error": "window %r did not %s within %.1fs" % (request.get("title"), "close" if gone else "appear", timeout)}
            time.sleep(0.3)
    if what != "change":
        return {"ok": False, "error": "wait for must be change or window"}
    threshold = max(0.0001, min(float(request.get("threshold") or 0.005), 1.0))
    first = ctl_screen_hash()
    if not first:
        return {"ok": False, "error": "cannot read the screen"}
    while time.time() < deadline:
        time.sleep(0.4)
        now = ctl_screen_hash()
        if len(now) == len(first):
            changed = sum(1 for a, b in zip(first, now) if abs(a - b) > 24) / float(len(first))
            if changed >= threshold:
                return {"ok": True, "changed": round(changed, 4), "waited": round(timeout - (deadline - time.time()), 2)}
    return {"ok": False, "timeout": True, "error": "the screen did not change within %.1fs" % timeout}


def ctl_status(request):
    """Whether a person has the desktop open in noVNC right now."""
    viewers = 0
    for table in ("/proc/net/tcp", "/proc/net/tcp6"):
        try:
            lines = Path(table).read_text().splitlines()[1:]
        except OSError:
            continue
        for line in lines:
            parts = line.split()
            if len(parts) > 3 and parts[3] == "01" and int(parts[1].rsplit(":", 1)[1], 16) == VNC_GATE_PORT:
                viewers += 1
    return {"ok": True, "viewers": viewers, "screen": ctl_screen_size()}


CTL_OPS = {
    "clipboard_get": ctl_clipboard_get,
    "clipboard_set": ctl_clipboard_set,
    "windows": ctl_windows,
    "window": ctl_window,
    "screen": ctl_screen,
    "wait": ctl_wait,
    "status": ctl_status,
    "bot_state": ctl_bot_state,
    "takeover_begin": ctl_takeover_begin,
    "takeover_end": ctl_takeover_end,
}


def run_ctl(raw):
    try:
        request = json.loads(base64.b64decode(raw.encode("ascii"), validate=True).decode("utf-8"))
        if not isinstance(request, dict):
            raise ValueError("request is not an object")
    except (ValueError, UnicodeError) as exc:
        print(json.dumps({"ok": False, "error": "bad request: %s" % exc}))
        return 2
    op = str(request.get("op") or "")
    if op in ("a11y_probe", "a11y_act"):
        request["op"] = "act" if op == "a11y_act" else "probe"
        return run_a11y(base64.b64encode(json.dumps(request).encode()).decode())
    handler = CTL_OPS.get(op)
    if handler is None:
        print(json.dumps({"ok": False, "error": "unknown op %r" % op}))
        return 2
    a11y_prepare_env(a11y_display())
    try:
        result = handler(request)
    except (LookupError, ValueError, TypeError) as exc:
        result = {"ok": False, "error": str(exc)}
    print(json.dumps(result, ensure_ascii=False, separators=(",", ":")))
    return 0 if result.get("ok") else 1


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv))
    except SystemExit as exc:
        if exc.code not in (None, 0):
            print(exc, file=sys.stderr)
            sys.exit(exc.code if isinstance(exc.code, int) else 1)
        raise
