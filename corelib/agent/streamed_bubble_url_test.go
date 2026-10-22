package agent

import (
	"strings"
	"testing"
)

func TestStreamedAnswerKeepingLinks(t *testing.T) {
	raw := "- 结论一：摘要已发布到知乎，链接为 https://zhuanlan.zhihu.com/p/2096778682895540557 。\n- 结论二：页面标题与文章一致，正文处于已发布阅读状态，没有进入编辑页。"
	stripped := streamedVisibleAnswer(raw)
	if strings.Contains(stripped, "://") {
		t.Fatalf("bubble text kept the URL: %q", stripped)
	}
	if got := streamedAnswerKeepingLinks(raw, stripped, nil); !strings.Contains(got, "https://zhuanlan.zhihu.com/p/2096778682895540557") {
		t.Fatalf("link dropped without a web search: %q", got)
	}
	search := []ConversationEntry{{Role: "tool", ToolName: "web_search", Content: "Public web results for x"}}
	if got := streamedAnswerKeepingLinks(raw, stripped, search); got != stripped {
		t.Fatalf("after a web search the cited URL must stay dropped: %q", got)
	}
}
