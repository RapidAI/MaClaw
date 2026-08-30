package agentservice

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// SkillToolProvider enables the agent loop to discover and execute Skills.
// Satisfied by *SkillToolBridge.
type SkillToolProvider interface {
	// ListSkills returns all active skills for the principal.
	ListSkills(ctx context.Context, p Principal) []SkillToolEntry

	// InstallSkill installs a skill from an allowed source for the principal.
	InstallSkill(ctx context.Context, p Principal, args map[string]interface{}) ([]corelib.NLSkillEntry, error)

	// RunSkill executes a skill by name with the given arguments.
	RunSkill(ctx context.Context, p Principal, name string, args map[string]interface{}) (string, error)

	// SearchSkills searches for skills across configured sources.
	SearchSkills(ctx context.Context, p Principal, query string) ([]SkillSearchResult, error)
}

// SkillDynamicContractResolver is a control-plane lookup. It is deliberately
// separate from Skill descriptions/triggers so untrusted Skill package content
// can never self-register a routable capability at execution time.
type SkillDynamicContractResolver interface {
	ResolveSkillDynamicContract(ctx context.Context, p Principal, stableID string) (DynamicCapabilityContract, bool)
}

type skillMaintenancePlanner interface {
	BuildSkillMaintenancePlan(ctx context.Context, p Principal, opts skill.SkillMaintenancePlanOptions) (skill.SkillMaintenancePlan, error)
}

// SkillToolEntry represents a single installed skill available for the agent.
type SkillToolEntry struct {
	StableID      string
	Name          string
	Description   string
	Mode          string // sequential/interactive/api_workflow
	Version       string
	ContentDigest string
	Params        []corelib.NLSkillParam
	// Contract is resolved by a trusted control-plane publisher. Active or
	// installed status alone never makes a Skill executable by the Agent.
	Contract DynamicCapabilityContract
}

// SkillBinding is the immutable executable identity selected from an active
// skill inventory. It deliberately excludes the display description and
// dynamic name-based lookup from the model-facing call surface.
type SkillBinding struct {
	StableID       string
	Name           string
	Version        string
	ContentDigest  string
	ContractDigest string
}

func (b SkillBinding) BindingID() string {
	return strings.Join([]string{strings.TrimSpace(b.StableID), strings.TrimSpace(b.Version), strings.TrimSpace(b.ContentDigest), strings.TrimSpace(b.ContractDigest)}, ":")
}

type boundSkillCallSurface struct {
	mu       sync.RWMutex
	adapters map[string]boundSkillAdapter
}

type boundSkillAdapter struct {
	Binding    SkillBinding
	Parameters map[string]interface{}
}

func newBoundSkillCallSurface() *boundSkillCallSurface {
	return &boundSkillCallSurface{adapters: make(map[string]boundSkillAdapter)}
}

func (s *boundSkillCallSurface) replace(adapters map[string]boundSkillAdapter) {
	if s == nil {
		return
	}
	clone := make(map[string]boundSkillAdapter, len(adapters))
	for name, adapter := range adapters {
		clone[name] = boundSkillAdapter{Binding: adapter.Binding, Parameters: cloneMCPJSONValue(adapter.Parameters).(map[string]interface{})}
	}
	s.mu.Lock()
	s.adapters = clone
	s.mu.Unlock()
}

func (s *boundSkillCallSurface) adapter(name string) (boundSkillAdapter, bool) {
	if s == nil {
		return boundSkillAdapter{}, false
	}
	s.mu.RLock()
	adapter, ok := s.adapters[strings.TrimSpace(name)]
	s.mu.RUnlock()
	return adapter, ok
}

// SkillToolBridge implements SkillToolProvider by delegating to the Service's
// existing skill management infrastructure.
type SkillToolBridge struct {
	svc       *Service
	contracts SkillDynamicContractResolver
}

// DynamicCatalogLifecycle records whether the active Skill inventory could be
// read for this principal. A failed list operation must remain
// catalog_incomplete rather than being mistaken for an empty, complete Skill
// family and silently hiding a requested capability.
func (b *SkillToolBridge) DynamicCatalogLifecycle(ctx context.Context, p Principal) DynamicCatalogLifecycle {
	_, lifecycle := b.DynamicSkillInventory(ctx, p)
	return lifecycle
}

// DynamicSkillInventory observes active skills once and derives both the
// catalog entries and coverage result from that same observation. This avoids
// a list succeeding for planning while a second list fails (or changes) when
// reporting readiness for the exact same user turn.
func (b *SkillToolBridge) DynamicSkillInventory(ctx context.Context, p Principal) ([]SkillToolEntry, DynamicCatalogLifecycle) {
	if b == nil || b.svc == nil {
		return nil, IncompleteDynamicCatalogLifecycle("catalog_incomplete")
	}
	contracts, err := b.contractSnapshot(p)
	if err != nil {
		return nil, IncompleteDynamicCatalogLifecycle("contract_registry_unavailable")
	}
	items, err := b.svc.ListSkills(ctx, p)
	if err != nil {
		return nil, IncompleteDynamicCatalogLifecycle("catalog_incomplete")
	}
	return b.entriesFromSnapshotWithContracts(ctx, p, items, contracts), CompleteDynamicCatalogLifecycle()
}

// SetSkillDynamicContractResolver installs the control-plane capability lookup.
// Without it, installed Skills remain quarantined from Agent execution.
func (b *SkillToolBridge) SetSkillDynamicContractResolver(resolver SkillDynamicContractResolver) {
	if b == nil {
		return
	}
	b.contracts = resolver
}

// NewSkillToolBridge creates a bridge that connects the CoreAgentExecutor to
// the Service's skill management layer.
func NewSkillToolBridge(svc *Service) *SkillToolBridge {
	bridge := &SkillToolBridge{svc: svc}
	if svc != nil {
		bridge.contracts = svc.DynamicCapabilityContracts()
	}
	return bridge
}

// ListSkills returns all active skills for the principal.
func (b *SkillToolBridge) ListSkills(ctx context.Context, p Principal) []SkillToolEntry {
	items, err := b.svc.ListSkills(ctx, p)
	if err != nil {
		return nil
	}
	return b.entriesFromSnapshot(ctx, p, items)
}

// entriesFromSnapshot applies only deterministic per-entry projection to the
// already-observed skill list. The control-plane contract lookup is scoped to
// the same principal and does not use skill descriptions as routing authority.
func (b *SkillToolBridge) entriesFromSnapshot(ctx context.Context, p Principal, items []corelib.NLSkillEntry) []SkillToolEntry {
	return b.entriesFromSnapshotWithContracts(ctx, p, items, b.contracts)
}

func (b *SkillToolBridge) entriesFromSnapshotWithContracts(ctx context.Context, p Principal, items []corelib.NLSkillEntry, contracts SkillDynamicContractResolver) []SkillToolEntry {
	entries := make([]SkillToolEntry, 0, len(items))
	for _, item := range items {
		status := strings.ToLower(strings.TrimSpace(item.Status))
		if status != "" && status != "active" {
			continue
		}
		entry := SkillToolEntry{
			StableID:      skillStableID(item),
			Name:          item.Name,
			Description:   item.Description,
			Mode:          item.Mode,
			Version:       item.Version,
			ContentDigest: skillContentDigest(item),
			Params:        append([]corelib.NLSkillParam(nil), item.Params...),
		}
		if contracts != nil {
			entry.Contract, _ = contracts.ResolveSkillDynamicContract(ctx, p, entry.StableID)
			if !dynamicSkillContractMatchesEntry(entry.Contract, entry) {
				entry.Contract = DynamicCapabilityContract{}
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

func dynamicSkillObservedBindingDigest(stableID, version, contentDigest string) string {
	return coretool.SchemaDigest([]byte(strings.Join([]string{
		"skill",
		strings.TrimSpace(stableID),
		strings.TrimSpace(version),
		strings.TrimSpace(contentDigest),
	}, "\x00")))
}

// DynamicSkillObservedBindingDigest is the reviewed-control-plane identity
// for an installed Skill package. It is exported for hosts that persist
// contracts independently from Service, while keeping declaration authority
// separate from discovery and Agent execution.
func DynamicSkillObservedBindingDigest(stableID, version, contentDigest string) string {
	return dynamicSkillObservedBindingDigest(stableID, version, contentDigest)
}

func dynamicSkillContractMatchesEntry(contract DynamicCapabilityContract, entry SkillToolEntry) bool {
	want := strings.TrimSpace(contract.ObservedBindingDigest)
	return want != "" && want == dynamicSkillObservedBindingDigest(entry.StableID, entry.Version, entry.ContentDigest)
}

func (b *SkillToolBridge) contractSnapshot(p Principal) (SkillDynamicContractResolver, error) {
	if b == nil || b.contracts == nil {
		return nil, nil
	}
	provider, ok := b.contracts.(dynamicCapabilityContractSnapshotProvider)
	if !ok {
		return b.contracts, nil
	}
	snapshot, err := provider.SnapshotDynamicCapabilityContracts(p)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func skillStableID(entry corelib.NLSkillEntry) string {
	if stableID := strings.TrimSpace(entry.SkillID); stableID != "" {
		return stableID
	}
	if stableID := strings.TrimSpace(entry.HubSkillID); stableID != "" {
		return stableID
	}
	return "legacy:" + strings.ToLower(strings.TrimSpace(entry.Name))
}

// DynamicSkillStableID returns the immutable identity used by the dynamic
// catalog and binding validator. Display names remain only a compatibility
// lookup aid and are never a contract key.
func DynamicSkillStableID(entry corelib.NLSkillEntry) string { return skillStableID(entry) }

func skillContentDigest(entry corelib.NLSkillEntry) string {
	payload := struct {
		StableID   string
		Name       string
		Version    string
		Mode       string
		Steps      []corelib.NLSkillStep
		Params     []corelib.NLSkillParam
		Operations []corelib.NLSkillOperation
		Pipeline   []corelib.SkillPipelineStep
	}{
		StableID:   skillStableID(entry),
		Name:       entry.Name,
		Version:    entry.Version,
		Mode:       entry.Mode,
		Steps:      entry.Steps,
		Params:     entry.Params,
		Operations: entry.Operations,
		Pipeline:   entry.Pipeline,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return coretool.SchemaDigest([]byte("invalid-skill"))
	}
	return coretool.SchemaDigest(data)
}

// DynamicSkillContentDigest returns the canonical package/runtime digest used
// by dynamic Skill bindings. A GUI or other host must not approximate this
// value from a display name or description when publishing/revalidating a
// reviewed contract.
func DynamicSkillContentDigest(entry corelib.NLSkillEntry) string { return skillContentDigest(entry) }

// BuildSkillMaintenancePlan returns a read-only local curator plan for the
// principal's installed skills. It does not mutate, execute, archive, or merge
// any skill.
func (b *SkillToolBridge) BuildSkillMaintenancePlan(ctx context.Context, p Principal, opts skill.SkillMaintenancePlanOptions) (skill.SkillMaintenancePlan, error) {
	items, err := b.svc.ListSkills(ctx, p)
	if err != nil {
		return skill.SkillMaintenancePlan{}, err
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	return skill.BuildSkillMaintenancePlan(items, opts), nil
}

// InstallSkill installs a skill using the Service's user-scoped skill
// lifecycle. Source allow-lists are enforced by Service.InstallSkill.
func (b *SkillToolBridge) InstallSkill(ctx context.Context, p Principal, args map[string]interface{}) ([]corelib.NLSkillEntry, error) {
	in := SkillInstallInput{
		Source:         normalizeSkillInstallToolSource(firstNonEmptySkillArg(args, "source", "origin")),
		RepoURL:        stringArg(args, "repo_url"),
		RawURL:         stringArg(args, "raw_url"),
		RepoFullName:   stringArg(args, "repo_full_name"),
		FilePath:       stringArg(args, "file_path"),
		Branch:         stringArg(args, "branch"),
		DefinitionType: stringArg(args, "definition_type"),
		ZipBase64:      stringArg(args, "zip_base64"),
		SkillHubURL:    firstNonEmptySkillArg(args, "skill_hub_url", "hub_url"),
		SkillMarketURL: firstNonEmptySkillArg(args, "skill_market_url", "market_url"),
		SkillID:        firstNonEmptySkillArg(args, "skill_id", "id"),
		Overwrite:      skillBoolArg(args, "overwrite"),
		GitHubToken:    stringArg(args, "github_token"),
	}
	applySkillInstallRef(&in, stringArg(args, "install_ref"))
	if in.Source == "" {
		in.Source = inferSkillInstallInputSource(in)
	}
	if in.Source == "github" && in.RawURL == "" && in.RepoURL != "" {
		in.Source = "github_repo"
	}
	if in.Source == "" {
		in.Source = inferSkillInstallSource(args)
	}
	return b.svc.InstallSkill(ctx, p, in)
}

// RunSkill executes a legacy name-addressed skill request. Semantic adapters
// must use CallBoundSkill instead: the legacy path deliberately retains
// compatibility alias matching while the bound path executes one exact entry.
func (b *SkillToolBridge) RunSkill(ctx context.Context, p Principal, name string, args map[string]interface{}) (string, error) {
	entry, err := b.svc.GetSkill(ctx, p, name)
	if err != nil {
		return "", fmt.Errorf("skill %q not found: %w", name, err)
	}
	return b.runSkillEntry(ctx, p, entry, args)
}

// runSkillEntry executes an already selected skill entry.  It intentionally
// does not perform any identity lookup: callers that have a semantic binding
// must carry the exact entry from their validation observation through this
// execution boundary.
func (b *SkillToolBridge) runSkillEntry(ctx context.Context, p Principal, entry *corelib.NLSkillEntry, args map[string]interface{}) (string, error) {
	if entry == nil {
		return "", fmt.Errorf("skill entry is required")
	}
	if b == nil || b.svc == nil || b.svc.hasPendingSkillCompensation(entry.Name) {
		return "", fmt.Errorf("skill %q is blocked by pending compensation", entry.Name)
	}
	status := strings.ToLower(strings.TrimSpace(entry.Status))
	if status != "" && status != "active" {
		return "", fmt.Errorf("skill %q is not active (status=%s)", entry.Name, status)
	}

	// Normalize the skill for execution.
	skill.NormalizeSkillForRunner(entry)

	if len(entry.Steps) == 0 {
		return "", fmt.Errorf("skill %q has no executable steps", entry.Name)
	}

	// Build template variables from args.
	vars := make(map[string]string)
	if input, ok := args["input"].(string); ok && input != "" {
		vars["input"] = input
	}
	if output, ok := args["output"].(string); ok && output != "" {
		vars["output"] = output
	}
	if argsMap, ok := args["args"].(map[string]interface{}); ok {
		for k, v := range argsMap {
			if s, ok := v.(string); ok {
				vars[k] = s
			} else if v != nil {
				data, _ := json.Marshal(v)
				vars[k] = string(data)
			}
		}
	}

	// Resolve selected steps for api_workflow mode.
	selectedSteps, _ := skill.ResolveSelectedStepLabels(entry, args)

	timeoutSec := b.runSkillTimeoutSec(p, entry)

	// Execute using the shared synchronous runner.
	result, err := skill.ExecuteStepsSync(ctx, entry, vars, skill.ExecConfig{
		SkillDir:      entry.SkillDir,
		Timeout:       time.Duration(timeoutSec) * time.Second,
		Params:        entry.Params,
		SelectedSteps: selectedSteps,
	}, &srvExecDeps{})
	if err != nil {
		if result != nil && result.LastStepOutput != "" {
			return result.LastStepOutput, err
		}
		return "", err
	}
	return result.Output, nil
}

// BindSkill resolves one active skill before it is rendered into a model
// adapter. The caller holds only this immutable binding afterwards; aliases or
// name collisions cannot cause execution to drift to another skill.
func BindSkill(entries []SkillToolEntry, stableID, name string) (SkillBinding, error) {
	stableID = strings.TrimSpace(stableID)
	name = strings.TrimSpace(name)
	if stableID == "" || name == "" {
		return SkillBinding{}, fmt.Errorf("skill binding requires stable identity and name")
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.StableID) != stableID || strings.TrimSpace(entry.Name) != name {
			continue
		}
		if strings.TrimSpace(entry.ContentDigest) == "" {
			return SkillBinding{}, fmt.Errorf("skill %q has no content digest", name)
		}
		if err := entry.Contract.validate(); err != nil {
			return SkillBinding{}, fmt.Errorf("skill %q is quarantined: %w", name, err)
		}
		return SkillBinding{StableID: stableID, Name: name, Version: strings.TrimSpace(entry.Version), ContentDigest: strings.TrimSpace(entry.ContentDigest), ContractDigest: entry.Contract.Digest()}, nil
	}
	return SkillBinding{}, fmt.Errorf("skill %q is not active", name)
}

// CallBoundSkill revalidates the selected content/version before entering the
// legacy runner. It intentionally turns model business arguments into the
// runner's server-owned args envelope instead of accepting a model-provided
// skill name or control action.
func (b *SkillToolBridge) CallBoundSkill(ctx context.Context, p Principal, binding SkillBinding, arguments map[string]interface{}) (string, error) {
	entry, err := b.resolveBoundSkill(ctx, p, binding)
	if err != nil {
		return "", err
	}
	return b.runSkillEntry(ctx, p, entry, map[string]interface{}{"args": arguments})
}

// resolveBoundSkill obtains one current principal-scoped inventory observation,
// identifies exactly one StableID, then proves that its immutable content and
// reviewed contract still match the materialized binding.  In particular, it
// never calls GetSkill/findSkill or MatchesName: those compatibility helpers
// can resolve an alias to a different installed package.
func (b *SkillToolBridge) resolveBoundSkill(ctx context.Context, p Principal, binding SkillBinding) (*corelib.NLSkillEntry, error) {
	if b == nil || b.svc == nil {
		return nil, fmt.Errorf("skill_bound_execution_unavailable")
	}
	stableID := strings.TrimSpace(binding.StableID)
	if stableID == "" || strings.TrimSpace(binding.Name) == "" || strings.TrimSpace(binding.ContentDigest) == "" {
		return nil, fmt.Errorf("skill_binding_stale")
	}
	contracts, err := b.contractSnapshot(p)
	if err != nil {
		return nil, fmt.Errorf("skill_binding_stale")
	}
	items, err := b.svc.ListSkills(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("skill_binding_stale")
	}
	var target *corelib.NLSkillEntry
	for _, item := range items {
		if skillStableID(item) != stableID {
			continue
		}
		// An ambiguous stable identity is a lifecycle failure, not permission
		// to select whichever same-name entry happens to appear first.
		if target != nil {
			return nil, fmt.Errorf("skill_binding_stale")
		}
		copy := item
		target = &copy
	}
	if target == nil || !isActiveSkillStatus(target.Status) ||
		strings.TrimSpace(target.Name) != strings.TrimSpace(binding.Name) ||
		strings.TrimSpace(target.Version) != strings.TrimSpace(binding.Version) ||
		skillContentDigest(*target) != strings.TrimSpace(binding.ContentDigest) {
		return nil, fmt.Errorf("skill_binding_stale")
	}
	entries := b.entriesFromSnapshotWithContracts(ctx, p, []corelib.NLSkillEntry{*target}, contracts)
	fresh, err := BindSkill(entries, stableID, target.Name)
	if err != nil || fresh.BindingID() != binding.BindingID() {
		return nil, fmt.Errorf("skill_binding_stale")
	}
	return target, nil
}

func isActiveSkillStatus(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return status == "" || status == "active"
}

func (b *SkillToolBridge) runSkillTimeoutSec(p Principal, entry *corelib.NLSkillEntry) int {
	timeoutSec := corelib.DefaultSkillRunnerTimeoutSec
	if b != nil && b.svc != nil {
		if cfg, cfgErr := b.svc.getOrLoadUserConfig(p.TenantID, p.UserID); cfgErr == nil {
			timeoutSec = corelib.NormalizeSkillRunnerTimeoutSec(cfg.AppConfig.SkillRunnerTimeoutSec)
		}
	}
	if entry != nil && entry.GlobalTimeout > 0 {
		return entry.GlobalTimeout
	}
	return timeoutSec
}

// SearchSkills searches for skills across configured sources.
func (b *SkillToolBridge) SearchSkills(ctx context.Context, p Principal, query string) ([]SkillSearchResult, error) {
	return b.svc.SearchSkills(ctx, p, SkillSearchInput{
		Query: query,
		TopN:  10,
	})
}

func firstNonEmptySkillArg(args map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringArg(args, key)); value != "" {
			return value
		}
	}
	return ""
}

func skillBoolArg(args map[string]interface{}, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1"
	default:
		return false
	}
}

func inferSkillInstallSource(args map[string]interface{}) string {
	hubURL := strings.ToLower(strings.TrimSpace(stringArg(args, "hub_url")))
	switch {
	case hubURL == "github" || strings.Contains(hubURL, "github.com"):
		return "github"
	case strings.Contains(hubURL, "clawhub"):
		return "clawhub"
	}
	if strings.TrimSpace(stringArg(args, "zip_base64")) != "" {
		return "zip"
	}
	if strings.TrimSpace(stringArg(args, "repo_url")) != "" {
		return "github_repo"
	}
	if strings.TrimSpace(stringArg(args, "raw_url")) != "" || strings.TrimSpace(stringArg(args, "repo_full_name")) != "" {
		return "github"
	}
	if strings.TrimSpace(stringArg(args, "skill_id")) != "" || strings.TrimSpace(stringArg(args, "id")) != "" {
		if strings.TrimSpace(firstNonEmptySkillArg(args, "skill_market_url", "market_url")) != "" {
			return "skillmarket"
		}
		return "skillhub"
	}
	return ""
}

func inferSkillInstallInputSource(in SkillInstallInput) string {
	if strings.TrimSpace(in.ZipBase64) != "" {
		return "zip"
	}
	if strings.TrimSpace(in.RawURL) != "" || strings.TrimSpace(in.RepoFullName) != "" {
		return "github"
	}
	if strings.TrimSpace(in.RepoURL) != "" {
		return "github_repo"
	}
	if strings.TrimSpace(in.SkillID) != "" {
		if strings.TrimSpace(in.SkillMarketURL) != "" {
			return "skillmarket"
		}
		return "skillhub"
	}
	return ""
}

func normalizeSkillInstallToolSource(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	switch {
	case source == "github" || strings.Contains(source, "github.com"):
		return "github"
	case strings.Contains(source, "clawhub"):
		return "clawhub"
	case source == "hubcenter" || source == "hub_center" || source == "market":
		return "skillmarket"
	default:
		return source
	}
}

func applySkillInstallRef(in *SkillInstallInput, installRef string) {
	if in == nil {
		return
	}
	installRef = strings.TrimSpace(installRef)
	if installRef == "" {
		return
	}
	var cand skill.GitHubSkillCandidate
	if strings.HasPrefix(installRef, "{") && json.Unmarshal([]byte(installRef), &cand) == nil {
		if in.RepoURL == "" {
			in.RepoURL = cand.RepoURL
		}
		if in.RawURL == "" {
			in.RawURL = cand.RawURL
		}
		if in.RepoFullName == "" {
			in.RepoFullName = cand.RepoFullName
		}
		if in.FilePath == "" {
			in.FilePath = cand.FilePath
		}
		if in.Branch == "" {
			in.Branch = cand.Branch
		}
		if in.DefinitionType == "" {
			in.DefinitionType = cand.DefinitionType
		}
		return
	}
	if in.RawURL == "" && (strings.Contains(installRef, "raw.githubusercontent.com") || strings.Contains(installRef, "/raw/")) {
		in.RawURL = installRef
		return
	}
	if in.RepoURL == "" {
		in.RepoURL = installRef
	}
}

// srvExecDeps implements skill.ExecDeps for MaClawSrv.
type srvExecDeps struct{}

func (d *srvExecDeps) ExecuteBash(ctx context.Context, command, workDir string, env map[string]string) (string, error) {
	return executeBashCommand(ctx, command, workDir, env)
}

func (d *srvExecDeps) OnStepProgress(stepIndex, totalSteps int, stepAction, status string) {
	// MaClawSrv: no-op for now. Could be wired to streaming progress in the future.
}

// executeBashCommand runs a command and returns combined stdout+stderr.
// The caller controls timeout via the context — this function does NOT
// add its own timeout to avoid double-timeout issues with ExecuteStepsSync.
func executeBashCommand(ctx context.Context, command, workDir string, extraEnv map[string]string) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command(coretool.ResolveCmdExe(), "/c", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	if workDir != "" {
		cmd.Dir = workDir
	}
	// Set UTF-8 environment + caller-provided env vars.
	env := append(cmd.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	for k, v := range extraEnv {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	coretool.PrepareCommandForTreeKill(cmd)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Start()
	if err == nil {
		err = coretool.WaitCommandWithContext(ctx, cmd)
	}
	return output.String(), err
}

// SetSkillToolProvider wires the skill tool provider into the executor.
func (e *CoreAgentExecutor) SetSkillToolProvider(provider SkillToolProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.skillProvider = provider
}

// --- Integration into coreAgentCallbacks ---

// executeManageSkill dispatches the manage_skill tool.
func (c *coreAgentCallbacks) executeManageSkill(args map[string]interface{}) agent.ToolExecutionResult {
	if c.skillProvider == nil {
		return agent.ToolExecutionResult{Result: "Error: skill system is not configured", Outcome: agent.ToolExecutionOutcomeError}
	}
	action := stringArg(args, "action")
	switch skill.NormalizeManageSkillAction(action) {
	case "list":
		return c.skillList()
	case "search":
		return c.skillSearch(args)
	case "install":
		return c.skillInstall(args)
	case "run":
		return c.skillRun(args)
	case "maintenance_plan":
		return c.skillMaintenancePlan(args)
	case "upload":
		return agent.ToolExecutionResult{Result: "Error: manage_skill(action=\"upload\") is not supported by this provider", Outcome: agent.ToolExecutionOutcomeError}
	case "execute_maintenance_plan":
		return agent.ToolExecutionResult{Result: "Error: execute_maintenance_plan is not supported by this provider", Outcome: agent.ToolExecutionOutcomeError}
	case "maintenance_drafts":
		return agent.ToolExecutionResult{
			Result:  `{"ok":false,"error":"maintenance_drafts is not supported by this provider; use the desktop or TUI client"}`,
			Outcome: agent.ToolExecutionOutcomeError,
		}
	case "evolution_status":
		// MaClawSrv does not host the desktop EvolutionPipeline; report capability gap.
		return agent.ToolExecutionResult{
			Result:  `{"ok":true,"non_executing":true,"pipeline_started":false,"enable_repair":false,"enable_optimizer":false,"enable_promoter":false,"note":"skill evolution pipeline is a desktop/TUI feature"}`,
			Outcome: agent.ToolExecutionOutcomeOK,
		}
	case "evolution_compensations":
		items, err := skill.ListEvolutionCompensationSummaries()
		if err != nil {
			return agent.ToolExecutionResult{Result: fmt.Sprintf(`{"ok":false,"error":%q,"fail_closed":true}`, err.Error()), Outcome: agent.ToolExecutionOutcomeError}
		}
		data, _ := json.Marshal(map[string]interface{}{"ok": true, "non_executing": true, "items": items, "count": len(items), "note": "desktop pipeline recovery is startup-controlled"})
		return agent.ToolExecutionResult{Result: string(data), Outcome: agent.ToolExecutionOutcomeOK}
	case "evolution_audit":
		// Best-effort: if the shared audit file exists on this host, return it.
		limit := 50
		if args != nil {
			switch v := args["limit"].(type) {
			case float64:
				limit = int(v)
			case int:
				limit = v
			}
		}
		skillFilter := ""
		if args != nil {
			if s, ok := args["name"].(string); ok {
				skillFilter = s
			} else if s, ok := args["skill"].(string); ok {
				skillFilter = s
			}
		}
		payload := skill.EvolutionAuditToolPayload("", limit, skillFilter)
		payload["note"] = "reads local desktop audit JSONL when present on this host"
		data, _ := json.MarshalIndent(payload, "", "  ")
		return agent.ToolExecutionResult{Result: string(data), Outcome: agent.ToolExecutionOutcomeOK}
	case "set_evolution_enabled":
		return agent.ToolExecutionResult{
			Result:  `{"ok":false,"error":"set_evolution_enabled is not supported by this provider; use the desktop or TUI client"}`,
			Outcome: agent.ToolExecutionOutcomeError,
		}
	case "trigger_repair":
		return agent.ToolExecutionResult{
			Result:  `{"ok":false,"error":"trigger_repair is not supported by this provider; use the desktop or TUI client"}`,
			Outcome: agent.ToolExecutionOutcomeError,
		}
	case "trigger_optimize":
		return agent.ToolExecutionResult{
			Result:  `{"ok":false,"error":"trigger_optimize is not supported by this provider; use the desktop or TUI client"}`,
			Outcome: agent.ToolExecutionOutcomeError,
		}
	default:
		return agent.ToolExecutionResult{
			Result:  skill.ManageSkillUnknownActionError(action),
			Outcome: agent.ToolExecutionOutcomeError,
		}
	}
}

func (c *coreAgentCallbacks) skillRun(args map[string]interface{}) agent.ToolExecutionResult {
	if c.skillProvider == nil {
		return agent.ToolExecutionResult{Result: "Error: skill system is not configured", Outcome: agent.ToolExecutionOutcomeError}
	}
	name := firstNonEmpty(stringArg(args, "name"), stringArg(args, "skill"), stringArg(args, "skill_id"))
	if strings.TrimSpace(name) == "" {
		return agent.ToolExecutionResult{Result: "Error: manage_skill(action=\"run\") requires name", Outcome: agent.ToolExecutionOutcomeError}
	}
	out, err := c.skillProvider.RunSkill(c.parentContext(), c.principal, name, manageSkillRunArgs(args))
	if err != nil {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: run skill %q failed: %v", name, err), Outcome: agent.ToolExecutionOutcomeError}
	}
	if strings.TrimSpace(out) == "" {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Skill %q finished with no output.", name), Outcome: agent.ToolExecutionOutcomeOK}
	}
	return toolTextResult(out)
}

func manageSkillRunArgs(args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return map[string]interface{}{}
	}
	out := map[string]interface{}{}
	if input, ok := args["input"]; ok {
		out["input"] = input
	}
	if output, ok := args["output"]; ok {
		out["output"] = output
	}
	if nested := skillRunArgsObject(args["args"]); nested != nil {
		out["args"] = nested
		return out
	}
	reserved := map[string]bool{
		"action": true, "name": true, "skill": true, "skill_id": true,
		"hub_url": true, "query": true, "input": true, "output": true,
	}
	nested := map[string]interface{}{}
	for key, value := range args {
		if reserved[strings.ToLower(strings.TrimSpace(key))] {
			continue
		}
		nested[key] = value
	}
	if len(nested) > 0 {
		out["args"] = nested
	}
	return out
}

func skillRunArgsObject(value interface{}) map[string]interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed
	case string:
		raw := strings.TrimSpace(typed)
		if raw == "" {
			return nil
		}
		var nested map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &nested); err != nil || nested == nil {
			return nil
		}
		return nested
	default:
		return nil
	}
}

func (c *coreAgentCallbacks) skillInstall(args map[string]interface{}) agent.ToolExecutionResult {
	entries, err := c.skillProvider.InstallSkill(c.parentContext(), c.principal, args)
	if err != nil {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: install failed: %v", err), Outcome: agent.ToolExecutionOutcomeError}
	}
	if len(entries) == 0 {
		return agent.ToolExecutionResult{Result: "No skill was installed.", Outcome: agent.ToolExecutionOutcomeOK}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Installed skills (%d):\n", len(entries)))
	for _, entry := range entries {
		b.WriteString(fmt.Sprintf("  - %s", entry.Name))
		if strings.TrimSpace(entry.Description) != "" {
			b.WriteString(": ")
			b.WriteString(entry.Description)
		}
		b.WriteByte('\n')
	}
	return agent.ToolExecutionResult{Result: b.String(), Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) skillList() agent.ToolExecutionResult {
	entries := c.skillProvider.ListSkills(c.parentContext(), c.principal)
	if len(entries) == 0 {
		return agent.ToolExecutionResult{Result: "No skills installed.", Outcome: agent.ToolExecutionOutcomeOK}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Installed skills (%d):\n", len(entries)))
	for _, e := range entries {
		desc := e.Description
		if len(desc) > 80 {
			desc = desc[:80] + "..."
		}
		b.WriteString(fmt.Sprintf("  - %s: %s\n", e.Name, desc))
	}
	return agent.ToolExecutionResult{Result: b.String(), Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) skillSearch(args map[string]interface{}) agent.ToolExecutionResult {
	query := stringArg(args, "query")
	if query == "" {
		return agent.ToolExecutionResult{Result: "Error: missing query parameter", Outcome: agent.ToolExecutionOutcomeError}
	}
	results, err := c.skillProvider.SearchSkills(c.parentContext(), c.principal, query)
	if err != nil {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: search failed: %v", err), Outcome: agent.ToolExecutionOutcomeError}
	}
	if len(results) == 0 {
		return agent.ToolExecutionResult{Result: "No skills found for query: " + query, Outcome: agent.ToolExecutionOutcomeOK}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Search results for %q (%d):\n", query, len(results)))
	for i, r := range results {
		if i >= 10 {
			b.WriteString(fmt.Sprintf("  ... and %d more\n", len(results)-10))
			break
		}
		b.WriteString(fmt.Sprintf("  %d. [%s] %s — %s\n", i+1, r.Source, r.Name, r.Description))
		if r.ID != "" {
			b.WriteString(fmt.Sprintf("     ID: %s\n", r.ID))
		}
	}
	return agent.ToolExecutionResult{Result: b.String(), Outcome: agent.ToolExecutionOutcomeOK}
}

func (c *coreAgentCallbacks) skillMaintenancePlan(args map[string]interface{}) agent.ToolExecutionResult {
	planner, ok := c.skillProvider.(skillMaintenancePlanner)
	if !ok {
		return agent.ToolExecutionResult{Result: "Error: skill maintenance planning is not supported by this provider", Outcome: agent.ToolExecutionOutcomeError}
	}
	plan, err := planner.BuildSkillMaintenancePlan(c.parentContext(), c.principal, skill.SkillMaintenancePlanOptions{
		Now:                 time.Now(),
		StaleAfterDays:      intArg(args, "stale_after_days", 0),
		MinFailureRuns:      intArg(args, "min_failure_runs", 0),
		MaxActions:          intArg(args, "max_actions", 0),
		DuplicateSimilarity: skillFloatArg(args, "duplicate_similarity"),
	})
	if err != nil {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: maintenance plan failed: %v", err), Outcome: agent.ToolExecutionOutcomeError}
	}
	payload := map[string]interface{}{
		"ok":                      true,
		"non_executing":           true,
		"boundary":                "read-only skill maintenance plan; no skill was modified, archived, merged, deleted, installed, or executed",
		"maintenance_plan_status": "local_skill_maintenance_plan_no_llm",
		"plan":                    plan,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return agent.ToolExecutionResult{Result: fmt.Sprintf("Error: marshal maintenance plan failed: %v", err), Outcome: agent.ToolExecutionOutcomeError}
	}
	return agent.ToolExecutionResult{Result: string(data), Outcome: agent.ToolExecutionOutcomeOK}
}

func skillFloatArg(args map[string]interface{}, key string) float64 {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case float32:
			return float64(n)
		case int:
			return float64(n)
		case json.Number:
			if f, err := n.Float64(); err == nil {
				return f
			}
		}
	}
	return 0
}

// manageSkillToolDef is a control-plane compatibility definition. It is not
// returned from BuildTools: installations, discovery and lifecycle management
// must be reached through authenticated service APIs, not model tool calls.
func (c *coreAgentCallbacks) manageSkillToolDef() map[string]interface{} {
	return functionToolDefinition("manage_skill",
		"Control-plane skill management compatibility endpoint. It is not an Agent task-execution tool.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action":                 map[string]interface{}{"type": "string", "description": "Action: " + skill.ManageSkillActionSlash()},
				"query":                  map[string]interface{}{"type": "string", "description": "Search keyword (required for search)"},
				"source":                 map[string]interface{}{"type": "string", "description": "Skill install source: skillmarket, skillhub, clawhub, github, github_repo, or zip"},
				"skill_id":               map[string]interface{}{"type": "string", "description": "Skill ID from search results (for install)"},
				"hub_url":                map[string]interface{}{"type": "string", "description": "SkillHub URL (for install/search when using skillhub)"},
				"skill_hub_url":          map[string]interface{}{"type": "string", "description": "SkillHub URL (for install/search when using skillhub)"},
				"skill_market_url":       map[string]interface{}{"type": "string", "description": "SkillMarket/HubCenter URL (for install/search when using skillmarket)"},
				"repo_url":               map[string]interface{}{"type": "string", "description": "GitHub repository URL (for github_repo install)"},
				"raw_url":                map[string]interface{}{"type": "string", "description": "Raw GitHub skill file URL (for github install)"},
				"install_ref":            map[string]interface{}{"type": "string", "description": "Install reference from search results, such as a GitHub raw URL or repo URL"},
				"repo_full_name":         map[string]interface{}{"type": "string", "description": "GitHub owner/repo from search results (for github install)"},
				"file_path":              map[string]interface{}{"type": "string", "description": "Skill file path in repo (for github install)"},
				"branch":                 map[string]interface{}{"type": "string", "description": "Git branch (for github install)"},
				"definition_type":        map[string]interface{}{"type": "string", "description": "Skill definition type from search results (for github install)"},
				"zip_base64":             map[string]interface{}{"type": "string", "description": "Base64-encoded skill zip archive (for zip install)"},
				"overwrite":              map[string]interface{}{"type": "boolean", "description": "Overwrite an installed skill with the same name"},
				"name":                   map[string]interface{}{"type": "string", "description": "Skill name (required for run)"},
				"args":                   map[string]interface{}{"type": "object", "description": "Skill arguments (for run). Template variables like {{key}} in skill commands are replaced with values from this object."},
				"input":                  map[string]interface{}{"type": "string", "description": "Input parameter (for run, shorthand for args.input)"},
				"output":                 map[string]interface{}{"type": "string", "description": "Output parameter (for run, shorthand for args.output)"},
				"max_actions":            map[string]interface{}{"type": "integer", "description": "Maximum number of maintenance actions returned by maintenance_plan"},
				"stale_after_days":       map[string]interface{}{"type": "integer", "description": "Days before an unused learned skill is considered stale for maintenance_plan"},
				"min_failure_runs":       map[string]interface{}{"type": "integer", "description": "Minimum failed runs before maintenance_plan recommends review or repair"},
				"duplicate_similarity":   map[string]interface{}{"type": "number", "description": "Name/description similarity threshold for duplicate skill recommendations"},
				"dry_run":                map[string]interface{}{"type": "boolean", "description": "execute_maintenance_plan preview mode; defaults true"},
				"confirm":                map[string]interface{}{"type": "boolean", "description": "Required true when execute_maintenance_plan uses dry_run=false"},
				"approved_actions":       map[string]interface{}{"type": "array", "description": "Approved maintenance action names for execute_maintenance_plan"},
				"allow_duplicate_retire": map[string]interface{}{"type": "boolean", "description": "Allow execute_maintenance_plan to disable the recommended duplicate skill after merge draft review"},
			},
			"required": []string{"action"},
		})
}

// skillToolDefs materializes active skills into opaque, bound task adapters.
// The generic manage_skill control-plane gateway is intentionally omitted.
func (c *coreAgentCallbacks) skillToolDefs() []map[string]interface{} {
	if c.skillProvider == nil {
		return nil
	}
	entries := c.skillProvider.ListSkills(c.parentContext(), c.principal)
	if len(entries) == 0 {
		c.boundSkillCalls().replace(nil)
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].StableID != entries[j].StableID {
			return entries[i].StableID < entries[j].StableID
		}
		return entries[i].Name < entries[j].Name
	})
	adapters := make(map[string]boundSkillAdapter, len(entries))
	defs := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		if err := entry.Contract.validate(); err != nil {
			continue
		}
		binding, err := BindSkill(entries, entry.StableID, entry.Name)
		if err != nil {
			continue
		}
		adapterName, err := newSkillAdapterName(adapters)
		if err != nil {
			continue
		}
		parameters := skillInvocationSchema(entry.Params)
		defs = append(defs, functionToolDefinition(adapterName, "Perform the approved skill capability.", parameters))
		adapters[adapterName] = boundSkillAdapter{Binding: binding, Parameters: parameters}
	}
	c.boundSkillCalls().replace(adapters)
	return defs
}

func (c *coreAgentCallbacks) boundSkillCalls() *boundSkillCallSurface {
	c.skillSurfaceMu.Lock()
	defer c.skillSurfaceMu.Unlock()
	if c.skillSurface == nil {
		c.skillSurface = newBoundSkillCallSurface()
	}
	return c.skillSurface
}

func newSkillAdapterName(existing map[string]boundSkillAdapter) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		buf := make([]byte, 12)
		if _, err := cryptorand.Read(buf); err != nil {
			return "", fmt.Errorf("generate skill adapter identity: %w", err)
		}
		name := "invoke_skill_" + base64.RawURLEncoding.EncodeToString(buf)
		if _, exists := existing[name]; !exists {
			return name, nil
		}
	}
	return "", fmt.Errorf("generate unique skill adapter identity")
}

func skillInvocationSchema(params []corelib.NLSkillParam) map[string]interface{} {
	properties := make(map[string]interface{}, len(params))
	required := make([]string, 0, len(params))
	for _, param := range params {
		name := strings.TrimSpace(param.Name)
		if name == "" || isReservedSkillInvocationField(name) {
			continue
		}
		kind := strings.TrimSpace(param.Type)
		switch kind {
		case "string", "number", "integer", "boolean", "array", "object":
		default:
			kind = "string"
		}
		properties[name] = map[string]interface{}{"type": kind}
		if param.Required {
			required = append(required, name)
		}
	}
	return map[string]interface{}{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func isReservedSkillInvocationField(field string) bool {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "name", "skill", "skill_id", "provider", "provider_id", "selection_id", "action", "credential", "credentials", "artifact_id", "artifact_ref":
		return true
	default:
		return false
	}
}

func (c *coreAgentCallbacks) executeBoundSkillTool(adapterName string, args map[string]interface{}) (string, bool) {
	adapter, ok := c.boundSkillCalls().adapter(adapterName)
	if !ok {
		return "", false
	}
	adapterRecord, execute, err := c.admitDynamicAdapterInvocation("skill", adapterName, adapter.Binding.BindingID())
	if err != nil {
		return "Error: " + err.Error(), true
	}
	if !execute {
		return dynamicOperationReplayResult(adapterRecord), true
	}
	if _, err := c.completeDynamicOperation(adapterRecord, DynamicOperationSucceeded, ""); err != nil {
		return "Error: " + err.Error(), true
	}
	if strings.TrimSpace(adapter.Binding.StableID) == "" || strings.TrimSpace(adapter.Binding.Name) == "" || strings.TrimSpace(adapter.Binding.ContentDigest) == "" {
		return "Error: skill_binding_stale", true
	}
	if err := validateMCPInvocationArguments(adapter.Parameters, args); err != nil {
		return "Error: " + err.Error(), true
	}
	record, execute, err := c.admitDynamicOperation("skill", adapter.Binding.BindingID(), args)
	if err != nil {
		return "Error: " + err.Error(), true
	}
	if !execute {
		return dynamicOperationReplayResult(record), true
	}
	bound, ok := c.skillProvider.(boundSkillToolCaller)
	if !ok {
		_, _ = c.completeDynamicOperation(record, DynamicOperationFailed, "skill_bound_execution_unavailable")
		return "Error: bound skill execution is unavailable", true
	}
	result, err := bound.CallBoundSkill(c.parentContext(), c.principal, adapter.Binding, args)
	if err != nil {
		_, _ = c.completeDynamicOperation(record, DynamicOperationUnknown, "skill_execution_unknown")
		return fmt.Sprintf("Error: %v", err), true
	}
	_, _ = c.completeDynamicOperation(record, DynamicOperationSucceeded, "")
	return result, true
}

type boundSkillToolCaller interface {
	CallBoundSkill(ctx context.Context, p Principal, binding SkillBinding, arguments map[string]interface{}) (string, error)
}
