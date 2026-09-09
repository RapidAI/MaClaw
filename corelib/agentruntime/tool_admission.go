package agentruntime

// This file holds transport-neutral tool admission policy: decisions about
// whether a model-emitted tool call may proceed must be identical for GUI and
// headless hosts, so the vocabulary and gate logic live here rather than in
// one host package.

import "strings"

// ManageScheduleAction is the shared action vocabulary of the manage_schedule
// tool. GUI and headless hosts must agree on these values (including legacy
// aliases) so admission gates and dispatch never diverge.
type ManageScheduleAction string

const (
	ManageScheduleActionUnknown ManageScheduleAction = ""
	ManageScheduleActionCreate  ManageScheduleAction = "create"
	ManageScheduleActionList    ManageScheduleAction = "list"
	ManageScheduleActionRun     ManageScheduleAction = "run"
	ManageScheduleActionPause   ManageScheduleAction = "pause"
	ManageScheduleActionResume  ManageScheduleAction = "resume"
	ManageScheduleActionDelete  ManageScheduleAction = "delete"
	ManageScheduleActionUpdate  ManageScheduleAction = "update"
	// ManageScheduleActionListTargets: generic IM delivery destinations
	// (channel=lansenger|…, query optional)
	ManageScheduleActionListTargets ManageScheduleAction = "list_targets"
)

// NormalizeManageScheduleAction canonicalizes a model-emitted manage_schedule
// action, accepting the historical aliases older agents produced.
func NormalizeManageScheduleAction(action string) ManageScheduleAction {
	switch ManageScheduleAction(strings.ToLower(strings.TrimSpace(action))) {
	case ManageScheduleActionCreate:
		return ManageScheduleActionCreate
	case ManageScheduleActionList:
		return ManageScheduleActionList
	case ManageScheduleActionRun, "execute", "trigger", "trigger_now":
		return ManageScheduleActionRun
	case ManageScheduleActionPause, "stop", "disable":
		return ManageScheduleActionPause
	case ManageScheduleActionResume, "enable":
		return ManageScheduleActionResume
	case ManageScheduleActionDelete:
		return ManageScheduleActionDelete
	case ManageScheduleActionUpdate:
		return ManageScheduleActionUpdate
	case ManageScheduleActionListTargets, "list_delivery_targets", "list_groups", "list_im_targets":
		// Aliases keep older/agent-phrased names working without new tools.
		return ManageScheduleActionListTargets
	default:
		return ManageScheduleActionUnknown
	}
}

// manageScheduleCreateCues is the frozen shared scheduling-cue vocabulary:
// an explicit cue in the originating user request is what legitimizes turning
// work into a persistent schedule.
var manageScheduleCreateCues = []string{
	"定时", "计划任务", "日程", "提醒", "cron", "schedule", "timer",
}

// ManageScheduleCreateBlockReason keeps a model from turning ordinary work
// (for example, a document conversion) into a recurring background task.
// Scheduling changes state persistently, so require an explicit scheduling cue
// in the originating user request rather than relying only on the tool call.
func ManageScheduleCreateBlockReason(name string, args map[string]any, userText string) string {
	if strings.TrimSpace(name) != "manage_schedule" {
		return ""
	}
	action := NormalizeManageScheduleAction(stringMapValue(args, "action"))
	if action != ManageScheduleActionCreate {
		// Some models reverse action and task_action. toolManageSchedule supports
		// that legacy form, so the execution gate must recognize it too.
		action = NormalizeManageScheduleAction(stringMapValue(args, "task_action"))
	}
	if action != ManageScheduleActionCreate {
		return ""
	}

	request := strings.ToLower(strings.TrimSpace(userText))
	for _, cue := range manageScheduleCreateCues {
		if strings.Contains(request, cue) {
			return ""
		}
	}
	return "创建定时任务需要用户在当前请求中明确提出定时、计划或提醒需求；不要把普通文档转换或一次性工作创建为定时任务。"
}

// StringMapValue extracts a string value from a map, returning "" if the key
// is missing or not a string.
func StringMapValue(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func stringMapValue(m map[string]any, key string) string {
	return StringMapValue(m, key)
}
