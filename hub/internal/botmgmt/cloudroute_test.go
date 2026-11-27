package botmgmt

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// recordingCloudRouter is a fake CloudRouter that captures each call so a
// test can assert the classify -> ensure -> send wiring without a real
// cloudagent.Service (botmgmt cannot import cloudagent).
type recordingCloudRouter struct {
	findKind   string
	createKind string
	createName string
	resumedID  string
	sentInst   string
	sentText   string
	sentKey    string

	// findResult steers FindReusable: the instance it returns and whether it
	// was found. Status "stopped" exercises the Resume path.
	findResult CloudInstance
	findFound  bool
}

func (r *recordingCloudRouter) FindReusable(_ context.Context, _, _, _, kind string) (CloudInstance, bool, error) {
	r.findKind = kind
	return r.findResult, r.findFound, nil
}

func (r *recordingCloudRouter) Create(_ context.Context, _, _, _, name, kind string) (CloudInstance, error) {
	r.createKind = kind
	r.createName = name
	return CloudInstance{ID: "cl_created", Kind: kind, Status: "idle"}, nil
}

func (r *recordingCloudRouter) Resume(_ context.Context, _, _, instanceID string) (CloudInstance, error) {
	r.resumedID = instanceID
	return CloudInstance{ID: instanceID, Kind: r.findResult.Kind, Status: "idle"}, nil
}

func (r *recordingCloudRouter) Delete(_ context.Context, _, _, instanceID string) error {
	return nil
}

func (r *recordingCloudRouter) SendMessage(_ context.Context, _, _, instanceID, clientMessageID, text string) error {
	r.sentInst = instanceID
	r.sentKey = clientMessageID
	r.sentText = text
	return nil
}

// seedCloudBot stores a record with a single computer-less bot owned by
// alice, ready for AdmitDesktopMessageAct to route onto the cloud path.
func seedCloudBot(t *testing.T, settings *memSettings, bot Bot) {
	t.Helper()
	raw, err := json.Marshal(record{
		Bots:   []Bot{bot},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
}

// A computer-less bot with a wired router classifies a coding ask, creates a
// coding instance (none reusable yet), and posts the turn to it.
func TestCloudRouteCreatesCodingInstanceAndSends(t *testing.T) {
	settings := &memSettings{}
	seedCloudBot(t, settings, Bot{
		ID: "bot_c", Name: "coder", OwnerUserID: "alice",
		DesktopMode: desktop.BotDesktopModeNone,
	})
	svc := NewService(settings)
	router := &recordingCloudRouter{}
	svc.CloudRouter = router

	admission, err := svc.AdmitDesktopMessageAct(
		context.Background(), "tenant-a", "alice", "bot_c",
		"改一下仓库的登录逻辑并提交 PR", "", "cm-code-1", "", "",
	)
	if err != nil {
		t.Fatalf("cloud admit failed: %v", err)
	}
	if !admission.Accepted {
		t.Fatalf("cloud turn not admitted: %+v", admission)
	}
	if router.createKind != KindCoding {
		t.Fatalf("coding ask should provision a coding instance, got kind=%q", router.createKind)
	}
	if router.sentInst != "cl_created" {
		t.Fatalf("turn went to instance %q, want created cl_created", router.sentInst)
	}
	if router.sentText == "" || router.sentKey != "cm-code-1" {
		t.Fatalf("SendMessage got text=%q key=%q, want the content + cm-code-1", router.sentText, router.sentKey)
	}
}

// A session ask reuses the live session instance the bot already owns instead
// of creating a second one.
func TestCloudRouteReusesLiveSessionInstance(t *testing.T) {
	settings := &memSettings{}
	seedCloudBot(t, settings, Bot{
		ID: "bot_s", Name: "chat", OwnerUserID: "alice",
		DesktopMode: desktop.BotDesktopModeNone,
	})
	svc := NewService(settings)
	router := &recordingCloudRouter{
		findResult: CloudInstance{ID: "cl_live", Kind: KindSession, Status: "idle"},
		findFound:  true,
	}
	svc.CloudRouter = router

	if _, err := svc.AdmitDesktopMessageAct(
		context.Background(), "tenant-a", "alice", "bot_s",
		"帮我发条小红书帖子", "", "cm-sess-1", "", "",
	); err != nil {
		t.Fatalf("cloud admit failed: %v", err)
	}
	if router.findKind != KindSession {
		t.Fatalf("session ask should look up a session instance, got %q", router.findKind)
	}
	if router.createKind != "" {
		t.Fatalf("live session instance was reusable but Create still ran (kind=%q)", router.createKind)
	}
	if router.sentInst != "cl_live" {
		t.Fatalf("turn went to %q, want reused cl_live", router.sentInst)
	}
}

// A stopped reusable instance is resumed, not replaced.
func TestCloudRouteResumesStoppedInstance(t *testing.T) {
	settings := &memSettings{}
	seedCloudBot(t, settings, Bot{
		ID: "bot_r", Name: "chat", OwnerUserID: "alice",
		DesktopMode: desktop.BotDesktopModeNone,
	})
	svc := NewService(settings)
	router := &recordingCloudRouter{
		findResult: CloudInstance{ID: "cl_stopped", Kind: KindSession, Status: "stopped"},
		findFound:  true,
	}
	svc.CloudRouter = router

	if _, err := svc.AdmitDesktopMessageAct(
		context.Background(), "tenant-a", "alice", "bot_r",
		"查一下资料", "", "cm-res-1", "", "",
	); err != nil {
		t.Fatalf("cloud admit failed: %v", err)
	}
	if router.resumedID != "cl_stopped" {
		t.Fatalf("stopped instance should be resumed, resumed=%q", router.resumedID)
	}
	if router.createKind != "" {
		t.Fatalf("stopped instance was resumable but Create still ran (kind=%q)", router.createKind)
	}
	if router.sentInst != "cl_stopped" {
		t.Fatalf("turn went to %q, want the resumed cl_stopped", router.sentInst)
	}
}
