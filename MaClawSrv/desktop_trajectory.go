package main

// Desktop run trajectory and budget.
//
// Every desktop tool call of one bot run is one trajectory step. The step's
// action (arguments redacted exactly like the [desktop-step] log line), its
// outcome, its duration, and the screenshot the call returned are written
// into the bot's desktop under ~/素材/<bot>/runs/<run id>/ so the person can
// open the run later and see what the bot saw and did. The idea follows
// cua's TrajectorySaverCallback and BudgetManagerCallback (MIT,
// https://github.com/trycua/cua, libs/python/agent/cua_agent/callbacks);
// no code is copied.
//
// The run budget caps how many desktop actions one run may take and how
// many image tokens its screenshots may cost the model. When either is
// spent, the next call returns an error telling the model to stop and
// report, instead of clicking on until the loop's own iteration cap.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

const (
	desktopMaxStepsEnv        = "MACLAW_DESKTOP_MAX_STEPS"
	desktopRunMaxActionsEnv   = "MACLAW_DESKTOP_RUN_MAX_ACTIONS"
	desktopRunMaxImageTokEnv  = "MACLAW_DESKTOP_RUN_MAX_IMAGE_TOKENS"
	desktopTrajectoryEnv      = "MACLAW_DESKTOP_TRAJECTORY"
	desktopDefaultMaxSteps    = 16
	desktopDefaultRunActions  = 200
	desktopDefaultRunImageTok = 2_000_000
	desktopTrajectoryChunk    = 150 << 10
	desktopRunStateTTL        = 6 * time.Hour
)

// desktopMaxBatchSteps bounds one task_run or app_run batch. It was the
// constant 16; MACLAW_DESKTOP_MAX_STEPS (1 to 64) changes it.
func desktopMaxBatchSteps() int {
	return desktopEnvInt(desktopMaxStepsEnv, desktopDefaultMaxSteps, 1, 64)
}

// desktopRunMaxActions is the per-run desktop action cap (0 = no cap).
func desktopRunMaxActions() int {
	return desktopEnvInt(desktopRunMaxActionsEnv, desktopDefaultRunActions, 0, 100000)
}

// desktopRunMaxImageTokens is the per-run screenshot token budget (0 = no cap).
func desktopRunMaxImageTokens() int {
	return desktopEnvInt(desktopRunMaxImageTokEnv, desktopDefaultRunImageTok, 0, 1<<30)
}

func desktopTrajectoryEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(desktopTrajectoryEnv))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

func desktopEnvInt(name string, def, min, max int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// desktopImageTokens estimates what one image costs a vision model, about
// one token per 750 pixels (the common OpenAI/Anthropic rule of thumb).
func desktopImageTokens(b64 string) int {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) == 0 {
		return 0
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0
	}
	return cfg.Width*cfg.Height/750 + 85
}

type desktopRunState struct {
	mu          sync.Mutex
	actions     int
	imageTokens int
	seq         int
	lastSeen    time.Time
	queue       chan desktopTrajectoryStep
	dir         string
	scope       agentruntime.Scope
	cloud       bool
}

type desktopTrajectoryStep struct {
	Seq        int               `json:"seq"`
	At         string            `json:"at"`
	Action     string            `json:"action"`
	Args       map[string]string `json:"args,omitempty"`
	OK         bool              `json:"ok"`
	Error      string            `json:"error,omitempty"`
	Result     string            `json:"result,omitempty"`
	TookMS     int64             `json:"took_ms"`
	Image      string            `json:"image,omitempty"`
	ImageToken int               `json:"image_tokens,omitempty"`
	imageB64   string
}

var desktopTrajRuns sync.Map // run key -> *desktopRunState

func desktopTrajRunKey(scope agentruntime.Scope) string {
	run := strings.TrimSpace(scope.RunID)
	if run == "" {
		return ""
	}
	return scope.TenantID + "\x00" + scope.UserID + "\x00" + run
}

// desktopMaterialsFolderKey carries the Hub-chosen materials folder of this
// bot (instance metadata hub_materials_folder) into the trajectory writer.
type desktopMaterialsFolderKey struct{}

const desktopMaterialsMetadataKey = "hub_materials_folder"

func desktopRunFor(scope agentruntime.Scope) *desktopRunState {
	return desktopRunForFolder(scope, "")
}

func desktopRunForFolder(scope agentruntime.Scope, folder string) *desktopRunState {
	key := desktopTrajRunKey(scope)
	if key == "" {
		return nil
	}
	now := time.Now()
	if v, ok := desktopTrajRuns.Load(key); ok {
		st := v.(*desktopRunState)
		st.mu.Lock()
		st.lastSeen = now
		st.mu.Unlock()
		return st
	}
	desktopTrajRunsGC(now)
	st := &desktopRunState{lastSeen: now, scope: scope, dir: desktopTrajectoryDir(scope, folder)}
	actual, _ := desktopTrajRuns.LoadOrStore(key, st)
	return actual.(*desktopRunState)
}

func desktopTrajRunsGC(now time.Time) {
	desktopTrajRuns.Range(func(k, v any) bool {
		st := v.(*desktopRunState)
		st.mu.Lock()
		stale := now.Sub(st.lastSeen) > desktopRunStateTTL
		st.mu.Unlock()
		if stale {
			desktopTrajRuns.Delete(k)
		}
		return true
	})
}

var desktopTrajectoryUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func desktopTrajectorySafe(s string) string {
	s = desktopTrajectoryUnsafe.ReplaceAllString(strings.TrimSpace(s), "_")
	s = strings.Trim(s, "._")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// desktopTrajectoryDir is ~/素材/<bot>/runs/<run id> in the desktop. A
// person's own (non-bot) desktop uses "default" for the bot folder.
func desktopTrajectoryDir(scope agentruntime.Scope, folder string) string {
	bot := ""
	if folder = strings.TrimSpace(folder); folder != "" && desktop.SafeMaterialName(folder) == folder {
		if _, err := desktop.MaterialPath(folder, "runs"); err == nil {
			bot = folder
		}
	}
	if bot == "" {
		_, id := desktop.SplitDesktopUser(scope.UserID)
		bot = desktopTrajectorySafe(id)
	}
	if bot == "" {
		bot = "default"
	}
	run := desktopTrajectorySafe(scope.RunID)
	if run == "" {
		return ""
	}
	return "~/素材/" + bot + "/runs/" + run
}

// desktopBudgetCheck refuses the call when the run has spent its budget.
func desktopBudgetCheck(scope agentruntime.Scope, action string) error {
	st := desktopRunFor(scope)
	if st == nil || action == "request_takeover" || action == "deliver" {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if max := desktopRunMaxActions(); max > 0 && st.actions >= max {
		return fmt.Errorf("desktop run budget spent: %d desktop actions in this run (limit %d). Stop operating the desktop now and tell the person what was done and what is left", st.actions, max)
	}
	if max := desktopRunMaxImageTokens(); max > 0 && st.imageTokens >= max {
		return fmt.Errorf("desktop run budget spent: screenshots in this run cost about %d image tokens (limit %d). Stop operating the desktop now and tell the person what was done and what is left", st.imageTokens, max)
	}
	return nil
}

// desktopRecordStep counts the call against the run budget and queues its
// trajectory step. Writing happens in the background; a failed write only
// logs, it never fails the bot's action.
func desktopRecordStep(ctx context.Context, scope agentruntime.Scope, args map[string]any, out string, err error, took time.Duration) {
	folder := ""
	if ctx != nil {
		folder, _ = ctx.Value(desktopMaterialsFolderKey{}).(string)
	}
	st := desktopRunForFolder(scope, folder)
	if st == nil {
		return
	}
	action := strings.ToLower(strings.TrimSpace(desktopArg(args, "action")))
	if !desktopActionAllowed(action) {
		action = "invalid"
	}
	plain, images := agentruntime.ExtractModelImages(out)
	step := desktopTrajectoryStep{
		At:     time.Now().UTC().Format(time.RFC3339Nano),
		Action: action,
		Args:   desktopTrajectoryArgs(args),
		OK:     err == nil,
		TookMS: took.Milliseconds(),
		Result: desktopStepClip(plain, 600),
	}
	if err != nil {
		step.Error = desktopStepClip(err.Error(), 300)
	}
	if n := len(images); n > 0 {
		img := images[n-1]
		step.imageB64 = img.Base64
		step.ImageToken = desktopImageTokens(img.Base64)
		ext := "png"
		if strings.Contains(img.MIME, "jpeg") {
			ext = "jpg"
		} else if strings.Contains(img.MIME, "webp") {
			ext = "webp"
		}
		step.Image = ext
	}
	tokens := 0
	for _, img := range images {
		tokens += desktopImageTokens(img.Base64)
	}
	st.mu.Lock()
	st.actions++
	st.imageTokens += tokens
	st.seq++
	step.Seq = st.seq
	if step.Image != "" {
		step.Image = fmt.Sprintf("step-%03d.%s", step.Seq, step.Image)
	}
	if !desktopTrajectoryEnabled() || st.dir == "" {
		st.mu.Unlock()
		return
	}
	if st.queue == nil {
		st.queue = make(chan desktopTrajectoryStep, 64)
		if ctx != nil {
			st.cloud, _ = ctx.Value(desktopCloudKey{}).(bool)
		}
		go desktopTrajectoryWriter(st.scope, st.cloud, st.dir, st.queue)
	}
	q := st.queue
	st.mu.Unlock()
	select {
	case q <- step:
	default:
		log.Printf("[desktop-trajectory] queue full, dropped step %d", step.Seq)
	}
}

// desktopTrajectoryArgs keeps the same arguments the step log keeps. Free
// text is reduced to its length so typed text and secrets are not stored.
func desktopTrajectoryArgs(args map[string]any) map[string]string {
	out := map[string]string{}
	keys := make([]string, 0, len(args))
	for k := range args {
		if k != "action" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch {
		case desktopStepLogKeys[k]:
			v := fmt.Sprint(args[k])
			if k == "url" {
				if i := strings.IndexAny(v, "?#"); i >= 0 {
					v = v[:i] + "?…"
				}
			}
			out[k] = desktopStepClip(v, 200)
		case desktopStepLogLenOnly[k]:
			out[k] = fmt.Sprintf("<%d chars>", len(fmt.Sprint(args[k])))
		default:
			out[k] = "…"
		}
	}
	return out
}

// desktopTrajectoryFile and desktopTrajectoryBash are the container file
// write and bash used by the writer. Tests replace them.
var (
	desktopTrajectoryFile = func(ctx context.Context, scope agentruntime.Scope, path, content string) error {
		_, err := desktopContainerFile(ctx, scope, "file_write", map[string]any{"path": path, "content": content})
		return err
	}
	desktopTrajectoryBash = func(ctx context.Context, scope agentruntime.Scope, command string) error {
		_, err := desktopContainerBash(ctx, scope, command)
		return err
	}
)

func desktopTrajectoryWriter(scope agentruntime.Scope, cloud bool, dir string, q chan desktopTrajectoryStep) {
	for {
		select {
		case step := <-q:
			base := context.WithValue(context.Background(), desktopCloudKey{}, cloud)
			ctx, cancel := context.WithTimeout(base, 2*time.Minute)
			if err := writeDesktopTrajectoryStep(ctx, scope, dir, step); err != nil {
				log.Printf("[desktop-trajectory] step %d not saved: %v", step.Seq, err)
			}
			cancel()
		case <-time.After(10 * time.Minute):
			// Idle: a new writer starts with the next step.
			key := desktopTrajRunKey(scope)
			if v, ok := desktopTrajRuns.Load(key); ok {
				st := v.(*desktopRunState)
				st.mu.Lock()
				if st.queue == q && len(q) == 0 {
					st.queue = nil
					st.mu.Unlock()
					return
				}
				st.mu.Unlock()
			} else {
				return
			}
		}
	}
}

func writeDesktopTrajectoryStep(ctx context.Context, scope agentruntime.Scope, dir string, step desktopTrajectoryStep) error {
	meta, err := json.MarshalIndent(step, "", "  ")
	if err != nil {
		return err
	}
	if err := desktopTrajectoryFile(ctx, scope, fmt.Sprintf("%s/step-%03d.json", dir, step.Seq), string(meta)+"\n"); err != nil {
		return err
	}
	if step.imageB64 == "" || step.Image == "" {
		return nil
	}
	// The file API carries text. The picture goes as base64 parts that the
	// container decodes into the image and then removes.
	b64 := step.imageB64
	var parts []string
	for i := 0; len(b64) > 0; i++ {
		n := desktopTrajectoryChunk
		if n > len(b64) {
			n = len(b64)
		}
		part := fmt.Sprintf("%s/.step-%03d.b64.%03d", dir, step.Seq, i)
		if err := desktopTrajectoryFile(ctx, scope, part, b64[:n]); err != nil {
			return err
		}
		parts = append(parts, part)
		b64 = b64[n:]
	}
	abs, err := desktop.ContainerPath(dir)
	if err != nil {
		return err
	}
	// docker exec runs as root. The desktop user must own the run folder
	// like uploaded materials, or Thunar cannot tidy it later.
	cmd := fmt.Sprintf("cd '%s' && cat .step-%03d.b64.* | base64 -d > '%s' && rm -f .step-%03d.b64.* && (chown desktop:desktop '%s' '%s' '%s' . step-%03d.* 2>/dev/null || true)",
		abs, step.Seq, step.Image, step.Seq, desktop.MaterialsRoot, path.Dir(path.Dir(abs)), path.Dir(abs), step.Seq)
	return desktopTrajectoryBash(ctx, scope, cmd)
}
