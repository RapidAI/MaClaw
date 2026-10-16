package agentservice

import (
	"context"
	"time"
)

func (s *Service) GetTenantSummary(ctx context.Context, tenantID string) (*TenantSummary, error) {
	_ = ctx
	tenant, err := s.store.GetTenant(tenantID)
	if err != nil {
		return nil, err
	}
	users, err := s.store.ListUsers(tenantID)
	if err != nil {
		return nil, err
	}
	summary := TenantSummary{
		TenantID:      tenant.ID,
		Name:          tenant.Name,
		Status:        tenant.Status,
		Quota:         tenant.Quota,
		Users:         len(users),
		RunsByStatus:  map[RunStatus]int{},
		UserSummaries: make([]TenantUserSummary, 0, len(users)),
	}
	for _, user := range users {
		if user.Status == UserStatusDisabled {
			summary.DisabledUsers++
		} else {
			summary.ActiveUsers++
		}
		usage, err := s.buildUsageSummary(tenantID, user.ID)
		if err != nil {
			return nil, err
		}
		effectiveQuota := mergeQuota(tenant.Quota, user.Quota)
		userSummary := TenantUserSummary{
			UserID:               user.ID,
			Name:                 user.Name,
			Email:                user.Email,
			Status:               user.Status,
			DataDir:              usage.DataDir,
			Quota:                user.Quota,
			EffectiveQuota:       effectiveQuota,
			QuotaUsage:           buildQuotaUsageSnapshot(effectiveQuota, usage),
			Instances:            usage.Instances,
			ReadyInstances:       usage.ReadyInstances,
			StoppedInstances:     usage.StoppedInstances,
			Sessions:             usage.Sessions,
			Messages:             usage.Messages,
			UserMessages:         usage.UserMessages,
			AssistantMessages:    usage.AssistantMessages,
			Runs:                 usage.Runs,
			RunsByStatus:         usage.RunsByStatus,
			Credentials:          usage.Credentials,
			ActiveCredentials:    usage.ActiveCredentials,
			SuspendedCredentials: usage.SuspendedCredentials,
			RevokedCredentials:   usage.RevokedCredentials,
			ExpiredCredentials:   usage.ExpiredCredentials,
			ExpiringCredentials:  usage.ExpiringCredentials,
			LastActivityAt:       usage.LastActivityAt,
		}
		summary.UserSummaries = append(summary.UserSummaries, userSummary)
		summary.Credentials += userSummary.Credentials
		summary.ActiveCredentials += userSummary.ActiveCredentials
		summary.SuspendedCredentials += userSummary.SuspendedCredentials
		summary.RevokedCredentials += userSummary.RevokedCredentials
		summary.ExpiredCredentials += userSummary.ExpiredCredentials
		summary.ExpiringCredentials += userSummary.ExpiringCredentials
		summary.Instances += usage.Instances
		summary.ReadyInstances += usage.ReadyInstances
		summary.StoppedInstances += usage.StoppedInstances
		summary.Sessions += usage.Sessions
		summary.Messages += usage.Messages
		summary.UserMessages += usage.UserMessages
		summary.AssistantMessages += usage.AssistantMessages
		summary.Runs += usage.Runs
		if usage.LastActivityAt != nil {
			summary.LastActivityAt = laterTime(summary.LastActivityAt, *usage.LastActivityAt)
		}
		for status, count := range usage.RunsByStatus {
			summary.RunsByStatus[status] += count
		}
	}
	summary.QuotaUsage = buildQuotaUsageSnapshot(tenant.Quota, &UsageSummary{Instances: summary.Instances, Sessions: summary.Sessions, Messages: summary.Messages, Runs: summary.Runs})
	return &summary, nil
}

func (s *Service) buildUsageSummary(tenantID, userID string) (*UsageSummary, error) {
	instances, err := s.store.ListInstances(tenantID, userID)
	if err != nil {
		return nil, err
	}
	tenant, err := s.store.GetTenant(tenantID)
	if err != nil {
		return nil, err
	}
	user, err := s.store.GetUser(tenantID, userID)
	if err != nil {
		return nil, err
	}
	effectiveQuota := mergeQuota(tenant.Quota, user.Quota)
	summary := UsageSummary{
		TenantID:     tenantID,
		UserID:       userID,
		DataDir:      s.userDataRoot(tenantID, userID),
		Quota:        effectiveQuota,
		Instances:    len(instances),
		RunsByStatus: map[RunStatus]int{},
	}
	now := s.now().UTC()
	credentialExpiryCutoff := now.Add(7 * 24 * time.Hour)
	credentials, err := s.store.ListCredentials(tenantID, userID)
	if err != nil {
		return nil, err
	}
	for _, cred := range credentials {
		summary.Credentials++
		switch credentialStatus(cred) {
		case CredentialStatusSuspended:
			summary.SuspendedCredentials++
		case CredentialStatusRevoked:
			summary.RevokedCredentials++
		default:
			summary.ActiveCredentials++
		}
		if cred.ExpiresAt != nil {
			expiresAt := cred.ExpiresAt.UTC()
			if !expiresAt.After(now) {
				summary.ExpiredCredentials++
			} else if !expiresAt.After(credentialExpiryCutoff) {
				summary.ExpiringCredentials++
			}
		}
	}
	for _, inst := range instances {
		inst = s.withInstanceReadiness(inst)
		if inst.Ready {
			summary.ReadyInstances++
		}
		if inst.Status == InstanceStatusStopped {
			summary.StoppedInstances++
		}
		summary.LastActivityAt = laterTime(summary.LastActivityAt, inst.UpdatedAt)

		sessions, err := s.store.ListSessions(tenantID, userID, inst.ID)
		if err != nil {
			return nil, err
		}
		summary.Sessions += len(sessions)
		for _, sess := range sessions {
			summary.LastActivityAt = laterTime(summary.LastActivityAt, sess.UpdatedAt)
			messages, err := s.store.ListMessages(sess.ID)
			if err != nil {
				return nil, err
			}
			summary.Messages += len(messages)
			for _, msg := range messages {
				summary.LastActivityAt = laterTime(summary.LastActivityAt, msg.CreatedAt)
				switch msg.Role {
				case MessageRoleUser:
					summary.UserMessages++
				case MessageRoleAssistant:
					summary.AssistantMessages++
				}
			}
		}

		runs, err := s.store.ListRuns(tenantID, userID, inst.ID)
		if err != nil {
			return nil, err
		}
		summary.Runs += len(runs)
		for _, run := range runs {
			summary.RunsByStatus[run.Status]++
			summary.LastActivityAt = laterTime(summary.LastActivityAt, run.StartedAt)
			if run.CompletedAt != nil {
				summary.LastActivityAt = laterTime(summary.LastActivityAt, *run.CompletedAt)
			}
		}
	}
	summary.QuotaUsage = buildQuotaUsageSnapshot(summary.Quota, &summary)
	return &summary, nil
}

func (s *Service) GetInstanceSummary(ctx context.Context, p Principal, instanceID string) (*InstanceSummary, error) {
	_ = ctx
	inst, err := s.store.GetInstance(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	inst = s.withInstanceReadiness(inst)
	summary := InstanceSummary{
		InstanceID:   inst.ID,
		TenantID:     inst.TenantID,
		UserID:       inst.UserID,
		Status:       inst.Status,
		Ready:        inst.Ready,
		ReadyReason:  inst.ReadyReason,
		RunsByStatus: map[RunStatus]int{},
	}
	summary.LastActivityAt = laterTime(summary.LastActivityAt, inst.UpdatedAt)

	sessions, err := s.store.ListSessions(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	summary.Sessions = len(sessions)
	for _, sess := range sessions {
		if sess.Archived {
			summary.ArchivedSessions++
		}
		if sess.Metadata != nil && sess.Metadata[sessionMetaPendingAskUser] == "true" {
			summary.WaitingSessions++
		}
		summary.LastActivityAt = laterTime(summary.LastActivityAt, sess.UpdatedAt)
		messages, err := s.store.ListMessages(sess.ID)
		if err != nil {
			return nil, err
		}
		summary.Messages += len(messages)
		for _, msg := range messages {
			summary.LastActivityAt = laterTime(summary.LastActivityAt, msg.CreatedAt)
			switch msg.Role {
			case MessageRoleUser:
				summary.UserMessages++
			case MessageRoleAssistant:
				summary.AssistantMessages++
			}
		}
	}

	runs, err := s.store.ListRuns(p.TenantID, p.UserID, instanceID)
	if err != nil {
		return nil, err
	}
	summary.Runs = len(runs)
	for _, run := range runs {
		summary.RunsByStatus[run.Status]++
		if run.WaitingForUser {
			summary.WaitingRuns++
		}
		summary.LastActivityAt = laterTime(summary.LastActivityAt, run.StartedAt)
		if run.CompletedAt != nil {
			summary.LastActivityAt = laterTime(summary.LastActivityAt, *run.CompletedAt)
		}
	}
	return &summary, nil
}
