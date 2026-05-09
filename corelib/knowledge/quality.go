package knowledge

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *SQLiteStore) ListDuplicateCards(ctx context.Context, limit int) ([]DuplicateCardGroup, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.source_id, c.claim, COALESCE(c.project_path, ''), COALESCE(c.owner_id, ''), COALESCE(c.tenant_id, ''),
		COALESCE(s.title, ''), COALESCE(s.relative_path, ''), COALESCE(s.canonical_uri, ''), COALESCE(s.uri, '')
		FROM knowledge_cards c
		JOIN knowledge_sources s ON s.id = c.source_id
		LEFT JOIN knowledge_card_suppressions kcs ON kcs.card_id = c.id
		WHERE s.status <> ? AND kcs.card_id IS NULL
		ORDER BY c.updated_at DESC`, StatusDisabled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type cardRow struct {
		cardID      string
		sourceID    string
		claim       string
		projectPath string
		ownerID     string
		tenantID    string
		example     string
	}
	groups := make(map[string]*DuplicateCardGroup)
	for rows.Next() {
		var row cardRow
		var title, relativePath, canonicalURI, uri string
		if err := rows.Scan(&row.cardID, &row.sourceID, &row.claim, &row.projectPath, &row.ownerID, &row.tenantID, &title, &relativePath, &canonicalURI, &uri); err != nil {
			return nil, err
		}
		normalized := normalizeCardClaimKey(row.claim)
		if normalized == "" {
			continue
		}
		row.example = firstNonEmpty(title, relativePath, canonicalURI, uri, row.sourceID)
		key := strings.Join([]string{row.tenantID, row.ownerID, row.projectPath, normalized}, "\x00")
		group := groups[key]
		if group == nil {
			group = &DuplicateCardGroup{
				Key:         normalized,
				Claim:       strings.TrimSpace(row.claim),
				OwnerID:     row.ownerID,
				TenantID:    row.tenantID,
				ProjectPath: row.projectPath,
			}
			groups[key] = group
		}
		group.Count++
		group.CardIDs = appendLimited(group.CardIDs, row.cardID, 20)
		group.SourceIDs = appendUniqueLimited(group.SourceIDs, row.sourceID, 20)
		group.Examples = appendUniqueLimited(group.Examples, row.example, 5)
		if len([]rune(row.claim)) > len([]rune(group.Claim)) {
			group.Claim = strings.TrimSpace(row.claim)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]DuplicateCardGroup, 0)
	for _, group := range groups {
		if group.Count > 1 {
			result = append(result, *group)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Claim < result[j].Claim
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *SQLiteStore) SuppressDuplicateCards(ctx context.Context, req DuplicateCardSuppressionRequest) (CardSuppressionResult, error) {
	key := normalizeDuplicateSuppressionKey(req.Key)
	if key == "" {
		return CardSuppressionResult{}, fmt.Errorf("duplicate key is required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.source_id, c.claim, COALESCE(c.project_path, ''), COALESCE(c.owner_id, ''), COALESCE(c.tenant_id, ''),
		COALESCE(c.confidence, 0), COALESCE(c.importance, 0), COALESCE(c.source_trust, 0), c.updated_at
		FROM knowledge_cards c
		JOIN knowledge_sources s ON s.id = c.source_id
		LEFT JOIN knowledge_card_suppressions kcs ON kcs.card_id = c.id
		WHERE s.status <> ? AND kcs.card_id IS NULL`, StatusDisabled)
	if err != nil {
		return CardSuppressionResult{}, err
	}
	defer rows.Close()

	matches := make([]duplicateCardCandidate, 0)
	for rows.Next() {
		var item duplicateCardCandidate
		if err := rows.Scan(&item.cardID, &item.sourceID, &item.claim, &item.projectPath, &item.ownerID, &item.tenantID, &item.confidence, &item.importance, &item.sourceTrust, &item.updatedAt); err != nil {
			return CardSuppressionResult{}, err
		}
		if req.ProjectPath != "" && item.projectPath != req.ProjectPath {
			continue
		}
		if req.OwnerID != "" && item.ownerID != req.OwnerID {
			continue
		}
		if req.TenantID != "" && item.tenantID != req.TenantID {
			continue
		}
		if normalizeCardClaimKey(item.claim) == key {
			matches = append(matches, item)
		}
	}
	if err := rows.Err(); err != nil {
		return CardSuppressionResult{}, err
	}
	if len(matches) <= 1 {
		return CardSuppressionResult{KeptCardID: firstCandidateCardID(matches)}, nil
	}
	keepID := strings.TrimSpace(req.KeepCardID)
	if keepID == "" || !candidateContainsCard(matches, keepID) {
		sort.SliceStable(matches, func(i, j int) bool {
			left := matches[i].importance + matches[i].sourceTrust + matches[i].confidence
			right := matches[j].importance + matches[j].sourceTrust + matches[j].confidence
			if left != right {
				return left > right
			}
			return matches[i].updatedAt > matches[j].updatedAt
		})
		keepID = matches[0].cardID
	}
	cardIDs := make([]string, 0, len(matches)-1)
	for _, item := range matches {
		if item.cardID != keepID {
			cardIDs = append(cardIDs, item.cardID)
		}
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "duplicate_card_claim:" + key
	}
	result, err := s.SuppressCards(ctx, cardIDs, reason)
	if err != nil {
		return CardSuppressionResult{}, err
	}
	result.KeptCardID = keepID
	return result, nil
}

func (s *SQLiteStore) SuppressCards(ctx context.Context, cardIDs []string, reason string) (CardSuppressionResult, error) {
	cardIDs = uniqueTrimmed(cardIDs)
	if len(cardIDs) == 0 {
		return CardSuppressionResult{}, nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "manual"
	}
	now := formatTime(time.Now().UTC())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CardSuppressionResult{}, err
	}
	suppressed := 0
	for _, cardID := range cardIDs {
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO knowledge_card_suppressions(card_id, reason, created_at)
			SELECT id, ?, ? FROM knowledge_cards WHERE id = ?`, reason, now, cardID)
		if err != nil {
			_ = tx.Rollback()
			return CardSuppressionResult{}, err
		}
		affected, _ := res.RowsAffected()
		suppressed += int(affected)
	}
	if err := tx.Commit(); err != nil {
		return CardSuppressionResult{}, err
	}
	items, err := s.ListSuppressedCards(ctx, len(cardIDs))
	if err != nil {
		return CardSuppressionResult{}, err
	}
	return CardSuppressionResult{Suppressed: suppressed, CardIDs: cardIDs, Items: filterSuppressedItems(items, cardIDs)}, nil
}

func (s *SQLiteStore) RestoreSuppressedCards(ctx context.Context, cardIDs []string) (CardSuppressionResult, error) {
	cardIDs = uniqueTrimmed(cardIDs)
	if len(cardIDs) == 0 {
		return CardSuppressionResult{}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CardSuppressionResult{}, err
	}
	restored := 0
	for _, cardID := range cardIDs {
		res, err := tx.ExecContext(ctx, `DELETE FROM knowledge_card_suppressions WHERE card_id = ?`, cardID)
		if err != nil {
			_ = tx.Rollback()
			return CardSuppressionResult{}, err
		}
		affected, _ := res.RowsAffected()
		restored += int(affected)
	}
	if err := tx.Commit(); err != nil {
		return CardSuppressionResult{}, err
	}
	return CardSuppressionResult{Restored: restored, CardIDs: cardIDs}, nil
}

func (s *SQLiteStore) ListSuppressedCards(ctx context.Context, limit int) ([]CardSuppression, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT kcs.card_id, COALESCE(c.source_id, ''), COALESCE(c.claim, ''),
		COALESCE(src.title, ''), COALESCE(src.relative_path, ''), COALESCE(kcs.reason, ''), kcs.created_at
		FROM knowledge_card_suppressions kcs
		LEFT JOIN knowledge_cards c ON c.id = kcs.card_id
		LEFT JOIN knowledge_sources src ON src.id = c.source_id
		ORDER BY kcs.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CardSuppression, 0)
	for rows.Next() {
		var item CardSuppression
		var createdAt string
		if err := rows.Scan(&item.CardID, &item.SourceID, &item.Claim, &item.SourceTitle, &item.RelativePath, &item.Reason, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func normalizeCardClaimKey(claim string) string {
	claim = strings.ToLower(strings.TrimSpace(claim))
	if claim == "" {
		return ""
	}
	parts := strings.FieldsFunc(claim, func(r rune) bool {
		return r <= ' ' || strings.ContainsRune(".,;:!?()[]{}\"'`~|/\\+-_=*#<>，。；：！？（）【】《》、“”‘’", r)
	})
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			terms = append(terms, part)
		}
	}
	normalized := strings.Join(terms, " ")
	if len([]rune(normalized)) < 20 {
		return ""
	}
	return normalized
}

func normalizeDuplicateSuppressionKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if normalized := normalizeCardClaimKey(value); normalized != "" {
		return normalized
	}
	return strings.ToLower(value)
}

type duplicateCardCandidate struct {
	cardID      string
	sourceID    string
	claim       string
	projectPath string
	ownerID     string
	tenantID    string
	confidence  float64
	importance  float64
	sourceTrust float64
	updatedAt   string
}

func firstCandidateCardID(items []duplicateCardCandidate) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].cardID
}

func candidateContainsCard(items []duplicateCardCandidate, cardID string) bool {
	for _, item := range items {
		if item.cardID == cardID {
			return true
		}
	}
	return false
}

func uniqueTrimmed(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func filterSuppressedItems(items []CardSuppression, cardIDs []string) []CardSuppression {
	allowed := make(map[string]struct{}, len(cardIDs))
	for _, cardID := range cardIDs {
		allowed[cardID] = struct{}{}
	}
	result := make([]CardSuppression, 0, len(items))
	for _, item := range items {
		if _, ok := allowed[item.CardID]; ok {
			result = append(result, item)
		}
	}
	return result
}

func appendUniqueLimited(values []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" || len(values) >= limit {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
