package cloudagent

import "testing"

// The thinking level is owner preference state that rides the provision and
// the custom-LLM save. Without it maclawCfg builds an auto (empty) config,
// the request body carries no reasoning field, and cloud turns show no 思考
// while the same provider locally does.
func TestThinkingLevelFlowsToMaclawCfg(t *testing.T) {
	rt := openRuntime(t)
	// The official branch needs the same-origin hub env the other tests set.
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	spec := putSession(t, rt, "http://127.0.0.1:9")
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}

	// No level — auto: the request stays provider-default, exactly like a
	// desktop running with the global level unset.
	cfg := rt.maclawCfg(inst)
	if cfg.ThinkingMode != "" || cfg.ReasoningEffort != "" {
		t.Fatalf("auto cfg thinking=%q effort=%q, want empty", cfg.ThinkingMode, cfg.ReasoningEffort)
	}

	inst.meta.LLMThinkingMode = "junk"
	if got := normalizeThinkingLevel(inst.meta.LLMThinkingMode); got != "" {
		t.Fatalf("junk level normalized to %q, want auto", got)
	}
	inst.meta.LLMThinkingMode = "high"
	cfg = rt.maclawCfg(inst)
	if cfg.ThinkingMode != "enabled" || cfg.ReasoningEffort != "high" {
		t.Fatalf("high cfg thinking=%q effort=%q, want enabled/high", cfg.ThinkingMode, cfg.ReasoningEffort)
	}

	inst.meta.LLMThinkingMode = "none"
	cfg = rt.maclawCfg(inst)
	if cfg.ThinkingMode != "disabled" || cfg.ReasoningEffort != "" {
		t.Fatalf("none cfg thinking=%q effort=%q, want disabled/empty", cfg.ThinkingMode, cfg.ReasoningEffort)
	}
}

func TestSaveLLMMirrorsThinkingLevel(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	if err := rt.SaveLLM(spec.ID, CustomLLM{
		URL: "https://api.example.com", Key: "k", Model: "m",
		ThinkingMode: "Weird",
	}); err != nil {
		t.Fatal(err)
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	// "Weird" is not a level: normalization empties it and the save keeps
	// the previous (empty) value instead of storing garbage.
	if got := inst.meta.LLMThinkingMode; got != "" {
		t.Fatalf("junk level mirrored as %q, want empty", got)
	}
	if err := rt.SaveLLM(spec.ID, CustomLLM{
		URL: "https://api.example.com", Key: "k", Model: "m",
		ThinkingMode: "low",
	}); err != nil {
		t.Fatal(err)
	}
	if got := inst.meta.LLMThinkingMode; got != "low" {
		t.Fatalf("spec level=%q, want low", got)
	}
	appCfg, err := rt.llmConfig(inst)
	if err != nil {
		t.Fatal(err)
	}
	if got := appCfg.MaclawLLMThinkingMode; got != "low" {
		t.Fatalf("llmConfig thinking mode=%q, want low", got)
	}
}

func TestPutPreservesThinkingLevel(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	inst.meta.LLMThinkingMode = "high"
	// A re-provision from an older Hub carries no level: the saved one wins.
	fresh := spec
	fresh.LLMThinkingMode = ""
	if err := rt.Put(fresh); err != nil {
		t.Fatal(err)
	}
	inst, err = rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := inst.meta.LLMThinkingMode; got != "high" {
		t.Fatalf("level after re-provision=%q, want high", got)
	}
	// An explicit level updates it.
	fresh.LLMThinkingMode = "none"
	if err := rt.Put(fresh); err != nil {
		t.Fatal(err)
	}
	inst, _ = rt.instance(spec.ID)
	if got := inst.meta.LLMThinkingMode; got != "none" {
		t.Fatalf("level after explicit re-provision=%q, want none", got)
	}
}
