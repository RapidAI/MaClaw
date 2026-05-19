package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

func TestBuildSkillRiskPromptForLang_LocalizesChinesePrompt(t *testing.T) {
	t.Parallel()

	prompt := buildSkillRiskPromptForLang("zh-Hans", "ui-ux-pro-max-skill", "auto_clawhub", security.RiskCritical, []string{
		"threat pattern [injection]: \"\\\\|\\\\s*(bash|sh|python|perl)\" matched",
		"community trust level: high escalated to critical",
	})

	for _, want := range []string{"安全警告", "严重风险", "风险因素", "威胁模式 [注入]", "社区信任级别", "高升级为严重", "是否允许安装此 Skill"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("localized prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, notWant := range []string{"Security warning", "Risk factors", "Do you want to allow", "critical risk"} {
		if strings.Contains(prompt, notWant) {
			t.Fatalf("localized prompt still contains English %q:\n%s", notWant, prompt)
		}
	}
}

func TestBuildSkillRiskPromptForLang_LocalizesTraditionalChinesePrompt(t *testing.T) {
	t.Parallel()

	prompt := buildSkillRiskPromptForLang("zh-Hant", "ui-ux-pro-max-skill", "auto_clawhub", security.RiskCritical, []string{
		"threat pattern [injection]: \"\\\\|\\\\s*(bash|sh|python|perl)\" matched",
		"community trust level: high escalated to critical",
	})

	for _, want := range []string{"安全警告", "嚴重風險", "風險因素", "威脅模式 [注入]", "社群信任級別", "高升級為嚴重", "是否允許安裝此 Skill"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("traditional localized prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, notWant := range []string{"Security warning", "Risk factors", "Do you want to allow", "严重风险", "风险因素"} {
		if strings.Contains(prompt, notWant) {
			t.Fatalf("traditional localized prompt contains unexpected text %q:\n%s", notWant, prompt)
		}
	}
}

func TestBuildSkillRiskPromptForLang_DefaultsEmptyLanguageToChinese(t *testing.T) {
	t.Parallel()

	prompt := buildSkillRiskPromptForLang("", "skill", "source", security.RiskHigh, nil)
	if strings.Contains(prompt, "Security warning") {
		t.Fatalf("empty language should not default to English:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Skill") {
		t.Fatalf("prompt missing skill marker:\n%s", prompt)
	}
}

func TestSkillInstallLocalizationHelpers(t *testing.T) {
	t.Parallel()

	if got := normalizeSkillConfirmLangKind(""); got != appLanguageZhHans {
		t.Fatalf("empty language = %q, want %q", got, appLanguageZhHans)
	}
	if got := normalizeSkillConfirmLangKind("zh-TW"); got != appLanguageZhHant {
		t.Fatalf("zh-TW language = %q, want %q", got, appLanguageZhHant)
	}

	confirm, reject := localizedSkillInstallActionLabels("zh-Hant")
	if confirm != "確認安裝" || reject != "拒絕安裝" {
		t.Fatalf("zh-Hant labels = %q/%q", confirm, reject)
	}
	confirm, reject = localizedSkillInstallActionLabels("")
	if confirm != "确认安装" || reject != "拒绝安装" {
		t.Fatalf("empty language labels = %q/%q", confirm, reject)
	}
}

func TestSkillInstallResultMessages_Localized(t *testing.T) {
	t.Parallel()

	checks := []struct {
		lang    string
		success bool
		errText string
		want    string
	}{
		{lang: "en", success: true, want: "installed successfully"},
		{lang: "en", success: false, errText: "scan failed", want: "was not installed successfully: scan failed"},
		{lang: "zh-Hans", success: true, want: "安装成功"},
		{lang: "zh-Hans", success: false, errText: "扫描失败", want: "安装未成功：扫描失败"},
		{lang: "zh-Hant", success: true, want: "安裝成功"},
		{lang: "zh-Hant", success: false, errText: "掃描失敗", want: "安裝未成功：掃描失敗"},
	}
	for _, tt := range checks {
		msg := localizedSkillInstallResultMessage(tt.lang, "demo", tt.success, tt.errText)
		if !strings.Contains(msg, tt.want) {
			t.Fatalf("localized result message for %s/%v = %q, want substring %q", tt.lang, tt.success, msg, tt.want)
		}
	}

	if got := localizedSkillInstallRejectedMessage("en", "demo"); !strings.Contains(got, "rejected and not installed") {
		t.Fatalf("English rejected message = %q", got)
	}
	if got := localizedSkillInstallRejectedMessage("zh-Hant", "demo"); !strings.Contains(got, "已拒絕安裝") {
		t.Fatalf("Traditional rejected message = %q", got)
	}
}

func TestSkillInstallStatusMessages_Localized(t *testing.T) {
	t.Parallel()

	blocked := localizedSkillInstallBlockedMessage("zh-Hant", "demo", true)
	if !strings.Contains(blocked, "安裝前") || !strings.Contains(blocked, "安全策略") {
		t.Fatalf("Traditional blocked message = %q", blocked)
	}
	blocked = localizedSkillInstallBlockedMessage("en", "demo", false)
	if !strings.Contains(blocked, "blocked by current security policy") {
		t.Fatalf("English blocked message = %q", blocked)
	}

	summary := localizedSkillInstallSuccessSummary("zh-Hans", "demo", "desc", "hub", "high")
	for _, want := range []string{"已成功安装", "描述：desc", "来源：hub", "信任等级：high"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("Simplified success summary missing %q: %q", want, summary)
		}
	}
	if got := localizedSkillInstallAutoRunStarting("en", "demo"); !strings.Contains(got, "Running Skill") {
		t.Fatalf("English auto-run message = %q", got)
	}
	if got := localizedSkillInstallRunHint("zh-Hant", "demo"); !strings.Contains(got, "執行") {
		t.Fatalf("Traditional run hint = %q", got)
	}
}

func TestSkillInstallCapabilityGapMessages_Localized(t *testing.T) {
	t.Parallel()

	review := localizedSkillInstallReviewStatus("zh-Hant", "critical")
	if !strings.Contains(review, "安全審查") || !strings.Contains(review, "critical") {
		t.Fatalf("Traditional review status = %q", review)
	}
	noConfirm := localizedSkillInstallNoConfirmationStatus("zh-Hans", "demo")
	if !strings.Contains(noConfirm, "没有可用的确认通道") {
		t.Fatalf("Simplified no-confirm status = %q", noConfirm)
	}
	if got := localizedSkillInstallScanRejectedError("en", true); !strings.Contains(got, "GitHub Skill security review did not pass") {
		t.Fatalf("English rejected error = %q", got)
	}
	installing := localizedSkillInstallInstallingStatus("zh-Hant", "demo", true)
	if !strings.Contains(installing, "正在從 GitHub 安裝") {
		t.Fatalf("Traditional GitHub installing status = %q", installing)
	}
	executing := localizedSkillInstallExecutingStatus("en", "demo")
	if !strings.Contains(executing, "Running Skill") {
		t.Fatalf("English executing status = %q", executing)
	}
}

func TestNextSkillConfirmID_IsUniqueAcrossConcurrentCalls(t *testing.T) {
	t.Parallel()

	const total = 128
	ids := make(map[string]struct{}, total)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := nextSkillConfirmID("skill_install")
			if !strings.HasPrefix(id, "skill_install_") {
				t.Errorf("confirm ID %q missing prefix", id)
			}
			mu.Lock()
			defer mu.Unlock()
			if _, exists := ids[id]; exists {
				t.Errorf("duplicate confirm ID generated: %s", id)
			}
			ids[id] = struct{}{}
		}()
	}
	wg.Wait()
	if len(ids) != total {
		t.Fatalf("generated %d unique IDs, want %d", len(ids), total)
	}
}

func waitForPendingCriticalConfirm(t *testing.T, h *IMMessageHandler) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var confirmID string
		h.pendingCriticalConfirm.Range(func(key, value interface{}) bool {
			confirmID = key.(string)
			return false
		})
		if confirmID != "" {
			return confirmID
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no pending confirmation found")
	return ""
}

// TestConfirmCriticalRisk_FailClosedEmptyPlatform verifies that calling
// confirmCriticalRiskSkill with an empty platform returns false immediately
// (fail-closed behavior).
func TestConfirmCriticalRisk_FailClosedEmptyPlatform(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	result := h.confirmCriticalRiskSkill(
		context.Background(),
		"dangerous-skill", "https://hub.example.com",
		[]string{"rm -rf found"}, "", "",
	)
	if result {
		t.Fatal("expected false for empty platform (fail-closed), got true")
	}
}

// TestConfirmCriticalRisk_TimeoutReturnsFalse verifies that when no response
// is sent on the channel, the function returns false after the timeout.
// We test the channel-close mechanism directly with a short timer to avoid
// waiting 120s.
func TestConfirmCriticalRisk_TimeoutReturnsFalse(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	// Create a pending entry (same type as confirmCriticalRiskSkill uses).
	entry := &pendingCriticalConfirmEntry{
		Ch: make(chan criticalRiskConfirmResponse, 1),
	}
	confirmID := "test_timeout_1"
	h.pendingCriticalConfirm.Store(confirmID, entry)

	// Simulate cleanup goroutine closing the channel after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		if entry.tryResolve() {
			h.pendingCriticalConfirm.Delete(confirmID)
			close(entry.Ch)
		}
	}()

	// Read from channel — should get ok=false (closed).
	select {
	case _, ok := <-entry.Ch:
		if ok {
			t.Fatal("expected channel close (ok=false), got a value")
		}
		// ok=false — timeout path, correct.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}

// TestConfirmCriticalRisk_ContextCancellation verifies that cancelling the
// context causes confirmCriticalRiskSkill to return false.
func TestConfirmCriticalRisk_ContextCancellation(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context after a short delay so the function unblocks.
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	result := h.confirmCriticalRiskSkill(
		ctx,
		"dangerous-skill", "https://hub.example.com",
		[]string{"rm -rf found"}, "desktop", "user1",
	)
	if result {
		t.Fatal("expected false on context cancellation, got true")
	}
}

// TestConfirmCriticalRisk_DesktopChannelAdaptation verifies that calling with
// platform="desktop" stores a pending confirmation entry.
func TestConfirmCriticalRisk_DesktopChannelAdaptation(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		result := h.confirmCriticalRiskSkill(
			ctx,
			"desktop-skill", "https://hub.example.com",
			[]string{"network access"}, "desktop", "user1",
		)
		done <- result
	}()

	confirmIDFound := waitForPendingCriticalConfirm(t, h)
	if !strings.HasPrefix(confirmIDFound, "crit_") {
		t.Fatalf("pending confirmation ID = %q, want crit_ prefix", confirmIDFound)
	}

	cancel()
	<-done
}

// TestConfirmCriticalRisk_IMChannelAdaptation verifies that calling with an
// IM platform stores a pending confirmation entry.
func TestConfirmCriticalRisk_IMChannelAdaptation(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		result := h.confirmCriticalRiskSkill(
			ctx,
			"im-skill", "clawhub",
			[]string{"shell access"}, "feishu", "user1",
		)
		done <- result
	}()

	confirmIDFound := waitForPendingCriticalConfirm(t, h)
	if !strings.HasPrefix(confirmIDFound, "crit_") {
		t.Fatalf("pending confirmation ID = %q, want crit_ prefix", confirmIDFound)
	}

	cancel()
	<-done
}

// TestConfirmCriticalRisk_ConfirmResponse verifies that resolving with
// confirmed=true causes confirmCriticalRiskSkill to return true.
func TestConfirmCriticalRisk_ConfirmResponse(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	ctx := context.Background()
	done := make(chan bool, 1)

	go func() {
		result := h.confirmCriticalRiskSkill(
			ctx,
			"good-skill", "https://hub.example.com",
			[]string{"network access"}, "desktop", "user1",
		)
		done <- result
	}()

	confirmID := waitForPendingCriticalConfirm(t, h)

	if err := h.ResolveCriticalConfirm(confirmID, true); err != nil {
		t.Fatalf("ResolveCriticalConfirm returned error: %v", err)
	}

	result := <-done
	if !result {
		t.Fatal("expected true when user confirms, got false")
	}
}

// TestConfirmCriticalRisk_RejectResponse verifies that resolving with
// confirmed=false causes confirmCriticalRiskSkill to return false.
func TestConfirmCriticalRisk_RejectResponse(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	ctx := context.Background()
	done := make(chan bool, 1)

	go func() {
		result := h.confirmCriticalRiskSkill(
			ctx,
			"bad-skill", "https://github.com/user/repo",
			[]string{"dangerous keyword"}, "feishu", "user1",
		)
		done <- result
	}()

	confirmID := waitForPendingCriticalConfirm(t, h)

	if err := h.ResolveCriticalConfirm(confirmID, false); err != nil {
		t.Fatalf("ResolveCriticalConfirm returned error: %v", err)
	}

	result := <-done
	if result {
		t.Fatal("expected false when user rejects, got true")
	}
}

// TestResolveCriticalConfirm_UnknownID verifies that calling
// ResolveCriticalConfirm with a non-existent ID returns an error.
func TestResolveCriticalConfirm_UnknownID(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	err := h.ResolveCriticalConfirm("nonexistent-id", true)
	if err == nil {
		t.Fatal("expected error for unknown confirmID, got nil")
	}
	err = h.ResolveCriticalConfirm("nonexistent-id", false)
	if err == nil {
		t.Fatal("expected error for unknown confirmID, got nil")
	}
}

// TestConfirmCriticalRisk_NilAppDesktop verifies fail-closed when app is nil
// on desktop platform.
func TestConfirmCriticalRisk_NilAppDesktop(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{} // app is nil

	result := h.confirmCriticalRiskSkill(
		context.Background(),
		"skill", "source",
		[]string{"factor"}, "desktop", "",
	)
	if result {
		t.Fatal("expected false when app is nil (fail-closed), got true")
	}
}

// TestConfirmCriticalRisk_NilAppIM verifies fail-closed when app is nil
// on IM platform.
func TestConfirmCriticalRisk_NilAppIM(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{} // app is nil

	result := h.confirmCriticalRiskSkill(
		context.Background(),
		"skill", "source",
		[]string{"factor"}, "feishu", "",
	)
	if result {
		t.Fatal("expected false when app is nil on IM (fail-closed), got true")
	}
}

// TestConfirmCriticalRisk_ConcurrentConfirmations verifies that multiple
// concurrent confirmations with different IDs don't interfere.
func TestConfirmCriticalRisk_ConcurrentConfirmations(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	// Create 3 pending entries with known IDs.
	ids := []string{"crit_concurrent_1", "crit_concurrent_2", "crit_concurrent_3"}
	entries := make([]*pendingCriticalConfirmEntry, 3)
	for i, id := range ids {
		entry := &pendingCriticalConfirmEntry{
			Ch: make(chan criticalRiskConfirmResponse, 1),
		}
		entries[i] = entry
		h.pendingCriticalConfirm.Store(id, entry)
	}

	// Resolve: first=true, second=false, third=true.
	expected := []bool{true, false, true}
	for i, id := range ids {
		if err := h.ResolveCriticalConfirm(id, expected[i]); err != nil {
			t.Fatalf("ResolveCriticalConfirm(%s) returned error: %v", id, err)
		}
	}

	// Verify each channel received the correct response.
	for i, entry := range entries {
		select {
		case resp := <-entry.Ch:
			if resp.Confirmed != expected[i] {
				t.Errorf("entry %d: expected confirmed=%v, got %v", i, expected[i], resp.Confirmed)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for entry %d", i)
		}
	}
}

// TestResolveCriticalConfirm_DoubleResolve verifies that resolving the same
// confirmID twice returns an error on the second call (CAS prevents double-send).
func TestResolveCriticalConfirm_DoubleResolve(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	entry := &pendingCriticalConfirmEntry{
		Ch: make(chan criticalRiskConfirmResponse, 1),
	}
	confirmID := "test_double_resolve"
	h.pendingCriticalConfirm.Store(confirmID, entry)

	// First resolve should succeed.
	if err := h.ResolveCriticalConfirm(confirmID, true); err != nil {
		t.Fatalf("first resolve returned error: %v", err)
	}

	// Second resolve should fail — entry was deleted by the first resolve.
	err := h.ResolveCriticalConfirm(confirmID, true)
	if err == nil {
		t.Fatal("expected error on second resolve, got nil")
	}
}
