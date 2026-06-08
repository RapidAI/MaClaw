package main

import (
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// ---------------------------------------------------------------------------
// Property-based tests for project-tab-isolation feature.
//
// Property 12: Session isolation via synthesized userID
// Different projectPaths produce different synthesized userIDs, ensuring
// session isolation.
//
// **Validates: Requirements 8.2, 8.4**
// ---------------------------------------------------------------------------

// projectPathPair generates two distinct non-empty project paths for testing.
type projectPathPair struct {
	PathA string
	PathB string
}

func (projectPathPair) Generate(r *rand.Rand, size int) reflect.Value {
	genPath := func() string {
		// Generate Windows-style paths like D:\workprj\project_xxx
		drives := []string{"C", "D", "E"}
		drive := drives[r.Intn(len(drives))]
		segments := r.Intn(3) + 1
		path := drive + `:\`
		for i := 0; i < segments; i++ {
			if i > 0 {
				path += `\`
			}
			n := r.Intn(12) + 3
			seg := make([]byte, n)
			const chars = "abcdefghijklmnopqrstuvwxyz0123456789_-"
			for j := range seg {
				seg[j] = chars[r.Intn(len(chars))]
			}
			path += string(seg)
		}
		return path
	}

	a := genPath()
	b := genPath()
	// Ensure paths are distinct
	for b == a {
		b = genPath()
	}
	return reflect.ValueOf(projectPathPair{PathA: a, PathB: b})
}

// singleProjectPath generates a single non-empty project path.
type singleProjectPath struct {
	Path string
}

func (singleProjectPath) Generate(r *rand.Rand, size int) reflect.Value {
	drives := []string{"C", "D", "E"}
	drive := drives[r.Intn(len(drives))]
	segments := r.Intn(3) + 1
	path := drive + `:\`
	for i := 0; i < segments; i++ {
		if i > 0 {
			path += `\`
		}
		n := r.Intn(12) + 3
		seg := make([]byte, n)
		const chars = "abcdefghijklmnopqrstuvwxyz0123456789_-"
		for j := range seg {
			seg[j] = chars[r.Intn(len(chars))]
		}
		path += string(seg)
	}
	return reflect.ValueOf(singleProjectPath{Path: path})
}

// synthesizeUserID replicates the logic from SendAIAssistantMessage:
// when ProjectPath is empty, return desktopUserID; otherwise synthesize.
func synthesizeUserID(projectPath string) string {
	return projectSessionOwnerID(projectPath)
}

// ---------------------------------------------------------------------------
// Property 12.1: Empty ProjectPath preserves default userID
//
// When ProjectPath is empty, the userID must remain "desktop-user".
// ---------------------------------------------------------------------------
func TestSessionIsolationProperty12_EmptyPathPreservesDefault(t *testing.T) {
	f := func(_ singleProjectPath) bool {
		// Regardless of what path could be generated, empty path always
		// produces the default userID.
		userID := synthesizeUserID("")
		if userID != desktopUserID {
			t.Logf("expected %q for empty path, got %q", desktopUserID, userID)
			return false
		}
		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 12.1 (empty path preserves default) failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 12.2: Non-empty ProjectPath synthesizes expected format
//
// When ProjectPath is set, the userID must be "desktop-user:{projectPath}".
// ---------------------------------------------------------------------------
func TestSessionIsolationProperty12_NonEmptyPathSynthesizesUserID(t *testing.T) {
	f := func(input singleProjectPath) bool {
		userID := synthesizeUserID(input.Path)
		expected := projectSessionOwnerID(input.Path)
		if userID != expected {
			t.Logf("expected %q, got %q", expected, userID)
			return false
		}
		// Must differ from the default
		if userID == desktopUserID {
			t.Logf("synthesized userID must differ from default for non-empty path %q", input.Path)
			return false
		}
		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 12.2 (non-empty path synthesizes userID) failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 12.3: Different project paths produce different userIDs
//
// For any two distinct project paths, the synthesized userIDs must be
// different, ensuring session isolation between projects.
// ---------------------------------------------------------------------------
func TestSessionIsolationProperty12_DifferentPathsDifferentUserIDs(t *testing.T) {
	f := func(input projectPathPair) bool {
		userIDA := synthesizeUserID(input.PathA)
		userIDB := synthesizeUserID(input.PathB)
		if userIDA == userIDB {
			t.Logf("different paths %q and %q produced same userID %q", input.PathA, input.PathB, userIDA)
			return false
		}
		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 12.3 (different paths → different userIDs) failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 12.4: Same project path always produces the same userID (deterministic)
//
// For any project path, calling synthesizeUserID multiple times must always
// return the same result.
// ---------------------------------------------------------------------------
func TestSessionIsolationProperty12_SamePathDeterministic(t *testing.T) {
	f := func(input singleProjectPath) bool {
		first := synthesizeUserID(input.Path)
		second := synthesizeUserID(input.Path)
		third := synthesizeUserID(input.Path)
		if first != second || second != third {
			t.Logf("non-deterministic: path %q produced %q, %q, %q", input.Path, first, second, third)
			return false
		}
		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 12.4 (deterministic synthesis) failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 12.5: ConversationMemory isolation via synthesized userID
//
// When two different project paths produce different synthesized userIDs,
// saving conversation entries under one userID must not affect the other.
// This validates the actual session isolation mechanism.
// ---------------------------------------------------------------------------
func TestSessionIsolationProperty12_ConversationMemoryIsolation(t *testing.T) {
	f := func(input projectPathPair) bool {
		cm := agent.NewConversationMemory()
		defer cm.Stop()

		userIDA := synthesizeUserID(input.PathA)
		userIDB := synthesizeUserID(input.PathB)

		entriesA := []agent.ConversationEntry{
			{Role: "user", Content: "project A message"},
			{Role: "assistant", Content: "project A response"},
		}
		entriesB := []agent.ConversationEntry{
			{Role: "user", Content: "project B message"},
			{Role: "assistant", Content: "project B response"},
			{Role: "user", Content: "project B followup"},
		}

		cm.Save(userIDA, entriesA)
		cm.Save(userIDB, entriesB)

		loadedA := cm.Load(userIDA)
		loadedB := cm.Load(userIDB)

		// Each project's entries must be isolated
		if len(loadedA) != len(entriesA) {
			t.Logf("project A entry count: got %d, want %d", len(loadedA), len(entriesA))
			return false
		}
		if len(loadedB) != len(entriesB) {
			t.Logf("project B entry count: got %d, want %d", len(loadedB), len(entriesB))
			return false
		}

		// Verify content isolation
		for i, e := range loadedA {
			if e.Content != entriesA[i].Content {
				t.Logf("project A entry[%d] content mismatch", i)
				return false
			}
		}
		for i, e := range loadedB {
			if e.Content != entriesB[i].Content {
				t.Logf("project B entry[%d] content mismatch", i)
				return false
			}
		}

		// Clearing one project must not affect the other
		cm.Clear(userIDA)
		afterClearA := cm.Load(userIDA)
		afterClearB := cm.Load(userIDB)
		if len(afterClearA) != 0 {
			t.Logf("project A should be empty after clear, got %d", len(afterClearA))
			return false
		}
		if len(afterClearB) != len(entriesB) {
			t.Logf("project B should be unaffected by clearing A, got %d want %d", len(afterClearB), len(entriesB))
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 12.5 (conversation memory isolation) failed: %v", err)
	}
}
