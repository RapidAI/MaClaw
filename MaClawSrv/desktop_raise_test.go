package main

import "testing"

func TestDesktopBashLaunchesGUI(t *testing.T) {
	yes := []string{"soffice ~/Desktop/a.docx &", "nohup lowriter x.docx >/dev/null 2>&1 &", "cd ~ && evince a.pdf &", "xdg-open ~/Desktop", "gio open a.txt"}
	no := []string{"soffice --headless --convert-to pdf a.docx", "ls ~/Desktop", "python3 make_doc.py", "cat libreoffice.log", "echo mousepadding"}
	for _, c := range yes {
		if !desktopBashLaunchesGUI(c) {
			t.Errorf("expected GUI launch: %q", c)
		}
	}
	for _, c := range no {
		if desktopBashLaunchesGUI(c) {
			t.Errorf("unexpected GUI launch: %q", c)
		}
	}
}
