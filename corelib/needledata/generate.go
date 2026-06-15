package needledata

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

type GenerateOptions struct {
	PerIntent         int
	PerWorkflowReview int
	IncludeSynthetic  bool
	Seed              int64
}

func DefaultGenerateOptions() GenerateOptions {
	return GenerateOptions{PerIntent: 12, PerWorkflowReview: 16, IncludeSynthetic: true, Seed: 7}
}

func GenerateSeedRecords(opts GenerateOptions) []TrainingRecord {
	if opts.PerIntent <= 0 {
		opts.PerIntent = 12
	}
	if opts.PerWorkflowReview <= 0 {
		opts.PerWorkflowReview = 16
	}
	r := rand.New(rand.NewSource(opts.Seed))
	var out []TrainingRecord
	out = append(out, generateIntentRecords(r, opts.PerIntent)...)
	out = append(out, generateReviewRecords(r, opts.PerWorkflowReview)...)
	out = append(out, generateMemoryGateRecords(r)...)
	return out
}

func generateIntentRecords(r *rand.Rand, n int) []TrainingRecord {
	patterns := map[intent.IntentLabel][]string{
		intent.LabelSSH:          {"check the server logs", "ssh into the host and inspect the port", "restart the remote service", "look at nginx status", "connect to production and diagnose disk usage"},
		intent.LabelBrowser:      {"open the page and click login", "test the form in a browser", "take a screenshot of the web page", "check the localhost button", "automate filling the admin page"},
		intent.LabelSearch:       {"look up the latest docs", "search this error message", "find online references", "check the official API usage", "search related papers"},
		intent.LabelOffice:       {"make a PPT", "organize this Excel file", "generate a Word document", "export a PDF report", "calculate the spreadsheet"},
		intent.LabelCoding:       {"build a small tool", "implement this feature", "help me edit the code", "add a login page", "write a server endpoint"},
		intent.LabelBugFix:       {"fix this crash", "debug the failing test", "how should I fix this error", "repair a nil pointer issue", "diagnose the build failure"},
		intent.LabelMaintenance:  {"refactor this module", "optimize startup speed", "remove duplicate code", "upgrade dependencies", "improve the logging structure"},
		intent.LabelWorkflowTask: {"run a contract review workflow", "start the project proposal workflow", "make a business plan", "design the product plan end to end", "generate a bid response document"},
		intent.LabelNonCoding:    {"summarize this paragraph", "translate this to English", "polish this sentence", "explain this concept", "write an email"},
	}
	tools := defaultToolSummaries()
	var labels []intent.IntentLabel
	for label := range patterns {
		labels = append(labels, label)
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i] < labels[j] })
	var out []TrainingRecord
	for _, label := range labels {
		items := patterns[label]
		for i := 0; i < n; i++ {
			text := items[i%len(items)]
			if r.Intn(3) == 0 {
				text += ", and give me a short conclusion"
			}
			name := intentDecisionName(label)
			out = append(out, TrainingRecord{
				ID:       fmt.Sprintf("seed-intent-%s-%03d", label, i),
				Task:     EventIntentGate,
				Messages: []ChatMessage{{Role: "system", Content: intentSystemPrompt()}, {Role: "user", Content: text}},
				Tools:    tools,
				Expected: Decision{Name: name, Arguments: map[string]any{"label": string(label)}, Source: "synthetic"},
			})
		}
	}
	return out
}

func intentDecisionName(label intent.IntentLabel) string {
	switch label {
	case intent.LabelSSH:
		return "route_ssh"
	case intent.LabelBrowser:
		return "route_browser"
	case intent.LabelSearch:
		return "route_web_search"
	case intent.LabelOffice:
		return "route_office"
	case intent.LabelCoding, intent.LabelBugFix, intent.LabelMaintenance:
		return "route_coding"
	case intent.LabelWorkflowTask:
		return "route_workflow"
	default:
		return "no_call"
	}
}

func generateReviewRecords(r *rand.Rand, n int) []TrainingRecord {
	patterns := map[v2.ReviewIntent][]string{
		v2.ReviewIntentConfirm:    {"looks good, continue", "approved", "no issue, next step", "confirm", "go with this version"},
		v2.ReviewIntentSupplement: {"add one more risk paragraph", "make the second point more conservative", "add the budget section", "this conclusion is not clear enough", "include acceptance criteria"},
		v2.ReviewIntentSkip:       {"skip this phase", "do not do this step for now", "skip it", "this part is unnecessary", "go directly to the next item"},
		v2.ReviewIntentCancel:     {"cancel this workflow", "stop for now", "do not continue", "end the workflow", "abandon this task"},
		v2.ReviewIntentSwitchTask: {"switch tasks and check the server", "stop this and write code instead", "move to another request", "start a PPT instead", "I want to do something else"},
		v2.ReviewIntentOther:      {"what is the weather today", "who are you", "wait a second", "what does this word mean", "let us just chat"},
	}
	var keys []v2.ReviewIntent
	for k := range patterns {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	var out []TrainingRecord
	for _, k := range keys {
		items := patterns[k]
		for i := 0; i < n; i++ {
			text := items[i%len(items)]
			if r.Intn(4) == 0 {
				text = "I reviewed it. " + text
			}
			out = append(out, TrainingRecord{
				ID:       fmt.Sprintf("seed-review-%s-%03d", k, i),
				Task:     EventWorkflowReview,
				Messages: []ChatMessage{{Role: "system", Content: reviewSystemPrompt()}, {Role: "user", Content: text}},
				Expected: Decision{Name: string(k), Arguments: map[string]any{"intent": string(k)}, Source: "synthetic"},
			})
		}
	}
	return out
}

func generateMemoryGateRecords(_ *rand.Rand) []TrainingRecord {
	cases := []struct{ text, decision string }{
		{"I prefer Chinese answers in future sessions", "extract_memory"},
		{"This project deploys to the internal server by default", "extract_memory"},
		{"thanks", "no_extract"},
		{"continue", "no_extract"},
		{"Remember: run the full test suite before release", "extract_memory"},
		{"haha okay", "no_extract"},
	}
	out := make([]TrainingRecord, 0, len(cases))
	for i, tc := range cases {
		out = append(out, TrainingRecord{ID: fmt.Sprintf("seed-memory-%03d", i), Task: EventMemoryExtractGate, Messages: []ChatMessage{{Role: "system", Content: memoryGatePrompt()}, {Role: "user", Content: tc.text}}, Expected: Decision{Name: tc.decision, Source: "synthetic"}})
	}
	return out
}

func defaultToolSummaries() []ToolSpec {
	names := []string{"bash", "read_file", "write_file", "ssh", "browser", "web_search", "office", "memory", "knowledge_search", "send_file"}
	out := make([]ToolSpec, 0, len(names))
	for _, name := range names {
		out = append(out, ToolSpec{Name: name, Description: strings.ReplaceAll(name, "_", " ")})
	}
	return out
}

func intentSystemPrompt() string {
	return "Route the user request to exactly one MacLaw micro-decision function. Prefer no_call for plain chat or text-only tasks."
}

func reviewSystemPrompt() string {
	return "Classify a workflow review reply into confirm, supplement, skip, cancel, switch_task, or other."
}

func memoryGatePrompt() string {
	return "Decide whether the message contains durable user/project knowledge worth extracting into memory."
}
