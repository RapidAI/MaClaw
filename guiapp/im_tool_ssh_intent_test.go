package guiapp

import "testing"

func TestRejectSSHForDatabaseIntent(t *testing.T) {
	if got := rejectSSHForDatabaseIntent("ssh", "查看 192.168.1.242 上的mysql数据库，用户名root"); got == "" {
		t.Fatal("mysql request must not use ssh")
	}
	if got := rejectSSHForDatabaseIntent("database", "查看 mysql"); got != "" {
		t.Fatalf("database tool blocked: %q", got)
	}
	if got := rejectSSHForDatabaseIntent("ssh", "ssh 登录 192.168.1.242"); got != "" {
		t.Fatalf("explicit ssh blocked: %q", got)
	}
	if got := rejectSSHForDatabaseIntent("ssh", "用 powershell 查看 mysql 数据库"); got == "" {
		t.Fatal("powershell must not bypass the mysql ssh block")
	}
	if got := rejectSSHForDatabaseIntent("ssh", "查看 mariadb 实例"); got == "" {
		t.Fatal("mariadb request must not use ssh")
	}
	if got := rejectSSHForDatabaseIntent("SSH", "查看 mysql 数据库"); got == "" {
		t.Fatal("SSH tool name must be matched without regard to case")
	}
	if got := rejectSSHForDatabaseIntent("ssh", "查看 oracle 数据库"); got != "" {
		t.Fatalf("unsupported engine must allow ssh: %q", got)
	}
	if got := rejectSSHForDatabaseIntent("ssh", "用 mongodb 查订单"); got != "" {
		t.Fatalf("mongodb must allow ssh: %q", got)
	}
	if got := rejectSSHForDatabaseIntent("ssh", "search mongolian history 数据库"); got == "" {
		t.Fatal("mongolian must not be treated as mongodb")
	}
}

func TestSQLDatabaseFollowUpUserText(t *testing.T) {
	if !sqlDatabaseFollowUpUserText("查看 rapidbi库") {
		t.Fatal("latin catalog 库 follow-up must overlay SQL database")
	}
	if !sqlDatabaseFollowUpUserText("查看 mysql 数据库") {
		t.Fatal("mysql text must overlay SQL database")
	}
	if sqlDatabaseFollowUpUserText("去图书馆借书") {
		t.Fatal("图书馆 must not overlay SQL database")
	}
	if !unsupportedExternalDatabaseUserText("查看 oracle 数据库") {
		t.Fatal("oracle must stay on the external-tool path")
	}
}

func TestRejectBashForDatabaseIntent(t *testing.T) {
	if got := rejectBashForDatabaseIntent("bash", map[string]interface{}{"command": "mysql -h 192.168.1.242 -uroot -p"}, "查看 mysql 数据库"); got == "" {
		t.Fatal("bash mysql must be rejected for a database request")
	}
	if got := rejectBashForDatabaseIntent("bash", map[string]interface{}{"command": "ls /tmp"}, "查看 mysql 数据库"); got != "" {
		t.Fatalf("ordinary bash blocked: %q", got)
	}
	if got := rejectBashForDatabaseIntent("bash", map[string]interface{}{"command": "mysql -h db -uroot"}, "列出目录"); got != "" {
		t.Fatalf("bash mysql blocked without database intent: %q", got)
	}
	if got := rejectBashForDatabaseIntent("BASH", map[string]interface{}{"command": "mariadb -h db -uroot"}, "查看 mysql 数据库"); got == "" {
		t.Fatal("bash mariadb must be rejected for a database request")
	}
	if got := rejectBashForDatabaseIntent("bash", map[string]interface{}{"command": "redis-cli ping"}, "查看 redis 数据库"); got != "" {
		t.Fatalf("unsupported engine must allow bash: %q", got)
	}
}
