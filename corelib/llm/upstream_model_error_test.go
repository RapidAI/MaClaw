package llm

import (
	"errors"
	"strings"
	"testing"
)

func TestParseSSEStreamSurfacesUpstreamErrorFrame(t *testing.T) {
	body := "data: {\"error\":{\"message\":\"context length exceeded\",\"type\":\"upstream_error\"}}\n\n"
	_, err := parseSSEStreamWithReasoning(strings.NewReader(body), nil, nil)
	var upstream *UpstreamModelError
	if !errors.As(err, &upstream) || !strings.Contains(err.Error(), "context length exceeded") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseSSEToResponseSurfacesQoderCode(t *testing.T) {
	body := []byte("data: {\"code\":\"InvalidParameter\",\"message\":\"tools schema rejected\"}\n\n")
	_, err := ParseSSEToResponse(body)
	if err == nil || !strings.Contains(err.Error(), "tools schema rejected") || !strings.Contains(err.Error(), "InvalidParameter") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseNonStreamOpenAIResponseBodySurfacesErrorObject(t *testing.T) {
	_, err := ParseNonStreamOpenAIResponseBody([]byte(`{"error":{"message":"model overloaded","code":"overloaded"}}`))
	if err == nil || !strings.Contains(err.Error(), "model overloaded") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseSSEStreamKeepsNormalChunk(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"
	resp, err := parseSSEStreamWithReasoning(strings.NewReader(body), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("resp = %#v", resp)
	}
}
