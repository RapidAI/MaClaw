#!/usr/bin/env python3
"""
Test moonshine-base-zh Chinese recognition using HuggingFace official pipeline.

Purpose: Verify the model itself can recognize Chinese, independent of any
custom inference code. This is the ground truth test.

Requirements:
    pip install transformers>=4.48 torch soundfile numpy

Usage:
    python scripts/test_moonshine_zh_pipeline.py
"""
import os, sys, json
import numpy as np

os.chdir(os.path.join(os.path.dirname(__file__), ".."))

MODEL_DIR = "models/moonshine-base-zh"
WAV_PATH = "test/real_speech/maclaw_16k.wav"
M4A_PATH = "test/real_speech/maclaw.m4a"

# --- Check model exists ---
if not os.path.exists(os.path.join(MODEL_DIR, "model.safetensors")):
    print(f"ERROR: Model not found at {MODEL_DIR}/model.safetensors")
    print("Run download_and_test_zh.py first to download the model.")
    sys.exit(1)

# --- Load audio ---
if os.path.exists(WAV_PATH):
    import wave
    with wave.open(WAV_PATH, "rb") as wf:
        assert wf.getframerate() == 16000, f"Expected 16kHz, got {wf.getframerate()}"
        frames = wf.readframes(wf.getnframes())
        pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
    print(f"Loaded WAV: {WAV_PATH} ({len(pcm)/16000:.1f}s)")
elif os.path.exists(M4A_PATH):
    try:
        import soundfile as sf
        pcm, sr = sf.read(M4A_PATH)
        if sr != 16000:
            import librosa
            pcm = librosa.resample(pcm, orig_sr=sr, target_sr=16000)
        pcm = pcm.astype(np.float32)
        print(f"Loaded M4A via soundfile: {M4A_PATH} ({len(pcm)/16000:.1f}s)")
    except Exception as e:
        print(f"ERROR: Cannot load {M4A_PATH}: {e}")
        print("Please convert to WAV first: ffmpeg -i maclaw.m4a -ar 16000 -ac 1 maclaw_16k.wav")
        sys.exit(1)
else:
    print(f"ERROR: No audio found at {WAV_PATH} or {M4A_PATH}")
    sys.exit(1)

# --- Print model info ---
with open(os.path.join(MODEL_DIR, "config.json")) as f:
    cfg = json.load(f)
print(f"Model: {cfg.get('model_type')}, vocab={cfg.get('vocab_size')}, "
      f"hidden={cfg.get('hidden_size')}, enc={cfg.get('encoder_num_hidden_layers')}, "
      f"dec={cfg.get('decoder_num_hidden_layers')}")

# Check Chinese tokens in vocab
with open(os.path.join(MODEL_DIR, "tokenizer.json"), "r", encoding="utf-8") as f:
    tok_data = json.load(f)
vocab = tok_data.get("model", {}).get("vocab", {})
zh_count = sum(1 for t in vocab if any(0x4e00 <= ord(c) <= 0x9fff for c in t))
print(f"Chinese tokens in vocab: {zh_count}")


# --- Import transformers ---
# NOTE: We avoid AutoProcessor because it triggers torchvision import which
# conflicts with torch 2.4.x. For ASR we only need the feature extractor
# and tokenizer, not any image processing.
try:
    import torch
    from transformers import (
        AutoModelForSpeechSeq2Seq,
        Wav2Vec2FeatureExtractor,
        AutoTokenizer,
    )
    import transformers
    print(f"\ntransformers version: {transformers.__version__}")
    print(f"torch version: {torch.__version__}")
except ImportError as e:
    print(f"ERROR: {e}")
    print("Install: pip install transformers>=4.48 torch")
    sys.exit(1)

# ============================================================
# Method 1: AutoModel + AutoProcessor (manual inference)
# ============================================================
print("\n" + "=" * 60)
print("  Method 1: AutoModel + AutoProcessor")
print("=" * 60)

try:
    model = AutoModelForSpeechSeq2Seq.from_pretrained(MODEL_DIR)
    feature_extractor = Wav2Vec2FeatureExtractor.from_pretrained(MODEL_DIR)
    tokenizer = AutoTokenizer.from_pretrained(MODEL_DIR)
    model.eval()

    inputs = feature_extractor(pcm, return_tensors="pt", sampling_rate=16000)
    print(f"Input shape: {inputs.input_values.shape if hasattr(inputs, 'input_values') else 'unknown'}")

    with torch.no_grad():
        # Default generate
        gen = model.generate(**inputs, max_length=200)
        text = tokenizer.decode(gen[0], skip_special_tokens=True)
        print(f'  Default generate: "{text}"')

        # With max_new_tokens
        gen2 = model.generate(**inputs, max_new_tokens=200)
        text2 = tokenizer.decode(gen2[0], skip_special_tokens=True)
        print(f'  max_new_tokens=200: "{text2}"')

        # Greedy (no special params)
        gen3 = model.generate(**inputs)
        text3 = tokenizer.decode(gen3[0], skip_special_tokens=True)
        print(f'  Greedy default: "{text3}"')

    # Check if output contains Chinese characters
    def has_chinese(s):
        return any(0x4e00 <= ord(c) <= 0x9fff for c in s)

    for label, t in [("Default", text), ("max_new_tokens", text2), ("Greedy", text3)]:
        is_zh = has_chinese(t)
        print(f"  {label} contains Chinese: {is_zh}")

except Exception as e:
    print(f"  ERROR: {e}")
    import traceback; traceback.print_exc()

# ============================================================
# Method 2: pipeline("automatic-speech-recognition")
# ============================================================
print("\n" + "=" * 60)
print("  Method 2: pipeline (automatic-speech-recognition)")
print("=" * 60)

try:
    from transformers import pipeline as hf_pipeline

    # Build pipeline manually to avoid AutoProcessor triggering torchvision
    asr = hf_pipeline(
        "automatic-speech-recognition",
        model=model,
        feature_extractor=feature_extractor,
        tokenizer=tokenizer,
        device="cpu",
    )

    result = asr(pcm, generate_kwargs={"max_new_tokens": 200})
    text_pipe = result["text"]
    print(f'  Pipeline result: "{text_pipe}"')
    print(f"  Contains Chinese: {has_chinese(text_pipe)}")

except Exception as e:
    print(f"  ERROR: {e}")
    import traceback; traceback.print_exc()

# ============================================================
# Summary
# ============================================================
print("\n" + "=" * 60)
print("  Summary")
print("=" * 60)
print("If all outputs are English or garbled, the model itself")
print("does not recognize Chinese on this audio.")
print("If outputs contain Chinese text, the model works for Chinese.")
print("Done.")
