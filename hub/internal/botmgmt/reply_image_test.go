package botmgmt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReplyImageKeepsTheLatestScreenshot(t *testing.T) {
	png := "iVBORw0KGgo="
	jpeg := "/9j/4AAQSkZJRg=="
	got := replyImages([]replyAttachment{
		{Type: "file", MimeType: "image/png", Data: png},
		{Type: "image", MimeType: "text/html", Data: png},
		{Type: "image", MimeType: "image/png", Data: png},
		{Type: "image", MimeType: "image/jpeg", Data: jpeg},
	})
	if len(got) != 1 || got[0].MIME != "image/jpeg" || got[0].Data != jpeg {
		t.Fatalf("images=%#v", got)
	}
	if got := replyImages([]replyAttachment{{Type: "image", MimeType: "image/png", Data: "not base64"}}); got != nil {
		t.Fatalf("invalid screenshot was kept: %#v", got)
	}
	if got := replyImages([]replyAttachment{{Type: "image", MimeType: "image/png", Data: "iVB=Rw0KGgo="}}); got != nil {
		t.Fatalf("broken padding was kept: %#v", got)
	}
	if got := replyImages(nil); got != nil {
		t.Fatalf("empty reply grew an image: %#v", got)
	}
}

func TestReplyFileKeepsTheLatestDocument(t *testing.T) {
	docx := "UEsDBAoAAAAA"
	got := replyFiles([]replyAttachment{
		{Type: "file", FileName: "../secret.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Data: docx},
		{Type: "image", FileName: "note.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Data: docx},
		{Type: "file", FileName: "note.docx", MimeType: "application/pdf", Data: docx},
		{Type: "file", FileName: "自我描述.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Data: docx},
	})
	if len(got) != 1 || got[0].Name != "自我描述.docx" || got[0].Data != docx {
		t.Fatalf("files=%#v", got)
	}
	if got := replyFiles([]replyAttachment{{Type: "file", FileName: "a.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Data: "not base64"}}); got != nil {
		t.Fatalf("invalid document was kept: %#v", got)
	}
	pdf := "JVBERi0x"
	got = replyFiles([]replyAttachment{
		{Type: "file", FileName: "../secret.pdf", MimeType: "application/pdf", Data: pdf},
		{Type: "file", FileName: "北京天气.pdf", MimeType: "application/pdf", Data: pdf},
	})
	if len(got) != 1 || got[0].Name != "北京天气.pdf" || got[0].MIME != "application/pdf" || got[0].Data != pdf {
		t.Fatalf("pdf=%#v", got)
	}
	got = replyFiles([]replyAttachment{{Type: "file", FileName: "Makefile", MimeType: "", Data: pdf}})
	if len(got) != 1 || got[0].Name != "Makefile" || got[0].MIME != "application/octet-stream" {
		t.Fatalf("makefile=%#v", got)
	}
}

func TestMessageResponseReadsAScreenshotPastOneMebibyte(t *testing.T) {
	shot := strings.Repeat("A", 1_100_000)
	body := `{"text":"桌面截图已生成","images":[{"mime":"image/png","data":"` + shot + `"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	svc := NewService(&memSettings{})
	svc.HTTP = srv.Client()
	rec := record{BaseURL: srv.URL}
	var dest struct {
		Text   string       `json:"text"`
		Images []ReplyImage `json:"images"`
	}
	if err := svc.call(context.Background(), rec, "", "", http.MethodPost, "/api/v1/instances/inst/messages", nil, &dest); err != nil {
		t.Fatal(err)
	}
	if dest.Text != "桌面截图已生成" || len(dest.Images) != 1 || len(dest.Images[0].Data) != len(shot) {
		t.Fatalf("text=%q images=%d", dest.Text, len(dest.Images))
	}
	if err := svc.call(context.Background(), rec, "", "", http.MethodGet, "/api/v1/instances/inst", nil, &dest); err == nil {
		t.Fatal("a non-message response larger than 1MiB was accepted")
	}
}
