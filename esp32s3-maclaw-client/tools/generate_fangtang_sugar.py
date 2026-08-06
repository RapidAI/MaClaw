#!/usr/bin/env python3
"""Render a warm, tactile coffee sugar cube as deterministic RGB565 + A8.

The artwork is drawn at 4x resolution and downsampled for the 240x240 NV3023.
The panel driver now applies the proven RGB + INVON + IDMOFF correction, so the
asset stays in canonical RGB565 just like remote pet media. Alpha stays linear
so the same mark can be composited over every device-state background.
"""

from __future__ import annotations

import random
import struct
import math
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter


WIDTH, HEIGHT, SSAA = 188, 164, 4
W, H = WIDTH * SSAA, HEIGHT * SSAA
ROOT = Path(__file__).resolve().parents[1]
MAIN = ROOT / "main"


def pts(values: list[tuple[float, float]]) -> list[tuple[int, int]]:
    return [(round(x * SSAA), round(y * SSAA)) for x, y in values]


def mask_polygon(vertices: list[tuple[float, float]], blur: float = 0.55) -> Image.Image:
    mask = Image.new("L", (W, H), 0)
    ImageDraw.Draw(mask).polygon(pts(vertices), fill=255)
    if blur:
        mask = mask.filter(ImageFilter.GaussianBlur(blur * SSAA))
    return mask


def textured_face(mask: Image.Image, base: tuple[int, int, int], seed: int,
                  light_bias: tuple[float, float], grain_count: int) -> Image.Image:
    rng = random.Random(seed)
    pixels = bytearray(W * H * 4)
    mask_px = mask.load()
    lx, ly = light_bias
    for y in range(H):
        fy = y / max(1, H - 1)
        for x in range(W):
            alpha = mask_px[x, y]
            if not alpha:
                continue
            fx = x / max(1, W - 1)
            # Keep the bulk of the pressed sugar bright. Material definition
            # comes from tiny warm pores and crystalline highlights rather
            # than from dark face shading, which turns muddy on a 240 px LCD.
            # Two scales of noise survive RGB565 without turning into coloured
            # LCD speckle: a soft pressed-sugar body plus individual grains.
            coarse = 2.2 * (
                math.sin(x * 0.041 + seed * 0.0001) +
                math.sin(y * 0.053 + seed * 0.0002)
            )
            fine = rng.gauss(0.0, 2.4)
            directional = (fx - 0.5) * lx + (fy - 0.5) * ly
            idx = (y * W + x) * 4
            for channel, value in enumerate(base):
                pixels[idx + channel] = max(
                    0, min(255, round(value + coarse + fine + directional))
                )
            pixels[idx + 3] = alpha
    image = Image.frombytes("RGBA", (W, H), bytes(pixels))

    # A sugar cube is an aggregate of grains, not a smooth plastic die. Draw
    # target-pixel-sized paired pore/highlight marks so the texture survives
    # the 4x antialiasing pass and RGB565 quantisation on the real panel.
    detail = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    draw = ImageDraw.Draw(detail)
    bounds = mask.getbbox()
    if bounds:
        for _ in range(grain_count):
            x = rng.randrange(bounds[0], bounds[2])
            y = rng.randrange(bounds[1], bounds[3])
            if mask_px[x, y] < 220:
                continue
            radius = rng.choice((3, 3, 4, 4, 5, 6, 7))
            if rng.random() < 0.68:
                tone = rng.randrange(249, 256)
                fill = (tone, tone, max(239, tone - 5), rng.randrange(135, 220))
            else:
                # Pores remain biscuit-warm, never grey/black. On the actual
                # NV3023 this is the difference between sugar and a metal box.
                tone = rng.randrange(214, 236)
                fill = (tone, max(207, tone - 4), max(194, tone - 12),
                        rng.randrange(65, 120))
            draw.ellipse((x - radius, y - radius, x + radius, y + radius), fill=fill)
            if radius >= 4 and rng.random() < 0.58:
                # A pin-point specular glint gives individual grains a glassy,
                # crystalline edge without making the whole cube metallic.
                glint = max(1, radius // 3)
                draw.ellipse((x - radius // 3 - glint, y - radius // 3 - glint,
                              x - radius // 3 + glint, y - radius // 3 + glint),
                             fill=(255, 255, 248, rng.randrange(130, 215)))
    image.alpha_composite(detail)
    return image


def main() -> None:
    canvas = Image.new("RGBA", (W, H), (0, 0, 0, 0))

    # Soft contact shadow, kept neutral so it remains natural on all UI states.
    shadow = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    sd = ImageDraw.Draw(shadow)
    sd.ellipse((24 * SSAA, 137 * SSAA, 171 * SSAA, 159 * SSAA),
               fill=(117, 82, 39, 72))
    shadow = shadow.filter(ImageFilter.GaussianBlur(7.5 * SSAA))
    canvas.alpha_composite(shadow)

    # Slightly irregular, bevelled perspective: a real pressed cube, not a
    # mathematically perfect black box. Shared vertices keep the faces joined.
    top = [(24, 54), (76, 16), (91, 18), (164, 41), (106, 79),
           (99, 82), (31, 63)]
    left = [(24, 54), (31, 63), (99, 82), (102, 147), (95, 153),
            (79, 150), (29, 127)]
    right = [(106, 79), (164, 41), (163, 115), (155, 122), (102, 147),
             (99, 82)]
    top_mask = mask_polygon(top)
    left_mask = mask_polygon(left)
    right_mask = mask_polygon(right)

    # Warm white cane-sugar tones. Even the shaded face remains cream rather
    # than grey, so the mark reads as something dropped into coffee at a glance.
    canvas.alpha_composite(textured_face(left_mask, (255, 249, 232), 0x51A7,
                                         (-4.0, -3.0), 720))
    canvas.alpha_composite(textured_face(right_mask, (250, 243, 226), 0x51A8,
                                         (-5.0, -2.0), 650))
    canvas.alpha_composite(textured_face(top_mask, (255, 253, 240), 0x51A9,
                                         (2.0, -4.0), 820))

    # Sugar crystals on the lit top and along broken pressed edges.
    crystals = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    cd = ImageDraw.Draw(crystals)
    rng = random.Random(0xC0FFEE)
    top_px = top_mask.load()
    for _ in range(280):
        x = rng.randrange(31 * SSAA, 158 * SSAA)
        y = rng.randrange(19 * SSAA, 75 * SSAA)
        if top_px[x, y] < 230:
            continue
        size = rng.choice((3, 3, 4, 4, 5, 6, 7))
        pale = rng.randrange(241, 256)
        cd.polygon([(x, y - size), (x + size, y), (x, y + size), (x - size, y)],
                   fill=(pale, pale, max(232, pale - 6), rng.randrange(125, 220)))
        if rng.random() < 0.45:
            cd.line((x - size, y + size, x + size, y),
                    fill=(194, 184, 164, 70), width=max(1, SSAA // 2))

    # A handful of recognisable loose grains near the base sell the material.
    for x, y, radius in [(24, 145, 2), (39, 151, 1), (151, 141, 2), (164, 137, 1),
                         (119, 157, 1), (72, 157, 2), (175, 148, 1)]:
        xx, yy, rr = x * SSAA, y * SSAA, radius * SSAA
        cd.polygon([(xx, yy - rr), (xx + rr, yy), (xx, yy + rr), (xx - rr, yy)],
                   fill=(246, 243, 228, 220))
        cd.line((xx, yy, xx + rr, yy), fill=(165, 160, 145, 120), width=max(1, SSAA // 2))
    canvas.alpha_composite(crystals)

    # Restrained cream edge definition; there is deliberately no black outline
    # or technology-box seam.
    edge = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    ed = ImageDraw.Draw(edge)
    ed.line(pts([(31, 62), (99, 82), (102, 147)]),
            fill=(255, 253, 242, 190), width=SSAA)
    ed.line(pts([(106, 79), (164, 41)]),
            fill=(255, 254, 245, 180), width=SSAA)
    ed.line(pts([(24, 53), (77, 15), (91, 17)]),
            fill=(255, 255, 248, 125), width=SSAA)
    canvas.alpha_composite(edge.filter(ImageFilter.GaussianBlur(0.2 * SSAA)))

    canvas = canvas.resize((WIDTH, HEIGHT), Image.Resampling.LANCZOS)

    rgb565 = bytearray()
    alpha = bytearray()
    for r, g, b, a in canvas.get_flattened_data():
        # Canonical RGB565; only the wire byte order is swapped for esp_lcd.
        packed = ((r & 0xF8) << 8) | ((g & 0xFC) << 3) | (b >> 3)
        swapped = ((packed << 8) & 0xFFFF) | (packed >> 8)
        rgb565 += struct.pack("<H", swapped)
        alpha.append(a)

    (MAIN / "fangtang_sugar.rgb565").write_bytes(rgb565)
    (MAIN / "fangtang_sugar.a8").write_bytes(alpha)

    # Match the real startup surface. A light preview used to conceal weak
    # edges, while the device background is the dark idle colour.
    preview = Image.new("RGB", (WIDTH, HEIGHT), (18, 24, 38))
    preview.paste(canvas, mask=canvas.getchannel("A"))
    preview.save(MAIN / "fangtang_sugar_preview.png")


if __name__ == "__main__":
    main()
