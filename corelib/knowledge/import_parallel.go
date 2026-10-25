package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// Light text imports (markdown/plain text) are dominated by CPU work: parse,
// card/fact distillation, and CJK FTS segmentation. Spreadsheet import already
// parallelizes that prep; this path does the same for multi-file directory
// imports so 8-core machines are not stuck on a single-file loop.

const importLightBatchMax = 64

func canParallelLightImport(kind string, req DirectoryImportRequest) bool {
	switch kind {
	case SourceKindMarkdown, SourceKindText:
		// LLM distillation is rate-limited / latency-bound; keep sequential so
		// progress steps and backpressure stay simple.
		if NormalizeDistillMode(req.DistillMode) == DistillModeLLMIfAny {
			return false
		}
		return true
	default:
		return false
	}
}

func importParallelWorkers(n int) int {
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	if n > 0 && workers > n {
		workers = n
	}
	return workers
}

type lightPreparedNode struct {
	node     DocumentNode
	ftsTitle string
	ftsText  string
	metaJSON string
}

type lightPreparedCard struct {
	card         Card
	entitiesJSON string
	topicsJSON   string
	tagsJSON     string
	ftsTitle     string
	ftsClaim     string
	ftsSummary   string
}

type lightPreparedFact struct {
	fact       Fact
	ftsSubject string
	ftsPred    string
	ftsObject  string
}

type lightPreparedItem struct {
	index  int
	item   ImportItem
	source Source
	// parsedNodes originate from the pre-delete, scan-hash-verified private
	// snapshot. Keeping this distinct from prepareLightImportItem's historical
	// live-path parse prevents a markdown/text re-import from deleting version A
	// and then indexing a replacement version B.
	parsedNodes      []DocumentNode
	parsedInputReady bool
	preserveExisting bool
	nodes            []lightPreparedNode
	cards            []lightPreparedCard
	facts            []lightPreparedFact
	parseErr         error
	unsupported      bool
	// itemFailed means the item should be recorded as failed after prep.
	itemFailed bool
	failMsg    string
	// replaceExisting deletes the previous derived rows in the phase-3 write
	// transaction, after parsing and distillation have released the lock.
	replaceExisting bool
}

// lightImportBeforeParse is a narrow test seam for the scan-to-snapshot
// boundary. Production leaves it as a no-op; tests use it to deterministically
// replace a source after ScanFiles established its duplicate hash.
var lightImportBeforeParse = func(ImportItem) {}

// fileRefreshBeforeParse is the refresh equivalent of the import seam above.
// Production leaves it as a no-op; tests use it to replace the live pathname
// between its initial metadata checks and private snapshot creation.
var fileRefreshBeforeParse = func(Source) {}

// importLightItemsBatch prepares and inserts items[start:end] using parallel CPU
// prep + sequential SQLite writes. Caller must ensure every item is queued and
// canParallelLightImport-eligible. start/end are indices into items.
func (s *SQLiteStore) importLightItemsBatch(
	ctx context.Context,
	txp **sql.Tx,
	req DirectoryImportRequest,
	batchID string,
	items []ImportItem,
	start, end int,
	imported, failed *int,
	importedSourceIDs *[]string,
	recordFailedItem func(ImportItem),
	markImportItemProcessed func(int, ImportItem),
	emitStepProgress func(ImportItem, string, int, int),
) error {
	if txp == nil || *txp == nil {
		return fmt.Errorf("knowledge import transaction is nil")
	}
	if end <= start {
		return nil
	}
	// Publish the caller's transaction even when this function returns early.
	// Parsing and distillation run with no transaction checked out. Phase 3 is
	// one write batch; each file uses a savepoint so a failed insert does not
	// keep an earlier delete in that batch.
	tx := *txp
	defer func() { *txp = tx }()
	// Warm CJK segmenter once so workers don't stampede dict load.
	bm25.PrewarmDict()

	n := end - start
	jobs := make([]lightPreparedItem, n)

	// Phase 1: sequential source identity + replace cleanup (DB-bound).
	for i := start; i < end; i++ {
		if tx == nil {
			next, beginErr := s.db.BeginTx(ctx, nil)
			if beginErr != nil {
				return beginErr
			}
			tx = next
		}
		item := items[i]
		item.BatchID = batchID
		source := BuildSourceFromImport(req, batchID, item)
		if emitStepProgress != nil {
			emitStepProgress(item, "preparing", 1, 5)
		}
		existingSource, exists, err := findExistingSourceForImport(ctx, tx, req, item, s.ownerForExistingSourceLookup(req.OwnerID))
		if err != nil {
			return err
		}
		if exists {
			source.ID = existingSource.ID
			source.CreatedAt = existingSource.CreatedAt
			if source.Title == "" {
				source.Title = existingSource.Title
			}
			if source.SourceTrust == 0 {
				source.SourceTrust = existingSource.SourceTrust
			}
			source = preserveStoredOwner(source, existingSource)
			source = preserveStoredProjectPath(source, existingSource)
		}
		// Commit the lookup before parsing. A deferred transaction that has
		// already read holds a shared lock until commit, and it also checks
		// out this store's only connection. The next statement begins again.
		kept, commitErr := commitImportTx(tx)
		if commitErr != nil {
			tx = kept
			return commitErr
		}
		tx = nil
		// Text and Markdown now share the scan-to-parse version contract of
		// Office/PDF/CSV imports. Parse an owned snapshot before destructive
		// replacement, bind Source.content_hash to those bytes, then reuse the
		// already parsed nodes during the parallel CPU-prep phase.
		lightImportBeforeParse(item)
		parsed, parseErr := parseDocumentNodesForOfficeReadImportWithOfficeReadConfig(source, item.FilePath, item.Kind, req.OfficeReadConfig)
		if parsed != nil && parsed.contentHash != item.FileHash {
			parsed.close()
			parsed = nil
			parseErr = agent.ErrOfficeReadSourceChanged
		}
		if parseErr != nil {
			if parsed != nil {
				parsed.close()
			}
			item.SourceID = source.ID
			item.Status = ItemStatusImported
			item.UpdatedAt = time.Now().UTC()
			jobs[i-start] = lightPreparedItem{
				index: i, item: item, source: source, parseErr: parseErr,
				preserveExisting: exists,
			}
			continue
		}
		if parsed == nil {
			return fmt.Errorf("light document parser returned no result")
		}
		source.ContentHash = parsed.contentHash
		parsedNodes := parsed.nodes
		parsed.close()
		item.SourceID = source.ID
		item.Status = ItemStatusImported
		item.UpdatedAt = time.Now().UTC()
		jobs[i-start] = lightPreparedItem{
			index: i, item: item, source: source, parsedNodes: parsedNodes, parsedInputReady: true,
			replaceExisting: exists,
		}
	}

	// Phase 2: parallel parse + distill + FTS segmentation.
	if emitStepProgress != nil && n > 0 {
		emitStepProgress(jobs[0].item, "distilling", 5, 5)
	}
	workers := importParallelWorkers(n)
	if workers <= 1 {
		for j := 0; j < n; j++ {
			if ctx.Err() != nil {
				jobs[j].itemFailed = true
				jobs[j].failMsg = ctx.Err().Error()
				continue
			}
			if jobs[j].parseErr != nil {
				continue
			}
			jobs[j] = s.prepareLightImportItem(ctx, req, jobs[j])
		}
	} else {
		var wg sync.WaitGroup
		chunkSize := (n + workers - 1) / workers
		for w := 0; w < workers; w++ {
			wStart := w * chunkSize
			wEnd := wStart + chunkSize
			if wEnd > n {
				wEnd = n
			}
			if wStart >= wEnd {
				break
			}
			wg.Add(1)
			go func(from, to int) {
				defer wg.Done()
				for j := from; j < to; j++ {
					if ctx.Err() != nil {
						jobs[j].itemFailed = true
						jobs[j].failMsg = ctx.Err().Error()
						continue
					}
					if jobs[j].parseErr != nil {
						continue
					}
					jobs[j] = s.prepareLightImportItem(ctx, req, jobs[j])
				}
			}(wStart, wEnd)
		}
		wg.Wait()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Optional: batch-embed all cards that need vectors (single-threaded; embedders
	// are often not concurrency-safe).
	embeddingModelID := ""
	if emb, _ := s.currentEmbedderSnapshot(); emb != nil && !embedding.IsNoop(emb) {
		embeddingModelID = embeddingModelIdentifier(emb)
		type cardRef struct {
			jobIdx, cardIdx int
		}
		refs := make([]cardRef, 0, n)
		texts := make([]string, 0, n)
		for j := range jobs {
			if jobs[j].itemFailed || jobs[j].unsupported || len(jobs[j].cards) == 0 {
				continue
			}
			for c := range jobs[j].cards {
				refs = append(refs, cardRef{j, c})
				texts = append(texts, cardEmbeddingText(jobs[j].cards[c].card))
			}
		}
		if len(texts) > 0 {
			if vectors, err := embedding.EmbedBatchAs(emb, texts, embedding.RoleDocument); err == nil && len(vectors) == len(texts) {
				for i, ref := range refs {
					jobs[ref.jobIdx].cards[ref.cardIdx].card.Embedding = vectors[i]
				}
			}
		}
	}

	// Phase 3: sequential SQL with prepared statements. Parsing finished with
	// no transaction checked out, so begin the write batch here.
	if tx == nil {
		next, beginErr := s.db.BeginTx(ctx, nil)
		if beginErr != nil {
			return beginErr
		}
		tx = next
	}
	stmts, err := prepareLightImportStmts(ctx, tx)
	if err != nil {
		return err
	}
	defer stmts.Close()

	for j := range jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `SAVEPOINT kimp`); err != nil {
			return err
		}
		prep := jobs[j]
		item := prep.item
		source := prep.source
		// undo drops this file's writes, including a replacement delete, then
		// records the import item. Earlier files stay in the batch transaction
		// because their savepoints were already released.
		undo := func(msg string) error {
			if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT kimp`); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `RELEASE SAVEPOINT kimp`); err != nil {
				return err
			}
			item.Status = ItemStatusFailed
			item.ErrorMessage = msg
			*failed++
			if recordFailedItem != nil {
				recordFailedItem(item)
			}
			if err := insertImportItem(ctx, tx, item); err != nil {
				return err
			}
			if markImportItemProcessed != nil {
				markImportItemProcessed(prep.index, item)
			}
			return nil
		}
		releaseItem := func() error {
			_, err := tx.ExecContext(ctx, `RELEASE SAVEPOINT kimp`)
			return err
		}

		if prep.itemFailed {
			if err := undo(prep.failMsg); err != nil {
				return err
			}
			continue
		}

		// A re-import whose scan-qualified source changed before the owned parse
		// must preserve the currently indexed Source in full. In particular, do
		// not let the generic early source write below overwrite its hash with the
		// scan-time candidate before we record the failed import item.
		if prep.parseErr != nil && prep.preserveExisting {
			if err := undo(sanitizeKnowledgeParseError(item.Kind, prep.parseErr).Error()); err != nil {
				return err
			}
			continue
		}

		// Delete the previous derived rows only in this write batch, after
		// distillation has finished, so a crash during the model call does not
		// leave the source empty and the model call does not hold the lock.
		if prep.replaceExisting {
			if err := deleteSourceDerivedRows(ctx, tx, source.ID); err != nil {
				return err
			}
		}
		// Persist source row (may already reflect distilled status).
		if err := s.insertSource(ctx, tx, source); err != nil {
			if undoErr := undo(err.Error()); undoErr != nil {
				return undoErr
			}
			continue
		}
		if err := addSourceLabelsTx(ctx, tx, source.ID, ingestLabelsForSource(source, req.Labels, req.AutoLabels)); err != nil {
			if undoErr := undo(err.Error()); undoErr != nil {
				return undoErr
			}
			continue
		}

		if prep.unsupported {
			source.Status = StatusPending
			if err := s.insertSource(ctx, tx, source); err != nil {
				return err
			}
		} else if prep.parseErr != nil {
			persistedParseErr := sanitizeKnowledgeParseError(item.Kind, prep.parseErr)
			source.Status = StatusFailed
			source.ErrorMessage = persistedParseErr.Error()
			if err := s.insertSource(ctx, tx, source); err != nil {
				return err
			}
			item.Status = ItemStatusFailed
			item.ErrorMessage = persistedParseErr.Error()
			*failed++
			if recordFailedItem != nil {
				recordFailedItem(item)
			}
			if err := insertImportItem(ctx, tx, item); err != nil {
				return err
			}
			if markImportItemProcessed != nil {
				markImportItemProcessed(prep.index, item)
			}
			if err := releaseItem(); err != nil {
				return err
			}
			continue
		} else if len(prep.nodes) > 0 {
			if err := insertPreparedLightNodes(ctx, stmts, prep.nodes); err != nil {
				if undoErr := undo(err.Error()); undoErr != nil {
					return undoErr
				}
				continue
			}
			if len(prep.cards) > 0 {
				if err := insertPreparedLightCardsFacts(ctx, tx, stmts, prep, embeddingModelID); err != nil {
					if undoErr := undo(err.Error()); undoErr != nil {
						return undoErr
					}
					continue
				}
				source.Status = StatusDistilled
				source.UpdatedAt = time.Now().UTC()
				if err := s.insertSource(ctx, tx, source); err != nil {
					return err
				}
			}
		}

		if err := insertSourceVersionTx(ctx, tx, source, "import"); err != nil {
			return err
		}
		*imported++
		*importedSourceIDs = append(*importedSourceIDs, source.ID)
		if err := insertImportItem(ctx, tx, item); err != nil {
			return err
		}
		if markImportItemProcessed != nil {
			markImportItemProcessed(prep.index, item)
		}
		if err := releaseItem(); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) prepareLightImportItem(ctx context.Context, req DirectoryImportRequest, prep lightPreparedItem) lightPreparedItem {
	select {
	case <-ctx.Done():
		prep.itemFailed = true
		prep.failMsg = ctx.Err().Error()
		return prep
	default:
	}

	source := prep.source
	nodes := prep.parsedNodes
	parseErr := prep.parseErr
	if !prep.parsedInputReady {
		nodes, parseErr = ParseDocumentNodes(source, prep.item.FilePath, prep.item.Kind)
	}
	if parseErr != nil {
		if IsUnsupportedParserError(parseErr) {
			prep.unsupported = true
			prep.source = source
			return prep
		}
		prep.parseErr = parseErr
		return prep
	}
	if len(nodes) == 0 {
		prep.source = source
		return prep
	}
	nodes = annotateMultilingualNodes(nodes)

	source.NodeCount = len(nodes)
	// Intra-document parallel prep (node FTS + cards/facts) so single large files
	// also use multiple cores, not only multi-file batches.
	prepNodes := prepareNodesForInsert(nodes)
	// prepareNodesForInsert mutates node IDs on the slice; refresh from prepared.
	nodes = make([]DocumentNode, len(prepNodes))
	for i, pn := range prepNodes {
		nodes[i] = pn.node
	}

	// Cards copy owner_id from the source. Stamp first so that copy matches the
	// source row written after this prepare step.
	source = s.applyDesktopOwnerStamp(source)
	cards := buildCardsForNodesFast(source, nodes)
	if s != nil && s.distiller != nil && shouldUseLLMForMode(req.DistillMode, source, nodes) {
		if llmCards, err := s.distiller.DistillCards(ctx, source, nodes); err == nil && len(llmCards) > 0 {
			cards = bindDistilledCardsToNodes(NormalizeDistilledCards(source, llmCards), nodes)
		}
	}
	prepCards, prepFacts := prepareCardsAndFacts(source, cards)

	if len(prepCards) > 0 {
		source.Status = StatusDistilled
		source.UpdatedAt = time.Now().UTC()
	} else {
		source.Status = StatusParsed
		source.UpdatedAt = time.Now().UTC()
	}
	prep.source = source
	prep.nodes = prepNodes
	prep.cards = prepCards
	prep.facts = prepFacts
	return prep
}

type lightImportStmts struct {
	nodeIns    *sql.Stmt
	nodeFTSIns *sql.Stmt
	cardIns    *sql.Stmt
	cardFTSIns *sql.Stmt
	factIns    *sql.Stmt
	factFTSIns *sql.Stmt
}

func prepareLightImportStmts(ctx context.Context, tx *sql.Tx) (*lightImportStmts, error) {
	nodeIns, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO document_nodes
		(id, source_id, parent_id, type, title, text, level, page, sheet_name, row_range, col_range, xpath, offset, metadata_json, token_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, fmt.Errorf("prepare document_nodes: %w", err)
	}
	nodeFTSIns, err := tx.PrepareContext(ctx, `INSERT INTO document_nodes_fts(node_id, title, text) VALUES (?, ?, ?)`)
	if err != nil {
		nodeIns.Close()
		return nil, fmt.Errorf("prepare document_nodes_fts: %w", err)
	}
	cardIns, err := tx.PrepareContext(ctx, insertCardSQL)
	if err != nil {
		nodeIns.Close()
		nodeFTSIns.Close()
		return nil, fmt.Errorf("prepare knowledge_cards: %w", err)
	}
	cardFTSIns, err := tx.PrepareContext(ctx, `INSERT INTO knowledge_cards_fts(card_id, title, claim, summary) VALUES (?, ?, ?, ?)`)
	if err != nil {
		nodeIns.Close()
		nodeFTSIns.Close()
		cardIns.Close()
		return nil, fmt.Errorf("prepare knowledge_cards_fts: %w", err)
	}
	factIns, err := tx.PrepareContext(ctx, insertFactSQL)
	if err != nil {
		nodeIns.Close()
		nodeFTSIns.Close()
		cardIns.Close()
		cardFTSIns.Close()
		return nil, fmt.Errorf("prepare knowledge_facts: %w", err)
	}
	factFTSIns, err := tx.PrepareContext(ctx, `INSERT INTO knowledge_facts_fts(fact_id, subject, predicate, object) VALUES (?, ?, ?, ?)`)
	if err != nil {
		nodeIns.Close()
		nodeFTSIns.Close()
		cardIns.Close()
		cardFTSIns.Close()
		factIns.Close()
		return nil, fmt.Errorf("prepare knowledge_facts_fts: %w", err)
	}
	return &lightImportStmts{
		nodeIns: nodeIns, nodeFTSIns: nodeFTSIns,
		cardIns: cardIns, cardFTSIns: cardFTSIns,
		factIns: factIns, factFTSIns: factFTSIns,
	}, nil
}

func (s *lightImportStmts) Close() {
	if s == nil {
		return
	}
	s.nodeIns.Close()
	s.nodeFTSIns.Close()
	s.cardIns.Close()
	s.cardFTSIns.Close()
	s.factIns.Close()
	s.factFTSIns.Close()
}

func insertPreparedLightNodes(ctx context.Context, stmts *lightImportStmts, nodes []lightPreparedNode) error {
	for _, pn := range nodes {
		node := pn.node
		if _, err := stmts.nodeIns.ExecContext(ctx,
			node.ID, node.SourceID, node.ParentID, node.Type, node.Title, node.Text, node.Level, node.Page, node.SheetName,
			node.RowRange, node.ColRange, node.XPath, node.Offset, pn.metaJSON, node.TokenCount,
		); err != nil {
			return err
		}
		// Fresh IDs: skip FTS DELETE.
		if _, err := stmts.nodeFTSIns.ExecContext(ctx, node.ID, pn.ftsTitle, pn.ftsText); err != nil {
			return err
		}
	}
	return nil
}

func insertPreparedLightCardsFacts(ctx context.Context, tx *sql.Tx, stmts *lightImportStmts, prep lightPreparedItem, modelID string) error {
	return insertPreparedCardsFacts(ctx, tx, stmts, prep.cards, prep.facts, modelID)
}
