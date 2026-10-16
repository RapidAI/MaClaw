package guiapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
)

// Exporting the whole conversation, not one reply.
//
// The per-message control already exports a single answer. This one writes the
// entire transcript so a reader can hand a session to someone else, or keep it
// beside the document it was about. Questions stay in the record because an
// answer alone usually will not stand on its own: "yes, change it" is only
// meaningful next to whatever was asked.

// CompanionChatTurn is one message in the conversation being exported.
type CompanionChatTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// FileCompanionExportChatPDF writes a whole companion conversation to a PDF at
// the path the reader picks. An empty first result means the dialog was
// cancelled, which is not an error.
func (a *App) FileCompanionExportChatPDF(path string, turns []CompanionChatTurn) (string, error) {
	if a == nil {
		return "", errors.New("应用不可用")
	}
	grant, _, err := a.fileCompanionGrant(path)
	if err != nil || grant == nil {
		return "", errFileCompanionNotGranted
	}
	body, turnCount := fileCompanionChatDocument(turns)
	if turnCount == 0 {
		return "", errors.New("没有可导出的内容")
	}
	source := strings.TrimSpace(grant.Canonical)
	dest, err := fileCompanionSavePDFDialog(a, fileCompanionChatFileName(source))
	if err != nil {
		return "", err
	}
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", nil
	}
	if !strings.EqualFold(filepath.Ext(dest), ".pdf") {
		dest += ".pdf"
	}
	if abs, absErr := filepath.Abs(dest); absErr == nil {
		dest = abs
	}
	if info, statErr := os.Stat(dest); statErr == nil && info.IsDir() {
		return "", errors.New("无法保存到该位置")
	}
	if info, statErr := os.Stat(source); statErr == nil && !info.IsDir() && sameFileCompanionExportPath(source, dest) {
		return "", errors.New("不能覆盖当前文件")
	}
	written, err := docgen.GenerateToFile(docgen.Spec{
		Title:          fileCompanionPaperSourceName(source),
		Subtitle:       fmt.Sprintf("伴读对话记录 · %d 条消息", turnCount),
		Brand:          fileCompanionExportBrand,
		Content:        body,
		FooterHint:     fileCompanionExportFooter,
		FileNamePrefix: "companion-chat",
		PaperSize:      "A4",
	}, dest)
	if err != nil {
		return "", err
	}
	logFileCompanion("export chat pdf turns=%d", turnCount)
	return written, nil
}

// fileCompanionChatFileName is the default save-dialog name for a transcript.
const fileCompanionChatNameSuffix = "-伴读对话.pdf"

func fileCompanionChatFileName(source string) string {
	base := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, fileCompanionPaperSourceName(source))
	base = strings.Trim(base, " .")
	rs := []rune(base)
	if len(rs) > 60 {
		base = strings.TrimRight(string(rs[:60]), " .")
	}
	if strings.TrimSpace(base) == "" {
		base = "document"
	}
	return base + fileCompanionChatNameSuffix
}

// fileCompanionExportableTurns counts messages that carry text. Role marks
// matter here: the reader asked every one of these, and dropping them changes
// the meaning of what is left.
func fileCompanionExportableTurns(turns []CompanionChatTurn) []CompanionChatTurn {
	kept := make([]CompanionChatTurn, 0, len(turns))
	for _, turn := range turns {
		if strings.TrimSpace(turn.Content) == "" {
			continue
		}
		kept = append(kept, turn)
	}
	return kept
}

// fileCompanionChatDocument renders the transcript and reports how many turns
// it kept, so the subtitle, the log and the body cannot disagree.
func fileCompanionChatDocument(turns []CompanionChatTurn) (string, int) {
	kept := fileCompanionExportableTurns(turns)
	if len(kept) == 0 {
		return "", 0
	}
	var body strings.Builder
	questions, answers := 0, 0
	for i, turn := range kept {
		if i > 0 {
			body.WriteString("\n\n")
		}
		var label string
		if fileCompanionChatIsQuestion(turn.Role) {
			questions++
			label = fileCompanionChatRoleLabel(turn.Role, questions)
		} else {
			answers++
			label = fileCompanionChatRoleLabel(turn.Role, answers)
		}
		body.WriteString(fmt.Sprintf("## %s\n\n%s\n", label, fileCompanionNestTurnContent(strings.TrimSpace(turn.Content))))
	}
	return body.String(), len(kept)
}

// fileCompanionNestTurnContent pushes a turn's own headings one level down so
// they sit inside the turn rather than beside it.
//
// An answer that writes "## 方法原理" would otherwise render at the same size
// as "## 第 1 个回答", and the reader cannot tell which headings belong to the
// answer and which to the transcript.
//
// Fenced code is left alone — a "#" in there is content.
//
// Only levels two through four move. A level-one heading is already bigger
// than the turn heading, so demoting it would *create* the collision this
// avoids; five and six have nowhere to go that the renderer understands. What
// remains is the case that actually happens: an answer that writes its own
// sections with "##".
func fileCompanionNestTurnContent(content string) string {
	lines := strings.Split(content, "\n")
	fence := ""
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if marker, rest, ok := fileCompanionSplitFence(trimmed); ok {
			switch {
			case fence == "":
				fence = marker
			case marker[0] == fence[0] && len(marker) >= len(fence) && strings.TrimSpace(rest) == "":
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		hashes := 0
		for hashes < len(trimmed) && trimmed[hashes] == '#' {
			hashes++
		}
		// A heading needs a space after its hashes.
		if hashes < 2 || hashes > fileCompanionNestableHeading || hashes >= len(trimmed) || trimmed[hashes] != ' ' {
			continue
		}
		lines[i] = "#" + line
	}
	return strings.Join(lines, "\n")
}

// fileCompanionNestableHeading is the deepest level that can move down without
// landing past what the PDF renderer understands.
const fileCompanionNestableHeading = 4

// fileCompanionSplitFence reports the fence run that starts a line, if any,
// along with whatever follows it on that line.
func fileCompanionSplitFence(trimmed string) (marker, rest string, ok bool) {
	var ch byte
	switch {
	case strings.HasPrefix(trimmed, "```"):
		ch = '`'
	case strings.HasPrefix(trimmed, "~~~"):
		ch = '~'
	default:
		return "", "", false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return "", "", false
	}
	return trimmed[:n], trimmed[n:], true
}

// fileCompanionChatIsQuestion reports whether a turn is the reader's.
func fileCompanionChatIsQuestion(role string) bool {
	return strings.TrimSpace(strings.ToLower(role)) == "user"
}

// fileCompanionChatRoleLabel names one turn.
//
// Questions and answers are counted apart, so the answer to 第 1 个问题 is
// 第 1 个回答. One running number over both would label that answer
// 第 2 个回答 and leave the reader matching headings to nothing.
func fileCompanionChatRoleLabel(role string, ordinal int) string {
	if fileCompanionChatIsQuestion(role) {
		return fmt.Sprintf("第 %d 个问题", ordinal)
	}
	return fmt.Sprintf("第 %d 个回答", ordinal)
}
