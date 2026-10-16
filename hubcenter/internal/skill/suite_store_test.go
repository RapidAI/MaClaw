package skill

import "testing"

func TestSkillStorePublishAndReloadSuite(t *testing.T) {
	dir := t.TempDir()
	s := NewSkillStore(dir)
	su := SkillSuiteFull{SkillSuiteMeta: SkillSuiteMeta{ID: "office-suite", Name: "Office", Version: "1.0.0", Members: []SkillSuiteMember{{SkillID: "a", Name: "A", Required: true}}}}
	if err := s.PublishSuite(su); err != nil {
		t.Fatalf("PublishSuite: %v", err)
	}
	got, err := s.GetSuite("office-suite")
	if err != nil {
		t.Fatalf("GetSuite: %v", err)
	}
	if len(got.Members) != 1 || got.Members[0].SkillID != "a" {
		t.Fatalf("unexpected members: %#v", got.Members)
	}
	reloaded := NewSkillStore(dir)
	got, err = reloaded.GetSuite("office-suite")
	if err != nil {
		t.Fatalf("GetSuite after reload: %v", err)
	}
	if got.Name != "Office" {
		t.Fatalf("name=%q", got.Name)
	}
}

func TestSkillStoreSuiteVersionHistory(t *testing.T) {
	s := NewSkillStore(t.TempDir())
	base := SkillSuiteFull{SkillSuiteMeta: SkillSuiteMeta{ID: "versioned", Name: "Versioned", Version: "1.0.0", Members: []SkillSuiteMember{{SkillID: "a"}}}}
	if err := s.PublishSuite(base); err != nil {
		t.Fatal(err)
	}
	if err := s.PublishSuite(SkillSuiteFull{SkillSuiteMeta: SkillSuiteMeta{ID: "versioned", Name: "Versioned", Version: "2.0.0", SourceRevision: "abc", Members: []SkillSuiteMember{{SkillID: "a"}}}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSuite("versioned")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.VersionHistory) != 1 || got.VersionHistory[0].Version != "1.0.0" {
		t.Fatalf("unexpected version history: %+v", got.VersionHistory)
	}
}

func TestSkillStoreSnapshotIncludesSuiteRevisionPayloads(t *testing.T) {
	s := NewSkillStore(t.TempDir())
	if err := s.PublishSuite(SkillSuiteFull{SkillSuiteMeta: SkillSuiteMeta{ID: "snap", Name: "Snap", Version: "1.0.0", Members: []SkillSuiteMember{{SkillID: "a"}}}, Skills: []HubSkillFull{{HubSkillMeta: HubSkillMeta{ID: "a"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PublishSuite(SkillSuiteFull{SkillSuiteMeta: SkillSuiteMeta{ID: "snap", Name: "Snap", Version: "2.0.0", Members: []SkillSuiteMember{{SkillID: "a"}}}, Skills: []HubSkillFull{{HubSkillMeta: HubSkillMeta{ID: "a"}}}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.DumpSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.SuiteVersions) != 1 || snapshot.SuiteVersions[0].Version != "1.0.0" {
		t.Fatalf("unexpected suite revisions: %+v", snapshot.SuiteVersions)
	}
	peer := NewSkillStore(t.TempDir())
	if err := peer.LoadSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.RollbackSuite("snap", "1.0.0"); err != nil {
		t.Fatalf("rollback after snapshot: %v", err)
	}
}
