package codingagent

import (
	"strings"
	"testing"
)

func TestFinalAnswerPresentationContractIsAdaptiveAndShared(t *testing.T) {
	contract := FinalAnswerPresentationContract()
	for _, want := range []string{
		"not a fixed template",
		"修复的问题",
		"优化",
		"审查过但不改的点",
		"## 验证",
		"commands actually run after the final edit",
		"Do not use internal audit headings",
	} {
		if !strings.Contains(contract, want) {
			t.Fatalf("shared contract missing %q:\n%s", want, contract)
		}
	}
	for _, unwanted := range []string{"质量审计", "探索状态", "验证状态", "执行报告"} {
		// These headings may appear only inside the negative instruction.
		if strings.Count(contract, unwanted) != 1 || !strings.Contains(contract, "Do not use internal audit headings such as") {
			t.Fatalf("internal heading %q is not confined to the prohibition", unwanted)
		}
	}
}

func TestLooksLikeVerificationCommandRequiresActualVerifierCommand(t *testing.T) {
	for _, command := range []string{
		"go test ./pkg",
		"env CI=true go test ./pkg",
		"env -u GOPRIVATE go test ./pkg",
		"npm run build",
		"python -m pytest tests",
		"make test",
		"cmake --build build",
	} {
		if !LooksLikeVerificationCommand(command) {
			t.Fatalf("verifier not recognized: %q", command)
		}
	}
	for _, command := range []string{
		"echo go test ./pkg",
		"printf 'npm run build'",
		"go run ./cmd/app",
		"dotnet run",
		"python app.py",
		"python unittest_helpers.py",
		"npm install test",
		"make install",
		"cmake -S . -B build",
	} {
		if LooksLikeVerificationCommand(command) {
			t.Fatalf("non-verifier misclassified: %q", command)
		}
	}
}

func TestAppendFinalAnswerHostFactsFillsMissingFileAndVerificationFacts(t *testing.T) {
	got := AppendFinalAnswerHostFacts(
		"已完成修复。",
		[]string{"cmd/app/main.go"},
		[]string{"pkg/fix.go"},
		[]FinalAnswerVerificationFact{{Command: "go test ./pkg", Succeeded: true, AfterFinalEdit: true}},
	)
	for _, want := range []string{"本轮实际改动", "cmd/app/main.go", "pkg/fix.go", "实际验证", "`go test ./pkg` 通过"} {
		if !strings.Contains(got, want) {
			t.Fatalf("host facts missing %q:\n%s", want, got)
		}
	}
}

func TestAppendFinalAnswerHostFactsDoesNotRepeatModelFacts(t *testing.T) {
	summary := "已修复 pkg/fix.go 的 nil 崩溃。\n\n## 验证\n- `go test ./pkg` 通过"
	got := AppendFinalAnswerHostFacts(summary,
		nil,
		[]string{"pkg/fix.go"},
		[]FinalAnswerVerificationFact{{Command: "go test ./pkg", Succeeded: true, AfterFinalEdit: true}},
	)
	if got != summary {
		t.Fatalf("host facts duplicated model facts:\n%s", got)
	}
}

func TestAppendFinalAnswerHostFactsAddsOnlyUnmentionedFiles(t *testing.T) {
	summary := "已修复 pkg/fix.go 的 nil 崩溃。"
	got := AppendFinalAnswerHostFacts(summary, []string{"pkg/fix_test.go"}, []string{"pkg/fix.go", "pkg/other.go"}, nil)
	if strings.Count(got, "pkg/fix.go") != 1 || strings.Contains(got, "更新 pkg/fix.go") {
		t.Fatalf("already mentioned file was repeated: %q", got)
	}
	if !strings.Contains(got, "pkg/fix_test.go") || !strings.Contains(got, "pkg/other.go") {
		t.Fatalf("unmentioned files were not added: %q", got)
	}
}

func TestAppendFinalAnswerHostFactsIgnoresStaleCommands(t *testing.T) {
	summary := "已完成优化。"
	got := AppendFinalAnswerHostFacts(summary, nil, nil, []FinalAnswerVerificationFact{
		{Command: "go test ./pkg", Succeeded: true, AfterFinalEdit: false},
	})
	if strings.Contains(got, "实际验证") || strings.Contains(got, "go test") {
		t.Fatalf("stale command leaked into facts: %q", got)
	}
}

func TestAppendFinalAnswerHostFactsUsesLatestRunForRepeatedVerifier(t *testing.T) {
	got := AppendFinalAnswerHostFacts("已完成修复。", nil, nil, []FinalAnswerVerificationFact{
		{Command: "go test ./pkg", Succeeded: false, AfterFinalEdit: true},
		{Command: "go test ./pkg", Succeeded: true, AfterFinalEdit: true},
	})
	if strings.Contains(got, "失败") || strings.Count(got, "go test ./pkg") != 1 {
		t.Fatalf("repeated verifier must report only the latest run: %q", got)
	}
	if !strings.Contains(got, "`go test ./pkg` 通过") {
		t.Fatalf("latest passing verifier missing: %q", got)
	}
}

func TestAppendFinalAnswerHostFactsKeepsHostClassifiedProjectVerifier(t *testing.T) {
	got := AppendFinalAnswerHostFacts("已完成优化。", nil, nil, []FinalAnswerVerificationFact{{
		Command:        `scripts\goheavy.cmd go test ./pkg`,
		Succeeded:      true,
		AfterFinalEdit: true,
	}})
	if !strings.Contains(got, `scripts\goheavy.cmd go test ./pkg`) {
		t.Fatalf("host-classified project verifier was dropped: %q", got)
	}
}

func TestAppendFinalAnswerHostFactsSanitizesRawCommandWithEnvironment(t *testing.T) {
	raw := "TOKEN=secret-value go test ./pkg"
	summary := "已验证：`TOKEN=secret-value go test ./pkg` 通过。"
	got := AppendFinalAnswerHostFacts(summary, nil, nil, []FinalAnswerVerificationFact{{
		Command:        raw,
		Succeeded:      true,
		AfterFinalEdit: true,
	}})
	if strings.Contains(got, "secret-value") {
		t.Fatalf("raw verifier secret remained in final answer:\n%s", got)
	}
	if !strings.Contains(got, "`go test ./pkg` 通过") || strings.Contains(got, "实际验证") {
		t.Fatalf("sanitized verifier mention was duplicated or lost:\n%s", got)
	}
}

func TestAppendFinalAnswerHostFactsRedactsCommandSecrets(t *testing.T) {
	got := AppendFinalAnswerHostFacts("已完成验证。", nil, nil, []FinalAnswerVerificationFact{{
		Command:        "TOKEN=secret-value go test ./pkg --api_key=private-value",
		Succeeded:      true,
		AfterFinalEdit: true,
	}})
	if strings.Contains(got, "secret-value") || strings.Contains(got, "private-value") {
		t.Fatalf("command secret leaked into host facts: %q", got)
	}
	if !strings.Contains(got, "go test ./pkg") || !strings.Contains(got, "--api_key=<redacted>") {
		t.Fatalf("sanitized verifier command missing: %q", got)
	}
}

func TestAppendFinalAnswerHostFactsRedactsSeparateSecretArguments(t *testing.T) {
	got := AppendFinalAnswerHostFacts("已完成验证。", nil, nil, []FinalAnswerVerificationFact{{
		Command:        "go test ./pkg --token private-value",
		Succeeded:      true,
		AfterFinalEdit: true,
	}})
	if strings.Contains(got, "private-value") {
		t.Fatalf("separate secret argument leaked into host facts: %q", got)
	}
	if !strings.Contains(got, "--token <redacted>") {
		t.Fatalf("separate secret argument was not redacted: %q", got)
	}
}

func TestAppendFinalAnswerHostFactsRedactsHeadersAndURLCredentials(t *testing.T) {
	got := AppendFinalAnswerHostFacts("已完成验证。", nil, nil, []FinalAnswerVerificationFact{{
		Command:        `go test ./pkg -args "Authorization: Bearer header-secret" https://user:url-secret@example.test/repo`,
		Succeeded:      true,
		AfterFinalEdit: true,
	}})
	for _, leaked := range []string{"header-secret", "url-secret", "user:url-secret"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("credential %q leaked into host facts: %q", leaked, got)
		}
	}
	if !strings.Contains(got, "Authorization: Bearer <redacted>") || !strings.Contains(got, "https://<redacted>@example.test/repo") {
		t.Fatalf("credential redactions malformed: %q", got)
	}
}

func TestAppendFinalAnswerHostFactsRequiresExactPathWhenBasenamesCollide(t *testing.T) {
	got := AppendFinalAnswerHostFacts("已更新 parser.go 的处理。", nil, []string{"cmd/parser.go", "pkg/parser.go"}, nil)
	if !strings.Contains(got, "cmd/parser.go") || !strings.Contains(got, "pkg/parser.go") {
		t.Fatalf("ambiguous basename must not suppress exact file facts: %q", got)
	}
}

func TestAppendFinalAnswerHostFactsDoesNotTreatSubstringAsPathMention(t *testing.T) {
	got := AppendFinalAnswerHostFacts("已调整 prefix.go 的处理。", nil, []string{"fix.go"}, nil)
	if !strings.Contains(got, "更新 `fix.go`") {
		t.Fatalf("substring inside another filename suppressed the real file fact: %q", got)
	}

	summary := "已调整 ./fix.go 的处理。"
	if got := AppendFinalAnswerHostFacts(summary, nil, []string{"fix.go"}, nil); got != summary {
		t.Fatalf("root-relative ./ path mention was repeated:\n%s", got)
	}
}

func TestAppendFinalAnswerHostFactsMatchesWindowsPathSeparator(t *testing.T) {
	summary := `已修复 pkg\fix.go 的 nil 崩溃。`
	got := AppendFinalAnswerHostFacts(summary, nil, []string{"pkg/fix.go"}, nil)
	if got != summary {
		t.Fatalf("windows-style path mention was repeated:\n%s", got)
	}
}

func TestAppendFinalAnswerHostFactsEnglishSummary(t *testing.T) {
	got := AppendFinalAnswerHostFacts(
		"Implemented the parser fix.",
		nil,
		[]string{"parser/parser.go"},
		[]FinalAnswerVerificationFact{{Command: "go test ./parser", Succeeded: true, AfterFinalEdit: true}},
	)
	if !strings.Contains(got, "Actual file changes") || !strings.Contains(got, "Observed verification") || !strings.Contains(got, "passed") {
		t.Fatalf("english host facts malformed: %q", got)
	}
}

// A borrowed Chinese word or a localized path inside a dominant-English
// summary must not flip the fact sentences' language.
func TestAppendFinalAnswerHostFactsChineseSummaryWithLongPathsStaysChinese(t *testing.T) {
	summary := "已修复 internal/parser/parser.go 与 internal/renderer/renderer.go 的崩溃。"
	got := AppendFinalAnswerHostFacts(summary, nil, []string{"internal/other/unmentioned.go"}, nil)
	if !strings.Contains(got, "本轮实际改动") {
		t.Fatalf("Chinese summary with long paths was treated as English: %q", got)
	}
}

func TestAppendFinalAnswerHostFactsEnglishSummaryKeepsLatinWhenHanIsMinor(t *testing.T) {
	got := AppendFinalAnswerHostFacts(
		"Implemented the parser fix; the 中文 sample input now parses too.",
		nil,
		[]string{"parser/parser.go"},
		[]FinalAnswerVerificationFact{{Command: "go test ./parser", Succeeded: true, AfterFinalEdit: true}},
	)
	if !strings.Contains(got, "Actual file changes") || !strings.Contains(got, "Observed verification") {
		t.Fatalf("latin facts were flipped to Chinese: %q", got)
	}
}
