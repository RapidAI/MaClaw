package structureddata

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

var businessIntentAmountPattern = regexp.MustCompile(`(?i)(?:[$￥¥]\s*)?\d+(?:[.,]\d+)?\s*(?:rmb|cny|usd|eur|元|块)?`)

func (s *Service) ResolveBusinessIntent(ctx context.Context, p Principal, in ResolveBusinessIntentInput) (*ResolveBusinessIntentResult, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, ErrInvalidInput
	}
	domainFilter := strings.ToLower(strings.TrimSpace(in.Domain))
	domains, err := s.businessDomainNames(ctx, p)
	if err != nil {
		return nil, err
	}
	matches := make([]BusinessIntentMatch, 0)
	for _, domain := range domains {
		if domainFilter != "" && domain != domainFilter {
			continue
		}
		for _, useCase := range businessDomainUseCases(domain) {
			score, matched := scoreBusinessUseCase(query, domain, useCase)
			if score <= 0 {
				continue
			}
			nextSteps := s.businessIntentNextSteps(ctx, p, domain, useCase)
			confidence := businessIntentConfidence(score)
			actionID := strings.TrimSpace(useCase.PreferredAction)
			matches = append(matches, BusinessIntentMatch{
				Domain:           domain,
				Title:            businessDomainTitle(domain),
				UseCase:          useCase,
				Score:            score,
				Confidence:       confidence,
				Decision:         businessIntentDecision(confidence),
				BusinessObjectID: businessIntentObjectID(actionID),
				BusinessActionID: actionID,
				IntentSignals:    semanticIntentSignals(matched),
				Matched:          matched,
				NextSteps:        nextSteps,
			})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].UseCase.ID < matches[j].UseCase.ID
		}
		return matches[i].Score > matches[j].Score
	})
	limit := in.Limit
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return &ResolveBusinessIntentResult{Query: query, Domain: domainFilter, Matches: matches}, nil
}

func (s *Service) businessIntentNextSteps(ctx context.Context, p Principal, domain string, useCase BusinessDomainUseCase) []BusinessIntentNextStep {
	steps := []BusinessIntentNextStep{}
	order := 1
	addStep := func(action, purpose, description string, adminOnly, dryRun bool, params map[string]any, requiredFields []string, inputFields []DatasetTemplateField) {
		dataTemplate := businessActionDataTemplate(inputFields)
		steps = append(steps, BusinessIntentNextStep{
			Order:            order,
			Action:           action,
			Purpose:          purpose,
			Description:      description,
			AdminOnly:        adminOnly,
			DryRun:           dryRun,
			RequiredFields:   append([]string(nil), requiredFields...),
			InputFields:      append([]DatasetTemplateField(nil), inputFields...),
			DataTemplate:     dataTemplate,
			BodyTemplate:     businessIntentBodyTemplate(action, dryRun, params, dataTemplate),
			ToolCallTemplate: businessIntentToolCallTemplate(action, dryRun, params, dataTemplate),
			Params:           params,
		})
		order++
	}
	catalog, err := s.GetBusinessDomain(ctx, p, domain)
	if err == nil && len(catalog.MissingTemplates) > 0 {
		addStep("bootstrap_templates", "initialize", "Preview missing standard datasets for this business domain before writing business data.", true, true, map[string]any{"domains": []string{domain}, "dry_run": true, "missing_templates": catalog.MissingTemplates}, nil, nil)
	}
	if useCase.PreferredDashboard != "" {
		addStep("run_dashboard", "overview", "Read the domain overview before making or explaining business changes.", false, false, map[string]any{"dashboard_id": useCase.PreferredDashboard}, nil, nil)
	}
	if useCase.PreferredAction != "" {
		requiredFields, inputFields := businessActionInputContract(useCase.PreferredAction)
		if useCase.DryRunRecommended {
			addStep("execute_business_action", "business_write_preview", "Validate and preview the business write before committing it.", false, true, map[string]any{"business_action_id": useCase.PreferredAction, "dry_run": true}, requiredFields, inputFields)
		}
		addStep("execute_business_action", "business_write", "Execute the preferred business action with caller-supplied business data.", false, false, map[string]any{"business_action_id": useCase.PreferredAction}, requiredFields, inputFields)
	}
	if useCase.PreferredView != "" {
		addStep("query_business_view", "business_read", "Read curated records for this business use case.", false, false, map[string]any{"view_id": useCase.PreferredView}, nil, nil)
	}
	if useCase.PreferredReport != "" {
		addStep("run_report", "analysis", "Run the built-in report for this business use case.", false, false, map[string]any{"report_id": useCase.PreferredReport}, nil, nil)
	}
	return steps
}

func businessActionInputContract(actionID string) ([]string, []DatasetTemplateField) {
	actionID = strings.TrimSpace(actionID)
	for _, action := range businessActions {
		if action.ID == actionID {
			return append([]string(nil), action.RequiredFields...), append([]DatasetTemplateField(nil), action.InputFields...)
		}
	}
	return nil, nil
}

func businessActionDataTemplate(fields []DatasetTemplateField) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			continue
		}
		out[key] = defaultValueForTemplateField(field)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func businessIntentBodyTemplate(action string, dryRun bool, params map[string]any, dataTemplate map[string]any) map[string]any {
	switch action {
	case "execute_business_action":
		body := map[string]any{
			"record_id":          "",
			"idempotency_key":    "",
			"title":              "",
			"tags":               []any{},
			"data":               dataTemplate,
			"occurred_at":        "",
			"dry_run":            dryRun,
			"business_action_id": "",
		}
		if actionID, ok := params["business_action_id"].(string); ok {
			body["business_action_id"] = actionID
		}
		return body
	case "bootstrap_templates", "run_dashboard", "query_business_view", "run_report":
		body := map[string]any{}
		for key, value := range params {
			body[key] = value
		}
		if dryRun {
			body["dry_run"] = true
		}
		if len(body) > 0 {
			return body
		}
	}
	return nil
}

func businessIntentToolCallTemplate(action string, dryRun bool, params map[string]any, dataTemplate map[string]any) map[string]any {
	toolArgs := map[string]any{"action": action}
	for key, value := range params {
		toolArgs[key] = value
	}
	if dryRun {
		toolArgs["dry_run"] = true
	}
	if action == "execute_business_action" {
		toolArgs["data"] = dataTemplate
	}
	if len(toolArgs) == 1 {
		return nil
	}
	return map[string]any{
		"tool": "mis_data",
		"args": toolArgs,
	}
}

func defaultValueForTemplateField(field DatasetTemplateField) any {
	if value, ok := firstEnumValue(field.Config); ok {
		return value
	}
	switch strings.ToLower(strings.TrimSpace(field.Type)) {
	case "number", "integer", "float", "decimal":
		return 0
	case "boolean", "bool":
		return false
	case "array", "list":
		return []any{}
	case "object", "json":
		return map[string]any{}
	case "date":
		return "YYYY-MM-DD"
	case "datetime", "timestamp":
		return "YYYY-MM-DDTHH:MM:SSZ"
	default:
		return ""
	}
}

func firstEnumValue(config map[string]any) (any, bool) {
	for _, key := range []string{"enum", "values"} {
		raw, ok := config[key]
		if !ok {
			continue
		}
		switch values := raw.(type) {
		case []any:
			if len(values) > 0 {
				return values[0], true
			}
		case []string:
			if len(values) > 0 {
				return values[0], true
			}
		}
	}
	return nil, false
}

func scoreBusinessUseCase(query, domain string, useCase BusinessDomainUseCase) (int, []string) {
	queryFold := strings.ToLower(strings.TrimSpace(query))
	if queryFold == "" {
		return 0, nil
	}
	score := 0
	matched := []string{}
	addMatch := func(label string, points int) {
		score += points
		matched = append(matched, label)
	}
	if strings.Contains(queryFold, strings.ToLower(domain)) {
		addMatch("domain:"+domain, 8)
	}
	for _, token := range strings.FieldsFunc(strings.ToLower(useCase.ID), splitIntentToken) {
		if len(token) >= 3 && strings.Contains(queryFold, token) {
			addMatch("id:"+token, 6)
		}
	}
	for _, token := range strings.FieldsFunc(strings.ToLower(useCase.Title), splitIntentToken) {
		if len(token) >= 3 && strings.Contains(queryFold, token) {
			addMatch("title:"+token, 5)
		}
	}
	for _, hint := range useCase.IntentHints {
		hint = strings.ToLower(strings.TrimSpace(hint))
		if hint == "" {
			continue
		}
		if strings.Contains(queryFold, hint) {
			addMatch("hint:"+hint, 20+len([]rune(hint)))
			continue
		}
		for _, token := range strings.FieldsFunc(hint, splitIntentToken) {
			if len(token) >= 2 && strings.Contains(queryFold, token) {
				addMatch("hint_token:"+token, 4)
			}
		}
	}
	for _, target := range []string{useCase.PreferredAction, useCase.PreferredView, useCase.PreferredReport, useCase.PreferredDashboard} {
		target = strings.ToLower(strings.TrimSpace(target))
		if target == "" {
			continue
		}
		if strings.Contains(queryFold, target) {
			addMatch("target:"+target, 10)
		}
	}
	semanticScore, semanticSignals := scoreBusinessUseCaseSemantics(queryFold, domain, useCase)
	if semanticScore > 0 {
		score += semanticScore
		matched = append(matched, semanticSignals...)
	}
	return score, dedupeStrings(matched)
}

func scoreBusinessUseCaseSemantics(queryFold, domain string, useCase BusinessDomainUseCase) (int, []string) {
	actionID := strings.ToLower(strings.TrimSpace(useCase.PreferredAction))
	useCaseID := strings.ToLower(strings.TrimSpace(useCase.ID))
	signals := detectedBusinessIntentSignals(queryFold)
	if len(signals) == 0 {
		return 0, nil
	}
	has := func(signal string) bool {
		_, ok := signals[signal]
		return ok
	}
	out := []string{}
	add := func(signal string) {
		out = append(out, "semantic:"+signal)
	}
	score := 0
	switch {
	case actionID == "finance.expense_submit" || useCaseID == "finance.submit_expense":
		if has("amount") && (has("travel") || has("meal") || has("lodging") || has("client_visit")) {
			score += 46
			add("expense_context")
		}
		if has("attachment") || has("invoice_document") {
			score += 12
			add("expense_evidence")
		}
	case actionID == "finance.invoice_upsert":
		if has("invoice_document") {
			score += 20
			add("invoice_document")
		}
		if has("tax") || has("counterparty") || has("issue_receive") {
			score += 18
			add("invoice_lifecycle")
		}
	case strings.HasPrefix(actionID, "procurement."):
		if has("purchase_request") || has("supplier") {
			score += 34
			add("procurement_context")
		}
		if has("amount") {
			score += 6
			add("amount")
		}
	case strings.HasPrefix(actionID, "hr.leave"):
		if has("absence") && has("date_time") {
			score += 40
			add("leave_context")
		}
	case strings.HasPrefix(actionID, "sales.") || domain == "sales":
		if has("client_visit") || has("sales_activity") {
			score += 24
			add("sales_context")
		}
	}
	return score, out
}

func detectedBusinessIntentSignals(queryFold string) map[string]struct{} {
	signals := map[string]struct{}{}
	add := func(signal string) { signals[signal] = struct{}{} }
	if businessIntentAmountPattern.MatchString(queryFold) {
		add("amount")
	}
	if containsAny(queryFold, "attached", "attachment", "receipt", "\u9644\u4ef6", "\u7968\u636e", "\u5355\u636e") {
		add("attachment")
	}
	if containsAny(queryFold, "invoice", "\u53d1\u7968", "\u5f00\u7968", "\u6536\u7968") {
		add("invoice_document")
	}
	if containsAny(queryFold, "train", "rail", "flight", "taxi", "trip", "travel", "\u9ad8\u94c1", "\u706b\u8f66", "\u673a\u7968", "\u6253\u8f66", "\u51fa\u5dee", "\u884c\u7a0b") {
		add("travel")
	}
	if containsAny(queryFold, "meal", "lunch", "dinner", "breakfast", "restaurant", "\u9910", "\u5348\u9910", "\u665a\u9910", "\u65e9\u9910", "\u996d") {
		add("meal")
	}
	if containsAny(queryFold, "hotel", "lodging", "stay", "\u9152\u5e97", "\u4f4f\u5bbf") {
		add("lodging")
	}
	if containsAny(queryFold, "customer visit", "client visit", "meet customer", "\u89c1\u5ba2\u6237", "\u62dc\u8bbf\u5ba2\u6237", "\u5ba2\u6237\u62dc\u8bbf") {
		add("client_visit")
	}
	if containsAny(queryFold, "supplier", "vendor", "\u4f9b\u5e94\u5546", "\u4f9b\u65b9") {
		add("supplier")
	}
	if containsAny(queryFold, "purchase", "procure", "buy", "request computer", "\u91c7\u8d2d", "\u7533\u8bf7\u7535\u8111", "\u4e70") {
		add("purchase_request")
	}
	if containsAny(queryFold, "leave", "vacation", "absence", "sick", "\u8bf7\u5047", "\u4f11\u5047", "\u75c5\u5047", "\u8c03\u4f11") {
		add("absence")
	}
	if containsAny(queryFold, "yesterday", "today", "tomorrow", "next week", "\u6628\u5929", "\u4eca\u5929", "\u660e\u5929", "\u4e0b\u5468") || regexp.MustCompile(`\d{4}-\d{1,2}-\d{1,2}`).MatchString(queryFold) {
		add("date_time")
	}
	if containsAny(queryFold, "tax", "vat", "\u7a0e", "\u7a0e\u989d", "\u7a0e\u53f7") {
		add("tax")
	}
	if containsAny(queryFold, "counterparty", "\u5bf9\u65b9", "\u5f80\u6765\u5355\u4f4d", "\u62ac\u5934") {
		add("counterparty")
	}
	if containsAny(queryFold, "issued", "received", "\u5f00\u5177", "\u6536\u5230", "\u5f00\u7968", "\u6536\u7968") {
		add("issue_receive")
	}
	if containsAny(queryFold, "lead", "deal", "opportunity", "order", "\u5546\u673a", "\u8ba2\u5355", "\u9500\u552e\u7ebf\u7d22") {
		add("sales_activity")
	}
	return signals
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(text, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func semanticIntentSignals(matched []string) []string {
	out := []string{}
	for _, item := range matched {
		if strings.HasPrefix(item, "semantic:") {
			out = append(out, strings.TrimPrefix(item, "semantic:"))
		}
	}
	return dedupeStrings(out)
}

func businessIntentConfidence(score int) float64 {
	switch {
	case score >= 80:
		return 0.95
	case score >= 60:
		return 0.86
	case score >= 40:
		return 0.72
	case score >= 25:
		return 0.55
	default:
		return float64(score) / 50
	}
}

func businessIntentDecision(confidence float64) string {
	switch {
	case confidence >= 0.8:
		return "auto_open_task_panel"
	case confidence >= 0.5:
		return "ask_user_to_choose"
	default:
		return "clarify"
	}
}

func businessIntentObjectID(actionID string) string {
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return ""
	}
	for _, action := range businessActions {
		if action.ID == actionID {
			return action.DatasetID
		}
	}
	return ""
}

func splitIntentToken(r rune) bool {
	return r == '.' || r == '_' || r == '-' || r == ' ' || r == '/' || r == ',' || r == ';' || r == ':' || r == '(' || r == ')'
}

func dedupeStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
