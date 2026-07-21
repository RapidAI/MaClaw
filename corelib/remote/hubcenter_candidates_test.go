package remote

import (
	"reflect"
	"testing"
)

func TestRegisteredPublicHubCenterURLsSkipsLoopback(t *testing.T) {
	// Loopback preferred is local/dev — must not promote leftover public seeds.
	got := RegisteredPublicHubCenterURLs("http://127.0.0.1:9", []string{
		"http://localhost:9388",
		"https://hubs.maclaw.top/",
		"https://hubs2.maclaw.top",
		"https://custom-center.example/",
	})
	if len(got) != 0 {
		t.Fatalf("RegisteredPublicHubCenterURLs(loopback preferred) = %#v, want empty", got)
	}
}

func TestRegisteredPublicHubCenterURLsStripsNonPreferredOfficialDefaults(t *testing.T) {
	// Polluted config from older HA write-back: hubs2 must not stay in identity.
	got := RegisteredPublicHubCenterURLs("https://hubs.maclaw.top", []string{
		"http://127.0.0.1:1",
		"https://hubs.maclaw.top",
		"https://hubs2.maclaw.top",
		"https://hubs.mypapers.top",
		"https://corp-center.example",
	})
	want := []string{"https://hubs.maclaw.top", "https://corp-center.example"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RegisteredPublicHubCenterURLs(polluted) = %#v, want %#v", got, want)
	}
}

func TestEffectiveHubCenterSeedsDoesNotMergeDefaultsWhenRegistered(t *testing.T) {
	defaults := []string{
		"https://hubs.mypapers.top",
		"https://hubs.maclaw.top",
		"https://hubs2.maclaw.top",
	}
	got := EffectiveHubCenterSeeds(
		"https://hubs.maclaw.top",
		[]string{"http://127.0.0.1:61729", "https://hubs.maclaw.top"},
		defaults,
	)
	want := []string{"https://hubs.maclaw.top"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveHubCenterSeeds(registered) = %#v, want %#v", got, want)
	}
}

func TestEffectiveHubCenterSeedsUsesDefaultsWhenUnregistered(t *testing.T) {
	defaults := []string{"https://hubs.mypapers.top", "https://hubs.maclaw.top", "https://hubs2.maclaw.top"}
	got := EffectiveHubCenterSeeds("", []string{"http://127.0.0.1:9"}, defaults)
	want := []string{"http://127.0.0.1:9", "https://hubs.mypapers.top", "https://hubs.maclaw.top", "https://hubs2.maclaw.top"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveHubCenterSeeds(unregistered) = %#v, want %#v", got, want)
	}
}

func TestEffectiveHubCenterSeedsDropsPublicPollutionWhenLoopbackPreferred(t *testing.T) {
	defaults := []string{"https://hubs.mypapers.top", "https://hubs.maclaw.top", "https://hubs2.maclaw.top"}
	// Loopback preferred with polluted public leftovers must not probe those first.
	got := EffectiveHubCenterSeeds("http://127.0.0.1:9", []string{
		"http://127.0.0.1:9",
		"https://hubs2.maclaw.top",
		"https://hubs.maclaw.top",
	}, defaults)
	want := []string{"http://127.0.0.1:9", "https://hubs.mypapers.top", "https://hubs.maclaw.top", "https://hubs2.maclaw.top"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveHubCenterSeeds(loopback+pollution) = %#v, want %#v", got, want)
	}
	// hubs2 must not appear before official default order (only via defaults list).
	if got[0] != "http://127.0.0.1:9" {
		t.Fatalf("first seed = %q, want loopback", got[0])
	}
}

func TestAlignHubCenterCandidatesDropsUnregisteredDiscoveryPeers(t *testing.T) {
	registered := []string{"https://hubs.maclaw.top"}
	seeds := []string{"https://hubs.maclaw.top"}
	resolved := []string{
		"https://hubs2.maclaw.top", // HA advertised but not enrolled
		"https://hubs.maclaw.top",
		"https://hubs.mypapers.top",
	}
	got := AlignHubCenterCandidates(registered, seeds, resolved)
	want := []string{"https://hubs.maclaw.top"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AlignHubCenterCandidates = %#v, want %#v", got, want)
	}
}

func TestAlignHubCenterCandidatesKeepsDiscoveryWhenUnregistered(t *testing.T) {
	// No registration identity: defaults may be in seeds, but loopback preferred
	// must not be dropped when that is what worked locally.
	seeds := []string{"http://127.0.0.1:1", "https://hubs.mypapers.top", "https://hubs.maclaw.top"}
	resolved := []string{"http://127.0.0.1:50427", "https://hubs2.maclaw.top"}
	got := AlignHubCenterCandidates(nil, seeds, resolved)
	want := []string{"http://127.0.0.1:50427", "https://hubs2.maclaw.top", "http://127.0.0.1:1", "https://hubs.mypapers.top", "https://hubs.maclaw.top"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AlignHubCenterCandidates(unregistered) = %#v, want %#v", got, want)
	}
}

func TestAlignHubCenterCandidatesDoesNotTreatDefaultsAsRegistration(t *testing.T) {
	// Official defaults are seeds only — without RegisteredPublic, do not
	// constrain away a working loopback preferred.
	defaults := []string{"https://hubs.mypapers.top", "https://hubs.maclaw.top", "https://hubs2.maclaw.top"}
	got := AlignHubCenterCandidates(nil, defaults, []string{"http://127.0.0.1:9"})
	if !ContainsHubCenterURL(got, "http://127.0.0.1:9") {
		t.Fatalf("unregistered align dropped working loopback: %#v", got)
	}
}

func TestPreferRegisteredHubCenterBase(t *testing.T) {
	// Loopback preferred is not public enrollment.
	if got := PreferRegisteredHubCenterBase("http://127.0.0.1:1", []string{"https://hubs.maclaw.top"}); got != "" {
		t.Fatalf("PreferRegisteredHubCenterBase(loopback) = %q, want empty", got)
	}
	if got := PreferRegisteredHubCenterBase("https://hubs.maclaw.top", []string{"https://hubs2.maclaw.top"}); got != "https://hubs.maclaw.top" {
		t.Fatalf("PreferRegisteredHubCenterBase(public) = %q", got)
	}
	if got := PreferRegisteredHubCenterBase("", nil); got != "" {
		t.Fatalf("PreferRegisteredHubCenterBase empty = %q", got)
	}
}

func TestPickAlignedHubCenterBase(t *testing.T) {
	aligned := []string{"https://hubs.maclaw.top", "https://hubs.mypapers.top"}
	if got := PickAlignedHubCenterBase("https://hubs2.maclaw.top", aligned); got != "https://hubs.maclaw.top" {
		t.Fatalf("PickAligned dropped base = %q", got)
	}
	if got := PickAlignedHubCenterBase("https://hubs.mypapers.top/", aligned); got != "https://hubs.mypapers.top" {
		t.Fatalf("PickAligned keep base = %q", got)
	}
	if got := PickAlignedHubCenterBase("https://x", nil); got != "" {
		t.Fatalf("PickAligned empty = %q", got)
	}
}

func TestDeprioritizeRecentlyFailedHubCenters(t *testing.T) {
	ResetFailureMemory()
	t.Cleanup(ResetFailureMemory)
	RecordProbeResult("https://hubs2.maclaw.top", false)
	got := DeprioritizeRecentlyFailedHubCenters([]string{
		"https://hubs2.maclaw.top",
		"https://hubs.maclaw.top",
	})
	if len(got) != 2 || got[0] != "https://hubs.maclaw.top" || got[1] != "https://hubs2.maclaw.top" {
		t.Fatalf("DeprioritizeRecentlyFailedHubCenters = %#v", got)
	}
}

func TestConstrainHubCenterPersistenceNeverInjectsDefaults(t *testing.T) {
	// Loopback preferred: drop official defaults, keep custom discovery peers.
	pref, urls := ConstrainHubCenterPersistence(nil, "http://127.0.0.1:9", []string{
		"http://127.0.0.1:9",
		"https://hubs2.maclaw.top",
		"https://hubs.maclaw.top",
		"https://custom.example",
	})
	if pref != "http://127.0.0.1:9" {
		t.Fatalf("preferred = %q", pref)
	}
	want := []string{"http://127.0.0.1:9", "https://custom.example"}
	if !reflect.DeepEqual(urls, want) {
		t.Fatalf("urls = %#v, want %#v", urls, want)
	}

	// Unregistered but selected an official default as preferred: keep only that one.
	pref, urls = ConstrainHubCenterPersistence(nil, "https://hubs.maclaw.top", []string{
		"https://hubs2.maclaw.top",
		"https://hubs.maclaw.top",
		"https://hubs.mypapers.top",
	})
	if pref != "https://hubs.maclaw.top" || !reflect.DeepEqual(urls, []string{"https://hubs.maclaw.top"}) {
		t.Fatalf("selected default only: pref=%q urls=%#v", pref, urls)
	}

	// Registered: drop foreign HA peer; preferred first.
	pref, urls = ConstrainHubCenterPersistence(
		[]string{"https://hubs.maclaw.top"},
		"https://hubs2.maclaw.top",
		[]string{"https://hubs2.maclaw.top", "https://hubs.maclaw.top"},
	)
	if pref != "https://hubs.maclaw.top" {
		t.Fatalf("registered preferred = %q", pref)
	}
	if !reflect.DeepEqual(urls, []string{"https://hubs.maclaw.top"}) {
		t.Fatalf("registered urls = %#v", urls)
	}
}
