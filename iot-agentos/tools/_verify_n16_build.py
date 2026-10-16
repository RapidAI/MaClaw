"""Post-build verification for the N1-6 echoear-2st firmware image.

Confirms the build actually produced the application binary and that the
device-side high-risk-approval code (N1-6) is present in the linked ELF.

Run ONLY after the ninja build has finished (tools/_ninja_build.py).
"""

import os
import re
import glob
import subprocess
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "scripts"))
import idf_run as ir  # noqa: E402

BUILD = "build-unified-echoear"
ELF = os.path.join(BUILD, "maclaw_esp32s3_client.elf")
BIN = os.path.join(BUILD, "maclaw_esp32s3_client.bin")

# Symbols that MUST exist (defined) in the image for N1-6 to be live.
EXPECTED = [
    "gateway_event_ack_service_flush",
    "gateway_event_ack_service_handle_input",
    "event_decision_begin",
    "event_decision_apply_input",
    "event_decision_settled",
    "approval_gesture_for",
]


def find_nm():
    """Locate the xtensa nm that matches the toolchain used in build.ninja.

    The firmware build invokes the toolchain by absolute path embedded in
    build.ninja, so PATH is irrelevant.  Derive the toolchain version from
    build.ninja and glob the matching nm executable.
    """
    nm = "xtensa-esp32s3-elf-nm"
    try:
        with open(os.path.join(BUILD, "build.ninja"), "r", encoding="utf-8", errors="replace") as f:
            data = f.read()
        m = re.search(r"xtensa-esp-elf[\\/](esp-[\d._]+)[\\/]xtensa-esp-elf[\\/]bin", data)
        ver = m.group(1) if m else None
        base = r"C:\Users\ma139\.espressif\tools\xtensa-esp-elf"
        if ver:
            cand = os.path.join(base, ver, "xtensa-esp-elf", "bin", "xtensa-esp32s3-elf-nm.exe")
            if os.path.exists(cand):
                return cand
        hits = glob.glob(os.path.join(base, "*", "xtensa-esp-elf", "bin", "xtensa-esp32s3-elf-nm.exe"))
        if hits:
            return sorted(hits)[-1]
    except OSError:
        pass
    return nm


def main():
    ok = True

    if not os.path.exists(BIN):
        print("FAIL: binary not found:", BIN)
        return 1
    size = os.path.getsize(BIN)
    print("BIN: %s bytes" % size)
    if size < 100_000:
        print("WARN: binary suspiciously small (<100KB)")
        ok = False

    if not os.path.exists(ELF):
        print("FAIL: ELF not found:", ELF)
        return 1

    env = ir.clean_environ()
    ir.merge_path(env, ir.exported_vars(env))

    nm = find_nm()
    print("nm:", nm)

    proc = subprocess.run(
        [nm, "--defined-only", "-C", ELF],
        env=env, cwd=os.getcwd(), capture_output=True, text=True,
    )
    if proc.returncode != 0:
        print("FAIL: nm failed\n", proc.stderr)
        return 1

    allnames = set()
    for line in proc.stdout.splitlines():
        parts = line.split()
        if len(parts) >= 3:
            allnames.add(parts[2])

    missing = [s for s in EXPECTED if s not in allnames]
    if missing:
        print("FAIL: missing N1-6 symbols in image:")
        for m in missing:
            print("   -", m)
        ok = False
    else:
        print("OK: all %d N1-6 symbols present in image" % len(EXPECTED))
        for s in EXPECTED:
            print("   +", s)

    print("RESULT:", "PASS" if ok else "FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
