package cloudagent

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	_ "modernc.org/sqlite"
)

const timeLayout = "2006-01-02T15:04:05.000000000Z"

func formatTS(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.UTC().Format(timeLayout)
}

type book struct {
	db *sql.DB
}

func openBook(dir string) (*book, error) {
	db, err := sql.Open("sqlite", filepath.Join(dir, "transcript.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	b := &book{db: db}
	if err := b.schema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return b, nil
}

func (b *book) Close() error {
	if b == nil || b.db == nil {
		return nil
	}
	return b.db.Close()
}

func (b *book) schema() error {
	_, err := b.db.Exec(`
CREATE TABLE IF NOT EXISTS tasks (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	role TEXT NOT NULL,
	content TEXT NOT NULL,
	as_error INTEGER NOT NULL DEFAULT 0,
	files TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS client_messages (
	client_message_id TEXT PRIMARY KEY,
	request_id TEXT NOT NULL,
	task_id TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS turns (
	request_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	status TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	finished_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS commit_approvals (
	task_id TEXT NOT NULL,
	fingerprint TEXT NOT NULL,
	decision TEXT NOT NULL,
	PRIMARY KEY (task_id, fingerprint)
);
CREATE TABLE IF NOT EXISTS commit_pending (
	task_id TEXT PRIMARY KEY,
	fingerprint TEXT NOT NULL,
	nonce TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS queued_turns (
	queue_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	text TEXT NOT NULL,
	client_message_id TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS plan_state (
	task_id TEXT PRIMARY KEY,
	state TEXT NOT NULL,
	request TEXT NOT NULL DEFAULT '',
	restatement TEXT NOT NULL DEFAULT '',
	plan TEXT NOT NULL DEFAULT '',
	steps TEXT NOT NULL DEFAULT '',
	revision INTEGER NOT NULL DEFAULT 0,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_task ON messages(task_id);
CREATE INDEX IF NOT EXISTS idx_turns_task ON turns(task_id);
CREATE INDEX IF NOT EXISTS idx_turns_running ON turns(request_id) WHERE status = 'running';`)
	if err != nil {
		return err
	}
	// Artifact paths a turn produced ride on its assistant row. Older dbs get
	// the column here; a fresh CREATE already has it through the ALTER too.
	if _, err := b.db.Exec(`ALTER TABLE messages ADD COLUMN files TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	// Task-scoped AI expert persona, shipped from the desktop at create time.
	for _, col := range []string{"expert_name", "expert_prompt"} {
		if _, err := b.db.Exec(`ALTER TABLE tasks ADD COLUMN ` + col + ` TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}

func (b *book) EnsureTask(id, title string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	_, err := b.db.Exec(`INSERT INTO tasks (id, title, created_at) VALUES (?, ?, ?)
		ON CONFLICT(id) DO NOTHING`, id, title, formatTS(time.Now()))
	return err
}

// Begin records a running turn. A repeated client message id returns the
// original request id and does not insert another row. A different message
// is refused while any turn is still running or parked input is ahead of it,
// so one instance runs one turn and a send cannot jump the durable queue in
// the finish→drain handoff window.
func (b *book) Begin(clientID, taskID, text string, now time.Time) (requestID string, created bool, err error) {
	return b.begin(clientID, taskID, text, now, false)
}

// BeginDrain is the queue runner's admission path. The oldest parked row is
// still present while it is being promoted, so the queue-ahead fence does not
// apply to this one internal begin.
func (b *book) BeginDrain(clientID, taskID, text string, now time.Time) (requestID string, created bool, err error) {
	return b.begin(clientID, taskID, text, now, true)
}

func (b *book) begin(clientID, taskID, text string, now time.Time, draining bool) (requestID string, created bool, err error) {
	clientID = strings.TrimSpace(clientID)
	tx, err := b.db.Begin()
	if err != nil {
		return "", false, err
	}
	defer func() { _ = tx.Rollback() }()
	if clientID != "" {
		var existing string
		err = tx.QueryRow(`SELECT request_id FROM client_messages WHERE client_message_id = ?`, clientID).Scan(&existing)
		if err == nil {
			return existing, false, nil
		}
		if err != sql.ErrNoRows {
			return "", false, err
		}
	}
	var running int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM turns WHERE status = 'running'`).Scan(&running); err != nil {
		return "", false, err
	}
	if running > 0 {
		return "", false, errBusy
	}
	if !draining {
		var queued int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM queued_turns`).Scan(&queued); err != nil {
			return "", false, err
		}
		if queued > 0 {
			return "", false, errBusy
		}
	}
	if strings.TrimSpace(taskID) == "" {
		taskID = newID("task_")
	}
	stamp := formatTS(now)
	if _, err = tx.Exec(`INSERT INTO tasks (id, title, created_at) VALUES (?, ?, ?) ON CONFLICT(id) DO NOTHING`, taskID, text, stamp); err != nil {
		return "", false, err
	}
	requestID = newID("req_")
	if _, err = tx.Exec(`INSERT INTO turns (request_id, task_id, status, updated_at, finished_at) VALUES (?, ?, 'running', ?, '')`,
		requestID, taskID, stamp); err != nil {
		return "", false, err
	}
	if _, err = tx.Exec(`INSERT INTO messages (id, task_id, role, content, as_error, created_at) VALUES (?, ?, 'user', ?, 0, ?)`,
		newID("msg_"), taskID, text, stamp); err != nil {
		return "", false, err
	}
	if clientID != "" {
		if _, err = tx.Exec(`INSERT INTO client_messages (client_message_id, request_id, task_id) VALUES (?, ?, ?)`,
			clientID, requestID, taskID); err != nil {
			return "", false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return "", false, err
	}
	return requestID, true, nil
}

// queuedTurn is one pre-typed message waiting for the running turn to end.
type queuedTurn struct {
	QueueID  string
	TaskID   string
	Text     string
	ClientID string
}

// queuedTurnsMax bounds one instance's pending input.
const queuedTurnsMax = 200

var errQueueFull = errors.New("预输入队列已满")

// Enqueue parks a message while a turn is running. A client message id the
// queue or the turns already know returns its row instead of duplicating.
func (b *book) Enqueue(clientID, taskID, text string, now time.Time) (string, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID != "" {
		var existing string
		err := b.db.QueryRow(`SELECT request_id FROM client_messages WHERE client_message_id = ?`, clientID).Scan(&existing)
		if err == nil {
			return existing, nil
		}
		if err != sql.ErrNoRows {
			return "", err
		}
		err = b.db.QueryRow(`SELECT queue_id FROM queued_turns WHERE client_message_id = ?`, clientID).Scan(&existing)
		if err == nil {
			return existing, nil
		}
		if err != sql.ErrNoRows {
			return "", err
		}
	}
	var count int
	if err := b.db.QueryRow(`SELECT COUNT(*) FROM queued_turns`).Scan(&count); err != nil {
		return "", err
	}
	if count >= queuedTurnsMax {
		return "", errQueueFull
	}
	queueID := newID("que_")
	if _, err := b.db.Exec(`INSERT INTO queued_turns (queue_id, task_id, text, client_message_id, created_at) VALUES (?, ?, ?, ?, ?)`,
		queueID, strings.TrimSpace(taskID), text, clientID, formatTS(now)); err != nil {
		return "", err
	}
	return queueID, nil
}

// PeekOldest returns the first queued message in FIFO order without removing it.
func (b *book) PeekOldest() (queuedTurn, bool, error) {
	var msg queuedTurn
	err := b.db.QueryRow(`SELECT queue_id, task_id, text, client_message_id FROM queued_turns ORDER BY rowid LIMIT 1`).
		Scan(&msg.QueueID, &msg.TaskID, &msg.Text, &msg.ClientID)
	if err == sql.ErrNoRows {
		return queuedTurn{}, false, nil
	}
	if err != nil {
		return queuedTurn{}, false, err
	}
	return msg, true, nil
}

// DropQueued removes a queued row once its turn has begun.
func (b *book) DropQueued(queueID string) error {
	_, err := b.db.Exec(`DELETE FROM queued_turns WHERE queue_id = ?`, strings.TrimSpace(queueID))
	return err
}

func (b *book) TaskOf(requestID string) (string, error) {
	var task string
	err := b.db.QueryRow(`SELECT task_id FROM turns WHERE request_id = ?`, requestID).Scan(&task)
	return task, err
}

func (b *book) History(taskID string) ([]string, error) {
	lines, _, _, err := b.history(taskID)
	return lines, err
}

// history returns the role\ncontent lines plus a parallel as_error flag and
// the produced-file list of each row. Callers that feed the model or the
// desktop split on the first newline, so the line stays role, newline,
// content. The flag and files are separate values.
func (b *book) history(taskID string) ([]string, []bool, [][]string, error) {
	// rowid breaks created_at ties: messages written inside one transaction
	// share a stamp, and the transcript order is the insertion order.
	rows, err := b.db.Query(`SELECT role, content, as_error, files FROM messages WHERE task_id = ? ORDER BY created_at ASC, rowid ASC`, taskID)
	if err != nil {
		return nil, nil, nil, err
	}
	var lines []string
	var flags []bool
	var files [][]string
	for rows.Next() {
		var role, content, rawFiles string
		var asError int
		if err := rows.Scan(&role, &content, &asError, &rawFiles); err != nil {
			_ = rows.Close()
			return nil, nil, nil, err
		}
		lines = append(lines, role+"\n"+content)
		flags = append(flags, asError != 0)
		list := []string{}
		_ = json.Unmarshal([]byte(rawFiles), &list)
		files = append(files, list)
	}
	err = rows.Err()
	_ = rows.Close()
	return lines, flags, files, err
}

// producedPaths is the fetch whitelist: every workspace-relative path a
// finished turn recorded, in the canonical slash form.
func (b *book) producedPaths() map[string]bool {
	out := map[string]bool{}
	rows, err := b.db.Query(`SELECT files FROM messages WHERE files != ''`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return out
		}
		var list []string
		if json.Unmarshal([]byte(raw), &list) != nil {
			continue
		}
		for _, path := range list {
			if path = strings.TrimSpace(path); path != "" {
				out[path] = true
			}
		}
	}
	return out
}

// Finish writes the assistant reply only while the turn is still running.
// An interrupted turn is left as it is, so a late model result is not a replay.
// wrote is true only when this call moved the turn out of running. files are
// the workspace-relative paths this turn produced, stored as JSON.
func (b *book) Finish(requestID, content string, asError bool, files []string, now time.Time) (wrote bool, err error) {
	taskID, err := b.TaskOf(requestID)
	if err != nil {
		return false, err
	}
	stamp := formatTS(now)
	res, err := b.db.Exec(`UPDATE turns SET status = 'finished', finished_at = ?, updated_at = ? WHERE request_id = ? AND status = 'running'`,
		stamp, stamp, requestID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return false, nil
	}
	flag := 0
	if asError {
		flag = 1
	}
	rawFiles := ""
	if len(files) > 0 {
		if raw, marshalErr := json.Marshal(files); marshalErr == nil {
			rawFiles = string(raw)
		}
	}
	_, err = b.db.Exec(`INSERT INTO messages (id, task_id, role, content, as_error, files, created_at) VALUES (?, ?, 'assistant', ?, ?, ?, ?)`,
		newID("msg_"), taskID, content, flag, rawFiles, stamp)
	return err == nil, err
}

func (b *book) interrupt(now time.Time, note string, olderThan time.Time) ([]string, error) {
	rows, err := b.db.Query(`SELECT request_id, task_id, updated_at FROM turns WHERE status = 'running'`)
	if err != nil {
		return nil, err
	}
	type row struct{ request, task, updated string }
	var due []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.request, &item.task, &item.updated); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if !olderThan.IsZero() {
			got, parseErr := time.Parse(timeLayout, item.updated)
			if parseErr != nil || !got.Before(olderThan) {
				continue
			}
		}
		due = append(due, item)
	}
	scanErr := rows.Err()
	_ = rows.Close()
	if scanErr != nil {
		return nil, scanErr
	}
	stamp := formatTS(now)
	var ids []string
	for _, item := range due {
		res, err := b.db.Exec(`UPDATE turns SET status = 'interrupted', finished_at = ?, updated_at = ? WHERE request_id = ? AND status = 'running'`,
			stamp, stamp, item.request)
		if err != nil {
			return ids, err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			continue
		}
		ids = append(ids, item.request)
		if _, err := b.db.Exec(`INSERT INTO messages (id, task_id, role, content, as_error, created_at) VALUES (?, ?, 'assistant', ?, 1, ?)`,
			newID("msg_"), item.task, note, stamp); err != nil {
			return ids, err
		}
	}
	return ids, nil
}

func (b *book) Status(requestID string) (string, error) {
	var status string
	err := b.db.QueryRow(`SELECT status FROM turns WHERE request_id = ?`, requestID).Scan(&status)
	return status, err
}

// latestTurn is the newest turn for one task. Cancel does not append a
// message, so the transcript alone still ends with the user line.
func (b *book) latestTurn(taskID string) (string, error) {
	var status string
	err := b.db.QueryRow(`SELECT status FROM turns WHERE task_id = ? ORDER BY updated_at DESC LIMIT 1`, taskID).Scan(&status)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return status, err
}

func (b *book) lookup(task, fp string) (agent.CommitDecision, bool) {
	var decision string
	err := b.db.QueryRow(`SELECT decision FROM commit_approvals WHERE task_id = ? AND fingerprint = ?`, task, fp).Scan(&decision)
	if err != nil || decision == "" {
		return "", false
	}
	return agent.CommitDecision(decision), true
}

func (b *book) storeDecision(task, fp, decision string) error {
	if strings.TrimSpace(fp) == "" {
		return nil
	}
	if decision == "" {
		_, err := b.db.Exec(`DELETE FROM commit_approvals WHERE task_id = ? AND fingerprint = ?`, task, fp)
		return err
	}
	_, err := b.db.Exec(`INSERT INTO commit_approvals (task_id, fingerprint, decision) VALUES (?, ?, ?)
		ON CONFLICT(task_id, fingerprint) DO UPDATE SET decision = excluded.decision`, task, fp, decision)
	return err
}

func (b *book) prepareAsk(task, fp string) string {
	nonce := newID("ask_")
	_, err := b.db.Exec(`INSERT INTO commit_pending (task_id, fingerprint, nonce) VALUES (?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET fingerprint = excluded.fingerprint, nonce = excluded.nonce`, task, fp, nonce)
	if err != nil {
		return ""
	}
	return nonce
}

func (b *book) pending(task string) string {
	var fp string
	_ = b.db.QueryRow(`SELECT fingerprint FROM commit_pending WHERE task_id = ?`, task).Scan(&fp)
	return fp
}

func (b *book) RecordAnswer(task, answer string) error {
	decision, ok := agent.ParseCommitApprovalAnswer(answer)
	if !ok {
		return errNotApproval
	}
	return b.storeDecision(task, b.pending(task), string(decision))
}

type taskRow struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (b *book) Tasks() ([]taskRow, error) {
	rows, err := b.db.Query(`SELECT id, title FROM tasks ORDER BY created_at ASC, rowid ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []taskRow
	for rows.Next() {
		var item taskRow
		if err := rows.Scan(&item.ID, &item.Title); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (b *book) Rename(id, title string) error {
	_, err := b.db.Exec(`UPDATE tasks SET title = ? WHERE id = ?`, title, id)
	return err
}

// SetPersona binds the task's AI expert persona: the desktop resolves the
// picked expert to a name and system prompt and ships both at create time.
func (b *book) SetPersona(id, name, prompt string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	_, err := b.db.Exec(`UPDATE tasks SET expert_name = ?, expert_prompt = ? WHERE id = ?`,
		strings.TrimSpace(name), strings.TrimSpace(prompt), id)
	return err
}

// Persona returns the expert name and system prompt stored for this task.
func (b *book) Persona(id string) (name, prompt string) {
	if strings.TrimSpace(id) == "" {
		return "", ""
	}
	_ = b.db.QueryRow(`SELECT expert_name, expert_prompt FROM tasks WHERE id = ?`, id).Scan(&name, &prompt)
	return name, prompt
}

func (b *book) Running() (taskID, requestID string) {
	_ = b.db.QueryRow(`SELECT task_id, request_id FROM turns WHERE status = 'running' ORDER BY updated_at DESC LIMIT 1`).Scan(&taskID, &requestID)
	return taskID, requestID
}

func (b *book) Touch(requestID string, now time.Time) error {
	_, err := b.db.Exec(`UPDATE turns SET updated_at = ? WHERE request_id = ? AND status = 'running'`, formatTS(now), requestID)
	return err
}

func (b *book) CancelRunning(taskID string, now time.Time) ([]string, error) {
	query := `SELECT request_id FROM turns WHERE status = 'running'`
	var args []any
	if strings.TrimSpace(taskID) != "" {
		query += ` AND task_id = ?`
		args = append(args, taskID)
	}
	rows, err := b.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	scanErr := rows.Err()
	_ = rows.Close()
	if scanErr != nil {
		return nil, scanErr
	}
	stamp := formatTS(now)
	for _, id := range ids {
		if _, err := b.db.Exec(`UPDATE turns SET status = 'cancelled', finished_at = ?, updated_at = ? WHERE request_id = ? AND status = 'running'`,
			stamp, stamp, id); err != nil {
			return ids, err
		}
	}
	return ids, nil
}
