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
	body := fileCompanionChatMarkdown(turns)
	if strings.TrimSpace(body) == "" {
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
		Subtitle:       fmt.Sprintf("伴读对话记录 · %d 条消息", len(fileCompanionExportableTurns(turns))),
		Brand:          fileCompanionExportBrand,
		Content:        body,
		FooterHint:     fileCompanionExportFooter,
		FileNamePrefix: "companion-chat",
		PaperSize:      "A4",
	}, dest)
	if err != nil {
		return "", err
	}
	logFileCompanion("export chat pdf turns=%d", len(turns))
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

// fileCompanionChatMarkdown renders the transcript as Markdown with each role
// heading its own section. Headings rather than bold lines, because the layout
// pauses before a heading: a long answer never runs head-first into the next
// question.
//
// Turns whose content is empty are skipped — a placeholder reply for a paper
// still in flight is not worth a page.
func fileCompanionChatMarkdown(turns []CompanionChatTurn) string {
	kept := fileCompanionExportableTurns(turns)
	if len(kept) == 0 {
		return ""
	}
	var body strings.Builder
	for i, turn := range kept {
		if i > 0 {
			body.WriteString("\n\n")
		}
		body.WriteString(fmt.Sprintf("## %s\n\n%s\n", fileCompanionChatRoleLabel(turn.Role, i+1), strings.TrimSpace(turn.Content)))
	}
	return body.String()
}

// fileCompanionChatRoleLabel names one turn. Numbering keeps the reader's place
// in a long session; asking "第 3 个问题" is far easier than counting headings.
func fileCompanionChatRoleLabel(role string, ordinal int) string {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "user":
		return fmt.Sprintf("第 %d 个问题", ordinal)
	default:
		return fmt.Sprintf("第 %d 个回答", ordinal)
	}
}
