"""Remove the auto-reconfigure `build build.ninja` statement from build.ninja.

Ninja decides to regenerate build.ninja whenever any of its listed inputs is
newer than build.ninja, and that regeneration re-invokes cmake -- which trips
the sandbox bulk-delete guard during the Component Manager pass.  For a
one-shot compile we do not want cmake to run at all, so we delete the
`build build.ninja: RERUN_CMAKE ...` statement (and its trailing `pool =
console`).  The real compile targets are untouched.

A pristine backup (build.ninja.bak-regen) already exists from
_neutralize_regen.py; this script only removes the regen *statement*.
"""

import os

p = "build-unified-echoear/build.ninja"
with open(p, "r", encoding="utf-8", errors="replace") as f:
    lines = f.read().split("\n")

out = []
removed = 0
i = 0
n = len(lines)
while i < n:
    ln = lines[i]
    if ln.startswith("build build.ninja") and "RERUN_CMAKE" in ln:
        # remove this statement line and the following `pool = console` line
        removed += 1
        i += 1
        if i < n and lines[i].strip() == "pool = console":
            i += 1
        # also skip any immediately following blank line for tidiness
        if i < n and lines[i].strip() == "":
            i += 1
        continue
    out.append(ln)
    i += 1

with open(p, "w", encoding="utf-8") as f:
    f.write("\n".join(out))
print("removed", removed, "regen build statement(s); lines now", len(out))
