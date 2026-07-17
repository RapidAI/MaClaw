package lansenger

import "strings"

// StripBotMentions removes leading @Bot tokens and known MentionedBots name
// prefixes from inbound user text. Use the result for agent/LLM input, slash
// command detection, survey matching, and text-based group reply quotes so
// "@机器人 /help" and "@机器人 帮我查天气" behave like the cleaned forms.
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
	return strings.TrimSpace(text)
}
