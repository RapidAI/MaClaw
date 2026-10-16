package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ToolSchemaHash is a deterministic snapshot identifier hosts can log and
// compare at startup to detect GUI/TUI/server contract drift.
func ToolSchemaHash() string {
	payload, _ := json.Marshal(ToolParameters())
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", sum[:])
}

// ToolDescription is the single LLM-facing description for the database
// capability. Hosts must not maintain a second copy of this text.
func ToolDescription() string {
	return "查看库、查看数据库、列出库/schema/表、查看表结构，以及连接并操作 MySQL、PostgreSQL、SQL Server、Access 和 Excel 数据源。必须调用本工具，禁止用 bash/mysql/psql/sqlcmd 或把密码写进命令，也不要当成 Git 仓库或改用 ssh。多个数据源时先 list_connections，按用户提到的主机/库名/id 选 profile_id。没有匹配时调用 propose_profile（只传 host/username/database，不要传密码），由宿主弹出表单写入密钥环。connect 认证失败时宿主会再次弹出密码表单，等待用户保存后再 connect，不要自己写授权文案，不要用 ssh。先 inspect，再使用参数化 query。写入需 write_enabled、dry-run 和宿主审批。"
}

// ToolParameters returns a detached schema for the shared database tool. The
// returned map is safe for a host to enrich with transport-only metadata.
func ToolParameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "操作类型；不同 action 的必填字段由 handler 再次校验",
				"enum":        []string{"list_connections", "connect", "disconnect", "inspect", "query", "execute", "batch_execute", "read_table", "write_table", "export_excel", "job_status", "explain", "list_favorites", "save_favorite", "delete_favorite", "propose_profile"},
			},
			"profile_id":             map[string]string{"type": "string", "description": "已授权数据源 profile ID；不得传密码或完整 DSN。多个数据源时用 list_connections 按 host/database 选择"},
			"host":                   map[string]string{"type": "string", "description": "仅 list_connections 过滤、connect 匹配或 propose_profile 草稿；不能代替 profile 去直连"},
			"port":                   map[string]interface{}{"type": "integer", "description": "propose_profile 的端口；query/execute 不得传端口来绕过 profile", "minimum": 1, "maximum": 65535},
			"username":               map[string]string{"type": "string", "description": "propose_profile 的用户名，或 list_connections/connect 匹配；不得传密码"},
			"name":                   map[string]string{"type": "string", "description": "propose_profile 的显示名"},
			"type":                   map[string]interface{}{"type": "string", "enum": []string{"mysql", "postgres", "sqlserver", "access", "excel"}, "description": "propose_profile 的数据源类型，默认 mysql"},
			"allow_external_host":    map[string]string{"type": "boolean", "description": "propose_profile：允许公网主机；内网地址通常不需要"},
			"connection_id":          map[string]string{"type": "string", "description": "当前 session 的短期连接 ID"},
			"sql":                    map[string]string{"type": "string", "description": "参数化 SQL，使用 :name 占位符；positional 模式使用 ?"},
			"params":                 map[string]interface{}{"description": "命名参数对象，或 parameter_mode=positional 时的数组"},
			"parameter_mode":         map[string]interface{}{"type": "string", "enum": []string{"named", "positional"}, "description": "参数模式；positional 仅 MySQL/SQLite/Excel facade，须显式声明"},
			"statements":             map[string]interface{}{"type": "array", "description": "batch_execute 的有序参数化语句列表", "items": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"sql": map[string]string{"type": "string"}, "params": map[string]string{"type": "object"}}, "required": []string{"sql"}}},
			"table":                  map[string]string{"type": "string", "description": "表名或 Excel sheet 表名"},
			"file_path":              map[string]string{"type": "string", "description": "Excel/Access 文件路径或导出目标路径"},
			"source_sha256":          map[string]string{"type": "string", "description": "预览阶段返回的源文件哈希；提交时用于检测并发修改"},
			"sheet":                  map[string]string{"type": "string", "description": "Excel 工作表名称"},
			"range":                  map[string]string{"type": "string", "description": "Excel A1 范围，例如 A1:D100"},
			"rows":                   map[string]string{"type": "array", "description": "表格行数据"},
			"limit":                  map[string]interface{}{"type": "integer", "description": "最大返回行数，默认 100，最大 5000", "minimum": 1, "maximum": 5000},
			"cursor":                 map[string]string{"type": "string", "description": "query 返回的短期分页游标"},
			"result_handle":          map[string]string{"type": "string", "description": "query 返回的加密结果句柄；export_excel 可用来代替 rows，由后端按 owner/session 解析"},
			"timeout_seconds":        map[string]interface{}{"type": "integer", "description": "查询超时秒数", "minimum": 1, "maximum": 600},
			"async":                  map[string]string{"type": "boolean", "description": "query 在后台执行，立即返回 job_id；用 job_status 轮询，完成后通过 result_handle 分页"},
			"job_id":                 map[string]string{"type": "string", "description": "job_status 的异步查询作业 ID"},
			"favorite_id":            map[string]string{"type": "string", "description": "查询收藏 ID；query/explain 可用来代替 sql"},
			"favorite_name":          map[string]string{"type": "string", "description": "save_favorite 的名称"},
			"prompt":                 map[string]string{"type": "string", "description": "自然语言查询意图；explain 在无 sql 时返回 schema 预览，不执行 SQL"},
			"dry_run":                map[string]string{"type": "boolean", "description": "写操作预检；提交必须为 false 且带审批上下文"},
			"max_affected_rows":      map[string]string{"type": "integer", "description": "写操作影响行数上限"},
			"expected_affected_rows": map[string]string{"type": "integer", "description": "审批时的影响行数预期，仅用于偏差告警，不作为安全边界"},
		},
		"required": []string{"action"},
	}
}

// ToolProperties returns a detached property map for registries whose schema
// builder already wraps the object/required envelope.
func ToolProperties() map[string]interface{} {
	params := ToolParameters()
	props, _ := params["properties"].(map[string]interface{})
	return props
}

// HandleTool executes the transport-neutral SQL portion of the database
// tool. Excel actions remain host adapters because they require the host's
// document reader/writer, but connection/query/execute semantics are shared
// by GUI, srv, and TUI.
func HandleTool(ctx context.Context, manager *Manager, args map[string]interface{}) string {
	if ctx == nil {
		ctx = context.Background()
	}
	if manager == nil {
		return "数据库连接工具未初始化。请先配置数据源 profile。"
	}
	if manager.IsClosed() {
		return "数据库连接工具已关闭。请重新创建 Agent 运行实例后重试。"
	}
	if reason := manager.disabledReason(); reason != "" {
		manager.noteDenial()
		return reason
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	scope := requestScopeFromContext(ctx)
	// Identity projections are transport-private. If a host did not attach a
	// trusted scope, reject model-supplied values instead of treating them as
	// authentication metadata. When a scope is present, the values are
	// overwritten below and therefore cannot influence authorization.
	for key := range args {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "_owner_id":
			if scope.OwnerID == "" {
				return "Error: _owner_id must be supplied by the trusted host request scope"
			}
		case "_session_id":
			if scope.SessionID == "" {
				return "Error: _session_id must be supplied by the trusted host request scope"
			}
		case "_operation_id":
			if scope.OperationID == "" {
				return "Error: _operation_id must be supplied by the trusted host request scope"
			}
		case "_attempt":
			if scope.Attempt == 0 {
				return "Error: _attempt must be supplied by the trusted host request scope"
			}
		case "_parent_action_id":
			if scope.ParentActionID == "" {
				return "Error: _parent_action_id must be supplied by the trusted host request scope"
			}
		}
	}
	// Request ownership is host-authenticated metadata. Clone the argument map
	// before adding its private projection so callers never observe internal
	// owner/session fields and model JSON cannot override a context value.
	if scope.OwnerID != "" || scope.SessionID != "" || scope.OperationID != "" || scope.Attempt != 0 || scope.ParentActionID != "" {
		bound := make(map[string]interface{}, len(args)+5)
		for key, value := range args {
			bound[key] = value
		}
		if scope.OwnerID != "" {
			bound["_owner_id"] = scope.OwnerID
		}
		if scope.SessionID != "" {
			bound["_session_id"] = scope.SessionID
		}
		if scope.OperationID != "" {
			bound["_operation_id"] = scope.OperationID
		}
		if scope.Attempt != 0 {
			bound["_attempt"] = scope.Attempt
		}
		if scope.ParentActionID != "" {
			bound["_parent_action_id"] = scope.ParentActionID
		}
		args = bound
	}
	// A token in model-controlled JSON is never an authorization source. Keep
	// this rejection explicit so callers do not accidentally believe a token
	// was consumed merely because it was present in the arguments. Reject the
	// field even when it is empty or has a non-string type: accepting an
	// alternate representation would make it too easy for a transport adapter
	// to accidentally turn model output into an authorization decision.
	for key := range args {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "approval_token" {
			return "Error: approval_token must be supplied by the trusted host approval context, not tool arguments"
		}
		if lower == "ssh_session_id" {
			return "Error: ssh_session_id must be configured on the profile, not tool arguments"
		}
		if lower == "_operation_id" || lower == "_attempt" || lower == "_parent_action_id" {
			// These private projections are accepted only after the trusted scope
			// checks above and are overwritten from that scope.
			continue
		}
		if lower == "replica_host" || lower == "replica_port" || lower == "replica_ssh_session_id" {
			return "Error: replica routing must be configured on the profile, not tool arguments"
		}
	}
	approval := approvalFromContext(ctx)
	if containsCredentialArgument(args) {
		return "Error: database arguments must not include credentials or DSN; use profile_id and secret_ref configuration"
	}
	action := stringArg(args, "action")
	switch action {
	case "list_connections", "connect", "propose_profile":
	default:
		if stringArg(args, "host") != "" || stringArg(args, "username") != "" {
			return "Error: host and username must be configured on the profile, not tool arguments for " + action
		}
	}
	ownerID, sessionID := stringArg(args, "_owner_id"), stringArg(args, "_session_id")
	switch action {
	case "list_connections":
		hint := ProfileMatch{ID: stringArg(args, "profile_id"), Host: stringArg(args, "host"), Database: stringArg(args, "database"), Username: stringArg(args, "username"), Type: SourceType(stringArg(args, "type"))}
		items := manager.MatchProfiles(hint)
		if db := strings.TrimSpace(hint.Database); db != "" {
			for _, item := range items {
				manager.RememberCatalogNames(item.ID, db)
			}
		}
		manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ResultClass: "ok"}))
		return marshal(map[string]interface{}{"items": items, "count": len(items)})
	case "propose_profile":
		draft := draftProposedProfile(args)
		if strings.TrimSpace(draft.Host) == "" && strings.TrimSpace(draft.FilePath) == "" {
			return "缺少 host 或 file_path 参数"
		}
		manager.RememberCatalogNames(draft.ID, draft.Database)
		if existing, ok := readyProposedProfile(manager, draft); ok {
			manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ProfileID: existing.ID, ResultClass: "exists"}))
			return marshal(map[string]interface{}{
				"ok":           true,
				"needs_secret": false,
				"profile":      existing,
				"guidance":     "数据源已配置。请 connect / inspect / query，不要再 propose_profile，不要用 ssh 或 bash mysql。",
			})
		}
		manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ProfileID: draft.ID, ResultClass: "needs_secret"}))
		return marshal(map[string]interface{}{
			"ok":           true,
			"needs_secret": true,
			"profile":      ProfileSummary{ID: draft.ID, Name: draft.Name, Type: draft.Type, Status: "proposed", Host: draft.Host, Port: draft.Port, Database: draft.Database, Username: draft.Username, ReadOnly: true},
			"draft":        map[string]interface{}{"id": draft.ID, "name": draft.Name, "type": draft.Type, "host": draft.Host, "port": draft.Port, "database": draft.Database, "username": draft.Username, "read_only": true, "allow_external_host": draft.AllowExternalHost},
			"guidance":     "宿主已在任务面板打开密码表单。等待用户保存后再 list_connections / connect，不要立刻 connect。",
		})
	case "connect":
		id, matched, resolveErr := manager.ResolveProfileID(ProfileMatch{ID: stringArg(args, "profile_id"), Host: stringArg(args, "host"), Database: stringArg(args, "database"), Username: stringArg(args, "username"), Type: SourceType(stringArg(args, "type"))})
		if resolveErr != nil {
			payload := map[string]interface{}{"ok": false, "error": resolveErr.Error(), "items": matched, "guidance": "没有唯一匹配时请调用 propose_profile（不要传密码）或让用户选择 profile_id"}
			manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ResultClass: resolveErr.Error()}))
			return marshal(payload)
		}
		connectionID, caps, err := manager.ConnectFor(ctx, id, ownerID, sessionID)
		if err != nil {
			class := connectErrorClass(err)
			manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ProfileID: id, ResultClass: class}))
			return connectFailureResult(manager, id, err)
		}
		manager.RememberCatalogNames(id, stringArg(args, "database"))
		if p, ok := manager.ProfileByID(id); ok {
			manager.RememberCatalogNames(id, p.Database, p.DefaultSchema)
		}
		manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ProfileID: id, ConnectionID: connectionID, ResultClass: "ok"}))
		return marshal(map[string]interface{}{"ok": true, "profile_id": id, "connection_id": connectionID, "capabilities": caps})
	case "disconnect":
		id := stringArg(args, "connection_id")
		if id == "" {
			return "缺少 connection_id 参数"
		}
		if err := manager.DisconnectFor(id, ownerID, sessionID); err != nil {
			return "database disconnect failed: " + err.Error()
		}
		manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, ResultClass: "ok"}))
		return marshal(map[string]interface{}{"ok": true, "connection_id": id})
	case "read_table":
		result, err := readTableAction(ctx, manager, args)
		if err != nil {
			manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ResultClass: "read_error"}))
			return "database read_table failed: " + err.Error()
		}
		manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ResultClass: "ok"}))
		return marshal(result)
	case "write_table", "export_excel":
		// Bind result_handle before policy checks so handle-only exports
		// still resolve the originating profile after a restart.
		exportProfileID := ""
		if action == "export_excel" {
			snapshot, err := manager.bindExportHandle(args, ownerID, sessionID)
			if err != nil {
				manager.noteDenial()
				return "database " + action + " failed: " + err.Error()
			}
			exportProfileID = snapshot.ProfileID
		}
		connectionID := stringArg(args, "connection_id")
		profileID := ""
		if connectionID != "" {
			profileID = manager.ProfileIDForConnection(connectionID)
			if profileID != "" {
				if _, ok := manager.AdapterFor(connectionID, ownerID, sessionID); !ok {
					return "database " + action + " failed: permission: connection is not bound to this session"
				}
			} else if action != "export_excel" {
				return "database " + action + " failed: connection not found"
			}
		}
		if profileID == "" {
			profileID = exportProfileID
		}
		if profileID != "" && !manager.operationAllowed(profileID, action) {
			return "database " + action + " failed: permission: operation is not allowed by profile"
		}
		if action == "export_excel" {
			if err := manager.enforceExportClassification(profileID); err != nil {
				manager.noteDenial()
				return "database " + action + " failed: " + err.Error()
			}
		}
		if !boolArg(args, "dry_run", true) {
			// Reuse the profile resolved above (live connection or result
			// handle). Shadowing it back to "" would reject a valid approval
			// after the originating connection has closed.
			schemaVersion := 0
			if connectionID != "" {
				if liveID := manager.ProfileIDForConnection(connectionID); liveID != "" {
					schemaVersion = manager.ProfileSchemaVersionForConnection(connectionID)
				}
			}
			if schemaVersion == 0 && profileID != "" {
				if p, ok := manager.ProfileByID(profileID); ok {
					schemaVersion = p.SchemaVersion
				}
			}
			if err := validateApprovalContextWithParams(approval, action+":"+stringArg(args, "file_path"), profileID, fileActionParamsMap(args), schemaVersion); err != nil {
				manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ApprovalID: approval.ID, ResultClass: "approval_rejected", Risk: "elevated"}))
				return "database " + action + " failed: " + err.Error()
			}
			if err := manager.consumeApproval(ctx, approval, sqlFingerprint(action+":"+stringArg(args, "file_path"))); err != nil {
				manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ApprovalID: approval.ID, ResultClass: "approval_rejected", Risk: "elevated"}))
				return "database " + action + " failed: " + err.Error()
			}
		}
		result, err := writeTableActionWithManager(ctx, manager, args, action == "export_excel")
		if err != nil {
			manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ApprovalID: approval.ID, ResultClass: "mutation_error", Risk: "elevated"}))
			return "database " + action + " failed: " + err.Error()
		}
		receiptID := ""
		if !boolArg(args, "dry_run", true) {
			if payload, ok := result.(map[string]interface{}); ok {
				payload["commit_id"] = "db-commit-" + randomID()
				receiptID = "db-receipt-" + randomID()
				payload["receipt_id"] = receiptID
			}
		}
		manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ApprovalID: approval.ID, ReceiptID: receiptID, ResultClass: "ok", Risk: "elevated"}))
		return marshal(result)
	case "list_favorites":
		store := manager.favoriteStore()
		if store == nil {
			return marshal([]QueryFavorite{})
		}
		manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ResultClass: "ok"}))
		return marshal(store.List(ownerID))
	case "save_favorite":
		store := manager.favoriteStore()
		if store == nil {
			return "database save_favorite failed: favorites store unavailable"
		}
		saved, err := store.Save(QueryFavorite{
			ID:        stringArg(args, "favorite_id"),
			Name:      stringArg(args, "favorite_name"),
			ProfileID: stringArg(args, "profile_id"),
			SQL:       stringArg(args, "sql"),
			OwnerID:   ownerID,
		})
		if err != nil {
			manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, SQLFingerprint: sqlFingerprint(stringArg(args, "sql")), ResultClass: "favorite_error"}))
			return "database save_favorite failed: " + err.Error()
		}
		manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ProfileID: saved.ProfileID, SQLFingerprint: sqlFingerprint(saved.SQL), ResultClass: "ok"}))
		return marshal(saved)
	case "delete_favorite":
		store := manager.favoriteStore()
		if store == nil {
			return "database delete_favorite failed: favorites store unavailable"
		}
		id := stringArg(args, "favorite_id")
		if id == "" {
			return "缺少 favorite_id 参数"
		}
		if err := store.Delete(id, ownerID); err != nil {
			return "database delete_favorite failed: " + err.Error()
		}
		manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ResultClass: "ok"}))
		return marshal(map[string]interface{}{"ok": true, "favorite_id": id})
	case "job_status":
		jobID := stringArg(args, "job_id")
		if jobID == "" {
			return "缺少 job_id 参数"
		}
		status, err := manager.AsyncJobStatus(jobID, ownerID, sessionID)
		if err != nil {
			manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ResultClass: "job_error"}))
			return "database job_status failed: " + err.Error()
		}
		manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: status.ConnectionID, ResultClass: status.Status}))
		return marshal(status)
	case "inspect", "query", "execute", "batch_execute", "explain":
		id := stringArg(args, "connection_id")
		cursorToken := ""
		if action == "query" {
			cursorToken = stringArg(args, "cursor")
		}
		if id == "" && cursorToken == "" && stringArg(args, "profile_id") != "" && (action == "inspect" || action == "query" || action == "explain") {
			var caps Capabilities
			var err error
			id, caps, err = manager.ConnectFor(ctx, stringArg(args, "profile_id"), ownerID, sessionID)
			_ = caps
			if err != nil {
				profileID := stringArg(args, "profile_id")
				manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ProfileID: profileID, ResultClass: connectErrorClass(err)}))
				return connectFailureResult(manager, profileID, err)
			}
			if action != "query" || !boolArg(args, "async", false) {
				defer func() { _ = manager.DisconnectFor(id, ownerID, sessionID) }()
			}
		}
		if id == "" && cursorToken == "" {
			return "缺少 connection_id 参数"
		}
		profileID := ""
		if id != "" {
			profileID = manager.ProfileIDForConnection(id)
		}
		allowAction := action
		if allowAction == "job_status" || allowAction == "explain" {
			allowAction = "query"
		}
		if profileID != "" && !manager.operationAllowed(profileID, allowAction) {
			return "database " + action + " failed: permission: operation is not allowed by profile"
		}
		if cursorToken != "" {
			result, err := manager.readResultPageFor(cursorToken, ownerID, sessionID, id, intArg(args, "limit", 100))
			if err != nil {
				manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, ResultClass: "cursor_error"}))
				return "database query failed: " + err.Error()
			}
			manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, ResultClass: "cursor_page"}))
			if result.ConnectionID == "" {
				result.ConnectionID = id
			}
			return marshal(result)
		}
		if id == "" {
			return "缺少 connection_id 参数"
		}
		adapter, ok := manager.AdapterFor(id, ownerID, sessionID)
		if !ok {
			return "database connection not found"
		}
		asyncQuery := action == "query" && boolArg(args, "async", false)
		if !asyncQuery {
			release, err := manager.acquireProfile(ctx, manager.ProfileIDForConnection(id))
			if err != nil {
				return "database request rejected: " + err.Error()
			}
			defer release()
		}
		switch action {
		case "inspect":
			info, err := adapter.Inspect(ctx, InspectRequest{Table: stringArg(args, "table")})
			if err != nil {
				return "database inspect failed: " + err.Error()
			}
			info.ProfileID = manager.ProfileIDForConnection(id)
			manager.RememberCatalogNames(info.ProfileID, catalogNamesFromSchema(info)...)
			return marshal(info)
		case "explain":
			prompt := stringArg(args, "prompt")
			sqlText := stringArg(args, "sql")
			if sqlText == "" {
				if favID := stringArg(args, "favorite_id"); favID != "" {
					favSQL, err := lookupFavoriteSQL(manager, favID, ownerID, id)
					if err != nil {
						return "database explain failed: " + err.Error()
					}
					sqlText = favSQL
				}
			}
			if sqlText == "" && prompt == "" {
				return "缺少 sql 或 prompt 参数"
			}
			if sqlText == "" {
				preview, err := buildNLPreview(ctx, adapter, prompt, manager.ProfileIDForConnection(id), id)
				if err != nil {
					return "database explain failed: " + err.Error()
				}
				manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, ResultClass: "nl_preview"}))
				return marshal(preview)
			}
			named, positional := toolCallParams(args)
			mode := stringArg(args, "parameter_mode")
			result, err := adapter.Query(ctx, QueryRequest{
				SQL: sqlText, Params: named, PositionalParams: positional, ParameterMode: mode,
				Limit: intArg(args, "limit", 100), Timeout: intArg(args, "timeout_seconds", 30), Explain: true,
			})
			if err != nil {
				manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, SQLFingerprint: sqlFingerprint(sqlText), ResultClass: "explain_error"}))
				return "database explain failed: " + err.Error()
			}
			out := ExplainResult{
				ContractVersion: ContractVersion,
				ProfileID:       manager.ProfileIDForConnection(id),
				ConnectionID:    id,
				Prompt:          prompt,
				SQL:             sqlText,
				Columns:         result.Columns,
				Rows:            result.Rows,
				RowCount:        result.RowCount,
				Warnings:        append([]string{"explain_preview"}, result.Warnings...),
				ElapsedMS:       result.ElapsedMS,
			}
			manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, SQLFingerprint: sqlFingerprint(sqlText), ResultClass: "ok"}))
			return marshal(out)
		case "query":
			sqlText := stringArg(args, "sql")
			if sqlText == "" {
				if favID := stringArg(args, "favorite_id"); favID != "" {
					favSQL, err := lookupFavoriteSQL(manager, favID, ownerID, id)
					if err != nil {
						return "database query failed: " + err.Error()
					}
					sqlText = favSQL
				}
			}
			if strings.TrimSpace(sqlText) == "" {
				return "缺少 sql 参数"
			}
			if n := intArg(args, "limit", 100); n < 1 || n > 5000 {
				return "quota_exceeded: limit must be between 1 and 5000"
			}
			if n := intArg(args, "timeout_seconds", 30); n < 1 || n > 600 {
				return "quota_exceeded: timeout_seconds must be between 1 and 600"
			}
			named, positional := toolCallParams(args)
			mode := stringArg(args, "parameter_mode")
			queryReq := QueryRequest{
				SQL: sqlText, Params: named, PositionalParams: positional, ParameterMode: mode,
				Limit: intArg(args, "limit", 100), Cursor: stringArg(args, "cursor"),
				Timeout: intArg(args, "timeout_seconds", 30),
			}
			if boolArg(args, "async", false) {
				job, err := manager.StartAsyncQuery(ctx, adapter, queryReq, ownerID, sessionID, id, manager.ProfileIDForConnection(id))
				if err != nil {
					manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, SQLFingerprint: sqlFingerprint(queryReq.SQL), ResultClass: "query_error"}))
					return "database query failed: " + err.Error()
				}
				manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, SQLFingerprint: sqlFingerprint(queryReq.SQL), ResultClass: "async_queued"}))
				return marshal(job)
			}
			result, err := adapter.Query(ctx, queryReq)
			if err != nil {
				manager.recordQueryOutcome(ctx, sqlFingerprint(sqlText), err)
				manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, SQLFingerprint: sqlFingerprint(sqlText), ResultClass: "query_error"}))
				return "database query failed: " + err.Error()
			}
			manager.recordQueryOutcome(ctx, sqlFingerprint(sqlText), nil)
			result.ProfileID = manager.ProfileIDForConnection(id)
			result.ConnectionID = id
			manager.metrics.queryCount.Add(1)
			if result.Truncated {
				manager.metrics.truncations.Add(1)
			}
			if result.Truncated && len(result.allRows) > len(result.Rows) && !hasWarning(result.Warnings, "pagination_unavailable") {
				result.NextCursor = manager.storeResultForConnection(result, ownerID, sessionID, id, len(result.Rows))
				result.ResultHandle = result.NextCursor
			}
			if strings.EqualFold(mode, "positional") {
				result.Warnings = append(result.Warnings, "parameter_mode_positional")
			}
			paramCount := len(named)
			if paramCount == 0 {
				paramCount = len(positional)
			}
			manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, SQLFingerprint: sqlFingerprint(sqlText), ParameterCount: paramCount, ResultClass: "ok"}))
			return marshal(result)
		case "execute":
			if strings.TrimSpace(stringArg(args, "sql")) == "" {
				return "缺少 sql 参数"
			}
			named, positional := toolCallParams(args)
			mode := stringArg(args, "parameter_mode")
			params := named
			if !boolArg(args, "dry_run", true) {
				if manager.StrictOperationGateEnabled() && approval.OperationID != "" {
					scope := requestScopeFromContext(ctx)
					if approval.OperationID != scope.OperationID || (approval.Attempt != 0 && scope.Attempt != 0 && approval.Attempt != scope.Attempt) {
						manager.terminateOperation(ctx, sqlFingerprint(stringArg(args, "sql")))
						return "database execute failed: permission: approval context does not match operation lineage"
					}
				}
				if err := manager.authorizeExecuteOperation(ctx, sqlFingerprint(stringArg(args, "sql"))); err != nil {
					manager.terminateOperation(ctx, sqlFingerprint(stringArg(args, "sql")))
					return "database execute failed: " + err.Error()
				}
				if err := validateApprovalContextWithParams(approval, stringArg(args, "sql"), manager.ProfileIDForConnection(id), params, manager.ProfileSchemaVersionForConnection(id)); err != nil {
					manager.terminateOperation(ctx, sqlFingerprint(stringArg(args, "sql")))
					manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, SQLFingerprint: sqlFingerprint(stringArg(args, "sql")), ParameterCount: len(params), ApprovalID: approval.ID, ResultClass: "approval_rejected", Risk: "elevated"}))
					return "database execute failed: " + err.Error()
				}
				if err := manager.consumeApproval(ctx, approval, sqlFingerprint(stringArg(args, "sql"))); err != nil {
					manager.terminateOperation(ctx, sqlFingerprint(stringArg(args, "sql")))
					manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, ApprovalID: approval.ID, ResultClass: "approval_rejected", Risk: "elevated"}))
					return "database execute failed: " + err.Error()
				}
			}
			result, err := adapter.Execute(ctx, ExecuteRequest{
				SQL: stringArg(args, "sql"), Params: params, PositionalParams: positional, ParameterMode: mode,
				DryRun:          boolArg(args, "dry_run", true),
				ApprovalToken:   approval.Token,
				MaxAffectedRows: int64(intArg(args, "max_affected_rows", 0)),
			})
			if err != nil {
				manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, SQLFingerprint: sqlFingerprint(stringArg(args, "sql")), ResultClass: "mutation_error", Risk: "elevated"}))
				return "database execute failed: " + err.Error()
			}
			result.ProfileID = manager.ProfileIDForConnection(id)
			if !result.DryRun {
				if result.CommitID == "" {
					result.CommitID = "db-commit-" + randomID()
				}
				result.ReceiptID = "db-receipt-" + randomID()
			}
			if expected, ok := integerArg(args, "expected_affected_rows"); ok && int64(expected) != result.AffectedRows {
				result.Warnings = append(result.Warnings, "expected_affected_rows_mismatch")
			}
			// Only the host-issued, non-secret approval ID enters the audit event;
			// the opaque token remains confined to the request context.
			manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, SQLFingerprint: sqlFingerprint(stringArg(args, "sql")), ParameterCount: len(params), AffectedRows: result.AffectedRows, ApprovalID: approval.ID, ReceiptID: result.ReceiptID, ResultClass: "ok", Risk: "elevated"}))
			return marshal(result)
		case "batch_execute":
			statements, err := parseBatchStatements(args["statements"])
			if err != nil {
				manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, ApprovalID: approval.ID, ResultClass: "mutation_error", Risk: "elevated"}))
				return "database batch_execute failed: " + err.Error()
			}
			if !boolArg(args, "dry_run", true) {
				if manager.StrictOperationGateEnabled() && approval.OperationID != "" {
					scope := requestScopeFromContext(ctx)
					if approval.OperationID != scope.OperationID || (approval.Attempt != 0 && scope.Attempt != 0 && approval.Attempt != scope.Attempt) {
						manager.terminateOperation(ctx, batchSQLFingerprint(statements))
						return "database batch_execute failed: permission: approval context does not match operation lineage"
					}
				}
				if err := manager.authorizeExecuteOperation(ctx, batchSQLFingerprint(statements)); err != nil {
					manager.terminateOperation(ctx, batchSQLFingerprint(statements))
					return "database batch_execute failed: " + err.Error()
				}
				if err := validateApprovalContextWithParams(approval, batchSQLCanonical(statements), manager.ProfileIDForConnection(id), batchParamsMap(statements), manager.ProfileSchemaVersionForConnection(id)); err != nil {
					manager.terminateOperation(ctx, batchSQLFingerprint(statements))
					manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, SQLFingerprint: batchSQLFingerprint(statements), ParameterCount: batchParameterCount(statements), ApprovalID: approval.ID, ResultClass: "approval_rejected", Risk: "elevated"}))
					return "database batch_execute failed: " + err.Error()
				}
				if err := manager.consumeApproval(ctx, approval, batchSQLFingerprint(statements)); err != nil {
					manager.terminateOperation(ctx, batchSQLFingerprint(statements))
					manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, ApprovalID: approval.ID, ResultClass: "approval_rejected", Risk: "elevated"}))
					return "database batch_execute failed: " + err.Error()
				}
			}
			result, err := batchAdapterExecute(ctx, adapter, BatchExecuteRequest{
				Statements: statements, DryRun: boolArg(args, "dry_run", true), ApprovalToken: approval.Token, MaxStatements: 100,
			})
			if err != nil {
				manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{Action: action, ConnectionID: id, SQLFingerprint: batchSQLFingerprint(statements), ParameterCount: batchParameterCount(statements), ApprovalID: approval.ID, ResultClass: "mutation_error", Risk: "elevated"}))
				return "database batch_execute failed: " + err.Error()
			}
			result.ProfileID = manager.ProfileIDForConnection(id)
			if !result.DryRun {
				if result.CommitID == "" {
					result.CommitID = "db-commit-" + randomID()
				}
				result.ReceiptID = "db-receipt-" + randomID()
			}
			manager.emitAudit(ctx, auditFromArgs(args, AuditEvent{
				Action:         action,
				ConnectionID:   id,
				SQLFingerprint: batchSQLFingerprint(statements),
				ParameterCount: batchParameterCount(statements),
				AffectedRows:   result.AffectedRows,
				ApprovalID:     approval.ID,
				ReceiptID:      result.ReceiptID,
				ResultClass:    "ok",
				Risk:           "elevated",
			}))
			return marshal(result)
		}
	default:
		return fmt.Sprintf("未知的 database action: %q", action)
	}
	return fmt.Sprintf("未知的 database action: %q", action)
}

// batchSQLFingerprint gives a stable, metadata-only identity for a batch
// without persisting SQL parameter values. Statement order and SQL text are
// significant because changing either must invalidate any host approval.
func batchSQLFingerprint(statements []BatchStatement) string {
	return sqlFingerprint(batchSQLCanonical(statements))
}

// MutationFingerprints computes the canonical SQL/parameter fingerprints that
// HandleTool validates for the given mutation arguments. Hosts use it to bind
// an issued approval (Manager.IssueApproval) to the exact previewed
// operation; keeping the canonical form in one place prevents approval
// validation and audit from drifting apart.
func MutationFingerprints(args map[string]interface{}) (sqlFP, paramsFP string, err error) {
	switch stringArg(args, "action") {
	case "execute":
		sqlText := stringArg(args, "sql")
		if strings.TrimSpace(sqlText) == "" {
			return "", "", fmt.Errorf("syntax: approval fingerprint requires sql")
		}
		params, _ := args["params"].(map[string]interface{})
		return sqlFingerprint(sqlText), paramsFingerprint(params), nil
	case "batch_execute":
		statements, parseErr := parseBatchStatements(args["statements"])
		if parseErr != nil {
			return "", "", parseErr
		}
		return batchSQLFingerprint(statements), paramsFingerprint(batchParamsMap(statements)), nil
	case "write_table", "export_excel":
		filePath := stringArg(args, "file_path")
		if strings.TrimSpace(filePath) == "" {
			return "", "", fmt.Errorf("syntax: approval fingerprint requires file_path")
		}
		// Mirrors the commit-side validation in HandleTool for file actions.
		return sqlFingerprint(stringArg(args, "action") + ":" + filePath), paramsFingerprint(fileActionParamsMap(args)), nil
	default:
		return "", "", fmt.Errorf("syntax: action %q is not a mutation", stringArg(args, "action"))
	}
}

func batchSQLCanonical(statements []BatchStatement) string {
	var b strings.Builder
	for i, statement := range statements {
		if i > 0 {
			b.WriteByte(0)
		}
		b.WriteString(strings.TrimSpace(statement.SQL))
	}
	return b.String()
}

func batchParameterCount(statements []BatchStatement) int {
	total := 0
	for _, statement := range statements {
		total += len(statement.Params)
	}
	return total
}

func parseBatchStatements(raw interface{}) ([]BatchStatement, error) {
	items, ok := raw.([]interface{})
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("statements must be a non-empty array")
	}
	statements := make([]BatchStatement, 0, len(items))
	var sqlBytes int
	for i, item := range items {
		entry, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("statements[%d] must be an object", i)
		}
		sqlText := stringArg(entry, "sql")
		if sqlText == "" {
			return nil, fmt.Errorf("statements[%d].sql is required", i)
		}
		sqlBytes += len(sqlText)
		if sqlBytes > 1_000_000 {
			return nil, fmt.Errorf("quota_exceeded: batch SQL exceeds 1 MB")
		}
		params, _ := entry["params"].(map[string]interface{})
		statements = append(statements, BatchStatement{SQL: sqlText, Params: params, MaxAffectedRows: int64(intArg(entry, "max_affected_rows", 0))})
	}
	return statements, nil
}

func batchAdapterExecute(ctx context.Context, adapter Adapter, req BatchExecuteRequest) (MutationResult, error) {
	batch, ok := adapter.(BatchAdapter)
	if !ok {
		return MutationResult{}, fmt.Errorf("unsupported_capability: batch transactions are unavailable")
	}
	return batch.ExecuteBatch(ctx, req)
}

func toolCallParams(args map[string]interface{}) (map[string]interface{}, []interface{}) {
	if args == nil {
		return nil, nil
	}
	switch value := args["params"].(type) {
	case map[string]interface{}:
		return value, nil
	case []interface{}:
		return nil, value
	default:
		return nil, nil
	}
}

func (m *Manager) enforceExportClassification(profileID string) error {
	if m == nil || strings.TrimSpace(profileID) == "" {
		return nil
	}
	profile, ok := m.ProfileByID(profileID)
	if !ok {
		return fmt.Errorf("permission: classified export requires an enabled profile")
	}
	switch strings.ToLower(strings.TrimSpace(profile.DataClassification)) {
	case "confidential", "restricted":
		if profile.Disabled {
			return fmt.Errorf("permission: profile is disabled")
		}
	}
	return nil
}

func lookupFavoriteSQL(manager *Manager, favoriteID, ownerID, connectionID string) (string, error) {
	if manager == nil {
		return "", fmt.Errorf("favorites store unavailable")
	}
	store := manager.favoriteStore()
	if store == nil {
		return "", fmt.Errorf("favorites store unavailable")
	}
	fav, err := store.Get(favoriteID, ownerID)
	if err != nil {
		return "", err
	}
	if connectionID != "" && fav.ProfileID != "" && manager.ProfileIDForConnection(connectionID) != fav.ProfileID {
		return "", fmt.Errorf("permission: favorite is bound to another profile")
	}
	return fav.SQL, nil
}

func stringArg(args map[string]interface{}, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func boolArg(args map[string]interface{}, key string, fallback bool) bool {
	value, ok := args[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func intArg(args map[string]interface{}, key string, fallback int) int {
	switch value := args[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &parsed); err == nil {
			return parsed
		}
	}
	return fallback
}

func integerArg(args map[string]interface{}, key string) (int, bool) {
	value, ok := args[key]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), v == float64(int(v))
	case json.Number:
		i, err := v.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func containsCredentialArgument(args map[string]interface{}) bool {
	for key := range args {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "password", "passwd", "secret", "secret_ref", "token", "access_token", "dsn":
			return true
		}
	}
	return false
}

func auditFromArgs(args map[string]interface{}, event AuditEvent) AuditEvent {
	event.OwnerID = stringArg(args, "_owner_id")
	event.SessionID = stringArg(args, "_session_id")
	event.OperationID = stringArg(args, "_operation_id")
	event.ParentActionID = stringArg(args, "_parent_action_id")
	if attempt, ok := integerArg(args, "_attempt"); ok {
		event.Attempt = attempt
	}
	return event
}

func validateApprovalContext(approval ApprovalContext, sqlText, profileID string, schemaVersions ...int) error {
	if strings.TrimSpace(approval.Token) == "" {
		return fmt.Errorf("permission: approval context is required")
	}
	if !approval.ExpiresAt.IsZero() && time.Now().After(approval.ExpiresAt) {
		return fmt.Errorf("permission: approval context has expired")
	}
	if approval.SQLFingerprint != "" && !strings.EqualFold(approval.SQLFingerprint, sqlFingerprint(sqlText)) {
		return fmt.Errorf("permission: approval does not match SQL fingerprint")
	}
	if approval.ProfileID != "" && approval.ProfileID != profileID {
		return fmt.Errorf("permission: approval does not match profile")
	}
	if approval.SchemaVersion != 0 && len(schemaVersions) > 0 && schemaVersions[0] != 0 && approval.SchemaVersion != schemaVersions[0] {
		return fmt.Errorf("permission: approval does not match profile schema version")
	}
	return nil
}

func validateApprovalContextWithParams(approval ApprovalContext, sqlText, profileID string, params map[string]interface{}, schemaVersion int) error {
	if err := validateApprovalContext(approval, sqlText, profileID, schemaVersion); err != nil {
		return err
	}
	if approval.ParamsFingerprint != "" && !strings.EqualFold(approval.ParamsFingerprint, paramsFingerprint(params)) {
		return fmt.Errorf("permission: approval does not match parameter fingerprint")
	}
	return nil
}

func paramsFingerprint(params map[string]interface{}) string {
	payload, _ := json.Marshal(params)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// fileActionParamsMap is the canonical, deterministic parameter projection
// bound into approvals for write_table/export_excel. Rows enter only as a
// digest so the fingerprint never persists cell values; MutationFingerprints
// and the commit path in HandleTool must both use this exact shape.
// json.Marshal of a map sorts keys, so the encoding is deterministic.
func fileActionParamsMap(args map[string]interface{}) map[string]interface{} {
	// Digest the rows that will actually be written: writeTableAction falls
	// back to args["data"]["rows"] when args["rows"] normalizes to an empty
	// array, so the approval must bind that fallback content as well.
	rowsValue := args["rows"]
	if rows, err := normalizeRows(rowsValue); err == nil && len(rows) == 0 {
		if data, ok := args["data"].(map[string]interface{}); ok {
			rowsValue = data["rows"]
		}
	}
	rowsPayload, _ := json.Marshal(rowsValue)
	rowsSum := sha256.Sum256(rowsPayload)
	return map[string]interface{}{
		"sheet":         stringArg(args, "sheet"),
		"range":         stringArg(args, "range"),
		"rows_sha256":   hex.EncodeToString(rowsSum[:]),
		"source_sha256": strings.ToLower(stringArg(args, "source_sha256")),
		"result_handle": stringArg(args, "result_handle"),
	}
}

func (m *Manager) bindExportHandle(args map[string]interface{}, ownerID, sessionID string) (QueryResult, error) {
	if m == nil || args == nil {
		return QueryResult{}, nil
	}
	handle := strings.TrimSpace(stringArg(args, "result_handle"))
	if handle == "" {
		return QueryResult{}, nil
	}
	// Model-supplied profile_id must not choose the classification/policy
	// applied to a handle-backed export.
	delete(args, "profile_id")
	connectionID := stringArg(args, "connection_id")
	snapshot, err := m.SnapshotResultHandle(handle, ownerID, sessionID, connectionID)
	if err != nil {
		return QueryResult{}, err
	}
	if snapshot.RowCount == 0 {
		return QueryResult{}, fmt.Errorf("syntax: result handle has no rows")
	}
	args["rows"] = snapshot.Rows
	if connectionID == "" && snapshot.ConnectionID != "" {
		args["connection_id"] = snapshot.ConnectionID
	}
	if snapshot.ProfileID != "" {
		args["profile_id"] = snapshot.ProfileID
	}
	return snapshot, nil
}

func batchParamsMap(statements []BatchStatement) map[string]interface{} {
	values := make(map[string]interface{}, len(statements))
	for i, statement := range statements {
		values[fmt.Sprintf("%d", i)] = statement.Params
	}
	return values
}

func marshal(value interface{}) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return "database result encode failed: " + err.Error()
	}
	return string(payload)
}

func secretRetryableAuth(err error) bool {
	classified := classify(err)
	if connectErrorClass(classified) != "authentication" {
		return false
	}
	msg := strings.ToLower(classified.Error())
	// TLS/material setup failures share the authentication prefix but cannot
	// be fixed by re-entering the database password.
	return !strings.Contains(msg, "tls")
}

func connectFailureResult(manager *Manager, profileID string, err error) string {
	classified := classify(err)
	class := connectErrorClass(classified)
	payload := map[string]interface{}{
		"ok":         false,
		"error":      class,
		"message":    classified.Error(),
		"profile_id": profileID,
		"guidance":   "keep using the database tool, do not fall back to ssh or bash mysql",
	}
	if secretRetryableAuth(classified) {
		if draft := secretDraftFromProfile(manager, profileID); draft != nil {
			draft["update_secret"] = true
			payload["needs_secret"] = true
			payload["draft"] = draft
			payload["guidance"] = "宿主已在任务面板打开密码表单。请更新密码后再次 connect，不要用 ssh 或 bash mysql。"
		}
	}
	return marshal(payload)
}

func secretDraftFromProfile(manager *Manager, id string) map[string]interface{} {
	if manager == nil {
		return nil
	}
	p, ok := manager.ProfileByID(id)
	if !ok {
		return nil
	}
	draft := map[string]interface{}{
		"id":                  p.ID,
		"name":                p.Name,
		"type":                p.Type,
		"host":                p.Host,
		"port":                p.Port,
		"database":            p.Database,
		"username":            p.Username,
		"read_only":           p.ReadOnly,
		"allow_external_host": p.AllowExternalHost,
	}
	if path := strings.TrimSpace(p.FilePath); path != "" {
		draft["file_path"] = path
	}
	return draft
}

func hasWarning(warnings []string, wanted string) bool {
	for _, warning := range warnings {
		if warning == wanted {
			return true
		}
	}
	return false
}
