package skill

import (
	"reflect"
	"testing"
)

func TestExtractScriptRequiredEnvPython(t *testing.T) {
	got := ExtractScriptRequiredEnv(`
import os
token = os.environ["OPENAI_API_KEY"]
base = os.getenv("OPENAI_BASE_URL")
home = os.getenv("HOME")
optional = os.getenv("OPTIONAL_API_KEY", "fallback")
optional2 = os.environ.get("OPTIONAL_TOKEN", "")
`, "python")
	want := []string{"OPENAI_API_KEY", "OPENAI_BASE_URL"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractScriptRequiredEnv() = %#v, want %#v", got, want)
	}
}

func TestExtractScriptRequiredEnvNodePowerShellAndBash(t *testing.T) {
	got := ExtractScriptRequiredEnv(`
const token = process.env.API_TOKEN
const org = process.env["OPENAI_ORG_ID"]
const mode = process.env.NODE_ENV || "development"
$env:AZURE_OPENAI_KEY
$env:USERPROFILE
: "${SEARCH_API_KEY:?missing}"
if [ -z "$REPORT_TOKEN" ]; then exit 1; fi
`, "")
	want := []string{"API_TOKEN", "AZURE_OPENAI_KEY", "OPENAI_ORG_ID", "REPORT_TOKEN", "SEARCH_API_KEY"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractScriptRequiredEnv() = %#v, want %#v", got, want)
	}
}
