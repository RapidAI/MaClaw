import os

p = "build-unified-echoear/build.ninja"
bak = p + ".bak-regen"
if not os.path.exists(bak):
    with open(p, "r", encoding="utf-8", errors="replace") as f:
        data = f.read()
    with open(bak, "w", encoding="utf-8") as f:
        f.write(data)
    print("backup written:", bak)

lines = data.split("\n")
noop = "  COMMAND = rem # regen disabled for build-bot guard"
out = []
n = 0
for ln in lines:
    if "--regenerate-during-build" in ln:
        out.append(noop)
        n += 1
    else:
        out.append(ln)
with open(p, "w", encoding="utf-8") as f:
    f.write("\n".join(out))
print("neutralized", n, "regen command lines")
