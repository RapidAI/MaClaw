package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/database"
)

// DatabaseChecks validates configured database profiles without opening a
// network connection or resolving any secret_ref. It is safe to include in a
// normal local doctor run and reports only non-sensitive profile metadata.
func DatabaseChecks(cfg corelib.AppConfig, baseDir string) []Check {
	profiles := cfg.DatabaseProfiles
	if len(profiles) == 0 {
		checks := []Check{{ID: "database.profiles", Status: StatusInfo, Message: "no database profiles configured"}}
		if !database.SchemaContractLocked() {
			checks = append(checks, Check{ID: "database.contract", Status: StatusFail, Message: "database tool schema hash does not match the locked contract", Hint: "Upgrade GUI, TUI and MaClawSrv to the same database tool contract"})
		}
		if !cfg.DatabaseToolIsEnabled() {
			checks = append(checks, Check{ID: "database.enabled", Status: StatusFail, Message: "database tool is disabled by database_tool_enabled"})
		}
		return checks
	}
	checks := make([]Check, 0, len(profiles)+1)
	contractStatus := StatusOK
	contractMessage := "database tool contract is available"
	if !database.SchemaContractLocked() {
		contractStatus = StatusFail
		contractMessage = "database tool schema hash does not match the locked contract"
	}
	contract := Check{ID: "database.contract", Status: contractStatus, Message: contractMessage, Detail: map[string]any{"contract_version": database.ContractVersion, "schema_hash": database.ToolSchemaHash(), "canonical_hash": database.CanonicalToolSchemaHash, "tool_enabled": cfg.DatabaseToolIsEnabled()}}
	if contractStatus == StatusFail {
		contract.Hint = "Upgrade GUI, TUI and MaClawSrv to the same database tool contract"
	}
	checks = append(checks, contract)
	if !cfg.DatabaseToolIsEnabled() {
		checks = append(checks, Check{ID: "database.enabled", Status: StatusFail, Message: "database tool is disabled by database_tool_enabled"})
	}
	checks = append(checks, Check{ID: "database.profiles", Status: StatusOK, Message: fmt.Sprintf("%d database profile(s) configured", len(profiles)), Detail: map[string]any{"count": len(profiles)}})
	seen := make(map[string]struct{}, len(profiles))
	for _, p := range profiles {
		id := strings.TrimSpace(p.ID)
		checkID := "database.profile"
		if id != "" {
			checkID += "." + id
		}
		if _, duplicate := seen[strings.ToLower(id)]; duplicate && id != "" {
			checks = append(checks, Check{ID: checkID, Status: StatusFail, Message: "duplicate database profile id", Detail: map[string]any{"type": p.Type}})
			continue
		}
		if id != "" {
			seen[strings.ToLower(id)] = struct{}{}
		}
		if err := database.ValidateProfile(p); err != nil {
			checks = append(checks, Check{ID: checkID, Status: StatusFail, Message: "invalid database profile: " + redactDatabaseError(err), Detail: map[string]any{"type": p.Type}})
			continue
		}
		if p.Type == database.SourceAccess {
			detection := database.DetectAccessODBC()
			detail := map[string]any{"type": p.Type, "adapter_linked": detection.AdapterLinked, "driver_installed": detection.DriverInstalled, "matching_bitness": detection.MatchingBitness, "process_bitness": detection.ProcessBitness}
			if len(detection.DriverNames) > 0 {
				detail["driver_names"] = detection.DriverNames
			}
			if hint := detection.Hint(); hint != "" {
				checks = append(checks, Check{ID: checkID, Status: StatusFail, Message: "Access profile requires the optional ODBC driver", Hint: hint, Detail: detail})
				continue
			}
		}
		if mode := strings.TrimSpace(p.TLS.Mode); mode != "" && mode != "disable" && mode != "require" && mode != "verify-full" {
			checks = append(checks, Check{ID: checkID, Status: StatusFail, Message: "invalid tls.mode", Detail: map[string]any{"type": p.Type}})
			continue
		}
		if ca := strings.TrimSpace(p.TLS.CAFile); ca != "" {
			caPath := ca
			if !filepath.IsAbs(caPath) && baseDir != "" {
				caPath = filepath.Join(baseDir, caPath)
			}
			if _, err := os.Stat(caPath); err != nil {
				checks = append(checks, Check{ID: checkID, Status: StatusFail, Message: "tls ca_file is not readable", Hint: "Verify tls.ca_file and workspace permissions", Detail: map[string]any{"type": p.Type}})
				continue
			}
		}
		if p.Type == database.SourceAccess || p.Type == database.SourceExcel {
			path := strings.TrimSpace(p.FilePath)
			if !filepath.IsAbs(path) && baseDir != "" {
				path = filepath.Join(baseDir, path)
			}
			if _, err := os.Stat(path); err != nil {
				status := StatusFail
				message := "database file is not readable"
				if os.IsNotExist(err) {
					message = "database file does not exist"
				}
				checks = append(checks, Check{ID: checkID, Status: status, Message: message, Hint: "Verify the profile file path and workspace permissions", Detail: map[string]any{"type": p.Type}})
				continue
			}
		}
		checks = append(checks, Check{ID: checkID, Status: StatusOK, Message: "database profile is structurally valid", Detail: map[string]any{"type": p.Type, "read_only": p.ReadOnly, "write_enabled": p.WriteEnabled, "has_replica": strings.TrimSpace(p.ReplicaHost) != "", "has_ssh_tunnel": strings.TrimSpace(p.SSHSessionID) != "", "has_replica_ssh_tunnel": strings.TrimSpace(p.ReplicaSSHSessionID) != ""}})
	}
	return checks
}

func redactDatabaseError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	// Validation errors are already stable and do not contain DSNs. Keep this
	// helper as a final guard should validation gain richer driver messages.
	for _, secret := range []string{"password=", "pwd=", "secret="} {
		if i := strings.Index(strings.ToLower(message), secret); i >= 0 {
			return strings.TrimSpace(message[:i]) + "[redacted]"
		}
	}
	return message
}
