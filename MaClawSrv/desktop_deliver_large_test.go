package main

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestDeliverLargeSendsThePiecesAsOneMessage(t *testing.T) {
	pb, pbytes, psay := desktopDeliverLargeBash, desktopDeliverLargeBytes, desktopDeliverLargeSay
	defer func() { desktopDeliverLargeBash, desktopDeliverLargeBytes, desktopDeliverLargeSay = pb, pbytes, psay }()
	var cmds []string
	desktopDeliverLargeBash = func(_ context.Context, _ agentruntime.Scope, c string) (string, error) {
		cmds = append(cmds, c)
		if strings.HasPrefix(c, "rm ") {
			return "", nil
		}
		return "exit 0\nMCLDL /tmp/maclaw-deliver.abcDEF12 900000\n", nil
	}
	desktopDeliverLargeBytes = func(_ context.Context, _ agentruntime.Scope, p string) (string, error) {
		n := 400000
		if strings.HasSuffix(p, "p.002") {
			n = 100000
		}
		return base64.StdEncoding.EncodeToString(make([]byte, n)), nil
	}
	var said map[string]any
	desktopDeliverLargeSay = func(_ context.Context, b map[string]any) error { said = b; return nil }
	scope := agentruntime.Scope{TenantID: "t", UserID: "u::desk::bot_x", InstanceID: "inst_1", RunID: "run_1"}
	out, err := deliverDesktopLarge(context.Background(), scope, "/tmp/a b'c.pdf", "a.pdf")
	if err != nil || !strings.Contains(out, "own chat message") {
		t.Fatalf("%q %v", out, err)
	}
	files, _ := said["files"].([]map[string]string)
	raw, _ := base64.StdEncoding.DecodeString(files[0]["data"])
	if len(raw) != 900000 || said["instance_id"] != "inst_1" || !strings.Contains(cmds[0], `'/tmp/a b'\''c.pdf'`) || !strings.HasPrefix(cmds[len(cmds)-1], "rm -rf -- ") {
		t.Fatalf("said %v len=%d cmds=%v", said["text"], len(raw), cmds)
	}
}

func TestDeliverLargeOverTheCapNamesTheLimit(t *testing.T) {
	pb := desktopDeliverLargeBash
	defer func() { desktopDeliverLargeBash = pb }()
	desktopDeliverLargeBash = func(context.Context, agentruntime.Scope, string) (string, error) { return "MCLDL_BIG 9999999\n", nil }
	_, err := deliverDesktopLarge(context.Background(), agentruntime.Scope{InstanceID: "i", UserID: "u"}, "/tmp/x.pdf", "x.pdf")
	if err == nil || !strings.Contains(err.Error(), "8388608") || !strings.Contains(err.Error(), "Do not shrink") {
		t.Fatalf("%v", err)
	}
}
