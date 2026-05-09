package structureddata

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func (s *Service) ProposeSchema(ctx context.Context, p Principal, datasetID string, in SchemaProposalInput) (*SchemaProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	datasetID = strings.TrimSpace(datasetID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	existing := map[string]struct{}{}
	for _, field := range fields {
		existing[field.Key] = struct{}{}
	}
	keys := make([]string, 0, len(in.SampleData))
	for key := range in.SampleData {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	suggested := []FieldDefinition{}
	ignored := []string{}
	for _, rawKey := range keys {
		key, err := normalizeIdentifier(rawKey, "field key")
		if err != nil {
			ignored = append(ignored, rawKey)
			continue
		}
		if _, ok := existing[key]; ok {
			ignored = append(ignored, rawKey)
			continue
		}
		suggested = append(suggested, FieldDefinition{Key: key, Type: inferFieldType(in.SampleData[rawKey]), Title: titleFromKey(key), Indexed: shouldIndexField(key), Sensitive: looksSensitive(key)})
	}
	now := s.now().UTC()
	proposal := SchemaProposal{ID: newID("schema-proposal"), TenantID: p.TenantID, DatasetID: datasetID, Reason: strings.TrimSpace(in.Reason), ExistingFields: fields, Suggested: suggested, Ignored: ignored, Impact: map[string]interface{}{"suggested_count": len(suggested), "requires_confirmation": true, "schema_version_will_increment": len(suggested) > 0}, Status: "pending", CreatedBy: p.UserID, CreatedAt: now, UpdatedAt: now}
	out, err := s.store.CreateSchemaProposal(ctx, proposal)
	if err == nil {
		s.audit(ctx, p, "schema.proposal_create", datasetID, "schema_proposal", proposal.ID, "Created schema proposal "+proposal.ID, map[string]any{"suggested_count": len(suggested)})
	}
	return out, err
}

func (s *Service) ListSchemaProposals(ctx context.Context, p Principal, datasetID string, in ListSchemaProposalsInput) ([]SchemaProposal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	datasetID = strings.TrimSpace(datasetID)
	in.Status = strings.TrimSpace(in.Status)
	if in.Status != "" && !validSchemaProposalStatus(in.Status) {
		return nil, fmt.Errorf("%w: invalid schema proposal status", ErrInvalidInput)
	}
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	in.Before = strings.TrimSpace(in.Before)
	in.BeforeID = strings.TrimSpace(in.BeforeID)
	return s.store.ListSchemaProposals(ctx, p.TenantID, datasetID, in)
}

func validSchemaProposalStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pending", "applied":
		return true
	default:
		return false
	}
}

func (s *Service) GetSchemaProposal(ctx context.Context, p Principal, datasetID, proposalID string) (*SchemaProposal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	datasetID = strings.TrimSpace(datasetID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	return s.store.GetSchemaProposal(ctx, p.TenantID, datasetID, strings.TrimSpace(proposalID))
}

func (s *Service) ApplySchemaProposal(ctx context.Context, p Principal, datasetID string, in ApplySchemaProposalInput) (*ApplySchemaProposalResult, error) {
	if !in.Confirm {
		return nil, fmt.Errorf("%w: confirm must be true", ErrInvalidInput)
	}
	datasetID = strings.TrimSpace(datasetID)
	fields := in.Fields
	proposalID := strings.TrimSpace(in.ProposalID)
	if proposalID != "" {
		proposal, err := s.GetSchemaProposal(ctx, p, datasetID, proposalID)
		if err != nil {
			return nil, err
		}
		if proposal.Status == "applied" {
			return nil, fmt.Errorf("%w: schema proposal already applied", ErrInvalidInput)
		}
		if len(fields) == 0 {
			fields = proposal.Suggested
		}
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: fields are required", ErrInvalidInput)
	}
	applied, err := s.UpsertFields(ctx, p, datasetID, UpsertFieldsInput{Fields: fields})
	if err != nil {
		return nil, err
	}
	if proposalID != "" {
		if _, err := s.store.UpdateSchemaProposalStatus(ctx, p.TenantID, datasetID, proposalID, "applied", p.UserID, s.now().UTC()); err != nil {
			return nil, err
		}
	}
	s.audit(ctx, p, "schema.proposal_apply", datasetID, "schema_proposal", proposalID, "Applied schema proposal", map[string]any{"field_count": len(applied)})
	return &ApplySchemaProposalResult{DatasetID: datasetID, Applied: applied, Reason: strings.TrimSpace(in.Reason)}, nil
}

func inferFieldType(value any) string {
	switch value.(type) {
	case int, int64, float64, float32, json.Number:
		return "number"
	case bool:
		return "boolean"
	case []any, []string:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "string"
	}
}

func titleFromKey(key string) string {
	parts := strings.Fields(strings.ReplaceAll(key, "_", " "))
	for i, part := range parts {
		if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}

func shouldIndexField(key string) bool {
	key = strings.ToLower(key)
	for _, part := range []string{"no", "id", "status", "type", "date", "department", "customer", "employee", "owner", "stage", "category"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

func looksSensitive(key string) bool {
	key = strings.ToLower(key)
	for _, part := range []string{"salary", "pay", "mobile", "phone", "email", "id_card", "bank", "tax"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}
