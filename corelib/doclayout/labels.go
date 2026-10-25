package doclayout

// Labels is the PP-DocLayoutV3 class order from the model's inference.yml.
// Index 0 is "abstract". The order is the model's, not a display preference.
var Labels = []string{
	"abstract",
	"algorithm",
	"aside_text",
	"chart",
	"content",
	"display_formula",
	"doc_title",
	"figure_title",
	"footer",
	"footer_image",
	"footnote",
	"formula_number",
	"header",
	"header_image",
	"image",
	"inline_formula",
	"number",
	"paragraph_title",
	"reference",
	"reference_content",
	"seal",
	"table",
	"text",
	"vertical_text",
	"vision_footnote",
}

// TableLabel is the region a wired table model receives.
const TableLabel = "table"

// FormulaLabel reports a region whose crop is a formula.
func FormulaLabel(label string) bool {
	return label == "display_formula" || label == "inline_formula"
}
