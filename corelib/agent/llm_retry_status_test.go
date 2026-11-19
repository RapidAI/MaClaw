package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestLLMRetryLiveStatusTextStripsTheRowMarker(t *testing.T) {
	raw := LLMRetryWaitStatus(2, maxLLMRetries, 4*time.Second, false)
	visible, ok := LLMRetryLiveStatusText(raw)
	if !ok || visible != "重试 2/5：API 暂时不可用，4 秒后重试" {
		t.Fatalf("visible=%q ok=%v raw=%q", visible, ok, raw)
	}
	if _, ok := LLMRetryLiveStatusText("正在读取文件"); ok {
		t.Fatal("ordinary progress was treated as a live-status row")
	}
	auth, ok := LLMRetryLiveStatusText(LLMRetryAuthRefreshStatus())
	if !ok || auth != "已刷新模型登录状态，即将重试..." {
		t.Fatalf("auth=%q ok=%v", auth, ok)
	}
}

func TestLLMSwitchPhraseNamesARealChangeAndHidesSecrets(t *testing.T) {
	from := corelib.MaclawLLMConfig{ProviderName: "Hub", Model: "old", URL: "https://llm.example/v1?key=secret", Key: "sk-secret"}
	to := corelib.MaclawLLMConfig{ProviderName: "Hub", Model: "new", URL: "https://llm.example/v1", Key: "sk-other"}
	phrase, ok := LLMSwitchPhrase(from, to)
	if !ok || phrase != "已从「Hub/old」改用「Hub/new」，" {
		t.Fatalf("phrase=%q ok=%v", phrase, ok)
	}
	if strings.Contains(phrase, "secret") || strings.Contains(phrase, "sk-") {
		t.Fatalf("phrase leaked a credential: %q", phrase)
	}
	line, ok := LLMRetrySwitchedInFlightStatus(1, from, to)
	visible, marked := LLMRetryLiveStatusText(line)
	if !ok || !marked || !strings.Contains(visible, "Hub/old") || !strings.Contains(visible, "Hub/new") || !strings.Contains(visible, "第 1 次") {
		t.Fatalf("line=%q ok=%v visible=%q", line, ok, visible)
	}
	if _, ok := LLMSwitchPhrase(from, from); ok {
		t.Fatal("identical route claimed a switch")
	}
	tokenOnly := to
	tokenOnly.Model = from.Model
	tokenOnly.Key = "rotated-token"
	if _, ok := LLMSwitchPhrase(from, tokenOnly); ok {
		t.Fatal("a new token claimed a model switch")
	}
	otherHost := from
	otherHost.URL = "https://other.example/v1?token=hidden"
	phrase, ok = LLMSwitchPhrase(from, otherHost)
	if !ok || !strings.Contains(phrase, "另一个服务") || strings.Contains(phrase, "hidden") || strings.Contains(phrase, "other.example") {
		t.Fatalf("host change phrase=%q ok=%v", phrase, ok)
	}
}
