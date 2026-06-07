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
			MaxEntries:  12,
			EntityLimit: 3,
		},
		IncludeSceneIndex: true,
		SceneLimit:        3,
		MaxScenes:         3,
		MaxArtifacts:      2,
		RecallEntries: RecallEntriesPromptOptions{
			Header:   "\n\u76f8\u5173\u8bb0\u5fc6\uff08\u81ea\u52a8\u53ec\u56de\uff09:",
			Footer:   "\uff08\u4ee5\u4e0a\u8bb0\u5fc6\u662f\u6839\u636e\u5f53\u524d\u6d88\u606f\u5b9e\u65f6\u53ec\u56de\u7684\u6700\u65b0\u7ed3\u679c\u3002\u8bf7\u76f4\u63a5\u4f7f\u7528\u4ee5\u4e0a\u4fe1\u606f\u3002\uff09",
			MaxRunes: defaultPromptMaxRunes,
		},

		PageIndexEnabled:      true,
		PageIndexMaxTokens:    800,
		PartialResultsEnabled: true,
	}
}

// IMProactivePromptOptions is the shared dynamic memory profile for the main
// desktop IM assistant.
func IMProactivePromptOptions(projectPath string, strictProject bool) ProactivePromptOptions {
	return ProactivePromptOptions{
		Recall: ProactiveRecallOptions{
			ProjectPath:        projectPath,
			StrictProject:      strictProject,
			MaxEntries:         12,
			EntityLimit:        1,
			IncludeUserProfile: true,
		},
		IncludeMemoryIndex: true,
		MemoryIndexLabel:   "\n[\u8bb0\u5fc6\u7d22\u5f15] ",
		MemoryIndexUnit:    "entries",
		IncludeSceneIndex:  true,
		SceneLimit:         5,
		MaxScenes:          3,
		MaxArtifacts:       2,
		RecallEntries: RecallEntriesPromptOptions{
			Header:          "\n\u76f8\u5173\u8bb0\u5fc6\uff08\u81ea\u52a8\u53ec\u56de\uff09:",
			Footer:          "\uff08\u4ee5\u4e0a\u8bb0\u5fc6\u662f\u6839\u636e\u5f53\u524d\u6d88\u606f\u5b9e\u65f6\u53ec\u56de\u7684\u6700\u65b0\u7ed3\u679c\u3002\u5373\u4f7f\u4f60\u5728\u4e4b\u524d\u7684\u5bf9\u8bdd\u4e2d\u8bf4\u8fc7\u201c\u6ca1\u627e\u5230\u201d\u6216\u201c\u8bb0\u5fc6\u5e93\u4e3a\u7a7a\u201d\uff0c\u73b0\u5728\u5df2\u7ecf\u627e\u5230\u4e86\uff0c\u8bf7\u76f4\u63a5\u4f7f\u7528\u4ee5\u4e0a\u4fe1\u606f\uff0c\u4e0d\u8981\u91cd\u590d\u4e4b\u524d\u7684\u9519\u8bef\u5224\u65ad\u3002\uff09",
			MaxRunes:        defaultPromptMaxRunes,
			SourceNumbering: true,
		},
		IncludeDerivedFacts: true,
		DerivedFactLimit:    5,

		PageIndexEnabled:      true,
		PageIndexMaxTokens:    800,
		PartialResultsEnabled: true,
	}
}

// VEProactivePromptOptions is the shared owner-memory profile for virtual
// employee sessions.
func VEProactivePromptOptions() ProactivePromptOptions {
	return ProactivePromptOptions{
		Recall: ProactiveRecallOptions{
			MaxEntries:         10,
			EntityLimit:        1,
			IncludeUserProfile: true,
		},
		IncludeMemoryIndex: true,
		MemoryIndexLabel:   "\n[Memory Index] ",
		MemoryIndexUnit:    "entries",
		RecallEntries: RecallEntriesPromptOptions{
			Header:   "\n## Owner Memory (auto recall)",
			Intro:    "The following information comes from the owner memory store and may be relevant. Use it together with knowledge-base context.",
			Footer:   "Call memory(action: recall, query: <keywords>) if more owner memory is needed.",
			MaxRunes: defaultPromptMaxRunes,
		},
	}
}

// BtwProactivePromptOptions is the shared lightweight recall profile for /btw
// side queries in GUI and TUI.
func BtwProactivePromptOptions(projectPath string, header string) ProactivePromptOptions {
	return ProactivePromptOptions{
		Recall: ProactiveRecallOptions{ProjectPath: projectPath, MaxEntries: 8, EntityLimit: 1},
		RecallEntries: RecallEntriesPromptOptions{
			Header:   header,
			MaxRunes: defaultPromptMaxRunes,
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
