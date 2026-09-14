package knowledge

import (
	"context"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/factclaim"
)

const (
	maxVerifiedFactClaimRunes     = 500
	maxVerifiedFactInPlaceRunes   = 500
	maxVerifiedFactLookupHits     = 40
	verifiedFactReplacementKind = SourceKindText
	verifiedFactReplacementHint = "verified_fact"
)

// VerifiedFactRequest is a tool-verified claim that should replace
// contradicting knowledge cards and agent-saved text sources.
type VerifiedFactRequest struct {
	Entity           string
	Claim            string
	Evidence         string
	OwnerID          string
	TenantID         string
	ProjectPath      string
	Aliases          []string
	Predicate        string
	StrictOwner      bool
	ExcludeSourceIDs []string
	// NoInsert skips writing a new replacement source. Set when the caller
	// already stored the verified claim (for example SaveText).
	NoInsert bool
}

// VerifiedFactSyncResult reports knowledge mutations for a verified claim.
type VerifiedFactSyncResult struct {
	Rewritten  int
	Suppressed int
	Inserted   bool
}

type verifiedFactHit struct {
	CardID    string
	SourceID  string
	Kind      string
	Title     string
	Text      string
	CreatedAt time.Time
}

// ApplyVerifiedFact replaces a live (subject, predicate) assertion with a
// newly verified value. Short agent-saved notes are rewritten in place.
// Distilled facts on immutable documents are invalidated so search no longer
// returns them; imported document text is left unchanged.
func (s *SQLiteStore) ApplyVerifiedFact(ctx context.Context, req VerifiedFactRequest) (VerifiedFactSyncResult, error) {
	var out VerifiedFactSyncResult
	if s == nil {
		return out, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	claim := strings.TrimSpace(req.Claim)
	if claim == "" || utf8.RuneCountInString(claim) > maxVerifiedFactClaimRunes {
		return out, nil
	}
	newer := claimsFromVerifiedRequest(req)
	if len(newer) == 0 {
		return out, nil
	}
	needles := needlesFromVerifiedClaims(newer)
	if len(needles) == 0 {
		return out, nil
	}
	exclude := verifiedFactExcludeSet(req.ExcludeSourceIDs)
	replacement := strings.TrimSpace(claim)
	if ev := strings.TrimSpace(req.Evidence); ev != "" {
		replacement = claim + "\n（已核实: " + ev + "）"
	}

	// Identity is (subject, predicate). Live fact rows are the source of truth;
	// short agent-saved notes are rewritten so their distilled triples stay in sync.
	rows, err := s.findLiveContradictingFacts(ctx, req, newer, needles, exclude)
	if err != nil {
		return out, err
	}
	rewritten := map[string]struct{}{}
	var invalidateIDs []string
	var invalidateCards []string
	for _, row := range rows {
		if _, done := rewritten[row.SourceID]; done {
			continue
		}
		hit := verifiedFactHit{CardID: row.CardID, SourceID: row.SourceID, Kind: row.Kind, Title: row.Title}
		inPlace, inPlaceErr := s.verifiedFactSourceInPlaceEligible(ctx, hit, needles, newer)
		if inPlaceErr != nil {
			return out, inPlaceErr
		}
		if inPlace && knowledgeSourceKindRewritable(row.Kind) {
			if req.NoInsert {
				if err := s.DeleteSource(ctx, row.SourceID); err != nil {
					return out, err
				}
			} else {
				ok, rewriteErr := s.rewriteVerifiedFactSource(ctx, hit, req, replacement)
				if rewriteErr != nil {
					return out, rewriteErr
				}
				if !ok {
					inPlace = false
				}
			}
			if inPlace {
				rewritten[row.SourceID] = struct{}{}
				out.Rewritten++
				continue
			}
		}
		invalidateIDs = append(invalidateIDs, row.ID)
		if knowledgeSourceKindRewritable(row.Kind) && row.CardID != "" {
			stored := factclaim.Extract(strings.TrimSpace(row.Claim + " " + row.Subject + " " + row.Object))
			if factclaim.Isolated(newer, stored) {
				invalidateCards = append(invalidateCards, row.CardID)
			}
		}
	}

	// Legacy short notes may have a card claim but no distilled triple yet.
	if err := s.rewriteShortContradictingNotes(ctx, req, newer, needles, exclude, rewritten, replacement, &out); err != nil {
		return out, err
	}

	if n, err := s.invalidateFacts(ctx, invalidateIDs, invalidateCards); err != nil {
		return out, err
	} else {
		out.Suppressed = n
	}
	if req.NoInsert || out.Rewritten > 0 || len(invalidateIDs) == 0 {
		return out, nil
	}
	title := strings.TrimSpace(claim)
	if runes := []rune(title); len(runes) > 80 {
		title = string(runes[:80])
	}
	_, insertErr := s.SaveText(ctx, TextSaveRequest{
		Text:        replacement,
		Title:       title,
		Kind:        verifiedFactReplacementKind,
		OwnerID:     strings.TrimSpace(req.OwnerID),
		TenantID:    strings.TrimSpace(req.TenantID),
		ProjectPath: strings.TrimSpace(req.ProjectPath),
		TopicHint:   verifiedFactReplacementHint,
		DistillMode: DistillModeRules,
	})
	if insertErr != nil {
		return out, insertErr
	}
	out.Inserted = true
	return out, nil
}

func (s *SQLiteStore) syncVerifiedTextFact(ctx context.Context, source Source, req TextSaveRequest) {
	if s == nil {
		return
	}
	if len(factclaim.Extract(req.Text)) == 0 {
		return
	}
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	syncCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	ownerID := firstNonEmpty(req.OwnerID, source.OwnerID)
	tenantID := firstNonEmpty(req.TenantID, source.TenantID)
	_, _ = s.ApplyVerifiedFact(syncCtx, VerifiedFactRequest{
		Claim:            req.Text,
		OwnerID:          ownerID,
		TenantID:         tenantID,
		ProjectPath:      firstNonEmpty(req.ProjectPath, source.ProjectPath),
		StrictOwner:      strings.TrimSpace(tenantID) != "",
		ExcludeSourceIDs: []string{source.ID},
		NoInsert:         true,
	})
}

func (s *SQLiteStore) rewriteVerifiedFactSource(ctx context.Context, hit verifiedFactHit, req VerifiedFactRequest, replacement string) (bool, error) {
	source, err := s.GetSource(ctx, hit.SourceID)
	if err != nil {
		return false, err
	}
	title := strings.TrimSpace(source.Title)
	if title == "" {
		title = strings.TrimSpace(hit.Title)
	}
	_, err = s.SaveText(ctx, TextSaveRequest{
		Text:           replacement,
		Title:          title,
		Kind:           firstNonEmpty(source.Kind, verifiedFactReplacementKind),
		OwnerID:        firstNonEmpty(req.OwnerID, source.OwnerID),
		TenantID:       firstNonEmpty(req.TenantID, source.TenantID),
		ProjectPath:    firstNonEmpty(req.ProjectPath, source.ProjectPath),
		TopicHint:      firstNonEmpty(source.TopicHint, verifiedFactReplacementHint),
		DistillMode:    DistillModeRules,
		ForceID:        source.ID,
		ForceCreatedAt: source.CreatedAt,
	})
	return err == nil, err
}

type liveFactRow struct {
	ID       string
	CardID   string
	SourceID string
	Kind     string
	Title    string
	Subject  string
	Predicate string
	Object   string
	Claim    string
}

func (s *SQLiteStore) findLiveContradictingFacts(ctx context.Context, req VerifiedFactRequest, newer []factclaim.Claim, needles []string, exclude map[string]struct{}) ([]liveFactRow, error) {
	if len(needles) == 0 {
		return nil, nil
	}
	where, args := s.verifiedFactScope("s", req)
	needleOr := make([]string, 0, len(needles)*3)
	for _, n := range needles {
		needleOr = append(needleOr, "instr(lower(COALESCE(f.subject, '')), ?) > 0", "instr(lower(COALESCE(f.object, '')), ?) > 0", "instr(lower(COALESCE(c.claim, '')), ?) > 0")
		ln := strings.ToLower(n)
		args = append(args, ln, ln, ln)
	}
	query := `SELECT f.id, f.card_id, f.source_id, COALESCE(s.kind, ''), COALESCE(s.title, ''),
		COALESCE(f.subject, ''), COALESCE(f.predicate, ''), COALESCE(f.object, ''), COALESCE(c.claim, '')
		FROM knowledge_facts f
		JOIN knowledge_cards c ON c.id = f.card_id
		JOIN knowledge_sources s ON s.id = f.source_id
		WHERE COALESCE(f.invalid_at, '') = '' AND ` + where + `
		AND (` + strings.Join(needleOr, " OR ") + `)
		LIMIT ?`
	args = append(args, maxVerifiedFactLookupHits)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []liveFactRow
	seen := map[string]struct{}{}
	for rows.Next() {
		var row liveFactRow
		if err := rows.Scan(&row.ID, &row.CardID, &row.SourceID, &row.Kind, &row.Title, &row.Subject, &row.Predicate, &row.Object, &row.Claim); err != nil {
			return nil, err
		}
		if _, skip := exclude[row.SourceID]; skip {
			continue
		}
		if _, dup := seen[row.ID]; dup {
			continue
		}
		blob := strings.TrimSpace(row.Subject + " " + row.Predicate + " " + row.Object + " " + row.Claim)
		if !factclaim.ContradictsAny(newer, blob) {
			continue
		}
		seen[row.ID] = struct{}{}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) rewriteShortContradictingNotes(ctx context.Context, req VerifiedFactRequest, newer []factclaim.Claim, needles []string, exclude map[string]struct{}, rewritten map[string]struct{}, replacement string, out *VerifiedFactSyncResult) error {
	var hits []verifiedFactHit
	seen := map[string]struct{}{}
	for _, needle := range needles {
		part, err := s.lookupVerifiedFactHits(ctx, req, needle)
		if err != nil {
			return err
		}
		for _, hit := range part {
			if _, skip := exclude[hit.SourceID]; skip {
				continue
			}
			if _, done := rewritten[hit.SourceID]; done {
				continue
			}
			if _, dup := seen[hit.SourceID]; dup {
				continue
			}
			if hit.CardID == "" || !knowledgeSourceKindRewritable(hit.Kind) {
				continue
			}
			seen[hit.SourceID] = struct{}{}
			hits = append(hits, hit)
		}
	}
	for _, hit := range hits {
		inPlace, err := s.verifiedFactSourceInPlaceEligible(ctx, hit, needles, newer)
		if err != nil {
			return err
		}
		if !inPlace {
			continue
		}
		if req.NoInsert {
			if err := s.DeleteSource(ctx, hit.SourceID); err != nil {
				return err
			}
		} else {
			ok, err := s.rewriteVerifiedFactSource(ctx, hit, req, replacement)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
		}
		rewritten[hit.SourceID] = struct{}{}
		out.Rewritten++
	}
	return nil
}

func (s *SQLiteStore) invalidateFacts(ctx context.Context, factIDs, cardIDs []string) (int, error) {
	factIDs = uniqueTrimmed(factIDs)
	cardIDs = uniqueTrimmed(cardIDs)
	if len(factIDs) == 0 && len(cardIDs) == 0 {
		return 0, nil
	}
	now := formatTime(time.Now().UTC())
	n := 0
	for _, id := range factIDs {
		res, err := s.db.ExecContext(ctx, `UPDATE knowledge_facts SET invalid_at = ? WHERE id = ? AND COALESCE(invalid_at, '') = ''`, now, id)
		if err != nil {
			return n, err
		}
		affected, _ := res.RowsAffected()
		n += int(affected)
	}
	for _, id := range cardIDs {
		if _, err := s.db.ExecContext(ctx, `UPDATE knowledge_cards SET invalid_at = ? WHERE id = ? AND COALESCE(invalid_at, '') = ''`, now, id); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (s *SQLiteStore) verifiedFactScope(alias string, req VerifiedFactRequest) (string, []interface{}) {
	where := []string{alias + ".status <> ?"}
	args := []interface{}{StatusDisabled}
	owner := strings.TrimSpace(req.OwnerID)
	tenant := strings.TrimSpace(req.TenantID)
	project := strings.TrimSpace(req.ProjectPath)
	if owner != "" {
		if req.StrictOwner {
			where = append(where, "COALESCE("+alias+".owner_id, '') = ?")
		} else {
			where = append(where, "COALESCE("+alias+".owner_id, '') IN ('', ?)")
		}
		args = append(args, owner)
	} else {
		where = append(where, "COALESCE("+alias+".owner_id, '') = ''")
	}
	if tenant != "" {
		if req.StrictOwner {
			where = append(where, "COALESCE("+alias+".tenant_id, '') = ?")
		} else {
			where = append(where, "COALESCE("+alias+".tenant_id, '') IN ('', ?)")
		}
		args = append(args, tenant)
	}
	if project != "" {
		where = append(where, "COALESCE("+alias+".project_path, '') IN ('', ?)")
		args = append(args, project)
	}
	return strings.Join(where, " AND "), args
}

func (s *SQLiteStore) verifiedFactSourceInPlaceEligible(ctx context.Context, hit verifiedFactHit, needles []string, newer []factclaim.Claim) (bool, error) {
	body, err := s.sourcePlainText(ctx, hit.SourceID)
	if err != nil {
		return false, err
	}
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > maxVerifiedFactInPlaceRunes {
		return false, nil
	}
	stored := factclaim.Extract(body)
	if !factclaim.ContradictsStored(newer, stored) && !factclaim.ContradictsAny(newer, body) {
		return false, nil
	}
	if !factclaim.Isolated(newer, stored) {
		return false, nil
	}
	return true, nil
}

func claimsFromVerifiedRequest(req VerifiedFactRequest) []factclaim.Claim {
	out := factclaim.Extract(req.Claim)
	if strings.TrimSpace(req.Entity) == "" {
		return out
	}
	c := factclaim.Claim{
		Subject:   strings.TrimSpace(req.Entity),
		Predicate: strings.TrimSpace(req.Predicate),
		Text:      strings.TrimSpace(req.Claim),
		Aliases:   append([]string(nil), req.Aliases...),
	}
	if c.Predicate == "" {
		if pol := factclaim.DetectPolarity(req.Claim + "\n" + req.Evidence); pol != factclaim.PolarityUnknown {
			c.Predicate = factclaim.PredicateReachability
			switch pol {
			case factclaim.PolarityReachable:
				c.Value = "reachable"
			case factclaim.PolarityUnreachable:
				c.Value = "unreachable"
			}
		} else {
			return out
		}
	}
	if c.Value == "" {
		c.Value = req.Claim
	}
	return append([]factclaim.Claim{c}, out...)
}

func needlesFromVerifiedClaims(claims []factclaim.Claim) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(raw string) {
		n := canonicalVerifiedFactNeedle(raw, "")
		if n == "" {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	for _, c := range claims {
		add(c.Subject)
		add(strings.TrimPrefix(strings.ToLower(c.Subject), "spo:"))
		for _, a := range c.Aliases {
			add(a)
		}
		for _, e := range factclaim.Entities(c.Text) {
			add(e)
		}
	}
	return out
}

func (s *SQLiteStore) sourcePlainText(ctx context.Context, sourceID string) (string, error) {
	nodes, err := s.ListNodesBySource(ctx, sourceID, 32)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, node := range nodes {
		text := strings.TrimSpace(node.Text)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(text)
	}
	return b.String(), nil
}

func (s *SQLiteStore) lookupVerifiedFactHits(ctx context.Context, req VerifiedFactRequest, needle string) ([]verifiedFactHit, error) {
	scope, scopeArgs := s.verifiedFactScope("s", req)
	ln := strings.ToLower(strings.TrimSpace(needle))
	args := []interface{}{ln, ln, ln, ln}
	args = append(args, scopeArgs...)
	args = append(args, maxVerifiedFactLookupHits)
	query := `SELECT c.id, c.source_id, COALESCE(s.kind, ''), COALESCE(s.title, ''),
		COALESCE(c.claim, '') || ' ' || COALESCE(c.summary, '') || ' ' || COALESCE(f.subject, '') || ' ' || COALESCE(f.predicate, '') || ' ' || COALESCE(f.object, ''),
		s.created_at
		FROM knowledge_cards c
		JOIN knowledge_sources s ON s.id = c.source_id
		LEFT JOIN knowledge_facts f ON f.card_id = c.id AND COALESCE(f.invalid_at, '') = ''
		WHERE (instr(lower(COALESCE(c.claim, '')), ?) > 0 OR instr(lower(COALESCE(c.summary, '')), ?) > 0
			OR instr(lower(COALESCE(f.subject, '')), ?) > 0 OR instr(lower(COALESCE(f.object, '')), ?) > 0)
		AND COALESCE(c.invalid_at, '') = ''
		AND NOT EXISTS (SELECT 1 FROM knowledge_card_suppressions kcs WHERE kcs.card_id = c.id)
		AND ` + scope + `
		ORDER BY s.updated_at DESC
		LIMIT ?`
	return s.scanVerifiedFactHits(ctx, query, args)
}

func (s *SQLiteStore) scanVerifiedFactHits(ctx context.Context, query string, args []interface{}) ([]verifiedFactHit, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	var hits []verifiedFactHit
	for rows.Next() {
		var hit verifiedFactHit
		var createdAt string
		if err := rows.Scan(&hit.CardID, &hit.SourceID, &hit.Kind, &hit.Title, &hit.Text, &createdAt); err != nil {
			return nil, err
		}
		key := hit.CardID + "\x00" + hit.SourceID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		hit.CreatedAt = parseTime(createdAt)
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}

func verifiedKnowledgeNeedles(entity, claim string, aliases []string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(raw string) {
		n := canonicalVerifiedFactNeedle(raw, "")
		if n == "" {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	add(entity)
	for _, alias := range aliases {
		add(alias)
	}
	for _, extracted := range agent.SessionFactEntities(claim) {
		add(extracted)
	}
	if len(out) == 0 {
		if n := canonicalVerifiedFactNeedle(entity, claim); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func knowledgeTextSharesAnyNeedle(text string, needles []string) bool {
	for _, needle := range needles {
		if knowledgeTextSharesNeedle(text, needle) {
			return true
		}
	}
	return false
}

func canonicalVerifiedFactNeedle(entity, claim string) string {
	entity = strings.TrimSpace(entity)
	lower := strings.ToLower(entity)
	switch {
	case strings.HasPrefix(lower, "ip:"):
		entity = strings.TrimSpace(entity[3:])
	case strings.HasPrefix(lower, "host:"):
		entity = strings.ToLower(strings.TrimSpace(entity[5:]))
	}
	if entity == "" {
		if extracted, ok := agent.ExtractSessionFactFromMemoryContent(claim); ok {
			return canonicalVerifiedFactNeedle(extracted.Entity, "")
		}
	}
	return strings.ToLower(strings.TrimSpace(entity))
}

func knowledgeSourceKindRewritable(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case SourceKindText, SourceKindConversation, SourceKindWorkflowArtifact, SourceKindMarkdown, "":
		return true
	default:
		return false
	}
}

func knowledgeTextSharesNeedle(text, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" || strings.TrimSpace(text) == "" {
		return false
	}
	if parsed := net.ParseIP(needle); parsed != nil && parsed.To4() != nil {
		want := parsed.To4().String()
		for _, entity := range agent.SessionFactEntities(text) {
			if strings.TrimPrefix(strings.ToLower(entity), "ip:") == want {
				return true
			}
		}
		return false
	}
	lower := strings.ToLower(text)
	start := 0
	for {
		idx := strings.Index(lower[start:], needle)
		if idx < 0 {
			return false
		}
		idx += start
		if hostBodyBoundary(lower, idx, len(needle)) {
			return true
		}
		start = idx + 1
	}
}

func hostBodyBoundary(lower string, idx, nlen int) bool {
	if idx > 0 && isHostBodyByte(lower[idx-1]) {
		return false
	}
	end := idx + nlen
	if end < len(lower) && isHostBodyByte(lower[end]) {
		return false
	}
	return true
}

func isHostBodyByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '-'
}

func uniqueVerifiedFactSources(hits []verifiedFactHit) []verifiedFactHit {
	seen := make(map[string]struct{}, len(hits))
	out := make([]verifiedFactHit, 0, len(hits))
	for _, hit := range hits {
		if strings.TrimSpace(hit.SourceID) == "" {
			continue
		}
		if _, ok := seen[hit.SourceID]; ok {
			continue
		}
		seen[hit.SourceID] = struct{}{}
		out = append(out, hit)
	}
	return out
}

func verifiedFactExcludeSet(ids []string) map[string]struct{} {
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out[id] = struct{}{}
	}
	return out
}
