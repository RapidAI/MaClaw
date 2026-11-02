package guiapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestExpertMarketReadsTheOfficialHubThatHoldsTheLibrary(t *testing.T) {
	var emptyList, fullList int
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Errorf("probe to empty hub omitted the session")
		}
		switch r.URL.Path {
		case "/api/v1/expert-market/account":
			_, _ = w.Write([]byte(`{"credits":0,"uploads":[],"purchases":[]}`))
		case "/api/v1/expert-market/experts":
			if r.URL.Query().Get("page_size") == "30" {
				emptyList++
			}
			_, _ = w.Write([]byte(`{"experts":[],"total":0,"page":1,"page_size":1}`))
		default:
			t.Errorf("unexpected empty-hub path %s", r.URL.Path)
		}
	}))
	defer empty.Close()
	full := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/expert-market/account":
			_, _ = w.Write([]byte(`{"credits":0,"uploads":[{"id":"expert_listing-1","name":"长篇连载写手","status":"listed"}],"purchases":[]}`))
		case "/api/v1/expert-market/experts":
			if r.URL.Query().Get("page_size") == "30" {
				fullList++
			}
			_, _ = w.Write([]byte(`{"experts":[{"id":"expert_listing-1","name":"长篇连载写手"}],"total":1,"page":1,"page_size":30}`))
		default:
			t.Errorf("unexpected full-hub path %s", r.URL.Path)
		}
	}))
	defer full.Close()

	orig := append([]string(nil), remote.DefaultRemoteHubCenterURLs...)
	remote.DefaultRemoteHubCenterURLs = []string{empty.URL, full.URL}
	t.Cleanup(func() { remote.DefaultRemoteHubCenterURLs = orig })

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:        empty.URL,
		SkillMarketSessionBaseURL: empty.URL,
		SkillMarketSessionToken:   "session-token",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := app.ListExpertMarketListings("", "published", 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	experts, _ := result["experts"].([]interface{})
	if len(experts) != 1 {
		t.Fatalf("experts=%v, want the listing stored on the other official hub", result)
	}
	if fullList != 1 || emptyList != 0 {
		t.Fatalf("catalogue page went to full=%d empty=%d", fullList, emptyList)
	}
	account, err := app.GetExpertMarketAccount()
	if err != nil {
		t.Fatal(err)
	}
	uploads, _ := account["uploads"].([]interface{})
	if len(uploads) != 1 {
		t.Fatalf("library=%v, want the submission stored on the other official hub", account)
	}
}

func TestExpertMarketStaysOnAPrivateHubCenter(t *testing.T) {
	var publicHits int
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicHits++
		_, _ = w.Write([]byte(`{"experts":[{"id":"expert_listing-1","name":"other"}],"total":1}`))
	}))
	defer public.Close()
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
			t.Errorf("private hub request had no session")
		}
		_, _ = w.Write([]byte(`{"experts":[],"total":0,"page":1,"page_size":30}`))
	}))
	defer private.Close()

	orig := append([]string(nil), remote.DefaultRemoteHubCenterURLs...)
	remote.DefaultRemoteHubCenterURLs = []string{public.URL}
	t.Cleanup(func() { remote.DefaultRemoteHubCenterURLs = orig })

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:        private.URL,
		SkillMarketSessionBaseURL: private.URL,
		SkillMarketSessionToken:   "session-token",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := app.ListExpertMarketListings("", "published", 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	experts, _ := result["experts"].([]interface{})
	if len(experts) != 0 {
		t.Fatalf("private hub catalogue = %v", result)
	}
	if publicHits != 0 {
		t.Fatalf("private enrollment sent the session to an official hub %d times", publicHits)
	}
}

func TestExpertMarketDoesNotPinAnEmptyHubWhenAPeerProbeFails(t *testing.T) {
	var emptyProbes, fullProbes int
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/expert-market/account" {
			emptyProbes++
		}
		_, _ = w.Write([]byte(`{"uploads":[],"purchases":[],"experts":[],"total":0}`))
	}))
	defer empty.Close()
	full := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/expert-market/account" {
			fullProbes++
		}
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer full.Close()

	orig := append([]string(nil), remote.DefaultRemoteHubCenterURLs...)
	remote.DefaultRemoteHubCenterURLs = []string{empty.URL, full.URL}
	t.Cleanup(func() { remote.DefaultRemoteHubCenterURLs = orig })

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:        empty.URL,
		SkillMarketSessionBaseURL: empty.URL,
		SkillMarketSessionToken:   "session-token",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ListExpertMarketListings("", "published", 1, 30); err != nil {
		t.Fatal(err)
	}
	app.expertMarketBaseMu.Lock()
	app.expertMarketBaseUntil = time.Now().Add(-time.Second)
	app.expertMarketBaseMu.Unlock()
	if _, err := app.ListExpertMarketListings("", "published", 1, 30); err != nil {
		t.Fatal(err)
	}
	if fullProbes < 2 || emptyProbes < 2 {
		t.Fatalf("failed peer was pinned after one probe: empty=%d full=%d", emptyProbes, fullProbes)
	}
}

func TestExpertMarketRetriesAPartialLibraryProbe(t *testing.T) {
	small := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"uploads":[{"id":"expert_listing-1"}],"purchases":[],"experts":[{"id":"expert_listing-1"}],"total":1}`))
	}))
	defer small.Close()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer down.Close()

	orig := append([]string(nil), remote.DefaultRemoteHubCenterURLs...)
	remote.DefaultRemoteHubCenterURLs = []string{small.URL, down.URL}
	t.Cleanup(func() { remote.DefaultRemoteHubCenterURLs = orig })

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:        small.URL,
		SkillMarketSessionBaseURL: small.URL,
		SkillMarketSessionToken:   "session-token",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ListExpertMarketListings("", "published", 1, 30); err != nil {
		t.Fatal(err)
	}
	app.expertMarketBaseMu.Lock()
	until := app.expertMarketBaseUntil
	app.expertMarketBaseMu.Unlock()
	if until.IsZero() {
		t.Fatal("partial probe was kept until the process exits")
	}
}

func TestExpertMarketReprobesAfterTheSessionChanges(t *testing.T) {
	var accounts int
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"uploads":[],"purchases":[],"experts":[],"total":0}`))
	}))
	defer empty.Close()
	full := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/expert-market/account" {
			accounts++
		}
		_, _ = w.Write([]byte(`{"uploads":[{"id":"expert_listing-1"}],"purchases":[],"experts":[{"id":"expert_listing-1"}],"total":1}`))
	}))
	defer full.Close()

	orig := append([]string(nil), remote.DefaultRemoteHubCenterURLs...)
	remote.DefaultRemoteHubCenterURLs = []string{empty.URL, full.URL}
	t.Cleanup(func() { remote.DefaultRemoteHubCenterURLs = orig })

	app := &App{testHomeDir: t.TempDir()}
	save := func(token string) {
		t.Helper()
		if err := app.SaveConfig(corelib.AppConfig{
			RemoteHubCenterURL:        empty.URL,
			SkillMarketSessionBaseURL: empty.URL,
			SkillMarketSessionToken:   token,
		}); err != nil {
			t.Fatal(err)
		}
	}
	save("session-a")
	if _, err := app.ListExpertMarketListings("", "published", 1, 30); err != nil {
		t.Fatal(err)
	}
	first := accounts
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SkillMarketSessionToken = "session-b"
	}); err != nil {
		t.Fatal(err)
	}
	result, err := app.ListExpertMarketListings("", "published", 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	experts, _ := result["experts"].([]interface{})
	if len(experts) != 1 {
		t.Fatalf("experts=%v", result)
	}
	if accounts <= first {
		t.Fatalf("new session reused the previous hub choice: account probes=%d", accounts)
	}
}
