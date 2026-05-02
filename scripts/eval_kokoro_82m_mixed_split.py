from __future__ import annotations

from pathlib import Path
import re
import time

import numpy as np
import soundfile as sf
from kokoro import KPipeline

OUT_DIR = Path(__file__).resolve().parents[1] / "tts_eval" / "kokoro_82m"
SAMPLE_RATE = 24000
TEXT = "我正在使用 AI Coder 测试 TTS 模型 Kokoro-82M，它需要同时处理中文、English words、数字二零二六，以及标点停顿。"

# CJK chunks go through Mandarin frontend; latin/digit chunks go through English frontend.
TOKEN_RE = re.compile(r"[\u4e00-\u9fff]+|[A-Za-z0-9][A-Za-z0-9 ._+\-/]*|[^\u4e00-\u9fffA-Za-z0-9]+")


def split_mixed_text(text: str):
    chunks = []
    for token in TOKEN_RE.findall(text):
        if not token.strip():
            continue
        if re.search(r"[\u4e00-\u9fff]", token):
            chunks.append(("z", token, "zf_xiaobei"))
        elif re.search(r"[A-Za-z0-9]", token):
            chunks.append(("a", token.strip(), "af_heart"))
        else:
            # Attach punctuation to the previous chunk where possible.
            if chunks:
                lang, prev, voice = chunks[-1]
                chunks[-1] = (lang, prev + token, voice)
    return chunks


def synth_one(pipeline: KPipeline, text: str, voice: str):
    audio_parts = [result.audio for result in pipeline(text, voice=voice, speed=1.0)]
    return np.concatenate(audio_parts) if audio_parts else np.array([], dtype=np.float32)


def with_short_fade(audio: np.ndarray, ms: int = 8):
    n = min(len(audio) // 2, int(SAMPLE_RATE * ms / 1000))
    if n <= 0:
        return audio
    faded = audio.copy()
    ramp = np.linspace(0.0, 1.0, n, dtype=np.float32)
    faded[:n] *= ramp
    faded[-n:] *= ramp[::-1]
    return faded


def main() -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    pipelines = {
        "z": KPipeline(lang_code="z", repo_id="hexgrad/Kokoro-82M"),
        "a": KPipeline(lang_code="a", repo_id="hexgrad/Kokoro-82M"),
    }
    chunks = split_mixed_text(TEXT)
    print("chunks:")
    for lang, chunk, voice in chunks:
        print(f"  {lang}/{voice}: {chunk}")

    t0 = time.perf_counter()
    audio_parts = []
    boundary_silence = np.zeros(int(SAMPLE_RATE * 0.035), dtype=np.float32)
    for lang, chunk, voice in chunks:
        audio = synth_one(pipelines[lang], chunk, voice)
        audio_parts.append(with_short_fade(audio, ms=5))
        audio_parts.append(boundary_silence)
    audio = np.concatenate(audio_parts[:-1]) if audio_parts else np.array([], dtype=np.float32)

    path = OUT_DIR / "zh_mixed_split_zh_en.wav"
    sf.write(path, audio, SAMPLE_RATE)
    elapsed = time.perf_counter() - t0
    duration = len(audio) / SAMPLE_RATE
    peak = float(np.max(np.abs(audio))) if len(audio) else 0.0
    rms = float(np.sqrt(np.mean(audio**2))) if len(audio) else 0.0
    print(f"saved {path}")
    print(f"duration={duration:.3f}s generation={elapsed:.3f}s speed={duration / elapsed:.3f}x peak={peak:.4f} rms={rms:.4f}")


if __name__ == "__main__":
    main()

