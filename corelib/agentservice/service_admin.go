package agentservice

import (
	"context"
	"sort"
	"strings"
	"time"
)

func (s *Service) GetAdminOverview(ctx context.Context) (*AdminOverview, error) {
	_ = ctx
	tenants, err := s.store.ListTenants()
	if err != nil {
		return nil, err
	}
	overview := &AdminOverview{Tenants: len(tenants), RunsByStatus: map[RunStatus]int{}}
	for _, tenant := range tenants {
		if tenant.Status == TenantStatusDisabled {
			overview.DisabledTenants++
		} else {
			overview.ActiveTenants++
		}
		users, err := s.store.ListUsers(tenant.ID)
		if err != nil {
			return nil, err
		}
		overview.Users += len(users)
		for _, user := range users {
			if user.Status == UserStatusDisabled {
				overview.DisabledUsers++
			} else {
				overview.ActiveUsers++
			}

			usage, err := s.buildUsageSummary(tenant.ID, user.ID)
			if err != nil {
				return nil, err
			}
			overview.Credentials += usage.Credentials
			overview.ActiveCredentials += usage.ActiveCredentials
			overview.SuspendedCredentials += usage.SuspendedCredentials
			overview.RevokedCredentials += usage.RevokedCredentials
			overview.ExpiredCredentials += usage.ExpiredCredentials
			overview.ExpiringCredentials += usage.ExpiringCredentials
			overview.Instances += usage.Instances
			overview.ReadyInstances += usage.ReadyInstances
			overview.StoppedInstances += usage.StoppedInstances
			overview.Sessions += usage.Sessions
			overview.Messages += usage.Messages
			overview.UserMessages += usage.UserMessages
			overview.AssistantMessages += usage.AssistantMessages
			overview.Runs += usage.Runs
			if usage.LastActivityAt != nil {
				overview.LastActivityAt = laterTime(overview.LastActivityAt, *usage.LastActivityAt)
			}
			for status, count := range usage.RunsByStatus {
				overview.RunsByStatus[status] += count
			}
		}
	}
	auditEvents, err := s.store.ListAuditEvents("", "")
	if err != nil {
		return nil, err
	}
	overview.AuditEvents = len(auditEvents)
	snapshots, snapshotBytes, err := s.snapshotStats(ctx)
	if err != nil {
		return nil, err
	}
	overview.Snapshots = snapshots
	overview.SnapshotBytes = snapshotBytes
	for _, event := range auditEvents {
		overview.LastAuditAt = laterTime(overview.LastAuditAt, event.CreatedAt)
	}
	return overview, nil
}

func (s *Service) snapshotStats(ctx context.Context) (int, int64, error) {
	snapshots, err := s.ListServiceSnapshots(ctx, ListServiceSnapshotsInput{})
	if err != nil {
		return 0, 0, err
	}
	var bytes int64
	for _, snapshot := range snapshots {
		bytes += snapshot.SizeBytes
	}
	return len(snapshots), bytes, nil
}

func buildAdminTrendPoints(now time.Time, points int, bucketSize time.Duration) []AdminTrendPoint {
	items := make([]AdminTrendPoint, 0, points)
	start := now.Truncate(bucketSize).Add(-bucketSize * time.Duration(points-1))
	for i := 0; i < points; i++ {
		items = append(items, AdminTrendPoint{BucketStart: start.Add(bucketSize * time.Duration(i)), RunsByStatus: map[RunStatus]int{}})
	}
	return items
}

func addTrendMessage(points []AdminTrendPoint, ts time.Time, bucketSize time.Duration) {
	for i := range points {
		start := points[i].BucketStart
		end := start.Add(bucketSize)
		if (ts.Equal(start) || ts.After(start)) && ts.Before(end) {
			points[i].Messages++
			return
		}
	}
}

func addTrendRun(points []AdminTrendPoint, run Run, bucketSize time.Duration) {
	for i := range points {
		start := points[i].BucketStart
		end := start.Add(bucketSize)
		if (run.StartedAt.Equal(start) || run.StartedAt.After(start)) && run.StartedAt.Before(end) {
			points[i].Runs++
			points[i].RunsByStatus[run.Status]++
			return
		}
	}
}

func addTrendAudit(points []AdminTrendPoint, event AuditEvent, bucketSize time.Duration) {
	for i := range points {
		start := points[i].BucketStart
		end := start.Add(bucketSize)
		if (event.CreatedAt.Equal(start) || event.CreatedAt.After(start)) && event.CreatedAt.Before(end) {
			points[i].AuditEvents++
			return
		}
	}
}

func (s *Service) GetAdminDashboard(ctx context.Context) (*AdminDashboard, error) {
	overview, err := s.GetAdminOverview(ctx)
	if err != nil {
		return nil, err
	}
	auditEvents, err := s.store.ListAuditEvents("", "")
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	last24 := buildAdminTrendPoints(now, 24, time.Hour)
	last7 := buildAdminTrendPoints(now, 7, 24*time.Hour)
	tenants, err := s.store.ListTenants()
	if err != nil {
		return nil, err
	}
	for _, tenant := range tenants {
		users, err := s.store.ListUsers(tenant.ID)
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			instances, err := s.store.ListInstances(tenant.ID, user.ID)
			if err != nil {
				return nil, err
			}
			for _, inst := range instances {
				sessions, err := s.store.ListSessions(tenant.ID, user.ID, inst.ID)
				if err != nil {
					return nil, err
				}
				for _, sess := range sessions {
					messages, err := s.store.ListMessages(sess.ID)
					if err != nil {
						return nil, err
					}
					for _, msg := range messages {
						addTrendMessage(last24, msg.CreatedAt.UTC(), time.Hour)
						addTrendMessage(last7, msg.CreatedAt.UTC(), 24*time.Hour)
					}
				}
				runs, err := s.store.ListRuns(tenant.ID, user.ID, inst.ID)
				if err != nil {
					return nil, err
				}
				for _, run := range runs {
					addTrendRun(last24, run, time.Hour)
					addTrendRun(last7, run, 24*time.Hour)
				}
			}
		}
	}
	for _, event := range auditEvents {
		addTrendAudit(last24, event, time.Hour)
		addTrendAudit(last7, event, 24*time.Hour)
	}
	sort.Slice(auditEvents, func(i, j int) bool { return auditEvents[i].CreatedAt.After(auditEvents[j].CreatedAt) })
	recent := auditEvents
	if len(recent) > 10 {
		recent = recent[:10]
	}
	return &AdminDashboard{
		Overview:          *overview,
		RecentAuditEvents: recent,
		Last24Hours:       last24,
		Last7Days:         last7,
		GeneratedAt:       now,
	}, nil
}

func (s *Service) GetAdminInsights(ctx context.Context, in AdminInsightsInput) (*AdminInsights, error) {
	_ = ctx
	now := s.now().UTC()
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	inactiveForDays := in.InactiveForDays
	if inactiveForDays <= 0 {
		inactiveForDays = 30
	}
	cutoff := now.Add(-time.Duration(inactiveForDays) * 24 * time.Hour)
	insights := &AdminInsights{GeneratedAt: now, InactiveCutoff: cutoff}
	tenants, err := s.store.ListTenants()
	if err != nil {
		return nil, err
	}
	for _, tenant := range tenants {
		summary, err := s.GetTenantSummary(ctx, tenant.ID)
		if err != nil {
			return nil, err
		}
		insights.TopTenants = append(insights.TopTenants, AdminTenantInsight{
			TenantID:       summary.TenantID,
			Name:           summary.Name,
			Status:         summary.Status,
			Users:          summary.Users,
			ActiveUsers:    summary.ActiveUsers,
			Instances:      summary.Instances,
			Messages:       summary.Messages,
			Runs:           summary.Runs,
			ActivityScore:  summary.Messages + summary.Runs,
			LastActivityAt: summary.LastActivityAt,
		})
		appendQuotaPressureInsights(&insights.QuotaPressure, "tenant", summary.TenantID, summary.Name, "", "", summary.QuotaUsage, summary.LastActivityAt)
		for _, user := range summary.UserSummaries {
			inactiveReason, inactiveDays, inactive := classifyInactiveUser(user, cutoff, now)
			if inactive {
				insights.InactiveUsers = append(insights.InactiveUsers, AdminInactiveUserInsight{
					TenantID:       summary.TenantID,
					UserID:         user.UserID,
					Name:           user.Name,
					Email:          user.Email,
					Status:         user.Status,
					Instances:      user.Instances,
					Messages:       user.Messages,
					Runs:           user.Runs,
					LastActivityAt: user.LastActivityAt,
					InactiveDays:   inactiveDays,
					Reason:         inactiveReason,
				})
			}
			appendQuotaPressureInsights(&insights.QuotaPressure, "user", summary.TenantID, summary.Name, user.UserID, user.Name, user.QuotaUsage, user.LastActivityAt)
		}
	}
	sort.Slice(insights.TopTenants, func(i, j int) bool {
		if insights.TopTenants[i].ActivityScore == insights.TopTenants[j].ActivityScore {
			if insights.TopTenants[i].Runs == insights.TopTenants[j].Runs {
				return insights.TopTenants[i].Messages > insights.TopTenants[j].Messages
			}
			return insights.TopTenants[i].Runs > insights.TopTenants[j].Runs
		}
		return insights.TopTenants[i].ActivityScore > insights.TopTenants[j].ActivityScore
	})
	sort.Slice(insights.InactiveUsers, func(i, j int) bool {
		if insights.InactiveUsers[i].InactiveDays == insights.InactiveUsers[j].InactiveDays {
			return insights.InactiveUsers[i].UserID < insights.InactiveUsers[j].UserID
		}
		return insights.InactiveUsers[i].InactiveDays > insights.InactiveUsers[j].InactiveDays
	})
	sort.Slice(insights.QuotaPressure, func(i, j int) bool {
		if insights.QuotaPressure[i].PressureRatio == insights.QuotaPressure[j].PressureRatio {
			return insights.QuotaPressure[i].Used > insights.QuotaPressure[j].Used
		}
		return insights.QuotaPressure[i].PressureRatio > insights.QuotaPressure[j].PressureRatio
	})
	if len(insights.TopTenants) > limit {
		insights.TopTenants = insights.TopTenants[:limit]
	}
	if len(insights.InactiveUsers) > limit {
		insights.InactiveUsers = insights.InactiveUsers[:limit]
	}
	if len(insights.QuotaPressure) > limit {
		insights.QuotaPressure = insights.QuotaPressure[:limit]
	}
	return insights, nil
}

func classifyInactiveUser(user TenantUserSummary, cutoff, now time.Time) (string, int, bool) {
	if user.LastActivityAt == nil {
		return "no recorded activity", 0, true
	}
	if user.LastActivityAt.Before(cutoff) {
		inactiveDays := int(now.Sub(*user.LastActivityAt).Hours() / 24)
		if inactiveDays < 0 {
			inactiveDays = 0
		}
		return "last activity is older than the inactivity cutoff", inactiveDays, true
	}
	return "", 0, false
}

func appendQuotaPressureInsights(items *[]AdminQuotaPressureInsight, scope, tenantID, tenantName, userID, userName string, usage QuotaUsageSnapshot, lastActivityAt *time.Time) {
	appendQuotaPressureMetric(items, scope, "instances", tenantID, tenantName, userID, userName, usage.Instances, lastActivityAt)
	appendQuotaPressureMetric(items, scope, "sessions", tenantID, tenantName, userID, userName, usage.Sessions, lastActivityAt)
	appendQuotaPressureMetric(items, scope, "messages", tenantID, tenantName, userID, userName, usage.Messages, lastActivityAt)
	appendQuotaPressureMetric(items, scope, "runs", tenantID, tenantName, userID, userName, usage.Runs, lastActivityAt)
}

func appendQuotaPressureMetric(items *[]AdminQuotaPressureInsight, scope, metric, tenantID, tenantName, userID, userName string, usage QuotaUsageItem, lastActivityAt *time.Time) {
	if usage.Unlimited || usage.Limit <= 0 {
		return
	}
	ratio := 0.0
	if usage.Limit > 0 {
		ratio = float64(usage.Used) / float64(usage.Limit)
	}
	if ratio < 0.8 {
		return
	}
	status := "high"
	if ratio >= 1.0 {
		status = "exceeded"
	} else if ratio >= 0.9 {
		status = "critical"
	}
	*items = append(*items, AdminQuotaPressureInsight{
		Scope:          scope,
		Metric:         metric,
		TenantID:       tenantID,
		TenantName:     tenantName,
		UserID:         userID,
		UserName:       userName,
		Limit:          usage.Limit,
		Used:           usage.Used,
		Remaining:      usage.Remaining,
		PressureRatio:  ratio,
		Status:         status,
		LastActivityAt: lastActivityAt,
	})
}

func (s *Service) GetAdminAlerts(ctx context.Context, in AdminAlertsInput) (*AdminAlerts, error) {
	_ = ctx
	now := s.now().UTC()
	alerts := &AdminAlerts{GeneratedAt: now}
	expiryWindowDays := in.CredentialExpiryWindowDays
	if expiryWindowDays <= 0 {
		expiryWindowDays = 7
	}
	if expiryWindowDays > 365 {
		expiryWindowDays = 365
	}
	credentialExpiryCutoff := now.Add(time.Duration(expiryWindowDays) * 24 * time.Hour)
	tenants, err := s.store.ListTenants()
	if err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(in.Kind)
	for _, tenant := range tenants {
		if strings.TrimSpace(in.TenantID) != "" && tenant.ID != strings.TrimSpace(in.TenantID) {
			continue
		}
		users, err := s.store.ListUsers(tenant.ID)
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			if strings.TrimSpace(in.UserID) != "" && user.ID != strings.TrimSpace(in.UserID) {
				continue
			}
			credentials, err := s.store.ListCredentials(tenant.ID, user.ID)
			if err != nil {
				return nil, err
			}
			for _, cred := range credentials {
				if cred.ExpiresAt == nil {
					continue
				}
				expiresAt := cred.ExpiresAt.UTC()
				if in.Since != nil && expiresAt.Before(*in.Since) {
					continue
				}
				alertKind := ""
				severity := "medium"
				title := "Credential expiring"
				reason := "credential expires soon"
				if !expiresAt.After(now) {
					alertKind = "credential_expired"
					severity = "high"
					title = "Credential expired"
					reason = "credential has expired"
				} else if !expiresAt.After(credentialExpiryCutoff) {
					alertKind = "credential_expiring"
				} else {
					continue
				}
				if kind != "" && kind != alertKind {
					continue
				}
				sanitized := sanitizeCredential(cred)
				alerts.CredentialAlerts = append(alerts.CredentialAlerts, sanitized)
				occurredAt := expiresAt
				alerts.Items = append(alerts.Items, AdminAlertItem{
					Kind:            alertKind,
					Severity:        severity,
					Title:           title,
					SuggestedAction: "Rotate the credential or extend expires_at before clients lose access.",
					TenantID:        cred.TenantID,
					UserID:          cred.UserID,
					CredentialID:    cred.ID,
					OccurredAt:      &occurredAt,
					Reason:          reason,
				})
			}
			instances, err := s.store.ListInstances(tenant.ID, user.ID)
			if err != nil {
				return nil, err
			}
			for _, inst := range instances {
				inst = s.withInstanceReadiness(inst)
				if !inst.Ready && (kind == "" || kind == "unready_instance") {
					if in.Since == nil || !inst.UpdatedAt.Before(*in.Since) {
						alerts.UnreadyInstances = append(alerts.UnreadyInstances, inst)
						occurredAt := inst.UpdatedAt
						alerts.Items = append(alerts.Items, AdminAlertItem{
							Kind:            "unready_instance",
							Severity:        "high",
							Title:           "Instance not ready",
							SuggestedAction: "Review user config and refresh instance readiness.",
							TenantID:        inst.TenantID,
							UserID:          inst.UserID,
							InstanceID:      inst.ID,
							OccurredAt:      &occurredAt,
							Reason:          inst.ReadyReason,
						})
					}
				}
				runs, err := s.store.ListRuns(tenant.ID, user.ID, inst.ID)
				if err != nil {
					return nil, err
				}
				enrichedRuns, err := s.enrichRuns(runs)
				if err != nil {
					return nil, err
				}
				for _, run := range enrichedRuns {
					if in.Since != nil && run.StartedAt.Before(*in.Since) {
						continue
					}
					if run.WaitingForUser && (kind == "" || kind == "waiting_run") {
						alerts.WaitingRuns = append(alerts.WaitingRuns, run)
						occurredAt := run.StartedAt
						alerts.Items = append(alerts.Items, AdminAlertItem{
							Kind:            "waiting_run",
							Severity:        "medium",
							Title:           "Run waiting for user input",
							SuggestedAction: "Prompt the user to answer the pending question and resume the session.",
							TenantID:        run.TenantID,
							UserID:          run.UserID,
							InstanceID:      run.InstanceID,
							SessionID:       run.SessionID,
							RunID:           run.ID,
							OccurredAt:      &occurredAt,
							Reason:          "run is waiting for user input",
						})
					}
					if run.Status == RunStatusFailed && (kind == "" || kind == "failed_run") {
						alerts.FailedRuns = append(alerts.FailedRuns, run)
						occurredAt := run.StartedAt
						alerts.Items = append(alerts.Items, AdminAlertItem{
							Kind:            "failed_run",
							Severity:        "high",
							Title:           "Run failed",
							SuggestedAction: "Inspect the run error and retry after fixing configuration or inputs.",
							TenantID:        run.TenantID,
							UserID:          run.UserID,
							InstanceID:      run.InstanceID,
							SessionID:       run.SessionID,
							RunID:           run.ID,
							OccurredAt:      &occurredAt,
							Reason:          run.Error,
						})
					}
				}
			}
		}
	}
	sort.Slice(alerts.UnreadyInstances, func(i, j int) bool {
		return alerts.UnreadyInstances[i].UpdatedAt.After(alerts.UnreadyInstances[j].UpdatedAt)
	})
	sort.Slice(alerts.WaitingRuns, func(i, j int) bool { return alerts.WaitingRuns[i].StartedAt.After(alerts.WaitingRuns[j].StartedAt) })
	sort.Slice(alerts.FailedRuns, func(i, j int) bool { return alerts.FailedRuns[i].StartedAt.After(alerts.FailedRuns[j].StartedAt) })
	sort.Slice(alerts.CredentialAlerts, func(i, j int) bool {
		if alerts.CredentialAlerts[i].ExpiresAt == nil {
			return false
		}
		if alerts.CredentialAlerts[j].ExpiresAt == nil {
			return true
		}
		return alerts.CredentialAlerts[i].ExpiresAt.Before(*alerts.CredentialAlerts[j].ExpiresAt)
	})
	sort.Slice(alerts.Items, func(i, j int) bool {
		if alerts.Items[i].OccurredAt == nil {
			return false
		}
		if alerts.Items[j].OccurredAt == nil {
			return true
		}
		return alerts.Items[i].OccurredAt.After(*alerts.Items[j].OccurredAt)
	})
	if in.Limit > 0 && len(alerts.Items) > in.Limit {
		alerts.Items = alerts.Items[:in.Limit]
	}
	return alerts, nil
}
