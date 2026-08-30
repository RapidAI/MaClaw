package llm

import "testing"

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
