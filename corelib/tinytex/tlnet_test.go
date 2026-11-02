package tinytex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConcreteTLNetDoesNotDoubleEncodeThePath(t *testing.T) {
	got, ok := concreteTLNet(
		"https://mirror.ctan.org/systems/texlive/tlnet",
		"https://mirror.example/CTAN%20files/systems/texlive/tlnet/",
	)
	if !ok {
		t.Fatal("repository was rejected")
	}
	if got != "https://mirror.example/CTAN%20files/systems/texlive/tlnet" {
		t.Fatalf("pinned = %s", got)
	}
}

func TestResolveTLNetPinsARedirect(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/systems/texlive/tlnet":
			w.WriteHeader(http.StatusNoContent)
		case "/redirect":
			if r.Method == http.MethodHead {
				http.Error(w, "no head", http.StatusMethodNotAllowed)
				return
			}
			http.Redirect(w, r, srv.URL+"/systems/texlive/tlnet", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client := srv.Client()
	client.CheckRedirect = tlnetCheckRedirect

	got := resolveTLNet(context.Background(), srv.URL+"/redirect", client)
	want := srv.URL + "/systems/texlive/tlnet"
	if got != want {
		t.Fatalf("pinned = %s, want %s", got, want)
	}
}

func TestResolveTLNetRefusesAnHTTPDowngrade(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(plain.Close)
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/systems/texlive/tlnet", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	client := srv.Client()
	client.CheckRedirect = tlnetCheckRedirect

	original := srv.URL + "/systems/texlive/tlnet"
	if got := resolveTLNet(context.Background(), original, client); got != original {
		t.Fatalf("downgrade pinned = %s", got)
	}
}

func TestResolveTLNetKeepsANonTLNetTarget(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/texlive/tlnet") {
			http.Redirect(w, r, srv.URL+"/not-a-repository", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	client := srv.Client()
	client.CheckRedirect = tlnetCheckRedirect

	original := srv.URL + "/systems/texlive/tlnet"
	if got := resolveTLNet(context.Background(), original, client); got != original {
		t.Fatalf("non-tlnet pinned = %s", got)
	}
}

func TestResolveTLNetKeepsTheOriginalWhenTheLookupFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	original := "https://mirror.ctan.org/systems/texlive/tlnet"
	if got := ResolveTLNet(ctx, original); got != original {
		t.Fatalf("failed lookup = %s", got)
	}
	if got := ResolveTLNet(context.Background(), "http://mirror.ctan.org/systems/texlive/tlnet"); got != "http://mirror.ctan.org/systems/texlive/tlnet" {
		t.Fatalf("plain http was resolved: %s", got)
	}
	if got := ResolveTLNet(context.Background(), "not a url"); got != "not a url" {
		t.Fatalf("non-url = %q", got)
	}
}
