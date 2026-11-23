package llm

// Streaming support for the OpenAI Responses API.  The Responses API uses
// named SSE events (rather than chat-completions' choices[].delta format), so
// it lives beside the non-streaming Responses parser instead of the chat
// stream implementation.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// responsesStreamMaxEventBytes bounds one SSE event. It must not be sized to a
// single tool call: response.completed carries every output item of the turn,
// each with its full argument string, in one event. Several calls at the
// MaxToolArgumentsBytes cap (180 KiB) plus reasoning text pass 256 KiB, and
// bufio.Scanner then fails the whole turn with "token too long". Google's
// stream reader already allows 4 MiB for the same reason. 4 MiB covers about
// twenty max-sized calls in one snapshot; a larger event is not scanned into
// one token. The split function emits every complete event already in the
// buffer before it reports that limit, so a later oversized event cannot
// erase them. A usable prefix is a truncated turn, not a failed one: pairing
// it with an error makes the agent loop drop the calls it just kept. Tests
// shrink this before the scanner is created.
var responsesStreamMaxEventBytes = 4 << 20

type responsesStreamItem struct {
	itemType string
	itemID   string
	callID   string
	name     string
	args     strings.Builder
}

// DoResponsesAPIRequestStream sends a streaming Responses API request. Text
// and display-safe reasoning summaries are delivered separately so callers
// can render reasoning while the turn is still active.  Providers must never
// expose private chain-of-thought through this API; only the summaries they
// explicitly emit are forwarded.
func DoResponsesAPIRequestStream(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
	onText TokenCallback,
	onReasoning TokenCallback,
) (*Response, error) {
	return DoResponsesAPIRequestStreamWithOptions(ctx, cfg, messages, tools, client, onText, onReasoning, ResponsesAPIRequestOptions{Stream: true, Tools: tools})
}

// DoResponsesAPIRequestStreamWithOptions preserves owner-selected invocation
// controls at the same final serialization path as the non-streaming builder.
func DoResponsesAPIRequestStreamWithOptions(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
	onText TokenCallback,
	onReasoning TokenCallback,
	opts ResponsesAPIRequestOptions,
) (*Response, error) {
	client = HTTPClientForRequestContext(ctx, client)
	opts.Stream = true
	opts.Tools = tools
	// The route deadline still bounds time-to-headers. Once an event stream
	// starts, it has no wall-clock cap. Idle silence, a stop, and an earlier
	// host deadline still abort it. A JSON body is not a token stream and
	// stays on the route deadline.
	reqCtx, cancelReq, markHeaders, headerDeadline := streamingRequestContext(ctx)
	defer cancelReq()
	req, _, endpoint, err := NewResponsesAPIRequest(reqCtx, cfg, messages, opts)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, normalizeStreamSetupError(headerDeadline, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Body: append([]byte(nil), body...)}
	}

	// A few compatible providers accept stream=true but still return a regular
	// Response object. Preserve compatibility and surface its safe summary.
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		parsed, parseErr := ParseNonStreamResponsesAPIResponse(resp)
		if parseErr == nil && len(parsed.Choices) > 0 {
			msg := parsed.Choices[0].Message
			if msg.ReasoningContent != "" && onReasoning != nil {
				onReasoning(msg.ReasoningContent)
			}
			if msg.Content != "" && onText != nil {
				onText(msg.Content)
			}
		}
		return parsed, parseErr
	}
	// Losing the claim means the route timer already canceled this request.
	if !markHeaders() {
		return nil, context.DeadlineExceeded
	}
	idleBody := newIdleTimeoutReader(resp.Body, SSEIdleTimeout)
	defer idleBody.Close()

	var textBuf strings.Builder
	var reasoningBuf strings.Builder
	items := make(map[int]*responsesStreamItem)
	var usage *Usage
	finishReason := ""
	// Responses stream events carry the provider-issued response object inside
	// their lifecycle envelope. Keep that ID on the assembled internal response
	// just as ParseNonStreamResponsesAPIBody does. It is provider evidence, not
	// a locally generated request/loop identifier; callers that need stronger
	// correlation still have to validate their transport lifecycle separately.
	responseID := ""
	seenEvent := false

	emitReasoningSummary := func(summary string) {
		delta := appendResponsesStreamReasoning(&reasoningBuf, summary)
		if delta != "" && onReasoning != nil {
			onReasoning(delta)
		}
	}
	emitReasoningDelta := func(delta string) {
		if delta == "" {
			return
		}
		reasoningBuf.WriteString(delta)
		if onReasoning != nil {
			onReasoning(delta)
		}
	}
	emitTextSummary := func(text string) {
		delta := appendResponsesStreamText(&textBuf, text)
		if delta != "" && onText != nil {
			onText(delta)
		}
	}

	scanner := bufio.NewScanner(idleBody)
	scanner.Split(scanResponsesAPIStreamEvent)
	scanner.Buffer(make([]byte, 0, 64*1024), responsesStreamMaxEventBytes)
	var oversizedEvent error
	for scanner.Scan() {
		eventType, payload, ok := parseResponsesAPIStreamEvent(scanner.Text())
		if !ok {
			continue
		}
		if eventType == responsesStreamEventTooLarge {
			// Complete events already in the scanner buffer were returned on
			// earlier Scan calls. A lone oversized event still fails: nothing
			// arrived. A usable prefix is truncated output, not a transport
			// failure. Returning it with an error makes the agent loop discard
			// the calls this scan just kept.
			oversizedEvent = fmt.Errorf("Responses API SSE event exceeds %d bytes", responsesStreamMaxEventBytes)
			break
		}
		seenEvent = true
		if payload == "[DONE]" {
			break
		}
		if eventResponseID := responsesStreamResponseID(payload); eventResponseID != "" {
			if responseID == "" {
				responseID = eventResponseID
			} else if responseID != eventResponseID {
				// One SSE request must not silently merge two provider response
				// identities. Returning an error is safer than binding tool calls
				// from an ambiguous wire stream to either response.
				return nil, fmt.Errorf("Responses API stream response ID changed: %q -> %q", responseID, eventResponseID)
			}
		}

		switch strings.TrimSpace(eventType) {
		case "response.output_item.added":
			var added struct {
				OutputIndex int                    `json:"output_index"`
				ItemID      string                 `json:"item_id"`
				Item        map[string]interface{} `json:"item"`
			}
			rawAdded := map[string]json.RawMessage{}
			if json.Unmarshal([]byte(payload), &added) != nil || json.Unmarshal([]byte(payload), &rawAdded) != nil {
				continue
			}
			item := &responsesStreamItem{}
			item.itemType, _ = added.Item["type"].(string)
			item.itemID = firstResponsesStreamText(added.ItemID, stringField(added.Item, "id"))
			item.callID, _ = added.Item["call_id"].(string)
			item.name, _ = added.Item["name"].(string)
			if args, _ := added.Item["arguments"].(string); args != "" {
				if len(args) > MaxToolArgumentsBytes {
					return responsesStreamPartialResponse(responseID, textBuf.String(), reasoningBuf.String(), items, usage, finishReason), responsesStreamToolArgumentsTooLarge(item.name, len(args))
				}
				item.args.WriteString(args)
			}
			// A later added event can reuse output_index 0 when the provider
			// omitted it. Do not write that name onto the neighbour that
			// already lives at 0 unless the item id says they are the same call.
			_, indexPresent := rawAdded["output_index"]
			placeResponsesStreamItem(items, added.OutputIndex, indexPresent, item)

		case "response.output_text.delta":
			var delta struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal([]byte(payload), &delta) == nil && delta.Delta != "" {
				textBuf.WriteString(delta.Delta)
				if onText != nil {
					onText(delta.Delta)
				}
			}
		case "response.output_text.done":
			var done struct {
				Text  string `json:"text"`
				Delta string `json:"delta"`
			}
			if json.Unmarshal([]byte(payload), &done) == nil {
				emitTextSummary(firstResponsesStreamText(done.Text, done.Delta))
			}

		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.reasoning_content.delta", "response.reasoning.delta":
			var delta struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal([]byte(payload), &delta) == nil {
				emitReasoningDelta(delta.Delta)
			}
		case "response.reasoning_summary_text.done", "response.reasoning_text.done", "response.reasoning_content.done", "response.reasoning.done":
			var done struct {
				Text  string `json:"text"`
				Delta string `json:"delta"`
			}
			if json.Unmarshal([]byte(payload), &done) == nil {
				emitReasoningSummary(firstResponsesStreamText(done.Text, done.Delta))
			}
		case "response.reasoning_summary_part.added":
			// Some providers deliver a whole summary part at once instead of
			// streaming deltas. Feed it through the same summary path; the
			// append helper dedupes against already-accumulated deltas.
			var added struct {
				Part struct {
					Text string `json:"text"`
				} `json:"part"`
			}
			if json.Unmarshal([]byte(payload), &added) == nil {
				emitReasoningSummary(added.Part.Text)
			}

		case "response.function_call_arguments.delta":
			var delta struct {
				Delta       string `json:"delta"`
				ItemID      string `json:"item_id"`
				OutputIndex int    `json:"output_index"`
			}
			rawDelta := map[string]json.RawMessage{}
			if json.Unmarshal([]byte(payload), &delta) != nil || json.Unmarshal([]byte(payload), &rawDelta) != nil || delta.Delta == "" {
				continue
			}
			_, indexPresent := rawDelta["output_index"]
			item := responsesStreamItemForEvent(items, delta.OutputIndex, indexPresent, delta.ItemID)
			item.itemType = firstResponsesStreamText(item.itemType, "function_call")
			item.itemID = firstResponsesStreamText(item.itemID, delta.ItemID)
			item.args.WriteString(delta.Delta)
			if item.args.Len() > MaxToolArgumentsBytes {
				return responsesStreamPartialResponse(responseID, textBuf.String(), reasoningBuf.String(), items, usage, finishReason), fmt.Errorf("tool arguments too large for %s: %d bytes exceeds limit %d", item.name, item.args.Len(), MaxToolArgumentsBytes)
			}

		case "response.function_call_arguments.done":
			var done struct {
				Arguments   string `json:"arguments"`
				ItemID      string `json:"item_id"`
				OutputIndex int    `json:"output_index"`
			}
			rawDone := map[string]json.RawMessage{}
			if json.Unmarshal([]byte(payload), &done) == nil && json.Unmarshal([]byte(payload), &rawDone) == nil && done.Arguments != "" {
				_, indexPresent := rawDone["output_index"]
				item := responsesStreamItemForEvent(items, done.OutputIndex, indexPresent, done.ItemID)
				item.itemType = firstResponsesStreamText(item.itemType, "function_call")
				item.itemID = firstResponsesStreamText(item.itemID, done.ItemID)
				if len(done.Arguments) > MaxToolArgumentsBytes {
					return responsesStreamPartialResponse(responseID, textBuf.String(), reasoningBuf.String(), items, usage, finishReason), responsesStreamToolArgumentsTooLarge(item.name, len(done.Arguments))
				}
				mergeResponsesStreamToolArguments(item, done.Arguments)
			}

		case "response.output_item.done":
			var done struct {
				OutputIndex int                    `json:"output_index"`
				ItemID      string                 `json:"item_id"`
				Item        responsesAPIOutputItem `json:"item"`
			}
			rawItemDone := map[string]json.RawMessage{}
			if json.Unmarshal([]byte(payload), &done) == nil && json.Unmarshal([]byte(payload), &rawItemDone) == nil {
				emitReasoningSummary(responsesStreamDisplaySummary(done.Item))
				if done.Item.Type == "message" {
					emitTextSummary(responsesStreamMessageText(done.Item))
				}
				if done.Item.Type == "function_call" {
					_, indexPresent := rawItemDone["output_index"]
					itemID := firstResponsesStreamText(done.ItemID, done.Item.ID)
					item := responsesStreamItemForEvent(items, done.OutputIndex, indexPresent, itemID)
					item.itemType = firstResponsesStreamText(item.itemType, done.Item.Type)
					item.itemID = firstResponsesStreamText(item.itemID, itemID)
					item.callID = firstResponsesStreamText(item.callID, done.Item.CallID)
					item.name = firstResponsesStreamText(item.name, done.Item.Name)
					if done.Item.Arguments != "" {
						if len(done.Item.Arguments) > MaxToolArgumentsBytes {
							return responsesStreamPartialResponse(responseID, textBuf.String(), reasoningBuf.String(), items, usage, finishReason), responsesStreamToolArgumentsTooLarge(item.name, len(done.Item.Arguments))
						}
						mergeResponsesStreamToolArguments(item, done.Item.Arguments)
					}
				}
			}

		case "response.completed", "response.incomplete":
			// Incomplete is the same final snapshot as completed, cut by the
			// output cap. Read its text before marking length; a body that
			// never sent deltas still has the partial program here.
			if strings.TrimSpace(eventType) == "response.incomplete" {
				finishReason = "length"
			}
			if eventUsage := ExtractResponsesAPIUsageFromEventPayload([]byte(payload)); eventUsage != nil {
				usage = eventUsage
			}
			for _, summary := range responsesCompletedStreamReasoning(payload) {
				emitReasoningSummary(summary)
			}
			for _, text := range responsesCompletedStreamText(payload) {
				emitTextSummary(text)
			}
			snapshot := responsesCompletedStreamToolCalls(payload)
			for outputIndex, item := range snapshot {
				if item.args.Len() > MaxToolArgumentsBytes {
					return responsesStreamPartialResponse(responseID, textBuf.String(), reasoningBuf.String(), items, usage, finishReason), responsesStreamToolArgumentsTooLarge(item.name, item.args.Len())
				}
				outputIndex = responsesStreamSnapshotSlot(items, outputIndex, item, len(snapshot))
				items[outputIndex] = mergeResponsesStreamToolItem(items[outputIndex], item)
			}
			// A provider can send function_call_arguments.* without the matching
			// output_item.added. The delta path then had to create an item with
			// arguments but no name, and every such item reaches the loop as a
			// nameless tool call, which the loop drops as a broken batch. The
			// final snapshot still names them, so fill those names back in.
			backfillResponsesStreamToolNames(items, snapshot)
			goto completed
		case "response.failed", "error":
			// Keep whatever text/reasoning/tool calls were accumulated before
			// the failure, mirroring the scanner read-error path below, so an
			// interrupted turn does not silently lose its reasoning trace.
			return responsesStreamPartialResponse(responseID, textBuf.String(), reasoningBuf.String(), items, usage, finishReason), responsesStreamEventError(payload)
		}
	}

completed:
	if err := scanner.Err(); err != nil {
		if textBuf.Len() > 0 || reasoningBuf.Len() > 0 || len(items) > 0 {
			return responsesStreamPartialResponse(responseID, textBuf.String(), reasoningBuf.String(), items, usage, finishReason), fmt.Errorf("Responses API SSE stream read error: %w", err)
		}
		return nil, fmt.Errorf("Responses API SSE stream read error: %w", err)
	}
	if oversizedEvent != nil {
		if textBuf.Len() > 0 || reasoningBuf.Len() > 0 || len(items) > 0 {
			// The unread tail was cut. Mark length only when a call's arguments
			// are not a complete JSON object: filterStreamTruncatedToolCalls
			// treats every empty-argument call as truncated under length, and
			// a finished call must still execute. A hard error is what makes
			// the agent loop discard this prefix.
			if finishReason == "" && responsesStreamArgsCut(items) {
				finishReason = "length"
			}
			return responsesStreamPartialResponse(responseID, textBuf.String(), reasoningBuf.String(), items, usage, finishReason), nil
		}
		return nil, oversizedEvent
	}
	if !seenEvent {
		return nil, fmt.Errorf("Responses API stream contained no SSE events from %s", endpoint)
	}

	return responsesStreamPartialResponse(responseID, textBuf.String(), reasoningBuf.String(), items, usage, finishReason), nil
}

func responsesStreamResponseID(payload string) string {
	var event struct {
		Response struct {
			ID string `json:"id"`
		} `json:"response"`
	}
	if json.Unmarshal([]byte(payload), &event) != nil {
		return ""
	}
	return strings.TrimSpace(event.Response.ID)
}

func responsesStreamPartialResponse(responseID, text, reasoning string, items map[int]*responsesStreamItem, usage *Usage, finishReason string) *Response {
	msg := Message{Role: "assistant", Content: StripAllExtra(text), ReasoningContent: StripLeakedReasoningProtocol(reasoning)}
	indices := make([]int, 0, len(items))
	for index := range items {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		item := items[index]
		if strings.EqualFold(strings.TrimSpace(item.itemType), "function_call") {
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: item.callID, Type: "function", Function: ToolCallFunction{Name: item.name, Arguments: item.args.String()}})
		}
	}
	if finishReason == "" {
		finishReason = "stop"
		if len(msg.ToolCalls) > 0 {
			finishReason = "tool_calls"
		}
	}
	finishReason, truncatedTools, truncatedArgs := filterStreamTruncatedToolCalls(&msg, finishReason)
	return &Response{ResponseID: strings.TrimSpace(responseID), Choices: []Choice{{Message: msg, FinishReason: finishReason, TruncatedToolNames: truncatedTools, TruncatedToolArgs: truncatedArgs}}, Usage: usage}
}

func responsesStreamDisplaySummary(item responsesAPIOutputItem) string {
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "reasoning", "analysis", "reasoning_summary":
	default:
		return ""
	}
	parts := item.Summary
	if len(parts) == 0 {
		parts = item.Content
	}
	var out strings.Builder
	for _, part := range parts {
		switch part.Type {
		case "summary_text", "reasoning_text", "reasoning_content", "text", "output_text", "thinking":
			if part.Text != "" {
				out.WriteString(part.Text)
			} else {
				out.WriteString(part.Content)
			}
		}
	}
	return strings.TrimSpace(out.String())
}

func responsesStreamMessageText(item responsesAPIOutputItem) string {
	if !strings.EqualFold(strings.TrimSpace(item.Type), "message") {
		return ""
	}
	var out strings.Builder
	for _, part := range item.Content {
		switch part.Type {
		case "output_text", "text", "input_text":
			if part.Text != "" {
				out.WriteString(part.Text)
			} else {
				out.WriteString(part.Content)
			}
		}
	}
	return strings.TrimSpace(out.String())
}

func responsesCompletedStreamReasoning(payload string) []string {
	var completed struct {
		Response struct {
			Output []responsesAPIOutputItem `json:"output"`
		} `json:"response"`
	}
	if json.Unmarshal([]byte(payload), &completed) != nil {
		return nil
	}
	var summaries []string
	for _, item := range completed.Response.Output {
		if summary := responsesStreamDisplaySummary(item); summary != "" {
			summaries = append(summaries, summary)
		}
	}
	return summaries
}

func responsesCompletedStreamText(payload string) []string {
	text := make([]string, 0)
	for _, item := range responsesCompletedStreamOutput(payload) {
		if content := responsesStreamMessageText(item); content != "" {
			text = append(text, content)
		}
	}
	return text
}

func responsesCompletedStreamToolCalls(payload string) map[int]*responsesStreamItem {
	output := responsesCompletedStreamOutput(payload)
	items := make(map[int]*responsesStreamItem)
	for position, item := range output {
		if !strings.EqualFold(strings.TrimSpace(item.Type), "function_call") {
			continue
		}
		streamItem := &responsesStreamItem{itemType: item.Type, itemID: item.ID, callID: item.CallID, name: item.Name}
		streamItem.args.WriteString(item.Arguments)
		// The provider's own output_index is the index the delta events used.
		// Without it, slice position is the only candidate, but every reasoning
		// or message item before this one offsets it.
		index := position
		if item.OutputIndex != nil {
			index = *item.OutputIndex
		} else if strings.TrimSpace(item.ID) != "" {
			// A missing output_index must not occupy a sibling's provider index.
			// The merge step relocates this item by id.
			index = -1 - position
		}
		items[index] = streamItem
	}
	return items
}

// backfillResponsesStreamToolNames gives a tool item the name the final
// snapshot carries when the stream never named it. It matches on item id,
// then call id, because those are the provider's own identities. Position is
// only a fallback when both sides have exactly one item and both omit ids:
// a known id that the snapshot does not contain must not borrow the neighbour
// sitting at the same index. An item that already has a name keeps it, and a
// snapshot without a name never clears one.
func backfillResponsesStreamToolNames(items map[int]*responsesStreamItem, snapshot map[int]*responsesStreamItem) {
	if len(items) == 0 || len(snapshot) == 0 {
		return
	}
	byCallID := make(map[string]*responsesStreamItem, len(snapshot))
	byItemID := make(map[string]*responsesStreamItem, len(snapshot))
	for _, item := range snapshot {
		if item == nil {
			continue
		}
		if id := strings.TrimSpace(item.callID); id != "" {
			byCallID[id] = item
		}
		if id := strings.TrimSpace(item.itemID); id != "" {
			byItemID[id] = item
		}
	}
	for index, item := range items {
		if item == nil || strings.TrimSpace(item.name) != "" {
			continue
		}
		var named *responsesStreamItem
		if id := strings.TrimSpace(item.itemID); id != "" {
			named = byItemID[id]
		}
		if named == nil {
			if id := strings.TrimSpace(item.callID); id != "" {
				named = byCallID[id]
			}
		}
		// Position is only safe when both sides have exactly one unnamed item.
		// Several nameless calls must not borrow the neighbour at the same index.
		if named == nil && strings.TrimSpace(item.callID) == "" && strings.TrimSpace(item.itemID) == "" && responsesStreamUnnamedCount(items) == 1 && responsesStreamUnnamedCount(snapshot) == 1 {
			if candidate := snapshot[index]; candidate != nil && strings.TrimSpace(candidate.callID) == "" && strings.TrimSpace(candidate.itemID) == "" {
				named = candidate
			}
		}
		if named == nil {
			continue
		}
		if name := strings.TrimSpace(named.name); name != "" {
			item.name = name
		}
		if item.callID == "" {
			item.callID = named.callID
		}
		if item.itemID == "" {
			item.itemID = named.itemID
		}
	}
}

// mergeResponsesStreamToolItem folds a completed snapshot into the item built
// from earlier deltas. Identity fields fill only when the live item is blank:
// a compatible provider may omit them from earlier events. Already-received
// delta arguments are kept when the snapshot is shorter or stale.
func mergeResponsesStreamToolItem(existing, completed *responsesStreamItem) *responsesStreamItem {
	if existing == nil {
		return completed
	}
	if completed == nil {
		return existing
	}
	existing.itemType = firstResponsesStreamText(existing.itemType, completed.itemType)
	existing.itemID = firstResponsesStreamText(existing.itemID, completed.itemID)
	existing.callID = firstResponsesStreamText(existing.callID, completed.callID)
	existing.name = firstResponsesStreamText(existing.name, completed.name)
	mergeResponsesStreamToolArguments(existing, completed.args.String())
	return existing
}

// placeResponsesStreamItem stores an added item without letting a missing
// output_index (the zero value) overwrite a different call already at 0.
func placeResponsesStreamItem(items map[int]*responsesStreamItem, outputIndex int, indexPresent bool, incoming *responsesStreamItem) {
	if incoming == nil {
		return
	}
	if idx, existing := responsesStreamFindByItemID(items, incoming.itemID); existing != nil {
		items[idx] = mergeResponsesStreamToolItem(existing, incoming)
		return
	}
	if !indexPresent {
		if idx, existing := responsesStreamUniqueUnkeyed(items); existing != nil {
			items[idx] = mergeResponsesStreamToolItem(existing, incoming)
			return
		}
		items[responsesStreamUnusedIndex(items)] = incoming
		return
	}
	current := items[outputIndex]
	if current != nil && !responsesStreamSameIdentity(current, incoming) {
		items[responsesStreamUnusedIndex(items)] = incoming
		return
	}
	items[outputIndex] = mergeResponsesStreamToolItem(current, incoming)
}

// responsesStreamItemForEvent returns the accumulator for an argument event.
// A missing output_index must not create or extend the item at index 0 when
// that slot already belongs to another call. Events that also omit item_id
// share the single unkeyed accumulator; they cannot be demultiplexed.
func responsesStreamItemForEvent(items map[int]*responsesStreamItem, outputIndex int, indexPresent bool, itemID string) *responsesStreamItem {
	if _, existing := responsesStreamFindByItemID(items, itemID); existing != nil {
		existing.itemID = firstResponsesStreamText(existing.itemID, itemID)
		return existing
	}
	if indexPresent {
		current := items[outputIndex]
		if current != nil && strings.TrimSpace(itemID) != "" && strings.TrimSpace(current.itemID) != "" && strings.TrimSpace(current.itemID) != strings.TrimSpace(itemID) {
			created := &responsesStreamItem{itemID: strings.TrimSpace(itemID)}
			items[responsesStreamUnusedIndex(items)] = created
			return created
		}
		if current == nil {
			current = &responsesStreamItem{}
			items[outputIndex] = current
		}
		return current
	}
	if _, existing := responsesStreamUniqueUnkeyed(items); existing != nil {
		return existing
	}
	created := &responsesStreamItem{itemID: strings.TrimSpace(itemID)}
	items[responsesStreamUnusedIndex(items)] = created
	return created
}

// responsesStreamSnapshotSlot is the live slot a completed item belongs to.
// Item id wins over a slice position that only coincides with another call.
func responsesStreamSnapshotSlot(items map[int]*responsesStreamItem, index int, incoming *responsesStreamItem, snapshotCalls int) int {
	if incoming == nil {
		return index
	}
	if idx, existing := responsesStreamFindByItemID(items, incoming.itemID); existing != nil {
		return idx
	}
	if idx, existing := responsesStreamFindByCallID(items, incoming.callID); existing != nil {
		return idx
	}
	// One live call and one snapshot call are the same call even when the
	// provider omitted both indexes. Two calls must not share that guess.
	if snapshotCalls == 1 {
		if count, idx, _ := responsesStreamFunctionCalls(items); count == 1 {
			return idx
		}
	}
	current := items[index]
	if current == nil {
		return index
	}
	if id := strings.TrimSpace(incoming.itemID); id != "" && strings.TrimSpace(current.itemID) == id {
		return index
	}
	if id := strings.TrimSpace(incoming.callID); id != "" && strings.TrimSpace(current.callID) == id {
		return index
	}
	// Two id-less items share a slot only when the stream has a single call.
	if strings.TrimSpace(incoming.itemID) == "" && strings.TrimSpace(incoming.callID) == "" && strings.TrimSpace(current.itemID) == "" && strings.TrimSpace(current.callID) == "" && len(items) == 1 {
		return index
	}
	return responsesStreamUnusedIndex(items)
}

func responsesStreamFindByItemID(items map[int]*responsesStreamItem, itemID string) (int, *responsesStreamItem) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return 0, nil
	}
	for index, item := range items {
		if item != nil && strings.TrimSpace(item.itemID) == itemID {
			return index, item
		}
	}
	return 0, nil
}

func responsesStreamFindByCallID(items map[int]*responsesStreamItem, callID string) (int, *responsesStreamItem) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return 0, nil
	}
	for index, item := range items {
		if item != nil && strings.TrimSpace(item.callID) == callID {
			return index, item
		}
	}
	return 0, nil
}

func responsesStreamSameIdentity(current, incoming *responsesStreamItem) bool {
	if current == nil || incoming == nil {
		return false
	}
	if id := strings.TrimSpace(incoming.itemID); id != "" && strings.TrimSpace(current.itemID) != "" {
		return strings.TrimSpace(current.itemID) == id
	}
	if id := strings.TrimSpace(incoming.callID); id != "" && strings.TrimSpace(current.callID) != "" {
		return strings.TrimSpace(current.callID) == id
	}
	return strings.TrimSpace(current.itemID) == "" && strings.TrimSpace(incoming.itemID) == "" && strings.TrimSpace(current.callID) == "" && strings.TrimSpace(incoming.callID) == ""
}

func responsesStreamUniqueUnkeyed(items map[int]*responsesStreamItem) (int, *responsesStreamItem) {
	foundIndex := 0
	var found *responsesStreamItem
	for index, item := range items {
		if item == nil || strings.TrimSpace(item.itemID) != "" || strings.TrimSpace(item.callID) != "" {
			continue
		}
		if found != nil {
			return 0, nil
		}
		foundIndex = index
		found = item
	}
	return foundIndex, found
}

func responsesStreamFunctionCalls(items map[int]*responsesStreamItem) (int, int, *responsesStreamItem) {
	count := 0
	index := 0
	var found *responsesStreamItem
	for i, item := range items {
		itemType := strings.TrimSpace(item.itemType)
		if item == nil || (itemType != "" && !strings.EqualFold(itemType, "function_call")) {
			continue
		}
		count++
		index = i
		found = item
	}
	return count, index, found
}

func responsesStreamUnnamedCount(items map[int]*responsesStreamItem) int {
	count := 0
	for _, item := range items {
		if item != nil && strings.TrimSpace(item.name) == "" {
			count++
		}
	}
	return count
}

func responsesStreamUnusedIndex(items map[int]*responsesStreamItem) int {
	index := 0
	for {
		if _, taken := items[index]; !taken {
			return index
		}
		index++
	}
}

// mergeResponsesStreamToolArguments accepts a final provider snapshot only
// when it extends the accumulated deltas. Compatible endpoints sometimes send
// a stale or truncated value in *.done / response.completed.
func mergeResponsesStreamToolArguments(item *responsesStreamItem, candidate string) {
	if item == nil || candidate == "" {
		return
	}
	current := item.args.String()
	if current != "" && !strings.HasPrefix(candidate, current) {
		return
	}
	item.args.Reset()
	item.args.WriteString(candidate)
}

func responsesCompletedStreamOutput(payload string) []responsesAPIOutputItem {
	var completed struct {
		Response struct {
			Output []responsesAPIOutputItem `json:"output"`
		} `json:"response"`
	}
	if json.Unmarshal([]byte(payload), &completed) != nil {
		return nil
	}
	return completed.Response.Output
}

func appendResponsesStreamReasoning(buf *strings.Builder, summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	existing := buf.String()
	if existing == "" {
		buf.WriteString(summary)
		return summary
	}
	if strings.Contains(existing, summary) {
		return ""
	}
	if strings.HasPrefix(summary, existing) {
		delta := summary[len(existing):]
		buf.WriteString(delta)
		return delta
	}
	buf.WriteByte('\n')
	buf.WriteString(summary)
	return "\n" + summary
}

func appendResponsesStreamText(buf *strings.Builder, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	existing := buf.String()
	if existing == "" {
		buf.WriteString(text)
		return text
	}
	if strings.Contains(existing, text) {
		return ""
	}
	if strings.HasPrefix(text, existing) {
		delta := text[len(existing):]
		buf.WriteString(delta)
		return delta
	}
	if strings.HasSuffix(existing, text) {
		return ""
	}
	buf.WriteByte('\n')
	buf.WriteString(text)
	return "\n" + text
}

// responsesStreamArgsCut reports a function call whose arguments are not a
// complete JSON object. An oversized tail may have cut that object. json.Valid
// is not enough: an array, number, or string is valid JSON but not a call's
// argument object, and treating it as closed lets the agent execute a cut
// call. Empty arguments stay unmarked here so a finished no-arg call is not
// forced through length truncation.
func responsesStreamArgsCut(items map[int]*responsesStreamItem) bool {
	for _, item := range items {
		if item == nil || !strings.EqualFold(strings.TrimSpace(item.itemType), "function_call") {
			continue
		}
		if !responsesStreamArgsObject(item.args.String()) {
			return true
		}
	}
	return false
}

func responsesStreamArgsObject(args string) bool {
	args = strings.TrimSpace(args)
	if args == "" {
		return true
	}
	var object map[string]json.RawMessage
	return json.Unmarshal([]byte(args), &object) == nil
}

func responsesStreamToolArgumentsTooLarge(name string, size int) error {
	if strings.TrimSpace(name) == "" {
		name = "function_call"
	}
	return fmt.Errorf("tool arguments too large for %s: %d bytes exceeds limit %d", name, size, MaxToolArgumentsBytes)
}

func firstResponsesStreamText(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func responsesStreamEventError(payload string) error {
	var event struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
		Response struct {
			Error struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal([]byte(payload), &event) == nil {
		message, code := event.Error.Message, event.Error.Code
		if message == "" {
			message, code = event.Response.Error.Message, event.Response.Error.Code
		}
		if message != "" {
			if code != "" {
				return fmt.Errorf("Responses API stream failed: %s (code=%s)", message, code)
			}
			return fmt.Errorf("Responses API stream failed: %s", message)
		}
	}
	return fmt.Errorf("Responses API stream failed: payload_len=%d", len(payload))
}

// responsesStreamEventTooLarge is a synthetic event type. The split function
// emits it once the unread tail cannot fit in the scanner buffer, so the
// caller can keep every event that already parsed.
const responsesStreamEventTooLarge = "maclaw.responses_stream_event_too_large"

func scanResponsesAPIStreamEvent(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF {
		return scanResponsesAPIStreamTail(data, true)
	}
	return scanResponsesAPIStreamTail(data, false)
}

// responsesStreamTooLargeToken is the frame Scan returns once the unread tail
// cannot fit. It is not a provider event.
var responsesStreamTooLargeToken = []byte("event: " + responsesStreamEventTooLarge + "\ndata: {}")

// scanResponsesAPIStreamTail returns one complete SSE event. A full buffer
// that still holds an earlier event must return that event first: Scanner
// stops with ErrTooLong, and drops the buffer, when a full buffer makes no
// progress. The oversized tail is reported only after those events are out.
func scanResponsesAPIStreamTail(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.Index(data, []byte("\r\n\r\n")); i >= 0 {
		return i + 4, bytes.TrimRight(data[:i], "\r\n"), nil
	}
	if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
		return i + 2, bytes.TrimRight(data[:i], "\r\n"), nil
	}
	// Some compatible gateways omit the blank SSE separator. The following
	// event field still provides an unambiguous boundary. Avoid data[1:] when
	// Scanner invokes the split function with an empty/one-byte buffer.
	if len(data) > 1 {
		if i := bytes.Index(data[1:], []byte("\r\nevent:")); i >= 0 {
			boundary := i + 1
			return boundary + 2, bytes.TrimRight(data[:boundary], "\r\n"), nil
		}
		if i := bytes.Index(data[1:], []byte("\nevent:")); i >= 0 {
			boundary := i + 1
			return boundary + 1, bytes.TrimRight(data[:boundary], "\r\n"), nil
		}
	}
	if len(data) >= responsesStreamMaxEventBytes {
		// No delimiter in a full buffer. The tail is one event that cannot
		// fit, not a pile of earlier events: those returned above. Advance
		// the buffer so Scanner can deliver the sentinel and stop. At EOF the
		// same tail is still not a usable event.
		return len(data), append([]byte(nil), responsesStreamTooLargeToken...), nil
	}
	if atEOF && len(data) > 0 {
		return len(data), bytes.TrimRight(data, "\r\n"), nil
	}
	return 0, nil, nil
}

func parseResponsesAPIStreamEvent(frame string) (eventType, payload string, ok bool) {
	var lines []string
	for _, line := range strings.Split(frame, "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			lines = append(lines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if len(lines) == 0 {
		return "", "", false
	}
	payload = strings.Join(lines, "\n")
	if eventType == "" && payload != "[DONE]" {
		var envelope struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(payload), &envelope) == nil {
			eventType = envelope.Type
		}
	}
	return strings.TrimSpace(eventType), strings.TrimSpace(payload), true
}
