#!/usr/bin/env python3
"""Show, back up, change, or restore Hub's desktop service settings.

Hub keeps Docker desktop services and their assignments as one JSON value in
system_settings (key "desktop_service", or "tenant:<id>:desktop_service" for
other tenants; see hub/internal/desktoppool). The Hub admin page
(PATCH /api/admin/desktop-services/{id}) is the normal way to change it; this
script writes the same row for hosts where only shell access is available.
Hub reads the row on every request (no cache), so no restart is needed.

  hub_desktop_settings.py show
  hub_desktop_settings.py backup /data/soft/backups/desktop_service.json
  hub_desktop_settings.py set --server-id dsrv_xxx --image maclaw-gui:2 --memory 3g --cpus 1.5 --shm 1g [--dry-run]
  hub_desktop_settings.py restore /data/soft/backups/desktop_service.json

Environment: HUB_DB (default /data/soft/hub/data/codeclaw-hub.db).
Access tokens are masked in every printout; backups contain them, so the
backup file is written with mode 0600. Runs on Python 3.6 / SQLite 3.22.
"""
import argparse
import datetime
import json
import os
import sqlite3
import sys

DB = os.environ.get("HUB_DB", "/data/soft/hub/data/codeclaw-hub.db")


def key_for(tenant):
    tenant = (tenant or "").strip()
    if tenant in ("", "tenant_default"):
        return "desktop_service"
    return "tenant:%s:desktop_service" % tenant


def read(key):
    con = sqlite3.connect("file:%s?mode=ro" % DB, uri=True, timeout=30)
    try:
        row = con.execute("SELECT value_json, updated_at FROM system_settings WHERE key=?", (key,)).fetchone()
    finally:
        con.close()
    return row


def masked(value):
    data = json.loads(value)
    for server in data.get("servers", []):
        server["access_token"] = "<set>" if server.get("access_token") else ""
    return data


def write(key, value):
    now = datetime.datetime.now().astimezone().replace(microsecond=0).isoformat()
    con = sqlite3.connect(DB, timeout=30)
    try:
        con.execute("PRAGMA busy_timeout=30000")
        with con:
            n = con.execute("UPDATE system_settings SET value_json=?, updated_at=? WHERE key=?", (value, now, key)).rowcount
            if n == 0:
                con.execute("INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, ?)", (key, value, now))
    finally:
        con.close()


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--tenant", default="tenant_default")
    sub = ap.add_subparsers(dest="cmd")
    sub.add_parser("show")
    b = sub.add_parser("backup")
    b.add_argument("path")
    r = sub.add_parser("restore")
    r.add_argument("path")
    s = sub.add_parser("set")
    s.add_argument("--server-id", help="defaults to the only server when there is exactly one")
    s.add_argument("--image")
    s.add_argument("--memory")
    s.add_argument("--cpus")
    s.add_argument("--shm")
    s.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()
    key = key_for(args.tenant)

    if args.cmd in (None, "show"):
        row = read(key)
        if not row:
            print("%s: not set" % key)
            return 0
        print(json.dumps({"key": key, "updated_at": row[1], "value": masked(row[0])}, ensure_ascii=False, indent=1))
        return 0
    if args.cmd == "backup":
        row = read(key)
        if not row:
            sys.exit("%s: not set" % key)
        fd = os.open(args.path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as fh:
            json.dump([{"key": key, "value_json": row[0], "updated_at": row[1]}], fh, ensure_ascii=False, indent=1)
        print("saved %s to %s" % (key, args.path))
        return 0
    if args.cmd == "restore":
        with open(args.path, encoding="utf-8") as fh:
            items = json.load(fh)
        for item in items:
            json.loads(item["value_json"])
            write(item["key"], item["value_json"])
            print("restored %s" % item["key"])
        return 0

    row = read(key)
    if not row:
        sys.exit("%s: not set; create the server in the Hub admin page first" % key)
    data = json.loads(row[0])
    servers = data.get("servers", [])
    if args.server_id:
        matches = [x for x in servers if x.get("id") == args.server_id]
    else:
        matches = servers if len(servers) == 1 else []
    if len(matches) != 1:
        sys.exit("pick a server with --server-id: %s" % [x.get("id") for x in servers])
    server = matches[0]
    for field, value in (("image", args.image), ("memory", args.memory), ("cpus", args.cpus), ("shm_size", args.shm)):
        if value:
            server[field] = value.strip()
    # Same compact encoding Go's json.Marshal writes.
    value = json.dumps(data, separators=(",", ":"), ensure_ascii=False)
    print(json.dumps({"key": key, "value": masked(value)}, ensure_ascii=False, indent=1))
    if args.dry_run:
        print("dry run: not written")
        return 0
    write(key, value)
    print("written")
    return 0


if __name__ == "__main__":
    sys.exit(main())
