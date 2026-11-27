package llm

import (
	"strings"
	"testing"
)

func TestParseNonStreamResponsesAPIBody_AcceptsProviderCompatibleTextParts(t *testing.T) {
	response, err := ParseNonStreamResponsesAPIBody([]byte(`{
        "output":[{"type":"message","content":[
            {"type":"text","text":"red"},
            {"type":"output_text","content":" image"}
        ]}]
    }`))
	if err != nil {
		t.Fatalf("ParseNonStreamResponsesAPIBody returned error: %v", err)
	}
	if got := response.Choices[0].Message.Content; got != "redimage" {
		t.Fatalf("content = %q, want %q", got, "redimage")
	}
}

func TestParseNonStreamResponsesAPIBody_ExtractsDisplaySafeReasoningSummary(t *testing.T) {
	response, err := ParseNonStreamResponsesAPIBody([]byte(`{
        "output":[
            {"type":"reasoning","summary":[{"type":"summary_text","text":"Check inputs. "},{"type":"summary_text","text":"Then answer."}]},
            {"type":"message","content":[{"type":"output_text","text":"Done."}]}
        ]
    }`))
	if err != nil {
		t.Fatalf("ParseNonStreamResponsesAPIBody returned error: %v", err)
	}
	if got, want := response.Choices[0].Message.ReasoningContent, "Check inputs. Then answer."; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
	if got, want := response.Choices[0].Message.Content, "Done."; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestParseNonStreamResponsesAPIBody_ExtractsReasoningSummaryAlias(t *testing.T) {
	response, err := ParseNonStreamResponsesAPIBody([]byte(`{
        "output":[{"type":"reasoning_summary","summary":[{"type":"summary_text","text":"Safe summary."}]}]
    }`))
	if err != nil {
		t.Fatalf("ParseNonStreamResponsesAPIBody returned error: %v", err)
	}
	if got, want := response.Choices[0].Message.ReasoningContent, "Safe summary."; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
}

func TestParseNonStreamResponsesAPIBody_AcceptsThinkingPart(t *testing.T) {
	response, err := ParseNonStreamResponsesAPIBody([]byte(`{
        "output":[{"type":"reasoning","summary":[{"type":"thinking","text":"Thought it through."}]}]
    }`))
	if err != nil {
		t.Fatalf("ParseNonStreamResponsesAPIBody returned error: %v", err)
	}
	if got, want := response.Choices[0].Message.ReasoningContent, "Thought it through."; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
}

func TestParseNonStreamResponsesAPIBody_IncompleteIsLength(t *testing.T) {
	response, err := ParseNonStreamResponsesAPIBody([]byte(`{
        "status": "incomplete",
        "incomplete_details": {"reason": "max_output_tokens"},
        "output":[{"type":"message","content":[{"type":"output_text","text":"print('hi'"}]}]
    }`))
	if err != nil {
		t.Fatalf("ParseNonStreamResponsesAPIBody: %v", err)
	}
	if got := response.Choices[0].FinishReason; got != "length" {
		t.Fatalf("finish reason = %q, want length", got)
	}
	if got := response.Choices[0].Message.Content; got != "print('hi'" {
		t.Fatalf("content = %q", got)
	}

	completed, err := ParseNonStreamResponsesAPIBody([]byte(`{
        "status": "completed",
        "output":[{"type":"message","content":[{"type":"output_text","text":"print(1)"}]}]
    }`))
	if err != nil {
		t.Fatalf("completed body: %v", err)
	}
	if got := completed.Choices[0].FinishReason; got != "stop" {
		t.Fatalf("completed finish reason = %q, want stop", got)
	}
}

func TestParseNonStreamResponsesAPIBody_FailedStatusIsRetryableError(t *testing.T) {
	body := []byte(`{
        "id": "resp_failed",
        "status": "failed",
        "error": {"code": "server_error", "message": "The server had an error while processing your request."},
        "output": [{"type": "reasoning", "summary": [{"type": "summary_text", "text": "still thinking"}]}]
    }`)
	response, err := ParseNonStreamResponsesAPIBody(body)
	if err == nil || !IsTransientResponsesStreamError(err) || !strings.Contains(err.Error(), "code=server_error") {
		t.Fatalf("err = %v, want a transient failed status", err)
	}
	if response == nil || ResponseHasCommittedAnswer(response) {
		t.Fatalf("response = %#v, want an uncommitted partial", response)
	}
	if got := response.Choices[0].Message.Content; got != "" {
		t.Fatalf("content = %q, want reasoning kept off the answer", got)
	}

	answered := []byte(`{
        "status": "failed",
        "error": {"code": "server_error", "message": "The server had an error while processing your request."},
        "output": [{"type": "message", "content": [{"type": "output_text", "text": "partial answer"}]}]
    }`)
	response, err = ParseNonStreamResponsesAPIBody(answered)
	if err == nil || !IsTransientResponsesStreamError(err) || !ResponseHasCommittedAnswer(response) {
		t.Fatalf("answered failed status: err=%v response=%#v", err, response)
	}

	filtered := []byte(`{
        "status": "failed",
        "error": {"code": "content_filter", "message": "blocked by content filter"},
        "output": [{"type": "reasoning", "summary": [{"type": "summary_text", "text": "thinking"}]}]
    }`)
	_, err = ParseNonStreamResponsesAPIBody(filtered)
	if err == nil || IsTransientResponsesStreamError(err) || !IsResponsesStreamFailure(err) {
		t.Fatalf("err = %v, want a permanent failed status", err)
	}
}

func TestParseNonStreamResponsesAPIBody_SeparatesMultipleReasoningItems(t *testing.T) {
	response, err := ParseNonStreamResponsesAPIBody([]byte(`{
        "output":[
            {"type":"reasoning","summary":[{"type":"summary_text","text":"First item."}]},
            {"type":"reasoning","summary":[{"type":"summary_text","text":"Second item."}]}
        ]
    }`))
	if err != nil {
		t.Fatalf("ParseNonStreamResponsesAPIBody returned error: %v", err)
	}
	if got, want := response.Choices[0].Message.ReasoningContent, "First item.\nSecond item."; got != want {
		t.Fatalf("reasoning_content = %q, want %q", got, want)
	}
}
