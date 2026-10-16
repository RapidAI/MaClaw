// Package database provides the policy-aware data-source layer used by the
// Agent database tool.  It intentionally has no dependency on a host (GUI,
// TUI, or HTTP service), so every host can share the same connection and
// result semantics.
package database

import (
	"context"
	"errors"
	"time"
)

const ContractVersion = 1

// ErrManagerClosed is returned when a host tries to use a database manager
// after its owning Runtime/executor has been shut down.  Keeping this as a
// stable sentinel lets GUI, MaClawSrv and TUI map shutdown races to the same
// fail-closed outcome instead of accidentally reopening a connection.
var ErrManagerClosed = errors.New("database manager is closed")

type SourceType string

const (
	SourceMySQL     SourceType = "mysql"
	SourcePostgres  SourceType = "postgres"
	SourceSQLServer SourceType = "sqlserver"
	SourceAccess    SourceType = "access"
	SourceExcel     SourceType = "excel"
)

type Profile struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	SchemaVersion int        `json:"schema_version,omitempty"`
	Type          SourceType `json:"type"`
	Host          string     `json:"host,omitempty"`
	Port          int        `json:"port,omitempty"`
	Database      string     `json:"database,omitempty"`
	Username      string     `json:"username,omitempty"`
	SecretRef     string     `json:"secret_ref,omitempty"`
	DefaultSchema string     `json:"default_schema,omitempty"`
	FilePath      string     `json:"file_path,omitempty"`
	Sheet         string     `json:"sheet,omitempty"`
	DSN           string     `json:"dsn,omitempty"`
	ReadOnly      bool       `json:"read_only"`
	// WriteEnabled is an explicit opt-in. ReadOnly remains for backwards
	// compatibility, but a zero-value profile is now safe by default.
	WriteEnabled      bool     `json:"write_enabled,omitempty"`
	AllowDDL          bool     `json:"allow_ddl,omitempty"`
	AllowedSchemas    []string `json:"allowed_schemas,omitempty"`
	AllowedTables     []string `json:"allowed_tables,omitempty"`
	AllowedOperations []string `json:"allowed_operations,omitempty"`
	DeniedColumns     []string `json:"denied_columns,omitempty"`
	MaskedColumns     []string `json:"masked_columns,omitempty"`
	MaxAffectedRows   int64    `json:"max_affected_rows,omitempty"`
	AllowExternalHost bool     `json:"allow_external_host,omitempty"`
	// Disabled is the per-profile kill switch. Disabled profiles stay in the
	// registry for audit/config but reject connect and invalidate live sessions.
	Disabled bool `json:"disabled,omitempty"`
	// TLS is optional. Empty Mode keeps the driver default; production profiles
	// should set require or verify-full.
	TLS TLSSettings `json:"tls,omitempty"`
	// Password is accepted only during v1 secret migration. After a successful
	// write to the secret provider it must be cleared; a leftover value
	// fail-closes the database tool.
	Password string `json:"password,omitempty"`
	// DataClassification is an optional export gate (public, internal,
	// confidential, restricted). confidential/restricted exports require a
	// bound connection_id so profile policy applies.
	DataClassification string `json:"data_classification,omitempty"`
	// SSHSessionID binds this profile to an already-approved, live SSH
	// session. The database tool never opens SSH connections or sees SSH
	// credentials; hosts inject a TunnelDialer that reuses the session.
	SSHSessionID string `json:"ssh_session_id,omitempty"`
	// ReplicaHost, when set, receives read traffic (query/inspect/explain).
	// Writes always use Host. Replica uses the same database, username,
	// secret_ref and TLS as the primary.
	ReplicaHost string `json:"replica_host,omitempty"`
	ReplicaPort int    `json:"replica_port,omitempty"`
	// ReplicaSSHSessionID optionally tunnels replica reads through a
	// different already-approved SSH session than the primary.
	ReplicaSSHSessionID string `json:"replica_ssh_session_id,omitempty"`
}

// TLSSettings controls driver TLS. Mode is disable, require, or verify-full.
type TLSSettings struct {
	Mode   string `json:"mode,omitempty"`
	CAFile string `json:"ca_file,omitempty"`
}

// ProfileSummary is the model-facing projection used by list_connections.
// Host/catalog/username are included so conversational text can pick among
// multiple sources. Paths, DSN, secret_ref and SSH session ids stay host-side.
type ProfileSummary struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Type         SourceType `json:"type"`
	Status       string     `json:"status"`
	Host         string     `json:"host,omitempty"`
	Port         int        `json:"port,omitempty"`
	Database     string     `json:"database,omitempty"`
	Username     string     `json:"username,omitempty"`
	ReadOnly     bool       `json:"read_only"`
	WriteEnabled bool       `json:"write_enabled"`
}

type Capabilities struct {
	Read         bool `json:"read"`
	Write        bool `json:"write"`
	Transactions bool `json:"transactions"`
	Cursors      bool `json:"cursors"`
	Explain      bool `json:"explain"`
	MaxPageSize  int  `json:"max_page_size"`
}

type QueryRequest struct {
	SQL              string
	Params           map[string]interface{}
	PositionalParams []interface{}
	ParameterMode    string
	Limit            int
	Cursor           string
	Timeout          int
	Explain          bool
}

type QueryResult struct {
	ContractVersion int             `json:"contract_version"`
	ProfileID       string          `json:"profile_id,omitempty"`
	ConnectionID    string          `json:"connection_id,omitempty"`
	Columns         []Column        `json:"columns"`
	Rows            [][]interface{} `json:"rows"`
	RowCount        int             `json:"row_count"`
	Truncated       bool            `json:"truncated"`
	NextCursor      string          `json:"next_cursor,omitempty"`
	ResultHandle    string          `json:"result_handle,omitempty"`
	Warnings        []string        `json:"warnings,omitempty"`
	ElapsedMS       int64           `json:"elapsed_ms"`
	allRows         [][]interface{} // manager-owned paging source; never serialized
}

// AsyncJobStatus is the model-facing envelope for background queries.
// ResultHandle is the same owner-bound token used by query cursors.
type AsyncJobStatus struct {
	ContractVersion int      `json:"contract_version"`
	JobID           string   `json:"job_id"`
	Status          string   `json:"status"`
	ProfileID       string   `json:"profile_id,omitempty"`
	ConnectionID    string   `json:"connection_id,omitempty"`
	ResultHandle    string   `json:"result_handle,omitempty"`
	RowCount        int      `json:"row_count,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
	Error           string   `json:"error,omitempty"`
	ElapsedMS       int64    `json:"elapsed_ms,omitempty"`
}

type Column struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type ExecuteRequest struct {
	SQL              string
	Params           map[string]interface{}
	PositionalParams []interface{}
	ParameterMode    string
	DryRun           bool
	ApprovalToken    string
	MaxAffectedRows  int64
}

// BatchStatement is one parameterized mutation in a batch transaction. It is
// deliberately transport-neutral so GUI, srv and TUI can share validation and
// rollback semantics without passing raw request envelopes between packages.
type BatchStatement struct {
	SQL              string
	Params           map[string]interface{}
	PositionalParams []interface{}
	ParameterMode    string
	MaxAffectedRows  int64
}

type BatchExecuteRequest struct {
	Statements    []BatchStatement
	DryRun        bool
	ApprovalToken string
	MaxStatements int
}

type MutationResult struct {
	ContractVersion int      `json:"contract_version"`
	ProfileID       string   `json:"profile_id,omitempty"`
	MatchedRows     int64    `json:"matched_rows"`
	AffectedRows    int64    `json:"affected_rows"`
	DryRun          bool     `json:"dry_run"`
	DryRunGuarantee string   `json:"dry_run_guarantee"`
	CommitID        string   `json:"commit_id,omitempty"`
	ReceiptID       string   `json:"receipt_id,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
	Plan            *DDLPlan `json:"plan,omitempty"`
}

// DDLPlan is a no-execute preview of a schema-change statement. It is the
// verified substitute for rolling back DDL, which may implicitly commit.
type DDLPlan struct {
	Operation     string      `json:"operation"`
	Targets       []string    `json:"targets,omitempty"`
	Destructive   bool        `json:"destructive"`
	CurrentTables []TableInfo `json:"current_tables,omitempty"`
	Warnings      []string    `json:"warnings,omitempty"`
}

// ExplainResult is the model-facing envelope for explain and NL→SQL
// preview. SQL explain returns a plan grid; prompt-only preview returns
// schema + guidance and never executes user SQL.
type ExplainResult struct {
	ContractVersion int             `json:"contract_version"`
	ProfileID       string          `json:"profile_id,omitempty"`
	ConnectionID    string          `json:"connection_id,omitempty"`
	Dialect         string          `json:"dialect,omitempty"`
	Prompt          string          `json:"prompt,omitempty"`
	SQL             string          `json:"sql,omitempty"`
	Columns         []Column        `json:"columns,omitempty"`
	Rows            [][]interface{} `json:"rows,omitempty"`
	RowCount        int             `json:"row_count,omitempty"`
	Tables          []TableInfo     `json:"tables,omitempty"`
	Guidance        string          `json:"guidance,omitempty"`
	Warnings        []string        `json:"warnings,omitempty"`
	ElapsedMS       int64           `json:"elapsed_ms,omitempty"`
}

// QueryFavorite is an owner-bound, parameterized read-only SQL snippet.
// It never stores credentials or positional secrets.
type QueryFavorite struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ProfileID string    `json:"profile_id,omitempty"`
	SQL       string    `json:"sql"`
	OwnerID   string    `json:"owner_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type InspectRequest struct{ Table string }
type SchemaInfo struct {
	ContractVersion int          `json:"contract_version"`
	ProfileID       string       `json:"profile_id,omitempty"`
	Dialect         string       `json:"dialect"`
	Capabilities    Capabilities `json:"capabilities"`
	Tables          []TableInfo  `json:"tables,omitempty"`
	Warnings        []string     `json:"warnings,omitempty"`
}
type TableInfo struct {
	Schema  string   `json:"schema,omitempty"`
	Name    string   `json:"name"`
	Columns []Column `json:"columns,omitempty"`
}

type Adapter interface {
	Ping(context.Context) error
	Inspect(context.Context, InspectRequest) (SchemaInfo, error)
	Query(context.Context, QueryRequest) (QueryResult, error)
	Execute(context.Context, ExecuteRequest) (MutationResult, error)
	Capabilities() Capabilities
	Close() error
}

// BatchAdapter is optional so existing third-party adapters remain source
// compatible. Adapters that do not implement it reject batch_execute with a
// stable unsupported_capability result.
type BatchAdapter interface {
	ExecuteBatch(context.Context, BatchExecuteRequest) (MutationResult, error)
}

type SecretResolver func(context.Context, string) (string, error)

// AuditEvent is intentionally metadata-only. SQL text and parameter values
// are represented by a fingerprint/count so sinks can persist events without
// creating a second sensitive data store.
type AuditEvent struct {
	Timestamp      time.Time `json:"timestamp"`
	OwnerID        string    `json:"owner_id,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	ProfileID      string    `json:"profile_id,omitempty"`
	ConnectionID   string    `json:"connection_id,omitempty"`
	Action         string    `json:"action"`
	SQLFingerprint string    `json:"sql_fingerprint,omitempty"`
	ParameterCount int       `json:"parameter_count,omitempty"`
	AffectedRows   int64     `json:"affected_rows,omitempty"`
	Risk           string    `json:"risk,omitempty"`
	ResultClass    string    `json:"result_class,omitempty"`
	ApprovalID     string    `json:"approval_id,omitempty"`
	ReceiptID      string    `json:"receipt_id,omitempty"`
	OperationID    string    `json:"operation_id,omitempty"`
	Attempt        int       `json:"attempt,omitempty"`
	ParentActionID string    `json:"parent_action_id,omitempty"`
}

type AuditSink func(context.Context, AuditEvent)
