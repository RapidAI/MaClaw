package tool

import "testing"

func TestRejectRawSSHCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "ssh direct", command: "ssh root@example.com", want: true},
		{name: "ssh after separator", command: "echo ok; ssh root@example.com", want: true},
		{name: "ssh via command builtin", command: "command ssh root@example.com", want: true},
		{name: "ssh via exec", command: "exec ssh root@example.com", want: true},
		{name: "ssh via env", command: "env TERM=xterm ssh root@example.com", want: true},
		{name: "ssh via env option", command: "env -i TERM=xterm ssh root@example.com", want: true},
		{name: "ssh via timeout", command: "timeout 5 ssh root@example.com", want: true},
		{name: "ssh via nohup", command: "nohup ssh root@example.com", want: true},
		{name: "scp direct", command: "scp app root@example.com:/tmp/app", want: true},
		{name: "sftp direct", command: "sftp root@example.com", want: true},
		{name: "rsync remote destination", command: "rsync -az build/ root@example.com:/srv/app", want: true},
		{name: "rsync remote source", command: "rsync root@example.com:/srv/app ./backup", want: true},
		{name: "rsync host relative destination", command: "rsync app.tar example.com:app.tar", want: true},
		{name: "rsync url remote", command: "rsync rsync://example.com/module ./module", want: true},
		{name: "rsync daemon remote", command: "rsync rsync.example.com::module ./module", want: true},
		{name: "rsync via env", command: "env RSYNC_RSH=ssh rsync -az build/ root@example.com:/srv/app", want: true},
		{name: "ssh exe", command: "ssh.exe root@example.com", want: true},
		{name: "nested bash quoted", command: "bash -lc 'ssh root@example.com uptime'", want: true},
		{name: "nested bash unquoted", command: "bash -lc ssh root@example.com uptime", want: true},
		{name: "nested powershell quoted", command: "powershell -Command \"ssh root@example.com\"", want: true},
		{name: "nested powershell unquoted", command: "powershell -Command ssh root@example.com", want: true},
		{name: "nested cmd", command: "cmd /c ssh root@example.com", want: true},
		{name: "nested wrapper", command: "bash -lc 'env TERM=xterm ssh root@example.com uptime'", want: true},
		{name: "nested rsync remote", command: "bash -lc 'rsync -az build/ root@example.com:/srv/app'", want: true},
		{name: "nested full windows powershell path", command: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -Command ssh root@example.com`, want: true},
		{name: "nested full unix bash path", command: "/bin/bash -lc ssh root@example.com", want: true},
		{name: "ssh keygen allowed", command: "ssh-keygen -t ed25519", want: false},
		{name: "local rsync allowed", command: "rsync -az ./src/ ./dst/", want: false},
		{name: "local relative colon rsync allowed", command: "rsync ./foo:bar ./dst/", want: false},
		{name: "local parent colon rsync allowed", command: "rsync ../foo:bar ./dst/", want: false},
		{name: "local absolute colon rsync allowed", command: "rsync /tmp/foo:bar ./dst/", want: false},
		{name: "windows local rsync allowed", command: `rsync C:\tmp\src\ C:\tmp\dst\`, want: false},
		{name: "plain echo allowed", command: "echo ssh root@example.com", want: false},
		{name: "plain echo rsync allowed", command: "echo rsync root@example.com:/srv/app", want: false},
		{name: "env var named ssh allowed", command: "env ssh=disabled echo ok", want: false},
		{name: "nested echo allowed", command: "bash -lc 'echo ssh root@example.com'", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := RejectRawSSHCommand(tt.command)
			if got != tt.want {
				t.Fatalf("RejectRawSSHCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}
