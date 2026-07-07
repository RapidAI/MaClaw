//go:build !windows

package tool

// AppendNoWindowEnv is a no-op on non-Windows platforms.
// On Windows it injects a sitecustomize.py via PYTHONPATH that patches
// subprocess.Popen to suppress console window creation for child processes.
func AppendNoWindowEnv(env []string) []string {
	return env
}
