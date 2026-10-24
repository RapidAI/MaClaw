package agent

import "regexp"

// PromptWritingRules is the general standard for any long-form text the bot
// writes (articles, reviews, comments, surveys, interpretations, posts,
// reports), wherever it ends up. Publishing only has to keep this structure.
const PromptWritingRules = `
## 长文写作格式（任何文章、评测、评论、综述、解读、帖子、报告都适用）
- 一个清楚的标题；正文按小节分段，每节有小标题。
- 关键观点和结论用**加粗**；并列内容用列表。
- 图片紧挨着它支撑的那段文字，每张配一句图注（图N：…）。
- 写出具体数字，并注明来源或链接。
- 结尾给一段简短总结或要点。
`

var promptWritingTaskRe = regexp.MustCompile(`(?i)写(一篇|篇|一份|份|个)?|文章|帖子|笔记|解读|评测|评论|综述|报告|总结|稿|发(到|在)?(知乎|小红书|微博|公众号|博客)|article|essay|blog|post|review|report|survey|write[- ]?up`)

// promptIsWritingTask reports a turn that asks for long-form text.
func promptIsWritingTask(msg string) bool { return promptWritingTaskRe.MatchString(msg) }
