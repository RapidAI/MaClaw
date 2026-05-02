from __future__ import annotations

from pathlib import Path
import time

import numpy as np
import soundfile as sf
from kokoro import KPipeline

OUT_DIR = Path(__file__).resolve().parents[1] / "tts_eval" / "kokoro_82m"
SAMPLE_RATE = 24000
ZH_TEXT = "我正在使用智能代码助手，测试科科罗八千二百万参数的语音合成模型。它需要同时处理中文、英文词组、数字二零二六，以及标点停顿。"
EN_TEXT = "Hello, this is a quick test of Kokoro eighty two million. The voice should sound clear, stable, and natural."
CASES = [
    ("requested_zh_yunxi", "z", "zm_yunxi", ZH_TEXT),
    ("requested_zh_yunyang", "z", "zm_yunyang", ZH_TEXT),
    ("requested_en_xiaoxiao", "a", "zf_xiaoxiao", EN_TEXT),
    ("requested_en_xiaoyi", "a", "zf_xiaoyi", EN_TEXT),
]


def main() -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    pipelines: dict[str, KPipeline] = {}
    rows = []
    for name, lang, voice, text in CASES:
        try:
            if lang not in pipelines:
                pipelines[lang] = KPipeline(lang_code=lang, repo_id="hexgrad/Kokoro-82M")
            t0 = time.perf_counter()
            chunks = [r.audio for r in pipelines[lang](text, voice=voice, speed=1.0)]
            audio = np.concatenate(chunks) if chunks else np.array([], dtype=np.float32)
            elapsed = time.perf_counter() - t0
            path = OUT_DIR / f"{name}.wav"
            sf.write(path, audio, SAMPLE_RATE)
            duration = len(audio) / SAMPLE_RATE if len(audio) else 0.0
            peak = float(np.max(np.abs(audio))) if len(audio) else 0.0
            rms = float(np.sqrt(np.mean(audio**2))) if len(audio) else 0.0
            status = "ok"
            detail = f"duration={duration:.3f}s generation={elapsed:.3f}s speed={duration / elapsed:.3f}x peak={peak:.4f} rms={rms:.4f}"
            print(f"saved {path.name} {detail}")
            rows.append((name, lang, voice, path.name, status, detail, text))
        except Exception as exc:
            print(f"failed {name} {lang}/{voice}: {exc}")
            rows.append((name, lang, voice, "", "failed", repr(exc), text))
    with (OUT_DIR / "requested_voice_summary.tsv").open("w", encoding="utf-8", newline="") as f:
        f.write("case\tlang\tvoice\tfile\tstatus\tdetail\ttext\n")
        for row in rows:
            f.write("\t".join(row) + "\n")


if __name__ == "__main__":
    main()
