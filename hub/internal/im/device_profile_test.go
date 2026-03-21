package im

import (
	"fmt"
	"sync"
	"testing"
)

func TestDeviceProfileCache_UpdateAndGetAll(t *testing.T) {
	c := NewDeviceProfileCache()

	// Empty initially.
	if got := c.GetAll("user1"); got != nil {
		t.Fatalf("expected nil for empty cache, got %v", got)
	}

	p1 := DeviceProfile{MachineID: "m1", Name: "MacBook"}
	p2 := DeviceProfile{MachineID: "m2", Name: "iMac"}
	c.Update("user1", p1)
	c.Update("user1", p2)

	all := c.GetAll("user1")
	if len(all) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(all))
	}

	names := map[string]bool{}
	for _, p := range all {
		names[p.Name] = true
	}
	if !names["MacBook"] || !names["iMac"] {
		t.Fatalf("unexpected profiles: %v", all)
	}
}

func TestDeviceProfileCache_UpdateOverwrite(t *testing.T) {
	c := NewDeviceProfileCache()
	c.Update("user1", DeviceProfile{MachineID: "m1", Name: "Old"})
	c.Update("user1", DeviceProfile{MachineID: "m1", Name: "New"})

	all := c.GetAll("user1")
	if len(all) != 1 {
		t.Fatalf("expected 1 profile after overwrite, got %d", len(all))
	}
	if all[0].Name != "New" {
		t.Fatalf("expected name=New, got %s", all[0].Name)
	}
}

func TestDeviceProfileCache_Remove(t *testing.T) {
	c := NewDeviceProfileCache()
	c.Update("user1", DeviceProfile{MachineID: "m1", Name: "A"})
	c.Update("user1", DeviceProfile{MachineID: "m2", Name: "B"})

	c.Remove("user1", "m1")
	all := c.GetAll("user1")
	if len(all) != 1 {
		t.Fatalf("expected 1 profile after remove, got %d", len(all))
	}
	if all[0].MachineID != "m2" {
		t.Fatalf("expected m2 remaining, got %s", all[0].MachineID)
	}

	// Remove last → user entry cleaned up.
	c.Remove("user1", "m2")
	if got := c.GetAll("user1"); got != nil {
		t.Fatalf("expected nil after removing all, got %v", got)
	}
}

func TestDeviceProfileCache_RemoveNonExistent(t *testing.T) {
	c := NewDeviceProfileCache()
	// Should not panic.
	c.Remove("nouser", "nomachine")
}

func TestDeviceProfileCache_IsolationBetweenUsers(t *testing.T) {
	c := NewDeviceProfileCache()
	c.Update("user1", DeviceProfile{MachineID: "m1", Name: "A"})
	c.Update("user2", DeviceProfile{MachineID: "m2", Name: "B"})

	if len(c.GetAll("user1")) != 1 {
		t.Fatal("user1 should have 1 profile")
	}
	if len(c.GetAll("user2")) != 1 {
		t.Fatal("user2 should have 1 profile")
	}

	c.Remove("user1", "m1")
	if c.GetAll("user1") != nil {
		t.Fatal("user1 should be empty")
	}
	if len(c.GetAll("user2")) != 1 {
		t.Fatal("user2 should still have 1 profile")
	}
}

func TestDeviceProfileCache_ConcurrentSafety(t *testing.T) {
	c := NewDeviceProfileCache()
	var wg sync.WaitGroup
	const n = 100

	// Concurrent writes.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.Update("user1", DeviceProfile{
				MachineID: fmt.Sprintf("m%d", i),
				Name:      fmt.Sprintf("device%d", i),
			})
		}(i)
	}
	wg.Wait()

	all := c.GetAll("user1")
	if len(all) != n {
		t.Fatalf("expected %d profiles, got %d", n, len(all))
	}

	// Concurrent reads + removes.
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			c.GetAll("user1")
		}(i)
		go func(i int) {
			defer wg.Done()
			c.Remove("user1", fmt.Sprintf("m%d", i))
		}(i)
	}
	wg.Wait()

	if got := c.GetAll("user1"); got != nil {
		t.Fatalf("expected nil after concurrent removes, got %d", len(got))
	}
}
