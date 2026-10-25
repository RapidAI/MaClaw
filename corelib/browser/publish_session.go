package browser

import (
	"encoding/json"
	"fmt"
	"time"
)

type sessionPublishDriver struct{ s *BrowserAgentSession }

// PublishDriver adapts a live session to RunPublish.
func (s *BrowserAgentSession) PublishDriver() PublishDriver { return sessionPublishDriver{s} }

func (d sessionPublishDriver) Send(method string, params interface{}) (json.RawMessage, error) {
	c := d.s.cdpClient()
	if c == nil {
		return nil, fmt.Errorf("browser page is not attached")
	}
	return c.Send(method, params, 15*time.Second)
}
func (d sessionPublishDriver) ArmChooser(files []string) error { return d.s.ArmFileChooser(files) }
func (d sessionPublishDriver) WaitChooser(t time.Duration) (bool, error) {
	return d.s.WaitFileChooser(t)
}
func (d sessionPublishDriver) DisarmChooser() { d.s.DisarmFileChooser() }

// Subscribe streams the session's CDP events to the publish run.
func (d sessionPublishDriver) Subscribe() (<-chan CDPEvent, func()) {
	d.s.startEventPump()
	return subscribeSessionEvents(d.s)
}

// CurrentURL is the active tab's URL ("" when the page does not answer).
func (s *BrowserAgentSession) CurrentURL() string {
	u, _ := s.pageURL()
	return u
}
