package agent

import (
	"strings"
	"testing"
)

func TestAdmitSessionFactReplacesSameEntity(t *testing.T) {
	overlay := NewSessionFactOverlay()
	if !AdmitSessionFact(overlay, SessionFact{Entity: "ip:10.0.0.1", Claim: "10.0.0.1 当前可达"}) {
		t.Fatal("expected first admit")
	}
	if !AdmitSessionFact(overlay, SessionFact{Entity: "ip:10.0.0.1", Claim: "10.0.0.1 当前不可达", Evidence: "bash: 100% packet loss"}) {
		t.Fatal("expected update")
	}
	if overlay.Len() != 1 {
		t.Fatalf("len=%d", overlay.Len())
	}
	got, ok := LookupSessionFact(overlay, "ip:10.0.0.1")
	if !ok {
		t.Fatal("missing fact")
	}
	if got.Claim != "10.0.0.1 当前不可达" {
		t.Fatalf("claim=%q", got.Claim)
	}
	if got.Prior != "10.0.0.1 当前可达" {
		t.Fatalf("prior=%q", got.Prior)
	}
}

func TestRenderSessionFactsOverridesInstruction(t *testing.T) {
	overlay := NewSessionFactOverlay()
	AdmitSessionFact(overlay, SessionFact{
		Entity:   "ip:192.168.1.10",
		Claim:    "192.168.1.10 当前不可达",
		Evidence: "bash: unreachable",
		Prior:    "192.168.1.10 当前可达",
	})
	got := RenderSessionFacts(overlay)
	if !strings.HasPrefix(got, SessionFactsMarker+"\n") {
		t.Fatalf("missing marker: %q", got)
	}
	if !strings.Contains(got, "覆盖历史记录与记忆仓库中的旧结论") {
		t.Fatalf("missing override instruction: %q", got)
	}
	if !strings.Contains(got, "192.168.1.10 当前不可达") {
		t.Fatalf("missing claim: %q", got)
	}
	if !strings.Contains(got, "旧结论作废") {
		t.Fatalf("missing prior: %q", got)
	}
}

func TestApplySessionFactSectionIdempotent(t *testing.T) {
	overlay := NewSessionFactOverlay()
	AdmitSessionFact(overlay, SessionFact{Entity: "ip:1.2.3.4", Claim: "1.2.3.4 当前不可达"})
	conv := []interface{}{map[string]string{"role": "system", "content": "policy"}}
	got := ApplySessionFactSection(conv, overlay, true)
	_, content, _ := systemPromptContent(got[0])
	if !strings.Contains(content, "policy") || !strings.Contains(content, SessionFactsMarker) {
		t.Fatalf("splice failed: %q", content)
	}
	again := ApplySessionFactSection(got, overlay, true)
	_, content2, _ := systemPromptContent(again[0])
	if strings.Count(content2, SessionFactsMarker) != 1 {
		t.Fatalf("not idempotent: %q", content2)
	}
	stripped := ApplySessionFactSection(again, overlay, false)
	_, content3, _ := systemPromptContent(stripped[0])
	if strings.Contains(content3, SessionFactsMarker) {
		t.Fatalf("strip failed: %q", content3)
	}
}

func TestExtractSessionFactFromToolPingUnreachable(t *testing.T) {
	fact, ok := ExtractSessionFactFromTool(
		"bash",
		`{"command":"ping -c 1 10.1.2.3"}`,
		"PING 10.1.2.3: 100% packet loss",
		ToolExecutionOutcomeOK,
	)
	if !ok {
		t.Fatal("expected fact")
	}
	if fact.Entity != "ip:10.1.2.3" {
		t.Fatalf("entity=%q", fact.Entity)
	}
	if !strings.Contains(fact.Claim, "不可达") {
		t.Fatalf("claim=%q", fact.Claim)
	}
}

func TestExtractSessionFactFromToolPrefersHostnameInArgsOverResolvedIP(t *testing.T) {
	fact, ok := ExtractSessionFactFromTool(
		"bash",
		`{"command":"ping -c 1 jump.example.com"}`,
		"PING jump.example.com (10.1.2.3): 100% packet loss",
		ToolExecutionOutcomeOK,
	)
	if !ok {
		t.Fatal("expected fact")
	}
	if fact.Entity != "host:jump.example.com" {
		t.Fatalf("entity=%q, want host:jump.example.com so warehouse hostname facts can be superseded", fact.Entity)
	}
	hasIP := false
	for _, alias := range fact.Aliases {
		if alias == "ip:10.1.2.3" {
			hasIP = true
			break
		}
	}
	if !hasIP {
		t.Fatalf("aliases=%v, want ip:10.1.2.3 so IP warehouse facts can be superseded", fact.Aliases)
	}
}

func TestExtractSessionFactAliasesSkipGatewayAndNoteIPs(t *testing.T) {
	fact, ok := ExtractSessionFactFromTool(
		"bash",
		`{"command":"ping -c 1 jump.example.com","note":"fallback 8.8.8.8"}`,
		"PING jump.example.com (10.1.2.3): 56 data bytes\nFrom 192.168.1.1 icmp_seq=1 Destination Host Unreachable\n100% packet loss",
		ToolExecutionOutcomeOK,
	)
	if !ok {
		t.Fatal("expected fact")
	}
	if fact.Entity != "host:jump.example.com" {
		t.Fatalf("entity=%q", fact.Entity)
	}
	want := map[string]bool{"ip:10.1.2.3": true}
	for _, alias := range fact.Aliases {
		if !want[alias] {
			t.Fatalf("unexpected alias %q in %#v", alias, fact.Aliases)
		}
		delete(want, alias)
	}
	for missing := range want {
		t.Fatalf("missing resolved-IP alias %q in %#v", missing, fact.Aliases)
	}
}

func TestExtractSessionFactFromToolPrefersDestinationOverBindAddress(t *testing.T) {
	fact, ok := ExtractSessionFactFromTool(
		"bash",
		`{"command":"ping -c 1 -I 192.168.0.2 10.1.2.3"}`,
		"PING 10.1.2.3 (10.1.2.3): 100% packet loss",
		ToolExecutionOutcomeOK,
	)
	if !ok {
		t.Fatal("expected fact")
	}
	if fact.Entity != "ip:10.1.2.3" {
		t.Fatalf("entity=%q, bind address must not win over the ping destination", fact.Entity)
	}
	for _, alias := range fact.Aliases {
		if alias == "ip:192.168.0.2" {
			t.Fatalf("bind address leaked into aliases: %#v", fact.Aliases)
		}
	}
}

func TestExtractSessionFactFromToolIgnoresUnrelatedIPInArgsJSON(t *testing.T) {
	fact, ok := ExtractSessionFactFromTool(
		"bash",
		`{"command":"ping -c 1 jump.example.com","note":"fallback 8.8.8.8"}`,
		"PING jump.example.com (10.1.2.3): 100% packet loss",
		ToolExecutionOutcomeOK,
	)
	if !ok {
		t.Fatal("expected fact")
	}
	if fact.Entity != "host:jump.example.com" {
		t.Fatalf("entity=%q, JSON note IP must not win over the ping target", fact.Entity)
	}
}

func TestExtractSessionFactFromToolSSHUserAtHost(t *testing.T) {
	fact, ok := ExtractSessionFactFromTool(
		"ssh",
		`{"command":"ssh user@jump.example.com"}`,
		"ssh: connect to host jump.example.com port 22: Connection refused",
		ToolExecutionOutcomeError,
	)
	if !ok {
		t.Fatal("expected fact")
	}
	if fact.Entity != "host:jump.example.com" {
		t.Fatalf("entity=%q, want host:jump.example.com from user@host", fact.Entity)
	}
}

func TestExtractSessionFactFromToolHostPort(t *testing.T) {
	fact, ok := ExtractSessionFactFromTool(
		"bash",
		`{"command":"nc -vz jump.example.com:22"}`,
		"nc: connect to jump.example.com port 22 (tcp) failed: Connection refused",
		ToolExecutionOutcomeError,
	)
	if !ok {
		t.Fatal("expected fact")
	}
	if fact.Entity != "host:jump.example.com" {
		t.Fatalf("entity=%q, want host without :port", fact.Entity)
	}
}

func TestExtractSessionFactFromToolIgnoresTrailingCommentAndNextCommand(t *testing.T) {
	fact, ok := ExtractSessionFactFromTool(
		"bash",
		`{"command":"ping -c 1 10.1.2.3 # see docs.example.com"}`,
		"PING 10.1.2.3 (10.1.2.3): 100% packet loss",
		ToolExecutionOutcomeOK,
	)
	if !ok {
		t.Fatal("expected fact")
	}
	if fact.Entity != "ip:10.1.2.3" {
		t.Fatalf("entity=%q, comment hostname must not win", fact.Entity)
	}

	fact, ok = ExtractSessionFactFromTool(
		"bash",
		`{"command":"ping -c 1 10.1.2.3 && curl -I https://docs.example.com"}`,
		"PING 10.1.2.3 (10.1.2.3): 100% packet loss",
		ToolExecutionOutcomeOK,
	)
	if !ok {
		t.Fatal("expected fact from compound command")
	}
	if fact.Entity != "ip:10.1.2.3" {
		t.Fatalf("entity=%q, later curl host must not win", fact.Entity)
	}
}

func TestExtractSessionFactFromToolIgnoresUnrelatedError(t *testing.T) {
	if _, ok := ExtractSessionFactFromTool("bash", `{"command":"ls"}`, "ls: no such file", ToolExecutionOutcomeError); ok {
		t.Fatal("unrelated error must not become a session fact")
	}
}

func TestDetectSessionFactPolarityPrefersLaterUnreachable(t *testing.T) {
	got := DetectSessionFactPolarity("host 10.0.0.8 is reachable\nping: 10.0.0.8 unreachable")
	if got != SessionFactPolarityUnreachable {
		t.Fatalf("polarity=%v", got)
	}
}

func TestDetectSessionFactPolarityRecognizesGeneratedClaims(t *testing.T) {
	if got := DetectSessionFactPolarity("10.9.8.7 当前不可达"); got != SessionFactPolarityUnreachable {
		t.Fatalf("unreachable claim polarity=%v", got)
	}
	if got := DetectSessionFactPolarity("10.9.8.7 当前可达"); got != SessionFactPolarityReachable {
		t.Fatalf("reachable claim polarity=%v", got)
	}
}

func TestDetectSessionFactPolarityCannotTongIsUnreachable(t *testing.T) {
	if got := DetectSessionFactPolarity("跳板机 10.0.0.1 不能通"); got != SessionFactPolarityUnreachable {
		t.Fatalf("不能通 polarity=%v", got)
	}
}

func TestDetectSessionFactPolarityNotConnectedToIsUnreachable(t *testing.T) {
	if got := DetectSessionFactPolarity("ssh: not connected to 10.0.0.1"); got != SessionFactPolarityUnreachable {
		t.Fatalf("not connected to polarity=%v", got)
	}
	if got := DetectSessionFactPolarity("host 10.0.0.1 is not reachable"); got != SessionFactPolarityUnreachable {
		t.Fatalf("not reachable polarity=%v", got)
	}
}

func TestNoteSessionFactFromToolReturnsAdmittedClaim(t *testing.T) {
	overlay, fact, ok := noteSessionFactFromTool(
		nil,
		"bash",
		`{"command":"ping -c 1 10.1.2.3"}`,
		"PING 10.1.2.3: 100% packet loss",
		ToolExecutionOutcomeOK,
	)
	if !ok {
		t.Fatal("expected admitted fact")
	}
	if overlay == nil || overlay.Len() != 1 {
		t.Fatalf("overlay=%#v", overlay)
	}
	if fact.Entity != "ip:10.1.2.3" || !strings.Contains(fact.Claim, "不可达") {
		t.Fatalf("fact=%#v", fact)
	}
	_, _, again := noteSessionFactFromTool(overlay, "bash", `{"command":"ping -c 1 10.1.2.3"}`, "PING 10.1.2.3: 100% packet loss", ToolExecutionOutcomeOK)
	if again {
		t.Fatal("identical fact must not re-admit")
	}
	_, _, evidenceOnly := noteSessionFactFromTool(
		overlay,
		"bash",
		`{"command":"ping -c 1 10.1.2.3"}`,
		"PING 10.1.2.3: destination host unreachable",
		ToolExecutionOutcomeOK,
	)
	if evidenceOnly {
		t.Fatal("same claim with new evidence must not rewrite stores")
	}
}

func TestNoteSessionFactFromToolAdmitsResolvedIPAlias(t *testing.T) {
	overlay, fact, ok := noteSessionFactFromTool(
		nil,
		"bash",
		`{"command":"ping -c 1 jump.example.com"}`,
		"PING jump.example.com (10.1.2.3): 100% packet loss",
		ToolExecutionOutcomeOK,
	)
	if !ok {
		t.Fatal("expected writeback")
	}
	if fact.Entity != "host:jump.example.com" {
		t.Fatalf("entity=%q", fact.Entity)
	}
	if _, found := LookupSessionFact(overlay, "ip:10.1.2.3"); !found {
		t.Fatal("resolved IP must be in the overlay so later IP recall sees the update")
	}
}

func TestExtractSessionFactFromMemoryContent(t *testing.T) {
	fact, ok := ExtractSessionFactFromMemoryContent("服务器 192.168.0.9 已经不通了")
	if !ok {
		t.Fatal("expected fact")
	}
	if fact.Entity != "ip:192.168.0.9" {
		t.Fatalf("entity=%q", fact.Entity)
	}
	if DetectSessionFactPolarity(fact.Claim) != SessionFactPolarityUnreachable && !strings.Contains(fact.Claim, "不可达") {
		t.Fatalf("claim=%q", fact.Claim)
	}
}

func TestExtractSessionFactFromMemoryContentHostname(t *testing.T) {
	fact, ok := ExtractSessionFactFromMemoryContent("跳板机 jump.example.com 当前不可达")
	if !ok {
		t.Fatal("expected hostname fact")
	}
	if fact.Entity != "host:jump.example.com" {
		t.Fatalf("entity=%q", fact.Entity)
	}
}
