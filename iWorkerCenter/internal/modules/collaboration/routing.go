package collaboration

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	colleagueDomain "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/domain"
	colleagueRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/repo"
)

const (
	StrategyLeastLoaded  = "least_loaded"
	StrategyPrimaryFirst = "primary_first"

	RuntimeStateActive    = "active"
	RuntimeStateStandby   = "standby"
	RuntimeStateUnhealthy = "unhealthy"

	DefaultHeartbeatTimeoutSeconds = 90
)

const routingSettingsKeyPrefix = "iwc_collaboration_routing:"

type RoutingSettings struct {
	DefaultStrategy          string            `json:"default_strategy"`
	RoleStrategies           map[string]string `json:"role_strategies"`
	PrimaryColleagueByRole   map[string]string `json:"primary_colleague_by_role"`
	RuntimeStateByColleague  map[string]string `json:"runtime_state_by_colleague"`
	LastHeartbeatByColleague map[string]string `json:"last_heartbeat_by_colleague"`
	HeartbeatTimeoutSeconds  int               `json:"heartbeat_timeout_seconds"`
}

type RoutingColleagueStatus struct {
	ColleagueID    string `json:"colleague_id"`
	ManualState    string `json:"manual_state"`
	EffectiveState string `json:"effective_state"`
	Reason         string `json:"reason"`
}

type RoutingOverview struct {
	Settings          RoutingSettings                   `json:"settings"`
	ActiveCount       int                               `json:"active_count"`
	StandbyCount      int                               `json:"standby_count"`
	UnhealthyCount    int                               `json:"unhealthy_count"`
	StatusByColleague map[string]RoutingColleagueStatus `json:"status_by_colleague"`
}

func DefaultRoutingSettings() RoutingSettings {
	return RoutingSettings{
		DefaultStrategy:          StrategyLeastLoaded,
		RoleStrategies:           map[string]string{},
		PrimaryColleagueByRole:   map[string]string{},
		RuntimeStateByColleague:  map[string]string{},
		LastHeartbeatByColleague: map[string]string{},
		HeartbeatTimeoutSeconds:  DefaultHeartbeatTimeoutSeconds,
	}
}

func normalizeStrategy(strategy string) string {
	switch strings.TrimSpace(strings.ToLower(strategy)) {
	case StrategyPrimaryFirst:
		return StrategyPrimaryFirst
	default:
		return StrategyLeastLoaded
	}
}

func normalizeRuntimeState(state string) string {
	switch strings.TrimSpace(strings.ToLower(state)) {
	case RuntimeStateStandby:
		return RuntimeStateStandby
	case RuntimeStateUnhealthy:
		return RuntimeStateUnhealthy
	default:
		return RuntimeStateActive
	}
}

func routingSettingsKey(tenantID string) string {
	return routingSettingsKeyPrefix + tenantID
}

func (r *Repo) LoadRoutingSettings(tenantID string) (RoutingSettings, error) {
	settings := DefaultRoutingSettings()
	if strings.TrimSpace(tenantID) == "" {
		return settings, nil
	}
	var raw string
	err := r.read.QueryRow(`SELECT value_json FROM system_settings WHERE key=?`, routingSettingsKey(tenantID)).Scan(&raw)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return settings, nil
		}
		return settings, err
	}
	if strings.TrimSpace(raw) == "" {
		return settings, nil
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return DefaultRoutingSettings(), nil
	}
	settings = normalizeRoutingSettings(settings)
	return settings, nil
}

func (r *Repo) SaveRoutingSettings(tenantID string, settings RoutingSettings) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("tenant_id is required")
	}
	settings = normalizeRoutingSettings(settings)
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal routing settings: %w", err)
	}
	now := time.Now().Format(time.RFC3339)
	_, err = r.write.Exec(
		`INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json, updated_at=excluded.updated_at`,
		routingSettingsKey(tenantID), string(data), now,
	)
	return err
}

func normalizeRoutingSettings(settings RoutingSettings) RoutingSettings {
	settings.DefaultStrategy = normalizeStrategy(settings.DefaultStrategy)
	if settings.RoleStrategies == nil {
		settings.RoleStrategies = map[string]string{}
	}
	if settings.PrimaryColleagueByRole == nil {
		settings.PrimaryColleagueByRole = map[string]string{}
	}
	if settings.RuntimeStateByColleague == nil {
		settings.RuntimeStateByColleague = map[string]string{}
	}
	if settings.LastHeartbeatByColleague == nil {
		settings.LastHeartbeatByColleague = map[string]string{}
	}
	if settings.HeartbeatTimeoutSeconds <= 0 {
		settings.HeartbeatTimeoutSeconds = DefaultHeartbeatTimeoutSeconds
	}
	for roleCode, strategy := range settings.RoleStrategies {
		settings.RoleStrategies[roleCode] = normalizeStrategy(strategy)
	}
	for colleagueID, state := range settings.RuntimeStateByColleague {
		settings.RuntimeStateByColleague[colleagueID] = normalizeRuntimeState(state)
	}
	return settings
}

func ResolveRoleAssignee(repo *Repo, colleagueRp *colleagueRepo.ColleagueRepo, tenantID, roleCode string) (*colleagueDomain.Colleague, string, error) {
	if colleagueRp == nil {
		return nil, StrategyLeastLoaded, fmt.Errorf("role-based routing is unavailable")
	}
	colleagues, err := colleagueRp.ListByRoleCode(tenantID, roleCode)
	if err != nil {
		return nil, StrategyLeastLoaded, fmt.Errorf("resolve role %s: %w", roleCode, err)
	}
	if len(colleagues) == 0 {
		return nil, StrategyLeastLoaded, fmt.Errorf("no active colleague found for role %s", roleCode)
	}
	settings := DefaultRoutingSettings()
	if repo != nil {
		loaded, loadErr := repo.LoadRoutingSettings(tenantID)
		if loadErr == nil {
			settings = loaded
		}
	}
	strategy := normalizeStrategy(settings.RoleStrategies[roleCode])
	if strategy == StrategyLeastLoaded && settings.RoleStrategies[roleCode] == "" {
		strategy = normalizeStrategy(settings.DefaultStrategy)
	}

	activeCandidates, standbyCandidates := splitHealthyCandidates(colleagues, settings)
	if len(activeCandidates) == 0 && len(standbyCandidates) == 0 {
		return nil, strategy, fmt.Errorf("no healthy colleague found for role %s", roleCode)
	}

	if strategy == StrategyPrimaryFirst {
		primaryID := strings.TrimSpace(settings.PrimaryColleagueByRole[roleCode])
		if primaryID != "" {
			if primary := findColleagueByID(activeCandidates, primaryID); primary != nil {
				return primary, strategy, nil
			}
			if primary := findColleagueByID(standbyCandidates, primaryID); primary != nil {
				return primary, strategy, nil
			}
		}
	}

	if len(activeCandidates) > 0 {
		selected, pickErr := pickLeastLoadedColleague(repo, tenantID, activeCandidates)
		if pickErr == nil {
			return selected, strategy, nil
		}
	}
	selected, err := pickLeastLoadedColleague(repo, tenantID, standbyCandidates)
	if err != nil {
		return nil, strategy, err
	}
	return selected, strategy, nil
}

func splitHealthyCandidates(colleagues []*colleagueDomain.Colleague, settings RoutingSettings) ([]*colleagueDomain.Colleague, []*colleagueDomain.Colleague) {
	activeCandidates := make([]*colleagueDomain.Colleague, 0, len(colleagues))
	standbyCandidates := make([]*colleagueDomain.Colleague, 0, len(colleagues))
	now := time.Now()
	for _, colleague := range colleagues {
		status := routingStatusForColleague(colleague.ID, settings, now)
		switch status.EffectiveState {
		case RuntimeStateUnhealthy:
			continue
		case RuntimeStateStandby:
			standbyCandidates = append(standbyCandidates, colleague)
		default:
			activeCandidates = append(activeCandidates, colleague)
		}
	}
	return activeCandidates, standbyCandidates
}

func BuildRoutingOverview(settings RoutingSettings, colleagues []*colleagueDomain.Colleague, now time.Time) RoutingOverview {
	overview := RoutingOverview{
		Settings:          normalizeRoutingSettings(settings),
		StatusByColleague: map[string]RoutingColleagueStatus{},
	}
	for _, colleague := range colleagues {
		status := routingStatusForColleague(colleague.ID, overview.Settings, now)
		overview.StatusByColleague[colleague.ID] = status
		switch status.EffectiveState {
		case RuntimeStateStandby:
			overview.StandbyCount++
		case RuntimeStateUnhealthy:
			overview.UnhealthyCount++
		default:
			overview.ActiveCount++
		}
	}
	return overview
}

func effectiveRuntimeState(colleagueID string, settings RoutingSettings, now time.Time) string {
	return routingStatusForColleague(colleagueID, settings, now).EffectiveState
}

func routingStatusForColleague(colleagueID string, settings RoutingSettings, now time.Time) RoutingColleagueStatus {
	manualState := normalizeRuntimeState(settings.RuntimeStateByColleague[colleagueID])
	status := RoutingColleagueStatus{
		ColleagueID: colleagueID,
		ManualState: manualState,
	}
	if manualState == RuntimeStateStandby {
		status.EffectiveState = RuntimeStateStandby
		status.Reason = "manual_standby"
		return status
	}
	if manualState == RuntimeStateUnhealthy {
		status.EffectiveState = RuntimeStateUnhealthy
		status.Reason = "manual_unhealthy"
		return status
	}
	if isHeartbeatExpired(settings.LastHeartbeatByColleague[colleagueID], settings.HeartbeatTimeoutSeconds, now) {
		status.EffectiveState = RuntimeStateUnhealthy
		status.Reason = "heartbeat_timeout"
		return status
	}
	if strings.TrimSpace(settings.LastHeartbeatByColleague[colleagueID]) == "" {
		status.EffectiveState = RuntimeStateActive
		status.Reason = "manual_active"
		return status
	}
	status.EffectiveState = RuntimeStateActive
	status.Reason = "heartbeat_healthy"
	return status
}

func isHeartbeatExpired(lastHeartbeat string, timeoutSeconds int, now time.Time) bool {
	if strings.TrimSpace(lastHeartbeat) == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339, lastHeartbeat)
	if err != nil {
		return true
	}
	return now.Sub(at) > time.Duration(timeoutSeconds)*time.Second
}

func findColleagueByID(colleagues []*colleagueDomain.Colleague, colleagueID string) *colleagueDomain.Colleague {
	for _, colleague := range colleagues {
		if colleague.ID == colleagueID {
			return colleague
		}
	}
	return nil
}

func pickLeastLoadedColleague(repo *Repo, tenantID string, colleagues []*colleagueDomain.Colleague) (*colleagueDomain.Colleague, error) {
	if len(colleagues) == 0 {
		return nil, fmt.Errorf("no active colleague found for role")
	}
	if len(colleagues) == 1 || repo == nil {
		return colleagues[0], nil
	}

	ids := make([]string, 0, len(colleagues))
	for _, colleague := range colleagues {
		ids = append(ids, colleague.ID)
	}
	openTasks, err := repo.CountOpenTasksByColleagueIDs(tenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("load role routing state: %w", err)
	}

	selected := colleagues[0]
	selectedLoad := openTasks[selected.ID]
	for _, colleague := range colleagues[1:] {
		load := openTasks[colleague.ID]
		if load < selectedLoad {
			selected = colleague
			selectedLoad = load
		}
	}
	return selected, nil
}
