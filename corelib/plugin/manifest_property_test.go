package plugin

import (
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
)

// Task 2.3: Property 3 — 往返一致性
// 任意有效 PluginManifest 序列化后再反序列化产生等价对象

func genValidManifest(r *rand.Rand) PluginManifest {
	types := []PluginType{PluginTypeMCP, PluginTypeLocalMCP, PluginTypeNLSkill, PluginTypeNative}
	name := randomString(r, 3+r.Intn(10))
	if name == "" {
		name = "a"
	}
	m := PluginManifest{
		Name:    name,
		Version: randomString(r, 5),
		Type:    types[r.Intn(len(types))],
		Author:  randomString(r, 5),
	}
	if r.Intn(2) == 0 {
		m.Description = randomString(r, 20)
	}
	if r.Intn(2) == 0 {
		n := r.Intn(3)
		m.Tags = make([]string, n)
		for i := range m.Tags {
			m.Tags[i] = randomString(r, 4)
		}
	}
	if r.Intn(2) == 0 {
		m.Settings = map[string]interface{}{
			"key": randomString(r, 5),
		}
	}
	return m
}

func randomString(r *rand.Rand, maxLen int) string {
	n := r.Intn(maxLen) + 1
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + r.Intn(26))
	}
	return string(b)
}

func TestProperty_ManifestRoundTrip(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		original := genValidManifest(r)

		data, err := FormatManifestFile(&original)
		if err != nil {
			t.Logf("FormatManifestFile error: %v", err)
			return false
		}
		got, err := ParseManifestBytes(data)
		if err != nil {
			t.Logf("ParseManifestBytes error: %v", err)
			return false
		}

		if got.Name != original.Name {
			t.Logf("Name: %q != %q", got.Name, original.Name)
			return false
		}
		if got.Version != original.Version {
			t.Logf("Version: %q != %q", got.Version, original.Version)
			return false
		}
		if got.Type != original.Type {
			t.Logf("Type: %q != %q", got.Type, original.Type)
			return false
		}
		if got.Author != original.Author {
			return false
		}
		if got.Description != original.Description {
			return false
		}
		if !reflect.DeepEqual(got.Tags, original.Tags) {
			// nil vs empty slice is ok
			if len(got.Tags) != 0 || len(original.Tags) != 0 {
				return false
			}
		}
		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}
