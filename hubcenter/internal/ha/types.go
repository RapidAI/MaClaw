package ha

import "time"

type StaticPeer struct {
	NodeID   string
	NodeName string
	BaseURL  string
}

type PeerRuntimeState struct {
	NodeID        string     `json:"node_id"`
	NodeName      string     `json:"node_name"`
	BaseURL       string     `json:"base_url"`
	Reachable     bool       `json:"reachable"`
	RTTMs         int64      `json:"rtt_ms"`
	QualityScore  int        `json:"quality_score"`
	ServiceStatus string     `json:"service_status"`
	ClusterStatus string     `json:"cluster_status"`
	LagSeconds    int64      `json:"lag_seconds"`
	Backlog       int64      `json:"backlog"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

type ClientQualityView struct {
	OK            bool   `json:"ok"`
	NodeID        string `json:"node_id"`
	NodeName      string `json:"node_name"`
	ServiceStatus string `json:"service_status"`
	QualityScore  int    `json:"quality_score"`
	Routable      bool   `json:"routable"`
	Cluster       struct {
		ReachableNodes int    `json:"reachable_nodes"`
		TotalNodes     int    `json:"total_nodes"`
		Status         string `json:"status"`
	} `json:"cluster"`
	Sync struct {
		Enabled       bool       `json:"enabled"`
		LagSeconds    int64      `json:"lag_seconds"`
		Backlog       int64      `json:"backlog"`
		LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	} `json:"sync"`
	Features struct {
		CanRegister  bool `json:"can_register"`
		CanHeartbeat bool `json:"can_heartbeat"`
		CanResolve   bool `json:"can_resolve"`
	} `json:"features"`
	ServerTime time.Time `json:"server_time"`
	TTLSeconds int       `json:"ttl_seconds"`
}

type EndpointView struct {
	NodeID        string `json:"node_id"`
	NodeName      string `json:"node_name"`
	BaseURL       string `json:"base_url"`
	ServiceStatus string `json:"service_status"`
	QualityScore  int    `json:"quality_score"`
	Routable      bool   `json:"routable"`
}

type EndpointsView struct {
	OK         bool           `json:"ok"`
	Nodes      []EndpointView `json:"nodes"`
	TTLSeconds int            `json:"ttl_seconds"`
	ServerTime time.Time      `json:"server_time"`
}
