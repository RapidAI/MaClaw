package memory

// topic_cluster.go implements lightweight topic clustering for memory entries,
// inspired by Graphiti/Zep's Community Subgraph. Instead of full community
// detection (label propagation / Leiden algorithm), we use tag-based clustering
// which is sufficient for MacLaw's 500-2000 entry scale.
//
// Each cluster generates a summary that provides a "global view" of a topic,
// similar to Graphiti's community summaries. These summaries can be injected
// into system prompts for high-level context.

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// TopicCluster represents a group of related memory entries.
type TopicCluster struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"` // human-readable cluster name
	Tags     []string `json:"tags"` // defining tags for this cluster
	EntryIDs []string `json:"entry_ids"`
	Summary  string   `json:"summary,omitempty"` // LLM-generated summary
}

// TopicClusterer groups memory entries by shared tags to create
// topic-level summaries (analogous to Graphiti's Community Subgraph).
type TopicClusterer struct {
	mu       sync.RWMutex
	clusters []TopicCluster
}

// NewTopicClusterer creates a TopicClusterer.
func NewTopicClusterer() *TopicClusterer {
	return &TopicClusterer{}
}

// Cluster groups entries by tag co-occurrence. Entries sharing non-trivial
// tags are placed in the same cluster. Returns the discovered clusters.
//
// This is a lightweight alternative to graph community detection:
// - No external dependencies (no Neo4j, no label propagation library)
// - O(n*t) where n=entries, t=avg tags per entry
// - Sufficient for 500-2000 entries
func (tc *TopicClusterer) Cluster(entries []Entry) []TopicCluster {
	// Build tag -> entry ID mapping (skip trivial tags).
	// Use a set per tag to avoid duplicate entry IDs when the same tag
	// appears in both Tags and Entities.
	tagEntrySet := make(map[string]map[string]bool) // tag -> set of entryIDs
	for _, e := range entries {
		if !e.IsActive() {
			continue
		}
		addTagEntry := func(tag string, id string) {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if isTrivialTag(tag) {
				return
			}
			if tagEntrySet[tag] == nil {
				tagEntrySet[tag] = make(map[string]bool)
			}
			tagEntrySet[tag][id] = true
		}
		for _, tag := range e.Tags {
			addTagEntry(tag, e.ID)
		}
		for _, ent := range e.Entities {
			if name, ok := semanticEntityTokenName(ent); ok {
				addTagEntry(name, e.ID)
			}
		}
	}

	// Convert sets to slices.
	tagEntries := make(map[string][]string, len(tagEntrySet))
	for tag, idSet := range tagEntrySet {
		ids := make([]string, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		tagEntries[tag] = ids
	}

	// Find significant tags (appearing in at least 3 entries).
	type tagInfo struct {
		tag   string
		count int
	}
	var significant []tagInfo
	for tag, ids := range tagEntries {
		if len(ids) >= 3 {
			significant = append(significant, tagInfo{tag: tag, count: len(ids)})
		}
	}

	// Sort by count descending.
	sort.Slice(significant, func(i, j int) bool {
		return significant[i].count > significant[j].count
	})

	// Build clusters from significant tags, merging overlapping clusters.
	entryCluster := make(map[string]int) // entryID -> cluster index
	var clusters []TopicCluster
	clusterIdx := 0

	for _, ti := range significant {
		ids := tagEntries[ti.tag]

		// Check if most entries in this tag already belong to a cluster.
		clusterCounts := make(map[int]int)
		for _, id := range ids {
			if ci, ok := entryCluster[id]; ok {
				clusterCounts[ci]++
			}
		}

		// Find the dominant existing cluster (if any).
		bestCluster := -1
		bestCount := 0
		for ci, count := range clusterCounts {
			if count > bestCount {
				bestCount = count
				bestCluster = ci
			}
		}

		// If >50% of entries already in a cluster, merge into it.
		if bestCluster >= 0 && bestCount > len(ids)/2 {
			for _, id := range ids {
				if _, ok := entryCluster[id]; !ok {
					entryCluster[id] = bestCluster
					clusters[bestCluster].EntryIDs = append(clusters[bestCluster].EntryIDs, id)
				}
			}
			// Add tag to cluster.
			clusters[bestCluster].Tags = append(clusters[bestCluster].Tags, ti.tag)
			continue
		}

		// Create a new cluster.
		cluster := TopicCluster{
			ID:   fmt.Sprintf("cluster_%d", clusterIdx),
			Tags: []string{ti.tag},
		}
		var clusterEntryIDs []string
		for _, id := range ids {
			if _, ok := entryCluster[id]; !ok {
				clusterEntryIDs = append(clusterEntryIDs, id)
			}
		}
		if len(clusterEntryIDs) >= 3 {
			cluster.EntryIDs = clusterEntryIDs
			for _, id := range clusterEntryIDs {
				entryCluster[id] = clusterIdx
			}
			clusters = append(clusters, cluster)
			clusterIdx++
		}
		// If < 3 unique entries, skip this tag entirely.
		// Do NOT increment clusterIdx; it must stay in sync with len(clusters).
	}

	// Generate cluster names from top tags.
	for i := range clusters {
		if len(clusters[i].Tags) > 0 {
			// Use top 3 tags as the cluster name.
			nameTags := clusters[i].Tags
			if len(nameTags) > 3 {
				nameTags = nameTags[:3]
			}
			clusters[i].Name = strings.Join(nameTags, " / ")
		}
	}

	tc.mu.Lock()
	tc.clusters = clusters
	tc.mu.Unlock()

	return clusters
}

// GenerateSummaries uses the LLM to generate summaries for each cluster.
// This is analogous to Graphiti's community summary generation.
func (tc *TopicClusterer) GenerateSummaries(clusters []TopicCluster, entries []Entry, llm LLMChatCaller) []TopicCluster {
	if llm == nil || !llm.IsConfigured() {
		return clusters
	}

	// Build entry ID -> content map.
	entryMap := make(map[string]string, len(entries))
	for _, e := range entries {
		entryMap[e.ID] = e.Content
	}

	for i := range clusters {
		if len(clusters[i].EntryIDs) < 3 {
			continue
		}

		// Collect entry contents for this cluster.
		var contentBuf strings.Builder
		count := 0
		for _, id := range clusters[i].EntryIDs {
			if content, ok := entryMap[id]; ok {
				runes := []rune(content)
				if len(runes) > 200 {
					runes = runes[:200]
				}
				fmt.Fprintf(&contentBuf, "- %s\n", string(runes))
				count++
				if count >= 15 { // cap to avoid token overflow
					break
				}
			}
		}

		if count < 3 {
			continue
		}

		prompt := fmt.Sprintf(`Summarize the following %d related memory entries into a concise topic summary (2-3 sentences). Focus on the key theme, important facts, and relationships.

Topic tags: %s

Entries:
%s

Return ONLY the summary text, no formatting.`, count, strings.Join(clusters[i].Tags, ", "), contentBuf.String())

		resp, err := llm.ChatCall([]map[string]string{
			{"role": "user", "content": prompt},
		})
		if err != nil {
			continue
		}
		clusters[i].Summary = strings.TrimSpace(resp)
	}

	return clusters
}

// Clusters returns the current clusters.
func (tc *TopicClusterer) Clusters() []TopicCluster {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	result := make([]TopicCluster, len(tc.clusters))
	copy(result, tc.clusters)
	return result
}

// trivialTags is the set of tags too generic for clustering.
// Package-level to avoid allocation on every isTrivialTag call.
var trivialTags = map[string]bool{
	"extracted":            true,
	"online_extracted":     true,
	"conversation_summary": true,
	"trimmed":              true,
	"auto_salvaged":        true,
	"tmt":                  true,
	"l1":                   true,
	"l2":                   true,
	"segment":              true,
	"workflow":             true,
}

// isTrivialTag returns true for tags that are too generic for clustering.
func isTrivialTag(tag string) bool {
	if trivialTags[tag] {
		return true
	}
	// Skip date-like tags (e.g. "2026-04-30").
	if len(tag) == 10 && tag[4] == '-' && tag[7] == '-' {
		return true
	}
	// Skip very short tags.
	if len(tag) < 2 {
		return true
	}
	return false
}
