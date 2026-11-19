package repomaint

// FrameType is the Hub → desktop websocket frame. There is no client ack frame.
const FrameType = "github.repo_maint"

// RepoMaintEvent is the normalized webhook Hub stores and forwards.
// payload_json is this struct, never the raw GitHub body.
type RepoMaintEvent struct {
	DeliveryID     string   `json:"delivery_id"`
	GitHubDelivery string   `json:"github_delivery"`
	RuleID         string   `json:"rule_id"`
	EventKind      string   `json:"event_kind"`
	ObjectID       string   `json:"object_id"`
	Owner          string   `json:"owner"`
	Repo           string   `json:"repo"`
	Number         int      `json:"number,omitempty"`
	Title          string   `json:"title,omitempty"`
	Author         string   `json:"author,omitempty"`
	HTMLURL        string   `json:"html_url,omitempty"`
	APIURL         string   `json:"api_url,omitempty"`
	HeadRef        string   `json:"head_ref,omitempty"`
	BaseRef        string   `json:"base_ref,omitempty"`
	Ref            string   `json:"ref,omitempty"`
	Draft          bool     `json:"draft,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	Body           string   `json:"body,omitempty"`
	BodyRunes      int      `json:"body_runes,omitempty"`
	CommentID      int64    `json:"comment_id,omitempty"`
	ReviewID       int64    `json:"review_id,omitempty"`
	SHA            string   `json:"sha,omitempty"`
	CheckName      string   `json:"check_name,omitempty"`
	Conclusion     string   `json:"conclusion,omitempty"`
	DefaultBranch  string   `json:"default_branch,omitempty"`
	PRNumbers      []int    `json:"pr_numbers,omitempty"`
}

const (
	kindIssuesOpened        = "issues.opened"
	kindIssuesClosed        = "issues.closed"
	kindIssuesLabeled       = "issues.labeled"
	kindIssueComment        = "issue_comment.created"
	kindPullOpened          = "pull_request.opened"
	kindPullMerged          = "pull_request.merged"
	kindReviewRequested     = "pull_request.review_requested"
	kindReviewSubmitted     = "pull_request_review.submitted"
	kindPush                = "push"
	kindCheckFailure        = "check_run.failure"
	statusPending           = "pending"
	statusHandedOff         = "handed_off"
	maxBodyRunes            = 4000
	defaultPendingLimit     = 200
	defaultHookRatePerMin   = 120
	defaultGlobalRatePerMin = 600
	defaultMaxWebhookBody   = 2 << 20
)

// timeLayout is fixed-width UTC so text comparisons match chronological order.
const timeLayout = "2006-01-02T15:04:05.000000000Z"
