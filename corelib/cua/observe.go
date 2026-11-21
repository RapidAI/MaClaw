package cua

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// # Observation and deterministic checks (probed against cua-driver 0.52.9)
//
//	get_window_state {pid, window_id, query?, tree_format?, max_elements?,
//	                  include_screenshot?, since?}
//	  -> elements[]: {element_token, role, label, value, frame{x,y,w,h},
//	                  actions[]}, snapshot_id, total_element_count,
//	                  returned_element_count, truncated, elements_complete
//	verify_state {pid, window_id, expect:[...], stable_samples?, timeout_ms?}
//	  -> status:"satisfied"|"unsatisfied"|"unknown", predicates[]
//	  selector accepts exactly {role} and/or {label_contains}; both are
//	  case-insensitive on role. exists:false for an element is refused
//	  ("element snapshots are not exhaustive"), so absence is only provable
//	  at window level: verify_state {window:{exists:false}}.

// Selector picks one element row in a window snapshot.
type Selector struct {
	// Role matches a UIA control type, case-insensitively ("Button" ==
	// "button"). A prefix of the type (rolePrefix) is not a role.
	Role string `json:"role,omitempty"`
	// Label pins the row whose label or value contains this text.
	Label string `json:"label_contains,omitempty"`
	// RolePrefix is a MaClaw-side convenience: it is turned into a role of
	// the matching row(s), never sent to the driver.
	RolePrefix string `json:"role_prefix,omitempty"`
	// Index picks one of several matches in document order, from 0.
	Index int `json:"index,omitempty"`
}

// UnmarshalJSON keeps a misspelled name a clear error instead of a silent
// empty selector.
func (s *Selector) UnmarshalJSON(b []byte) error {
	type plain Selector
	var p plain
	if err := unmarshalStrict(b, &p, map[string]bool{"role": true, "label_contains": true, "role_prefix": true, "index": true}); err != nil {
		return err
	}
	*s = Selector(p)
	return nil
}

// Element is one row of a window snapshot.
type Element struct {
	Token   string   `json:"element_token"`
	Role    string   `json:"role"`
	Label   string   `json:"label"`
	Value   string   `json:"value"`
	Actions []string `json:"actions"`
	Frame   Frame    `json:"frame"`
}

// Frame is an element rectangle in screen points, origin top-left.
type Frame struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Center is the point a pixel action would use.
func (f Frame) Center() (int, int) { return f.X + f.W/2, f.Y + f.H/2 }

// Snapshot is one read of a window's accessibility tree.
type Snapshot struct {
	ID           string    `json:"snapshot_id"`
	WindowTitle  string    `json:"window_title"`
	AppName      string    `json:"app_name"`
	Elements     []Element `json:"elements"`
	Total        int       `json:"total_element_count"`
	Returned     int       `json:"returned_element_count"`
	Truncated    bool      `json:"truncated"`
	Complete     bool      `json:"elements_complete"`
	WindowBounds Frame     `json:"-"`
	Query        string    `json:"-"`
	At           time.Time `json:"-"`
	driverText   string
}

// Snapshot reads one window, optionally projected to rows matching query.
// maxElements bounds the walk (0 keeps the driver default). The query is a
// case-insensitive substring of a row's role, label, value, or id; "a|b"
// matches either.
func (d *Driver) Snapshot(ctx context.Context, pid, windowID int, query string, maxElements int) (Snapshot, error) {
	// The driver decodes numbers as integers, so the ids go as ints, not
	// as the pre-serialized any value of a caller's own type.
	args := map[string]any{
		"pid":         int64(pid),
		"window_id":   int64(windowID),
		"tree_format": "elements",
		// The tree is the observation; a screenshot would be dropped here.
		"include_screenshot": false,
	}
	if strings.TrimSpace(query) != "" {
		args["query"] = query
	}
	if maxElements > 0 {
		args["max_elements"] = maxElements
	}
	out, text, err := d.CallDetail(ctx, "get_window_state", args)
	if err != nil {
		return Snapshot{}, err
	}
	raw, err := remarshal[struct {
		SnapshotID       string    `json:"snapshot_id"`
		WindowTitle      string    `json:"window_title"`
		AppName          string    `json:"app_name"`
		Elements         []Element `json:"elements"`
		Total            int       `json:"total_element_count"`
		Returned         int       `json:"returned_element_count"`
		Truncated        bool      `json:"truncated"`
		ElementsComplete bool      `json:"elements_complete"`
		WindowBounds     Frame     `json:"window_bounds"`
	}](out)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		ID:           raw.SnapshotID,
		WindowTitle:  raw.WindowTitle,
		AppName:      raw.AppName,
		Elements:     raw.Elements,
		Total:        raw.Total,
		Returned:     raw.Returned,
		Truncated:    raw.Truncated,
		Complete:     raw.ElementsComplete,
		WindowBounds: raw.WindowBounds,
		Query:        query,
		At:           time.Now(),
		driverText:   text,
	}, nil
}

// Match returns the elements a selector picks, in document order.
func (s Snapshot) Match(sel Selector) []Element {
	var hits []Element
	label := strings.ToLower(strings.TrimSpace(sel.Label))
	role := strings.ToLower(strings.TrimSpace(sel.Role))
	prefix := strings.ToLower(strings.TrimSpace(sel.RolePrefix))
	for _, e := range s.Elements {
		if role != "" && !strings.EqualFold(e.Role, sel.Role) {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(e.Role), prefix) {
			continue
		}
		if label != "" {
			if !strings.Contains(strings.ToLower(e.Label), label) && !strings.Contains(strings.ToLower(e.Value), label) {
				continue
			}
		}
		hits = append(hits, e)
	}
	if len(hits) == 0 {
		return nil
	}
	return hits
}

// Pick returns the selected element, an error naming what was missing
// otherwise. A label-only selector prefers an actionable row (one that
// exposes invoke/set_value/expand) over a static text row with the same
// words, which is what a click on "确定" means.
func (s Snapshot) Pick(sel Selector) (Element, error) {
	hits := s.Match(sel)
	if len(hits) == 0 {
		return Element{}, fmt.Errorf("%s not found in %s (%d rows read%s)", sel.String(), describeWindow(s), s.Returned, truncationNote(s))
	}
	if sel.Index > 0 {
		if sel.Index >= len(hits) {
			return Element{}, fmt.Errorf("%s matches %d element(s); index %d is out of range", sel.String(), len(hits), sel.Index)
		}
		return hits[sel.Index], nil
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	if sel.Label != "" {
		for _, e := range hits {
			if len(e.Actions) > 0 {
				return e, nil
			}
		}
	}
	return hits[0], nil
}

// String renders a selector for a message.
func (sel Selector) String() string {
	parts := make([]string, 0, 2)
	if sel.Role != "" {
		parts = append(parts, "role="+sel.Role)
	}
	if sel.RolePrefix != "" {
		parts = append(parts, "role~"+sel.RolePrefix+"*")
	}
	if sel.Label != "" {
		parts = append(parts, "label~"+sel.Label)
	}
	if len(parts) == 0 {
		return "element"
	}
	return strings.Join(parts, " ")
}

func describeWindow(s Snapshot) string {
	title := s.WindowTitle
	if title == "" {
		title = s.AppName
	}
	if strings.TrimSpace(s.Query) != "" {
		return fmt.Sprintf("window %q (query %q)", title, s.Query)
	}
	return fmt.Sprintf("window %q", title)
}

func truncationNote(s Snapshot) string {
	switch {
	case s.Truncated && !s.Complete:
		return fmt.Sprintf("; the tree was cut at %d of %d rows, so a match may sit outside this read", s.Returned, s.Total)
	case s.Truncated:
		return fmt.Sprintf("; %d of %d rows stopped at the row limit", s.Returned, s.Total)
	}
	return ""
}

// ElementsText renders rows the way a model reads an element list: one
// line per row, with the token it must act by.
func ElementsText(rows []Element, limit int) string {
	if len(rows) == 0 {
		return "no elements"
	}
	var b strings.Builder
	for i, e := range rows {
		if limit > 0 && i >= limit {
			fmt.Fprintf(&b, "… %d more not listed\n", len(rows)-limit)
			break
		}
		fmt.Fprintf(&b, "[%s] %s %q", e.Token, e.Role, e.Label)
		if strings.TrimSpace(e.Value) != "" && e.Value != e.Label {
			fmt.Fprintf(&b, " value=%q", oneLine(e.Value, 60))
		}
		fmt.Fprintf(&b, " at screen %d,%d %dx%d", e.Frame.X, e.Frame.Y, e.Frame.W, e.Frame.H)
		if len(e.Actions) > 0 {
			fmt.Fprintf(&b, " [%s]", strings.Join(e.Actions, ","))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// # Verification

// Expect is one verify_state predicate. Exactly one of Element and Window
// is set.
type Expect struct {
	Element *ElementExpect `json:"element,omitempty"`
	Window  *WindowExpect  `json:"window,omitempty"`
}

// ElementExpect asserts something about rows matching Selector. Absence of
// an element cannot be asserted (the driver refuses it: element snapshots
// are not exhaustive).
type ElementExpect struct {
	Selector    Selector `json:"selector"`
	Exists      *bool    `json:"exists,omitempty"`
	Enabled     *bool    `json:"enabled,omitempty"`
	Selected    *bool    `json:"selected,omitempty"`
	ValueEquals string   `json:"value_equals,omitempty"`
}

// WindowExpect asserts the window's existence and, optionally, its frame
// within tolerancePx.
type WindowExpect struct {
	Exists      *bool  `json:"exists,omitempty"`
	Bounds      *Frame `json:"bounds,omitempty"`
	TolerancePx int    `json:"tolerance_px,omitempty"`
}

// Verdict is the driver's answer to one verify_state call.
type Verdict struct {
	Status     string          `json:"status"` // satisfied | unsatisfied | unknown
	Stable     bool            `json:"stable"`
	Samples    int             `json:"samples"`
	ElapsedMs  int64           `json:"elapsed_ms"`
	Predicates []PredicateNote `json:"predicates"`
	Text       string          `json:"text"`
}

// PredicateNote explains one predicate's outcome.
type PredicateNote struct {
	Index         int    `json:"index"`
	Status        string `json:"status"`
	UnknownReason string `json:"unknown_reason"`
	Observed      string `json:"observed_json"`
}

// Satisfied reports a verdict the task may rely on.
func (v Verdict) Satisfied() bool { return v.Status == "satisfied" }

// Verify runs verify_state. stableSamples (>=1) is how many consecutive
// samples must agree; timeoutMs bounds the polling. A dead selector comes
// back as status "unknown" with reason observation_unavailable, which is
// the driver saying "no row matched, so there is nothing to assert".
func (d *Driver) Verify(ctx context.Context, pid, windowID int, expect []Expect, stableSamples int, timeoutMs int) (Verdict, error) {
	if len(expect) == 0 {
		return Verdict{}, fmt.Errorf("verify needs at least one expectation")
	}
	prepared := make([]Expect, len(expect))
	for i, e := range expect {
		if e.Element != nil {
			// A role prefix is a MaClaw reading of the tree, not a driver
			// selector: resolve it to the exact role from a fresh read.
			if e.Element.Selector.RolePrefix != "" && e.Element.Selector.Role == "" {
				snap, err := d.Snapshot(ctx, pid, windowID, e.Element.Selector.RolePrefix, 1)
				if err != nil {
					return Verdict{}, err
				}
				hits := snap.Match(e.Element.Selector)
				if len(hits) == 0 {
					return Verdict{Status: "unknown", Text: fmt.Sprintf("verify: no %s role in %s", e.Element.Selector.RolePrefix, describeWindow(snap))}, nil
				}
				e.Element.Selector.Role = hits[0].Role
			}
			if e.Element.Exists != nil && !*e.Element.Exists {
				return Verdict{}, fmt.Errorf("element absence cannot be verified: element snapshots are not exhaustive; assert window.exists=false instead")
			}
		}
		prepared[i] = e
	}
	wire := make([]map[string]any, 0, len(prepared))
	for _, e := range prepared {
		m := map[string]any{}
		if e.Element != nil {
			el := map[string]any{}
			if e.Element.Selector.Role != "" {
				el["role"] = e.Element.Selector.Role
			}
			if e.Element.Selector.Label != "" {
				el["label_contains"] = e.Element.Selector.Label
			}
			sel := map[string]any{}
			if len(el) > 0 {
				sel = el
			}
			cond := map[string]any{"selector": sel}
			if e.Element.Exists != nil {
				cond["exists"] = *e.Element.Exists
			}
			if e.Element.Enabled != nil {
				cond["enabled"] = *e.Element.Enabled
			}
			if e.Element.Selected != nil {
				cond["selected"] = *e.Element.Selected
			}
			if e.Element.ValueEquals != "" {
				cond["value_equals"] = e.Element.ValueEquals
			}
			m["element"] = cond
		}
		if e.Window != nil {
			w := map[string]any{}
			if e.Window.Exists != nil {
				w["exists"] = *e.Window.Exists
			}
			if e.Window.Bounds != nil {
				b := e.Window.Bounds
				bounds := map[string]any{"x": b.X, "y": b.Y, "width": b.W, "height": b.H}
				if e.Window.TolerancePx > 0 {
					bounds["tolerance_px"] = e.Window.TolerancePx
				}
				w["bounds"] = bounds
			}
			m["window"] = w
		}
		wire = append(wire, m)
	}
	args := map[string]any{
		"pid":            int64(pid),
		"window_id":      int64(windowID),
		"expect":         wire,
		"stable_samples": max(1, stableSamples),
		"timeout_ms":     timeoutMs,
	}
	out, text, err := d.CallDetail(ctx, "verify_state", args)
	if err != nil {
		return Verdict{}, err
	}
	v := Verdict{Text: text}
	raw, err := remarshal[struct {
		Status     string          `json:"status"`
		Stable     bool            `json:"stable"`
		Samples    int             `json:"samples"`
		ElapsedMs  int64           `json:"elapsed_ms"`
		Predicates []PredicateNote `json:"predicates"`
	}](out)
	if err != nil {
		return v, err
	}
	v.Status, v.Stable, v.Samples, v.ElapsedMs, v.Predicates = raw.Status, raw.Stable, raw.Samples, raw.ElapsedMs, raw.Predicates
	return v, nil
}

// VerifyText renders a verdict for a model: the driver's line plus the
// first unmet predicate's reason.
func VerifyText(v Verdict) string {
	if v.Status == "satisfied" {
		return fmt.Sprintf("verified: %d expectation(s) held for %d consecutive sample(s) in %d ms", len(v.Predicates), v.Samples, v.ElapsedMs)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "not verified (%s): %s", v.Status, oneLine(v.Text, 300))
	for _, p := range v.Predicates {
		if p.Status == "satisfied" {
			continue
		}
		reason := p.UnknownReason
		if reason == "" {
			reason = p.Status
		}
		fmt.Fprintf(&b, "; expectation %d: %s", p.Index+1, reason)
		if strings.TrimSpace(p.Observed) != "" {
			fmt.Fprintf(&b, " (saw %s)", oneLine(p.Observed, 200))
		}
	}
	return b.String()
}

func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
	r := []rune(s)
	if limit > 0 && len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}

// SortedActions lists an element's action names sorted, for messages.
func SortedActions(e Element) string {
	names := append([]string(nil), e.Actions...)
	sort.Strings(names)
	return strings.Join(names, ",")
}
