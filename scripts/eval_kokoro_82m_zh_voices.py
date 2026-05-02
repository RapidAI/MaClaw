from __future__ import annotations

from pathlib import Path
import time

import numpy as np
import soundfile as sf
from kokoro import KPipeline

OUT_DIR = Path(__file__).resolve().parents[1] / "tts_eval" / "kokoro_82m"
SAMPLE_RATE = 24000
TEXT = "我正在使用智能代码助手，测试科科罗八千二百万参数的语音合成模型。它需要同时处理中文、英文词组、数字二零二六，以及标点停顿。"
VOICES = [
    "zf_xiaobei",
    "zf_xiaoni",
    "zf_xiaoxiao",
    "zf_xiaoyi",
    "zm_yunjian",
    "zm_yunxi",
    "zm_yunxia",
    "zm_yunyang",
]


def main() -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    pipe = KPipeline(lang_code="z", repo_id="hexgrad/Kokoro-82M")
    rows = []
    for voice in VOICES:
        t0 = time.perf_counter()
        chunks = [r.audio for r in pipe(TEXT, voice=voice, speed=1.0)]
        audio = np.concatenate(chunks) if chunks else np.array([], dtype=np.float32)
        elapsed = time.perf_counter() - t0
        path = OUT_DIR / f"zh_voice_{voice}.wav"
        sf.write(path, audio, SAMPLE_RATE)
        duration = len(audio) / SAMPLE_RATE if len(audio) else 0.0
        peak = float(np.max(np.abs(audio))) if len(audio) else 0.0
        rms = float(np.sqrt(np.mean(audio**2))) if len(audio) else 0.0
        print(f"saved {path.name} duration={duration:.3f}s generation={elapsed:.3f}s speed={duration / elapsed:.3f}x peak={peak:.4f} rms={rms:.4f}")
        rows.append((voice, path.name, f"{duration:.3f}", f"{elapsed:.3f}", f"{duration / elapsed:.3f}", f"{peak:.4f}", f"{rms:.4f}", TEXT))
    with (OUT_DIR / "zh_voice_summary.tsv").open("w", encoding="utf-8", newline="") as f:
        f.write("voice\tfile\tduration_s\tgeneration_s\tspeed_x\tpeak\trms\ttext\n")
        for row in rows:
            f.write("\t".join(row) + "\n")


if __name__ == "__main__":
    main()
