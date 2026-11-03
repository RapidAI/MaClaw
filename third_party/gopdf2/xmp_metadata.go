package gopdf

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// XMPMetadata holds XMP (Extensible Metadata Platform) metadata for the PDF.
// XMP is the standard metadata format for PDF 2.0 and is also supported
// in earlier versions. It provides richer metadata than the traditional
// Document Information Dictionary (PdfInfo).
type XMPMetadata struct {
	// Dublin Core (dc:) properties
	Title       string   // dc:title
	Creator     []string // dc:creator (authors)
	Description string   // dc:description
	Subject     []string // dc:subject (keywords)
	Rights      string   // dc:rights (copyright)
	Language    string   // dc:language (e.g. "en-US")

	// XMP Basic (xmp:) properties
	CreatorTool string    // xmp:CreatorTool (application name)
	CreateDate  time.Time // xmp:CreateDate
	ModifyDate  time.Time // xmp:ModifyDate

	// PDF-specific (pdf:) properties
	Producer string // pdf:Producer
	Keywords string // pdf:Keywords
	Trapped  string // pdf:Trapped ("True", "False", "Unknown")

	// PDF/A conformance (pdfaid:)
	PDFAConformance string // pdfaid:conformance ("A", "B", "U")
	PDFAPart        int    // pdfaid:part (1, 2, 3)

	// Custom properties
	Custom map[string]string
}

// SetXMPMetadata sets the XMP metadata for the document.
// The XMP metadata stream will be embedded in the PDF output.
//
// Example:
//
//	pdf.SetXMPMetadata(gopdf.XMPMetadata{
//	    Title:       "Annual Report 2025",
//	    Creator:     []string{"John Doe", "Jane Smith"},
//	    Description: "Company annual financial report",
//	    Subject:     []string{"finance", "annual report"},
//	    CreatorTool: "GoPDF2",
//	    Producer:    "GoPDF2",
//	    CreateDate:  time.Now(),
//	    ModifyDate:  time.Now(),
//	})
func (gp *GoPdf) SetXMPMetadata(meta XMPMetadata) {
	gp.xmpMetadata = &meta
}

// GetXMPMetadata returns the current XMP metadata, or nil if not set.
func (gp *GoPdf) GetXMPMetadata() *XMPMetadata {
	return gp.xmpMetadata
}

// xmpMetadataObj is the PDF metadata stream object containing XMP data.
type xmpMetadataObj struct {
	meta *XMPMetadata
}

func (x xmpMetadataObj) init(f func() *GoPdf) {}

func (x xmpMetadataObj) getType() string {
	return "Metadata"
}

func (x xmpMetadataObj) write(w io.Writer, objID int) error {
	xmpData := x.buildXMP()

	fmt.Fprintf(w, "<<\n")
	fmt.Fprintf(w, "/Type /Metadata\n")
	fmt.Fprintf(w, "/Subtype /XML\n")
	fmt.Fprintf(w, "/Length %d\n", len(xmpData))
	fmt.Fprintf(w, ">>\n")
	fmt.Fprintf(w, "stream\n")
	io.WriteString(w, xmpData)
	fmt.Fprintf(w, "\nendstream\n")
	return nil
}

func (x xmpMetadataObj) buildXMP() string {
	m := x.meta
	var b strings.Builder

	b.WriteString(`<?xpacket begin="` + "\xef\xbb\xbf" + `" id="W5M0MpCehiHzreSzNTczkc9d"?>` + "\n")
	b.WriteString(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` + "\n")
	b.WriteString(`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` + "\n")
	b.WriteString(`<rdf:Description rdf:about=""` + "\n")
	b.WriteString(`  xmlns:dc="http://purl.org/dc/elements/1.1/"` + "\n")
	b.WriteString(`  xmlns:xmp="http://ns.adobe.com/xap/1.0/"` + "\n")
	b.WriteString(`  xmlns:pdf="http://ns.adobe.com/pdf/1.3/"` + "\n")
	b.WriteString(`  xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"` + "\n")
	b.WriteString(`>` + "\n")

	// dc:title
	if m.Title != "" {
		b.WriteString(`  <dc:title><rdf:Alt><rdf:li xml:lang="x-default">`)
		b.WriteString(xmlEscape(m.Title))
		b.WriteString(`</rdf:li></rdf:Alt></dc:title>` + "\n")
	}

	// dc:creator
	if len(m.Creator) > 0 {
		b.WriteString("  <dc:creator><rdf:Seq>\n")
		for _, c := range m.Creator {
			b.WriteString("    <rdf:li>")
			b.WriteString(xmlEscape(c))
			b.WriteString("</rdf:li>\n")
		}
		b.WriteString("  </rdf:Seq></dc:creator>\n")
	}

	// dc:description
	if m.Description != "" {
		b.WriteString(`  <dc:description><rdf:Alt><rdf:li xml:lang="x-default">`)
		b.WriteString(xmlEscape(m.Description))
		b.WriteString(`</rdf:li></rdf:Alt></dc:description>` + "\n")
	}

	// dc:subject
	if len(m.Subject) > 0 {
		b.WriteString("  <dc:subject><rdf:Bag>\n")
		for _, s := range m.Subject {
			b.WriteString("    <rdf:li>")
			b.WriteString(xmlEscape(s))
			b.WriteString("</rdf:li>\n")
		}
		b.WriteString("  </rdf:Bag></dc:subject>\n")
	}

	// dc:rights
	if m.Rights != "" {
		b.WriteString(`  <dc:rights><rdf:Alt><rdf:li xml:lang="x-default">`)
		b.WriteString(xmlEscape(m.Rights))
		b.WriteString(`</rdf:li></rdf:Alt></dc:rights>` + "\n")
	}

	// dc:language
	if m.Language != "" {
		b.WriteString("  <dc:language><rdf:Bag><rdf:li>")
		b.WriteString(xmlEscape(m.Language))
		b.WriteString("</rdf:li></rdf:Bag></dc:language>\n")
	}

	// xmp:CreatorTool
	if m.CreatorTool != "" {
		b.WriteString("  <xmp:CreatorTool>")
		b.WriteString(xmlEscape(m.CreatorTool))
		b.WriteString("</xmp:CreatorTool>\n")
	}

	// xmp:CreateDate
	if !m.CreateDate.IsZero() {
		b.WriteString("  <xmp:CreateDate>")
		b.WriteString(m.CreateDate.UTC().Format("2006-01-02T15:04:05Z"))
		b.WriteString("</xmp:CreateDate>\n")
	}

	// xmp:ModifyDate
	if !m.ModifyDate.IsZero() {
		b.WriteString("  <xmp:ModifyDate>")
		b.WriteString(m.ModifyDate.UTC().Format("2006-01-02T15:04:05Z"))
		b.WriteString("</xmp:ModifyDate>\n")
	}

	// pdf:Producer
	if m.Producer != "" {
		b.WriteString("  <pdf:Producer>")
		b.WriteString(xmlEscape(m.Producer))
		b.WriteString("</pdf:Producer>\n")
	}

	// pdf:Keywords
	if m.Keywords != "" {
		b.WriteString("  <pdf:Keywords>")
		b.WriteString(xmlEscape(m.Keywords))
		b.WriteString("</pdf:Keywords>\n")
	}

	// pdf:Trapped
	if m.Trapped != "" {
		b.WriteString("  <pdf:Trapped>")
		b.WriteString(xmlEscape(m.Trapped))
		b.WriteString("</pdf:Trapped>\n")
	}

	// pdfaid:part and pdfaid:conformance
	if m.PDFAPart > 0 {
		fmt.Fprintf(&b, "  <pdfaid:part>%d</pdfaid:part>\n", m.PDFAPart)
	}
	if m.PDFAConformance != "" {
		b.WriteString("  <pdfaid:conformance>")
		b.WriteString(xmlEscape(m.PDFAConformance))
		b.WriteString("</pdfaid:conformance>\n")
	}

	b.WriteString(`</rdf:Description>` + "\n")
	b.WriteString(`</rdf:RDF>` + "\n")
	b.WriteString(`</x:xmpmeta>` + "\n")
	b.WriteString(`<?xpacket end="w"?>`)

	return b.String()
}

// xmlEscape escapes special XML characters.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
