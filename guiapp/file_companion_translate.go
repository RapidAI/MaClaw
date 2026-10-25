package guiapp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const (
	// fileCompanionTranslateRunes is the longest selection one translation
	// sends. The highlight store keeps its own shorter copy.
	fileCompanionTranslateRunes = 4000
	// fileCompanionTranslateReplyRunes bounds the Chinese text kept for the popover.
	fileCompanionTranslateReplyRunes = 12000
	fileCompanionTranslateBudget     = 90 * time.Second
)

const fileCompanionTranslateInstruction = "把下面的文字翻译成中文。只输出译文。原文已经是中文时原样输出。"

// TranslateFileCompanionSelection translates one selected string into Chinese.
// The model receives that string and the translate instruction. The open file,
// its path, and the document chat are not part of the request.
func (a *App) TranslateFileCompanionSelection(path, text string) (string, error) {
	if a == nil {
		return "", errors.New("app unavailable")
	}
	if !a.isFileCompanionProcess() {
		return "", errors.New("file companion is not running")
	}
	path = strings.TrimSpace(path)
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("请先选中要翻译的文字")
	}
	if path == "" {
		return "", errors.New("path is required")
	}
	text = fileCompanionClipRunes(text, fileCompanionTranslateRunes)
	_, canonical, err := a.fileCompanionGrant(path)
	if err != nil {
		return "", err
	}
	// This owner is not the document chat. A translate call must not open
	// that session file or append a turn to it.
	msg := IMUserMessage{
		UserID:   "file-companion:translate:" + fileCompanionSessionID(canonical),
		Platform: desktopLaunchFileCompanion,
		Text:     text,
		AssistantBinding: &agent.AssistantBinding{
			Mode:          "file-companion",
			InitialPrompt: fileCompanionTranslateInstruction,
		},
		NoWorkflowInterception: true,
	}
	var reply string
	if fileCompanionTurnModelHook != nil {
		resp := fileCompanionTurnModelHook(msg)
		if resp == nil {
			return "", errors.New("没有得到译文")
		}
		if strings.TrimSpace(resp.Error) != "" {
			return "", errors.New(strings.TrimSpace(resp.Error))
		}
		reply = resp.Text
	} else {
		reply, err = a.fileCompanionTranslateOnce(text)
		if err != nil {
			return "", err
		}
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return "", errors.New("没有得到译文")
	}
	return fileCompanionClipRunes(reply, fileCompanionTranslateReplyRunes), nil
}

// fileCompanionTranslateOnce asks the configured model with an empty history.
// The user message is the selection. The system message is the instruction.
func (a *App) fileCompanionTranslateOnce(text string) (string, error) {
	ctx := context.Background()
	if a != nil && a.ctx != nil {
		ctx = a.ctx
	}
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": fileCompanionTranslateInstruction},
		map[string]interface{}{"role": "user", "content": text},
	}
	resp, err := doSimpleLLMRequestWithOptions(ctx, a.GetMaclawLLMConfig(), messages, nil, fileCompanionTranslateBudget, simpleLLMRequestOptions{AwaitResponse: true})
	if err != nil {
		return "", fileCompanionTranslateFailure(err)
	}
	if resp == nil {
		return "", errors.New("翻译失败，请稍后重试")
	}
	return resp.Content, nil
}

// fileCompanionTranslateFailure keeps a login or credit denial. Transport
// and provider bodies stay the short retry sentence.
func fileCompanionTranslateFailure(err error) error {
	if err == nil {
		return errors.New("翻译失败，请稍后重试")
	}
	msg := strings.TrimSpace(err.Error())
	if strings.Contains(msg, "MaClaw Hub login is required") {
		return errors.New("需要先在主窗口登录")
	}
	if strings.Contains(msg, "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST") || strings.Contains(strings.ToLower(msg), "insufficient credits") {
		return errors.New("本次请求额度不足")
	}
	return errors.New("翻译失败，请稍后重试")
}

func fileCompanionClipRunes(text string, limit int) string {
	if limit <= 0 || text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
