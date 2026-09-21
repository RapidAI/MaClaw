// Package taskeval is the offline task-success evaluator for the
// tool-routing improvement plan (docs/design/tool-routing-improvement-plan-zh.md
// §6 Phase 0 item 2, docs/design/tool-routing-phase0-baseline-zh.md §3). It is
// the LLM-in-the-loop half of the Phase-0 runtime evaluation capability, with
// the LLM replaced by recorded evidence: each transcript turn carries the
// user text and the tool calls the model actually made in a recorded session.
// For every turn the harness runs the SAME surface-selection pipeline as
// corelib/tool/surfaceeval (corelib/tool.Router.RouteWithOptions, with the
// deterministic UIC cache stub when the turn declares a simulated_intent)
// and grades whether the rendered surface contained every tool the model
// actually called.
//
// This harness makes NO LLM calls and NO network calls: no embedder, no
// reranker, no skill provider, no recommender, no live Unified Intent
// Classifier and — unlike the eventual live mode — no model of its own. The
// model's behavior is frozen into the transcript's called_tools.
//
// Provider seam: grading consumes a TurnSelector, the interface a surface
// provider must satisfy. The offline implementation is RouterReplaySelector,
// which replays the legacy router headlessly. The planned live
// implementation (NOT implemented here) is a LiveTurnSelector that presents
// the rendered surface of each turn to a real model and records which tools
// that model calls; grading then compares those calls against the same
// surface, so every metric below applies unchanged to live mode. A live
// selector is expected to carry its own model credentials and make network
// calls; nothing in this package does.
//
// Grading semantics per turn:
//
//	eligible      the turn declares at least one called tool that exists in
//	              the catalog. Turns with no catalog-known calls (chitchat,
//	              cost-only turns) run for surface-cost measurement only and
//	              are excluded from the adequacy denominator — the same
//	              recall-eligible convention surfaceeval uses for empty
//	              expect_tools.
//	adequate      an eligible turn whose every catalog-known called tool is
//	              present in the rendered surface.
//	model error   a called tool that does NOT exist in the catalog at all is
//	              a model error (the model hallucinated a tool name): it is
//	              counted in ModelErrorCalls and excluded from the adequacy
//	              denominator, so a hallucinated call can never be "rescued"
//	              by the surface and never dilutes adequacy. Dataset
//	              validation rejects such names up front (called_tools must
//	              name catalog tools, the same rule surfaceeval applies to
//	              expect_tools); the grading path still handles them so that
//	              Run on a hand-built, unvalidated Transcript degrades
//	              gracefully instead of mis-measuring.
//
// Metrics per transcript:
//
//	surface_adequacy   adequate_turns / eligible_turns; 0 when the transcript
//	                   has no eligible turn (pure cost transcript).
//	surface size /     per turn, exactly the surfaceeval definitions
//	est tokens
//	baseline_* /       the same cost metrics computed over the FULL catalog
//	token_savings      surface (no routing).
//
// Transcript format (one JSON file per transcript, see data/):
//
//	{
//	  "version": 1,                          // schema version, must be 1
//	  "tools": [                             // optional; built-in default
//	    {"name": "ssh", "description": "...", "tags": ["remote"]}
//	  ],
//	  "transcript": [
//	    {
//	      "user_text": "帮我连上服务器 10.0.0.1",   // the recorded turn
//	      "called_tools": ["ssh"],             // tools the model actually
//	                                             // called; must name catalog
//	                                             // tools; empty = cost-only
//	      "simulated_intent": {                // optional; same semantics as
//	        "label": "ssh",                    // surfaceeval: a UIC cache
//	        "confidence": 0.9,                 // stub returns this for the
//	        "tool_names": ["ssh"]              // turn, activating conditional
//	      }                                    // tools fail-closed otherwise
//	    }
//	  ]
//	}
//
// Transcript capture (export.go): two durable sources convert into
// datasets, both with an ExportStats report of omitted unknown tool calls:
//
//   - Source 1: the corelib/agent ConversationMemory snapshot file (TUI
//     ~/.maclaw/data/tui_conversation.json and the other hosts sharing that
//     format), via ExportFromConversationStore / ExportFile.
//   - Source 2: the guiapp IM session search store (SQLite FTS5
//     <dataDir>/session_search.db, written per IM turn by
//     persistSessionTranscriptAsync), via guiapp.ExportFromIMSessionStore /
//     guiapp.ExportIMFile. That converter lives in the guiapp package —
//     guiapp imports corelib/tool, so the reverse import would cycle — and
//     reuses the public Dataset/Turn/ExportStats types plus Validate below.
package taskeval

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/tool/surfaceeval"
)

// DatasetVersion is the only supported transcript schema version.
const DatasetVersion = 1

// Dataset is the top-level document of one transcript file. When Tools is
// empty, surfaceeval.DefaultCatalog is used — the same catalog the seed
// baseline was measured against.
type Dataset struct {
	Version    int                       `json:"version"`
	Tools      []surfaceeval.CatalogTool `json:"tools,omitempty"`
	Transcript []Turn                    `json:"transcript"`
}

// Turn is one recorded transcript turn: the user text and the tool calls
// the model actually made in response.
type Turn struct {
	UserText    string   `json:"user_text"`
	CalledTools []string `json:"called_tools"`
	// SimulatedIntent, when non-nil, attaches the deterministic UIC cache
	// stub for this turn (see the package doc). Absent means no UIC:
	// conditional tools stay fail-closed.
	SimulatedIntent *surfaceeval.SimulatedIntent `json:"simulated_intent,omitempty"`
}

// TurnSelector is the provider seam: it answers, for one turn, which tool
// names the surface rendered for that turn contains. The offline
// implementation is RouterReplaySelector. The planned live implementation
// (see the package doc) would ask a real model given the rendered surface
// and return the tools the model calls; grading consumes only this
// interface, so no grading code changes when a live selector arrives.
type TurnSelector interface {
	// SurfaceFor returns the sorted names of the tools in the surface
	// selected for turn. Implementations must be safe to reuse across turns.
	SurfaceFor(turn Turn) []string
}

// RouterReplaySelector is the offline TurnSelector: it replays each turn
// through the same corelib/tool.Router.RouteWithOptions pipeline
// corelib/tool/surfaceeval measures, attaching the deterministic UIC cache
// stub when the turn declares a simulated_intent. It makes no LLM and no
// network calls.
type RouterReplaySelector struct {
	router *tool.Router
	defs   []map[string]interface{}
}

// NewRouterReplaySelector builds a headless replay selector over catalog,
// registered through the standard corelib/tool registration path.
func NewRouterReplaySelector(catalog []surfaceeval.CatalogTool) *RouterReplaySelector {
	registry := tool.NewRegistry()
	for _, entry := range catalog {
		_ = registry.Register(tool.RegisteredTool{
			Name:        entry.Name,
			Description: entry.Description,
			Tags:        entry.Tags,
			Category:    tool.CategoryBuiltin,
			Status:      tool.StatusAvailable,
		})
	}
	router := tool.NewRouter(nil)
	router.SetRegistry(registry)
	return &RouterReplaySelector{router: router, defs: buildDefinitions(registry)}
}

// SurfaceFor replays one turn through the legacy router and returns the
// sorted names of the selected surface.
func (s *RouterReplaySelector) SurfaceFor(turn Turn) []string {
	s.router.SetUnifiedClassifier(surfaceeval.BuildStubUIC(turn.UserText, turn.SimulatedIntent))
	selectedDefs := s.router.RouteWithOptions(turn.UserText, s.defs, tool.RouteOptions{})
	selected := make([]string, 0, len(selectedDefs))
	for _, def := range selectedDefs {
		selected = append(selected, tool.ExtractToolName(def))
	}
	sort.Strings(selected)
	return selected
}

// buildDefinitions renders the registry into OpenAI function-calling
// definition maps, the same projection hosts feed into RouteWithOptions.
func buildDefinitions(registry *tool.Registry) []map[string]interface{} {
	tools := registry.ListAvailable()
	defs := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, tool.RegisteredToolToDef(t))
	}
	return defs
}

// TurnResult holds the per-turn grading: which called tools the rendered
// surface covered, which it missed, and the cost of the surface.
type TurnResult struct {
	UserText    string   `json:"user_text"`
	CalledTools []string `json:"called_tools,omitempty"`

	SelectedTools []string `json:"selected_tools"`
	// MissingTools is the catalog-known called_tools minus the selected
	// surface; empty iff the turn is adequate.
	MissingTools []string `json:"missing_tools,omitempty"`
	// UnknownCalledTools is the called tools that do not exist in the
	// catalog at all: model errors, excluded from the adequacy denominator
	// (see the package doc). Empty for any transcript that passed
	// validation.
	UnknownCalledTools []string `json:"unknown_called_tools,omitempty"`

	// Eligible is false when the turn has no catalog-known called tool;
	// such turns are cost-measurement only and excluded from the adequacy
	// denominator.
	Eligible bool `json:"eligible"`
	// Adequate is true when every catalog-known called tool is present in
	// the rendered surface.
	Adequate bool `json:"adequate"`

	SurfaceSize      int `json:"surface_size"`
	EstSurfaceTokens int `json:"est_surface_tokens"`

	// Baseline metrics for the full-catalog surface (no routing).
	BaselineSurfaceSize int `json:"baseline_surface_size"`
	BaselineEstTokens   int `json:"baseline_est_tokens"`
}

// TranscriptResult holds every per-turn result plus the transcript-level
// metrics of one replayed transcript.
type TranscriptResult struct {
	ID string `json:"id"`

	Turns         int `json:"turns"`
	EligibleTurns int `json:"eligible_turns"`
	AdequateTurns int `json:"adequate_turns"`
	// ModelErrorCalls is the total number of called tools that did not
	// exist in the catalog (model hallucinations); see the package doc.
	ModelErrorCalls int `json:"model_error_calls"`

	// SurfaceAdequacy is adequate_turns / eligible_turns; 0 when the
	// transcript has no eligible turn (pure cost transcript), in which case
	// EligibleTurns == 0 flags that the value is not a measurement.
	SurfaceAdequacy float64 `json:"surface_adequacy"`

	MeanSurfaceSize float64 `json:"mean_surface_size"`
	TotalEstTokens  int     `json:"total_est_tokens"`
	// Baseline metrics aggregated over every turn (full-catalog surface).
	TotalBaselineEstTokens int `json:"total_baseline_est_tokens"`
	// TokenSavings is 1 - TotalEstTokens/TotalBaselineEstTokens.
	TokenSavings float64 `json:"token_savings"`

	TurnResults []TurnResult `json:"turn_results"`
}

// InadequateTurns returns the results of all eligible turns that were not
// adequate, i.e. the turns where the model needed a tool the routed surface
// did not render.
func (r *TranscriptResult) InadequateTurns() []TurnResult {
	var out []TurnResult
	for _, res := range r.TurnResults {
		if res.Eligible && !res.Adequate {
			out = append(out, res)
		}
	}
	return out
}

// Aggregate is the cross-transcript rollup produced by AggregateResults.
type Aggregate struct {
	Transcripts int `json:"transcripts"`
	Turns       int `json:"turns"`

	AdequacyEligibleTranscripts int     `json:"adequacy_eligible_transcripts"`
	MeanSurfaceAdequacy         float64 `json:"mean_surface_adequacy"`
	MinSurfaceAdequacy          float64 `json:"min_surface_adequacy"`
	// MinSet is true when at least one transcript had eligible turns, i.e.
	// MinSurfaceAdequacy is a real measurement. When every transcript was
	// pure cost (no eligible turns), MinSurfaceAdequacy stays 0 and MinSet
	// stays false — a bare 0 is then ambiguous between "measured 0" and
	// "not measured".
	MinSet bool `json:"min_set"`

	ModelErrorCalls int `json:"model_error_calls"`

	MeanSurfaceSize        float64 `json:"mean_surface_size"`
	TotalEstTokens         int     `json:"total_est_tokens"`
	TotalBaselineEstTokens int     `json:"total_baseline_est_tokens"`
	TokenSavings           float64 `json:"token_savings"`
}

// AggregateResults folds per-transcript results into cross-transcript
// means. MeanSurfaceAdequacy is the mean over transcripts that have at
// least one eligible turn; pure cost transcripts are excluded, mirroring
// surfaceeval's exclusion of empty-expect samples from recall averages.
func AggregateResults(results []*TranscriptResult) *Aggregate {
	agg := &Aggregate{Transcripts: len(results)}
	if len(results) == 0 {
		return agg
	}
	var adequacySum, surfaceSum float64
	for _, r := range results {
		agg.Turns += r.Turns
		agg.ModelErrorCalls += r.ModelErrorCalls
		surfaceSum += r.MeanSurfaceSize
		agg.TotalEstTokens += r.TotalEstTokens
		agg.TotalBaselineEstTokens += r.TotalBaselineEstTokens
		if r.EligibleTurns == 0 {
			continue
		}
		agg.AdequacyEligibleTranscripts++
		adequacySum += r.SurfaceAdequacy
		if !agg.MinSet || r.SurfaceAdequacy < agg.MinSurfaceAdequacy {
			agg.MinSurfaceAdequacy = r.SurfaceAdequacy
			agg.MinSet = true
		}
	}
	agg.MeanSurfaceSize = surfaceSum / float64(len(results))
	if agg.AdequacyEligibleTranscripts > 0 {
		agg.MeanSurfaceAdequacy = adequacySum / float64(agg.AdequacyEligibleTranscripts)
	}
	if agg.TotalBaselineEstTokens > 0 {
		agg.TokenSavings = 1.0 - float64(agg.TotalEstTokens)/float64(agg.TotalBaselineEstTokens)
	}
	return agg
}

// RunFile loads a transcript from path, validates it strictly and replays
// it with the offline RouterReplaySelector. Any parse or validation problem
// is returned as an error; a successfully returned TranscriptResult always
// covers every turn.
func RunFile(path string) (*TranscriptResult, error) {
	return RunFileWithSelector(path, nil)
}

// RunFileWithSelector is RunFile with an explicit TurnSelector; nil selects
// the offline RouterReplaySelector. This is the entry point a future live
// mode would call with a LiveTurnSelector.
func RunFileWithSelector(path string, sel TurnSelector) (*TranscriptResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("taskeval: read transcript %q: %w", path, err)
	}
	var ds Dataset
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, fmt.Errorf("taskeval: parse transcript %q: %w", path, err)
	}
	if err := validate(&ds); err != nil {
		return nil, fmt.Errorf("taskeval: invalid transcript %q: %w", path, err)
	}
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return RunWithSelector(&ds, sel, id), nil
}

// Run replays every turn of ds with the offline RouterReplaySelector.
// ds must have been validated (RunFile does this).
func Run(ds *Dataset) *TranscriptResult {
	return RunWithSelector(ds, nil, "")
}

// RunWithSelector replays every turn of ds through sel (nil = offline
// RouterReplaySelector) and returns the transcript-level result.
//
// RunWithSelector is an offline measurement when sel is nil or any other
// deterministic selector: it disables corelib/tool's global routing
// telemetry for its duration (restored on return) so replayed turns never
// mutate the live counters or schedule durable writes against the real
// routing.json.
func RunWithSelector(ds *Dataset, sel TurnSelector, id string) *TranscriptResult {
	prev := tool.RecordRoutingStats
	tool.RecordRoutingStats = false
	defer func() { tool.RecordRoutingStats = prev }()

	catalog := ds.Tools
	if len(catalog) == 0 {
		catalog = surfaceeval.DefaultCatalog()
	}
	if sel == nil {
		sel = NewRouterReplaySelector(catalog)
	}
	catalogNames := make(map[string]bool, len(catalog))
	byName := make(map[string]surfaceeval.CatalogTool, len(catalog))
	for _, t := range catalog {
		catalogNames[t.Name] = true
		byName[t.Name] = t
	}
	baselineSize, baselineTokens := surfaceeval.SurfaceCost(catalog)

	res := &TranscriptResult{ID: id, Turns: len(ds.Transcript)}
	res.TurnResults = make([]TurnResult, 0, len(ds.Transcript))
	var surfaceSum float64
	for _, turn := range ds.Transcript {
		tr := gradeTurn(turn, sel.SurfaceFor(turn), catalogNames, byName, baselineSize, baselineTokens)
		if tr.Eligible {
			res.EligibleTurns++
			if tr.Adequate {
				res.AdequateTurns++
			}
		}
		res.ModelErrorCalls += len(tr.UnknownCalledTools)
		surfaceSum += float64(tr.SurfaceSize)
		res.TotalEstTokens += tr.EstSurfaceTokens
		res.TotalBaselineEstTokens += tr.BaselineEstTokens
		res.TurnResults = append(res.TurnResults, tr)
	}
	if res.EligibleTurns > 0 {
		res.SurfaceAdequacy = float64(res.AdequateTurns) / float64(res.EligibleTurns)
	}
	if res.Turns > 0 {
		res.MeanSurfaceSize = surfaceSum / float64(res.Turns)
	}
	if res.TotalBaselineEstTokens > 0 {
		res.TokenSavings = 1.0 - float64(res.TotalEstTokens)/float64(res.TotalBaselineEstTokens)
	}
	return res
}

// gradeTurn scores one turn: which catalog-known called tools the selected
// surface covered, which called tools are unknown to the catalog (model
// errors), and the cost of the surface. See the package doc for the
// semantics.
func gradeTurn(turn Turn, selected []string, catalogNames map[string]bool, byName map[string]surfaceeval.CatalogTool, baselineSize, baselineTokens int) TurnResult {
	selectedSet := make(map[string]bool, len(selected))
	for _, name := range selected {
		selectedSet[name] = true
	}
	var missing, unknown []string
	knownCalls := 0
	for _, called := range turn.CalledTools {
		if !catalogNames[called] {
			unknown = append(unknown, called)
			continue
		}
		knownCalls++
		if !selectedSet[called] {
			missing = append(missing, called)
		}
	}
	eligible := knownCalls > 0

	selectedCatalog := make([]surfaceeval.CatalogTool, 0, len(selected))
	for _, name := range selected {
		if t, ok := byName[name]; ok {
			selectedCatalog = append(selectedCatalog, t)
		}
	}
	_, estTokens := surfaceeval.SurfaceCost(selectedCatalog)

	return TurnResult{
		UserText:            turn.UserText,
		CalledTools:         turn.CalledTools,
		SelectedTools:       selected,
		MissingTools:        missing,
		UnknownCalledTools:  unknown,
		Eligible:            eligible,
		Adequate:            eligible && len(missing) == 0,
		SurfaceSize:         len(selected),
		EstSurfaceTokens:    estTokens,
		BaselineSurfaceSize: baselineSize,
		BaselineEstTokens:   baselineTokens,
	}
}

// Validate reports whether a Dataset would pass the same checks RunFile
// applies before replay: schema version, non-empty turns, non-empty user
// text, called_tools and simulated_intent naming catalog tools. Exporters
// outside this package (e.g. the guiapp IM-session converter, which cannot
// import unexported members) call Validate to guarantee validate-before-write
// semantics identical to ExportFile.
func Validate(ds *Dataset) error {
	return validate(ds)
}

// validate rejects transcripts that would produce misleading results: wrong
// schema version, an empty transcript, empty user text, called_tools names
// that do not exist in the effective catalog, or a malformed
// simulated_intent (unknown label, confidence outside [0,1], or tool_names
// that do not exist in the effective catalog). Validation is strict about
// called_tools naming catalog tools so that model hallucinations are caught
// at dataset authoring time; the grading path still tolerates them (as
// model errors) for hand-built transcripts that skip validation.
func validate(ds *Dataset) error {
	if ds == nil {
		return fmt.Errorf("transcript is nil")
	}
	if ds.Version != DatasetVersion {
		return fmt.Errorf("unsupported version %d (want %d)", ds.Version, DatasetVersion)
	}
	if len(ds.Transcript) == 0 {
		return fmt.Errorf("transcript has no turns")
	}
	catalog := ds.Tools
	if len(catalog) == 0 {
		catalog = surfaceeval.DefaultCatalog()
	}
	names := make(map[string]bool, len(catalog))
	for _, t := range catalog {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			return fmt.Errorf("catalog entry with empty name")
		}
		if names[name] {
			return fmt.Errorf("duplicate catalog tool %q", name)
		}
		names[name] = true
	}
	for i, turn := range ds.Transcript {
		if strings.TrimSpace(turn.UserText) == "" {
			return fmt.Errorf("turn %d has empty user_text", i)
		}
		for _, called := range turn.CalledTools {
			if !names[called] {
				return fmt.Errorf("turn %d calls unknown tool %q (not in catalog)", i, called)
			}
		}
		if sim := turn.SimulatedIntent; sim != nil {
			if !knownIntentLabels[sim.Label] {
				return fmt.Errorf("turn %d simulated_intent has unknown label %q", i, sim.Label)
			}
			if math.IsNaN(sim.Confidence) || sim.Confidence < 0 || sim.Confidence > 1 {
				return fmt.Errorf("turn %d simulated_intent confidence %.3f outside [0,1]", i, sim.Confidence)
			}
			for _, toolName := range sim.ToolNames {
				if !names[toolName] {
					return fmt.Errorf("turn %d simulated_intent names unknown tool %q (not in catalog)", i, toolName)
				}
			}
		}
	}
	return nil
}

// knownIntentLabels is the set of valid simulated_intent labels: every label
// the intent package declares (corelib/intent.AllLabels).
var knownIntentLabels = func() map[string]bool {
	labels := intent.AllLabels()
	m := make(map[string]bool, len(labels))
	for _, l := range labels {
		m[string(l)] = true
	}
	return m
}()
