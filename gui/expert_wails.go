package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// ---------------------------------------------------------------------------
// Expert Wails bindings. All methods pass JSON strings across the boundary
// (same convention as survey_wails.go). Local store is the source of truth;
// Hub sync is best-effort and never blocks or fails a local operation.
// ---------------------------------------------------------------------------

// expertHubListTimeout bounds the Hub pull in ListExperts so unreachable Hub
// cannot stall expert card rendering.
const expertHubListTimeout = 4 * time.Second

var expertHubSyncMutex sync.Mutex

// ListExperts returns the effective expert list JSON: builtin experts first
// (user override copies flagged builtin=true), then user experts. When Hub is
// reachable the remote list is merged in under a single store lock (LWW +
// tombstones + atomic writeback) and pending local custom experts are pushed
// back best-effort. Offline is fine — local list only.
func (a *App) ListExperts() (string, error) {
	if _, _, err := defaultExpertStore.List(); err != nil {
		return "", err
	}
	if client, cerr := a.newExpertHubClient(); cerr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), expertHubListTimeout)
		hubList, herr := client.List(ctx)
		cancel()
		if herr != nil {
			log.Printf("[experts] hub list skipped: %v", herr)
		} else {
			changedIDs, merr := defaultExpertStore.MergeAndSaveFromHub(hubList)
			if merr != nil {
				log.Printf("[experts] hub merge failed: %v", merr)
			} else {
				for _, id := range changedIDs {
					invalidateExpertDefCache(id)
				}
			}
			a.syncPendingExpertsToHub(client, hubList)
		}
	}
	local, _, err := defaultExpertStore.List()
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(mergeBuiltinExpertList(local))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// syncPendingExpertsToHub performs one bounded background reconciliation pass.
// The mutex coalesces concurrent ListExperts calls so repeated page refreshes
// cannot fan out duplicate upload/delete requests for the same local change.
func (a *App) syncPendingExpertsToHub(client *expertHubClient, hubItems []ExpertDefinition) {
	if client == nil {
		return
	}
	go func() {
		expertHubSyncMutex.Lock()
		defer expertHubSyncMutex.Unlock()
		a.syncPendingExpertsToHubLocked(client, hubItems)
	}()
}

func (a *App) syncPendingExpertsToHubLocked(client *expertHubClient, hubItems []ExpertDefinition) {
	local, tombstones, pendingUploads, pendingDeletes, err := defaultExpertStore.ListForHubSync()
	if err != nil {
		return
	}
	a.retryPendingHubDeletes(client, pendingDeletes)
	stale := expertsPendingHubSync(local, tombstones, pendingUploads, hubItems)
	if len(stale) == 0 {
		return
	}
	for _, def := range stale {
		if !defaultExpertStore.PendingHubUploadIsCurrent(def.ID, def.UpdatedAt) {
			continue
		}
		raw, merr := json.Marshal(def)
		if merr != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		result, perr := client.Upsert(ctx, raw)
		if perr != nil {
			log.Printf("[experts] hub push-back %q failed: %v", def.ID, perr)
		} else if !result.applied() {
			log.Printf("[experts] hub push-back %q was superseded; retaining retry marker", def.ID)
		} else if err := defaultExpertStore.ClearPendingHubUploadIfCurrent(def.ID, def.UpdatedAt); err != nil {
			log.Printf("[experts] clear pending Hub upload %q failed: %v", def.ID, err)
		}
		cancel()
	}
}

// retryPendingHubDeletes completes deletions that were made while Hub was
// unreachable. A remote 404 is already the desired state, so it clears the
// local retry marker too.
func (a *App) retryPendingHubDeletes(client *expertHubClient, pendingDeletes map[string]string) {
	if client == nil || len(pendingDeletes) == 0 {
		return
	}
	ids := make([]string, 0, len(pendingDeletes))
	for id, deletedAt := range pendingDeletes {
		if deletedAt != "" && builtinExpertByID(id) == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	for _, id := range ids {
		deletedAt := pendingDeletes[id]
		if !defaultExpertStore.PendingHubDeleteIsCurrent(id, deletedAt) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := client.Delete(ctx, id)
		cancel()
		if err != nil && !expertHubNotFound(err) {
			log.Printf("[experts] hub delete retry %q failed: %v", id, err)
			continue
		}
		if err := defaultExpertStore.ClearPendingHubDeleteIfCurrent(id, deletedAt); err != nil {
			log.Printf("[experts] clear pending Hub delete %q failed: %v", id, err)
		}
	}
}

func expertHubNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), " 404")
}

// expertsPendingHubSync selects local custom experts that need eventual Hub
// upload. Keeping this decision independent from I/O makes the retry behavior
// easy to verify and ensures imports receive the same treatment as edits.
func expertsPendingHubSync(local []ExpertDefinition, tombstones map[string]string, pendingUploads map[string]bool, hubItems []ExpertDefinition) []ExpertDefinition {
	hubByID := make(map[string]ExpertDefinition, len(hubItems))
	for _, h := range hubItems {
		hubByID[h.ID] = h
	}
	var stale []ExpertDefinition
	for _, l := range local {
		if builtinExpertByID(l.ID) != nil {
			continue // builtins never sync
		}
		if _, deleted := tombstones[l.ID]; deleted {
			continue // never resurrect a locally deleted expert
		}
		h, ok := hubByID[l.ID]
		if !ok {
			if pendingUploads[l.ID] {
				stale = append(stale, l) // new/imported expert whose initial push failed
			}
			continue
		}
		if expertUpdatedAtAfter(l.UpdatedAt, h.UpdatedAt) {
			stale = append(stale, l)
		}
	}
	return stale
}

// expertIDPattern constrains expert ids (user-supplied and generated alike):
// path-safe, URL-safe, no whitespace.
var expertIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// SaveExpert creates or updates an expert from its JSON definition.
//   - Empty id → a new "expert-<ts>-<rand>" id and created_at are assigned.
//   - Editing a builtin expert (id "builtin-*") stores a user override copy
//     under the same id (Builtin=false on disk); the override wins over the
//     in-binary definition.
//   - Non-builtin experts are pushed to Hub best-effort (failure only logs;
//     a later ListExperts retry also uploads a Hub-absent local copy).
//
// Returns the saved definition JSON (with id/timestamps filled in).
func (a *App) SaveExpert(expertJSON string) (string, error) {
	// Review metadata is intentionally absent from ExpertDefinition. JSON
	// decoding into this closed persistence contract drops any accidental UI
	// fields before the definition reaches disk, Hub sync, or an LLM session.
	var def ExpertDefinition
	if err := json.Unmarshal([]byte(expertJSON), &def); err != nil {
		return "", fmt.Errorf("invalid expert JSON: %w", err)
	}
	def.ID = strings.TrimSpace(def.ID)
	def.Name = strings.TrimSpace(def.Name)
	if def.Name == "" {
		return "", fmt.Errorf("expert name is required")
	}
	if def.ID != "" && !expertIDPattern.MatchString(def.ID) {
		return "", fmt.Errorf("invalid expert id %q: must match %s", def.ID, expertIDPattern.String())
	}
	def.OptimizedFromID = strings.TrimSpace(def.OptimizedFromID)
	var storedExisting *ExpertDefinition
	if def.ID != "" {
		if existing, ok, err := defaultExpertStore.Get(def.ID); err != nil {
			return "", fmt.Errorf("look up existing expert: %w", err)
		} else if ok {
			storedExisting = &existing
		}
	}
	isNewOptimized := def.OptimizedFromID != "" && storedExisting == nil
	if def.OptimizedFromID != "" {
		if def.OptimizedFromID == def.ID && def.ID != "" {
			return "", fmt.Errorf("optimized expert cannot reference itself")
		}
		if source := loadExpertDefByID(def.OptimizedFromID); source == nil || source.ID == "" {
			return "", fmt.Errorf("optimized expert source not found: %s", def.OptimizedFromID)
		}
		if isNewOptimized {
			if existing, found, err := defaultExpertStore.FindOptimizedFor(def.OptimizedFromID); err != nil {
				return "", fmt.Errorf("look up existing optimized expert: %w", err)
			} else if found {
				return "", fmt.Errorf("an optimized expert already exists for %s; edit %s instead", def.OptimizedFromID, existing.ID)
			}
		}
	}
	// An ordinary edit must not be able to erase or re-parent an optimized
	// expert's lineage. The UI always sends the lineage explicitly, but this
	// server-side guard also protects API callers and older clients.
	if storedExisting != nil {
		if strings.TrimSpace(storedExisting.OptimizedFromID) != "" {
			if def.OptimizedFromID != storedExisting.OptimizedFromID {
				return "", fmt.Errorf("optimized expert lineage cannot be changed")
			}
		} else if def.OptimizedFromID != "" {
			return "", fmt.Errorf("an existing expert cannot be converted into an optimized expert")
		}
	}
	isBuiltinID := builtinExpertByID(def.ID) != nil
	// RFC3339Nano keeps sub-second precision so rapid successive edits keep a
	// strict updated_at ordering for LWW merges.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if def.ID == "" {
		def.ID = newExpertID()
		def.CreatedAt = now
	} else {
		if storedExisting != nil && storedExisting.CreatedAt != "" {
			def.CreatedAt = storedExisting.CreatedAt
		}
		if strings.TrimSpace(def.CreatedAt) == "" {
			def.CreatedAt = now
		}
	}
	def.UpdatedAt = now
	// On disk a stored record is always a user definition/override copy; the
	// builtin flag is reserved for in-binary definitions and re-applied at list
	// time by mergeBuiltinExpertList.
	def.Builtin = false
	def = normalizeExpertLists(def)
	if isNewOptimized {
		if existingID, err := defaultExpertStore.SaveNewOptimized(def); err != nil {
			return "", err
		} else if existingID != "" {
			return "", fmt.Errorf("an optimized expert already exists for %s; edit %s instead", def.OptimizedFromID, existingID)
		}
	} else if storedExisting != nil && strings.TrimSpace(storedExisting.OptimizedFromID) != "" {
		if err := defaultExpertStore.UpdateOptimized(def); err != nil {
			return "", err
		}
	} else if err := defaultExpertStore.Save(def); err != nil {
		return "", err
	}
	invalidateExpertDefCache(def.ID)
	localOnly, localOnlyErr := defaultExpertStore.IsLocalOnly(def.ID)
	if localOnlyErr != nil {
		log.Printf("[experts] inspect local-only state %q failed: %v", def.ID, localOnlyErr)
	}

	// Best-effort Hub sync. Builtin experts (and their override copies) stay
	// local-only: every device ships the same builtins. Expert Market installs
	// are also device-local, including after a user edits one in the editor.
	if !isBuiltinID && !localOnly {
		if err := defaultExpertStore.MarkPendingHubUpload(def.ID); err != nil {
			log.Printf("[experts] mark pending Hub upload %q failed: %v", def.ID, err)
		}
		if client, cerr := a.newExpertHubClient(); cerr == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if raw, merr := json.Marshal(def); merr == nil {
				result, perr := client.Upsert(ctx, raw)
				if perr != nil {
					log.Printf("[experts] hub upsert %q failed (kept locally): %v", def.ID, perr)
				} else if !result.applied() {
					log.Printf("[experts] hub upsert %q was superseded; retaining retry marker", def.ID)
				} else if err := defaultExpertStore.ClearPendingHubUploadIfCurrent(def.ID, def.UpdatedAt); err != nil {
					log.Printf("[experts] clear pending Hub upload %q failed: %v", def.ID, err)
				}
			}
		}
	}
	out, err := json.Marshal(def)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// DeleteExpert removes a user expert and records a tombstone so a later Hub
// pull cannot resurrect it. Builtin experts cannot be deleted.
func (a *App) DeleteExpert(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("expert id is required")
	}
	if builtinExpertByID(id) != nil {
		return fmt.Errorf("builtin expert cannot be deleted: %s", id)
	}
	if err := defaultExpertStore.Delete(id, true); err != nil {
		return err
	}
	invalidateExpertDefCache(id)
	deletedAt, markErr := defaultExpertStore.MarkPendingHubDelete(id)
	if markErr != nil {
		log.Printf("[experts] mark pending Hub delete %q failed: %v", id, markErr)
	}
	if client, cerr := a.newExpertHubClient(); cerr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if derr := client.Delete(ctx, id); derr != nil && !expertHubNotFound(derr) {
			log.Printf("[experts] hub delete %q failed (tombstone recorded): %v", id, derr)
		} else if err := defaultExpertStore.ClearPendingHubDeleteIfCurrent(id, deletedAt); err != nil {
			log.Printf("[experts] clear pending Hub delete %q failed: %v", id, err)
		}
	}
	return nil
}

// ResetBuiltinExpert removes the user override copy of a builtin expert,
// restoring the in-binary definition.
func (a *App) ResetBuiltinExpert(id string) error {
	id = strings.TrimSpace(id)
	if builtinExpertByID(id) == nil {
		return fmt.Errorf("not a builtin expert: %s", id)
	}
	// No tombstone: builtin ids never sync to Hub, so there is nothing to
	// protect against resurrection.
	if err := defaultExpertStore.Delete(id, false); err != nil {
		return err
	}
	invalidateExpertDefCache(id)
	return nil
}

// ListAvailableToolNames returns the tool registry as
// [{"name":"...","description":"...","deferred":bool}] for the expert editor's
// tool picker. Deferred tools (activated on demand via discover_tool) are
// included so they can be whitelisted up front. Descriptions are truncated to
// keep the payload small.
func (a *App) ListAvailableToolNames() (string, error) {
	a.ensureInteractionInfra()
	h := a.imHandler
	if h == nil {
		return "", errors.New("assistant not ready")
	}
	// BuildAll includes deferred tools; getTools() would filter the inactive
	// ones out. Fall back to getTools() when the builder path is unavailable.
	var tools []map[string]interface{}
	if h.toolBuilder != nil && h.registry != nil {
		tools = h.toolBuilder.BuildAll()
	} else {
		tools = h.getTools()
	}
	deferred := make(map[string]bool, len(DeferredToolNames))
	for _, name := range DeferredToolNames {
		deferred[name] = true
	}
	type toolEntry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Deferred    bool   `json:"deferred"`
		Category    string `json:"category"`
		Risk        string `json:"risk"`
		LabelZh     string `json:"label_zh"`
		LabelEn     string `json:"label_en"`
	}
	out := make([]toolEntry, 0, len(tools))
	seen := make(map[string]bool, len(tools))
	for _, def := range tools {
		name := extractToolName(def)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		meta := lookupExpertToolMeta(name)
		out = append(out, toolEntry{
			Name:        name,
			Description: truncateForLogGUI(extractToolDescription(def), 100),
			Deferred:    deferred[name],
			Category:    meta.Category,
			Risk:        meta.Risk,
			LabelZh:     meta.LabelZh,
			LabelEn:     meta.LabelEn,
		})
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ---------------------------------------------------------------------------
// Phase 3: meta-prompt generation — turn a one-line idea into a full expert
// profile draft via a lightweight LLM call.
// ---------------------------------------------------------------------------

// expertProfileGenSystemPrompt is the meta prompt for profile generation.
const expertProfileGenSystemPrompt = `你是一名"AI 专家设计师"。用户会给你一句话想法，你要把它设计成一个可落地的"专家助手"配置。

只输出一个严格的 JSON 对象，不要输出任何其他文字、解释或 Markdown 代码块标记：
{
  "name": "专家名称，不超过 8 个汉字",
  "description": "一句话卡片简介，不超过 40 个汉字",
  "icon": "一个最能代表该专家的 emoji（单个字符）",
  "system_prompt": "完整的专家系统提示词",
  "suggested_tools": ["从用户给出的工具清单中挑选的工具名"],
  "suggested_skills": ["从用户给出的技能清单中挑选的技能名"]
}

system_prompt 写作要求：
- 用中文撰写，300-600 字。
- 必须结构化，包含四个小节：# 角色定位、# 工作流程、# 输出格式、# 边界约束。
- 角色定位：说明专家身份、专长领域与服务对象。
- 工作流程：3-5 个编号步骤，描述接到任务后如何处理（含必要时向用户确认信息）。
- 输出格式：说明回答的组织方式（小节标题/列表/表格等）。
- 边界约束：明确不做什么——不虚构信息、不越界处理无关事务、不确定时向用户确认。

suggested_tools / suggested_skills 规则：
- 只能从用户提供的清单中原样挑选，不得编造名字；没有合适的就输出空数组 []。
- 只选完成该专家核心任务所必需的最小集合，宁缺毋滥。`

// expertProfileGenSimplifiedPrompt is the retry prompt when the full meta
// prompt's output fails to parse.
const expertProfileGenSimplifiedPrompt = `把用户的想法转成一个专家配置。只输出一个 JSON 对象，不要其他文字：
{"name":"≤8个汉字的专家名","description":"≤40字简介","icon":"单个emoji","system_prompt":"300字以上的中文系统提示词，含 角色定位/工作流程/输出格式/边界约束 四节","suggested_tools":[],"suggested_skills":[]}
suggested_tools 和 suggested_skills 只能从用户给的清单中原样挑选，没有就输出 []。`

// expertProfileSuggestion validates the LLM's profile draft shape.
type expertProfileSuggestion struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Icon            string   `json:"icon"`
	SystemPrompt    string   `json:"system_prompt"`
	SuggestedTools  []string `json:"suggested_tools"`
	SuggestedSkills []string `json:"suggested_skills"`
}

// GenerateExpertProfile generates an expert profile draft JSON from a one-line
// idea. The returned JSON is passed through for the frontend editor to refine;
// suggested_tools/skills are pre-selections only.
func (a *App) GenerateExpertProfile(idea string) (string, error) {
	idea = strings.TrimSpace(idea)
	if idea == "" {
		return "", fmt.Errorf("idea is required")
	}
	a.ensureInteractionInfra()
	h := a.imHandler
	if h == nil {
		return "", errors.New("assistant not ready")
	}

	userMsg := buildExpertProfileGenUserMessage(h, idea)
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "expert-profile-gen", OwnerID: desktopUserID})

	// Attempt 1: full meta prompt.
	result, err := h.LLMClassify(ctx, LLMClassifyRequest{
		SystemPrompt:      expertProfileGenSystemPrompt,
		UserMessage:       userMsg,
		PreferLightweight: true,
		TimeoutSec:        60,
		Tag:               "expert-profile-gen",
	})
	if err == nil {
		if parsed, perr := parseExpertProfileResponse(result.Text); perr == nil {
			return parsed, nil
		} else {
			log.Printf("[expert-profile-gen] attempt 1 parse failed: %v (raw_len=%d)", perr, len([]rune(result.Text)))
		}
	} else {
		log.Printf("[expert-profile-gen] attempt 1 LLM call failed: %v", err)
	}

	// Attempt 2: simplified prompt.
	result2, err2 := h.LLMClassify(ctx, LLMClassifyRequest{
		SystemPrompt:      expertProfileGenSimplifiedPrompt,
		UserMessage:       userMsg,
		PreferLightweight: true,
		TimeoutSec:        60,
		Tag:               "expert-profile-gen-retry",
	})
	if err2 != nil {
		return "", fmt.Errorf("expert profile generation failed: %w", err2)
	}
	parsed2, perr2 := parseExpertProfileResponse(result2.Text)
	if perr2 != nil {
		return "", fmt.Errorf("expert profile generation returned unparseable output: %w", perr2)
	}
	return parsed2, nil
}

// buildExpertProfileGenUserMessage assembles the user message: the idea plus
// the current tool and skill catalogs the suggestions must pick from.
func buildExpertProfileGenUserMessage(h *IMMessageHandler, idea string) string {
	var b strings.Builder
	b.WriteString("用户想法：")
	b.WriteString(idea)

	toolNames := make([]string, 0, 32)
	for _, def := range h.getTools() {
		if name := extractToolName(def); name != "" {
			toolNames = append(toolNames, name)
		}
	}
	b.WriteString("\n\n当前可用工具清单（suggested_tools 只能从这里选）：\n")
	b.WriteString(strings.Join(toolNames, ", "))

	b.WriteString("\n\n当前可用技能清单（suggested_skills 只能从这里选技能名）：\n")
	if se := h.getSkillExecutor(); se != nil {
		skills := se.List()
		if len(skills) == 0 {
			b.WriteString("（无）")
		}
		for _, s := range skills {
			desc := truncateForLogGUI(strings.TrimSpace(s.Description), 60)
			if desc == "" {
				b.WriteString("- " + s.Name + "\n")
			} else {
				b.WriteString("- " + s.Name + ": " + desc + "\n")
			}
		}
	} else {
		b.WriteString("（无）")
	}
	return b.String()
}

// parseExpertProfileResponse extracts and validates the profile draft JSON,
// returning the raw JSON object for pass-through. Mirrors
// parseTaskUnderstandingResponse: strip code fences, take first { to last }.
func parseExpertProfileResponse(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty response")
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return "", fmt.Errorf("no JSON object found")
	}
	raw = raw[start : end+1]

	var profile expertProfileSuggestion
	if err := json.Unmarshal([]byte(raw), &profile); err != nil {
		return "", fmt.Errorf("JSON parse: %w", err)
	}
	if strings.TrimSpace(profile.Name) == "" || strings.TrimSpace(profile.SystemPrompt) == "" {
		return "", fmt.Errorf("name or system_prompt is empty")
	}
	return raw, nil
}
