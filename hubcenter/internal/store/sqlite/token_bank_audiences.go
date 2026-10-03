package sqlite

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

// TokenBankAudienceChoice is one hub or tenant the signed-in account may name
// on a private share. Name is the display label; ID is what the allow-list stores.
type TokenBankAudienceChoice struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	HubID   string `json:"hub_id,omitempty"`
	HubName string `json:"hub_name,omitempty"`
}

// TokenBankShareAudiences is the pick-list for a private share. Hubs and
// tenants are the caller's own links, plus every tenant recorded on those hubs.
type TokenBankShareAudiences struct {
	Hubs    []TokenBankAudienceChoice `json:"hubs"`
	Tenants []TokenBankAudienceChoice `json:"tenants"`
}

// ListShareAudiences returns the hubs and tenants this user can grant. Another
// account's links are not included. A deployment without the hub tables returns
// an empty list rather than failing the share dialog.
func (r *TokenBankRepo) ListShareAudiences(ctx context.Context, userID string) (TokenBankShareAudiences, error) {
	empty := TokenBankShareAudiences{Hubs: []TokenBankAudienceChoice{}, Tenants: []TokenBankAudienceChoice{}}
	userID = strings.TrimSpace(userID)
	if r == nil || r.read == nil || userID == "" {
		return empty, nil
	}
	rows, err := r.read.QueryContext(ctx, `
		SELECT l.hub_id,
		       COALESCE(h.name, ''),
		       COALESCE(l.tenant_id, ''),
		       COALESCE(h.registration_policy_json, '{}'),
		       COALESCE(h.capabilities_json, '{}')
		  FROM hub_user_links l
		  JOIN sm_users u ON lower(u.email) = lower(l.email)
		  LEFT JOIN hub_instances h ON h.id = l.hub_id
		 WHERE u.id = ?
		 ORDER BY l.hub_id, l.tenant_id`, userID)
	if err != nil {
		if isMissingRelationError(err) {
			return empty, nil
		}
		return empty, err
	}
	defer rows.Close()

	type linkRow struct {
		hubID, hubName, tenantID, policy, caps string
	}
	var links []linkRow
	for rows.Next() {
		var row linkRow
		if err := rows.Scan(&row.hubID, &row.hubName, &row.tenantID, &row.policy, &row.caps); err != nil {
			return empty, err
		}
		row.hubID = strings.TrimSpace(row.hubID)
		row.hubName = strings.TrimSpace(row.hubName)
		row.tenantID = strings.TrimSpace(row.tenantID)
		if row.hubID == "" {
			continue
		}
		links = append(links, row)
	}
	if err := rows.Err(); err != nil {
		return empty, err
	}

	hubs := map[string]TokenBankAudienceChoice{}
	tenants := map[string]TokenBankAudienceChoice{}
	rememberTenant := func(hubID, hubName, tenantID, name string) {
		tenantID = strings.TrimSpace(tenantID)
		if tenantID == "" {
			return
		}
		key := strings.ToLower(tenantID)
		hubID = strings.TrimSpace(hubID)
		hubName = strings.TrimSpace(hubName)
		name = strings.TrimSpace(name)
		if existing, ok := tenants[key]; ok {
			if existing.Name == "" && name != "" {
				existing.Name = name
			}
			// A tenant-only allow matches this tenant on every hub. Once the
			// same id shows up on two hubs, drop the single-hub label so the
			// picker does not present the first hub as the grant boundary.
			if existing.HubID != "" && hubID != "" && !strings.EqualFold(existing.HubID, hubID) {
				existing.HubID = ""
				existing.HubName = ""
				tenants[key] = existing
				return
			}
			if existing.HubID != "" && strings.EqualFold(existing.HubID, hubID) && existing.HubName == "" && hubName != "" {
				existing.HubName = hubName
				existing.HubID = hubID
			}
			tenants[key] = existing
			return
		}
		tenants[key] = TokenBankAudienceChoice{ID: tenantID, Name: name, HubID: hubID, HubName: hubName}
	}
	for _, row := range links {
		key := strings.ToLower(row.hubID)
		if existing, ok := hubs[key]; ok {
			if existing.Name == "" && row.hubName != "" {
				existing.Name = row.hubName
				// The name comes from hub_instances, so keep that row's id.
				existing.ID = row.hubID
				hubs[key] = existing
			}
		} else {
			hubs[key] = TokenBankAudienceChoice{ID: row.hubID, Name: row.hubName}
		}
		policyNames := tokenBankPolicyTenantNames(row.policy)
		capNames := tokenBankCapabilityTenantNames(row.caps)
		if row.tenantID != "" {
			rememberTenant(row.hubID, row.hubName, row.tenantID, firstNonEmpty(foldedMapValue(policyNames, row.tenantID), foldedMapValue(capNames, row.tenantID)))
		}
		seen := map[string]struct{}{}
		for id, name := range policyNames {
			seen[id] = struct{}{}
			rememberTenant(row.hubID, row.hubName, id, firstNonEmpty(name, capNames[id]))
		}
		for id, name := range capNames {
			if _, ok := seen[id]; ok {
				continue
			}
			rememberTenant(row.hubID, row.hubName, id, name)
		}
	}

	empty.Hubs = sortedAudienceChoices(hubs)
	empty.Tenants = sortedAudienceChoices(tenants)
	return empty, nil
}

func foldedMapValue(items map[string]string, id string) string {
	if len(items) == 0 {
		return ""
	}
	if value, ok := items[id]; ok {
		return value
	}
	for key, value := range items {
		if strings.EqualFold(key, id) {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func tokenBankPolicyTenantNames(raw string) map[string]string {
	var policy struct {
		Tenants map[string]struct {
			TenantName string `json:"tenant_name"`
		} `json:"tenants"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &policy) != nil || len(policy.Tenants) == 0 {
		return nil
	}
	out := make(map[string]string, len(policy.Tenants))
	for id, item := range policy.Tenants {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out[id] = strings.TrimSpace(item.TenantName)
	}
	return out
}

func tokenBankCapabilityTenantNames(raw string) map[string]string {
	var caps struct {
		TenantNames map[string]string `json:"tenant_names"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &caps) != nil || len(caps.TenantNames) == 0 {
		return nil
	}
	out := make(map[string]string, len(caps.TenantNames))
	for id, name := range caps.TenantNames {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out[id] = strings.TrimSpace(name)
	}
	return out
}

func sortedAudienceChoices(items map[string]TokenBankAudienceChoice) []TokenBankAudienceChoice {
	out := make([]TokenBankAudienceChoice, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		left := strings.ToLower(firstNonEmpty(out[i].Name, out[i].ID))
		right := strings.ToLower(firstNonEmpty(out[j].Name, out[j].ID))
		if left != right {
			return left < right
		}
		return out[i].ID < out[j].ID
	})
	return out
}
