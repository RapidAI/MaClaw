package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestCodingTemplateLocalRemoteVariants(t *testing.T) {
	tmpl := v2.CodingTemplate()
	if tmpl == nil || len(tmpl.Phases) == 0 || tmpl.Phases[0].InputSchema == nil {
		t.Fatal("coding template missing requirements input schema")
	}
	schema := tmpl.Phases[0].InputSchema
	if len(schema.Variants) != 2 {
		t.Fatalf("variants = %d, want 2 (local/remote)", len(schema.Variants))
	}
	if schema.Variants[0].ID != "local" || schema.Variants[1].ID != "remote" {
		t.Fatalf("variant ids = %q/%q, want local/remote", schema.Variants[0].ID, schema.Variants[1].ID)
	}
	remoteFields := map[string]bool{}
	for _, f := range schema.Variants[1].Fields {
		remoteFields[f.Name] = true
		if f.Name == "ssh_password" && !f.Sensitive {
			t.Fatal("ssh_password must be Sensitive")
		}
	}
	for _, name := range []string{"ssh_profile", "remote_host", "remote_user", "remote_port", "ssh_password", "ssh_key_path", "remote_workdir"} {
		if !remoteFields[name] {
			t.Fatalf("remote field %q missing", name)
		}
	}
	for _, name := range []string{"project_name", "tech_stack", "description"} {
		found := false
		for _, f := range schema.Fields {
			if f.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("common field %q missing", name)
		}
	}
	for _, f := range schema.Fields {
		if f.Name == "project_path" {
			t.Fatal("project_path should live in local variant only")
		}
	}
}

func TestResolveCodingWorkflowRemoteFormData(t *testing.T) {
	hosts := []corelib.SSHHostEntry{
		{Label: "gpu-box", Host: "192.168.1.10", User: "root", Port: 2222, Password: "secret", KeyPath: ""},
	}

	// Local variant is a no-op.
	local := map[string]interface{}{
		"_agent_view_variant": "local",
		"project_name":        "snake",
	}
	if err := resolveCodingWorkflowRemoteFormData(hosts, local); err != nil {
		t.Fatalf("local variant: %v", err)
	}

	// Missing profile.
	missing := map[string]interface{}{
		"_agent_view_variant": "remote",
		"remote_workdir":      "/home/root/app",
	}
	if err := resolveCodingWorkflowRemoteFormData(hosts, missing); err == nil {
		t.Fatal("expected error for missing ssh_profile")
	}

	// Unknown profile.
	unknown := map[string]interface{}{
		"_agent_view_variant": "remote",
		"ssh_profile":         "nope",
		"remote_workdir":      "/home/root/app",
	}
	if err := resolveCodingWorkflowRemoteFormData(hosts, unknown); err == nil {
		t.Fatal("expected error for unknown profile")
	}

	// Profile happy path expands host/user/port and keeps password for session vault.
	ok := map[string]interface{}{
		"_agent_view_variant": "remote",
		"ssh_profile":         "gpu-box",
		"remote_workdir":      " /home/root/app ",
	}
	if err := resolveCodingWorkflowRemoteFormData(hosts, ok); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if fmt.Sprint(ok["remote_host"]) != "192.168.1.10" {
		t.Fatalf("remote_host = %v", ok["remote_host"])
	}
	if fmt.Sprint(ok["remote_user"]) != "root" {
		t.Fatalf("remote_user = %v", ok["remote_user"])
	}
	if ok["remote_port"] != 2222 {
		t.Fatalf("remote_port = %v", ok["remote_port"])
	}
	if fmt.Sprint(ok["remote_workdir"]) != "/home/root/app" {
		t.Fatalf("remote_workdir = %v", ok["remote_workdir"])
	}
	if fmt.Sprint(ok["ssh_password"]) != "secret" {
		t.Fatalf("profile password should be copied for session vault, got %v", ok["ssh_password"])
	}

	// New connection requires host/user and password or key.
	newMissing := map[string]interface{}{
		"_agent_view_variant": "remote",
		"ssh_profile":         workflowFormSSHProfileNew,
		"remote_workdir":      "/tmp/app",
		"remote_host":         "10.0.0.1",
		"remote_user":         "ubuntu",
	}
	if err := resolveCodingWorkflowRemoteFormData(hosts, newMissing); err == nil {
		t.Fatal("new connection without password/key should fail")
	}
	newOK := map[string]interface{}{
		"_agent_view_variant": "remote",
		"ssh_profile":         workflowFormSSHProfileNew,
		"remote_workdir":      "/tmp/app",
		"remote_host":         "10.0.0.1",
		"remote_user":         "ubuntu",
		"remote_port":         22,
		"ssh_password":        "p@ss",
	}
	if err := resolveCodingWorkflowRemoteFormData(nil, newOK); err != nil {
		t.Fatalf("new connection: %v", err)
	}
	creds := captureCodingWorkflowRemoteCreds(newOK)
	if creds.Host != "10.0.0.1" || creds.User != "ubuntu" || creds.Password != "p@ss" || creds.WorkDir != "/tmp/app" {
		t.Fatalf("capture creds = %#v", creds)
	}
}

func TestSSHHostProfileOptions(t *testing.T) {
	empty := sshHostProfileOptions(nil)
	if len(empty) != 1 || empty[0]["value"] != workflowFormSSHProfileNew {
		t.Fatalf("empty hosts should still offer new connection, got %#v", empty)
	}
	opts := sshHostProfileOptions([]corelib.SSHHostEntry{
		{Label: "prod", Host: "10.0.0.1", User: "ubuntu", Port: 22},
	})
	if len(opts) != 2 {
		t.Fatalf("options len = %d, want 2 (host + new)", len(opts))
	}
	if opts[0]["value"] != "prod" {
		t.Fatalf("first option = %#v", opts[0])
	}
	if opts[0]["label"] != "prod (ubuntu@10.0.0.1:22)" {
		t.Fatalf("label = %q", opts[0]["label"])
	}
	if opts[1]["value"] != workflowFormSSHProfileNew {
		t.Fatalf("last option should be new connection, got %#v", opts[1])
	}
}

func TestCodingWorkflowRemoteEnvFromState(t *testing.T) {
	local := &v2.WorkflowState{
		Phases: []v2.Phase{{
			FormData: map[string]interface{}{
				"_agent_view_variant": "local",
				"project_path":        "D:\\proj",
			},
		}},
	}
	if isCodingWorkflowRemoteExecution(local) {
		t.Fatal("local variant must not be remote execution")
	}
	remote := &v2.WorkflowState{
		Phases: []v2.Phase{{
			FormData: map[string]interface{}{
				"_agent_view_variant": "remote",
				"ssh_profile":         "prod",
				"remote_host":         "10.0.0.8",
				"remote_user":         "ubuntu",
				"remote_port":         22,
				"remote_workdir":      "/home/ubuntu/app",
			},
		}},
	}
	if !isCodingWorkflowRemoteExecution(remote) {
		t.Fatal("remote variant should be remote execution")
	}
	host, user, wd, port, ok := codingWorkflowRemoteEnvFromState(remote)
	if !ok || host != "10.0.0.8" || user != "ubuntu" || wd != "/home/ubuntu/app" || port != 22 {
		t.Fatalf("env = %s %s %s %d ok=%v", host, user, wd, port, ok)
	}
}

func TestStoreAndLoadCodingWorkflowRemoteCreds(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/proj"
	h.storeCodingWorkflowRemoteCreds(userID, codingWorkflowRemoteCreds{
		Host: "10.0.0.1", User: "u", Port: 22, Password: "x", WorkDir: "/a",
	})
	got, ok := h.loadCodingWorkflowRemoteCreds(userID)
	if !ok || got.Password != "x" || got.Host != "10.0.0.1" {
		t.Fatalf("load = %#v ok=%v", got, ok)
	}
	h.clearCodingWorkflowRemoteCreds(userID)
	if _, ok := h.loadCodingWorkflowRemoteCreds(userID); ok {
		t.Fatal("cleared creds should be gone")
	}
}

func TestRemoteTaskPathFromWorkflowState(t *testing.T) {
	if got := remoteTaskPathFromWorkflowState(nil); got != "" {
		t.Fatalf("nil state = %q", got)
	}
	st := &v2.WorkflowState{
		Phases: []v2.Phase{
			{FormData: map[string]interface{}{"project_name": "x"}},
			{FormData: map[string]interface{}{workflowFormRemoteTaskPath: `D:\tasks\remote-1`}},
		},
	}
	got := remoteTaskPathFromWorkflowState(st)
	if got == "" {
		t.Fatal("expected remote_task_path")
	}
}

func TestSyncCodingWorkflowRemoteTaskCreatesRecord(t *testing.T) {
	app := newProjectSearchTestApp(t)
	h := &IMMessageHandler{app: app}
	userID := "desktop-user:C:/wf"
	data := map[string]interface{}{
		"_agent_view_variant":       workflowFormExecRemote,
		"project_name":              "站点修复",
		workflowFormRemoteHostField: "10.0.0.8",
		workflowFormRemoteUserField: "ubuntu",
		workflowFormRemotePortField: 22,
		workflowFormRemoteWorkDir:   "/var/www/app",
	}
	h.syncCodingWorkflowRemoteTask(userID, data, &v2.WorkflowState{Summary: "fix site"})
	path := formDataTrimString(data, workflowFormRemoteTaskPath)
	if path == "" {
		t.Fatal("expected remote_task_path after sync")
	}
	meta, err := app.GetRemoteCodingTaskMeta(path)
	if err != nil {
		t.Fatalf("GetRemoteCodingTaskMeta: %v", err)
	}
	if meta.Host != "10.0.0.8" || meta.User != "ubuntu" || meta.WorkDir != "/var/www/app" {
		t.Fatalf("meta = %#v", meta)
	}
	// Must be tagged as coding-workflow origin for sidebar badge.
	rec := app.memoryStore.ProjectIndex().Get(path)
	if rec == nil {
		t.Fatal("task record missing")
	}
	hasSource := false
	for _, tag := range rec.Tags {
		if tag == taskSourceCodingWorkflowTag {
			hasSource = true
			break
		}
	}
	if !hasSource {
		t.Fatalf("tags missing %s: %#v", taskSourceCodingWorkflowTag, rec.Tags)
	}
	// Second sync updates rather than creating a blank path.
	data[workflowFormRemoteWorkDir] = "/var/www/app2"
	h.syncCodingWorkflowRemoteTask(userID, data, nil)
	meta2, err := app.GetRemoteCodingTaskMeta(path)
	if err != nil {
		t.Fatalf("GetRemoteCodingTaskMeta after update: %v", err)
	}
	if meta2.WorkDir != "/var/www/app2" {
		t.Fatalf("workdir after update = %q", meta2.WorkDir)
	}
	// Password must never appear in tags.
	rec2 := app.memoryStore.ProjectIndex().Get(path)
	if rec2 == nil {
		t.Fatal("task record missing after update")
	}
	for _, tag := range rec2.Tags {
		if strings.Contains(strings.ToLower(tag), "password") || strings.Contains(tag, "secret") {
			t.Fatalf("sensitive tag leaked: %q", tag)
		}
	}
}

func TestFindRemoteCodingTaskByMetaAndDedupeSync(t *testing.T) {
	app := newProjectSearchTestApp(t)
	first := app.CreateRemoteCodingTask("原任务", "10.0.0.5", "dev", "/srv/app/", 22)
	if first.ProjectPath == "" {
		t.Fatal("create failed")
	}
	// Trailing slash should still match.
	found := app.FindRemoteCodingTaskByMeta("10.0.0.5", "dev", "/srv/app")
	if normalizeProjectSessionPath(found.ProjectPath) != normalizeProjectSessionPath(first.ProjectPath) {
		t.Fatalf("FindRemoteCodingTaskByMeta = %#v, want %s", found, first.ProjectPath)
	}

	h := &IMMessageHandler{app: app}
	// New workflow form without remote_task_path should reuse by meta.
	data := map[string]interface{}{
		"_agent_view_variant":       workflowFormExecRemote,
		"project_name":              "再次修复",
		workflowFormRemoteHostField: "10.0.0.5",
		workflowFormRemoteUserField: "dev",
		workflowFormRemotePortField: 2222,
		workflowFormRemoteWorkDir:   "/srv/app",
	}
	h.syncCodingWorkflowRemoteTask("desktop-user:C:/other", data, nil)
	if formDataTrimString(data, workflowFormRemoteTaskPath) != normalizeProjectSessionPath(first.ProjectPath) {
		t.Fatalf("dedupe path = %q, want %q", data[workflowFormRemoteTaskPath], first.ProjectPath)
	}
	meta, err := app.GetRemoteCodingTaskMeta(first.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Port != 2222 {
		t.Fatalf("port after dedupe update = %d, want 2222", meta.Port)
	}
}

func TestSyncCodingWorkflowRemoteTaskSwitchesToExistingTarget(t *testing.T) {
	app := newProjectSearchTestApp(t)
	first := app.CreateRemoteCodingTask("workflow original", "10.0.0.6", "dev", "/srv/original", 22)
	second := app.CreateRemoteCodingTask("existing target", "10.0.0.6", "dev", "/srv/target", 22)
	if first.ProjectPath == "" || second.ProjectPath == "" {
		t.Fatal("failed to create remote task fixtures")
	}

	h := &IMMessageHandler{app: app}
	data := map[string]interface{}{
		"_agent_view_variant":       workflowFormExecRemote,
		"project_name":              "workflow retarget",
		workflowFormRemoteTaskPath:  first.ProjectPath,
		workflowFormRemoteHostField: "10.0.0.6",
		workflowFormRemoteUserField: "dev",
		workflowFormRemotePortField: 2200,
		workflowFormRemoteWorkDir:   "/srv/target/",
	}
	h.syncCodingWorkflowRemoteTask("desktop-user:C:/workflow", data, nil)

	if got := formDataTrimString(data, workflowFormRemoteTaskPath); got != normalizeProjectSessionPath(second.ProjectPath) {
		t.Fatalf("workflow remote task path = %q, want existing target %q", got, second.ProjectPath)
	}
	firstMeta, err := app.GetRemoteCodingTaskMeta(first.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if firstMeta.WorkDir != "/srv/original" {
		t.Fatalf("original task was incorrectly retargeted: %+v", firstMeta)
	}
	secondMeta, err := app.GetRemoteCodingTaskMeta(second.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if secondMeta.Port != 2200 || secondMeta.WorkDir != "/srv/target/" {
		t.Fatalf("target task metadata = %+v", secondMeta)
	}
	if matched := app.FindRemoteCodingTaskByMeta("10.0.0.6", "dev", "/srv/target"); normalizeProjectSessionPath(matched.ProjectPath) != normalizeProjectSessionPath(second.ProjectPath) {
		t.Fatalf("target lookup = %q, want %q", matched.ProjectPath, second.ProjectPath)
	}
}

func TestPrefillCodingRemoteEnvFieldsFromSticky(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/wf"
	h.storeStickyCodingWorkbenchMemory(userID, stickyCodingWorkbenchMemory{
		Kind:          "remote",
		RemoteHost:    "192.168.0.9",
		RemoteUser:    "ubuntu",
		RemotePort:    22,
		RemoteWorkDir: "/home/ubuntu/p",
	})
	schema := v2.CodingTemplate().Phases[0].InputSchema
	phase := &v2.Phase{ID: "requirements", InputSchema: schema}
	got := h.prefillCodingRemoteEnvFields(userID, phase, nil)
	if got == nil {
		t.Fatal("expected prefill")
	}
	if fmt.Sprint(got[workflowFormRemoteHostField].Value) != "192.168.0.9" {
		t.Fatalf("host prefill = %#v", got[workflowFormRemoteHostField])
	}
	if fmt.Sprint(got[workflowFormRemoteUserField].Value) != "ubuntu" {
		t.Fatalf("user prefill = %#v", got[workflowFormRemoteUserField])
	}
	if fmt.Sprint(got[workflowFormRemoteWorkDir].Value) != "/home/ubuntu/p" {
		t.Fatalf("workdir prefill = %#v", got[workflowFormRemoteWorkDir])
	}
	// Must never invent password.
	if _, ok := got[workflowFormSSHPasswordField]; ok {
		t.Fatal("ssh_password must not be prefilled")
	}
}

func TestNormalizeRemoteWorkDirKey(t *testing.T) {
	if normalizeRemoteWorkDirKey("/a/b/") != "/a/b" {
		t.Fatal(normalizeRemoteWorkDirKey("/a/b/"))
	}
	if normalizeRemoteWorkDirKey(`C:\x\`) != `C:\x` && normalizeRemoteWorkDirKey(`C:\x\`) != "C:\\x" {
		// TrimRight on backslash
		got := normalizeRemoteWorkDirKey(`C:\x\`)
		if got != `C:\x` {
			t.Fatalf("got %q", got)
		}
	}
}

func TestNormalizeRemoteWorkDirKeyCanonicalizesEquivalentPOSIXPaths(t *testing.T) {
	for _, workDir := range []string{"/srv/app", "/srv/app/", "/srv//app//", "/srv/app/./", "/srv/other/../app"} {
		if got := normalizeRemoteWorkDirKey(workDir); got != "/srv/app" {
			t.Fatalf("normalizeRemoteWorkDirKey(%q) = %q, want /srv/app", workDir, got)
		}
	}
	if got := normalizeRemoteWorkDirKey("/"); got != "/" {
		t.Fatalf("root remote workdir = %q, want /", got)
	}
}
