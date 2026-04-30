#!/usr/bin/env python3
"""Test Piper Chinese TTS models with sherpa-onnx."""
import os, subprocess, tarfile
import sherpa_onnx
import numpy as np
import scipy.io.wavfile as wavfile

outdir = os.path.dirname(os.path.abspath(__file__))

models = [
    ("vits-piper-zh_CN-huayan-medium", "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-piper-zh_CN-huayan-medium.tar.bz2"),
    ("vits-piper-zh_CN-xiao_ya-medium", "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-piper-zh_CN-xiao_ya-medium.tar.bz2"),
]

tests = [
    "你好世界",
    "今天天气不错",
    "欢迎使用MacLaw",
    "我们一起来写代码吧",
]

for model_name, url in models:
    model_dir = os.path.join(outdir, model_name)
    if not os.path.exists(model_dir):
        tar_path = os.path.join(outdir, f"{model_name}.tar.bz2")
        if not os.path.exists(tar_path):
            print(f"Downloading {model_name}...")
            subprocess.run(["curl", "-L", "-o", tar_path, url], check=True)
        print(f"Extracting {model_name}...")
        with tarfile.open(tar_path, "r:bz2") as tar:
            tar.extractall(outdir)

    # Find model files
    model_onnx = os.path.join(model_dir, "model.onnx")
    tokens = os.path.join(model_dir, "tokens.txt")
    data_dir = os.path.join(model_dir, "espeak-ng-data")

    if not os.path.exists(data_dir):
        # Download espeak-ng-data if needed
        espeak_tar = os.path.join(outdir, "espeak-ng-data.tar.bz2")
        if not os.path.exists(espeak_tar):
            print("Downloading espeak-ng-data...")
            subprocess.run(["curl", "-L", "-o", espeak_tar,
                "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/espeak-ng-data.tar.bz2"],
                check=True)
        if not os.path.exists(os.path.join(outdir, "espeak-ng-data")):
            with tarfile.open(espeak_tar, "r:bz2") as tar:
                tar.extractall(outdir)
        data_dir = os.path.join(outdir, "espeak-ng-data")

    # Check for lexicon
    lexicon = os.path.join(model_dir, "lexicon.txt")
    if not os.path.exists(lexicon):
        # Try downloading lexicon for Chinese piper models
        lex_url = "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/lexicon-zh-g2pw.txt"
        lex_path = os.path.join(model_dir, "lexicon-zh-g2pw.txt")
        if not os.path.exists(lex_path):
            print(f"Downloading lexicon...")
            subprocess.run(["curl", "-L", "-o", lex_path, lex_url])
        lexicon = lex_path

    print(f"\n=== {model_name} ===")
    print(f"  model: {model_onnx}")
    print(f"  tokens: {tokens}")
    print(f"  data_dir: {data_dir}")

    try:
        tts_config = sherpa_onnx.OfflineTtsConfig(
            model=sherpa_onnx.OfflineTtsModelConfig(
                vits=sherpa_onnx.OfflineTtsVitsModelConfig(
                    model=model_onnx,
                    tokens=tokens,
                    data_dir=data_dir,
                    lexicon=lexicon if os.path.exists(lexicon) else "",
                ),
                num_threads=4,
            ),
        )
        tts = sherpa_onnx.OfflineTts(tts_config)
        print(f"  Sample rate: {tts.sample_rate}")

        for text in tests:
            audio = tts.generate(text, sid=0, speed=1.0)
            samples = np.array(audio.samples)
            dur = len(samples) / audio.sample_rate
            print(f"  '{text}': {len(samples)} samples ({dur:.2f}s), max={abs(samples).max():.4f}")

            fname = f"{model_name}_{text[:4]}.wav"
            fpath = os.path.join(outdir, fname)
            samples_int16 = np.clip(samples * 32767, -32768, 32767).astype(np.int16)
            wavfile.write(fpath, audio.sample_rate, samples_int16)

    except Exception as e:
        print(f"  ERROR: {e}")
        import traceback
        traceback.print_exc()

print("\nDone!")
