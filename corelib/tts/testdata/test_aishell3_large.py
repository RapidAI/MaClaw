#!/usr/bin/env python3
"""Test large vits-zh-aishell3 (140MB) model."""
import os, tarfile
import sherpa_onnx, numpy as np, scipy.io.wavfile as wavfile

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "vits-zh-aishell3")

if not os.path.exists(model_dir):
    tar = os.path.join(outdir, "vits-zh-aishell3.tar.bz2")
    if not os.path.exists(tar):
        print(f"Model not downloaded yet: {tar}")
        exit(1)
    print("Extracting...")
    with tarfile.open(tar, "r:bz2") as t:
        t.extractall(outdir)

print(f"Files: {os.listdir(model_dir)}")

model_onnx = os.path.join(model_dir, "model.onnx")
lexicon = os.path.join(model_dir, "lexicon.txt")
tokens = os.path.join(model_dir, "tokens.txt")
rules = ""
for f in ["phone.fst", "rule.fst"]:
    p = os.path.join(model_dir, f)
    if os.path.exists(p):
        rules = p
        break

tts = sherpa_onnx.OfflineTts(sherpa_onnx.OfflineTtsConfig(
    model=sherpa_onnx.OfflineTtsModelConfig(
        vits=sherpa_onnx.OfflineTtsVitsModelConfig(
            model=model_onnx, tokens=tokens, lexicon=lexicon,
        ), num_threads=4,
    ),
))
print(f"Sample rate: {tts.sample_rate}")
print(f"Num speakers: {tts.num_speakers}")

for text in ["你好世界", "今天天气不错", "欢迎使用MacLaw", "我们一起来写代码吧",
             "你好，欢迎使用MacLaw。今天天气不错，我们一起来写代码吧。"]:
    audio = tts.generate(text, sid=0, speed=1.0)
    s = np.array(audio.samples)
    dur = len(s) / audio.sample_rate
    print(f"  '{text}': {dur:.2f}s, max={abs(s).max():.4f}, sr={audio.sample_rate}")
    safe = text[:8].replace(' ','_').replace('，','').replace('。','')
    fname = f"aishell3L_{safe}.wav"
    wavfile.write(os.path.join(outdir, fname), audio.sample_rate,
                  np.clip(s*32767,-32768,32767).astype(np.int16))
    print(f"  → {fname}")
