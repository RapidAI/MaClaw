#!/usr/bin/env python3
"""Preview the buddy task/standby layout against the round aperture.

This is a host-side sanity check for coordinates only: glyph shapes are drawn
with a desktop font, while the device uses its packed 24px tables. What it
verifies is that every text band stays inside the visible circle (r=150 around
the centre) before flashing.
"""

from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

W = H = 360
CX = CY = 180
APERTURE_R = 150
BG = (14, 17, 22)
TEXT = (248, 250, 252)
MUTED = (139, 154, 180)
ACCENT = (56, 189, 248)
SUCCESS = (52, 211, 153)
WARN = (245, 158, 11)
DANGER = (248, 113, 113)
PANEL = (24, 30, 40)

FONT_CJK = ImageFont.truetype(r"C:\Windows\Fonts\msyh.ttc", 24)
FONT_NUM = ImageFont.truetype(r"C:\Windows\Fonts\msyh.ttc", 28)
FONT_HEAD = ImageFont.truetype(r"C:\Windows\Fonts\msyh.ttc", 14)
FONT_TEMP = ImageFont.truetype(r"C:\Windows\Fonts\msyh.ttc", 21)

OUT = Path(__file__).resolve().parents[2] / "build" / "verify"


def canvas() -> tuple[Image.Image, ImageDraw.ImageDraw]:
    img = Image.new("RGB", (W, H), BG)
    return img, ImageDraw.Draw(img)


def text_width(draw: ImageDraw.ImageDraw, text: str, font: ImageFont.FreeTypeFont) -> int:
    return int(draw.textlength(text, font=font))


def draw_text(draw, x, y, text, color, font, max_w=None):
    if max_w is None:
        draw.text((x, y), text, fill=color, font=font, anchor="lt")
        return
    # Trim with an ellipsis on rune boundaries.
    ell = "…"
    if text_width(draw, text, font) <= max_w:
        draw.text((x, y), text, fill=color, font=font, anchor="lt")
        return
    while text and text_width(draw, text + ell, font) > max_w:
        text = text[:-1]
    draw.text((x, y), text + ell, fill=color, font=font, anchor="lt")


def centered(draw, y, text, color, font, max_w=None):
    if max_w is not None:
        ell = "…"
        while text and text_width(draw, text, font) > max_w:
            text = text[:-1]
        if text_width(draw, text, font) > max_w:
            text = text[:-1]
        if text != ell and text_width(draw, text + ell, font) <= max_w:
            text += ell
    w = text_width(draw, text, font)
    draw.text((CX - w // 2, y), text, fill=color, font=font, anchor="lt")


def apply_aperture(img: Image.Image) -> None:
    """Black out everything outside the visible circle and mark its edge."""
    pixels = img.load()
    for y in range(H):
        for x in range(W):
            if (x - CX) ** 2 + (y - CY) ** 2 > APERTURE_R ** 2:
                pixels[x, y] = (0, 0, 0)


def task_page() -> Image.Image:
    img, d = canvas()
    centered(d, 36, "MaClaw", MUTED, FONT_HEAD)
    rows = [
        ("总任务", 62, 74, "61", TEXT),
        ("进行中", 114, 126, "1", ACCENT),
        ("已完成", 166, 178, "58", SUCCESS),
    ]
    for label, vy, ly, value, color in rows:
        draw_text(d, 88, ly, label, color, FONT_CJK)
        w = text_width(d, value, FONT_NUM)
        draw_text(d, 262 - w, vy, value, color, FONT_NUM)
    d.line((90, 204, 270, 204), fill=PANEL, width=1)
    draw_text(d, 60, 206, "北京天气，生成pdf", TEXT, FONT_CJK, 240)
    draw_text(d, 60, 234, "并导出最终文件", TEXT, FONT_CJK, 240)
    centered(d, 270, "待处理 2 · 异常 1", WARN, FONT_CJK)
    d.rectangle((108, 298, 108 + 144, 298 + 12), fill=PANEL)
    d.rectangle((108 + 50, 298, 108 + 80, 298 + 12), fill=MUTED)
    apply_aperture(img)
    return img


def standby_page() -> Image.Image:
    img, d = canvas()
    centered(d, 48, "10月31日 周二", TEXT, FONT_CJK)
    # Pet.
    d.ellipse((180 - 40 - 17, 146 - 44 - 17, 180 - 40 + 17, 146 - 44 + 17), fill=(59, 130, 246))
    d.ellipse((180 + 40 - 17, 146 - 44 - 17, 180 + 40 + 17, 146 - 44 + 17), fill=(59, 130, 246))
    d.ellipse((180 - 58, 146 - 58, 180 + 58, 146 + 58), fill=(96, 165, 250))
    d.ellipse((180 - 28, 146 - 15, 180 - 14, 146 - 1), fill=BG)
    d.ellipse((180 + 14, 146 - 15, 180 + 28, 146 - 1), fill=BG)
    centered(d, 208, "晴", TEXT, FONT_CJK)
    temp = "8"
    tw = text_width(d, temp, FONT_TEMP)
    tx = CX - (tw + 22) // 2
    draw_text(d, tx, 236, temp, ACCENT, FONT_TEMP)
    d.ellipse((tx + tw + 3, 242, tx + tw + 11, 242 + 8), outline=ACCENT, width=2)
    draw_text(d, tx + tw + 10, 238, "C", ACCENT, FONT_HEAD)
    centered(d, 262, "鄂尔多斯市", MUTED, FONT_CJK, 190)
    centered(d, 292, "未连接", MUTED, FONT_CJK)
    apply_aperture(img)
    return img



def celebrate_page() -> Image.Image:
    img, d = canvas()
    import math
    bursts = [(110, 150, ACCENT), (250, 130, SUCCESS), (180, 100, WARN)]
    for x, y, color in bursts:
        for i in range(14):
            ang = i * 2 * math.pi / 14
            speed = 40 + (i % 4) * 14
            t = 0.9
            px = x + math.cos(ang) * speed * t
            py = y + math.sin(ang) * speed * t + 0.5 * 90 * t * t
            d.ellipse((px - 2, py - 2, px + 2, py + 2), fill=color)
        d.ellipse((x - 7, y - 7, x + 7, y + 7), outline=TEXT, width=2)
    for x, y in ((130, 230), (240, 210)):
        d.rectangle((x - 1, y, x + 1, y + 10), fill=ACCENT)
        d.rectangle((x, y + 10, x, y + 18), fill=MUTED)
    centered(d, 92, "✓ 任务完成", ACCENT, FONT_CJK)
    centered(d, 128, "北京天气，生成pdf", TEXT, FONT_CJK, 240)
    apply_aperture(img)
    return img

def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    task_page().save(OUT / "preview_task.png")
    standby_page().save(OUT / "preview_standby.png")
    celebrate_page().save(OUT / "preview_celebrate.png")
    print(f"wrote {OUT / 'preview_task.png'}")
    print(f"wrote {OUT / 'preview_standby.png'}")
    print(f"wrote {OUT / 'preview_celebrate.png'}")


if __name__ == "__main__":
    main()
