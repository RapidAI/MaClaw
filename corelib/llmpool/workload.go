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
	Class         string           `json:"class"`
	RoutedClass   string           `json:"routed_class,omitempty"`
	Source        string           `json:"class_source,omitempty"`
	ResolvedModel string           `json:"resolved_model,omitempty"`
	Quality       string           `json:"quality,omitempty"`
	Upgraded      bool             `json:"capability_upgraded,omitempty"`
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
	case !dec.Passthrough:
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
func NormalizeOfficialTier(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case OfficialTierHigh:
		return OfficialTierHigh
	case OfficialTierMid:
		return OfficialTierMid
	case OfficialTierLow:
		return OfficialTierLow
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
	text := strings.ToLower(extractRequestText(in.Body))
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
func RouteWorkloadClass(group *ServiceGroup, class string) (routedClass, model, quality string) {
	class = NormalizeWorkloadClass(class)
	if class == "" {
		class = WorkloadFallbackBalanced
	}
	if route, ok := findWorkloadRoute(group, class); ok {
		return class, strings.TrimSpace(route.Model), effectiveRouteQuality(group, class, route)
	}
	if class != WorkloadFallbackBalanced {
		if route, ok := findWorkloadRoute(group, WorkloadFallbackBalanced); ok {
			return WorkloadFallbackBalanced, strings.TrimSpace(route.Model), effectiveRouteQuality(group, WorkloadFallbackBalanced, route)
		}
	}
	return WorkloadFallbackBalanced, "", effectiveRouteQuality(group, WorkloadFallbackBalanced, WorkloadRoute{})
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
		class := hintClass(ClassifyInput{Header: header, Body: body})
		source := ClassSourceHint
		if class == "" {
			class = WorkloadUnclassified
			source = ClassSourceNone
		}
		return finishDecision(WorkloadDecision{
			Class:         class,
			RoutedClass:   class,
			Source:        source,
			ResolvedModel: strings.TrimSpace(requestedModel),
			Passthrough:   true,
		}, group, requestedModel)
	}
	class, source := ClassifyWorkload(ClassifyInput{Header: header, Body: body})
	routedClass, model, quality := RouteWorkloadClass(group, class)
	upgraded := false
	if upgradedModel, ok := upgradeModelInBand(group, model, quality, capabilityNeeds(body)); ok {
		model = upgradedModel
		upgraded = true
	}
	return finishDecision(WorkloadDecision{
		Class:         class,
		RoutedClass:   routedClass,
		Source:        source,
		ResolvedModel: model,
		Quality:       quality,
		Upgraded:      upgraded,
	}, group, requestedModel)
}

func upgradeModelInBand(group *ServiceGroup, selected, quality string, needs map[string]int) (string, bool) {
	if group == nil || selected == "" || len(needs) == 0 {
		return selected, false
	}
	if modelHasNeeds(findGroupModel(group, selected), needs) {
		return selected, false
	}
	quality = NormalizeQuality(quality)
	bestName := selected
	bestScore := modelNeedScore(findGroupModel(group, selected), needs)
	improved := false
	for _, model := range group.Models {
		name := strings.TrimSpace(model.Name)
		if name == "" || strings.EqualFold(name, selected) || IsAutoModel(name) {
			continue
		}
		if modelQuality(group, name) != quality {
			continue
		}
		score := modelNeedScore(&model, needs)
		if score > bestScore {
			bestScore = score
			bestName = name
			improved = true
		}
	}
	return bestName, improved
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
	if len(extractRequestText(body)) > 12000 {
		needs["window"] += 4
	}
	return needs
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
		if strings.EqualFold(stringValue(val["type"]), "image_url") || val["image_url"] != nil {
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
	return []string{"auto"}
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
