"""PDF to Word converter using PyMuPDF + python-docx."""
import argparse
import os
import re
import sys


# XML 1.0 allows: #x9 | #xA | #xD | [#x20-#xD7FF] | [#xE000-#xFFFD] | [#x10000-#x10FFFF]
# Remove all characters outside this set to prevent lxml ValueError.
_ILLEGAL_XML_CHARS_RE = re.compile(
    r'[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x84\x86-\x9f'
    r'\ud800-\udfff\ufdd0-\ufdef\ufffe\uffff]'
)


def _clean_xml_text(text: str) -> str:
    """Remove characters that are not valid in XML 1.0."""
    if not text:
        return text
    return _ILLEGAL_XML_CHARS_RE.sub('', text)

def convert_pdf_to_docx(input_path: str, output_path: str) -> str:
    """Convert a PDF file to DOCX preserving text and basic layout."""
    try:
        import fitz  # pymupdf
    except ImportError:
        print("ERROR: pymupdf not installed. Run: pip install pymupdf", file=sys.stderr)
        sys.exit(1)
    try:
        from docx import Document
        from docx.shared import Pt, Inches
    except ImportError:
        print("ERROR: python-docx not installed. Run: pip install python-docx", file=sys.stderr)
        sys.exit(1)

    if not os.path.isfile(input_path):
        print(f"ERROR: Input file not found: {input_path}", file=sys.stderr)
        sys.exit(1)

    # Determine output path
    if os.path.isdir(output_path):
        base = os.path.splitext(os.path.basename(input_path))[0]
        output_path = os.path.join(output_path, f"{base}.docx")
    elif not output_path.lower().endswith('.docx'):
        output_path += '.docx'

    os.makedirs(os.path.dirname(output_path) or '.', exist_ok=True)

    doc = Document()
    pdf = fitz.open(input_path)

    if pdf.is_encrypted:
        print("ERROR: PDF is encrypted/password-protected. Please provide an unprotected PDF.", file=sys.stderr)
        pdf.close()
        sys.exit(1)

    page_count = len(pdf)

    for page_num in range(page_count):
        page = pdf[page_num]
        blocks = page.get_text("dict")["blocks"]
        page_has_images = False

        for block in blocks:
            if block["type"] == 0:  # text block
                for line in block.get("lines", []):
                    text_parts = []
                    for span in line.get("spans", []):
                        text_parts.append(span["text"])
                    line_text = "".join(text_parts).strip()
                    # Clean illegal XML characters that may exist in PDF text
                    line_text = _clean_xml_text(line_text)
                    if line_text:
                        para = doc.add_paragraph()
                        # Detect heading by font size
                        max_size = max((span.get("size", 11) for span in line.get("spans", [])), default=11)
                        if max_size >= 18:
                            para.style = 'Heading 1'
                        elif max_size >= 14:
                            para.style = 'Heading 2'
                        else:
                            para.style = 'Normal'
                        
                        for span in line.get("spans", []):
                            run = para.add_run(_clean_xml_text(span["text"]))
                            run.font.size = Pt(span.get("size", 11))
                            flags = span.get("flags", 0)
                            if flags & 16:  # bit 4 = bold
                                run.bold = True
                            if flags & 2:   # bit 1 = italic
                                run.italic = True

            elif block["type"] == 1:  # image block marker
                page_has_images = True

        # Extract and embed page images once (after all blocks processed)
        if page_has_images:
            try:
                for img_info in page.get_images(full=True):
                    xref = img_info[0]
                    base_image = pdf.extract_image(xref)
                    if base_image and base_image.get("image"):
                        img_ext = base_image.get("ext", "png")
                        img_path = os.path.join(
                            os.path.dirname(output_path) or '.',
                            f"_img_p{page_num}_{xref}.{img_ext}"
                        )
                        with open(img_path, "wb") as f:
                            f.write(base_image["image"])
                        doc.add_picture(img_path, width=Inches(5))
                        os.remove(img_path)
            except Exception:
                pass  # Skip images that fail to extract

        # Add page break between pages (except last)
        if page_num < page_count - 1:
            doc.add_page_break()

    pdf.close()
    doc.save(output_path)
    print(f"✅ 转换完成: {output_path}")
    print(f"   页数: {page_count}")
    print(f"   输出: {os.path.abspath(output_path)}")
    return output_path


def main():
    parser = argparse.ArgumentParser(description="Convert PDF to Word (DOCX)")
    parser.add_argument("--input", required=True, help="Input PDF file path")
    parser.add_argument("--output", default=".", help="Output DOCX file path or directory")
    args = parser.parse_args()
    convert_pdf_to_docx(args.input, args.output)


if __name__ == "__main__":
    main()
