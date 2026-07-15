package workflow

import (
	"context"
	"strings"
)

type approvalResolveContextKey struct{}

// ApprovalResolveContext carries runtime facts needed to expand dynamic
// approval-role references (e.g. applicant_department → concrete department role).
type ApprovalResolveContext struct {
	// ApplicantID is typically an email or stable user identity from instance data.
	ApplicantID string
	// ApplicantName is optional display name.
	ApplicantName string
	// DepartmentID is optional pre-resolved org group id.
	DepartmentID string
	// InstanceData is the full instance payload for flexible field extraction.
	InstanceData map[string]interface{}
}

// WithApprovalResolveContext attaches resolution facts to ctx for ApproverResolvers.
func WithApprovalResolveContext(ctx context.Context, resolve *ApprovalResolveContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if resolve == nil {
		return ctx
	}
	return context.WithValue(ctx, approvalResolveContextKey{}, resolve)
}

// ApprovalResolveContextFrom returns the resolve context if present.
func ApprovalResolveContextFrom(ctx context.Context) *ApprovalResolveContext {
	if ctx == nil {
		return nil
	}
	resolve, _ := ctx.Value(approvalResolveContextKey{}).(*ApprovalResolveContext)
	return resolve
}

// ApprovalResolveContextFromInstanceData builds a resolve context from workflow instance data.
// It understands common keys used by Hub trigger payloads and MaClaw App projections.
func ApprovalResolveContextFromInstanceData(data map[string]interface{}) *ApprovalResolveContext {
	if len(data) == 0 {
		return &ApprovalResolveContext{InstanceData: data}
	}
	applicant := firstStringFromMap(data,
		"requester_id", "requesterId",
		"applicant", "applicant_id", "applicantId", "applicant_email", "applicantEmail",
		"owner", "owner_email", "ownerEmail",
		"initiator_id", "initiatorId", "initiator_email", "initiatorEmail",
		"submitted_by", "submittedBy",
	)
	name := firstStringFromMap(data,
		"requester_name", "requesterName",
		"applicant_name", "applicantName",
		"initiator_name", "initiatorName",
		"owner_name", "ownerName",
	)
	dept := firstStringFromMap(data,
		"department_id", "departmentId",
		"applicant_department_id", "applicantDepartmentId",
		"requester_department_id", "requesterDepartmentId",
		"group_id", "groupId",
	)
	// Nested business_payload / form_data / details.
	for _, nestedKey := range []string{"business_payload", "businessPayload", "form_data", "formData", "details"} {
		if nested, ok := data[nestedKey].(map[string]interface{}); ok {
			if applicant == "" {
				applicant = firstStringFromMap(nested,
					"applicant", "applicant_email", "applicantEmail", "requester_id", "requesterId", "email", "owner")
			}
			if name == "" {
				name = firstStringFromMap(nested, "applicant_name", "applicantName", "requester_name", "name")
			}
			if dept == "" {
				dept = firstStringFromMap(nested,
					"department_id", "departmentId", "department", "dept_id", "deptId", "group_id")
			}
		}
	}
	return &ApprovalResolveContext{
		ApplicantID:   applicant,
		ApplicantName: name,
		DepartmentID:  dept,
		InstanceData:  data,
	}
}

func firstStringFromMap(data map[string]interface{}, keys ...string) string {
	if data == nil {
		return ""
	}
	for _, key := range keys {
		if v, ok := data[key]; ok {
			switch s := v.(type) {
			case string:
				if t := strings.TrimSpace(s); t != "" {
					return t
				}
			}
		}
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

func cloneStringAnyMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// normalizeApprovalExecutionMode canonicalizes role/node execution modes.
func normalizeApprovalExecutionMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "manual", "human":
		return "manual"
	case "digital_suggest", "digital-suggest", "suggest":
		return "digital_suggest"
	case "digital_review", "digital-review", "review":
		return "digital_review"
	case "auto", "automatic", "auto_approve":
		return "auto"
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

// applyDigitalTwoPhaseDispatchShape promotes single-approver nodes that resolve
// to multiple identities under digital_suggest/digital_review into sequential
// mode so digital runs first and a human can finalize.
func applyDigitalTwoPhaseDispatchShape(cfg ApprovalNodeConfig) ApprovalNodeConfig {
	mode := normalizeApprovalExecutionMode(cfg.ExecutionMode)
	if mode != "digital_suggest" && mode != "digital_review" {
		return cfg
	}
	if len(cfg.ApproverIDs) < 2 {
		return cfg
	}
	if cfg.Mode != "" && cfg.Mode != ModeSingle {
		// Keep explicit countersign / any-N / sequential as designed.
		if len(cfg.ApproverOrder) == 0 {
			cfg.ApproverOrder = append([]string(nil), cfg.ApproverIDs...)
		}
		return cfg
	}
	cfg.Mode = ModeSequential
	if len(cfg.ApproverOrder) == 0 {
		cfg.ApproverOrder = append([]string(nil), cfg.ApproverIDs...)
	}
	return cfg
}
