package moa

import "testing"

func TestParseSlash_Basic(t *testing.T) {
	if ParseSlash("hello").Kind != SlashNone {
		t.Fatal("non-moa")
	}
	if ParseSlash("/moab").Kind != SlashNone {
		t.Fatal("glued /moab")
	}
	if ParseSlash("/moa").Kind != SlashHelp {
		t.Fatal("help")
	}
	if ParseSlash("  /MOA  ").Kind != SlashHelp {
		t.Fatal("case help")
	}
}

func TestParseSlash_OneShotDefault(t *testing.T) {
	c := ParseSlash("/moa review this plan")
	if c.Kind != SlashOneShot || c.Preset != "" || c.Prompt != "review this plan" {
		t.Fatalf("%+v", c)
	}
	c = ParseSlash("/MOA  评估方案")
	if c.Kind != SlashOneShot || c.Prompt != "评估方案" {
		t.Fatalf("%+v", c)
	}
}

func TestParseSlash_AtPreset(t *testing.T) {
	c := ParseSlash("/moa @review compare A vs B")
	if c.Kind != SlashOneShot || c.Preset != "review" || c.Prompt != "compare A vs B" {
		t.Fatalf("%+v", c)
	}
	c = ParseSlash("/moa @Review  风险")
	if c.Kind != SlashOneShot || c.Preset != "review" || c.Prompt != "风险" {
		t.Fatalf("case fold preset: %+v", c)
	}
	c = ParseSlash("/moa @moa:deep dive here")
	// "moa:deep" as single field after @? fields[0]="@moa:deep" → preset moa:deep stripped to deep
	if c.Kind != SlashOneShot {
		// actually fields[0] is "@moa:deep" → name "moa:deep" → normalize → "deep"
		t.Fatalf("kind=%v", c.Kind)
	}
	if c.Preset != "deep" || c.Prompt != "dive here" {
		t.Fatalf("moa: strip: %+v", c)
	}
	// Missing prompt
	c = ParseSlash("/moa @review")
	if c.Kind != SlashUsage || c.Preset != "review" {
		t.Fatalf("need prompt: %+v", c)
	}
	c = ParseSlash("/moa @")
	if c.Kind != SlashUsage {
		t.Fatalf("bare @: %+v", c)
	}
	c = ParseSlash("/moa @!!! hello")
	if c.Kind != SlashUsage {
		t.Fatalf("invalid @name: %+v", c)
	}
	// "@ name prompt" form
	c = ParseSlash("/moa @ arch do it")
	if c.Kind != SlashOneShot || c.Preset != "arch" || c.Prompt != "do it" {
		t.Fatalf("@ name: %+v", c)
	}
}

func TestParseSlash_StickyAndStats(t *testing.T) {
	c := ParseSlash("/moa sticky on review")
	if c.Kind != SlashSticky || c.StickyArg != "on" || c.StickyPreset != "review" {
		t.Fatalf("%+v", c)
	}
	c = ParseSlash("/moa session off")
	if c.Kind != SlashSticky || c.StickyArg != "off" {
		t.Fatalf("%+v", c)
	}
	c = ParseSlash("/moa stats")
	if c.Kind != SlashStats {
		t.Fatalf("%+v", c)
	}
	// Natural language must NOT steal sticky/stats subcommands.
	c = ParseSlash("/moa sticky keys in redis")
	if c.Kind != SlashOneShot || c.Prompt != "sticky keys in redis" {
		t.Fatalf("sticky NL: %+v", c)
	}
	c = ParseSlash("/moa stats about cost")
	if c.Kind != SlashOneShot || c.Prompt != "stats about cost" {
		t.Fatalf("stats NL: %+v", c)
	}
	if !IsMoASlash("/moa x") || IsMoASlash("/help") {
		t.Fatal("IsMoASlash")
	}
}

func TestNormalizePresetToken(t *testing.T) {
	if got := normalizePresetToken("MOA:Review"); got != "review" {
		t.Fatalf("got %q", got)
	}
	if got := normalizePresetToken("  Deep  "); got != "deep" {
		t.Fatalf("got %q", got)
	}
	if got := normalizePresetToken("Design Review"); got != "design-review" {
		t.Fatalf("spaces: %q", got)
	}
}
