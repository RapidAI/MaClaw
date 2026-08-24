package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestSendProactiveFileToPeerUsesExactSessionAndNotLastActive(t *testing.T) {
	gw := &fakeSrvWeixinGateway{
		tokens: map[string]string{"wx-1": "tok-1"},
	}
	mgr := &srvWeixinGatewayManager{runtimes: map[string]*srvWeixinRuntime{
		"t\x00u": {
			principal:  agentservice.Principal{TenantID: "t", UserID: "u"},
			gateway:    gw,
			status:     srvWeixinRuntimeStatus{Status: srvWeixinStatusConnected},
			lastUserID: "wx-other",
		},
	}}

	if err := mgr.SendProactiveFileToPeer(context.Background(), "wx-other", []byte("xlsx"), "sheet.xlsx", ""); err == nil {
		t.Fatal("last-active peer without a token must not receive the file")
	}
	if len(gw.sentMedia) != 0 {
		t.Fatalf("sentMedia=%#v", gw.sentMedia)
	}

	if err := mgr.SendProactiveFileToPeer(context.Background(), "wx-1", []byte("xlsx"), "sheet.xlsx", ""); err != nil {
		t.Fatal(err)
	}
	if len(gw.sentMedia) != 1 || gw.sentMedia[0].ToUserID != "wx-1" || gw.sentMedia[0].ContextToken != "tok-1" || string(gw.sentMedia[0].FileData) != "xlsx" || gw.sentMedia[0].FileName != "sheet.xlsx" || gw.sentMedia[0].MediaType != "file" {
		t.Fatalf("sentMedia=%#v", gw.sentMedia)
	}

	if err := mgr.SendProactiveFileToPeer(context.Background(), "self", []byte("xlsx"), "sheet.xlsx", ""); err == nil || !strings.Contains(err.Error(), "exact target") {
		t.Fatalf("self must fail closed, err=%v", err)
	}
	if err := (*srvWeixinGatewayManager)(nil).SendProactiveFileToPeer(context.Background(), "wx-1", []byte("xlsx"), "sheet.xlsx", ""); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("nil manager must stay unavailable, err=%v", err)
	}
}
