package agentservice

import (
	"context"
	"fmt"
)

type quotaMetric string

const (
	quotaMetricInstances quotaMetric = "instances"
	quotaMetricSessions  quotaMetric = "sessions"
	quotaMetricMessages  quotaMetric = "messages"
	quotaMetricRuns      quotaMetric = "runs"
)

func applyQuotaUpdate(quota *TenantQuota, maxInstances, maxSessions, maxMessages, maxRuns *int) error {
	if quota == nil {
		return nil
	}
	if maxInstances != nil {
		if *maxInstances < 0 {
			return fmt.Errorf("max_instances must be >= 0")
		}
		quota.MaxInstances = *maxInstances
	}
	if maxSessions != nil {
		if *maxSessions < 0 {
			return fmt.Errorf("max_sessions must be >= 0")
		}
		quota.MaxSessions = *maxSessions
	}
	if maxMessages != nil {
		if *maxMessages < 0 {
			return fmt.Errorf("max_messages must be >= 0")
		}
		quota.MaxMessages = *maxMessages
	}
	if maxRuns != nil {
		if *maxRuns < 0 {
			return fmt.Errorf("max_runs must be >= 0")
		}
		quota.MaxRuns = *maxRuns
	}
	return nil
}

func effectiveQuotaLimit(tenantQuota, userQuota TenantQuota, metric quotaMetric) int {
	tenantLimit := quotaValue(tenantQuota, metric)
	userLimit := quotaValue(userQuota, metric)
	if tenantLimit == 0 {
		return userLimit
	}
	if userLimit == 0 {
		return tenantLimit
	}
	if tenantLimit < userLimit {
		return tenantLimit
	}
	return userLimit
}

func quotaValue(quota TenantQuota, metric quotaMetric) int {
	switch metric {
	case quotaMetricInstances:
		return quota.MaxInstances
	case quotaMetricSessions:
		return quota.MaxSessions
	case quotaMetricMessages:
		return quota.MaxMessages
	case quotaMetricRuns:
		return quota.MaxRuns
	default:
		return 0
	}
}

func (s *Service) enforceQuotaLimit(tenantID, userID string, metric quotaMetric) error {
	tenant, err := s.store.GetTenant(tenantID)
	if err != nil {
		return err
	}
	user, err := s.store.GetUser(tenantID, userID)
	if err != nil {
		return err
	}
	limit := effectiveQuotaLimit(tenant.Quota, user.Quota, metric)
	if limit <= 0 {
		return nil
	}
	count, err := s.currentQuotaUsage(tenantID, userID, metric)
	if err != nil {
		return err
	}
	if count >= limit {
		s.RuntimeMetrics().RecordQuotaRejected(tenantID)
		return fmt.Errorf("%w: %s limit reached (%d)", ErrQuotaExceeded, metric, limit)
	}
	return nil
}

func (s *Service) currentQuotaUsage(tenantID, userID string, metric quotaMetric) (int, error) {
	instances, err := s.store.ListInstances(tenantID, userID)
	if err != nil {
		return 0, err
	}
	switch metric {
	case quotaMetricInstances:
		return len(instances), nil
	case quotaMetricSessions, quotaMetricMessages, quotaMetricRuns:
		count := 0
		for _, inst := range instances {
			sessions, err := s.store.ListSessions(tenantID, userID, inst.ID)
			if err != nil {
				return 0, err
			}
			if metric == quotaMetricSessions {
				count += len(sessions)
				continue
			}
			if metric == quotaMetricRuns {
				runs, err := s.store.ListRuns(tenantID, userID, inst.ID)
				if err != nil {
					return 0, err
				}
				count += len(runs)
				continue
			}
			for _, sess := range sessions {
				messages, err := s.store.ListMessages(sess.ID)
				if err != nil {
					return 0, err
				}
				count += len(messages)
			}
		}
		return count, nil
	default:
		return 0, nil
	}
}

func mergeQuota(tenantQuota, userQuota TenantQuota) TenantQuota {
	return TenantQuota{
		MaxInstances: effectiveQuotaLimit(tenantQuota, userQuota, quotaMetricInstances),
		MaxSessions:  effectiveQuotaLimit(tenantQuota, userQuota, quotaMetricSessions),
		MaxMessages:  effectiveQuotaLimit(tenantQuota, userQuota, quotaMetricMessages),
		MaxRuns:      effectiveQuotaLimit(tenantQuota, userQuota, quotaMetricRuns),
	}
}

func buildQuotaUsageItem(limit, used int) QuotaUsageItem {
	item := QuotaUsageItem{Limit: limit, Used: used}
	if limit <= 0 {
		item.Unlimited = true
		item.Limit = 0
		return item
	}
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	item.Remaining = &remaining
	return item
}

func buildQuotaUsageSnapshot(limit TenantQuota, usage *UsageSummary) QuotaUsageSnapshot {
	if usage == nil {
		usage = &UsageSummary{}
	}
	return QuotaUsageSnapshot{
		Instances: buildQuotaUsageItem(limit.MaxInstances, usage.Instances),
		Sessions:  buildQuotaUsageItem(limit.MaxSessions, usage.Sessions),
		Messages:  buildQuotaUsageItem(limit.MaxMessages, usage.Messages),
		Runs:      buildQuotaUsageItem(limit.MaxRuns, usage.Runs),
	}
}

func (s *Service) GetUsageSummary(ctx context.Context, p Principal) (*UsageSummary, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	return s.buildUsageSummary(p.TenantID, p.UserID)
}
