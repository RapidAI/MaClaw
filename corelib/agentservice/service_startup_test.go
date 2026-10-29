package agentservice

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type closeTrackingServiceResource struct {
	closeCalls int
}

func (r *closeTrackingServiceResource) Close() error {
	r.closeCalls++
	return nil
}

func TestOpenServiceResourcesClosesSuccessfulResourcesOnError(t *testing.T) {
	first := &closeTrackingServiceResource{}
	third := &closeTrackingServiceResource{}
	openErr := errors.New("open failed")
	_, err := openServiceResources([]serviceResourceOpen{
		{name: "create record store", open: func() (interface{}, error) { return first, nil }},
		{name: "create dynamic operation ledger", open: func() (interface{}, error) { return nil, openErr }},
		{name: "create dynamic capability registry", open: func() (interface{}, error) { return third, nil }},
	})
	if !errors.Is(err, openErr) {
		t.Fatalf("openServiceResources error = %v, want %v", err, openErr)
	}
	if !strings.Contains(err.Error(), "create dynamic operation ledger") {
		t.Fatalf("error lost resource context: %v", err)
	}
	if first.closeCalls != 1 || third.closeCalls != 1 {
		t.Fatalf("successful resource close calls = %d, %d; want 1, 1", first.closeCalls, third.closeCalls)
	}
}

func TestOpenServiceResourcesReturnsFirstErrorInDeclarationOrder(t *testing.T) {
	firstErr := errors.New("first")
	laterErr := errors.New("later")
	middle := &closeTrackingServiceResource{}
	_, err := openServiceResources([]serviceResourceOpen{
		{name: "first store", open: func() (interface{}, error) { return nil, firstErr }},
		{name: "middle store", open: func() (interface{}, error) { return middle, nil }},
		{name: "later store", open: func() (interface{}, error) { return nil, laterErr }},
	})
	if !errors.Is(err, firstErr) || errors.Is(err, laterErr) {
		t.Fatalf("error = %v, want only the first declaration-order failure", err)
	}
	if middle.closeCalls != 1 {
		t.Fatalf("successful resource close calls = %d, want 1", middle.closeCalls)
	}
}

func TestNewServiceParallelOpenFailureClosesResources(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "operations"), []byte("blocks database directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Config{DataRoot: root}, nil, EchoExecutor{})
	if err == nil || svc != nil {
		t.Fatalf("NewService = %v, %v; want nil service and open error", svc, err)
	}
	if !strings.Contains(err.Error(), "create dynamic operation ledger") {
		t.Fatalf("NewService error = %v, want dynamic operation context", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove data root after failed parallel open (open handles may have leaked): %v", err)
	}
}
