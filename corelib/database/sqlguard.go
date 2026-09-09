package database

import (
	"fmt"
	"strings"
)

// StatementClass is the conservative classification used by policy checks.
type StatementClass string

const (
	StatementRead     StatementClass = "read"
	StatementDML      StatementClass = "dml"
	StatementDDL      StatementClass = "ddl"
	StatementSession  StatementClass = "session"
	StatementExternal StatementClass = "external"
	StatementUnknown  StatementClass = "unknown"
)

type GuardResult struct {
	Class StatementClass
	SQL   string // comments/literals are preserved; this is never executed
}

// sqlScanDialect selects which lexical conventions the shared scanners honor.
// Dialect values match the adapter dialects in manager.go ("postgres",
// "mysql", "sqlserver", "access") plus "sqlite" used by the excel facade.
type sqlScanDialect struct {
	bracketQuote bool // [name] identifier quoting (sqlserver/access/sqlite)
	dollarQuote  bool // $$...$$ / $tag$...$tag$ string literals (postgres)
	backslashEsc bool // \' and friends escape inside '...'/"..." (mysql)
}

// scanDialect maps a dialect name onto scanner behavior. An empty or unknown
// dialect is deliberately conservative and keeps treating '[' as a quote
// (the pre-dialect-aware behavior); PostgreSQL and MySQL must be named
// explicitly, because '[' is an array/subscript operator there, not a quote.
func scanDialect(dialect string) sqlScanDialect {
	switch strings.ToLower(strings.TrimSpace(dialect)) {
	case "postgres", "postgresql":
		return sqlScanDialect{dollarQuote: true}
	case "mysql", "mariadb":
		return sqlScanDialect{backslashEsc: true}
	default:
		// sqlserver, access, sqlite and the unknown/empty fallback.
		return sqlScanDialect{bracketQuote: true}
	}
}

// GuardSQL performs a deliberately conservative lexical gate. It is not a
// SQL parser: syntax it cannot classify is rejected by callers (fail closed).
// It is equivalent to GuardSQLDialect with an unknown dialect, which keeps
// the historical conservative behavior (brackets treated as quotes).
func GuardSQL(sqlText string) (GuardResult, error) {
	return GuardSQLDialect(sqlText, "")
}

// GuardSQLDialect is GuardSQL with explicit dialect awareness. In dialects
// where '[' is not an identifier quote (postgres, mysql) it is scanned as an
// ordinary character, so jsonb/array subscripts such as j['a]b'] no longer
// desynchronize the scanners; PostgreSQL dollar-quoted strings ($$...$$,
// $tag$...$tag$) are recognized as literals, and MySQL backslash escapes are
// honored inside string literals.
func GuardSQLDialect(sqlText, dialect string) (GuardResult, error) {
	d := scanDialect(dialect)
	if strings.TrimSpace(sqlText) == "" {
		return GuardResult{}, fmt.Errorf("syntax: empty SQL")
	}
	if hasSQLCommentDialect(sqlText, d) {
		return GuardResult{}, fmt.Errorf("syntax: comments are not allowed")
	}
	clean, err := stripSQLComments(sqlText, d)
	if err != nil {
		return GuardResult{}, err
	}
	if hasTopLevelChar(clean, ';', d) {
		return GuardResult{}, fmt.Errorf("syntax: multiple statements are not allowed")
	}
	trimmed := strings.TrimSpace(strings.ToLower(clean))
	first := strings.Fields(trimmed)
	if len(first) == 0 {
		return GuardResult{}, fmt.Errorf("syntax: empty SQL")
	}
	class := StatementUnknown
	switch first[0] {
	case "select", "with", "show", "describe", "desc", "explain":
		class = StatementRead
	case "insert", "update", "delete", "merge":
		class = StatementDML
	case "create", "alter", "drop", "truncate", "grant", "revoke":
		class = StatementDDL
	case "set", "use", "reset", "begin", "commit", "rollback", "savepoint", "release":
		class = StatementSession
	case "exec", "execute", "call", "copy", "attach", "load", "openrowset":
		class = StatementExternal
	}
	if class == StatementUnknown {
		return GuardResult{}, fmt.Errorf("permission: unsupported or unknown SQL statement")
	}
	// EXPLAIN ANALYZE (keyword or parenthesized option form) actually executes
	// the explained statement on PostgreSQL, so EXPLAIN <DML> would bypass the
	// read-only gate. Reject any EXPLAIN that requests ANALYZE.
	if class == StatementRead && first[0] == "explain" && explainRequestsAnalyze(clean, d) {
		return GuardResult{}, fmt.Errorf("permission: EXPLAIN ANALYZE executes the statement and is not allowed")
	}
	// A WITH prefix only proves the CTE list exists; the statement that follows
	// it may be DML (WITH x AS (SELECT 1) DELETE FROM t), and any CTE body may
	// itself be data-modifying (WITH x AS (DELETE ...) SELECT ...). Parse the
	// whole WITH prefix once, requiring read-only CTE bodies and a read-only
	// main statement, failing closed when the prefix cannot be parsed.
	if class == StatementRead && first[0] == "with" {
		main, dataModifying, ok := parseWithStatement(clean, d)
		if !ok {
			return GuardResult{}, fmt.Errorf("syntax: unable to parse WITH statement safely")
		}
		if dataModifying {
			return GuardResult{}, fmt.Errorf("permission: data-modifying CTE is not allowed in query")
		}
		switch main {
		case "select", "values", "table":
		default:
			return GuardResult{}, fmt.Errorf("permission: WITH main statement must be read-only, got %q", main)
		}
	}
	// SELECT INTO writes out a new table (or exfiltrates via INTO OUTFILE);
	// treat it as external so both the query and execute paths refuse it.
	if class == StatementRead && hasTopLevelKeyword(clean, "into", d) {
		class = StatementExternal
	}
	return GuardResult{Class: class, SQL: clean}, nil
}

// skipQuotedAt reports whether offset i opens a quoted region (string literal,
// quoted identifier, bracket identifier, or — for PostgreSQL — a
// dollar-quoted string) under dialect d. When it does, next is the index just
// past the closing delimiter, or len(sqlText) when the region is unterminated
// (closed=false); scanning callers treat the tail as quoted, stripping
// callers fail closed.
func skipQuotedAt(sqlText string, i int, d sqlScanDialect) (next int, isQuote bool, closed bool) {
	c := sqlText[i]
	if c == '$' && d.dollarQuote {
		return skipDollarQuoted(sqlText, i)
	}
	switch c {
	case '\'', '"', '`':
	case '[':
		if !d.bracketQuote {
			return 0, false, false
		}
	default:
		return 0, false, false
	}
	end := c
	if c == '[' {
		end = ']'
	}
	for j := i + 1; j < len(sqlText); j++ {
		if d.backslashEsc && (c == '\'' || c == '"') && sqlText[j] == '\\' {
			j++ // a backslash-escaped character never terminates the literal
			continue
		}
		if sqlText[j] == end {
			if j+1 < len(sqlText) && sqlText[j+1] == end {
				j++ // doubled delimiter is an escaped literal delimiter
				continue
			}
			return j + 1, true, true
		}
	}
	return len(sqlText), true, false
}

// skipDollarQuoted consumes a PostgreSQL dollar-quoted string starting at i
// ($$...$$ or $tag$...$tag$). The tag follows unquoted-identifier rules (so
// $1-style parameter placeholders are never mistaken for quotes); the body is
// scanned literally with no escape processing, exactly like the server.
func skipDollarQuoted(sqlText string, i int) (next int, isQuote bool, closed bool) {
	j := i + 1
	for j < len(sqlText) && isIdentByte(sqlText[j]) {
		j++
	}
	if j >= len(sqlText) || sqlText[j] != '$' {
		return 0, false, false
	}
	if tag := sqlText[i+1 : j]; tag != "" && tag[0] >= '0' && tag[0] <= '9' {
		return 0, false, false // $1 parameter placeholder, not a quote
	}
	delim := sqlText[i : j+1]
	if k := strings.Index(sqlText[j+1:], delim); k >= 0 {
		return j + 1 + k + len(delim), true, true
	}
	return len(sqlText), true, false
}

// HasTopLevelKeyword reports whether keyword appears at parenthesis depth 0
// after comments and quoted literals/identifiers have been stripped, matched
// case-insensitively with ASCII word boundaries. It is the building block for
// policy checks such as the mandatory-WHERE enforcement: keywords inside
// subqueries or literals no longer count. Any strip failure returns false,
// which fails closed for "must contain keyword" checks.
func HasTopLevelKeyword(sqlText, dialect, keyword string) bool {
	d := scanDialect(dialect)
	clean, err := stripSQLComments(sqlText, d)
	if err != nil {
		return false
	}
	stripped, err := stripSQLLiteralsDialect(clean, d)
	if err != nil {
		return false
	}
	depth := 0
	for i := 0; i < len(stripped); i++ {
		switch stripped[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && matchKeywordAt(stripped, i, keyword) {
				return true
			}
		}
	}
	return false
}

// hasTopLevelKeyword reports whether keyword appears at parenthesis depth 0,
// outside quoted literals/identifiers, with ASCII word boundaries and
// case-insensitive matching. Comments must be stripped beforehand.
func hasTopLevelKeyword(sqlText, keyword string, d sqlScanDialect) bool {
	depth := 0
	for i := 0; i < len(sqlText); i++ {
		if next, isQuote, _ := skipQuotedAt(sqlText, i, d); isQuote {
			i = next - 1
			continue
		}
		switch sqlText[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && matchKeywordAt(sqlText, i, keyword) {
				return true
			}
		}
	}
	return false
}

// matchKeywordAt reports whether keyword starts at offset i with ASCII word
// boundaries on both sides, matched case-insensitively.
func matchKeywordAt(sqlText string, i int, keyword string) bool {
	if i > 0 && isIdentByte(sqlText[i-1]) {
		return false
	}
	if i+len(keyword) > len(sqlText) {
		return false
	}
	if !strings.EqualFold(sqlText[i:i+len(keyword)], keyword) {
		return false
	}
	return i+len(keyword) == len(sqlText) || !isIdentByte(sqlText[i+len(keyword)])
}

func isIdentByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func hasTopLevelChar(sqlText string, target byte, d sqlScanDialect) bool {
	for i := 0; i < len(sqlText); i++ {
		if next, isQuote, _ := skipQuotedAt(sqlText, i, d); isQuote {
			i = next - 1
			continue
		}
		if sqlText[i] == target {
			return true
		}
	}
	return false
}

// hasTopLevelKeywordPair detects a clause without treating text inside quoted
// literals/identifiers as SQL keywords. It is intentionally small and only
// used for consistency warnings, never as an authorization decision.
func hasTopLevelKeywordPair(sqlText, first, second string) bool {
	d := scanDialect("")
	clean, err := stripSQLComments(sqlText, d)
	if err != nil {
		return false
	}
	words := make([]string, 0, 8)
	for i := 0; i < len(clean); {
		if next, isQuote, closed := skipQuotedAt(clean, i, d); isQuote {
			if !closed {
				return false
			}
			i = next
			continue
		}
		if isIdentByte(clean[i]) && (clean[i] < '0' || clean[i] > '9') {
			j := i + 1
			for j < len(clean) && isIdentByte(clean[j]) {
				j++
			}
			words = append(words, strings.ToLower(clean[i:j]))
			i = j
			continue
		}
		i++
	}
	for i := 0; i+1 < len(words); i++ {
		if words[i] == strings.ToLower(first) && words[i+1] == strings.ToLower(second) {
			return true
		}
	}
	return false
}

// hasSQLComment keeps the pre-dialect behavior (unknown dialect, brackets
// treated as quotes) for existing callers.
func hasSQLComment(sqlText string) bool {
	return hasSQLCommentDialect(sqlText, scanDialect(""))
}

func hasSQLCommentDialect(sqlText string, d sqlScanDialect) bool {
	for i := 0; i+1 < len(sqlText); i++ {
		if next, isQuote, _ := skipQuotedAt(sqlText, i, d); isQuote {
			i = next - 1
			continue
		}
		if (sqlText[i] == '-' && sqlText[i+1] == '-') || (sqlText[i] == '/' && sqlText[i+1] == '*') {
			return true
		}
	}
	return false
}

func stripSQLComments(sqlText string, d sqlScanDialect) (string, error) {
	var b strings.Builder
	for i := 0; i < len(sqlText); {
		if next, isQuote, closed := skipQuotedAt(sqlText, i, d); isQuote {
			if !closed {
				return "", fmt.Errorf("syntax: unterminated quoted literal")
			}
			b.WriteString(sqlText[i:next])
			i = next
			continue
		}
		c := sqlText[i]
		if c == '-' && i+1 < len(sqlText) && sqlText[i+1] == '-' {
			for i < len(sqlText) && sqlText[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
			continue
		}
		if c == '/' && i+1 < len(sqlText) && sqlText[i+1] == '*' {
			i += 2
			closed := false
			for i+1 < len(sqlText) {
				if sqlText[i] == '*' && sqlText[i+1] == '/' {
					i += 2
					closed = true
					break
				}
				i++
			}
			if !closed {
				return "", fmt.Errorf("syntax: unterminated comment")
			}
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), nil
}

// explainRequestsAnalyze reports whether an EXPLAIN statement carries the
// ANALYZE option in any position (EXPLAIN ANALYZE ..., EXPLAIN (ANALYZE) ...,
// EXPLAIN (ANALYZE true) ...), case-insensitively and ignoring quoted regions.
// A bare identifier named "analyze" also trips the detector; that false
// positive fails closed, which is the safe direction for a read gate.
func explainRequestsAnalyze(sqlText string, d sqlScanDialect) bool {
	for i := 0; i < len(sqlText); {
		if next, isQuote, _ := skipQuotedAt(sqlText, i, d); isQuote {
			i = next
			continue
		}
		if isIdentByte(sqlText[i]) && (sqlText[i] < '0' || sqlText[i] > '9') {
			j := i + 1
			for j < len(sqlText) && isIdentByte(sqlText[j]) {
				j++
			}
			if strings.EqualFold(sqlText[i:j], "analyze") {
				return true
			}
			i = j
			continue
		}
		i++
	}
	return false
}

// parseWithStatement parses a WITH prefix — WITH [RECURSIVE] name [(columns)]
// AS (...), ... — in a single pass. It returns the lowercased keyword of the
// statement that follows the CTE list and whether any CTE body begins with a
// data-modifying keyword (insert/update/delete/merge), detected by parsing
// the body opener rather than by substring matching, so AS( with no space,
// column lists and multiple CTEs cannot slip past. Quoted identifiers/
// literals and nested parentheses are skipped correctly; any parse failure
// returns ok=false and callers must fail closed.
func parseWithStatement(sqlText string, d sqlScanDialect) (main string, dataModifying bool, ok bool) {
	i := 0
	skipSpace := func() {
		for i < len(sqlText) && (sqlText[i] == ' ' || sqlText[i] == '\t' || sqlText[i] == '\r' || sqlText[i] == '\n') {
			i++
		}
	}
	readIdent := func() (string, bool) {
		skipSpace()
		if i >= len(sqlText) {
			return "", false
		}
		c := sqlText[i]
		if c == '"' || c == '`' || (c == '[' && d.bracketQuote) {
			end := c
			if end == '[' {
				end = ']'
			}
			i++
			start := i
			for i < len(sqlText) && sqlText[i] != end {
				i++
			}
			if i >= len(sqlText) {
				return "", false
			}
			name := sqlText[start:i]
			i++
			return name, true
		}
		if !isIdentByte(c) || (c >= '0' && c <= '9') {
			return "", false
		}
		start := i
		for i < len(sqlText) && isIdentByte(sqlText[i]) {
			i++
		}
		return sqlText[start:i], true
	}
	// skipParens consumes a parenthesized group starting at the current
	// position, honoring nested parens and quoted regions.
	skipParens := func() bool {
		skipSpace()
		if i >= len(sqlText) || sqlText[i] != '(' {
			return false
		}
		depth := 0
		for i < len(sqlText) {
			if next, isQuote, closed := skipQuotedAt(sqlText, i, d); isQuote {
				if !closed {
					return false
				}
				i = next
				continue
			}
			switch sqlText[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					i++
					return true
				}
			}
			i++
		}
		return false
	}
	word, ok2 := readIdent()
	if !ok2 || !strings.EqualFold(word, "with") {
		return "", false, false
	}
	// Optional RECURSIVE modifier; rewind when the next token is a CTE name.
	save := i
	if word, ok2 = readIdent(); !ok2 {
		return "", false, false
	} else if !strings.EqualFold(word, "recursive") {
		i = save
	}
	for {
		if _, ok2 := readIdent(); !ok2 { // CTE name
			return "", false, false
		}
		// Optional column list between the name and AS.
		skipSpace()
		if i < len(sqlText) && sqlText[i] == '(' {
			if !skipParens() {
				return "", false, false
			}
		}
		if word, ok2 = readIdent(); !ok2 || !strings.EqualFold(word, "as") {
			return "", false, false
		}
		// Peek at the first keyword of the CTE body to detect data-modifying
		// CTEs, then rewind and consume the whole parenthesized body.
		skipSpace()
		if i >= len(sqlText) || sqlText[i] != '(' {
			return "", false, false
		}
		bodyStart := i
		i++
		body, ok2 := readIdent()
		if !ok2 {
			return "", false, false // unreadable body opener: fail closed
		}
		switch strings.ToLower(body) {
		case "insert", "update", "delete", "merge":
			dataModifying = true
		}
		i = bodyStart
		if !skipParens() { // CTE body
			return "", false, false
		}
		skipSpace()
		if i < len(sqlText) && sqlText[i] == ',' {
			i++
			continue
		}
		if word, ok2 = readIdent(); !ok2 {
			return "", false, false
		}
		return strings.ToLower(word), dataModifying, true
	}
}

// stripSQLLiterals replaces every quoted region — delimiters, contents and
// escapes included — with a single space, so top-level keyword detection
// cannot be fooled by literal contents. Comments must be stripped beforehand;
// an unterminated literal fails closed. This wrapper keeps the pre-dialect
// behavior (unknown dialect, brackets treated as quotes).
func stripSQLLiterals(sqlText string) (string, error) {
	return stripSQLLiteralsDialect(sqlText, scanDialect(""))
}

// stripSQLLiteralsDialect is stripSQLLiterals with dialect awareness:
// PostgreSQL dollar-quoted strings and MySQL backslash escapes are honored,
// and '[' only quotes in dialects where it is an identifier delimiter.
func stripSQLLiteralsDialect(sqlText string, d sqlScanDialect) (string, error) {
	var b strings.Builder
	for i := 0; i < len(sqlText); {
		if next, isQuote, closed := skipQuotedAt(sqlText, i, d); isQuote {
			if !closed {
				return "", fmt.Errorf("syntax: unterminated quoted literal")
			}
			b.WriteByte(' ')
			i = next
			continue
		}
		b.WriteByte(sqlText[i])
		i++
	}
	return b.String(), nil
}
