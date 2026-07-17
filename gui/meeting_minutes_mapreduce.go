package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/meetingminutes"
)

const (
	meetingMinutesLLMTimeout     = 75 * time.Second
	meetingMinutesOverallTimeout = 3 * time.Minute
	meetingMinutesMapConcurrency = 2
)

// meetingMinutesDraftPath returns the sidecar path for draft markdown next to audio.
func meetingMinutesDraftPath(audioPath string) string {
	audioPath = strings.TrimSpace(audioPath)
	if audioPath == "" {
		return "meeting_minutes_draft.md"
	}
	ext := filepath.Ext(audioPath)
	base := strings.TrimSuffix(audioPath, ext)
	if strings.TrimSpace(base) == "" {
		base = audioPath
	}
	return base + "_minutes_draft.md"
}

// buildMeetingMinutesDraft builds a minutes body from a full transcript.
// When allowLLM is false (default for plain asr/transcribe), only the fast
// extractive draft is used so tool calls stay responsive.
// When allowLLM is true and Maclaw LLM is configured, runs map-reduce with a
// hard overall timeout, falling back to extractive on any failure.
func buildMeetingMinutesDraft(ctx context.Context, app *App, title, purpose, transcript string, allowLLM bool) (draft string, usedLLM bool) {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return "", false
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "会议纪要"
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if !allowLLM || app == nil || !app.isMaclawLLMConfigured() {
		return meetingminutes.ExtractiveDraft(title, purpose, transcript), false
	}

	ctx, cancel := context.WithTimeout(ctx, meetingMinutesOverallTimeout)
	defer cancel()

	text, err := summarizeMeetingTranscriptLLM(ctx, app, title, purpose, transcript)
	if err != nil || strings.TrimSpace(text) == "" {
		if err != nil {
			log.Printf("[meeting-minutes] map-reduce failed, extractive fallback: %v", err)
		}
		return meetingminutes.ExtractiveDraft(title, purpose, transcript), false
	}
	return meetingminutes.TruncateRunes(strings.TrimSpace(text), meetingminutes.DefaultDraftMaxRunes), true
}

func summarizeMeetingTranscriptLLM(ctx context.Context, app *App, title, purpose, transcript string) (string, error) {
	if app == nil {
		return "", fmt.Errorf("app unavailable")
	}
	chunks := meetingminutes.SplitPlainText(
		transcript,
		meetingminutes.DefaultChunkMaxTokens,
		meetingminutes.DefaultSinglePassMaxTokens,
		meetingminutes.DefaultMaxMapChunks,
	)
	if len(chunks) == 0 {
		return "", fmt.Errorf("empty chunks")
	}

	cfg := app.GetMaclawLLMConfig()
	client := &http.Client{}

	if len(chunks) == 1 {
		user := meetingminutes.BuildSinglePassUserPrompt(title, purpose, chunks[0].Text)
		return callMeetingMinutesLLM(ctx, cfg, client, meetingminutes.SinglePassSystemPrompt, user, "meeting-minutes-single")
	}

	// Map phase. Only real LLM successes count toward usedLLM / reduce input quality.
	partials := make([]string, len(chunks))
	sem := make(chan struct{}, meetingMinutesMapConcurrency)
	var wg sync.WaitGroup
	var mapOK atomic.Int32

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

			user := meetingminutes.BuildMapUserPrompt(title, purpose, ch, len(chunks))
			part, err := callMeetingMinutesLLM(ctx, cfg, client, meetingminutes.MapSystemPrompt, user, "meeting-minutes-map")
			if err != nil {
				log.Printf("[meeting-minutes] map chunk %d/%d failed: %v", ch.Index+1, len(chunks), err)
				return
			}
			part = strings.TrimSpace(part)
			if part == "" {
				return
			}
			partials[ch.Index] = fmt.Sprintf("【分段 %d/%d】\n%s", ch.Index+1, len(chunks), part)
			mapOK.Add(1)
		}()
	}
	wg.Wait()

	okN := int(mapOK.Load())
	// No successful LLM map → let outer extractive draft take over (usedLLM=false).
	if okN == 0 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("全部分段摘要调用失败")
	}

	// Fill holes with short extractive snippets only when some maps succeeded,
	// so reduce still sees full coverage without claiming pure LLM quality.
	for i := range partials {
		if strings.TrimSpace(partials[i]) == "" {
			partials[i] = fallbackMeetingMapPartial(chunks[i])
		}
	}
	joinedPartials := strings.TrimSpace(strings.Join(partials, "\n\n"))

	// Cancelled mid-flight but some maps completed: return joined partials.
	if err := ctx.Err(); err != nil {
		log.Printf("[meeting-minutes] map phase cancelled (%v); returning %d/%d LLM partials", err, okN, len(chunks))
		return joinedPartials, nil
	}

	reduceUser := meetingminutes.BuildReduceUserPrompt(title, purpose, partials)
	text, err := callMeetingMinutesLLM(ctx, cfg, client, meetingminutes.ReduceSystemPrompt, reduceUser, "meeting-minutes-reduce")
	if err != nil {
		log.Printf("[meeting-minutes] reduce failed, joining map partials: %v", err)
		return joinedPartials, nil
	}
	return text, nil
}

func fallbackMeetingMapPartial(ch meetingminutes.TextChunk) string {
	sample := meetingminutes.TruncateRunes(strings.TrimSpace(ch.Text), 400)
	return "（本段模型失败，摘录）\n" + sample
}

func callMeetingMinutesLLM(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	client *http.Client,
	systemPrompt, userPrompt, caller string,
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
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{
		Caller:    caller,
		OwnerID:   "meeting-minutes",
		RequestID: fmt.Sprintf("mm-%d", time.Now().UnixNano()),
	})
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, client, meetingMinutesLLMTimeout)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("empty LLM response")
	}
	return strings.TrimSpace(resp.Content), nil
}

// enrichLongASRWithMinutesDraft appends a minutes draft after a long ASR spill.
// Caller should only invoke this for for_minutes=true. When LLM is available,
// runs map-reduce; otherwise extractive draft. allowLLM is always true here —
// the hot path skips this function entirely when minutes are not requested.
func (h *IMMessageHandler) enrichLongASRWithMinutesDraft(audioPath, fullText, baseResult string, allowLLM bool) string {
	if strings.TrimSpace(fullText) == "" || strings.TrimSpace(baseResult) == "" {
		return baseResult
	}
	if !asrShouldSpillToFile(fullText) {
		return baseResult
	}

	title := guessMeetingTitleFromPath(audioPath)
	var app *App
	if h != nil {
		app = h.app
	}

	draft, usedLLM := buildMeetingMinutesDraft(context.Background(), app, title, "", fullText, allowLLM)
	if strings.TrimSpace(draft) == "" {
		return baseResult
	}

	draftPath := meetingMinutesDraftPath(audioPath)
	if err := os.WriteFile(draftPath, []byte(draft), 0o644); err != nil {
		log.Printf("[meeting-minutes] write draft failed: %v", err)
		// Still attach inline draft (already size-capped).
		return baseResult + "\n\n[engine_minutes_draft — file save failed]\n" +
			fmt.Sprintf("used_llm: %v\n", usedLLM) +
			"draft_inline:\n" + draft + "\n"
	}

	var b strings.Builder
	b.WriteString(baseResult)
	b.WriteString("\n\n[engine_minutes_draft]\n")
	b.WriteString(fmt.Sprintf("minutes_draft_file: %s\n", draftPath))
	b.WriteString(fmt.Sprintf("used_llm_map_reduce: %v\n", usedLLM))
	if !usedLLM {
		b.WriteString("note: extractive draft (LLM map-reduce unavailable or failed); verify against transcript_file.\n")
	}
	b.WriteString("HOW TO USE:\n")
	b.WriteString("1) Open minutes_draft_file as the structured body (summary/decisions/actions).\n")
	b.WriteString("2) Build final .md = draft body + section 完整转写 assembled FROM transcript_file (no rewrite).\n")
	b.WriteString("3) generate_pdf from the final .md; send_file md+pdf+mp3.\n")
	b.WriteString("4) You may lightly edit the draft for clarity, but do not invent facts absent from draft/transcript.\n")
	// Include a short preview of the draft so the model can act even if read_file is skipped.
	preview := meetingminutes.TruncateRunes(draft, 1800)
	b.WriteString("\n--- draft_preview ---\n")
	b.WriteString(preview)
	if len([]rune(draft)) > 1800 {
		b.WriteString("\n… (see minutes_draft_file for full draft)\n")
	}
	return b.String()
}

func guessMeetingTitleFromPath(audioPath string) string {
	base := filepath.Base(strings.TrimSpace(audioPath))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "会议纪要"
	}
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	name = strings.TrimSpace(name)
	if name == "" {
		return "会议纪要"
	}
	// Strip common recording prefixes.
	for _, p := range []string{"record_", "recording_", "rec_"} {
		if strings.HasPrefix(strings.ToLower(name), p) {
			name = name[len(p):]
			break
		}
	}
	if name == "" {
		return "会议纪要"
	}
	return name
}
