package guiapp

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// NewTaskTypeSuggestion is the wizard's reading of one new-task command.
// Suggested is "chat" or "coding" only when Confident is true. An empty
// suggestion means the classifiers could not tell, and the wizard keeps the
// type the user already picked.
type NewTaskTypeSuggestion struct {
	Suggested string `json:"suggested"`
	Confident bool   `json:"confident"`
	Reason    string `json:"reason,omitempty"`
	Source    string `json:"source,omitempty"`
}

const (
	// Local embedding is usually tens of milliseconds once the model is warm.
	// The cap only stops a cold load from holding task creation open.
	newTaskTypeLocalTimeout = 3 * time.Second
	newTaskTypeHeadTimeout  = 2 * time.Second
	// The LLM is the last reader. Four seconds is enough for a short JSON
	// reply and keeps an unsure command from stalling create.
	newTaskTypeLLMTimeout = 4 * time.Second
	// Coding changes the workbench, so the local reading must clear the same
	// bar Layer 2 uses before it skips the tree. Chat early-exit can accept
	// the lookup floor. A same-family agreement is a weaker exit, so chat
	// uses the higher floor and does not nag on a 0.71 score.
	newTaskTypeCodingMinConfidence    = 0.78
	newTaskTypeChatMinConfidence      = 0.70
	newTaskTypeChatAgreeMinConfidence = 0.75
	newTaskTypeSameFamilyMinGap       = 0.06
	newTaskTypeLLMMinConfidence       = 0.70
	newTaskCommandMaxRunes            = 500
)

// embeddingRead is how the local embedding step finished.
// unavailable: nothing was called, so the head may still run.
// ready: a result came back, and it may still abstain.
// timeout: the embedder did not return. The head uses that same model.
type embeddingRead int

const (
	embeddingUnavailable embeddingRead = iota
	embeddingReady
	embeddingTimeout
)

// newTaskTypeSystemPrompt is the command adapter for 会话 versus 编程.
// Short imperatives are the usual new-task text. Mentioning a file, an
// error, or the word "code" is not enough to pick coding.
const newTaskTypeSystemPrompt = `你判断一条「新建任务」命令应该用哪种任务类型。只输出 JSON：{"type":"chat"|"coding"|"unsure","confidence":0到1的小数}

coding（编程）：命令是要在代码仓库里开发、修改、调试、重构、补测试，或实现软件功能。
例如：修复登录空指针；给 parser 补单元测试；重构 auth 模块；实现导出 CSV 的接口；看看为什么 go test 失败并改掉；fix the nil pointer in login；add a settings page component。

chat（会话）：命令是问答、查询、翻译、总结、写信、做文档或幻灯片、解释原因，而不是改软件。
例如：北京今天天气；写一封请假邮件；把这段话翻译成英文；总结会议纪要；生成周报 pdf；这段报错是什么意思；现在几点；做一个项目介绍 PPT；explain this stack trace。

命令常常是短祈使句。提到代码、报错或文件，但只要是在解释、翻译或写文档，仍是 chat。只有要改代码、跑开发任务，或在仓库里实现功能时才是 coding。
太短或两种都像（继续、看看、处理一下）用 unsure。不要输出解释。`

// SuggestNewTaskType reads a new-task command with the local intent model
// and, when that reading is not confident, with the configured LLM.
// It never fails the create: an unsure or unavailable classifier returns an
// empty suggestion so the wizard keeps the selected type.
func (a *App) SuggestNewTaskType(text string) NewTaskTypeSuggestion {
	command := newTaskCommandForType(text)
	if a == nil || command == "" {
		return NewTaskTypeSuggestion{}
	}
	suggestion := suggestNewTaskType(command, a.taskTypeSuggestDeps())
	if suggestion.Confident {
		log.Printf("[task-type] suggested=%s source=%s", suggestion.Suggested, suggestion.Source)
	}
	return suggestion
}

type taskTypeSuggestDeps struct {
	classify func(text string) (intent.ClassificationResult, embeddingRead)
	probe    func(ctx context.Context, text string, prior intent.ClassificationResult) (label string, prob, strong float64, ok bool)
	askLLM   func(text string) (string, bool)
}

func (a *App) taskTypeSuggestDeps() taskTypeSuggestDeps {
	return taskTypeSuggestDeps{
		classify: func(text string) (intent.ClassificationResult, embeddingRead) {
			if a == nil || a.unifiedClassifier == nil {
				return intent.ClassificationResult{}, embeddingUnavailable
			}
			return classifyTaskTypeLocal(a.unifiedClassifier, text)
		},
		probe: func(ctx context.Context, text string, prior intent.ClassificationResult) (string, float64, float64, bool) {
			if a == nil {
				return "", 0, 0, false
			}
			probe := a.localL3Runtime().ProbeFunc()
			if probe == nil {
				return "", 0, 0, false
			}
			if a.unifiedClassifier != nil {
				ctx = a.unifiedClassifier.WithEmbeddingQuery(ctx, text, prior)
			}
			return probe(ctx, text)
		},
		askLLM: func(text string) (string, bool) {
			return a.askNewTaskTypeLLM(text)
		},
	}
}

func suggestNewTaskType(command string, deps taskTypeSuggestDeps) NewTaskTypeSuggestion {
	command = strings.TrimSpace(command)
	if command == "" || newTaskCommandIsThin(command) {
		return NewTaskTypeSuggestion{}
	}
	skipHead := false
	var prior intent.ClassificationResult
	if deps.classify != nil {
		result, read := deps.classify(command)
		switch read {
		case embeddingReady:
			prior = result
			if suggested, confident := taskTypeFromClassification(result); confident {
				return NewTaskTypeSuggestion{
					Suggested: suggested,
					Confident: true,
					Source:    "local",
					Reason:    strings.TrimSpace(result.Reason),
				}
			}
		case embeddingTimeout:
			skipHead = true
		}
	}
	if !skipHead && deps.probe != nil {
		if label, prob, strong, ok := probeNewTaskTypeHead(deps.probe, command, prior); ok {
			if suggested, confident := taskTypeFromHead(label, prob, strong); confident {
				return NewTaskTypeSuggestion{
					Suggested: suggested,
					Confident: true,
					Source:    "local",
					Reason:    "local head",
				}
			}
		}
	}
	if deps.askLLM != nil {
		if raw, ok := deps.askLLM(command); ok {
			if suggested, confidence, parsed := parseNewTaskTypeReply(raw); parsed {
				return NewTaskTypeSuggestion{
					Suggested: suggested,
					Confident: true,
					Source:    "llm",
					Reason:    strconv.FormatFloat(confidence, 'f', 2, 64),
				}
			}
		}
	}
	return NewTaskTypeSuggestion{}
}

func classifyTaskTypeLocal(uic *intent.UnifiedIntentClassifier, text string) (intent.ClassificationResult, embeddingRead) {
	if uic == nil {
		return intent.ClassificationResult{}, embeddingUnavailable
	}
	ch := make(chan intent.ClassificationResult, 1)
	go func() {
		defer func() {
			if recover() != nil {
				select {
				case ch <- intent.ClassificationResult{Primary: intent.LabelUnknown, Degraded: true}:
				default:
				}
			}
		}()
		result := uic.ClassifyEmbeddingOnly(intent.MessageContext{Text: text})
		select {
		case ch <- result:
		default:
		}
	}()
	select {
	case result := <-ch:
		return result, embeddingReady
	case <-time.After(newTaskTypeLocalTimeout):
		return intent.ClassificationResult{}, embeddingTimeout
	}
}

func probeNewTaskTypeHead(probe func(context.Context, string, intent.ClassificationResult) (string, float64, float64, bool), text string, prior intent.ClassificationResult) (string, float64, float64, bool) {
	if probe == nil {
		return "", 0, 0, false
	}
	type headResult struct {
		label  string
		prob   float64
		strong float64
		ok     bool
	}
	ch := make(chan headResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), newTaskTypeHeadTimeout)
	defer cancel()
	go func() {
		defer func() { recover() }()
		label, prob, strong, ok := probe(ctx, text, prior)
		select {
		case ch <- headResult{label: label, prob: prob, strong: strong, ok: ok}:
		default:
		}
	}()
	select {
	case got := <-ch:
		return got.label, got.prob, got.strong, got.ok
	case <-ctx.Done():
		return "", 0, 0, false
	}
}

// taskTypeFromClassification maps a local embedding reading onto the
// wizard's two types. An early exit is enough. Without one, a same-family
// lead can still decide: the runner-up is empty, unmapped, or the same
// type, and the gap is wide enough. A cross-family lead abstains so the
// head or the LLM can read the wording. shell_command and ssh stay unmapped.
func taskTypeFromClassification(result intent.ClassificationResult) (string, bool) {
	if result.Degraded || !confidenceIsFinite(result.Confidence) || !confidenceIsFinite(result.RunnerUpScore) {
		return "", false
	}
	kind := newTaskKindForLabel(result.Primary)
	if kind == "" {
		return "", false
	}
	if result.EmbeddingEarlyExit {
		if result.Confidence >= newTaskTypeFloor(kind, false) {
			return kind, true
		}
		return "", false
	}
	if !sameFamilyReading(kind, result) {
		return "", false
	}
	if result.RunnerUpScore > 0 && result.Confidence-result.RunnerUpScore < newTaskTypeSameFamilyMinGap {
		return "", false
	}
	if result.Confidence >= newTaskTypeFloor(kind, true) {
		return kind, true
	}
	return "", false
}

func newTaskTypeFloor(kind string, agreement bool) float64 {
	if kind == "coding" {
		return newTaskTypeCodingMinConfidence
	}
	if agreement {
		return newTaskTypeChatAgreeMinConfidence
	}
	return newTaskTypeChatMinConfidence
}

// sameFamilyReading is true when no rival label belongs to the other type.
// An unmapped runner-up is not a rival. A companion in Secondary is.
func sameFamilyReading(kind string, result intent.ClassificationResult) bool {
	if !sameFamilyLabel(kind, result.RunnerUp) {
		return false
	}
	for _, label := range result.Secondary {
		if !sameFamilyLabel(kind, label) {
			return false
		}
	}
	return true
}

func sameFamilyLabel(kind string, label intent.IntentLabel) bool {
	if strings.TrimSpace(string(label)) == "" {
		return true
	}
	other := newTaskKindForLabel(label)
	return other == "" || other == kind
}

func taskTypeFromHead(label string, prob, strong float64) (string, bool) {
	if !confidenceIsFinite(prob) || !confidenceIsFinite(strong) || strong <= 0 || prob < strong || prob <= 0 || prob > 1 {
		return "", false
	}
	kind := newTaskKindForLabel(intent.IntentLabel(strings.ToLower(strings.TrimSpace(label))))
	if kind == "" {
		return "", false
	}
	return kind, true
}

func confidenceIsFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// newTaskKindForLabel is the 会话 / 编程 split for one intent label.
// Coding is repository work. Chat is everything the session can already do
// without opening a coding workbench. Host commands and remote shells stay
// unmapped: the wording decides, and the LLM prompt is that decider.
func newTaskKindForLabel(label intent.IntentLabel) string {
	switch label {
	case intent.LabelCoding, intent.LabelBugFix, intent.LabelMaintenance,
		intent.LabelGitInspect, intent.LabelGitMutate, intent.LabelDelegateTask:
		return "coding"
	case intent.LabelNonCoding, intent.LabelBrowser, intent.LabelSearch,
		intent.LabelLiveData, intent.LabelLiveDataVisual,
		intent.LabelDocumentDelivery, intent.LabelDocumentGenerate,
		intent.LabelDocumentRead, intent.LabelDocumentOpen, intent.LabelAttachmentDelivery,
		intent.LabelBusinessData, intent.LabelDatabase, intent.LabelOffice,
		intent.LabelScreenshot, intent.LabelComputerUse,
		intent.LabelKnowledgeWrite, intent.LabelKnowledgeRead, intent.LabelKnowledgeAdmin,
		intent.LabelCurrentTime,
		intent.LabelAudioRecord, intent.LabelAudioTranscribe, intent.LabelAudioSynthesize, intent.LabelAudioDeliver,
		intent.LabelWebFetch, intent.LabelFileDownload, intent.LabelFileRead, intent.LabelFileWrite, intent.LabelFileDelete,
		intent.LabelAppLaunch, intent.LabelScheduleManage, intent.LabelScheduleDispatch,
		intent.LabelConfigManage, intent.LabelMemoryManage, intent.LabelTaskTrack, intent.LabelGoalManage, intent.LabelTemplateManage,
		intent.LabelSessionManage, intent.LabelWorkflowTask,
		intent.LabelWorklogRead, intent.LabelWorklogUpdate,
		intent.LabelMailboxRead, intent.LabelMailboxSend, intent.LabelMailboxConfigure, intent.LabelMailboxDelete,
		intent.LabelAuditRead:
		return "chat"
	default:
		return ""
	}
}

// newTaskCommandForType keeps the instruction a new task would actually run.
// /goal <objective> is that instruction, and the objective must be a separate
// token. Other slash commands stay on the open conversation and are not
// classified. A leading fullwidth slash counts.
func newTaskCommandForType(text string) string {
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "\uFEFF"))
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "／") {
		text = "/" + strings.TrimPrefix(text, "／")
	}
	if strings.HasPrefix(text, "/") {
		lower := strings.ToLower(text)
		if !strings.HasPrefix(lower, "/goal") {
			return ""
		}
		rest := text[len("/goal"):]
		if rest != "" {
			r, _ := utf8.DecodeRuneInString(rest)
			if !unicode.IsSpace(r) {
				return ""
			}
		}
		rest = strings.TrimSpace(rest)
		switch strings.ToLower(rest) {
		case "", "status", "get", "pause", "resume", "cancel", "clear", "暂停", "继续", "恢复", "取消":
			return ""
		}
		return truncateRunes(rest, newTaskCommandMaxRunes)
	}
	return truncateRunes(text, newTaskCommandMaxRunes)
}

// newTaskCommandIsThin is an abstention, not a type guess. These are the
// fillers the prompt already calls unsure, plus a single character.
func newTaskCommandIsThin(command string) bool {
	if utf8.RuneCountInString(strings.TrimSpace(command)) <= 1 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "继续", "看看", "处理一下", "continue":
		return true
	default:
		return false
	}
}

func (a *App) askNewTaskTypeLLM(text string) (string, bool) {
	if a == nil {
		return "", false
	}
	cfg := attachLightweightHubHint(a.GetMaclawLLMConfig(), llm.TaskIntent)
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return "", false
	}
	messages := []interface{}{
		map[string]string{"role": "system", "content": newTaskTypeSystemPrompt},
		map[string]string{"role": "user", "content": truncateRunes(strings.TrimSpace(text), newTaskCommandMaxRunes)},
	}
	ctx, cancel := context.WithTimeout(context.Background(), newTaskTypeLLMTimeout)
	defer cancel()
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "new-task-type"})
	client := &http.Client{Timeout: newTaskTypeLLMTimeout}
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, client, newTaskTypeLLMTimeout)
	if err != nil || resp == nil || strings.TrimSpace(resp.Content) == "" {
		if err != nil {
			log.Printf("[task-type] LLM classify failed: %v", err)
		}
		return "", false
	}
	return resp.Content, true
}

// parseNewTaskTypeReply accepts the JSON object, including a confidence that
// arrived as a string, and the Chinese type names. The confidence has to be
// a finite number from the floor through 1. NaN compares false against the
// floor, so it is rejected on its own.
func parseNewTaskTypeReply(raw string) (suggested string, confidence float64, ok bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```JSON")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "```"))
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return "", 0, false
	}
	var parsed struct {
		Type       string          `json:"type"`
		Confidence json.RawMessage `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return "", 0, false
	}
	confidence = parseNewTaskConfidence(parsed.Confidence)
	switch normalizeNewTaskTypeName(parsed.Type) {
	case "chat", "coding":
		if !newTaskLLMConfidenceOK(confidence) {
			return "", confidence, false
		}
		return normalizeNewTaskTypeName(parsed.Type), confidence, true
	default:
		return "", confidence, false
	}
}

func newTaskLLMConfidenceOK(confidence float64) bool {
	return confidenceIsFinite(confidence) && confidence >= newTaskTypeLLMMinConfidence && confidence <= 1
}

func parseNewTaskConfidence(raw json.RawMessage) float64 {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return 0
	}
	return number
}

func normalizeNewTaskTypeName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "coding", "code", "编程", "編程", "程式":
		return "coding"
	case "chat", "conversation", "non_coding", "会话", "會話":
		return "chat"
	default:
		return ""
	}
}
