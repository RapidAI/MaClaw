package browser

import (
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// BrowserTabSnapshot captures a known page target in the browser session.
type BrowserTabSnapshot struct {
	TabID       string `json:"tab_id"`
	Title       string `json:"title,omitempty"`
	URL         string `json:"url,omitempty"`
	Type        string `json:"type,omitempty"`
	Active      bool   `json:"active,omitempty"`
	OpenerTabID string `json:"opener_tab_id,omitempty"`
}

// BrowserFrameSnapshot captures a lightweight frame tree node.
type BrowserFrameSnapshot struct {
	FrameID        string `json:"frame_id"`
	ParentFrameID  string `json:"parent_frame_id,omitempty"`
	URL            string `json:"url,omitempty"`
	Name           string `json:"name,omitempty"`
	SecurityOrigin string `json:"security_origin,omitempty"`
}

// BrowserConsoleEvent is a structured console entry.
type BrowserConsoleEvent struct {
	Type      string `json:"type,omitempty"`
	Level     string `json:"level,omitempty"`
	Text      string `json:"text,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// BrowserNetworkEvent is a structured network summary item.
type BrowserNetworkEvent struct {
	RequestID  string `json:"request_id,omitempty"`
	Method     string `json:"method,omitempty"`
	URL        string `json:"url,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Kind       string `json:"kind,omitempty"`
	CreatedAt  int64  `json:"created_at"`
}

// BrowserTimelineSlice is a session-level timeline summary.
type BrowserTimelineSlice struct {
	Kind      string `json:"kind"`
	Summary   string `json:"summary"`
	CreatedAt int64  `json:"created_at"`
}

// BrowserPolicy constrains what a browser agent session may do.
type BrowserPolicy struct {
	AllowedDomains             []string `json:"allowed_domains,omitempty"`
	BlockedDomains             []string `json:"blocked_domains,omitempty"`
	AllowCrossOriginNavigation bool     `json:"allow_cross_origin_navigation,omitempty"`
	AllowPopup                 bool     `json:"allow_popup,omitempty"`
	AllowDownload              bool     `json:"allow_download,omitempty"`
	AllowUpload                bool     `json:"allow_upload,omitempty"`
	ContentBoundary            bool     `json:"content_boundary,omitempty"`
}

// BrowserBoundingBox describes a visible element rectangle.
type BrowserBoundingBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// BrowserElementRef is the agent-facing ref returned by browser_observe.
type BrowserElementRef struct {
	Ref                string             `json:"ref"`
	SnapshotID         string             `json:"snapshot_id,omitempty"`
	FrameID            string             `json:"frame_id,omitempty"`
	TabID              string             `json:"tab_id,omitempty"`
	Tag                string             `json:"tag,omitempty"`
	Role               string             `json:"role,omitempty"`
	InputType          string             `json:"input_type,omitempty"`
	Name               string             `json:"name,omitempty"`
	Text               string             `json:"text,omitempty"`
	Selector           string             `json:"selector,omitempty"`
	SelectorCandidates []string           `json:"selector_candidates,omitempty"`
	StableKey          string             `json:"stable_key,omitempty"`
	BoundingBox        BrowserBoundingBox `json:"bounding_box,omitempty"`
	StabilityScore     float64            `json:"stability_score,omitempty"`
	Enabled            bool               `json:"enabled,omitempty"`
	Disabled           bool               `json:"disabled,omitempty"`
	Checked            bool               `json:"checked,omitempty"`
	Value              string             `json:"value,omitempty"`
	Visible            bool               `json:"visible,omitempty"`
	InViewport         bool               `json:"in_viewport,omitempty"`
	BackendNodeID      int                `json:"backend_node_id,omitempty"`
}

// CompactElementRef is the LLM-visible SoM row. Selector candidates, bbox,
// and backend node ids stay on the internal BrowserElementRef only.
type CompactElementRef struct {
	Ref     string `json:"ref"`
	Role    string `json:"role,omitempty"`
	Name    string `json:"name,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Enabled bool   `json:"enabled"`
	Checked *bool  `json:"checked,omitempty"`
	FrameID string `json:"frame_id,omitempty"`
}

// BrowserPageFlags captures observe-time page conditions that should stop
// blind clicking.
type BrowserPageFlags struct {
	CaptchaWidget bool `json:"captcha_widget,omitempty"`
	Captcha       bool `json:"captcha,omitempty"`
	LoginWall     bool `json:"login_wall,omitempty"`
	MFA           bool `json:"mfa,omitempty"`
	Canvas        bool `json:"canvas,omitempty"`
	VisionUsed    bool `json:"vision_used,omitempty"`
}

// ExpectSpec is an optional post-condition for navigate/click/type/select.
type ExpectSpec struct {
	Type    string `json:"type,omitempty"` // url_contains, text, ref_appears, dialog
	Pattern string `json:"pattern,omitempty"`
}

// BrowserSnapshot captures an observe step.
type BrowserSnapshot struct {
	SnapshotID      string                 `json:"snapshot_id"`
	SessionID       string                 `json:"session_id"`
	TargetID        string                 `json:"target_id,omitempty"`
	CreatedAt       int64                  `json:"created_at"`
	URL             string                 `json:"url"`
	Title           string                 `json:"title"`
	ReadyState      string                 `json:"ready_state,omitempty"`
	FrameTree       []BrowserFrameSnapshot `json:"frame_tree,omitempty"`
	Refs            []BrowserElementRef    `json:"refs"`
	PageTextExcerpt string                 `json:"page_text_excerpt,omitempty"`
	PageTextTotal   int                    `json:"page_text_total,omitempty"`
	PageTextOffset  int                    `json:"page_text_offset,omitempty"`
	PageTextHasMore bool                   `json:"page_text_has_more,omitempty"`
	ConsoleSummary  string                 `json:"console_summary,omitempty"`
	NetworkSummary  string                 `json:"network_summary,omitempty"`
	Screenshot      string                 `json:"screenshot,omitempty"`
	PageFlags       BrowserPageFlags       `json:"page_flags,omitempty"`
	RefsTruncated   bool                   `json:"refs_truncated,omitempty"`
	VisionExcerpt   string                 `json:"vision_excerpt,omitempty"`
}

// BrowserObservation is the structured result of browser_observe.
type BrowserObservation struct {
	Snapshot  BrowserSnapshot        `json:"snapshot"`
	PageState map[string]interface{} `json:"page_state,omitempty"`
	Display   string                 `json:"display"`
	Data      map[string]interface{} `json:"data"`
}

// BrowserActionResult is the structured result of a browser action.
type BrowserActionResult struct {
	SessionID         string                 `json:"session_id"`
	SnapshotID        string                 `json:"snapshot_id,omitempty"`
	Action            string                 `json:"action"`
	Status            string                 `json:"status"`
	Detail            string                 `json:"detail,omitempty"`
	Display           string                 `json:"display"`
	Data              map[string]interface{} `json:"data,omitempty"`
	AskUser           *agent.AskUserRequest  `json:"-"`
	GoalClass         bool                   `json:"-"`
	submitRememberKey string
}

// BrowserTraceEvent is a browser-specific trace projection.
type BrowserTraceEvent struct {
	Kind      string `json:"kind"`
	Summary   string `json:"summary"`
	CreatedAt int64  `json:"created_at"`
}

// BrowserAgentState is a read-only projection of a browser agent session.
type BrowserAgentState struct {
	ID             string
	OwnerID        string
	Addr           string
	TargetID       string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Policy         BrowserPolicy
	CurrentURL     string
	CurrentTitle   string
	ReadyState     string
	ActivityLog    []string
	ConsoleLines   []string
	NetworkLines   []string
	ErrorLines     []string
	Trace          []BrowserTraceEvent
	Timeline       []BrowserTimelineSlice
	ConsoleEntries []BrowserConsoleEvent
	NetworkEntries []BrowserNetworkEvent
	Tabs           []BrowserTabSnapshot
	Frames         []BrowserFrameSnapshot
	Snapshots      []BrowserSnapshot
	LatestRefs     []BrowserElementRef
	LastSnapshotID string
	ActiveTabID    string
	ActiveFrameID  string
	Alive          bool
}

// BrowserAgentSession is the agent-facing long-lived browser session.
type BrowserAgentSession struct {
	mu        sync.RWMutex
	recoverMu sync.Mutex
	ID        string
	OwnerID   string
	Addr      string
	TargetID  string
	CreatedAt time.Time
	UpdatedAt time.Time
	Policy    BrowserPolicy

	// Mode records how this session was connected (persistent/connect_user/isolated).
	Mode SessionMode
	// ManagedUserDataDir records the profile directory for managed browser modes.
	ManagedUserDataDir string
	// LastActivityAt tracks the last tool operation time (for inactivity timeout).
	LastActivityAt time.Time
	// timedOut is set by the inactivity timer before calling StopAgentSession,
	// so StopAgentSession can skip duplicate audit logging.
	timedOut bool

	session         *Session
	stopCh          chan struct{}
	eventPumpClient *CDPClient

	// targetGoneCh is closed when the active target is destroyed, detached, or
	// the inspector disconnects. Operations waiting on CDP responses can select
	// on this channel to abort immediately instead of waiting the full timeout.
	targetGoneCh chan struct{}

	recentConsole        []string
	recentNetwork        []string
	recentErrors         []string
	recentTrace          []BrowserTraceEvent
	recentTimeline       []BrowserTimelineSlice
	recentConsoleEntries []BrowserConsoleEvent
	recentNetworkEntries []BrowserNetworkEvent
	activityLog          []string
	recentSubmitClicks   map[string]time.Time

	snapshots       map[string]*BrowserSnapshot
	lastSnapshotID  string
	lastFingerprint string
	lastExpect      ExpectSpec
	lastMissingKey  string
	missingExpectN  int
}
