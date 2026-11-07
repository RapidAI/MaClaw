package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func (c *guiHopJITCaller) executeActFree(ctx context.Context, step hopjit.StepReady) (map[string]any, error) {
	if c.handler == nil {
		return nil, errors.New("act free requires the local GUI tool host")
	}
	if c.app == nil {
		return nil, errors.New("HopJIT LLM app is unavailable")
	}
	if len(step.Tools) == 0 {
		return nil, errors.New("act free requires an explicit tool whitelist")
	}
	provider, definitions, err := c.handler.hopJITActFreeToolProvider(step, c.identity)
	if err != nil {
		return nil, err
	}
	callbacks := &hopJITActFreeCallbacks{
		ctx: ctx, cfg: c.app.GetMaclawLLMConfig(), tools: definitions, provider: provider,
		task: c.task, lookupQuery: hopLookupQuery(c.task, step),
	}
	return completeActFree(callbacks, step, c.actFreeRunner(ctx, callbacks))
}

// actFreeRunner sends a tool whitelist through the tool agent. The lookup
// answer is a JSON completion: that turn has already searched and must not
// be offered tools.
func (c *guiHopJITCaller) actFreeRunner(ctx context.Context, callbacks *hopJITActFreeCallbacks) func(string, []agent.ConversationEntry) agent.LoopResult {
	return func(prompt string, history []agent.ConversationEntry) agent.LoopResult {
		if callbacks != nil && callbacks.answerFromEvidence {
			text, err := c.call(ctx, hopLookupAnswerMessages(callbacks, prompt), true)
			if err != nil {
				return agent.LoopResult{Error: err.Error()}
			}
			return agent.LoopResult{Text: text}
		}
		return agentruntime.RunAgentTurn(callbacks, prompt, history, nil)
	}
}

func hopLookupAnswerMessages(callbacks *hopJITActFreeCallbacks, prompt string) []interface{} {
	system := "检索已经完成。你只根据给出的证据写出用户要的答案。只返回 JSON 对象，键必须严格匹配输出声明。不要粘贴检索列表，不要调用工具。"
	if callbacks != nil {
		system = callbacks.BuildSystemPrompt("", false)
	}
	return []interface{}{
		map[string]interface{}{"role": "system", "content": system},
		map[string]interface{}{"role": "user", "content": prompt},
	}
}

// completeActFree runs a whitelisted act. A string-only web_search lookup
// searches on the host, then one JSON completion writes the answer from that
// evidence. The transcript is not stored. An empty search fails before the
// model. Other whitelists use the tool agent. web_search must succeed before
// its JSON is kept. web_fetch is required only when web_search is absent.
// A failed or missed retrieval gets one correction turn.
func completeActFree(callbacks *hopJITActFreeCallbacks, step hopjit.StepReady, run func(string, []agent.ConversationEntry) agent.LoopResult) (map[string]any, error) {
	if actFreeHostOwnsLookup(callbacks, step) {
		callbacks.ensureLookupSearch(step)
		if !callbacks.calledTool("web_search") {
			if message := callbacks.toolFailure("web_search"); message != "" {
				return nil, fmt.Errorf("act free web_search failed: %s", message)
			}
			return nil, fmt.Errorf("act free finished without calling web_search")
		}
		evidence := ""
		if callbacks != nil {
			evidence = hopLookupEvidence(callbacks.retrieval["web_search"])
		}
		if evidence == "" {
			return nil, fmt.Errorf("act free finished without a web_search result")
		}
		// The search transcript is evidence. The declared text is the answer.
		return completeLookupAnswer(callbacks, step, evidence, run)
	}
	result := run(hopJITActFreePrompt(step), nil)
	if result.Error == "" && result.AskUser == nil && result.RecordAudio == nil {
		callbacks.ensureLookupSearch(step)
	}
	if required := actFreeRequiredTool(step); required != "" && !callbacks.calledTool(required) && result.Error == "" && result.AskUser == nil && result.RecordAudio == nil {
		prompt := actFreeSearchCorrection(required)
		if message := callbacks.toolFailure(required); message != "" {
			prompt = actFreeSearchFailureCorrection(required, message)
		}
		if len(result.HistoryDelta) == 0 {
			prompt = hopJITActFreePrompt(step) + "\n" + prompt
		}
		result = run(prompt, result.HistoryDelta)
	}
	if result.AskUser != nil || result.RecordAudio != nil {
		return nil, errors.New("interactive act free requires a HopJIT pause step")
	}
	if result.Error != "" {
		return nil, errors.New(result.Error)
	}
	if required := actFreeRequiredTool(step); required != "" && !callbacks.calledTool(required) {
		if message := callbacks.toolFailure(required); message != "" {
			return nil, fmt.Errorf("act free %s failed: %s", required, message)
		}
		return nil, fmt.Errorf("act free finished without calling %s", required)
	}
	var outputs map[string]any
	if err := json.Unmarshal([]byte(stripHopJITCodeFence(result.Text)), &outputs); err != nil {
		if actFreeCanFinishWithoutJSON(callbacks, step) {
			return bindActFreeRetrieval(callbacks, step, map[string]any{})
		}
		return nil, fmt.Errorf("act free returned invalid JSON: %w", err)
	}
	return bindActFreeRetrieval(callbacks, step, outputs)
}

// bindActFreeRetrieval writes the retrieval result into each string output of a
// weather, news, or price step. A string output is any declared value that can
// hold text, including an omitted type. bool, int, float, and lists stay.
// The model JSON for the string names is discarded.
// A blank value and the stable empty-search reply are not a result.
func bindActFreeRetrieval(callbacks *hopJITActFreeCallbacks, step hopjit.StepReady, outputs map[string]any) (map[string]any, error) {
	if !actFreeBindsRetrieval(callbacks, step) {
		return outputs, nil
	}
	required := actFreeRequiredTool(step)
	if required == "" {
		return outputs, nil
	}
	names := actFreeRetrievalNames(step)
	if len(names) == 0 {
		return outputs, nil
	}
	evidence := ""
	if callbacks != nil {
		evidence = hopLookupEvidence(callbacks.retrieval[required])
	}
	if evidence == "" {
		return nil, fmt.Errorf("act free finished without a %s result", required)
	}
	if outputs == nil {
		outputs = map[string]any{}
	}
	for _, name := range names {
		outputs[name] = evidence
	}
	return outputs, nil
}

func actFreeBindsRetrieval(callbacks *hopJITActFreeCallbacks, step hopjit.StepReady) bool {
	task := ""
	if callbacks != nil {
		task = callbacks.task
	}
	return hopLookupQuery(task, step) != ""
}

// actFreeCanFinishWithoutJSON reports that this lookup step's declared outputs
// are all string results. The host can search first and then ask for the answer.
// A bool, number, or list output still needs the tool-agent JSON.
func actFreeCanFinishWithoutJSON(callbacks *hopJITActFreeCallbacks, step hopjit.StepReady) bool {
	if !actFreeBindsRetrieval(callbacks, step) || actFreeRequiredTool(step) == "" || len(actFreeRetrievalNames(step)) == 0 {
		return false
	}
	for _, decl := range step.OutputSchema {
		if decl.Name != "" && !actFreeRetrievalStringType(decl.Type) {
			return false
		}
	}
	return true
}

func actFreeRetrievalNames(step hopjit.StepReady) []string {
	var names []string
	for _, decl := range step.OutputSchema {
		if decl.Name == "" || !actFreeRetrievalStringType(decl.Type) {
			continue
		}
		names = append(names, decl.Name)
	}
	return names
}

// actFreeHostOwnsLookup is a string-only web_search lookup. The host searches
// once. A later JSON completion writes the answer from that evidence.
func actFreeHostOwnsLookup(callbacks *hopJITActFreeCallbacks, step hopjit.StepReady) bool {
	if callbacks == nil || !actFreeCanFinishWithoutJSON(callbacks, step) || len(step.Tools) == 0 {
		return false
	}
	for _, name := range step.Tools {
		if strings.TrimSpace(name) != "web_search" {
			return false
		}
	}
	return true
}

func actFreeRetrievalStringType(typ string) bool {
	typ = strings.TrimSpace(typ)
	if strings.HasPrefix(typ, "[") && strings.HasSuffix(typ, "]") {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(strings.TrimRight(typ, "。．.，,"))) {
	case "bool", "boolean", "布尔", "真假", "int", "float":
		return false
	default:
		return true
	}
}

// hopLookupEvidence is retrieved text. The empty-search replies are the whole
// tool result, not a sentence inside a hit list.
func hopLookupEvidence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || hopLookupSearchMiss(text) {
		return ""
	}
	return text
}

// hopLookupAnswerPrompt asks for the user-facing answer. The evidence stays
// in the prompt and is not a valid value for a declared text output.
func hopLookupAnswerPrompt(task string, step hopjit.StepReady, evidence string) string {
	task = strings.TrimSpace(task)
	if task == "" {
		task = strings.TrimSpace(step.Goal)
	}
	if task == "" {
		task = strings.TrimSpace(step.Summary)
	}
	var keys []string
	for _, decl := range step.OutputSchema {
		name := strings.TrimSpace(decl.Name)
		if name == "" {
			continue
		}
		typ := strings.TrimSpace(decl.Type)
		if typ == "" {
			typ = "text"
		}
		keys = append(keys, name+" % "+typ)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "任务: %s\n步骤: %s\n", task, strings.TrimSpace(step.Summary))
	if instruction := strings.TrimSpace(step.Instruction); instruction != "" {
		fmt.Fprintf(&b, "说明: %s\n", instruction)
	}
	fmt.Fprintf(&b, "检索证据:\n%s\n\n根据证据写出用户要的答案。不要粘贴检索列表，不要输出“找到 N 条结果”、标题或 URL。只返回 JSON 对象，键为: %s", evidence, strings.Join(keys, ", "))
	return b.String()
}

func hopLookupAnswerCorrection() string {
	return "上一份 JSON 是空的，或整段复制了检索证据。那不是给用户的答案。用证据中的事实写简短答案，只返回 JSON。"
}

func completeLookupAnswer(callbacks *hopJITActFreeCallbacks, step hopjit.StepReady, evidence string, run func(string, []agent.ConversationEntry) agent.LoopResult) (map[string]any, error) {
	if callbacks != nil {
		callbacks.answerFromEvidence = true
	}
	task := ""
	if callbacks != nil {
		task = callbacks.task
	}
	prompt := hopLookupAnswerPrompt(task, step, evidence)
	result := run(prompt, nil)
	if outputs, ok := hopLookupAnswerFromModel(step, evidence, result); ok {
		return outputs, nil
	}
	if result.Error != "" {
		return nil, errors.New(result.Error)
	}
	if result.AskUser != nil || result.RecordAudio != nil {
		return nil, errors.New("interactive act free requires a HopJIT pause step")
	}
	result = run(prompt+"\n"+hopLookupAnswerCorrection(), nil)
	if outputs, ok := hopLookupAnswerFromModel(step, evidence, result); ok {
		return outputs, nil
	}
	if result.AskUser != nil || result.RecordAudio != nil {
		return nil, errors.New("interactive act free requires a HopJIT pause step")
	}
	if result.Error != "" {
		return nil, errors.New(result.Error)
	}
	return nil, errors.New("act free lookup answer was not written from the search evidence")
}

func hopLookupAnswerFromModel(step hopjit.StepReady, evidence string, result agent.LoopResult) (map[string]any, bool) {
	if result.Error != "" || result.AskUser != nil || result.RecordAudio != nil {
		return nil, false
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stripHopJITCodeFence(result.Text)), &parsed); err != nil || parsed == nil {
		return nil, false
	}
	outputs := map[string]any{}
	for _, decl := range step.OutputSchema {
		name := strings.TrimSpace(decl.Name)
		if name == "" {
			continue
		}
		value, ok := parsed[name]
		if !ok {
			return nil, false
		}
		if !actFreeRetrievalStringType(decl.Type) {
			outputs[name] = value
			continue
		}
		text, ok := value.(string)
		if !ok {
			return nil, false
		}
		text = strings.TrimSpace(text)
		if text == "" || hopLookupAnswerCopiesEvidence(text, evidence) {
			return nil, false
		}
		outputs[name] = text
	}
	if len(outputs) == 0 {
		return nil, false
	}
	return outputs, true
}

// hopLookupAnswerCopiesEvidence rejects the transcript as the answer. A short
// fact quoted inside a longer answer stays. A paste of the transcript, with
// or without a short prefix, does not.
func hopLookupAnswerCopiesEvidence(answer, evidence string) bool {
	answer = strings.TrimSpace(answer)
	evidence = strings.TrimSpace(evidence)
	if answer == "" || evidence == "" {
		return false
	}
	if answer == evidence {
		return true
	}
	if !strings.Contains(answer, evidence) {
		return false
	}
	return len([]rune(evidence))*5 >= len([]rune(answer))*4
}

func hopLookupSearchMiss(text string) bool {
	trimmed := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(text), "。．."))
	switch strings.ToLower(trimmed) {
	case "未找到相关结果", "没有找到相关结果", "no results found":
		return true
	default:
		return false
	}
}

// hopLookupQuery is the web_search query for a weather, news, or price step.
// The user task wins. Otherwise the longest of the goal, summary, and
// instruction that names the lookup is used.
func hopLookupQuery(task string, step hopjit.StepReady) string {
	task = strings.TrimSpace(task)
	if hopLiveLookupTask(task) {
		return task
	}
	best := ""
	for _, text := range []string{step.Goal, step.Summary, step.Instruction} {
		text = strings.TrimSpace(text)
		if !hopLiveLookupTask(text) {
			continue
		}
		if len([]rune(text)) > len([]rune(best)) {
			best = text
		}
	}
	return best
}

// applyLookupSearchQuery pins web_search to the lookup query. The model's
// query is not used. Other tools and other arguments stay as the caller sent them.
func applyLookupSearchQuery(name string, args map[string]any, query string) map[string]any {
	query = strings.TrimSpace(query)
	if name != "web_search" || query == "" {
		return args
	}
	next := make(map[string]any, len(args)+1)
	for key, value := range args {
		next[key] = value
	}
	next["query"] = query
	return next
}

type hopLookupSearchProvider struct {
	inner hopjit.ToolProvider
	query string
}

func (p hopLookupSearchProvider) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	if p.inner == nil {
		return nil, errors.New("HopJIT tool host is unavailable")
	}
	value, err := p.inner.Execute(ctx, name, applyLookupSearchQuery(name, args, p.query))
	if err != nil || name != "web_search" {
		return value, err
	}
	if hopLookupEvidence(fmt.Sprint(value)) == "" {
		return nil, errors.New("web_search returned no results")
	}
	return value, nil
}

// actFreeRequiredTool is the retrieval call this whitelist must actually make.
// web_search wins when both retrieval tools are listed. read_file is not here.
func actFreeRequiredTool(step hopjit.StepReady) string {
	fetch := false
	for _, name := range step.Tools {
		if name == "web_search" {
			return "web_search"
		}
		if name == "web_fetch" {
			fetch = true
		}
	}
	if fetch {
		return "web_fetch"
	}
	return ""
}

func actFreeSearchCorrection(name string) string {
	return "你还没有调用 " + name + "。先调用 " + name + " 取得实时结果，再只返回符合声明的 JSON。"
}

func actFreeSearchFailureCorrection(name, message string) string {
	return name + " 没有返回可用结果：" + message + "。再调用 " + name + " 取得实时结果，然后只返回符合声明的 JSON。"
}

type hopJITActFreeCallbacks struct {
	ctx                context.Context
	cfg                corelib.MaclawLLMConfig
	tools              []map[string]interface{}
	provider           *tool.HopJITPlanToolProvider
	called             map[string]int
	retrieval          map[string]string
	toolErrors         map[string]string
	lookupSearch       func(string) (string, error)
	task               string
	lookupQuery        string
	answerFromEvidence bool
}

func (c *hopJITActFreeCallbacks) GetLLMConfig() corelib.MaclawLLMConfig { return c.cfg }
func (c *hopJITActFreeCallbacks) GetMaxIterations() int                 { return config.EffectiveMaxIterations(30) }
func (c *hopJITActFreeCallbacks) BuildSystemPrompt(string, bool) string {
	if c != nil && c.answerFromEvidence {
		return "检索已经完成。你只根据给出的证据写出用户要的答案。只返回 JSON 对象，键必须严格匹配输出声明。不要粘贴检索列表，不要调用工具。"
	}
	return "你在 HopJIT 的 act free 步骤中工作。只能使用当前列出的工具。白名单里有 web_search 时必须先调用它。没有 web_search 但有 web_fetch 时必须先调用 web_fetch。完成任务后只返回符合声明的 JSON 输出，不要 Markdown 围栏。"
}
func (c *hopJITActFreeCallbacks) BuildTools(string) []map[string]interface{} {
	if c != nil && c.answerFromEvidence {
		return nil
	}
	return c.tools
}
func (c *hopJITActFreeCallbacks) calledTool(name string) bool {
	return c != nil && c.called[name] > 0
}

func (c *hopJITActFreeCallbacks) toolFailure(name string) string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.toolErrors[name])
}

// ensureLookupSearch runs web_search when a lookup step declares a string
// output and the model did not call it. The lookup query is already known.
// web_fetch is left to the model because the host does not choose a URL.
// A step with no string output still requires the model to call the tool.
func (c *hopJITActFreeCallbacks) ensureLookupSearch(step hopjit.StepReady) {
	if c == nil || c.calledTool("web_search") || actFreeRequiredTool(step) != "web_search" {
		return
	}
	if !actFreeBindsRetrieval(c, step) || len(actFreeRetrievalNames(step)) == 0 {
		return
	}
	query := strings.TrimSpace(c.lookupQuery)
	if query == "" {
		query = hopLookupQuery(c.task, step)
	}
	if query == "" {
		return
	}
	text, err := c.executeLookupSearch(query)
	if err != nil || hopLookupEvidence(text) == "" {
		if c.toolErrors == nil {
			c.toolErrors = map[string]string{}
		}
		if err != nil {
			c.toolErrors["web_search"] = err.Error()
			return
		}
		c.toolErrors["web_search"] = "web_search returned no results"
		return
	}
	if c.called == nil {
		c.called = map[string]int{}
	}
	c.called["web_search"]++
	if c.retrieval == nil {
		c.retrieval = map[string]string{}
	}
	c.retrieval["web_search"] = text
}

func (c *hopJITActFreeCallbacks) executeLookupSearch(query string) (string, error) {
	if c.lookupSearch != nil {
		return c.lookupSearch(query)
	}
	if c.provider == nil {
		return "", errors.New("HopJIT tool host is unavailable")
	}
	value, err := c.provider.Execute(c.ctx, "web_search", applyLookupSearchQuery("web_search", nil, query))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(fmt.Sprint(value)), nil
}

func (c *hopJITActFreeCallbacks) ExecuteTool(name, argsJSON string) string {
	args, err := agentruntime.ParseToolArgumentsObject(argsJSON)
	if err != nil {
		return "Error: hopjit_tool_arguments_invalid: " + err.Error()
	}
	args = applyLookupSearchQuery(name, args, c.lookupQuery)
	value, err := c.provider.Execute(c.ctx, name, args)
	if err != nil {
		if c.toolErrors == nil {
			c.toolErrors = map[string]string{}
		}
		c.toolErrors[name] = err.Error()
		return "Error: " + err.Error()
	}
	if c.called == nil {
		c.called = map[string]int{}
	}
	c.called[name]++
	if c.retrieval == nil {
		c.retrieval = map[string]string{}
	}
	c.retrieval[name] = fmt.Sprint(value)
	return fmt.Sprint(value)
}
func (c *hopJITActFreeCallbacks) OnToken(string)      {}
func (c *hopJITActFreeCallbacks) OnProgress(string)   {}
func (c *hopJITActFreeCallbacks) OnToolCall(string)   {}
func (c *hopJITActFreeCallbacks) OnToolResult(string) {}
func (c *hopJITActFreeCallbacks) ShouldStop() bool    { return c.ctx.Err() != nil }

func (h *IMMessageHandler) hopJITActFreeToolProvider(step hopjit.StepReady, identity hopJITExecutionIdentity) (*tool.HopJITPlanToolProvider, []map[string]interface{}, error) {
	if h == nil || h.registry == nil {
		return nil, nil, errors.New("HopJIT tool registry is unavailable")
	}
	identity.PrincipalID = strings.TrimSpace(identity.PrincipalID)
	identity.PolicyOwnerID = strings.TrimSpace(identity.PolicyOwnerID)
	identity.SessionID = strings.TrimSpace(identity.SessionID)
	if identity.PrincipalID == "" || identity.PolicyOwnerID == "" {
		return nil, nil, errors.New("HopJIT act free requires an authenticated execution owner")
	}
	if identity.SessionID == "" {
		identity.SessionID = strings.TrimSpace(step.RunID)
	}
	if identity.SessionID == "" {
		return nil, nil, errors.New("HopJIT act free requires a session identity")
	}
	rootTaskID := strings.TrimSpace(step.RunID)
	if rootTaskID == "" {
		rootTaskID = identity.SessionID
	}
	capabilityRegistry := newIMSemanticCapabilityRegistry()
	catalog := tool.NewToolCatalog(capabilityRegistry)
	providers := make([]tool.ProviderSpec, 0, len(step.Tools))
	needs := make([]tool.CapabilityNeed, 0, len(step.Tools))
	pins := make([]tool.ProviderPin, 0, len(step.Tools))
	definitions := make([]map[string]interface{}, 0, len(step.Tools))
	schemas := map[string]map[string]interface{}{}
	for _, name := range step.Tools {
		registered, ok := h.registry.Get(name)
		if !ok || registered == nil || registered.Status != RegToolAvailable || len(registered.CapabilityProvisions) == 0 {
			return nil, nil, fmt.Errorf("HopJIT tool is unavailable or ungoverned: %s", name)
		}
		definition := registeredToolToDef(*registered)
		schema, err := semanticInvocationSchemaForRegisteredTool(*registered, definition)
		if err != nil {
			return nil, nil, err
		}
		authorization, err := tool.NewParameterAuthorization(schema)
		if err != nil {
			return nil, nil, err
		}
		providers = append(providers, tool.ProviderSpec{
			AdapterName: registered.Name,
			Binding: tool.ProviderBinding{
				Kind: semanticProviderKind(*registered), ProviderID: semanticProviderID(*registered), ImplementationID: registered.Name,
				SchemaDigest: tool.SchemaDigest(canonicalToolDefinitionBytes(definition)),
			},
			ParameterAuthorization: authorization,
			Provides:               registered.CapabilityProvisions, Consumes: registered.SemanticConsumes, Produces: registered.SemanticProduces,
			Effects: registered.SemanticEffects, Ready: true,
		})
		provision := registered.CapabilityProvisions[0]
		needs = append(needs, tool.CapabilityNeed{ID: "hopjit:" + name, Capability: provision.Capability, Qualifiers: provision.Qualifiers, Required: true})
		pins = append(pins, tool.ProviderPin{Capability: provision.Capability, Kind: semanticProviderKind(*registered), ProviderID: semanticProviderID(*registered), ImplementationID: registered.Name})
		definitions = append(definitions, definition)
		schemas[name] = schema
	}
	snapshot, err := catalog.Publish(providers, time.Now().UTC())
	if err != nil {
		return nil, nil, err
	}
	plan, err := tool.NewToolPlanner(capabilityRegistry).Plan(tool.RouteRequest{
		RootTaskID: rootTaskID, Snapshot: snapshot, Needs: needs, Pins: pins, Now: time.Now().UTC(),
	})
	if err != nil || len(plan.Unmet) > 0 {
		return nil, nil, fmt.Errorf("HopJIT act free plan failed: %v %+v", err, plan.Unmet)
	}
	issuer, err := h.semanticInvocationIssuer()
	if err != nil {
		return nil, nil, err
	}
	var executions tool.PlanExecutionStore = tool.NewMemoryPlanExecutionStore()
	if h.app != nil {
		coordinator, err := h.app.semanticExecutionCoordinatorForApp()
		if err != nil {
			return nil, nil, err
		}
		executions = coordinator.Executions
	}
	executor, err := tool.NewPlanExecutor(issuer, executions)
	if err != nil {
		return nil, nil, err
	}
	turnID := step.StepID
	if step.IterationID != "" {
		turnID += "@" + step.IterationID
	}
	scope := tool.InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: identity.SessionID, TurnID: turnID, PrincipalID: identity.PrincipalID}
	grants, err := issuer.IssueReady(plan, scope, time.Minute, nil)
	if err != nil {
		return nil, nil, err
	}
	grantByName := map[string]tool.InvocationGrant{}
	for _, grant := range grants {
		grantByName[grant.AdapterName] = grant
	}
	var resultStore tool.HopJITResultStore = h.hopJITResults
	if resultStore == nil && h.app != nil {
		resultStore = newFileHopJITResultStore(hopJITResultRoot(h.app.GetDataDir(), identity))
	}
	provider := &tool.HopJITPlanToolProvider{
		Executor: executor, Scope: scope, Plan: plan, Grants: grantByName, Schemas: schemas,
		ResultStore: resultStore, AllowResultRecovery: step.Recovered,
		Invoke: func(ctx context.Context, selection tool.PlannedSelection, request tool.CanonicalRequest) tool.SelectionExecutionResult {
			ctx = withTrustedAuditPrincipal(ctx, identity.PrincipalID)
			result := h.executeToolDetailedWithRuntimeContext(ctx, identity.PolicyOwnerID, true, identity.Platform, selection.AdapterName, string(request.CanonicalJSON), step.Summary, nil)
			switch result.Outcome {
			case toolOutcomeSucceeded:
				return tool.SelectionExecutionResult{Result: result.Text, Succeeded: true}
			case toolOutcomeUncertain:
				if result.FailureKind == toolFailureApprovalRequired || isRegisteredToolApprovalWaitText(result.Text) {
					return tool.SelectionExecutionResult{Result: result.Text, Paused: true, ReasonCode: "hopjit_tool_approval_required"}
				}
				return tool.SelectionExecutionResult{Result: result.Text, Unknown: true, ReasonCode: "hopjit_tool_outcome_unknown"}
			default:
				reason := strings.TrimSpace(result.Text)
				if reason == "" {
					reason = "hopjit_tool_failed"
				}
				return tool.SelectionExecutionResult{Result: result.Text, ReasonCode: reason}
			}
		},
	}
	return provider, definitions, nil
}

func hopJITActFreePrompt(step hopjit.StepReady) string {
	return fmt.Sprintf("步骤: %s\n%s", step.Summary, step.Context.Render())
}
