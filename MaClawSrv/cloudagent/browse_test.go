package cloudagent

import (
	"archive/tar"
	"bytes"
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
	if out.Name != "sub.tar" {
		t.Fatalf("name=%s", out.Name)
	}
	data, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(data))
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

	for _, op := range []string{"cloud_list_dir", "cloud_read_file", "cloud_stat", "cloud_delete_file", "cloud_pack_dir"} {
		controlErr(t, rt, spec.ID, op, `{"path":"../"}`)
		controlErr(t, rt, spec.ID, op, `{"path":"a/../../b"}`)
		controlErr(t, rt, spec.ID, op, `{"path":"llm.json"}`)
	}
	controlErr(t, rt, spec.ID, "cloud_read_file", `{"path":"sub"}`)
	controlErr(t, rt, spec.ID, "cloud_list_dir", `{"path":"a.txt"}`)
}

func TestBrowseCodingRequiresCheckout(t *testing.T) {
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
	err := controlErr(t, rt, spec.ID, "cloud_list_dir", `{"path":""}`)
	if !strings.Contains(err.Error(), "检出") {
		t.Fatalf("err=%v", err)
	}
	// With a checkout the jail root is the repo dir.
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(inst.dir, "workspace", "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := control(t, rt, spec.ID, "cloud_list_dir", `{"path":""}`)
	if !strings.Contains(string(raw), "main.go") {
		t.Fatalf("listing=%s", raw)
	}
}
