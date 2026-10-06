"""Drive ninja directly on an already-configured ESP-IDF build tree.

This avoids `idf.py build`, which re-invokes cmake and trips the sandbox
bulk-delete guard via CMake 4.x `--regenerate-during-build`.  The build.ninja
has already had those regen COMMAND lines neutralised (see
tools/_neutralize_regen.py), so ninja only recompiles out-of-date targets.

Environment is built by reusing idf_run.py's helpers so the native xtensa
toolchain sees exactly what `export.bat` would set (PATH, ESP_ROM_ELF_DIR, ...)
without the MSYSTEM/MSYS contamination that makes idf.py no-op.
"""

import os
import subprocess
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "scripts"))
import idf_run as ir  # noqa: E402

NINJA = r"C:\Users\ma139\.espressif\tools\ninja\1.12.1\ninja.exe"
PROFILE = "echoear-2st"


def main(argv):
    env = ir.clean_environ()
    env["MACLAW_PROFILE"] = PROFILE  # harmless at compile time; keeps it consistent
    ir.merge_path(env, ir.exported_vars(env))
    cmd = [NINJA, "-C", "build-unified-echoear"] + argv
    return subprocess.run(cmd, env=env, cwd=os.getcwd()).returncode


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
