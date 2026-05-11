package main

import (
	"fmt"
	"strings"
	"time"
)

// resolveIntentLocalFallback generates a local AgentView form when MIS service
// is unavailable. It parses the LLM-provided "fields" array (or infers fields
// from the "query" text) and emits a form directly, without requiring a remote
// API call. This enables AG UI form generation in offline/disabled mode.
func (a *App) resolveIntentLocalFallback(args map[string]interface{}) string {
	query := strings.TrimSpace(stringArg(args, "query"))
	if query == "" {
		return "missing query parameter for resolve_intent"
	}

	// Try to use explicitly provided fields array first.
	fields := buildLocalAgentViewFieldsFromArgs(args)

	// If no explicit fields, infer from query text.
	if len(fields) == 0 {
		fields = inferLocalAgentViewFieldsFromQuery(query)
	}

	// Ultimate fallback: single textarea field.
	if len(fields) == 0 {
		fields = []map[string]interface{}{
			{
				"name":        "details",
				"label":       "Details",
				"type":        agentViewFieldTypeTextarea.String(),
				"required":    true,
				"description": "Describe the structured business data to submit.",
			},
		}
	}

	title := strings.TrimSpace(stringArg(args, "title"))
	if title == "" {
		title = inferLocalFormTitle(query)
	}
	if title == "" {
		title = "Local business task"
	}

	actionID := strings.TrimSpace(firstNonEmptyMISAgentView(
		stringArg(args, "business_action_id"),
		stringArg(args, "action_id"),
		fmt.Sprintf("local_form_%d", time.Now().UnixMilli()),
	))

	a.ensureMISBusinessTransactionsLoaded()
	transactionID := createMISBusinessTransaction(actionID, "local.adhoc", "local", "create", query, nil, "local.resolve_intent")

	if transactionID != "" {
		fields = append(fields, misTransactionHiddenField(transactionID))
	}

	meta := map[string]interface{}{
		"source":             "mis.resolve_intent.local_fallback",
		"query":              query,
		"business_action_id": actionID,
		"offline":            true,
	}
	view := map[string]interface{}{
		"type":        "form",
		"id":          "mis:intent:" + actionID,
		"title":       title,
		"description": "MIS data service is unavailable. Form generated locally from your description.",
		"fields":      fields,
		"submitLabel": "Submit",
		"meta":        meta,
	}
	a.emitAgentView(view)
	a.saveMISBusinessTransactions()

	// field_count excludes the hidden transaction_id field if present.
	visibleFieldCount := len(fields)
	if transactionID != "" {
		visibleFieldCount--
	}

	return marshalToolResult(map[string]interface{}{
		"status":         "local_fallback",
		"message":        fmt.Sprintf("MIS service offline; generated local AgentView form with %d field(s) in the right-side panel.", visibleFieldCount),
		"transaction_id": transactionID,
		"field_count":    visibleFieldCount,
		"generatedAt":    time.Now().Format(time.RFC3339),
	})
}

// buildLocalAgentViewFieldsFromArgs extracts fields from the "fields" argument
// if the LLM provided an explicit field definition array.
func buildLocalAgentViewFieldsFromArgs(args map[string]interface{}) []map[string]interface{} {
	rawFields, ok := args["fields"]
	if !ok {
		return nil
	}
	fieldSlice, ok := rawFields.([]interface{})
	if !ok || len(fieldSlice) == 0 {
		return nil
	}
	fields := make([]map[string]interface{}, 0, len(fieldSlice))
	for _, raw := range fieldSlice {
		fieldMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(fieldMap["name"]))
		if name == "" {
			continue
		}
		field := map[string]interface{}{"name": name}
		if label, ok := fieldMap["label"].(string); ok && label != "" {
			field["label"] = label
		} else {
			field["label"] = name
		}
		if fieldType, ok := fieldMap["type"].(string); ok && fieldType != "" {
			field["type"] = fieldType
		} else {
			field["type"] = inferSkillAgentViewFieldKind(name, fmt.Sprint(fieldMap["description"])).FieldType().String()
		}
		if req, ok := fieldMap["required"].(bool); ok {
			field["required"] = req
		}
		if desc, ok := fieldMap["description"].(string); ok && desc != "" {
			field["description"] = desc
		}
		if pattern, ok := fieldMap["pattern"].(string); ok && pattern != "" {
			field["pattern"] = pattern
		}
		if min, ok := fieldMap["min"]; ok {
			field["min"] = min
		}
		if max, ok := fieldMap["max"]; ok {
			field["max"] = max
		}
		if options, ok := fieldMap["options"]; ok {
			field["options"] = options
		}
		fields = append(fields, field)
	}
	return fields
}

// inferLocalAgentViewFieldsFromQuery parses a natural-language query to extract
// field definitions. Supports Chinese and English field descriptions.
func inferLocalAgentViewFieldsFromQuery(query string) []map[string]interface{} {
	type fieldHint struct {
		name     string
		required bool
		options  []string
	}

	// Known field mappings for common Chinese field names.
	// Ordered from longest to shortest to prefer longer matches (e.g. "手机号" over "手机").
	type knownEntry struct {
		zh   string
		hint fieldHint
	}
	knownFieldsOrdered := []knownEntry{
		{"手机号", fieldHint{name: "phone"}},
		{"手机", fieldHint{name: "phone"}},
		{"姓名", fieldHint{name: "name", required: true}},
		{"名字", fieldHint{name: "name", required: true}},
		{"性别", fieldHint{name: "gender", options: []string{"男", "女", "其他"}}},
		{"年龄", fieldHint{name: "age"}},
		{"电话", fieldHint{name: "phone"}},
		{"邮箱", fieldHint{name: "email"}},
		{"邮件", fieldHint{name: "email"}},
		{"住址", fieldHint{name: "address"}},
		{"地址", fieldHint{name: "address"}},
		{"备注", fieldHint{name: "notes"}},
		{"公司", fieldHint{name: "company"}},
		{"部门", fieldHint{name: "department"}},
		{"职位", fieldHint{name: "position"}},
		{"身份证", fieldHint{name: "id_number"}},
		{"生日", fieldHint{name: "birthday"}},
	}

	// Strategy: scan the query for all known field names (substring match).
	// This handles queries like "录入用户信息，包含姓名、性别、年龄..." where
	// delimiter-splitting produces fragments like "我想录入用户信息，包含姓名"
	// that aren't pure field names.
	seen := map[string]bool{}
	fields := make([]map[string]interface{}, 0, 8)

	for _, entry := range knownFieldsOrdered {
		if !strings.Contains(query, entry.zh) {
			continue
		}
		if seen[entry.hint.name] {
			continue
		}
		seen[entry.hint.name] = true

		field := map[string]interface{}{
			"name":  entry.hint.name,
			"label": entry.zh,
			"type":  "text",
		}

		if entry.hint.required {
			field["required"] = true
		}
		if len(entry.hint.options) > 0 {
			field["type"] = "select"
			opts := make([]map[string]string, len(entry.hint.options))
			for i, o := range entry.hint.options {
				opts[i] = map[string]string{"label": o, "value": o}
			}
			field["options"] = opts
		}

		// Infer type from the canonical name using existing heuristic.
		fieldType := inferSkillAgentViewFieldKind(entry.hint.name, entry.zh).FieldType().String()
		if fieldType != "" && fieldType != "text" && field["type"] == "text" {
			field["type"] = fieldType
		}

		// Check for "必填" / "不能为空" markers near this field name.
		if strings.Contains(query, entry.zh+"为必填") ||
			strings.Contains(query, entry.zh+"不能为空") ||
			strings.Contains(query, entry.zh+"必填") ||
			// Handle patterns like "手机号和姓名为必填项" or "姓名和手机号为必填"
			containsFieldInRequiredPhrase(query, entry.zh) {
			field["required"] = true
		}

		fields = append(fields, field)
	}

	return fields
}

// inferLocalFormTitle extracts a short title from the query.
func inferLocalFormTitle(query string) string {
	for _, prefix := range []string{"录入", "填写", "提交", "创建", "新建", "生成"} {
		if idx := strings.Index(query, prefix); idx >= 0 {
			end := idx + len(prefix)
			remaining := []rune(query[end:])
			if len(remaining) > 20 {
				remaining = remaining[:20]
			}
			title := string(remaining)
			for _, sep := range []string{"，", ",", "。", ".", "；", ";", "包含", "包括"} {
				if i := strings.Index(title, sep); i > 0 {
					title = title[:i]
				}
			}
			title = strings.TrimSpace(title)
			if title != "" {
				return prefix + title
			}
		}
	}
	runes := []rune(query)
	if len(runes) > 30 {
		return string(runes[:30]) + "..."
	}
	return query
}

// containsFieldInRequiredPhrase checks if a field name appears in a phrase
// that indicates required fields, e.g. "手机号和姓名为必填项" or "姓名、手机号为必填".
func containsFieldInRequiredPhrase(query, fieldName string) bool {
	// Find all occurrences of "必填" in the query and check if fieldName
	// appears in the surrounding context (within ~40 bytes before "必填").
	const windowBytes = 40
	for i := 0; ; {
		idx := strings.Index(query[i:], "必填")
		if idx < 0 {
			break
		}
		absIdx := i + idx
		start := absIdx - windowBytes
		if start < 0 {
			start = 0
		}
		// Align start to a valid UTF-8 character boundary (continuation bytes are 10xxxxxx).
		for start < absIdx && start > 0 && query[start]&0xC0 == 0x80 {
			start--
		}
		window := query[start:absIdx]
		if strings.Contains(window, fieldName) {
			return true
		}
		i = absIdx + len("必填")
	}
	return false
}
