package plugin

import (
	"math/rand"
	"testing"
	"testing/quick"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// Task 6.4: Property 2 — 注册/注销一致性
// 任意顺序注册/注销插件后，registry 中 Name 唯一且状态一致

func TestProperty_Registry_NameUniqueness(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		pr := NewPluginRegistry(tool.NewRegistry())

		// Generate random register/unregister operations
		names := make([]string, 0)
		for i := 0; i < 20; i++ {
			name := randomString(r, 3+r.Intn(5))
			names = append(names, name)
		}

		registered := make(map[string]bool)
		for _, name := range names {
			if r.Intn(3) == 0 && registered[name] {
				// Unregister
				pr.Unregister(name)
				delete(registered, name)
			} else if !registered[name] {
				// Register
				p := &mockRegistryPlugin{name: name}
				if err := pr.Register(p); err == nil {
					registered[name] = true
				}
			}
		}

		// Verify uniqueness
		list := pr.List()
		seen := make(map[string]bool)
		for _, info := range list {
			if seen[info.Name] {
				t.Logf("duplicate name in registry: %q", info.Name)
				return false
			}
			seen[info.Name] = true
		}

		// Verify count matches
		if len(list) != len(registered) {
			t.Logf("list count %d != registered count %d", len(list), len(registered))
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

func TestProperty_Registry_DuplicateAlwaysRejected(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		pr := NewPluginRegistry(tool.NewRegistry())

		name := randomString(r, 5)
		p1 := &mockRegistryPlugin{name: name}
		p2 := &mockRegistryPlugin{name: name}

		err1 := pr.Register(p1)
		err2 := pr.Register(p2)

		if err1 != nil {
			return false // first register should succeed
		}
		if err2 == nil {
			t.Logf("duplicate register of %q should fail", name)
			return false
		}

		// Only one in the list
		list := pr.List()
		count := 0
		for _, info := range list {
			if info.Name == name {
				count++
			}
		}
		return count == 1
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}
