package guiapp

import (
	"encoding/json"
	"log"
	"strings"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// noteLegacySQLDataRead records a table read that ran outside the planned
// selection. The turn keeps a count and reapplies it to the current plan,
// including a child revision published by a later petition.
func (c *sharedAgentLoopCallbacks) noteLegacySQLDataRead(name, argsJSON string, result agent.ToolExecutionResult) {
	if c == nil || !legacySQLDatabaseToolName(name) {
		return
	}
	action, cursor := legacySQLCall(argsJSON)
	switch action {
	case "query", "read_table":
	default:
		return
	}
	if !legacySQLReadBodySucceeded(result.Result) {
		return
	}
	// A cursor page is the rest of a query already counted. Counting it
	// again closes the next required read. The first page of a turn that
	// only continues a handle still counts, so that turn can finish.
	if action == "query" && cursor != "" && c.sqlDataReads > 0 {
		return
	}
	c.sqlDataReads++
	c.sqlReadCoveredPlan = ""
	c.settleOpenBusinessDataRead()
}

// settleOpenBusinessDataRead closes unstarted required business.data.read
// selections, one per recorded read. Reads the plan already completed, and
// failed or unknown records, are left alone. A second call on the same plan
// does not spend the same read again.
func (c *sharedAgentLoopCallbacks) settleOpenBusinessDataRead() {
	if c == nil || c.sqlDataReads == 0 || c.semanticSurface == nil {
		return
	}
	planID := c.semanticSurface.plan.ID
	if planID != "" && c.sqlReadCoveredPlan == planID {
		return
	}
	// A recorded plan already stored which selections are required.
	// Rebuilding the classification map repeats work the planner finished.
	var optional map[string]bool
	var optionalKnown bool
	if !c.semanticSurface.plan.ObligationsRecorded {
		optional, optionalKnown = semanticSurfaceOptionalNeeds(c.semanticSurface)
	}
	satisfied := 0
	retryable := false
	var open []tool.PlannedSelection
	for _, selection := range c.semanticSurface.plan.Selections {
		if selection.FitProof.MatchedCapability != tool.CapabilityBusinessDataRead {
			continue
		}
		if !hopSelectionRequired(selection, optional, optionalKnown, c.semanticSurface.plan.ObligationsRecorded) {
			continue
		}
		// The in-memory bit is already a trusted success. Asking the store
		// again only repeats a miss for a selection this turn just closed.
		if c.semanticSurface.completed[selection.ID] {
			satisfied++
			continue
		}
		state := hopSelectionEvidence(c.semanticSurface, selection.ID).State
		if state == tool.PlanExecutionSucceeded {
			satisfied++
			continue
		}
		if state != "" {
			// Failed stays failed. Unknown or in-flight can still become
			// unstarted, so this plan is not finished until that resolves.
			if state != tool.PlanExecutionFailed {
				retryable = true
			}
			continue
		}
		open = append(open, selection)
	}
	need := c.sqlDataReads - satisfied
	if need <= 0 || len(open) == 0 {
		c.noteSQLReadCovered(planID, retryable)
		return
	}
	if need > len(open) {
		need = len(open)
	}
	if c.semanticSurface.completed == nil {
		c.semanticSurface.completed = map[string]bool{}
	}
	for i := 0; i < need; i++ {
		c.semanticSurface.completed[open[i].ID] = true
		c.rememberSettledSQLRead(open[i].ID)
		log.Printf("[semantic] legacy SQL read settled plan selection %s", open[i].ID)
	}
	c.noteSQLReadCovered(planID, retryable)
}

func (c *sharedAgentLoopCallbacks) noteSQLReadCovered(planID string, retryable bool) {
	if c == nil || retryable || planID == "" {
		return
	}
	c.sqlReadCoveredPlan = planID
}

// rememberSettledSQLRead keeps the snapshot from reading the store for a
// selection that has no execution row. A later hop evidence lookup hits this
// cache and still reports success.
func (c *sharedAgentLoopCallbacks) rememberSettledSQLRead(selectionID string) {
	if c == nil || c.semanticSurface == nil || selectionID == "" {
		return
	}
	if c.semanticSurface.hopEvidence == nil {
		c.semanticSurface.hopEvidence = map[string]HopStepEvidence{}
	}
	key := hopEvidenceKey(c.semanticSurface, selectionID)
	if _, ok := c.semanticSurface.hopEvidence[key]; ok {
		return
	}
	c.semanticSurface.hopEvidence[key] = HopStepEvidence{State: tool.PlanExecutionSucceeded}
}

func legacySQLCall(argsJSON string) (action, cursor string) {
	// Match the database tool's map parse. A null or non-string cursor is
	// absent there; decoding it into a string fails the whole object and
	// drops a query that did return rows.
	var args map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &args) != nil {
		return "", ""
	}
	action, _ = args["action"].(string)
	cursor, _ = args["cursor"].(string)
	return strings.ToLower(strings.TrimSpace(action)), strings.TrimSpace(cursor)
}

// legacySQLReadBodySucceeded accepts a query payload, including one whose
// cells contain the word "failed". It rejects a leading adapter error and a
// top-level error object. A preview counts only when it carries a row count.
func legacySQLReadBodySucceeded(result string) bool {
	text := strings.TrimSpace(result)
	if text == "" {
		return false
	}
	first, _, _ := strings.Cut(text, "\n")
	first = strings.TrimSpace(first)
	lowerFirst := strings.ToLower(first)
	if strings.HasPrefix(lowerFirst, "[system rejected]") || strings.HasPrefix(lowerFirst, "error:") {
		return false
	}
	if strings.HasPrefix(lowerFirst, "database ") && strings.Contains(lowerFirst, " failed:") {
		return false
	}
	if strings.HasPrefix(text, "{") && legacySQLTopLevelRejects(text) {
		return false
	}
	return legacySQLHasRowCount(text)
}

// legacySQLTopLevelRejects reads the object head, before the column list.
// ok:false there is the adapter's failure object. A later cell is not.
func legacySQLTopLevelRejects(text string) bool {
	head := text
	if i := strings.Index(text, `"columns"`); i > 1 {
		head = text[:i]
	} else if len(head) > 512 {
		head = head[:512]
	}
	return strings.Contains(head, `"ok":false`) || strings.Contains(head, `"ok": false`)
}

func legacySQLHasRowCount(text string) bool {
	if legacySQLQuotedRowCount(text) {
		return true
	}
	return legacySQLPlainRowCount(text)
}

func legacySQLQuotedRowCount(text string) bool {
	const marker = `"row_count":`
	for text != "" {
		idx := strings.LastIndex(text, marker)
		if idx < 0 {
			return false
		}
		if legacySQLDigitsFollow(text[idx+len(marker):]) {
			return true
		}
		text = text[:idx]
	}
	return false
}

func legacySQLPlainRowCount(text string) bool {
	const marker = "row_count:"
	start := 0
	for start < len(text) {
		rel := strings.Index(text[start:], marker)
		if rel < 0 {
			return false
		}
		idx := start + rel
		start = idx + len(marker)
		if idx > 0 && text[idx-1] == '"' {
			continue
		}
		if legacySQLDigitsFollow(text[idx+len(marker):]) {
			return true
		}
	}
	return false
}

func legacySQLDigitsFollow(rest string) bool {
	rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
	return rest != "" && rest[0] >= '0' && rest[0] <= '9'
}
