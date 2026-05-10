package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func (h *IMMessageHandler) emitSkillRunAgentViewIfNeeded(name string, args map[string]interface{}) bool {
	if h == nil || h.app == nil {
		return false
	}
	target := h.app.findSkillForAgentView(name)
	if target == nil {
		return false
	}
	runArgs := buildRunSkillArgs(args)
	vars := normalizeSkillRunVars(runArgs)
	params, missing := skillRunParameterContract(target, vars, runArgs)
	if len(missing) == 0 {
		return false
	}
	view := buildSkillRunAgentView(*target, runArgs, params, missing)
	if view == nil {
		return false
	}
	h.app.emitAgentView(view)
	return true
}

func (a *App) handleSkillRunAgentViewSubmit(skillName string, data map[string]interface{}) *IMAgentResponse {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return &IMAgentResponse{Text: "Skill task panel submission is missing skill name.", Error: "missing skill name", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	a.ensureSkillRunner()
	if a.skillRunner == nil {
		return &IMAgentResponse{Text: "Skill Runner is not initialized.", Error: "skill runner not initialized", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	target := a.findSkillForAgentView(skillName)
	baseArgs, _ := data["_run_args"].(map[string]interface{})
	runArgs := cloneMISInterfaceMap(baseArgs)
	formArgs, _ := runArgs["args"].(map[string]interface{})
	if formArgs == nil {
		formArgs = map[string]interface{}{}
	}
	for key, value := range data {
		key = strings.TrimSpace(key)
		if key == "" || strings.HasPrefix(key, "_") {
			continue
		}
		runArgs[key] = value
		formArgs[key] = value
	}
	if len(formArgs) > 0 {
		runArgs["args"] = formArgs
	}
	if target != nil {
		vars := normalizeSkillRunVars(runArgs)
		params, missing := skillRunParameterContract(target, vars, runArgs)
		fields := skillAgentViewFields(params, missing, runArgs)
		attachSkillOperationOptions(fields, target.Operations)
		if validationErrors := normalizeSkillAgentViewSubmittedValues(fields, data, runArgs, formArgs); len(validationErrors) > 0 {
			view := buildSkillRunAgentView(*target, runArgs, params, missing)
			if view != nil {
				view["formErrors"] = validationErrors
				a.emitAgentView(view)
			}
			return &IMAgentResponse{Text: "Skill parameters need correction. Review the task panel.", Error: strings.Join(validationErrors, "; "), ResponseSource: imResponseSourceAgentViewSubmit.String()}
		}
		vars = normalizeSkillRunVars(runArgs)
		params, missing = skillRunParameterContract(target, vars, runArgs)
		if len(missing) > 0 {
			view := buildSkillRunAgentView(*target, runArgs, params, missing)
			if view != nil {
				a.emitAgentView(view)
			}
			return &IMAgentResponse{Text: "Skill parameters are still incomplete. Review the task panel.", Error: "missing required parameters: " + strings.Join(missing, ", "), ResponseSource: imResponseSourceAgentViewSubmit.String()}
		}
	}
	runID, err := a.skillRunner.StartRun(skillName, runArgs)
	if err != nil {
		return &IMAgentResponse{Text: "Skill start failed.", Error: err.Error(), ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	status, err := waitForSkillRunnerSnapshot(a.skillRunner, runID, 2*time.Second)
	if err != nil {
		return &IMAgentResponse{Text: fmt.Sprintf("Skill started, but status snapshot failed. run_id=%s", runID), Error: err.Error(), ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	if a.ctx != nil {
		a.emitAgentView(buildSkillRunStatusAgentView(status, runID))
	}
	return &IMAgentResponse{Text: fmt.Sprintf("Skill started from task panel. run_id=%s", runID), ResponseSource: imResponseSourceAgentViewSubmit.String()}
}

func (a *App) handleSkillStatusAgentViewSubmit(data map[string]interface{}) *IMAgentResponse {
	if a == nil {
		return &IMAgentResponse{Text: "Skill status is not available.", Error: "app not initialized", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	a.ensureSkillRunner()
	if a.skillRunner == nil {
		return &IMAgentResponse{Text: "Skill Runner is not initialized.", Error: "skill runner not initialized", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	runID := strings.TrimSpace(fmt.Sprint(data["run_id"]))
	if runID == "" || runID == "<nil>" {
		return &IMAgentResponse{Text: "Skill status submission is missing run_id.", Error: "missing run_id", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	status, err := waitForSkillRunnerSnapshot(a.skillRunner, runID, 500*time.Millisecond)
	if err != nil {
		return &IMAgentResponse{Text: "Skill status refresh failed.", Error: err.Error(), ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	a.emitAgentView(buildSkillRunStatusAgentView(status, runID))
	return &IMAgentResponse{Text: "Skill status refreshed in the task panel.", ResponseSource: imResponseSourceAgentViewSubmit.String()}
}

func (a *App) findSkillForAgentView(name string) *corelib.NLSkillEntry {
	name = strings.TrimSpace(name)
	if a == nil || a.skillExecutor == nil || name == "" {
		return nil
	}
	a.skillExecutor.mu.RLock()
	defer a.skillExecutor.mu.RUnlock()
	for _, item := range a.skillExecutor.loadSkills() {
		if item.MatchesName(name) {
			cp := item
			cskill.NormalizeSkillForRunner(&cp)
			return &cp
		}
	}
	return nil
}

func skillRunParameterContract(skill *corelib.NLSkillEntry, vars map[string]string, runArgs map[string]interface{}) ([]corelib.NLSkillParam, []string) {
	if skill == nil {
		return nil, nil
	}
	selectedSteps, err := cskill.ResolveSelectedStepLabels(skill, runArgs)
	if err != nil {
		params := cskill.CompleteParamsForRunner(skill.Params, skill.Steps, skill.RequiredArgs)
		if skillNeedsOperationChoice(skill, runArgs) {
			params = ensureSkillOperationParam(params)
			return params, []string{"operation"}
		}
		return params, nil
	}
	executionSteps := cskill.SelectedExecutableSteps(skill.Steps, selectedSteps)
	if len(executionSteps) == 0 {
		executionSteps = skill.Steps
	}
	params := cskill.CompleteParamsForRunner(skill.Params, executionSteps, skill.RequiredArgs)
	missingSet := map[string]bool{}
	for _, item := range cskill.MissingRunRequiredArgs(skill.RequiredArgs, params, vars) {
		missingSet[item] = true
	}
	requiredArgs := cskill.RequiredArgsForRunnerPrecheck(skill.RequiredArgs, executionSteps)
	precheckParams := cskill.CompleteParamsForRunner(skill.Params, executionSteps, requiredArgs)
	for _, item := range cskill.DetectImplicitRunRequiredArgs(executionSteps, vars, requiredArgs, precheckParams) {
		missingSet[item] = true
	}
	if skillNeedsOperationChoice(skill, runArgs) {
		params = ensureSkillOperationParam(params)
		missingSet["operation"] = true
	}
	missing := make([]string, 0, len(missingSet))
	for key := range missingSet {
		missing = append(missing, key)
	}
	sort.Strings(missing)
	return params, missing
}

func skillNeedsOperationChoice(skill *corelib.NLSkillEntry, runArgs map[string]interface{}) bool {
	if skill == nil || !strings.EqualFold(skill.Mode, "api_workflow") {
		return false
	}
	ops := skillOperationsWithLabels(skill.Operations)
	if len(ops) <= 1 {
		return false
	}
	selected := strings.TrimSpace(skillRunOperationArg(runArgs))
	if selected == "" {
		return true
	}
	for _, op := range ops {
		if strings.EqualFold(op.Name, selected) {
			return false
		}
	}
	return true
}

func ensureSkillOperationParam(params []corelib.NLSkillParam) []corelib.NLSkillParam {
	for i := range params {
		if strings.EqualFold(strings.TrimSpace(params[i].Name), "operation") {
			params[i].Required = true
			if strings.TrimSpace(params[i].Description) == "" {
				params[i].Description = "Choose which workflow operation to run."
			}
			return params
		}
	}
	return append([]corelib.NLSkillParam{{
		Name:        "operation",
		Description: "Choose which workflow operation to run.",
		Required:    true,
		Synthetic:   true,
	}}, params...)
}

func skillRunOperationArg(runArgs map[string]interface{}) string {
	if runArgs == nil {
		return ""
	}
	if value := cleanSkillRunOperationValue(runArgs["operation"]); value != "" {
		return value
	}
	rawArgs, ok := runArgs["args"]
	if !ok || rawArgs == nil {
		return ""
	}
	switch v := rawArgs.(type) {
	case map[string]interface{}:
		return cleanSkillRunOperationValue(v["operation"])
	case map[string]string:
		return strings.TrimSpace(v["operation"])
	case string:
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(v), &decoded); err == nil {
			return cleanSkillRunOperationValue(decoded["operation"])
		}
	}
	return ""
}

func cleanSkillRunOperationValue(raw interface{}) string {
	value := strings.TrimSpace(fmt.Sprint(raw))
	if value == "" || value == "<nil>" {
		return ""
	}
	return value
}

func buildSkillRunAgentView(skill corelib.NLSkillEntry, runArgs map[string]interface{}, params []corelib.NLSkillParam, missing []string) map[string]interface{} {
	fields := skillAgentViewFields(params, missing, runArgs)
	attachSkillOperationOptions(fields, skill.Operations)
	if len(fields) == 0 {
		for _, key := range missing {
			fields = append(fields, map[string]interface{}{"name": key, "label": key, "type": "text", "required": true})
		}
	}
	if len(fields) == 0 {
		return nil
	}
	title := strings.TrimSpace(skill.Name)
	if title == "" {
		title = "Run skill"
	}
	description := strings.TrimSpace(skill.Description)
	if description == "" {
		description = "Fill required parameters before running this standard skill."
	}
	return attachAgentViewSchemaVersion(map[string]interface{}{
		"type":        "form",
		"id":          "skill:run:" + skill.Name,
		"title":       "Run " + title,
		"description": description,
		"fields":      fields,
		"formErrors":  []string{"Required parameters are missing. Fill them here to run the skill safely."},
		"submitLabel": "Run skill",
		"meta": map[string]interface{}{
			"source": "skill.adapter",
			"skill":  skill.Name,
		},
	}, "skill.adapter", skill.Name, skillAgentViewSchemaContract(skill, params))
}

func skillAgentViewSchemaContract(skill corelib.NLSkillEntry, params []corelib.NLSkillParam) map[string]interface{} {
	return map[string]interface{}{
		"name":        strings.TrimSpace(skill.Name),
		"description": strings.TrimSpace(skill.Description),
		"operations":  append([]corelib.NLSkillOperation(nil), skill.Operations...),
		"params":      append([]corelib.NLSkillParam(nil), params...),
	}
}

func skillAgentViewFields(params []corelib.NLSkillParam, missing []string, runArgs map[string]interface{}) []map[string]interface{} {
	missingSet := map[string]bool{}
	for _, key := range missing {
		missingSet[strings.TrimSpace(key)] = true
	}
	vars := normalizeSkillRunVars(runArgs)
	fields := []map[string]interface{}{}
	seen := map[string]bool{}
	for _, param := range params {
		name := strings.TrimSpace(param.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		fieldType := skillAgentViewFieldType(name, param.Description)
		field := map[string]interface{}{
			"name":        name,
			"label":       skillAgentViewFieldLabel(name),
			"type":        fieldType,
			"required":    param.Required || missingSet[name],
			"description": strings.TrimSpace(param.Description),
		}
		for key, value := range skillAgentViewFieldHints(param, fieldType) {
			field[key] = value
		}
		if value := strings.TrimSpace(vars[name]); value != "" {
			field["value"] = skillAgentViewCoerceFieldValue(fieldType, value)
		} else if strings.EqualFold(name, "operation") {
			if value := skillRunOperationArg(runArgs); value != "" {
				field["value"] = value
			}
		} else if param.Default != "" {
			field["defaultValue"] = skillAgentViewCoerceFieldValue(fieldType, param.Default)
		}
		if missingSet[name] {
			field["error"] = "Required before running this skill."
		}
		fields = append(fields, field)
	}
	fields = append(fields, map[string]interface{}{
		"name":  "_run_args",
		"label": "_run_args",
		"type":  "hidden",
		"value": cloneMISInterfaceMap(runArgs),
	})
	return fields
}

func normalizeSkillAgentViewSubmittedValues(fields []map[string]interface{}, submitted, runArgs, formArgs map[string]interface{}) []string {
	var issues []string
	for _, field := range fields {
		name := strings.TrimSpace(fmt.Sprint(field["name"]))
		if name == "" || strings.HasPrefix(name, "_") || field["type"] == "hidden" {
			continue
		}
		raw, ok := submitted[name]
		if !ok {
			continue
		}
		normalized, errText := normalizeSkillAgentViewSubmittedValue(field, raw)
		if errText != "" {
			issues = append(issues, errText)
			continue
		}
		runArgs[name] = normalized
		formArgs[name] = normalized
	}
	if len(formArgs) > 0 {
		runArgs["args"] = formArgs
	}
	return issues
}

func normalizeSkillAgentViewSubmittedValue(field map[string]interface{}, raw interface{}) (interface{}, string) {
	name := strings.TrimSpace(fmt.Sprint(field["name"]))
	label := strings.TrimSpace(fmt.Sprint(field["label"]))
	if label == "" {
		label = name
	}
	fieldType := normalizeAgentViewFieldType(fmt.Sprint(field["type"]))
	switch fieldType {
	case agentViewFieldTypeNumber:
		if raw == nil || strings.TrimSpace(fmt.Sprint(raw)) == "" {
			return raw, ""
		}
		number, ok := skillAgentViewNumberFromAny(raw)
		if !ok {
			return raw, label + " must be a valid number"
		}
		return number, ""
	case agentViewFieldTypeBoolean:
		value, ok := skillAgentViewBoolFromAny(raw)
		if !ok {
			return raw, label + " must be true or false"
		}
		return value, ""
	case agentViewFieldTypeSelect, agentViewFieldTypeBusinessRef, agentViewFieldTypeUserRef, agentViewFieldTypeDepartmentRef:
		value := strings.TrimSpace(fmt.Sprint(raw))
		if errText := skillAgentViewValidateOption(label, value, field["options"]); errText != "" {
			return raw, errText
		}
		return value, ""
	case agentViewFieldTypeMultiSelect:
		values := skillAgentViewStringSliceFromAny(raw)
		for _, value := range values {
			if errText := skillAgentViewValidateOption(label, value, field["options"]); errText != "" {
				return raw, errText
			}
		}
		return values, ""
	case agentViewFieldTypeDate:
		value := strings.TrimSpace(fmt.Sprint(raw))
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return raw, label + " must be a valid date"
			}
		}
		return value, ""
	default:
		return raw, ""
	}
}

func skillAgentViewNumberFromAny(raw interface{}) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return n, err == nil
	default:
		n, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(raw)), 64)
		return n, err == nil
	}
}

func skillAgentViewBoolFromAny(raw interface{}) (bool, bool) {
	switch v := raw.(type) {
	case bool:
		return v, true
	case string:
		return coerceAgentViewBoolToken(v)
	}
	return false, false
}

func skillAgentViewValidateOption(label, value string, rawOptions interface{}) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	options := skillAgentViewOptionValues(rawOptions)
	if len(options) == 0 {
		return ""
	}
	if options[value] {
		return ""
	}
	return label + " must be one of the available options"
}

func skillAgentViewOptionValues(raw interface{}) map[string]bool {
	out := map[string]bool{}
	switch options := raw.(type) {
	case []map[string]interface{}:
		for _, option := range options {
			value := strings.TrimSpace(fmt.Sprint(option["value"]))
			if value != "" {
				out[value] = true
			}
		}
	case []interface{}:
		for _, option := range options {
			switch v := option.(type) {
			case map[string]interface{}:
				value := strings.TrimSpace(fmt.Sprint(v["value"]))
				if value != "" {
					out[value] = true
				}
			case string:
				out[strings.TrimSpace(v)] = true
			}
		}
	case []string:
		for _, option := range options {
			out[strings.TrimSpace(option)] = true
		}
	}
	return out
}

func skillAgentViewStringSliceFromAny(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		var decoded []string
		if err := json.Unmarshal([]byte(v), &decoded); err == nil {
			return decoded
		}
		parts := strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == '|' || r == '\n' || r == '\u3001' || r == '\uff0c'
		})
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if value := strings.TrimSpace(part); value != "" {
				out = append(out, value)
			}
		}
		return out
	default:
		if raw == nil {
			return nil
		}
		return []string{strings.TrimSpace(fmt.Sprint(raw))}
	}
}

func attachSkillOperationOptions(fields []map[string]interface{}, operations []corelib.NLSkillOperation) {
	options := skillAgentViewOperationOptions(operations)
	if len(options) == 0 {
		return
	}
	for _, field := range fields {
		if !strings.EqualFold(strings.TrimSpace(fmt.Sprint(field["name"])), "operation") {
			continue
		}
		field["type"] = "select"
		field["options"] = options
		if strings.TrimSpace(fmt.Sprint(field["description"])) == "" {
			field["description"] = "Choose which workflow operation to run."
		}
		return
	}
}

func skillAgentViewOperationOptions(operations []corelib.NLSkillOperation) []map[string]interface{} {
	ops := skillOperationsWithLabels(operations)
	options := make([]map[string]interface{}, 0, len(ops))
	for _, op := range ops {
		label := strings.TrimSpace(op.Description)
		if label == "" {
			label = op.Name
		} else {
			label = op.Name + " - " + label
		}
		options = append(options, map[string]interface{}{
			"value": op.Name,
			"label": label,
		})
	}
	return options
}

func skillOperationsWithLabels(operations []corelib.NLSkillOperation) []corelib.NLSkillOperation {
	out := make([]corelib.NLSkillOperation, 0, len(operations))
	for _, op := range operations {
		if strings.TrimSpace(op.Name) == "" || len(op.Labels) == 0 {
			continue
		}
		out = append(out, op)
	}
	return out
}

func skillAgentViewFieldType(name, description string) string {
	return inferSkillAgentViewFieldKind(name, description).FieldType().String()
}

func skillAgentViewFieldHints(param corelib.NLSkillParam, fieldType string) map[string]interface{} {
	hints := map[string]interface{}{}
	normalizedFieldType := normalizeAgentViewFieldType(fieldType)
	if normalizedFieldType.UsesOptions() {
		if options := skillAgentViewEnumOptions(param.Description); len(options) > 0 {
			hints["options"] = options
		}
	}
	text := strings.ToLower(strings.Join(append([]string{param.Name, param.Description}, param.Aliases...), " "))
	switch {
	case strings.Contains(text, "email"):
		hints["format"] = "email"
	case strings.Contains(text, "url") || strings.Contains(text, "uri") || strings.Contains(text, "link"):
		hints["format"] = "uri"
	case strings.Contains(text, "password") || strings.Contains(text, "secret") || strings.Contains(text, "token") || strings.Contains(text, "api key") || strings.Contains(text, "apikey"):
		hints["format"] = "password"
		hints["sensitive"] = true
	}
	if normalizedFieldType == agentViewFieldTypeNumber {
		if strings.Contains(text, "seconds") || strings.Contains(text, "count") || strings.Contains(text, "limit") {
			hints["min"] = 0
		}
	}
	if placeholder := skillAgentViewPlaceholder(param, fieldType); placeholder != "" {
		hints["placeholder"] = placeholder
	}
	return hints
}

func skillAgentViewEnumOptions(description string) []map[string]interface{} {
	text := strings.TrimSpace(description)
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	var segment string
	for _, marker := range []string{"options:", "option:", "one of:", "oneof:", "enum:", "values:"} {
		if idx := strings.Index(lower, marker); idx >= 0 {
			segment = strings.TrimSpace(text[idx+len(marker):])
			break
		}
	}
	if segment == "" {
		return nil
	}
	if idx := strings.IndexAny(segment, ".;\n"); idx >= 0 {
		segment = segment[:idx]
	}
	parts := strings.FieldsFunc(segment, func(r rune) bool {
		return r == ',' || r == '|' || r == '/' || r == '\u3001' || r == '\uff0c'
	})
	options := make([]map[string]interface{}, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		value := strings.Trim(strings.TrimSpace(part), "`'\" ")
		if value == "" || seen[strings.ToLower(value)] {
			continue
		}
		seen[strings.ToLower(value)] = true
		options = append(options, map[string]interface{}{"value": value, "label": value})
	}
	if len(options) < 2 {
		return nil
	}
	return options
}

func skillAgentViewFieldLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func skillAgentViewPlaceholder(param corelib.NLSkillParam, fieldType string) string {
	normalizedFieldType := normalizeAgentViewFieldType(fieldType)
	if normalizedFieldType.SuppressesPlaceholder() {
		return ""
	}
	if len(param.Aliases) > 0 {
		return "Aliases: " + strings.Join(param.Aliases, ", ")
	}
	switch normalizedFieldType {
	case agentViewFieldTypeFile:
		return "Enter a file or folder path"
	case agentViewFieldTypeDate:
		return "YYYY-MM-DD"
	default:
		return ""
	}
}

func skillAgentViewCoerceFieldValue(fieldType, value string) interface{} {
	value = strings.TrimSpace(value)
	switch normalizeAgentViewFieldType(fieldType) {
	case agentViewFieldTypeBoolean:
		parsed, _ := coerceAgentViewBoolToken(value)
		return parsed
	case agentViewFieldTypeNumber:
		var n float64
		if _, err := fmt.Sscanf(value, "%f", &n); err == nil {
			return n
		}
	}
	return value
}

func buildSkillRunStatusAgentView(status *SkillRunStatus, runID string) map[string]interface{} {
	if status != nil && status.IsFinished() {
		return buildSkillRunResultAgentView(status, runID)
	}
	return buildSkillRunProgressAgentView(status, runID)
}

func buildSkillRunProgressAgentView(status *SkillRunStatus, runID string) map[string]interface{} {
	steps := []map[string]interface{}{}
	title := "Skill running"
	descriptionParts := []string{"run_id: " + runID}
	if status != nil {
		if strings.TrimSpace(status.Skill) != "" {
			title = "Running " + strings.TrimSpace(status.Skill)
			descriptionParts = append(descriptionParts, "skill: "+strings.TrimSpace(status.Skill))
		}
		if strings.TrimSpace(status.Status) != "" {
			descriptionParts = append(descriptionParts, "status: "+strings.TrimSpace(status.Status))
		}
		if status.SessionProgress != nil {
			if text := strings.TrimSpace(firstNonEmptyMISAgentView(status.SessionProgress.CurrentTask, status.SessionProgress.ProgressSummary)); text != "" {
				descriptionParts = append(descriptionParts, text)
			}
		}
		for _, step := range status.Steps {
			title := strings.TrimSpace(step.Action)
			if title == "" {
				title = fmt.Sprintf("Step %d", step.Index+1)
			}
			steps = append(steps, map[string]interface{}{
				"id":          fmt.Sprintf("%d", step.Index),
				"title":       title,
				"status":      agentViewStepStatus(step.Status),
				"description": skillRunStepDescription(step),
			})
		}
	}
	return map[string]interface{}{
		"type":        "progress",
		"id":          "skill:run-status:" + runID,
		"title":       title,
		"description": strings.Join(descriptionParts, " | "),
		"steps":       steps,
		"actions": []map[string]interface{}{{
			"label":   "Refresh",
			"viewId":  "skill:status",
			"primary": true,
			"data": map[string]interface{}{
				"run_id": runID,
			},
		}},
		"meta": map[string]interface{}{
			"source": "skill.adapter",
			"run_id": runID,
		},
	}
}

func buildSkillRunResultAgentView(status *SkillRunStatus, runID string) map[string]interface{} {
	title := "Skill result"
	if status != nil && strings.TrimSpace(status.Skill) != "" {
		title = strings.TrimSpace(status.Skill) + " result"
	}
	resultStatus := ""
	if status != nil {
		resultStatus = strings.TrimSpace(status.Status)
	}
	results := []map[string]interface{}{
		{
			"id":       "summary",
			"title":    "Summary",
			"subtitle": "run_id: " + runID,
			"status":   resultStatus,
			"data":     skillRunSummaryData(status, runID),
			"actions": []map[string]interface{}{{
				"label":   "Refresh",
				"viewId":  "skill:status",
				"primary": status != nil && status.IsRunning(),
				"data": map[string]interface{}{
					"run_id": runID,
				},
			}},
		},
	}
	if status != nil {
		for _, step := range status.Steps {
			stepTitle := strings.TrimSpace(firstNonEmptyMISAgentView(step.Name, step.Action))
			if stepTitle == "" {
				stepTitle = fmt.Sprintf("Step %d", step.Index+1)
			}
			data := map[string]interface{}{
				"status":      step.Status,
				"duration_ms": step.DurationMs,
			}
			if output := skillRunTruncate(step.Output, 1600); output != "" {
				data["output"] = output
			}
			if errText := skillRunTruncate(step.Error, 1200); errText != "" {
				data["error"] = errText
			}
			if len(step.StdoutLastLines) > 0 {
				data["stdout_tail"] = strings.Join(step.StdoutLastLines, "\n")
			}
			if len(step.StderrLastLines) > 0 {
				data["stderr_tail"] = strings.Join(step.StderrLastLines, "\n")
			}
			results = append(results, map[string]interface{}{
				"id":     fmt.Sprintf("step-%d", step.Index),
				"title":  stepTitle,
				"status": step.Status,
				"data":   data,
			})
		}
	}
	return map[string]interface{}{
		"type":        "result_browser",
		"id":          "skill:run-result:" + runID,
		"title":       title,
		"description": "Skill execution finished.",
		"results":     results,
		"meta": map[string]interface{}{
			"source": "skill.adapter",
			"run_id": runID,
		},
	}
}

func skillRunSummaryData(status *SkillRunStatus, runID string) map[string]interface{} {
	data := map[string]interface{}{"run_id": runID}
	if status == nil {
		data["status"] = "unknown"
		return data
	}
	data["skill"] = status.Skill
	data["status"] = status.Status
	if status.DurationMs > 0 {
		data["duration_ms"] = status.DurationMs
	}
	if status.TotalSteps > 0 {
		data["total_steps"] = status.TotalSteps
	}
	if status.FailedSteps > 0 {
		data["failed_steps"] = status.FailedSteps
	}
	if status.SkippedSteps > 0 {
		data["skipped_steps"] = status.SkippedSteps
	}
	if len(status.Warnings) > 0 {
		data["warnings"] = strings.Join(status.Warnings, "\n")
	}
	if status.Error != "" {
		data["error"] = skillRunTruncate(status.Error, 1200)
	}
	if status.Summary.ArtifactPath != "" {
		data["artifact_path"] = status.Summary.ArtifactPath
	}
	if status.Summary.ArtifactStatus != "" {
		data["artifact_status"] = status.Summary.ArtifactStatus
	}
	if status.Summary.LastOutputSnippet != "" {
		data["last_output"] = status.Summary.LastOutputSnippet
	}
	if status.Summary.LastErrorSnippet != "" {
		data["last_error"] = status.Summary.LastErrorSnippet
	}
	if status.Session != nil && status.Session.SessionID != "" {
		data["session_id"] = status.Session.SessionID
	}
	return data
}

func skillRunStepDescription(step StepResult) string {
	return skillRunTruncate(firstNonEmptyMISAgentView(step.Error, step.Output, strings.Join(step.StdoutLastLines, "\n"), strings.Join(step.StderrLastLines, "\n")), 260)
}

func skillRunTruncate(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "... (truncated)"
}

func agentViewStepStatus(status string) string {
	return string(normalizeAgentViewStepStatus(status))
}
