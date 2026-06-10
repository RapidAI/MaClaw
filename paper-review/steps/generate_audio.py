#!/usr/bin/env python3
"""Step 6: Generate narration audio (MP3) from review text."""

import argparse, os, sys, glob, re, asyncio, json, urllib.request, urllib.error

def generate_audio_edge_tts(text: str, output_path: str, voice: str = "zh-CN-XiaoxiaoNeural"):
    """Generate audio using Edge TTS."""
    try:
        import edge_tts
    except ImportError:
        print("WARNING: edge-tts not installed, trying pip install...", file=sys.stderr)
        import subprocess
        subprocess.run([sys.executable, "-m", "pip", "install", "edge-tts"],
                       capture_output=True, timeout=60)
        try:
            import edge_tts
        except ImportError:
            return False

    async def _tts():
        communicate = edge_tts.Communicate(text, voice)
        await communicate.save(output_path)

    try:
        asyncio.run(_tts())
        return os.path.exists(output_path) and os.path.getsize(output_path) > 100
    except Exception as e:
        print(f"WARNING: Edge TTS failed: {e}", file=sys.stderr)
        return False


def generate_audio_openai_tts(text: str, output_path: str, voice: str = "alloy"):
    """Generate audio using OpenAI TTS API."""
    base_url = os.environ.get("OPENAI_BASE_URL", "").rstrip("/")
    api_key = os.environ.get("OPENAI_API_KEY", "")

    url = f"{base_url}/audio/speech"
    payload = json.dumps({
        "model": "tts-1",
        "input": text,
        "voice": voice,
        "response_format": "mp3"
    }).encode("utf-8")

    headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {api_key}"
    }

    try:
        req = urllib.request.Request(url, data=payload, headers=headers, method="POST")
        with urllib.request.urlopen(req, timeout=120) as resp:
            with open(output_path, "wb") as f:
                f.write(resp.read())
        return os.path.exists(output_path) and os.path.getsize(output_path) > 100
    except Exception as e:
        print(f"WARNING: OpenAI TTS failed: {e}", file=sys.stderr)
        return False


def split_text_into_sections(md_text: str) -> list:
    """Split review markdown into sections for audio narration."""
    sections = []

    # Extract sections by markdown headers
    current_section = "寮曡█"
    current_content = []

    for line in md_text.split("\n"):
        if line.startswith("## ") or line.startswith("# "):
            if current_content:
                text = " ".join(current_content).strip()
                if len(text) > 20:
                    sections.append({"title": current_section, "text": text})
            current_section = line.replace("#", "").strip()
            current_content = []
        else:
            # Skip markdown formatting
            clean = re.sub(r'\*\*|__|\*|`', '', line)
            if clean.strip():
                current_content.append(clean)

    if current_content:
        text = " ".join(current_content).strip()
        if len(text) > 20:
            sections.append({"title": current_section, "text": text})

    return sections


def prepare_narration_text(md_text: str, max_chars: int = 3000) -> str:
    """Prepare a concise narration text suitable for TTS."""
    sections = split_text_into_sections(md_text)

    # Build a flowing narration script
    narration_parts = []

    for sec in sections:
        title = sec["title"]
        text = sec["text"]

        # Truncate very long sections
        if len(text) > 800:
            text = text[:800] + "銆備互涓嬫槸璇︾粏鍐呭銆?

        narration_parts.append(f"鎺ヤ笅鏉ユ槸{title}閮ㄥ垎銆倇text}")

    full_text = "銆俓n".join(narration_parts)

    # Truncate to max_chars if too long
    if len(full_text) > max_chars:
        full_text = full_text[:max_chars] + "銆備互涓婃槸鏈璁烘枃瑙ｈ鐨勫叏閮ㄥ唴瀹癸紝鎰熻阿鏀跺惉銆?

    return full_text


def main():
    parser = argparse.ArgumentParser(description="Generate audio from paper review")
    parser.add_argument("--text", required=True, help="Review markdown file path or glob")
    parser.add_argument("--output", default=".", help="Output directory")
    parser.add_argument("--engine", default="edge", choices=["edge", "openai"],
                        help="TTS engine: edge (free) or openai")
    args = parser.parse_args()

    md_files = glob.glob(args.text)
    if not md_files and os.path.exists(args.text):
        md_files = [args.text]

    if not md_files:
        print("ERROR: No review markdown files found", file=sys.stderr)
        sys.exit(1)

    os.makedirs(args.output, exist_ok=True)

    for md_path in md_files:
        md_path = os.path.abspath(md_path)
        with open(md_path, "r", encoding="utf-8") as f:
            md_text = f.read()

        base = os.path.splitext(os.path.basename(md_path))[0].replace("_review", "")
        audio_path = os.path.join(args.output, f"{base}_audio.mp3")

        print(f"Generating audio narration...")
        narration = prepare_narration_text(md_text)
        print(f"Narration length: {len(narration)} chars")

        success = False

        # Try specified engine first
        if args.engine == "edge":
            success = generate_audio_edge_tts(narration, audio_path)
        else:
            success = generate_audio_openai_tts(narration, audio_path)

        # Fallback to other engine
        if not success:
            print("Primary TTS failed, trying fallback...")
            if args.engine == "edge":
                success = generate_audio_openai_tts(narration, audio_path)
            else:
                success = generate_audio_edge_tts(narration, audio_path)

        if success:
            size_mb = os.path.getsize(audio_path) / 1048576
            print(f"AUDIO_SAVED: {audio_path} ({size_mb:.1f} MB)")
            print(f"AUDIO_PATH: {audio_path}")
        else:
            print(f"WARNING: All TTS methods failed for {base}", file=sys.stderr)
            # Create empty marker file
            with open(os.path.join(args.output, f"{base}_audio_failed.txt"), "w") as f:
                f.write("Audio generation failed. Please check TTS dependencies.\n")

if __name__ == "__main__":
    main()
