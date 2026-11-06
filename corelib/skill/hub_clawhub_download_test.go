package skill

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDownloadClawHubMaterializesZipScriptsAndFileList(t *testing.T) {
	t.Run("zip wrapper", func(t *testing.T) {
		zipBody := clawHubZip(t, map[string]string{
			"pack/SKILL.md":         "---\nname: Not Drawing\n---\n# Drawing\n\nFollow prompt-system.md.\n",
			"pack/prompt-system.md": "color notes\n",
			"pack/scripts/draw.py":  "print('mouse')\n",
		})
		dir := t.TempDir()
		entry := downloadClawHubFixture(t, "drawing", "Drawing", zipBody, nil, dir)
		if entry.Name != "Drawing" {
			t.Fatalf("name = %q", entry.Name)
		}
		if len(entry.Triggers) != 1 || entry.Triggers[0] != "drawing" {
			t.Fatalf("triggers = %#v", entry.Triggers)
		}
		if entry.SkillDir != dir {
			t.Fatalf("skill dir = %q", entry.SkillDir)
		}
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "pack")); !os.IsNotExist(err) {
			t.Fatalf("wrapper directory was not hoisted: %v", err)
		}
		notes, err := os.ReadFile(filepath.Join(dir, "prompt-system.md"))
		if err != nil || string(notes) != "color notes\n" {
			t.Fatalf("notes = %q err=%v", notes, err)
		}
		var command string
		for _, step := range entry.Steps {
			text, _ := step.Params["command"].(string)
			if strings.Contains(text, "draw.py") {
				command = text
			}
		}
		if command == "" || !strings.Contains(command, filepath.ToSlash(dir)) {
			t.Fatalf("script step = %q steps=%#v", command, entry.Steps)
		}
		finalDir := filepath.Join(t.TempDir(), "Drawing")
		RetargetInstalledSkillPaths(entry, dir, finalDir)
		retargeted, _ := entry.Steps[0].Params["command"].(string)
		for _, step := range entry.Steps {
			text, _ := step.Params["command"].(string)
			if strings.Contains(text, "draw.py") {
				retargeted = text
			}
		}
		if strings.Contains(retargeted, filepath.ToSlash(dir)) || !strings.Contains(retargeted, filepath.ToSlash(finalDir)) {
			t.Fatalf("retargeted command = %q", retargeted)
		}
	})

	t.Run("public github file list", func(t *testing.T) {
		dir := t.TempDir()
		files := map[string]string{
			"SKILL.md":         "# Drawing\n\nRead the notes.\n",
			"prompt-system.md": "notes\n",
		}
		entry := downloadClawHubFixture(t, "drawing", "Drawing", []byte(`{"source":"public-github"}`), files, dir)
		if _, err := os.Stat(filepath.Join(dir, "prompt-system.md")); err != nil {
			t.Fatal(err)
		}
		if entry.SkillDir != dir || len(entry.Steps) == 0 {
			t.Fatalf("entry = %#v", entry)
		}
	})

	t.Run("unsafe file path", func(t *testing.T) {
		parent := t.TempDir()
		dir := filepath.Join(parent, "pkg")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		files := map[string]string{
			"../evil.txt": "nope",
			"SKILL.md":    "# Drawing\n",
		}
		_, err := downloadClawHubFixtureResult(t, "drawing", "Drawing", []byte(`{"source":"public-github"}`), files, dir)
		if err == nil || !strings.Contains(err.Error(), "unsafe") {
			t.Fatalf("err = %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(parent, "evil.txt")); !os.IsNotExist(statErr) {
			t.Fatalf("path escaped the package dir: %v", statErr)
		}
	})

	t.Run("ephemeral dir", func(t *testing.T) {
		zipBody := clawHubZip(t, map[string]string{"SKILL.md": "# Drawing\n"})
		entry := downloadClawHubFixture(t, "drawing", "Drawing", zipBody, nil, "")
		t.Cleanup(func() { _ = os.RemoveAll(entry.SkillDir) })
		if !EphemeralSkillDownloadDir(entry.SkillDir) || !strings.HasPrefix(filepath.Base(entry.SkillDir), "maclaw-clawhub-dl-") {
			t.Fatalf("skill dir = %q", entry.SkillDir)
		}
		if _, err := os.Stat(filepath.Join(entry.SkillDir, "SKILL.md")); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("empty package", func(t *testing.T) {
		dir := t.TempDir()
		_, err := downloadClawHubFixtureResult(t, "empty-skill", "", []byte(`{"source":"public-github"}`), nil, dir)
		if err == nil || !strings.Contains(err.Error(), "no SKILL.md") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("listed parent directory is not a file", func(t *testing.T) {
		dir := t.TempDir()
		files := map[string]string{
			"SKILL.md":        "# Drawing\n\nRead the script.\n",
			"scripts":         "not a directory\n",
			"scripts/draw.py": "print('mouse')\n",
		}
		downloadClawHubFixture(t, "drawing", "Drawing", []byte(`{"source":"public-github"}`), files, dir)
		info, err := os.Stat(filepath.Join(dir, "scripts"))
		if err != nil || !info.IsDir() {
			t.Fatalf("scripts is not a directory: err=%v info=%v", err, info)
		}
		body, err := os.ReadFile(filepath.Join(dir, "scripts", "draw.py"))
		if err != nil || string(body) != "print('mouse')\n" {
			t.Fatalf("draw.py = %q err=%v", body, err)
		}
	})

	t.Run("baseDir token is not cleaned", func(t *testing.T) {
		entry := &corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command":     "python C:/tmp/stage/scripts/draw.py C:/tmp/stage-extra/scripts/draw.py notC:/tmp/stage/x",
				"working_dir": "C:/tmp/stage",
			},
		}}}
		RetargetInstalledSkillPaths(entry, "C:/tmp/stage", "{baseDir}")
		command, _ := entry.Steps[0].Params["command"].(string)
		working, _ := entry.Steps[0].Params["working_dir"].(string)
		if command != "python {baseDir}/scripts/draw.py C:/tmp/stage-extra/scripts/draw.py notC:/tmp/stage/x" || working != "{baseDir}" {
			t.Fatalf("command=%q working=%q", command, working)
		}
	})
}

func TestClawHubMaterialFilePathsDropsParents(t *testing.T) {
	got := clawHubMaterialFilePaths([]string{
		"scripts/draw.py",
		"scripts",
		"scripts/",
		"SKILL.md",
		"",
		`scripts\nested.txt`,
		"Scripts/other.py",
	})
	want := map[string]int{
		"scripts/draw.py":    1,
		"SKILL.md":           1,
		"scripts/nested.txt": 1,
		"Scripts/other.py":   1,
	}
	if len(got) != len(want) {
		t.Fatalf("paths = %#v", got)
	}
	for _, path := range got {
		want[path]--
	}
	for path, left := range want {
		if left != 0 {
			t.Fatalf("path %s left %d in %#v", path, left, got)
		}
	}
	folded := clawHubMaterialFilePaths([]string{"Scripts/draw.py", "scripts"})
	if len(folded) != 1 || folded[0] != "Scripts/draw.py" {
		t.Fatalf("case-folded parent = %#v", folded)
	}
}

func TestClawHubListedFilesSkipsDirectoryEntries(t *testing.T) {
	raw := json.RawMessage(`[{"path":"scripts","type":"dir"},{"path":"scripts/draw.py","type":"file"},{"name":"notes","kind":"folder"},{"path":"SKILL.md"}]`)
	got, err := clawHubListedFiles(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "scripts/draw.py,SKILL.md" {
		t.Fatalf("files = %#v", got)
	}
}

func downloadClawHubFixture(t *testing.T, slug, display string, download []byte, files map[string]string, target string) *corelib.NLSkillEntry {
	t.Helper()
	entry, err := downloadClawHubFixtureResult(t, slug, display, download, files, target)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func downloadClawHubFixtureResult(t *testing.T, slug, display string, download []byte, files map[string]string, target string) (*corelib.NLSkillEntry, error) {
	t.Helper()
	detail := map[string]interface{}{
		"skill": map[string]string{"slug": slug, "displayName": display, "summary": "draw"},
		"metaContent": map[string]interface{}{
			"skillMd": "# Drawing\n",
			"Files":   fileListKeys(files),
		},
	}
	if len(files) == 0 {
		detail["metaContent"] = map[string]interface{}{"skillMd": "", "Files": []string{}}
	}
	body, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	client := &HubClient{
		userAgent: "test/1.0",
		httpClient: fakeHTTPClient(func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			switch {
			case strings.HasSuffix(path, "/download") || req.URL.Query().Get("slug") != "":
				return newHTTPResponse(http.StatusOK, download, nil), nil
			case strings.HasSuffix(path, "/file"):
				rel := req.URL.Query().Get("path")
				payload, ok := files[rel]
				if !ok {
					return newHTTPResponse(http.StatusNotFound, []byte("missing"), nil), nil
				}
				return newHTTPResponse(http.StatusOK, []byte(payload), nil), nil
			default:
				return newHTTPResponse(http.StatusOK, body, nil), nil
			}
		}),
	}
	return client.DownloadClawHubWithOptions(context.Background(), slug, ClawHubDownloadOptions{TargetDir: target, Source: "clawhub"})
}

func fileListKeys(files map[string]string) []string {
	out := make([]string, 0, len(files))
	for name := range files {
		out = append(out, name)
	}
	return out
}

func clawHubZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
