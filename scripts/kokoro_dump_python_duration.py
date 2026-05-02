from pathlib import Path
import json
import torch
from kokoro import KModel

root = Path(r"D:\workprj\aicoder\tts_eval\kokoro_go_assets")
meta = json.loads((root / "quality_zh_short_meta.json").read_text(encoding="utf-8"))
model_path = Path(r"C:\Users\ma139\.cache\huggingface\hub\models--hexgrad--Kokoro-82M\snapshots\f3ff3571791e39611d31c381e3a41a3af07b4987\kokoro-v1_0.pth")
config_path = Path(r"C:\Users\ma139\.cache\huggingface\hub\models--hexgrad--Kokoro-82M\snapshots\f3ff3571791e39611d31c381e3a41a3af07b4987\config.json")
voice_path = Path(r"C:\Users\ma139\.cache\huggingface\hub\models--hexgrad--Kokoro-82M\snapshots\f3ff3571791e39611d31c381e3a41a3af07b4987\voices") / f"{meta['voice']}.pt"

m = KModel(repo_id="hexgrad/Kokoro-82M", config=str(config_path), model=str(model_path)).eval()
pack = torch.load(voice_path, map_location="cpu", weights_only=True)
phonemes = meta["phonemes"]
ref_s = pack[len(phonemes)-1]
out = m(phonemes, ref_s, speed=1, return_output=True)
ids = [m.vocab.get(p) for p in phonemes if m.vocab.get(p) is not None]
res = {
    "phonemes": phonemes,
    "phoneme_runes": len(phonemes),
    "input_ids": [0, *ids, 0],
    "pred_dur": out.pred_dur.tolist(),
    "sum_dur": int(out.pred_dur.sum().item()),
    "audio_samples": int(out.audio.numel()),
    "audio_duration_s": float(out.audio.numel() / 24000),
}
(root / "quality_python_duration.json").write_text(json.dumps(res, ensure_ascii=False, indent=2), encoding="utf-8")
print(json.dumps({k: res[k] for k in ["phoneme_runes", "sum_dur", "audio_samples", "audio_duration_s"]}, ensure_ascii=False))

