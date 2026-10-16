package database

import (
	"net"
	"strings"
	"testing"
)

func TestAuthorizeProfileEndpointRejectsPublicWithoutFlag(t *testing.T) {
	original := lookupIP
	t.Cleanup(func() { lookupIP = original })
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}
	err := authorizeProfileEndpoint(Profile{Type: SourcePostgres, Host: "db.example.com"})
	if err == nil || !strings.Contains(err.Error(), "allow_external_host") {
		t.Fatalf("got %v", err)
	}
}

func TestAuthorizeProfileEndpointAllowsPublicWithFlag(t *testing.T) {
	original := lookupIP
	t.Cleanup(func() { lookupIP = original })
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}
	if err := authorizeProfileEndpoint(Profile{Type: SourceMySQL, Host: "db.example.com", AllowExternalHost: true}); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizeProfileEndpointRejectsMixedAnswers(t *testing.T) {
	original := lookupIP
	t.Cleanup(func() { lookupIP = original })
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.1"), net.ParseIP("8.8.8.8")}, nil
	}
	err := authorizeProfileEndpoint(Profile{Type: SourceSQLServer, Host: "db.internal", AllowExternalHost: true})
	if err == nil || !strings.Contains(err.Error(), "rebinding") {
		t.Fatalf("got %v", err)
	}
}

func TestAuthorizeProfileEndpointAllowsLoopback(t *testing.T) {
	if err := authorizeProfileEndpoint(Profile{Type: SourceMySQL, Host: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizeProfileEndpointSkipsFiles(t *testing.T) {
	if err := authorizeProfileEndpoint(Profile{Type: SourceExcel, FilePath: "a.xlsx"}); err != nil {
		t.Fatal(err)
	}
}
