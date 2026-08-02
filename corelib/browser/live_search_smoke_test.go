//go:build livesmoke

package browser

// Live smoke for SearchViaBrowser against the real managed browser.
// Run: go test ./corelib/browser/ -tags livesmoke -run TestLiveSearchViaBrowser -v

import (
	"context"
	"testing"
	"time"
)

func TestLiveSearchViaBrowser(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	hits, err := SearchViaBrowser(ctx, "bing_cn", "golang programming language", 3)
	if err != nil {
		t.Logf("SearchViaBrowser FAILED (browser/network unavailable): %v", err)
		return
	}
	for i, h := range hits {
		t.Logf("hit %d: %s — %s", i+1, h.Title, h.URL)
	}
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
}
