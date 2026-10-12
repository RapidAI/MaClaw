package agent

import (
	"strings"
	"testing"
)

// TestBuildPromptBundle_DiagramBlockStaysInStableSegment pins the placement
// that makes the visual-output block cache-friendly.
//
// The visualization rules must live in the STABLE segment. They are static text
// for a given surface, so writing them there keeps StableCacheKey unchanged
// across every turn of a chat — measured: identical prompts for three different
// user questions on the same GUI surface all hash to one key. Moving them into
// SessionContext (which varies per message: project dir, memory, MCP list)
// would mint a new cache key on every single turn and silently disable prompt
// caching for the whole session.
func TestBuildPromptBundle_DiagramBlockStaysInStableSegment(t *testing.T) {
	on := SystemPromptConfig{
		RoleName:                  "MaClaw",
		PromptProfile:             PromptProfileFull,
		DiagramRenderingSupported: true,
	}
	off := SystemPromptConfig{
		RoleName:      "MaClaw",
		PromptProfile: PromptProfileFull,
	}

	withBlock := BuildPromptBundle(SystemPromptDeps{Config: on}, "解释一下这个系统的架构", true)
	without := BuildPromptBundle(SystemPromptDeps{Config: off}, "解释一下这个系统的架构", true)

	// Present in stable, absent from session — placement is the whole point.
	if !strings.Contains(withBlock.StableSystemPrompt, "讲解配图") {
		t.Fatal("visualization rules must be written to the stable segment")
	}
	if strings.Contains(withBlock.SessionContext, "讲解配图") {
		t.Fatal("visualization rules must not leak into the per-turn session segment")
	}
	// A turn without support has no diagram text anywhere at all.
	if strings.Contains(without.StableSystemPrompt, "讲解配图") {
		t.Fatal("undeclared surface must carry no visualization text")
	}

	// Stable key is stable across turns of one surface, and only differs
	// between surfaces. Per-turn variability here would be the real cost.
	second := BuildPromptBundle(SystemPromptDeps{Config: on}, "讲讲 RAG 的原理", true)
	third := BuildPromptBundle(SystemPromptDeps{Config: on}, "帮我看看这个函数", true)
	keys := []string{withBlock.StableCacheKey(), second.StableCacheKey(), third.StableCacheKey()}
	if keys[0] != keys[1] || keys[1] != keys[2] {
		t.Fatalf("diagram-enabled GUI turns must share one cache key, got %v", keys)
	}
	if without.StableCacheKey() == keys[0] {
		t.Fatal("surfaces with different diagram support should not share a cache key")
	}
}

// TestBuildPromptBundle_DiagramFlagStableAcrossEveryProfile extends the same
// guarantee to every profile that can carry the block. Diagram support is a
// property of the reply SURFACE, not of the message, so once a conversation
// starts with a given surface each profile must keep one key turn after turn.
// (Profiles themselves legitimately differ from each other — that is not
// fragmentation; a key that changed between turns of the same profile would be.)
func TestBuildPromptBundle_DiagramFlagStableAcrossEveryProfile(t *testing.T) {
	// Mix odd and even lengths deliberately. A set of same-parity (or otherwise
	// uniform) messages would silently absorb any per-message variation in the
	// block and make this guard vacuous.
	messages := []string{
		"解释架构",
		"讲讲 RAG 的原理到底是什么",
		"帮我看看这个函数",
		"梳理审批流程，并画出状态流转图，再说明关键路径和异常情况怎么处理。",
		"为什么？",
	}
	profiles := []struct {
		name    string
		profile PromptProfile
		managed bool
	}{
		{name: "full", profile: PromptProfileFull},
		{name: "light", profile: PromptProfileLight},
		{name: "managed", profile: PromptProfileFull, managed: true},
	}
	for _, p := range profiles {
		seen := make(map[string]bool)
		var sample PromptBundle
		for _, msg := range messages {
			sample = BuildPromptBundle(SystemPromptDeps{
				Config: SystemPromptConfig{
					RoleName:                  "MaClaw",
					IsProMode:                 true,
					PromptProfile:             p.profile,
					ManagedSemantic:           p.managed,
					DiagramRenderingSupported: true,
				},
			}, msg, true)
			seen[sample.StableCacheKey()] = true
		}
		if len(seen) != 1 {
			t.Fatalf("%s profile: diagram-supported turns produced %d distinct cache keys (want 1); a per-turn key silently disables prompt caching", p.name, len(seen))
		}
		// Sanity: the profile must actually carry the block. A profile with no
		// diagram text would be trivially "stable" and prove nothing.
		if !strings.Contains(sample.StableSystemPrompt, "讲解配图") {
			t.Fatalf("%s profile should carry the visualization block, otherwise this test is vacuous", p.name)
		}
	}
}

// TestBuildPromptBundle_DiagramBlockSizeBudget records the cost of the feature
// so a future expansion has to be a conscious decision. The full block is the
// expensive one; the light cue is tiny because that segment has a byte cap.
func TestBuildPromptBundle_DiagramBlockSizeBudget(t *testing.T) {
	base := BuildPromptBundle(SystemPromptDeps{
		Config: SystemPromptConfig{RoleName: "MaClaw", PromptProfile: PromptProfileFull},
	}, "解释架构", true)
	with := BuildPromptBundle(SystemPromptDeps{
		Config: SystemPromptConfig{RoleName: "MaClaw", PromptProfile: PromptProfileFull, DiagramRenderingSupported: true},
	}, "解释架构", true)
	delta := len(with.StableSystemPrompt) - len(base.StableSystemPrompt)
	if delta > 4096 {
		t.Fatalf("full visualization block too large: adds %d bytes (budget 4096)", delta)
	}

	baseLight := BuildPromptBundle(SystemPromptDeps{
		Config: SystemPromptConfig{RoleName: "MaClaw", PromptProfile: PromptProfileLight},
	}, "讲讲原理", true)
	withLight := BuildPromptBundle(SystemPromptDeps{
		Config: SystemPromptConfig{RoleName: "MaClaw", PromptProfile: PromptProfileLight, DiagramRenderingSupported: true},
	}, "讲讲原理", true)
	lightDelta := len(withLight.StableSystemPrompt) - len(baseLight.StableSystemPrompt)
	if lightDelta > 512 {
		t.Fatalf("light visualization cue too large: adds %d bytes (budget 512)", lightDelta)
	}
}
