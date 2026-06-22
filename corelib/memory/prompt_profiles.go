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
			Footer:          "\uff08\u4ee5\u4e0a\u8bb0\u5fc6\u662f\u6839\u636e\u5f53\u524d\u6d88\u606f\u5b9e\u65f6\u53ec\u56de\u7684\u6700\u65b0\u7ed3\u679c\u3002\u89c4\u5219\uff1a1. \u4ee5\u4e0a\u4fe1\u606f\u89c6\u4e3a\u5df2\u786e\u8ba4\u7684\u4e8b\u5b9e\uff0c\u76f4\u63a5\u4f7f\u7528\uff0c\u4e0d\u8981\u518d\u6b21\u5411\u7528\u6237\u7d22\u8981\u6216\u53cd\u590d\u8c03\u7528 memory \u5de5\u5177\u67e5\u627e\u76f8\u540c\u5185\u5bb9\u3002 2. \u5373\u4f7f\u4f60\u5728\u4e4b\u524d\u7684\u5bf9\u8bdd\u4e2d\u8bf4\u8fc7\u201c\u6ca1\u627e\u5230\u201d\uff0c\u73b0\u5728\u5df2\u7ecf\u627e\u5230\u4e86\uff0c\u4ee5\u5f53\u524d\u53ec\u56de\u7ed3\u679c\u4e3a\u51c6\u3002 3. \u5f53\u4efb\u52a1\u9700\u8981\u67d0\u4e2a\u53c2\u6570\uff08\u5982\u8fde\u63a5\u4fe1\u606f\u3001API key\u3001\u6587\u4ef6\u8def\u5f84\u7b49\uff09\u800c\u4e0a\u65b9\u8bb0\u5fc6\u4e2d\u5df2\u5305\u542b\u8be5\u53c2\u6570\u65f6\uff0c\u76f4\u63a5\u4f7f\u7528\u5e76\u8c03\u7528\u5bf9\u5e94\u5de5\u5177\u6267\u884c\uff0c\u4e0d\u9700\u7b49\u5f85\u7528\u6237\u518d\u6b21\u63d0\u4f9b\u3002\uff09",
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
