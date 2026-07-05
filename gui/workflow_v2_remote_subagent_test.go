package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestExtractExperimentParamsPrefersPaperMetricOverReproductionMetric(t *testing.T) {
	state := &v2.WorkflowState{
		Phases: []v2.Phase{
			{ID: "paper_analysis", Output: "Paper reports final Accuracy: 92.5% on the benchmark."},
			{ID: "baseline_reproduction", Output: "Our reproduced Accuracy: 88.1% after three seeds."},
		},
	}

	params := extractExperimentParams(state)
	if params.BaselineMetricName != "Accuracy" && params.BaselineMetricName != "accuracy" {
		t.Fatalf("metric name = %q", params.BaselineMetricName)
	}
	if params.BaselineMetricValue != 92.5 {
		t.Fatalf("paper metric should remain target comparator, got %.4f", params.BaselineMetricValue)
	}
	if !params.MetricHigherBetter {
		t.Fatalf("accuracy should be higher-is-better")
	}
}

func TestExtractExperimentParamsFallsBackToReproductionMetricWhenPaperMissing(t *testing.T) {
	state := &v2.WorkflowState{
		Phases: []v2.Phase{
			{ID: "paper_analysis", Output: "The paper discusses the model but omits the final score."},
			{ID: "baseline_reproduction", Output: "Reproduction val_loss: 0.42"},
		},
	}

	params := extractExperimentParams(state)
	if params.BaselineMetricName != "val_loss" {
		t.Fatalf("metric name = %q", params.BaselineMetricName)
	}
	if params.BaselineMetricValue != 0.42 {
		t.Fatalf("fallback reproduction metric = %.4f", params.BaselineMetricValue)
	}
	if params.MetricHigherBetter {
		t.Fatalf("loss should be lower-is-better")
	}
}

func TestParseBaselineMetricPrefersLatestCandidateByTextPosition(t *testing.T) {
	output := "Early reproduction Accuracy: 88.1%. Later evaluation says val_loss: 0.42. Final report: Accuracy: 92.5%."
	name, value, higherBetter := parseBaselineMetric(output)
	if name != "Accuracy" {
		t.Fatalf("metric name = %q", name)
	}
	if value != 92.5 {
		t.Fatalf("metric value = %.4f", value)
	}
	if !higherBetter {
		t.Fatalf("accuracy should be higher-is-better")
	}
}

func TestParseBaselineMetricTakesLastMetricWithinSecondHalf(t *testing.T) {
	output := strings.Repeat("setup notes. ", 30) + "Validation Accuracy: 84.0%. Final Accuracy: 87.3%."
	name, value, _ := parseBaselineMetric(output)
	if name != "Accuracy" {
		t.Fatalf("metric name = %q", name)
	}
	if value != 87.3 {
		t.Fatalf("metric value = %.4f", value)
	}
}

func TestParseSSHHostStringHandlesCommonAndIPv6Forms(t *testing.T) {
	tests := []struct {
		input    string
		wantUser string
		wantHost string
		wantPort int
	}{
		{input: "example.com", wantUser: "root", wantHost: "example.com", wantPort: 22},
		{input: "alice@example.com:2200", wantUser: "alice", wantHost: "example.com", wantPort: 2200},
		{input: "ssh://bob@example.com:2222/home/project", wantUser: "bob", wantHost: "example.com", wantPort: 2222},
		{input: "2001:db8::10", wantUser: "root", wantHost: "2001:db8::10", wantPort: 22},
		{input: "carol@[2001:db8::10]:2022", wantUser: "carol", wantHost: "2001:db8::10", wantPort: 2022},
		{input: "[2001:db8::11]:2023", wantUser: "root", wantHost: "2001:db8::11", wantPort: 2023},
		{input: "dave@example.com:notaport", wantUser: "dave", wantHost: "example.com:notaport", wantPort: 22},
	}

	for _, tt := range tests {
		user, host, port := parseSSHHostString(tt.input)
		if user != tt.wantUser || host != tt.wantHost || port != tt.wantPort {
			t.Fatalf("parseSSHHostString(%q) = (%q,%q,%d), want (%q,%q,%d)", tt.input, user, host, port, tt.wantUser, tt.wantHost, tt.wantPort)
		}
	}
}

func TestExtractSSHSessionIDFromConnectResultPrefersExplicitFields(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "chinese connect output",
			input: "✅ SSH 连接成功\n会话 ID: ssh_root@example.com:22_3\n主机: root@example.com:22",
			want:  "ssh_root@example.com:22_3",
		},
		{
			name:  "json field",
			input: `{"session_id":"ssh_alice@host.internal:2200_12","status":"running"}`,
			want:  "ssh_alice@host.internal:2200_12",
		},
		{
			name:  "bracketed ipv6",
			input: "session-id = ssh_carol@[2001:db8::10]:2022_4",
			want:  "ssh_carol@[2001:db8::10]:2022_4",
		},
		{
			name:  "fallback regex",
			input: "reused ssh_bob@10.0.0.5:22_9 from previous connection",
			want:  "ssh_bob@10.0.0.5:22_9",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		if got := extractSSHSessionIDFromConnectResult(tt.input); got != tt.want {
			t.Fatalf("%s: got %q want %q", tt.name, got, tt.want)
		}
	}
}

func TestInferRemoteProjectDirFromSSHSessionHandlesCommonLaunchCommands(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "cd /repo/project && bash", want: "/repo/project"},
		{input: "cd '/repo/project with space'; exec bash", want: "/repo/project with space"},
		{input: "cd \"/srv/app\" && clear", want: "/srv/app"},
		{input: "cd -- /opt/service && exec bash", want: "/opt/service"},
		{input: "cd\t/var/www && bash", want: "/var/www"},
		{input: "cd -P /srv/physical && bash", want: "/srv/physical"},
		{input: "cd -L '/srv/link target' && bash", want: "/srv/link target"},
		{input: "cdfoo /wrong && bash", want: ""},
		{input: "echo hello", want: ""},
	}

	for _, tt := range tests {
		if got := inferRemoteProjectDirFromSSHSession(tt.input); got != tt.want {
			t.Fatalf("inferRemoteProjectDirFromSSHSession(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDirectRemoteCodingSessionUsableOnlyAllowsInteractiveStates(t *testing.T) {
	allowed := []remote.SessionStatus{remote.SessionRunning, remote.SessionWaitingInput}
	for _, status := range allowed {
		if !directRemoteCodingSessionUsable(status) {
			t.Fatalf("status %s should be usable", status)
		}
	}

	blocked := []remote.SessionStatus{remote.SessionStarting, remote.SessionBusy, remote.SessionExited, remote.SessionError}
	for _, status := range blocked {
		if directRemoteCodingSessionUsable(status) {
			t.Fatalf("status %s should not be usable", status)
		}
	}
}

func TestDirectRemoteCodingSessionSelectableRequiresIDAndInteractiveState(t *testing.T) {
	tests := []struct {
		name    string
		session *remote.SSHManagedSession
		want    bool
	}{
		{name: "nil session", session: nil, want: false},
		{name: "empty id", session: &remote.SSHManagedSession{Status: remote.SessionRunning}, want: false},
		{name: "blank id", session: &remote.SSHManagedSession{ID: "   ", Status: remote.SessionRunning}, want: false},
		{name: "busy", session: &remote.SSHManagedSession{ID: "ssh-1", Status: remote.SessionBusy}, want: false},
		{name: "running", session: &remote.SSHManagedSession{ID: "ssh-1", Status: remote.SessionRunning}, want: true},
		{name: "waiting input", session: &remote.SSHManagedSession{ID: "ssh-1", Status: remote.SessionWaitingInput}, want: true},
	}

	for _, tt := range tests {
		if got := directRemoteCodingSessionSelectable(tt.session); got != tt.want {
			t.Fatalf("%s: got %v want %v", tt.name, got, tt.want)
		}
	}
}

func TestDirectCodingPanelPlaceholderOnlyMatchesPanelLaunchText(t *testing.T) {
	if !isDirectCodingPanelPlaceholder("启动简化编程任务") {
		t.Fatalf("quick coding panel launch text should be treated as placeholder")
	}
	if !isDirectCodingPanelPlaceholder(" 启动远程编程任务 ") {
		t.Fatalf("remote coding panel launch text should be treated as placeholder")
	}
	if isDirectCodingPanelPlaceholder("请修改登录页样式") {
		t.Fatalf("real user task should not be treated as placeholder")
	}
}

func TestScrubActivePhaseSensitiveFormDataRemovesSecretsOnly(t *testing.T) {
	state := &v2.WorkflowState{
		Phases: []v2.Phase{
			{
				ID: "remote_direct_coding",
				InputSchema: &v2.PhaseInputSchema{
					Fields: []v2.PhaseInputField{
						{Name: "ssh_host", Label: "主机", Type: "text"},
						{Name: "ssh_password", Label: "密码", Type: "text", Sensitive: true},
					},
				},
				FormData: map[string]interface{}{
					"ssh_host":        "10.0.0.8",
					"ssh_password":    "secret",
					"api_token":       "token",
					"project_request": "fix login",
				},
			},
		},
		CurrentPhase: 0,
	}

	if !scrubActivePhaseSensitiveFormData(state) {
		t.Fatalf("expected sensitive form data to be scrubbed")
	}
	formData := state.Phases[0].FormData
	for _, key := range []string{"ssh_password", "api_token"} {
		if _, ok := formData[key]; ok {
			t.Fatalf("%s should be removed from form data", key)
		}
	}
	if got := formData["ssh_host"]; got != "10.0.0.8" {
		t.Fatalf("ssh_host = %v, want preserved", got)
	}
	if got := formData["project_request"]; got != "fix login" {
		t.Fatalf("project_request = %v, want preserved", got)
	}
	if scrubActivePhaseSensitiveFormData(state) {
		t.Fatalf("second scrub should report no changes")
	}
}

func TestHasPendingDirectSubAgentExecutionRequiresDirectContext(t *testing.T) {
	h := &IMMessageHandler{}
	if h.hasPendingDirectSubAgentExecution("u") {
		t.Fatalf("empty handler state should not be pending direct execution")
	}

	h.pendingV2SubAgentExecution.Store("u", true)
	if h.hasPendingDirectSubAgentExecution("u") {
		t.Fatalf("generic pending v2 execution without direct context should not be treated as direct")
	}

	h.pendingDirectCodingProjectPath.Store("u", "D:/repo")
	if !h.hasPendingDirectSubAgentExecution("u") {
		t.Fatalf("local direct coding context should be pending direct execution")
	}
	h.pendingDirectCodingProjectPath.Delete("u")

	h.pendingDirectRemoteCoding.Store("u", directRemoteCodingContext{SessionID: "ssh-1", ProjectDir: "/repo", WorkDir: "/repo"})
	if !h.hasPendingDirectSubAgentExecution("u") {
		t.Fatalf("remote direct coding context should be pending direct execution")
	}
}

func TestClearPerUserSessionStateClearsDirectSubAgentPendingState(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "u"
	h.pendingV2SubAgentExecution.Store(userID, true)
	h.pendingDirectCodingProjectPath.Store(userID, "D:/repo")
	h.pendingDirectRemoteCoding.Store(userID, directRemoteCodingContext{SessionID: "ssh-1", ProjectDir: "/repo", WorkDir: "/repo"})

	h.clearPerUserSessionState(userID)

	if _, ok := h.pendingV2SubAgentExecution.Load(userID); ok {
		t.Fatalf("pendingV2SubAgentExecution should be cleared")
	}
	if _, ok := h.pendingDirectCodingProjectPath.Load(userID); ok {
		t.Fatalf("pendingDirectCodingProjectPath should be cleared")
	}
	if _, ok := h.pendingDirectRemoteCoding.Load(userID); ok {
		t.Fatalf("pendingDirectRemoteCoding should be cleared")
	}
}
