package memory

// themeAwareDiversityRerank promotes a compact set of representatives from
// different themes before filling the rest with the original score order. It is
// intentionally conservative: only callers that already classified the query as
// hybrid/complex should use it.
func themeAwareDiversityRerank(candidates []recallScored, themes []ThemeNode, representativeLimit int) []recallScored {
	if len(candidates) < 4 || len(themes) == 0 || representativeLimit <= 1 {
		return candidates
	}
	if representativeLimit > len(candidates) {
		representativeLimit = len(candidates)
	}

	entryTheme := make(map[string]string)
	themeNeighborSim := make(map[string]map[string]float64)
	for _, theme := range themes {
		for _, id := range theme.EntryIDs {
			if _, exists := entryTheme[id]; !exists {
				entryTheme[id] = theme.ID
			}
		}
		if len(theme.Neighbors) > 0 {
			m := make(map[string]float64, len(theme.Neighbors))
			for i, nid := range theme.Neighbors {
				if i < len(theme.NeighborSims) {
					m[nid] = theme.NeighborSims[i]
				}
			}
			themeNeighborSim[theme.ID] = m
		}
	}
	if len(entryTheme) == 0 {
		return candidates
	}

	selected := make([]recallScored, 0, len(candidates))
	selectedIDs := make(map[string]struct{}, len(candidates))
	coveredThemes := make(map[string]struct{})

	for _, candidate := range candidates {
		if len(selected) >= representativeLimit {
			break
		}
		themeID := entryTheme[candidate.entry.ID]
		if themeID == "" {
			continue
		}
		if _, ok := coveredThemes[themeID]; ok {
			continue
		}
		if themeRedundantWithCovered(themeID, coveredThemes, themeNeighborSim) {
			continue
		}
		selected = append(selected, candidate)
		selectedIDs[candidate.entry.ID] = struct{}{}
		coveredThemes[themeID] = struct{}{}
	}

	if len(selected) <= 1 {
		return candidates
	}

	out := make([]recallScored, 0, len(candidates))
	out = append(out, selected...)
	for _, candidate := range candidates {
		if _, ok := selectedIDs[candidate.entry.ID]; ok {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func themeRedundantWithCovered(themeID string, covered map[string]struct{}, neighborSim map[string]map[string]float64) bool {
	for coveredID := range covered {
		if sim := neighborSim[themeID][coveredID]; sim >= 0.95 {
			return true
		}
		if sim := neighborSim[coveredID][themeID]; sim >= 0.95 {
			return true
		}
	}
	return false
}
