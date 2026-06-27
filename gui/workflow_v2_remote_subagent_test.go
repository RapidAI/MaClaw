package main

import (
	"strings"
	"testing"

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
