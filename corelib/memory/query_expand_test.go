package memory

import (
	"strings"
	"testing"
)

func TestExpandQuery_NumberPlusNoun(t *testing.T) {
	result := ExpandQuery("登录 4090 服务器检查 GPU 占用率")
	assertContainsAny(t, result.Entities, []string{"4090服务器", "4090"})
}

func TestExpandQuery_DeployScript(t *testing.T) {
	result := ExpandQuery("用上次那个部署脚本")
	assertContainsAny(t, result.Entities, []string{"部署脚本"})
}

func TestExpandQuery_TestEnvironment(t *testing.T) {
	result := ExpandQuery("连上测试环境跑一下")
	assertContainsAny(t, result.Entities, []string{"测试环境"})
}

func TestExpandQuery_PersonName(t *testing.T) {
	result := ExpandQuery("帮我看看张三的项目进度")
	// "张三" is only 2 chars, may appear in tokens
	found := false
	for _, tok := range result.QueryTokens {
		if tok == "张三" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected QueryTokens to contain '张三', got %v", result.QueryTokens)
	}
}

func TestExpandQuery_ClaudeKey(t *testing.T) {
	result := ExpandQuery("用之前配好的 Claude key")
	assertContainsAny(t, result.Entities, []string{"Claude"})
}

func TestExpandQuery_QuotedContent(t *testing.T) {
	result := ExpandQuery(`打开"生产数据库"看看`)
	assertContainsAny(t, result.Entities, []string{"生产数据库"})
}

func TestExpandQuery_ChineseQuotes(t *testing.T) {
	result := ExpandQuery(`连接「测试服务器」`)
	assertContainsAny(t, result.Entities, []string{"测试服务器"})
}

func TestExpandQuery_IPAddress(t *testing.T) {
	result := ExpandQuery("连接 192.168.1.100 看看")
	assertContainsAny(t, result.Entities, []string{"192.168.1.100"})
}

func TestExpandQuery_UnixPath(t *testing.T) {
	result := ExpandQuery("编辑 /etc/nginx/nginx.conf")
	assertContainsAny(t, result.Entities, []string{"/etc/nginx/nginx.conf"})
}

func TestExpandQuery_WindowsPath(t *testing.T) {
	result := ExpandQuery(`打开 C:\Users\test\config.yaml`)
	assertContainsAny(t, result.Entities, []string{`C:\Users\test\config.yaml`})
}

func TestExpandQuery_DomainName(t *testing.T) {
	result := ExpandQuery("访问 test.example.com 的接口")
	assertContainsAny(t, result.Entities, []string{"test.example.com"})
}

func TestExpandQuery_EnglishProperNoun(t *testing.T) {
	result := ExpandQuery("配置 Visual Studio 的插件")
	assertContainsAny(t, result.Entities, []string{"Visual Studio"})
}

func TestExpandQuery_EmptyMessage(t *testing.T) {
	result := ExpandQuery("")
	if len(result.Entities) != 0 {
		t.Errorf("expected empty entities for empty message, got %v", result.Entities)
	}
	if len(result.QueryTokens) != 0 {
		t.Errorf("expected empty tokens for empty message, got %v", result.QueryTokens)
	}
}

func TestExpandQuery_PureStopwords(t *testing.T) {
	result := ExpandQuery("帮我看看")
	// Should not extract stopword-only entities
	for _, e := range result.Entities {
		if chineseStopwords[e] {
			t.Errorf("entity %q should not be a pure stopword", e)
		}
	}
}

func TestExpandQuery_MaxEntities(t *testing.T) {
	// Message with many potential entities
	result := ExpandQuery(`连接 192.168.1.1 和 192.168.1.2 还有 test.example.com 以及 "数据库A" 和 "数据库B" 和 "缓存C"`)
	if len(result.Entities) > maxEntities {
		t.Errorf("entities count %d exceeds max %d", len(result.Entities), maxEntities)
	}
}

func TestExpandQuery_MaxTokens(t *testing.T) {
	// Very long message
	msg := "这是一个非常长的消息包含很多不同的词语和短语用来测试分词上限功能是否正常工作以及各种边界情况的处理能力"
	result := ExpandQuery(msg)
	if len(result.QueryTokens) > maxQueryTokens {
		t.Errorf("tokens count %d exceeds max %d", len(result.QueryTokens), maxQueryTokens)
	}
}

func TestTokenizeForTagMatch_Basic(t *testing.T) {
	tokens := tokenizeForTagMatch("登录 4090 服务器")
	// Should contain "4090" and Chinese segments
	found4090 := false
	for _, tok := range tokens {
		if tok == "4090" {
			found4090 = true
		}
	}
	if !found4090 {
		t.Errorf("expected tokens to contain '4090', got %v", tokens)
	}
}

func TestTokenizeForTagMatch_MixedLanguage(t *testing.T) {
	tokens := tokenizeForTagMatch("配置 Claude API 的 key")
	foundClaude := false
	for _, tok := range tokens {
		if tok == "claude" {
			foundClaude = true
		}
	}
	if !foundClaude {
		t.Errorf("expected tokens to contain 'claude', got %v", tokens)
	}
}

func TestExpandQuery_TechTerm(t *testing.T) {
	result := ExpandQuery("运行 pytest 测试")
	assertContainsAny(t, result.Entities, []string{"pytest"})
}

func TestExpandQuery_NumberNounNoSpace(t *testing.T) {
	result := ExpandQuery("检查3080机器的温度")
	assertContainsAny(t, result.Entities, []string{"3080机器", "3080"})
}

func TestExpandQuery_UppercaseAcronym(t *testing.T) {
	result := ExpandQuery("检查 GPU 占用率")
	assertContainsAny(t, result.Entities, []string{"GPU"})
}

func TestExpandQuery_SSHAcronym(t *testing.T) {
	result := ExpandQuery("通过 SSH 连接服务器")
	assertContainsAny(t, result.Entities, []string{"SSH"})
}

func TestExpandQuery_EnglishStopwordsNotExtracted(t *testing.T) {
	result := ExpandQuery("check this with the other tool")
	for _, e := range result.Entities {
		lower := strings.ToLower(e)
		if englishStopwords[lower] {
			t.Errorf("entity %q should not be an English stopword", e)
		}
	}
}

func TestTokenizeForTagMatch_NoEnglishStopwords(t *testing.T) {
	tokens := tokenizeForTagMatch("check this with the other tool from here")
	for _, tok := range tokens {
		if englishStopwords[tok] {
			t.Errorf("token %q should not be an English stopword", tok)
		}
	}
}

// assertContainsAny checks that at least one of the expected strings
// appears in the actual slice.
func assertContainsAny(t *testing.T, actual []string, anyOf []string) {
	t.Helper()
	for _, want := range anyOf {
		for _, got := range actual {
			if got == want {
				return
			}
		}
	}
	t.Errorf("expected any of %v in %v", anyOf, actual)
}
