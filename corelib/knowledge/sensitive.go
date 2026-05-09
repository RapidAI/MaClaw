package knowledge

import (
	"context"
	"regexp"
	"strings"
)

var sensitivePatterns = []struct {
	kind     string
	severity string
	re       *regexp.Regexp
}{
	{"private_key", "error", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"aws_access_key", "error", regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"github_token", "error", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{30,}\b`)},
	{"openai_api_key", "error", regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}\b`)},
	{"slack_token", "warning", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{20,}\b`)},
	{"password_assignment", "warning", regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key)\s*[:=]\s*['"]?[^'"\s]{8,}`)},
	{"bearer_token", "warning", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{20,}`)},
}

func (s *SQLiteStore) ScanSensitiveContent(ctx context.Context, limit int) (SensitiveScanResult, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	result := SensitiveScanResult{}
	if err := s.scanSensitiveNodes(ctx, limit, &result); err != nil {
		return result, err
	}
	if result.Count < limit {
		if err := s.scanSensitiveCards(ctx, limit, &result); err != nil {
			return result, err
		}
	}
	result.Count = len(result.Findings)
	result.MaxSeverity = maxSensitiveSeverity(result.Findings)
	return result, nil
}

func (s *SQLiteStore) DisableSensitiveSources(ctx context.Context, limit int) (SensitiveIsolationResult, error) {
	scan, err := s.ScanSensitiveContent(ctx, limit)
	if err != nil {
		return SensitiveIsolationResult{Scan: scan}, err
	}
	sourceIDs := make([]string, 0, len(scan.Findings))
	seen := map[string]struct{}{}
	for _, finding := range scan.Findings {
		sourceID := strings.TrimSpace(finding.SourceID)
		if sourceID == "" {
			continue
		}
		if _, ok := seen[sourceID]; ok {
			continue
		}
		seen[sourceID] = struct{}{}
		sourceIDs = append(sourceIDs, sourceID)
	}
	return SensitiveIsolationResult{
		Scan:      scan,
		SourceIDs: sourceIDs,
		Update:    s.DisableSources(ctx, sourceIDs),
	}, nil
}

func (s *SQLiteStore) scanSensitiveNodes(ctx context.Context, limit int, result *SensitiveScanResult) error {
	rows, err := s.db.QueryContext(ctx, `SELECT n.id, n.source_id, COALESCE(n.text, ''), COALESCE(src.title, ''), COALESCE(src.relative_path, ''), COALESCE(src.uri, '')
		FROM document_nodes n JOIN knowledge_sources src ON src.id = n.source_id
		WHERE src.status <> ? ORDER BY src.updated_at DESC`, StatusDisabled)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID, sourceID, text, title, relativePath, uri string
		if err := rows.Scan(&nodeID, &sourceID, &text, &title, &relativePath, &uri); err != nil {
			return err
		}
		findSensitiveMatches(text, limit, result, SensitiveFinding{
			SourceID:     sourceID,
			SourceTitle:  title,
			RelativePath: relativePath,
			URI:          uri,
			NodeID:       nodeID,
			Field:        "node.text",
		})
		if len(result.Findings) >= limit {
			return nil
		}
	}
	return rows.Err()
}

func (s *SQLiteStore) scanSensitiveCards(ctx context.Context, limit int, result *SensitiveScanResult) error {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.source_id, COALESCE(c.claim, ''), COALESCE(c.summary, ''), COALESCE(src.title, ''), COALESCE(src.relative_path, ''), COALESCE(src.uri, '')
		FROM knowledge_cards c JOIN knowledge_sources src ON src.id = c.source_id
		LEFT JOIN knowledge_card_suppressions kcs ON kcs.card_id = c.id
		WHERE src.status <> ? AND kcs.card_id IS NULL ORDER BY c.updated_at DESC`, StatusDisabled)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cardID, sourceID, claim, summary, title, relativePath, uri string
		if err := rows.Scan(&cardID, &sourceID, &claim, &summary, &title, &relativePath, &uri); err != nil {
			return err
		}
		base := SensitiveFinding{SourceID: sourceID, SourceTitle: title, RelativePath: relativePath, URI: uri, CardID: cardID}
		base.Field = "card.claim"
		findSensitiveMatches(claim, limit, result, base)
		if len(result.Findings) >= limit {
			return nil
		}
		base.Field = "card.summary"
		findSensitiveMatches(summary, limit, result, base)
		if len(result.Findings) >= limit {
			return nil
		}
	}
	return rows.Err()
}

func findSensitiveMatches(text string, limit int, result *SensitiveScanResult, base SensitiveFinding) {
	if strings.TrimSpace(text) == "" || result == nil || len(result.Findings) >= limit {
		return
	}
	for _, pattern := range sensitivePatterns {
		matches := pattern.re.FindAllStringIndex(text, -1)
		for _, match := range matches {
			if len(result.Findings) >= limit {
				return
			}
			raw := text[match[0]:match[1]]
			finding := base
			finding.Kind = pattern.kind
			finding.Severity = pattern.severity
			finding.Redacted = redactSensitive(raw)
			finding.Snippet = sensitiveSnippet(text, match[0], match[1])
			result.Findings = append(result.Findings, finding)
		}
	}
}

func redactSensitive(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= 8 {
		return "****"
	}
	prefix := string(runes[:sensitiveMin(4, len(runes))])
	suffix := string(runes[sensitiveMax(0, len(runes)-4):])
	return prefix + "..." + suffix
}

func sensitiveSnippet(text string, start, end int) string {
	left := sensitiveMax(0, start-40)
	right := sensitiveMin(len(text), end+40)
	snippet := text[left:start] + redactSensitive(text[start:end]) + text[end:right]
	return truncateRunes(normalizeWhitespace(snippet), 180)
}

func sensitiveMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sensitiveMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxSensitiveSeverity(findings []SensitiveFinding) string {
	max := ""
	for _, finding := range findings {
		if finding.Severity == "error" {
			return "error"
		}
		if finding.Severity == "warning" {
			max = "warning"
		} else if max == "" && finding.Severity != "" {
			max = finding.Severity
		}
	}
	return max
}
