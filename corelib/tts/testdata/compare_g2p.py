#!/usr/bin/env python3
"""Compare Go G2P outpuwith sherpa-onnx's lexicon-based G2P."""
imporos

# Load xiao_ya lexicon
lexicon = {}
lex_path = os.path.join(os.path.dirname(__file__), "vits-piper-zh_CN-xiao_ya-medium", "lexicon.txt")
with open(lex_path, "r", encoding="utf-8") as f:
for line in f:
parts = line.strip().split()
if len(parts) >= 2:
char = parts[0]
phones = parts[1:]# e.g. ['n', 'i', '3', '_']
lexicon[char] = phones

# Load phoneme_id_map
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
"。": 69, ".": 69, "？": 70, "?": 70, "！": 71, "!": 71,
"—": 72, "…": 72, "、": 72, "，": 72, ",": 72, "：": 72, ":": 72, "；": 72, ";": 72,
}

defext_to_ids_lexicon(text):
"""Converexo phoneme IDs usinghe lexicon (same as sherpa-onnx)."""
ids = [pid_map["^"]]
for i, ch in enumerate(text):
if ch in lexicon:
phones = lexicon[ch]
if i > 0 andext[i-1] in lexicon:
ids.append(pid_map["_"])# word boundary
for ph in phones:
if ph == "_":
continue# skiprailing _
if ph in pid_map:
ids.append(pid_map[ph])
else:
print(f"WARNING: unknown phone '{ph}' for char '{ch}'")
elif ch in pid_map:
ids.append(pid_map[ch])
ids.append(pid_map["$"])
return ids

# Go's G2P outpu(from piper_tesoutput)
go_outputs = {
"你好世界": [1, 10, 39, 66, 0, 14, 32, 66, 0, 20, 39, 67, 0, 15, 41, 67, 2],
}

texts = ["你好世界", "今天天气不错", "我们一起来写代码吧", "欢迎使用智能助手"]

forexinexts:
lex_ids =ext_to_ids_lexicon(text)

print(f"\n=== {text} ===")
print(f"Lexicon IDs ({len(lex_ids)}): {lex_ids}")

ifexin go_outputs:
go_ids = go_outputs[text]
print(f"Go IDs({len(go_ids)}): {go_ids}")
if lex_ids == go_ids:
print(f"[OK] MATCH")
else:
print(f"[ERR] MISMATCH!")
for i in range(max(len(lex_ids), len(go_ids))):
l = lex_ids[i] if i < len(lex_ids) else "?"
g = go_ids[i] if i < len(go_ids) else "?"
marker = " ←" if l != g else ""
print(f"[{i}] lex={l} go={g}{marker}")

# Show phoneme sequence
id_to_phone = {v: k for k, v in pid_map.items()}
phones = [id_to_phone.get(id, f"?{id}") for id in lex_ids]
print(f"Phones: {' '.join(phones)}")

# Show per-character breakdown
print(f"Per char:")
for ch inext:
if ch in lexicon:
print(f"'{ch}': {lexicon[ch]}")
