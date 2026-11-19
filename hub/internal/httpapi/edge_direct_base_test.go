package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
)

func TestNodeReplacesItsIntranetAddress(t *testing.T) {
	t.Setenv("HUB_EDGE_NODES", "on")
	reg := edgenode.NewRegistry(&edgeMemSettings{})
	code, err := reg.IssueCode(context.Background(), "tenant-a", "lab")
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := reg.Enroll(context.Background(), code.Code, "desk-key", "", "v1", true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	post := func(body, secret, nodeID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/edge/direct-base", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("X-Node-Id", nodeID)
		rec := httptest.NewRecorder()
		PostEdgeDirectBaseHandler(reg).ServeHTTP(rec, req)
		return rec
	}
	if rec := post(`{"direct_base":"http://10.2.0.9:18081/"}`, enrolled.NodeSecret, enrolled.NodeID); rec.Code != http.StatusNoContent {
		t.Fatalf("save %d %s", rec.Code, rec.Body.String())
	}
	node, err := reg.Lookup(context.Background(), enrolled.NodeID)
	if err != nil || node.DirectBase != "http://10.2.0.9:18081" {
		t.Fatalf("stored %q err=%v", node.DirectBase, err)
	}
	if rec := post(`{"direct_base":"http://desktopd.abc.node.invalid"}`, enrolled.NodeSecret, enrolled.NodeID); rec.Code != http.StatusBadRequest {
		t.Fatalf("virtual host %d", rec.Code)
	}
	node, _ = reg.Lookup(context.Background(), enrolled.NodeID)
	if node.DirectBase != "http://10.2.0.9:18081" {
		t.Fatalf("rejected address replaced the stored one: %q", node.DirectBase)
	}
	if rec := post(`{"direct_base":"http://10.9.9.9:18081"}`, "not-the-secret", enrolled.NodeID); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong secret %d", rec.Code)
	}
	if rec := post(`{"direct_base":""}`, enrolled.NodeSecret, enrolled.NodeID); rec.Code != http.StatusNoContent {
		t.Fatalf("clear %d %s", rec.Code, rec.Body.String())
	}
	node, _ = reg.Lookup(context.Background(), enrolled.NodeID)
	if node.DirectBase != "" {
		t.Fatalf("clear left %q", node.DirectBase)
	}
}
