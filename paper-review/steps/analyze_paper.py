#!/usr/bin/env python3
"""Step 3: LLM deep analysis of the paper content."""

import argparse, json, os, sys, glob, urllib.request, urllib.error

ANALYSIS_PROMPT = """You are a senior AI researcher giving a deep paper review for a group meeting presentation. Analyze the following paper and produce a structured JSON report.

Analyze these aspects in detail:

1. **problem** (1-2 paragraphs): What problem does this paper solve? Why is it important? What are the challenges?
2. **novelty** (list of 3-5 items): Key innovations/contributions of this paper.
3. **method_essence** (2-3 paragraphs): The core idea and essential principle behind the method. Explain the "why" 鈥?why does this approach work from a fundamental perspective?
4. **method_detail** (detailed, 4-6 paragraphs): In-depth technical explanation of the method:
   - Overall architecture/pipeline
   - Key mathematical formulations
   - Algorithmic steps
   - Important design choices and intuition
5. **experiments** (3-4 paragraphs): Experimental setup, datasets, baselines, main results, ablation studies, qualitative analysis.
6. **conclusion** (1-2 paragraphs): Summary of contributions, limitations, future work.

Output ONLY a valid JSON object with these exact keys. Be thorough and technically precise.

Paper text:
```
{paper_text}
```"""

def call_llm(prompt: str) -> str:
    """Call OpenAI-compatible API."""
    base_url = os.environ.get("OPENAI_BASE_URL", "").rstrip("/")
    api_key = os.environ.get("OPENAI_API_KEY", "")
    model = os.environ.get("OPENAI_MODEL", "gpt-4o-mini")

    url = f"{base_url}/chat/completions"
    payload = json.dumps({
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "temperature": 0.3,
        "max_tokens": 8192
    }).encode("utf-8")

    headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {api_key}"
    }

    req = urllib.request.Request(url, data=payload, headers=headers, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=300) as resp:
            body = json.loads(resp.read().decode("utf-8"))
            return body["choices"][0]["message"]["content"].strip()
    except Exception as e:
        print(f"API call failed: {e}", file=sys.stderr)
        raise

def parse_json_from_response(text: str) -> dict:
    """Extract JSON object from LLM response (handles markdown fences)."""
    # Remove markdown code fences
    text = text.strip()
    if text.startswith("```"):
        text = text.split("\n", 1)[1] if "\n" in text else text[3:]
    if text.endswith("```"):
        text = text.rsplit("```", 1)[0]
    text = text.strip()

    try:
        return json.loads(text)
    except json.JSONDecodeError:
        # Try to find JSON object
        start = text.find("{")
        end = text.rfind("}") + 1
        if start >= 0 and end > start:
            try:
                return json.loads(text[start:end])
            except:
                pass
        print(f"WARNING: Failed to parse JSON from response", file=sys.stderr)
        print(f"Response snippet: {text[:500]}", file=sys.stderr)
        raise

def format_review_md(review: dict, paper_title: str) -> str:
    """Format structured review as Markdown."""
    lines = []
    lines.append(f"# 璁烘枃娣卞害瑙ｈ鎶ュ憡\n")
    lines.append(f"**璁烘枃鏍囬**锛歿paper_title}\n")
    lines.append("---\n")

    lines.append("## 涓€銆佽В鍐崇殑闂\n")
    lines.append(review.get("problem", "N/A") + "\n")

    lines.append("## 浜屻€佹牳蹇冨垱鏂扮偣\n")
    for i, n in enumerate(review.get("novelty", []), 1):
        lines.append(f"{i}. {n}")
    lines.append("")

    lines.append("## 涓夈€佹柟娉曟湰璐ㄤ笌鏍稿績鎬濇兂\n")
    lines.append(review.get("method_essence", "N/A") + "\n")

    lines.append("## 鍥涖€佽缁嗗師鐞嗚В璇籠n")
    lines.append(review.get("method_detail", "N/A") + "\n")

    lines.append("## 浜斻€佸疄楠屾柟娉曞強缁撴灉鍒嗘瀽\n")
    lines.append(review.get("experiments", "N/A") + "\n")

    lines.append("## 鍏€佺粨璁轰笌鍚ず\n")
    lines.append(review.get("conclusion", "N/A") + "\n")

    return "\n".join(lines)

def main():
    parser = argparse.ArgumentParser(description="Analyze paper with LLM")
    parser.add_argument("--text", required=True, help="Paper text file path or glob")
    parser.add_argument("--output", default=".", help="Output directory")
    args = parser.parse_args()

    os.makedirs(args.output, exist_ok=True)

    txt_files = glob.glob(args.text)
    if not txt_files and os.path.exists(args.text):
        txt_files = [args.text]

    if not txt_files:
        print("ERROR: No text files found", file=sys.stderr)
        sys.exit(1)

    for txt_path in txt_files:
        txt_path = os.path.abspath(txt_path)
        with open(txt_path, "r", encoding="utf-8") as f:
            paper_text = f.read()

        paper_title = os.path.basename(txt_path).replace("_text.txt", "")
        print(f"Analyzing: {paper_title} ({len(paper_text)} chars)")

        prompt = ANALYSIS_PROMPT.format(paper_text=paper_text)
        print("Calling LLM for deep analysis...", flush=True)

        response = call_llm(prompt)
        review = parse_json_from_response(response)

        base = os.path.splitext(os.path.basename(txt_path))[0].replace("_text", "")

        # Save JSON
        json_path = os.path.join(args.output, f"{base}_review.json")
        with open(json_path, "w", encoding="utf-8") as f:
            json.dump(review, f, ensure_ascii=False, indent=2)
        print(f"JSON_SAVED: {json_path}")

        # Save Markdown
        md_text = format_review_md(review, paper_title)
        md_path = os.path.join(args.output, f"{base}_review.md")
        with open(md_path, "w", encoding="utf-8") as f:
            f.write(md_text)
        print(f"MD_SAVED: {md_path}")

        print(f"REVIEW_JSON: {json_path}")
        print(f"REVIEW_MD: {md_path}")

if __name__ == "__main__":
    main()
