package skill

import (
	"strings"
	"testing"
	"unicode/utf8"
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

func TestClassifyStepError_CommandNotFound_PowerShellMessage(t *testing.T) {
	result := ClassifyStepError(1,
		"The term 'xparse-cli' is not recognized as the name of a cmdlet, function, script file, or operable program.",
		"exit status 1",
		"xparse-cli parse report.pdf",
	)
	if result.Class != ErrCommandNotFound {
		t.Errorf("expected ErrCommandNotFound, got %s", result.Class)
	}
	if !contains(result.UserMessage, "xparse-cli") {
		t.Errorf("expected missing command name in message, got: %s", result.UserMessage)
	}
}

func TestClassifyStepError_CommandNotFoundSkipsEnvAssignmentsAndWrappers(t *testing.T) {
	result := ClassifyStepError(127, "", "bash: xparse-cli: command not found", `TOKEN="a b" env MODE=test xparse-cli parse report.pdf`)
	if result.Class != ErrCommandNotFound {
		t.Errorf("expected ErrCommandNotFound, got %s", result.Class)
	}
	if !contains(result.UserMessage, "xparse-cli") {
		t.Errorf("expected real missing command in message, got: %s", result.UserMessage)
	}
	for _, bad := range []string{"TOKEN=", "env\""} {
		if contains(result.UserMessage, bad) {
			t.Errorf("message %q should not identify wrapper/assignment %q as missing command", result.UserMessage, bad)
		}
	}
}

func TestClassifyStepError_CommandNotFoundFromRequirementPrecheck(t *testing.T) {
	result := ClassifyStepError(0, "", "skill \"xparse\" runner requirements not satisfied: required command xparse-cli was not found on PATH [action: install_dependency] Install xparse-cli and ensure it is available on PATH.", "")
	if result.Class != ErrCommandNotFound {
		t.Errorf("expected ErrCommandNotFound, got %s", result.Class)
	}
	if !contains(result.UserMessage, "xparse-cli") {
		t.Errorf("expected command name from precheck message, got: %s", result.UserMessage)
	}
	if contains(result.UserMessage, "exit 0") {
		t.Errorf("non-process precheck error should not mention exit 0: %s", result.UserMessage)
	}
	if !contains(result.ActionHint, "install_dependency") {
		t.Errorf("expected embedded install_dependency hint to be preserved, got: %s", result.ActionHint)
	}
	if !contains(result.ActionHint, "xparse-cli") {
		t.Errorf("expected full embedded install hint to be preserved, got: %s", result.ActionHint)
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

func TestClassifyStepError_MissingParamChineseOutput(t *testing.T) {
	result := ClassifyStepError(1, "\u9519\u8bef: \u7f3a\u5c11 city \u53c2\u6570", "exit status 1", "weather.py")
	if result.Class != ErrMissingParam {
		t.Errorf("expected ErrMissingParam, got %s", result.Class)
	}
	if contains(result.UserMessage, "\u7f3a\u5c11") {
		t.Errorf("UserMessage should be ASCII-stable, got %q", result.UserMessage)
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

func TestClassifyStepError_MissingEnvFromRequirementPrecheck(t *testing.T) {
	result := ClassifyStepError(0, "", "skill \"simple_trans\" runner requirements not satisfied: required environment variable OPENAI_API_KEY is not set [action: provide_env]", "")
	if result.Class != ErrMissingEnvVar {
		t.Errorf("expected ErrMissingEnvVar, got %s", result.Class)
	}
	if result.Repairable {
		t.Error("precheck missing_env_var should not be repairable")
	}
	if !contains(result.ActionHint, "provide_env") {
		t.Errorf("expected embedded provide_env hint to be preserved, got: %s", result.ActionHint)
	}
}

func TestClassifyStepError_MissingPythonPackageFromRequirementPrecheck(t *testing.T) {
	result := ClassifyStepError(0, "", "skill \"md2pdf\" runner requirements not satisfied: required Python package weasyprint>=61 is not installed [action: install_dependency] Install package weasyprint>=61, then retry the skill.", "")
	if result.Class != ErrMissingDependency {
		t.Errorf("expected ErrMissingDependency, got %s", result.Class)
	}
	if result.Repairable {
		t.Error("precheck missing_dependency should not be repairable")
	}
	if !contains(result.ActionHint, "install_dependency") {
		t.Errorf("expected embedded install_dependency hint to be preserved, got: %s", result.ActionHint)
	}
	if !contains(result.ActionHint, "weasyprint>=61") {
		t.Errorf("expected full embedded package install hint to be preserved, got: %s", result.ActionHint)
	}
	if contains(result.UserMessage, "exit 0") {
		t.Errorf("non-process precheck error should not mention exit 0: %s", result.UserMessage)
	}
}

func TestClassifyStepError_MissingNodePackageFromRequirementPrecheck(t *testing.T) {
	result := ClassifyStepError(0, "", "skill \"drawio\" runner requirements not satisfied: required Node package @drawio/export is not installed [action: install_dependency]", "")
	if result.Class != ErrMissingDependency {
		t.Errorf("expected ErrMissingDependency, got %s", result.Class)
	}
	if result.Retryable {
		t.Error("missing_dependency should not be retryable without installing the dependency")
	}
}

func TestClassifyStepError_MissingPythonPackageUsesPipHint(t *testing.T) {
	result := ClassifyStepError(0, "", "required Python package weasyprint>=61 is not installed", "")
	if result.Class != ErrMissingDependency {
		t.Errorf("expected ErrMissingDependency, got %s", result.Class)
	}
	if !strings.Contains(result.ActionHint, "Python package weasyprint>=61") || !strings.Contains(result.ActionHint, "pip") {
		t.Errorf("expected pip install hint with package name, got: %s", result.ActionHint)
	}
}

func TestClassifyStepError_MissingPackagePrecheckNameExtractionIsCaseInsensitive(t *testing.T) {
	result := ClassifyStepError(0, "", "required python package PyYAML is not installed", "")
	if result.Class != ErrMissingDependency {
		t.Errorf("expected ErrMissingDependency, got %s", result.Class)
	}
	if !strings.Contains(result.ActionHint, "PyYAML") {
		t.Errorf("expected package name from lowercase precheck marker, got: %s", result.ActionHint)
	}
}

func TestClassifyStepError_MissingNodePackageUsesNPMHint(t *testing.T) {
	result := ClassifyStepError(0, "", "required Node package @drawio/export is not installed", "")
	if result.Class != ErrMissingDependency {
		t.Errorf("expected ErrMissingDependency, got %s", result.Class)
	}
	if !strings.Contains(result.ActionHint, "Node package @drawio/export") || !strings.Contains(result.ActionHint, "npm") {
		t.Errorf("expected npm install hint with package name, got: %s", result.ActionHint)
	}
}

func TestClassifyStepError_MissingPythonModuleAtRuntime(t *testing.T) {
	result := ClassifyStepError(1, "Traceback...\nModuleNotFoundError: No module named 'weasyprint'", "exit status 1", "python convert.py")
	if result.Class != ErrMissingDependency {
		t.Errorf("expected ErrMissingDependency, got %s", result.Class)
	}
	if !contains(result.UserMessage, "weasyprint") {
		t.Errorf("expected dependency name in message, got: %s", result.UserMessage)
	}
	if !strings.Contains(result.ActionHint, "Python package weasyprint") || !strings.Contains(result.ActionHint, "pip") {
		t.Errorf("expected runtime pip install hint, got: %s", result.ActionHint)
	}
}

func TestClassifyStepError_MissingNodeModuleAtRuntime(t *testing.T) {
	result := ClassifyStepError(1, "Error: Cannot find module '@drawio/export'\nRequire stack:", "exit status 1", "node export.js")
	if result.Class != ErrMissingDependency {
		t.Errorf("expected ErrMissingDependency, got %s", result.Class)
	}
	if !contains(result.UserMessage, "@drawio/export") {
		t.Errorf("expected dependency name in message, got: %s", result.UserMessage)
	}
	if !strings.Contains(result.ActionHint, "Node package @drawio/export") || !strings.Contains(result.ActionHint, "npm") {
		t.Errorf("expected runtime npm install hint, got: %s", result.ActionHint)
	}
}

func TestClassifyStepError_MissingPythonModuleMapsImportNameToPackageName(t *testing.T) {
	result := ClassifyStepError(1, "Traceback...\nModuleNotFoundError: No module named 'PIL'", "exit status 1", "python convert.py")
	if result.Class != ErrMissingDependency {
		t.Errorf("expected ErrMissingDependency, got %s", result.Class)
	}
	if !strings.Contains(result.UserMessage, "Pillow") || !strings.Contains(result.UserMessage, "PIL") {
		t.Errorf("expected user message to show package and import names, got: %s", result.UserMessage)
	}
	if !strings.Contains(result.ActionHint, "Python package Pillow") || !strings.Contains(result.ActionHint, "pip") {
		t.Errorf("expected pip install hint with mapped package name, got: %s", result.ActionHint)
	}
}

func TestClassifyStepError_MissingPythonSubmoduleMapsTopLevelImport(t *testing.T) {
	result := ClassifyStepError(1, "Traceback...\nModuleNotFoundError: No module named 'PIL.Image'", "exit status 1", "python convert.py")
	if result.Class != ErrMissingDependency {
		t.Errorf("expected ErrMissingDependency, got %s", result.Class)
	}
	if !strings.Contains(result.UserMessage, "Pillow") || !strings.Contains(result.UserMessage, "PIL.Image") {
		t.Errorf("expected package and full import in message, got: %s", result.UserMessage)
	}
	if !strings.Contains(result.ActionHint, "Python package Pillow") || strings.Contains(result.ActionHint, "PIL.Image") {
		t.Errorf("expected pip install hint with top-level mapped package, got: %s", result.ActionHint)
	}
}

func TestClassifyStepError_MissingPythonModuleMapsLowercaseImportName(t *testing.T) {
	result := ClassifyStepError(1, "Traceback...\nModuleNotFoundError: No module named 'yaml'", "exit status 1", "python convert.py")
	if result.Class != ErrMissingDependency {
		t.Errorf("expected ErrMissingDependency, got %s", result.Class)
	}
	if !strings.Contains(result.ActionHint, "Python package PyYAML") {
		t.Errorf("expected mapped PyYAML package hint, got: %s", result.ActionHint)
	}
}

func TestClassifyStepError_MissingNodeModuleMapsSubpathToPackageName(t *testing.T) {
	result := ClassifyStepError(1, "Error: Cannot find module '@scope/pkg/subpath'\nRequire stack:", "exit status 1", "node export.js")
	if result.Class != ErrMissingDependency {
		t.Errorf("expected ErrMissingDependency, got %s", result.Class)
	}
	if !strings.Contains(result.ActionHint, "Node package @scope/pkg") || strings.Contains(result.ActionHint, "subpath") {
		t.Errorf("expected npm install hint with package root, got: %s", result.ActionHint)
	}
}

func TestClassifyStepError_MissingNodeESMPackageAtRuntime(t *testing.T) {
	result := ClassifyStepError(1, "Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'chalk' imported from C:\\skills\\tool.mjs", "exit status 1", "node export.mjs")
	if result.Class != ErrMissingDependency {
		t.Errorf("expected ErrMissingDependency, got %s", result.Class)
	}
	if !strings.Contains(result.ActionHint, "Node package chalk") || !strings.Contains(result.ActionHint, "npm") {
		t.Errorf("expected npm install hint for ESM package, got: %s", result.ActionHint)
	}
}

func TestClassifyStepError_LocalNodeModuleReferenceRemainsFileNotFound(t *testing.T) {
	result := ClassifyStepError(1, "Error: Cannot find module './local-helper'", "exit status 1", "node export.js")
	if result.Class == ErrMissingDependency {
		t.Fatalf("local module reference should not be classified as dependency: %#v", result)
	}
}

func TestClassifyStepError_WindowsNodeModulePathIsNotDependency(t *testing.T) {
	result := ClassifyStepError(1, `Error: Cannot find module 'D:\skills\helper.js'`, "exit status 1", "node export.js")
	if result.Class == ErrMissingDependency {
		t.Fatalf("windows module path should not be classified as dependency: %#v", result)
	}
}

func TestClassifyStepError_LocalPythonModuleReferenceIsNotDependency(t *testing.T) {
	result := ClassifyStepError(1, "Traceback...\nModuleNotFoundError: No module named './local_helper'", "exit status 1", "python convert.py")
	if result.Class == ErrMissingDependency {
		t.Fatalf("local module reference should not be classified as dependency: %#v", result)
	}
}

func TestClassifyStepError_PythonStdlibModuleIsNotDependency(t *testing.T) {
	result := ClassifyStepError(1, "Traceback...\nModuleNotFoundError: No module named 'json'", "exit status 1", "python convert.py")
	if result.Class == ErrMissingDependency {
		t.Fatalf("stdlib module should not be classified as pip dependency: %#v", result)
	}
}

func TestClassifyStepError_PythonStdlibSubmoduleIsNotDependency(t *testing.T) {
	result := ClassifyStepError(1, "Traceback...\nModuleNotFoundError: No module named 'json.decoder'", "exit status 1", "python convert.py")
	if result.Class == ErrMissingDependency {
		t.Fatalf("stdlib submodule should not be classified as pip dependency: %#v", result)
	}
}

func TestClassifyStepError_NodeBuiltinModuleIsNotDependency(t *testing.T) {
	result := ClassifyStepError(1, "Error: Cannot find module 'fs'\nRequire stack:", "exit status 1", "node export.js")
	if result.Class == ErrMissingDependency {
		t.Fatalf("node builtin should not be classified as npm dependency: %#v", result)
	}
}

func TestClassifyStepError_NodeBuiltinESMPackageIsNotDependency(t *testing.T) {
	result := ClassifyStepError(1, "Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'fs' imported from C:\\skills\\tool.mjs", "exit status 1", "node export.mjs")
	if result.Class == ErrMissingDependency {
		t.Fatalf("node builtin ESM package should not be classified as npm dependency: %#v", result)
	}
}

func TestFormatErrorForLLMDoesNotDuplicateEmbeddedActionHint(t *testing.T) {
	classified := ClassifyStepError(0, "", "skill \"xparse\" runner requirements not satisfied: required command xparse-cli was not found on PATH [action: install_dependency] Install xparse-cli and ensure it is available on PATH.", "")
	got := FormatErrorForLLM(classified)
	if strings.Count(got, "[action: install_dependency]") != 1 {
		t.Fatalf("FormatErrorForLLM() = %q, want one embedded action hint", got)
	}
	if !contains(got, "[class: command_not_found]") || !contains(got, "Install xparse-cli") {
		t.Fatalf("FormatErrorForLLM() = %q, want class and command context", got)
	}
}

func TestTruncateFormattedErrorForStoragePreservesActionHint(t *testing.T) {
	formatted := "[class: missing_env_var] " + strings.Repeat("long diagnostic detail ", 80) + "\n[action: inform_user] Configure the required environment variables."
	got := TruncateFormattedErrorForStorage(formatted, 300)
	if len(got) > 300 {
		t.Fatalf("TruncateFormattedErrorForStorage() length = %d, want <= 300", len(got))
	}
	if !strings.Contains(got, "[class: missing_env_var]") || !strings.Contains(got, "[action: inform_user]") {
		t.Fatalf("TruncateFormattedErrorForStorage() = %q, want class and action", got)
	}
	if !strings.Contains(got, "\n...\n") {
		t.Fatalf("TruncateFormattedErrorForStorage() = %q, want truncation marker", got)
	}
}

func TestTruncateFormattedErrorForStorageKeepsValidUTF8(t *testing.T) {
	formatted := "[class: unknown] " + strings.Repeat("缺少参数", 120) + "\n[action: inspect] Inspect the step output."
	got := TruncateFormattedErrorForStorage(formatted, 300)
	if !utf8.ValidString(got) {
		t.Fatalf("TruncateFormattedErrorForStorage() returned invalid UTF-8: %q", got)
	}
	if !strings.Contains(got, "[action: inspect]") {
		t.Fatalf("TruncateFormattedErrorForStorage() = %q, want action preserved", got)
	}
}

func TestTruncateFormattedErrorForStoragePreservesVeryLongActionHintTag(t *testing.T) {
	formatted := "[class: missing_dependency] dependency failure details\n[action: install_dependency] " + strings.Repeat("install package with a very long explanation ", 40)
	got := TruncateFormattedErrorForStorage(formatted, 220)
	if len(got) > 220 {
		t.Fatalf("TruncateFormattedErrorForStorage() length = %d, want <= 220", len(got))
	}
	if !strings.Contains(got, "[class: missing_dependency]") || !strings.Contains(got, "[action: install_dependency]") {
		t.Fatalf("TruncateFormattedErrorForStorage() = %q, want class and action tag", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("TruncateFormattedErrorForStorage() returned invalid UTF-8: %q", got)
	}
}

func TestTruncateFormattedErrorForStoragePreservesClassTagWithSmallBudget(t *testing.T) {
	formatted := "[class: missing_dependency] dependency failure details that cannot fit\n[action: install_dependency] " + strings.Repeat("install package with a very long explanation ", 20)
	got := TruncateFormattedErrorForStorage(formatted, 90)
	if len(got) > 90 {
		t.Fatalf("TruncateFormattedErrorForStorage() length = %d, want <= 90", len(got))
	}
	if !strings.Contains(got, "[class: missing_dependency]") || !strings.Contains(got, "[action:") {
		t.Fatalf("TruncateFormattedErrorForStorage() = %q, want complete class tag and action tag", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("TruncateFormattedErrorForStorage() returned invalid UTF-8: %q", got)
	}
}

func TestTruncateFormattedErrorForStoragePreservesClassTagWithShortActionAndSmallBudget(t *testing.T) {
	formatted := "[class: missing_env_var] " + strings.Repeat("diagnostic detail ", 20) + "\n[action: inform_user]"
	got := TruncateFormattedErrorForStorage(formatted, 55)
	if len(got) > 55 {
		t.Fatalf("TruncateFormattedErrorForStorage() length = %d, want <= 55", len(got))
	}
	if !strings.Contains(got, "[class: missing_env_var]") || !strings.Contains(got, "[action: inform_user]") {
		t.Fatalf("TruncateFormattedErrorForStorage() = %q, want complete class and short action tags", got)
	}
}

func TestClassifyStepError_EmbeddedActionHintStopsAtLineBreak(t *testing.T) {
	result := ClassifyStepError(0, "", "missing requirement [action: inspect_skill] Inspect the selected step.\nTraceback details should stay in the user message.", "")
	if result.Class != ErrUnknown {
		t.Errorf("expected ErrUnknown, got %s", result.Class)
	}
	if result.ActionHint != "[action: inspect_skill] Inspect the selected step." {
		t.Fatalf("ActionHint = %q, want only the embedded action line", result.ActionHint)
	}
}

func TestClassifyStepError_EmbeddedActionHintDoesNotSwallowNextViolation(t *testing.T) {
	errMsg := "required command xparse-cli was not found on PATH [action: install_dependency] Install xparse-cli.\nrequired Python package weasyprint is not installed [action: install_dependency] Install package weasyprint."
	result := ClassifyStepError(0, "", errMsg, "")
	if result.Class != ErrCommandNotFound {
		t.Errorf("expected ErrCommandNotFound, got %s", result.Class)
	}
	if result.ActionHint != "[action: install_dependency] Install xparse-cli." {
		t.Fatalf("ActionHint = %q, want first violation action line only", result.ActionHint)
	}
	if strings.Contains(result.ActionHint, "weasyprint") {
		t.Fatalf("ActionHint = %q, should not swallow next violation", result.ActionHint)
	}
}

func TestClassifyStepError_MissingPythonModuleInParentheses(t *testing.T) {
	// Regression test: babeldoc skill wraps the ModuleNotFoundError inside parentheses.
	// The package name extraction must strip () in addition to quotes.
	stderr := `[PaperTranslator] [babeldoc:err] C:\Users\ma139\.maclaw\python\install\python.exe: Error while finding module specification for 'babeldoc.main' (ModuleNotFoundError: No module named 'babeldoc') (exit code: 1)`
	result := ClassifyStepError(1, "", stderr, "python run.py input.pdf")
	if result.Class != ErrMissingDependency {
		t.Fatalf("expected ErrMissingDependency, got %s", result.Class)
	}
	if !strings.Contains(result.UserMessage, `"babeldoc"`) {
		t.Errorf("expected clean package name 'babeldoc' in message, got: %s", result.UserMessage)
	}
	// Verify the extracted name has no trailing parenthesis.
	name := missingDependencyNameFromMessage(stderr)
	if strings.Contains(name, ")") || strings.Contains(name, "(") {
		t.Fatalf("missingDependencyNameFromMessage() = %q, must not contain parentheses", name)
	}
	if name != "babeldoc" {
		t.Fatalf("missingDependencyNameFromMessage() = %q, want 'babeldoc'", name)
	}
}

func TestMissingDependencyNameFromMessage_WrappingCharacters(t *testing.T) {
	// Verify that various wrapping characters are stripped from package names.
	tests := []struct {
		input string
		want  string
	}{
		// Standard Python traceback
		{`ModuleNotFoundError: No module named 'requests'`, "requests"},
		// Double-quoted
		{`ModuleNotFoundError: No module named "requests"`, "requests"},
		// Parenthesized (babeldoc-style wrapper output)
		{`(ModuleNotFoundError: No module named 'babeldoc') (exit code: 1)`, "babeldoc"},
		// Square brackets (JSON log output)
		{`[error] No module named [pdfplumber]`, "pdfplumber"},
		// Curly braces (rare but possible in structured logs)
		{`{ModuleNotFoundError: No module named {docx}}`, "docx"},
		// No wrapping at all
		{`ModuleNotFoundError: No module named babeldoc`, "babeldoc"},
		// Submodule with dot
		{`ModuleNotFoundError: No module named 'PIL.Image'`, "PIL.Image"},
		// Backtick-wrapped (markdown-style)
		{"ModuleNotFoundError: No module named `yaml`", "yaml"},
		// Node.js: Cannot find module
		{`Error: Cannot find module '@scope/pkg'`, "@scope/pkg"},
		// Node.js: Cannot find package (ESM)
		{`Cannot find package 'chalk' imported from /app/index.mjs`, "chalk"},
	}
	for _, tt := range tests {
		got := missingDependencyNameFromMessage(tt.input)
		if got != tt.want {
			t.Errorf("missingDependencyNameFromMessage(%q) = %q, want %q", tt.input, got, tt.want)
		}
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

func TestClassifyStepError_UnsupportedStepAction(t *testing.T) {
	result := ClassifyStepError(0, "", "unsupported_step_action: craft_tool requires GUI skill runner; not supported by TUI runner", "")
	if result.Class != ErrUnsupportedAction {
		t.Errorf("expected ErrUnsupportedAction, got %s", result.Class)
	}
	if result.Repairable {
		t.Error("unsupported_action should not be repairable")
	}
	if !contains(result.UserMessage, "craft_tool requires GUI skill runner") {
		t.Errorf("expected actionable message, got: %s", result.UserMessage)
	}
	if !contains(result.ActionHint, "open_gui") {
		t.Errorf("expected open_gui action hint, got: %s", result.ActionHint)
	}
}

func TestClassifyStepError_UnsupportedGUIActionSuggestsPatch(t *testing.T) {
	result := ClassifyStepError(0, "", "unsupported_step_action: action \"python\" is not supported by gui runner; supported actions: bash, craft_tool", "")
	if result.Class != ErrUnsupportedAction {
		t.Errorf("expected ErrUnsupportedAction, got %s", result.Class)
	}
	if !contains(result.ActionHint, "patch") {
		t.Errorf("expected patch action hint, got: %s", result.ActionHint)
	}
	if contains(result.ActionHint, "open_gui") {
		t.Errorf("GUI unsupported action should not suggest opening GUI: %s", result.ActionHint)
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
