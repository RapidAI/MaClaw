#!/usr/bin/env python3
"""Render a high-key, tactile coffee sugar cube as deterministic RGB565 + A8.

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
            coarse = 2.6 * (
                math.sin(x * 0.041 + seed * 0.0001) +
                math.sin(y * 0.053 + seed * 0.0002)
            )
            fine = rng.gauss(0.0, 2.6)
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
            if rng.random() < 0.58:
                tone = rng.randrange(249, 256)
                fill = (tone, tone, max(239, tone - 5), rng.randrange(135, 220))
            else:
                # Pores remain biscuit-warm, never grey/black. On the actual
                # NV3023 this is the difference between sugar and a metal box.
                tone = rng.randrange(218, 239)
                fill = (tone, max(207, tone - 4), max(194, tone - 12),
                        rng.randrange(72, 118))
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

    # A very light coffee-coloured contact shadow anchors the white cube.  The
    # shadow is intentionally much paler than a normal product-photo shadow:
    # the 240 px panel crushes low tones, which used to make this mark read as
    # a black technology box instead of something edible.
    shadow = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    sd = ImageDraw.Draw(shadow)
    sd.ellipse((18 * SSAA, 133 * SSAA, 177 * SSAA, 162 * SSAA),
               fill=(218, 174, 119, 42))
    sd.ellipse((43 * SSAA, 142 * SSAA, 157 * SSAA, 158 * SSAA),
               fill=(189, 137, 82, 28))
    shadow = shadow.filter(ImageFilter.GaussianBlur(9.0 * SSAA))
    canvas.alpha_composite(shadow)

    # Slightly irregular, bevelled perspective: a real pressed cube, not a
    # mathematically perfect black box. Shared vertices keep the faces joined.
    top = [(23, 52), (74, 15), (90, 17), (165, 40), (163, 48),
           (107, 81), (98, 84), (30, 64)]
    left = [(23, 52), (30, 64), (98, 84), (102, 146), (96, 153),
            (81, 151), (30, 130), (26, 119)]
    right = [(107, 81), (163, 48), (164, 112), (158, 121), (102, 146),
             (98, 84)]
    top_mask = mask_polygon(top)
    left_mask = mask_polygon(left)
    right_mask = mask_polygon(right)

    # Warm white cane-sugar tones. Even the shaded face remains cream rather
    # than grey, so the mark reads as something dropped into coffee at a glance.
    canvas.alpha_composite(textured_face(left_mask, (255, 255, 248), 0x51A7,
                                         (-0.5, -0.4), 980))
    canvas.alpha_composite(textured_face(right_mask, (255, 251, 239), 0x51A8,
                                         (-0.8, -0.4), 930))
    canvas.alpha_composite(textured_face(top_mask, (255, 255, 252), 0x51A9,
                                         (0.5, -0.7), 1160))

    # Pressed sugar has shallow pits and clumped crystals on all faces. Larger
    # warm pinholes survive the 188x164 -> LCD pipeline and prevent the result
    # from reading as smooth white plastic.
    pores = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    pd = ImageDraw.Draw(pores)
    rng = random.Random(0x5A6A2)
    for mask, count in ((left_mask, 88), (right_mask, 82), (top_mask, 72)):
        pixels = mask.load()
        bounds = mask.getbbox()
        if not bounds:
            continue
        for _ in range(count):
            x = rng.randrange(bounds[0], bounds[2])
            y = rng.randrange(bounds[1], bounds[3])
            if pixels[x, y] < 240:
                continue
            radius = rng.choice((3, 4, 4, 5, 5, 6))
            pd.ellipse((x - radius, y - radius, x + radius, y + radius),
                       fill=(224, 207, 175, rng.randrange(30, 53)))
            pd.ellipse((x - radius // 2, y - radius // 2,
                        x + radius // 3, y + radius // 3),
                       fill=(255, 255, 245, rng.randrange(62, 116)))
    canvas.alpha_composite(pores.filter(ImageFilter.GaussianBlur(0.2 * SSAA)))

    # Broad, low-contrast reflected light makes the front faces feel rounded
    # and porous on the small RGB565 panel. This is deliberately warm studio
    # light, not a dark outline or a coloured status effect.
    bounce = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    bd = ImageDraw.Draw(bounce)
    bd.ellipse((17 * SSAA, 48 * SSAA, 98 * SSAA, 148 * SSAA),
               fill=(255, 255, 244, 30))
    bd.ellipse((104 * SSAA, 66 * SSAA, 178 * SSAA, 150 * SSAA),
               fill=(255, 239, 206, 18))
    bounce = bounce.filter(ImageFilter.GaussianBlur(18 * SSAA))
    face_mask = Image.new("L", (W, H), 0)
    face_mask = Image.composite(left_mask, face_mask, left_mask)
    face_mask = Image.composite(right_mask, face_mask, right_mask)
    bounce.putalpha(Image.composite(bounce.getchannel("A"),
                                    Image.new("L", (W, H), 0), face_mask))
    canvas.alpha_composite(bounce)

    # Sugar crystals on the lit top and along broken pressed edges.
    crystals = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    cd = ImageDraw.Draw(crystals)
    rng = random.Random(0xC0FFEE)
    top_px = top_mask.load()
    for _ in range(430):
        x = rng.randrange(31 * SSAA, 158 * SSAA)
        y = rng.randrange(19 * SSAA, 75 * SSAA)
        if top_px[x, y] < 230:
            continue
        size = rng.choice((3, 3, 4, 4, 5, 5, 6, 7))
        pale = rng.randrange(241, 256)
        cd.polygon([(x, y - size), (x + size, y), (x, y + size), (x - size, y)],
                   fill=(pale, pale, max(232, pale - 6), rng.randrange(125, 220)))
        if rng.random() < 0.45:
            cd.line((x - size, y + size, x + size, y),
                    fill=(194, 184, 164, 70), width=max(1, SSAA // 2))

    # Front-face crystal clumps are essential at device scale: without them,
    # the two vertical faces can read as smooth plastic even if the top looks
    # granular. Keep them sparse and warm so this remains food, not glitter.
    for mask, x_range, y_range, count in (
        (left_mask, (29, 96), (68, 142), 120),
        (right_mask, (105, 160), (62, 132), 95),
    ):
        face_px = mask.load()
        for _ in range(count):
            x = rng.randrange(x_range[0] * SSAA, x_range[1] * SSAA)
            y = rng.randrange(y_range[0] * SSAA, y_range[1] * SSAA)
            if face_px[x, y] < 230:
                continue
            size = rng.choice((2, 3, 3, 4, 4, 5))
            pale = rng.randrange(246, 256)
            cd.polygon([(x, y - size), (x + size, y),
                        (x, y + size), (x - size, y)],
                       fill=(pale, max(242, pale - 2), max(226, pale - 10),
                             rng.randrange(85, 155)))

    # A handful of recognisable loose grains near the base sell the material.
    for x, y, radius in [(20, 145, 2), (37, 153, 1), (150, 143, 2), (166, 137, 1),
                         (119, 159, 1), (70, 158, 2), (178, 149, 1), (51, 146, 1),
                         (139, 155, 1)]:
        xx, yy, rr = x * SSAA, y * SSAA, radius * SSAA
        cd.polygon([(xx, yy - rr), (xx + rr, yy), (xx, yy + rr), (xx - rr, yy)],
                   fill=(246, 243, 228, 220))
        cd.line((xx, yy, xx + rr, yy), fill=(165, 160, 145, 120), width=max(1, SSAA // 2))
    canvas.alpha_composite(crystals)

    # Break the silhouette with a few attached grains.  These are large enough
    # to survive RGB565 and make the perimeter look pressed/crumbly rather than
    # injection-moulded.  They remain almost white, so none can become a dark
    # spot after panel gamma and inversion.
    rim = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    rd = ImageDraw.Draw(rim)
    for x, y, radius in [
        (25, 60, 2), (31, 91, 2), (31, 119, 2), (43, 136, 2),
        (79, 151, 2), (96, 151, 2), (104, 143, 2), (134, 132, 2),
        (159, 113, 2), (162, 82, 2), (160, 48, 2), (142, 43, 2),
        (111, 29, 2), (83, 17, 2), (54, 31, 2),
    ]:
        xx, yy, rr = x * SSAA, y * SSAA, radius * SSAA
        rd.ellipse((xx - rr, yy - rr, xx + rr, yy + rr),
                   fill=(255, 253, 243, 238))
        rd.ellipse((xx - rr // 3, yy - rr // 2,
                    xx + rr // 4, yy + rr // 4),
                   fill=(255, 255, 255, 220))
    canvas.alpha_composite(rim.filter(ImageFilter.GaussianBlur(0.12 * SSAA)))

    # Restrained cream edge definition; there is deliberately no black outline
    # or technology-box seam.
    edge = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    ed = ImageDraw.Draw(edge)
    ed.line(pts([(30, 63), (98, 84), (102, 146)]),
            fill=(255, 255, 250, 220), width=SSAA)
    ed.line(pts([(107, 81), (163, 48)]),
            fill=(255, 254, 245, 180), width=SSAA)
    ed.line(pts([(23, 52), (74, 15), (90, 17)]),
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

    # Also keep a nearest-neighbour 240x240 device mock. It shows the exact
    # RGB565 palette and pixels the NV3023 receives, instead of the smoother
    # full-colour authoring preview above.
    device = Image.new("RGB", (240, 240), (18, 24, 38))
    quantized = Image.new("RGB", (WIDTH, HEIGHT))
    quantized_pixels = []
    for r, g, b, _ in canvas.get_flattened_data():
        quantized_pixels.append((r & 0xF8, g & 0xFC, b & 0xF8))
    quantized.putdata(quantized_pixels)
    scaled_rgb = quantized.resize((188, 164), Image.Resampling.NEAREST)
    scaled_alpha = canvas.getchannel("A").resize((188, 164), Image.Resampling.NEAREST)
    device.paste(scaled_rgb, (26, 8), scaled_alpha)
    device.save(MAIN / "fangtang_sugar_device_preview.png")


if __name__ == "__main__":
    main()
