#!/usr/bin/env python3
"""Generate the license-safe MaClaw desktop wallpapers (procedural, no
third-party images). Run from this directory: python3 gen_wallpapers.py
Needs Pillow + numpy. Output: wallpapers/*.jpg (1920x1200).
The MaClaw logo comes from this repository (hubcenter/web/skillmarket)."""
import math, random
from pathlib import Path
import numpy as np
from PIL import Image, ImageFilter, ImageDraw, ImageFont

W, H = 1920, 1200
OUT = Path(__file__).with_name("wallpapers"); OUT.mkdir(exist_ok=True)
LOGO = Path(__file__).resolve().parents[3] / "hubcenter/web/skillmarket/maclaw-logo.png"
yy, xx = np.mgrid[0:H, 0:W].astype(np.float32)

def blob(img, cx, cy, r, color, strength):
    d = np.exp(-(((xx - cx) ** 2 + (yy - cy) ** 2) / (2 * r * r)))[..., None] * strength
    return img * (1 - d) + np.array(color, np.float32) * d

def dither(a):
    a = a + np.random.default_rng(1).uniform(-1.2, 1.2, a.shape)
    return Image.fromarray(np.clip(a, 0, 255).astype(np.uint8))

def dark():
    t = (xx / W * 0.6 + yy / H * 0.4)[..., None]
    img = np.array([14, 20, 34], np.float32) * (1 - t) + np.array([6, 8, 14], np.float32) * t
    img = blob(img, W * 0.18, H * 0.22, 420, (20, 70, 120), 0.55)
    img = blob(img, W * 0.82, H * 0.78, 520, (90, 30, 30), 0.35)
    img = blob(img, W * 0.62, H * 0.30, 300, (30, 110, 140), 0.25)
    im = dither(img).convert("RGBA")
    # Faint flowing lines.
    lines = Image.new("RGBA", (W, H)); d = ImageDraw.Draw(lines)
    for k in range(18):
        pts = []
        for x in range(-40, W + 41, 20):
            y = H * 0.62 + k * 9 + 90 * math.sin(x / 260.0 + k * 0.18) + 40 * math.sin(x / 97.0 - k * 0.3)
            pts.append((x, y))
        d.line(pts, fill=(120, 190, 230, 18 + 2 * k), width=1)
    im = Image.alpha_composite(im, lines.filter(ImageFilter.GaussianBlur(0.6)))
    # Subtle logo, low opacity, right of center (stays clear of the dock).
    logo = Image.open(LOGO).convert("RGBA")
    logo = logo.resize((int(logo.width * 0.62), int(logo.height * 0.62)), Image.LANCZOS)
    a = np.array(logo).astype(np.float32); a[..., 3] *= 0.22
    logo = Image.fromarray(a.astype(np.uint8))
    im.alpha_composite(logo, (W - logo.width - 150, H - logo.height - 190))
    im.convert("RGB").save(OUT / "maclaw-dark.jpg", quality=92, optimize=True)

def colorful():
    # Weighted mix of soft color fields (no gray mud in the middle).
    acc = np.zeros((H, W, 3), np.float32); wsum = np.zeros((H, W, 1), np.float32)
    for cx, cy, r, c in [
        (0.05, 0.10, 520, (255, 150, 120)), (0.45, 0.00, 520, (255, 105, 170)),
        (0.95, 0.20, 560, (150, 110, 255)), (0.15, 0.95, 600, (70, 150, 255)),
        (0.80, 0.95, 560, (60, 200, 220)), (0.55, 0.50, 380, (255, 190, 130))]:
        w = np.exp(-(((xx - W * cx) ** 2 + (yy - H * cy) ** 2) / (2 * r * r)))[..., None] + 1e-4
        acc += w * np.array(c, np.float32); wsum += w
    img = acc / wsum
    img = img * 0.9 + 255 * 0.1
    im = dither(img).filter(ImageFilter.GaussianBlur(3))
    im.save(OUT / "maclaw-colorful.jpg", quality=92, optimize=True)
    # Softer pastel levels: the same color field mixed toward a light
    # neutral (not white) so the desktop stays calm at mid-high luminance.
    light = np.array([226, 229, 236], np.float32)
    for level in (25, 40, 55):
        k = level / 100.0
        soft = img * k + light * (1 - k)
        dither(soft).filter(ImageFilter.GaussianBlur(3)).save(OUT / ("maclaw-soft-%d.jpg" % level), quality=92, optimize=True)

def gray():
    t = np.sqrt(((xx - W / 2) / W) ** 2 + ((yy - H / 2) / H) ** 2)[..., None] / 0.71
    img = np.array([241, 243, 245], np.float32) * (1 - t) + np.array([191, 196, 203], np.float32) * t
    dither(img).save(OUT / "maclaw-gray.png", optimize=True)

dark(); colorful()
print("ok")
