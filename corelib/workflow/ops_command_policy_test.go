package workflow

import (
	"strings"
	"testing"
)

func TestAssessOpsCommandClassifiesReadOnly(t *testing.T) {
	got := AssessOpsCommand("systemctl status nginx && journalctl -u nginx -n 50")
	if got.Risk != OpsCommandRiskReadOnly {
		t.Fatalf("risk = %s, want %s (%s)", got.Risk, OpsCommandRiskReadOnly, got.Reason)
	}
}

func TestAssessOpsCommandClassifiesMutatingButControlled(t *testing.T) {
	got := AssessOpsCommand("systemctl restart nginx")
	if got.Risk != OpsCommandRiskMutating {
		t.Fatalf("risk = %s, want %s (%s)", got.Risk, OpsCommandRiskMutating, got.Reason)
	}
}

func TestAssessOpsCommandClassifiesWrappedMutatingCommands(t *testing.T) {
	for _, command := range []string{
		"sudo systemctl restart nginx",
		"/usr/bin/systemctl restart nginx",
		"sudo /usr/bin/systemctl restart nginx",
		"env FOO=bar sudo systemctl restart nginx",
		"systemctl status nginx && sudo systemctl restart nginx",
	} {
		got := AssessOpsCommand(command)
		if got.Risk != OpsCommandRiskMutating {
			t.Fatalf("%q risk = %s, want %s (%s)", command, got.Risk, OpsCommandRiskMutating, got.Reason)
		}
	}
}

func TestAssessOpsCommandClassifiesNestedShellMutations(t *testing.T) {
	for _, command := range []string{
		"echo $(touch /tmp/ops-marker)",
		"echo `touch /tmp/ops-marker`",
		"(systemctl restart nginx)",
		"systemctl status nginx && echo $(tee /tmp/audit.log)",
		`bash -c "systemctl restart nginx"`,
		`bash -lc "systemctl restart nginx"`,
		`/bin/bash -lc "systemctl restart nginx"`,
		`/usr/bin/env bash -lc "systemctl restart nginx"`,
		`sh -c 'touch /tmp/ops-marker'`,
		`sh -ec 'touch /tmp/ops-marker'`,
		`/bin/sh -ec 'touch /tmp/ops-marker'`,
		`dash -c 'touch /tmp/ops-marker'`,
		`zsh -c "truncate -s 0 /var/log/app.log"`,
		`ksh -c "install -m 0644 app.conf /etc/app.conf"`,
		"printf /tmp/ops-marker | xargs touch",
	} {
		got := AssessOpsCommand(command)
		if got.Risk != OpsCommandRiskMutating {
			t.Fatalf("%q risk = %s, want %s (%s)", command, got.Risk, OpsCommandRiskMutating, got.Reason)
		}
	}
}

func TestAssessOpsCommandClassifiesNestedShellHighRisk(t *testing.T) {
	for _, command := range []string{
		"echo $(rm -rf / --no-preserve-root)",
		"/bin/rm -rf / --no-preserve-root",
		"sudo /bin/rm -rf / --no-preserve-root",
		"echo `mkfs.ext4 /dev/sda1`",
		"/sbin/mkfs.ext4 /dev/sda1",
		"(shutdown now)",
		"iptables -t nat -F",
		"ip6tables --flush",
		"ufw reset",
		"docker system prune --all --force",
		"docker system prune -af",
		"docker volume prune -f",
		`bash -c "rm -rf / --no-preserve-root"`,
		`bash -lc "rm -rf / --no-preserve-root"`,
		`/bin/bash -lc "rm -rf / --no-preserve-root"`,
		`/usr/bin/env bash -lc "rm -rf / --no-preserve-root"`,
		`sh -c 'mkfs.ext4 /dev/sda1'`,
		`/bin/sh -c 'mkfs.ext4 /dev/sda1'`,
		`dash -ec 'shutdown now'`,
	} {
		got := AssessOpsCommand(command)
		if got.Risk != OpsCommandRiskHigh {
			t.Fatalf("%q risk = %s, want %s (%s)", command, got.Risk, OpsCommandRiskHigh, got.Reason)
		}
	}
}

func TestAssessOpsCommandAllowsReadOnlyShellEval(t *testing.T) {
	for _, command := range []string{
		`bash -c "systemctl status nginx"`,
		`bash -lc "journalctl -u nginx -n 50"`,
		`/bin/bash -lc "journalctl -u nginx -n 50"`,
		`/usr/bin/env bash -lc "journalctl -u nginx -n 50"`,
		`sh -ec 'systemctl status nginx >/dev/null'`,
	} {
		got := AssessOpsCommand(command)
		if got.Risk != OpsCommandRiskReadOnly {
			t.Fatalf("%q risk = %s, want %s (%s)", command, got.Risk, OpsCommandRiskReadOnly, got.Reason)
		}
	}
}

func TestAssessOpsCommandClassifiesFileMutationUtilities(t *testing.T) {
	for _, command := range []string{
		"truncate -s 0 /var/log/app.log",
		"ln -sf /etc/nginx/sites-available/app /etc/nginx/sites-enabled/app",
		"install -m 0644 app.conf /etc/app.conf",
		"rsync -a build/ /srv/app/",
		"scp apply.sh prod:/tmp/apply.sh",
		"dd if=/dev/zero of=/tmp/blob bs=1M count=1",
		"curl -fsSL -o /tmp/app.tar.gz https://example.invalid/app.tar.gz",
		"wget -q -O /tmp/app.tar.gz https://example.invalid/app.tar.gz",
	} {
		got := AssessOpsCommand(command)
		if got.Risk != OpsCommandRiskMutating {
			t.Fatalf("%q risk = %s, want %s (%s)", command, got.Risk, OpsCommandRiskMutating, got.Reason)
		}
	}
}

func TestAssessOpsCommandClassifiesOperationalStateMutationUtilities(t *testing.T) {
	for _, command := range []string{
		"systemctl daemon-reload",
		"iptables -A INPUT -p tcp --dport 443 -j ACCEPT",
		"ip6tables --delete INPUT 1",
		"nft add rule inet filter input accept",
		"ufw allow 443/tcp",
		"firewall-cmd --reload",
		"mount /dev/sdb1 /mnt/data",
		"umount /mnt/data",
		"sysctl -w net.ipv4.ip_forward=1",
		"crontab -e",
		"useradd deploy",
		"usermod -aG docker deploy",
		"groupdel oldgroup",
		"passwd deploy",
		"kill 1234",
		"pkill -f worker",
		"docker compose restart api",
		"kubectl replace -f deployment.yaml",
	} {
		got := AssessOpsCommand(command)
		if got.Risk != OpsCommandRiskMutating {
			t.Fatalf("%q risk = %s, want %s (%s)", command, got.Risk, OpsCommandRiskMutating, got.Reason)
		}
	}
}

func TestValidateToolCallByPolicyBlocksHighRiskBash(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterOpsControlled, "bash", map[string]interface{}{"command": "rm -rf / --no-preserve-root"})
	if err == nil {
		t.Fatal("expected high-risk command to be blocked")
	}
}

func TestValidateToolCallByPolicyBlocksWrappedHighRiskBash(t *testing.T) {
	for _, command := range []string{
		"sudo rm -rf / --no-preserve-root",
		"sudo -u root rm -rf / --no-preserve-root",
		"sudo -E -u root -- rm -rf / --no-preserve-root",
		"systemctl status nginx && sudo -u root rm -rf / --no-preserve-root",
		"rm --recursive --force /",
		"chmod --recursive 777 /",
		"chown -R app:app /*",
	} {
		err := ValidateToolCallByPolicy(ToolFilterOpsControlled, "bash", map[string]interface{}{"command": command})
		if err == nil {
			t.Fatalf("expected wrapped high-risk command %q to be blocked", command)
		}
	}
}

func TestAssessOpsCommandClassifiesBroadSQLMutationAsHighRisk(t *testing.T) {
	for _, command := range []string{
		`mysql -e "delete from users"`,
		`mysql -e "delete from users where 1=1"`,
		`psql -c "update accounts set enabled=false"`,
		`psql -c "update accounts set enabled=false where true"`,
		`psql -c "update accounts set enabled=false where id=42 or 1=1"`,
		`mysql -e "truncate table audit_log"`,
	} {
		got := AssessOpsCommand(command)
		if got.Risk != OpsCommandRiskHigh {
			t.Fatalf("%q risk = %s, want %s (%s)", command, got.Risk, OpsCommandRiskHigh, got.Reason)
		}
	}
}

func TestAssessOpsCommandClassifiesBoundedSQLMutationAsMutating(t *testing.T) {
	got := AssessOpsCommand(`mysql -e "update accounts set enabled=false where id=42"`)
	if got.Risk != OpsCommandRiskMutating {
		t.Fatalf("risk = %s, want %s (%s)", got.Risk, OpsCommandRiskMutating, got.Reason)
	}
}

func TestValidateToolCallByPolicyBlocksHighRiskEvenWhenManifestApproved(t *testing.T) {
	approved := []OpsApprovedCommand{{Tool: "bash", Command: `mysql -e "delete from users"`}}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "bash", map[string]interface{}{"command": `mysql -e "delete from users"`}, approved)
	if err == nil {
		t.Fatal("expected high-risk approved command to still be blocked")
	}
}

func TestValidateToolCallByPolicyBlocksHighRiskSSHExec(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":  "exec",
		"command": "terraform destroy -auto-approve",
	})
	if err == nil {
		t.Fatal("expected high-risk ssh exec to be blocked")
	}
}

func TestValidateToolCallByPolicyBlocksDocOnlySSHConnectWithoutInitialCommand(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterDocOnly, "ssh", map[string]interface{}{"action": "connect", "label": "prod"})
	if err == nil {
		t.Fatal("expected doc-only ssh connect to be blocked")
	}
}

func TestValidateToolCallByPolicyRequiresApprovedSSHConnectInControlledPhase(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterOpsControlled, "ssh", map[string]interface{}{"action": "connect", "label": "prod"})
	if err == nil {
		t.Fatal("expected controlled ssh connect without manifest to be blocked")
	}
	approved := []OpsApprovedCommand{{Tool: "ssh", Action: "connect", Target: "prod", Command: "connect"}}
	err = ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{"action": "connect", "label": "prod"}, approved)
	if err != nil {
		t.Fatalf("approved controlled ssh connect should pass: %v", err)
	}
}

func TestValidateToolCallByPolicyRequiresApprovedSSHConnectWithInitialCommand(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":          "connect",
		"label":           "prod",
		"initial_command": "uname -a",
	})
	if err == nil {
		t.Fatal("expected controlled ssh connect with initial command without manifest to be blocked")
	}
	approved := []OpsApprovedCommand{{Tool: "ssh", Action: "connect", Target: "prod", Command: "uname -a"}}
	err = ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":          "connect",
		"label":           "prod",
		"initial_command": "uname -a",
	}, approved)
	if err != nil {
		t.Fatalf("approved controlled ssh connect with initial command should pass: %v", err)
	}
	err = ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":          "connect",
		"label":           "prod",
		"initial_command": "hostname",
	}, approved)
	if err == nil {
		t.Fatal("expected unapproved connect initial command to be blocked")
	}
}

func TestValidateToolCallByPolicyWithApprovalIncludesSSHConnectPortInTarget(t *testing.T) {
	approved := []OpsApprovedCommand{{Tool: "ssh", Action: "connect", Target: "deploy@prod.example:22", Command: "connect"}}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action": "connect",
		"user":   "deploy",
		"host":   "prod.example",
		"port":   22,
	}, approved)
	if err != nil {
		t.Fatalf("approved ssh connect target with port should pass: %v", err)
	}
	err = ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action": "connect",
		"user":   "deploy",
		"host":   "prod.example",
		"port":   2222,
	}, approved)
	if err == nil {
		t.Fatal("expected ssh connect on unapproved port to be blocked")
	}
}

func TestValidateToolCallByPolicyBlocksUnknownSSHAction(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterOpsControlled, "ssh", map[string]interface{}{"action": "restart_service", "session_id": "prod"})
	if err == nil {
		t.Fatal("expected unknown ssh action to be blocked")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateToolCallByPolicyAllowsReadOnlyWithoutManifest(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterOpsControlled, "bash", map[string]interface{}{"command": "systemctl status nginx"})
	if err != nil {
		t.Fatalf("read-only command should be allowed without manifest: %v", err)
	}
	err = ValidateToolCallByPolicy(ToolFilterOpsControlled, "bash", map[string]interface{}{"command": "systemctl status nginx >/dev/null"})
	if err != nil {
		t.Fatalf("read-only command redirecting to /dev/null should be allowed without manifest: %v", err)
	}
}

func TestValidateToolCallByPolicyBlocksMissingExecutableCommands(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   map[string]interface{}
		policy ToolFilterPolicy
	}{
		{name: "bash", args: map[string]interface{}{}, policy: ToolFilterOpsControlled},
		{name: "bash", args: map[string]interface{}{"command": "   "}, policy: ToolFilterDocOnly},
		{name: "ssh", args: map[string]interface{}{"action": "exec"}, policy: ToolFilterOpsControlled},
		{name: "ssh", args: map[string]interface{}{"action": "exec_background", "command": ""}, policy: ToolFilterDocOnly},
	} {
		if err := ValidateToolCallByPolicy(tc.policy, tc.name, tc.args); err == nil {
			t.Fatalf("expected missing command to be blocked: policy=%s name=%s args=%#v", tc.policy, tc.name, tc.args)
		}
	}
}

func TestValidateToolCallByPolicyBlocksMutatingWithoutManifest(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterOpsControlled, "bash", map[string]interface{}{"command": "systemctl restart nginx"})
	if err == nil {
		t.Fatal("expected mutating command without manifest to be blocked")
	}
}

func TestValidateToolCallByPolicyBlocksDocOnlyBash(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterDocOnly, "bash", map[string]interface{}{"command": "systemctl restart nginx"})
	if err == nil {
		t.Fatal("expected doc-only bash command to be blocked")
	}
}

func TestValidateToolCallByPolicyBlocksDocOnlyReadOnlyBash(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterDocOnly, "bash", map[string]interface{}{"command": "systemctl status nginx && journalctl -u nginx -n 50"})
	if err == nil {
		t.Fatal("expected doc-only read-only bash command to be blocked by phase policy")
	}
}

func TestValidateToolCallByPolicyBlocksPlanningBash(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterPlanning, "bash", map[string]interface{}{"command": "git status --short && rg -n \"TODO\""})
	if err == nil {
		t.Fatal("expected planning bash command to be blocked by phase policy")
	}
}

func TestValidateToolCallByPolicyBlocksPlanningMutatingToolsAndCommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]interface{}
	}{
		{name: "write_file", args: map[string]interface{}{"path": "src/main.go", "content": "package main"}},
		{name: "edit_file", args: map[string]interface{}{"path": "src/main.go", "old": "a", "new": "b"}},
		{name: "edit_lines", args: map[string]interface{}{"path": "src/main.go"}},
		{name: "delegate_task", args: map[string]interface{}{"task": "implement"}},
		{name: "task", args: map[string]interface{}{"prompt": "implement"}},
		{name: "bash", args: map[string]interface{}{"command": "touch generated.go"}},
		{name: "bash", args: map[string]interface{}{"command": "echo ok > generated.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateToolCallByPolicy(ToolFilterPlanning, tc.name, tc.args); err == nil {
				t.Fatalf("expected planning policy to block %#v", tc)
			}
		})
	}
}

func TestValidateToolCallByPolicyBlocksDocOnlyFileWrites(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "write_file", path: "src/main.cpp"},
		{name: "edit_file", path: "CMakeLists.txt"},
		{name: "edit_lines", path: "package.json"},
		{name: "write_file", path: "docs/requirements.md"},
		{name: "edit_file", path: "notes/task-breakdown.txt"},
	} {
		t.Run(tc.name+"/"+tc.path, func(t *testing.T) {
			err := ValidateToolCallByPolicy(ToolFilterDocOnly, tc.name, map[string]interface{}{"path": tc.path})
			if err == nil {
				t.Fatalf("expected doc-only %s to block file mutation %q", tc.name, tc.path)
			}
		})
	}
}

func TestValidateToolCallByPolicyRestrictsDocOnlyOfficeActions(t *testing.T) {
	for _, action := range []string{"generate_pdf", "read_excel", "read_pptx"} {
		if err := ValidateToolCallByPolicy(ToolFilterDocOnly, "office", map[string]interface{}{"action": action}); err != nil {
			t.Fatalf("expected doc-only office action %s to pass: %v", action, err)
		}
	}
	for _, action := range []string{"", "write_excel", "unknown"} {
		if err := ValidateToolCallByPolicy(ToolFilterDocOnly, "office", map[string]interface{}{"action": action}); err == nil {
			t.Fatalf("expected doc-only office action %q to be blocked", action)
		}
	}
}

func TestValidateToolCallByPolicyRestrictsDocOnlyMemoryActions(t *testing.T) {
	for _, args := range []map[string]interface{}{
		{"action": "recall", "query": "requirements"},
		{"action": "themes"},
		{"action": "themes", "plan": true},
		{"action": "scenes"},
		{"action": "trace"},
		{"action": "candidates"},
		{"action": "derived"},
		{"action": "scene_index"},
		{"action": "memory_candidates"},
		{"action": "derived_audit"},
	} {
		if err := ValidateToolCallByPolicy(ToolFilterDocOnly, "memory", args); err != nil {
			t.Fatalf("expected doc-only memory action to pass: %#v: %v", args, err)
		}
	}
	for _, args := range []map[string]interface{}{
		{"action": ""},
		{"action": "save", "content": "remember this"},
		{"action": "delete", "id": "mem-1"},
		{"action": "list"},
		{"action": "derived_surgery", "id": "derived-1"},
		{"action": "supersede_derived", "id": "derived-1"},
		{"action": "themes", "apply": true},
		{"action": "candidates", "apply": true},
	} {
		if err := ValidateToolCallByPolicy(ToolFilterDocOnly, "memory", args); err == nil {
			t.Fatalf("expected doc-only memory action to be blocked: %#v", args)
		}
	}
}

func TestValidateToolCallByPolicyBlocksDocOnlyStatefulSSHActions(t *testing.T) {
	for _, args := range []map[string]interface{}{
		{"action": "upload", "local_path": "apply.sh", "remote_path": "/tmp/apply.sh"},
		{"action": "download", "remote_path": "/etc/passwd", "local_path": "passwd.copy"},
		{"action": "kill_task", "task_id": "task-1"},
		{"action": "sudo_prepare"},
		{"action": "close", "session_id": "prod-session"},
		{"action": "close_all"},
	} {
		if err := ValidateToolCallByPolicy(ToolFilterDocOnly, "ssh", args); err == nil {
			t.Fatalf("expected doc-only ssh action to be blocked: %#v", args)
		}
	}
}

func TestValidateToolCallByPolicyBlocksDocOnlyMutatingSSHExec(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterDocOnly, "ssh", map[string]interface{}{"action": "exec", "command": "kubectl scale deploy api --replicas=3"})
	if err == nil {
		t.Fatal("expected doc-only mutating ssh exec to be blocked")
	}
}

func TestValidateToolCallByPolicyBlocksShellWriteRedirectionWithoutManifest(t *testing.T) {
	for _, command := range []string{
		"echo enabled >/etc/app.conf",
		"systemctl status nginx 1>/tmp/status.log",
		"systemctl status nginx 2>/tmp/status.err",
		"systemctl status nginx &>/tmp/status.log",
		"cat <<'EOF' > /tmp/apply.sh\ntrue\nEOF",
		"systemctl status nginx && echo ok >> /tmp/audit.log",
		"systemctl status nginx >/dev/null 2>/tmp/status.err",
	} {
		err := ValidateToolCallByPolicy(ToolFilterOpsControlled, "bash", map[string]interface{}{"command": command})
		if err == nil {
			t.Fatalf("expected write redirection command %q without manifest to be blocked", command)
		}
	}
}

func TestValidateToolCallByPolicyAllowsDevNullRedirectionWithoutManifest(t *testing.T) {
	for _, command := range []string{
		"systemctl status nginx >/dev/null",
		"systemctl status nginx 1>/dev/null 2>/dev/null",
		"systemctl status nginx &>/dev/null",
		`grep "latency > 100" /var/log/app.log`,
		`awk '$1 > 100 { print $0 }' /var/log/app.log`,
	} {
		err := ValidateToolCallByPolicy(ToolFilterOpsControlled, "bash", map[string]interface{}{"command": command})
		if err != nil {
			t.Fatalf("expected /dev/null redirection command %q without manifest to pass: %v", command, err)
		}
	}
}

func TestExtractOpsApprovedCommands(t *testing.T) {
	got := ExtractOpsApprovedCommands(`
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
  - tool: ssh
    action: exec
    target: prod-session
    command: "journalctl -u nginx -n 100"
blocked_actions:
  - broad restart
`)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(got), got)
	}
	if got[0].Tool != "bash" || got[0].Command != "systemctl restart nginx" {
		t.Fatalf("unexpected first command: %#v", got[0])
	}
	if got[1].Tool != "ssh" || got[1].Action != "exec" || got[1].Target != "prod-session" || got[1].Command != "journalctl -u nginx -n 100" {
		t.Fatalf("unexpected second command: %#v", got[1])
	}
}

func TestExtractOpsApprovedCommandsStripsUnquotedLineComments(t *testing.T) {
	got := ExtractOpsApprovedCommands(`
decision: approval_required # reviewed by operator
risk_level: L2
approval_required: single
allowed_commands: # reviewed manifest
  - tool: bash # local host
    command: "systemctl restart nginx" # bounded service restart
  - tool: ssh
    action: exec
    target: prod-session # selected session
    command: "grep '# ERROR' /var/log/app.log" # quoted hash is part of command
`)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(got), got)
	}
	if got[0].Tool != "bash" || got[0].Command != "systemctl restart nginx" {
		t.Fatalf("unexpected first command: %#v", got[0])
	}
	if got[1].Target != "prod-session" || got[1].Command != "grep '# ERROR' /var/log/app.log" {
		t.Fatalf("unexpected second command: %#v", got[1])
	}
}

func TestExtractOpsApprovedCommandsRequiresExecutableDecision(t *testing.T) {
	for _, decision := range []string{"deny", "document_only", "propose", ""} {
		policy := `
decision: ` + decision + `
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`
		if got := ExtractOpsApprovedCommands(policy); len(got) != 0 {
			t.Fatalf("decision %q extracted commands unexpectedly: %#v", decision, got)
		}
	}

	got := ExtractOpsApprovedCommands(`
decision: auto_execute
risk_level: L1
approval_required: none
allowed_commands:
  - tool: bash
    command: "systemctl status nginx"
`)
	if len(got) != 1 {
		t.Fatalf("auto_execute should expose approved commands, got %#v", got)
	}
}

func TestExtractOpsApprovedCommandsAutoExecuteOnlyExposesReadOnlyCommands(t *testing.T) {
	got := ExtractOpsApprovedCommands(`
decision: auto_execute
risk_level: L1
approval_required: none
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
  - tool: bash
    command: "systemctl status nginx"
  - tool: ssh
    action: exec
    target: prod-session
    command: "journalctl -u nginx -n 100"
  - tool: ssh
    action: upload
    target: prod-session
    command: "apply.sh -> /tmp/apply.sh"
  - tool: ssh
    action: close_all
    command: "all"
`)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(got), got)
	}
	if got[0].Tool != "bash" || got[0].Command != "systemctl status nginx" {
		t.Fatalf("unexpected first auto-executable command: %#v", got[0])
	}
	if got[1].Tool != "ssh" || got[1].Action != "exec" || got[1].Command != "journalctl -u nginx -n 100" {
		t.Fatalf("unexpected second auto-executable command: %#v", got[1])
	}
}

func TestExtractOpsApprovedCommandsRequiresExecutableRiskLevel(t *testing.T) {
	for _, policy := range []string{
		`
decision: approval_required
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`,
		`
decision: approval_required
risk_level: L4
approval_required: double
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`,
		`
decision: auto_execute
risk_level: L2
approval_required: none
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`,
	} {
		if got := ExtractOpsApprovedCommands(policy); len(got) != 0 {
			t.Fatalf("policy should not expose executable commands: %#v", got)
		}
	}
}

func TestExtractOpsApprovedCommandsFiltersHighRiskManifestEntries(t *testing.T) {
	got := ExtractOpsApprovedCommands(`
decision: approval_required
risk_level: L3
approval_required: double
allowed_commands:
  - tool: bash
    command: "rm -rf /"
  - tool: bash
    command: "systemctl restart nginx"
  - tool: ssh
    action: exec
    target: prod-session
    command: "dd if=/tmp/image of=/dev/sda"
  - tool: ssh
    action: exec
    target: prod-session
    command: "journalctl -u nginx -n 100"
`)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(got), got)
	}
	if got[0].Tool != "bash" || got[0].Command != "systemctl restart nginx" {
		t.Fatalf("unexpected first retained command: %#v", got[0])
	}
	if got[1].Tool != "ssh" || got[1].Action != "exec" || got[1].Command != "journalctl -u nginx -n 100" {
		t.Fatalf("unexpected second retained command: %#v", got[1])
	}
}

func TestExtractOpsApprovedCommandsKeepsNonShellSSHDescriptors(t *testing.T) {
	got := ExtractOpsApprovedCommands(`
decision: approval_required
risk_level: L3
approval_required: double
allowed_commands:
  - tool: ssh
    action: upload
    target: prod-session
    command: "apply.sh -> /tmp/apply.sh"
  - tool: ssh
    action: close_all
    command: "all"
  - tool: ssh
    action: check_task
    command: "task-123"
`)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(got), got)
	}
	if got[0].Action != "upload" || got[0].Command != "apply.sh -> /tmp/apply.sh" {
		t.Fatalf("unexpected upload descriptor: %#v", got[0])
	}
	if got[1].Action != "close_all" || got[1].Command != "all" {
		t.Fatalf("unexpected close_all descriptor: %#v", got[1])
	}
	if got[1].RiskLevel != OpsRiskLevelL3 || got[1].ApprovalRequirement != OpsApprovalRequirementDouble {
		t.Fatalf("close_all should retain its policy strength metadata: %#v", got[1])
	}
}

func TestExtractOpsApprovedCommandsRequiresDoubleApprovalForSSHCloseAll(t *testing.T) {
	got := ExtractOpsApprovedCommands(`
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: ssh
    action: close_all
    command: "all"
  - tool: ssh
    action: close
    target: prod-session
    command: "prod-session"
`)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %#v", len(got), got)
	}
	if got[0].Action != "close" {
		t.Fatalf("close_all should require stronger approval, got %#v", got)
	}

	got = ExtractOpsApprovedCommands(`
decision: approval_required
risk_level: L3
approval_required: double
allowed_commands:
  - tool: ssh
    action: close_all
    command: "all"
`)
	if len(got) != 1 || got[0].Action != "close_all" {
		t.Fatalf("L3 double approval should retain close_all, got %#v", got)
	}
}

func TestExtractOpsApprovedCommandsFiltersIncompleteTargetedSSHEntries(t *testing.T) {
	got := ExtractOpsApprovedCommands(`
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: ssh
    action: exec
    command: "systemctl restart nginx"
  - tool: ssh
    action: upload
    command: "apply.sh -> /tmp/apply.sh"
  - tool: ssh
    action: upload
    target: prod-session
    command: "apply.sh -> /tmp/apply.sh"
  - tool: ssh
    action: close
    target: prod-session
    command: "prod-session"
`)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(got), got)
	}
	if got[0].Action != "upload" || got[0].Target != "prod-session" {
		t.Fatalf("unexpected targeted descriptor: %#v", got[0])
	}
	if got[1].Action != "close" || got[1].Target != "prod-session" {
		t.Fatalf("unexpected targeted descriptor: %#v", got[1])
	}
}

func TestExtractOpsRiskDecision(t *testing.T) {
	got := ExtractOpsRiskDecision("decision: Approval_Required\n")
	if got != OpsRiskDecisionApprovalRequired {
		t.Fatalf("decision = %q, want %q", got, OpsRiskDecisionApprovalRequired)
	}
	if OpsRiskDecisionAllowsExecution(OpsRiskDecisionDeny, OpsRiskLevelL1, OpsApprovalRequirementNone) {
		t.Fatal("deny decision must not allow execution")
	}
	if OpsRiskDecisionAllowsExecution(OpsRiskDecisionAutoExecute, OpsRiskLevelL2, OpsApprovalRequirementNone) {
		t.Fatal("auto_execute must be limited to L0/L1")
	}
	if OpsRiskDecisionAllowsExecution(OpsRiskDecisionApprovalRequired, OpsRiskLevelL3, OpsApprovalRequirementSingle) {
		t.Fatal("L3 approval_required must require double approval")
	}
	if !OpsRiskDecisionAllowsExecution(OpsRiskDecisionApprovalRequired, OpsRiskLevelL3, OpsApprovalRequirementDouble) {
		t.Fatal("approval_required should allow L3 after explicit double approval")
	}
}

func TestExtractOpsRiskLevel(t *testing.T) {
	got := ExtractOpsRiskLevel("risk_level: l3\n")
	if got != OpsRiskLevelL3 {
		t.Fatalf("risk level = %q, want %q", got, OpsRiskLevelL3)
	}
}

func TestExtractOpsApprovalRequirement(t *testing.T) {
	got := ExtractOpsApprovalRequirement("approval_required: double\n")
	if got != OpsApprovalRequirementDouble {
		t.Fatalf("approval requirement = %q, want %q", got, OpsApprovalRequirementDouble)
	}
}

func TestOpsApprovalDigestNormalizesLineEndingsAndTrim(t *testing.T) {
	a := OpsApprovalDigest(" decision: approval_required\r\nallowed_commands: []\r\n")
	b := OpsApprovalDigest("decision: approval_required\nallowed_commands: []")
	if a != b {
		t.Fatalf("digest should normalize trim and line endings: %q != %q", a, b)
	}
}

func TestValidateToolCallByPolicyWithApprovalRequiresExactCommand(t *testing.T) {
	approved := []OpsApprovedCommand{{Tool: "bash", Command: "systemctl restart nginx"}}
	if err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "bash", map[string]interface{}{"command": "systemctl   restart   nginx"}, approved); err != nil {
		t.Fatalf("approved command should pass: %v", err)
	}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "bash", map[string]interface{}{"command": "systemctl restart mysql"}, approved)
	if err == nil {
		t.Fatal("expected unapproved command to be blocked")
	}
}

func TestValidateToolCallByPolicyWithApprovalPreservesQuotedWhitespace(t *testing.T) {
	approved := []OpsApprovedCommand{{Tool: "bash", Command: `printf "a b" >/tmp/ops.txt`}}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "bash", map[string]interface{}{"command": `printf   "a b"   >/tmp/ops.txt`}, approved)
	if err != nil {
		t.Fatalf("outside whitespace should normalize: %v", err)
	}
	err = ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "bash", map[string]interface{}{"command": `printf "a  b" >/tmp/ops.txt`}, approved)
	if err == nil {
		t.Fatal("expected command with different quoted whitespace to be blocked")
	}
}

func TestValidateToolCallByPolicyWithApprovalDoesNotNormalizeCommandSeparatingNewline(t *testing.T) {
	approved := []OpsApprovedCommand{{Tool: "bash", Command: "printf ok true"}}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "bash", map[string]interface{}{"command": "printf ok\ntrue"}, approved)
	if err == nil {
		t.Fatal("expected command with newline separator to differ from approved space-separated command")
	}
}

func TestValidateToolCallByPolicyBlocksSSHUploadWithoutManifest(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":      "upload",
		"local_path":  "apply.sh",
		"remote_path": "/tmp/apply.sh",
	})
	if err == nil {
		t.Fatal("expected ssh upload without manifest to be blocked")
	}
}

func TestValidateToolCallByPolicyAllowsReadOnlySSHStatusActionsWithoutManifest(t *testing.T) {
	for _, args := range []map[string]interface{}{
		{"action": "check_task", "task_id": "task-1"},
		{"action": "list_tasks"},
		{"action": "list"},
	} {
		if err := ValidateToolCallByPolicy(ToolFilterOpsControlled, "ssh", args); err != nil {
			t.Fatalf("expected read-only ssh action to pass without manifest: %#v: %v", args, err)
		}
	}
}

func TestValidateToolCallByPolicyBlocksSSHCloseWithoutManifest(t *testing.T) {
	err := ValidateToolCallByPolicy(ToolFilterOpsControlled, "ssh", map[string]interface{}{"action": "close", "session_id": "prod-session"})
	if err == nil {
		t.Fatal("expected ssh close without manifest to be blocked")
	}
	err = ValidateToolCallByPolicy(ToolFilterOpsControlled, "ssh", map[string]interface{}{"action": "close_all"})
	if err == nil {
		t.Fatal("expected ssh close_all without manifest to be blocked")
	}
}

func TestValidateToolCallByPolicyWithApprovalAllowsSSHCloseDescriptor(t *testing.T) {
	approved := []OpsApprovedCommand{
		{Tool: "ssh", Action: "close", Target: "prod-session", Command: "prod-session"},
		{Tool: "ssh", Action: "close_all", Command: "all", RiskLevel: OpsRiskLevelL3, ApprovalRequirement: OpsApprovalRequirementDouble},
	}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{"action": "close", "session_id": "prod-session"}, approved)
	if err != nil {
		t.Fatalf("approved ssh close should pass: %v", err)
	}
	err = ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{"action": "close_all"}, approved)
	if err != nil {
		t.Fatalf("approved ssh close_all should pass: %v", err)
	}
}

func TestValidateToolCallByPolicyWithApprovalRequiresStrongCloseAllApprovalMetadata(t *testing.T) {
	for _, approved := range [][]OpsApprovedCommand{
		{{Tool: "ssh", Action: "close_all", Command: "all"}},
		{{Tool: "ssh", Action: "close_all", Command: "all", RiskLevel: OpsRiskLevelL2, ApprovalRequirement: OpsApprovalRequirementSingle}},
		{{Tool: "ssh", Action: "close_all", Command: "all", RiskLevel: OpsRiskLevelL3, ApprovalRequirement: OpsApprovalRequirementSingle}},
	} {
		err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{"action": "close_all"}, approved)
		if err == nil {
			t.Fatalf("expected close_all without L3 double approval metadata to be blocked: %#v", approved)
		}
	}
}

func TestValidateToolCallByPolicyWithApprovalBlocksEmptySSHActionDescriptor(t *testing.T) {
	approved := []OpsApprovedCommand{{Tool: "ssh", Action: "sudo_prepare", Target: "prod-session", Command: "prod-session"}}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{"action": "sudo_prepare"}, approved)
	if err == nil {
		t.Fatal("expected sudo_prepare without session_id to be blocked")
	}
	if !strings.Contains(err.Error(), "non-empty action descriptor") && !strings.Contains(err.Error(), "execution target") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateToolCallByPolicyWithApprovalAllowsSSHTaskAndSudoDescriptors(t *testing.T) {
	approved := []OpsApprovedCommand{
		{Tool: "ssh", Action: "kill_task", Target: "task-123", Command: "task-123"},
		{Tool: "ssh", Action: "sudo_prepare", Target: "prod-session", Command: "prod-session"},
	}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{"action": "kill_task", "task_id": "task-123"}, approved)
	if err != nil {
		t.Fatalf("approved ssh kill_task should pass: %v", err)
	}
	err = ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{"action": "sudo_prepare", "session_id": "prod-session"}, approved)
	if err != nil {
		t.Fatalf("approved ssh sudo_prepare should pass: %v", err)
	}
	err = ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{"action": "kill_task", "task_id": "task-456"}, approved)
	if err == nil {
		t.Fatal("expected unapproved ssh kill_task task_id to be blocked")
	}
}

func TestValidateToolCallByPolicyWithApprovalAllowsSSHUploadDescriptor(t *testing.T) {
	approved := []OpsApprovedCommand{{Tool: "ssh", Action: "upload", Target: "prod-session", Command: "apply.sh -> /tmp/apply.sh"}}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"session_id":  "prod-session",
		"action":      "upload",
		"local_path":  "apply.sh",
		"remote_path": "/tmp/apply.sh",
	}, approved)
	if err != nil {
		t.Fatalf("approved ssh upload should pass: %v", err)
	}
	err = ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"session_id":  "prod-session",
		"action":      "upload",
		"local_path":  "other.sh",
		"remote_path": "/tmp/apply.sh",
	}, approved)
	if err == nil {
		t.Fatal("expected unapproved ssh upload descriptor to be blocked")
	}
	err = ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"session_id":  "staging-session",
		"action":      "upload",
		"local_path":  "apply.sh",
		"remote_path": "/tmp/apply.sh",
	}, approved)
	if err == nil {
		t.Fatal("expected approved ssh upload descriptor on wrong target to be blocked")
	}
}

func TestValidateToolCallByPolicyBlocksSSHTransferMissingPaths(t *testing.T) {
	approved := []OpsApprovedCommand{
		{Tool: "ssh", Action: "upload", Target: "prod-session", Command: "apply.sh -> /tmp/apply.sh"},
		{Tool: "ssh", Action: "download", Target: "prod-session", Command: "/tmp/report.txt -> report.txt"},
	}
	for _, args := range []map[string]interface{}{
		{"session_id": "prod-session", "action": "upload", "remote_path": "/tmp/apply.sh"},
		{"session_id": "prod-session", "action": "upload", "local_path": "apply.sh"},
		{"session_id": "prod-session", "action": "download", "local_path": "report.txt"},
		{"session_id": "prod-session", "action": "download", "remote_path": "/tmp/report.txt"},
	} {
		err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", args, approved)
		if err == nil {
			t.Fatalf("expected transfer with missing path to be blocked: %#v", args)
		}
		if !strings.Contains(err.Error(), "requires non-empty") {
			t.Fatalf("unexpected error for %#v: %v", args, err)
		}
	}
}

func TestValidateToolCallByPolicyWithApprovalRequiresSSHTargetMatch(t *testing.T) {
	approved := []OpsApprovedCommand{{Tool: "ssh", Action: "exec", Target: "prod-session", Command: "systemctl restart nginx"}}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":     "exec",
		"session_id": "prod-session",
		"command":    "systemctl restart nginx",
	}, approved)
	if err != nil {
		t.Fatalf("approved ssh exec target should pass: %v", err)
	}
	err = ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":     "exec",
		"session_id": "staging-session",
		"command":    "systemctl restart nginx",
	}, approved)
	if err == nil {
		t.Fatal("expected approved ssh exec on wrong target to be blocked")
	}
}

func TestValidateToolCallByPolicyWithApprovalRequiresCaseSensitiveSSHTargetMatch(t *testing.T) {
	approved := []OpsApprovedCommand{{Tool: "ssh", Action: "exec", Target: "Prod-Session", Command: "systemctl restart nginx"}}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":     "exec",
		"session_id": "Prod-Session",
		"command":    "systemctl restart nginx",
	}, approved)
	if err != nil {
		t.Fatalf("exact-case ssh exec target should pass: %v", err)
	}
	err = ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":     "exec",
		"session_id": "prod-session",
		"command":    "systemctl restart nginx",
	}, approved)
	if err == nil {
		t.Fatal("expected target with different case to be blocked")
	}
}

func TestValidateToolCallByPolicyWithApprovalRequiresExplicitSSHTargetInManifest(t *testing.T) {
	approved := []OpsApprovedCommand{{Tool: "ssh", Action: "exec", Command: "systemctl restart nginx"}}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":     "exec",
		"session_id": "prod-session",
		"command":    "systemctl restart nginx",
	}, approved)
	if err == nil {
		t.Fatal("expected ssh exec manifest without target to be blocked")
	}
	if !strings.Contains(err.Error(), "explicit target") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateToolCallByPolicyWithApprovalContinuesPastIncompleteSSHManifestCandidate(t *testing.T) {
	approved := []OpsApprovedCommand{
		{Tool: "ssh", Command: "systemctl restart nginx"},
		{Tool: "ssh", Action: "exec", Command: "systemctl restart nginx"},
		{Tool: "ssh", Action: "exec", Target: "prod-session", Command: "systemctl restart nginx"},
	}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":     "exec",
		"session_id": "prod-session",
		"command":    "systemctl restart nginx",
	}, approved)
	if err != nil {
		t.Fatalf("complete later manifest entry should pass despite earlier incomplete candidates: %v", err)
	}
}

func TestValidateToolCallByPolicyWithApprovalContinuesPastIncompleteSSHActionCandidate(t *testing.T) {
	approved := []OpsApprovedCommand{
		{Tool: "ssh", Action: "upload", Command: "apply.sh -> /tmp/apply.sh"},
		{Tool: "ssh", Action: "upload", Target: "prod-session", Command: "apply.sh -> /tmp/apply.sh"},
	}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":      "upload",
		"session_id":  "prod-session",
		"local_path":  "apply.sh",
		"remote_path": "/tmp/apply.sh",
	}, approved)
	if err != nil {
		t.Fatalf("complete later action manifest entry should pass despite earlier missing target: %v", err)
	}
}

func TestValidateToolCallByPolicyWithApprovalRequiresExplicitSSHActionInManifest(t *testing.T) {
	approved := []OpsApprovedCommand{{Tool: "ssh", Target: "prod-session", Command: "systemctl restart nginx"}}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":     "exec",
		"session_id": "prod-session",
		"command":    "systemctl restart nginx",
	}, approved)
	if err == nil {
		t.Fatal("expected ssh exec manifest without action to be blocked")
	}
	if !strings.Contains(err.Error(), "explicit action") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateToolCallByPolicyWithApprovalRequiresActualSSHTarget(t *testing.T) {
	approved := []OpsApprovedCommand{{Tool: "ssh", Action: "exec", Target: "prod-session", Command: "systemctl restart nginx"}}
	err := ValidateToolCallByPolicyWithApproval(ToolFilterOpsControlled, "ssh", map[string]interface{}{
		"action":  "exec",
		"command": "systemctl restart nginx",
	}, approved)
	if err == nil {
		t.Fatal("expected ssh exec without actual target to be blocked")
	}
	if !strings.Contains(err.Error(), "requires an execution target") {
		t.Fatalf("unexpected error: %v", err)
	}
}
