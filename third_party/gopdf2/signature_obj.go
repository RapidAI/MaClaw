package gopdf

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// signatureValueObj is the PDF signature value dictionary (/Type /Sig).
// It writes a placeholder for /Contents that gets patched after signing.
type signatureValueObj struct {
	cfg          *SignatureConfig
	contentsSize int // size in bytes (hex will be 2x this)

	// contentsPlaceholder is the hex placeholder string used to locate
	// the /Contents value in the final PDF for patching.
	contentsPlaceholder string
}

func (s *signatureValueObj) init(fn func() *GoPdf) {}

func (s *signatureValueObj) getType() string {
	return "Sig"
}

func (s *signatureValueObj) write(w io.Writer, objID int) error {
	io.WriteString(w, "<<\n")
	io.WriteString(w, "/Type /Sig\n")
	io.WriteString(w, "/Filter /Adobe.PPKLite\n")
	io.WriteString(w, "/SubFilter /adbe.pkcs7.detached\n")

	// Contents placeholder — hex-encoded PKCS#7 signature, patched later.
	placeholder := strings.Repeat("0", s.contentsSize*2)
	s.contentsPlaceholder = placeholder
	fmt.Fprintf(w, "/Contents <%s>\n", placeholder)

	// ByteRange placeholder — patched after PDF is rendered.
	fmt.Fprintf(w, "/ByteRange %s\n", s.byteRangePlaceholder())

	if s.cfg != nil {
		if s.cfg.Name != "" {
			fmt.Fprintf(w, "/Name (%s)\n", escapeAnnotString(s.cfg.Name))
		}
		if s.cfg.Reason != "" {
			fmt.Fprintf(w, "/Reason (%s)\n", escapeAnnotString(s.cfg.Reason))
		}
		if s.cfg.Location != "" {
			fmt.Fprintf(w, "/Location (%s)\n", escapeAnnotString(s.cfg.Location))
		}
		if s.cfg.ContactInfo != "" {
			fmt.Fprintf(w, "/ContactInfo (%s)\n", escapeAnnotString(s.cfg.ContactInfo))
		}
		if !s.cfg.SignTime.IsZero() {
			fmt.Fprintf(w, "/M (%s)\n", infodate(s.cfg.SignTime))
		}
	}

	io.WriteString(w, ">>\n")
	return nil
}

// byteRangePlaceholder returns the fixed-width placeholder for /ByteRange.
func (s *signatureValueObj) byteRangePlaceholder() string {
	// Fixed-width placeholder that will be replaced with actual values.
	placeholder := "[0 0000000000 0000000000 0000000000]"
	// Pad to signatureByteRangeSize
	for len(placeholder) < signatureByteRangeSize {
		placeholder += " "
	}
	return placeholder
}

// findContentsPlaceholder locates the /Contents <hex> value in the PDF bytes.
// Returns the byte offset of '<' and the byte after '>'.
func (s *signatureValueObj) findContentsPlaceholder(pdfBytes []byte) (start, end int, err error) {
	placeholder := []byte(s.contentsPlaceholder)
	idx := bytes.Index(pdfBytes, placeholder)
	if idx < 0 {
		return 0, 0, fmt.Errorf("signature contents placeholder not found in PDF output")
	}
	// '<' is one byte before the placeholder hex
	start = idx - 1
	// '>' is one byte after the placeholder hex
	end = idx + len(placeholder) + 1
	return start, end, nil
}

// signatureFieldObj is the PDF signature field widget annotation.
type signatureFieldObj struct {
	cfg         *SignatureConfig
	sigValueRef int // 1-based object ID of the signature value object
	getRoot     func() *GoPdf
}

func (s *signatureFieldObj) init(fn func() *GoPdf) {}

func (s *signatureFieldObj) getType() string {
	return "FormField"
}

func (s *signatureFieldObj) write(w io.Writer, objID int) error {
	io.WriteString(w, "<<\n")
	io.WriteString(w, "/Type /Annot\n")
	io.WriteString(w, "/Subtype /Widget\n")
	io.WriteString(w, "/FT /Sig\n")
	fmt.Fprintf(w, "/T (%s)\n", escapeAnnotString(s.cfg.SignatureFieldName))
	fmt.Fprintf(w, "/V %d 0 R\n", s.sigValueRef)

	if s.cfg.Visible {
		gp := s.getRoot()
		pageH := gp.config.PageSize.H
		x1 := s.cfg.X
		y1 := pageH - s.cfg.Y - s.cfg.H
		x2 := s.cfg.X + s.cfg.W
		y2 := pageH - s.cfg.Y
		fmt.Fprintf(w, "/Rect [%.2f %.2f %.2f %.2f]\n", x1, y1, x2, y2)
	} else {
		io.WriteString(w, "/Rect [0 0 0 0]\n")
	}

	// Find the page object reference for /P
	if s.cfg.Visible {
		gp := s.getRoot()
		count := 0
		for i, obj := range gp.pdfObjs {
			if _, ok := obj.(*PageObj); ok {
				count++
				if count == s.cfg.PageNo {
					fmt.Fprintf(w, "/P %d 0 R\n", i+1)
					break
				}
			}
		}
	}

	// Flags: locked, print
	io.WriteString(w, "/F 132\n") // Print + Locked
	io.WriteString(w, "/Ff 1\n")  // ReadOnly

	io.WriteString(w, ">>\n")
	return nil
}
