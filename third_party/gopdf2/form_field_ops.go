package gopdf

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// ============================================================
// Form Field Operations — delete, modify, detect, bake
// ============================================================

// DeleteFormField removes a form field by name from the document.
// Returns an error if the field is not found.
//
// Example:
//
//	err := pdf.DeleteFormField("username")
func (gp *GoPdf) DeleteFormField(name string) error {
	idx := -1
	for i, ref := range gp.formFields {
		if ref.field.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("form field %q not found", name)
	}

	ref := gp.formFields[idx]

	// Null out the PDF object.
	if ref.objIdx >= 0 && ref.objIdx < len(gp.pdfObjs) {
		gp.pdfObjs[ref.objIdx] = nullObj{}
	}

	// Remove from page's annotation list.
	objID := ref.objIdx + 1
	for _, obj := range gp.pdfObjs {
		if page, ok := obj.(*PageObj); ok {
			for j, id := range page.LinkObjIds {
				if id == objID {
					page.LinkObjIds = append(page.LinkObjIds[:j], page.LinkObjIds[j+1:]...)
					break
				}
			}
		}
	}

	// Remove from formFields slice.
	gp.formFields = append(gp.formFields[:idx], gp.formFields[idx+1:]...)

	return nil
}

// ModifyFormFieldValue updates the value of an existing form field by name.
//
// Example:
//
//	err := pdf.ModifyFormFieldValue("username", "John Doe")
func (gp *GoPdf) ModifyFormFieldValue(name, value string) error {
	for i, ref := range gp.formFields {
		if ref.field.Name == name {
			gp.formFields[i].field.Value = value

			// Update the underlying PDF object.
			if ref.objIdx >= 0 && ref.objIdx < len(gp.pdfObjs) {
				if ffObj, ok := gp.pdfObjs[ref.objIdx].(formFieldObj); ok {
					ffObj.field.Value = value
					gp.pdfObjs[ref.objIdx] = ffObj
				}
			}
			return nil
		}
	}
	return fmt.Errorf("form field %q not found", name)
}

// IsFormPDF detects whether the given PDF data contains form fields (AcroForm).
// This is a standalone function that operates on raw PDF bytes.
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	if gopdf.IsFormPDF(data) {
//	    fmt.Println("This PDF contains form fields")
//	}
func IsFormPDF(pdfData []byte) bool {
	// Look for /AcroForm in the catalog or anywhere in the PDF.
	return bytes.Contains(pdfData, []byte("/AcroForm")) &&
		(bytes.Contains(pdfData, []byte("/Fields")) ||
			bytes.Contains(pdfData, []byte("/FT ")))
}

// BakeAnnotations flattens all annotations and form fields into the page
// content streams, making them non-interactive. This is similar to
// PyMuPDF's bake() function.
//
// After baking, form fields become static content and can no longer be edited.
//
// Example:
//
//	pdf.BakeAnnotations()
//	pdf.WritePdf("baked.pdf")
func (gp *GoPdf) BakeAnnotations() {
	// For each page, convert annotations to static drawing commands.
	for _, obj := range gp.pdfObjs {
		page, ok := obj.(*PageObj)
		if !ok {
			continue
		}

		var bakedContent strings.Builder

		for _, annotID := range page.LinkObjIds {
			objIdx := annotID - 1
			if objIdx < 0 || objIdx >= len(gp.pdfObjs) {
				continue
			}

			switch a := gp.pdfObjs[objIdx].(type) {
			case formFieldObj:
				// Bake form field as static text.
				bakedContent.WriteString(bakeFormField(a))
				gp.pdfObjs[objIdx] = nullObj{}
			case annotationObj:
				// Bake annotation as static drawing.
				bakedContent.WriteString(bakeAnnotation(a))
				gp.pdfObjs[objIdx] = nullObj{}
			}
		}

		if bakedContent.Len() > 0 {
			// Append baked content to the page's content stream.
			gp.appendContentToPage(page, bakedContent.String())
		}

		// Clear the annotation list.
		page.LinkObjIds = nil
	}

	// Clear form fields.
	gp.formFields = nil
}

// bakeFormField converts a form field to static PDF drawing commands.
func bakeFormField(f formFieldObj) string {
	field := f.field
	var buf strings.Builder

	buf.WriteString("q\n")

	// Draw background if filled.
	if field.HasFill {
		fmt.Fprintf(&buf, "%.4f %.4f %.4f rg\n",
			float64(field.FillColor[0])/255,
			float64(field.FillColor[1])/255,
			float64(field.FillColor[2])/255)
		fmt.Fprintf(&buf, "%.2f %.2f %.2f %.2f re f\n",
			field.X, field.Y, field.W, field.H)
	}

	// Draw border if present.
	if field.HasBorder {
		fmt.Fprintf(&buf, "%.4f %.4f %.4f RG\n",
			float64(field.BorderColor[0])/255,
			float64(field.BorderColor[1])/255,
			float64(field.BorderColor[2])/255)
		fmt.Fprintf(&buf, "1 w\n")
		fmt.Fprintf(&buf, "%.2f %.2f %.2f %.2f re S\n",
			field.X, field.Y, field.W, field.H)
	}

	// Draw text value.
	if field.Value != "" && (field.Type == FormFieldText || field.Type == FormFieldChoice) {
		fontSize := field.FontSize
		if fontSize == 0 {
			fontSize = 12
		}
		fmt.Fprintf(&buf, "BT\n")
		fmt.Fprintf(&buf, "%.4f %.4f %.4f rg\n",
			float64(field.Color[0])/255,
			float64(field.Color[1])/255,
			float64(field.Color[2])/255)
		fmt.Fprintf(&buf, "/Helv %.1f Tf\n", fontSize)
		fmt.Fprintf(&buf, "%.2f %.2f Td\n", field.X+2, field.Y+field.H-fontSize-2)
		fmt.Fprintf(&buf, "(%s) Tj\n", escapeAnnotString(field.Value))
		fmt.Fprintf(&buf, "ET\n")
	}

	// Draw checkmark for checked checkboxes.
	if field.Type == FormFieldCheckbox && field.Checked {
		cx := field.X + field.W*0.2
		cy := field.Y + field.H*0.2
		fmt.Fprintf(&buf, "0 0 0 RG\n2 w\n")
		fmt.Fprintf(&buf, "%.2f %.2f m %.2f %.2f l S\n",
			cx, cy+field.H*0.3, cx+field.W*0.3, cy)
		fmt.Fprintf(&buf, "%.2f %.2f m %.2f %.2f l S\n",
			cx+field.W*0.3, cy, cx+field.W*0.6, cy+field.H*0.6)
	}

	buf.WriteString("Q\n")
	return buf.String()
}

// bakeAnnotation converts an annotation to static PDF drawing commands.
func bakeAnnotation(a annotationObj) string {
	opt := a.opt
	var buf strings.Builder

	buf.WriteString("q\n")

	// Draw annotation rectangle with color.
	r := float64(opt.Color[0]) / 255
	g := float64(opt.Color[1]) / 255
	b := float64(opt.Color[2]) / 255
	fmt.Fprintf(&buf, "%.4f %.4f %.4f RG\n", r, g, b)
	fmt.Fprintf(&buf, "1 w\n")
	fmt.Fprintf(&buf, "%.2f %.2f %.2f %.2f re S\n",
		opt.X, opt.Y, opt.W, opt.H)

	// Draw content text if present.
	if opt.Content != "" {
		fmt.Fprintf(&buf, "BT\n")
		fontSize := opt.FontSize
		if fontSize == 0 {
			fontSize = 10
		}
		fmt.Fprintf(&buf, "%.4f %.4f %.4f rg\n", r, g, b)
		fmt.Fprintf(&buf, "/Helv %.1f Tf\n", fontSize)
		fmt.Fprintf(&buf, "%.2f %.2f Td\n", opt.X+2, opt.Y+opt.H-fontSize-2)
		fmt.Fprintf(&buf, "(%s) Tj\n", escapeAnnotString(opt.Content))
		fmt.Fprintf(&buf, "ET\n")
	}

	buf.WriteString("Q\n")
	return buf.String()
}

// appendContentToPage appends raw content stream data to a page.
func (gp *GoPdf) appendContentToPage(page *PageObj, content string) {
	// Create a new content object with the baked content.
	contentObj := new(ContentObj)
	contentObj.init(func() *GoPdf { return gp })
	// Use the raw cache to inject pre-built content stream commands.
	contentObj.listCache.append(&cacheContentRaw{data: content})
	idx := gp.addObj(contentObj)
	page.Contents = fmt.Sprintf("%s %d 0 R ", page.Contents, idx+1)
}

// cacheContentRaw is a cache item that writes raw PDF content stream data.
type cacheContentRaw struct {
	data string
}

func (c *cacheContentRaw) write(w io.Writer, protection *PDFProtection) error {
	_, err := io.WriteString(w, c.data)
	return err
}


// FlattenFormFields bakes the current form field values into the page
// content as static text and removes the interactive widgets. Text and
// dropdown fields draw their value; checkboxes and radios draw an "X" when
// set. After flattening the document has no AcroForm fields.
//
// Example:
//
//	pdf.ModifyFormFieldValue("name", "Jane Doe")
//	pdf.FlattenFormFields()
//	pdf.WritePdf("flattened.pdf")
func (gp *GoPdf) FlattenFormFields() error {
	if len(gp.formFields) == 0 {
		return nil
	}

	// map page object ids to 1-based page numbers and objects
	pageNum := map[int]int{}
	pageObjs := map[int]*PageObj{}
	num := 0
	for i, obj := range gp.pdfObjs {
		if p, ok := obj.(*PageObj); ok {
			num++
			pageNum[i+1] = num
			pageObjs[i+1] = p
		}
	}

	// remember the caller's page for restoring afterwards
	savedPageNo := 0
	if no, ok := pageNum[gp.curr.IndexOfPageObj+1]; ok {
		savedPageNo = no
	}

	for _, ref := range gp.formFields {
		ffObj, ok := gp.pdfObjs[ref.objIdx].(formFieldObj)
		if !ok {
			continue
		}
		pNo, ok := pageNum[ffObj.pageRef]
		if !ok {
			continue
		}
		// remove the widget from the page's annotation list
		if page, ok2 := pageObjs[ffObj.pageRef]; ok2 {
			kept := page.LinkObjIds[:0]
			for _, id := range page.LinkObjIds {
				if id != ref.objIdx+1 {
					kept = append(kept, id)
				}
			}
			page.LinkObjIds = kept
		}
		// bake the value as static text
		if strings.TrimSpace(ffObj.field.Value) == "" {
			continue
		}
		if err := gp.SetPage(pNo); err != nil {
			return err
		}
		gp.SetXY(ffObj.field.X, ffObj.field.Y)
		if ffObj.field.FontFamily != "" {
			_ = gp.SetFont(ffObj.field.FontFamily, "", ffObj.field.FontSize)
		} else if ffObj.field.FontSize > 0 {
			gp.curr.FontSize = ffObj.field.FontSize
		}
		text := ffObj.field.Value
		if ffObj.field.Type == FormFieldCheckbox || ffObj.field.Type == FormFieldRadio {
			text = "X"
		}
		if text == "" {
			continue
		}
		if strings.Contains(text, "\n") {
			// multi-line values wrap inside the field box
			_ = gp.MultiCellWithOption(&Rect{W: ffObj.field.W, H: ffObj.field.H}, text, CellOption{Align: Left | Top})
		} else {
			_ = gp.CellWithOption(&Rect{W: ffObj.field.W, H: ffObj.field.H}, text, CellOption{Align: Left | Top})
		}
	}

	// restore the caller's page
	if savedPageNo > 0 {
		_ = gp.SetPage(savedPageNo)
	}

	// drop the interactive fields and the AcroForm field references
	gp.formFields = nil
	for i, obj := range gp.pdfObjs {
		if af, ok := obj.(acroFormObj); ok {
			af.fieldObjIDs = nil
			gp.pdfObjs[i] = af
		}
	}
	return nil
}
