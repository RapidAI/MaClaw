package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/configfile"
	contract "github.com/RapidAI/CodeClaw/corelib/structureddata"
)

const misAgentViewTransactionField = "_mis_transaction_id"

type misTransactionField struct {
	Value       interface{} `json:"value,omitempty"`
	Source      string      `json:"source,omitempty"`
	Confidence  float64     `json:"confidence,omitempty"`
	Confirmed   bool        `json:"confirmed,omitempty"`
	ConfirmedAt *time.Time  `json:"confirmed_at,omitempty"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type misTransactionEvent struct {
	Type      string                 `json:"type"`
	Message   string                 `json:"message,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

type misTransactionActionSnapshot struct {
	ID             string                          `json:"id"`
	Domain         string                          `json:"domain,omitempty"`
	DatasetID      string                          `json:"dataset_id,omitempty"`
	Operation      string                          `json:"operation,omitempty"`
	Title          string                          `json:"title,omitempty"`
	Description    string                          `json:"description,omitempty"`
	RequiredFields []string                        `json:"required_fields,omitempty"`
	InputFields    []contract.DatasetTemplateField `json:"input_fields,omitempty"`
}

type misBusinessTransaction struct {
	ID             string                         `json:"id"`
	ActionID       string                         `json:"business_action_id"`
	BusinessObject string                         `json:"business_object_id,omitempty"`
	Domain         string                         `json:"domain,omitempty"`
	Operation      string                         `json:"operation,omitempty"`
	Query          string                         `json:"query,omitempty"`
	State          string                         `json:"state"`
	Fields         map[string]misTransactionField `json:"fields,omitempty"`
	Events         []misTransactionEvent          `json:"events,omitempty"`
	ActionSnapshot *misTransactionActionSnapshot  `json:"action_snapshot,omitempty"`
	CreatedAt      time.Time                      `json:"created_at"`
	UpdatedAt      time.Time                      `json:"updated_at"`
}

var misTransactionStore = struct {
	sync.Mutex
	next       int64
	loadedPath string
	items      map[string]*misBusinessTransaction
}{items: map[string]*misBusinessTransaction{}}

type misTransactionSnapshot struct {
	Next         int64                    `json:"next"`
	Transactions []misBusinessTransaction `json:"transactions"`
}

func createMISBusinessTransaction(actionID, businessObject, domain, operation, query string, data map[string]interface{}, source string) string {
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return ""
	}
	now := time.Now()
	misTransactionStore.Lock()
	defer misTransactionStore.Unlock()
	misTransactionStore.next++
	id := fmt.Sprintf("mistxn-%d-%d", now.UnixNano(), misTransactionStore.next)
	txn := &misBusinessTransaction{
		ID:             id,
		ActionID:       actionID,
		BusinessObject: strings.TrimSpace(businessObject),
		Domain:         strings.TrimSpace(domain),
		Operation:      strings.TrimSpace(operation),
		Query:          strings.TrimSpace(query),
		State:          "collecting",
		Fields:         map[string]misTransactionField{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	applyMISTransactionFieldsLocked(txn, data, source, false, 0)
	appendMISTransactionEventLocked(txn, "transaction.created", "Business transaction opened from AgentView.", nil)
	misTransactionStore.items[id] = txn
	pruneMISBusinessTransactionsLocked(now)
	return id
}

func ensureMISBusinessTransaction(txnID, actionID, businessObject, domain, operation, query string, data map[string]interface{}, source string) string {
	txnID = strings.TrimSpace(txnID)
	actionID = strings.TrimSpace(actionID)
	if txnID != "" {
		misTransactionStore.Lock()
		txn := misTransactionStore.items[txnID]
		if txn != nil && (actionID == "" || txn.ActionID == actionID) {
			applyMISTransactionFieldsLocked(txn, data, source, false, 0)
			if txn.State == "" {
				txn.State = "collecting"
			}
			txn.UpdatedAt = time.Now()
			misTransactionStore.Unlock()
			return txnID
		}
		misTransactionStore.Unlock()
	}
	return createMISBusinessTransaction(actionID, businessObject, domain, operation, query, data, source)
}

func updateMISBusinessTransactionFields(txnID string, data map[string]interface{}, source string, confirmed bool, confidence float64) {
	txnID = strings.TrimSpace(txnID)
	if txnID == "" {
		return
	}
	misTransactionStore.Lock()
	defer misTransactionStore.Unlock()
	if txn := misTransactionStore.items[txnID]; txn != nil {
		applyMISTransactionFieldsLocked(txn, data, source, confirmed, confidence)
		appendMISTransactionEventLocked(txn, "fields.updated", "Business fields updated from AgentView.", map[string]interface{}{"fields": sortedMISMapKeys(data), "source": source, "confirmed": confirmed})
	}
}

func markMISBusinessTransaction(txnID, state, eventType, message string, data map[string]interface{}) {
	txnID = strings.TrimSpace(txnID)
	if txnID == "" {
		return
	}
	misTransactionStore.Lock()
	defer misTransactionStore.Unlock()
	if txn := misTransactionStore.items[txnID]; txn != nil {
		if strings.TrimSpace(state) != "" {
			txn.State = strings.TrimSpace(state)
		}
		appendMISTransactionEventLocked(txn, eventType, message, data)
	}
}

func setMISBusinessTransactionActionSnapshot(txnID string, snapshot *misTransactionActionSnapshot) {
	txnID = strings.TrimSpace(txnID)
	if txnID == "" || snapshot == nil || strings.TrimSpace(snapshot.ID) == "" {
		return
	}
	misTransactionStore.Lock()
	defer misTransactionStore.Unlock()
	if txn := misTransactionStore.items[txnID]; txn != nil {
		txn.ActionSnapshot = cloneMISActionSnapshot(snapshot)
		txn.UpdatedAt = time.Now()
		appendMISTransactionEventLocked(txn, "action.snapshot_updated", "Business action form snapshot captured for resumable AgentView.", map[string]interface{}{"business_action_id": snapshot.ID})
	}
}

func getMISBusinessTransaction(txnID string) (*misBusinessTransaction, bool) {
	txnID = strings.TrimSpace(txnID)
	if txnID == "" {
		return nil, false
	}
	misTransactionStore.Lock()
	defer misTransactionStore.Unlock()
	txn := misTransactionStore.items[txnID]
	if txn == nil {
		return nil, false
	}
	clone := *txn
	clone.Fields = map[string]misTransactionField{}
	for key, field := range txn.Fields {
		clone.Fields[key] = field
	}
	clone.Events = append([]misTransactionEvent(nil), txn.Events...)
	clone.ActionSnapshot = cloneMISActionSnapshot(txn.ActionSnapshot)
	return &clone, true
}

func activeMISBusinessTransactions(actionID, businessObject string, limit int) []misBusinessTransaction {
	actionID = strings.TrimSpace(actionID)
	businessObject = strings.TrimSpace(businessObject)
	if limit <= 0 {
		limit = 5
	}
	misTransactionStore.Lock()
	defer misTransactionStore.Unlock()
	now := time.Now()
	pruneMISBusinessTransactionsLocked(now)
	out := make([]misBusinessTransaction, 0, limit)
	for _, txn := range misTransactionStore.items {
		if txn == nil || !isRecoverableMISTransactionState(txn.State) {
			continue
		}
		if actionID != "" && txn.ActionID != actionID {
			continue
		}
		if businessObject != "" && txn.BusinessObject != "" && txn.BusinessObject != businessObject {
			continue
		}
		clone := *txn
		clone.Fields = map[string]misTransactionField{}
		for key, field := range txn.Fields {
			clone.Fields[key] = field
		}
		clone.Events = append([]misTransactionEvent(nil), txn.Events...)
		clone.ActionSnapshot = cloneMISActionSnapshot(txn.ActionSnapshot)
		out = append(out, clone)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func isRecoverableMISTransactionState(state string) bool {
	switch strings.TrimSpace(state) {
	case "", "collecting", "validating", "awaiting_validation", "validation_failed", "awaiting_commit", "commit_failed":
		return true
	default:
		return false
	}
}

func misTransactionFieldValues(txn *misBusinessTransaction) map[string]interface{} {
	values := map[string]interface{}{}
	if txn == nil {
		return values
	}
	for key, field := range txn.Fields {
		if strings.TrimSpace(key) != "" {
			values[key] = field.Value
		}
	}
	return values
}

func misTransactionHiddenField(txnID string) map[string]interface{} {
	return map[string]interface{}{"name": misAgentViewTransactionField, "label": misAgentViewTransactionField, "type": "hidden", "value": strings.TrimSpace(txnID)}
}

func misActionSnapshotFromBusinessAction(action contract.BusinessAction) *misTransactionActionSnapshot {
	action.ID = strings.TrimSpace(action.ID)
	if action.ID == "" {
		return nil
	}
	return &misTransactionActionSnapshot{
		ID:             action.ID,
		Domain:         strings.TrimSpace(action.Domain),
		DatasetID:      strings.TrimSpace(action.DatasetID),
		Operation:      strings.TrimSpace(action.Operation),
		Title:          strings.TrimSpace(action.Title),
		Description:    strings.TrimSpace(action.Description),
		RequiredFields: append([]string(nil), action.RequiredFields...),
		InputFields:    cloneMISDatasetTemplateFields(action.InputFields),
	}
}

func misActionSnapshotFromIntent(actionID, businessObject, domain, operation, title, description string, requiredFields []string, inputFields []contract.DatasetTemplateField) *misTransactionActionSnapshot {
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return nil
	}
	return &misTransactionActionSnapshot{
		ID:             actionID,
		Domain:         strings.TrimSpace(domain),
		DatasetID:      strings.TrimSpace(businessObject),
		Operation:      strings.TrimSpace(operation),
		Title:          strings.TrimSpace(title),
		Description:    strings.TrimSpace(description),
		RequiredFields: append([]string(nil), requiredFields...),
		InputFields:    cloneMISDatasetTemplateFields(inputFields),
	}
}

func (snapshot *misTransactionActionSnapshot) toBusinessAction() contract.BusinessAction {
	if snapshot == nil {
		return contract.BusinessAction{}
	}
	return contract.BusinessAction{
		ID:             strings.TrimSpace(snapshot.ID),
		Domain:         strings.TrimSpace(snapshot.Domain),
		DatasetID:      strings.TrimSpace(snapshot.DatasetID),
		Operation:      strings.TrimSpace(snapshot.Operation),
		Title:          strings.TrimSpace(snapshot.Title),
		Description:    strings.TrimSpace(snapshot.Description),
		RequiredFields: append([]string(nil), snapshot.RequiredFields...),
		InputFields:    cloneMISDatasetTemplateFields(snapshot.InputFields),
	}
}

func misActionSnapshotFromAny(value interface{}) *misTransactionActionSnapshot {
	if value == nil {
		return nil
	}
	var snapshot misTransactionActionSnapshot
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil
	}
	if strings.TrimSpace(snapshot.ID) == "" {
		return nil
	}
	return cloneMISActionSnapshot(&snapshot)
}

func cloneMISActionSnapshot(snapshot *misTransactionActionSnapshot) *misTransactionActionSnapshot {
	if snapshot == nil {
		return nil
	}
	clone := *snapshot
	clone.RequiredFields = append([]string(nil), snapshot.RequiredFields...)
	clone.InputFields = cloneMISDatasetTemplateFields(snapshot.InputFields)
	return &clone
}

func cloneMISDatasetTemplateFields(fields []contract.DatasetTemplateField) []contract.DatasetTemplateField {
	if len(fields) == 0 {
		return nil
	}
	clone := make([]contract.DatasetTemplateField, len(fields))
	for i, field := range fields {
		clone[i] = field
		if field.Config != nil {
			clone[i].Config = cloneMISAnyMap(field.Config)
		}
	}
	return clone
}

func cloneMISAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	var out map[string]any
	if data, err := json.Marshal(in); err == nil {
		if err := json.Unmarshal(data, &out); err == nil {
			return out
		}
	}
	out = make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func extractMISAgentViewTransactionID(data map[string]interface{}) string {
	if data == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(data[misAgentViewTransactionField]))
}

func sanitizeMISAgentViewSubmittedData(data map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for key, value := range data {
		if strings.HasPrefix(strings.TrimSpace(key), "_mis_") {
			continue
		}
		out[key] = value
	}
	return out
}

func applyMISTransactionFieldsLocked(txn *misBusinessTransaction, data map[string]interface{}, source string, confirmed bool, confidence float64) {
	if txn == nil || len(data) == 0 {
		return
	}
	if txn.Fields == nil {
		txn.Fields = map[string]misTransactionField{}
	}
	now := time.Now()
	source = strings.TrimSpace(source)
	for key, value := range data {
		key = strings.TrimSpace(key)
		if key == "" || strings.HasPrefix(key, "_mis_") {
			continue
		}
		field := misTransactionField{Value: value, Source: source, Confidence: confidence, Confirmed: confirmed, UpdatedAt: now}
		if confirmed {
			field.ConfirmedAt = &now
		}
		txn.Fields[key] = field
	}
	txn.UpdatedAt = now
}

func appendMISTransactionEventLocked(txn *misBusinessTransaction, eventType, message string, data map[string]interface{}) {
	if txn == nil {
		return
	}
	now := time.Now()
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		eventType = "transaction.updated"
	}
	txn.Events = append(txn.Events, misTransactionEvent{Type: eventType, Message: strings.TrimSpace(message), Data: data, CreatedAt: now})
	txn.UpdatedAt = now
}

func pruneMISBusinessTransactionsLocked(now time.Time) {
	for id, txn := range misTransactionStore.items {
		if txn == nil || now.Sub(txn.UpdatedAt) > 24*time.Hour {
			delete(misTransactionStore.items, id)
		}
	}
}

func sortedMISMapKeys(data map[string]interface{}) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		key = strings.TrimSpace(key)
		if key != "" && !strings.HasPrefix(key, "_mis_") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func misTransactionStorePath(a *App) string {
	if a == nil {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "mis_transactions.json")
}

func ensureMISBusinessTransactionsLoaded(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	misTransactionStore.Lock()
	defer misTransactionStore.Unlock()
	if misTransactionStore.loadedPath == path {
		return nil
	}
	misTransactionStore.items = map[string]*misBusinessTransaction{}
	misTransactionStore.next = 0
	misTransactionStore.loadedPath = path
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var snapshot misTransactionSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	misTransactionStore.next = snapshot.Next
	now := time.Now()
	for i := range snapshot.Transactions {
		txn := snapshot.Transactions[i]
		if txn.ID == "" || now.Sub(txn.UpdatedAt) > 24*time.Hour {
			continue
		}
		if txn.Fields == nil {
			txn.Fields = map[string]misTransactionField{}
		}
		copied := txn
		misTransactionStore.items[txn.ID] = &copied
	}
	return nil
}

func saveMISBusinessTransactions(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	misTransactionStore.Lock()
	defer misTransactionStore.Unlock()
	pruneMISBusinessTransactionsLocked(time.Now())
	snapshot := misTransactionSnapshot{Next: misTransactionStore.next}
	for _, txn := range misTransactionStore.items {
		if txn != nil {
			snapshot.Transactions = append(snapshot.Transactions, *txn)
		}
	}
	sort.Slice(snapshot.Transactions, func(i, j int) bool {
		return snapshot.Transactions[i].UpdatedAt.After(snapshot.Transactions[j].UpdatedAt)
	})
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return configfile.AtomicWrite(path, append(data, '\n'))
}

func (a *App) ensureMISBusinessTransactionsLoaded() {
	_ = ensureMISBusinessTransactionsLoaded(misTransactionStorePath(a))
}

func (a *App) saveMISBusinessTransactions() {
	_ = saveMISBusinessTransactions(misTransactionStorePath(a))
}
