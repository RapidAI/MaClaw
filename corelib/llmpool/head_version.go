package llmpool

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	HeadRoleServing   = "serving"
	HeadRolePrevious  = "previous"
	HeadRoleCandidate = "candidate"
	HeadRoleHistory   = "history"
	HeadSourceTrain   = "train"
	HeadSourcePull    = "pull_official"
	HeadSourceReplica = "replica"
	HeadMaxHistory    = 20
)

// TrainRunRow is the frozen per-sample label copy kept with a TrainRun.
// It never carries preview text; previews live only in the corpus store.
type TrainRunRow struct {
	ID         string `json:"id"`
	GoldClass  string `json:"gold_class"`
	GoldSource string `json:"gold_source"`
	GroupID    string `json:"group_id,omitempty"`
}

// TrainRun is the immutable snapshot of one successful fit, attached to the
// candidate slot. It moves with the weights on adopt and rollback.
type TrainRun struct {
	Version   int           `json:"version"`
	TrainedAt string        `json:"trained_at,omitempty"`
	SampleIDs []string      `json:"sample_ids,omitempty"`
	Rows      []TrainRunRow `json:"rows,omitempty"`
}

// HasSampleID reports whether id was part of this run's fit set.
func (r *TrainRun) HasSampleID(id string) bool {
	if r == nil {
		return false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, existing := range r.SampleIDs {
		if existing == id {
			return true
		}
	}
	return false
}

// HeadSlots is the three-slot weight set of one global classification head:
// serving (hot path reads this only), previous (one rollback step), and
// candidate (latest training output, never read by the hot path).
type HeadSlots struct {
	Serving         *ClassificationHead `json:"serving,omitempty"`
	Previous        *ClassificationHead `json:"previous,omitempty"`
	Candidate       *ClassificationHead `json:"candidate,omitempty"`
	ServingSource   string              `json:"serving_source,omitempty"`
	PreviousSource  string              `json:"previous_source,omitempty"`
	CandidateSource string              `json:"candidate_source,omitempty"`
	ServingRun      *TrainRun           `json:"serving_run,omitempty"`
	PreviousRun     *TrainRun           `json:"previous_run,omitempty"`
	CandidateRun    *TrainRun           `json:"candidate_run,omitempty"`
}

// NextVersion is max(serving, previous, candidate, history)+1 so a rollback
// cannot reuse a retired version number and hide that row from Versions.
func (s *HeadSlots) NextVersion(history []ClassHeadVersionInfo) int {
	max := 0
	for _, h := range []*ClassificationHead{s.serving(), s.previous(), s.candidate()} {
		if h != nil && h.Version > max {
			max = h.Version
		}
	}
	for _, item := range history {
		if item.Version > max {
			max = item.Version
		}
	}
	return max + 1
}

func (s *HeadSlots) serving() *ClassificationHead {
	if s == nil {
		return nil
	}
	return s.Serving
}

func (s *HeadSlots) previous() *ClassificationHead {
	if s == nil {
		return nil
	}
	return s.Previous
}

func (s *HeadSlots) candidate() *ClassificationHead {
	if s == nil {
		return nil
	}
	return s.Candidate
}

// InstallCandidate overwrites an un-adopted candidate. Serving and previous
// stay untouched; a failed fit must not call this at all.
func (s *HeadSlots) InstallCandidate(next *ClassificationHead, source string, run *TrainRun) {
	if s == nil || next == nil {
		return
	}
	s.Candidate = next
	s.CandidateSource = strings.TrimSpace(source)
	s.CandidateRun = run
}

// Adopt promotes candidate into serving and keeps the old serving as the one
// rollback step. trained_at values travel with their weights; nothing here
// rewrites timestamps. Returns false when there is no candidate.
func (s *HeadSlots) Adopt() bool {
	if s == nil || s.Candidate == nil || !s.Candidate.Ready() {
		return false
	}
	s.Previous = s.Serving
	s.PreviousSource = s.ServingSource
	s.PreviousRun = s.ServingRun
	s.Serving = s.Candidate
	s.ServingSource = s.CandidateSource
	s.ServingRun = s.CandidateRun
	s.Candidate = nil
	s.CandidateSource = ""
	s.CandidateRun = nil
	return true
}

// Rollback swaps serving and previous (weights, sources and TrainRuns move
// together). Pipeline and candidate stay untouched. Returns false when there
// is no previous to roll back to.
func (s *HeadSlots) Rollback() bool {
	if s == nil || s.Previous == nil || !s.Previous.Ready() {
		return false
	}
	s.Serving, s.Previous = s.Previous, s.Serving
	s.ServingSource, s.PreviousSource = s.PreviousSource, s.ServingSource
	s.ServingRun, s.PreviousRun = s.PreviousRun, s.ServingRun
	return true
}

// Versions lists serving, previous and candidate followed by retired history.
func (s *HeadSlots) Versions(history []ClassHeadVersionInfo) []ClassHeadVersionInfo {
	seen := map[int]struct{}{}
	out := make([]ClassHeadVersionInfo, 0, 3+len(history))
	for _, item := range []struct {
		role   string
		source string
		head   *ClassificationHead
	}{
		{HeadRoleServing, s.servingSource(), s.serving()},
		{HeadRolePrevious, s.previousSource(), s.previous()},
		{HeadRoleCandidate, s.candidateSource(), s.candidate()},
	} {
		if item.head == nil || item.head.Version <= 0 {
			continue
		}
		out = append(out, VersionInfoFromHead(item.role, item.source, item.head))
		seen[item.head.Version] = struct{}{}
	}
	for _, item := range history {
		if item.Version <= 0 {
			continue
		}
		if _, ok := seen[item.Version]; ok {
			continue
		}
		item.Role = HeadRoleHistory
		out = append(out, item)
		seen[item.Version] = struct{}{}
	}
	return out
}

func (s *HeadSlots) servingSource() string {
	if s == nil {
		return ""
	}
	return s.ServingSource
}

func (s *HeadSlots) previousSource() string {
	if s == nil {
		return ""
	}
	return s.PreviousSource
}

func (s *HeadSlots) candidateSource() string {
	if s == nil {
		return ""
	}
	return s.CandidateSource
}

// ResolveSlot maps "serving"/"previous"/"candidate" or an explicit version
// number to a slot head plus its TrainRun. Retired versions are metadata only.
func (s *HeadSlots) ResolveSlot(slot string) (string, *ClassificationHead, *TrainRun, error) {
	slot = strings.ToLower(strings.TrimSpace(slot))
	switch slot {
	case "", HeadRoleServing, "current":
		if s == nil || s.Serving == nil || !s.Serving.Ready() {
			return "", nil, nil, errors.New("serving head is not ready")
		}
		return HeadRoleServing, s.Serving, s.ServingRun, nil
	case HeadRolePrevious, "prev":
		if s == nil || s.Previous == nil || !s.Previous.Ready() {
			return "", nil, nil, errors.New("previous head is not ready")
		}
		return HeadRolePrevious, s.Previous, s.PreviousRun, nil
	case HeadRoleCandidate:
		if s == nil || s.Candidate == nil || !s.Candidate.Ready() {
			return "", nil, nil, errors.New("candidate head is not ready")
		}
		return HeadRoleCandidate, s.Candidate, s.CandidateRun, nil
	}
	n, err := strconv.Atoi(slot)
	if err != nil || n <= 0 {
		return "", nil, nil, errors.New("unknown head slot")
	}
	if s != nil && s.Serving != nil && s.Serving.Version == n && s.Serving.Ready() {
		return HeadRoleServing, s.Serving, s.ServingRun, nil
	}
	if s != nil && s.Previous != nil && s.Previous.Version == n && s.Previous.Ready() {
		return HeadRolePrevious, s.Previous, s.PreviousRun, nil
	}
	if s != nil && s.Candidate != nil && s.Candidate.Version == n && s.Candidate.Ready() {
		return HeadRoleCandidate, s.Candidate, s.CandidateRun, nil
	}
	return "", nil, nil, errors.New("retired head versions are metadata only")
}

var ErrEmptyScoreText = errors.New("enter text to score")

func ScoreRequestPreview(body map[string]any) (string, error) {
	preview := RequestTextPreview(body, 400)
	if strings.TrimSpace(preview) == "" {
		return "", ErrEmptyScoreText
	}
	return preview, nil
}

// ClassHeadVersionInfo is a compact version card for admin UI and retired history.
// History rows must not carry weights.
type ClassHeadVersionInfo struct {
	Role      string  `json:"role"`
	Version   int     `json:"version"`
	TrainedAt string  `json:"trained_at,omitempty"`
	Tau       float64 `json:"tau,omitempty"`
	Ready     bool    `json:"ready"`
	Source    string  `json:"source,omitempty"`
	RetiredAt string  `json:"retired_at,omitempty"`
}

// ClassHeadScoreReport is a dry-run of rules vs one stored head version.
type ClassHeadScoreReport struct {
	Slot          string             `json:"slot"`
	Version       int                `json:"version,omitempty"`
	StoreKey      string             `json:"store_key,omitempty"`
	GroupID       string             `json:"group_id,omitempty"`
	EmbedderReady bool               `json:"embedder_ready"`
	Preview       string             `json:"preview,omitempty"`
	RuleClass     string             `json:"rule_class,omitempty"`
	RuleSource    string             `json:"rule_source,omitempty"`
	HeadClass     string             `json:"head_class,omitempty"`
	HeadMaxP      float64            `json:"head_max_p,omitempty"`
	HeadTau       float64            `json:"head_tau,omitempty"`
	HeadProbs     map[string]float64 `json:"head_probs,omitempty"`
	IfLiveClass   string             `json:"if_live_class,omitempty"`
	IfLiveSource  string             `json:"if_live_source,omitempty"`
	IfLiveUsed    bool               `json:"if_live_used,omitempty"`
	HeadEligible  bool               `json:"head_eligible"`
	WouldRewrite  bool               `json:"would_rewrite"`
	RoutedClass   string             `json:"routed_class,omitempty"`
	ResolvedModel string             `json:"resolved_model,omitempty"`
	Quality       string             `json:"quality,omitempty"`
}

func VersionInfoFromHead(role, source string, h *ClassificationHead) ClassHeadVersionInfo {
	info := ClassHeadVersionInfo{Role: role, Source: strings.TrimSpace(source)}
	if h == nil {
		return info
	}
	info.Version = h.Version
	info.TrainedAt = h.TrainedAt
	info.Tau = h.EffectiveTau()
	info.Ready = h.Ready()
	return info
}

func ArchiveRetiredHead(history []ClassHeadVersionInfo, retired ClassHeadVersionInfo) []ClassHeadVersionInfo {
	if retired.Version <= 0 {
		return history
	}
	retired.Role = HeadRoleHistory
	if strings.TrimSpace(retired.RetiredAt) == "" {
		retired.RetiredAt = time.Now().UTC().Format(time.RFC3339)
	}
	out := []ClassHeadVersionInfo{retired}
	seen := map[int]struct{}{retired.Version: {}}
	for _, item := range history {
		if item.Version <= 0 {
			continue
		}
		if _, ok := seen[item.Version]; ok {
			continue
		}
		item.Role = HeadRoleHistory
		out = append(out, item)
		seen[item.Version] = struct{}{}
		if len(out) >= HeadMaxHistory {
			break
		}
	}
	return out
}

func ScoreHeadAgainstRules(group *ServiceGroup, header http.Header, body map[string]any, slot string, head *ClassificationHead, pred HeadPrediction) ClassHeadScoreReport {
	if group == nil {
		group = &ServiceGroup{}
	}
	dec := ClassifyAndRoute(header, body, group)
	liveClass, liveSource, used := ApplyHeadPipeline(PipelineOn, "", dec.Class, dec.Source, pred)
	routed, model, quality := RouteWorkloadClass(group, liveClass)
	report := ClassHeadScoreReport{
		Slot:          strings.TrimSpace(slot),
		Preview:       RequestTextPreview(body, 400),
		RuleClass:     dec.Class,
		RuleSource:    dec.Source,
		HeadClass:     pred.Class,
		HeadMaxP:      pred.MaxP,
		HeadProbs:     pred.Probs,
		IfLiveClass:   liveClass,
		IfLiveSource:  liveSource,
		IfLiveUsed:    used,
		HeadEligible:  HeadMayRewriteSource(dec.Source),
		WouldRewrite:  liveClass != dec.Class,
		RoutedClass:   routed,
		ResolvedModel: model,
		Quality:       quality,
	}
	if head != nil {
		report.Version = head.Version
		report.HeadTau = head.EffectiveTau()
	}
	return report
}
