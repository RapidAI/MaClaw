package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime/promptparts"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/botregistry"
)

// Tool names the main bot's LLM sees.
const (
	toolListBots     = "list_bots"
	toolGetBotStatus = "get_bot_status"
	toolSendToBot    = "send_to_bot"
	toolAssignTask   = "assign_task"
	toolStopBot      = "stop_bot"
	toolPauseBot     = "pause_bot"
	toolResumeBot    = "resume_bot"
	toolBotEvents    = "bot_events"
	toolShareFile    = "share_file_with_bot"
	toolListArtifact = "list_bot_artifacts"
)

func multiBotToolNames() []string {
	return []string{toolListBots, toolGetBotStatus, toolSendToBot, toolAssignTask, toolStopBot, toolPauseBot, toolResumeBot, toolBotEvents, toolShareFile, toolListArtifact}
}

const multiBotNeedsConfirm = "NEEDS_CONFIRMATION"

func mbJSON(v any) string {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "error: " + err.Error()
	}
	return string(raw)
}

func mbArg(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok && v != nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

func mbBool(args map[string]interface{}, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || v == "1"
	}
	return false
}

func mbList(args map[string]interface{}, key string) []string {
	var out []string
	switch v := args[key].(type) {
	case []interface{}:
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
	case []string:
		for _, s := range v {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	case string:
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func (mb *multiBot) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

func (mb *multiBot) self() botregistry.Bot {
	b, _ := mb.reg.Get(multiBotSelfID)
	return b
}

// resolveOne refreshes when the name is unknown, so a bot created a moment
// ago is still found.
func (mb *multiBot) resolve(target string) ([]botregistry.Bot, error) {
	if bots, ok := mb.reg.Resolve(target); ok {
		return bots, nil
	}
	ctx, cancel := mb.ctx()
	defer cancel()
	if err := mb.refresh(ctx, true); err != nil {
		return nil, err
	}
	if bots, ok := mb.reg.Resolve(target); ok {
		return bots, nil
	}
	names := []string{}
	for _, b := range mb.reg.List() {
		names = append(names, b.Name+" ("+b.ID+")")
	}
	return nil, fmt.Errorf("no single bot matches %q; bots: %s", target, strings.Join(names, ", "))
}

func (mb *multiBot) toolListBots(args map[string]interface{}) string {
	ctx, cancel := mb.ctx()
	defer cancel()
	err := mb.refresh(ctx, mbBool(args, "refresh"))
	mb.ensureWatch()
	out := map[string]any{"bots": mb.reg.List(), "unread_events": len(mb.unread(true))}
	if err != nil {
		out["hub_error"] = err.Error()
	}
	return mbJSON(out)
}

func (mb *multiBot) toolGetBotStatus(args map[string]interface{}) string {
	ctx, cancel := mb.ctx()
	defer cancel()
	_ = mb.refresh(ctx, true)
	bots, err := mb.resolve(mbArg(args, "bot"))
	if err != nil {
		return "error: " + err.Error()
	}
	return mbJSON(bots)
}

func (mb *multiBot) confirmGate(target botregistry.Bot, action botregistry.Action, text string, confirmed bool) (string, bool) {
	d := botregistry.Check(mb.self(), target, action, text)
	if !d.Allowed {
		return "refused: " + d.Reason, false
	}
	if d.NeedsConfirm && !confirmed {
		return fmt.Sprintf("%s: %s (%s %s). Ask the person; call again with confirm=true only after they say yes.", multiBotNeedsConfirm, d.Reason, action, target.Name), false
	}
	return "", true
}

func (mb *multiBot) toolSendToBot(args map[string]interface{}) string {
	text := clipRunes(mbArg(args, "text"), desktopBotUnderstandToldMax)
	if text == "" {
		return "error: text is required"
	}
	bots, err := mb.resolve(mbArg(args, "bot"))
	if err != nil {
		return "error: " + err.Error()
	}
	ctx, cancel := mb.ctx()
	defer cancel()
	replyTo := mbArg(args, "reply_to")
	if arts := mbList(args, "artifact_ids"); len(arts) > 0 {
		text += "\n[files: " + strings.Join(arts, ", ") + " — ask the main bot to share them if you need them]"
	}
	wire := text
	if replyTo != "" {
		wire = "[re " + replyTo + "] " + text
	}
	if replyTo != "" && len(bots) != 1 {
		return "error: reply_to answers one bot"
	}
	sent := []string{}
	for _, b := range bots {
		if b.ID == multiBotSelfID {
			continue
		}
		if msg, ok := mb.confirmGate(b, botregistry.ActionNotify, text, mbBool(args, "confirm")); !ok {
			return msg
		}
		// The bus is the loop guard: a reply chain or a burst between the
		// main bot and one teammate stops here until the person speaks.
		if _, err := mb.bus.Submit(botregistry.Message{From: multiBotSelfID, To: b.ID, Kind: botregistry.NormalizeKind(firstNonEmptyMB(mbArg(args, "kind"), map[bool]string{true: "reply", false: "info"}[replyTo != ""])), Text: text, ReplyTo: replyTo}); err != nil {
			if errors.Is(err, botregistry.ErrDuplicate) {
				return "already sent the same message to " + b.Name + " just now; not sent again"
			}
			return "not sent: " + err.Error() + ". Tell the person instead of continuing the back-and-forth."
		}
		relay, err := mb.hubRelay(ctx)
		if err != nil {
			return "error: " + err.Error()
		}
		// Hub is the broker: the message goes into the bot's Hub mailbox,
		// which its MaClawSrv run reads itself. An older Hub without
		// reports gets the legacy notify note.
		if nh := mb.hubNotes(); nh != nil {
			kind := firstNonEmptyMB(mbArg(args, "kind"), map[bool]string{true: "reply", false: "info"}[replyTo != ""])
			if _, err := nh.report(ctx, relay, b.ID, string(botregistry.NormalizeKind(kind)), text, replyTo, mbList(args, "artifact_ids")); err != nil {
				return "error: " + err.Error()
			}
		} else if err := mb.hub.cross(ctx, relay, "notify", b.ID, wire, ""); err != nil {
			return "error: " + err.Error()
		}
		mb.reg.Notify(botregistry.EventMessage, b.ID, "main bot → "+b.Name+": "+text)
		sent = append(sent, b.Name)
	}
	if len(sent) == 0 {
		return "error: nothing to send (the main bot does not message itself)"
	}
	return "sent to " + strings.Join(sent, ", ") + ". It waits in their Hub mailbox; they read it at their next turn or step."
}

// toolAssignTask hands a task to a bot. Files named by artifact_ids or
// local_paths are moved to a remote bot's desktop first, and the order tells
// it where they are. A paused bot holds the order until resume_bot.
func (mb *multiBot) toolAssignTask(args map[string]interface{}) string {
	task := clipDesktopBotOrder(mbArg(args, "task"), desktopBotUnderstandOrderMax)
	if task == "" {
		return "error: task is required"
	}
	bots, err := mb.resolve(mbArg(args, "bot"))
	if err != nil {
		return "error: " + err.Error()
	}
	if len(bots) != 1 {
		return "error: assign one bot at a time"
	}
	b := bots[0]
	if b.ID == multiBotSelfID {
		return "this is the main bot itself: do the task directly with your own tools (local files, shell, HTTP)."
	}
	if msg, ok := mb.confirmGate(b, botregistry.ActionAssign, task, mbBool(args, "confirm")); !ok {
		return msg
	}
	ctx, cancel := mb.ctx()
	defer cancel()
	var placed []string
	for _, p := range mb.artifactPaths(args) {
		if p.err != nil {
			return "error: " + p.err.Error()
		}
		remote, err := mb.hub.uploadFile(ctx, b.ID, p.path)
		if err != nil {
			return "error: upload " + p.name + ": " + err.Error()
		}
		placed = append(placed, remote)
		mb.reg.Notify(botregistry.EventArtifact, b.ID, "sent "+p.name+" → "+remote)
	}
	order := task
	if len(placed) > 0 {
		order += "\n\nFiles from the main bot are on your desktop: " + strings.Join(placed, ", ")
	}
	if mb.reg.Hold(b.ID, order) {
		return b.Name + " is paused; the task is held and starts when you call resume_bot."
	}
	return mb.dispatch(ctx, b, order, mbArg(args, "note"))
}

func (mb *multiBot) dispatch(ctx context.Context, b botregistry.Bot, order, note string) string {
	relay, err := mb.hubRelay(ctx)
	if err != nil {
		return "error: " + err.Error()
	}
	if note == "" {
		note = clipRunes(order, desktopBotUnderstandToldMax)
	}
	if err := mb.hub.cross(ctx, relay, "delegate", b.ID, note, order); err != nil {
		return "error: " + err.Error()
	}
	mb.reg.Update(b.ID, func(x *botregistry.Bot) {
		if x.State == botregistry.StateIdle {
			x.State = botregistry.StateQueued
			x.Task = clipRunes(order, 120)
		}
	})
	mb.ensureWatch()
	return "handed to " + b.Name + ". It starts on its next turn; you will get a bot_events entry when it finishes, fails or needs the person. Keep helping the person meanwhile."
}

type mbPath struct {
	name, path string
	err        error
}

func (mb *multiBot) artifactPaths(args map[string]interface{}) []mbPath {
	var out []mbPath
	for _, id := range mbList(args, "artifact_ids") {
		if mb.artifacts == nil {
			out = append(out, mbPath{err: fmt.Errorf("artifact store unavailable")})
			continue
		}
		a, ok := mb.artifacts.Get(id)
		if !ok {
			out = append(out, mbPath{err: fmt.Errorf("unknown artifact %s", id)})
			continue
		}
		out = append(out, mbPath{name: a.Name, path: a.Path})
	}
	for _, p := range mbList(args, "local_paths") {
		st, err := os.Stat(p)
		if err != nil {
			out = append(out, mbPath{err: err})
			continue
		}
		if err := botregistry.CheckOutboundPath(p, st.Size()); err != nil {
			out = append(out, mbPath{err: err})
			continue
		}
		out = append(out, mbPath{name: st.Name(), path: p})
	}
	return out
}

func (mb *multiBot) toolStopBot(args map[string]interface{}) string {
	bots, err := mb.resolve(mbArg(args, "bot"))
	if err != nil {
		return "error: " + err.Error()
	}
	ctx, cancel := mb.ctx()
	defer cancel()
	_ = mb.refresh(ctx, true)
	stopped := []string{}
	for _, b := range bots {
		if b.ID == multiBotSelfID {
			continue
		}
		b, _ = mb.reg.Get(b.ID)
		if msg, ok := mb.confirmGate(b, botregistry.ActionStop, "", mbBool(args, "confirm")); !ok {
			return msg
		}
		runs, err := mb.hub.activeRuns(ctx, b.ID)
		if err != nil {
			return "error: " + err.Error()
		}
		want := mbArg(args, "run_id")
		for _, r := range runs {
			if r.RunID == "" || (want != "" && r.RunID != want) {
				continue
			}
			if err := mb.hub.cancelRun(ctx, b.ID, r.RunID); err != nil {
				return "error: " + err.Error()
			}
			stopped = append(stopped, b.Name+" "+r.RunID)
			mb.reg.Notify(botregistry.EventStopped, b.ID, r.RunID)
		}
	}
	if len(stopped) == 0 {
		return "nothing was running."
	}
	_ = mb.refresh(ctx, true)
	return "stopped: " + strings.Join(stopped, ", ")
}

func (mb *multiBot) toolPauseResume(args map[string]interface{}, pause bool) string {
	bots, err := mb.resolve(mbArg(args, "bot"))
	if err != nil {
		return "error: " + err.Error()
	}
	ctx, cancel := mb.ctx()
	defer cancel()
	out := []string{}
	for _, b := range bots {
		action := botregistry.ActionResume
		if pause {
			action = botregistry.ActionPause
		}
		if msg, ok := mb.confirmGate(b, action, "", true); !ok {
			return msg
		}
		if pause {
			mb.reg.Pause(b.ID)
			out = append(out, b.Name+" "+mb.pauseRemoteRuns(ctx, b))
			continue
		}
		resumed := mb.resumeRemoteRuns(ctx, b)
		held, _ := mb.reg.Resume(b.ID)
		line := b.Name + " resumed" + resumed
		for _, order := range held {
			line += "; " + mb.dispatch(ctx, b, order, "")
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// pauseRemoteRuns pauses a remote bot's running runs at their next step
// boundary on the server. The registry hold (new orders wait) is applied by
// the caller either way, so an older Hub still gets the soft pause.
func (mb *multiBot) pauseRemoteRuns(ctx context.Context, b botregistry.Bot) string {
	if b.Location != botregistry.LocationRemote {
		return "paused (new tasks are held)"
	}
	runs, err := mb.hub.activeRuns(ctx, b.ID)
	if err != nil {
		return "paused locally only (could not read its runs: " + err.Error() + "); new tasks are held"
	}
	n := 0
	for _, r := range runs {
		if r.RunID == "" || r.Paused || strings.EqualFold(r.Status, "queued") {
			continue
		}
		if err := mb.hub.pauseRun(ctx, b.ID, r.RunID, "paused by the main bot"); err != nil {
			if errors.Is(err, errDesktopBotPauseUnsupported) {
				return "paused softly: this Hub cannot hold a running task yet, so the current run finishes; new tasks are held"
			}
			return "pause failed for run " + r.RunID + ": " + err.Error() + "; new tasks are held"
		}
		n++
	}
	mb.mu.Lock()
	mb.lastRefresh = time.Time{}
	mb.mu.Unlock()
	if n == 0 {
		return "paused (nothing running; new tasks are held)"
	}
	return fmt.Sprintf("paused: %d running task(s) will hold at the next step boundary (the server cancels a pause left open 30 min); new tasks are held", n)
}

// resumeRemoteRuns releases runs Hub reports as paused.
func (mb *multiBot) resumeRemoteRuns(ctx context.Context, b botregistry.Bot) string {
	if b.Location != botregistry.LocationRemote {
		return ""
	}
	runs, err := mb.hub.activeRuns(ctx, b.ID)
	if err != nil {
		return ""
	}
	n := 0
	for _, r := range runs {
		if !r.Paused || r.RunID == "" {
			continue
		}
		if err := mb.hub.resumeRun(ctx, b.ID, r.RunID); err == nil {
			n++
		}
	}
	mb.mu.Lock()
	mb.lastRefresh = time.Time{}
	mb.mu.Unlock()
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d held task(s) continue where they stopped)", n)
}

func (mb *multiBot) toolBotEvents(args map[string]interface{}) string {
	ctx, cancel := mb.ctx()
	defer cancel()
	_ = mb.refresh(ctx, false)
	evs := mb.unread(mbBool(args, "peek"))
	return mbJSON(map[string]any{"events": evs})
}

func (mb *multiBot) toolShareFile(args map[string]interface{}) string {
	if mb.artifacts == nil {
		return "error: artifact store unavailable"
	}
	path := mbArg(args, "path")
	a, err := mb.artifacts.PutFile(path, multiBotSelfID)
	if err != nil {
		return "error: " + err.Error()
	}
	if target := mbArg(args, "bot"); target != "" {
		bots, err := mb.resolve(target)
		if err != nil {
			return "error: " + err.Error()
		}
		ctx, cancel := mb.ctx()
		defer cancel()
		for _, b := range bots {
			if msg, ok := mb.confirmGate(b, botregistry.ActionTransfer, mbArg(args, "note"), mbBool(args, "confirm")); !ok {
				return msg
			}
			remote, err := mb.hub.uploadFile(ctx, b.ID, a.Path)
			if err != nil {
				return "error: " + err.Error()
			}
			mb.reg.Notify(botregistry.EventArtifact, b.ID, "sent "+a.Name+" → "+remote)
			if note := mbArg(args, "note"); note != "" {
				if relay, err := mb.hubRelay(ctx); err == nil {
					_ = mb.hub.cross(ctx, relay, "notify", b.ID, note+" ("+remote+")", "")
				}
			}
			return mbJSON(map[string]any{"artifact": a, "bot": b.Name, "remote_path": remote})
		}
	}
	return mbJSON(map[string]any{"artifact": a})
}

func (mb *multiBot) toolListArtifacts(args map[string]interface{}) string {
	if mb.artifacts == nil {
		return "error: artifact store unavailable"
	}
	items := mb.artifacts.List()
	if len(items) > 30 {
		items = items[:30]
	}
	return mbJSON(map[string]any{"artifacts": items})
}

// registerMultiBotTools adds the team tools to the main bot's tool registry.
func registerMultiBotTools(registry *ToolRegistry, mb *multiBot) {
	if registry == nil || mb == nil {
		return
	}
	str := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "description": desc}
	}
	boolean := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "boolean", "description": desc}
	}
	arr := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": desc}
	}
	botArg := str("Bot id or exact name; \"all\" for every other bot")
	confirm := boolean("Set true only after the person approved an action this tool flagged NEEDS_CONFIRMATION")
	add := func(name, desc string, props map[string]interface{}, required []string, fn func(map[string]interface{}) string) {
		_ = registry.Register(RegisteredTool{
			Name:        name,
			Description: desc,
			Category:    ToolCategoryBuiltin,
			Tags:        []string{"bots", "team", "multibot"},
			Status:      RegToolAvailable,
			InputSchema: props,
			Required:    required,
			Source:      "builtin",
			Handler:     fn,
		})
	}
	add(toolListBots, "List every bot on this account (you, the main bot, first): where it runs (local GUI or remote desktop), capabilities, state (idle/running/waiting_user/queued/paused/error/offline), task, current step.",
		map[string]interface{}{"refresh": boolean("Force a fresh read from Hub")}, nil, mb.toolListBots)
	add(toolGetBotStatus, "Live status of one bot: state, task, current/next step, run id, why it stopped, queue.",
		map[string]interface{}{"bot": botArg}, []string{"bot"}, mb.toolGetBotStatus)
	add(toolSendToBot, "Send a short message to another bot, or answer a teammate message (pass reply_to=its id from the teammate-messages turn). It sees it at the start of its next turn; its current turn is not interrupted. Teammates message you the same way (their done/blocked/question/file reports wake you). Loops are capped: if a send is refused, tell the person instead.",
		map[string]interface{}{"bot": botArg, "text": str("The message"), "kind": map[string]interface{}{"type": "string", "enum": []string{"info", "reply", "question", "done", "blocked", "artifact", "progress"}, "description": "Optional message kind (default info, or reply with reply_to)"}, "reply_to": str("Optional teammate message id this answers"), "artifact_ids": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional artifact ids to mention"}, "confirm": confirm}, []string{"bot", "text"}, mb.toolSendToBot)
	add(toolAssignTask, "Hand a task to another bot (e.g. a remote desktop bot to publish in a browser). Optional files (artifact ids or local paths) are copied to its desktop first. Returns immediately; results arrive via bot_events. Do the parts that need this computer's files/shell yourself."+mainBotLeadHint(),
		map[string]interface{}{"bot": botArg, "task": str("What the bot should do, self-contained"), "note": str("Optional one-line note shown to that bot"), "artifact_ids": arr("Artifacts to send along"), "local_paths": arr("Local files to send along"), "confirm": confirm},
		[]string{"bot", "task"}, mb.toolAssignTask)
	add(toolStopBot, "Stop another bot's current run (or one run_id).",
		map[string]interface{}{"bot": botArg, "run_id": str("Optional run id"), "confirm": confirm}, []string{"bot"}, mb.toolStopBot)
	add(toolPauseBot, "Pause a bot: its current run finishes, new tasks are held until resume_bot.",
		map[string]interface{}{"bot": botArg}, []string{"bot"}, func(a map[string]interface{}) string { return mb.toolPauseResume(a, true) })
	add(toolResumeBot, "Resume a paused bot and start the tasks held for it.",
		map[string]interface{}{"bot": botArg}, []string{"bot"}, func(a map[string]interface{}) string { return mb.toolPauseResume(a, false) })
	add(toolBotEvents, "Unread team events: a bot started, finished, failed, needs the person, sent a file. Check after handing work off and before telling the person how things are going.",
		map[string]interface{}{"peek": boolean("Do not mark as read")}, nil, mb.toolBotEvents)
	add(toolShareFile, "Register a local file as an artifact and optionally copy it to a bot's desktop. Secret-looking files (.env, keys, .ssh) are refused.",
		map[string]interface{}{"path": str("Local file path"), "bot": botArg, "note": str("Optional message to that bot"), "confirm": confirm}, []string{"path"}, mb.toolShareFile)
	add(toolListArtifact, "List files passed between bots (remote outputs saved locally, local files shared), newest first, with ids and local paths.",
		map[string]interface{}{}, nil, mb.toolListArtifacts)
}

// mainBotLeadHint is the style layer's lead part for the main bot, attached
// to the tool it uses to hand out work ("" when MACLAW_STYLE_LAYER=off).
func mainBotLeadHint() string {
	if t := strings.TrimSpace(promptparts.Render(nil, promptparts.MainBotLead)); t != "" {
		return " " + t
	}
	return ""
}
