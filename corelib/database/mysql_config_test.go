package database

import (
	"errors"
	"strings"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
)

func TestMySQLClientConfigAllowsNativePasswords(t *testing.T) {
	cfg, err := mysqlClientConfig(Profile{ID: "lab", Username: "root", Database: "mysql"}, "secret", "192.168.1.242:3306", "192.168.1.242")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AllowNativePasswords {
		t.Fatal("AllowNativePasswords must be true")
	}
	parsed, err := mysql.ParseDSN(cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.AllowNativePasswords {
		t.Fatal("FormatDSN serialized allowNativePasswords=false")
	}
	if parsed.ParseTime != true {
		t.Fatal("ParseTime must stay enabled")
	}
}

func TestClassifyNativePasswordIsAuthentication(t *testing.T) {
	err := classify(mysql.ErrNativePassword)
	if err == nil || !strings.HasPrefix(err.Error(), "authentication:") {
		t.Fatalf("classify native password = %v", err)
	}
	if connectErrorClass(mysql.ErrNativePassword) != "authentication" {
		t.Fatalf("class = %q", connectErrorClass(mysql.ErrNativePassword))
	}
	if connectErrorClass(errors.New("dial tcp 127.0.0.1:1: connectex: No connection could be made because the target machine actively refused it.")) != "connection" {
		t.Fatal("windows refused connect should stay connection")
	}
}

func TestClassifyIsIdempotent(t *testing.T) {
	once := classify(mysql.ErrNativePassword)
	twice := classify(once)
	if once.Error() != twice.Error() {
		t.Fatalf("native wrap %q vs %q", once, twice)
	}
	denied := errors.New("Error 1045: Access denied for user 'root'@'192.168.1.10' (using password: YES)")
	first := classify(denied)
	second := classify(first)
	if first.Error() != second.Error() {
		t.Fatalf("access denied wrap %q vs %q", first, second)
	}
	if strings.Count(second.Error(), "authentication:") != 1 {
		t.Fatalf("double class prefix: %q", second)
	}
}
