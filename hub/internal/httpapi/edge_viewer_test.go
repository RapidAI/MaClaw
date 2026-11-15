package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/edgelink"
	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestDirectViewHidesTheGateTokenAndConfirmStopsWhenThePictureIsGone(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	reg := edgenode.NewRegistry(&edgeMemSettings{})
	code, err := reg.IssueCode(context.Background(), store.DefaultTenantID, "lab")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-key", "", "v1", true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetDirectBase(context.Background(), enrolled.NodeID, "http://10.1.0.8:18081"); err != nil {
		t.Fatal(err)
	}
	prev := edgeRegistry
	edgeRegistry = reg
	prevOpen := viewerStillOpen
	t.Cleanup(func() {
		edgeRegistry = prev
		viewerStillOpen = prevOpen
		viewerGrants.mu.Lock()
		viewerGrants.items = nil
		viewerGrants.mu.Unlock()
	})
	key, err := desktop.UserKey("tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	raw := "http://desktop:gate-secret@desktopd." + enrolled.NodeID + ".node.invalid/v1/desktops/tunnel/" + key + "/vnc/vnc.html"
	direct := edgeDirectURL("tenant-a", "alice", raw, false)
	if !strings.HasPrefix(direct, "http://10.1.0.8:18081/v1/desktops/viewer/") || strings.Contains(direct, "gate-secret") || !strings.Contains(direct, "view_only=1") {
		t.Fatalf("direct %s", direct)
	}
	parsed, err := url.Parse(direct)
	if err != nil {
		t.Fatal(err)
	}
	socket := parsed.Query().Get("path")
	if !strings.HasPrefix(socket, "v1/desktops/viewer/") || !strings.HasSuffix(socket, "/websockify") {
		t.Fatalf("socket path %q", socket)
	}
	again := edgeDirectURL("tenant-a", "alice", raw, false)
	if again != direct {
		t.Fatal("a second look minted another ticket")
	}
	rememberViewerGrant("rotated-grant", viewerGrant{
		Page: direct, NodeID: enrolled.NodeID, TenantID: store.NormalizeTenantID("tenant-a"), UserID: "alice",
		Control: false, Expires: time.Now().Add(time.Hour), Key: "previous-desk-key",
	})
	if rotated := edgeDirectURL("tenant-a", "alice", raw, false); rotated != direct {
		t.Fatalf("rotated key changed the live page: %s", rotated)
	}
	if _, ok := lookupViewerGrant("rotated-grant"); ok {
		t.Fatal("a ticket signed with the previous key was kept")
	}
	if edgeDirectURL("tenant-a", "alice", "http://dockerd.example:6080/vnc.html", false) != "" {
		t.Fatal("a public desktop was given a direct url")
	}

	viewerStillOpen = func(string, string) bool { return true }
	path := strings.TrimPrefix(strings.Split(direct, "?")[0], "http://10.1.0.8:18081/v1/desktops/viewer/")
	ticket, _, _ := strings.Cut(path, "/")
	id := ticketID(t, ticket)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/edge/viewer/"+id, nil)
	req.SetPathValue("id", id)
	req.Header.Set("Authorization", "Bearer "+enrolled.NodeSecret)
	req.Header.Set("X-Node-Id", enrolled.NodeID)
	rec := httptest.NewRecorder()
	EdgeViewerConfirmHandler(reg).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm %d %s", rec.Code, rec.Body.String())
	}
	viewerStillOpen = func(string, string) bool { return false }
	rec = httptest.NewRecorder()
	EdgeViewerConfirmHandler(reg).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("withdrawn confirm %d", rec.Code)
	}
}

func ticketID(t *testing.T, ticket string) string {
	t.Helper()
	claims, err := edgelink.VerifyViewer("desk-key", ticket, time.Now())
	if err != nil || claims.ID == "" {
		t.Fatalf("ticket id: %v", err)
	}
	return claims.ID
}
