from __future__ import annotations

from pathlib import Path
import argparse
import re
import struct

from PIL import Image, ImageDraw, ImageFont


ROOT = Path(__file__).resolve().parents[1]
FONT_PATH = Path(r"C:\Windows\Fonts\msyh.ttc")
SOURCE_FILES = (
    ROOT / "main" / "main.c",
    ROOT / "main" / "board_port.c",
    ROOT / "main" / "compact_renderer.c",
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


def glyph_rows(font: ImageFont.FreeTypeFont, char: str, size: int) -> list[int]:
    image = Image.new("1", (size, size), 0)
    draw = ImageDraw.Draw(image)
    draw.text((0, 0), char, fill=1, font=font, anchor="lt")
    # Pillow's `lt` anchor uses the glyph's tight ink box. For the single-line
    # ideograph 一 that puts the stroke at the top of the em square, unlike its
    # normal visual position inside words such as 星期一. Keep generated fonts
    # consistent with the firmware-side correction.
    if char == "一":
        image = image.transform(
            image.size, Image.Transform.AFFINE, (1, 0, 0, 0, 1, -(size * 10 // 24)),
            resample=Image.Resampling.NEAREST,
        )
    return [
        sum((1 << (size - 1 - x)) for x in range(size) if image.getpixel((x, y)))
        for y in range(size)
    ]


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Generate packed CJK bitmap resources for MaClaw displays.")
    parser.add_argument("--size", type=int, choices=(24, 32), default=24,
                        help="square glyph size in pixels (default: 24)")
    size = parser.parse_args().size
    output = ROOT / "main" / f"font_cjk{size}.h"
    embedded_binary_output = ROOT / "main" / f"cjk{size}_cjk.bin"
    if not FONT_PATH.exists():
        raise SystemExit(f"font not found: {FONT_PATH}")
    font = ImageFont.truetype(str(FONT_PATH), size)
    chars = collect_characters()
    lines = [
        "#pragma once",
        "",
        "#include <stdint.h>",
        "",
        f"typedef struct {{ uint32_t codepoint; uint32_t rows[{size}]; }} maclaw_cjk{size}_glyph_t;",
        "",
        f"static const maclaw_cjk{size}_glyph_t s_maclaw_cjk{size}[{len(chars)}] = {{",
    ]
    for char in chars:
        rows = ",".join(
            f"0x{row:0{size // 4}X}" for row in glyph_rows(font, char, size))
        lines.append(f"    {{0x{ord(char):04X}, {{{rows}}}}}, // U+{ord(char):04X}")
    lines.extend(("};", ""))
    output.write_text("\n".join(lines), encoding="utf-8")
    # Keep the complete CJK Unified Ideographs block in the selected native
    # raster format. Profile CMake selects exactly one fallback asset:
    # Waveshare uses 32x32; other boards retain 24x24 to protect flash budget.
    row_bytes = size // 8
    packed = bytearray()
    for codepoint in range(0x4E00, 0xA000):
        for row in glyph_rows(font, chr(codepoint), size):
            packed.extend(row.to_bytes(row_bytes, "big"))
    # The fallback is linked into the app image. Do not mirror a 32-dot font
    # into SPIFFS: Waveshare reserves that partition for user assets/records.
    destinations = ((ROOT / "data" / "cjk24_cjk.bin"), embedded_binary_output)
    if size != 24:
        destinations = (embedded_binary_output,)
    for destination in destinations:
        destination.parent.mkdir(exist_ok=True)
        destination.write_bytes(packed)
    print(f"generated {output} with {len(chars)} glyphs and "
          f"{embedded_binary_output} ({len(packed)} bytes)")


if __name__ == "__main__":
    main()
