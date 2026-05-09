package structureddata

import (
	"context"
	"sort"
	"strings"
)

func (s *Service) ListEventContracts(ctx context.Context, p Principal, domain string, query ...QueryEventContractsInput) ([]EventContract, error) {
	_ = ctx
	_ = p
	in := QueryEventContractsInput{Domain: strings.ToLower(strings.TrimSpace(domain))}
	if len(query) > 0 {
		in = query[0]
		in.Domain = strings.ToLower(strings.TrimSpace(in.Domain))
	}
	out := make([]EventContract, 0, len(businessActions))
	for _, action := range businessActions {
		if in.Domain != "" && strings.ToLower(strings.TrimSpace(action.Domain)) != in.Domain {
			continue
		}
		out = append(out, eventContractFromAction(action))
	}
	if len(query) == 0 {
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out, nil
	}
	return paginateEventContracts(out, in), nil
}

func paginateEventContracts(items []EventContract, in QueryEventContractsInput) []EventContract {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := append([]EventContract(nil), items...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	beforeID := strings.TrimSpace(in.BeforeID)
	if beforeID != "" {
		filtered := out[:0]
		for _, contract := range out {
			if contract.ID < beforeID {
				filtered = append(filtered, contract)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) GetEventContract(ctx context.Context, p Principal, actionID string) (*EventContract, error) {
	action, err := s.GetBusinessAction(ctx, p, actionID)
	if err != nil {
		return nil, err
	}
	contract := eventContractFromAction(*action)
	return &contract, nil
}

func eventContractFromAction(action BusinessAction) EventContract {
	dataTemplate := businessActionDataTemplate(action.InputFields)
	base := map[string]any{
		"source":             sourceHintForDomain(action.Domain),
		"business_action_id": action.ID,
		"record_id":          recordIDTemplateForAction(action),
		"idempotency_key":    idempotencyTemplateForAction(action),
		"data":               dataTemplate,
	}
	dryRun := cloneJSONMap(base)
	dryRun["dry_run"] = true
	commit := cloneJSONMap(base)
	commit["dry_run"] = false
	return EventContract{
		ID:                  action.ID,
		Domain:              action.Domain,
		Title:               action.Title,
		Description:         action.Description,
		SourceHint:          sourceHintForDomain(action.Domain),
		Endpoint:            "/api/v1/data/events",
		ConnectorEndpoint:   "/api/v1/data/connectors/{connector_id}/events",
		Method:              "POST",
		BusinessAction:      action.ID,
		EventType:           action.EventType,
		Operation:           action.Operation,
		DatasetID:           action.DatasetID,
		RequiredFields:      append([]string(nil), action.RequiredFields...),
		InputFields:         append([]DatasetTemplateField(nil), action.InputFields...),
		SuggestedTags:       append([]string(nil), action.SuggestedTags...),
		DataTemplate:        dataTemplate,
		DryRunBodyTemplate:  dryRun,
		CommitBodyTemplate:  commit,
		IdempotencyTemplate: idempotencyTemplateForAction(action),
		RecommendedFlow: []string{
			"GET /api/v1/data/event-contracts/" + action.ID,
			"POST /api/v1/data/connectors/{connector_id}/events with dry_run=true for registered connectors, or POST /api/v1/data/events",
			"POST /api/v1/data/connectors/{connector_id}/events with dry_run=false and a stable idempotency_key",
			"GET /api/v1/data/events?business_action_id=" + action.ID,
		},
	}
}

func sourceHintForDomain(domain string) string {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "sales":
		return "crm"
	case "finance":
		return "finance"
	case "hr":
		return "hris"
	case "procurement":
		return "erp"
	case "inventory":
		return "wms"
	case "assets":
		return "asset_system"
	case "legal":
		return "contract_system"
	default:
		return "external_system"
	}
}

func recordIDTemplateForAction(action BusinessAction) string {
	for _, key := range []string{"order_no", "customer_no", "expense_no", "invoice_no", "employee_no", "contract_no", "po_no", "sku", "asset_no"} {
		if stringSliceContains(action.RequiredFields, key) {
			return "{" + key + "}"
		}
		for _, field := range action.InputFields {
			if field.Key == key {
				return "{" + key + "}"
			}
		}
	}
	return "{external_record_id}"
}

func stringSliceContains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func idempotencyTemplateForAction(action BusinessAction) string {
	return sourceHintForDomain(action.Domain) + ":" + action.ID + ":" + recordIDTemplateForAction(action) + ":{version}"
}
