package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestDeliverLargeSendsThePiecesAsOneMessage(t *testing.T) {
	pb, pbytes, psay, pup := desktopDeliverLargeBash, desktopDeliverLargeBytes, desktopDeliverLargeSay, desktopSayUploadBytes
	defer func() {
		desktopDeliverLargeBash, desktopDeliverLargeBytes, desktopDeliverLargeSay, desktopSayUploadBytes = pb, pbytes, psay, pup
	}()
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
	var uploaded int
	desktopSayUploadBytes = func(_ context.Context, _ agentruntime.Scope, _, _ string, raw []byte) (string, error) {
		uploaded = len(raw)
		return "bf_00000000000000000000001", nil
	}
	desktopDeliverLargeSay = func(_ context.Context, b map[string]any) error { said = b; return nil }
	scope := agentruntime.Scope{TenantID: "t", UserID: "u::desk::bot_x", InstanceID: "inst_1", RunID: "run_1"}
	out, err := deliverDesktopLarge(context.Background(), scope, "/tmp/a b'c.pdf", "a.pdf")
	if err != nil || !strings.Contains(out, "own chat message") {
		t.Fatalf("%q %v", out, err)
	}
	files, _ := said["files"].([]map[string]string)
	if uploaded != 900000 || files[0]["ref"] == "" || files[0]["data"] != "" || said["instance_id"] != "inst_1" || !strings.Contains(cmds[0], `'/tmp/a b'\''c.pdf'`) || !strings.HasPrefix(cmds[len(cmds)-1], "rm -rf -- ") {
		t.Fatalf("said %v uploaded=%d files=%v cmds=%v", said["text"], uploaded, files, cmds)
	}
}

func TestDeliverWithoutReplySlotSendsTheFileOnItsOwn(t *testing.T) {
	oldRemote, oldUpload, oldSay := desktopRemoteFile, desktopSayUploadBytes, desktopDeliverLargeSay
	defer func() {
		desktopRemoteFile, desktopSayUploadBytes, desktopDeliverLargeSay = oldRemote, oldUpload, oldSay
	}()
	desktopRemoteFile = func(context.Context, string, string, string, string, string, string, string) (string, error) {
		return base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 weather")), nil
	}
	var uploaded []byte
	var said map[string]any
	desktopDeliverLargeSay = func(_ context.Context, body map[string]any) error { said = body; return nil }
	desktopSayUploadBytes = func(_ context.Context, _ agentruntime.Scope, name, mime string, raw []byte) (string, error) {
		if name != "weather-report.pdf" || mime != "application/pdf" {
			t.Fatalf("upload name %q mime %q", name, mime)
		}
		uploaded = append([]byte(nil), raw...)
		return "bf_weather", nil
	}
	out, err := deliverDesktopPath(context.Background(), agentruntime.Scope{TenantID: "t", UserID: "u", InstanceID: "inst"}, "/home/desktop/work/main/run/weather-report.pdf", "")
	files, _ := said["files"].([]map[string]string)
	if err != nil || len(files) != 1 || files[0]["ref"] != "bf_weather" || files[0]["data"] != "" || string(uploaded) != "%PDF-1.4 weather" {
		t.Fatalf("out %q said %v uploaded %q err %v", out, said, uploaded, err)
	}
	if strings.Contains(out, "/home/desktop") || strings.Contains(out, "larger than one reply") || strings.Contains(out, "give the file's path") || !strings.Contains(out, "own chat message") || !strings.Contains(out, "cloud path") {
		t.Fatalf("tool result %q", out)
	}

	desktopSayUploadBytes = func(context.Context, agentruntime.Scope, string, string, []byte) (string, error) {
		return "", fmt.Errorf("no bot file store yet")
	}
	said = nil
	out, err = deliverDesktopPath(context.Background(), agentruntime.Scope{TenantID: "t", UserID: "u::desk::bot_x", InstanceID: "inst", RunID: "run"}, "/home/desktop/work/main/run/weather-report.pdf", "")
	files, _ = said["files"].([]map[string]string)
	if err != nil || len(files) != 1 || said["instance_id"] != "inst" || files[0]["ref"] != "" {
		t.Fatalf("fallback out %q said %v err %v", out, said, err)
	}
	raw, _ := base64.StdEncoding.DecodeString(files[0]["data"])
	if string(raw) != "%PDF-1.4 weather" {
		t.Fatalf("fallback out %q said %v err %v", out, said, err)
	}
	if strings.Contains(out, "/home/desktop") || strings.Contains(out, "larger than one reply") || strings.Contains(out, "give the file's path") || !strings.Contains(out, "own chat message") || !strings.Contains(out, "cloud path") {
		t.Fatalf("fallback tool result %q", out)
	}
}

func TestDeliverWithoutReplySlotDoesNotOfferTheCloudPath(t *testing.T) {
	oldRemote, oldBash := desktopRemoteFile, desktopDeliverLargeBash
	defer func() { desktopRemoteFile, desktopDeliverLargeBash = oldRemote, oldBash }()
	desktopRemoteFile = func(context.Context, string, string, string, string, string, string, string) (string, error) {
		return "", fmt.Errorf("file is too large")
	}
	desktopDeliverLargeBash = func(context.Context, agentruntime.Scope, string) (string, error) {
		return "MCLDL_BIG 9999999\n", nil
	}
	_, err := deliverDesktopPath(context.Background(), agentruntime.Scope{TenantID: "t", UserID: "u", InstanceID: "inst"}, "/home/desktop/work/main/run/weather-report.pdf", "")
	if err == nil || strings.Contains(err.Error(), "path on the computer") || strings.Contains(err.Error(), "give the file's path") || !strings.Contains(err.Error(), "cloud path") {
		t.Fatalf("%v", err)
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
