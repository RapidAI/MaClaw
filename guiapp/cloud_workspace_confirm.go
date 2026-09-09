package guiapp

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const frontendConfirmTimeout = 5 * time.Minute

var (
	frontendConfirmMu      sync.Mutex
	frontendConfirmWaiters = map[string]chan bool{}
	frontendConfirmSeq     uint64
)

func (a *App) askFrontendConfirm(title, message, confirmText, cancelText string, danger, defaultIfUnavailable bool) bool {
	if a == nil || !a.hasWailsEventsContext() {
		return defaultIfUnavailable
	}
	frontendConfirmMu.Lock()
	frontendConfirmSeq++
	id := fmt.Sprintf("frontend_confirm_%d", frontendConfirmSeq)
	ch := make(chan bool, 1)
	frontendConfirmWaiters[id] = ch
	frontendConfirmMu.Unlock()
	defer func() {
		frontendConfirmMu.Lock()
		delete(frontendConfirmWaiters, id)
		frontendConfirmMu.Unlock()
	}()
	payload := map[string]any{
		"id":          id,
		"title":       title,
		"message":     message,
		"confirmText": confirmText,
		"cancelText":  cancelText,
	}
	if danger {
		payload["confirmVariant"] = "danger"
	}
	a.emitEvent(EventShowConfirm, payload)
	select {
	case v := <-ch:
		return v
	case <-time.After(frontendConfirmTimeout):
		return false
	}
}

func resolveFrontendConfirm(id string, confirmed bool) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("confirmation id is required")
	}
	frontendConfirmMu.Lock()
	ch, ok := frontendConfirmWaiters[id]
	if ok {
		delete(frontendConfirmWaiters, id)
	}
	frontendConfirmMu.Unlock()
	if !ok {
		return fmt.Errorf("confirmation expired or already handled")
	}
	select {
	case ch <- confirmed:
	default:
	}
	return nil
}
