package memory

import (
	"sort"
	"sync"
	"time"
)

// maxRelatedPerEntry is the maximum number of related entries per node.
const maxRelatedPerEntry = 5

// graphEdge represents a weighted, typed edge in the memory graph.
type graphEdge struct {
	Strength  float64   `json:"strength"`
	LinkType  LinkType  `json:"link_type,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// memoryGraph maintains bidirectional weighted edges between memory entries.
type memoryGraph struct {
	mu    sync.RWMutex
	edges map[string]map[string]graphEdge // id → {relatedID → edge}
}

func newMemoryGraph() *memoryGraph {
	return &memoryGraph{edges: make(map[string]map[string]graphEdge)}
}

// link creates a bidirectional edge between two entries with default link type.
func (g *memoryGraph) link(id1, id2 string, strength float64) {
	g.linkTyped(id1, id2, strength, LinkRelated)
}

// linkTyped creates a bidirectional typed edge between two entries.
func (g *memoryGraph) linkTyped(id1, id2 string, strength float64, lt LinkType) {
	g.mu.Lock()
	defer g.mu.Unlock()
	edge := graphEdge{Strength: strength, LinkType: lt, UpdatedAt: time.Now()}
	g.linkOneSided(id1, id2, edge)
	g.linkOneSided(id2, id1, edge)
}

func (g *memoryGraph) linkOneSided(from, to string, edge graphEdge) {
	if g.edges[from] == nil {
		g.edges[from] = make(map[string]graphEdge)
	}
	// Enforce max related limit — only add if under limit or stronger than weakest.
	if len(g.edges[from]) >= maxRelatedPerEntry {
		weakestID := ""
		weakestStr := edge.Strength
		for id, e := range g.edges[from] {
			if e.Strength < weakestStr {
				weakestStr = e.Strength
				weakestID = id
			}
		}
		if weakestID == "" {
			return // new edge is weaker than all existing
		}
		delete(g.edges[from], weakestID)
	}
	g.edges[from][to] = edge
}

// remove deletes all edges involving the given entry.
func (g *memoryGraph) remove(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Remove outgoing edges.
	if neighbors, ok := g.edges[id]; ok {
		for neighbor := range neighbors {
			delete(g.edges[neighbor], id)
		}
		delete(g.edges, id)
	}
}

// expand performs a BFS expansion from the given seed IDs up to `hops` levels.
// Returns all discovered IDs (excluding seeds) with their accumulated edge weight.
func (g *memoryGraph) expand(seedIDs []string, hops int) map[string]float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	visited := make(map[string]bool, len(seedIDs))
	for _, id := range seedIDs {
		visited[id] = true
	}

	result := make(map[string]float64)
	frontier := seedIDs

	for hop := 0; hop < hops && len(frontier) > 0; hop++ {
		var next []string
		for _, id := range frontier {
			for neighbor, edge := range g.edges[id] {
				if visited[neighbor] {
					continue
				}
				visited[neighbor] = true
				// Decay factor per hop.
				decayed := edge.Strength * 0.5
				if existing, ok := result[neighbor]; ok {
					if decayed > existing {
						result[neighbor] = decayed
					}
				} else {
					result[neighbor] = decayed
				}
				next = append(next, neighbor)
			}
		}
		frontier = next
	}
	return result
}

// neighborsOf returns the direct neighbors and their edge weights.
func (g *memoryGraph) neighborsOf(id string) map[string]float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make(map[string]float64)
	for k, e := range g.edges[id] {
		out[k] = e.Strength
	}
	return out
}

// neighborsTypedOf returns the direct neighbors with full edge info.
func (g *memoryGraph) neighborsTypedOf(id string) map[string]graphEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make(map[string]graphEdge)
	for k, e := range g.edges[id] {
		out[k] = e
	}
	return out
}

// rebuild reconstructs the graph from Entry.RelatedIDs fields.
func (g *memoryGraph) rebuild(entries []Entry) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.edges = make(map[string]map[string]graphEdge, len(entries))
	idSet := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.IsActive() {
			idSet[e.ID] = true
		}
	}
	for _, e := range entries {
		if !e.IsActive() {
			continue
		}
		if len(e.RelatedEdges) > 0 {
			for _, rel := range e.RelatedEdges {
				if rel.ID == "" || !idSet[rel.ID] {
					continue
				}
				if g.edges[e.ID] == nil {
					g.edges[e.ID] = make(map[string]graphEdge)
				}
				strength := rel.Strength
				if strength <= 0 {
					strength = 1.0
				}
				g.edges[e.ID][rel.ID] = graphEdge{Strength: strength, LinkType: rel.LinkType, UpdatedAt: rel.UpdatedAt}
			}
			continue
		}

		for _, relID := range e.RelatedIDs {
			if !idSet[relID] {
				continue
			}
			if g.edges[e.ID] == nil {
				g.edges[e.ID] = make(map[string]graphEdge)
			}
			g.edges[e.ID][relID] = graphEdge{Strength: 1.0} // legacy default from persisted data
		}
	}
}

// relatedIDsFor returns the sorted list of neighbor IDs for persistence.
func (g *memoryGraph) relatedIDsFor(id string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	neighbors := g.edges[id]
	if len(neighbors) == 0 {
		return nil
	}
	ids := make([]string, 0, len(neighbors))
	for k := range neighbors {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	return ids
}

// relatedEdgesFor returns stable, persistent edge metadata for a node.
func (g *memoryGraph) relatedEdgesFor(id string) []RelatedEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	neighbors := g.edges[id]
	if len(neighbors) == 0 {
		return nil
	}
	edges := make([]RelatedEdge, 0, len(neighbors))
	for relatedID, edge := range neighbors {
		edges = append(edges, RelatedEdge{
			ID:        relatedID,
			Strength:  edge.Strength,
			LinkType:  edge.LinkType,
			UpdatedAt: edge.UpdatedAt,
		})
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return edges
}
