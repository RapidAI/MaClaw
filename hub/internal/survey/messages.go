package survey

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

// IM reply event codes carried in IMHandleResponse.Event. The gateway machine
// (desktop) drives its free-text session hints off these instead of matching
// localized reply text.
const (
	// EventResponseSubmitted — a response was stored (SurveyID set).
	EventResponseSubmitted = "response_submitted"
	// EventSessionEnded — terminal state: cancelled, submitted, deadline,
	// stopped, unavailable. Gateway clears the free-text session hint.
	EventSessionEnded = "session_ended"
	// EventSessionActive — user is mid-survey (started / answering / re-prompt).
	// Gateway marks the free-text session hint.
	EventSessionActive = "session_active"
)

// Message keys for IM-facing survey texts (see surveyMessages).
const (
	msgHelp = "help"

	msgCancelDone        = "cancel_done"
	msgNoActiveSurvey    = "no_active_survey"
	msgListP2P           = "list_p2p"
	msgListNoGroup       = "list_no_group"
	msgListEmpty         = "list_empty"
	msgListHeader        = "list_header"
	msgListItem          = "list_item"
	msgListItemMeta      = "list_item_meta"
	msgListFooter        = "list_footer"
	msgSessionExpired    = "session_expired"
	msgStatusSubmitted   = "status_submitted"
	msgStatusProgress    = "status_progress"
	msgNeedCode          = "need_code"
	msgCodeNotFound      = "code_not_found"
	msgCodeInvalid       = "code_invalid"
	msgNotCollecting     = "not_collecting"
	msgDeadlinePassed    = "deadline_passed"
	msgGroupOnly         = "group_only"
	msgNotBoundGroup     = "not_bound_group"
	msgBusyOtherSurvey   = "busy_other_survey"
	msgNoQuestions       = "no_questions"
	msgSubmittedEditable = "submitted_editable"
	msgRequiredNoSkip    = "required_no_skip"
	msgSubmitOK          = "submit_ok"
	msgInvalidAnswer     = "invalid_answer"
	msgStartIntro        = "start_intro"
	msgResumeIntro       = "resume_intro"
	msgAlreadySubmitted  = "already_submitted"
	msgStoppedCollecting = "stopped_collecting"
	msgCancelled         = "cancelled"
	msgSurveyUnavailable = "survey_unavailable"
	msgNoModifyAllowed   = "no_modify_allowed"
	msgSurveyGoneEnded   = "survey_gone_ended"
	msgBusyAnswering     = "busy_answering"

	msgPromptProgress  = "prompt_progress"
	msgPromptOptional  = "prompt_optional"
	msgPromptMulti     = "prompt_multi"
	msgPromptSingle    = "prompt_single"
	msgPromptRating    = "prompt_rating"
	msgPromptText      = "prompt_text"
	msgPromptSkipHint  = "prompt_skip_hint"
	msgPromptTailFirst = "prompt_tail_first"
	msgPromptTailPrev  = "prompt_tail_prev"
	msgMetaDeadline    = "meta_deadline"
	msgMetaTarget      = "meta_target"

	msgErrEmptyAnswer     = "err_empty_answer"
	msgErrOptionRange     = "err_option_range"
	msgErrAmbiguous       = "err_ambiguous"
	msgErrUnknownOption   = "err_unknown_option"
	msgErrRatingInt       = "err_rating_int"
	msgErrRatingRange     = "err_rating_range"
	msgErrRequired        = "err_required"
	msgErrTextTooLong     = "err_text_too_long"
	msgErrUnsupportedType = "err_unsupported_type"
)

// surveyMessages holds IM-facing texts per language. zh is the fallback and
// must stay byte-compatible with the pre-i18n replies (tests assert on it).
var surveyMessages = map[string]map[string]string{
	"zh": {
		msgHelp: `问卷帮助：
· @机器人 /survey <短码> — 开始填写（群须绑定且已发布）
· @机器人 /survey <短码> <答案> — 单题快投
· @机器人 问卷 <短码> / 调查 <短码>
· 答题中：直接回复编号或文本；「上一题」回退；「取消」结束
· 选填题可回复「跳过」
· 已提交且允许修改：回复「修改」重答、「取消」退出
· /survey list — 本群已发布问卷
· /survey status — 当前填写进度
· /survey cancel — 取消进行中的填写
· /survey help — 本说明`,

		msgCancelDone:        "已取消当前问卷填写。",
		msgNoActiveSurvey:    "当前没有进行中的问卷。",
		msgListP2P:           "私聊请直接发送 /survey <短码> 开始填写（需问卷开启私聊）。查看绑定群内问卷请在群里发送 /survey list。",
		msgListNoGroup:       "无法识别群。",
		msgListEmpty:         "本群暂无进行中的问卷。",
		msgListHeader:        "本群问卷：\n",
		msgListItem:          "· %s（%s）\n",
		msgListItemMeta:      "· %s（%s） — %s\n",
		msgListFooter:        "回复 /survey <短码> 开始填写",
		msgSessionExpired:    "会话已失效，请重新开始。",
		msgStatusSubmitted:   "已提交《%s》。回复「修改」可重新作答，或「取消」退出。",
		msgStatusProgress:    "正在填写《%s》，进度 %d/%d。回复「取消」可退出。",
		msgNeedCode:          "请提供问卷短码，例如 /survey A3F9K2",
		msgCodeNotFound:      "未找到该问卷短码。",
		msgCodeInvalid:       "短码无效，请使用 6 位合法短码。",
		msgNotCollecting:     "该问卷未在收集中。",
		msgDeadlinePassed:    "问卷已截止",
		msgGroupOnly:         "该问卷仅支持群内填写。",
		msgNotBoundGroup:     "该问卷未绑定到本群。",
		msgBusyOtherSurvey:   "您正在填写《%s》。回复「取消」结束后再开始新问卷",
		msgNoQuestions:       "问卷配置异常：暂无题目。",
		msgSubmittedEditable: "您已提交。回复「修改」可重新作答，或「取消」退出",
		msgRequiredNoSkip:    "该题为必填，不能跳过。\n",
		msgSubmitOK:          "提交成功，感谢参与！",
		msgInvalidAnswer:     "答案无效：",
		msgStartIntro:        "开始填写《%s》",
		msgResumeIntro:       "继续填写《%s》\n\n%s",
		msgAlreadySubmitted:  "您已提交过该问卷，感谢参与",
		msgStoppedCollecting: "问卷已停止收集。",
		msgCancelled:         "已取消。",
		msgSurveyUnavailable: "问卷不可用。",
		msgNoModifyAllowed:   "该问卷不允许修改答卷。",
		msgSurveyGoneEnded:   "问卷不可用，已结束会话。",
		msgBusyAnswering:     "当前正在填写问卷。发送「取消」结束问卷后再对话。\n",

		msgPromptProgress:  "【%d/%d】%s",
		msgPromptOptional:  "（选填）",
		msgPromptMulti:     "（多选，用空格或逗号分隔序号）\n",
		msgPromptSingle:    "（回复选项序号）\n",
		msgPromptRating:    "请回复 %d–%d 的整数\n",
		msgPromptText:      "请直接输入文字\n",
		msgPromptSkipHint:  "选填可回复「跳过」\n",
		msgPromptTailFirst: "回复「取消」可退出",
		msgPromptTailPrev:  "回复「取消」可退出；「上一题」可返回",
		msgMetaDeadline:    "截止：%s",
		msgMetaTarget:      "目标回收：%d 份",

		msgErrEmptyAnswer:     "空答案",
		msgErrOptionRange:     "选项序号超出范围",
		msgErrAmbiguous:       "选项不明确",
		msgErrUnknownOption:   "未知选项",
		msgErrRatingInt:       "评分必须是整数",
		msgErrRatingRange:     "评分超出范围 [%d,%d]",
		msgErrRequired:        "必填",
		msgErrTextTooLong:     "文本过长",
		msgErrUnsupportedType: "不支持的题型",
	},
	"en": {
		msgHelp: `Survey help:
· @bot /survey <code> — start (group must be bound & published)
· @bot /survey <code> <answer> — quick vote (single question)
· @bot 问卷 <code> / 调查 <code>
· While answering: reply with the option number or text; "prev" goes back; "cancel" exits
· Optional questions: reply "skip"
· Submitted & editable: reply "modify" to redo, "cancel" to exit
· /survey list — published surveys in this group
· /survey status — current progress
· /survey cancel — cancel the current session
· /survey help — this message`,

		msgCancelDone:        "Current survey cancelled.",
		msgNoActiveSurvey:    "No survey in progress.",
		msgListP2P:           "In private chat send /survey <code> to start (the survey must allow private chat). To see group surveys, send /survey list in the group.",
		msgListNoGroup:       "Cannot identify the group.",
		msgListEmpty:         "No active surveys in this group.",
		msgListHeader:        "Surveys in this group:\n",
		msgListItem:          "· %s (%s)\n",
		msgListItemMeta:      "· %s (%s) — %s\n",
		msgListFooter:        "Reply /survey <code> to start",
		msgSessionExpired:    "Session expired, please start again.",
		msgStatusSubmitted:   "You have submitted \"%s\". Reply \"modify\" to redo, or \"cancel\" to exit.",
		msgStatusProgress:    "Filling \"%s\", progress %d/%d. Reply \"cancel\" to exit.",
		msgNeedCode:          "Please provide a survey code, e.g. /survey A3F9K2",
		msgCodeNotFound:      "Survey code not found.",
		msgCodeInvalid:       "Invalid code. Please use a valid 6-character code.",
		msgNotCollecting:     "This survey is not collecting responses.",
		msgDeadlinePassed:    "Survey closed (deadline passed)",
		msgGroupOnly:         "This survey can only be answered in a group.",
		msgNotBoundGroup:     "This survey is not bound to this group.",
		msgBusyOtherSurvey:   "You are filling \"%s\". Reply \"cancel\" to finish it before starting a new survey.",
		msgNoQuestions:       "Survey misconfigured: no questions.",
		msgSubmittedEditable: "Already submitted. Reply \"modify\" to redo, or \"cancel\" to exit.",
		msgRequiredNoSkip:    "This question is required and cannot be skipped.\n",
		msgSubmitOK:          "Submitted successfully. Thank you!",
		msgInvalidAnswer:     "Invalid answer: ",
		msgStartIntro:        "Starting \"%s\"",
		msgResumeIntro:       "Resuming \"%s\"\n\n%s",
		msgAlreadySubmitted:  "You have already submitted this survey. Thank you!",
		msgStoppedCollecting: "This survey has stopped collecting.",
		msgCancelled:         "Cancelled.",
		msgSurveyUnavailable: "Survey unavailable.",
		msgNoModifyAllowed:   "This survey does not allow modifying responses.",
		msgSurveyGoneEnded:   "Survey unavailable; session ended.",
		msgBusyAnswering:     "You are currently filling a survey. Send \"cancel\" to finish it before chatting.\n",

		msgPromptProgress:  "[%d/%d] %s",
		msgPromptOptional:  "(optional)",
		msgPromptMulti:     "(multi-choice; separate numbers with space or comma)\n",
		msgPromptSingle:    "(reply with the option number)\n",
		msgPromptRating:    "Reply with an integer between %d and %d\n",
		msgPromptText:      "Type your answer directly\n",
		msgPromptSkipHint:  "Optional: reply \"skip\" to skip\n",
		msgPromptTailFirst: "Reply \"cancel\" to exit",
		msgPromptTailPrev:  "Reply \"cancel\" to exit; \"prev\" for the previous question",
		msgMetaDeadline:    "Deadline: %s",
		msgMetaTarget:      "Target: %d responses",

		msgErrEmptyAnswer:     "empty answer",
		msgErrOptionRange:     "option index out of range",
		msgErrAmbiguous:       "ambiguous option",
		msgErrUnknownOption:   "unknown option",
		msgErrRatingInt:       "rating must be an integer",
		msgErrRatingRange:     "rating out of range [%d,%d]",
		msgErrRequired:        "required",
		msgErrTextTooLong:     "text too long",
		msgErrUnsupportedType: "unsupported type",
	},
}

// tr returns the localized IM text for key. lang is normalized via
// corelib/i18n (zh-Hans→zh, en-*→en); missing keys fall back to zh.
func tr(lang, key string, args ...any) string {
	lang = i18n.NormalizeLang(lang)
	table, ok := surveyMessages[lang]
	if !ok {
		table = surveyMessages["zh"]
	}
	s, ok := table[key]
	if !ok {
		s = surveyMessages["zh"][key]
	}
	if s == "" {
		return key
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}
