package gopdf

import (
	"fmt"
	"io"
	"strings"
)

// OCGIntent represents the intent of an Optional Content Group.
type OCGIntent string

const (
	// OCGIntentView indicates the OCG is for viewing purposes.
	OCGIntentView OCGIntent = "View"
	// OCGIntentDesign indicates the OCG is for design purposes.
	OCGIntentDesign OCGIntent = "Design"
)

// OCG represents an Optional Content Group (layer) in a PDF.
// OCGs allow content to be selectively shown or hidden.
type OCG struct {
	// Name is the display name of the layer.
	Name string
	// Intent is the visibility intent ("View" or "Design").
	Intent OCGIntent
	// On indicates whether the layer is initially visible.
	On bool
	// objIndex is the internal object index (set after adding).
	objIndex int
}

// ocgObj is the PDF object for an Optional Content Group.
type ocgObj struct {
	name   string
	intent OCGIntent
}

func (o ocgObj) init(f func() *GoPdf) {}

func (o ocgObj) getType() string {
	return "OCG"
}

func (o ocgObj) write(w io.Writer, objID int) error {
	io.WriteString(w, "<<\n")
	io.WriteString(w, "/Type /OCG\n")
	fmt.Fprintf(w, "/Name (%s)\n", escapeAnnotString(o.name))
	intent := string(o.intent)
	if intent == "" {
		intent = "View"
	}
	fmt.Fprintf(w, "/Intent /%s\n", intent)
	io.WriteString(w, ">>\n")
	return nil
}

// ocPropertiesObj is the PDF object for the /OCProperties dictionary
// in the catalog. It lists all OCGs and their default visibility.
type ocPropertiesObj struct {
	ocgs         []ocgRef
	layerConfigs []LayerConfig
	uiConfig     *LayerUIConfig
}

type ocgRef struct {
	objID int // 1-based PDF object ID
	on    bool
}

func (o ocPropertiesObj) init(f func() *GoPdf) {}

func (o ocPropertiesObj) getType() string {
	return "OCProperties"
}

func (o ocPropertiesObj) write(w io.Writer, objID int) error {
	// Build OCG reference list.
	var allRefs []string
	var onRefs []string
	var offRefs []string

	for _, ref := range o.ocgs {
		r := fmt.Sprintf("%d 0 R", ref.objID)
		allRefs = append(allRefs, r)
		if ref.on {
			onRefs = append(onRefs, r)
		} else {
			offRefs = append(offRefs, r)
		}
	}

	io.WriteString(w, "<<\n")
	// /OCGs array — all OCGs in the document.
	fmt.Fprintf(w, "/OCGs [%s]\n", strings.Join(allRefs, " "))

	// /D — default configuration dictionary.
	io.WriteString(w, "/D <<\n")

	// Apply UI config to default configuration if set.
	if o.uiConfig != nil {
		if o.uiConfig.Name != "" {
			fmt.Fprintf(w, "  /Name (%s)\n", escapeAnnotString(o.uiConfig.Name))
		}
		if o.uiConfig.Creator != "" {
			fmt.Fprintf(w, "  /Creator (%s)\n", escapeAnnotString(o.uiConfig.Creator))
		}
		if o.uiConfig.BaseState != "" {
			fmt.Fprintf(w, "  /BaseState /%s\n", o.uiConfig.BaseState)
		}
	}

	if len(onRefs) > 0 {
		fmt.Fprintf(w, "  /ON [%s]\n", strings.Join(onRefs, " "))
	}
	if len(offRefs) > 0 {
		fmt.Fprintf(w, "  /OFF [%s]\n", strings.Join(offRefs, " "))
	}
	// /Order array controls the layer panel display order.
	fmt.Fprintf(w, "  /Order [%s]\n", strings.Join(allRefs, " "))

	// Locked OCGs from UI config.
	if o.uiConfig != nil && len(o.uiConfig.Locked) > 0 {
		var lockedRefs []string
		for _, ocg := range o.uiConfig.Locked {
			lockedRefs = append(lockedRefs, fmt.Sprintf("%d 0 R", ocg.objIndex+1))
		}
		fmt.Fprintf(w, "  /Locked [%s]\n", strings.Join(lockedRefs, " "))
	}

	io.WriteString(w, ">>\n")

	// /Configs — alternate configuration dictionaries.
	if len(o.layerConfigs) > 0 {
		io.WriteString(w, "/Configs [\n")
		for _, cfg := range o.layerConfigs {
			io.WriteString(w, "  <<\n")
			if cfg.Name != "" {
				fmt.Fprintf(w, "    /Name (%s)\n", escapeAnnotString(cfg.Name))
			}
			if cfg.Creator != "" {
				fmt.Fprintf(w, "    /Creator (%s)\n", escapeAnnotString(cfg.Creator))
			}
			if len(cfg.OnOCGs) > 0 {
				var refs []string
				for _, ocg := range cfg.OnOCGs {
					refs = append(refs, fmt.Sprintf("%d 0 R", ocg.objIndex+1))
				}
				fmt.Fprintf(w, "    /ON [%s]\n", strings.Join(refs, " "))
			}
			if len(cfg.OffOCGs) > 0 {
				var refs []string
				for _, ocg := range cfg.OffOCGs {
					refs = append(refs, fmt.Sprintf("%d 0 R", ocg.objIndex+1))
				}
				fmt.Fprintf(w, "    /OFF [%s]\n", strings.Join(refs, " "))
			}
			if len(cfg.Order) > 0 {
				var refs []string
				for _, ocg := range cfg.Order {
					refs = append(refs, fmt.Sprintf("%d 0 R", ocg.objIndex+1))
				}
				fmt.Fprintf(w, "    /Order [%s]\n", strings.Join(refs, " "))
			}
			io.WriteString(w, "  >>\n")
		}
		io.WriteString(w, "]\n")
	}

	io.WriteString(w, ">>\n")
	return nil
}

// AddOCG adds an Optional Content Group (layer) to the document.
// Returns the OCG for use with SetContentOCG.
//
// Example:
//
//	watermarkLayer := pdf.AddOCG(gopdf.OCG{
//	    Name:   "Watermark",
//	    Intent: gopdf.OCGIntentView,
//	    On:     true,
//	})
//	draftLayer := pdf.AddOCG(gopdf.OCG{
//	    Name:   "Draft Notes",
//	    Intent: gopdf.OCGIntentDesign,
//	    On:     false,
//	})
func (gp *GoPdf) AddOCG(ocg OCG) OCG {
	if ocg.Intent == "" {
		ocg.Intent = OCGIntentView
	}
	idx := gp.addObj(ocgObj{
		name:   ocg.Name,
		intent: ocg.Intent,
	})
	ocg.objIndex = idx
	gp.ocgs = append(gp.ocgs, ocgRef{
		objID: idx + 1,
		on:    ocg.On,
	})
	return ocg
}

// GetOCGs returns all Optional Content Groups defined in the document.
func (gp *GoPdf) GetOCGs() []OCG {
	var result []OCG
	for i, obj := range gp.pdfObjs {
		if o, ok := obj.(ocgObj); ok {
			on := true
			for _, ref := range gp.ocgs {
				if ref.objID == i+1 {
					on = ref.on
					break
				}
			}
			result = append(result, OCG{
				Name:     o.name,
				Intent:   o.intent,
				On:       on,
				objIndex: i,
			})
		}
	}
	return result
}

// OCGVisibilityPolicy defines how OCMD member OCGs are combined.
type OCGVisibilityPolicy string

const (
	// OCGVisibilityAllOn means all member OCGs must be ON for content to be visible.
	OCGVisibilityAllOn OCGVisibilityPolicy = "AllOn"
	// OCGVisibilityAnyOn means any member OCG being ON makes content visible.
	OCGVisibilityAnyOn OCGVisibilityPolicy = "AnyOn"
	// OCGVisibilityAllOff means all member OCGs must be OFF for content to be visible.
	OCGVisibilityAllOff OCGVisibilityPolicy = "AllOff"
	// OCGVisibilityAnyOff means any member OCG being OFF makes content visible.
	OCGVisibilityAnyOff OCGVisibilityPolicy = "AnyOff"
)

// OCMD represents an Optional Content Membership Dictionary.
// It combines multiple OCGs with a visibility policy.
type OCMD struct {
	// OCGs is the list of member OCGs.
	OCGs []OCG
	// Policy determines how member visibility is combined.
	Policy OCGVisibilityPolicy
	// objIndex is the internal object index (set after adding).
	objIndex int
}

// ocmdObj is the PDF object for an Optional Content Membership Dictionary.
type ocmdObj struct {
	ocgObjIDs []int // 1-based PDF object IDs of member OCGs
	policy    OCGVisibilityPolicy
}

func (o ocmdObj) init(f func() *GoPdf) {}

func (o ocmdObj) getType() string {
	return "OCMD"
}

func (o ocmdObj) write(w io.Writer, objID int) error {
	io.WriteString(w, "<<\n")
	io.WriteString(w, "/Type /OCMD\n")
	var refs []string
	for _, id := range o.ocgObjIDs {
		refs = append(refs, fmt.Sprintf("%d 0 R", id))
	}
	fmt.Fprintf(w, "/OCGs [%s]\n", strings.Join(refs, " "))
	policy := string(o.policy)
	if policy == "" {
		policy = "AnyOn"
	}
	fmt.Fprintf(w, "/P /%s\n", policy)
	io.WriteString(w, ">>\n")
	return nil
}

// LayerConfig represents a named layer configuration.
// Alternate configurations allow switching between different layer visibility presets.
type LayerConfig struct {
	// Name is the display name of this configuration.
	Name string
	// Creator is an optional creator string.
	Creator string
	// OnOCGs lists OCGs that should be ON in this configuration.
	OnOCGs []OCG
	// OffOCGs lists OCGs that should be OFF in this configuration.
	OffOCGs []OCG
	// Order defines the display order of OCGs in the layer panel.
	// If nil, all document OCGs are listed in document order.
	Order []OCG
}

// LayerUIConfig represents the UI configuration for the layer panel.
type LayerUIConfig struct {
	// Name is the display name shown in the layer panel.
	Name string
	// Creator is an optional creator string.
	Creator string
	// BaseState is the default visibility state: "ON", "OFF", or "Unchanged".
	BaseState string
	// Locked lists OCGs that cannot be toggled by the user.
	Locked []OCG
}

// SetOCGState sets the initial visibility state of an existing OCG by name.
//
// Example:
//
//	err := pdf.SetOCGState("Watermark", false)
func (gp *GoPdf) SetOCGState(name string, on bool) error {
	for i, ref := range gp.ocgs {
		idx := ref.objID - 1
		if idx >= 0 && idx < len(gp.pdfObjs) {
			if o, ok := gp.pdfObjs[idx].(ocgObj); ok && o.name == name {
				gp.ocgs[i].on = on
				return nil
			}
		}
	}
	return fmt.Errorf("OCG %q not found", name)
}

// SetOCGStates sets the visibility state of multiple OCGs at once.
// The map keys are OCG names and values are the desired visibility states.
//
// Example:
//
//	pdf.SetOCGStates(map[string]bool{
//	    "Watermark":   true,
//	    "Draft Notes": false,
//	})
func (gp *GoPdf) SetOCGStates(states map[string]bool) error {
	for name, on := range states {
		if err := gp.SetOCGState(name, on); err != nil {
			return err
		}
	}
	return nil
}

// AddOCMD adds an Optional Content Membership Dictionary to the document.
// An OCMD combines multiple OCGs with a visibility policy.
//
// Example:
//
//	ocmd := pdf.AddOCMD(gopdf.OCMD{
//	    OCGs:   []gopdf.OCG{layer1, layer2},
//	    Policy: gopdf.OCGVisibilityAllOn,
//	})
func (gp *GoPdf) AddOCMD(ocmd OCMD) OCMD {
	var ids []int
	for _, ocg := range ocmd.OCGs {
		ids = append(ids, ocg.objIndex+1)
	}
	if ocmd.Policy == "" {
		ocmd.Policy = OCGVisibilityAnyOn
	}
	idx := gp.addObj(ocmdObj{
		ocgObjIDs: ids,
		policy:    ocmd.Policy,
	})
	ocmd.objIndex = idx
	return ocmd
}

// GetOCMD returns the OCMD at the given object index, or an error if not found.
func (gp *GoPdf) GetOCMD(ocmd OCMD) (OCMD, error) {
	if ocmd.objIndex < 0 || ocmd.objIndex >= len(gp.pdfObjs) {
		return OCMD{}, fmt.Errorf("OCMD not found")
	}
	obj, ok := gp.pdfObjs[ocmd.objIndex].(ocmdObj)
	if !ok {
		return OCMD{}, fmt.Errorf("OCMD not found")
	}
	result := OCMD{
		Policy:   obj.policy,
		objIndex: ocmd.objIndex,
	}
	for _, id := range obj.ocgObjIDs {
		idx := id - 1
		if idx >= 0 && idx < len(gp.pdfObjs) {
			if o, ok := gp.pdfObjs[idx].(ocgObj); ok {
				on := true
				for _, ref := range gp.ocgs {
					if ref.objID == id {
						on = ref.on
						break
					}
				}
				result.OCGs = append(result.OCGs, OCG{
					Name:     o.name,
					Intent:   o.intent,
					On:       on,
					objIndex: idx,
				})
			}
		}
	}
	return result, nil
}

// AddLayerConfig adds an alternate layer configuration to the document.
// Alternate configurations allow PDF viewers to switch between different
// layer visibility presets.
//
// Example:
//
//	pdf.AddLayerConfig(gopdf.LayerConfig{
//	    Name:    "Print Version",
//	    OnOCGs:  []gopdf.OCG{contentLayer},
//	    OffOCGs: []gopdf.OCG{draftLayer},
//	})
func (gp *GoPdf) AddLayerConfig(config LayerConfig) {
	gp.layerConfigs = append(gp.layerConfigs, config)
}

// GetLayerConfigs returns all alternate layer configurations.
func (gp *GoPdf) GetLayerConfigs() []LayerConfig {
	result := make([]LayerConfig, len(gp.layerConfigs))
	copy(result, gp.layerConfigs)
	return result
}

// SetLayerUIConfig sets the UI configuration for the layer panel.
// This controls how layers appear in the PDF viewer's layer panel.
//
// Example:
//
//	pdf.SetLayerUIConfig(gopdf.LayerUIConfig{
//	    Name:      "Default",
//	    BaseState: "ON",
//	    Locked:    []gopdf.OCG{watermarkLayer},
//	})
func (gp *GoPdf) SetLayerUIConfig(config LayerUIConfig) {
	gp.layerUIConfig = &config
}

// GetLayerUIConfig returns the current layer UI configuration, or nil if not set.
func (gp *GoPdf) GetLayerUIConfig() *LayerUIConfig {
	return gp.layerUIConfig
}

// SwitchLayer changes the visibility of a layer by name, turning it on
// and optionally turning off all other layers.
//
// Example:
//
//	err := pdf.SwitchLayer("Print Version", true)
func (gp *GoPdf) SwitchLayer(name string, exclusive bool) error {
	found := false
	for i, ref := range gp.ocgs {
		idx := ref.objID - 1
		if idx >= 0 && idx < len(gp.pdfObjs) {
			if o, ok := gp.pdfObjs[idx].(ocgObj); ok {
				if o.name == name {
					gp.ocgs[i].on = true
					found = true
				} else if exclusive {
					gp.ocgs[i].on = false
				}
			}
		}
	}
	if !found {
		return fmt.Errorf("OCG %q not found", name)
	}
	return nil
}
