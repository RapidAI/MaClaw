package guiapp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOpsAgentUsesTheVerdictAndKeepsThePromptOffTheDisplay(t *testing.T) {
	var logs []string
	started := time.Now()
	result, err := RunOpsAgent(nil, OpsAgentJob{
		Name:    "安装示例",
		Display: "〔运维〕在 host 上安装示例",
		Prompt:  "密码：secret-value\n完成后最后一行输出 `WIDGET: OK`。",
		Marker:  "WIDGET",
		Budget:  time.Minute,
		Every:   time.Hour,
		newTask: func(name string) string {
			if name != "安装示例" {
				t.Fatalf("task name %q", name)
			}
			return "/tmp/ops-task"
		},
		send: func(req AIAssistantSendRequest) (*IMAgentResponse, error) {
			if req.BackendDisplayText != "〔运维〕在 host 上安装示例" {
				t.Fatalf("display %q", req.BackendDisplayText)
			}
			if !strings.Contains(req.Text, "secret-value") {
				t.Fatal("the agent prompt lost the credential")
			}
			if strings.Contains(req.BackendDisplayText, "secret-value") {
				t.Fatal("the visible row contains the credential")
			}
			req.OnDone(&IMAgentResponse{Text: "做完了\nWIDGET: OK\n"})
			return &IMAgentResponse{}, nil
		},
		Log: func(step, detail string) {
			logs = append(logs, step+" "+detail)
		},
		OnTask: func(path string) {
			if path != "/tmp/ops-task" {
				t.Fatalf("path %q", path)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict != "OK" || result.ProjectPath != "/tmp/ops-task" {
		t.Fatalf("result %+v", result)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("waited %s", time.Since(started))
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "agent-done OK") {
		t.Fatalf("logs %s", joined)
	}
	if strings.Contains(joined, "secret-value") {
		t.Fatalf("log kept the credential: %s", joined)
	}
}

func TestOpsAgentStopsWhenTheCheckCannotRecover(t *testing.T) {
	started := time.Now()
	_, err := RunOpsAgent(nil, OpsAgentJob{
		Name:    "检查",
		Display: "检查一台机器",
		Marker:  "WIDGET",
		Budget:  time.Minute,
		Every:   time.Hour,
		newTask: func(string) string { return "/tmp/ops-task" },
		send: func(req AIAssistantSendRequest) (*IMAgentResponse, error) {
			req.OnDone(&IMAgentResponse{Text: "WIDGET: OK"})
			return &IMAgentResponse{}, nil
		},
		Fatal: func(err error) bool { return err != nil && strings.Contains(err.Error(), "需要先更新") },
		Probe: func(agentDone bool, _ string) error {
			if !agentDone {
				return errors.New("正在安装")
			}
			return errors.New("Hub 还没有这个接口，需要先更新 Hub")
		},
	})
	var opsErr *OpsAgentError
	if !errors.As(err, &opsErr) || opsErr.Phase != OpsAgentPhaseProbe {
		t.Fatalf("err %v", err)
	}
	if !strings.Contains(opsErr.Error(), "需要先更新 Hub") {
		t.Fatal(opsErr)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("a fatal check waited %s", time.Since(started))
	}
}

func TestOpsAgentGivesUpSoonAfterAFailedVerdict(t *testing.T) {
	started := time.Now()
	_, err := RunOpsAgent(nil, OpsAgentJob{
		Name:        "检查",
		Display:     "检查一台机器",
		Marker:      "WIDGET",
		Budget:      time.Minute,
		Every:       15 * time.Millisecond,
		Grace:       time.Minute,
		FailedGrace: 80 * time.Millisecond,
		newTask:     func(string) string { return "/tmp/ops-task" },
		send: func(req AIAssistantSendRequest) (*IMAgentResponse, error) {
			req.OnDone(&IMAgentResponse{Text: "WIDGET: FAILED 编译失败"})
			return &IMAgentResponse{}, nil
		},
		Fatal: func(error) bool { return false },
		Probe: func(bool, string) error { return errors.New("还在等") },
	})
	var opsErr *OpsAgentError
	if !errors.As(err, &opsErr) || opsErr.Verdict != "FAILED 编译失败" {
		t.Fatalf("err %v", err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("failed verdict waited %s", time.Since(started))
	}
}

func TestOpsAgentReportsAMissingTaskAndAStartFailure(t *testing.T) {
	_, err := RunOpsAgent(nil, OpsAgentJob{Name: "空", Display: "检查一台机器", newTask: func(string) string { return "" }})
	var opsErr *OpsAgentError
	if !errors.As(err, &opsErr) || opsErr.Phase != OpsAgentPhaseTask {
		t.Fatalf("task err %v", err)
	}
	_, err = RunOpsAgent(nil, OpsAgentJob{
		Name:    "启动",
		Display: "检查一台机器",
		newTask: func(string) string { return "/tmp/ops-task" },
		send:    func(AIAssistantSendRequest) (*IMAgentResponse, error) { return nil, errors.New("agent down") },
	})
	if !errors.As(err, &opsErr) || opsErr.Phase != OpsAgentPhaseStart || !strings.Contains(err.Error(), "agent down") {
		t.Fatalf("start err %v", err)
	}
}

func TestOpsAgentRejectsAVisibleSecretAndCanBeCancelled(t *testing.T) {
	_, err := RunOpsAgent(nil, OpsAgentJob{
		Display: "",
		Prompt:  "密码：secret-value",
		newTask: func(string) string {
			t.Fatal("empty display still created a task")
			return ""
		},
	})
	var opsErr *OpsAgentError
	if !errors.As(err, &opsErr) || opsErr.Phase != OpsAgentPhaseStart || !strings.Contains(err.Error(), "不能为空") {
		t.Fatalf("empty display %v", err)
	}
	_, err = RunOpsAgent(nil, OpsAgentJob{
		Display: "密码：secret-value",
		newTask: func(string) string {
			t.Fatal("secret display still created a task")
			return ""
		},
	})
	if !errors.As(err, &opsErr) || !strings.Contains(err.Error(), "不能带密码") {
		t.Fatalf("secret display %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	_, err = RunOpsAgent(nil, OpsAgentJob{
		Display: "检查一台机器",
		Context: ctx,
		Budget:  time.Minute,
		Every:   time.Hour,
		newTask: func(string) string { return "/tmp/ops-task" },
		send: func(AIAssistantSendRequest) (*IMAgentResponse, error) {
			return &IMAgentResponse{}, nil
		},
	})
	if !errors.As(err, &opsErr) || !strings.Contains(err.Error(), "操作已取消") {
		t.Fatalf("cancel %v", err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("cancel waited %s", time.Since(started))
	}
}

func TestOpsAgentLogHidesAProbeSecret(t *testing.T) {
	var logs []string
	_, err := RunOpsAgent(nil, OpsAgentJob{
		Display: "检查一台机器",
		Budget:  200 * time.Millisecond,
		Every:   20 * time.Millisecond,
		Grace:   time.Minute,
		newTask: func(string) string { return "/tmp/ops-task" },
		send: func(AIAssistantSendRequest) (*IMAgentResponse, error) {
			return &IMAgentResponse{}, nil
		},
		Log:   func(step, detail string) { logs = append(logs, step+" "+detail) },
		Fatal: func(error) bool { return false },
		Probe: func(bool, string) error { return errors.New("password=secret-value") },
	})
	if err == nil {
		t.Fatal("expected the budget to end the probe")
	}
	joined := strings.Join(logs, "\n")
	if strings.Contains(joined, "secret-value") || !strings.Contains(joined, "password=<redacted>") {
		t.Fatalf("logs %s", joined)
	}
}

func TestOpsAgentVerdictIgnoresAMentionAndReadsTheMarkerLine(t *testing.T) {
	mentioned := &IMAgentResponse{Text: "说明里写着 DESKTOPD_DEPLOY: OK，这不是结论", Error: "boom"}
	if got := opsAgentVerdict(mentioned, "DESKTOPD_DEPLOY"); got != "boom" {
		t.Fatalf("mention = %q", got)
	}
	line := &IMAgentResponse{Text: "做完了\n`DESKTOPD_DEPLOY: FAILED 编译失败`"}
	if got := opsAgentVerdict(line, "DESKTOPD_DEPLOY"); got != "FAILED 编译失败" {
		t.Fatalf("marker line = %q", got)
	}
	bullet := &IMAgentResponse{Text: "- DESKTOPD_DEPLOY: OK"}
	if got := opsAgentVerdict(bullet, "DESKTOPD_DEPLOY"); got != "OK" {
		t.Fatalf("bullet = %q", got)
	}
}

func TestOpsAgentRejectsASecretTaskName(t *testing.T) {
	_, err := RunOpsAgent(nil, OpsAgentJob{
		Name:    "密码：secret-value",
		Display: "检查一台机器",
		newTask: func(string) string {
			t.Fatal("secret task name still created a task")
			return ""
		},
	})
	var opsErr *OpsAgentError
	if !errors.As(err, &opsErr) || !strings.Contains(err.Error(), "任务名不能带") {
		t.Fatalf("name %v", err)
	}
}

func TestOpsAgentCancelDuringTheCheckWins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := time.Now()
	_, err := RunOpsAgent(nil, OpsAgentJob{
		Display: "检查一台机器",
		Marker:  "WIDGET",
		Context: ctx,
		Budget:  time.Minute,
		Every:   time.Hour,
		newTask: func(string) string { return "/tmp/ops-task" },
		send: func(req AIAssistantSendRequest) (*IMAgentResponse, error) {
			req.OnDone(&IMAgentResponse{Text: "WIDGET: OK"})
			return &IMAgentResponse{}, nil
		},
		Probe: func(bool, string) error {
			cancel()
			return errors.New("还在等")
		},
	})
	var opsErr *OpsAgentError
	if !errors.As(err, &opsErr) || !strings.Contains(err.Error(), "操作已取消") {
		t.Fatalf("cancel %v", err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("cancel waited %s", time.Since(started))
	}
}

func TestOpsScrubHidesAPasswordAndKeepsAShortNodeID(t *testing.T) {
	text := OpsScrub("password=secret 密码：zh-pass node=143017c567847d66 blob=0123456789abcdef0123456789abcdef0123456789abcdef")
	if strings.Contains(text, "secret") || strings.Contains(text, "zh-pass") || strings.Contains(text, "0123456789abcdef0123456789abcdef0123456789abcdef") {
		t.Fatalf("scrubbed %s", text)
	}
	if !strings.Contains(text, "143017c567847d66") || !strings.Contains(text, "password=<redacted>") {
		t.Fatalf("scrubbed %s", text)
	}
}
