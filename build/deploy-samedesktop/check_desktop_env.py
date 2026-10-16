#!/usr/bin/env python3
import os
import subprocess

def run(args):
    return subprocess.check_output(args, stderr=subprocess.STDOUT).decode("utf-8", "replace")

out = run(["ps", "-eo", "pid,args"])
pids = {}
for line in out.splitlines():
    parts = line.strip().split(None, 1)
    if len(parts) != 2:
        continue
    pid, args = parts
    if "/maclaw_srv/bin/maclawsrv" in args:
        pids["maclawsrv"] = pid
    elif "/hub/maclaw-hub" in args:
        pids["hub"] = pid

def show(pid, want):
    path = "/proc/%s/environ" % pid
    data = open(path, "rb").read().split(b"\0")
    have = {}
    for item in data:
        if b"=" not in item:
            continue
        key, value = item.split(b"=", 1)
        name = key.decode("utf-8", "replace")
        if name in want:
            have[name] = len(value)
    print("PROC", pid)
    for key in want:
        if key in have:
            print(" ", key, "len", have[key])
        else:
            print(" ", key, "absent")

print("PIDS", pids)
if "maclawsrv" in pids:
    show(pids["maclawsrv"], ("MACLAW_HUB_URL", "MACLAW_DESKTOP_API_TOKEN"))
if "hub" in pids:
    show(pids["hub"], ("MACLAW_DESKTOP_API_TOKEN",))

names = run(["bash", "-lc", "grep -l MACLAW_DESKTOP_API_TOKEN /data/soft/hub/start.sh /data/soft/hub/configs/config.yaml /etc/systemd/system/maclawsrv.service /root/.bashrc /etc/profile 2>/dev/null || true"])
print("MENTIONS")
print(names.strip() or "(none)")
