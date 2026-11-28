package cloudagent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func control(t *testing.T, rt *Runtime, id, op, body string) json.RawMessage {
	t.Helper()
	raw, err := rt.Control(context.Background(), id, "user-a", op, []byte(body))
	if err != nil {
		t.Fatalf("%s err=%v", op, err)
	}
	return raw
}

func controlErr(t *testing.T, rt *Runtime, id, op, body string) error {
	t.Helper()
	_, err := rt.Control(context.Background(), id, "user-a", op, []byte(body))
	if err == nil {
		t.Fatalf("%s %s unexpectedly succeeded", op, body)
	}
	return err
}

func seedWorkspace(t *testing.T, rt *Runtime, id string) string {
	t.Helper()
	inst, err := rt.instance(id)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(inst.dir, "workspace")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello-world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.go"), []byte("package sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Hidden and secret-looking entries must never leave the jail.
	if err := os.WriteFile(filepath.Join(root, ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "llm.json"), []byte(`{"key":"secret"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestBrowseListDir(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	seedWorkspace(t, rt, spec.ID)

	raw := control(t, rt, spec.ID, "cloud_list_dir", `{"path":""}`)
	var out struct {
		Path    string `json:"path"`
		Entries []struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			IsDir bool   `json:"is_dir"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Path != "/" {
		t.Fatalf("root path=%q", out.Path)
	}
	names := map[string]bool{}
	dirs := map[string]bool{}
	for _, e := range out.Entries {
		names[e.Name] = true
		dirs[e.Name] = e.IsDir
	}
	for _, want := range []string{"a.txt", "sub"} {
		if !names[want] {
			t.Fatalf("missing %s in %s", want, raw)
		}
	}
	if !dirs["sub"] || dirs["a.txt"] {
		t.Fatalf("is_dir wrong: %s", raw)
	}
	if names[".hidden"] || names["llm.json"] {
		t.Fatalf("hidden/secret leaked: %s", raw)
	}
	// dirs sort before files
	if len(out.Entries) >= 2 && !out.Entries[0].IsDir {
		t.Fatalf("dir not first: %s", raw)
	}

	raw = control(t, rt, spec.ID, "cloud_list_dir", `{"path":"sub"}`)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 1 || out.Entries[0].Name != "b.go" || out.Entries[0].Path != "sub/b.go" {
		t.Fatalf("subdir listing=%s", raw)
	}
}

func TestBrowseReadFile(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	seedWorkspace(t, rt, spec.ID)

	raw := control(t, rt, spec.ID, "cloud_read_file", `{"path":"a.txt"}`)
	var out struct {
		Name      string `json:"name"`
		Size      int64  `json:"size"`
		Truncated bool   `json:"truncated"`
		Data      string `json:"data_base64"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello-world" || out.Size != 11 || out.Truncated || out.Name != "a.txt" {
		t.Fatalf("read=%s", raw)
	}

	raw = control(t, rt, spec.ID, "cloud_read_file", `{"path":"a.txt","max_bytes":4}`)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	data, _ = base64.StdEncoding.DecodeString(out.Data)
	if string(data) != "hell" || !out.Truncated {
		t.Fatalf("capped read=%s", raw)
	}
}

func TestBrowseReadFileReturnsStashedOriginal(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	seedWorkspace(t, rt, spec.ID)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	inst.rememberPreviewBaseline("a.txt", "old-text")
	inst.rememberPreviewBaseline("a.txt", "newer-text")

	raw := control(t, rt, spec.ID, "cloud_read_file", `{"path":"a.txt"}`)
	var out struct {
		Original  string `json:"original"`
		Data      string `json:"data_base64"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello-world" || out.Truncated || out.Original != "old-text" {
		t.Fatalf("read with original=%s", raw)
	}

	raw = control(t, rt, spec.ID, "cloud_read_file", `{"path":"a.txt","max_bytes":4}`)
	out = struct {
		Original  string `json:"original"`
		Data      string `json:"data_base64"`
		Truncated bool   `json:"truncated"`
	}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Original != "" || !out.Truncated {
		t.Fatalf("truncated read must omit the baseline: %s", raw)
	}
}

func TestBrowseStat(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	seedWorkspace(t, rt, spec.ID)

	raw := control(t, rt, spec.ID, "cloud_stat", `{"path":"sub/b.go"}`)
	var out struct {
		Name      string `json:"name"`
		Path      string `json:"path"`
		IsDir     bool   `json:"is_dir"`
		Size      int64  `json:"size"`
		SizeKnown bool   `json:"size_known"`
		Mode      string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "b.go" || out.Path != "sub/b.go" || out.IsDir || !out.SizeKnown || out.Size != 12 || out.Mode == "" {
		t.Fatalf("stat=%s", raw)
	}
	raw = control(t, rt, spec.ID, "cloud_stat", `{"path":""}`)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.IsDir || out.Path != "/" {
		t.Fatalf("root stat=%s", raw)
	}
}

func TestBrowseDelete(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	root := seedWorkspace(t, rt, spec.ID)

	control(t, rt, spec.ID, "cloud_delete_file", `{"path":"a.txt"}`)
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("a.txt still there: %v", err)
	}
	control(t, rt, spec.ID, "cloud_delete_file", `{"path":"sub"}`)
	if _, err := os.Stat(filepath.Join(root, "sub")); !os.IsNotExist(err) {
		t.Fatalf("sub still there: %v", err)
	}
	controlErr(t, rt, spec.ID, "cloud_delete_file", `{"path":""}`)
	controlErr(t, rt, spec.ID, "cloud_delete_file", `{"path":"/"}`)
}

func TestBrowsePackDir(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	seedWorkspace(t, rt, spec.ID)

	raw := control(t, rt, spec.ID, "cloud_pack_dir", `{"path":"sub"}`)
	var out struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		Data string `json:"data_base64"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "sub.tar.gz" {
		t.Fatalf("name=%s", out.Name)
	}
	data, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil {
		t.Fatal(err)
	}
	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("pack is not gzip: %v", err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
	}
	if len(names) != 1 || names[0] != "b.go" {
		t.Fatalf("tar=%v", names)
	}
}

func TestBrowseJailEscapeRefused(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	seedWorkspace(t, rt, spec.ID)

	for _, op := range []string{"cloud_list_dir", "cloud_read_file", "cloud_stat", "cloud_delete_file", "cloud_pack_dir", "cloud_write_file", "cloud_mkdir"} {
		controlErr(t, rt, spec.ID, op, `{"path":"../"}`)
		controlErr(t, rt, spec.ID, op, `{"path":"a/../../b"}`)
		controlErr(t, rt, spec.ID, op, `{"path":"llm.json"}`)
		controlErr(t, rt, spec.ID, op, `{"path":".git"}`)
		controlErr(t, rt, spec.ID, op, `{"path":"x/.git/hooks/pre-push"}`)
	}
	controlErr(t, rt, spec.ID, "cloud_read_file", `{"path":"sub"}`)
	controlErr(t, rt, spec.ID, "cloud_list_dir", `{"path":"a.txt"}`)
}

func TestBrowseWriteFile(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	root := seedWorkspace(t, rt, spec.ID)

	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	write := func(path, body string, offset int64) json.RawMessage {
		payload, _ := json.Marshal(map[string]any{"path": path, "data_base64": body, "offset": offset})
		return control(t, rt, spec.ID, "cloud_write_file", string(payload))
	}
	// Chunked append: first chunk creates, the second must match the offset.
	write("new/deep/f.txt", b64("hello"), 0)
	write("new/deep/f.txt", b64("-world"), 5)
	raw, err := os.ReadFile(filepath.Join(root, "new", "deep", "f.txt"))
	if err != nil || string(raw) != "hello-world" {
		t.Fatalf("written=%q err=%v", raw, err)
	}
	// offset==0 truncates an existing file.
	write("new/deep/f.txt", b64("x"), 0)
	raw, _ = os.ReadFile(filepath.Join(root, "new", "deep", "f.txt"))
	if string(raw) != "x" {
		t.Fatalf("truncated=%q", raw)
	}
	// A mismatched offset and a missing target both refuse rather than corrupt.
	controlErr(t, rt, spec.ID, "cloud_write_file", `{"path":"new/deep/f.txt","data_base64":"`+b64("y")+`","offset":7}`)
	controlErr(t, rt, spec.ID, "cloud_write_file", `{"path":"gone.txt","data_base64":"`+b64("y")+`","offset":3}`)
	controlErr(t, rt, spec.ID, "cloud_write_file", `{"path":"sub","data_base64":"`+b64("y")+`","offset":0}`)
	controlErr(t, rt, spec.ID, "cloud_write_file", `{"path":"","data_base64":"`+b64("y")+`","offset":0}`)
	controlErr(t, rt, spec.ID, "cloud_write_file", `{"path":"f.txt","data_base64":"%%%","offset":0}`)
}

func TestBrowseMkdir(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	root := seedWorkspace(t, rt, spec.ID)

	control(t, rt, spec.ID, "cloud_mkdir", `{"path":"deep/nested/dir"}`)
	info, err := os.Stat(filepath.Join(root, "deep", "nested", "dir"))
	if err != nil || !info.IsDir() {
		t.Fatalf("mkdir err=%v", err)
	}
	// Existing dir is a no-op; a file at that path refuses.
	control(t, rt, spec.ID, "cloud_mkdir", `{"path":"sub"}`)
	controlErr(t, rt, spec.ID, "cloud_mkdir", `{"path":"a.txt"}`)
	controlErr(t, rt, spec.ID, "cloud_mkdir", `{"path":""}`)
}

func TestBrowseCodingWithoutCheckoutUsesWorkspace(t *testing.T) {
	rt := openRuntime(t)
	spec := Spec{
		ID:          newID("cag_"),
		TenantID:    "tenant-a",
		OwnerUserID: "user-a",
		Name:        "coding",
		Kind:        KindCoding,
		HubOrigin:   "https://hub.example",
		HubLoopback: "http://127.0.0.1:9",
	}
	if err := rt.Put(spec); err != nil {
		t.Fatal(err)
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	// No checkout: the bare workspace is the jail root.
	if err := os.WriteFile(filepath.Join(inst.dir, "workspace", "main.cpp"), []byte("int main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := control(t, rt, spec.ID, "cloud_list_dir", `{"path":""}`)
	if !strings.Contains(string(raw), "main.cpp") {
		t.Fatalf("repo-less listing=%s", raw)
	}
	// A directory holding .git is just data until the binding in git.json
	// names it: without one the bare workspace stays the root.
	repo := filepath.Join(inst.dir, "workspace", "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeGit(inst.dir, gitSecret{Token: "bind-token-01234567", Clone: "https://github.com/acme/repo.git", Repo: "acme/repo"}); err != nil {
		t.Fatal(err)
	}
	raw = control(t, rt, spec.ID, "cloud_list_dir", `{"path":""}`)
	if !strings.Contains(string(raw), "main.go") || strings.Contains(string(raw), "main.cpp") {
		t.Fatalf("listing=%s", raw)
	}
}
