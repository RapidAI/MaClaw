package knowledge

import (
	"context"
	"database/sql"
	"sort"
	"strings"
)

const searchBodyWindowRunes = 360

// searchResultTypeBonus preserves the historical auto-route thresholds after
// relevance scores stopped carrying type-specific adjustments.
func searchResultTypeBonus(resultType string) float64 {
	switch strings.ToLower(strings.TrimSpace(resultType)) {
	case "card":
		return 0.30
	case "fact":
		return 0.18
	case "node":
		return 0.08
	default:
		return 0
	}
}

// searchBodyScore compares lexical, vector, and fused scores without inventing
// a type-bonus adjustment. Type priority belongs only in tie-breaking.
func searchBodyScore(result SearchResult) float64 {
	return result.Score
}

// searchBodyKey is the carrier a hit belongs to. A table row wins over a node.
// Cards and facts with neither id are not a default-search body.
func searchBodyKey(result SearchResult) string {
	if rowID := strings.TrimSpace(result.RowID); rowID != "" {
		return "row:" + rowID
	}
	if nodeID := strings.TrimSpace(result.NodeID); nodeID != "" {
		return "node:" + nodeID
	}
	return ""
}

type searchBodyGroup struct {
	key         string
	score       float64
	seen        bool
	hasBody     bool
	body        SearchResult
	hasCard     bool
	card        SearchResult
	hasFact     bool
	fact        SearchResult
	hasFallback bool
	fallback    SearchResult
}

func (g *searchBodyGroup) add(result SearchResult) {
	score := searchBodyScore(result)
	if !g.seen || score > g.score {
		g.score = score
		g.seen = true
	}
	if !g.hasFallback {
		g.fallback = result
		g.hasFallback = true
	}
	switch result.ResultType {
	case "table_row":
		if !g.hasBody || searchBodyScore(g.body) < score {
			g.body = result
			g.hasBody = true
		}
	case "node":
		if strings.HasPrefix(g.key, "row:") {
			return
		}
		if !g.hasBody || searchBodyScore(g.body) < score {
			g.body = result
			g.hasBody = true
		}
	case "card":
		if !g.hasCard || searchBodyScore(g.card) < score {
			g.card = result
			g.hasCard = true
		}
	case "fact":
		if !g.hasFact || searchBodyScore(g.fact) < score {
			g.fact = result
			g.hasFact = true
		}
	}
}

func (g searchBodyGroup) result() SearchResult {
	out := g.fallback
	if g.hasBody {
		out = g.body
	}
	if strings.HasPrefix(g.key, "row:") {
		out.ResultType = "table_row"
		if !g.hasBody {
			out.Snippet = ""
			out.Summary = ""
		}
	} else if strings.HasPrefix(g.key, "node:") {
		out.ResultType = "node"
		if !g.hasBody {
			out.Snippet = ""
			out.Summary = ""
		}
	}
	out.Score = g.score
	if g.hasCard {
		if g.card.CardID != "" {
			out.CardID = g.card.CardID
		}
		if title := strings.TrimSpace(g.card.CardTitle); title != "" {
			out.CardTitle = title
		}
		if claim := strings.TrimSpace(g.card.Claim); claim != "" {
			out.Claim = claim
		}
		out = fillSearchBodyProvenance(out, g.card)
	}
	if g.hasFact {
		if g.fact.FactID != "" {
			out.FactID = g.fact.FactID
		}
		if out.CardID == "" {
			out.CardID = g.fact.CardID
		}
		if strings.TrimSpace(g.fact.Subject) != "" {
			out.Subject = g.fact.Subject
			out.Predicate = g.fact.Predicate
			out.Object = g.fact.Object
		}
		if strings.TrimSpace(out.Claim) == "" {
			out.Claim = strings.TrimSpace(g.fact.Claim)
		}
		if strings.TrimSpace(out.CardTitle) == "" {
			out.CardTitle = strings.TrimSpace(g.fact.CardTitle)
		}
		out = fillSearchBodyProvenance(out, g.fact)
	}
	out.Citation = formatResultCitation(out)
	return out
}

func fillSearchBodyProvenance(out, from SearchResult) SearchResult {
	if out.NodeID == "" {
		out.NodeID = from.NodeID
	}
	if out.RowID == "" {
		out.RowID = from.RowID
	}
	if out.Source.ID == "" {
		out.Source = from.Source
	}
	if out.Page == 0 {
		out.Page = from.Page
	}
	if out.SheetName == "" {
		out.SheetName = from.SheetName
	}
	if out.NodeTitle == "" {
		out.NodeTitle = from.NodeTitle
	}
	if out.NodeType == "" {
		out.NodeType = from.NodeType
	}
	if out.RowIndex == 0 {
		out.RowIndex = from.RowIndex
	}
	if out.RowRange == "" {
		out.RowRange = from.RowRange
	}
	if out.ColRange == "" {
		out.ColRange = from.ColRange
	}
	if out.TableID == "" {
		out.TableID = from.TableID
	}
	if out.LibraryID == "" {
		out.LibraryID = from.LibraryID
	}
	return out
}

// collapseSearchBodies keeps one hit per row or node. Standalone cards and
// facts remain searchable when legacy or imported data has no body association.
func collapseSearchBodies(results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}
	order := make([]string, 0, len(results))
	groups := make(map[string]*searchBodyGroup, len(results))
	for _, result := range results {
		key := searchBodyKey(result)
		if key == "" {
			key = embeddingResultKey(result)
			if key == "node:" {
				key = result.ResultType + ":" + result.Citation
			}
		}
		group, ok := groups[key]
		if !ok {
			group = &searchBodyGroup{key: key}
			groups[key] = group
			order = append(order, key)
		}
		group.add(result)
	}
	out := make([]SearchResult, 0, len(order))
	for _, key := range order {
		out = append(out, groups[key].result())
	}
	return out
}

func sortSearchResultsByBodyScore(results []SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Citation < results[j].Citation
	})
}

func (s *SQLiteStore) hydrateCollapsedBodyText(ctx context.Context, query string, results []SearchResult) error {
	if s == nil || s.db == nil || len(results) == 0 {
		return nil
	}
	var nodeStmt, rowStmt *sql.Stmt
	defer func() {
		if nodeStmt != nil {
			nodeStmt.Close()
		}
		if rowStmt != nil {
			rowStmt.Close()
		}
	}()
	for i := range results {
		switch results[i].ResultType {
		case "node":
			if strings.TrimSpace(results[i].Snippet) != "" || strings.TrimSpace(results[i].NodeID) == "" {
				continue
			}
			if nodeStmt == nil {
				var err error
				nodeStmt, err = s.db.PrepareContext(ctx, `SELECT COALESCE(text, '') FROM document_nodes WHERE id = ?`)
				if err != nil {
					return err
				}
			}
			var text string
			err := nodeStmt.QueryRowContext(ctx, results[i].NodeID).Scan(&text)
			if err == sql.ErrNoRows {
				continue
			}
			if err != nil {
				return err
			}
			results[i].Snippet = searchBodyTextWindow(text, query)
		case "table_row":
			if strings.TrimSpace(results[i].Summary) != "" || strings.TrimSpace(results[i].Snippet) != "" || strings.TrimSpace(results[i].RowID) == "" {
				continue
			}
			if rowStmt == nil {
				var err error
				rowStmt, err = s.db.PrepareContext(ctx, `SELECT COALESCE(row_text, '') FROM kb_rows WHERE id = ?`)
				if err != nil {
					return err
				}
			}
			var text string
			err := rowStmt.QueryRowContext(ctx, results[i].RowID).Scan(&text)
			if err == sql.ErrNoRows {
				continue
			}
			if err != nil {
				return err
			}
			text = strings.TrimSpace(text)
			results[i].Summary = text
			results[i].Snippet = text
		}
	}
	return nil
}

func searchBodyTextWindow(text, query string) string {
	return searchBodyTextWindowLimit(text, query, searchBodyWindowRunes)
}

func searchBodyTextWindowLimit(text, query string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	terms := extractQueryTermsForSnippet(query)
	lower := strings.ToLower(text)
	for _, term := range terms {
		if strings.Contains(lower, strings.ToLower(term)) {
			return extractLikeSnippet(text, terms, limit)
		}
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}
