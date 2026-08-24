package memory

const defaultPromptMaxRunes = 200
const defaultUserFactMaxRunes = 400

// StaticUserMemoryPromptOptions returns the standard static user-memory block
// used by host prompts. Hosts choose only the section spacing and guide text.
func StaticUserMemoryPromptOptions(header string, includeGuide bool, guide string) StaticMemoryPromptOptions {
	return StaticMemoryPromptOptions{
		UserFacts: UserFactSummaryPromptOptions{
			Header:   header,
			Prefix:   "\u7528\u6237\u4fe1\u606f: ",
			MaxRunes: defaultUserFactMaxRunes,
		},
		IncludeRecallHint: true,
		IncludeGuide:      includeGuide,
		Guide:             guide,
		GuidePrefix:       "\n",
	}
}

// RecallHintAndGuidePromptOptions returns a static section containing only the
// explicit recall hint and optional write guide.
func RecallHintAndGuidePromptOptions(includeGuide bool, guide string) StaticMemoryPromptOptions {
	return StaticMemoryPromptOptions{
		IncludeRecallHint: true,
		IncludeGuide:      includeGuide,
		Guide:             guide,
		GuidePrefix:       "\n",
	}
}

// CoreAgentProactivePromptOptions is the shared dynamic memory profile for the
// core server/agent prompt.
func CoreAgentProactivePromptOptions() ProactivePromptOptions {
	return ProactivePromptOptions{
		Recall: ProactiveRecallOptions{
			MaxEntries:  0,
			EntityLimit: 3,
		},
		IncludeMemoryIndex: true,
		MemoryIndexLabel:   "\n[\u8bb0\u5fc6\u7d22\u5f15] ",
		MemoryIndexUnit:    "entries",
		CatalogOnly:        true,
		RecallEntries: RecallEntriesPromptOptions{
			Footer: CatalogOnlyWorkingSetFooter(),
		},
	}
}

// IMProactivePromptOptions is the shared dynamic memory profile for the main
// desktop IM assistant.
func IMProactivePromptOptions(projectPath string, strictProject bool) ProactivePromptOptions {
	return ProactivePromptOptions{
		Recall: ProactiveRecallOptions{
			ProjectPath:   projectPath,
			StrictProject: strictProject,
			MaxEntries:    0,
			EntityLimit:   1,
		},
		IncludeMemoryIndex: true,
		MemoryIndexLabel:   "\n[\u8bb0\u5fc6\u7d22\u5f15] ",
		MemoryIndexUnit:    "entries",
		CatalogOnly: true,
		RecallEntries: RecallEntriesPromptOptions{
			Footer: CatalogOnlyWorkingSetFooter(),
		},
	}
}

// VEProactivePromptOptions is the shared owner-memory profile for virtual
// employee sessions. Catalog only: VE hosts write user-fact identity, not
// warehouse bodies.
func VEProactivePromptOptions() ProactivePromptOptions {
	return ProactivePromptOptions{
		Recall: ProactiveRecallOptions{
			MaxEntries:         0,
			EntityLimit:        1,
			IncludeUserProfile: true,
		},
		IncludeMemoryIndex: true,
		MemoryIndexLabel:   "\n[Memory Index] ",
		MemoryIndexUnit:    "entries",
		CatalogOnly:        true,
		RecallEntries: RecallEntriesPromptOptions{
			Footer: CatalogOnlyWorkingSetFooter(),
		},
	}
}

// BtwProactivePromptOptions is the shared lightweight profile for /btw
// side queries. Catalog only: hosts keep identity + user facts + tool hints.
func BtwProactivePromptOptions(projectPath string, header string) ProactivePromptOptions {
	return ProactivePromptOptions{
		Recall: ProactiveRecallOptions{ProjectPath: projectPath, MaxEntries: 0, EntityLimit: 1},
		IncludeMemoryIndex: true,
		CatalogOnly:        true,
		RecallEntries: RecallEntriesPromptOptions{
			Header: header,
			Footer: CatalogOnlyWorkingSetFooter(),
		},
	}
}

// UserFactTemplatePromptOptions returns a localized template-based user fact profile.
func UserFactTemplatePromptOptions(template string) UserFactSummaryPromptOptions {
	return UserFactSummaryPromptOptions{Template: template, MaxRunes: defaultUserFactMaxRunes}
}

// UserInfoPromptOptions returns the standard Chinese user-info summary line.
func UserInfoPromptOptions(header string) UserFactSummaryPromptOptions {
	return UserFactSummaryPromptOptions{Header: header, Prefix: "\u7528\u6237\u4fe1\u606f: ", MaxRunes: defaultUserFactMaxRunes}
}

// UserFactPromptOptions returns a compact user fact section profile for prompts
// that are intentionally not using the full static memory block.
func UserFactPromptOptions(header string) UserFactSummaryPromptOptions {
	return UserFactSummaryPromptOptions{Header: header, MaxRunes: defaultUserFactMaxRunes}
}
