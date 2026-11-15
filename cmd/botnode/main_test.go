package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
)

func TestEnrollStoresTheSecrets(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/edge/enroll" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"node_id":"n1","node_secret":"sec","hub_token":"hub"}`))
	}))
	defer srv.Close()
	dir := t.TempDir()
	cfg := nodeConfig{
		HubURL: srv.URL, EnrollCode: "CODE", StatePath: dir + "/botnode.json",
		Desktopd: "127.0.0.1:18081", DesktopdKey: "desk-key", Srv: "127.0.0.1:18080", SrvAdmin: "admin-secret",
	}
	st, err := enroll(context.Background(), srv.Client(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if st.NodeSecret != "sec" || st.HubToken != "hub" {
		t.Fatalf("%+v", st)
	}
	if got["code"] != "CODE" || got["desktopd_key"] != "desk-key" || got["has_desktopd"] != true || got["has_srv"] != true {
		t.Fatalf("%v", got)
	}
	if err := edgelink.SaveCredentials(cfg.StatePath, st); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfg.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"node_secret": "sec"`) {
		t.Fatal(string(raw))
	}
	loaded, err := edgelink.LoadCredentials(cfg.StatePath)
	if err != nil || loaded.NodeID != "n1" || loaded.HubURL != srv.URL {
		t.Fatalf("%+v %v", loaded, err)
	}
}

func TestLinkForwardsOnlyToTheNamedPort(t *testing.T) {
	hit := make(chan struct{}, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hit" {
			hit <- struct{}{}
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Node-Id") != "n1" || r.Header.Get("Authorization") != "Bearer sec" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		sess, err := yamux.Client(edgelink.NewWSConn(ws), edgelink.YamuxConfig())
		if err != nil {
			return
		}
		defer sess.Close()
		stream, err := sess.OpenStream()
		if err != nil {
			return
		}
		defer stream.Close()
		if err := edgelink.WriteTarget(stream, edgelink.TargetDesktopd); err != nil {
			return
		}
		_, _ = io.WriteString(stream, "GET /hit HTTP/1.1\r\nHost: desktopd\r\n\r\n")
		_, _ = io.Copy(io.Discard, stream)
	}))
	defer hub.Close()

	link, err := edgelink.LinkURL(hub.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- edgelink.ServeLink(ctx, link, "n1", "sec", map[string]string{edgelink.TargetDesktopd: ln.Addr().String()}, nil)
	}()
	select {
	case <-hit:
		cancel()
	case err := <-done:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("local desktopd was not reached")
	}
}
