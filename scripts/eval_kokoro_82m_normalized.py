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
        "norm_zh_readable",
        "z",
        "zf_xiaobei",
        "我正在使用 A I Coder 测试 T T S 模型科科罗八十二 M，它需要同时处理中文、英文单词、数字二零二六，以及标点停顿。",
    ),
    (
        "norm_product_cn",
        "z",
        "zf_xiaobei",
        "我正在使用智能编程助手测试语音合成模型科科罗八十二 M，它需要同时处理中文、英文单词、数字二零二六，以及标点停顿。",
    ),
    (
        "norm_en_spelled",
        "z",
        "zf_xiaobei",
        "我正在使用 A I Coder 测试 T T S 模型 Kokoro eighty two M，它需要同时处理中文、English words、数字 two zero two six，以及标点停顿。",
    ),
    (
        "norm_no_brand",
        "z",
        "zf_xiaobei",
        "我正在使用 A I 编程助手测试语音合成模型，它需要同时处理中文、英文单词、数字二零二六，以及标点停顿。",
    ),
]


def main() -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    pipe = KPipeline(lang_code="z", repo_id="hexgrad/Kokoro-82M")
    rows = []
    for name, lang, voice, text in CASES:
        t0 = time.perf_counter()
        chunks = [result.audio for result in pipe(text, voice=voice, speed=1.0)]
        audio = np.concatenate(chunks) if chunks else np.array([], dtype=np.float32)
        elapsed = time.perf_counter() - t0
        path = OUT_DIR / f"{name}_{voice}.wav"
        sf.write(path, audio, SAMPLE_RATE)
        duration = len(audio) / SAMPLE_RATE if len(audio) else 0.0
        peak = float(np.max(np.abs(audio))) if len(audio) else 0.0
        rms = float(np.sqrt(np.mean(audio**2))) if len(audio) else 0.0
        rows.append((name, path.name, f"{duration:.3f}", f"{elapsed:.3f}", f"{duration / elapsed:.3f}", f"{peak:.4f}", f"{rms:.4f}", text))
        print(f"saved {path.name} duration={duration:.3f}s generation={elapsed:.3f}s speed={duration / elapsed:.3f}x peak={peak:.4f} rms={rms:.4f}")

    with (OUT_DIR / "normalization_summary.tsv").open("w", encoding="utf-8", newline="") as f:
        f.write("case\tfile\tduration_s\tgeneration_s\tspeed_x\tpeak\trms\ttext\n")
        for row in rows:
            f.write("\t".join(row) + "\n")


if __name__ == "__main__":
    main()
