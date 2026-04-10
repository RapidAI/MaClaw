package clawnet

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ── Clawnet error message localisation ──────────────────────────────────
//
// Raw clawnet errors look like:
//   clawnet POST /api/tasks/.../claim: status 403: {"error":"cannot claim your own task"}
//
// FormatError extracts the human-readable part and optionally translates
// known error keys into the requested language ("zh" or "en").

// knownErrors maps the English error text returned by the clawnet node to
// a localised version.  The "en" entry may refine the wording; "zh" provides
// a Chinese translation.
var knownErrors = map[string]map[string]string{
	// ── task claim / bid ──
	"cannot claim your own task": {
		"zh": "不能认领自己发布的任务",
		"en": "cannot claim your own task",
	},
	"task already claimed": {
		"zh": "任务已被认领",
		"en": "task already claimed",
	},
	"task not found": {
		"zh": "任务不存在",
		"en": "task not found",
	},
	"task is not open": {
		"zh": "任务不在开放状态",
		"en": "task is not open",
	},
	"already bid on this task": {
		"zh": "已经对此任务出价",
		"en": "already bid on this task",
	},
	// ── submit ──
	"not assigned to you": {
		"zh": "任务未分配给你",
		"en": "not assigned to you",
	},
	"result is empty": {
		"zh": "提交结果为空",
		"en": "result is empty",
	},
	// ── credits ──
	"insufficient credits": {
		"zh": "积分不足",
		"en": "insufficient credits",
	},
	// ── auth ──
	"not authorized": {
		"zh": "未授权",
		"en": "not authorized",
	},
	"forbidden": {
		"zh": "无权限",
		"en": "forbidden",
	},
}

// operationLabels maps the operation prefix used in failTask messages to
// localised labels.
var operationLabels = map[string]map[string]string{
	"claim": {
		"zh": "认领任务",
		"en": "claim",
	},
	"bid": {
		"zh": "竞标任务",
		"en": "bid",
	},
	"submit": {
		"zh": "提交结果",
		"en": "submit",
	},
	"execution": {
		"zh": "执行任务",
		"en": "execution",
	},
	"get_task": {
		"zh": "获取任务",
		"en": "get task",
	},
}

// reClawnetHTTP matches the raw clawnet HTTP error format:
//   clawnet POST /api/...: status 403: {"error":"..."}
//   clawnet GET  /api/...: status 404: {"error":"..."}
var reClawnetHTTP = regexp.MustCompile(
	`clawnet (?:GET|POST|DELETE|PUT|PATCH) \S+: status \d+: (.+)`)

// extractJSONError tries to pull the "error" field from a JSON string.
func extractJSONError(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 || s[0] != '{' {
		return s
	}
	var obj struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(s), &obj) == nil && obj.Error != "" {
		return obj.Error
	}
	return s
}

// extractCoreMessage takes a raw clawnet error string and returns the
// human-readable core message (stripping HTTP method, path, status code).
func extractCoreMessage(raw string) string {
	if m := reClawnetHTTP.FindStringSubmatch(raw); len(m) == 2 {
		return extractJSONError(m[1])
	}
	return raw
}

// localise looks up a core message in knownErrors and returns the
// localised version.  Falls back to the original message.
func localise(core, lang string) string {
	lower := strings.ToLower(strings.TrimSpace(core))
	if m, ok := knownErrors[lower]; ok {
		if v, ok := m[lang]; ok {
			return v
		}
	}
	return core
}

// opLabel returns the localised label for an operation key.
func opLabel(op, lang string) string {
	if m, ok := operationLabels[op]; ok {
		if v, ok := m[lang]; ok {
			return v
		}
	}
	return op
}

// FormatError takes a raw clawnet error, extracts the meaningful message,
// and returns a localised, human-friendly string.
//
//	lang: "zh" or "en" (defaults to "en" if unknown)
//
// Example:
//
//	in:  "clawnet POST /api/tasks/xxx/claim: status 403: {\"error\":\"cannot claim your own task\"}"
//	out: "不能认领自己发布的任务"  (lang="zh")
func FormatError(err error, lang string) string {
	if err == nil {
		return ""
	}
	if lang != "zh" && lang != "en" {
		lang = "en"
	}
	core := extractCoreMessage(err.Error())
	return localise(core, lang)
}

// FormatTaskError formats a task-operation error with a localised operation
// prefix.
//
//	op:   "claim", "bid", "submit", "execution", "get_task"
//	lang: "zh" or "en"
//
// Example:
//
//	FormatTaskError("claim", err, "zh")
//	→ "认领任务失败: 不能认领自己发布的任务"
func FormatTaskError(op string, err error, lang string) string {
	if err == nil {
		return ""
	}
	if lang != "zh" && lang != "en" {
		lang = "en"
	}
	core := localise(extractCoreMessage(err.Error()), lang)
	label := opLabel(op, lang)
	if lang == "zh" {
		return label + "失败: " + core
	}
	return label + " failed: " + core
}
