#!/usr/bin/env python3
"""Fine-tune cactus-compute/needle on MacLaw micro-decision JSONL.

Preferred input is the Needle native export from:
  go run ./cmd/maclaw-needle export-needle -in train.jsonl -out needle_train.jsonl

For convenience this script also accepts MacLaw's internal training-record JSONL
and renders it into the same prompt shape.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path


def load_records(path: Path):
    with path.open("r", encoding="utf-8-sig") as f:
        for line in f:
            line = line.strip()
            if line:
                yield json.loads(line)


def is_needle_record(rec: dict) -> bool:
    return all(k in rec for k in ("query", "tools", "answers"))


def needle_tools_for_task(task: str, tools: list[dict]) -> list[dict]:
    def tool(name: str, description: str) -> dict:
        return {"name": name, "description": description, "parameters": {}}

    if task == "workflow_review":
        return [
            tool("confirm", "Approve the current workflow phase and continue."),
            tool("supplement", "Add corrections, requirements, or extra context for the current phase."),
            tool("skip", "Skip the current phase when allowed."),
            tool("cancel", "Cancel the current workflow."),
            tool("switch_task", "Cancel this workflow and start a different task."),
            tool("other", "Keep waiting because the reply is unrelated or ambiguous."),
        ]
    if task == "memory_extract_gate":
        return [tool("extract_memory", "Extract durable user or project knowledge into memory."), tool("no_extract", "Do not extract a memory from this message.")]
    if task == "intent_gate":
        return [
            tool("route_ssh", "Route to remote shell or server operation."),
            tool("route_browser", "Route to browser automation."),
            tool("route_web_search", "Route to web search or online research."),
            tool("route_office", "Route to document, spreadsheet, or presentation work."),
            tool("route_coding", "Route to local coding, debugging, or maintenance work."),
            tool("route_workflow", "Route to a structured MacLaw workflow."),
            tool("no_call", "Do not route to a tool or workflow."),
        ]
    return [{"name": t.get("name", ""), "description": t.get("description", ""), "parameters": t.get("parameters") or {}} for t in tools]


def training_record_to_needle(rec: dict) -> dict:
    messages = rec.get("messages") or []
    system = next((m.get("content", "") for m in messages if m.get("role") == "system"), "")
    user = next((m.get("content", "") for m in messages if m.get("role") == "user"), "")
    task = rec.get("task", "")
    query = f"Task: {task}\nInstruction: {system}\nUser: {user}".strip()
    expected = rec.get("expected") or {}
    answers = [{"name": expected.get("name", ""), "arguments": expected.get("arguments") or {}}]
    return {
        "query": query,
        "tools": json.dumps(needle_tools_for_task(task, rec.get("tools") or []), ensure_ascii=False, separators=(",", ":")),
        "answers": json.dumps(answers, ensure_ascii=False, separators=(",", ":")),
    }


def normalize_record(rec: dict) -> dict:
    if is_needle_record(rec):
        return rec
    return training_record_to_needle(rec)


def render_record(rec: dict, include_answer: bool = True) -> str:
    rec = normalize_record(rec)
    suffix = f"\nassistant: {rec.get('answers', '[]')}" if include_answer else "\nassistant:"
    return f"Query:\n{rec.get('query', '')}\n\nTools:\n{rec.get('tools', '[]')}" + suffix


def prediction_id(rec: dict) -> str:
    return rec.get("id") or rec.get("event_id") or rec.get("query", "")[:80]


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--train", required=True)
    p.add_argument("--eval")
    p.add_argument("--model", default="cactus-compute/needle")
    p.add_argument("--out", default="models/needle-maclaw-ft")
    p.add_argument("--epochs", type=float, default=3)
    p.add_argument("--lr", type=float, default=2e-5)
    p.add_argument("--max-length", type=int, default=1024)
    p.add_argument("--batch-size", type=int, default=4)
    p.add_argument("--predict", help="optional JSONL records to run after training and write predictions for")
    p.add_argument("--predictions-out", default="predictions.jsonl")
    args = p.parse_args()

    from datasets import Dataset
    from transformers import AutoModelForCausalLM, AutoTokenizer, DataCollatorForLanguageModeling, Trainer, TrainingArguments

    train_texts = [render_record(r) for r in load_records(Path(args.train))]
    if not train_texts:
        raise SystemExit("no training records")
    eval_texts = [render_record(r) for r in load_records(Path(args.eval))] if args.eval else None

    tokenizer = AutoTokenizer.from_pretrained(args.model, trust_remote_code=True)
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token

    def tok(batch):
        return tokenizer(batch["text"], truncation=True, max_length=args.max_length)

    train_ds = Dataset.from_dict({"text": train_texts}).map(tok, batched=True, remove_columns=["text"])
    eval_ds = Dataset.from_dict({"text": eval_texts}).map(tok, batched=True, remove_columns=["text"]) if eval_texts else None
    model = AutoModelForCausalLM.from_pretrained(args.model, trust_remote_code=True)

    training_args = TrainingArguments(
        output_dir=args.out,
        num_train_epochs=args.epochs,
        learning_rate=args.lr,
        per_device_train_batch_size=args.batch_size,
        per_device_eval_batch_size=args.batch_size,
        eval_strategy="epoch" if eval_ds is not None else "no",
        save_strategy="epoch",
        logging_steps=20,
        report_to=[],
    )
    trainer = Trainer(
        model=model,
        args=training_args,
        train_dataset=train_ds,
        eval_dataset=eval_ds,
        data_collator=DataCollatorForLanguageModeling(tokenizer=tokenizer, mlm=False),
    )
    trainer.train()
    trainer.save_model(args.out)
    tokenizer.save_pretrained(args.out)
    print(f"saved fine-tuned model to {args.out}")

    if args.predict:
        import torch

        pred_path = Path(args.predictions_out)
        pred_path.parent.mkdir(parents=True, exist_ok=True)
        model.eval()
        with pred_path.open("w", encoding="utf-8") as f:
            for rec in load_records(Path(args.predict)):
                prompt = render_record(rec, include_answer=False)
                inputs = tokenizer(prompt, return_tensors="pt", truncation=True, max_length=args.max_length).to(model.device)
                with torch.no_grad():
                    output = model.generate(**inputs, max_new_tokens=64, do_sample=False, pad_token_id=tokenizer.eos_token_id)
                text = tokenizer.decode(output[0][inputs["input_ids"].shape[1]:], skip_special_tokens=True).strip()
                name = ""
                parsed_args = {}
                try:
                    parsed = json.loads(text.splitlines()[0])
                    first = parsed[0] if isinstance(parsed, list) and parsed else parsed
                    name = first.get("name", "")
                    parsed_args = first.get("arguments") or {}
                except Exception:
                    name = text.split()[0] if text.split() else ""
                f.write(json.dumps({"id": prediction_id(rec), "name": name, "arguments": parsed_args, "raw": text}, ensure_ascii=False) + "\n")
        print(f"wrote predictions to {pred_path}")


if __name__ == "__main__":
    main()
