package desktop

import "testing"

func TestFitHostResourcesFollowsTheMachine(t *testing.T) {
	if _, err := FitHostResources(6<<30, 4); err != ErrHostTooSmall {
		t.Fatalf("small machine err = %v", err)
	}
	cases := []struct {
		ram  uint64
		cpus int
		mem  string
		cpu  string
		shm  string
	}{
		{ram: 7 << 30, cpus: 2, mem: "3g", cpu: "1.5", shm: "1g"},
		{ram: 14<<30 - 1, cpus: 4, mem: "3g", cpu: "2", shm: "1g"},
		{ram: 14 << 30, cpus: 8, mem: "4g", cpu: "4", shm: "1g"},
		{ram: 28<<30 - 1, cpus: 16, mem: "4g", cpu: "4", shm: "1g"},
		{ram: 28 << 30, cpus: 1, mem: "6g", cpu: "1.5", shm: "2g"},
		{ram: 64 << 30, cpus: 0, mem: "6g", cpu: "1.5", shm: "2g"},
	}
	for _, tc := range cases {
		got, err := FitHostResources(tc.ram, tc.cpus)
		if err != nil {
			t.Fatalf("ram %d: %v", tc.ram, err)
		}
		if got.Memory != tc.mem || got.CPUs != tc.cpu || got.ShmSize != tc.shm {
			t.Fatalf("ram %d cpus %d = %+v", tc.ram, tc.cpus, got)
		}
	}
}
