package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	URLDomainActionAllow = "allow"
	URLDomainActionBlock = "block"
)

func (s *SQLiteStore) ListURLDomainPolicies(ctx context.Context) ([]URLDomainPolicy, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT domain, action, COALESCE(reason, ''), created_at, updated_at
		FROM knowledge_url_domain_policies ORDER BY action DESC, domain ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	policies := make([]URLDomainPolicy, 0)
	for rows.Next() {
		policy, err := scanURLDomainPolicy(rows)
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}

func (s *SQLiteStore) UpdateURLDomainPolicies(ctx context.Context, req URLDomainPolicyUpdateRequest) (URLDomainPolicyUpdateResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return URLDomainPolicyUpdateResult{}, err
	}
	defer tx.Rollback()
	result := URLDomainPolicyUpdateResult{}
	if req.Replace {
		res, err := tx.ExecContext(ctx, `DELETE FROM knowledge_url_domain_policies`)
		if err != nil {
			return result, err
		}
		if deleted, err := res.RowsAffected(); err == nil {
			result.Deleted = int(deleted)
		}
	}
	now := time.Now().UTC()
	for _, domain := range splitURLPolicyDomains(req.AllowDomains) {
		policy, ok := normalizeURLDomainPolicy(domain, URLDomainActionAllow, req.Reason, now)
		if ok {
			if err := upsertURLDomainPolicy(ctx, tx, policy); err != nil {
				return result, err
			}
			result.Updated++
		}
	}
	for _, domain := range splitURLPolicyDomains(req.BlockDomains) {
		policy, ok := normalizeURLDomainPolicy(domain, URLDomainActionBlock, req.Reason, now)
		if ok {
			if err := upsertURLDomainPolicy(ctx, tx, policy); err != nil {
				return result, err
			}
			result.Updated++
		}
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	policies, err := s.ListURLDomainPolicies(ctx)
	if err != nil {
		return result, err
	}
	result.Policies = policies
	return result, nil
}

func (s *SQLiteStore) CheckURLDomainPolicy(ctx context.Context, rawURL string) (URLDomainPolicyCheck, error) {
	u, err := ValidatePublicHTTPURL(rawURL)
	if err != nil {
		return URLDomainPolicyCheck{URL: strings.TrimSpace(rawURL), Allowed: false, Reason: err.Error()}, err
	}
	check := URLDomainPolicyCheck{URL: u.String(), Host: strings.ToLower(u.Hostname()), Allowed: true}
	policies, err := s.ListURLDomainPolicies(ctx)
	if err != nil {
		return check, err
	}
	matched := bestURLDomainPolicyMatch(check.Host, policies)
	if matched != nil {
		check.MatchedPolicy = matched
		check.Allowed = matched.Action != URLDomainActionBlock
		if check.Allowed {
			check.Reason = "matched allow policy"
		} else {
			check.Reason = firstNonEmpty(matched.Reason, "matched block policy")
		}
		return check, nil
	}
	if hasURLDomainAllowPolicies(policies) {
		check.Allowed = false
		check.Reason = "no allow policy matched"
	}
	return check, nil
}

func enforceURLDomainPolicy(ctx context.Context, s *SQLiteStore, rawURL string) error {
	check, err := s.CheckURLDomainPolicy(ctx, rawURL)
	if err != nil {
		return err
	}
	if !check.Allowed {
		return fmt.Errorf("URL host %q is blocked by knowledge domain policy: %s", check.Host, check.Reason)
	}
	return nil
}

func scanURLDomainPolicy(scanner interface {
	Scan(dest ...interface{}) error
}) (URLDomainPolicy, error) {
	var policy URLDomainPolicy
	var createdAt, updatedAt string
	if err := scanner.Scan(&policy.Domain, &policy.Action, &policy.Reason, &createdAt, &updatedAt); err != nil {
		return policy, err
	}
	policy.CreatedAt = parseTime(createdAt)
	policy.UpdatedAt = parseTime(updatedAt)
	return policy, nil
}

func upsertURLDomainPolicy(ctx context.Context, tx *sql.Tx, policy URLDomainPolicy) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO knowledge_url_domain_policies(domain, action, reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(domain) DO UPDATE SET action = excluded.action, reason = excluded.reason, updated_at = excluded.updated_at`,
		policy.Domain, policy.Action, policy.Reason, formatTime(policy.CreatedAt), formatTime(policy.UpdatedAt))
	return err
}

func splitURLPolicyDomains(values []string) []string {
	seen := make(map[string]struct{})
	domains := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, isKnowledgeListSeparator) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			key := strings.ToLower(part)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			domains = append(domains, part)
		}
	}
	return domains
}

func normalizeURLDomainPolicy(domain, action, reason string, now time.Time) (URLDomainPolicy, bool) {
	domain = normalizeURLPolicyDomain(domain)
	if domain == "" || IsBlockedHost(domain) {
		return URLDomainPolicy{}, false
	}
	action = strings.ToLower(strings.TrimSpace(action))
	if action != URLDomainActionAllow && action != URLDomainActionBlock {
		return URLDomainPolicy{}, false
	}
	return URLDomainPolicy{
		Domain:    domain,
		Action:    action,
		Reason:    strings.TrimSpace(reason),
		CreatedAt: now,
		UpdatedAt: now,
	}, true
}

func normalizeURLPolicyDomain(domain string) string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return ""
	}
	if strings.Contains(domain, "://") {
		if u, err := url.Parse(domain); err == nil {
			domain = u.Hostname()
		}
	} else if u, err := url.Parse("https://" + domain); err == nil && u.Hostname() != "" {
		domain = u.Hostname()
	}
	domain = strings.TrimSpace(domain)
	domain = strings.TrimPrefix(domain, "*.")
	domain = strings.Trim(domain, ".[]")
	if idx := strings.IndexAny(domain, "/?#:"); idx >= 0 {
		domain = domain[:idx]
	}
	return domain
}

func bestURLDomainPolicyMatch(host string, policies []URLDomainPolicy) *URLDomainPolicy {
	host = strings.ToLower(strings.Trim(host, ".[]"))
	blockMatches := make([]URLDomainPolicy, 0)
	allowMatches := make([]URLDomainPolicy, 0)
	for _, policy := range policies {
		domain := normalizeURLPolicyDomain(policy.Domain)
		if domain == "" {
			continue
		}
		if host == domain || strings.HasSuffix(host, "."+domain) {
			if policy.Action == URLDomainActionBlock {
				blockMatches = append(blockMatches, policy)
			} else {
				allowMatches = append(allowMatches, policy)
			}
		}
	}
	if len(blockMatches) > 0 {
		sort.SliceStable(blockMatches, func(i, j int) bool {
			return len(blockMatches[i].Domain) > len(blockMatches[j].Domain)
		})
		return &blockMatches[0]
	}
	if len(allowMatches) == 0 {
		return nil
	}
	sort.SliceStable(allowMatches, func(i, j int) bool {
		return len(allowMatches[i].Domain) > len(allowMatches[j].Domain)
	})
	return &allowMatches[0]
}

func hasURLDomainAllowPolicies(policies []URLDomainPolicy) bool {
	for _, policy := range policies {
		if policy.Action == URLDomainActionAllow {
			return true
		}
	}
	return false
}
