#!/bin/sh
echo PROBE2_BEGIN
systemctl show maclawsrv.service -p EnvironmentFiles --no-page
python3 - <<'PY'
import os, urllib.request

def lengths(pid):
    path = "/proc/%s/environ" % pid
    out = {}
    try:
        data = open(path, "rb").read().split(b"\0")
    except Exception:
        print("PID_%s_ENV=unreadable" % pid)
        return
    for item in data:
        if b"=" not in item:
            continue
        k, v = item.split(b"=", 1)
        name = k.decode("utf-8", "replace")
        if name == "MACLAW_DESKTOP_API_TOKEN":
            out["token"] = len(v)
        elif name == "MACLAW_HUB_URL":
            out["url"] = len(v)
    print("PID_%s_TOKEN_LEN=%s" % (pid, out.get("token", "missing")))
    print("PID_%s_URL_LEN=%s" % (pid, out.get("url", "missing")))

srv = os.popen("systemctl show -p MainPID --value maclawsrv.service").read().strip()
print("SRV_PID=%s" % srv)
lengths(srv)
hub = ""
for pid in os.listdir("/proc"):
    if not pid.isdigit():
        continue
    try:
        cmd = open("/proc/%s/cmdline" % pid, "rb").read().split(b"\0")
    except Exception:
        continue
    if cmd and cmd[0].endswith(b"/maclaw-hub") and b"hubcenter" not in cmd[0]:
        hub = pid
        break
print("HUB_PID=%s" % hub)
if hub:
    lengths(hub)
file_token = None
file_url = None
for line in open("/data/soft/maclaw_srv/.env"):
    line = line.strip()
    if not line or line.startswith("#") or "=" not in line:
        continue
    k, v = line.split("=", 1)
    v = v.strip().strip('"').strip("'")
    if k == "MACLAW_DESKTOP_API_TOKEN":
        file_token = len(v)
    elif k == "MACLAW_HUB_URL":
        file_url = len(v)
print("FILE_TOKEN_LEN=%s" % (file_token if file_token is not None else "missing"))
print("FILE_URL_LEN=%s" % (file_url if file_url is not None else "missing"))
token = ""
for line in open("/data/soft/hub/desktop-api.env"):
    line = line.strip()
    if line.startswith("MACLAW_DESKTOP_API_TOKEN="):
        token = line.split("=", 1)[1].strip().strip('"').strip("'")
        break
print("HUBFILE_TOKEN_LEN=%s" % (len(token) if token else "missing"))
if token:
    req = urllib.request.Request(
        "http://127.0.0.1:9399/api/v1/desktop-services/session",
        data=b"{",
        headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"},
        method="POST",
    )
    try:
        urllib.request.urlopen(req, timeout=3)
        print("SESSION_PROBE=unexpected-200")
    except Exception as exc:
        status = getattr(exc, "code", None)
        print("SESSION_PROBE=%s" % (status if status else "err"))
PY
echo PROBE2_END
