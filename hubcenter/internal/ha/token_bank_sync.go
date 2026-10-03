package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

// TokenBankLedgerRepository is what the HA service needs from the ledger store.
// It is an interface rather than a concrete repo so the sync path can be tested
// without a database, the same way the usage-batch sync is.
type TokenBankLedgerRepository interface {
	// InsertLedgerBatch reports applied=false when the batch was already seen.
	InsertLedgerBatch(ctx context.Context, batchID, sourceNodeID string, entries []sqlite.TokenBankLedgerEntry) (bool, error)
}

func (s *Service) AttachTokenBankLedger(repo TokenBankLedgerRepository) {
	if s == nil {
		return
	}
	s.tokenBankLedger = repo
}

// AppendTokenBankLedgerBatch replicates one batch of locally-recorded ledger
// movements to every peer.
//
// This is what makes the E1 design hold in a cluster: the ledger is the balance,
// and a balance that is not replicated is a balance that disagrees per node. The
// rows are append-only with deterministic ids, so the receiving side applies them
// with INSERT OR IGNORE and a redelivered batch is inert rather than
// double-crediting.
func (s *Service) AppendTokenBankLedgerBatch(ctx context.Context, batch *sqlite.TokenBankLedgerBatch) error {
	if s == nil || batch == nil || batch.BatchID == "" || len(batch.Entries) == 0 {
		return nil
	}
	if batch.OriginNodeID == "" {
		return fmt.Errorf("token bank ledger batch %s has no origin node id", batch.BatchID)
	}
	createdAt := batch.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	if err := s.AppendUpsert(ctx, EntityTokenBankLedgerBatch, batch.BatchID, batch, createdAt); err != nil {
		log.Printf("[hubcenter][ha] append token bank ledger batch: %v", err)
		s.recordFailure(ctx, "ha_sync", "append_token_bank_ledger_batch_failed", err.Error(), batch.BatchID, map[string]any{"entries": len(batch.Entries)})
		return err
	}
	return nil
}

// TokenBankPriceBookRepository is what the HA service needs to apply a price
// rule that arrived from a peer.
//
// It is the admin-managed side of Token Bank, so it is a plain upsert/delete
// pair rather than the append-only batch the ledger uses: a rule is identified
// by its id and the latest write wins. That is safe here only because there is
// exactly one writer per rule in practice — an admin editing it in one place —
// and because a concurrent edit is an admin-visible discrepancy, not money that
// silently appears or disappears.
type TokenBankPriceBookRepository interface {
	UpsertPriceRule(ctx context.Context, rule sqlite.TokenBankPriceRule, now time.Time) (*sqlite.TokenBankPriceRule, error)
	DeletePriceRule(ctx context.Context, idOrPattern string) (bool, error)
}

func (s *Service) AttachTokenBankPriceBook(repo TokenBankPriceBookRepository) {
	if s == nil {
		return
	}
	s.tokenBankPriceBook = repo
}

// AppendTokenBankPriceRule replicates one admin edit to every peer.
//
// Without this an edit is invisible outside the node that happened to serve the
// admin's request, and the cluster ends up running several price lists at once.
// The symptom is not an error anywhere: two identical calls settle for
// different amounts depending on which node proxied them.
func (s *Service) AppendTokenBankPriceRule(ctx context.Context, rule *sqlite.TokenBankPriceRule) error {
	if s == nil || rule == nil || strings.TrimSpace(rule.ID) == "" {
		return nil
	}
	updatedAt := rule.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	if err := s.AppendUpsert(ctx, EntityTokenBankPriceBook, rule.ID, rule, updatedAt); err != nil {
		log.Printf("[hubcenter][ha] append token bank price rule: %v", err)
		s.recordFailure(ctx, "ha_sync", "append_token_bank_price_rule_failed", err.Error(), rule.ID, map[string]any{"pattern": rule.ModelPattern})
		return err
	}
	return nil
}

// AppendTokenBankPriceRuleDelete replicates a removal. The id alone is enough:
// the receiving side deletes by id and a second delivery is a no-op.
func (s *Service) AppendTokenBankPriceRuleDelete(ctx context.Context, ruleID string, now time.Time) error {
	if s == nil || strings.TrimSpace(ruleID) == "" {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := s.AppendDelete(ctx, EntityTokenBankPriceBook, ruleID, map[string]string{"id": ruleID}, now); err != nil {
		log.Printf("[hubcenter][ha] append token bank price rule delete: %v", err)
		s.recordFailure(ctx, "ha_sync", "append_token_bank_price_rule_delete_failed", err.Error(), ruleID, nil)
		return err
	}
	return nil
}

func (s *Service) applyTokenBankPriceBookOp(ctx context.Context, op *store.HASyncOp) error {
	if s.tokenBankPriceBook == nil {
		// Mid-rollout a peer may not have the table yet. Skipping keeps the
		// rest of replication flowing.
		return nil
	}
	switch op.OpType {
	case OpUpsert:
		var rule sqlite.TokenBankPriceRule
		if err := json.Unmarshal([]byte(op.PayloadJSON), &rule); err != nil {
			return err
		}
		if strings.TrimSpace(rule.ID) == "" {
			rule.ID = op.EntityID
		}
		if rule.ID != op.EntityID {
			return fmt.Errorf("token bank price rule identity mismatch")
		}
		_, err := s.tokenBankPriceBook.UpsertPriceRule(ctx, rule, time.Now().UTC())
		return err
	case OpDelete:
		// A rule that is already gone reports false, which is the outcome a
		// redelivery wants, not an error.
		_, err := s.tokenBankPriceBook.DeletePriceRule(ctx, op.EntityID)
		return err
	default:
		return fmt.Errorf("unsupported token bank price book op: %s", op.OpType)
	}
}

func (s *Service) applyTokenBankLedgerBatchOp(ctx context.Context, op *store.HASyncOp) error {
	if s.tokenBankLedger == nil {
		// A node that predates the token bank feature can still be in the
		// cluster mid-rollout. Refusing here would wedge replication for every
		// other entity type, so an unattached ledger is a skip, not an error.
		return nil
	}
	if op.OpType != OpUpsert {
		return fmt.Errorf("unsupported token bank ledger op: %s", op.OpType)
	}
	var batch sqlite.TokenBankLedgerBatch
	if err := json.Unmarshal([]byte(op.PayloadJSON), &batch); err != nil {
		return err
	}
	if batch.BatchID == "" {
		batch.BatchID = op.EntityID
	}
	if batch.BatchID != op.EntityID {
		return fmt.Errorf("token bank ledger batch identity mismatch")
	}
	if batch.OriginNodeID == "" {
		return fmt.Errorf("token bank ledger batch %s has no origin node id", op.EntityID)
	}
	_, err := s.tokenBankLedger.InsertLedgerBatch(ctx, batch.BatchID, batch.OriginNodeID, batch.Entries)
	return err
}
