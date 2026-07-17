package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestNewChannelSenderUnsupported(t *testing.T) {
	_, err := newChannelSender(context.Background(), nil, agentservice.Principal{}, "discord")
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("want unsupported channel err, got %v", err)
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

func TestSendWeixinRejectsNonSelf(t *testing.T) {
	_, err := sendWeixinTarget(context.Background(), nil, scheduler.DeliveryTarget{
		Kind:   scheduler.DeliveryKindUser,
		UserID: "other-user",
	}, "x")
	if err == nil || !strings.Contains(err.Error(), "self") {
		t.Fatalf("want self-only err, got %v", err)
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
