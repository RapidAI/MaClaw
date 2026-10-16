package guiapp

import coreconfig "github.com/RapidAI/CodeClaw/corelib/config"

// AppConfigSchemaResponse is the Wails-facing projection of the canonical
// corelib AppConfig schema. The frontend can render settings from this one
// source instead of maintaining another field-name list.
type AppConfigSchemaResponse struct {
	SchemaVersion string                              `json:"schema_version"`
	Fields        []coreconfig.AppConfigFieldMetadata `json:"fields"`
}

// GetAppConfigSchema returns the versioned shared configuration schema.
// GUI-only policy is already encoded by corelib/config; this method performs
// no additional field filtering that could drift from MaClawSrv.
func (a *App) GetAppConfigSchema() AppConfigSchemaResponse {
	return AppConfigSchemaResponse{
		SchemaVersion: coreconfig.AppConfigSchemaVersion,
		Fields:        coreconfig.AppConfigSchema(),
	}
}
