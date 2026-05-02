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
        "norm_full_cn_terms",
        "我正在使用智能代码助手，测试科科罗八千二百万参数的语音合成模型。它需要同时处理中文、英文词组、数字二零二六，以及标点停顿。",
    ),
    (
        "norm_tts_cn_brand_cn",
        "我正在使用 A I 代码助手，测试科科罗八千二百万参数的文字转语音模型。它需要同时处理中文、英文词组、数字二零二六，以及标点停顿。",
    ),
    (
        "norm_keep_ai_only",
        "我正在使用 A I 代码助手测试语音合成模型科科罗，模型规模是八千二百万参数。它需要同时处理中文、英文词组、数字二零二六，以及标点停顿。",
    ),
]


def main() -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    pipe = KPipeline(lang_code="z", repo_id="hexgrad/Kokoro-82M")
    rows = []
    for name, text in CASES:
        t0 = time.perf_counter()
        chunks = [r.audio for r in pipe(text, voice="zf_xiaobei", speed=1.0)]
        audio = np.concatenate(chunks) if chunks else np.array([], dtype=np.float32)
        elapsed = time.perf_counter() - t0
        path = OUT_DIR / f"{name}_zf_xiaobei.wav"
        sf.write(path, audio, SAMPLE_RATE)
        duration = len(audio) / SAMPLE_RATE if len(audio) else 0.0
        print(f"saved {path.name} duration={duration:.3f}s generation={elapsed:.3f}s speed={duration / elapsed:.3f}x")
        rows.append((name, path.name, f"{duration:.3f}", f"{elapsed:.3f}", text))
    with (OUT_DIR / "normalization_more_summary.tsv").open("w", encoding="utf-8", newline="") as f:
        f.write("case\tfile\tduration_s\tgeneration_s\ttext\n")
        for row in rows:
            f.write("\t".join(row) + "\n")


if __name__ == "__main__":
    main()
