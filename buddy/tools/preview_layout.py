#!/usr/bin/env python3
"""Preview buddy pages on both glasses.

Coordinates are checked here; glyph shapes use a desktop font, while the
device uses its packed 24px tables. The round glass must keep every checked
band inside r=150. The square glass uses buddy_layout.c's rectangle metrics
(side margins, index on the header row, straight date and weather) and is
then scaled 3:2. A circle crop is a failure on that profile.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

W = H = 360
SQUARE = 240
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

def load_cjk(size: int) -> ImageFont.FreeTypeFont:
    candidates = [
        Path(r"C:\Windows\Fonts\msyh.ttc"),
        Path(r"C:\Windows\Fonts\msyh.ttf"),
        Path("/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"),
        Path("/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc"),
    ]
    for path in candidates:
        if path.is_file():
            return ImageFont.truetype(str(path), size)
    raise SystemExit("no CJK font found for the buddy layout check")


FONT_CJK = load_cjk(24)
FONT_NUM = load_cjk(28)
FONT_HEAD = load_cjk(14)
FONT_TEMP = load_cjk(21)

OUT = Path(__file__).resolve().parents[2] / "build" / "verify"


@dataclass(frozen=True)
class Layout:
    """Pixel metrics for the 360 compose buffer. Mirrors buddy_layout.c."""

    header_y: int
    label_x: int
    value_right: int
    index_right: int
    index_y: int
    rule_x0: int
    rule_x1: int
    text_x: int
    title_w: int
    bar_x: int
    bar_w: int
    arc: bool
    date_y: int
    weather_y: int
    location_y: int


ROUND = Layout(36, 88, 262, 284, 186, 90, 270, 60, 240, 108, 144, True, 0, 0, 0)
RECT = Layout(16, 20, 340, 340, 16, 20, 340, 20, 320, 20, 320, False, 16, 292, 320)


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
    if max_w is not None and text_width(draw, text, font) > max_w:
        ell = "…"
        while text and text_width(draw, text + ell, font) > max_w:
            text = text[:-1]
        text += ell
    w = text_width(draw, text, font)
    draw.text((CX - w // 2, y), text, fill=color, font=font, anchor="lt")


Check = tuple[str, int, int, str, ImageFont.FreeTypeFont]


def text_box(draw, x: int, y: int, text: str, font) -> tuple[tuple[int, int], ...]:
    w = int(text_width(draw, text, font))
    h = int(getattr(font, "size", 24))
    return ((x, y), (x + w, y), (x, y + h), (x + w, y + h))


def assert_inside(draw, x: int, y: int, text: str, font, label: str) -> None:
    for px, py in text_box(draw, x, y, text, font):
        if (px - CX) ** 2 + (py - CY) ** 2 > APERTURE_R ** 2:
            raise SystemExit(f"{label} escapes the round aperture at ({px},{py})")


def assert_on_square(draw, x: int, y: int, text: str, font, label: str) -> None:
    for px, py in text_box(draw, x, y, text, font):
        sx = px * SQUARE // W
        sy = py * SQUARE // H
        if sx < 0 or sy < 0 or sx >= SQUARE or sy >= SQUARE:
            raise SystemExit(f"{label} escapes the 240 square at ({sx},{sy})")


def apply_aperture(img: Image.Image) -> None:
    """Black out everything outside the visible circle and mark its edge."""
    pixels = img.load()
    for y in range(H):
        for x in range(W):
            if (x - CX) ** 2 + (y - CY) ** 2 > APERTURE_R ** 2:
                pixels[x, y] = (0, 0, 0)


def boxes_overlap(a: tuple[int, int, int, int], b: tuple[int, int, int, int]) -> bool:
    ax0, ay0, ax1, ay1 = a
    bx0, by0, bx1, by1 = b
    return ax0 < bx1 and bx0 < ax1 and ay0 < by1 and by0 < ay1


def task_page(layout: Layout) -> tuple[Image.Image, list[Check]]:
    img, d = canvas()
    centered(d, layout.header_y, "MaClaw", MUTED, FONT_HEAD)
    rows = [
        ("总任务", 62, 74, "61", TEXT),
        ("进行中", 114, 126, "1", ACCENT),
        ("已完成", 166, 178, "58", SUCCESS),
    ]
    for label, vy, ly, value, color in rows:
        draw_text(d, layout.label_x, ly, label, color, FONT_CJK)
        w = text_width(d, value, FONT_NUM)
        draw_text(d, layout.value_right - w, vy, value, color, FONT_NUM)
    d.line((layout.rule_x0, 204, layout.rule_x1, 204), fill=PANEL, width=1)
    draw_text(d, layout.text_x, 206, "北京天气，生成pdf", TEXT, FONT_CJK, layout.title_w)
    draw_text(d, layout.text_x, 234, "并导出最终文件", TEXT, FONT_CJK, layout.title_w)
    tail = "待确认 2 · 异常 1"
    centered(d, 270, tail, WARN, FONT_CJK)
    index = "2/3"
    index_w = text_width(d, index, FONT_CJK)
    index_x = layout.index_right - index_w
    draw_text(d, index_x, layout.index_y, index, MUTED, FONT_CJK)
    d.rectangle((layout.bar_x, 298, layout.bar_x + layout.bar_w, 298 + 12), fill=PANEL)
    d.rectangle((layout.bar_x + 50, 298, layout.bar_x + 80, 298 + 12), fill=MUTED)
    if not layout.arc:
        label_dx = layout.label_x - CX
        label_dy = 74 - CY
        if label_dx * label_dx + label_dy * label_dy <= APERTURE_R * APERTURE_R:
            raise SystemExit("square task label fell back inside the round aperture")
        done_w = text_width(d, "58", FONT_NUM)
        index_box = (index_x, layout.index_y, index_x + index_w, layout.index_y + FONT_CJK.size)
        done_box = (layout.value_right - done_w, 166, layout.value_right, 166 + FONT_NUM.size)
        if boxes_overlap(index_box, done_box):
            raise SystemExit("square task index covers the done counter")
    checks = [
        ("task tail", CX - text_width(d, tail, FONT_CJK) // 2, 270, tail, FONT_CJK),
        ("task index", index_x, layout.index_y, index, FONT_CJK),
    ]
    return img, checks


def standby_page(layout: Layout) -> tuple[Image.Image, list[Check]]:
    img, d = canvas()
    date = "10月31日 周二"
    offline = "未连接"
    location = "鄂尔多斯市"
    pet_y = 170 if not layout.arc else 146
    d.ellipse((180 - 40 - 17, pet_y - 44 - 17, 180 - 40 + 17, pet_y - 44 + 17), fill=(59, 130, 246))
    d.ellipse((180 + 40 - 17, pet_y - 44 - 17, 180 + 40 + 17, pet_y - 44 + 17), fill=(59, 130, 246))
    d.ellipse((180 - 58, pet_y - 58, 180 + 58, pet_y + 58), fill=(96, 165, 250))
    d.ellipse((180 - 28, pet_y - 15, 180 - 14, pet_y - 1), fill=BG)
    d.ellipse((180 + 14, pet_y - 15, 180 + 28, pet_y - 1), fill=BG)
    if layout.arc:
        centered(d, 72, date, TEXT, FONT_CJK)
        centered(d, 208, "晴", TEXT, FONT_CJK)
        temp = "8"
        tw = text_width(d, temp, FONT_TEMP)
        tx = CX - (tw + 22) // 2
        draw_text(d, tx, 236, temp, ACCENT, FONT_TEMP)
        d.ellipse((tx + tw + 3, 242, tx + tw + 11, 242 + 8), outline=ACCENT, width=2)
        draw_text(d, tx + tw + 10, 238, "C", ACCENT, FONT_HEAD)
        centered(d, 262, location, MUTED, FONT_CJK, 190)
        centered(d, 292, offline, MUTED, FONT_CJK)
        checks = [
            ("standby date", CX - text_width(d, date, FONT_CJK) // 2, 72, date, FONT_CJK),
            ("standby offline", CX - text_width(d, offline, FONT_CJK) // 2, 292, offline, FONT_CJK),
        ]
        return img, checks

    centered(d, layout.date_y, date, TEXT, FONT_CJK)
    centered(d, 61, offline, MUTED, FONT_CJK)
    weather = "晴 8°C"
    centered(d, layout.weather_y, weather, ACCENT, FONT_CJK)
    centered(d, layout.location_y, location, MUTED, FONT_CJK, layout.title_w)
    loc_x = CX - text_width(d, location, FONT_CJK) // 2
    outside = False
    for px, py in text_box(d, loc_x, layout.location_y, location, FONT_CJK):
        if (px - CX) ** 2 + (py - CY) ** 2 > APERTURE_R ** 2:
            outside = True
            break
    if not outside:
        raise SystemExit("square location stayed inside the round aperture")
    checks = [
        ("standby date", CX - text_width(d, date, FONT_CJK) // 2, layout.date_y, date, FONT_CJK),
        ("standby weather", CX - text_width(d, weather, FONT_CJK) // 2, layout.weather_y, weather, FONT_CJK),
        ("standby location", loc_x, layout.location_y, location, FONT_CJK),
    ]
    return img, checks


def celebrate_page() -> tuple[Image.Image, list[Check]]:
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
    title = "✓ 任务完成"
    name = "北京天气，生成pdf"
    centered(d, 92, title, ACCENT, FONT_CJK)
    centered(d, 128, name, TEXT, FONT_CJK, 240)
    checks = [
        ("celebrate title", CX - text_width(d, title, FONT_CJK) // 2, 92, title, FONT_CJK),
        ("celebrate name", CX - text_width(d, name, FONT_CJK) // 2, 128, name, FONT_CJK),
    ]
    return img, checks


def prompt_page() -> tuple[Image.Image, list[Check]]:
    img, d = canvas()
    heading = "MaClaw Buddy"
    line1 = "任务计数不一致"
    line2 = "已暂停显示"
    centered(d, 140, heading, ACCENT, FONT_HEAD)
    centered(d, 188, line1, TEXT, FONT_CJK)
    centered(d, 220, line2, TEXT, FONT_CJK)
    checks = [
        ("prompt heading", CX - text_width(d, heading, FONT_HEAD) // 2, 140, heading, FONT_HEAD),
        ("prompt line", CX - text_width(d, line1, FONT_CJK) // 2, 188, line1, FONT_CJK),
        ("prompt line 2", CX - text_width(d, line2, FONT_CJK) // 2, 220, line2, FONT_CJK),
    ]
    return img, checks


def write_pair(
    name: str,
    round_img: Image.Image,
    round_checks: list[Check],
    square_img: Image.Image,
    square_checks: list[Check],
) -> None:
    round_draw = ImageDraw.Draw(round_img)
    for label, x, y, text, font in round_checks:
        assert_inside(round_draw, x, y, text, font, label)
    apply_aperture(round_img)
    if round_img.getpixel((2, 2)) != (0, 0, 0):
        raise SystemExit(f"{name} round glass left a corner pixel visible")
    square_draw = ImageDraw.Draw(square_img)
    for label, x, y, text, font in square_checks:
        assert_on_square(square_draw, x, y, text, font, label)
    square = square_img.resize((SQUARE, SQUARE), Image.Resampling.NEAREST)
    if square.getpixel((2, 2)) == (0, 0, 0):
        raise SystemExit(f"{name} square glass was cropped to the round aperture")
    round_path = OUT / f"preview_{name}.png"
    square_path = OUT / f"preview_{name}_240.png"
    round_img.save(round_path)
    square.save(square_path)
    print(f"wrote {round_path}")
    print(f"wrote {square_path}")


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    write_pair("task", *task_page(ROUND), *task_page(RECT))
    write_pair("standby", *standby_page(ROUND), *standby_page(RECT))
    celebrate = celebrate_page()
    prompt = prompt_page()
    write_pair("celebrate", *celebrate, celebrate[0].copy(), celebrate[1])
    write_pair("prompt", *prompt, prompt[0].copy(), prompt[1])


if __name__ == "__main__":
    main()
