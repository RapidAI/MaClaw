package guiapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedScheduleAdapter        = "semantic_administer_trusted_schedule"
	semanticTrustedScheduleImplementation = "trusted-schedule-administer-v1"
	semanticTrustedScheduleNameMax        = 200
	semanticTrustedScheduleActionMax      = 2000
	semanticTrustedScheduleTimeout        = 10 * time.Second
)

// semanticTrustedScheduleArgs is the shared admission record. The host
// package aliases the same struct, so a field cannot exist on only one surface.
type semanticTrustedScheduleArgs = scheduler.AdmitFields

// schedule.manage.local is unpublished alongside schedule.administer.local.
// No intent rule maps to it, and the record editing and channel firing it used
// to combine are now separate trusted capabilities. Leaving it published only
// kept the legacy multiplexer's schema — a model-writable user_id, group_id and
// open delivery object — reachable in the managed catalog.
func semanticUnpublishedLegacyScheduleProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		switch provision.Capability {
		case tool.CapabilityScheduleAdministerLocal, tool.CapabilityScheduleManageLocal:
			return true
		}
	}
	return false
}

func semanticTrustedScheduleDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedScheduleAdapter,
			"description": "Read or change one host-local scheduled task. Field presence decides the operation, and this call does not run the task. No fields lists the records. name plus task_action plus a schedule creates one record. status on that create is the record's initial state, not another operation: omit it or send active for a running record, or paused to store it paused. id alone deletes. id plus any other field updates. A schedule is either hour, a calendar clock time, or interval_minutes greater than zero, which repeats every that many minutes. Repeating every hour is interval_minutes 60. When interval_minutes is set, hour is not required.",
			"parameters":  semanticTrustedScheduleInvocationSchema(),
		},
	}
}

func semanticTrustedScheduleInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":        map[string]interface{}{"type": "string"},
			"task_action": map[string]interface{}{"type": "string"},
			"id":          map[string]interface{}{"type": "string"},
			"status": map[string]interface{}{
				"type":        "string",
				"enum":        tool.ScheduleStatusOptions(),
				"description": "Record state, not an operation. On create this is the initial state: omit it or send active for a running record, or paused to store it paused. An existing record changes status only together with its id.",
			},
			"hour":             map[string]interface{}{"type": "integer", "description": "Clock hour 0-23 for a calendar run. Omit when interval_minutes is the schedule."},
			"minute":           map[string]interface{}{"type": "integer"},
			"day_of_week":      map[string]interface{}{"type": "integer"},
			"day_of_month":     map[string]interface{}{"type": "integer"},
			"interval_minutes": map[string]interface{}{"type": "integer", "description": "Repeat every N minutes. Above zero this is the schedule, and hour is not required. Every hour is 60."},
			"start_date":       map[string]interface{}{"type": "string", "description": "Inclusive first day, YYYY-MM-DD."},
			"end_date":         map[string]interface{}{"type": "string", "description": "Inclusive last day, YYYY-MM-DD. The next day is past the window. A paused record keeps a passed end date until it is made active."},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticTrustedScheduleArgsAllowed(args map[string]interface{}) (semanticTrustedScheduleArgs, error) {
	parsed, err := scheduler.ParseAdmit(args)
	if err != nil {
		return parsed, fmt.Errorf("trusted_schedule_arguments_rejected")
	}
	if _, err := screenTrustedSchedule(parsed); err != nil {
		return parsed, err
	}
	return parsed, nil
}

// screenTrustedSchedule rejects an unstorable field before it selects the
// operation. A negative interval, or a date that is not YYYY-MM-DD, is a
// range failure. Zero interval is in range, and with no clock hour it is
// not a schedule.
func screenTrustedSchedule(args semanticTrustedScheduleArgs) (string, error) {
	op, field, ok := scheduler.AdmitScreen(args)
	if field != "" {
		return "", fmt.Errorf("trusted_schedule_%s_rejected", field)
	}
	if !ok {
		return "", fmt.Errorf("trusted_schedule_field_presence_rejected")
	}
	return op, nil
}

// scheduleUtteranceObligesMutation reports that the utterance still asks to
// change a scheduled task after a list. Each clause is its own request. A
// change verb governs only when it is the act in its clause: a list cue in
// front of it, a 的-modifier, or a refusal attached to it does not ask for a
// write. A bare frequency also does, unless that same clause is asking to
// list or a refusal governs the frequency. A list in another clause does not
// cancel it: that list spent the published call, and the frequency is the
// write still owed. Empty text does not.
func scheduleUtteranceObligesMutation(userText string) bool {
	q := strings.ToLower(strings.TrimSpace(acpInnerUserRequest(userText)))
	if q == "" {
		return false
	}
	for _, clause := range scheduleClauses(q) {
		if scheduleClauseObliges(strings.TrimSpace(clause)) {
			return true
		}
	}
	return false
}

func scheduleClauseObliges(clause string) bool {
	if clause == "" {
		return false
	}
	if scheduleClauseChanges(clause) {
		return true
	}
	// A list cue is this clause's act only when it leads the frequency.
	// 每小时查看邮箱 is a hourly check; 查看 is the payload, not a request
	// to list the records. 列出每小时执行的定时任务 is a list.
	freqAt := scheduleClauseFrequencyAt(clause)
	listAt := scheduleClauseListAt(clause)
	if listAt >= 0 && (freqAt < 0 || listAt <= freqAt) {
		return false
	}
	return freqAt >= 0
}

func scheduleClauseChanges(clause string) bool {
	// A Chinese act has to name a schedule record in this clause. 设置 and
	// 添加 are ordinary words; without 定时任务 they are not this tool's write.
	// English verbs stay ungated: "update the daily reminder" names no record.
	named := scheduleClauseNamesRecord(clause)
	for _, hit := range scheduleChangeVerbs(clause) {
		if scheduleVerbIsChinese(hit.verb) && !named {
			continue
		}
		if scheduleListCueBefore(clause, hit.at) || scheduleVerbIsModifier(clause, hit.verb, hit.at) || scheduleVerbRefused(clause[:hit.at]) {
			continue
		}
		return true
	}
	return false
}

func scheduleVerbIsChinese(verb string) bool {
	for i := 0; i < len(verb); i++ {
		if verb[i] >= 0x80 {
			return true
		}
	}
	return false
}

// scheduleClauseNamesRecord is the same schedule-record vocabulary the mail
// payload planner uses. A shorter noun must not be added here or 定时 inside
// 定时任务 would make every schedule sentence look named twice.
func scheduleClauseNamesRecord(clause string) bool {
	for _, noun := range []string{"定时任务", "定時任務", "定时提醒", "定時提醒", "计划任务", "計劃任務", "日程"} {
		if strings.Contains(clause, noun) {
			return true
		}
	}
	return scheduleBoundedPhrase(clause, "scheduled tasks") || scheduleBoundedPhrase(clause, "scheduled task") || scheduleBoundedPhrase(clause, "cron")
}

type scheduleChangeHit struct {
	at   int
	verb string
}

// scheduleChangeMarkers are the acts schedule.administer.local can perform.
// Longer markers are not required to be first: longestScheduleChangeAt keeps
// 删除 from also matching a shorter verb at the same byte. 建立 is the same
// create verb the schedule-payload planner already recognizes. 删掉, 去掉,
// and 移除 are the same delete as 删除; 停掉 and 关掉 are the same pause.
var scheduleChangeMarkers = []string{
	"创建", "新建", "新增", "添加", "设置", "設定", "安排", "建立",
	"修改", "更新",
	"暂停", "暫停", "停掉", "关掉", "關掉", "关闭", "關閉",
	"恢复", "恢復",
	"取消", "删除", "刪除", "删掉", "刪掉", "去掉", "移除",
}

var scheduleChangeEnglish = []string{"create", "add", "update", "pause", "resume", "delete", "cancel", "remove", "stop"}

func scheduleChangeVerbs(clause string) []scheduleChangeHit {
	var hits []scheduleChangeHit
	for i := 0; i < len(clause); {
		verb, n := longestScheduleChangeAt(clause, i)
		if n == 0 {
			i++
			continue
		}
		hits = append(hits, scheduleChangeHit{at: i, verb: verb})
		i += n
	}
	return hits
}

func longestScheduleChangeAt(clause string, i int) (string, int) {
	best := ""
	for _, marker := range scheduleChangeMarkers {
		if strings.HasPrefix(clause[i:], marker) && len(marker) > len(best) {
			best = marker
		}
	}
	for _, word := range scheduleChangeEnglish {
		if strings.HasPrefix(clause[i:], word) && scheduleBoundedAt(clause, i, len(word)) && len(word) > len(best) {
			best = word
		}
	}
	return best, len(best)
}

func scheduleBoundedAt(q string, at, n int) bool {
	if at < 0 || at+n > len(q) {
		return false
	}
	beforeOK := at == 0 || !scheduleASCIIAlphaNum(q[at-1])
	after := at + n
	afterOK := after >= len(q) || !scheduleASCIIAlphaNum(q[after])
	return beforeOK && afterOK
}

func scheduleListCueBefore(clause string, changeAt int) bool {
	if changeAt <= 0 {
		return false
	}
	head := clause[:changeAt]
	for _, cue := range []string{"列出", "查看", "有哪些"} {
		if strings.Contains(head, cue) {
			return true
		}
	}
	return scheduleBoundedPhrase(head, "list")
}

func scheduleVerbIsModifier(clause, verb string, at int) bool {
	rest := strings.TrimLeft(clause[at+len(verb):], " \t")
	return strings.HasPrefix(rest, "的")
}

// scheduleVerbRefused is the closest cue that actually governs the verb.
// Government is an empty gap, 要/想/去, or 把/将/给. 不要的 and 把不要的 do not
// govern: the noun sits between the cue and the verb, so 把不要的定时任务删掉
// is still a delete. 不要把这个定时任务删掉 does govern, because 把 opens the
// gap. A later 但是 that governs cancels an earlier refusal. 别 governs only
// as its own particle, including 别把; 特别, 区别, 别人, and 别的 do not.
func scheduleVerbRefused(prefix string) bool {
	p := strings.TrimSpace(prefix)
	bestAt := -1
	bestNeg := false
	consider := func(at int, neg bool) {
		if at > bestAt {
			bestAt = at
			bestNeg = neg
		}
	}
	for _, cue := range []struct {
		text string
		neg  bool
	}{
		{"但是", false}, {"不過", false}, {"不过", false}, {"然而", false},
		{"不要再", true}, {"别再", true}, {"別再", true}, {"勿", true}, {"不要", true},
	} {
		consider(lastGoverningScheduleCue(p, cue.text), cue.neg)
	}
	for _, word := range []string{"don't", "do not"} {
		consider(lastGoverningScheduleASCII(p, word), true)
	}
	consider(lastScheduleBie(p, "别"), true)
	consider(lastScheduleBie(p, "別"), true)
	return bestNeg
}

func lastGoverningScheduleCue(prefix, cue string) int {
	best := -1
	for start := 0; start < len(prefix); {
		idx := strings.Index(prefix[start:], cue)
		if idx < 0 {
			return best
		}
		at := start + idx
		if scheduleCueGoverns(prefix[at+len(cue):]) {
			best = at
		}
		start = at + len(cue)
	}
	return best
}

func lastGoverningScheduleASCII(q, word string) int {
	best := -1
	for start := 0; start < len(q); {
		idx := strings.Index(q[start:], word)
		if idx < 0 {
			return best
		}
		at := start + idx
		if scheduleBoundedAt(q, at, len(word)) && scheduleCueGoverns(q[at+len(word):]) {
			best = at
		}
		start = at + 1
	}
	return best
}

// scheduleCueGoverns accepts an empty gap, 要/想/去, or 把/将/给 after those.
// A noun in the gap keeps the verb a request.
func scheduleCueGoverns(rest string) bool {
	rest = strings.TrimLeft(rest, " \t")
	for {
		stripped := false
		for _, modal := range []string{"要", "想", "去"} {
			if strings.HasPrefix(rest, modal) {
				rest = strings.TrimLeft(rest[len(modal):], " \t")
				stripped = true
				break
			}
		}
		if !stripped {
			break
		}
		if rest == "" {
			return true
		}
	}
	if rest == "" {
		return true
	}
	for _, particle := range []string{"把", "将", "將", "给", "給"} {
		if strings.HasPrefix(rest, particle) {
			return true
		}
	}
	return false
}

func lastScheduleBie(prefix, phrase string) int {
	best := -1
	for start := 0; start < len(prefix); {
		idx := strings.Index(prefix[start:], phrase)
		if idx < 0 {
			return best
		}
		at := start + idx
		if scheduleBieGoverns(prefix, at, phrase) {
			best = at
		}
		start = at + len(phrase)
	}
	return best
}

func scheduleBieGoverns(prefix string, at int, phrase string) bool {
	if at >= len("特") && prefix[at-len("特"):at] == "特" {
		return false
	}
	if at >= len("区") && prefix[at-len("区"):at] == "区" {
		return false
	}
	if at >= len("區") && prefix[at-len("區"):at] == "區" {
		return false
	}
	rest := strings.TrimLeft(prefix[at+len(phrase):], " \t")
	if rest == "" {
		return true
	}
	for _, particle := range []string{"把", "将", "將", "给", "給"} {
		if strings.HasPrefix(rest, particle) {
			return true
		}
	}
	return false
}

func scheduleClauses(q string) []string {
	parts := strings.FieldsFunc(q, func(r rune) bool {
		switch r {
		case '，', '。', '；', '、', '！', '？', ',', ';', '!', '?', '\n':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		return []string{q}
	}
	return parts
}

func scheduleClauseListAt(clause string) int {
	best := -1
	for _, cue := range []string{"列出", "查看", "有哪些"} {
		if i := strings.Index(clause, cue); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	if i := scheduleBoundedIndex(clause, "list"); i >= 0 && (best < 0 || i < best) {
		best = i
	}
	return best
}

func scheduleClauseFrequencyAt(clause string) int {
	best := -1
	consider := func(at int) {
		if at >= 0 && (best < 0 || at < best) {
			best = at
		}
	}
	for _, cue := range []string{"每小时", "每小時", "每天", "每周", "每週", "每月"} {
		consider(scheduleFrequencyAt(clause, cue))
	}
	consider(scheduleEnglishFrequencyAt(clause, "every hour"))
	consider(scheduleEnglishFrequencyAt(clause, "hourly"))
	consider(scheduleEnglishFrequencyAt(clause, "daily"))
	return best
}

// scheduleFrequencyAt is a schedule instruction. 每天的 modifies the next
// noun, the same way 创建的 is not a create, so it does not by itself ask for
// a write. A refusal that governs the cue does not either.
func scheduleFrequencyAt(clause, cue string) int {
	start := 0
	for start < len(clause) {
		idx := strings.Index(clause[start:], cue)
		if idx < 0 {
			return -1
		}
		at := start + idx
		rest := strings.TrimLeft(clause[at+len(cue):], " \t")
		if !strings.HasPrefix(rest, "的") && !scheduleVerbRefused(clause[:at]) {
			return at
		}
		start = at + len(cue)
	}
	return -1
}

func scheduleEnglishFrequencyAt(clause, phrase string) int {
	start := 0
	for start < len(clause) {
		idx := strings.Index(clause[start:], phrase)
		if idx < 0 {
			return -1
		}
		at := start + idx
		if scheduleBoundedAt(clause, at, len(phrase)) && !scheduleVerbRefused(clause[:at]) {
			return at
		}
		start = at + 1
	}
	return -1
}

func scheduleBoundedIndex(q, phrase string) int {
	if phrase == "" {
		return -1
	}
	start := 0
	for start < len(q) {
		idx := strings.Index(q[start:], phrase)
		if idx < 0 {
			return -1
		}
		at := start + idx
		if scheduleBoundedAt(q, at, len(phrase)) {
			return at
		}
		start = at + 1
	}
	return -1
}

func scheduleBoundedPhrase(q, phrase string) bool {
	return scheduleBoundedIndex(q, phrase) >= 0
}

func scheduleASCIIAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func semanticTrustedScheduleDispatch(a semanticTrustedScheduleArgs) (string, bool) {
	return scheduler.AdmitOperation(a)
}

// semanticTrustedScheduleStatus maps what the model sent to the canonical
// value the record stores. The vocabulary, its aliases, and the empty-is-unset
// reading all live in the shared schedule contract: the GUI surface and the
// agent-service surface both serve schedule.administer.local, and two copies
// of this answer would drift.
func semanticTrustedScheduleStatus(raw string) (string, bool) {
	return tool.ScheduleStatusCanonical(raw)
}

func (h *IMMessageHandler) administerTrustedSchedule(principalID string, args semanticTrustedScheduleArgs) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_schedule_unavailable")
	}
	if strings.TrimSpace(principalID) == "" {
		return "", fmt.Errorf("trusted_schedule_principal_required")
	}
	if h.semanticTrustedSchedule != nil {
		return h.semanticTrustedSchedule(principalID, args)
	}
	op, err := screenTrustedSchedule(args)
	if err != nil {
		return "", err
	}
	if !semanticTrustedTemplateTokenOK(args.Name, semanticTrustedScheduleNameMax) {
		return "", fmt.Errorf("trusted_schedule_name_rejected")
	}
	if !semanticTrustedTemplateTokenOK(args.TaskAction, semanticTrustedScheduleActionMax) {
		return "", fmt.Errorf("trusted_schedule_task_action_rejected")
	}
	status, statusOK := semanticTrustedScheduleStatus(args.Status)
	if !statusOK {
		return "", fmt.Errorf("trusted_schedule_status_rejected (%s)", tool.ScheduleStatusHint())
	}
	manager := h.scheduledTaskManagerForTool()
	if manager == nil {
		return "", fmt.Errorf("trusted_schedule_unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), semanticTrustedScheduleTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	switch op {
	case "create":
		task := scheduler.AdmitTask(args)
		task.Status = status
		id, err := manager.Add(task)
		if err != nil {
			return "", err
		}
		created := manager.Get(id)
		if created == nil {
			return "", fmt.Errorf("trusted_schedule_create_failed")
		}
		if created.Delivery != nil {
			_ = manager.Delete(id)
			return "", fmt.Errorf("trusted_schedule_delivery_rejected")
		}
		h.rememberAdministeredTaskID(id)
		h.emitAppEvent("scheduled-tasks-changed")
		return semanticTrustedScheduleProjection("created", created), nil
	case "update":
		current := manager.Get(args.ID)
		if current == nil {
			return "", fmt.Errorf("trusted_schedule_not_found")
		}
		if err := manager.UpdateState(args.ID, scheduler.AdmitPatch(args), status); err != nil {
			return "", err
		}
		updated := manager.Get(args.ID)
		if updated == nil {
			return "", fmt.Errorf("trusted_schedule_update_failed")
		}
		h.rememberAdministeredTaskID(args.ID)
		h.emitAppEvent("scheduled-tasks-changed")
		return semanticTrustedScheduleProjection("updated", updated), nil
	case "delete":
		if err := manager.Delete(args.ID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				return "", fmt.Errorf("trusted_schedule_not_found")
			}
			return "", err
		}
		h.emitAppEvent("scheduled-tasks-changed")
		return "定时任务已删除。", nil
	default:
		listed := manager.List()
		if len(listed) == 0 {
			return "当前没有定时任务。", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "共 %d 个定时任务:\n", len(listed))
		for _, item := range listed {
			fmt.Fprintf(&b, "- %s\n", semanticTrustedScheduleLine(item))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
}

func semanticTrustedScheduleProjection(kind string, task *scheduler.ScheduledTask) string {
	if task == nil {
		return "当前没有定时任务。"
	}
	line := semanticTrustedScheduleLine(*task)
	switch kind {
	case "created":
		return "定时任务已创建: " + line
	case "updated":
		return "定时任务已更新: " + line
	default:
		return line
	}
}

func semanticTrustedScheduleLine(task scheduler.ScheduledTask) string {
	next := "-"
	if task.NextRunAt != nil {
		next = task.NextRunAt.Format("2006-01-02 15:04")
	}
	return fmt.Sprintf("%s [%s] ID=%s 时间=%s 操作=%s 下次=%s",
		task.Name, task.Status, task.ID, scheduler.FormatSchedule(task), task.Action, next)
}

func semanticTrustedScheduleResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_schedule_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_schedule_empty")
	}
	return text, nil
}
