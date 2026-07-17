package lansenger

import "strings"

// StripBotMentions removes leading @Bot tokens and known MentionedBots name
// prefixes from inbound user text. Use the result for agent/LLM input, slash
// command detection, survey matching, and text-based group reply quotes so
// "@机器人 /help" and "@机器人 帮我查天气" behave like the cleaned forms.
//
// It also strips the group-chat slash postfix form "/cmd@BotName" (and
// "/cmd@BotName args") that Lansenger / OpenClaw clients often emit, so
// command parsers see a bare "/cmd".
//
// msg.Text is left unchanged; callers that need the raw platform payload keep
// using it separately.
func StripBotMentions(msg IncomingMessage) string {
	text := strings.TrimSpace(msg.Text)
	// Prefer Fields so multi-byte names are handled correctly.
	for {
		fields := strings.Fields(text)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "@") {
			break
		}
		text = strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	}
	for _, b := range msg.MentionedBots {
		name := strings.TrimSpace(b.Name)
		if name == "" {
			continue
		}
		for _, p := range []string{"@" + name + " ", "@" + name, name + " ", name} {
			if strings.HasPrefix(text, p) {
				text = strings.TrimSpace(text[len(p):])
			}
		}
	}
	return stripSlashCommandBotPostfix(strings.TrimSpace(text))
}

// stripSlashCommandBotPostfix turns "/summary@BotName" / "/summary@Bot start"
// into "/summary" / "/summary start". Only rewrites the first field when it is a
// slash command with an embedded @ (not a leading @mention).
func stripSlashCommandBotPostfix(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return text
	}
	head := fields[0]
	// Accept ASCII "/" and fullwidth "／" command heads.
	if !strings.HasPrefix(head, "/") && !strings.HasPrefix(head, "／") {
		return text
	}
	at := strings.Index(head, "@")
	if at <= 1 {
		// at==0 would be "@/…" (invalid); at==1 is "/@…" — leave alone.
		// at<0 means no postfix.
		return text
	}
	fields[0] = head[:at]
	return strings.Join(fields, " ")
}
