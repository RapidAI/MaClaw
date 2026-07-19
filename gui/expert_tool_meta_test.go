package main

import "testing"

func TestLookupExpertToolMeta_KnownTools(t *testing.T) {
	ssh := lookupExpertToolMeta("ssh")
	if ssh.Risk != "dangerous" || ssh.Category != "system" || ssh.LabelZh == "" {
		t.Fatalf("ssh meta = %#v", ssh)
	}
	read := lookupExpertToolMeta("read_file")
	if read.Risk != "safe" || read.Category != "files" {
		t.Fatalf("read_file meta = %#v", read)
	}
	// Alias / case
	if lookupExpertToolMeta("fs_read").LabelZh != "读取文件" {
		t.Fatalf("fs_read label = %q", lookupExpertToolMeta("fs_read").LabelZh)
	}
	if lookupExpertToolMeta("FileRead").Category != "files" {
		t.Fatalf("FileRead category = %q", lookupExpertToolMeta("FileRead").Category)
	}
}

func TestLookupExpertToolMeta_PrefixInference(t *testing.T) {
	k := lookupExpertToolMeta("knowledge_list_sources")
	if k.Category != "knowledge" {
		t.Fatalf("knowledge_* category = %q", k.Category)
	}
	if k.Risk != "safe" {
		t.Fatalf("knowledge list risk want safe, got %q", k.Risk)
	}
	gui := lookupExpertToolMeta("gui_click")
	if gui.Category != "system" || gui.Risk != "dangerous" {
		t.Fatalf("gui_click meta = %#v", gui)
	}
	browser := lookupExpertToolMeta("browser_observe")
	if browser.Category != "automation" {
		t.Fatalf("browser_* category = %q", browser.Category)
	}
}

func TestLookupExpertToolMeta_DeferredCatalogHasMeta(t *testing.T) {
	// Every deferred tool should at least classify (curated or inferred).
	for _, name := range DeferredToolNames {
		m := lookupExpertToolMeta(name)
		if m.Category == "" || m.Risk == "" {
			t.Fatalf("deferred tool %q missing meta: %#v", name, m)
		}
		if m.Risk != "safe" && m.Risk != "elevated" && m.Risk != "dangerous" {
			t.Fatalf("deferred tool %q bad risk %q", name, m.Risk)
		}
	}
}
