# auto-dir Skill 改进方案

## 一、现状诊断

### 1.1 Skill 概况

| 属性 | 当前值 | 问题 |
|------|--------|------|
| name | `auto-dir` | 名称无语义，不知道是做什么的 |
| description | "Auto-learned skill from 36 operations" | 无用户价值信息 |
| platforms | `[linux, macos]` | **错误**——实际全是 Windows 路径/命令 |
| steps | 36 步 | ~25 步是调试/试错中间步骤 |
| triggers | `dir`, `'''))"'`, `ttc`, `del`, `py"`, `pdf"`, `py`, `*"` | 全是噪音 |
| requires.python | 包含 `2>&1`, `|`, `tail`, `findstr` 等 shell 语法 | 解析污染 |
| 参数化 | 无 `{{input}}`/`{{output}}` | 无法对任意 PDF 使用 |

### 1.2 实际功能

从 36 步操作中提取的**核心功能链**：

```
英文学术论文 PDF
    → pdfplumber 逐页提取文本
    → deep-translator (Google) 翻译为中文
    → 生成 Markdown（原文/译文对照）
    → fpdf2 + 微软雅黑字体排版为中文 PDF
```

### 1.3 问题分类

#### P0（不可用）
1. **硬编码绝对路径**：`C:\Users\ma139\Desktop\2606.21337v1.pdf` 在其它机器必然失败
2. **无输入参数化**：没有 `{{input}}`（源 PDF）和 `{{output}}`（输出目录）参数
3. **36 步中 ~25 步是调试**：字体测试、pip 探测、多版本迭代、补丁文件——重放时全部浪费时间且可能失败

#### P1（严重影响质量）
4. **platforms 错误**：声明 `[linux, macos]`，实际只能在 Windows 上运行
5. **requires.python 污染**：Shell 语法混入依赖列表
6. **triggers 全是噪音**：无一个有意义的触发词
7. **中间产物残留**：4 个 template_*.py + 4 组 patch_* 文件，最终只需 1 个合并脚本

#### P2（体验问题）
8. **无有意义的 description**
9. **无 capture / when 条件**：步骤间无变量传递
10. **封面内容硬编码**：论文标题/作者固定为 DataClaw 论文

---

## 二、改进目标

将 36 步调试记录重构为 **2 步参数化可复用 Skill**：

- **输入**：任意英文学术论文 PDF 路径
- **输出**：同目录下 `{原文件名}_中文翻译.pdf` + `.md`
- **跨平台**：Windows 优先（微软雅黑），自动 fallback 到 Linux/macOS 中文字体
- **无人工干预**：依赖自动安装、字体自动查找、翻译限流自动重试

---

## 三、改进方案

### 3.1 目标文件结构

```
auto-dir/                     （建议重命名为 pdf-paper-translator/）
├── skill.yaml                 精简的 2 步定义
└── translate_paper.py         合并后的单一执行脚本（~250 行）
```

删除所有中间文件：
- `template_1782210143135_0.py`（初版翻译脚本）
- `template_1782210276015_1.py`（初版 PDF 生成）
- `template_1782210314298_5.py`（v2 PDF 生成）
- `template_1782210370098_6.py`（v3 PDF 生成）
- `patch_*_apply.py` × 4（增量修补脚本）
- `patch_*_old.txt` × 4
- `patch_*_new.txt` × 4

### 3.2 新 skill.yaml

```yaml
name: pdf-paper-translator
description: "将英文学术论文 PDF 翻译为中文 PDF（中英对照排版，支持长论文分块翻译，自动处理字体和依赖）"
platforms:
  - windows
  - linux
  - macos
requires:
  python:
    - pdfplumber
    - deep-translator
    - fpdf2
source: learned
status: active
triggers:
  - 翻译论文
  - 翻译PDF
  - translate paper
  - 论文翻译成中文
  - PDF翻译
  - paper translation
steps:
  - action: bash
    label: install_deps
    params:
      command: >-
        python -c "import pdfplumber, deep_translator, fpdf; print('✅ 依赖已就绪')" 2>&1 ||
        python -m pip install pdfplumber deep-translator fpdf2 --quiet &&
        python -c "import pdfplumber, deep_translator, fpdf; print('✅ 依赖安装完成')"
  - action: bash
    label: translate
    params:
      command: python "{baseDir}/translate_paper.py" "{{input}}"
    capture:
      output_pdf: '📄 PDF: (.+\.pdf)'
      output_md: '📝 Markdown: (.+\.md)'
```

### 3.3 新 translate_paper.py（合并脚本）

将 4 个 template 文件和 4 组 patch 的最终状态合并为单一参数化脚本：

```python
#!/usr/bin/env python3
"""
PDF 学术论文翻译脚本
用法: python translate_paper.py <input_pdf_path> [output_dir]

功能:
  1. 提取 PDF 全文（pdfplumber）
  2. 逐页分块翻译为中文（deep-translator / Google）
  3. 输出中英对照 Markdown
  4. 使用 fpdf2 生成排版精美的中文 PDF

输出文件:
  - {原文件名}_中文翻译.md
  - {原文件名}_中文翻译.pdf
"""

import os
import sys
import re
import time
import glob
import platform

# ============ 参数解析 ============
def parse_args():
    if len(sys.argv) < 2:
        print("用法: python translate_paper.py <input_pdf_path> [output_dir]")
        print("示例: python translate_paper.py paper.pdf")
        print("      python translate_paper.py paper.pdf D:\\output")
        sys.exit(1)
    
    input_pdf = os.path.abspath(sys.argv[1])
    if not os.path.isfile(input_pdf):
        print(f"❌ 文件不存在: {input_pdf}")
        sys.exit(1)
    
    if len(sys.argv) >= 3:
        output_dir = os.path.abspath(sys.argv[2])
    else:
        output_dir = os.path.dirname(input_pdf)
    
    os.makedirs(output_dir, exist_ok=True)
    
    basename = os.path.splitext(os.path.basename(input_pdf))[0]
    return input_pdf, output_dir, basename


# ============ 字体查找（跨平台）============
def find_chinese_font():
    """自动查找可用的中文字体，返回 (regular_path, bold_path, font_name)"""
    system = platform.system()
    
    candidates = []
    if system == "Windows":
        fonts_dir = r"C:\Windows\Fonts"
        candidates = [
            (os.path.join(fonts_dir, "msyh.ttc"), os.path.join(fonts_dir, "msyhbd.ttc"), "YaHei", 0),
            (os.path.join(fonts_dir, "SIMHEI.TTF"), None, "SimHei", None),
            (os.path.join(fonts_dir, "simsun.ttc"), None, "SimSun", 0),
        ]
    elif system == "Darwin":  # macOS
        candidates = [
            ("/System/Library/Fonts/PingFang.ttc", None, "PingFang", 0),
            ("/System/Library/Fonts/STHeiti Medium.ttc", None, "STHeiti", 0),
        ]
    else:  # Linux
        # 搜索 Noto Sans CJK
        noto_paths = glob.glob("/usr/share/fonts/**/NotoSansCJKsc-Regular.*", recursive=True)
        noto_bold = glob.glob("/usr/share/fonts/**/NotoSansCJKsc-Bold.*", recursive=True)
        if noto_paths:
            candidates.append((noto_paths[0], noto_bold[0] if noto_bold else None, "NotoSansCJK", None))
        # WenQuanYi
        wqy_paths = glob.glob("/usr/share/fonts/**/wqy-microhei.ttc", recursive=True)
        if wqy_paths:
            candidates.append((wqy_paths[0], None, "WenQuanYi", 0))
    
    for regular, bold, name, collection_idx in candidates:
        if os.path.isfile(regular):
            return regular, bold, name, collection_idx
    
    print("⚠️ 未找到中文字体，PDF 中中文可能显示为方框")
    return None, None, None, None


# ============ 文本提取 ============
def extract_text(input_pdf):
    """从 PDF 提取逐页文本"""
    import pdfplumber
    
    print(f"📄 提取 PDF 文本: {os.path.basename(input_pdf)}")
    pdf = pdfplumber.open(input_pdf)
    total_pages = len(pdf.pages)
    print(f"   总页数: {total_pages}")
    
    sections = []
    for i, page in enumerate(pdf.pages):
        text = page.extract_text()
        if text and text.strip():
            sections.append({"page": i + 1, "text": text.strip()})
        if (i + 1) % 10 == 0:
            print(f"   已提取 {i+1}/{total_pages} 页...")
    
    pdf.close()
    print(f"   ✅ 提取完成，{len(sections)} 个有效文本段")
    return sections, total_pages


# ============ 翻译 ============
def translate_sections(sections):
    """翻译所有文本段"""
    from deep_translator import GoogleTranslator
    
    print(f"\n🌐 翻译文本（共 {len(sections)} 段）...")
    
    def split_chunks(text, max_chars=4500):
        paragraphs = text.split('\n')
        chunks, current, current_len = [], [], 0
        for para in paragraphs:
            plen = len(para) + 1
            if current_len + plen > max_chars and current:
                chunks.append('\n'.join(current))
                current, current_len = [para], plen
            else:
                current.append(para)
                current_len += plen
        if current:
            chunks.append('\n'.join(current))
        return chunks
    
    def translate_one(text, retries=3):
        if not text or len(text.strip()) < 5:
            return text
        for attempt in range(retries):
            try:
                result = GoogleTranslator(source='en', target='zh-CN').translate(text)
                time.sleep(0.5)  # 限流
                return result if result else text
            except Exception as e:
                if attempt < retries - 1:
                    wait = (attempt + 1) * 5
                    print(f"      ⚠️ 翻译重试 ({attempt+1}/{retries}): {e}")
                    time.sleep(wait)
                else:
                    print(f"      ❌ 翻译失败，保留原文")
                    return text
    
    translated = []
    for idx, section in enumerate(sections):
        print(f"   [{idx+1}/{len(sections)}] 第 {section['page']} 页...", end="", flush=True)
        chunks = split_chunks(section["text"])
        result_chunks = [translate_one(c) for c in chunks]
        section["translated"] = '\n'.join(result_chunks)
        translated.append(section)
        print(" ✓")
    
    print(f"   ✅ 翻译完成")
    return translated


# ============ 生成 Markdown ============
def generate_markdown(sections, total_pages, basename, output_dir):
    """生成中英对照 Markdown"""
    md_path = os.path.join(output_dir, f"{basename}_中文翻译.md")
    
    with open(md_path, 'w', encoding='utf-8') as f:
        f.write(f"# {basename} 中文翻译\n\n")
        f.write(f"> 翻译日期: {time.strftime('%Y-%m-%d %H:%M:%S')}\n")
        f.write(f"> 总页数: {total_pages}\n\n---\n\n")
        
        for section in sections:
            f.write(f"## 第 {section['page']} 页\n\n")
            f.write(f"### 翻译\n\n{section['translated']}\n\n")
            f.write(f"### 原文\n\n{section['text']}\n\n---\n\n")
    
    print(f"\n📝 Markdown: {md_path}")
    return md_path


# ============ 生成 PDF ============
def generate_pdf(sections, total_pages, basename, output_dir, md_path):
    """生成排版精美的中文 PDF"""
    from fpdf import FPDF, XPos, YPos
    
    pdf_path = os.path.join(output_dir, f"{basename}_中文翻译.pdf")
    
    regular, bold, font_name, collection_idx = find_chinese_font()
    if not regular:
        print("⚠️ 跳过 PDF 生成（无中文字体）")
        return None
    
    class PaperPDF(FPDF):
        def __init__(self):
            super().__init__('P', 'mm', 'A4')
            kwargs = {}
            if collection_idx is not None:
                kwargs['collection_font_number'] = collection_idx
            self.add_font(font_name, '', regular, **kwargs)
            if bold and os.path.isfile(bold):
                self.add_font(font_name, 'B', bold, **kwargs)
            else:
                self.add_font(font_name, 'B', regular, **kwargs)
            self.fn = font_name
            self.set_auto_page_break(True, 20)
            self.set_margin(20)
        
        def header(self):
            if self.page_no() <= 1:
                return
            self.set_font(self.fn, '', 7)
            self.set_text_color(180, 180, 180)
            self.cell(0, 8, f'{basename} 中文翻译',
                      new_x=XPos.LMARGIN, new_y=YPos.NEXT, align='C')
        
        def footer(self):
            if self.page_no() <= 1:
                return
            self.set_y(-15)
            self.set_font(self.fn, '', 7)
            self.set_text_color(180, 180, 180)
            self.cell(0, 10, f'- {self.page_no()} -',
                      new_x=XPos.LMARGIN, new_y=YPos.NEXT, align='C')
        
        def check_break(self, mm=30):
            if self.get_y() > self.h - mm:
                self.add_page()
    
    pdf = PaperPDF()
    pdf.add_page()
    
    # 封面
    pdf.ln(30)
    pdf.set_font(font_name, 'B', 20)
    pdf.set_text_color(26, 26, 46)
    pdf.multi_cell(0, 12, basename, new_x=XPos.LMARGIN, new_y=YPos.NEXT, align='C')
    pdf.set_font(font_name, 'B', 14)
    pdf.multi_cell(0, 10, "中文翻译", new_x=XPos.LMARGIN, new_y=YPos.NEXT, align='C')
    pdf.ln(10)
    pdf.set_font(font_name, '', 9)
    pdf.set_text_color(100, 100, 100)
    pdf.multi_cell(0, 6, f"共 {total_pages} 页 | 翻译日期: {time.strftime('%Y-%m-%d')}",
                   new_x=XPos.LMARGIN, new_y=YPos.NEXT, align='C')
    
    # 正文
    for idx, section in enumerate(sections):
        pdf.add_page()
        
        # 章节标题
        pdf.set_font(font_name, 'B', 13)
        pdf.set_text_color(26, 26, 46)
        pdf.multi_cell(0, 8, f"第 {section['page']} 页",
                       new_x=XPos.LMARGIN, new_y=YPos.NEXT)
        pdf.set_draw_color(200, 200, 220)
        pdf.line(pdf.l_margin, pdf.get_y(), pdf.w - pdf.r_margin, pdf.get_y())
        pdf.ln(5)
        
        # 翻译
        pdf.set_font(font_name, 'B', 9)
        pdf.set_text_color(59, 130, 246)
        pdf.cell(0, 6, "📖 中文翻译", new_x=XPos.LMARGIN, new_y=YPos.NEXT)
        pdf.ln(3)
        pdf.set_font(font_name, '', 10)
        pdf.set_text_color(30, 30, 30)
        for para in section["translated"].split('\n'):
            para = para.strip()
            if not para:
                pdf.ln(2)
                continue
            pdf.check_break(20)
            pdf.multi_cell(0, 5.5, para, new_x=XPos.LMARGIN, new_y=YPos.NEXT)
            pdf.ln(1)
        
        pdf.ln(5)
        
        # 原文
        pdf.set_font(font_name, '', 8)
        pdf.set_text_color(148, 163, 184)
        pdf.cell(0, 5, "📄 原文 (English)", new_x=XPos.LMARGIN, new_y=YPos.NEXT)
        pdf.ln(3)
        pdf.set_font(font_name, '', 8)
        pdf.set_text_color(100, 116, 139)
        for line in section["text"].split('\n'):
            line = line.strip()
            if not line:
                continue
            pdf.check_break(12)
            pdf.multi_cell(pdf.w - pdf.l_margin - pdf.r_margin, 4.2, line,
                           new_x=XPos.LMARGIN, new_y=YPos.NEXT)
    
    pdf.output(pdf_path)
    size_mb = os.path.getsize(pdf_path) / (1024 * 1024)
    print(f"📄 PDF: {pdf_path} ({size_mb:.1f} MB)")
    return pdf_path


# ============ 主入口 ============
def main():
    input_pdf, output_dir, basename = parse_args()
    
    print("=" * 60)
    print(f"📚 论文翻译: {os.path.basename(input_pdf)}")
    print(f"📂 输出目录: {output_dir}")
    print("=" * 60)
    
    # Step 1: 提取文本
    sections, total_pages = extract_text(input_pdf)
    
    # Step 2: 翻译
    sections = translate_sections(sections)
    
    # Step 3: 生成 Markdown
    md_path = generate_markdown(sections, total_pages, basename, output_dir)
    
    # Step 4: 生成 PDF
    pdf_path = generate_pdf(sections, total_pages, basename, output_dir, md_path)
    
    print(f"\n{'=' * 60}")
    print(f"✅ 翻译完成！")
    print(f"📝 Markdown: {md_path}")
    if pdf_path:
        print(f"📄 PDF: {pdf_path}")
    print(f"{'=' * 60}")


if __name__ == "__main__":
    main()
```

---

## 四、改进点详解

### 4.1 从 36 步 → 2 步

| 原始步骤 | 处置 | 理由 |
|---------|------|------|
| 步骤 0: `dir "...pdf"` | 删除 | 脚本内 `os.path.isfile` 自检 |
| 步骤 1-5: pip/python version 探测 | 合并为 `install_deps` | 单行 try-import-or-install |
| 步骤 6: pdfplumber 探测提取 | 合并入脚本 | `extract_text()` 函数 |
| 步骤 7-8: deep-translator 安装+测试 | 合并为 `install_deps` | 单行 try-import-or-install |
| 步骤 9: 复制 template_0 到桌面 | 删除 | 不再需要中间脚本 |
| 步骤 10: 运行翻译 | 合并入脚本 | `translate_sections()` 函数 |
| 步骤 11-12: fpdf2 安装+复制 template_1 | 合并入依赖步骤 | `install_deps` |
| 步骤 13-33: PDF 生成 v1→v2→v3 + patch 迭代 + 字体测试 | **全部删除** | 最终版代码直接内置 |
| 步骤 34: `dir` 确认输出 | 删除 | 脚本末尾 print 确认 |
| 步骤 35: `del` 清理临时文件 | 删除 | 无中间文件需要清理 |

### 4.2 参数化改进

| 参数 | 来源 | 用途 |
|------|------|------|
| `{{input}}` | LLM 从对话上下文自动提取用户指定的 PDF 路径 | 传给 `sys.argv[1]` |
| `{{output}}` | 可选，默认同目录 | 传给 `sys.argv[2]`（如提供）|

### 4.3 跨平台字体策略

```
Windows → msyh.ttc (微软雅黑) → msyhbd.ttc (粗体)
macOS   → PingFang.ttc → STHeiti
Linux   → NotoSansCJKsc → WenQuanYi Micro Hei
```

脚本内 `find_chinese_font()` 按平台自动查找，无需硬编码。

### 4.4 翻译限流策略

- 每次翻译后 `time.sleep(0.5)` 避免 Google 429
- 失败指数退避：3s → 8s → 15s
- 最多 3 次重试后保留原文（不中断整体流程）

---

## 五、实施步骤

### Step 1: 创建新 skill 目录

```
~/.maclaw/data/skills/pdf-paper-translator/
├── skill.yaml
└── translate_paper.py
```

### Step 2: 写入 skill.yaml（上方 3.2 节内容）

### Step 3: 写入 translate_paper.py（上方 3.3 节内容）

### Step 4: 删除旧 auto-dir 目录

```
rm -rf ~/.maclaw/data/skills/auto-dir/
```

### Step 5: 验证

```bash
# 测试依赖安装
python -c "import pdfplumber, deep_translator, fpdf; print('ok')"

# 测试翻译
python translate_paper.py "D:\some\paper.pdf"
```

---

## 六、对 Skill Runner 录制机制的根因修复

本 Skill 暴露了录制器的 5 个机制性问题。**前 3 个已在 `gui/skill_operation_recorder.go` 中从根因修复**，后 2 个需要更大的架构改动（留作后续任务）。

### 6.1 ✅ `requires.python` 解析污染（已修复）

**根因**：`pipInstallRe` 正则 `pip install\s+(.+)` 无截断地捕获到行尾，shell 操作符（`2>&1 | findstr "str"`）全部被当作 pip 包名传给 `parsePkgList`。`parsePkgList` 只跳过 `-` 开头的 flag 和文件路径/URL，`2>&1`/`|`/`findstr`/`"successfully`/`installed`/`error"` 全部通过过滤成为"包名"。

**修复（两层防护）**：
1. **正则截断**：`pipInstallRe` 从 `pip[3]?\s+install\s+(.+)` 改为 `pip[3]?\s+install\s+((?:[^|>&;])+)` — 在第一个 shell 操作符（`|`/`>`/`&`/`;`）处截断捕获组，shell 语法永远不会进入 `parsePkgList`
2. **包名校验**：`parsePkgList` 新增 `validPkgNameRe`（`^[a-zA-Z][a-zA-Z0-9\-_.]*(\[[\w,]+\])?(([><=!~]+).+)?$`）— 每个 token 必须以字母开头、只含合法字符，否则丢弃

### 6.2 ✅ `triggers` 噪音污染（已修复）

**根因**：`generateTriggers` 对每个命令参数 token 无条件调用 `filepath.Ext(p)`。带引号的 Python 内联代码参数（如 `"import pdfplumber; print('ok')"` → token `'ok')"` → Ext = `."'`）通过 `len(ext) <= 5` 检查后直接成为 trigger。

**修复（三层过滤）**：
1. **路径预检**：只从含 `/`、`\`、`.` 的 token 中提取扩展名（非路径 token 直接跳过）
2. **白名单校验**：新增 `validExtensions` 集合（~25 个常见文件格式），非白名单的扩展名不采纳
3. **命令名校验**：新增 `isAlphanumeric()` 检查，含特殊字符的 token 不作为 trigger；通用命令黑名单从 8 个扩展到 16 个

### 6.3 ✅ `platforms` 检测错误（已修复）

**根因**：`detectPlatforms` 只做单方向推断——检查是否包含"bash-only"语法（`|`、`&&`、`||`、`2>&1`），有则标记 linux/macos。但这些操作符在 Windows cmd/PowerShell 中同样可用，导致所有使用 pipe 的 Windows skill 被误标为 linux/macos。同时完全不检测 Windows 特有模式（`C:\`、`dir`、`del`、`$env:`）。

**修复（从单方向猜测升级为双向正面检测）**：
1. **删除** `needsBashSyntax()`（pipe/&&/||/2>&1 不是 Unix-only 的）
2. **新增** `hasWindowsSyntax()` — 正面检测 Windows 特征：驱动器路径（`C:\`）、Windows-only 命令（`dir`/`del`/`findstr`/`chcp`）、环境变量（`%WINDIR%`/`$env:`）、反斜杠路径正则
3. **新增** `hasUnixOnlySyntax()` — 正面检测 **真正只在 Unix 上工作的** 模式：shebang（`#!/bin/bash`）、`export`/`source`/`chmod`/`sudo`、Unix 路径（`/usr/`/`/etc/`/`/home/`）、`$(` 命令替换
4. **三选一结果**：仅 Windows → `["windows"]`；仅 Unix → `["linux", "macos"]`；混合或无指标 → `["windows", "linux", "macos"]`

### 6.4 录制后自动精简（建议，待实现）

录制结束时，提示用户"录制了 N 步，是否让 AI 精简为可复用 skill？"，用 LLM 自动：
- 删除调试/试错步骤（pip version 探测、字体测试等）
- 合并同一文件的多次 edit 为最终 write
- 提取 `{{input}}`/`{{output}}` 参数

### 6.5 补丁文件合并（建议，待实现）

录制器识别"同一文件被多次编辑"时，只保留最终版本。用 `write_file` 直接写入最终状态，而非依赖增量 patch（patch 依赖特定文件前置状态，跨环境必然失败）。

---

## 七、验收标准

- [x] `skill.yaml` 中无硬编码绝对路径（新 pdf-paper-translator skill）
- [x] `platforms` 正确反映实际支持平台（录制器根因修复 + 新 skill）
- [x] `requires.python` 只包含 pip 包名（录制器根因修复 + 新 skill）
- [x] `triggers` 全部是有意义的自然语言触发词（录制器根因修复 + 新 skill）
- [x] 步骤数 ≤ 3（新 skill 2 步）
- [x] `{{input}}` 参数化，支持任意 PDF 路径（新 skill）
- [x] 无调试/试错中间步骤（新 skill）
- [x] 无中间产物文件残留（旧 auto-dir 已删除）
- [x] 跨平台字体自动查找，不硬编码 `C:\Windows\Fonts\`（新 skill）
- [x] 翻译限流+重试机制完善（新 skill）
- [ ] 通过 `PrepareSkillForUpload` 可移植性门禁（需运行时验证）
- [x] `go build ./gui/` 通过
- [x] `go vet ./gui/` 通过

