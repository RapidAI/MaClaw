package ha

import "time"

type StaticPeer struct {
	NodeID       string
	NodeName     string
	BaseURL      string
	PublicKeyPEM string
}

type PeerRuntimeState struct {
	NodeID        string     `json:"node_id"`
	NodeName      string     `json:"node_name"`
	BaseURL       string     `json:"base_url"`
	PublicKeyPEM  string     `json:"public_key,omitempty"`
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

type AdminStatusView struct {
	Enabled       bool             `json:"enabled"`
	NodeID        string           `json:"node_id"`
	NodeName      string           `json:"node_name"`
	AdvertiseURL  string           `json:"advertise_url"`
	ServiceStatus string           `json:"service_status"`
	QualityScore  int              `json:"quality_score"`
	Routable      bool             `json:"routable"`
	Cluster       AdminClusterView `json:"cluster"`
	Sync          AdminSyncView    `json:"sync"`
	Peers         []AdminPeerView  `json:"peers"`
	GeneratedAt   time.Time        `json:"generated_at"`
}

type AdminClusterView struct {
	ReachableNodes int    `json:"reachable_nodes"`
	TotalNodes     int    `json:"total_nodes"`
	QuorumSize     int    `json:"quorum_size"`
	Status         string `json:"status"`
}

type AdminSyncView struct {
	Enabled                         bool                    `json:"enabled"`
	MaxOpSeq                        int64                   `json:"max_op_seq"`
	PushDebounceSeconds             int64                   `json:"push_debounce_seconds"`
	HeartbeatSyncMinIntervalSeconds int64                   `json:"heartbeat_sync_min_interval_seconds"`
	LastSuccessAt                   *time.Time              `json:"last_success_at,omitempty"`
	Details                         []AdminSyncCategoryView `json:"details,omitempty"`
}

type AdminSyncCategoryView struct {
	Key          string                      `json:"key"`
	Label        string                      `json:"label"`
	Status       string                      `json:"status"`
	PendingPeers int                         `json:"pending_peers"`
	ErrorPeers   int                         `json:"error_peers"`
	PendingOps   int64                       `json:"pending_ops"`
	LocalRecords int64                       `json:"local_records"`
	LastOpSeq    int64                       `json:"last_op_seq"`
	LastOpAt     *time.Time                  `json:"last_op_at,omitempty"`
	Peers        []AdminSyncCategoryPeerView `json:"peers,omitempty"`
}

type AdminSyncCategoryPeerView struct {
	NodeID              string     `json:"node_id"`
	NodeName            string     `json:"node_name"`
	Status              string     `json:"status"`
	PendingOps          int64      `json:"pending_ops"`
	CursorLastPulledSeq int64      `json:"cursor_last_pulled_seq"`
	CursorLastSuccessAt *time.Time `json:"cursor_last_success_at,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
}

type AdminPeerView struct {
	NodeID              string     `json:"node_id"`
	NodeName            string     `json:"node_name"`
	BaseURL             string     `json:"base_url"`
	PublicKeyPEM        string     `json:"public_key,omitempty"`
	Reachable           bool       `json:"reachable"`
	RTTMs               int64      `json:"rtt_ms"`
	QualityScore        int        `json:"quality_score"`
	ServiceStatus       string     `json:"service_status"`
	ClusterStatus       string     `json:"cluster_status"`
	LagSeconds          int64      `json:"lag_seconds"`
	Backlog             int64      `json:"backlog"`
	LastCheckedAt       *time.Time `json:"last_checked_at,omitempty"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	CursorLastPulledSeq int64      `json:"cursor_last_pulled_seq"`
	CursorLastPulledAt  *time.Time `json:"cursor_last_pulled_at,omitempty"`
	CursorLastSuccessAt *time.Time `json:"cursor_last_success_at,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
}
