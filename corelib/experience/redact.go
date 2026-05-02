package experience

import (
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

var (
	experienceRedactor     *security.SensitiveDetector
	experienceRedactorOnce sync.Once
)

// RedactExperienceText removes secrets before session evidence is sent to an
// LLM or persisted as learned experience. Experience should preserve reusable
// procedure, never incidental credentials.
func RedactExperienceText(text string) string {
	if text == "" {
		return text
	}
	experienceRedactorOnce.Do(func() {
		experienceRedactor = security.NewSensitiveDetector()
	})
	return experienceRedactor.Redact(text)
}
