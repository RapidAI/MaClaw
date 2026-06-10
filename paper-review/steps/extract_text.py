#!/usr/bin/env python3
"""Step 2: Extract text from PDF (with image placeholders)."""

import argparse, os, sys, glob, re

def extract_text_from_pdf(pdf_path: str) -> str:
    """Extract text from PDF using PyPDF2."""
    try:
        import PyPDF2
    except ImportError:
        print("ERROR: PyPDF2 not installed. Run: pip install PyPDF2", file=sys.stderr)
        sys.exit(1)

    text_parts = []
    try:
        with open(pdf_path, "rb") as f:
            reader = PyPDF2.PdfReader(f)
            num_pages = len(reader.pages)
            for i, page in enumerate(reader.pages):
                page_text = page.extract_text() or ""
                # Add page marker
                text_parts.append(f"\n\n=== Page {i+1}/{num_pages} ===\n")
                # Insert image placeholder markers
                text_parts.append(page_text)
                # Check for images on page
                if "/XObject" in page._objects or hasattr(page, "_images"):
                    try:
                        img_count = 0
                        if hasattr(page, "_images"):
                            img_count = len(page._images)
                        # Also try to detect images via resources
                        if "/XObject" in page._objects:
                            xobj = page._objects["/XObject"]
                            if xobj:
                                for key in xobj:
                                    if key and ("Image" in str(xobj[key]) or "image" in str(key).lower()):
                                        img_count += 1
                        if img_count > 0:
                            text_parts.append(f"\n[!PAGE_HAS_IMAGES: {img_count} images on page {i+1}]\n")
                    except:
                        pass
    except Exception as e:
        print(f"WARNING: Error extracting from {pdf_path}: {e}", file=sys.stderr)
        text_parts.append(f"[Extraction error on page {i+1}: {e}]\n")

    return "".join(text_parts)

def main():
    parser = argparse.ArgumentParser(description="Extract text from PDF")
    parser.add_argument("--pdf", required=True, help="PDF file path or glob pattern")
    parser.add_argument("--output", default=".", help="Output directory")
    args = parser.parse_args()

    os.makedirs(args.output, exist_ok=True)

    # Resolve glob pattern
    pdf_files = glob.glob(args.pdf)
    if not pdf_files:
        # Try as exact path
        if os.path.exists(args.pdf):
            pdf_files = [args.pdf]

    if not pdf_files:
        print("ERROR: No PDF files found matching pattern", file=sys.stderr)
        sys.exit(1)

    for pdf_path in pdf_files:
        pdf_path = os.path.abspath(pdf_path)
        print(f"Extracting text from: {os.path.basename(pdf_path)}")
        text = extract_text_from_pdf(pdf_path)

        base = os.path.splitext(os.path.basename(pdf_path))[0]
        txt_path = os.path.join(args.output, f"{base}_text.txt")
        with open(txt_path, "w", encoding="utf-8") as f:
            f.write(text)

        print(f"TEXT_SAVED: {txt_path} ({len(text)} chars)")
        print(f"TEXT_PATH: {txt_path}")

if __name__ == "__main__":
    main()
