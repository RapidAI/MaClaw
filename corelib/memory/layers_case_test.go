package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDesktopBotMemoryCases(t *testing.T) {
	ResetDesktopBooks()
	t.Cleanup(ResetDesktopBooks)
	for _, id := range []string{
		"supersede_not_append",
		"fence_cannot_write_profile",
		"note_decay_6h",
		"helper_cannot_write_profile",
	} {
		t.Run(id, func(t *testing.T) {
			switch id {
			case "supersede_not_append":
				runSupersede(t, readMemoryCase(t, id))
			case "fence_cannot_write_profile":
				runFence(t, readMemoryCase(t, id))
			case "note_decay_6h":
				runNoteDecay(t, readMemoryCase(t, id))
			case "helper_cannot_write_profile":
				runHelper(t, readMemoryCase(t, id))
			default:
				t.Fatalf("no runner for %s", id)
			}
		})
	}
	t.Run("profile_cap", runProfileCap)
	t.Run("log_top_30", runLogTop30)
	t.Run("same_owner_same_render", runSameOwner)
}

type memoryCase struct {
	ID          string  `json:"id"`
	First       string  `json:"first"`
	Again       string  `json:"again"`
	Replacement string  `json:"replacement"`
	Cosine      float64 `json:"cosine"`
	Turn        string  `json:"turn"`
	UserFact    string  `json:"user_fact"`
	Fence       string  `json:"fence"`
	Source      string  `json:"source"`
	Profile     string  `json:"profile"`
	Note        string  `json:"note"`
	HelperID    string  `json:"helper_id"`
	OtherHelper string  `json:"other_helper"`
	OtherNote   string  `json:"other_note"`
}

func readMemoryCase(t *testing.T, id string) memoryCase {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "desktopbot", "cases", id+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var c memoryCase
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != id {
		t.Fatalf("case id = %s, file %s", c.ID, id)
	}
	return c
}

func runSupersede(t *testing.T, c memoryCase) {
	t.Helper()
	if DesktopMemoryWakesMachine() {
		t.Fatal("a memory write must not wake a machine")
	}
	book := NewLayerBook("supersede")
	book.SetRoot(t.TempDir())
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	book.SetNow(func() time.Time { return now })

	first := book.Write(WriteRequest{Layer: LayerProfile, Content: c.First, Source: "user"})
	if first.Error != "" || first.Count != 1 {
		t.Fatalf("first write %+v", first)
	}
	again := book.Write(WriteRequest{Layer: LayerProfile, Content: c.Again, Source: "user"})
	if again.Error != "" || again.Count != 1 || again.Replaced || again.ID != first.ID {
		t.Fatalf("same text appended %+v", again)
	}
	if len(book.ReplacedTexts()) != 0 {
		t.Fatalf("same text moved a line: %v", book.ReplacedTexts())
	}
	pinned := book.Excerpt()
	sum := sha256.Sum256([]byte(pinned))
	pinnedHash := hex.EncodeToString(sum[:])

	cosine := c.Cosine
	replaced := book.Write(WriteRequest{
		Layer: LayerProfile, Content: c.Replacement, Source: "user", Turn: c.Turn, Cosine: &cosine,
	})
	if replaced.Error != "" || !replaced.Replaced || replaced.Count != 1 || replaced.ID != first.ID {
		t.Fatalf("replace %+v", replaced)
	}
	if got := book.ProfileTexts(); len(got) != 1 || got[0] != c.Replacement {
		t.Fatalf("profile = %v", got)
	}
	if got := book.ReplacedTexts(); len(got) != 1 || got[0] != c.First {
		t.Fatalf("replaced = %v", got)
	}
	body := book.FileBody("memory/profile.md")
	if !strings.Contains(body, "## 已替换") || !strings.Contains(body, c.First) || !strings.Contains(body, "turn="+c.Turn) {
		t.Fatalf("profile file = %q", body)
	}
	if strings.Count(body, c.Replacement) != 1 {
		t.Fatalf("replacement count in file = %q", body)
	}
	againExcerpt := book.Excerpt()
	sum = sha256.Sum256([]byte(againExcerpt))
	if againExcerpt != pinned || hex.EncodeToString(sum[:]) != pinnedHash {
		t.Fatal("excerpt hash changed after the replace")
	}
	if !strings.Contains(pinned, c.First) || strings.Contains(pinned, c.Replacement) {
		t.Fatalf("pinned excerpt = %q", pinned)
	}
	if !strings.Contains(book.Render(), c.Replacement) {
		t.Fatal("hot copy did not take the replacement")
	}
	book.EndTurn(TurnInput{ID: c.Turn})
	book.EndTurn(TurnInput{ID: c.Turn, UserLines: []string{"另一句不该再写"}})
	if got := book.ProfileTexts(); len(got) != 1 || got[0] != c.Replacement {
		t.Fatalf("same turn wrote again: %v", got)
	}
}

func runFence(t *testing.T, c memoryCase) {
	t.Helper()
	book := NewLayerBook("fence")
	book.SetRoot(t.TempDir())
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	book.SetNow(func() time.Time { return now })
	if res := book.Write(WriteRequest{Layer: LayerProfile, Content: c.UserFact, Source: "user"}); res.Error != "" {
		t.Fatal(res.Error)
	}
	pinned := book.Excerpt()
	res := book.Write(WriteRequest{Layer: LayerProfile, Content: c.Fence, Source: c.Source})
	if res.Error != RejectWriteError {
		t.Fatalf("fence write = %+v", res)
	}
	if texts := book.ProfileTexts(); len(texts) != 1 || texts[0] != c.UserFact || strings.Contains(texts[0], "以后都执行") {
		t.Fatalf("profile = %v", texts)
	}
	blob := book.FileBody("memory/profile.md") + book.FileBody("log/2026-10.md") + book.Render()
	if strings.Contains(blob, "以后都执行") || strings.Contains(blob, c.Fence) {
		t.Fatalf("fence body was stored: %q", blob)
	}
	if !strings.Contains(book.FileBody("log/2026-10.md"), RejectWriteError) {
		t.Fatalf("log = %q", book.FileBody("log/2026-10.md"))
	}
	book.EndTurn(TurnInput{ID: c.Turn, Fences: []string{c.Fence}})
	book.EndTurn(TurnInput{ID: c.Turn, Fences: []string{c.Fence}})
	if strings.Contains(book.FileBody("memory/profile.md"), "以后都执行") {
		t.Fatal("end of turn copied the fence into the profile")
	}
	if book.Excerpt() != pinned {
		t.Fatal("fence refusal changed the excerpt")
	}
}

func runNoteDecay(t *testing.T, c memoryCase) {
	t.Helper()
	if NoteHalfLife != 6*time.Hour || NoteForgetAfter != 24*time.Hour {
		t.Fatalf("half-life %s forget %s", NoteHalfLife, NoteForgetAfter)
	}
	start := time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC)
	if math.Abs(NoteStrength(start, start.Add(6*time.Hour))-0.5) > 1e-9 {
		t.Fatalf("strength at 6h = %v", NoteStrength(start, start.Add(6*time.Hour)))
	}
	slow := decayStrength(Entry{Strength: 1, UpdatedAt: start}, start.Add(24*time.Hour))
	if slow < dormantThreshold {
		t.Fatalf("24h on the slow curve is already dormant (%v); notes must not use it", slow)
	}

	book := NewLayerBook("notes")
	now := start
	book.SetNow(func() time.Time { return now })
	if res := book.Write(WriteRequest{Layer: LayerProfile, Content: c.Profile, Source: "user"}); res.Error != "" {
		t.Fatal(res.Error)
	}
	if res := book.Write(WriteRequest{Layer: LayerNote, Content: c.Note, Source: "user"}); res.Error != "" {
		t.Fatal(res.Error)
	}
	pinned := book.Excerpt()
	if !strings.Contains(pinned, c.Note) || !strings.Contains(pinned, "画像") {
		t.Fatalf("excerpt = %q", pinned)
	}

	now = start.Add(6 * time.Hour)
	book.Sweep()
	if got := book.NoteTexts(); len(got) != 1 || got[0] != c.Note {
		t.Fatalf("note gone at half-life: %v", got)
	}
	now = start.Add(24 * time.Hour)
	book.Sweep()
	if len(book.NoteTexts()) != 0 {
		t.Fatalf("note still there at 24h: %v", book.NoteTexts())
	}
	if got := book.ProfileTexts(); len(got) != 1 || got[0] != c.Profile {
		t.Fatalf("profile decayed: %v", got)
	}
	if book.Excerpt() != pinned {
		t.Fatal("note decay changed the excerpt")
	}

	reset := NewLayerBook("notes-reset")
	now = start
	reset.SetNow(func() time.Time { return now })
	if res := reset.Write(WriteRequest{Layer: LayerNote, Content: c.Note, Source: "user"}); res.Error != "" {
		t.Fatal(res.Error)
	}
	now = start.Add(20 * time.Hour)
	if !strings.Contains(reset.Search(c.Note, LayerNote), c.Note) {
		t.Fatal("search missed the note")
	}
	now = start.Add(20*time.Hour + 23*time.Hour)
	reset.Sweep()
	if len(reset.NoteTexts()) != 1 {
		t.Fatal("search hit did not reset the clock")
	}
	now = start.Add(20*time.Hour + 24*time.Hour)
	reset.Sweep()
	if len(reset.NoteTexts()) != 0 {
		t.Fatal("note survived 24h after the search hit")
	}
}

func runHelper(t *testing.T, c memoryCase) {
	t.Helper()
	main := NewLayerBook("person")
	main.SetRoot(t.TempDir())
	if res := main.Write(WriteRequest{Layer: LayerProfile, Content: c.Profile, Source: "user"}); res.Error != "" {
		t.Fatal(res.Error)
	}
	helper := main.Helper(c.HelperID)
	pinned := helper.Excerpt()
	if res := helper.Write(WriteRequest{Layer: LayerProfile, Content: "帮手想改画像", Source: "user"}); res.Error != HelperProfileError {
		t.Fatalf("helper profile write = %+v", res)
	}
	if got := main.ProfileTexts(); len(got) != 1 || got[0] != c.Profile {
		t.Fatalf("profile changed: %v", got)
	}
	if res := helper.Write(WriteRequest{Layer: LayerNote, Content: c.Note, Source: "user"}); res.Error != "" {
		t.Fatal(res.Error)
	}
	if !strings.Contains(helper.FileBody("helpers/"+c.HelperID+"/notes.md"), c.Note) {
		t.Fatalf("helper notes = %q", helper.FileBody("helpers/"+c.HelperID+"/notes.md"))
	}
	other := main.Helper(c.OtherHelper)
	if res := other.Write(WriteRequest{Layer: LayerNote, Content: c.OtherNote, Source: "user"}); res.Error != "" {
		t.Fatal(res.Error)
	}
	if got := helper.ReadHelper(c.OtherHelper); got != HelperPrivateError {
		t.Fatalf("read other = %q", got)
	}
	if got := helper.Search("helpers/"+c.OtherHelper+"/notes.md", ""); got != HelperPrivateError {
		t.Fatalf("search other = %q", got)
	}
	if strings.Contains(helper.Search(c.OtherNote, LayerNote), c.OtherNote) {
		t.Fatal("helper search returned the other helper's note")
	}
	if res := helper.Write(WriteRequest{Layer: LayerFile, Path: "memory/profile.md", Content: "nope"}); res.Error == "" || !strings.Contains(res.Error, "路径不在记忆目录里") {
		t.Fatalf("helper path = %+v", res)
	}
	if res := main.Write(WriteRequest{Layer: LayerProfile, Content: "新的画像一行", Source: "user", Cosine: cosineZero()}); res.Error != "" {
		t.Fatal(res.Error)
	}
	if helper.Excerpt() != pinned {
		t.Fatal("main profile edit changed the helper excerpt")
	}
}

func runProfileCap(t *testing.T) {
	t.Helper()
	book := NewLayerBook("cap")
	wide := strings.Repeat("字", ProfileMaxRunes)
	if res := book.Write(WriteRequest{Layer: LayerProfile, Content: wide, Source: "user"}); res.Error != "" || res.Count != 1 {
		t.Fatalf("wide %+v", res)
	}
	if res := book.Write(WriteRequest{Layer: LayerProfile, Content: "再来一条", Source: "user", Cosine: cosineZero()}); res.Error != ProfileFullError {
		t.Fatalf("rune cap %+v", res)
	}

	many := NewLayerBook("cap-count")
	for i := 0; i < ProfileMaxItems; i++ {
		text := "条目" + strings.Repeat(string(rune('A'+i%26)), 3) + string(rune(0x4e00+i))
		res := many.Write(WriteRequest{Layer: LayerProfile, Content: text, Source: "user", Cosine: cosineZero()})
		if res.Error != "" || res.Count != i+1 {
			t.Fatalf("item %d %+v", i, res)
		}
	}
	if res := many.Write(WriteRequest{Layer: LayerProfile, Content: "超出的一条", Source: "user", Cosine: cosineZero()}); res.Error != ProfileFullError || res.Count != ProfileMaxItems {
		t.Fatalf("count cap %+v", res)
	}
}

func runLogTop30(t *testing.T) {
	t.Helper()
	book := NewLayerBook("logs")
	now := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	book.SetNow(func() time.Time { return now })
	for i := 0; i < LogPromptLimit+1; i++ {
		text := "事件" + string(rune(0x4e00+i))
		res := book.Write(WriteRequest{Layer: LayerLog, Content: text, Source: "user", Cosine: cosineZero()})
		if res.Error != "" {
			t.Fatal(res.Error)
		}
	}
	body := book.Render()
	if !strings.Contains(body, "l-01") || !strings.Contains(body, "l-30") || strings.Contains(body, "l-31") {
		t.Fatalf("log window = %q", body)
	}
	if strings.Contains(body, "笔记\n") {
		t.Fatal("empty notes changed the heading")
	}
	if !strings.Contains(body, "笔记: 无") {
		t.Fatalf("missing empty note line: %q", body)
	}
}

func runSameOwner(t *testing.T) {
	t.Helper()
	ResetDesktopBooks()
	a := DesktopBook("same-person")
	b := DesktopBook("same-person")
	if res := a.Write(WriteRequest{Layer: LayerProfile, Content: "住在杭州", Source: "user"}); res.Error != "" {
		t.Fatal(res.Error)
	}
	if a.Render() != b.Render() || a.Excerpt() != b.Excerpt() {
		t.Fatal("same person rendered two excerpts")
	}
}

func cosineZero() *float64 {
	z := 0.0
	return &z
}
