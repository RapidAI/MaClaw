package database

import (
	"context"
	"fmt"
	"strings"
)

const nlSQLGuidance = "根据上方 schema 生成单条参数化 SELECT（:name 占位符），先调用 explain 预览计划，再 query 执行。禁止拼接用户值、多语句、注释和 EXPLAIN ANALYZE。"

func wrapExplainSQL(dialect, sqlText string) (wrapped string, useShowplan bool, err error) {
	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return "", false, fmt.Errorf("syntax: sql is required")
	}
	if err := validateReadSQLDialect(sqlText, dialect); err != nil {
		return "", false, err
	}
	d := scanDialect(dialect)
	if explainRequestsAnalyze(sqlText, d) {
		return "", false, fmt.Errorf("permission: EXPLAIN ANALYZE executes the statement and is not allowed")
	}
	fields := strings.Fields(strings.ToLower(sqlText))
	if len(fields) > 0 && fields[0] == "explain" {
		return sqlText, false, nil
	}
	switch strings.ToLower(strings.TrimSpace(dialect)) {
	case "postgres", "postgresql":
		return "EXPLAIN (FORMAT TEXT) " + sqlText, false, nil
	case "mysql", "mariadb":
		return "EXPLAIN " + sqlText, false, nil
	case "sqlite", "excel":
		return "EXPLAIN QUERY PLAN " + sqlText, false, nil
	case "sqlserver":
		return sqlText, true, nil
	case "access":
		return "", false, fmt.Errorf("unsupported_capability: Access does not support EXPLAIN")
	default:
		return "EXPLAIN " + sqlText, false, nil
	}
}

func classifyDDLOperation(sqlText string) DDLPlan {
	plan := DDLPlan{Operation: "ddl", Warnings: []string{"ddl_may_implicitly_commit", "policy_only_preview"}}
	guard, err := GuardSQL(sqlText)
	if err != nil {
		plan.Warnings = append(plan.Warnings, err.Error())
		return plan
	}
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(guard.SQL)))
	if len(fields) == 0 {
		return plan
	}
	plan.Operation = fields[0]
	if fields[0] == "create" && len(fields) > 1 && fields[1] == "index" {
		plan.Operation = "create_index"
	}
	if fields[0] == "drop" || fields[0] == "truncate" {
		plan.Destructive = true
	}
	plan.Targets = extractSQLTableNames(guard.SQL)
	return plan
}

func extractSQLTableNames(sqlText string) []string {
	fields := splitSQLTokens(sqlText)
	seen := map[string]bool{}
	var names []string
	for i := 0; i+1 < len(fields); i++ {
		keyword := strings.ToLower(strings.Trim(fields[i], "(),"))
		if keyword != "from" && keyword != "join" && keyword != "update" && keyword != "table" && keyword != "index" && keyword != "on" && keyword != "into" && keyword != "truncate" {
			continue
		}
		name := normalizeTableReference(fields[i+1])
		if name == "" || !safeTableName(name) || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		names = append(names, name)
	}
	return names
}

func (a *sqlAdapter) planDDL(ctx context.Context, sqlText string) *DDLPlan {
	plan := classifyDDLOperation(sqlText)
	if a == nil || len(plan.Targets) == 0 {
		return &plan
	}
	for _, target := range plan.Targets {
		info, err := a.Inspect(ctx, InspectRequest{Table: target})
		if err != nil {
			plan.Warnings = append(plan.Warnings, "inspect_unavailable:"+target)
			continue
		}
		plan.CurrentTables = append(plan.CurrentTables, info.Tables...)
	}
	return &plan
}

func buildNLPreview(ctx context.Context, adapter Adapter, prompt, profileID, connectionID string) (ExplainResult, error) {
	out := ExplainResult{
		ContractVersion: ContractVersion,
		ProfileID:       profileID,
		ConnectionID:    connectionID,
		Prompt:          strings.TrimSpace(prompt),
		Guidance:        nlSQLGuidance,
	}
	if adapter == nil {
		return out, fmt.Errorf("database connection not found")
	}
	caps := adapter.Capabilities()
	if !caps.Read {
		return out, fmt.Errorf("permission: profile is not readable")
	}
	info, err := adapter.Inspect(ctx, InspectRequest{})
	if err != nil {
		out.Warnings = append(out.Warnings, "inspect_unavailable")
		return out, nil
	}
	out.Dialect = info.Dialect
	out.Tables = info.Tables
	return out, nil
}
