from __future__ import annotations

from pathlib import Path
import re
import struct

from PIL import Image, ImageDraw, ImageFont


ROOT = Path(__file__).resolve().parents[1]
FONT_PATH = Path(r"C:\Windows\Fonts\msyh.ttc")
OUTPUT = ROOT / "main" / "font_cjk24.h"
BINARY_OUTPUT = ROOT / "data" / "cjk24_cjk.bin"
EMBEDDED_BINARY_OUTPUT = ROOT / "main" / "cjk24_cjk.bin"
SOURCE_FILES = (
    ROOT / "main" / "main.c",
    ROOT / "main" / "board_port.c",
    ROOT / "main" / "board_port_bread_compact.c",
)
# Weather summaries are supplied by MaClaw GUI at runtime, so they cannot all
# be discovered by scanning firmware string literals. Keep every character
# emitted by openMeteoWeatherSummary() here to prevent a valid Hub payload from
# rendering as a missing-glyph box on the device.
EXTRA_CHARACTERS = (
    "\u5317\u4eac\u4e0a\u6d77\u5929\u6d25\u91cd\u5e86\u5e7f\u5dde\u6df1\u5733\u676d\u5dde\u5357\u4eac\u6b66\u6c49\u6210\u90fd\u897f\u5b89\u90d1\u5dde\u6d4e\u5357\u6c88\u9633\u5927\u8fde\u957f\u6625\u54c8\u5c14\u6ee8"
    "\u798f\u5dde\u53a6\u95e8\u5357\u660c\u957f\u6c99\u6606\u660e\u8d35\u9633\u5357\u5b81\u6d77\u53e3\u77f3\u5bb6\u5e84\u592a\u539f\u547c\u548c\u6d69\u7279\u5170\u5dde\u897f\u5b81\u94f6\u5ddd\u62c9\u8428"
    "\u4e4c\u9c81\u6728\u9f50\u56fe\u68ee\u672c\u5730"
    "\u6674\u9634\u591a\u4e91\u5c11\u4e91\u5c0f\u96e8\u4e2d\u96e8\u5927\u96e8\u66b4\u96e8\u9635\u96e8\u96f7\u96e8\u96e8\u5939\u96ea\u5c0f\u96ea\u4e2d\u96ea\u5927\u96ea\u66b4\u96ea\u96fe\u973e\u51b0\u96f9\u98ce\u6c99\u6d6e\u5c18\u5e72\u71e5\u6e7f\u6da6"
    "，。！？：；、‘’“”（）【】《》〈〉—…·℃°～•"
)


def collect_characters() -> list[str]:
    chars: set[str] = set(EXTRA_CHARACTERS)
    for source in SOURCE_FILES:
        text = source.read_text(encoding="utf-8")
        for literal in re.findall(r'"((?:[^"\\]|\\.)*)"', text):
            chars.update(ch for ch in literal if ord(ch) >= 0x80)
    return sorted(chars, key=ord)


def glyph_rows(font: ImageFont.FreeTypeFont, char: str) -> list[int]:
    image = Image.new("1", (24, 24), 0)
    draw = ImageDraw.Draw(image)
    draw.text((0, 0), char, fill=1, font=font, anchor="lt")
    # Pillow's `lt` anchor uses the glyph's tight ink box. For the single-line
    # ideograph 一 that puts the stroke at the top of the em square, unlike its
    # normal visual position inside words such as 星期一. Keep generated fonts
    # consistent with the firmware-side correction.
    if char == "一":
        image = image.transform(
            image.size, Image.Transform.AFFINE, (1, 0, 0, 0, 1, -10),
            resample=Image.Resampling.NEAREST,
        )
    return [
        sum((1 << (23 - x)) for x in range(24) if image.getpixel((x, y)))
        for y in range(24)
    ]


def main() -> None:
    if not FONT_PATH.exists():
        raise SystemExit(f"font not found: {FONT_PATH}")
    font = ImageFont.truetype(str(FONT_PATH), 24)
    chars = collect_characters()
    lines = [
        "#pragma once",
        "",
        "#include <stdint.h>",
        "",
        "typedef struct { uint32_t codepoint; uint32_t rows[24]; } maclaw_cjk24_glyph_t;",
        "",
        f"static const maclaw_cjk24_glyph_t s_maclaw_cjk24[{len(chars)}] = {{",
    ]
    for char in chars:
        rows = ",".join(f"0x{row:06X}" for row in glyph_rows(font, char))
        lines.append(f"    {{0x{ord(char):04X}, {{{rows}}}}}, // U+{ord(char):04X}")
    lines.extend(("};", ""))
    OUTPUT.write_text("\n".join(lines), encoding="utf-8")
    # Keep the complete CJK Unified Ideographs block in a packed 24x24 format:
    # 20,992 glyphs x 24 rows x 3 bytes = about 1.5 MiB.  The compact board
    # display boards embed this binary in their application partition, so
    # arbitrary Chinese replies render even when the writable meeting-recording
    # SPIFFS partition is deliberately preserved during an app-only update.
    packed = bytearray()
    for codepoint in range(0x4E00, 0xA000):
        for row in glyph_rows(font, chr(codepoint)):
            packed.extend(((row >> 16) & 0xFF, (row >> 8) & 0xFF, row & 0xFF))
    for destination in (BINARY_OUTPUT, EMBEDDED_BINARY_OUTPUT):
        destination.parent.mkdir(exist_ok=True)
        destination.write_bytes(packed)
    print(
        f"generated {OUTPUT} with {len(chars)} glyphs and "
        f"{EMBEDDED_BINARY_OUTPUT} ({len(packed)} bytes)"
    )


if __name__ == "__main__":
    main()
