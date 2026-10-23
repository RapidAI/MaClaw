package llmpool

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	ServiceGroupKindStatic  = "static"
	ServiceGroupKindDynamic = "dynamic"

	WorkloadClassPlan     = "plan"
	WorkloadClassDesign   = "design"
	WorkloadClassReview   = "review"
	WorkloadClassDocWrite = "doc_write"
	WorkloadClassCode     = "code"
	WorkloadClassOps      = "ops"
	WorkloadClassChat     = "chat"
	WorkloadClassClassify = "classify"

	// WorkloadFallbackBalanced is the unclassified fallback. It is not a ninth class.
	WorkloadFallbackBalanced = "balanced"
	WorkloadUnclassified     = "unclassified"

	QualityHigh = "high"
	QualityMid  = "mid"
	QualityLow  = "low"

	OfficialTierHigh = "official-high"
	OfficialTierMid  = "official-mid"
	OfficialTierLow  = "official-low"
	OfficialGroupID  = "maclaw-official"

	// HubOfficialServiceGroupID is Hub's undeletable official provider entry.
	// HubCenter does not treat it as a local catalog group; it resolves the
	// request against the tenant compute-card pool (redeem / grant-backed).
	HubOfficialServiceGroupID = "maclaw_official_group"

	ClassSourceHint      = "hint"
	ClassSourceWorkflow  = "workflow"
	ClassSourceTaskType  = "task_type"
	ClassSourceHeuristic = "heuristic"
	ClassSourceFallback  = "fallback"
	ClassSourceNone      = ""

	BodyWorkloadClassKey = "maclaw_workload_class"
)

// FrozenWorkloadClasses is the product class list. balanced is not included.
var FrozenWorkloadClasses = []string{
	WorkloadClassPlan,
	WorkloadClassDesign,
	WorkloadClassReview,
	WorkloadClassDocWrite,
	WorkloadClassCode,
	WorkloadClassOps,
	WorkloadClassChat,
	WorkloadClassClassify,
}

// RequiredDynamicRoutes must be present on every dynamic group.
var RequiredDynamicRoutes = []string{
	WorkloadClassPlan,
	WorkloadClassDesign,
	WorkloadClassDocWrite,
	WorkloadClassCode,
	WorkloadFallbackBalanced,
}

// OptionalDynamicRoutes fall back to balanced when missing.
var OptionalDynamicRoutes = []string{
	WorkloadClassReview,
	WorkloadClassOps,
	WorkloadClassChat,
	WorkloadClassClassify,
}

// WorkloadDecision is the L1 result for one request.
type WorkloadDecision struct {
	Class         string `json:"class"`
	RoutedClass   string `json:"routed_class,omitempty"`
	Source        string `json:"class_source,omitempty"`
	ResolvedModel string `json:"resolved_model,omitempty"`
	Quality       string `json:"quality,omitempty"`
	Upgraded      bool   `json:"capability_upgraded,omitempty"`
	// AvailabilityFallback is set when the routed high/mid/low model was not
	// usable and a sibling quality tier in the same group was selected instead.
	AvailabilityFallback bool `json:"availability_fallback,omitempty"`
	// Degree is simple, complex, or empty. Empty does not move the band.
	Degree string `json:"degree,omitempty"`
	// DegreeApplied is set when this turn's degree moved the class band.
	DegreeApplied bool             `json:"degree_applied,omitempty"`
	Passthrough   bool             `json:"passthrough,omitempty"`
	HeadClass     string           `json:"head_class,omitempty"`
	HeadMaxP      float64          `json:"head_max_p,omitempty"`
	HeadUsed      bool             `json:"head_used,omitempty"`
	RuleClass     string           `json:"rule_class,omitempty"`
	RuleSource    string           `json:"rule_source,omitempty"`
	Attribution   RouteAttribution `json:"attribution,omitempty"`
}

// RouteAttribution is the documented L1/L3 audit payload.
type RouteAttribution struct {
	RequestedGroup       string `json:"requested_group,omitempty"`
	RequestedModel       string `json:"requested_model,omitempty"`
	WorkloadClass        string `json:"workload_class,omitempty"`
	ClassSource          string `json:"class_source,omitempty"`
	ResolvedModel        string `json:"resolved_model,omitempty"`
	ResolvedProvider     string `json:"resolved_provider,omitempty"`
	UpstreamModel        string `json:"upstream_model,omitempty"`
	OfficialProviderPool string `json:"official_provider_pool,omitempty"`
	SelectionReason      string `json:"selection_reason,omitempty"`
}

func AttributionFrom(dec WorkloadDecision, requestedGroup, requestedModel string) RouteAttribution {
	requestedModel = strings.TrimSpace(requestedModel)
	reason := "manual model selection"
	switch {
	case dec.AvailabilityFallback:
		reason = "official tier availability fallback"
	case dec.DegreeApplied:
		reason = "workload degree gate"
	case !dec.Passthrough:
		reason = "dynamic workload route"
	case dec.Upgraded:
		reason = "dynamic workload route"
	case requestedModel != "" && !IsAutoModel(requestedModel):
		reason = "pinned model"
	case IsAutoModel(requestedModel):
		reason = "static auto selection"
	}
	attr := RouteAttribution{
		RequestedGroup:  strings.TrimSpace(requestedGroup),
		RequestedModel:  requestedModel,
		WorkloadClass:   strings.TrimSpace(dec.Class),
		ClassSource:     strings.TrimSpace(dec.Source),
		ResolvedModel:   strings.TrimSpace(dec.ResolvedModel),
		SelectionReason: reason,
	}
	if tier := NormalizeOfficialTier(attr.ResolvedModel); tier != "" {
		attr.UpstreamModel = tier
		attr.OfficialProviderPool = OfficialGroupID
	}
	return attr
}

func finishDecision(dec WorkloadDecision, group *ServiceGroup, requestedModel string) WorkloadDecision {
	groupID := ""
	if group != nil {
		groupID = strings.TrimSpace(group.ID)
	}
	dec.Attribution = AttributionFrom(dec, groupID, requestedModel)
	return dec
}

func (d *WorkloadDecision) BindRequestedGroup(groupID string) {
	if d == nil {
		return
	}
	d.Attribution = AttributionFrom(*d, groupID, d.Attribution.RequestedModel)
}

// ClassifyInput is the request surface the V1 rule classifier reads.
type ClassifyInput struct {
	Header http.Header
	Body   map[string]any
}

// NormalizeServiceGroupKind treats an empty kind as static.
func NormalizeServiceGroupKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case ServiceGroupKindDynamic:
		return ServiceGroupKindDynamic
	default:
		return ServiceGroupKindStatic
	}
}

// IsDynamicKind reports whether the group runs L1.
func IsDynamicKind(kind string) bool {
	return NormalizeServiceGroupKind(kind) == ServiceGroupKindDynamic
}

// ClientHeaderSelectsAuto reports that the user selected auto. Hub rewrites
// the forwarded body to the resolved official tier and keeps that choice in
// ClientModelHeader. An empty header is not auto, so a direct tier pin stays.
func ClientHeaderSelectsAuto(header http.Header) bool {
	if header == nil {
		return false
	}
	raw := strings.TrimSpace(header.Get(ClientModelHeader))
	if raw == "" {
		return false
	}
	return CanonicalClientModel(raw) == "auto"
}

// IsAutoModel reports whether the client asked Hub/HubCenter to choose.
func IsAutoModel(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "", "auto", "default":
		return true
	default:
		return false
	}
}

// IsOfficialTierName reports a HubCenter official logical tier.
func IsOfficialTierName(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case OfficialTierHigh, OfficialTierMid, OfficialTierLow:
		return true
	default:
		return false
	}
}

// NormalizeOfficialTier returns the canonical official tier name, or empty.
// low/mid/high are the same bands as official-low/mid/high.
func NormalizeOfficialTier(model string) string {
	canon := CanonicalClientModel(model)
	switch canon {
	case OfficialTierHigh, OfficialTierMid, OfficialTierLow:
		return canon
	default:
		return ""
	}
}

// IsWorkloadClass reports a frozen class (not balanced).
func IsWorkloadClass(class string) bool {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case WorkloadClassPlan, WorkloadClassDesign, WorkloadClassReview, WorkloadClassDocWrite, WorkloadClassCode, WorkloadClassOps, WorkloadClassChat, WorkloadClassClassify:
		return true
	default:
		return false
	}
}

// NormalizeWorkloadClass accepts a frozen class or balanced. Invalid values are empty.
func NormalizeWorkloadClass(class string) string {
	class = strings.ToLower(strings.TrimSpace(class))
	if class == WorkloadFallbackBalanced {
		return WorkloadFallbackBalanced
	}
	if IsWorkloadClass(class) {
		return class
	}
	return ""
}

// NormalizeQuality accepts high/mid/low.
func NormalizeQuality(quality string) string {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case QualityHigh:
		return QualityHigh
	case QualityMid:
		return QualityMid
	case QualityLow:
		return QualityLow
	default:
		return ""
	}
}

// QualityForOfficialTier maps official-high/mid/low onto a quality band.
func QualityForOfficialTier(model string) string {
	switch NormalizeOfficialTier(model) {
	case OfficialTierHigh:
		return QualityHigh
	case OfficialTierMid:
		return QualityMid
	case OfficialTierLow:
		return QualityLow
	default:
		return ""
	}
}

// OfficialTierForQuality is the inverse of QualityForOfficialTier.
func OfficialTierForQuality(quality string) string {
	switch NormalizeQuality(quality) {
	case QualityHigh:
		return OfficialTierHigh
	case QualityMid:
		return OfficialTierMid
	case QualityLow:
		return OfficialTierLow
	default:
		return ""
	}
}

// QualityAvailabilityChain is the same-group retry order for a quality band.
// high descends through mid before low. mid climbs to high before it falls
// to low. low only climbs. plan/design never include low.
func QualityAvailabilityChain(selectedQuality, class string) []string {
	var chain []string
	switch NormalizeQuality(selectedQuality) {
	case QualityHigh:
		chain = []string{QualityHigh, QualityMid, QualityLow}
	case QualityMid:
		chain = []string{QualityMid, QualityHigh, QualityLow}
	case QualityLow:
		chain = []string{QualityLow, QualityMid, QualityHigh}
	default:
		return nil
	}
	if class != WorkloadClassPlan && class != WorkloadClassDesign {
		return chain
	}
	out := make([]string, 0, len(chain))
	for _, quality := range chain {
		if quality != QualityLow {
			out = append(out, quality)
		}
	}
	return out
}

// DefaultOfficialAutoRoutes is the HubCenter official Auto table.
func DefaultOfficialAutoRoutes() []WorkloadRoute {
	return []WorkloadRoute{
		{Class: WorkloadClassPlan, Model: OfficialTierHigh, Quality: QualityHigh},
		{Class: WorkloadClassDesign, Model: OfficialTierHigh, Quality: QualityHigh},
		{Class: WorkloadClassReview, Model: OfficialTierHigh, Quality: QualityHigh},
		{Class: WorkloadClassDocWrite, Model: OfficialTierMid, Quality: QualityMid},
		{Class: WorkloadClassCode, Model: OfficialTierMid, Quality: QualityMid},
		{Class: WorkloadClassOps, Model: OfficialTierMid, Quality: QualityMid},
		{Class: WorkloadFallbackBalanced, Model: OfficialTierMid, Quality: QualityMid},
		{Class: WorkloadClassChat, Model: OfficialTierLow, Quality: QualityLow},
		{Class: WorkloadClassClassify, Model: OfficialTierLow, Quality: QualityLow},
	}
}

// IsOfficialConventionGroupID is the HubCenter official pool convention.
func IsOfficialConventionGroupID(id string) bool {
	return strings.EqualFold(strings.TrimSpace(id), OfficialGroupID)
}

// IsHubOfficialServiceGroup reports Hub's undeletable official entry ID.
func IsHubOfficialServiceGroup(id string) bool {
	return strings.EqualFold(strings.TrimSpace(id), HubOfficialServiceGroupID)
}

// IsOfficialFacadeGroupID reports Hub builtin official SKU or HC official group.
func IsOfficialFacadeGroupID(id string) bool {
	return IsHubOfficialServiceGroup(id) || IsOfficialConventionGroupID(id)
}

// ClassifyWorkload runs the V1 rule stack: P0 hint, P1 workflow/phase, P2 task type, P3 body, P4 balanced.
func ClassifyWorkload(in ClassifyInput) (class, source string) {
	if class := hintClass(in); class != "" {
		return class, ClassSourceHint
	}
	if class := phaseOrWorkflowClass(in); class != "" {
		return class, ClassSourceWorkflow
	}
	if class := taskTypeClass(in); class != "" {
		return class, ClassSourceTaskType
	}
	if class := heuristicClass(in); class != "" {
		return class, ClassSourceHeuristic
	}
	return WorkloadFallbackBalanced, ClassSourceFallback
}

// HintClassFromRequest returns a valid P0 class or empty.
func HintClassFromRequest(header http.Header, body map[string]any) string {
	return hintClass(ClassifyInput{Header: header, Body: body})
}

func hintClass(in ClassifyInput) string {
	if class := NormalizeWorkloadClass(headerValue(in.Header, WorkloadClassHeader)); IsWorkloadClass(class) {
		return class
	}
	if class := NormalizeWorkloadClass(stringValue(in.Body[BodyWorkloadClassKey])); IsWorkloadClass(class) {
		return class
	}
	return ""
}

func phaseOrWorkflowClass(in ClassifyInput) string {
	if class := classFromPhaseKind(headerValue(in.Header, PhaseKindHeader)); class != "" {
		return class
	}
	return classFromWorkflowType(headerValue(in.Header, WorkflowTypeHeader))
}

func classFromPhaseKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "document_planning", "code_planning", "planning", "requirements", "design", "tasks":
		return WorkloadClassPlan
	case "artifact_generation":
		return WorkloadClassDocWrite
	case "execution", "implementation":
		return WorkloadClassCode
	case "review", "verification":
		return WorkloadClassReview
	case "ops_execution", "ops":
		return WorkloadClassOps
	case "ops_risk_policy":
		return WorkloadClassReview
	case "intake", "classify":
		return WorkloadClassClassify
	default:
		return ""
	}
}

func classFromWorkflowType(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case value == "business_plan", value == "project_proposal", value == "grant_proposal", value == "event_planning", value == "gaokao_application", value == "changjiang_scholar", strings.HasPrefix(value, "nsfc_"):
		return WorkloadClassPlan
	case value == "product_design", value == "innovation", value == "due_diligence", value == "competitive_analysis", value == "experiment_design":
		return WorkloadClassDesign
	case value == "paper_writing", value == "presentation_design", value == "bid_response", value == "research_report", value == "literature_review", value == "patent_application", value == "us_patent_application":
		return WorkloadClassDocWrite
	case value == "coding", value == "testing", value == "paper_reproduction", value == "maintenance":
		return WorkloadClassCode
	case value == "bid_review", value == "contract_review", value == "compliance_audit", value == "patent_analysis", value == "changjiang_scholar_review":
		return WorkloadClassReview
	case value == "ops_maintenance":
		return WorkloadClassOps
	default:
		return ""
	}
}

func taskTypeClass(in ClassifyInput) string {
	switch strings.ToLower(strings.TrimSpace(headerValue(in.Header, TaskTypeHeader))) {
	case "fast", "c0", "chat":
		return WorkloadClassChat
	case "intent", "summary", "summarize", "c1":
		return WorkloadClassClassify
	case "reasoning", "tools", "coding", "code":
		return WorkloadClassCode
	default:
		return ""
	}
}

func heuristicClass(in ClassifyInput) string {
	return heuristicClassFromText(strings.ToLower(latestUserRequestText(in.Body)))
}

func heuristicClassFromText(text string) string {
	if text == "" {
		return ""
	}
	// Writing about a plan is still document work. Do not escalate to plan.
	if containsAny(text, "写商业计划", "write a business plan", "write the business plan", "撰写商业计划", "写一份计划", "write a plan") {
		return WorkloadClassDocWrite
	}
	scores := map[string]int{}
	add := func(class string, weight int, keywords ...string) {
		if containsAny(text, keywords...) {
			scores[class] += weight
		}
	}
	add(WorkloadClassDocWrite, 6, "写文档", "撰写", "write a report", "write the report", "文档", "报告", "方案说明书", "readme", "markdown")
	add(WorkloadClassCode, 6, "实现", "写代码", "fix bug", "compile", "refactor", "function", "class ", "import ", "代码", "bug", "unittest")
	add(WorkloadClassDesign, 5, "设计", "design", "mockup", "ux", "ui 稿", "架构图以外的设计")
	add(WorkloadClassPlan, 4, "规划", "roadmap", "architecture", "架构", "立项")
	add(WorkloadClassReview, 5, "审查", "评审", "review this", "code review", "audit")
	add(WorkloadClassOps, 5, "部署", "deploy", "kubernetes", "k8s", "运维", "rollback")
	add(WorkloadClassClassify, 4, "分类", "classify", "intent")
	add(WorkloadClassChat, 2, "你好", "hello", "hi ", "谢谢")
	bestClass := ""
	bestScore := 0
	for class, score := range scores {
		if score > bestScore || (score == bestScore && class < bestClass) {
			bestScore = score
			bestClass = class
		}
	}
	if bestScore == 0 {
		return ""
	}
	return bestClass
}

// RouteWorkloadClass picks a logical model from group.routes.
// Missing optional classes fall back to balanced. The class itself is never upgraded.
// If the routed high/mid/low model is missing or has no providers, a sibling
// quality tier in the same group is used so the request can still be served.
func RouteWorkloadClass(group *ServiceGroup, class string) (routedClass, model, quality string) {
	routedClass, model, quality, _ = routeWorkloadClass(group, class)
	return routedClass, model, quality
}

func routeWorkloadClass(group *ServiceGroup, class string) (routedClass, model, quality string, fallback bool) {
	class = NormalizeWorkloadClass(class)
	if class == "" {
		class = WorkloadFallbackBalanced
	}
	if route, ok := findWorkloadRoute(group, class); ok {
		model, quality, fallback = applyAvailabilityFallback(group, strings.TrimSpace(route.Model), class, effectiveRouteQuality(group, class, route))
		model, quality, fallback = upgradePlanRouteAwayFromLow(group, class, model, quality, fallback)
		return class, model, quality, fallback
	}
	if class != WorkloadFallbackBalanced {
		if route, ok := findWorkloadRoute(group, WorkloadFallbackBalanced); ok {
			model, quality, fallback = applyAvailabilityFallback(group, strings.TrimSpace(route.Model), class, effectiveRouteQuality(group, WorkloadFallbackBalanced, route))
			model, quality, fallback = upgradePlanRouteAwayFromLow(group, class, model, quality, fallback)
			return WorkloadFallbackBalanced, model, quality, fallback
		}
	}
	return WorkloadFallbackBalanced, "", effectiveRouteQuality(group, WorkloadFallbackBalanced, WorkloadRoute{}), false
}

func upgradePlanRouteAwayFromLow(group *ServiceGroup, class, model, quality string, fallback bool) (string, string, bool) {
	if class != WorkloadClassPlan && class != WorkloadClassDesign {
		return model, quality, fallback
	}
	if QualityForOfficialTier(model) != QualityLow && NormalizeQuality(quality) != QualityLow && modelQuality(group, model) != QualityLow {
		return model, quality, fallback
	}
	for _, band := range []string{QualityMid, QualityHigh} {
		name := firstUsableModelForQuality(group, band)
		if name == "" || strings.EqualFold(name, model) {
			continue
		}
		return name, band, true
	}
	return model, quality, fallback
}

// FallbackAvailableModel returns a same-group sibling when selected cannot serve.
// Downgrade high→mid→low; upgrade low→mid→high. plan/design never land on low.
func FallbackAvailableModel(group *ServiceGroup, selected, class string) (string, bool) {
	model, _, ok := applyAvailabilityFallback(group, selected, class, "")
	return model, ok
}

func applyAvailabilityFallback(group *ServiceGroup, selected, class, quality string) (string, string, bool) {
	selected = strings.TrimSpace(selected)
	if selected == "" || group == nil {
		return selected, quality, false
	}
	if modelIsUsable(group, selected) {
		return selected, quality, false
	}
	selectedQuality := NormalizeQuality(quality)
	if selectedQuality == "" {
		selectedQuality = modelQuality(group, selected)
	}
	if selectedQuality == "" {
		selectedQuality = QualityForOfficialTier(selected)
	}
	for _, band := range QualityAvailabilityChain(selectedQuality, class) {
		name := firstUsableModelForQuality(group, band)
		if name == "" || strings.EqualFold(name, selected) {
			continue
		}
		nextQuality := NormalizeQuality(band)
		if q := QualityForOfficialTier(name); q != "" {
			nextQuality = q
		} else if q := modelQuality(group, name); q != "" {
			nextQuality = q
		}
		if nextQuality == "" {
			nextQuality = quality
		}
		return name, nextQuality, true
	}
	return selected, quality, false
}

func firstUsableModelForQuality(group *ServiceGroup, quality string) string {
	quality = NormalizeQuality(quality)
	if quality == "" || group == nil {
		return ""
	}
	if official := OfficialTierForQuality(quality); official != "" && modelIsUsable(group, official) {
		return official
	}
	for i := range group.Models {
		name := strings.TrimSpace(group.Models[i].Name)
		if name == "" || IsAutoModel(name) {
			continue
		}
		if modelQuality(group, name) != quality {
			continue
		}
		if modelIsUsable(group, name) {
			return name
		}
	}
	return ""
}

func modelIsUsable(group *ServiceGroup, name string) bool {
	model := findGroupModel(group, name)
	if model == nil || IsAutoModel(model.Name) {
		return false
	}
	if modelHasProviders(model) {
		return true
	}
	return !groupHasAnyProviders(group)
}

func modelHasProviders(model *ModelConfig) bool {
	if model == nil {
		return false
	}
	for _, id := range model.ProviderIDs {
		if strings.TrimSpace(id) != "" {
			return true
		}
	}
	for _, pc := range model.ProviderConfigs {
		if strings.TrimSpace(pc.ProviderID) != "" {
			return true
		}
	}
	return false
}

func groupHasAnyProviders(group *ServiceGroup) bool {
	if group == nil {
		return false
	}
	for i := range group.Models {
		if modelHasProviders(&group.Models[i]) {
			return true
		}
	}
	return false
}

func findWorkloadRoute(group *ServiceGroup, class string) (WorkloadRoute, bool) {
	if group == nil {
		return WorkloadRoute{}, false
	}
	class = NormalizeWorkloadClass(class)
	for _, route := range group.Routes {
		if NormalizeWorkloadClass(route.Class) == class && strings.TrimSpace(route.Model) != "" {
			return route, true
		}
	}
	return WorkloadRoute{}, false
}

func routeQuality(route WorkloadRoute) string {
	if q := NormalizeQuality(route.Quality); q != "" {
		return q
	}
	if q := QualityForOfficialTier(route.Model); q != "" {
		return q
	}
	return ""
}

func effectiveRouteQuality(group *ServiceGroup, class string, route WorkloadRoute) string {
	if q := routeQuality(route); q != "" {
		return q
	}
	floor := ""
	if group != nil {
		floor = NormalizeQuality(group.QualityFloor)
	}
	if (class == WorkloadClassPlan || class == WorkloadClassDesign) && floor == QualityLow {
		return QualityMid
	}
	return floor
}

// ClassifyAndRoute runs L1 for a dynamic group. Concrete model names skip classification.
func ClassifyAndRoute(header http.Header, body map[string]any, group *ServiceGroup) WorkloadDecision {
	requested := ""
	if body != nil {
		requested, _ = body["model"].(string)
	}
	return ClassifyAndRouteModel(header, body, group, requested)
}

// ClassifyAndRouteModel is ClassifyAndRoute with an explicit requested model.
func ClassifyAndRouteModel(header http.Header, body map[string]any, group *ServiceGroup, requestedModel string) WorkloadDecision {
	if group == nil || !IsDynamicKind(group.Kind) {
		return finishDecision(WorkloadDecision{Passthrough: true, ResolvedModel: strings.TrimSpace(requestedModel)}, group, requestedModel)
	}
	if !IsAutoModel(requestedModel) {
		// Pin keeps the model; classify only so plan/design availability omits low.
		class, source := ClassifyWorkload(ClassifyInput{Header: header, Body: body})
		model := strings.TrimSpace(requestedModel)
		quality := QualityForOfficialTier(model)
		model, quality, availabilityFallback := applyAvailabilityFallback(group, model, class, quality)
		upgraded := false
		// Hub resolves auto without live member tags, then forwards the tier.
		// Climb from that tier when the user selected auto. A real pin does not.
		if ClientHeaderSelectsAuto(header) && IsOfficialTierName(CanonicalClientModel(model)) {
			// Hub's sibling retry can name low after a higher band fails.
			// Plan and design still leave that tier before vision runs.
			model, quality, availabilityFallback = upgradePlanRouteAwayFromLow(group, class, model, quality, availabilityFallback)
			next, nextQuality, visionUp, climbed := applyVisionRoute(group, class, model, quality, capabilityNeeds(body), judgeDegree(latestUserRequestText(body)))
			if visionUp {
				model, quality = next, nextQuality
				upgraded = true
				if climbed {
					availabilityFallback = false
				}
			}
		}
		return finishDecision(WorkloadDecision{
			Class:                class,
			RoutedClass:          class,
			Source:               source,
			ResolvedModel:        model,
			Quality:              quality,
			Upgraded:             upgraded,
			AvailabilityFallback: availabilityFallback,
			Passthrough:          true,
		}, group, requestedModel)
	}
	class, source := ClassifyWorkload(ClassifyInput{Header: header, Body: body})
	routedClass, model, quality, degree, upgraded, degreeApplied, availabilityFallback := routeAutoDecision(group, body, class, source)
	return finishDecision(WorkloadDecision{
		Class:                class,
		RoutedClass:          routedClass,
		Source:               source,
		ResolvedModel:        model,
		Quality:              quality,
		Upgraded:             upgraded,
		AvailabilityFallback: availabilityFallback,
		Degree:               degree,
		DegreeApplied:        degreeApplied,
	}, group, requestedModel)
}

const (
	DegreeSimple  = "simple"
	DegreeComplex = "complex"

	degreeSimpleMaxRunes  = 80
	degreeComplexMinRunes = 400

	// capabilityLongTextBytes matches the window tag in capabilityNeeds.
	capabilityLongTextBytes = 12000
)

// judgeDegree reads only the latest user message. Unknown stays empty so the
// class band is kept. A workload keyword is not a small edit: "fix bug" stays
// on its class band instead of falling to low.
func judgeDegree(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	if containsAny(lower, degreeComplexMarkers...) || len([]rune(text)) >= degreeComplexMinRunes {
		return DegreeComplex
	}
	if collapsedRuneCount(text) > degreeSimpleMaxRunes || strings.ContainsAny(text, "\r\n") || strings.Contains(text, "```") {
		return ""
	}
	if containsAny(lower, degreeSubstantialMarkers...) || heuristicClassFromText(lower) != "" {
		return ""
	}
	return DegreeSimple
}

func collapsedRuneCount(text string) int {
	return len([]rune(strings.Join(strings.Fields(text), "")))
}

var degreeSubstantialMarkers = []string{
	"写", "撰写", "实现", "重构", "全文", "报告",
	"write", "implement", "refactor",
}

var degreeComplexMarkers = []string{
	"为什么", "为何", "怎么回事", "根因", "调试", "排查", "架构", "设计",
	"why", "root cause", "debug", "architect",
}

func degreeMayLower(source string) bool {
	switch strings.TrimSpace(source) {
	case ClassSourceHeuristic, ClassSourceFallback, ClassSourceHead:
		return true
	default:
		return false
	}
}

func routeAutoDecision(group *ServiceGroup, body map[string]any, class, source string) (routedClass, model, quality, degree string, upgraded, degreeApplied, availabilityFallback bool) {
	routedClass, model, quality, availabilityFallback = routeWorkloadClass(group, class)
	classModel := model
	degree = judgeDegree(latestUserRequestText(body))
	needs := capabilityNeeds(body)
	// Vision and a long transcript stay off low when the degree gate is the
	// only reason to move. Tools alone must not block a short edit: agent
	// calls carry tools on every turn.
	blockLower := needs["vision"] > 0 || needs["window"] > 0
	if next, nextQuality, ok := applyDegreeGate(group, class, source, model, quality, degree, blockLower); ok {
		model, quality, degreeApplied = next, nextQuality, true
		availabilityFallback = false
	}
	// A band that cannot see is ineligible when another band can. Search
	// upward first. A long transcript does not fall back to low. Plan and
	// design do not land on low. A group with no vision tag stays put.
	// Climb lands on the official tier name, then the in-band pick runs again.
	if next, nextQuality, visionUp, climbed := applyVisionRoute(group, class, model, quality, needs, degree); visionUp {
		model, quality = next, nextQuality
		upgraded = true
		if climbed {
			availabilityFallback = false
		}
	}
	if degreeApplied && strings.EqualFold(model, classModel) {
		degreeApplied = false
	}
	return
}

// applyVisionRoute moves to a model that can see, then prefers the sibling in
// that band that also covers the other needs. climbed is set only when the
// band changes.
func applyVisionRoute(group *ServiceGroup, class, model, quality string, needs map[string]int, degree string) (string, string, bool, bool) {
	changed := false
	climbed := false
	// A long image that Hub retries onto low is not allowed to stay there
	// just because low can see. Chat and classify keep low when degree did
	// not raise them; that band is where those turns belong.
	if next, nextQuality, ok := liftWindowVisionOffLow(group, class, model, quality, degree, needs); ok {
		model, quality = next, nextQuality
		changed = true
		climbed = true
	}
	if next, ok := upgradeModelInBand(group, model, quality, needs); ok {
		model = next
		changed = true
	}
	if next, nextQuality, ok := climbForUnmetVision(group, class, model, quality, needs); ok {
		model, quality = next, nextQuality
		changed = true
		climbed = true
		if again, ok := upgradeModelInBand(group, model, quality, needs); ok {
			model = again
		}
	}
	return model, quality, changed, climbed
}

func applyDegreeGate(group *ServiceGroup, class, source, model, quality, degree string, blockLower bool) (string, string, bool) {
	if strings.TrimSpace(model) == "" {
		return model, quality, false
	}
	class = NormalizeWorkloadClass(class)
	switch class {
	case WorkloadClassPlan, WorkloadClassDesign, WorkloadClassReview:
		return model, quality, false
	}
	current := routedQuality(group, model, quality)
	switch class {
	case WorkloadClassChat, WorkloadClassClassify:
		if degree != DegreeComplex || current != QualityLow {
			return model, quality, false
		}
		name := firstUsableModelForQuality(group, QualityMid)
		if name == "" || strings.EqualFold(name, model) {
			return model, quality, false
		}
		return name, QualityMid, true
	case WorkloadClassCode, WorkloadClassDocWrite, WorkloadClassOps, WorkloadFallbackBalanced:
		if blockLower || degree != DegreeSimple || !degreeMayLower(source) || current != QualityMid {
			return model, quality, false
		}
		name := firstUsableModelForQuality(group, QualityLow)
		if name == "" || strings.EqualFold(name, model) {
			return model, quality, false
		}
		return name, QualityLow, true
	default:
		return model, quality, false
	}
}

func routedQuality(group *ServiceGroup, model, quality string) string {
	if q := NormalizeQuality(quality); q != "" {
		return q
	}
	if q := QualityForOfficialTier(model); q != "" {
		return q
	}
	return modelQuality(group, model)
}

func upgradeModelInBand(group *ServiceGroup, selected, quality string, needs map[string]int) (string, bool) {
	if group == nil || selected == "" || len(needs) == 0 {
		return selected, false
	}
	// A tag on a model with no providers is not a capability this group can
	// dial. Treating it as satisfied would stop the later vision search on
	// an undialable name.
	selectedModel := findGroupModel(group, selected)
	selectedUsable := modelIsUsable(group, selected)
	if selectedUsable && modelHasNeeds(selectedModel, needs) {
		return selected, false
	}
	quality = NormalizeQuality(quality)
	var best *ModelConfig
	bestName := selected
	if selectedUsable {
		best = selectedModel
	}
	improved := false
	for i := range group.Models {
		model := &group.Models[i]
		name := strings.TrimSpace(model.Name)
		if name == "" || strings.EqualFold(name, selected) || IsAutoModel(name) {
			continue
		}
		if modelQuality(group, name) != quality || !modelIsUsable(group, name) {
			continue
		}
		if !betterCapabilityModel(model, best, needs) {
			continue
		}
		best = model
		bestName = name
		improved = true
	}
	return bestName, improved
}

// betterCapabilityModel prefers a model that can see when the request has an
// image. Other needs break the remaining ties by score.
func betterCapabilityModel(candidate, selected *ModelConfig, needs map[string]int) bool {
	if needs["vision"] > 0 && modelHasTag(candidate, "vision") != modelHasTag(selected, "vision") {
		return modelHasTag(candidate, "vision")
	}
	return modelNeedScore(candidate, needs) > modelNeedScore(selected, needs)
}

// liftWindowVisionOffLow moves a long image off low for classes whose own
// band is mid or high. Chat and classify stay on low unless degree already
// called them complex. A transcript without an image keeps the normal
// availability descent, and an explicit tier pin never reaches this helper.
func liftWindowVisionOffLow(group *ServiceGroup, class, model, quality, degree string, needs map[string]int) (string, string, bool) {
	if needs["vision"] == 0 || needs["window"] == 0 {
		return model, quality, false
	}
	if routedQuality(group, model, quality) != QualityLow {
		return model, quality, false
	}
	switch NormalizeWorkloadClass(class) {
	case WorkloadClassChat, WorkloadClassClassify:
		if degree != DegreeComplex {
			return model, quality, false
		}
	}
	for _, band := range []string{QualityMid, QualityHigh} {
		name := firstUsableModelForQuality(group, band)
		if name == "" || strings.EqualFold(name, model) {
			continue
		}
		return name, band, true
	}
	return model, quality, false
}

func climbForUnmetVision(group *ServiceGroup, class, model, quality string, needs map[string]int) (string, string, bool) {
	if group == nil || strings.TrimSpace(model) == "" || needs["vision"] == 0 {
		return model, quality, false
	}
	// An undialable model does not count as seeing. Search continues so a
	// usable band can still take the image.
	if modelIsUsable(group, model) && modelHasTag(findGroupModel(group, model), "vision") {
		return model, quality, false
	}
	quality = routedQuality(group, model, quality)
	for _, band := range visionCandidateBands(quality, class, needs["window"] > 0) {
		name := firstUsableModelWithTag(group, band, "vision")
		if name == "" || strings.EqualFold(name, model) {
			continue
		}
		return name, band, true
	}
	return model, quality, false
}

func visionCandidateBands(quality, class string, holdLow bool) []string {
	class = NormalizeWorkloadClass(class)
	protectLow := holdLow || class == WorkloadClassPlan || class == WorkloadClassDesign
	switch quality {
	case QualityLow:
		return []string{QualityMid, QualityHigh}
	case QualityMid:
		if protectLow {
			return []string{QualityHigh}
		}
		return []string{QualityHigh, QualityLow}
	case QualityHigh:
		if protectLow {
			return []string{QualityMid}
		}
		return []string{QualityMid, QualityLow}
	default:
		return nil
	}
}

func firstUsableModelWithTag(group *ServiceGroup, quality, tag string) string {
	quality = NormalizeQuality(quality)
	tag = strings.ToLower(strings.TrimSpace(tag))
	if quality == "" || tag == "" || group == nil {
		return ""
	}
	if official := OfficialTierForQuality(quality); official != "" && modelIsUsable(group, official) && modelHasTag(findGroupModel(group, official), tag) {
		return official
	}
	for i := range group.Models {
		name := strings.TrimSpace(group.Models[i].Name)
		if name == "" || IsAutoModel(name) || modelQuality(group, name) != quality || !modelIsUsable(group, name) {
			continue
		}
		if modelHasTag(&group.Models[i], tag) {
			return name
		}
	}
	return ""
}

func modelHasTag(model *ModelConfig, tag string) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if model == nil || tag == "" {
		return false
	}
	return modelNeedScore(model, map[string]int{tag: 1}) > 0
}

// ModelHasUsableTag reports that name can be dialed and advertises tag.
// A tag on a model with no providers does not count when the group has other
// providers. That model cannot take the request.
func ModelHasUsableTag(group *ServiceGroup, name, tag string) bool {
	if !modelIsUsable(group, name) {
		return false
	}
	return modelHasTag(findGroupModel(group, name), tag)
}

func QualityForModel(group *ServiceGroup, modelName string) string {
	if q := modelQuality(group, modelName); q != "" {
		return q
	}
	return NormalizeQuality(modelName)
}

func modelQuality(group *ServiceGroup, modelName string) string {
	if q := QualityForOfficialTier(modelName); q != "" {
		return q
	}
	if group == nil {
		return ""
	}
	for _, route := range group.Routes {
		if strings.EqualFold(strings.TrimSpace(route.Model), strings.TrimSpace(modelName)) {
			if q := routeQuality(route); q != "" {
				return q
			}
		}
	}
	return ""
}

func findGroupModel(group *ServiceGroup, name string) *ModelConfig {
	if group == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	for i := range group.Models {
		if strings.EqualFold(strings.TrimSpace(group.Models[i].Name), name) {
			return &group.Models[i]
		}
	}
	canon := CanonicalClientModel(name)
	if !IsOfficialTierName(canon) {
		return nil
	}
	for i := range group.Models {
		if CanonicalClientModel(group.Models[i].Name) == canon {
			return &group.Models[i]
		}
	}
	return nil
}

func modelHasNeeds(model *ModelConfig, needs map[string]int) bool {
	return modelNeedScore(model, needs) >= needWeightSum(needs)
}

func modelNeedScore(model *ModelConfig, needs map[string]int) int {
	if model == nil || len(needs) == 0 {
		return 0
	}
	tags := map[string]struct{}{}
	for _, tag := range model.CapabilityTags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag != "" {
			tags[tag] = struct{}{}
		}
	}
	for _, pc := range model.ProviderConfigs {
		for _, tag := range pc.CapabilityTags {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag != "" {
				tags[tag] = struct{}{}
			}
		}
	}
	score := 0
	for need, weight := range needs {
		if _, ok := tags[need]; ok {
			score += weight
		}
	}
	return score
}

func needWeightSum(needs map[string]int) int {
	sum := 0
	for _, weight := range needs {
		sum += weight
	}
	return sum
}

func capabilityNeeds(body map[string]any) map[string]int {
	needs := map[string]int{}
	if body == nil {
		return needs
	}
	if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
		needs["tools"] += 8
	}
	if toolChoice := strings.ToLower(strings.TrimSpace(stringValue(body["tool_choice"]))); toolChoice != "" && toolChoice != "none" {
		needs["tools"] += 4
	}
	if hasVisionContent(body) {
		needs["vision"] += 8
	}
	if RequestHasLongContext(body) {
		needs["window"] += 4
	}
	return needs
}

// RequestHasVisionContent reports an image block on the chat or responses body.
// Provider order and token-bank member dial both use it so a picture page is
// not rotated onto a text-only upstream.
func RequestHasVisionContent(body map[string]any) bool {
	return hasVisionContent(body)
}

// RequestHasLongContext reports a transcript above the window threshold.
// The comparison is strict greater-than on the full extracted text, matching
// the degree gate. Vision climb and image failover both leave low alone then.
func RequestHasLongContext(body map[string]any) bool {
	return len(extractRequestText(body)) > capabilityLongTextBytes
}

func hasVisionContent(body map[string]any) bool {
	if body == nil {
		return false
	}
	return containsVisionValue(body["messages"]) || containsVisionValue(body["input"])
}

func containsVisionValue(v any) bool {
	switch val := v.(type) {
	case []any:
		for _, item := range val {
			if containsVisionValue(item) {
				return true
			}
		}
	case map[string]any:
		switch strings.ToLower(strings.TrimSpace(stringValue(val["type"]))) {
		case "image_url", "image", "input_image":
			return true
		}
		if val["image_url"] != nil {
			return true
		}
		for _, key := range []string{"content", "parts"} {
			if containsVisionValue(val[key]) {
				return true
			}
		}
	}
	return false
}

// ValidateDynamicServiceGroup checks kind=dynamic shape at save time.
func ValidateDynamicServiceGroup(group *ServiceGroup) error {
	if group == nil || !IsDynamicKind(group.Kind) {
		return nil
	}
	if q := NormalizeQuality(group.QualityFloor); group.QualityFloor != "" && q == "" {
		return fmt.Errorf("quality_floor must be high, mid, or low")
	}
	seen := map[string]WorkloadRoute{}
	for _, route := range group.Routes {
		class := NormalizeWorkloadClass(route.Class)
		if class == "" {
			return fmt.Errorf("invalid workload route class %q", strings.TrimSpace(route.Class))
		}
		if strings.TrimSpace(route.Model) == "" {
			return fmt.Errorf("workload route %s is missing model", class)
		}
		if IsAutoModel(route.Model) {
			return fmt.Errorf("workload route %s cannot target auto", class)
		}
		quality := routeQuality(route)
		if route.Quality != "" && quality == "" {
			return fmt.Errorf("workload route %s has invalid quality %q", class, route.Quality)
		}
		if (class == WorkloadClassPlan || class == WorkloadClassDesign) && quality == QualityLow {
			return fmt.Errorf("workload route %s cannot target quality=low", class)
		}
		if _, ok := seen[class]; ok {
			return fmt.Errorf("duplicate workload route class %s", class)
		}
		seen[class] = route
	}
	for _, required := range RequiredDynamicRoutes {
		if _, ok := seen[required]; !ok {
			return fmt.Errorf("dynamic service group is missing required route %s", required)
		}
	}
	modelNames := map[string]struct{}{}
	for _, model := range group.Models {
		name := strings.TrimSpace(model.Name)
		if name != "" {
			modelNames[strings.ToLower(name)] = struct{}{}
		}
		for _, pc := range model.ProviderConfigs {
			if err := ValidateOfficialProviderModel(pc.Model); err != nil {
				return err
			}
		}
	}
	for class, route := range seen {
		if _, ok := modelNames[strings.ToLower(strings.TrimSpace(route.Model))]; !ok {
			return fmt.Errorf("workload route %s targets unknown model %q", class, route.Model)
		}
	}
	return nil
}

// ValidateOfficialProviderModel accepts empty (static) or an official tier name.
func ValidateOfficialProviderModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	if NormalizeOfficialTier(model) == "" && looksLikeOfficialTier(model) {
		return fmt.Errorf("official provider model must be official-high, official-mid, or official-low")
	}
	if NormalizeOfficialTier(model) == "" {
		return nil
	}
	return nil
}

func looksLikeOfficialTier(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "official-")
}

// EnsureOfficialDynamicTemplate upgrades the convention official group in place.
// Existing concrete model names are kept so old clients skip L1.
func EnsureOfficialDynamicTemplate(group *ServiceGroup) {
	if group == nil || !IsOfficialConventionGroupID(group.ID) {
		return
	}
	group.Kind = ServiceGroupKindDynamic
	if len(group.Routes) == 0 {
		group.Routes = DefaultOfficialAutoRoutes()
	}
	var template *ModelConfig
	if src := firstGroupModel(group, "auto"); src != nil {
		cloned := *src
		copyModelProviders(&cloned, src)
		template = &cloned
	} else if len(group.Models) > 0 {
		cloned := group.Models[0]
		copyModelProviders(&cloned, &group.Models[0])
		template = &cloned
	}
	ensureGroupModel(group, "auto", template)
	ensureGroupModel(group, OfficialTierHigh, template)
	ensureGroupModel(group, OfficialTierMid, template)
	ensureGroupModel(group, OfficialTierLow, template)
}

func firstGroupModel(group *ServiceGroup, name string) *ModelConfig {
	if group == nil {
		return nil
	}
	for i := range group.Models {
		if strings.EqualFold(strings.TrimSpace(group.Models[i].Name), name) {
			return &group.Models[i]
		}
	}
	return nil
}

func ensureGroupModel(group *ServiceGroup, name string, template *ModelConfig) {
	if group == nil || strings.TrimSpace(name) == "" {
		return
	}
	for i := range group.Models {
		if strings.EqualFold(strings.TrimSpace(group.Models[i].Name), name) {
			group.Models[i].Name = name
			if len(group.Models[i].ProviderIDs) == 0 && len(group.Models[i].ProviderConfigs) == 0 && template != nil {
				copyModelProviders(&group.Models[i], template)
			}
			return
		}
	}
	next := ModelConfig{Name: name}
	copyModelProviders(&next, template)
	group.Models = append(group.Models, next)
}

func copyModelProviders(dst *ModelConfig, src *ModelConfig) {
	if dst == nil || src == nil {
		return
	}
	dst.ProviderIDs = append([]string(nil), src.ProviderIDs...)
	dst.CapabilityTags = append([]string(nil), src.CapabilityTags...)
	dst.Priority = src.Priority
	dst.ResolutionTier = src.ResolutionTier
	dst.CreditMultiplier = src.CreditMultiplier
	if len(src.ProviderConfigs) == 0 {
		return
	}
	dst.ProviderConfigs = make([]ModelProviderConfig, len(src.ProviderConfigs))
	copy(dst.ProviderConfigs, src.ProviderConfigs)
}

// PublicCatalogModels returns names a dynamic group may list on /v1/models.
func PublicCatalogModels(group *ServiceGroup) []string {
	if group == nil {
		return nil
	}
	if !IsDynamicKind(group.Kind) {
		names := make([]string, 0, len(group.Models))
		for _, model := range group.Models {
			if name := strings.TrimSpace(model.Name); name != "" {
				names = append(names, name)
			}
		}
		return names
	}
	if len(group.ExposedModels) > 0 {
		out := make([]string, 0, len(group.ExposedModels))
		for _, name := range group.ExposedModels {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	}
	return DefaultCapabilityCatalog()
}

func headerValue(header http.Header, key string) string {
	if header == nil {
		return ""
	}
	return strings.TrimSpace(header.Get(key))
}

func containsAny(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if keyword != "" && strings.Contains(text, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}
