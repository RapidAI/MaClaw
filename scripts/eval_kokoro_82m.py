from __future__ import annotations

from pathlib import Path
import time

import numpy as np
import soundfile as sf
from kokoro import KPipeline


OUT_DIR = Path(__file__).resolve().parents[1] / "tts_eval" / "kokoro_82m"
SAMPLE_RATE = 24000

CASES = [
    (
        "en_short",
        "a",
        "af_heart",
        "Hello, this is a quick test of Kokoro eighty two million. The voice should sound clear, stable, and natural.",
    ),
    (
        "en_long",
        "a",
        "af_heart",
        "Today we are testing text to speech quality in a realistic product scenario. Please check pronunciation, rhythm, pauses, and whether the ending fades naturally.",
    ),
    (
        "zh_short",
        "z",
        "zf_xiaobei",
        "你好，这是 Kokoro 八千二百万参数模型的中文语音测试。请注意音质是否清晰，停顿是否自然。",
    ),
    (
        "zh_long",
        "z",
        "zf_xiaobei",
        "今天我们测试中英文语音生成质量，包括普通话发音、数字读法、语气连贯性，以及较长句子里的节奏控制。",
    ),
    (
        "zh_mixed",
        "z",
        "zf_xiaobei",
        "我正在使用 AI Coder 测试 TTS 模型 Kokoro-82M，它需要同时处理中文、English words、数字二零二六，以及标点停顿。",
    ),
]


def synthesize_case(pipeline: KPipeline, name: str, voice: str, text: str):
    t0 = time.perf_counter()
    chunks = []
    phonemes = []
    for result in pipeline(text, voice=voice, speed=1.0):
        chunks.append(result.audio)
        phonemes.append(getattr(result, "phonemes", "") or "")
    elapsed = time.perf_counter() - t0
    audio = np.concatenate(chunks) if chunks else np.array([], dtype=np.float32)

    path = OUT_DIR / f"{name}_{voice}.wav"
    sf.write(path, audio, SAMPLE_RATE)
    duration = len(audio) / SAMPLE_RATE if len(audio) else 0.0
    peak = float(np.max(np.abs(audio))) if len(audio) else 0.0
    rms = float(np.sqrt(np.mean(audio**2))) if len(audio) else 0.0
    speed_x = duration / elapsed if elapsed else 0.0
    return {
        "case": name,
        "voice": voice,
        "file": path.name,
        "duration_s": f"{duration:.3f}",
        "generation_s": f"{elapsed:.3f}",
        "speed_x": f"{speed_x:.3f}",
        "peak": f"{peak:.4f}",
        "rms": f"{rms:.4f}",
        "text": text,
        "phonemes_head": " ".join(phonemes)[:300],
    }


def main() -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    pipelines: dict[str, KPipeline] = {}
    rows = []
    for name, lang, voice, text in CASES:
        if lang not in pipelines:
            t0 = time.perf_counter()
            pipelines[lang] = KPipeline(lang_code=lang, repo_id="hexgrad/Kokoro-82M")
            print(f"pipeline {lang} ready in {time.perf_counter() - t0:.2f}s", flush=True)
        row = {"lang": lang, **synthesize_case(pipelines[lang], name, voice, text)}
        rows.append(row)
        print(
            f"saved {row['file']} dur={row['duration_s']}s gen={row['generation_s']}s "
            f"speed={row['speed_x']}x peak={row['peak']} rms={row['rms']}",
            flush=True,
        )

    fields = [
        "case",
        "lang",
        "voice",
        "file",
        "duration_s",
        "generation_s",
        "speed_x",
        "peak",
        "rms",
        "text",
        "phonemes_head",
    ]
    with (OUT_DIR / "summary.tsv").open("w", encoding="utf-8", newline="") as f:
        f.write("\t".join(fields) + "\n")
        for row in rows:
            f.write("\t".join(row[field] for field in fields) + "\n")
    print(f"summary {OUT_DIR / 'summary.tsv'}")


if __name__ == "__main__":
    main()
