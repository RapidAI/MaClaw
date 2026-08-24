package longhorizon

import "time"

const (
	DefaultMaxRounds     = 25
	MaxRounds            = 1000
	MaxExperiencePerTask = 2

	GoalCap             = 8_000
	AcceptanceCap       = 4_000
	RelatedAuditMax     = 3
	RelatedAuditEachCap = 1_200
	VerifiedContextCap  = 60_000
	ManagerHistoryCap   = 100_000
	AuditorOutputCap    = 24_000
	CLIMaxIterations     = 80
	GUIMaxIterations     = 20
	BrowserMaxIterations = 20
	CarryoverCapRunes    = 8_000
	CarryoverMaxItems   = 40
	CarryoverItemCap    = 2_000

	RoleManager         = "manager"
	RoleCLIExecutor     = "cli_executor"
	RoleGUIExecutor     = "gui_executor"
	RoleBrowserExecutor = "browser_executor"
	RoleCLIAuditor      = "cli_auditor"
	RoleGUIAuditor      = "gui_auditor"
	RoleBrowserAuditor  = "browser_auditor"

	StatusIdle      = "idle"
	StatusManaging  = "managing"
	StatusExecuting = "executing"
	StatusAuditing  = "auditing"
	StatusAsking    = "asking"
	StatusBlocked   = "blocked"
	StatusDone      = "done"
	StatusCancelled = "cancelled"

	NextCLI     NextStep = "cli"
	NextGUI     NextStep = "gui"
	NextBrowser NextStep = "browser"
	NextAsk     NextStep = "ask"
	NextBlocked NextStep = "blocked"
	NextDone    NextStep = "done"
	NextInvalid NextStep = "invalid"
)

type NextStep string

type PolicySnapshot struct {
	OwnerID       string `json:"owner_id"`
	ProjectRoot   string `json:"project_root"`
	WriteSet      string `json:"write_set,omitempty"`
	Untrusted     bool   `json:"untrusted,omitempty"`
	HorizonTaskID string `json:"horizon_task_id"`
	RoundIndex    int    `json:"round_index"`
	EventScopeID  string `json:"event_scope_id,omitempty"`
}

type EpisodeBudget struct {
	MaxIterations int `json:"max_iterations,omitempty"`
	MaxDurationS  int `json:"max_duration_seconds,omitempty"`
}

type EpisodeContext struct {
	Role          string
	Goal          string
	Acceptance    string
	RelatedAudits []string
	Evidence      string
	ToolSurface   []string
	SystemPrompt  string
	History       string
	Policy        PolicySnapshot
	Budget        EpisodeBudget
}

type AuditReport struct {
	RoundIndex int       `json:"round_index"`
	Status     string    `json:"status"`    // complete | incomplete | blocked
	Integrity  string    `json:"integrity"` // clean | suspect | violation
	Alignment  string    `json:"alignment"` // aligned | drifted
	Summary    string    `json:"summary"`
	Digest     string    `json:"digest"`
	Synthetic  bool      `json:"synthetic,omitempty"`
	Mechanical bool      `json:"mechanical,omitempty"`
	HasProbe   bool      `json:"has_probe,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type ManagedRound struct {
	RoundIndex int          `json:"round_index"`
	Next       NextStep     `json:"next"`
	Goal       string       `json:"goal"`
	Acceptance string       `json:"acceptance"`
	Audit      *AuditReport `json:"audit,omitempty"`
}

type TaskState struct {
	TaskID           string         `json:"task_id"`
	UserGoal         string         `json:"user_goal"`
	Status           string         `json:"status"`
	RoundIndex       int            `json:"round_index"`
	MaxRounds        int            `json:"max_rounds"`
	ManagerNext      NextStep       `json:"manager_next,omitempty"`
	Policy           PolicySnapshot `json:"policy"`
	Rounds           []ManagedRound `json:"rounds,omitempty"`
	Carryover        []string       `json:"carryover,omitempty"`
	ExperienceWrites int            `json:"experience_writes,omitempty"`
	Completed        bool           `json:"completed"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type ProbeResult struct {
	Digest string
	OK     bool
}

type EligibilityInput struct {
	HorizonTaskID        string
	RoundIndex           int
	AuditDigest          string
	Audit                *AuditReport
	AttemptTerminal      bool
	Cancelled            bool
	Interrupted          bool
	Untrusted            bool
	MissingControlHeader bool
}

type ManagerPlan struct {
	Next          NextStep
	Goal          string
	Acceptance    string
	RelatedAudits []string
	Question      string
	Raw           string
}
