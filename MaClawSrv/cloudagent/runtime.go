package cloudagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	KindSession = "session"
	KindCoding  = "coding"

	llmOfficial = "official"
	llmCustom   = "custom"

	restartNote   = "云端运行时已重启，这一轮没有跑完。"
	heartbeatNote = "云端心跳中断，这一轮没有跑完。"
	stopNote      = "云端实例已停止，这一轮没有跑完。"
	missWindow    = 90 * time.Second
)

var (
	errNotFound    = errors.New("没有找到这个云端实例")
	errOwner       = errors.New("不能控制别人的云端实例")
	errNotApproval = errors.New("不是提交确认")
	errKind        = errors.New("实例类型不能修改")
	errSessionGit  = errors.New("会话实例不能连接代码库")
	errNeedsPerson = errors.New("缺少人的批准")
	errBusy        = errors.New("这个实例正在跑一轮")
	slugRe         = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
)

func newID(prefix string) string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return prefix + hex.EncodeToString(buf[:])
}

func slug(v string) string {
	v = slugRe.ReplaceAllString(strings.TrimSpace(v), "_")
	if v == "" {
		return "_"
	}
	return v
}

// Spec is the provision record. Secrets never belong here.
type Spec struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	OwnerUserID string `json:"owner_user_id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	LLMMode     string `json:"llm_mode"`
	LLMModel    string `json:"llm_model"`
	// LLMThinkingMode carries the owner's desktop thinking level
	// ("" auto | low | high | max | none) so cloud coding turns request
	// reasoning exactly like the desktop's own coding agent does.
	LLMThinkingMode string `json:"llm_thinking_mode,omitempty"`
	HubOrigin       string `json:"hub_origin"`
	HubLoopback     string `json:"hub_loopback"`
}

type instance struct {
	dir  string
	meta Spec
	book *book
	// workflow is the lazily opened V2 workflow machine for this instance —
	// one store file, workflows keyed by task id.
	wfMu     sync.Mutex
	workflow *workflowHolder
	// drainMu keeps concurrent recovery/stop/finish drain triggers from
	// promoting the same parked row twice.
	drainMu sync.Mutex
}

type trackedCancel struct {
	instanceID string
	cancel     context.CancelFunc
	done       chan struct{}
}

// Runtime is the in-process cloud-agent host. Open does not start tickers.
type Runtime struct {
	root        string
	adminSecret string
	mu          sync.Mutex
	instances   map[string]*instance
	cancels     map[string]trackedCancel
	stop        context.CancelFunc
	httpClient  *http.Client
	exec        executor
	inFlight    int
	lastUsed    time.Time
	streamMu    sync.Mutex
	grants      map[string]map[string]struct{}
	stream      map[string]*streamLane
	streamLogAt time.Time
}

// Open prepares cloud-agents under dataRoot and marks leftover running turns
// interrupted. It does not start the heartbeat loop.
func Open(dataRoot, adminSecret string) (*Runtime, error) {
	root := filepath.Join(dataRoot, "cloud-agents")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	rt := &Runtime{
		root:        root,
		adminSecret: adminSecret,
		instances:   map[string]*instance{},
		cancels:     map[string]trackedCancel{},
	}
	rt.httpClient = rt.newHTTPClient()
	if err := rt.reload(); err != nil {
		_ = rt.Close()
		return nil, err
	}
	if err := rt.Recover(); err != nil {
		_ = rt.Close()
		return nil, err
	}
	return rt, nil
}

func (rt *Runtime) reload() error {
	return filepath.WalkDir(rt.root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() || d.Name() != "meta.json" {
			return nil
		}
		if strings.Contains(path, ".deleted-") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var meta Spec
		if err := json.Unmarshal(raw, &meta); err != nil {
			return err
		}
		if meta.ID == "" {
			return nil
		}
		bk, err := openBook(filepath.Dir(path))
		if err != nil {
			return err
		}
		rt.instances[meta.ID] = &instance{dir: filepath.Dir(path), meta: meta, book: bk}
		return nil
	})
}

// Start runs the heartbeat and the deleted-directory sweep until ctx ends.
func (rt *Runtime) Start(ctx context.Context) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	if rt.stop != nil {
		rt.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	rt.stop = cancel
	rt.mu.Unlock()
	go rt.loop(ctx)
}

func (rt *Runtime) loop(ctx context.Context) {
	heart := time.NewTicker(30 * time.Second)
	sweep := time.NewTicker(time.Hour)
	defer heart.Stop()
	defer sweep.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-heart.C:
			rt.pulse(ctx, now)
			rt.idleUnload(15 * time.Minute)
		case now := <-sweep.C:
			_ = rt.Sweep(now)
		}
	}
}

// finishTurn stores the reply and tells Hub the request is done, so the
// concurrent-turn slot is released without waiting out the heartbeat window.
// The finish beat retries asynchronously: one lost POST must not pin the slot
// for the whole miss window, but a slow/dead Hub must not delay the durable
// input queue either — the next turn's start beat independently retires any
// older shadow row.
func (rt *Runtime) finishTurn(inst *instance, requestID, taskID, content string, asError bool, files []string, now time.Time) {
	wrote, err := inst.book.Finish(requestID, content, asError, files, now)
	slog.Debug("cloudagent finish turn",
		slog.String("instance", inst.meta.ID), slog.String("task", taskID),
		slog.String("request", requestID), slog.Bool("wrote", wrote),
		slog.Bool("as_error", asError), slog.Int("artifacts", len(files)),
		slog.Int("content_len", len(content)), slog.Any("err", err))
	if err != nil || !wrote {
		return
	}
	go func() {
		for attempt := 0; attempt < 4; attempt++ {
			if _, ok := rt.postHeartbeat(context.Background(), inst, taskID, requestID, true); ok {
				return
			}
			time.Sleep(time.Duration(attempt+1) * time.Second)
		}
	}()
}

func (rt *Runtime) pulse(ctx context.Context, now time.Time) {
	for _, inst := range rt.snapshot() {
		taskID, requestID := inst.book.Running()
		if requestID == "" {
			continue
		}
		enabled, ok := rt.postHeartbeat(ctx, inst, taskID, requestID, false)
		if !ok {
			continue
		}
		// The stream keepalive rides the same cadence: a watcher sees the lane
		// alive and the cursor advancing while a long tool call emits nothing.
		rt.publish(inst, taskID, requestID, "heartbeat", "", nil)
		if !enabled {
			ids, _ := inst.book.interrupt(now, heartbeatNote, time.Time{})
			rt.cancelIDs(ids)
			continue
		}
		_ = inst.book.Touch(requestID, now)
	}
	ids, _ := rt.interrupt(now, heartbeatNote, now.Add(-missWindow))
	rt.cancelIDs(ids)
}

// Recover marks every running turn interrupted and does not replay it.
// Messages parked in an instance's input queue still resume in order.
func (rt *Runtime) Recover() error {
	ids, err := rt.interrupt(time.Now(), restartNote, time.Time{})
	rt.cancelIDs(ids)
	for _, inst := range rt.snapshot() {
		go rt.drainQueue(inst)
	}
	return err
}

// InterruptMissed marks running turns whose updated_at is older than 90s.
func (rt *Runtime) InterruptMissed(now time.Time) error {
	ids, err := rt.interrupt(now, heartbeatNote, now.Add(-missWindow))
	rt.cancelIDs(ids)
	return err
}

// Resume lets a stopped instance continue with work that was already parked.
// drainQueue waits for the interrupted goroutine to finish unwinding before
// it promotes the oldest row.
func (rt *Runtime) Resume(id string) error {
	inst, err := rt.instance(id)
	if err != nil {
		return err
	}
	go rt.drainQueue(inst)
	return nil
}

// Stop interrupts every running turn of one instance. The workspace stays.
func (rt *Runtime) Stop(id string) error {
	inst, err := rt.instance(id)
	if err != nil {
		return err
	}
	ids, err := inst.book.interrupt(time.Now(), stopNote, time.Time{})
	rt.cancelIDs(ids)
	return err
}

func (rt *Runtime) interrupt(now time.Time, note string, olderThan time.Time) ([]string, error) {
	var ids []string
	var first error
	for _, inst := range rt.snapshot() {
		got, err := inst.book.interrupt(now, note, olderThan)
		ids = append(ids, got...)
		if err != nil && first == nil {
			first = err
		}
	}
	return ids, first
}

// Put writes meta.json and an empty workspace. Kind cannot change.
func (rt *Runtime) Put(spec Spec) error {
	spec.ID = strings.TrimSpace(spec.ID)
	spec.Kind = strings.TrimSpace(spec.Kind)
	spec.TenantID = strings.TrimSpace(spec.TenantID)
	spec.OwnerUserID = strings.TrimSpace(spec.OwnerUserID)
	spec.LLMThinkingMode = normalizeThinkingLevel(spec.LLMThinkingMode)
	if spec.ID == "" || spec.TenantID == "" || spec.OwnerUserID == "" {
		return errors.New("实例标识不完整")
	}
	if spec.Kind != KindSession && spec.Kind != KindCoding {
		return errors.New("实例类型无效")
	}
	if spec.LLMMode == "" {
		spec.LLMMode = llmOfficial
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if existing := rt.instances[spec.ID]; existing != nil {
		if existing.meta.Kind != spec.Kind {
			return errKind
		}
		spec.LLMMode = existing.meta.LLMMode
		if spec.LLMModel == "" {
			spec.LLMModel = existing.meta.LLMModel
		}
		// The thinking level is owner preference state: a re-provision that
		// does not carry it (older Hub) must not reset a saved level.
		if spec.LLMThinkingMode == "" {
			spec.LLMThinkingMode = existing.meta.LLMThinkingMode
		}
		if err := writeMeta(existing.dir, spec); err != nil {
			return err
		}
		existing.meta = spec
		return nil
	}
	dir := filepath.Join(rt.root, slug(spec.TenantID), slug(spec.OwnerUserID), slug(spec.ID))
	if err := os.MkdirAll(filepath.Join(dir, "workspace"), 0o755); err != nil {
		return err
	}
	if err := writeMeta(dir, spec); err != nil {
		return err
	}
	bk, err := openBook(dir)
	if err != nil {
		return err
	}
	rt.instances[spec.ID] = &instance{dir: dir, meta: spec, book: bk}
	return nil
}

func writeMeta(dir string, spec Spec) error {
	raw, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	if bytesContainSecret(raw, secretValues(dir)...) {
		return errors.New("实例记录不能保存密钥")
	}
	return writeFile(filepath.Join(dir, "meta.json"), raw, 0o644)
}

// bytesContainSecret rejects the official placeholder and any secret loaded
// for this instance. A value shorter than 8 characters is not treated as a
// credential, so a short label cannot block an ordinary name.
func bytesContainSecret(raw []byte, secrets ...string) bool {
	text := string(raw)
	if strings.Contains(text, officialKey) {
		return true
	}
	return valueHasSecret(text, secrets)
}

// Delete keeps the directory, renamed, until the sweep removes it.
func (rt *Runtime) Delete(id string) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	inst := rt.instances[id]
	if inst == nil {
		return errNotFound
	}
	// The tombstone must be unique: a re-provisioned instance reuses the same
	// id, so a second delete would otherwise fail on the existing directory.
	dest := filepath.Join(filepath.Dir(inst.dir), ".deleted-"+id+"-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	// Windows refuses to rename a directory while its sqlite handle is open,
	// so the book closes first; a failed rename reopens it so the instance is
	// not left broken in the map. The workflow store is a second sqlite
	// handle in the same directory and must close for the same reason.
	rt.workflowClose(inst)
	_ = inst.book.Close()
	if err := os.Rename(inst.dir, dest); err != nil {
		if bk, openErr := openBook(inst.dir); openErr == nil {
			inst.book = bk
		}
		return err
	}
	delete(rt.instances, id)
	rt.dropInstance(id)
	return nil
}

// Sweep removes deleted instance directories older than 7 days.
func (rt *Runtime) Sweep(now time.Time) error {
	cutoff := now.Add(-7 * 24 * time.Hour)
	return filepath.WalkDir(rt.root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || !d.IsDir() || !strings.HasPrefix(d.Name(), ".deleted-") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(path)
			return filepath.SkipDir
		}
		return nil
	})
}

func (rt *Runtime) Close() error {
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	stop := rt.stop
	rt.stop = nil
	exec := rt.exec
	rt.exec = nil
	instances := rt.instances
	rt.instances = map[string]*instance{}
	cancels := rt.cancels
	rt.cancels = map[string]trackedCancel{}
	rt.mu.Unlock()
	if stop != nil {
		stop()
	}
	for _, tracked := range cancels {
		tracked.cancel()
		close(tracked.done)
	}
	if exec != nil {
		_ = exec.Close()
	}
	var first error
	for _, inst := range instances {
		rt.workflowClose(inst)
		if err := inst.book.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (rt *Runtime) snapshot() []*instance {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make([]*instance, 0, len(rt.instances))
	for _, inst := range rt.instances {
		out = append(out, inst)
	}
	return out
}

func (rt *Runtime) instance(id string) (*instance, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	inst := rt.instances[strings.TrimSpace(id)]
	if inst == nil {
		return nil, errNotFound
	}
	return inst, nil
}

func (rt *Runtime) track(instanceID, id string, cancel context.CancelFunc) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.cancels[id] = trackedCancel{instanceID: instanceID, cancel: cancel, done: make(chan struct{})}
}

func (rt *Runtime) instanceTurnWaits(instanceID string) []chan struct{} {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	var waits []chan struct{}
	for _, tracked := range rt.cancels {
		if tracked.instanceID == instanceID && tracked.cancel != nil {
			waits = append(waits, tracked.done)
		}
	}
	return waits
}

func (rt *Runtime) trackTurn(inst *instance, id string, cancel context.CancelFunc) {
	rt.track(inst.meta.ID, id, cancel)
	// Admit and cancel are asynchronous. If stop/interrupt won the tiny gap
	// between the durable Begin commit and this tracking step, the context was
	// not yet addressable by cancelIDs; honour the terminal row now instead of
	// letting an untracked turn run anyway.
	if inst != nil && inst.book != nil {
		if status, err := inst.book.Status(id); err == nil && status != "running" {
			cancel()
		}
	}
}

func (rt *Runtime) untrack(id string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if tracked := rt.cancels[id]; tracked.cancel != nil {
		close(tracked.done)
		delete(rt.cancels, id)
	}
}

func (rt *Runtime) cancelIDs(ids []string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, id := range ids {
		if tracked := rt.cancels[id]; tracked.cancel != nil {
			tracked.cancel()
		}
	}
}

// cancelIDsAndDrain cancels the rows a user stopped, then drains parked input
// only after those goroutines have actually unwound. Starting the next turn at
// cancel time could overlap a tool that is still tearing down; never draining
// it would strand the durable queue behind a cancelled row.
func (rt *Runtime) cancelIDsAndDrain(ids []string, inst *instance) {
	if len(ids) == 0 {
		return
	}
	rt.mu.Lock()
	waits := make([]chan struct{}, 0, len(ids))
	for _, id := range ids {
		if tracked := rt.cancels[id]; tracked.cancel != nil {
			tracked.cancel()
			waits = append(waits, tracked.done)
		}
	}
	rt.mu.Unlock()
	if len(waits) == 0 {
		go rt.drainQueue(inst)
		return
	}
	go func() {
		for _, done := range waits {
			<-done
		}
		rt.drainQueue(inst)
	}()
}

// Messages returns the task transcript. cloud_get_task uses this.
func (rt *Runtime) Messages(instanceID, taskID string) ([]string, error) {
	inst, err := rt.instance(instanceID)
	if err != nil {
		return nil, err
	}
	return inst.book.History(taskID)
}

// TurnStatus returns running, finished, interrupted, or cancelled.
func (rt *Runtime) TurnStatus(instanceID, requestID string) (string, error) {
	inst, err := rt.instance(instanceID)
	if err != nil {
		return "", err
	}
	return inst.book.Status(requestID)
}

func (rt *Runtime) idleUnload(after time.Duration) {
	rt.mu.Lock()
	exec := rt.exec
	idle := exec != nil && rt.inFlight == 0 && !rt.lastUsed.IsZero() && time.Since(rt.lastUsed) >= after
	if idle {
		rt.exec = nil
	}
	rt.mu.Unlock()
	if idle {
		_ = exec.Close()
	}
}

func writeFile(path string, raw []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, mode); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmp, path)
}
