package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerShellVerbIsPerBrandDocumentCommand(t *testing.T) {
	text := readRepoFile(t, filepath.Join("build", "windows", "installer", "multiarch.nsi"))
	if !strings.Contains(text, `${INFO_PROJECTNAME}.FileCompanion`) {
		t.Fatal("missing per-brand verb")
	}
	if !strings.Contains(text, "open-file") {
		t.Fatal("missing open-file")
	}
	if !strings.Contains(text, `MultiSelectModel" "Document"`) {
		t.Fatal("missing document multi-select")
	}
	if !strings.Contains(text, "用 ${INFO_PRODUCTNAME} 伴读打开") {
		t.Fatal("missing companion menu label")
	}
	if !strings.Contains(text, `NeverDefault" ""`) {
		t.Fatal("missing NeverDefault")
	}
	if strings.Contains(text, "!insertmacro FileCompanionAddVerb ") || strings.Contains(text, "!insertmacro FileCompanionRemoveVerb ") {
		t.Fatal("verb target bypasses the shared install/uninstall list")
	}
	targets := nsiMacroBody(t, text, "FileCompanionVerbTargets")
	seen := map[string]bool{}
	for _, line := range strings.Split(targets, "\n") {
		line = strings.TrimSpace(line)
		const prefix = `!insertmacro ${OP} "`
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		key := strings.TrimSuffix(strings.TrimPrefix(line, prefix), `"`)
		if seen[key] {
			t.Fatalf("duplicate verb target %s", key)
		}
		seen[key] = true
	}
	for _, key := range []string{
		`Software\Classes\*\shell\${INFO_PROJECTNAME}.FileCompanion`,
		`Software\Classes\SystemFileAssociations\.pdf\shell\${INFO_PROJECTNAME}.FileCompanion`,
		`Software\Classes\SystemFileAssociations\.docx\shell\${INFO_PROJECTNAME}.FileCompanion`,
		`Software\Classes\SystemFileAssociations\.pptx\shell\${INFO_PROJECTNAME}.FileCompanion`,
		`Software\Classes\SystemFileAssociations\.md\shell\${INFO_PROJECTNAME}.FileCompanion`,
		`Software\Classes\SystemFileAssociations\text\shell\${INFO_PROJECTNAME}.FileCompanion`,
		`Software\Classes\SystemFileAssociations\image\shell\${INFO_PROJECTNAME}.FileCompanion`,
		`Software\Classes\SystemFileAssociations\document\shell\${INFO_PROJECTNAME}.FileCompanion`,
		`Software\Classes\SystemFileAssociations\video\shell\${INFO_PROJECTNAME}.FileCompanion`,
		`Software\Classes\SystemFileAssociations\audio\shell\${INFO_PROJECTNAME}.FileCompanion`,
	} {
		if !seen[key] {
			t.Fatalf("shared verb list missing %s", key)
		}
	}
	if strings.Contains(targets, `Software\Classes\Directory`) {
		t.Fatal("verb list registers a folder")
	}
	uninstallAt := strings.Index(text, `Section "uninstall"`)
	if uninstallAt < 0 {
		t.Fatal("missing uninstall section")
	}
	addCall := "!insertmacro FileCompanionVerbTargets FileCompanionAddVerb"
	removeCall := "!insertmacro FileCompanionVerbTargets FileCompanionRemoveVerb"
	if strings.Count(text, addCall) != 1 {
		t.Fatal("install must expand the shared verb list once")
	}
	addAt := strings.Index(text, addCall)
	if addAt > uninstallAt {
		t.Fatal("verb registration is in the uninstall section")
	}
	view := strings.LastIndex(text[:addAt], "SetRegView")
	if view < 0 || !strings.Contains(text[view:addAt], "SetRegView 64") {
		t.Fatal("verb is not in the HKLM 64-bit view")
	}
	uninstall := text[uninstallAt:]
	if strings.Count(uninstall, removeCall) != 2 {
		t.Fatal("uninstall does not remove the verb from both registry views")
	}
	view32 := strings.Index(uninstall, "SetRegView 32")
	if view32 < 0 || !strings.Contains(uninstall[view32:], removeCall) {
		t.Fatal("32-bit view does not remove the verb")
	}
	if strings.Count(text[:uninstallAt], "SHChangeNotify") != 1 || strings.Count(uninstall, "SHChangeNotify") != 1 {
		t.Fatal("install and uninstall must each tell Explorer the associations changed")
	}
	if !strings.Contains(text, `"$APPDATA\${INFO_PROJECTNAME}.file-companion"`) {
		t.Fatal("uninstall does not remove the companion profile")
	}
	if strings.Contains(text, "MaClaw*") {
		t.Fatal("installer uses a MaClaw* glob")
	}
	openMarker := `Software\Classes\maclaw\shell\open\command`
	openAt := strings.Index(text, openMarker)
	if openAt < 0 {
		t.Fatal("shell\\open command missing")
	}
	lineEnd := strings.Index(text[openAt:], "\n")
	line := text[openAt : openAt+lineEnd]
	if strings.Contains(line, "open-file") {
		t.Fatal("shell\\open was changed to the file-companion command")
	}
}

func TestDarwinDocumentTypeIsAlternateViewer(t *testing.T) {
	for _, name := range []string{
		filepath.Join("build", "darwin", "Info.plist"),
		filepath.Join("build", "darwin", "Info.dev.plist"),
		"build_maclinux.sh",
	} {
		text := readRepoFile(t, name)
		if !strings.Contains(text, "CFBundleTypeRole") || !strings.Contains(text, "Viewer") {
			t.Fatalf("%s is not a Viewer", name)
		}
		if !strings.Contains(text, "LSHandlerRank") || !strings.Contains(text, "Alternate") {
			t.Fatalf("%s is not Alternate", name)
		}
		if !strings.Contains(text, "public.data") {
			t.Fatalf("%s missing public.data", name)
		}
		if strings.Contains(text, "public.folder") {
			t.Fatalf("%s declares a folder type", name)
		}
		if strings.Contains(text, "lsregister -u") {
			t.Fatalf("%s adds an lsregister -u uninstall", name)
		}
	}
}

func nsiMacroBody(t *testing.T, text, name string) string {
	t.Helper()
	marker := "!macro " + name
	start := strings.Index(text, marker)
	if start < 0 {
		t.Fatalf("missing macro %s", name)
	}
	rest := text[start+len(marker):]
	end := strings.Index(rest, "!macroend")
	if end < 0 {
		t.Fatalf("macro %s is not closed", name)
	}
	return rest[:end]
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join("..", rel)
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}
