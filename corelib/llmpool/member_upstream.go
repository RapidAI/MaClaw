package llmpool

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// MemberExtraBodyPriorityMember is the default: member extra_body wins
	// when the downstream request already has the same key.
	MemberExtraBodyPriorityMember = "member"
	// MemberExtraBodyPriorityDownstream keeps a downstream value and only
	// fills keys the request left out.
	MemberExtraBodyPriorityDownstream = "downstream"

	maxMemberExtraBodyBytes = 16 << 10
	maxMemberExtraBodyDepth = 8
	maxMemberDropFields     = 32
	maxMemberDropFieldRunes = 128
)

// MemberUpstreamOverrideError is a rejected extra_body, priority, or drop_fields value.
type MemberUpstreamOverrideError struct {
	Reason string
}

func (e *MemberUpstreamOverrideError) Error() string {
	if e == nil || strings.TrimSpace(e.Reason) == "" {
		return "invalid member upstream overrides"
	}
	return e.Reason
}

func memberUpstreamError(reason string) error {
	return &MemberUpstreamOverrideError{Reason: reason}
}

// NormalizeMemberUpstreamOverrides canonicalizes the optional request-body
// overrides stored on a provider. A nil extra body and a nil drop list mean
// unset. An empty object or an empty list clears the stored value. Priority
// "" and "member" are the default and are stored as "".
func NormalizeMemberUpstreamOverrides(extra map[string]any, priority string, drop []string) (map[string]any, string, []string, error) {
	priority, err := normalizeMemberExtraBodyPriority(priority)
	if err != nil {
		return nil, "", nil, err
	}
	extra, err = normalizeMemberExtraBody(extra)
	if err != nil {
		return nil, "", nil, err
	}
	drop, err = normalizeMemberDropFields(drop)
	if err != nil {
		return nil, "", nil, err
	}
	return extra, priority, drop, nil
}

func normalizeMemberExtraBodyPriority(priority string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "", MemberExtraBodyPriorityMember:
		return "", nil
	case MemberExtraBodyPriorityDownstream:
		return MemberExtraBodyPriorityDownstream, nil
	default:
		return "", memberUpstreamError("extra_body_priority must be member or downstream")
	}
}

func normalizeMemberDropFields(fields []string) ([]string, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	if len(fields) > maxMemberDropFields {
		return nil, memberUpstreamError(fmt.Sprintf("drop_fields accepts at most %d names", maxMemberDropFields))
	}
	out := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			return nil, memberUpstreamError("drop_fields contains an empty name")
		}
		if len([]rune(field)) > maxMemberDropFieldRunes {
			return nil, memberUpstreamError("drop_fields name is too long")
		}
		if memberUpstreamProtectedKey(field) {
			return nil, memberUpstreamError("drop_fields cannot remove " + field)
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func normalizeMemberExtraBody(extra map[string]any) (map[string]any, error) {
	if len(extra) == 0 {
		return nil, nil
	}
	cloned, err := cloneMemberExtraBody(extra, 1)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(cloned)
	if err != nil {
		return nil, memberUpstreamError("extra_body must be a JSON object")
	}
	if len(raw) > maxMemberExtraBodyBytes {
		return nil, memberUpstreamError("extra_body is too large")
	}
	return cloned, nil
}

func cloneMemberExtraBody(extra map[string]any, depth int) (map[string]any, error) {
	if depth > maxMemberExtraBodyDepth {
		return nil, memberUpstreamError("extra_body is nested too deeply")
	}
	out := make(map[string]any, len(extra))
	seen := map[string]struct{}{}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, memberUpstreamError("extra_body contains an empty key")
		}
		if _, ok := seen[key]; ok {
			return nil, memberUpstreamError("extra_body contains a duplicate key")
		}
		seen[key] = struct{}{}
		if depth == 1 && memberUpstreamProtectedKey(key) {
			return nil, memberUpstreamError("extra_body cannot set " + key)
		}
		cloned, err := cloneMemberExtraValue(value, depth)
		if err != nil {
			return nil, err
		}
		out[key] = cloned
	}
	return out, nil
}

func cloneMemberExtraValue(value any, depth int) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMemberExtraBody(typed, depth+1)
	case []any:
		if depth+1 > maxMemberExtraBodyDepth {
			return nil, memberUpstreamError("extra_body is nested too deeply")
		}
		out := make([]any, len(typed))
		for i, item := range typed {
			cloned, err := cloneMemberExtraValue(item, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = cloned
		}
		return out, nil
	case string, bool, nil, float64, json.Number:
		return typed, nil
	case int, int32, int64, uint, uint32, uint64, float32:
		return typed, nil
	default:
		return nil, memberUpstreamError("extra_body contains a value that is not JSON")
	}
}

func memberUpstreamProtectedKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "model", "messages", "stream":
		return true
	default:
		return false
	}
}
