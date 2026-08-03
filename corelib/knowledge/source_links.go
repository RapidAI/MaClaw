package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const SourceRelationTopicRelated = "topic_related"

func (s *SQLiteStore) RefreshSourceTopicLinks(ctx context.Context, sourceID string, limit int) (SourceTopicLinkBuildResult, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return SourceTopicLinkBuildResult{}, fmt.Errorf("source id is required")
	}
	if limit <= 0 {
		limit = 8
	}
	if limit > 50 {
		limit = 50
	}
	source, err := s.GetSource(ctx, sourceID)
	if err != nil {
		return SourceTopicLinkBuildResult{}, err
	}
	opts := SearchOptions{
		Query:           source.Title,
		TopicHint:       source.TopicHint,
		ContextTerms:    source.Labels,
		OwnerID:         source.OwnerID,
		TenantID:        source.TenantID,
		ProjectPath:     source.ProjectPath,
		IncludeDisabled: false,
		Limit:           limit + 1,
	}
	report, err := s.TopicRelevance(ctx, opts)
	if err != nil {
		return SourceTopicLinkBuildResult{}, err
	}
	result := SourceTopicLinkBuildResult{
		SourceID: sourceID,
		Scanned:  report.Count,
		Notes:    []string{"local_source_topic_links_no_llm", "derived_from_topic_relevance"},
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_source_links WHERE relation = ? AND (source_id = ? OR related_source_id = ?)`, SourceRelationTopicRelated, sourceID, sourceID); err != nil {
		return result, err
	}
	for _, item := range report.Sources {
		if item.Source.ID == "" || item.Source.ID == sourceID {
			result.Skipped++
			continue
		}
		link := SourceLink{
			SourceID:        sourceID,
			RelatedSourceID: item.Source.ID,
			Relation:        SourceRelationTopicRelated,
			Score:           item.Score,
			Terms:           item.MatchedTerms,
			Evidence:        sourceLinkEvidence(item),
			RelatedSource:   item.Source,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		result.Candidates++
		if err := insertSourceLinkTx(ctx, tx, link); err != nil {
			return result, err
		}
		reverse := link
		reverse.SourceID = item.Source.ID
		reverse.RelatedSourceID = sourceID
		reverse.RelatedSource = source
		if err := insertSourceLinkTx(ctx, tx, reverse); err != nil {
			return result, err
		}
		result.Linked++
		result.Links = append(result.Links, link)
		if result.Linked >= limit {
			break
		}
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func (s *SQLiteStore) PreviewSourceTopicLinks(ctx context.Context, sourceID string, limit int) (SourceTopicLinkBuildResult, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return SourceTopicLinkBuildResult{}, fmt.Errorf("source id is required")
	}
	if limit <= 0 {
		limit = 8
	}
	if limit > 50 {
		limit = 50
	}
	source, err := s.GetSource(ctx, sourceID)
	if err != nil {
		return SourceTopicLinkBuildResult{}, err
	}
	opts := SearchOptions{
		Query:           source.Title,
		TopicHint:       source.TopicHint,
		ContextTerms:    source.Labels,
		OwnerID:         source.OwnerID,
		TenantID:        source.TenantID,
		ProjectPath:     source.ProjectPath,
		IncludeDisabled: false,
		Limit:           limit + 1,
	}
	report, err := s.TopicRelevance(ctx, opts)
	if err != nil {
		return SourceTopicLinkBuildResult{}, err
	}
	result := SourceTopicLinkBuildResult{
		SourceID: sourceID,
		Scanned:  report.Count,
		Notes:    []string{"local_source_topic_link_preview_no_llm", "derived_from_topic_relevance", "dry_run_no_write"},
	}
	now := time.Now().UTC()
	for _, item := range report.Sources {
		if item.Source.ID == "" || item.Source.ID == sourceID {
			result.Skipped++
			continue
		}
		result.Links = append(result.Links, SourceLink{
			SourceID:        sourceID,
			RelatedSourceID: item.Source.ID,
			Relation:        SourceRelationTopicRelated,
			Score:           item.Score,
			Terms:           item.MatchedTerms,
			Evidence:        sourceLinkEvidence(item),
			RelatedSource:   item.Source,
			CreatedAt:       now,
			UpdatedAt:       now,
		})
		result.Candidates++
		if result.Candidates >= limit {
			break
		}
	}
	return result, nil
}

func (s *SQLiteStore) LinkSources(ctx context.Context, link SourceLink) (SourceLink, error) {
	link.SourceID = strings.TrimSpace(link.SourceID)
	link.RelatedSourceID = strings.TrimSpace(link.RelatedSourceID)
	if link.SourceID == "" || link.RelatedSourceID == "" {
		return SourceLink{}, fmt.Errorf("source_id and related_source_id are required")
	}
	if link.SourceID == link.RelatedSourceID {
		return SourceLink{}, fmt.Errorf("cannot link a source to itself")
	}
	source, err := s.GetSource(ctx, link.SourceID)
	if err != nil {
		return SourceLink{}, err
	}
	related, err := s.GetSource(ctx, link.RelatedSourceID)
	if err != nil {
		return SourceLink{}, err
	}
	link.Relation = strings.TrimSpace(link.Relation)
	if link.Relation == "" {
		link.Relation = SourceRelationTopicRelated
	}
	link.Terms = uniqueTrimmed(link.Terms)
	link.Evidence = uniqueTrimmed(link.Evidence)
	if link.Score <= 0 {
		link.Score = 1
	}
	now := time.Now().UTC()
	link.CreatedAt = now
	link.UpdatedAt = now
	link.RelatedSource = related
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SourceLink{}, err
	}
	defer tx.Rollback()
	if err := insertSourceLinkTx(ctx, tx, link); err != nil {
		return SourceLink{}, err
	}
	reverse := link
	reverse.SourceID = link.RelatedSourceID
	reverse.RelatedSourceID = link.SourceID
	reverse.RelatedSource = source
	if err := insertSourceLinkTx(ctx, tx, reverse); err != nil {
		return SourceLink{}, err
	}
	if err := insertSourceLinkEventTx(ctx, tx, link, "link", "manual"); err != nil {
		return SourceLink{}, err
	}
	if err := tx.Commit(); err != nil {
		return SourceLink{}, err
	}
	return link, nil
}

func (s *SQLiteStore) UnlinkSources(ctx context.Context, sourceID string, relatedSourceID string, relation string) (SourceUnlinkResult, error) {
	sourceID = strings.TrimSpace(sourceID)
	relatedSourceID = strings.TrimSpace(relatedSourceID)
	relation = strings.TrimSpace(relation)
	if relation == "" {
		relation = SourceRelationTopicRelated
	}
	if sourceID == "" || relatedSourceID == "" {
		return SourceUnlinkResult{}, fmt.Errorf("source_id and related_source_id are required")
	}
	result := SourceUnlinkResult{
		SourceID:        sourceID,
		RelatedSourceID: relatedSourceID,
		Relation:        relation,
		Notes:           []string{"local_source_unlink_no_llm"},
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM knowledge_source_links
		WHERE relation = ? AND ((source_id = ? AND related_source_id = ?) OR (source_id = ? AND related_source_id = ?))`,
		relation, sourceID, relatedSourceID, relatedSourceID, sourceID)
	if err != nil {
		return result, err
	}
	deleted, _ := res.RowsAffected()
	result.Deleted = int(deleted)
	if result.Deleted > 0 {
		link := SourceLink{SourceID: sourceID, RelatedSourceID: relatedSourceID, Relation: relation}
		if err := insertSourceLinkEventTx(ctx, tx, link, "unlink", "manual"); err != nil {
			return result, err
		}
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func (s *SQLiteStore) ListSourceLinkEvents(ctx context.Context, sourceID string, limit int) ([]SourceLinkEvent, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, fmt.Errorf("source id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_id, related_source_id, relation, action, score, COALESCE(terms_json, '[]'), COALESCE(evidence_json, '[]'), COALESCE(note, ''), created_at
		FROM knowledge_source_link_events
		WHERE source_id = ? OR related_source_id = ?
		ORDER BY created_at DESC
		LIMIT ?`, sourceID, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]SourceLinkEvent, 0)
	for rows.Next() {
		event, err := scanSourceLinkEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *SQLiteStore) RefreshSourceTopicLinksByFilter(ctx context.Context, opts ListSourcesOptions, limitPerSource int) (SourceTopicLinkBuildResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.Limit > 1000 {
		opts.Limit = 1000
	}
	if opts.Status == "" {
		opts.IncludeDisabled = true
	}
	sources, err := s.ListSources(ctx, opts)
	if err != nil {
		return SourceTopicLinkBuildResult{}, err
	}
	result := SourceTopicLinkBuildResult{
		Scanned: len(sources),
		Notes:   []string{"local_source_topic_links_no_llm", "filter_batch"},
	}
	for _, source := range sources {
		item, err := s.RefreshSourceTopicLinks(ctx, source.ID, limitPerSource)
		if err != nil {
			result.Skipped++
			result.Notes = append(result.Notes, source.ID+":"+err.Error())
			continue
		}
		result.Linked += item.Linked
		result.Skipped += item.Skipped
		result.Links = append(result.Links, item.Links...)
	}
	return result, nil
}

func (s *SQLiteStore) ListSourceLinks(ctx context.Context, sourceID string, limit int) ([]SourceLink, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, fmt.Errorf("source id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT l.source_id, l.related_source_id, l.relation, l.score, COALESCE(l.terms_json, '[]'), COALESCE(l.evidence_json, '[]'), l.created_at, l.updated_at,
		s.id, s.kind, s.uri, s.canonical_uri, s.title, s.author, s.site_name, s.published_at, s.fetched_at, s.content_hash,
		s.owner_id, s.tenant_id, s.project_path, s.topic_hint, s.source_trust, s.batch_id, s.relative_path, s.status, s.error_message, s.created_at, s.updated_at
		FROM knowledge_source_links l
		JOIN knowledge_sources s ON s.id = l.related_source_id
		WHERE l.source_id = ?
		ORDER BY l.score DESC, l.updated_at DESC
		LIMIT ?`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := make([]SourceLink, 0)
	for rows.Next() {
		link, err := scanSourceLink(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].Score != links[j].Score {
			return links[i].Score > links[j].Score
		}
		return links[i].RelatedSourceID < links[j].RelatedSourceID
	})
	return links, nil
}

func (s *SQLiteStore) SourceGraph(ctx context.Context, opts ListSourcesOptions, edgeLimit int) (SourceGraphResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.Limit > 500 {
		opts.Limit = 500
	}
	if edgeLimit <= 0 {
		edgeLimit = 500
	}
	if edgeLimit > 2000 {
		edgeLimit = 2000
	}
	if opts.Status == "" {
		opts.IncludeDisabled = true
	}
	sources, err := s.ListSources(ctx, opts)
	if err != nil {
		return SourceGraphResult{}, err
	}
	result := SourceGraphResult{
		Nodes: make([]SourceGraphNode, 0, len(sources)),
		Notes: []string{"local_source_graph_no_llm", "derived_from_persisted_source_links"},
	}
	sourceIDs := make([]string, 0, len(sources))
	nodesByID := map[string]*SourceGraphNode{}
	for _, source := range sources {
		if source.ID == "" {
			continue
		}
		node := sourceGraphNode(source)
		result.Nodes = append(result.Nodes, node)
		nodesByID[source.ID] = &result.Nodes[len(result.Nodes)-1]
		sourceIDs = append(sourceIDs, source.ID)
	}
	result.Count = len(result.Nodes)
	if len(sourceIDs) == 0 {
		return result, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(sourceIDs)), ",")
	args := make([]interface{}, 0, len(sourceIDs)*2+1)
	for _, id := range sourceIDs {
		args = append(args, id)
	}
	for _, id := range sourceIDs {
		args = append(args, id)
	}
	args = append(args, edgeLimit*2)
	rows, err := s.db.QueryContext(ctx, `SELECT source_id, related_source_id, relation, score, COALESCE(terms_json, '[]'), COALESCE(evidence_json, '[]')
		FROM knowledge_source_links
		WHERE source_id IN (`+placeholders+`) AND related_source_id IN (`+placeholders+`)
		ORDER BY score DESC, updated_at DESC
		LIMIT ?`, args...)
	if err != nil {
		return SourceGraphResult{}, err
	}
	defer rows.Close()
	seen := map[string]struct{}{}
	for rows.Next() {
		var edge SourceGraphEdge
		var termsJSON, evidenceJSON string
		if err := rows.Scan(&edge.SourceID, &edge.RelatedSourceID, &edge.Relation, &edge.Score, &termsJSON, &evidenceJSON); err != nil {
			return SourceGraphResult{}, err
		}
		if edge.SourceID == "" || edge.RelatedSourceID == "" || edge.SourceID == edge.RelatedSourceID {
			continue
		}
		key := sourceGraphEdgeKey(edge.SourceID, edge.RelatedSourceID, edge.Relation)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		_ = json.Unmarshal([]byte(termsJSON), &edge.Terms)
		_ = json.Unmarshal([]byte(evidenceJSON), &edge.Evidence)
		edge.Terms = uniqueTrimmed(edge.Terms)
		edge.Evidence = uniqueTrimmed(edge.Evidence)
		edge.ID = key
		result.Edges = append(result.Edges, edge)
		if node := nodesByID[edge.SourceID]; node != nil {
			node.Degree++
		}
		if node := nodesByID[edge.RelatedSourceID]; node != nil {
			node.Degree++
		}
		if len(result.Edges) >= edgeLimit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return SourceGraphResult{}, err
	}
	result.EdgeCount = len(result.Edges)
	if result.Count > 1 {
		result.Density = float64(result.EdgeCount) / (float64(result.Count*(result.Count-1)) / 2)
	}
	annotateSourceGraphComponents(&result)
	for _, node := range result.Nodes {
		if node.Degree == 0 {
			result.Isolates = append(result.Isolates, node)
		}
	}
	sort.SliceStable(result.Nodes, func(i, j int) bool {
		if result.Nodes[i].Degree != result.Nodes[j].Degree {
			return result.Nodes[i].Degree > result.Nodes[j].Degree
		}
		return result.Nodes[i].Label < result.Nodes[j].Label
	})
	sort.SliceStable(result.Isolates, func(i, j int) bool {
		return result.Isolates[i].Label < result.Isolates[j].Label
	})
	return result, nil
}

func (s *SQLiteStore) SourceNeighborhood(ctx context.Context, sourceID string, depth int, limit int, edgeLimit int) (SourceGraphResult, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return SourceGraphResult{}, fmt.Errorf("source id is required")
	}
	if _, err := s.GetSource(ctx, sourceID); err != nil {
		return SourceGraphResult{}, err
	}
	if depth <= 0 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if edgeLimit <= 0 {
		edgeLimit = 500
	}
	if edgeLimit > 2000 {
		edgeLimit = 2000
	}
	visited := map[string]int{sourceID: 0}
	order := []string{sourceID}
	frontier := []string{sourceID}
	for level := 1; level <= depth && len(frontier) > 0 && len(order) < limit; level++ {
		nextIDs, err := s.sourceNeighborIDs(ctx, frontier, edgeLimit)
		if err != nil {
			return SourceGraphResult{}, err
		}
		nextFrontier := make([]string, 0)
		for _, id := range nextIDs {
			if id == "" {
				continue
			}
			if _, ok := visited[id]; ok {
				continue
			}
			visited[id] = level
			order = append(order, id)
			nextFrontier = append(nextFrontier, id)
			if len(order) >= limit {
				break
			}
		}
		frontier = nextFrontier
	}
	graph, err := s.SourceGraph(ctx, ListSourcesOptions{SourceIDs: order, Limit: len(order)}, edgeLimit)
	if err != nil {
		return SourceGraphResult{}, err
	}
	graph.FocusSourceID = sourceID
	graph.Depth = depth
	graph.Notes = append(graph.Notes, "local_source_neighborhood_no_llm")
	return graph, nil
}

func (s *SQLiteStore) SourcePath(ctx context.Context, fromSourceID string, toSourceID string, maxDepth int, edgeLimit int) (SourcePathResult, error) {
	fromSourceID = strings.TrimSpace(fromSourceID)
	toSourceID = strings.TrimSpace(toSourceID)
	if fromSourceID == "" || toSourceID == "" {
		return SourcePathResult{}, fmt.Errorf("from_source_id and to_source_id are required")
	}
	if maxDepth <= 0 {
		maxDepth = 4
	}
	if maxDepth > 6 {
		maxDepth = 6
	}
	if edgeLimit <= 0 {
		edgeLimit = 1000
	}
	if edgeLimit > 5000 {
		edgeLimit = 5000
	}
	if _, err := s.GetSource(ctx, fromSourceID); err != nil {
		return SourcePathResult{}, err
	}
	if _, err := s.GetSource(ctx, toSourceID); err != nil {
		return SourcePathResult{}, err
	}
	result := SourcePathResult{
		FromSourceID: fromSourceID,
		ToSourceID:   toSourceID,
		MaxDepth:     maxDepth,
		Notes:        []string{"local_source_path_no_llm", "derived_from_persisted_source_links"},
	}
	if fromSourceID == toSourceID {
		node, err := s.sourcePathNode(ctx, fromSourceID)
		if err != nil {
			return SourcePathResult{}, err
		}
		result.Found = true
		result.VisitedCount = 1
		result.Nodes = []SourceGraphNode{node}
		return result, nil
	}
	visited := map[string]struct{}{fromSourceID: {}}
	parent := map[string]string{}
	parentStep := map[string]SourcePathStep{}
	frontier := []string{fromSourceID}
	found := false
	for depth := 1; depth <= maxDepth && len(frontier) > 0 && !found; depth++ {
		frontierSet := make(map[string]struct{}, len(frontier))
		for _, id := range frontier {
			frontierSet[id] = struct{}{}
		}
		edges, err := s.sourceNeighborEdges(ctx, frontier, edgeLimit)
		if err != nil {
			return SourcePathResult{}, err
		}
		result.SearchedEdgeCount += len(edges)
		if len(edges) >= edgeLimit {
			result.Truncated = true
		}
		nextFrontier := make([]string, 0)
		for _, edge := range edges {
			candidates := [][2]string{}
			if _, ok := frontierSet[edge.SourceID]; ok {
				candidates = append(candidates, [2]string{edge.SourceID, edge.RelatedSourceID})
			}
			if _, ok := frontierSet[edge.RelatedSourceID]; ok {
				candidates = append(candidates, [2]string{edge.RelatedSourceID, edge.SourceID})
			}
			for _, candidate := range candidates {
				currentID := strings.TrimSpace(candidate[0])
				nextID := strings.TrimSpace(candidate[1])
				if currentID == "" || nextID == "" {
					continue
				}
				if _, ok := visited[nextID]; ok {
					continue
				}
				visited[nextID] = struct{}{}
				parent[nextID] = currentID
				parentStep[nextID] = SourcePathStep{
					FromSourceID: currentID,
					ToSourceID:   nextID,
					Relation:     firstNonEmpty(edge.Relation, SourceRelationTopicRelated),
					Score:        edge.Score,
					Terms:        append([]string(nil), edge.Terms...),
					Evidence:     append([]string(nil), edge.Evidence...),
				}
				if nextID == toSourceID {
					found = true
					break
				}
				nextFrontier = append(nextFrontier, nextID)
			}
			if found {
				break
			}
		}
		frontier = nextFrontier
	}
	result.VisitedCount = len(visited)
	if !found && len(frontier) > 0 {
		result.Truncated = true
	}
	if !found {
		return result, nil
	}
	pathIDs := []string{toSourceID}
	for current := toSourceID; current != fromSourceID; {
		prev := parent[current]
		if prev == "" {
			return result, nil
		}
		pathIDs = append(pathIDs, prev)
		current = prev
	}
	for i, j := 0, len(pathIDs)-1; i < j; i, j = i+1, j-1 {
		pathIDs[i], pathIDs[j] = pathIDs[j], pathIDs[i]
	}
	result.Found = true
	result.HopCount = len(pathIDs) - 1
	result.Nodes = make([]SourceGraphNode, 0, len(pathIDs))
	for _, id := range pathIDs {
		node, err := s.sourcePathNode(ctx, id)
		if err != nil {
			return SourcePathResult{}, err
		}
		result.Nodes = append(result.Nodes, node)
	}
	result.Steps = make([]SourcePathStep, 0, len(pathIDs)-1)
	for i := 1; i < len(pathIDs); i++ {
		result.Steps = append(result.Steps, parentStep[pathIDs[i]])
	}
	return result, nil
}

func (s *SQLiteStore) sourcePathNode(ctx context.Context, sourceID string) (SourceGraphNode, error) {
	source, err := s.GetSource(ctx, sourceID)
	if err != nil {
		return SourceGraphNode{}, err
	}
	return sourceGraphNode(source), nil
}

func (s *SQLiteStore) sourceNeighborIDs(ctx context.Context, sourceIDs []string, limit int) ([]string, error) {
	sourceIDs = normalizeSearchStrings(sourceIDs)
	if len(sourceIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(sourceIDs)), ",")
	args := make([]interface{}, 0, len(sourceIDs)*2+1)
	for _, id := range sourceIDs {
		args = append(args, id)
	}
	for _, id := range sourceIDs {
		args = append(args, id)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT source_id, related_source_id
		FROM knowledge_source_links
		WHERE source_id IN (`+placeholders+`) OR related_source_id IN (`+placeholders+`)
		ORDER BY score DESC, updated_at DESC
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sourceSet := make(map[string]struct{}, len(sourceIDs))
	for _, id := range sourceIDs {
		sourceSet[id] = struct{}{}
	}
	result := make([]string, 0)
	seen := map[string]struct{}{}
	for rows.Next() {
		var left, right string
		if err := rows.Scan(&left, &right); err != nil {
			return nil, err
		}
		candidate := ""
		if _, ok := sourceSet[left]; ok {
			candidate = right
		} else if _, ok := sourceSet[right]; ok {
			candidate = left
		}
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		result = append(result, candidate)
	}
	return result, rows.Err()
}

func (s *SQLiteStore) sourceNeighborEdges(ctx context.Context, sourceIDs []string, limit int) ([]SourceGraphEdge, error) {
	sourceIDs = normalizeSearchStrings(sourceIDs)
	if len(sourceIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		limit = 5000
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(sourceIDs)), ",")
	args := make([]interface{}, 0, len(sourceIDs)*2+1)
	for _, id := range sourceIDs {
		args = append(args, id)
	}
	for _, id := range sourceIDs {
		args = append(args, id)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT source_id, related_source_id, relation, score, COALESCE(terms_json, '[]'), COALESCE(evidence_json, '[]')
		FROM knowledge_source_links
		WHERE source_id IN (`+placeholders+`) OR related_source_id IN (`+placeholders+`)
		ORDER BY score DESC, updated_at DESC
		LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	edges := make([]SourceGraphEdge, 0)
	seen := map[string]struct{}{}
	for rows.Next() {
		var edge SourceGraphEdge
		var termsJSON, evidenceJSON string
		if err := rows.Scan(&edge.SourceID, &edge.RelatedSourceID, &edge.Relation, &edge.Score, &termsJSON, &evidenceJSON); err != nil {
			return nil, err
		}
		if edge.SourceID == "" || edge.RelatedSourceID == "" || edge.SourceID == edge.RelatedSourceID {
			continue
		}
		key := sourceGraphEdgeKey(edge.SourceID, edge.RelatedSourceID, edge.Relation)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		edge.ID = key
		_ = json.Unmarshal([]byte(termsJSON), &edge.Terms)
		_ = json.Unmarshal([]byte(evidenceJSON), &edge.Evidence)
		edge.Terms = uniqueTrimmed(edge.Terms)
		edge.Evidence = uniqueTrimmed(edge.Evidence)
		edges = append(edges, edge)
	}
	return edges, rows.Err()
}

func annotateSourceGraphComponents(result *SourceGraphResult) {
	if result == nil || len(result.Nodes) == 0 {
		return
	}
	nodeIndex := make(map[string]int, len(result.Nodes))
	adjacency := make(map[string][]string, len(result.Nodes))
	for i := range result.Nodes {
		nodeIndex[result.Nodes[i].ID] = i
		adjacency[result.Nodes[i].ID] = nil
	}
	for _, edge := range result.Edges {
		if edge.SourceID == "" || edge.RelatedSourceID == "" {
			continue
		}
		if _, ok := nodeIndex[edge.SourceID]; !ok {
			continue
		}
		if _, ok := nodeIndex[edge.RelatedSourceID]; !ok {
			continue
		}
		adjacency[edge.SourceID] = append(adjacency[edge.SourceID], edge.RelatedSourceID)
		adjacency[edge.RelatedSourceID] = append(adjacency[edge.RelatedSourceID], edge.SourceID)
	}
	visited := map[string]bool{}
	components := make([]SourceGraphComponent, 0)
	componentForNode := map[string]int{}
	for _, node := range result.Nodes {
		if visited[node.ID] {
			continue
		}
		stack := []string{node.ID}
		visited[node.ID] = true
		memberIDs := make([]string, 0)
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			memberIDs = append(memberIDs, id)
			for _, next := range adjacency[id] {
				if visited[next] {
					continue
				}
				visited[next] = true
				stack = append(stack, next)
			}
		}
		componentID := len(components) + 1
		for _, id := range memberIDs {
			componentForNode[id] = componentID
		}
		component := sourceGraphComponent(componentID, memberIDs, result.Nodes, nodeIndex, result.Edges)
		components = append(components, component)
		if component.Count > result.LargestComponentSize {
			result.LargestComponentSize = component.Count
		}
	}
	for i := range result.Nodes {
		result.Nodes[i].ComponentID = componentForNode[result.Nodes[i].ID]
	}
	sort.SliceStable(components, func(i, j int) bool {
		if components[i].Count != components[j].Count {
			return components[i].Count > components[j].Count
		}
		if components[i].EdgeCount != components[j].EdgeCount {
			return components[i].EdgeCount > components[j].EdgeCount
		}
		return components[i].ID < components[j].ID
	})
	result.Components = components
	result.ComponentCount = len(components)
	result.Notes = append(result.Notes, "local_connected_components")
}

func sourceGraphComponent(id int, memberIDs []string, nodes []SourceGraphNode, nodeIndex map[string]int, edges []SourceGraphEdge) SourceGraphComponent {
	memberSet := make(map[string]struct{}, len(memberIDs))
	for _, memberID := range memberIDs {
		memberSet[memberID] = struct{}{}
	}
	edgeCount := 0
	termCounts := map[string]int{}
	for _, edge := range edges {
		if _, ok := memberSet[edge.SourceID]; !ok {
			continue
		}
		if _, ok := memberSet[edge.RelatedSourceID]; !ok {
			continue
		}
		edgeCount++
		for _, term := range edge.Terms {
			term = strings.TrimSpace(term)
			if term != "" {
				termCounts[term]++
			}
		}
	}
	sort.SliceStable(memberIDs, func(i, j int) bool {
		left := nodes[nodeIndex[memberIDs[i]]]
		right := nodes[nodeIndex[memberIDs[j]]]
		if left.Degree != right.Degree {
			return left.Degree > right.Degree
		}
		return left.Label < right.Label
	})
	topNodeIDs := append([]string(nil), memberIDs...)
	if len(topNodeIDs) > 5 {
		topNodeIDs = topNodeIDs[:5]
	}
	topLabels := make([]string, 0, len(topNodeIDs))
	for _, memberID := range topNodeIDs {
		label := nodes[nodeIndex[memberID]].Label
		if label != "" {
			topLabels = append(topLabels, label)
		}
	}
	component := SourceGraphComponent{
		ID:            id,
		Count:         len(memberIDs),
		EdgeCount:     edgeCount,
		AverageDegree: 0,
		TopNodeIDs:    topNodeIDs,
		TopLabels:     topLabels,
		Terms:         topGraphTerms(termCounts, 8),
		Isolated:      len(memberIDs) == 1 && edgeCount == 0,
	}
	if component.Count > 0 {
		component.AverageDegree = float64(edgeCount*2) / float64(component.Count)
	}
	if component.Count > 1 {
		component.Density = float64(edgeCount) / (float64(component.Count*(component.Count-1)) / 2)
	}
	return component
}

func topGraphTerms(counts map[string]int, limit int) []string {
	if limit <= 0 || len(counts) == 0 {
		return nil
	}
	terms := make([]string, 0, len(counts))
	for term := range counts {
		terms = append(terms, term)
	}
	sort.SliceStable(terms, func(i, j int) bool {
		if counts[terms[i]] != counts[terms[j]] {
			return counts[terms[i]] > counts[terms[j]]
		}
		return terms[i] < terms[j]
	})
	if len(terms) > limit {
		terms = terms[:limit]
	}
	return terms
}

func insertSourceLinkTx(ctx context.Context, tx *sql.Tx, link SourceLink) error {
	if link.SourceID == "" || link.RelatedSourceID == "" || link.SourceID == link.RelatedSourceID {
		return nil
	}
	relation := strings.TrimSpace(link.Relation)
	if relation == "" {
		relation = SourceRelationTopicRelated
	}
	now := time.Now().UTC()
	if link.CreatedAt.IsZero() {
		link.CreatedAt = now
	}
	if link.UpdatedAt.IsZero() {
		link.UpdatedAt = now
	}
	termsJSON, _ := json.Marshal(uniqueTrimmed(link.Terms))
	evidenceJSON, _ := json.Marshal(uniqueTrimmed(link.Evidence))
	_, err := tx.ExecContext(ctx, `INSERT INTO knowledge_source_links(source_id, related_source_id, relation, score, terms_json, evidence_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_id, related_source_id, relation) DO UPDATE SET
			score = excluded.score,
			terms_json = excluded.terms_json,
			evidence_json = excluded.evidence_json,
			updated_at = excluded.updated_at`,
		link.SourceID, link.RelatedSourceID, relation, link.Score, string(termsJSON), string(evidenceJSON), formatTime(link.CreatedAt), formatTime(link.UpdatedAt))
	return err
}

func insertSourceLinkEventTx(ctx context.Context, tx *sql.Tx, link SourceLink, action string, note string) error {
	if link.SourceID == "" || link.RelatedSourceID == "" || link.SourceID == link.RelatedSourceID {
		return nil
	}
	relation := strings.TrimSpace(link.Relation)
	if relation == "" {
		relation = SourceRelationTopicRelated
	}
	action = strings.TrimSpace(action)
	if action == "" {
		action = "link"
	}
	termsJSON, _ := json.Marshal(uniqueTrimmed(link.Terms))
	evidenceJSON, _ := json.Marshal(uniqueTrimmed(link.Evidence))
	now := time.Now().UTC()
	_, err := tx.ExecContext(ctx, `INSERT INTO knowledge_source_link_events(id, source_id, related_source_id, relation, action, score, terms_json, evidence_json, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		NewID("ksle"), link.SourceID, link.RelatedSourceID, relation, action, link.Score, string(termsJSON), string(evidenceJSON), strings.TrimSpace(note), formatTime(now))
	return err
}

func insertSourceLinkEventRecordTx(ctx context.Context, tx *sql.Tx, event SourceLinkEvent) error {
	if event.ID == "" || event.SourceID == "" || event.RelatedSourceID == "" || event.SourceID == event.RelatedSourceID {
		return nil
	}
	relation := strings.TrimSpace(event.Relation)
	if relation == "" {
		relation = SourceRelationTopicRelated
	}
	action := strings.TrimSpace(event.Action)
	if action == "" {
		action = "link"
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	termsJSON, _ := json.Marshal(uniqueTrimmed(event.Terms))
	evidenceJSON, _ := json.Marshal(uniqueTrimmed(event.Evidence))
	_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO knowledge_source_link_events(id, source_id, related_source_id, relation, action, score, terms_json, evidence_json, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.SourceID, event.RelatedSourceID, relation, action, event.Score, string(termsJSON), string(evidenceJSON), strings.TrimSpace(event.Note), formatTime(event.CreatedAt))
	return err
}

func scanSourceLink(row interface {
	Scan(dest ...interface{}) error
}) (SourceLink, error) {
	var link SourceLink
	var termsJSON, evidenceJSON string
	var createdAt, updatedAt string
	var source Source
	var publishedAt, fetchedAt, sourceCreatedAt, sourceUpdatedAt string
	err := row.Scan(&link.SourceID, &link.RelatedSourceID, &link.Relation, &link.Score, &termsJSON, &evidenceJSON, &createdAt, &updatedAt,
		&source.ID, &source.Kind, &source.URI, &source.CanonicalURI, &source.Title, &source.Author, &source.SiteName, &publishedAt, &fetchedAt, &source.ContentHash,
		&source.OwnerID, &source.TenantID, &source.ProjectPath, &source.TopicHint, &source.SourceTrust, &source.BatchID, &source.RelativePath,
		&source.Status, &source.ErrorMessage, &sourceCreatedAt, &sourceUpdatedAt)
	if err != nil {
		return SourceLink{}, err
	}
	_ = json.Unmarshal([]byte(termsJSON), &link.Terms)
	_ = json.Unmarshal([]byte(evidenceJSON), &link.Evidence)
	link.CreatedAt = parseTime(createdAt)
	link.UpdatedAt = parseTime(updatedAt)
	source.PublishedAt = parseTime(publishedAt)
	source.FetchedAt = parseTime(fetchedAt)
	source.CreatedAt = parseTime(sourceCreatedAt)
	source.UpdatedAt = parseTime(sourceUpdatedAt)
	link.RelatedSource = source
	return link, nil
}

func scanSourceLinkEvent(row interface {
	Scan(dest ...interface{}) error
}) (SourceLinkEvent, error) {
	var event SourceLinkEvent
	var termsJSON, evidenceJSON, createdAt string
	err := row.Scan(&event.ID, &event.SourceID, &event.RelatedSourceID, &event.Relation, &event.Action, &event.Score, &termsJSON, &evidenceJSON, &event.Note, &createdAt)
	if err != nil {
		return SourceLinkEvent{}, err
	}
	_ = json.Unmarshal([]byte(termsJSON), &event.Terms)
	_ = json.Unmarshal([]byte(evidenceJSON), &event.Evidence)
	event.Terms = uniqueTrimmed(event.Terms)
	event.Evidence = uniqueTrimmed(event.Evidence)
	event.CreatedAt = parseTime(createdAt)
	return event, nil
}

func sourceLinkEvidence(item TopicRelevanceSource) []string {
	evidence := make([]string, 0, 4)
	if item.SourceHits > 0 {
		evidence = append(evidence, fmt.Sprintf("source:%d", item.SourceHits))
	}
	if len(item.LabelMatches) > 0 {
		evidence = append(evidence, "labels:"+strings.Join(item.LabelMatches, ","))
	}
	if item.CardHits > 0 {
		evidence = append(evidence, fmt.Sprintf("cards:%d", item.CardHits))
	}
	if item.FactHits > 0 {
		evidence = append(evidence, fmt.Sprintf("facts:%d", item.FactHits))
	}
	if item.NodeHits > 0 {
		evidence = append(evidence, fmt.Sprintf("nodes:%d", item.NodeHits))
	}
	return evidence
}

func sourceGraphNode(source Source) SourceGraphNode {
	return SourceGraphNode{
		ID:           source.ID,
		Label:        firstNonEmpty(source.Title, source.RelativePath, source.CanonicalURI, source.URI, source.ID),
		Kind:         source.Kind,
		Status:       source.Status,
		TopicHint:    source.TopicHint,
		ProjectPath:  source.ProjectPath,
		Labels:       append([]string(nil), source.Labels...),
		NodeCount:    source.NodeCount,
		CardCount:    source.CardCount,
		FactCount:    source.FactCount,
		SourceTrust:  source.SourceTrust,
		RelativePath: source.RelativePath,
		URI:          firstNonEmpty(source.CanonicalURI, source.URI),
	}
}

func sourceGraphEdgeKey(left, right, relation string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	relation = strings.TrimSpace(relation)
	if relation == "" {
		relation = SourceRelationTopicRelated
	}
	if right < left {
		left, right = right, left
	}
	return left + ":" + right + ":" + relation
}
