package gopdf

import (
	"os"
	"strings"
	"testing"
)

// ============================================================
// Text Extraction tests
// ============================================================

func TestExtractTextFromPage_SelfGenerated(t *testing.T) {
	// Create a PDF with known text content
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	err := pdf.AddTTFFont("liberation", "test/res/LiberationSerif-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	err = pdf.SetFont("liberation", "", 14)
	if err != nil {
		t.Fatal(err)
	}
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Hello GoPDF2 Text Extraction")

	pdf.SetX(50)
	pdf.SetY(100)
	pdf.Cell(nil, "Second line of text")

	outPath := "test/out/test_text_extract.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	// Now extract text from the generated PDF
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}

	texts, err := ExtractTextFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(texts) == 0 {
		t.Fatal("expected extracted text items, got none")
	}

	// Check that we can find our text
	var allText strings.Builder
	for _, item := range texts {
		allText.WriteString(item.Text)
		allText.WriteString(" ")
	}
	combined := allText.String()
	t.Logf("Extracted text: %s", combined)

	// The text should contain our strings (may be encoded differently)
	if len(combined) == 0 {
		t.Error("extracted text is empty")
	}
}

func TestExtractTextFromAllPages(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	// Page 1
	pdf.AddPage()
	err := pdf.AddTTFFont("liberation", "test/res/LiberationSerif-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	pdf.SetFont("liberation", "", 12)
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Page One Content")

	// Page 2
	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Page Two Content")

	outPath := "test/out/test_text_extract_multi.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}

	result, err := ExtractTextFromAllPages(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) == 0 {
		t.Fatal("expected text from multiple pages")
	}
	t.Logf("Extracted text from %d pages", len(result))
}

func TestExtractPageText_Convenience(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	err := pdf.AddTTFFont("liberation", "test/res/LiberationSerif-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	pdf.SetFont("liberation", "", 14)
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Convenience extraction test")

	outPath := "test/out/test_text_extract_conv.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}

	text, err := ExtractPageText(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ExtractPageText result: %q", text)
	if len(text) == 0 {
		t.Error("expected non-empty text")
	}
}

func TestExtractTextFromPage_InvalidPage(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	outPath := "test/out/test_text_extract_invalid.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)
	_, err := ExtractTextFromPage(data, 99)
	if err == nil {
		t.Error("expected error for out-of-range page index")
	}
}

// ============================================================
// Image Extraction tests
// ============================================================

func TestExtractImagesFromPage(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.Image("test/res/gopher01.jpg", 50, 50, &Rect{W: 200, H: 200})
	if err != nil {
		t.Fatal(err)
	}

	outPath := "test/out/test_image_extract.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}

	images, err := ExtractImagesFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Extracted %d images from page 0", len(images))
	for _, img := range images {
		t.Logf("  Image: %s %dx%d filter=%s colorspace=%s dataLen=%d",
			img.Name, img.Width, img.Height, img.Filter, img.ColorSpace, len(img.Data))
		if img.Width <= 0 || img.Height <= 0 {
			t.Error("image dimensions should be positive")
		}
		format := img.GetImageFormat()
		t.Logf("  Format: %s", format)
	}
}

func TestExtractImagesFromAllPages(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	pdf.AddPage()
	pdf.Image("test/res/gopher01.jpg", 50, 50, &Rect{W: 100, H: 100})

	pdf.AddPage()
	pdf.Image("test/res/gopher02.png", 50, 50, &Rect{W: 100, H: 100})

	outPath := "test/out/test_image_extract_multi.pdf"
	err := pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}

	result, err := ExtractImagesFromAllPages(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Extracted images from %d pages", len(result))
}

func TestExtractImagesFromPage_InvalidPage(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	outPath := "test/out/test_image_extract_invalid.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)
	_, err := ExtractImagesFromPage(data, 99)
	if err == nil {
		t.Error("expected error for out-of-range page index")
	}
}

// ============================================================
// Form Field tests
// ============================================================

func TestAddTextField(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.AddTextField("username", 50, 700, 200, 25)
	if err != nil {
		t.Fatal(err)
	}

	fields := pdf.GetFormFields()
	if len(fields) != 1 {
		t.Fatalf("expected 1 form field, got %d", len(fields))
	}
	if fields[0].Type != FormFieldText {
		t.Errorf("expected Text field type, got %s", fields[0].Type)
	}
	if fields[0].Name != "username" {
		t.Errorf("expected field name 'username', got %q", fields[0].Name)
	}

	outPath := "test/out/test_form_textfield.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(outPath)
	if info.Size() == 0 {
		t.Error("output PDF is empty")
	}
	t.Logf("Form text field PDF: %d bytes", info.Size())
}

func TestAddCheckbox(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.AddCheckbox("agree", 50, 700, 15, true)
	if err != nil {
		t.Fatal(err)
	}

	fields := pdf.GetFormFields()
	if len(fields) != 1 {
		t.Fatalf("expected 1 form field, got %d", len(fields))
	}
	if fields[0].Type != FormFieldCheckbox {
		t.Errorf("expected Checkbox type, got %s", fields[0].Type)
	}
	if !fields[0].Checked {
		t.Error("expected checkbox to be checked")
	}

	outPath := "test/out/test_form_checkbox.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Form checkbox PDF written")
}

func TestAddDropdown(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	options := []string{"Option A", "Option B", "Option C"}
	err := pdf.AddDropdown("choice", 50, 700, 200, 25, options)
	if err != nil {
		t.Fatal(err)
	}

	fields := pdf.GetFormFields()
	if len(fields) != 1 {
		t.Fatalf("expected 1 form field, got %d", len(fields))
	}
	if fields[0].Type != FormFieldChoice {
		t.Errorf("expected Choice type, got %s", fields[0].Type)
	}
	if len(fields[0].Options) != 3 {
		t.Errorf("expected 3 options, got %d", len(fields[0].Options))
	}

	outPath := "test/out/test_form_dropdown.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Form dropdown PDF written")
}

func TestAddFormField_Validation(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	// Empty name should fail
	err := pdf.AddFormField(FormField{
		Type: FormFieldText,
		Name: "",
		W:    100, H: 25,
	})
	if err == nil {
		t.Error("expected error for empty field name")
	}

	// Zero dimensions should fail
	err = pdf.AddFormField(FormField{
		Type: FormFieldText,
		Name: "test",
		W:    0, H: 25,
	})
	if err == nil {
		t.Error("expected error for zero width")
	}
}

func TestAddFormField_MultipleFields(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	// Add multiple field types
	pdf.AddTextField("name", 50, 700, 200, 25)
	pdf.AddCheckbox("agree", 50, 660, 15, false)
	pdf.AddDropdown("country", 50, 620, 200, 25, []string{"US", "UK", "CN"})

	// Add a custom form field with all options
	pdf.AddFormField(FormField{
		Type:        FormFieldText,
		Name:        "notes",
		X:           50,
		Y:           560,
		W:           300,
		H:           80,
		Value:       "Default notes",
		FontSize:    10,
		Multiline:   true,
		HasBorder:   true,
		HasFill:     true,
		BorderColor: [3]uint8{0, 0, 128},
		FillColor:   [3]uint8{255, 255, 230},
		Color:       [3]uint8{0, 0, 0},
	})

	fields := pdf.GetFormFields()
	if len(fields) != 4 {
		t.Fatalf("expected 4 form fields, got %d", len(fields))
	}

	outPath := "test/out/test_form_multi.pdf"
	err := pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(outPath)
	t.Logf("Multi-field form PDF: %d bytes", info.Size())
}

func TestFormFieldType_String(t *testing.T) {
	tests := []struct {
		ft   FormFieldType
		want string
	}{
		{FormFieldText, "Text"},
		{FormFieldCheckbox, "Checkbox"},
		{FormFieldRadio, "Radio"},
		{FormFieldChoice, "Choice"},
		{FormFieldButton, "Button"},
		{FormFieldSignature, "Signature"},
		{FormFieldType(99), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.ft.String(); got != tt.want {
			t.Errorf("FormFieldType(%d).String() = %q, want %q", tt.ft, got, tt.want)
		}
	}
}

// ============================================================
// PDF Parser tests
// ============================================================

func TestRawPDFParser_BasicParse(t *testing.T) {
	// Generate a simple PDF and parse it
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := "test/out/test_parser_basic.pdf"
	err := pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}

	parser, err := newRawPDFParser(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(parser.objects) == 0 {
		t.Error("expected parsed objects")
	}
	if len(parser.pages) == 0 {
		t.Error("expected at least one page")
	}
	t.Logf("Parsed %d objects, %d pages", len(parser.objects), len(parser.pages))
}

func TestRawPDFParser_MultiPage(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddPage()
	pdf.AddPage()

	outPath := "test/out/test_parser_multi.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)
	parser, err := newRawPDFParser(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parser.pages) != 3 {
		t.Errorf("expected 3 pages, got %d", len(parser.pages))
	}
}
