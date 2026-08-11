package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/lansengergroupsummary"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

// lansengerGroupSummaryService buffers group chat and handles /summary.
type lansengerGroupSummaryService struct {
	mu    sync.Mutex
	app   *App
	store *lansengergroupsummary.Store
	// inFlight prevents concurrent /summary for the same group.
	inFlight map[string]struct{}
	// shared HTTP client for summary LLM calls (connection reuse).
	httpClient *http.Client
}

const (
	lansengerGroupSummaryLLMTimeout = 90 * time.Second
	// Wall-clock budget for the full multi-wave map+reduce pipeline.
	lansengerGroupSummaryOverallTimeout = 8 * time.Minute
	// Bound concurrent map-phase LLM calls to control cost/latency spikes.
	lansengerGroupSummaryMapConcurrency = 2
)

func (m *lansengerGatewayManager) groupSummaryService() *lansengerGroupSummaryService {
	if m == nil || m.app == nil {
		return nil
	}
	// Fast path: lock-free load after first init.
	if svc := m.groupSummaryAtomic.Load(); svc != nil {
		return svc
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if svc := m.groupSummaryAtomic.Load(); svc != nil {
		return svc
	}
	base := m.app.getMaclawBaseDir()
	svc := &lansengerGroupSummaryService{
		app:      m.app,
		store:    lansengergroupsummary.NewStore(base),
		inFlight: make(map[string]struct{}),
		httpClient: &http.Client{
			// Per-request contexts enforce tighter deadlines.
			Timeout: 0,
		},
	}
	m.groupSummary = svc // keep legacy field for any direct readers / tests
	m.groupSummaryAtomic.Store(svc)
	return svc
}

// recordGroupMessage appends a group chat line for later /summary.
// Called for every delivered group event (before mention gate), like 盯人.
func (m *lansengerGatewayManager) recordGroupMessage(msg lansenger.IncomingMessage) {
	if m == nil || !isLansengerGroupMessage(msg) {
		return
	}
	svc := m.groupSummaryService()
	if svc == nil || svc.store == nil {
		return
	}
	groupID := strings.TrimSpace(msg.GroupID)
	if groupID == "" {
		return
	}
	text := strings.TrimSpace(msg.Text)
	if stripped := stripLansengerBotMentions(msg); strings.TrimSpace(stripped) != "" {
		// Prefer cleaned text so @Bot tokens do not dominate the transcript.
		text = strings.TrimSpace(stripped)
	}
	if text == "" {
		// Non-text media: keep a short placeholder so media-only bursts still show up.
		if msg.MediaType != "" {
			text = "[媒体: " + mediaLabel(msg.MediaType) + "]"
		} else {
			return
		}
	}
	if _, err := svc.store.Append(
		m.groupSummaryScopeID(groupID),
		msg.GroupName,
		msg.MessageID,
		msg.FromUserID,
		msg.SenderName,
		text,
		time.Now(),
	); err != nil {
		log.Printf("[lansenger-summary] append group=%s: %v", groupID, err)
	}
}

// tryHandleGroupSummaryCommand handles @Bot /summary [start] in a group.
//
//   - /summary        — generate a discussion summary from the current cursor
//   - /summary start  — set the cursor to "now" so earlier buffered messages
//     are ignored; only messages after this point are included in later /summary
//
// Returns true when the command was claimed (success or user-visible error).
func (m *lansengerGatewayManager) tryHandleGroupSummaryCommand(msg lansenger.IncomingMessage) bool {
	if m == nil || !isLansengerGroupMessage(msg) {
		return false
	}
	clean := strings.TrimSpace(stripLansengerBotMentions(msg))
	kind := lansengergroupsummary.ParseSummaryCommand(clean)
	if kind == lansengergroupsummary.SummaryCmdNone {
		return false
	}
	svc := m.groupSummaryService()
	if svc == nil {
		return false
	}

	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return true
	}

	groupID := strings.TrimSpace(msg.GroupID)
	opts := m.currentGroupOpts()
	if groupID == "" {
		_ = gw.SendText(context.Background(), buildLansengerOutgoingTextEx(
			msg, "无法识别群 ID，无法处理摘要命令。", opts, true))
		return true
	}
	summaryKey := m.groupSummaryScopeID(groupID)

	switch kind {
	case lansengergroupsummary.SummaryCmdStart:
		// Serialize with in-flight summary so cursor moves are not racing a mark.
		if !svc.tryBegin(summaryKey) {
			_ = gw.SendText(context.Background(), buildLansengerOutgoingTextEx(
				msg, "该群正在生成摘要，请稍后再试。", opts, true))
			return true
		}
		// defer runs when tryHandle returns (this case returns immediately after).
		defer svc.end(summaryKey)
		body := svc.markSummaryStart(summaryKey, msg.GroupName)
		_ = gw.SendText(context.Background(), buildLansengerOutgoingTextEx(msg, body, opts, true))
		return true

	case lansengergroupsummary.SummaryCmdUnknown:
		_ = gw.SendText(context.Background(), buildLansengerOutgoingTextEx(
			msg, "用法：\n· /summary — 生成从起点（或上次摘要）以来的群讨论摘要\n· /summary start — 将此处设为新起点，忽略此前消息", opts, true))
		return true

	case lansengergroupsummary.SummaryCmdRun:
		// Claim the in-flight slot before acknowledging, so concurrent /summary
		// gets a clear busy reply instead of duplicate "正在生成…".
		if !svc.tryBegin(summaryKey) {
			_ = gw.SendText(context.Background(), buildLansengerOutgoingTextEx(
				msg, "该群正在生成摘要，请稍后再试。", opts, true))
			return true
		}

		_ = gw.SendText(context.Background(), buildLansengerOutgoingTextEx(
			msg, "正在生成群讨论摘要…", opts, true))

		go func() {
			defer svc.end(summaryKey)
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[lansenger-summary] panic: %v", r)
				}
			}()
			body := svc.generateSummary(summaryKey, msg.GroupName)
			if err := gw.SendText(context.Background(), buildLansengerOutgoingText(msg, body, opts)); err != nil {
				log.Printf("[lansenger-summary] send group=%s: %v", groupID, err)
			}
		}()
		return true
	}
	return false
}

// groupSummaryScopeID isolates the otherwise group-keyed summary store across
// bot profiles. The legacy singleton keeps its historical raw group key so an
// upgrade does not silently hide existing summaries; every profile runtime gets
// an unambiguous profile-qualified key instead.
func (m *lansengerGatewayManager) groupSummaryScopeID(groupID string) string {
	groupID = strings.TrimSpace(groupID)
	if m == nil || m.profile == nil {
		return groupID
	}
	profileID := strings.TrimSpace(m.profileID())
	return fmt.Sprintf("lansenger-summary:%d:%s:group:%d:%s", len(profileID), profileID, len(groupID), groupID)
}

// markSummaryStart advances the summary cursor through the latest buffered
// message (including this /summary start line) so earlier content is ignored.
// Does not set LastSummaryAt (not a completed summary).
func (s *lansengerGroupSummaryService) markSummaryStart(groupID, groupName string) string {
	if s == nil || s.store == nil {
		return "群摘要服务不可用。"
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return "无法识别群 ID。"
	}
	st, err := s.store.LoadState(groupID)
	if err != nil {
		log.Printf("[lansenger-summary] start load state group=%s: %v", groupID, err)
		return "设置摘要起点失败，请稍后重试。"
	}
	// NextSeq is the last assigned seq (including the just-recorded /summary start).
	// When NextSeq==0 there is nothing buffered; still report success as a no-op.
	cursor := st.NextSeq
	if cursor < st.LastSummarySeq {
		cursor = st.LastSummarySeq
	}
	if cursor > 0 {
		if err := s.store.MarkCursor(groupID, cursor); err != nil {
			log.Printf("[lansenger-summary] start mark group=%s: %v", groupID, err)
			return "设置摘要起点失败，请稍后重试。"
		}
	}
	gn := strings.TrimSpace(groupName)
	if gn == "" {
		gn = strings.TrimSpace(st.GroupName)
	}
	if gn != "" {
		return fmt.Sprintf("已设置摘要起点（%s）。\n此前消息将被忽略；之后的新讨论可用 /summary 生成摘要。", gn)
	}
	return "已设置摘要起点。\n此前消息将被忽略；之后的新讨论可用 /summary 生成摘要。"
}

func (s *lansengerGroupSummaryService) tryBegin(groupID string) bool {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFlight == nil {
		s.inFlight = make(map[string]struct{})
	}
	if _, busy := s.inFlight[groupID]; busy {
		return false
	}
	s.inFlight[groupID] = struct{}{}
	return true
}

func (s *lansengerGroupSummaryService) end(groupID string) {
	groupID = strings.TrimSpace(groupID)
	s.mu.Lock()
	delete(s.inFlight, groupID)
	s.mu.Unlock()
}

// generateSummary builds the group discussion summary text. Callers that need
// mutual exclusion must hold the in-flight slot (tryBegin/end).
func (s *lansengerGroupSummaryService) generateSummary(groupID, groupName string) string {
	if s == nil || s.store == nil {
		return "群摘要服务不可用。"
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return "无法识别群 ID。"
	}

	if s.app == nil || !s.app.isMaclawLLMConfigured() {
		return "未配置 LLM，无法生成群摘要。请先在设置中配置模型。"
	}

	rawMsgs, st, err := s.store.LoadNew(groupID)
	if err != nil {
		log.Printf("[lansenger-summary] load group=%s: %v", groupID, err)
		return "读取群消息失败，请稍后重试。"
	}
	// Cursor covers everything loaded (including the /summary trigger).
	cursorSeq := lansengergroupsummary.MaxSeq(rawMsgs)
	msgs := lansengergroupsummary.FilterSummaryCommands(rawMsgs)
	if len(msgs) == 0 {
		// Advance past pure /summary control lines so they do not keep reappearing
		// as "new". Do not set LastSummaryAt — nothing was actually summarized.
		if cursorSeq > 0 {
			_ = s.store.MarkCursor(groupID, cursorSeq)
		}
		switch {
		case !st.LastSummaryAt.IsZero():
			return fmt.Sprintf("自上次摘要（%s）以来暂无新消息。",
				st.LastSummaryAt.Local().Format("01-02 15:04"))
		case st.LastSummarySeq > 0 || cursorSeq > 0:
			// Cursor advanced (start / reclaim / command-only) but no successful summary yet.
			return "暂无需要摘要的新群消息。"
		default:
			return "暂无可摘要的群消息。\n说明：机器人需能接收群内消息（含未 @ 的发言）才会缓冲内容。可用 /summary start 设定起点，之后再 /summary 生成摘要。"
		}
	}

	// Chronological multi-wave: keep older content first. Extremely long histories
	// use up to MaxWaves waves; an oversized last wave may PreferOldest-trim its tail.
	waves := lansengergroupsummary.SplitWaves(
		msgs,
		lansengergroupsummary.DefaultMaxTotalInputTokens,
		lansengergroupsummary.DefaultPerMessageMaxRunes,
		lansengergroupsummary.DefaultMaxWaves,
	)
	waveNote := ""
	if len(waves) > 1 {
		waveNote = fmt.Sprintf("（分 %d 段处理）", len(waves))
		log.Printf("[lansenger-summary] group=%s waves=%d msgs=%d", groupID, len(waves), len(msgs))
	}

	ctx, cancel := context.WithTimeout(context.Background(), lansengerGroupSummaryOverallTimeout)
	defer cancel()

	summary, covered, err := s.summarizeWaves(ctx, groupID, groupName, waves)
	if err != nil {
		log.Printf("[lansenger-summary] llm group=%s: %v", groupID, err)
		// Preserve multi-wave progress so a later /summary continues instead of
		// redoing successful early waves.
		if covered > 0 {
			markSeq := extendMarkPastSummaryCommands(rawMsgs, covered)
			if markErr := s.store.MarkSummarized(groupID, markSeq, time.Now()); markErr != nil {
				log.Printf("[lansenger-summary] partial mark group=%s: %v", groupID, markErr)
			}
			if ctx.Err() != nil {
				return "生成摘要超时（已保存进度，可再次 /summary 继续）。"
			}
			return "生成摘要部分成功后失败（已保存进度，可再次 /summary 继续）。"
		}
		if ctx.Err() != nil {
			return "生成摘要超时，请稍后再试或缩短讨论跨度后重试。"
		}
		return "生成摘要失败，请稍后重试。"
	}
	summary = strings.TrimSpace(textutil.StripMarkdown(summary))
	if summary == "" {
		return "模型未返回有效摘要，请稍后重试。"
	}
	summary = truncateRunesForGroupSummary(summary, lansengergroupsummary.DefaultMaxOutputRunes)

	// Advance cursor only past content actually included (PreferOldest prefix).
	// Never fall back to cursorSeq (raw max): that could skip unsummarized tail.
	// Also skip contiguous trailing /summary command lines after covered.
	if covered > 0 {
		markSeq := extendMarkPastSummaryCommands(rawMsgs, covered)
		if err := s.store.MarkSummarized(groupID, markSeq, time.Now()); err != nil {
			log.Printf("[lansenger-summary] mark group=%s: %v", groupID, err)
		}
	}

	coveredMsgs := filterMsgsUpToSeq(msgs, covered)
	if len(coveredMsgs) == 0 {
		coveredMsgs = msgs
	}

	rangeLabel := lansengergroupsummary.TimeRangeLabel(coveredMsgs)
	header := "📋 群讨论摘要"
	gn := strings.TrimSpace(groupName)
	if gn == "" {
		gn = strings.TrimSpace(st.GroupName)
	}
	if gn != "" {
		header += " · " + gn
	}
	if rangeLabel != "" {
		header += "\n时间范围：" + rangeLabel
	}
	header += fmt.Sprintf("\n消息条数：%d%s", len(coveredMsgs), waveNote)
	if remaining := countMsgsAfterSeq(msgs, covered); remaining > 0 {
		header += fmt.Sprintf("（另有 %d 条较新内容未纳入本轮，下次 /summary 继续）", remaining)
	}
	header += "\n\n"
	return header + summary
}

// extendMarkPastSummaryCommands advances markSeq only through a contiguous
// run of pure /summary lines immediately after markSeq.
//
// It must NOT jump over non-command messages: e.g. covered=10, then real msg
// seq=11 and /summary seq=12 must leave markSeq=10 so seq=11 stays "new".
func extendMarkPastSummaryCommands(raw []lansengergroupsummary.Message, markSeq int64) int64 {
	for {
		var next *lansengergroupsummary.Message
		for i := range raw {
			m := &raw[i]
			if m.Seq <= markSeq {
				continue
			}
			if next == nil || m.Seq < next.Seq {
				next = m
			}
		}
		if next == nil || !lansengergroupsummary.IsSummaryControlLine(next.Text) {
			return markSeq
		}
		markSeq = next.Seq
	}
}

func countMsgsAfterSeq(msgs []lansengergroupsummary.Message, seq int64) int {
	n := 0
	for _, m := range msgs {
		if m.Seq > seq {
			n++
		}
	}
	return n
}

func filterMsgsUpToSeq(msgs []lansengergroupsummary.Message, maxSeq int64) []lansengergroupsummary.Message {
	out := make([]lansengergroupsummary.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Seq <= maxSeq {
			out = append(out, m)
		}
	}
	return out
}

const (
	lansengerGroupSummarySystemPrompt = `你是群聊讨论摘要助手。根据提供的群消息记录，生成简洁、结构化的中文讨论摘要。

要求：
1. 用要点列出主要议题、关键结论、待办/未决事项、分歧（如有）。
2. 标注重要发言人的立场（若可从记录看出）。
3. 不要编造记录中不存在的内容；不确定就写「未明确」。
4. 控制在 800 字以内，适合即时通讯阅读；可用简短条目，勿使用复杂 Markdown 表格。
5. 忽略机器人命令行（如 /summary、/summary start）与无意义灌水。`

	lansengerGroupSummaryMapPrompt = `你是群聊分段摘要助手。下面是整段讨论中的一部分消息，请生成本段的中文要点摘要（议题、观点、待办）。
不要编造；控制在 400 字以内；不要使用复杂 Markdown。`

	lansengerGroupSummaryReducePrompt = `你是群聊摘要汇总助手。下面是同一讨论的多段分段摘要，请合并为一份连贯的中文总摘要。

要求：
1. 合并重复点，保留分歧与待办。
2. 按「议题 / 结论 / 待办 / 分歧」组织（某节无内容可省略）。
3. 不要编造分段摘要中没有的信息。
4. 控制在 800 字以内，适合发到群里阅读。`
)

// summarizeWaves runs one map-reduce per wave, then a final reduce across waves.
// Returns the summary text and the max message Seq actually included.
func (s *lansengerGroupSummaryService) summarizeWaves(ctx context.Context, groupID, groupName string, waves [][]lansengergroupsummary.Message) (string, int64, error) {
	if len(waves) == 0 {
		return "", 0, fmt.Errorf("empty waves")
	}
	if len(waves) == 1 {
		text, covered, err := s.summarizeOneWave(ctx, groupID, groupName, waves[0], 1, 1)
		return text, covered, err
	}

	partials := make([]string, 0, len(waves))
	var covered int64
	for i, wave := range waves {
		if err := ctx.Err(); err != nil {
			return "", covered, err
		}
		part, waveCovered, err := s.summarizeOneWave(ctx, groupID, groupName, wave, i+1, len(waves))
		if err != nil {
			return "", covered, err
		}
		if waveCovered > covered {
			covered = waveCovered
		}
		partials = append(partials, fmt.Sprintf("【时段 %d/%d · %d 条】\n%s", i+1, len(waves), len(wave), part))
	}

	var b strings.Builder
	if gn := strings.TrimSpace(groupName); gn != "" {
		b.WriteString("群名称：")
		b.WriteString(gn)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("共 %d 个时段摘要，请合并为总摘要：\n\n", len(partials)))
	b.WriteString(strings.Join(partials, "\n\n"))
	reduceInput := lansengergroupsummary.TruncateToTokenBudget(b.String(), lansengergroupsummary.DefaultMaxReduceInputTokens)
	text, err := s.callSummaryLLM(ctx, s.app.GetMaclawLLMConfig(), s.client(), lansengerGroupSummaryReducePrompt, reduceInput, "lansenger-group-summary-wave-reduce", groupID)
	if err != nil {
		// Do not fail after waves already succeeded: concatenate wave summaries
		// so the user still gets content and the cursor can advance safely.
		log.Printf("[lansenger-summary] wave-reduce failed, using joined wave partials: %v", err)
		joined := strings.TrimSpace(strings.Join(partials, "\n\n"))
		if joined == "" {
			return "", covered, err
		}
		return joined, covered, nil
	}
	return text, covered, nil
}

// summarizeOneWave map-reduces a single wave. coveredSeq is MaxSeq of messages
// actually passed to the LLM after any PreferOldest trim inside BuildChunks.
func (s *lansengerGroupSummaryService) summarizeOneWave(ctx context.Context, groupID, groupName string, msgs []lansengergroupsummary.Message, wave, totalWaves int) (string, int64, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if len(msgs) == 0 {
		return "", 0, fmt.Errorf("empty wave")
	}

	llmCfg := s.app.GetMaclawLLMConfig()
	client := s.client()
	chunks := lansengergroupsummary.BuildChunks(
		msgs,
		lansengergroupsummary.DefaultChunkMaxTokens,
		lansengergroupsummary.DefaultSinglePassMaxTokens,
		lansengergroupsummary.DefaultPerMessageMaxRunes,
	)
	if len(chunks) == 0 {
		return "", 0, fmt.Errorf("empty chunks")
	}

	// coveredSeq: max seq among chunk messages (after BuildChunks PreferOldest).
	var coveredSeq int64
	for _, ch := range chunks {
		if seq := lansengergroupsummary.MaxSeq(ch.Messages); seq > coveredSeq {
			coveredSeq = seq
		}
	}

	owner := strings.TrimSpace(groupID)
	if owner == "" {
		owner = strings.TrimSpace(groupName)
	}

	label := groupName
	if totalWaves > 1 {
		label = fmt.Sprintf("%s（时段 %d/%d）", groupName, wave, totalWaves)
	}

	if len(chunks) == 1 {
		user := buildGroupSummaryUserPrompt(label, chunks[0].Formatted, len(chunks[0].Messages), 1, 1)
		text, err := s.callSummaryLLM(ctx, llmCfg, client, lansengerGroupSummarySystemPrompt, user, "lansenger-group-summary", owner)
		if err != nil {
			// Do not report coveredSeq on failure — otherwise a single-wave error
			// would advance the cursor without producing a usable summary.
			return "", 0, err
		}
		return text, coveredSeq, nil
	}

	// Map phase with bounded concurrency.
	partials := make([]string, len(chunks))
	sem := make(chan struct{}, lansengerGroupSummaryMapConcurrency)
	var wg sync.WaitGroup
	var failCount atomic.Int32

	for i := range chunks {
		if ctx.Err() != nil {
			break
		}
		ch := chunks[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()

			user := buildGroupSummaryUserPrompt(label, ch.Formatted, len(ch.Messages), ch.Index+1, len(chunks))
			part, err := s.callSummaryLLM(ctx, llmCfg, client, lansengerGroupSummaryMapPrompt, user, "lansenger-group-summary-map", owner)
			if err != nil {
				log.Printf("[lansenger-summary] map chunk %d/%d failed: %v", ch.Index+1, len(chunks), err)
				failCount.Add(1)
				part = fallbackChunkSummary(ch)
			} else {
				part = strings.TrimSpace(part)
				if part == "" {
					failCount.Add(1)
					part = fallbackChunkSummary(ch)
				}
			}
			partials[ch.Index] = fmt.Sprintf("【分段 %d/%d · %d 条】\n%s", ch.Index+1, len(chunks), len(ch.Messages), part)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		// Wave incomplete — do not report coveredSeq so multi-wave progress
		// stays at the last fully completed wave only.
		return "", 0, err
	}

	// If every map call failed, do not pretend we have a good summary.
	if int(failCount.Load()) >= len(chunks) {
		return "", 0, fmt.Errorf("全部分段摘要调用失败")
	}

	for i := range partials {
		if strings.TrimSpace(partials[i]) == "" {
			partials[i] = fallbackChunkSummary(chunks[i])
		}
	}

	var b strings.Builder
	if gn := strings.TrimSpace(label); gn != "" {
		b.WriteString("群名称：")
		b.WriteString(gn)
		b.WriteString("\n")
	}
	totalMsgs := 0
	for _, ch := range chunks {
		totalMsgs += len(ch.Messages)
	}
	b.WriteString(fmt.Sprintf("共 %d 条消息，分 %d 段摘要如下：\n\n", totalMsgs, len(partials)))
	b.WriteString(strings.Join(partials, "\n\n"))
	reduceInput := lansengergroupsummary.TruncateToTokenBudget(b.String(), lansengergroupsummary.DefaultMaxReduceInputTokens)
	text, err := s.callSummaryLLM(ctx, llmCfg, client, lansengerGroupSummaryReducePrompt, reduceInput, "lansenger-group-summary-reduce", owner)
	if err != nil {
		log.Printf("[lansenger-summary] chunk-reduce failed, using joined map partials: %v", err)
		joined := strings.TrimSpace(strings.Join(partials, "\n\n"))
		if joined == "" {
			return "", coveredSeq, err
		}
		return joined, coveredSeq, nil
	}
	return text, coveredSeq, nil
}

func (s *lansengerGroupSummaryService) client() *http.Client {
	if s != nil && s.httpClient != nil {
		return s.httpClient
	}
	return &http.Client{}
}

func buildGroupSummaryUserPrompt(groupName, transcript string, msgCount, part, totalParts int) string {
	var b strings.Builder
	if gn := strings.TrimSpace(groupName); gn != "" {
		b.WriteString("群名称：")
		b.WriteString(gn)
		b.WriteString("\n")
	}
	if totalParts > 1 {
		b.WriteString(fmt.Sprintf("本段：%d/%d\n", part, totalParts))
	}
	b.WriteString(fmt.Sprintf("消息条数：%d\n\n群消息记录：\n", msgCount))
	b.WriteString(transcript)
	return b.String()
}

func fallbackChunkSummary(ch lansengergroupsummary.Chunk) string {
	text := strings.TrimSpace(ch.Formatted)
	runes := []rune(text)
	if len(runes) > 500 {
		text = string(runes[:500]) + "…"
	}
	return "（本段模型失败，摘录）\n" + text
}

func truncateRunesForGroupSummary(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func (s *lansengerGroupSummaryService) callSummaryLLM(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	client *http.Client,
	systemPrompt, userPrompt, caller, owner string,
) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	messages := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
		map[string]string{"role": "user", "content": userPrompt},
	}
	// Per-call timeout is applied inside doSimple*Request; attach trace on parent ctx.
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{
		Caller:    caller,
		OwnerID:   "lansenger-group:" + strings.TrimSpace(owner),
		RequestID: fmt.Sprintf("gs-%d", time.Now().UnixNano()),
	})
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, client, lansengerGroupSummaryLLMTimeout)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("empty LLM response")
	}
	return strings.TrimSpace(resp.Content), nil
}
