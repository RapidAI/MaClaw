package guiapp

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"hash"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

const chatTaskLegacyMetaKey = "legacy_import"
const chatTaskSnapshotGenKey = "snapshot_gen"

const chatTaskSQLiteSchema = `
CREATE TABLE IF NOT EXISTS chat_tasks (
    id TEXT PRIMARY KEY,
    session_key TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    prompt TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    progress TEXT NOT NULL DEFAULT '',
    question TEXT NOT NULL DEFAULT '',
    result_text TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    where_we_are TEXT NOT NULL DEFAULT '',
    next_step TEXT NOT NULL DEFAULT '',
    open_problems TEXT NOT NULL DEFAULT '',
    progress_path TEXT NOT NULL DEFAULT '',
    artifact_paths TEXT NOT NULL DEFAULT '[]',
    created_at_unix INTEGER NOT NULL DEFAULT 0,
    updated_at_unix INTEGER NOT NULL DEFAULT 0,
    recovered INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS chat_tasks_updated ON chat_tasks(updated_at_unix);
CREATE VIRTUAL TABLE IF NOT EXISTS chat_tasks_fts USING fts5(
    id UNINDEXED,
    title,
    prompt,
    result_text,
    progress,
    tokenize='trigram'
);
CREATE TABLE IF NOT EXISTS chat_task_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
`

func chatTaskDBPath(jsonPath string) string {
	ext := filepath.Ext(jsonPath)
	return strings.TrimSuffix(jsonPath, ext) + ".db"
}

func (r *chatTaskRegistry) openChatTaskDB() error {
	if r == nil || r.storePath == "" {
		return os.ErrInvalid
	}
	r.dbOnce.Do(func() {
		dbPath := chatTaskDBPath(r.storePath)
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			r.dbErr = err
			return
		}
		db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
		if err != nil {
			r.dbErr = err
			return
		}
		db.SetMaxOpenConns(1)
		if testing.Testing() {
			db.SetMaxIdleConns(0)
		} else {
			db.SetMaxIdleConns(1)
		}
		if _, err := db.Exec(chatTaskSQLiteSchema); err != nil {
			_ = db.Close()
			r.dbErr = err
			return
		}
		if _, err := db.Exec(`DROP INDEX IF EXISTS chat_tasks_session_status`); err != nil {
			_ = db.Close()
			r.dbErr = err
			return
		}
		r.db = db
	})
	return r.dbErr
}

func (r *chatTaskRegistry) chatTaskLegacyImported() bool {
	if r == nil || r.db == nil {
		return false
	}
	var value string
	err := r.db.QueryRow(`SELECT value FROM chat_task_meta WHERE key=?`, chatTaskLegacyMetaKey).Scan(&value)
	return err == nil && value == "1"
}

func (r *chatTaskRegistry) setChatTaskMeta(key, value string) error {
	if r == nil || r.db == nil {
		return os.ErrInvalid
	}
	_, err := r.db.Exec(`INSERT INTO chat_task_meta(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

type chatTaskQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

func chatTaskMetaInt(q chatTaskQueryer, key string) (int64, error) {
	var raw string
	err := q.QueryRow(`SELECT value FROM chat_task_meta WHERE key=?`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
}

func (r *chatTaskRegistry) chatSnapshotGen() (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	return chatTaskMetaInt(r.db, chatTaskSnapshotGenKey)
}

func (r *chatTaskRegistry) chatTasksPresent() bool {
	if r == nil || r.db == nil {
		return false
	}
	var id string
	err := r.db.QueryRow(`SELECT id FROM chat_tasks LIMIT 1`).Scan(&id)
	return err == nil && id != ""
}

type chatTaskSig struct {
	sum uint64
}

func (r *chatTaskRegistry) persistSQLite(tasks []AIAssistantChatTask, gen int64) error {
	if err := r.openChatTaskDB(); err != nil {
		return err
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	known := r.taskSig
	if !r.taskSigReady {
		loaded, err := loadChatTaskRows(tx)
		if err != nil {
			return err
		}
		known = make(map[string]chatTaskSig, len(loaded))
		for id, task := range loaded {
			artifacts, err := chatTaskArtifactJSON(task.ArtifactPaths)
			if err != nil {
				return err
			}
			known[id] = chatTaskSignature(task, artifacts)
		}
	}
	keep := make(map[string]struct{}, len(tasks))
	next := make(map[string]chatTaskSig, len(tasks))
	for _, task := range tasks {
		if task.ID == "" {
			continue
		}
		keep[task.ID] = struct{}{}
		artifacts, err := chatTaskArtifactJSON(task.ArtifactPaths)
		if err != nil {
			return err
		}
		sig := chatTaskSignature(task, artifacts)
		next[task.ID] = sig
		prev, ok := known[task.ID]
		if ok && prev == sig {
			continue
		}
		recovered := 0
		if task.Recovered {
			recovered = 1
		}
		if _, err := tx.Exec(`INSERT INTO chat_tasks(
			id, session_key, title, prompt, status, progress, question, result_text, error,
			where_we_are, next_step, open_problems, progress_path, artifact_paths,
			created_at_unix, updated_at_unix, recovered)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
			session_key=excluded.session_key, title=excluded.title, prompt=excluded.prompt,
			status=excluded.status, progress=excluded.progress, question=excluded.question,
			result_text=excluded.result_text, error=excluded.error, where_we_are=excluded.where_we_are,
			next_step=excluded.next_step, open_problems=excluded.open_problems,
			progress_path=excluded.progress_path, artifact_paths=excluded.artifact_paths,
			created_at_unix=excluded.created_at_unix, updated_at_unix=excluded.updated_at_unix,
			recovered=excluded.recovered`,
			task.ID, task.SessionKey, task.Title, task.Prompt, task.Status, task.Progress, task.Question,
			task.ResultText, task.Error, task.WhereWeAre, task.NextStep, task.OpenProblems, task.ProgressPath,
			artifacts, task.CreatedAtUnix, task.UpdatedAtUnix, recovered); err != nil {
			return err
		}
	}
	for id := range known {
		if _, ok := keep[id]; ok {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM chat_tasks WHERE id=?`, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO chat_task_meta(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, chatTaskLegacyMetaKey, "1"); err != nil {
		return err
	}
	current, err := chatTaskMetaInt(tx, chatTaskSnapshotGenKey)
	if err != nil {
		return err
	}
	if gen <= current {
		gen = current + 1
	}
	if _, err := tx.Exec(`INSERT INTO chat_task_meta(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, chatTaskSnapshotGenKey, strconv.FormatInt(gen, 10)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.taskSig = next
	r.taskSigReady = true
	return nil
}

func chatTaskSignature(task AIAssistantChatTask, artifacts string) chatTaskSig {
	sum := fnv.New64a()
	hashChatParts(sum, task.Title, task.Prompt, task.ResultText, task.Progress, task.SessionKey, task.Status, task.Question, task.Error, task.WhereWeAre, task.NextStep, task.OpenProblems, task.ProgressPath, artifacts)
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(task.CreatedAtUnix))
	_, _ = sum.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], uint64(task.UpdatedAtUnix))
	_, _ = sum.Write(buf[:])
	if task.Recovered {
		_, _ = sum.Write([]byte{1})
	} else {
		_, _ = sum.Write([]byte{0})
	}
	return chatTaskSig{sum: sum.Sum64()}
}

func hashChatParts(h hash.Hash, parts ...string) {
	var buf [8]byte
	for _, part := range parts {
		binary.LittleEndian.PutUint64(buf[:], uint64(len(part)))
		_, _ = h.Write(buf[:])
		_, _ = h.Write([]byte(part))
	}
}

func loadChatTaskRows(tx *sql.Tx) (map[string]AIAssistantChatTask, error) {
	rows, err := tx.Query(`SELECT id, session_key, title, prompt, status, progress, question, result_text, error,
		where_we_are, next_step, open_problems, progress_path, artifact_paths,
		created_at_unix, updated_at_unix, recovered FROM chat_tasks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]AIAssistantChatTask{}
	for rows.Next() {
		var task AIAssistantChatTask
		var artifacts string
		var recovered int
		if err := rows.Scan(&task.ID, &task.SessionKey, &task.Title, &task.Prompt, &task.Status, &task.Progress,
			&task.Question, &task.ResultText, &task.Error, &task.WhereWeAre, &task.NextStep, &task.OpenProblems,
			&task.ProgressPath, &artifacts, &task.CreatedAtUnix, &task.UpdatedAtUnix, &recovered); err != nil {
			return nil, err
		}
		task.Recovered = recovered != 0
		if artifacts != "" && artifacts != "[]" {
			_ = json.Unmarshal([]byte(artifacts), &task.ArtifactPaths)
		}
		task.ArtifactPaths = normalizeChatTaskArtifacts(task.ArtifactPaths)
		out[task.ID] = task
	}
	return out, rows.Err()
}

func chatTaskArtifactJSON(paths []string) (string, error) {
	paths = normalizeChatTaskArtifacts(paths)
	if len(paths) == 0 {
		return "[]", nil
	}
	raw, err := json.Marshal(paths)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func normalizeChatTaskArtifacts(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	return paths
}

func (r *chatTaskRegistry) selectChatTasks() ([]AIAssistantChatTask, error) {
	rows, err := r.db.Query(`SELECT id, session_key, title, prompt, status, progress, question, result_text, error,
		where_we_are, next_step, open_problems, progress_path, artifact_paths,
		created_at_unix, updated_at_unix, recovered
		FROM chat_tasks ORDER BY updated_at_unix, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AIAssistantChatTask
	for rows.Next() {
		var task AIAssistantChatTask
		var artifacts string
		var recovered int
		if err := rows.Scan(&task.ID, &task.SessionKey, &task.Title, &task.Prompt, &task.Status, &task.Progress,
			&task.Question, &task.ResultText, &task.Error, &task.WhereWeAre, &task.NextStep, &task.OpenProblems,
			&task.ProgressPath, &artifacts, &task.CreatedAtUnix, &task.UpdatedAtUnix, &recovered); err != nil {
			return nil, err
		}
		if artifacts != "" && artifacts != "[]" {
			_ = json.Unmarshal([]byte(artifacts), &task.ArtifactPaths)
		}
		task.Recovered = recovered != 0
		out = append(out, task)
	}
	return out, rows.Err()
}

// Search finds tasks by title, prompt, result, progress, question, error, or
// the where/next/open notes. The registry already holds every task.
func (r *chatTaskRegistry) Search(query string, limit int) []AIAssistantChatTask {
	if r == nil {
		return nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	needle := strings.ToLower(query)
	var out []AIAssistantChatTask
	for _, task := range r.List("") {
		blob := strings.ToLower(strings.Join([]string{task.Title, task.Prompt, task.ResultText, task.Progress, task.Question, task.Error, task.WhereWeAre, task.NextStep, task.OpenProblems}, "\n"))
		if strings.Contains(blob, needle) {
			out = append(out, task)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
