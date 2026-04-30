#!/usr/bin/env python3
"""
Generate real audio using MeloTTS official model.
Workaround: build SynthesizerTrn from state_dict and run infer().
"""
import os, sys, json, math
import numpy as np
import torch
import torch.nn as nn
import torch.nn.functional as F

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")

# Add MeloTTS to path if installed
try:
    from melo import models as melo_models
    from melo import modules as melo_modules
    from melo import attentions as melo_attentions
    from melo import commons as melo_commons
    HAS_MELO = True
    print("MeloTTS modules found")
except ImportError:
    HAS_MELO = False
    print("MeloTTS modules not found, using manual inference")

def main():
    config_path = os.path.join(model_dir, "config.json")
    ckpt_path = os.path.join(model_dir, "checkpoint.pth")

    with open(config_path, encoding='utf-8') as f:
        config = json.load(f)

    ckpt = torch.load(ckpt_path, map_location="cpu", weights_only=False)
    sd = ckpt.get("model", ckpt)

    if HAS_MELO:
        generate_with_melo(config, sd)
    else:
        print("Cannot generate real audio without melo package.")
        print("Install: pip install melo-tts")
        print("If torchaudio fails, try: pip install torchaudio --index-url https://download.pytorch.org/whl/cpu")

def generate_with_melo(config, sd):
    mc = config["model"]
    dc = config["data"]
    symbols = config["symbols"]

    # Build model
    model = melo_models.SynthesizerTrn(
        n_vocab=len(symbols),
        spec_channels=dc.get("n_fft", 2048) // 2 + 1,  # 1025 for n_fft=2048
        segment_size=config["train"]["segment_size"] // dc["hop_length"],
        inter_channels=mc["inter_channels"],
        hidden_channels=mc["hidden_channels"],
        filter_channels=mc["filter_channels"],
        n_heads=mc["n_heads"],
        n_layers=mc["n_layers"],
        kernel_size=mc["kernel_size"],
        p_dropout=mc["p_dropout"],
        resblock=mc["resblock"],
        resblock_kernel_sizes=mc["resblock_kernel_sizes"],
        resblock_dilation_sizes=mc["resblock_dilation_sizes"],
        upsample_rates=mc["upsample_rates"],
        upsample_initial_channel=mc["upsample_initial_channel"],
        upsample_kernel_sizes=mc["upsample_kernel_sizes"],
        n_speakers=dc["n_speakers"],
        gin_channels=mc.get("gin_channels", 256),
        use_sdp=mc.get("use_sdp", True),
        n_flow_layer=mc.get("n_flow_layer", 4),
        n_layers_trans_flow=mc.get("n_layers_trans_flow", 3),
        flow_share_parameter=mc.get("flow_share_parameter", False),
        use_transformer_flow=mc.get("use_transformer_flow", True),
        num_languages=config.get("num_languages", 8),
        num_tones=config.get("num_tones", 16),
    )

    # Load weights
    model.load_state_dict(sd, strict=False)
    model.eval()

    # Phone IDs for "Hello" (same as Go)
    phone_ids = [0, 49, 0, 127, 0, 70, 0, 80, 0]
    T = len(phone_ids)

    x = torch.LongTensor([phone_ids])
    x_lengths = torch.LongTensor([T])
    sid = torch.LongTensor([0])
    tone = torch.LongTensor([[0] * T])
    language = torch.LongTensor([[2] * T])  # EN=2
    bert = torch.zeros(1, 1024, T)
    ja_bert = torch.zeros(1, 768, T)

    with torch.no_grad():
        # Standard inference with noise
        audio, attn, y_mask, (z, z_p, m_p, logs_p) = model.infer(
            x, x_lengths, sid, tone, language, bert, ja_bert,
            noise_scale=0.667,
            length_scale=1.0,
            noise_scale_w=0.8,
            sdp_ratio=0.0,
        )

    audio_np = audio.squeeze().cpu().numpy()
    print(f"Audio: {len(audio_np)} samples, mean={audio_np.mean():.4f}, "
          f"std={audio_np.std():.4f}, max={abs(audio_np).max():.4f}")

    # Dump intermediates
    def dump(name, t):
        d = t.squeeze(0).cpu().numpy().astype(np.float32)
        d.tofile(os.path.join(outdir, f"{name}.bin"))
        print(f"  {name}: shape={list(d.shape)}, mean={d.mean():.4f}, std={d.std():.4f}")

    dump("ref_real_z_p", z_p)
    dump("ref_real_z", z)
    dump("ref_real_m_p", m_p)
    dump("ref_real_logs_p", logs_p)
    dump("ref_real_audio", audio)

    # Save WAV
    import scipy.io.wavfile as wavfile
    sr = dc.get("sampling_rate", 22050)
    audio_int16 = np.clip(audio_np * 32767, -32768, 32767).astype(np.int16)
    wav_path = os.path.join(outdir, "ref_real_hello.wav")
    wavfile.write(wav_path, sr, audio_int16)
    print(f"\nSaved: {wav_path} ({sr} Hz)")

    # Also generate with noise_scale=0 for comparison
    with torch.no_grad():
        audio0, _, _, (z0, z_p0, _, _) = model.infer(
            x, x_lengths, sid, tone, language, bert, ja_bert,
            noise_scale=0.0,
            length_scale=1.0,
            noise_scale_w=0.0,
            sdp_ratio=0.0,
        )
    audio0_np = audio0.squeeze().cpu().numpy()
    print(f"\nnoise_scale=0: {len(audio0_np)} samples, mean={audio0_np.mean():.4f}, "
          f"std={audio0_np.std():.4f}, max={abs(audio0_np).max():.4f}")
    dump("ref_real_z_p_ns0", z_p0)
    dump("ref_real_z_ns0", z0)
    dump("ref_real_audio_ns0", audio0)

    audio0_int16 = np.clip(audio0_np * 32767, -32768, 32767).astype(np.int16)
    wavfile.write(os.path.join(outdir, "ref_real_hello_ns0.wav"), sr, audio0_int16)
    print(f"Saved: ref_real_hello_ns0.wav")

if __name__ == "__main__":
    main()
