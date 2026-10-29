package agentservice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/demoskill"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

// ErrDemoSkillDisabled is returned while MACLAW_DEMO_SKILL is off.
var ErrDemoSkillDisabled = errors.New("demo skill is disabled (MACLAW_DEMO_SKILL)")

const demoTimelineFile = "demo-timeline.json"

// DemoDistillInput is the body of POST /api/v1/skills/demo/distill.
type DemoDistillInput struct {
	Timeline demoskill.Timeline `json:"timeline"`
	// Shots maps Ref → base64 PNG. Prefer sending shots so the srv can
	// caption without another round trip; Hub may also caption first.
	Shots map[string]string `json:"shots,omitempty"`
	Notes string            `json:"notes,omitempty"`
}

// DemoRedistillInput re-runs distill on a stored demo skill's timeline
// (optionally with notes) or on a replacement timeline.
type DemoRedistillInput struct {
	SkillName string              `json:"skill_name,omitempty"`
	Timeline  *demoskill.Timeline `json:"timeline,omitempty"`
	Shots     map[string]string   `json:"shots,omitempty"`
	Notes     string              `json:"notes,omitempty"`
}

// DemoSkillUpdateInput patches a demo skill's guidance content (edit page).
type DemoSkillUpdateInput struct {
	Content     string   `json:"content"`
	Description string   `json:"description,omitempty"`
	Triggers    []string `json:"triggers,omitempty"`
}

// DistillDemoSkill turns a recorded demonstration into a knowledge skill and
// installs it through the normal skill store, so the existing skill matcher
// injects it into the agent loop as guidance. The user may edit it later;
// there is no confirm-only gate.
func (s *Service) DistillDemoSkill(ctx context.Context, p Principal, llm demoskill.LLM, tl demoskill.Timeline) (corelib.NLSkillEntry, error) {
	return s.DistillDemoSkillWithCaptions(ctx, p, llm, tl, nil, nil, nil, "")
}

// DistillDemoSkillWithCaptions captions screenshots (vision, else OCR) then
// distills and installs the skill, also writing demo-timeline.json for re-distill.
func (s *Service) DistillDemoSkillWithCaptions(ctx context.Context, p Principal, llm demoskill.LLM, tl demoskill.Timeline, shots map[string][]byte, vision demoskill.VisionCaptioner, ocr demoskill.OCRCaptioner, notes string) (corelib.NLSkillEntry, error) {
	if !demoskill.Enabled() {
		return corelib.NLSkillEntry{}, ErrDemoSkillDisabled
	}
	if notes = strings.TrimSpace(notes); notes != "" {
		tl.Events = append(tl.Events, demoskill.Event{At: time.Now(), Kind: demoskill.KindNarration, Text: "编辑备注：" + notes})
	}
	fetch := func(_ context.Context, _, ref string) ([]byte, error) {
		if shots == nil {
			return nil, fmt.Errorf("no shots")
		}
		data, ok := shots[ref]
		if !ok || len(data) == 0 {
			return nil, fmt.Errorf("missing shot %s", ref)
		}
		return data, nil
	}
	if err := demoskill.Caption(ctx, &tl, fetch, vision, ocr); err != nil {
		return corelib.NLSkillEntry{}, err
	}
	sk, err := demoskill.Distill(ctx, llm, tl)
	if err != nil {
		return corelib.NLSkillEntry{}, err
	}
	entry := sk.Entry(time.Now())
	out, err := s.persistImportedEntries(ctx, p, []corelib.NLSkillEntry{entry}, true)
	if err != nil {
		return corelib.NLSkillEntry{}, err
	}
	if len(out) == 0 {
		return corelib.NLSkillEntry{}, errors.New("demo skill was not installed")
	}
	_ = s.writeDemoTimeline(p, out[0].Name, tl)
	return out[0], nil
}

// UpdateDemoSkillContent overwrites the guidance of an installed demo skill.
func (s *Service) UpdateDemoSkillContent(ctx context.Context, p Principal, name string, in DemoSkillUpdateInput) (corelib.NLSkillEntry, error) {
	if !demoskill.Enabled() {
		return corelib.NLSkillEntry{}, ErrDemoSkillDisabled
	}
	if s == nil {
		return corelib.NLSkillEntry{}, fmt.Errorf("service is unavailable")
	}
	entry, dir, err := s.findSkill(p, name)
	if err != nil {
		return corelib.NLSkillEntry{}, err
	}
	if entry.Source != demoskill.SourceDemo && !strings.Contains(entry.Content, demoskill.SpecMarker) {
		return corelib.NLSkillEntry{}, fmt.Errorf("skill %q is not a demo skill", name)
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return corelib.NLSkillEntry{}, fmt.Errorf("content is required")
	}
	entry.Content = content
	if d := strings.TrimSpace(in.Description); d != "" {
		entry.Description = d
	}
	if len(in.Triggers) > 0 {
		entry.Triggers = in.Triggers
	}
	entry.Type = "knowledge"
	entry.RequiresGUI = true
	entry.SkillDir = ""
	if err := writeEntryToSkillDir(dir, entry); err != nil {
		return corelib.NLSkillEntry{}, err
	}
	_ = fileutil.AtomicWriteFile(filepath.Join(dir, "KNOWLEDGE.md"), []byte(content+"\n"), 0o644)
	entry.SkillDir = dir
	return entry, nil
}

// RedistillDemoSkill reloads the stored timeline (or uses the provided one)
// and overwrites the skill via DistillDemoSkillWithCaptions.
func (s *Service) RedistillDemoSkill(ctx context.Context, p Principal, llm demoskill.LLM, in DemoRedistillInput, vision demoskill.VisionCaptioner, ocr demoskill.OCRCaptioner) (corelib.NLSkillEntry, error) {
	if !demoskill.Enabled() {
		return corelib.NLSkillEntry{}, ErrDemoSkillDisabled
	}
	var tl demoskill.Timeline
	if in.Timeline != nil {
		tl = *in.Timeline
	} else {
		name := strings.TrimSpace(in.SkillName)
		if name == "" {
			return corelib.NLSkillEntry{}, fmt.Errorf("skill_name or timeline is required")
		}
		stored, err := s.readDemoTimeline(p, name)
		if err != nil {
			return corelib.NLSkillEntry{}, err
		}
		tl = stored
	}
	shots := map[string][]byte{}
	for ref, b64 := range in.Shots {
		data, err := decodeDemoShotB64(b64)
		if err == nil && len(data) > 0 {
			shots[ref] = data
		}
	}
	return s.DistillDemoSkillWithCaptions(ctx, p, llm, tl, shots, vision, ocr, in.Notes)
}

func (s *Service) writeDemoTimeline(p Principal, name string, tl demoskill.Timeline) error {
	_, dir, err := s.findSkill(p, name)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(tl, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(dir, demoTimelineFile), raw, 0o644)
}

func (s *Service) readDemoTimeline(p Principal, name string) (demoskill.Timeline, error) {
	_, dir, err := s.findSkill(p, name)
	if err != nil {
		return demoskill.Timeline{}, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, demoTimelineFile))
	if err != nil {
		return demoskill.Timeline{}, fmt.Errorf("demo timeline not found for %q: %w", name, err)
	}
	var tl demoskill.Timeline
	if err := json.Unmarshal(raw, &tl); err != nil {
		return demoskill.Timeline{}, err
	}
	return tl, nil
}

func decodeDemoShotB64(v string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(v))
}
