package structureddata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

func (s *Service) GovernanceEvidencePack(ctx context.Context, p Principal, in GovernanceEvidencePackInput) (*GovernanceEvidencePack, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	out := &GovernanceEvidencePack{
		TenantID:   p.TenantID,
		ExportedAt: s.now().UTC(),
		GeneratedBy: GovernanceEvidenceActor{
			UserID:   p.UserID,
			Role:     p.Role,
			APIKeyID: p.APIKeyID,
		},
		Sections: []GovernanceEvidenceSection{},
	}
	add := func(name string, load func() (any, error)) {
		data, err := load()
		section := GovernanceEvidenceSection{Name: name, OK: err == nil}
		if err != nil {
			section.Error = err.Error()
		} else {
			section.Data = data
		}
		out.Sections = append(out.Sections, section)
	}
	add("service_stats", func() (any, error) {
		return s.SystemStats(ctx, p)
	})
	add("access_review", func() (any, error) {
		return s.ReviewAccess(ctx, p, AccessReviewInput{MinSeverity: in.MinSeverity})
	})
	add("access_remediation_plan", func() (any, error) {
		return s.PlanAccessRemediation(ctx, p, AccessRemediationPlanInput{MinSeverity: in.MinSeverity})
	})
	add("recent_audit", func() (any, error) {
		items, err := s.QueryAuditLogs(ctx, p, QueryAuditLogsInput{Limit: 20})
		if err != nil {
			return nil, err
		}
		return ListResponse[AuditLog]{Items: items, Limit: 20, HasMore: len(items) == 20}, nil
	})
	add("work_queue", func() (any, error) {
		return s.MISInboxSummary(ctx, p, QueryMISInboxInput{Limit: 100})
	})
	add("connector_health", func() (any, error) {
		return s.ListConnectorHealth(ctx, p, QueryExternalConnectorsInput{Limit: 100})
	})
	out.Summary = summarizeGovernanceEvidence(out.Sections)
	out.EvidenceID, out.EvidenceSHA256 = governanceEvidenceDigest(out)
	out.SummaryText = governanceEvidenceSummaryText(out, in.Lang)
	s.audit(ctx, p, "governance.evidence_pack_export", "", "governance", "evidence_pack", "Exported governance evidence pack", governanceEvidenceAuditMetadata(out, in))
	return out, nil
}

func governanceEvidenceAuditMetadata(pack *GovernanceEvidencePack, in GovernanceEvidencePackInput) map[string]any {
	metadata := map[string]any{
		"section_count":        0,
		"min_severity":         in.MinSeverity,
		"lang":                 governanceEvidenceLang(in.Lang),
		"evidence_id":          "",
		"evidence_sha256":      "",
		"status":               "",
		"risk_level":           "",
		"failed_sections":      0,
		"control_failures":     0,
		"control_warnings":     0,
		"recommendation_count": 0,
	}
	if pack == nil {
		return metadata
	}
	metadata["section_count"] = len(pack.Sections)
	metadata["evidence_id"] = pack.EvidenceID
	metadata["evidence_sha256"] = pack.EvidenceSHA256
	metadata["status"] = pack.Summary.Status
	metadata["risk_level"] = pack.Summary.RiskLevel
	metadata["failed_sections"] = pack.Summary.FailedSections
	metadata["control_failures"] = pack.Summary.ControlFailures
	metadata["control_warnings"] = pack.Summary.ControlWarnings
	metadata["recommendation_count"] = len(pack.Summary.Recommendations)
	metadata["backup_count"] = pack.Summary.BackupCount
	metadata["managed_keys"] = pack.Summary.ManagedKeys
	metadata["audit_items"] = pack.Summary.AuditItems
	return metadata
}

func governanceEvidenceDigest(pack *GovernanceEvidencePack) (string, string) {
	if pack == nil {
		return "", ""
	}
	copyPack := *pack
	copyPack.EvidenceID = ""
	copyPack.EvidenceSHA256 = ""
	copyPack.SummaryText = ""
	data, err := json.Marshal(copyPack)
	if err != nil {
		data = []byte(pack.TenantID + "|" + pack.ExportedAt.UTC().Format("2006-01-02T15:04:05Z07:00"))
	}
	sum := sha256.Sum256(data)
	encoded := hex.EncodeToString(sum[:])
	return "gev_" + encoded[:16], encoded
}

func governanceEvidenceSummaryText(pack *GovernanceEvidencePack, lang string) string {
	if pack == nil {
		return ""
	}
	if governanceEvidenceLang(lang) == "zh" {
		return governanceEvidenceSummaryTextZH(pack)
	}
	summary := pack.Summary
	lines := []string{
		"MaClawDataSrv governance evidence summary",
		"Evidence ID: " + pack.EvidenceID,
		"Evidence SHA256: " + pack.EvidenceSHA256,
		"Tenant: " + pack.TenantID,
		"Exported at: " + pack.ExportedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		"Status: " + summary.Status,
		"Risk level: " + summary.RiskLevel,
		"Sections: " + strconv.Itoa(summary.OKSections) + "/" + strconv.Itoa(summary.SectionCount) + " ok",
		"Controls: " + strconv.Itoa(summary.ControlFailures) + " fail / " + strconv.Itoa(summary.ControlWarnings) + " warn",
	}
	if len(summary.Controls) > 0 {
		lines = append(lines, "", "Controls:")
		for _, item := range summary.Controls {
			label := item.ID
			if label == "" {
				label = item.Title
			}
			line := "- " + label + ": " + item.Status
			if item.Detail != "" {
				line += " (" + item.Detail + ")"
			}
			if item.RecommendedAction != "" {
				line += " -> " + item.RecommendedAction
			}
			lines = append(lines, line)
		}
	}
	if len(summary.Recommendations) > 0 {
		lines = append(lines, "", "Recommendations:")
		for _, item := range summary.Recommendations {
			lines = append(lines, "- "+item)
		}
	}
	return strings.Join(lines, "\n")
}

func governanceEvidenceSummaryTextZH(pack *GovernanceEvidencePack) string {
	summary := pack.Summary
	lines := []string{
		"MaClawDataSrv 治理证据摘要",
		"证据 ID: " + pack.EvidenceID,
		"证据 SHA256: " + pack.EvidenceSHA256,
		"租户: " + pack.TenantID,
		"导出时间: " + pack.ExportedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		"状态: " + governanceEvidenceZH(summary.Status),
		"风险等级: " + governanceEvidenceZH(summary.RiskLevel),
		"证据分区: " + strconv.Itoa(summary.OKSections) + "/" + strconv.Itoa(summary.SectionCount) + " 正常",
		"控制项: " + strconv.Itoa(summary.ControlFailures) + " 失败 / " + strconv.Itoa(summary.ControlWarnings) + " 警告",
	}
	if len(summary.Controls) > 0 {
		lines = append(lines, "", "控制项:")
		for _, item := range summary.Controls {
			label := item.ID
			if label == "" {
				label = item.Title
			}
			line := "- " + label + ": " + governanceEvidenceZH(item.Status)
			if item.Detail != "" {
				line += " (" + governanceEvidenceZHDetail(item.Detail) + ")"
			}
			if item.RecommendedAction != "" {
				line += " -> " + governanceEvidenceZH(item.RecommendedAction)
			}
			lines = append(lines, line)
		}
	}
	if len(summary.Recommendations) > 0 {
		lines = append(lines, "", "建议:")
		for _, item := range summary.Recommendations {
			lines = append(lines, "- "+governanceEvidenceZH(item))
		}
	}
	return strings.Join(lines, "\n")
}

func governanceEvidenceLang(lang string) string {
	normalized := strings.ToLower(strings.TrimSpace(lang))
	if normalized == "zh" || normalized == "zh-cn" || normalized == "cn" || strings.HasPrefix(normalized, "zh_") || strings.HasPrefix(normalized, "zh-") {
		return "zh"
	}
	return "en"
}

func governanceEvidenceZH(text string) string {
	switch text {
	case "ready":
		return "就绪"
	case "needs_review":
		return "需要复核"
	case "blocked":
		return "阻塞"
	case "critical":
		return "严重"
	case "high":
		return "高"
	case "medium":
		return "中"
	case "low":
		return "低"
	case "pass":
		return "通过"
	case "warn":
		return "警告"
	case "fail":
		return "失败"
	case "Review or create recovery points":
		return "复核或创建恢复点"
	case "Create backup":
		return "创建备份"
	case "Review access":
		return "复核授权"
	case "Create scoped key":
		return "创建范围化密钥"
	case "Review scoped keys":
		return "复核范围化密钥"
	case "Review audit":
		return "查看审计"
	case "Run audit-producing check":
		return "运行可产生审计记录的检查"
	case "Review work queue":
		return "查看待办队列"
	case "Open inbox":
		return "打开待办"
	case "Review inbox":
		return "查看待办"
	case "Clear critical work":
		return "处理严重待办"
	case "Clear high or overdue work":
		return "处理高优先级或逾期待办"
	case "Review connectors":
		return "查看连接器"
	case "Open connectors":
		return "打开连接器"
	case "Fix connector issues":
		return "修复连接器问题"
	case "Re-run the evidence pack with a data_admin key and inspect failed sections before handoff.":
		return "使用 data_admin 密钥重新生成证据包，并在交付前检查失败分区。"
	case "Create a verified backup before production rollout, imports, bulk cleanup, or schema changes.":
		return "在生产上线、导入、批量清理或结构变更前创建可验证备份。"
	case "Create scoped API keys for agents and employees instead of relying on only the root token.":
		return "为 agent 和员工创建范围化 API key，不要只依赖 root token。"
	case "Review high-risk API keys and apply the remediation plan before production rollout.":
		return "在生产上线前复核高风险 API key，并应用整改计划。"
	case "Execute or explicitly waive remediation actions with an audit note.":
		return "执行整改动作，或带审计备注明确豁免。"
	case "Clear critical, high, or overdue MIS inbox items before declaring the service operationally ready.":
		return "在宣布服务可运营前，处理严重、高优先级或逾期的 MIS 待办。"
	case "Fix degraded connectors and retry dead-letter or failed sync work before external system handoff.":
		return "在外部系统交接前修复降级连接器，并重试死信或失败同步任务。"
	case "Generate an auditable activity trail by running a controlled backup, quality check, or access review.":
		return "运行受控备份、质量检查或授权复核，生成可审计活动轨迹。"
	case "Governance evidence looks ready for normal operations; archive this pack with the rollout or audit record.":
		return "治理证据已适合正常运营；请将此证据包归档到上线或审计记录。"
	default:
		return text
	}
}

func governanceEvidenceZHDetail(text string) string {
	switch text {
	case "no backups available":
		return "没有可用备份"
	case "high-risk access findings require review":
		return "高风险授权发现需要复核"
	case "no managed API keys configured":
		return "未配置托管 API key"
	case "no audit activity found":
		return "未发现审计活动"
	case "critical work items require action":
		return "严重待办需要处理"
	case "critical_work_items=":
		return "严重待办="
	case "high or overdue work items require review":
		return "高优先级或逾期待办需要复核"
	case "connector issues require review":
		return "连接器问题需要复核"
	}
	if strings.HasPrefix(text, "critical_work_items=") {
		return "严重待办=" + strings.TrimPrefix(text, "critical_work_items=")
	}
	if strings.HasPrefix(text, "high_work_items=") {
		replaced := strings.ReplaceAll(text, "high_work_items=", "高优先级待办=")
		replaced = strings.ReplaceAll(replaced, "overdue_work_items=", "逾期待办=")
		return replaced
	}
	if strings.HasPrefix(text, "connector_issues=") {
		return "连接器问题=" + strings.TrimPrefix(text, "connector_issues=")
	}
	if strings.HasPrefix(text, "backup_count=") {
		return "备份数=" + strings.TrimPrefix(text, "backup_count=")
	}
	if strings.HasPrefix(text, "managed_keys=") {
		return "托管密钥数=" + strings.TrimPrefix(text, "managed_keys=")
	}
	if strings.HasPrefix(text, "audit_items=") {
		return "审计条数=" + strings.TrimPrefix(text, "audit_items=")
	}
	if strings.HasPrefix(text, "open_work_items=") {
		return "待办数=" + strings.TrimPrefix(text, "open_work_items=")
	}
	if strings.HasPrefix(text, "connectors=") {
		return "连接器=" + strings.TrimPrefix(text, "connectors=")
	}
	return text
}

func summarizeGovernanceEvidence(sections []GovernanceEvidenceSection) GovernanceEvidenceSummary {
	out := GovernanceEvidenceSummary{
		SectionCount:     len(sections),
		AccessBySeverity: map[string]int{},
	}
	for _, section := range sections {
		if section.OK {
			out.OKSections++
		} else {
			out.FailedSections++
			continue
		}
		switch section.Name {
		case "service_stats":
			if stats, ok := section.Data.(*SystemStats); ok && stats != nil {
				out.BackupCount = stats.BackupCount
				if out.AuditItems == 0 {
					out.AuditItems = stats.AuditLogCount
				}
			}
		case "access_review":
			if review, ok := section.Data.(*AccessReviewResult); ok && review != nil {
				out.ManagedKeys = review.Total
				out.AccessFindings = review.Filtered
				for severity, count := range review.BySeverity {
					out.AccessBySeverity[severity] = count
				}
			}
		case "access_remediation_plan":
			if plan, ok := section.Data.(*AccessRemediationPlan); ok && plan != nil {
				out.RemediationActions = plan.Total
			}
		case "recent_audit":
			if audit, ok := section.Data.(ListResponse[AuditLog]); ok {
				out.AuditItems = len(audit.Items)
			}
		case "work_queue":
			if inbox, ok := section.Data.(*MISInboxSummary); ok && inbox != nil {
				out.OpenWorkItems = inbox.Total
				out.CriticalWorkItems = inbox.Critical
				out.HighWorkItems = inbox.High
				out.OverdueWorkItems = inbox.Overdue
			}
		case "connector_health":
			if connectors, ok := section.Data.([]ConnectorHealth); ok {
				out.Connectors = len(connectors)
				for _, item := range connectors {
					if item.Status != "ok" {
						out.ConnectorIssues++
					}
				}
			}
		}
	}
	if len(out.AccessBySeverity) == 0 {
		out.AccessBySeverity = nil
	}
	out.Controls = governanceControls(out)
	for _, item := range out.Controls {
		switch item.Status {
		case "fail":
			out.ControlFailures++
		case "warn":
			out.ControlWarnings++
		}
	}
	out.RiskLevel = governanceRiskLevel(out)
	out.Status = governanceStatus(out)
	out.Recommendations = governanceRecommendations(out)
	return out
}

func governanceRiskLevel(in GovernanceEvidenceSummary) string {
	if in.FailedSections > 0 || in.CriticalWorkItems > 0 || in.AccessBySeverity["critical"] > 0 {
		return "critical"
	}
	if in.AccessBySeverity["high"] > 0 || in.HighWorkItems > 0 || in.OverdueWorkItems > 0 || in.ConnectorIssues > 0 || in.ControlFailures > 0 {
		return "high"
	}
	if in.AccessFindings > 0 || in.RemediationActions > 0 || in.OpenWorkItems > 0 || in.ControlWarnings > 0 {
		return "medium"
	}
	return "low"
}

func governanceStatus(in GovernanceEvidenceSummary) string {
	switch in.RiskLevel {
	case "critical":
		return "blocked"
	case "high", "medium":
		return "needs_review"
	default:
		return "ready"
	}
}

func governanceRecommendations(in GovernanceEvidenceSummary) []string {
	out := []string{}
	if in.FailedSections > 0 {
		out = append(out, "Re-run the evidence pack with a data_admin key and inspect failed sections before handoff.")
	}
	if hasGovernanceControl(in.Controls, "recovery_backup", "fail") {
		out = append(out, "Create a verified backup before production rollout, imports, bulk cleanup, or schema changes.")
	}
	if hasGovernanceControl(in.Controls, "scoped_access", "warn") {
		out = append(out, "Create scoped API keys for agents and employees instead of relying on only the root token.")
	}
	if in.AccessBySeverity["critical"] > 0 || in.AccessBySeverity["high"] > 0 {
		out = append(out, "Review high-risk API keys and apply the remediation plan before production rollout.")
	}
	if in.RemediationActions > 0 && len(out) < 5 {
		out = append(out, "Execute or explicitly waive remediation actions with an audit note.")
	}
	if in.CriticalWorkItems > 0 || in.HighWorkItems > 0 || in.OverdueWorkItems > 0 {
		out = append(out, "Clear critical, high, or overdue MIS inbox items before declaring the service operationally ready.")
	}
	if in.ConnectorIssues > 0 {
		out = append(out, "Fix degraded connectors and retry dead-letter or failed sync work before external system handoff.")
	}
	if in.AuditItems == 0 {
		out = append(out, "Generate an auditable activity trail by running a controlled backup, quality check, or access review.")
	}
	if len(out) == 0 {
		out = append(out, "Governance evidence looks ready for normal operations; archive this pack with the rollout or audit record.")
	}
	return out
}

func governanceControls(in GovernanceEvidenceSummary) []GovernanceControl {
	return []GovernanceControl{
		governanceControlRecovery(in),
		governanceControlScopedAccess(in),
		governanceControlAuditTrail(in),
		governanceControlWorkQueue(in),
		governanceControlConnectors(in),
	}
}

func governanceControlRecovery(in GovernanceEvidenceSummary) GovernanceControl {
	if in.BackupCount > 0 {
		return GovernanceControl{ID: "recovery_backup", Title: "Recovery backup", Status: "pass", Detail: "backup_count=" + strconv.Itoa(in.BackupCount), RecommendedAction: "Review or create recovery points", ActionTarget: "backups"}
	}
	return GovernanceControl{ID: "recovery_backup", Title: "Recovery backup", Status: "fail", Detail: "no backups available", RecommendedAction: "Create backup", ActionTarget: "backups"}
}

func governanceControlScopedAccess(in GovernanceEvidenceSummary) GovernanceControl {
	if in.AccessBySeverity["critical"] > 0 || in.AccessBySeverity["high"] > 0 {
		return GovernanceControl{ID: "scoped_access", Title: "Scoped access", Status: "fail", Detail: "high-risk access findings require review", RecommendedAction: "Review access", ActionTarget: "access"}
	}
	if in.ManagedKeys == 0 {
		return GovernanceControl{ID: "scoped_access", Title: "Scoped access", Status: "warn", Detail: "no managed API keys configured", RecommendedAction: "Create scoped key", ActionTarget: "access"}
	}
	return GovernanceControl{ID: "scoped_access", Title: "Scoped access", Status: "pass", Detail: "managed_keys=" + strconv.Itoa(in.ManagedKeys), RecommendedAction: "Review scoped keys", ActionTarget: "access"}
}

func governanceControlAuditTrail(in GovernanceEvidenceSummary) GovernanceControl {
	if in.AuditItems > 0 {
		return GovernanceControl{ID: "audit_trail", Title: "Audit trail", Status: "pass", Detail: "audit_items=" + strconv.Itoa(in.AuditItems), RecommendedAction: "Review audit", ActionTarget: "audit"}
	}
	return GovernanceControl{ID: "audit_trail", Title: "Audit trail", Status: "warn", Detail: "no audit activity found", RecommendedAction: "Run audit-producing check", ActionTarget: "ops"}
}

func governanceControlWorkQueue(in GovernanceEvidenceSummary) GovernanceControl {
	if in.CriticalWorkItems > 0 {
		return GovernanceControl{ID: "work_queue", Title: "Work queue", Status: "fail", Detail: "critical_work_items=" + strconv.Itoa(in.CriticalWorkItems), RecommendedAction: "Open inbox", ActionTarget: "inbox"}
	}
	if in.HighWorkItems > 0 || in.OverdueWorkItems > 0 {
		return GovernanceControl{ID: "work_queue", Title: "Work queue", Status: "warn", Detail: "high_work_items=" + strconv.Itoa(in.HighWorkItems) + ", overdue_work_items=" + strconv.Itoa(in.OverdueWorkItems), RecommendedAction: "Open inbox", ActionTarget: "inbox"}
	}
	return GovernanceControl{ID: "work_queue", Title: "Work queue", Status: "pass", Detail: "open_work_items=" + strconv.Itoa(in.OpenWorkItems), RecommendedAction: "Review inbox", ActionTarget: "inbox"}
}

func governanceControlConnectors(in GovernanceEvidenceSummary) GovernanceControl {
	if in.ConnectorIssues > 0 {
		return GovernanceControl{ID: "connector_health", Title: "Connector health", Status: "fail", Detail: "connector_issues=" + strconv.Itoa(in.ConnectorIssues), RecommendedAction: "Open connectors", ActionTarget: "connectors"}
	}
	return GovernanceControl{ID: "connector_health", Title: "Connector health", Status: "pass", Detail: "connectors=" + strconv.Itoa(in.Connectors), RecommendedAction: "Review connectors", ActionTarget: "connectors"}
}

func hasGovernanceControl(items []GovernanceControl, id string, status string) bool {
	for _, item := range items {
		if item.ID == id && item.Status == status {
			return true
		}
	}
	return false
}
