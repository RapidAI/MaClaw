package lansenger

import "testing"

func TestStripBotMentions(t *testing.T) {
	tests := []struct {
		name string
		msg  IncomingMessage
		want string
	}{
		{
			name: "leading @token",
			msg:  IncomingMessage{Text: "@Bot /help"},
			want: "/help",
		},
		{
			name: "mentioned bot name with multibyte",
			msg: IncomingMessage{
				Text: "@测试机器人 帮我查天气",
				MentionedBots: []MentionedBot{
					{ID: "bot-1", Name: "测试机器人"},
				},
			},
			want: "帮我查天气",
		},
		{
			name: "survey after mention",
			msg: IncomingMessage{
				Text: "@Bot /survey A3F9K2",
				MentionedBots: []MentionedBot{
					{ID: "b1", Name: "Bot"},
				},
			},
			want: "/survey A3F9K2",
		},
		{
			name: "multiple leading @tokens",
			msg:  IncomingMessage{Text: "@Bot @other /run demo"},
			want: "/run demo",
		},
		{
			name: "no mention unchanged",
			msg:  IncomingMessage{Text: "  plain text  "},
			want: "plain text",
		},
		{
			name: "empty",
			msg:  IncomingMessage{Text: "  "},
			want: "",
		},
		{
			name: "name without @ when listed in MentionedBots",
			msg: IncomingMessage{
				Text: "M-Wiggins /exec echo hi",
				MentionedBots: []MentionedBot{
					{ID: "bot", Name: "M-Wiggins"},
				},
			},
			want: "/exec echo hi",
		},
		{
			name: "slash command bot postfix",
			msg:  IncomingMessage{Text: "/summary@测试机器人"},
			want: "/summary",
		},
		{
			name: "slash command bot postfix with args",
			msg:  IncomingMessage{Text: "/summary@Bot start"},
			want: "/summary start",
		},
		{
			name: "leading mention then slash postfix",
			msg: IncomingMessage{
				Text: "@Bot /summary@Bot",
				MentionedBots: []MentionedBot{
					{ID: "b1", Name: "Bot"},
				},
			},
			want: "/summary",
		},
		{
			name: "fullwidth slash with postfix",
			msg:  IncomingMessage{Text: "／summary@Bot"},
			want: "／summary",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripBotMentions(tt.msg); got != tt.want {
				t.Fatalf("StripBotMentions() = %q, want %q", got, tt.want)
			}
		})
	}
}
