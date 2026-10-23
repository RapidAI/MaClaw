package agent

import (
	"strings"
	"testing"
)

func TestCommandBypassesTerminalDocument(t *testing.T) {
	records := []TerminalExtract{{
		Path:   `C:\Users\ma139\Desktop\信息安全数学基础.pdf`,
		Format: "pdf",
		Class:  "input_too_large",
	}}
	bypass := []string{
		`pip install rapidocr-onnxruntime --quiet`,
		`python -m pip install paddleocr`,
		`where tesseract`,
		`python -c "import pymupdf; pymupdf.open(r'C:\Users\ma139\Desktop\信息安全数学基础.pdf')"`,
		`python -c "import fitz; fitz.open('C:/Users/ma139/Desktop/信息安全数学基础.pdf')"`,
		`py -c "import fitz; fitz.open(r'C:\Users\ma139\Desktop\信息安全数学基础.pdf')"`,
		`py.exe -c "open(r'C:\Users\ma139\Desktop\信息安全数学基础.pdf')"`,
		`pythonw -c "open(r'C:\Users\ma139\Desktop\信息安全数学基础.pdf')"`,
		`pyw -c "open(r'C:\Users\ma139\Desktop\信息安全数学基础.pdf')"`,
		`gswin64c -sDEVICE=pdfwrite "C:\Users\ma139\Desktop\信息安全数学基础.pdf"`,
	}
	for _, command := range bypass {
		if !CommandBypassesTerminalDocument(command, records) {
			t.Fatalf("command must be a bypass: %s", command)
		}
	}
	allowed := []string{
		`python -c "print(1)"`,
		`ls "C:/Users/ma139/Desktop/信息安全数学基础.pdf"`,
		`pip install requests`,
		`echo pipeline "C:\Users\ma139\Desktop\信息安全数学基础.pdf"`,
		`pytest "C:\Users\ma139\Desktop\信息安全数学基础.pdf"`,
		`copy notes.py "C:\Users\ma139\Desktop\信息安全数学基础.pdf"`,
		`copy notes.pyw "C:\Users\ma139\Desktop\信息安全数学基础.pdf"`,
	}
	for _, command := range allowed {
		if CommandBypassesTerminalDocument(command, records) {
			t.Fatalf("command must stay allowed: %s", command)
		}
	}
	if CommandBypassesTerminalDocument(`pip install rapidocr-onnxruntime`, nil) {
		t.Fatal("no terminal extract must not block an OCR install")
	}
	pyDir := []TerminalExtract{{
		Path:   `C:\py\scan.pdf`,
		Format: "pdf",
		Class:  "input_too_large",
	}}
	if CommandBypassesTerminalDocument(`dir "C:\py\scan.pdf"`, pyDir) {
		t.Fatal("dir of a path that contains py must stay allowed")
	}
	if !CommandBypassesTerminalDocument(`py -c "open(r'C:\py\scan.pdf')"`, pyDir) {
		t.Fatal("py launcher opening that path must be a bypass")
	}
}

func TestToolCallBypassesTerminalDocument(t *testing.T) {
	text := AutoExtractBeginMarker + `path="C:\\docs\\scan.pdf" format="pdf" error="自动注入已跳过（error_class=input_too_large）。" ---` + "\n文件超过 32 MiB\n"
	records := AutoExtractTerminalRecords(text)
	if len(records) != 1 || !TerminalExtractOffersBuiltinOCR(records) {
		t.Fatalf("records = %#v", records)
	}
	denial := ToolCallBypassesTerminalDocument("bash", `{"command":"pip install rapidocr-onnxruntime"}`, records)
	if denial == "" || !strings.Contains(denial, "ocr_recognize") || !strings.Contains(denial, "PP-OCRv6") || !strings.Contains(denial, "input_too_large") {
		t.Fatalf("bash install denial = %q", denial)
	}
	if got := ToolCallBypassesTerminalDocument("ocr_recognize", `{"image_path":"C:\\docs\\scan.pdf","output_path":"out.txt"}`, records); got != "" {
		t.Fatalf("builtin OCR must stay allowed, got %q", got)
	}
	if got := ToolCallBypassesTerminalDocument("bash", `{"command":"python -V"}`, records); got != "" {
		t.Fatalf("unrelated python must stay allowed, got %q", got)
	}
	if got := ToolCallBypassesTerminalDocument("office", `{"action":"read_document","file_path":"C:\\docs\\scan.pdf"}`, records); got == "" {
		t.Fatal("office reread of the refused file must be denied")
	}
	if got := ToolCallBypassesTerminalDocument("office", `{"action":"read_document"}`, records); got == "" {
		t.Fatal("office read with no path must be denied")
	}
	if got := ToolCallBypassesTerminalDocument("office", `{"action":"generate_pdf"}`, records); got != "" {
		t.Fatalf("generate_pdf with no path must stay allowed, got %q", got)
	}
	if got := ToolCallBypassesTerminalDocument("office", `{"action":"write_excel"}`, records); got != "" {
		t.Fatalf("write_excel with no path must stay allowed, got %q", got)
	}
	if got := ToolCallBypassesTerminalDocument("office", `{"action":"generate_pdf","file_path":"C:\\docs\\out.pdf"}`, records); got != "" {
		t.Fatalf("generate_pdf to a different path must stay allowed, got %q", got)
	}
	if got := ToolCallBypassesTerminalDocument("read_file", `{"path":"C:\\other\\notes.txt"}`, records); got != "" {
		t.Fatalf("a different file must stay readable, got %q", got)
	}
	encrypted := AutoExtractTerminalRecords(AutoExtractBeginMarker + ` path="C:\a.docx" format="docx" error="自动注入已跳过（error_class=encrypted）。" ---`)
	denial = ToolCallBypassesTerminalDocument("bash", `{"command":"pip install rapidocr-onnxruntime"}`, encrypted)
	if denial == "" || strings.Contains(denial, "ocr_recognize") {
		t.Fatalf("encrypted denial must not offer scan OCR, got %q", denial)
	}
}

func TestOCRImagePDFPageBatchRejectsEmpty(t *testing.T) {
	_, err := OCRImagePDFPageBatch(nil, 1, 8, 0, func(string) (string, error) { return "x", nil })
	if err == nil {
		t.Fatal("empty pdf must fail")
	}
}

func TestOCRTranscriptMaxPage(t *testing.T) {
	text := "## Page 1\n甲\n\n## Page 8\n见 ## Page 40\n\n## Page 9\n乙\n"
	if got := OCRTranscriptMaxPage(text); got != 9 {
		t.Fatalf("max page = %d, want 9", got)
	}
	if got := OCRTranscriptMaxPage("没有页眉"); got != 0 {
		t.Fatalf("text without a header = %d, want 0", got)
	}
	if got := OCRTranscriptMaxPage("正文里写了 ## Page 12 但不是页眉\n"); got != 0 {
		t.Fatalf("a mention inside a paragraph = %d, want 0", got)
	}
	if got := OCRTranscriptFirstMissing(text); got != 2 {
		t.Fatalf("gap between page 1 and 8 = %d, want 2", got)
	}
	contiguous := "## Page 1\n甲\n\n## Page 2\n乙\n"
	if got := OCRTranscriptFirstMissing(contiguous); got != 3 {
		t.Fatalf("contiguous transcript = %d, want 3", got)
	}
	broken := "## Page 1\n甲\n\n## Page 2\n[page extract error]\n\n## Page 4\n乙\n"
	if OCRTranscriptPageDone(broken, 2) {
		t.Fatal("an extract-error page is not done")
	}
	if got := OCRTranscriptFirstMissing(broken); got != 3 {
		t.Fatalf("error page must not hide the gap, got %d", got)
	}
	merged := MergeOCRTranscript("## Page 1\n甲\n\n## Page 8\n乙\n\n## Page 1\n重复\n", "## Page 2\n丙\n\n## Page 8\n新\n")
	if merged != "## Page 1\n甲\n\n## Page 2\n丙\n\n## Page 8\n乙" {
		t.Fatalf("merged = %q", merged)
	}
	replaced := MergeOCRTranscript("## Page 3\n[page extract error]", "## Page 3\n丙")
	if replaced != "## Page 3\n丙" {
		t.Fatalf("replaced = %q", replaced)
	}
	book := "## Page 1\n甲\n\n## Page 2\n乙\n\n## Page 17\n庚\n"
	if got := OCRTranscriptNextDonePage(book, 9); got != 17 {
		t.Fatalf("next done page after the gap = %d, want 17", got)
	}
	if got := OCRTranscriptNextDonePage(book, 17); got != 0 {
		t.Fatalf("no finished page after 17, got %d", got)
	}
}
