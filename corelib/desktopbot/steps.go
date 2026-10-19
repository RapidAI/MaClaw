package desktopbot

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrJournalOnVM means the step journal path is one of the computer's private
// volumes. The journal is server state. It is not part of a virtual-machine
// snapshot and it is not written under /desktops, /home/desktop, /opt, or
// /usr/local.
var ErrJournalOnVM = errors.New("desktop step journal is server state and is not stored on a virtual machine volume")

// Step is one completed side effect. The fields are the shape of one future
// Temporal step result: step number, tool, idempotency key, result, and
// whether it is done. This commit does not install Temporal.
type Step struct {
	RunID  string
	Number int
	Tool   string
	Key    string
	Result string
	Done   bool
}

// ResumePlan is where a crashed round continues.
// Sentence is always empty: resuming does not call send_to_user.
// Display is the person-facing next step. It loses when it conflicts with a
// completed side effect, which is listed in Blocked.
type ResumePlan struct {
	Start    int
	Sentence string
	Display  string
	Blocked  []string
}

// StepKey is the idempotency key. An existing tool call id is the key.
// Otherwise the key is run_id plus a step number. An empty key is not stored.
func StepKey(runID, toolCallID string, number int) string {
	if id := strings.TrimSpace(toolCallID); id != "" {
		return id
	}
	runID = strings.TrimSpace(runID)
	if runID != "" && number > 0 {
		return runID + ":" + strconv.Itoa(number)
	}
	return ""
}

// SideEffect reports whether a retry of this tool must return the recorded
// result instead of running again. Screenshot, file_list, probe, click, type,
// and an ordinary shell command may run again. Send, payment, and deploy may not.
func SideEffect(tool, command string) bool {
	switch EffectName(tool, command) {
	case "send_to_user", "email", "delivery", "install", "charge", "git push", "docker compose up":
		return true
	default:
		return false
	}
}

// EffectName is the tool stored on the step row.
func EffectName(tool, command string) string {
	folded := strings.ToLower(strings.TrimSpace(tool))
	switch folded {
	case "send_to_user":
		return "send_to_user"
	case "email", "mailbox", "mailbox_send", "mailbox_delete":
		return "email"
	case "delivery":
		return "delivery"
	case "install":
		return "install"
	case "charge", "payment", "pay", "扣款":
		return "charge"
	case "git push":
		return "git push"
	case "docker compose up", "docker-compose up":
		return "docker compose up"
	case "bash":
		if hasGitPush(command) {
			return "git push"
		}
		if hasComposeUp(command) {
			return "docker compose up"
		}
		return "bash"
	default:
		if hasGitPush(tool) || hasGitPush(command) {
			return "git push"
		}
		if hasComposeUp(tool) || hasComposeUp(command) {
			return "docker compose up"
		}
		return folded
	}
}

// ResumeFrom is the step after the last completed one. No completed step
// starts at 1. A failed step is not completed, so it can be tried again.
func ResumeFrom(steps []Step) int {
	maxDone := 0
	for _, step := range steps {
		if step.Done && step.Number > maxDone {
			maxDone = step.Number
		}
	}
	if maxDone == 0 {
		return 1
	}
	return maxDone + 1
}

// ResumeExecution starts after the last completed step. The progress file
// still supplies the person-facing line. A line that would repeat a completed
// send or deploy is blocked, and the plan never carries a sentence to send.
func ResumeExecution(fileText string, copy Sections, steps []Step) ResumePlan {
	plan := ResumePlan{
		Start:    ResumeFrom(steps),
		Sentence: "",
		Display:  ResumeFromNextStep(fileText, copy),
	}
	progress := fileText + "\n" + plan.Display
	seen := map[string]struct{}{}
	for _, step := range steps {
		if !step.Done || strings.TrimSpace(step.Key) == "" || !SideEffect(step.Tool, "") {
			continue
		}
		if !ProgressRepeatsDone(progress, step.Tool) {
			continue
		}
		if _, ok := seen[step.Key]; ok {
			continue
		}
		seen[step.Key] = struct{}{}
		plan.Blocked = append(plan.Blocked, step.Key)
	}
	plan.Sentence = ""
	return plan
}

// ProgressRepeatsDone reports that the progress line would do a side effect
// that the step record already completed. The step record wins.
func ProgressRepeatsDone(progress, tool string) bool {
	folded := strings.ToLower(progress)
	switch EffectName(tool, "") {
	case "email":
		return strings.Contains(folded, "email") || strings.Contains(folded, "mailbox") || strings.Contains(progress, "邮件")
	case "git push":
		return strings.Contains(folded, "git push") || strings.Contains(progress, "推送")
	case "docker compose up":
		return strings.Contains(folded, "compose") || strings.Contains(progress, "部署")
	case "install":
		return strings.Contains(progress, "安装") || strings.Contains(progress, "部署")
	case "charge":
		return strings.Contains(folded, "pay") || strings.Contains(folded, "charge") || strings.Contains(progress, "扣")
	case "send_to_user", "delivery":
		return strings.Contains(folded, "send") || strings.Contains(progress, "发送") || strings.Contains(progress, "投递")
	default:
		return false
	}
}

// JournalOnVM reports that path sits on a computer volume. A service database
// on one Hub disk is not such a path. A local sqlite file is not a cluster.
func JournalOnVM(path string) bool {
	slash := strings.ToLower(filepath.ToSlash(path))
	if slash == "" {
		return false
	}
	for _, volume := range []string{"/desktops/", "/home/desktop/", "/opt/", "/usr/local/"} {
		if strings.Contains(slash, volume) {
			return true
		}
	}
	return false
}

// TemporalThisCommit reports that this commit does not install the Temporal
// Go SDK, vendor a server, or start one. Cross-server resume in this commit
// is another process opening the same service database.
func TemporalThisCommit() bool { return false }
