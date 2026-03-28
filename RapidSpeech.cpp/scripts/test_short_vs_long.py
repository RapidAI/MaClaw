#!/usr/bin/env python3
"""Quick test: compare greedy vs strategy on the jenny wav (longest available)."""
import numpy as np, torch, wave, sys, os
sys.path.insert(0, os.path.dirname(__file__))
os.chdir(os.path.join(os.path.dirname(__file__), ".."))

from test_decode_strategies import encode, decode, tokens_to_text, compression_ratio, load_wav

# Test with en_male_guy files (concatenated for longer audio)
files = ["test/real_speech/en_male_guy_0.wav", "test/real_speech/en_male_guy_1.wav"]
pcm_parts = []
for f in files:
    if os.path.exists(f):
        pcm_parts.append(load_wav(f))

if not pcm_parts:
    print("No test files found"); sys.exit(1)

# Single file
pcm_single = pcm_parts[0]
print(f"=== Single file: {len(pcm_single)/16000:.1f}s ===")
enc = encode(pcm_single)
toks_g, lp_g = decode(enc, 200, False)
toks_s, lp_s = decode(enc, 200, True)
print(f"  GREEDY:   {len(toks_g)-1} tokens, cr={compression_ratio(toks_g):.2f}, text=\"{tokens_to_text(toks_g)[:100]}\"")
print(f"  STRATEGY: {len(toks_s)-1} tokens, cr={compression_ratio(toks_s):.2f}, text=\"{tokens_to_text(toks_s)[:100]}\"")

# Concatenated (longer)
if len(pcm_parts) > 1:
    pcm_long = np.concatenate(pcm_parts)
    print(f"\n=== Concatenated: {len(pcm_long)/16000:.1f}s ===")
    enc = encode(pcm_long)
    toks_g, lp_g = decode(enc, 300, False)
    toks_s, lp_s = decode(enc, 300, True)
    print(f"  GREEDY:   {len(toks_g)-1} tokens, cr={compression_ratio(toks_g):.2f}, text=\"{tokens_to_text(toks_g)[:100]}\"")
    print(f"  STRATEGY: {len(toks_s)-1} tokens, cr={compression_ratio(toks_s):.2f}, text=\"{tokens_to_text(toks_s)[:100]}\"")
