#!/usr/bin/env python3
"""Step 1: Download paper PDF from URL (arXiv / DOI / direct PDF link)."""

import argparse, os, re, urllib.request, urllib.error, sys

def download_pdf(url: str, output_dir: str) -> str:
    """Download PDF from URL, return local path."""
    os.makedirs(output_dir, exist_ok=True)

    url = url.strip()

    # Handle arXiv
    arxiv_id = None
    m = re.search(r'arxiv\.org/(?:abs|pdf)/(\d+\.\d+)', url)
    if m:
        arxiv_id = m.group(1)

    m2 = re.search(r'(\d{4}\.\d{4,})(v\d+)?', url)
    if m2 and not arxiv_id:
        arxiv_id = m2.group(1)

    if arxiv_id:
        pdf_url = f"https://arxiv.org/pdf/{arxiv_id}.pdf"
        filename = f"{arxiv_id}.pdf"
    else:
        # Direct PDF or DOI
        if url.endswith('.pdf') or '/pdf/' in url:
            pdf_url = url
            filename = os.path.basename(url.split('?')[0])
        else:
            # DOI link - try to resolve
            pdf_url = url
            filename = "paper.pdf"

    local_path = os.path.join(output_dir, filename)

    if os.path.exists(local_path) and os.path.getsize(local_path) > 1000:
        print(f"EXISTS: {local_path}")
        return local_path

    print(f"Downloading: {pdf_url}")
    try:
        req = urllib.request.Request(pdf_url, headers={
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
        })
        with urllib.request.urlopen(req, timeout=60) as resp:
            data = resp.read()
            if len(data) < 1000:
                print(f"ERROR: Downloaded file too small ({len(data)} bytes)", file=sys.stderr)
                sys.exit(1)
            with open(local_path, "wb") as f:
                f.write(data)
        print(f"SAVED: {local_path} ({len(data)//1024} KB)")
    except Exception as e:
        print(f"ERROR: Download failed - {e}", file=sys.stderr)
        sys.exit(1)

    return local_path

def main():
    parser = argparse.ArgumentParser(description="Download paper PDF")
    parser.add_argument("--url", required=True, help="Paper URL (arXiv/DOI/direct PDF)")
    parser.add_argument("--output", default=".", help="Output directory")
    args = parser.parse_args()

    path = download_pdf(args.url, args.output)
    print(f"PDF_PATH: {path}")

if __name__ == "__main__":
    main()
