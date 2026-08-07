#!/usr/bin/env python3
"""Capture a bounded Fangtang transaction from COM5.

This helper deliberately owns COM5 only in the foreground.  It never touches
COM3/COM4 and always closes the serial handle before returning, including on
Ctrl+C.  Terminal markers let a normal voice command finish early while the
timeout still covers long Agent work and cancellation testing.
"""

from __future__ import annotations

import argparse
import pathlib
import time

import serial


TERMINAL_MARKERS = (
    b"command timing: terminal=",
    b"voice command cancelled by double tap",
    b"cancelled command returned to standby",
    b"response dismissed; ambient screen restored",
    b"meeting_result",
)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--seconds", type=int, default=180)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument(
        "--settle-seconds",
        type=float,
        default=5.0,
        help="keep recording briefly after the last terminal marker",
    )
    args = parser.parse_args()
    if args.seconds < 1 or args.seconds > 900:
        parser.error("--seconds must be in 1..900")

    args.output.parent.mkdir(parents=True, exist_ok=True)
    deadline = time.monotonic() + args.seconds
    terminal_at: float | None = None
    marker_scan_from = 0
    captured = bytearray()

    def persist() -> None:
        temporary = args.output.with_suffix(args.output.suffix + ".part")
        temporary.write_bytes(captured)
        temporary.replace(args.output)

    try:
        with serial.Serial("COM5", 115200, timeout=0.2) as port:
            while time.monotonic() < deadline:
                chunk = port.read(port.in_waiting or 1)
                if chunk:
                    captured.extend(chunk)
                    scan_from = max(0, marker_scan_from - max(map(len, TERMINAL_MARKERS)))
                    if any(marker in captured[scan_from:] for marker in TERMINAL_MARKERS):
                        terminal_at = terminal_at or time.monotonic()
                    marker_scan_from = len(captured)
                    # Persist incrementally so a Codex tool timeout, terminal
                    # close, or Ctrl+C never strands a serial owner with all
                    # useful evidence held only in RAM.
                    if len(captured) % 4096 < len(chunk):
                        persist()
                if terminal_at and time.monotonic() - terminal_at >= args.settle_seconds:
                    break
    except KeyboardInterrupt:
        pass
    finally:
        persist()

    print(f"captured {len(captured)} bytes to {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
