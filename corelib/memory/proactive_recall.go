package memory

// ProactiveRecallOptions controls prompt-context recall shared by GUI, TUI,
// and server agents.
type ProactiveRecallOptions struct {
	ProjectPath        string
	OwnerID            string
	StrictProject      bool
	MaxEntries         int
	EntityLimit        int
	IncludeUserProfile bool
}

// RecallProactive builds the shared proactive recall set for system prompts.
// It uses the planned LightMem controller first, then supplements with a small
// entity recall pass when the primary result leaves room.
func (s *Store) RecallProactive(query string, opts ProactiveRecallOptions) []Entry {
	if s == nil || query == "" {
		return nil
	}
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 12
	}
	entityLimit := opts.EntityLimit
	if entityLimit <= 0 {
		entityLimit = 1
	}

	recalled := s.RecallLightMem(query, "", opts.ProjectPath, opts.OwnerID)
	if len(recalled) < maxEntries {
		recalled = s.supplementProactiveRecallByEntity(query, recalled, opts, maxEntries, entityLimit)
	}

	filtered := make([]Entry, 0, len(recalled))
	for _, entry := range recalled {
		if shouldSkipProactiveRecallEntry(entry, opts) {
			continue
		}
		filtered = append(filtered, entry)
		if len(filtered) >= maxEntries {
			break
		}
	}
	return filtered
}

func (s *Store) supplementProactiveRecallByEntity(query string, recalled []Entry, opts ProactiveRecallOptions, maxEntries int, entityLimit int) []Entry {
	expanded := ExpandQuery(query)
	if len(expanded.Entities) == 0 {
		return recalled
	}
	seen := make(map[string]bool, len(recalled))
	for _, entry := range recalled {
		seen[entry.ID] = true
	}
	entities := expanded.Entities
	if len(entities) > entityLimit {
		entities = entities[:entityLimit]
	}
	for _, entity := range entities {
		var extra []Entry
		if opts.StrictProject && opts.ProjectPath != "" {
			extra = s.RecallDynamicStrict(entity, "", opts.ProjectPath, opts.OwnerID)
		} else {
			extra = s.RecallDynamic(entity, "", opts.ProjectPath, opts.OwnerID)
		}
		for _, entry := range extra {
			if seen[entry.ID] {
				continue
			}
			seen[entry.ID] = true
			recalled = append(recalled, entry)
			if len(recalled) >= maxEntries {
				return recalled
			}
		}
	}
	return recalled
}

func shouldSkipProactiveRecallEntry(entry Entry, opts ProactiveRecallOptions) bool {
	canonical := MapToCanonical(entry.Category)
	if !opts.IncludeUserProfile && (canonical == CategoryUserFact || canonical == CategorySelfIdentity) {
		return true
	}
	return canonical == CategorySessionCheckpoint || canonical == CategoryConversationSummary
}
