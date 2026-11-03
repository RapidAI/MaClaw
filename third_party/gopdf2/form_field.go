package gopdf

import (
	"fmt"
	"io"
)

// ============================================================
// PDF Form Fields (AcroForm / Widgets)
// ============================================================

// FormFieldType represents the type of a PDF form field.
type FormFieldType int

const (
	// FormFieldText is a text input field.
	FormFieldText FormFieldType = iota
	// FormFieldCheckbox is a checkbox field.
	FormFieldCheckbox
	// FormFieldRadio is a radio button field.
	FormFieldRadio
	// FormFieldChoice is a dropdown/list field.
	FormFieldChoice
	// FormFieldButton is a push button field.
	FormFieldButton
	// FormFieldSignature is a signature field.
	FormFieldSignature
)

// String returns the field type name.
func (ft FormFieldType) String() string {
	switch ft {
	case FormFieldText:
		return "Text"
	case FormFieldCheckbox:
		return "Checkbox"
	case FormFieldRadio:
		return "Radio"
	case FormFieldChoice:
		return "Choice"
	case FormFieldButton:
		return "Button"
	case FormFieldSignature:
		return "Signature"
	default:
		return "Unknown"
	}
}

// FormField defines a form field to be added to a PDF page.
type FormField struct {
	// Type is the field type.
	Type FormFieldType
	// Name is the field name (unique identifier).
	Name string
	// X, Y are the top-left corner position.
	X, Y float64
	// W, H are the width and height.
	W, H float64
	// Value is the default value.
	Value string
	// FontFamily is the font family for text fields (must be pre-loaded).
	FontFamily string
	// FontSize is the font size for text fields (default 12).
	FontSize float64
	// Options is a list of choices for Choice fields.
	Options []string
	// MaxLen is the maximum character length for text fields (0 = unlimited).
	MaxLen int
	// Multiline enables multi-line text input.
	Multiline bool
	// ReadOnly makes the field non-editable.
	ReadOnly bool
	// Required marks the field as required.
	Required bool
	// Color is the text color [R, G, B] (0-255).
	Color [3]uint8
	// BorderColor is the border color [R, G, B] (0-255).
	BorderColor [3]uint8
	// FillColor is the background fill color [R, G, B] (0-255).
	FillColor [3]uint8
	// HasBorder controls whether a border is drawn.
	HasBorder bool
	// HasFill controls whether a background fill is drawn.
	HasFill bool
	// Checked is the initial state for checkboxes.
	Checked bool
	// PageNo is the 1-based page number (set internally).
	pageNo int
}

// formFieldObj is the PDF widget annotation + field object.
type formFieldObj struct {
	field    FormField
	pageRef  int // page object ID (1-based)
	fontRef  string // font resource name like "/F1"
	fontObjID int  // font object ID (1-based)
}

func (f formFieldObj) init(fn func() *GoPdf) {}

func (f formFieldObj) getType() string {
	return "FormField"
}

func (f formFieldObj) write(w io.Writer, objID int) error {
	field := f.field
	x1 := field.X
	y1 := field.Y
	x2 := field.X + field.W
	y2 := field.Y + field.H

	io.WriteString(w, "<<\n")
	io.WriteString(w, "/Type /Annot\n")
	io.WriteString(w, "/Subtype /Widget\n")
	fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y1, x2, y2)
	fmt.Fprintf(w, "/P %d 0 R\n", f.pageRef)
	fmt.Fprintf(w, "/T (%s)\n", escapeAnnotString(field.Name))

	if field.Value != "" {
		fmt.Fprintf(w, "/V (%s)\n", escapeAnnotString(field.Value))
		fmt.Fprintf(w, "/DV (%s)\n", escapeAnnotString(field.Value))
	}

	// Field flags
	ff := 0
	if field.ReadOnly {
		ff |= 1 // bit 1
	}
	if field.Required {
		ff |= 2 // bit 2
	}

	switch field.Type {
	case FormFieldText:
		io.WriteString(w, "/FT /Tx\n")
		if field.Multiline {
			ff |= 1 << 12 // bit 13
		}
		if field.MaxLen > 0 {
			fmt.Fprintf(w, "/MaxLen %d\n", field.MaxLen)
		}
	case FormFieldCheckbox:
		io.WriteString(w, "/FT /Btn\n")
		if field.Checked {
			io.WriteString(w, "/V /Yes\n/AS /Yes\n")
		} else {
			io.WriteString(w, "/V /Off\n/AS /Off\n")
		}
	case FormFieldRadio:
		io.WriteString(w, "/FT /Btn\n")
		ff |= 1 << 15 // bit 16 = radio
		ff |= 1 << 14 // bit 15 = NoToggleToOff
	case FormFieldChoice:
		io.WriteString(w, "/FT /Ch\n")
		if len(field.Options) > 0 {
			io.WriteString(w, "/Opt [")
			for _, opt := range field.Options {
				fmt.Fprintf(w, "(%s) ", escapeAnnotString(opt))
			}
			io.WriteString(w, "]\n")
		}
	case FormFieldButton:
		io.WriteString(w, "/FT /Btn\n")
		ff |= 1 << 16 // bit 17 = pushbutton
	case FormFieldSignature:
		io.WriteString(w, "/FT /Sig\n")
	}

	if ff != 0 {
		fmt.Fprintf(w, "/Ff %d\n", ff)
	}

	// Appearance characteristics
	io.WriteString(w, "/MK <<\n")
	if field.HasBorder {
		fmt.Fprintf(w, "  /BC [%.4f %.4f %.4f]\n",
			float64(field.BorderColor[0])/255,
			float64(field.BorderColor[1])/255,
			float64(field.BorderColor[2])/255)
	}
	if field.HasFill {
		fmt.Fprintf(w, "  /BG [%.4f %.4f %.4f]\n",
			float64(field.FillColor[0])/255,
			float64(field.FillColor[1])/255,
			float64(field.FillColor[2])/255)
	}
	if field.Type == FormFieldCheckbox {
		io.WriteString(w, "  /CA (4)\n") // checkmark character
	}
	io.WriteString(w, ">>\n")

	// Border style
	if field.HasBorder {
		io.WriteString(w, "/BS << /W 1 /S /S >>\n")
	}

	// Default appearance string for text fields
	if field.Type == FormFieldText || field.Type == FormFieldChoice {
		fontSize := field.FontSize
		if fontSize == 0 {
			fontSize = 12
		}
		r := float64(field.Color[0]) / 255
		g := float64(field.Color[1]) / 255
		b := float64(field.Color[2]) / 255
		if f.fontRef != "" {
			fmt.Fprintf(w, "/DA (%s %.1f Tf %.4f %.4f %.4f rg)\n",
				f.fontRef, fontSize, r, g, b)
		} else {
			fmt.Fprintf(w, "/DA (/Helv %.1f Tf %.4f %.4f %.4f rg)\n",
				fontSize, r, g, b)
		}
	}

	io.WriteString(w, ">>\n")
	return nil
}

// AddFormField adds a form field (widget) to the current page.
// The field will appear as an interactive form element in PDF viewers.
//
// For text fields, set FontFamily to a pre-loaded font name.
// If FontFamily is empty, the standard Helvetica font is used.
//
// Example:
//
//	pdf.AddFormField(gopdf.FormField{
//	    Type:      gopdf.FormFieldText,
//	    Name:      "username",
//	    X:         50,
//	    Y:         100,
//	    W:         200,
//	    H:         25,
//	    Value:     "Enter name",
//	    FontSize:  12,
//	    HasBorder: true,
//	    BorderColor: [3]uint8{0, 0, 0},
//	})
func (gp *GoPdf) AddFormField(field FormField) error {
	if field.Name == "" {
		return fmt.Errorf("form field name is required")
	}
	if field.W <= 0 || field.H <= 0 {
		return fmt.Errorf("form field width and height must be positive")
	}

	// Find current page object
	pageObjID := gp.findCurrentPageObjID()
	if pageObjID <= 0 {
		return fmt.Errorf("no current page")
	}

	// Resolve font reference
	var fontRef string
	var fontObjID int
	if field.FontFamily != "" {
		for i, obj := range gp.pdfObjs {
			switch o := obj.(type) {
			case *SubsetFontObj:
				if o.GetFamily() == field.FontFamily {
					fontRef = fmt.Sprintf("/F%d", o.CountOfFont+1)
					fontObjID = i + 1
				}
			case *FontObj:
				if o.Family == field.FontFamily {
					fontRef = fmt.Sprintf("/F%d", o.CountOfFont+1)
					fontObjID = i + 1
				}
			}
		}
	}

	ffObj := formFieldObj{
		field:    field,
		pageRef:  pageObjID,
		fontRef:  fontRef,
		fontObjID: fontObjID,
	}
	idx := gp.addObj(ffObj)

	// Add to page's annotation list
	page := gp.findCurrentPageObj()
	if page != nil {
		page.LinkObjIds = append(page.LinkObjIds, idx+1)
	}

	gp.formFields = append(gp.formFields, formFieldRef{
		field:  field,
		objIdx: idx,
	})

	return nil
}

// AddTextField is a convenience method for adding a text input field.
func (gp *GoPdf) AddTextField(name string, x, y, w, h float64) error {
	return gp.AddFormField(FormField{
		Type:        FormFieldText,
		Name:        name,
		X:           x,
		Y:           y,
		W:           w,
		H:           h,
		FontSize:    12,
		HasBorder:   true,
		BorderColor: [3]uint8{0, 0, 0},
	})
}

// AddCheckbox is a convenience method for adding a checkbox field.
func (gp *GoPdf) AddCheckbox(name string, x, y, size float64, checked bool) error {
	return gp.AddFormField(FormField{
		Type:        FormFieldCheckbox,
		Name:        name,
		X:           x,
		Y:           y,
		W:           size,
		H:           size,
		Checked:     checked,
		HasBorder:   true,
		BorderColor: [3]uint8{0, 0, 0},
	})
}

// AddDropdown is a convenience method for adding a dropdown choice field.
func (gp *GoPdf) AddDropdown(name string, x, y, w, h float64, options []string) error {
	return gp.AddFormField(FormField{
		Type:        FormFieldChoice,
		Name:        name,
		X:           x,
		Y:           y,
		W:           w,
		H:           h,
		Options:     options,
		FontSize:    12,
		HasBorder:   true,
		BorderColor: [3]uint8{0, 0, 0},
	})
}

// GetFormFields returns all form fields added to the document.
func (gp *GoPdf) GetFormFields() []FormField {
	fields := make([]FormField, len(gp.formFields))
	for i, ref := range gp.formFields {
		fields[i] = ref.field
	}
	return fields
}

// findCurrentPageObjID returns the 1-based object ID of the current page.
func (gp *GoPdf) findCurrentPageObjID() int {
	if gp.curr.IndexOfPageObj >= 0 && gp.curr.IndexOfPageObj < len(gp.pdfObjs) {
		return gp.curr.IndexOfPageObj + 1
	}
	return 0
}

// findCurrentPageObj returns the current PageObj.
func (gp *GoPdf) findCurrentPageObj() *PageObj {
	if gp.curr.IndexOfPageObj >= 0 && gp.curr.IndexOfPageObj < len(gp.pdfObjs) {
		if p, ok := gp.pdfObjs[gp.curr.IndexOfPageObj].(*PageObj); ok {
			return p
		}
	}
	return nil
}
