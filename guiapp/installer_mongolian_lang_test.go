package guiapp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerSelectsCyrillicMongolian(t *testing.T) {
	text := readRepoFile(t, filepath.Join("build", "windows", "installer", "multiarch.nsi"))
	if !strings.Contains(text, `!insertmacro MUI_LANGUAGE "Mongolian"`) {
		t.Fatal("missing Mongolian MUI language")
	}
	if !strings.Contains(text, "LangString LaunchAfterInstall ${LANG_MONGOLIAN}") {
		t.Fatal("missing Mongolian launch string")
	}
	if !strings.Contains(text, "LangString AlreadyInstalled ${LANG_MONGOLIAN}") {
		t.Fatal("missing Mongolian already-installed string")
	}
	if !strings.Contains(text, "LangString AppIsRunning ${LANG_MONGOLIAN}") {
		t.Fatal("missing Mongolian running-app string")
	}
	if !strings.Contains(text, "LangString AppStillRunning ${LANG_MONGOLIAN}") {
		t.Fatal("missing Mongolian still-running string")
	}
	if !strings.Contains(text, "LangString DeleteUserData ${LANG_MONGOLIAN}") {
		t.Fatal("missing Mongolian delete-user-data string")
	}
	if !strings.Contains(text, `.maclaw/data`) || !strings.Contains(text, `memories.json`) {
		t.Fatal("delete-user-data copy must keep the preserved paths")
	}
	if !strings.Contains(text, `StrCmp $0 "1104" lang_mn`) {
		t.Fatal("missing Cyrillic Mongolian LANGID 1104")
	}
	if strings.Contains(text, `StrCmp $0 "2128"`) {
		t.Fatal("traditional Mongolian LANGID 2128 must stay on the English path")
	}
	for _, keep := range []string{"1041", "1042", "1036", "1031", "3082", "1049"} {
		if !strings.Contains(text, `StrCmp $0 "`+keep+`"`) {
			t.Fatalf("existing installer language %s was removed", keep)
		}
	}
}
