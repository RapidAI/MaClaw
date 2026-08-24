package llmpool

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	HeadRoleCurrent   = "current"
	HeadRolePrevious  = "previous"
	HeadRoleHistory   = "history"
	HeadSourceTrain   = "train"
	HeadSourcePull    = "pull_official"
	HeadSourceReplica = "replica"
	HeadMaxHistory    = 20
)

// NextHeadVersion is max(current, previous, history)+1 so a rollback cannot
// reuse a retired version number and hide that row from CollectHeadVersions.
func NextHeadVersion(current, previous *ClassificationHead, history []ClassHeadVersionInfo) int {
	max := 0
	if current != nil && current.Version > max {
		max = current.Version
	}
	if previous != nil && previous.Version > max {
		max = previous.Version
	}
	for _, item := range history {
		if item.Version > max {
			max = item.Version
		}
	}
	return max + 1
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

// RotateClassificationHead archives ready Previous, clones Current into Previous, then installs next.
func RotateClassificationHead(current, previous **ClassificationHead, currentSrc, previousSrc *string, history *[]ClassHeadVersionInfo, next *ClassificationHead, source string) {
	if current == nil || previous == nil || currentSrc == nil || previousSrc == nil || history == nil || next == nil {
		return
	}
	if *previous != nil && (*previous).Ready() {
		*history = ArchiveRetiredHead(*history, VersionInfoFromHead(HeadRoleHistory, *previousSrc, *previous))
	}
	if *current != nil && (*current).Ready() {
		*previous = (*current).Clone()
		*previousSrc = *currentSrc
	}
	*current = next
	*currentSrc = strings.TrimSpace(source)
}

func CollectHeadVersions(current, previous *ClassificationHead, currentSrc, previousSrc string, history []ClassHeadVersionInfo) []ClassHeadVersionInfo {
	seen := map[int]struct{}{}
	out := make([]ClassHeadVersionInfo, 0, 2+len(history))
	if current != nil && current.Version > 0 {
		out = append(out, VersionInfoFromHead(HeadRoleCurrent, currentSrc, current))
		seen[current.Version] = struct{}{}
	}
	if previous != nil && previous.Version > 0 {
		out = append(out, VersionInfoFromHead(HeadRolePrevious, previousSrc, previous))
		seen[previous.Version] = struct{}{}
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

func ResolveHeadSlot(slot string, current, previous *ClassificationHead) (string, *ClassificationHead, error) {
	slot = strings.ToLower(strings.TrimSpace(slot))
	switch slot {
	case "", HeadRoleCurrent, "serving":
		if current == nil || !current.Ready() {
			return "", nil, errors.New("current head is not ready")
		}
		return HeadRoleCurrent, current, nil
	case HeadRolePrevious, "prev":
		if previous == nil || !previous.Ready() {
			return "", nil, errors.New("previous head is not ready")
		}
		return HeadRolePrevious, previous, nil
	}
	n, err := strconv.Atoi(slot)
	if err != nil || n <= 0 {
		return "", nil, errors.New("unknown head slot")
	}
	if current != nil && current.Version == n && current.Ready() {
		return HeadRoleCurrent, current, nil
	}
	if previous != nil && previous.Version == n && previous.Ready() {
		return HeadRolePrevious, previous, nil
	}
	return "", nil, errors.New("retired head versions are metadata only")
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
