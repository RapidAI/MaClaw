from __future__ import annotations

import argparse
import json
import struct
from pathlib import Path
from typing import Iterable, Tuple

import torch

MAGIC = b"KOROTNSR"
VERSION = 1
VERSION_TYPED = 2
DTYPE_F32 = 0
DTYPE_Q8_ROWWISE = 1


def flatten_state(prefix: str, obj) -> Iterable[Tuple[str, torch.Tensor]]:
    if isinstance(obj, torch.Tensor):
        yield prefix, obj.detach().cpu().contiguous().to(torch.float32)
    elif isinstance(obj, dict):
        for key, value in obj.items():
            name = f"{prefix}.{key}" if prefix else str(key)
            yield from flatten_state(name, value)
    else:
        return


def should_quantize_mixed(name: str, tensor: torch.Tensor) -> bool:
    if tensor.numel() < 4096 or tensor.ndim < 2:
        return False
    lower = name.lower()
    excluded = (
        ".bias",
        "layernorm",
        ".norm",
        "adain",
        ".alpha",
        "embedding",
        "conv_post",
    )
    if any(part in lower for part in excluded):
        return False
    included_suffixes = (
        ".weight",
        ".weight_v",
        ".weight_ih_l0",
        ".weight_hh_l0",
        ".weight_ih_l0_reverse",
        ".weight_hh_l0_reverse",
    )
    return name.endswith(included_suffixes)


def quantize_rowwise_q8(tensor: torch.Tensor) -> tuple[torch.Tensor, torch.Tensor, int]:
    tensor = tensor.detach().cpu().contiguous().to(torch.float32)
    if tensor.ndim == 0:
        tensor = tensor.reshape(1)
    rows = int(tensor.shape[0]) if tensor.ndim > 0 else 1
    flat = tensor.reshape(rows, -1)
    max_abs = flat.abs().amax(dim=1)
    scales = torch.where(max_abs > 0, max_abs / 127.0, torch.ones_like(max_abs))
    q = torch.round(flat / scales[:, None]).clamp(-127, 127).to(torch.int8).contiguous()
    return scales.to(torch.float32).contiguous(), q, int(flat.shape[1])


def write_koro(path: Path, tensors: Iterable[Tuple[str, torch.Tensor]], mixed_q8: bool = False) -> dict:
    items = [(name, tensor) for name, tensor in tensors]
    path.parent.mkdir(parents=True, exist_ok=True)
    q8_count = 0
    q8_f32_bytes = 0
    q8_bytes = 0
    with path.open("wb") as f:
        f.write(MAGIC)
        f.write(struct.pack("<II", VERSION_TYPED if mixed_q8 else VERSION, len(items)))
        for name, tensor in items:
            name_b = name.encode("utf-8")
            if len(name_b) > 65535:
                raise ValueError(f"tensor name too long: {name}")
            tensor = tensor.detach().cpu().contiguous().to(torch.float32)
            shape = list(tensor.shape)
            if not shape:
                shape = [1]
                tensor = tensor.reshape(1)
            if len(shape) > 8:
                raise ValueError(f"tensor rank too large: {name} {shape}")
            f.write(struct.pack("<H", len(name_b)))
            f.write(name_b)
            f.write(struct.pack("<B", len(shape)))
            for dim in shape:
                f.write(struct.pack("<I", int(dim)))
            if mixed_q8 and should_quantize_mixed(name, tensor):
                scales, q, cols = quantize_rowwise_q8(tensor)
                rows = int(scales.numel())
                f.write(struct.pack("<BII", DTYPE_Q8_ROWWISE, rows, cols))
                f.write(scales.numpy().astype("<f4", copy=False).tobytes(order="C"))
                f.write(q.numpy().tobytes(order="C"))
                q8_count += 1
                q8_f32_bytes += tensor.numel() * 4
                q8_bytes += rows * 4 + tensor.numel()
            else:
                if mixed_q8:
                    f.write(struct.pack("<B", DTYPE_F32))
                f.write(tensor.numpy().astype("<f4", copy=False).tobytes(order="C"))
    return {
        "tensors": len(items),
        "q8_tensors": q8_count,
        "q8_f32_bytes": q8_f32_bytes,
        "q8_bytes": q8_bytes,
        "saved_bytes": q8_f32_bytes - q8_bytes,
    }


def main() -> None:
    ap = argparse.ArgumentParser(description="Export Kokoro PyTorch assets into pure-Go .koro tensor files.")
    ap.add_argument("--snapshot", required=True, help="Path to the Hugging Face Kokoro snapshot directory")
    ap.add_argument("--out", required=True, help="Output directory for Go-readable assets")
    ap.add_argument("--voices", default="zm_yunxi,zm_yunyang,zf_xiaoxiao,zf_xiaoyi", help="Comma-separated voice names to export")
    ap.add_argument("--mixed-q8", action="store_true", help="Write model weights as typed v2 .koro with selected large weights quantized row-wise to int8")
    args = ap.parse_args()

    snapshot = Path(args.snapshot)
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)

    config_src = snapshot / "config.json"
    config_dst = out / "config.json"
    config_dst.write_text(config_src.read_text(encoding="utf-8"), encoding="utf-8")

    model_path = snapshot / "kokoro-v1_0.pth"
    state = torch.load(model_path, map_location="cpu", weights_only=True)
    model_stats = write_koro(out / "kokoro-v1_0.koro", flatten_state("", state), mixed_q8=args.mixed_q8)

    voice_dir = out / "voices"
    exported_voices = []
    for voice in [v.strip() for v in args.voices.split(",") if v.strip()]:
        voice_path = snapshot / "voices" / f"{voice}.pt"
        pack = torch.load(voice_path, map_location="cpu", weights_only=True)
        voice_stats = write_koro(voice_dir / f"{voice}.koro", flatten_state("pack", pack), mixed_q8=False)
        exported_voices.append({"voice": voice, "file": f"voices/{voice}.koro", "tensors": voice_stats["tensors"]})

    manifest = {
        "format": "kokoro-go-assets-v2-mixed-q8" if args.mixed_q8 else "kokoro-go-assets-v1",
        "repo_id": "hexgrad/Kokoro-82M",
        "config": "config.json",
        "model": {"file": "kokoro-v1_0.koro", **model_stats},
        "voices": exported_voices,
    }
    (out / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(manifest, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
