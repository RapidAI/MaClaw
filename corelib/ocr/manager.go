// corelib/ocr/manager.go — Lazy-load OCR engine with auto-unload after idle.
package ocr

import (
	"image"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/modelmanager"
)

const defaultUnloadDelay = 5 * time.Minute

// Manager provides lazy-loaded, auto-unloading OCR.
// Call Recognize; models load on first use and unload after idle.
type Manager struct {
	mm *modelmanager.Manager[*Engine]
}

// NewManager creates an OCR manager. Models are NOT loaded until first use.
func NewManager(detPath, recPath string) *Manager {
	mm := modelmanager.New(modelmanager.Config[*Engine]{
		Name: "ocr",
		Load: func() (*Engine, error) {
			return NewEngine(detPath, recPath)
		},
		Close:       func(e *Engine) { e.Close() },
		UnloadDelay: defaultUnloadDelay,
	})
	return &Manager{mm: mm}
}

// SetUnloadDelay configures idle timeout before models are unloaded.
func (m *Manager) SetUnloadDelay(d time.Duration) {
	m.mm.SetUnloadDelay(d)
}

// Unload releases the models from memory.
func (m *Manager) Unload() {
	m.mm.Unload()
}

// Shutdown unloads the models; subsequent Recognize calls reload them.
func (m *Manager) Shutdown() {
	m.mm.Shutdown()
}

// Loaded returns true if the models are currently in memory.
func (m *Manager) Loaded() bool {
	return m.mm.Loaded()
}

// Recognize loads models on demand, runs OCR, schedules idle unload.
func (m *Manager) Recognize(img image.Image) ([]Result, error) {
	e, done, err := m.mm.Acquire()
	if err != nil {
		return nil, err
	}
	defer done()
	return e.Recognize(img)
}
