package memory

import (
	"strings"
	"testing"
)

func TestRedactSecretsInMemory_APIKey(t *testing.T) {
	input := "The API key is sk-abcdefghijklmnopqrstuvwxyz1234567890"
	result := redactSecretsInMemory(input)
	if strings.Contains(result, "sk-abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("API key not redacted: %s", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker in output: %s", result)
	}
}

func TestRedactSecretsInMemory_Password(t *testing.T) {
	input := "SSH config: host=api.rapidai.tech password=SuperSecret123"
	result := redactSecretsInMemory(input)
	if strings.Contains(result, "SuperSecret123") {
		t.Errorf("password not redacted: %s", result)
	}
}

func TestRedactSecretsInMemory_AWSKey(t *testing.T) {
	input := "AWS access key: AKIAIOSFODNN7EXAMPLE"
	result := redactSecretsInMemory(input)
	if strings.Contains(result, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("AWS key not redacted: %s", result)
	}
}

func TestRedactSecretsInMemory_PrivateKey(t *testing.T) {
	input := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA..."
	result := redactSecretsInMemory(input)
	if strings.Contains(result, "BEGIN RSA PRIVATE KEY") {
		t.Errorf("private key header not redacted: %s", result)
	}
}

func TestRedactSecretsInMemory_JWT(t *testing.T) {
	input := "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	result := redactSecretsInMemory(input)
	if strings.Contains(result, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9") {
		t.Errorf("JWT not redacted: %s", result)
	}
}

func TestRedactSecretsInMemory_NormalContent(t *testing.T) {
	input := "The user prefers Python 3.12 with PostgreSQL 16 for the project."
	result := redactSecretsInMemory(input)
	if result != input {
		t.Errorf("normal content was modified: got %q, want %q", result, input)
	}
}

func TestRedactSecretsInMemory_Empty(t *testing.T) {
	result := redactSecretsInMemory("")
	if result != "" {
		t.Errorf("empty input should return empty: got %q", result)
	}
}
