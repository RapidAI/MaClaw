#!/usr/bin/env python3
"""Check if Go's pinyin splitting matches lexicon for ALL characters in the pinyin table."""
import os, re

# Load lexicon
lex = {}
with open("corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/lexicon.txt", "r", encoding="utf-8") as f:
    for line in f:
        parts = line.strip().split()
        if len(parts) >= 2 and len(parts[0]) == 1:
            phones = [p for p in parts[1:] if p != "_"]
            lex[parts[0]] = phones

# Load Go pinyin tables
go_pinyin = {}
for fname in ["corelib/tts/g2p_zh.go", "corelib/tts/g2p_zh_pinyin_full.go"]:
    if not os.path.exists(fname):
        continue
    with open(fname, "r", encoding="utf-8") as f:
        content = f.read()
    for m in re.finditer(r"'(.)'\s*:\s*\"(\w+)\"", content):
        ch, py = m.group(1), m.group(2)
        go_pinyin[ch] = py

# Simulate Go's splitPinyinForPiper
def go_to_phones(py):
    py_lower = py.lower()
    tone = "5"
    if py_lower and py_lower[-1] in "12345":
        tone = py_lower[-1]
        py_lower = py_lower[:-1]
    
    initial = ""
    final = py_lower
    
    # Two-char initials
    if len(py_lower) >= 2 and py_lower[:2] in ("zh", "ch", "sh"):
        initial = py_lower[:2]
        final = py_lower[2:]
    elif py_lower and py_lower[0] in "bpmfdtnlgkhjqxywrzcs":
        initial = py_lower[0]
        final = py_lower[1:]
    
    # Map special finals
    if final == "ü":
        final = "v"
    
    phones = []
    if initial:
        phones.append(initial)
    if final:
        phones.append(final)
    phones.append(tone)
    return phones

# Compare for all chars that are in both Go pinyin and lexicon
mismatches = []
total = 0
for ch in sorted(set(go_pinyin.keys()) & set(lex.keys())):
    total += 1
    go_phones = go_to_phones(go_pinyin[ch])
    lex_phones = lex[ch]
    if go_phones != lex_phones:
        mismatches.append((ch, go_pinyin[ch], go_phones, lex_phones))

print(f"Compared {total} characters")
print(f"Mismatches: {len(mismatches)}")

# Group mismatches by type
tone_only = []
initial_diff = []
final_diff = []
for ch, py, go_ph, lex_ph in mismatches:
    if len(go_ph) == len(lex_ph) and go_ph[:-1] == lex_ph[:-1]:
        tone_only.append((ch, py, go_ph, lex_ph))
    elif len(go_ph) >= 1 and len(lex_ph) >= 1 and go_ph[0] != lex_ph[0]:
        initial_diff.append((ch, py, go_ph, lex_ph))
    else:
        final_diff.append((ch, py, go_ph, lex_ph))

print(f"\nTone-only mismatches ({len(tone_only)}):")
for ch, py, go_ph, lex_ph in tone_only[:10]:
    print(f"  '{ch}' ({py}): Go={go_ph} Lex={lex_ph}")

print(f"\nInitial mismatches ({len(initial_diff)}):")
for ch, py, go_ph, lex_ph in initial_diff[:10]:
    print(f"  '{ch}' ({py}): Go={go_ph} Lex={lex_ph}")

print(f"\nFinal/structure mismatches ({len(final_diff)}):")
for ch, py, go_ph, lex_ph in final_diff[:20]:
    print(f"  '{ch}' ({py}): Go={go_ph} Lex={lex_ph}")
