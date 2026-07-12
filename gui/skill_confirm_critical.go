package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

// criticalRiskConfirmResponse is sent on the response channel when the user
// answers the confirmation prompt.
type criticalRiskConfirmResponse struct {
	Confirmed bool
}

// pendingCriticalConfirmEntry holds the state for a single pending confirmation.
//
// LIFECYCLE: The entry is owned by a dedicated cleanup goroutine that fires
// after confirmTimeout. This decouples the confirmation lifecycle from the
// caller's context/goroutine — the key design invariant.
//
// Three actors interact with the entry:
//   - confirmCriticalRiskSkill (creator): stores entry, blocks on Ch
//   - ResolveCriticalConfirm (user click): sends response on Ch, deletes entry
//   - cleanup goroutine (timeout): closes Ch, deletes entry
//
// The resolved flag (atomic CAS) ensures exactly one actor wins the race
// to operate on the channel. The loser returns early without touching Ch.
type pendingCriticalConfirmEntry struct {
	Ch       chan criticalRiskConfirmResponse
	resolved int32 // 0 = pending, 1 = resolved; atomic CAS guards channel ops
}

// tryResolve atomically transitions the entry from pending to resolved.
// Returns true if this caller won the race (and should operate on Ch).
// Returns false if another actor already resolved the entry.
func (e *pendingCriticalConfirmEntry) tryResolve() bool {
	return atomic.CompareAndSwapInt32(&e.resolved, 0, 1)
}

const confirmTimeout = 120 * time.Second

var skillConfirmIDCounter uint64

func nextSkillConfirmID(prefix string) string {
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), atomic.AddUint64(&skillConfirmIDCounter, 1))
}

// buildSkillRiskPrompt formats a confirmation prompt for a risky skill
// installation. It is shared by IM-driven and manual desktop installs.
func buildSkillRiskPrompt(skillName, source string, level security.RiskLevel, factors []string) string {
	return buildSkillRiskPromptForLang("en", skillName, source, level, factors)
}

func buildSkillRiskPromptForLang(lang, skillName, source string, level security.RiskLevel, factors []string) string {
	kind := normalizeSkillConfirmLangKind(lang)
	var sb strings.Builder
	if kind.IsEnglish() {
		fmt.Fprintf(&sb, "Security warning: Skill %q from %s was assessed as %s risk.\n", skillName, source, localizeRiskLevel(kind, level))
	} else {
		fmt.Fprintf(&sb, "%s\n", localizeSkillRiskWarning(kind, skillName, source, level))
	}
	if len(factors) > 0 {
		if kind.IsEnglish() {
			sb.WriteString("\nRisk factors:\n")
		} else if kind == appLanguageZhHant {
			sb.WriteString("\n風險因素：\n")
		} else {
			sb.WriteString("\n风险因素：\n")
		}
		for _, f := range factors {
			fmt.Fprintf(&sb, "  - %s\n", localizeSkillRiskFactor(kind, f))
		}
	}
	if kind.IsEnglish() {
		sb.WriteString("\nDo you want to allow this skill installation?\n")
	} else if kind == appLanguageZhHant {
		sb.WriteString("\n是否允許安裝此 Skill？\n")
	} else {
		sb.WriteString("\n是否允许安装此 Skill？\n")
	}
	return sb.String()
}

func localizeSkillRiskWarning(lang appLanguageKind, skillName, source string, level security.RiskLevel) string {
	if lang == appLanguageZhHant {
		return fmt.Sprintf("安全警告：Skill %q（來源：%s）被評估為%s風險。", skillName, source, localizeRiskLevel(lang, level))
	}
	return fmt.Sprintf("安全警告：Skill %q（来源：%s）被评估为%s风险。", skillName, source, localizeRiskLevel(lang, level))
}

func localizeSkillRiskFactor(lang appLanguageKind, factor string) string {
	factor = strings.TrimSpace(factor)
	if lang.IsEnglish() || factor == "" {
		return factor
	}
	if strings.HasPrefix(factor, "threat pattern [") && strings.HasSuffix(factor, " matched") {
		body := strings.TrimSuffix(strings.TrimPrefix(factor, "threat pattern ["), " matched")
		if idx := strings.Index(body, "]: "); idx >= 0 {
			category := body[:idx]
			pattern := body[idx+3:]
			if lang == appLanguageZhHant {
				return fmt.Sprintf("威脅模式 [%s]：%s 已匹配", localizeRiskCategory(lang, category), pattern)
			}
			return fmt.Sprintf("威胁模式 [%s]：%s 已匹配", localizeRiskCategory(lang, category), pattern)
		}
	}
	if strings.HasPrefix(factor, "community trust level: ") {
		body := strings.TrimPrefix(factor, "community trust level: ")
		parts := strings.Split(body, " escalated to ")
		if len(parts) == 2 {
			from := localizeRiskLevel(lang, security.RiskLevel(strings.TrimSpace(parts[0])))
			to := localizeRiskLevel(lang, security.RiskLevel(strings.TrimSpace(parts[1])))
			if lang == appLanguageZhHant {
				return fmt.Sprintf("社群信任級別：%s升級為%s", from, to)
			}
			return fmt.Sprintf("社区信任级别：%s升级为%s", from, to)
		}
		if lang == appLanguageZhHant {
			return "社群信任級別：" + body
		}
		return "社区信任级别：" + body
	}
	return factor
}

func localizeSkillRiskFactors(lang string, factors []string) []string {
	if len(factors) == 0 {
		return factors
	}
	kind := normalizeSkillConfirmLangKind(lang)
	localized := make([]string, 0, len(factors))
	for _, factor := range factors {
		localized = append(localized, localizeSkillRiskFactor(kind, factor))
	}
	return localized
}

func localizeRiskCategory(lang appLanguageKind, category string) string {
	if lang.IsEnglish() {
		return category
	}
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "injection":
		return "注入"
	default:
		return category
	}
}

func localizeRiskLevel(lang appLanguageKind, level security.RiskLevel) string {
	value := strings.ToLower(strings.TrimSpace(string(level)))
	if lang.IsEnglish() {
		return value
	}
	switch security.RiskLevel(value) {
	case security.RiskLow:
		return "低"
	case security.RiskMedium:
		return "中"
	case security.RiskHigh:
		return "高"
	case security.RiskCritical:
		if lang == appLanguageZhHant {
			return "嚴重"
		}
		return "严重"
	default:
		return strings.TrimSpace(string(level))
	}
}

func normalizeSkillConfirmLangKind(lang string) appLanguageKind {
	if strings.TrimSpace(lang) == "" {
		return appLanguageZhHans
	}
	return normalizeAppLanguageKind(lang)
}

func (h *IMMessageHandler) skillConfirmLang() string {
	if h != nil && h.app != nil {
		return h.app.skillConfirmLang()
	}
	return string(appLanguageZhHans)
}

func (d *CapabilityGapDetector) skillConfirmLang() string {
	if d != nil && d.app != nil {
		return d.app.skillConfirmLang()
	}
	return string(appLanguageZhHans)
}

func (a *App) skillConfirmLang() string {
	if a != nil {
		if cfg, err := a.LoadConfig(); err == nil {
			return normalizeSkillConfirmLangKind(cfg.Language).TranslationTag()
		}
	}
	return string(appLanguageZhHans)
}

func localizedSkillInstallActionLabels(lang string) (confirmLabel, rejectLabel string) {
	switch normalizeSkillConfirmLangKind(lang) {
	case appLanguageEnglish:
		return "Confirm install", "Reject install"
	case appLanguageZhHant:
		return "確認安裝", "拒絕安裝"
	default:
		return "确认安装", "拒绝安装"
	}
}

func localizedSkillInstallResultMessage(lang, skillName string, success bool, errText string) string {
	switch normalizeSkillConfirmLangKind(lang) {
	case appLanguageEnglish:
		if success {
			return fmt.Sprintf("Skill %q installed successfully.", skillName)
		}
		return fmt.Sprintf("Skill %q was not installed successfully: %s", skillName, errText)
	case appLanguageZhHant:
		if success {
			return fmt.Sprintf("Skill「%s」安裝成功。", skillName)
		}
		return fmt.Sprintf("Skill「%s」安裝未成功：%s", skillName, errText)
	default:
		if success {
			return fmt.Sprintf("Skill「%s」安装成功。", skillName)
		}
		return fmt.Sprintf("Skill「%s」安装未成功：%s", skillName, errText)
	}
}

func localizedSkillInstallRejectedMessage(lang, skillName string) string {
	switch normalizeSkillConfirmLangKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("Skill %q was rejected and not installed.", skillName)
	case appLanguageZhHant:
		return fmt.Sprintf("Skill %q 已拒絕安裝。", skillName)
	default:
		return fmt.Sprintf("Skill %q 已拒绝安装。", skillName)
	}
}

func localizedSkillInstallBlockedMessage(lang, skillName string, beforeInstall bool) string {
	switch normalizeSkillConfirmLangKind(lang) {
	case appLanguageEnglish:
		if beforeInstall {
			return fmt.Sprintf("Skill %q was blocked by current security policy before installation.", skillName)
		}
		return fmt.Sprintf("Skill %q was blocked by current security policy and not installed.", skillName)
	case appLanguageZhHant:
		if beforeInstall {
			return fmt.Sprintf("Skill %q 已在安裝前被目前安全策略封鎖。", skillName)
		}
		return fmt.Sprintf("Skill %q 已被目前安全策略封鎖，未安裝。", skillName)
	default:
		if beforeInstall {
			return fmt.Sprintf("Skill %q 已在安装前被当前安全策略阻止。", skillName)
		}
		return fmt.Sprintf("Skill %q 已被当前安全策略阻止，未安装。", skillName)
	}
}

func localizedSkillInstallSuccessSummary(lang, skillName, description, source, trustLevel string) string {
	switch normalizeSkillConfirmLangKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("Skill %q installed successfully\nDescription: %s\nSource: %s\nTrust level: %s\n", skillName, description, source, trustLevel)
	case appLanguageZhHant:
		return fmt.Sprintf("已成功安裝 Skill「%s」\n描述：%s\n來源：%s\n信任等級：%s\n", skillName, description, source, trustLevel)
	default:
		return fmt.Sprintf("已成功安装 Skill「%s」\n描述：%s\n来源：%s\n信任等级：%s\n", skillName, description, source, trustLevel)
	}
}

func localizedSkillInstallAutoRunStarting(lang, skillName string) string {
	switch normalizeSkillConfirmLangKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("\nRunning Skill %q now...\n", skillName)
	case appLanguageZhHant:
		return fmt.Sprintf("\n正在立即執行 Skill「%s」...\n", skillName)
	default:
		return fmt.Sprintf("\n正在立即执行 Skill「%s」...\n", skillName)
	}
}

func localizedSkillInstallRunHint(lang, skillName string) string {
	switch normalizeSkillConfirmLangKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("\nYou can run it with manage_skill(action=\"run\", name=\"%s\")", skillName)
	case appLanguageZhHant:
		return fmt.Sprintf("\n可以使用 manage_skill(action=\"run\", name=\"%s\") 執行", skillName)
	default:
		return fmt.Sprintf("\n可以使用 manage_skill(action=\"run\", name=\"%s\") 执行", skillName)
	}
}

func localizedSkillInstallReviewStatus(lang, summary string) string {
	switch normalizeSkillConfirmLangKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("Waiting for security review confirmation: %s", summary)
	case appLanguageZhHant:
		return fmt.Sprintf("正在等待安全審查確認：%s", summary)
	default:
		return fmt.Sprintf("正在等待安全审查确认：%s", summary)
	}
}

func localizedSkillInstallNoConfirmationStatus(lang, skillName string) string {
	switch normalizeSkillConfirmLangKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("No confirmation channel available; current policy records and allows Skill %s.", skillName)
	case appLanguageZhHant:
		return fmt.Sprintf("沒有可用的確認通道；目前策略將記錄並允許 Skill %s。", skillName)
	default:
		return fmt.Sprintf("没有可用的确认通道；当前策略将记录并允许 Skill %s。", skillName)
	}
}

func localizedSkillInstallScanRejectedError(lang string, github bool) string {
	switch normalizeSkillConfirmLangKind(lang) {
	case appLanguageEnglish:
		if github {
			return "GitHub Skill security review did not pass; automatic installation was rejected"
		}
		return "Skill security review did not pass; automatic installation was rejected"
	case appLanguageZhHant:
		if github {
			return "GitHub Skill 安全審查未通過，已拒絕自動安裝"
		}
		return "Skill 安全審查未通過，已拒絕自動安裝"
	default:
		if github {
			return "GitHub Skill 安全审查未通过，已拒绝自动安装"
		}
		return "Skill 安全审查未通过，已拒绝自动安装"
	}
}

func localizedSkillInstallInstallingStatus(lang, skillName string, github bool) string {
	switch normalizeSkillConfirmLangKind(lang) {
	case appLanguageEnglish:
		if github {
			return fmt.Sprintf("Installing Skill from GitHub: %s ...", skillName)
		}
		return fmt.Sprintf("Installing Skill: %s ...", skillName)
	case appLanguageZhHant:
		if github {
			return fmt.Sprintf("正在從 GitHub 安裝 Skill：%s ...", skillName)
		}
		return fmt.Sprintf("正在安裝 Skill：%s ...", skillName)
	default:
		if github {
			return fmt.Sprintf("正在从 GitHub 安装 Skill：%s ...", skillName)
		}
		return fmt.Sprintf("正在安装 Skill：%s ...", skillName)
	}
}

func localizedSkillInstallExecutingStatus(lang, skillName string) string {
	switch normalizeSkillConfirmLangKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("Running Skill: %s ...", skillName)
	case appLanguageZhHant:
		return fmt.Sprintf("正在執行 Skill：%s ...", skillName)
	default:
		return fmt.Sprintf("正在执行 Skill：%s ...", skillName)
	}
}

// buildCriticalRiskPrompt formats a confirmation prompt for a Critical-risk
// skill installation.
func buildCriticalRiskPrompt(skillName, source string, factors []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "安全警告: Skill「%s」来自 %s 被评估为 Critical 风险。\n", skillName, source)
	if len(factors) > 0 {
		sb.WriteString("\n风险因素:\n")
		for _, f := range factors {
			fmt.Fprintf(&sb, "  • %s\n", f)
		}
	}
	sb.WriteString("\n确认安装此 Skill？\n")
	return sb.String()
}

// confirmCriticalRiskSkill presents a blocking confirmation prompt to the user
// when a skill is assessed as RiskCritical. Returns true if the user confirms,
// false on rejection, timeout, or any error condition (fail-closed).
//
// LIFECYCLE DESIGN:
// The confirmation channel's lifetime is decoupled from the caller's context.
// A single cleanup goroutine owns the timeout: after confirmTimeout it closes
// the channel and removes the entry. The caller's context cancellation causes
// this function to return false, but does NOT remove the channel — the user
// can still click the button within the confirmTimeout window.
//
// CONCURRENCY: Two actors race to resolve the entry — the cleanup goroutine
// (timeout) and ResolveCriticalConfirm (user click). atomic CAS on
// entry.resolved ensures exactly one wins. The loser never touches the channel.
func (h *IMMessageHandler) confirmCriticalRiskSkill(
	ctx context.Context,
	skillName, source string,
	factors []string,
	platform string,
	userID string,
) bool {
	return h.confirmRiskSkillInstall(ctx, skillName, source, security.RiskCritical, factors, platform, userID)
}

func (h *IMMessageHandler) confirmRiskSkillInstall(
	ctx context.Context,
	skillName, source string,
	level security.RiskLevel,
	factors []string,
	platform string,
	userID string,
) bool {
	if platform == "" {
		log.Printf("[critical-confirm] fail-closed: empty platform for skill %q", skillName)
		return false
	}

	confirmID := nextSkillConfirmID("crit")
	lang := h.skillConfirmLang()
	promptText := buildSkillRiskPromptForLang(lang, skillName, source, level, factors)
	confirmLabel, rejectLabel := localizedSkillInstallActionLabels(lang)

	entry := &pendingCriticalConfirmEntry{
		Ch: make(chan criticalRiskConfirmResponse, 1),
	}
	h.pendingCriticalConfirm.Store(confirmID, entry)

	// Single cleanup goroutine — the sole timeout owner.
	// After confirmTimeout it tries to win the resolve race. If it wins,
	// it closes the channel (unblocking the select below) and removes the entry.
	// If ResolveCriticalConfirm already won, this is a no-op.
	go func() {
		time.Sleep(confirmTimeout)
		if entry.tryResolve() {
			h.pendingCriticalConfirm.Delete(confirmID)
			close(entry.Ch)
			log.Printf("[critical-confirm] cleanup: confirmID=%s expired after %v", confirmID, confirmTimeout)
		}
	}()

	// Dispatch confirmation UI.
	switch {
	case normalizeIMMessagePlatformKind(platform).IsDesktop():
		if h.app != nil {
			payload := map[string]interface{}{
				"confirm_id": confirmID,
				"summary":    promptText,
				"lang":       lang,
				"actions": []map[string]string{
					{"label": confirmLabel, "command": "confirm"},
					{"label": rejectLabel, "command": "reject"},
				},
			}
			h.app.emitEvent("critical-risk-confirm", payload)
			log.Printf("[critical-confirm] desktop event emitted confirm_id=%s skill=%q", confirmID, skillName)
		} else {
			log.Printf("[critical-confirm] fail-closed: app is nil for desktop platform, skill %q", skillName)
			return false
		}

	default:
		displayText := FormatAskUserForDisplay(&AskUserRequest{
			Question:  promptText,
			Options:   []string{confirmLabel, rejectLabel},
			InputType: askUserInputConfirm.String(),
		})
		if h.app != nil {
			imConfirmKey := platform + ":" + userID
			h.pendingCriticalConfirmIM.Store(imConfirmKey, confirmID)
			hubClient := h.app.hubClient()
			if hubClient != nil {
				if err := hubClient.SendIMProactiveMessage(displayText); err != nil {
					log.Printf("[critical-confirm] failed to send proactive message for confirm_id=%s: %v", confirmID, err)
				}
			}
			log.Printf("[critical-confirm] IM proactive message sent confirm_id=%s platform=%s skill=%q", confirmID, platform, skillName)
		} else {
			log.Printf("[critical-confirm] fail-closed: app is nil for IM platform %s, skill %q", platform, skillName)
			return false
		}
	}

	// Block until: user responds, channel closed (timeout), or caller cancelled.
	select {
	case resp, ok := <-entry.Ch:
		if !ok {
			// Channel closed by cleanup goroutine — timeout.
			log.Printf("[critical-confirm] timeout confirm_id=%s skill=%q", confirmID, skillName)
			return false
		}
		log.Printf("[critical-confirm] received response confirm_id=%s confirmed=%v", confirmID, resp.Confirmed)
		return resp.Confirmed

	case <-ctx.Done():
		// Caller's context cancelled — return false but do NOT clean up.
		// The cleanup goroutine owns the entry's lifetime.
		log.Printf("[critical-confirm] context cancelled confirm_id=%s skill=%q — channel stays alive for user", confirmID, skillName)
		return false
	}
}

// ResolveCriticalConfirm is called by the frontend or IM gateway when the user
// responds to a critical-risk confirmation prompt.
// Returns an error if the confirmation has expired or was already resolved.
func (h *IMMessageHandler) ResolveCriticalConfirm(confirmID string, confirmed bool) error {
	v, ok := h.pendingCriticalConfirm.Load(confirmID)
	if !ok {
		log.Printf("[critical-confirm] resolve: confirmID=%s not found (expired or already resolved)", confirmID)
		return fmt.Errorf("确认已过期或已处理，请重新安装")
	}
	entry, ok := v.(*pendingCriticalConfirmEntry)
	if !ok {
		log.Printf("[critical-confirm] resolve: confirmID=%s has unexpected entry type", confirmID)
		return fmt.Errorf("内部错误：确认状态异常")
	}

	// Try to win the resolve race against the cleanup goroutine.
	if !entry.tryResolve() {
		// Cleanup goroutine already won — the channel is closed or being closed.
		log.Printf("[critical-confirm] resolve: confirmID=%s already resolved (timeout race)", confirmID)
		return fmt.Errorf("确认已超时，请重新安装")
	}

	// We won the race — safe to send on the channel (it's not closed).
	h.pendingCriticalConfirm.Delete(confirmID)
	entry.Ch <- criticalRiskConfirmResponse{Confirmed: confirmed}
	log.Printf("[critical-confirm] resolve: confirmID=%s confirmed=%v", confirmID, confirmed)
	return nil
}
