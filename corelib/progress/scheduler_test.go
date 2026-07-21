package progress

import (
	"testing"
)

func TestSchedule_DecisionMatrix(t *testing.T) {
	tests := []struct {
		name     string
		input    ScheduleInput
		expected ScheduleAction
	}{
		// --- Negation row ---
		{
			name: "negation + low relevance → Replace",
			input: ScheduleInput{
				Relevance:   0.10,
				DomainMatch: false,
				Structure:   StructureSignal{Length: 4, IsShort: true, HasNegation: true},
			},
			expected: ActionReplace,
		},
		{
			name: "negation + high relevance + medium msg → Merge (modification)",
			input: ScheduleInput{
				Relevance:   0.75,
				DomainMatch: true,
				Structure:   StructureSignal{Length: 15, IsMedium: true, HasNegation: true},
			},
			expected: ActionMerge,
		},
		{
			name: "negation + high relevance + short msg → Replace",
			input: ScheduleInput{
				Relevance:   0.75,
				DomainMatch: true,
				Structure:   StructureSignal{Length: 3, IsShort: true, HasNegation: true},
			},
			expected: ActionReplace,
		},

		// --- High relevance / same domain row ---
		{
			name: "high relevance + same domain + short → Merge",
			input: ScheduleInput{
				Relevance:   0.72,
				DomainMatch: true,
				Structure:   StructureSignal{Length: 8, IsMedium: true},
			},
			expected: ActionMerge,
		},
		{
			name: "high relevance + same domain + long → Merge",
			input: ScheduleInput{
				Relevance:   0.80,
				DomainMatch: true,
				Structure:   StructureSignal{Length: 50, IsLong: true},
			},
			expected: ActionMerge,
		},

		// --- Low relevance / different domain row ---
		{
			name: "low relevance + diff domain + short → StatusQuery",
			input: ScheduleInput{
				Relevance:   0.10,
				DomainMatch: false,
				Structure:   StructureSignal{Length: 1, IsShort: true},
			},
			expected: ActionStatusQuery,
		},
		{
			name: "low relevance + diff domain + medium → Queue",
			input: ScheduleInput{
				Relevance:   0.15,
				DomainMatch: false,
				Structure:   StructureSignal{Length: 15, IsMedium: true},
			},
			expected: ActionQueue,
		},
		{
			name: "low relevance + diff domain + long → Queue",
			input: ScheduleInput{
				Relevance:   0.05,
				DomainMatch: false,
				Structure:   StructureSignal{Length: 50, IsLong: true},
			},
			expected: ActionQueue,
		},

		// --- Embedding unavailable (relevance = -1) ---
		{
			name: "no embedding + same domain + medium → Queue to avoid merging independent tasks",
			input: ScheduleInput{
				Relevance:   -1,
				DomainMatch: true,
				Structure:   StructureSignal{Length: 15, IsMedium: true},
			},
			expected: ActionQueue,
		},
		{
			name: "no embedding + same domain + short → Merge",
			input: ScheduleInput{
				Relevance:   -1,
				DomainMatch: true,
				Structure:   StructureSignal{Length: 3, IsShort: true},
			},
			expected: ActionMerge,
		},
		{
			name: "no embedding + diff domain + medium → Queue",
			input: ScheduleInput{
				Relevance:   -1,
				DomainMatch: false,
				Structure:   StructureSignal{Length: 15, IsMedium: true},
			},
			expected: ActionQueue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := Schedule(tt.input)
			if decision.Action != tt.expected {
				t.Errorf("expected %s, got %s (reason: %s)",
					tt.expected, decision.Action, decision.Reason)
			}
		})
	}
}

func TestSchedule_RealWorldScenarios(t *testing.T) {
	tests := []struct {
		name        string
		currentTask string
		newMessage  string
		relevance   float64
		domainMatch bool
		expected    ScheduleAction
	}{
		{
			name:        "cancel: 算了不做了",
			currentTask: "开发贪吃蛇游戏",
			newMessage:  "算了不做了",
			relevance:   0.20,
			domainMatch: false,
			expected:    ActionReplace,
		},
		{
			name:        "supplement: 颜色改红色",
			currentTask: "开发贪吃蛇游戏",
			newMessage:  "颜色改成红色",
			relevance:   0.72,
			domainMatch: true,
			expected:    ActionMerge,
		},
		{
			name:        "new task: 查天气",
			currentTask: "开发贪吃蛇游戏",
			newMessage:  "帮我查下杭州天气",
			relevance:   0.08,
			domainMatch: false,
			expected:    ActionQueue,
		},
		{
			name:        "status query: ?",
			currentTask: "开发贪吃蛇游戏",
			newMessage:  "？",
			relevance:   0.05,
			domainMatch: false,
			expected:    ActionStatusQuery,
		},
		{
			name:        "modification with negation: 不要Python改用C++",
			currentTask: "开发贪吃蛇游戏",
			newMessage:  "不要用Python，改用C++",
			relevance:   0.70,
			domainMatch: true,
			expected:    ActionMerge, // high relevance overrides negation
		},
		{
			name:        "cancel order — negation detected but semantically new task",
			currentTask: "开发贪吃蛇游戏",
			newMessage:  "帮我把那个订单取消了",
			relevance:   0.05,
			domainMatch: false,
			// The structure detector sees "取消" → negation. With low relevance,
			// the scheduler returns Replace. In the full pipeline, the IM
			// AsyncDispatcher would use L3 LLM tree reasoning to override this
			// to Insert when the LLM determines "取消订单" is a new task.
			// This is a known edge case where structure-only signals are insufficient.
			expected: ActionReplace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			structure := AnalyzeStructure(tt.newMessage)
			decision := Schedule(ScheduleInput{
				Relevance:   tt.relevance,
				DomainMatch: tt.domainMatch,
				Structure:   structure,
			})
			if decision.Action != tt.expected {
				t.Errorf("expected %s, got %s (reason: %s, negation: %v)",
					tt.expected, decision.Action, decision.Reason, structure.HasNegation)
			}
		})
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{"nil a", nil, []float32{1, 0}, -1.0},
		{"empty", []float32{}, []float32{1}, -1.0},
		{"length mismatch", []float32{1, 2}, []float32{1, 2, 3}, -1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, tt.b)
			if got < tt.want-0.01 || got > tt.want+0.01 {
				t.Errorf("expected ~%.2f, got %.2f", tt.want, got)
			}
		})
	}
}

func TestCharOverlapRatio(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		minRatio float64
		maxRatio float64
	}{
		{
			name:     "identical strings",
			a:        "是两个端口的反代，9399/9399 ,确认下",
			b:        "是两个端口的反代，9399/9399 ,确认下",
			minRatio: 1.0,
			maxRatio: 1.0,
		},
		{
			name:     "correction: one number changed",
			a:        "是两个端口的反代，9399/9399 ,确认下",
			b:        "是两个端口的反代，9399/9388 ,确认下",
			minRatio: 0.70,
			maxRatio: 0.99,
		},
		{
			name:     "correction: port number changed (English)",
			a:        "set up reverse proxy for ports 9399/9399, confirm",
			b:        "set up reverse proxy for ports 9399/9388, confirm",
			minRatio: 0.70,
			maxRatio: 0.99,
		},
		{
			name:     "different topic entirely",
			a:        "帮我查一下杭州的天气",
			b:        "开发一个贪吃蛇游戏",
			minRatio: 0.0,
			maxRatio: 0.30,
		},
		{
			name:     "same topic different content (supplement)",
			a:        "开发一个贪吃蛇游戏",
			b:        "用C++和CMake来实现，需要音效",
			minRatio: 0.0,
			maxRatio: 0.40,
		},
		{
			name:     "empty strings",
			a:        "",
			b:        "something",
			minRatio: 0.0,
			maxRatio: 0.0,
		},
		{
			name:     "both empty",
			a:        "",
			b:        "",
			minRatio: 1.0,
			maxRatio: 1.0,
		},
		{
			name:     "short correction: typo fix",
			a:        "用C++ cmake",
			b:        "用C++ CMake",
			minRatio: 0.60,
			maxRatio: 0.99,
		},
		{
			name:     "single char: too short for correction detection",
			a:        "是",
			b:        "否",
			minRatio: 0.0,
			maxRatio: 0.0,
		},
		{
			name:     "length ratio > 2:1 would be caught by caller guard",
			a:        "确认",
			b:        "确认一下，另外帮我加上SSL证书和负载均衡",
			minRatio: 0.0,
			maxRatio: 0.50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ratio := CharOverlapRatio(tt.a, tt.b)
			if ratio < tt.minRatio || ratio > tt.maxRatio {
				t.Errorf("CharOverlapRatio(%q, %q) = %.3f, want [%.2f, %.2f]",
					tt.a, tt.b, ratio, tt.minRatio, tt.maxRatio)
			}
		})
	}
}
