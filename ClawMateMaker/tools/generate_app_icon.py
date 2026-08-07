"""Generate the transparent Windows icon from the documented vector design."""

from pathlib import Path
from typing import Optional, Tuple

from PIL import Image, ImageDraw


ROOT = Path(__file__).resolve().parents[1]
SIZE = 1024
NAVY = "#123d63"
GREEN = "#2d9f72"
WHITE = "#f8fbff"


def rounded(draw: ImageDraw.ImageDraw, box: Tuple[int, int, int, int], radius: int, *, fill: Optional[str], outline: Optional[str] = None, width: int = 1) -> None:
    draw.rounded_rectangle(box, radius=radius, fill=fill, outline=outline, width=width)


def draw_mark(size: int) -> Image.Image:
    """Draw the transparent chip-and-arrow mark at a pixel-safe scale.

    Windows commonly selects the 16px or 20px member for title bars. The old
    asset was a large illustration simply resampled, making its outline too
    thin at those sizes. This intentionally bolder rendering keeps the chip
    silhouette and programming-arrow meaning clear in a tiny title bar.
    """
    supersample = 4
    canvas = size * supersample
    scale = canvas / 1024
    icon = Image.new("RGBA", (canvas, canvas), (0, 0, 0, 0))
    draw = ImageDraw.Draw(icon)

    def px(value: int) -> int:
        return round(value * scale)

    outline = max(px(48), supersample * 2)
    # Freestanding chip silhouette: no dark square or tile background.
    rounded(draw, (px(218), px(246), px(806), px(790)), px(98), fill=WHITE, outline=NAVY, width=outline)
    for top in (344, 488, 632):
        rounded(draw, (px(144), px(top), px(238), px(top + 64)), px(18), fill=NAVY)
        rounded(draw, (px(786), px(top), px(880), px(top + 64)), px(18), fill=NAVY)

    rounded(draw, (px(462), px(118), px(562), px(424)), px(50), fill=GREEN)
    draw.polygon([(px(352), px(382)), (px(672), px(382)), (px(538), px(578)), (px(512), px(604)), (px(486), px(578))], fill=GREEN)
    rounded(draw, (px(326), px(570), px(698), px(720)), px(54), fill=WHITE, outline=NAVY, width=outline)
    draw.line((px(404), px(645), px(620), px(645)), fill=GREEN, width=max(px(36), supersample * 2))

    return icon.resize((size, size), getattr(Image, "Resampling", Image).LANCZOS)


def main() -> None:
    icon = draw_mark(SIZE)
    icon.save(ROOT / "build" / "appicon.png")

    # Wails embeds this ICO in the final EXE. Explicit title-bar dimensions
    # avoid Windows having to rescale a single large bitmap at runtime.
    icon.save(
        ROOT / "build" / "windows" / "icon.ico",
        format="ICO",
        sizes=[(16, 16), (20, 20), (24, 24), (32, 32), (40, 40), (48, 48), (64, 64), (128, 128), (256, 256)],
        bitmap_format="bmp",
    )


if __name__ == "__main__":
    main()
