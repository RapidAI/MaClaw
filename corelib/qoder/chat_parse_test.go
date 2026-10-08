package qoder

import (
	"strings"
	"testing"
)

// TestParseChatSSEFailureFrames pins the probe's failure classification: no
// silent pass on empty streams, upstream tails surface as errors.
func TestParseChatSSEFailureFrames(t *testing.T) {
	cases := []struct {
		name    string
		stream  string
		want    string
		wantErr string
	}{
		{
			name:    "empty stream errors",
			stream:  "data:{\"firstTokenDuration\":123}\n\n",
			wantErr: "未返回任何模型内容",
		},
		{
			name:    "finish but no content errors",
			stream:  "data:{\"body\":\"{\\\"choices\\\":[{\\\"delta\\\":{},\\\"finish_reason\\\":\\\"stop\\\"}]}\"}\n\ndata:[DONE]\n\n",
			wantErr: "模型回复为空",
		},
		{
			name:    "plain json failure tail errors",
			stream:  "data:{\"hdrs\":1}\n\ndata:{\"success\":false,\"msgCode\":500,\"message\":\"Internal Server Error\"}\n\n",
			wantErr: "Qoder 上游错误 (HTTP 500): Internal Server Error",
		},
		{
			name:   "normal delta still passes",
			stream: "data:{\"body\":\"{\\\"choices\\\":[{\\\"delta\\\":{\\\"content\\\":\\\"ok\\\"}}]}\"}\n\ndata:[DONE]\n\n",
			want:   "ok",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseChatSSE(strings.NewReader(tc.stream))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want contains %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("content = %q, want %q", got, tc.want)
			}
		})
	}
}
