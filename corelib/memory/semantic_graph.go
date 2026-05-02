package memory

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// SemanticGraph is a derived Entity/Fact/Memory graph built from Entry.Entities.
// It is intentionally separate from memoryGraph: memoryGraph models entry-to-entry
// relatedness, while SemanticGraph models typed facts grounded by entries.
type SemanticGraph struct {
	mu          sync.RWMutex
	entities    map[string]map[string]struct{} // entity -> entry IDs mentioning it
	facts       []SemanticFact
	adjacency   map[string][]int // entity -> fact indices touching that entity
	byEntry     map[string][]SemanticFact
	rawEntities map[string][]string
	entryMeta   map[string]semanticEntryMeta
	aliases     map[string]map[string]struct{} // normalized entity -> equivalent entities
}

// SemanticFact is a subject-predicate-object edge grounded by one memory entry.
type SemanticFact struct {
	Subject    string
	Predicate  string
	Object     string
	EntryID    string
	Content    string
	Negated    bool
	Scope      Scope
	Tags       []string
	ValidAt    *time.Time
	InvalidAt  *time.Time
	Status     Status
	OwnerID    string
	Category   Category
	SourceType string
	Pinned     bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SemanticSearchHit describes why a memory was found through the semantic graph.
type SemanticSearchHit struct {
	EntryID string
	Score   float64
	Paths   []string
}

// SemanticSearchOptions carries relation-aware query intent into graph search.
type SemanticSearchOptions struct {
	Now             time.Time
	AsOf            *time.Time
	OwnerID         string
	ProjectPath     string
	RelationHints   []string
	SeedWeights     map[string]float64
	MaxHops         int
	MaxHits         int
	MaxVisitedFacts int
	TemporalMode    SemanticTemporalMode
}

// SemanticGraphDiagnostics reports graph quality signals useful for extractor tuning.
type SemanticGraphDiagnostics struct {
	EntityCount            int
	FactCount              int
	AdjacencyKeys          int
	RelationCounts         map[string]int
	UnknownRelations       []string
	UnknownRelationDetails []SemanticGraphRelationIssue
	MalformedTripleCount   int
	MalformedTriples       []SemanticGraphMalformedTriple
	HighDegreeEntities     []SemanticGraphEntityDegree
	AliasComponents        [][]string
	DominanceConflicts     []SemanticGraphConflict
}

type SemanticRelationSchemaItem struct {
	Name           string
	Weight         float64
	Functional     bool
	AllowExpansion bool
	AllowReverse   bool
	ReverseFactor  float64
}

type SemanticGraphRelationIssue struct {
	Relation string
	Count    int
	EntryIDs []string
}

type SemanticGraphMalformedTriple struct {
	EntryID string
	Offset  int
	Reason  string
	Items   []string
}

type SemanticGraphEntityDegree struct {
	Entity string
	Degree int
}

type SemanticGraphConflict struct {
	Subject   string
	Predicate string
	Objects   []string
	EntryIDs  []string
}

// SemanticTemporalMode controls whether search answers current-state or history queries.
type SemanticTemporalMode int

const (
	SemanticTemporalCurrent SemanticTemporalMode = iota
	SemanticTemporalHistorical
	SemanticTemporalAsOf
)

type semanticRelationSpec struct {
	Weight         float64
	Functional     bool
	AllowExpansion bool
	AllowReverse   bool
	ReverseFactor  float64
}

var semanticRelationSchema = map[string]semanticRelationSpec{
	"about":          {Weight: 2.0, AllowExpansion: true, ReverseFactor: 0.65},
	"alias_of":       {Weight: 3.0, AllowExpansion: true, AllowReverse: true, ReverseFactor: 1.0},
	"same_as":        {Weight: 3.0, AllowExpansion: true, AllowReverse: true, ReverseFactor: 1.0},
	"config_of":      {Weight: 2.6, Functional: true, AllowExpansion: true, ReverseFactor: 0.45},
	"credential_for": {Weight: 2.6, Functional: true, AllowExpansion: true, ReverseFactor: 0.45},
	"preference_for": {Weight: 2.5, AllowExpansion: true, ReverseFactor: 0.45},
	"located_in":     {Weight: 2.4, AllowExpansion: true, ReverseFactor: 0.45},
	"works_at":       {Weight: 2.4, AllowExpansion: true, ReverseFactor: 0.45},
	"belongs_to":     {Weight: 2.6, Functional: true, AllowExpansion: true, AllowReverse: true, ReverseFactor: 0.85},
	"part_of":        {Weight: 2.6, Functional: true, AllowExpansion: true, AllowReverse: true, ReverseFactor: 0.85},
	"depends_on":     {Weight: 2.4, AllowExpansion: true, ReverseFactor: 0.45},
	"caused_by":      {Weight: 2.4, AllowExpansion: true, ReverseFactor: 0.45},
	"blocks":         {Weight: 2.4, AllowExpansion: true, ReverseFactor: 0.45},
	"references":     {Weight: 2.0, AllowExpansion: true, ReverseFactor: 0.65},
	"derived_from":   {Weight: 2.0, AllowExpansion: true, ReverseFactor: 0.65},
	"supersedes":     {Weight: 1.2, ReverseFactor: 0.45},
	"contradicts":    {Weight: 1.2, ReverseFactor: 0.45},
	"conflicts":      {Weight: 1.2, ReverseFactor: 0.45},
}

type semanticEntryMeta struct {
	Status     Status
	OwnerID    string
	Category   Category
	Scope      Scope
	Tags       []string
	SourceType string
	Pinned     bool
	ValidAt    *time.Time
	InvalidAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewSemanticGraph creates an empty semantic graph.
func NewSemanticGraph() *SemanticGraph {
	return &SemanticGraph{
		entities:    make(map[string]map[string]struct{}),
		adjacency:   make(map[string][]int),
		byEntry:     make(map[string][]SemanticFact),
		rawEntities: make(map[string][]string),
		entryMeta:   make(map[string]semanticEntryMeta),
		aliases:     make(map[string]map[string]struct{}),
	}
}

// Rebuild reconstructs the graph from entries.
func (g *SemanticGraph) Rebuild(entries []Entry) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.entities = make(map[string]map[string]struct{}, len(entries))
	g.facts = nil
	g.adjacency = make(map[string][]int, len(entries))
	g.byEntry = make(map[string][]SemanticFact, len(entries))
	g.rawEntities = make(map[string][]string, len(entries))
	g.entryMeta = make(map[string]semanticEntryMeta, len(entries))
	g.aliases = make(map[string]map[string]struct{})
	for i := range entries {
		g.indexEntryLocked(&entries[i])
	}
	g.rebuildAliasesLocked()
}

// IndexEntry updates one entry's semantic nodes and facts.
func (g *SemanticGraph) IndexEntry(e *Entry) {
	if e == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.removeEntryLocked(e.ID)
	g.indexEntryLocked(e)
	g.rebuildAliasesLocked()
}

// RemoveEntry removes all semantic facts grounded by entryID.
func (g *SemanticGraph) RemoveEntry(entryID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.removeEntryLocked(entryID)
	g.rebuildAliasesLocked()
}

// Stats returns basic graph index sizes for diagnostics and tests.
func (g *SemanticGraph) Stats() (entities int, facts int, adjacencyKeys int) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.entities), len(g.facts), len(g.adjacency)
}

// SemanticRelationSchema returns a stable snapshot of canonical relation definitions.
func SemanticRelationSchema() []SemanticRelationSchemaItem {
	names := make([]string, 0, len(semanticRelationSchema))
	for name := range semanticRelationSchema {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]SemanticRelationSchemaItem, 0, len(names))
	for _, name := range names {
		spec := semanticRelationSchema[name]
		items = append(items, SemanticRelationSchemaItem{
			Name:           name,
			Weight:         spec.Weight,
			Functional:     spec.Functional,
			AllowExpansion: spec.AllowExpansion,
			AllowReverse:   spec.AllowReverse,
			ReverseFactor:  spec.ReverseFactor,
		})
	}
	return items
}

func semanticRelationSchemaPrompt() string {
	items := SemanticRelationSchema()
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return strings.Join(names, ", ")
}

// Diagnostics returns structured quality signals from the visible semantic subgraph.
func (g *SemanticGraph) Diagnostics(opts SemanticSearchOptions) SemanticGraphDiagnostics {
	g.mu.RLock()
	defer g.mu.RUnlock()
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	asOf := now
	if opts.AsOf != nil {
		asOf = *opts.AsOf
	}
	projectLower := strings.ToLower(opts.ProjectPath)
	out := SemanticGraphDiagnostics{
		RelationCounts: make(map[string]int),
	}
	for entryID, raw := range g.rawEntities {
		meta, ok := g.entryMeta[entryID]
		if !ok || !semanticEntryAllowed(meta, asOf, opts.TemporalMode) || !semanticEntryProjectAllowed(meta, projectLower) {
			continue
		}
		if opts.OwnerID != "" && meta.OwnerID != "" && meta.OwnerID != opts.OwnerID {
			continue
		}
		out.MalformedTriples = append(out.MalformedTriples, semanticMalformedTriplesForEntry(entryID, raw)...)
	}
	sort.SliceStable(out.MalformedTriples, func(i, j int) bool {
		if out.MalformedTriples[i].EntryID != out.MalformedTriples[j].EntryID {
			return out.MalformedTriples[i].EntryID < out.MalformedTriples[j].EntryID
		}
		return out.MalformedTriples[i].Offset < out.MalformedTriples[j].Offset
	})
	out.MalformedTripleCount = len(out.MalformedTriples)
	visibleEntities := make(map[string]struct{})
	degree := make(map[string]int)
	unknown := make(map[string]map[string]struct{})
	conflicts := make(map[string]map[string]map[string]struct{})
	conflictEntries := make(map[string]map[string]struct{})
	aliasParent := make(map[string]string)
	var find func(string) string
	find = func(x string) string {
		if aliasParent[x] == "" {
			aliasParent[x] = x
		}
		if aliasParent[x] != x {
			aliasParent[x] = find(aliasParent[x])
		}
		return aliasParent[x]
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			aliasParent[rb] = ra
		}
	}
	for _, fact := range g.facts {
		if !semanticFactVisible(fact, opts.OwnerID, projectLower, asOf, opts.TemporalMode) {
			continue
		}
		out.FactCount++
		out.RelationCounts[fact.Predicate]++
		visibleEntities[fact.Subject] = struct{}{}
		visibleEntities[fact.Object] = struct{}{}
		degree[fact.Subject]++
		if fact.Object != fact.Subject {
			degree[fact.Object]++
		}
		if !semanticKnownRelation(fact.Predicate) {
			if unknown[fact.Predicate] == nil {
				unknown[fact.Predicate] = make(map[string]struct{})
			}
			unknown[fact.Predicate][fact.EntryID] = struct{}{}
		}
		if fact.Predicate == "alias_of" || fact.Predicate == "same_as" {
			union(fact.Subject, fact.Object)
		}
		if semanticIsDominanceRelation(fact.Predicate) && !fact.Negated {
			key := fact.Subject + "\x00" + fact.Predicate
			if conflicts[key] == nil {
				conflicts[key] = make(map[string]map[string]struct{})
			}
			if conflicts[key][fact.Object] == nil {
				conflicts[key][fact.Object] = make(map[string]struct{})
			}
			conflicts[key][fact.Object][fact.EntryID] = struct{}{}
			if conflictEntries[key] == nil {
				conflictEntries[key] = make(map[string]struct{})
			}
			conflictEntries[key][fact.EntryID] = struct{}{}
		}
	}
	out.EntityCount = len(visibleEntities)
	out.AdjacencyKeys = len(degree)
	for rel, entryIDs := range unknown {
		out.UnknownRelations = append(out.UnknownRelations, rel)
		issue := SemanticGraphRelationIssue{Relation: rel, Count: len(entryIDs)}
		for entryID := range entryIDs {
			issue.EntryIDs = append(issue.EntryIDs, entryID)
		}
		sort.Strings(issue.EntryIDs)
		out.UnknownRelationDetails = append(out.UnknownRelationDetails, issue)
	}
	sort.Strings(out.UnknownRelations)
	sort.SliceStable(out.UnknownRelationDetails, func(i, j int) bool {
		return out.UnknownRelationDetails[i].Relation < out.UnknownRelationDetails[j].Relation
	})
	for entity, n := range degree {
		if n >= 4 {
			out.HighDegreeEntities = append(out.HighDegreeEntities, SemanticGraphEntityDegree{Entity: entity, Degree: n})
		}
	}
	sort.SliceStable(out.HighDegreeEntities, func(i, j int) bool {
		if out.HighDegreeEntities[i].Degree != out.HighDegreeEntities[j].Degree {
			return out.HighDegreeEntities[i].Degree > out.HighDegreeEntities[j].Degree
		}
		return out.HighDegreeEntities[i].Entity < out.HighDegreeEntities[j].Entity
	})
	components := make(map[string][]string)
	for entity := range aliasParent {
		root := find(entity)
		components[root] = append(components[root], entity)
	}
	for _, component := range components {
		if len(component) < 2 {
			continue
		}
		sort.Strings(component)
		out.AliasComponents = append(out.AliasComponents, component)
	}
	sort.SliceStable(out.AliasComponents, func(i, j int) bool { return out.AliasComponents[i][0] < out.AliasComponents[j][0] })
	for key, byObject := range conflicts {
		if len(byObject) < 2 {
			continue
		}
		parts := strings.SplitN(key, "\x00", 2)
		conflict := SemanticGraphConflict{Subject: parts[0], Predicate: parts[1]}
		for object := range byObject {
			conflict.Objects = append(conflict.Objects, object)
		}
		for entryID := range conflictEntries[key] {
			conflict.EntryIDs = append(conflict.EntryIDs, entryID)
		}
		sort.Strings(conflict.Objects)
		sort.Strings(conflict.EntryIDs)
		out.DominanceConflicts = append(out.DominanceConflicts, conflict)
	}
	sort.SliceStable(out.DominanceConflicts, func(i, j int) bool {
		if out.DominanceConflicts[i].Subject != out.DominanceConflicts[j].Subject {
			return out.DominanceConflicts[i].Subject < out.DominanceConflicts[j].Subject
		}
		return out.DominanceConflicts[i].Predicate < out.DominanceConflicts[j].Predicate
	})
	return out
}

// Search returns entries reachable from query entities through typed semantic facts.
func (g *SemanticGraph) Search(queryEntities []string, now time.Time, ownerID string) []SemanticSearchHit {
	return g.SearchWithOptions(queryEntities, SemanticSearchOptions{Now: now, OwnerID: ownerID})
}

// SearchWithOptions returns entries reachable from query entities and relation intent.
func (g *SemanticGraph) SearchWithOptions(queryEntities []string, opts SemanticSearchOptions) []SemanticSearchHit {
	g.mu.RLock()
	defer g.mu.RUnlock()
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	asOf := now
	if opts.AsOf != nil {
		asOf = *opts.AsOf
	}
	maxHops := opts.MaxHops
	if maxHops <= 0 {
		maxHops = 2
	}
	if maxHops > 3 {
		maxHops = 3
	}
	maxHits := opts.MaxHits
	if maxHits <= 0 {
		maxHits = 50
	}
	maxVisitedFacts := opts.MaxVisitedFacts
	if maxVisitedFacts <= 0 {
		maxVisitedFacts = 500
	}
	temporalMode := opts.TemporalMode
	relationHints := normalizeRelationHints(opts.RelationHints)
	projectLower := strings.ToLower(opts.ProjectPath)
	dominance := g.semanticDominanceFactorsLocked(asOf, opts.OwnerID, projectLower, temporalMode)

	seedSet := make(map[string]struct{}, len(queryEntities))
	seedWeights := make(map[string]float64, len(queryEntities))
	for _, ent := range queryEntities {
		if n := normalizeEntityName(ent); n != "" {
			seedSet[n] = struct{}{}
			seedWeights[n] = semanticSeedWeight(n, opts.SeedWeights)
			for _, alias := range g.semanticAliasesForEntityLocked(n, opts.OwnerID, projectLower, asOf, temporalMode) {
				seedSet[alias] = struct{}{}
				seedWeights[alias] = seedWeights[n] * 0.95
			}
		}
	}
	relationOnly := len(seedSet) == 0
	if relationOnly && len(relationHints) == 0 {
		return nil
	}

	hits := make(map[string]*SemanticSearchHit)
	addHit := func(entryID string, score float64, path string) {
		meta, ok := g.entryMeta[entryID]
		if !ok || !semanticEntryAllowed(meta, asOf, temporalMode) || !semanticEntryProjectAllowed(meta, projectLower) {
			return
		}
		if opts.OwnerID != "" && meta.OwnerID != "" && meta.OwnerID != opts.OwnerID {
			return
		}
		h := hits[entryID]
		if h == nil {
			h = &SemanticSearchHit{EntryID: entryID}
			hits[entryID] = h
		}
		h.Score += score * semanticMarginalPathFactor(len(h.Paths))
		if path != "" && len(h.Paths) < 3 {
			h.Paths = append(h.Paths, path)
		}
	}

	if !relationOnly {
		for seed := range seedSet {
			for entryID := range g.entities[seed] {
				if meta, ok := g.entryMeta[entryID]; ok && semanticEntryProjectAllowed(meta, projectLower) {
					addHit(entryID, seedWeights[seed]*semanticTemporalFactor(meta.Status, meta.ValidAt, meta.InvalidAt, asOf, temporalMode), "entity:"+seed)
				}
			}
		}
	}

	visitedEntities := make(map[string]struct{}, len(seedSet))
	frontier := make(map[string]struct{}, len(seedSet))
	frontierWeights := make(map[string]float64, len(seedSet))
	for seed := range seedSet {
		visitedEntities[seed] = struct{}{}
		frontier[seed] = struct{}{}
		frontierWeights[seed] = seedWeights[seed]
	}
	visitedFacts := make(map[int]struct{})
	visitedFactCount := 0

	if relationOnly {
		g.searchRelationOnlyLocked(addHit, relationHints, dominance, opts.OwnerID, projectLower, asOf, temporalMode, maxVisitedFacts)
	}

	for hop := 0; !relationOnly && hop < maxHops && len(frontier) > 0; hop++ {
		next := make(map[string]struct{})
		nextWeights := make(map[string]float64)
		factIndexes := g.factIndexesForFrontierLocked(frontier, frontierWeights, relationHints, opts.OwnerID, projectLower, asOf, temporalMode)
		for _, factIndex := range factIndexes {
			if visitedFactCount >= maxVisitedFacts {
				break
			}
			if _, ok := visitedFacts[factIndex]; ok {
				continue
			}
			visitedFacts[factIndex] = struct{}{}
			visitedFactCount++
			fact := g.facts[factIndex]
			if !semanticFactVisible(fact, opts.OwnerID, projectLower, asOf, temporalMode) {
				continue
			}
			subjFrontier := containsEntity(frontier, fact.Subject)
			objFrontier := containsEntity(frontier, fact.Object)
			if !subjFrontier && !objFrontier {
				continue
			}

			weight := semanticRelationWeight(fact.Predicate) * semanticRelationHintFactor(fact.Predicate, relationHints) * semanticRelationSchemaFactor(fact.Predicate)
			weight *= semanticFrontierWeight(fact, subjFrontier, objFrontier, frontierWeights)
			if fact.Negated {
				weight *= 0.35
			}
			weight *= semanticDirectionFactor(fact.Predicate, subjFrontier, objFrontier)
			weight *= g.semanticDegreeFactorLocked(fact, subjFrontier, objFrontier)
			weight *= semanticProvenanceFactor(fact.SourceType, fact.Pinned)
			weight *= semanticCertaintyFactor(fact.Content)
			weight *= dominance[factIndex]
			weight *= semanticTemporalFactor(fact.Status, fact.ValidAt, fact.InvalidAt, asOf, temporalMode)
			if _, ok := seedSet[fact.Subject]; ok {
				if _, ok2 := seedSet[fact.Object]; ok2 {
					weight += 1.0
				}
			}
			weight *= semanticFactRecencyFactor(fact.ValidAt, fact.CreatedAt, fact.UpdatedAt, asOf)
			weight *= semanticHopDecay(hop)
			addHit(fact.EntryID, weight, semanticPathString(fact, hop, subjFrontier))

			if subjFrontier && semanticAllowsExpansion(fact.Predicate) {
				g.addNextEntityLocked(next, nextWeights, visitedEntities, fact.Object, frontierWeights[fact.Subject]*0.85, opts.OwnerID, projectLower, asOf, temporalMode)
			}
			if objFrontier && semanticAllowsReverseExpansion(fact.Predicate) {
				g.addNextEntityLocked(next, nextWeights, visitedEntities, fact.Subject, frontierWeights[fact.Object]*0.85, opts.OwnerID, projectLower, asOf, temporalMode)
			}
		}
		if visitedFactCount >= maxVisitedFacts {
			break
		}
		frontier = next
		frontierWeights = nextWeights
	}

	result := make([]SemanticSearchHit, 0, len(hits))
	for _, h := range hits {
		result = append(result, *h)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].EntryID < result[j].EntryID
	})
	if len(result) > maxHits {
		result = result[:maxHits]
	}
	return result
}

func (g *SemanticGraph) indexEntryLocked(e *Entry) {
	if e == nil || e.ID == "" {
		return
	}
	g.entryMeta[e.ID] = semanticEntryMeta{
		Status:     e.Status,
		OwnerID:    e.OwnerID,
		Category:   e.Category,
		Scope:      e.Scope,
		Tags:       append([]string(nil), e.Tags...),
		SourceType: e.SourceType,
		Pinned:     e.Pinned,
		ValidAt:    e.ValidAt,
		InvalidAt:  e.InvalidAt,
		CreatedAt:  e.CreatedAt,
		UpdatedAt:  e.UpdatedAt,
	}

	g.rawEntities[e.ID] = append([]string(nil), e.Entities...)

	entities := semanticEntities(e.Entities)
	for _, ent := range entities {
		if g.entities[ent] == nil {
			g.entities[ent] = make(map[string]struct{})
		}
		g.entities[ent][e.ID] = struct{}{}
	}

	facts := semanticFactsFromEntry(e)
	g.byEntry[e.ID] = facts
	for _, fact := range facts {
		factIndex := len(g.facts)
		g.facts = append(g.facts, fact)
		g.adjacency[fact.Subject] = append(g.adjacency[fact.Subject], factIndex)
		if fact.Object != fact.Subject {
			g.adjacency[fact.Object] = append(g.adjacency[fact.Object], factIndex)
		}
	}
}

func (g *SemanticGraph) rebuildAliasesLocked() {
	direct := make(map[string]map[string]struct{})
	for _, fact := range g.facts {
		if fact.Predicate != "alias_of" && fact.Predicate != "same_as" {
			continue
		}
		if direct[fact.Subject] == nil {
			direct[fact.Subject] = make(map[string]struct{})
		}
		if direct[fact.Object] == nil {
			direct[fact.Object] = make(map[string]struct{})
		}
		direct[fact.Subject][fact.Object] = struct{}{}
		direct[fact.Object][fact.Subject] = struct{}{}
	}
	g.aliases = make(map[string]map[string]struct{}, len(direct))
	for entity := range direct {
		component := make(map[string]struct{})
		stack := []string{entity}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if _, seen := component[cur]; seen {
				continue
			}
			component[cur] = struct{}{}
			for next := range direct[cur] {
				stack = append(stack, next)
			}
		}
		delete(component, entity)
		if len(component) > 0 {
			g.aliases[entity] = component
		}
	}
}

func (g *SemanticGraph) semanticAliasesForEntityLocked(entity, ownerID, projectLower string, asOf time.Time, temporalMode SemanticTemporalMode) []string {
	if entity == "" {
		return nil
	}
	seen := map[string]struct{}{entity: {}}
	queue := []string{entity}
	var aliases []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, idx := range g.adjacency[cur] {
			fact := g.facts[idx]
			if fact.Predicate != "alias_of" && fact.Predicate != "same_as" {
				continue
			}
			if !semanticFactVisible(fact, ownerID, projectLower, asOf, temporalMode) {
				continue
			}
			next := ""
			if fact.Subject == cur {
				next = fact.Object
			} else if fact.Object == cur {
				next = fact.Subject
			}
			if next == "" {
				continue
			}
			if _, ok := seen[next]; ok {
				continue
			}
			seen[next] = struct{}{}
			aliases = append(aliases, next)
			queue = append(queue, next)
		}
	}
	return aliases
}
func (g *SemanticGraph) addNextEntityLocked(next map[string]struct{}, nextWeights map[string]float64, visited map[string]struct{}, entity string, weight float64, ownerID, projectLower string, asOf time.Time, temporalMode SemanticTemporalMode) {
	if entity == "" {
		return
	}
	entities := []string{entity}
	entities = append(entities, g.semanticAliasesForEntityLocked(entity, ownerID, projectLower, asOf, temporalMode)...)
	for _, ent := range entities {
		if _, ok := visited[ent]; ok {
			continue
		}
		visited[ent] = struct{}{}
		next[ent] = struct{}{}
		if weight > nextWeights[ent] {
			nextWeights[ent] = weight
		}
	}
}

func (g *SemanticGraph) semanticDegreeFactorLocked(fact SemanticFact, subjFrontier, objFrontier bool) float64 {
	entity := fact.Subject
	if !subjFrontier && objFrontier {
		entity = fact.Object
	}
	degree := len(g.adjacency[entity])
	if degree <= 3 {
		return 1.0
	}
	factor := 1.0 / math.Log2(float64(degree)+1)
	if factor < 0.35 {
		return 0.35
	}
	return factor
}

func (g *SemanticGraph) semanticDominanceFactorsLocked(now time.Time, ownerID, projectLower string, temporalMode SemanticTemporalMode) map[int]float64 {
	factors := make(map[int]float64, len(g.facts))
	groups := make(map[string]map[string][]int)
	for i, fact := range g.facts {
		factors[i] = 1.0
		if !semanticIsDominanceRelation(fact.Predicate) || !semanticFactVisible(fact, ownerID, projectLower, now, temporalMode) {
			continue
		}
		if ownerID != "" && fact.OwnerID != "" && fact.OwnerID != ownerID {
			continue
		}
		key := fact.Subject + "\x00" + fact.Predicate
		if groups[key] == nil {
			groups[key] = make(map[string][]int)
		}
		groups[key][fact.Object] = append(groups[key][fact.Object], i)
	}
	for _, byObject := range groups {
		if len(byObject) < 2 && !semanticHasPolarityCompetition(g.facts, byObject) {
			continue
		}
		bestObject := ""
		bestScore := -1.0
		for object, idxs := range byObject {
			clusterScore := semanticPositiveEvidenceClusterScore(g.facts, idxs, now)
			if clusterScore > bestScore || (clusterScore == bestScore && object < bestObject) {
				bestObject = object
				bestScore = clusterScore
			}
		}
		for object, idxs := range byObject {
			clusterBoost := semanticEvidenceClusterBoost(semanticEvidenceSourceCount(g.facts, idxs))
			polarity := semanticDominantPolarity(g.facts, idxs, now)
			for _, idx := range idxs {
				if object == bestObject && (!g.facts[idx].Negated || polarity == semanticPolarityNegated) {
					factors[idx] = 1.15 * clusterBoost * semanticPolarityFactor(g.facts[idx], polarity)
				} else if object == bestObject {
					factors[idx] = 0.55
				} else if g.facts[idx].Negated && polarity == semanticPolarityNegated {
					factors[idx] = 1.05 * clusterBoost
				} else {
					factors[idx] = 0.45
				}
			}
		}
	}
	return factors
}

func semanticHasPolarityCompetition(facts []SemanticFact, byObject map[string][]int) bool {
	for _, idxs := range byObject {
		hasPositive := false
		hasNegated := false
		for _, idx := range idxs {
			if facts[idx].Negated {
				hasNegated = true
			} else {
				hasPositive = true
			}
			if hasPositive && hasNegated {
				return true
			}
		}
	}
	return false
}

func semanticPositiveEvidenceClusterScore(facts []SemanticFact, idxs []int, now time.Time) float64 {
	best := 0.0
	for _, idx := range idxs {
		if facts[idx].Negated {
			continue
		}
		if score := semanticDominanceScore(facts[idx], now); score > best {
			best = score
		}
	}
	if best == 0 {
		return semanticEvidenceClusterScore(facts, idxs, now) * 0.5
	}
	return best * semanticEvidenceClusterBoost(semanticEvidenceSourceCount(facts, idxs))
}

func semanticEvidenceClusterScore(facts []SemanticFact, idxs []int, now time.Time) float64 {
	best := 0.0
	for _, idx := range idxs {
		if score := semanticDominanceScore(facts[idx], now); score > best {
			best = score
		}
	}
	return best * semanticEvidenceClusterBoost(semanticEvidenceSourceCount(facts, idxs))
}

type semanticPolarity int

const (
	semanticPolarityPositive semanticPolarity = iota
	semanticPolarityNegated
)

func semanticDominantPolarity(facts []SemanticFact, idxs []int, now time.Time) semanticPolarity {
	positiveScore := 0.0
	negatedScore := 0.0
	for _, idx := range idxs {
		score := semanticDominanceScore(facts[idx], now)
		if facts[idx].Negated {
			if score > negatedScore {
				negatedScore = score
			}
		} else if score > positiveScore {
			positiveScore = score
		}
	}
	if negatedScore > positiveScore {
		return semanticPolarityNegated
	}
	return semanticPolarityPositive
}

func semanticPolarityFactor(fact SemanticFact, polarity semanticPolarity) float64 {
	if fact.Negated && polarity == semanticPolarityNegated {
		return 4.0
	}
	if fact.Negated {
		return 0.5
	}
	return 1.0
}

func semanticEvidenceSourceCount(facts []SemanticFact, idxs []int) int {
	if len(idxs) == 0 {
		return 0
	}
	sources := make(map[string]struct{}, len(idxs))
	anonymous := 0
	for _, idx := range idxs {
		if idx < 0 || idx >= len(facts) {
			continue
		}
		entryID := facts[idx].EntryID
		if entryID == "" {
			anonymous++
			continue
		}
		sources[entryID] = struct{}{}
	}
	return len(sources) + anonymous
}

func semanticEvidenceClusterBoost(n int) float64 {
	if n <= 1 {
		return 1.0
	}
	boost := 1.0 + 0.15*math.Log2(float64(n))
	if boost > 1.45 {
		return 1.45
	}
	return boost
}

func semanticKnownRelation(predicate string) bool {
	_, ok := semanticRelationSchema[predicate]
	return ok
}

func semanticIsDominanceRelation(predicate string) bool {
	return semanticRelationSchema[predicate].Functional
}

func semanticDominanceScore(fact SemanticFact, now time.Time) float64 {
	score := semanticProvenanceFactor(fact.SourceType, fact.Pinned) * semanticCertaintyFactor(fact.Content)
	if fact.Negated {
		score *= 1.15
	}
	if fact.ValidAt != nil {
		score += 0.25
	}
	if !now.IsZero() {
		score += semanticFactRecencyFactor(fact.ValidAt, fact.CreatedAt, fact.UpdatedAt, now)
	} else {
		score += 1.0
	}
	return score
}

func semanticIsNegated(content string) bool {
	lower := strings.ToLower(content)
	markers := []string{
		" not ", " no longer ", " never ", " disabled", " removed", "without ", "doesn't", "does not", "isn't", "is not",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (g *SemanticGraph) removeEntryLocked(entryID string) {
	if entryID == "" {
		return
	}
	for ent, ids := range g.entities {
		delete(ids, entryID)
		if len(ids) == 0 {
			delete(g.entities, ent)
		}
	}
	delete(g.byEntry, entryID)
	delete(g.rawEntities, entryID)
	delete(g.entryMeta, entryID)
	kept := g.facts[:0]
	for _, fact := range g.facts {
		if fact.EntryID != entryID {
			kept = append(kept, fact)
		}
	}
	g.facts = kept
	g.rebuildAdjacencyLocked()
}

func (g *SemanticGraph) rebuildAdjacencyLocked() {
	g.adjacency = make(map[string][]int, len(g.facts))
	for i, fact := range g.facts {
		g.adjacency[fact.Subject] = append(g.adjacency[fact.Subject], i)
		if fact.Object != fact.Subject {
			g.adjacency[fact.Object] = append(g.adjacency[fact.Object], i)
		}
	}
}

func (g *SemanticGraph) factIndexesForFrontierLocked(frontier map[string]struct{}, frontierWeights map[string]float64, relationHints map[string]struct{}, ownerID, projectLower string, asOf time.Time, temporalMode SemanticTemporalMode) []int {
	seen := make(map[int]struct{})
	indexes := make([]int, 0)
	for entity := range frontier {
		for _, idx := range g.adjacency[entity] {
			if !semanticFactVisible(g.facts[idx], ownerID, projectLower, asOf, temporalMode) {
				continue
			}
			if _, ok := seen[idx]; ok {
				continue
			}
			seen[idx] = struct{}{}
			indexes = append(indexes, idx)
		}
	}
	sort.SliceStable(indexes, func(i, j int) bool {
		pi := g.semanticTraversalPriorityLocked(g.facts[indexes[i]], frontier, frontierWeights, relationHints)
		pj := g.semanticTraversalPriorityLocked(g.facts[indexes[j]], frontier, frontierWeights, relationHints)
		if pi != pj {
			return pi > pj
		}
		return indexes[i] < indexes[j]
	})
	return indexes
}

func (g *SemanticGraph) semanticTraversalPriorityLocked(fact SemanticFact, frontier map[string]struct{}, frontierWeights map[string]float64, relationHints map[string]struct{}) float64 {
	subjFrontier := containsEntity(frontier, fact.Subject)
	objFrontier := containsEntity(frontier, fact.Object)
	priority := semanticRelationWeight(fact.Predicate) * semanticRelationHintFactor(fact.Predicate, relationHints) * semanticRelationSchemaFactor(fact.Predicate)
	priority *= semanticFrontierWeight(fact, subjFrontier, objFrontier, frontierWeights)
	priority *= semanticDirectionFactor(fact.Predicate, subjFrontier, objFrontier)
	priority *= g.semanticDegreeFactorLocked(fact, subjFrontier, objFrontier)
	if fact.Negated {
		priority *= 0.5
	}
	return priority
}

func (g *SemanticGraph) searchRelationOnlyLocked(addHit func(string, float64, string), relationHints map[string]struct{}, dominance map[int]float64, ownerID, projectLower string, asOf time.Time, temporalMode SemanticTemporalMode, maxVisitedFacts int) {
	indexes := make([]int, 0, len(g.facts))
	for i := range g.facts {
		indexes = append(indexes, i)
	}
	sort.SliceStable(indexes, func(i, j int) bool {
		pi := semanticRelationWeight(g.facts[indexes[i]].Predicate) * semanticRelationHintFactor(g.facts[indexes[i]].Predicate, relationHints) * semanticRelationSchemaFactor(g.facts[indexes[i]].Predicate)
		pj := semanticRelationWeight(g.facts[indexes[j]].Predicate) * semanticRelationHintFactor(g.facts[indexes[j]].Predicate, relationHints) * semanticRelationSchemaFactor(g.facts[indexes[j]].Predicate)
		if pi != pj {
			return pi > pj
		}
		return indexes[i] < indexes[j]
	})
	visited := 0
	for _, idx := range indexes {
		if visited >= maxVisitedFacts {
			return
		}
		fact := g.facts[idx]
		if !semanticFactVisible(fact, ownerID, projectLower, asOf, temporalMode) {
			continue
		}
		if !semanticRelationMatchesHints(fact.Predicate, relationHints) {
			continue
		}
		visited++
		weight := semanticRelationWeight(fact.Predicate) * semanticRelationHintFactor(fact.Predicate, relationHints) * semanticRelationSchemaFactor(fact.Predicate)
		if fact.Negated {
			weight *= 0.35
		}
		weight *= semanticProvenanceFactor(fact.SourceType, fact.Pinned)
		weight *= semanticCertaintyFactor(fact.Content)
		weight *= dominance[idx]
		weight *= semanticTemporalFactor(fact.Status, fact.ValidAt, fact.InvalidAt, asOf, temporalMode)
		weight *= semanticFactRecencyFactor(fact.ValidAt, fact.CreatedAt, fact.UpdatedAt, asOf)
		addHit(fact.EntryID, weight, semanticPathString(fact, 0, true))
	}
}

func semanticMalformedTriplesForEntry(entryID string, raw []string) []SemanticGraphMalformedTriple {
	if len(raw) == 0 {
		return nil
	}
	var issues []SemanticGraphMalformedTriple
	for i := 0; i < len(raw); i += 3 {
		end := i + 3
		if end > len(raw) {
			end = len(raw)
		}
		items := append([]string(nil), raw[i:end]...)
		issue := SemanticGraphMalformedTriple{EntryID: entryID, Offset: i, Items: items}
		if len(items) != 3 {
			issue.Reason = "incomplete_triple"
			issues = append(issues, issue)
			continue
		}
		if _, ok := semanticEntityTokenName(items[0]); !ok {
			issue.Reason = "missing_subject_entity"
		} else if _, ok := semanticRelationTokenName(items[1]); !ok {
			issue.Reason = "missing_relation"
		} else if _, ok := semanticEntityTokenName(items[2]); !ok {
			issue.Reason = "missing_object_entity"
		}
		if issue.Reason != "" {
			issues = append(issues, issue)
		}
	}
	return issues
}
func semanticEntityTokenName(token string) (string, bool) {
	trimmed := strings.TrimSpace(token)
	if !strings.HasPrefix(strings.ToLower(trimmed), "entity:") {
		return "", false
	}
	name := normalizeEntityName(strings.TrimSpace(trimmed[len("entity:"):]))
	return name, name != ""
}

func semanticRelationTokenName(token string) (string, bool) {
	name, _, ok := semanticRelationTokenNameWithDirection(token)
	return name, ok
}

func semanticRelationTokenNameWithDirection(token string) (string, bool, bool) {
	trimmed := strings.TrimSpace(token)
	if !strings.HasPrefix(strings.ToLower(trimmed), "relation:") {
		return "", false, false
	}
	name, reverse := normalizeRelationNameWithDirection(strings.TrimSpace(trimmed[len("relation:"):]))
	return name, reverse, name != ""
}
func semanticEntities(raw []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, item := range raw {
		name, ok := semanticEntityTokenName(item)
		if !ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func semanticFactsFromEntry(e *Entry) []SemanticFact {
	if e == nil || len(e.Entities) < 3 {
		return nil
	}
	var facts []SemanticFact
	seen := make(map[string]struct{})
	negated := semanticIsNegated(e.Content)
	for i := 0; i+2 < len(e.Entities); i += 3 {
		subj, subjOK := semanticEntityTokenName(e.Entities[i])
		pred, reverse, predOK := semanticRelationTokenNameWithDirection(e.Entities[i+1])
		obj, objOK := semanticEntityTokenName(e.Entities[i+2])
		if !subjOK || !predOK || !objOK {
			continue
		}
		if reverse {
			subj, obj = obj, subj
		}
		key := subj + "\x00" + pred + "\x00" + obj
		if negated {
			key += "\x00neg"
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		facts = append(facts, SemanticFact{
			Subject:    subj,
			Predicate:  pred,
			Object:     obj,
			EntryID:    e.ID,
			Content:    e.Content,
			Negated:    negated,
			Scope:      e.Scope,
			Tags:       append([]string(nil), e.Tags...),
			ValidAt:    e.ValidAt,
			InvalidAt:  e.InvalidAt,
			Status:     e.Status,
			OwnerID:    e.OwnerID,
			Category:   e.Category,
			SourceType: e.SourceType,
			Pinned:     e.Pinned,
			CreatedAt:  e.CreatedAt,
			UpdatedAt:  e.UpdatedAt,
		})
	}
	return facts
}

func normalizeRelationNameWithDirection(name string) (string, bool) {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	switch name {
	case "blocked_by", "superseded_by", "contradicted_by":
		return canonicalRelationName(name), true
	default:
		return canonicalRelationName(name), false
	}
}
func normalizeRelationName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	return canonicalRelationName(name)
}

func canonicalRelationName(name string) string {
	switch name {
	case "same", "same_as", "also_called", "aka", "known_as", "alias", "alias_of", "\u522b\u540d", "\u53c8\u53eb", "\u4e5f\u53eb", "\u540c\u540d", "\u76f8\u540c":
		return "alias_of"
	case "has_config", "configured_as", "configuration_of", "has_port", "ssh_port", "port", "endpoint", "has_endpoint", "url", "host", "runs_on", "\u914d\u7f6e", "\u7aef\u53e3", "\u63a5\u53e3", "\u5730\u5740", "\u4e3b\u673a", "\u8fd0\u884c\u5728":
		return "config_of"
	case "credential", "credential_of", "has_credential", "login_for", "auth_for", "password_for", "token_for", "key_for", "\u51ed\u636e", "\u5bc6\u7801", "\u53e3\u4ee4", "\u4ee4\u724c", "\u5bc6\u94a5", "\u767b\u5f55":
		return "credential_for"
	case "preference", "prefers", "prefer", "likes", "preference_for", "\u504f\u597d", "\u559c\u6b22", "\u503e\u5411":
		return "preference_for"
	case "lives_in", "located_in", "location", "based_in", "stays_in", "\u4f4d\u4e8e", "\u4f4d\u7f6e", "\u5730\u70b9", "\u6240\u5728\u5730":
		return "located_in"
	case "works_at", "employed_by", "employer", "job_at", "\u5de5\u4f5c\u4e8e", "\u4efb\u804c", "\u96c7\u4e3b":
		return "works_at"
	case "depends", "depend_on", "requires", "uses", "calls", "needs", "\u4f9d\u8d56", "\u9700\u8981", "\u8c03\u7528", "\u4f7f\u7528":
		return "depends_on"
	case "cause", "causes", "caused", "because_of", "root_cause", "\u539f\u56e0", "\u5bfc\u81f4", "\u56e0\u4e3a", "\u6839\u56e0":
		return "caused_by"
	case "block", "blocked_by", "blocking", "\u963b\u585e", "\u963b\u6b62", "\u5361\u4f4f":
		return "blocks"
	case "replaces", "replaced", "supersede", "superseded_by", "\u66ff\u4ee3", "\u53d6\u4ee3", "\u8986\u76d6", "\u66f4\u65b0\u4e3a":
		return "supersedes"
	case "contradict", "contradicted_by", "conflict", "conflicts_with", "\u77db\u76fe", "\u51b2\u7a81", "\u4e0d\u4e00\u81f4":
		return "contradicts"
	case "part", "part_of", "belongs", "belongs_to", "member_of", "\u5c5e\u4e8e", "\u90e8\u5206", "\u96b6\u5c5e", "\u6210\u5458":
		return "belongs_to"
	default:
		return name
	}
}
func semanticRelationHintsFromQuery(query string, expanded ExpandResult) []string {
	seen := make(map[string]struct{})
	var hints []string
	add := func(s string) {
		s = normalizeRelationName(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		hints = append(hints, s)
	}
	for _, token := range expanded.QueryTokens {
		add(token)
	}
	for _, ent := range expanded.Entities {
		add(ent)
	}
	for _, part := range splitOnBoundaries(query) {
		add(part)
	}
	return hints
}

func semanticSeedWeightsFromEntities(entities []string) map[string]float64 {
	weights := make(map[string]float64, len(entities))
	for i, ent := range entities {
		n := normalizeEntityName(ent)
		if n == "" {
			continue
		}
		lengthBoost := 1.0 + math.Min(float64(len([]rune(n)))/20.0, 0.6)
		positionBoost := 1.0
		if i == 0 {
			positionBoost = 1.15
		} else if i == 1 {
			positionBoost = 1.05
		}
		weights[n] = lengthBoost * positionBoost
	}
	return weights
}

func semanticSeedWeight(entity string, provided map[string]float64) float64 {
	if provided != nil {
		if w := provided[entity]; w > 0 {
			return w
		}
	}
	return 1.0
}

func semanticFrontierWeight(fact SemanticFact, subjFrontier, objFrontier bool, weights map[string]float64) float64 {
	best := 1.0
	if subjFrontier && weights[fact.Subject] > best {
		best = weights[fact.Subject]
	}
	if objFrontier && weights[fact.Object] > best {
		best = weights[fact.Object]
	}
	return best
}

func semanticTemporalModeFromQuery(query string) SemanticTemporalMode {
	mode, _ := semanticTemporalOptionsFromQuery(query)
	return mode
}

func semanticTemporalOptionsFromQuery(query string) (SemanticTemporalMode, *time.Time) {
	if asOf := semanticAsOfTimeFromQuery(query); asOf != nil {
		return SemanticTemporalAsOf, asOf
	}
	lower := strings.ToLower(query)
	markers := []string{"history", "historical", "previous", "before", "formerly", "used to", "changed", "old", "past", "\u5386\u53f2", "\u4ee5\u524d", "\u4e4b\u524d", "\u8fc7\u53bb", "\u539f\u6765", "\u66fe\u7ecf", "\u65e7", "\u5f53\u65f6"}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return SemanticTemporalHistorical, nil
		}
	}
	return SemanticTemporalCurrent, nil
}

var (
	semanticDateISORe     = regexp.MustCompile(`(\d{4})-(\d{1,2})(?:-(\d{1,2}))?`)
	semanticDateChineseRe = regexp.MustCompile(`(\d{4})\x{5e74}(\d{1,2})\x{6708}(?:(\d{1,2})\x{65e5})?`)
)

func semanticAsOfTimeFromQuery(query string) *time.Time {
	if t := semanticParseDateMatch(semanticDateISORe.FindStringSubmatch(query)); t != nil {
		return t
	}
	return semanticParseDateMatch(semanticDateChineseRe.FindStringSubmatch(query))
}

func semanticParseDateMatch(match []string) *time.Time {
	if len(match) == 0 {
		return nil
	}
	year, ok := semanticAtoi(match[1])
	if !ok {
		return nil
	}
	month, ok := semanticAtoi(match[2])
	if !ok || month < 1 || month > 12 {
		return nil
	}
	day := 1
	if len(match) > 3 && match[3] != "" {
		var ok bool
		day, ok = semanticAtoi(match[3])
		if !ok || day < 1 || day > 31 {
			return nil
		}
	}
	t := time.Date(year, time.Month(month), day, 23, 59, 59, 0, time.Local)
	return &t
}

func semanticAtoi(s string) (int, bool) {
	n := 0
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

func semanticRelationSchemaFactor(predicate string) float64 {
	if semanticKnownRelation(predicate) {
		return 1.0
	}
	return 0.25
}

func semanticRelationWeight(predicate string) float64 {
	if spec, ok := semanticRelationSchema[predicate]; ok && spec.Weight > 0 {
		return spec.Weight
	}
	return 2.0
}

func normalizeRelationHints(hints []string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, hint := range hints {
		hint = normalizeRelationName(hint)
		if hint == "" {
			continue
		}
		for _, expanded := range relationHintFamily(hint) {
			out[expanded] = struct{}{}
		}
	}
	return out
}

func relationHintFamily(hint string) []string {
	switch hint {
	case "config", "configuration", "config_of", "port", "endpoint", "url", "host", "server":
		return []string{"config_of", "credential_for", "belongs_to", "part_of"}
	case "credential", "credential_for", "secret", "password", "token", "key", "login", "auth":
		return []string{"credential_for", "config_of"}
	case "preference", "preference_for", "prefers", "prefer", "like", "likes":
		return []string{"preference_for"}
	case "location", "located_in", "located", "lives", "address", "city":
		return []string{"located_in"}
	case "work", "works_at", "job", "employer", "company":
		return []string{"works_at"}
	case "dependency", "depends_on", "depends", "depend", "blocks", "blocked", "caused_by", "cause", "why", "because", "error":
		return []string{"depends_on", "caused_by", "blocks"}
	case "alias", "alias_of", "same_as", "same", "aka", "called", "name":
		return []string{"alias_of", "same_as"}
	default:
		return []string{hint}
	}
}

func semanticRelationMatchesHints(predicate string, hints map[string]struct{}) bool {
	if len(hints) == 0 {
		return true
	}
	_, ok := hints[predicate]
	return ok
}
func semanticRelationHintFactor(predicate string, hints map[string]struct{}) float64 {
	if len(hints) == 0 {
		return 1.0
	}
	if _, ok := hints[predicate]; ok {
		return 1.8
	}
	return 0.75
}

func semanticProvenanceFactor(sourceType string, pinned bool) float64 {
	factor := 1.0
	if pinned {
		factor *= 1.35
	}
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "manual", "user", "profile":
		factor *= 1.2
	case "conversation", "meeting":
		factor *= 1.0
	case "web", "import", "external":
		factor *= 0.9
	}
	return factor
}

func semanticCertaintyFactor(content string) float64 {
	lower := strings.ToLower(content)
	uncertain := []string{
		"might", "maybe", "possibly", "probably", "seems", "appears", "guess", "unsure", "unknown",
	}
	for _, marker := range uncertain {
		if strings.Contains(lower, marker) {
			return 0.65
		}
	}
	confirmed := []string{
		"confirmed", "verified", "must", "equals",
	}
	for _, marker := range confirmed {
		if strings.Contains(lower, marker) {
			return 1.08
		}
	}
	return 1.0
}

func semanticDirectionFactor(predicate string, fromSubject, fromObject bool) float64 {
	if fromSubject || !fromObject {
		return 1.0
	}
	if spec, ok := semanticRelationSchema[predicate]; ok && spec.ReverseFactor > 0 {
		return spec.ReverseFactor
	}
	return 0.65
}

func semanticAllowsExpansion(predicate string) bool {
	return semanticRelationSchema[predicate].AllowExpansion
}

func semanticAllowsReverseExpansion(predicate string) bool {
	return semanticRelationSchema[predicate].AllowReverse
}

func containsEntity(set map[string]struct{}, entity string) bool {
	_, ok := set[entity]
	return ok
}

func semanticHopDecay(hop int) float64 {
	switch hop {
	case 0:
		return 1.0
	case 1:
		return 0.55
	default:
		return 0.3
	}
}

func semanticMarginalPathFactor(existingPaths int) float64 {
	switch existingPaths {
	case 0:
		return 1.0
	case 1:
		return 0.5
	default:
		return 0.25
	}
}

func semanticPathString(fact SemanticFact, hop int, forward bool) string {
	dir := "-->"
	if !forward {
		dir = "<--"
	}
	if hop <= 0 {
		return fact.Subject + " --" + fact.Predicate + dir + " " + fact.Object
	}
	return fmt.Sprintf("%s --%s%s %s (hop %d)", fact.Subject, fact.Predicate, dir, fact.Object, hop+1)
}

func semanticFactRecencyFactor(validAt *time.Time, createdAt, updatedAt time.Time, reference time.Time) float64 {
	anchor := semanticKnownAnchor(createdAt, updatedAt)
	if validAt != nil && (anchor.IsZero() || (!reference.IsZero() && anchor.After(reference))) {
		anchor = *validAt
	}
	return semanticRecencyFactor(anchor, reference)
}
func semanticRecencyFactor(updatedAt, now time.Time) float64 {
	if updatedAt.IsZero() || now.IsZero() || updatedAt.After(now) {
		return 1.0
	}
	ageHours := now.Sub(updatedAt).Hours()
	if ageHours <= 0 {
		return 1.0
	}
	factor := 1.0 / (1.0 + ageHours/(24*30))
	if factor < 0.35 {
		return 0.35
	}
	return factor
}

func semanticKnownAnchor(createdAt, updatedAt time.Time) time.Time {
	if !createdAt.IsZero() {
		return createdAt
	}
	return updatedAt
}
func semanticKnownAt(validAt *time.Time, createdAt, updatedAt time.Time, asOf time.Time) bool {
	if asOf.IsZero() || validAt != nil {
		return true
	}
	anchor := semanticKnownAnchor(createdAt, updatedAt)
	return anchor.IsZero() || !anchor.After(asOf)
}
func semanticFactCurrent(validAt, invalidAt *time.Time, now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	if validAt != nil && validAt.After(now) {
		return false
	}
	if invalidAt != nil && !invalidAt.After(now) {
		return false
	}
	return true
}

func semanticFactVisible(fact SemanticFact, ownerID, projectLower string, now time.Time, mode SemanticTemporalMode) bool {
	if !semanticFactAllowed(fact, now, mode) {
		return false
	}
	if ownerID != "" && fact.OwnerID != "" && fact.OwnerID != ownerID {
		return false
	}
	return semanticProjectAllowed(fact.Scope, fact.Tags, projectLower)
}

func semanticEntryProjectAllowed(meta semanticEntryMeta, projectLower string) bool {
	return semanticProjectAllowed(meta.Scope, meta.Tags, projectLower)
}

func semanticProjectAllowed(scope Scope, tags []string, projectLower string) bool {
	if scope != ScopeProject || projectLower == "" {
		return true
	}
	boundToOtherProject := false
	for _, tag := range tags {
		tl := strings.ToLower(strings.TrimSpace(tag))
		if !semanticLooksLikePath(tl) {
			continue
		}
		if semanticProjectPathMatches(tl, projectLower) {
			return true
		}
		boundToOtherProject = true
	}
	return !boundToOtherProject
}

func semanticLooksLikePath(path string) bool {
	return (len(path) > 1 && path[0] == '/') || (len(path) > 2 && path[1] == ':' && (path[2] == '/' || path[2] == '\\'))
}

func semanticProjectPathMatches(tagPath, projectPath string) bool {
	tagPath = strings.TrimRight(tagPath, `/\\`)
	projectPath = strings.TrimRight(projectPath, `/\\`)
	if tagPath == "" || projectPath == "" {
		return false
	}
	if tagPath == projectPath {
		return true
	}
	return semanticPathIsWithin(projectPath, tagPath) || semanticPathIsWithin(tagPath, projectPath)
}

func semanticPathIsWithin(child, parent string) bool {
	if len(child) <= len(parent) || !strings.HasPrefix(child, parent) {
		return false
	}
	next := child[len(parent)]
	return next == '/' || next == '\\'
}
func semanticEntryAllowed(meta semanticEntryMeta, now time.Time, mode SemanticTemporalMode) bool {
	if mode == SemanticTemporalHistorical {
		return meta.Status == StatusActive || meta.Status == StatusSuperseded || meta.InvalidAt != nil
	}
	if mode == SemanticTemporalAsOf {
		return (meta.Status == StatusActive || meta.Status == StatusSuperseded || meta.InvalidAt != nil) && semanticKnownAt(meta.ValidAt, meta.CreatedAt, meta.UpdatedAt, now) && semanticFactCurrent(meta.ValidAt, meta.InvalidAt, now)
	}
	return meta.Status == StatusActive && semanticFactCurrent(meta.ValidAt, meta.InvalidAt, now)
}

func semanticFactAllowed(fact SemanticFact, now time.Time, mode SemanticTemporalMode) bool {
	if mode == SemanticTemporalHistorical {
		return fact.Status == StatusActive || fact.Status == StatusSuperseded || fact.InvalidAt != nil
	}
	if mode == SemanticTemporalAsOf {
		return (fact.Status == StatusActive || fact.Status == StatusSuperseded || fact.InvalidAt != nil) && semanticKnownAt(fact.ValidAt, fact.CreatedAt, fact.UpdatedAt, now) && semanticFactCurrent(fact.ValidAt, fact.InvalidAt, now)
	}
	return fact.Status == StatusActive && semanticFactCurrent(fact.ValidAt, fact.InvalidAt, now)
}

func semanticTemporalFactor(status Status, validAt, invalidAt *time.Time, now time.Time, mode SemanticTemporalMode) float64 {
	if mode == SemanticTemporalHistorical {
		if invalidAt != nil || status == StatusSuperseded {
			return 0.75
		}
		return 1.0
	}
	if !semanticFactCurrent(validAt, invalidAt, now) {
		return 0.0
	}
	if mode == SemanticTemporalAsOf {
		if invalidAt != nil || status == StatusSuperseded {
			return 0.9
		}
		return 1.0
	}
	if status == StatusSuperseded {
		return 0.0
	}
	return 1.0
}
