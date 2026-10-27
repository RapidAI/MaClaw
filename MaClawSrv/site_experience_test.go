package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func withSiteExpRoot(t *testing.T) {
	t.Helper()
	old := siteExpRoot
	siteExpRoot = t.TempDir()
	t.Cleanup(func() { siteExpRoot = old })
}

func liveRep(evidence string, method map[string]string) *browser.PublishReport {
	return &browser.PublishReport{State: browser.PublishLive, Evidence: evidence, Method: method}
}

func TestSiteExperienceSeedsAreV0(t *testing.T) {
	withSiteExpRoot(t)
	for _, site := range []string{"zhihu", "xiaohongshu"} {
		e := siteExpLoad(site)
		if e == nil || e.Version != 0 || e.Source != "seed" || e.Status != siteExpStatusActive || e.Profile.ID != site || len(e.Profile.Playbook) != 0 {
			t.Fatalf("%s seed: %+v", site, e)
		}
	}
	if siteExpKey("https://zhuanlan.zhihu.com/write") != "zhihu" || siteExpKey("https://mp.blog.example.org/x") != "example.org" {
		t.Fatal("site keys")
	}
	if siteExpLoad("example.org") != nil {
		t.Fatal("a site without a seed has no record")
	}
}

func TestSiteExperienceLearnVerifyRetireRollback(t *testing.T) {
	withSiteExpRoot(t)
	scope := agentruntime.Scope{TenantID: "t", UserID: "u"}
	site := "example.org"
	spec := browser.PublishSpec{EditorURL: "https://www.example.org/write", VerifyAPI: "https://api.example.org/list", Title: "x"}
	m := map[string]string{"upload": "file_input"}
	// A toast or an unconfirmed submit never creates experience.
	siteExpRecord(scope, site, false, spec, &browser.PublishReport{State: browser.PublishSubmitted}, nil)
	siteExpRecord(scope, site, false, spec, liveRep("", m), nil)
	if siteExpLoad(site) != nil {
		t.Fatal("unverified run learned")
	}
	siteExpRecord(scope, site, false, spec, liveRep("response", m), nil)
	e := siteExpLoad(site)
	if e == nil || e.Version != 1 || e.Source != "learned" || e.VerifiedRuns != 1 || e.Profile.EditorURL != spec.EditorURL || e.Method["evidence"] != "response" {
		t.Fatalf("learned: %+v", e)
	}
	// Same method again: verified_runs grows, no new version.
	siteExpRecord(scope, site, true, spec, liveRep("response", m), nil)
	if e = siteExpLoad(site); e.Version != 1 || e.VerifiedRuns != 2 {
		t.Fatalf("reverified: %+v", e)
	}
	var applied browser.PublishSpec
	if !siteExpApply(e, &applied) || applied.EditorURL != spec.EditorURL || applied.VerifyAPI != spec.VerifyAPI {
		t.Fatalf("apply: %+v", applied)
	}
	// A different method that works is a new version.
	siteExpRecord(scope, site, true, spec, liveRep("redirect", map[string]string{"upload": "chooser"}), nil)
	if e = siteExpLoad(site); e.Version != 2 || e.Method["upload"] != "chooser" {
		t.Fatalf("new method: %+v", e)
	}
	// Account-caused failures do not count against the method.
	siteExpRecord(scope, site, true, spec, &browser.PublishReport{State: browser.PublishNotSubmitted, Notice: "转正答题后才能发布"}, errors.New("blocked"))
	if e = siteExpLoad(site); e.FailStreak != 0 {
		t.Fatalf("account block counted: %+v", e)
	}
	if b := siteExpBlocks(scope, site); len(b) != 1 || b[0].Kind != "notice" {
		t.Fatalf("blocks %+v", b)
	}
	if b := siteExpBlocks(agentruntime.Scope{TenantID: "t", UserID: "other"}, site); len(b) != 0 {
		t.Fatal("a block leaked to another account")
	}
	// Two method failures: suspect, then retired.
	fail := &browser.PublishReport{State: browser.PublishNotSubmitted}
	siteExpRecord(scope, site, true, spec, fail, errors.New("upload made no request"))
	if e = siteExpLoad(site); e.Status != siteExpStatusSuspct || e.FailStreak != 1 {
		t.Fatalf("suspect: %+v", e)
	}
	siteExpRecord(scope, site, true, spec, fail, errors.New("upload made no request"))
	if e = siteExpLoad(site); e.Status != siteExpStatusRetird {
		t.Fatalf("retired: %+v", e)
	}
	if siteExpApply(e, &browser.PublishSpec{}) {
		t.Fatal("a retired record was applied")
	}
	// Roll back to v1.
	r, err := siteExpRollback(site, 1)
	if err != nil || r.Status != siteExpStatusActive || r.Method["upload"] != "file_input" || r.Version <= 2 {
		t.Fatalf("rollback %+v %v", r, err)
	}
	h := siteExpHistory(site)
	if len(h) < 4 || h[len(h)-1].Source != "rollback" {
		t.Fatalf("history %d", len(h))
	}
	var out bytes.Buffer
	if siteExpMain([]string{"history", site}, &out) != 0 || !strings.Contains(out.String(), "rollback") {
		t.Fatal(out.String())
	}
	if siteExpMain([]string{"rollback", "../etc", "1"}, &out) == 0 {
		t.Fatal("bad site name accepted")
	}
}

func TestSiteExperienceSeedRetireFallsBackAndBlocksExpire(t *testing.T) {
	withSiteExpRoot(t)
	scope := agentruntime.Scope{TenantID: "t", UserID: "u"}
	spec := browser.PublishSpec{EditorURL: "https://zhuanlan.zhihu.com/write"}
	siteExpRecord(scope, "zhihu", true, spec, liveRep("response", map[string]string{"body": "paste"}), nil)
	if e := siteExpLoad("zhihu"); e.Version != 1 || e.Profile.SuccessURL == "" {
		t.Fatalf("learned over the seed keeps seed fields: %+v", e)
	}
	fail := &browser.PublishReport{State: browser.PublishNotSubmitted}
	siteExpRecord(scope, "zhihu", true, spec, fail, errors.New("x"))
	siteExpRecord(scope, "zhihu", true, spec, fail, errors.New("x"))
	if e := siteExpLoad("zhihu"); e.Source != "seed" || e.Status != siteExpStatusActive || e.Version != 2 {
		t.Fatalf("retired learned record falls back to the seed: %+v", e)
	}
	// Expired blocks are dropped.
	siteExpAddBlock(scope, "zhihu", "login", "sign in")
	path := siteExpBlocksPath(scope)
	raw, _ := os.ReadFile(path)
	old := strings.Replace(string(raw), `"expire":`, `"expire":1,"x":`, 1)
	_ = os.WriteFile(path, []byte(old), 0o600)
	if b := siteExpBlocks(scope, "zhihu"); len(b) != 0 {
		t.Fatalf("expired block kept: %+v", b)
	}
	_ = time.Now
}
