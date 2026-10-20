//go:build windows

package windows

import (
	"net/url"
	"strings"
	"testing"
)

func TestRequestIsAppOrigin(t *testing.T) {
	start, err := url.Parse("http://wails.localhost/")
	if err != nil {
		t.Fatal(err)
	}
	dev, err := url.Parse("http://wails.localhost:34115/")
	if err != nil {
		t.Fatal(err)
	}
	if !requestIsAppOrigin(start, "http://wails.localhost/index.html") {
		t.Fatal("app page was treated as third-party")
	}
	if !requestIsAppOrigin(start, "HTTP://WAILS.LOCALHOST/index.html") {
		t.Fatal("app origin comparison was case-sensitive")
	}
	if !requestIsAppOrigin(dev, "http://wails.localhost:34115/src/main.tsx") {
		t.Fatal("dev server was treated as third-party")
	}
	for _, raw := range []string{
		"https://www.youtube.com/embed/ILGxXoQjPEU",
		"https://www.youtube.com/embed/ILGxXoQjPEU?origin=http://wails.localhost/",
		"https://www.youtube-nocookie.com/embed/ILGxXoQjPEU",
		"https://i.ytimg.com/vi/ILGxXoQjPEU/hqdefault.jpg",
		"http://wails.localhost:34115/index.html",
		"https://hub.example/api",
		"",
	} {
		if requestIsAppOrigin(start, raw) {
			t.Fatalf("%s was treated as the app origin", raw)
		}
	}
	if requestIsAppOrigin(nil, "http://wails.localhost/") {
		t.Fatal("nil start matched")
	}
}

func TestAppOriginRequestFilter(t *testing.T) {
	start, err := url.Parse("http://wails.localhost/")
	if err != nil {
		t.Fatal(err)
	}
	dev, err := url.Parse("http://wails.localhost:34115/")
	if err != nil {
		t.Fatal(err)
	}
	upper, err := url.Parse("HTTP://Wails.LocalHost/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if got := appOriginRequestFilter(start); got != "http://wails.localhost/*" {
		t.Fatalf("app filter %q", got)
	}
	if got := appOriginRequestFilter(dev); got != "http://wails.localhost:34115/*" {
		t.Fatalf("dev filter %q", got)
	}
	if got := appOriginRequestFilter(upper); got != "http://wails.localhost/*" {
		t.Fatalf("case filter %q", got)
	}
	if got := appOriginRequestFilter(nil); got != "" {
		t.Fatalf("nil filter %q", got)
	}
	embed := "https://www.youtube.com/embed/ILGxXoQjPEU"
	if strings.Contains(embed, strings.TrimSuffix(appOriginRequestFilter(start), "*")) {
		t.Fatal("app filter is a prefix of the YouTube embed")
	}
}
