package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestACPRemoteCodingFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "host"); got != "host" {
		t.Fatalf("got %q", got)
	}
	if got := firstNonEmpty("a", "b"); got != "a" {
		t.Fatalf("got %q", got)
	}
}

func TestOnMaclawListRemoteCodingTasksEmptyApp(t *testing.T) {
	s := &acpHostSession{}
	_, err := s.onMaclawListRemoteCodingTasks(nil)
	if err == nil {
		t.Fatal("expected error for nil app")
	}
}

func TestOnMaclawCreateRemoteCodingTaskRequiresCoordinates(t *testing.T) {
	s := &acpHostSession{app: &App{}}
	_, rpcErr := s.onMaclawCreateRemoteCodingTask([]byte(`{"name":"demo"}`))
	if rpcErr == nil {
		t.Fatal("expected required SSH fields error")
	}
	if !strings.Contains(rpcErr.Message, "ssh_host") {
		t.Fatalf("unexpected error: %#v", rpcErr)
	}
}

func TestOnMaclawCreateRemoteCodingTaskRejectsInvalidPort(t *testing.T) {
	s := &acpHostSession{app: &App{}}
	raw := []byte(`{"name":"demo","ssh_host":"example.com","ssh_user":"root","work_dir":"/srv/app","ssh_port":65536}`)
	_, rpcErr := s.onMaclawCreateRemoteCodingTask(raw)
	if rpcErr == nil || !strings.Contains(rpcErr.Message, "ssh_port") {
		t.Fatalf("expected invalid port error, got %#v", rpcErr)
	}
}

func TestOnMaclawCreateRemoteCodingTaskRejectsNegativePort(t *testing.T) {
	s := &acpHostSession{app: &App{}}
	raw := []byte(`{"name":"demo","ssh_host":"example.com","ssh_user":"root","work_dir":"/srv/app","ssh_port":-1}`)
	_, rpcErr := s.onMaclawCreateRemoteCodingTask(raw)
	if rpcErr == nil || !strings.Contains(rpcErr.Message, "ssh_port") {
		t.Fatalf("expected invalid port error, got %#v", rpcErr)
	}
}

func TestOnMaclawCreateRemoteCodingTaskReportsReuse(t *testing.T) {
	app := newProjectSearchTestApp(t)
	s := &acpHostSession{app: app}
	first, rpcErr := s.onMaclawCreateRemoteCodingTask([]byte(`{"name":"first","ssh_host":"10.0.0.8","ssh_user":"ubuntu","work_dir":"/srv/app","ssh_port":22}`))
	if rpcErr != nil {
		t.Fatalf("first create: %v", rpcErr)
	}
	firstResult, ok := first.(map[string]interface{})
	if !ok || firstResult["reused"] != false {
		t.Fatalf("first result = %#v, want reused=false", first)
	}
	second, rpcErr := s.onMaclawCreateRemoteCodingTask([]byte(`{"name":"second","ssh_host":"10.0.0.8","ssh_user":"ubuntu","work_dir":"/srv/app/","ssh_port":2200}`))
	if rpcErr != nil {
		t.Fatalf("second create: %v", rpcErr)
	}
	secondResult, ok := second.(map[string]interface{})
	if !ok || secondResult["reused"] != true {
		t.Fatalf("second result = %#v, want reused=true", second)
	}
}

func TestOnMaclawPrepareRemoteCodingRequiresPassword(t *testing.T) {
	app := &App{}
	s := &acpHostSession{app: app}
	raw, _ := json.Marshal(map[string]any{
		"project_path": filepath.Join(t.TempDir(), "task"),
		"ssh_host":     "example.com",
		"ssh_user":     "root",
		"work_dir":     "/tmp/x",
	})
	_, rpcErr := s.onMaclawPrepareRemoteCoding(raw)
	if rpcErr == nil {
		t.Fatal("expected password required")
	}
	if !strings.Contains(strings.ToLower(rpcErr.Message), "password") {
		t.Fatalf("unexpected error: %#v", rpcErr)
	}
}

func TestOnMaclawPrepareRemoteCodingRejectsNegativePort(t *testing.T) {
	s := &acpHostSession{app: &App{}}
	raw := []byte(`{"project_path":"task","ssh_port":-1}`)
	_, rpcErr := s.onMaclawPrepareRemoteCoding(raw)
	if rpcErr == nil || !strings.Contains(rpcErr.Message, "ssh_port") {
		t.Fatalf("expected invalid port error, got %#v", rpcErr)
	}
}

func TestOnMaclawGetCodingWorkbenchStatusRequiresPath(t *testing.T) {
	s := &acpHostSession{app: &App{}}
	_, rpcErr := s.onMaclawGetCodingWorkbenchStatus([]byte(`{}`))
	if rpcErr == nil {
		t.Fatal("expected project_path required")
	}
}

func TestAcpResolveRemotePath(t *testing.T) {
	if got := acpResolveRemotePath("/abs/x.go", "/home/proj"); got != "/abs/x.go" {
		t.Fatalf("abs path: %q", got)
	}
	if got := acpResolveRemotePath("src/main.go", "/home/proj"); !strings.HasSuffix(got, "/home/proj/src/main.go") && got != "/home/proj/src/main.go" {
		// remoteCleanPath may normalize; accept clean join
		if got != "/home/proj/src/main.go" {
			t.Fatalf("rel path: %q", got)
		}
	}
}

func TestAcpStripSSHEnvelope(t *testing.T) {
	raw := "noise\n$ ls\ntotal 4\n-rw-r--r-- 1 root root 0 a.go\nEXIT: 0\n"
	got := acpStripSSHEnvelope(raw)
	if !strings.Contains(got, "total 4") || strings.Contains(got, "EXIT:") {
		t.Fatalf("got %q", got)
	}
}

func TestAcpParseSearchHits(t *testing.T) {
	raw := "/home/p/a.go:12:func main() {\n/home/p/b.go:3:package main\n"
	hits := acpParseSearchHits(raw, "/home/p", 50)
	if len(hits) != 2 {
		t.Fatalf("hits=%d %#v", len(hits), hits)
	}
	if hits[0].Path != "/home/p/a.go" || hits[0].Line != 12 {
		t.Fatalf("hit0 %#v", hits[0])
	}
}
