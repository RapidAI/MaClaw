package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tinytex"
)

func TestLatexRepairReadsTheBlockTheDisplayFilterWouldDrop(t *testing.T) {
	source := "\\documentclass{article}\n\\begin{document}\nVaswani et al.\\~\\cite{vaswani2017}\n\\end{document}\n"
	excerpt := "./main.tex:3: Missing \\endcsname inserted.\nl.3 Vaswani et al.\\~\\cite\n"
	wrapped := "<think>\n@@@ 3 3\nVaswani et al.~\\cite{vaswani2017}\n@@@\n</think>"
	resp := finishSimpleLLMText(wrapped, "", "stop")
	if strings.TrimSpace(resp.Content) != "" {
		t.Fatalf("display filter kept %q", resp.Content)
	}
	body, ok := firstLatexRepair(source, excerpt, resp)
	if !ok || strings.Contains(body, `\~\cite`) || !strings.Contains(body, `~\cite{vaswani2017}`) {
		t.Fatalf("think-wrapped block was dropped: ok=%v body=%s", ok, body)
	}

	prose := finishSimpleLLMText("我先看第 3 行。", "@@@ 3 3\nVaswani et al.~\\cite{vaswani2017}\n@@@", "stop")
	body, ok = firstLatexRepair(source, excerpt, prose)
	if !ok || strings.Contains(body, `\~\cite`) {
		t.Fatalf("reasoning field was ignored: ok=%v body=%s", ok, body)
	}
}

func firstLatexRepair(source, excerpt string, resp *llmSimpleResponse) (string, bool) {
	for _, candidate := range latexRepairCandidateTexts(resp) {
		if body, ok := tinytex.ApplyRepairReply(source, excerpt, candidate); ok {
			return body, true
		}
	}
	return "", false
}
