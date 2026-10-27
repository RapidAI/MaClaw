#!/usr/bin/env python3
"""Single-session app-partition backup using the esptool Python API.

Reconnecting for every chunk repeatedly resets the chip and re-uploads the stub,
which is both slow and the phase where this board's USB-Serial/JTAG link tends
to wedge. This helper keeps one session alive, reads the partition in chunks,
and only reconnects if a chunk actually fails. Progress is written to disk so a
later run resumes instead of starting over.

Usage:
    python backup_app_session.py [--port COM3] [--out backup/voice-app.bin]
"""

from __future__ import annotations

import argparse
import hashlib
import os
import sys
import time

APP_OFFSET = 0x10000
APP_SIZE = 0x3A0000
# 512 B: block reads fail randomly at several addresses (e.g. 0x9E800 fails at
# 2 KiB but succeeds at 512 B), and small reads also stay under the link's
# per-session continuous-read budget.
CHUNK = 0x200
# The USB-Serial/JTAG endpoint wedges after a varying amount of continuous
# reads, and a wedged session cannot be recovered in-process. Read a safe
# budget, then close cleanly (leaving the stub alive) and let the caller start
# a new session that resumes from the output size.
CHUNK_PAUSE_S = 0.005
SESSION_BUDGET = 0x20000


def connect(port: str):
    from esptool import detect_chip

    # Prefer attaching to the stub that a previous session left running: a hard
    # reset would boot the installed app, whose USB console output corrupts the
    # next stub sync ("Possible serial noise or corruption"). Fall back to a
    # reset only when no stub is alive.
    try:
        esp = detect_chip(port=port, baud=115200, connect_mode="no-reset")
    except Exception:
        esp = detect_chip(port=port, baud=115200)
    # run_stub() returns a NEW loader instance with IS_STUB=True; ignoring the
    # return value silently falls back to the ROM read path, which rejects
    # READ_FLASH with "Invalid command" on ESP32-S3.
    return esp.run_stub()


def close(esp, reset: bool = False) -> None:
    if esp is None:
        return
    if reset:
        try:
            esp.hard_reset()
        except Exception:
            pass
    # Leaving the stub alive avoids booting the app between sessions.
    try:
        esp._port.close()
    except Exception:
        pass


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", default="COM3")
    parser.add_argument("--out", default=os.path.join("backup", "voice-app-backup.bin"))
    parser.add_argument("--chunk", type=lambda v: int(v, 0), default=CHUNK)
    args = parser.parse_args()

    from esptool.cmds import read_flash

    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    done = 0
    if os.path.exists(args.out):
        done = (os.path.getsize(args.out) // args.chunk) * args.chunk
        with open(args.out, "r+b") as fh:
            fh.truncate(done)
        if done:
            print(f"resuming at {done}/{APP_SIZE} bytes")

    esp = None
    part = args.out + ".part"
    offset = done
    session_bytes = 0
    while offset < APP_SIZE:
        if session_bytes >= SESSION_BUDGET:
            close(esp)
            print(f"\npaused at {offset}/{APP_SIZE} bytes; start a new session to resume")
            return 0
        size = min(args.chunk, APP_SIZE - offset)
        ok = False
        for attempt in range(1, 4):
            try:
                if esp is None:
                    esp = connect(args.port)
                read_flash(esp, APP_OFFSET + offset, size, part, no_progress=True,
                           flash_size="16MB")
                if not os.path.exists(part) or os.path.getsize(part) != size:
                    raise RuntimeError("short read")
                with open(args.out, "ab") as out, open(part, "rb") as fh:
                    out.write(fh.read())
                ok = True
                break
            except Exception as exc:  # noqa: BLE001 - esptool raises many types
                print(f"\n[retry {attempt}] 0x{APP_OFFSET + offset:X}: {exc}")
                close(esp)
                esp = None
                # The stub can wedge the USB-Serial/JTAG endpoint; give the
                # device time to re-enumerate before reconnecting.
                time.sleep(min(2 + attempt, 8))
        if not ok:
            print(f"FAILED at 0x{APP_OFFSET + offset:X}", file=sys.stderr)
            close(esp, reset=False)
            return 1
        offset += size
        session_bytes += size
        print(f"\r{offset}/{APP_SIZE} bytes ({offset * 100 // APP_SIZE}%)",
              end="", flush=True)
        time.sleep(CHUNK_PAUSE_S)

    close(esp, reset=True)
    if os.path.exists(part):
        os.remove(part)
    with open(args.out, "rb") as fh:
        digest = hashlib.sha256(fh.read()).hexdigest()
    print(f"\nbackup: {args.out} ({os.path.getsize(args.out)} bytes)")
    print(f"sha256: {digest}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
