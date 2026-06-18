package main

import (
	"os"
	"strings"
	"testing"
)

func TestBashAutoSpillEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want bool
	}{
		{"simple python -c", `python -c "print(1)"`, true},
		{"python3 -c", `python3 -c "print(1)"`, true},
		{"prefixed", `pip install x && python -c "print(1)"`, true},
		{"python -X utf8 -c", `python -X utf8 -c "print(1)"`, true},
		{"python -u -c", `python -u -c "print(1)"`, true},
		{"python without -c", `python script.py`, false},
		{"node command", `node -e "console.log(1)"`, false},
		{"empty", ``, false},
		{"python -c inside double quotes", `echo "use python -c for inline"`, false},
		{"python -c inside single quotes", `echo 'try python -c inline'`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bashCommandIsAutoSpillable(tc.cmd)
			if got != tc.want {
				t.Errorf("bashCommandIsAutoSpillable(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestAutoSpillChineseContent(t *testing.T) {
	dir := t.TempDir()
	script := "from docx import Document\ndoc = Document()\ndoc.add_paragraph('一种文件类型智能识别装置')\ndoc.save('claims.docx')"
	command := `python -c "` + script + `"`
	result, err := autoSpillPythonScript(command, dir)
	if err != nil {
		t.Fatalf("autoSpillPythonScript with Chinese: %v", err)
	}
	if result == nil || result.Command == "" {
		t.Fatal("spilled command is empty")
	}
	defer os.Remove(result.TempFile)
	if !strings.Contains(result.Command, "python") || !strings.Contains(result.Command, ".py") {
		t.Fatalf("spilled command doesn't look right: %q", result.Command)
	}
}

func TestAutoSpillNoQuoteCommand(t *testing.T) {
	dir := t.TempDir()
	command := `python -c print(42)`
	result, err := autoSpillPythonScript(command, dir)
	if err != nil {
		t.Fatalf("autoSpillPythonScript no-quote: %v", err)
	}
	if result == nil || result.Command == "" {
		t.Fatal("spilled command is empty")
	}
	defer os.Remove(result.TempFile)
}
