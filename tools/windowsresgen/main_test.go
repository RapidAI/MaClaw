package main

import (
	"testing"

	"github.com/tc-hib/winres"
)

func TestTargetArch(t *testing.T) {
	tests := []struct {
		name string
		want winres.Arch
		ok   bool
	}{
		{name: "amd64", want: winres.ArchAMD64, ok: true},
		{name: "arm64", want: winres.ArchARM64, ok: true},
		{name: "unsupported", ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := targetArch(test.name)
			if test.ok {
				if err != nil || got != test.want {
					t.Fatalf("targetArch(%q) = %q, %v; want %q, nil", test.name, got, err, test.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("targetArch(%q) succeeded unexpectedly with %q", test.name, got)
			}
		})
	}
}
