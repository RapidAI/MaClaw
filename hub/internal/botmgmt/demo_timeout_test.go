package botmgmt

import "testing"

func TestDemoDistillCallOutlivesDefaultClient(t *testing.T) {
	s := &Service{}
	if got := s.httpClient("/api/v1/skills/demo/distill").Timeout; got != demoSkillCallTimeout {
		t.Fatalf("distill timeout %v", got)
	}
	if got := s.httpClient("/api/v1/bots").Timeout; got >= demoSkillCallTimeout {
		t.Fatalf("plain call timeout %v", got)
	}
}
