package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseResponsesSSEEventPreservesMultilineData(t *testing.T) {
	eventType, payload, ok := parseResponsesSSEEvent("event: response.reasoning_summary_text.delta\r\ndata: {\"delta\":\"First \",\r\ndata: \"provider\":\"test\"}")
	if !ok {
		t.Fatal("parseResponsesSSEEvent returned ok=false")
	}
	if eventType != "response.reasoning_summary_text.delta" {
		t.Fatalf("event type = %q", eventType)
	}
	if want := "{\"delta\":\"First \",\n\"provider\":\"test\"}"; payload != want {
		t.Fatalf("payload = %q, want %q", payload, want)
	}
}

func TestParseResponsesSSEEventUsesPayloadTypeFallback(t *testing.T) {
	eventType, payload, ok := parseResponsesSSEEvent("data: {\"type\":\"response.reasoning.delta\",\"delta\":\"summary\"}")
	if !ok {
		t.Fatal("parseResponsesSSEEvent returned ok=false")
	}
	if eventType != "response.reasoning.delta" {
		t.Fatalf("event type = %q", eventType)
	}
	if payload == "" {
		t.Fatal("payload is empty")
	}
}

func TestScanResponsesSSEEventSplitsCRLFFrames(t *testing.T) {
	data := []byte("event: one\r\ndata: a\r\n\r\nevent: two\r\ndata: b\r\n\r\n")
	advance, token, err := scanResponsesSSEEvent(data, false)
	if err != nil || string(token) != "event: one\r\ndata: a" {
		t.Fatalf("first frame = %q, advance=%d, err=%v", token, advance, err)
	}
	_, token, err = scanResponsesSSEEvent(data[advance:], false)
	if err != nil || string(token) != "event: two\r\ndata: b" {
		t.Fatalf("second frame = %q, err=%v", token, err)
	}
}

func TestScanResponsesSSEEventPrefersCRLFBoundaryAfterLFInsideData(t *testing.T) {
	data := []byte("event: one\r\ndata: first\n\r\ndata: second\r\n\r\nevent: two\r\ndata: b\r\n\r\n")
	advance, token, err := scanResponsesSSEEvent(data, false)
	if err != nil || string(token) != "event: one\r\ndata: first\n\r\ndata: second" {
		t.Fatalf("first frame = %q, advance=%d, err=%v", token, advance, err)
	}
	_, token, err = scanResponsesSSEEvent(data[advance:], false)
	if err != nil || string(token) != "event: two\r\ndata: b" {
		t.Fatalf("second frame = %q, err=%v", token, err)
	}
}

func TestScanResponsesSSEEventAcceptsEmptyAndOneByteBuffers(t *testing.T) {
	for _, data := range [][]byte{nil, {}, {'e'}} {
		advance, token, err := scanResponsesSSEEvent(data, false)
		if err != nil {
			t.Fatalf("scanResponsesSSEEvent(%q) error = %v", data, err)
		}
		if advance != 0 || token != nil {
			t.Fatalf("scanResponsesSSEEvent(%q) = advance=%d token=%q, want no frame", data, advance, token)
		}
	}
}

func TestResponsesReasoningOutputItemAcceptsSummaryAlias(t *testing.T) {
	item := responsesAPIReasoningOutputItem{
		Type:    "reasoning_summary",
		Summary: []responsesAPIReasoningContent{{Type: "summary_text", Text: "safe summary"}},
	}
	if got, want := item.DisplaySummary(), "safe summary"; got != want {
		t.Fatalf("DisplaySummary() = %q, want %q", got, want)
	}
}

func TestAppendResponsesReasoningSummaryDeduplicatesRepeatedFinalItem(t *testing.T) {
	var buf strings.Builder
	if got := appendResponsesReasoningSummary(&buf, "Streamed summary."); got != "Streamed summary." {
		t.Fatalf("first append = %q", got)
	}
	if got := appendResponsesReasoningSummary(&buf, "Streamed summary."); got != "" {
		t.Fatalf("duplicate append = %q, want empty", got)
	}
	if got, want := buf.String(), "Streamed summary."; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestAppendResponsesReasoningSummaryCompletesPartialStream(t *testing.T) {
	var buf strings.Builder
	appendResponsesReasoningSummary(&buf, "First ")
	if got := appendResponsesReasoningSummary(&buf, "First answer."); got != " answer." {
		t.Fatalf("second append = %q", got)
	}
	if got, want := buf.String(), "First answer."; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestAppendResponsesReasoningSummaryAppendsOnlyOverlappingSuffix(t *testing.T) {
	var buf strings.Builder
	appendResponsesReasoningSummary(&buf, "Inspect inputs and ")
	if got := appendResponsesReasoningSummary(&buf, "and return answer."); got != " return answer." {
		t.Fatalf("overlap append = %q, want suffix only", got)
	}
	if got, want := buf.String(), "Inspect inputs and return answer."; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestAppendResponsesReasoningSummaryHandlesUTF8Overlap(t *testing.T) {
	var buf strings.Builder
	appendResponsesReasoningSummary(&buf, "先检查输入，然后")
	if got := appendResponsesReasoningSummary(&buf, "然后给出答案。"); got != "给出答案。" {
		t.Fatalf("UTF-8 overlap append = %q, want suffix only", got)
	}
	if got, want := buf.String(), "先检查输入，然后给出答案。"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestResponsesCompletedReasoningSummariesExtractsOnlyDisplaySafeOutput(t *testing.T) {
	payload := []byte(`{
        "response":{"output":[
            {"type":"reasoning","summary":[{"type":"summary_text","text":"Safe summary."}]},
            {"type":"message","content":[{"type":"output_text","text":"Do not include."}]},
            {"type":"reasoning_summary","summary":[{"type":"summary_text","text":"Second summary."}]}
        ]}
    }`)
	if got, want := responsesCompletedReasoningSummaries(payload), []string{"Safe summary.", "Second summary."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("summaries = %#v, want %#v", got, want)
	}
}
