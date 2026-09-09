package tool

import "testing"

func TestRejectShellDatabaseCLI(t *testing.T) {
	for _, tt := range []struct {
		command string
		want    bool
	}{
		{`mysql -h 192.168.1.242 -u root -psecret -e "SHOW DATABASES;"`, true},
		{`mysqldump -u root -pfoo app`, true},
		{`psql -h db.internal -U app -d crm`, true},
		{`sqlcmd -S db -U sa -P secret`, true},
		{`C:\xampp\mysql\bin\mysql.exe -uroot -p`, true},
		{`bash -c "mysql -h db -u root -psecret -e SHOW DATABASES"`, true},
		{`systemctl restart mysql`, false},
		{`net start mysql`, false},
		{`echo mysql`, false},
		{`ls`, false},
	} {
		_, got := RejectShellDatabaseCLI(tt.command)
		if got != tt.want {
			t.Fatalf("RejectShellDatabaseCLI(%q) = %v, want %v", tt.command, got, tt.want)
		}
	}
}
