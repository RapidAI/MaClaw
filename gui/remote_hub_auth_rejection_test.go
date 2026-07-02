package main

import (
	"encoding/json"
	"testing"
)

func TestIsDefinitiveAuthRejection(t *testing.T) {
	tests := []struct {
		name     string
		payload  interface{}
		expected bool
	}{
		// Definitive rejections - should return true
		{
			name:     "code auth_failed",
			payload:  map[string]string{"code": "auth_failed", "message": "machine token is invalid"},
			expected: true,
		},
		{
			name:     "message machine not found",
			payload:  map[string]string{"message": "machine not found"},
			expected: true,
		},
		{
			name:     "message user not found",
			payload:  map[string]string{"message": "user not found in database"},
			expected: true,
		},
		{
			name:     "message invalid token (space)",
			payload:  map[string]string{"message": "Authentication failed: invalid token"},
			expected: true,
		},
		{
			name:     "message token_expired",
			payload:  map[string]string{"message": "token_expired"},
			expected: true,
		},
		{
			name:     "message token expired (space)",
			payload:  map[string]string{"message": "token expired, please re-register"},
			expected: true,
		},
		{
			name:     "reason unbound",
			payload:  map[string]string{"reason": "machine was unbound by admin"},
			expected: true,
		},
		{
			name:     "message unauthorized",
			payload:  map[string]string{"message": "unauthorized access"},
			expected: true,
		},
		{
			name:     "code machine_not_found",
			payload:  map[string]string{"code": "machine_not_found"},
			expected: true,
		},

		// Transient / non-rejection - should return false
		{
			name:     "empty payload",
			payload:  nil,
			expected: false,
		},
		{
			name:     "generic server error",
			payload:  map[string]string{"message": "internal server error"},
			expected: false,
		},
		{
			name:     "rate limited",
			payload:  map[string]string{"message": "too many requests, try again later"},
			expected: false,
		},
		{
			name:     "service unavailable",
			payload:  map[string]string{"message": "service temporarily unavailable"},
			expected: false,
		},
		{
			name:     "endpoint not found (generic 404)",
			payload:  map[string]string{"message": "endpoint not found: /api/v2/auth"},
			expected: false,
		},
		{
			name:     "connection overloaded",
			payload:  map[string]string{"code": "overloaded", "message": "too many connections"},
			expected: false,
		},
		{
			name:     "empty fields",
			payload:  map[string]string{"code": "", "message": "", "reason": ""},
			expected: false,
		},
		{
			name:     "maintenance mode",
			payload:  map[string]string{"message": "server is in maintenance mode"},
			expected: false,
		},
		{
			name:     "unparseable payload",
			payload:  "not a json object",
			expected: false,
		},
		{
			name:     "code not_found alone",
			payload:  map[string]string{"code": "not_found"},
			expected: false,
		},
		{
			name:     "code forbidden alone",
			payload:  map[string]string{"code": "forbidden"},
			expected: false,
		},
		{
			name:     "code not_found but message says endpoint",
			payload:  map[string]string{"code": "not_found", "message": "route /api/v3/hello not found"},
			expected: false,
		},
		{
			name:     "code forbidden but message says api path",
			payload:  map[string]string{"code": "forbidden", "message": "api not found for /api/v3/auth"},
			expected: false,
		},
		// Route error guard - machine-specific messages should NOT be blocked
		{
			name:     "code not_found with machine-specific message",
			payload:  map[string]string{"code": "not_found", "message": "machine m_123abc not registered in cluster"},
			expected: true,
		},
		{
			name:     "code not_found with 404 not found",
			payload:  map[string]string{"code": "not_found", "message": "404 not found"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload json.RawMessage
			if tt.payload != nil {
				data, err := json.Marshal(tt.payload)
				if err != nil {
					t.Fatalf("failed to marshal test payload: %v", err)
				}
				payload = data
			}
			got := isDefinitiveAuthRejection(payload)
			if got != tt.expected {
				t.Errorf("isDefinitiveAuthRejection(%s) = %v, want %v", string(payload), got, tt.expected)
			}
		})
	}
}
