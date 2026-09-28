package guiapp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// Connect mode is the session-less variant of the ssh adapter: it must
// publish exactly when there is no runtime binding and no live session, so a
// turn that needs to open a connection always has a provider to plan or
// petition against.
func TestCompactSSHPtyOutputDropsPaddingAndANSI(t *testing.T) {
	raw := "\x1b[?2004l\r=== UPTIME ===\n 03:35:30 up 18 min\n" +
		"tcp   LISTEN 0      511          0.0.0.0:80         0.0.0.0:*    users:((\"nginx\",pid=969,fd=21))                                                                                             \n" +
		"\x1b]0;root@racknerd: ~\x07root@racknerd:~#"
	got := compactSSHPtyOutput(raw)
	if strings.Contains(got, "\x1b") {
		t.Fatalf("ANSI left in output: %q", got)
	}
	var listen string
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "nginx") {
			listen = line
		}
	}
	if listen == "" || strings.HasSuffix(listen, " ") {
		t.Fatalf("trailing PTY padding left in place: %q", got)
	}
	if !strings.Contains(listen, "tcp   LISTEN") {
		t.Fatalf("column spacing inside the line was collapsed: %q", listen)
	}
	if !strings.Contains(got, "UPTIME") || !strings.Contains(got, "root@racknerd:~#") {
		t.Fatalf("metrics were removed: %q", got)
	}
}

func TestStripLeadingCommandEchoKeepsCommandOutput(t *testing.T) {
	command := `echo "=== UPTIME ==="; uptime; echo "=== DISK ==="; df -h /`
	echo := command
	if len(echo) > 40 {
		echo = command[:40] + "\n" + command[40:]
	}
	output := echo + "\n=== UPTIME ===\n 18 min\n=== DISK ===\n/dev/vda2 52%\nroot@host:~#"
	got := stripLeadingCommandEcho(output, command)
	if strings.Contains(got, `echo "=== UPTIME ==="`) {
		t.Fatalf("command echo still present: %q", got)
	}
	if !strings.Contains(got, "=== UPTIME ===") || !strings.Contains(got, "52%") || !strings.Contains(got, "root@host:~#") {
		t.Fatalf("command output was removed: %q", got)
	}

	prompted := "root@racknerd:~# " + command + "\nup 18 min\nroot@racknerd:~#"
	got = stripLeadingCommandEcho(prompted, command)
	if strings.Contains(got, "echo ") || !strings.Contains(got, "up 18 min") {
		t.Fatalf("prompted echo strip = %q", got)
	}

	plain := "up 18 min\nroot@host:~#"
	if stripLeadingCommandEcho(plain, command) != plain {
		t.Fatal("output that is not an echo must stay unchanged")
	}
}

func TestSSHConnectResultForFollowUpCommandDropsLoginPreview(t *testing.T) {
	raw := "SSH 连接成功\n会话 ID: ssh_1\n主机: root@api2.maclaw.top:22\n状态: running\n\n--- 初始输出 ---\n" +
		strings.Repeat("MOTD line\n", 40) + "root@host:~# for f in /tmp/maclaw_bg_*.pid; do echo MACLAW_ORPHAN; done\n"
	got := sshConnectResultForFollowUpCommand(raw)
	if strings.Contains(got, "初始输出") || strings.Contains(got, "MOTD") || strings.Contains(got, "MACLAW_ORPHAN") {
		t.Fatalf("login preview still attached: %q", got)
	}
	if !strings.Contains(got, "SSH 连接成功") || !strings.Contains(got, "ssh_1") {
		t.Fatalf("session header was removed: %q", got)
	}
	reused := "复用已有 SSH 会话\n会话 ID: ssh_1\n主机: root@api2:22\n状态: running\n\n最近输出: root@host:~# "
	if got := sshConnectResultForFollowUpCommand(reused); strings.Contains(got, "最近输出") || !strings.Contains(got, "ssh_1") {
		t.Fatalf("reused session preview = %q", got)
	}
}

func TestSemanticTrustedSSHConnectPublishedGate(t *testing.T) {
	if semanticTrustedSSHConnectPublished(nil) {
		t.Fatal("nil handler must not publish connect mode")
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	if !semanticTrustedSSHConnectPublished(h) {
		t.Fatal("session-less handler without runtime binding must publish connect mode")
	}
	if semanticTrustedSSHPublished(h) {
		t.Fatal("same handler must not publish exec mode without a bound session")
	}
	h.semanticTrustedSSH = func(string, string) (string, error) { return "", nil }
	if semanticTrustedSSHConnectPublished(h) {
		t.Fatal("runtime-bound handler must not publish connect mode")
	}
	if !semanticTrustedSSHPublished(h) {
		t.Fatal("runtime-bound handler must publish exec mode")
	}
}

func TestSemanticSSHSchemaModeDetection(t *testing.T) {
	if semanticSSHSchemaIsConnectMode(semanticTrustedSSHInvocationSchema()) {
		t.Fatal("exec schema must not be detected as connect mode")
	}
	if !semanticSSHSchemaIsConnectMode(semanticTrustedSSHConnectSchema()) {
		t.Fatal("connect schema must be detected as connect mode")
	}
	if semanticSSHSchemaIsConnectMode(nil) {
		t.Fatal("nil schema must report exec mode (wash stays conservative)")
	}
}

func TestSemanticTrustedSSHTimeoutBudget(t *testing.T) {
	command, timeout, err := semanticTrustedSSHArgsAllowed(map[string]interface{}{
		"command": "apt-get upgrade -y",
	})
	if err != nil || command != "apt-get upgrade -y" || timeout != semanticTrustedSSHCommandTimeout {
		t.Fatalf("default wait = %s err=%v command=%q", timeout, err, command)
	}
	if semanticTrustedSSHCommandTimeout != semanticTrustedShellMaxTimeout || semanticTrustedSSHCommandTimeout == semanticTrustedShellDefaultTimeout {
		t.Fatalf("remote command lifetime %s must be the safety cap, not the %s stuck-session probe", semanticTrustedSSHCommandTimeout, semanticTrustedShellDefaultTimeout)
	}
	_, timeout, err = semanticTrustedSSHArgsAllowed(map[string]interface{}{
		"command":         "sleep 45",
		"timeout_seconds": float64(90),
	})
	if err != nil || timeout != 90*time.Second {
		t.Fatalf("explicit wait = %s err=%v", timeout, err)
	}
	_, timeout, err = semanticTrustedSSHArgsAllowed(map[string]interface{}{
		"command":         "apt-get upgrade -y",
		"timeout_seconds": float64(9999),
	})
	if err != nil || timeout != semanticTrustedShellMaxTimeout {
		t.Fatalf("over-max wait = %s err=%v, want %s", timeout, err, semanticTrustedShellMaxTimeout)
	}
	if _, _, err = semanticTrustedSSHArgsAllowed(map[string]interface{}{
		"command":         "true",
		"timeout_seconds": float64(0),
	}); err == nil {
		t.Fatal("zero timeout must be rejected")
	}
	if _, _, err = semanticTrustedSSHArgsAllowed(map[string]interface{}{
		"command":      "true",
		"wait_seconds": float64(120),
	}); err == nil {
		t.Fatal("wait_seconds must stay rejected")
	}

	authorization, err := tool.NewParameterAuthorization(semanticTrustedSSHInvocationSchema())
	if err != nil {
		t.Fatalf("authorize schema: %v", err)
	}
	selection := tool.PlannedSelection{
		ID:                     "sel-ssh",
		AdapterName:            semanticTrustedSSHAdapter,
		FitProof:               tool.FitProof{MatchedCapability: tool.CapabilityShellExecuteRemoteHost},
		ParameterAuthorization: authorization,
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: &semanticCallSurface{
		plan:             tool.ToolPlan{ID: "plan-ssh", Selections: []tool.PlannedSelection{selection}},
		parameterSchemas: map[string]map[string]interface{}{semanticTrustedSSHAdapter: semanticTrustedSSHInvocationSchema()},
	}}
	canonical, err := cb.semanticCanonicalArguments(selection, `{"command":"sleep 45","timeout":"90"}`)
	if err != nil {
		t.Fatalf("timeout alias must be admitted: %v", err)
	}
	admitted := map[string]interface{}{}
	if err := json.Unmarshal(canonical.CanonicalJSON, &admitted); err != nil {
		t.Fatalf("canonical json: %v", err)
	}
	if admitted["timeout_seconds"] != float64(90) {
		t.Fatalf("admitted timeout = %#v", admitted)
	}
	if _, err := cb.semanticCanonicalArguments(selection, `{"command":"sleep 45","wait_seconds":120}`); err == nil {
		t.Fatal("wait_seconds must still fail canonicalization")
	}
	if !strings.Contains(remoteSSHTimeoutHint(30*time.Second), "timeout_seconds") {
		t.Fatal("a short wait must tell the model it can raise timeout_seconds")
	}
	if hint := remoteSSHTimeoutHint(0); !strings.Contains(hint, "600") || strings.Contains(hint, "timeout_seconds") {
		t.Fatalf("safety-cap hint = %q", hint)
	}
}

func TestSemanticTrustedSSHConnectArgsValidation(t *testing.T) {
	if _, err := semanticTrustedSSHConnectArgsAllowed(map[string]interface{}{"user": "root"}); err == nil {
		t.Fatal("missing host must be rejected")
	}
	if _, err := semanticTrustedSSHConnectArgsAllowed(map[string]interface{}{"host": "h", "user": "  "}); err == nil {
		t.Fatal("blank user must be rejected")
	}
	args, err := semanticTrustedSSHConnectArgsAllowed(map[string]interface{}{"host": "h", "user": "root", "password": "x", "port": float64(22)})
	if err != nil {
		t.Fatalf("valid connect args rejected: %v", err)
	}
	if args["host"] != "h" {
		t.Fatalf("connect args must pass through: %#v", args)
	}
}

// The admission-time wash is mode-aware: exec mode strips legacy decoration,
// connect mode must receive host/user/password untouched.
func TestSemanticSSHAdmissionWashRespectsSchemaMode(t *testing.T) {
	selectionFor := func(t *testing.T, schema map[string]interface{}) tool.PlannedSelection {
		t.Helper()
		authorization, err := tool.NewParameterAuthorization(schema)
		if err != nil {
			t.Fatalf("authorize schema: %v", err)
		}
		return tool.PlannedSelection{
			ID:                     "sel-ssh",
			AdapterName:            semanticTrustedSSHAdapter,
			FitProof:               tool.FitProof{MatchedCapability: tool.CapabilityShellExecuteRemoteHost},
			ParameterAuthorization: authorization,
		}
	}
	surfaceFor := func(t *testing.T, schema map[string]interface{}) (*sharedAgentLoopCallbacks, tool.PlannedSelection) {
		t.Helper()
		selection := selectionFor(t, schema)
		return &sharedAgentLoopCallbacks{semanticSurface: &semanticCallSurface{
			plan:             tool.ToolPlan{ID: "plan-ssh", Selections: []tool.PlannedSelection{selection}},
			parameterSchemas: map[string]map[string]interface{}{semanticTrustedSSHAdapter: schema},
		}}, selection
	}

	execCB, execSelection := surfaceFor(t, semanticTrustedSSHInvocationSchema())
	canonical, err := execCB.semanticCanonicalArguments(execSelection, `{"action":"exec","command":"df -h","session_id":"ssh_root@h:22_1"}`)
	if err != nil {
		t.Fatalf("exec-shaped legacy args must wash clean: %v", err)
	}
	if got := string(canonical.CanonicalJSON); strings.Contains(got, "session_id") {
		t.Fatalf("exec mode must strip session_id: %s", got)
	}

	connectCB, connectSelection := surfaceFor(t, semanticTrustedSSHConnectSchema())
	canonical, err = connectCB.semanticCanonicalArguments(connectSelection, `{"host":"h","user":"root","password":"s3cret"}`)
	if err != nil {
		t.Fatalf("connect args must be admitted in connect mode: %v", err)
	}
	got := string(canonical.CanonicalJSON)
	if !strings.Contains(got, "s3cret") || !strings.Contains(got, "host") {
		t.Fatalf("connect mode must keep connection fields: %s", got)
	}
}

// The 11:04 production turn: the tree channel 502'd, the verdict stayed
// degraded, and the petition re-plan died on the resolver floor — the model
// answered "SSH 工具不在可用工具列表". A degraded classification carrying an
// ssh signal must now plan the connect surface (provider-bounded), so the
// petition rescue and planned surface both work despite the classifier.
func TestIMSemanticDegradedSSHPlansViaFloorExemption(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	degraded := &intent.ClassificationResult{
		Primary:    intent.LabelUnknown,
		Confidence: 0.30,
		Degraded:   true,
		Reason:     "tree classification unavailable (l2=ssh conf=0.79)",
		Secondary:  []intent.IntentLabel{intent.LabelSSH},
	}
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "连接驱网服务器，查看状态", "desktop", "root", "turn", degraded)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("degraded ssh must plan via the floor exemption handled=%v err=%v", handled, err)
	}
	assertPlanHasSSHConnectSelection(t, prepared.plan)
}

// TestSSHAvailabilityIndependentOfClassifierAndSession is the executable
// form of the design invariant learned across the 2026-09-18 production
// incidents: ssh reachability must NEVER depend on classifier health or on
// session state. The semantic migration made availability depend on UIC
// confidence, degradation flags, provider publication, and grant machinery
// all at once while withdrawing the always-on legacy surface — each
// dependency became a separate outage (quota exhaustion, missing connect
// path, resolver-floor refusal, surface-less petition dead-end) and each was
// repaired with one compensating layer. This test pins the four scenarios
// together so a change to any single layer cannot silently void the whole
// guarantee.
func TestSSHAvailabilityIndependentOfClassifierAndSession(t *testing.T) {
	// Scenario 1: confident LabelSSH, no session → managed connect surface
	// (planner gate + connect-mode provider publication).
	t.Run("confident unbound plans connect surface", func(t *testing.T) {
		h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSSH)}
		registerBuiltinTools(h.registry, h)
		defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
			"user-1", "do it", "lansenger", "root-inv-s1", "turn-inv-s1",
			&intent.ClassificationResult{Primary: intent.LabelSSH, Confidence: .98, ToolNames: []string{"ssh"}})
		if err != nil || !handled || surface == nil {
			t.Fatalf("scenario 1 surface handled=%v err=%v", handled, err)
		}
		assertPlanHasSSHConnectSelection(t, surface.plan)
		assertSurfaceRendersSSHConnectSchema(t, defs)
	})

	// Scenario 2: degraded classification that carries an ssh signal → the
	// resolver-floor exemption plans the connect surface anyway.
	t.Run("degraded ssh signal plans via floor exemption", func(t *testing.T) {
		h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSSH)}
		registerBuiltinTools(h.registry, h)
		defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
			"user-1", "连接驱网服务器", "desktop", "root-inv-s2", "turn-inv-s2",
			&intent.ClassificationResult{
				Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true,
				Reason:    "tree classification unavailable (l2=ssh conf=0.79)",
				Secondary: []intent.IntentLabel{intent.LabelSSH},
			})
		if err != nil || !handled || surface == nil {
			t.Fatalf("scenario 2 surface handled=%v err=%v", handled, err)
		}
		assertPlanHasSSHConnectSelection(t, surface.plan)
		assertSurfaceRendersSSHConnectSchema(t, defs)
	})

	// Scenario 3: degraded classification WITHOUT an ssh label → surface-less
	// fallback turn; the model's direct ssh call is rescued by the legacy
	// overlay petition (grant + survives the request rebuild).
	t.Run("surface-less fallback turn rescued by petition", func(t *testing.T) {
		h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSearch)}
		h.semanticTrustedWebSearch = func(userID, query string) (string, error) { return "found: " + query, nil }
		registerBuiltinTools(h.registry, h)
		_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
			"user-1", "连接驱网服务器", "desktop", "root-inv-s3", "turn-inv-s3",
			&intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true, Reason: "tree classification unavailable"})
		if err != nil || handled || surface != nil {
			t.Fatalf("scenario 3 fixture must be surface-less handled=%v err=%v", handled, err)
		}
		cb := &sharedAgentLoopCallbacks{handler: h, platform: "desktop", userText: "连接驱网服务器"}
		granted, message := cb.PetitionToolCall("ssh")
		if !granted || !strings.Contains(message, "action") {
			t.Fatalf("scenario 3 petition granted=%v message=%q", granted, message)
		}
		rebuilt := cb.BuildToolsForModelRequest("连接驱网服务器", 1)
		found := false
		for _, def := range rebuilt {
			if extractToolName(def) == "ssh" {
				found = true
			}
		}
		if !found {
			t.Fatalf("scenario 3 overlay must survive rebuild: %v", agentLoopToolNamesForLog(rebuilt))
		}
	})

	// Scenario 4: routing-miss fallback that keeps the LabelSSH
	// classification (non-degraded) → leftover exposes the builtin legacy ssh
	// tool. This is the pre-migration surface and remains the backstop when
	// neither managed mode can publish (e.g. several ambiguous live sessions).
	t.Run("leftover keeps builtin ssh for non-degraded labelssh miss", func(t *testing.T) {
		h := leftoverSSHCatalogHandler(t)
		ctx := &LoopContext{
			Runtime: RuntimeContext{
				SemanticIntent: &intent.ClassificationResult{
					Primary: intent.LabelSSH, Confidence: 0.95, ToolNames: []string{"ssh"},
				},
				RoutingMissFallback: true,
			},
		}
		set := h.prepareAgentLoopTools("desktop-user", "restart the remote service", ctx, agentLoopPhase{})
		if !toolListContainsName(set.Tools, "ssh") {
			t.Fatalf("scenario 4 leftover must expose builtin ssh, got %v", agentLoopToolNamesForLog(set.Tools))
		}
	})
}

// The definitive regression for the 11:04 production failure: a degraded turn
// whose classification does NOT carry an ssh label (tree 502'd before the
// verdict) starts with a lookup-only surface; the model's direct ssh call
// must be rescued by the petition expansion — which is only possible because
// the re-plan passes the degraded floor exemption and the session-less
// catalog publishes the connect provider.
func TestSemanticToolCallPetitionRescuesDegradedTurnSSH(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSearch)}
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) { return "found: " + query, nil }
	registerBuiltinTools(h.registry, h)
	degraded := &intent.ClassificationResult{
		Primary:    intent.LabelUnknown,
		Confidence: 0.30,
		Degraded:   true,
		Reason:     "tree classification unavailable",
	}
	// A degraded turn without an ssh label is a routing miss: no managed
	// surface at all — exactly the production fallback state at 11:04.
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "连接驱网服务器，查看状态", "desktop", "root-ssh-petition", "turn-ssh-petition", degraded)
	if err != nil || handled || surface != nil {
		t.Fatalf("fixture must be a surface-less fallback turn handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, platform: "desktop", userText: "连接驱网服务器，查看状态"}
	granted, message := cb.PetitionToolCall("ssh")
	if !granted || !strings.Contains(message, "ssh") || !strings.Contains(message, "授权") {
		t.Fatalf("surface-less ssh petition must be granted, granted=%v message=%q", granted, message)
	}
	if !strings.Contains(message, "[ssh-rescue:legacy_overlay]") {
		t.Fatalf("rescue message must carry the §B4 layer token: %q", message)
	}
	// The overlay must appear in the callback tool list for the re-issued call.
	found := false
	for _, def := range cb.tools {
		if extractToolName(def) == "ssh" {
			found = true
		}
	}
	if !found {
		t.Fatalf("legacy ssh overlay missing from tools %v", agentLoopToolNamesForLog(cb.tools))
	}
	// The effectful petition budget is spent by the rescue.
	if !cb.semanticEffectfulPetitionConsumed {
		t.Fatal("legacy ssh petition must spend the effectful budget")
	}
	// The successor request rebuilds the tool surface from policy — the
	// overlay must survive that rebuild or the re-issued call is denied again.
	rebuilt := cb.BuildToolsForModelRequest("连接驱网服务器，查看状态", 1)
	found = false
	for _, def := range rebuilt {
		if extractToolName(def) == "ssh" {
			found = true
		}
	}
	if !found {
		t.Fatalf("legacy ssh overlay must survive the request rebuild: %v", agentLoopToolNamesForLog(rebuilt))
	}
	// A second petition in the same turn is denied.
	if again, _ := cb.PetitionToolCall("ssh"); again {
		t.Fatal("second ssh petition must be denied within one turn")
	}
}

// Surface-level integration for the 11:04 scenario: a degraded turn whose
// classification carries an ssh signal must render the ssh tool (connect
// schema) in the initial model-facing surface, not just plan it.
func TestIMSemanticDegradedSSHSurfaceRendersConnectTool(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSSH)}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "连接驱网服务器，查看状态", "desktop", "root-ssh-degraded-surface", "turn-ssh-degraded-surface",
		&intent.ClassificationResult{
			Primary:    intent.LabelUnknown,
			Confidence: 0.30,
			Degraded:   true,
			Reason:     "tree classification unavailable (l2=ssh conf=0.79)",
			Secondary:  []intent.IntentLabel{intent.LabelSSH},
		},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("degraded ssh surface handled=%v err=%v", handled, err)
	}
	assertPlanHasSSHConnectSelection(t, surface.plan)
	assertSurfaceRendersSSHConnectSchema(t, defs)
}

// The 17:42 production incident: the tree timed out and the classifier's
// L2-fallback collapsed ssh (0.83) to bare unknown — the ssh signal survived
// only as RunnerUp escalation evidence. The floor exemption must honor that
// evidence, otherwise every availability layer stays dark.
func TestIMSemanticDegradedSSHRunnerUpSignalPlansViaExemption(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	degraded := &intent.ClassificationResult{
		Primary:       intent.LabelUnknown,
		Confidence:    0.30,
		Degraded:      true,
		Reason:        "embedding ambiguous; tree classification unavailable (l2=ssh conf=0.83)",
		RunnerUp:      intent.LabelSSH,
		RunnerUpScore: 0.83,
	}
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "连接驱网服务器，查看状态", "desktop", "root", "turn", degraded)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("RunnerUp ssh signal must plan via the floor exemption handled=%v err=%v", handled, err)
	}
	assertPlanHasSSHConnectSelection(t, prepared.plan)
}

// The exemption must not become a general degraded-turn loophole: mutating
// secondaries other than ssh are dropped by the projection, and a degraded
// turn WITHOUT an ssh signal still misses to leftover.
func TestIMSemanticDegradedSSHExemptionDropsOtherMutatingFamilies(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	mixed := &intent.ClassificationResult{
		Primary:    intent.LabelUnknown,
		Confidence: 0.30,
		Degraded:   true,
		Secondary:  []intent.IntentLabel{intent.LabelSSH, intent.LabelShellCommand},
	}
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "连接驱网服务器，查看状态", "desktop", "root", "turn", mixed)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("degraded ssh+shell must still plan the ssh leg handled=%v err=%v", handled, err)
	}
	assertPlanHasSSHConnectSelection(t, prepared.plan)
	for _, selection := range prepared.plan.Selections {
		if selection.FitProof.MatchedCapability == tool.CapabilityShellExecuteLocal {
			t.Fatalf("degraded exemption must not mint local shell: %#v", selection.FitProof)
		}
	}

	noSSH := &intent.ClassificationResult{
		Primary:    intent.LabelUnknown,
		Confidence: 0.30,
		Degraded:   true,
		Secondary:  []intent.IntentLabel{intent.LabelShellCommand},
	}
	if _, handled, err := h.semanticPlanForTurnWithClassification("user", "run something", "desktop", "root", "turn", noSSH); handled || err != nil {
		t.Fatalf("degraded non-ssh mutating turn must stay a miss handled=%v err=%v", handled, err)
	}
}

// The connect-mode executor dispatches on the presence of host: a refused
// dial must surface as a rejection (grant preserved for a corrected retry),
// not as a success.
func TestSemanticSSHConnectDispatchFailureStaysRejected(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	callbacks := &sharedAgentLoopCallbacks{handler: h, userID: "user-1"}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, tool.CanonicalRequest{
		CanonicalJSON: []byte(`{"host":"127.0.0.1","user":"root","password":"x","port":1}`),
	})
	if !strings.HasPrefix(got, "[system rejected]") {
		t.Fatalf("refused connect must stay a rejection: %q", got)
	}
	if !strings.Contains(got, "SSH 连接失败") {
		t.Fatalf("rejection must carry the connect failure: %q", got)
	}
}
