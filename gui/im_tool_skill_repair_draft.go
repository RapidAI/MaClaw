package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// This file implements the human-reviewed repair-draft flow for file-backed
// skills (P0-4 of the skill self-evolution fix plan). The evolution pipeline
// never auto-applies repairs to file-backed skills; it writes a draft under
// <skill_dir>/.evolution-drafts/ and emits skill:repair_draft_ready. The
// manage_skill actions below list, apply (write back skill.yaml + config) or
// reject those drafts.

// repairDraftStepsSummary is a compact, LLM/UI-friendly view of a step list.
type repairDraftStepsSummary struct {
	Count   int      `json:"count"`
	Actions []string `json:"actions,omitempty"`
}

func summarizeRepairDraftSteps(steps []corelib.NLSkillStep) repairDraftStepsSummary {
	out := repairDraftStepsSummary{Count: len(steps)}
	for _, s := range steps {
		action := strings.TrimSpace(s.Action)
		if action == "" {
			continue
		}
		out.Actions = append(out.Actions, action)
	}
	return out
}

// repairDraftStepView is the frontend contract for one step inside a listed
// repair draft: action + params (passed through untruncated) plus the
// advanced fields apply will write back verbatim, so reviewers can see them.
type repairDraftStepView struct {
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
	OnError   string                 `json:"on_error"`
	Name      string                 `json:"name,omitempty"`
	Label     string                 `json:"label,omitempty"`
	When      string                 `json:"when,omitempty"`
	Capture   map[string]string      `json:"capture,omitempty"`
	Condition string                 `json:"condition,omitempty"`
}

func repairDraftStepViews(steps []corelib.NLSkillStep) []repairDraftStepView {
	out := make([]repairDraftStepView, 0, len(steps))
	for _, s := range steps {
		out = append(out, repairDraftStepView{
			Action:    s.Action,
			Params:    s.Params,
			OnError:   s.OnError,
			Name:      s.Name,
			Label:     s.Label,
			When:      s.When,
			Capture:   s.Capture,
			Condition: s.Condition,
		})
	}
	return out
}

// sameRepairDraftSteps reports whether two step lists are semantically equal.
// Comparison goes through canonical JSON so numeric params surviving the
// draft's JSON round-trip (int → float64) don't register as false mismatches.
// Both sides are normalized like the scanner does (empty on_error → "stop",
// nil params → empty map) so a draft captured before/without hydration and a
// fresh disk re-parse don't falsely conflict over representation defaults.
func sameRepairDraftSteps(a, b []corelib.NLSkillStep) bool {
	normalize := func(steps []corelib.NLSkillStep) []corelib.NLSkillStep {
		out := make([]corelib.NLSkillStep, len(steps))
		for i, s := range steps {
			if s.OnError == "" {
				s.OnError = "stop"
			}
			if s.Params == nil {
				s.Params = map[string]interface{}{}
			}
			out[i] = s
		}
		return out
	}
	aj, _ := json.Marshal(normalize(a))
	bj, _ := json.Marshal(normalize(b))
	return string(aj) == string(bj)
}

// truncateRepairExplanation caps the explanation embedded into a LastError
// artifact marker so the marker stays a compact one-liner.
func truncateRepairExplanation(s string) string {
	const max = 200
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "..."
}

// findFileBackedSkillEntry loads the skill list and returns the entry matching
// name when it exists and is file-backed (source=file + skill_dir). The
// returned entry is a deep copy safe to mutate.
func findFileBackedSkillEntry(app *App, name string) (*corelib.NLSkillEntry, string) {
	if app == nil {
		return nil, "app not initialized"
	}
	app.ensureInteractionInfra()
	if app.skillExecutor == nil {
		return nil, "skill executor not initialized"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "name is required"
	}
	skills := app.skillExecutor.loadSkills()
	for i := range skills {
		if !skills[i].MatchesName(name) {
			continue
		}
		entry := cskill.CloneNLSkillEntry(&skills[i])
		if !cskill.IsFileBackedSkill(*entry) {
			return nil, fmt.Sprintf("skill %q is not file-backed; repair drafts only apply to file-backed skills", entry.Name)
		}
		return entry, ""
	}
	return nil, fmt.Sprintf("skill %q not found", name)
}

// resolveRepairDraftPath validates that draftName refers to a *.json file
// directly inside <skillDir>/.evolution-drafts/ and returns its absolute path.
// Any path traversal (../, absolute paths, separators) is rejected.
func resolveRepairDraftPath(skillDir, draftName string) (string, error) {
	draftName = strings.TrimSpace(draftName)
	if draftName == "" {
		return "", fmt.Errorf("draft file name is required")
	}
	if filepath.Base(draftName) != draftName || strings.Contains(draftName, "..") ||
		strings.ContainsAny(draftName, `/\`) || filepath.IsAbs(draftName) {
		return "", fmt.Errorf("invalid draft file name %q: must be a plain file name inside %s", draftName, cskill.RepairDraftsDirName)
	}
	if !strings.HasSuffix(strings.ToLower(draftName), ".json") {
		return "", fmt.Errorf("invalid draft file name %q: must end with .json", draftName)
	}
	dir := filepath.Join(strings.TrimSpace(skillDir), cskill.RepairDraftsDirName)
	path := filepath.Join(dir, draftName)
	// Defense in depth: the resolved path must stay inside the drafts dir.
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("draft path %q escapes %s", draftName, cskill.RepairDraftsDirName)
	}
	return path, nil
}

func readRepairDraftFile(path string) (cskill.RepairDraft, error) {
	var draft cskill.RepairDraft
	data, err := os.ReadFile(path)
	if err != nil {
		return draft, err
	}
	if err := json.Unmarshal(data, &draft); err != nil {
		return draft, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return draft, nil
}

// toolListSkillRepairDrafts lists pending repair drafts for file-backed skills.
// Args: name (optional skill name filter).
func (h *IMMessageHandler) toolListSkillRepairDrafts(args map[string]interface{}) string {
	if h == nil || h.app == nil {
		return `{"ok":false,"error":"app not initialized"}`
	}
	h.app.ensureInteractionInfra()
	if h.app.skillExecutor == nil {
		return `{"ok":false,"error":"skill executor not initialized"}`
	}
	filter := strings.TrimSpace(stringVal(args, "name"))
	if filter == "" {
		filter = strings.TrimSpace(stringVal(args, "skill"))
	}

	type draftItem struct {
		Skill       string `json:"skill"`
		Draft       string `json:"draft"`
		Explanation string `json:"explanation,omitempty"`
		LastError   string `json:"last_error,omitempty"`
		CreatedAt   string `json:"created_at,omitempty"`
		// Unreadable marks draft files that failed to parse; they are still
		// listed (without steps) so the user can reject them.
		Unreadable bool `json:"unreadable,omitempty"`
		// Disable marks "disable suggestion" drafts (apply sets the skill
		// status to disabled instead of writing steps back).
		Disable bool `json:"disable,omitempty"`
		// Full step arrays (frontend contract), omitted for unreadable drafts.
		OldSteps []repairDraftStepView `json:"old_steps,omitempty"`
		NewSteps []repairDraftStepView `json:"new_steps,omitempty"`
		// Compact count+actions summaries kept for backward compatibility.
		OldStepsSummary repairDraftStepsSummary `json:"old_steps_summary"`
		NewStepsSummary repairDraftStepsSummary `json:"new_steps_summary"`
	}
	items := make([]draftItem, 0)
	skills := h.app.skillExecutor.loadSkills()
	for i := range skills {
		entry := skills[i]
		if !cskill.IsFileBackedSkill(entry) {
			continue
		}
		if filter != "" && !entry.MatchesName(filter) {
			continue
		}
		dir := filepath.Join(entry.SkillDir, cskill.RepairDraftsDirName)
		files, err := os.ReadDir(dir)
		if err != nil {
			continue // no drafts dir — nothing pending
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(strings.ToLower(f.Name()), ".json") {
				continue
			}
			draft, err := readRepairDraftFile(filepath.Join(dir, f.Name()))
			if err != nil {
				// Unreadable drafts must stay visible so the user can reject
				// them; skill ownership comes from the directory, not the file.
				log.Printf("[skill-repair-draft] list unreadable draft %s/%s: %v", entry.Name, f.Name(), err)
				items = append(items, draftItem{
					Skill:      entry.Name,
					Draft:      f.Name(),
					Unreadable: true,
				})
				continue
			}
			// skill is always the owning entry name (directory ownership),
			// never the draft file's self-declared skill field.
			items = append(items, draftItem{
				Skill:           entry.Name,
				Draft:           f.Name(),
				Explanation:     draft.Explanation,
				LastError:       draft.LastError,
				CreatedAt:       draft.CreatedAt,
				Disable:         draft.Disable,
				OldSteps:        repairDraftStepViews(draft.OldSteps),
				NewSteps:        repairDraftStepViews(draft.NewSteps),
				OldStepsSummary: summarizeRepairDraftSteps(draft.OldSteps),
				NewStepsSummary: summarizeRepairDraftSteps(draft.NewSteps),
			})
		}
	}
	payload := map[string]interface{}{
		"ok":            true,
		"non_executing": true,
		"boundary":      "read-only repair draft listing",
		"count":         len(items),
		"drafts":        items,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("List repair drafts marshal failed: %v", err)
	}
	return string(data)
}

// skillYAMLPath returns the skill.yaml / skill.yml path inside skillDir, or ""
// when neither exists (SKILL.md-only skills have no machine-applicable steps
// file).
func skillYAMLPath(skillDir string) string {
	for _, name := range []string{"skill.yaml", "skill.yml"} {
		p := filepath.Join(skillDir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// recordReviewedDraftRepair mirrors corelib recordRepairAttempt for the
// human-reviewed draft channel: bumps the attempt counter (so max_attempts
// also gates the draft flow, including repeated rejections) and appends a
// history row tagged with the given via marker. On a successful apply it also
// rewrites LastError into a repair artifact marker (aligned with the
// automatic ApplyRepair path) so failure notifications queued before the
// apply don't regenerate drafts from the stale error.
func recordReviewedDraftRepair(skill *corelib.NLSkillEntry, draft cskill.RepairDraft, via string, success bool) {
	skill.RepairAttemptCount++
	skill.LastRepairAt = time.Now().Format(time.RFC3339)
	skill.RepairHistory = append(skill.RepairHistory, corelib.SkillRepairRecord{
		Timestamp:   skill.LastRepairAt,
		ErrorClass:  cskill.ExtractErrorClass(draft.LastError),
		Explanation: draft.Explanation,
		Success:     success,
		Via:         via,
	})
	if len(skill.RepairHistory) > 5 {
		skill.RepairHistory = skill.RepairHistory[len(skill.RepairHistory)-5:]
	}
	if success {
		prefix := "auto-repaired: "
		if draft.Disable {
			prefix = "auto-disabled: "
		}
		skill.LastError = prefix + truncateRepairExplanation(draft.Explanation)
	}
}

// recordRejectedDraftAttempt bumps only the attempt counter for a rejected
// draft whose file could not be parsed — no history row is possible without
// the draft content, but the counter must still advance so max_attempts
// eventually gates the LLM cost loop.
func recordRejectedDraftAttempt(skill *corelib.NLSkillEntry) {
	skill.RepairAttemptCount++
	skill.LastRepairAt = time.Now().Format(time.RFC3339)
}

// saveRepairDraftSkills is indirected so tests can simulate a config persist
// failure and verify the skill.yaml rollback in toolApplySkillRepairDraft.
var saveRepairDraftSkills = func(exec *SkillExecutor, skills []corelib.NLSkillEntry) error {
	return exec.saveSkills(skills)
}

// toolApplySkillRepairDraft applies a reviewed repair draft: validates the
// draft steps, writes them back to skill.yaml (hard gate — the yaml file is
// the only durable store for file-backed skills), persists the thin config
// overlay with repair counters, deletes the draft file, records an audit row
// and notifies the frontend.
// Args: name (required), draft (required file name).
func (h *IMMessageHandler) toolApplySkillRepairDraft(args map[string]interface{}) string {
	fail := func(extra map[string]interface{}, msg string) string {
		payload := map[string]interface{}{"ok": false, "error": msg}
		for k, v := range extra {
			payload[k] = v
		}
		data, _ := json.MarshalIndent(payload, "", "  ")
		return string(data)
	}
	if h == nil || h.app == nil {
		return `{"ok":false,"error":"app not initialized"}`
	}
	name := strings.TrimSpace(stringVal(args, "name"))
	if name == "" {
		name = strings.TrimSpace(stringVal(args, "skill"))
	}
	draftName := strings.TrimSpace(stringVal(args, "draft"))
	entry, errMsg := findFileBackedSkillEntry(h.app, name)
	if entry == nil {
		return fail(nil, errMsg)
	}
	base := map[string]interface{}{"skill": entry.Name, "draft": draftName}
	draftPath, err := resolveRepairDraftPath(entry.SkillDir, draftName)
	if err != nil {
		return fail(map[string]interface{}{"skill": entry.Name}, err.Error())
	}
	draft, err := readRepairDraftFile(draftPath)
	if err != nil {
		return fail(map[string]interface{}{"skill": entry.Name}, err.Error())
	}

	if draft.Disable {
		// "Disable suggestion" draft (LLM judged the skill unfixable): no step
		// validation, no TOCTOU check and no skill.yaml write-back — only the
		// stored status flips to disabled. Counter/history/audit/event reuse
		// the standard apply path with via="reviewed_draft_disable".
		exec := h.app.skillExecutor
		exec.skillListMutateMu.Lock()
		skills := exec.loadSkills()
		saved := false
		var saveErr error
		for i := range skills {
			if skills[i].MatchesName(entry.Name) {
				skills[i].Status = "disabled"
				recordReviewedDraftRepair(&skills[i], draft, "reviewed_draft_disable", true)
				saveErr = exec.saveSkills(skills)
				saved = true
				break
			}
		}
		exec.skillListMutateMu.Unlock()
		if !saved {
			return fail(base, "skill disappeared from storage during apply")
		}
		if saveErr != nil {
			return fail(base, "save config failed: "+saveErr.Error())
		}
		deleteWarning := ""
		if err := os.Remove(draftPath); err != nil {
			deleteWarning = "delete applied draft failed: " + err.Error()
			log.Printf("[skill-repair-draft] delete applied draft %s failed: %v", draftPath, err)
		}
		auditData := map[string]string{
			"skill":       entry.Name,
			"draft":       draftName,
			"via":         "reviewed_draft_disable",
			"explanation": draft.Explanation,
		}
		cskill.RecordEvolutionEvent(cskill.EventSkillRepaired, auditData, "desktop")
		h.app.emitEvent(EventSkillRepaired, auditData)
		payload := map[string]interface{}{
			"ok":          true,
			"skill":       entry.Name,
			"draft":       draftName,
			"applied":     true,
			"disabled":    true,
			"explanation": draft.Explanation,
			"message":     "skill disabled per reviewed draft (config overlay) and draft removed",
		}
		if deleteWarning != "" {
			payload["warning"] = deleteWarning
		}
		data, _ := json.MarshalIndent(payload, "", "  ")
		return string(data)
	}

	// Hard gate: file-backed skills persist through skill.yaml only — the
	// config overlay strips steps, so without a yaml file the applied repair
	// would silently vanish on reload. SKILL.md-form skills are not supported.
	yamlPath := skillYAMLPath(entry.SkillDir)
	if yamlPath == "" {
		return fail(base, "skill has no skill.yaml/skill.yml (SKILL.md-form skills do not support automatic draft apply); merge manually or reject the draft")
	}

	// Optimistic concurrency (TOCTOU): the draft's old_steps were captured at
	// generation time; if the user hand-edited the skill since, applying would
	// silently overwrite those edits. The comparison must NOT use entry.Steps:
	// loadSkills serves a two-layer 10-minute cache (skillCache +
	// CachedSkillScanner), so its steps can be stale. Re-parse skill.yaml from
	// disk instead. freshSteps doubles as the pre-apply state used to roll
	// skill.yaml back when the config save below fails. Drafts without
	// old_steps (hand-written) skip the check — there is nothing to compare
	// against. Note a residual micro-window remains between this read and the
	// re-read inside WriteBackOptimizedSteps; it is inherent to lock-free
	// optimistic concurrency and only affects same-moment hand edits.
	freshSteps, freshDescription, err := cskill.LoadSkillMetaFromDir(entry.SkillDir)
	if err != nil {
		return fail(base, "re-read "+filepath.Base(yamlPath)+" for concurrency check failed: "+err.Error())
	}
	if len(draft.OldSteps) > 0 && !sameRepairDraftSteps(freshSteps, draft.OldSteps) {
		return fail(base, "skill was modified after the draft was generated; please regenerate the draft")
	}
	if len(draft.NewSteps) == 0 {
		return fail(base, "draft has no new_steps; reject it instead")
	}
	// Hard gate: reviewed drafts must still satisfy the GUI runner action
	// whitelist — the draft file is on disk and could have been hand-edited.
	if err := cskill.ValidateRepairSteps(draft.NewSteps); err != nil {
		return fail(base, "draft new_steps invalid: "+err.Error())
	}
	// Hard gate: WriteBackOptimizedSteps does not round-trip poll/loop configs.
	// Applying to a skill whose steps carry them (or a hand-edited draft that
	// adds them) would silently strip the configs — refuse and let the user
	// merge manually. The pipeline already skips such skills at generation.
	if cskill.StepsHavePollLoop(freshSteps) || cskill.StepsHavePollLoop(draft.NewSteps) {
		return fail(base, "skill steps carry poll/loop configs which the draft flow cannot round-trip; merge manually or reject the draft")
	}

	// 1. Write the new steps back to skill.yaml FIRST: it is the durable
	// store. WriteBackOptimizedSteps re-reads the yaml and only replaces the
	// steps section, so hand edits to other fields survive. Any failure
	// aborts the apply — the draft is kept and no repaired event is emitted.
	// Description is refreshed from disk as well: the cached entry may hold a
	// stale one, and WriteBackOptimizedSteps writes it back when non-empty.
	entry.Steps = draft.NewSteps
	entry.Description = freshDescription
	if err := cskill.WriteBackOptimizedSteps(entry); err != nil {
		return fail(base, "write back "+filepath.Base(yamlPath)+" failed: "+err.Error())
	}

	// 2. Persist the config overlay (steps stripped for file-backed skills)
	// plus repair counters/history so max_attempts also gates the draft flow.
	exec := h.app.skillExecutor
	exec.skillListMutateMu.Lock()
	skills := exec.loadSkills()
	saved := false
	var saveErr error
	for i := range skills {
		if skills[i].MatchesName(entry.Name) {
			skills[i].Steps = draft.NewSteps
			recordReviewedDraftRepair(&skills[i], draft, "reviewed_draft", true)
			saveErr = saveRepairDraftSkills(exec, skills)
			saved = true
			break
		}
	}
	exec.skillListMutateMu.Unlock()
	// Rollback: the yaml already holds new_steps. Leaving it there while the
	// config save failed would make every retry hit the TOCTOU check above
	// (disk new_steps != draft old_steps) and brick the draft forever, so
	// restore the pre-apply steps before reporting the failure.
	if !saved || saveErr != nil {
		msg := "skill disappeared from storage during apply"
		if saveErr != nil {
			msg = "save config failed: " + saveErr.Error()
		}
		rollbackEntry := *entry
		rollbackEntry.Steps = freshSteps
		if rbErr := cskill.WriteBackOptimizedSteps(&rollbackEntry); rbErr != nil {
			msg += "; rollback of " + filepath.Base(yamlPath) + " ALSO FAILED: " + rbErr.Error() +
				" — skill.yaml currently contains the draft's new_steps while the config was not updated; regenerate the draft"
		} else {
			msg += "; " + filepath.Base(yamlPath) + " was rolled back to its pre-apply steps"
		}
		return fail(base, msg)
	}

	// 3. Delete the reviewed draft file. A deletion failure is reported as a
	// warning but does not fail the apply — the repair itself is durable.
	deleteWarning := ""
	if err := os.Remove(draftPath); err != nil {
		deleteWarning = "delete applied draft failed: " + err.Error()
		log.Printf("[skill-repair-draft] delete applied draft %s failed: %v", draftPath, err)
	}

	// 4. Audit + frontend notification (same event/kind as an applied repair).
	auditData := map[string]string{
		"skill":       entry.Name,
		"draft":       draftName,
		"via":         "reviewed_draft",
		"explanation": draft.Explanation,
	}
	cskill.RecordEvolutionEvent(cskill.EventSkillRepaired, auditData, "desktop")
	h.app.emitEvent(EventSkillRepaired, auditData)

	payload := map[string]interface{}{
		"ok":          true,
		"skill":       entry.Name,
		"draft":       draftName,
		"applied":     true,
		"explanation": draft.Explanation,
		// Named new_steps_summary (not new_steps) to avoid clashing with the
		// list contract where new_steps is the full step array.
		"new_steps_summary": summarizeRepairDraftSteps(draft.NewSteps),
		"message":           "repair draft applied (skill.yaml + config overlay) and removed",
	}
	if deleteWarning != "" {
		payload["warning"] = deleteWarning
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

// toolRejectSkillRepairDraft deletes a pending repair draft without applying
// it. The rejection still bumps the repair attempt counter (plus a
// via="reviewed_draft_rejected" history row when the draft is parseable) so
// max_attempts eventually gates the regenerate → reject LLM cost loop, and it
// records an audit row. Unreadable drafts skip the history row but still
// count, so they can always be cleaned up.
// Args: name (required), draft (required file name).
func (h *IMMessageHandler) toolRejectSkillRepairDraft(args map[string]interface{}) string {
	fail := func(extra map[string]interface{}, msg string) string {
		payload := map[string]interface{}{"ok": false, "error": msg}
		for k, v := range extra {
			payload[k] = v
		}
		data, _ := json.MarshalIndent(payload, "", "  ")
		return string(data)
	}
	if h == nil || h.app == nil {
		return `{"ok":false,"error":"app not initialized"}`
	}
	name := strings.TrimSpace(stringVal(args, "name"))
	if name == "" {
		name = strings.TrimSpace(stringVal(args, "skill"))
	}
	draftName := strings.TrimSpace(stringVal(args, "draft"))
	entry, errMsg := findFileBackedSkillEntry(h.app, name)
	if entry == nil {
		return fail(nil, errMsg)
	}
	draftPath, err := resolveRepairDraftPath(entry.SkillDir, draftName)
	if err != nil {
		return fail(map[string]interface{}{"skill": entry.Name}, err.Error())
	}

	// Best-effort read: an unreadable draft is still rejected (counted +
	// deleted + audited), only the history row is skipped.
	draft, draftErr := readRepairDraftFile(draftPath)

	// 1. Count the rejection (and append history when the draft parsed) so
	// repeated rejections cannot regenerate drafts forever. A persist failure
	// keeps the draft — the user may retry.
	exec := h.app.skillExecutor
	exec.skillListMutateMu.Lock()
	skills := exec.loadSkills()
	saved := false
	var saveErr error
	for i := range skills {
		if skills[i].MatchesName(entry.Name) {
			if draftErr != nil {
				recordRejectedDraftAttempt(&skills[i])
			} else {
				recordReviewedDraftRepair(&skills[i], draft, "reviewed_draft_rejected", false)
			}
			saveErr = exec.saveSkills(skills)
			saved = true
			break
		}
	}
	exec.skillListMutateMu.Unlock()
	if !saved {
		return fail(map[string]interface{}{"skill": entry.Name, "draft": draftName}, "skill disappeared from storage during reject")
	}
	if saveErr != nil {
		return fail(map[string]interface{}{"skill": entry.Name, "draft": draftName}, "save config failed: "+saveErr.Error())
	}

	// 2. Delete the draft file.
	if err := os.Remove(draftPath); err != nil {
		return fail(map[string]interface{}{"skill": entry.Name, "draft": draftName}, "delete draft failed: "+err.Error())
	}
	log.Printf("[skill-repair-draft] rejected draft %s for skill %q", draftName, entry.Name)

	// 3. Audit + frontend notification (kind repair_draft is registered).
	auditData := map[string]string{
		"skill":  entry.Name,
		"draft":  draftName,
		"status": "rejected",
	}
	if draftErr == nil {
		auditData["explanation"] = draft.Explanation
	}
	cskill.RecordEvolutionEvent(cskill.EventSkillRepairDraftReady, auditData, "desktop")
	h.app.emitEvent(EventSkillRepairDraftReady, map[string]string{
		"skill":  entry.Name,
		"draft":  draftName,
		"status": "rejected",
	})
	data, _ := json.MarshalIndent(map[string]interface{}{
		"ok":       true,
		"skill":    entry.Name,
		"draft":    draftName,
		"rejected": true,
		"message":  "repair draft rejected and removed",
	}, "", "  ")
	return string(data)
}

// ListSkillRepairDrafts is a Wails binding for the Skills panel "pending
// review" list. Returns the same JSON payload as the manage_skill action.
func (a *App) ListSkillRepairDrafts() string {
	h := &IMMessageHandler{app: a}
	return h.toolListSkillRepairDrafts(map[string]interface{}{})
}

// ApplySkillRepairDraft is a Wails binding for the "apply" button on a pending
// repair draft.
func (a *App) ApplySkillRepairDraft(skill, draft string) string {
	h := &IMMessageHandler{app: a}
	return h.toolApplySkillRepairDraft(map[string]interface{}{
		"name":  skill,
		"draft": draft,
	})
}

// RejectSkillRepairDraft is a Wails binding for the "reject" button on a
// pending repair draft.
func (a *App) RejectSkillRepairDraft(skill, draft string) string {
	h := &IMMessageHandler{app: a}
	return h.toolRejectSkillRepairDraft(map[string]interface{}{
		"name":  skill,
		"draft": draft,
	})
}
