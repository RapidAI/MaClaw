package guiapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestChatTaskSQLiteImportsJSONAndSearches(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Unix()
	blob, err := json.Marshal(chatTaskDisk{Tasks: []AIAssistantChatTask{{
		ID:            "t1",
		SessionKey:    "desktop-user",
		Title:         "天气简报",
		Prompt:        "生成兰州天气",
		Status:        string(chatTaskDone),
		ResultText:    "明天有雨",
		CreatedAtUnix: now,
		UpdatedAtUnix: now,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, chatTaskStoreFile), blob, 0o644); err != nil {
		t.Fatal(err)
	}
	reg := newChatTaskRegistry(nil, dir)
	tasks := reg.List("")
	if len(tasks) != 1 || tasks[0].ID != "t1" || tasks[0].ResultText != "明天有雨" {
		t.Fatalf("imported = %+v", tasks)
	}
	found := reg.Search("兰州天气", 5)
	if len(found) != 1 || found[0].ID != "t1" {
		t.Fatalf("search = %+v", found)
	}
	reloaded := newChatTaskRegistry(nil, dir)
	if got := reloaded.List(""); len(got) != 1 || got[0].Prompt != "生成兰州天气" {
		t.Fatalf("reloaded = %+v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, chatTaskStoreFile), []byte(`{"tasks":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	again := newChatTaskRegistry(nil, dir)
	if got := again.List(""); len(got) != 1 || got[0].ID != "t1" {
		t.Fatalf("stale json clobbered sqlite: %+v", got)
	}
}

func TestChatTaskSQLiteSurvivesMissingOrBrokenLegacyFile(t *testing.T) {
	dir := t.TempDir()
	reg := newChatTaskRegistry(nil, dir)
	now := time.Now()
	reg.mu.Lock()
	reg.tasks["a"] = &chatTaskRecord{
		ID: "a", Title: "兰州天气", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist")
	}
	if _, err := reg.db.Exec(`DELETE FROM chat_task_meta WHERE key=?`, chatTaskLegacyMetaKey); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(dir, chatTaskStoreFile))
	kept := newChatTaskRegistry(nil, dir)
	if got := kept.List(""); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("missing json wiped sqlite: %+v", got)
	}

	if _, err := kept.db.Exec(`DELETE FROM chat_task_meta WHERE key=?`, chatTaskLegacyMetaKey); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, chatTaskStoreFile), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := newChatTaskRegistry(nil, dir)
	if got := broken.List(""); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("broken json hid sqlite: %+v", got)
	}
}

func TestChatTaskJSONFallbackImportsWhenSQLiteWriteFails(t *testing.T) {
	dir := t.TempDir()
	reg := newChatTaskRegistry(nil, dir)
	now := time.Now()
	reg.mu.Lock()
	reg.tasks["a"] = &chatTaskRecord{
		ID: "a", Title: "已保存", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist")
	}
	if _, err := reg.db.Exec(`DROP TABLE chat_tasks`); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.tasks["b"] = &chatTaskRecord{
		ID: "b", Title: "只写进了JSON", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if reg.persist() {
		t.Fatal("sqlite persist succeeded after the table was dropped")
	}
	again := newChatTaskRegistry(nil, dir)
	got := map[string]bool{}
	for _, task := range again.List("") {
		got[task.ID] = true
	}
	if !got["a"] || !got["b"] {
		t.Fatalf("fallback import = %+v", again.List(""))
	}
}

func TestChatTaskFallbackMarkerImportsWhenFlagRemains(t *testing.T) {
	dir := t.TempDir()
	reg := newChatTaskRegistry(nil, dir)
	now := time.Now()
	reg.mu.Lock()
	reg.tasks["a"] = &chatTaskRecord{
		ID: "a", Title: "已保存", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist")
	}
	if _, err := reg.db.Exec(`DROP TABLE chat_tasks`); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.tasks["b"] = &chatTaskRecord{
		ID: "b", Title: "回退快照", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if reg.persist() {
		t.Fatal("sqlite persist succeeded after the table was dropped")
	}
	if _, err := os.Stat(chatTaskFallbackMarker(filepath.Join(dir, chatTaskStoreFile))); err != nil {
		t.Fatal(err)
	}
	// The process stopped after the file and marker were published, before the
	// import flag was cleared.
	if err := reg.setChatTaskMeta(chatTaskLegacyMetaKey, "1"); err != nil {
		t.Fatal(err)
	}
	again := newChatTaskRegistry(nil, dir)
	got := map[string]bool{}
	for _, task := range again.List("") {
		got[task.ID] = true
	}
	if !got["a"] || !got["b"] {
		t.Fatalf("fallback import = %+v", again.List(""))
	}
}

func TestChatTaskFallbackSnapshotImportsWithoutJSONFile(t *testing.T) {
	dir := t.TempDir()
	reg := newChatTaskRegistry(nil, dir)
	now := time.Now()
	reg.mu.Lock()
	reg.tasks["a"] = &chatTaskRecord{
		ID: "a", Title: "已保存", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist")
	}
	if _, err := reg.db.Exec(`DROP TABLE chat_tasks`); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.tasks["b"] = &chatTaskRecord{
		ID: "b", Title: "回退快照", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if reg.persist() {
		t.Fatal("sqlite persist succeeded after the table was dropped")
	}
	jsonPath := filepath.Join(dir, chatTaskStoreFile)
	if err := os.Remove(jsonPath); err != nil {
		t.Fatal(err)
	}
	if err := reg.setChatTaskMeta(chatTaskLegacyMetaKey, "1"); err != nil {
		t.Fatal(err)
	}
	again := newChatTaskRegistry(nil, dir)
	got := map[string]bool{}
	for _, task := range again.List("") {
		got[task.ID] = true
	}
	if !got["a"] || !got["b"] {
		t.Fatalf("fallback snapshot import = %+v", again.List(""))
	}
}

func TestChatTaskLegacyFallbackFlagStillReadsJSON(t *testing.T) {
	dir := t.TempDir()
	reg := newChatTaskRegistry(nil, dir)
	now := time.Now()
	reg.mu.Lock()
	reg.tasks["a"] = &chatTaskRecord{
		ID: "a", Title: "已保存", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist")
	}
	if _, err := reg.db.Exec(`DROP TABLE chat_tasks`); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.tasks["b"] = &chatTaskRecord{
		ID: "b", Title: "回退快照", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if reg.persist() {
		t.Fatal("sqlite persist succeeded after the table was dropped")
	}
	jsonPath := filepath.Join(dir, chatTaskStoreFile)
	if err := os.WriteFile(chatTaskFallbackMarker(jsonPath), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reg.setChatTaskMeta(chatTaskLegacyMetaKey, "1"); err != nil {
		t.Fatal(err)
	}
	again := newChatTaskRegistry(nil, dir)
	got := map[string]bool{}
	for _, task := range again.List("") {
		got[task.ID] = true
	}
	if !got["a"] || !got["b"] {
		t.Fatalf("legacy flag import = %+v", again.List(""))
	}
}

func TestChatTaskStaleFallbackDoesNotReplaceCommittedSQLite(t *testing.T) {
	dir := t.TempDir()
	reg := newChatTaskRegistry(nil, dir)
	now := time.Now()
	reg.mu.Lock()
	reg.tasks["a"] = &chatTaskRecord{
		ID: "a", Title: "已提交", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist")
	}
	marker := chatTaskFallbackMarker(filepath.Join(dir, chatTaskStoreFile))
	stale := []byte("{\n  \"generation\": 1,\n  \"tasks\": [{\"id\": \"b\", \"title\": \"旧回退\", \"status\": \"done\"}]\n}")
	if err := os.WriteFile(marker, stale, 0o644); err != nil {
		t.Fatal(err)
	}
	again := newChatTaskRegistry(nil, dir)
	got := map[string]bool{}
	for _, task := range again.List("") {
		got[task.ID] = true
	}
	if !got["a"] || got["b"] {
		t.Fatalf("stale fallback replaced sqlite: %+v", again.List(""))
	}

	newer := []byte("{\n  \"generation\": 2,\n  \"tasks\": [{\"id\": \"a\", \"title\": \"已提交\", \"status\": \"done\"}, {\"id\": \"c\", \"title\": \"更新的回退\", \"status\": \"done\"}]\n}")
	if err := os.WriteFile(marker, newer, 0o644); err != nil {
		t.Fatal(err)
	}
	third := newChatTaskRegistry(nil, dir)
	got = map[string]bool{}
	for _, task := range third.List("") {
		got[task.ID] = true
	}
	if !got["a"] || !got["c"] || got["b"] {
		t.Fatalf("newer fallback import = %+v", third.List(""))
	}
}

func TestChatTaskUnreadSQLiteIsNotReplaced(t *testing.T) {
	dir := t.TempDir()
	reg := newChatTaskRegistry(nil, dir)
	now := time.Now()
	reg.mu.Lock()
	reg.tasks["a"] = &chatTaskRecord{
		ID: "a", Title: "已保存", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist")
	}
	if _, err := reg.db.Exec(`UPDATE chat_tasks SET recovered=? WHERE id=?`, "nope", "a"); err != nil {
		t.Fatal(err)
	}
	again := newChatTaskRegistry(nil, dir)
	if got := again.List(""); len(got) != 0 {
		t.Fatalf("unreadable rows became a task list: %+v", got)
	}
	again.mu.Lock()
	again.tasks["b"] = &chatTaskRecord{
		ID: "b", Title: "新任务", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	again.mu.Unlock()
	if again.persist() {
		t.Fatal("persist committed without a loaded task list")
	}
	var title string
	if err := again.db.QueryRow(`SELECT title FROM chat_tasks WHERE id=?`, "a").Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "已保存" {
		t.Fatalf("title = %q", title)
	}
	var n int
	if err := again.db.QueryRow(`SELECT COUNT(*) FROM chat_tasks WHERE id=?`, "b").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("unread table gained a row: %d", n)
	}
	if _, err := os.Stat(chatTaskFallbackMarker(filepath.Join(dir, chatTaskStoreFile))); !os.IsNotExist(err) {
		t.Fatalf("fallback published from an unloaded registry: %v", err)
	}
}

func TestChatTaskSameGenerationJSONDoesNotReplaceSQLite(t *testing.T) {
	dir := t.TempDir()
	reg := newChatTaskRegistry(nil, dir)
	now := time.Now()
	reg.mu.Lock()
	reg.tasks["a"] = &chatTaskRecord{
		ID: "a", Title: "已提交", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist")
	}
	if _, err := reg.db.Exec(`DELETE FROM chat_task_meta WHERE key=?`, chatTaskLegacyMetaKey); err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(dir, chatTaskStoreFile)
	if err := os.WriteFile(jsonPath, []byte(`{"tasks":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	again := newChatTaskRegistry(nil, dir)
	if got := again.List(""); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("generation 0 json replaced sqlite: %+v", got)
	}
	if err := os.WriteFile(jsonPath, []byte("{\n  \"generation\": 1,\n  \"tasks\": [{\"id\": \"b\", \"title\": \"同代\", \"status\": \"done\"}]\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	third := newChatTaskRegistry(nil, dir)
	got := map[string]bool{}
	for _, task := range third.List("") {
		got[task.ID] = true
	}
	if !got["a"] || got["b"] {
		t.Fatalf("same generation json replaced sqlite: %+v", third.List(""))
	}
}

func TestChatTaskNewerJSONWinsOverOlderFallback(t *testing.T) {
	dir := t.TempDir()
	reg := newChatTaskRegistry(nil, dir)
	now := time.Now()
	reg.mu.Lock()
	reg.tasks["a"] = &chatTaskRecord{
		ID: "a", Title: "已提交", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist")
	}
	jsonPath := filepath.Join(dir, chatTaskStoreFile)
	marker := chatTaskFallbackMarker(jsonPath)
	if err := os.WriteFile(marker, []byte("{\n  \"generation\": 2,\n  \"tasks\": [{\"id\": \"b\", \"title\": \"旧回退\", \"status\": \"done\"}]\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, []byte("{\n  \"generation\": 4,\n  \"tasks\": [{\"id\": \"a\", \"title\": \"已提交\", \"status\": \"done\"}, {\"id\": \"c\", \"title\": \"更新的JSON\", \"status\": \"done\"}]\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	again := newChatTaskRegistry(nil, dir)
	got := map[string]bool{}
	for _, task := range again.List("") {
		got[task.ID] = true
	}
	if !got["a"] || !got["c"] || got["b"] {
		t.Fatalf("newer json import = %+v", again.List(""))
	}
}

func TestChatTaskFallbackGenerationStaysAboveExistingFile(t *testing.T) {
	if nextChatTaskGeneration(1, 4) != 5 || nextChatTaskGeneration(1) != 2 || nextChatTaskGeneration(0, 0) != 1 {
		t.Fatalf("next generation = %d %d %d", nextChatTaskGeneration(1, 4), nextChatTaskGeneration(1), nextChatTaskGeneration(0, 0))
	}
	dir := t.TempDir()
	reg := newChatTaskRegistry(nil, dir)
	now := time.Now()
	reg.mu.Lock()
	reg.tasks["a"] = &chatTaskRecord{
		ID: "a", Title: "已提交", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist")
	}
	jsonPath := filepath.Join(dir, chatTaskStoreFile)
	marker := chatTaskFallbackMarker(jsonPath)
	if err := os.WriteFile(marker, []byte("{\n  \"generation\": 4,\n  \"tasks\": [{\"id\": \"a\", \"title\": \"已提交\", \"status\": \"done\"}]\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.db.Exec(`DROP TABLE chat_tasks`); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.tasks["b"] = &chatTaskRecord{
		ID: "b", Title: "更新的回退", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if reg.persist() {
		t.Fatal("sqlite persist succeeded after the table was dropped")
	}
	blob, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	_, gen, ok := chatTaskSnapshotBytes(blob)
	if !ok || gen != 5 {
		t.Fatalf("published generation = %d ok=%v", gen, ok)
	}
}

func TestChatTaskCommitGenerationSupersedesFallbackFile(t *testing.T) {
	dir := t.TempDir()
	reg := newChatTaskRegistry(nil, dir)
	now := time.Now()
	reg.mu.Lock()
	reg.tasks["a"] = &chatTaskRecord{
		ID: "a", Title: "已提交", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist")
	}
	marker := chatTaskFallbackMarker(filepath.Join(dir, chatTaskStoreFile))
	stale := []byte("{\n  \"generation\": 4,\n  \"tasks\": [{\"id\": \"b\", \"title\": \"旧回退\", \"status\": \"done\"}]\n}")
	if err := os.WriteFile(marker, stale, 0o644); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.tasks["c"] = &chatTaskRecord{
		ID: "c", Title: "新提交", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist newer")
	}
	var raw string
	if err := reg.db.QueryRow(`SELECT value FROM chat_task_meta WHERE key=?`, chatTaskSnapshotGenKey).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != "5" {
		t.Fatalf("snapshot_gen = %s", raw)
	}
	if err := os.WriteFile(marker, stale, 0o644); err != nil {
		t.Fatal(err)
	}
	again := newChatTaskRegistry(nil, dir)
	got := map[string]bool{}
	for _, task := range again.List("") {
		got[task.ID] = true
	}
	if !got["a"] || !got["c"] || got["b"] {
		t.Fatalf("stale high file replaced the commit: %+v", again.List(""))
	}
}

func TestChatTaskPublishDoesNotTruncateRecoveryFile(t *testing.T) {
	dir := t.TempDir()
	reg := newChatTaskRegistry(nil, dir)
	now := time.Now()
	reg.mu.Lock()
	reg.tasks["a"] = &chatTaskRecord{
		ID: "a", Title: "已提交", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist")
	}
	if _, err := reg.db.Exec(`DROP TABLE chat_tasks`); err != nil {
		t.Fatal(err)
	}
	marker := chatTaskFallbackMarker(filepath.Join(dir, chatTaskStoreFile))
	recovery := []byte("{\n  \"generation\": 7,\n  \"tasks\": [{\"id\": \"kept\", \"title\": \"仅临时文件\", \"status\": \"done\"}]\n}")
	if err := os.WriteFile(marker+".tmp", recovery, 0o644); err != nil {
		t.Fatal(err)
	}
	writing := []byte("{\n  \"generation\": 6,\n  \"tasks\": [{\"id\": \"parked\", \"title\": \"写入中的快照\", \"status\": \"done\"}]\n}")
	if err := os.WriteFile(marker+".writing", writing, 0o644); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	reg.tasks["b"] = &chatTaskRecord{
		ID: "b", Title: "新回退", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if reg.persist() {
		t.Fatal("sqlite persist succeeded after the table was dropped")
	}
	got, err := os.ReadFile(marker + ".tmp")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(recovery) {
		t.Fatalf("recovery temp = %s", got)
	}
	parked, err := os.ReadFile(marker + ".writing.prior")
	if err != nil {
		t.Fatal(err)
	}
	if string(parked) != string(writing) {
		t.Fatalf("parked writing = %s", parked)
	}
	blob, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	_, gen, ok := chatTaskSnapshotBytes(blob)
	if !ok || gen != 8 {
		t.Fatalf("published generation = %d ok=%v body=%s", gen, ok, blob)
	}
}

func TestChatTaskSearchIncludesQuestion(t *testing.T) {
	dir := t.TempDir()
	reg := newChatTaskRegistry(nil, dir)
	now := time.Now()
	reg.mu.Lock()
	reg.tasks["a"] = &chatTaskRecord{
		ID: "a", Title: "兰州天气", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.tasks["b"] = &chatTaskRecord{
		ID: "b", Title: "其他任务", Question: "补充兰州天气", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.tasks["c"] = &chatTaskRecord{
		ID: "c", Title: "笔记", WhereWeAre: "兰州天气写在进展里", Status: chatTaskDone, CreatedAt: now, UpdatedAt: now,
	}
	reg.mu.Unlock()
	if !reg.persist() {
		t.Fatal("persist")
	}
	found := reg.Search("兰州天气", 5)
	got := map[string]bool{}
	for _, task := range found {
		got[task.ID] = true
	}
	if !got["a"] || !got["b"] || !got["c"] {
		t.Fatalf("search = %+v", found)
	}
}
