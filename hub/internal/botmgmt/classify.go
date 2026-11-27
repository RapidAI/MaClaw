package botmgmt

import "strings"

// ClassifyBotTask picks the cloud-agent kind a new bot task should run on.
// It is a pure function over the request text — no LLM call, no state — and
// defaults to "session" when nothing matches.
//
// Rules:
//   - coding: change code / repo / function, refactor, fix a compile or
//     build error, git commit/push/PR, local build & debug, write unit
//     tests, edit source files.
//   - session: chat, browse the web, post to social media (小红书, 微博,
//     朋友圈), write copy for publishing, look things up, search.
//
// Conflict resolution: social posting / browsing / research wins session;
// repo / code / PR / build-debug wins coding. The two priority groups are
// checked in that order — a social intent word short-circuits before any
// coding word, and a coding word beats the plain-chat fallback.
func ClassifyBotTask(text string) string {
	text = strings.ToLower(text)
	if containsAny(text, classifySessionTerms) {
		return "session"
	}
	if containsAny(text, classifyCodingTerms) || standalonePR(text) {
		return "coding"
	}
	return "session"
}

// classifySessionTerms are social-media / browsing / research intents that
// outrank any coding keyword in the same message.
var classifySessionTerms = []string{
	"小红书",
	"微博",
	"朋友圈",
	"twitter",
	"x.com",
	"发帖",
	"发个帖",
	"发一条",
	"发布一条",
	"发布到",
	"发到",
	"浏览",
	"查一下",
	"查资料",
	"查一查",
	"搜一下",
	"搜索",
	"搜搜",
	"google",
	"百度",
	"写文案",
	"文案",
	"种草",
	"推文",
	"lookup",
	"search for",
	"browse",
}

// classifyCodingTerms mark repo / code / build-debug work. Anything here
// routes the task to a coding instance unless a session term already won.
var classifyCodingTerms = []string{
	"代码",
	"仓库",
	"repo",
	"repository",
	"函数",
	"重构",
	"refactor",
	"编译错误",
	"编译失败",
	"构建失败",
	"编译",
	"构建",
	"调试",
	"debug",
	"commit",
	"push",
	"pull request",
	"merge",
	"分支",
	"branch",
	"单元测试",
	"单测",
	"unit test",
	"源文件",
	"源码",
	"源代码",
	"bug",
	"修复",
	"报错",
	"stack trace",
	".go",
	".py",
	".ts",
	".tsx",
	".js",
	".java",
	".rs",
	".cpp",
	".c ",
	".h ",
	".dart",
	".sql",
	"build",
	"compile",
}

func containsAny(text string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

// standalonePR reports whether text contains a bare "pr" token. The byte on
// each side must not be an ASCII letter or digit, so "prompt", "press", and
// "apr" do not match while "提交pr"（CJK 边界）, "a pr", and a trailing "PR" do.
func standalonePR(text string) bool {
	for i := 0; i+1 < len(text); i++ {
		if text[i] != 'p' || text[i+1] != 'r' {
			continue
		}
		beforeOK := i == 0 || !isASCIIAlphaNum(text[i-1])
		afterOK := i+2 == len(text) || !isASCIIAlphaNum(text[i+2])
		if beforeOK && afterOK {
			return true
		}
	}
	return false
}

func isASCIIAlphaNum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
