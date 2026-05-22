package remote

import "testing"

func TestLooksLikeShellPrompt(t *testing.T) {
	tests := []struct {
		name   string
		lines  []string
		expect bool
	}{
		{
			name:   "empty",
			lines:  nil,
			expect: false,
		},
		{
			name:   "simple root prompt",
			lines:  []string{"root@server:~# "},
			expect: true,
		},
		{
			name:   "simple user prompt",
			lines:  []string{"user@host:~/project$ "},
			expect: true,
		},
		{
			name:   "prompt with ANSI OSC title + CSI bracket-paste",
			lines:  []string{"\x1b[?2004h\x1b]0;root@racknerd-2453af7: /opt/omniroute-src\x07root@racknerd-2453af7:/opt/omniroute-src# "},
			expect: true,
		},
		{
			name:   "prompt with color codes",
			lines:  []string{"\x1b[01;32muser@host\x1b[00m:\x1b[01;34m~/dir\x1b[00m$ "},
			expect: true,
		},
		{
			name:   "command output - not a prompt",
			lines:  []string{"total 48"},
			expect: false,
		},
		{
			name:   "command echo - not a prompt",
			lines:  []string{"cp -r /opt/data /opt/backup"},
			expect: false,
		},
		{
			name:   "zsh prompt with %",
			lines:  []string{"user@host % "},
			expect: true,
		},
		{
			name:   "fish prompt with >",
			lines:  []string{"user@host ~/dir> "},
			expect: true,
		},
		{
			name:   "empty line",
			lines:  []string{""},
			expect: false,
		},
		{
			name:   "line ending with carriage return",
			lines:  []string{"root@server:/opt# \r"},
			expect: true,
		},
		{
			name:   "single char prompt - bare hash",
			lines:  []string{"# "},
			expect: true,
		},
		{
			name:   "single char prompt - bare dollar",
			lines:  []string{"$ "},
			expect: true,
		},
		{
			name:   "false positive prevention - output ending with dollar",
			lines:  []string{"price is 100$"},
			expect: false,
		},
		{
			name:   "false positive prevention - output ending with hash",
			lines:  []string{"comment #"},
			expect: false,
		},
		{
			name:   "false positive prevention - HTML tag ending with >",
			lines:  []string{"</div>"},
			expect: false,
		},
		{
			name:   "prompt with tilde path",
			lines:  []string{"~$ "},
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeShellPrompt(tt.lines)
			if got != tt.expect {
				t.Errorf("looksLikeShellPrompt(%q) = %v, want %v", tt.lines, got, tt.expect)
			}
		})
	}
}

func TestStripANSIForPromptCheck(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "no escape",
			input:  "root@server:~# ",
			expect: "root@server:~# ",
		},
		{
			name:   "CSI color",
			input:  "\x1b[01;32muser\x1b[00m:\x1b[01;34m~/dir\x1b[00m$ ",
			expect: "user:~/dir$ ",
		},
		{
			name:   "OSC title set",
			input:  "\x1b]0;root@server: /opt\x07root@server:/opt# ",
			expect: "root@server:/opt# ",
		},
		{
			name:   "bracket paste mode",
			input:  "\x1b[?2004hroot@server:~# ",
			expect: "root@server:~# ",
		},
		{
			name:   "combined OSC + CSI",
			input:  "\x1b[?2004h\x1b]0;root@racknerd-2453af7: /opt/omniroute-src\x07root@racknerd-2453af7:/opt/omniroute-src# ",
			expect: "root@racknerd-2453af7:/opt/omniroute-src# ",
		},
		{
			name:   "charset selection escape",
			input:  "\x1b(Broot@server:~# ",
			expect: "root@server:~# ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSIForPromptCheck(tt.input)
			if got != tt.expect {
				t.Errorf("stripANSIForPromptCheck(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}
