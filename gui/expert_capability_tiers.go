package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Expert capability tier ids (must stay aligned with frontend expertCapabilityTiers.ts).
const (
	expertTierFull    = "full"
	expertTierAdvisor = "advisor"
	expertTierDocs    = "docs"
	expertTierOffice  = "office"
	expertTierCustom  = "custom"
)

// expertCapabilityTierResult is the JSON payload for ResolveExpertCapabilityTier.
type expertCapabilityTierResult struct {
	Tier   string   `json:"tier"`
	Tools  []string `json:"tools"`
	Skills []string `json:"skills"`
}

type expertTierToolRule struct {
	MaxRisk    string
	Categories map[string]bool
}

type expertTierSkillRule struct {
	MaxRisk    string
	Categories map[string]bool // empty → select no skills
}

// Mirrors frontend TIER_TOOL_RULES / TIER_SKILL_RULES.
var expertTierToolRules = map[string]expertTierToolRule{
	expertTierAdvisor: {
		MaxRisk: "safe",
		Categories: map[string]bool{
			"interaction": true,
			"media":       true,
		},
	},
	expertTierDocs: {
		MaxRisk: "elevated",
		Categories: map[string]bool{
			"interaction": true,
			"files":       true,
			"web":         true,
			"knowledge":   true,
			"office":      true,
			"media":       true,
		},
	},
	expertTierOffice: {
		MaxRisk: "elevated",
		Categories: map[string]bool{
			"interaction": true,
			"files":       true,
			"web":         true,
			"knowledge":   true,
			"office":      true,
			"media":       true,
			"automation":  true,
		},
	},
}

var expertTierSkillRules = map[string]expertTierSkillRule{
	expertTierAdvisor: {MaxRisk: "safe", Categories: map[string]bool{}},
	expertTierDocs: {
		MaxRisk: "elevated",
		Categories: map[string]bool{
			"docs":     true,
			"security": true,
		},
	},
	expertTierOffice: {
		MaxRisk: "elevated",
		Categories: map[string]bool{
			"docs":     true,
			"office":   true,
			"security": true,
		},
	},
}

func expertRiskRank(risk string) int {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "safe":
		return 0
	case "elevated":
		return 1
	case "dangerous":
		return 2
	default:
		return 1
	}
}

func expertRiskAllowed(risk, maxRisk string) bool {
	return expertRiskRank(risk) <= expertRiskRank(maxRisk)
}

// lookupExpertSkillMeta classifies NL skills by name (substring rules).
// Categories: docs|office|dev|security|other. Risks: safe|elevated|dangerous.
// expertSkillMetaRules is ordered: first substring match wins (put longer tokens first).
var expertSkillMetaRules = []struct {
	match, category, risk string
}{
	{"pdf-word", "docs", "elevated"},
	{"pdf_word", "docs", "elevated"},
	{"doc-redact", "security", "elevated"},
	{"doc_redact", "security", "elevated"},
	{"pptx", "office", "elevated"},
	{"sheet", "office", "elevated"},
	{"excel", "office", "elevated"},
	{"paper", "docs", "safe"},
	{"translat", "docs", "safe"},
	{"contract", "docs", "elevated"},
	{"ssh", "security", "dangerous"},
	{"craft", "dev", "elevated"},
	{"code", "dev", "elevated"},
	{"agent", "dev", "elevated"},
	{"empty", "other", "safe"},
	// "ppt" last among ppt* so "pptx" wins when both would match.
	{"ppt", "office", "elevated"},
}

func lookupExpertSkillMeta(name string) (category, risk string) {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return "other", "elevated"
	}
	for _, r := range expertSkillMetaRules {
		if strings.Contains(lower, r.match) {
			return r.category, r.risk
		}
	}
	return "other", "elevated"
}

func resolveExpertCapabilityTier(tier string, toolNames, skillNames []string) (tools, skills []string) {
	tier = strings.ToLower(strings.TrimSpace(tier))
	tools = []string{}
	skills = []string{}
	if tier == expertTierFull || tier == expertTierCustom || tier == "" {
		return tools, skills
	}
	toolRule, ok := expertTierToolRules[tier]
	if !ok {
		return tools, skills
	}
	for _, name := range toolNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		meta := lookupExpertToolMeta(name)
		if !expertRiskAllowed(meta.Risk, toolRule.MaxRisk) {
			continue
		}
		if !toolRule.Categories[meta.Category] {
			continue
		}
		tools = append(tools, name)
	}
	skillRule := expertTierSkillRules[tier]
	if len(skillRule.Categories) == 0 {
		return tools, skills
	}
	for _, name := range skillNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		cat, risk := lookupExpertSkillMeta(name)
		if !expertRiskAllowed(risk, skillRule.MaxRisk) {
			continue
		}
		if !skillRule.Categories[cat] {
			continue
		}
		skills = append(skills, name)
	}
	return tools, skills
}

func inferExpertCapabilityTier(tools, skills, availableTools, availableSkills []string) string {
	if len(tools) == 0 && len(skills) == 0 {
		return expertTierFull
	}
	for _, tier := range []string{expertTierAdvisor, expertTierDocs, expertTierOffice} {
		gotTools, gotSkills := resolveExpertCapabilityTier(tier, availableTools, availableSkills)
		if sameStringSet(gotTools, tools) && sameStringSet(gotSkills, skills) {
			return tier
		}
	}
	return expertTierCustom
}

func sameStringSet(a, b []string) bool {
	norm := func(in []string) []string {
		out := make([]string, 0, len(in))
		seen := map[string]bool{}
		for _, s := range in {
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
		sort.Strings(out)
		return out
	}
	na, nb := norm(a), norm(b)
	if len(na) != len(nb) {
		return false
	}
	for i := range na {
		if na[i] != nb[i] {
			return false
		}
	}
	return true
}

// listExpertCatalogToolNames returns live tool names for tier resolution.
func (a *App) listExpertCatalogToolNames() ([]string, error) {
	a.ensureInteractionInfra()
	h := a.imHandler
	if h == nil {
		return nil, fmt.Errorf("assistant not ready")
	}
	var defs []map[string]interface{}
	if h.toolBuilder != nil && h.registry != nil {
		defs = h.toolBuilder.BuildAll()
	} else {
		defs = h.getTools()
	}
	out := make([]string, 0, len(defs))
	seen := map[string]bool{}
	for _, def := range defs {
		name := extractToolName(def)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

func (a *App) listExpertCatalogSkillNames() []string {
	a.ensureInteractionInfra()
	h := a.imHandler
	if h == nil {
		return nil
	}
	se := h.getSkillExecutor()
	if se == nil {
		return nil
	}
	skills := se.List()
	out := make([]string, 0, len(skills))
	for _, s := range skills {
		if name := strings.TrimSpace(s.Name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// ResolveExpertCapabilityTier returns JSON {tier,tools,skills} for a preset
// capability profile, resolved against the live tool/skill catalogs.
// full/custom → empty allow-lists (unrestricted / manual).
func (a *App) ResolveExpertCapabilityTier(tier string) (string, error) {
	tier = strings.ToLower(strings.TrimSpace(tier))
	switch tier {
	case expertTierFull, expertTierAdvisor, expertTierDocs, expertTierOffice, expertTierCustom:
	default:
		return "", fmt.Errorf("unknown capability tier %q", tier)
	}
	toolNames, err := a.listExpertCatalogToolNames()
	if err != nil {
		return "", err
	}
	skillNames := a.listExpertCatalogSkillNames()
	tools, skills := resolveExpertCapabilityTier(tier, toolNames, skillNames)
	if tools == nil {
		tools = []string{}
	}
	if skills == nil {
		skills = []string{}
	}
	data, err := json.Marshal(expertCapabilityTierResult{
		Tier:   tier,
		Tools:  tools,
		Skills: skills,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// InferExpertCapabilityTier inspects allow-lists and returns the matching
// preset tier id (full|advisor|docs|office|custom) as a plain string.
func (a *App) InferExpertCapabilityTier(toolsJSON, skillsJSON string) (string, error) {
	var tools, skills []string
	if strings.TrimSpace(toolsJSON) != "" {
		if err := json.Unmarshal([]byte(toolsJSON), &tools); err != nil {
			return "", fmt.Errorf("invalid tools json: %w", err)
		}
	}
	if strings.TrimSpace(skillsJSON) != "" {
		if err := json.Unmarshal([]byte(skillsJSON), &skills); err != nil {
			return "", fmt.Errorf("invalid skills json: %w", err)
		}
	}
	toolNames, err := a.listExpertCatalogToolNames()
	if err != nil {
		// Without a live catalog, only full/custom can be inferred.
		if len(tools) == 0 && len(skills) == 0 {
			return expertTierFull, nil
		}
		return expertTierCustom, nil
	}
	skillNames := a.listExpertCatalogSkillNames()
	return inferExpertCapabilityTier(tools, skills, toolNames, skillNames), nil
}
