package httpthreat

const (
	MaxPreviewRunes      = 800
	MaxRequestBodyBytes  = 2048
	HeadDim              = 256
	DefaultTau           = 0.55
	CanaryPercent        = 5
	GateMinReviews       = 200
	GateMinAccuracy      = 0.85
	GateMinRecall        = 0.80
	BatchLabelLimit      = 50
	DefaultCorpusCap     = 4000
	DefaultAutoGoldShare = 0.35
	DefaultAuditCap      = 200
	MapVersion           = "httpthreat-vocab-v1"
	RestPrefix           = "enc:v1:"

	PipelineOff     = "off"
	PipelineShadow  = "shadow"
	PipelineCanary  = "canary"
	PipelineOn      = "on"
	PromoteOverride = "PROMOTE"

	ClassBenign    = "benign"
	ClassScan      = "scan"
	ClassExploit   = "exploit"
	ClassAuthAbuse = "auth_abuse"
	ClassMalware   = "malware"
	ClassExfil     = "exfil"
	ClassFraud     = "fraud"
	ClassAbuse     = "abuse"
	ClassUnknown   = "unknown"

	ActionAllow     = "allow"
	ActionObserve   = "observe"
	ActionRateLimit = "ratelimit"
	ActionChallenge = "challenge"
	ActionBlock     = "block"

	SourceSignature = "signature"
	SourceIntel     = "intel"
	SourceSession   = "session"
	SourceHeuristic = "heuristic"
	SourceFallback  = "fallback"
	SourceHead      = "head"

	GoldAuto  = "auto"
	GoldHuman = "human"
	GoldLLM   = "llm"

	RoleAnalyst = "analyst"
	RoleAdmin   = "admin"
)

// TrainableClasses is the frozen W rows. unknown is reject-not-a-row.
var TrainableClasses = []string{
	ClassBenign, ClassScan, ClassExploit, ClassAuthAbuse,
	ClassMalware, ClassExfil, ClassFraud, ClassAbuse,
}

var HighRiskClasses = []string{ClassExploit, ClassMalware, ClassExfil}

// Transaction is one finished HTTP detect input (request-side; empty status/body allowed).
type Transaction struct {
	TenantID    string
	SiteID      string
	SourceID    string
	Upgrade     string
	Method      string
	Host        string
	Path        string
	Query       string
	UserAgent   string
	ContentType string
	Status      string
	Referer     string
	Body        string
	Headers     map[string]string
}

// Decision is the hot-path result.
type Decision struct {
	RuleClass   string
	RuleSource  string
	RuleID      string
	RuleAction  string
	HeadClass   string
	HeadMaxP    float64
	HeadProbs   map[string]float64
	Class       string
	Source      string
	Action      string
	HeadUsed    bool
	Preview     string
	SampleID    string
	Pipeline    string
	ServingHash string
	Demoted     bool
}

// Sample is one corpus row.
type Sample struct {
	ID         string
	TenantID   string
	EncoderID  string
	Preview    string
	Embedding  []float32
	RuleClass  string
	RuleSource string
	RuleID     string
	SiteID     string
	HeadClass  string
	HeadMaxP   float64
	GoldClass  string
	GoldSource string
	LabeledAt  string
	LastSeen   string
	CreatedAt  string
	LLMClass   string
	LLMReason  string
	NeedHuman  bool
	Abstained  bool
}

// LabelRequest is a human/LLM label write.
type LabelRequest struct {
	TenantID   string `json:"tenant_id,omitempty"`
	SampleID   string `json:"sample_id"`
	GoldClass  string `json:"gold_class"`
	GoldSource string `json:"gold_source,omitempty"`
	Role       string `json:"role,omitempty"`
	Abstain    bool   `json:"abstain,omitempty"`
}

// BatchLabelRequest labels one filter result with one class. Cap is BatchLabelLimit.
type BatchLabelRequest struct {
	SampleIDs []string `json:"sample_ids"`
	GoldClass string   `json:"gold_class"`
	Role      string   `json:"role,omitempty"`
}

// ClassProb is one softmax row for the review card.
type ClassProb struct {
	Class string  `json:"class"`
	P     float64 `json:"p"`
}

// QueueItem is one review card: preview, scores, and the action if promoted.
type QueueItem struct {
	Sample
	HeadProbs   []ClassProb `json:"head_probs,omitempty"`
	WouldAction string      `json:"would_action,omitempty"`
	QueueReason string      `json:"queue_reason,omitempty"`
}

// NodeIdentity is the authenticated detect-node principal.
type NodeIdentity struct {
	TenantID string
	NodeID   string
}

// StatusView is the operator status bar (human reasons, not raw internals).
type StatusView struct {
	Pipeline       string   `json:"pipeline"`
	Training       bool     `json:"training"`
	EncoderID      string   `json:"encoder_id"`
	EncoderReady   bool     `json:"encoder_ready"`
	ServingReady   bool     `json:"serving_ready"`
	ServingHash    string   `json:"serving_hash,omitempty"`
	CandidateHash  string   `json:"candidate_hash,omitempty"`
	Distributed    bool     `json:"distributed"`
	QueueCount     int      `json:"queue_count"`
	WhyNotPromote  []string `json:"why_not_promote,omitempty"`
	OverrideNote   string   `json:"override_note,omitempty"`
	CanTrain       bool     `json:"can_train"`
	CannotTrain    []string `json:"cannot_train,omitempty"`
	GoldAuto       int      `json:"gold_auto"`
	GoldHuman      int      `json:"gold_human"`
	GoldLLM        int      `json:"gold_llm"`
	TargetNodes    []string `json:"target_nodes,omitempty"`
	PendingNodes   []string `json:"pending_nodes,omitempty"`
	DropCount      int      `json:"drop_count"`
	NATHint        string   `json:"nat_hint,omitempty"`
	MapVersion     string   `json:"map_version,omitempty"`
	CorpusCap      int      `json:"corpus_cap"`
	CorpusWritable bool     `json:"corpus_writable"`
	ExportEnabled  bool     `json:"export_enabled"`
	SafetyValve    bool     `json:"safety_valve,omitempty"`
	UsingPrevious  bool     `json:"using_previous,omitempty"`
	Role           string   `json:"role,omitempty"`
	TrainError     string   `json:"train_error,omitempty"`
	LLMReady       bool     `json:"llm_ready,omitempty"`
}

// AuditRow is one recent disposition (not corpus). Used for "this is business".
type AuditRow struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	RuleClass string `json:"rule_class"`
	RuleID    string `json:"rule_id,omitempty"`
	HeadClass string `json:"head_class,omitempty"`
	Preview   string `json:"preview"`
	At        string `json:"at"`
}

// RuleHit is one rule-ops row: hits plus optional teacher remap.
type RuleHit struct {
	RuleID string `json:"rule_id"`
	Class  string `json:"class,omitempty"`
	Hits   int    `json:"hits"`
}

// IntelHost is a tenant-scoped P1 teacher row.
type IntelHost struct {
	Host  string `json:"host"`
	Class string `json:"class"`
}

// RuleMapView is the rule-ops read model. The class-head page only shows version.
type RuleMapView struct {
	Version string      `json:"version"`
	Entries []RuleHit   `json:"entries"`
	Intel   []IntelHost `json:"intel,omitempty"`
	Sites   []string    `json:"sites,omitempty"`
}

// SampleView is a read-only trainer fill for TrainRun ids. No embeddings.
type SampleView struct {
	ID         string `json:"id"`
	Preview    string `json:"preview"`
	RuleClass  string `json:"rule_class,omitempty"`
	RuleID     string `json:"rule_id,omitempty"`
	GoldClass  string `json:"gold_class,omitempty"`
	GoldSource string `json:"gold_source,omitempty"`
	HeadClass  string `json:"head_class,omitempty"`
}

// ServingBundle is the node pull: weights + hashes, never previews.
type ServingBundle struct {
	Pipeline     string            `json:"pipeline"`
	Hash         string            `json:"hash,omitempty"`
	PreviousHash string            `json:"previous_hash,omitempty"`
	Serving      *Head             `json:"serving,omitempty"`
	Previous     *Head             `json:"previous,omitempty"`
	IntelHosts   map[string]string `json:"intel_hosts,omitempty"`
	RuleMap      map[string]string `json:"rule_map,omitempty"`
	Sites        []string          `json:"sites,omitempty"`
}

// IngestRequest is node delivery. Decision+embedding skip a second embed.
type IngestRequest struct {
	Transaction
	Decision  *Decision `json:"decision,omitempty"`
	Embedding []float32 `json:"embedding,omitempty"`
	EncoderID string    `json:"encoder_id,omitempty"`
}

type ExportRow struct {
	ID         string `json:"id"`
	RuleClass  string `json:"rule_class"`
	RuleID     string `json:"rule_id,omitempty"`
	GoldClass  string `json:"gold_class,omitempty"`
	GoldSource string `json:"gold_source,omitempty"`
	Preview    string `json:"preview"`
}

// ObserveReport is bypass cross-check. Agreement is observation, not a gate.
type ObserveReport struct {
	Compared    int                       `json:"compared"`
	Agree       int                       `json:"agree"`
	Rewrite     int                       `json:"would_rewrite"`
	LowConf     int                       `json:"low_confidence"`
	Cross       map[string]map[string]int `json:"cross,omitempty"`
	Note        string                    `json:"note"`
	RewriteRate float64                   `json:"rewrite_rate"`
	BlockRate   float64                   `json:"block_rate"`
	UnlabelRate float64                   `json:"unlabel_rate"`
	Kind        string                    `json:"kind,omitempty"`
}
