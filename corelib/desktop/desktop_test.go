package desktop

import (
	"strings"
	"testing"
)

func TestUserKeyIgnoresInstanceAndRejectsEmptyUser(t *testing.T) {
	first, err := UserKey("tenant", "alice")
	if err != nil || !ValidUserKey(first) {
		t.Fatalf("key=%q err=%v", first, err)
	}
	second, err := UserKey("tenant", "alice")
	if err != nil || second != first {
		t.Fatalf("same user key changed: %q %q", first, second)
	}
	other, err := UserKey("tenant", "bob")
	if err != nil || other == first {
		t.Fatalf("different user reused %q", other)
	}
	if _, err := UserKey("tenant", "  "); err == nil {
		t.Fatal("empty user was accepted")
	}
	aliceMounts, err := PrivateMounts("tenant", "alice")
	if err != nil || len(aliceMounts) != 4 {
		t.Fatalf("mounts=%#v err=%v", aliceMounts, err)
	}
	bobMounts, err := PrivateMounts("tenant", "bob")
	if err != nil {
		t.Fatal(err)
	}
	for i := range aliceMounts {
		if aliceMounts[i].Volume == bobMounts[i].Volume {
			t.Fatalf("users share volume %s", aliceMounts[i].Volume)
		}
		if !strings.Contains(aliceMounts[i].Volume, first) {
			t.Fatalf("volume %s is not bound to alice", aliceMounts[i].Volume)
		}
	}
	image, err := StateImage("tenant", "alice")
	if err != nil || image != "maclaw-desktop-user-"+first+":state" || image == DefaultImage {
		t.Fatalf("state image=%q", image)
	}
}

func TestNormalizeResourcesUsesContainerDefaults(t *testing.T) {
	got, err := NormalizeResources("", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Image != DefaultImage || got.Memory != DefaultMemory || got.CPUs != DefaultCPUs || got.ShmSize != DefaultShmSize {
		t.Fatalf("defaults=%#v", got)
	}
	got, err = NormalizeResources("maclaw-gui:1", "4G", "2", "1G")
	if err != nil || got.Memory != "4g" || got.CPUs != "2" || got.ShmSize != "1g" {
		t.Fatalf("resources=%#v err=%v", got, err)
	}
	if _, err := NormalizeResources("bad image", "4g", "2", "1g"); err == nil {
		t.Fatal("invalid image was accepted")
	}
}

func TestParseEnsureReadsSupervisorLine(t *testing.T) {
	got, err := ParseEnsure("starting\n{\"display\":\":20\",\"cdp_port\":19020}\n")
	if err != nil || got.Display != ":20" || got.CDPPort != 19020 {
		t.Fatalf("status=%#v err=%v", got, err)
	}
	if _, err := ParseEnsure("no json"); err == nil {
		t.Fatal("missing status was accepted")
	}
}
