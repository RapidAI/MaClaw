package agentservice

import "os"

func secureMkdirAll(path string) error {
	return os.MkdirAll(path, 0o700)
}
