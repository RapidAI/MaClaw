#!/usr/bin/env python3
"""Build the Bread Compact 240x320 RGB565 startup artwork."""

from __future__ import annotations

import math
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter, ImageFont


ROOT = Path(__file__).resolve().parents[1]
OUTPUT_RGB565 = ROOT / "main" / "bread_compact_splash.rgb565"
OUTPUT_PREVIEW = ROOT / "main" / "bread_compact_splash_preview.png"
WIDTH, HEIGHT = 240, 320
SUPERSAMPLE = 4


def font(size: int, bold: bool = False) -> ImageFont.FreeTypeFont:
    names = [
        "C:/Windows/Fonts/segoeuib.ttf" if bold else "C:/Windows/Fonts/segoeui.ttf",
        "C:/Windows/Fonts/arialbd.ttf" if bold else "C:/Windows/Fonts/arial.ttf",
    ]
    for name in names:
        if Path(name).exists():
            return ImageFont.truetype(name, size)
    return ImageFont.load_default()


def rounded(draw: ImageDraw.ImageDraw, box, radius, fill, outline=None, width=1):
    draw.rounded_rectangle(tuple(int(value * SUPERSAMPLE) for value in box),
                           radius * SUPERSAMPLE, fill=fill, outline=outline,
                           width=width * SUPERSAMPLE)


def ellipse(draw: ImageDraw.ImageDraw, box, fill, outline=None, width=1):
    draw.ellipse(tuple(int(value * SUPERSAMPLE) for value in box), fill=fill,
                 outline=outline, width=width * SUPERSAMPLE)


def make_artwork() -> Image.Image:
    w, h = WIDTH * SUPERSAMPLE, HEIGHT * SUPERSAMPLE
    image = Image.new("RGB", (w, h), (5, 13, 24))
    pixels = image.load()
    for y in range(h):
        normalized_y = y / max(h - 1, 1)
        for x in range(w):
            dx = (x / SUPERSAMPLE - WIDTH / 2) / 150
            dy = (y / SUPERSAMPLE - 123) / 190
            glow = max(0.0, 1.0 - math.sqrt(dx * dx + dy * dy))
            vignette = max(0.0, ((x / SUPERSAMPLE - WIDTH / 2) / 170) ** 2 +
                           ((y / SUPERSAMPLE - HEIGHT / 2) / 250) ** 2)
            pixels[x, y] = (
                int(5 + 6 * glow),
                int(13 + 22 * glow - 3 * normalized_y),
                int(24 + 38 * glow - 7 * vignette),
            )

    glow_layer = Image.new("RGBA", image.size, (0, 0, 0, 0))
    glow_draw = ImageDraw.Draw(glow_layer)
    ellipse(glow_draw, (43, 33, 197, 221), (22, 179, 227, 46))
    glow_layer = glow_layer.filter(ImageFilter.GaussianBlur(24 * SUPERSAMPLE))
    image = Image.alpha_composite(image.convert("RGBA"), glow_layer)

    shadow = Image.new("RGBA", image.size, (0, 0, 0, 0))
    shadow_draw = ImageDraw.Draw(shadow)
    ellipse(shadow_draw, (52, 205, 188, 232), (0, 0, 0, 150))
    shadow = shadow.filter(ImageFilter.GaussianBlur(11 * SUPERSAMPLE))
    image = Image.alpha_composite(image, shadow)

    robot = Image.new("RGBA", image.size, (0, 0, 0, 0))
    draw = ImageDraw.Draw(robot)

    # Antenna, illuminated tip, ear pods and collar establish a tangible device.
    rounded(draw, (114, 25, 126, 65), 6, (73, 101, 122, 255),
            (147, 187, 207, 255), 1)
    ellipse(draw, (108, 15, 132, 39), (46, 207, 238, 255),
            (188, 249, 255, 255), 2)
    ellipse(draw, (114, 21, 126, 33), (235, 255, 255, 255))
    ellipse(draw, (20, 101, 61, 155), (48, 76, 98, 255),
            (116, 171, 197, 255), 2)
    ellipse(draw, (179, 101, 220, 155), (48, 76, 98, 255),
            (116, 171, 197, 255), 2)
    ellipse(draw, (29, 111, 51, 145), (16, 52, 72, 255),
            (70, 208, 231, 255), 2)
    ellipse(draw, (189, 111, 211, 145), (16, 52, 72, 255),
            (70, 208, 231, 255), 2)
    rounded(draw, (91, 199, 149, 225), 11, (48, 76, 97, 255),
            (129, 170, 190, 255), 2)

    # Layered pearl-metal shell with highlights and cool edge reflections.
    rounded(draw, (35, 48, 205, 207), 43, (54, 81, 101, 255),
            (126, 172, 195, 255), 2)
    rounded(draw, (43, 55, 197, 199), 36, (177, 200, 213, 255),
            (233, 247, 251, 255), 2)
    rounded(draw, (49, 61, 191, 193), 31, (130, 163, 181, 255))
    rounded(draw, (52, 62, 188, 184), 29, (216, 231, 237, 255))
    draw.arc((51 * SUPERSAMPLE, 57 * SUPERSAMPLE, 184 * SUPERSAMPLE,
              181 * SUPERSAMPLE), 205, 292, fill=(250, 255, 255, 235),
             width=3 * SUPERSAMPLE)

    # Deep glass face with a real bezel, reflection and cyan light language.
    rounded(draw, (60, 79, 180, 170), 26, (18, 60, 79, 255),
            (78, 208, 232, 255), 2)
    rounded(draw, (66, 85, 174, 164), 21, (3, 19, 33, 255),
            (37, 101, 126, 255), 1)
    rounded(draw, (72, 90, 168, 108), 10, (55, 93, 114, 72))

    for cx in (96, 144):
        ellipse(draw, (cx - 16, 108, cx + 16, 137), (20, 105, 133, 255))
        ellipse(draw, (cx - 12, 110, cx + 12, 135), (44, 220, 244, 255))
        ellipse(draw, (cx - 7, 113, cx + 7, 132), (196, 252, 255, 255))
        ellipse(draw, (cx - 3, 118, cx + 4, 129), (247, 255, 255, 255))
    draw.arc((91 * SUPERSAMPLE, 129 * SUPERSAMPLE, 149 * SUPERSAMPLE,
              160 * SUPERSAMPLE), 20, 160, fill=(78, 224, 243, 255),
             width=3 * SUPERSAMPLE)
    ellipse(draw, (69, 143, 82, 156), (41, 169, 194, 160))
    ellipse(draw, (158, 143, 171, 156), (41, 169, 194, 160))
    ellipse(draw, (56, 181, 66, 191), (54, 220, 236, 255))
    ellipse(draw, (174, 181, 184, 191), (54, 220, 236, 255))

    # Small physical seams stop the head from reading as a flat icon.
    draw.line((84 * SUPERSAMPLE, 196 * SUPERSAMPLE, 156 * SUPERSAMPLE,
               196 * SUPERSAMPLE), fill=(74, 103, 121, 230), width=2 * SUPERSAMPLE)

    image = Image.alpha_composite(image, robot)

    text_layer = Image.new("RGBA", image.size, (0, 0, 0, 0))
    text_draw = ImageDraw.Draw(text_layer)
    title_font = font(25 * SUPERSAMPLE, bold=True)
    subtitle_font = font(9 * SUPERSAMPLE)
    title = "MaClaw Mate"
    subtitle = "YOUR AI COMPANION"
    title_box = text_draw.textbbox((0, 0), title, font=title_font)
    subtitle_box = text_draw.textbbox((0, 0), subtitle, font=subtitle_font)
    text_draw.text(((w - (title_box[2] - title_box[0])) / 2, 239 * SUPERSAMPLE),
                   title, font=title_font, fill=(242, 248, 251, 255),
                   stroke_width=1 * SUPERSAMPLE, stroke_fill=(5, 18, 30, 180))
    text_draw.text(((w - (subtitle_box[2] - subtitle_box[0])) / 2, 278 * SUPERSAMPLE),
                   subtitle, font=subtitle_font, fill=(115, 193, 213, 255))
    text_draw.rounded_rectangle((86 * SUPERSAMPLE, 302 * SUPERSAMPLE,
                                 154 * SUPERSAMPLE, 304 * SUPERSAMPLE),
                                radius=SUPERSAMPLE, fill=(57, 189, 218, 210))
    image = Image.alpha_composite(image, text_layer)
    return image.convert("RGB").resize((WIDTH, HEIGHT), Image.Resampling.LANCZOS)


def rgb565_bytes(image: Image.Image) -> bytes:
    output = bytearray(WIDTH * HEIGHT * 2)
    offset = 0
    for red, green, blue in image.get_flattened_data():
        value = ((red & 0xF8) << 8) | ((green & 0xFC) << 3) | (blue >> 3)
        # The LCD driver consumes byte-swapped RGB565, matching board_port color().
        output[offset] = value >> 8
        output[offset + 1] = value & 0xFF
        offset += 2
    return bytes(output)


def main() -> None:
    artwork = make_artwork()
    artwork.save(OUTPUT_PREVIEW, optimize=True)
    OUTPUT_RGB565.write_bytes(rgb565_bytes(artwork))
    print(f"wrote {OUTPUT_PREVIEW} and {OUTPUT_RGB565} ({OUTPUT_RGB565.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
