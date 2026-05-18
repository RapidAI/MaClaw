#!/usr/bin/env python3
"""Create a MacLaw Needle artifact directory from a fine-tuned model folder.

This is the packaging step between Python training and the pure-Go runtime. It
records a stable manifest and copies tokenizer/config files plus an explicit
converted q8 weight file. Raw HuggingFace .safetensors/.bin files are not Go q8
weights; pass --weight after conversion, or --allow-placeholder for inspect-only
smoke tests.
"""

from __future__ import annotations

import argparse
import struct
import hashlib
import json
import shutil
from pathlib import Path


TOKENIZER_FILES = (
    "tokenizer.json",
    "tokenizer.model",
    "tokenizer_config.json",
    "special_tokens_map.json",
    "vocab.json",
    "merges.txt",
)

CONFIG_FILES = ("config.json", "generation_config.json")
WEIGHT_MAGIC = b"MLNDLQ8\0"
WEIGHT_VERSION = 1
DEFAULT_LABELS = {
    "workflow_review": ["confirm", "supplement", "skip", "cancel", "switch_task", "other"],
    "intent_gate": ["route_ssh", "route_browser", "route_web_search", "route_office", "route_coding", "route_workflow", "no_call"],
    "memory_extract_gate": ["extract_memory", "no_extract"],
}


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def copy_existing(src_dir: Path, out_dir: Path, names: tuple[str, ...]) -> list[str]:
    copied = []
    for name in names:
        src = src_dir / name
        if src.exists() and src.is_file():
            shutil.copy2(src, out_dir / name)
            copied.append(name)
    return copied


def write_placeholder_weight(path: Path) -> None:
    header = struct.pack("<8sIIIIII", WEIGHT_MAGIC, WEIGHT_VERSION, 1, 1, 1, 0, 32)
    embeddings = struct.pack("b", 1)
    head = struct.pack("b", 1)
    bias = struct.pack("<f", 0.0)
    path.write_bytes(header + embeddings + head + bias)


def read_q8_header(path: Path) -> dict:
    data = path.read_bytes()[:32]
    if len(data) != 32:
        raise SystemExit(f"q8 weight header is too short: {path}")
    magic, version, vocab, hidden, labels, flags, offset = struct.unpack("<8sIIIIII", data)
    if magic != WEIGHT_MAGIC:
        raise SystemExit(f"invalid q8 weight magic in {path}; pass a converted MacLaw q8 file")
    if version != WEIGHT_VERSION:
        raise SystemExit(f"unsupported q8 weight version {version} in {path}")
    return {"vocab_size": vocab, "hidden_size": hidden, "num_labels": labels, "flags": flags, "data_offset": offset}


def expected_q8_size(header: dict) -> int:
    vocab = int(header["vocab_size"])
    hidden = int(header["hidden_size"])
    labels = int(header["num_labels"])
    flags = int(header["flags"])
    sparse_hash_head = bool(flags & 2)
    embedding_bytes = 0 if sparse_hash_head else vocab * hidden
    return int(header["data_offset"]) + embedding_bytes + labels * hidden + labels * 4


def validate_q8_weight(path: Path) -> dict:
    header = read_q8_header(path)
    expected = expected_q8_size(header)
    actual = path.stat().st_size
    if actual != expected:
        raise SystemExit(f"q8 weight size mismatch for {path}: got {actual} bytes, want {expected} bytes")
    return header


def labels_for_tasks(tasks: list[str]) -> list[str]:
    labels = []
    seen = set()
    for task in tasks:
        for label in DEFAULT_LABELS.get(task, []):
            if label not in seen:
                labels.append(label)
                seen.add(label)
    return labels


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--model", required=True, help="fine-tuned model directory from needle_finetune.py")
    p.add_argument("--out", required=True, help="output MacLaw Needle artifact directory")
    p.add_argument("--tasks", default="workflow_review,intent_gate,memory_extract_gate", help="comma-separated task names")
    p.add_argument("--quant", default="q8-placeholder", help="quantization label recorded in manifest")
    p.add_argument("--weight", help="converted Go q8 weight file to copy; raw HF weights are not accepted")
    p.add_argument("--allow-placeholder", action="store_true", help="write a placeholder weight file when no converted weight is available")
    args = p.parse_args()

    model_dir = Path(args.model)
    out_dir = Path(args.out)
    if not model_dir.exists() or not model_dir.is_dir():
        raise SystemExit(f"model directory not found: {model_dir}")
    out_dir.mkdir(parents=True, exist_ok=True)

    copied_tokenizers = copy_existing(model_dir, out_dir, TOKENIZER_FILES)
    copied_configs = copy_existing(model_dir, out_dir, CONFIG_FILES)

    weight_src = Path(args.weight) if args.weight else None
    weight_name = "needle.q8"
    weight_path = out_dir / weight_name
    weight_sha256 = ""
    if weight_src and weight_src.exists():
        shutil.copy2(weight_src, weight_path)
        weight_header = validate_q8_weight(weight_path)
        weight_sha256 = sha256_file(weight_path)
    elif args.allow_placeholder:
        write_placeholder_weight(weight_path)
        weight_header = validate_q8_weight(weight_path)
        weight_sha256 = sha256_file(weight_path)
    else:
        raise SystemExit("no converted q8 weight found; pass --weight or --allow-placeholder")

    tokenizer = ""
    for candidate in ("tokenizer.json", "tokenizer.model"):
        if candidate in copied_tokenizers:
            tokenizer = candidate
            break

    tasks = [t.strip() for t in args.tasks.split(",") if t.strip()]
    labels = labels_for_tasks(tasks)
    labels_name = "labels.json"
    (out_dir / labels_name).write_text(json.dumps(labels, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    manifest = {
        "format": "maclaw-needle",
        "version": 1,
        "runtime": "go",
        "tasks": tasks,
        "weight_path": weight_name,
        "weight_sha256": weight_sha256,
        "tokenizer": tokenizer,
        "labels": labels_name,
        "quant": args.quant,
        "weight_header": weight_header,
        "source_model": str(model_dir),
        "files": sorted(copied_tokenizers + copied_configs + [weight_name, labels_name]),
        "notes": "Packaged for MacLaw local Needle runtime. Go tensor inference is wired separately.",
    }
    (out_dir / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(manifest, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
