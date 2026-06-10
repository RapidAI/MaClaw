#!/usr/bin/env python3
"""Step 4: Extract images from PDF pages for PPT绱犳潗."""

import argparse, os, sys, glob, subprocess, tempfile

def extract_images_pdf2image(pdf_path: str, output_dir: str, dpi: int = 200):
    """Extract pages as images using pdf2image."""
    try:
        from pdf2image import convert_from_path
    except ImportError:
        print("WARNING: pdf2image not installed. Trying alternative method...", file=sys.stderr)
        return False

    os.makedirs(output_dir, exist_ok=True)
    base = os.path.splitext(os.path.basename(pdf_path))[0]

    print(f"Extracting pages as images: {base} (DPI={dpi})")
    try:
        images = convert_from_path(pdf_path, dpi=dpi)
        for i, img in enumerate(images):
            img_path = os.path.join(output_dir, f"{base}_page_{i+1:03d}.png")
            img.save(img_path, "PNG")
            print(f"  Page {i+1}: {img_path} ({img.size})")

        print(f"Total: {len(images)} pages extracted")
        return True
    except Exception as e:
        print(f"ERROR: pdf2image extraction failed: {e}", file=sys.stderr)
        return False

def extract_images_mutool(pdf_path: str, output_dir: str):
    """Fallback: use mutool if available."""
    os.makedirs(output_dir, exist_ok=True)
    base = os.path.splitext(os.path.basename(pdf_path))[0]

    for ext in [".exe", ""]:
        mutool = f"mutool{ext}"
        try:
            result = subprocess.run(
                [mutool, "draw", "-o", os.path.join(output_dir, f"{base}_page_%03d.png"),
                 "-r", "200", pdf_path],
                capture_output=True, text=True, timeout=120
            )
            if result.returncode == 0:
                pngs = sorted(glob.glob(os.path.join(output_dir, f"{base}_page_*.png")))
                print(f"mutool extracted {len(pngs)} pages")
                return True
        except FileNotFoundError:
            continue
    return False

def main():
    parser = argparse.ArgumentParser(description="Extract images from PDF")
    parser.add_argument("--pdf", required=True, help="PDF file path or glob")
    parser.add_argument("--output", default="./images", help="Output directory for images")
    parser.add_argument("--dpi", type=int, default=200, help="Image DPI")
    args = parser.parse_args()

    pdf_files = glob.glob(args.pdf)
    if not pdf_files and os.path.exists(args.pdf):
        pdf_files = [args.pdf]

    if not pdf_files:
        print("ERROR: No PDF files found", file=sys.stderr)
        sys.exit(1)

    os.makedirs(args.output, exist_ok=True)

    for pdf_path in pdf_files:
        pdf_path = os.path.abspath(pdf_path)
        pdf_name = os.path.basename(pdf_path)

        # Try pdf2image first
        if extract_images_pdf2image(pdf_path, args.output, args.dpi):
            continue

        # Try mutool fallback
        if extract_images_mutool(pdf_path, args.output):
            continue

        print(f"WARNING: No image extraction method available for {pdf_name}", file=sys.stderr)
        print("Install: pip install pdf2image and ensure poppler is in PATH", file=sys.stderr)

        # Create a note file
        note_path = os.path.join(args.output, "_extraction_note.txt")
        with open(note_path, "w") as f:
            f.write(f"Image extraction failed for {pdf_name}.\n")
            f.write("Install poppler: https://github.com/oschwartz10612/poppler-windows/releases/\n")
            f.write("Then add bin/ directory to PATH.\n")

    print(f"IMAGES_DIR: {args.output}")

if __name__ == "__main__":
    main()
