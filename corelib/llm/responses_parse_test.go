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
