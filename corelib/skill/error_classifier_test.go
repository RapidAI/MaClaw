package skill

import (
	"testing"
)

func TestClassifyStepError_CommandNotFound_Exit9009(t *testing.T) {
	result := ClassifyStepError(9009, "", "exit status 9009", "python3 script.py")
	if result.Class != ErrCommandNotFound {
		t.Errorf("expected ErrCommandNotFound, got %s", result.Class)
	}
	if !result.Repairable {
		t.Error("command_not_found should be repairable")
	}
	if result.Retryable {
		t.Error("command_not_found should not be retryable")
	}
	if result.UserMessage == "" {
		t.Error("UserMessage should not be empty")
	}
}

func TestClassifyStepError_CommandNotFound_Exit127(t *testing.T) {
	result := ClassifyStepError(127, "", "bash: python3: command not found", "python3 script.py")
	if result.Class != ErrCommandNotFound {
		t.Errorf("expected ErrCommandNotFound, got %s", result.Class)
	}
}

func TestClassifyStepError_RateLimit(t *testing.T) {
	result := ClassifyStepError(1, "HTTP 429 Too Many Requests: rate limit exceeded", "exit status 1", "curl api.example.com")
	if result.Class != ErrRateLimit {
		t.Errorf("expected ErrRateLimit, got %s", result.Class)
	}
	if result.Repairable {
		t.Error("rate_limit should not be repairable")
	}
	if !result.Retryable {
		t.Error("rate_limit should be retryable")
	}
}

func TestClassifyStepError_FileNotFound(t *testing.T) {
	result := ClassifyStepError(1, "Error: ENOENT: no such file or directory, open '/tmp/missing.txt'", "exit status 1", "node convert.js")
	if result.Class != ErrFileNotFound {
		t.Errorf("expected ErrFileNotFound, got %s", result.Class)
	}
	if !result.Repairable {
		t.Error("file_not_found should be repairable")
	}
}

func TestClassifyStepError_Timeout(t *testing.T) {
	result := ClassifyStepError(1, "", "context deadline exceeded", "python3 long_task.py")
	if result.Class != ErrTimeout {
		t.Errorf("expected ErrTimeout, got %s", result.Class)
	}
	if !result.Repairable {
		t.Error("timeout should be repairable")
	}
}

func TestClassifyStepError_NetworkError(t *testing.T) {
	result := ClassifyStepError(1, "dial tcp 127.0.0.1:8080: connection refused", "exit status 1", "curl localhost:8080")
	if result.Class != ErrNetworkError {
		t.Errorf("expected ErrNetworkError, got %s", result.Class)
	}
	if result.Repairable {
		t.Error("network_error should not be repairable")
	}
	if !result.Retryable {
		t.Error("network_error should be retryable")
	}
}

func TestClassifyStepError_AuthError(t *testing.T) {
	result := ClassifyStepError(1, "HTTP 401 Unauthorized: invalid API key", "exit status 1", "curl api.example.com")
	if result.Class != ErrAuthError {
		t.Errorf("expected ErrAuthError, got %s", result.Class)
	}
	if result.Repairable {
		t.Error("auth_error should not be repairable")
	}
}

func TestClassifyStepError_MissingParam(t *testing.T) {
	result := ClassifyStepError(2, "usage: convert.py [-h] --input INPUT --output OUTPUT\nerror: missing argument: --input", "exit status 2", "python3 convert.py")
	if result.Class != ErrMissingParam {
		t.Errorf("expected ErrMissingParam, got %s", result.Class)
	}
	if !result.Repairable {
		t.Error("missing_param should be repairable")
	}
}

func TestClassifyStepError_MissingEnvVar(t *testing.T) {
	result := ClassifyStepError(1, "Error: API_KEY environment variable not set", "exit status 1", "python3 query.py")
	if result.Class != ErrMissingEnvVar {
		t.Errorf("expected ErrMissingEnvVar, got %s", result.Class)
	}
	if result.Repairable {
		t.Error("missing_env_var should not be repairable")
	}
}

func TestClassifyStepError_Unknown(t *testing.T) {
	result := ClassifyStepError(1, "some random error output", "exit status 1", "my_tool")
	if result.Class != ErrUnknown {
		t.Errorf("expected ErrUnknown, got %s", result.Class)
	}
	if !result.Repairable {
		t.Error("unknown errors should be repairable (optimistic)")
	}
}

func TestClassifyStepError_Success(t *testing.T) {
	result := ClassifyStepError(0, "all good", "", "echo hello")
	if result.Class != ErrUnknown {
		t.Errorf("expected ErrUnknown for success case, got %s", result.Class)
	}
}

func TestClassifyStepError_PythonInstallHint(t *testing.T) {
	result := ClassifyStepError(9009, "", "exit status 9009", "python3 script.py")
	if result.Class != ErrCommandNotFound {
		t.Errorf("expected ErrCommandNotFound, got %s", result.Class)
	}
	if !contains(result.UserMessage, "python.org") {
		t.Errorf("expected python install hint in message, got: %s", result.UserMessage)
	}
}

func TestClassifyStepError_NodeInstallHint(t *testing.T) {
	result := ClassifyStepError(9009, "", "exit status 9009", "node convert.js")
	if result.Class != ErrCommandNotFound {
		t.Errorf("expected ErrCommandNotFound, got %s", result.Class)
	}
	if !contains(result.UserMessage, "nodejs.org") {
		t.Errorf("expected node install hint in message, got: %s", result.UserMessage)
	}
}

func TestClassifyStepError_IsRepairableAlignedWithSelfRepair(t *testing.T) {
	// Verify that the unified classifier's Repairable field aligns with
	// self_repair.go's nonRepairableErrorClasses for the two known classes.
	rateLimitResult := ClassifyStepError(1, "HTTP 429 rate limit", "exit status 1", "curl")
	if rateLimitResult.Repairable {
		t.Error("rate_limit should not be repairable (aligned with self_repair.go)")
	}

	networkResult := ClassifyStepError(1, "connection refused", "exit status 1", "curl")
	if networkResult.Repairable {
		t.Error("network_error should not be repairable (aligned with self_repair.go)")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && containsCI(s, substr)
}

func containsCI(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && len(substr) > 0 && indexCI(s, substr) >= 0)
}

func indexCI(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
