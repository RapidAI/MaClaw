package desktopd

import (
	"encoding/base64"
	"encoding/json"
)

// commandRequest is one call to the in-container command service.
// shell, click, shot, and ctl run there. The docker exec line is only
// the supervisor command; it does not name bash, xdotool, or sh.
type commandRequest struct {
	Op      string   `json:"op"`
	Command string   `json:"command,omitempty"`
	Display string   `json:"display,omitempty"`
	Args    []string `json:"args,omitempty"`
	Script  string   `json:"script,omitempty"`
	Body    string   `json:"body,omitempty"`
}

func commandServiceArgs(container string, req commandRequest) ([]string, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	return []string{"exec", container, "python3", "/desktop_supervisor.py", "command", encoded}, nil
}
