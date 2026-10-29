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

func TestParseSSEStreamKeepsQoderBillingCode(t *testing.T) {
	body := "data: {\"error\":{\"message\":\"Qoder 上游错误 (110): Billing daily count exceeded\",\"code\":\"110\",\"type\":\"upstream_error\"}}\n\n"
	_, err := parseSSEStreamWithReasoning(strings.NewReader(body), nil, nil)
	var upstream *UpstreamModelError
	if !errors.As(err, &upstream) || upstream.Code != "110" {
		t.Fatalf("error = %#v", err)
	}
	if got := UserFacingError(err); got != "Qoder 日调用额度已用尽，请明日再试或切换其他模型提供方。" {
		t.Fatalf("user facing = %q", got)
	}
}

func TestParseSSEStreamKeepsNumericQoderBillingCode(t *testing.T) {
	body := "data: {\"code\":110,\"message\":\"Billing daily count exceeded\"}\n\n"
	_, err := parseSSEStreamWithReasoning(strings.NewReader(body), nil, nil)
	var upstream *UpstreamModelError
	if !errors.As(err, &upstream) || upstream.Code != "110" {
		t.Fatalf("error = %#v", err)
	}
	if got := UserFacingError(err); !strings.Contains(got, "日调用额度已用尽") {
		t.Fatalf("user facing = %q", got)
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
