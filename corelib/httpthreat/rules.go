package httpthreat

import (
	"strings"
	"sync"
	"time"
)

type ruleHit struct {
	Class  string
	Source string
	RuleID string
	Level  int // 0=P0 ... 4=P4
}

type sessionCounter struct {
	fails int
	notf  int
	until time.Time
}

// RuleEngine is the teacher/guard. Strong hits win; P2 is attribution only.
type RuleEngine struct {
	mu      sync.Mutex
	intel   map[string]string
	session map[string]sessionCounter
	now     func() time.Time
}

func NewRuleEngine() *RuleEngine {
	return &RuleEngine{
		intel:   map[string]string{},
		session: map[string]sessionCounter{},
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func (e *RuleEngine) SetIntelHost(host, class string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.intel[normalizeHost(host)] = class
}

func (e *RuleEngine) NoteAuthFailure(tenant, source string) {
	if e == nil || strings.TrimSpace(source) == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	key := tenant + "|" + source
	cur := e.session[key]
	now := e.now()
	if now.After(cur.until) {
		cur = sessionCounter{}
	}
	cur.fails++
	cur.until = now.Add(10 * time.Minute)
	e.session[key] = cur
	e.pruneSessionsLocked(now)
}

func (e *RuleEngine) NoteNotFound(tenant, source string) {
	if e == nil || strings.TrimSpace(source) == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	key := tenant + "|" + source
	cur := e.session[key]
	now := e.now()
	if now.After(cur.until) {
		cur = sessionCounter{}
	}
	cur.notf++
	cur.until = now.Add(10 * time.Minute)
	e.session[key] = cur
	e.pruneSessionsLocked(now)
}

func (e *RuleEngine) pruneSessionsLocked(now time.Time) {
	if e == nil || len(e.session) <= 4096 {
		return
	}
	for key, cur := range e.session {
		if now.After(cur.until) {
			delete(e.session, key)
		}
	}
}

func (e *RuleEngine) Classify(tx Transaction) ruleHit {
	blob := strings.ToLower(tx.Method + " " + tx.Host + " " + tx.Path + " " + tx.Query + " " + tx.Body + " " + tx.UserAgent)
	if hit := p0Signature(blob, tx); hit.Class != "" {
		return hit
	}
	if e != nil {
		e.mu.Lock()
		if class := e.intel[normalizeHost(tx.Host)]; class != "" {
			e.mu.Unlock()
			return ruleHit{Class: class, Source: SourceIntel, RuleID: "p1.intel.host", Level: 1}
		}
		key := tx.TenantID + "|" + tx.SourceID
		cur := e.session[key]
		now := e.now()
		if tx.SourceID != "" && now.Before(cur.until) && cur.fails >= 20 {
			e.mu.Unlock()
			return ruleHit{Class: ClassAuthAbuse, Source: SourceSession, RuleID: "p2.session.authfail", Level: 2}
		}
		if tx.SourceID != "" && now.Before(cur.until) && cur.notf >= 40 {
			e.mu.Unlock()
			return ruleHit{Class: ClassScan, Source: SourceSession, RuleID: "p2.session.notfound", Level: 2}
		}
		e.mu.Unlock()
	}
	if hit := p3Heuristic(blob, tx); hit.Class != "" {
		return hit
	}
	return ruleHit{Class: ClassBenign, Source: SourceFallback, RuleID: "p4.fallback", Level: 4}
}

func p0Signature(blob string, tx Transaction) ruleHit {
	switch {
	case strings.Contains(blob, "../etc/passwd"):
		return ruleHit{Class: ClassExploit, Source: SourceSignature, RuleID: "p0.lfi.passwd", Level: 0}
	case strings.Contains(blob, "union select"):
		return ruleHit{Class: ClassExploit, Source: SourceSignature, RuleID: "p0.sqli.union", Level: 0}
	case strings.Contains(blob, "${jndi:"):
		return ruleHit{Class: ClassExploit, Source: SourceSignature, RuleID: "p0.jndi", Level: 0}
	case strings.Contains(blob, "<script>alert"):
		return ruleHit{Class: ClassExploit, Source: SourceSignature, RuleID: "p0.xss.alert", Level: 0}
	case strings.Contains(strings.ToLower(tx.Path), "/wp-admin/install.php"):
		return ruleHit{Class: ClassExploit, Source: SourceSignature, RuleID: "p0.wp.install", Level: 0}
	case strings.Contains(blob, "mimikatz"):
		return ruleHit{Class: ClassMalware, Source: SourceSignature, RuleID: "p0.malware.mimikatz", Level: 0}
	case strings.Contains(blob, "cobaltstrike"):
		return ruleHit{Class: ClassMalware, Source: SourceSignature, RuleID: "p0.malware.cobalt", Level: 0}
	case strings.Contains(strings.ToLower(tx.UserAgent), "sqlmap"):
		return ruleHit{Class: ClassMalware, Source: SourceSignature, RuleID: "p0.malware.sqlmap", Level: 0}
	case strings.Contains(blob, "/exfil/") && strings.Contains(blob, "base64"):
		return ruleHit{Class: ClassExfil, Source: SourceSignature, RuleID: "p0.exfil.base64", Level: 0}
	}
	return ruleHit{}
}

func p3Heuristic(blob string, tx Transaction) ruleHit {
	q := strings.ToLower(tx.Query)
	if strings.Contains(q, "select=") || strings.Contains(q, "sleep(") {
		id := "p3.sqli.select"
		if strings.Contains(q, "sleep(") {
			id = "p3.sqli.sleep"
		}
		return ruleHit{Class: ClassExploit, Source: SourceHeuristic, RuleID: id, Level: 3}
	}
	if strings.Count(tx.Path, "/") >= 8 || strings.Contains(strings.ToLower(tx.Path), "/admin/") && strings.Contains(tx.Path, "..") {
		return ruleHit{Class: ClassScan, Source: SourceHeuristic, RuleID: "p3.scan.deep", Level: 3}
	}
	if strings.Contains(blob, "login") && strings.Contains(blob, "password=") {
		return ruleHit{Class: ClassAuthAbuse, Source: SourceHeuristic, RuleID: "p3.auth.login", Level: 3}
	}
	return ruleHit{}
}

func noteSession(rules *RuleEngine, tenant string, tx Transaction) {
	status := strings.TrimSpace(tx.Status)
	if status == "401" || status == "403" {
		rules.NoteAuthFailure(tenant, tx.SourceID)
	}
	if status == "404" {
		rules.NoteNotFound(tenant, tx.SourceID)
	}
}

func AutoGold(source string) bool {
	return source == SourceSignature || source == SourceIntel
}

func HeadMayScore(source string) bool {
	return source == SourceHeuristic || source == SourceFallback
}
