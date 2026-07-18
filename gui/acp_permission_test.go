package main

import (
	"path/filepath"
	"testing"
)

func TestAcpToolNeedsPermission(t *testing.T) {
	if !acpToolNeedsPermission("bash", `{"command":"ls"}`) {
		t.Fatal("bash should need permission")
	}
	// write_file enters the gate so workspace auto-allow / outside prompt can run.
	if !acpToolNeedsPermission("write_file", `{"path":"a.go"}`) {
		t.Fatal("write_file should enter permission gate")
	}
	if !acpToolNeedsPermission("delete_file", `{"path":"a.go"}`) {
		t.Fatal("delete should need permission")
	}
}

func TestPathUnderWorkspace(t *testing.T) {
	cwd := filepath.Clean(`D:\work\proj`)
	if !pathUnderWorkspace(cwd, []string{"src/main.go"}) {
		t.Fatal("relative under cwd")
	}
	if pathUnderWorkspace(cwd, []string{`D:\other\x.go`}) {
		t.Fatal("outside cwd")
	}
	// Case-insensitive drive on Windows
	if filepath.Separator == '\\' {
		if !pathUnderWorkspace(`d:\work\proj`, []string{`D:\work\proj\a.go`}) {
			t.Fatal("case-insensitive under cwd")
		}
	}
}

func TestWriteMissingPathRejectedAtGateLogic(t *testing.T) {
	// requestClientPermission with empty paths for write should fail closed.
	s := newACPHostSession(nil, "tok", nil)
	ok, reason := s.requestClientPermission(nil, "sess", "write_file", `{"content":"x"}`, `D:\work`)
	if ok {
		t.Fatalf("expected reject, reason=%q", reason)
	}
	if reason == "" {
		t.Fatal("expected reason")
	}
}

func TestWriteOutsideWorkspaceNotAutoAllowed(t *testing.T) {
	// Outside-cwd write must not auto-allow; without a live client reverse RPC
	// it fails closed (permission request fails / no connection).
	s := newACPHostSession(nil, "tok", nil)
	ok, reason := s.requestClientPermission(nil, "sess", "write_file",
		`{"path":"D:\\other\\escape.go","content":"x"}`, `D:\work\proj`)
	if ok {
		t.Fatalf("outside workspace must not auto-allow, reason=%q", reason)
	}
	if reason == "" {
		t.Fatal("expected rejection reason")
	}
}

func TestWriteInsideWorkspaceAutoAllowed(t *testing.T) {
	s := newACPHostSession(nil, "tok", nil)
	ok, reason := s.requestClientPermission(nil, "sess", "write_file",
		`{"path":"src\\a.go","content":"x"}`, `D:\work\proj`)
	if !ok {
		t.Fatalf("inside workspace should auto-allow, reason=%q", reason)
	}
}
