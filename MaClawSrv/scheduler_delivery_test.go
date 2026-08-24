package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestNewChannelSenderUnsupported(t *testing.T) {
	_, err := newChannelSender(context.Background(), nil, agentservice.Principal{}, "discord")
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("want unsupported channel err, got %v", err)
	}
}

func TestChannelSenderDeliveryStateIsScopedToPrincipal(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "01234567890123456789012345678901",
		TokenTTL:    time.Hour,
	}, agentservice.NewMemoryStore(), nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}
	principalB := agentservice.Principal{TenantID: "tenant-a", UserID: "user-b"}
	storeA := deliveryStateForChannelSender(svc, principalA)
	storeB := deliveryStateForChannelSender(svc, principalB)
	if storeA == nil || storeB == nil || storeA == storeB {
		t.Fatalf("channel sender stores must be distinct and non-nil: A=%p B=%p", storeA, storeB)
	}

	storeA.RememberPeer(scheduler.DeliveryChannelTelegram, "10001")
	if got := storeA.ResolveSelfPeer(scheduler.DeliveryChannelTelegram, "self"); got != "10001" {
		t.Fatalf("principal A self peer = %q, want 10001", got)
	}
	if got := storeB.ResolveSelfPeer(scheduler.DeliveryChannelTelegram, "self"); got == "10001" {
		t.Fatalf("principal B resolved principal A peer: %q", got)
	}

	rootA := filepath.Clean(svc.UserDataRoot(principalA))
	rootB := filepath.Clean(svc.UserDataRoot(principalB))
	if rootA == rootB {
		t.Fatalf("principal data roots must differ: %q", rootA)
	}
}

func TestSendWeixinRequiresProactiveHook(t *testing.T) {
	setSrvWeixinProactiveSender(nil)
	_, err := sendWeixinTarget(context.Background(), nil, scheduler.DeliveryTarget{
		Kind:   scheduler.DeliveryKindUser,
		UserID: "self",
	}, "hello")
	if err == nil || !strings.Contains(err.Error(), "未接线") {
		t.Fatalf("want hook missing err, got %v", err)
	}

	setSrvWeixinProactiveSender(func(text string) (string, error) {
		if text != "hello" {
			t.Fatalf("text=%q", text)
		}
		return "wx-user-1", nil
	})
	t.Cleanup(func() { setSrvWeixinProactiveSender(nil) })

	peer, err := sendWeixinTarget(context.Background(), nil, scheduler.DeliveryTarget{
		Kind:   scheduler.DeliveryKindUser,
		UserID: "self",
	}, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if peer != "wx-user-1" {
		t.Fatalf("peer=%q", peer)
	}
}

func TestDeliverSrvIMFileToWeixinUsesExactPeerAndNotLastActive(t *testing.T) {
	setSrvWeixinExactFileSender(nil)
	err := deliverSrvIMFileToTarget(context.Background(), nil, agentservice.Principal{TenantID: "t", UserID: "u"}, scheduler.DeliveryChannelWeixin, scheduler.DeliveryTarget{Kind: scheduler.DeliveryKindUser, UserID: "wx-1"}, []byte("xlsx"), "sheet.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "")
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("unwired weixin file send must stay unavailable, err=%v", err)
	}

	sentPeer := ""
	setSrvWeixinExactFileSender(func(_ context.Context, peer string, data []byte, fileName, mimeType string) error {
		if peer != "wx-1" || string(data) != "xlsx" || fileName != "sheet.xlsx" {
			t.Fatalf("peer=%q name=%q data=%q mime=%q", peer, fileName, data, mimeType)
		}
		sentPeer = peer
		return nil
	})
	t.Cleanup(func() { setSrvWeixinExactFileSender(nil) })

	if err := deliverSrvIMFileToTarget(context.Background(), nil, agentservice.Principal{TenantID: "t", UserID: "u"}, scheduler.DeliveryChannelWeixin, scheduler.DeliveryTarget{Kind: scheduler.DeliveryKindUser, UserID: "wx-1"}, []byte("xlsx"), "sheet.xlsx", "", ""); err != nil {
		t.Fatal(err)
	}
	if sentPeer != "wx-1" {
		t.Fatalf("sentPeer=%q", sentPeer)
	}
	if err := deliverSrvIMFileToTarget(context.Background(), nil, agentservice.Principal{TenantID: "t", UserID: "u"}, scheduler.DeliveryChannelWeixin, scheduler.DeliveryTarget{Kind: scheduler.DeliveryKindGroup, GroupID: "ops"}, []byte("xlsx"), "sheet.xlsx", "", ""); err == nil {
		t.Fatal("weixin group file send must fail closed")
	}
	if err := deliverSrvIMFileToTarget(context.Background(), nil, agentservice.Principal{TenantID: "t", UserID: "u"}, scheduler.DeliveryChannelWeixin, scheduler.DeliveryTarget{Kind: scheduler.DeliveryKindUser, UserID: "self"}, []byte("xlsx"), "sheet.xlsx", "", ""); err == nil {
		t.Fatal("weixin self file send must fail closed")
	}
}

func TestSendWeixinRejectsNonSelf(t *testing.T) {
	_, err := sendWeixinTarget(context.Background(), nil, scheduler.DeliveryTarget{
		Kind:   scheduler.DeliveryKindUser,
		UserID: "other-user",
	}, "x")
	if err == nil || !strings.Contains(err.Error(), "self") {
		t.Fatalf("want self-only err, got %v", err)
	}
}

func TestSrvIMOutgoingMediaKindUsesImageMIME(t *testing.T) {
	if got := srvIMOutgoingMediaKind("image/png"); got != "image" {
		t.Fatalf("png=%q", got)
	}
	if got := srvIMOutgoingMediaKind("audio/wav"); got != "voice" {
		t.Fatalf("wav=%q", got)
	}
	if got := srvIMOutgoingMediaKind("application/pdf"); got != "file" {
		t.Fatalf("pdf=%q", got)
	}
}

func TestShouldDeliverAbortPartial_SrvPath(t *testing.T) {
	// Regression guard: annotated timeout must still deliver partial body.
	d := &scheduler.TaskDelivery{
		Enabled: true,
		Channel: scheduler.DeliveryChannelTelegram,
		Targets: []scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindUser, UserID: "1"}},
		On:      scheduler.DeliveryOnSuccess,
	}
	runErr := scheduler.AnnotateRunErrWithContext(func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}(), nil)
	if !d.ShouldDeliver(runErr, "partial report") {
		t.Fatal("expected deliver on abort+partial")
	}
}
