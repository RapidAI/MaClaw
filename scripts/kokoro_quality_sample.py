from pathlib import Path
import json
import time

import numpy as np
import soundfile as sf
from kokoro import KPipeline

out = Path(r"D:\workprj\aicoder\tts_eval\kokoro_go_assets")
out.mkdir(parents=True, exist_ok=True)
text = "你好，测试语音。"
voice = "zm_yunxi"

pipe_quiet = KPipeline(lang_code="z", repo_id="hexgrad/Kokoro-82M", model=False)
items = list(pipe_quiet(text, voice=voice))
phonemes = ""
for item in items:
    if hasattr(item, "phonemes"):
        phonemes += item.phonemes or ""
    else:
        phonemes += item[1]

pipe = KPipeline(lang_code="z", repo_id="hexgrad/Kokoro-82M")
t0 = time.perf_counter()
chunks = [r.audio for r in pipe(text, voice=voice, speed=1.0)]
audio = np.concatenate(chunks) if chunks else np.array([], dtype=np.float32)
sf.write(out / "quality_py_zh_short.wav", audio, 24000)
meta = {
    "text": text,
    "voice": voice,
    "phonemes": phonemes,
    "python_file": "quality_py_zh_short.wav",
    "python_duration_s": len(audio) / 24000,
    "python_generation_s": time.perf_counter() - t0,
    "python_peak": float(np.max(np.abs(audio))) if len(audio) else 0,
    "python_rms": float(np.sqrt(np.mean(audio**2))) if len(audio) else 0,
}
(out / "quality_zh_short_meta.json").write_text(json.dumps(meta, ensure_ascii=False, indent=2), encoding="utf-8")
print(json.dumps(meta, ensure_ascii=False, indent=2))
