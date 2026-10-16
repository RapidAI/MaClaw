package desktop

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
)

func TestContainerPathStaysInsideTheDesktop(t *testing.T) {
	got, err := ContainerPath("~/Desktop/a.c")
	if err != nil || got != "/home/desktop/Desktop/a.c" {
		t.Fatalf("home=%q err=%v", got, err)
	}
	got, err = ContainerPath("src/main.c")
	if err != nil || got != "/home/desktop/src/main.c" {
		t.Fatalf("relative=%q err=%v", got, err)
	}
	got, err = ContainerPath("/root/a c")
	if err != nil || got != "/root/a c" {
		t.Fatalf("space=%q err=%v", got, err)
	}
	for _, bad := range []string{"", `C:\Windows\a.c`, "http://x"} {
		if _, err := ContainerPath(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestFileBytesStayASCIIUntilTheyAreDecoded(t *testing.T) {
	args := FileBytesArgs("maclaw-desktop-1", "/home/desktop/Desktop/北京天气.pdf", FileBytesMax)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, `base64 -w 0`) || !strings.Contains(joined, `head -c`) || args[len(args)-2] != "/home/desktop/Desktop/北京天气.pdf" || args[len(args)-1] != strconv.Itoa(FileBytesMax+1) {
		t.Fatalf("args=%v", args)
	}
	raw := append([]byte("%PDF-1.4\n"), 0xff, 0xfe)
	got, err := DecodeFileBytes(base64.StdEncoding.EncodeToString(raw))
	if err != nil || string(got) != string(raw) {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if _, err := DecodeFileBytes(""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty err=%v", err)
	}
	if _, err := DecodeFileBytes("%%%%"); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("bad err=%v", err)
	}
	tooBig := base64.StdEncoding.EncodeToString(make([]byte, FileBytesMax+1))
	if _, err := DecodeFileBytes(tooBig); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("large err=%v", err)
	}
}

func TestApplyEditTouchesOnePassage(t *testing.T) {
	next, err := ApplyEdit("alpha beta alpha", "beta", "gamma")
	if err != nil || next != "alpha gamma alpha" {
		t.Fatalf("next=%q err=%v", next, err)
	}
	if _, err := ApplyEdit("alpha alpha", "alpha", "b"); err == nil || !strings.Contains(err.Error(), "matched") {
		t.Fatalf("ambiguous err=%v", err)
	}
	if _, err := ApplyEdit("alpha", "missing", "b"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing err=%v", err)
	}
}

func TestBashAndCurlStayInTheContainerArgv(t *testing.T) {
	args, err := BashArgs("maclaw-desktop-1", "echo hi\n")
	if err != nil || args[len(args)-1] != "echo hi" || args[1] != "-w" || args[2] != "/home/desktop" {
		t.Fatalf("bash=%v err=%v", args, err)
	}
	if _, err := BashArgs("maclaw-desktop-1", ""); err == nil {
		t.Fatal("empty command")
	}
	args, err = CurlGetArgs("maclaw-desktop-1", "https://example.com/a", 100)
	if err != nil || args[len(args)-1] != "https://example.com/a" || args[2] != "curl" {
		t.Fatalf("curl=%v err=%v", args, err)
	}
	if _, err := CurlGetArgs("maclaw-desktop-1", "file:///etc/passwd", 100); err == nil {
		t.Fatal("file url")
	}
	body, status, kind := SplitCurl("hello" + curlMarker + "200 text/html")
	if body != "hello" || status != 200 || kind != "text/html" {
		t.Fatalf("body=%q status=%d kind=%q", body, status, kind)
	}
}

func TestDuckDuckGoResultsComeFromThePage(t *testing.T) {
	page := `<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa&amp;rut=1">Example <b>Page</b></a>`
	hits := ParseDuckDuckGo(page)
	if len(hits) != 1 || hits[0].URL != "https://example.com/a" || hits[0].Title != "Example Page" {
		t.Fatalf("hits=%#v", hits)
	}
	text := HTMLText("<style>x</style><p>Hello</p><script>no</script>")
	if strings.Contains(text, "no") || !strings.Contains(text, "Hello") {
		t.Fatalf("text=%q", text)
	}
	if !LooksLikeHTML("text/html", "plain") || !LooksLikeHTML("", "<!DOCTYPE html><p>Hello") {
		t.Fatal("html page was treated as source")
	}
	if LooksLikeHTML("text/plain", "int main(){ if (a <p) return 1; }") {
		t.Fatal("source was treated as html")
	}
	failed := ProgramOutput("a.c:1: error: expected ';'\n", 1, false)
	if !strings.Contains(failed, "expected ';'") || !strings.Contains(failed, "(exit 1)") {
		t.Fatalf("program=%q", failed)
	}
	if ProgramOutput("hi\n", 0, false) != "hi" {
		t.Fatal("success output changed")
	}
}
