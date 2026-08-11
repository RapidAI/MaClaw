package enterpriseknowledge

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// AppendAutoRecall injects enterprise digital-asset hits (access_state=active only)
// into the system prompt. Safe no-op when client/query empty or no hits.
//
// minScore <= 0 uses agent.KnowledgeAutoRecallScoreThreshold.
func AppendAutoRecall(c *Client, b *strings.Builder, userMsg string, priorUserMessages []string, minScore float64) {
	if c == nil || b == nil || strings.TrimSpace(userMsg) == "" {
		return
	}
	if minScore <= 0 {
		minScore = agent.KnowledgeAutoRecallScoreThreshold
	}
	query := agent.ExpandKnowledgeAutoRecallQuery(userMsg, priorUserMessages)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hits, err := c.SearchActive(ctx, query, "")
	if err != nil {
		log.Printf("[enterpriseknowledge] auto-recall search error: %v", err)
		return
	}
	if len(hits) == 0 {
		return
	}
	topScore := hits[0].Score
	maxInject := agent.KnowledgeAutoRecallMaxInjectWithMin(topScore, minScore)
	if maxInject == 0 {
		return
	}
	b.WriteString(agent.EnterpriseKnowledgeAutoRecallHeader)
	injected := 0
	for _, r := range hits {
		if injected >= maxInject {
			break
		}
		if r.Score < minScore {
			break
		}
		source := r.Source.Title
		if source == "" {
			source = r.Source.RelativePath
		}
		if source == "" {
			source = r.Source.URI
		}
		if source == "" {
			source = r.Source.ID
		}
		text := knowledge.BestContentText(r)
		if text == "" {
			text = r.CardTitle
			if text == "" {
				text = r.Citation
			}
		}
		if text == "" {
			continue
		}
		if len([]rune(text)) > agent.KnowledgeAutoRecallSnippetMaxRunes {
			text = string([]rune(text)[:agent.KnowledgeAutoRecallSnippetMaxRunes]) + "..."
		}
		b.WriteString(fmt.Sprintf("- [企业知识:%s] %s\n", source, text))
		injected++
	}
}

// AppendAutoRecallFromDataDir leases a pooled client under dataDir for recall.
// Prefer a long-lived Client when available (e.g. GUI).
func AppendAutoRecallFromDataDir(dataDir string, b *strings.Builder, userMsg string, priorUserMessages []string, minScore float64) {
	if strings.TrimSpace(dataDir) == "" || b == nil || strings.TrimSpace(userMsg) == "" {
		return
	}
	// Hot path: most users never synced enterprise assets — skip SQLite open.
	if !MetaDBExists(dataDir) {
		return
	}
	lease, err := LeaseMeta(dataDir)
	if err != nil {
		return
	}
	defer lease.Release()
	if !lease.Client.HasActiveLibraries() {
		return
	}
	AppendAutoRecall(lease.Client, b, userMsg, priorUserMessages, minScore)
}
