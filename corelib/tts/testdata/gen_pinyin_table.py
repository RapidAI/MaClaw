#!/usr/bin/env python3
"""Generate a comprehensive pinyin table for Go."""
from pypinyin import pinyin, Style

lines = []
for cp in range(0x4E00, 0x9FFF + 1):
    c = chr(cp)
    try:
        py = pinyin(c, style=Style.TONE3, neutral_tone_with_five=True)
        if py and py[0] and py[0][0]:
            p = py[0][0]
            lines.append(f"\t'{c}': \"{p}\",")
    except:
        pass

with open("corelib/tts/g2p_zh_pinyin_data.txt", "w", encoding="utf-8") as f:
    for line in lines:
        f.write(line + "\n")

print(f"Generated {len(lines)} pinyin entries")
