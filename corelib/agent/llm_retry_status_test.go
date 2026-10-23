package agent

import (
	"testing"
	"time"
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
