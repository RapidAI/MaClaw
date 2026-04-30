#!/usr/bin/env python3
"""Systematically compare Go G2P fallback vs lexicon for common characters."""
import os, subprocess, json

# Load lexicon
lex = {}
with open("corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/lexicon.txt", "r", encoding="utf-8") as f:
    for line in f:
        parts = line.strip().split()
        if len(parts) >= 2:
            chars = parts[0]
            if len(chars) == 1:
                phones = [p for p in parts[1:] if p != "_"]
                lex[chars] = phones

pid_map = {
    "_": 0, "^": 1, "$": 2, "Ø": 3,
    "b": 4, "p": 5, "m": 6, "f": 7, "d": 8, "t": 9, "n": 10, "l": 11,
    "g": 12, "k": 13, "h": 14, "j": 15, "q": 16, "x": 17,
    "zh": 18, "ch": 19, "sh": 20, "r": 21, "z": 22, "c": 23, "s": 24,
    "y": 25, "w": 26,
    "a": 27, "o": 28, "e": 29, "ai": 30, "ei": 31, "ao": 32, "ou": 33,
    "an": 34, "en": 35, "ang": 36, "eng": 37, "ong": 38,
    "i": 39, "ia": 40, "ie": 41, "iao": 42, "iu": 43, "ian": 44, "in": 45,
    "iang": 46, "ing": 47, "iong": 48,
    "u": 49, "ua": 50, "uo": 51, "uai": 52, "ui": 53, "uan": 54, "un": 55,
    "uang": 56, "ueng": 57,
    "v": 58, "ve": 59, "van": 60, "vn": 61, "er": 62, "ue": 63,
    "1": 64, "2": 65, "3": 66, "4": 67, "5": 68,
}

# Load Go pinyin tables to simulate Go G2P fallback
# Read g2p_zh.go and g2p_zh_pinyin_full.go to extract pinyin mappings
go_pinyin = {}
for fname in ["corelib/tts/g2p_zh.go", "corelib/tts/g2p_zh_pinyin_full.go"]:
    if not os.path.exists(fname):
        continue
    with open(fname, "r", encoding="utf-8") as f:
        content = f.read()
    # Parse entries like '你': "ni3"
    import re
    for m in re.finditer(r"'(.)'\s*:\s*\"(\w+)\"", content):
        ch, py = m.group(1), m.group(2)
        go_pinyin[ch] = py

print(f"Lexicon: {len(lex)} chars, Go pinyin: {len(go_pinyin)} chars")

# Simulate Go's splitPinyinForPiper
def go_split(py):
    py = py.lower()
    # Extract tone
    tone = "5"
    if py and py[-1] in "12345":
        tone = py[-1]
        py = py[:-1]
    
    # Two-char initials
    if len(py) >= 2 and py[:2] in ("zh", "ch", "sh"):
        return py[:2], py[2:], tone
    # Single-char initials
    if py and py[0] in "bpmfdtnlgkhjqxywrzcs":
        return py[0], py[1:], tone
    return "", py, tone

# Compare for test texts
test_texts = ["你好世界", "今天天气不错", "我们一起来写代码吧", "欢迎使用智能助手", "人工智能正在改变世界"]

mismatches = []
for text in test_texts:
    for ch in text:
        if ch not in lex:
            continue
        lex_phones = lex[ch]
        
        # Simulate Go G2P
        if ch in go_pinyin:
            py = go_pinyin[ch]
            initial, final, tone = go_split(py)
            go_phones = []
            if initial:
                go_phones.append(initial)
            if final:
                go_phones.append(final)
            go_phones.append(tone)
        else:
            go_phones = ["?"]
        
        if go_phones != lex_phones:
            mismatches.append((ch, go_phones, lex_phones))
            print(f"  MISMATCH '{ch}': Go={go_phones} Lex={lex_phones}")

if not mismatches:
    print("All characters match!")
else:
    print(f"\n{len(mismatches)} mismatches found")
    
# Also check some common problem characters
print("\n=== Common problem characters ===")
problem_chars = "的了不一是在有这个人我他她它们你好大小上下来去说做看想要能会可以"
for ch in problem_chars:
    if ch in lex and ch in go_pinyin:
        py = go_pinyin[ch]
        initial, final, tone = go_split(py)
        go_phones = []
        if initial:
            go_phones.append(initial)
        if final:
            go_phones.append(final)
        go_phones.append(tone)
        lex_phones = lex[ch]
        if go_phones != lex_phones:
            print(f"  '{ch}': Go={go_phones} Lex={lex_phones}")
