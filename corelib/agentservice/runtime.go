package agentservice

import (
	"encoding/json"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

func runtimeConfigPath(dataDir string) string {
	return filepath.Join(dataDir, "config.json")
}

func writeRuntimeConfig(dataDir string, cfg corelib.AppConfig) error {
	path := runtimeConfigPath(dataDir)
	if err := secureMkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fileutil.AtomicWriteFile(path, data, 0o600)
}

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
