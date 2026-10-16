package slack

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLoginExchangesCodeOnLoopback(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth.v2.access" {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()
		if r.Form.Get("code") != "auth-code" || r.Form.Get("client_id") != "cid" || r.Form.Get("client_secret") != "csecret" {
			t.Errorf("form = %#v", r.Form)
		}
		if !strings.HasSuffix(r.Form.Get("redirect_uri"), "/slack/callback") {
			t.Errorf("redirect = %q", r.Form.Get("redirect_uri"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"access_token":"xoxb-test","bot_user_id":"UBOT","team":{"id":"T1","name":"Acme"},"authed_user":{"id":"U1"}}`)
	}))
	defer tokenSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := Login(ctx, "cid", "csecret", func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		if u.Query().Get("client_id") != "cid" {
			t.Fatalf("client_id = %q", u.Query().Get("client_id"))
		}
		if !strings.Contains(u.Query().Get("scope"), "chat:write") || !strings.Contains(u.Query().Get("scope"), "im:history") {
			t.Fatalf("scope = %q", u.Query().Get("scope"))
		}
		callback, err := url.Parse(u.Query().Get("redirect_uri"))
		if err != nil {
			return err
		}
		q := callback.Query()
		q.Set("code", "auth-code")
		q.Set("state", u.Query().Get("state"))
		callback.RawQuery = q.Encode()
		resp, err := http.Get(callback.String())
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "已登录 Slack") {
			t.Fatalf("callback page = %s", body)
		}
		return nil
	}, LoginOptions{ListenAddr: "127.0.0.1:0", TokenURL: tokenSrv.URL + "/oauth.v2.access"})
	if err != nil {
		t.Fatal(err)
	}
	if session.BotToken != "xoxb-test" || session.TeamName != "Acme" || session.BotUserID != "UBOT" || session.AuthedUserID != "U1" || session.TeamID != "T1" {
		t.Fatalf("session = %#v", session)
	}
}

func TestLoginRejectsMissingAppCredentials(t *testing.T) {
	_, err := Login(context.Background(), "", "secret", func(string) error { return nil }, LoginOptions{})
	if err == nil || !strings.Contains(err.Error(), "Client ID") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginRejectsUserToken(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"access_token":"xoxp-user","token_type":"user","bot_user_id":"","team":{"id":"T1","name":"Acme"},"authed_user":{"id":"U1"}}`)
	}))
	defer tokenSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Login(ctx, "cid", "csecret", func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		callback, err := url.Parse(u.Query().Get("redirect_uri"))
		if err != nil {
			return err
		}
		q := callback.Query()
		q.Set("code", "auth-code")
		q.Set("state", u.Query().Get("state"))
		callback.RawQuery = q.Encode()
		resp, err := http.Get(callback.String())
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}, LoginOptions{ListenAddr: "127.0.0.1:0", TokenURL: tokenSrv.URL})
	if err == nil || !strings.Contains(err.Error(), "机器人令牌") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginCancelledWhenSlackDenies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Login(ctx, "cid", "csecret", func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		callback, err := url.Parse(u.Query().Get("redirect_uri"))
		if err != nil {
			return err
		}
		q := callback.Query()
		q.Set("error", "access_denied")
		q.Set("state", u.Query().Get("state"))
		callback.RawQuery = q.Encode()
		resp, err := http.Get(callback.String())
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}, LoginOptions{ListenAddr: "127.0.0.1:0", TokenURL: "http://127.0.0.1:1/unused"})
	if err == nil || !strings.Contains(err.Error(), "登录已取消") {
		t.Fatalf("err = %v", err)
	}
}
