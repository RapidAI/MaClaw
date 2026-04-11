package experience

import (
	"database/sql"
	"log"
	"strings"
	"time"
)

// Deduplicator handles memory deduplication, merging, and expiry.
type Deduplicator struct {
	write *sql.DB
	read  *sql.DB
}

// NewDeduplicator creates a Deduplicator.
func NewDeduplicator(write, read *sql.DB) *Deduplicator {
	return &Deduplicator{write: write, read: read}
}

// DedupResult holds the outcome of a dedup run.
type DedupResult struct {
	Merged  int `json:"merged"`
	Expired int `json:"expired"`
	Scanned int `json:"scanned"`
}

// RunDedup scans auto-extracted memories and merges near-duplicates.
// Two memories are considered duplicates if they share the same scope
// and their titles have a Jaccard similarity > 0.6.
func (d *Deduplicator) RunDedup() DedupResult {
	rows, err := d.read.Query(`SELECT id, tenant_id, title, content, level, scope, tags, version, created_at
		FROM shared_memories WHERE status='active' AND tags LIKE '%自动提取%' ORDER BY tenant_id, scope, created_at`)
	if err != nil {
		log.Printf("[dedup] query failed: %v", err)
		return DedupResult{}
	}
	defer rows.Close()

	type memEntry struct {
		id, tenantID, title, content, level, scope, tags, createdAt string
		version                                                     int
	}
	var entries []memEntry
	for rows.Next() {
		var e memEntry
		if err := rows.Scan(&e.id, &e.tenantID, &e.title, &e.content, &e.level, &e.scope, &e.tags, &e.version, &e.createdAt); err != nil {
			continue
		}
		entries = append(entries, e)
	}

	result := DedupResult{Scanned: len(entries)}
	merged := make(map[string]bool)

	// Group by (tenant_id, scope) to avoid cross-tenant dedup
	scopeGroups := make(map[string][]int)
	for i, e := range entries {
		key := e.tenantID + "\x00" + e.scope
		scopeGroups[key] = append(scopeGroups[key], i)
	}

	for _, indices := range scopeGroups {
		for ii := 0; ii < len(indices); ii++ {
			i := indices[ii]
			if merged[entries[i].id] {
				continue
			}
			for jj := ii + 1; jj < len(indices); jj++ {
				j := indices[jj]
				if merged[entries[j].id] {
					continue
				}
				if jaccardSimilarity(entries[i].title, entries[j].title) > 0.6 {
					// Keep the newer one (j), disable the older one (i)
					d.disableMemory(entries[i].id)
					merged[entries[i].id] = true
					result.Merged++
					break
				}
			}
		}
	}
	return result
}

// RunExpiry disables auto-extracted memories older than the given days.
func (d *Deduplicator) RunExpiry(maxAgeDays int) int {
	if maxAgeDays <= 0 {
		maxAgeDays = 90
	}
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays).Format(time.RFC3339)
	res, err := d.write.Exec(`UPDATE shared_memories SET status='expired', updated_at=?
		WHERE status='active' AND tags LIKE '%自动提取%' AND created_at < ?`,
		time.Now().Format(time.RFC3339), cutoff)
	if err != nil {
		log.Printf("[dedup] expiry failed: %v", err)
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

func (d *Deduplicator) disableMemory(id string) {
	now := time.Now().Format(time.RFC3339)
	_, _ = d.write.Exec(`UPDATE shared_memories SET status='merged', updated_at=? WHERE id=?`, now, id)
}

// jaccardSimilarity computes Jaccard similarity between two strings
// using character bigrams.
func jaccardSimilarity(a, b string) float64 {
	setA := bigrams(strings.ToLower(a))
	setB := bigrams(strings.ToLower(b))
	if len(setA) == 0 && len(setB) == 0 {
		return 1.0
	}

	intersection := 0
	for k := range setA {
		if setB[k] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func bigrams(s string) map[string]bool {
	runes := []rune(s)
	set := make(map[string]bool)
	for i := 0; i < len(runes)-1; i++ {
		set[string(runes[i:i+2])] = true
	}
	return set
}

// CountAutoExtracted returns the number of active auto-extracted memories.
func (d *Deduplicator) CountAutoExtracted() int {
	var count int
	_ = d.read.QueryRow(`SELECT COUNT(*) FROM shared_memories WHERE status='active' AND tags LIKE '%自动提取%'`).Scan(&count)
	return count
}
