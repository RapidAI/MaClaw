package botmgmt

import "testing"

func TestClassifyBotTask(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		// coding intents
		{"edit code", "帮我改一下这个函数的代码", "coding"},
		{"repo", "检查这个仓库的提交历史", "coding"},
		{"refactor", "refactor the login module", "coding"},
		{"compile error", "修一下这个编译错误", "coding"},
		{"git commit", "帮我把这些改动 commit 并 push", "coding"},
		{"pull request", "review 这个 pull request", "coding"},
		{"build debug", "本地构建失败，帮我调试一下", "coding"},
		{"unit test", "给这个函数写单元测试", "coding"},
		{"source file", "编辑 main.go 里的启动逻辑", "coding"},
		{"bug fix", "修复这个 bug", "coding"},
		{"stack trace", "看下这个 stack trace 是什么问题", "coding"},
		{"repo login logic", "改一下仓库里的登录逻辑", "coding"},
		{"submit pr", "帮我提交 PR", "coding"},
		{"submit pr no space", "改动好了，提交pr", "coding"},
		{"local compile debug", "本地编译跑不起来，调试下", "coding"},
		{"write unit test", "写个单测覆盖这个分支", "coding"},
		{"edit go file", "修改 server.go 里的路由", "coding"},

		// session intents
		{"chat", "今天天气怎么样", "session"},
		{"xiaohongshu", "帮我在小红书发一条种草帖", "session"},
		{"post a thread", "帮我在小红书发帖子", "session"},
		{"weibo", "把这段话发到微博", "session"},
		{"moments", "写个朋友圈文案", "session"},
		{"copy writing", "帮我写文案宣传新产品", "session"},
		{"copy publish", "写一段文案发布出去", "session"},
		{"research", "查一下 Go 1.22 的新特性", "session"},
		{"search", "搜索一下附近的美食", "session"},
		{"browse", "浏览一下这个网页总结内容", "session"},
		{"browse page", "帮我浏览页面看看有没有更新", "session"},


		// conflicts: social / browse / research wins session
		{"post about code", "把这个代码片段发到小红书", "session"},
		{"search build fix", "搜索一下怎么修编译错误", "session"},
		{"weibo pr", "PR 合并后发条微博庆祝", "session"},

		// conflicts: repo / code / build-debug wins coding
		{"commit and chat", "改完代码顺便 commit", "coding"},
		{"debug sentence", "这个报错帮我调试下", "coding"},

		// standalone PR must not match inside another word
		{"prompt not pr", "write a prompt for the model", "session"},
		{"press not pr", "press the button twice", "session"},

		// defaults
		{"empty", "", "session"},
		{"unrelated", "你好", "session"},
		{"mixed case", "FIX THE BUG IN MAIN.GO", "coding"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyBotTask(tc.text); got != tc.want {
				t.Fatalf("ClassifyBotTask(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}
