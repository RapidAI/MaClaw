#!/usr/bin/env python3
"""Generate the stress corpus for the native PP-OCRv6 engine.

Run from the repo root:
    python3 corelib/ocr/testdata/stress/gen_stress.py

Regenerates all PNGs in this directory. Deterministic (fixed seeds).
"""
import math
import os
import random

from PIL import Image, ImageDraw, ImageFilter, ImageFont

OUT = os.path.dirname(os.path.abspath(__file__))
FONT_DIRS = [r"C:\Windows\Fonts", "/usr/share/fonts", "/Library/Fonts"]


def font(names, size):
    for d in FONT_DIRS:
        for n in names:
            p = os.path.join(d, n)
            if os.path.exists(p):
                return ImageFont.truetype(p, size)
    return ImageFont.load_default()


def sans(size):
    return font(["arial.ttf", "DejaVuSans.ttf"], size)


def mono(size):
    return font(["consola.ttf", "DejaVuSansMono.ttf"], size)


def cjk(size):
    return font(["msyh.ttc", "simsun.ttc", "PingFang.ttc", "NotoSansCJK-Regular.ttc"], size)


def save(img, name, **kw):
    img.save(os.path.join(OUT, name), optimize=True, **kw)
    print(name, img.size, os.path.getsize(os.path.join(OUT, name)) // 1024, "KB")


WORDS = ("File Edit View Run Terminal Help Debug Build Search Settings Account "
         "OK Cancel Apply Retry Submit Save Open Close Print Export Import").split()
LOREM = ("The quick brown fox jumps over the lazy dog while processing 42 "
         "records per second in the background daemon.").split()


def ui_dense():
    """1920x1080 IDE-like screenshot: toolbar, sidebar, many small text lines."""
    rng = random.Random(11)
    img = Image.new("RGB", (1920, 1080), (30, 32, 36))
    d = ImageDraw.Draw(img)
    # title / toolbar
    d.rectangle([0, 0, 1920, 44], fill=(45, 48, 54))
    x = 16
    for i, w in enumerate(WORDS[:8]):
        d.text((x, 14), w, font=sans(18), fill=(210, 214, 220))
        x += d.textlength(w, font=sans(18)) + 34
    # sidebar
    d.rectangle([0, 44, 240, 1080], fill=(37, 40, 45))
    for i in range(38):
        d.text((18 + (i % 3) * 12, 60 + i * 26),
               f"module_{i:02d}_{rng.choice(WORDS).lower()}.go",
               font=mono(15), fill=(160, 168, 178))
    # editor gutter + code lines
    for i in range(40):
        y = 60 + i * 25
        d.text((258, y), f"{i + 1:3d}", font=mono(15), fill=(90, 96, 105))
        indent = "    " * (i % 4)
        line = (f"{indent}func {rng.choice(WORDS).lower()}{i}(ctx *Context) error "
                f"{{ // {rng.choice(LOREM)} {rng.choice(LOREM)} {i * 7 % 1000}")
        d.text((310, y), line, font=mono(15), fill=(200, 205, 212))
    # status bar
    d.rectangle([0, 1048, 1920, 1080], fill=(20, 90, 160))
    d.text((12, 1056), "Ln 40, Col 12   UTF-8   Go   0 errors 3 warnings",
           font=sans(16), fill=(240, 244, 250))
    return img


def blank():
    img = Image.new("RGB", (800, 600), (245, 246, 248))
    d = ImageDraw.Draw(img)
    d.rectangle([100, 100, 700, 500], outline=(200, 205, 210))  # border only, no text
    return img


def single_word():
    img = Image.new("RGB", (320, 96), (255, 255, 255))
    ImageDraw.Draw(img).text((30, 26), "Settings", font=sans(40), fill=(20, 20, 20))
    return img


def wide_strip():
    img = Image.new("RGB", (3000, 80), (255, 255, 255))
    d = ImageDraw.Draw(img)
    d.text((20, 22), "Invoice Total 1,234.56 CNY paid on 2026-08-07 ref A20240807",
           font=mono(30), fill=(0, 0, 0))
    return img


def tall_strip():
    img = Image.new("RGB", (80, 3000), (255, 255, 255))
    d = ImageDraw.Draw(img)
    for i in range(20):
        # vertical stack of short rotated words
        t = Image.new("RGB", (200, 44), (255, 255, 255))
        ImageDraw.Draw(t).text((8, 8), f"row {i:02d}", font=sans(26), fill=(0, 0, 0))
        t = t.rotate(90, expand=True)
        img.paste(t, (14, 20 + i * 148))
    return img


def tiny():
    img = Image.new("RGB", (16, 16), (255, 255, 255))
    ImageDraw.Draw(img).text((1, 1), "A", font=sans(10), fill=(0, 0, 0))
    return img


def noisy_lowcontrast():
    """Photo-like: gradient background + heavy noise + low-contrast text."""
    rng = random.Random(23)
    img = Image.new("RGB", (1024, 640))
    px = img.load()
    for y in range(640):
        for x in range(1024):
            base = 120 + int(60 * math.sin(x / 200.0)) + int(40 * y / 640)
            n = rng.randint(-28, 28)
            v = max(0, min(255, base + n))
            px[x, y] = (v, min(255, v + 8), max(0, v - 6))
    img = img.filter(ImageFilter.GaussianBlur(0.6))
    d = ImageDraw.Draw(img)
    lines = ["Exit sign level 3", "Door code 4581", "Keep clear zone"]
    for i, s in enumerate(lines):
        d.text((80, 120 + i * 140), s, font=sans(52), fill=(70, 74, 70))
    img = img.filter(ImageFilter.GaussianBlur(0.8))
    return img


def rotated():
    """Text lines rotated 10-30 degrees on a white canvas."""
    img = Image.new("RGB", (1280, 800), (255, 255, 255))
    lines = [("Quarterly report 2026", 10), ("Revenue up 12 percent", -15),
             ("Meeting at 14:30 sharp", 20), ("Draft version 7 final", -25),
             ("Handle with care", 30)]
    y = 40
    for s, deg in lines:
        t = Image.new("RGBA", (700, 70), (0, 0, 0, 0))
        ImageDraw.Draw(t).text((10, 12), s, font=sans(36), fill=(10, 10, 10, 255))
        t = t.rotate(deg, expand=True, resample=Image.BICUBIC)
        img.paste(t, (120, y), t)
        y += 150
    return img


def dense_blobs():
    """Grid of tiny text blobs; det must hit (or approach) the 3000-candidate cap."""
    rng = random.Random(31)
    img = Image.new("RGB", (2000, 2000), (255, 255, 255))
    d = ImageDraw.Draw(img)
    f = mono(11)
    n = 0
    for gy in range(0, 2000, 28):
        for gx in range(0, 2000, 34):
            if rng.random() < 0.88:
                d.text((gx + 2, gy + 2), rng.choice(WORDS)[:4], font=f,
                       fill=(rng.randint(0, 90),) * 3)
                n += 1
    print("  blobs drawn:", n)
    return img.convert("L")  # gray-on-white: L-mode PNG is ~half the size


def screenshot_2560():
    """2560x1440 text screenshot: the exact production input size (provider
    max-long-edge is 2560, det then resizes to max-side 960)."""
    rng = random.Random(47)
    img = Image.new("RGB", (2560, 1440), (250, 250, 252))
    d = ImageDraw.Draw(img)
    for i in range(38):
        y = 20 + i * 36
        d.text((40, y), f"[{i:02d}] service-{rng.choice(WORDS).lower()} "
                        f"status=ok latency={rng.randint(1, 99)}ms "
                        f"req={rng.randint(1000, 9999)}", font=mono(26), fill=(30, 34, 40))
    return img


def main():
    os.makedirs(OUT, exist_ok=True)
    save(ui_dense(), "ui_dense_1920x1080.png")
    save(blank(), "blank_800x600.png")
    save(single_word(), "single_word.png")
    save(wide_strip(), "wide_strip_3000x80.png")
    save(tall_strip(), "tall_strip_80x3000.png")
    save(tiny(), "tiny_16x16.png")
    save(noisy_lowcontrast(), "noisy_lowcontrast.png")
    save(rotated(), "rotated_10_30deg.png")
    save(dense_blobs(), "dense_blobs.png")
    save(screenshot_2560(), "screenshot_2560x1440.png")


if __name__ == "__main__":
    main()
