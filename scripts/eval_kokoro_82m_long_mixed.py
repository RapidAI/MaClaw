from __future__ import annotations

from pathlib import Path
import re
import time

import numpy as np
import soundfile as sf
from kokoro import KPipeline

OUT_DIR = Path(__file__).resolve().parents[1] / "tts_eval" / "kokoro_82m"
SAMPLE_RATE = 24000

ZH_VOICES = ["zm_yunxi", "zm_yunyang"]
EN_VOICES = ["zf_xiaoxiao", "zf_xiaoyi"]

CASES = [
    (
        "long_mixed_product_yunxi_xiaoxiao",
        "zm_yunxi",
        "zf_xiaoxiao",
        "今天我们做一次更接近真实产品场景的语音评估：用户先用中文描述需求，然后突然插入 AI workflow、real time transcription 和 cloud sync 这些英文词组。系统需要保持普通话发音自然，英文部分也不要被念成奇怪的拼音，同时在逗号、冒号和句号之间给出稳定的停顿。By the end of this test, we want the whole paragraph to sound coherent, calm, and useful.",
    ),
    (
        "long_mixed_meeting_yunyang_xiaoyi",
        "zm_yunyang",
        "zf_xiaoyi",
        "下午的产品评审会里，项目经理说：这个版本的 TTS engine 已经支持 mixed language prompts，但我们还需要听一下长句的呼吸感、段落衔接和数字读法。For example, release two point six should not sound rushed, and twenty twenty six should remain easy to understand. 如果中英文切换太硬，用户会立刻感觉这是拼接出来的，所以这条样例专门用来检查过渡是否舒服。",
    ),
    (
        "long_mixed_story_yunxi_xiaoyi",
        "zm_yunxi",
        "zf_xiaoyi",
        "凌晨一点，开发者终于把日志里的 warning 清理干净，屏幕上只剩下一行 message: build completed successfully. 她松了一口气，又让语音助手读出最后的测试摘要：latency is lower, memory usage is stable, and the generated voice keeps a friendly tone. 这段话不追求夸张的情绪，只看它能不能在长时间朗读里保持清晰、平稳和不刺耳。",
    ),
    (
        "long_mixed_numbers_yunyang_xiaoxiao",
        "zm_yunyang",
        "zf_xiaoxiao",
        "请记录这组混合信息：订单编号 AICoder-2026-0502，服务区域是 Shanghai and San Francisco，目标延迟低于 one hundred milliseconds，音频采样率为 twenty four kilohertz。接下来系统会连续播报中文说明、English labels、版本号 v1.0.3，以及几个较长的从句，用来观察模型是否会吞字、断句过早，或者在结尾出现不自然的能量衰减。",
    ),
]

TOKEN_RE = re.compile(r"[\u4e00-\u9fff]+|[A-Za-z0-9][A-Za-z0-9 ._+\-/]*|[^\u4e00-\u9fffA-Za-z0-9]+")


def split_mixed_text(text: str, zh_voice: str, en_voice: str):
    chunks = []
    for token in TOKEN_RE.findall(text):
        if not token.strip():
            continue
        if re.search(r"[\u4e00-\u9fff]", token):
            chunks.append(("z", token, zh_voice))
        elif re.search(r"[A-Za-z0-9]", token):
            chunks.append(("a", token.strip(), en_voice))
        elif chunks:
            lang, prev, voice = chunks[-1]
            chunks[-1] = (lang, prev + token, voice)
    return chunks


def synth_one(pipeline: KPipeline, text: str, voice: str) -> np.ndarray:
    parts = [result.audio for result in pipeline(text, voice=voice, speed=1.0)]
    return np.concatenate(parts) if parts else np.array([], dtype=np.float32)


def fade_edges(audio: np.ndarray, ms: int = 6) -> np.ndarray:
    n = min(len(audio) // 2, int(SAMPLE_RATE * ms / 1000))
    if n <= 0:
        return audio
    out = audio.copy()
    ramp = np.linspace(0.0, 1.0, n, dtype=np.float32)
    out[:n] *= ramp
    out[-n:] *= ramp[::-1]
    return out


def audio_stats(audio: np.ndarray) -> tuple[float, float, float]:
    duration = len(audio) / SAMPLE_RATE if len(audio) else 0.0
    peak = float(np.max(np.abs(audio))) if len(audio) else 0.0
    rms = float(np.sqrt(np.mean(audio**2))) if len(audio) else 0.0
    return duration, peak, rms


def main() -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    pipelines = {
        "z": KPipeline(lang_code="z", repo_id="hexgrad/Kokoro-82M"),
        "a": KPipeline(lang_code="a", repo_id="hexgrad/Kokoro-82M"),
    }
    silence = np.zeros(int(SAMPLE_RATE * 0.045), dtype=np.float32)
    rows = []
    for name, zh_voice, en_voice, text in CASES:
        chunks = split_mixed_text(text, zh_voice, en_voice)
        print(f"case={name}")
        for lang, chunk, voice in chunks:
            print(f"  {lang}/{voice}: {chunk}")
        t0 = time.perf_counter()
        audio_parts = []
        for lang, chunk, voice in chunks:
            audio_parts.append(fade_edges(synth_one(pipelines[lang], chunk, voice)))
            audio_parts.append(silence)
        audio = np.concatenate(audio_parts[:-1]) if audio_parts else np.array([], dtype=np.float32)
        elapsed = time.perf_counter() - t0
        path = OUT_DIR / f"{name}.wav"
        sf.write(path, audio, SAMPLE_RATE)
        duration, peak, rms = audio_stats(audio)
        detail = f"duration={duration:.3f}s generation={elapsed:.3f}s speed={duration / elapsed:.3f}x peak={peak:.4f} rms={rms:.4f}"
        print(f"saved {path.name} {detail}")
        rows.append((name, zh_voice, en_voice, path.name, f"{duration:.3f}", f"{elapsed:.3f}", f"{peak:.4f}", f"{rms:.4f}", text))
    with (OUT_DIR / "long_mixed_summary.tsv").open("w", encoding="utf-8", newline="") as f:
        f.write("case\tzh_voice\ten_voice\tfile\tduration_s\tgeneration_s\tpeak\trms\ttext\n")
        for row in rows:
            f.write("\t".join(row) + "\n")


if __name__ == "__main__":
    main()
