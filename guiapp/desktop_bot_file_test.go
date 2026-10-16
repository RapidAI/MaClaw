package guiapp

import "testing"

func TestCleanDesktopBotFilesKeepsAnyType(t *testing.T) {
	data := "d29yZA=="
	got := cleanDesktopBotFiles([]DesktopBotFile{
		{Name: "../secret.pdf", MIME: "application/pdf", Data: data},
		{Name: "北京天气.pdf", MIME: "application/pdf", Data: data},
	})
	if len(got) != 1 || got[0].Name != "北京天气.pdf" || got[0].MIME != "application/pdf" || got[0].Data != data {
		t.Fatalf("pdf=%#v", got)
	}
	got = cleanDesktopBotFiles([]DesktopBotFile{{Name: "Makefile", MIME: "", Data: data}})
	if len(got) != 1 || got[0].Name != "Makefile" || got[0].MIME != "application/octet-stream" || got[0].Data != data {
		t.Fatalf("makefile=%#v", got)
	}
	if got := cleanDesktopBotFiles([]DesktopBotFile{{Name: "note.pdf", MIME: "text/html\r\nX", Data: data}}); got != nil {
		t.Fatalf("hostile mime kept: %#v", got)
	}
}
