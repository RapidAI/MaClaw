package main

import (
	"regexp"
	"strings"
)

// A reply must not say files are attached unless an attach succeeded
// (papers 11-12 01:12: Bot 1's watcher turn could attach nothing; 11-11
// 22:12: the chat showed 「两个文件都附在这条消息里」 with no files). The check
// is the delivery result, not the model's word.
var desktopAttachClaimRe = regexp.MustCompile(`(?i)(附在(这条|本条|此)?(消息|回复)|已附上|附上了|随(这条)?(消息|回复)附|附件(里|中|已|在)|见附件|attached (to|in|with) (this|the) (message|reply)|see (the )?attach)`)

const desktopAttachCorrection = "（更正：这条消息实际没有带附件，文件留在电脑上，路径见上文。）"

func correctDesktopAttachClaim(text string, attached int) string {
	if attached > 0 || !desktopAttachClaimRe.MatchString(text) || strings.Contains(text, desktopAttachCorrection) {
		return text
	}
	return strings.TrimRight(text, "\n ") + "\n" + desktopAttachCorrection
}
