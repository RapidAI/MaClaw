package main

import "strings"

type browserToolAction string

const (
	browserToolActionUnknown      browserToolAction = ""
	browserToolActionSessionStart browserToolAction = "session_start"
	browserToolActionSessionStop  browserToolAction = "session_stop"
	browserToolActionObserve      browserToolAction = "observe"
	browserToolActionNavigate     browserToolAction = "navigate"
	browserToolActionClick        browserToolAction = "click"
	browserToolActionType         browserToolAction = "type"
	browserToolActionWait         browserToolAction = "wait"
	browserToolActionRefresh      browserToolAction = "refresh"
	browserToolActionBack         browserToolAction = "back"
	browserToolActionExtract      browserToolAction = "extract"
	browserToolActionConnect      browserToolAction = "connect"
	browserToolActionScreenshot   browserToolAction = "screenshot"
	browserToolActionGetText      browserToolAction = "get_text"
	browserToolActionGetHTML      browserToolAction = "get_html"
	browserToolActionEval         browserToolAction = "eval"
	browserToolActionScroll       browserToolAction = "scroll"
	browserToolActionSelect       browserToolAction = "select"
	browserToolActionListPages    browserToolAction = "list_pages"
	browserToolActionSwitchPage   browserToolAction = "switch_page"
	browserToolActionClose        browserToolAction = "close"
	browserToolActionClickAt      browserToolAction = "click_at"
	browserToolActionSetFiles     browserToolAction = "set_files"
	browserToolActionInfo         browserToolAction = "info"
	browserToolActionOCR          browserToolAction = "ocr"
	browserToolActionTaskRun      browserToolAction = "task_run"
	browserToolActionTaskStatus   browserToolAction = "task_status"
	browserToolActionTaskVerify   browserToolAction = "task_verify"
	browserToolActionTaskReplay   browserToolAction = "task_replay"
	browserToolActionRecordStart  browserToolAction = "record_start"
	browserToolActionRecordStop   browserToolAction = "record_stop"
	browserToolActionListFlows    browserToolAction = "list_flows"
)

func normalizeBrowserToolAction(action string) browserToolAction {
	switch browserToolAction(strings.ToLower(strings.TrimSpace(action))) {
	case browserToolActionSessionStart:
		return browserToolActionSessionStart
	case browserToolActionSessionStop:
		return browserToolActionSessionStop
	case browserToolActionObserve:
		return browserToolActionObserve
	case browserToolActionNavigate:
		return browserToolActionNavigate
	case browserToolActionClick:
		return browserToolActionClick
	case browserToolActionType:
		return browserToolActionType
	case browserToolActionWait:
		return browserToolActionWait
	case browserToolActionRefresh:
		return browserToolActionRefresh
	case browserToolActionBack:
		return browserToolActionBack
	case browserToolActionExtract:
		return browserToolActionExtract
	case browserToolActionConnect:
		return browserToolActionConnect
	case browserToolActionScreenshot:
		return browserToolActionScreenshot
	case browserToolActionGetText:
		return browserToolActionGetText
	case browserToolActionGetHTML:
		return browserToolActionGetHTML
	case browserToolActionEval:
		return browserToolActionEval
	case browserToolActionScroll:
		return browserToolActionScroll
	case browserToolActionSelect:
		return browserToolActionSelect
	case browserToolActionListPages:
		return browserToolActionListPages
	case browserToolActionSwitchPage:
		return browserToolActionSwitchPage
	case browserToolActionClose:
		return browserToolActionClose
	case browserToolActionClickAt:
		return browserToolActionClickAt
	case browserToolActionSetFiles:
		return browserToolActionSetFiles
	case browserToolActionInfo:
		return browserToolActionInfo
	case browserToolActionOCR:
		return browserToolActionOCR
	case browserToolActionTaskRun:
		return browserToolActionTaskRun
	case browserToolActionTaskStatus:
		return browserToolActionTaskStatus
	case browserToolActionTaskVerify:
		return browserToolActionTaskVerify
	case browserToolActionTaskReplay:
		return browserToolActionTaskReplay
	case browserToolActionRecordStart:
		return browserToolActionRecordStart
	case browserToolActionRecordStop:
		return browserToolActionRecordStop
	case browserToolActionListFlows:
		return browserToolActionListFlows
	default:
		return browserToolAction(strings.TrimSpace(action))
	}
}

func (a browserToolAction) ToolName() string {
	if a == browserToolActionUnknown {
		return ""
	}
	return "browser_" + string(a)
}

func (a browserToolAction) ShouldSyncSessions() bool {
	switch a {
	case browserToolActionSessionStart, browserToolActionSessionStop, browserToolActionConnect, browserToolActionClose:
		return true
	default:
		return false
	}
}

func browserSupportedActionNames() []string {
	return []string{
		string(browserToolActionSessionStart),
		string(browserToolActionSessionStop),
		string(browserToolActionObserve),
		string(browserToolActionNavigate),
		string(browserToolActionClick),
		string(browserToolActionType),
		string(browserToolActionWait),
		string(browserToolActionRefresh),
		string(browserToolActionBack),
		string(browserToolActionExtract),
		string(browserToolActionConnect),
		string(browserToolActionScreenshot),
		string(browserToolActionGetText),
		string(browserToolActionGetHTML),
		string(browserToolActionEval),
		string(browserToolActionScroll),
		string(browserToolActionSelect),
		string(browserToolActionListPages),
		string(browserToolActionSwitchPage),
		string(browserToolActionClose),
		string(browserToolActionClickAt),
		string(browserToolActionSetFiles),
		string(browserToolActionInfo),
		string(browserToolActionOCR),
		string(browserToolActionTaskRun),
		string(browserToolActionTaskStatus),
		string(browserToolActionTaskVerify),
		string(browserToolActionTaskReplay),
		string(browserToolActionRecordStart),
		string(browserToolActionRecordStop),
		string(browserToolActionListFlows),
	}
}
