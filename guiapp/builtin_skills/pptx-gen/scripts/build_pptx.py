"""Build a designed .pptx deck from a JSON outline using python-pptx.

The deck uses a real visual system (cover, accent rail, cards, page footer),
matching the native office write_pptx renderer. Input JSON:

    {"title": "...", "subtitle": "...", "theme": "business|academic|warm",
     "slides": [{"title": "...", "kicker": "...",
                 "layout": "auto|bullets|cards|section|agenda|kpi|quote|closing",
                 "bullets": ["..."], "notes": "...",
                 "images": [{"path": "..."}],
                 "charts": [{"chart_type": "line", "title": "...",
                             "categories": ["..."],
                             "series": [{"name": "...", "values": [1]}]}]}]}

Usage:
    python build_pptx.py --input outline.json --output deck.pptx
    python build_pptx.py --input outline.json --output ./out_dir
    echo '{...}' | python build_pptx.py --output deck.pptx
"""
import argparse
import json
import os
import re
import sys

_ILLEGAL_XML_CHARS_RE = re.compile(
    r'[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x84\x86-\x9f'
    r'\ud800-\udfff\ufdd0-\ufdef\ufffe\uffff]'
)

_DEFAULT_FONT = "Microsoft YaHei"

# Keep ids, aliases, and keywords aligned with corelib/pptx/write_style.go.
_STYLES = [
    {"id": "business", "aliases": ["business", "商务", "商务汇报", "汇报"], "cover_dark": True,
     "keywords": ["季度", "经营分析", "客户提案", "商务", "董事会", "复盘", "工作汇报", "季度汇报", "quarterly review", "board meeting", "client proposal", "business review"],
     "colors": {"paper": "F3F6F8", "ink": "1C2B3A", "navy": "0B1F33", "navy2": "14324C", "accent": "0E7C73", "gold": "C4A35A", "white": "FFFFFF", "mute": "5E6B78", "slate": "3E4C5A", "card": "FFFFFF", "on_dark": "F7FBFC", "on_dark_mute": "C3D2DE"}},
    {"id": "academic", "aliases": ["academic", "学术", "学术答辩", "scholar"], "cover_dark": True,
     "keywords": ["论文", "答辩", "课题", "开题", "文献", "学术", "综述", "thesis", "dissertation", "research paper", "literature review"],
     "colors": {"paper": "F7F4EE", "ink": "241C18", "navy": "1C2430", "navy2": "2A3544", "accent": "7C2F3B", "gold": "A6843D", "white": "FFFFFF", "mute": "6E655C", "slate": "3E4652", "card": "FFFCF8", "on_dark": "F8F4EE", "on_dark_mute": "D9D0C4"}},
    {"id": "warm", "aliases": ["warm", "温馨", "温馨纪念", "warmth", "soft"], "cover_dark": True,
     "keywords": ["生日", "纪念", "婚礼", "满月", "寿辰", "温馨", "宠物", "致谢", "birthday", "wedding", "memorial", "family gathering"],
     "colors": {"paper": "FBF6F1", "ink": "3A2A24", "navy": "3D2B26", "navy2": "5A3E36", "accent": "C4785A", "gold": "E0B15A", "white": "FFFFFF", "mute": "7A6A60", "slate": "5C463E", "card": "FFFCF8", "on_dark": "FFF8F3", "on_dark_mute": "E6D2C6"}},
    {"id": "launch", "aliases": ["launch", "发布", "发布路演", "路演"], "cover_dark": True,
     "keywords": ["发布会", "路演", "融资", "产品发布", "品牌发布", "product launch", "pitch deck", "fundraising"],
     "colors": {"paper": "F6F5F3", "ink": "1A1A1A", "navy": "141414", "navy2": "2A2A2A", "accent": "E24A2A", "gold": "F0C14A", "white": "FFFFFF", "mute": "6A6A6A", "slate": "3A3A3A", "card": "FFFFFF", "on_dark": "F7F4F1", "on_dark_mute": "E7C7BE"}},
    {"id": "tech", "aliases": ["tech", "技术", "技术分享"], "cover_dark": True,
     "keywords": ["技术分享", "架构", "研发", "开源", "系统设计", "技术方案", "open source", "engineering talk", "system architecture"],
     "colors": {"paper": "F3F6FA", "ink": "102033", "navy": "071422", "navy2": "10243A", "accent": "19B5C7", "gold": "8EB4FF", "white": "FFFFFF", "mute": "5C6B7A", "slate": "314255", "card": "FFFFFF", "on_dark": "E7F6F8", "on_dark_mute": "B7D4DC"}},
    {"id": "education", "aliases": ["education", "教学", "课件", "教学课件"], "cover_dark": True,
     "keywords": ["课件", "培训", "课堂", "教学", "课程", "教案", "lecture", "training course", "classroom", "lesson plan"],
     "colors": {"paper": "FFF8EF", "ink": "2A241C", "navy": "1E3A5F", "navy2": "2C4C73", "accent": "E39B2B", "gold": "F3D48A", "white": "FFFFFF", "mute": "6E6256", "slate": "4A4036", "card": "FFFCF7", "on_dark": "FFF6E8", "on_dark_mute": "F0D7B0"}},
    {"id": "ceremony", "aliases": ["ceremony", "典礼", "年会", "典礼年会"], "cover_dark": True,
     "keywords": ["年会", "典礼", "颁奖", "致辞", "开幕", "闭幕", "纪念典礼", "awards ceremony", "annual gala", "opening remarks"],
     "colors": {"paper": "F8F3EE", "ink": "2C1A1E", "navy": "3A1520", "navy2": "4E2030", "accent": "D4AF67", "gold": "F0D78C", "white": "FFFFFF", "mute": "7A655C", "slate": "4E3A3A", "card": "FFFCF8", "on_dark": "FBF4EA", "on_dark_mute": "E6D3C4"}},
    {"id": "minimal", "aliases": ["minimal", "极简"], "cover_dark": False,
     "keywords": ["极简", "作品集", "简历", "个人介绍", "portfolio", "resume", "personal introduction"],
     "colors": {"paper": "F7F6F3", "ink": "1C1C1C", "navy": "1C1C1C", "navy2": "3A3A3A", "accent": "1C1C1C", "gold": "B08948", "white": "FFFFFF", "mute": "6E6A64", "slate": "3F3F3C", "card": "FFFFFF", "on_dark": "1C1C1C", "on_dark_mute": "5C5852"}},
]


def _clean_xml_text(text):
    if not text:
        return ""
    return _ILLEGAL_XML_CHARS_RE.sub("", str(text)).strip()


def _rgb(hex_code):
    from pptx.dml.color import RGBColor
    hex_code = hex_code.strip().lstrip("#")
    return RGBColor(int(hex_code[0:2], 16), int(hex_code[2:4], 16), int(hex_code[4:6], 16))


def _set_run_font(run, size=None, bold=None, color=None):
    from pptx.oxml.ns import qn
    run.font.name = _DEFAULT_FONT
    if size is not None:
        run.font.size = size
    if bold is not None:
        run.font.bold = bold
    if color is not None:
        run.font.color.rgb = color
    r_pr = run._r.get_or_add_rPr()
    for tag in ("a:ea", "a:latin"):
        node = r_pr.find(qn(tag))
        if node is None:
            node = r_pr.makeelement(qn(tag), {})
            r_pr.append(node)
        node.set("typeface", _DEFAULT_FONT)


def _load_outline(args):
    if args.input:
        if not os.path.isfile(args.input):
            print(f"ERROR: Input JSON file not found: {args.input}", file=sys.stderr)
            sys.exit(1)
        with open(args.input, "r", encoding="utf-8") as handle:
            raw = handle.read()
    else:
        raw = sys.stdin.read()
    try:
        data = json.loads(raw)
    except json.JSONDecodeError as exc:
        print(f"ERROR: Invalid outline JSON: {exc}", file=sys.stderr)
        sys.exit(1)
    if not isinstance(data, dict):
        print("ERROR: Outline JSON must be an object with 'title' and 'slides'.", file=sys.stderr)
        sys.exit(1)
    slides = data.get("slides")
    if not isinstance(slides, list) or not slides:
        print("ERROR: Outline JSON must contain a non-empty 'slides' array.", file=sys.stderr)
        sys.exit(1)
    return data


def _resolve_output_path(output, title):
    if os.path.isdir(output):
        safe = re.sub(r'[\\/:*?"<>|]+', "_", (title or "presentation").strip()) or "presentation"
        output = os.path.join(output, f"{safe}.pptx")
    elif not output.lower().endswith(".pptx"):
        output += ".pptx"
    parent = os.path.dirname(os.path.abspath(output))
    if parent:
        os.makedirs(parent, exist_ok=True)
    return output


def _lookup_style(name):
    key = str(name or "").strip().lower()
    if key in ("", "auto", "自动", "default", "默认"):
        return None
    for style in _STYLES:
        names = [style["id"], *style["aliases"]]
        if key in [item.lower() for item in names]:
            return style
    return None


def _match_style(hint):
    hint = str(hint or "").lower()
    best = None
    best_len = 0
    best_score = 0
    for style in _STYLES:
        score = 0
        longest = 0
        for keyword in style["keywords"]:
            if keyword.lower() in hint:
                n = len(keyword)
                score += n
                longest = max(longest, n)
        if score and (longest > best_len or (longest == best_len and score > best_score)):
            best = style
            best_len = longest
            best_score = score
    return best


def _style_hint(outline):
    parts = [outline.get("purpose"), outline.get("title"), outline.get("subtitle")]
    for item in outline.get("slides") or []:
        if isinstance(item, dict):
            parts.extend([item.get("title"), item.get("kicker")])
    return " ".join(_clean_xml_text(part) for part in parts if part)


def _resolve_theme(name, hint):
    found = _lookup_style(name)
    if found:
        return found
    text = hint
    if str(name or "").strip().lower() not in ("", "auto", "自动", "default", "默认"):
        text = f"{name} {hint}"
    return _match_style(text) or _STYLES[0]


def _texts(bullets):
    if isinstance(bullets, str):
        bullets = [bullets]
    out = []
    for item in bullets or []:
        text = _clean_xml_text(item)
        if text:
            out.append(text)
    return out


def _layout(item):
    raw = str(item.get("layout") or "auto").strip().lower()
    aliases = {
        "": "auto", "自动": "auto",
        "bullet": "bullets", "list": "bullets", "要点": "bullets", "列表": "bullets",
        "card": "cards", "卡片": "cards",
        "divider": "section", "分节": "section", "章节": "section",
        "toc": "agenda", "目录": "agenda", "议程": "agenda",
        "metrics": "kpi", "指标": "kpi",
        "引述": "quote", "金句": "quote",
        "close": "closing", "end": "closing", "结尾": "closing", "结束": "closing",
    }
    kind = aliases.get(raw, raw if raw in ("auto", "bullets", "cards", "section", "agenda", "kpi", "quote", "closing") else "auto")
    texts = _texts(item.get("bullets"))
    has_media = bool(item.get("images") or item.get("charts"))
    if has_media:
        return "bullets"
    if kind == "cards" and (not texts or len(texts) > 6):
        return "bullets"
    if kind == "agenda" and (not texts or len(texts) > 8):
        return "bullets"
    if kind == "kpi" and (not texts or len(texts) > 6):
        return "bullets"
    if kind != "auto":
        return kind
    if not texts:
        return "section"
    if 2 <= len(texts) <= 4 and all(len(text) <= 36 for text in texts):
        return "cards"
    return "bullets"


def _blank_layout(prs):
    for layout in prs.slide_layouts:
        if "blank" in (layout.name or "").lower():
            return layout
    return prs.slide_layouts[min(6, len(prs.slide_layouts) - 1)]


def _no_line(shape):
    shape.line.fill.background()


def _rect(slide, inches, x, y, w, h, fill, rounded=False):
    from pptx.enum.shapes import MSO_SHAPE
    kind = MSO_SHAPE.ROUNDED_RECTANGLE if rounded else MSO_SHAPE.RECTANGLE
    shape = slide.shapes.add_shape(kind, inches(x), inches(y), inches(w), inches(h))
    shape.fill.solid()
    shape.fill.fore_color.rgb = fill
    _no_line(shape)
    if rounded:
        try:
            shape.adjustments[0] = 0.08
        except Exception:
            pass
    return shape


def _tb(slide, inches, pt, x, y, w, h, text, size, color, bold=False, align="left"):
    from pptx.enum.text import PP_ALIGN
    text = _clean_xml_text(text)
    if not text:
        return None
    box = slide.shapes.add_textbox(inches(x), inches(y), inches(w), inches(h))
    frame = box.text_frame
    frame.word_wrap = True
    paragraph = frame.paragraphs[0]
    paragraph.alignment = {"center": PP_ALIGN.CENTER, "right": PP_ALIGN.RIGHT}.get(align, PP_ALIGN.LEFT)
    run = paragraph.add_run()
    run.text = text
    _set_run_font(run, size=pt(size), bold=bold, color=color)
    return box


def _lines(slide, inches, pt, x, y, w, h, lines, size, color, space=8):
    from pptx.enum.text import PP_ALIGN
    from pptx.util import Pt
    lines = _texts(lines)
    if not lines:
        return
    box = slide.shapes.add_textbox(inches(x), inches(y), inches(w), inches(h))
    frame = box.text_frame
    frame.word_wrap = True
    for index, line in enumerate(lines):
        paragraph = frame.paragraphs[0] if index == 0 else frame.add_paragraph()
        paragraph.alignment = PP_ALIGN.LEFT
        paragraph.space_after = Pt(space)
        run = paragraph.add_run()
        run.text = line
        _set_run_font(run, size=pt(size), color=color)


def _page_mark(slide, inches, pt, page, total, color):
    _tb(slide, inches, pt, 11.05, 7.08, 1.8, 0.28, f"{page:02d}  /  {total:02d}", 12, color, align="right")


def _dark_canvas(slide, inches, theme):
    _rect(slide, inches, 0, 0, 13.333, 7.5, theme["navy"])
    _rect(slide, inches, 0, 6.52, 13.333, 0.98, theme["navy2"])
    _rect(slide, inches, 0, 0, 0.16, 7.5, theme["accent"])


def _feature_canvas(slide, inches, theme, cover_dark):
    if cover_dark:
        _dark_canvas(slide, inches, theme)
        return
    _light_canvas(slide, inches, theme)
    _rect(slide, inches, 0, 7.42, 13.333, 0.08, theme["accent"])


def _feature_colors(theme, cover_dark):
    if cover_dark:
        return theme["on_dark"], theme["on_dark_mute"], theme["gold"], theme["accent"]
    return theme["navy"], theme["slate"], theme["gold"], theme["ink"]


def _light_canvas(slide, inches, theme):
    _rect(slide, inches, 0, 0, 13.333, 7.5, theme["paper"])
    _rect(slide, inches, 0, 0, 0.12, 7.5, theme["accent"])
    _rect(slide, inches, 0, 0, 13.333, 0.045, theme["navy"])


def _light_chrome(slide, inches, pt, theme, kicker, title, footer, page, total):
    _light_canvas(slide, inches, theme)
    title_y = 0.26
    title_size = 28
    if kicker:
        _tb(slide, inches, pt, 0.48, 0.14, 12.2, 0.26, kicker, 12, theme["accent"], bold=True)
        title_y = 0.40
        title_size = 26
    if title:
        _tb(slide, inches, pt, 0.48, title_y, 12.2, 0.52, title, title_size, theme["navy"], bold=True)
        _rect(slide, inches, 0.48, title_y + 0.56, 1.45, 0.042, theme["accent"])
    if footer:
        _tb(slide, inches, pt, 0.48, 7.08, 9.3, 0.28, footer, 11, theme["mute"])
    _page_mark(slide, inches, pt, page, total, theme["mute"])
    return 1.36 if kicker else 1.16


def _split_card(text):
    for sep in ("：", ": ", " — ", " – ", " - ", " | "):
        if sep in text:
            head, body = text.split(sep, 1)
            head, body = head.strip(), body.strip()
            if head and body and len(head) <= 16:
                return head, body
    return "", text


def _split_kpi(text):
    for sep in (" | ", "｜", " — ", " – "):
        if sep in text:
            value, label = text.split(sep, 1)
            value, label = value.strip(), label.strip()
            if value and label:
                return value, label
    return text, ""


def _cards(slide, inches, pt, theme, bullets, top):
    texts = _texts(bullets)
    n = len(texts)
    cols, rows = (2, 2) if n == 4 else ((n, 1) if n <= 3 else (3, 2))
    gap = 0.16
    usable_w, usable_h = 12.43, 6.90 - top
    card_w = (usable_w - gap * (cols - 1)) / cols
    card_h = (usable_h - gap * (rows - 1)) / rows
    card_h = min(card_h, 2.70 if rows == 1 else 2.40)
    for index, text in enumerate(texts):
        col, row = index % cols, index // cols
        x = 0.48 + col * (card_w + gap)
        y = top + row * (card_h + gap)
        _rect(slide, inches, x, y, card_w, card_h, theme["card"], rounded=True)
        _rect(slide, inches, x, y, card_w, 0.07, theme["accent"])
        _tb(slide, inches, pt, x + 0.18, y + 0.18, card_w - 0.36, 0.32, f"{index + 1:02d}", 12, theme["accent"], bold=True)
        head, body = _split_card(text)
        if head:
            _tb(slide, inches, pt, x + 0.18, y + 0.52, card_w - 0.36, 0.6, head, 16, theme["navy"], bold=True)
            _tb(slide, inches, pt, x + 0.18, y + 1.14, card_w - 0.36, card_h - 1.35, body, 14, theme["slate"])
        else:
            _tb(slide, inches, pt, x + 0.18, y + 0.52, card_w - 0.36, card_h - 0.75, text, 15, theme["ink"])


def _agenda(slide, inches, pt, theme, bullets, top):
    texts = _texts(bullets)
    n = len(texts)
    gap = 0.12
    row_h = min((6.90 - top - gap * (n - 1)) / n, 0.92)
    for index, text in enumerate(texts):
        y = top + index * (row_h + gap)
        _rect(slide, inches, 0.48, y, 12.43, row_h, theme["card"], rounded=True)
        _rect(slide, inches, 0.64, y + (row_h - 0.42) / 2, 0.72, 0.42, theme["accent"], rounded=True)
        _tb(slide, inches, pt, 0.64, y + (row_h - 0.42) / 2, 0.72, 0.42, f"{index + 1:02d}", 14, theme["white"], bold=True, align="center")
        _tb(slide, inches, pt, 1.56, y + (row_h - 0.42) / 2, 11.0, 0.42, text, 16, theme["ink"])


def _kpi(slide, inches, pt, theme, bullets, top):
    texts = _texts(bullets)
    n = len(texts)
    cols = n if n <= 4 else 3
    rows = 1 if n <= 4 else 2
    gap = 0.16
    card_w = (12.43 - gap * (cols - 1)) / cols
    card_h = min(2.15, (6.90 - top - gap * (rows - 1)) / rows)
    for index, text in enumerate(texts):
        col, row = index % cols, index // cols
        x = 0.48 + col * (card_w + gap)
        y = top + row * (card_h + gap)
        _rect(slide, inches, x, y, card_w, card_h, theme["card"], rounded=True)
        _rect(slide, inches, x, y, card_w, 0.07, theme["accent"])
        value, label = _split_kpi(text)
        _tb(slide, inches, pt, x + 0.18, y + 0.28, card_w - 0.36, 0.7, value, 22 if cols >= 4 else 26, theme["navy"], bold=True)
        if label:
            _tb(slide, inches, pt, x + 0.18, y + 1.05, card_w - 0.36, 0.7, label, 13, theme["slate"])


def _bullets(slide, inches, pt, theme, bullets, top, bottom=6.90):
    from pptx.enum.text import PP_ALIGN
    from pptx.util import Pt
    texts = _texts(bullets)
    if not texts or bottom - top < 0.4:
        return
    box = slide.shapes.add_textbox(inches(0.48), inches(top), inches(12.43), inches(bottom - top))
    frame = box.text_frame
    frame.word_wrap = True
    for index, text in enumerate(texts):
        paragraph = frame.paragraphs[0] if index == 0 else frame.add_paragraph()
        paragraph.alignment = PP_ALIGN.LEFT
        paragraph.space_after = Pt(10)
        run = paragraph.add_run()
        run.text = "•  " + text
        _set_run_font(run, size=pt(18), color=theme["ink"])


def _images(slide, inches, item, top):
    images = item.get("images") or []
    if isinstance(images, dict):
        images = [images]
    paths = []
    for image in images:
        if isinstance(image, str):
            path = image
        elif isinstance(image, dict):
            path = image.get("path") or ""
        else:
            continue
        path = str(path).strip()
        if not path:
            continue
        if not os.path.isfile(path):
            print(f"ERROR: slide image not found: {path}", file=sys.stderr)
            sys.exit(1)
        paths.append(path)
    if not paths:
        return top
    slot = 12.43 / len(paths)
    y = 6.90 - 2.3
    if y < top + 0.4:
        y = top
    for index, path in enumerate(paths):
        slide.shapes.add_picture(path, inches(0.48 + index * slot + 0.08), inches(y), height=inches(2.05))
    return y - 0.1


def _chart_type(name, enum):
    key = str(name or "").strip().lower().replace("-", " ").replace("_", " ")
    if key in ("bar h", "bar horizontal", "horizontal bar", "条形"):
        return enum.BAR_CLUSTERED
    if key in ("line", "折线"):
        return enum.LINE
    if key in ("pie", "饼"):
        return enum.PIE
    if key in ("area", "面积"):
        return enum.AREA
    if key in ("radar", "雷达") and hasattr(enum, "RADAR"):
        return enum.RADAR
    return enum.COLUMN_CLUSTERED


def _charts(slide, inches, item, top, bottom=6.85):
    charts = item.get("charts") or []
    if not charts:
        return
    try:
        from pptx.chart.data import CategoryChartData
        from pptx.enum.chart import XL_CHART_TYPE
    except ImportError:
        print("ERROR: python-pptx chart support is unavailable.", file=sys.stderr)
        sys.exit(1)
    slot = 12.43 / len(charts)
    for index, spec in enumerate(charts):
        if not isinstance(spec, dict):
            continue
        data = CategoryChartData()
        data.categories = [_clean_xml_text(c) for c in (spec.get("categories") or [])]
        for series in spec.get("series") or []:
            if isinstance(series, dict):
                data.add_series(_clean_xml_text(series.get("name") or "系列"), series.get("values") or [])
        graphic = slide.shapes.add_chart(
            _chart_type(spec.get("chart_type"), XL_CHART_TYPE),
            inches(0.48 + index * slot), inches(top), inches(max(1.2, slot - 0.16)), inches(max(1.6, bottom - top)),
            data,
        )
        chart = graphic.chart
        chart.has_legend = len(spec.get("series") or []) > 1
        title = _clean_xml_text(spec.get("title") or "")
        chart.has_title = bool(title)
        if title:
            chart.chart_title.text_frame.paragraphs[0].text = title


def _add_notes(slide, notes):
    notes = _clean_xml_text(notes)
    if notes:
        slide.notes_slide.notes_text_frame.text = notes


def build_pptx(outline, output_path):
    try:
        from pptx import Presentation
        from pptx.util import Inches, Pt
    except ImportError:
        print("ERROR: python-pptx not installed. Run: pip install python-pptx", file=sys.stderr)
        sys.exit(1)

    style = _resolve_theme(outline.get("theme"), _style_hint(outline))
    cover_dark = bool(style["cover_dark"])
    theme = {key: _rgb(value) for key, value in style["colors"].items()}
    title = _clean_xml_text(outline.get("title") or "演示文稿")
    subtitle = _clean_xml_text(outline.get("subtitle") or "")
    slides = [item for item in outline["slides"] if isinstance(item, dict)]
    footer = title if len(title) <= 36 else title[:35] + "…"
    total = len(slides) + 1

    prs = Presentation()
    prs.slide_width = Inches(13.333)
    prs.slide_height = Inches(7.5)
    prs.core_properties.title = title
    blank = _blank_layout(prs)

    cover = prs.slides.add_slide(blank)
    title_color, sub_color, rule, _kicker = _feature_colors(theme, cover_dark)
    _feature_canvas(cover, Inches, theme, cover_dark)
    _tb(cover, Inches, Pt, 0.72, 1.85, 11.7, 1.55, title, 40, title_color, bold=True)
    _rect(cover, Inches, 0.72, 3.62, 2.15, 0.05, rule)
    if subtitle:
        _tb(cover, Inches, Pt, 0.72, 3.82, 11.4, 1.15, subtitle, 20, sub_color)
    _page_mark(cover, Inches, Pt, 1, total, sub_color)

    for index, item in enumerate(slides, start=2):
        slide = prs.slides.add_slide(blank)
        kind = _layout(item)
        slide_title = _clean_xml_text(item.get("title") or f"第 {index - 1} 页")
        kicker = _clean_xml_text(item.get("kicker") or "")
        bullets = item.get("bullets")
        if kind in ("section", "closing"):
            title_color, sub_color, rule, kicker_color = _feature_colors(theme, cover_dark)
            _feature_canvas(slide, Inches, theme, cover_dark)
            if kicker:
                _tb(slide, Inches, Pt, 0.72, 2.05, 11.5, 0.32, kicker, 14, rule if kind == "closing" else kicker_color, bold=True)
            _tb(slide, Inches, Pt, 0.72, 2.45, 11.6, 1.35, slide_title, 36, title_color, bold=True)
            _rect(slide, Inches, 0.72, 4.00, 2.15, 0.05, rule)
            _lines(slide, Inches, Pt, 0.72, 4.25, 11.2, 1.8, bullets, 18, title_color if kind == "closing" else sub_color)
            _page_mark(slide, Inches, Pt, index, total, sub_color)
        elif kind == "quote":
            texts = _texts(bullets)
            label = kicker or (slide_title if texts else "")
            quote = texts or [slide_title]
            top = _light_chrome(slide, Inches, Pt, theme, label, "", footer, index, total)
            _tb(slide, Inches, Pt, 0.48, top, 1.4, 0.8, "“", 60, theme["accent"], bold=True)
            _lines(slide, Inches, Pt, 0.48, top + 0.85, 12.2, 4.6, quote, 26, theme["navy"], space=10)
        else:
            top = _light_chrome(slide, Inches, Pt, theme, kicker, slide_title, footer, index, total)
            if kind == "cards":
                _cards(slide, Inches, Pt, theme, bullets, top)
            elif kind == "agenda":
                _agenda(slide, Inches, Pt, theme, bullets, top)
            elif kind == "kpi":
                _kpi(slide, Inches, Pt, theme, bullets, top)
            else:
                has_images = bool(item.get("images"))
                has_charts = bool(item.get("charts"))
                bullet_bottom = 6.90
                if has_images:
                    bullet_bottom = min(bullet_bottom, 4.50)
                    _images(slide, Inches, item, top)
                if has_charts:
                    chart_top = top if not _texts(bullets) else min(top + 1.7, 4.3)
                    bullet_bottom = min(bullet_bottom, chart_top - 0.08)
                    _charts(slide, Inches, item, chart_top, 4.40 if has_images else 6.85)
                _bullets(slide, Inches, Pt, theme, bullets, top, bullet_bottom)
        _add_notes(slide, item.get("notes") or "")

    prs.save(output_path)
    return output_path


def main():
    parser = argparse.ArgumentParser(description="Build a designed .pptx deck from a JSON outline")
    parser.add_argument("--input", default="", help="Outline JSON file path (omit to read from stdin)")
    parser.add_argument("--output", default=".", help="Output .pptx file path or directory")
    args = parser.parse_args()

    outline = _load_outline(args)
    output_path = _resolve_output_path(args.output, outline.get("title"))
    result = build_pptx(outline, output_path)
    print(f"PPTX 生成完成: {result}")
    print(f"   页数: {len(outline['slides']) + 1}")
    print(f"   输出: {os.path.abspath(result)}")


if __name__ == "__main__":
    main()
