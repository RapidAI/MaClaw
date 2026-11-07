package memory

import (
	"encoding/json"
	"time"
)

type TemporalPatch struct {
	ValidAt      *time.Time
	InvalidAt    *time.Time
	SetValidAt   bool
	SetInvalidAt bool
	Revalidated  bool
}

func (p TemporalPatch) apply(entry *Entry) {
	if entry == nil {
		return
	}
	if p.SetValidAt {
		entry.ValidAt = cloneTimePtr(p.ValidAt)
	}
	if p.SetInvalidAt {
		entry.InvalidAt = cloneTimePtr(p.InvalidAt)
	}
	if p.Revalidated {
		entry.InvalidAt = nil
		entry.Stale = false
		entry.Status = StatusActive
	}
}

func (f *ExtractedFact) UnmarshalJSON(data []byte) error {
	type extractedFactAlias ExtractedFact
	var decoded extractedFactAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*f = ExtractedFact(decoded)
	_, f.ValidAtPresent = fields["valid_at"]
	_, f.InvalidAtPresent = fields["invalid_at"]
	return nil
}
