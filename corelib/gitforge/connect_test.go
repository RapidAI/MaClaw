package gitforge

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthorizeScopesAndLinkStayOnTheForge(t *testing.T) {
	scopes, err := AuthorizeScopes(GitHub, "")
	if err != nil || !strings.Contains(scopes, "workflow") || !strings.Contains(scopes, "read:org") {
		t.Fatalf("default scopes: %s %v", scopes, err)
	}
	if _, err := AuthorizeScopes(GitHub, "repo admin:org"); err == nil {
		t.Fatal("admin scope was accepted")
	}
	link, err := AuthorizeLink(GitHub, "", "ABCD-EFGH")
	if err != nil || link != "https://github.com/login/device?user_code=ABCD-EFGH" {
		t.Fatalf("link: %s %v", link, err)
	}
	if _, err := AuthorizeLink(GitHub, "", "bad code"); err == nil {
		t.Fatal("user code with a space was accepted")
	}
	gl, err := AuthorizeLink(GitLab, "https://gitlab.example.com", "ABCD-EFGH")
	if err != nil || gl != "https://gitlab.example.com/oauth/authorize_device?user_code=ABCD-EFGH" {
		t.Fatalf("gitlab link: %s %v", gl, err)
	}
}

func TestGitLabDevicePollDoesNotReturnTheTokenInAnError(t *testing.T) {
	const token = "glpat-testtokenvalue123456"
	var posts int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), token) {
			t.Fatal("request echoed the token")
		}
		posts++
		switch r.URL.Path {
		case "/oauth/authorize_device":
			if !strings.Contains(string(body), "scope=read_user+api") && !strings.Contains(string(body), "scope=read_user%20api") && !strings.Contains(string(body), "scope=read_user") {
				t.Fatalf("scope form: %s", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"device_code":"device-secret","user_code":"ABCD-EFGH","interval":1}`))
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			if posts < 3 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"` + token + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	user, device, interval, err := StartGitLabDevice(context.Background(), srv.Client(), srv.URL, "client", "read_user api")
	if err != nil || user != "ABCD-EFGH" || device != "device-secret" || interval != 1 {
		t.Fatalf("start: %s %s %d %v", user, device, interval, err)
	}
	got, err := PollGitLabDevice(context.Background(), srv.Client(), srv.URL, "client", "", device, 1)
	if err != nil || got != token {
		t.Fatalf("poll: %q %v", got, err)
	}
}

func TestVisibleListsOrgsAndReposWithoutTheToken(t *testing.T) {
	const token = "ghp_testtokenvalue1234567890"
	client := &Client{Token: token, Bearer: true, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("PRIVATE-TOKEN") != "" {
			t.Fatalf("auth header bearer=%q private=%q", r.Header.Get("Authorization"), r.Header.Get("PRIVATE-TOKEN"))
		}
		switch r.URL.Path {
		case "/user":
			return jsonResponse(`{"login":"octo"}`), nil
		case "/user/orgs":
			return jsonResponse(`[{"login":"acme"}]`), nil
		case "/user/repos":
			return jsonResponse(`[{"full_name":"acme/app"},{"full_name":"octo/lib"}]`), nil
		default:
			t.Errorf("path %s", r.URL.Path)
			return jsonResponse(`[]`), nil
		}
	})}}
	repo, err := Parse("acme/app", GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.Visible(context.Background(), repo)
	if err != nil || !strings.Contains(text, "login=octo") || !strings.Contains(text, "acme") || !strings.Contains(text, "acme/app") || strings.Contains(text, token) {
		t.Fatalf("visible: %s %v", text, err)
	}
}
