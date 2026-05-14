package agentservice

import (
	"encoding/json"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

func writeBootstrap(path string, spec InstanceBootstrap) error {
	if err := secureMkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fileutil.AtomicWriteFile(path, data, 0o600)
}
