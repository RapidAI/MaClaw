package llm

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestIsPermanentForbiddenError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{
			"transient gateway authorization denial",
			&HTTPStatusError{StatusCode: http.StatusForbidden, Body: []byte(`{"error":"authorization denied: no active authorization for hub=h tenant=t group=g"}`)},
			false,
		},
		{
			"body-free 403",
			&HTTPStatusError{StatusCode: http.StatusForbidden},
			false,
		},
		{
			"content policy",
			&HTTPStatusError{StatusCode: http.StatusForbidden, Body: []byte(`{"error":{"type":"content_policy_violation","message":"blocked by content policy"}}`)},
			true,
		},
		{
			"region lock",
			&HTTPStatusError{StatusCode: http.StatusForbidden, Body: []byte(`{"error":{"type":"RegionError","message":"model is not supported in your region"}}`)},
			true,
		},
		{
			"model entitlement missing",
			&HTTPStatusError{StatusCode: http.StatusForbidden, Body: []byte(`{"code":"LLM_MODEL_FORBIDDEN","message":"no active model service entitlement"}`)},
			true,
		},
		{
			"oauth token validation stays transient (handled before this check)",
			&HTTPStatusError{StatusCode: http.StatusForbidden, Body: []byte(`{"error":{"message":"The OAuth2 access token could not be validated."}}`)},
			false,
		},
		{
			"non-403 status",
			&HTTPStatusError{StatusCode: http.StatusInternalServerError, Body: []byte(`{"error":"content policy"}`)},
			false,
		},
		{
			"wrapped transient 403",
			fmt.Errorf("stream: %w", &HTTPStatusError{StatusCode: http.StatusForbidden, Body: []byte(`{"error":"authorization denied"}`)}),
			false,
		},
		{
			"plain non-status error",
			errors.New("HTTP 403: authorization denied"),
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPermanentForbiddenError(tc.err); got != tc.want {
				t.Fatalf("IsPermanentForbiddenError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
