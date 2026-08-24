package main

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func (a *App) searchEnterpriseCodingExperiences(ctx context.Context, opts knowledge.CodingSearchOptions) []knowledge.CodingExperience {
	if a == nil {
		return nil
	}
	store := a.ensureEnterpriseKnowledgeStore()
	if store == nil {
		return nil
	}
	coding := knowledge.WrapCodingKnowledgeStore(store)
	if coding == nil {
		return nil
	}
	if opts.Limit <= 0 {
		opts.Limit = 4
	}
	if len(opts.Status) == 0 {
		opts.Status = []string{knowledge.CodingStatusActive, knowledge.CodingStatusVerified}
	}
	items, err := coding.SearchExperiences(ctx, opts)
	if err != nil || len(items) == 0 {
		return nil
	}
	return items
}

func mergeCodingExperiences(local, enterprise []knowledge.CodingExperience, limit int) []knowledge.CodingExperience {
	if limit <= 0 {
		limit = 4
	}
	seen := map[string]struct{}{}
	out := make([]knowledge.CodingExperience, 0, limit)
	appendUnique := func(items []knowledge.CodingExperience) {
		for _, exp := range items {
			if len(out) >= limit {
				return
			}
			key := strings.ToLower(strings.TrimSpace(exp.Title) + "|" + strings.TrimSpace(exp.TriggerCondition))
			if key == "|" {
				key = exp.ID
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, exp)
		}
	}
	appendUnique(local)
	appendUnique(enterprise)
	return out
}

func appendEnterpriseCodingPackItems(pack knowledge.ContextPackResult, extras []knowledge.CodingExperience, maxItems int) knowledge.ContextPackResult {
	if maxItems <= 0 {
		maxItems = 4
	}
	for _, exp := range extras {
		if len(pack.Items) >= maxItems {
			break
		}
		dup := false
		for _, item := range pack.Items {
			if strings.EqualFold(strings.TrimSpace(item.Title), strings.TrimSpace(exp.Title)) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		text := strings.TrimSpace(exp.Content)
		if text == "" {
			text = strings.TrimSpace(exp.TriggerCondition)
		}
		if text == "" {
			continue
		}
		pack.Items = append(pack.Items, knowledge.ContextPackItem{
			Label:      "E",
			ResultType: "coding_experience",
			Title:      exp.Title,
			Text:       text,
			SourceID:   exp.ID,
			Citation:   "enterprise coding experience",
			Score:      exp.Confidence,
		})
	}
	pack.Count = len(pack.Items)
	if len(extras) > 0 {
		pack.Notes = append(pack.Notes, "enterprise_coding_experiences_readonly")
	}
	return pack
}

func (a *App) mergeEnterpriseCodingPack(ctx context.Context, pack knowledge.ContextPackResult, query, language, projectPath string, maxItems int) knowledge.ContextPackResult {
	extras := a.searchEnterpriseCodingExperiences(ctx, knowledge.CodingSearchOptions{
		Query: query, Language: language, ProjectPath: projectPath,
		Status: []string{knowledge.CodingStatusActive, knowledge.CodingStatusVerified},
		Limit:  maxItems,
	})
	return appendEnterpriseCodingPackItems(pack, extras, maxItems)
}

func (a *App) mergeEnterpriseCodingSearch(ctx context.Context, local []knowledge.CodingExperience, query, language, projectPath string, limit int) []knowledge.CodingExperience {
	extras := a.searchEnterpriseCodingExperiences(ctx, knowledge.CodingSearchOptions{
		Query: query, Language: language, ProjectPath: projectPath,
		Status: []string{knowledge.CodingStatusActive, knowledge.CodingStatusVerified},
		Limit:  limit,
	})
	return mergeCodingExperiences(local, extras, limit)
}
