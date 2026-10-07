package guiapp

import "github.com/RapidAI/CodeClaw/corelib/progress"

// qwen3InterruptRelevanceBuiltin is the Qwen3 interrupt relevance model
// (round 3, maclaw_reranker/out/replace_audit/int3): asymmetric instruction
// cosine, chosen among 36 task×message instruction combos + a logistic
// scorer on the eval TRAIN half only (scorer weights fit on 1222 fresh
// Llama/GLM-generated, cross-family-judged pairs); bands calibrated on the
// train half (≤5% unrelated ≥ High, ≤5% related < Low) and mapped onto the
// scheduler's 0.60/0.30. Test half (n=124): AUC 0.830 (plain cosine 0.738,
// Gemma 0.851). Pure cosine, so it ships in code (no release asset).
var qwen3InterruptRelevanceBuiltin = &progress.RelevanceModel{
	Format:             progress.RelevanceModelFormat,
	Kind:               "cosine",
	EmbedderModelID:    "Qwen3 Embedding 0.6b:1024:prompt-v1:qwen3-instruct-v1",
	Dims:               1024,
	TaskInstruction:    "Represent the user request by the task it concerns, so that a follow-up message and the task it modifies are close",
	MessageInstruction: "Given a message a user sent while an AI agent was executing a task, retrieve the task this message refers to",
	High:               0.6335,
	Low:                0.3651,
}
