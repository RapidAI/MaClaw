package gopdf

import "strings"

// PageLayout controls how pages are displayed when the document is opened.
type PageLayout string

const (
	// PageLayoutSinglePage displays one page at a time.
	PageLayoutSinglePage PageLayout = "SinglePage"
	// PageLayoutOneColumn displays pages in one continuous column.
	PageLayoutOneColumn PageLayout = "OneColumn"
	// PageLayoutTwoColumnLeft displays pages in two columns, odd pages on left.
	PageLayoutTwoColumnLeft PageLayout = "TwoColumnLeft"
	// PageLayoutTwoColumnRight displays pages in two columns, odd pages on right.
	PageLayoutTwoColumnRight PageLayout = "TwoColumnRight"
	// PageLayoutTwoPageLeft displays two pages at a time, odd pages on left.
	PageLayoutTwoPageLeft PageLayout = "TwoPageLeft"
	// PageLayoutTwoPageRight displays two pages at a time, odd pages on right.
	PageLayoutTwoPageRight PageLayout = "TwoPageRight"
)

// PageMode controls what panel is displayed when the document is opened.
type PageMode string

const (
	// PageModeUseNone shows no panel (default).
	PageModeUseNone PageMode = "UseNone"
	// PageModeUseOutlines shows the bookmarks/outline panel.
	PageModeUseOutlines PageMode = "UseOutlines"
	// PageModeUseThumbs shows the page thumbnails panel.
	PageModeUseThumbs PageMode = "UseThumbs"
	// PageModeFullScreen opens in full-screen mode.
	PageModeFullScreen PageMode = "FullScreen"
	// PageModeUseOC shows the optional content (layers) panel.
	PageModeUseOC PageMode = "UseOC"
	// PageModeUseAttachments shows the attachments panel.
	PageModeUseAttachments PageMode = "UseAttachments"
)

// SetPageLayout sets the page layout for the document.
// This controls how pages are arranged when the document is opened.
//
// Example:
//
//	pdf.SetPageLayout(gopdf.PageLayoutTwoColumnLeft)
func (gp *GoPdf) SetPageLayout(layout PageLayout) {
	catalogObj := gp.pdfObjs[gp.indexOfCatalogObj].(*CatalogObj)
	catalogObj.pageLayout = string(layout)
}

// GetPageLayout returns the current page layout setting.
func (gp *GoPdf) GetPageLayout() PageLayout {
	catalogObj := gp.pdfObjs[gp.indexOfCatalogObj].(*CatalogObj)
	if catalogObj.pageLayout == "" {
		return PageLayoutSinglePage
	}
	return PageLayout(catalogObj.pageLayout)
}

// SetPageMode sets the page mode for the document.
// This controls which panel is shown when the document is opened.
//
// Note: If outlines are present, PageMode may be overridden to UseOutlines.
//
// Example:
//
//	pdf.SetPageMode(gopdf.PageModeUseThumbs)
func (gp *GoPdf) SetPageMode(mode PageMode) {
	catalogObj := gp.pdfObjs[gp.indexOfCatalogObj].(*CatalogObj)
	catalogObj.pageMode = string(mode)
}

// GetPageMode returns the current page mode setting.
func (gp *GoPdf) GetPageMode() PageMode {
	catalogObj := gp.pdfObjs[gp.indexOfCatalogObj].(*CatalogObj)
	if catalogObj.pageMode == "" {
		return PageModeUseNone
	}
	return PageMode(catalogObj.pageMode)
}

// validPageLayout checks if a string is a valid PageLayout value.
func validPageLayout(s string) bool {
	switch strings.ToLower(s) {
	case "singlepage", "onecolumn", "twocolumnleft", "twocolumnright",
		"twopageleft", "twopageright":
		return true
	}
	return false
}

// validPageMode checks if a string is a valid PageMode value.
func validPageMode(s string) bool {
	switch strings.ToLower(s) {
	case "usenone", "useoutlines", "usethumbs", "fullscreen",
		"useoc", "useattachments":
		return true
	}
	return false
}
