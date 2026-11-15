package agentservice

import "strings"

// scheduleFiringMustNotRearm stops a timer's own round from registering,
// editing, or cancelling that timer. The model did this after handing over
// the weather PDF (Bot 4, 15:21). A second write moves the next slot or
// replaces the standing order. Listing is still allowed.
func scheduleFiringMustNotRearm(meta map[string]string, args map[string]interface{}) string {
	if strings.TrimSpace(meta["bot_origin"]) != "schedule" {
		return ""
	}
	if scheduleToolReadOnly(args) {
		return ""
	}
	return "这一轮是定时器已经到点的一轮。定时任务已经在跑，不要再登记、修改或取消。把这一轮要求交给这个人的东西做完。"
}

func scheduleToolReadOnly(args map[string]interface{}) bool {
	action := ""
	if args != nil {
		action, _ = args["action"].(string)
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "list", "ls", "show", "all", "list_targets":
		return true
	default:
		return false
	}
}
